# story
A local-first Go CLI that compiles a fiction manuscript into a layered, source-addressable story model.

The authoritative specification is [docs/cli-spec.md](docs/cli-spec.md).

## Build

```
go build ./cmd/story
```

## Connect a Local LLM

`story compile` and `story ask` use the LLM settings in the project `story.toml`. New projects are initialized with a local OpenAI-compatible provider, but the model names are intentionally blank until you choose the model running on your machine.

1. Start a local model server that exposes an OpenAI-compatible API.

   Ollama's default local endpoint matches the generated config:

   ```
   ollama serve
   ```

   LM Studio or another OpenAI-compatible server can also work; use its local `/v1` base URL, for example `http://127.0.0.1:1234/v1`.

2. List the model IDs exposed by that server.

   ```
   curl http://127.0.0.1:11434/v1/models
   ```

3. Edit the project's `story.toml` and set each role to a model ID returned by the server. A minimal one-model local setup can use the same model for all roles:

   ```toml
   [llm]
     default_provider = "local"

     [llm.providers.local]
       type = "openai-compatible"
       base_url = "http://127.0.0.1:11434/v1"
       api_key_env = ""
       request_timeout_seconds = 300

     [llm.roles.extraction]
       provider = "local"
       model = "<your-local-model-id>"
       prompt_profile = "conservative"

     [llm.roles.verification]
       provider = "local"
       model = "<your-local-model-id>"
       prompt_profile = "strict-evidence"

     [llm.roles.discussion]
       provider = "local"
       model = "<your-local-model-id>"
       prompt_profile = "literary-analysis"
   ```

   If your endpoint requires an API key, store the environment variable name in `api_key_env`; do not put the key itself in `story.toml`.

4. Verify the connection from the project directory:

   ```
   story --project ./my-novel llm doctor
   ```

   Once this passes, `story compile`, `story compile --layer scene-cards`, and `story ask` can call the configured local model. `story compile --layer scenes` can still build deterministic scene boundaries without an LLM.

## Usage

```
story init ./my-novel --title "My Novel"
story --project ./my-novel import md ./chapters
story --project ./my-novel import md ./manuscript.md
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

Markdown import accepts either a folder of chapter files or one continuous `.md` manuscript. If `--project` points at a directory without `story.toml`, `story import md` initializes the default project layout and generated config before importing. Folder import is deterministic: it uses an explicit `toc.toml`/`book.toml` manifest when present (or `--toc <path>`), and otherwise requires unique numeric filename prefixes (`01-road.md`, `2-house.md`). Continuous-file import splits on deterministic chapter headings, or imports the whole file as one chapter with `--single-chapter`. When ordering or chapter boundaries are ambiguous, the import fails without touching the canonical manuscript and writes an actionable report under `source/import-records/<run-id>/` for review.

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
