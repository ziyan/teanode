# One binary with the dashboard compiled in

- Status: accepted
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

The repository was two projects: `backend/`, a Go module, and `frontend/`, a
React application whose `npm run deploy` script synchronised the built bundle
to an S3 bucket. The dashboard was never served by the Go binary at all, so
running the software meant deploying two things to two places, one of which was
a specific cloud bucket.

## Decision

The repository root is the Go module `github.com/ziyan/teanode`. Go source
lives in `internal/` with the entry point at `main.go` and subcommands in
`cmd/`. Dashboard source lives in `web/`, and its build output is written into
`internal/frontend/static/`, embedded with `go:embed` and served by the binary.

`make` produces one statically linked executable containing everything.

## Consequences

- Deploying is copying one file. Upgrading is replacing one file.
- `go install github.com/ziyan/teanode@latest` works for anyone.
- `go build` fails on a clean checkout unless `internal/frontend/static/`
  exists, because `go:embed` requires the directory. A committed stub keeps
  the build working before `npm run build` has ever run.
- The binary carries the dashboard's weight even on a headless deployment. At a
  few megabytes that is a fair trade for having one artefact.
