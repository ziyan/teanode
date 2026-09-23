# Sources are installed as types, not written as scripts

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A knowledge source is somewhere the agent reads on the person's behalf: a folder of code, a chat server, a wiki, a tracker, a mailbox. Today there are two ways to make one. A folder is read by the program attached to the person's computer (`teanode computer`) with rules built into it. Anything else needs a "records folder": a directory on the person's computer holding an executable `records` script, usually Python, that calls some command line tool and prints JSON lines. Every such script reimplements the same machinery (listing, paging, retrying, caching, the rule that a listing must be complete or fail) slightly differently, lives outside any version control, and has to be copied by hand to the computer that runs it.

After this change a person adds a source the way they add a skill. An operator installs a *source type*, a short YAML file that says which command line tool to call, how to list what it holds, how to page through it, and how each item becomes a record. The person then adds a source of that type from the Knowledge page or the command line, fills in its settings (which account, which query, which spaces), and chooses which attached computer runs it. The program on that computer runs the type itself. No script is written or copied anywhere.

Seeing it work: install the Gmail type, then run

    teanode agent knowledge add --type gmail --computer laptop --setting query='newer_than:30d' inbox

and within a minute the source page shows messages being read, with no file created on the computer except a cache under `~/.cache/teanode/sources/`. The same install, pointed at another computer, is how a work tracker is read from a work laptop while a personal chat is read from a personal one.

## Progress

- [x] (2026-09-23) Surveyed the skills system, the source model, the scan and records path, and the five records scripts in use; wrote this plan.
- [x] (2026-09-23) Created `github.com/teanode/teanode-sources` with its own signing key and draft types for GitHub, GitLab, Mattermost, Confluence, Google Drive and Gmail, their field names and paging read from each tool; not yet runnable.
- [ ] Milestone 1: prototype runner on the computer, driven by a local YAML file, no server change.
- [ ] Milestone 2: the source type format as a package, parsed and validated like a skill.
- [ ] Milestone 3: installing types and adding sources of a type, end to end, for one type.
- [ ] Milestone 4: the types that replace the five scripts, each checked against the source it replaces before switching.
- [ ] Milestone 5: the dashboard: types under Settings, a type picker and settings form on the Knowledge page.
- [ ] Milestone 6: the scripts retired; the records contract kept for anything a type cannot say.

## Surprises & Discoveries

- Observation: every records script in use does the same five things, and three of five do not use the shared helper library written for them.
  Evidence: the survey found listing with a cache, paging by token, retry on unparsable output, a per-item fetch with a versioned cache, and the complete-or-fail rule in each, written separately.
- Observation: a listing that silently comes up short deletes documents. The pass ends by removing every document it did not see, so a script that skips a scope on error, or reads only the first page, sweeps away what that scope had filed.
  Evidence: `internal/agent/ingest_pass.go`, `DeleteAgentDocumentsUnseen`; a script reviewed on 2026-09-22 did both.
- Observation: some tools page only by a result limit, with no page token, and some answer in text rather than JSON.
  Evidence: the wiki command line tool used for Confluence searches with `--limit` only and prints `N. Title (ID: 123)` lines.
- Observation: `skill` and `web` source kinds are declared in `internal/models/knowledge.go` and accepted by the API but cannot be read (`internal/agent/ingest.go` fails them as "not built yet").

## Decision Log

- Decision: a source type is a YAML file like a skill, distributed through a registry of its own, `github.com/teanode/teanode-sources`, laid out as `github.com/teanode/teanode-skills` is: an `index.json` listing each type with its version, URL, SHA-256 and signature, and `sources/<name>/source.md` for each type. It is signed with a key of its own, `keys/teanode-sources-ed25519-public.pem`, which TeaNode carries built in beside the skills key.
  Rationale: types and skills are installed, reviewed and released on their own schedules, and a person browsing either should see only one kind of thing. A key of its own means a key that signs source types cannot be used to publish a skill, and the other way round. The signing decision record, `docs/decisions/20260911-skills-are-signed-and-run-where-they-can-do-least.md`, gains a note that there are now two registries, each with its own key.
  Date/Author: 2026-09-23, the person.

- Decision: the runner lives in `teanode computer`, as a new scan format, and produces the same records the records contract produces today.
  Rationale: everything after the records (filing, attachments by hash, the sweep, embeddings) already works and is tested. Only the part that scripts do by hand moves into the program. The server sends the type's YAML and the source's settings with the scan request, so the computer needs no installation step and always runs what the server holds.
  Date/Author: 2026-09-23, agent.

- Decision: folders stay a built-in type ("Files and code"), not a YAML type.
  Rationale: git awareness, authorship ("is this checkout the person's work"), file readers and commit history are code, tuned over many passes, and are not a matter of calling a tool. The UI shows it as one type among the others so the person sees one list.
  Date/Author: 2026-09-23, agent.

