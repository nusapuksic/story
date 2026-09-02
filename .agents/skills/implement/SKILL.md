---
name: implement
description: 'Provides a workflow for taking an approved bug-fix or feature plan and implementing it safely in the current repository.'
---
Use this skill when a bug report or feature request already has a plan and the next step is implementation.

1. Read the plan and restate the exact scope, constraints, and acceptance criteria.
2. Inspect current repository code paths that the plan touches and align with existing patterns.
3. Break the implementation into ordered, testable steps and apply changes incrementally.
4. Implement only the planned scope, reusing existing helpers and abstractions where possible.
5. Update related wiring and configuration so behavior is consistent across all affected surfaces.
6. Add or update tests for the planned behavior and important edge cases.
7. Resolve issues found during build/test execution and iterate until the planned acceptance criteria are met.
8. Summarize the implementation outcome, key decisions, and any remaining follow-ups.

Guardrails:
- Do not expand scope without explicitly marking it as a follow-up.
- Prefer root-cause fixes over symptom patches.
- Preserve existing behavior outside the planned change.
