package importmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

func newTestProject(t *testing.T) *project.Project {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "novel")
	p, err := project.Init(dir, project.InitOptions{Title: "Test Novel"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func chapterTitles(t *testing.T, p *project.Project) []string {
	t.Helper()
	toc, err := manuscript.LoadTOC(p.Path(project.TOCPath))
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, len(toc.Chapters))
	for i, c := range toc.Chapters {
		titles[i] = c.Title
	}
	return titles
}

func TestImportNumericPrefixOrdering(t *testing.T) {
	p := newTestProject(t)
	res, err := Run(p, "testdata/ordered", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chapters != 3 {
		t.Fatalf("chapters = %d, want 3", res.Chapters)
	}
	// Numeric, not lexicographic: 2 < 010 < 020.
	got := chapterTitles(t, p)
	want := []string{"Prologue", "The Road", "The House"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter %d title = %q, want %q (order %v)", i+1, got[i], want[i], got)
		}
	}
	toc, err := manuscript.LoadTOC(p.Path(project.TOCPath))
	if err != nil {
		t.Fatal(err)
	}
	if toc.Chapters[0].SourceKey != "2-prologue.md" {
		t.Errorf("first chapter source = %q, want 2-prologue.md", toc.Chapters[0].SourceKey)
	}
	// README.md must be ignored.
	for _, c := range toc.Chapters {
		if c.SourceKey == "README.md" {
			t.Error("README.md was imported")
		}
	}
	// Original sources are preserved.
	preserved := p.Path(filepath.Join(project.SourceOriginalDir, res.RunID, "010-the-road.md"))
	if _, err := os.Stat(preserved); err != nil {
		t.Errorf("original source not preserved: %v", err)
	}
	// Import report exists with completed status.
	report := p.Path(filepath.Join(project.ImportRecordsDir, res.RunID, "report.json"))
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("import report: %v", err)
	}
	if !strings.Contains(string(data), `"status": "completed"`) {
		t.Errorf("report does not mark completion: %s", data)
	}
}

