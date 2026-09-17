# Memory that stays correct as it learns: reversible writes, honest cursors, checked evidence, provisional inference, and a way to measure it

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file. It builds on
`docs/planning/active/20260915-memory-that-learns-the-person.md`, which is
checked in and describes the memory graph and the dream, and on
`docs/planning/active/20260916-every-model-call-is-a-run.md`, which put every
model call through the conversation loop. Everything this plan needs from
either is repeated here.

## Purpose / Big Picture

The agent's memory is a graph of pages and facts that a nightly process, the
dream, writes and tidies. It reads the person's documents and conversations,
files what it learned, merges duplicates, rewrites page summaries, and draws
links between pages. It has grown fast this week, and an outside review of
the code (two written reviews, every concrete claim of which was checked
against the tree on 2026-09-16) found the same thing operating it found:
the graph learns quickly and forgets carefully in some places and
carelessly in others. A merged fact is deleted rather than kept. A
conversation with more than sixty unread messages has the oldest ones
marked as read without being read. A fact the prompt never carried is
marked as used. Every dream, however soon after the last, fades every link
by a fifth. A quote the model invented is stored as evidence at full
confidence. A link the dream guessed from walking the graph looks exactly
like a link the person stated. And nothing measures whether any of this
makes the next answer better.

After this plan, every write the dream makes can be undone and traced:
nothing the agent learned is deleted by a merge, a cursor never claims to
have read what it skipped, and what the prompt carried is what counts as
used. Links fade by elapsed time, not by how often the dream ran. A quote
that does not occur in the message it cites is marked as the model's
inference, not the person's word. A link the dream inferred is stored as
proposed, shown with that doubt, and promoted only when a document supports
it. Rehearsal says "supported", "gap" or "unknown" rather than "yes" when
it could not ask. And a fixed set of fifty questions, replayed against a
snapshot with each dream stage switched off in turn, says in numbers what
each stage is worth.

A person sees it working in three ways. On any page in the knowledge
explorer, a folded fact still exists, dormant, pointing at the fact that
absorbed it. In a conversation, the agent says "perhaps" about a proposed
link and cites a document for a supported one. And `teanode agent memory
evaluate` prints a table of hits and misses over the question set.

## Progress

- [x] (2026-09-17 01:40Z) Research: two outside reviews checked claim by
  claim against the code; findings and line references are in Context and
  Orientation.
- [x] (2026-09-17 12:45Z) Reviewed for simplicity at the maintainer's ask:
  the evidence check drops a bad quote rather than rewriting it; edges get
  one status column and no promotion stage, origin or expiry; the
  evaluation has no per-stage switches. Recorded in the Decision Log.
- [x] (2026-09-17 08:14Z) Milestone 1: reversible writes. Folded, struck and
  repeated facts go dormant with a pointer, never deleted; the docs' promise
  becomes true. The knowledge page lists what was folded under the facts,
  greyed, each saying which number absorbed it.
- [x] (2026-09-17 08:17Z) Milestone 2: honest cursors and bookkeeping.
  Remember reads the oldest unread segment and advances only through it;
  recall marks used only what it carried; pages in the index still get their
  facts expanded.
- [x] (2026-09-17 13:10Z) Milestone 3: decay by elapsed time, with a
  watermark on the agent, and reinforcement over the same real interval.
  The first pass writes the watermark and fades nothing; `hebbianDecay` is
  gone; a link's tense no longer counts having been read.
- [x] (2026-09-17 13:55Z) Milestone 4: evidence checked at the write
  boundary. A quote that is not in what the run showed the model is
  dropped and the fact marked inferred at half confidence; a citation of
  something that was never shown loses its evidence altogether; both runs
  count the outcomes on their own row. The knowledge page says "quote not
  found" beside such a fact.
- [ ] Milestone 5: provisional links. Edges gain a status and an origin;
  walks write proposed edges; the prompt says so; a dream stage looks for
  support and promotes.
- [x] (2026-09-17 14:40Z) Milestone 6: rehearsal with three outcomes.
  Every failure path is unknown, a supported answer has to name a fact it
  was shown, the dream row counts the third number through migration 0086,
  and the dashboard's dream line and the command line's log read "12
  rehearsed, 3 gaps, 4 unknown".
- [x] (2026-09-17 13:35Z) Milestones 3, 4 and 6 deployed to the
  maintainer's server: `agent`, `agent_dream` and `agent_edge` backed up to
  `/root/teanode-backups/before-memory-m346-20260917.sql.gz`, migrations
  0084 and 0086 applied on restart. The dream running since 13:32Z is the
  first under them; its rehearsal line and any "quote not found" fact are
  still to be seen, since the first dreams of the day were each cut short
  by a deploy restart and the facts filed so far cite documents without
  quoting them.
- [x] (2026-09-17 22:54Z) Milestone 7: the question set and `teanode agent
  memory evaluate`. A question is replayed through the turn's own recall
  over a new query, `RecallAgentMemory`, which answers with the pages and
  facts a turn would have been carried and asks no model and marks
  nothing as used; the command grades each question against what came
  back and prints hits, misses with the expectation that failed, totals
  per kind, and `--json`, exiting non-zero when anything missed. The
  shape of the file and a starter set of ten examples are in
  `docs/evaluation/`. No snapshot switches: a stage is measured by
  running the set either side of a night, as the Decision Log says.
- [ ] Milestone 8: docs and retrospective (completed 2026-09-17 22:50Z:
  `memory.md` describes elapsed-time decay and the watermark, the
  evidence check and the inferred marking, and rehearsal's three
  outcomes; remaining: the two edge statuses and the evaluation command
  once Milestones 5 and 7 land, `command-line.md`, and the
  retrospective's numbers).

- [x] (2026-09-17 12:55Z) Chrome check of Milestone 1's one visible change:
  on the dev server a near-duplicate fact written from the Facts card was
  folded at write time and appeared under "Folded in" as "#2 … folded into
  #1", muted, with #1 unchanged. Milestones 1 and 2 are deployed on the
  maintainer's server with the goals work (image built from the merged
  tree at 12:33Z).

