package manuscript

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// TOC is the authoritative canonical table of contents (manuscript/toc.toml).
type TOC struct {
	Version  int        `toml:"version"`
	Chapters []TOCEntry `toml:"chapter"`
}

// TOCEntry is one chapter entry in the canonical table of contents.
type TOCEntry struct {
	ID        string `toml:"id"`
	Order     int    `toml:"order"`
	Title     string `toml:"title"`
	File      string `toml:"file"`
	SourceKey string `toml:"source_key"`
}

// LoadTOC reads a canonical toc.toml.
func LoadTOC(path string) (TOC, error) {
	var t TOC
	if _, err := toml.DecodeFile(path, &t); err != nil {
		return TOC{}, fmt.Errorf("load %s: %w", path, err)
	}
	if t.Version != 1 {
		return TOC{}, fmt.Errorf("load %s: unsupported toc version %d", path, t.Version)
	}
	return t, nil
}

// SaveTOC writes a canonical toc.toml atomically.
func SaveTOC(path string, t TOC) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".toc.toml.*")
	if err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if err := toml.NewEncoder(tmp).Encode(t); err != nil {
		tmp.Close()
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}
