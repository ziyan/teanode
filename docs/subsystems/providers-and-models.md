# Providers, models and what they cost

`internal/llm` talks to model services and knows nothing about mail.
`internal/config/agent.go` is what an operator sets.
`internal/agent/policy.go` is what stops a day costing more than it should.

## Three clients, one shape

Every provider implements `Chat`, `ChatStream` and `ListModels` over
`net/http`, and answers in neutral types: a message with parts, tool calls,
a finish reason and a usage. Nothing is constructed while the agent is off —
`Open` returns nil, and every caller treats nil as "no model".

- **openai** — also every service that speaks the same API, including a local
  one. It is the only client that can embed. Official endpoints take
  `max_completion_tokens`, others `max_tokens`. Cached tokens are subtracted
  out of the prompt count so the total is honest. Tool calls stream as
  fragments and are emitted once the stream ends.
- **anthropic** — system messages are hoisted out of the list, tool results
  become blocks inside a user message, and consecutive same-role messages are
  merged, because the API wants strict alternation. A cache breakpoint marks
  the last block of its group. Tool calls are emitted as each completes.
- **gemini** — the assistant role is called `model`, and there are no tool-call
  identifiers, so results are matched back by name and ids are invented per
  request. Usage is cumulative per chunk, so each chunk overwrites.

Two provider-specific edges worth knowing: an Anthropic tool call with no input
arrives with empty arguments rather than `{}` on the non-streaming path, and
two Gemini calls to the same tool in one round cannot be told apart on the way
back.

Newer OpenAI models refuse function tools with default reasoning. The client
notices the refusal, repeats the call asking for no reasoning effort, and
remembers that model for the life of the process.

## The registry

Built once from the `agent` section. It resolves `provider:model`, filters by
each provider's allow and deny lists, caches what a provider lists for five
minutes, and answers three questions: the model for a kind of work, the model
a person chose, and the embedding model.

`ForWork` resolves in one step: the override for that kind of work, else the
fast model for the three cheap kinds — triage, summaries, compaction — else the
default.

**The registry is a snapshot.** `agent.enabled`, `agent.providers` and
`agent.models` are read at startup; changing them needs a restart, and the
dashboard says so. Everything else in the section — limits, features, tool
policy, retention, currency, search, browser, connected servers — is read live.
Prices are read live too, because cost is computed from the current
configuration rather than from the registry's copy.

## Prices

Prices are per million tokens, in four parts: input, output, reading from the
cache, and writing to it. A provider carries a set, and any number of models
can carry their own; the first model pattern that matches wins, so exact names
belong above the patterns that would also catch them. A pattern matches the way
a shell matches a filename, except that `*` crosses a slash — model names are
names, not paths.

What a call cost is the four counts against those four prices. A model whose
provider is not in the configuration prices at zero, silently, as does one no
entry covers.

The currency is a three-letter code, `USD` unless set. It labels and formats.
Nothing is converted: an operator whose provider bills in euros enters euro
prices and says `EUR`.

## Budgets

A budget can be set in tokens, in money, or both, for a person's day and for
the whole server's month. The person's own caps are looked at first, because
those are the ones they can see. Whichever runs out first stops the day, and
what the agent says names the one that did.

The day is midnight to midnight **in the person's own zone**. The server's
month starts on the first, in the same zone.

Both numbers come from one query: usage grouped by model, each group priced by
its own model. The server's month is expensive to add up, so it is cached for a
minute — the cap can be passed by at most a minute's spending, which is the
price of not scanning the table on every round.

A run that has no budget left is *deferred*, not failed: a job goes back in the
queue with the time it may run again, and a conversation stops with a note.

At four fifths of whichever cap binds first, the model is told in an overlay to
avoid long reads, and the operator gets a warning in the log for the server's
caps.

## Usage

Every call records an hourly row keyed by instance, agent, mailbox, model and
kind, holding prompt, completion, cache-read and cache-write tokens and a call
count. Two writers to the same hour add rather than overwrite.

The model on the row is always the configured `provider:model` name, never what
the provider echoed back — that is what makes pricing work later.

## Structured answers

Where a model is asked for JSON, the answer is taken from a fenced block if
there is one, cut to the outermost object or array, and repaired before it is
parsed: trailing commas, single quotes, comments, an unterminated string. An
answer that is not JSON after repair is an error with an excerpt.

## Estimating tokens

Not a tokenizer, and not meant to be one: four Latin characters to a token, one
and a half tokens per CJK character, plus fixed amounts per message, per tool
call and per tool schema, and a flat thousand for an image. It exists to decide
when to compact and when to shorten the catalog.

## Caveats

- **`agent.models.research` has no effect.** A research run is an Ask turn and
  uses the ask model.
- **Two limits are configured, validated, and enforced nowhere**:
  `maxRoundsPerReply` (a reply is a single call) and `maxToolCallsPerRun`.
- **`concurrency` is read once**, when the worker is built, so it needs a
  restart even though nothing warns about it.
- **Usage rows are never swept.** The sweeper exists and is not called; they
  grow until the agent is deleted.
- **A person's chosen model is honoured only if the operator still lists it in
  `choices`**, but the usage row is written under their choice either way. Remove
  a model from `choices` and the work is done by the ask model while the cost is
  attributed to the other.
- An unknown feature name reads as off, so a typo in `agent.features` silently
  disables what it guards.
