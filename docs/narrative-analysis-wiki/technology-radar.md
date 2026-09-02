# Technology Radar and Discovery Method

[← Index](index.md)

## Principle: search for problems, not products

Searching for “AI novel analysis software” tends to surface commercial writing assistants.

Search the underlying problems instead.

## Core vocabulary

### Narrative comprehension

- computational narratology
- narrative understanding
- narrative intelligence
- long-form narrative understanding
- story comprehension
- fiction understanding

### Representation

- narrative knowledge graph
- causal narrative graph
- event graph
- entity-event graph
- story world model
- character state tracking
- belief state tracking
- epistemic state narrative
- temporal narrative graph

### Retrieval

- narrative RAG
- long-form RAG
- temporal RAG
- causal RAG
- GraphRAG fiction
- source-grounded narrative
- retrospective retrieval

### Information dynamics

- narrative surprise
- narrative revelation
- retrospective coherence
- retroactive reinterpretation
- narrative causality
- counterfactual narrative
- Bayesian surprise narrative

### Systems

- computational storytelling
- interactive narrative
- narrative simulation
- story planning
- agent memory
- temporal knowledge graph

Combine with:

- GitHub
- open source
- implementation
- framework
- toolkit
- library
- code
- demo
- local LLM
- Ollama

## High-yield venues

Track:

- ACL Anthology
- Workshop on Narrative Understanding (WNU)
- Computational Models of Narrative (CMN)
- Text2Story
- arXiv categories around NLP/AI
- GitHub repositories/topics

## Paper-to-code workflow

```text
interesting paper
      |
      v
authors / lab
      |
      v
project page
      |
      v
GitHub organization
      |
      v
code + datasets
      |
      v
references + citing papers
```

Do not require a repo at the first search stage.

## Search adjacent domains

Narrative tooling can borrow from fields solving similar structural problems:

- meeting understanding;
- legal chronology;
- longitudinal medical records;
- intelligence analysis;
- game-world simulation;
- temporal knowledge graphs;
- event sourcing;
- agent memory;
- case reconstruction.

Shared problem:

> A large sequential evidence base describes changing entities and events; recover the relevant state at a particular time without losing provenance.

## Candidate classification

Every discovery gets one label:

- **competitor** — aims at substantially the same user/problem;
- **complement** — Story should feed or invoke it;
- **dependency** — reusable component/library;
- **technique** — algorithm or architecture to learn from;
- **benchmark** — useful for evaluation.

Then ask:

> **Does this solve something Story should own, or something Story should feed?**

Examples from this discussion:

| Candidate | Classification | Current conclusion |
|---|---|---|
| Shadow-Loom | complement | Story should feed it before considering reimplementation |
| Narrative Knowledge Weaver | overlap + technique | study architecture/retrieval ideas |
| NarCo | technique | relevant to retrospective narrative retrieval |
| Beyond LLMs | technique | causal graph extraction prior art |
| Narrative Information Theory | technique/prior art | formal narrative information measures |
| WNU surprise framework | benchmark/technique | surprise + retrospective coherence |
