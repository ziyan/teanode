# Retrieval depth: searchable overview sections, stated coverage, bounded planning, execution lessons, and an account of what an answer was given

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The night now writes an overview for most pages: several sections under fixed headings (what the thing is, what has been happening, how it relates, its parts, what stands out), themes over clusters of pages, and reflections over themes. A survey answers a question about a whole area by asking each page for its part.

Three gaps remain between what the night writes and what an answer is given.

First, the overview is not searchable. A page's vector is its name, summary and aliases, and recall always attaches the overview's first section. A question about how a thing relates to others, or what has been happening to it, reaches the page (when it does) and is shown the paragraph that says what the thing is.

Second, a summary and a survey do not say what they left out. An overview reads at most thirty children and thirty links; a survey asks at most forty pages, the most important first, whatever the question. An answer built from a selection reads as an answer about everything.

Third, nobody can see why an answer was given these memories and not others.

After this plan, recall finds and carries the section of an overview that answers the question; overviews and surveys state what they covered and what they did not, counted by code; a turn's retrieval is planned in proportion to the question; lessons from verified work are kept as their own kind of record; and a question can be replayed to show what was searched, found, chosen and left out, and why.

## Progress

- [x] (2026-09-30) Verified the gaps in code at v0.86.0: `nodeText` embeds name, summary and aliases only; `chooseRecalled` attaches the first section; overview inputs cap children and links at thirty; `resolveSurveyScope` takes no question and keeps the forty most important pages; reflection validation checks that citations were shown, not that they support the claim.
- [x] (2026-09-30) The recall API returns the overview section it carried, and the recall evaluation counts a claim met by it. Deployed, so the baseline below is measured by the same code as the change.
- [x] (2026-09-30) Baseline on a held-out set of 30 questions, each answered by one later section of an overview: recall carried what 6 needed; 21 reached the page.
- [ ] Milestone 1: overview sections as retrieval records, and recall carrying the matching section.
- [ ] Milestone 2: coverage and freshness counted and shown for overviews and surveys.
- [ ] Milestone 3: a retrieval plan bounded by the effort already decided for the turn; question-aware survey selection.
- [ ] Milestone 4: execution lessons as their own records, from verified outcomes only.
- [ ] Milestone 5: an explanation of a recall: what was searched, found, chosen and excluded.
- [ ] Rerun the stored question set and the held-out set after each milestone that changes recall.

## Surprises & Discoveries

- Observation: the overview's five headings are the same on every page, so the first section is always "what it is", which the page's opening already says in brief.
  Evidence: a count of headings over every stored overview found exactly five distinct headings.

- Observation: of the held-out questions recall missed, most reached the right page and were shown the wrong section; the rest never reached the page, and those are mostly themes, whose names are broad.
  Evidence: 15 of the 24 misses carried the page; 9 did not.

## Decision Log

- Decision: a section's vector is stored under an id made of the page, the section's place and a hash of its words.
  Rationale: a rewritten section is a new id, so a vector of old words can never be taken for new ones, and the embedding pass needs no stored copy of the text to tell what changed. Stale rows are deleted by the same pass.
  Date/Author: 2026-09-30, agent.

- Decision: a page found by a section is fused as one more ranked list, beside pages by meaning and by words; the section recall carries is the matched one, else the one sharing most of the question's words, else the first.
  Rationale: reciprocal-rank fusion needs no tuning, and a page found by words alone still gets the section its words point at, at no model cost.
  Date/Author: 2026-09-30, agent.

- Decision: measure the held-out set by recall alone (did recall carry the words) as well as by graded answers.
  Rationale: recall's grading is free and exact, so it can run after every change; graded answers cost model calls and vary between runs.
  Date/Author: 2026-09-30, agent.

## Outcomes & Retrospective

(To be written as milestones complete.)

## Context and Orientation

A *page* is a row of `agent_node`; its `overview` is markdown written nightly by `internal/agent/dream_overview.go`, sections under `## ` headings, with `overview_inputs` a hash of what it was written from. A page's vector is in `agent_node_vector`, a fact's in `agent_fact_vector`, written by `EmbedGraph` in `internal/agent/graph_indexing.go`, called nightly from `dreamEmbed` in `internal/agent/dream.go`.

Recall is `internal/agent/graph_recall.go`: `searchGraph` fuses four searches (pages and facts, by words and by meaning) by reciprocal rank (`fuseNodes`, `fuseFacts` in `graph_ranking.go`); `chooseRecalled` expands the top pages under a token budget. `RecallForQuestion` runs the same two steps without a turn, and `teanode agent memory recall` and `evaluate` call it through the `RecallAgentMemory` query.

A survey is `internal/agent/survey.go`: `resolveSurveyScope` picks the pages, one run per page answers, one more combines. The effort a turn deserves is decided in `internal/agent/effort.go`. Reflections are written by `internal/agent/dream_reflect.go`.

## Plan of Work

Milestone 1. Add `agent_overview_section_vector` (migration 0136) and `db.AgentOverviewSectionTable`. `internal/agent/overview_section.go` parses an overview into sections, gives each an id, embeds the missing ones and deletes the stale ones (`EmbedOverviewSections`, called by `dreamEmbed`), searches them by meaning (`nearestOverviewSectionsTo`), and picks the section recall carries (`overviewSectionFor`). `searchGraph` returns the matched section of each page and fuses the pages found by sections; `chooseRecalled` carries the chosen section.

Milestone 2. `readOverviewInputs` and `resolveSurveyScope` return, beside what they chose, what was eligible and why the rest was left out (over the cap, no overview yet). The overview stores the counts with the evidence; the survey report ends with them; the dashboard shows them.

Milestone 3. Where `effort.go` already classifies a turn, a relational or ambiguous question gets up to two focused searches and one hop along the graph's links; a broad one is given overview sections first and offered a survey. `resolveSurveyScope` takes the question and ranks candidate pages by it as well as by importance, keeping a floor of representative pages when the question is about the whole area.

Milestone 4. A lesson record (applies when, what worked, what to avoid, how it was verified, scope, evidence) is filed only after an outcome a tool result confirmed, never from the assistant's own account, and recall injected into a turn is stripped before the turn is read for lessons.

Milestone 5. `RecallForQuestion` records its searches, their candidates, the ranks and the reason each candidate was chosen or excluded (budget, inactive, duplicate, no vector); `teanode agent memory recall --explain` prints it.

## Concrete Steps

From the repository root:

    go test ./internal/agent/ -run TestTheSectionOfAnOverviewRecallCarries
    TEANODE_TEST_DATABASE_HOST=<postgres> go test -mod=vendor ./internal/agent/ ./internal/db/...
    make lint-ci

Against a deployed server, with a question set whose claims name the overview's words:

    teanode agent memory evaluate --json questions.json
    teanode agent memory answers --stored --from memory

## Validation and Acceptance

Milestone 1 is accepted when the held-out set carries more of what it needs than the baseline of 6 of 30, the stored set graded from memory does not fall, and a turn costs no more model calls than before.

## Idempotence and Recovery

The embedding pass writes only missing sections and removes only sections no overview holds, so it can run any number of times. Reverting migration 0136 drops the table; recall then carries each page's first section again.

## Artifacts and Notes

The held-out questions are written from the person's own graph and are kept with the server's evaluation data, not in this repository.

## Interfaces and Dependencies

`db.OverviewSectionOperation` (`ListAgentOverviewSectionVectorIds`, `PutAgentOverviewSectionVector`, `DeleteAgentOverviewSectionVectors`) and `db.AgentOverviewSectionTable`. The `RecallAgentMemory` query's pages gain `summary` and `overview`.
