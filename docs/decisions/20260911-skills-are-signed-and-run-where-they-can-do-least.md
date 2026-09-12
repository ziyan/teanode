# Skills are trusted by a signature, and run where they can do least

## Status

Accepted, 2026-09-11.

## Context

Every tool the agent had was compiled in, so giving it a new one meant a
release. A skill is a file of declarations — some requests, some commands, a
short sequence of them — that becomes tools while the server is running, and
is fetched from a registry over the internet.

That is a new kind of thing for this server to hold: something it did not
build, that says what requests to make and what commands to run, arriving
after the release was signed.

## Decision

**A skill is trusted by a signature against a key built into the server.** The
registry signs each entry over the skill's name, version, address and content
hash together; the public half of that key is committed here and shipped in the
binary. The file is downloaded only after its entry verifies, and is kept only
if its bytes hash to what was signed. A key fetched from the same place as the
thing it vouches for would prove nothing, so it is not fetched.

**A skill's commands run on the person's own attached computer, never on this
server.** Even signed, a skill is something from the internet, and this is a
mail server. On the attached computer the person confirms each call, the blast
radius is their own machine, and it is where `claude_code` and `codex` already
run. Requests a skill makes do go from the server, but through the same
address guard as fetching a web page, so a skill cannot reach the network the
server sits in.

**An operator installs a skill and it belongs to the server.** Installing adds
tools to everybody's agent, which is the same kind of act as declaring a
connected server, and that is already `server:manage`. It gives one list to
audit.

**Every reference in a skill is checked when it is read**, not when it runs.

## Consequences

A skill that runs commands does nothing in a run with nobody present — no
schedule, no mail processing — because it needs a computer attached and asks
before it acts. That is the cost of the second decision, and it is accepted:
the kind of skill worth running unattended is the kind that only makes
requests, and those work everywhere.

The signature says the file is the one that publisher published. It does not
say the file is harmless. An operator installing a skill is vouching for it to
everybody on the server, which is why the whole file is stored and the
dashboard says what each skill brings and whether any of it runs commands.

A second registry, or a skill written by hand, would need its key added here
and a release to carry it. That is deliberate for now: one publisher, one key,
one thing to trust.
