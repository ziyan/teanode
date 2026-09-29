# A round runs its reads together

- Status: accepted
- Date: 2026-09-29
- Deciders: the-owner

## Context

A model may answer with several tool calls at once, and the providers ask
for that: the ChatGPT one sends `parallel_tool_calls`. Over a week of one
deployment's conversations, 4,267 answers asked for between two and
eighteen calls, and 64 of them for more than one `subagent`. The loop ran
them one after another, so a round of six searches took six times as long
as one, and two subagents took twice as long as the slower.

`20260923-a-background-command-wakes-the-conversation-that-started-it.md`
said the loop still runs them in order, and that what is slow goes in the
background. That covers a command that outlives the turn. It does not
cover a handful of reads the model needs before it can answer.

## Decision

A round's calls run a batch at a time, in the model's order. Calls that
follow each other and only read are one batch and run together, at most six
at once. Any other call is a batch of its own: it starts after everything
before it has answered, and nothing after it starts until it has.

A call only reads when its risk for its arguments is `read` and it raises no
card before it runs. `subagent` and `survey` are reads by their risk. The
browser is not, even to read, because a run has one page; nor is a call the
fast model judges, because whether it asks is not known until it is judged.

Only the calls run together. The history, the transcript, the stuck check
and the pictures are kept after each batch, one call at a time and in the
model's order, as they were.

A run puts one card at a time. Two subagents in one batch can each reach a
call that asks, and both cards are shown in the parent's conversation. A
chat app keeps one waiting card per chat, so its "yes" answered the second
and left the first to time out. The second card now waits for the first to
be answered, and the work around them goes on.

## Consequences

This amends the line of the background-command decision that says the loop
runs a round's calls in order. It runs its reads together; everything else
still runs in order.

A read the model asked for after a change reads the change, because the
change is a barrier. Two reads in one batch see the world as it was before
the batch, whichever order they finish in, which is all a model asking for
them at once can expect.

A turn can now hold six database transactions or six outgoing calls at
once where it held one. Stop cancels the batch, and the answers of calls
that finished before it are not kept, as a call cut short never was.

A tool's per-turn state must bear being called from several goroutines at
once. What a tool reaches through the run (loading a tool, recall, the one
survey a turn may start, the judged calls) is under the run's lock; a new
tool that keeps state on the run has to be too.
