# Records from anywhere: one common shape for every source, and a script the agent writes to fill it

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file. It builds on
`docs/planning/active/20260915-memory-that-learns-the-person.md`, which is
checked in and describes the memory graph, the knowledge sources and the
dream; and on `docs/planning/active/20260916-every-model-call-is-a-run.md`,
which put every model call through the conversation loop. Everything this
plan needs from either is repeated here.

## Purpose / Big Picture

Today the agent can index three shapes of thing on the person's computer: a
tree of files, a folder of dated notes, and a Mattermost export. Each shape
is a reader written in Go inside the `teanode computer` daemon (the program a
person runs on their own machine so the agent can reach it), and adding a
fourth shape means adding a fourth reader. The person's knowledge is not in
three shapes. It is in Google Drive and Gmail, reachable through the `gog`
command line tool; in Confluence, reachable through the `confluence` command
line tool; in Slack and Discord exports; in whatever the next tool exports.
Every one of those is "run a command, get JSON, turn it into documents".

After this plan there is a fourth shape, `records`, and it is the last one
that needs to be written in Go. A `records` source is a folder holding files
of JSON lines, one record a line, in one common shape this plan defines:
what it is, when it happened, who wrote it, what it says. The daemon reads
that folder the way it reads the others, pages it to the server, and the
server files documents from it exactly as it files documents from a file
tree. Chat-shaped records are grouped into threads and windows by the same
code that groups the Mattermost export, so a Slack export and a Mattermost
export produce the same kind of document.

Who fills the folder is the point. A folder may hold an executable named
`refresh`; the daemon runs it at the start of every scan, and it is that
script that runs `gog` or `confluence` or reads an export and writes the
records. The person can write it, and the agent can write it for them: asked
"index my Confluence space DEV", the agent opens a shell on the computer,
writes `~/.teanode/records/confluence-dev/refresh`, runs it once to see the
records come out, adds the folder as a `records` source, and the nightly
scan keeps it current from then on. The agent knows the record shape because
the knowledge tool's guidance carries it.

A person can see it working by adding a `records` source over a folder with
one `refresh` script in it, running `teanode agent knowledge sync` on it, and
finding the documents in the dashboard's knowledge page, then asking the
agent a question only those documents answer.

## Progress

- [x] (2026-09-17 01:05Z) Research: mapped the source model, the daemon's
  scan and its paging, the server's ingest, the digest's metadata contract,
  and the two command line tools on the person's machine. Findings are in
  Context and Orientation.
- [ ] Milestone 1: the daemon reads a `records` folder (`scan_records.go`),
  with the chat grouping shared with the Mattermost reader, and tests.
- [ ] Milestone 2: the daemon runs `refresh` before a scan, with a timeout,
  and reports its failure as the scan's failure.
- [ ] Milestone 3: the server, the command line, the knowledge tool and the
  dashboard know the format; `message` and `page` kinds survive filing.
- [ ] Milestone 4: the agent can write a `refresh` script from the guidance
  alone; a Confluence source and a Google Drive source on the maintainer's
  machine, tested on a subset, then the whole.
- [ ] Milestone 5: docs (`docs/subsystems/memory.md`, `docs/reference/command-line.md`,
  `docs/configuration.md` if a setting appears) and the retrospective.

## Surprises & Discoveries

- Observation: the format list lives in two places that do not share code,
  `internal/models/knowledge.go` and `internal/computer/scan.go`, because
  the daemon is built into the same binary but must not import the server's
  models. A new format is added in both, and `Validate()` on a source never
  checks the format, so a typo only fails when the daemon says
  `"x" is not a shape this program can read`.
  Evidence: `internal/computer/scan.go:236-253`, `internal/models/knowledge.go:56-67,153-190`.
- Observation: the daemon's scan is the one action that runs with nobody
  watching, and its only guard is the list of allowed roots in
  `~/.config/teanode/scan-roots.json` on the daemon's machine. The `shell`
  action, by contrast, is always with the person present and goes through a
  confirmation card when the command looks risky. Running `refresh`
  unattended is therefore a new trust decision, resolved in the Decision Log.
  Evidence: `internal/computer/scan.go:30-35,263-281`, `internal/computer/policy.go:1-26`.
