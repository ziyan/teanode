# The person chooses what the agent does alone

- Status: accepted
- Date: 2026-10-07
- Deciders: the-owner

## Context

`20261006-the-agent-uses-the-persons-devices-whoever-is-present.md` let runs
with nobody present reach the person's devices, and kept one rule: a call that
needs the person's word is refused when nobody can give it. For a personal
agent that keeps at things in the background, that rule is the wall it hits
most: a goal that should send the weekly summary, or a schedule that should
pay a bill the person set up, prepares the work and stops.

## Decision

The person chooses, on their agent's settings, which kinds of call the agent
may make without their word when they are not there: speaking for them
(`outward`, which includes acting in their signed-in browser tab), spending or
moving their money (`money`), what cannot be undone (`destructive`), giving
somebody access (`granting`), and the tools on their own "ask me first" list
(`listed`). Each is off until they turn it on. A call needing confirmation for
several reasons runs only when every one is allowed. The command judge answers
in the same kinds, so a command a skill or a connected server runs is held to
them too.

Three things are never allowed this way. A tool on the operator's confirm list
is its own reason, which no person can allow: that list only ever makes the
agent more careful. A run held to reading or to a few named tools (sorting,
drafting and research on mail from strangers) never uses the allowance. And
the setting itself changes only on a card the person answered: a run with
nobody present, a subagent of one, or a call let through by the allowance
cannot widen it.

Turns with nobody present are told what they may do, so that they do it
rather than preparing it and stopping; a subagent asks only when the turn
that started it can, and a turn woken in a goal's conversation counts as
having nobody there.

## Consequences

A message or a page an unattended run reads can try to steer it into a kind of
call the person allowed, and nobody is watching when it does. That is the
person's trade, made one kind at a time; the defaults keep the 2026-10-06 rule.
