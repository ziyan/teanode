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
hand that number to the next thing written. A sentence filed under the
wrong name is moved rather than rewritten — the memory tool's `move` with
a number, `teanode agent memory move --number 3 people/alice-chen
projects/portal`, the `MoveAgentFact` mutation — which keeps the words it
came from and the day it was learned, and gives it a new number on the
page it lands on. A sentence that says the wrong thing is corrected where
it stands for the same reasons — the memory tool's `note` with a number,
`teanode agent memory note --number 3 people/alice-chen "..."`, the
`SaveAgentFact` mutation with one — and keeps its number too, so whatever
cited it still points at it.

A fact carries two dates, because they answer different questions:
`happened_at` is when it was true and `created_at` is when the agent
learned it. "They moved to Osaka" happened in 2019 and was learned last
Tuesday, and a question about 2019 wants the first.

It also carries its evidence: the words it came from, with what they came
from — a conversation, a message, a document. A sentence with a quote
behind it can be checked; one without cannot, and the dashboard says
which is which.

The evidence is checked where the fact is written, against what the run
actually showed the model. A quote that does not occur in the shown text
is dropped and the fact is marked inferred at half confidence, and the
Knowledge page says "quote not found" beside it; a citation of something
the run never showed loses its evidence altogether. Both the dream's
reading and the after-conversation writer count how often this happened
on their own rows, so a model that paraphrases where it should quote is
visible as a number rather than as a graph full of confident fiction.

**An edge** joins two pages and says how. Not "related": `works_on`,
`member_of`, `knows`, `owns`, `uses`, `located_in`, `decided_in`,
`about`, `part_of`. Each relation knows how to say itself in both
directions — the edge from Alice to Portal reads "works on" from her page
and "is worked on by" from the project's — and an edge may carry a
sentence of its own ("led the controls work until 2025"). An edge nothing
has touched in a long time is said in the past tense, which is the
smallest honest way to show that it may no longer be true.

An edge is also either **stated** or **proposed**. Stated is everything
somebody said — the person in the Link dialog, the model through its
memory tool, the ingest reading a checkout's README — and is the default.
Proposed is the one thing nobody said: a link the generative half of the
night guessed from a walk across the graph. A proposed edge reads as
"perhaps related to X (the agent's guess)" wherever it is written out and
is drawn dashed in the explorers. Nothing promotes it on its own: making
the same link yourself states it, and unlinking drops it.

The guesses made before the distinction existed were not lost with it.
Migration 0088 finds them from what a walk leaves behind — its own
evidence, a `linked` revision in the page's history whose actor is the
dream, and the `linked` proposal the night wrote on its own row — and
marks those proposed. A link any other actor also made stays stated, and
so does one none of the three recognizes: a guess left stated reads as it
always did, while a stated link called a guess drops out of the index and
starts hedging, which is the expensive way to be wrong.

Which commits are **yours** is decided by matching the commit author
against the addresses on the card you marked as yourself, so that card is
what makes a career timeline possible at all. A source that finds commits
by nobody it recognizes keeps those addresses and says so, rather than
quietly attributing none of your work to you. Each span is filed as one
event on `self/work`, a page of its own, so a person with a hundred
checkouts does not get a hundred lines on the page that says who they are.

The first edges come from git, with no model involved: a repository the
person committed to becomes `self works_on <project>`, with the commit
span as its sentence. That matters more than it looks — until something
draws the first edges the graph is a list of pages, and the half of the
dream that looks for connections has nothing to look at.

**A document** is something read from somewhere else: a file in a
checkout, a commit, a chat thread, a note, a message. Documents are not
facts; they are what facts get made out of, and they live beside the
graph with their passages and their vectors.

## What every prompt carries

