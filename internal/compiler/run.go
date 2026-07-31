// Package compiler implements the story compilation pipeline.
//
// The pipeline converts a canonical manuscript into a layered story model.
// Full compilation runs scenes, scene cards, optional verification, summaries,
// then entities.
//
// Each compilation creates a run record under .story/runs/<run-id>/ that can
// be used for resumability and provenance.
package compiler

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

// RunStatus values for compilation runs.
const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
)

// TaskStatus values for individual compilation tasks.
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusSkipped   = "skipped"
)

// RunRecord is written to .story/runs/<run-id>/run.json.
type RunRecord struct {
	RunID     string `json:"run_id"`
	RunType   string `json:"run_type"`
	StartedAt string `json:"started_at"`
	// FinishedAt is set when the run completes or fails.
	FinishedAt string `json:"finished_at,omitempty"`
	Status     string `json:"status"`
	Layer      string `json:"layer,omitempty"`
	ChapterID  string `json:"chapter_id,omitempty"`
}

// TaskRecord is one entry appended to .story/runs/<run-id>/tasks.jsonl.
type TaskRecord struct {
	TaskID        string         `json:"task_id"`
	RunID         string         `json:"run_id"`
	TaskType      string         `json:"task_type"`
	ChapterID     string         `json:"chapter_id,omitempty"`
	SceneID       string         `json:"scene_id,omitempty"`
	RecordID      string         `json:"record_id,omitempty"`
	PromptVersion string         `json:"prompt_version,omitempty"`
	Status        string         `json:"status"`
	StartedAt     string         `json:"started_at,omitempty"`
	FinishedAt    string         `json:"finished_at,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	Error         string         `json:"error,omitempty"`
	Response      *ResponseAudit `json:"response,omitempty"`
}

// ResponseAudit records provider metadata for one raw model response.
type ResponseAudit struct {
	CapturedAt   string `json:"captured_at,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	PromptTokens int    `json:"prompt_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	ContentBytes int    `json:"content_bytes"`
	ContentEmpty bool   `json:"content_empty,omitempty"`
}

// Run manages the lifecycle of one compilation run.
type Run struct {
	Record RunRecord
	dir    string

	tasksMu sync.Mutex
	stateMu sync.Mutex

	metricsMu sync.Mutex
	metrics   runMetrics
}

type runMetrics struct {
	TaskCount                      int
	ProviderCalls                  int
	PromptTokens                   int
	OutputTokens                   int
	ProviderCallDurationMS         int64
	MaxObservedProviderConcurrency int
	TaskTypeCounts                 map[string]int
	RetryTasks                     int
	RecoveryTasks                  int
	activeProviderCalls            int
}

type runMetricsSnapshot struct {
	TaskCount                      int
	ProviderCalls                  int
	PromptTokens                   int
	OutputTokens                   int
	ProviderCallDurationMS         int64
	MaxObservedProviderConcurrency int
	TaskTypeCounts                 map[string]int
	RetryTasks                     int
	RecoveryTasks                  int
}

type taskTiming struct {
	Started  time.Time
	Finished time.Time
}

func (t taskTiming) duration() time.Duration {
	if t.Started.IsZero() || t.Finished.IsZero() || t.Finished.Before(t.Started) {
		return 0
	}
	return t.Finished.Sub(t.Started)
}

func (t taskTiming) applyTo(record *TaskRecord) {
	if record == nil || t.Started.IsZero() {
		return
	}
	record.StartedAt = formatAuditTime(t.Started)
	if t.Finished.IsZero() {
		record.FinishedAt = record.StartedAt
		return
	}
	record.FinishedAt = formatAuditTime(t.Finished)
	record.DurationMS = t.duration().Milliseconds()
}

// newRun creates a run directory and writes the initial run.json.
func newRun(p *project.Project, runType, layer, chapterID string) (*Run, error) {
	runID := ids.NewCompileRunID()
	dir := p.Path(filepath.Join(project.RunsDir, runID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create run directory %s: %w", dir, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "raw-responses"), 0o755); err != nil {
		return nil, fmt.Errorf("create raw-responses directory: %w", err)
	}
	rec := RunRecord{
		RunID:     runID,
		RunType:   runType,
		StartedAt: formatAuditTime(time.Now().UTC()),
		Status:    RunStatusRunning,
		Layer:     layer,
		ChapterID: chapterID,
	}
	r := &Run{
		Record: rec,
		dir:    dir,
		metrics: runMetrics{
			TaskTypeCounts: make(map[string]int),
		},
	}
	if err := r.save(); err != nil {
		return nil, err
	}
	return r, nil
}

// complete marks the run as completed and updates run.json.
func (r *Run) complete() error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.Record.Status = RunStatusCompleted
	r.Record.FinishedAt = formatAuditTime(time.Now().UTC())
	return r.saveLocked()
}

