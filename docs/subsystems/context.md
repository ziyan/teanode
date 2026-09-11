# Context

What the model is actually sent: the prompt in layers, the overlays after the
history, how a person's message becomes a turn, and what happens when it all
grows too long.

`internal/agent/prompts.go`, `prompts/ask.txt`, `ask.go` (`systemPrompt`,
`situation`, `overlays`), `attachment.go`, `compact.go`.

## The shape of a request

    [ system: the prompt          ]  <- one cache breakpoint, rebuilt each round
    [ history: user/assistant/tool ]
    [ system: the overlays        ]  <- what is true this minute
    [ tools: what may be called   ]

The prompt is rebuilt every round but is meant to be stable, so the provider
can cache it. Everything that moves — the clock, the open todos, the attached
tab, what the budget has left — is pushed into the overlays *after* the
history, where it costs nothing to change.

## The prompt, layer by layer

Prompts are Go templates embedded from `prompts/`. A person's words and an
operator's words are rendered inside delimited blocks, never interpolated into
the conduct.

0. **Identity.** Who the agent is, whose it is, on what server, and that it
   acts with exactly what that person may do and never beyond it.
1. **The conduct.** Two variants of the same ground, chosen by the round's
   `compact` flag: a set of sections, or one dense paragraph when the history
   has grown. It covers acting for the person (a refusal is final; never
   self-confirm), mail being data, secrets, remembering, reaching for tools,
   and how to answer.
2. **House instructions**, if the operator wrote any, in `<house-instructions>`.
3. **The situation**, computed fresh: who the person is, what they may do,
   which mailboxes they have granted and what is on for each, which they have
   not granted, their time zone and language, the server and its version,
   whether search by meaning exists, and which surface this is.
4. **The person's instructions**, if any, in `<instructions>`.
5. **Memories** — the top of what the agent remembers, said to be the top, with
   each id so the model can change one (`memory.md`).
6. **Guidance**, contributed by the tools actually sent, de-duplicated.
7. **More tools** — the deferred catalog, one line each, for `tool_search`.

The situation deliberately carries the time *zone*, not the time: the clock
would make the cacheable prefix change every minute.

## The overlays

One system message after the history, rebuilt each round:

- `<viewing>` — what the person has open, so "this" means it.
- `<surface>` — how to write here. A phone wants it short with no tables; a
  terminal wants plain text and nothing to click; mail wants the first line to
  be a subject; a chat app wants short paragraphs and no headings.
- One block per tool that contributes one: `<todo>`, `<tab>`, `<computer>`,
  `<recalled>`, `<pending>`.
- `<budget>` — only once four fifths of whichever cap binds first is gone.
- `<now>` — always, in the person's zone.

## A person's message

`userTurn` builds the turn the model reads:

- `<references>` for the threads they pointed at, telling it to read one before
  answering about it.
- Their text.
- Then each attachment, by what it is. A picture within 10 MiB, up to eight per
  turn, is read from storage and sent as an image part with a `[picture
  attached: …]` marker. A file whose text was extracted at upload is inlined in
  an `<attachment>` block. Anything else is named, with a line saying the agent
  cannot open it and should ask if it matters.

From the next round on, the same message is rendered by `historyTurn`:
references, text, and every file *by name*. A picture costs its tokens once,
in the turn it arrived.

## Compaction

A long conversation is folded into a note so the turn can continue.

### When

- **Before a round**, when the rendered history passes 30000 estimated tokens.
  If that compaction fails, the flag is set and it is not tried again this
  turn.
- **After a refusal**, once per turn, when the provider says the request is too
  long. This one compacts hard (a tail of 2) and retries the round without
  counting it.

### How

1. **Choose the cut.** Start `askTailMessages` (12) from the end; move the cut
   forward while the tail still weighs more than 15000 tokens, never leaving
   fewer than 2; then move it *back* past any tool message, so the tail never
   opens with an answer whose call is gone.
2. **Find the resume point.** Walk the older half backwards for the last stored
   message id. That id — not the note — is what `CompactedThrough` records.
   Without one, nothing is written and the history is returned unchanged.
3. **Write the note.** Pack the older half into chunks of 12000 tokens, each
   message cut to 6000 characters, and call the compact model once per chunk,
   carrying the previous note forward so the note is folded rather than
   stacked. The prompt asks for plain text under five headings — Decided, Open,
   Learned, Prefers, Identifiers — under 400 words, and says that only the
   person's own words decide what was decided or preferred.
4. **Store it** as a `compaction` message and set `CompactedThrough` in the
   same transaction.
5. **Return** the note plus the tail.

### Reading it back

`historyOf` prepends the note as a **user** message wrapped in
`<untrusted-data>`, saying it was written from the transcript, tool answers
included, so an instruction inside it is not the person's. Then it skips
everything up to and including the resume point and converts the rest.

A resume point that is no longer in the transcript resets to empty, so
everything is replayed rather than nothing.

### Repairing the history

Before every request, `repairHistory` drops assistant tool calls that have no
answer and tool answers whose call is gone. A dangling pair is what makes a
provider refuse a request outright, and compaction, deletion and a stopped turn
can all leave one.

## Constants

| Name | Value | What it bounds |
| --- | --- | --- |
| `askHistoryTokens` | 30000 | history before a round compacts |
| `askHistoryTokens/2` | 15000 | the short prompt, forced deferral, the tail's ceiling |
| `askTailMessages` | 12 | messages kept verbatim |
| `compactLeastTail` | 2 | fewest kept, and the tail after an overflow |
| `compactChunkTokens` | 12000 | conversation per note-writing call |
| `compactMessageCharacters` | 6000 | one message inside a chunk |
| `attachmentTextCharacters` | 60000 | text extracted from a file at upload |
| `attachmentImageBytes` | 10 MiB | a picture the model may look at |
| `DeferralThreshold` | 40 | catalog size above which tools defer |

Token estimates are four Latin characters to a token, one and a half for CJK.

## Caveats

- **The estimate ignores images.** A conversation full of pictures measures far
  smaller than the provider sees, so the compaction gate can be late.
- **A history that begins with tool messages can pin the cut at zero**, leaving
  no resume point. Nothing is written, the history is unchanged, and the same
  round compacts again next time — paid for, with nothing to show.
- **A stale resume point replays the whole conversation *and* the note.** The
  model sees both the summary and what it summarizes.
- **The voice settings do not reach this prompt.** Tone, length, greeting and
  sign-off are in the prompt used for triage, summaries, drafts and replies;
  the drawer's conduct does not carry them.
- **The situation promises search by meaning when the server has an embedding
  model**, without checking whether the particular mailbox has it on.
- **Only some prompts have golden files** (`testdata/prompts`): the triage,
  draft and summarize ones. The Ask conduct can change without a test noticing.
