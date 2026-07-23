package importmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nusapuksic/story/internal/project"
)

type chapterBoundary struct {
	Line  int
	Title string
}

func planContinuousFile(sourceName, content string, opts Options) ([]plannedChapter, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if opts.SingleChapter {
		return []plannedChapter{{
			Title:         opts.Title,
			FallbackTitle: titleFromFilename(sourceName),
			SourceKey:     sourceChapterKey(sourceName, 1),
			Content:       normalized,
		}}, nil
	}

	level := opts.ChapterHeadingLevel
	if level == 0 {
		level = 1
	}
	if level < 1 || level > 6 {
		return nil, fmt.Errorf("invalid --chapter-heading-level %d: must be between 1 and 6", level)
	}

	lines := strings.Split(normalized, "\n")
	boundaries, err := detectContinuousBoundaries(lines, level, opts.ChapterRegex)
	if err != nil {
		return nil, err
	}
	if len(boundaries) == 0 {
		return nil, fmt.Errorf("%w: no chapter boundaries found in %s (use --single-chapter to import as one chapter)", ErrAmbiguousOrder, sourceName)
	}
	if hasNonEmptyLine(lines[:boundaries[0].Line]) {
		return nil, fmt.Errorf("%w: non-empty manuscript text appears before the first chapter boundary in %s", ErrAmbiguousOrder, sourceName)
	}
	if err := validateBoundaryTitles(sourceName, boundaries); err != nil {
		return nil, err
	}

	planned := make([]plannedChapter, 0, len(boundaries))
	for i, boundary := range boundaries {
		start := boundary.Line + 1
		end := len(lines)
		if i+1 < len(boundaries) {
			end = boundaries[i+1].Line
		}
		planned = append(planned, plannedChapter{
			Title:         boundary.Title,
			FallbackTitle: titleFromFilename(sourceName),
			SourceKey:     sourceChapterKey(sourceName, i+1),
			Content:       strings.Join(lines[start:end], "\n"),
		})
	}
	return planned, nil
}

func detectContinuousBoundaries(lines []string, level int, chapterRegex string) ([]chapterBoundary, error) {
	if chapterRegex != "" {
		re, err := regexp.Compile(chapterRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid --chapter-regex %q: %w", chapterRegex, err)
		}
		return detectRegexBoundaries(lines, re)
	}
	return detectHeadingBoundaries(lines, level), nil
}

func detectHeadingBoundaries(lines []string, level int) []chapterBoundary {
	prefix := strings.Repeat("#", level) + " "
	var boundaries []chapterBoundary
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		title := strings.TrimSpace(line[len(prefix):])
		if title == "" {
			continue
		}
		boundaries = append(boundaries, chapterBoundary{Line: i, Title: title})
	}
	return boundaries
}

func detectRegexBoundaries(lines []string, re *regexp.Regexp) ([]chapterBoundary, error) {
	var boundaries []chapterBoundary
	for i, line := range lines {
		match := re.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if match[0] == "" {
			return nil, fmt.Errorf("%w: --chapter-regex matched a zero-length boundary at line %d", ErrAmbiguousOrder, i+1)
		}
		title := strings.TrimSpace(line)
		if len(match) > 1 {
			title = strings.TrimSpace(match[1])
		}
		if title == "" {
			return nil, fmt.Errorf("%w: --chapter-regex produced an empty chapter title at line %d", ErrAmbiguousOrder, i+1)
		}
		boundaries = append(boundaries, chapterBoundary{Line: i, Title: title})
	}
	return boundaries, nil
}

func validateBoundaryTitles(sourceName string, boundaries []chapterBoundary) error {
	seen := make(map[string]string, len(boundaries))
	for _, boundary := range boundaries {
		title := strings.TrimSpace(boundary.Title)
		if title == "" {
			return fmt.Errorf("%w: empty chapter heading in %s at line %d", ErrAmbiguousOrder, sourceName, boundary.Line+1)
		}
		key := strings.ToLower(title)
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate chapter heading %q in %s (first seen as %q)", ErrAmbiguousOrder, title, sourceName, prev)
		}
		seen[key] = title
	}
	return nil
}

func hasNonEmptyLine(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func sourceChapterKey(sourceName string, chapter int) string {
	return fmt.Sprintf("%s#chapter-%04d", sourceName, chapter)
}

func writeFileAmbiguityRecord(p *project.Project, runID, sourcePath string, cause error) (string, error) {
	dir := p.Path(filepath.Join(project.ImportRecordsDir, runID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("write import record: %w", err)
	}
	report := Report{
		RunID:      runID,
		Type:       "md",
		SourcePath: sourcePath,
		ImportedAt: time.Now().Format(time.RFC3339),
		Warnings:   []string{cause.Error()},
		Status:     "ambiguous",
	}
	if err := writeJSON(filepath.Join(dir, "report.json"), report); err != nil {
		return "", err
	}
	warnings := cause.Error() + "\n\n" +
		"Recovery options:\n" +
		"- import the file as one chapter with --single-chapter\n" +
		"- choose a different heading level with --chapter-heading-level\n" +
		"- provide an explicit boundary regex with --chapter-regex\n"
	if err := os.WriteFile(filepath.Join(dir, "warnings.txt"), []byte(warnings), 0o644); err != nil {
		return "", fmt.Errorf("write import record: %w", err)
	}
	return dir, nil
}
