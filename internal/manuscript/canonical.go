package manuscript

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// paragraphMarker is the canonical paragraph identifier comment format.
const paragraphMarkerFmt = "<!-- story:paragraph id=%q -->"

var paragraphMarkerRe = regexp.MustCompile(`^<!-- story:paragraph id="(p-[0-9A-HJKMNP-TV-Z]+)" -->$`)

// Render produces the canonical Markdown text of a chapter and records the
// 1-based line range of each block's text in the rendered file.
func Render(ch *Chapter) string {
	var b strings.Builder
	line := 1
	writeLine := func(s string) {
		b.WriteString(s)
		b.WriteString("\n")
		line++
	}
	writeLine("# " + ch.Title)
	for i := range ch.Blocks {
		blk := &ch.Blocks[i]
		writeLine("")
		if blk.Type.Citable() {
			writeLine(fmt.Sprintf(paragraphMarkerFmt, blk.ParagraphID))
		}
		blk.LineStart = line
		for _, l := range strings.Split(blk.Text, "\n") {
			writeLine(l)
		}
		blk.LineEnd = line - 1
	}
	return b.String()
}

// WriteChapter atomically writes the canonical chapter file under dir.
func WriteChapter(dir string, ch *Chapter) error {
	path := filepath.Join(dir, filepath.FromSlash(ch.File))
	tmp, err := os.CreateTemp(filepath.Dir(path), ".chapter.*.md")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(Render(ch)); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// LoadChapter parses a canonical chapter file back into blocks, restoring
// paragraph identifiers and line ranges. manuscriptDir is the manuscript/
// directory; entry describes the chapter in the canonical TOC.
func LoadChapter(manuscriptDir string, entry TOCEntry, sceneBreakMarkers []string) (*Chapter, error) {
	path := filepath.Join(manuscriptDir, filepath.FromSlash(entry.File))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load chapter %s: %w", entry.ID, err)
	}
	markers := make(map[string]bool, len(sceneBreakMarkers))
	for _, m := range sceneBreakMarkers {
		markers[m] = true
	}

	ch := &Chapter{ID: entry.ID, Order: entry.Order, Title: entry.Title, File: entry.File, SourceKey: entry.SourceKey}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")

	var (
		cur       []string
		curStart  int
		pendingID string
		sawTitle  bool
	)
	flush := func(end int) {
		if len(cur) == 0 {
			return
		}
		text := strings.Join(cur, "\n")
		cur = nil
		blk := Block{Text: text, LineStart: curStart, LineEnd: end}
		if headingRe.MatchString(text) && !strings.Contains(text, "\n") {
			blk.Type = BlockHeading
		} else {
			blk.Type = classify(text, markers)
		}
		if blk.Type.Citable() {
			blk.ParagraphID = pendingID
		}
		pendingID = ""
		ch.Blocks = append(ch.Blocks, blk)
	}
	for i, line := range lines {
		n := i + 1
		if strings.TrimSpace(line) == "" {
			flush(n - 1)
			continue
		}
		if m := paragraphMarkerRe.FindStringSubmatch(line); m != nil {
			flush(n - 1)
			pendingID = m[1]
			continue
		}
		if !sawTitle {
			if m := titleLineRe.FindStringSubmatch(line); m != nil {
				sawTitle = true
				continue
			}
		}
		if len(cur) == 0 {
			curStart = n
		}
		cur = append(cur, line)
	}
	flush(len(lines))

	for _, blk := range ch.Blocks {
		if blk.Type.Citable() && blk.ParagraphID == "" {
			return nil, fmt.Errorf("load chapter %s: %s: citable block at line %d has no paragraph identifier", entry.ID, path, blk.LineStart)
		}
	}
	return ch, nil
}