## Surprises & Discoveries

- Observation (2026-09-17 22:54Z): `writeRecalled` could not be reused as
  it stood. It chose what fit the budget, wrote it into the overlay and
  moved `used_at` over it in one pass, and an evaluation wants the first
  of those three and none of the others: a run of the set that marked
  every carried fact as used would feed importance and decay, so the
  second run of the same set would be graded against a graph the first
  had rearranged. The choosing is now `chooseRecalled`, which returns the
  blocks and touches nothing, and `writeRecalled` is what writes them and
  marks them. `RecallForQuestion` calls the search and the choosing and
  stops there, which is what makes the evaluation free and repeatable.
- Observation (2026-09-17 22:54Z): a fact can be carried both ways. A
  page is expanded with its first five facts, and a sixth fact of the
  same page can still come back as a loose hit, so the recall result is
  gathered by path rather than listed block by block -- otherwise a
  question would see two entries for one page and a claim graded against
  whichever came first.
- Observation (2026-09-17 22:54Z): an abstain question that lists
  `expects` is a mistake in the file, not a graph that forgot something.
  The grading says so in the row rather than grading it, because a set
  that quietly passes the question it contradicts is worse than one that
  fails loudly.

- Observation (2026-09-17 15:00Z): a night's reading stopped at the
  first batch the model did not answer, and on the local model a stream
  runs past the request timeout about once an hour -- seven times in the
  last day. Four of the last five productive dreams read sixty to a
  hundred documents of the two hundred and forty they had time for, with
  a backlog of ninety-five thousand. The reading now goes on past one
  silence and stops at the third in a row (`dreamSilences` in
  `internal/agent/dream.go`); an unanswered batch is still not marked
  read.

- Observation: the reviewer's claim that reading an old relationship makes
  it sound current again is true of `decayOfEdge` in
  `internal/agent/decay.go:136-147`, but that function has no caller
  outside its test, and an edge's `used_at` is written only by the nightly
  co-activation pass. The bug is latent. It is fixed in Milestone 3 all the
  same, because the function is what the prompt will use once edges are
  rendered with their doubt.
  Evidence: `grep -rn decayOfEdge internal` finds `decay.go` and
  `decay_test.go` only; `internal/db/database_dream.go:412-416` is the one
  writer of edge `used_at`.
- Observation: changed numbers survive the twin merge more often than the
  reviewer thought, because `properNouns` in `internal/agent/graph.go:928-937`
  treats a digit-leading token as a name, so "40" and "60" make the two
  facts share no name and the merge is refused. Negations have no such
  protection: "she prefers tea" and "she no longer prefers tea" share every
  name and embed alike.
- Observation: the "never deletes" promise in `docs/subsystems/memory.md`
  (lines 143, 201, 232, 400) is broken in more places than the review
  found: `FoldIntoWhatThePageSays` (`internal/agent/remember.go:565-586`),
  two dream stages (`internal/agent/dream_stages.go:504` striking a
  vacuous fact and `:646` forgetting a fact said twice), the ingest
  (`internal/agent/ingest.go:762,816`, which re-derives a checkout's
  "about" facts and "Worked on" spans on every describe), the memory tool's
  `forget`, and the API's delete. The ingest's two are re-derived rows and
  are exempt below; the person's own `forget` stays a delete.
- Observation (2026-09-17 08:14Z): there is no `tx.PutAgentFact`, which
  Milestone 1 was written against; `AddAgentFact` and `UpdateAgentFact`
  are the whole of the fact writer. Worse, `UpdateAgentFact` already
  journals a newly set `SupersededBy` as `RevisionFactMerged`, so an
  agent-side revision entry would have been the second one for the same
  change. The fold and the striking became two transaction methods,
  `FoldAgentFact` and `StrikeAgentFact`, over one private writer that
  takes the kind and the reason from the caller — the columns that move
  are the same for a merge, a write-time fold and a striking, and only
  the caller knows which it did.
- Observation (2026-09-17 08:14Z): nothing rendered `supersededBy`
  anywhere. `AgentGraphPage` listed a page's facts with
  `includeDormant = false`, so a folded fact reached neither the
  dashboard nor the command line, and the explorer's dialog shows only
  live facts. The plan's "check that it shows folded into #N" was
  therefore a change and not a check: the page result gained a `folded`
  list, each entry carrying the *number* of the fact that absorbed it
  because an identifier is not something a page can cite.
- Observation (2026-09-17 08:17Z): the requeue Milestone 2 asks for
  cannot be an `Enqueue`. `EnqueueAgentJob` keeps one open job per agent,
  kind and subject, counting *running* as open, and the job doing the
  asking is that open job — so an Enqueue from inside `runRemember` hands
  back the row it is already running and writes nothing. The run returns
  a `Deferral` with `Until: now` instead, which is the worker's own way
  of saying "put me back in the queue": `FinishAgentJob` sets the row to
  queued, clears the claim, and the next tick claims it again. The test
  asserts the queued job and the log line says how many messages are
  still unread.
- Observation (2026-09-17 08:17Z): dropping the `inPrompt` skip outright
  would have left that method with no caller, and a page already in the
  index would have had its opening written into the overlay a second
  time. Indexed and expanded are now distinguished as the plan asks, and
  the distinction has a use: an indexed page gives up its facts and not
  its opening, which the index line already carries the gist of.
- Observation (2026-09-17 13:10Z): the watermark cannot be read from
  `run.Agent`. The dream holds a snapshot of the agent taken when the job
  was claimed, and the quiet half is the only writer of `DecayedAt`, so
  reading the snapshot and writing the row would let two nights that
  overlapped account for the same interval twice. The read and the write
  are one `UpdateAgent` inside the pass's own transaction instead, with
  the interval taken from the closure's argument, and `run.Agent` updated
  after it so the rest of the night sees what was written.
- Observation (2026-09-17 14:40Z): migration 0085 is skipped, not used.
  Milestone 5 is not built, and its `agent_edge.status` is what 0085 is
  for; the rehearsal column is 0086 as the plan numbered it. The runner
  applies what it finds in order and does not mind a hole, and the
  forward file says why the hole is there so nobody fills it by accident.
