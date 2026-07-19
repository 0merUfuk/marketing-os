package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the complete typed application configuration. Secrets are referenced
// only by environment-variable name and are never accepted inline.
type Config struct {
	Database  DatabaseConfig  `yaml:"database"`
	Workspace WorkspaceConfig `yaml:"workspace"`
	Skills    SkillsConfig    `yaml:"skills"`
	LLM       LLMConfig       `yaml:"llm"`
	GitHub    GitHubConfig    `yaml:"github"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Safety    SafetyConfig    `yaml:"safety"`
	Logging   LoggingConfig   `yaml:"logging"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	Path   string `yaml:"path"`
}

type WorkspaceConfig struct {
	ProductsPath string `yaml:"products_path"`
}

type SkillsConfig struct {
	RepositoryPath string `yaml:"repository_path"`
	LockFile       string `yaml:"lock_file"`
}

type LLMConfig struct {
	Provider                string  `yaml:"provider"`
	BaseURL                 string  `yaml:"base_url"`
	APIKeyEnv               string  `yaml:"api_key_env"`
	Model                   string  `yaml:"model"`
	Timeout                 int     `yaml:"timeout_seconds"`
	MaxRetries              int     `yaml:"max_retries"`
	MaxRepairAttempts       int     `yaml:"max_repair_attempts"`
	MaxInputTokens          int     `yaml:"max_input_tokens"`
	MaxOutputTokens         int     `yaml:"max_output_tokens"`
	MaxCostPerRunUSD        float64 `yaml:"max_cost_per_run_usd"`
	InputCostPerMillionUSD  float64 `yaml:"input_cost_per_million_usd"`
	OutputCostPerMillionUSD float64 `yaml:"output_cost_per_million_usd"`
}

type GitHubConfig struct {
	APIBaseURL         string   `yaml:"api_base_url"`
	TokenEnv           string   `yaml:"token_env"`
	ApprovalRepository string   `yaml:"approval_repository"`
	ApprovalLabels     []string `yaml:"approval_labels"`
	Timeout            int      `yaml:"timeout_seconds"`
	MaxRetries         int      `yaml:"max_retries"`
}

type SchedulerConfig struct {
	Enabled           bool `yaml:"enabled"`
	RetryDelaySeconds int  `yaml:"retry_delay_seconds"`
	MaxRetries        int  `yaml:"max_retries"`
}

