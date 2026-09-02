# Story Architecture Conclusions

[← Index](index.md)

## Proposed architectural definition

> **Story is a source-grounded compiler that turns novel-sized manuscripts into reusable narrative intermediate representations and task-specific projections.**

This is stronger and more precise than:

- “RAG for novels”;
- “fiction analysis app”;
- “a tool that should implement every narrative algorithm.”

## Four layers

```text
1. CANON
   manuscript
      |
      v
2. STORY IR
   structure
   scenes
   scene cards
   canonical entities
   occurrences
   reusable narrative facts/state
      |
      v
3. PROJECTIONS
   book synopsis
   character timeline
   theme map
   Loom treatment
   editorial context
   epistemic/reinterpretation analysis
      |
      v
4. RUNNERS
   Story ask
   Shadow-Loom
   specialist critique models
   future tools
```

## IR versus projection

### Story IR

Broadly reusable facts/representations learned from the manuscript.

Examples:

- stable entity identity;
- aliases;
- scene participation;
- events;
- world facts;
- character knowledge/belief/intention when well grounded;
- temporal relations.

### Projection

A deliberately lossy representation optimized for one task or consumer.

Examples:

- human editorial synopsis;
- Loom treatment;
- character-specific context packet;
- causal neighborhood;
- style-analysis packet.

This distinction prevents every useful output from becoming a permanent semantic layer.

## Compile should become an artifact DAG

Instead of conceptualizing compile as one rigid pass:

```text
scene cards
   +--> entities
   +--> summaries
   +--> reverse indexes
   +--> character state
   +--> plot episodes
          +--> storylines
          +--> Loom projection
          +--> epistemic analysis
```

Each artifact should declare:

- dependencies;
- output type;
- invalidation conditions;
- whether generation is deterministic or LLM-backed;
- whether it can be independently rerun.

This naturally supports:

- partial rebuilds;
- independent principal-character recalculation;
- optional audits;
- incremental upgrades;
- model-role specialization;
- avoiding repeated expensive calls.

## Verification is orthogonal

Verification/audit is not necessarily a semantic layer.

It can be applied to many generated artifacts:

```text
scene card --------entity assertion ---belief record -------+--> verify / audit
summary claim -------/
projection ----------/
```

This fits a normal-compile → selective-audit workflow.

## Provenance should survive projections

A generated statement should retain evidence:

```json
{
  "statement": "Mara knew Elias was alive before the archive fire.",
  "evidence": ["sc-014", "sc-019"]
}
```

An external projection can then have a provenance sidecar:

```text
loom-treatment.md
loom-treatment.provenance.json
```

Future harness behavior:

```text
external conclusion
      |
      v
projection proposition
      |
      v
Story scene(s)
      |
      v
canonical paragraph(s)
```

This is a major advantage of using Story as the compiler beneath specialized tools.

## Harness direction

A higher-level harness can route by capability:

```text
question
   |
   v
intent/router
   |
   +--> Story retrieval and manuscript evidence
   |
   +--> task-specific projection
             |
             v
       specialized runner
             |
             v
       external result
             |
             v
       Story source verification
```

Potential runners:

- Shadow-Loom → causal/counterfactual reasoning;
- native Story ask → source-grounded discussion;
- specialist developmental-editing model;
- style analysis;
- timeline/continuity checker;
- future epistemic/reinterpretation analysis.

## What should remain native to Story?

Strong candidate:

> **full-manuscript retroactive-reinterpretation analysis with source evidence**

Reason: its value depends on comparing a revelation to many earlier passages. Compressing to a synopsis before analysis destroys the fine-grained backward effects that the measure is meant to detect.

Loom-like counterfactual simulation, by contrast, is a good external-runner candidate.