- Observation (2026-09-17 14:40Z): the facts a rehearsal judge is shown
  had no numbers to name. They are listed by their position in the five
  shown -- "1." to "5." -- rather than by their number on their page,
  because two pages can each have a fact #3 and the model is being asked
  to point at a line in front of it, not to cite the graph.
- Observation (2026-09-17 14:40Z): an object that parses and does not say
  `answered` stays a gap rather than becoming unknown. A parse failure is
  the model not answering; an object that answers something else is the
  model saying no in its own words, which is the plan's reading.
- Observation (2026-09-17 14:40Z): `TestDreamingRunsEveryPhase` had to be
  given a watermark. It asserts that the quiet half strengthens one link
  and fades another, and after Milestone 3 an agent's first pass does
  neither; the test now says the agent had a night six hours ago, which
  is what the assertion was always about.
- Observation (2026-09-17 13:55Z): the existing test
  `TestAPreferenceHasToComeFromTheirOwnWords` files a fact whose quote is
  cited to the wrong message, so the evidence check now also marks it
  inferred. That is the right answer -- the words are not in the message
  named -- and the test still passes, because what it asserts is that the
  fact stopped being a preference.
- Observation (2026-09-17 13:55Z): the "inferred" tag beside a fact's
  number was already on the knowledge page and in the agent page's
  learned-facts list, from the work that gave facts a confidence. Only
  the evidence line was missing, so the User experience section's first
  half was a check and its second half a change.
- Observation (2026-09-17 13:10Z): `dreamQuietHalf` had to take the
  moment rather than call `time.Now()`. What the milestone is about is an
  interval of thirty days, and no test can sit through one; the caller in
  `dream.go` passes `time.Now()`, so nothing changed but the seam.
- Observation: the extraction cursor loses history. `runRemember`
  (`internal/agent/remember.go:190-191`) keeps the last sixty unread
  messages, and `markRemembered` then advances `RememberedThrough` to the
  final message, so a conversation that had, say, two hundred unread
  messages has its first hundred and forty marked remembered unread.

## Decision Log

- Decision: a folded fact goes dormant with `SupersededBy` pointing at the
  fact that absorbed it, reusing the field the dream's consolidate merge
  already uses (`internal/agent/dream.go:913-915`), rather than a new
  `merged_into` column.
  Rationale: one mechanism for "this row stands behind that one", already
  excluded from the index and from search by `superseded_by IS NULL`, and
  already shown by the explorer. A second field would mean a second set of
  filters.
  Date/Author: 2026-09-17, the agent.
- Decision: the person's own `forget`, through the memory tool or the API,
  still deletes. The dream never does.
  Rationale: "forget this" from the person is a request to remove, and
  keeping it dormant would be keeping what they asked to lose. The promise
  in the docs is about what the agent does on its own.
  Date/Author: 2026-09-17, the agent.
- Decision: similarity stays a candidate generator and the name check stays,
  but a negation guard is added before any fold: two texts where exactly
  one contains a negation word (`not`, `no longer`, `never`, `n't`, `stopped`,
  `former`) are never folded, and both stay, the newer marked as
  superseding the older only when it is the newer statement of the same
  subject. Classification by a model (equivalent, extends, contradicts) is
  left to a later plan.
  Rationale: the reviewer is right that a threshold is not a justification,
  and wrong that a verifier per write is affordable at bootstrap scale
  (hundreds of facts an hour on the station model). A string guard catches
  the one class of loss that is certain, negation, at no model cost, and
  keeps both rows so a later classifier has something to classify.
  Date/Author: 2026-09-17, the agent.
- Decision: the striking of a vacuous fact keeps the row and files no
  pointer, and only the fold files one. The dashboard shows the folded
  rows and not the struck ones.
  Rationale: a struck line stands behind nothing, so "folded into #N" has
  nothing to say about it, and the nightly pass that retires facts nobody
  has wanted in half a year would put a page's whole history under its
  own facts. What the striking leaves is its history entry, under its own
  kind and with the words it used to say, which is what somebody
  disagreeing with it needs.
  Date/Author: 2026-09-17, the agent.
- Decision: decay is a function of elapsed time since the last decay pass,
  stored as a watermark on the agent, with a half-life of thirty days for
  an untouched edge, and reinforcement looks back over the same interval.
  Rationale: the current constant of 0.8 a pass was written for one pass a
  night; bootstrap runs a pass every few minutes, and after a day of that
  every untouched edge is at the floor. Time is what the constant meant.
  Date/Author: 2026-09-17, the agent.
- Decision: evidence is checked with strings, not a model. A quote must be
  found in the cited message after whitespace and case are normalized, and
  the cited message must be one of the conversation's; a fact that fails
  either is kept, with `Inferred: true` and `Confidence: 0.5`, and its
  evidence quote replaced by the closest sentence of the cited message when
  one is found by word overlap, else dropped.
  Rationale: fabricated quotes are the failure the reviewer named, and
  whether a string occurs is not a judgement call. Semantic support
  checking is reserved for promotions in Milestone 5.
  Date/Author: 2026-09-17, the agent.
- Decision (2026-09-17 13:55Z): a citation is checked against what the
  run put in front of the model, handed to `fileWhatWasLearned` as a map
  of id to the text shown, rather than against the document looked up
  through the transaction as the plan first said.
  Rationale: the model can only quote what it saw. A digest shows a
  document's citation line and its opening passage, so holding a quote
  against the whole document would pass words that were never in the
  prompt, and a coarse night, which shows a title and no body at all,
  would have every quote it offers pass against text it never read. The
  map also makes the conversation and the document paths one rule instead
  of two.
  Date/Author: 2026-09-17, the agent.
- Decision (2026-09-17 13:55Z): a citation the run did show but with no
  text to check against keeps its quote. Nothing was shown, so a check
  that cannot be made is not a check that failed. In practice this is
  only a document whose opening was empty.
  Rationale: marking a fact as the agent's guesswork is a claim about the
  model, and a claim nothing supports is the fault this milestone exists
  to fix.
  Date/Author: 2026-09-17, the agent.
