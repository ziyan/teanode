# Memory in the person's coding sessions

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

A person who codes with Claude Code or Codex starts every session from nothing. The tool does not know what the project decided last week, what failed the last time somebody tried a command, or where the previous session stopped, although TeaNode has read all of it: the checkout's page, the facts filed from earlier sessions, and the lessons from verified work. Today a coding tool sees that memory only when it decides to call one of TeaNode's tools over the Model Context Protocol, which it rarely does, so it spends turns rediscovering what is already known.

After this change the person runs `teanode hook install claude-code` (or `codex`) once on a computer. From then on, every session of that tool on that computer:

1. starts with a short block about the checkout it was opened in: each project page the checkout is filed on, with its summary and liveliest facts, and where the last session in that directory stopped;
2. gets, before each prompt the person types, the memory that prompt recalls and any lesson close to it, kept to the checkout's project and the work pages linked to it, within a token budget, and never the same page twice in a few prompts;
3. is read into TeaNode within about a minute of each answer, rather than at the source's nightly pass, so the next session sees this one.

Separately, a knowledge search over indexed code can be narrowed to a directory and says which directories its hits cluster in, so an agent can look at the right part of a large tree first and then search inside it.

To see it working: install the hooks, open `claude` in a checkout TeaNode has profiled, and the first answer can say what the project is and what was done last time without reading a file; ask about a convention the person stated in an earlier session and the answer uses it without searching. `teanode agent memory checkout <directory>` prints the same block a session starts with, and `--prompt "<words>"` prints what a prompt would recall.

## Progress

- [x] (2026-10-09 15:40Z) Researched the hooks of both tools and how transcripts, checkouts and lessons are stored today (see Context and Orientation).
- [x] (2026-10-09 16:30Z) Milestone 1: the agent and API side: finding a checkout's project pages from a directory, the session-start block, scoped prompt recall, capture; migration 0156 for the directory lookup.
- [x] (2026-10-09 16:30Z) Milestone 2: the command line: `teanode hook claude-code|codex`, `teanode hook install|uninstall`, `teanode agent memory checkout`.
- [x] (2026-10-09 16:45Z) Milestone 3: directory-first knowledge search on the knowledge tool, `SearchAgentDocuments`, `teanode agent knowledge search --directory` and the documents dialog.
- [x] (2026-10-09 16:50Z) Milestone 4: the Recall dialog's checkout field and the memory tool's `checkout` action (also reachable over MCP).
- [x] (2026-10-09 17:10Z) Deployed and run live: the session-start block and a prompt's recall checked on the development checkout; three fixes from what the live output showed (every project page the checkout is filed on, linked people and months kept out, lessons only above a stricter similarity).
- [x] (2026-10-09 17:15Z) Real hooks in real sessions: `codex exec` and `claude -p` both answered "what was the last session here about" from the block, in one turn with no tool call; a Stop event set the claude-code source due at once.
- [ ] Milestone 5: the measured comparison with and without the hooks (running), and capture going ahead of long passes on the same computer (committed, deploy after the trial).

## Surprises & Discoveries

- Observation: the line that ties a checkout to a page is a fact, "The checkout is at <directory> on <computer>.", and the dream moves such facts onto subpages: the development checkout's line sits on `projects/teanode/operations`, not on `projects/teanode`.
  Evidence: a query over `agent_fact` for that text found it on `projects/teanode/operations` and `projects/teanode/agent/teanode-development-and-repository`, with superseded copies beside them.
- Observation: most units filed from coding sessions hold one post, because a long answer fills the 3000-character window alone, and the dream reads a chat unit only when it has two posts or more.
  Evidence: the three newest Claude Code documents in production each carry `"posts": 1`.

- Observation: both tools keep what a hook adds out of what the transcript sources read. Claude Code writes it as an `attachment` line of type `hook_additional_context` (the claude-code type reads only queued commands among attachments); Codex writes it as a `developer` message (the codex type reads only `user` and `assistant`). No change to the source types was needed for the injected block not to be filed back as something the person said.
  Evidence: a probe hook's text appeared once in each transcript, on those line types, and nowhere the types read.
