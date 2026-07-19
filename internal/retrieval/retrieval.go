// Package retrieval provides full-text search over indexed manuscript content.
// It combines FTS5 searches on paragraphs and scene cards, deduplicates
// results, and returns ordered evidence for the query pipeline.
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
}

// Options controls a search operation.
type Options struct {
	// ChapterID restricts paragraph results to a specific chapter.
	ChapterID string
	// MaxParagraphs is the maximum number of paragraph results (default 20).
	MaxParagraphs int
	// MaxSceneCards is the maximum number of scene card results (default 10).
	MaxSceneCards int
}

// Search retrieves paragraphs and scene cards that match query.  Both FTS
// indexes are searched independently; the caller receives all matching content
// up to the configured limits.
func Search(st *store.Store, query string, opts Options) (Result, error) {
	if opts.MaxParagraphs <= 0 {
		opts.MaxParagraphs = 20
	}
	if opts.MaxSceneCards <= 0 {
		opts.MaxSceneCards = 10
	}

	paras, err := st.SearchParagraphs(query, opts.ChapterID, opts.MaxParagraphs)
	if err != nil {
		return Result{}, err
	}

	cards, err := st.SearchSceneCards(query, opts.MaxSceneCards)
	if err != nil {
		return Result{}, err
	}

	return Result{Paragraphs: paras, SceneCards: cards}, nil
}
