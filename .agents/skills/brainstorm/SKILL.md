---
name: brainstorm
description: 'Provides a workflow for turning feature ideas into an implementation-ready spec based on current repository state and delivery requirements.'
---
Use this skill when a new feature needs planning before coding, especially when requirements are incomplete or tradeoffs need to be made explicit.

1. Clarify the feature request: goals, user value, constraints, and non-goals.
2. Inspect current repository state: existing architecture, related modules, patterns, and technical constraints.
3. Map the feature to current code paths and identify what can be reused versus what must be added.
4. Define implementation requirements: behavior, interfaces, data model changes, config, migrations, and operational concerns.
5. Evaluate alternatives with tradeoffs (complexity, risk, performance, maintainability, and rollout impact).
6. Produce an implementation-ready spec with:
   - problem statement and success criteria;
   - scoped solution design;
   - file/package-level change plan;
   - API/CLI or UX changes;
   - testing strategy and acceptance criteria;
   - rollout, compatibility, and fallback notes.
7. List open questions and assumptions that must be resolved before implementation starts.

Guardrails:
- Base the spec on observed repository patterns, not idealized rewrites.
- Keep scope explicit and split follow-up work instead of expanding one feature plan indefinitely.
- Do not present speculative details as decided facts; mark assumptions clearly.
