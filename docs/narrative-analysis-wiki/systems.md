# Candidate Systems: Story, Shadow-Loom, and Narrative Knowledge Weaver

[← Index](index.md)

## Story

Repository:  
https://github.com/nusapuksic/story

Current public description:

> A local-first Go CLI that compiles a fiction manuscript into a layered, source-addressable story model.

Current strengths:

- novel-sized manuscript is canonical source;
- deterministic import and stable source addressing;
- scenes and scene cards;
- canonical entities and occurrences;
- chapter/book summaries;
- rebuildable SQLite projection;
- reverse indexes;
- source-grounded `ask`;
- local-first model execution;
- per-run provenance and diagnostics.

Current architectural identity:

> **full-manuscript compiler + evidence resolver**

## Shadow-Loom

Repository:  
https://github.com/dwlmt/shadow-loom

Paper:  
https://arxiv.org/abs/2605.02475

Repository status as reviewed 2026-08-07:

- alpha;
- open source under AGPL-3.0-or-later, with commercial licensing also described;
- Python;
- local Ollama supported;
- NiceGUI workspace;
- MCP and REST surfaces;
- versioned world model;
- causal, belief, information-flow, and spatial “physics”;
- counterfactual/intervention machinery;
- structural narrative-affect scoring.

Architectural identity:

> **causal world simulator + counterfactual narrative reasoner**

### Relationship to Story

Primarily **complement**, not competitor.

A sensible boundary:

```text
full manuscript
      |
      v
    Story
      |
      +--> retrieval / provenance / manuscript truth
      |
      +--> task-specific projection
                |
                v
          Shadow-Loom
                |
                +--> intervention
                +--> counterfactuals
                +--> causal simulation
                +--> structural affect
```

### Why not rebuild Loom inside Story now?

Non-license reasons:

- Loom solves a materially different problem;
- its graph-first causal simulator introduces substantial schema/runtime complexity;
- Story's comparative advantage is access to the entire manuscript and source provenance;
- a specialized exporter/runner can test whether the integration boundary is sufficient before duplicating implementation;
- external-tool conclusions can later be checked back against the manuscript.

### Proposed first integration

```text
story export loom
```

Initially this can be manual: produce a treatment optimized for Loom's extractor.

Longer term, task-specific projections are better than one static synopsis.

Examples:

- relationship history;
- causal neighborhood around a target event;
- reader/character knowledge transitions;
- chronology vs presentation order;
- relevant propositions and evidence.

## Narrative Knowledge Weaver (NKW)

Paper:  
https://arxiv.org/abs/2606.05724

Architectural identity:

> **source-grounded narrative representation + multi-view retrieval/reasoning**

NKW is more direct overlap with Story than Loom.

Important concepts:

- stable canonical graph for entities/relations;
- events/interactions as narrative-process objects rather than forcing everything into one graph ontology;
- atomic facts;
- evolving entity profiles;
- episode/storyline hierarchy;
- multiple retrieval channels;
- source text remains authoritative;
- post-retrieval “reading skills” constrain reasoning by question type.

### Relationship to Story

Classify NKW as:

- **overlap**
- **technique source**
- **benchmark/reference architecture**

rather than a runner Story should simply feed.

## Comparative summary

| Dimension | Story | Shadow-Loom | NKW |
|---|---|---|---|
| Primary aim | compile/analyze full manuscript | causal simulation/counterfactuals | narrative QA/retrieval |
| Canon | source manuscript | typed/versioned world model | source evidence remains authoritative |
| Core unit | paragraph/scene/entity | graph event/entity/state | facts/entities/events/episodes/storylines |
| Main strength | provenance + long manuscript | explicit causal reasoning | multi-view narrative retrieval |
| Best relationship | platform/compiler | external specialized runner | architectural prior art |
| Local-first | yes | yes-capable | research framework; implementation not located |
| Rebuildable source model | yes | graph versions are central | source-grounded assets |
