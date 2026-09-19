# What was attached: the pictures and files in a person's records

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file. It builds on
`docs/planning/active/20260916-records-from-anywhere.md`, which defines the
`records` shape this plan extends, and on
`docs/planning/active/20260915-memory-that-learns-the-person.md`, which defines
the graph, the knowledge sources and the night. Everything this plan needs from
either is repeated here, so it can be read on its own.

## Purpose / Big Picture

A person's chat archive is not only what they typed. On the machine this was
written for, the chat export holds 53,660 files beside the messages — 24
gigabytes of them — and the graph knows about none of it. Roughly 47,700 are
pictures, and most of those are screenshots of a terminal, a dashboard, or a
robot cell, pasted into a thread with a sentence like "look at this". The
sentence is indexed. The thing it points at is not.

That is the gap this plan closes. After it, a picture or a file attached to a
record can become a document like any other: the bytes are kept in object
storage, the agent decides whether it is worth reading, and what it reads
becomes the document's text, chunked and searchable and quotable as evidence on
a page. Asked "what went wrong at Carlisle that week", the agent can answer from
the screenshot somebody pasted, not only from the words around it.

You will know it works when this happens. Index a chat archive that has
attachments. Wait for a night. Then ask the agent about something that only
appears inside a screenshot — an error code, a hostname, a value on a dashboard
— and it answers, citing the attachment as its evidence, with the thread it came
from. Before this change the same question returns nothing.

Three terms are used throughout and are defined once here.

A **record** is one line of JSON in a file a `records` source reads. It says
what a thing is, when it happened, who wrote it and what it says. The shape is
defined in `internal/computer/scan_records.go` in the Go type named `record`.

The **computer daemon** is the program a person runs on their own machine
(`teanode computer daemon`) so the server can read files there. It reads, it
never thinks: it has no model and makes no model calls. Its code is
`internal/computer/`.

The **night**, also called a dream, is the background run that reads whatever
the sources have indexed and writes what it learned onto pages. Its code is
`internal/agent/dream.go`. It has a token budget for the day and decides for
itself what to read.

## Progress

- [x] Milestone 1: a record can name its attachments, and the daemon reports
      them as documents with their bytes in object storage — PR #122
- [x] Milestone 2: a document's bytes can be fetched back from storage, and a
      document nobody can read yet says so on the source's page — PR #122
- [x] Milestone 3: the night decides whether an attachment is worth reading,
      from what it knows for free — PR #123
- [x] Milestone 4: the night reads a picture with a vision model and files what
      it saw — PR #123
- [x] Milestone 5: local preparation — a record may give a file's text, and the
      daemon reads what it can here — PR #125
- [x] Milestone 6: the dashboard shows an attachment on the page it belongs to
      — PR #126
- [ ] Milestone 7: a file reaches the conversation — the agent can look at one
      again, and a person can see the one an answer rests on

## What exists today, and where

Read this section before touching anything; it is the map.

`internal/computer/scan_records.go` reads a folder of `.jsonl` files, or runs an
executable named `records` in that folder which prints the names of virtual
files and, asked for one, prints its lines. Each line becomes a `record`. A
record of a document kind becomes one `ScanEntry` through the function
`documentEntry`; records of kind `chat` are grouped into threads and windows by
`chatUnitsOf`. A `ScanEntry` is defined in `internal/computer/scan.go` and is
what the daemon sends the server: an external identifier, a kind, a title, a
URL, a hash, the text, and some dates.

The `record` type has no field for an attachment. That is the first thing this
plan changes.

`internal/computer/scan.go` also holds `textOf`, which decides what a file's
text is: anything that looks like UTF-8 is taken as it stands, a `.pdf` goes
through the `pdftotext` program, and office documents go through `soffice`. A
file it cannot read is refused with the error "not text". The function
`availableExtractors` reports which of those programs exist on the machine, so
the source's page can say what it cannot read there. Pictures are refused.

