package domain

import "time"

type ApprovalStatus string

const (
	ApprovalCreating         ApprovalStatus = "creating"
	ApprovalAwaiting         ApprovalStatus = "awaiting_approval"
	ApprovalApproved         ApprovalStatus = "approved"
	ApprovalChangesRequested ApprovalStatus = "changes_requested"
	ApprovalRejected         ApprovalStatus = "rejected"
	ApprovalFailed           ApprovalStatus = "failed"
)

type GeneratedAsset struct {
	ID          string    `json:"id"`
	ProductID   string    `json:"product_id,omitempty"`
	WorkflowID  string    `json:"workflow_id,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	ApprovalID  string    `json:"approval_id,omitempty"`
	Channel     string    `json:"channel"`
	Subject     string    `json:"subject,omitempty"`
	Content     string    `json:"content"`
	EvidenceIDs []string  `json:"evidence_ids"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

type Approval struct {
	ID                  string         `json:"id"`
	ProductID           string         `json:"product_id"`
	WorkflowID          string         `json:"workflow_id"`
	DedupeKey           string         `json:"dedupe_key"`
	TriggerID           string         `json:"trigger_id"`
	RunID               string         `json:"run_id"`
	Status              ApprovalStatus `json:"status"`
	EvidenceSummaryJSON string         `json:"evidence_summary_json"`
	ProposedActionJSON  string         `json:"proposed_action_json"`
	RisksJSON           string         `json:"risks_json"`
	WarningsJSON        string         `json:"warnings_json"`
	EstimatedCostUSD    float64        `json:"estimated_cost_usd"`
	IssueRepository     string         `json:"issue_repository"`
	IssueMarker         string         `json:"issue_marker"`
	IssueRequestHash    string         `json:"issue_request_hash"`
	IssueTitle          string         `json:"issue_title"`
	IssueBody           string         `json:"issue_body"`
	IssueID             int64          `json:"issue_id,omitempty"`
	IssueNumber         int            `json:"issue_number,omitempty"`
	IssueURL            string         `json:"issue_url,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}