- Decision: completeness is the runner's job, not the type author's. A listing that fails, answers something unparsable, or may have been cut by a limit fails the pass; the runner never prints a partial listing.
  Rationale: the sweep deletes whatever a listing leaves out. Each script had to remember this and one did not. Moving the rule into the runner makes it impossible to forget.
  Date/Author: 2026-09-23, agent.

- Decision: operators may also add a local type from a file, unsigned, marked "local".
  Rationale: the person running their own server is the operator, and writing a type for a tool nobody has published (a company's own command line tool) is the common case. Adding one needs `server:manage`, the same permission installing a signed type needs, and the type is shown as local wherever it appears.
  Date/Author: 2026-09-23, agent.

- Decision: a migrated source keeps its document identifiers.
  Rationale: identifiers are the container name plus the record id. A chat source here holds hundreds of thousands of documents with their embeddings; switching it to a type that names things differently would delete and re-read all of them. Each type used for a migration is checked with a dry run that reports how many identifiers match the existing source before the switch.
  Date/Author: 2026-09-23, agent.

## Outcomes & Retrospective

Nothing implemented yet.

## Context and Orientation

The server is the Go program in this repository (`cmd/teanode-server`). The person's computer runs `teanode computer`, a background program (the "daemon") built from `internal/computer/`, which holds a websocket to the server and answers requests: run a command, read a file, and scan. A *scan* is a request to read a source page by page: `internal/computer/scan.go` (`RunScan`) takes `ScanArguments` (root folder, format, a map of hashes the server already holds, a cursor) and answers a page of entries.

A *source* is a row in the `agent_source` table (`internal/models/knowledge.go`, `AgentKnowledgeSource`): a kind (`computer`, `archive`, `sent`), a specification (the computer's name, a path, a format of `files`, `journal` or `records`, and a few knobs), a schedule, and a place in the memory graph (`RootPath`) where what it finds is filed. The server's side of a pass is `internal/agent/ingest.go` and `ingest_computer.go`: it sends scan requests to the source's computer, files each page (`ingest_page.go`), fetches attachment bytes with the daemon's `blob` action, and when the pass completes deletes documents the pass did not see (`ingest_pass.go`).

The *records* format (`internal/computer/scan_records.go`) reads a folder: every `.jsonl` file in it, plus, if the folder holds an executable `records` script, the names the script prints when run with no argument; `records <name>` prints that name's records as JSON lines. A record is a JSON object with `id` (required), `kind`, `title`, `url`, `at`, `modifiedAt`, `author`, `text`, `private`, `channel`, `thread`, `metadata`, and `attachments` (each `path`, `name`, `contentType`, `text`). A document's identifier is `<name>#<id>`; chat records are grouped by channel and thread. A `refresh` script, if present, runs first. These scripts are what this plan replaces.

A *skill* (`internal/skills/`, documented in `docs/subsystems/skills.md`) is the model for this plan: a markdown file with a YAML header declaring tools, each a list of steps that call an HTTP endpoint or run a command on the person's computer. Commands are lists of words, never one shell string; `{{name}}` fills a parameter in, and install refuses a reference to something undeclared. Skills are installed from a signed index (`internal/skills/registry.go`: an Ed25519 key built into the binary, a SHA-256 per file), stored whole in the `agent_skill` table, and managed by `teanode agent skill ...` and Settings > Skills.

Terms used below. A *source type* is the YAML file. A *source* is one configured instance of a type, with its settings and computer. A *container* is one unit a listing returns and a record belongs to: a chat channel, a repository, a wiki space, a folder, a mailbox label. A *listing* is the list of containers for one pass.

## The source type format

A type is a markdown file whose YAML header describes the type; the prose under it is for people. The header has:

`name` and `description`, as for a skill. `settings`: what a person fills in when adding a source of this type, each with a name, a description, a JSON Schema type, an optional default, and an optional pattern the value must match (a setting is a word passed to a command, so a pattern such as `^[A-Za-z0-9_.@-]+$` keeps a value from becoming a flag). `requires`: the command line tools the type calls, checked on the computer before the first pass so a missing tool is reported as that rather than as a failed listing.

`containers`: how to list the containers. One or more *listings*, each a `command` (a list of words, with `{{setting}}` references), a `parse` block, and a `paging` block, and a `name` template giving each container's name from the fields of what was listed. All listings run; their results are joined and deduplicated by name. If any listing fails, the pass fails.

`records`: how to read one container. A `command` with `{{container.field}}` references, `parse`, `paging`, an optional `since` (a flag and the time of the last complete pass over this container, for tools that can list only what changed), and a `record` block mapping fields of each item to the record's fields with templates.

`detail` (optional): a command run for each item whose `version` field changed since the last pass, whose output supplies the record's text; used where a listing gives identifiers and titles but not bodies. Its output is cached on the computer by item and version, so an unchanged item is never fetched twice.

`attachments` (optional): a command that writes one file for an item to a path the runner gives it, cached the same way, with a maximum size.

`parse` says what the command prints: `json` with an `items` path to the list (dotted keys, as skills use), `jsonl`, or `lines` with a regular expression whose named groups become the item's fields. `paging` is one of `none`, `token` (the field holding the next page's token and the flag that passes it), `all` (the tool pages by itself, such as `gh ... --paginate`), or `limit` (a flag and a number; a page that comes back full is treated as possibly cut and fails the pass rather than being trusted).

An abridged type for a mailbox read with the `gog` Google command line tool, as it might be written:

    ---
    name: gmail
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
    containers:
      - command: [gog, --account, "{{account}}", --json, gmail, labels, list]
        parse: {json: {items: labels}}
        paging: none
        name: "labels/{{name}}"
    records:
      command: [gog, --account, "{{account}}", --json, gmail, search, "label:{{container.name}} {{query}}", --max, "100"]
      parse: {json: {items: threads}}
      paging: {token: {field: nextPageToken, flag: --page}}
      record:
        id: "{{id}}"
        kind: mail
        title: "{{subject}}"
        at: "{{date}}"
        author: "{{from}}"
        version: "{{historyId}}"
    detail:
      command: [gog, --account, "{{account}}", --json, gmail, get, "{{id}}", --format, full]
      text: "{{body.text}}"
    ---

Whether `gog` answers with these exact field names is what Milestone 1 finds out; the format is settled by it, not by this example.

## Plan of Work

Milestone 1 is a prototype that proves a declarative runner can read real tools before anything else is built. Add `internal/computer/scan_typed.go` with a function that takes a parsed type (a Go struct in the same package for now), the settings, and a cursor, and yields records exactly as `scan_records.go` yields them from a script. Add a hidden command, `teanode computer try-source <file.yaml> --setting k=v --limit N`, that runs it locally and prints records as JSON lines and, at the end, the number of containers and records and any refusal. Write two local YAML files outside the repository: one for `gh` (repositories, then issues and pull requests) and one for `gog` Gmail. Acceptance is that both print records whose text matches what the tool shows, and that a listing with a deliberately wrong flag fails with the tool's error instead of printing a partial list. This is where the field names, the paging of each tool, and the text of a Gmail message body (which may need decoding from the tool's JSON) are settled. If a tool cannot be described (its output cannot be parsed into items without code), record it in Surprises and decide whether the records contract stays its path.

