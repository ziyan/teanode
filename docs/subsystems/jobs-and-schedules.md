# Jobs, and work at a time somebody chose

Everything the agent does with nobody watching.

`internal/agent/agent.go` (the worker), `triage.go`, `summarize.go`,
`embed.go`, `reply.go`, `send.go`, `research.go`, `schedule.go`, `goal.go`,
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

**Claiming is `FOR UPDATE SKIP LOCKED`**: foreground work first, then ingest,
dream and backfill, oldest first within each group. Each claim receives a unique
identifier as well as the instance name. The attempt count records every claim;
the failure count records only errors that need retries. Finishing requires the
same running claim identifier, so even a late worker on the same instance cannot
finish a replacement claim.

## The tick

Every five seconds while the agent feature is on, the worker queues due work and
runs housekeeping, even when all execution slots are occupied. If a slot is free
it releases stale claims and claims more work. Ordinary jobs have a ten-minute
deadline; ingest and dream use the longer bounds in `job_policy.go`. A claim
expires five minutes after its kind's work deadline. Recording an outcome has a
separate ten-second context so cancellation cannot strand an otherwise finished
job or hang shutdown indefinitely.

## Failure, retry, and giving up

| Failure | Then |
| --- | --- |
| 1 | try again in 1 minute |
| 2 | 5 minutes |
| 3 | 15 minutes |
| 4 | 1 hour |
| 5 | 4 hours |
| 6 | dead |

Six failures, about five and a half hours. A dead job keeps its error and is
listed for an operator, who can put it back by hand.

A *deferral* is not a failure: the job returns to the queue with the time it may
run again. A budget that has run out and a reply still inside its hold window
both defer. Neither a deferral nor a shutdown cancellation consumes the failure
allowance. A manual retry of a dead or cancelled job clears its failure count
and terminal timestamp, while retaining the claim attempt count for diagnostics.

### Upgrading claim tracking

Migration `0090_agent_job_claims` adds failure counts and individual claim
identifiers. Stop and drain every old worker before starting the new binary;
old and new workers must not share the queue during this upgrade. Existing
running rows return to the queue, and historical attempts are retained. Their
failure counts start at zero because historical attempts also included waits.

Before a downgrade, stop every new worker and back up the database. The reverse
migration requeues unfinished work and drops the new columns. The older binary
cannot retain the separate failure allowance or claim-identity protection.


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
- **goal** — a turn of the agent's own toward the goal on a conversation. Its
  subject is the conversation, so the queue's own rule of one open job per
  agent, kind and subject is what keeps a conversation to one goal turn at a
  time.
- **noop** — proves the queue end to end.

## Replying on somebody's behalf

The most guarded path in the system. Before a model is asked anything, a reply
must pass a ladder, in this order: the mail server's own auto-reply refusals
(lists, bounces, spam, an envelope sender nothing vouched for, an hourly cap),
the insight saying a reply is wanted, the never list, the scope (somebody in
the address book, or an allow list, or anyone), the categories the policy
answers, the hours or the away window, whether the person has already replied
themselves, whether a reply is already held for the thread, and the day's cap.

What passes is written as an ordinary draft in Drafts, held for a number of
minutes the person set, and a send job is queued for when the hold ends.

At send time the ladder runs again, and so do fresh checks: the draft still
exists, the message is still in the Inbox, answering is still switched on. The
sender then locks the draft and reply, verifies the reply is still held and
unchanged, and accepts the mail in the same transaction as its Sent state,
hourly count, answered flag and draft removal. Delivery starts after commit.
A repeated job finds the committed reply state and does not accept another
message. Failure before commit restores the held reply and its draft; changed
held content is retried from a fresh snapshot.

A person's takeover cancels a held reply as part of accepting their message,
even if later draft cleanup needs recovery. Cancellation rechecks the reply
under its lock and cannot change an already accepted reply back to Cancelled.
The status called *sending* is now only an intermediate uncommitted state for
new sends. A persisted Sending record from an older release still means its
outcome is unknown and is marked failed with a note to check Sent. Stop older
send workers before upgrading; their acceptance path cannot join this boundary.

The mail goes out as the person, marked in the audit trail as the agent's
doing, carrying `Auto-Submitted: auto-replied`.

## Schedules

A schedule is a name, a cron line, a prompt, and where the answer goes: the
drawer or an email.

