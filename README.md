# story
A local-first Go CLI that compiles a fiction manuscript into a layered, source-addressable story model.

The authoritative specification is [docs/cli-spec.md](docs/cli-spec.md).

## Releases

Prebuilt release archives are published on the [GitHub Releases](https://github.com/nusapuksic/story/releases) page.

Download the archive for your platform and architecture:

* `story-v<version>-windows-amd64.zip`
* `story-v<version>-linux-amd64.zip`
* `story-v<version>-linux-arm64.zip`
* `story-v<version>-darwin-amd64.zip`
* `story-v<version>-darwin-arm64.zip`

Each archive includes the `story` binary, `README.md`, and `LICENSE`. For beta testing, the simplest path is to unzip the archive, open a terminal in the extracted folder, and run the executable from there:

* Windows PowerShell: `.\story.exe`
* macOS/Linux: `./story`

Installing the binary on your `PATH` is optional. If you do install it, you can drop the local prefix and type `story` from any folder.

Each release also includes `SHA256SUMS.txt` for verifying downloaded assets:

```
sha256sum -c SHA256SUMS.txt
```

On Windows PowerShell, use `Get-FileHash` and compare the output with the matching line in `SHA256SUMS.txt`:

```powershell
Get-FileHash .\story-v<version>-windows-amd64.zip -Algorithm SHA256
```

## Build

```
go build ./cmd/story
```

## Demo Flow

Start with a manuscript in a Markdown file saved somewhere on your machine.
Import itself does not upload your manuscript. It creates a local project copy and normalizes it into chapters and paragraphs. The `compile` command then builds scenes, scene cards, entities, verification records, and summaries that manage context for the LLM. With the default local setup, `compile` and `ask` talk to a model server on your machine; if you configure a remote provider, excerpted evidence is sent to that endpoint.

The commands below assume you are running the binary from the folder you extracted or built it into, without installing it on your `PATH`.

A typical Windows PowerShell first run looks like this:

```powershell
.\story.exe init "C:\Users\YourName\story-projects\my-novel" --title "My Novel" --model <your-local-model-id>
.\story.exe --project "C:\Users\YourName\story-projects\my-novel" import md "C:\Users\YourName\Documents\my-novel\draft.md"
.\story.exe --project "C:\Users\YourName\story-projects\my-novel" status
.\story.exe --project "C:\Users\YourName\story-projects\my-novel" inspect chapter ch-0001
.\story.exe --project "C:\Users\YourName\story-projects\my-novel" llm doctor
.\story.exe --project "C:\Users\YourName\story-projects\my-novel" compile
.\story.exe --project "C:\Users\YourName\story-projects\my-novel" ask "What does the protagonist want in the opening chapters?"
```

On macOS/Linux, the same flow looks like this:

```bash
./story init ~/story-projects/my-novel --title "My Novel" --model <your-local-model-id>
./story --project ~/story-projects/my-novel import md ~/Documents/my-novel/draft.md
./story --project ~/story-projects/my-novel status
./story --project ~/story-projects/my-novel inspect chapter ch-0001
./story --project ~/story-projects/my-novel llm doctor
./story --project ~/story-projects/my-novel compile
./story --project ~/story-projects/my-novel ask "What does the protagonist want in the opening chapters?"
```

The import preserves your original files under `source/original/`, normalizes the working manuscript into the project folder, and builds a rebuildable local index for `status`, `inspect`, `search`, `compile`, and `ask`.

## Connect a Local or LAN LLM

`story compile` and `story ask` use the LLM settings in the project `story.toml`. New projects are initialized with a local OpenAI-compatible provider. By default, model names are blank until you choose the model running on your machine or on a machine reachable on your local network, but `story init --model <model-id>` pre-fills the same model for every LLM role.

1. Start a model server that exposes an OpenAI-compatible API.

   Ollama's default loopback endpoint matches the generated config:

   ```
   ollama serve
   ```

   LM Studio or another OpenAI-compatible server can also work; use its `/v1` base URL, for example `http://127.0.0.1:1234/v1`.

   If the server runs on another machine, configure it to listen on your LAN and use that machine's private IP in `story.toml`, for example `http://192.168.1.50:11434/v1`. Make sure the server firewall allows the connection only from machines you trust.

2. List the model IDs exposed by that server.

   ```
   curl http://127.0.0.1:11434/v1/models
   ```

   For a LAN server, replace `127.0.0.1` with the server's private IP.
   Use the exact value from each model object's `id` field. That is the name `story` expects in `--model` and in `story.toml`.

   Example response:

   ```json
   {
     "data": [
       { "id": "llama3.1:8b" },
       { "id": "mistral-small3.1:24b" }
     ]
   }
   ```

   In that case, valid choices would be `llama3.1:8b` or `mistral-small3.1:24b`.

   For example:

   ```powershell
   .\story.exe init "C:\Users\YourName\story-projects\my-novel" --title "My Novel" --model llama3.1:8b
   ```

   ```bash
   ./story init ~/story-projects/my-novel --title "My Novel" --model llama3.1:8b
   ```

3. If you did not pass `--model` during project setup, edit the project's `story.toml` and set each role to a model ID returned by the server. A minimal one-model local setup can use the same model for all roles:

   ```toml
   [llm]
     default_provider = "local"

     [llm.providers.local]
       type = "openai-compatible"
       base_url = "http://127.0.0.1:11434/v1" # or a private LAN IP such as http://192.168.1.50:11434/v1
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

4. Verify the connection for the project:

   Windows PowerShell:

   ```powershell
   .\story.exe --project .\my-novel llm doctor
   ```

   macOS/Linux:

   ```
   ./story --project ./my-novel llm doctor
   ```

   Once this passes, `story compile`, `story compile --layer scene-cards`, `story compile --layer entities`, `story compile --layer verification`, `story compile --layer summaries`, and `story ask` can call the configured model. Full `story compile` uses the verification role when `[compile].verification = true`. `story compile --layer scenes` can still build deterministic scene boundaries without an LLM.

## Usage

These examples use `./story` for a binary in the current folder. In Windows PowerShell, use `.\story.exe` instead. If you installed the binary on your `PATH`, use `story`.

```
./story init ./my-novel --title "My Novel" --model <your-local-model-id>
./story --project ./my-novel import md ./chapters
./story --project ./my-novel import md ./manuscript.md
./story --project ./my-novel status
./story --project ./my-novel doctor
./story --project ./my-novel inspect chapter ch-0001
./story --project ./my-novel inspect paragraph p-<ULID>
./story --project ./my-novel inspect summary book
./story --project ./my-novel inspect summary ch-0001
./story --project ./my-novel import report
./story --project ./my-novel index rebuild
./story --project ./my-novel compile
./story --project ./my-novel compile status
./story --project ./my-novel compile --layer scenes
./story --project ./my-novel compile --layer scene-cards
./story --project ./my-novel compile --layer entities
./story --project ./my-novel compile --layer verification
./story --project ./my-novel compile --layer summaries
./story --project ./my-novel search "farmhouse fire"
./story --project ./my-novel search "Mara" --chapter ch-0004 --limit 10
./story --project ./my-novel ask "What does Mara know when she enters the farmhouse?"
./story --project ./my-novel ask --mode continuity "What has the detective already discovered?"
./story --project ./my-novel ask --mode style "How is the fog used as a motif?"
```

Markdown import accepts either a folder of chapter files or one continuous `.md` manuscript. If `--project` points at a directory without `story.toml`, `story import md` initializes the default project layout and generated config before importing. Folder import is deterministic: it uses an explicit `toc.toml`/`book.toml` manifest when present (or `--toc <path>`), and otherwise requires unique numeric filename prefixes (`01-road.md`, `2-house.md`). Continuous-file import splits on deterministic chapter headings, or imports the whole file as one chapter with `--single-chapter`. When ordering or chapter boundaries are ambiguous, the import fails without touching the canonical manuscript and writes an actionable report under `source/import-records/<run-id>/` for review.

The SQLite index at `.story/index.sqlite` is a rebuildable projection of the canonical project files; deleting it never loses data (`story index rebuild` reconstructs it).

`story compile` builds the story model from the canonical manuscript in layers: `scenes`, `scene-cards`, `entities`, `verification`, and `summaries`. `scenes` can run from explicit manuscript scene breaks without an LLM; model-assisted scene detection and the other compile layers require configured LLM roles (see `docs/cli-spec.md`).

`story search` runs full-text search over indexed paragraphs and scene cards. The FTS index is populated during indexing; run `story index rebuild` to refresh it.

## Prompts

Every project has editable prompt templates under `prompts/`:

* `scene-boundaries.md`
* `scene-extraction.md`
* `entity-resolution.md`
* `record-verification.md`
* `chapter-summary.md`
* `book-summary.md`
* `answer-question.md`

Compiler tasks and `story ask` load these project prompts at runtime. If a prompt file is missing or blank, `story` falls back to its embedded default. When customizing a prompt, keep the `<!-- prompt_version: ... -->` marker so generated records and run tasks keep useful provenance.

## Generated Artifacts

Canonical generated records live in `model/` as JSONL files:

* `scenes.jsonl` stores scene boundaries and scene cards, including verification updates.
* `entities.jsonl` stores extracted candidate entities.
* `mentions.jsonl` stores paragraph-level entity mentions.
* `summaries.jsonl` stores chapter and book summaries.

Each compile run also writes `.story/runs/<run-id>/` with `run.json`, `summary.json`, task records, errors when present, and raw model responses. These run files are provenance and debugging artifacts; the rebuildable SQLite index is still derived from the canonical project files.

`story ask` retrieves relevant evidence from the index, sends it to the configured discussion model, validates cited paragraph identifiers, and returns an answer with provenance. Available modes: `recall` (default), `continuity`, `interpretation`, `style`, `development`. When the index does not contain enough evidence to answer, the command exits with code 40.

## Development

```
gofmt -l .
go vet ./...
go test ./...
```
