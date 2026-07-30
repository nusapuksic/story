package compiler_test

import (
	"context"

	"github.com/nusapuksic/story/internal/provider"
)

// fakeProvider implements provider.Provider using a fixed response string.
type fakeProvider struct {
	response     string
	responses    []string
	finishReason string
	promptTokens int
	outputTokens int
	err          error
	errors       []error
	requests     []provider.GenerationRequest
}

func (f *fakeProvider) Health(_ context.Context) error { return f.err }
func (f *fakeProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "fake-model"}}, f.err
}
func (f *fakeProvider) Capabilities(_ context.Context, _ string) (provider.Capabilities, error) {
	return provider.Capabilities{Chat: true, JSONMode: true}, f.err
}
func (f *fakeProvider) Generate(_ context.Context, req provider.GenerationRequest) (provider.GenerationResponse, error) {
	f.requests = append(f.requests, req)
	idx := len(f.requests) - 1

	content := f.response
	if len(f.responses) > 0 {
		responseIdx := idx
		if responseIdx >= len(f.responses) {
			responseIdx = len(f.responses) - 1
		}
		content = f.responses[responseIdx]
	}

	err := f.err
	if len(f.errors) > 0 {
		errIdx := idx
		if errIdx >= len(f.errors) {
			errIdx = len(f.errors) - 1
		}
		err = f.errors[errIdx]
	}

	return provider.GenerationResponse{
		Content:      content,
		FinishReason: f.finishReason,
		PromptTokens: f.promptTokens,
		OutputTokens: f.outputTokens,
	}, err
}
func (f *fakeProvider) Embed(_ context.Context, _ provider.EmbeddingRequest) (provider.EmbeddingResponse, error) {
	return provider.EmbeddingResponse{}, f.err
}
