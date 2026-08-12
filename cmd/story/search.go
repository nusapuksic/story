package main

import (
	"github.com/spf13/cobra"

	"github.com/nusapuksic/story/internal/retrieval"
)

func newSearchCmd() *cobra.Command {
	var (
		chapterID string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search over indexed manuscript and model records",
		Long: `Search the indexed manuscript and generated model records using full-text search.

Results include matching summaries, entities, scene cards, and paragraphs, ordered by relevance within each record type.
The FTS index is built when the project is compiled; run 'story index rebuild'
to refresh it after adding new content.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(args[0], chapterID, limit)
		},
	}
	cmd.Flags().StringVar(&chapterID, "chapter", "", "restrict results to a chapter (e.g. ch-0001)")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of paragraph results")
	return cmd
}

func runSearch(query, chapterID string, limit int) error {
	p, err := openProject()
	if err != nil {
		return err
	}
	st, err := openIndex(p)
	if err != nil {
		return err
	}
	defer st.Close()

	result, err := retrieval.Search(st, query, retrieval.Options{
		ChapterID:     chapterID,
		MaxParagraphs: limit,
		MaxSceneCards: 10,
		MaxSummaries:  5,
		MaxEntities:   10,
	})
	if err != nil {
		return err
	}

	total := len(result.Summaries) + len(result.Entities) + len(result.SceneCards) + len(result.Paragraphs)
	if total == 0 {
		info("No results found for %q", query)
		return nil
	}

	if flags.jsonOut {
		type jsonSummary struct {
			RecordID   string   `json:"record_id"`
			RecordType string   `json:"record_type"`
			ChapterID  string   `json:"chapter_id,omitempty"`
			Summary    string   `json:"summary"`
			Themes     []string `json:"themes,omitempty"`
		}
		type jsonEntity struct {
			ID            string   `json:"id"`
			ChapterID     string   `json:"chapter_id"`
			Type          string   `json:"type"`
			CanonicalName string   `json:"canonical_name"`
			Aliases       []string `json:"aliases,omitempty"`
		}
		type jsonPara struct {
			ID        string `json:"id"`
			ChapterID string `json:"chapter_id"`
			Text      string `json:"text"`
		}
		type jsonCard struct {
			SceneID string `json:"scene_id"`
			Title   string `json:"title"`
			Summary string `json:"summary"`
		}
		summaries := make([]jsonSummary, 0, len(result.Summaries))
		for _, s := range result.Summaries {
			summaries = append(summaries, jsonSummary{RecordID: s.RecordID, RecordType: s.RecordType, ChapterID: s.ChapterID, Summary: s.Summary, Themes: s.Themes})
		}
		entities := make([]jsonEntity, 0, len(result.Entities))
		for _, e := range result.Entities {
			entities = append(entities, jsonEntity{ID: e.ID, ChapterID: e.ChapterID, Type: e.Type, CanonicalName: e.CanonicalName, Aliases: e.Aliases})
		}
		paras := make([]jsonPara, 0, len(result.Paragraphs))
		for _, p := range result.Paragraphs {
			paras = append(paras, jsonPara{ID: p.ID, ChapterID: p.ChapterID, Text: p.Text})
		}
		cards := make([]jsonCard, 0, len(result.SceneCards))
		for _, c := range result.SceneCards {
			cards = append(cards, jsonCard{SceneID: c.SceneID, Title: c.Title, Summary: c.Summary})
		}
		return printJSON(map[string]any{
			"query":       query,
			"summaries":   summaries,
			"entities":    entities,
			"scene_cards": cards,
			"paragraphs":  paras,
		})
	}

	if len(result.Summaries) > 0 {
		info("Summaries (%d):", len(result.Summaries))
		for _, s := range result.Summaries {
			info("  [%s] %s", s.RecordID, s.ChapterTitle)
			info("    %s", truncate(s.Summary, 120))
		}
		info("")
	}

	if len(result.Entities) > 0 {
		info("Entities (%d):", len(result.Entities))
		for _, e := range result.Entities {
			info("  [%s] %s (%s, %s)", e.ID, e.CanonicalName, e.Type, e.ChapterID)
		}
		info("")
	}

	if len(result.SceneCards) > 0 {
		info("Scene cards (%d):", len(result.SceneCards))
		for _, c := range result.SceneCards {
			info("  [%s] %s", c.SceneID, c.Title)
			info("    %s", truncate(c.Summary, 120))
		}
		info("")
	}

	if len(result.Paragraphs) > 0 {
		info("Paragraphs (%d):", len(result.Paragraphs))
		for _, para := range result.Paragraphs {
			info("  [%s] (%s)", para.ID, para.ChapterID)
			info("    %s", truncate(para.Text, 120))
		}
	}
	return nil
}

// truncate returns s truncated to at most n runes, appending "…" if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
