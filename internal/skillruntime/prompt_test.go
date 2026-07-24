package skillruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/github"
	"github.com/omerufuk/marketing-os/internal/skills"
)

func TestBuildReleasePromptLoadsOnlyAllowlistedSkillReferences(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	fixtures := map[string]string{"launch": "launch rules", "copywriting": "copy rules", "social": "social rules", "emails": "email rules", "product-marketing": "product rules"}
	for name, body := range fixtures {
		path := filepath.Join(repo, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: Guidance for " + name + ".\nmetadata: {version: 1.0.0}\n---\n" + body
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustPromptFile(t, filepath.Join(repo, "skills", "social", "references", "platform-limits.md"), "X LIMIT SENTINEL")
	mustPromptFile(t, filepath.Join(repo, "skills", "social", "references", "platforms.md"), "UNREQUESTED SOCIAL SENTINEL")
	mustPromptFile(t, filepath.Join(repo, "skills", "emails", "references", "copy-guidelines.md"), "EMAIL COPY SENTINEL")
	mustPromptFile(t, filepath.Join(repo, "skills", "emails", "references", "sequence-templates.md"), "UNREQUESTED EMAIL SENTINEL")

	loader := skills.NewLoader(repo, filepath.Join(repo, "lock.yaml"))
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const commit = "1111111111111111111111111111111111111111"
	lock := skills.Lock{
		Distribution: skills.VendoredDistribution, Repository: "https://example.test/skills.git",
		Ref: commit, Commit: commit, RepositoryVersion: "1.0.0",
		SelectedSkills: []skills.SelectedSkill{
			{Name: "copywriting", Version: "1.0.0"}, {Name: "emails", Version: "1.0.0"},
			{Name: "launch", Version: "1.0.0"}, {Name: "product-marketing", Version: "1.0.0"},
			{Name: "social", Version: "1.0.0"},
		},
		UpstreamManifestSHA256: strings.Repeat("a", 64), VendoredManifestSHA256: manifest,
		UpdatedAt: time.Now().UTC(),
	}
	if err := skills.WriteLock(loader.LockPath, lock); err != nil {
		t.Fatal(err)
	}
	snapshot, err := loader.RequirePinned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	set, err := LoadReleaseSkills(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := BuildReleasePrompt(ReleasePromptInput{
		Product:         domain.Product{ID: "alpha", Name: "Alpha", ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"},
		ApprovedContext: "# Approved context\nNo invented claims.", ContextVersion: 1,
		Release:          github.Release{ID: 42, Tag: "v1.4", Name: "Alpha v1.4", Body: "CSV export", PublishedAt: time.Now()},
		Evidence:         []domain.Evidence{{ID: "release-42", ProductID: "alpha", Content: "CSV export", SourceType: "github_release"}},
		RepositoryCommit: "commit", Skills: set,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"launch rules", "copy rules", "social rules", "email rules", "X LIMIT SENTINEL", "EMAIL COPY SENTINEL", "release-42"} {
		if !strings.Contains(prompt.Prompt, expected) {
			t.Errorf("prompt missing %q", expected)
		}
	}
	for _, forbidden := range []string{"UNREQUESTED SOCIAL SENTINEL", "UNREQUESTED EMAIL SENTINEL"} {
		if strings.Contains(prompt.Prompt, forbidden) {
			t.Errorf("prompt loaded forbidden reference %q", forbidden)
		}
	}
	if len(prompt.SkillVersions) != 4 || prompt.PrimarySkill != "launch" {
		t.Fatalf("prompt metadata = %+v", prompt)
	}
}

func mustPromptFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
