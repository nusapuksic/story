package compiler_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
)

func TestCompileSummariesRebuildsBookSummaryWhenPromptVersionChanges(t *testing.T) {
	p, st := buildTestProject(t)
	paragraphs, err := st.ParagraphsByChapter("ch-0001")
	if err != nil {
		t.Fatalf("ParagraphsByChapter: %v", err)
	}

	summariesPath := p.Path(filepath.Join(project.ModelDir, "summaries.jsonl"))
	existing := `{"record_type":"chapter_summary","chapter_id":"ch-0001","chapter_title":"The Road","summary":"Existing chapter summary.","evidence":["` + paragraphs[0].ID + `"],"generation":{"run_id":"old-run","model":"fake-model","prompt_version":"chapter-summary-v1","generated_at":"2026-08-05T00:00:00Z"},"status":"generated"}` + "\n" +
		`{"record_type":"book_summary","summary":"Old book summary.","evidence":["ch-0001"],"generation":{"run_id":"old-run","model":"fake-model","prompt_version":"book-summary-v1","generated_at":"2026-08-05T00:00:00Z"},"status":"generated"}` + "\n"
	if err := os.WriteFile(summariesPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing summaries: %v", err)
	}

	fake := &fakeProvider{response: `{"summary":"Updated book summary mentions ch-0001.","evidence":["ch-0001"]}`}
	result, err := compiler.Compile(context.Background(), p, st, compiler.Options{
		Layer:              compiler.LayerSummaries,
		ExtractionProvider: fake,
		ExtractionModel:    "fake-model",
	})
	if err != nil {
		t.Fatalf("compile summaries: %v", err)
	}
	if result.SummariesBuilt != 1 {
		t.Fatalf("SummariesBuilt = %d, want 1 book summary rebuild", result.SummariesBuilt)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("Generate calls = %d, want 1", len(fake.requests))
	}
	if !strings.Contains(fake.requests[0].Messages[0].Content, "book-summary-v2") {
		t.Fatalf("book summary system prompt missing book-summary-v2 marker:\n%s", fake.requests[0].Messages[0].Content)
	}

	data, err := os.ReadFile(summariesPath)
	if err != nil {
		t.Fatalf("read summaries: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Updated book summary mentions ch-0001.") {
		t.Fatalf("summaries.jsonl missing regenerated book summary:\n%s", content)
	}
	if !strings.Contains(content, `"prompt_version":"book-summary-v2"`) {
		t.Fatalf("summaries.jsonl missing regenerated book prompt version:\n%s", content)
	}
}
