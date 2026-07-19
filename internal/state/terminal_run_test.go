package state

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
)

func TestRecordTerminalRunPersistsKilledStateAndAudit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	product := domain.Product{ID: "alpha", Name: "Alpha", Repository: "acme/alpha", ProductType: "saas", PrimaryConversionAction: "trial", DefaultLanguage: "en"}
	if err := store.AddProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	definition := domain.ReleaseToMarketingDefinition(product.ID)
	if err := store.UpsertWorkflow(ctx, definition); err != nil {
		t.Fatal(err)
	}
	run, err := store.RecordTerminalRun(ctx, product.ID, definition.ID, "scheduled", domain.RunKilled, "kill_switch", "operator stop")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != domain.RunKilled || run.ErrorCode != "kill_switch" || run.FinishedAt == nil {
		t.Fatalf("run=%+v", run)
	}
	var eventType string
	if err := store.db.QueryRowContext(ctx, `SELECT event_type FROM audit_events WHERE run_id=?`, run.ID).Scan(&eventType); err != nil || eventType != "workflow_killed" {
		t.Fatalf("event_type=%q err=%v", eventType, err)
	}
}
