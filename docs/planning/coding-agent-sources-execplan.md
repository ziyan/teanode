# Claude Code and Codex as sources

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

People who work with coding agents say a great deal to them: what they are building, why, what went wrong, what they decided, what they want remembered. Claude Code and Codex keep all of it on the person's computer, and both keep a memory of their own. TeaNode reads none of it today.

After this change a person adds a source of type `claude-code` or `codex` from the registry, chooses the computer, and the agent reads that tool's conversations and memory the way it reads a chat archive: the conversations become documents it can search and file facts from at night, and the memory files become pages of notes. To see it working: on a computer running `teanode computer` with `jq` installed, add a Claude Code source; after the next pass, `teanode agent knowledge search "<something said to Claude Code>"` finds the conversation, and after a night, facts from it appear on the person's pages with the conversation as their evidence.

## Progress

- [x] (2026-09-24) Wrote this plan after reading both stores on a real computer and the source code.
- [x] (2026-09-24) First built as two readers in Go inside `teanode computer`; replaced, the same day, by two registry types (see the Decision Log).
- [x] (2026-09-24) Milestone 1: `claude-code.md` and `codex.md` in `internal/sources/testdata/registry`, each a `jq` program over the tool's files; end to end tests with invented sessions in `internal/computer/scan_coding_agent_types_test.go`.
- [x] (2026-09-24) Milestone 2: the server files a post whose author is `@you` under the owner's username (`namePerson` in `internal/agent/ingest_page.go`).
- [x] (2026-09-24) Found and fixed on the way: the daemon's one-file records cache was not dropped at the start of a typed source's pass, so a type with one container never read it again.
- [ ] Milestone 3: the two types in the registry repository, signed; the daemon and server deployed; a real pass on the development computer and a search that finds something said in a session.

## Surprises & Discoveries

- Observation: most of what the tools store is not conversation. The largest Claude Code session on the development computer is 233 MB; cut to what was said it is 25 MB, and `jq` does it in 3.5 seconds in 39 MB of memory. The store is 1.3 GB for 18 project folders; Codex keeps 165 MB in 119 session files.
- Observation: the type language could already express nearly all of this (a `*.text` path picks the text parts of a message and nothing else), and a type may run any tool it declares under `requires`. `jq` covers the rest: removing a block the tool injected into the middle of a turn, and reading a session's title, directory and branch once for all its posts.
- Observation: most Codex sessions on the development computer were started by a program (`source: exec`), with skill lists injected into the prompt; subagent sessions name their parent in `source`. Neither is the person talking.
- Observation: a command word that renders empty is dropped with its flag, which is how a type leaves an unset option out. An empty `--arg exclude ""` therefore shifted jq's arguments; the types pass `({{settings.exclude}})` so the word is always there.
- Observation: the night reads a chat unit only if its participants include the person's username or name (`reading.ChatNamesOf`), and a type on the person's computer does not know their TeaNode username.

## Decision Log

- Decision: two registry types running `jq`, not readers written in Go.
  Rationale: both session formats are undocumented and change with the tools; a type is fixed with a signed registry update, a reader only with a TeaNode release and a daemon upgrade on every computer. The Go readers were about 500 lines; the types need no engine change. The cost is that `jq` must be installed where the source runs, which the type declares and the daemon checks.
  Date/Author: 2026-09-24, the person and agent.

- Decision: only what was said is kept. From each transcript: what the person typed, and the assistant's visible answers. Tool calls and their results, thinking, hook and system messages, attachments, side chains (subagents), the tools' own compaction summaries, Codex's injected context, and Codex sessions a program or another session started are left out. A conversation carries its title, working directory, branch and (Claude Code) pull requests as metadata.
  Rationale: the person asked for it, and it is what a person would call the conversation. Tool output is most of the volume, is often the contents of files already read from their own sources, and would drown the facts.
  Date/Author: 2026-09-24, the person and agent.

- Decision: a conversation is cut into units the way a chat is: a window ends at a thirty minute silence or at a size, so a growing session changes only its last unit, and older units keep their identifiers and are sent as unchanged. Only what never changes is in a unit's text; a retitled session does not change its units' hashes.
  Rationale: a session can run for days; one document for all of it would be read again after every new message.
  Date/Author: 2026-09-24, agent.