// fail marks the run as failed, records the error, and updates run.json.
func (r *Run) fail(runErr error) error {
	r.stateMu.Lock()
	r.Record.Status = RunStatusFailed
	r.Record.FinishedAt = formatAuditTime(time.Now().UTC())
	saveErr := r.saveLocked()
	r.stateMu.Unlock()
	if saveErr != nil {
		return errors.Join(runErr, saveErr)
	}
	errPath := filepath.Join(r.dir, "errors.jsonl")
	f, err := os.OpenFile(errPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return errors.Join(runErr, err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(map[string]string{
		"time":  formatAuditTime(time.Now().UTC()),
		"error": runErr.Error(),
	}); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}

// recordTask appends a task record to tasks.jsonl.
func (r *Run) recordTask(t TaskRecord) error {
	if t.RunID == "" {
		t.RunID = r.id()
	}
	if t.Response == nil {
		t.Response = r.responseAuditForTask(t.TaskID)
	}
	t = normalizeTaskTiming(t, time.Now().UTC())

	path := filepath.Join(r.dir, "tasks.jsonl")
	r.tasksMu.Lock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		r.tasksMu.Unlock()
		return fmt.Errorf("record task: %w", err)
	}
	encodeErr := json.NewEncoder(f).Encode(t)
	closeErr := f.Close()
	r.tasksMu.Unlock()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}

	r.recordTaskMetrics(t)
	return nil
}

// generateWithAudit calls a provider and captures timing plus raw response metadata.
func generateWithAudit(
	ctx context.Context,
	run *Run,
	taskID string,
	prov provider.Provider,
	req provider.GenerationRequest,
) (provider.GenerationResponse, taskTiming, error) {
	timing := taskTiming{Started: time.Now().UTC()}
	if run != nil {
		run.beginProviderCall()
	}
	resp, err := prov.Generate(ctx, req)
	timing.Finished = time.Now().UTC()
	if run != nil {
		run.endProviderCall()
		_ = run.saveRawResponse(taskID, resp, timing.duration())
	}
	return resp, timing, err
}

