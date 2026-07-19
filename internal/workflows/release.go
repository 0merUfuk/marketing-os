package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/omerufuk/marketing-os/internal/approvals"
	"github.com/omerufuk/marketing-os/internal/domain"
	gh "github.com/omerufuk/marketing-os/internal/github"
	"github.com/omerufuk/marketing-os/internal/llm"
	"github.com/omerufuk/marketing-os/internal/products"
	"github.com/omerufuk/marketing-os/internal/skillruntime"
	"github.com/omerufuk/marketing-os/internal/skills"
	"github.com/omerufuk/marketing-os/internal/state"
)

type GitHubAPI interface {
	Repository(context.Context, string) (gh.Repository, error)
	LatestPublishedRelease(context.Context, string) (gh.Release, error)
	Release(context.Context, string, int64) (gh.Release, error)
	ReadFile(context.Context, string, string, string) ([]byte, error)
	FindIssueByMarker(context.Context, string, string) (gh.Issue, bool, error)
	CreateIssue(context.Context, string, gh.CreateIssueRequest) (gh.Issue, error)
}

type ReleaseWorkflow struct {
	Store                  *state.Store
	GitHub                 GitHubAPI
	Skills                 *skills.Loader
	Model                  llm.ModelClient
	Workspace              *products.Workspace
	ApprovalRepository     string
	ApprovalLabels         []string
	MaxOutputTokens        int
	MaxRepairAttempts      int
	MarketabilityThreshold int
	Secrets                []string
	Logger                 *slog.Logger
}

type RunOptions struct {
	TriggerType string
	ReleaseID   int64
	DryRun      bool
}

type RunOutcome struct {
	RunID        string           `json:"run_id,omitempty"`
	ProductID    string           `json:"product_id"`
	WorkflowID   string           `json:"workflow_id"`
	ReleaseID    int64            `json:"release_id,omitempty"`
	Status       domain.RunStatus `json:"status"`
	Action       string           `json:"action,omitempty"`
	Duplicate    bool             `json:"duplicate"`
	DryRun       bool             `json:"dry_run"`
	ApprovalID   string           `json:"approval_id,omitempty"`
	IssueURL     string           `json:"issue_url,omitempty"`
	ItemsChecked int              `json:"items_checked"`
	ItemsActedOn int              `json:"items_acted_on"`
}