`internal/models/knowledge.go` defines `AgentDocument`, which already carries a
field named `StorageKey`, and `internal/db/database_knowledge.go` reads and
writes the `storage_key` column. Nothing ever sets it: on the machine this was
written for, 0 of 588,331 documents have one. The field was reserved by the
original design for exactly this purpose and is finally used here.

`internal/storage/` is the object store. `storage.go` is the interface,
`filesystem.go` keeps blobs in a directory, and `s3.go` mirrors them to anything
that speaks the S3 protocol. It is configured under `storage.s3` in
`internal/config/config.go`. On the machine this was written for, a MinIO
service has been running beside the server for weeks and already holds mail.

`internal/agent/dream.go` is the night. The variable `dreamTools` on line 372
names the tools a night may use, and today it is exactly two: `memory` and
`knowledge`, both read-only. `digestBatch` shows the model a batch of documents,
each rendered by `openingOf`, which returns the first 1,200 runes of a
document's first chunk. A night therefore cannot reach the person's machine and
cannot see a picture.

`internal/llm/openai.go` and `internal/llm/anthropic.go` can both already send a
picture to a model — the first as an `image_url` part, the second as a base64
image block. This is used when a person attaches a picture to a conversation. No
background run uses it.

`internal/computer/computer.go` is the daemon's side of the connection. Its
`filesystem` tool already supports `read`, `fetch` (which returns a file's bytes
and detects its content type), `put` and `write`, and its `shell` tool runs
commands. These are reached by the agent during a conversation, never by a
night.

## Milestone 1: a record can name its attachments

At the end of this milestone, a records script can say that a message had files
with it, the daemon uploads those files to object storage, and each becomes a
document with an empty text and a storage key. Nothing reads them yet. You will
see new rows in `agent_document` whose `bytes` is non-zero and whose
`storage_key` is set, and you will see the objects in the store.

Add to the `record` type in `internal/computer/scan_records.go` a field named
`Attachments`, a list of a new small type with three parts: a `Path`, which is
where the file is on this machine, a `Name`, which is what to call it, and an
optional `ContentType`. A path is resolved relative to the records folder unless
it is absolute, and an absolute path outside the roots the person allowed is
refused the way the rest of the daemon refuses them — see `resolve` and the
allowed-roots check in `internal/computer/computer.go`, and do not weaken it.

For each attachment the daemon makes one `ScanEntry` of a new kind, `attachment`
(add it beside `DocumentFile` in `internal/models/knowledge.go`). Its external
identifier must be stable and must not collide across scripts, so use the same
rule the existing entries use: the virtual file's relative name, a `#`, then the
attachment's own identity, which is the hash of its bytes. Using the hash means
the same screenshot pasted into four threads is one document, which is what you
want.

The entry's hash is the hash of the bytes. Its text is empty. Its title is the
attachment's name. Its metadata carries the record it came with: the thread, the
channel, the author, and the identifier of the record itself, so that a later
milestone can show the model what was said around the picture.

Uploading is the daemon's job because the daemon is the only thing that can see
the file. Add to the daemon protocol a way to send a blob, next to the existing
`scan` and `filesystem` actions in `internal/computer/computer.go`, and have the
server put it in the store through `internal/storage` and record the key on the
document. Key the object by the hash, so an upload of a file already in the
store is a no-op and re-scanning costs nothing.

An attachment larger than the limit is not reported and not uploaded. The limit
is a setting, not a constant: a server-wide default under `agent.limits` in
`internal/config/config.go`, twenty-five megabytes to begin with, and a value on
the knowledge source itself that overrides it where one archive deserves
different treatment from another. The daemon is told the number rather than
knowing it, so the decision stays on the server; a daemon told nothing falls
back to the default rather than treating it as unlimited. The daemon says so on the source's page, in the same place it says
what it cannot read, so a person can see what was passed over rather than
wondering. Twenty-five megabytes takes in every screenshot and nearly every
document while leaving out the videos and the disk images, which are the things
that would fill a store without teaching the agent anything.

The decision to upload every attachment rather than only the ones that turn out
to be worth reading was made deliberately; see the Decision Log.

