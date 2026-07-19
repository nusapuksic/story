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

// NewSceneID returns a new scene identifier of the form sc-<ULID>.
func NewSceneID() string {
	return "sc-" + NewULID()
}

// NewCompileRunID returns a new compilation run identifier of the form compile-<ULID>.
func NewCompileRunID() string {
	return "compile-" + NewULID()
}

// NewTaskID returns a new compilation task identifier of the form task-<ULID>.
func NewTaskID() string {
	return "task-" + NewULID()
}

// NewQueryRunID returns a new query run identifier of the form query-<ULID>.
func NewQueryRunID() string {
	return "query-" + NewULID()
}

// ChapterID returns the stable chapter identifier for a 1-based order,
// e.g. ch-0001.
func ChapterID(order int) string {
	return fmt.Sprintf("ch-%04d", order)
}
