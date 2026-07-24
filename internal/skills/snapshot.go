package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxManifestEntries   = 4096
	maxManifestPathBytes = 1024 * 1024
)

type snapshotEntryKind uint8

const (
	snapshotDirectory snapshotEntryKind = iota + 1
	snapshotRegularFile
	snapshotSymlink
)

type snapshotEntry struct {
	kind          snapshotEntryKind
	content       []byte
	symlinkTarget string
}

// repositorySnapshot owns a bounded, private copy of every byte used to
// calculate a repository manifest. Runtime parsing is performed only from this
// copy, never by reopening a verified path.
type repositorySnapshot struct {
	root     string
	entries  map[string]snapshotEntry
	manifest string
}

// VerifiedSnapshot is an immutable, successfully pinned view of the vendored
// repository. Its fields are private so callers cannot replace verified bytes.
type VerifiedSnapshot struct {
	repository *repositorySnapshot
	lock       Lock
	status     Status
	metadata   SnapshotMetadata
}

type SnapshotMetadata struct {
	ID                      string `json:"id"`
	Distribution            string `json:"distribution"`
	Repository              string `json:"repository"`
	Ref                     string `json:"ref"`
	Commit                  string `json:"commit"`
	RepositoryVersion       string `json:"repository_version"`
	UpstreamManifestSHA256  string `json:"upstream_manifest_sha256"`
	VendoredManifestSHA256  string `json:"vendored_manifest_sha256"`
	ExpectedInventorySHA256 string `json:"expected_inventory_sha256"`
	ExpectedInventoryCount  int    `json:"expected_inventory_count"`
}

func NewSnapshotMetadata(lock Lock, actualVendoredManifest string) SnapshotMetadata {
	return SnapshotMetadata{
		ID:                      snapshotID(lock.Commit, actualVendoredManifest),
		Distribution:            lock.Distribution,
		Repository:              lock.Repository,
		Ref:                     lock.Ref,
		Commit:                  lock.Commit,
		RepositoryVersion:       lock.RepositoryVersion,
		UpstreamManifestSHA256:  lock.UpstreamManifestSHA256,
		VendoredManifestSHA256:  actualVendoredManifest,
		ExpectedInventorySHA256: selectedSkillsIdentity(lock.SelectedSkills),
		ExpectedInventoryCount:  len(lock.SelectedSkills),
	}
}

func (m SnapshotMetadata) Validate() error {
	if m.ID != snapshotID(m.Commit, m.VendoredManifestSHA256) {
		return errors.New("skill snapshot ID does not match its upstream commit and vendored manifest")
	}
	if !sha256Pattern.MatchString(m.ExpectedInventorySHA256) {
		return errors.New("skill snapshot expected inventory identity must be a lowercase 64-character SHA-256")
	}
	if m.ExpectedInventoryCount != 5 {
		return errors.New("skill snapshot expected inventory must contain exactly 5 canonical skills")
	}
	lock := Lock{
		Distribution: m.Distribution, Repository: m.Repository, Ref: m.Ref, Commit: m.Commit,
		RepositoryVersion: m.RepositoryVersion,
		SelectedSkills: []SelectedSkill{
			{Name: "copywriting", Version: "0.0.0"},
			{Name: "emails", Version: "0.0.0"},
			{Name: "launch", Version: "0.0.0"},
			{Name: "product-marketing", Version: "0.0.0"},
			{Name: "social", Version: "0.0.0"},
		},
		UpstreamManifestSHA256: m.UpstreamManifestSHA256,
		VendoredManifestSHA256: m.VendoredManifestSHA256,
		UpdatedAt:              time.Unix(1, 0).UTC(),
	}
	return validateLock(lock)
}

func (m SnapshotMetadata) ValidateInventory(indexed []Skill) error {
	seen := make(map[string]struct{}, len(indexed))
	selected := make([]SelectedSkill, 0, len(indexed))
	for _, skill := range indexed {
		if err := validateSkillName(skill.Name); err != nil {
			return fmt.Errorf("invalid indexed skill name %q: %w", skill.Name, err)
		}
		if !semverPattern.MatchString(skill.Version) {
			return fmt.Errorf("indexed skill %q must have a semantic version", skill.Name)
		}
		if _, exists := seen[skill.Name]; exists {
			return fmt.Errorf("indexed skill inventory contains duplicate %q", skill.Name)
		}
		seen[skill.Name] = struct{}{}
		selected = append(selected, SelectedSkill{Name: skill.Name, Version: skill.Version})
	}
	if len(selected) != m.ExpectedInventoryCount {
		return fmt.Errorf("indexed skill inventory contains %d skills, want %d", len(selected), m.ExpectedInventoryCount)
	}
	if actual := selectedSkillsIdentity(selected); actual != m.ExpectedInventorySHA256 {
		return errors.New("indexed skill inventory does not match the lock-selected name and version set")
	}
	return nil
}

