# How hard a turn thinks and looks

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent answered questions about the person's own work from the first thing a search turned up: one search, a passage read without its thread, a parameter it was not sure existed, and an answer given with confidence. A person who wants it to look harder had no way to say so.

After this change a turn somebody typed is looked into as deeply as the message needs. A model judges each message, with the conversation before it, the way the person would: a thanks is answered at once; a question the notes most likely hold is looked up with a procedure for it; and a question about how or why their own systems work, a problem, a decision, a pushback on the last answer, or a request for more care is researched and thought through. The operator chooses with `agent.effort`: empty (as before), a fixed `low`, `medium` or `high`, or `auto`. To see it: set `agent.effort: auto`, ask "how do I stop the cases leaning on the pallet?", and the conversation says "looking into this carefully" before the agent searches more than once, reads the thread, and says where it found the answer.

## Progress

- [x] (2026-09-25) Found that no turn with tools ever reasoned: chat completions refuses function tools while a newer OpenAI model reasons, and the provider answered that refusal by sending `reasoning_effort: none` on every such turn.
- [x] (2026-09-25) `llm.ChatRequest.ReasoningEffort`; the OpenAI provider sends a request that asks for reasoning to the Responses endpoint, in the shape the ChatGPT sign-in already speaks.
- [x] (2026-09-25) `AskSettings.Effort` and `Research`, the `effort` argument on `AskAgent`, `teanode agent ask --effort`.
- [x] (2026-09-25) The evaluation answers as the agent itself: `--from agent`, `agent@high`, `agent@medium+research`.
- [x] (2026-09-25) Measured effort alone, then the research procedure (see Outcomes).
- [x] (2026-09-25) `agent.effort` and the per-message choice (`internal/agent/effort.go`).
- [x] (2026-09-25) End to end on the development server with `agent.effort: auto`: a thanks judged `answer` and answered in four seconds; a question about improving a pack from a chat post judged `dig`, searched the chat and the wiki several times and named the team's own findings; a pushback asking to look at the attached picture judged `dig`, fetched the picture and named the actual defect; the notes show in the drawer after a reload, under the message they are about.

## Surprises & Discoveries

- Observation: on 15 answered how-to questions from a chat archive, run as real turns, reasoning alone did not help: no reasoning 53%, low 40%, medium 46%, high 43%, at up to three times the cost and twice the time. The high-effort answers reasoned out what is generally true of such systems instead of finding what the person's own people had written.
- Observation: some answers graded wrong were newer than the thread the expected answer came from; a question set built from old threads grades a fix found later as a miss.

## Decision Log

- Decision: reasoning goes through the Responses endpoint for OpenAI, and nowhere else changes; a request with no effort goes where it always went.
  Rationale: the refusal is the endpoint's, not the model's, and the Responses shape is already spoken for the ChatGPT sign-in.
  Date/Author: 2026-09-25, agent.

- Decision: depth is chosen per message, in three steps, and decides both the effort and whether the research procedure is given.
  Rationale: what helped was how to look, not how long to think; a thanks should not pay for either.
  Date/Author: 2026-09-25, the person and agent.

- Decision: the depth is a model's judgement, not a rule about words. Before the turn the fast model reads the newest message and the last six of the conversation and answers `answer`, `look` or `dig`, choosing the deeper when unsure; a judgement that fails leaves the turn as before.
  Rationale: the person asked for it: whether they want more care is in how the conversation has gone (pushing back on a thin answer, asking again) as much as in any words, and a keyword list catches neither. A first version matched phrases such as "think harder" and was replaced before it shipped.
  Date/Author: 2026-09-25, the person and agent.

## Context and Orientation

`internal/agent/ask.go` runs a turn: `Ask` starts it, `loop` runs it, and each round is one `llm.ChatRequest`. `internal/agent/prompts/ask.txt` is the turn's system prompt. `internal/llm/openai.go` speaks chat completions; `internal/llm/codex_wire.go` the Responses shape. `internal/agent/evaluate_answer.go` answers and grades evaluation questions; `teanode agent memory answers <file> --from ...` runs it.

## Plan of Work

Done as listed. The last step is the dashboard: the note a deeper turn emits shows in the drawer, and the setting is set to `auto` on the development server.

## Validation and Acceptance

`TestReasoningGoesToTheResponsesEndpoint`, `TestTheDepthJudgementIsReadOrLeftAlone`, `TestAnAgentAnswerNamesItsEffort`. The evaluation file of answered chat questions, run with `--from agent,agent+research,agent@medium+research,agent@high+research`, twice.

## Outcomes & Retrospective

On 15 answered how-to questions, run as real turns and graded against the thread's answer (two rounds for the leading two):

| Turn | Correct | Invented | Cost a question | Time |
|---|---|---|---|---|
| as before (no reasoning) | 17 of 30 | 3 | $0.03 | 10 s |
| research procedure only | 8 of 15 | 2 | $0.06 | 14 s |
| medium reasoning + research | 8 of 15 | 2 | $0.16 | 23 s |
| high reasoning + research | 16 of 30 | 1 | $0.23 | 33 s |

On single look-up questions, thinking and looking harder is no more often right and costs seven times as much, so only a message judged to deserve it is dug into. Where it does, on a diagnosis the person pushed back on, the difference is plain: the turn read the post, its thread and its picture and named the defect, where the plain turn had given a list of parameters from one search. The question set measures look-ups; a set of diagnoses, graded by what was actually wrong, is what would measure digging, and does not exist yet.