// saveRawResponse writes raw model content and provider metadata under raw-responses/.
func (r *Run) saveRawResponse(taskID string, resp provider.GenerationResponse, duration time.Duration) error {
	contentPath := filepath.Join(r.dir, "raw-responses", taskID+".json")
	if err := os.WriteFile(contentPath, []byte(resp.Content), 0o644); err != nil {
		return err
	}

	audit := newResponseAudit(resp, duration)
	data, err := json.MarshalIndent(audit, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(r.dir, "raw-responses", taskID+".meta.json")
	return os.WriteFile(metaPath, data, 0o644)
}

func (r *Run) responseAuditForTask(taskID string) *ResponseAudit {
	path := filepath.Join(r.dir, "raw-responses", taskID+".meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var audit ResponseAudit
	if err := json.Unmarshal(data, &audit); err != nil {
		return nil
	}
	return &audit
}

func newResponseAudit(resp provider.GenerationResponse, duration time.Duration) ResponseAudit {
	return ResponseAudit{
		CapturedAt:   formatAuditTime(time.Now().UTC()),
		FinishReason: resp.FinishReason,
		PromptTokens: resp.PromptTokens,
		OutputTokens: resp.OutputTokens,
		DurationMS:   duration.Milliseconds(),
		ContentBytes: len(resp.Content),
		ContentEmpty: strings.TrimSpace(resp.Content) == "",
	}
}

func (r *Run) save() error {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.saveLocked()
}

func (r *Run) saveLocked() error {
	data, err := json.MarshalIndent(r.Record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run record: %w", err)
	}
	path := filepath.Join(r.dir, "run.json")
	tmp, err := os.CreateTemp(r.dir, ".run.*.json")
	if err != nil {
		return fmt.Errorf("write run.json: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write run.json: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write run.json: %w", err)
	}
	return os.Rename(tmp.Name(), path)
}

// SaveSummary writes a summary.json to the run directory.
func (r *Run) saveSummary(scenes, cards, sceneCardRecoveries int, sceneCardRecoveryEvents []SceneCardRecoveryEvent, entities, verifications, summaries int) error {
	record := r.recordSnapshot()
	metrics := r.metricsSnapshot()
	data, err := json.MarshalIndent(map[string]any{
		"run_id":                            record.RunID,
		"started_at":                        record.StartedAt,
		"finished_at":                       record.FinishedAt,
		"status":                            record.Status,
		"wall_clock_duration_ms":            runRecordDurationMS(record),
		"scenes_built":                      scenes,
		"cards_built":                       cards,
		"scene_card_recoveries":             sceneCardRecoveries,
		"scene_card_recovery_events":        sceneCardRecoveryEvents,
		"entities_built":                    entities,
		"verifications_built":               verifications,
		"summaries_built":                   summaries,
		"total_tasks":                       metrics.TaskCount,
		"total_provider_calls":              metrics.ProviderCalls,
		"total_prompt_tokens":               metrics.PromptTokens,
		"total_output_tokens":               metrics.OutputTokens,
		"total_provider_call_duration_ms":   metrics.ProviderCallDurationMS,
		"max_observed_provider_concurrency": metrics.MaxObservedProviderConcurrency,
		"task_type_counts":                  metrics.TaskTypeCounts,
		"retry_tasks":                       metrics.RetryTasks,
		"recovery_tasks":                    metrics.RecoveryTasks,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.dir, "summary.json"), data, 0o644)
}

func (r *Run) id() string {
	if r == nil {
		return ""
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.Record.RunID
}

func (r *Run) recordSnapshot() RunRecord {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.Record
}

func (r *Run) beginProviderCall() {
	r.metricsMu.Lock()
	defer r.metricsMu.Unlock()
	r.metrics.activeProviderCalls++
	if r.metrics.activeProviderCalls > r.metrics.MaxObservedProviderConcurrency {
		r.metrics.MaxObservedProviderConcurrency = r.metrics.activeProviderCalls
	}
}

func (r *Run) endProviderCall() {
	r.metricsMu.Lock()
	defer r.metricsMu.Unlock()
	if r.metrics.activeProviderCalls > 0 {
		r.metrics.activeProviderCalls--
	}
}

func (r *Run) recordTaskMetrics(t TaskRecord) {
	r.metricsMu.Lock()
	defer r.metricsMu.Unlock()
	if r.metrics.TaskTypeCounts == nil {
		r.metrics.TaskTypeCounts = make(map[string]int)
	}
	r.metrics.TaskCount++
	r.metrics.TaskTypeCounts[t.TaskType]++
	if t.Response != nil {
		r.metrics.ProviderCalls++
		r.metrics.PromptTokens += t.Response.PromptTokens
		r.metrics.OutputTokens += t.Response.OutputTokens
		r.metrics.ProviderCallDurationMS += t.Response.DurationMS
	}
	kind := strings.ToLower(t.TaskType)
	if strings.Contains(kind, "retry") {
		r.metrics.RetryTasks++
	}
	if strings.Contains(kind, "fallback") || strings.Contains(kind, "recovered") {
		r.metrics.RecoveryTasks++
	}
}

func (r *Run) metricsSnapshot() runMetricsSnapshot {
	r.metricsMu.Lock()
	defer r.metricsMu.Unlock()
	counts := make(map[string]int, len(r.metrics.TaskTypeCounts))
	for key, value := range r.metrics.TaskTypeCounts {
		counts[key] = value
	}
	return runMetricsSnapshot{
		TaskCount:                      r.metrics.TaskCount,
		ProviderCalls:                  r.metrics.ProviderCalls,
		PromptTokens:                   r.metrics.PromptTokens,
		OutputTokens:                   r.metrics.OutputTokens,
		ProviderCallDurationMS:         r.metrics.ProviderCallDurationMS,
		MaxObservedProviderConcurrency: r.metrics.MaxObservedProviderConcurrency,
		TaskTypeCounts:                 counts,
		RetryTasks:                     r.metrics.RetryTasks,
		RecoveryTasks:                  r.metrics.RecoveryTasks,
	}
}

func normalizeTaskTiming(t TaskRecord, now time.Time) TaskRecord {
	if strings.TrimSpace(t.StartedAt) == "" {
		t.StartedAt = formatAuditTime(now)
	}
	if strings.TrimSpace(t.FinishedAt) == "" {
		t.FinishedAt = formatAuditTime(now)
	}
	if t.DurationMS == 0 {
		started, okStart := parseAuditTime(t.StartedAt)
		finished, okFinish := parseAuditTime(t.FinishedAt)
		if okStart && okFinish && !finished.Before(started) {
			t.DurationMS = finished.Sub(started).Milliseconds()
		}
	}
	return t
}

func runRecordDurationMS(record RunRecord) int64 {
	started, okStart := parseAuditTime(record.StartedAt)
	finished, okFinish := parseAuditTime(record.FinishedAt)
	if !okStart || !okFinish || finished.Before(started) {
		return 0
	}
	return finished.Sub(started).Milliseconds()
}

func formatAuditTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseAuditTime(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// contextOrBackground returns ctx if non-nil, otherwise context.Background().
func contextOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
