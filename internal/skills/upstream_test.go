package skills

import (
	"context"
	"path/filepath"
	"testing"
)

func TestVendoredDistributionContainsExactlyRequiredWorkflowSkills(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", "..", "third_party", "marketingskills"))
	loader := NewLoader(repo, filepath.Join("..", "..", "skills.lock.yaml"))
	status, err := loader.Status(context.Background())
	if err != nil {
		t.Fatalf("status vendored skills: %v", err)
	}
	if !status.PinValid || !status.InventoryMatches || !status.VendoredManifestMatches {
		t.Fatalf("vendored status = %+v", status)
	}
	indexed, err := loader.Index(context.Background())
	if err != nil {
		t.Fatalf("index vendored skills: %v", err)
	}
	if len(indexed) != 5 {
		t.Fatalf("vendored skill count = %d, want 5", len(indexed))
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
