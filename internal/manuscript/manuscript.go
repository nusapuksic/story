// Package manuscript defines the canonical manuscript representation:
// the table of contents, structural blocks, and paragraph identifiers.
package manuscript

import (
	"crypto/sha256"
	"encoding/hex"
)

// BlockType classifies a structural block in a chapter.
type BlockType string

// Recognized structural block types.
const (
	BlockHeading    BlockType = "heading"
	BlockParagraph  BlockType = "paragraph"
	BlockBlockquote BlockType = "blockquote"
	BlockList       BlockType = "list"
	BlockSceneBreak BlockType = "scene_break"
	BlockUnknown    BlockType = "unknown"
)

// Block is one structural block of canonical chapter text.
type Block struct {
	Type BlockType
	// ParagraphID is set for citable text blocks (paragraph, blockquote,
	// list). It is empty for headings and scene breaks.
	ParagraphID string
	// Text is the canonical Markdown text of the block, without the
	// paragraph identifier comment.
	Text string
	// LineStart and LineEnd are 1-based line numbers of the block text in
	// the canonical chapter file. Zero until the chapter is rendered.
	LineStart int
	LineEnd   int
}

// Citable reports whether this block type receives a paragraph identifier.
func (t BlockType) Citable() bool {
	switch t {
	case BlockParagraph, BlockBlockquote, BlockList:
		return true
	}
	return false
}

// Chapter is a canonical chapter.
type Chapter struct {
	ID        string
	Order     int
	Title     string
	File      string // path relative to manuscript/, e.g. chapters/ch-0001.md
	SourceKey string // original source file name
	Blocks    []Block
}

// TextHash returns the sha256 hash of a block's canonical text, prefixed
// with the algorithm name.
func TextHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}