- Observation: a capture's pass waited behind four other sources on the same computer, each part way through a long pass (chat, mail, a code tree, the other tool's transcripts); the server gives a computer to one source at a time, the longest waiter first.
  Evidence: the claude-code source said "waiting its turn on gen7, which is reading chat" for over ten minutes after a Stop event made it due.
- Observation: the first live session start resolved the development checkout to the profile's own page (`projects/<name>-<parent>`), which held three facts, and left out the older page the night had grown around it, which held almost everything; the one lesson matched to the whole project was about downloading photos; and a prompt's recall carried a colleague's page and a month page through their links to the project.
  Evidence: the output of `teanode agent memory checkout .` before and after the fixes in commits 8f29f220, 27cc3bdc and da9a97c9.
- Observation: a memory fact can be about a neighbouring part of the system and still read as an answer. Asked which GraphQL calls the command-line client retries, memory offered the dashboard client's rule (a read-only query once); with the hooks both tools still checked the code and answered for the command line correctly.
  Evidence: the trial's "retries" answers in both arms.

## Decision Log

- Decision: hooks that push memory into the session, not only the existing MCP tools that wait to be called.
  Rationale: a coding tool rarely decides to ask; a hook puts the memory in front of it every time, at a fixed and small cost. Recall needs no model since it was made fast, so the cost is one embedding and a few queries per prompt.
  Date/Author: 2026-10-09, the person and agent.
- Decision: recall in a coding session is kept to the checkout's project page, the pages under it, the pages it links to, and lessons. A flag widens it to everything.
  Rationale: the person's whole graph holds mail, finance and family; a prompt about a build should not carry them into a coding tool's context, which may be a third party's model.
  Date/Author: 2026-10-09, agent.
- Decision: where the last session stopped is taken from the stored transcript (its title, the person's last requests, the assistant's last answer), not written by a model.
  Rationale: it costs nothing, it is never wrong about what was said, and it is there seconds after capture. A summary written by a model would need a model call at every session start or a dream before it exists.
  Date/Author: 2026-10-09, agent.
- Decision: capture asks the computer's existing `claude-code` or `codex` source to read again now, rather than adding a second way for transcripts to arrive.
  Rationale: the sources already cut transcripts into documents, leave out tool output and injected blocks, and skip unchanged files by modification time. A second path would need its own filtering and would file the same words twice.
  Date/Author: 2026-10-09, agent.
- Decision: the hook never blocks or fails the coding tool. Any error prints nothing and exits 0.
  Rationale: memory is a help; a server that is down must not stop the person typing.
  Date/Author: 2026-10-09, agent.

- Decision: show every project page a checkout is filed on, the profile's own first; keep linked pages to projects, topics, things and folders; show lessons only for a prompt, above a similarity of 0.45.
  Rationale: what the first live runs showed (see Surprises). People and months linked to a project are the person's business, not a coding tool's, and the lessons on file are about the agent's own errands.
  Date/Author: 2026-10-09, agent.
- Decision: a coding tool's transcripts starting a pass go ahead of other sources waiting for the same computer, never ahead of the one reading now.
  Rationale: the pass is a session asking to be read in and its pages are quick; without it a capture waited behind hours of other sources' pages. At first only a pass's first page was urgent, and a capture then waited behind the other sources one page at a time; every page is urgent now, which a long first read of the whole store also gets, once.
  Date/Author: 2026-10-09, agent.

## Outcomes & Retrospective

(To be written at the end of each milestone.)

## Context and Orientation

TeaNode is a Go server with a command line (`cmd/teanode`, commands in `internal/cmd`), a GraphQL-like API declared by reflection (`internal/api/v1api/apigraph`, every exported method of `*graph` is an operation, documented by its Go comment), a client library the command line uses (`internal/client`), and a React dashboard (`web/src`). A person's agent keeps a memory graph: pages (`agent_node`, `models.AgentNode`) at paths such as `projects/teanode`, each with a summary, and numbered facts on them (`agent_fact`, `models.AgentFact`). A lesson is a fact of kind `lesson` on a page under `lessons/`, written only from TeaNode's own conversations when a command it names succeeded (`internal/agent/lessons.go`, `LessonsForQuestion`).