func (w *ReleaseWorkflow) Run(ctx context.Context, productID string, options RunOptions) (RunOutcome, error) {
	const workflowID = domain.ReleaseToMarketingWorkflowID
	outcome := RunOutcome{ProductID: productID, WorkflowID: workflowID, Status: domain.RunBlocked, DryRun: options.DryRun}
	if w.Store == nil || w.GitHub == nil || w.Skills == nil || w.Model == nil || w.Workspace == nil {
		return outcome, errors.New("release workflow dependencies are incomplete")
	}
	if w.MaxOutputTokens <= 0 {
		return outcome, errors.New("release workflow max output tokens must be positive")
	}
	if w.MarketabilityThreshold <= 0 {
		w.MarketabilityThreshold = 60
	}
	if options.TriggerType == "" {
		options.TriggerType = "manual"
	}
	product, err := w.Store.GetProduct(ctx, productID)
	if err != nil {
		return outcome, err
	}
	definition, err := w.Store.GetWorkflow(ctx, productID, workflowID)
	if err != nil {
		return outcome, err
	}
	ctx, cancel := context.WithTimeout(ctx, definition.Timeout)
	defer cancel()
	blocked := func(code string, cause error) (RunOutcome, error) {
		if !options.DryRun {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cleanupCancel()
			run, recordErr := w.Store.RecordTerminalRun(cleanupCtx, productID, workflowID, options.TriggerType, domain.RunBlocked, code, cause.Error())
			if recordErr == nil {
				outcome.RunID = run.ID
			} else if w.Logger != nil {
				w.Logger.Error("failed to record blocked workflow run", "product", productID, "workflow", workflowID, "error", recordErr)
			}
		}
		return outcome, cause
	}
	if !definition.Enabled {
		return blocked("workflow_disabled", errors.New("release-to-marketing workflow is disabled"))
	}
	approvedContext, err := w.Store.ApprovedContext(ctx, productID)
	if err != nil {
		return blocked("approved_context_required", fmt.Errorf("workflow blocked: %w", err))
	}
	lock, err := w.Skills.RequirePinned(ctx)
	if err != nil {
		return blocked("skills_pin_invalid", fmt.Errorf("workflow blocked by skills pin: %w", err))
	}
	skillSet, err := skillruntime.LoadReleaseSkills(ctx, w.Skills)
	if err != nil {
		return blocked("skills_load_failed", fmt.Errorf("workflow blocked by skill loading: %w", err))
	}
	snapshot := []skills.Skill{skillSet.Primary.Skill}
	for _, bundle := range skillSet.Supporting {
		snapshot = append(snapshot, bundle.Skill)
	}
	if !options.DryRun {
		if err := w.Store.SyncSkillSnapshot(ctx, lock, snapshot); err != nil {
			return blocked("skills_snapshot_failed", fmt.Errorf("workflow blocked while recording skill versions: %w", err))
		}
	}
	if strings.TrimSpace(w.ApprovalRepository) == "" {
		return blocked("approval_repository_required", errors.New("workflow blocked: GitHub approval repository is required"))
	}

	repository, err := w.GitHub.Repository(ctx, product.Repository)
	if err != nil {
		return blocked("github_repository_failed", fmt.Errorf("read GitHub repository: %w", err))
	}
	if product.RepositoryID != 0 && product.RepositoryID != repository.ID {
		return blocked("github_repository_identity_mismatch", errors.New("configured GitHub repository ID does not match the repository name"))
	}
	var release gh.Release
	if options.ReleaseID > 0 {
		release, err = w.GitHub.Release(ctx, product.Repository, options.ReleaseID)
	} else {
		release, err = w.GitHub.LatestPublishedRelease(ctx, product.Repository)
	}
	if err != nil {
		return blocked("github_release_failed", fmt.Errorf("read GitHub release: %w", err))
	}
	outcome.ReleaseID, outcome.ItemsChecked = release.ID, 1
	triggerID := fmt.Sprintf("github-release:%d", release.ID)
	inputBytes, _ := json.Marshal(release)
	inputHash := domain.ContentHash(string(inputBytes))
	dedupeKey := domain.ReleaseDedupeKey(productID, workflowID, repository.ID, release.ID)

	if options.DryRun {
		evidence := w.transientReleaseEvidence(product, repository, release)
		return w.generateDryRun(ctx, outcome, product, approvedContext, release, evidence, lock, skillSet, definition)
	}
	claim, err := w.Store.ClaimWorkflow(ctx, state.ClaimRequest{
		ProductID: productID, WorkflowID: workflowID, TriggerID: triggerID, TriggerType: options.TriggerType,
		DedupeKey: dedupeKey, InputHash: inputHash, LeaseDuration: definition.Timeout,
	})
	if errors.Is(err, state.ErrDuplicate) {
		outcome.Status, outcome.Action, outcome.Duplicate = domain.RunNoAction, "duplicate", true
		return outcome, nil
	}
	if err != nil {
		return outcome, err
	}
	outcome.RunID, outcome.Status = claim.RunID, domain.RunRunning

	if existing, found, lookupErr := w.Store.ApprovalByDedupe(ctx, productID, workflowID, dedupeKey); lookupErr != nil {
		return w.fail(ctx, outcome, claim, "approval_lookup_failed", lookupErr)
	} else if found {
		return w.recoverApproval(ctx, outcome, claim, existing, release)
	}

	evidence, err := w.captureReleaseEvidence(ctx, product, repository, release)
	if err != nil {
		return w.fail(ctx, outcome, claim, "evidence_capture_failed", err)
	}
	prompt, err := skillruntime.BuildReleasePrompt(skillruntime.ReleasePromptInput{
		Product: product, ApprovedContext: approvedContext.Content, ContextVersion: approvedContext.Version,
		Release: release, Evidence: evidence, RepositoryCommit: lock.Commit, Skills: skillSet,
	})
	if err != nil {
		return w.fail(ctx, outcome, claim, "prompt_assembly_failed", err)
	}
	allowedEvidence := make(map[string]struct{}, len(evidence))
	evidenceIDs := make([]string, 0, len(evidence))
	for _, item := range evidence {
		allowedEvidence[item.ID] = struct{}{}
		evidenceIDs = append(evidenceIDs, item.ID)
	}
	result, generation, err := skillruntime.GenerateRelease(ctx, w.Model, skillruntime.ReleaseGenerationRequest{
		System: prompt.System, Prompt: prompt.Prompt, MaxOutputTokens: w.MaxOutputTokens,
		MaxRepairAttempts: w.MaxRepairAttempts, AllowedEvidence: allowedEvidence,
		ForbiddenTerms: skillruntime.WordsToAvoid(approvedContext.Content), Secrets: w.Secrets,
	})
	if err != nil {
		return w.fail(ctx, outcome, claim, "model_generation_failed", err)
	}
	if !validEstimatedCost(generation.EstimatedCostUSD) || generation.EstimatedCostUSD > definition.MaxCostUSD {
		return w.fail(ctx, outcome, claim, "cost_limit_exceeded", fmt.Errorf("model cost %.6f exceeds workflow limit %.6f", generation.EstimatedCostUSD, definition.MaxCostUSD))
	}
	if check := skillruntime.SelfCheckReleaseResult(result, allowedEvidence, skillruntime.RequiredReleaseChannels); !check.Passed {
		return w.fail(ctx, outcome, claim, "self_check_failed", fmt.Errorf("workflow self-check failed: %s", strings.Join(check.Issues, "; ")))
	}
	if result.Action == "stage_for_approval" && result.Marketability.Score < w.MarketabilityThreshold {
		result = belowThresholdResult(result, w.MarketabilityThreshold)
	}
	resultBytes, _ := json.Marshal(result)
	outputHash := domain.ContentHash(string(resultBytes))
	metadata := state.RunMetadata{
		RepositoryCommit: lock.Commit, SkillVersions: prompt.SkillVersions, ContextVersion: approvedContext.Version,
		ModelProvider: generation.Provider, ModelName: generation.Model, EvidenceIDs: evidenceIDs,
		InputTokens: generation.Usage.InputTokens, OutputTokens: generation.Usage.OutputTokens,
		EstimatedCostUSD: generation.EstimatedCostUSD, OutputHash: outputHash,
	}
	if result.Action == "no_action" {
		err := w.Store.CompleteNoAction(ctx, state.NoActionCompletion{
			Claim: claim, RepositoryCommit: metadata.RepositoryCommit, SkillVersions: metadata.SkillVersions,
			ContextVersion: metadata.ContextVersion, EvidenceIDs: metadata.EvidenceIDs,
			ModelProvider: metadata.ModelProvider, ModelName: metadata.ModelName, InputTokens: metadata.InputTokens,
			OutputTokens: metadata.OutputTokens, EstimatedCostUSD: metadata.EstimatedCostUSD, OutputHash: metadata.OutputHash,
			CursorName: "github_release", CursorValue: strconv.FormatInt(release.ID, 10),
		})
		if err != nil {
			return w.fail(ctx, outcome, claim, "no_action_persistence_failed", err)
		}
		outcome.Status, outcome.Action = domain.RunNoAction, "no_action"
		w.logOutcome(outcome, generation)
		return outcome, nil
	}
	return w.stageApproval(ctx, outcome, claim, product, release, evidence, result, metadata)
}

