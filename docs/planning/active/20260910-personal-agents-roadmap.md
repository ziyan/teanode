# Personal agents: the roadmap

Every person on this server can have an agent: theirs, with their name for
it, their instructions, its own memory of them, and a standing conversation.
What makes it useful is what it can reach. The first thing it reaches is
their mail, because this is a mail server: it sorts what arrives, summarizes
conversations, drafts replies, sends them under a policy the person sets,
and does on their behalf whatever they could do themselves — including, for
an operator, running the server. A calendar and an address book, served to
phones and desktop programs over CalDAV and CardDAV with invitations
travelling as mail, are the next things it reaches.

The agent is the product. The mailbox is its first source.

This is a program of four plans, each an ExecPlan of its own when its turn
comes, written in accordance with `~/.claude/PLAN.md` where that file exists
and otherwise with the shape this repository's planning documents already
have. They are:

**A — the LLM core and the personal agent.** Done 2026-09-11:
`../done/20260910-personal-agents.md`, and its tools as packages in
`../done/20260911-one-tool-one-package.md`. Talking to models; the agent as a row per
account; a mailbox as a source with its own processing policy; the worker and
the runs it executes — triage, research, summarize, embed, reply, send,
schedule; the tool catalog over everything the person may do; connected
servers over the Model Context Protocol; a browser; the command line; what the
operator controls. It ships in two halves so that B need not wait for all of
it: A1, the agent working on the mailbox (milestones 1–6), and A2, the agent
you talk to (7–11).

**B — contacts and CardDAV.** A real address book that the learned contacts
feed into; a DAV stack at `/dav/` signing in with mailbox app passwords;
`sync-collection`, ETags, discovery; the contacts page becomes editable. An
address book becomes a source the agent can be granted.

**C — calendar, CalDAV, invitations by mail.** Events with recurrence and
free-busy; CalDAV on B's stack; iMIP — a `text/calendar` part with
`METHOD:REQUEST` parsed at delivery into a tentative event, RSVP from the
reader sending `METHOD:REPLY`; a calendar page. A calendar becomes a source.

**D — the agent reaches both.** Events out of mail, proposed times from
free-busy, contact enrichment, a daily briefing sent as mail, and mail itself
as a surface: an alias of kind `agent` whose messages land in the main
conversation and whose answers come back by mail.

## Decisions settled before A began

Recorded in `docs/decisions/`:

- `20260910-agents-belong-to-people.md` — an agent is a person's, opt-in,
  and is granted sources one at a time. Nothing from a source the person has
  not granted is ever sent to a model.
- `20260910-embeddings-without-pgvector.md` — vectors are `real[]` columns
  ranked in Go, because the compose file runs stock PostgreSQL.
- `20260910-dav-signs-in-with-app-passwords.md` — CardDAV and CalDAV clients
  sign in the way IMAP clients do.
- `20260910-stdio-servers-are-the-operators.md` — a connected server that is
  a subprocess is declared only by the operator.
- `20260910-the-attached-tab-is-the-persons.md` — a browser tab the agent
  drives with the person's session is attached only by that person, only
  while they watch, and never by a run with nobody present.

Two more were taken with the owner and are recorded in A's decision log
rather than as records, because they are choices of degree: full auto-reply
from the start, behind a policy, a hold window and a refusal ladder; and
full two-way DAV with invitations by mail rather than read-only first.

## Order, and why

A before B and C because the agent is the thing being built and mail is the
source that already exists. B before C because CardDAV is the smaller
protocol and proves the DAV stack that CalDAV then reuses. D last because it
needs all three.

## Terminology that holds across all four

- **Rules** are the mailbox's rules and nothing else. The agent may add one;
  it never has rules of its own.
- **Conduct** is how the agent behaves — the fixed part of its prompt that
  ships with a release.
- **Instructions** are the person's standing words to their agent; **house
  instructions** are the operator's, for every agent on the server.
- **Guidance** is the text inside an auto-reply policy.
- **Memory** is what the agent keeps about the person; each item has an
  audience. **Corrections** are what the person did after the agent acted.
- **A source** is something the person has granted their agent: a mailbox
  now, a calendar or an address book later.
