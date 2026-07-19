package llm

import (
	"context"
	"errors"
	"time"
)

var ErrCostLimit = errors.New("model request exceeds configured cost limit")
var ErrInputLimit = errors.New("model request exceeds configured input token limit")

type GenerationRequest struct {
	System          string         `json:"system"`
	Prompt          string         `json:"prompt"`
	SchemaName      string         `json:"schema_name"`
	JSONSchema      string         `json:"json_schema"`
	MaxOutputTokens int            `json:"max_output_tokens"`
	Temperature     float64        `json:"temperature"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type GenerationResult struct {
	Content          string    `json:"content"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	RequestID        string    `json:"request_id,omitempty"`
	Usage            Usage     `json:"usage"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
}

type ModelClient interface {
	Generate(ctx context.Context, request GenerationRequest) (GenerationResult, error)
}

type StaticClient struct {
	Result GenerationResult
	Err    error
	Calls  int
}

func (c *StaticClient) Generate(_ context.Context, _ GenerationRequest) (GenerationResult, error) {
	c.Calls++
	return c.Result, c.Err
}
