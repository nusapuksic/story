package compiler_test

import (
	"context"

	"github.com/nusapuksic/story/internal/provider"
)

// fakeProvider implements provider.Provider using a fixed response string.
type fakeProvider struct {
	response  string
	responses []string
	err       error
	requests  []provider.GenerationRequest
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
	content := f.response
	if len(f.responses) > 0 {
		idx := len(f.requests) - 1
		if idx >= len(f.responses) {
			idx = len(f.responses) - 1
		}
		content = f.responses[idx]
	}
	return provider.GenerationResponse{Content: content}, f.err
}
func (f *fakeProvider) Embed(_ context.Context, _ provider.EmbeddingRequest) (provider.EmbeddingResponse, error) {
	return provider.EmbeddingResponse{}, f.err
}