func TestImportAmbiguousNoPrefix(t *testing.T) {
	p := newTestProject(t)
	res, err := Run(p, "testdata/ambiguous-noprefix", Options{})
	if !errors.Is(err, ErrAmbiguousOrder) {
		t.Fatalf("err = %v, want ErrAmbiguousOrder", err)
	}
	// No canonical manuscript is written.
	if _, err := os.Stat(p.Path(project.TOCPath)); !errors.Is(err, os.ErrNotExist) {
		t.Error("canonical toc.toml was written on ambiguous import")
	}
	entries, err := os.ReadDir(p.Path(project.ChaptersDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("chapters written on ambiguous import: %v", entries)
	}
	// A proposed manifest is written and usable.
	if res.ProposedTOC == "" {
		t.Fatal("no proposed TOC path returned")
	}
	data, err := os.ReadFile(res.ProposedTOC)
	if err != nil {
		t.Fatalf("proposed toc: %v", err)
	}
	for _, f := range []string{"afterword.md", "chapter-one.md", "chapter-two.md"} {
		if !strings.Contains(string(data), f) {
			t.Errorf("proposed toc missing %q", f)
		}
	}
	// The proposal must be importable after review.
	if _, err := Run(p, "testdata/ambiguous-noprefix", Options{TOC: res.ProposedTOC}); err != nil {
		t.Fatalf("import with proposed toc: %v", err)
	}
}

func TestImportAmbiguousDuplicateNumbers(t *testing.T) {
	p := newTestProject(t)
	_, err := Run(p, "testdata/ambiguous-duplicate", Options{})
	if !errors.Is(err, ErrAmbiguousOrder) {
		t.Fatalf("err = %v, want ErrAmbiguousOrder", err)
	}
	if !strings.Contains(err.Error(), "same order value") {
		t.Errorf("error does not explain duplicate order: %v", err)
	}
}

func TestImportManifestMode(t *testing.T) {
	p := newTestProject(t)
	res, err := Run(p, "testdata/manifest", Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := chapterTitles(t, p)
	want := []string{"Prologue", "The Road", "The House"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter %d title = %q, want %q", i+1, got[i], want[i])
		}
	}
	// Unlisted files warn and are not imported.
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "notes.md") {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning for unlisted notes.md: %v", res.Warnings)
	}
	if res.Chapters != 3 {
		t.Errorf("chapters = %d, want 3", res.Chapters)
	}
	// Manifest title becomes the project title.
	reopened, err := project.Open(p.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Config.Title != "The Unopened Letter" {
		t.Errorf("project title = %q", reopened.Config.Title)
	}
}

func TestImportManifestMissingFileFails(t *testing.T) {
	p := newTestProject(t)
	src := t.TempDir()
	manifest := filepath.Join(src, "toc.toml")
	if err := os.WriteFile(manifest, []byte("version = 1\n[[chapter]]\nfile = \"missing.md\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(p, src, Options{}); err == nil {
		t.Fatal("expected failure for manifest listing a missing file")
	}
	if _, err := os.Stat(p.Path(project.TOCPath)); !errors.Is(err, os.ErrNotExist) {
		t.Error("canonical manuscript changed after failed import")
	}
}

func TestImportSingleFileHeadingSplit(t *testing.T) {
	p := newTestProject(t)
	src := filepath.Join(t.TempDir(), "manuscript.md")
	content := "# The Road\n\nMara walked the road.\n\n# The House\n\nThe house waited."
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(p, src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chapters != 2 || res.Paragraphs != 2 {
		t.Fatalf("result = %+v, want 2 chapters and 2 paragraphs", res)
	}
	got := chapterTitles(t, p)
	want := []string{"The Road", "The House"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter %d title = %q, want %q", i+1, got[i], want[i])
		}
	}
	toc, err := manuscript.LoadTOC(p.Path(project.TOCPath))
	if err != nil {
		t.Fatal(err)
	}
	if toc.Chapters[0].SourceKey != "manuscript.md#chapter-0001" {
		t.Errorf("source key = %q", toc.Chapters[0].SourceKey)
	}
	preserved := p.Path(filepath.Join(project.SourceOriginalDir, res.RunID, "manuscript.md"))
	if _, err := os.Stat(preserved); err != nil {
		t.Errorf("original single-file source not preserved: %v", err)
	}
}

func TestImportSingleFileSingleChapter(t *testing.T) {
	p := newTestProject(t)
	src := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(src, []byte("Mara walked the road.\n\nThe house waited."), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(p, src, Options{SingleChapter: true, Title: "Whole Draft"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chapters != 1 || res.Paragraphs != 2 {
		t.Fatalf("result = %+v, want 1 chapter and 2 paragraphs", res)
	}
	got := chapterTitles(t, p)
	if got[0] != "Whole Draft" {
		t.Errorf("chapter title = %q, want Whole Draft", got[0])
	}
}

func TestImportSingleFileRegexSplit(t *testing.T) {
	p := newTestProject(t)
	src := filepath.Join(t.TempDir(), "manuscript.md")
	content := "Chapter One\n\nMara walked the road.\n\nChapter Two\n\nThe house waited."
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(p, src, Options{ChapterRegex: `^Chapter (.+)$`})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chapters != 2 || res.Paragraphs != 2 {
		t.Fatalf("result = %+v, want 2 chapters and 2 paragraphs", res)
	}
	got := chapterTitles(t, p)
	want := []string{"One", "Two"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chapter %d title = %q, want %q", i+1, got[i], want[i])
		}
	}
}

func TestImportSingleFileAmbiguousNoHeading(t *testing.T) {
	p := newTestProject(t)
	src := filepath.Join(t.TempDir(), "manuscript.md")
	if err := os.WriteFile(src, []byte("Mara walked the road without a chapter heading."), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(p, src, Options{})
	if !errors.Is(err, ErrAmbiguousOrder) {
		t.Fatalf("err = %v, want ErrAmbiguousOrder", err)
	}
	if _, err := os.Stat(p.Path(project.TOCPath)); !errors.Is(err, os.ErrNotExist) {
		t.Error("canonical toc.toml was written on ambiguous single-file import")
	}
	report := p.Path(filepath.Join(project.ImportRecordsDir, res.RunID, "report.json"))
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("ambiguous import report: %v", err)
	}
	if !strings.Contains(string(data), `"status": "ambiguous"`) {
		t.Errorf("report does not mark ambiguity: %s", data)
	}
}

func TestImportConflictWithoutReplace(t *testing.T) {
	p := newTestProject(t)
	if _, err := Run(p, "testdata/ordered", Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(p, "testdata/ordered", Options{}); !errors.Is(err, ErrManuscriptConflict) {
		t.Fatalf("err = %v, want ErrManuscriptConflict", err)
	}
	if _, err := Run(p, "testdata/ordered", Options{Replace: true}); err != nil {
		t.Fatalf("replace import: %v", err)
	}
}

func TestImportDryRunDoesNotMutate(t *testing.T) {
	p := newTestProject(t)
	res, err := Run(p, "testdata/ordered", Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Chapters != 3 {
		t.Errorf("dry run result = %+v", res)
	}
	if _, err := os.Stat(p.Path(project.TOCPath)); !errors.Is(err, os.ErrNotExist) {
		t.Error("dry run wrote canonical toc.toml")
	}
	// The dry run still records a report.
	report := p.Path(filepath.Join(project.ImportRecordsDir, res.RunID, "report.json"))
	if _, err := os.Stat(report); err != nil {
		t.Errorf("dry-run report missing: %v", err)
	}
}

func TestStableParagraphIDsAcrossIndexRebuild(t *testing.T) {
	p := newTestProject(t)
	if _, err := Run(p, "testdata/ordered", Options{}); err != nil {
		t.Fatal(err)
	}
	ids := paragraphIDs(t, p)
	if len(ids) == 0 {
		t.Fatal("no paragraphs indexed")
	}

	// Delete the index and rebuild it from canonical files only.
	if err := os.Remove(p.Path(project.IndexPath)); err != nil {
		t.Fatal(err)
	}
	if err := store.Rebuild(p); err != nil {
		t.Fatal(err)
	}
	rebuilt := paragraphIDs(t, p)
	if len(rebuilt) != len(ids) {
		t.Fatalf("rebuilt %d paragraphs, want %d", len(rebuilt), len(ids))
	}
	for i := range ids {
		if rebuilt[i] != ids[i] {
			t.Errorf("paragraph %d id changed: %q -> %q", i, ids[i], rebuilt[i])
		}
	}
}

func TestIndexedParagraphMetadata(t *testing.T) {
	p := newTestProject(t)
	if _, err := Run(p, "testdata/ordered", Options{}); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(p.Path(project.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ch, err := s.InspectChapter("ch-0002")
	if err != nil {
		t.Fatal(err)
	}
	if ch.Title != "The Road" || ch.Ordinal != 2 {
		t.Errorf("chapter = %+v", ch)
	}
	if ch.ParagraphCount != 3 {
		t.Errorf("paragraph count = %d, want 3", ch.ParagraphCount)
	}

	ids := paragraphIDs(t, p)
	row, err := s.InspectParagraph(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if row.Text != "Before everything, there was the road." {
		t.Errorf("text = %q", row.Text)
	}
	if row.TextHash != manuscript.TextHash(row.Text) {
		t.Errorf("hash mismatch: %s", row.TextHash)
	}
	if row.SourceFile != "chapters/ch-0001.md" || row.SourceLineStart == 0 {
		t.Errorf("source location = %s:%d-%d", row.SourceFile, row.SourceLineStart, row.SourceLineEnd)
	}

	if _, err := s.InspectParagraph("p-DOESNOTEXIST"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// paragraphIDs returns all paragraph IDs from the canonical manuscript in
// chapter order.
func paragraphIDs(t *testing.T, p *project.Project) []string {
	t.Helper()
	toc, err := manuscript.LoadTOC(p.Path(project.TOCPath))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range toc.Chapters {
		ch, err := manuscript.LoadChapter(p.Path(project.ManuscriptDir), entry, p.Config.Manuscript.SceneBreakMarkers)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range ch.Blocks {
			if b.ParagraphID != "" {
				out = append(out, b.ParagraphID)
			}
		}
	}
	return out
}
