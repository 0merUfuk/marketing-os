package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedUpstreamContainsRequiredWorkflowSkills(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", ".skills", "marketingskills"))
	if _, err := os.Stat(filepath.Join(repo, "skills")); os.IsNotExist(err) {
		t.Skip("marketingskills submodule is not initialized")
	}
	loader := NewLoader(repo, filepath.Join("..", "..", "skills.lock.yaml"))
	indexed, err := loader.Index(context.Background())
	if err != nil {
		t.Fatalf("index pinned upstream: %v", err)
	}
	versions := map[string]string{}
	for _, skill := range indexed {
		versions[skill.Name] = skill.Version
	}
	want := map[string]string{
		"product-marketing": "2.1.0",
		"launch":            "2.0.1",
		"copywriting":       "2.0.1",
		"social":            "2.2.0",
		"emails":            "2.0.0",
	}
	for name, version := range want {
		if versions[name] != version {
			t.Errorf("skill %s version = %q, want %q", name, versions[name], version)
		}
	}
}
