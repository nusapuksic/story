// Package importmd implements deterministic import of a folder of Markdown
// chapter files into the canonical manuscript.
package importmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Typed failures mapped to CLI exit codes by the command layer.
var (
	// ErrAmbiguousOrder indicates the chapter order could not be
	// determined deterministically. No canonical manuscript is changed.
	ErrAmbiguousOrder = errors.New("E_IMPORT_AMBIGUOUS_ORDER: chapter order cannot be determined")
	// ErrManuscriptConflict indicates a canonical manuscript already
	// exists and --replace was not supplied.
	ErrManuscriptConflict = errors.New("E_MANUSCRIPT_EXISTS: canonical manuscript already exists (use --replace)")
)

// numericPrefixRe is the accepted implicit-ordering prefix pattern.
var numericPrefixRe = regexp.MustCompile(`^([0-9]+)[-_. ]+`)

// sourceChapter is one chapter discovered in the source folder, in
// authoritative order.
type sourceChapter struct {
	// File is the file name relative to the source folder.
	File string
	// Title is the manifest-provided title; empty when not specified.
	Title string
}

// Manifest is a source table-of-contents manifest (toc.toml / book.toml).
type Manifest struct {
	Version  int             `toml:"version"`
	Title    string          `toml:"title"`
	Chapters []ManifestEntry `toml:"chapter"`
}

// ManifestEntry is one chapter listed in a source manifest.
type ManifestEntry struct {
	File  string `toml:"file"`
	Title string `toml:"title"`
}

// discover returns the eligible Markdown files in folder, sorted
// lexicographically. Hidden files, README.md, LICENSE.md, files outside the
// glob pattern, and subdirectories are ignored.
func discover(folder, pattern string) ([]string, error) {
	if pattern == "" {
		pattern = "*.md"
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, fmt.Errorf("read source folder %s: %w", folder, err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		switch strings.ToLower(name) {
		case "readme.md", "license.md", "toc.toml", "book.toml":
			continue
		}
		ok, err := filepath.Match(pattern, name)
		if err != nil {
			return nil, fmt.Errorf("invalid --pattern %q: %w", pattern, err)
		}
		if !ok {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	return files, nil
}

// findManifest returns the manifest path to use, or "" when none exists.
// An explicit tocPath takes precedence over toc.toml and book.toml in the
// source folder.
func findManifest(folder, tocPath string) (string, error) {
	if tocPath != "" {
		if _, err := os.Stat(tocPath); err != nil {
			return "", fmt.Errorf("manifest %s: %w", tocPath, err)
		}
		return tocPath, nil
	}
	for _, name := range []string{"toc.toml", "book.toml"} {
		p := filepath.Join(folder, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("manifest %s: %w", p, err)
		}
	}
	return "", nil
}

// loadManifest reads and validates a source manifest.
func loadManifest(path string) (Manifest, error) {
	var m Manifest
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return Manifest{}, fmt.Errorf("manifest %s: %w", path, err)
	}
	if len(m.Chapters) == 0 {
		return Manifest{}, fmt.Errorf("manifest %s: no chapters listed", path)
	}
	seen := make(map[string]bool, len(m.Chapters))
	for i, e := range m.Chapters {
		if e.File == "" {
			return Manifest{}, fmt.Errorf("manifest %s: chapter %d has no file", path, i+1)
		}
		if seen[e.File] {
			return Manifest{}, fmt.Errorf("manifest %s: duplicate chapter file %q", path, e.File)
		}
		seen[e.File] = true
	}
	return m, nil
}

// orderFromManifest resolves the authoritative order from a manifest,
// verifying every listed file exists. Unlisted eligible files produce
// warnings and are not imported.
func orderFromManifest(folder string, m Manifest, eligible []string) ([]sourceChapter, []string, error) {
	var chapters []sourceChapter
	listed := make(map[string]bool, len(m.Chapters))
	for _, e := range m.Chapters {
		path := filepath.Join(folder, filepath.FromSlash(e.File))
		if _, err := os.Stat(path); err != nil {
			return nil, nil, fmt.Errorf("manifest lists %q but the file does not exist: %w", e.File, err)
		}
		listed[e.File] = true
		chapters = append(chapters, sourceChapter{File: e.File, Title: e.Title})
	}
	var warnings []string
	for _, f := range eligible {
		if !listed[f] {
			warnings = append(warnings, fmt.Sprintf("file %q is not listed in the manifest and was not imported", f))
		}
	}
	return chapters, warnings, nil
}

// orderFromNumericPrefixes resolves implicit ordering. It succeeds only when
// every eligible file has a numeric prefix and all normalized numeric values
// are unique. Ordering is numeric, never lexicographic.
func orderFromNumericPrefixes(files []string) ([]sourceChapter, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: no Markdown files found", ErrAmbiguousOrder)
	}
	type numbered struct {
		n    int
		file string
	}
	var ordered []numbered
	seen := make(map[int]string, len(files))
	for _, f := range files {
		m := numericPrefixRe.FindStringSubmatch(f)
		if m == nil {
			return nil, fmt.Errorf("%w: %q has no numeric prefix", ErrAmbiguousOrder, f)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("%w: %q has an invalid numeric prefix", ErrAmbiguousOrder, f)
		}
		if prev, dup := seen[n]; dup {
			return nil, fmt.Errorf("%w: %q and %q normalize to the same order value %d", ErrAmbiguousOrder, prev, f, n)
		}
		seen[n] = f
		ordered = append(ordered, numbered{n: n, file: f})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].n < ordered[j].n })
	chapters := make([]sourceChapter, len(ordered))
	for i, o := range ordered {
		chapters[i] = sourceChapter{File: o.file}
	}
	return chapters, nil
}

// titleFromFilename derives a human-readable fallback title from a file name.
func titleFromFilename(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = numericPrefixRe.ReplaceAllString(base, "")
	base = strings.NewReplacer("-", " ", "_", " ").Replace(base)
	base = strings.TrimSpace(base)
	if base == "" {
		return name
	}
	words := strings.Fields(base)
	for i, w := range words {
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return strings.Join(words, " ")
}
