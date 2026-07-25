package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/skills"
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

func TestCLIVendoredSkillsStatusListAndInvalidExit(t *testing.T) {
	root := t.TempDir()
	configPath := writeCLIConfig(t, root)
	writeCLISkillsFixture(t, root)

	var output bytes.Buffer
	command := NewRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", configPath, "--json", "skills", "status"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("valid skills status: %v\n%s", err, output.String())
	}
	var status skills.Status
	if err := json.Unmarshal(output.Bytes(), &status); err != nil || !status.PinValid || !status.InventoryMatches {
		t.Fatalf("status=%+v err=%v output=%s", status, err, output.String())
	}

	output.Reset()
	command = NewRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", configPath, "--json", "skills", "list"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("skills list: %v\n%s", err, output.String())
	}
	var indexed []skills.Skill
	if err := json.Unmarshal(output.Bytes(), &indexed); err != nil || len(indexed) != 5 {
		t.Fatalf("indexed=%+v err=%v output=%s", indexed, err, output.String())
	}

	skillPath := filepath.Join(root, "skills", "skills", "launch", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: launch\ndescription: tampered\nmetadata: {version: 1.0.0}\n---\nchanged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	command = NewRootCommand()
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", configPath, "--json", "skills", "status"})
	err := command.ExecuteContext(context.Background())
	if !errors.Is(err, skills.ErrInvalidPin) || !strings.Contains(output.String(), `"pin_valid": false`) {
		t.Fatalf("invalid status err=%v output=%s", err, output.String())
	}
}

func TestCLIVendoredSkillsUpdateRefusesBeforeFilesystemMutation(t *testing.T) {
	for _, args := range [][]string{{"skills", "update"}, {"skills", "update", "--ref", strings.Repeat("1", 40)}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			root := t.TempDir()
			missingConfig := filepath.Join(root, "missing.yaml")
			var output bytes.Buffer
			command := NewRootCommand()
			command.SetOut(&output)
			command.SetErr(&output)
			command.SetArgs(append([]string{"--config", missingConfig}, args...))
			err := command.ExecuteContext(context.Background())
			if !errors.Is(err, skills.ErrVendoredUpdateDisabled) {
				t.Fatalf("update err=%v output=%s", err, output.String())
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("runtime update mutated filesystem: %+v", entries)
			}
		})
	}
}

func writeCLISkillsFixture(t *testing.T, root string) {
	t.Helper()
	repository := filepath.Join(root, "skills")
	for _, name := range []string{"copywriting", "emails", "launch", "product-marketing", "social"} {
		path := filepath.Join(repository, "skills", name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: Safe " + name + " guidance.\nmetadata: {version: 1.0.0}\n---\nbody\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loader := skills.NewLoader(repository, filepath.Join(root, "skills.lock.yaml"))
	manifest, err := loader.ComputeManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("4", 40)
	selected := []skills.SelectedSkill{
		{Name: "copywriting", Version: "1.0.0"},
		{Name: "emails", Version: "1.0.0"},
		{Name: "launch", Version: "1.0.0"},
		{Name: "product-marketing", Version: "1.0.0"},
		{Name: "social", Version: "1.0.0"},
	}
	if err := skills.WriteLock(loader.LockPath, skills.Lock{
		Distribution: skills.VendoredDistribution, Repository: "https://example.test/skills.git",
		Ref: commit, Commit: commit, RepositoryVersion: "1.0.0", SelectedSkills: selected,
		UpstreamManifestSHA256: strings.Repeat("a", 64), VendoredManifestSHA256: manifest,
	}); err != nil {
		t.Fatal(err)
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