To see it work, on a machine with a chat archive:

    cd ~/chat-records
    ./records | head -3
    teanode agent knowledge sync chat
    teanode agent knowledge list

The source's document count rises by the number of attachments. Then, against
the server's database:

    select count(*), sum(bytes) from agent_document where kind = 'attachment';

and against the object store, the same number of objects.

## Milestone 2: the bytes can be fetched back, and unread documents say so

At the end of this milestone, the server can read an attachment's bytes out of
the store without the person's machine being attached, and the dashboard's
source page says how many documents are waiting for something that can read
them. This matters because a night runs at three in the morning when a laptop is
shut.

Add a function to `internal/agent/` that takes a document and returns its bytes
from `internal/storage`, and a test that writes a blob, records the key, and
reads it back.

A document with no text is not a failure and must not be reported as one. Extend
whatever the source's page shows — see `internal/agent/reading/reading.go`,
which computes what a source has read and what waits — so that documents with no
text are counted separately and described in plain words, for example "1,020
files nothing here can read yet". Do not let them count as read, or the reading
progress will lie.

## Milestone 3: the night decides what is worth reading

At the end of this milestone the night looks at an attachment and decides,
before spending anything, whether to open it. This is the heart of the plan and
the reason it is affordable.

The night already walks documents in batches in `digestBatch`. An attachment has
no text, so `openingOf` returns nothing and the model is shown a heading and
silence. Change that: for a document of kind `attachment`, show the model what is
free — the file's name, its size, its content type, the channel and thread it
came from, and the text of the record it was attached to, which the metadata
from Milestone 1 carries. Ask, in the same object the digest already returns,
whether this one is worth opening.

Name the choice in the prompt in plain words, because the model is being asked
to spend the person's money: a screenshot in a thread about a failure is worth
opening; an avatar, a logo, a meme in a social channel, or a picture smaller
than a few kilobytes is not.

Record the answer on the document so it is not asked twice. A document the night
declined should say so and why, and a person should be able to overrule it
later, which argues for storing the decision rather than deleting the document.

Acceptance is a test in `internal/agent/` that runs a digest over a batch of
attachments with a scripted model and asserts that the ones it declined are
marked declined and cost nothing further.

## Milestone 4: the night reads a picture

At the end of this milestone, an attachment the night chose to open is described
by a vision model and the description becomes the document's text, chunked and
searchable like any other.

The model layer can already carry a picture: see the `image_url` part in
`internal/llm/openai.go` and the base64 image block in
`internal/llm/anthropic.go`. What does not exist is a caller in a background run.
Add one, on the scan model, with the cost taken off the night's budget exactly as
`dreamThought` does, so a night that has spent its allowance stops asking.

The prompt should ask for what a person would want months later: what the
picture shows, and any text in it that carries meaning — error messages,
identifiers, values — read out rather than summarised. A sample of this against
a real screenshot from the archive this plan was written for produced: "The
meaningful status reads 'Container is empty' with timestamp '2020/10/16 20:26:21
JST' and scene identifier 'test201016_0004.mujin.dae'." That is the standard to
aim at.

The description is the document's text. Everything downstream — chunking,
embedding, the digest that turns documents into facts, the evidence a fact
quotes — then works unchanged, which is the point of putting it here rather than
inventing a parallel path.

Guard the size. A four-thousand-pixel screenshot costs several times what a
thousand-pixel one does for no extra meaning; Milestone 5 provides the
downscaling, and until it exists this milestone should refuse anything above a
few megabytes rather than spend the budget on it.

To see it work, after a night:

    teanode agent knowledge search "container is empty"

returns the passage, and `teanode agent knowledge read <document-id>` shows the
description with the attachment's name and thread.

## Milestone 5: local preparation, without a model

At the end of this milestone the cheap work happens on the person's own machine,
free, and only what is left goes to a model.

The records script is the right home for this, because it already runs on that
machine as that person and needs no new privilege. A script can downscale a
picture before it is uploaded, pull a frame out of a video, convert a
spreadsheet to comma-separated text, or run a local text recogniser and put what
it found in the record. Adding a capability then means editing a script rather
than releasing a version, which is the whole reason the `records` shape exists.

