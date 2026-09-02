# Prior Art and Literature

[← Index](index.md)

## Foundational reader-model work

### Zwaan, Langston & Graesser (1995) — Event-Indexing Model

Readers construct situation models in which narrative events are linked along five dimensions:

- time;
- space;
- protagonist;
- causality;
- intentionality.

**Why it matters:** Story does not need to invent the idea that readers maintain a structured, evolving representation of narrative state.

Primary source:  
https://www.psychologicalscience.org/journals/psychological-science/j.1467-9280.1995.tb00513.x/

## Causal centrality and importance

### Trabasso & van den Broek (1985) — Causal Thinking and the Representation of Narrative Events

Causal-network properties predicted:

- immediate and delayed recall;
- summarization;
- judged importance.

Whether an event belonged to a causal chain and the number of causal connections both mattered.

**Why it matters:** “important event” already has a strong causal-network interpretation. Frequency and sentiment are not sufficient substitutes.

Primary source:  
https://doi.org/10.1016/0749-596X(85)90049-X

## Retroactive reinterpretation

### Cardier et al. (2017) — Modeling the Resituation of Memory in Neurobiology and Narrative

The paper explicitly uses **retroactive reinterpretation** for the restructuring of an established knowledge state after an unexpected event.

It proposes graphical knowledge modeling for tracking this process across narrative and neurobiological domains.

**Why it matters:** do not claim the underlying phenomenon as a new concept. The possible contribution is a practical, source-grounded manuscript-scale measurement of its scope.

Sources:  
https://m.aaai.org/Library/Symposia/Spring/ss17-07.php  
https://cris.unibo.it/handle/11585/610285

## Narrative revelation

### Piper, Xu & Kolaczyk (2023) — Modeling Narrative Revelation

Uses relative entropy and time-series analysis to model how information is disseminated over narrative time across a corpus of more than 2,700 books.

**Why it matters:** information-theoretic modeling of plot-level revelation is established prior art.

Source:  
https://discourse.computational-humanities-research.org/t/modeling-narrative-revelation/2073

## Retrospective narrative dependencies

### Xu et al. (2024) — NarCo

**Fine-Grained Modeling of Narrative Context: A Coherence Perspective via Retrospective Questions**

NarCo builds a graph of task-agnostic coherence dependencies between narrative snippets. Edges are expressed as retrospective questions that reinstate relevant earlier events.

**Why it matters for Story:**

- useful model for reverse narrative dependencies;
- relevant to retrieval beyond lexical/entity matching;
- directly supports the idea that later passages activate specific earlier context.

Primary source:  
https://aclanthology.org/2024.acl-long.317/

## Narrative Information Theory

### Schulz, Patrício & Odijk (2024)

Proposes an information-theoretic framework for measuring pivotal moments, cliffhangers, twists, narrative complexity, and emotional dynamics.

**Why it matters:** Story should not present information-theoretic treatment of plot as novel.

Status: arXiv preprint.

Primary source:  
https://arxiv.org/abs/2411.12907

## Narrative surprise

### Bissell, Paulin & Piper (2025)

**A Theoretical Framework for Evaluating Narrative Surprise in Large Language Models**

Operationalizes six criteria for narrative surprise, including predictability, post-dictability, and importance, and studies human/LLM endings to mystery stories.

**Why it matters:** compelling surprise is not simply unpredictability; retrospective coherence is an explicit part of the problem.

Primary source:  
https://aclanthology.org/2025.wnu-1.7/

## Causal graph generation

### Li, Pan & Pi (2025) — Beyond LLMs

Builds causal graphs from narrative text using agent-centered event vertices, linguistically informed features, a STAC classification model, and iterative graph refinement.

**Why it matters:** another prior-art route to causal narrative graphs; useful for evaluating whether causal extraction should be LLM-only.

Primary source:  
https://aclanthology.org/2025.wnu-1.10/

## Shadow-Loom

### Wilmot (2026)

Shadow-Loom turns narrative text into a versioned typed graph containing entities, events, causal/social/spatial relations, beliefs, information flow, and fabula/syuzhet positions. It supports intervention and counterfactual reasoning plus structural reader-state scoring such as mystery, suspense, surprise, and dramatic irony.

Its repository describes it as an alpha research artifact and uses LLMs primarily at extraction/rendering/audit boundaries while typed code performs graph reasoning.

**Why it matters:** validates a graph-first causal/counterfactual direction, but its objective is different from Story's full-manuscript compiler.

Sources:  
https://arxiv.org/abs/2605.02475  
https://github.com/dwlmt/shadow-loom

## Narrative Knowledge Weaver

### Tian et al. (2026)

NKW is a source-grounded long-form narrative reasoning framework aligning:

- textual evidence;
- atomic facts;
- canonical graph structure;
- entity profiles;
- interactions;
- episodes;
- storylines.

At query time it uses text, graph, and narrative tools plus post-retrieval reading skills.

**Why it matters:** this is the closest conceptual overlap found so far with Story's source-grounded narrative compilation/retrieval problem.

Status: arXiv preprint; no public implementation was located during this review.

Primary source:  
https://arxiv.org/abs/2606.05724
