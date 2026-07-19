package config

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesSafeDefaultsAndResolvesNoSecrets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
database:
  driver: sqlite
  path: ./data/test.db
skills:
  repository_path: ./.skills/marketingskills
  lock_file: ./skills.lock.yaml
llm:
  provider: openai-compatible
  base_url: https://llm.example/v1
  api_key_env: TEST_LLM_KEY
  model: test-model
github:
  api_base_url: https://api.github.test
  token_env: TEST_GITHUB_TOKEN
  approval_repository: acme/marketing-approvals
safety:
  publishing_enabled: false
  sending_enabled: false
  spending_enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.Timeout != 90 || cfg.LLM.MaxRetries != 2 {
		t.Fatalf("unsafe/default LLM bounds: %+v", cfg.LLM)
	}
	if cfg.LLM.MaxCostPerRunUSD != 1.0 {
		t.Fatalf("max cost = %v", cfg.LLM.MaxCostPerRunUSD)
	}
	if cfg.Safety.PublishingEnabled || cfg.Safety.SendingEnabled || cfg.Safety.SpendingEnabled {
		t.Fatal("tier-2 capabilities must default disabled")
	}
}

func TestLoadRejectsUnknownFieldsIncludingInlineToken(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `
database: {driver: sqlite, path: ./test.db}
skills: {repository_path: ./.skills/marketingskills, lock_file: ./skills.lock.yaml}
llm: {provider: mock, model: mock}
github:
  token: should-never-be-here
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted an inline token/unknown field")
	}
}

func TestValidateRejectsIncompleteOrUnsafeServiceConfiguration(t *testing.T) {
	tests := map[string]func(*Config){
		"empty model":              func(cfg *Config) { cfg.LLM.Model = "" },
		"non-loopback LLM HTTP":    func(cfg *Config) { cfg.LLM.BaseURL = "http://llm.example/v1" },
		"LLM URL credentials":      func(cfg *Config) { cfg.LLM.BaseURL = "https://user:pass@llm.example/v1" },
		"non-loopback GitHub HTTP": func(cfg *Config) { cfg.GitHub.APIBaseURL = "http://github.example" },
		"unsafe approval repo":     func(cfg *Config) { cfg.GitHub.ApprovalRepository = "../escape" },
		"invalid logging level":    func(cfg *Config) { cfg.Logging.Level = "trace" },
		"non-finite input price":   func(cfg *Config) { cfg.LLM.InputCostPerMillionUSD = math.NaN() },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			cfg.LLM.Model = "model"
			cfg.GitHub.ApprovalRepository = "acme/approvals"
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() accepted unsafe or incomplete service configuration")
			}
		})
	}
}

func TestValidateRejectsTier2Enablement(t *testing.T) {
	cfg := Default()
	cfg.Database.Path = "test.db"
	cfg.Skills.RepositoryPath = ".skills/marketingskills"
	cfg.Skills.LockFile = "skills.lock.yaml"
	cfg.Safety.PublishingEnabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted publishing in an MVP build")
	}
}
