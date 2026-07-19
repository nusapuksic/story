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
```

Markdown-folder import is deterministic: it uses an explicit `toc.toml`/`book.toml` manifest when present (or `--toc <path>`), and otherwise requires unique numeric filename prefixes (`01-road.md`, `2-house.md`). When ordering is ambiguous, the import fails without touching the canonical manuscript and writes a proposed table of contents under `source/import-records/<run-id>/proposed-toc.toml` for review.

The SQLite index at `.story/index.sqlite` is a rebuildable projection of the canonical project files; deleting it never loses data (`story index rebuild` reconstructs it).

## Development

```
gofmt -l .
go vet ./...
go test ./...
``` 