Milestone 2 turns the prototype's struct into a package, `internal/sources`, beside `internal/skills`: `Parse(content []byte) (*Type, error)` reading the YAML header, and validation modelled on `internal/skills/skill.go` (`validate`, `checkReference`, `checkScripted`): commands are word lists, every `{{...}}` names a declared setting, a container field or an item field, no reference sits inside a `sh -c` script, `paging` and `parse` are one of the known shapes, and each setting has a type. Unit tests cover each refusal with a small invented type. The daemon imports the package so the server and the computer agree on what a type means.

Milestone 3 wires one type end to end. Add a migration creating `agent_source_type` (name, version, publisher, url, sha256, local flag, content, timestamps), and add to the source's specification a `type` (the type's name) and `settings` (a map), with `format: typed`. `internal/agent/ingest_computer.go` sends the type's content and the source's settings in `ScanArguments` when the format is `typed`, and `RunScan` hands them to the runner from Milestone 1. Types come from their own registry, `https://raw.githubusercontent.com/teanode/teanode-sources/main/index.json`: `internal/skills/registry.go`'s index fetch, download and signature check are made to take the index address and the public key as parameters so both registries use them, the sources key built in from `internal/sources/keys/teanode-sources-ed25519-public.pem`, and install stores the file in `agent_source_type`. The repository exists (created 2026-09-23 with the tooling of `teanode-skills` and a key of its own) and holds the draft types this plan describes; each is added to its index, signed, once Milestone 4 has run it. API: `ListAgentSourceTypes`, `InstallAgentSourceType`, `AddLocalAgentSourceType` (needs `server:manage`), `RemoveAgentSourceType`, and `SaveAgentKnowledgeSource` accepting `type` and `settings`, validating settings against the type. CLI: `teanode agent source-type list|search|install|add-local|remove`, and `teanode agent knowledge add --type <name> --setting k=v`. Acceptance: install the Gmail type as a local type, add a source of it pointed at an attached computer, and see documents filed; pausing and removing it behave as for any source.

