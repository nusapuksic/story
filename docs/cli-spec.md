Story CLI Specification

Version: 0.1
Status: Implementation draft
Implementation language: Go
Primary purpose: Compile a novel manuscript into a local, layered, source-addressable story model that can be queried using a configured local or remote LLM.

⸻

1. Purpose

story is a local-first command-line application for importing, decomposing, indexing, and discussing long-form fiction.

It accepts either:

1. a .docx manuscript; or
2. a folder of Markdown files, normally one file per chapter.

The imported manuscript is normalized into a canonical project folder. The application then uses deterministic parsing and a configured LLM to construct a layered representation of the novel:

* document structure;
* paragraphs;
* scenes;
* entities;
* events and claims;
* character knowledge and state changes;
* chapter and whole-book summaries;
* evidence links back to the exact manuscript paragraphs.

The LLM is treated as a fallible extraction and reasoning component. It does not control identifiers, source ordering, file writes, record status, or citation integrity.

⸻

2. Product principles

2.1 The project folder is canonical

A project is a normal directory that can be copied, backed up, inspected, version-controlled, and opened without the original application.

SQLite indexes and embeddings are rebuildable projections. They are not the sole copy of the project.

2.2 Source text remains authoritative

Generated summaries and structured records never replace the manuscript.

Every concrete generated claim must cite one or more source paragraph identifiers.

2.3 Import order must be deterministic

The application must never guess chapter order when multiple plausible orderings exist.

When folder ordering is ambiguous, import fails and produces an actionable proposed table of contents.

2.4 Deterministic code owns structure

Ordinary code controls:

* file discovery;
* chapter ordering;
* paragraph ordering;
* identifier generation;
* source hashes;
* database writes;
* record validation;
* approval status;
* retrieval filters.

The LLM proposes:

* scene boundaries where not explicitly marked;
* scene summaries;
* entities and aliases;
* events;
* character-state changes;
* interpretations and unresolved questions.

2.5 Generated material has provenance

Every model-generated record retains:

* provider;
* model name;
* prompt version;
* input source identifiers;
* generation time;
* verification status;
* originating compilation run.

2.6 Local operation is the default

The application must work with a model server running on localhost.

Remote model providers may be supported, but must be explicitly configured.

⸻

3. Installation and invocation

The application is distributed as a single executable where practical:

story
story.exe

General syntax:

story [global-options] <command> [command-options]

Global options:

--project <path>    Project directory. Defaults to current directory.
--json              Emit machine-readable JSON.
--quiet             Suppress nonessential output.
--verbose           Include diagnostic information.
--no-color          Disable terminal colors.
--version           Print version.
--help              Print help.

The CLI should return stable exit codes and avoid interactive prompts unless explicitly requested.

⸻

4. Project structure

A project initialized with:

story init ./my-novel

produces:

my-novel/
  story.toml
  source/
    original/
    import-records/
  manuscript/
    toc.toml
    chapters/
  model/
    scenes.jsonl
    entities.jsonl
    mentions.jsonl
    claims.jsonl
    events.jsonl
    character-states.jsonl
    unresolved.jsonl
    summaries.jsonl
  reviews/
    decisions.jsonl
  prompts/
    scene-boundaries.md
    scene-extraction.md
    entity-resolution.md
    record-verification.md
    chapter-summary.md
    book-summary.md
    answer-question.md
  .story/
    index.sqlite
    cache/
    runs/
    locks/
    logs/

4.1 Canonical and rebuildable content

Canonical project content:

story.toml
source/
manuscript/
model/
reviews/
prompts/

Rebuildable content:

.story/index.sqlite
.story/cache/
.story/runs/
.story/logs/

Deleting .story/ must not destroy the manuscript or accepted story model.

⸻

5. Project configuration

The root configuration file is story.toml.

Example:

