# Memory: a graph the agent writes, and reads back

What the agent keeps between conversations, how it gets there without
anybody asking it to, and what happens to it while nobody is watching.

`internal/agent/graph.go`, `remember.go`, `recall.go`, `decay.go`,
`duplicate.go`, `ingest.go`, `dream.go`, `dream_stages.go`, `digest.go`;
`internal/agent/tools/memory/`; `internal/db/database_graph.go`,
`database_knowledge.go`, `database_dream.go`, `database_revision.go`,
`database_vector.go`; `internal/computer/scan.go`.

The flat list of memories this replaced is gone. What it held was moved
onto the graph the first time the graph was touched, and the API keeps
the old queries for one release.

## The shape of it

Four things, and everything else is built from them.

**A page** is something the person's life has a name for: a person, a
project, an organization, a place, a thing, a topic, a stretch of time.
It lives at a path — `people/alice-chen`, `projects/portal`,
`time/2026/09` — and the path is its address everywhere: in the prompt,
in a citation, in the dashboard's URL, on the command line. Every page
has a one-paragraph summary, which is what the page is *about*.

**A fact** is one sentence on a page, numbered. `people/alice-chen#3` is
the third thing the agent knows about Alice, and that is how the agent
cites it in a conversation and how the person finds it again. Numbers are
never reused: a page keeps a high-water mark, so striking #3 does not
hand that number to the next thing written.

A fact carries two dates, because they answer different questions:
`happened_at` is when it was true and `created_at` is when the agent
learned it. "They moved to Osaka" happened in 2019 and was learned last
Tuesday, and a question about 2019 wants the first.

It also carries its evidence: the words it came from, with what they came
from — a conversation, a message, a document. A sentence with a quote
behind it can be checked; one without cannot, and the dashboard says
which is which.

**An edge** joins two pages and says how. Not "related": `works_on`,
`member_of`, `knows`, `owns`, `uses`, `located_in`, `decided_in`,
`about`, `part_of`. Each relation knows how to say itself in both
directions — the edge from Alice to Portal reads "works on" from her page
and "is worked on by" from the project's — and an edge may carry a
sentence of its own ("led the controls work until 2025"). An edge nothing
has touched in a long time is said in the past tense, which is the
smallest honest way to show that it may no longer be true.

Which commits are **yours** is decided by matching the commit author
against the addresses on the card you marked as yourself, so that card is
what makes a career timeline possible at all. A source that finds commits
by nobody it recognizes keeps those addresses and says so, rather than
quietly attributing none of your work to you.

The first edges come from git, with no model involved: a repository the
person committed to becomes `self works_on <project>`, with the commit
span as its sentence. That matters more than it looks — until something
draws the first edges the graph is a list of pages, and the half of the
night that looks for connections has nothing to look at.

**A document** is something read from somewhere else: a file in a
checkout, a commit, a chat thread, a note, a message. Documents are not
facts; they are what facts get made out of, and they live beside the
graph with their passages and their vectors.

## What every prompt carries

The top of the graph: the most important pages, each as one line, in a
fixed order. Importance is recomputed by the nightly run and nowhere
else, on purpose — a list reordered by every read is a prompt prefix that
is never a cache hit.

The agent reaches for a page by path, searches by words or by meaning,
and writes with the same tool. Because the path is the address, the model
can say `projects/portal#2` in a sentence and the person can click it.

## Writing, without being asked

The first version of this asked the model to write memory during a turn.
Over 456 turns on a real server it wrote two, both tests. A model given a
task and a memory tool does the task.

So writing is not in the turn. A job (`remember.go`) runs after a
conversation goes quiet, reads what was said, and files what it taught:
pages made or found, facts added with their quotes, links drawn. It knows
where it got to (`agent_conversation.remembered_through`), so it never
reads the same exchange twice and never misses one.

The same job's shape does the reading of sources: `ingest.go` asks an
attached computer for a page of a scan, files the documents, and the
nightly run turns them into facts.

## Not making the same page twice

A graph's real failure is not forgetting; it is fragmenting — "Portal",
"the portal", `projects/portal` and `work/portal` as four pages that each
know a quarter of it.

So every writer goes through one resolver (`duplicate.go`), which looks
for an existing page three ways before making one: by path; by name or
alias among the pages under the same parent; and by meaning, above 0.94
cosine, with the kind matching. Above that floor two pages of the same
kind under the same parent are the same thing often enough that merging
is right and rare enough that a mistake is visible.

