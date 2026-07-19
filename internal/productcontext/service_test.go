package productcontext

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/llm"
	"github.com/omerufuk/marketing-os/internal/products"
	"github.com/omerufuk/marketing-os/internal/skills"
	"github.com/omerufuk/marketing-os/internal/state"
)

type evidenceAwareModel struct{ invalidEvidence bool }

func (m *evidenceAwareModel) Generate(_ context.Context, request llm.GenerationRequest) (llm.GenerationResult, error) {
	match := regexp.MustCompile(`"id":\s*"([^"]+)"`).FindStringSubmatch(request.Prompt)
	evidenceID := "missing"
	if len(match) == 2 {
		evidenceID = match[1]
	}
	if m.invalidEvidence {
		evidenceID = "cross-product-or-invented"
	}
	result := DraftResult{Markdown: completeContextMarkdown(), EvidenceIDs: []string{evidenceID}, UnsupportedOrUncertain: []string{"Pricing and testimonials are unknown."}}
	data, _ := json.Marshal(result)
	return llm.GenerationResult{Content: string(data), Provider: "mock", Model: "mock-context"}, nil
}

func TestDraftAndApproveCanonicalProductContext(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	localRepo := filepath.Join(root, "source")
	if err := os.MkdirAll(localRepo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRepo, "README.md"), []byte("# Widget\nExport operational reports as CSV."), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(ctx, filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	product := domain.Product{ID: "widget", Name: "Widget", Repository: "acme/widget", LocalRepository: localRepo, ProductType: "saas", PrimaryConversionAction: "start_trial", DefaultLanguage: "en"}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	workspace := products.NewWorkspace(filepath.Join(root, "products"))
	if err := workspace.Initialize(product); err != nil {
		t.Fatal(err)
	}
	loader := contextSkillFixture(t, root)
	service := Service{Store: store, Workspace: workspace, Skills: loader, Model: &evidenceAwareModel{}, MaxOutputTokens: 5000, MaxRepairAttempts: 1}
	draft, err := service.Draft(ctx, "widget")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != domain.ContextDraft || draft.Version != 1 || len(draft.EvidenceIDs) == 0 || len(draft.Uncertainty) == 0 {
		t.Fatalf("draft=%+v", draft)
	}
	if _, err := store.ApprovedContext(ctx, "widget"); err == nil {
		t.Fatal("draft must not be treated as approved")
	}
	approved, err := service.Approve(ctx, "widget", 1, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != domain.ContextApproved {
		t.Fatalf("approved=%+v", approved)
	}
	canonical := filepath.Join(root, "products", "widget", ".agents", "product-marketing.md")
	content, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "## Target Audience") {
		t.Fatalf("canonical context missing sections: %s", content)
	}
}

func TestDraftRejectsUnknownEvidenceReference(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := state.Open(ctx, filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	product := domain.Product{ID: "widget", Name: "Widget", Repository: "acme/widget", ProductType: "saas", PrimaryConversionAction: "start_trial", DefaultLanguage: "en"}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	workspace := products.NewWorkspace(filepath.Join(root, "products"))
	if err := workspace.Initialize(product); err != nil {
		t.Fatal(err)
	}
	service := Service{Store: store, Workspace: workspace, Skills: contextSkillFixture(t, root), Model: &evidenceAwareModel{invalidEvidence: true}, MaxOutputTokens: 5000}
	if _, err := service.Draft(ctx, "widget"); err == nil {
		t.Fatal("expected invented evidence reference to be rejected")
	}
}

func contextSkillFixture(t *testing.T, root string) *skills.Loader {
	t.Helper()
	repo := filepath.Join(root, "skills")
	path := filepath.Join(repo, "skills", "product-marketing", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: product-marketing\ndescription: Build evidence-based product context.\nmetadata: {version: 1.0.0}\n---\nMark unknown facts explicitly."), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := skills.NewLoader(repo, filepath.Join(root, "skills.lock.yaml"))
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.WriteLock(loader.LockPath, skills.Lock{Repository: "fixture", Ref: "fixture", Commit: "fixture-context", ManifestSHA256: manifest}); err != nil {
		t.Fatal(err)
	}
	return loader
}

func completeContextMarkdown() string {
	headings := RequiredHeadings()
	var b strings.Builder
	b.WriteString("# Product Marketing Context\n\n")
	for _, heading := range headings {
		b.WriteString("## " + heading + "\n\nUnknown — requires human review. [evidence]\n\n")
	}
	return b.String()
}