version = 1
project_id = "01JZK3H0EJG6M8BTRXQTRMW5AZ"
title = "The Unopened Letter"
language = "en"
[manuscript]
canonical_format = "markdown"
chapter_boundary = "hard"
scene_break_markers = ["***", "* * *", "---", "§"]
[compile]
target_context_tokens = 12000
maximum_output_tokens = 3000
window_overlap_paragraphs = 3
scene_detection = "hybrid"
verification = true
auto_accept_verified = false
temperature = 0.1
[llm]
default_provider = "local"
[llm.providers.local]
type = "openai-compatible"
base_url = "http://127.0.0.1:11434/v1"
api_key_env = ""
request_timeout_seconds = 300
[llm.roles.extraction]
provider = "local"
model = "configured-extraction-model"
prompt_profile = "conservative"
[llm.roles.verification]
provider = "local"
model = "configured-verification-model"
prompt_profile = "strict-evidence"
[llm.roles.discussion]
provider = "local"
model = "configured-discussion-model"
prompt_profile = "literary-analysis"
[embeddings]
enabled = false
provider = "local"
model = ""

API keys must not be stored directly in story.toml. Remote credentials are referenced through environment variables or operating-system credential storage.

⸻

6. Canonical manuscript representation

Regardless of source format, the working manuscript is normalized to:

manuscript/
  toc.toml
  chapters/
    ch-0001.md
    ch-0002.md
    ch-0003.md

The source files remain preserved under source/original/.

6.1 Table of contents

manuscript/toc.toml is authoritative.

version = 1
[[chapter]]
id = "ch-0001"
order = 1
title = "The Road"
file = "chapters/ch-0001.md"
source_key = "01-the-road.md"
[[chapter]]
id = "ch-0002"
order = 2
title = "The House"
file = "chapters/ch-0002.md"
source_key = "02-the-house.md"

Chapter identifiers are stable after import.

6.2 Paragraph identifiers

Every canonical manuscript paragraph receives a stable local identifier.

<!-- story:paragraph id="p-01JZK47F6T42NDB9V0DNE3BB3P" -->
Mara placed the unopened letter beneath the stove.

Paragraph metadata stored in SQLite includes:

{
  "id": "p-01JZK47F6T42NDB9V0DNE3BB3P",
  "chapter_id": "ch-0007",
  "ordinal": 34,
  "text_hash": "sha256:...",
  "source_file": "chapters/ch-0007.md",
  "source_line_start": 91,
  "source_line_end": 92
}

The identifier is independent of the paragraph’s current ordinal. Reordering a paragraph must not automatically change its identifier.

6.3 Structural block types

The parser recognizes:

heading
paragraph
blockquote
list
scene_break
epigraph
embedded_document
unknown

The MVP must preserve:

* plain text;
* chapter headings;
* ordinary paragraphs;
* basic emphasis;
* blockquotes;
* explicit scene-break markers.

Complex layout, floating text boxes, footnotes, tracked changes, and images may be preserved as source attachments without full semantic import in v0.1.

⸻

7. Import commands

7.1 Initialize a project

story init <directory>

Options:

--title <title>
--language <code>
--force

Behavior:

1. create the project directory;
2. generate project_id;
3. write default configuration;
4. create canonical directories;
5. initialize SQLite;
6. copy default prompts.

The command must fail when the destination is nonempty unless --force is provided.

⸻

7.2 Import DOCX

story import docx <file.docx>

Options:

--chapter-style <style>
--chapter-regex <regex>
--single-chapter
--title <title>
--replace
--dry-run

DOCX import procedure

1. Validate that the input is a readable DOCX archive.
2. Copy the original file to source/original/.
3. Extract document paragraphs and basic formatting.
4. identify candidate chapter boundaries;
5. produce an import plan;
6. reject ambiguous plans;
7. normalize accepted chapters to Markdown;
8. assign chapter and paragraph identifiers;
9. write manuscript/toc.toml;
10. build the source index;
11. write an import report.

Chapter-boundary precedence

The importer checks, in order:

1. explicit --chapter-style;
2. configured DOCX styles;
3. headings such as Heading 1;
4. explicit --chapter-regex;
5. conservative built-in patterns such as:

Chapter 1
Chapter One
CHAPTER I
Part Two
Prologue
Epilogue

A DOCX table of contents may be used as a hint, but not as sole authority because it may be stale.

Ambiguity policy

Import must fail when:

