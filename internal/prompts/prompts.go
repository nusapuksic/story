// Package prompts owns project-visible prompt templates and prompt loading.
package prompts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	SceneBoundaries    = "scene-boundaries.md"
	SceneExtraction    = "scene-extraction.md"
	EntityResolution   = "entity-resolution.md"
	RecordVerification = "record-verification.md"
	ChapterSummary     = "chapter-summary.md"
	BookSummary        = "book-summary.md"
	AnswerQuestion     = "answer-question.md"
)

// Loaded is a prompt loaded from a project prompt file or embedded defaults.
type Loaded struct {
	Name        string
	Content     string
	Version     string
	FromDefault bool
}

type template struct {
	version string
	body    string
}

var defaults = map[string]template{
	SceneBoundaries: {
		version: "scene-boundaries-v1",
		body: `You are a literary analyst. Identify where scene boundaries occur in manuscript excerpts.
A scene boundary occurs when there is a meaningful shift in time, location, point of view, or narrative focus.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Use only paragraph IDs that appear in the provided input. Do not invent identifiers.`,
	},
	SceneExtraction: {
		version: "scene-extraction-v1",
		body: `You are a literary analyst extracting structured scene cards from manuscript excerpts.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Cite only paragraph IDs that appear in the provided input.
Omit unsupported fields rather than guessing.`,
	},
	EntityResolution: {
		version: "entity-resolution-v2",
		body: `You are a literary analyst consolidating entity candidates already extracted into scene cards.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Use only the supplied reverse-index scene IDs and surface texts; do not invent new occurrences.
Preserve ambiguity: do not merge aliases unless the candidate evidence strongly supports one identity.
Flag likely typos or suspicious variants instead of silently correcting original surface text.`,
	},
	RecordVerification: {
		version: "record-verification-v1",
		body: `You verify whether cited manuscript paragraphs support a proposed generated story record.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Distinguish explicit fact, reasonable inference, narrator fact, and character belief.
Mark records unsupported when the citations do not directly support the proposed statements.`,
	},
	ChapterSummary: {
		version: "chapter-summary-v1",
		body: `You are a literary analyst summarizing a manuscript chapter.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Cite only paragraph IDs that appear in the provided input.
Preserve uncertainty and do not resolve intentionally unresolved questions.`,
	},
	BookSummary: {
		version: "book-summary-v1",
		body: `You are a literary analyst producing a whole-book orientation summary.
Return only valid JSON matching the requested schema. Do not add commentary outside the JSON object.
Use only the provided chapter summary records as source material. Cite chapter IDs only; do not cite paragraph IDs.
Preserve uncertainty and flag unresolved details briefly for later expansion.`,
	},
	AnswerQuestion: {
		version: "answer-question-v1",
		body: `You are a literary analyst answering questions about a fiction manuscript.
Answer strictly from the provided context. Do not use general narrative expectations.
Return ONLY a JSON object with this exact schema:
{"answer":"...","evidence":["p-...","p-..."],"uncertainties":["..."]}

Rules:
- "answer": your prose answer grounded in the provided context.
- Use summary context when provided for high-level themes, motifs, arcs, or whole-book orientation.
- "evidence": list only paragraph IDs from the provided evidence that directly support your answer. Omit IDs that do not support the answer.
- "uncertainties": list genuine gaps or unresolved questions from the manuscript. Omit if none.
- Cite no paragraph IDs that were not provided to you.
- If the provided context is insufficient, say so in "answer" and leave "evidence" empty.`,
	},
}

// Names returns the canonical project prompt filenames in stable order.
func Names() []string {
	names := make([]string, 0, len(defaults))
	for name := range defaults {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Default returns the embedded default prompt for name.
func Default(name string) (Loaded, bool) {
	t, ok := defaults[name]
	if !ok {
		return Loaded{}, false
	}
	return Loaded{
		Name:        name,
		Content:     promptText(t.version, t.body),
		Version:     t.version,
		FromDefault: true,
	}, true
}

// Load reads a project-visible prompt from promptsDir, falling back to the
// embedded default when the file is missing or blank.
func Load(promptsDir, name string) (Loaded, error) {
	def, ok := Default(name)
	if !ok {
		return Loaded{}, fmt.Errorf("unknown prompt %q", name)
	}
	if strings.TrimSpace(promptsDir) == "" {
		return def, nil
	}
	data, err := os.ReadFile(filepath.Join(promptsDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return def, nil
	}
	if err != nil {
		return Loaded{}, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return def, nil
	}
	content := string(data)
	version := VersionFromText(content)
	if version == "" {
		version = def.Version
	}
	return Loaded{
		Name:    name,
		Content: content,
		Version: version,
	}, nil
}

// WriteDefaults copies all embedded prompt templates into dir.
func WriteDefaults(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range Names() {
		p, _ := Default(name)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(p.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// VersionFromText extracts a prompt_version marker from prompt text.
func VersionFromText(text string) string {
	const marker = "prompt_version:"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		version := strings.TrimSpace(line[idx+len(marker):])
		version = strings.TrimSuffix(version, "-->")
		return strings.TrimSpace(version)
	}
	return ""
}

func promptText(version, body string) string {
	return "<!-- prompt_version: " + version + " -->\n\n" +
		"The manuscript excerpts are the sole authority for this task.\n" +
		"Do not use general narrative expectations to fill missing events,\n" +
		"motives, relationships, chronology, or world facts.\n\n" +
		strings.TrimSpace(body) + "\n"
}
