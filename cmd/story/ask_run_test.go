package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
)

type askRunFakeProvider struct {
	resp     provider.GenerationResponse
	err      error
	requests []provider.GenerationRequest
}

func (f *askRunFakeProvider) Health(_ context.Context) error { return f.err }
func (f *askRunFakeProvider) Models(_ context.Context) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{ID: "fake-model"}}, f.err
}
func (f *askRunFakeProvider) Capabilities(_ context.Context, _ string) (provider.Capabilities, error) {
	return provider.Capabilities{Chat: true, JSONMode: true}, f.err
}
func (f *askRunFakeProvider) Generate(_ context.Context, req provider.GenerationRequest) (provider.GenerationResponse, error) {
	f.requests = append(f.requests, req)
	return f.resp, f.err
}
func (f *askRunFakeProvider) Embed(_ context.Context, _ provider.EmbeddingRequest) (provider.EmbeddingResponse, error) {
	return provider.EmbeddingResponse{}, f.err
}

func TestAskRunRecorderWritesPromptResponseAndFailure(t *testing.T) {
	p := &project.Project{Dir: t.TempDir()}
	recorder, err := newAskRunRecorder(p, askRunConfig{
		Question:    "Where does Mara put the letter?",
		Mode:        "recall",
		MaxEvidence: 7,
	})
	if err != nil {
		t.Fatalf("newAskRunRecorder: %v", err)
	}
	if err := recorder.setModel("fake-model"); err != nil {
		t.Fatalf("setModel: %v", err)
	}

	fake := &askRunFakeProvider{resp: provider.GenerationResponse{
		Content:      "",
		FinishReason: "length",
		PromptTokens: 11,
		OutputTokens: 2,
	}}
	wrapped := &askRecordingProvider{inner: fake, recorder: recorder}
	req := provider.GenerationRequest{
		Model: "fake-model",
		Messages: []provider.Message{
			{Role: "system", Content: "system instructions"},
			{Role: "user", Content: "## Evidence paragraphs\n\n[p-1] text"},
		},
		Temperature: 0.2,
		MaxTokens:   2000,
		JSONMode:    true,
	}
	if _, err := wrapped.Generate(context.Background(), req); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	parseErr := errors.New("parse model response: model returned empty response")
	if err := recorder.finish(parseErr, ""); err != nil {
		t.Fatalf("finish: %v", err)
	}

	runDir := recorder.runDir()
	prompt, err := os.ReadFile(filepath.Join(runDir, "prompt.md"))
	if err != nil {
		t.Fatalf("read prompt.md: %v", err)
	}
	for _, want := range []string{"system instructions", "## Evidence paragraphs", "[p-1] text"} {
		if !strings.Contains(string(prompt), want) {
			t.Fatalf("prompt.md missing %q:\n%s", want, prompt)
		}
	}

	requestData, err := os.ReadFile(filepath.Join(runDir, "request.json"))
	if err != nil {
		t.Fatalf("read request.json: %v", err)
	}
	for _, want := range []string{`"model": "fake-model"`, `"json_mode": true`, `"max_tokens": 2000`} {
		if !strings.Contains(string(requestData), want) {
			t.Fatalf("request.json missing %s:\n%s", want, requestData)
		}
	}

	raw, err := os.ReadFile(filepath.Join(runDir, "raw-response.txt"))
	if err != nil {
		t.Fatalf("read raw-response.txt: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("raw-response.txt length = %d, want 0", len(raw))
	}

	meta, err := os.ReadFile(filepath.Join(runDir, "raw-response.meta.json"))
	if err != nil {
		t.Fatalf("read raw-response.meta.json: %v", err)
	}
	for _, want := range []string{`"finish_reason": "length"`, `"prompt_tokens": 11`, `"output_tokens": 2`, `"content_bytes": 0`, `"content_empty": true`} {
		if !strings.Contains(string(meta), want) {
			t.Fatalf("response metadata missing %s:\n%s", want, meta)
		}
	}

	runData, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatalf("read run.json: %v", err)
	}
	var run askRunRecord
	if err := json.Unmarshal(runData, &run); err != nil {
		t.Fatalf("parse run.json: %v", err)
	}
	if run.Status != askRunStatusFailed {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
	if run.Model != "fake-model" {
		t.Fatalf("run model = %q, want fake-model", run.Model)
	}

	errorsData, err := os.ReadFile(filepath.Join(runDir, "errors.jsonl"))
	if err != nil {
		t.Fatalf("read errors.jsonl: %v", err)
	}
	if !strings.Contains(string(errorsData), parseErr.Error()) {
		t.Fatalf("errors.jsonl missing parse error:\n%s", errorsData)
	}

	logData, err := os.ReadFile(p.Path(filepath.Join(project.LogsDir, "runs.jsonl")))
	if err != nil {
		t.Fatalf("read runs.jsonl: %v", err)
	}
	if !strings.Contains(string(logData), recorder.id()) || !strings.Contains(string(logData), parseErr.Error()) {
		t.Fatalf("runs.jsonl missing run id/error:\n%s", logData)
	}
}

func TestAskRunRecorderWritesNumberedModelCalls(t *testing.T) {
	p := &project.Project{Dir: t.TempDir()}
	recorder, err := newAskRunRecorder(p, askRunConfig{Question: "Summarize the story", Mode: "recall", MaxEvidence: 2})
	if err != nil {
		t.Fatalf("newAskRunRecorder: %v", err)
	}
	fake := &askRunFakeProvider{resp: provider.GenerationResponse{Content: `{"answer":"ok","evidence":[]}`}}
	wrapped := &askRecordingProvider{inner: fake, recorder: recorder}
	for _, content := range []string{"condense call", "final answer call"} {
		_, err := wrapped.Generate(context.Background(), provider.GenerationRequest{
			Model:    "fake-model",
			Messages: []provider.Message{{Role: "user", Content: content}},
			JSONMode: true,
		})
		if err != nil {
			t.Fatalf("Generate %q: %v", content, err)
		}
	}

	runDir := recorder.runDir()
	for _, name := range []string{
		"calls/0001-request.json",
		"calls/0001-prompt.md",
		"calls/0001-raw-response.txt",
		"calls/0002-request.json",
		"calls/0002-prompt.md",
		"calls/0002-raw-response.txt",
	} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("missing numbered call artifact %s: %v", name, err)
		}
	}
	rootPrompt, err := os.ReadFile(filepath.Join(runDir, "prompt.md"))
	if err != nil {
		t.Fatalf("read root prompt.md: %v", err)
	}
	if !strings.Contains(string(rootPrompt), "final answer call") || strings.Contains(string(rootPrompt), "condense call") {
		t.Fatalf("root prompt should mirror latest call:\n%s", rootPrompt)
	}
}
