package compiler

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNilRunStagingStore     = errors.New("run staging store requires a run")
	ErrInvalidStagingLayer    = errors.New("invalid staging layer")
	ErrInvalidStagingSequence = errors.New("invalid staging sequence")
)

type StagedResultMeta struct {
	Sequence      int
	TaskID        string
	TargetID      string
	TargetHash    string
	SchemaVersion int
}

type StagedResultRef struct {
	Sequence    int    `json:"sequence"`
	Layer       string `json:"layer"`
	TaskID      string `json:"task_id,omitempty"`
	TargetID    string `json:"target_id,omitempty"`
	TargetHash  string `json:"target_hash,omitempty"`
	ContentHash string `json:"content_hash"`
	PendingPath string `json:"pending_path"`
}

type StagedResultEnvelope struct {
	Sequence      int             `json:"sequence"`
	Layer         string          `json:"layer"`
	TaskID        string          `json:"task_id,omitempty"`
	TargetID      string          `json:"target_id,omitempty"`
	TargetHash    string          `json:"target_hash,omitempty"`
	SchemaVersion int             `json:"schema_version,omitempty"`
	ContentHash   string          `json:"content_hash"`
	PendingPath   string          `json:"pending_path"`
	WrittenAt     string          `json:"written_at"`
	Payload       json.RawMessage `json:"payload"`
}

type StagedCommitEntry struct {
	Sequence    int    `json:"sequence"`
	Layer       string `json:"layer"`
	TaskID      string `json:"task_id,omitempty"`
	TargetID    string `json:"target_id,omitempty"`
	TargetHash  string `json:"target_hash,omitempty"`
	ContentHash string `json:"content_hash"`
	PendingPath string `json:"pending_path"`
	CommittedAt string `json:"committed_at"`
}

type RunStagingStore struct {
	run *Run
	mu  sync.Mutex
}

func newRunStagingStore(run *Run) (*RunStagingStore, error) {
	if run == nil {
		return nil, ErrNilRunStagingStore
	}
	store := &RunStagingStore{run: run}
	if err := os.MkdirAll(filepath.Join(run.dir, "pending"), 0o755); err != nil {
		return nil, fmt.Errorf("create pending staging directory: %w", err)
	}
	return store, nil
}
func optionalRunStagingStore(run *Run) (*RunStagingStore, error) {
	if run == nil {
		return nil, nil
	}
	return newRunStagingStore(run)
}

func (s *RunStagingStore) StageJSON(layer string, meta StagedResultMeta, payload any) (StagedResultRef, error) {
	if s == nil || s.run == nil {
		return StagedResultRef{}, ErrNilRunStagingStore
	}
	layer = strings.TrimSpace(layer)
	if layer == "" {
		return StagedResultRef{}, ErrInvalidStagingLayer
	}
	if meta.Sequence < 0 {
		return StagedResultRef{}, fmt.Errorf("%w: %d", ErrInvalidStagingSequence, meta.Sequence)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return StagedResultRef{}, fmt.Errorf("marshal staged payload: %w", err)
	}
	contentHash := sha256Hex(payloadBytes)
	layerDirName := sanitizeStagingName(layer, "layer")
	fileName := stagingFileName(meta.Sequence, meta.TargetID, meta.TaskID)
	relPath := filepath.ToSlash(filepath.Join("pending", layerDirName, fileName))
	absDir := filepath.Join(s.run.dir, "pending", layerDirName)
	absPath := filepath.Join(absDir, fileName)

	envelope := StagedResultEnvelope{
		Sequence:      meta.Sequence,
		Layer:         layer,
		TaskID:        meta.TaskID,
		TargetID:      meta.TargetID,
		TargetHash:    meta.TargetHash,
		SchemaVersion: meta.SchemaVersion,
		ContentHash:   contentHash,
		PendingPath:   relPath,
		WrittenAt:     formatAuditTime(time.Now().UTC()),
		Payload:       json.RawMessage(payloadBytes),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return StagedResultRef{}, fmt.Errorf("marshal staged result: %w", err)
	}

	ref := StagedResultRef{
		Sequence:    meta.Sequence,
		Layer:       layer,
		TaskID:      meta.TaskID,
		TargetID:    meta.TargetID,
		TargetHash:  meta.TargetHash,
		ContentHash: contentHash,
		PendingPath: relPath,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return StagedResultRef{}, fmt.Errorf("create staging layer directory: %w", err)
	}
	if existing, ok, err := readExistingStagedResult(absPath, ref); err != nil {
		return StagedResultRef{}, err
	} else if ok {
		return existing, nil
	}
	if err := writeAtomicFile(absDir, absPath, data); err != nil {
		return StagedResultRef{}, err
	}

	return ref, nil
}

