package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestEvidenceIsImmutableDeduplicatedAndProductScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"alpha", "beta"} {
		if err := store.AddProduct(ctx, domain.Product{ID: id, Name: id, ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"}); err != nil {
			t.Fatal(err)
		}
	}
	input := domain.EvidenceInput{
		ID: "release-42", ProductID: "alpha", SourceType: "github_release",
		SourceURL: "https://github.test/acme/alpha/releases/42", ExternalID: "42",
		Content: "Users can export CSV reports.",
	}
	first, err := store.SaveEvidence(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveEvidence(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ContentHash == "" {
		t.Fatalf("dedupe failed: first=%+v second=%+v", first, second)
	}
	changed := input
	changed.Content = "Different content"
	if _, err := store.SaveEvidence(ctx, changed); !errors.Is(err, ErrImmutableConflict) {
		t.Fatalf("changed immutable evidence error = %v", err)
	}
	if _, err := store.EvidenceByIDs(ctx, "beta", []string{first.ID}); !errors.Is(err, ErrEvidenceScope) {
		t.Fatalf("cross-product evidence error = %v", err)
	}
	got, err := store.EvidenceByIDs(ctx, "alpha", []string{first.ID})
	if err != nil || len(got) != 1 || got[0].Content != input.Content {
		t.Fatalf("EvidenceByIDs = %+v, %v", got, err)
	}
}