* no chapter boundaries are found and --single-chapter was not supplied;
* several heading styles produce materially different chapter structures;
* duplicate chapter boundaries occur;
* a substantial amount of text falls outside all proposed chapters;
* chapter headings cannot be ordered deterministically.

The importer writes:

source/import-records/<run-id>/
  report.json
  proposed-toc.toml
  warnings.txt

No partial canonical manuscript is written after an ambiguous import.

Dry run

story import docx manuscript.docx --dry-run

The command performs detection and writes an import report without modifying the canonical manuscript.

⸻

7.3 Import a Markdown folder

story import md <folder>

Options:

--toc <path>
--pattern <glob>
--title <title>
--replace
--dry-run

The expected input is one Markdown file per chapter.

Authoritative manifest mode

The importer first looks for:

toc.toml
book.toml

An explicit --toc path takes precedence.

Example source manifest:

version = 1
title = "The Unopened Letter"
[[chapter]]
file = "prologue.md"
title = "Prologue"
[[chapter]]
file = "01-road.md"
title = "The Road"
[[chapter]]
file = "02-house.md"
title = "The House"

The listed order is authoritative.

Every listed file must exist. Unlisted Markdown files produce a warning and are not imported.

Unambiguous implicit ordering

Without a manifest, automatic ordering is allowed only when all eligible files have unique numeric prefixes:

001-prologue.md
010-the-road.md
020-the-house.md

Accepted prefix pattern:

^([0-9]+)[-_. ]+

Ordering is numeric, not lexicographic.

These names are valid:

1-introduction.md
02-house.md
003_finale.md

These are ambiguous:

chapter-one.md
chapter-two.md
afterword.md

These are also ambiguous:

01-road.md
1-house.md

when both normalize to the same numeric order.

Ambiguous folder behavior

When ordering is ambiguous:

1. import fails;
2. no canonical manuscript is changed;
3. the tool writes a proposed manifest:

source/import-records/<run-id>/proposed-toc.toml

The proposal lists discovered files in lexicographic order but clearly marks the order as unconfirmed.

The user may edit it and run:

story import md ./chapters \
  --toc source/import-records/<run-id>/proposed-toc.toml

Files ignored by default

hidden files
README.md
LICENSE.md
files outside the selected glob
subdirectories unless explicitly listed in the manifest

⸻

8. Import record

Every import creates a record:

{
  "run_id": "import-01JZK...",
  "type": "docx",
  "source_path": "/home/user/book.docx",
  "source_hash": "sha256:...",
  "imported_at": "2026-07-19T15:00:00+02:00",
  "chapters": 24,
  "paragraphs": 4821,
  "warnings": [],
  "status": "completed"
}

The record is written to:

source/import-records/<run-id>/report.json

⸻

9. Layered story representation

The compiler constructs the representation incrementally.

Layer 0: Source snapshot

Contains:

* original imported files;
* source hashes;
* import reports;
* source-to-canonical mappings.

No model generation occurs at this layer.

Layer 1: Document structure

Contains:

* book;
* chapters;
* structural blocks;
* paragraphs;
* paragraph ordering;
* explicit scene breaks.

This layer is deterministic.

Layer 2: Scenes

A scene is a contiguous paragraph range within one chapter.

{
  "record_type": "scene",
  "id": "sc-01JZK...",
  "chapter_id": "ch-0007",
  "paragraph_start": "p-...",
  "paragraph_end": "p-...",
  "ordinal": 3,
  "boundary_source": "explicit",
  "status": "verified"
}

Scenes are append-only records in model/scenes.jsonl. A committed chapter snapshot is
declared with an explicit marker that replaces the previous committed snapshot for
that chapter, including empty replacements:

{
  "record_type": "chapter_snapshot",
  "chapter_id": "ch-0007",
  "scene_count": 0,
  "committed_at": "2024-06-01T12:00:00Z"
}

boundary_source values:

explicit
model
manual

A chapter boundary is a hard scene boundary in v0.1. Scenes do not span chapters.

Layer 3: Scene cards

Each scene receives a compact structured record:

