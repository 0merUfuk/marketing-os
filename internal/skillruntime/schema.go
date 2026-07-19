package skillruntime

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var RequiredReleaseChannels = []string{"release_summary", "changelog", "linkedin", "x", "email"}

var validClassifications = map[string]struct{}{
	"major_launch": {}, "feature_launch": {}, "minor_improvement": {},
	"maintenance_release": {}, "security_update": {}, "internal_only_change": {},
}

var markdownListPrefix = regexp.MustCompile(`^(?:[-*+]\s+|\d+[.)]\s+)`)

type Marketability struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

type GroundedText struct {
	Summary     string   `json:"summary"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type AssetDraft struct {
	Channel     string   `json:"channel"`
	Subject     string   `json:"subject,omitempty"`
	Content     string   `json:"content"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type ReleaseResult struct {
	Action                string        `json:"action"`
	ReleaseClassification string        `json:"release_classification"`
	Marketability         Marketability `json:"marketability"`
	Audience              []string      `json:"audience"`
	CustomerValue         GroundedText  `json:"customer_value"`
	Assets                []AssetDraft  `json:"assets"`
	UnsupportedClaims     []string      `json:"unsupported_claims"`
	Warnings              []string      `json:"warnings"`
	RequiresHumanApproval bool          `json:"requires_human_approval"`
}

type SelfCheck struct {
	Passed bool     `json:"passed"`
	Issues []string `json:"issues"`
}

func ValidateReleaseResult(result ReleaseResult, allowedEvidence map[string]struct{}, requiredChannels []string) error {
	var errs []error
	if result.Action != "stage_for_approval" && result.Action != "no_action" {
		errs = append(errs, fmt.Errorf("action %q is not allowed", result.Action))
	}
	if _, ok := validClassifications[result.ReleaseClassification]; !ok {
		errs = append(errs, fmt.Errorf("release classification %q is invalid", result.ReleaseClassification))
	}
	if result.Marketability.Score < 0 || result.Marketability.Score > 100 {
		errs = append(errs, errors.New("marketability score must be 0-100"))
	}
	if strings.TrimSpace(result.Marketability.Reason) == "" {
		errs = append(errs, errors.New("marketability reason is required"))
	}
	if result.UnsupportedClaims == nil || result.Warnings == nil {
		errs = append(errs, errors.New("unsupported_claims and warnings must be explicit arrays"))
	}
	if result.Action == "no_action" {
		if result.RequiresHumanApproval {
			errs = append(errs, errors.New("no_action cannot require approval"))
		}
		if len(result.Assets) != 0 {
			errs = append(errs, errors.New("no_action cannot contain generated assets"))
		}
		return errors.Join(errs...)
	}
	if !result.RequiresHumanApproval {
		errs = append(errs, errors.New("all staged assets require human approval"))
	}
	if result.ReleaseClassification == "maintenance_release" || result.ReleaseClassification == "internal_only_change" {
		errs = append(errs, errors.New("maintenance and internal-only releases cannot be staged"))
	}
	if len(result.Audience) == 0 {
		errs = append(errs, errors.New("at least one audience is required"))
	}
	if strings.TrimSpace(result.CustomerValue.Summary) == "" {
		errs = append(errs, errors.New("customer value summary is required"))
	}
	if err := validateEvidenceIDs("customer_value", result.CustomerValue.EvidenceIDs, allowedEvidence); err != nil {
		errs = append(errs, err)
	}
	seenChannels := make(map[string]int, len(result.Assets))
	for i, asset := range result.Assets {
		label := fmt.Sprintf("assets[%d]", i)
		seenChannels[asset.Channel]++
		if strings.TrimSpace(asset.Content) == "" {
			errs = append(errs, fmt.Errorf("%s content is required", label))
		}
		if asset.Channel == "email" && strings.TrimSpace(asset.Subject) == "" {
			errs = append(errs, errors.New("email asset subject is required"))
		}
		if err := validateEvidenceIDs(label, asset.EvidenceIDs, allowedEvidence); err != nil {
			errs = append(errs, err)
		}
	}
	for _, channel := range requiredChannels {
		if seenChannels[channel] != 1 {
			errs = append(errs, fmt.Errorf("required channel %s must appear exactly once", channel))
		}
	}
	for channel := range seenChannels {
		known := false
		for _, required := range requiredChannels {
			if channel == required {
				known = true
				break
			}
		}
		if !known {
			errs = append(errs, fmt.Errorf("channel %s is not allowlisted", channel))
		}
	}
	return errors.Join(errs...)
}

func SelfCheckReleaseResult(result ReleaseResult, allowedEvidence map[string]struct{}, requiredChannels []string) SelfCheck {
	err := ValidateReleaseResult(result, allowedEvidence, requiredChannels)
	if err == nil {
		return SelfCheck{Passed: true, Issues: []string{}}
	}
	issues := make([]string, 0)
	for _, part := range strings.Split(err.Error(), "\n") {
		if strings.TrimSpace(part) != "" {
			issues = append(issues, part)
		}
	}
	return SelfCheck{Passed: false, Issues: issues}
}

func WordsToAvoid(approvedContext string) []string {
	lines := strings.Split(approvedContext, "\n")
	inSection := false
	seen := map[string]struct{}{}
	var terms []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if inSection && !strings.EqualFold(heading, "Words to Avoid") {
				break
			}
			inSection = strings.EqualFold(heading, "Words to Avoid")
			continue
		}
		if !inSection || trimmed == "" {
			continue
		}
		term := markdownListPrefix.ReplaceAllString(trimmed, "")
		term = strings.Trim(strings.TrimSpace(term), "`'\".,;:")
		if term == "" || strings.Contains(strings.ToLower(term), "unknown") {
			continue
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			terms = append(terms, term)
		}
	}
	return terms
}

