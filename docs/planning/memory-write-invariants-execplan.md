# Memory writes keep what the evidence says, and dreaming is measured

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent's memory is written by several paths: remembering a conversation (`internal/agent/remember*.go`), reading documents at night (`dream_digest*.go`), and tidying pages at night (`dream_consolidate.go`). Each path decides on its own whether two facts are the same, whether a new fact replaces an old one, and whether a model's answer is usable. They disagree, and where they disagree memory loses information: two distinct dated events become one, an unrelated fact retires a true one, a malformed answer is recorded as "read, nothing found", an inferred statement inherits a stated fact's confidence, and a page tidied while the person edits it loses the edit.

After this plan, every path that writes a fact obeys the same rules, a model answer that cannot be read is never taken as an empty one, a tidy-up never overwrites what it did not read, and there is a repeatable way to see whether each stage of dreaming makes answers better or worse. Seeing it work: the regression suite in Milestone 6 runs the cases this plan names (two events on two dates, an unrelated supersession, a malformed answer, an inferred-to-stated merge, an edit during consolidation) and every one passes; the replay comparison prints answer correctness for raw sources, extracted memory, and each dreaming stage on the same history.

## Progress

- [x] (2026-09-23) An outside review of main at v0.54.1 named five defects; each was checked against the code and four were reproduced with throwaway tests (see Surprises). Wrote this plan.
- [x] (2026-09-23) Milestone 1: one rule for "the same fact", used by every path that folds or merges (#135). The fold now asks every near candidate, not only the nearest.
- [x] (2026-09-23) Milestone 2: a supersession names what replaces what, and keeps both when unsure (#136).
- [x] (2026-09-23) Milestone 3: a validated answer contract: valid and empty, valid with results, or invalid; invalid is retried and then shown as failed, never recorded as read (#138). Left: showing given-up documents on the source's page, and marking one oversized document failed rather than read.
- [x] (2026-09-23) Milestone 4: merging keeps what the evidence establishes (#139).
- [x] (2026-09-23) Milestone 5: consolidation writes only the opening, marks the page with the time it read the facts, and skips a merge of facts changed during the call.
- [ ] Milestones 1 to 5 watched over a night's dream on the deployed server.
- [ ] Milestone 6: evaluation: a deterministic regression suite, and a replay that compares answers with and without each dreaming stage.
- [ ] Milestone 7: when a claim was true and where it came from: validity intervals, derivation, and no past tense by age alone.

## Surprises & Discoveries

- Observation: the fold after a fact is written ignores what the check before writing respects. `isTheSameFact` (`remember_fold.go`) compares kind and date; `whatToFold`, reached from the embedding-based twin search, compares wording only, so a second "Completed the annual inspection" dated a year later is folded behind the first and only one year remains visible.
  Evidence: reproduced with an embedding model; the existing two-dates test runs without embeddings, so the fold never runs, and it reads inactive facts as well, so it would pass even if it did.
- Observation: `replacementFor` (`remember_replace.go`) accepts any fact filed on the same page that is at least as well evidenced, whatever it says; `retractionHolds` accepts a quote found in any message the run saw, the agent's own replies included, and an empty message passes.
  Evidence: reproduced both: a fact about something else retired a true one, and a quote of the agent's own "Quite." retired a fact with nothing filed in its place.
- Observation: `parseDigestResponse` returns an empty answer without error for `{}`, for `{"error": ...}`, and for a cut-off `{"facts":` that the JSON repair completes; the batch is then marked read, which bypasses the two-tries rule. `parseRememberAnswer` turns prose or a cut-off object into an empty answer and the conversation's read mark still moves; this one is deliberate (its comment and test say so), and it silently skips a window of messages. Consolidation cannot tell `{}` from `{"summary": ""}`, so an error object blanks a summary.
  Evidence: reproduced; `dream_attachment.go` already distinguishes a missing answer from an empty one and is the pattern to follow.
- Observation: `mergeSaidTwice` keeps the lower-numbered fact and copies the chosen wording onto it, combining evidence but keeping the kept fact's inferred flag, confidence, kind and date.
  Evidence: reproduced: an inferred sentence ended up as a stated fact at full confidence.
- Observation: consolidation rebuilds the whole page row from a copy read before the model call and saves every column; it stamps `consolidated_at` with the time the write ends. A rename, alias or pin made during the call is reverted, the page's dormant flag is written as false (bringing back a page archived meanwhile), and a fact added during the call falls before the mark and is never summarized until it changes again.
  Evidence: from the code (`PutAgentNode` saves the whole row; the due-page query compares facts' `modified_at` with `consolidated_at`).
- Observation: `decayOfEdge` calls a link stale by age alone, but nothing outside its test calls it, so no link is shown in the past tense today.

## Decision Log

- Decision: fix the write paths before building evaluation or new retrieval.
  Rationale: the defects lose or distort memory now, the fixes are ordinary code with no recurring inference cost, and an evaluation run over a memory that corrupts itself would measure the corruption. Order: shared identity and response validation, then consolidation concurrency, then evaluation, then temporal and provenance metadata.
  Date/Author: 2026-09-23, the person, following the outside review.

- Decision: when a destructive change is uncertain, keep both.
  Rationale: a duplicate costs a line on a page; a wrongly retired or merged fact costs the truth, and nobody sees it happen. Every rule below prefers leaving two facts to losing one.
  Date/Author: 2026-09-23, agent.

- Decision: a named pairing is trusted without the model check the plan described for a pair that shares no subject.
  Rationale: the check would put a model call inside the transaction that files an answer, and the main defect, any fact on the page standing in for the retired one, is gone once the answer must name its replacement; kind, date and evidence are still checked.
  Date/Author: 2026-09-23, agent.

- Decision: a merge's survivor takes the standing of the fact whose wording it keeps, rather than always the weaker of the two; a richer wording on softer ground is not merged.
  Rationale: always taking the weaker would downgrade a stated rewording of an inferred line; what the review found was an inferred wording borrowing a stated row's standing, and this rules that out.
  Date/Author: 2026-09-23, agent.

- Decision: after a consolidation that merged facts, the page is due once more, because the merge touched facts after the time the page is marked with.
  Rationale: marking with the time the facts were read is what lets a fact added during the call be seen; the cost is one extra rewrite of a page whose facts were merged.
  Date/Author: 2026-09-23, agent.

- Decision: citation membership stays a deterministic check, but it is never taken as proof that a citation supports a correction.
  Rationale: a quote that occurs in the conversation shows the words were said, not that they retract a claim; the second question needs the pairing in Milestone 2 and, only where it stays ambiguous, a model check.
  Date/Author: 2026-09-23, agent.

## Outcomes & Retrospective

Milestones 1 to 5 shipped on 2026-09-23, each with tests that fail on the code before it. Every defect the review named was reproduced by a test before it was fixed. The night's dream after the deploy has not been read yet.

## Context and Orientation

A *fact* is a row of `agent_fact`: a sentence on a page of the memory graph, with its evidence (quotes and where they came from), an `Inferred` flag (the model concluded it rather than someone saying it), a `Confidence`, a `Kind` (a state, an event, a preference) and, for events, `HappenedAt`. A *page* is a row of `agent_node` with a summary written by consolidation. Facts are retired, not deleted: an inactive fact stays for history and is left out of recall.

The paths that write facts: `remember.go` files what a conversation established, through `remember_fold.go` (is a new fact the same as one on the page, `isTheSameFact` before writing, `whatToFold` after writing when the embedding search finds a twin) and `remember_replace.go` (a new fact supersedes an old one: `replacementFor`, `retractionHolds`). The night's reading, `dream_digest.go`, files facts from documents with the same helpers; its answers are parsed by `dream_digest_response.go`. The night's tidying, `dream_consolidate.go`, rewrites a page's summary and merges facts said twice (`mergeSaidTwice`), writing the page back with `PutAgentNode` (`internal/db/database_graph.go`) and stamping it with `MarkAgentNodeConsolidated`. The remember answer is parsed by `remember_response.go`. The attachment answer parser, `dream_attachment.go`, is the one that already tells a missing answer from an empty one.

The existing memory evaluation, `teanode agent memory eval` (see `docs/subsystems/memory.md`), measures recall of facts for a question, not the final answer, and its questions are examples.

## Plan of Work

Milestone 1, one rule for the same fact. Make `isTheSameFact` the only answer to "is this the same fact": `whatToFold` calls it instead of comparing wording, and `mergeSaidTwice` refuses to merge two facts it says are different (another kind, another date). Two events with the same words on different dates stay two facts. The regression test uses the world that embeds (`newRememberWorldThatEmbeds`), files the same sentence on two dates, and asserts that both facts are active and that recall for a question about the event returns both. A second case files the same sentence twice on the same date and asserts one active fact.

Milestone 2, supersession names its replacement. The remember and digest answers gain an explicit pairing: a supersession says which new fact (by its index in the answer) replaces which old fact (by number), or that the old fact is retracted with a quote. `replacementFor` accepts only the named fact, and only when it is on the same page and compatible with the old one: the same kind, and, for dated facts, a date not earlier than the old one's. A retraction quote must come from the person's own message in the window, not from the agent's reply, and must be non-empty. When either check fails, both facts stay and the run notes the unpaired supersession in its record. Only when the pairing is present but the two facts share no subject (none of the names or numbers in one occurs in the other) is a model asked, once, whether the new fact replaces the old; a "no" or no answer keeps both. Tests: the unrelated replacement and the self-quote retraction from Surprises each leave the old fact active; a correct pairing retires it.

Milestone 3, a validated answer contract. Add a small shared type in `internal/agent` for a model's structured answer with three outcomes: valid and empty, valid with results, invalid (unparsable, cut off, an error object, or the expected field missing). Fields the answer must have are pointers, so their absence is seen; the JSON repair may still be used, but a repaired object whose required field is missing or cut is invalid. Digest: an invalid answer counts toward `givingUpOn` exactly as a failed call does, and after the second failure the documents are marked failed with the reason, visible on the source's page, not read. Remember: an invalid answer does not move the read mark; the window is tried again on the next remember, and after the second invalid answer the window is recorded as skipped with the reason on the conversation, so it is visible rather than silent. Consolidation: an invalid answer leaves the page untouched. A single document that stays too large for the model after splitting is marked failed with that reason. The existing test that asserts a repaired `{"facts":` is an empty success is changed to assert it is invalid.

Milestone 4, merging keeps the evidence's standing. Merging distinguishes equivalent wording from additional information. `mergeSaidTwice` merges automatically only when the model says the two are the same and `isTheSameFact` agrees on kind and date; the kept fact takes the chosen wording and the weaker of the two facts' standing (inferred if either is, the lower confidence). When the chosen wording adds information to the kept fact's, the two are not merged: the richer statement stays its own fact with its own evidence and uncertainty. Test: a stated short fact and an inferred richer one are asked to merge; afterwards either both remain, or the survivor is inferred at the lower confidence.

Milestone 5, consolidation writes what it owns. Consolidation no longer saves the whole page: a targeted update writes only the summary (and the fields the job owns), so a rename, alias, pin or dormant flag set meanwhile stands. The job records the time it read the page's facts and stamps `consolidated_at` with that time, not the time it finished, so a fact added during the model call is newer than the mark and the page is due again. No transaction is held open across the model call. Before applying merges, the job checks that the facts it read are still active and unchanged (their `modified_at`); a merge whose facts changed meanwhile is skipped, not applied to the new state. Tests: a rename during the call survives; a dormant page stays dormant; a fact added during the call makes the page due again.

Milestone 6, evaluation. Two layers. First, the deterministic regression suite: the cases of Milestones 1 to 5 as tests that run in `make test`, plus a late-arriving correction (a fact contradicted a week later) and two consolidations of one page at once. Second, an end-to-end replay: a small set of questions (fifty to a hundred, written by the person about their own history, covering extraction, cross-conversation reasoning, dates, updates, and questions whose honest answer is "I do not know"), and a public benchmark slice for reproducibility. `teanode agent memory eval` gains a replay mode that loads a fixed source history into an isolated agent and answers every question under four configurations with the same answering model and context budget: raw sources only; extracted memory with raw sources; the above plus consolidation; the above plus association and rehearsal. It reports, per configuration: answer correctness, stale answers, unsupported claims, citation support, latency, and the total cost of ingestion, dreaming and answering. Failures are read by hand before any model grader is trusted.

Milestone 7, time and provenance. Changing claims and relationships gain optional validity (`ValidFrom`, `ValidUntil`), set only from evidence; age alone never ends a claim, and `decayOfEdge` becomes relevance and a freshness note, not a past tense, or is removed while nothing uses it. Facts gain a derivation (said by a person, read in a document, concluded by the model, described from a picture), the parent facts it was derived from, and the source's version and hash where there is one; every rewrite preserves the weakest derivation of what it combines. The picture-reading path is the first user: a model's description of an image is labelled an interpretation, keeps the image's reference, and facts extracted from it inherit that label.

## Concrete Steps

Each milestone is one pull request with its tests, deployed and watched over a night's dream before the next. For each, from the repository root:

    TEANODE_TEST_DATABASE_HOST=<test database> go test ./internal/agent/...
    make lint-ci

and on the deployed server, the dream log for the night after the deploy (`teanode agent dream log --first 2`): the counts of facts filed, folded and superseded compared with the nights before, and the runs of any failure the milestone makes visible.

## Validation and Acceptance

Milestones 1 to 5 are accepted when their named tests pass and a night's dream on the deployed server shows no new failures and no drop in facts filed beyond what the changed rules explain (fewer folds of distinct events, fewer supersessions without a pairing). Milestone 6 is accepted when the replay prints its table for all four configurations on the person's question set and the public slice. Milestone 7 is accepted when a claim with an end date is shown as ended and one without is not, whatever its age, and when a fact from a picture is shown as an interpretation.

## Idempotence and Recovery

Every change is to code paths and tests; schema changes come only in Milestone 7 (new nullable columns), each with a reverse migration. A rule that turns out too strict (for example, pairing that keeps too many duplicates) is relaxed by a later change; nothing in this plan deletes facts that exist today, and facts wrongly folded or retired before it stay as they are unless the person restores them.

## Artifacts and Notes

The reproductions behind Surprises were throwaway tests run against main at v0.54.1 and are not kept; each becomes a regression test in its milestone.

Research that informs the later milestones, as design direction rather than a promise of any score here: separating when a claim was true from when it was learned (temporal knowledge graphs of the Graphiti kind); separating evidence from derived beliefs and summaries, and keeping reflection traceable; offline consolidation separated from answering, which the night already does; background work pays when later questions are predictable, which argues for favouring pages that change and are asked about.

## Interfaces and Dependencies

In `internal/agent`, a shared answer type:

    type modelAnswer[T any] struct {
        Value   T
        IsValid bool   // false: unparsable, cut off, an error object, or a required field missing
        Problem string // why it is not valid, for the record
    }

used by `parseDigestResponse`, `parseRememberAnswer` and the consolidation parser, with required fields as pointers in their decoding structs. `isTheSameFact(a, b *models.AgentFact) bool` stays the single identity rule, called by `whatToFold` and `mergeSaidTwice`. Consolidation's page write becomes a targeted update in `internal/db` (a method that sets the summary of one page), and `MarkAgentNodeConsolidated` takes the time the facts were read. No new third-party libraries.
