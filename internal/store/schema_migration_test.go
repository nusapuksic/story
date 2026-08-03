package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesMentionEraEntityProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite")
	seedMentionEraEntityProjection(t, path)

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	for _, check := range []struct {
		table  string
		column string
	}{
		{table: "entities", column: "chapter_id"},
		{table: "occurrences", column: "scene_id"},
		{table: "occurrences", column: "surface_texts_json"},
		{table: "chapter_entity_snapshots", column: "occurrence_count"},
	} {
		hasColumn, err := tableHasColumn(st.db, check.table, check.column)
		if err != nil {
			t.Fatalf("tableHasColumn(%s, %s): %v", check.table, check.column, err)
		}
		if !hasColumn {
			t.Fatalf("%s missing migrated column %s", check.table, check.column)
		}
	}
	if tableExistsForTest(t, st.db, "mentions") {
		t.Fatal("legacy mentions table should be dropped after entity projection migration")
	}

	committed, err := st.IsEntitySnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted: %v", err)
	}
	if committed {
		t.Fatal("mention-era entity snapshot should not be trusted after migration")
	}

	if err := st.InsertEntity(EntityRow{
		ID:              "entity-current",
		ChapterID:       "ch-0001",
		Type:            "character",
		CanonicalName:   "Mara",
		Aliases:         []string{"Mara"},
		Evidence:        []string{"sc-001"},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "entity-resolution-v2",
		Status:          "generated",
		RawJSON:         `{"id":"entity-current"}`,
	}); err != nil {
		t.Fatalf("InsertEntity after migration: %v", err)
	}
	if err := st.MarkEntitySnapshotCommitted("ch-0001", 1, 0, "2024-01-01T00:00:00Z"); err != nil {
		t.Fatalf("MarkEntitySnapshotCommitted after migration: %v", err)
	}
}

func TestOpenDropsLegacyMentionsFromCurrentEntityProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.InsertChapterForTest("ch-0001", 1, "Chapter One"); err != nil {
		t.Fatalf("InsertChapterForTest: %v", err)
	}
	if err := st.InsertParagraphForTest("p-001", "ch-0001", 1); err != nil {
		t.Fatalf("InsertParagraphForTest: %v", err)
	}
	if err := st.InsertEntity(EntityRow{
		ID:              "entity-current",
		ChapterID:       "ch-0001",
		Type:            "character",
		CanonicalName:   "Mara",
		Aliases:         []string{"Mara"},
		Evidence:        []string{"sc-001"},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "entity-resolution-v2",
		Status:          "generated",
		RawJSON:         `{"id":"entity-current"}`,
	}); err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	if _, err := st.db.Exec(`
CREATE TABLE mentions (
	entity_id        TEXT NOT NULL REFERENCES entities(id),
	chapter_id       TEXT NOT NULL REFERENCES chapters(id),
	paragraph_id     TEXT NOT NULL REFERENCES paragraphs(id),
	surface_text     TEXT NOT NULL,
	confidence       REAL NOT NULL,
	generation_run   TEXT NOT NULL,
	generation_model TEXT NOT NULL,
	prompt_version   TEXT NOT NULL,
	status           TEXT NOT NULL,
	raw_json         TEXT NOT NULL,
	PRIMARY KEY (entity_id, paragraph_id, surface_text)
);
INSERT INTO mentions
	(entity_id, chapter_id, paragraph_id, surface_text, confidence, generation_run, generation_model, prompt_version, status, raw_json)
VALUES
	('entity-current', 'ch-0001', 'p-001', 'Mara', 0.95, 'compile-old', 'test-model', 'entity-extraction-v1', 'generated', '{"entity_id":"entity-current"}');
`); err != nil {
		t.Fatalf("seed legacy mentions: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if tableExistsForTest(t, reopened.db, "mentions") {
		t.Fatal("legacy mentions table should be dropped from an otherwise-current projection")
	}
	if err := reopened.DeleteEntityOccurrencesForChapter("ch-0001"); err != nil {
		t.Fatalf("DeleteEntityOccurrencesForChapter after dropping legacy mentions: %v", err)
	}
	entities, occurrences, err := reopened.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 0 || occurrences != 0 {
		t.Fatalf("entity counts after orphan cleanup = (%d, %d), want (0, 0)", entities, occurrences)
	}
}

func TestOpenPreservesCurrentEntityProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.InsertChapterForTest("ch-0001", 1, "Chapter One"); err != nil {
		t.Fatalf("InsertChapterForTest: %v", err)
	}
	if err := st.InsertParagraphForTest("p-001", "ch-0001", 1); err != nil {
		t.Fatalf("InsertParagraphForTest: %v", err)
	}
	if err := st.InsertScene(SceneRow{
		ID:             "sc-001",
		ChapterID:      "ch-0001",
		ParagraphStart: "p-001",
		ParagraphEnd:   "p-001",
		Ordinal:        1,
		BoundarySource: "chapter_end",
		Status:         "generated",
	}); err != nil {
		t.Fatalf("InsertScene: %v", err)
	}
	if err := st.InsertEntity(EntityRow{
		ID:              "entity-current",
		ChapterID:       "ch-0001",
		Type:            "character",
		CanonicalName:   "Mara",
		Aliases:         []string{"Mara"},
		Evidence:        []string{"sc-001"},
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "entity-resolution-v2",
		Status:          "generated",
		RawJSON:         `{"id":"entity-current"}`,
	}); err != nil {
		t.Fatalf("InsertEntity: %v", err)
	}
	if err := st.InsertOccurrence(OccurrenceRow{
		EntityID:        "entity-current",
		ChapterID:       "ch-0001",
		SceneID:         "sc-001",
		SurfaceTexts:    []string{"Mara"},
		SourceFields:    []string{"participants"},
		Confidence:      0.95,
		GenerationRun:   "compile-test",
		GenerationModel: "test-model",
		PromptVersion:   "entity-resolution-v2",
		Status:          "generated",
		RawJSON:         `{"entity_id":"entity-current"}`,
	}); err != nil {
		t.Fatalf("InsertOccurrence: %v", err)
	}
	if err := st.MarkEntitySnapshotCommitted("ch-0001", 1, 1, "2024-01-01T00:00:00Z"); err != nil {
		t.Fatalf("MarkEntitySnapshotCommitted: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	entities, occurrences, err := reopened.EntityCounts()
	if err != nil {
		t.Fatalf("EntityCounts: %v", err)
	}
	if entities != 1 || occurrences != 1 {
		t.Fatalf("entity counts after reopen = (%d, %d), want (1, 1)", entities, occurrences)
	}
	committed, err := reopened.IsEntitySnapshotCommitted("ch-0001")
	if err != nil {
		t.Fatalf("IsEntitySnapshotCommitted after reopen: %v", err)
	}
	if !committed {
		t.Fatal("current entity snapshot should survive reopen")
	}
}

func seedMentionEraEntityProjection(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE chapters (
	id         TEXT PRIMARY KEY,
	ordinal    INTEGER NOT NULL UNIQUE,
	title      TEXT NOT NULL,
	file       TEXT NOT NULL,
	source_key TEXT NOT NULL
);
CREATE TABLE entities (
	id               TEXT PRIMARY KEY,
	type             TEXT NOT NULL,
	canonical_name   TEXT NOT NULL,
	aliases_json     TEXT NOT NULL,
	evidence_json    TEXT NOT NULL,
	generation_run   TEXT NOT NULL,
	generation_model TEXT NOT NULL,
	prompt_version   TEXT NOT NULL,
	status           TEXT NOT NULL,
	raw_json         TEXT NOT NULL
);
CREATE TABLE mentions (
	entity_id        TEXT NOT NULL REFERENCES entities(id),
	chapter_id       TEXT NOT NULL REFERENCES chapters(id),
	paragraph_id     TEXT NOT NULL,
	surface_text     TEXT NOT NULL,
	confidence       REAL NOT NULL,
	generation_run   TEXT NOT NULL,
	generation_model TEXT NOT NULL,
	prompt_version   TEXT NOT NULL,
	status           TEXT NOT NULL,
	raw_json         TEXT NOT NULL,
	PRIMARY KEY (entity_id, paragraph_id, surface_text)
);
CREATE TABLE chapter_entity_snapshots (
	chapter_id    TEXT PRIMARY KEY REFERENCES chapters(id),
	entity_count  INTEGER NOT NULL,
	mention_count INTEGER NOT NULL,
	committed_at  TEXT NOT NULL
);
INSERT INTO chapters (id, ordinal, title, file, source_key)
VALUES ('ch-0001', 1, 'Chapter One', 'chapters/ch-0001.md', 'ch-0001');
INSERT INTO entities
	(id, type, canonical_name, aliases_json, evidence_json, generation_run, generation_model, prompt_version, status, raw_json)
VALUES
	('entity-old', 'character', 'Mara', '[]', '[]', 'compile-old', 'test-model', 'entity-extraction-v1', 'generated', '{"id":"entity-old"}');
INSERT INTO chapter_entity_snapshots (chapter_id, entity_count, mention_count, committed_at)
VALUES ('ch-0001', 1, 0, '2024-01-01T00:00:00Z');
`)
	if err != nil {
		t.Fatalf("seed old schema: %v", err)
	}
}

func tableExistsForTest(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
		t.Fatalf("tableExists(%s): %v", table, err)
	}
	return n > 0
}