The top of the graph: the most important pages, each as one line, in a
fixed order. Importance is recomputed by a dream and nowhere
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
reads the same exchange twice and never misses one. A run reads sixty
messages, the oldest sixty, and moves the mark to the last one it was
actually given; where more are waiting it puts itself straight back in
the queue rather than waiting to be noticed again, so a conversation
somebody left running for a week drains sixty at a time. The mark is a
promise that everything behind it has been read, and it was not one: the
run kept the *newest* sixty and moved the mark to the end of the whole
list, so a backlog of two hundred had its first hundred and forty marked
filed unread, and nothing ever came back for them.

A window lands whole or not at all. The reading and the embeddings happen
first — they are calls to another service and have no business holding a
database connection — and then the facts, the links, the strikes that
supersede what they replace, and the mark itself are one transaction.
That was the second way the mark lied. A fact whose write failed was
logged and stepped over while the run reported success, so the mark moved
past the whole window and those messages were never read again; and the
strikes ran afterwards, in transactions of their own, so a page could end
up with its old line struck and nothing standing in its place. Now a
failure leaves the graph and the mark exactly as they were and the job
comes round again. The one thing that can outlive a failed window is a
page opened with no facts on it, which the nightly pass takes away after
two days.

The same job's shape does the reading of sources: `ingest.go` asks an
attached computer for a page of a scan, files the documents, and the
dream turns them into facts. A document a full pass no longer reports is
removed with its passages; a document the source still holds but could
not read is kept. The hashes of everything a source holds go to the daemon once a pass,
under the pass's name, and every later page names the pass instead of
carrying them: for a source of four hundred thousand documents the map is
fifty megabytes, and sending it with every page of two hundred and fifty
entries was most of what a page cost. A daemon restarted mid-pass no
longer holds the map, says so, and is sent it again.

A source is stopped by pausing it, from any of the three surfaces — the
dashboard, `teanode agent knowledge pause`, and the agent's own knowledge
tool — which only clears `enabled`, so every document and passage stays
and resuming carries on from where the last pass got to. Removing a
source is the other thing, and it takes what it found with it: a first
pass over a checkout or a chat archive is hours of reading and the
embeddings that went with it, and there is no way to get them back except
to pay for them again.

## Sources that are scripts

Most of what a person knows is not in a shape this program has a reader
for. It is in a drive, a wiki, a tracker, a mailbox behind a command line
tool, an export somebody downloaded once. So there is one shape that is
not a reader: a `records` source is a folder on an attached computer
holding files of JSON lines, one record a line, each saying what it is
(`kind`), when it happened (`at`), who wrote it (`author`) and what it
says (`text`). A document-kind record is one document; `chat`-kind
records are grouped into threads and windows by the same grouping every
chat goes through. The daemon reads every `.jsonl` and `.ndjson` under
the folder, in sorted path order, skipping dot-names, so a script may
keep its state and its downloads beside the records. A document's
external id is `<file path>#<record id>`, which is why a script that
keeps its ids keeps its documents.

A record may also name the files it came with, in an `attachments` list of
`{"path", "name", "contentType"}` — a picture pasted into a thread, a
document sent with a message. The path is relative to the records folder
unless it is absolute, and is refused if it leads outside the directories
allowed on that computer. Each attachment becomes a document of its own,
of kind `attachment`, identified by the hash of its bytes rather than by
where it sits, so the same screenshot pasted into four threads is one
document. It arrives with no text: the daemon hashes and measures it, and the
server asks for its bytes with the `blob` action and keeps them in object
storage under that hash. A file larger than the limit the source runs
under — `agent.limits.maxScannedAttachmentBytes`, 25 MB by default, or the
source's own — is named on the source's page as passed over and never
uploaded.

The text is made by the night, in two steps, and both are in
`internal/agent/dream_attachment.go`. First it is shown what is free to
know about a batch of these files — the name, the size, the kind of file,
the channel and thread, and the words of the message each arrived with —
and asked which are worth opening, because there are tens of thousands of
them and describing one costs money. A screenshot in a thread about
something going wrong is worth opening; an avatar, a logo, a signature
image or a meme is not. What it passes over carries the reason on its row,
in words, and is never asked about again; nothing is deleted, so a person
who disagrees can read why and put it back. Then what it chose is fetched
out of the store and sent to the scan model as a picture, with a prompt
asking for what the picture shows and for any text in it read out word for
word rather than summarised. What comes back becomes the document's text
through the ordinary path, so from there the passages, the vectors, the
reading that turns documents into facts and the evidence a fact quotes all
work unchanged, and the source's page says how many files are waiting for
a decision, how many the agent declined and how many it read.