type SafetyConfig struct {
	GlobalKillSwitch  bool `yaml:"global_kill_switch"`
	PublishingEnabled bool `yaml:"publishing_enabled"`
	SendingEnabled    bool `yaml:"sending_enabled"`
	SpendingEnabled   bool `yaml:"spending_enabled"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func Default() Config {
	return Config{
		Database:  DatabaseConfig{Driver: "sqlite", Path: "./data/marketing-os.db"},
		Workspace: WorkspaceConfig{ProductsPath: "./products"},
		Skills:    SkillsConfig{RepositoryPath: "./.skills/marketingskills", LockFile: "./skills.lock.yaml"},
		LLM: LLMConfig{
			Provider: "openai-compatible", BaseURL: "https://api.openai.com/v1",
			APIKeyEnv: "OPENAI_API_KEY", Timeout: 90, MaxRetries: 2,
			MaxRepairAttempts: 1, MaxInputTokens: 30000, MaxOutputTokens: 5000,
			MaxCostPerRunUSD: 1.00,
		},
		GitHub: GitHubConfig{
			APIBaseURL: "https://api.github.com", TokenEnv: "GITHUB_TOKEN",
			ApprovalLabels: []string{"marketing-approval"}, Timeout: 30, MaxRetries: 2,
		},
		Scheduler: SchedulerConfig{Enabled: true, RetryDelaySeconds: 30, MaxRetries: 1},
		Logging:   LoggingConfig{Level: "info"},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	cfg.resolveRelativePaths(filepath.Dir(path))
	return cfg, nil
}

func (c *Config) resolveRelativePaths(base string) {
	c.Database.Path = resolvePath(base, c.Database.Path)
	c.Workspace.ProductsPath = resolvePath(base, c.Workspace.ProductsPath)
	c.Skills.RepositoryPath = resolvePath(base, c.Skills.RepositoryPath)
	c.Skills.LockFile = resolvePath(base, c.Skills.LockFile)
}

func resolvePath(base, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(base, path))
}

func (c Config) Validate() error {
	var errs []error
	if c.Database.Driver != "sqlite" {
		errs = append(errs, fmt.Errorf("database.driver must be sqlite for the MVP"))
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		errs = append(errs, errors.New("database.path is required"))
	}
	if strings.TrimSpace(c.Workspace.ProductsPath) == "" {
		errs = append(errs, errors.New("workspace.products_path is required"))
	}
	if strings.TrimSpace(c.Skills.RepositoryPath) == "" || strings.TrimSpace(c.Skills.LockFile) == "" {
		errs = append(errs, errors.New("skills.repository_path and skills.lock_file are required"))
	}
	if strings.TrimSpace(c.LLM.Provider) == "" || strings.TrimSpace(c.LLM.Model) == "" {
		errs = append(errs, errors.New("llm.provider and llm.model are required"))
	}
	llmLoopback, llmURLErr := validateServiceURL(c.LLM.BaseURL, "llm.base_url")
	if llmURLErr != nil {
		errs = append(errs, llmURLErr)
	}
	if strings.TrimSpace(c.LLM.APIKeyEnv) == "" && !llmLoopback {
		errs = append(errs, errors.New("llm.api_key_env may be empty only for a loopback endpoint"))
	}
	if c.LLM.Timeout <= 0 || c.LLM.MaxRetries < 0 || c.LLM.MaxRetries > 5 || c.LLM.MaxRepairAttempts < 0 || c.LLM.MaxRepairAttempts > 2 {
		errs = append(errs, errors.New("LLM timeout/retry bounds are invalid"))
	}
	if c.LLM.MaxInputTokens <= 0 || c.LLM.MaxOutputTokens <= 0 || c.LLM.MaxCostPerRunUSD <= 0 {
		errs = append(errs, errors.New("LLM token and cost limits must be positive"))
	}
	if !finiteNonNegative(c.LLM.InputCostPerMillionUSD) || !finiteNonNegative(c.LLM.OutputCostPerMillionUSD) || math.IsNaN(c.LLM.MaxCostPerRunUSD) || math.IsInf(c.LLM.MaxCostPerRunUSD, 0) {
		errs = append(errs, errors.New("LLM pricing and cost limits must be finite and non-negative"))
	}
	if _, githubURLErr := validateServiceURL(c.GitHub.APIBaseURL, "github.api_base_url"); githubURLErr != nil {
		errs = append(errs, githubURLErr)
	}
	if strings.TrimSpace(c.GitHub.TokenEnv) == "" {
		errs = append(errs, errors.New("github.token_env is required"))
	}
	if !repositoryPattern.MatchString(c.GitHub.ApprovalRepository) || strings.Contains(c.GitHub.ApprovalRepository, "..") {
		errs = append(errs, errors.New("github.approval_repository must use a safe owner/repository value"))
	}
	if c.GitHub.Timeout <= 0 || c.GitHub.MaxRetries < 0 || c.GitHub.MaxRetries > 5 {
		errs = append(errs, errors.New("GitHub timeout/retry bounds are invalid"))
	}
	if c.Scheduler.RetryDelaySeconds <= 0 || c.Scheduler.MaxRetries < 0 || c.Scheduler.MaxRetries > 5 {
		errs = append(errs, errors.New("scheduler retry bounds are invalid"))
	}
	if c.Safety.PublishingEnabled || c.Safety.SendingEnabled || c.Safety.SpendingEnabled {
		errs = append(errs, errors.New("publishing, sending, and spending cannot be enabled in the MVP"))
	}
	if level := strings.ToLower(strings.TrimSpace(c.Logging.Level)); level != "debug" && level != "info" && level != "warn" && level != "error" {
		errs = append(errs, errors.New("logging.level must be debug, info, warn, or error"))
	}
	return errors.Join(errs...)
}

func validateServiceURL(raw, field string) (bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false, fmt.Errorf("%s must be an absolute URL without embedded credentials", field)
	}
	if parsed.Scheme == "https" {
		return false, nil
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || ip != nil && ip.IsLoopback()
	if parsed.Scheme == "http" && loopback {
		return true, nil
	}
	return false, fmt.Errorf("%s must use HTTPS unless it is loopback", field)
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func SecretFromEnv(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("secret environment variable name is empty")
	}
	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("required secret environment variable %s is not set", name)
	}
	return value, nil
}
