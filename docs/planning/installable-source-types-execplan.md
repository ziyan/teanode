# Sources are installed as types, not written as scripts

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A knowledge source is somewhere the agent reads on the person's behalf: a folder of code, a chat server, a wiki, a tracker, a mailbox. Today there are two ways to make one. A folder is read by the program attached to the person's computer (`teanode computer`) with rules built into it. Anything else needs a "records folder": a directory on the person's computer holding an executable `records` script, usually Python, that calls some command line tool and prints JSON lines. Every such script reimplements the same machinery (listing, paging, retrying, caching, the rule that a listing must be complete or fail) slightly differently, lives outside any version control, and has to be copied by hand to the computer that runs it.

After this change a person adds a source the way they add a skill. An operator installs a *source type*, a short YAML file that says which command line tool to call, how to list what it holds, how to page through it, and how each item becomes a record. The person then adds a source of that type from the Knowledge page or the command line, fills in its settings (which account, which query, which spaces), and chooses which attached computer runs it. The program on that computer runs the type itself. No script is written or copied anywhere.

Seeing it work: install the `gmail-gog` type, then run

    teanode agent knowledge add --type gmail-gog --computer laptop --setting query='newer_than:30d' inbox

and within a minute the source page shows messages being read, with no file created on the computer except a cache under `~/.cache/teanode/sources/`. The same install, pointed at another computer, is how a work tracker is read from a work laptop while a personal chat is read from a personal one.

## Progress

