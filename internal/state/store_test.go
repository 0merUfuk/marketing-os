package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestMigrationsAndProductPersistenceAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "marketing-os.db")

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	product := domain.Product{
		ID: "alpha", Name: "Alpha", Repository: "acme/alpha",
		Website: "https://alpha.example", ProductType: "saas",
		PrimaryConversionAction: "start_trial", DefaultLanguage: "en",
	}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatalf("AddProduct() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer store.Close()
	got, err := store.GetProduct(ctx, "alpha")
	if err != nil {
		t.Fatalf("GetProduct() error = %v", err)
	}
	if got.Repository != product.Repository || got.Name != product.Name {
		t.Fatalf("persisted product = %+v", got)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repeated migration must be idempotent: %v", err)
	}
}

func TestApprovedContextIsProductScopedVersionedAndIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"alpha", "beta"} {
		if err := store.AddProduct(ctx, domain.Product{ID: id, Name: id, ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"}); err != nil {
			t.Fatal(err)
		}
	}

	v1, err := store.CreateContextDraft(ctx, "alpha", "# Alpha context\n\nUnsupported: pricing", []string{"ev-alpha"}, []string{"pricing unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApprovedContext(ctx, "alpha"); !errors.Is(err, ErrUnapprovedContext) {
		t.Fatalf("ApprovedContext before approval error = %v", err)
	}
	if _, err := store.ApproveContext(ctx, "beta", v1.Version, "reviewer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-product approval error = %v", err)
	}
	approved, err := store.ApproveContext(ctx, "alpha", v1.Version, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != domain.ContextApproved || approved.ApprovedBy != "reviewer" {
		t.Fatalf("approved context = %+v", approved)
	}
	again, err := store.ApproveContext(ctx, "alpha", v1.Version, "reviewer")
	if err != nil || again.ID != approved.ID {
		t.Fatalf("repeated approval should be idempotent: %+v, %v", again, err)
	}

	v2, err := store.CreateContextDraft(ctx, "alpha", "# Alpha v2", nil, []string{"proof points unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != 2 {
		t.Fatalf("version = %d", v2.Version)
	}
	if _, err := store.ApproveContext(ctx, "alpha", v2.Version, "reviewer-2"); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListContextVersions(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Status != domain.ContextApproved || versions[1].Status != domain.ContextSuperseded {
		t.Fatalf("versions = %+v", versions)
	}
}