- Decision (2026-09-17 13:55Z): the check drops a failed quote and does
  not look for the closest sentence of the message to put in its place,
  which the Decision Log's earlier entry allowed for.
  Rationale: the maintainer's simplification. A sentence chosen by word
  overlap is the program's guess at what the model meant, stored where a
  reader expects the person's words; the fact already says it was worked
  out, and a reader who wants the message can open it.
  Date/Author: 2026-09-17, the agent, simplified with the maintainer.
- Decision: edges gain one column, `status` (stated or proposed); a walk
  writes `proposed`; only `stated` edges are carried in the prompt index;
  `proposed` edges appear on a page as "perhaps" lines and are drawn
  dashed. No origin column, no promotion stage, no time-to-live.
  Rationale: the reviewer's separation of confidence from retrieval weight,
  done with the one field that changes what the agent may assert. An
  origin, a support stage and an expiry were in the first draft and came
  out when the maintainer asked whether the plan was overcomplicated: a
  dashed guess that nobody confirms is exactly what it is, and promotion
  from documents can wait until the evaluation says walks are worth it.
  Date/Author: 2026-09-17, the agent, simplified with the maintainer.
- Decision: the evaluation runs recall only, not the answer model, in its
  first form: for each question, did the memories the turn would carry
  include the facts the question is about. A second form that asks the
  model and grades the answer is written down but not built here. No
  per-stage switches: a stage's worth is measured by running the set
  before and after a night, or on a restored snapshot.
  Rationale: recall is deterministic and free, so it can run in CI and on
  every snapshot; grading answers costs model calls and a grader. Switches
  were a setting, a docs entry and a branch in every stage for an
  experiment that snapshots already allow.
  Date/Author: 2026-09-17, the agent, simplified with the maintainer.

## Outcomes & Retrospective

To be written at the end of each milestone and at completion.

Milestones 3, 4 and 6 (2026-09-17 14:55Z). The three are independent and
landed as three commits on one branch; what is written above under
Progress, Surprises and the Decision Log is the record of them. What is
worth saying beyond that:

- The fade was the cheapest of the three to build and is the one whose
  effect on a real graph will be largest. Bootstrapping was taking every
  untouched link to the floor within a day, which means the weights on
  the maintainer's server say very little at the moment; they will climb
  back only as the pages are used. The first night after the deploy fades
  nothing at all, by design, so the change shows from the second.
- The evidence check has one number nobody can predict from here: how
  many facts a night files whose quote is not in what the model read. The
  count is on each run's row now, so the first night after the deploy
  answers it. If it is large the constant to loosen is
  `evidenceInferredConfidence` and the rule to soften is the substring
  test, both in one place.
- Rehearsal's third outcome cost a column and a prompt change and made
  the phase honest, but it also raises the bar for "answered", so the
  gaps and unknowns a night reports will both go up before they go down.
  A night on a server whose embedding model is not configured now reports
  every question as unknown, which is the truth and used to read as a
  night that found nothing missing.
- What is not done here: Milestones 5, 7 and 8. Milestone 5's migration
  number, 0085, is left free for it.

## Context and Orientation

TeaNode is a mail server with a personal agent, in Go under `internal/`,
with a React dashboard under `web/` and PostgreSQL behind it. The memory
subsystem is documented in `docs/subsystems/memory.md`; what follows is
what this plan needs of it.

The graph. A *page* is a row of `agent_node` (`models.AgentNode`,
`internal/models/graph.go`), addressed by a path such as `people/alice-chen`
or `projects/portal` or `self` (the person). A *fact* is a row of
`agent_fact` (`models.AgentFact`, `internal/models/graph.go:240-300`): a
sentence on a page with a `Kind`, a `Confidence` (a float, 1 for anything
the extraction writes), `Inferred` (true when the agent worked it out
rather than read it), `Evidence` (a list of `models.Evidence{Kind, ID,
Quote, At}`, line 221: the message or document it came from and the line
it was read in), `SupersededBy` (the id of a newer statement of the same
thing, line 271) and `Dormant` (out of the index and still searchable,
line 273). A fact is *live* when neither is set (`IsLive`, line 614). A
*link* is a row of `agent_edge` (`models.AgentEdge`, line 312): `FromID`,
`ToID`, `Relation`, `Weight` (a float the dream tunes), `Evidence`, `Note`,
`HappenedAt`, `UsedAt`. It has no status: a link the person stated and one
the dream guessed are the same row. Every write to the graph is also a row
of `agent_revision` (a journal of who changed what; `models.RevisionFactGone`
is the entry a deletion leaves, with only the fact's number and text).

Writing. `fileWhatWasLearned` in `internal/agent/remember.go` (line 396)
is the extraction writer: it takes the model's answer about a batch of
conversation messages and files facts with `Confidence: 1` and the
evidence the model named (line 471-482), checking only that the quote is
not one of the prompt's own examples (line 418) and that the text is new
(line 467). `runRemember` (line 141) chooses which messages: everything
after the conversation's `RememberedThrough`, cut to the last sixty (line
190-191), then `markRemembered` (line 238) moves `RememberedThrough` to the
last message it was given, which is the last of the whole unread list, not
of the sixty. Before a fact is stored, `twinOf` (line 600-620) looks for a
near neighbour on the same page above `twinFloor = 0.92` cosine similarity
(`internal/agent/graph.go:63`) that `sharesAName` with it (line 880-901);
if one is found, `FoldIntoWhatThePageSays` (line 565-586) appends the new
fact's evidence to the old one and deletes the new row with
`tx.DeleteAgentFact`, which hard-deletes (`internal/db/database_graph.go:912-927`).
The same twin check runs at ask time in `NoteFact` (`graph.go:826-846`).

