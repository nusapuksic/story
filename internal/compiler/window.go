package compiler

import "github.com/nusapuksic/story/internal/store"

// approxTokens estimates the token count of text using a simple 4-chars-per-token
// heuristic. A proper tokenizer would depend on the specific model family; this
// approximation is sufficient for window sizing.
func approxTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	t := len(text) / 4
	if t == 0 {
		return 1
	}
	return t
}

// Window is a contiguous slice of paragraphs sent to the LLM as one request.
type Window struct {
	Paragraphs []store.ParagraphRow
}

// buildWindows partitions paragraphs into windows that do not exceed
// targetTokens. No paragraph is split. The overlap parameter causes the last
// overlapN paragraphs of each window to be repeated at the start of the next
// (for reconciliation); it must be < targetTokens in practice.
func buildWindows(paragraphs []store.ParagraphRow, targetTokens, overlapN int) []Window {
	if len(paragraphs) == 0 {
		return nil
	}
	if targetTokens <= 0 {
		// Return one window for all paragraphs.
		return []Window{{Paragraphs: paragraphs}}
	}
	if overlapN < 0 {
		overlapN = 0
	}

	var windows []Window
	start := 0
	for start < len(paragraphs) {
		tokens := 0
		end := start
		for end < len(paragraphs) {
			t := approxTokens(paragraphs[end].Text)
			if end > start && tokens+t > targetTokens {
				break
			}
			tokens += t
			end++
		}
		windows = append(windows, Window{Paragraphs: paragraphs[start:end]})
		if end >= len(paragraphs) {
			break
		}
		// Advance with overlap.
		next := end - overlapN
		if next <= start {
			next = start + 1 // always progress
		}
		start = next
	}
	return windows
}