Recall is the step that picks which pages and facts a question carries into a model's prompt: `Agent.RecallForQuestion` in `internal/agent/graph_recall.go` runs a word search and a vector search over pages, facts and passages, fuses them, and chooses blocks under a token budget (1200 tokens by default). It moves no `used_at`, so asking it changes nothing. The API exposes it as the query `RecallAgentMemory`, and `teanode agent memory recall` prints it.

A knowledge source is something a computer attached with `teanode computer` reads for the agent (`agent_source`, `models.AgentKnowledgeSource`). Two installed source types, `claude-code` and `codex` (`internal/sources/testdata/registry/claude-code.md` and `codex.md`), run `jq` over the tools' transcripts on the computer and send what was said, cut into chat units of at most 40 posts or 3000 characters. Each unit is a document (`agent_document`) whose `external_id` is the transcript file path under `~/.claude` or `~/.codex` followed by `#` and the first post's id, and whose metadata carries `directory` (the session's working directory), `branch`, `assistant` and `channel` (the session title). The text is lines of the form `15:04 <author>: <words>`, where the person's author has been replaced by their username. A source runs on its cron (nightly by default), or at once when its `next_run_at` is set to now, which is what `SyncAgentKnowledgeSource` does; the server looks for due sources every 15 seconds.

A checkout (a git working copy) under a `files` source is profiled into a project page, `<root>/<name>`, and a fact "The checkout is at <directory> on <computer>." is filed on it. The dream may later move that fact to a page under the project. There is no other link from a directory to a page.

Claude Code and Codex both run hooks: commands named in `~/.claude/settings.json` (key `hooks`) or `~/.codex/hooks.json`, run at points in a session's life with a JSON object on standard input. Both give `session_id`, `transcript_path`, `cwd` and `hook_event_name`; `UserPromptSubmit` adds `prompt`, `SessionStart` adds `source` (`startup`, `resume`, `clear` or `compact`). A hook adds text to the model's context by printing `{"hookSpecificOutput": {"hookEventName": "<event>", "additionalContext": "<text>"}}` on standard output. Codex gives `SessionEnd` at most three seconds and asks the person to trust a hook command before it first runs (the trust is recorded against the command's hash in `~/.codex/config.toml`). Both files may already hold hooks from other programs (herdr installs one), which must be kept.

## Plan of Work

Milestone 1 adds `internal/agent/coding.go`. `Agent.CheckoutPage(ctx, found, directory, computerName, homeDirectory)` lists the live checkout facts of the agent (a new database method, `ListAgentCheckoutFacts`, selecting facts whose text starts with "The checkout is at " and are neither superseded nor dormant), reads each back with `checkoutLocationOf`, expands a leading `~` with the given home directory, and keeps those whose directory is the given one or an ancestor of it, on the given computer when one matches, else on any. The longest directory wins. From the fact's page it walks up the parents to the highest page of kind `project` below the root and returns that path with the checkout directory.

`Agent.CodingSessionStart(ctx, found, owner, request)` builds the session-start block: the project page's name and summary, its first facts (the most lively, up to eight, within 600 tokens), up to two lessons for a question made of the project's name and summary, and the last session in that directory: documents of kind chat from a `claude-code` or `codex` source whose metadata `directory` is the session's directory, newest first, skipping the session being started (its id appears in the external id). It shows that session's title and age, the person's last three requests and the assistant's last answer, each cut to a few hundred characters. A new database method `ListAgentCodingDocuments(agentId, directory, limit)` reads those documents.

`Agent.CodingPromptRecall(ctx, found, owner, request)` runs `RecallForQuestion` on the prompt, keeps the pages in scope (the project page, pages under it, pages it is linked to by an edge either way, and `lessons/`), drops pages in the request's list of pages shown recently, adds the lessons for the prompt, and renders it. A prompt of fewer than three words, or one that starts with `/`, recalls nothing.

`Agent.CaptureCodingSession(ctx, found, computerName, assistant)` sets `next_run_at` to now on the enabled sources of type `claude-code` (or `codex`) on that computer, unless the source is already due or ran in the last 30 seconds.

The API gets `internal/api/v1api/apigraph/agent_coding.go` with the queries `CodingSessionContext` and `RecallCodingMemory` (both added to `isModelBackedQuery`, since they embed) and the mutation `CaptureCodingSession`. Each returns the rendered text and its parts (project path, pages and facts shown, lessons, last session), so the command line prints the text and the dashboard shows the parts.

Milestone 2 adds `internal/cmd/hook.go`: `teanode hook claude-code` and `teanode hook codex` read the event from standard input and act on `hook_event_name`. `SessionStart` prints the session-start block; `UserPromptSubmit` prints the prompt's recall and records the pages shown in `~/.cache/teanode/hooks/<session id>.json`, so the next five prompts skip them; `Stop`, `PreCompact` and `SessionEnd` start a detached `teanode hook capture` and return at once. The block is wrapped in `<teanode-memory>` so the source types can leave it out of what they read. `teanode hook install claude-code|codex` adds these hooks to the tool's file, keeping every other hook, and `uninstall` removes only TeaNode's. `teanode agent memory checkout [directory] [--prompt words] [--computer name]` prints what a session would see.

Milestone 3 lets `indexed.Search` take a directory: the absolute directory is matched to the sources on that computer whose path holds it, and the search is kept to documents whose external id is under the remainder. The result gains `Directories`: the directories the passages found fall in, with how many each, so an agent sees where hits cluster before reading any. The knowledge tool, `SearchAgentDocuments`, `teanode agent knowledge search --directory` and the dashboard's search all take and show it.

Milestone 4 adds a `checkout` action to the memory tool (the same block for a directory, so the agent itself can see what a session there starts with) and a Coding sessions section on the dashboard's Knowledge page that previews the block and a prompt's recall for a directory and computer.

Milestone 5 installs the hooks on the development computer, runs real sessions of both tools, and checks each step in the logs and the database; then runs a fixed set of questions about a checkout with and without the hooks (`claude -p --output-format json` and `codex exec --json`), comparing turns, tokens, time and whether the answer was right.

## Concrete Steps

From the repository root:

    make build
    go test ./internal/agent/ -run Coding
    go test ./internal/cmd/ -run Hook
    ./build/teanode agent memory checkout "$PWD"
    ./build/teanode hook install claude-code

## Validation and Acceptance

`teanode agent memory checkout <a profiled checkout>` prints the project page, its facts and the last session there. In a new Claude Code session in that checkout the transcript shows the hook's context before the first answer, and asking "what were we doing here last time?" is answered without a tool call. After an answer, the claude-code source on that computer runs within a minute and the new turn is searchable with `teanode agent knowledge search`. The measurement in Milestone 5 is recorded in Outcomes with the numbers.

## Idempotence and Recovery

`hook install` run twice leaves one set of hooks; `uninstall` removes only commands that start with the TeaNode hook command. The cache under `~/.cache/teanode/hooks` may be deleted at any time. No migration is needed; the new database methods only read.

## Artifacts and Notes

(Filled in as the work produces them.)

## Interfaces and Dependencies

In `internal/agent/coding.go`:

    type CodingRequest struct {
        Directory, ComputerName, HomeDirectory, SessionID, Prompt string
        ShownPaths []string
        IsEverywhere bool
    }
    type CodingContext struct {
        ProjectPath, CheckoutDirectory, Text string
        Pages []*RecalledPage
        Lessons []string
        LastSession *CodingSession
    }
    func (self *Agent) CodingSessionStart(ctx context.Context, found *models.Agent, owner *models.User, request *CodingRequest) (*CodingContext, error)
    func (self *Agent) CodingPromptRecall(ctx context.Context, found *models.Agent, owner *models.User, request *CodingRequest) (*CodingContext, error)
    func (self *Agent) CaptureCodingSession(ctx context.Context, found *models.Agent, computerName, assistant string) (bool, error)
