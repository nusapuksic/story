package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenAIProvider implements Provider using the OpenAI-compatible HTTP API.
// It works with any server that speaks the OpenAI REST API, including local
// model servers such as Ollama and LM Studio.
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAI creates an OpenAIProvider.  baseURL must not have a trailing
// slash.  apiKeyEnv is the environment variable name holding the API key; an
// empty string means no authentication is required.
// timeoutSeconds is the per-request timeout; zero uses a 300 s default.
func NewOpenAI(baseURL, apiKeyEnv string, timeoutSeconds int) *OpenAIProvider {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}
	key := ""
	if apiKeyEnv != "" {
		key = os.Getenv(apiKeyEnv)
	}
	return &OpenAIProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  key,
		client:  &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
	}
}

// Health checks whether the endpoint is reachable by listing models.
func (p *OpenAIProvider) Health(ctx context.Context) error {
	_, err := p.Models(ctx)
	return err
}

// Models returns the list of models from GET /models (relative to base URL).
func (p *OpenAIProvider) Models(ctx context.Context) ([]ModelInfo, error) {
	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := p.get(ctx, "/models", &resp); err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	out := make([]ModelInfo, len(resp.Data))
	for i, m := range resp.Data {
		out[i] = ModelInfo{ID: m.ID, OwnedBy: m.OwnedBy}
	}
	return out, nil
}

// Capabilities returns basic capability information for the given model.
// The OpenAI-compatible API does not expose per-model capability metadata, so
// this performs a lightweight probe by sending a minimal chat request with
// json_object response format and observing whether it succeeds.
func (p *OpenAIProvider) Capabilities(ctx context.Context, model string) (Capabilities, error) {
	models, err := p.Models(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	found := false
	for _, m := range models {
		if m.ID == model {
			found = true
			break
		}
	}
	if !found {
		return Capabilities{}, fmt.Errorf("model %q not found", model)
	}
	// Probe JSON mode with a minimal request.
	req := GenerationRequest{
		Model:     model,
		Messages:  []Message{{Role: "user", Content: `respond with valid JSON: {"ok":true}`}},
		MaxTokens: 16,
		JSONMode:  true,
	}
	_, jsonErr := p.Generate(ctx, req)
	return Capabilities{
		Chat:     true,
		JSONMode: jsonErr == nil,
	}, nil
}

// Generate calls POST /v1/chat/completions and returns the response content.
func (p *OpenAIProvider) Generate(ctx context.Context, req GenerationRequest) (GenerationResponse, error) {
	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.JSONMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := p.post(ctx, "/chat/completions", body, &resp); err != nil {
		return GenerationResponse{}, fmt.Errorf("generate: %w", err)
	}
	if len(resp.Choices) == 0 {
		return GenerationResponse{}, errors.New("generate: no choices in response")
	}
	return GenerationResponse{
		Content:      resp.Choices[0].Message.Content,
		FinishReason: resp.Choices[0].FinishReason,
		PromptTokens: resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}, nil
}

// Embed calls POST /v1/embeddings.
func (p *OpenAIProvider) Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error) {
	body := map[string]any{
		"model": req.Model,
		"input": req.Input,
	}
	var resp struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := p.post(ctx, "/embeddings", body, &resp); err != nil {
		return EmbeddingResponse{}, fmt.Errorf("embed: %w", err)
	}
	out := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		out[i] = d.Embedding
	}
	return EmbeddingResponse{Embeddings: out}, nil
}

// post marshals body to JSON, sends a POST request, and decodes the response.
func (p *OpenAIProvider) post(ctx context.Context, path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return p.do(req, out)
}

// get sends a GET request and decodes the response.
func (p *OpenAIProvider) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return p.do(req, out)
}

// do executes an HTTP request and decodes the JSON response body.
func (p *OpenAIProvider) do(req *http.Request, out any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(body), 256))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
