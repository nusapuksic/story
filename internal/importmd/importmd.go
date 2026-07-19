package importmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/nusapuksic/story/internal/config"
	"github.com/nusapuksic/story/internal/ids"
	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
	"github.com/nusapuksic/story/internal/store"
)

// Options control a Markdown-folder import.
type Options struct {
	TOC     string // explicit manifest path (--toc)
	Pattern string // source file glob (--pattern), default *.md
	Title   string // project title override (--title)
	Replace bool   // replace an existing canonical manuscript (--replace)
	DryRun  bool   // detect and report without modifying the manuscript (--dry-run)
}

// Result summarizes a completed (or dry-run) import.
type Result struct {
	RunID      string
	Chapters   int
	Paragraphs int
	Warnings   []string
	DryRun     bool
	// ProposedTOC is the path of the proposed manifest written when the
	// import failed because ordering was ambiguous.
	ProposedTOC string
}

// Report is the import record written to
// source/import-records/<run-id>/report.json.
type Report struct {
	RunID      string   `json:"run_id"`
	Type       string   `json:"type"`
	SourcePath string   `json:"source_path"`
	SourceHash string   `json:"source_hash,omitempty"`
	ImportedAt string   `json:"imported_at"`
	Chapters   int      `json:"chapters"`
	Paragraphs int      `json:"paragraphs"`
	Warnings   []string `json:"warnings"`
	Status     string   `json:"status"`
}

