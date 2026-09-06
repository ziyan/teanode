# CLAUDE.md

Start with `AGENTS.md`. It covers what this program is, where the code lives,
how a message flows through it, and the invariants that matter.

Then, depending on what you are doing:

- `CONTRIBUTING.md` — naming, comments, commits, changelog, tests
- `docs/reference/local-development.md` — build, test, run a dev server
- `docs/reference/project-structure.md` — what each package is for
- `docs/reference/command-line.md` — the CLI, and how it reaches the whole API
- `docs/reference/deployment.md` — running it with docker compose, and backups
- `docs/coding/database-migrations.md` — how to add a migration safely
- `docs/security/security-review.md` — what was audited, and what is open
- `docs/decisions/` — why the architecture is the way it is
- `docs/planning/active/` — work in flight, including the current restructure

## Notes specific to Claude Code

- `make test` starts a PostgreSQL container. It needs Docker running.
- `make lint` runs a local naming check that is not installed everywhere and is
  not in
  CI. `make lint-ci` is what CI runs.
- Do not run `git add -A` without looking at what it staged. A previous session
  nearly committed a directory of private keys that way.
