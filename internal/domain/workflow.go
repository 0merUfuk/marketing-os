package domain

import (
	"errors"
	"math"
	"strings"
	"time"
)

const ReleaseToMarketingWorkflowID = "release-to-marketing"

type RunStatus string

const (
	RunPending          RunStatus = "pending"
	RunRunning          RunStatus = "running"
	RunNoAction         RunStatus = "no_action"
	RunAwaitingApproval RunStatus = "awaiting_approval"
	RunCompleted        RunStatus = "completed"
	RunFailed           RunStatus = "failed"
	RunCancelled        RunStatus = "cancelled"
	RunBlocked          RunStatus = "blocked"
	RunKilled           RunStatus = "killed"
)

func CanTransition(from, to RunStatus) bool {
	allowed := map[RunStatus]map[RunStatus]struct{}{
		RunPending: {RunRunning: {}, RunBlocked: {}, RunKilled: {}, RunCancelled: {}},
		RunRunning: {RunNoAction: {}, RunAwaitingApproval: {}, RunFailed: {}, RunKilled: {}, RunCancelled: {}},
	}
	_, ok := allowed[from][to]
	return ok
}

type WorkflowDefinition struct {
	ID                  string        `json:"id"`
	ProductID           string        `json:"product_id"`
	Trigger             string        `json:"trigger"`
	Cadence             string        `json:"check_cadence"`
	ActivationCondition string        `json:"activation_condition"`
	Purpose             string        `json:"purpose"`
	PrimarySkill        string        `json:"primary_skill"`
	SupportingSkills    []string      `json:"supporting_skills"`
	RequiredInputs      []string      `json:"required_inputs"`
	OrderedSteps        []string      `json:"ordered_steps"`
	SelfCheck           string        `json:"self_check"`
	StateRequirements   []string      `json:"state_requirements"`
	DedupeKeyTemplate   string        `json:"dedupe_key"`
	Cooldown            time.Duration `json:"cooldown"`
	StopCondition       string        `json:"stop_condition"`
	ErrorBehavior       string        `json:"error_behavior"`
	OutputDestination   string        `json:"output_destination"`
	ApprovalPolicy      string        `json:"approval_policy"`
	MaxCostUSD          float64       `json:"max_cost_usd"`
	Timeout             time.Duration `json:"timeout"`
	Enabled             bool          `json:"enabled"`
	AllowOverlap        bool          `json:"allow_overlap"`
}

func ReleaseToMarketingDefinition(productID string) WorkflowDefinition {
	return WorkflowDefinition{
		ID: ReleaseToMarketingWorkflowID, ProductID: productID,
		Trigger: "github_release_poll", Cadence: "0 */6 * * *",
		ActivationCondition: "published release is materially marketable and score meets threshold",
		Purpose:             "turn a customer-visible GitHub release into evidence-backed approval drafts",
		PrimarySkill:        "launch", SupportingSkills: []string{"copywriting", "social", "emails"},
		RequiredInputs:    []string{"approved_product_context", "github_release", "pinned_skills", "llm", "github_approval_repository"},
		OrderedSteps:      []string{"fetch", "dedupe", "capture_evidence", "assess", "generate", "validate", "self_check", "stage_or_exit", "audit"},
		SelfCheck:         "all factual fields cite same-product evidence; all channels and human gate validate",
		StateRequirements: []string{"durable_dedupe", "workflow_lease", "cursor_after_success"},
		DedupeKeyTemplate: "sha256(product_id,workflow_id,repository_id,release_id)",
		Cooldown:          0, StopCondition: "no published release, duplicate release, no_action, or awaiting approval",
		ErrorBehavior:     "fail without completing dedupe or advancing cursor; retry after lease expiry",
		OutputDestination: "github_issue", ApprovalPolicy: "human_required_no_execution",
		MaxCostUSD: 1.0, Timeout: 2 * time.Minute, Enabled: false, AllowOverlap: false,
	}
}

func (w WorkflowDefinition) Validate() error {
	if err := ValidateProductID(w.ProductID); err != nil {
		return err
	}
	required := []string{w.ID, w.Trigger, w.Cadence, w.ActivationCondition, w.Purpose,
		w.PrimarySkill, w.SelfCheck, w.DedupeKeyTemplate, w.StopCondition,
		w.ErrorBehavior, w.OutputDestination, w.ApprovalPolicy}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return errors.New("workflow is missing a required deterministic or safety field")
		}
	}
	if len(w.RequiredInputs) == 0 || len(w.OrderedSteps) == 0 || len(w.StateRequirements) == 0 {
		return errors.New("workflow inputs, ordered steps, and state requirements are required")
	}
	if w.Timeout <= 0 || w.MaxCostUSD <= 0 || math.IsNaN(w.MaxCostUSD) || math.IsInf(w.MaxCostUSD, 0) {
		return errors.New("workflow timeout and cost limit must be positive")
	}
	if w.OutputDestination != "github_issue" || w.ApprovalPolicy != "human_required_no_execution" {
		return errors.New("MVP workflows may only stage GitHub Issue approvals")
	}
	return nil
}

type WorkflowRun struct {
	ID               string            `json:"id"`
	ProductID        string            `json:"product_id"`
	WorkflowID       string            `json:"workflow_id"`
	TriggerID        string            `json:"trigger_id"`
	TriggerType      string            `json:"trigger_type"`
	DedupeKey        string            `json:"dedupe_key"`
	Status           RunStatus         `json:"status"`
	Attempt          int               `json:"attempt"`
	RepositoryCommit string            `json:"repository_commit,omitempty"`
	SkillVersions    map[string]string `json:"skill_versions"`
	ContextVersion   int               `json:"context_version,omitempty"`
	ModelProvider    string            `json:"model_provider,omitempty"`
	ModelName        string            `json:"model_name,omitempty"`
	EvidenceIDs      []string          `json:"evidence_ids"`
	StartedAt        time.Time         `json:"started_at"`
	FinishedAt       *time.Time        `json:"finished_at,omitempty"`
	InputTokens      int               `json:"input_tokens"`
	OutputTokens     int               `json:"output_tokens"`
	EstimatedCostUSD float64           `json:"estimated_cost_usd"`
	OutputHash       string            `json:"output_hash,omitempty"`
	ApprovalID       string            `json:"approval_id,omitempty"`
	ErrorCode        string            `json:"error_code,omitempty"`
	ErrorMessage     string            `json:"error_message,omitempty"`
	DryRun           bool              `json:"dry_run"`
}