- Observation: the digest reads a fixed metadata vocabulary. A chat document
  without `participants` is dropped from the month's write-up entirely;
  `channel` and `posts` order the threads; a commit needs `address` and
  `repository`. Records must carry these names or they are invisible to the
  dream's timeline.
  Evidence: `internal/agent/digest.go:223-282`.

## Decision Log

- Decision: one format, `records`, whose records carry a `kind`; chat-kind
  records are posts to be grouped, every other kind is one document a
  record. Not two formats (one for chat, one for documents).
  Rationale: a Slack export and a Confluence space are both "a script wrote
  JSON lines"; the difference is only whether a record is already a unit of
  meaning. A Confluence page is; a chat post is not, and needs its
  neighbours. The kind says which, and the grouping code decides.
  Date/Author: 2026-09-17, the agent, agreed in conversation with the maintainer.
- Decision: the script is a file named `refresh` in the folder's root, run by
  the daemon at the start of a scan, rather than a command stored on the
  source in the server's database.
  Rationale: what runs on the person's machine should be on the person's
  machine, where they can read it, edit it, and delete it, and where the
  server cannot change it. The folder already has to be under an allowed
  scan root, which the person grants by hand with `teanode computer allow`;
  that grant is the consent for what the folder holds, script included. A
  command in the database would be the server telling the daemon what to
  run, which is what the scan's design refuses.
  Date/Author: 2026-09-17, the agent.
- Decision: `refresh` runs unattended, as the person, with a timeout, and
  only when it is a regular file, executable, owned by the person, and
  inside the folder being scanned (not a symlink out of it).
  Rationale: the point is a nightly refresh. The checks make sure the script
  is one the person (or their agent, with the person present) put there.
  Date/Author: 2026-09-17, the agent.
- Decision: the Mattermost reader stays, and the grouping it uses moves into
  a shared function both readers call. It is not rewritten as a `refresh`
  script in this plan.
  Rationale: it works, it is tested, and the person's archive is what it
  reads. The shared grouping is what generalizes; the Mattermost walk is a
  hundred lines that cost nothing to keep. A later plan may retire it once
  the archiver writes records itself.
  Date/Author: 2026-09-17, the agent.
- Decision: the agent learns the record shape from the knowledge tool's
  guidance, not from a skill.
  Rationale: skills are declarations of tools the agent calls in a
  conversation; the shape of a file is a fact the agent needs when writing
  a script, which the guidance already exists to carry.
  Date/Author: 2026-09-17, the agent.

## Outcomes & Retrospective

To be written at the end of each milestone and at completion.

## Context and Orientation

TeaNode is a mail server with a personal agent. The agent lives in the Go
server in `internal/`; the dashboard is a React application in `web/`. A
person may run `teanode computer` on their own machine: a daemon (a program
that stays running in the background) that connects out to the server over
a websocket and does things there on the agent's behalf. Its code is in
`internal/computer/`, built into the same `teanode` binary.

A *knowledge source* is somewhere the agent indexes. The model is
`AgentKnowledgeSource` in `internal/models/knowledge.go` (line 105). Its
`Kind` is one of `computer` (a folder on an attached computer), `archive`
(the same, for an export), `skill`, `web`, `sent`. Its `Specification`
(line 71) is one JSON blob with, among other fields, `Computer` (which
attached computer), `Path` (the folder), `Format` (how to read it: `files`,
`mattermost`, `journal`; constants at line 56), `Include` and `Exclude`
(globs). Because the specification is a blob, a new field needs no
database migration. `Validate()` (line 153) checks the kind and the
required fields but never the format.

A source is created three ways, and all three must learn a new format:

- The GraphQL mutation `SaveAgentKnowledgeSource` in
  `internal/api/v1api/apigraph/agent_graph.go` (line 994), whose arguments
  (line 261) include `format`.
- The command `teanode agent knowledge add <name> <path> --format ...` in
  `internal/cmd/agent_graph.go` (line 147; the flag's usage string at 151-157
  lists the formats).
- The dashboard's add dialog in `web/src/pages/agent.tsx` (line 1598), whose
  format `<select>` at line 1644 lists `['files','mattermost','journal']`
  with labels under the i18n keys `agent.knowledgeFormat.*` in
  `web/src/i18n/en.ts` (about line 2479), `ja.ts` and `zh.ts`.
- The agent's own knowledge tool in
  `internal/agent/tools/knowledge/knowledge.go`, whose `add` action takes a
  `format` (line 60) and whose description (line 47) is the guidance the
  model reads.

