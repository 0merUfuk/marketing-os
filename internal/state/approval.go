package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/omerufuk/marketing-os/internal/domain"
)

type RunMetadata struct {
	RepositoryCommit string
	SkillVersions    map[string]string
	ContextVersion   int
	ModelProvider    string
	ModelName        string
	EvidenceIDs      []string
	InputTokens      int
	OutputTokens     int
	EstimatedCostUSD float64
	OutputHash       string
}

type ApprovalIntentInput struct {
	Claim               WorkflowClaim
	ApprovalID          string
	TriggerID           string
	EvidenceSummaryJSON string
	ProposedActionJSON  string
	RisksJSON           string
	WarningsJSON        string
	EstimatedCostUSD    float64
	IssueRepository     string
	IssueMarker         string
	IssueRequestHash    string
	IssueTitle          string
	IssueBody           string
	Assets              []domain.GeneratedAsset
	RunMetadata         RunMetadata
}

type AwaitingApprovalCompletion struct {
	Claim       WorkflowClaim
	ApprovalID  string
	IssueID     int64
	IssueNumber int
	IssueURL    string
	RunMetadata RunMetadata
	CursorName  string
	CursorValue string
}

func (s *Store) PersistApprovalIntent(ctx context.Context, input ApprovalIntentInput) (domain.Approval, error) {
	if input.ApprovalID == "" || input.TriggerID == "" || input.IssueRepository == "" || input.IssueMarker == "" || input.IssueRequestHash == "" || input.IssueTitle == "" || input.IssueBody == "" {
		return domain.Approval{}, errors.New("complete approval intent is required")
	}
	for name, raw := range map[string]string{"evidence summary": input.EvidenceSummaryJSON, "proposed action": input.ProposedActionJSON, "risks": input.RisksJSON, "warnings": input.WarningsJSON} {
		if !json.Valid([]byte(raw)) {
			return domain.Approval{}, fmt.Errorf("%s must be valid JSON", name)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Approval{}, err
	}
	defer tx.Rollback()
	if err := verifyClaim(ctx, tx, input.Claim); err != nil {
		return domain.Approval{}, err
	}
	existing, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE product_id=? AND workflow_id=? AND dedupe_key=?`, input.Claim.ProductID, input.Claim.WorkflowID, input.Claim.DedupeKey))
	if err == nil {
		if existing.ID != input.ApprovalID || existing.IssueRequestHash != input.IssueRequestHash {
			return domain.Approval{}, fmt.Errorf("%w: approval intent changed", ErrImmutableConflict)
		}
		if err := tx.Commit(); err != nil {
			return domain.Approval{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, err
	}
	now := s.now()
	approval := domain.Approval{
		ID: input.ApprovalID, ProductID: input.Claim.ProductID, WorkflowID: input.Claim.WorkflowID,
		DedupeKey: input.Claim.DedupeKey, TriggerID: input.TriggerID, RunID: input.Claim.RunID,
		Status: domain.ApprovalCreating, EvidenceSummaryJSON: input.EvidenceSummaryJSON,
		ProposedActionJSON: input.ProposedActionJSON, RisksJSON: input.RisksJSON,
		WarningsJSON: input.WarningsJSON, EstimatedCostUSD: input.EstimatedCostUSD,
		IssueRepository: input.IssueRepository, IssueMarker: input.IssueMarker,
		IssueRequestHash: input.IssueRequestHash, IssueTitle: input.IssueTitle, IssueBody: input.IssueBody,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO approvals(
		id,product_id,workflow_id,dedupe_key,trigger_id,run_id,status,evidence_summary_json,
		proposed_action_json,risks_json,warnings_json,estimated_cost_usd,issue_repository,
		issue_marker,issue_request_hash,issue_title,issue_body,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, approval.ID, approval.ProductID, approval.WorkflowID,
		approval.DedupeKey, approval.TriggerID, approval.RunID, approval.Status, approval.EvidenceSummaryJSON,
		approval.ProposedActionJSON, approval.RisksJSON, approval.WarningsJSON, approval.EstimatedCostUSD,
		approval.IssueRepository, approval.IssueMarker, approval.IssueRequestHash, approval.IssueTitle,
		approval.IssueBody, formatTime(now), formatTime(now))
	if err != nil {
		return domain.Approval{}, fmt.Errorf("insert approval intent: %w", err)
	}
	for _, asset := range input.Assets {
		if asset.ID == "" || asset.Channel == "" || asset.Content == "" || len(asset.EvidenceIDs) == 0 {
			return domain.Approval{}, errors.New("generated asset id, channel, content, and evidence are required")
		}
		expectedHash := domain.AssetContentHash(asset.Channel, asset.Subject, asset.Content)
		if asset.ContentHash == "" {
			asset.ContentHash = expectedHash
		}
		if asset.ContentHash != expectedHash {
			return domain.Approval{}, errors.New("generated asset content hash is invalid")
		}
		evidenceJSON, _ := json.Marshal(asset.EvidenceIDs)
		_, err := tx.ExecContext(ctx, `INSERT INTO generated_assets(id,product_id,workflow_id,run_id,approval_id,channel,subject,content,evidence_ids_json,content_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			asset.ID, input.Claim.ProductID, input.Claim.WorkflowID, input.Claim.RunID, input.ApprovalID,
			asset.Channel, asset.Subject, asset.Content, string(evidenceJSON), asset.ContentHash, formatTime(now))
		if err != nil {
			return domain.Approval{}, fmt.Errorf("insert generated asset: %w", err)
		}
	}
	if err := updateRunningMetadata(ctx, tx, input.Claim, input.RunMetadata); err != nil {
		return domain.Approval{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_history(id,approval_id,from_status,to_status,actor,external_event_id,note,created_at) VALUES(?,?,?,?,'system',?,'approval intent persisted',?)`, uuid.NewString(), input.ApprovalID, "", domain.ApprovalCreating, "intent:"+input.ApprovalID, formatTime(now)); err != nil {
		return domain.Approval{}, err
	}
	if err := insertAudit(ctx, tx, input.Claim.ProductID, input.Claim.WorkflowID, input.Claim.RunID, "approval.intent_created", map[string]any{"approval_id": input.ApprovalID, "assets": len(input.Assets)}, now); err != nil {
		return domain.Approval{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Approval{}, err
	}
	return approval, nil
}

func (s *Store) CompleteAwaitingApproval(ctx context.Context, completion AwaitingApprovalCompletion) error {
	if completion.ApprovalID == "" || completion.IssueID <= 0 || completion.IssueNumber <= 0 || completion.IssueURL == "" {
		return errors.New("approval id and GitHub issue identity are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyClaim(ctx, tx, completion.Claim); err != nil {
		return err
	}
	approval, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE id=?`, completion.ApprovalID))
	if err != nil {
		return err
	}
	if approval.ProductID != completion.Claim.ProductID || approval.WorkflowID != completion.Claim.WorkflowID || approval.DedupeKey != completion.Claim.DedupeKey {
		return ErrEvidenceScope
	}
	if approval.Status != domain.ApprovalCreating {
		return fmt.Errorf("approval is in unexpected state %s", approval.Status)
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `UPDATE approvals SET status=?,run_id=?,issue_id=?,issue_number=?,issue_url=?,updated_at=? WHERE id=? AND status=?`,
		domain.ApprovalAwaiting, completion.Claim.RunID, completion.IssueID, completion.IssueNumber,
		completion.IssueURL, formatTime(now), completion.ApprovalID, domain.ApprovalCreating)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	if err := finishAwaitingRun(ctx, tx, completion.Claim, completion.RunMetadata, completion.ApprovalID, now); err != nil {
		return err
	}
	if err := completeDedupeAndCursor(ctx, tx, completion.Claim, completion.CursorName, completion.CursorValue, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO approval_history(id,approval_id,from_status,to_status,actor,external_event_id,note,created_at) VALUES(?,?,?,?,?,?,?,?)`, uuid.NewString(), completion.ApprovalID, domain.ApprovalCreating, domain.ApprovalAwaiting, "system", fmt.Sprintf("github-issue:%d", completion.IssueID), "GitHub approval issue stored", formatTime(now)); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, completion.Claim.ProductID, completion.Claim.WorkflowID, completion.Claim.RunID, "workflow.awaiting_approval", map[string]any{"approval_id": completion.ApprovalID, "issue_id": completion.IssueID, "items_acted_on": 1}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func updateRunningMetadata(ctx context.Context, tx *sql.Tx, claim WorkflowClaim, metadata RunMetadata) error {
	skillsJSON, _ := json.Marshal(nonNilMap(metadata.SkillVersions))
	evidenceJSON, _ := json.Marshal(nonNilStrings(metadata.EvidenceIDs))
	result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET repository_commit=?,skill_versions_json=?,context_version=?,model_provider=?,model_name=?,evidence_ids_json=?,input_tokens=?,output_tokens=?,estimated_cost_usd=?,output_hash=? WHERE id=? AND status=? AND fencing_token=?`,
		metadata.RepositoryCommit, string(skillsJSON), metadata.ContextVersion, metadata.ModelProvider, metadata.ModelName,
		string(evidenceJSON), metadata.InputTokens, metadata.OutputTokens, metadata.EstimatedCostUSD,
		metadata.OutputHash, claim.RunID, domain.RunRunning, claim.FencingToken)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	return nil
}

