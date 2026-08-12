package main

import (
	"strings"

	"github.com/nusapuksic/story/internal/query"
	"github.com/nusapuksic/story/internal/store"
)

func summaryContextForAsk(st *store.Store, chapterID string) ([]query.SummaryContext, error) {
	rows, err := st.SummaryRowsForChapter(chapterID)
	if err != nil {
		return nil, err
	}
	out := make([]query.SummaryContext, 0, len(rows))
	for _, row := range rows {
		out = append(out, summaryContextFromRow(row, true))
	}
	return out, nil
}

func summaryContextFromRow(row store.SummaryRow, includeText bool) query.SummaryContext {
	ctx := query.SummaryContext{
		RecordType:           row.RecordType,
		ChapterID:            row.ChapterID,
		ChapterTitle:         row.ChapterTitle,
		Evidence:             copyStrings(row.Evidence),
		SourceRecords:        copyStrings(row.SourceRecords),
		CharacterFinalStates: summaryFinalStatesFromRow(row.CharacterFinalStates),
	}
	if includeText {
		ctx.Summary = row.Summary
		ctx.Themes = copyStrings(row.Themes)
		ctx.Unresolved = copyStrings(row.Unresolved)
	}
	return ctx
}

func summaryFinalStatesFromRow(values []store.SummaryCharacterFinalState) []query.SummaryCharacterFinalState {
	if len(values) == 0 {
		return nil
	}
	out := make([]query.SummaryCharacterFinalState, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.CharacterID) == "" || strings.TrimSpace(value.State) == "" {
			continue
		}
		out = append(out, query.SummaryCharacterFinalState{
			CharacterID: value.CharacterID,
			State:       value.State,
		})
	}
	return out
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
