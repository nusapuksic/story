# Custom Skills for Story CLI

This folder contains reusable **skills** for the `story` Go CLI repository.

Skills are workflow guides stored as `SKILL.md` files under `.agents/skills/<name>/SKILL.md`.

## Structure

```
.agents/
├── README.md
└── skills/
    ├── brainstorm/
    │   └── SKILL.md
    ├── bug-fix/
    │   └── SKILL.md
    ├── implement/
    │   └── SKILL.md
    ├── perf-optimization/
    │   └── SKILL.md
    └── spec-guard/
        └── SKILL.md
```

## Available skills

### brainstorm
Turns a feature idea into an implementation-ready spec grounded in current repository structure and constraints.

### bug-fix
Collects bug context and evidence, then produces a concrete fix plan with validation steps.

### implement
Executes an approved bug-fix or feature plan with scoped code changes and tests.

### perf-optimization
Finds bottlenecks, applies targeted optimizations, and keeps only measurable, behavior-safe improvements.

### spec-guard
Checks proposed or in-progress changes against `docs/cli-spec.md` invariants and architecture boundaries.

## How to use effectively

1. Use one skill per phase: `brainstorm` -> `spec-guard` -> `implement` (optionally `perf-optimization`).
2. Provide clear inputs: goal, constraints, acceptance criteria, and relevant files.
3. Keep scope narrow for each run, then switch skills as phase changes.
4. Prefer explicit outputs: spec, file-level change plan, risks, and acceptance checks.

Use `/skills` in the CLI to inspect or manage installed skills.
