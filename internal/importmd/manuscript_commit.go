package importmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nusapuksic/story/internal/manuscript"
	"github.com/nusapuksic/story/internal/project"
)

type canonicalManuscriptCommit struct {
	project  *project.Project
	runID    string
	chapters []*manuscript.Chapter
	replace  bool

	root               string
	stageManuscriptDir string
	backupDir          string

	movedChapters     bool
	movedTOC          bool
	backedUpModel     bool
	installedChapters bool
	installedTOC      bool
}

func commitCanonicalManuscript(
	p *project.Project,
	runID string,
	chapters []*manuscript.Chapter,
	replace bool,
) error {
	commit := canonicalManuscriptCommit{
		project:  p,
		runID:    runID,
		chapters: chapters,
		replace:  replace,
	}
	if err := commit.stage(); err != nil {
		if commit.root != "" {
			_ = os.RemoveAll(commit.root)
		}
		return err
	}
	return commit.apply()
}

func (c *canonicalManuscriptCommit) stage() error {
	if c.runID == "" || filepath.Base(c.runID) != c.runID {
		return fmt.Errorf("stage manuscript commit: invalid run id %q", c.runID)
	}

	c.root = c.project.Path(filepath.Join(project.StoryDir, "tmp", c.runID, "manuscript-commit"))
	c.stageManuscriptDir = filepath.Join(c.root, "stage", project.ManuscriptDir)
	c.backupDir = filepath.Join(c.root, "backup")

	if err := os.RemoveAll(c.root); err != nil {
		return fmt.Errorf("stage manuscript commit: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(c.stageManuscriptDir, "chapters"), 0o755); err != nil {
		return fmt.Errorf("stage manuscript commit: %w", err)
	}

	toc := manuscript.TOC{Version: 1}
	for _, ch := range c.chapters {
		if err := validateCanonicalChapterPath(ch.File); err != nil {
			return err
		}
		if err := manuscript.WriteChapter(c.stageManuscriptDir, ch); err != nil {
			return err
		}
		toc.Chapters = append(toc.Chapters, manuscript.TOCEntry{
			ID:        ch.ID,
			Order:     ch.Order,
			Title:     ch.Title,
			File:      ch.File,
			SourceKey: ch.SourceKey,
		})
	}
	if err := manuscript.SaveTOC(filepath.Join(c.stageManuscriptDir, "toc.toml"), toc); err != nil {
		return err
	}
	if err := c.validateStaged(); err != nil {
		return err
	}
	return nil
}

func validateCanonicalChapterPath(file string) error {
	clean := path.Clean(file)
	if clean != file || strings.HasPrefix(clean, "../") || clean == "." || strings.Contains(clean, `\`) {
		return fmt.Errorf("stage manuscript commit: invalid chapter file path %q", file)
	}
	if !strings.HasPrefix(clean, "chapters/") {
		return fmt.Errorf("stage manuscript commit: invalid chapter file path %q", file)
	}
	return nil
}

func (c *canonicalManuscriptCommit) validateStaged() error {
	toc, err := manuscript.LoadTOC(filepath.Join(c.stageManuscriptDir, "toc.toml"))
	if err != nil {
		return err
	}
	markers := c.project.Config.Manuscript.SceneBreakMarkers
	for _, entry := range toc.Chapters {
		if _, err := manuscript.LoadChapter(c.stageManuscriptDir, entry, markers); err != nil {
			return err
		}
	}
	return nil
}

func (c *canonicalManuscriptCommit) apply() error {
	if err := os.MkdirAll(c.project.Path(project.ManuscriptDir), 0o755); err != nil {
		return fmt.Errorf("commit manuscript: %w", err)
	}
	if err := os.MkdirAll(c.backupDir, 0o755); err != nil {
		return fmt.Errorf("commit manuscript: %w", err)
	}

	chaptersDir := c.project.Path(project.ChaptersDir)
	tocPath := c.project.Path(project.TOCPath)
	modelDir := c.project.Path(project.ModelDir)

	var err error
	if c.movedChapters, err = moveIfExists(chaptersDir, filepath.Join(c.backupDir, "chapters")); err != nil {
		return c.fail(fmt.Errorf("commit manuscript: move current chapters: %w", err))
	}
	if c.movedTOC, err = moveIfExists(tocPath, filepath.Join(c.backupDir, "toc.toml")); err != nil {
		return c.fail(fmt.Errorf("commit manuscript: move current toc: %w", err))
	}
	if c.replace {
		if c.backedUpModel, err = copyDirIfExists(modelDir, filepath.Join(c.backupDir, "model")); err != nil {
			return c.fail(fmt.Errorf("commit manuscript: back up current model: %w", err))
		}
	}

	if err := os.Rename(filepath.Join(c.stageManuscriptDir, "chapters"), chaptersDir); err != nil {
		return c.fail(fmt.Errorf("commit manuscript: install chapters: %w", err))
	}
	c.installedChapters = true
	if err := os.Rename(filepath.Join(c.stageManuscriptDir, "toc.toml"), tocPath); err != nil {
		return c.fail(fmt.Errorf("commit manuscript: install toc: %w", err))
	}
	c.installedTOC = true
	if c.replace {
		if err := c.project.ResetModelFiles(); err != nil {
			return c.fail(fmt.Errorf("commit manuscript: reset model files: %w", err))
		}
	}

	if err := os.RemoveAll(c.root); err != nil {
		return fmt.Errorf("commit manuscript: cleanup: %w", err)
	}
	return nil
}

func moveIfExists(src, dst string) (bool, error) {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

func copyDirIfExists(src, dst string) (bool, error) {
	info, err := os.Stat(src)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", src)
	}
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type().IsRegular() {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return copyFile(path, target)
		}
		return fmt.Errorf("unsupported model entry %s", path)
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (c *canonicalManuscriptCommit) fail(cause error) error {
	if err := c.rollback(); err != nil {
		return errors.Join(cause, err)
	}
	_ = os.RemoveAll(c.root)
	return cause
}

func (c *canonicalManuscriptCommit) rollback() error {
	var errs []error
	chaptersDir := c.project.Path(project.ChaptersDir)
	tocPath := c.project.Path(project.TOCPath)
	modelDir := c.project.Path(project.ModelDir)

	if c.installedChapters {
		if err := os.RemoveAll(chaptersDir); err != nil {
			errs = append(errs, fmt.Errorf("rollback manuscript chapters: %w", err))
		}
	}
	if c.installedTOC {
		if err := os.Remove(tocPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("rollback manuscript toc: %w", err))
		}
	}
	if c.replace && c.backedUpModel {
		if err := os.RemoveAll(modelDir); err != nil {
			errs = append(errs, fmt.Errorf("rollback manuscript model: %w", err))
		}
	}

	if c.movedChapters {
		if err := os.Rename(filepath.Join(c.backupDir, "chapters"), chaptersDir); err != nil {
			errs = append(errs, fmt.Errorf("restore manuscript chapters: %w", err))
		}
	}
	if c.movedTOC {
		if err := os.Rename(filepath.Join(c.backupDir, "toc.toml"), tocPath); err != nil {
			errs = append(errs, fmt.Errorf("restore manuscript toc: %w", err))
		}
	}
	if c.backedUpModel {
		if err := os.Rename(filepath.Join(c.backupDir, "model"), modelDir); err != nil {
			errs = append(errs, fmt.Errorf("restore manuscript model: %w", err))
		}
	}
	return errors.Join(errs...)
}