func ValidateBrandTerminology(result ReleaseResult, forbiddenTerms []string) error {
	if result.Action == "no_action" {
		return nil
	}
	for _, asset := range result.Assets {
		text := asset.Subject + "\n" + asset.Content
		for _, term := range forbiddenTerms {
			term = strings.TrimSpace(term)
			if term != "" && containsStandaloneTerm(text, term) {
				return fmt.Errorf("asset channel %s uses forbidden product-context term %q", asset.Channel, term)
			}
		}
	}
	return nil
}

func containsStandaloneTerm(text, term string) bool {
	pattern, err := regexp.Compile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(term) + `([^\p{L}\p{N}]|$)`)
	return err == nil && pattern.MatchString(text)
}

func validateEvidenceIDs(field string, ids []string, allowed map[string]struct{}) error {
	if len(ids) == 0 {
		return fmt.Errorf("%s requires at least one evidence id", field)
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("%s references unknown evidence id %q", field, id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("%s repeats evidence id %q", field, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

const ReleaseResultSchema = `{
  "type":"object",
  "additionalProperties":false,
  "required":["action","release_classification","marketability","audience","customer_value","assets","unsupported_claims","warnings","requires_human_approval"],
  "properties":{
    "action":{"enum":["no_action","stage_for_approval"]},
    "release_classification":{"enum":["major_launch","feature_launch","minor_improvement","maintenance_release","security_update","internal_only_change"]},
    "marketability":{"type":"object","additionalProperties":false,"required":["score","reason"],"properties":{"score":{"type":"integer","minimum":0,"maximum":100},"reason":{"type":"string"}}},
    "audience":{"type":"array","items":{"type":"string"}},
    "customer_value":{"type":"object","additionalProperties":false,"required":["summary","evidence_ids"],"properties":{"summary":{"type":"string"},"evidence_ids":{"type":"array","items":{"type":"string"}}}},
    "assets":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["channel","content","evidence_ids"],"properties":{"channel":{"enum":["release_summary","changelog","linkedin","x","email"]},"subject":{"type":"string"},"content":{"type":"string"},"evidence_ids":{"type":"array","items":{"type":"string"}}}}},
    "unsupported_claims":{"type":"array","items":{"type":"string"}},
    "warnings":{"type":"array","items":{"type":"string"}},
    "requires_human_approval":{"type":"boolean"}
  }
}`