func snapshotID(commit, vendoredManifest string) string {
	hash := sha256.New()
	io.WriteString(hash, "marketing-os-skill-snapshot-v1")
	hash.Write([]byte{0})
	io.WriteString(hash, commit)
	hash.Write([]byte{0})
	io.WriteString(hash, vendoredManifest)
	return hex.EncodeToString(hash.Sum(nil))
}

func selectedSkillsIdentity(selected []SelectedSkill) string {
	canonical := append([]SelectedSkill(nil), selected...)
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].Name == canonical[j].Name {
			return canonical[i].Version < canonical[j].Version
		}
		return canonical[i].Name < canonical[j].Name
	})
	hash := sha256.New()
	io.WriteString(hash, "marketing-os-skill-inventory-v1")
	hash.Write([]byte{0})
	for _, skill := range canonical {
		io.WriteString(hash, skill.Name)
		hash.Write([]byte{0})
		io.WriteString(hash, skill.Version)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (s *VerifiedSnapshot) Lock() Lock {
	return cloneLock(s.lock)
}

func (s *VerifiedSnapshot) Status() Status {
	return cloneStatus(s.status)
}

func (s *VerifiedSnapshot) Metadata() SnapshotMetadata {
	return s.metadata
}

func (s *VerifiedSnapshot) Index(ctx context.Context) ([]Skill, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("verified skill snapshot is required")
	}
	return s.repository.index(ctx)
}

func (s *VerifiedSnapshot) Load(ctx context.Context, name string, requestedReferences []string) (Bundle, error) {
	if s == nil || s.repository == nil {
		return Bundle{}, errors.New("verified skill snapshot is required")
	}
	return s.repository.load(ctx, name, requestedReferences)
}

