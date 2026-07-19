package skillruntime

import "testing"

func TestValidateReleaseResultAcceptsGroundedApprovalStage(t *testing.T) {
	t.Parallel()
	result := validReleaseResult()
	if err := ValidateReleaseResult(result, map[string]struct{}{"release-42": {}}, RequiredReleaseChannels); err != nil {
		t.Fatalf("ValidateReleaseResult() error = %v", err)
	}
	check := SelfCheckReleaseResult(result, map[string]struct{}{"release-42": {}}, RequiredReleaseChannels)
	if !check.Passed || len(check.Issues) != 0 {
		t.Fatalf("self-check = %+v", check)
	}
}

func TestValidateReleaseResultRejectsUnsafeOrUngroundedOutputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*ReleaseResult)
	}{
		{"unknown action", func(r *ReleaseResult) { r.Action = "publish" }},
		{"approval disabled", func(r *ReleaseResult) { r.RequiresHumanApproval = false }},
		{"unknown evidence", func(r *ReleaseResult) { r.Assets[0].EvidenceIDs = []string{"invented"} }},
		{"missing channel", func(r *ReleaseResult) { r.Assets = r.Assets[:4] }},
		{"bad score", func(r *ReleaseResult) { r.Marketability.Score = 101 }},
		{"missing email subject", func(r *ReleaseResult) {
			for i := range r.Assets {
				if r.Assets[i].Channel == "email" {
					r.Assets[i].Subject = ""
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validReleaseResult()
			tt.mutate(&result)
			if err := ValidateReleaseResult(result, map[string]struct{}{"release-42": {}}, RequiredReleaseChannels); err == nil {
				t.Fatal("unsafe result was accepted")
			}
		})
	}
}

func TestNoActionCannotSmuggleAssets(t *testing.T) {
	t.Parallel()
	result := ReleaseResult{
		Action: "no_action", ReleaseClassification: "maintenance_release",
		Marketability:     Marketability{Score: 10, Reason: "Dependency maintenance only."},
		Assets:            []AssetDraft{{Channel: "x", Content: "should not exist", EvidenceIDs: []string{"release-42"}}},
		UnsupportedClaims: []string{}, Warnings: []string{}, RequiresHumanApproval: false,
	}
	if err := ValidateReleaseResult(result, map[string]struct{}{"release-42": {}}, RequiredReleaseChannels); err == nil {
		t.Fatal("no_action result with assets was accepted")
	}
	result.Assets = nil
	if err := ValidateReleaseResult(result, map[string]struct{}{"release-42": {}}, RequiredReleaseChannels); err != nil {
		t.Fatalf("valid no_action rejected: %v", err)
	}
}

func TestWordsToAvoidBecomeDeterministicAssetValidation(t *testing.T) {
	context := "## Words to Use\n- clear\n\n## Words to Avoid\n- cheap\n- `guaranteed`\n- Unknown — requires human input\n\n## Brand Voice\nDirect"
	terms := WordsToAvoid(context)
	if len(terms) != 2 || terms[0] != "cheap" || terms[1] != "guaranteed" {
		t.Fatalf("terms = %#v", terms)
	}
	result := validReleaseResult()
	result.Assets[0].Content = "A cheap way to ship"
	if err := ValidateBrandTerminology(result, terms); err == nil {
		t.Fatal("forbidden terminology passed validation")
	}
	result = validReleaseResult()
	result.Assets[0].Content = "Send an email update."
	if err := ValidateBrandTerminology(result, []string{"AI"}); err != nil {
		t.Fatalf("forbidden short term matched inside another word: %v", err)
	}
	result.Assets[0].Content = "Built with AI for teams."
	if err := ValidateBrandTerminology(result, []string{"AI"}); err == nil {
		t.Fatal("standalone forbidden short term passed validation")
	}
}

func validReleaseResult() ReleaseResult {
	assets := make([]AssetDraft, 0, len(RequiredReleaseChannels))
	for _, channel := range RequiredReleaseChannels {
		asset := AssetDraft{Channel: channel, Content: "Evidence-backed copy.", EvidenceIDs: []string{"release-42"}}
		if channel == "email" {
			asset.Subject = "Product update"
		}
		assets = append(assets, asset)
	}
	return ReleaseResult{
		Action: "stage_for_approval", ReleaseClassification: "feature_launch",
		Marketability: Marketability{Score: 82, Reason: "Customer-visible workflow improvement."},
		Audience:      []string{"engineering managers"},
		CustomerValue: GroundedText{Summary: "Teams can export reports.", EvidenceIDs: []string{"release-42"}},
		Assets:        assets, UnsupportedClaims: []string{}, Warnings: []string{}, RequiresHumanApproval: true,
	}
}
