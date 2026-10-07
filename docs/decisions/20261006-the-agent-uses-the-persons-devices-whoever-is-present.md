# The agent uses the person's devices whether or not they are present

- Status: accepted; amended by `20261007-the-person-chooses-what-the-agent-does-alone.md`, where the person allows kinds of call to run with nobody present
- Date: 2026-10-06
- Deciders: the-owner

## Context

Runs with nobody present (a schedule, a goal, a night) were kept off most of
the person's own devices. Only the night could reach an attached computer, and
any other unattended run was refused with "the computer is not reached by a
run with nobody present". No unattended run could use the attached tab or the
terminal overlay, or reach an MCP server that runs on the person's computer.
In the headless browser it could only read. It could not leave a command
running in the background or start background work.

The owner set up a goal that saves photos from their mail every three hours
with a script on their own computer. Every run was refused the computer while
the computer was attached all day, and the agent reported it as unreachable.
This is a personal agent working for one person on their own machines, and it
is expected to do what they asked whether or not they are watching.

## Decision

Any run reaches the person's attached computer, terminal and browser, and
the MCP servers that run on their computer, with or without the person
present. Their attached tab is read by any run but acted in only with the
person present, since a click there acts as them, signed in; and a server on
their computer is never offered to the restricted runs that read mail from
strangers. A run that has a conversation to wake (a schedule's, a goal's, the
person's) may leave a command running in the background and start background
work, and is woken where it ran when that ends; a goal's own turn starts the
count of turns that may wake it again, as the person's word does elsewhere.
Only a run that is a transcript of its own, such as a night's, has nothing to
wake and runs commands in the foreground.

What stays is the confirmation a call is held to. A call that needs the
person's word (by its risk, by the command judgement, by the operator's or the
person's confirm list) is still refused when nobody is there to give it, and
the run is told to say what it would have done.

This supersedes, for unattended runs, the computer rule in
`20260911-the-computer-is-a-device-the-person-runs.md` and
`docs/subsystems/devices.md`, the headless rule in
`20260915-a-server-the-computer-runs-is-the-persons.md`, the tab rule in
`20260910-the-attached-tab-is-the-persons.md`, and the first of the three
bounds in `20260923-a-background-command-wakes-the-conversation-that-started-it.md`.

## Consequences

A scheduled or goal turn can now run anything on the person's computer that a
turn they are watching could run without a card: every shell command is a
write, and writes do not ask. A message or a page such a run reads can try to
steer it, and nobody is watching when it does. What still stands between that
and an act that cannot be undone is the confirmation rule, so the person's
confirm list is now the way to keep a category of action for when they are
present.
