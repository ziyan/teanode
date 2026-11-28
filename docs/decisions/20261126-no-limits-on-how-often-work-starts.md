# No limits on how often or how much the agent's work runs

- Status: accepted
- Date: 2026-11-26
- Deciders: the-owner

Supersedes the share of the day and the turns alone in
`20261123-goals-spend-a-share-of-the-day.md`.

## Context

The agent's unasked work grew a bound for every way it could start, and
each bound surprised somebody:

- a goal told to look again in a minute looked in five;
- a goal stalled after twenty-four turns and waited to be restarted;
- the goals stopped for the day once they had spent half of it;
- a conversation stopped waking for finished background work after twenty
  endings;
- a sixth alert in a day never arrived, and nothing was said at night;
- a turn stopped after 150 rounds, a reply after six, a dream call after
  twelve.

The owner's rule: do not hardcode limits, and do not give people knobs nobody
sets. Each person's daily money limit (`agent.limits.dailyCostPerAgent`, or
the tokens beside it) and the server's monthly one stop spending whatever
starts the work, so a second, hidden bound on how often or how much adds only
surprise. Production sets the money limits and leaves the token caps off.

And two settings pages were goals in all but name: the daily brief, a goal
with a page of its own, and the weekly look around, a job with a day and time
picker.

## Decision

Each of these is removed outright: not a flag, not a setting, not a constant
moved elsewhere.

How often work starts:

- **A goal's wait.** `goaltool.Soonest` (5 minutes) and `Latest` (a day) and
  their clamp in `interval()` are gone. The wait a goal asks for is the wait it
  gets, except that it is never under a minute: a wait of zero or less is a
  minute, a loop guard rather than a limit, since a turn takes about that long
  and it is the minute a goal already waits after the person or another turn.
  A goal that never gave one keeps `DefaultInterval` (30 minutes).
- **Doubling.** `goalDoubled` is gone. A turn that made no goal call, or
  failed, keeps the wait the goal had (`goalKeptWait`).
- **Stalling.** `GoalTurnsAlone` (24) and the stall it caused are gone.
- **The goals' share of the day.** `GoalShare` (half), `goalShareSpent` and its
  held-back note are gone. This supersedes the share in the decision of
  2026-11-23.
- **Background wakes.** `BackgroundWakesAlone` (20) is gone, and the
  conversation's `background_wake_count` is no longer read or written (the
  column stays until a later migration). Every finished piece of work or ended
  command wakes its conversation; an organization out of budget still gets a
  note instead of a turn.
- **Alerts.** `AlertDailyMost` (5) and the quiet hours (`AlertQuietStart` and
  `AlertQuietEnd`, 22:00 to 07:00) are gone, with the alerts held for the
  morning. A card lands in the feed at any hour, and the phone's own Focus or
  Do Not Disturb decides whether it makes a sound.
- **Speaking first.** The 24 hours between speaking-first turns
  (`speakFirstApart`) is gone. Each subject is still raised once.

How much work runs:

- **Rounds.** `MaxRoundsPerAsk`, `MaxRoundsPerResearch`, `MaxRoundsPerReply`,
  `MaxRoundsPerTriage`, `MaxRoundsPerDream` and `MaxRoundsPerSubagent` are
  gone, with `MaxToolCallsPerRun`, which nothing read. A turn goes on until the
  model answers, the budget defers it, the loop guard stops it or the person
  stops it. A call built for one answer (a title, a summary) still takes one
  round, and the goal's second ask, which offers the goal tool alone, two.
- **Member turns keep their caps.** `memberTurnMaximumRounds` (8),
  `memberTurnSearches` (6) and `memberTurnFetches` (6) stay, with
  `memberTurnsWaitingMost`. A member's turn is somebody else's choice to spend
  the owner's money, and where the owner's model costs nothing in the daily
  accounting (a plan model) no budget would ever stop it. They protect the
  owner from other people's use; they are not caps on the owner's own work,
  which has none.
- **The journal job.** `journalRoundsMost` (5) is gone: the job goes on until
  nothing it has not tried is waiting.
- **Dreams.** `DreamShare` (0.3) is gone. A night is bounded by the person's
  daily limit like everything else, less what the day has already spent.
- **Embeddings.** `EmbeddingTokensPerDay`, which nothing read, is gone.
  Embedding never waits on the daily budget (see Budget coverage).

Two settings pages become goals:

- The look around job (`AgentJobNearby`, `queueNearbyRuns`) and the
  `LookAroundWeekday` and `LookAroundTime` settings are gone, with the weekend
  message and the `nearby` mute. Migration 0285 makes a built-in goal called
  "Look around" for each agent that was on and had alerts on, at the day and
  time they had chosen (Thursday at 17:00 otherwise). It searches the web,
  posts the few worth the person's attention to the feed, and adds a follow-up
  for anything with a day.
- The Daily brief operations `SetAgentBrief` and `RunAgentBriefNow` and
  `teanode agent brief` are gone. The brief is an ordinary built-in goal
  (which keeps it out of Passwords, since it reads the inbox). The ideas
  catalog offers "Send me a daily brief" to people who have none, and "Do it"
  starts the same goal with the same text, at `30 7 * * 1-5`.

Every removed YAML key still loads: it is reported once in the log and lost
the next time the file is written. The removed GraphQL settings fields
(`goalShare`, `dreamShare`, `goalTurnsAlone`, `backgroundWakesAlone`,
`maxRoundsPer*`, `maxToolCallsPerRun`, `embeddingTokensPerDay`, and the
agent's `alertDailyMost`, `alertQuietStart`, `alertQuietEnd`,
`lookAroundWeekday` and `lookAroundTime`) keep answering for one release, and
are ignored when sent, so an older app's settings page loads and saves. Their
columns stay until a later migration drops them.

