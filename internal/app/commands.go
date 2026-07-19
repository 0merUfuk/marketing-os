package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/omerufuk/marketing-os/internal/domain"
	"github.com/omerufuk/marketing-os/internal/productcontext"
	"github.com/omerufuk/marketing-os/internal/scheduler"
	"github.com/omerufuk/marketing-os/internal/workflows"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	flags := &globalFlags{}
	defaultConfig := os.Getenv("MARKETING_OS_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "./config.yaml"
	}
	root := &cobra.Command{Use: "marketing-os", Short: "Local-first, evidence-grounded marketing workflow orchestrator", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&flags.configPath, "config", defaultConfig, "configuration YAML path")
	root.PersistentFlags().BoolVar(&flags.json, "json", false, "emit machine-readable JSON")
	root.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "run without persistent workflow state or external writes")
	root.PersistentFlags().BoolVarP(&flags.verbose, "verbose", "v", false, "enable debug logging")
	root.AddCommand(productCommand(flags), contextCommand(flags), skillsCommand(flags), workflowCommand(flags), approvalsCommand(flags), runsCommand(flags), schedulerCommand(flags), killCommand(flags, true), killCommand(flags, false))
	return root
}

func productCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "product", Short: "Register and inspect products"}
	var product domain.Product
	add := &cobra.Command{Use: "add", Short: "Register a product", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := product.Validate(); err != nil {
			return err
		}
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		if err := r.Workspace.Initialize(product); err != nil {
			return err
		}
		if err := r.Store.AddProduct(cmd.Context(), product); err != nil {
			return err
		}
		definition := domain.ReleaseToMarketingDefinition(product.ID)
		definition.Enabled = false
		if err := r.Store.UpsertWorkflow(cmd.Context(), definition); err != nil {
			return err
		}
		stored, err := r.Store.GetProduct(cmd.Context(), product.ID)
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, stored, fmt.Sprintf("registered product %s (%s)\n", stored.Name, stored.ID))
	}}
	add.Flags().StringVar(&product.ID, "id", "", "lowercase product slug (required)")
	add.Flags().StringVar(&product.Name, "name", "", "product name (required)")
	add.Flags().StringVar(&product.Repository, "repository", "", "GitHub owner/repository")
	add.Flags().Int64Var(&product.RepositoryID, "repository-id", 0, "stable GitHub repository ID")
	add.Flags().StringVar(&product.LocalRepository, "local-repository", "", "local repository path")
	add.Flags().StringVar(&product.Website, "website", "", "website URL")
	add.Flags().StringVar(&product.DocumentationURL, "docs", "", "documentation URL")
	add.Flags().StringVar(&product.PricingURL, "pricing", "", "pricing URL")
	add.Flags().StringVar(&product.ChangelogURL, "changelog", "", "changelog URL")
	add.Flags().StringVar(&product.ProductType, "product-type", "", "product type (required)")
	add.Flags().StringVar(&product.PrimaryConversionAction, "conversion", "", "primary conversion action (required)")
	add.Flags().StringVar(&product.DefaultLanguage, "language", "en", "default language")
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		items, err := r.Store.ListProducts(cmd.Context())
		if err != nil {
			return err
		}
		if flags.json {
			return emit(cmd, true, items, "")
		}
		if len(items) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "no products registered")
			return err
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.ID, item.Name, item.Repository)
		}
		return nil
	}}
	inspect := &cobra.Command{Use: "inspect <product>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		item, err := r.Store.GetProduct(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, item, fmt.Sprintf("%s (%s)\nrepository: %s\nwebsite: %s\nconversion: %s\n", item.Name, item.ID, item.Repository, item.Website, item.PrimaryConversionAction))
	}}
	command.AddCommand(add, list, inspect)
	return command
}

func contextCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "context", Short: "Draft, approve, and inspect canonical product context"}
	draft := &cobra.Command{Use: "draft <product>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		model, err := r.model()
		if err != nil {
			return err
		}
		github, err := r.github(false)
		if err != nil {
			return err
		}
		service := productcontext.Service{Store: r.Store, Workspace: r.Workspace, Skills: r.Skills, Model: model, GitHub: github, MaxOutputTokens: r.Config.LLM.MaxOutputTokens, MaxRepairAttempts: r.Config.LLM.MaxRepairAttempts, Secrets: r.secretValues()}
		result, err := service.Draft(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, result, fmt.Sprintf("created unapproved context draft v%d for %s\n", result.Version, result.ProductID))
	}}
	var showVersion int
	show := &cobra.Command{Use: "show <product>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		versions, err := r.Store.ListContextVersions(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return errors.New("no context versions found")
		}
		selected := versions[0]
		if showVersion > 0 {
			found := false
			for _, item := range versions {
				if item.Version == showVersion {
					selected = item
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("context version %d not found", showVersion)
			}
		}
		if flags.json {
			return emit(cmd, true, selected, "")
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), selected.Content)
		return err
	}}
	show.Flags().IntVar(&showVersion, "version", 0, "specific context version")
	actor := os.Getenv("USER")
	approve := &cobra.Command{Use: "approve <product> <version>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		version, err := strconv.Atoi(args[1])
		if err != nil || version <= 0 {
			return errors.New("version must be a positive integer")
		}
		if strings.TrimSpace(actor) == "" {
			return errors.New("approval actor is required")
		}
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		service := productcontext.Service{Store: r.Store, Workspace: r.Workspace}
		result, err := service.Approve(cmd.Context(), args[0], version, actor)
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, result, fmt.Sprintf("approved context v%d for %s by %s\n", version, args[0], actor))
	}}
	approve.Flags().StringVar(&actor, "actor", actor, "approval actor")
	versions := &cobra.Command{Use: "versions <product>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		items, err := r.Store.ListContextVersions(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if flags.json {
			return emit(cmd, true, items, "")
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "v%d\t%s\t%s\n", item.Version, item.Status, item.CreatedAt.Format(time.RFC3339))
		}
		return nil
	}}
	command.AddCommand(draft, show, approve, versions)
	return command
}

func skillsCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "skills", Short: "Inspect and explicitly update pinned marketing skills"}
	status := &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		value, err := r.Skills.Status(cmd.Context())
		if err != nil {
			return err
		}
		text := fmt.Sprintf("commit: %s\nmanifest: %s\npin valid: %t\n", value.Lock.Commit, value.Lock.ManifestSHA256, value.PinValid)
		return emit(cmd, flags.json, value, text)
	}}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		items, err := r.Skills.Index(cmd.Context())
		if err != nil {
			return err
		}
		if flags.json {
			return emit(cmd, true, items, "")
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", item.Name, item.Version, item.Description)
		}
		return nil
	}}
	var ref, repository string
	update := &cobra.Command{Use: "update", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(ref) == "" {
			return errors.New("--ref is required")
		}
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		lock, err := r.Skills.Update(cmd.Context(), repository, ref)
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, lock, fmt.Sprintf("updated skills to %s (%s)\n", lock.Commit, lock.Ref))
	}}
	update.Flags().StringVar(&ref, "ref", "", "exact commit, tag, or release ref (required)")
	update.Flags().StringVar(&repository, "repository", "https://github.com/coreyhaines31/marketingskills.git", "skills repository URL")
	command.AddCommand(status, list, update)
	return command
}

func workflowCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "workflow", Short: "List, run, enable, or disable workflows"}
	list := &cobra.Command{Use: "list <product>", Short: "List product workflow definitions", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		items, err := r.Store.ListWorkflows(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if flags.json {
			return emit(cmd, true, items, "")
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\tenabled=%t\tcadence=%s\n", item.ID, item.Enabled, item.Cadence)
		}
		return nil
	}}
	var releaseID int64
	run := &cobra.Command{Use: "run <product> release-to-marketing", Short: "Run a workflow manually", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if args[1] != domain.ReleaseToMarketingWorkflowID {
			return fmt.Errorf("unsupported workflow %q", args[1])
		}
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		model, err := r.model()
		if err != nil {
			return err
		}
		github, err := r.github(!flags.dryRun)
		if err != nil {
			return err
		}
		outcome, err := r.releaseWorkflow(model, github).Run(cmd.Context(), args[0], workflows.RunOptions{TriggerType: "manual", ReleaseID: releaseID, DryRun: flags.dryRun})
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, outcome, fmt.Sprintf("run %s: %s action=%s approval=%s\n", outcome.RunID, outcome.Status, outcome.Action, outcome.ApprovalID))
	}}
	run.Flags().Int64Var(&releaseID, "release-id", 0, "specific GitHub release ID (default: latest published)")
	toggle := func(enabled bool) *cobra.Command {
		verb := "disable"
		if enabled {
			verb = "enable"
		}
		return &cobra.Command{Use: verb + " <product> release-to-marketing", Short: verb + " a product workflow", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] != domain.ReleaseToMarketingWorkflowID {
				return fmt.Errorf("unsupported workflow %q", args[1])
			}
			r, err := openRuntime(cmd, flags)
			if err != nil {
				return err
			}
			defer r.Close()
			if err := r.Store.SetWorkflowEnabled(cmd.Context(), args[0], args[1], enabled); err != nil {
				return err
			}
			return emit(cmd, flags.json, map[string]any{"product_id": args[0], "workflow_id": args[1], "enabled": enabled}, fmt.Sprintf("%sd %s for %s\n", verb, args[1], args[0]))
		}}
	}
	command.AddCommand(list, run, toggle(true), toggle(false))
	return command
}

func approvalsCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "approvals", Short: "Inspect staged human approvals"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		items, err := r.Store.ListApprovals(cmd.Context(), 100)
		if err != nil {
			return err
		}
		if flags.json {
			return emit(cmd, true, items, "")
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.ID, item.ProductID, item.Status, item.IssueURL)
		}
		return nil
	}}
	show := &cobra.Command{Use: "show <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		approval, err := r.Store.GetApproval(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		assets, err := r.Store.AssetsForApproval(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		value := map[string]any{"approval": approval, "assets": assets}
		if flags.json {
			return emit(cmd, true, value, "")
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\nstatus: %s\nissue: %s\n\n%s\n", approval.ID, approval.Status, approval.IssueURL, approval.IssueBody)
		return err
	}}
	command.AddCommand(list, show)
	return command
}

func runsCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "runs", Short: "Inspect workflow runs"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		items, err := r.Store.ListRuns(cmd.Context(), 100)
		if err != nil {
			return err
		}
		if flags.json {
			return emit(cmd, true, items, "")
		}
		for _, item := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.ID, item.ProductID, item.WorkflowID, item.Status)
		}
		return nil
	}}
	show := &cobra.Command{Use: "show <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		item, err := r.Store.GetRun(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return emit(cmd, flags.json, item, fmt.Sprintf("%s\nproduct: %s\nworkflow: %s\nstatus: %s\nattempt: %d\nerror: %s\n", item.ID, item.ProductID, item.WorkflowID, item.Status, item.Attempt, item.ErrorMessage))
	}}
	command.AddCommand(list, show)
	return command
}

func schedulerCommand(flags *globalFlags) *cobra.Command {
	command := &cobra.Command{Use: "scheduler", Short: "Run cron-style workflow polling"}
	start := &cobra.Command{Use: "start", Short: "Start the workflow scheduler", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		if !r.Config.Scheduler.Enabled {
			return errors.New("scheduler is disabled in configuration")
		}
		model, err := r.model()
		if err != nil {
			return err
		}
		github, err := r.github(true)
		if err != nil {
			return err
		}
		engine := scheduler.New(r.Store, r.releaseWorkflow(model, github), scheduler.Options{RetryDelay: time.Duration(r.Config.Scheduler.RetryDelaySeconds) * time.Second, MaxRetries: r.Config.Scheduler.MaxRetries, Logger: r.Logger})
		return engine.Start(cmd.Context())
	}}
	command.AddCommand(start)
	return command
}

func killCommand(flags *globalFlags, stop bool) *cobra.Command {
	name := "start-all"
	short := "Clear the global scheduler kill switch"
	enabled := false
	reason := "operator resumed scheduled workflows"
	if stop {
		name = "stop-all"
		short = "Activate the global scheduler kill switch"
		enabled = true
		reason = "operator stopped all scheduled workflows"
	}
	return &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, err := openRuntime(cmd, flags)
		if err != nil {
			return err
		}
		defer r.Close()
		if err := r.Store.SetKillSwitch(cmd.Context(), enabled, reason); err != nil {
			return err
		}
		return emit(cmd, flags.json, map[string]any{"global_kill_switch": enabled, "reason": reason}, fmt.Sprintf("global kill switch: %t (%s)\n", enabled, reason))
	}}
}

func emit(cmd *cobra.Command, asJSON bool, value any, text string) error {
	if asJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), text)
	return err
}
