package main

import (
	"fmt"

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
		Short: "Full-text search over paragraphs and scene cards",
		Long: `Search the indexed manuscript using full-text search.

Results include matching paragraphs and scene cards, ordered by relevance.
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
	})
	if err != nil {
		return err
	}

	total := len(result.Paragraphs) + len(result.SceneCards)
	if total == 0 {
		info("No results found for %q", query)
		return nil
	}

	if flags.jsonOut {
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
			"paragraphs":  paras,
			"scene_cards": cards,
		})
	}

	if len(result.SceneCards) > 0 {
		info("Scene cards (%d):", len(result.SceneCards))
		for _, c := range result.SceneCards {
			info("  [%s] %s", c.SceneID, c.Title)
			info("    %s", truncate(c.Summary, 120))
		}
		fmt.Println()
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
