package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncDefaultsUpdatesOlderPrompt(t *testing.T) {
	dir := t.TempDir()
	oldPrompt := "<!-- prompt_version: book-summary-v1 -->\n\nold custom book prompt\n"
	writePromptForTest(t, dir, BookSummary, oldPrompt)

	report, err := SyncDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := readPromptForTest(t, dir, BookSummary)
	if version := VersionFromText(got); version != "book-summary-v2" {
		t.Fatalf("book summary version = %q, want book-summary-v2\n%s", version, got)
	}
	if strings.Contains(got, "old custom book prompt") {
		t.Fatalf("old prompt content was preserved after version update:\n%s", got)
	}
	if action := syncActionFor(report, BookSummary); action != SyncActionUpdated {
		t.Fatalf("book summary sync action = %q, want %q", action, SyncActionUpdated)
	}
}

func TestSyncDefaultsKeepsSameOrNewerLocalPrompts(t *testing.T) {
	dir := t.TempDir()
	writePromptForTest(t, dir, BookSummary, "<!-- prompt_version: book-summary-v2 -->\n\nlocal same-version customization\n")
	writePromptForTest(t, dir, ChapterSummary, "<!-- prompt_version: chapter-summary-v9 -->\n\nlocal newer customization\n")

	report, err := SyncDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}

	book := readPromptForTest(t, dir, BookSummary)
	if !strings.Contains(book, "local same-version customization") {
		t.Fatalf("same-version prompt was not preserved:\n%s", book)
	}
	chapter := readPromptForTest(t, dir, ChapterSummary)
	if !strings.Contains(chapter, "local newer customization") {
		t.Fatalf("newer local prompt was downgraded:\n%s", chapter)
	}
	if action := syncActionFor(report, BookSummary); action != SyncActionKept {
		t.Fatalf("book summary sync action = %q, want %q", action, SyncActionKept)
	}
	if action := syncActionFor(report, ChapterSummary); action != SyncActionKept {
		t.Fatalf("chapter summary sync action = %q, want %q", action, SyncActionKept)
	}
}

func TestSyncDefaultsRestoresMissingBlankAndUnversionedPrompts(t *testing.T) {
	dir := t.TempDir()
	writePromptForTest(t, dir, SceneExtraction, "   \n\t\n")
	writePromptForTest(t, dir, AnswerQuestion, "custom prompt without version marker\n")

	report, err := SyncDefaults(dir)
	if err != nil {
		t.Fatal(err)
	}

	if version := VersionFromText(readPromptForTest(t, dir, RecordVerification)); version != "record-verification-v1" {
		t.Fatalf("missing prompt restored version = %q, want record-verification-v1", version)
	}
	if action := syncActionFor(report, RecordVerification); action != SyncActionRestored {
		t.Fatalf("missing prompt action = %q, want %q", action, SyncActionRestored)
	}
	if version := VersionFromText(readPromptForTest(t, dir, SceneExtraction)); version != "scene-extraction-v1" {
		t.Fatalf("blank prompt restored version = %q, want scene-extraction-v1", version)
	}
	if action := syncActionFor(report, SceneExtraction); action != SyncActionRestored {
		t.Fatalf("blank prompt action = %q, want %q", action, SyncActionRestored)
	}
	answer := readPromptForTest(t, dir, AnswerQuestion)
	if version := VersionFromText(answer); version != "answer-question-v2" {
		t.Fatalf("unversioned prompt updated version = %q, want answer-question-v2", version)
	}
	if strings.Contains(answer, "custom prompt without version marker") {
		t.Fatalf("unversioned prompt content was preserved:\n%s", answer)
	}
	if action := syncActionFor(report, AnswerQuestion); action != SyncActionUpdated {
		t.Fatalf("unversioned prompt action = %q, want %q", action, SyncActionUpdated)
	}
}

func writePromptForTest(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPromptForTest(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func syncActionFor(report SyncReport, name string) string {
	for _, result := range report.Results {
		if result.Name == name {
			return result.Action
		}
	}
	return ""
}
