package project

import (
	"os"
	"path/filepath"
)

// defaultPrompts are the versioned, project-visible prompt templates copied
// into every new project. They are placeholders for later compilation
// milestones; each already states the standard evidence constraints.
var defaultPrompts = map[string]string{
	"scene-boundaries.md":    promptHeader("scene-boundaries-v1", "Propose candidate scene boundaries as JSON. Return only paragraph IDs that appear in the input window. Do not rewrite paragraph text and do not invent identifiers."),
	"scene-extraction.md":    promptHeader("scene-extraction-v1", "Extract a structured scene card as JSON. Cite paragraph IDs for every concrete statement. Omit unsupported records rather than completing plausible gaps."),
	"entity-resolution.md":   promptHeader("entity-resolution-v1", "Extract candidate entities and mentions as JSON. Preserve ambiguity: do not merge aliases unless the text is explicit. Cite paragraph IDs for every mention."),
	"record-verification.md": promptHeader("record-verification-v1", "Verify whether the cited paragraphs support the proposed record. Distinguish explicit fact from inference and narrator fact from character belief. Return schema-valid JSON only."),
	"chapter-summary.md":     promptHeader("chapter-summary-v1", "Summarize the chapter, citing paragraph IDs for every concrete claim. Preserve uncertainty and avoid resolving intentionally unresolved questions."),
	"book-summary.md":        promptHeader("book-summary-v1", "Produce a whole-book orientation summary citing lower-level records and source paragraphs. Do not cite only another summary."),
	"answer-question.md":     promptHeader("answer-question-v1", "Answer the question strictly from the provided evidence packet. Cite paragraph IDs. State plainly when the text does not establish something."),
}

func promptHeader(version, body string) string {
	return "<!-- prompt_version: " + version + " -->\n\n" +
		"The manuscript excerpts are the sole authority for this task.\n" +
		"Do not use general narrative expectations to fill missing events,\n" +
		"motives, relationships, chronology, or world facts.\n\n" +
		body + "\n"
}

func writeDefaultPrompts(dir string) error {
	for name, content := range defaultPrompts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
