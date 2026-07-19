package manuscript

import "testing"

func TestParseSource(t *testing.T) {
	markers := []string{"***", "* * *", "---", "§"}
	tests := []struct {
		name      string
		src       string
		wantTitle string
		wantTypes []BlockType
		wantTexts []string
	}{
		{
			name:      "title and paragraphs",
			src:       "# The Road\n\nMara walked.\n\nIt rained.\n",
			wantTitle: "The Road",
			wantTypes: []BlockType{BlockParagraph, BlockParagraph},
			wantTexts: []string{"Mara walked.", "It rained."},
		},
		{
			name:      "scene break markers",
			src:       "One.\n\n***\n\nTwo.\n\n* * *\n\nThree.\n",
			wantTypes: []BlockType{BlockParagraph, BlockSceneBreak, BlockParagraph, BlockSceneBreak, BlockParagraph},
		},
		{
			name:      "blockquote and list",
			src:       "> A note.\n> Second line.\n\n- one\n- two\n",
			wantTypes: []BlockType{BlockBlockquote, BlockList},
			wantTexts: []string{"> A note.\n> Second line.", "- one\n- two"},
		},
		{
			name:      "multi-line paragraph joined",
			src:       "First line\nsecond line.\n\nNext.\n",
			wantTypes: []BlockType{BlockParagraph, BlockParagraph},
			wantTexts: []string{"First line\nsecond line.", "Next."},
		},
		{
			name:      "subheading kept as heading block",
			src:       "# Title\n\n## Part One\n\nText.\n",
			wantTitle: "Title",
			wantTypes: []BlockType{BlockHeading, BlockParagraph},
		},
		{
			name:      "second level-one heading is a block not the title",
			src:       "# Title\n\nText.\n\n# Another\n",
			wantTitle: "Title",
			wantTypes: []BlockType{BlockParagraph, BlockHeading},
		},
		{
			name:      "no title",
			src:       "Just text.\n",
			wantTitle: "",
			wantTypes: []BlockType{BlockParagraph},
		},
		{
			name:      "windows line endings",
			src:       "# T\r\n\r\nOne.\r\n\r\nTwo.\r\n",
			wantTitle: "T",
			wantTypes: []BlockType{BlockParagraph, BlockParagraph},
			wantTexts: []string{"One.", "Two."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, blocks := ParseSource(tt.src, markers)
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if len(blocks) != len(tt.wantTypes) {
				t.Fatalf("got %d blocks, want %d: %+v", len(blocks), len(tt.wantTypes), blocks)
			}
			for i, want := range tt.wantTypes {
				if blocks[i].Type != want {
					t.Errorf("block %d type = %s, want %s", i, blocks[i].Type, want)
				}
			}
			for i, want := range tt.wantTexts {
				if blocks[i].Text != want {
					t.Errorf("block %d text = %q, want %q", i, blocks[i].Text, want)
				}
			}
		})
	}
}
