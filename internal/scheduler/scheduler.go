package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/state"
	"github.com/omerufuk/marketing-os/internal/workflows"
	"github.com/robfig/cron/v3"
)

var ErrKilled = errors.New("global kill switch is enabled")

type Runner interface {
	Run(context.Context, string, workflows.RunOptions) (workflows.RunOutcome, error)
}

type Options struct {
	RetryDelay      time.Duration
	MaxRetries      int
	RefreshInterval time.Duration
	Logger          *slog.Logger
}

type Scheduler struct {
	store   *state.Store
	runner  Runner
	options Options
}

func New(store *state.Store, runner Runner, options Options) *Scheduler {
	if options.RetryDelay <= 0 {
		options.RetryDelay = 30 * time.Second
	}
	if options.MaxRetries < 0 {
		options.MaxRetries = 0
	}
	if options.RefreshInterval <= 0 {
		options.RefreshInterval = 30 * time.Second
	}
	return &Scheduler{store: store, runner: runner, options: options}
}

func (s *Scheduler) RunOnce(ctx context.Context, productID, workflowID string) error {
	if s.store == nil || s.runner == nil {
		return errors.New("scheduler store and runner are required")
	}
	killed, reason, err := s.store.KillSwitch(ctx)
	if err != nil {
		return fmt.Errorf("read kill switch: %w", err)
	}
	if killed {
		run, recordErr := s.store.RecordTerminalRun(ctx, productID, workflowID, "scheduled", domain.RunKilled, "kill_switch", reason)
		if s.options.Logger != nil {
			s.options.Logger.Warn("scheduled workflow blocked by kill switch", "product", productID, "workflow", workflowID, "run_id", run.ID, "reason", reason, "record_error", recordErr)
		}
		return fmt.Errorf("%w: %s", ErrKilled, reason)
	}
	if workflowID != domain.ReleaseToMarketingWorkflowID {
		return fmt.Errorf("unsupported scheduled workflow %q", workflowID)
	}
	_, err = s.runner.Run(ctx, productID, workflows.RunOptions{TriggerType: "scheduled"})
	return err
}

func (s *Scheduler) Start(ctx context.Context) error {
	if s.store == nil || s.runner == nil {
		return errors.New("scheduler store and runner are required")
	}
	clock := cron.New()
	type registration struct {
		entry       cron.EntryID
		fingerprint string
	}
	registered := map[string]registration{}
	reconcile := func() error {
		definitions, err := s.store.ListEnabledScheduledWorkflows(ctx)
		if err != nil {
			return fmt.Errorf("list enabled workflows: %w", err)
		}
		wanted := make(map[string]domain.WorkflowDefinition, len(definitions))
		for _, definition := range definitions {
			key := definition.ProductID + "\x00" + definition.ID
			wanted[key] = definition
			fingerprint := fmt.Sprintf("%s|%s", definition.Cadence, definition.Timeout)
			if current, ok := registered[key]; ok && current.fingerprint == fingerprint {
				continue
			}
			if current, ok := registered[key]; ok {
				clock.Remove(current.entry)
			}
			definition := definition
			entry, err := clock.AddFunc(definition.Cadence, func() {
				s.executeWithRetry(ctx, definition)
			})
			if err != nil {
				return fmt.Errorf("register cron for %s/%s: %w", definition.ProductID, definition.ID, err)
			}
			registered[key] = registration{entry: entry, fingerprint: fingerprint}
		}
		for key, current := range registered {
			if _, ok := wanted[key]; !ok {
				clock.Remove(current.entry)
				delete(registered, key)
			}
		}
		return nil
	}
	if err := reconcile(); err != nil {
		return err
	}
	clock.Start()
	if s.options.Logger != nil {
		s.options.Logger.Info("scheduler started", "workflows", len(registered))
	}
	ticker := time.NewTicker(s.options.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := reconcile(); err != nil && s.options.Logger != nil {
				s.options.Logger.Error("scheduler reconciliation failed", "error", err)
			}
		case <-ctx.Done():
			stopCtx := clock.Stop()
			select {
			case <-stopCtx.Done():
			case <-time.After(10 * time.Second):
				return errors.New("scheduler shutdown timed out")
			}
			return nil
		}
	}
}

func (s *Scheduler) executeWithRetry(parent context.Context, definition domain.WorkflowDefinition) {
	for attempt := 0; attempt <= s.options.MaxRetries; attempt++ {
		if parent.Err() != nil {
			return
		}
		runCtx, cancel := context.WithTimeout(parent, definition.Timeout)
		err := s.RunOnce(runCtx, definition.ProductID, definition.ID)
		cancel()
		if err == nil || errors.Is(err, ErrKilled) || errors.Is(err, state.ErrDuplicate) {
			return
		}
		if s.options.Logger != nil {
			s.options.Logger.Error("scheduled workflow failed", "product", definition.ProductID, "workflow", definition.ID, "attempt", attempt+1, "error", err)
		}
		if attempt == s.options.MaxRetries {
			return
		}
		timer := time.NewTimer(s.options.RetryDelay)
		select {
		case <-parent.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
