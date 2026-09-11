# Memory, and finding it again

What the agent keeps between conversations, and the two ways it gets it back.

`internal/agent/memory.go`, `memory_meaning.go`, `internal/agent/tools/memory/`,
`internal/agent/embed.go`, `internal/db/database_memory.go`.

## What a memory is

A title, a body of at most four thousand characters, tags, whether it is
pinned, when it was last used, and the audiences it is for. One person's, on
one server, never shared.

Audiences are the five places a memory can be read: the conversation, sorting
mail, research, drafting a reply, and summaries. The agent's own tool always
adds the conversation to whatever else is asked for, because a memory kept for
sorting alone was one the person could never ask about, and the agent would
then say it knew nothing.

## What every prompt carries

The system prompt carries the twenty most useful memories — pinned first, then
by when they were last read, then by when they changed. Each line ends with its
id, so the agent can update or delete exactly the one it means. A job that
cannot ask for more carries thirty, without ids.

Reading a memory stamps it, so what the agent actually uses drifts to the top
of the next prompt and what it never touches sinks.

## Recall, once a turn

Before the first call of a turn, the person's own words are turned into search
words: lowercased, cut at anything that is not a letter or a digit, anything
shorter than four characters dropped, sixty-one words that are long enough to
pass that test and still say nothing dropped as well, and at most twelve kept.

Those words fetch twenty candidates by substring, ranked by how many of the
words each one holds. If there is an embedding model, the five nearest by
meaning are put in front of them. At most five survive, skipping anything
already in the prompt and anything not for the conversation, and they are
written into an overlay the next round sees.

A message with no word long enough recalls nothing at all, by meaning or
otherwise.

## Vectors

Memories carry their vector and the name of the model that made it, in two
columns on the same row. There is no vector extension: cosine is computed in
Go over a bounded set, the same decision the mail search took.

| | value |
| --- | --- |
| characters embedded | 4000 |
| candidates ranked | 1000 |
| neighbours kept | 5 |
| least similarity to count | 0.25 |
| similarity that means "a copy" | 0.92 |
| vectors written per turn | 10 |

Text is cut by character, never by byte: a byte cut through a character embeds
a replacement mark instead of the word.

There is no job for memory vectors. Each turn writes ten for memories that have
none, which is also how a change of embedding model catches up — the query asks
for memories whose vector is missing *or* from another model.

## Not remembering the same thing twice

When a memory is written, its vector is stored and the nearest others are
looked at. Anything above ninety-two hundredths comes back with an instruction
naming both ids: put what is new into the older one, then delete this one, in
this turn. It was written as a hedge first, and the model kept both. An
imperative naming both ids is obeyed.

## Searching mail by meaning

The same shape, different bounds. One vector per message per mailbox per model,
in its own table. A search embeds the query and ranks the mailbox's newest three
thousand vectors, keeping anything above a quarter.

The words and the meaning are two searches whose results are joined, word hits
first. Because the joined list is then cut to the limit, a word search that
already filled the limit leaves no room for what the words missed.

Trash and junk are never embedded.

## Caveats

- **A memory that names no audience is read by nothing.** The agent's tool
  cannot make one, but the dashboard and the command line pass audiences through
  as given.
- **Recall by meaning needs a word first.** Where the word list comes out empty,
  the vector search is never reached.
- **Ranking is by substring**, so a short word scores inside a longer one.
- **An agent with more than a thousand memories** ranks its pinned and its most
  recently changed, never the rest.
- **Mail is never re-embedded** after a model change unless the mailbox is
  granted again with sorting and its backfill on, and vectors from the old model
  are never deleted. Memory vectors do catch up, ten a turn.
- **Memory embedding is not behind the search feature switch**, unlike mail. An
  operator who turns search off still pays for memory vectors.
- The `list` action of the memory tool has no upper bound and stamps everything
  it returns, which reorders the next prompt.
