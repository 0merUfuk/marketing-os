package skills

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxSkillBytes     = 512 * 1024
	maxReferenceBytes = 1024 * 1024
	maxBundleBytes    = 2 * 1024 * 1024
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Skill struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	License      string         `json:"license,omitempty"`
	Version      string         `json:"version,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Instructions string         `json:"-"`
	References   []string       `json:"references"`
	Scripts      []string       `json:"scripts"`
	Assets       []string       `json:"assets"`
	// Path is informational provenance. Verified consumers must load content
	// through VerifiedSnapshot so they consume the bytes that were hashed.
	Path string `json:"path"`
}

type Bundle struct {
	Skill      Skill             `json:"skill"`
	References map[string]string `json:"references"`
}

type Loader struct {
	RepositoryPath string
	LockPath       string
}

func NewLoader(repositoryPath, lockPath string) *Loader {
	return &Loader{RepositoryPath: repositoryPath, LockPath: lockPath}
}

func (l *Loader) Index(ctx context.Context) ([]Skill, error) {
	snapshot, err := l.captureRepository(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.index(ctx)
}

func (l *Loader) Load(ctx context.Context, name string, requestedReferences []string) (Bundle, error) {
	snapshot, err := l.captureRepository(ctx)
	if err != nil {
		return Bundle{}, err
	}
	return snapshot.load(ctx, name, requestedReferences)
}

func (l *Loader) readSkill(directoryName string) (Skill, error) {
	if err := validateSkillName(directoryName); err != nil {
		return Skill{}, fmt.Errorf("invalid skill directory %q: %w", directoryName, err)
	}
	if err := requireNonSymlinkDirectory(l.RepositoryPath); err != nil {
		return Skill{}, fmt.Errorf("inspect skills repository: %w", err)
	}
	skillsDirectory := filepath.Join(l.RepositoryPath, "skills")
	if err := requireNonSymlinkDirectory(skillsDirectory); err != nil {
		return Skill{}, fmt.Errorf("inspect skills directory: %w", err)
	}
	skillDirectory := filepath.Join(skillsDirectory, directoryName)
	if err := requireNonSymlinkDirectory(skillDirectory); err != nil {
		return Skill{}, fmt.Errorf("inspect skill directory %s: %w", directoryName, err)
	}
	path := filepath.Join(skillDirectory, "SKILL.md")
	content, err := readBoundedFile(path, maxSkillBytes)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", directoryName, err)
	}
	front, body, err := parseFrontmatter(content)
	if err != nil {
		return Skill{}, fmt.Errorf("parse skill %s: %w", directoryName, err)
	}
	if front.Name != directoryName {
		return Skill{}, fmt.Errorf("skill name %q does not match directory %q", front.Name, directoryName)
	}
	if err := validateSkillName(front.Name); err != nil {
		return Skill{}, err
	}
	if len(front.Description) == 0 || len(front.Description) > 1024 {
		return Skill{}, errors.New("skill description must contain 1-1024 characters")
	}
	version := ""
	if value, ok := front.Metadata["version"]; ok {
		version = fmt.Sprint(value)
	}
	skill := Skill{
		Name: front.Name, Description: front.Description, License: front.License,
		Version: version, Metadata: front.Metadata, Instructions: body, Path: path,
	}
	skill.References, err = indexOptionalFiles(filepath.Join(filepath.Dir(path), "references"))
	if err != nil {
		return Skill{}, err
	}
	skill.Scripts, err = indexOptionalFiles(filepath.Join(filepath.Dir(path), "scripts"))
	if err != nil {
		return Skill{}, err
	}
	skill.Assets, err = indexOptionalFiles(filepath.Join(filepath.Dir(path), "assets"))
	if err != nil {
		return Skill{}, err
	}
	return skill, nil
}

type frontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	License     string         `yaml:"license"`
	Metadata    map[string]any `yaml:"metadata"`
}

func parseFrontmatter(content []byte) (frontmatter, string, error) {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), maxSkillBytes)
	if !scanner.Scan() || scanner.Text() != "---" {
		return frontmatter{}, "", errors.New("missing opening YAML frontmatter delimiter")
	}
	var yamlLines []string
	foundEnd := false
	for scanner.Scan() {
		if scanner.Text() == "---" {
			foundEnd = true
			break
		}
		yamlLines = append(yamlLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return frontmatter{}, "", err
	}
	if !foundEnd {
		return frontmatter{}, "", errors.New("missing closing YAML frontmatter delimiter")
	}
	var front frontmatter
	decoder := yaml.NewDecoder(strings.NewReader(strings.Join(yamlLines, "\n")))
	decoder.KnownFields(true)
	if err := decoder.Decode(&front); err != nil {
		return frontmatter{}, "", fmt.Errorf("decode YAML frontmatter: %w", err)
	}
	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return frontmatter{}, "", err
	}
	return front, strings.TrimSpace(body.String()), nil
}

func indexOptionalFiles(root string) ([]string, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("optional skill path %s is not a non-symlink directory", root)
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in skill optional directory: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type() != 0 {
			return fmt.Errorf("optional skill file must be regular: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func validateSkillName(name string) error {
	if len(name) < 1 || len(name) > 64 || !skillNamePattern.MatchString(name) || strings.Contains(name, "--") {
		return errors.New("skill name must be 1-64 lowercase alphanumeric/hyphen characters without edge or consecutive hyphens")
	}
	return nil
}

func cleanRelative(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return "", errors.New("path must be a non-empty slash-separated relative path")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return "", errors.New("path traversal is not allowed")
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path traversal is not allowed")
	}
	return clean, nil
}

func readBoundedFile(path string, max int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("only regular non-symlink files are allowed")
	}
	if info.Size() > max {
		return nil, fmt.Errorf("file exceeds %d-byte limit", max)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > max {
		return nil, fmt.Errorf("file exceeds %d-byte limit", max)
	}
	return content, nil
}

func requireNonSymlinkDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path must be a non-symlink directory")
	}
	return nil
}
