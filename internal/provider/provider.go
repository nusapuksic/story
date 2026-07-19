// Package provider defines the LLM provider interface and shared types.
// All LLM access goes through this interface; the core never depends on a
// specific runtime or API SDK.
package provider

import "context"

// ModelInfo describes one model offered by a provider.
type ModelInfo struct {
	ID      string
	OwnedBy string
}

// Capabilities describes what a model supports.
type Capabilities struct {
	Chat       bool
	JSONMode   bool
	Vision     bool
	Embed      bool
	MaxContext int
}

// Message is one chat message.
type Message struct {
	Role    string // "system", "user", or "assistant"
	Content string
}

// GenerationRequest is the input to a chat-completion call.
type GenerationRequest struct {
	Model       string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	// JSONMode requests JSON-only output when supported.
	JSONMode bool
}

// GenerationResponse is the output of a chat-completion call.
type GenerationResponse struct {
	Content      string
	PromptTokens int
	OutputTokens int
	FinishReason string
}

// EmbeddingRequest is the input to an embedding call.
type EmbeddingRequest struct {
	Model string
	Input []string
}

// EmbeddingResponse is the output of an embedding call.
type EmbeddingResponse struct {
	Embeddings [][]float32
}

// Provider is the narrow interface the compilation pipeline uses for all
// model access. Implementations must be safe for concurrent use.
type Provider interface {
	// Health returns a non-nil error if the provider endpoint is
	// unreachable or misconfigured.
	Health(ctx context.Context) error

	// Models returns the list of models offered by the endpoint.
	Models(ctx context.Context) ([]ModelInfo, error)

	// Capabilities returns the capabilities of a specific model.
	Capabilities(ctx context.Context, model string) (Capabilities, error)

	// Generate calls the chat-completion endpoint and returns the
	// model's response.
	Generate(ctx context.Context, req GenerationRequest) (GenerationResponse, error)

	// Embed calls the embedding endpoint.
	Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error)
}
