# Jobs, and work at a time somebody chose

Everything the agent does with nobody watching.

`internal/agent/agent.go` (the worker), `triage.go`, `summarize.go`,
`embed.go`, `reply.go`, `send.go`, `research.go`, `goal.go`, `goal_times.go`,
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
- **goal** — a turn of the agent's own toward a goal, in the goal's own
  conversation, at a time the goal set itself or at one of the times somebody
  chose for it. Its subject is the conversation, so the queue's own rule of one
  open job per agent, kind and subject is what keeps a goal to one turn at a
  time. The tick also runs `surfaceGoals`, which says each goal that came to
  need the person in their main conversation, once; see `conversations.md`.
- **background** — one survey or subagent the agent did not wait for. Its
  subject is the `agent_background_work` row, which holds what was asked and
  keeps what came of it; see `the-ask-loop.md`. It has a bound of its own,
  twenty minutes, a little inside the job's, so work that reaches it fails on
  its row rather than going back in the queue.
- **watch** — one look by one watch of an installed skill; its subject is
  `skill/watch`. The tick queues each watch on the cadence the skill gives it,
  ten minutes by default, for each person it applies to, and the queue's one
  open job per agent, kind and subject keeps looks from piling up behind a
  computer that is slow to answer. See `agents.md`, "Telling the person,
  unasked".
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

## Goals, and the times they run at

A **goal** is a sentence the agent works toward in a conversation of its own
until it is met or the person drops it (`conversations.md` has the rest of
its life: how it calls the person, how they answer). This section is when its
turns are.

A goal keeps its own pace unless it is given **times** or **mail** to wait
for. Its times (`goal_times`) are cron lines in the person's zone, five fields
(`30 7 * * 1-5`), or `@at` and a moment, which is dropped once it has passed;
`@in` and a distance is accepted and stored as the moment it means, so it does
not move every time it is read. Its mail (`goal_mail_conditions`) is the tests
a mailbox rule makes (from, to, subject, a header, or any message; contains,
equals or matches), only those a message can be judged by as it arrives. A goal
keeps at most twelve times. The daily brief and the asset estimates are goals
their switches start, found by title.

| | at its own pace | on set times |
| --- | --- | --- |
| when the next turn is | the last turn says, within bounds | the soonest of its times, or now when its mail came |
| what decides it | the agent, through `note` | the worker, whatever the turn asked |
| the day's caps | apply | do not: the person chose the cadence |
| how it ends | the agent says `met`, or the person closes it | the same; times that are all moments end with `met` |

The sweep, `dueGoals`, queues one job per goal conversation that is `working`
with its time passed, and writes nothing: the dedupe on the subject means the
sweep five seconds later finds the job in flight, and the handler is what
moves the time on, since only it knows what the turn decided. A goal that is
`paused` is not queued and hears no mail; taken up again it is due at its next
time.

`runGoal` then, in order: reads the conversation and stops if the goal is no
longer working; for a goal at its own pace adds up the tokens of every goal turn since
the person's own midnight and, once they reach `agent.limits.goalShare` of
the daily budget (default half), puts the next turn at tomorrow's midnight
with a note saying so, and counts this conversation's goal jobs since the later of the goal's setting and the person's last word
and, at **twenty-four**, sets the goal `waiting` with the note "Goal stalled:
…", writes that line into the transcript and lets the sweep call the person,
so a goal nobody can meet stops rather than costing the goals' share every day;
checks the budget and, when it is spent, puts the next turn at the reset with
the budget's reason as the note; and otherwise runs one headless turn in the
conversation with the check-in as its message.

The check-in says what the goal is for: in the person's words when they wrote
it, or pressed the switch that started it (`goal_written_by` is `person` or
`built_in`), and fenced as a note the agent wrote for itself otherwise, since
an agent writes on the strength of what it has read and what it has read
includes mail from strangers. A goal on set times is also told its times,
which of them this turn is for, and what mail came, oldest first, with senders
and subjects quoted as data and an unverified sender said to be one. A turn a
goal woke counts as the person asking (which Passwords needs) only for a goal
the person wrote, with a tap on a screen, on times they chose and waiting for
no mail: the command line's token is one the agent's own shell holds too, so a
goal written or changed from there is the agent's.

Afterwards a goal on set times goes on to its next time whatever the turn
said, forgets the messages it was told about (those that came during the turn
stay, and make it due again at once), and drops a moment that has had its
turn; with no time left and no mail to wait for, it goes back to a pace of its
own. A failed turn on set times is not tried again before its next time,
since each try would post its cards again. A goal at its own pace reads the
row again: a turn that called the `goal` tool has already written what happens
next; one that did not is asked once more with the goal tool alone, and failing
that the next turn is **twice as far off** as the last, bounded by five
minutes and a day, so a goal waiting on a nightly build backs off by itself.

What a goal tells the person goes to their feed with the feed tool. The cards
of one turn of a goal on set times are one stack, keyed by that turn's job, so
each morning's brief is a stack of its own; with `is_goal_notified_when_posted`
the first card of a turn sends one quiet notification. A `wait` and a `met` are
said in the person's main conversation by `surfaceGoals`.

### Mail a goal waits for

The delivery hook (`noteMailForGoals` in `internal/agent/goal_times.go`) runs
inside the delivery transaction for a mailbox the agent was given. For each
working goal whose conditions the message passes, unless the filters put it in
Junk or Trash, it appends the message to the goal's `goal_mail_arrivals` (its
item id, mailbox, From line, subject, when, and whether DMARC passed or it was
submitted here) and makes the goal due now. Nothing slower happens there, for
the same reason as the agent's other delivery work. Messages that come together
share one turn; at most fifty are kept for a turn, and past that the oldest are
dropped and the turn is told more may have come.

## Caveats

- **The goals' share counts tokens, not turns.** Goals at their own pace spend
  at most `agent.limits.goalShare` of the day's budget between them; a goal
  that finds it spent waits for tomorrow. A goal on set times is outside the
  share, and is bounded only by its times and the budget.
- **A goal turn cannot confirm anything**, being headless, so a goal that needs
  something sent or deleted can only prepare it and call `goal` with `wait`.
- **Day and weekday are ANDed**, not ORed as in classic cron: a line naming both
  runs only when both match.
- **Embedding backfill only happens through a backfill job**, and a backfill job
  is only queued when a mailbox is granted with triage on. A mailbox granted
  with search by meaning but triage off never gets vectors for the mail it
  already has, and changing the embedding model schedules nothing.
- **A deferral spends a retry**, as above.
- **The describer takes no claim**, so two instances can describe the same
  conversation and pay for both calls.
