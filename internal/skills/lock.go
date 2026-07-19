package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Lock struct {
	Repository        string    `yaml:"repository" json:"repository"`
	Ref               string    `yaml:"ref" json:"ref"`
	Commit            string    `yaml:"commit" json:"commit"`
	RepositoryVersion string    `yaml:"repository_version" json:"repository_version"`
	ManifestSHA256    string    `yaml:"manifest_sha256" json:"manifest_sha256"`
	UpdatedAt         time.Time `yaml:"updated_at" json:"updated_at"`
}

type Status struct {
	Lock            Lock   `json:"lock"`
	ActualCommit    string `json:"actual_commit,omitempty"`
	ActualManifest  string `json:"actual_manifest"`
	CommitMatches   bool   `json:"commit_matches"`
	ManifestMatches bool   `json:"manifest_matches"`
	PinValid        bool   `json:"pin_valid"`
}

func ReadLock(path string) (Lock, error) {
	f, err := os.Open(path)
	if err != nil {
		return Lock{}, fmt.Errorf("open skills lock: %w", err)
	}
	defer f.Close()
	var lock Lock
	decoder := yaml.NewDecoder(io.LimitReader(f, 64*1024))
	decoder.KnownFields(true)
	if err := decoder.Decode(&lock); err != nil {
		return Lock{}, fmt.Errorf("decode skills lock: %w", err)
	}
	if lock.Repository == "" || lock.Ref == "" || lock.Commit == "" || lock.ManifestSHA256 == "" {
		return Lock{}, errors.New("skills lock requires repository, ref, commit, and manifest_sha256")
	}
	return lock, nil
}

func WriteLock(path string, lock Lock) error {
	if lock.UpdatedAt.IsZero() {
		lock.UpdatedAt = time.Now().UTC()
	}
	data, err := yaml.Marshal(lock)
	if err != nil {
		return fmt.Errorf("encode skills lock: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".skills-lock-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (l *Loader) Status(ctx context.Context) (Status, error) {
	lock, err := ReadLock(l.LockPath)
	if err != nil {
		return Status{}, err
	}
	manifest, err := l.ComputeManifest(ctx)
	if err != nil {
		return Status{}, err
	}
	actualCommit, _ := gitOutput(ctx, l.RepositoryPath, "rev-parse", "HEAD")
	status := Status{
		Lock: lock, ActualCommit: actualCommit, ActualManifest: manifest,
		ManifestMatches: manifest == lock.ManifestSHA256,
	}
	status.CommitMatches = actualCommit == "" || actualCommit == lock.Commit
	status.PinValid = status.CommitMatches && status.ManifestMatches
	return status, nil
}

func (l *Loader) RequirePinned(ctx context.Context) (Lock, error) {
	status, err := l.Status(ctx)
	if err != nil {
		return Lock{}, err
	}
	if !status.PinValid {
		return Lock{}, fmt.Errorf("marketing skills repository does not match lock (commit_match=%t manifest_match=%t)", status.CommitMatches, status.ManifestMatches)
	}
	return status.Lock, nil
}

func (l *Loader) ComputeManifest(ctx context.Context) (string, error) {
	root, err := filepath.Abs(l.RepositoryPath)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve skills repository root: %w", err)
	}
	lockAbs, _ := filepath.Abs(l.LockPath)
	if lockParent, resolveErr := filepath.EvalSymlinks(filepath.Dir(lockAbs)); resolveErr == nil {
		lockAbs = filepath.Join(lockParent, filepath.Base(lockAbs))
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		pathAbs, _ := filepath.Abs(path)
		if pathAbs == lockAbs {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("resolve repository symlink %s: %w", rel, err)
			}
			inside, err := pathContainedBy(root, resolved)
			if err != nil || !inside {
				return fmt.Errorf("repository symlink %s escapes the pinned repository", rel)
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("repository symlink %s must resolve to a regular file", rel)
			}
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk skills repository: %w", err)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		rel, _ := filepath.Rel(root, path)
		io.WriteString(hash, filepath.ToSlash(rel))
		hash.Write([]byte{0})
		contentPath := path
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", err
			}
			io.WriteString(hash, "symlink")
			hash.Write([]byte{0})
			io.WriteString(hash, filepath.ToSlash(target))
			hash.Write([]byte{0})
			contentPath, err = filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
		}
		f, err := os.Open(contentPath)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(hash, f); err != nil {
			f.Close()
			return "", err
		}
		f.Close()
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func pathContainedBy(root, candidate string) (bool, error) {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func (l *Loader) Update(ctx context.Context, repositoryURL, ref string) (Lock, error) {
	if strings.TrimSpace(repositoryURL) == "" || strings.TrimSpace(ref) == "" {
		return Lock{}, errors.New("repository URL and ref are required")
	}
	if dirty, err := gitOutput(ctx, l.RepositoryPath, "status", "--porcelain"); err != nil {
		return Lock{}, fmt.Errorf("inspect skills repository: %w", err)
	} else if dirty != "" {
		return Lock{}, errors.New("skills repository has local changes; refusing update")
	}
	cmd := exec.CommandContext(ctx, "git", "-C", l.RepositoryPath, "fetch", "--depth=1", repositoryURL, ref)
	if output, err := cmd.CombinedOutput(); err != nil {
		return Lock{}, fmt.Errorf("fetch skills ref: %w: %s", err, strings.TrimSpace(string(output)))
	}
	commit, err := gitOutput(ctx, l.RepositoryPath, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return Lock{}, fmt.Errorf("resolve fetched skills commit: %w", err)
	}
	cmd = exec.CommandContext(ctx, "git", "-C", l.RepositoryPath, "checkout", "--detach", commit)
	if output, err := cmd.CombinedOutput(); err != nil {
		return Lock{}, fmt.Errorf("checkout skills commit: %w: %s", err, strings.TrimSpace(string(output)))
	}
	manifest, err := l.ComputeManifest(ctx)
	if err != nil {
		return Lock{}, err
	}
	lock := Lock{
		Repository: repositoryURL, Ref: ref, Commit: commit,
		RepositoryVersion: repositoryVersion(l.RepositoryPath),
		ManifestSHA256:    manifest, UpdatedAt: time.Now().UTC(),
	}
	if err := WriteLock(l.LockPath, lock); err != nil {
		return Lock{}, err
	}
	return lock, nil
}

func repositoryVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	return manifest.Version
}

func gitOutput(ctx context.Context, repository string, args ...string) (string, error) {
	all := append([]string{"-C", repository}, args...)
	output, err := exec.CommandContext(ctx, "git", all...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
