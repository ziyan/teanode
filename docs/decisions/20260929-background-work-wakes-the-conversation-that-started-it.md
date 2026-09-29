# Background work wakes the conversation that started it

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

A survey takes minutes, and a subagent going through a repository can take
ten. Both ran inside the turn that asked for them: the person watched one
tool call for the whole time, the turn could do nothing else, and a survey
asked from the command line held one HTTP request open for a quarter of an
hour, which a proxy with a short timeout cut off.

`20260923-a-background-command-wakes-the-conversation-that-started-it.md`
solved the same problem for shell commands: the command goes on, the turn
ends, and the conversation is woken when the command ends. That decision
rests on the program on the person's computer, which holds the command and
says it ended until the server acknowledges it. A survey or a subagent runs
on the server, where nothing outlives a deploy but the database.

## Decision

A survey or a subagent can be started in the background. The survey tool
does so unless told to wait; the subagent tool when told to. The call
answers at once with an id.

The work is a row of `agent_background_work`, holding what was asked, where
to wake, and what came of it, run by a queued job whose subject is the row.
The row is the record the computer is for commands: a job claimed again
after a restart finds it running and runs it again, and a sweep wakes
finished work whose wake was lost, the way a computer says an ending again
until it is acknowledged.

When it finishes, it wakes the conversation through the same waker as a
command, under the same rules. The woken turn opens with a message marked
`[background work]` and carries the result fenced as untrusted data. Only a
turn with somebody present starts background work, so only such a turn is
woken. A conversation takes at most twenty woken turns, commands and work
counted together, before the person writes there again; after that, and when
out of budget, what finished is written into the transcript as a note.
Stopped work wakes nothing: whoever stopped it knows.

A subagent in the background keeps the tools of the turn that started it,
less those that start or manage such work, but it shows no card. A waiting
subagent shows its cards in the turn that started it; that turn has ended, and
a card raised in the subagent's own run would sit where nobody reads. A call
that needs the person's word is refused, and the subagent says what it would
have done, for the woken turn, which can show a card, to take to them.

## Consequences

The command line and the API start a survey and ask for it every few
seconds, so no request is held open for minutes. `SurveyAgentMemory` stays
for clients that ask it.

Work can run twice: a job claimed again after a restart runs from the start.
That costs the work again, which the background command decision already
accepts for a wake. The conversation is woken once for it all the same: the
wake claims the row first, and only the claimer wakes, so the instance that
ran the work and another instance's sweep never both do. A claim is held for
half an hour, longer than a woken turn takes; one older than that belongs to a
server that went down mid-wake, and the sweep takes it again.

The twenty is now the bound on chains of either kind: a survey whose woken
turn starts a subagent whose woken turn starts a command is one chain. The
count lives on the conversation's row, not in one instance's memory, so a
chain spread over instances stops at twenty too. A subagent in the background
can do less than a woken turn, never more: it runs with the permissions of
the turn that started it, kept in its row, whatever the person may do by the
time it runs.

Work is bounded at twenty minutes, inside its job's bound, so reaching it is
a failure on the row rather than a job put back in the queue. Work whose job
was lost is failed after a day; finished work is removed with the runs, on
the runs' retention.
