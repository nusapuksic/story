---
name: perf-optimization
description: 'Provides a workflow for implementing code performance optimizations when faster runtime or lower resource usage is needed.'
---
Use this skill when a task asks for performance improvements in code paths, queries, builds, or runtime behavior.

1. Identify the bottleneck and define the target metric to improve (time, memory, CPU, I/O, allocations, or query latency).
2. Measure the baseline using existing project tooling and representative inputs.
3. Focus on the highest-impact hotspot first, preferring algorithmic and data-structure improvements over micro-tweaks.
4. Implement a small, behavior-preserving optimization and avoid unrelated refactors.
5. Re-measure under the same conditions and compare against the baseline.
6. Keep the optimization only if it provides a meaningful, repeatable gain without correctness regressions.
7. Document the change with the before/after metric and any tradeoffs.

Guardrails:
- Preserve functional behavior and public interfaces unless explicitly requested.
- Prefer deterministic, maintainable changes over risky low-level tricks.
- Do not add speculative complexity without measured benefit.
