# Memory scenarios

The question set in `../README.md` asks the graph as it happens to be. A
scenario controls what went in and when: a history told in order, records
filed the way a `records` source files them, dreams run between them, and
checkpoints that ask what should be known by then. It measures how memory
behaves as knowledge changes: a decision reversed, a proposal rejected, a
procedure that stops working, a report nobody has confirmed, news that
arrives late or is corrected.

Everything in a scenario here is invented: the projects, the libraries and
the people, who are named by role. Keep it that way; a scenario about your
own life belongs beside your own question set, outside any repository.

## Running one

    teanode-server evaluate scenario <scenario.json> \
        --models models.yaml --database-host 127.0.0.1 \
        --output <directory> --from memory,sources,both,memory@planned --budget 3

The run creates a PostgreSQL database of its own (pgvector wanted, as for
the server), migrates it, and makes a person, an agent and a records source
in it. Nothing of a running server is read or changed, and nothing one run
learns reaches another. The database is kept afterwards for inspection;
drop it when done.

`--models` is what a server configuration's `agent` section holds, on its
own: providers with their keys and prices, and models. Give prices: the
budget is checked against the cost the runs record, and a model with no
price costs nothing to it.

`--from` is what each question is answered from and graded against, as
`teanode agent memory answers` does it; `memory@planned` asks the depth
judgement for the plan a live turn would follow and replays it, and
`survey` answers with a survey of the whole graph, a model call a page.
Answers from memory are given what a turn is given: the self page,
recall's pages with their overview sections, and the lessons nearest the
question. Empty asks recall alone, which costs nothing. `--budget` stops
the run between steps once it has spent that many dollars.

The output directory gets `report.json`, `report.md`, the records as the
reader saw them, and the stored files.

## The file

```json
{
  "name": "changing project",
  "description": "what happens, in a sentence",
  "steps": [
    {"id": "initial-choice", "stepKind": "records", "records": [
      {"id": "decision-001", "kind": "note", "at": "2031-03-02T10:00:00Z",
       "author": "build lead", "title": "Storage decision", "text": "..."}
    ]},
    {"id": "dream-1", "stepKind": "dream", "dreamCount": 1},
    {"id": "checkpoint-1", "stepKind": "checkpoint", "questions": [
      {"id": "store-1", "question": "Which database ...?", "kind": "direct",
       "expects": [{"words": ["Burrowdb", "4.2"]}], "forbids": [],
       "expectedAnswer": "Burrowdb 4.2.", "outdatedClaims": [],
       "evidence": ["decision-001"]}
    ]}
  ]
}
```

A step is one of four kinds:

- `records`: lines in the records shape (`id`, `kind`, `at`, `author`,
  `title`, `text`, and `channel` and `thread` for `chat`). `at` becomes the
  document's date, the time a dream reads. `author` `@you` is the person.
- `conversation`: `messages` of a conversation of the agent's own
  (`role`; `content`; `toolCalls` of `id`, `toolName`, `arguments`;
  `toolCallId` and `toolName` on a `tool` message, whose content is the
  tool's result as the tool returned it). It is stored as a turn stores
  one and remembered as a finished conversation is: what it learned, and
  the lessons its commands bore out. A shell result names its `exitCode`.
- `dream`: `dreamCount` dreams, one after another. More than one with
  nothing new filed shows what repeated maintenance does on its own.
- `checkpoint`: questions in the question set's shape. A claim with no
  `path` is met by any page, since the pages a scenario grows are named by
  the model. `outdatedClaims` are statements true once and not now: recall
  may carry them, as history, and they are looked for in every layer to
  see where one still stands as current. `evidence` names what the
  expected answer rests on, among what was filed before the question: a
  record by its `id`, a message of a conversation step as
  `<step id>#<number>` counting from one, or a whole conversation step by
  its `id`. A chat thread is filed as one document, so a chat record is
  met by a fact read from its thread; give every chat record named as
  evidence a `thread`, since posts without one are filed in windows that
  cannot be traced to a post. A question that names something not
  filed before it is refused when the file is read.

A chat thread becomes facts only when the person took part in it and it
has two posts or more; the rest of a chat archive is searched, never
read on its own. A scenario about the person's own project has them in
its threads.

## The report

For each step, the time, the cost and the size of the graph. For each
question at a checkpoint:

- whether recall carried every expected claim and no forbidden one;
- the lessons a turn asking the question is shown;
- each answer's grade, from each source asked for;
- for each expected and each outdated claim, the layers that say it and
  how many times: a current fact, a superseded or dormant one, a page's
  summary or overview, a theme, a reflection, a lesson, and the source
  documents;
- for a question with expected claims, where the facts recall carried
  that say one came from: each is followed back through its evidence to
  the records and messages it was read from, and a fact the agent derived
  from other facts, a reflection or a theme, is followed to theirs. The
  report names the inputs cited, those named in `evidence` that no
  supporting fact rests on, how many supporting facts there were and how
  many independent inputs they rest on, and how many lead back to no
  input at all. Ten facts repeating one record are one source, not ten
  confirmations.

An expected claim only in the source documents was never learned. An
outdated claim in a current fact or an overview is a stale statement, and
the layer it stands in is where to look. A step's graph size counts the
facts a later one superseded: the corrections made so far.

The dream runs on the wall clock: the scenario's time is only in the
documents' dates. What depends on the wall clock, such as decay, is not
what a scenario measures.

## Public benchmarks

`scripts/longmemeval-scenarios.py` turns LongMemEval instances into one
scenario each, and `scripts/longmemeval-summary.py` sums up their reports
per question type. `scripts/memoryagentbench-scenarios.py` turns the
MemoryAgentBench conflict-resolution histories (FactConsolidation, single
and multiple hop) into one scenario a history, its facts filed as notes a
day apart so a correction is newer than what it corrects, and
`scripts/memoryagentbench-summary.py` reports both TeaNode's grade and the
benchmark's own measure, whether a reference answer appears in the
answer. Both are TeaNode's adaptation of the benchmark, not an official
result.

## The scenarios

- `changing-project.json`: a storage decision, a rejected proposal, a
  version migration that breaks a verified procedure and brings a new one,
  an unrelated command that succeeds, and a report left unresolved.
- `execution-playbook.json`: the agent's own work, with commands: a build
  that needs a newer runtime, a deploy exception seen once, a command
  that succeeds without proving what is claimed, and a later version that
  retires the first procedure.
- `project-risks.json`: one standing question asked after each event:
  confirmed risks, an unmeasured concern, an unrelated update, a risk
  resolved, a concern that becomes a risk, and a corrected date. Run it
  with `survey` among the sources to compare a survey each time with
  recall.
- `late-corrections.json`: a change reported five days late, its date
  corrected two days later, a rejected proposal and an unconfirmed report;
  questions about what is current, what was true on a date, and what was
  known on a date.
