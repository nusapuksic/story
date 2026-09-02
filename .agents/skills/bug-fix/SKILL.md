---
name: bug-fix
description: 'Provides a workflow for collecting bug reports and context, then producing a concrete plan to fix the issue.'
---
Use this skill when a task involves diagnosing a defect and planning a safe, targeted fix.

1. Capture the bug report clearly: expected behavior, actual behavior, scope, and urgency.
2. Gather reproducibility details: exact steps, environment, versions, inputs, and observed errors.
3. Collect technical context: relevant logs, stack traces, recent changes, and impacted modules.
4. Define likely root-cause hypotheses and map each to evidence needed to confirm or reject it.
5. Produce a fix plan with ordered steps, touched files/components, and risk notes.
6. Include a validation plan: how to confirm the bug is fixed and guard against regressions.
7. Call out blockers or missing information explicitly before implementation starts.

Guardrails:
- Do not skip reproduction/context gathering unless the issue is already fully evidenced.
- Prefer the smallest fix that addresses root cause over broad refactors.
- Keep behavior changes explicit and bounded to the reported issue.
