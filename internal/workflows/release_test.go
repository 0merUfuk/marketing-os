package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	gh "github.com/omerufuk/marketing-os/internal/github"
	"github.com/omerufuk/marketing-os/internal/llm"
	"github.com/omerufuk/marketing-os/internal/products"
	"github.com/omerufuk/marketing-os/internal/skills"
	"github.com/omerufuk/marketing-os/internal/state"
)

type countingModel struct {
	content        string
	calls          atomic.Int32
	err            error
	waitForContext bool
	estimatedCost  float64
}

func (m *countingModel) Generate(ctx context.Context, _ llm.GenerationRequest) (llm.GenerationResult, error) {
	m.calls.Add(1)
	if m.waitForContext {
		<-ctx.Done()
		return llm.GenerationResult{}, ctx.Err()
	}
	if m.err != nil {
		return llm.GenerationResult{}, m.err
	}
	return llm.GenerationResult{Content: m.content, Provider: "mock", Model: "mock-model", RequestID: "mock-1", Usage: llm.Usage{InputTokens: 100, OutputTokens: 50}, EstimatedCostUSD: m.estimatedCost}, nil
}

type githubFixture struct {
	server       *httptest.Server
	requests     atomic.Int32
	issuePosts   atomic.Int32
	dropResponse atomic.Bool
	mu           sync.Mutex
	issues       []gh.Issue
}

func TestReleaseToMarketingEndToEndIsIdempotentAcrossReplay(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	first, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if first.Status != domain.RunAwaitingApproval || first.ApprovalID == "" || first.IssueURL == "" {
		t.Fatalf("first outcome = %+v", first)
	}
	if h.model.calls.Load() != 1 || h.github.issuePosts.Load() != 1 {
		t.Fatalf("calls model=%d issue=%d", h.model.calls.Load(), h.github.issuePosts.Load())
	}
	run, err := h.store.GetRun(ctx, first.RunID)
	if err != nil || run.Status != domain.RunAwaitingApproval || run.RepositoryCommit != "fixture-commit" || run.ContextVersion != 1 || len(run.EvidenceIDs) == 0 {
		t.Fatalf("stored run = %+v, err=%v", run, err)
	}
	assets, err := h.store.AssetsForApproval(ctx, first.ApprovalID)
	if err != nil || len(assets) != 5 {
		t.Fatalf("assets=%d err=%v", len(assets), err)
	}
	for _, asset := range assets {
		if len(asset.EvidenceIDs) == 0 {
			t.Fatalf("ungrounded asset: %+v", asset)
		}
	}

	if err := h.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := state.Open(ctx, h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	h.store = reopened
	h.workflow.Store = reopened
	second, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "scheduled"})
	if err != nil {
		t.Fatalf("replay Run() error = %v", err)
	}
	if !second.Duplicate || h.model.calls.Load() != 1 || h.github.issuePosts.Load() != 1 {
		t.Fatalf("replay=%+v model=%d issues=%d", second, h.model.calls.Load(), h.github.issuePosts.Load())
	}
}

func TestAmbiguousIssueCreateReconcilesWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	h.github.dropResponse.Store(true)
	outcome, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err != nil {
		t.Fatalf("Run() after dropped issue response error = %v", err)
	}
	if outcome.Status != domain.RunAwaitingApproval || h.github.issuePosts.Load() != 1 {
		t.Fatalf("outcome=%+v issue_posts=%d", outcome, h.github.issuePosts.Load())
	}
	h.github.mu.Lock()
	issueCount := len(h.github.issues)
	h.github.mu.Unlock()
	if issueCount != 1 {
		t.Fatalf("remote issue count = %d", issueCount)
	}
}

func TestFailedModelRunCanRetrySafely(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	h.model.err = errors.New("temporary model outage")
	first, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err == nil || first.Status != domain.RunFailed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	h.model.err = nil
	second, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err != nil || second.Status != domain.RunAwaitingApproval {
		t.Fatalf("retry=%+v err=%v", second, err)
	}
	if h.model.calls.Load() != 2 || h.github.issuePosts.Load() != 1 {
		t.Fatalf("model calls=%d issue posts=%d", h.model.calls.Load(), h.github.issuePosts.Load())
	}
}

func TestCancelledModelCallPersistsCancelledRun(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	h.model.err = context.Canceled
	outcome, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if !errors.Is(err, context.Canceled) || outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	run, getErr := h.store.GetRun(context.Background(), outcome.RunID)
	if getErr != nil || run.Status != domain.RunCancelled || run.ErrorCode != "cancelled" {
		t.Fatalf("run=%+v err=%v", run, getErr)
	}
}