Only a picture is opened, and only one small enough to be worth sending:
anything else is passed over with its reason, the same way. Both steps
come off the night's allowance like every other call it makes, so a night
that has spent its share stops asking and the rest waits for tomorrow.

Two kinds of script fill such a folder, and which one to write is
decided by where the records are.

An executable named `refresh` in the folder's root is run at the start of
every pass, before the folder is read, and writes the records. That is
the shape for records that have to be fetched: a tool asked for what
changed, an API paged through. Its output goes to `.refresh.log` in the
folder, truncated each run; a non-zero exit or a timeout fails the pass
with the last lines of its standard error on the source's page, so a
source whose token expired says so rather than quietly holding last
month's documents.

An executable named `records` in the folder's root is the other shape,
and it writes nothing. Run with no argument it prints the names of its
files, one a line, in the style of real ones
(`posts/team/backend.jsonl`); run with one of those names it prints that
file's records on standard output. The names are files that never exist.
Everything past the parser is the same code either way, so a virtual file
files the same documents, under the same ids, with the same hashes, in
the same order as a real file of that name — which is what lets a source
stop copying an archive and start reading it in place without a single
document being filed again. It is read one name at a time, as the pages
ask for them, and a name that cannot be printed is reported as that
file's failure the way an unreadable file is. A folder with a `records`
script needs no `refresh`.

Both run only on the first page of a pass — later pages are the same pass
still being read, and a script run again under them would move the ground
the cursor stands on — as the person, with the folder as the working
directory, in their own environment, with thirty minutes (the server
waits thirty-five for that first page). Each must be a regular file, not
a symlink, executable by its owner and owned by that account: a scan is
the one thing this program does with nobody watching, and the folder
having been allowed by hand with `teanode computer allow` is the consent
for what is in it.

The agent writes these scripts. The knowledge tool's `shape` action is
the record shape and both contracts in words, and asked to index an
archive that is already on the computer it is told to write a `records`
script over the files rather than a `refresh` that copies them.

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

A link's weight fades by elapsed time, not by how often the night ran. The
agent keeps a watermark, `decayed_at`, of when the fade was last
accounted for; each pass fades what nothing touched by how long it has
been since then (half after thirty days, never below a floor in one pass)
and strengthens what was used together over the same real interval. The
first pass writes the watermark and fades nothing. This used to be a
fixed fifth a night, which was right only while a night ran once a day:
bootstrapping runs one every few minutes, and a day of it left every
untouched link at the floor.

## What happens in a dream

A dream is one run of the agent over its own graph, and not only at
night: it runs when the person has been quiet for half an hour, inside
the hours they set for it, at most once every six hours. It has its own share of
the day's tokens (30% by default) — the day's share and not each night's, so
what today's earlier nights spent comes off it, read back from the usage rows,
and four nights in a day cannot spend four shares between them. A call claims
an estimate against the allowance before it is made and corrects it with what
it really cost afterwards, because the reading runs several batches at once and
a check with nothing claimed answers all of them the same. Nothing it
learned is deleted: a
fact it decides against goes dormant, behind the one that replaced it
where there is one, and a page it retires leaves the index and stays. The
only thing it removes is a page that never said anything at all.

Every call a dream makes to a model is a turn of the conversation loop
(`docs/subsystems/the-ask-loop.md`), in a run conversation of its own,
tagged with the dream's job: a batch of documents read, a month written up,
a page divided, a walk judged. The turn is read-only and may reach the
`memory` and `knowledge` tools, so the model can look a page up before it
files to it or read a document whole when its first passage is not enough;
what it changes it changes through the object it ends with, which the code
files with its evidence. `limits.maxRoundsPerDream` (three by default) is
how many times one call may go back to the model. The dream log's Open
button on the agent page shows that dream's runs in the activity table, and
`teanode agent dream runs <id>` lists them for `teanode agent run show`.