The daemon's reading is `RunScan` in `internal/computer/scan.go` (line 226).
It takes `ScanArguments` (line 77): `Root`, `Format`, `Include`, `Exclude`,
`Known` (a map from a document's external id to the hash the server already
holds, so unchanged things are named but not sent again), `Allowed`, `After`
(the cursor: where the last page stopped), `Most` (page size, at most 256).
It answers a `ScanResult` (line 152): `Entries`, `Next` (the cursor for the
next page, empty when done), `Sensitive`, `Refused`. Each `ScanEntry` (line
105) has `ExternalID` (what the server files it under; JSON name `id`),
`Kind`, `Title`, `URL`, `Size`, `Hash`, `ModifiedAt`, `HappenedAt`, `Text`,
`Unchanged`, `Refused`, `Metadata`, `Private`. `RunScan` first resolves the
root against the allowed roots (`allowedRoot`, line 263), which are in
`~/.config/teanode/scan-roots.json` on the daemon's machine and are added
with `teanode computer allow <path>`; a root outside them is refused. Then
it dispatches on the format (line 236): `scanFiles`, `scanJournal` (line
1020, the simplest reader and the template to copy), `scanMattermost` in
`internal/computer/scan_chat.go`.

The Mattermost reader is the one to generalize. It reads `users.json`,
`channels.json` and `posts/<team>/<channel>.jsonl`, and cuts each channel
into *units*: a thread (a root post and everything that replied to it), else
a *window* of consecutive posts with no silence over thirty minutes, at most
forty posts and three thousand characters (`chatGap`, `chatWindowPosts`,
`chatWindowCharacters`, line 36). A channel where more than eighty percent
of posts are an integration's is refused as a bot channel. Each unit is one
entry: `Kind: "chat"`, `Title` is the channel and the day, `Text` is
`HH:MM username: message` lines, `Metadata` carries `team`, `channel`,
`participants` (the usernames who spoke), `posts` (how many), `purpose`;
`Private` for private, direct and group channels; `HappenedAt` the first
post's time; `ExternalID` is `<file>#<first post id>`. The cursor is the
file, or `<file>#<last unit sent>` when a page stops mid-file. The functions
are `readChannelFile` (line 211), `renderChat` (line 365), and the paging
loop in `scanMattermost` (line 96).

On the server, `runIngest` in `internal/agent/ingest.go` (line 132) asks the
daemon for pages through `readFromComputer` (line 301): it loads the known
hashes, sends `device.Ask(ctx, "scan", &computer.ScanArguments{...})` (line
321), and files each entry with `fileDocument` (line 469), which maps the
entry's kind to a document kind with `documentKindOf` (line 510): `commit`,
`chat`, `journal`, `page` map to themselves and everything else becomes
`file`. The one format-specific line on the server is 317: a Mattermost
source's page holds 2048 entries rather than 256, because chat units are
small. Documents are rows of `agent_document` with `external_id` unique per
source; `PutAgentDocument` in `internal/db/database_knowledge.go` (line 341)
upserts on it.

The dream's digest (`internal/agent/digest.go`, line 52) writes a month's
page from the documents that happened in it (`HappenedAt` places them) and
reads metadata by name: for `chat` documents `participants` (a list of
names, matched against the person's username and the words of their name;
a chat document without it is dropped), `channel`, `posts`; for `commit`
documents `address` and `repository`. `journal` documents contribute their
titles. Other kinds are searchable but not in the write-up.

The daemon can also run a command for the agent: the `shell` action
(`RunShell`, `internal/computer/computer.go` line 377) runs a command as the
person through their shell, but only while the person is in the
conversation, and a command the policy in `internal/computer/policy.go`
finds risky puts a confirmation card in front of them. The scan is the one
unattended action and is guarded by the allowed roots instead.

On the maintainer's machine two command line tools are installed and signed
in. `gog` (build 0.11.0) reaches Google: `gog drive ls --json`, `gog drive
search <query> --json`, `gog drive download <fileId>`, `gog gmail search
<query> --json`, `gog gmail get <messageId> --json`; every command takes
`--json`. `confluence` reaches Atlassian Confluence: `confluence spaces`,
`confluence search <query> --cql --limit N`, `confluence read <pageId>
--format markdown`, `confluence info <pageId>`. Both print JSON a script can
turn into records.

