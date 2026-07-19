package approvals

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/omerufuk/marketing-os/internal/skillruntime"
)

var approvalIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type EvidenceSummary struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

type IssueInput struct {
	ApprovalID       string
	ProductID        string
	ProductName      string
	WorkflowID       string
	RunID            string
	TriggerID        string
	ReleaseTitle     string
	ReleaseURL       string
	Evidence         []EvidenceSummary
	Result           skillruntime.ReleaseResult
	EstimatedCostUSD float64
	CreatedAt        time.Time
}

func RenderIssue(input IssueInput) (title, body, marker string, err error) {
	if !approvalIDPattern.MatchString(input.ApprovalID) {
		return "", "", "", errors.New("approval id is invalid")
	}
	if strings.TrimSpace(input.ProductName) == "" || strings.TrimSpace(input.ReleaseTitle) == "" || input.Result.Action != "stage_for_approval" || !input.Result.RequiresHumanApproval {
		return "", "", "", errors.New("complete staged approval input is required")
	}
	marker = fmt.Sprintf("<!-- marketing-os-approval:%s -->", input.ApprovalID)
	title = fmt.Sprintf("Marketing Approval: %s — %s", safe(input.ProductName), safe(input.ReleaseTitle))
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n# Marketing Approval: %s\n\n", marker, safe(input.ReleaseTitle))
	fmt.Fprintf(&b, "## Trigger\n\nNew GitHub release detected.\n\n- Product: `%s`\n- Workflow: `%s`\n- Run: `%s`\n- Trigger: `%s`\n- Release: %s\n- Created: `%s`\n\n",
		safe(input.ProductID), safe(input.WorkflowID), safe(input.RunID), safe(input.TriggerID), markdownLink("source", input.ReleaseURL), input.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "## Marketability\n\n**Score: %d/100**\n\n**Classification:** `%s`\n\n%s\n\n",
		input.Result.Marketability.Score, safe(input.Result.ReleaseClassification), quote(input.Result.Marketability.Reason))
	b.WriteString("## Evidence\n\n")
	for _, evidence := range input.Evidence {
		fmt.Fprintf(&b, "### `%s` — %s\n\n%s\n\n", safe(evidence.ID), safe(evidence.Source), quote(evidence.Content))
	}
	fmt.Fprintf(&b, "## Customer Value\n\n%s\n\n**Evidence:** %s\n\n", quote(input.Result.CustomerValue.Summary), codeList(input.Result.CustomerValue.EvidenceIDs))
	if len(input.Result.Audience) > 0 {
		fmt.Fprintf(&b, "**Audience:** %s\n\n", safe(strings.Join(input.Result.Audience, ", ")))
	}
	order := map[string]int{"release_summary": 0, "changelog": 1, "linkedin": 2, "x": 3, "email": 4}
	assets := append([]skillruntime.AssetDraft(nil), input.Result.Assets...)
	sort.SliceStable(assets, func(i, j int) bool { return order[assets[i].Channel] < order[assets[j].Channel] })
	for _, asset := range assets {
		fmt.Fprintf(&b, "## %s\n\n", channelHeading(asset.Channel))
		if asset.Subject != "" {
			fmt.Fprintf(&b, "**Subject:** %s\n\n", safe(asset.Subject))
		}
		fmt.Fprintf(&b, "%s\n\n**Evidence:** %s\n\n", quote(asset.Content), codeList(asset.EvidenceIDs))
	}
	b.WriteString("## Unsupported Claims\n\n")
	writeList(&b, input.Result.UnsupportedClaims, "None reported.")
	b.WriteString("\n## Warnings\n\n")
	writeList(&b, input.Result.Warnings, "No additional warnings.")
	fmt.Fprintf(&b, "\n## Estimated Model Cost\n\n`$%.4f USD`\n\n", input.EstimatedCostUSD)
	b.WriteString("## Approval\n\nNo content is published or sent by this MVP. A human reviewer may record one of these decisions in the issue:\n\n- Approve\n- Request changes\n- Reject\n")
	return title, b.String(), marker, nil
}

func channelHeading(channel string) string {
	switch channel {
	case "release_summary":
		return "Customer-Facing Release Summary"
	case "changelog":
		return "Changelog Copy"
	case "linkedin":
		return "LinkedIn Draft"
	case "x":
		return "X Draft"
	case "email":
		return "Email Draft"
	default:
		return "Draft: " + safe(channel)
	}
}

func safe(value string) string {
	value = strings.ReplaceAll(value, "@", "@\u200b")
	value = strings.ReplaceAll(value, "<!-- marketing-os-approval:", "&lt;!-- marketing-os-approval:")
	return value
}

func quote(value string) string {
	lines := strings.Split(safe(value), "\n")
	for i := range lines {
		lines[i] = "> " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func codeList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = "`" + strings.ReplaceAll(safe(value), "`", "") + "`"
	}
	return strings.Join(quoted, ", ")
}

func writeList(builder *strings.Builder, values []string, empty string) {
	if len(values) == 0 {
		builder.WriteString(empty + "\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(builder, "- %s\n", safe(value))
	}
}

func markdownLink(label, target string) string {
	if target == "" {
		return "not provided"
	}
	target = strings.ReplaceAll(safe(target), ")", "%29")
	return fmt.Sprintf("[%s](%s)", label, target)
}
