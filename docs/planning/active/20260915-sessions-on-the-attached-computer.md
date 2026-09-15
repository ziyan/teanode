# Sessions on the attached computer: stdio servers, a terminal, and subagents

## Why this matters

Three things were asked for. Two of them are the same thing underneath, and
the third is unrelated and much smaller, so it can go first.

1. **A connected server the person's own computer runs.** Today a server
   spoken to over a command is the operator's: it is declared in the
   configuration and the command runs on this server. The ask is for the
   command to run on the computer the person attached instead -- their
   machine, their installed programs, their credentials.
2. **An interactive terminal.** The shell tool runs one command and returns
   when it ends. Anything that prompts, or that is worth watching, needs a
   session that stays open.
3. **Subagent calls.** The agent hands a piece of work to a nested run of
   itself and gets back an answer, without the person managing a second
   agent.

(1) and (2) both need something the device protocol does not have: a
long-lived process with input and output flowing while it runs. (3) needs
nothing from the device at all.

## What already exists, and where

`internal/agent/device.go` is strictly one numbered request to one answer:
`Ask` puts a channel in `pending[id]`, sends `{type, id, action, args}`, and
waits up to a minute for `{id, ok, data}`. Every action the computer
understands -- `shell`, `filesystem` -- fits that shape because each one
ends.

A process that stays open does not. It needs output to arrive when the
process feels like it, which is a message the server did not ask for, and
input to be written to it later, which is a request about something started
earlier.

## The work

### A. Subagents (no device work; can land on its own)

A `subagent` tool: a prompt, an optional set of tool families, and an
answer. It runs a nested ask loop with its own round cap, drawing on the
same person's daily budget and recording its own run transcript, shown in
the drawer under the call that made it.

Bounds, all of them named constants with reasons:

- depth 1. A subagent has no `subagent` tool. Recursion here is a way to
  spend somebody's budget in a loop with nothing to show.
- its own round and tool-call caps, smaller than a turn's.
- no tool that needs a confirmation, and no tool the parent could not use.
  A card the person answers belongs to the turn they are watching, not to
  something spawned inside it.
- the parent's sources, not more: the same mailboxes, the same computers.

A subagent may reach the attached computer exactly as the turn that spawned
it can (settled with the person, 2026-09-15). The argument for it: the
person is present, it is their machine, and a subagent that cannot run
anything is not much use for the work anyone would delegate to one. The
argument against, which is why it was asked rather than assumed: the run
inside is not one they are reading as it happens. What answers that is not a
narrower tool set but a legible record -- the subagent's transcript is a run
of its own, shown in the drawer under the call that made it, so what it did
on their machine can be read afterwards.

### B. A session on the attached computer (the foundation)

New actions, and for the first time a message the server did not ask for:

    session_start   {kind: "stdio"|"pty", command, args, env, directory,
                     cols, rows}            -> {session}
    session_write   {session, data}         -> {ok}
    session_signal  {session, signal}       -> {ok}
    session_resize  {session, cols, rows}   -> {ok}      (pty only)
    session_close   {session}               -> {ok}

    {type: "session_output", session, stream: "stdout"|"stderr", data}
    {type: "session_ended",  session, code}

`deviceLink` grows a second route: a message carrying a `session` goes to
that session's subscriber rather than to `pending`. Sessions are held per
device, closed when the turn that opened them ends, when the computer
detaches, or after an idle timeout; bounded per person, and each one's
output is bounded too, so a command that prints forever is cut rather than
held.

The computer daemon gains the other half. A stdio session is
`exec.Command` with pipes. A pty session needs a pty library on the
computer side only -- the server never opens one.

What it costs, stated plainly: the computer's protocol stops being
request-and-answer, which is what made it easy to reason about. The
mitigation is that a session is always owned by a turn, and never outlives
one.

### C. A connected server the computer runs

`agent.mcp.servers[]` gains `location: server | computer`, defaulting to
`server` so nothing declared today changes. A server with
`location: computer` and a command is spoken to over a stdio session on the
person's attached computer: the MCP client's reader and writer are the
session's output and input, and the rest of the MCP code does not change.

This reopens a decision that was settled the other way. The record
`20260910-stdio-servers-are-the-operators.md` says a command-spoken server
is the operator's because the command runs on this server, as this server.
When it runs on the person's own computer, as them, that argument does not
hold -- it is no different from the shell tool they already have. So a
computer-located server may be declared by the person, and a new decision
record should say why the two are not the same thing.

Tools appear as `mcp__<server>__<tool>` as they do now, and are offered only
while that person has a computer attached.

### D. An interactive terminal, two ways in

A terminal arrives the way a browser tab does -- as a thing the person
attaches -- and also as something the agent can start for itself. Both drive
the same pty session; they differ in who is sitting in front of it.

**Attached.** `teanode terminal` opens a pty here, runs the person's shell
in it, and connects to `/api/v1/agent/terminal`, beside the two endpoints
that already exist for a computer and a tab. The person works in it as an
ordinary terminal. The agent sees what is on the screen and can type into
it, and a `<terminal>` overlay says one is attached and what it is showing,
the way `<tab>` does today. This is the one to watch something happen in:
the person and the agent are looking at the same screen, and either can
take over.