Terms used below. A *record* is one line of a records file. A *unit* is one
document the server files: one record for a document kind, a thread or
window of records for the chat kind. The *cursor* is the string the server
keeps between pages so a scan resumes where it stopped.

## The record shape

A `records` folder is any folder the person has allowed. Inside it the
daemon reads every file ending in `.jsonl` (also `.ndjson`) at any depth,
in sorted path order, skipping names that start with a dot. Any other file
is ignored, so a script may keep its state, its downloads and its logs
beside the records. One line is one record, a JSON object with these
fields; only `id` and `text` are required:

    {
      "id": "page:123456",
      "kind": "page",
      "title": "Deployment runbook",
      "url": "https://example.atlassian.net/wiki/spaces/DEV/pages/123456",
      "at": "2026-08-14T09:30:00Z",
      "modifiedAt": "2026-09-01T17:02:11Z",
      "author": "ziyan",
      "text": "...the page's content as plain text or markdown...",
      "private": false,
      "channel": "",
      "thread": "",
      "participants": [],
      "metadata": {"space": "DEV", "version": 7}
    }

`id` is the record's identity within the folder; the document's external id
is `<file path>#<id>` so two files may reuse ids without colliding, and a
script that rewrites a file keeps the same ids for the same things so the
server sees them as unchanged when their text is unchanged. `kind` is one
of `page` (a wiki or web page, a Drive document), `file` (a file's
contents), `message` (a mail message), `journal` (a dated note), `commit`,
`chat` (one post in a conversation, to be grouped); missing means `page`.
`at` is when it happened, RFC 3339; a record without it is filed but never
appears in a month's write-up, so scripts should fill it. `author` is who
wrote it. `private` marks a document the agent must not quote to anybody
else. `metadata` is kept as given and shown on the document.

For `chat` records three more fields matter. `channel` names the
conversation the post belongs to; posts are grouped within a channel and a
file, in time order, so a script writes one channel's posts together and in
order. `thread` is the id of the post this one replies to, or of the
thread's root; posts sharing a `thread` become one unit with the root. Posts
with no `thread` are cut into windows by the same silence, count and size
bounds as Mattermost. `author` is the poster's name, which the unit's
`participants` is built from, so a person's own name here is what the
digest recognizes as theirs; `participants` on a chat record is ignored.
A unit's title is the channel and the day; its text is `HH:MM author: text`
lines; its `Metadata` carries `channel`, `participants`, `posts`, and any
`metadata` of the first post.

Lines that are not valid JSON, and records with no `id` or an empty `text`,
are skipped and counted; a file with nothing readable in it is reported as
one refused entry saying so, the way an unreadable channel file is today,
so the source's page shows it rather than silently missing it.

## Milestone 1: the daemon reads a records folder

At the end of this milestone `teanode computer` answers a scan with
`Format: "records"` over a folder of `.jsonl` files, paged and hashed like
the other readers, with chat records grouped into the same units the
Mattermost reader makes. Nothing on the server knows yet; the proof is the
daemon's tests.

First the shared grouping. In `internal/computer/scan_chat.go` the unit
cutting and rendering are written against `mattermostPost` and
`mattermostUser`. Introduce a small neutral type in a new file
`internal/computer/chat_units.go`:

    // chatPost is one post of any chat, as the readers hand it to the
    // grouping: the Mattermost reader from its export, the records reader
    // from a record.
    type chatPost struct {
        ID       string
        Thread   string    // the root this replies to, or empty
        Replied  bool      // a root with replies elsewhere
        At       time.Time
        Author   string
        Text     string
        Metadata map[string]any
    }

    // chatUnits cuts a channel's posts into threads and windows and renders
    // each as one entry. relative is the file the posts came from, channel
    // its name; private marks every unit.
    func chatUnits(relative, channel string, posts []chatPost, private bool) []ScanEntry

