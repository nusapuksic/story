package manuscript

import (
	"regexp"
	"strings"
)

var (
	headingRe   = regexp.MustCompile(`^#{1,6}\s+`)
	listItemRe  = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+`)
	titleLineRe = regexp.MustCompile(`^#\s+(.*\S)\s*$`)
)

// ParseSource deterministically splits raw source Markdown into structural
// blocks. Blocks are separated by blank lines. It returns the chapter title
// taken from the first level-one heading, if any; that heading is not
// included in the returned blocks.
func ParseSource(src string, sceneBreakMarkers []string) (title string, blocks []Block) {
	markers := make(map[string]bool, len(sceneBreakMarkers))
	for _, m := range sceneBreakMarkers {
		markers[m] = true
	}

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		text := strings.Join(cur, "\n")
		cur = nil
		blocks = append(blocks, Block{Type: classify(text, markers), Text: text})
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		// A heading always starts a new block, and titles are captured
		// from the first level-one heading in the file.
		if headingRe.MatchString(line) {
			flush()
			if m := titleLineRe.FindStringSubmatch(line); m != nil && title == "" {
				title = m[1]
				continue
			}
			blocks = append(blocks, Block{Type: BlockHeading, Text: strings.TrimRight(line, " \t")})
			continue
		}
		cur = append(cur, strings.TrimRight(line, " \t"))
	}
	flush()
	return title, blocks
}

func classify(text string, sceneBreakMarkers map[string]bool) BlockType {
	trimmed := strings.TrimSpace(text)
	if sceneBreakMarkers[trimmed] {
		return BlockSceneBreak
	}
	first := strings.SplitN(text, "\n", 2)[0]
	switch {
	case strings.HasPrefix(strings.TrimSpace(first), ">"):
		return BlockBlockquote
	case listItemRe.MatchString(first):
		return BlockList
	default:
		return BlockParagraph
	}
}
