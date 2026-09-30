# Memory evaluation: scenarios fed through real ingestion and dreams, graded at checkpoints

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The memory is measured today by two question sets asked of the graph as it happens to be: `agent memory evaluate` (did recall carry the facts, free and exact) and `agent memory answers` (was the answer right, graded by a model). Neither can say how the memory behaves as knowledge changes, because nobody controls what went in or when. A decision that was reversed, a proposal that was rejected, a procedure that stopped working under a new version: whether the graph keeps these straight is only visible by accident.

After this plan, a scenario is a file of ordered, dated events with the answers that are right after each of them. A harness feeds the events through the same code that files a records source's documents, runs real dreams between them, and at each checkpoint asks the scenario's questions: through recall (free), and answered and graded from memory, from the sources and from both (priced). It then looks through every layer, facts, overview sections, themes, reflections and lessons, for the words of what is no longer true, so a stale answer can be traced to the layer it entered or survived in. Every run happens in a database of its own, so nothing it does touches a person's agent, and nothing one run learns reaches another.

The same harness carries the experiments that hang off the evaluation program: late corrections and knowledge time (#268), maintained answers to a standing question (#267), execution playbooks (#269), and public benchmarks (#270, starting with LongMemEval-S).

## Progress

- [x] (2026-09-30) Planned retrieval can be replayed (#264, PR #271): an evaluation can say whether it measured basic recall or the plan a live turn would follow.
- [x] (2026-09-30) Milestone 1, harness: `teanode-server evaluate scenario`, the changing-project scenario, and a test against the fake model server.
- [ ] Milestone 1, first report: runs of the changing-project scenario with a working embedding model (the first two ran without one; see Surprises).
- [ ] Milestone 2: knowledge-correctness and evidence-preservation scenarios (#265), with answers traced to the documents that support them.
- [ ] Milestone 3: late corrections and knowledge time (#268), existing behavior first.
- [ ] Milestone 4: a standing question answered by recall, by a survey each time, and (only if the first two show a gap worth closing) by a maintained answer (#267).
- [ ] Milestone 5: execution lessons over repeated update cycles (#269), existing lessons first.
- [ ] Milestone 6: LongMemEval-S, a pilot of 50 questions, then a cost estimate for the full set before running it (#270).

## Surprises & Discoveries

- Observation: a chat thread the person did not take part in is never read into facts; it is only searched. The first run's threads were between two other roles, and nothing said in them (a rejected proposal, a failed procedure, its replacement, an open report) reached a fact.
  Evidence: six documents gave three facts, all from the two notes; the digest's rule (`ListAgentDocumentsToDigest`) reads a chat unit only when the person is a participant and it has three posts or more. The scenario now has the person in its threads, as on a project of their own.

- Observation: after "Quillmoss moves from Burrowdb 4.2 to Burrowdb 5.1", the fact "Job state is stored in Burrowdb, pinned to Burrowdb 4.2" stayed current rather than superseded. Answers were right only because the answering model reconciled the two facts it was shown.
  Evidence: first run, `agent_fact` after dream-3. The second run did not repeat it: the old fact no longer stood, and the migration was filed once current and once superseded. Not a reproducible defect on two runs; watched in later runs.

- Observation: the embedding key the first runs were given was refused, so they recalled by words alone and their meaning search found nothing.
  Evidence: `cannot embed: llm: the provider answered 401` throughout the log. Those runs are a words-only baseline, not the system as deployed.

- Observation: with the person in the threads, answers from memory matched the sources on every question but the upgrade procedure right after it changed (partial) and whether a command's exit code proved the upgrade (partial).
  Evidence: second run, 1.83 dollars, words only; checkpoint 5, after two dreams with nothing new, answered all four from memory correctly.

- Observation: a LongMemEval-S instance (51 sessions) was read whole by one dream for about 0.17 dollars on the small model, and gave 12 facts; memory missed the answer that the sources found.
  Evidence: one probe instance, without embeddings.

## Decision Log

- Decision: the harness runs in its own database, created for the run and kept afterwards for inspection, and never against a running server.
  Rationale: evaluation must not change a person's graph (recall marks pages as used, answers file memories) and one arm of a comparison must not learn from another. A database per run is also what makes a run repeatable.
  Date/Author: 2026-09-30, agent.

- Decision: events enter as records, the same JSON lines a `records` source reads, through the daemon's own reader (`computer.RunScan`) and the server's own filing (`fileComputerPage`), with no daemon or websocket in between.
  Rationale: this is the path a real source takes, so what is measured is what runs; only the transport is skipped. Records carry their own time (`at`), which becomes the document's `happenedAt`, the time the dream reads.
  Date/Author: 2026-09-30, agent.

- Decision: the dream runs on the wall clock; the fixture's time lives only in the documents' dates.
  Rationale: the dream reads `time.Now()` in about a hundred places and has no clock to set. Ordering, which is what knowledge time needs, is kept by feeding events in order and grading at checkpoints. Where a result depends on the wall clock (decay, "recent"), the report says so.
  Date/Author: 2026-09-30, agent.

- Decision: model-graded answers are reported beside deterministic checks, never instead of them.
  Rationale: recall claims and the stale-word search are free and exact, and can run after every change; a graded answer costs calls and varies between runs, so a small difference in it is not a finding until repeated.
  Date/Author: 2026-09-30, agent.

## Outcomes & Retrospective

(Filled in as milestones finish.)

## Context and Orientation

- Records: `internal/computer/scan_records.go` reads JSON lines (`id, kind, at, author, title, text, channel, thread`) into `computer.ScanEntry`; chat-kind lines are grouped into threads and windows by `chatUnitsOf`. `internal/agent/ingest_page.go` `fileComputerPage` files a page of entries as documents and chunks. Embedding happens later, in `embedChunks`, which the dream also runs.
- Dreams: `internal/agent/dream.go` `runDream` is the handler for `models.AgentJobDream`; a run reaches it through `Agent.Enqueue`, `TickAt` and `Wait`. It needs a registry of models (`llm.Open`), the `dreaming` feature and an operations factory (`SetOperationsFactory`), as `dream_test.go` sets up.
- Grading: `EvaluateAnswer` in `internal/agent/evaluate_answer.go` answers from `memory`, `sources` or `both` (and `@planned` with a plan) and grades against an expected and an outdated answer. `JudgeRetrievalPlan` in `effort.go` gives the plan a live turn would follow. Recall for a question without touching usage is `RecallForQuestionPlanned`.
- Models come from a file holding a configuration's agent section on its own, opened by `llm.Open` as the server opens it.

## Plan of Work

Milestone 1 adds `internal/agent/scenario.go`: `RunScenario(ctx, *ScenarioSettings) (*ScenarioReport, error)`. The scenario file is JSON: a name, and a list of steps, each one of `records` (lines to file, in the records shape), `dream` (run one dream, and how many times, to see drift when nothing new came in) or `checkpoint` (questions). A question is the evaluation question set's shape (`id`, `question`, `kind`, `expects`, `forbids`, `expectedAnswer`, `outdatedAnswer`) plus `outdatedClaims`, statements true once and not now, looked for in each layer.

At a checkpoint the harness, for each question: recalls it and checks the claims; answers and grades it from each arm asked for (`memory`, `sources`, `both`, `memory@planned`); and counts, per layer, the facts and sections that say the stale words. The report is JSON and a short Markdown table per checkpoint, with the cost of each stage read from the usage the runs recorded, split into filing, dreaming and answering.

`teanode-server evaluate scenario <file> --models <models.yaml> --database-host <host> --output <directory>` runs it: it creates the database, migrates it, makes a person, an agent and a records source, and passes them in. A `--budget` stops the run between steps once the recorded cost passes it.

The scenarios live in `docs/evaluation/scenarios/`, invented from start to finish: a project, its dependencies and its people are made up and say nothing about anybody.

Milestones 2 to 6 add scenarios, and the smallest code each needs; each is described here when it starts.

## Concrete Steps

From the repository root, with a PostgreSQL that has pgvector (as `make test` starts):

    go build -mod=vendor -o build/teanode-server ./cmd/teanode-server
    build/teanode-server evaluate scenario docs/evaluation/scenarios/changing-project.json \
        --models ~/.config/teanode/evaluation/models.yaml \
        --database-host 127.0.0.1 \
        --output /tmp/scenario-run --budget 2

## Validation and Acceptance

A unit test runs a two-step scenario against the fake model server the agent tests use, and checks that the documents were filed with the records' dates, a dream ran, and the checkpoint reported each question with its claims, arms and stale-word counts. The first real run of the changing-project scenario produces a report whose numbers are reproduced, within the variation of the graded answers, by a second run.

## Idempotence and Recovery

Each run creates a database named after the scenario and the time, and leaves it for inspection; nothing else is written. Dropping the database undoes a run.

## Artifacts and Notes

Reports stay out of the repository when a scenario is a person's own; the scenarios here are invented, and their reports may be quoted in the issues.

## Interfaces and Dependencies

`agent.RunScenario(ctx context.Context, settings *agent.ScenarioSettings) (*agent.ScenarioReport, error)`, with `ScenarioSettings{Database, Storage, Configuration, Scenario, RecordsDirectory, AnswerSources, BudgetDollars, Progress}`; the person, the agent and the records source are made in the run's database by `RunScenario` itself.
