package skills

import (
	"context"
	"os"
	"path/filepath"
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
	mustWrite(t, filepath.Join(repo, "skills", "launch", "SKILL.md"), `---
name: launch
description: Launch guidance.
metadata: {version: 2.0.1}
---
launch
`)
	lockPath := filepath.Join(repo, "skills.lock.yaml")
	loader := NewLoader(repo, lockPath)
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(lockPath, Lock{Repository: "https://example.test/skills.git", Ref: "v1", Commit: "fixture", RepositoryVersion: "1.0.0", ManifestSHA256: manifest}); err != nil {
		t.Fatal(err)
	}
	status, err := loader.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.ManifestMatches || status.Lock.Commit != "fixture" {
		t.Fatalf("status = %+v", status)
	}
	mustWrite(t, filepath.Join(repo, "skills", "launch", "SKILL.md"), `---
name: launch
description: Changed guidance.
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

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