// Run imports a folder of Markdown chapter files into the project's
// canonical manuscript. On ambiguous ordering it writes a proposed manifest
// under source/import-records/<run-id>/ and returns ErrAmbiguousOrder
// without changing the canonical manuscript.
func Run(p *project.Project, folder string, opts Options) (*Result, error) {
	runID := ids.NewImportRunID()
	res := &Result{RunID: runID, DryRun: opts.DryRun}

	absFolder, err := filepath.Abs(folder)
	if err != nil {
		absFolder = folder
	}

	tocExists := false
	if _, err := os.Stat(p.Path(project.TOCPath)); err == nil {
		tocExists = true
	}
	if tocExists && !opts.Replace && !opts.DryRun {
		return nil, fmt.Errorf("import md %s: %w", folder, ErrManuscriptConflict)
	}

	eligible, err := discover(folder, opts.Pattern)
	if err != nil {
		return nil, err
	}

	manifestPath, err := findManifest(folder, opts.TOC)
	if err != nil {
		return nil, err
	}

	var (
		sources       []sourceChapter
		manifestTitle string
	)
	if manifestPath != "" {
		m, err := loadManifest(manifestPath)
		if err != nil {
			return nil, err
		}
		manifestTitle = m.Title
		var warnings []string
		sources, warnings, err = orderFromManifest(folder, m, eligible)
		if err != nil {
			return nil, err
		}
		res.Warnings = append(res.Warnings, warnings...)
	} else {
		sources, err = orderFromNumericPrefixes(eligible)
		if errors.Is(err, ErrAmbiguousOrder) {
			proposedPath, werr := writeAmbiguityRecord(p, runID, absFolder, eligible, err)
			if werr != nil {
				return nil, errors.Join(err, werr)
			}
			res.ProposedTOC = proposedPath
			return res, fmt.Errorf("%w\nNo manuscript files were imported.\nA proposed table of contents was written to:\n%s\nReview the file and run:\nstory import md %s --toc %s",
				err, proposedPath, folder, proposedPath)
		}
		if err != nil {
			return nil, err
		}
	}

	// Parse everything in memory before any canonical write so that a
	// failed import never partially mutates the manuscript.
	markers := p.Config.Manuscript.SceneBreakMarkers
	chapters := make([]*manuscript.Chapter, 0, len(sources))
	paragraphs := 0
	for i, src := range sources {
		raw, err := os.ReadFile(filepath.Join(folder, filepath.FromSlash(src.File)))
		if err != nil {
			return nil, fmt.Errorf("read chapter source %s: %w", src.File, err)
		}
		headingTitle, blocks := manuscript.ParseSource(string(raw), markers)
		order := i + 1
		title := src.Title
		if title == "" {
			title = headingTitle
		}
		if title == "" {
			title = titleFromFilename(src.File)
		}
		id := ids.ChapterID(order)
		ch := &manuscript.Chapter{
			ID:        id,
			Order:     order,
			Title:     title,
			File:      "chapters/" + id + ".md",
			SourceKey: src.File,
			Blocks:    blocks,
		}
		for bi := range ch.Blocks {
			if ch.Blocks[bi].Type.Citable() {
				ch.Blocks[bi].ParagraphID = ids.NewParagraphID()
				paragraphs++
			}
		}
		chapters = append(chapters, ch)
	}
	res.Chapters = len(chapters)
	res.Paragraphs = paragraphs

	report := Report{
		RunID:      runID,
		Type:       "md",
		SourcePath: absFolder,
		ImportedAt: time.Now().Format(time.RFC3339),
		Chapters:   res.Chapters,
		Paragraphs: res.Paragraphs,
		Warnings:   append([]string{}, res.Warnings...),
		Status:     "completed",
	}

	if opts.DryRun {
		report.Status = "dry-run"
		if err := writeImportRecord(p, runID, report, res.Warnings); err != nil {
			return nil, err
		}
		return res, nil
	}

	// Preserve the original source files.
	originalDir := p.Path(filepath.Join(project.SourceOriginalDir, runID))
	if err := os.MkdirAll(originalDir, 0o755); err != nil {
		return nil, fmt.Errorf("preserve source: %w", err)
	}
	for _, src := range sources {
		if err := copyFile(filepath.Join(folder, filepath.FromSlash(src.File)), filepath.Join(originalDir, filepath.Base(src.File))); err != nil {
			return nil, fmt.Errorf("preserve source %s: %w", src.File, err)
		}
	}
	if manifestPath != "" {
		if err := copyFile(manifestPath, filepath.Join(originalDir, filepath.Base(manifestPath))); err != nil {
			return nil, fmt.Errorf("preserve manifest: %w", err)
		}
	}

	// Replace the canonical manuscript.
	chaptersDir := p.Path(project.ChaptersDir)
	if opts.Replace {
		if err := os.RemoveAll(chaptersDir); err != nil {
			return nil, fmt.Errorf("replace manuscript: %w", err)
		}
	}
	if err := os.MkdirAll(chaptersDir, 0o755); err != nil {
		return nil, fmt.Errorf("write manuscript: %w", err)
	}
	toc := manuscript.TOC{Version: 1}
	for _, ch := range chapters {
		if err := manuscript.WriteChapter(p.Path(project.ManuscriptDir), ch); err != nil {
			return nil, err
		}
		toc.Chapters = append(toc.Chapters, manuscript.TOCEntry{
			ID: ch.ID, Order: ch.Order, Title: ch.Title, File: ch.File, SourceKey: ch.SourceKey,
		})
	}
	if err := manuscript.SaveTOC(p.Path(project.TOCPath), toc); err != nil {
		return nil, err
	}

	// Update the project title when the source provides one.
	newTitle := opts.Title
	if newTitle == "" {
		newTitle = manifestTitle
	}
	if newTitle != "" && newTitle != p.Config.Title {
		p.Config.Title = newTitle
		if err := config.Save(p.Dir, p.Config); err != nil {
			return nil, err
		}
	}

	if err := writeImportRecord(p, runID, report, res.Warnings); err != nil {
		return nil, err
	}

	// Rebuild the SQLite index from the canonical files just written.
	if err := store.Rebuild(p); err != nil {
		return nil, err
	}
	s, err := store.Open(p.Path(project.IndexPath))
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err := s.RecordImport(store.ImportRow{
		RunID:      report.RunID,
		Type:       report.Type,
		SourcePath: report.SourcePath,
		ImportedAt: report.ImportedAt,
		Chapters:   report.Chapters,
		Paragraphs: report.Paragraphs,
		Status:     report.Status,
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// writeAmbiguityRecord writes report.json, warnings.txt, and a proposed
// manifest for an ambiguous import, and returns the proposed manifest path.
func writeAmbiguityRecord(p *project.Project, runID, sourcePath string, files []string, cause error) (string, error) {
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
	if err := os.WriteFile(filepath.Join(dir, "warnings.txt"), []byte(cause.Error()+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write import record: %w", err)
	}

	proposed := filepath.Join(dir, "proposed-toc.toml")
	var b []byte
	b = append(b, "# Proposed table of contents.\n"...)
	b = append(b, "# The chapter order below is UNCONFIRMED: files are listed in\n"...)
	b = append(b, "# lexicographic order, which may not be the intended reading order.\n"...)
	b = append(b, "# Review, reorder, and re-run:\n"...)
	b = append(b, fmt.Sprintf("#   story import md <folder> --toc %s\n\n", proposed)...)
	b = append(b, "version = 1\n"...)
	for _, f := range files {
		b = append(b, fmt.Sprintf("\n[[chapter]]\nfile = %q\ntitle = %q\n", f, titleFromFilename(f))...)
	}
	if err := os.WriteFile(proposed, b, 0o644); err != nil {
		return "", fmt.Errorf("write proposed toc: %w", err)
	}
	return proposed, nil
}

func writeImportRecord(p *project.Project, runID string, report Report, warnings []string) error {
	dir := p.Path(filepath.Join(project.ImportRecordsDir, runID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("write import record: %w", err)
	}
	if err := writeJSON(filepath.Join(dir, "report.json"), report); err != nil {
		return err
	}
	if len(warnings) > 0 {
		var b []byte
		for _, w := range warnings {
			b = append(b, w...)
			b = append(b, '\n')
		}
		if err := os.WriteFile(filepath.Join(dir, "warnings.txt"), b, 0o644); err != nil {
			return fmt.Errorf("write import record: %w", err)
		}
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
