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
`RecallAgentCodingMemory`), `teanode agent memory checkout`, the Recall
dialog and the iPhone's Coding menu ("What a session sees"). Reading a
computer's new sessions now, which the hooks ask after each answer, is the
memory tool's `capture` action, `CaptureAgentCodingSession`, `teanode agent
memory capture`, "Read new sessions" on the Coding page and the iPhone's
Coding menu. The decision is
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

A Claude Code session also hands parts of a request to agents of its own, and
an agent can start agents under it. The `subagents` action (the herdr tool's,
`ReadAgentHerdrSubagents`, `teanode computer herdr subagents`) shows them as a
tree: each agent's kind, one-line task, model, whether it runs, is done or was
stopped, how long it ran, and the tools it used, with the agent that started
it named. The program reads them from the files Claude Code writes beside the
session's history (`<session id>/subagents/`), so it needs no hook and sees
agents started before it looked. They are those started for the person's last
request, plus any still running from before it, the newest 100. Older versions
of Claude Code never write that an agent finished, so one without that mark is
done when its last reply ended its turn, and running while it is still being
written, for 30 minutes after a reply that called a tool (nothing is written
while a build or a test suite runs), or while the session works or asks; it is
stopped otherwise. An agent interrupted mid-tool by a version that writes no
stop mark shows as running for up to 30 minutes, or while the session works on
the same request; one started for an earlier request runs only by its own
signs. Anything the person types to the session, even while it works, starts a
new request. A Codex session has none and
answers with an empty tree.

The three are independent. Hooks need no herdr, herdr needs no hooks, and
without a transcript source the hooks still show what was filed before.
