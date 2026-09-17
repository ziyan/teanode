# Memory that learns the person: a graph with a hierarchy, written without being asked, organized while idle

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file.

## Purpose / Big Picture

The agent is meant to know the person. Today it does not, and the evidence is
plain: on the maintainer's own server, after five days, 456 turns and 313 tool
calls, the agent holds two memories, and one of them is a test sentence about
a neighbour's boat written twice. Every conversation about the house, the
cameras, the repositories, the chat rooms and the people in them left nothing
behind. The model was asked to remember "whenever a turn teaches you
something lasting", and a model asked to do a thing at its own discretion
does it three times in five days.

After this plan, the agent keeps a **graph** of what it knows about the
person — people, projects, places, things, preferences, decisions — with a
**hierarchy** over it so the top of the graph fits in every prompt, and every
fact carrying where it came from. The graph is **written without the model
being asked**: a short run after each turn reads what was said and files it.
It is **read without the model being asked**: each turn's words find the pages
and facts they touch, by words and by meaning, and put them in front of the
model. It grows from more than conversations: the person points it at a
repository on their computer, a Confluence space, a chat archive, a web
site, their own sent mail, and an ingestion job keeps a copy the agent can
search and cite. And while nobody is talking to it, it **dreams**: it works
through what arrived, folds it into the pages, merges what it has twice,
files what has no home, and writes down what it did so the person can read it
over coffee.

The end the maintainer named is that the agent could stand in for them: know
what they know, write as they write, decide as they would. This plan gets the
knowing and the organizing right and lays the writing-as-them on top of it,
and it is honest that the last of those is bounded by what the person has
allowed the agent to do — an outward act still asks.

You can see it working like this. Talk to the agent about a colleague for two
turns, then open a new conversation and ask "what did I say about Alice's
project?" — it answers from the page `people/alice-chen` without searching,
and the page names the conversation it learned from. Point it at
`~/projects/teanode` on an attached computer and ask, the next morning, "where
do we compute cosine similarity?" — it answers with the file and cites it.
Open the agent page and read what it dreamed last night.

Also in scope, because the graph cannot scale without it: **pgvector**, used
when the database has it and never required. The compose file ships an image
that has it; a database that lacks it ranks in Go as before, over a bounded
candidate set, exactly as `docs/decisions/20260910-embeddings-without-pgvector.md`
anticipated.

## Progress

- [x] (2026-09-15 16:30Z) Evidence gathered from root@server; plan written.
- [x] (2026-09-15 17:10Z) The three open calls settled by the maintainer:
      pgvector image is the compose default; person nodes bind to contacts;
      the model may add sources, asking first.
- [x] (2026-09-15 17:30Z) Added at the maintainer's ask: the person picks
      the contact that is them (`user.contact_id`); `self` binds to it.
- [x] (2026-09-15 20:30Z) Reviewed the plan against measured numbers
      (630 k chunks, 150 M tokens, a 4-vCPU server at PostgreSQL defaults):
      fourteen findings, each folded back into the plan; see Review.
- [x] (2026-09-15 19:40Z) Read the maintainer's hand-built work survey
      (`personal/mujin`, 2015–2026) and generalized its pipeline into the
      plan: `period` nodes, a digest function, a timeline dream phase,
      two dates on a fact, time-aware recall, `forge` and `journal`
      sources, bootstrapping from dated notes; and the scan-model tiering.
- [x] (2026-09-15 19:00Z) Traced "who did the payload angle deviation
      feature" through gen7 and the chat export: the answer is
      assembled from chat, a symbol and git, never read from a profile.
      Added commits as documents, the symbol index, learning at the point
      of use, and the chat-unit design for `~/chat-archive`
      (1.96 M posts → ~250 k units). Milestone 4 carries it.
- [x] (2026-09-15 18:20Z) Measured gen7's home directory and designed the
      computer source around it: git-aware scan on the machine, text
      sniffing, a secret filter before the socket, repository profiles as
      the first facts, people only from the person's own history, the
      hierarchy mirroring the directory, sensitive folders opt-in, 512-wide
      chunk vectors, git deltas. Milestone 4 carries it.
- [x] (2026-09-15 20:50Z) Milestones 0-4 built and deployed on root@server
      as `teanode:memory`; migrations 0065-0072 applied. The graph is
      filing by itself (21 pages, 30 facts from real conversations), and
      a first knowledge source is reading a 93 MB checkout.
- [x] (2026-09-15 21:20Z) The four things the maintainer asked for after
      seeing Dream-Memory and the sleep papers, all built:
      **edges carry meaning** (migration 0070; `Phrase`/`Sentence`, and a
      stale link is said in the past tense); **decay** (`internal/agent/
      decay.go`, per-kind half-lives, lifted by use, folded into recall);
      **duplicate detection at creation** (`internal/agent/duplicate.go`:
      by path, then by name or alias within the parent, then by meaning
      above 0.94 with the kind matching, so a second "Portal" page is
      never made); **versioning** (migration 0071; every write files a
      revision with who made it and what it moved, and a page's history
      is on its page, in `teanode agent memory history`, and in the API).
- [x] (2026-09-15 21:40Z) The night split into two halves
      (`internal/agent/dream_stages.go`, migration 0072): the quiet half
      reweights links by what was used together, downscales everything
      and retires what falls under a threshold the graph itself sets; the
      generative half walks the graph and asks whether two pages that are
      connected but not adjacent have a real relation, writing a typed
      link with a sentence on it when they do. Then rehearsal: the
      questions tomorrow is likely to bring, asked of memory alone, with
      the ones it cannot answer written down as gaps.
- [ ] Milestone 0: pgvector spike, then the vector store behind one interface.
- [ ] Milestone 1: the graph — nodes, facts, edges, paths; the `memory` tool
      rewritten around paths; the index in the prompt; old memories migrated.
- [ ] Milestone 2: the `remember` job — written after every turn, without the
      model being asked; "Learned today" on the agent page.
- [ ] Milestone 3: recall — hybrid search over the graph in every turn, by
      words and by meaning, under a token budget; the word-first gate removed.
- [ ] Milestone 4: knowledge sources — documents and chunks from an attached
      computer, a skill, a web address, and sent mail; the `ingest` job; the
      `knowledge` tool.
- [ ] Milestone 5: dreaming — the `dream` job and its phases; the dream log.
- [ ] Milestone 6: acting as the person — the `self` page, exemplars from
      their own sent mail at reply time, voice proposals from dreaming.

## Surprises & Discoveries

- Observation: the agent on root@server almost never writes memory, and the
  writes it made were a test.
  Evidence (2026-09-15, `agent_message` on root@server, 2026-09-10 to
  2026-09-15):

      memories|2|2                    -- two memories, both with vectors
      tool calls by name: terminal 37, tool_search 34, unifi 25, browser 21,
        filesystem 15, mail_read 15, mail_search 14, memory 12, shell 12 ...
      memory calls: 8 search, 3 add, 1 update, 1 delete
      the adds: "CI failures are not priority" (a triage rule), and
        "Neighbour's boat" twice (the twin test), later merged.

  Over the same days the person talked about Home Assistant, UniFi cameras,
  Homebridge, Mattermost channels, GitHub pull requests and a Confluence
  space. None of it was kept. The embedding model was configured throughout
  (`openai:text-embedding-3-small`), so this is not the meaning path being
  absent; it is the model not being asked to write.

