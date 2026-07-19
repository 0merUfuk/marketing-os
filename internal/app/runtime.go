package app

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/omerufuk/marketing-os/internal/config"
	gh "github.com/omerufuk/marketing-os/internal/github"
	"github.com/omerufuk/marketing-os/internal/llm"
	"github.com/omerufuk/marketing-os/internal/products"
	"github.com/omerufuk/marketing-os/internal/skills"
	"github.com/omerufuk/marketing-os/internal/state"
	"github.com/omerufuk/marketing-os/internal/workflows"
	"github.com/spf13/cobra"
)

type globalFlags struct {
	configPath string
	json       bool
	dryRun     bool
	verbose    bool
}

type runtime struct {
	Config    config.Config
	Store     *state.Store
	Workspace *products.Workspace
	Skills    *skills.Loader
	Logger    *slog.Logger
}

func openRuntime(cmd *cobra.Command, flags *globalFlags) (*runtime, error) {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return nil, err
	}
	level := slog.LevelInfo
	if flags.verbose || strings.EqualFold(cfg.Logging.Level, "debug") {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level}))
	store, err := state.Open(cmd.Context(), cfg.Database.Path)
	if err != nil {
		return nil, err
	}
	if cfg.Safety.GlobalKillSwitch {
		if err := store.SetKillSwitch(cmd.Context(), true, "enabled in configuration"); err != nil {
			store.Close()
			return nil, err
		}
	}
	return &runtime{
		Config: cfg, Store: store, Workspace: products.NewWorkspace(cfg.Workspace.ProductsPath),
		Skills: skills.NewLoader(cfg.Skills.RepositoryPath, cfg.Skills.LockFile), Logger: logger,
	}, nil
}

func (r *runtime) Close() error {
	if r == nil || r.Store == nil {
		return nil
	}
	return r.Store.Close()
}

func (r *runtime) model() (llm.ModelClient, error) {
	apiKey := ""
	var err error
	if strings.TrimSpace(r.Config.LLM.APIKeyEnv) != "" {
		apiKey, err = config.SecretFromEnv(r.Config.LLM.APIKeyEnv)
		if err != nil {
			return nil, err
		}
	}
	return llm.NewOpenAICompatible(llm.Options{
		Provider: r.Config.LLM.Provider, BaseURL: r.Config.LLM.BaseURL, APIKey: apiKey,
		Model: r.Config.LLM.Model, Timeout: time.Duration(r.Config.LLM.Timeout) * time.Second,
		MaxRetries: r.Config.LLM.MaxRetries, MaxCostUSD: r.Config.LLM.MaxCostPerRunUSD, MaxInputTokens: r.Config.LLM.MaxInputTokens,
		InputCostPerMillionUSD:  r.Config.LLM.InputCostPerMillionUSD,
		OutputCostPerMillionUSD: r.Config.LLM.OutputCostPerMillionUSD,
	})
}

func (r *runtime) github(requireToken bool) (*gh.Client, error) {
	token := ""
	if strings.TrimSpace(r.Config.GitHub.TokenEnv) != "" {
		token = os.Getenv(r.Config.GitHub.TokenEnv)
		if requireToken && token == "" {
			return nil, errors.New("required GitHub token environment variable is not set")
		}
	}
	return gh.NewClient(gh.Options{BaseURL: r.Config.GitHub.APIBaseURL, Token: token, Timeout: time.Duration(r.Config.GitHub.Timeout) * time.Second, MaxRetries: r.Config.GitHub.MaxRetries})
}

func (r *runtime) releaseWorkflow(model llm.ModelClient, github *gh.Client) *workflows.ReleaseWorkflow {
	return &workflows.ReleaseWorkflow{
		Store: r.Store, GitHub: github, Skills: r.Skills, Model: model, Workspace: r.Workspace,
		ApprovalRepository: r.Config.GitHub.ApprovalRepository, ApprovalLabels: r.Config.GitHub.ApprovalLabels,
		MaxOutputTokens: r.Config.LLM.MaxOutputTokens, MaxRepairAttempts: r.Config.LLM.MaxRepairAttempts,
		MarketabilityThreshold: 60, Secrets: r.secretValues(), Logger: r.Logger,
	}
}

func (r *runtime) secretValues() []string {
	values := make([]string, 0, 2)
	for _, name := range []string{r.Config.LLM.APIKeyEnv, r.Config.GitHub.TokenEnv} {
		if name != "" {
			if value := os.Getenv(name); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}
