# Concepts and Measures

[← Index](index.md)

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
