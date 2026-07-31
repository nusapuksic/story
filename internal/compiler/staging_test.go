package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nusapuksic/story/internal/project"
)

func TestRunStagingStorePendingResults(t *testing.T) {
	store, run := newTestRunStagingStore(t)

	ref2, err := store.StageJSON("scene-cards", StagedResultMeta{Sequence: 2, TaskID: "task-2", TargetID: "sc-0002", TargetHash: "hash-2", SchemaVersion: 1}, map[string]string{"scene_id": "sc-0002"})
	if err != nil {
		t.Fatalf("StageJSON seq2: %v", err)
	}
	ref0, err := store.StageJSON("scene-cards", StagedResultMeta{Sequence: 0, TaskID: "task-0", TargetID: "sc-0000", TargetHash: "hash-0", SchemaVersion: 1}, map[string]string{"scene_id": "sc-0000"})
	if err != nil {
		t.Fatalf("StageJSON seq0: %v", err)
	}
	ref1, err := store.StageJSON("scene-cards", StagedResultMeta{Sequence: 1, TaskID: "task-1", TargetID: "sc-0001", TargetHash: "hash-1", SchemaVersion: 1}, map[string]string{"scene_id": "sc-0001"})
	if err != nil {
		t.Fatalf("StageJSON seq1: %v", err)
	}

	if ref2.PendingPath == "" || ref0.ContentHash == "" || ref1.Layer != "scene-cards" {
		t.Fatalf("unexpected refs: %#v %#v %#v", ref0, ref1, ref2)
	}

	tmpPath := filepath.Join(run.dir, "pending", "scene-cards", ".partial.tmp")
	if err := os.WriteFile(tmpPath, []byte(`{"partial":true}`), 0o644); err != nil {
		t.Fatalf("write temp pending file: %v", err)
	}

	pending, err := store.ListPending("scene-cards")
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if got := stagedSequences(pending); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("pending sequences = %v, want [0 1 2]", got)
	}
	var payload map[string]string
	if err := json.Unmarshal(pending[0].Payload, &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload["scene_id"] != "sc-0000" {
		t.Fatalf("payload scene_id = %q, want sc-0000", payload["scene_id"])
	}
}

func TestRunStagingStoreCommitLogSkipsCommittedPending(t *testing.T) {
	store, run := newTestRunStagingStore(t)
	refs := make([]StagedResultRef, 3)
	for i := range refs {
		ref, err := store.StageJSON("entities", StagedResultMeta{Sequence: i, TaskID: "task", TargetID: "ch"}, map[string]int{"sequence": i})
		if err != nil {
			t.Fatalf("StageJSON %d: %v", i, err)
		}
		refs[i] = ref
	}
	if err := store.RecordCommit(refs[0]); err != nil {
		t.Fatalf("RecordCommit: %v", err)
	}

	resumed, err := newRunStagingStore(run)
	if err != nil {
		t.Fatalf("newRunStagingStore resumed: %v", err)
	}
	uncommitted, err := resumed.UncommittedPending("entities")
	if err != nil {
		t.Fatalf("UncommittedPending: %v", err)
	}
	if got := stagedSequences(uncommitted); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("uncommitted sequences = %v, want [1 2]", got)
	}

	entries, err := resumed.ReadCommitLog()
	if err != nil {
		t.Fatalf("ReadCommitLog: %v", err)
	}
	if len(entries) != 1 || entries[0].Sequence != 0 || entries[0].CommittedAt == "" {
		t.Fatalf("commit log entries = %#v", entries)
	}
}

func TestRunStagingStoreRestageIdenticalResult(t *testing.T) {
	store, _ := newTestRunStagingStore(t)
	meta := StagedResultMeta{Sequence: 7, TaskID: "task-7", TargetID: "target-7"}
	payload := map[string]string{"value": "same"}

	first, err := store.StageJSON("summaries", meta, payload)
	if err != nil {
		t.Fatalf("StageJSON first: %v", err)
	}
	second, err := store.StageJSON("summaries", meta, payload)
	if err != nil {
		t.Fatalf("StageJSON identical: %v", err)
	}
	if first != second {
		t.Fatalf("restaged ref = %#v, want %#v", second, first)
	}
}

func newTestRunStagingStore(t *testing.T) (*RunStagingStore, *Run) {
	t.Helper()
	p, err := project.Init(t.TempDir(), project.InitOptions{Title: "Staging", Language: "en"})
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}
	run, err := newRun(p, "compile", "", "")
	if err != nil {
		t.Fatalf("newRun: %v", err)
	}
	store, err := newRunStagingStore(run)
	if err != nil {
		t.Fatalf("newRunStagingStore: %v", err)
	}
	return store, run
}

func stagedSequences(pending []StagedResultEnvelope) []int {
	out := make([]int, len(pending))
	for i, result := range pending {
		out[i] = result.Sequence
	}
	return out
}
