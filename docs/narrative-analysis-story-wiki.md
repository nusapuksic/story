# Narrative Analysis, Information Dynamics, and the Story Toolchain

**Combined single-file edition — 2026-08-07**

> This document combines the pages in the companion mini-wiki bundle.

---

# Concepts and Measures

## 1. Meaning as a model, not a bag of facts

The motivating literary comparison was between *Maps of Meaning*, *The Hitchhiker's Guide to the Galaxy*, and the *Fever* series.

The common structure is epistemic:

- a protagonist begins with a workable model of reality;
- an anomaly appears that does not fit;
- the model must be updated, expanded, or recursively reinterpreted;
- the significance of prior observations changes.

The works differ in the topology of the update:

- **Maps of Meaning:** anomaly → exploration → integration → renewed order.
- **Fever:** apparent order → hidden ontology → repeated expansion and remapping.
- **Hitchhiker's:** apparent order → larger explanatory frame → further absurdity → repeated non-convergence.

This is useful as a conceptual lens, not as an established literary classification.

## 2. Local surprisal

For an observation \(x_t\),

\[
I(x_t)=-\log P(x_t\mid x_{<t})
\]

This captures how unexpected an observation is under a predictive model.

**Limitation:** a high-surprisal event can have little narrative consequence.

A sudden explosion may be highly unexpected while leaving the interpretation of the previous twenty chapters unchanged.

## 3. Bayesian surprise / belief-state change

Let \(Z\) denote a latent interpretation of the narrative: causal structure, identity, trust, motives, world rules, character knowledge, etc.

A belief update can be represented as:

\[
D_{KL}\left(P(Z\mid E_{1:t}) \,\|\, P(Z\mid E_{1:t-1})\right)
\]

This is much closer to “the switch flipped.”

It asks:

> How much did the current model change after this observation?

## 4. Entropy can increase or decrease

A revelation need not resolve uncertainty.

### Resolution

A detective revelation may collapse hypotheses:

\[
H(Z\mid E_{1:t}) < H(Z\mid E_{1:t-1})
\]

### Ontological expansion

A world-changing discovery may enlarge the hypothesis space:

\[
H(Z\mid E_{1:t}) > H(Z\mid E_{1:t-1})
\]

This distinction is useful for genre and plot-arc analysis:

- mysteries often trend toward convergence;
- hidden-world fiction often repeatedly expands the ontology;
- absurdist or non-convergent fiction may repeatedly destabilize explanatory frames.

## 5. Retroactive reinterpretation

Established term:

> **Retroactive reinterpretation**: an unexpected event causes an established state of knowledge to be restructured so that its causal interpretation changes.

This is the closest literature term to the discussion's “recontextualization” idea.

The key distinction is:

- **surprise:** how much the present belief state changes;
- **retroactive reinterpretation:** how prior information changes meaning.

## 6. Proposed operational measure: backward impact

This is a proposed engineering measure, **not an established standard term**.

For a revelation \(R_t\), consider each earlier scene \(s_i\). Compare its interpretation before and after \(R_t\):

\[
B(R_t)=\sum_{i<t} w_i\,D\left(M_i^{after\,R_t}\,\|\,M_i^{before\,R_t}\right)
\]

Possible decompositions:

### Breadth

