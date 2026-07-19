package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/omerufuk/marketing-os/internal/domain"
)

type ClaimRequest struct {
	ProductID     string
	WorkflowID    string
	TriggerID     string
	TriggerType   string
	DedupeKey     string
	InputHash     string
	LeaseDuration time.Duration
	DryRun        bool
}

type WorkflowClaim struct {
	RunID        string `json:"run_id"`
	ProductID    string `json:"product_id"`
	WorkflowID   string `json:"workflow_id"`
	DedupeKey    string `json:"dedupe_key"`
	FencingToken int64  `json:"fencing_token"`
	Attempt      int    `json:"attempt"`
}

type NoActionCompletion struct {
	Claim            WorkflowClaim
	RepositoryCommit string
	SkillVersions    map[string]string
	ContextVersion   int
	EvidenceIDs      []string
	ModelProvider    string
	ModelName        string
	InputTokens      int
	OutputTokens     int
	EstimatedCostUSD float64
	OutputHash       string
	CursorName       string
	CursorValue      string
}

var ErrStaleClaim = errors.New("workflow claim is stale")

func (s *Store) UpsertWorkflow(ctx context.Context, workflow domain.WorkflowDefinition) error {
	if err := workflow.Validate(); err != nil {
		return err
	}
	supporting, _ := json.Marshal(workflow.SupportingSkills)
	inputs, _ := json.Marshal(workflow.RequiredInputs)
	steps, _ := json.Marshal(workflow.OrderedSteps)
	stateRequirements, _ := json.Marshal(workflow.StateRequirements)
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflows(
		product_id, id, trigger_type, cadence, activation_condition, purpose,
		primary_skill, supporting_skills_json, required_inputs_json, ordered_steps_json,
		self_check, state_requirements_json, dedupe_key_template, cooldown_seconds,
		stop_condition, error_behavior, output_destination, approval_policy,
		max_cost_usd, timeout_seconds, enabled, allow_overlap, created_at, updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(product_id,id) DO UPDATE SET
		trigger_type=excluded.trigger_type, cadence=excluded.cadence,
		activation_condition=excluded.activation_condition, purpose=excluded.purpose,
		primary_skill=excluded.primary_skill, supporting_skills_json=excluded.supporting_skills_json,
		required_inputs_json=excluded.required_inputs_json, ordered_steps_json=excluded.ordered_steps_json,
		self_check=excluded.self_check, state_requirements_json=excluded.state_requirements_json,
		dedupe_key_template=excluded.dedupe_key_template, cooldown_seconds=excluded.cooldown_seconds,
		stop_condition=excluded.stop_condition, error_behavior=excluded.error_behavior,
		output_destination=excluded.output_destination, approval_policy=excluded.approval_policy,
		max_cost_usd=excluded.max_cost_usd, timeout_seconds=excluded.timeout_seconds,
		enabled=excluded.enabled, allow_overlap=excluded.allow_overlap, updated_at=excluded.updated_at`,
		workflow.ProductID, workflow.ID, workflow.Trigger, workflow.Cadence,
		workflow.ActivationCondition, workflow.Purpose, workflow.PrimarySkill, string(supporting),
		string(inputs), string(steps), workflow.SelfCheck, string(stateRequirements), workflow.DedupeKeyTemplate,
		int64(workflow.Cooldown/time.Second), workflow.StopCondition, workflow.ErrorBehavior,
		workflow.OutputDestination, workflow.ApprovalPolicy, workflow.MaxCostUSD,
		int64(workflow.Timeout/time.Second), boolInt(workflow.Enabled), boolInt(workflow.AllowOverlap), now, now)
	if err != nil {
		return fmt.Errorf("upsert workflow: %w", err)
	}
	return nil
}

func (s *Store) SetWorkflowEnabled(ctx context.Context, productID, workflowID string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workflows SET enabled=?, updated_at=? WHERE product_id=? AND id=?`, boolInt(enabled), formatTime(s.now()), productID, workflowID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: workflow %s/%s", ErrNotFound, productID, workflowID)
	}
	return nil
}

