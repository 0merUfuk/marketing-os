package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	VendoredDistribution = "vendored"
	maxLockBytes         = 64 * 1024
	maxManifestFileBytes = 8 * 1024 * 1024
	maxManifestBytes     = 64 * 1024 * 1024
)

var (
	immutableCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern          = regexp.MustCompile(`^[0-9a-f]{64}$`)
	semverPattern          = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

	ErrVendoredUpdateDisabled = errors.New("runtime skills update is disabled for vendored distribution; follow the reviewed offline maintainer procedure in docs/skills.md")
	ErrInvalidPin             = errors.New("vendored marketing skills do not match the lock")
)

type SelectedSkill struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
}

type Lock struct {
	Distribution           string          `yaml:"distribution" json:"distribution"`
	Repository             string          `yaml:"repository" json:"repository"`
	Ref                    string          `yaml:"ref" json:"ref"`
	Commit                 string          `yaml:"commit" json:"commit"`
	RepositoryVersion      string          `yaml:"repository_version" json:"repository_version"`
	SelectedSkills         []SelectedSkill `yaml:"selected_skills" json:"selected_skills"`
	UpstreamManifestSHA256 string          `yaml:"upstream_manifest_sha256" json:"upstream_manifest_sha256"`
	VendoredManifestSHA256 string          `yaml:"vendored_manifest_sha256" json:"vendored_manifest_sha256"`
	UpdatedAt              time.Time       `yaml:"updated_at" json:"updated_at"`
}

type Status struct {
	Lock                    Lock            `json:"lock"`
	Distribution            string          `json:"distribution"`
	DistributionValid       bool            `json:"distribution_valid"`
	ActualInventory         []SelectedSkill `json:"actual_inventory"`
	InventoryMatches        bool            `json:"inventory_matches"`
	ActualManifest          string          `json:"actual_manifest"`
	ManifestMatches         bool            `json:"manifest_matches"`
	VendoredManifestMatches bool            `json:"vendored_manifest_matches"`
	PinValid                bool            `json:"pin_valid"`
}

func ReadLock(path string) (Lock, error) {
	data, err := readBoundedFile(path, maxLockBytes)
	if err != nil {
		return Lock{}, fmt.Errorf("open skills lock: %w", err)
	}
	var lock Lock
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&lock); err != nil {
		return Lock{}, fmt.Errorf("decode skills lock: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Lock{}, errors.New("decode skills lock: multiple YAML documents are not allowed")
		}
		return Lock{}, fmt.Errorf("decode skills lock trailing document: %w", err)
	}
	if err := validateLock(lock); err != nil {
		return Lock{}, err
	}
	return lock, nil
}

func WriteLock(path string, lock Lock) error {
	if lock.UpdatedAt.IsZero() {
		lock.UpdatedAt = time.Now().UTC()
	}
	if err := validateLock(lock); err != nil {
		return err
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
	_, status, err := l.inspect(ctx)
	return status, err
}

func (l *Loader) inspect(ctx context.Context) (*repositorySnapshot, Status, error) {
	lock, err := ReadLock(l.LockPath)
	if err != nil {
		return nil, Status{}, err
	}
	snapshot, err := l.captureRepository(ctx)
	if err != nil {
		return nil, Status{}, err
	}
	indexed, err := snapshot.index(ctx)
	if err != nil {
		return nil, Status{}, fmt.Errorf("index vendored skills: %w", err)
	}
	actualInventory := make([]SelectedSkill, 0, len(indexed))
	for _, skill := range indexed {
		actualInventory = append(actualInventory, SelectedSkill{Name: skill.Name, Version: skill.Version})
	}
	manifestMatches := snapshot.manifest == lock.VendoredManifestSHA256
	status := Status{
		Lock: lock, Distribution: lock.Distribution,
		DistributionValid:       lock.Distribution == VendoredDistribution,
		ActualInventory:         actualInventory,
		InventoryMatches:        selectedSkillsEqual(actualInventory, lock.SelectedSkills),
		ActualManifest:          snapshot.manifest,
		ManifestMatches:         manifestMatches,
		VendoredManifestMatches: manifestMatches,
	}
	status.PinValid = status.DistributionValid && status.InventoryMatches && status.VendoredManifestMatches
	return snapshot, status, nil
}

func (l *Loader) RequirePinned(ctx context.Context) (*VerifiedSnapshot, error) {
	repository, status, err := l.inspect(ctx)
	if err != nil {
		return nil, err
	}
	if !status.PinValid {
		return nil, fmt.Errorf("%w (distribution_valid=%t inventory_match=%t vendored_manifest_match=%t)",
			ErrInvalidPin, status.DistributionValid, status.InventoryMatches, status.VendoredManifestMatches)
	}
	return &VerifiedSnapshot{
		repository: repository,
		lock:       cloneLock(status.Lock),
		status:     cloneStatus(status),
		metadata:   NewSnapshotMetadata(status.Lock, status.ActualManifest),
	}, nil
}

func (l *Loader) ComputeManifest(ctx context.Context) (string, error) {
	snapshot, err := l.captureRepository(ctx)
	if err != nil {
		return "", err
	}
	return snapshot.manifest, nil
}

func pathContainedBy(root, candidate string) (bool, error) {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func (l *Loader) Update(context.Context, string, string) (Lock, error) {
	return Lock{}, ErrVendoredUpdateDisabled
}

func validateLock(lock Lock) error {
	if lock.Distribution != VendoredDistribution {
		return fmt.Errorf("skills lock distribution must be %q", VendoredDistribution)
	}
	repository, err := url.Parse(lock.Repository)
	if err != nil || repository.Scheme != "https" || repository.Host == "" || repository.User != nil || repository.RawQuery != "" || repository.Fragment != "" {
		return errors.New("skills lock repository must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if !immutableCommitPattern.MatchString(lock.Ref) || !immutableCommitPattern.MatchString(lock.Commit) || lock.Ref != lock.Commit {
		return errors.New("skills lock ref and commit must be the same full lowercase 40-character commit SHA")
	}
	if !semverPattern.MatchString(lock.RepositoryVersion) {
		return errors.New("skills lock repository_version must be a semantic version")
	}
	if err := validateSelectedSkills(lock.SelectedSkills); err != nil {
		return err
	}
	if !sha256Pattern.MatchString(lock.UpstreamManifestSHA256) {
		return errors.New("skills lock upstream_manifest_sha256 must be a lowercase 64-character SHA-256")
	}
	if !sha256Pattern.MatchString(lock.VendoredManifestSHA256) {
		return errors.New("skills lock vendored_manifest_sha256 must be a lowercase 64-character SHA-256")
	}
	if lock.UpdatedAt.IsZero() {
		return errors.New("skills lock updated_at is required")
	}
	return nil
}

func validateSelectedSkills(selected []SelectedSkill) error {
	expectedNames := []string{"copywriting", "emails", "launch", "product-marketing", "social"}
	if len(selected) != len(expectedNames) {
		return fmt.Errorf("skills lock selected_skills must contain exactly %d canonical skills", len(expectedNames))
	}
	for i, expectedName := range expectedNames {
		if selected[i].Name != expectedName {
			return fmt.Errorf("skills lock selected_skills must be sorted and contain %q at index %d", expectedName, i)
		}
		if !semverPattern.MatchString(selected[i].Version) {
			return fmt.Errorf("skills lock selected skill %q must have a semantic version", selected[i].Name)
		}
	}
	return nil
}

func selectedSkillsEqual(actual, expected []SelectedSkill) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range expected {
		if actual[i] != expected[i] {
			return false
		}
	}
	return true
}