Document this in `docs/subsystems/memory.md` beside the existing description of
the records shape, and write the example script. On the machine this plan was
written for, `ffmpeg`, `pdftoppm`, `unzip` and `7z` are installed;
`tesseract` and ImageMagick are not, and are worth installing for text
recognition and downscaling respectively.

Where a script has already extracted text, the attachment arrives with text and
the night has nothing to decide: it reads it like any other document and no
picture is ever sent to a model. Where the script could not, the document arrives
empty and Milestones 3 and 4 take over. Make sure both paths are exercised by
tests.

An alternative was considered and rejected: giving the night the computer tools
directly, so that it could run a program on the person's machine while they
slept. See the Decision Log.

## Milestone 6: the dashboard shows what was attached

At the end of this milestone a person can see an attachment where it belongs. On
the Knowledge page, a fact whose evidence is an attachment shows the picture, or
the file's name where it is not a picture, with the thread it came from. On the
agent's Dreams tab, the source's card says how many attachments are waiting, how
many were read, and how many the agent declined and why.

Follow `docs/coding/frontend-design.md`, keep every string in the three
catalogues, and check it in Chrome at both a desktop width and a phone width
before opening the pull request.

## Milestone 7: a file reaches the conversation

The first six milestones put a picture in the graph and on the Knowledge page.
They leave it out of the one place a person spends their time: the
conversation. Two things are missing, and they are worth naming apart because
they fail for different reasons.

The agent cannot look at a picture again. Recall carries text into a turn, so a
fact read out of a screenshot arrives as the sentence the night wrote about it.
That answers "what did that error say" and nothing the night did not happen to
write down. The plumbing for the fix already exists: Milestone 4 gave a turn a
way to carry pictures, for the night's own use. A `look` on the memory tool,
given a fact or a file, would fetch the bytes and put them in the turn the same
way, and the agent could answer from the picture rather than from a description
of it. It costs what a night's look costs, and it is the person asking, which
is the right moment to spend.

A person cannot see the picture an answer rests on. An assistant's line in the
drawer carries text and nothing else; only a person's own message may carry a
file. The fix that fits this codebase is not to teach the model to attach
things. The agent already cites what it used, as `work/mcx#3`, and has done
since the graph was built. The drawer should read those citations and, where
the fact behind one was read from a file that is still kept, show it: a
thumbnail under the message, the file's name where it is not a picture. Nothing
the model does has to change, it works for every citation already written, and
a person sees the evidence rather than being asked to take it on trust.

## Surprises & Discoveries

An attachment with no text would have been offered to the night by
`ListAgentDocumentsToDigest`, which selects anything not yet marked digested.
The night would have shown the model a heading and silence, learned nothing, and
marked the document read — and read is the one state a document must not reach
without having been read, because nothing goes back for it afterwards. Fifty
thousand files would have been quietly consumed on the first night after the
plumbing landed. A file with no passages is now excluded from that query, and
becomes eligible the moment something gives it passages.

There are already two different limits called an attachment limit. The existing
`agent.limits.maxAttachmentBytes` bounds what a person uploads to a
conversation. Reusing it here would have let a number someone chose for their
own uploads silently bound an unattended archive scan, so scanning has its own,
`maxScannedAttachmentBytes`, and both comments say why they are apart.

The computer protocol is an exact-match check: a daemon whose number differs is
refused outright. So a new action cannot come with a protocol bump without
detaching every daemon that has not been updated. The `blob` action was added
without one, which is safe because a daemon that does not know it simply never
reports an attachment to ask about.

The `AgentDocument` type has carried an unused `StorageKey` field and a
`storage_key` column since the memory graph was built. Nothing has ever set it,
and on the machine this plan was written for, 0 of 588,331 documents have one.
The original design reserved a place for document bytes and never filled it;
this plan fills it rather than inventing something new.