- Decision: a file is run through `jq` again only when it changed since the last pass (`since` with `unchangedWhen` on the file's modification time); what it printed is kept in the source's cache directory meanwhile.
  Rationale: running `jq` over 1.3 GB every pass costs minutes of processor time for nothing; the kept records are the conversation only, a small part of that.
  Date/Author: 2026-09-24, agent.

- Decision: Codex's SQLite memory is not read; its Markdown memory, `AGENTS.md` and the sessions are.
  Rationale: the summaries in SQLite are made from sessions the type already reads, and the table is empty where this was built.
  Date/Author: 2026-09-24, agent.

- Decision: a record whose author is `@you` (`computer.PersonAuthor`) is the person's; the server writes their username in its place, in the text and among the participants, for any source. The hash is left as the daemon made it.
  Rationale: the night's rule that a chat is read only if the person was in it stays as it is, and a type need not know who the person is. Any other type reading the person's own tools can use it.
  Date/Author: 2026-09-24, agent.

## Context and Orientation

A source is a row of `agent_source` (`models.AgentKnowledgeSource`, `internal/models/knowledge.go`) of a source type. A type is a Markdown file with a YAML header, published in the signed registry `github.com/teanode/teanode-sources` (`sources/<name>/source.md`, listed with its hash and signature in `index.json`) and parsed by `internal/sources`. The server asks the daemon on the source's computer, `teanode computer`, to scan it a page at a time; for a typed source the daemon runs the type (`internal/computer/scan_typed.go`): its `containers` listings name the files, its `records` reading runs a command per file and maps each line printed to a record, and the records reader (`internal/computer/scan_records.go`) cuts chat records into units (`chatUnits` in `chat_units.go`). The server files each entry as a document (`fileComputerPage` in `internal/agent/ingest_page.go`).

Claude Code keeps each session as `~/.claude/projects/<folder named after the working directory>/<session>.jsonl`: one JSON object a line, with `type` `user`, `assistant`, `system`, `custom-title`, `pr-link` and a dozen more. A user line's `message.content` is a string (what the person typed) or a list of parts (`text`, `image`, `tool_result`); an assistant line's parts are `text`, `thinking` and `tool_use`. Lines carry `timestamp`, `cwd`, `gitBranch`, `isSidechain`, `isMeta`, `isCompactSummary`. Its memory is `~/.claude/CLAUDE.md` and `~/.claude/projects/*/memory/*.md`.

Codex keeps each session as `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl`: lines with `type` and `payload`. The first, `session_meta`, carries `cwd`, `git` and `source` (`cli`, `exec`, or an object naming a subagent); `response_item` lines with payload type `message` carry `role` (`user`, `assistant`, `developer`) and content parts. Its memory is `~/.codex/AGENTS.md` and `~/.codex/memories/**/*.md`.

## Plan of Work

Milestone 1 wrote the types and their tests. Milestone 2 made the server name the person. Milestone 3 publishes the types: copy them to `sources/claude-code/source.md` and `sources/codex/source.md` in the registry repository, add them to `index.json`, `make sign` with the registry key (kept out of the repository), open its pull request; deploy the server and restart the daemon on the development computer with the cache fix; once the registry has them, add a Claude Code source there and check a pass and a search.

## Concrete Steps

    go test ./internal/computer/ -run 'TheClaudeCodeType|TheCodexType'
    go test ./internal/sources/ ./internal/agent/ -run 'Registry|Person'
    make lint-ci

## Validation and Acceptance

The tests hold invented sessions with typed prompts, tool calls, tool results, thinking, a side chain, a meta message, a compaction summary, injected blocks, and Codex subagent and scripted sessions; only the prompts and the visible answers come out, and a session that grows sends its old unit as unchanged and only the new talk in full. On the development computer, a pass over `~/.claude` files the memory pages and the conversations, and a knowledge search for a phrase from a recent session finds it.

## Idempotence and Recovery

Units are keyed by session file and first post, so passes send only what changed. Deleting a source deletes its documents. The types only read.

## Interfaces and Dependencies

`jq` on the computer the source reads (tested with 1.7). New: `computer.PersonAuthor`, `namePerson` in `internal/agent`.