func (s *RunStagingStore) ListPending(layer string) ([]StagedResultEnvelope, error) {
	if s == nil || s.run == nil {
		return nil, ErrNilRunStagingStore
	}
	layer = strings.TrimSpace(layer)
	if layer == "" {
		return nil, ErrInvalidStagingLayer
	}
	dir := filepath.Join(s.run.dir, "pending", sanitizeStagingName(layer, "layer"))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending staging directory: %w", err)
	}

	pending := make([]StagedResultEnvelope, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read staged result %s: %w", name, err)
		}
		var envelope StagedResultEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, fmt.Errorf("parse staged result %s: %w", name, err)
		}
		if envelope.Sequence < 0 {
			return nil, fmt.Errorf("%w: %d", ErrInvalidStagingSequence, envelope.Sequence)
		}
		if envelope.PendingPath == "" {
			envelope.PendingPath = filepath.ToSlash(filepath.Join("pending", sanitizeStagingName(layer, "layer"), name))
		}
		pending = append(pending, envelope)
	}

	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].Sequence != pending[j].Sequence {
			return pending[i].Sequence < pending[j].Sequence
		}
		if pending[i].TargetID != pending[j].TargetID {
			return pending[i].TargetID < pending[j].TargetID
		}
		if pending[i].TaskID != pending[j].TaskID {
			return pending[i].TaskID < pending[j].TaskID
		}
		return pending[i].PendingPath < pending[j].PendingPath
	})
	return pending, nil
}

func (s *RunStagingStore) UncommittedPending(layer string) ([]StagedResultEnvelope, error) {
	pending, err := s.ListPending(layer)
	if err != nil {
		return nil, err
	}
	committed, err := s.CommittedKeys()
	if err != nil {
		return nil, err
	}
	out := pending[:0]
	for _, envelope := range pending {
		if committed[stagingCommitKey{Layer: envelope.Layer, Sequence: envelope.Sequence}] {
			continue
		}
		out = append(out, envelope)
	}
	return out, nil
}

func (s *RunStagingStore) RecordCommit(ref StagedResultRef) error {
	return s.AppendCommit(StagedCommitEntry{
		Sequence:    ref.Sequence,
		Layer:       ref.Layer,
		TaskID:      ref.TaskID,
		TargetID:    ref.TargetID,
		TargetHash:  ref.TargetHash,
		ContentHash: ref.ContentHash,
		PendingPath: ref.PendingPath,
	})
}

func (s *RunStagingStore) AppendCommit(entry StagedCommitEntry) error {
	if s == nil || s.run == nil {
		return ErrNilRunStagingStore
	}
	entry.Layer = strings.TrimSpace(entry.Layer)
	if entry.Layer == "" {
		return ErrInvalidStagingLayer
	}
	if entry.Sequence < 0 {
		return fmt.Errorf("%w: %d", ErrInvalidStagingSequence, entry.Sequence)
	}
	if entry.CommittedAt == "" {
		entry.CommittedAt = formatAuditTime(time.Now().UTC())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.run.dir, "commit-log.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open commit log: %w", err)
	}
	encodeErr := json.NewEncoder(f).Encode(entry)
	closeErr := f.Close()
	if encodeErr != nil {
		return fmt.Errorf("write commit log: %w", encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close commit log: %w", closeErr)
	}
	return nil
}

func (s *RunStagingStore) ReadCommitLog() ([]StagedCommitEntry, error) {
	if s == nil || s.run == nil {
		return nil, ErrNilRunStagingStore
	}
	path := filepath.Join(s.run.dir, "commit-log.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open commit log: %w", err)
	}
	defer f.Close()

	var entries []StagedCommitEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry StagedCommitEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse commit log: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan commit log: %w", err)
	}
	return entries, nil
}

func (s *RunStagingStore) CommittedKeys() (map[stagingCommitKey]bool, error) {
	entries, err := s.ReadCommitLog()
	if err != nil {
		return nil, err
	}
	keys := make(map[stagingCommitKey]bool, len(entries))
	for _, entry := range entries {
		keys[stagingCommitKey{Layer: entry.Layer, Sequence: entry.Sequence}] = true
	}
	return keys, nil
}

type stagingCommitKey struct {
	Layer    string
	Sequence int
}

func stagingFileName(sequence int, targetID, taskID string) string {
	name := strings.TrimSpace(targetID)
	if name == "" {
		name = strings.TrimSpace(taskID)
	}
	return fmt.Sprintf("%06d-%s.json", sequence, sanitizeStagingName(name, "result"))
}

func sanitizeStagingName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.'
		if allowed {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	cleaned := strings.Trim(b.String(), "-.")
	if cleaned == "" {
		return fallback
	}
	return cleaned
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readExistingStagedResult(path string, expected StagedResultRef) (StagedResultRef, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return StagedResultRef{}, false, nil
	}
	if err != nil {
		return StagedResultRef{}, false, fmt.Errorf("read existing staged result: %w", err)
	}
	var envelope StagedResultEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return StagedResultRef{}, false, fmt.Errorf("parse existing staged result: %w", err)
	}
	if envelope.Sequence != expected.Sequence || envelope.Layer != expected.Layer || envelope.TaskID != expected.TaskID || envelope.TargetID != expected.TargetID || envelope.TargetHash != expected.TargetHash || envelope.ContentHash != expected.ContentHash {
		return StagedResultRef{}, false, fmt.Errorf("staged result already exists with different content: %s", path)
	}
	return StagedResultRef{
		Sequence:    envelope.Sequence,
		Layer:       envelope.Layer,
		TaskID:      envelope.TaskID,
		TargetID:    envelope.TargetID,
		TargetHash:  envelope.TargetHash,
		ContentHash: envelope.ContentHash,
		PendingPath: envelope.PendingPath,
	}, true, nil
}

func writeAtomicFile(dir, path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("staged result already exists with different content: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read existing staged result: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".staged-*.tmp")
	if err != nil {
		return fmt.Errorf("create staged temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write staged temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close staged temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("publish staged result: %w", err)
	}
	return nil
}
