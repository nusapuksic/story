# story
A local-first Go CLI that compiles a fiction manuscript into a layered, source-addressable story model.

The authoritative specification is [docs/cli-spec.md](docs/cli-spec.md).

## Build

```
go build ./cmd/story
```

## Usage

```
story init ./my-novel --title "My Novel"
story --project ./my-novel import md ./chapters
story --project ./my-novel status
story --project ./my-novel inspect chapter ch-0001
story --project ./my-novel inspect paragraph p-<ULID>
story --project ./my-novel import report
story --project ./my-novel index rebuild
story --project ./my-novel compile
story --project ./my-novel compile --layer scenes
story --project ./my-novel compile --layer scene-cards
story --project ./my-novel search "farmhouse fire"
story --project ./my-novel search "Mara" --chapter ch-0004 --limit 10
story --project ./my-novel ask "What does Mara know when she enters the farmhouse?"
story --project ./my-novel ask --mode continuity "What has the detective already discovered?"
story --project ./my-novel ask --mode style "How is the fog used as a motif?"
```

Markdown-folder import is deterministic: it uses an explicit `toc.toml`/`book.toml` manifest when present (or `--toc <path>`), and otherwise requires unique numeric filename prefixes (`01-road.md`, `2-house.md`). When ordering is ambiguous, the import fails without touching the canonical manuscript and writes a proposed table of contents under `source/import-records/<run-id>/proposed-toc.toml` for review.

The SQLite index at `.story/index.sqlite` is a rebuildable projection of the canonical project files; deleting it never loses data (`story index rebuild` reconstructs it).

`story compile` builds the story model from the canonical manuscript in layers (`scenes`, `scene-cards`). It requires a configured LLM provider (see `docs/cli-spec.md`).

`story search` runs full-text search over indexed paragraphs and scene cards. The FTS index is populated during indexing; run `story index rebuild` to refresh it.

`story ask` retrieves relevant evidence from the index, sends it to the configured discussion model, validates cited paragraph identifiers, and returns an answer with provenance. Available modes: `recall` (default), `continuity`, `interpretation`, `style`, `development`. When the index does not contain enough evidence to answer, the command exits with code 40.

## Development

```
gofmt -l .
go vet ./...
go test ./...
``` 