Move the thread and window logic of `readChannelFile` (from "Threads first"
to the final sort) and `renderChat` into `chatUnits`, working on
`chatPost`. `readChannelFile` keeps its own reading of the export, its bot
channel refusal, and its sorting, then converts each `mattermostPost` into
a `chatPost` (`Thread` from `RootID`, `Replied` when `ReplyCount > 0`,
`Author` from the user's username or "somebody") and calls `chatUnits`,
adding `team` and `purpose` to each entry's metadata afterwards. The tests
in `internal/computer/scan_test.go` that cover Mattermost must pass
unchanged; that is the proof the move lost nothing.

Then the reader, in a new file `internal/computer/scan_records.go`, shaped
on `scanJournal` and `scanMattermost`:

    // scanRecords reads a folder of JSON lines, one record a line, in the
    // one shape every script writes. Document-kind records are one entry
    // each; chat-kind records are grouped into units by chatUnits.
    func scanRecords(root string, arguments *ScanArguments, most int) (*ScanResult, error)

    // record is one line of a records file.
    type record struct {
        ID           string         `json:"id"`
        Kind         string         `json:"kind"`
        Title        string         `json:"title"`
        URL          string         `json:"url"`
        At           string         `json:"at"`
        ModifiedAt   string         `json:"modifiedAt"`
        Author       string         `json:"author"`
        Text         string         `json:"text"`
        Private      bool           `json:"private"`
        Channel      string         `json:"channel"`
        Thread       string         `json:"thread"`
        Metadata     map[string]any `json:"metadata"`
    }

The walk collects every `.jsonl` and `.ndjson` under the root, skipping
dot-names, sorted. The paging is the Mattermost paging: the cursor is the
file, or `<file>#<last entry sent>` mid-file, and a page stops at `most`
entries or `scanPageBytes` of text. `recordEntries(root, relative,
arguments)` reads one file: it parses each line (a scanner with the same
8 MiB line buffer as the chat reader), skips bad lines and counts them,
then splits records by kind. Document-kind records become entries directly:
`ExternalID: relative + "#" + id`, `Kind` as given (default `page`),
`Title` as given or the id, `URL`, `HappenedAt` and `ModifiedAt` parsed
from RFC 3339 (nil when absent or unparsable), `Text`, `Size`, `Hash`
sha256 of the text, `Private`, `Metadata` as given plus `author` when
given; a record whose text `SecretContent` flags is skipped. Chat-kind
records are gathered per `channel` in file order, converted to `chatPost`,
and handed to `chatUnits(relative, channel, posts, anyPrivate)`, whose
entries get an id of `relative + "#" + firstPostID` from the function
itself. The same cache the chat reader keeps for one file (`channelCache`)
serves here; generalize its name to `fileCache` if that reads better, or
leave it and use it. Known hashes mark entries `Unchanged` with their text
dropped, as everywhere.

Add `FormatRecords = "records"` beside the other constants in
`internal/computer/scan.go` and a `case FormatRecords:` in `RunScan`.

Tests, in `internal/computer/scan_records_test.go`, each over a temporary
folder written by the test:

- Two document records in one file produce two entries with the expected
  external ids, kinds, times, hashes and metadata; a second scan with
  `Known` set to those hashes returns them `Unchanged` with empty text.
- A file of chat records in two channels, one with a thread of three posts
  and four loose posts with a two hour gap in the middle, produces the
  units the Mattermost grouping would: one thread entry, two window
  entries per the gap, each with `participants`, `posts`, `channel`.
- A page of `Most: 1` walks three records across two files with the cursor
  ending each page, and the union of pages is every record once.
- A file with only unparsable lines produces one refused entry and
  `Refused: 1`.
- A dot-file and a `.txt` beside the records are ignored.

Run from the repository root:

    go test ./internal/computer/ -run 'Records|Mattermost|Chat' -count=1

and expect every test to pass; the Mattermost tests prove the shared
grouping is unchanged.

## Milestone 2: the daemon runs `refresh` first

At the end of this milestone, a records folder holding an executable
`refresh` has it run at the start of each scan, and a scan whose refresh
fails says so.

In `scan_records.go`, before the walk and only when `arguments.After == ""`
(the first page of a pass; later pages must not run it again):

    // refreshRecords runs the folder's refresh script, which is what fills
    // it: a command line tool asked for what changed, an export read
    // again. It runs as the person with the folder as its directory, and
    // its failure is the scan's failure, so the source's page says why.
    func refreshRecords(root string) error

It looks for `<root>/refresh`. Nothing there: return nil. Otherwise it must
be a regular file (`Lstat`, not a symlink), executable by the owner, and
owned by the current user; anything else returns an error naming the
check, so a script planted by something other than the person is refused
rather than run. It runs with `exec.CommandContext` under a
`refreshTimeout` of thirty minutes, directory `root`, the daemon's own
environment, stdout and stderr captured to `<root>/.refresh.log`
(truncated each run, so the person can read what happened), and the last
few lines of stderr folded into the error on a non-zero exit or timeout.
The scan then proceeds over whatever the folder holds, even after a
refresh failure? No: on failure `scanRecords` returns the error, so the
pass fails and `LastError` on the source shows it, and the next scheduled
pass tries again. A refresh that succeeds but writes nothing new is fine;
the known hashes make the pass cheap.

The daemon runs the scan under a deadline already (`ingestDeviceWait`, ten
minutes on the server side, `internal/agent/ingest.go` line 91); a refresh
of thirty minutes would outlive it. Raise the server's wait for the first
page of a records source: in `readFromComputer`, when the format is
`records` and `after` is empty, use `ingestRefreshWait = 35 * time.Minute`
for that `device.Ask`. Document both numbers next to each other.

Tests, in the same test file: a `refresh` that writes a record makes the
next scan find it; a `refresh` that exits 3 with a message makes the scan
return an error containing the message and the log file hold it; a
`refresh` that is a symlink is refused; a second page (`After` set) does
not run it (the script appends to a counter file; the test reads it).

## Milestone 3: the server, the command line, the tool and the dashboard

At the end of this milestone a person can add a `records` source every way
a source is added, the server pages it at chat size, and documents of kind
`message` and `page` keep their kind.

- `internal/models/knowledge.go`: add `FormatRecords = "records"` beside the
  other formats, and in `Validate()` check that `Specification.Format`,
  when set, is one of the four, so a typo fails at save time with
  `"x" is not a format: files, mattermost, journal or records`.
- `internal/agent/ingest.go` line 317: the 2048-entry page applies to
  `records` as well as `mattermost`; and the first page's wait from
  Milestone 2. In `documentKindOf` add `message → DocumentMessage`, `post →
  DocumentPost`, `file → DocumentFile` explicitly, and keep the default.
- `internal/cmd/agent_graph.go`: the `--format` usage string lists
  `records`; `teanode agent knowledge add` prints, for a records source, one
  more line after the allow reminder: `write records as JSON lines under
  that folder; a refresh script there runs before each scan (see
  docs/subsystems/memory.md)`.
- `internal/agent/tools/knowledge/knowledge.go`: `format` enum gains
  `models.FormatRecords`; the archive check at line 482 accepts it; and the
  tool's description gains the guidance the agent writes scripts from. Keep
  the description a paragraph; put the record shape in a second string the
  tool returns when asked `add` with `format: records` and no path, or
  better, add an action `shape` that returns the record shape and the
  refresh contract as text. Decide for `shape`: it is one more enum value
  and the model can ask for it when it needs it, rather than every prompt
  carrying the schema.
- `web/src/pages/agent.tsx` line 1645: the format list gains `records`;
  i18n keys `agent.knowledgeFormat.records` in the three catalogues, with a
  one-line hint under the field when `records` is chosen: "A folder of
  JSON-lines records, filled by a refresh script; ask the agent to write
  one." Check with `make check-catalogs`.
- `internal/api/v1api/apigraph/agent_graph.go`: nothing to change, the
  argument is a string; the validation above covers it.

Proof: `go test ./internal/models/ ./internal/agent/ -count=1` passes with
a new test that `Validate()` refuses `format: "recrods"`; `teanode agent
knowledge add notes ~/.teanode/records/notes --kind archive --format
records` succeeds against the dev server; the dashboard's add dialog
offers Records and the hint, verified in Chrome, desktop and phone width.

## Milestone 4: the agent writes the script; a subset, then everything

At the end of this milestone the maintainer's Confluence and Google Drive
are indexed through records the agent's own script produced.

First by hand, to prove the contract: write
`~/.teanode/records/confluence-dev/refresh` on the maintainer's machine
(`gen7`, where `teanode computer` runs) as a shell script that runs
`confluence search "space = DEV" --cql --limit 20`, then `confluence read
<id> --format markdown` for each hit, and writes `pages.jsonl` with one
`page` record each: `id` the page id, `title`, `url`, `at` from the page's
created date if the tool gives it else the modified date, `modifiedAt`,
`author`, `text` the markdown, `metadata` with `space` and `version`. Allow
the folder (`teanode computer allow ~/.teanode/records`), add the source
with `--format records`, `teanode agent knowledge sync` it, and watch
`teanode agent knowledge list` show twenty documents. Then in the
dashboard's knowledge page, open one; ask the agent a question the page
answers and see it cite the page.

Then the agent. In a conversation, ask: "index my Google Drive documents
modified this year as a knowledge source; write the refresh script
yourself under ~/.teanode/records/drive". The agent should read the shape
with the knowledge tool's `shape` action, open a shell, write a script that
runs `gog drive search "modifiedTime > '2026-01-01'" --json`, exports each
Google Doc as text (`gog drive download <id>` with the export format the
tool offers; find the flag with `gog drive download --help`, and record it
in this plan), writes `documents.jsonl`, and then adds the source with
`format: records`. Where the agent stumbles, the guidance in Milestone 3 is
what to fix, not the agent. Record the transcript's shape in Artifacts.

Subset first: both scripts take a `RECORDS_LIMIT` environment variable the
daemon does not set, so a person can run `RECORDS_LIMIT=20 ./refresh` by
hand; the nightly run reads everything. Once the subset is right, run the
full refresh by hand once (Drive may take an hour), sync, and let the
dream read it. Watch the monitors already armed on the dream (facts and
documents read per fifteen minutes) for the days it takes.

## Milestone 5: documentation

`docs/subsystems/memory.md` gains a section "Sources that are scripts"
after "Writing, without being asked", describing the records folder, the
record shape (the same text as the tool's `shape`), the refresh contract
and its checks, and that the agent can write the script. `docs/reference/command-line.md`
lists `records` under `agent knowledge add --format`. If a setting was
added (none is planned), `docs/configuration.md` documents it and
`make check-config-docs` proves it. The retrospective is written here.

## Concrete Steps

All commands run from the repository root, `/home/ziyan/projects/ziyan/teanode`.

    go build ./... && go vet ./internal/computer/ ./internal/agent/
    go test ./internal/computer/ -count=1
    make lint-ci
    cd web && npx tsc --noEmit -p . && npx prettier --check src

The dev server for the dashboard check: `make build`, then with
`dev/.env` in the environment, `./build/teanode-server run`; the dashboard
is `http://127.0.0.1:10081`. Deploy to the maintainer's server with
`make docker DOCKER_TAG=teanode:memory`, `docker save teanode:memory | ssh
root@server docker load`, `ssh root@server 'cd /opt/teanode && docker
compose up -d --force-recreate teanode'`. The daemon on `gen7` is the
binary it was started from: after a daemon change, `make build` then
`./build/teanode computer stop && ./build/teanode computer start`.

## Validation and Acceptance

After Milestone 1, `go test ./internal/computer/` passes with the five new
tests. After Milestone 2, a folder with a `refresh` that writes one record
is scanned to one entry, and a failing `refresh` puts its message in the
source's last error. After Milestone 3, all four ways of adding a source
accept `records` and a wrong format is refused at save. After Milestone 4,
`teanode agent knowledge list` shows the Confluence and Drive sources with
document counts, the knowledge page opens their documents, and a question
only they answer is answered with a citation. After every milestone that
touches the dashboard, the page is looked at in Chrome before it is
deployed or committed.

## Idempotence and Recovery

Every step is additive. Scanning the same folder twice files nothing new
because of the known hashes. A refresh script that fails leaves the folder
as it was and the source's last error saying why; fixing the script and
`teanode agent knowledge sync` recovers. Removing a source removes its
documents; the folder and the script stay on the person's machine.

## Artifacts and Notes

To be filled as milestones complete: the test transcript, the first
records file's head, the agent's transcript writing the Drive script.

## Interfaces and Dependencies

In `internal/computer/chat_units.go`: `type chatPost`, `func
chatUnits(relative, channel string, posts []chatPost, private bool)
[]ScanEntry`. In `internal/computer/scan_records.go`: `type record`, `func
scanRecords(root string, arguments *ScanArguments, most int) (*ScanResult,
error)`, `func refreshRecords(root string) error`, `const refreshTimeout`.
In `internal/computer/scan.go`: `FormatRecords`. In
`internal/models/knowledge.go`: `FormatRecords`, format validation. In
`internal/agent/ingest.go`: the page size and wait for records. In
`internal/agent/tools/knowledge/knowledge.go`: the `shape` action and the
`records` format. No new libraries; `encoding/json`, `bufio`, `os/exec`
from the standard library.
