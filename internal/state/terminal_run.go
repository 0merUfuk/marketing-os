package state

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/omerufuk/marketing-os/internal/domain"
)

func (s *Store) RecordTerminalRun(ctx context.Context, productID, workflowID, triggerType string, status domain.RunStatus, code, message string) (domain.WorkflowRun, error) {
	if status != domain.RunBlocked && status != domain.RunKilled && status != domain.RunCancelled {
		return domain.WorkflowRun{}, errors.New("terminal preflight status must be blocked, killed, or cancelled")
	}
	if strings.TrimSpace(triggerType) == "" {
		triggerType = "scheduled"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	defer tx.Rollback()
	now := s.now()
	runID := uuid.NewString()
	triggerID := "preflight:" + runID
	dedupeKey := triggerID
	_, err = tx.ExecContext(ctx, `INSERT INTO workflow_runs(id,product_id,workflow_id,trigger_id,trigger_type,dedupe_key,input_hash,status,attempt,fencing_token,started_at,finished_at,error_code,error_message,dry_run)
		VALUES(?,?,?,?,?,?,?, ?,1,0,?,?,?,?,0)`, runID, productID, workflowID, triggerID, triggerType, dedupeKey, domain.ContentHash(triggerID), string(status), formatTime(now), formatTime(now), code, message)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	if err := insertAudit(ctx, tx, productID, workflowID, runID, "workflow_"+string(status), map[string]any{"code": code, "message": message}, now); err != nil {
		return domain.WorkflowRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.WorkflowRun{}, err
	}
	return s.GetRun(ctx, runID)
}