func (s *Store) GetWorkflow(ctx context.Context, productID, workflowID string) (domain.WorkflowDefinition, error) {
	row := s.db.QueryRowContext(ctx, workflowSelect+` WHERE product_id=? AND id=?`, productID, workflowID)
	workflow, err := scanWorkflow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkflowDefinition{}, fmt.Errorf("%w: workflow", ErrNotFound)
	}
	return workflow, err
}

func (s *Store) ListWorkflows(ctx context.Context, productID string) ([]domain.WorkflowDefinition, error) {
	rows, err := s.db.QueryContext(ctx, workflowSelect+` WHERE product_id=? ORDER BY id`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.WorkflowDefinition
	for rows.Next() {
		workflow, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, workflow)
	}
	return result, rows.Err()
}

const workflowSelect = `SELECT product_id,id,trigger_type,cadence,activation_condition,purpose,
	primary_skill,supporting_skills_json,required_inputs_json,ordered_steps_json,self_check,
	state_requirements_json,dedupe_key_template,cooldown_seconds,stop_condition,error_behavior,
	output_destination,approval_policy,max_cost_usd,timeout_seconds,enabled,allow_overlap FROM workflows`

func scanWorkflow(row rowScanner) (domain.WorkflowDefinition, error) {
	var workflow domain.WorkflowDefinition
	var supporting, inputs, steps, requirements string
	var cooldown, timeout int64
	var enabled, overlap int
	err := row.Scan(&workflow.ProductID, &workflow.ID, &workflow.Trigger, &workflow.Cadence,
		&workflow.ActivationCondition, &workflow.Purpose, &workflow.PrimarySkill, &supporting,
		&inputs, &steps, &workflow.SelfCheck, &requirements, &workflow.DedupeKeyTemplate,
		&cooldown, &workflow.StopCondition, &workflow.ErrorBehavior, &workflow.OutputDestination,
		&workflow.ApprovalPolicy, &workflow.MaxCostUSD, &timeout, &enabled, &overlap)
	if err != nil {
		return domain.WorkflowDefinition{}, err
	}
	_ = json.Unmarshal([]byte(supporting), &workflow.SupportingSkills)
	_ = json.Unmarshal([]byte(inputs), &workflow.RequiredInputs)
	_ = json.Unmarshal([]byte(steps), &workflow.OrderedSteps)
	_ = json.Unmarshal([]byte(requirements), &workflow.StateRequirements)
	workflow.Cooldown, workflow.Timeout = time.Duration(cooldown)*time.Second, time.Duration(timeout)*time.Second
	workflow.Enabled, workflow.AllowOverlap = enabled == 1, overlap == 1
	return workflow, nil
}