Three forms are understood. Five cron fields in the person's own zone; `@at`
and a moment, which runs once and is then removed; and `@in` and a
distance, which is resolved to a moment when it is stored, so it does not move
every time it is looked at.

Due schedules are found under a row lock, their next run is computed and
written, and a job is queued. A schedule whose agent is switched off still
advances, so it skips its runs rather than firing them all at once when the
agent comes back. A schedule with no time left is ended by its run once that
run is done, not when it is queued: switched off as it was queued, the run
found it off and did nothing, and every reminder for one moment was dropped.
A schedule for one moment is then removed, having done what it was for; one
whose cron line stops making sense is switched off, so it can be seen and
mended.

The run is a headless turn with the whole catalog the person's permissions
allow — but nobody is present to confirm anything, so every destructive or
outward call is refused with an explanation rather than performed.

A schedule that answers in the drawer takes that turn in the conversation it
was made in, the way a goal's check-in does: the turn opens with a message
marked `[schedule]`, which the drawer draws as a muted line, saying which
schedule is due and what it says, fenced as a note when the agent wrote it.
One made from the dashboard or the command line, or whose conversation is
gone, takes it in the main conversation. A schedule that answers by mail runs
in a `run` transcript of its own and mails what it said, the subject being the
first line when that line is short enough.

Mail notifications use the job identifier as a durable submission identity.
Acceptance, Sent filing and the submission record commit in one transaction;
a failure of the final SQL write rolls all of them back. A retry returns the
existing acceptance even if the generated answer or notification address changed.
The same schedule's next occurrence has a new job and may send a new answer.
Before another scheduled model turn starts, the worker checks whether that job's
mail was already accepted. This record survives message retention. Goal notices
use the same acceptance path but remain best effort when the first attempt fails.
This does not make every tool effect or conversation insertion within a scheduled
turn idempotent, and it does not guarantee exactly one remote SMTP delivery.

## Goals, and how they differ from a schedule

A schedule is a clock with a prompt. A **goal** is a sentence on a
conversation that the agent works toward until it is met, and the two look
alike from the queue and are opposite everywhere else:

| | schedule | goal |
| --- | --- | --- |
| where the turn runs | the conversation it was made in (a `run` transcript when it mails) | the person's own conversation |
| what it remembers | the conversation, when it answers there | everything, it is the same transcript |
| when the next one is | the cron line says | the last turn says, within bounds |
| how it ends | one for a moment is removed once it has run; a repeating one when somebody switches it off | the agent says `met`, or the person clears it |

The sweep, `dueGoals`, queues one job per conversation whose goal is
`working` with its time passed, and writes nothing: the dedupe on the subject
means the sweep five seconds later finds the job in flight, and the handler is
what moves the time on, since only it knows what the turn decided.

`runGoal` then, in order: reads the conversation and stops if the goal is no
longer working; counts the goal jobs finished for this conversation since the
person's own midnight and, at **forty-eight**, puts the next turn at tomorrow's
midnight with a note saying so; counts them again since the later of the
goal's setting and the person's last word and, at **twenty-four**, sets the
goal `waiting` with the note "Goal stalled: …", writes that line into the
transcript, and mails them under the same subject, so a goal nobody can meet
stops rather than costing the day's cap every day and the person sees in the
conversation that it did, with their three ways on -- write, clear, change; checks the
budget and, when it is spent, puts the next turn at the reset with the
budget's reason as the note; and otherwise runs one headless turn in the
conversation with the check-in as its message.

Afterwards it reads the row again. A turn that called the `goal` tool has
already written what happens next. One that did not is silent, and the next
turn is **twice as far off** as the last, bounded by five minutes and a day —
so a goal waiting on a nightly build backs off by itself. A turn that ended
`waiting` or `met` is **told to the person by mail**, from a granted mailbox to
the account's notification address, the same path a schedule's answer takes;
it is best effort and never a dead letter, because the state is on the
conversation either way.

A goal that waits is resumed by the person: at the end of their own turn in
that conversation the state goes back to `working`, a minute out. The bounds
are constants in `internal/agent/goal.go` and
`internal/agent/tools/goal`; there is no setting.

## Caveats

- **The day's cap counts jobs, not turns.** A goal job that did nothing but
  postpone itself for the budget still counts toward the forty-eight, and one
  the retention sweep removed no longer does.
- **A goal turn cannot confirm anything**, being headless, so a goal that needs
  something sent or deleted can only prepare it and call `goal` with `wait`.
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
