package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestApprovalIntentSurvivesFailedRunAndFinalizesOnRetry(t *testing.T) {
	ctx := context.Background()
	store := newWorkflowStore(t)
	defer store.Close()
	request := ClaimRequest{
		ProductID: "alpha", WorkflowID: "release-to-marketing", TriggerID: "github-release:42", TriggerType: "manual",
		DedupeKey: domain.ReleaseDedupeKey("alpha", "release-to-marketing", 1001, 42), InputHash: "input-42", LeaseDuration: time.Minute,
	}
	first, err := store.ClaimWorkflow(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	intent := ApprovalIntentInput{
		Claim: first, ApprovalID: "approval-stable", TriggerID: request.TriggerID,
		EvidenceSummaryJSON: `[{"id":"release-42"}]`, ProposedActionJSON: `{"action":"stage_for_approval"}`,
		RisksJSON: `[]`, WarningsJSON: `[]`, EstimatedCostUSD: 0.01,
		IssueRepository: "acme/approvals", IssueMarker: "<!-- marketing-os-approval:approval-stable -->",
		IssueRequestHash: "issue-hash", IssueTitle: "Approval", IssueBody: "body",
		Assets:      []domain.GeneratedAsset{{ID: "asset-1", Channel: "linkedin", Content: "draft", EvidenceIDs: []string{"release-42"}, ContentHash: domain.AssetContentHash("linkedin", "", "draft")}},
		RunMetadata: RunMetadata{RepositoryCommit: "commit", SkillVersions: map[string]string{"launch": "2.0.1"}, ContextVersion: 1, ModelProvider: "mock", ModelName: "mock", EvidenceIDs: []string{"release-42"}, InputTokens: 10, OutputTokens: 5, EstimatedCostUSD: 0.01, OutputHash: "output"},
	}
	approval, err := store.PersistApprovalIntent(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != domain.ApprovalCreating {
		t.Fatalf("approval=%+v", approval)
	}
	if err := store.FailWorkflowRun(ctx, first, "github_issue_ambiguous", "retry reconciliation"); err != nil {
		t.Fatal(err)
	}

	second, err := store.ClaimWorkflow(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	recovered, found, err := store.ApprovalByDedupe(ctx, "alpha", "release-to-marketing", request.DedupeKey)
	if err != nil || !found || recovered.ID != approval.ID || recovered.IssueMarker != approval.IssueMarker {
		t.Fatalf("recovered=%+v found=%t err=%v", recovered, found, err)
	}
	if err := store.CompleteAwaitingApproval(ctx, AwaitingApprovalCompletion{
		Claim: second, ApprovalID: approval.ID, IssueID: 9001, IssueNumber: 7, IssueURL: "https://github.test/issues/7",
		RunMetadata: intent.RunMetadata, CursorName: "github_release", CursorValue: "42",
	}); err != nil {
		t.Fatal(err)
	}
	final, found, err := store.ApprovalByDedupe(ctx, "alpha", "release-to-marketing", request.DedupeKey)
	if err != nil || !found || final.Status != domain.ApprovalAwaiting || final.IssueID != 9001 {
		t.Fatalf("final=%+v found=%t err=%v", final, found, err)
	}
	assets, err := store.AssetsForApproval(ctx, approval.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
}

func TestApprovalSchemaSurvivesDatabaseRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "approval.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
}
