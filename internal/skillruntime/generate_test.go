package skillruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/omerufuk/marketing-os/internal/llm"
)

type scriptedModel struct {
	results []llm.GenerationResult
	errors  []error
	calls   int
}

func (m *scriptedModel) Generate(_ context.Context, _ llm.GenerationRequest) (llm.GenerationResult, error) {
	index := m.calls
	m.calls++
	if index < len(m.errors) && m.errors[index] != nil {
		return llm.GenerationResult{}, m.errors[index]
	}
	if index >= len(m.results) {
		return llm.GenerationResult{}, errors.New("unexpected call")
	}
	return m.results[index], nil
}

func TestGenerateReleaseRepairsInvalidOutputOnce(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{results: []llm.GenerationResult{
		{Content: `{"action":"publish"}`},
		{Content: `{"action":"no_action","release_classification":"maintenance_release","marketability":{"score":10,"reason":"maintenance only"},"audience":[],"customer_value":{"summary":"","evidence_ids":[]},"assets":[],"unsupported_claims":[],"warnings":[],"requires_human_approval":false}`, Usage: llm.Usage{InputTokens: 10, OutputTokens: 10}},
	}}
	result, metadata, err := GenerateRelease(context.Background(), model, ReleaseGenerationRequest{
		System: "system", Prompt: "prompt", MaxOutputTokens: 1000, MaxRepairAttempts: 1,
		AllowedEvidence: map[string]struct{}{"release-42": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "no_action" || model.calls != 2 || metadata.RepairAttempts != 1 {
		t.Fatalf("result=%+v calls=%d metadata=%+v", result, model.calls, metadata)
	}
}

func TestGenerateReleaseRejectsStillInvalidRepair(t *testing.T) {
	t.Parallel()
	model := &scriptedModel{results: []llm.GenerationResult{{Content: `{}`}, {Content: `{}`}}}
	_, _, err := GenerateRelease(context.Background(), model, ReleaseGenerationRequest{Prompt: "x", MaxOutputTokens: 100, MaxRepairAttempts: 1, AllowedEvidence: map[string]struct{}{}})
	if err == nil || model.calls != 2 {
		t.Fatalf("error=%v calls=%d", err, model.calls)
	}
}