func finishAwaitingRun(ctx context.Context, tx *sql.Tx, claim WorkflowClaim, metadata RunMetadata, approvalID string, now time.Time) error {
	if err := updateRunningMetadata(ctx, tx, claim, metadata); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=?,approval_id=?,finished_at=? WHERE id=? AND status=? AND fencing_token=?`, domain.RunAwaitingApproval, approvalID, formatTime(now), claim.RunID, domain.RunRunning, claim.FencingToken)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	return nil
}

func (s *Store) ApprovalByDedupe(ctx context.Context, productID, workflowID, dedupeKey string) (domain.Approval, bool, error) {
	approval, err := scanApproval(s.db.QueryRowContext(ctx, approvalSelect+` WHERE product_id=? AND workflow_id=? AND dedupe_key=?`, productID, workflowID, dedupeKey))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, false, nil
	}
	return approval, err == nil, err
}

func (s *Store) GetApproval(ctx context.Context, id string) (domain.Approval, error) {
	approval, err := scanApproval(s.db.QueryRowContext(ctx, approvalSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Approval{}, fmt.Errorf("%w: approval", ErrNotFound)
	}
	return approval, err
}

func (s *Store) ListApprovals(ctx context.Context, limit int) ([]domain.Approval, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, approvalSelect+` ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Approval
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, approval)
	}
	return result, rows.Err()
}