{
  "scene_id": "sc-01JZK...",
  "title": "Mara hides Elias's letter",
  "summary": "Mara receives a letter from Elias but hides it without opening it.",
  "pov": ["entity-mara"],
  "participants": ["entity-mara", "entity-elias"],
  "locations": ["entity-farmhouse-kitchen"],
  "story_time": {
    "description": "Three days after the funeral",
    "certainty": "explicit"
  },
  "events": [
    "event-01JZK..."
  ],
  "unresolved": [
    "Why Mara refuses to open the letter"
  ],
  "evidence": [
    "p-...",
    "p-..."
  ],
  "generation": {
    "run_id": "compile-...",
    "model": "configured-extraction-model",
    "prompt_version": "scene-extraction-v1"
  },
  "status": "generated"
}

Layer 4: Entities and mentions

Entity types:

character
location
object
organization
group
document
event-concept
unknown

Entity record:

{
  "id": "entity-mara",
  "type": "character",
  "canonical_name": "Mara",
  "aliases": ["Mara Venn", "the archivist"],
  "status": "accepted"
}

Mention record:

{
  "entity_id": "entity-mara",
  "paragraph_id": "p-...",
  "surface_text": "the archivist",
  "confidence": 0.91
}

Entity resolution must preserve ambiguity. Two possible aliases are not merged solely because the model considers the merge plausible.

Layer 5: Narrative records

Narrative records use a common envelope:

{
  "id": "claim-01JZK...",
  "record_type": "character_belief",
  "statement": "Mara believes Elias caused the archive fire.",
  "subject": "entity-mara",
  "object": "event-archive-fire",
  "valid_from": "sc-...",
  "valid_until": null,
  "certainty": "explicit",
  "evidence": ["p-...", "p-..."],
  "contradicted_by": [],
  "status": "generated"
}

Initial record types:

event
world_fact
character_belief
character_knowledge
character_intention
relationship_change
reveal
contradiction
unresolved_thread
interpretation

The distinction between fact, belief, dialogue claim, rumor, and interpretation must be retained.

Layer 6: Synthesis

Contains:

* chapter summaries;
* character timelines;
* relationship progressions;
* unresolved-thread index;
* whole-book orientation summary;
* tentative motifs and themes.

Synthesis records cite lower-level records and source paragraphs.

They must not cite only another summary.

⸻

10. Compilation pipeline

Compilation is initiated with:

story compile

Optional scope:

story compile --chapter ch-0007
story compile --layer scenes
story compile --from scenes --to claims
story compile --resume
story compile --force

10.1 Pipeline stages

validate project
    ↓
index manuscript
    ↓
detect scene boundaries
    ↓
construct scenes
    ↓
extract scene cards
    ↓
extract entities and mentions
    ↓
resolve aliases conservatively
    ↓
extract narrative records
    ↓
verify evidence
    ↓
construct chapter synthesis
    ↓
construct whole-book indexes

Each stage is independently resumable.

10.2 Deterministic preparation

Before calling the LLM, the compiler:

1. reads canonical chapter order;
2. verifies paragraph IDs;
3. calculates text hashes;
4. detects explicit scene breaks;
5. calculates approximate token counts;
6. creates bounded model windows;
7. assigns immutable run-local task IDs.

10.3 Window construction

A model request must never split a paragraph.

Default target:

8,000–12,000 input tokens

Windows are scoped to one chapter.

When a chapter exceeds the configured context size:

* split on explicit scene breaks where available;
* otherwise split between paragraphs;
* include the configured paragraph overlap;
* preserve absolute paragraph IDs.

Example:

window 1: p-001 through p-085
window 2: p-083 through p-166

The overlap is used only for reconciliation. Duplicate extracted records must be deduplicated before promotion.

10.4 Scene-boundary detection

Boundary precedence:

1. explicit scene-break block;
2. manually recorded boundary;
3. LLM-proposed boundary;
4. chapter end.

For model detection, the LLM receives paragraph IDs and paragraph text and returns only candidate boundaries:

{
  "boundaries": [
    {
      "after_paragraph_id": "p-...",
      "reason": "location and point-of-view shift",
      "confidence": 0.86
    }
  ]
}

