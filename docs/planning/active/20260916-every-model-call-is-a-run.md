# Every model call is a run: the dream, the ingest and every one-shot call go through the conversation loop

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file. It builds on
`docs/planning/active/20260915-memory-that-learns-the-person.md`, which is
checked in; that plan describes the memory graph and the dream this one
re-plumbs.

## Purpose / Big Picture

The person can open anything their agent did. A sorted message, a drafted
answer, a research run: each is a *run*, a conversation of kind `run` in the
dashboard's activity table and in `teanode agent run list`, and opening one
shows what was asked, what tools were called, what came back and what it
cost. The dream is not. It reads two thousand documents an hour, files
hundreds of facts, rewrites pages, draws links, and the person sees only a
line of counts in the dream log. The same is true of the ingest describing a
checkout, of the run that files what a conversation taught, of the thread
summary, of the title a conversation is given, and of the compaction note.
Those are calls made straight to the model provider by code, recorded, when
they are recorded at all, by a hand-written transcript writer that mimics
what the loop writes.

After this plan there is one way to ask a model anything: the conversation
loop in `internal/agent/ask.go`, the one a person's own turn goes through.
Every headless piece of work, one call or twenty, with tools or without, is a
turn of that loop in a run conversation of its own. The dream becomes a job
whose many calls are many runs tagged with the dream, each openable from the
dream log; the reading turns may look a page up with the memory tool before
filing; and the person opens `teanode agent run show <id>` or presses Open
in the dashboard on any of them. Model choice per kind of work is kept: the
dream and the ingest still run on the operator's `scan` model, which on the
maintainer's server is a local one that costs nothing.

To see it working after the plan is done: switch bootstrapping on, wait for a
dream, open the agent page, and press the dream's Open button; the activity
table shows that dream's runs, each titled by what it did ("Read 20
documents", "Wrote up August 2026", "Looked for a link between ...") and each
opens as a transcript with a memory `get` or `search` call in it where the
model chose to look before filing.

## Progress

- [x] Audit of every model call that bypasses the loop (see Context).
- [x] Milestone 1: the loop takes a kind of work and a one-call mode.
- [x] Milestone 2: the dream's eight call sites are runs; the dream record
      carries its job so its runs can be listed; dashboard and CLI open them.
- [x] Milestone 3: the ingest's checkout description, the remember run, the
      thread summary, the sorting and drafting fallbacks, the conversation
      title and the compaction note are runs; the hand-written transcript
      writer is gone (code done; twelve tests being rewritten for streamed
      answers and an operations factory).
- [x] Milestone 4: docs written (agents.md, memory.md, the-ask-loop.md,
      command-line.md, configuration.md); deployed on root@server four
      times over the afternoon of 2026-09-16 as each discovery below came
      up; a dream's runs open from the dream log and from the activity
      table, which pages on the server.

## Surprises & Discoveries

- The loop always chose the `ask` model (`AskRun.chooseModel` in
  `internal/agent/ask.go`), so a dream through the loop would have run on the
  paid default model. A kind of work on the settings fixes that.
- The loop's turns are serialized per conversation, not per agent
  (`AskRun.loop` waits on `previous.done`), so concurrent digest batches in
  conversations of their own do not queue behind each other or behind the
  person's chat.
- The first deployed dream hung: every turn of the loop recalls pages and
  passages by the words of its message, and a job's message is a nine
  thousand token prompt. "Any word matches" over a corpus of a third of a
  million documents ranked the whole table, eleven queries at once, for
  seven minutes, with the model idle. Headless turns skip the recall now
  (their prompts carry what they need), and a search is cut to its first
  sixty-four words.
- llama.cpp with the Qwen chat template answers 500 to a second system
  message ("System message must be at the beginning"), and the loop sends
  what is true now as a second one after the history so the first can be
  cached. The OpenAI provider now folds the system messages into one when a
  model refuses, and remembers to for that model.
- A prompt that quotes a file read from disk can carry bytes that are not
  UTF-8; the loop stores every message, and PostgreSQL refused the row. The
  message writer replaces invalid bytes with the replacement character.
- A dream whose reading failed had "digested 0", which the bootstrap rule
  read as "caught up" and switched bootstrapping off. The rule now needs
  the dream to have ended without an error.
