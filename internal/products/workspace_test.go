package products

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestWorkspaceCreatesIsolatedProductLayout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workspace := NewWorkspace(root)
	alpha := domain.Product{ID: "alpha", Name: "Alpha", ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"}
	beta := domain.Product{ID: "beta", Name: "Beta", ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"}
	for _, p := range []domain.Product{alpha, beta} {
		if err := workspace.Initialize(p); err != nil {
			t.Fatalf("Initialize(%s) error = %v", p.ID, err)
		}
	}
	if err := workspace.WriteContextDraft("alpha", 1, "alpha-only"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteApprovedContext("beta", 1, "beta-only", "reviewer"); err != nil {
		t.Fatal(err)
	}
	alphaDraft, err := os.ReadFile(filepath.Join(root, "alpha", ".agents", "product-marketing.v1.draft.md"))
	if err != nil || string(alphaDraft) != "alpha-only" {
		t.Fatalf("alpha draft = %q, %v", alphaDraft, err)
	}
	betaContext, err := os.ReadFile(filepath.Join(root, "beta", ".agents", "product-marketing.md"))
	if err != nil || string(betaContext) != "beta-only" {
		t.Fatalf("beta context = %q, %v", betaContext, err)
	}
	if _, err := os.Stat(filepath.Join(root, "alpha", ".agents", "product-marketing.md")); !os.IsNotExist(err) {
		t.Fatalf("alpha unexpectedly received beta context: %v", err)
	}
}

func TestWorkspaceRejectsTraversal(t *testing.T) {
	t.Parallel()
	workspace := NewWorkspace(t.TempDir())
	bad := domain.Product{ID: "../escape", Name: "Bad", ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"}
	if err := workspace.Initialize(bad); err == nil {
		t.Fatal("Initialize accepted path traversal product id")
	}
	if err := workspace.WriteContextDraft("../escape", 1, "bad"); err == nil {
		t.Fatal("WriteContextDraft accepted path traversal")
	}
}