Reading. Once a turn, `recallForTurn` (`internal/agent/graph.go:353-381`)
searches the graph by words and by vector (`searchGraph`, line 471-486,
fused by reciprocal rank) and writes an overlay of pages and facts into
the prompt with `writeRecalled` (line 496-560). That function expands the
top pages' facts into blocks; it appends each fact to the block, marks it
in `shown` and `usedFacts` (line 521-533), and only then measures the
block against `recallTokens`, breaking out if it does not fit. Facts in
`usedFacts` get `TouchAgentFacts` at line 554, which bumps `used_at`, which
feeds importance and decay. Pages already carried by the compact index
that every prompt holds are skipped for expansion (`self.inPrompt`, line
509). There is no hop along edges and no date filter; time enters only as
a rank multiplier (`decayOfNode`, line 462).

Decay. `internal/agent/decay.go` has `decayOf(age, halfLife)` and
`decayOfNode`, used in ranking, and `decayOfEdge` (line 136-147), which
takes the latest of created, happened and used as the edge's age and says
whether to speak of it in the past tense; nothing calls it yet. The dream's
quiet half calls `tx.StrengthenAgentEdges(agentId, since, rise, decay)`
(`internal/db/database_dream.go:402-420`): one `UPDATE agent_edge SET
weight = GREATEST(0.05, weight * 0.8)` over every edge, then a rise for
edges whose both ends were used since `since`, which the caller sets to
six hours ago (`dream_stages.go:98`, `dreamApart` in `dream.go:40`). No
watermark of when it last ran exists; `agent.DreamedAt`
(`internal/models/agent.go:78`) is written when a dream finishes (`dream.go:285`)
and read only to keep dreams six hours apart when not bootstrapping
(`dream.go:147`).

The dream. `internal/agent/dream.go` runs the stages in order: digest
(read documents in batches through a run of the conversation loop with
read-only lookups), remember, consolidate (rewrite a page's summary from
its facts, `prompts/consolidate.txt`, whose lines 49-53 say to keep a
sentence of the old summary that no fact repeats unless a fact contradicts
it), organize, revise (`dreamRevise` in `dream_stages.go`, which at line
504 deletes a fact found vacuous), forget-said-twice (line 646, deletes),
associate (`dreamAssociate` and `askAboutAWalk`, line 137-260: walk the
graph from a page, ask the model whether the two ends are related, and if
so `PutAgentEdge` at weight 0.5 with evidence whose quote is the path
walked and whose id is empty, line 232-239), and rehearse (`dreamRehearse`,
line 263-330: generate questions the person might ask, and for each,
`canAnswerFromMemory` → `factsAnswer`, line 392-422, which returns true
when the budget is gone, the prompt cannot render, the model call fails,
or the answer does not parse). Each dream is a row of `agent_dream` with
counts per stage (`digested`, `filed`, `merged`, `revised`, `rewritten`,
`strengthened`, `rehearsed`, `gaps`, ...). `newDreamBudget` in `dream.go`
gives each dream a fresh allowance.

The daemon and the station. The dream's model calls go to the operator's
scan model, a local Qwen on a machine called station, free but slow, with
`limits.scanConcurrency` runs at a time; the person's own conversations
use a paid model. Every model call is a *run*, a conversation of kind
`run` visible in the dashboard's Agents page under Runs.

Terms. *Dormant*: kept, searchable, out of the prompt index. *Superseded*:
dormant because a newer row says the same thing. *Proposed*: a link the
dream inferred and nothing yet supports. *Watermark*: a stored time saying
when a pass last ran, so the next pass knows the interval. *Snapshot*: a
copy of one agent's graph tables at a moment, restored into a test
database for the evaluation.

## User experience

What the person sees of each milestone, in the knowledge explorer, the
conversation and the command line. Every dashboard change is checked in
Chrome at 1600 and 420 wide before it is deployed.

Folded and struck facts (Milestone 1). On a page in the explorer, a fact
that was folded into another no longer vanishes: it sits under the fact
that absorbed it, indented and muted, with the tag "folded into #12", and
a struck fact sits in the same place with the tag "struck". Both are
collapsed by default behind a line "3 folded" at the foot of the facts
card, so a page with many merges reads as its live facts; the line opens
them. Nothing in the conversation changes. The page's history shows
"folded #14 into #12" and "struck #9" as it shows every other change.

Inferred facts (Milestone 4). A fact whose quote did not occur in the
message it cites carries the tag "inferred" beside its number, and its
evidence line reads "from a conversation, quote not found" instead of a
quote. In the conversation, nothing changes; the agent's answers already
cite pages, not confidence.

Proposed links (Milestone 5). In the explorer's drawing, a link the dream
guessed is a dashed line, and the dialog that opens on it says "proposed
by the agent" under the relation. The page's Connections card lists it
with the word "proposed" in muted type. Confirming it is making the same
link from the Link dialog, which turns it solid; dropping it is the
existing unlink. In a conversation, a proposed link is spoken of as a
guess: "perhaps related to the Portal project".

Rehearsal (Milestone 6). The dream's row on the agents page's Runs tab,
and the dream log the operator reads, show three numbers where two were:
"12 rehearsed, 3 gaps, 4 unknown", so a night whose model was
unreachable no longer reads as a night with nothing missing.

Decay and evidence (Milestones 3 and 4). No new controls. What the person
notices is that a page they have not touched in a month reads the same
whether the dream ran once or a hundred times, and that fewer facts cite
words that were never said.

The evaluation (Milestone 7). A command, not a page:

    teanode agent memory evaluate docs/evaluation/memory-questions.json

prints one row a question, "direct 03 hit", "changed 12 miss: carried the
old address", and totals per kind at the foot. `--json` for scripts. A
page for it can come when the numbers are worth watching over time.

## Milestone 1: reversible writes

At the end of this milestone the dream deletes nothing: a fact folded into
its twin, struck as vacuous, or found said twice goes dormant with
`SupersededBy` set, its evidence intact, visible in the explorer under the
fact that absorbed it. The docs' promise is true.