A night cannot reach the person's machine at all. `dreamTools` names exactly two
tools, `memory` and `knowledge`, and the frame it is given says they are for
looking. This was not obvious from the outside and it shapes the whole design:
preparation has to happen during the scan, not during the night. *(That was
true when this was surveyed, and the design above was built on it. The owner
has since reversed it — see the Decision Log — and a night now has every tool.
Nothing in the milestones depends on the old answer; it only means preparation
has a second place it can happen.)*

The model layer has been able to send a picture all along, in both the OpenAI
and Anthropic paths. The capability was built for conversations, where a person
attaches something, and no background run has ever used it.

A single real screenshot from the archive, given to the configured model, came
back with the status line, the timestamp and the scene file identifier read
correctly out of the image. The quality question is settled; the open questions
were only ever cost and plumbing.

## Decision Log

**Attachments and vision are one feature, not two.** Carrying attachments
without being able to read a picture reaches about a ninth of the files in the
archive this was written for, since 47,700 of 53,660 are images. Reading a
picture without the attachment plumbing leaves the description with no document
to belong to and no thread to cite. They ship together.

**The agent decides what is worth reading, per file, before spending.** The
owner asked for this and it is also what makes the cost bearable. A described
image costs roughly two thousand input tokens and a hundred output; at the rates
of the cheap model that is about twenty-seven dollars for all 47,700, and rather
less once most are declined. The decision belongs in the night because that is
where the budget and the judgement already are.

**Blobs go in object storage, never in Postgres.** The owner was explicit. The
store already exists, `StorageKey` already exists, and 24 gigabytes in the
database would land in every backup of it.

**Every attachment is uploaded, not only the ones that are read.** The owner
chose this. It costs the space and means a decision the agent got wrong can be
revisited later without the person's machine being attached again. The opposite
choice, uploading on demand, keeps the store small but ties the first read to the
laptop being awake.

**Twenty-five megabytes is the default limit, and it is configurable per
source.** The owner set both the number and the shape. Twenty-five is generous
enough for screenshots and documents, which is where the meaning is, and mean
enough to leave out video and disk images, which are most of the bulk. One
archive may deserve a different number from another — a folder of design
drawings is not a chat export — so the source carries an override and the
server-wide value is only the default.

**Preparation lives in the records script, not in the night.** The night could
have been given the computer tools — `shell` and `filesystem` both exist and
would work. It was rejected because an unattended nightly run that can execute
programs on a person's machine is a materially different risk from a
conversation where they are present and watching. The records script runs on the
same machine, as the same person, and is something they or their agent wrote and
can read.

*Reversed by the owner, after reading the above.* The risk is real and the
owner accepted it: "I accept this risk, allow dream to use all tools." The
night is no longer given a named pair of tools. It is given the whole kit —
everything an attached conversation has, the person's own computer among it —
and the computer's tools are in its first round rather than behind a search,
so it does not spend a round of its allowance finding out that a machine is
attached. Its budget is unchanged; what it may spend in a night is what it
could spend before.

What this opens is that preparation no longer has to happen ahead of the
night. A records script is still the right place for records that have to be
fetched on a schedule, and nothing about it changes; but a night that finds a
file it cannot read, or a question it could answer by running something, can
now do that in the night rather than leaving a note asking for a script to be
written. The reading and the filing of orphans — the two calls that decide
where things go — are the calls that have it; a call that answers from its
prompt still gets no tools at all.

Two things still stand in the night's way, and both are deliberate. A call
that would raise a confirmation card — anything destructive, anything that
leaves the server, anything that hands out a way in — is refused outright,
because there is nobody there to be shown the card; the night is told to say
what it would have done instead of looking for another route. And changes to
the graph are still made from the object a call ends with rather than by hand
with the memory tool. That second one is not a permission any more, and the
frame says so in as many words: it is how filing works here, and it is what
keeps a fact attached to the evidence it came from and a move a proposal the
person can still refuse.

## Outcomes & Retrospective

Not yet. Fill this in when the milestones land: what was achieved, what was left
out, what the costs turned out to be against the estimates above, and what the
next person should know.