## The Limits page holds only the budget

The owner looked at the Limits page and asked why these limits were necessary:
no one can configure them correctly. So the page, and `config.AgentLimits`,
hold only the person's daily tokens and daily cost and the server's monthly
tokens and monthly cost. Every other field stops being a setting:

- **Gone**, because they bound nothing the money limit does not:
  `digestDocumentRunes` (a dream reads every document whole, a long one in
  parts), and `requestTimeout` (below).
- **A constant, at the value it had, with its reason beside it:**
  `MaxRecordingMediaBytes` (2 GB), `MaxRecordingAccountBytes` (64 GB),
  `MaxAttachmentBytes` (25 MB), `MaxSentFileBytes` (100 MB) and
  `MaxChatAttachmentAccountBytes` (1 GB) in `internal/config/agent.go`, which
  are sizes the disk and the upload path are built for;
  `MaxScannedAttachmentBytes` is `computer.DefaultMaxAttachmentBytes` (25 MB);
  `messageBodyCharactersMost` (12,000), `recallTokens` (1,200),
  `ingestChunksPerRun` (2,000), `dreamScanConcurrency` (6) and
  `dreamRewriteConcurrency` (4) in the agent package; and
  `WorkerConcurrency` (2).

`requestTimeout` (60 seconds) cut a whole answer at a minute, and a long
document read ("Read 20 documents") failed with "the answer stopped: context
deadline exceeded" while the provider was still sending. It is replaced, for
every provider, by two clocks in `internal/llm/timing.go`:

- an answer has no total time limit; a streamed one is cut only when nothing
  has arrived for 3 minutes, and every byte, a heartbeat comment included,
  starts the 3 minutes again (the wait for the first byte counts too);
- a call that does not stream (a model list, a vector) has 15 minutes in all,
  so a connection that died without telling anyone still ends.

The HTTP clients carry no timeout of their own, since a client timeout bounds
the whole of a response. Both cuts report `context.DeadlineExceeded`, as the
old one did, with a sentence that says which clock ran out, and as their own
error type (`llm.IsProviderTimeout` recognizes it). The job policy counts that type as a failure,
with the normal backoff and dead letter, since a provider that hangs would
otherwise requeue its job for ever; a cancellation or a shutdown still
requeues without counting.

The removed YAML keys load with a warning through `deprecatedLimits`. The
removed GraphQL fields keep answering zero or empty for one release and are
ignored when sent, as the fields above are. The dashboard, the iPhone app, the
Android app and the command line show and send the four budget fields only.

## What stays, and why

None of these is a count of how often or how much work runs:

- the person's daily limit and the server's monthly one, the catch-all, and the only limits there are to set;
- the rhythms of background jobs, which are each job's schedule: a dream at
  most every 6 hours, ideas every 20 hours, reflection and evaluation weekly,
  journal periods daily;
- the waits that protect someone who is working: 30 quiet minutes before
  speaking first, the dream's quiet, the minute a goal waits after the person
  or another turn, and the 7 day repeat guard of alerts;
- technical bounds: payload, attachment and prompt sizes, timeouts,
  concurrency and the browser's contexts, the loop guard that stops a turn
  calling the same failing tool three times, batch sizes, the push relay's
  abuse limits.

## Budget coverage

Every kind of work that calls a model checks the daily and monthly limits first
(`RequireBudget` or `CheckBudget`), which is what makes them the catch-all.
Reading the code for this decision found two places that did not, one of which was left alone on purpose:

- embeddings are deliberately not checked, here or at ingest: they stay
  outside the daily allowance, as `ingest_embedding.go` explains (a first
  archive load is about 100 million tokens and costs cents). A check added
  at query time (`graph_embedding.go`, `embed.go`) was taken out again;
- the title and summary of a conversation (`describe.go`), which now waits and
  marks nothing, so the next look tries again;
- the recording sub-steps (`recording_accurate.go`, `recording_speaker_merge.go`)
  were found to check already, through `recordingAuthority` before each call,
  and are unchanged.

## Consequences

- A goal decides when it looks next, and a goal that asks to look every minute
  does, until the daily or monthly limit stops its turns. The budget gate waits
  the goal out and tells its owner once.
- A goal nobody can meet goes on until the person drops it or pauses it. The
  person is no longer asked to restart it.
- A failing goal retries after its own wait and no longer backs off. The cost
  of its failed turns counts against the same limits.
- An operator who relied on `maxRoundsPer*` or the shares to keep spending down
  sets `dailyCostPerAgent` and `monthlyCostPerServer` instead.
- A person who had set quiet hours or a daily most of alerts no longer has
  them. Their phone's Focus is the control.

## Addendum: settings nobody sets, and what the agent may undo

- A new server starts with no token cap (`agent.limits.dailyTokensPerAgent`
  is 0), as production does. The money limits are the budget.
- `agent.retention.runs` and `corrections` (30 and 90 days), and
  `agent.browser.idleTimeout` and `maxContexts` (5 minutes and 4) are
  constants in `internal/config/agent.go`, each with its reason. The keys
  still load with one warning, and the settings GraphQL answers them empty for
  one release and ignores them when sent.
- The agent may end or decline a finance share, and stop a touch-up, remove a
  correction or a pin, delete a narrative, change the journal's settings and
  erase the journal. The last two are destructive, so the person's card comes
  first. It still may not share finance or read what others share.
