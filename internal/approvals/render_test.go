package approvals

import (
	"strings"
	"testing"
	"time"

	"github.com/omerufuk/marketing-os/internal/skillruntime"
)

func TestRenderGitHubIssueContainsAuditSectionsMarkerAndSafeMentions(t *testing.T) {
	t.Parallel()
	input := IssueInput{
		ApprovalID: "approval-123", ProductID: "widget", ProductName: "Widget", WorkflowID: "release-to-marketing",
		RunID: "run-1", TriggerID: "github-release:42", ReleaseTitle: "Widget v1.4", ReleaseURL: "https://github.test/release/42",
		Evidence: []EvidenceSummary{{ID: "release-42", Source: "GitHub release", Content: "CSV export for @backend teams"}},
		Result: skillruntime.ReleaseResult{
			Action: "stage_for_approval", ReleaseClassification: "feature_launch",
			Marketability:     skillruntime.Marketability{Score: 82, Reason: "Customer-visible improvement."},
			Audience:          []string{"engineering managers"},
			CustomerValue:     skillruntime.GroundedText{Summary: "Export reports.", EvidenceIDs: []string{"release-42"}},
			Assets:            []skillruntime.AssetDraft{{Channel: "linkedin", Content: "Hello @everyone", EvidenceIDs: []string{"release-42"}}},
			UnsupportedClaims: []string{"No performance data"}, Warnings: []string{"Human review required"}, RequiresHumanApproval: true,
		},
		EstimatedCostUSD: 0.03, CreatedAt: time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC),
	}
	title, body, marker, err := RenderIssue(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Marketing Approval", "## Trigger", "## Marketability", "## Evidence", "## Customer Value", "## LinkedIn Draft", "## Warnings", "## Approval", marker} {
		if !strings.Contains(title+body, required) {
			t.Errorf("missing %q in rendered issue", required)
		}
	}
	if strings.Contains(body, "@everyone") || strings.Contains(body, "@backend") {
		t.Fatalf("untrusted mention was not neutralized: %s", body)
	}
	if marker != "<!-- marketing-os-approval:approval-123 -->" {
		t.Fatalf("marker = %q", marker)
	}
}
