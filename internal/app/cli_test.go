package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/state"
)

func TestCLIProductRegistrationListingAndDurableStopAll(t *testing.T) {
	root := t.TempDir()
	configPath := writeCLIConfig(t, root)
	var output bytes.Buffer
	command := NewRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", configPath, "--json", "product", "add", "--id", "widget", "--name", "Widget", "--repository", "acme/widget", "--product-type", "saas", "--conversion", "start_trial", "--language", "en"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("product add: %v\n%s", err, output.String())
	}
	var product domain.Product
	if err := json.Unmarshal(output.Bytes(), &product); err != nil {
		t.Fatalf("decode product: %v output=%s", err, output.String())
	}
	if product.ID != "widget" {
		t.Fatalf("product=%+v", product)
	}
	if _, err := os.Stat(filepath.Join(root, "products", "widget", "product.yaml")); err != nil {
		t.Fatal(err)
	}

	output.Reset()
	command = NewRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", configPath, "--json", "product", "list"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var products []domain.Product
	if err := json.Unmarshal(output.Bytes(), &products); err != nil || len(products) != 1 {
		t.Fatalf("products=%+v err=%v output=%s", products, err, output.String())
	}

	output.Reset()
	command = NewRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", configPath, "stop-all"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(context.Background(), filepath.Join(root, "data", "marketing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	killed, _, err := store.KillSwitch(context.Background())
	if err != nil || !killed {
		t.Fatalf("killed=%t err=%v", killed, err)
	}
	workflow, err := store.GetWorkflow(context.Background(), "widget", domain.ReleaseToMarketingWorkflowID)
	if err != nil || workflow.Enabled {
		t.Fatalf("workflow=%+v err=%v", workflow, err)
	}
}

func writeCLIConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	content := `database:
  driver: sqlite
  path: ./data/marketing.db
workspace:
  products_path: ./products
skills:
  repository_path: ./skills
  lock_file: ./skills.lock.yaml
llm:
  provider: openai-compatible
  base_url: http://127.0.0.1:9999/v1
  api_key_env: TEST_LLM_KEY
  model: test
  timeout_seconds: 5
  max_retries: 0
  max_repair_attempts: 1
  max_input_tokens: 10000
  max_output_tokens: 4000
  max_cost_per_run_usd: 1
  input_cost_per_million_usd: 0
  output_cost_per_million_usd: 0
github:
  api_base_url: http://127.0.0.1:9998
  token_env: TEST_GITHUB_TOKEN
  approval_repository: acme/approvals
  approval_labels: [marketing-approval]
  timeout_seconds: 5
  max_retries: 0
scheduler:
  enabled: true
  retry_delay_seconds: 1
  max_retries: 1
safety:
  global_kill_switch: false
  publishing_enabled: false
  sending_enabled: false
  spending_enabled: false
logging:
  level: info
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
