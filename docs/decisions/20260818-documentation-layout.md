# Documentation layout and the local-only naming lint

- Status: accepted
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

Documentation was four uppercase files at the repository root — `FAQ.md`,
`FEATURES.md`, `NOTES.md`, `TODO.md` — describing a hosted service, a marketing
feature list, and a backlog that was almost entirely finished. There was
nowhere to put a design document or a decision record, and nothing told a new
contributor or a coding agent where to look.

## Decision

Documentation lives under `docs/` with lowercase kebab filenames:

- `docs/planning/active/` and `docs/planning/done/` — design documents, named
  `<YYYYMMDD>-<slug>.md`, moved to `done/` when implemented.
- `docs/decisions/` — decision records, same naming.
- `docs/reference/` — how this project is built, structured and run.
- `docs/coding/` — conventions a change has to follow.
- `docs/team/` — how to work in this repository, human or agent.
- `docs/subsystems/` — evergreen explanations of one part of the system.
- `docs/security/` — security-relevant references.

Conventional root files keep their canonical names: `README.md`,
`CONTRIBUTING.md`, `CHANGELOG.md`, `AGENTS.md`, `CLAUDE.md`, `LICENSE`.

Separately: `mulint`, which enforces the naming and error-prefix conventions,
runs locally through `make lint` but never in CI. `make lint-ci` is what CI
runs.

## Consequences

- A decision has somewhere to go, so the reasoning behind a change survives the
  conversation it happened in.
- `mulint` not being in CI means a contributor without it installed is never
  blocked, at the cost of naming drift being caught at review rather than
  automatically. `mulint.yaml` registers this project's mail vocabulary so the
  check is quiet enough to be worth running.
