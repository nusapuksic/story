package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/provider"
)

const (
	askRunType              = "query"
	askRunStatusRunning     = "running"
	askRunStatusCompleted   = "completed"
	askRunStatusFailed      = "failed"
	askRunStatusInterrupted = "interrupted"
)

type askRunConfig struct {
	Question         string
	Mode             string
	ChapterID        string
	MaxEvidence      int
	IncludeGenerated bool
}

type askRunRecord struct {
	RunID            string `json:"run_id"`
	RunType          string `json:"run_type"`
	StartedAt        string `json:"started_at"`
	FinishedAt       string `json:"finished_at,omitempty"`
	Status           string `json:"status"`
	Mode             string `json:"mode,omitempty"`
	ChapterID        string `json:"chapter_id,omitempty"`
	Question         string `json:"question,omitempty"`
	MaxEvidence      int    `json:"max_evidence,omitempty"`
	IncludeGenerated bool   `json:"include_generated,omitempty"`
	Model            string `json:"model,omitempty"`
	PromptVersion    string `json:"prompt_version,omitempty"`
}

type askRunLogRecord struct {
	askRunRecord
	Error string `json:"error,omitempty"`
}

type askRequestRecord struct {
	RunID       string             `json:"run_id"`
	CapturedAt  string             `json:"captured_at"`
	Model       string             `json:"model"`
	Messages    []provider.Message `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	JSONMode    bool               `json:"json_mode,omitempty"`
}

type askResponseAudit struct {
	CapturedAt    string `json:"captured_at,omitempty"`
	FinishReason  string `json:"finish_reason,omitempty"`
	PromptTokens  int    `json:"prompt_tokens,omitempty"`
	OutputTokens  int    `json:"output_tokens,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	ContentBytes  int    `json:"content_bytes"`
	ContentEmpty  bool   `json:"content_empty,omitempty"`
	ProviderError string `json:"provider_error,omitempty"`
}

type askRunRecorder struct {
	runID   string
	dir     string
	logsDir string

	mu     sync.Mutex
	record askRunRecord
}