func validEstimatedCost(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (w *ReleaseWorkflow) transientReleaseEvidence(product domain.Product, repository gh.Repository, release gh.Release) []domain.Evidence {
	content := releaseEvidenceContent(release)
	return []domain.Evidence{{ID: fmt.Sprintf("github-release-%d-%d", repository.ID, release.ID), ProductID: product.ID, SourceType: "github_release", SourceURL: release.HTMLURL, ExternalID: strconv.FormatInt(release.ID, 10), CapturedAt: time.Now().UTC(), Content: content, ContentHash: domain.ContentHash(content)}}
}

func (w *ReleaseWorkflow) captureReleaseEvidence(ctx context.Context, product domain.Product, repository gh.Repository, release gh.Release) ([]domain.Evidence, error) {
	base := w.transientReleaseEvidence(product, repository, release)[0]
	saved, err := w.Store.SaveEvidence(ctx, domain.EvidenceInput{ID: base.ID, ProductID: base.ProductID, SourceType: base.SourceType, SourceURL: base.SourceURL, ExternalID: base.ExternalID, Content: base.Content, Metadata: map[string]any{"tag": release.Tag, "published_at": release.PublishedAt}})
	if err != nil {
		return nil, err
	}
	result := []domain.Evidence{saved}
	if changelog, readErr := w.GitHub.ReadFile(ctx, product.Repository, "CHANGELOG.md", release.Tag); readErr == nil && len(changelog) > 0 {
		if len(changelog) > 128*1024 {
			changelog = changelog[:128*1024]
		}
		input := domain.EvidenceInput{ID: fmt.Sprintf("github-changelog-%d-%d", repository.ID, release.ID), ProductID: product.ID, SourceType: "github_changelog", SourceURL: repository.HTMLURL + "/blob/" + release.Tag + "/CHANGELOG.md", ExternalID: fmt.Sprintf("%d:CHANGELOG.md", release.ID), Content: string(changelog), Metadata: map[string]any{"tag": release.Tag, "path": "CHANGELOG.md"}}
		stored, saveErr := w.Store.SaveEvidence(ctx, input)
		if saveErr != nil {
			return nil, saveErr
		}
		result = append(result, stored)
	}
	return result, nil
}

func releaseEvidenceContent(release gh.Release) string {
	return fmt.Sprintf("Release title: %s\nTag: %s\nPublished at: %s\n\n%s", release.Name, release.Tag, release.PublishedAt.UTC().Format(time.RFC3339), release.Body)
}

func (w *ReleaseWorkflow) generateDryRun(ctx context.Context, outcome RunOutcome, product domain.Product, approved domain.ContextVersion, release gh.Release, evidence []domain.Evidence, lock skills.Lock, set skillruntime.ReleaseSkillSet, definition domain.WorkflowDefinition) (RunOutcome, error) {
	prompt, err := skillruntime.BuildReleasePrompt(skillruntime.ReleasePromptInput{Product: product, ApprovedContext: approved.Content, ContextVersion: approved.Version, Release: release, Evidence: evidence, RepositoryCommit: lock.Commit, Skills: set})
	if err != nil {
		return outcome, err
	}
	allowed := map[string]struct{}{}
	for _, item := range evidence {
		allowed[item.ID] = struct{}{}
	}
	result, metadata, err := skillruntime.GenerateRelease(ctx, w.Model, skillruntime.ReleaseGenerationRequest{System: prompt.System, Prompt: prompt.Prompt, MaxOutputTokens: w.MaxOutputTokens, MaxRepairAttempts: w.MaxRepairAttempts, AllowedEvidence: allowed, ForbiddenTerms: skillruntime.WordsToAvoid(approved.Content), Secrets: w.Secrets})
	if err != nil {
		return outcome, err
	}
	if !validEstimatedCost(metadata.EstimatedCostUSD) || metadata.EstimatedCostUSD > definition.MaxCostUSD {
		return outcome, llm.ErrCostLimit
	}
	outcome.DryRun, outcome.Action, outcome.ItemsActedOn = true, result.Action, 0
	if result.Action == "no_action" {
		outcome.Status = domain.RunNoAction
	} else {
		outcome.Status = domain.RunAwaitingApproval
	}
	return outcome, nil
}

func belowThresholdResult(result skillruntime.ReleaseResult, threshold int) skillruntime.ReleaseResult {
	return skillruntime.ReleaseResult{Action: "no_action", ReleaseClassification: result.ReleaseClassification, Marketability: skillruntime.Marketability{Score: result.Marketability.Score, Reason: fmt.Sprintf("Below deterministic activation threshold %d/100: %s", threshold, result.Marketability.Reason)}, Audience: []string{}, CustomerValue: skillruntime.GroundedText{EvidenceIDs: []string{}}, Assets: []skillruntime.AssetDraft{}, UnsupportedClaims: result.UnsupportedClaims, Warnings: append(result.Warnings, "No assets staged because marketability score was below threshold."), RequiresHumanApproval: false}
}

func (w *ReleaseWorkflow) stageApproval(ctx context.Context, outcome RunOutcome, claim state.WorkflowClaim, product domain.Product, release gh.Release, evidence []domain.Evidence, result skillruntime.ReleaseResult, metadata state.RunMetadata) (RunOutcome, error) {
	approvalID := deterministicID("approval", claim.DedupeKey)
	summaries := make([]approvals.EvidenceSummary, 0, len(evidence))
	for _, item := range evidence {
		summaries = append(summaries, approvals.EvidenceSummary{ID: item.ID, Source: item.SourceType, Content: item.Content})
	}
	input := approvals.IssueInput{ApprovalID: approvalID, ProductID: product.ID, ProductName: product.Name, WorkflowID: claim.WorkflowID, RunID: claim.RunID, TriggerID: fmt.Sprintf("github-release:%d", release.ID), ReleaseTitle: release.Name, ReleaseURL: release.HTMLURL, Evidence: summaries, Result: result, EstimatedCostUSD: metadata.EstimatedCostUSD, CreatedAt: time.Now().UTC()}
	title, body, marker, err := approvals.RenderIssue(input)
	if err != nil {
		return w.fail(ctx, outcome, claim, "approval_render_failed", err)
	}
	resultJSON, _ := json.Marshal(result)
	summaryJSON, _ := json.Marshal(summaries)
	risksJSON, _ := json.Marshal(result.UnsupportedClaims)
	warningsJSON, _ := json.Marshal(result.Warnings)
	assets := make([]domain.GeneratedAsset, 0, len(result.Assets))
	for _, draft := range result.Assets {
		hash := domain.AssetContentHash(draft.Channel, draft.Subject, draft.Content)
		assets = append(assets, domain.GeneratedAsset{ID: deterministicID("asset", claim.DedupeKey+":"+draft.Channel+":"+hash), Channel: draft.Channel, Subject: draft.Subject, Content: draft.Content, EvidenceIDs: draft.EvidenceIDs, ContentHash: hash})
	}
	approval, err := w.Store.PersistApprovalIntent(ctx, state.ApprovalIntentInput{Claim: claim, ApprovalID: approvalID, TriggerID: input.TriggerID, EvidenceSummaryJSON: string(summaryJSON), ProposedActionJSON: string(resultJSON), RisksJSON: string(risksJSON), WarningsJSON: string(warningsJSON), EstimatedCostUSD: metadata.EstimatedCostUSD, IssueRepository: w.ApprovalRepository, IssueMarker: marker, IssueRequestHash: domain.ContentHash(title + "\n" + body), IssueTitle: title, IssueBody: body, Assets: assets, RunMetadata: metadata})
	if err != nil {
		return w.fail(ctx, outcome, claim, "approval_intent_failed", err)
	}
	if err := w.mirrorApproval(approval, assets); err != nil {
		return w.fail(ctx, outcome, claim, "approval_workspace_failed", err)
	}
	return w.createOrRecoverIssue(ctx, outcome, claim, approval, metadata, release.ID)
}

func (w *ReleaseWorkflow) recoverApproval(ctx context.Context, outcome RunOutcome, claim state.WorkflowClaim, approval domain.Approval, release gh.Release) (RunOutcome, error) {
	if approval.Status != domain.ApprovalCreating {
		return w.fail(ctx, outcome, claim, "approval_state_invalid", fmt.Errorf("approval %s is %s", approval.ID, approval.Status))
	}
	previous, err := w.Store.GetRun(ctx, approval.RunID)
	if err != nil {
		return w.fail(ctx, outcome, claim, "approval_run_missing", err)
	}
	metadata := state.RunMetadata{RepositoryCommit: previous.RepositoryCommit, SkillVersions: previous.SkillVersions, ContextVersion: previous.ContextVersion, ModelProvider: previous.ModelProvider, ModelName: previous.ModelName, EvidenceIDs: previous.EvidenceIDs, InputTokens: previous.InputTokens, OutputTokens: previous.OutputTokens, EstimatedCostUSD: previous.EstimatedCostUSD, OutputHash: previous.OutputHash}
	assets, err := w.Store.AssetsForApproval(ctx, approval.ID)
	if err != nil {
		return w.fail(ctx, outcome, claim, "approval_assets_missing", err)
	}
	if err := w.mirrorApproval(approval, assets); err != nil {
		return w.fail(ctx, outcome, claim, "approval_workspace_failed", err)
	}
	return w.createOrRecoverIssue(ctx, outcome, claim, approval, metadata, release.ID)
}

func (w *ReleaseWorkflow) mirrorApproval(approval domain.Approval, assets []domain.GeneratedAsset) error {
	if err := w.Workspace.WriteApproval(approval.ProductID, approval.ID, approval.IssueBody); err != nil {
		return err
	}
	return w.Workspace.WriteAssets(approval.ProductID, approval.ID, assets)
}

func (w *ReleaseWorkflow) createOrRecoverIssue(ctx context.Context, outcome RunOutcome, claim state.WorkflowClaim, approval domain.Approval, metadata state.RunMetadata, releaseID int64) (RunOutcome, error) {
	issue, found, err := w.GitHub.FindIssueByMarker(ctx, approval.IssueRepository, approval.IssueMarker)
	if err != nil {
		return w.fail(ctx, outcome, claim, "approval_reconciliation_failed", err)
	}
	if !found {
		issue, err = w.GitHub.CreateIssue(ctx, approval.IssueRepository, gh.CreateIssueRequest{Title: approval.IssueTitle, Body: approval.IssueBody, Labels: w.ApprovalLabels})
		if err != nil {
			var reconcileErr error
			issue, found, reconcileErr = w.GitHub.FindIssueByMarker(ctx, approval.IssueRepository, approval.IssueMarker)
			if reconcileErr != nil || !found {
				return w.fail(ctx, outcome, claim, "github_issue_creation_failed", err)
			}
		}
	}
	err = w.Store.CompleteAwaitingApproval(ctx, state.AwaitingApprovalCompletion{Claim: claim, ApprovalID: approval.ID, IssueID: issue.ID, IssueNumber: issue.Number, IssueURL: issue.HTMLURL, RunMetadata: metadata, CursorName: "github_release", CursorValue: strconv.FormatInt(releaseID, 10)})
	if err != nil {
		return w.fail(ctx, outcome, claim, "approval_finalization_failed", err)
	}
	outcome.Status, outcome.Action, outcome.ApprovalID, outcome.IssueURL, outcome.ItemsActedOn = domain.RunAwaitingApproval, "stage_for_approval", approval.ID, issue.HTMLURL, 1
	w.logOutcome(outcome, skillruntime.GenerationMetadata{Provider: metadata.ModelProvider, Model: metadata.ModelName, Usage: llm.Usage{InputTokens: metadata.InputTokens, OutputTokens: metadata.OutputTokens}, EstimatedCostUSD: metadata.EstimatedCostUSD})
	return outcome, nil
}

func (w *ReleaseWorkflow) fail(ctx context.Context, outcome RunOutcome, claim state.WorkflowClaim, code string, cause error) (RunOutcome, error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var failErr error
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		outcome.Status = domain.RunCancelled
		code = "cancelled"
		if errors.Is(cause, context.DeadlineExceeded) {
			code = "deadline_exceeded"
		}
		failErr = w.Store.CancelWorkflowRun(cleanupCtx, claim, code, cause.Error())
	} else {
		outcome.Status = domain.RunFailed
		failErr = w.Store.FailWorkflowRun(cleanupCtx, claim, code, cause.Error())
	}
	if failErr != nil {
		return outcome, fmt.Errorf("%v; additionally failed to persist workflow failure: %w", cause, failErr)
	}
	return outcome, cause
}

func deterministicID(prefix, value string) string {
	hash := domain.ContentHash(value)
	return prefix + "-" + hash[:24]
}

func (w *ReleaseWorkflow) logOutcome(outcome RunOutcome, metadata skillruntime.GenerationMetadata) {
	if w.Logger == nil {
		return
	}
	w.Logger.Info("workflow run finished", "run_id", outcome.RunID, "product", outcome.ProductID, "workflow", outcome.WorkflowID, "trigger_release_id", outcome.ReleaseID, "items_checked", outcome.ItemsChecked, "items_acted_on", outcome.ItemsActedOn, "result", outcome.Status, "input_tokens", metadata.Usage.InputTokens, "output_tokens", metadata.Usage.OutputTokens, "estimated_cost_usd", metadata.EstimatedCostUSD, "approval_id", outcome.ApprovalID)
}