- [x] (2026-09-23) Surveyed the skills system, the source model, the scan and records path, and the five records scripts in use; wrote this plan.
- [x] (2026-09-23) Created `github.com/teanode/teanode-sources` with its own signing key and draft types for GitHub, GitLab, Mattermost, Confluence, Google Drive and Gmail, their field names and paging read from each tool; not yet runnable.
- [x] (2026-09-23) Added `folder`, `journal` and `website` as types naming a built-in reader, and `rss` as the first type that makes a web request and can run on the server.
- [x] (2026-09-23) Milestone 1: the runner (`internal/sources`) and `teanode computer try-source`, run against `gh`, `gog` (Gmail and Drive), `mm` and `confluence` on a real computer.
- [x] (2026-09-23) Milestone 2: the format as a package, parsed and validated, with requests, secrets, pace and retries; files a tool keeps on disk, lookups, a refresh command, walks named by path, and the filters the scripts' text needed.
- [x] (2026-09-23) Milestone 3: `agent_source_type`, install from the signed registry or add a local type, sources of a type (`type` and `settings` on the source), the typed scan, and `teanode agent source-type`. The registry publishes nine types, signed.
- [x] (2026-09-23) Milestone 4: the types that replace the five scripts, each checked record by record against its script (see Artifacts), and every live source switched: folders to `folder`, the chat archive to `mattermost-mm`, the drive to `google-drive-gog`, the code host to `github-gh`, the two exports to local types. The self-hosted code host later moved to `gitlab-glab` on another computer, at the person's request.
- [x] (2026-09-23) Milestone 5: the dashboard: Source types under Agents (installed, the registry, local types, each type's guide), a Sources tab on the agent page with a type picker and a settings form, and a typed source's settings when it is edited. Checked at desktop and phone widths.
- [x] (2026-09-23) The agent's knowledge tool lists types and adds a source of one; `teanode agent source-type` and `knowledge add/set --type --setting --computer`.
- [x] (2026-09-23) Code review of the whole change; its findings fixed (see Outcomes), shipped as #127 and #128.
- [x] (2026-09-23) Paging by offset, and a tool that answers the same page when asked for the next fails the reading; a source put off because its computer is busy with another source tries again in minutes, not the next night. Shipped as #128.
- [x] (2026-09-23) Secrets carried to where a type runs: a type declares them as a skill does, the person fills them in for each source (dashboard, `teanode agent knowledge secret`, `try-source --secret`), they are kept sealed with the server secret and sent only in the scan request. `link` paging follows the next page an answer names, and neither it nor a redirect leaves the scheme and host it started on. Shipped as #129.
- [x] (2026-09-23) `confluence-api` in the registry: a wiki read with its web API and a token, one container per space, a page's body fetched once for each version. The wiki source moved to it from its export and reads the whole site.
- [x] (2026-09-23) Milestone 6, first half: the scripts retired, at the person's request on the day rather than after a week of quiet passes. The carried caches (video sheets, exported workbooks) were checked against the new ones before the scripts and their records folders were removed; the records contract stays for anything a type cannot say.
- [ ] Milestone 6, second half: `docs/subsystems/memory.md` and the knowledge tool's `shape` text describe types first, and the `skill` and `web` kinds that were never built are removed.

## Surprises & Discoveries

- Observation: every records script in use does the same five things, and three of five do not use the shared helper library written for them.
  Evidence: the survey found listing with a cache, paging by token, retry on unparsable output, a per-item fetch with a versioned cache, and the complete-or-fail rule in each, written separately.
- Observation: a listing that silently comes up short deletes documents. The pass ends by removing every document it did not see, so a script that skips a scope on error, or reads only the first page, sweeps away what that scope had filed.
  Evidence: `internal/agent/ingest_pass.go`, `DeleteAgentDocumentsUnseen`; a script reviewed on 2026-09-22 did both.
- Observation: some tools page only by a result limit, with no page token, and some answer in text rather than JSON.
  Evidence: the wiki command line tool used for Confluence searches with `--limit` only and prints `N. Title (ID: 123)` lines.
- Observation: three of the five scripts did not call a service at all; they read an export or an archive a tool had written to disk (the chat archive `mm archive` keeps, and one-off exports of a wiki and a code host). A type had to be able to read files, not only run commands, or those sources could not move without every document being read again.
  Evidence: the chat, code host and wiki scripts in the survey each walked a directory of files.
- Observation: `mm archive sync --files all` fetches every attachment the archive lacks, not only those of new posts: 342,000 on the archive here, against 54,000 kept. `--files mine` is 36.
  Evidence: a sync run on 2026-09-23, stopped before it fetched anything.
- Observation: a document's hash is the hash of its text, so a type must reproduce a script's text exactly, down to the dash in a generated heading, a date cut to ten characters and Windows line endings turned into newlines, or everything is read again.
  Evidence: the comparisons in Artifacts found each of these before the switch.
- Observation: a wiki's command line tool cannot list a large site completely. It lists at most 500 spaces, a search answers at most 250 results whatever limit is asked for, and in its newer version the flag that says where to start is ignored for query searches, so every page is the first. A bulk edit changed thousands of pages in one hour, so no window of time is small enough either.
  Evidence: a switch of the wiki source to the live type on 2026-09-23, paused before its pass finished; nothing was deleted and the source went back to its export.
- Observation: a source that found its computer busy reading another source was put off to its hour the next day, not retried when the other finished, so behind one long source the rest never ran.
  Evidence: `internal/agent/ingest.go`, the waiting branch; four sources were found scheduled for the next night after the switch.
- Observation: the review found several ways a pass could still delete what it had not really lost: a container that failed to read was sent as one refused entry and its documents swept; a cursor naming an item gone since the last page skipped the rest of its container; a comparison with an empty time held, skipping readings; an error object with no list read as an empty listing.
  Evidence: the review of #127; each is now an unfinished pass, a failure, or a test.
- Observation: the wiki's web API could not be read as one container either. The first try listed every page of the site under one container, and the computer held the whole listing and its kept records in memory at once.
  Evidence: the switch to `confluence-api` 1.0.0; 1.1.0 lists spaces as containers and reads each space's pages on its own.
- Observation: a secret-backed source could not be given its secret in the same save that made it: the secrets API read the source in a transaction of its own, which did not see the source the save had just written.
  Evidence: the review of #129; the secrets API now reads within the request's transaction.
- Observation: the check for secrets in tracked files treats anything shaped like a host name as a leak, including a setting named `site` in a template (`settings.site`).
  Evidence: CI on #129; the setting became `domain`.
- Observation: `skill` and `web` source kinds are declared in `internal/models/knowledge.go` and accepted by the API but cannot be read (`internal/agent/ingest.go` fails them as "not built yet").

## Decision Log

- Decision: a source type is a YAML file like a skill, distributed through a registry of its own, `github.com/teanode/teanode-sources`, laid out as `github.com/teanode/teanode-skills` is: an `index.json` listing each type with its version, URL, SHA-256 and signature, and `sources/<name>/source.md` for each type. It is signed with a key of its own, `keys/teanode-sources-ed25519-public.pem`, which TeaNode carries built in beside the skills key.
  Rationale: types and skills are installed, reviewed and released on their own schedules, and a person browsing either should see only one kind of thing. A key of its own means a key that signs source types cannot be used to publish a skill, and the other way round. The signing decision record, `docs/decisions/20260911-skills-are-signed-and-run-where-they-can-do-least.md`, gains a note that there are now two registries, each with its own key.
  Date/Author: 2026-09-23, the person.

- Decision: a type is named for the service and the tool it calls, `<service>-<tool>` (`github-gh`, `gitlab-glab`, `mattermost-mm`, `google-drive-gog`, `gmail-gog`), or for the service alone when the tool is named after it (`confluence`).
  Rationale: one service can be read by more than one tool, and the name says which, so a second way of reading a service can be published beside the first without renaming it.
  Date/Author: 2026-09-23, the person.

- Decision: the runner lives in `teanode computer`, as a new scan format, and produces the same records the records contract produces today.
  Rationale: everything after the records (filing, attachments by hash, the sweep, embeddings) already works and is tested. Only the part that scripts do by hand moves into the program. The server sends the type's YAML and the source's settings with the scan request, so the computer needs no installation step and always runs what the server holds.
  Date/Author: 2026-09-23, agent.

- Decision: reading a folder, a journal and a website stays code built into TeaNode, but each is a type in the registry all the same: `folder`, `journal` and `website` say `reader: files`, `reader: journal` or `reader: web` and declare the settings that reader takes, with no commands.
  Rationale: git awareness, authorship ("is this checkout the person's work"), file readers, commit history, following links and turning HTML into text are code, tuned over many passes, and not a matter of calling a tool. Declaring them as types means every source a person has is of a registry type, the add form is built from each type's settings the same way, and a reader's settings are described in one place. This replaces the earlier decision (same day) that folders stay outside the registry.
  Date/Author: 2026-09-23, the person (folders in the registry), agent (as readers named by a type).

- Decision: a type can make a web request wherever it can run a command, with `secrets` and `authenticationProfiles` as a skill has them, and says where it `runs`: on an attached computer, on the server, or either, chosen per source; left out, a computer.
  Rationale: some services have a web API and no tool worth installing (a feed, a self-hosted service's REST API), and wrapping `curl` in a command would put a credential in a command line. A type that only makes requests needs no computer at all, and one whose service answers only inside a network sends its requests through a computer in it with the daemon's existing `http` action (`internal/computer/http.go`), as skills already do. A credential is sent only to a host written into the type or taken from a secret, the rule `internal/skills/skill.go` keeps (`settledHost`), except that a person's own secret may go to an address that person gave in the source's settings; an operator's never does.
  Date/Author: 2026-09-23, agent, at the person's request.

- Decision: completeness is the runner's job, not the type author's. A listing that fails, answers something unparsable, or may have been cut by a limit fails the pass; the runner never prints a partial listing.
  Rationale: the sweep deletes whatever a listing leaves out. Each script had to remember this and one did not. Moving the rule into the runner makes it impossible to forget.
  Date/Author: 2026-09-23, agent.

- Decision: operators may also add a local type from a file, unsigned, marked "local".
  Rationale: the person running their own server is the operator, and writing a type for a tool nobody has published (a company's own command line tool) is the common case. Adding one needs `server:manage`, the same permission installing a signed type needs, and the type is shown as local wherever it appears.
  Date/Author: 2026-09-23, agent.

- Decision: a migrated source keeps its document identifiers.
  Rationale: identifiers are the container name plus the record id. A chat source here holds hundreds of thousands of documents with their embeddings; switching it to a type that names things differently would delete and re-read all of them. Each type used for a migration is checked with a dry run that reports how many identifiers match the existing source before the switch.
  Date/Author: 2026-09-23, agent.

- Decision: a type can read files on the computer it runs on (`files` in a listing, `file` or `files` in a reading, `lookups` read from files, a `refresh` command run first), and the Mattermost type reads the copy `mm archive` keeps rather than calling the server post by post.
  Rationale: the archive already names direct and group channels, keeps exclusions and downloads attachments; reading it keeps every document identifier the chat source had, and `mm archive sync` does the incremental work. Reading files is refused for a type that runs on the server.
  Date/Author: 2026-09-23, agent.

- Decision: the one-off exports of a wiki and a code host are read by local types (`gitlab-export`, `confluence-export`) that stay off the public registry.
  Rationale: their layout is that of a script nobody publishes, and the live types read the same services with different text, which would read 250,000 documents again. A local type replaces the script without that.
  Date/Author: 2026-09-23, agent.

- Decision: making a sheet of frames for a video moves into the computer program, for every typed source, cached by the hash of the video.
  Rationale: the chat script did it for itself; a type cannot run ffmpeg. The script's sheets are carried over to the new cache so no sheet document changes.
  Date/Author: 2026-09-23, agent.

- Decision: a reading that runs out of time returns what it read and marks the pass unfinished, and an unfinished pass never deletes.
  Rationale: a first read of a large mailbox or chat takes many pages; a pass cut short is not a pass that saw everything.
  Date/Author: 2026-09-23, agent.

- Decision: the dry run the plan called `knowledge try` was not built; each type was compared with its script by running both over the same containers with `teanode computer try-source` and diffing the records field by field.
  Rationale: the comparison had to cover text and attachments, not only identifiers, and the scripts were on the same computer. A dry run on the server remains worth building for sources whose script is gone.
  Date/Author: 2026-09-23, agent.

- Decision: a computer forgets what a source's directory learned (its listing, its marks, its kept records) when the source's type or settings change, keeping fetched files, whose names already say what they are.
  Rationale: a mailbox pointed at another account must not keep reporting the first account's mail, and a reading that starts from where the last stopped must start again.
  Date/Author: 2026-09-23, agent, from the review.

- Decision: the wiki source stays on its export; the registry's `confluence` type fails when an answer may have been cut instead of passing for complete, and the runner fails a tool that answers the same page when asked for the next.
  Rationale: see Surprises. Reading that site live needs its web API with cursor paging, which needs secrets carried to the computer.
  Date/Author: 2026-09-23, agent. Superseded the same day by the next decision.

- Decision: a type's secrets are the person's and belong to one source: every secret a type declares is `scope: person`, kept for each source (two sources of one type are usually two accounts), and used only inside an authentication profile, never in a URL or a command line. The wiki source moved to `confluence-api` once they shipped.
  Rationale: a URL ends up in logs and error messages, and a command line in the process list; a profile puts the secret in a header the runner builds. Keeping them for each source rather than each type is what lets a person read two accounts of one service.
  Date/Author: 2026-09-23, the person (secrets as skills have them), agent (their scope and where they may appear).

- Decision: the scripts and their records folders were removed on the day the last source moved, not after a week of passes.
  Rationale: the person asked for it, and the carried caches had been checked against the new ones, so nothing a later pass needs came from the folders.
  Date/Author: 2026-09-23, the person.

## Outcomes & Retrospective

Shipped on 2026-09-23 in #127, #128 and #129. Every knowledge source on the deployed server is now a source of a type; nine types are published in the signed registry, and two local types read one-off exports. The switch kept identifiers and hashes wherever the type replaced a script: the drive's first typed pass saw all 1,958 documents and filed three new ones; the chat, export and folder sources re-saw what their first typed passes reached without re-reading any of it. The one source moved to a different reading (the self-hosted code host, from its export to `glab`) was re-read on purpose.

What worked: comparing each type record by record before switching found every difference that would have changed a hash (a dash, a date cut to ten characters, line endings, a label's case), and carrying the scripts' caches over (video sheets, exported workbooks) kept the files' identities too.

What was learned: the sweep is the risk in everything here. Every defect of consequence the review found was a path by which a pass that had not seen everything could still delete, and the fix each time was the same rule: a pass that is not sure it saw everything is unfinished, and an unfinished pass deletes nothing. A tool's limits are the other risk: a tool that caps or ignores paging must make the reading fail, never pass for complete.

Secrets for typed sources followed in #129, and with them the wiki moved from a one-off export to its live web API; the scripts were retired the same day.

Left to do: the rest of Milestone 6 (the memory document and the knowledge tool's text, and removing the unbuilt kinds), the `website` reader, and a server-side dry run.

## Context and Orientation

The server is the Go program in this repository (`cmd/teanode-server`). The person's computer runs `teanode computer`, a background program (the "daemon") built from `internal/computer/`, which holds a websocket to the server and answers requests: run a command, read a file, and scan. A *scan* is a request to read a source page by page: `internal/computer/scan.go` (`RunScan`) takes `ScanArguments` (root folder, format, a map of hashes the server already holds, a cursor) and answers a page of entries.

A *source* is a row in the `agent_source` table (`internal/models/knowledge.go`, `AgentKnowledgeSource`): a kind (`computer`, `archive`, `sent`), a specification (the computer's name, a path, a format of `files`, `journal` or `records`, and a few knobs), a schedule, and a place in the memory graph (`RootPath`) where what it finds is filed. The server's side of a pass is `internal/agent/ingest.go` and `ingest_computer.go`: it sends scan requests to the source's computer, files each page (`ingest_page.go`), fetches attachment bytes with the daemon's `blob` action, and when the pass completes deletes documents the pass did not see (`ingest_pass.go`).

The *records* format (`internal/computer/scan_records.go`) reads a folder: every `.jsonl` file in it, plus, if the folder holds an executable `records` script, the names the script prints when run with no argument; `records <name>` prints that name's records as JSON lines. A record is a JSON object with `id` (required), `kind`, `title`, `url`, `at`, `modifiedAt`, `author`, `text`, `private`, `channel`, `thread`, `metadata`, and `attachments` (each `path`, `name`, `contentType`, `text`). A document's identifier is `<name>#<id>`; chat records are grouped by channel and thread. A `refresh` script, if present, runs first. These scripts are what this plan replaces.

A *skill* (`internal/skills/`, documented in `docs/subsystems/skills.md`) is the model for this plan: a markdown file with a YAML header declaring tools, each a list of steps that call an HTTP endpoint or run a command on the person's computer. Commands are lists of words, never one shell string; `{{name}}` fills a parameter in, and install refuses a reference to something undeclared. Skills are installed from a signed index (`internal/skills/registry.go`: an Ed25519 key built into the binary, a SHA-256 per file), stored whole in the `agent_skill` table, and managed by `teanode agent skill ...` and Settings > Skills.

Terms used below. A *source type* is the YAML file. A *source* is one configured instance of a type, with its settings and computer. A *container* is one unit a listing returns and a record belongs to: a chat channel, a repository, a wiki space, a folder, a mailbox label. A *listing* is the list of containers for one pass.

## The source type format

A type is a markdown file whose YAML header describes the type; the prose under it is for people. The header has:

`name` and `description`, as for a skill. `settings`: what a person fills in when adding a source of this type, each with a name, a description, a type (`string`, `path`, `array`, `boolean` or `integer`), an optional default, and an optional pattern the value must match (a setting is a word passed to a command, so a pattern such as `^[A-Za-z0-9_.@-]+$` keeps a value from becoming a flag); a setting with no default is required. `requires`: the command line tools the type calls, checked on the computer before the first pass so a missing tool is reported as that rather than as a failed listing. `refresh`: commands run once at the start of a pass, before anything is listed, for a tool that keeps its own copy of a service. `lookups`: tables read from files, reached as `lookup.<name>[key]`. `pace`: the least time between two calls.

`containers`: how to list the containers. One or more *listings*, each a `command` (a list of words, with `{{setting}}` references), a `parse` block, and a `paging` block, and a `name` template giving each container's name from the fields of what was listed. All listings run; their results are joined and deduplicated by name. If any listing fails, the pass fails.

`records`: how to read one container. A `command` with `{{container.field}}` references, `parse`, `paging`, an optional `since` (a flag and the time of the last complete pass over this container, for tools that can list only what changed), and a `record` block mapping fields of each item to the record's fields with templates.

`detail` (optional): a command run for each item whose `version` field changed since the last pass, whose output supplies the record's text; used where a listing gives identifiers and titles but not bodies. Its output is cached on the computer by item and version, so an unchanged item is never fetched twice.

`attachments` (optional): a command that writes one file for an item to a path the runner gives it, cached the same way, with a maximum size.

`parse` says what the command prints: `json` or `xml` with an `items` path to the list (dotted keys, as skills use), `jsonl`, `lines` with a regular expression whose named groups become the item's fields, `markdown` (a header of fields and a body), or `text`. `paging` is one of `none`, `token` (the field holding the next page's token and the flag that passes it), `offset` (the flag that says where to start, and the size of a full page), `all` (the tool pages by itself, such as `gh ... --paginate`), or `limit` (a page that comes back full is treated as possibly cut and fails the pass rather than being trusted). A listing may also be `files` under a directory, `fixed` items, or a `walk` down a tree; a reading may read a `file` or `files` instead of running a command. The registry's README is the full reference.

Templates name where a value comes from: `{{settings.account}}`, `{{container.name}}`, `{{item.id}}`, `{{each}}`, `{{pass.since}}`, `{{detail.text}}`, `{{lookup.users[item.user_id]}}`, `{{secret:key}}`, with filters such as `{{item.create_at | epoch-ms | local-time}}`. Conditions (`skip`, `when`, a record's `private`) compare them with `== != < <= > >= in matches` and `&& || !`.

An abridged type for a mailbox read with the `gog` Google command line tool, as it might be written:

    ---
    name: gmail-gog
    description: A Gmail mailbox, read with the gog command line tool.
    requires: [gog]
    settings:
      - name: account
        description: the Google account to read
        type: string
        pattern: "^[^-][^ ]*@[^ ]+$"
      - name: query
        description: which messages, in Gmail's search words
        type: string
        default: "newer_than:365d"
    pace: 250ms
    containers:
      - fixed: [{}]
        name: threads.jsonl
    records:
      - command: [gog, --account, "{{settings.account}}", --json, gmail, search, "{{settings.query}}", --max, "500", --timezone, UTC]
        parse: {json: {items: threads}}
        paging: {token: {field: nextPageToken, flag: --page}}
        unseen: keep
        record:
          id: "{{item.id}}"
          kind: mail
          title: "{{item.subject}}"
          at: "{{item.date | time}}"
          author: "{{item.from}}"
          version: "{{item.messageCount}}/{{item.date}}"
    ---

The published type is `sources/gmail-gog/source.md` in the registry, with the detail call that reads each thread in full; its field names and paging were read from the tool in Milestone 1.

## Plan of Work

Milestone 1 is a prototype that proves a declarative runner can read real tools before anything else is built. Add `internal/computer/scan_typed.go` with a function that takes a parsed type (a Go struct in the same package for now), the settings, and a cursor, and yields records exactly as `scan_records.go` yields them from a script. Add a hidden command, `teanode computer try-source <file.yaml> --setting k=v --limit N`, that runs it locally and prints records as JSON lines and, at the end, the number of containers and records and any refusal. Write two local YAML files outside the repository: one for `gh` (repositories, then issues and pull requests) and one for `gog` Gmail. Acceptance is that both print records whose text matches what the tool shows, and that a listing with a deliberately wrong flag fails with the tool's error instead of printing a partial list. This is where the field names, the paging of each tool, and the text of a Gmail message body (which may need decoding from the tool's JSON) are settled. If a tool cannot be described (its output cannot be parsed into items without code), record it in Surprises and decide whether the records contract stays its path.

Milestone 2 turns the prototype's struct into a package, `internal/sources`, beside `internal/skills`: `Parse(content []byte) (*Type, error)` reading the YAML header, and validation modelled on `internal/skills/skill.go` (`validate`, `checkReference`, `checkScripted`): commands are word lists, every `{{...}}` names a declared setting, a container field or an item field, no reference sits inside a `sh -c` script, `paging` and `parse` are one of the known shapes, and each setting has a type. Unit tests cover each refusal with a small invented type. The daemon imports the package so the server and the computer agree on what a type means.

Milestone 2 also covers the rest of the format: `reader` (a type naming a built-in reader is only its settings, checked against what that reader takes), `request` in place of `command` (validated as `internal/skills/skill.go` validates an http step, including `settledHost`), `secrets` and `authenticationProfiles` (the shapes `internal/skills` already parses, reused rather than copied), `runs`, and `xml` beside `json` in `parse`.

Milestone 3 wires one type end to end. Add a migration creating `agent_source_type` (name, version, publisher, url, sha256, local flag, content, timestamps), and add to the source's specification a `type` (the type's name) and `settings` (a map), with `format: typed`. `internal/agent/ingest_computer.go` sends the type's content and the source's settings in `ScanArguments` when the format is `typed`, and `RunScan` hands them to the runner from Milestone 1. Types come from their own registry, `https://raw.githubusercontent.com/teanode/teanode-sources/main/index.json`: `internal/skills/registry.go`'s index fetch, download and signature check are made to take the index address and the public key as parameters so both registries use them, the sources key built in from `internal/sources/keys/teanode-sources-ed25519-public.pem`, and install stores the file in `agent_source_type`. The repository exists (created 2026-09-23 with the tooling of `teanode-skills` and a key of its own) and holds the draft types this plan describes; each is added to its index, signed, once Milestone 4 has run it. API: `ListAgentSourceTypes`, `InstallAgentSourceType`, `AddLocalAgentSourceType` (needs `server:manage`), `RemoveAgentSourceType`, and `SaveAgentKnowledgeSource` accepting `type` and `settings`, validating settings against the type. CLI: `teanode agent source-type list|search|install|add-local|remove`, and `teanode agent knowledge add --type <name> --setting k=v`. Acceptance: install the Gmail type as a local type, add a source of it pointed at an attached computer, and see documents filed; pausing and removing it behave as for any source.

Milestone 4 writes the types that replace the scripts, one at a time, each checked before its source is switched. For each, run `teanode agent knowledge try <source> --type <name> --setting ...`, a dry run that asks the source's computer to run the type and reports how many of the identifiers it produces the existing source already holds, and how many it holds that the type did not produce. A switch goes ahead only when the two agree (a handful of differences explained, not thousands). The types, each named for the service and calling a public command line tool where one exists:

A code host's issues and pull requests with `gh` (repositories the account owns plus named organizations, each organization a listing so that one failing fails the pass). A self-hosted code host with `glab`, run on whichever computer can reach it, which is the reason the computer is chosen per source. A team chat server with its command line tool: channels as containers, posts since the last pass, threads as the record's thread, files as attachments. A wiki with its command line tool: spaces as containers, pages found by query with a limit that fails when full, each page's text from a detail command in markdown. A cloud drive with `gog`: folders as containers, walked by a listing that follows child folders, each file a record, native documents exported through the detail command and other files fetched as attachments. Gmail as in the example above.

Two things the scripts do that a type should not have to: reading a video attachment (describing it and sampling frames) and grouping chat records by thread. The first moves into the daemon's attachment readers in `internal/computer/scan_records.go`, available to every type; the second already happens there for records of kind `chat`.

The existing sources that read folders are switched to the `folder` and `journal` types in Milestone 4 as well: their specification becomes `type: folder` with the same settings, which changes nothing they read, since the reader and the document identifiers are the same. A source of a type that `runs` on the server is read by a new path in `internal/agent/ingest.go` that runs the type's requests itself, with no computer; secrets for a type are kept as a skill's are, in a table like `agent_skill_secret` (`agent_source_type_secret`), encrypted with the server's secret. The `website` type's reader, never built, is its own piece of work after this plan; until then the type installs but cannot be read, and says so.

Milestone 5 is the dashboard. Settings gets a "Source types" section beside Skills, listing installed and local types with install, update and remove. The Knowledge page's add dialog starts with the type (Files and code, Journal, and each installed type), then shows that type's settings as a form, the computer to run it on, and where in memory to file it. A source's card shows its type, its computer and its settings, and editing a setting starts a new pass.

Milestone 6 retires the scripts. With every source switched and a week of passes without a sweep larger than the day's changes, the records folders on the person's computer are left for the person to delete, and `docs/subsystems/memory.md` and the knowledge tool's `shape` text describe types first and the records contract as the path for anything a type cannot say. The `skill` and `web` kinds that were never built are removed, since a type covers what they were for.

## Concrete Steps

Trying a type on a computer, without the server:

    make build
    ./build/teanode computer try-source sources/gmail-gog/source.md --setting account=someone@example.com --containers 1 --records 5

It prints each record as a JSON line, with the container it came from, and the containers and counts on standard error; `--only <container>` reads chosen containers and `--records -1` prints every record, which is how each type was compared with its script.

Installing a type and adding a source of it:

    teanode agent source-type search
    teanode agent source-type install gmail-gog
    teanode agent source-type add-local ./my-type/source.md
    teanode agent knowledge add --type gmail-gog --computer laptop --setting account=someone@example.com inbox
    teanode agent knowledge set inbox --setting query=newer_than:30d

## Validation and Acceptance

Each milestone's acceptance is stated with it. Across the whole plan: `make test` passes, with new tests for the parser's refusals, for the runner (a fake tool written as a small Go test binary that prints pages, including one that fails on the second page, which must fail the pass and print nothing), and for the dry run's identifier comparison. The end state is observed on a deployed server: every source on the Knowledge page shows a type; no records folder is needed; a pass over each migrated source files the same documents it did before the switch (the dry run's counts, and document totals within the day's normal change).

## Idempotence and Recovery

Installing a type twice is an update. Switching a source to a type is a settings change on the source and can be reverted by setting its format back to `records` and its path to the old folder, which is kept until Milestone 6. A dry run changes nothing on the server. The migration adding `agent_source_type` has a reverse migration that drops the table; sources of a type fail to read until reverted, and nothing they filed is deleted by the failure, since a failed pass never sweeps.

## Artifacts and Notes

Record-by-record comparisons of each type against the script it replaces, over the same containers on the same computer (2026-09-23): chat, 244,252 records in 300 channels, every field identical, attachments a superset (23 posts gain a file the script's index missed); GitLab export, 48,777 records identical; Confluence export, all 130,380 identifiers and texts identical (titles with quotes now read correctly); Google Drive, all 94 folders and 270 sampled records identical, re-exported sheets and decks carried over from the script's cache; GitHub, all 256 repositories and every sampled record identical.

The survey behind this plan found these limits and behaviors worth keeping in the runner: listings cached for a pass so later pages do not list again; retries of a command whose output does not parse, three times with a growing pause; a per-pass budget of time and of fetches, as the shared script library had; a cache of fetched details and files keyed by item and version with a size limit; and records defaulting to private, so that nothing read from a service is shown as public unless the type says so.

## Interfaces and Dependencies

In `internal/sources/source.go`:

    type Type struct {
        Name, Description string
        Reader            string   // "files", "journal", "web", or empty for a type of calls
        Runs              []string // "computer", "server"
        Requires          []string
        Settings          []Setting
        Secrets           []skills.Secret
        Profiles          map[string]skills.Profile
        Containers        []Listing
        Records           []Reading
    }

    func Parse(content []byte) (*Type, error)
    func (self *Type) CheckSettings(values map[string]any) (map[string]any, error)
    func (self *Type) Specify(source *models.AgentKnowledgeSource, values map[string]any) error

The runner is `sources.Runner` (`List`, `Read`) in `internal/sources/runner.go`, with an `Executor` that runs commands and requests. In `internal/computer/scan_typed.go`, `openTyped` builds one for a scan page, and `scanRecords` reads a typed source as it reads a records folder. `ScanArguments` gains `SourceType`, `Settings`, `Secrets` and `SourceKey`, sent only when `Format` is `typed`; `ScanResult` gains `IsUnfinished`. No new third-party libraries: YAML parsing uses the library `internal/skills` already uses.

Revision note (2026-09-23, after #129): secrets for typed sources, the `confluence-api` type and the wiki's move to it, and the scripts' retirement recorded in Progress, Surprises, the Decision Log and Outcomes; Milestone 6 split into what was done and what is left.

Revision note (2026-09-23, after shipping): progress, discoveries, decisions and outcomes brought up to what shipped in #127 and #128; the format section, the example, the steps and the interfaces describe the code as it is.

Revision note (2026-09-23, later): folders, journals and websites are types in the registry too, each naming a reader built into TeaNode; a type can make web requests with secrets, and can run on the server.

Revision note (2026-09-23): source types have a registry of their own, `github.com/teanode/teanode-sources`, instead of a `sources/` directory in the skills registry, and a signing key of their own, both at the person's request. The repository now exists with draft types.