func (s *Store) ClaimWorkflow(ctx context.Context, request ClaimRequest) (WorkflowClaim, error) {
	if err := domain.ValidateProductID(request.ProductID); err != nil {
		return WorkflowClaim{}, err
	}
	if request.WorkflowID == "" || request.TriggerID == "" || request.TriggerType == "" || request.DedupeKey == "" || request.InputHash == "" || request.LeaseDuration <= 0 {
		return WorkflowClaim{}, errors.New("complete workflow claim request is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkflowClaim{}, claimError(err)
	}
	defer tx.Rollback()
	var enabled, allowOverlap int
	if err := tx.QueryRowContext(ctx, `SELECT enabled,allow_overlap FROM workflows WHERE product_id=? AND id=?`, request.ProductID, request.WorkflowID).Scan(&enabled, &allowOverlap); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkflowClaim{}, fmt.Errorf("%w: workflow", ErrNotFound)
		}
		return WorkflowClaim{}, claimError(err)
	}
	if enabled != 1 {
		return WorkflowClaim{}, errors.New("workflow is disabled")
	}
	now := s.now()
	leaseExpires := now.Add(request.LeaseDuration)
	var dedupeState, storedInputHash, dedupeLease string
	dedupeErr := tx.QueryRowContext(ctx, `SELECT state,input_hash,lease_expires_at FROM dedupe_keys WHERE product_id=? AND workflow_id=? AND dedupe_key=?`, request.ProductID, request.WorkflowID, request.DedupeKey).Scan(&dedupeState, &storedInputHash, &dedupeLease)
	if dedupeErr == nil {
		if storedInputHash != request.InputHash {
			return WorkflowClaim{}, fmt.Errorf("%w: release input changed for an existing stable event", ErrImmutableConflict)
		}
		if dedupeState == "completed" {
			return WorkflowClaim{}, fmt.Errorf("%w: event already completed", ErrDuplicate)
		}
		lease, _ := parseTime(dedupeLease)
		if dedupeState == "in_flight" && lease.After(now) {
			return WorkflowClaim{}, fmt.Errorf("%w: event in flight", ErrBusy)
		}
	} else if !errors.Is(dedupeErr, sql.ErrNoRows) {
		return WorkflowClaim{}, claimError(dedupeErr)
	}

	var previousRun string
	var previousFence int64
	var workflowLease string
	claimErr := tx.QueryRowContext(ctx, `SELECT run_id,fencing_token,lease_expires_at FROM workflow_claims WHERE product_id=? AND workflow_id=?`, request.ProductID, request.WorkflowID).Scan(&previousRun, &previousFence, &workflowLease)
	leaseExpired := false
	if claimErr == nil {
		lease, _ := parseTime(workflowLease)
		if allowOverlap == 0 && lease.After(now) {
			return WorkflowClaim{}, fmt.Errorf("%w: workflow lease active", ErrBusy)
		}
		leaseExpired = !lease.After(now)
	} else if !errors.Is(claimErr, sql.ErrNoRows) {
		return WorkflowClaim{}, claimError(claimErr)
	}
	if leaseExpired {
		result, updateErr := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=?,finished_at=?,error_code=?,error_message=? WHERE id=? AND status=?`,
			domain.RunCancelled, formatTime(now), "lease_expired", "workflow lease expired before completion", previousRun, domain.RunRunning)
		if updateErr != nil {
			return WorkflowClaim{}, claimError(updateErr)
		}
		if rows, _ := result.RowsAffected(); rows == 1 {
			if _, updateErr = tx.ExecContext(ctx, `INSERT INTO errors(id,run_id,code,message,retryable,created_at) VALUES(?,?,?,?,1,?)`, uuid.NewString(), previousRun, "lease_expired", "workflow lease expired before completion", formatTime(now)); updateErr != nil {
				return WorkflowClaim{}, claimError(updateErr)
			}
			if updateErr = insertAudit(ctx, tx, request.ProductID, request.WorkflowID, previousRun, "workflow.cancelled", map[string]any{"code": "lease_expired"}, now); updateErr != nil {
				return WorkflowClaim{}, updateErr
			}
		}
	}
	fence := previousFence + 1
	if fence < 1 {
		fence = 1
	}
	var attempt int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)+1 FROM workflow_runs WHERE product_id=? AND workflow_id=? AND dedupe_key=?`, request.ProductID, request.WorkflowID, request.DedupeKey).Scan(&attempt); err != nil {
		return WorkflowClaim{}, claimError(err)
	}
	runID := uuid.NewString()
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_runs(id,product_id,workflow_id,trigger_id,trigger_type,dedupe_key,input_hash,status,attempt,fencing_token,started_at,dry_run)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, runID, request.ProductID, request.WorkflowID, request.TriggerID,
		request.TriggerType, request.DedupeKey, request.InputHash, domain.RunRunning, attempt, fence, formatTime(now), boolInt(request.DryRun))
	if err != nil {
		return WorkflowClaim{}, claimError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_claims(product_id,workflow_id,run_id,fencing_token,lease_expires_at,claimed_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(product_id,workflow_id) DO UPDATE SET run_id=excluded.run_id,
		fencing_token=excluded.fencing_token,lease_expires_at=excluded.lease_expires_at,claimed_at=excluded.claimed_at`,
		request.ProductID, request.WorkflowID, runID, fence, formatTime(leaseExpires), formatTime(now))
	if err != nil {
		return WorkflowClaim{}, claimError(err)
	}
	if dedupeErr == nil {
		_, err = tx.ExecContext(ctx, `UPDATE dedupe_keys SET state='in_flight',run_id=?,lease_expires_at=?,updated_at=? WHERE product_id=? AND workflow_id=? AND dedupe_key=? AND state!='completed'`,
			runID, formatTime(leaseExpires), formatTime(now), request.ProductID, request.WorkflowID, request.DedupeKey)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO dedupe_keys(product_id,workflow_id,dedupe_key,input_hash,state,run_id,lease_expires_at,updated_at)
			VALUES(?,?,?,?,'in_flight',?,?,?)`, request.ProductID, request.WorkflowID, request.DedupeKey, request.InputHash, runID, formatTime(leaseExpires), formatTime(now))
	}
	if err != nil {
		return WorkflowClaim{}, claimError(err)
	}
	claim := WorkflowClaim{RunID: runID, ProductID: request.ProductID, WorkflowID: request.WorkflowID, DedupeKey: request.DedupeKey, FencingToken: fence, Attempt: attempt}
	if err := insertAudit(ctx, tx, request.ProductID, request.WorkflowID, runID, "workflow.claimed", map[string]any{"attempt": attempt, "trigger_id": request.TriggerID}, now); err != nil {
		return WorkflowClaim{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkflowClaim{}, claimError(err)
	}
	return claim, nil
}