**Launched.** The agent starts a pty session on the computer already
attached -- `terminal` with `start` -- and nobody is sitting in it. This is
for driving a program that wants a terminal without asking the person to
open one.

One tool either way: `start`, `type`, `keys` (named keys and control
characters, which is most of what driving a program needs), `read`, `wait`
for the screen to settle, and `close`. It is destructive, so it asks, and
present-only like everything else that reaches the computer.

The screen, not the scrollback. A model handed ten thousand lines of redrawn
progress bars learns nothing; what a terminal is for is reading what a
program is showing now. The device keeps the last screen -- a real terminal
buffer, so that redraws and cursor moves resolve into what is actually
displayed -- and `read` answers with that, plus an "it has not changed since
you last looked" so a wait loop is cheap.

**The test that decides whether this works** is driving codex through it to
write a small program: start a terminal, run codex, answer what it asks,
watch it work, and end with a file on disk that runs. That exercises every
hard part at once -- a program that redraws, that prompts, that takes
minutes, and that only makes sense if the screen is read as a screen.

## Milestones

1. **A** -- subagent tool, bounds, transcript in the drawer.
2. **B** -- sessions end to end: protocol, server routing, daemon side,
   with a test that starts `cat`, writes, reads and closes.
3. **D1** -- the terminal tool over a launched session, with a screen
   buffer, and codex driven through it.
4. **D2** -- `teanode terminal`, the attach endpoint, the overlay.
5. **C** -- `location: computer`, the MCP client over a session, the
   decision record, the person's own declaration.

D comes before C because the pty half is the part that is hard to get right
and the codex test is what proves the session layer is real. A connected
server over stdio is the easy case once a session works: no screen, no
keys, just a reader and a writer.

## Verification

Unit tests for the session router with a fake device, including a session
that ends by itself, one that outruns its bound, and a computer that
detaches mid-session. End to end on a server with gen7 attached: a stdio
server of the person's own answering a tool call, and a terminal running
something that prompts.

## Progress

All five milestones are built and were driven against a real server with a
real computer attached (gen7), on 2026-09-15.

- **A, subagents** -- a nested run with its own rounds and transcript; its
  confirmation cards appear in the parent's turn. A subagent ran the github
  skill through the attached computer and its card was answered in the
  parent conversation. Merged separately in #97.
- **B, the session layer** -- `session_start/write/signal/resize/close` and
  the unsolicited `session_output`/`session_ended`; a bounded buffer per
  session on the server, sixteen sessions a computer, thirty minutes idle.
  `cat` started, written to, answering as it went, closed cleanly.
- **D1, the terminal** -- pty on the computer, screen kept there with a
  terminal emulator, read as a screen. The agent opened python3, assigned,
  printed 42, answered an `input()` prompt, printed what it answered, left
  with ctrl-d. It also opened codex, dismissed the update prompt with down
  and enter, accepted the trust prompt, typed a request, and read on the
  screen that codex's own login had expired -- the mechanism working up to
  a wall that is codex's, and the person's to re-open with `codex login`.
- **D2, the attached terminal** -- `teanode terminal`, connecting as a
  computer with one pty already open. The agent read the screen the person
  was sitting at and typed `echo hello from the agent` into it; the person's
  own terminal showed the command and its output.
- **C, a server the computer runs** -- `location: computer`; the stdio
  transport speaks over a session's pipes. `mc mcp serve` ran on gen7, its
  two tools were discovered, and `mc_help` answered.

## Decisions

- **2026-09-15, the person:** build it, in the order A, B, D, C.
- **2026-09-15, the person:** a subagent reaches the attached computer
  exactly as its parent does, answered by a legible transcript rather than a
  narrower tool set.
- **2026-09-15:** the terminal comes before the connected server, because
  the pty half is the hard half and driving codex through it is what proves
  the session layer is real.

- **2026-09-15:** the screen lives on the computer, not the server. No
  terminal emulator on the server, none of the redrawing on the network,
  and reading the screen is one request however busy the program was.
- **2026-09-15:** an attached terminal is a computer connection with one
  session already open, not a new kind of connection. The difference
  between a terminal the agent opened and one the person is sitting in is
  who else is looking, which is a fact about the session.
- **2026-09-15:** opening a terminal asks; the keys after it do not. A card
  in front of each keystroke is a card nobody reads, and the program the
  person said yes to is what the keys go to.

## Still open

- A launched terminal that outlives its turn is closed by the idle sweep
  after thirty minutes. Whether that should be sooner, or tied to the turn,
  is not yet decided.
- A connected server the computer runs is dropped when the computer
  detaches and rediscovered when it returns; a call in flight at the moment
  of detaching fails. Waiting for the computer to come back is not done.
- With several computers attached, a server on "the computer" runs on the
  first by name. Choosing one is not yet possible.
- ~~codex driven to a finished program~~ Done, once codex was signed in
  again and its pinned model dropped: the agent opened codex in a terminal
  on gen7, accepted its prompt, asked for fizzbuzz.py, approved what codex
  asked, left codex, ran the program itself and reported the fifteen lines
  -- which matched the file on disk when run by hand afterwards.
