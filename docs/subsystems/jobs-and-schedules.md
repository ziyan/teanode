# Jobs, and work at a time somebody chose

Everything the agent does with nobody watching.

`internal/agent/agent.go` (the worker), `triage.go`, `summarize.go`,
`embed.go`, `reply.go`, `send.go`, `research.go`, `schedule.go`,
`internal/db/database_agent.go`, `internal/agent/tools/cron.go`.

## The queue

One table. A job names the agent, the mailbox, a kind and a subject — usually
the id of the message or thread it is about — and carries its status, attempts,
the earliest time it may run, and who claimed it.

**Enqueuing is idempotent.** A job that is already queued or running for the
same agent, kind and subject is returned instead of a second one being made; a
partial unique index makes that safe when two deliveries race. The mailbox is
not part of that key, so the same message queued from two mailboxes of one
agent coalesces into the first.

**Claiming is `FOR UPDATE SKIP LOCKED`**, oldest first, as many as there are
free slots, and each claimed row is marked running with this instance's name
and its attempt count raised. A claim that has gone quiet for fifteen minutes
is put back. Finishing a job is guarded by the claimant, so an instance whose
claim lapsed cannot overwrite whoever holds it now.

## The tick

Every five seconds, while the agent feature is on and a slot is free: queue any
due schedules, sweep once an hour, describe conversations that have gone quiet,
sweep idle browser contexts, release stale claims, and claim what is due. Each
job runs in its own goroutine under a **ten-minute** deadline, five minutes
inside the stale-claim window.

Because the tick returns early when every slot is busy, a deployment with one
slot and one long run does none of the housekeeping until it finishes.

## Failure, retry, and giving up

| Attempt | Then |
| --- | --- |
| 1 | try again in 1 minute |
| 2 | 5 minutes |
| 3 | 15 minutes |
| 4 | 1 hour |
| 5 | 4 hours |
| 6 | dead |

Six attempts, about five and a half hours. A dead job keeps its error and is
listed for an operator, who can put it back by hand.

A *deferral* is not a failure: the job returns to the queue with the time it may
run again. A budget that has run out and a reply still inside its hold window
both defer. Attempts are counted at claim time and never reset, so a job
deferred five times dead-letters on its first real failure.

## The kinds

- **triage** — sorts one message: a category from the fixed vocabulary plus the
  person's own, a priority, whether somebody is waiting on an answer, a
  sentence of summary and what it asks for. It writes the insight, records a
  transcript, then runs the mailbox rules that were waiting for a category, and
  queues a reply or a research run if the policy asks for one. Five categories
  never need a reply whatever the model says: newsletter, notification, receipt,
  promotion and social.
- **summarize** — one thread. It summarizes incrementally: only what arrived
  since the last summary, with the previous text as the base, so a hundred
  messages cost a hundred messages once.
- **embed** — one message's vector for search by meaning.
- **backfill** — catches up an existing mailbox when it is granted: vectors
  first, then triage, two hundred at a time, requeueing itself ten minutes
  later while there is more and the person asked for all of it.
- **research** — a read-only turn with a fixed, small tool set that gathers
  context before a reply and writes notes onto the insight.
- **reply** — drafts an answer on the person's behalf. Everything below.
- **send** — sends one held reply once its hold has passed.
- **schedule** — a turn at a time somebody chose.
- **noop** — proves the queue end to end.

## Replying on somebody's behalf

The most guarded path in the system. Before a model is asked anything, a reply
must pass a ladder, in this order: the mail server's own auto-reply refusals
(lists, no-reply senders, bounces, spam, a per-sender quiet period), the
insight saying a reply is wanted, the never list, the scope (a contact seen
more than once, or an allow list, or anyone), the categories the policy
answers, the hours or the away window, whether the person has already replied
themselves, whether a reply is already held for the thread, and the day's cap.

What passes is written as an ordinary draft in Drafts, held for a number of
minutes the person set, and a send job is queued for when the hold ends.

At send time the ladder runs again, and so do fresh checks: the draft still
exists, the message is still in the Inbox, answering is still switched on. A
reply is marked *sending* before the mailer is called, so a crash between
sending and recording is settled as failed with a note to look in Sent rather
than sent twice. The sender is marked replied to only after it has gone, so a
retry is not refused by its own first try.

The mail goes out as the person, marked in the audit trail as the agent's
doing, carrying `Auto-Submitted: auto-replied`.

## Schedules

A schedule is a name, a cron line, a prompt, and where the answer goes: the
drawer or an email.

Three forms are understood. Five cron fields in the person's own zone; `@at`
and a moment, which runs once and then switches itself off; and `@in` and a
distance, which is resolved to a moment when it is stored, so it does not move
every time it is looked at.

Due schedules are found under a row lock, their next run is computed and
written, and a job is queued. A schedule whose agent is switched off still
advances, so it skips its runs rather than firing them all at once when the
agent comes back. A cron line that stops making sense switches the schedule
off.

The run is a headless turn with the whole catalog the person's permissions
allow — but nobody is present to confirm anything, so every destructive or
outward call is refused with an explanation rather than performed. The answer
goes into the main conversation, or out as mail whose subject is the first line
when that line is short enough.

## Caveats

- **`@in` only works from the agent's own tool.** The command line and the
  dashboard save a schedule without resolving it, so `@in 20m` there fails with
  a message about five cron fields.
- **Day and weekday are ANDed**, not ORed as in classic cron: a line naming both
  runs only when both match.
- **Embedding backfill only happens through a backfill job**, and a backfill job
  is only queued when a mailbox is granted with triage on. A mailbox granted
  with search by meaning but triage off never gets vectors for the mail it
  already has, and changing the embedding model schedules nothing.
- **A deferral spends a retry**, as above.
- **The describer takes no claim**, so two instances can describe the same
  conversation and pay for both calls.
- Sending a scheduled answer by mail does not carry the audit principal that a
  reply does.
