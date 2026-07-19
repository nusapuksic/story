// Package compiler implements the story compilation pipeline.
//
// The pipeline converts a canonical manuscript into a layered story model:
//
//	Layer 2: Scenes  (deterministic + optional LLM boundary proposals)
//	Layer 3: Scene cards  (LLM extraction with paragraph-ID validation)
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
	"time"

	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/project"
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
	TaskID    string `json:"task_id"`
	RunID     string `json:"run_id"`
	TaskType  string `json:"task_type"`
	ChapterID string `json:"chapter_id,omitempty"`
	SceneID   string `json:"scene_id,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// Run manages the lifecycle of one compilation run.
type Run struct {
	Record RunRecord
	dir    string
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
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Status:    RunStatusRunning,
		Layer:     layer,
		ChapterID: chapterID,
	}
	r := &Run{Record: rec, dir: dir}
	if err := r.save(); err != nil {
		return nil, err
	}
	return r, nil
}

// complete marks the run as completed and updates run.json.
func (r *Run) complete() error {
	r.Record.Status = RunStatusCompleted
	r.Record.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return r.save()
}

// fail marks the run as failed, records the error, and updates run.json.
func (r *Run) fail(runErr error) error {
	r.Record.Status = RunStatusFailed
	r.Record.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if saveErr := r.save(); saveErr != nil {
		return errors.Join(runErr, saveErr)
	}
	errPath := filepath.Join(r.dir, "errors.jsonl")
	f, err := os.OpenFile(errPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return errors.Join(runErr, err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(map[string]string{
		"time":  time.Now().UTC().Format(time.RFC3339),
		"error": runErr.Error(),
	})
	return runErr
}

// recordTask appends a task record to tasks.jsonl.
func (r *Run) recordTask(t TaskRecord) error {
	path := filepath.Join(r.dir, "tasks.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("record task: %w", err)
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(t)
}

// saveRawResponse writes a raw model response to raw-responses/<task-id>.json.
func (r *Run) saveRawResponse(taskID, content string) error {
	path := filepath.Join(r.dir, "raw-responses", taskID+".json")
	return os.WriteFile(path, []byte(content), 0o644)
}

func (r *Run) save() error {
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
func (r *Run) saveSummary(scenes, cards int) error {
	data, err := json.MarshalIndent(map[string]any{
		"run_id":       r.Record.RunID,
		"scenes_built": scenes,
		"cards_built":  cards,
		"finished_at":  r.Record.FinishedAt,
		"status":       r.Record.Status,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.dir, "summary.json"), data, 0o644)
}

// contextOrBackground returns ctx if non-nil, otherwise context.Background().
func contextOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