**Read what arrived.** Documents, by priority rather than by order: what
the person wrote, then what they took part in, then the rest, newest
first, and a chat archive only where the person was in the thread: a
thread they took part in, of three posts or more, is read before
anything else, and a thread they were not in is never read on its own —
it is searched when a question needs it. A quarter of a million other
people's threads at four hundred a dream is years of reading and none of
the person's business. Nothing is read coarsely any more: titles instead
of contents made pages and no facts — after the pass over what an older build wrote, which asks no
model and runs first so a long reading cannot crowd it out — and among files, the ones written to be read (a readme, a note,
a document) before source code. A checkout is mostly code, and code says
almost nothing about the person: a dream that read four hundred files of
Go filed two facts, with a hundred thousand more behind them. The code
stays indexed for search and is read last. Every bound here is pacing,
never truncation — what is not read
in this dream is read in the next, the backlog is reported as a number the person
can see, and when the backlog is larger than anybody would wait for the
dream reads a stretch at a time instead of an item at a time and marks
that it did, so a later dream can go back over it finely.

**Divide what has outgrown a page.** A page past forty facts is put to
the model once — three to eight themed groups, a slug each, at least
three facts a group — and the facts move to children under it
(`projects/pepsico-carlisle/wes-configuration`), keeping their
identifiers and taking new numbers; both histories say so, and both
pages are due a fresh opening. The person's own page divides the same
way, into topics: who they are stays, their finances go under it. Five
pages a dream. A turn reads twenty facts of a page and the tool shows
sixty, most recently wanted or changed first — by number they were the
oldest twenty, and the fact filed last week never reached a prompt.

**Write up the month.** One call for the month in hand, from a digest
assembled without a model at all (`digest.go`: commits by repository,
the threads the person took part in by channel and by first words, their own
notes, with the noise dropped). Other people's channels are not in it:
a month written from a count of threads the person never opened is a
month about somebody else. Then the months before, most recent first,
six a dream, where the person's own record amounts to something. A
month whose page reads like a guess ("suggests", "likely", "must have")
is owed again, after the months with no page at all, because a page
that says what a count implies is not a diary.

**Rewrite what changed.** A page that gained four facts today is a page
whose opening no longer says what it is about. The same pass merges the
facts that say the same thing — the older keeps its number, the newer
goes dormant carrying a pointer to it. Never deleted: a merge the person
disagrees with can be undone. That pass asks a model, which is what lets
it merge two wordings of one statement. A line the nightly pass finds
says nothing, or finds the page already says in the same words, goes the
same way rather than away. The knowledge page lists them under the facts,
greyed, each saying which number absorbed it.

The fold at the write boundary is the same idea with nobody watching, and
it is deliberately much narrower. A new fact is folded behind an older one
only where the two are the same sentence written twice — identical once
case, spacing and the punctuation words are written with have been taken
off — so a conversation filed today and one filed a week ago end as one
statement with both days' evidence and a dormant row behind it.
Everything else stands, and both rows are recalled.

That is narrow because the search behind it is a vector floor and a name
check, and neither can see the difference between "the rent is 4200 a
month from March" and "the rent is 3100 a month from March". They share
March, they sit on top of each other in the vector space, and one of them
is this year's. A change of amount, date, frequency or who is responsible
carries no negation and reads as a rewording, so the newer row went
dormant behind the older: the page kept last year's figure, normal recall
never carried this year's, and nothing in the conversation said so. Two
candidates that disagree about any number, date or quantity word are now
not twins at all.