\[
\text{breadth}(R_t)=
\frac{\#\{\text{prior scenes materially affected}\}}
{\#\{\text{prior scenes}\}}
\]

### Weighted mass

\[
\text{mass}(R_t)=
\sum_i \text{importance}(s_i)\times
\text{reinterpretation}(s_i,R_t)
\]

### Useful vector

Rather than collapse everything into one “entropy” number:

\[
R_t =
(\text{surprisal},
\text{belief shift},
\Delta H,
\text{retroactive breadth},
\text{retroactive mass})
\]

This preserves distinctions between:

- local surprise;
- current model change;
- uncertainty expansion/collapse;
- how much prior narrative is reinterpreted;
- whether affected prior material is structurally important.

## 7. Narrative profiles by information dynamics

These are useful **descriptive categories**, not established editorial labels.

### Local-change / episodic
Events alter circumstances without repeatedly rewriting earlier meaning.

### Convergent
Uncertainty gradually collapses as hypotheses are resolved.

### Retrospective-reorganization
A small number of revelations reconfigure large portions of prior narrative.

### Ontological-expansion
Major discoveries enlarge the reader's model of possible reality.

### Non-convergent expansion
Each explanatory layer opens onto a larger or less stable one.

## 8. Important caution

Do not call this entire family of ideas **narrative entropy** unless a probability distribution is explicitly defined.

Safer vocabulary:

- situation-model change;
- belief-state divergence;
- narrative revelation;
- Bayesian surprise;
- causal centrality;
- retrospective coherence;
- retroactive reinterpretation;
- backward impact / reinterpretation scope (as implementation terms).

---

# Prior Art and Literature

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

---

# Candidate Systems: Story, Shadow-Loom, and Narrative Knowledge Weaver

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

---

# Story Architecture Conclusions

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

---

# Technology Radar and Discovery Method

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

---

# Sources

Sources are grouped by role. Links below were rechecked while compiling this wiki on **2026-08-07**.

## Foundational cognitive/narrative models

### Zwaan, R. A., Langston, M. C., & Graesser, A. C. (1995)
**The Construction of Situation Models in Narrative Comprehension: An Event-Indexing Model.**  
*Psychological Science, 6(5), 292–297.*  
https://www.psychologicalscience.org/journals/psychological-science/j.1467-9280.1995.tb00513.x/

### Trabasso, T., & van den Broek, P. (1985)
**Causal Thinking and the Representation of Narrative Events.**  
*Journal of Memory and Language, 24(5), 612–630.*  
DOI: https://doi.org/10.1016/0749-596X(85)90049-X

### Cardier, B., Sanford, L. D., Goranson, H. T., Lundberg, P. S., Ciavarra, R. P., Devlin, K., Cassas, N., & Erioli, A. (2017)
**Modeling the Resituation of Memory in Neurobiology and Narrative.**  
AAAI Spring Symposium technical report.  
https://m.aaai.org/Library/Symposia/Spring/ss17-07.php  
https://cris.unibo.it/handle/11585/610285

## Information dynamics and narrative structure

### Piper, A., Xu, H., & Kolaczyk, E. D. (2023)
**Modeling Narrative Revelation.**  
Computational Humanities Research 2023.  
https://discourse.computational-humanities-research.org/t/modeling-narrative-revelation/2073

### Xu, L., Li, J., Yu, M., & Zhou, J. (2024)
**Fine-Grained Modeling of Narrative Context: A Coherence Perspective via Retrospective Questions.**  
ACL 2024.  
https://aclanthology.org/2024.acl-long.317/

### Schulz, L., Patrício, M., & Odijk, D. (2024)
**Narrative Information Theory.**  
arXiv:2411.12907.  
https://arxiv.org/abs/2411.12907

### Bissell, A., Paulin, E., & Piper, A. (2025)
**A Theoretical Framework for Evaluating Narrative Surprise in Large Language Models.**  
Workshop on Narrative Understanding 2025.  
https://aclanthology.org/2025.wnu-1.7/

### Li, Z., Pan, R., & Pi, X. (2025)
**Beyond LLMs: A Linguistic Approach to Causal Graph Generation from Narrative Texts.**  
Workshop on Narrative Understanding 2025.  
https://aclanthology.org/2025.wnu-1.10/

## Systems and current frameworks

### Wilmot, D. (2026)
**Shadow-Loom: Causal Reasoning over Graphical World Models of Narratives.**  
arXiv:2605.02475.  
https://arxiv.org/abs/2605.02475

Project repository:  
https://github.com/dwlmt/shadow-loom

### Tian, Q., Chen, F., Li, Y., et al. (2026)
**Narrative Knowledge Weaver: Narrative-Centric Retrieval-Augmented Reasoning for Long-Form Text Understanding.**  
arXiv:2606.05724.  
https://arxiv.org/abs/2606.05724

## Story

### Story repository
Local-first Go CLI for compiling a fiction manuscript into a layered, source-addressable story model.  
https://github.com/nusapuksic/story

## Literary works that motivated the conceptual comparison

These are not technical sources for the computational claims; they motivated the discussion about changing maps of reality and narrative salience.

### Literary works that motivated the conceptual comparison

The literary works cited here are not technical sources for the computational framework. They serve instead as examples of different ways narratives can destabilize, expand, replace, or fail to resolve a reader’s model of reality.

A useful starting point is Jordan Peterson’s *Maps of Meaning*, read less as a theory of mythology than as a psychological account of **model maintenance under uncertainty**. Familiar order corresponds to a sufficiently stable predictive and action-guiding model; anomaly exposes its limits; attention and affect are recruited toward the discrepancy; and adaptation requires some degree of model revision. Meaning, in this framing, depends not simply on what information is present but on how deeply that information affects expectations, goals, values, and possible actions.

Karen Marie Moning’s *Fever* series dramatizes repeated **ontological expansion**. New discoveries do not merely append facts to an existing world model. They progressively expose a larger hidden structure, causing earlier people, events, objects, and observations to acquire different significance. The protagonist’s increasing competence is therefore partly the growth and repeated reconstruction of her map of reality.

Douglas Adams’s *The Hitchhiker’s Guide to the Galaxy* explores a complementary dynamic: **expansion without stable convergence**. Each larger explanatory frame reveals another beyond it, often making the universe less rather than more intuitively comprehensible. The problem of navigation remains even when the possibility of a final, satisfactory map is repeatedly undermined.

Several other works help distinguish additional forms of narrative model change.

Jorge Luis Borges’s “Tlön, Uqbar, Orbis Tertius” presents a particularly pure case of **ontological replacement**: an initially fictional explanatory system progressively acquires enough coherence and cultural force to displace the reality it was meant only to describe.

Stanisław Lem’s *Solaris* illustrates **persistent epistemic uncertainty**. Evidence accumulates, theories proliferate, and observation continues, yet no adequate model of the phenomenon emerges. Information gain does not necessarily produce interpretive convergence.

Gene Wolfe’s *The Book of the New Sun* is especially relevant to **retroactive reinterpretation**. Information encountered early often changes significance only much later, requiring the reader to recompute earlier passages rather than simply integrate new forward-moving events.

Umberto Eco’s *Foucault’s Pendulum* supplies the inverse danger: **pathological model construction**. Its characters impose increasingly elaborate causal and symbolic structure on loosely connected evidence until the constructed interpretation becomes self-reinforcing. This is an important counterexample to the assumption that increasing coherence necessarily means increasing truth. It also provides a useful warning for computational narrative analysis: a system can generate an elegant causal or thematic model that exceeds what the source text actually supports.

Philip K. Dick’s *Ubik* repeatedly destabilizes the current explanatory frame, producing a condition in which apparent reality continually ceases to support the model used to interpret it. Kazuo Ishiguro’s *Never Let Me Go* demonstrates a quieter but equally important phenomenon: **large semantic revision without correspondingly large local surprise**. Ordinary earlier details become increasingly charged as the underlying situation becomes clear.

Ursula K. Le Guin’s *The Left Hand of Darkness* broadens the problem beyond hidden facts or supernatural ontology. Its protagonist must revise entrenched conceptual categories used to interpret another society, illustrating **category-level model revision**. Ted Chiang’s “Story of Your Life” extends this further by making the representational system itself transformative: acquiring a new way of organizing information changes how temporal events can be understood and experienced.

Taken together, these works suggest that narrative information dynamics can take several distinct forms:

* **adaptive reconstruction** — anomaly forces a more adequate model;
* **ontological expansion** — the world repeatedly proves larger than expected;
* **non-convergent expansion** — explanatory frames proliferate without final stabilization;
* **ontological replacement** — one model progressively displaces another;
* **persistent uncertainty** — evidence grows without yielding an adequate interpretation;
* **retroactive reinterpretation** — later information changes the meaning of earlier material;
* **pathological coherence** — apparent meaning increases through unsupported pattern construction;
* **category revision** — the concepts used to interpret events themselves must change;
* **representational transformation** — the structure through which events are understood is altered.

The computational concepts discussed elsewhere in this document—situation-model updating, Bayesian surprise, narrative revelation, causal centrality, retrospective coherence, and retroactive reinterpretation—provide more precise ways to formalize parts of this literary intuition. The motivating question is therefore not simply whether an event is surprising, but **what kind of model change it produces, how extensive that change is, and how much of the previously interpreted narrative must be reorganized as a result**.

### Literary works cited

* Peterson, Jordan B. *Maps of Meaning: The Architecture of Belief*. Routledge, 1999.
* Adams, Douglas. *The Hitchhiker’s Guide to the Galaxy*. Pan Books, 1979, and subsequent novels in the *Hitchhiker’s Guide* series.
* Moning, Karen Marie. *Fever* series, beginning with *Darkfever*. Delacorte Press, 2006.
* Borges, Jorge Luis. “Tlön, Uqbar, Orbis Tertius.” In *El jardín de senderos que se bifurcan*, 1941; later collected in *Ficciones*, 1944.
* Lem, Stanisław. *Solaris*. Wydawnictwo Ministerstwa Obrony Narodowej, 1961.
* Wolfe, Gene. *The Book of the New Sun*, beginning with *The Shadow of the Torturer*. Simon & Schuster, 1980–1983.
* Eco, Umberto. *Foucault’s Pendulum*. Bompiani, 1988.
* Dick, Philip K. *Ubik*. Doubleday, 1969.
* Ishiguro, Kazuo. *Never Let Me Go*. Faber and Faber, 2005.
* Le Guin, Ursula K. *The Left Hand of Darkness*. Ace Books, 1969.
* Chiang, Ted. “Story of Your Life.” In *Starlight 2*, edited by Patrick Nielsen Hayden. Tor Books, 1998; later collected in *Stories of Your Life and Others*, 2002.

## Source-status cautions

- **Narrative Information Theory** is an arXiv preprint.
- **Shadow-Loom** is presented by its repository as an alpha research artifact under active development.
- **Narrative Knowledge Weaver** is an arXiv preprint; no current public implementation was located during this review.
- Proposed terms such as **backward impact**, **reinterpretation breadth**, or **reinterpretation mass** in this wiki are discussion-derived implementation language, not claims of established terminology.

---

