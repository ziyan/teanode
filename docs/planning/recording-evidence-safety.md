# Recording evidence in unattended dreaming

This ExecPlan records implementation and validation of the recording evidence boundary.

## Purpose

Passive speech is evidence, never permission to execute tools. Recording-backed digest
calls previously used the general unattended tool catalog; trusted prompt instructions
alone could not enforce this requirement. Learning still files facts and revisions
through the existing controlled synthesis path, without an approval step.

## Decisions

Each dream owns a shared atomic recording-evidence flag. Trusted document metadata sets
it before recording digestion. Knowledge and memory lookups set it when owned raw or
derived recording evidence is returned, including before the digest stage. Once set,
all later model-requested calls in that dream and its nested turns are held to memory,
knowledge and recording lookups. Each variable-risk operation is also checked as a read
at dispatch. Connected and headless tool exceptions cannot widen these permissions.

The flag is internal, not a user setting or model-supplied argument. Provenance checks
follow account-owned document, fact and overview citations with bounded traversal;
missing or unreadable provenance refuses the result and restricts subsequent calls.
Initial self-page and index context checks the same owned provenance before the first
model request. Guarded turns read this context afresh because the existing cross-turn
snapshot does not retain citations; ordinary conversation caching stays unchanged.
Memory batches preserve the flag, including errors and retained deletion
history.
Human-started conversations and dreams that consume no recording evidence retain
existing behavior. Background work cannot be started from an existing run conversation;
synchronous nested turns retain the same flag. Controlled synthesis writes remain
automatic and retain the existing evidence-generation checks.

## Progress

- [x] Inspect digest, ideas-before-digest, tool dispatch and nested-turn paths.
- [x] Implement shared provenance flag and guarded lookup dispatch.
- [x] Preserve automatic fact application and ordinary user conversations.
- [x] Complete adversarial fake-provider, initial-context, ownership, history and race regressions.
- [x] Run full lint including naming and fix review findings.
- [ ] Publish the reviewed draft increment.
- [ ] Integrate recording tool markers with the surface workstream.
- [ ] Merge and deploy reviewed green changes through the coordinating session.

## Validation and limits

Tests use a fake provider, dummy accounts and local PostgreSQL only. The test environment
has no external network, read-only source/toolchain mounts, an empty allowlisted
environment and bounded CPU, memory, process, temporary-file and execution limits.
No production commands, model calls, settings changes or broad dream triggers are
part of this increment. Actual production digestion, useful-memory quality and
real-iPhone checks remain separate evidence.

Focused race tests passed for the agent, shared tools, knowledge and memory packages.
The cases prove that hostile execution and variable-risk write calls never reach their
handlers after recording evidence, including initial self/index context, nested turns,
later phases, connected tools and mixed batches. Owned lookups and controlled fact
application still complete. Full `make lint` passed, including the project naming checks.
Review fixed a new ordinary-conversation query by limiting that provenance read to
guarded dream contexts. These checks do not claim provider resistance or memory quality.