- The loop's prompt for a reading batch is about 18,000 tokens: the persona,
  the tool catalog for memory and knowledge, and the batch. With four slots
  on a 64k context llama-server gives each 16k and refused; two slots of
  32k fit, and two slots read as fast as four did (the model is decode
  bound).
- The station model, given three rounds and the memory tool, spent all
  three looking pages up and never answered; the loop now asks its last
  round without tools, so a turn always ends in words.
- A failed turn left a transcript holding only its prompt, with the reason
  in the server log; the loop now writes "failed: ..." as a note row.
- The activity table asked for a thousand runs and the API capped that at
  fifty, silently. Runs are paged on the server now, with a total, kinds and
  a title query, and the table has a remote mode for lists too long to hold.
- Half the station model's reading runs ended in a tool call written out
  as words: llama.cpp parses the Qwen template's XML call only when it is
  alone and well formed, and two calls in one message, or a call after a
  sentence, came back as text. The provider reads textual calls itself now
  (`internal/llm/textual_tools.go`, XML and JSON forms), and the loop asks
  again when an answer still looks like a call it could not read. An hour
  went to a phantom on the way: a psql one-liner over ssh with `E'\\s+'`
  stripped every letter s from the transcripts it printed, and the data
  was fine.
- With tools in hand the station model reads thoroughly and then, a third
  of the time, ends in words rather than the object, or runs out of rounds
  still looking. Two answers: only the phases that decide where things go
  (reading, filing orphans) get tools now, the rest are one-shot again; and
  a reading given in words is handed to one more tool-less call that only
  writes the object. A batch whose lookups overflow the station's 32k
  context is marked read with its run titled "too long for the model,
  skipped" instead of stopping the whole reading, and llama.cpp's wording
  for that error is recognised now, so the loop's own compaction fires too.
- The loop streams (`ChatStream`) and only falls back to a plain call when
  opening the stream fails. The dream's test fakes answer plain JSON to a
  streaming request; they have to answer server-sent events, as the sorting
  tests in `internal/agent/triage_tools_test.go` already do.

## Decision Log

- 2026-09-16: one run per model call, not one per dream. A dream makes a
  hundred and fifty calls; a single conversation would carry every earlier
  batch into every later prompt, or be compacted into uselessness. A run per
  call is what triage already does, and a dream's runs are found by the job
  that made them. The dream record gets the job's identifier for that.
- 2026-09-16: the dream's turns are read-only on the loop, with the memory
  and knowledge tools offered for looking things up, and the phase's answer
  stays the JSON object the code files. Filing in code is what keeps evidence
  on every fact (the document and the words it came from), keeps merges and
  moves as proposals, and keeps the deterministic checks. Writing through the
  tools can come later once the transcripts show the model using the reads
  well.
- 2026-09-16: no fallback to a direct call anywhere. Sorting and drafting
  had one for when the loop is off; with every call a run, "the loop is off"
  means no model work at all, which is what the switch should mean. The
  headless path is gated by the feature that owns it (triage, dreaming, ...),
  not by the `ask` feature that gates the person's own chat.
- 2026-09-16: a one-shot call is a run with no tools and one round. The
  transcript then holds the user prompt, the assistant answer and its usage,
  exactly as the hand-written writer produced, so nothing the dashboard shows
  changes shape.

## Outcomes & Retrospective

### 2026-09-16, the first afternoon

