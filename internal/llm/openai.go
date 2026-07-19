package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Provider                string
	BaseURL                 string
	APIKey                  string
	Model                   string
	Timeout                 time.Duration
	MaxRetries              int
	MaxCostUSD              float64
	MaxInputTokens          int
	InputCostPerMillionUSD  float64
	OutputCostPerMillionUSD float64
	HTTPClient              *http.Client
}

type OpenAICompatible struct {
	provider, endpoint, apiKey, model string
	maxRetries                        int
	maxInputTokens                    int
	maxCost, inputCost, outputCost    float64
	http                              *http.Client
}

func NewOpenAICompatible(options Options) (*OpenAICompatible, error) {
	if strings.TrimSpace(options.Provider) == "" || strings.TrimSpace(options.BaseURL) == "" || strings.TrimSpace(options.Model) == "" {
		return nil, errors.New("provider, base URL, and model are required")
	}
	parsed, err := url.Parse(options.BaseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("LLM base URL is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, errors.New("LLM base URL must use HTTPS unless it is loopback")
	}
	if options.Timeout <= 0 || options.MaxRetries < 0 || options.MaxRetries > 5 || options.MaxCostUSD <= 0 || !finiteCost(options.MaxCostUSD) || !finiteCost(options.InputCostPerMillionUSD) || !finiteCost(options.OutputCostPerMillionUSD) {
		return nil, errors.New("LLM timeout, retry, or cost options are invalid")
	}
	if options.MaxInputTokens <= 0 {
		options.MaxInputTokens = 128000
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: options.Timeout}
	}
	return &OpenAICompatible{
		provider: options.Provider, endpoint: strings.TrimRight(options.BaseURL, "/") + "/chat/completions",
		apiKey: options.APIKey, model: options.Model, maxRetries: options.MaxRetries,
		maxCost: options.MaxCostUSD, maxInputTokens: options.MaxInputTokens, inputCost: options.InputCostPerMillionUSD,
		outputCost: options.OutputCostPerMillionUSD, http: httpClient,
	}, nil
}

func (c *OpenAICompatible) Generate(ctx context.Context, request GenerationRequest) (GenerationResult, error) {
	if request.MaxOutputTokens <= 0 || strings.TrimSpace(request.JSONSchema) == "" || strings.TrimSpace(request.SchemaName) == "" {
		return GenerationResult{}, errors.New("positive max output tokens, schema name, and JSON schema are required")
	}
	var schema any
	if err := json.Unmarshal([]byte(request.JSONSchema), &schema); err != nil {
		return GenerationResult{}, fmt.Errorf("invalid JSON schema: %w", err)
	}
	inputEstimate := (len(request.System) + len(request.Prompt) + len(request.JSONSchema) + 3) / 4
	if inputEstimate > c.maxInputTokens {
		return GenerationResult{}, fmt.Errorf("%w: estimated %d tokens > %d", ErrInputLimit, inputEstimate, c.maxInputTokens)
	}
	maximumCost := c.cost(inputEstimate, request.MaxOutputTokens)
	if maximumCost > c.maxCost {
		return GenerationResult{}, fmt.Errorf("%w: estimated maximum %.6f USD > %.6f USD", ErrCostLimit, maximumCost, c.maxCost)
	}
	payload := map[string]any{
		"model":           c.model,
		"messages":        []map[string]string{{"role": "system", "content": request.System}, {"role": "user", "content": request.Prompt}},
		"max_tokens":      request.MaxOutputTokens,
		"temperature":     request.Temperature,
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": request.SchemaName, "strict": true, "schema": schema}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return GenerationResult{}, err
	}
	started := time.Now().UTC()
	var response apiResponse
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return GenerationResult{}, err
		}
		response, err = c.do(ctx, encoded)
		if err == nil {
			break
		}
		var retryable *retryableError
		if !errors.As(err, &retryable) || attempt == c.maxRetries {
			return GenerationResult{}, err
		}
		delay := retryable.after
		if delay <= 0 {
			delay = time.Duration(100*(1<<attempt)) * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return GenerationResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return GenerationResult{}, errors.New("model response contained no generated content")
	}
	result := GenerationResult{
		Content: response.Choices[0].Message.Content, Provider: c.provider, Model: response.Model,
		RequestID: response.ID, Usage: Usage{InputTokens: response.Usage.PromptTokens, OutputTokens: response.Usage.CompletionTokens},
		StartedAt: started, FinishedAt: time.Now().UTC(),
	}
	if result.Model == "" {
		result.Model = c.model
	}
	result.EstimatedCostUSD = c.cost(result.Usage.InputTokens, result.Usage.OutputTokens)
	if result.EstimatedCostUSD > c.maxCost {
		return GenerationResult{}, fmt.Errorf("%w: actual %.6f USD > %.6f USD", ErrCostLimit, result.EstimatedCostUSD, c.maxCost)
	}
	return result, nil
}

func (c *OpenAICompatible) do(ctx context.Context, payload []byte) (apiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return apiResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return apiResponse{}, ctx.Err()
		}
		return apiResponse{}, &retryableError{err: errors.New("model transport failed")}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return apiResponse{}, &retryableError{err: fmt.Errorf("model service returned HTTP %d", resp.StatusCode), after: retryAfter(resp.Header.Get("Retry-After"))}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return apiResponse{}, fmt.Errorf("model service returned HTTP %d", resp.StatusCode)
	}
	var decoded apiResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024))
	if err := decoder.Decode(&decoded); err != nil {
		return apiResponse{}, fmt.Errorf("decode model response: %w", err)
	}
	return decoded, nil
}

func (c *OpenAICompatible) cost(input, output int) float64 {
	return float64(input)/1_000_000*c.inputCost + float64(output)/1_000_000*c.outputCost
}

type apiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type retryableError struct {
	err   error
	after time.Duration
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil && seconds >= 0 && seconds <= 60 {
		return time.Duration(seconds) * time.Second
	}
	return 0
}

func finiteCost(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
