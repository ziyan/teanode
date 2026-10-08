# The agent works in the person's herdr sessions, and the person answers their questions from anywhere

- Status: accepted
- Date: 2026-10-08
- Deciders: the-owner

## Context

People who use coding agents keep several open at once, each in a terminal
of its own. Herdr holds those terminals in panes, recognizes the Claude Code
or Codex session in each, and answers on a socket. The agent's `claude_code`
and `codex` tools started a new, headless run for each call, which knew
nothing of the person's sessions and which the person never saw. And a
session that stopped to ask a question waited until the person came back to
that terminal.

## Decision

The agent works in the person's own panes, through herdr, on every computer
they attached. It reads a session's history and screen, types an instruction
into the pane in front of the person, and is woken when a session it watches
finishes. It never starts a second process for a session, whether `claude -p`,
`codex exec` or a resume of the same session.

A question or an approval a session asks is the person's to answer. It is
written into their main conversation, which opens the drawer with the options
as buttons and goes on to their chat apps. Their answer is pressed into the
pane for them. Whoever answers first wins: each answer carries the
fingerprint of the question as it was shown, and the program on the computer
reads the screen again and refuses an answer whose question has gone or
changed. The agent's `herdr` tool carries the person's choice, with the
chosen options' labels, which must match the form. Sending and answering are
write risk and raise no confirmation card: a card for every answer was more
than the person wanted. A person who wants one can still list the tool in
their confirm list.

The program on the computer decides each session's state itself rather than
taking herdr's: herdr reads mostly the window title and reported a Claude Code
question form as idle, and typing an instruction into a form would answer it.

Every action is on the tool, the API, the dashboard and `teanode computer
herdr`, named in one table that a test checks against all four.

## Consequences

- One process, one history and one screen, which the person and the agent
  share. The person can take over at any moment, and sees everything typed.
- The questions are recognized from the screen, from layouts seen in the
  current Claude Code and Codex. A coding agent that redraws its forms
  differently will not be recognized until the recognizer learns it; the
  session then shows as idle or unknown, and its screen is still readable.
- Polling every pane every three seconds costs a socket read per pane, about
  0.4 seconds for fifteen panes, on a program that is otherwise idle.
- The questions waiting and the watches are kept in small files on the
  computer, so a restart of the program, which every deploy causes, loses
  neither.
- TeaNode's reporting hooks change the person's Claude Code settings, so they
  are put in only when the person asks, beside what is there, with a copy of
  the file kept first. They report and decide nothing.
