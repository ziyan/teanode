# Memory limits audit

Every hard-coded number in the memory's write and read paths that cuts,
caps, samples or skips content, and what is done about each. About a
hundred and twenty were found; most are pacing (what is not done now is
done by a later run, and nothing is lost) and are not listed. Listed are
the ones that lose information, or keep it out of an answer, and the
evidence for each. Audited 2026-09-30.

A number backed by a measured incident in its comment is noted as such;
most of the limits below had reasoning at most.

## Fixed

| Where | Was | Now |
| --- | --- | --- |
| `dream_digest_retrieval.go` reading | each document's first passage, cut to 1200 characters, then marked read | whole; a long document in parts of about 40000 characters, marked read with its last part; a part too long for the model's window is halved |
| `dream.go` `digestFacts` and `remember_prepare.go` `rememberFacts` | ten facts a reading call, fifteen a conversation run, the rest dropped silently | about one fact per 600 characters read, ten at the least |
| `remember_request.go` `rememberMessageCharacters` | each message of a conversation cut to 1500 characters, then marked remembered | whole; a run reads as many messages as fit one call and the rest wait |
| `lessons.go` lesson transcript | each message cut to 1500 characters | whole, within the pass's own 40000 |
| `dream_consolidate.go` merge | a pair merged whatever the dropped wording named | not merged when the dropped wording names something the kept one does not |
| `database_dream.go` chat threads | read only with the person in them and three posts | two posts: a question and its answer is the shortest decision there is |
| `dream.go` `digestSmallest` | documents under 160 characters marked read without being read | only documents with nothing in them (under 8 characters by both measures) |
| `dream_attachment.go` full lists | files left out of a full list declined for good, unjudged | passed over: they wait behind the files not yet shown and come back in another batch; declined after three full lists |
| `dream_consolidate.go`, `dream_overview.go` | a page's opening and overview written from its first 200 facts, the oldest | from its newest 200 |
| `ask.go` overlay | ten lines kept while recall could write twelve; the broad-area note was dropped | the sum of what recall writes |
| `evaluate_answer.go` | eight passages of 1200 characters; no index for the memory arm | the knowledge tool's twelve, each whole; the index a turn carries |
| `knowledge` tool and `teanode agent knowledge search` | each passage cut to 700 characters, though passages run to 2000 (76% were cut) | each passage whole |

Measured on LongMemEval-S (see `memory-evaluation-execplan.md`): with the
first 1200 characters, memory alone answered 12% of 12 questions; reading
16000, 46%. Where the evidence sat in the first 1200 characters memory got
10 of 28, past it 2 of 18.

## Write side, open

- **Files over 512 KiB keep their first 64 KiB, and OCR reads 30 pages** (`computer/scan.go`, `scan_ocr.go`). The rest is never chunked, so search cannot find it either. Needs the sizes of real archives before a number is chosen.
- **The lessons pass reads the last 40000 characters of a window** (`lessons.go`). A long working session loses its start.
- **The scan never keeps a commit's diff** (`computer/scan.go` `scanDiffBytes` is declared and unused).

## Read side, open

- **Recall carries 1200 tokens, 400 of them held for loose facts** (`graph.go` `recallTokens`, `recallFactTokens`), about two to four pages a turn, each with at most five matched facts, one overview section cut to 600 characters, and two pages no fact matched. Each of these keeps a found answer out of a turn. The cost of raising them is prompt tokens on every turn, so they want a measured comparison in the scenario harness rather than a new number.
- **Three document passages of 600 characters, 700 tokens in all** (`graph.go` `recallChunks`, `recallChunkCharacters`, `recallKnowledgeTokens`), and the loop stops at the first that does not fit. Same: measure.
- **The index lists 1500 tokens of pages by importance, first sentences of 140 characters** (`graph.go` `indexTokens`, `graph_prompt.go`). A page not named is a page a turn does not think to read.
- **The self page carries 20 facts** (`graph_prompt.go`), where its comment says it is always carried in full.
- **A survey asks 40 pages, 10 by importance first** (`survey.go`); it says what it left out, the one limit on the read side that does.
- **Search candidates are twenty a list, and meaning below 0.25 is dropped** (`graph.go` `recallCandidates`, `meaningFloorGraph`). Reasoned, not measured.
- **A planned search longer than 120 characters is discarded, and a failed depth judgement plans nothing** (`effort.go`). Silent.

## How to decide the rest

The scenario harness (`docs/evaluation/scenarios/`) runs a change against
the same histories with the same models, and LongMemEval-S gives a public
comparison. A limit is raised, removed or kept on what those show, with
its cost, and the number's comment says what it was measured on.