In `internal/agent/remember.go`, `FoldIntoWhatThePageSays` stops calling
`DeleteAgentFact`. It appends the new evidence to the older fact as now,
then updates the newer row with `SupersededBy = older.ID` and `Dormant =
true` through `tx.PutAgentFact`, and journals it with a new revision kind
`models.RevisionFactFolded` carrying both ids. Before folding, the negation
guard from the Decision Log runs: a new function `negates(left, right
string) bool` in `internal/agent/graph.go` beside `sharesAName`, true when
exactly one of the two texts contains a negation token; when it is true the
new fact is stored as its own row, and `SupersededBy` on the *older* fact
is set to the new one only when `sharesAName` is true and the new fact's
`HappenedAt` (or filing time) is later, so the newer statement stands and
the older is kept behind it. The test `TestANegationIsNeverFolded` in
`remember_test.go` writes "prefers tea" then "no longer prefers tea" and
expects two rows, the older superseded.

In `internal/agent/dream_stages.go`, the delete at line 504 (a vacuous
fact in `dreamRevise`) becomes `Dormant = true` with revision kind
`RevisionFactStruck`, and the delete at line 646 (`dreamForgetSaidTwice`)
becomes `SupersededBy` pointing at the kept twin, the same as a fold. The
ingest's two deletes in `internal/agent/ingest.go:762,816` are left: they
replace rows the ingest itself derived from a checkout's profile and
commit spans, and re-derives on every describe; a comment says so.

In `internal/db/database_graph.go`, `DeleteAgentFact` keeps its behaviour
for the person's `forget`, and its revision entry grows to carry the whole
fact as JSON (`kind`, `evidence`, `confidence`, `happened_at`, `audiences`)
rather than number and text, so a deletion is at least reconstructible
from the journal. The explorer already shows dormant facts greyed under
their page; check that a superseded fact shows "folded into #N" by reading
`web/src/pages/knowledgeExplore.tsx` where `supersededBy` is rendered, and
add the line if it is not.

`docs/subsystems/memory.md` line 143 and 201 are made true by the code; the
paragraph at 201 gains one sentence saying the write-time fold does the
same as the dream's merge.

Proof: `go test ./internal/agent/ -run 'Fold|Negation|Struck|Twice'
-count=1` (needs the test database; see Concrete Steps). Then on the dev
server, note two facts that are twins through the memory tool and see both
rows in the explorer, one dormant with the pointer.

## Milestone 2: honest cursors and bookkeeping

At the end of this milestone a long backlog is read oldest first, sixty at
a time, across as many remember runs as it takes; and recall counts as used
only what it carried.

In `runRemember`, replace the tail cut at `remember.go:190-191` with a head
cut: `unread = unread[:rememberMessages]` when longer, and pass that slice
to `markRemembered` so `RememberedThrough` advances to the last message
*read*. When more remained, the run ends by enqueueing another remember
job for the same conversation (`Enqueue(models.AgentJobRemember, ...)` as
the caller does) rather than waiting for the next trigger, so a backlog
drains at sixty a run. Test: a conversation with 150 messages and
`RememberedThrough` empty; after one run the cursor is at message 60 and a
job is queued; after three runs it is at 150.

In `writeRecalled`, build each page's block into a local slice of facts
first, measure it, and only when it fits append to the overlay and then
mark the facts in `shown` and `usedFacts`. When a block does not fit,
`continue` to the next page rather than `break`, since a smaller page may
still fit, but stop when the remaining budget is under a floor
(`recallTokens/8`) to avoid scanning every page. Distinguish `indexed`
from `expanded`: a page the compact index already names may still have its
facts expanded when it is among the top hits, because the index line is a
description and not the facts; keep the skip only for pages whose facts
were already written in this overlay. Tests in `graph_test.go`: a page
whose block is over budget leaves its facts unmarked and untouched; a page
in the index gets its facts expanded when it is a hit.

Proof: the two tests, and `TouchAgentFacts` observed (in a test with a fake
transaction, or by reading `used_at` after a turn on the dev server) to
touch only facts present in the rendered overlay.

## Milestone 3: decay by elapsed time

At the end of this milestone an edge untouched for thirty days has half
its weight whether the dream ran once or a thousand times in between, and
reinforcement counts use over the real interval since the last pass.

Add `DecayedAt *time.Time` to `models.Agent` and a migration
`0084_agent_decayed_at.sql` adding the column. In the quiet half
(`dream_stages.go` around line 98), compute `elapsed = now -
agent.DecayedAt` (or zero on the first pass, which then only sets the
watermark), `factor = 0.5^(elapsed / edgeHalfLife)` with `edgeHalfLife =
30 * 24h`, clamp `factor` to `[0.05, 1]`, and call
`StrengthenAgentEdges(agentId, *agent.DecayedAt, hebbianRise, factor)`, so
the rise window is the same interval; then store `DecayedAt = now`. Remove
the constant `hebbianDecay`. `decayOfEdge` in `decay.go` stops taking
`UsedAt` into its age: the tense of a relationship depends on when it
happened or was created, and use affects ranking only; its test changes
accordingly. Bootstrap, which runs dreams back to back, now decays nothing
between them, which is the point.

Proof: `TestDecayIsByElapsedTime` in `dream_stages_test.go` (or a database
test in `internal/db`) runs the quiet half twice a minute apart and once a
month later and checks the weights: unchanged, unchanged, halved.

## Milestone 4: evidence checked at the write boundary

At the end of this milestone a fact whose quote is not in the message it
cites is filed as inferred at half confidence with no quote, and a fact
citing a message that does not exist has its evidence dropped.

In `fileWhatWasLearned`, before building the fact at line 471, look the
cited message up among the batch's messages (for a document evidence
kind, the document by id through the transaction). If the id names
nothing, file with `Evidence: nil`, `Inferred: true`, `Confidence: 0.5`.
If it exists, normalize both the quote and the message content
(lowercase, whitespace collapsed, quotes and dashes unified) and require
the quote to occur as a substring; when it does not, keep the evidence id
without the quote and mark `Inferred: true`, `Confidence: 0.5`. The
existing downgrade at line 429 (a preference cited to a message the person
did not write) stays. The run's note row counts the outcomes ("filed 7
facts, 2 without their quote, 1 without evidence") so the dream log shows
how often the model invents.

