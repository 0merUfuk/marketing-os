package state

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestTransactionalWorkflowClaimAllowsExactlyOneConcurrentRunner(t *testing.T) {
	ctx := context.Background()
	store := newWorkflowStore(t)
	defer store.Close()
	const workers = 12
	start := make(chan struct{})
	var acquired atomic.Int32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			claim, err := store.ClaimWorkflow(ctx, ClaimRequest{
				ProductID: "alpha", WorkflowID: "release-to-marketing", TriggerID: "github-release:42",
				TriggerType: "manual", DedupeKey: domain.ReleaseDedupeKey("alpha", "release-to-marketing", 1001, 42),
				InputHash: "input-42", LeaseDuration: time.Minute,
			})
			if err == nil {
				acquired.Add(1)
				_ = claim
				return
			}
			if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrDuplicate) {
				t.Errorf("unexpected claim error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if acquired.Load() != 1 {
		t.Fatalf("acquired claims = %d, want 1", acquired.Load())
	}
}

func TestFailedRunRemainsRetryableAndCursorAdvancesOnlyAfterSuccess(t *testing.T) {
	ctx := context.Background()
	store := newWorkflowStore(t)
	defer store.Close()
	request := ClaimRequest{
		ProductID: "alpha", WorkflowID: "release-to-marketing", TriggerID: "github-release:42",
		TriggerType: "manual", DedupeKey: domain.ReleaseDedupeKey("alpha", "release-to-marketing", 1001, 42),
		InputHash: "input-42", LeaseDuration: time.Minute,
	}
	first, err := store.ClaimWorkflow(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FailWorkflowRun(ctx, first, "llm_unavailable", "temporary"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Cursor(ctx, "alpha", "release-to-marketing", "github_release"); err != nil || found {
		t.Fatalf("cursor advanced after failure: found=%t err=%v", found, err)
	}
	second, err := store.ClaimWorkflow(ctx, request)
	if err != nil {
		t.Fatalf("retry claim: %v", err)
	}
	if second.Attempt != 2 {
		t.Fatalf("retry attempt = %d", second.Attempt)
	}
	if err := store.CompleteNoAction(ctx, NoActionCompletion{
		Claim: second, RepositoryCommit: "skills-commit", SkillVersions: map[string]string{"launch": "2.0.1"},
		ContextVersion: 1, EvidenceIDs: []string{"release-42"}, ModelProvider: "mock", ModelName: "mock-model",
		InputTokens: 10, OutputTokens: 5, EstimatedCostUSD: 0.001, OutputHash: "output", CursorName: "github_release", CursorValue: "42",
	}); err != nil {
		t.Fatal(err)
	}
	cursor, found, err := store.Cursor(ctx, "alpha", "release-to-marketing", "github_release")
	if err != nil || !found || cursor != "42" {
		t.Fatalf("cursor=%q found=%t err=%v", cursor, found, err)
	}
	if _, err := store.ClaimWorkflow(ctx, request); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("completed release claim error = %v", err)
	}
	run, err := store.GetRun(ctx, second.RunID)
	if err != nil || run.Status != domain.RunNoAction {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestExpiredLeaseTakeoverCancelsStaleRun(t *testing.T) {
	ctx := context.Background()
	store := newWorkflowStore(t)
	defer store.Close()
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	request := ClaimRequest{
		ProductID: "alpha", WorkflowID: "release-to-marketing", TriggerID: "github-release:42",
		TriggerType: "scheduled", DedupeKey: domain.ReleaseDedupeKey("alpha", "release-to-marketing", 1001, 42),
		InputHash: "input-42", LeaseDuration: time.Minute,
	}
	first, err := store.ClaimWorkflow(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	second, err := store.ClaimWorkflow(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempt != 2 {
		t.Fatalf("second attempt=%d", second.Attempt)
	}
	stale, err := store.GetRun(ctx, first.RunID)
	if err != nil || stale.Status != domain.RunCancelled || stale.ErrorCode != "lease_expired" || stale.FinishedAt == nil {
		t.Fatalf("stale run=%+v err=%v", stale, err)
	}
}

func newWorkflowStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	product := domain.Product{ID: "alpha", Name: "Alpha", Repository: "acme/alpha", RepositoryID: 1001, ProductType: "saas", PrimaryConversionAction: "signup", DefaultLanguage: "en"}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	definition := domain.ReleaseToMarketingDefinition("alpha")
	definition.Enabled = true
	if err := store.UpsertWorkflow(ctx, definition); err != nil {
		t.Fatal(err)
	}
	return store
}