The kind has to match because "Alice Chen the person" and "Alice Chen the
project she named after herself" are two pages, however close the words.

## Decay: what surfaces, and what stops surfacing

A fact is not equally true for ever, and the ones that stop being true do
not announce it.

Each kind of fact ages at its own rate: a status ("they are on parental
leave") halves in ninety days, an event in a year, a how-to in three, and
a preference or a decision does not decay at all — those are asked for by
name and a stale one is still what the person said. Nothing decays to
nothing: the floor is 0.15, so an old fact can still be found, it just
stops arriving uninvited.

Ageing is from `happened_at` where it is known, because a fact learned
today about 2019 is a 2019 fact. Use lifts it back up. An inferred fact
starts at 0.85 of a stated one.

Pages and edges decay the same way, with periods and pinned pages exempt:
a page about September 2026 does not become less true in October.

## What happens overnight

A night runs when the person has been quiet for half an hour, inside
their own night, at most once every six hours. It has its own share of
the day's tokens (30% by default), and it never deletes anything.

**Read what arrived.** Documents, by priority rather than by order: what
the person wrote, then what they took part in, then the rest, newest
first. Every bound here is pacing, never truncation — what is not read
tonight is read tomorrow, the backlog is reported as a number the person
can see, and when the backlog is larger than anybody would wait for the
night reads a stretch at a time instead of an item at a time and marks
that it did, so a later night can go back over it finely.

**Write up the month.** One call for the month in hand, from a digest
assembled without a model at all (`digest.go`: commits by repository,
threads by channel, the person's own notes, with the noise dropped).

**Rewrite what changed.** A page that gained four facts today is a page
whose opening no longer says what it is about. The same pass merges the
facts that say the same thing — the older keeps its number, the newer
goes dormant carrying a pointer to it. Never deleted: a merge the person
disagrees with can be undone.

**File the orphans.** Pages under `notes` are offered a home. A move to a
path that already exists is made; anything else is a proposal with a
button, because a hierarchy invented while nobody is watching is one the
person will not recognize.

Then the night splits in two.

**The quiet half** is arithmetic and no model runs in it. Importance is
five things: how many facts a page has, how many links, how recently it
was wanted, what kind of thing it is, and a fortnight of grace for being
new — that last one because the other four make a trap between them, in
which a page written last night has never been used, so is not in the
index, so nothing can use it, so it never will be. Every link is
downscaled a little; then any link whose *both* ends were wanted today
rises. Down first and then up, so a link used today ends the night above
where it started. Importance is recomputed, facts nothing has wanted in
half a year go dormant, and pages that fall under a threshold the graph
sets for itself — mean importance minus a standard deviation scaled by
how far past its intended size the graph has grown — leave the index.
They are marked dormant, never deleted, and a page that has not yet had
forty-five days to be wanted is exempt.

**The generative half** walks the graph from what matters, five steps,
weighted by link strength with enough jitter that it does not take the
same path every night. It puts the two ends of the walk to the model and
asks whether there is a real relation between them. "No" is the ordinary
answer and the right one: almost everything in a person's life is
reachable from almost everything else in five steps. When the answer is
yes, a typed link is written with the sentence that justifies it, at half
the weight of one somebody stated — it earns the rest by being useful.

This is the only phase that adds a relation nobody typed, which is the
whole reason to keep a graph rather than a list.

**Rehearsal** is last, after the vectors are written, because it asks
the graph by meaning and everything filed tonight has no vector until
then. The agent writes down the questions the person is
most likely to ask tomorrow, tries each against its own memory, and
records the ones it cannot answer. Nobody reads the answers; the failures
are the point. A gap found at three in the morning costs one model call,
and the same gap found mid-conversation is the person watching their
agent say it does not know. Gaps are written down, never filled: a run
with nobody present inventing answers to its own questions is how a graph
fills with fiction.

## Facts that say nothing

A page already carries three things: its name, what kind of thing it is,
and that it exists. A line repeating any of those is not knowledge, and
it is worse than an empty page, because "Formatting is a project or work
channel." reads like something was learned.

This happened at scale on the first real ingest. A night working
coarsely — titles only, because the backlog was thirty thousand things —
was told it could file "what a title plainly establishes", and a title
plainly establishes only that the thing exists. Twenty-two per cent of
the graph became "X is a project or work channel" and "the X work
channel had activity in September 2026".

The prompts say not to now. The rule is also in code, because a prompt is
a request and the same request will be made of a different model next
year: take the sentence, subtract the page's own name (allowing for the
ends of words moving, so "Depalletize" covers "depalletizing"), subtract
the words that only say what a page is, subtract dates. If nothing is
left, nothing was said. It errs towards keeping — one surviving word of
substance is enough.

## Traceable

Every write files a revision against the page: what kind of change, who
made it (the person, the agent, the job that files conversations, the
nightly run, a source being read), what it moved, and what was there
before. A link files one against *both* pages it joins, each naming the
other end.

Without the "before", a history can be read but not undone, which is the
half somebody wants at the moment they go looking. Without the actor, a
page the nightly run wrote and a page the person wrote look identical.

Every page and every fact also carries the **build that last wrote it**,
which is what lets a newer version of the program go back over what an
older one filed. A graph outlives its code: the rule above did not exist
when those twenty-eight lines were written, and nothing could find them
afterwards except a person with a regular expression. Now the first thing
a night does is offer everything an older build wrote to the rules as
they stand, strike what fails them — in the page's history, as the
nightly run's doing, with the words it used to say — and stamp the rest
so tomorrow looks elsewhere. No model runs in that pass: a rule worth
applying to a graph unattended is one that can be stated in code.

Revision numbers are taken from a high-water mark on the page, the same
way fact numbers are, so they rise and are never reused. A link written
again with nothing changed files nothing — the quiet half writes every
edge every night, and a history full of changes nobody made is a history
nobody reads.

It is on the page in the dashboard, in `teanode agent memory history
<path>`, and in the API.

## Vectors

Pages, facts and passages carry their vector and the name of the model
that made it, in columns on their own rows. Where the pgvector extension
is present an HNSW index is built over each — as an expression index on
the `real[]` column, so the same rows still serve the Go path — and
where it is not, cosine is computed in Go over a bounded candidate set.
Both go through one interface (`database_vector.go`); nothing above it
knows which ran.

Passages are embedded at 512 dimensions rather than 1536. A corpus of
this size at full width is most of the disk for a difference nothing can
measure at this scale.

| | value |
| --- | --- |
| passage vector width | 512 |
| candidates ranked without the extension | 1000 |
| similarity that means "the same page" | 0.94 |
| decay floor | 0.15 |
| status half-life | 90 days |
| event half-life | 1 year |
| how-to half-life | 3 years |
| a night's share of the day | 30% |
| pages the index is sized for | 400 |
| passages embedded per ingest run | 2000 |

## Recall, once a turn

Words and meaning, both, merged by reciprocal rank fusion — a full-text
search over pages, facts and passages, a vector search over the same, and
one ranked list out. Decay multiplies the score, so a stale fact sinks
without disappearing. There is no minimum word length any more; the
previous version needed a word of four letters before it would search by
meaning at all, so "who is he?" recalled nothing.

## Caveats

- **A night is paced, not complete.** A large backlog takes several
  nights, and the log says how many. Nothing is dropped, but a fact from
  a document read tonight may not be on its page until tomorrow.
- **Rehearsal asks by meaning and not by words.** The word search joins a
  question's words with OR, which is right for recall and wrong for "do
  I know this": it answers yes to almost anything, and a rehearsal that
  never finds a gap is a phase that costs a call and reports nothing.
  Where there is no embedding model there are no gaps rather than all of
  them.
- **The generative half can be wrong.** A link it writes is a link the
  agent will state as though it were told. It starts at half weight and
  carries the walk that suggested it as its evidence, which is what a
  person needs to disagree with it.
- **"Which passages still need a vector" is a filter on one table**, not
  a join against the vectors. The model that embedded a passage is
  written on the passage, and the query has no ORDER BY — asking for
  oldest-first meant sorting half a million rows to return a hundred.
- **A scan page is bounded by bytes and by count.** Both, because 256
  commit subjects are nothing and 256 source files are tens of megabytes.
- **Pages are retired, never deleted, by the nightly run** — but a person
  deleting a page deletes everything under it.
- **Dormant is not gone.** A dormant page or fact is out of the index and
  still findable by search, which is why retiring is safe and why a
  search can return something the prompt did not carry.