The model may not rewrite paragraph text or generate paragraph identifiers.

Conflicting boundary proposals from overlapping windows are reconciled by deterministic code using:

* matching paragraph IDs;
* confidence;
* explicit-break precedence;
* proximity tolerance;
* optional verification pass.

10.5 Structured extraction

All extraction calls request JSON matching a versioned schema.

The application must:

1. parse the response;
2. validate it;
3. reject unknown required fields or malformed identifiers;
4. verify that every cited paragraph exists;
5. verify that cited paragraphs occur inside the input window;
6. store the raw response with the run record;
7. write valid records as candidates.

Invalid model output must never be written as accepted story state.

10.6 Evidence verification

For every generated factual record, a verification task receives:

* the proposed record;
* the cited paragraphs;
* limited neighboring context;
* the required epistemic classification.

Verification output:

{
  "supported": true,
  "support_level": "explicit",
  "epistemic_type": "character_belief",
  "overstatement": null,
  "missing_counterevidence": false
}

Verification statuses:

unverified
verified
rejected
needs_review

A record may be verified automatically but remains distinct from author acceptance.

10.7 Record status

Canonical statuses:

generated
verified
accepted
rejected
superseded

Meaning:

* generated: valid model output with valid source references;
* verified: a verification pass found the cited evidence adequate;
* accepted: explicitly approved by the user;
* rejected: explicitly rejected or unsupported;
* superseded: replaced by a later record.

By default, query operations use:

accepted
verified
source text

Generated but unverified records may be included only with an explicit option.

⸻

11. LLM provider interface

The core defines an internal provider interface independent of any one runtime.

type Provider interface {
    Health(ctx context.Context) error
    Models(ctx context.Context) ([]ModelInfo, error)
    Capabilities(ctx context.Context, model string) (Capabilities, error)
    Generate(ctx context.Context, req GenerationRequest) (GenerationResponse, error)
    Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error)
}

The first provider implementation is:

openai-compatible

Required configuration:

base URL
model name
optional API-key environment variable
timeout
context limit

The implementation should work with compatible local model servers without depending on provider-specific model management.

11.1 LLM health check

story llm doctor

Checks:

* endpoint availability;
* model listing where supported;
* configured model existence;
* structured-output capability;
* embedding availability when enabled;
* context configuration;
* localhost versus remote endpoint.

11.2 Model roles

Different roles may use different models:

extraction
verification
discussion
embeddings

All roles may point to the same model in a minimal local setup.

⸻

12. Core CLI commands

12.1 Project commands

story init <directory>
story status
story doctor
story config show
story config validate

story status reports:

project title
source import
chapter count
paragraph count
scene compilation progress
candidate record count
verified record count
accepted record count
last completed run
configured model availability

⸻

12.2 Import commands

story import docx <file>
story import md <folder>
story import report [<run-id>]

⸻

12.3 Compilation commands

story compile
story compile --chapter <chapter-id>
story compile --from <layer>
story compile --to <layer>
story compile --resume
story compile --force
story compile status

Supported layer names:

structure
scenes
scene-cards
entities
records
verification
synthesis

⸻

12.4 Inspection commands

story inspect chapter <id>
story inspect paragraph <id>
story inspect scene <id>
story inspect entity <id>
story inspect claim <id>
story inspect event <id>
story inspect run <id>

Example:

story inspect claim claim-01JZK...

Output:

Claim: Mara believes Elias caused the archive fire.
Type: character_belief
Status: verified
Valid from: Scene 6.1
Evidence:
  ch-0006 / p-...:
  “...”
Generated by:
  provider: local
  model: configured-verification-model
  run: compile-...

⸻

12.5 Review commands

story review list
story review show <record-id>
story review accept <record-id>
story review reject <record-id>
story review supersede <old-id> <new-id>

Filters:

--status generated
--status verified
--type character_belief
--chapter ch-0007
--entity entity-mara

Review decisions are appended to:

reviews/decisions.jsonl

They are never represented only by destructive database updates.

⸻

12.6 Query command

story ask "<question>"

Options:

--mode recall
--mode continuity
--mode interpretation
--mode style
--mode development
--chapter <id>
--character <id>
--include-generated
--max-evidence <count>
--json

Example:

story ask \
  --mode continuity \
  "What does Mara know when she enters the farmhouse?"

Human-readable output:

Mara knows that the archive fire was deliberate, but she does not yet
know that Elias survived.
Evidence:
  [ch-0004:p-0031]
  [ch-0006:p-0088]
Uncertainty:
  The manuscript does not establish whether she has connected the seal
  on the warning to Elias at this point.

Machine-readable output:

{
  "answer": "Mara knows ...",
  "mode": "continuity",
  "evidence": [
    {
      "paragraph_id": "p-...",
      "chapter_id": "ch-0004"
    }
  ],
  "uncertainties": [
    "The manuscript does not establish ..."
  ],
  "records_used": [
    "claim-..."
  ],
  "model_run": "query-..."
}

12.7 Query workflow

The query engine:

1. classifies or accepts the requested mode;
2. retrieves relevant accepted and verified records;
3. retrieves exact source paragraphs;
4. expands to neighboring paragraphs where useful;
5. constructs a bounded evidence packet;
6. calls the discussion model;
7. validates all returned evidence identifiers;
8. removes unsupported citations;
9. returns the answer with provenance.

A query answer is never promoted into the story model automatically.

⸻

13. Search and retrieval

SQLite provides the operational index.

Minimum tables:

projects
imports
documents
chapters
blocks
paragraphs
scenes
entities
mentions
records
evidence
record_edges
model_runs
review_decisions

SQLite FTS indexes:

paragraph text
scene summaries
record statements
entity names and aliases
unresolved questions

Embeddings are optional in v0.1.

The initial retrieval strategy may combine:

* exact entity matching;
* full-text ranking;
* chapter and scene filters;
* record-edge traversal;
* chronology filters;
* optional vector similarity.

A separate graph database is not required.

⸻

14. Compilation runs and resumability

Every compilation creates:

.story/runs/<run-id>/
  run.json
  tasks.jsonl
  raw-responses/
  errors.jsonl
  summary.json

Task states:

pending
running
completed
failed
skipped
cancelled

A run interrupted by process termination can resume:

story compile --resume

The application must not repeat completed model calls unless:

* input hashes changed;
* prompt versions changed;
* model configuration changed;
* --force was supplied.

The cache key includes:

task type
input text hashes
prompt version
model provider
model name
generation settings
schema version

⸻

15. Error handling

Representative exit codes:

0   success
1   general failure
2   invalid command or arguments
10  invalid project
11  ambiguous import
12  unsupported source format
13  canonical manuscript conflict
20  LLM provider unavailable
21  configured model unavailable
22  invalid model response
23  structured-output validation failure
30  compilation incomplete
31  compilation lock active
40  query could not gather sufficient evidence

Errors must identify:

* the operation;
* the affected file or record;
* whether canonical data was changed;
* the recovery command.

Example:

E_IMPORT_AMBIGUOUS_ORDER
Seven Markdown files were found, but their order cannot be determined.
No manuscript files were imported.
A proposed table of contents was written to:
source/import-records/import-01JZK/proposed-toc.toml
Review the file and run:
story import md ./chapters --toc <path>

⸻

16. Re-import policy

Full re-import reconciliation is deferred from the narrow v0.1 implementation.

For v0.1:

* the first import creates the canonical manuscript;
* compilation operates on canonical Markdown;
* --replace creates a new import snapshot and replaces the canonical manuscript;
* replacement invalidates derived records whose source paragraph hashes no longer exist;
* old import records and model-run provenance are retained.

A later version should reconcile changed DOCX manuscripts against existing paragraphs using:

* stable embedded identifiers where available;
* exact text hashes;
* ordered sequence alignment;
* conservative fuzzy matching;
* explicit review for uncertain matches.

No fuzzy re-import match may silently preserve a paragraph identifier.

⸻

17. Prompt requirements

Prompts are project-visible and versioned.

All extraction prompts must require the model to:

* cite paragraph IDs;
* distinguish explicit fact from inference;
* distinguish narrator fact from character belief;
* preserve uncertainty;
* avoid resolving intentionally unresolved questions;
* return schema-valid JSON only;
* avoid inventing identifiers;
* omit unsupported records rather than completing plausible gaps.

The compiler adds a standard system constraint:

The manuscript excerpts are the sole authority for this task.
Do not use general narrative expectations to fill missing events,
motives, relationships, chronology, or world facts.

⸻

18. Security and privacy

Default provider URLs must be loopback addresses:

127.0.0.1
localhost
::1

When a configured endpoint is remote, the CLI displays a warning unless:

[llm.providers.remote]
allow_remote = true

The application must never:

* upload the complete project without an explicit command;
* publish manuscript content;
* expose a local HTTP server by default;
* store raw API keys in the project folder;
* execute model-generated commands;
* allow the model to choose arbitrary local file paths.

⸻

19. MVP scope

The first complete vertical slice includes:

1. project initialization;
2. DOCX import;
3. ordered Markdown-folder import;
4. ambiguity detection and proposed TOC generation;
5. canonical Markdown generation;
6. stable chapter and paragraph identifiers;
7. SQLite indexing;
8. connection to one OpenAI-compatible provider;
9. scene-boundary detection;
10. structured scene-card extraction;
11. entity extraction;
12. basic narrative-record extraction;
13. source-evidence validation;
14. resumable compilation;
15. record inspection;
16. evidence-backed story ask.

⸻

20. Explicitly out of scope for v0.1

The first version does not include:

* graphical interface;
* Nostr signing;
* relay synchronization;
* collaborative editing;
* automatic manuscript rewriting;
* embedded model downloading;
* GPU configuration;
* direct model execution;
* graph visualization;
* multi-book libraries;
* translation alignment;
* EPUB export;
* automatic Git operations;
* plugin execution;
* mobile applications;
* autonomous agents;
* complete DOCX layout preservation;
* reliable tracked-change import;
* automatic acceptance of literary interpretations.

⸻

21. Acceptance criteria

The CLI is considered functionally complete for v0.1 when it can:

1. import a DOCX novel with recognizable chapter headings;
2. import a folder of numerically ordered Markdown chapters;
3. reject an ambiguously ordered folder without partial import;
4. generate a usable proposed TOC for that folder;
5. preserve the original source;
6. produce canonical chapter Markdown with paragraph identifiers;
7. compile the manuscript incrementally through a configured local model;
8. recover from an interrupted compile;
9. produce scenes and structured records with valid source evidence;
10. answer factual and continuity questions with inspectable paragraph citations;
11. state that the text does not establish something when evidence is insufficient;
12. rebuild SQLite from the canonical project files;
13. move the complete project folder to another machine without an application-specific export.

⸻

22. Recommended Go package structure

cmd/
  story/
internal/
  project/
  config/
  importdocx/
  importmd/
  manuscript/
  segmentation/
  compiler/
  provider/
  prompts/
  schema/
  verification/
  retrieval/
  query/
  review/
  store/
  runlog/
  diagnostics/
pkg/
  storymodel/

Dependency direction:

CLI
  ↓
application services
  ↓
domain packages
  ↓
storage and provider adapters

The domain model must not depend directly on:

* CLI formatting;
* SQLite-specific row types;
* one LLM provider;
* a future GUI framework.

This allows a later Wails/Svelte interface to invoke the same Go application services without duplicating compilation logic.

⸻

23. First implementation sequence

The core can be implemented in the following order:

1. init and project validation
2. Markdown-folder import
3. canonical manuscript and paragraph IDs
4. SQLite indexing and inspection
5. OpenAI-compatible provider
6. scene extraction for one chapter
7. resumable whole-book compilation
8. DOCX import
9. entities and narrative records
10. evidence-backed ask
11. review commands
12. synthesis layers

Markdown import should precede DOCX because it provides a simple fixture format for testing the rest of the compiler.

The first end-to-end test should use a small known novel fixture and prove:

ordered chapters
  → paragraphs
  → scenes
  → scene cards
  → narrative records
  → retrieved evidence
  → answer with valid citations