The one thing that does put an older fact behind a newer one is a
negation. "She prefers tea" and "she no longer prefers tea" name the same
things and sit on top of each other in the vector space, so neither the
similarity nor the name check can tell them apart — and they are the pair
it matters most not to lose one of. Where exactly one of two sentences
carries a negation both rows stay and the later statement is the one the
page states — but only where the later one is evidenced at least as well
as the earlier. A fact whose quote was nowhere in what the run was shown
is inferred at half confidence because the model may have composed it,
and letting that supersede something the person said is the agent's own
paraphrase winning an argument with its source. The inferred
contradiction stays as a row of its own instead.

**Fold the person into self.** A page under `people` that names the person
themselves — by username, by a word of their name, by its slug — is
`self` under another name, and the dream folds it in. The filing and the
memory tool route such a path to `self` before it is written; this is for
the page that was made before those rules, or by a model that spelt them
differently, and for the conversation that, asked to consolidate the two,
crowned the duplicate.

**File the orphans.** Pages under `notes` are offered a home. A move to a
path that already exists is made; anything else is a proposal with a
button, because a hierarchy invented while nobody is watching is one the
person will not recognize.

Then the dream splits in two.

**The quiet half** is arithmetic and no model runs in it. Importance is
five things: how many facts a page has, how many links, how recently it
was wanted, what kind of thing it is, and a fortnight of grace for being
new — that last one because the other four make a trap between them, in
which a page written in the last dream has never been used, so is not in the
index, so nothing can use it, so it never will be. Every link is
downscaled a little; then any link whose *both* ends were wanted today
rises. Down first and then up, so a link used today ends the dream above
where it started. Importance is recomputed, facts nothing has wanted in
half a year go dormant, and pages that fall under a threshold the graph
sets for itself — mean importance minus a standard deviation scaled by
how far past its intended size the graph has grown — leave the index.
They are marked dormant, never deleted, and a page that has not yet had
forty-five days to be wanted is exempt.

**The generative half** walks the graph from what matters, five steps,
weighted by link strength with enough jitter that it does not take the
same path every dream. It puts the two ends of the walk to the model and
asks whether there is a real relation between them. "No" is the ordinary
answer and the right one: almost everything in a person's life is
reachable from almost everything else in five steps. When the answer is
yes, a typed link is written with the sentence that justifies it, at half
the weight of one somebody stated — it earns the rest by being useful —
and marked proposed, so the agent speaks of it as a guess until somebody
makes the same link themselves.

This is the only phase that adds a relation nobody typed, which is the
whole reason to keep a graph rather than a list.

**Rehearsal** is last, after the vectors are written, because it asks
the graph by meaning and everything filed in this dream has no vector until
then. The agent writes down the questions the person is
most likely to ask tomorrow, tries each against its own memory, and
records the ones it cannot answer. Each question ends one of three ways:
*answered*, when the model names a fact it was shown that answers it;
*gap*, when the graph was asked and had nothing, which is what the person
would hear as "I don't know" tomorrow; or *unknown*, when the question
could not be tried at all — no embedding model, one that did not answer, a
database that did not, a model that did not answer, an answer with no fact
behind it. Every failure path is unknown rather than either of the others,
because a gap that was really a timeout would send the person chasing an
answer their agent already has. Only a search that ran and came back
empty is a gap: the search says so itself now, rather than the phase
asking afterwards whether an embedding model was configured — a provider
that timed out was configured, so an outage read as a graph full of holes.
The judge has to say so too: its answer must carry the field that says
whether the question was answered, or the verdict is unknown. Read as a
plain yes-or-no, an absent field was a no, so any object at all that was
not the one asked for passed for the model having looked and found
nothing. The dream row counts all three,
and the Dreams tab reads "12 rehearsed, 3 gaps, 4 unknown". Nobody reads
the answers; the failures are the point. A gap found at three in the morning costs one model call,
and the same gap found mid-conversation is the person watching their
agent say it does not know. Gaps are written down, never filled: a run
with nobody present inventing answers to its own questions is how a graph
fills with fiction.

## Facts that say nothing

A page already carries three things: its name, what kind of thing it is,
and that it exists. A line repeating any of those is not knowledge, and
it is worse than an empty page, because "Formatting is a project or work
channel." reads like something was learned.