const approvalSelect = `SELECT id,product_id,workflow_id,dedupe_key,trigger_id,run_id,status,
	evidence_summary_json,proposed_action_json,risks_json,warnings_json,estimated_cost_usd,
	issue_repository,issue_marker,issue_request_hash,issue_title,issue_body,issue_id,
	issue_number,issue_url,created_at,updated_at FROM approvals`

func scanApproval(row rowScanner) (domain.Approval, error) {
	var approval domain.Approval
	var status, created, updated string
	var issueID, issueNumber sql.NullInt64
	err := row.Scan(&approval.ID, &approval.ProductID, &approval.WorkflowID, &approval.DedupeKey,
		&approval.TriggerID, &approval.RunID, &status, &approval.EvidenceSummaryJSON, &approval.ProposedActionJSON,
		&approval.RisksJSON, &approval.WarningsJSON, &approval.EstimatedCostUSD, &approval.IssueRepository,
		&approval.IssueMarker, &approval.IssueRequestHash, &approval.IssueTitle, &approval.IssueBody,
		&issueID, &issueNumber, &approval.IssueURL, &created, &updated)
	if err != nil {
		return domain.Approval{}, err
	}
	approval.Status = domain.ApprovalStatus(status)
	approval.CreatedAt, _ = parseTime(created)
	approval.UpdatedAt, _ = parseTime(updated)
	if issueID.Valid {
		approval.IssueID = issueID.Int64
	}
	if issueNumber.Valid {
		approval.IssueNumber = int(issueNumber.Int64)
	}
	return approval, nil
}

func (s *Store) AssetsForApproval(ctx context.Context, approvalID string) ([]domain.GeneratedAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,product_id,workflow_id,run_id,approval_id,channel,subject,content,evidence_ids_json,content_hash,created_at FROM generated_assets WHERE approval_id=? ORDER BY channel`, approvalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.GeneratedAsset
	for rows.Next() {
		var asset domain.GeneratedAsset
		var evidenceJSON, created string
		if err := rows.Scan(&asset.ID, &asset.ProductID, &asset.WorkflowID, &asset.RunID, &asset.ApprovalID,
			&asset.Channel, &asset.Subject, &asset.Content, &evidenceJSON, &asset.ContentHash, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evidenceJSON), &asset.EvidenceIDs)
		asset.CreatedAt, _ = parseTime(created)
		result = append(result, asset)
	}
	return result, rows.Err()
}
