package llm

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewOpenAICompatibleRejectsNonFinitePricing(t *testing.T) {
	t.Parallel()
	_, err := NewOpenAICompatible(Options{
		Provider: "test", BaseURL: "https://llm.example/v1", Model: "model", Timeout: time.Second,
		MaxCostUSD: 1, InputCostPerMillionUSD: math.NaN(),
	})
	if err == nil {
		t.Fatal("constructor accepted non-finite pricing")
	}
}

func TestOpenAICompatibleGenerateStructuredJSON(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["response_format"]; !ok {
			t.Fatal("structured response_format missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"req-1","model":"test-model","choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatible(Options{
		Provider: "test", BaseURL: server.URL, APIKey: "test-secret", Model: "test-model",
		Timeout: time.Second, MaxRetries: 1, MaxCostUSD: 1,
		InputCostPerMillionUSD: 1, OutputCostPerMillionUSD: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Generate(context.Background(), GenerationRequest{
		System: "bounded", Prompt: "evidence", SchemaName: "test", JSONSchema: `{"type":"object"}`, MaxOutputTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != `{"ok":true}` || result.Usage.InputTokens != 100 || result.EstimatedCostUSD <= 0 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d", result, calls.Load())
	}
}

func TestOpenAICompatibleRetriesServerFailureWithinBound(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{}}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatible(Options{Provider: "test", BaseURL: server.URL, APIKey: "x", Model: "m", Timeout: time.Second, MaxRetries: 1, MaxCostUSD: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), GenerationRequest{Prompt: "x", SchemaName: "s", JSONSchema: `{}`, MaxOutputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestCostLimitPreventsHTTPCall(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := NewOpenAICompatible(Options{
		Provider: "test", BaseURL: server.URL, APIKey: "x", Model: "m", Timeout: time.Second,
		MaxCostUSD: 0.000001, InputCostPerMillionUSD: 100, OutputCostPerMillionUSD: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), GenerationRequest{Prompt: strings.Repeat("x", 1000), SchemaName: "s", JSONSchema: `{}`, MaxOutputTokens: 1000})
	if err == nil || calls.Load() != 0 {
		t.Fatalf("cost preflight error=%v calls=%d", err, calls.Load())
	}
}

func TestInputTokenLimitPreventsHTTPCall(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := NewOpenAICompatible(Options{Provider: "test", BaseURL: server.URL, Model: "m", Timeout: time.Second, MaxCostUSD: 1, MaxInputTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), GenerationRequest{Prompt: strings.Repeat("x", 1000), SchemaName: "s", JSONSchema: `{}`, MaxOutputTokens: 10})
	if !errors.Is(err, ErrInputLimit) || calls.Load() != 0 {
		t.Fatalf("input preflight error=%v calls=%d", err, calls.Load())
	}
}

func TestRedactRemovesExplicitAndCommonSecrets(t *testing.T) {
	t.Parallel()
	input := "token=sk-abcdefghijklmnopqrstuvwxyz123456 and explicit-value"
	got := Redact(input, []string{"explicit-value"})
	if strings.Contains(got, "explicit-value") || strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("secret remained in %q", got)
	}
}