- Observation: the memory tool's reads happen, its writes do not. Eight of the
  twelve calls were searches, several with long word lists ("keep an eye on
  watch monitoring reminder", limit 50). The model searches when it suspects
  it should know something, and finds nothing because nothing was written.
  Reading is not the broken half.

- Observation: the person already reaches Confluence, Mattermost and GitHub
  through installed skills (`skill__confluence__*`, `skill__mattermost__*`,
  `skill__github__*`), with their own secrets. Ingestion does not need new
  connectors; it needs a way to call what a skill already declares on a
  schedule and file what comes back.

- Observation: the recall path requires a word of four letters or more before
  the meaning search runs (`recallWords` in `internal/agent/memory.go`), so
  "who is he?" recalls nothing by meaning either. Recorded in
  `docs/subsystems/memory.md` as a caveat; fixed in milestone 3.

- Observation, from the maintainer looking at the result: "some of them
  are not very useful memory". Twenty-eight of a hundred and ten facts —
  twenty-two per cent — said no more than that their page existed:
  "Formatting is a project or work channel.", "The documentation work
  channel had activity in September 2026." They came from the coarse
  digest, whose instruction was "file only what a title plainly
  establishes", and a title plainly establishes only existence. The
  summaries the consolidate pass wrote over them were worse: "This page
  concerns Formatting, which is currently treated as a project or work
  channel. That classification gives Ziyan the relevant context for
  understanding how Formatting is organized."

  Three changes. The coarse instruction now says a title is enough to
  make a page and almost never enough to make a fact. The consolidate
  prompt is told an empty opening is a good answer, and the code accepts
  one. And `saysSomethingNew` refuses the shape in code, because a prompt
  is a request and the same request will be made of a different model
  next year.

- The maintainer's own suggestion, which is the part that generalizes:
  "node version can help with rewriting and fixing memory created by
  older version of teanode". Every page, fact and revision now carries
  the build that wrote it (migration 0073), and the first thing a night
  does is offer everything an older build wrote to the rules as they
  stand. On the real graph the first such pass struck 32 lines. It costs
  no model call, it is recorded in each page's history with the words the
  line used to say, and it means a rule written today reaches a graph
  filled last month.

- The maintainer's own answer to the last of it: their account address is
  the one they sign in with (the address they sign in with) and their commits are
  authored as three others, none of which the agent knew. `ownAddresses`
  matched nothing, so 358 facts were filed about their checkouts and one
  link was drawn, and their career timeline was empty. They chose to put
  all three git addresses on their card and mark it as themselves,
  including the work one.

  Three things came out of it. The card could not be marked at all:
  `user.contact_id` and `agent_node.contact_id` were `varchar(32)` to fit
  this program's own identifiers, and a card synchronized from a phone
  carries the phone's — a 36-character UUID. The write was refused at the
  column, the refusal poisoned the transaction, and the dashboard showed
  an HTTP 500 (migration 0075). A readme was cut at its 1200th *byte*,
  which lands inside a character and makes PostgreSQL refuse the whole
  statement — the same cut existed in four other places that write to the
  database. And a source now keeps the commit addresses it could not
  place (migration 0076) and says so, on the page and on the command
  line, because the failure was otherwise total and silent.

- The maintainer: "why $47.72 of $100.00 today? I only see 6.22 USD on
  the provider's usage page". That page showed 304.7 M embedding
  tokens, which at the real $0.02 per million is $6.09. This program
  names a model used at a width "text-embedding-3-small@512" so that
  vectors of different widths stay apart, and the price lookup matched
  the whole name against the list -- so it missed, fell back to the
  provider's default rate for chat input ($0.15), and charged the same
  tokens seven and a half times over: 304.7 M × 0.15 = $45.70. The token
  counts themselves were right (cached tokens are taken out of the
  prompt count on every path). Costs are computed from stored token
  counts at read time, so the dashboard corrected itself on deploy.

- Observation, and the reason the graph had no edges at all: a
  repository inside the scanned tree had its profile computed on the
  machine and then thrown away. The profile is keyed by the checkout's
  directory, and the entry meant to carry it was found by looking that
  key up among the *file* paths — a directory is never one of those. Only
  a source whose own root was a checkout ever worked, which is why
  `projects/personal` had three facts and the forty projects under
  `~/projects` and `~/mujin` had none. Every checkout found is now
  offered as its own entry.

  Two smaller ones in the same area: a repository was only filed when the
  file carrying its profile had *changed*, so a checkout whose tree was
  untouched never had its facts refreshed; and `tickAt` returned before
  the queueing passes when every slot was busy, so during a long ingest
  nothing else was ever queued.

- Measured, once the corpus was real: finding the passages that still
  need a vector cost a quarter of a second a time and got worse every
  hour. It was a LEFT JOIN against the vector table with an IS NULL test,
  which PostgreSQL answers with a hash anti-join over both tables in
  full: 474,000 passages against 422,000 vectors, ninety thousand buffers
  touched, to return a hundred rows -- and it runs once per batch of a
  hundred. The first ingest slowed to a crawl exactly as it got large,
  which is the moment it must not.

  Two changes, and the second mattered more than the first. The model a
  passage was embedded by is written on the passage (migration 0074), so
  the question is a filter on one table. And the `ORDER BY created_at`
  went: every passage gets a vector eventually and nothing reads them in
  between, so the order was never a property anybody had -- but it forced
  the whole table to be sorted before a hundred rows could come back.
  **250ms to 5.5ms**, and it now stops as soon as it has its hundred
  rather than getting slower as the corpus grows.

- Observation, under the real load: `deadlock detected (SQLSTATE 40P01)`
  writing passage vectors. Two ingest runs happen at once, each writes a
  hundred vectors in one transaction, and both ask for "chunks with no
  vector" and get overlapping answers — so two transactions took the same
  row locks in different orders. PostgreSQL killed one and the batch was
  lost. Batches are written in chunk-identifier order now, so the second
  waits instead. The log line on its own gives no hint of what to do,
  which is why it is written down here.

- Observation: a source that finished reading kept its cursor, so every
  later run resumed from the last path it had seen and found nothing
  before it. A file added anywhere earlier in the tree was never noticed
  again. The cursor is cleared at the end of a pass now; starting over is
  cheap, because the server sends the hash of everything it holds and the
  program leaves out whatever still matches.

- Observation, three times in one afternoon: `UpdateAgent` writes its
  columns by name, and a field missing from that list is written nowhere
  while the mutation succeeds and returns the value the caller asked for.
  It cost the dream window (a night nobody could move) and then
  `dreamed_at` — so the nightly run never recorded that it had run, the
  "not more than once every six hours" rule read a nil, and a second
  night started five minutes after the first finished, for ever. The
  general shape is that an explicit column list and a struct drift apart
  silently; the specific defence is a test that reads a field back after
  writing it, which is now `TestAgentTheNightIsSaved`.

- Observation: the graph had 42 pages, 66 facts and **zero edges**. Both
  filing prompts can emit links and neither was producing any — the
  digest prompt showed `"links": []` as its example, which teaches a
  model to emit nothing. With no edges there is nothing for the walk to
  follow and nothing for the quiet half to reweight, so the whole
  generative half of the night was running against an empty graph and
  correctly reporting that it had found nothing. Fixed three ways: the
  digest prompt now shows a real link and says when to draw one; a link
  carries the sentence that justifies it through to the edge (the field
  was in the prompt and not in the struct, so it was parsed and dropped);
  and the first edges are now drawn with no model at all, from git —
  `self works_on <project>`, with the commit span as the note, because
  git already says who committed and when.

- Observation: a worker with every slot busy stopped noticing that
  anything else was due. `tickAt` checked for a free slot and returned
  before the queueing passes, so while the first ingest saturated both
  slots nothing was filed from a conversation, no schedule ran and no
  night happened — for as long as the backlog lasted, which on a first
  ingest is days. Queueing writes rows and takes no slot; the capacity
  check belongs after it, just before claiming. Found by waiting half an
  hour for a night that could not come.

- Measured, on root@server with `text-embedding-3-small` at 512
  dimensions, what two facts on one page actually score:

      0.92+  three wordings of "the boiler was serviced in March by the
             Elm Street plumber" — all three folded into one at the write
      0.83   "Kittiwake is the neighbour's sailing boat" against
             "Kittiwake belongs to the neighbour next door", which a
             person would call one fact and the floor does not
      0.41 to 0.54  the boiler, the roof and the recycling: three facts
             about one house that are not each other

  So the floor of 0.92 is conservative and the gap below it is wide. Left
  where it is on purpose: a wrong merge loses something, a missed merge
  leaves a duplicate that the nightly consolidate still catches, and that
  one asks a model which facts say the same thing rather than a cosine.
  The two nets are meant to be different.

- Observation: a fact written through the API or the command line was
  never compared with what the page already said — only the agent's two
  writers checked. Folded into one exported helper now, called by both.
  While proving it, `sharesAName` turned out to reject the commonest
  duplicate of all: two sentences that both open with the subject's own
  name share no *mid-sentence* name, so "Kittiwake is..." and "Kittiwake
  belongs..." were never candidates. The page's own name is now removed
  from both sides rather than counted on either, which leaves what the
  two sentences actually disagree about.

- Observation: the nightly run's first phase had never read anything.
  `ListAgentDocumentsToDigest` is `WHERE "agent_id" = ? AND NOT
  ("metadata" ? 'digested')`, and `?` is how a parameter is written — so
  the driver read the JSONB existence operator as a placeholder and
  substituted the limit into it, producing `NOT ("metadata" 400
  'digested')` and a syntax error every time. The count beside it
  survived only because it had no second argument to take. `jsonb_exists`
  now, and the end-to-end night test is what found it: nothing else
  looked at the phase's return value.

- Observation: every question phrased as a sentence found nothing by
  words. The searches index with the `simple` dictionary because stemming
  a graph of names and paths does more harm than good, but `simple` drops
  no stop words, and `plainto_tsquery` joins what it finds with AND — so
  "what is the portal?" asked for a row holding *what*, *is*, *the* and
  *portal*. Joining with OR instead turned out worse: a summary
  containing *the* matched everything, and a search that always answers
  is one nobody can tell is broken. Both were needed: drop the words a
  question is made of (`SearchText`), then OR what is left and let
  `ts_rank` sort it (`AnyWord`).

- Observation: a source with more to read waited for its next cron slot,
  so a first pass moved eight pages a day — about twenty megabytes — and
  a home directory would have taken weeks. The cron line is how often to
  *look* for new work, not how fast to get through a backlog.

- Observation: a source whose computer was not attached had its "there is
  more to read" flag cleared and was put an hour out, so every restart of
  the server stalled the first pass for an hour. The flag is kept now,
  and a source with more to read is due whatever its next run says, so
  the work resumes the moment the computer is back.

- Observation: a commit was filed under its file list rather than its
  hash. `--name-only` prints a commit's files *after* its format, and the
  record separator was at the end of the format, so every record after
  the first opened with the previous commit's paths — and the field the
  parse read as the hash was twenty-one of them. PostgreSQL refused it at
  64 characters, which is the only reason anybody found out.

- Observation: a scan page was bounded by entry count (256) and not by
  bytes. On the first real ingest — a 93 MB checkout — 256 source files
  came to tens of megabytes, the websocket closed with 1009 ("message too
  big"), and the source sat at `waiting` saying only that the computer had
  detached. Counting entries bounds nothing a socket cares about. Fixed
  with `scanPageBytes` (3 MiB) in all three scanners and a read limit of
  its own on the computer socket, which had been sharing the 1 MiB
  GraphQL body limit. `TestScanPageIsBoundedByBytes` covers it.

- Observation: the computer program and the server disagreed about the
  protocol version. `internal/computer.Protocol` was raised to 2 for
  `scan` while `apigraph.computerProtocol` stayed at 1, so a program new
  enough to scan was refused by a server new enough to ask. The server now
  takes the constant from the program's package, so the two cannot drift.

- Observation: every string added for this work used `{{name}}` for its
  placeholders, and this dashboard's interpolation takes `{name}`. The
  page read "…what not to keep. {22} pages." Twenty-four strings across
  three locales; the two in `editor.*` really do mean the braces, because
  they describe the mail template syntax. Worth knowing that nothing
  catches this: the catalogues agreed, the types checked, and the lint
  passed.

- Measured, on root@server, partway through the first full ingest of
  ~/projects (9.6 GB), ~/mujin (85 GB), ~/chat-archive (8.3 GB) and
  ~/Documents:

      20,037 documents, 53,589 passages, 32,258 vectors so far
      agent_chunk 180 MB, agent_chunk_vector 176 MB, agent_document 16 MB
      the whole database 417 MB, from 60 MB before
      embeddings: 19.4 M tokens at 512 dimensions, $2.91

  The vectors cost about as much space as the text they point at, which
  is what 512 dimensions was chosen for — at 1536 they would have been
  three times the text. Reading is cheap and paced; embedding is the
  bottleneck and the only part with a bill.

## Decision Log

- Decision: the model addresses memory by **path**, never by id.
  Rationale: `01m2947xwnq990p3dk3kqd89f1` is an address a model copies wrongly
  and never remembers between rounds; `people/alice-chen` is one it can
  guess, and a guess that lands on a page is a read that would otherwise
  have been a search. The id stays in the database and the API.
  Date/Author: 2026-09-15, plan author.

- Decision: writing memory is a **job after the turn**, not a request to the
  model during it.
  Rationale: the evidence above. A model doing the person's actual task does
  not stop to file what it learned, and a prompt that begs harder is not a
  fix. A separate run whose only job is filing, with the fast model, is
  cheap and does the thing every time.
  Date/Author: 2026-09-15, plan author.

- Decision: the graph has three tables — `agent_node`, `agent_fact`,
  `agent_edge` — and the hierarchy is the node's path plus `part_of` edges,
  not a separate tree table.
  Rationale: a path is what the model reads and writes; an edge is what a
  query walks. Both say the same thing, and the path is derived from the
  `part_of` chain when a node moves. One table for "a fact" rather than
  properties on the node, because a fact has its own evidence, its own date,
  its own vector and can be superseded on its own.
  Date/Author: 2026-09-15, plan author.

- Decision: a node of kind `person` is bound to a **contact** in the address
  book, and making one makes the contact.
  Rationale: `docs/decisions/20260913-the-address-book-is-the-only-list-of-people.md`.
  The page about Alice is the agent's; the row that says Alice exists is the
  address book's. A name with no address is a valid vCard. The cost — the
  address book gains everyone the agent learns about — is accepted, and the
  Contacts page marks such an entry as "known to your agent" so the person
  can tell it from one they typed.
  Date/Author: 2026-09-15, plan author. Confirmed by the maintainer the
  same day; the alternative — person nodes that only link to a contact when
  one exists — was two lists of people again.

- Decision: the person picks the contact that is **them**, and the `self`
  node binds to it.
  Rationale: the agent knows the person's display name and nothing else
  about them that is not in a memory. A "me" card is a thing every address
  book already has a shape for — macOS Contacts calls it My Card — and it
  carries exactly what the `self` page needs first: their own addresses (so
  "is this addressed to me" and "is this one I sent" have an answer that is
  not a guess from mailbox aliases), telephone, organization, title,
  birthday. It is kept on the **user** row (`user.contact_id`), not the
  agent row, because it is true about the person whether or not they have
  an agent, and the composer and CardDAV clients can use it. The Contacts
  page gets "This is me" on a contact; the agent page shows which contact
  is you; `teanode contact me <contact-id>` sets it. Where none is chosen,
  the agent's first `remember` run offers to make one from the account's
  name and the addresses of the mailboxes it was granted — a proposal, as
  everything the agent writes about the person is. The `self` page's first
  facts are the card's fields, with evidence kind `contact`, and they are
  re-read whenever the card changes. The card's addresses are also what
  attributes a commit to the person: gen7's repositories carry ten of
  theirs (one of their own addresses, one of their own addresses, one of their own addresses,
  one of their own addresses, the address they sign in with and five older ones),
  and a scan that meets one the card lacks offers it as an addition.
  Date/Author: 2026-09-15, asked for by the maintainer.

- Decision: pgvector is **used when present, never required**, and the
  compose file's PostgreSQL image becomes `pgvector/pgvector:pg17`.
  Rationale: the image is stock PostgreSQL 17 with one extension added; the
  data directory format is the same, so an existing deployment that changes
  the image line keeps its data. A deployment that brought its own database
  loses nothing: without the extension the server ranks in Go as it does
  today. This supersedes part of
  `docs/decisions/20260910-embeddings-without-pgvector.md` and a new
  decision record says so.
  Date/Author: 2026-09-15, plan author. Confirmed by the maintainer the
  same day: the compose default is the pgvector image.

- Decision: the index is built over the `real[]` column by an **expression
  index**, `USING hnsw (("vector"::vector(N)) vector_cosine_ops) WHERE
  "vector_model" = '<model>'`, created by the server at start when the
  extension exists, not by a migration.
  Rationale: a migration must succeed on a database without the extension,
  and the schema does not change: the column stays `real[]`, readable by the
  Go path. The dimension `N` differs by model, so there is one partial index
  per model actually configured, and the query casts the same way so the
  planner uses it. Milestone 0 proves this on a real database before
  anything depends on it.
  Date/Author: 2026-09-15, plan author.

- Decision: bulk understanding runs on a **scan model**, configured apart
  from the conversation's, and everything that can be done without a model
  is.
  Rationale: the maintainer's words: "we can use super cheap model for
  scanning, understanding and embedding". The work divides into three
  tiers. Tier 0, no model: manifests, profiles, symbols, digests,
  aggregates, twin detection by cosine, fork attribution, noise filters.
  Tier 1, the scan model (`agent.models.scan`, defaulting to `fast`): the
  remember job, chat-unit and document summaries where a page needs one,
  the timeline narrative, consolidation, organizing proposals. Tier 2, the
  embedding model at 512 dimensions for chunks. The default model is used
  only in the person's own turns. Every tier-1 call is bounded and
  batched, and the dream's share of the budget caps the lot.
  Date/Author: 2026-09-15, decided by the maintainer.

- Decision: time is an axis of the graph — `period` nodes, `happened_at`
  on every fact, and a recency term in recall — generalized from the
  maintainer's hand-built work survey, not copied from it.
  Rationale: the survey proves the shape works for eleven years of one
  person's work; the server cannot ship one person's repository names,
  customer aliases or credentials. What ships is the digest as a function
  over documents, the narrative as a dream phase under the person's own
  writing rules, and dated-notes bootstrapping so the person's existing
  survey seeds the timeline.
  Date/Author: 2026-09-15, asked for by the maintainer.

- Decision: **a cap is pacing, never truncation** — batch bounds re-run
  from a cursor, pace bounds report their backlog, and a backlog too large
  to wait for is worked through at coarser resolution, never skipped.
  Rationale: the maintainer, reviewing the plan: "what if we exceed that?
  we just simply ignore? that's not a good strategy". The Review section
  has the three kinds of bound and what each does at its limit.
  Date/Author: 2026-09-15, decided by the maintainer.

- Decision: vectors live in their own tables (`agent_chunk_vector`,
  `agent_fact_vector`, `agent_node_vector`), one row per (row, model@width),
  `STORAGE EXTERNAL`; the compose file tunes PostgreSQL for them; the HNSW
  index is created before the bulk load; the no-pgvector path is
  words-then-rerank; embeddings have their own usage ledger; `scan` roots
  are held by the program; ingest jobs have instance affinity.
  Rationale: the Review section, finding by finding, against measured
  numbers.
  Date/Author: 2026-09-15, plan author, after the maintainer's review ask.

- Decision: the first load is embedded on the maintainer's GPU station,
  reached over SSH; the trickle afterwards uses the configured provider.
  Rationale: the maintainer's answer to "where do embeddings run": "ssh to
  station where I have a gpu, run the model there and send there for
  initial embedding, future ones use openai as configured". The provider
  configuration already takes a `baseUrl`, so the station runs an
  OpenAI-compatible endpoint and the server is pointed at it for the load.
  **Consequence the maintainer should weigh:** a local model and
  `text-embedding-3-small` are two vector spaces. The plan already keys
  every vector by `model@width`, so both can coexist; a search then embeds
  the query once per space present and fuses the lists by reciprocal rank —
  and the local model must stay reachable to embed queries for its space,
  or those rows fall back to words only. The cheaper shape is one model:
  the same small model on the station's GPU for the load and on gen7's CPU
  (a query is milliseconds; a night's trickle is minutes) for everything
  after. The plan supports both; which one is the maintainer's call and is
  recorded here as open until said.
  Date/Author: 2026-09-15, decided by the maintainer; consequence noted by
  the plan author.

- Decision: private channels and direct messages in the chat export are
  ingested like any other; `private` is kept on the document as metadata
  and changes nothing.
  Rationale: the maintainer: "they are not really private". The flag
  costs nothing and lets a later rule use it if one is ever wanted.
  Date/Author: 2026-09-15, decided by the maintainer.

- Decision: `scan` runs unattended; the **program** holds the list of
  roots it will scan, written on the person's own machine when they accept
  a source, and refuses a root outside it. `shell` and `filesystem` keep
  the present-only rule of the 2026-09-11 record, which a new record
  supersedes for this one read-only action.
  Rationale: the Review finding; confirmed by the maintainer.
  Date/Author: 2026-09-15, decided by the maintainer.

- Decision: dreaming **never deletes**. It merges, rewrites pages, marks a
  fact superseded or dormant, and proposes; the person and the `memory` tool
  delete.
  Rationale: a consolidation that can lose a fact is one the person cannot
  trust to run at night. Dormant facts leave the index and stay searchable.
  Date/Author: 2026-09-15, plan author.

- Decision: mail is folded into the graph by **dreaming**, not by a `remember`
  job per message.
  Rationale: a run per message would cost what triage costs again; the
  dream reads the summaries and insights triage already wrote, in bulk, under
  its own share of the budget.
  Date/Author: 2026-09-15, plan author.

- Decision: the model **may add, sync and remove** knowledge sources, and
  adding one is a **granting** call — the person is asked before it is made,
  whatever the model says.
  Rationale: the maintainer wants to say "keep an eye on the platform
  channel" in the drawer and have it happen, not open a form. A source is
  still a standing grant of reach — a directory on their machine, a
  credential's worth of pages — so it takes the same class as minting a
  token (`tools.RiskGranting`): the card names the computer or the skill and
  the path or query, and nothing is created until the person says yes.
  Removing one is destructive (its documents go) and asks too. Syncing is a
  write. The dashboard and command line keep the same operations.
  Date/Author: 2026-09-15, decided by the maintainer; the plan's first
  draft had sources person-managed and the tool read-only.

## Review: how this behaves in real life

Written 2026-09-15 before any code, against measured numbers, at the
maintainer's ask: scale, cost, space, performance, and what happens when a
bound is hit. Each finding ends with what the plan now says.

**The corpus, measured rather than estimated.** On gen7, applying the
sniff rule to the 45,254 candidate files: 44,522 are text under 512 KiB,
**327 MB, about 86 million tokens**; 53 are larger (first 64 KiB kept);
498 are binary by the NUL test; 71 are secret-named. The chat export,
cut by the thread-or-window rule with bots and system posts dropped
(364,140 of them): **128,993 threads and 140,021 windows, 269,014 units,
218 MB, about 54 million tokens, 321,114 chunks**. Commits in the person's
repositories: about 100,000 documents, the person's own 15,000 with a
bounded diff, perhaps 130,000 chunks. Altogether **about 630,000 chunks
and 150 million tokens to embed once**, then a few thousand chunks a night.
root@server has 4 vCPUs, 15 GB of memory, 33 GB free where the database
lives, a 91 MB database with 699 mail rows, and PostgreSQL at its defaults:
`shared_buffers` 128 MB, `maintenance_work_mem` 64 MB, `work_mem` 4 MB.

**Space.** 630,000 chunks × 2 KB of text is 1.3 GB in the table and about
0.5 GB in the GIN full-text index; 630,000 vectors × 512 × 4 bytes is
another 1.3 GB, and an HNSW index over them is 1.5 to 2 GB. Documents'
full text in object storage is about 550 MB. Call it **5 GB of database
and 0.6 GB of storage for the first load, then about 1 GB a year**. The
server can hold it; the maintainer's `pg_dump` before every deploy grows
from 36 MB to gigabytes unless vectors are left out, and they can be:
they are rebuilt for three dollars. A wrinkle found here: a 2 KB `real[]`
beside 2 KB of text pushes the row over PostgreSQL's TOAST threshold, so
the vector would be compressed (floats do not compress) and stored out of
line, and every scan would detoast it. *Plan:* vectors live in their own
table per width, `agent_chunk_vector (chunk_id, model, vector)`, with
`STORAGE EXTERNAL`; the chunk table stays small and hot; the dump
instructions in `docs/reference/deployment.md` exclude the vector tables.

**Without pgvector this does not work at this scale, and the plan said
otherwise.** Ranking 630,000 × 512 floats in Go is 1.3 GB read per query.
The "bounded candidate set" the Go path uses today is the newest few
thousand, which for a code corpus is meaningless. *Plan:* on a database
without the extension, knowledge search is **words first, then meaning
over the word hits**: the full-text query returns up to 2,000 chunks, and
those are re-ranked by cosine in Go (4 MB read). It finds everything the
words find, in a better order, and misses a paraphrase that shares no
word. The decision record says so plainly: pgvector is used when present,
and at this scale it is what makes meaning search over knowledge real;
memories and mail stay fine without it.

**Building the HNSW index needs memory the compose file does not give.**
pgvector builds an HNSW graph in `maintenance_work_mem`; at 630,000 × 512
that graph is about 1.5 GB, and at the default 64 MB pgvector falls back
to a build that is many times slower on 4 vCPUs. *Plan:* the compose file
sets `shared_buffers=2GB`, `maintenance_work_mem=2GB`, `work_mem=32MB`,
`random_page_cost=1.1` on the PostgreSQL service (there is 15 GB; the
mail server needs little); and the index is **created before the bulk
load**, so each chunk's insert maintains it (one to three milliseconds
each, twenty to thirty CPU-minutes spread over the first night) and no
large build ever happens. Query time is then milliseconds at the default
`ef_search`.

**The first load would have taken two days by the plan's own numbers.**
2,000 chunks per ingest run with a ten-minute requeue is 315 runs and 52
hours for 630,000 chunks. *Plan:* a run that finds more work requeues
itself **immediately** while budget remains, as `backfill` does, one run
per agent at a time; each run stays under the ten-minute job deadline
(2,000 chunks is about twenty embedding calls of a hundred). The first
load is then bounded by the provider's rate limit — 150 million tokens at
one to five million tokens a minute is thirty minutes to three hours —
and finishes in a night. Cost at `text-embedding-3-small`'s price is about
three dollars; twenty with `-large`.

**The daily token budget would have stopped it on the first night.**
root@server allows 2 million tokens per agent per day, and embeddings are
recorded as usage, so 150 million tokens is 75 days. *Plan:* embedding
usage is its own ledger — `agent.limits.embeddingTokensPerDay`, unlimited
by default — and the money caps (`dailyCostPerAgent`, `monthlyCostPerServer`)
still bind it. The token budget is a guard on chat models and stays one.

**A cap is pacing, never truncation.** The maintainer's point: a plan that
reads "400 items a night" and says nothing about the 401st is a plan that
loses things. Every bound in this document is now one of three kinds, and
none of them drops anything. *A batch bound* (2,000 chunks a run, 15 facts
a `remember`, 40 posts a window) says how much one transaction does; the
job re-runs on the remainder from its cursor. *A pace bound* (400 threads
a night) says how much full-resolution work a dream does; the cursor and
the priority order guarantee every item is reached, and the dream log says
what the backlog is and how many nights it is at this pace ("12,300
threads behind; 31 nights; raise the share, or let it coarsen"). *A
resolution bound* is what happens when the backlog is larger than the
person will wait for: the dream **coarsens rather than skips** — a
channel-day becomes one unit summarised in one call instead of its
threads one by one; a month of mail is digested from the thread summaries
triage already wrote (tier 0, complete by construction) rather than
message by message. Coverage is always total; only detail degrades, and
the log says which months were done coarse so a later dream, or the
person ("catch up on the PepsiCo channel"), can redo them fine. The person
can also raise `dreamShare` for a week. Nothing is ever marked done that
was not read.

**Prompt cost per turn is unchanged; prompt caching is at risk from one
detail.** The index is 1,500 tokens where the memory list was twenty
lines; the recall overlay is 1,200 tokens after the history where the
`<recalled>` block already is; recall's one embedding call is the one
`nearestMemories` makes today. But today's memory list is ordered by
`used_at` and `TouchAgentMemories` runs every turn, so the cacheable
prefix changes every turn. *Plan:* the index is ordered by `importance`,
which the dream recomputes once a night, and by nothing that moves during
the day; `used_at` is kept for the dream, not for ordering.

**Steady-state model cost is cents; the first nights are not.** `remember`
is about 15,000 tokens in and 500 out per quiet period; twenty a day is
300,000 tokens on the scan model. A dream is one timeline call, ten
digest batches, and consolidation of the pages touched — after the first
scan that is hundreds of pages, more than the 30% share of a 2-million
budget allows. *Plan:* consolidation is a pace bound too (100 pages a
night, most-used first); the first week's dreams drain it; steady state
is well under a million tokens a night.

**Answers filed at the point of use can be wrong.** A "who did that
feature" assembled from chat and git is an inference, and filing it as a
fact makes a guess permanent. *Plan:* such facts carry `confidence` below
one and a flag `inferred`; the page and the recall overlay say
"(inferred)"; nothing inferred is written on `self`; the person's "yes,
that's right" in a later turn, or the same answer reached again from
different evidence, promotes it.

**Twins by cosine alone merge the wrong things.** Two short facts about
two people can sit above 0.92. *Plan:* merging stays within one node (as
written) and additionally requires the two texts to share a proper noun
or a number when either has one.

**Full-text search does not read Chinese.** `to_tsvector('simple')` does
not segment CJK, so the words path is blind to the China-support team's
channels; the meaning path is not. Stock PostgreSQL has no segmenter.
*Plan:* accepted and written down; a chunk that is mostly CJK is marked
so that the tool's answer can say the match was by meaning only.

**The computer at night contradicts a decision record.**
`20260911-the-computer-is-a-device-the-person-runs.md` says the computer
is used only by a conversation the person is present in, with every call
confirmed. A nightly scan is neither. *Plan:* a new record supersedes that
part for one action. `scan` is read-only, and the program — not the server
— holds the list of roots it will scan, in its own configuration file,
written when the person accepts the source on their own machine (the
granting card is answered there when the computer is the thing being
granted). A server asking for a root outside that list is refused by the
program, so a server that is taken over cannot read `~/.ssh` through
`scan` the way it could through `shell` while the person is present.
`shell` and `filesystem` keep their rule unchanged.

**Jobs and sockets are on different instances.** Any instance claims any
job, but a computer's websocket is held by one. *Plan:* an `ingest` job for
a `computer` or `archive` source records the instance holding the socket
when it is queued, and the tick claims such a job only on that instance;
if the socket moves, the job is re-pointed on its next claim.

**Deleting a source is 300,000 rows.** *Plan:* removal is a job that
deletes in batches of 5,000 and lets autovacuum reclaim the HNSW entries;
the source shows "removing, 61% done" meanwhile.

**Latency inside a turn.** A `knowledge search` is a full-text query, one
embedding call and one indexed vector query: 300 to 500 ms, dominated by
the embedding round trip. Recall at turn start runs concurrently with the
prompt build. Neither is felt beside a model round.

**What is left that this review did not fix.** Windows of forty unrelated
posts make muddy vectors; they are searched, never distilled, and that is
accepted. The secret filter will flag base64 blobs and hashes in code; the
source log lists them and a per-source allowlist exists. Code embeddings
from a general text model are adequate for "find the file", not for
semantic code search; the symbol index carries the exact case. A person
away for a month comes back to a timeline page written from the tier-0
digest, complete and terse, which the next fine dream improves.

## Outcomes & Retrospective

### 2026-09-16, the audit of the small graph and the first full ingest

Audited with the small set (191 pages, 747 facts, 72 links) before the
full ingest, tested by asking the agent, and looped on the night until
the numbers converged. What it found, and what was done:

- **The agent uses the graph.** Seven of eight questions about the
  person's own projects, things and people were answered from memory,
  correctly, with checkout paths. The eighth was answered too, but the
  agent first tried to open a terminal to read a file it invented. Not
  yet addressed: a prompt-level nudge toward memory before tools on
  questions about the person's own history.
- **Openings were padding.** Fifteen `work/*` pages opened with "this
  project matters to Ziyan because they contributed to its development",
  written by the consolidation from a repository profile alone, and the
  first rewording of the prompt wrote it again. The prompt now says a
  profile is not an opening; the revise pass clears an opening made of
  those phrases (`saysNothingOpening`) once per build and makes the page
  due. After one night: zero padded openings, honest empty ones.
- **A readme's sentence was lost.** Ingest put it in the opening and
  nowhere else; the night writes openings from the facts alone and
  threw it away. It is a keyed fact now (`Its readme says: …`).
- **Facts said twice.** Twelve pages carried the same repository line
  twice, from a keyed pass that kept one fact per key and never struck
  a second under the same key. One per key now, and the night strikes
  any fact whose page already says the same words on a lower number.
- **Rehearsal found no gap in eight nights.** Any page or fact within a
  quarter's similarity counted as an answer. Now only a fact counts, and
  the model is shown the nearest five and asked whether they answer the
  question; the questions and verdicts are kept in the night's notes.
  The first night after: eight of eight were gaps, all about intentions
  and follow-ups, which repository profiles cannot answer -- honest,
  and the phase's point.
- **The night ran out of time.** Every job had ten minutes; reading four
  hundred chat-days took all of it and the phases after (revise, embed,
  rehearse) were cut, twice in a row. A night may take 45 minutes now,
  the reading stops at half of what is left, and the revise pass runs
  first because it asks no model.
- **The digest read code.** Four hundred Go files a night filed two
  facts, with a hundred thousand more behind them. Files written to be
  read come first now; documents under 160 bytes are marked read without
  a call (channel-days that are one person joining).
- **Twenty-eight empty pages**, a name and nothing else, from sources
  naming channels; removed after two days empty.
- **Old wording from a paused source** ("1 commits by 1 people, July
  2026 to July 2026") reworded in place by the revise pass, since no
  pass would say it again. The pass is paced at two thousand rows: every
  build shipped makes every row an older build's again.
- **Parity.** Links could only be made by the agent's tool. `LinkAgentNodes`
  and `UnlinkAgentNodes`, `agent memory link|unlink`, a tool `unlink`,
  and on the dashboard: move, link/unlink, pause/resume a source, run
  tonight now, and the night's questions. `agent dream now` exists
  because the night could not be started any other way without writing
  to the database by hand.

The full ingest, started 2026-09-16 00:00, turned up four more:

- **A restart stalled every pass for a quarter of an hour.** Jobs the
  replaced container held stayed "running" until the stale-claim rule
  put them back. An instance now releases its own claims at start-up.
- **A one-second detach put a pass down until its scheduled hour.** The
  detach mid-answer is a named error now and treated as the computer
  not being there: retry in five minutes.
- **Two big scans at once took the computer down every hundred seconds**
  -- or so it looked. The server now logs why a computer's socket ended,
  and the reason was `read limit exceeded`: the chat scanner appended a
  whole channel file's units before checking the page bound, and a
  support channel with years of posts was one answer of tens of
  megabytes. The scanner pages within a file now (`posts/x.jsonl#unit`
  cursors), one source reads a computer at a time, the daemon answers a
  panic in a request instead of dying, and the server takes a larger
  answer from an older program until it is rebuilt.
- **Pace.** `work` (17.9k documents) and `projects` (44.8k) completed a
  full pass in about forty minutes each. The chat export finished its
  first full pass at 07:18 on the 16th: 464,844 units from 890 of the 940
  channel files; the 50 left out are the monitoring and bot channels
  (the largest 426 MB), refused by the bot-channel rule or made of
  system posts. Pages of 2,048 chat units, sixteen pages a run, twenty
  seconds between runs, and the daemon keeping the last channel file it
  cut, took the rate from two thousand units a run to about four
  thousand a minute.

### 2026-09-16, the shape of a big graph

Asked whether a page of seventy facts scales: reads were bounded (twenty
facts a turn, sixty in the tool) but by number, oldest first, so the fact
filed last week never reached a prompt; and nothing divided a page. Now
recall and the tool take the liveliest facts first, the night divides a
page past forty facts into themed children (the PepsiCo page became six:
AGV operations, controllers, PLC, UI, vision, WES), pages merge
(`agent memory merge`, the tool's `merge`) -- three PepsiCo pages from
three channel names became one, and `people/ziyan` folded into `self`,
which a digest of the person's own threads is now routed to. The dashboard
gained a full-page graph explorer (pan, zoom, edges by weight, expand on
click, search) and a drill-down navigator that slides into any depth of
folder with a back button, replacing the three columns; facts page fifty
at a time. A night that marked four thousand documents read while the
model was unreachable led to two rules: a batch the model never answered
is not marked, and `agent dream reread --minutes N` puts back what a
night marked in that window.

### 2026-09-16, the person's own model

The scan work -- digest, consolidation, rehearsal, association, and now
the description of each checkout from its readme -- runs on a model of
the person's own: Qwen3.8-27B (IQ4_XS, llama.cpp) on their GPU machine,
reached by the server through two ssh forwards, declared as the
`station` provider at zero price. A 7B vision model was tried first and
copied the prompts' examples back as facts and as rehearsal questions;
the 27B files specific, dated, sourced facts from the person's own
threads (the PepsiCo Carlisle page gained eight in an hour). With the
model free, the night reads two thousand documents in batches of twenty,
`agent dream now --catch-up` keeps it running at every tick until nothing
waits, the request timeout is five minutes (the 27B answers a long prompt
in one to two), and a running night is no longer put back as stale at
fifteen minutes -- which had started a second night beside the first.
Chat is read only where the person was in the thread, three posts or
more; the rest stays searchable and is not counted as waiting. Facts a
digest files cite the document, not a conversation.

## Context and Orientation

TeaNode is a mail server in one Go binary with a React dashboard compiled in,
a PostgreSQL database, and an optional personal agent per account. This plan
is entirely about the agent. Read this section even if you know the
repository; it names every file the plan touches.

**An agent** is one row in `agent` per account (`internal/models/agent.go`),
made when the person turns it on. Everything it holds — memories,
conversations, schedules, jobs, usage — hangs off `agent.id`, and deleting the
account deletes it all. `docs/decisions/20260910-agents-belong-to-people.md`
says why the agent is the person's.

**A turn** is the person talking: `Agent.Ask` in `internal/agent/ask.go`
starts an `AskRun`, builds a prompt, calls the model in rounds, runs tools,
streams events. **A job** is work nobody is watching: a row in `agent_job`
(`internal/db/database_agent.go`, `agentJobModel`) with a kind, an agent, an
optional mailbox and a subject id; `Agent.tickAt` in `internal/agent/agent.go`
claims due jobs every five seconds and runs each under a ten-minute deadline.
The kinds are constants `AgentJobTriage`, `AgentJobEmbed`, `AgentJobResearch`
and so on in `internal/models/agent.go` around line 361, and each has a
handler `runXxx` in its own file (`triage.go`, `embed.go`, `research.go`).
Enqueuing is idempotent on (agent, kind, subject): `Agent.Enqueue(tx, kind,
agentId, mailboxId, subjectId)`. `docs/subsystems/jobs-and-schedules.md`
describes the queue, retries and the budget check `RequireBudget`
(`internal/agent/policy.go`).

**Memory today** is `agent_memory`: title, content of at most 4000
characters, tags, `applies_to` (which runs read it: `ask`, `triage`,
`research`, `reply`, `summaries` — `models.AgentAudiences` in
`internal/models/memory.go`), pinned, `used_at`, and a `vector real[]` with
`vector_model`. `internal/agent/memory.go` puts the top twenty into every
prompt (`memories()`, called from `systemPrompt` in `ask.go`) and, once a
turn, `recallForTurn` searches the turn's words by substring
(`RecallAgentMemories`) and by meaning (`nearestMemories` in
`memory_meaning.go`, cosine in Go over up to a thousand candidates) and
writes what it finds into the `<recalled>` overlay. The tool the model has is
`internal/agent/tools/memory/memory.go`, actions add, update, delete, get,
list, search, batch; it calls `run.NoteMemory` (interface `Remembering` in
`internal/agent/tools/remembering.go`) to vectorize a new memory and learn
of twins above 0.92 cosine. `docs/subsystems/memory.md` is the full account.

**Mail search by meaning** is the same shape in `internal/agent/embed.go` and
`internal/db/database_embedding.go`: table `mail_embedding` (migration
`0035_mail_embedding.sql`), one `real[]` per message per mailbox per model,
ranked in Go over the newest three thousand. `mail.search` is a `tsvector`
column written by `to_tsvector('simple', ...)` in `database_mail.go`, so
PostgreSQL full-text search is already in use here and is what "by words"
means in this plan.

**The embedding model** comes from `Registry.Embedding()` in
`internal/llm/registry.go`; only the OpenAI provider implements `Embedder`
(`internal/llm/provider.go`). `configuration.Agent.Models.Embedding` names it;
empty means no meaning search anywhere. `configuration.Agent.Models.Fast` is
the cheap model used by triage and compaction and is what every new job in
this plan uses.

**The prompt** is layered (`docs/subsystems/context.md`): identity, conduct,
house instructions, the situation, the person's instructions, memories,
tool guidance, deferred tool catalog; then the history; then one system
message of overlays (`<viewing>`, `<recalled>`, `<now>`…) rebuilt each round.
Layer 5, "memories", is what milestone 1 replaces with the index. Prompts are
Go templates in `internal/agent/prompts/` (`ask.txt`, `compact.txt`,
`triage.txt`…), rendered by `internal/agent/prompts.go`.

**Tools** are packages under `internal/agent/tools/<name>/` registering a
`*tools.Tool` in `init` (`internal/agent/tools/tool.go`). A tool reaches the
run through `tools.RunFrom(ctx)` — the `Run` interface in
`internal/agent/tools/context.go` gives it the owner, the agent, the
database, `Recall`, `Enqueue`, `MeaningSearch`. Every tool has a `Risk`
(read, write, destructive, outward, granting) and a `RiskOf` that can judge
the call rather than the tool. The convention is one tool per thing with an
`action` first argument (`docs/subsystems/agents.md`, "One tool per thing").

**Devices** (`docs/subsystems/devices.md`): `teanode computer start` is a
program the person runs on their own machine; it holds a websocket to the
server and answers `shell` and `filesystem` actions (read, list, search,
grep, fetch a whole file up to 32 MiB) as the person. It is attached per
person and only usable while attached. `internal/agent/computer.go` is the
server's side; `internal/computer/` the program. This is how a repository
on the person's laptop reaches the server: the server asks the computer for
the files, in bounded reads.

**Skills** (`docs/subsystems/skills.md`): signed files of declared tools —
an HTTP request, a shell command, or a workflow of them — installed by the
operator, with secrets per person. On root@server the person has skills for
Confluence, Mattermost, GitHub, Home Assistant and more. A skill's HTTP
steps run on the server through the same address guard as `web_fetch`.
`internal/agent/tools_skill.go` turns a skill into `*tools.Tool`s named
`skill__<skill>__<tool>`; `internal/skills/run.go` carries one out.

**The address book** is the only list of people
(`docs/decisions/20260913-the-address-book-is-the-only-list-of-people.md`):
`ListContacts`, `FindContactByAddress`, `PutContact` in
`internal/db/database_contact.go`, vCards under `internal/contacts`.

**Migrations** are numbered SQL pairs in `internal/db/migrations/`; the next
free number when this plan was written is `0065`. Every forward file needs a
`.reverse.sql`; both run in a transaction; `make test` applies them all to a
fresh database. `docs/coding/database-migrations.md` has the rules. Database
tests use `dbtest.AcquireDatabase(t)` and need a PostgreSQL reachable at
`TEANODE_TEST_DATABASE_HOST`; `make test` starts a container for the whole
suite, and for one package run

    docker run -d --rm --name teanode-test-db -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_USER=teanode -e POSTGRES_DB=teanode pgvector/pgvector:pg17
    export TEANODE_TEST_DATABASE_HOST=$(docker inspect --format '{{ range .NetworkSettings.Networks }}{{ .IPAddress }}{{ end }}' teanode-test-db)
    go test ./internal/db/... -run TestName

**The API** is a graph-shaped API under `internal/api/v1api/apigraph/`; the
agent's memory endpoints are `agent_memory.go` (queries `ListAgentMemories`,
mutations `SaveAgentMemory`, `DeleteAgentMemory`), sources `agent_source.go`.
The command line client mirrors it in `internal/cmd/agent_memory.go`
(`teanode agent memory list|add|remove`). The dashboard's agent page is
`web/src/pages/agent.tsx`; the drawer `web/src/components/agentDrawer.tsx`.

**Terms used below.** A *node* is a page in the graph: one person, project,
place, thing or topic, addressed by a *path* like `people/alice-chen`. A
*fact* is one sentence kept on a node, with its *evidence* — where it came
from — and an optional date. An *edge* joins two nodes with a named
relation. The *index* is the top of the hierarchy as a list of paths with one
line each, small enough for every prompt. A *document* is one thing
ingested from a source (a file, a page, a post, a sent message) and a
*chunk* is a slice of it with a vector. *Recall* is what the loop puts in
front of the model for a turn without being asked. *Dreaming* is the
idle-time job that consolidates.

## Plan of Work

### Milestone 0: pgvector, proven, then behind one interface

The goal is that ranking by meaning stops being bounded by how many rows the
server is willing to load into Go, without the schema changing and without
the extension becoming a requirement. At the end, a database with pgvector
answers "nearest memories" and "nearest chunks" with one indexed query; one
without answers as it does today; and nothing else in the server knows which.

**The spike first.** In the scratchpad, against a `pgvector/pgvector:pg17`
container, create a table shaped like `agent_memory` (`vector real[]`,
`vector_model text`), insert twenty thousand random 1536-wide rows under
model `m`, and run

    CREATE EXTENSION IF NOT EXISTS vector;
    CREATE INDEX memory_vector_m ON t USING hnsw ((("vector")::vector(1536)) vector_cosine_ops) WHERE "vector_model" = 'm';
    EXPLAIN ANALYZE SELECT id FROM t WHERE "vector_model" = 'm' ORDER BY ("vector")::vector(1536) <=> $1::vector(1536) LIMIT 5;

The plan proceeds only if the plan shows an index scan on the HNSW index and
the query takes single-digit milliseconds. If the expression index is
refused or ignored, the fallback is a shadow column `vector_indexed
vector(N)` maintained by the server on every write when the extension
exists; record which in the Decision Log. Also prove that `CREATE EXTENSION`
by the `teanode` role fails cleanly on `postgres:17` (no extension available)
and on a role without privilege, so the probe can treat both as "absent".

**Then the store.** Create `internal/db/database_vector.go` with

    // VectorSearch ranks rows by cosine against a query vector.
    type VectorOperation interface {
        // VectorIndexing says whether the database can rank vectors itself.
        VectorIndexing() bool
        // EnsureVectorIndex makes the partial HNSW index for one table,
        // column and model at a dimension, and is idempotent. A no-op
        // without the extension.
        EnsureVectorIndex(table, column, modelColumn, model string, dimension int) error
        // NearestAgentFacts, NearestAgentNodes, NearestKnowledgeChunks:
        // the ids nearest a query, best first, above a floor. Without the
        // extension each loads a bounded candidate set and ranks in Go.
        NearestAgentFacts(agentId, model string, query []float32, limit int, floor float64) ([]Scored, error)
        NearestAgentNodes(agentId, model string, query []float32, limit int, floor float64) ([]Scored, error)
        NearestKnowledgeChunks(agentId string, sourceIds []string, model string, query []float32, limit int, floor float64) ([]Scored, error)
    }
    type Scored struct{ ID string; Score float64 }

`VectorIndexing` is decided once at open, in `internal/db/database.go`, by
`CREATE EXTENSION IF NOT EXISTS vector` inside a savepoint that is rolled
back on error and logged at info level either way ("vector indexing: on" or
"vector indexing: off, ranking in the server"). The Go fallback moves the
existing `nearest` and `rankByCosine` from `internal/agent/memory_meaning.go`
and `embed.go` into `internal/db/vector_go.go` so there is one cosine. The
SQL path renders `<=>` with the cast and the model predicate; pgvector's
`<=>` is cosine *distance*, so the score is `1 - distance`. The dimension
comes from the first vector written for a model and is remembered in a new
table `vector_model` (`model`, `dimension`, `created_at`; migration `0065`);
the server calls `EnsureVectorIndex` for the configured embedding model at
start and whenever a vector of a new model is first written. Mail search
(`meaningSearch` in `embed.go`) switches to the same interface
(`NearestMailEmbeddings`), which lifts the three-thousand cap where the
extension exists.

Change the compose files: `deploy/docker-compose.yml`,
`docker-compose.dev.yml`, `docker-compose.test.yml` and the `Makefile`'s test
container from `postgres:17` to `pgvector/pgvector:pg17`. Add
`docs/decisions/20260915-pgvector-when-present-never-required.md` and a
"superseded in part" line to the 20260910 record. Extend `docs/reference/deployment.md`
with the image change and the note that an operator who keeps `postgres:17`
loses nothing but scale.

**Acceptance.** `go test ./internal/db/ -run TestVector` passes against both
images: the same test is run twice by the Makefile with `TEANODE_TEST_VECTOR=
on|off`, asserting that `VectorIndexing()` reports what the image has and
that the five nearest of a seeded set are the same five in the same order
on both. Start the server against each and read the log line.

### Milestone 1: the graph, and a tool the model can drive

The goal is a place for knowledge with a shape a model finds its way around:
paths, pages, facts with sources. At the end, `teanode agent memory index`
prints a tree, `teanode agent memory get people/alice-chen` prints a page,
the model's `memory` tool does the same by path, the prompt carries the
index, and the two old memories are facts on a page.

**Schema**, migration `0066_agent_graph.sql`:

    agent_node   id, agent_id, path (unique with agent_id), parent_id (nullable,
                 references agent_node), kind, name, aliases jsonb,
                 summary text (at most 8000 chars), contact_id (nullable),
                 pinned bool, importance real, dormant bool, used_at,
                 created_at, modified_at, vector real[], vector_model,
                 search tsvector
    agent_fact   id, agent_id, node_id (references agent_node, cascade),
                 number int (unique with node_id; the "#3" a person and a
                 model refer to), kind ('fact','preference','decision',
                 'event','howto'), text (at most 1000 chars),
                 happened_at (nullable), confidence real,
                 evidence jsonb, superseded_by (nullable), dormant bool,
                 used_at, created_at, modified_at, vector real[],
                 vector_model, search tsvector
    agent_edge   agent_id, from_id, to_id, relation, weight real,
                 evidence jsonb, created_at; primary key (from_id, to_id,
                 relation)

Node kinds: `self`, `person`, `organization`, `project`, `topic`, `place`,
`thing`, `folder`. Relations: `part_of`, `works_on`, `member_of`, `knows`,
`owns`, `uses`, `located_in`, `related_to`, `decided_in`. Evidence is a list
of `{kind, id, quote}` where kind is one of `conversation` (id is a message
id), `mail` (an item id), `document` (a chunk id, milestone 4), `person` (the
person said it in a dashboard edit). `search` is `to_tsvector('simple', name
|| aliases || summary)` for a node and of `text` for a fact, kept by the
write path, not a trigger, to match `database_mail.go`.

Every agent gets a root set on first use: `self` (kind self, "who you are"),
`people`, `projects`, `places`, `things`, `topics`, `notes` (all kind folder).
The same migration adds `user.contact_id` (nullable, no foreign key across
the ownership boundary is needed since contacts cascade with the user; the
reverse drops the column). `self.contact_id` is the user's `contact_id`,
read at prompt time rather than copied, so choosing a different card moves
the binding without a write to the graph. The `self` page's fields from the
card — name, addresses, telephone, organization, title, birthday — are
rendered into the page's first lines by `models.AgentNode.SelfLines(contact)`
rather than stored as facts, so they are never stale; facts on `self` are
what was *learned*, and start empty.
The data migration copies each `agent_memory` row into a fact under `notes`
with `evidence: [{kind: "memory", id: <old id>}]`, its tags into the fact
text's trailing line, `applies_to` kept as a fact attribute `audiences`
(jsonb, so triage and reply still read what was addressed to them). The
reverse migration deletes facts whose evidence kind is `memory` and drops the
three tables; `agent_memory` itself is left in place until a later release
removes it, so a downgrade is exact.

**Database layer**: `internal/db/database_graph.go` with `GraphOperation`:
`GetNodeByPath`, `ListChildren`, `ListIndex(agentId, budgetNodes)` (the
pinned, then by importance, then by use, excluding dormant), `PutNode`,
`MoveNode` (rewrites the path of the subtree), `DeleteNode`, `AddFact`,
`UpdateFact`, `SupersedeFact`, `ListFacts(nodeId)`, `SearchGraph(agentId,
words, limit)` (full-text over both tables, ranked by `ts_rank`), `PutEdge`,
`ListEdges(nodeId)`, `TouchNodes`, `TouchFacts`, the vector accessors from
milestone 0, `ListWithoutVector`. Models in `internal/models/graph.go` with
`Validate`. A path is lowercase ASCII words joined by `-`, segments joined
by `/`, at most 8 segments and 200 characters; `models.Slug(name)` makes one
from a name.

**The tool.** Rewrite `internal/agent/tools/memory/memory.go` around paths:

    action   index   the tree: path — name — one line, to a depth (2 by default)
             get     a page: summary, facts numbered, links, children
             search  words; answers pages and facts with their paths
             note    add a fact to a path; makes the node if it is missing
                     (kind required then); happened_at, kind optional
             page    rewrite a node's summary, or rename it
             link    from, to, relation
             move    a node to a new parent
             forget  a path, or a fact as path#number
             batch   up to 25 of the above

`RiskOf` keeps `index`, `get`, `search` as read. The tool's description is
rewritten to say what the model actually needs to know: "Addressed by path.
The index in your prompt is the top; `get` a path before saying you do not
know something about the person. You need not file what you learn — a run
after the turn does — but `note` anything they ask you to remember, and
correct a page that is wrong." The `<recalled>` overlay renders pages and
facts with their paths so a follow-up `get` is one copy away.

**The prompt.** Replace layer 5 in `internal/agent/prompts.go` and
`prompts/ask.txt` with the index: the `self` page's summary in full (this is
"who they are", and it is the one page that is always worth its tokens),
then the index to a budget of 1500 estimated tokens using the existing
token estimator in `internal/llm/tokens.go`, most important first, dormant
never. Jobs that cannot ask (`triage`, `reply`, `summarize`, `research`)
get the same index plus the facts whose `audiences` name them, replacing
`memoryLines`.

**Me.** The Contacts page gains "This is me" on a contact's row and a
"You" marker on the chosen one; the agent page's top says "You are
<contact>" with a link, or "Choose the contact that is you" where none is.
`apigraph/contact.go` gains `SetMyContact(contactId)` and the user query
returns `contactId`; `teanode contact me <contact-id>` and `teanode contact
me --clear`. Where none is chosen, the prompt's `self` page opens with the
account's name and the sentence "No contact is marked as you; offer to make
one when it comes up", and the memory tool's `note self …` answers with the
same hint once per conversation.

**API and command line.** `apigraph/agent_memory.go` gains
`AgentGraphIndex`, `AgentGraphNode(path)`, `SearchAgentGraph`, `SaveAgentNode`,
`SaveAgentFact`, `DeleteAgentNode`, `DeleteAgentFact`, `MoveAgentNode`; the
old memory queries keep working over `notes` for one release and are marked
deprecated in their doc comments. `internal/cmd/agent_memory.go` gains
`index`, `get`, `note`, `page`, `link`, `move`, `forget`. The dashboard's
agent page shows the tree with pages and facts, each fact with its evidence
as a link to the message, mail or document; editing a summary or striking a
fact writes evidence `{kind: "person"}`.

**Acceptance.** `go test ./internal/db/ -run TestGraph` and
`./internal/agent/tools/memory/` pass. Mark a contact as you on the Contacts
page; `teanode agent memory get self` prints its name, addresses and
organization on the first lines, and "what's my work address?" in the drawer
is answered without a tool call. From a terminal:
`teanode agent memory note people/alice-chen --kind person "Alice runs the
platform team"` then `teanode agent memory get people/alice-chen` prints the
page with fact `#1` and its evidence `person`. In the drawer, "what do you
know about Alice?" is answered without a tool call because the index carried
`people/alice-chen — Alice Chen: runs the platform team`.

### Milestone 2: written without being asked

The goal is that a turn leaves something behind every time it taught
something. At the end, after any conversation the agent page shows "Learned
today" with the facts filed from it, each with the sentence it came from,
and the model was never asked to do it.

**The job.** New kind `AgentJobRemember`, subject the conversation id.
`AskRun` enqueues it when a turn ends in a `main` or `named` conversation
(in the transaction that stores the last message; see the end of the ask
loop in `ask.go`, where `describe` is queued for quiet conversations — same
place). Idempotent enqueue means a busy conversation has one pending job at
a time; the handler reads everything after `remembered_through` (a new
column on `agent_conversation`, migration `0067`) and advances it in the same
transaction that writes the facts, so a crash re-reads rather than skips.

**The prompt**, `prompts/remember.txt`, rendered with: the index (paths and
one line each, to 3000 tokens), the pages the turn's own recall already
touched (so an existing fact is updated rather than duplicated), and the new
messages with tool results cut to 1500 characters each. It asks for JSON:

    {"facts": [{"path": "people/alice-chen", "node_kind": "person",
                "node_name": "Alice Chen", "kind": "fact",
                "text": "Runs the platform team at Acme.",
                "happened_at": null, "quote": "Alice, who runs platform at Acme, ...",
                "message_id": "..."}],
     "links": [{"from": "people/alice-chen", "to": "projects/acme-migration", "relation": "works_on"}],
     "self":  [{"kind": "preference", "text": "Wants CI failure mail sorted as normal, not priority.", "quote": "...", "message_id": "..."}],
     "supersedes": [{"path": "things/car", "number": 2, "text": "..."}]}

with the rules the compaction prompt already uses: only the person's own
words make a `preference` or a `decision`; a tool's answer, a page or a mail
is evidence for a `fact` at lower confidence and never for what the person
wants; nothing about the agent itself, nothing already on the page, no
transient state ("is waiting for a build"); a person is filed under
`people/` by a slug of their name, a project under `projects/`, and where
the model cannot tell the kind it uses `notes`. Fifteen facts at most per
run. `llm.Structured` (`internal/llm/structured.go`) parses the answer.

**Writing.** For each fact: resolve or create the node (a `person` node
creates the contact per the Decision Log, through `PutContact`), embed the
text, ask `NearestAgentFacts` on that node's facts; a twin at or above 0.92
becomes an update of the twin with the new evidence appended; otherwise
`AddFact`. `self` entries go on the `self` node. `supersedes` marks the
named fact `superseded_by` the new one. Then `TouchNodes`. Usage is recorded
as kind `remember` against the agent. The run leaves a transcript of kind
`run` like every job, so "why did it think that?" has an answer.

**What the person sees.** The agent page gains "Learned today": facts by
day, newest first, each with its page, its quote and a strike (delete) and
a "wrong page" move. `teanode agent memory learned --since 1d` prints the
same. A struck fact writes `agent_feedback` of kind `unlearned` with the
text, and the remember prompt is shown the last twenty of those under "do
not file things like these".

**Cost.** The fast model, one call, a few thousand tokens in and a few
hundred out per conversation-quiet-period. Bounded by `RequireBudget` like
any job; a deployment that wants it off has feature `agent.features.remember`.

**Acceptance.** A test in `internal/agent/` runs `runRemember` with a fake
provider returning a fixed answer and asserts the facts, the contact, the
twin merge and `remembered_through`. Live: say "my dentist is Dr Patel on
Elm Street, appointments are always Tuesdays" in the drawer, wait a minute,
open the agent page: `people/dr-patel` with two facts and the quote; the
Contacts page shows Dr Patel marked "known to your agent". Ask in a new
conversation "when are my dentist appointments?" and it answers from the
index without a tool call.

### Milestone 3: read without being asked

The goal is that the model is shown what the turn is about before it
answers, from the whole graph, by words and by meaning, and that a question
with no long word in it still recalls. At the end `recallForTurn` is replaced
and `docs/subsystems/memory.md`'s caveats about "a word first" and "ranking
by substring" are gone.

Replace `recallForTurn` in `internal/agent/memory.go`: the turn's text (and
the subject and sender of any referenced mail) becomes a `plainto_tsquery
('simple', ...)` over `agent_node.search` and `agent_fact.search`
(`SearchGraph`), and, where there is an embedding model, one embedding of
the same text ranked by `NearestAgentNodes` and `NearestAgentFacts`. Merge
the four lists by reciprocal rank fusion (score = sum of 1/(60 + rank)
across lists), which needs no tuning across the two kinds of score. Then
expand: for each node in the top five, its summary cut to 600 characters
and its top five facts by use; for each fact in the top ten whose node is
not already there, the fact and its path. Stop at 1200 estimated tokens.
Skip anything the index already carries. Write it into `<recalled>` as

    people/alice-chen — Alice Chen
      Runs the platform team at Acme. (#1, from a conversation on 2026-09-12)
      ...

and touch what was shown. Delete `recallWords`, `recallStopWords` and the
four-letter rule; the tsquery parser drops stop words itself, and the
meaning search needs no words at all. The `recalled` and `memoryNeighbours`
constants become one budget. Jobs get the same function with the audience
filter, so a reply run recalls the sender's page.

**Acceptance.** `TestRecallFindsThePageForAPronoun`: with `people/alice-chen`
in the graph and a previous turn about Alice, the turn "what does she work
on?" produces a `<recalled>` block naming `people/alice-chen` (by meaning;
the test uses a fake embedder that maps "she" near "Alice" is not honest, so
the test instead asserts the words path with "Alice" and the meaning path
with a fake embedder returning fixed vectors, separately). Live: on
root@server, ask about anything filed yesterday in different words and see
`<recalled>` in the transcript view.

### Milestone 4: knowledge from outside the conversation

The goal is that the person points the agent at where their knowledge
actually lives, and it keeps a searchable, citable copy. At the end, a
source is added on the agent page or by `teanode agent source add`, an
`ingest` job keeps it current, and `knowledge search` answers with chunks
and citations. Milestone 0 is what makes this scale: a repository is
tens of thousands of chunks.

**Sources.** Extend `agent_source.go` and the models: a source has a kind and
a specification —

    computer   {computer: "gen7", path: "~/projects/teanode", include: ["**/*.go", "**/*.md"], exclude: [...]}
    archive    {computer: "gen7", path: "~/chat-archive", format: "mattermost"}   a chat export the scan understood; retired for "records" on 2026-09-17
    skill      {tool: "skill__confluence__confluence_search", arguments: {...}, cursor_field: "start", item_path: "results", id_field: "id", text_field: "body.storage.value", title_field: "title", url_field: "_links.webui"}
    web        {start: "https://wiki.example.com/", allow: ["https://wiki.example.com/"], depth: 2, respect_robots: true}
    sent       {mailbox_id: "..."}   the person's own sent messages, for milestone 6

Table `agent_source` (migration `0068`): id, agent_id, kind, name,
specification jsonb, enabled, schedule (cron, default nightly at 03:00 in
the person's zone), cursor jsonb, last_run_at, last_error, document_count.
`agent_document`: id, source_id, external_id (unique with source), title,
url, kind (`file`, `page`, `post`, `message`), modified_at, hash, bytes,
storage_key (the text is kept in `storage.Storage`, not the database, as
attachments are). `agent_chunk`: id, document_id, number, text (at most
2000 characters), vector, vector_model, search tsvector. `agent_symbol`:
document_id, symbol, line, kind — the definitions a code file declares,
for exact lookup before any vector. Document kinds: `file`, `page`,
`post`, `message`, `commit`, `chat`; a `commit` document carries author
address and date in its metadata so "who" is a column, not a parse.

**Ingestion**, `internal/agent/ingest.go`, job kind `AgentJobIngest`,
subject the source id, queued by its schedule (through the existing
`agent_schedule` machinery: a source's schedule is a schedule row whose
prompt is empty and whose `deliver` is `ingest`) and by "Sync now". Each run
is bounded: at most 200 documents and 2000 chunks embedded, then it requeues
itself in ten minutes while there is more, as `backfill` does. Per kind:

- `computer`: refuses unless that computer is attached (the job defers an
  hour, and the agent page says "waiting for gen7"). Lists with the
  `filesystem` `search` action under the include globs, skips anything
  `git check-ignore` says to skip when the path is a repository (one
  `shell` call per batch of paths), reads files under 4 MiB, compares the
  hash to the document's, and chunks changed ones. Code is chunked at blank
  lines and brace-balanced boundaries where it can, prose at paragraphs,
  both with a 200-character overlap. Deleted files delete their documents.
- `skill`: calls the named skill tool through `internal/skills/run.go` as
  the person (their secrets), pages with the cursor field until the tool
  answers fewer than a page, maps fields by the specification's paths using
  the skill package's own selector, strips HTML with the existing sanitizer
  in `internal/mailer` or `bluemonday`, and files each item as a document.
  The Confluence, Mattermost and GitHub skills already installed are the
  first three tested.
- `web`: `internal/browser` or `web_fetch`'s fetcher, breadth-first from the
  start address within the allow prefixes to the depth, one request a
  second, honouring `robots.txt`, through the same address guard. Pages are
  read as text the way `web_fetch` reads them.
- `sent`: the mailbox's Sent folder items, newest first, and — where the
  person's own contact is set — any message in any granted mailbox whose
  `From` is one of that card's addresses, which catches mail sent from a
  phone or another program and filed elsewhere; each message's own
  text without quoted replies (`internal/mailer` has the quote stripper the
  reply pipeline uses), as documents of kind `message` with the recipient
  and subject in the title.

Everything ingested is untrusted data: it is chunk text the model reads,
never instructions, and the `knowledge` tool's results are wrapped as
`web_fetch`'s are.

**A home directory, measured.** The `computer` kind was designed above as
"list, read, chunk". The maintainer's own machine (gen7) says what that
meets, and the design below is shaped by it. Measured 2026-09-15:

    ~/projects    9.6 GB   550,330 files    82 repositories (depth ≤ 3)
    ~/mujin        85 GB   946,842 files    44 repositories; jhbuild/ 72 GB, security/ 8.4 GB
    ~/Documents    36 MB       367 files    99 PDFs, 19 Markdown, 3 xlsx
    ~/Downloads    17 GB       933 files    jpg 193, pdf 177, xml 111, mp4 77, dll 40, exe 18

    tracked files across the 126 repositories:            97,797
    of which text by extension, outside vendor/ etc.:     45,254  (1.7 GB — but see below)
    git authors, distinct:                                 3,279
    commits, all repositories:                           107,932   2005 .. 2026
    repositories with no commit by the person:                23 of 126
    the person's own author addresses:                        10  (one of their own addresses 9,732 commits,
                                                                    one of their own addresses 2,298, one of their own addresses 1,764, ...)
    extractors on the machine: pdftotext, libreoffice, ffprobe; no pandoc, no tesseract

Four things follow, and each is a rule in the ingestion rather than advice.

*Tracked files only, in a repository.* The 72 GB under `mujin/jhbuild` is
build output around two repositories the person has 1,990 commits in; the
build trees are ignored by git and the sources are not. So inside a
repository the manifest is `git ls-files` (which also honours every
`.gitignore` for free), plus `git status --porcelain` for what is new and
not yet ignored. Outside a repository the walk is the filesystem's, with
the source's own include and exclude globs.

*An extension list is not a text test.* The "1.7 GB of text" above is
mostly `.iso` (512 MB in `dev/provisioning`), an AppImage, `.pcap`, `.npy`
and `.mat` files that no list anticipated. A file is text when its first
8 KiB has no NUL byte and decodes as UTF-8, and it is chunked only when
it is under 512 KiB; larger text files (a 60 MB CouchDB dump) get a
document row and their first 64 KiB, so a search can still find the file.
That brings the corpus to an estimated 250 MB of real text, about 60
million tokens: a few dollars at `text-embedding-3-small`'s price, once.

*Secrets never leave the machine.* `mujin/security` has 1,146 of the
person's commits and 2,888 tracked `.pem` files. The scan refuses a file
whose name matches the secret patterns (`*.pem`, `*.key`, `*.p12`,
`*.pfx`, `id_*`, `.env*`, `*.kdbx`, `*credentials*`, `*secret*`), and
refuses a chunk whose text contains `-----BEGIN` followed by `PRIVATE KEY`
or `CERTIFICATE`, an `AKIA[0-9A-Z]{16}`, a `ghp_`/`sk-`/`xox[abp]-` token,
or a line whose Shannon entropy is above 4.5 bits per character over 32
characters or more; a refused chunk is counted and named in the source's
log, never sent. This runs **on the computer**, before anything crosses the
socket, so a mistake in the server cannot widen it.

*People come from the person's own history, not from git.* 3,279 authors
is every upstream contributor of every mirrored project. A person node is
made for an author only when they share a repository in which the person
has commits, their commits there overlap the person's active window in
that repository, they have five or more, and their address is at one of
the person's own work domains (from the "me" card) or already in the
address book. Everyone else is a number on the project page ("upstream:
3,100 contributors"). The person's own ten addresses — read from the "me"
card, which is why the card carries them — are what attributes commits to
them; an address the card lacks is offered as an addition to it.

**The scan runs on the computer.** The `teanode computer` program gains a
`scan` action beside `shell` and `filesystem`: given a root, include and
exclude globs, and the previous manifest's hash, it walks (git-aware),
sniffs, filters secrets, extracts text from what it can (`pdftotext` for
PDF; `soffice --headless --convert-to txt` for docx, pptx, xlsx, odt;
`ffprobe` for the duration, dimensions and title of video and audio; EXIF
date and dimensions for images) and answers a manifest of `{path, size,
mtime, hash, kind, extracted_bytes}` plus the text of files whose hash the
server does not already hold, in pages of 256 entries. The server does the
chunking and embedding and never runs an extractor itself. A 32 MiB
per-file cap and the existing 4-concurrent-requests limit bound it; a first
scan of `~/projects` and `~/mujin` is around 45,000 files and takes an
evening at nightly pace, then minutes.

**Repository profiles are the first thing on a project page.** Before any
chunk is embedded, each repository gets a *profile* the scan computes
locally in one `git` pass and the server files as facts on the project's
node with evidence kind `repository`:

    what it is        the README's first paragraph; go.mod / package.json /
                      pyproject name and description
    stack             languages by tracked-file count; the frameworks the
                      manifest names
    where it lives    remotes, and the organization in their path
    when              first and last commit; the person's first and last
                      commit; commits by month for the last two years
    who               the person's share of commits; co-authors by the rule
                      above; upstream contributor count
    state             default branch, ahead/behind, uncommitted changes,
                      the newest tag

The `when` facts are also written as `event` facts on `self` — "worked on
applysquare-django, 466 commits, April 2013 to January 2015" — which is
how the graph gets a career timeline without anybody typing one: the
person's git history says they were at applysquare 2013–2015, at Stanford
2013–2017 (`projects/ziyan/stanford`, 208 commits), and at Mujin from
February 2015 to now.

**The hierarchy mirrors the directory, then the dream links across it.**
A `computer` source names a *root path* in the graph; the directories
under it become `folder` nodes and each repository a `project` node:

    ~/projects  →  projects/                        (the root; the person's own)
                   projects/applysquare/…            organization applysquare, 14 repos
                   projects/ziyan/teanode            project
                   projects/ziyan/stanford           project; the dream will call it education
    ~/mujin     →  work/mujin/                       organization Mujin
                   work/mujin/dev/portal             project, 2,389 of 4,780 commits the person's
                   work/mujin/itl/walmart-calgary    project; the dream links organizations/walmart
                   work/mujin/ziyan-zhou/mussh       project
    ~/Documents →  documents/                        folder; one document node per PDF is too many,
                                                     so documents are chunks under the folder node
    ~/Downloads →  downloads/                        folder, dormant by default: searchable, never
                                                     in the index, never a source of facts

The first-level directory names under `~/projects` are organizations or
eras (applysquare, keppt, bitsq, zhile, upflare, ziyan), and under
`~/mujin` they are the company's own groups (dev, itl — integration
projects per customer — product, security, ziyan.zhou). The scan does not
guess what they mean; the dream's organize phase reads the profiles under
a folder and proposes the node's kind and a one-line summary
("applysquare: a startup, 2013–2015, Django and Android"), and proposes
cross-links a directory cannot hold — `work/mujin/itl/walmart-calgary`
`works_on` `organizations/walmart`, `people/<colleague>` `member_of`
`organizations/mujin`. Those are proposals in the dream log until applied,
as every cross-cutting edge is.

**Sensitive folders are opt-in, by name.** `~/Documents/mujineval` holds
evaluations of named people. The scan flags a directory whose name or
README matches `eval`, `review`, `salary`, `hr`, `recruiting`, `medical`,
`legal`, `tax` and lists it on the source as "skipped: looks sensitive;
include it?" with a switch; `mujin/recruiting` is the same. Nothing under a
flagged directory crosses the socket until the switch is on.

**Vectors at two widths.** A chunk's vector is 512 wide
(`text-embedding-3-small` accepts a `dimensions` argument and is trained so
the first 512 rank nearly as well as all 1536); a fact's or a node's is the
full 1536. Half a million chunks at 512 floats is one gigabyte in the
table and an index the same order; at 1536 it is three. The width is
recorded in `vector_model` as `openai:text-embedding-3-small@512` so the
milestone 0 index is built per width, and a change of width re-embeds
like a change of model.

**Delta scans are git's.** A repository's profile records its HEAD; the
next scan sends `git diff --name-status <recorded>..HEAD` plus the
porcelain status, and only those files are re-read. Outside a repository
the manifest's mtime and size say what to hash again. A file that
disappears deletes its document; a repository that disappears marks its
node dormant and leaves the page.

**Profiles are the floor, not the ceiling.** The maintainer asked the
question that decides whether metadata is enough: *"remember that
Quicktron v2 robot feature for detecting payload angle deviation? who did
that feature?"* Traced on gen7 on 2026-09-15, the answer is in none of the
places a profile reads:

    git log --grep=angle --grep=deviation across the quicktron repositories:   nothing
    code mentioning "payload angle":  vendored planning-client copies, and the vendor's own UI scripts
    Chat posts mentioning "payload angle" or "angle deviation":                82
      by channel:  m-220100-pepsico-carlisle-pa-… 25, 230118-pg-amiens-acp-system 18,
                   240132-kans-ssi-marshalltown-rcp 14, truckbot-onsite-tst 5, product-development-private 5
      by person:   manuj.trehan 25, ziyan 13, akshaya.srinivasan 5, kshitij.kabeer 4, yupeng.yang 4, ross.diankov 3
      one of them pastes a log line naming the code:  mwesexecutor.py:97 ResetPayloadAngularOffset

So the answer is assembled, not looked up: the chat says who talked about it
and when and names the function; the function's file is in a repository
(possibly one not on this machine); `git log` on that file says who wrote
it. Three consequences for the design.

*Commits are documents.* Every commit in a repository the person has
commits in becomes a document of kind `commit`: subject, body, author,
date, the files it touched, and — for commits by the person or by a
co-author who is a person node — the diff's added lines cut to 4000
characters. 107,932 commits on gen7 come to 4.7 MB of subject lines and
perhaps 200 MB of bounded diffs: cheap to embed, and "who added
`ResetPayloadAngularOffset`" is then one search. 5,198 of those subjects
name a merge or pull request; where the GitLab or GitHub skill is a source,
the request's description and discussion join the commit as one document.

*Symbols are an index of their own.* When a code file is chunked, the
scan also lists its top-level definitions (`func`, `class`, `def`, `type`,
exported constants; tree-sitter is not needed — one regular expression per
language is enough for names) as `{symbol, path, line}` rows in
`agent_symbol`. A search whose words contain something that looks like a
symbol (`CamelCase`, `snake_case_with_underscores`, a dotted path) hits
this table first, exactly, before any vector. The log line pasted into
chat resolves to a file and a line in one query.

*The graph learns at the point of use.* Nothing can distill two million
posts and a hundred thousand commits into facts in advance, and it should
not try: most of it will never be asked about. When a turn answers a
question from search — the `knowledge` tool was called and the answer
cites its results — the `remember` job (milestone 2) files the answer as a
fact on the node the evidence points at, with the chunk and commit ids as
evidence: "Payload angle deviation detection (WES executor,
`ResetPayloadAngularOffset`): written by <author> in <repo>, <month>;
discussed with manuj.trehan in the PepsiCo Carlisle channel" lands on
`work/mujin/project/pepsico-carlisle` and on the product's node. The next
time anybody asks, it is in the index. The graph grows where the person's
attention went, which is the only order that scales.

**Chat at scale: `~/chat-archive`.** The maintainer keeps an export of
their company's chat, made by a script of their own against the chat app's
API. Measured 2026-09-15:

    posts                       1,963,356    in 462 channels (342 public, 120 private), 7 teams
    message text                  228 MB     about 57 million tokens
    root posts / replies      676,163 / 1,287,193
    the person's own posts        159,086
    users                           1,310 in users.json (username, email, position); 1,046 have posted
    span                        2017-08-16 .. 2026-09-15; 2023–2026 are 300,000+ posts a year each
    post types                  post 1,599,706; slack_attachment (bots: TeamCity, alerts) 321,485; system_* 38,000
    files                       188,921 posts carry one; 5.3 GB on disk: png 3,843, jpg 286, mp4 167, pdf 51
    layout                      channels.json, users.json, me.json, state.json (per-channel last timestamp),
                                posts/<team>/<channel>.jsonl, files.jsonl, files/

The unit of chat is not the post. Embedding two million posts one by one
would be two million vectors of "ok" and "thanks". The scan reads a channel
file and cuts it into *units*: a thread (a root and its replies) where
`reply_count` is above zero, otherwise a *window* — consecutive posts in
one channel with no gap longer than thirty minutes, at most forty posts or
3,000 characters, whichever first. Each unit is one document of kind
`chat`, its title the channel and the date, its text the posts as
`[time] username: message`, its metadata the participants and the post
ids. On this archive that is around 250,000 units for 57 million tokens: a
dollar or two of embeddings, and eight hundred megabytes of 512-wide
vectors.

What is skipped, by rule: `system_*` posts always; `slack_attachment`
posts and any channel where more than four fifths of posts are of that
type (TeamCity, alert channels — a `bot` flag on the channel node,
searchable on request, never embedded); files other than PDF, text and
office documents (the 3,843 screenshots are named on their posts and
nothing more, until there is a vision model in the loop). Private channels
and direct messages are ingested — they are the person's own export, on
their own machine — but marked `private` on the document, and the
`knowledge` tool says so when it cites one, so an answer drafted for a
third party does not quote a private room without the person seeing that
it did.

Channels become nodes: `work/mujin/channels/<team>/<channel>`, kind
`topic`, with the channel's purpose and header as the summary and facts
for its span, post count, the person's share, and the five most active
participants. The channel names carry the customer and the project —
`m-220100-pepsico-carlisle-pa-robotic-case-picking-system` beside the
repository `mujin/project/pepsico-carlisle` — and the dream's organize
phase proposes the link between the channel node and the project node,
and both to `organizations/pepsico`. People come from `users.json`, by the
same rule as git authors: a person node for a user who shares a thread or
a private channel with the person and has fifty or more posts in those, or
who is already a contact; bound to the contact by email, with `position`
as a fact. 1,046 users become perhaps two hundred people; the rest are a
username on a unit.

Incremental ingestion is the archive's own `state.json`: the scan sends
units for posts after each channel's recorded `last`, and the server keeps
the same cursor per channel on the source. The live Mattermost skill
already installed on root@server carries on from the archive's last
timestamp for channels the person names, so the archive is the backfill and
the skill is the tail; both write the same documents.

Dreaming's digest phase treats chat by priority rather than by order, since
676,163 threads will never all be read: pinned posts first (`is_pinned`),
then threads the person started, then threads the person replied in with
three or more replies, then the rest, newest first, four hundred a night.
A thread digested becomes facts on the channel's node, or on the project's
where the link exists, with the thread's post ids as evidence. At that
pace the person's own 159,086 posts are worked through in about a year,
which is fine: the point-of-use rule above files what is asked about
long before the dream gets there.

**The tool**, `internal/agent/tools/knowledge/`: `search` (words; hybrid as
milestone 3, over the person's sources, optionally one source; answers
chunks with document title, url, path and the chunk number), `read`
(a document by id, in slices of 6000 characters), `sources` (list),
`add` (a kind and its specification; `RiskGranting`, so the person is
asked on a card that names the computer or skill and the path or query,
and the source is created only on their yes), `sync` (now; a write),
`remove` (`RiskDestructive`; asks, and its documents go with it). `RiskOf`
judges the call, as the memory tool's does, so a research run that may
only read still has `search` and `read`. Recall (milestone 3) gains a fifth
list: the top three chunks, when the turn's words score above the floor, so
a question about the code is answered from the code without a search. The
tool's guidance says when to offer a source: when the person asks the agent
to "keep up with", "know about" or "watch" something that has a home the
agent can reach, and the answer today would be a one-off fetch.

**Dashboard and command line.** The agent page lists sources with their
state (documents, last run, error, "waiting for gen7"), Sync now, and
remove; adding one is a form per kind. `teanode agent source add
computer|skill|web|sent ...`, `list`, `sync`, `remove`. Removing a source
removes its documents and chunks; deleting the agent removes everything.

**Acceptance.** Tests per kind with fakes: a fake computer answering a fixed
tree, a fake skill tool paging three pages, a fake fetcher; and one for the
tool asserting that `add` without the person's yes creates nothing. Live:
say "index ~/projects/teanode on gen7" in the drawer, approve the card,
wait for the sync, ask "where is cosine similarity computed?" and get
`internal/db/vector_go.go` cited by path and chunk. Say "keep up with the
platform channel in Mattermost", approve, and the next day ask about a
post by a phrase that is not in it.

### Milestone 5: dreaming

The goal is that the agent organizes what it knows while nobody is talking
to it, and says what it did. At the end, the agent page has a dream log,
pages are rewritten from their facts, mail has been folded in, and nothing
was lost.

**When.** `Agent.tickAt` queues a `dream` for an agent when all of: the
agent's last turn ended thirty minutes ago or more; no job of the agent is
running; the last dream finished six hours ago or more, or never; the
person's local time is inside their dream window (default 01:00–06:00,
settable on the agent page; "any time" allowed); and the day's budget has at
least the dream share left (`agent.limits.dreamShare`, default 0.3 of the
daily token budget, so a dream never eats the person's day). Idempotent on
(agent, `dream`, date).

**Phases**, each a bounded call of the fast model with a transcript of kind
`run` named `dream`, in order, each skippable when there is nothing to do:

1. *Digest.* What arrived since the last dream and was never filed: mail
   insights and thread summaries triage wrote (`mail_insight`), chat
   channel messages, and new documents' titles. In batches of forty items
   the remember prompt (milestone 2) runs over them with the index, filing
   facts with evidence pointing at the item. Capped at 400 items a dream;
   the rest waits for tomorrow, oldest first, so a backfilled mailbox is
   digested over a week rather than one night.
2. *Consolidate.* For each node touched since the last dream (`modified_at`
   later than it), the fast model is given the page and all its live facts
   and asked to rewrite the summary in at most 1200 characters as a page
   about the thing — who, what, why it matters to the person, what is
   current — and to name pairs of facts that say the same thing. Twin pairs
   are merged (the older keeps the number, evidence is unioned). Facts the
   summary now states are left as facts; the summary is the digest, the
   facts are the record.
3. *Organize.* Nodes under `notes` with more than one fact, and nodes with no
   parent but the root, are offered to the model with the index and asked
   where they belong; a `move` and a `link` are written only when the model
   names an existing path, otherwise a proposal is recorded. A node with
   more than eighty live facts is offered for splitting into children;
   the split is a proposal too. Proposals appear in the dream log with an
   Apply button and are never applied by the dream.
4. *Importance.* `importance` is recomputed for every node as a blend of
   use recency, live fact count, edge count, and whether it is `self` or a
   person the address book keeps; this is what orders the index. Facts not
   used or cited in 180 days and not preferences or decisions become
   `dormant`, and a node whose facts are all dormant becomes dormant.
   Nothing is deleted.
5. *Vectors.* Facts, nodes and chunks whose vector is missing or from
   another model are embedded, up to the chunk cap.
6. *Voice* (milestone 6).

Each phase records what it did in the run's transcript and in a
`agent_dream` row (migration `0069`: id, agent_id, started_at, finished_at,
digested, filed, merged, moved, proposals jsonb, tokens). The agent page's
"Last night" shows the row in a sentence — "Worked through 34 messages and 2
documents, filed 12 facts on 7 pages, merged 3, tidied `projects/` — and 2
suggestions" — with the list under it and Apply/Dismiss per proposal.
`teanode agent dream log` and `teanode agent dream now` from a terminal.

**Acceptance.** Tests per phase with a fake provider: digest files with the
right evidence and honours the cap; consolidate merges the pair the model
named and keeps the lower number; organize records a proposal for an
unknown path and moves for a known one; importance orders `self` first and
puts an untouched node under a used one; nothing is ever deleted by any
phase (a test counts rows before and after). Live: after a night on
root@server, read the log and open a page that was rewritten.

### Time: the axis everything accumulates along

Work arrives day by day, and "what did I do in March", "when did we last
touch the conveyor bridge", "who was on that site in 2022" are questions
about *when* as much as *what*. The maintainer has already built the
answer by hand, once, for one employer: `~/projects/ziyan/personal/mujin/`
is a survey of eleven and a half years at Mujin — a `README.md` with a
year-by-year table (37,905 commits, 2,655 merge requests authored, 4,465
reviewed, 655 issues, 157 GitHub pull requests, 125 Confluence pages), a
`<year>/README.md` with "what the year was about" as four to eight threads
and a top-repositories table, and a `<year>/<year>-<MM>.md` per month
grouped by theme, every merge request linked, written in the person's own
plain style ("no em dashes; explain why, not what; name the concrete
thing"). Its `scripts/` are the pipeline: `collect` (git logs from local
clones restricted to `origin` refs, GitLab merge requests and issues by
API, GitHub pull requests, Confluence pages by year), `dedupe.py` (the same
commit hash lives in several forks and renamed repositories; attribute it
to the repository whose history starts earliest), `gen_digest.py` (per
year, per month: repositories touched, commit subjects with twenty-eight
noise patterns dropped — "Update.", "WIP", "Bump version", "Fix typo" —
merge request titles with labels, issues, reviews, Confluence; and
`~/TODO.md`, a weekly journal 2017–2020, read by date heading with
credential-looking lines dropped), `build_data.py` (aggregate TSVs) and
`write_message_sections.py` (chat message share per project into
the month and year files, because "message share is the best measure of
effort per project, and the share matters more than the total").

That is one person's setup, and the maintainer said so: it has to be
generalized before it goes into the server. What generalizes is the
*shape*, and each piece has a home in this plan already.

**Period nodes.** The graph gets `time/<yyyy>` and `time/<yyyy>/<mm>` nodes
of a new kind `period`, made on demand. A period's page is the month's
narrative; its facts are the aggregates; its edges point at the projects,
people and organizations the month touched. `time/` is a root beside
`people/` and `projects/`, and the index carries the current month and the
previous one so "this month" is always in the prompt.

**The digest is a server function, not a script.** `Digest(agentId,
from, until)` in `internal/agent/digest.go` reads every document with
`happened_at` in the range — commits by the person (by the "me" card's
addresses), merge and pull requests where the forge skill is a source,
chat units the person posted in, mail threads with a summary, calendar
events, journal entries, documents changed under a `computer` source —
and writes the same thing `gen_digest.py` writes, as text: repositories
touched with counts, subjects with the noise patterns dropped (the
twenty-eight from the script become `internal/agent/digest_noise.go`,
generic as they already are), requests by title, issues, reviews, pages,
channels by the person's share, journal lines. No model runs. Fork
attribution is the commit document's identity: a `commit` document is
keyed by hash per agent, first writer wins, and the scan sends each
repository's first commit date so the server attributes a hash seen twice
to the repository whose history starts earliest — `dedupe.py`, once, in
Go, for everybody.

**The narrative is a dream phase.** *Timeline*, after *digest* and before
*consolidate*: for the open month, the scan model is given the month's
digest, the page as it stands, and the person's writing rules (from
`Agent.Voice` and instructions — the survey's "writing style" section is
what those fields are for), and rewrites the page grouped by theme with
every request and repository linked and every claim carrying the id of
the commit, request or thread it came from. A month closes when the next
one has a digest; a closed month is rewritten once more and then only by
the person. The year page is rewritten from its months when a month
closes. That is one scan-model call a night for the current month, one a
month for the year: the whole timeline costs less than one conversation.

**Facts with two dates.** Every fact already has `happened_at` (when the
thing was true or happened) beside `created_at` (when the agent learned
it). The digest fills `happened_at` from the document; the `remember` job
fills it when the person's words say when ("last spring", "in 2023",
"yesterday") through the existing `timeparse.go`; otherwise it is the
turn's date. A fact of kind `event` — "worked on rgmpo, 56 commits, June
2023" — is what the timeline and `self` are made of.

**Recall is time-aware.** Two changes to milestone 3. First, when the
turn's words name a time, `timeparse.go` turns them into a range and
recall filters on `happened_at` *before* ranking, so "what was I doing in
June 2023" reads `time/2023/06` and that month's facts and nothing from
2019. Second, the fused score carries a recency term per kind: none for a
preference, a decision or a period page (they are asked for by name);
`exp(-age / 90 days)` for a status fact and a chat unit; `exp(-age / 365
days)` for a commit; a document's is by its own `modified_at`. Older
things are not hidden — they are behind newer ones of the same weight,
which is what a person means by relevant.

**Sources this needs, generalized.**

    forge     a `skill` source against GitLab or GitHub with the person's own
              identity: merge or pull requests authored and reviewed, issues
              authored and assigned, each a document of kind `request` or
              `issue` with its discussion; the commit documents link to them
              by the "!123" / "#123" in their subjects
    journal   a `computer` source with `format: journal`: a Markdown file or
              folder whose date headings (`# 2020/12/07`, `## 2023-06-01`) or
              file names (`2026-09.md`, `2026-09-15.md`) carry the date; each
              entry is a document with `happened_at`; the secret filter runs
              on every line, which on `~/TODO.md` drops the credentials its
              first lines hold
    wiki      Confluence or any page skill, already a `skill` source; pages
              carry their created date as `happened_at`

**Bootstrapping from what exists.** The survey's month files are period
pages already written; a `journal` source pointed at
`~/projects/ziyan/personal/mujin` maps `<yyyy>/<yyyy>-<mm>.md` to
`time/<yyyy>/<mm>` and `<yyyy>/README.md` to `time/<yyyy>` on first scan,
with evidence kind `document`, so the timeline is eleven years deep on day
one and the dream continues it from 2026-09. Anybody else's dated notes
bootstrap the same way; a person with none starts at their first digest.

**What stays the person's.** The forge and chat credentials (their `glab`,
`gh`, `mm` and Confluence logins) are skill secrets of theirs, never the
operator's. The repository-name mapping the survey needed (`controllercommon
→ mujin/dev/portal`) is not needed: the forge source knows every project's
path and the commit document carries the remote. The customer-name
shortening table in `write_message_sections.py` ("WM RDC 7101 Calgary Floor
Line Depal" → "Walmart Calgary") is the organize phase's job, as a proposed
alias on the organization node, not a table in the server.

**Acceptance.** With the journal source on the survey and the computer
source on `~/mujin`, `teanode agent memory get time/2023/06` prints the
survey's June 2023 page; after a night, `time/2026/09` has a section for
this week's commits with each one linked, in the person's style; "what did
I work on in June 2023?" is answered from the page without a tool call,
and "what did I do last week?" from the open month with evidence ids.

### Milestone 6: acting as the person

The goal is the maintainer's stated end: an agent that answers as they
would. It stands on the other five and adds three things.

**The `self` page.** Dreaming's consolidate phase treats `self` specially:
its summary is written as "who you are" — name, roles, employer, family,
where they live, how they like things done — from its facts, at most 2000
characters, and it is always the first thing in the prompt (milestone 1).
The person edits it directly on the agent page; their edit is evidence of
kind `person` and outranks anything filed.

**Exemplars at reply time.** With a `sent` source (milestone 4) ingested,
the reply and draft runs (`reply.go`, `draft.go`) fetch the five of the
person's own past messages nearest in meaning to the message being
answered, cut to 1500 characters each, and render them in the prompt under
"How {{.PersonName}} has written to people about things like this", plus
the sender's page and the pages the message's words recall. The prompt
already says "as {{.PersonName}} would send it"; now it has examples of
what that is.

**Voice proposals.** A dream phase, once a week, reads a sample of forty
sent messages (stratified: work domains, personal domains) and proposes an
`AgentVoice` (the existing structured fields on the agent row) per audience,
with three quoted lines each as the reason; the proposal goes to the dream
log, and Apply writes it. Nothing about the voice changes without the
person's press.

**Decisions.** Facts of kind `decision` — "chose Postgres over the
filesystem for mail because the dashboard must search it" — are what the
recall step ranks first when the turn contains "should", "decide", "which"
or a question mark, so "how did I handle this last time" is answered from
the record. The reply prompt gains the same block.

**Acceptance.** The maintainer's own check, written down here so it is
run: take ten threads they answered in the last month, hide their
answers, have the agent draft each with the reply pipeline, and count how
many they would have sent with no edits. The plan calls the milestone done
at five of ten, and records the number.

## Concrete Steps

All commands run from the repository root, the checkout
(or the worktree in use). `make` formats, builds and tests; `make test`
starts its own PostgreSQL container. Use `TEANODE_PROFILE=local` for the
command line client against a development server (the active profile in
`build/teanode` points at production).

Milestone 0:

    # the spike, in the scratchpad
    docker run -d --rm --name vector-spike -e POSTGRES_HOST_AUTH_METHOD=trust pgvector/pgvector:pg17
    # write scratchpad/spike.sql per the milestone; then
    docker exec -i vector-spike psql -U postgres < scratchpad/spike.sql
    # expect: "Index Scan using memory_vector_m" in the EXPLAIN output

    # then the code
    go test ./internal/db/ -run TestVector
    TEANODE_TEST_VECTOR=off go test ./internal/db/ -run TestVector   # against postgres:17
    make

Milestone 1 onward, after each:

    make            # gofmt, build, test
    make lint
    git checkout -- vendor    # make test rewrites vendored files; never stage them
    make docker DOCKER_TAG=teanode:memory
    docker save teanode:memory | ssh root@server docker load
    # edit image: in /opt/teanode/docker-compose.yml, confirm the upgrade directory is empty,
    ssh root@server 'cd /opt/teanode && docker compose up -d --force-recreate teanode'
    # confirm the teanode.<hash>.css name changed on https://mail.teanode.com/

Before the first deploy that adds migrations 0065–0069, take the usual dump:

    ssh root@server 'cd /opt/teanode && docker compose exec -T postgres pg_dump -U teanode teanode | gzip > /root/teanode-backups/before-memory-$(date +%Y%m%d-%H%M).sql.gz'

## Validation and Acceptance

Each milestone's acceptance is stated with it. Across the whole plan, the
things a person can verify:

1. `make` passes on `pgvector/pgvector:pg17` and on `postgres:17` (the
   Makefile runs the vector tests both ways).
2. In the drawer, tell the agent something about a person, wait a minute,
   and find it on that person's page with the quote, without having asked
   it to remember.
3. Ask about the same person in different words in a new conversation and
   get the answer without a tool call; the transcript shows `<recalled>`.
4. Add a repository on an attached computer as a source; the next morning,
   ask a question about the code and get a cited answer.
5. Read last night's dream log; open a page it rewrote; apply one of its
   suggestions.
6. Count the ten-thread reply check and record the number in Outcomes.

## Idempotence and Recovery

Migrations 0065–0069 each carry a reverse; `TEANODE_ALLOW_MIGRATION_REVERT=true`
with the previous binary reverts them. The copy of `agent_memory` into facts
is tagged by evidence kind `memory`, so the reverse deletes exactly what it
made and `agent_memory` is untouched. Vector indexes are created with `IF
NOT EXISTS` and can be dropped by hand (`DROP INDEX IF EXISTS
agent_fact_vector_<model>`); the server recreates them at the next start.
`remember`, `ingest` and `dream` are idempotent on their subject and advance
a cursor in the same transaction as their writes, so a job that dies is
re-run from where it was. Ingestion compares hashes, so a re-sync of an
unchanged source writes nothing. A dream never deletes; a person who
dislikes what it did can move or strike, and "Dismiss" on a proposal keeps
it from being proposed again (the proposal's hash is kept on the
`agent_dream` row).

## Artifacts and Notes

To be filled as milestones land: the spike's `EXPLAIN` transcript, the
first dream log, the reply-check tally.

Evidence that started the plan (root@server, 2026-09-15):

    select role, count(*) from agent_message group by role;
      assistant|752  user|456  note|186  compaction|1  tool|313
    select name, count(*) from agent_message where role='tool' group by name order by 2 desc;
      terminal|37 tool_search|34 skill__unifi_protect__protect_ops|25 browser|21
      filesystem|15 mail_read|15 mail_search|14 memory|12 shell|12 ...
    select count(*), count(vector) from agent_memory;   -- 2|2

## Interfaces and Dependencies

No new Go dependency for milestone 0: pgvector is spoken as SQL text through
`lib/pq` (`'[0.1,0.2,...]'::vector`), the same driver GORM uses here; the
`pgvector-go` module is not needed and is not added. Chunking, slugs and
reciprocal rank fusion are written here; the HTML stripping reuses what the
mailer has. `robots.txt` parsing uses `github.com/temoto/robotstxt` if it is
not already vendored, added in milestone 4 — check `vendor/modules.txt`
first.

In `internal/db/database_vector.go`:

    type VectorOperation interface {
        VectorIndexing() bool
        EnsureVectorIndex(table, column, modelColumn, model string, dimension int) error
        NearestAgentFacts(agentId, model string, query []float32, limit int, floor float64) ([]Scored, error)
        NearestAgentNodes(agentId, model string, query []float32, limit int, floor float64) ([]Scored, error)
        NearestKnowledgeChunks(agentId string, sourceIds []string, model string, query []float32, limit int, floor float64) ([]Scored, error)
        NearestMailEmbeddings(mailboxId, model string, query []float32, limit int, floor float64) ([]Scored, error)
    }

In `internal/models/graph.go`:

    type AgentNode struct {
        ID, AgentID, Path, ParentID string
        Kind      AgentNodeKind
        Name      string
        Aliases   []string
        Summary   string
        ContactID string
        Pinned, Dormant bool
        Importance float32
        UsedAt    *time.Time
        CreatedAt, ModifiedAt time.Time
        Vector []float32; VectorModel string
    }
    type AgentFact struct {
        ID, AgentID, NodeID string
        Number int
        Kind   AgentFactKind
        Text   string
        HappenedAt *time.Time
        Confidence float32
        Evidence []Evidence
        Audiences []AgentAudience
        SupersededBy string
        Dormant bool
        UsedAt *time.Time
        CreatedAt, ModifiedAt time.Time
        Vector []float32; VectorModel string
    }
    type Evidence struct{ Kind, ID, Quote string }
    type AgentEdge struct{ AgentID, FromID, ToID, Relation string; Weight float32; Evidence []Evidence }
    func Slug(name string) string
    func ValidPath(path string) error

In `internal/computer/` (the program the person runs), a `scan` action
beside `shell` and `filesystem`; `Protocol` goes to 2 and a server that
needs `scan` says so to a computer speaking 1 ("update teanode on gen7 to
index it"). In `internal/computer/scan.go`:

    type ScanRequest struct {
        Root, Previous string       // the root, and the manifest hash the server holds
        Include, Exclude []string   // globs
        Sensitive []string          // directory names switched on by the person
        Page int                    // 256 entries a page
    }
    type ScanEntry struct {
        Path string; Size int64; ModifiedAt time.Time; Hash string
        Kind string                 // text, pdf, office, image, video, audio, binary, secret, sensitive
        Text string                 // present when the server said it lacks this hash
        Repository *RepositoryProfile  // on the repository's root entry only
    }
    type RepositoryProfile struct {
        Head, DefaultBranch, NewestTag, Remotes []string
        Languages map[string]int
        Readme string               // first 4000 characters
        First, Last time.Time
        Commits int
        Own struct{ Commits int; First, Last time.Time; ByMonth map[string]int }
        Authors []struct{ Name, Address string; Commits int; First, Last time.Time }
        Contributors int            // everybody, for "upstream: n"
        Dirty bool; Ahead, Behind int
    }

`internal/computer/secret.go` holds the name patterns and the content
tests and is used by `scan` only; `internal/computer/extract.go` finds
`pdftotext`, `soffice` and `ffprobe` on the PATH once and says on the
source's page which are missing.

In `internal/agent/`: `runRemember`, `runIngest`, `runDream` as job handlers
registered where `runEmbed` and `runResearch` are; `recallForTurn` rewritten;
`prompts/remember.txt`, `prompts/dream_consolidate.txt`,
`prompts/dream_organize.txt`, `prompts/dream_voice.txt`.

In `internal/config/agent.go`: `AgentFeatures.Remember`, `.Knowledge`,
`.Dreaming` (*bool, nil is on); `AgentLimits.DreamShare float64` (default
0.3), `.IngestChunksPerRun int` (default 2000); `AgentModels.Scan string`
(the bulk-understanding model; empty resolves to `Fast`) and
`AgentModels.EmbeddingDimensions int` (0 means the model's full width; 512
is the compose file's example).

Node kinds gain `period`; `internal/agent/digest.go` exposes
`Digest(ctx, agentId string, from, until time.Time) (string, error)` and
`internal/agent/digest_noise.go` the subject patterns; `prompts/dream_timeline.txt`
is the narrative prompt. Source formats for the `computer` kind: `files`
(default), `mattermost`, `journal`. (`mattermost` was retired on
2026-09-17 by `docs/planning/active/20260916-records-from-anywhere.md`,
which reads a chat export through a `records` folder instead.)

Tools: `internal/agent/tools/memory/` rewritten; `internal/agent/tools/knowledge/`
new. Both `Family: FamilyGeneral`, `Core: true`.