func TestDefinitionTimeoutAppliesToManualRun(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	definition, err := h.store.GetWorkflow(ctx, "widget", domain.ReleaseToMarketingWorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	definition.Timeout = time.Second
	if err := h.store.UpsertWorkflow(ctx, definition); err != nil {
		t.Fatal(err)
	}
	h.model.waitForContext = true
	outcome, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if !errors.Is(err, context.DeadlineExceeded) || outcome.Status != domain.RunCancelled {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	run, getErr := h.store.GetRun(context.Background(), outcome.RunID)
	if getErr != nil || run.Status != domain.RunCancelled || run.ErrorCode != "deadline_exceeded" {
		t.Fatalf("run=%+v err=%v", run, getErr)
	}
}

func TestNonFiniteModelCostCannotBypassWorkflowBudget(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	h.model.estimatedCost = math.NaN()
	outcome, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err == nil || outcome.Status != domain.RunFailed {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	run, getErr := h.store.GetRun(ctx, outcome.RunID)
	if getErr != nil || run.ErrorCode != "cost_limit_exceeded" {
		t.Fatalf("run=%+v err=%v", run, getErr)
	}
}

func TestNoActionCreatesNoApprovalIssue(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	h.model.content = noActionJSON()
	outcome, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err != nil || outcome.Status != domain.RunNoAction || outcome.ItemsActedOn != 0 {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if h.github.issuePosts.Load() != 0 {
		t.Fatalf("no_action created %d issues", h.github.issuePosts.Load())
	}
	approvals, err := h.store.ListApprovals(ctx, 10)
	if err != nil || len(approvals) != 0 {
		t.Fatalf("approvals=%+v err=%v", approvals, err)
	}
}

func TestDryRunDoesNotConsumeDedupeOrCreateIssue(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, true)
	defer h.close()
	dry, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual", DryRun: true})
	if err != nil || !dry.DryRun || h.github.issuePosts.Load() != 0 {
		t.Fatalf("dry=%+v err=%v posts=%d", dry, err, h.github.issuePosts.Load())
	}
	real, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err != nil || real.Status != domain.RunAwaitingApproval || h.github.issuePosts.Load() != 1 {
		t.Fatalf("real=%+v err=%v posts=%d", real, err, h.github.issuePosts.Load())
	}
	if h.model.calls.Load() != 2 {
		t.Fatalf("model calls=%d, dry run likely consumed dedupe", h.model.calls.Load())
	}
}

func TestUnapprovedContextBlocksBeforeGitHubOrLLM(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, false)
	defer h.close()
	outcome, err := h.workflow.Run(ctx, "widget", RunOptions{TriggerType: "manual"})
	if err == nil || outcome.Status != domain.RunBlocked {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if h.github.requests.Load() != 0 || h.model.calls.Load() != 0 || h.github.issuePosts.Load() != 0 {
		t.Fatalf("blocked workflow made external calls: github=%d model=%d issues=%d", h.github.requests.Load(), h.model.calls.Load(), h.github.issuePosts.Load())
	}
}

type harness struct {
	dbPath   string
	store    *state.Store
	workflow *ReleaseWorkflow
	model    *countingModel
	github   *githubFixture
}

func newHarness(t *testing.T, approveContext bool) *harness {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "marketing.db")
	store, err := state.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	product := domain.Product{ID: "widget", Name: "Widget", Repository: "acme/widget", RepositoryID: 1001, Website: "https://widget.example", ProductType: "saas", PrimaryConversionAction: "start_trial", DefaultLanguage: "en"}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	definition := domain.ReleaseToMarketingDefinition("widget")
	definition.Enabled = true
	if err := store.UpsertWorkflow(ctx, definition); err != nil {
		t.Fatal(err)
	}
	contextVersion, err := store.CreateContextDraft(ctx, "widget", "# Product Marketing Context\n\n**Audience:** engineering managers\n\n**Brand voice:** direct and factual\n\n**Proof points:** none beyond cited evidence", nil, []string{"customer metrics unknown"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := products.NewWorkspace(filepath.Join(root, "products"))
	if err := workspace.Initialize(product); err != nil {
		t.Fatal(err)
	}
	if err := workspace.WriteContextDraft("widget", contextVersion.Version, contextVersion.Content); err != nil {
		t.Fatal(err)
	}
	if approveContext {
		approved, err := store.ApproveContext(ctx, "widget", 1, "tester")
		if err != nil {
			t.Fatal(err)
		}
		if err := workspace.WriteApprovedContext("widget", approved.Version, approved.Content, "tester"); err != nil {
			t.Fatal(err)
		}
	}

	skillRoot := filepath.Join(root, "marketingskills")
	for _, item := range []struct{ name, body string }{{"launch", "launch rules"}, {"copywriting", "copy rules"}, {"social", "social rules"}, {"emails", "email rules"}} {
		writeFixture(t, filepath.Join(skillRoot, "skills", item.name, "SKILL.md"), "---\nname: "+item.name+"\ndescription: Safe "+item.name+" guidance.\nmetadata: {version: 1.0.0}\n---\n"+item.body)
	}
	writeFixture(t, filepath.Join(skillRoot, "skills", "social", "references", "platform-limits.md"), "platform limits")
	writeFixture(t, filepath.Join(skillRoot, "skills", "emails", "references", "copy-guidelines.md"), "email copy guidelines")
	loader := skills.NewLoader(skillRoot, filepath.Join(root, "skills.lock.yaml"))
	manifest, err := loader.ComputeManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.WriteLock(loader.LockPath, skills.Lock{Repository: "https://example.test/skills", Ref: "fixture", Commit: "fixture-commit", RepositoryVersion: "1.0.0", ManifestSHA256: manifest}); err != nil {
		t.Fatal(err)
	}

	githubFixture := newGitHubFixture(t)
	client, err := gh.NewClient(gh.Options{BaseURL: githubFixture.server.URL, Token: "token", Timeout: time.Second, MaxRetries: 0})
	if err != nil {
		t.Fatal(err)
	}
	model := &countingModel{content: marketableJSON(), estimatedCost: 0.01}
	workflow := &ReleaseWorkflow{
		Store: store, GitHub: client, Skills: loader, Model: model, Workspace: workspace,
		ApprovalRepository: "acme/approvals", ApprovalLabels: []string{"marketing-approval"},
		MaxOutputTokens: 4000, MaxRepairAttempts: 1, MarketabilityThreshold: 60,
	}
	return &harness{dbPath: dbPath, store: store, workflow: workflow, model: model, github: githubFixture}
}

func (h *harness) close() {
	if h.store != nil {
		_ = h.store.Close()
	}
	h.github.server.Close()
}

func newGitHubFixture(t *testing.T) *githubFixture {
	t.Helper()
	fixture := &githubFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget":
			_, _ = w.Write([]byte(`{"id":1001,"full_name":"acme/widget","default_branch":"main"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/releases":
			_, _ = w.Write([]byte(`[{"id":42,"tag_name":"v1.4","name":"Widget v1.4","body":"Users can now export reports as CSV.","html_url":"https://github.test/acme/widget/releases/42","draft":false,"prerelease":false,"published_at":"2026-07-18T10:00:00Z"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/widget/contents/CHANGELOG.md":
			_, _ = w.Write([]byte(`{"encoding":"base64","content":"IyB2MS40XG4tIEFkZCBDU1YgZXhwb3J0XG4="}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/approvals/issues":
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			_ = json.NewEncoder(w).Encode(fixture.issues)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/approvals/issues":
			fixture.issuePosts.Add(1)
			var request gh.CreateIssueRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode issue: %v", err)
				http.Error(w, "bad", 400)
				return
			}
			fixture.mu.Lock()
			issue := gh.Issue{ID: 9001, Number: 7, HTMLURL: "https://github.test/acme/approvals/issues/7", Title: request.Title, Body: request.Body, State: "open"}
			fixture.issues = append(fixture.issues, issue)
			fixture.mu.Unlock()
			if fixture.dropResponse.Swap(false) {
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					t.Error("test server does not support response hijacking")
					return
				}
				connection, _, hijackErr := hijacker.Hijack()
				if hijackErr != nil {
					t.Errorf("hijack issue response: %v", hijackErr)
					return
				}
				_ = connection.Close()
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(issue)
		default:
			t.Errorf("unexpected GitHub endpoint: %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	return fixture
}

func noActionJSON() string {
	return `{"action":"no_action","release_classification":"maintenance_release","marketability":{"score":12,"reason":"Dependency maintenance only."},"audience":[],"customer_value":{"summary":"","evidence_ids":[]},"assets":[],"unsupported_claims":[],"warnings":[],"requires_human_approval":false}`
}

func marketableJSON() string {
	return `{"action":"stage_for_approval","release_classification":"feature_launch","marketability":{"score":82,"reason":"Customer-visible CSV export."},"audience":["engineering managers"],"customer_value":{"summary":"Teams can export reports as CSV.","evidence_ids":["github-release-1001-42"]},"assets":[{"channel":"release_summary","content":"Export reports as CSV.","evidence_ids":["github-release-1001-42"]},{"channel":"changelog","content":"Added CSV report exports.","evidence_ids":["github-release-1001-42"]},{"channel":"linkedin","content":"Widget now exports reports as CSV.","evidence_ids":["github-release-1001-42"]},{"channel":"x","content":"Export Widget reports as CSV.","evidence_ids":["github-release-1001-42"]},{"channel":"email","subject":"Export reports as CSV","content":"Widget now supports CSV report exports.","evidence_ids":["github-release-1001-42"]}],"unsupported_claims":[],"warnings":[],"requires_human_approval":true}`
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
