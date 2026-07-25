package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderParsesFrontmatterAndLoadsOnlyRequestedReferences(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	skillDir := filepath.Join(repo, "skills", "social")
	mustWrite(t, filepath.Join(skillDir, "SKILL.md"), `---
name: social
description: Social guidance for release posts.
metadata:
  version: 2.2.0
---
# Social
Do not invent facts.
`)
	mustWrite(t, filepath.Join(skillDir, "references", "platforms.md"), "platform strategies")
	mustWrite(t, filepath.Join(skillDir, "references", "limits.md"), "platform limits")
	marker := filepath.Join(repo, "executed")
	mustWrite(t, filepath.Join(skillDir, "scripts", "unsafe.sh"), "touch "+marker)
	mustWrite(t, filepath.Join(skillDir, "assets", "template.md"), "asset")

	loader := NewLoader(repo, filepath.Join(repo, "skills.lock.yaml"))
	indexed, err := loader.Index(context.Background())
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if len(indexed) != 1 || indexed[0].Name != "social" || indexed[0].Version != "2.2.0" {
		t.Fatalf("indexed = %+v", indexed)
	}
	if len(indexed[0].References) != 2 || len(indexed[0].Scripts) != 1 || len(indexed[0].Assets) != 1 {
		t.Fatalf("optional files not indexed: %+v", indexed[0])
	}
	bundle, err := loader.Load(context.Background(), "social", []string{"limits.md"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(bundle.References) != 1 || bundle.References["limits.md"] != "platform limits" {
		t.Fatalf("loaded references = %+v", bundle.References)
	}
	if _, ok := bundle.References["platforms.md"]; ok {
		t.Fatal("unrequested reference was loaded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("skill script was executed: %v", err)
	}
	if _, err := loader.Load(context.Background(), "social", []string{"../../unsafe"}); err == nil {
		t.Fatal("Load accepted reference traversal")
	}
	if _, err := loader.Load(context.Background(), "social", []string{"nested/../limits.md"}); err == nil {
		t.Fatal("Load accepted an interior traversal segment")
	}
}

func TestLoaderRejectsInvalidOrMismatchedFrontmatter(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "skills", "social", "SKILL.md"), `---
name: Email
metadata: invalid-scalar
---
body
`)
	loader := NewLoader(repo, filepath.Join(repo, "lock.yaml"))
	if _, err := loader.Index(context.Background()); err == nil {
		t.Fatal("Index accepted invalid frontmatter")
	}
}

func TestManifestAllowsOnlyRepositoryContainedSymlinks(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "AGENTS.md"), "safe instructions\n")
	if err := os.Symlink("AGENTS.md", filepath.Join(repo, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(repo, filepath.Join(t.TempDir(), "skills.lock.yaml"))
	first, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatalf("safe internal symlink rejected: %v", err)
	}
	mustWrite(t, filepath.Join(repo, "AGENTS.md"), "changed instructions\n")
	second, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("manifest did not change with symlink target content")
	}

	outside := filepath.Join(t.TempDir(), "outside.md")
	mustWrite(t, outside, "outside\n")
	if err := os.Symlink(outside, filepath.Join(repo, "ESCAPE.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.ComputeManifest(context.Background()); err == nil {
		t.Fatal("manifest accepted symlink escaping repository")
	}
}

