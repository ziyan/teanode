# Coding sessions are shown memory by hooks, kept to the checkout's project

- Status: accepted
- Date: 2026-10-09
- Deciders: the person, agent

## Context

Claude Code and Codex could already reach the agent's memory through the
inward MCP server, but only by deciding to call a tool, which they rarely
do. A session therefore started without what TeaNode had read about the
checkout: what was decided, what failed before, where the last session
stopped. Both tools run hooks at points in a session's life and add what a
hook prints to the model's context. Recall had become fast enough (no model,
milliseconds) to run on every prompt.

## Decision

`teanode hook install claude-code|codex` adds hooks that show a session the
checkout's project page, lessons and the last session there when it starts,
the prompt's recall before each prompt, and ask the transcript source to
read again after each answer. Recall in a session is kept to the checkout's
project, the pages under it and linked to it, and lessons. Where the last
session stopped is read from its stored transcript, not written by a model.
Capture reuses the existing `claude-code` and `codex` sources rather than a
second way for transcripts to arrive. A hook that fails shows nothing and
exits cleanly.

## Consequences

Every prompt costs an embedding and a few queries, and a session start a
few more; nothing is marked as used, so the hooks do not shift importance
or decay. A session outside any checkout the agent has profiled is shown
nothing, and a project the agent knows little about shows little: the hooks
are only as good as the checkout's page. The project scope keeps the
person's private life out of a third party's model, at the price of a
prompt that is about something outside the project recalling nothing
unless the hook was installed with `--everywhere`. The capture depends on a
source of the tool's type being set up on that computer and its daemon
running; without one the next session still sees what was filed before.