This happened at scale on the first real ingest. A dream working
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
a dream, a source being read), what it moved, and what was there
before. A link files one against *both* pages it joins, each naming the
other end.

Without the "before", a history can be read but not undone, which is the
half somebody wants at the moment they go looking. Without the actor, a
page a dream wrote and a page the person wrote look identical.

A fold and a striking each have their own kind, because a page whose
history says "merged two facts" for three quite different decisions is a
history nobody can act on, and each says why beside it. The one write
that really removes a row is the person's own "forget this", and its
entry carries the whole fact — its kind, its evidence, how sure of it the
agent was, when it was true, who read it — since after that the journal
is all there is.

Every page and every fact also carries the **build that last wrote it**,
which is what lets a newer version of the program go back over what an
older one filed. A graph outlives its code: the rule above did not exist
when those twenty-eight lines were written, and nothing could find them
afterwards except a person with a regular expression. Now the first thing
a dream does is offer everything an older build wrote to the rules as
they stand, strike what fails them — in the page's history, as the
dream's doing, with the words it used to say — and stamp the rest
so tomorrow looks elsewhere. A line an older build worded badly — "1
commits by 1 people, July 2026 to July 2026" — is reworded rather than
struck, since the source it came from may be paused and never say it
again. The same pass removes pages that say nothing at all: no opening,
no facts, nothing under them, no links, two days old. A source names a
channel and makes a page for it; a page that has stood empty for two days
is not going to fill, and is made again if the name comes up. No model
runs in that pass: a rule worth applying to a graph unattended is one
that can be stated in code.

Revision numbers are taken from a high-water mark on the page, the same
way fact numbers are, so they rise and are never reused. A link written
again with nothing changed files nothing — the quiet half writes every
edge every dream, and a history full of changes nobody made is a history
nobody reads.

It is on the page in the dashboard, in `teanode agent memory history
<path>`, in the API, and in the agent's own memory tool as `history`, so
the agent can account for a line the person does not recognize instead of
guessing at where it came from.

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
| a dream's share of the day | 30% |
| how long a dream may run | 45 minutes, the reading at most half of what is left when it starts |
| pages the index is sized for | 400 |
| passages embedded per ingest run | 2000 |

## Recall, once a turn

Words and meaning, both, merged by reciprocal rank fusion — a full-text
search over pages, facts and passages, a vector search over the same, and
one ranked list out. Decay multiplies the score, so a stale fact sinks
without disappearing. There is no minimum word length any more; the
previous version needed a word of four letters before it would search by
meaning at all, so "who is he?" recalled nothing.

The question is embedded once for the turn and the vector is kept by the
words it came from, so the graph and the documents rank against the same
call and a tool that searches for the same thing later in the turn pays
nothing. Nothing is backfilled here either: giving twenty vectorless rows
their vectors, for rows this turn was not going to read, stood between
the person pressing return and the model being asked anything. The
night's embedding stage does it, two hundred at a time, with nobody
waiting.

An expanded page shows the facts the question hit, not the facts that
happen to be oldest. The store hands a page's facts over by number, which
was the whole page while a page held a handful; on a page of fifty to
ninety it meant the five oldest sentences whatever had been asked, and
the matched sentence was left to the loose-fact loop, which by then had
neither a block nor a token to spare. So sixty are read and five are
shown: the ones the search matched first, in the order it ranked them,
then the rest by number. What is shown is laid out by number all the
same, so the block still reads as a page and its `#n` references climb.
A fact keeps its vector when it is struck, folded away or superseded, so
that what a page used to say can still be found; recall is the side that
leaves those out, since a sentence the page has taken back is not one to
put back in its mouth.

What counts as used is what was carried. A page's block is built,
measured against the budget, and only then written and its facts marked —
the other way round, a page that turned out not to fit still moved
`used_at` on every fact in it, and `used_at` is what feeds importance,
decay and tomorrow's index. A page over the budget is passed over and the
next one down is still tried, since a shorter page may fit where a long
one did not; the scan stops only when what is left of the budget could
not hold a page at all.