Milestone 4 writes the types that replace the scripts, one at a time, each checked before its source is switched. For each, run `teanode agent knowledge try <source> --type <name> --setting ...`, a dry run that asks the source's computer to run the type and reports how many of the identifiers it produces the existing source already holds, and how many it holds that the type did not produce. A switch goes ahead only when the two agree (a handful of differences explained, not thousands). The types, each named for the service and calling a public command line tool where one exists:

A code host's issues and pull requests with `gh` (repositories the account owns plus named organizations, each organization a listing so that one failing fails the pass). A self-hosted code host with `glab`, run on whichever computer can reach it, which is the reason the computer is chosen per source. A team chat server with its command line tool: channels as containers, posts since the last pass, threads as the record's thread, files as attachments. A wiki with its command line tool: spaces as containers, pages found by query with a limit that fails when full, each page's text from a detail command in markdown. A cloud drive with `gog`: folders as containers, walked by a listing that follows child folders, each file a record, native documents exported through the detail command and other files fetched as attachments. Gmail as in the example above.

Two things the scripts do that a type should not have to: reading a video attachment (describing it and sampling frames) and grouping chat records by thread. The first moves into the daemon's attachment readers in `internal/computer/scan_records.go`, available to every type; the second already happens there for records of kind `chat`.

Milestone 5 is the dashboard. Settings gets a "Source types" section beside Skills, listing installed and local types with install, update and remove. The Knowledge page's add dialog starts with the type (Files and code, Journal, and each installed type), then shows that type's settings as a form, the computer to run it on, and where in memory to file it. A source's card shows its type, its computer and its settings, and editing a setting starts a new pass.

Milestone 6 retires the scripts. With every source switched and a week of passes without a sweep larger than the day's changes, the records folders on the person's computer are left for the person to delete, and `docs/subsystems/memory.md` and the knowledge tool's `shape` text describe types first and the records contract as the path for anything a type cannot say. The `skill` and `web` kinds that were never built are removed, since a type covers what they were for.

## Concrete Steps

For Milestone 1, from the repository root:

    make build
    ./build/teanode computer try-source ~/scratch/gmail.yaml --setting account=someone@example.com --limit 5

Expected: five JSON lines, each with an `id`, a `title` and a non-empty `text`, then a summary line such as `2 containers, 5 records, nothing refused`. With `--setting account=-x`, expected: `the setting account does not match ^[^-][^ ]*@[^ ]+$` and no command run.

Later milestones add their commands to this section as they are built.

## Validation and Acceptance

Each milestone's acceptance is stated with it. Across the whole plan: `make test` passes, with new tests for the parser's refusals, for the runner (a fake tool written as a small Go test binary that prints pages, including one that fails on the second page, which must fail the pass and print nothing), and for the dry run's identifier comparison. The end state is observed on a deployed server: every source on the Knowledge page shows a type; no records folder is needed; a pass over each migrated source files the same documents it did before the switch (the dry run's counts, and document totals within the day's normal change).

## Idempotence and Recovery

Installing a type twice is an update. Switching a source to a type is a settings change on the source and can be reverted by setting its format back to `records` and its path to the old folder, which is kept until Milestone 6. A dry run changes nothing on the server. The migration adding `agent_source_type` has a reverse migration that drops the table; sources of a type fail to read until reverted, and nothing they filed is deleted by the failure, since a failed pass never sweeps.

## Artifacts and Notes

The survey behind this plan found these limits and behaviors worth keeping in the runner: listings cached for a pass so later pages do not list again; retries of a command whose output does not parse, three times with a growing pause; a per-pass budget of time and of fetches, as the shared script library had; a cache of fetched details and files keyed by item and version with a size limit; and records defaulting to private, so that nothing read from a service is shown as public unless the type says so.

## Interfaces and Dependencies

In `internal/sources/source.go`:

    type Type struct {
        Name, Description string
        Requires          []string
        Settings          []Setting
        Containers        []Listing
        Records           Reading
        Detail            *Detail
        Attachments       *Fetch
    }

    func Parse(content []byte) (*Type, error)
    func (self *Type) CheckSettings(values map[string]string) error

In `internal/computer/scan_typed.go`:

    func runTyped(ctx context.Context, options *Options, arguments *ScanArguments) (*ScanResult, error)

`ScanArguments` gains `TypeContent string` and `Settings map[string]string`, sent only when `Format` is `typed`. No new third-party libraries: YAML parsing uses the library `internal/skills` already uses.

Revision note (2026-09-23): source types have a registry of their own, `github.com/teanode/teanode-sources`, instead of a `sources/` directory in the skills registry, and a signing key of their own, both at the person's request. The repository now exists with draft types.