func newAskRunRecorder(p *project.Project, cfg askRunConfig) (*askRunRecorder, error) {
	runID := ids.NewQueryRunID()
	dir := p.Path(filepath.Join(project.RunsDir, runID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create ask run directory %s: %w", dir, err)
	}

	rec := askRunRecord{
		RunID:            runID,
		RunType:          askRunType,
		StartedAt:        formatAskAuditTime(time.Now().UTC()),
		Status:           askRunStatusRunning,
		Mode:             strings.TrimSpace(cfg.Mode),
		ChapterID:        strings.TrimSpace(cfg.ChapterID),
		Question:         strings.TrimSpace(cfg.Question),
		MaxEvidence:      cfg.MaxEvidence,
		IncludeGenerated: cfg.IncludeGenerated,
	}
	r := &askRunRecorder{
		runID:   runID,
		dir:     dir,
		logsDir: p.Path(project.LogsDir),
		record:  rec,
	}
	if err := r.save(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *askRunRecorder) id() string {
	if r == nil {
		return ""
	}
	return r.runID
}

func (r *askRunRecorder) runDir() string {
	if r == nil {
		return ""
	}
	return r.dir
}

func (r *askRunRecorder) setModel(model string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record.Model = strings.TrimSpace(model)
	return r.saveLocked()
}

func (r *askRunRecorder) recordRequest(req provider.GenerationRequest) error {
	if r == nil {
		return nil
	}
	rec := askRequestRecord{
		RunID:       r.runID,
		CapturedAt:  formatAskAuditTime(time.Now().UTC()),
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		JSONMode:    req.JSONMode,
	}
	if err := writeIndentedJSON(filepath.Join(r.dir, "request.json"), rec); err != nil {
		return fmt.Errorf("write ask request: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "prompt.md"), []byte(formatAskPromptMarkdown(r.runID, req.Messages)), 0o644); err != nil {
		return fmt.Errorf("write ask prompt: %w", err)
	}
	return nil
}

func (r *askRunRecorder) recordResponse(resp provider.GenerationResponse, duration time.Duration, providerErr error) error {
	if r == nil {
		return nil
	}
	if err := os.WriteFile(filepath.Join(r.dir, "raw-response.txt"), []byte(resp.Content), 0o644); err != nil {
		return fmt.Errorf("write ask raw response: %w", err)
	}
	audit := askResponseAudit{
		CapturedAt:   formatAskAuditTime(time.Now().UTC()),
		FinishReason: resp.FinishReason,
		PromptTokens: resp.PromptTokens,
		OutputTokens: resp.OutputTokens,
		DurationMS:   duration.Milliseconds(),
		ContentBytes: len(resp.Content),
		ContentEmpty: strings.TrimSpace(resp.Content) == "",
	}
	if providerErr != nil {
		audit.ProviderError = providerErr.Error()
	}
	if err := writeIndentedJSON(filepath.Join(r.dir, "raw-response.meta.json"), audit); err != nil {
		return fmt.Errorf("write ask response metadata: %w", err)
	}
	return nil
}

func (r *askRunRecorder) finish(runErr error, promptVersion string) error {
	if r == nil {
		return nil
	}
	status := askRunStatusCompleted
	if runErr != nil {
		status = askRunStatusFailed
		if errors.Is(runErr, context.Canceled) {
			status = askRunStatusInterrupted
		}
	}

	r.mu.Lock()
	r.record.Status = status
	r.record.FinishedAt = formatAskAuditTime(time.Now().UTC())
	if strings.TrimSpace(promptVersion) != "" {
		r.record.PromptVersion = strings.TrimSpace(promptVersion)
	}
	record := r.record
	saveErr := r.saveLocked()
	r.mu.Unlock()

	var errs []error
	if saveErr != nil {
		errs = append(errs, saveErr)
	}
	if runErr != nil {
		errs = append(errs, r.recordError(runErr))
	}
	errs = append(errs, r.appendRunLog(record, runErr))
	return errors.Join(errs...)
}

func (r *askRunRecorder) annotateError(err error) error {
	if err == nil || r == nil {
		return err
	}
	return fmt.Errorf("%w (ask run %s; artifacts: %s)", err, r.runID, r.dir)
}

func (r *askRunRecorder) save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

func (r *askRunRecorder) saveLocked() error {
	data, err := json.MarshalIndent(r.record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ask run record: %w", err)
	}
	path := filepath.Join(r.dir, "run.json")
	tmp, err := os.CreateTemp(r.dir, ".run.*.json")
	if err != nil {
		return fmt.Errorf("write ask run.json: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write ask run.json: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write ask run.json: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write ask run.json: %w", err)
	}
	return nil
}

func (r *askRunRecorder) recordError(runErr error) error {
	if r == nil || runErr == nil {
		return nil
	}
	path := filepath.Join(r.dir, "errors.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	entry := map[string]string{
		"time":  formatAskAuditTime(time.Now().UTC()),
		"error": runErr.Error(),
	}
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		return err
	}
	return nil
}

func (r *askRunRecorder) appendRunLog(record askRunRecord, runErr error) error {
	if r == nil || strings.TrimSpace(r.logsDir) == "" {
		return nil
	}
	if err := os.MkdirAll(r.logsDir, 0o755); err != nil {
		return fmt.Errorf("write ask run log: %w", err)
	}
	entry := askRunLogRecord{askRunRecord: record}
	if runErr != nil {
		entry.Error = runErr.Error()
	}
	path := filepath.Join(r.logsDir, "runs.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("write ask run log: %w", err)
	}
	encodeErr := json.NewEncoder(f).Encode(entry)
	closeErr := f.Close()
	if encodeErr != nil {
		return fmt.Errorf("write ask run log: %w", encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("write ask run log: %w", closeErr)
	}
	return nil
}

type askRecordingProvider struct {
	inner    provider.Provider
	recorder *askRunRecorder
}

func (p *askRecordingProvider) Health(ctx context.Context) error {
	return p.inner.Health(ctx)
}

func (p *askRecordingProvider) Models(ctx context.Context) ([]provider.ModelInfo, error) {
	return p.inner.Models(ctx)
}

func (p *askRecordingProvider) Capabilities(ctx context.Context, model string) (provider.Capabilities, error) {
	return p.inner.Capabilities(ctx, model)
}

func (p *askRecordingProvider) Generate(ctx context.Context, req provider.GenerationRequest) (provider.GenerationResponse, error) {
	if err := p.recorder.recordRequest(req); err != nil {
		return provider.GenerationResponse{}, err
	}
	started := time.Now().UTC()
	resp, err := p.inner.Generate(ctx, req)
	duration := time.Since(started)
	if recordErr := p.recorder.recordResponse(resp, duration, err); recordErr != nil {
		return resp, errors.Join(err, recordErr)
	}
	return resp, err
}

func (p *askRecordingProvider) Embed(ctx context.Context, req provider.EmbeddingRequest) (provider.EmbeddingResponse, error) {
	return p.inner.Embed(ctx, req)
}

func writeIndentedJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func formatAskPromptMarkdown(runID string, messages []provider.Message) string {
	var sb strings.Builder
	sb.WriteString("# story ask prompt\n\n")
	if strings.TrimSpace(runID) != "" {
		sb.WriteString("Run: ")
		sb.WriteString(runID)
		sb.WriteString("\n\n")
	}
	for i, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "message"
		}
		fmt.Fprintf(&sb, "## %s message %d\n\n", role, i+1)
		sb.WriteString(msg.Content)
		if !strings.HasSuffix(msg.Content, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatAskAuditTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