func (l *Loader) captureRepository(ctx context.Context) (*repositorySnapshot, error) {
	root, err := filepath.Abs(l.RepositoryPath)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect skills repository root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("skills repository root must be a non-symlink directory")
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open skills repository root: %w", err)
	}
	defer rootHandle.Close()
	openedRootInfo, err := rootHandle.Stat(".")
	if err != nil {
		return nil, fmt.Errorf("inspect opened skills repository root: %w", err)
	}
	if !os.SameFile(rootInfo, openedRootInfo) {
		return nil, errors.New("skills repository root changed while it was opened")
	}

	lockRelative := ""
	if lockAbs, absErr := filepath.Abs(l.LockPath); absErr == nil {
		if rel, relErr := filepath.Rel(root, lockAbs); relErr == nil && rel != "." &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			lockRelative = filepath.ToSlash(rel)
		}
	}

	snapshot := &repositorySnapshot{root: root, entries: map[string]snapshotEntry{}}
	var manifestPaths []string
	var bounds manifestResourceBounds
	err = fs.WalkDir(rootHandle.FS(), ".", func(path string, directoryEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := bounds.add(path); err != nil {
			return err
		}
		if path == ".git" || strings.HasPrefix(path, ".git/") {
			if directoryEntry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == "." {
			return snapshot.retainEntry(path, snapshotEntry{kind: snapshotDirectory}, &bounds)
		}
		info, err := rootHandle.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return snapshot.retainEntry(path, snapshotEntry{kind: snapshotDirectory}, &bounds)
		}
		if path == lockRelative {
			return nil
		}
		entry, err := readSnapshotEntry(rootHandle, path, info)
		if err != nil {
			return err
		}
		if err := snapshot.retainEntry(path, entry, &bounds); err != nil {
			return err
		}
		manifestPaths = append(manifestPaths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk skills repository: %w", err)
	}
	sort.Strings(manifestPaths)
	hash := sha256.New()
	for _, path := range manifestPaths {
		entry := snapshot.entries[path]
		io.WriteString(hash, path)
		hash.Write([]byte{0})
		if entry.kind == snapshotSymlink {
			io.WriteString(hash, "symlink")
			hash.Write([]byte{0})
			io.WriteString(hash, filepath.ToSlash(entry.symlinkTarget))
			hash.Write([]byte{0})
		}
		hash.Write(entry.content)
		hash.Write([]byte{0})
	}
	snapshot.manifest = hex.EncodeToString(hash.Sum(nil))
	return snapshot, nil
}

func (s *repositorySnapshot) retainEntry(path string, entry snapshotEntry, bounds *manifestResourceBounds) error {
	if err := bounds.addContent(len(entry.content)); err != nil {
		return err
	}
	s.entries[path] = entry
	return nil
}

func readSnapshotEntry(root *os.Root, path string, before os.FileInfo) (snapshotEntry, error) {
	kind := snapshotRegularFile
	target := ""
	if before.Mode()&os.ModeSymlink != 0 {
		kind = snapshotSymlink
		var err error
		target, err = root.Readlink(path)
		if err != nil {
			return snapshotEntry{}, fmt.Errorf("read repository symlink %s: %w", path, err)
		}
	} else if !before.Mode().IsRegular() {
		return snapshotEntry{}, fmt.Errorf("repository entry %s must be a regular file or contained file symlink", path)
	}
	file, err := root.Open(path)
	if err != nil {
		return snapshotEntry{}, fmt.Errorf("open contained repository entry %s: %w", path, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return snapshotEntry{}, err
	}
	if !opened.Mode().IsRegular() {
		return snapshotEntry{}, fmt.Errorf("repository entry %s must resolve to a regular file", path)
	}
	if opened.Size() > maxManifestFileBytes {
		return snapshotEntry{}, fmt.Errorf("manifest entry %s exceeds %d-byte limit", path, maxManifestFileBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxManifestFileBytes+1))
	if err != nil {
		return snapshotEntry{}, err
	}
	if int64(len(content)) > maxManifestFileBytes {
		return snapshotEntry{}, fmt.Errorf("manifest entry %s exceeds %d-byte limit", path, maxManifestFileBytes)
	}
	after, err := root.Lstat(path)
	if err != nil {
		return snapshotEntry{}, fmt.Errorf("reinspect repository entry %s: %w", path, err)
	}
	if kind == snapshotRegularFile {
		if !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(before, after) {
			return snapshotEntry{}, fmt.Errorf("repository entry %s changed while it was read", path)
		}
	} else {
		afterTarget, readErr := root.Readlink(path)
		if readErr != nil || after.Mode()&os.ModeSymlink == 0 || !os.SameFile(before, after) || afterTarget != target {
			return snapshotEntry{}, fmt.Errorf("repository symlink %s changed while it was read", path)
		}
	}
	return snapshotEntry{kind: kind, content: content, symlinkTarget: target}, nil
}

func (s *repositorySnapshot) index(ctx context.Context) ([]Skill, error) {
	if entry, ok := s.entries["skills"]; !ok || entry.kind != snapshotDirectory {
		return nil, errors.New("inspect skills directory: path must be a non-symlink directory")
	}
	var names []string
	for path, entry := range s.entries {
		if filepath.ToSlash(filepath.Dir(path)) != "skills" {
			continue
		}
		if entry.kind != snapshotDirectory {
			return nil, fmt.Errorf("skills directory entry %s must be a non-symlink directory", filepath.Base(path))
		}
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	result := make([]Skill, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		skill, err := s.readSkill(name)
		if err != nil {
			return nil, err
		}
		result = append(result, skill)
	}
	return result, nil
}

func (s *repositorySnapshot) load(ctx context.Context, name string, requestedReferences []string) (Bundle, error) {
	if err := validateSkillName(name); err != nil {
		return Bundle{}, err
	}
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	skill, err := s.readSkill(name)
	if err != nil {
		return Bundle{}, err
	}
	allowed := make(map[string]struct{}, len(skill.References))
	for _, ref := range skill.References {
		allowed[ref] = struct{}{}
	}
	bundle := Bundle{Skill: skill, References: map[string]string{}}
	total := len(skill.Instructions)
	for _, requested := range requestedReferences {
		clean, err := cleanRelative(requested)
		if err != nil {
			return Bundle{}, fmt.Errorf("invalid reference %q: %w", requested, err)
		}
		if _, ok := allowed[clean]; !ok {
			return Bundle{}, fmt.Errorf("reference %q is not indexed for skill %s", clean, name)
		}
		path := "skills/" + name + "/references/" + clean
		entry, ok := s.entries[path]
		if !ok || entry.kind != snapshotRegularFile {
			return Bundle{}, fmt.Errorf("read reference %s/%s: only regular non-symlink files are allowed", name, clean)
		}
		if len(entry.content) > maxReferenceBytes {
			return Bundle{}, fmt.Errorf("read reference %s/%s: file exceeds %d-byte limit", name, clean, maxReferenceBytes)
		}
		total += len(entry.content)
		if total > maxBundleBytes {
			return Bundle{}, errors.New("skill bundle exceeds maximum context size")
		}
		bundle.References[clean] = string(entry.content)
	}
	return bundle, nil
}

func (s *repositorySnapshot) readSkill(directoryName string) (Skill, error) {
	if err := validateSkillName(directoryName); err != nil {
		return Skill{}, fmt.Errorf("invalid skill directory %q: %w", directoryName, err)
	}
	skillDirectory := "skills/" + directoryName
	if entry, ok := s.entries[skillDirectory]; !ok || entry.kind != snapshotDirectory {
		return Skill{}, fmt.Errorf("inspect skill directory %s: path must be a non-symlink directory", directoryName)
	}
	path := skillDirectory + "/SKILL.md"
	entry, ok := s.entries[path]
	if !ok {
		return Skill{}, fmt.Errorf("read skill %s: file does not exist", directoryName)
	}
	if entry.kind != snapshotRegularFile {
		return Skill{}, fmt.Errorf("read skill %s: only regular non-symlink files are allowed", directoryName)
	}
	if len(entry.content) > maxSkillBytes {
		return Skill{}, fmt.Errorf("read skill %s: file exceeds %d-byte limit", directoryName, maxSkillBytes)
	}
	front, body, err := parseFrontmatter(entry.content)
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
		Version: version, Metadata: front.Metadata, Instructions: body,
		Path: filepath.Join(s.root, filepath.FromSlash(path)),
	}
	skill.References, err = s.indexOptionalFiles(skillDirectory + "/references")
	if err != nil {
		return Skill{}, err
	}
	skill.Scripts, err = s.indexOptionalFiles(skillDirectory + "/scripts")
	if err != nil {
		return Skill{}, err
	}
	skill.Assets, err = s.indexOptionalFiles(skillDirectory + "/assets")
	if err != nil {
		return Skill{}, err
	}
	return skill, nil
}

func (s *repositorySnapshot) indexOptionalFiles(root string) ([]string, error) {
	entry, ok := s.entries[root]
	if !ok {
		return []string{}, nil
	}
	if entry.kind != snapshotDirectory {
		return nil, fmt.Errorf("optional skill path %s is not a non-symlink directory", filepath.Join(s.root, filepath.FromSlash(root)))
	}
	prefix := root + "/"
	var files []string
	for path, item := range s.entries {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		if item.kind == snapshotDirectory {
			continue
		}
		if item.kind != snapshotRegularFile {
			return nil, fmt.Errorf("symlink is not allowed in skill optional directory: %s", filepath.Join(s.root, filepath.FromSlash(path)))
		}
		files = append(files, strings.TrimPrefix(path, prefix))
	}
	sort.Strings(files)
	return files, nil
}

func enforceManifestResourceBounds(paths []string) error {
	var bounds manifestResourceBounds
	for _, path := range paths {
		if err := bounds.add(path); err != nil {
			return err
		}
	}
	return nil
}

type manifestResourceBounds struct {
	entries      int
	pathBytes    int
	contentBytes int64
}

func (b *manifestResourceBounds) add(path string) error {
	if b.entries >= maxManifestEntries {
		return fmt.Errorf("skills repository exceeds %d-entry manifest limit", maxManifestEntries)
	}
	if !fs.ValidPath(path) || filepath.ToSlash(path) != path {
		return fmt.Errorf("manifest path %q is not normalized", path)
	}
	b.entries++
	b.pathBytes += len(path)
	if b.pathBytes > maxManifestPathBytes {
		return fmt.Errorf("skills repository exceeds %d-byte normalized path limit", maxManifestPathBytes)
	}
	return nil
}

func (b *manifestResourceBounds) addContent(size int) error {
	if int64(size) > int64(maxManifestBytes)-b.contentBytes {
		return fmt.Errorf("skills repository exceeds %d-byte manifest limit", maxManifestBytes)
	}
	b.contentBytes += int64(size)
	return nil
}

func cloneLock(lock Lock) Lock {
	lock.SelectedSkills = append([]SelectedSkill(nil), lock.SelectedSkills...)
	return lock
}

func cloneStatus(status Status) Status {
	status.Lock = cloneLock(status.Lock)
	status.ActualInventory = append([]SelectedSkill(nil), status.ActualInventory...)
	return status
}