func TestStatusVerifiesManifestAgainstLock(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeCanonicalSkillFixture(t, repo)
	lockPath := filepath.Join(repo, "skills.lock.yaml")
	loader := NewLoader(repo, lockPath)
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(lockPath, validTestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	status, err := loader.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.ManifestMatches || !status.InventoryMatches || status.Lock.Commit != testCommit {
		t.Fatalf("status = %+v", status)
	}
	mustWrite(t, filepath.Join(repo, "skills", "launch", "SKILL.md"), `---
name: launch
description: Changed guidance.
metadata: {version: 1.0.0}
---
changed
`)
	status, err = loader.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.ManifestMatches {
		t.Fatal("status failed to detect changed skill content")
	}
}

func TestVerifiedSnapshotConsumesOnlyTheBytesThatWereHashed(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	writeCanonicalSkillFixture(t, repo)
	lockPath := filepath.Join(repo, "skills.lock.yaml")
	loader := NewLoader(repo, lockPath)
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(lockPath, validTestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loader.RequirePinned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before, err := snapshot.Load(context.Background(), "launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, "skills", "launch", "SKILL.md"), `---
name: launch
description: Mutated guidance.
metadata: {version: 1.0.0}
---
MUTATED AFTER VERIFICATION
`)
	after, err := snapshot.Load(context.Background(), "launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if after.Skill.Instructions != before.Skill.Instructions || strings.Contains(after.Skill.Instructions, "MUTATED") {
		t.Fatalf("verified snapshot changed after filesystem mutation: %q", after.Skill.Instructions)
	}
	if _, err := loader.RequirePinned(context.Background()); !errors.Is(err, ErrInvalidPin) {
		t.Fatalf("new snapshot after mutation error = %v, want ErrInvalidPin", err)
	}
}

func TestManifestResourceBoundaries(t *testing.T) {
	t.Parallel()
	entriesAtLimit := make([]string, maxManifestEntries)
	for i := range entriesAtLimit {
		entriesAtLimit[i] = "a"
	}
	if err := enforceManifestResourceBounds(entriesAtLimit); err != nil {
		t.Fatalf("entry boundary rejected: %v", err)
	}
	if err := enforceManifestResourceBounds(append(entriesAtLimit, "b")); err == nil {
		t.Fatal("entry limit+1 accepted")
	}
	if err := enforceManifestResourceBounds([]string{strings.Repeat("p", maxManifestPathBytes)}); err != nil {
		t.Fatalf("path-byte boundary rejected: %v", err)
	}
	if err := enforceManifestResourceBounds([]string{strings.Repeat("p", maxManifestPathBytes), "x"}); err == nil {
		t.Fatal("path-byte limit+1 accepted")
	}
}

func TestManifestDirectoryFloodCountsTowardEntryLimit(t *testing.T) {
	repo := t.TempDir()
	for i := 0; i < maxManifestEntries-1; i++ {
		if err := os.Mkdir(filepath.Join(repo, fmt.Sprintf("d%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	loader := NewLoader(repo, filepath.Join(t.TempDir(), "skills.lock.yaml"))
	if _, err := loader.ComputeManifest(context.Background()); err != nil {
		t.Fatalf("directory entry boundary rejected: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repo, "overflow"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.ComputeManifest(context.Background()); err == nil {
		t.Fatal("directory entry limit+1 accepted")
	}
}

func TestManifestAggregateBytesChargedBeforeSnapshotRetention(t *testing.T) {
	t.Parallel()
	snapshot := &repositorySnapshot{entries: map[string]snapshotEntry{}}
	bounds := manifestResourceBounds{contentBytes: maxManifestBytes - 1}
	entry := snapshotEntry{kind: snapshotRegularFile, content: []byte("x")}
	if err := snapshot.retainEntry("boundary", entry, &bounds); err != nil {
		t.Fatalf("aggregate byte boundary rejected: %v", err)
	}
	if err := snapshot.retainEntry("overflow", entry, &bounds); err == nil {
		t.Fatal("aggregate byte limit+1 accepted")
	}
	if _, retained := snapshot.entries["overflow"]; retained {
		t.Fatal("over-limit entry retained in snapshot map")
	}
	if bounds.contentBytes != maxManifestBytes {
		t.Fatalf("content bytes = %d, want %d", bounds.contentBytes, maxManifestBytes)
	}
}

func TestReadLockRejectsNonCanonicalYAML(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeCanonicalSkillFixture(t, root)
	loader := NewLoader(root, filepath.Join(root, "lock.yaml"))
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(loader.LockPath, validTestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(loader.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, suffix := range map[string]string{
		"unknown field":     "unknown_field: true\n",
		"duplicate field":   "distribution: vendored\n",
		"multiple document": "---\ndistribution: vendored\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lock.yaml")
			if err := os.WriteFile(path, append(append([]byte{}, valid...), []byte(suffix)...), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadLock(path); err == nil {
				t.Fatalf("ReadLock accepted %s", name)
			}
		})
	}
	invalid := validTestLock(manifest)
	invalid.SelectedSkills[0], invalid.SelectedSkills[1] = invalid.SelectedSkills[1], invalid.SelectedSkills[0]
	if err := WriteLock(filepath.Join(root, "invalid.yaml"), invalid); err == nil {
		t.Fatal("WriteLock accepted an out-of-order inventory")
	}
}

func TestLoaderRejectsSymlinkedIntermediateDirectoriesAndOversizedSkill(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "SKILL.md"), "---\nname: social\ndescription: unsafe\n---\nbody\n")
	if err := os.MkdirAll(filepath.Join(repo, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "skills", "social")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLoader(repo, filepath.Join(repo, "lock.yaml")).Index(context.Background()); err == nil {
		t.Fatal("Index accepted a symlinked skill directory")
	}

	repositoryWithSymlinkedRoot := t.TempDir()
	outsideSkills := t.TempDir()
	mustWrite(t, filepath.Join(outsideSkills, "social", "SKILL.md"), "---\nname: social\ndescription: unsafe\n---\nbody\n")
	if err := os.Symlink(outsideSkills, filepath.Join(repositoryWithSymlinkedRoot, "skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLoader(repositoryWithSymlinkedRoot, filepath.Join(repositoryWithSymlinkedRoot, "lock.yaml")).Load(context.Background(), "social", nil); err == nil {
		t.Fatal("Load accepted a symlinked intermediate skills directory")
	}

	oversized := t.TempDir()
	mustWrite(t, filepath.Join(oversized, "skills", "social", "SKILL.md"),
		"---\nname: social\ndescription: large\n---\n"+strings.Repeat("x", maxSkillBytes))
	if _, err := NewLoader(oversized, filepath.Join(oversized, "lock.yaml")).Index(context.Background()); err == nil {
		t.Fatal("Index accepted an oversized SKILL.md")
	}
}

const testCommit = "1111111111111111111111111111111111111111"

func validTestLock(manifest string) Lock {
	return Lock{
		Distribution: VendoredDistribution, Repository: "https://example.test/skills.git",
		Ref: testCommit, Commit: testCommit, RepositoryVersion: "1.0.0",
		SelectedSkills: []SelectedSkill{
			{Name: "copywriting", Version: "1.0.0"},
			{Name: "emails", Version: "1.0.0"},
			{Name: "launch", Version: "1.0.0"},
			{Name: "product-marketing", Version: "1.0.0"},
			{Name: "social", Version: "1.0.0"},
		},
		UpstreamManifestSHA256: strings.Repeat("a", 64),
		VendoredManifestSHA256: manifest,
	}
}

func writeCanonicalSkillFixture(t *testing.T, repo string) {
	t.Helper()
	for _, name := range []string{"copywriting", "emails", "launch", "product-marketing", "social"} {
		mustWrite(t, filepath.Join(repo, "skills", name, "SKILL.md"),
			"---\nname: "+name+"\ndescription: Safe "+name+" guidance.\nmetadata: {version: 1.0.0}\n---\nbody\n")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
