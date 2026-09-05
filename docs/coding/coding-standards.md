# Coding standards

The rules that `CONTRIBUTING.md` states, with the reasoning behind them.

## Naming

Acronym casing follows the first letter of the identifier. `SessionID` and
`sessionId`; `DKIMResult` and `dkimResult`; `GetFTPID` and `getFtpId`. The rule
is mechanical so that it never needs discussing, and `gogolint` checks it. A new
acronym goes in `.gogolint.yaml` with a comment saying what it stands for — if it
cannot be expanded in a short comment, it is probably an abbreviation, not an
acronym, and should be spelled out instead.

Do not abbreviate: `command`, `response`, `request`, `configuration`. The
exception is Go package names, which should be short because they are repeated
at every call site. `err` is the one blessed short variable name.

Struct receivers are `self`, everywhere. `.golangci.yml` disables
`receiver-naming` and `ST1006` accordingly.

## Errors

Wrap with context that says what was being attempted, in the language of the
person who will read the log:

    return fmt.Errorf("cannot connect to the database at %s:%d: %w", host, port, err)

Not `fmt.Errorf("db: %w", err)`. The operator reading that line at 3am has no
idea what `db` was doing.

Sentinel errors are declared at package level with an `Err` prefix, and package
errors carry the package name — `errors.New("autoacme: no certificate")` — so a
message that escapes to a log still says where it came from.

## Comments

Explain why. The what is in the code. A comment is worth writing when it
records a protocol requirement, a failure observed in production, or a
deliberate choice among reasonable alternatives — the things a reader cannot
recover by reading harder.

## Concurrency

A goroutine that can panic must not take the process down with it. A mail
server that dies because one malformed message tripped a nil dereference loses
every connection in flight.

Prefer a bounded, owned goroutine over a fire-and-forget one: something should
be able to wait for it and something should be able to cancel it. `sync.WaitGroup`
plus a context is the pattern used throughout `internal/mx`.

## Configuration

Anything an operator sets goes in `internal/config`, with a doc comment on the
field explaining what it does and what breaks if it is wrong — that comment is
the documentation, since the example file is generated from the same types.

Optional integrations are off by default and construct nothing when disabled.
An `enabled: false` that still dials a socket is a bug.

## Tests

Test the behaviour that matters, not the implementation. The valuable tests in
this repository are the ones that feed a real message through a parser and
assert the verdict, and the ones that assert a failure mode: a rejected update
leaves the file untouched, a missing challenge refuses the handshake.

Never reach the network from a unit test.
