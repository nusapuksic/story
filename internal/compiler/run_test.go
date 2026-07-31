package compiler

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nusapuksic/story/internal/project"
)

func TestRunRecordTaskConcurrent(t *testing.T) {
	p, err := project.Init(t.TempDir(), project.InitOptions{Title: "Concurrent Tasks", Language: "en"})
	if err != nil {
		t.Fatalf("project.Init: %v", err)
	}
	run, err := newRun(p, "compile", "", "")
	if err != nil {
		t.Fatalf("newRun: %v", err)
	}

	const tasks = 25
	var wg sync.WaitGroup
	for i := 0; i < tasks; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := run.recordTask(TaskRecord{
				TaskID:   fmt.Sprintf("task-%02d", i),
				RunID:    run.id(),
				TaskType: "test-task",
				Status:   TaskStatusCompleted,
			}); err != nil {
				t.Errorf("recordTask: %v", err)
			}
		}()
	}
	wg.Wait()

	path := filepath.Join(run.dir, "tasks.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tasks.jsonl: %v", err)
	}
	defer f.Close()

	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan tasks.jsonl: %v", err)
	}
	if lines != tasks {
		t.Fatalf("task lines = %d, want %d", lines, tasks)
	}

	metrics := run.metricsSnapshot()
	if metrics.TaskCount != tasks {
		t.Fatalf("TaskCount = %d, want %d", metrics.TaskCount, tasks)
	}
	if metrics.TaskTypeCounts["test-task"] != tasks {
		t.Fatalf("test-task count = %d, want %d", metrics.TaskTypeCounts["test-task"], tasks)
	}
}