Proof: `TestAQuoteMustOccurInItsMessage` files a fact whose quote is a
paraphrase and expects no quote and `Inferred`;
`TestAFactCitingNothingHasNoEvidence` expects nil evidence and 0.5. On the
server, after a day, `select count(*) filter (where inferred) from
agent_fact where created_at > now() - interval '1 day'` says how much the
guard catches.

## Milestone 5: provisional links

At the end of this milestone a link the dream inferred is stored as
proposed, rendered as "perhaps", and kept out of the prompt index; a link
the person states is stated.

Migration `0085_agent_edge_status.sql`: `status text not null default
'stated'` on `agent_edge`. `models.AgentEdge` gains `Status
AgentEdgeStatus` (`EdgeStated`, `EdgeProposed`); `PutAgentEdge` stores it;
`askAboutAWalk` writes `Status: EdgeProposed` and its evidence becomes
`{Kind: EvidenceDream, Quote: path}` so no reader mistakes it for a
document. Every other writer, the memory tool's `link`, the ingest's
derived links, the dashboard's Link dialog, writes `EdgeStated`, which is
the default.

Rendering: wherever an edge becomes prompt text (the index line in
`carryIndex`, the page block in `writeRecalled`, `AgentEdge.Sentence`), a
proposed edge is omitted from the index and written on the page as
"perhaps related to X (the agent's guess)". The explorer draws a proposed
edge dashed (`web/src/pages/knowledgeExplore.tsx` and the card's
`graphExplorer.tsx`, the edge style) with the word "proposed" in the
dialog. A person confirms a proposed link by making the same link from
the Link dialog, which `PutAgentEdge` treats as an update to stated; a
person drops one with the existing unlink.

No promotion stage, no origin column, no time-to-live in this plan: a
proposed link that nobody confirms stays a dashed guess, which is what it
is. Promotion from documents is written down under Outcomes as the next
step once the evaluation in Milestone 7 says walks are worth keeping.

Proof: `go test ./internal/agent/ -run 'Proposed'`, and on the server,
`select status, count(*) from agent_edge group by 1` after a dream; a
proposed edge in the explorer dashed; a page's prompt block containing
"perhaps".

## Milestone 6: rehearsal with three outcomes

At the end of this milestone `factsAnswer` returns one of `answered`,
`gap`, `unknown`, and the dream row counts all three.

Change `factsAnswer` (`dream_stages.go:392-422`) to return a
`rehearsalOutcome` and make each failure path (`!budget.left()`, render
error, model error, JSON extraction or parse error) `rehearsalUnknown`;
`canAnswerFromMemory` returns `unknown` when there is no embedder. A
supported answer must name at least one fact number from the facts it was
shown, or it is `unknown` too. `dreamRehearse` counts them into the dream
row (`rehearsed`, `gaps`, and a new `unknown` column via migration
`0086_agent_dream_unknown.sql`) and writes a gap only for `gap`. The dream
log line in the dashboard shows "12 rehearsed, 3 gaps, 4 unknown".

Proof: a test that makes the model call fail expects `unknown` and no gap
written; the dashboard's dream row after a dream on the dev server shows
the three numbers.

## Milestone 7: the question set and the evaluation command

At the end of this milestone `teanode agent memory evaluate questions.json`
replays fifty questions through recall against the live graph and prints,
per question, whether the facts it needs were carried, and a total.

The set lives in `docs/evaluation/memory-questions.json` (the maintainer
writes the questions; the plan writes the shape): a list of `{id,
question, kind: direct|paraphrase|changed|multihop|abstain, expects:
[{path, words: [..]}], forbids: [{path, words}]}`. `expects` is satisfied
when the overlay recall would carry contains a fact on `path` containing
every word; `forbids` is what a changed fact must not carry (the obsolete
statement). Fifty questions, ten of each kind, with the changed ones built
from real corrections the maintainer made this week.

The command, in `internal/cmd/agent_graph.go` under `agent memory`: it
signs in as the person as every command does, calls a new GraphQL query
`RecallAgentMemory(question) { pages { path facts { number text } } }`
that runs `searchGraph` and `writeRecalled` without a turn and returns
what would have been carried, and grades locally. Output is a table with
kind, id, hit or miss, and which expectation failed, then totals per kind.
`--json` for machines.

Measuring a stage's worth is done by running the set before and after a
night, and by restoring a snapshot of the graph tables into the dev
database and running it there; no per-stage switches are added. The plan
records the first numbers in Artifacts.

Proof: `teanode agent memory evaluate docs/evaluation/memory-questions.json`
prints fifty rows and a total on the maintainer's server; a deliberately
wrong expectation shows as a miss.

## Milestone 8: documentation

`docs/subsystems/memory.md` gains: the fold's dormant row, the honest
cursor, elapsed-time decay and the watermark, checked evidence and the
inferred marking, the two edge statuses and what a proposed link looks
like, rehearsal's three outcomes, and the evaluation command and its
question file. `docs/reference/command-line.md` lists `agent memory
evaluate`. The retrospective compares the numbers before and after.

## Concrete Steps

All commands run from the repository root.

    go build ./... && go vet ./internal/agent/ ./internal/db/ ./internal/models/
    make lint-ci
    go test ./internal/agent/ -count=1        # needs the test database
    cd web && npx tsc --noEmit -p . && npx prettier --check src

The agent package's tests want PostgreSQL; `make test` starts a container,
or the existing `teanode-test-db` container can be pointed at with the
environment the Makefile sets (see `docs/reference/local-development.md`).
Migrations are added as numbered files under `internal/db/migrations/`
(`docs/coding/database-migrations.md`); the next free number is 0084; 0083 belongs to the goals plan. The
dev server for dashboard checks is `make build` then `./build/teanode-server
run` with `dev/.env` in the environment; the dashboard is
`http://127.0.0.1:10081`. Every dashboard change is looked at in Chrome
before it is deployed or committed. Deploy with `make docker
DOCKER_TAG=teanode:memory`, `docker save teanode:memory | ssh root@server
docker load`, `ssh root@server 'cd /opt/teanode && docker compose up -d
--force-recreate teanode'`; a deploy ends the running dream, so batch them.

