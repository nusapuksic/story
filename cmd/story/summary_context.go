package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nusapuksic/story/internal/compiler"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/query"
	"github.com/nusapuksic/story/internal/store"
)

type latestSummarySet struct {
	Book     *compiler.SummaryRecord
	Chapters map[string]compiler.SummaryRecord
}

func summaryContextForAsk(p *project.Project, st *store.Store, chapterID string) ([]query.SummaryContext, error) {
	set, err := readLatestSummarySet(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	chapterID = strings.TrimSpace(chapterID)
	if chapterID != "" {
		if rec, ok := set.Chapters[chapterID]; ok {
			return []query.SummaryContext{summaryContextFromRecord(rec, true)}, nil
		}
		return nil, nil
	}

	var out []query.SummaryContext
	includeChapterText := set.Book == nil
	if set.Book != nil {
		out = append(out, summaryContextFromRecord(*set.Book, true))
	}

	remaining := make(map[string]compiler.SummaryRecord, len(set.Chapters))
	for id, rec := range set.Chapters {
		remaining[id] = rec
	}
	chapters, err := st.AllChapters()
	if err == nil {
		for _, ch := range chapters {
			rec, ok := remaining[ch.ID]
			if !ok {
				continue
			}
			out = append(out, summaryContextFromRecord(rec, includeChapterText))
			delete(remaining, ch.ID)
		}
	}

	ids := make([]string, 0, len(remaining))
	for id := range remaining {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, summaryContextFromRecord(remaining[id], includeChapterText))
	}

	return out, nil
}

func readLatestSummarySet(p *project.Project) (latestSummarySet, error) {
	set := latestSummarySet{Chapters: make(map[string]compiler.SummaryRecord)}
	path := p.Path(filepath.Join(project.ModelDir, "summaries.jsonl"))
	f, err := os.Open(path)
	if err != nil {
		return set, fmt.Errorf("read summaries: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var rec compiler.SummaryRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		switch rec.RecordType {
		case "book_summary":
			recCopy := rec
			set.Book = &recCopy
		case "chapter_summary":
			if rec.ChapterID != "" {
				set.Chapters[rec.ChapterID] = rec
			}
		}
	}
	if err := sc.Err(); err != nil {
		return set, fmt.Errorf("read summaries: %w", err)
	}
	return set, nil
}

func summaryContextFromRecord(rec compiler.SummaryRecord, includeText bool) query.SummaryContext {
	ctx := query.SummaryContext{
		RecordType:    rec.RecordType,
		ChapterID:     rec.ChapterID,
		ChapterTitle:  rec.ChapterTitle,
		Evidence:      copyStrings(rec.Evidence),
		SourceRecords: copyStrings(rec.SourceRecords),
	}
	if includeText {
		ctx.Summary = rec.Summary
		ctx.Themes = copyStrings(rec.Themes)
		ctx.Unresolved = copyStrings(rec.Unresolved)
	}
	return ctx
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