Being in the index is not being expanded. The index line says what a page
is about, which is not what the page knows, so a page the prompt already
names still gives up its facts when the turn's words hit it — only its
opening is left out, since the index line carries the first sentence of
it already.

### The same search, from anywhere

Searching and reading what the sources indexed is
`internal/agent/indexed`, and every surface calls it: the agent's
`knowledge` tool, the API's `SearchAgentDocuments` and
`ReadAgentDocument`, and `teanode agent knowledge search` and `read`
through those. It is one package rather than three copies because the
tool cannot import the agent and the API cannot import the tool, and
because a second implementation of a ranked search drifts from the first
without anybody noticing which one is wrong.

The half that knows what a question *means* is the agent's, since it
takes an embedding call: in a turn it is the run, which embeds the
question once and keeps it; from the API it is `Agent.KnowledgeMeaning`,
which embeds per search. Where a deployment has no embedding model both
fall back to the words, and the answer says so rather than letting a
reader assume the meaning was searched.

## Bootstrapping

A first ingest brings years of record at once, and a dream that reads two
thousand documents and rests six hours is a dream for a graph that grows
a day at a time. Bootstrapping is the person saying "read it all, as fast
as you can": `teanode agent dream bootstrap on`, or the switch on the
agent page. While it is on, a dream runs again at the very next tick,
waits five minutes after the person's last word rather than thirty, reads
for seventy percent of its time and up to five thousand documents, writes
up twelve owed months and divides ten crowded pages. It switches itself
off when nothing waits to be read.

It is meant for a model of the person's own. Point `models.scan` at it
(an OpenAI-compatible server such as llama.cpp or vLLM, declared as a
provider at zero price) and set `limits.scanConcurrency` to the slots it
has; the reading then costs nothing but the machine's time, and a dream
that would have been a dollar of a metered service is free. On a metered
service bootstrapping is still bounded by the dream's share of the daily
budget.

One dream at a time. The job is queued under the day's date, and a dream
that crosses midnight would otherwise be joined by the new day's at the
next tick: two dreams took every worker slot between them, the ingest
starved, and the second marked the first cut short while it went on
reading. So a dream is not due while a dream job is queued or running for
the agent, bootstrap or not.

## Caveats

- **A dream is paced, not complete.** A large backlog takes several
  dreams, and the log says how many. Nothing is dropped, but a fact from
  a document read in one dream may not be on its page until the next.
- **Rehearsal asks by meaning and not by words.** The word search joins a
  question's words with OR, which is right for recall and wrong for "do
  I know this": it answers yes to almost anything, and a rehearsal that
  never finds a gap is a phase that costs a call and reports nothing.
  And only a fact counts as an answer: a page matching at a quarter's
  similarity says the subject exists, not that the question is answered,
  and counting it made every question answerable. The questions and
  their verdicts are kept in the dream's notes. Where the search cannot
  be made at all — no embedding model, or one that did not answer —
  there are no gaps rather than all of them.
- **The generative half can be wrong.** So a link it writes is stored as
  proposed rather than stated, at half weight, carrying the walk that
  suggested it as its evidence — which is what a person needs to disagree
  with it. A weight is not doubt: nothing downstream reads one, and until
  the status existed the agent repeated a guess it had made at three in
  the morning in the same voice it used for something it had been told.
- **"Which passages still need a vector" is a filter on one table**, not
  a join against the vectors. The model that embedded a passage is
  written on the passage, and the query has no ORDER BY — asking for
  oldest-first meant sorting half a million rows to return a hundred.
- **A scan page is bounded by bytes and by count.** Both, because 256
  commit subjects are nothing and 256 source files are tens of megabytes.
- **Pages are retired, never deleted, by a dream** — but a person
  deleting a page deletes everything under it.
- **Dormant is not gone.** A dormant page or fact is out of the index and
  still findable by search, which is why retiring is safe and why a
  search can return something the prompt did not carry.
