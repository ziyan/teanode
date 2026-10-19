# The agents a live coding session started, as a tree

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

A Claude Code session often hands parts of a request to agents of its own (Claude Code calls them subagents): one researches, one writes, one codes, and an agent can start agents under it. Today TeaNode shows a live coding session's turns and its screen, and nothing of the agents working under it, so a person watching from the dashboard or the iPhone sees a session that says "working" for ten minutes and cannot tell what it is doing.

After this change, a live session (one of the person's herdr panes) has an Agents view beside Turns and Screen. It shows the session at the top and every agent it started for the current request under the agent that started it: what kind of agent it is, its one-line task, its model, whether it is running, done or stopped, how long it ran, and which tools it used. The session's card says "Needs you" while the session asks the person something. The view refreshes while it is open. A heading says how many agents there are and how many still run. No prices or token counts are shown (the person's decision), and only live sessions have the view; finished sessions in memory do not (the person's decision).

To see it working: in a Claude Code session in a herdr pane on a computer running the TeaNode program, ask for something that starts two or three agents (for example "use three subagents to look at three files and summarize each"). Open the session on the dashboard's Coding page or in the iPhone app's Coding, choose Agents: the session's card is at the top with the agents under it, each Running, then Done as they finish. `teanode computer herdr subagents <pane>` prints the same tree, and the agent's herdr tool has a `subagents` action that answers the same.

## Progress

- [x] Milestone 1: the reader on the computer (`internal/computer/herdr_subagents.go`, `herdr_subagents_test.go`), the `herdr_subagents` action and its feature. (2026-10-19)
- [x] Milestone 2: the server: `Agent.ReadHerdrSubagents`, the GraphQL query `ReadAgentHerdrSubagents`, the herdr tool's `subagents` action, the `teanode computer herdr subagents` command, the parity table, docs. (2026-10-19; `go test` of computer, tools/computer, cmd, client and the apigraph document tests pass)
- [x] Milestone 3: the dashboard's Agents view on the Coding page (2026-10-19): `web/src/components/herdrSubagents.tsx` (tree builder, cards, view), the `ReadAgentHerdrSubagents` document and types in `herdrSessions.tsx`, the three-way view switch and `view=agents` in `pages/coding.tsx`, styles in `style.css`, strings in the three catalogs, tests in `herdrSubagents.test.tsx` and `coding.test.tsx`. Checked: tsc, eslint, prettier on the changed files, vitest, `npm run build`, `make check-catalogs check-link-buttons`; looked at on production at 1400 and 390 px, light and dark, against a live session.
- [x] Milestone 4 (code written 2026-10-19; TeaNodeKit tests pass, 17 Herdr tests; the app compiles first in CI, whose `coding-session-agents-*` screenshots still have to be opened): the iPhone's Agents view in a coding session, its demo and screenshots.
- [x] (2026-10-19) Tried end to end: the program on the person's computer and the server deployed from this branch read a live session's three agents through `teanode computer herdr subagents`, the dashboard and its headless screenshots.
- [ ] Review until a round finds nothing; CI's screenshots opened.

## Surprises & Discoveries

- Claude Code writes each subagent's history beside the session's, as `~/.claude/projects/<folder>/<session id>/subagents/agent-<agent id>.jsonl`, with a small `agent-<agent id>.meta.json` next to it. The metadata already holds what the tree needs: `agentType` (such as `general-purpose`, `Explore`), `description` (the one-line task the parent gave), `model` (sometimes), `name` (sometimes), `toolUseId` (the parent's tool call that started it), `parentAgentId` (the agent that started it, when it is not the session itself), `spawnDepth` (1 under the session, 2 under an agent, and so on), `finished` (written as `true` by newer versions only) and `stoppedByUser`. Every line of the subagent's history carries `timestamp`, and every reply carries `message.model` and its tool calls.
- The reader in `internal/computer/herdr_transcript.go` skips every line marked `isSidechain`, which is how subagent lines appear when they are in the session's own file; that stays as it is, since the turns view is the session's own conversation.
- iPhone, Milestone 4: the Agents part is `ios/TeaNode/Sources/CodingAgentsViews.swift`. The session row is the root of one `DisclosureGroup` whose content is the agents, so the system's own indentation shows the depth and no padding is drawn by hand. The part switch is a `Menu` on the same round glass label (`.glassEffect(.regular.interactive(), in: .circle)`) holding an inline `Picker` of Turns, Agents, Screen, so the current part has a check mark. The old `.buttonStyle(.glass)` button was replaced rather than wrapped, since a `Menu` takes its own label.
- iPhone: the question card is not drawn in the Agents part. A card full of buttons inside a `List` row makes every button in the row fire together unless each is borderless, and the part sits beside Turns, where the card already is. While the session asks, the list's footer says its question is in Turns.
- iPhone: the heading reads "8 agents · 2 running" and drops "· 0 running" when none run. `readHerdrSubagents` sends `computer` as `String!`, which a nullable or required argument both accept.
- Tests found nothing wrong, but the tree function was written to survive a parent that names itself or an agent that names its own child (a loop): those agents go under the session rather than disappear.

- Milestone 3: the Coding page's one round button (Turns or Screen) became a small floating `segmented` group of three icon buttons (Turns, Agents, Screen), with the Agents icon the existing `GraphIcon`, because a single toggle cannot choose among three. The wide tree and the narrow indented list are the same nested `ul`/`li` markup; the frame's container query at 860 px (the one the page already uses) switches the drawing, so no second render path exists. A wide tree with more agents than the pane holds scrolls inside its own box, never the page.
- Milestone 3: the shared polling hook skips a state update when a read is identical, so a running agent's duration is counted up by a one-second clock in the view, not by the reads. The Agents view reads every 3 seconds even when the session is idle, since agents finish after a session goes idle.
- Milestone 3: `prettier --write` on `en.ts`, `zh.ts`, `ja.ts` and `style.css` rewrites hundreds of unrelated lines (as `frontend-design.md` warns); only the files that were new or already mostly mine were formatted.
- The refusal of an old computer program is the server's own message ("...update teanode there and restart it"), shown by the page's existing read-failure toast; the view draws nothing else, per the rule that errors are toasts and not text in the card.

- Milestones 1 and 2: the plan's order for a subagent's state ("running when written in the last 120 seconds or the session is working or asking, then done when the last reply ended its turn") would show every older-version agent as running for as long as the session works, however long ago it ended. `subagentStateOf` therefore checks the end of the turn first: `finished`, then `stoppedByUser`, then a last reply with `stop_reason` `end_turn` (done), and only then recent writing or a working or asking session (running), else stopped. A reply written in pieces, whose last piece has a null `stop_reason`, reads as not ended, so it stays running while the session works.
- Milestones 1 and 2: `readSubagents` returns a `*HerdrSubagentsResult` (the method fills `HerdrSession`) instead of a separate `subagentsRead` type, and finds the person's last turn and the session's model in one backwards pass over `tailLines` with `claudeTurnsOfLine`, instead of `readTranscriptTail`, which parses and joins every turn of 1 MB or more for ten of them. It reads a chunk of 1 MB, doubling to 16 MB while the person's last turn is not in it.
- Milestones 1 and 2: the tools of an agent are counted only for the (at most 100) agents kept, after the filter and the cut, so an old session with hundreds of large agent files costs a first-line and a 256 KB read for each, not a pass over each file.
- Milestones 1 and 2: an agent whose parent was cut by the 100 limit (or is from before the request) names a `parentSubagentId` that is not in the list; the command line shows it with the agents the session started, and the dashboard and iPhone views should do the same.
- Milestones 1 and 2: a session without a history file yet answers with an empty tree rather than the refusal `herdr_read` gives, since an Agents view on a fresh session should say it has no agents, not show an error. The herdr tool refuses on a program without `herdr_subagents` itself (it asks the computer directly, not through `Agent.ReadHerdrSubagents`).

- Looking at it on production showed two things the tests had not. A row of agents wider than the pane was centered and hung off both sides, its first cards out of reach; and in the Coding page's pane (about 700 px beside the session list) four agents never fit side by side, which leaves the session's card over the wrong agents. The view now measures itself and draws the tree only while the session's agents fit side by side (272 px a column, and never under 600 px), and the indented list otherwise.
- An agent that ended its turn and was then handed the end of a command it had left running works again, though its last reply ended its turn. A newer line handed to it after that reply now means it runs.
- The metadata's `model` is the model asked for, often a short name ("sonnet"); the replies carry the full name, which is what is shown.
- A long session's subagents folder has hundreds of agents; an agent whose history file was last written before the request began is passed over before any of its files is read, which also keeps an unfinished agent of an earlier request from showing as running.

## Decision Log

- Decision: the tree is read from the files Claude Code writes, on the person's computer, by the TeaNode program that already reads the session's history for herdr. No hook and no new process. Rationale: the files have everything; hooks would only see agents started after they were installed. Date: 2026-10-19.
- Decision: no cost and no token counts, and only live sessions (herdr panes), as the person chose. Date: 2026-10-19.
- Decision: the view shows the agents of the current request only: those started at or after the session's last person turn, plus any still running from before it. Rationale: a long session starts hundreds of agents; the request at hand is what a person watching wants. At most 100 agents are returned, the newest kept, with `isTruncated` set. Date: 2026-10-19.
- Decision: a subagent's state is `done` when its metadata says `finished: true`, `stopped` when it says `stoppedByUser: true`; otherwise `running` when its history file was written in the last 120 seconds or the session is working or asking, `done` when its last reply ended its turn (`message.stop_reason` is `end_turn`), and `stopped` otherwise. Rationale: older Claude Code versions never write `finished`. Date: 2026-10-19.
- Decision: Codex sessions have no subagents to read: their Agents view shows the session alone. The action still answers, so every surface behaves the same. Date: 2026-10-19.
- Decision: "Needs you" belongs to the session's card only, from `herdrSessionState == "asking"`: a subagent's permission prompt shows in the session's pane, and herdr cannot tell whose it is. Date: 2026-10-19.
- Decision: the heading's running count is left out when nothing runs ("3 agents"), as the session list leaves out its asking and working counts at zero; the count of a folded agent is every agent under it, however deep. Date: 2026-10-19.
- Decision: the word on every surface is "Agents" (the person's word); identifiers say `subagent`, which is Claude Code's. Date: 2026-10-19.

## Outcomes & Retrospective

(To be written when the work is done.)

## Context and Orientation

Herdr is a terminal multiplexer the person runs on their computers; each pane can hold a coding session (Claude Code or Codex). The TeaNode program on the computer (`teanode computer`, code in `internal/computer/`) talks to herdr and answers the server's `herdr_*` actions over the computer connection. `internal/computer/herdr.go` has the types (`HerdrSession`, `HerdrArguments`, `HerdrReadResult`), the action switch (`func (self *Herdr) Do`, the `case "herdr_read":` lines near 947), and `read` (near 970), which finds the session (`self.session(ctx, paneId)`) and reads its history file (`session.TranscriptPath`, found by `findTranscript` in `herdr_transcript.go`). Features the program has are announced in `internal/computer/computer.go` near line 233 (`hello.Features = append(hello.Features, FeatureHerdr, ...)`), and the read-only actions are listed near line 373 (`"herdr_read": true, ...`).

The server side is `internal/agent/herdr.go`: `ReadHerdrSession` (near line 216) asks the computer with `askHerdr[computer.HerdrReadResult](ctx, attached, "herdr_read", ...)`; `attached.HasFeature(computer.FeatureHerdrFiles)` is how a newer action is refused on an older program. GraphQL is in `internal/api/v1api/apigraph/agent_herdr.go` (`ReadAgentHerdrSession` near line 323, views such as `AgentHerdrReadView`); the schema is generated from the Go types and their doc comments (`make generate` regenerates `internal/util/commentparse/commentparse_gen.go`; run it after adding types). The agent's tool is `internal/agent/tools/computer/herdr.go`; `HerdrActions` (near line 43) lists every action on every surface and a test fails when a surface lacks one. The CLI is `internal/cmd/computer_herdr.go` with the client in `internal/client/agent_herdr.go`.

The dashboard is React in `web/`. The Coding page is `web/src/pages/coding.tsx`: the view is in the URL (`view=screen`, near line 576), a round button switches Turns and Screen (near line 854), and a polling hook (near line 110) reloads while the tab is visible. GraphQL documents for herdr are in `web/src/components/herdrSessions.tsx` (`HERDR_DOCUMENTS`, `SESSION_FIELDS` near line 95). Strings are in `web/src/i18n/{en,zh,ja}.ts`. Read `docs/coding/frontend-design.md` before writing the view.

The iPhone app is SwiftUI in `ios/TeaNode/Sources/`. `CodingViews.swift` has `enum CodingPart { case turns, screen }` (near line 440), `@State private var part` (near 457), and `content(_:)` (near 556), which shows `transcript` or `screen` and floats one round button that switches them. The GraphQL calls live in TeaNodeKit, `ios/TeaNodeKit/Sources/TeaNodeKit/Herdr.swift` (`readHerdrSession` near 237), testable on Linux in Docker. The demo server `ios/TeaNode/Sources/DemoServer.swift` answers screenshots (`ReadAgentHerdrSession` near line 216); CI's `shoot` lines are in `.github/workflows/ios.yml` near line 334 (`coding-session-light` and others). Strings go in `ios/TeaNode/Resources/Localizable.xcstrings` in English, Simplified Chinese and Japanese. Read `ios/DESIGN.md` before writing the view. There is no Swift compiler for the app on this machine: avoid a ternary mixing nil with a closure or method reference in a view body, `==` on types that are not Equatable, and very long view expressions; CI's App job is the first compile.

Names follow `CONTRIBUTING.md`: no bare `state`, `type`, `count`; booleans start with `is`/`has`; acronyms as `SubagentID` in exported Go and `subagentId` in JSON. Tests and examples use invented content only: no real paths under a home directory (use `~/…` or a temp dir), no real session ids, names or repositories.

## Plan of Work

Milestone 1, the reader. Add `internal/computer/herdr_subagents.go` with the types under Interfaces and a function `readSubagents(transcriptPath string, sessionState string, now time.Time) (*subagentsRead, error)`. The subagents folder is the transcript's path without `.jsonl`, plus `/subagents`. For each `agent-*.meta.json` read the metadata; for its `.jsonl`, read the first line's `timestamp` (started), the last line's `timestamp` (last written), the model from the first reply with `message.model` when the metadata has none, the tool calls (`message.content[]` items of `type: "tool_use"`, by `name`; an MCP tool `mcp__<server>__<tool>` counts as `<server>`), and the last reply's `message.stop_reason`. A subagent's parent is `parentAgentId` when set, otherwise the session (empty `ParentSubagentID`). The session's own model is the last `message.model` in the main transcript's last 1 MB, and the current request's start is the last person turn's time there (reuse `readTranscriptTail` and the `HerdrTurnRolePerson` turns). Keep the agents started at or after it, plus those still running; sort by start; keep the newest 100. Files that cannot be read are skipped. A history file may be large: read the first line, and the last 256 KB for the end, never the whole file, except to collect tool names, which reads the file once line by line with a 16 MB buffer and stops at 32 MB. Add `herdr_subagents` to the action switch (a method `subagents`, shaped like `read`), to the read-only list in `computer.go`, and the feature `FeatureHerdrSubagents = "herdr_subagents"` to the announced features. A Codex session answers with no subagents. Tests: build a temporary home with a transcript and a subagents folder of invented files (a done agent with `finished`, one stopped by the user, one running, one nested under another, one from an earlier request that is dropped, one Codex session) and check the tree, states, parents, models, tool names and the truncation.

Milestone 2, the server and the command line. `internal/agent/herdr.go`: `ReadHerdrSubagents(ctx, agentId, computerName, paneId)` like `ReadHerdrSession`, refusing with "the program on %s is too old to show a session's agents; update teanode there and restart it" when `!attached.HasFeature(computer.FeatureHerdrSubagents)`. GraphQL `ReadAgentHerdrSubagents(computer, paneId)` returning `AgentHerdrSubagentsView`. The herdr tool's `subagents` action (it reads, so it needs no approval, like `read`); its description says what the tree is. The command `teanode computer herdr subagents <pane> [--computer] [--json]` prints the tree indented, one agent a line: state, type, task, model, time. Add the row to `HerdrActions`. `make generate`, then the parity and tool tests. Update `docs/subsystems/coding-sessions.md` ("The agent into sessions") and `docs/reference/command-line.md`.

Milestone 3, the dashboard. On the Coding page, Agents is a third view (`view=agents` in the URL), reached from the same place as Screen: replace the single switch button with a small segmented control of three (Turns, Agents, Screen) using the page's existing toggle-button components, or, if the toolbar has no room at phone width, keep the button and add the third choice to it; follow `frontend-design.md`. The view polls `ReadAgentHerdrSubagents` every 3 seconds while visible (the existing polling hook). At desktop width it draws a tree: the session's card centered at the top, its agents in a row under it with lines joining them, and each agent's own agents in a column under it with a line down its left; cards are the page's existing card style, about 260 px wide, showing the type (bold) and model (muted) on the first line with a state badge on the right (Running with a spinner, Done with a check, Stopped grey, Needs you amber on the session's card only), the task in two lines at most, the tools used as muted words, and the duration. A card with agents under it shows their count with a chevron and collapses them. Under 860 px the same cards are an indented list (each level indented 16 px with a line down the left), never a sideways-scrolling tree. The heading reads "8 agents · 2 running". With no agents yet the view says the session has not started any agents for this request. An old computer program's refusal shows as the toast the page already uses for errors, and the view says to update the program. Strings in three languages. Tests with vitest for the tree layout (parents, collapse, states) and the URL.

Milestone 4, the iPhone. TeaNodeKit: `HerdrSubagents` and `HerdrSubagent` Decodable types and `readHerdrSubagents(computer:paneId:)`, with a decoding test. `CodingPart` gains `agents`; the floating round button becomes a `Menu` with the three parts (symbols `text.bubble`, `point.3.connected.trianglepath.dotted`, `terminal`), or the system's segmented `Picker` in the navigation bar if `ios/DESIGN.md` says so. The Agents part is a `List` of rows indented by depth (a `DisclosureGroup` for an agent with agents under it), each row: type and model, the task, the state as a label in the state's color (green Done, the accent for Running with a `ProgressView`, secondary for Stopped, orange for Needs you on the session row), tools and time in the footnote style. It reloads every 3 seconds while shown. Demo: `DemoServer` answers `ReadAgentHerdrSubagents` with an invented tree (a session building a pricing page with a researcher, a writer and a coder, two of them with agents under them, mixed states), and `-TeaNodeScreen coding-session-agents` opens it; add `shoot coding-session-agents-light`, `-dark` and `-largest-text` lines. Strings in the catalog in three languages.

## Concrete Steps

From the repository root:

    go test ./internal/computer/ -run Subagent
    make generate
    go test ./internal/agent/... ./internal/api/... ./internal/cmd/... ./internal/client/...
    cd web && npx tsc --noEmit -p . && npx eslint . --max-warnings 0 && npx vitest run src/pages/coding
    cd ios/TeaNodeKit && docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD":/kit -w /kit swift:6.1 bash -c 'swift build --build-tests -q; swift test --skip-build --filter Herdr'
    make lint-ci

## Validation and Acceptance

On a computer running the new program, in a herdr pane's Claude Code session, ask it to use three subagents. `teanode computer herdr subagents <pane>` prints the session and three agents, Running, then Done. The dashboard's Coding page with `view=agents` shows the same tree at 1400 px and the indented list at 390 px, light and dark. The iPhone screenshots `coding-session-agents-*` show the demo tree. An old program on the computer gives the refusal, not an empty tree.

## Idempotence and Recovery

Nothing is written anywhere: the reader only reads files, and the server only relays. Rerunning any step is safe.

## Interfaces and Dependencies

In `internal/computer/herdr_subagents.go`:

    const FeatureHerdrSubagents = "herdr_subagents"   // in herdr.go beside the others

    const (
        HerdrSubagentStateRunning = "running"
        HerdrSubagentStateDone    = "done"
        HerdrSubagentStateStopped = "stopped"
    )

    // HerdrSubagent is one agent a session started, or one an agent started.
    type HerdrSubagent struct {
        SubagentID       string     `json:"subagentId"`
        ParentSubagentID string     `json:"parentSubagentId,omitempty"` // empty: the session started it
        SubagentType     string     `json:"subagentType"`              // agentType, such as general-purpose
        SubagentName     string     `json:"subagentName,omitempty"`
        TaskText         string     `json:"taskText"`                  // the description its parent gave
        ModelName        string     `json:"modelName"`
        SubagentState    string     `json:"subagentState"`
        SpawnDepth       int        `json:"spawnDepth"`
        StartedAt        *time.Time `json:"startedAt,omitempty"`
        EndedAt          *time.Time `json:"endedAt,omitempty"`         // nil while running
        ToolNames        []string   `json:"toolNames"`                 // most used first, at most 5
        ToolCallCount    int        `json:"toolCallCount"`
    }

    // HerdrSubagentsResult is the agents of a session's current request.
    type HerdrSubagentsResult struct {
        HerdrSession     *HerdrSession    `json:"herdrSession"`
        SessionModelName string           `json:"sessionModelName"`
        RequestStartedAt *time.Time       `json:"requestStartedAt,omitempty"`
        Subagents        []*HerdrSubagent `json:"subagents"`
        IsTruncated      bool             `json:"isTruncated"`
    }

The GraphQL view `AgentHerdrSubagentsView` has the same fields (`herdrSession` as `AgentHerdrSessionView`), and `AgentHerdrSubagentView` mirrors `HerdrSubagent` with times as `Time`. The query is `ReadAgentHerdrSubagents(computer: String!, paneId: String!)`, the tool action `subagents`, the command `subagents`.
