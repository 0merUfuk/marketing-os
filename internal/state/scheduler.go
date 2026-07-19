package state

import (
	"context"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func (s *Store) SetKillSwitch(ctx context.Context, enabled bool, reason string) error {
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `UPDATE scheduler_state SET kill_switch=?,reason=?,updated_at=? WHERE singleton=1`, boolInt(enabled), reason, now)
	return err
}

func (s *Store) KillSwitch(ctx context.Context) (bool, string, error) {
	var value int
	var reason string
	err := s.db.QueryRowContext(ctx, `SELECT kill_switch,reason FROM scheduler_state WHERE singleton=1`).Scan(&value, &reason)
	if err != nil {
		return false, "", err
	}
	return value == 1, reason, nil
}

func (s *Store) ListEnabledScheduledWorkflows(ctx context.Context) ([]domain.WorkflowDefinition, error) {
	rows, err := s.db.QueryContext(ctx, workflowSelect+` WHERE enabled=1 AND cadence!='' ORDER BY product_id,id`)
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