## Validation and Acceptance

After Milestone 1, `select count(*) from agent_revision where kind =
'fact_gone' and actor = 'dream' and created_at > <deploy time>` stays at
zero over a night of dreaming, and folded facts appear dormant with a
pointer. After Milestone 2, no conversation has `RememberedThrough` past a
message that was never in a remember batch (checked by a test), and
`used_at` moves only on carried facts. After Milestone 3, two dreams a
minute apart leave weights unchanged. After Milestone 4, the dream log's
note rows count evidence rewritten and dropped. After Milestone 5, the
explorer shows proposed links dashed and a supported one carries a
document. After Milestone 6, the dream row shows unknowns. After Milestone
7, the evaluation prints fifty rows, and the same set run with `associate`
off and on gives two totals to compare.

## Idempotence and Recovery

Every milestone is additive: new columns with defaults, new revision
kinds, a new stage, a new command. The fold change can be reverted by
restoring the delete; nothing it wrote is lost since the rows stay. The
decay watermark's first pass decays nothing and only stores the time. If
the evidence check proves too strict (many facts marked inferred), the
confidence it assigns is one constant. Migrations are forward-only, as the
project's are; each is written to run once and to be harmless on a
database that already has the column.

## Artifacts and Notes

To be filled: the count of `fact_gone` revisions by actor before and after
Milestone 1; the first evaluation table; the associate-off and associate-on
totals.

## Sources

What this plan draws on, beyond the code. The two reviews were written by
an outside reader of commit 0d325449 on 2026-09-16 and handed to the
maintainer; every claim they made about the code was checked against the
tree (the results are in Context and Orientation and in the Surprises).
The papers and articles below are the ones those reviews cited, with what
each was taken for; the addresses are as the reviews gave them.

- Letta, "Sleep-time compute": moving memory work into background agents
  that run between conversations, which is the shape the dream already
  has. https://www.letta.com/blog/sleep-time-compute/
- Letta, "Towards agents that learn" (June 2026): memory models trained to
  write memories that help later tasks; taken as a direction, not a
  dependency, and as the reason to log what a retrieved memory did for an
  answer (Milestone 7). https://www.letta.com/blog/towards-agents-that-learn/
- Zep, Graphiti: temporal validity on facts and relationships, `valid_from`
  and `valid_to` beside the time a thing was learned, and replacement links
  between statements; the pattern behind supersession and, later, temporal
  windows. https://github.com/getzep/graphiti
- Hindsight, retrieval architecture: candidates from meaning, words, graph
  neighbours and time, then reranking; the reference for the retrieval work
  this plan defers to a later one.
  https://hindsight.vectorize.io/developer/retrieval
- Hindsight Memory-PRM (August 2026): citations and removal-and-reanswer
  tests to measure what a memory was worth; the audit trail and the
  offline removal test in Milestone 7 come from it, the trained controller
  does not. https://arxiv.org/html/2608.29605v1
- Mem0 (Chhikara et al., 2025), "Building production-ready AI agents with
  scalable long-term memory": extraction, consolidation and a graph
  variant; the simpler baseline the evaluation should beat.
  https://arxiv.org/abs/2504.19413
- Mastra, observational memory: a background observer and reflector
  keeping a compressed log; a comparison for conversation continuity and
  context cost. https://mastra.ai/docs/memory/observational-memory
- LongMemEval (Wu et al., 2024): a benchmark of extraction, multi-session
  reasoning, temporal reasoning, knowledge updates and abstention; the
  external half of the question set in Milestone 7.
  https://arxiv.org/abs/2410.10813
- SimpleMem (January 2026): compact, self-contained records that resolve
  names and dates while keeping source references; the shape a filed fact
  already has, and the reason the evidence check keeps the quote.
  https://arxiv.org/html/2601.02553v3
- LycheeMemory V2 (August 2026): consolidating coherent conversation
  segments rather than every turn, at lower construction cost; behind the
  honest cursor in Milestone 2 reading one segment at a time.
  https://arxiv.org/abs/2608.12990
- "Sleep-time Compute" (April 2025): preparing for related future queries
  pays when the queries are predictable; the reason association should
  favour active projects and recurring questions, and the caution about
  arbitrary walks. https://arxiv.org/abs/2504.13171
- Memora (April 2026): tests of evolving preferences and repeated updates
  that penalise reliance on obsolete memories; the "changed" kind of
  question in Milestone 7. https://arxiv.org/html/2604.20006v1
- "Total Recall at What Cost?" (August 2026): the financial break-even of
  memory systems depends on the whole pipeline; the reason Milestone 7
  counts ingestion, dreaming and answering together.
  https://arxiv.org/html/2608.11879v1

Internal: `docs/subsystems/memory.md`, `docs/subsystems/the-ask-loop.md`,
and the two plans named at the top.

## Interfaces and Dependencies

In `internal/models/graph.go`: `RevisionFactFolded`, `RevisionFactStruck`;
`AgentEdgeStatus` with `EdgeStated`, `EdgeProposed`; `AgentEdge.Status`;
`EvidenceDream`. In `internal/models/agent.go`: `Agent.DecayedAt`. In
`internal/agent/graph.go`: `func negates(left, right string) bool`; the
reordered `writeRecalled`. In `internal/agent/remember.go`: the head cut
and requeue in `runRemember`; the evidence check in `fileWhatWasLearned`. In
`internal/agent/dream_stages.go`: `edgeHalfLife`, the watermark decay,
`type rehearsalOutcome` with `rehearsalAnswered`,
`rehearsalGap`, `rehearsalUnknown`. In `internal/db`: migrations 0084
(`agent.decayed_at`), 0085 (`agent_edge.status`), 0086
(`agent_dream.unknown`); `StrengthenAgentEdges` taking a factor. In
`internal/api/v1api/apigraph`: `RecallAgentMemory` query. In
`internal/cmd`: `agent memory evaluate`. No new settings, no new
libraries.
