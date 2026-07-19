package skills

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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
	Path         string         `json:"path"`
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
	root := filepath.Join(l.RepositoryPath, "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read skills directory: %w", err)
	}
	result := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		skill, err := l.readSkill(entry.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (l *Loader) Load(ctx context.Context, name string, requestedReferences []string) (Bundle, error) {
	if err := validateSkillName(name); err != nil {
		return Bundle{}, err
	}
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	skill, err := l.readSkill(name)
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
		path := filepath.Join(l.RepositoryPath, "skills", name, "references", filepath.FromSlash(clean))
		content, err := readBoundedFile(path, maxReferenceBytes)
		if err != nil {
			return Bundle{}, fmt.Errorf("read reference %s/%s: %w", name, clean, err)
		}
		total += len(content)
		if total > maxBundleBytes {
			return Bundle{}, errors.New("skill bundle exceeds maximum context size")
		}
		bundle.References[clean] = string(content)
	}
	return bundle, nil
}

func (l *Loader) readSkill(directoryName string) (Skill, error) {
	if err := validateSkillName(directoryName); err != nil {
		return Skill{}, fmt.Errorf("invalid skill directory %q: %w", directoryName, err)
	}
	path := filepath.Join(l.RepositoryPath, "skills", directoryName, "SKILL.md")
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
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("optional skill path %s is not a directory", root)
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
	return os.ReadFile(path)
}
