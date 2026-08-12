// Package retrieval provides full-text search over indexed manuscript and model content.
// It combines FTS5 searches on paragraphs, scene cards, summaries, and entities,
// then returns ordered evidence for the query pipeline.
package retrieval

import (
	"github.com/nusapuksic/story/internal/store"
)

// Result is the output of a Search call.
type Result struct {
	// Paragraphs are the matching manuscript paragraphs, ordered by FTS rank.
	Paragraphs []store.ParagraphRow
	// SceneCards are the matching scene cards, ordered by FTS rank.
	SceneCards []store.SceneCardRow
	// Summaries are matching generated summaries, ordered by FTS rank.
	Summaries []store.SummaryRow
	// Entities are matching generated entity records, ordered by FTS rank.
	Entities []store.EntityRow
}

// Options controls a search operation.
type Options struct {
	// ChapterID restricts paragraph, scene-card, summary, and entity results to a specific chapter.
	ChapterID string
	// MaxParagraphs is the maximum number of paragraph results (default 20).
	MaxParagraphs int
	// MaxSceneCards is the maximum number of scene card results (default 10).
	MaxSceneCards int
	// MaxSummaries is the maximum number of summary results (default 5).
	MaxSummaries int
	// MaxEntities is the maximum number of entity results (default 10).
	MaxEntities int
	// SceneCardStatusPolicy controls which scene-card statuses can be returned.
	// Empty defaults to store.SceneCardStatusTrustedOnly.
	SceneCardStatusPolicy store.SceneCardStatusPolicy
}

// Search retrieves paragraphs, scene cards, summaries, and entities that match query.
// The FTS indexes are searched independently; the caller receives all matching
// content up to the configured limits.
func Search(st *store.Store, query string, opts Options) (Result, error) {
	if opts.MaxParagraphs <= 0 {
		opts.MaxParagraphs = 20
	}
	if opts.MaxSceneCards <= 0 {
		opts.MaxSceneCards = 10
	}
	if opts.MaxSummaries <= 0 {
		opts.MaxSummaries = 5
	}
	if opts.MaxEntities <= 0 {
		opts.MaxEntities = 10
	}
	if opts.SceneCardStatusPolicy == "" {
		opts.SceneCardStatusPolicy = store.SceneCardStatusTrustedOnly
	}

	paras, err := st.SearchParagraphs(query, opts.ChapterID, opts.MaxParagraphs)
	if err != nil {
		return Result{}, err
	}

	cards, err := st.SearchSceneCardsByStatusPolicyForChapter(query, opts.ChapterID, opts.SceneCardStatusPolicy, opts.MaxSceneCards)
	if err != nil {
		return Result{}, err
	}

	summaries, err := st.SearchSummaries(query, opts.ChapterID, opts.MaxSummaries)
	if err != nil {
		return Result{}, err
	}

	entities, err := st.SearchEntities(query, opts.ChapterID, opts.MaxEntities)
	if err != nil {
		return Result{}, err
	}

	return Result{Paragraphs: paras, SceneCards: cards, Summaries: summaries, Entities: entities}, nil
}
