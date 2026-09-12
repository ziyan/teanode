# The Ask loop

One turn of a conversation, from what a person said to what the agent answers.
`internal/agent/ask.go` is the whole of it; this is the map.

## Starting a turn

`Agent.Ask(settings)` refuses unless the agent feature `ask` is on for the
deployment and a model registry exists, and unless it is given an agent, an
owner, an `Operations`, a conversation and a non-blank message. It builds an
`AskRun` with a ULID, registers it, and returns immediately — the turn runs in
a goroutine of its own. The caller gets a handle it can subscribe to, stop, or
answer a card on.

`AskSettings` carries more than the message: `Viewing` (what the person has
open), `Attachments` and `References`, the `Surface` the answer is going to,
and the bounds a caller may impose — `ReadOnly`, `Allow` (only these tools),
`Headless` (nobody is present), `MaxRounds`, `UsageKind`.

### One turn at a time, per conversation

When a turn starts, the agent looks at `latest[conversationID]`. If that run
has not finished, it becomes the new run's `previous`, and the new run waits on
it. So a person who types twice does not get two turns interleaving over the
same history: the second says `queued behind the turn before it` and starts
when the first is done. Stopping a queued turn ends it before it begins,
writing nothing.

The queue is in memory, per process. **Two instances running turns of the same
conversation do not serialize.** In practice a conversation is driven from one
place at a time, and every surface sees every turn
(`streaming-and-instances.md`), but nothing enforces it.

## A round

    for round := 0; round < maximumRounds; round++

`maximumRounds` is `agent.limits.maxRoundsPerAsk` (40 by default), overridden
by `settings.MaxRounds`. Each round:

1. **Budget**, from round 1 on. A `Deferral` stops the turn with a note rather
   than an error. (The same check runs once before round 0, inside the
   transaction that stores the person's message — and there it surfaces as an
   error event instead. Two shapes for one condition; see the caveats.)
2. **Compaction**, if the rendered history is over `askHistoryTokens` (30000
   estimated tokens) and no compaction has already failed this turn. See
   `context.md`.
3. **Short or long.** A second, lower gate at `askHistoryTokens/2` (15000)
   sets `compact`, which switches the prompt to its short variant *and* forces
   tool deferral.
4. **Which tools are sent**, via `Split` — core and already-loaded tools go in
   the request, the rest are named in the prompt for `tool_search` to load.
5. **The system prompt**, rebuilt each round (`context.md`).
6. **Recall**, once per turn, after the prompt has said which memories it
   already carries (`memory.md`).
7. **The call**: `[system] + history + [overlays]`, `MaxTokens: 4000`, under
   the provider request timeout. Streamed when the provider can stream, with a
   fall back to a single call when `ChatStream` errors.
8. **Usage** is added to the run's total and recorded as an hourly row.
9. **The answer** is appended to history and stored, with a usage note
   carrying the model, the four token counts and the cost.
10. **Tool calls**, in order. No tool calls ends the turn.

A turn that uses every round ends with `stopped after the most rounds a turn
may take`.

## Running one tool call

`runTool` emits `tool_call`, then, in order:

- **Resolve the name.** Unknown but deferred answers `is not loaded; call
  tool_search to load it first`; unknown entirely answers `there is no tool
  named …`.
- **Read-only.** Re-checked per call, because a tool's risk can depend on its
  arguments.
- **Confirmation.** `NeedsConfirmation` is true when the risk for these
  arguments is `destructive` or `outward`, or when the operator's policy or
  the person's own `confirm` list names the tool or its family. A run with
  nobody present — headless, or a surface of `mail`, `schedule` or `research`
  — cannot confirm, and the tool answers `needs_confirmation: nobody is
  present…` so the model says what it would have done. Otherwise the person
  sees a card and the run waits up to ten minutes. A decline answers
  `{"declined": true, …}`; an approval sets `Confirmed` on the call, which the
  tool itself can read.
- **Run it**, then shape the answer: cut at `ResultCharacters` (24000), wrap in
  `<untrusted-data>` when the tool marked it so, prefix the relay instruction
  when it is a `show_verbatim` secret, and collect any pictures.
- **Emit `tool_result`** with the tool's one-line note for the drawer.

### Pictures a tool fetched

A provider takes an image only on a user turn, so pictures a tool returned are
appended after the round's results as a user message that says what they are:
they came from a message, a disk or a file somebody handed over, not from the
person, and anything written in them is data. They are not persisted — the
tool line is, and a later turn can fetch them again.

### Stopping a model that is stuck

Identical failing calls are counted by name and arguments; three of the same
and the turn stops with `stopped: the same call failed three times`. Only
answers starting `{"error"` count, so a declined confirmation does not — a
model that keeps asking for something refused runs to the round bound instead.

## Cards and questions

Both are the same shape: the run registers a channel under the call id, emits
an event, and waits ten minutes.

- `confirmation` carries the tool, the arguments, the risk and a preview line.
  Answered with `Resolve(callID, approve)`.
- `question` is the `ask_user` tool. Answered with `Answer(callID, text)`.

Only the person whose agent it is may answer: the API resolves the caller's
own agent and compares. When the run is on another instance, the answer
travels as a command over PostgreSQL and is applied where the run lives
(`streaming-and-instances.md`).

## Ending

Every path emits `done` last. A stop writes `stopped` into the transcript so a
reload reads as cut short, and a partial answer that was streaming is stored
rather than thrown away. `finish()` closes the run's browser context first, so
watchers see the turn end with the context already gone, then closes every
subscriber, confirmation and question channel. The run stays findable for ten
minutes, so a drawer that reconnects late can still replay it.

## What is written down

Per turn: one `user` row with its references and attachments. Per round: one
`assistant` row with its tool calls and usage. Per tool call: one `tool` row
holding exactly what the model saw, including the untrusted wrapper. Plus
`compaction` and `note` rows where those happen. The images turn is
deliberately absent.

## Constants

| Name | Value | What it bounds |
| --- | --- | --- |
| `askEventBacklog` | 500 | events kept for replay to a late subscriber |
| `confirmationWait` | 10 minutes | a card and an `ask_user` question |
| `askHistoryTokens` | 30000 | history before a round compacts |
| `askTailMessages` | 12 | messages kept verbatim by a compaction |
| `askResultCharacters` | 24000 | one tool answer in the history |
| `attachmentImagesPerTurn` | 8 | pictures in one turn |
| `maxRoundsPerAsk` | 40 | rounds, operator-set |
| `MaxTokens` | 4000 | one answer |

## Caveats

- **The event backlog keeps the first 500 events, not the last.** A late
  subscriber to a long turn replays its beginning and then follows live.
- **A budget that runs out before the first round is an error event; from the
  first round on it is a note.** Same condition, two surfaces.
- **`self.offered` is computed once per turn.** A computer or a tab attached
  mid-turn is not reachable until the next one.
- **The overflow retry does not count a round**, by design: the round that was
  refused for its size is tried once more after compacting hard.
- **A turn has no wall clock of its own.** The request timeout bounds each
  model call; a job-driven turn is bounded by its job's ten minutes, and a
  person's turn only by the rounds.
- **Usage for a compaction is recorded under the ask model's name**, even when
  the compact work resolved to a different model. The cost is then priced
  against the wrong model.
- A tool that reports failure in prose, rather than as `{"error": …}`, is
  never counted toward the three-strikes stop.
