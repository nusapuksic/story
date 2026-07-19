// Package ids generates the stable identifiers used across a story project.
// Identifiers are always produced by deterministic application code; they are
// never accepted from external systems or model output.
package ids

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewULID returns a new ULID string using crypto/rand entropy.
func NewULID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader).String()
}

// NewProjectID returns a new project identifier.
func NewProjectID() string {
	return NewULID()
}

// NewParagraphID returns a new paragraph identifier of the form p-<ULID>.
func NewParagraphID() string {
	return "p-" + NewULID()
}

// NewImportRunID returns a new import run identifier of the form import-<ULID>.
func NewImportRunID() string {
	return "import-" + NewULID()
}

// ChapterID returns the stable chapter identifier for a 1-based order,
// e.g. ch-0001.
func ChapterID(order int) string {
	return fmt.Sprintf("ch-%04d", order)
}
