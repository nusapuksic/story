# Narrative Analysis, Information Dynamics, and the Story Toolchain

**Status:** working research wiki  
**Last reviewed:** 2026-08-07  
**Scope:** synthesis of a discussion about narrative meaning, information theory, computational narratology, Shadow-Loom, Narrative Knowledge Weaver, and the architectural direction of Story.

## Executive summary

The central idea explored here is that an important narrative event is not necessarily one with unusual words, high sentiment, or even high local surprise. A passage can be important because it **changes the model required to interpret other passages**, especially earlier ones.

That intuition already has substantial prior art:

- readers construct and update **situation models** of narratives;
- causal connectivity predicts judged importance, recall, and summarization;
- **narrative revelation** has been modeled using relative entropy over narrative time;
- **Bayesian surprise / divergence** can formalize local belief updates and twists;
- **retroactive reinterpretation** is established terminology for restructuring an existing knowledge state after an unexpected event;
- NarCo models retrospective dependencies between later and earlier narrative passages;
- newer systems such as Shadow-Loom and Narrative Knowledge Weaver build explicit narrative graphs and reason over them.

The apparently open engineering opportunity is narrower:

> **Measure the scope and magnitude of retroactive reinterpretation across a full manuscript while preserving source provenance.**

This should not be presented as a new theory of “narrative entropy.” It is better framed as an operationalization built from established work in situation-model updating, causal narrative networks, surprise, revelation, retrospective coherence, and retroactive reinterpretation.

## Key conclusions

1. **Do not use raw token entropy as a proxy for narrative importance.** Narrative information lives in the relation between a new proposition and the reader's existing model.
2. **Separate local surprise from model revision.** An unexpected event may change little; a quiet revelation may reorganize the entire story.
3. **Separate forward uncertainty from backward reinterpretation.** Some events expand the future hypothesis space; others alter the meaning of already-read scenes.
4. **Editors mostly assess these effects qualitatively.** The neighboring concepts are well known, but there is no widely adopted editorial metric for manuscript-scale recontextualization breadth.
5. **Story and Shadow-Loom are complementary.** Story is strongest as a full-manuscript, source-addressable compiler; Loom is a causal/counterfactual world-model reasoner.
6. **Narrative Knowledge Weaver overlaps Story more directly.** Its strongest lessons are source-grounded multi-view retrieval, stable entity graphs, explicit event/episode/storyline layers, and query-specific reading skills.
7. **Story should become a narrative compiler/toolchain, not absorb every analysis technique.** A clean architecture is Canon → reusable IR → task-specific projections → specialized runners.
8. **Compile should be treated as an artifact dependency DAG.** Verification/audit is orthogonal to semantic layers.
9. **Provenance is strategically important.** Any projection or external runner should be traceable back to scenes and canonical manuscript paragraphs.
10. **Search for adjacent systems by problem vocabulary, not product category.** Computational narratology, narrative understanding, causal graphs, long-form RAG, event models, agent memory, and temporal knowledge graphs are higher-yield discovery spaces than “AI writing tools.”

## Wiki pages

- [Concepts and Measures](concepts-and-measures.md)
- [Prior Art and Literature](prior-art.md)
- [Candidate Systems: Story, Shadow-Loom, NKW](systems.md)
- [Story Architecture Conclusions](story-architecture.md)
- [Technology Radar and Discovery Method](technology-radar.md)
- [Sources](sources.md)

## One-line architectural position

> **Story is a source-grounded compiler that turns novel-sized manuscripts into reusable narrative intermediate representations and task-specific projections for retrieval, analysis, and specialized reasoning systems.**
