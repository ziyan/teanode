# Coding sessions on the dashboard and the iPhone

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md` (the ExecPlan rules) and builds on `docs/planning/herdr-execplan.md`, which introduced herdr support.

## Purpose / Big Picture

Herdr is a terminal program on the person's computer that keeps Claude Code and Codex sessions in panes, grouped into workspaces (one per project, usually) and tabs inside each workspace. TeaNode already reaches those sessions through the program `teanode computer` running on each computer: the agent's `herdr` tool, the GraphQL operations behind it, and `teanode computer herdr ...` on the command line can list sessions, read a session's last turns, type into it, answer its questions, open a new one and close one. The dashboard only has a card buried in Settings › Assistant › Connections that opens a dialog, and the iPhone app can only answer a session's question from a notification card.

After this change the person can, on the dashboard and on the iPhone alike:

- open a Coding page that lists their herdr sessions by computer and workspace;
- pick a session and read its transcript live (the turns from its history file, refreshed while it works), or its screen as it stands;
- write a prompt, edit it, attach files, and send it;
- switch between the tabs of that workspace with a tab strip, the way herdr itself shows them;
- open a new tab beside the current one (a "sibling": same workspace, same directory, Claude Code or Codex);
- close a tab's session;
- bring that tab forward in herdr on the computer with "Show on computer", which is a separate button so browsing from the phone never moves the terminal under someone at the desk.

The command line and the agent's tool gain the same two new abilities (focus a session's tab, open beside a session), so every surface can do the same things.

To see it working: open `/coding` on the dashboard with a computer running herdr attached, click a session, watch its turns appear, switch tabs, send "say hello", and see the reply arrive; on the iPhone, open Coding from the side menu and do the same.

## Progress

- [ ] Milestone 1: server side: session placement fields, `focus` action, `open` beside a pane, on the computer program, agent tool, GraphQL, Go client, CLI, tests, docs.
- [ ] Milestone 2: dashboard Coding page, rail row, settings card linking to it, i18n in en/zh/ja.
- [ ] Milestone 3: iPhone Coding screen, TeaNodeKit operations and tests, demo data, CI screenshots, strings in three languages.
- [ ] Milestone 4: deploy the branch and the computer program on gen7, use it for real from desktop and phone widths, look at every screen, review until clean, parity check.

## Surprises & Discoveries

- herdr 0.8.2's `tab.list` gives each tab `number` (its position) and `focused`; `workspace.list` gives `number`, `focused` and `active_tab_id`. The socket method `agent.focus` takes `{"target": <pane id>}` (the same target `agent.read` takes) and brings the agent's workspace, tab and pane forward. `tab.focus` takes a tab id.

## Decision Log

- Decision: switching tabs in TeaNode only changes what is viewed; a separate "Show on computer" action focuses the tab in herdr. Rationale: the person chose it (2026-10-17), so that browsing from the phone never moves the terminal under someone at the keyboard. Date: 2026-10-17.
- Decision: a page of its own at `/coding` (selected session in the URL), linked from the mailbox rail, replacing the settings card's dialog. The card stays for the hooks setup and links rows to the page. Rationale: the person chose it (2026-10-17).
- Decision: "open a sibling tab" is the existing `open` action given a pane: the new tab goes into that pane's workspace, and the directory and coding agent default to that pane's. No new operation. Rationale: one name per idea; `open` already creates tabs.
- Decision: only tabs that hold a coding session appear in the tab strip. A shell tab has no transcript and nothing TeaNode can do with it.
- Decision: the new tool action is called `focus`, the operation `FocusAgentHerdrSession`, the command `focus`, matching herdr's own word and the naming of the existing actions.

## Outcomes & Retrospective

(To be written at completion.)

## Context and Orientation

Paths are relative to the repository root.

The program on each computer, `internal/computer/herdr.go`, does every herdr action. `RunHerdr` (near line 840) dispatches by action name (`herdr_list`, `herdr_read`, ..., `herdr_open`, `herdr_close`). It talks to herdr over a Unix socket with `internal/computer/herdr_client.go` (`call(ctx, method, params, result)`, `listWorkspaces`, `listTabs`, `listAgents`). `HerdrSession` (near line 109) is what one session looks like; `refresh` builds the list; `paneNames` (near line 743) computes the human name of each pane from the workspace and tab lists; `open` (near line 1115) creates a tab in the workspace whose label matches the directory's base name, or a new workspace, then starts the agent; `close` (near line 1238) closes the pane. Tests are in `internal/computer/herdr_test.go`, which fakes the socket.

The server side: `internal/agent/herdr.go` carries calls from the tool and GraphQL to the computer. The agent tool is `internal/agent/tools/computer/herdr.go`; `HerdrActions` lists every action with its tool name, GraphQL operation and CLI command, and a test fails when a surface lacks one. GraphQL is `internal/api/v1api/apigraph/agent_herdr.go` (interfaces `AgentHerdrQuery`, `AgentHerdrMutation`, argument structs, views, `herdrSessionView`). The Go client is `internal/client/agent_herdr.go`, the commands `internal/cmd/computer_herdr.go`. Documents used by the dashboard and the iPhone are checked against the schema by `internal/api/v1api/apigraph/client_documents_test.go` and `appdocuments_test.go`.

The dashboard's herdr code is `web/src/components/herdrSessions.tsx`: `HERDR_DOCUMENTS` (the GraphQL documents), `listHerdrSessions` (a shared cached list), `HerdrStateTag`, `HerdrQuestionAnswer` (answers a question), `HerdrSessionDialog` (turns and screen, send with attachments, watch, close), `HerdrOpenDialog`, `HerdrHooksDialog`, `HerdrSessionsCard` (shown by `web/src/pages/agent.tsx` on the Connections tab of `/settings/agent`) and `HerdrQuestionCard` (used by `web/src/components/agentDrawer.tsx`). Routes are in `web/src/app.tsx`; the mailbox rail rows are `MAILBOX_RAIL_ITEMS` and the no-mailbox group in `web/src/components/sidebar.tsx`. Strings live in `web/src/i18n/en.ts`, `zh.ts`, `ja.ts` under `herdr.*`. Design rules: `docs/coding/frontend-design.md`.

The iPhone app: sections of the side menu are `AppSection` in `ios/TeaNode/Sources/AppShell.swift`. Existing herdr support is the coding-question card in `ios/TeaNode/Sources/ConversationView.swift` and the `AnswerAgentHerdrQuestion` call in `ios/TeaNodeKit/Sources/TeaNodeKit/Operations.swift`. Demo data for screenshots is `ios/TeaNode/Sources/DemoServer.swift`, answering by operation name; CI shoots screens with lines in `.github/workflows/ios.yml` such as `shoot name appearance size -TeaNodeDemo -TeaNodeTab <section> -TeaNodeScreen <screen>`. Strings are in `ios/TeaNode/Resources/Localizable.xcstrings` with en, ja and zh-Hans. Design rules: `ios/DESIGN.md`.

## Plan of Work

### Milestone 1: server side

In `internal/computer/herdr_client.go`, add to `herdrWorkspace` the fields `Number int json:"number"`, `IsFocused bool json:"focused"`, `ActiveTabID string json:"active_tab_id"`, and to `herdrTab` `Number int json:"number"`, `IsFocused bool json:"focused"`. Add `focusAgent(ctx, paneId)` calling `agent.focus` with `{"target": paneId}`.

In `internal/computer/herdr.go`, add to `HerdrSession`, after `PaneName`:

    WorkspaceID     string `json:"workspaceId"`
    WorkspaceLabel  string `json:"workspaceLabel"`
    WorkspaceNumber int    `json:"workspaceNumber"`
    TabID           string `json:"tabId"`
    TabLabel        string `json:"tabLabel"`
    TabNumber       int    `json:"tabNumber"`
    // IsTabFocused says herdr shows this session's tab on the computer's
    // screen now: its tab is the active one of the focused workspace.
    IsTabFocused bool `json:"isTabFocused"`

Fill them where the names are computed (extend `paneNames` to return the placement too, or add a sibling helper that uses the same workspace and tab lists, so the socket is asked once per refresh). When the lists cannot be read, leave them empty, as names fall back to the pane id today.

Add `focus(ctx, arguments)` returning the refreshed `*HerdrSession`: find the session (`self.session(ctx, arguments.PaneID)`), call `focusAgent`, and return it re-read so `IsTabFocused` is true. Add `case "herdr_focus"` to `RunHerdr`.

Change `open`: when `arguments.PaneID` is given, it is the pane to open beside. Look it up; the workspace is that pane's workspace (`tab.create` with its `workspace_id`), the directory defaults to its `WorkingDirectory` when `Directory` is empty, and the coding agent kind defaults to its `CodingAgentKind` when empty. The label rule stays as for a new tab of an existing workspace. Refuse "open beside which pane? <id> is not a coding session here" when the pane is unknown. Without `PaneID`, behavior is unchanged.

In `internal/agent/herdr.go`, add the focus call the same way close is carried (follow `close` exactly), and pass the pane through for open.

In `internal/agent/tools/computer/herdr.go`: add `{ToolAction: "focus", Operation: "FocusAgentHerdrSession", Command: "focus"}` to `HerdrActions`; describe it in the tool description ("focus brings the session's tab forward in herdr on that computer, so whoever sits there sees it; only when the person asks"); say that `open` given `pane` opens beside it ("a new tab in that pane's workspace, in its directory with its coding agent unless directory and coding_agent say otherwise"); update the `pane` parameter text ("every action but list and setup needs it; open takes it to open beside that pane"); `focus` is `RiskWrite`; add a Preview line "Show pane X on Y". List results carry the new fields automatically if the tool passes the session through; check the tool's list text output and add the workspace and tab so the model can tell tabs apart.

In `internal/api/v1api/apigraph/agent_herdr.go`: add the seven fields to `AgentHerdrSessionView` and `herdrSessionView`; add `FocusAgentHerdrSession(ctx, FocusAgentHerdrSessionArguments{Computer, PaneID}) (*AgentHerdrSessionView, error)` to `AgentHerdrMutation` with a doc comment ("Bring a session's tab forward in herdr on its computer, so whoever sits there sees it. Needs agent:use."); make `OpenAgentHerdrSessionArguments.Directory` and `CodingAgentKind` nullable and add `PaneID string json:"paneId" graphapi:"nullable"` with a comment. Update the schema snapshot or registration the package uses (see how `CloseAgentHerdrSession` is registered in `schema.go` or `graph.go`).

In `internal/client/agent_herdr.go`: add the fields to the client's session struct, `FocusAgentHerdrSession`, and the pane argument to open. In `internal/cmd/computer_herdr.go`: add a `focus <pane>` command ("bring the session's tab forward in herdr on its computer") and `--pane PANE` on `open`, which makes `<directory>` optional and the agent kind optional (`open [claude|codex] [directory] --pane PANE`). `list` prints the workspace and tab grouping (a line per workspace, sessions under it ordered by tab number, the focused tab marked "on screen").

Tests: in `internal/computer/herdr_test.go`, cases for the placement fields from a faked workspace/tab list, for `focus` sending `agent.focus` with the pane, and for `open` beside a pane creating a tab in that pane's workspace with its directory and agent. Keep the parity tests passing (`go test ./internal/agent/tools/... ./internal/cmd/... ./internal/api/...`). Update `docs/reference/command-line.md` (the herdr rows) and `docs/subsystems/devices.md` ("Herdr").

Acceptance: `go build ./... && go test ./internal/computer/ ./internal/agent/... ./internal/api/v1api/apigraph/ ./internal/cmd/ ./internal/client/` pass; `make lint-ci` passes.

### Milestone 2: dashboard

Add `web/src/pages/coding.tsx`, routed at `/coding` in `web/src/app.tsx` (lazy, like `RecordingsPage`), with the selected session in the query string: `/coding?computer=<name>&pane=<paneId>`. Add a rail row "Coding" (`nav.coding`) to `MAILBOX_RAIL_ITEMS` and the no-mailbox group in `sidebar.tsx`, shown with `agent:use` when the herdr list names at least one computer (a small presence hook over `listHerdrSessions`, cached like the finance presence), add `/coding` to the `inMailbox` paths, and a `navigationShortcuts.tsx` entry if the neighbors have one.

Layout, desktop: the page's list on the left (sessions grouped by computer, then workspace in workspace number order; each row shows the tab label or agent name, the state tag, and a question marker), the selected session on the right. On the right, top to bottom: a header (workspace label, computer, directory) with actions "Show on computer", "Watch" (when not watched) and "Close tab" (danger, disabled while working, confirms as the dialog does); a tab strip (`Tabs` component or the same visual) of the workspace's coding sessions ordered by tab number, with the focused one marked by a small dot labeled "On screen" for screen readers, and a trailing "+" icon button "New tab" that opens a small menu (Claude Code / Codex, defaulting to the current session's agent) and calls `OpenAgentHerdrSession` with `paneId` of the current session, then selects the new session; a Turns / Screen segmented control; the question answer (`HerdrQuestionAnswer`) when the session asks; the transcript; and the composer at the bottom (textarea that grows, attach button, send; Cmd/Ctrl+Enter sends; disabled with the existing words while asking). Phone width: the list is the page; selecting a session shows the session view full width with a back control to the list (the URL drives it).

Transcript: `ReadAgentHerdrSession` with `turnCount` 50, refreshed every 3 seconds while the session is working or asking and every 15 seconds while idle, only when the tab is visible; keep the scroll at the bottom when it was at the bottom; render person turns as right-aligned bubbles and agent turns as text (reuse the conversation page's message styling if a component exists; tool turns as one muted mono line). A "Show earlier turns" control raises `turnCount` to 100 (the server's maximum). Screen view: `ReadAgentHerdrScreen` in a `pre`, refreshed the same way.

Move the reusable pieces out of `HerdrSessionDialog` rather than duplicating them: the send form with attachments and the turns list become components used by the page; delete `HerdrSessionDialog` and make `HerdrSessionsCard` rows link to `/coding?computer=..&pane=..`. Keep `HerdrOpenDialog` (the card's "Open a session" stays, and the page offers it too when nothing is selected) and `HerdrHooksDialog`. Update `herdrSessions.test.tsx` and add `coding.test.tsx` for: grouping and ordering, tab strip switching changes the URL and the transcript query, "New tab" calls open with the pane, "Show on computer" calls focus, close confirms and calls close.

Strings: every new string in en, zh and ja under `herdr.*` and `nav.coding`. Reuse existing `herdr.*` words (send, sendPlaceholder, closeSession, watch...).

Acceptance: `cd web && npx tsc --noEmit && npx vitest run src/components/herdrSessions.test.tsx src/pages/coding.test.tsx` pass; `npm run build` succeeds.

### Milestone 3: iPhone

TeaNodeKit: add `ios/TeaNodeKit/Sources/TeaNodeKit/Herdr.swift` with `HerdrSession` (all view fields including the seven new ones and the question), `HerdrQuestion`, `HerdrQuestionOption`, `HerdrTurn`, `HerdrList` (computerNames, failedComputerNames, sessions), and calls on the client like the others: `herdrSessions(computer:)`, `readHerdrSession(computer:paneId:turnCount:)`, `readHerdrScreen(computer:paneId:)`, `sendHerdrSession(computer:paneId:text:attachmentIds:)`, `focusHerdrSession`, `watchHerdrSession`, `openHerdrSession(computer:paneId:codingAgentKind:)`, `closeHerdrSession`. Reuse the existing question types in `Operations.swift` if they already model the question; do not define the same thing twice. Tests in `ios/TeaNodeKit/Tests/TeaNodeKitTests/HerdrTests.swift` decode each response and check each call's variables. The documents must pass `go test ./internal/api/v1api/apigraph/ -run AppDocuments`.

App: add `case coding` to `AppSection` (title "Coding", system image "chevron.left.forwardslash.chevron.right"), shown in the menu only when the herdr list names a computer (follow how the menu hides finance, if it does; otherwise filter in the menu). `ios/TeaNode/Sources/CodingViews.swift`: `CodingSessionsView` (a `List` grouped by computer and workspace, rows with the tab label or agent name, a state badge, a question badge; pull to refresh; a toolbar "+" to open a session in a directory, mirroring the dashboard's open dialog fields) pushing `CodingSessionView`. `CodingSessionView`: the navigation title is the workspace label, subtitle the computer; under the bar a horizontally scrolling tab strip of the workspace's coding sessions in tab number order (selected one highlighted, focused one with a dot), with a trailing "+" (New tab: a menu of Claude Code / Codex); a Turns / Screen segmented picker; the transcript as a scrolling list (person turns as bubbles on the right, agent turns as text, tool turns as one secondary monospaced line) that refreshes on the same timing as the dashboard while the view is visible and stays at the bottom; the question card reused from `ConversationView.swift` when asking; a bottom composer (multi-line `TextField` with axis vertical, attach button using the same attachment upload the conversation composer uses, send button), disabled while asking. Toolbar menu: Show on computer, Watch, Close tab (destructive, confirmation dialog, disabled while working). Follow `ios/DESIGN.md` and load the `ios-design` skill: system components, no capsule buttons inside lists.

Demo: `DemoServer.swift` answers `ListAgentHerdrSessions` (two computers invented, e.g. "laptop" and "desk"; a workspace "website" with three tabs, one working, one asking with two options, one idle; another workspace with one tab), `ReadAgentHerdrSession` (a short invented exchange with one tool line), `ReadAgentHerdrScreen`, and the mutations. Nothing real in the demo: invented names only. Demo screens `coding` (the list) and `coding-session` (a session with its tab strip and transcript), shot in `.github/workflows/ios.yml` as `coding-light`, `coding-session-light`, `coding-session-dark`, `coding-session-largest-text`.

Strings: every new string in `Localizable.xcstrings` with ja and zh-Hans, inserted as text in sorted position (never re-dump the file); reuse the dashboard's translations.

Acceptance: CI "App on macOS" and "TeaNodeKit on Linux" pass, and the four screenshots look right.

### Milestone 4: use it, look at it, review

Deploy the branch to the server and the new `teanode computer` build to gen7 (the computer program does the new actions, so it must be rebuilt and restarted there; see the devices doc). Then on the dashboard at desktop and 390px widths, light and dark: open `/coding`, select a session, switch tabs, send a prompt to a scratch session opened with "New tab", watch the reply arrive, use "Show on computer" on the tab already focused, close the scratch tab. Run the ux-review skill. Review the whole diff, fix findings, repeat until a round finds nothing. Check parity: list, read, screen, send, answer, watch, open, open beside, close, focus, on tool, GraphQL, CLI, dashboard and iPhone.

## Concrete Steps

From the repository root:

    go build ./... && go test ./internal/computer/ ./internal/agent/... ./internal/api/v1api/apigraph/ ./internal/cmd/ ./internal/client/
    make lint-ci
    cd web && npx tsc --noEmit && npx vitest run && npm run build

Expected: all pass. `make test` also runs database tests and needs Docker; the herdr code touches no tables.

## Validation and Acceptance

On a running server with gen7 attached: `teanode computer herdr list` shows sessions grouped by workspace with the on-screen tab marked; `teanode computer herdr focus <pane>` makes herdr show that tab; `teanode computer herdr open --pane <pane>` opens a new tab in the same workspace with the same agent; the dashboard and iPhone do the same from their buttons, and the agent does it when asked in a conversation ("open another Claude tab beside the teanode one").

## Idempotence and Recovery

No migrations. Every step can be repeated. A scratch session opened during testing is closed afterwards with close. If `agent.focus` turns out not to switch tabs in some herdr version, fall back to `tab.focus` with the session's tab id and record it under Surprises.

## Artifacts and Notes

(Filled in as work proceeds.)

## Interfaces and Dependencies

GraphQL, after this change:

    mutation FocusAgentHerdrSession(computer: String, paneId: String!): AgentHerdrSessionView
    mutation OpenAgentHerdrSession(computer: String, directory: String, codingAgentKind: String, agentName: String, shouldSkipPermissions: Boolean, paneId: String): AgentHerdrSessionView
    AgentHerdrSessionView gains workspaceId, workspaceLabel, workspaceNumber, tabId, tabLabel, tabNumber, isTabFocused

Agent tool `herdr`: new action `focus` (needs `pane`); `open` accepts `pane`. CLI: `teanode computer herdr focus <pane>`, `teanode computer herdr open [claude|codex] [directory] --pane PANE`.