func (s *Store) FailWorkflowRun(ctx context.Context, claim WorkflowClaim, code, message string) error {
	if code == "" {
		code = "workflow_failed"
	}
	return s.finishUnsuccessfulRun(ctx, claim, domain.RunFailed, code, message, true)
}

func (s *Store) CancelWorkflowRun(ctx context.Context, claim WorkflowClaim, code, message string) error {
	if code == "" {
		code = "cancelled"
	}
	return s.finishUnsuccessfulRun(ctx, claim, domain.RunCancelled, code, message, false)
}

func (s *Store) finishUnsuccessfulRun(ctx context.Context, claim WorkflowClaim, status domain.RunStatus, code, message string, retryable bool) error {
	if status != domain.RunFailed && status != domain.RunCancelled {
		return errors.New("unsuccessful run status must be failed or cancelled")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyClaim(ctx, tx, claim); err != nil {
		return err
	}
	now := s.now()
	result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=?,finished_at=?,error_code=?,error_message=? WHERE id=? AND status=? AND fencing_token=?`, status, formatTime(now), code, message, claim.RunID, domain.RunRunning, claim.FencingToken)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dedupe_keys SET state='failed',updated_at=? WHERE product_id=? AND workflow_id=? AND dedupe_key=? AND run_id=?`, formatTime(now), claim.ProductID, claim.WorkflowID, claim.DedupeKey, claim.RunID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_claims WHERE product_id=? AND workflow_id=? AND run_id=? AND fencing_token=?`, claim.ProductID, claim.WorkflowID, claim.RunID, claim.FencingToken); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO errors(id,run_id,code,message,retryable,created_at) VALUES(?,?,?,?,?,?)`, uuid.NewString(), claim.RunID, code, message, boolInt(retryable), formatTime(now)); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, claim.ProductID, claim.WorkflowID, claim.RunID, "workflow."+string(status), map[string]any{"code": code}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompleteNoAction(ctx context.Context, completion NoActionCompletion) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := verifyClaim(ctx, tx, completion.Claim); err != nil {
		return err
	}
	now := s.now()
	skillsJSON, _ := json.Marshal(nonNilMap(completion.SkillVersions))
	evidenceJSON, _ := json.Marshal(nonNilStrings(completion.EvidenceIDs))
	result, err := tx.ExecContext(ctx, `UPDATE workflow_runs SET status=?,repository_commit=?,skill_versions_json=?,context_version=?,model_provider=?,model_name=?,evidence_ids_json=?,finished_at=?,input_tokens=?,output_tokens=?,estimated_cost_usd=?,output_hash=? WHERE id=? AND status=? AND fencing_token=?`,
		domain.RunNoAction, completion.RepositoryCommit, string(skillsJSON), completion.ContextVersion,
		completion.ModelProvider, completion.ModelName, string(evidenceJSON), formatTime(now), completion.InputTokens,
		completion.OutputTokens, completion.EstimatedCostUSD, completion.OutputHash, completion.Claim.RunID,
		domain.RunRunning, completion.Claim.FencingToken)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	if err := completeDedupeAndCursor(ctx, tx, completion.Claim, completion.CursorName, completion.CursorValue, now); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, completion.Claim.ProductID, completion.Claim.WorkflowID, completion.Claim.RunID, "workflow.no_action", map[string]any{"items_checked": 1, "items_acted_on": 0}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func completeDedupeAndCursor(ctx context.Context, tx *sql.Tx, claim WorkflowClaim, cursorName, cursorValue string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE dedupe_keys SET state='completed',completed_at=?,updated_at=? WHERE product_id=? AND workflow_id=? AND dedupe_key=? AND run_id=? AND state='in_flight'`, formatTime(now), formatTime(now), claim.ProductID, claim.WorkflowID, claim.DedupeKey, claim.RunID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	if cursorName != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO cursors(product_id,workflow_id,name,value,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(product_id,workflow_id,name) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, claim.ProductID, claim.WorkflowID, cursorName, cursorValue, formatTime(now))
		if err != nil {
			return err
		}
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM workflow_claims WHERE product_id=? AND workflow_id=? AND run_id=? AND fencing_token=?`, claim.ProductID, claim.WorkflowID, claim.RunID, claim.FencingToken)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrStaleClaim
	}
	return nil
}