Every model call in the agent is a run of the loop: the sixteen direct
calls in the audit are gone, `grep -rn "\.Chat(" internal` outside tests and
`internal/llm` finds only the loop's own call, and `recordRun`, `recordCall`
and `callRecord` are deleted. A dream's calls are runs tagged with its job;
the dream log's Runs button narrows the activity table to them; `teanode
agent dream runs <id>` lists them and `teanode agent run show` opens one.
The transcripts show the station model calling the memory tool before
answering, which is what the tools were offered for.

What the first deployed dream taught, in order: the loop's by-words recall
ranked the whole corpus for a job's prompt (headless turns skip it now, and
a search is cut to sixty-four words); llama.cpp refuses a second system
message (the provider folds them); a prompt quoting a binary file broke the
message row (invalid bytes are replaced); a failed reading switched
bootstrapping off (it needs a clean end now); four station slots gave 16k
of context and the loop's prompt is 18k (two slots of 32k); three rounds of
tool calls ended without an answer (the last round is asked without tools);
a failed turn left a bare prompt (the failure is a note row now). None of
these were visible before the calls were runs, which is the point of them
being runs.

Later the same afternoon: the operator got `/agent`, a row of the rail with
four tabs (agents, use and cost, every run, jobs given up on), and the
`agent:act` permission behind the runs tab -- every person's runs and
conversations, openable in the drawer and spoken into as that person,
logged each time; the person's own `/settings/agent` got six tabs; the
save toasts say what the server reports as pending a restart rather than
claiming a restart for everything.

Still open: the compact and describe runs fall back to the fast paid model
when no `compact` model is set; the activity table shows hundreds of
"Named a conversation" and reading runs a night, which the kind filter
narrows but a person may want hidden by default; and the loop's persona and
tool catalog add several thousand tokens to every dream call, which on the
station box is about five seconds of prefill each.

## Context and Orientation

The *loop* is `Agent.Ask` in `internal/agent/ask.go`: given a conversation
and a message, it builds the prompt (`internal/agent/context.go`), calls the
model, runs the tool calls the model asks for, and repeats until the model
answers in words or the rounds run out. It writes every message to the
conversation as it goes (`AppendAgentMessage`) with usage on the assistant
rows, and its events stream to whoever watches. `AskSettings` carries the
knobs: `Allow` (which tools by name), `Headless` (nobody watching), `ReadOnly`
(no tool that changes anything), `MaxRounds`, `UsageKind` (the name on the
usage rows), `Short` (short conduct rules). `think` in
`internal/agent/thinking.go` is the small wrapper headless jobs use: it makes
the run conversation, runs one turn, and hands back the last thing said.

A *run conversation* is a row in `agent_conversation` with `kind = 'run'`,
`job_id`, `job_kind` and `subject_id` saying which job made it. The dashboard
lists them in the activity table on the agent page (`ActivityCard` in
`web/src/pages/agent.tsx`, query `ListAgentRuns`) and opens one in the
drawer; the CLI has `teanode agent run list` and `teanode agent run show`.

The *dream* is `runDream` in `internal/agent/dream.go`: phases in order,
revise, digest, timeline, consolidate, organize, split, the quiet half,
associate, embed, rehearse; its record is a row in `agent_dream`, shown in
the dream log on the agent page and in `teanode agent dream log`.

The audit of direct model calls (`grep -rn "\.Chat(" internal --include=*.go`
without tests and without `internal/llm`), each with what it does and what it
becomes:

    internal/agent/dream.go:513        digestBatch      reads 20 documents, files facts   -> run "Read N documents"
    internal/agent/dream.go:658        writeMonth       writes a month page               -> run "Wrote up <month>"
    internal/agent/dream.go:794        consolidatePage  rewrites a page's opening         -> run "Rewrote <path>"
    internal/agent/dream.go:958        dreamOrganize    offers homes for orphans          -> run "Filed the orphans"
    internal/agent/dream.go:1133       splitPage        divides a crowded page            -> run "Divided <path>"
    internal/agent/dream_stages.go:220 dreamAssociate   judges a walk between two pages   -> run "Looked for a link between A and B"
    internal/agent/dream_stages.go:325 dreamRehearse    asks itself questions             -> run "Rehearsed"
    internal/agent/dream_stages.go:445 factsAnswer      judges whether facts answer       -> run "Judged: <question>"
    internal/agent/ingest.go:1373      describeCheckout describes a checkout              -> run "Described <path>" (job ingest)
    internal/agent/remember.go:326     askWhatWasLearned files what a conversation taught -> run (job remember), already recorded by hand
    internal/agent/summarize.go:225    runSummarize     summarizes a thread               -> run (job summarize), already recorded by hand
    internal/agent/triage.go:247       runTriage        the fallback when the loop is off -> removed; the loop is the only path
    internal/agent/reply.go:453        runReply         the fallback when the loop is off -> removed
    internal/agent/draft.go:212        Draft            a draft asked for from the drawer -> run (kind draft)
    internal/agent/describe.go:156     describe         titles a conversation             -> run (kind describe)
    internal/agent/compact.go:191      compact          the compaction note               -> run (kind compact)

`internal/agent/ask.go:862` is the loop's own call and stays. Embedding calls
(`Embed`) are not chat and are outside this plan.

## Plan of Work

### Milestone 1: the loop takes a kind of work and a one-call mode

`AskSettings` gains `Work config.AgentWork`. When set, `chooseModel` returns
`registry.ForWork(settings.Work)` and the model name on usage rows comes from
`Models.ForWork(settings.Work)`; when empty, the person's choice or the `ask`
model as today. `think` gains the same parameter and stops requiring the
`ask` feature: it requires only that there is a way to act as the person
(`self.operations`). A run with `Allow` set to an empty map is offered no
tools; with `MaxRounds: 1` it makes exactly one call. Together those are the
one-shot mode, and `think` is the only entry point every headless call uses.

Acceptance: `go test ./internal/agent/ -run TestAsk` passes; a new test
drives `think` with an empty allow set and one round against a fake model
and finds a run conversation holding exactly one user row and one assistant
row with usage, on the model configured for the given work.

### Milestone 2: the dream's calls are runs

Each of the eight dream call sites builds its prompt as today and calls
`think(ctx, run, title, prompt, dreamTools, rounds, models.AgentJobDream,
config.AgentWorkScan)`; `dreamTools` is `memory` and `knowledge`, offered
read-only; `rounds` is `limits.maxRoundsPerDream`, default 3, a new limit
next to `maxRoundsPerResearch` in `internal/config/agent.go`, documented in
`docs/configuration.md`. The phase reads `thought.Text` with `llm.Extract` as
it read the response before, and retitles the run with what it did. The
prompts say, in one sentence, that the memory tool may be used to look a page
up before filing, and that the answer is the object at the end.

Migration 0079 adds `job_id` to `agent_dream` (replacing the transcript
column drafted earlier in this working tree); `runDream` stores
`run.Job.ID`. `ListAgentRuns` takes an optional `jobId`; the dream log row
gets an Open button that shows the activity table filtered to the dream's
runs, and `teanode agent dream runs <id>` lists them.

Acceptance: `TestDreamingRunsEveryPhase` passes with its fake model answering
server-sent events; after it, `ListAgentRuns` for the dream's job returns one
run per phase call with the titles above. On root@server, a bootstrapped
dream reads at a pace within a third of before (the loop adds the persona
and the tool catalog to every prompt), and its runs open in the drawer.

### Milestone 3: everything else

The ingest's checkout description, the remember run, the thread summary, the
draft, the conversation title and the compaction note become `think` calls
with no tools and one round, on their own kinds of work; the sorting and
drafting runs lose their direct-call fallbacks. `recordCall`, `recordRun`,
`callRecord` and `appendCall` in `internal/agent/triage.go` and the
`transcript.go` drafted in this tree are deleted. The tests that scripted the
fallback single call are rewritten to script the loop's rounds.

Acceptance: `grep -rn "\.Chat(" internal --include=*.go` outside tests and
`internal/llm` lists only `internal/agent/ask.go`; `make lint-ci` and the
package tests pass.

### Milestone 4: docs, deploy, watch

`docs/subsystems/agents.md` ("Runs") says every model call is a run and lists
the kinds; `docs/subsystems/memory.md` says the dream's calls are runs and
how to open them; `docs/subsystems/the-ask-loop.md` notes the kind of work
and the one-call mode; `docs/reference/command-line.md` gains `dream runs`.
Deploy on root@server, bootstrap on, and watch one dream: its runs, the
memory reads in them, the pace, the facts.

## Concrete Steps

Build, vet and lint from the repository root with `go build -mod=vendor
./...`, `go vet -mod=vendor ./internal/...` and `make lint-ci`. Database
tests need PostgreSQL: start `pgvector/pgvector:pg17` in Docker, export
`TEANODE_TEST_DATABASE_HOST` to its address, and run `go test -mod=vendor
-count=1 ./internal/agent/ ./internal/db/`. Deploy with `make docker
DOCKER_TAG=teanode:memory`, `docker save teanode:memory | ssh root@server
docker load`, and `docker compose up -d --force-recreate teanode` in
`/opt/teanode` on the server; the migration applies at start.

## Validation and Acceptance

After deploy: `teanode agent dream bootstrap on`; after a dream,
`teanode agent dream log` shows it, `teanode agent dream runs <id>` lists its
runs, `teanode agent run show <run id>` prints a transcript with the digest
prompt, any memory calls, and the JSON answer. On the agent page the dream
row's Open button shows the same runs, each opening in the drawer.
