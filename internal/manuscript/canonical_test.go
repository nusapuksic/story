package manuscript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderLoadRoundTrip(t *testing.T) {
	ch := &Chapter{
		ID:        "ch-0001",
		Order:     1,
		Title:     "The Road",
		File:      "chapters/ch-0001.md",
		SourceKey: "01-road.md",
		Blocks: []Block{
			{Type: BlockParagraph, ParagraphID: "p-01ABCDEFGHJKMNPQRSTVWXYZ01", Text: "Mara walked the road."},
			{Type: BlockSceneBreak, Text: "***"},
			{Type: BlockBlockquote, ParagraphID: "p-01ABCDEFGHJKMNPQRSTVWXYZ02", Text: "> A note.\n> Two lines."},
			{Type: BlockHeading, Text: "## Later"},
			{Type: BlockParagraph, ParagraphID: "p-01ABCDEFGHJKMNPQRSTVWXYZ03", Text: "The sun returned."},
		},
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteChapter(dir, ch); err != nil {
		t.Fatal(err)
	}

	markers := []string{"***", "* * *", "---", "§"}
	got, err := LoadChapter(dir, TOCEntry{ID: ch.ID, Order: 1, Title: ch.Title, File: ch.File, SourceKey: ch.SourceKey}, markers)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != ch.Title {
		t.Errorf("title = %q, want %q", got.Title, ch.Title)
	}
	if len(got.Blocks) != len(ch.Blocks) {
		t.Fatalf("got %d blocks, want %d: %+v", len(got.Blocks), len(ch.Blocks), got.Blocks)
	}
	for i, want := range ch.Blocks {
		g := got.Blocks[i]
		if g.Type != want.Type {
			t.Errorf("block %d type = %s, want %s", i, g.Type, want.Type)
		}
		if g.Text != want.Text {
			t.Errorf("block %d text = %q, want %q", i, g.Text, want.Text)
		}
		if g.ParagraphID != want.ParagraphID {
			t.Errorf("block %d id = %q, want %q", i, g.ParagraphID, want.ParagraphID)
		}
	}
	// Line numbers recorded during render must match the loaded file.
	for i := range ch.Blocks {
		if ch.Blocks[i].LineStart != got.Blocks[i].LineStart || ch.Blocks[i].LineEnd != got.Blocks[i].LineEnd {
			t.Errorf("block %d lines render=%d-%d load=%d-%d",
				i, ch.Blocks[i].LineStart, ch.Blocks[i].LineEnd, got.Blocks[i].LineStart, got.Blocks[i].LineEnd)
		}
	}
}

func TestLoadChapterRejectsMissingParagraphID(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "# T\n\nNo identifier here.\n"
	if err := os.WriteFile(filepath.Join(dir, "chapters", "ch-0001.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadChapter(dir, TOCEntry{ID: "ch-0001", Order: 1, Title: "T", File: "chapters/ch-0001.md"}, nil)
	if err == nil {
		t.Fatal("expected error for citable block without paragraph identifier")
	}
}