func verifyClaim(ctx context.Context, tx *sql.Tx, claim WorkflowClaim) error {
	var runID string
	var fence int64
	err := tx.QueryRowContext(ctx, `SELECT run_id,fencing_token FROM workflow_claims WHERE product_id=? AND workflow_id=?`, claim.ProductID, claim.WorkflowID).Scan(&runID, &fence)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrStaleClaim
	}
	if err != nil {
		return err
	}
	if runID != claim.RunID || fence != claim.FencingToken {
		return ErrStaleClaim
	}
	return nil
}

func (s *Store) Cursor(ctx context.Context, productID, workflowID, name string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM cursors WHERE product_id=? AND workflow_id=? AND name=?`, productID, workflowID, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) GetRun(ctx context.Context, runID string) (domain.WorkflowRun, error) {
	run, err := scanRun(s.db.QueryRowContext(ctx, runSelect+` WHERE id=?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkflowRun{}, fmt.Errorf("%w: run", ErrNotFound)
	}
	return run, err
}

func (s *Store) ListRuns(ctx context.Context, limit int) ([]domain.WorkflowRun, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, runSelect+` ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.WorkflowRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

const runSelect = `SELECT id,product_id,workflow_id,trigger_id,trigger_type,dedupe_key,status,attempt,
	repository_commit,skill_versions_json,context_version,model_provider,model_name,evidence_ids_json,
	started_at,finished_at,input_tokens,output_tokens,estimated_cost_usd,output_hash,approval_id,
	error_code,error_message,dry_run FROM workflow_runs`

func scanRun(row rowScanner) (domain.WorkflowRun, error) {
	var run domain.WorkflowRun
	var status, skillsJSON, evidenceJSON, started string
	var finished sql.NullString
	var dry int
	err := row.Scan(&run.ID, &run.ProductID, &run.WorkflowID, &run.TriggerID, &run.TriggerType, &run.DedupeKey,
		&status, &run.Attempt, &run.RepositoryCommit, &skillsJSON, &run.ContextVersion, &run.ModelProvider,
		&run.ModelName, &evidenceJSON, &started, &finished, &run.InputTokens, &run.OutputTokens, &run.EstimatedCostUSD,
		&run.OutputHash, &run.ApprovalID, &run.ErrorCode, &run.ErrorMessage, &dry)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	run.Status = domain.RunStatus(status)
	run.StartedAt, _ = parseTime(started)
	run.DryRun = dry == 1
	_ = json.Unmarshal([]byte(skillsJSON), &run.SkillVersions)
	_ = json.Unmarshal([]byte(evidenceJSON), &run.EvidenceIDs)
	if finished.Valid {
		value, _ := parseTime(finished.String)
		run.FinishedAt = &value
	}
	return run, nil
}

func insertAudit(ctx context.Context, tx *sql.Tx, productID, workflowID, runID, eventType string, data map[string]any, now time.Time) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,product_id,workflow_id,run_id,event_type,data_json,created_at) VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), productID, workflowID, runID, eventType, string(encoded), formatTime(now))
	return err
}

func claimError(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "database is locked") || strings.Contains(lower, "busy") {
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
	return err
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func nonNilMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return value
}
