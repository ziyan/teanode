# Coding sessions

The person's Claude Code and Codex sessions meet the agent in three places,
each built on something that exists for its own reasons. This page says how
they fit together; the detail is in the sections it points to.

| Direction | What happens | Built on | Detail |
| --- | --- | --- | --- |
| sessions into memory | their transcripts are read, searchable and filed | the `claude-code` and `codex` source types | `memory.md`, "Where it reads from" |
| memory into sessions | each session is shown what memory knows about its checkout | `teanode hook` | `memory.md`, "In the person's coding sessions" |
| the agent into sessions | the agent lists, reads, types into and answers them | herdr and the computer program | `devices.md`, "Herdr" |

## Sessions into memory

A source of type `claude-code` or `codex`, installed from the signed registry
and run on the person's computer, reads the tool's transcripts as chat
documents: what the person asked and what the session answered, not the
commands it ran. They are searchable like any document, the dream files what
they taught, and each unit keeps the session's id and working directory, which
is how the last session in a directory is found (migration 0156 indexes them by
directory). A source asked to read again now goes ahead of the computer's
other sources.

## Memory into sessions

`teanode hook install claude-code|codex` (`internal/cmd/hook.go`) adds hooks
for five events: `SessionStart` (30 seconds), `UserPromptSubmit` (15), `Stop`
and `PreCompact` (10), and `SessionEnd` (3, the most Codex allows). What they
print is wrapped in `<teanode-memory>` and decided by `internal/agent/coding.go`:

- **SessionStart:** the checkout's project pages with their liveliest facts,
  and the last session held in that directory or checkout: its title, the
  person's last three requests and the start of its last answer, read from
  the stored transcript rather than written by a model.
- **UserPromptSubmit:** the recall a turn does, kept while recall chooses to
  the checkout's project, the pages under it, what it links to by a stated
  link, and lessons at a similarity of at least 0.45; less pages shown in the
  last five prompts, and nothing for a prompt under three words, a slash
  command, or text the tool sends on its own.
- **Stop, PreCompact, SessionEnd:** `CaptureAgentCodingSession` asks the
  computer's source of that tool's type to read again now.

The checkout is found from the profile's "The checkout is at ... on ..." line
on the session's own computer, else by the git remotes against the profile's
"Lives at" line, with the commit memory read it at. A hook never fails the
tool: it exits 0, shows nothing, and writes what went wrong to `hook.log` in
the user's cache directory. The same blocks are on the memory tool's
`checkout` action, the API (`ReadAgentCodingContext`,
`RecallAgentCodingMemory`), `teanode agent memory checkout` and the Recall
dialog. The decision is
`docs/decisions/20261009-coding-sessions-are-shown-memory-by-hooks.md`.

## The agent into sessions

The program on each computer watches herdr's socket, recognizes the forms a
session draws for a question, an approval and a plan, and reports them to the
server. The `herdr` tool, the Herdr sessions card and `teanode computer herdr`
list sessions, read their history or screen, type into a pane, answer a
question, watch a session until its turn ends, and open or close one. A
question is written into the person's main conversation and sent to their
linked chat app; the answer is pressed only after the screen is read again.
The decision is
`docs/decisions/20261008-the-agent-works-in-the-persons-herdr-sessions.md`.

The three are independent. Hooks need no herdr, herdr needs no hooks, and
without a transcript source the hooks still show what was filed before.
