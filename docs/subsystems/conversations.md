# Conversations

Where everything the agent has said and done is kept.

`internal/models/insight.go`, `internal/db/database_insight.go`,
`internal/agent/describe.go`, `internal/agent/tools/conversation`.

## Three kinds

- **main** — the one continuous conversation. There is exactly one per agent,
  made on demand. It is never deleted and never archived; it is compacted
  instead.
- **named** — a conversation kept apart, listed in the drawer's picker, which
  a person can rename, archive or delete.
- **run** — the transcript of a job nobody watched: a triage, a research pass,
  a scheduled run. Written so there is something to read afterwards.

A person can make a named conversation the main one, which demotes the old main
to named; the command line does it with `teanode agent conversation main`.

A run transcript can be talked into. Opening one from the agent page and asking
a question starts an ordinary turn in it.

## What a message is

One row per user turn, per round's answer, and per tool result, oldest first,
with the tool calls and a usage note on the answer. Two other roles appear:
`note` for a stopped turn, and `compaction` for the note that stands in for
everything before it (`context.md`). Neither is sent to a model.

Reading a conversation returns the **newest** page — a drawer opens at the end
— along with the conversation's open todos.

## Titles and summaries

A background describer names conversations and writes the one line under them.

It looks once a minute, takes twenty conversations that have been quiet for
three minutes and whose last message is newer than their last description, and
asks a model for a title of five words or fewer and a one-sentence summary, in
the language the person writes in. It reads the last twelve user and assistant
messages, and at most the last six thousand characters of them: not the tools,
not the notes.

Three rules shape it:

- **A person's title wins for good.** Renaming a conversation marks it as
  theirs, and the describer never touches the title again. It keeps the summary
  current.
- **The main conversation is never titled**, only summarized. Every surface
  calls it the primary conversation.
- **A brand new named conversation is titled at once**, as soon as the first
  answer lands, rather than waiting for the quiet period — so the picker has a
  name for it.

The describer runs off the worker's tick in a goroutine, one at a time, because
twenty model calls would otherwise stop the tick claiming jobs.

## Searching

Searching matches the title, the summary, and what was said in user and
assistant messages, newest first, archived ones included. It does not reach run
transcripts, and it does not read tool results.

The `conversation` tool gives the agent the same three things: `search` over
main and named conversations, `list` over every kind including run transcripts,
and `read` for one conversation from its end, with a note saying how many
earlier messages there are.

Two rules hold in `read`. Another agent's conversation reads exactly like one
that does not exist, and nothing is loaded before that is checked. And a secret
a tool showed once — an API token, an app password — is replaced by a note
saying one was there: it was to be relayed once and kept nowhere, so it is not
read back into another conversation.

## Retention

Once an hour the worker sweeps, using the two retention settings:

- `agent.retention.runs`, thirty days by default, removes **run transcripts**
  and their messages, every **finished job** row, and the agent's **reply
  records**.
- `agent.retention.corrections`, ninety days by default, removes recorded
  corrections.
- Separately, uploaded files nobody claimed within a day are removed with their
  bytes.

Main and named conversations are never swept. They grow until compaction folds
their beginning into a note.

## Caveats

- **A person's title is theirs; their summary is not.** The describer keeps
  rewriting the summary of a conversation the person named.
- **A title given at creation is not marked as the person's**, so the describer
  will replace it. Renaming afterwards is what makes it stick.
- **A search query is not escaped**, so `%` and `_` in what a person types act
  as wildcards.
- **Archived conversations are still readable by the tool**; it does not check.
- **Files attached to a run transcript outlive it.** The sweep removes the
  transcript and its messages, but attachments are not tied to a conversation
  by a foreign key and only unclaimed ones are collected, so artifacts made
  inside a run leave their rows and their bytes behind.
