package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/state"
	"github.com/omerufuk/marketing-os/internal/workflows"
)

type fakeRunner struct {
	calls  atomic.Int32
	notify chan struct{}
}

func (r *fakeRunner) Run(context.Context, string, workflows.RunOptions) (workflows.RunOutcome, error) {
	r.calls.Add(1)
	if r.notify != nil {
		select {
		case r.notify <- struct{}{}:
		default:
		}
	}
	return workflows.RunOutcome{}, nil
}

func TestKillSwitchStopsScheduledExecutionWithoutDeletingState(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(ctx, filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runner := &fakeRunner{}
	scheduler := New(store, runner, Options{})
	if err := store.SetKillSwitch(ctx, true, "operator stop"); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunOnce(ctx, "alpha", "release-to-marketing"); !errors.Is(err, ErrKilled) {
		t.Fatalf("RunOnce error = %v", err)
	}
	if runner.calls.Load() != 0 {
		t.Fatalf("runner called %d times while killed", runner.calls.Load())
	}
	enabled, reason, err := store.KillSwitch(ctx)
	if err != nil || !enabled || reason != "operator stop" {
		t.Fatalf("kill state enabled=%t reason=%q err=%v", enabled, reason, err)
	}
	if err := store.SetKillSwitch(ctx, false, "operator resume"); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.RunOnce(ctx, "alpha", "release-to-marketing"); err != nil {
		t.Fatal(err)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls = %d", runner.calls.Load())
	}
}

func TestRunningSchedulerReconcilesNewlyEnabledWorkflow(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(ctx, filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	product := domain.Product{ID: "alpha", Name: "Alpha", Repository: "acme/alpha", ProductType: "saas", PrimaryConversionAction: "start trial", DefaultLanguage: "en"}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	definition := domain.ReleaseToMarketingDefinition(product.ID)
	definition.Cadence = "@every 1s"
	definition.Enabled = false
	if err := store.UpsertWorkflow(ctx, definition); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{notify: make(chan struct{}, 1)}
	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- New(store, runner, Options{RefreshInterval: 10 * time.Millisecond}).Start(runCtx)
	}()
	t.Cleanup(cancel)
	time.Sleep(30 * time.Millisecond)
	if err := store.SetWorkflowEnabled(ctx, product.ID, definition.ID, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.notify:
	case <-time.After(2 * time.Second):
		t.Fatal("newly enabled workflow was not scheduled without restart")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not shut down after cancellation")
	}
}
