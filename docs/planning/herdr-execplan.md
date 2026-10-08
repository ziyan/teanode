# Working with the person in their herdr coding sessions

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

Many people who use coding agents keep several of them open at once, each in its own terminal: a Claude Code session working on one repository, a Codex session reviewing another. Herdr is a terminal workspace manager built for this. It holds those terminals in workspaces, tabs and panes, recognizes which coding agent runs in each pane, and makes all of it available to programs through a socket.

TeaNode cannot see any of this today. Its `claude_code` and `codex` tools (`internal/agent/tools/coding/coding.go`) start a new, headless run of a coding agent for each call. That run has no memory of the person's sessions, and the person never sees it.

After this change, TeaNode works in the same sessions the person works in.

- It can list them, read what was said and done in each, and look at the screen.
- It can type a new instruction into a pane, in front of the person, who can take over at any moment.
- It is told when a session finishes, and comes back to the conversation that sent the work.
- When a coding agent stops to ask a question or to ask approval, TeaNode shows it to the person wherever they are: as a card in the agent drawer with the options as buttons, and in their chat apps. Their answer is pressed into the pane for them. Whoever answers first wins: the person at the keyboard, or the person from their phone.

All of this is reachable in the same way from the language model's `herdr` tool, from the dashboard, and from `teanode computer herdr` on the command line.

To see it working, on a computer running `teanode computer` with herdr running and a Claude Code session open in a pane:

- `teanode computer herdr list` prints the sessions with their state.
- Ask that Claude Code session something that makes it ask a question back, then walk away from the keyboard. The drawer shows a card with the question and its options.
- Choose an option on the phone. The option is selected in the pane, and the session carries on.

## Progress

- [x] (2026-10-08) Researched herdr 0.8.2 on the development computer (command line, socket protocol, event subscription, detection rules), the history files of both coding agents, live question screens of both, Claude Code's documented hooks, and the TeaNode code this touches. Wrote this plan.
- [x] (2026-10-08) Revised after review by the person: the tool is named `herdr`; questions and approvals are shown to the person so they can answer remotely; herdr's own state is not trusted alone; strict parity across the tool, the dashboard and the command line.
- [x] (2026-10-08) Milestone 0: probed a headless `herdr --session teanode-probe server` with Claude Code and Codex panes; fixtures in `internal/computer/testdata/herdr/`; key sequences and hook payloads recorded below.
- [x] (2026-10-08) Milestone 1: the computer program (`internal/computer/herdr*.go`), with tests against a fake herdr socket. A read-only run against the real herdr recognized both questions the person had left waiting, including the Claude Code one herdr called idle.
- [x] (2026-10-08) Milestone 2: list, read, screen, send, wait, answer, watch and setup on the tool, the API, the dashboard and the command line; `TestHerdrParity` checks all four.
- [x] (2026-10-08) Milestone 3: questions written into the main conversation under `[herdr question]` (drawer card with buttons, chat relay); watches wake the conversation under `[herdr session]`.
- [x] (2026-10-08) Milestone 4: `setup` puts reporting hooks into Claude Code's settings; Codex needs none.
- [x] (2026-10-08) Milestone 5: decision record and docs; three review passes, each fixed; deployed to the server with the computer program on the development computer; end-to-end check in a scratch herdr workspace (below).

## Surprises & Discoveries

- Observation: herdr maps each pane to the coding agent's own session identifier, which names the history file.
  Evidence: `herdr agent list` prints `"agent_session":{"agent":"claude","kind":"id","value":"<uuid>"}`. A Claude Code `<uuid>` is `~/.claude/projects/<folder>/<uuid>.jsonl`. A Codex `<uuid>` ends the file name `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl`.

- Observation: Claude Code's project folder is not always derived from the pane's working directory. A session started in a git worktree keeps the worktree's folder after the pane's directory changes back.
  Evidence: a pane whose working directory was the main checkout had its history under the folder for `<checkout>/.claude/worktrees/<name>`. The folder name is the directory with every character that is not a letter or digit replaced by `-`. Since the folder cannot be reliably predicted, look the file up by identifier (`~/.claude/projects/*/<uuid>.jsonl`).

- Observation: herdr's state is not reliable enough to act on by itself. The person reported this, and it was seen directly.
  Evidence: a Claude Code session showing a question form was reported `idle`. Herdr's rule for question forms looks for "Enter to select ... Esc to cancel" after the last horizontal line on the screen. Claude Code draws a titled horizontal line below that footer, so the rule never matches, and the window title's idle mark (rule `osc_title_idle`) decides instead. `herdr agent explain <pane>` prints the rule that decided, which is the first thing to run when a state looks wrong.

- Observation: the two agents record a pending question differently.
  Evidence:
  - Codex writes the question into its history file as soon as it asks. The record is a `response_item` of type `function_call`, named `request_user_input_async`, whose `arguments` are `{"questions":[{"title":"...","options":["...","...","..."]}]}`. It is followed by an output of `{"accepted":true}` and a `task_complete` event: the turn ends, and the question waits in the composer as "? 1 question · alt+↑ to answer". Herdr reported it `blocked` through the window title "[ ! ] Action Required".
  - Claude Code had not written its `AskUserQuestion` call to the history file while the question was on screen. The file ended in unrelated records. The screen showed everything:

        ←  ☐ Choice  ✔ Submit  →
        │ Which of these should I take on next?
        ❯ 1. [ ] First option
                 A description that wraps
                 onto a second line.
          2. [ ] Second option
                 Its description.
          3. [ ] Type something
             Submit
        ───────────────────────────────
          4. Chat about this
        Enter to select · ↑/↓ to navigate · Esc to cancel

    (The options are invented; the layout, markers and footer are as seen. `[ ]` boxes mean this question allows several answers.)

- Observation: Claude Code's documented hooks can report what herdr misses, without changing how the session behaves.
  Evidence: the hooks reference lists `UserPromptSubmit`, `Stop`, `PreToolUse` (whose input includes `tool_name` and `tool_input`, so `AskUserQuestion` arrives with its questions and options), `PermissionRequest` (fired when Claude Code is about to ask permission, with `tool_name` and `tool_input`), and `Notification` (with `notification_type` such as `permission_prompt`, sent only after a prompt has waited about six seconds). A hook that prints nothing and exits 0 changes nothing. Codex has a hooks file too (`~/.codex/hooks.json`, enabled by `[features] hooks = true` in `config.toml`), and each hook command must be trusted by hash in `config.toml`, which herdr's own install does.

- Observation: Claude Code has its own remote approval, "channels permission relay", with first-answer-wins between the terminal and the remote side.
  Evidence: documented in the channels reference. It covers tool approvals only, not `AskUserQuestion` or plan approval. It requires starting each session with a `--channels` flag naming a plugin, and it is a research preview. It does nothing for Codex.

- Observation: the history files are large, and most of each is not conversation.
  Evidence: one Claude Code session was 124 MB in 16,880 lines, of which 2,530 were tool results. The `read` action must read backwards from the end, never the whole file.

- Observation: the herdr socket works from outside a herdr pane. It is newline-delimited JSON: each request is `{"id","method","params"}`, and its answer carries the same `id`. A subscription to `pane.agent_status_changed` must name a pane. `pane.agent_detected`, `pane.exited` and `pane.closed` need none.

- Observation: another coding session switched the shared checkout's branch while this plan was being written.
  Evidence: a session in a different pane ran `git checkout` and a rebase in the same working tree; the untracked plan file survived. It is also an argument for this feature, since TeaNode could have seen it happen. In the meantime, implement this in a checkout no other session works in.

- Observation: in Claude Code, every form is answered by the option's number. A question that takes one answer is answered by the digit alone; one that takes several ticks an option per digit, then `right` moves to the next question or to a review ("Ready to submit your answers?", answered with `1`); "Type something" takes its digit, then the text (herdr's `pane.send_text`), then `enter`. Tool, edit and plan approvals take a digit. Codex approvals take a digit too.
  Evidence: tried each in the probe session.

- Observation: Codex 0.161 draws a `request_user_input_async` question as plain text, with no form and no "alt+↑ to answer"; 0.156 put it under "Queued follow-up inputs". Both write it to the history file, and the person's next message answers it.
  Evidence: the probe pane, and the person's own pane read without touching it.

- Observation: a coding agent's startup dialogs are forms too, and herdr reports them idle. Text sent to a Codex session at its "Update available" dialog chose "Update now" and ran `npm install -g @openai/codex` on the development computer.
  Evidence: the probe pane. This is why `send` reads the screen first and refuses while anything is asked.

- Observation: Claude Code's hooks fire as hoped. `PreToolUse` for `AskUserQuestion` carries `tool_input.questions[]` (question, header, options with label and description, multiSelect) before the form draws; `PermissionRequest` fires for it as well; `Notification` with `permission_prompt` follows; `PostToolUse` carries the answers; `Stop` ends the turn. Every input has `session_id`, and `HERDR_PANE_ID` is in the hook's environment.
  Evidence: a per-session `--settings` file in the probe, so the person's own settings were not touched.

- Observation: the secret check reads a file name ending in the shell suffix as a host name under that top-level domain.
  Evidence: `make lint-ci` failed on the hook script's name; the script is `teanode-herdr-hook`.

- Observation: herdr's socket spells a read's source `recent_unwrapped`, where its command line says `recent-unwrapped`, and a request it cannot read is refused with an empty `id`. A read given `lines` answers with no text at all, so the program reads the recent text whole and keeps its last lines itself.
  Evidence: the agent's `screen` with lines failed as "herdr did not answer agent.read: EOF" in the end-to-end test: the client waited for its own id and never saw the refusal.

## Decision Log

- Decision: TeaNode works only through the person's own herdr panes. It never starts a headless run (`claude -p`, `codex exec`, or a second process resuming the same session).
  Rationale: the person asked for TeaNode to work with them in herdr, together. A second process writes to the same history behind the person's back. Typing into the pane keeps one process, one history and one screen both of them see.
  Date/Author: 2026-10-08, the person.

- Decision: the tool is named `herdr`, and the name runs through every surface: the `herdr` tool, `teanode computer herdr ...`, GraphQL operations named `...AgentHerdr...`, the computer program's `herdr_*` actions and its `herdr` feature, and Go types `HerdrSession`, `HerdrQuestion`.
  Rationale: the person asked for it. The capability is defined by herdr: it reaches what herdr holds, and only when herdr runs.
  Date/Author: 2026-10-08, the person.

- Decision: questions and approvals from the coding agents are shown to the person, who answers them from anywhere. TeaNode's language model never answers one on its own.
  - The dashboard card and the command line answer directly: the person's tap or command is the answer.
  - The `herdr` tool's `answer` action is granting risk. It needs the person's confirmation like any granting call, so in a conversation or a chat app the model can carry the person's choice, but cannot make it.
  - A person who allows granting calls to run when they are not there allows this too, since that switch is theirs.
  Rationale: the person asked to approve remotely. An approval gives the coding agent permission to act as the person on their machine, which is what granting risk means here (`tools.RiskGranting`, which maps to `models.UnattendedRiskGranting`).
  Date/Author: 2026-10-08, the person and agent.

- Decision: an answer is accepted only if the same question is still on the screen. Each question carries a `questionFingerprint`: a hash of the question's text and options as read from the screen. Before pressing keys, the computer program reads the screen again and compares. If the question has gone or changed, the answer is refused with "this question was already answered or has changed".
  Rationale: the person may answer at the keyboard at any moment, and a late remote answer must not press keys into whatever comes next. This gives the same first-answer-wins rule Claude Code's own relay has.
  Date/Author: 2026-10-08, agent.

- Decision: the computer program decides each session's state itself, from evidence in this order:
  1. reports from TeaNode's own hooks (Milestone 4), when installed;
  2. the history file: Codex's `task_started`, `task_complete` and an unanswered `request_user_input_async`; Claude Code's last records;
  3. TeaNode's own recognition of a question or approval on the screen;
  4. herdr's `agent_status`.

  Herdr's status changes are used as a reason to look again, and as the last resort. The state TeaNode shows is its own, `herdrSessionState`, with the values `idle`, `working`, `asking` and `unknown`. A session is `asking` exactly when a recognized question is waiting.
  Rationale: herdr reads mostly the window title and the screen, and was wrong in the one live test. Acting on a wrong `idle` would type an instruction into a question form.
  Date/Author: 2026-10-08, the person and agent.

- Decision: questions are recognized from the screen for both agents, and taken from the history file when it has them (Codex).
  Rationale: the screen is the one place every question shows, for both agents, whether or not hooks are installed. The answer is pressed into that same screen, so the screen is what has to match.
  Date/Author: 2026-10-08, agent.

- Decision: the reporting hooks are installed only when the person asks (`setup` on every surface). They only report: they print nothing, exit 0, and never decide anything.
  Rationale: they edit the person's own `~/.claude/settings.json` and `~/.codex/hooks.json`, and Codex must trust each hook by hash. A hook that decided permissions would hold the dialog away from the person at the keyboard, and Claude Code does not document what happens to the local dialog while a deciding hook runs.
  Date/Author: 2026-10-08, agent.

- Decision: Claude Code's channels permission relay is not used.
  Rationale: it covers tool approvals but not questions or plan approval, needs every session started with a flag, is a research preview, and does nothing for Codex. Herdr covers both agents and every kind of question, in sessions that are already running.
  Date/Author: 2026-10-08, agent.

- Decision: every action is on all three surfaces with the same behavior. One table, `herdrActions` in `internal/agent/tools/computer/herdr.go`, names each action's tool action, GraphQL operation and command, and a test fails when any surface lacks one. The dashboard uses the same GraphQL documents as the command line.
  Rationale: the person asked for strict parity, and a list checked by a test does not drift the way a convention does.
  Date/Author: 2026-10-08, the person.

- Decision: the computer program talks to herdr over its Unix socket in Go, not by running `herdr`.
  Rationale: watching for state changes needs a long-lived subscription, which only the socket offers. The answers have the same JSON shape as the command line's output, so tests can stand up a fake herdr socket.
  Date/Author: 2026-10-08, agent.

- Decision: history files are read by a Go reader in the computer program that reads backwards from the end. The `jq` programs of the `claude-code` and `codex` source types are not used.
  Rationale: those types were chosen for filing whole conversations into memory, where a fix without a release matters most. A read here is interactive, needs only the last turns and needs the tool calls, which the source types drop. The reader skips any line it does not understand, and `screen` always works as a fallback.
  Date/Author: 2026-10-08, agent.

- Decision: `send` is write risk, like `claude_code` and `codex`. It is refused while the session is `asking` (answer the question first) and while it is `working`, unless `shouldQueue` is set. The text is typed as given, with nothing added to say TeaNode sent it.
  Rationale: a session started with permission prompts turned off does what it is told, which is the same power the existing coding tools have. The person sees the text arrive in the pane, and TeaNode's conversation records that it sent it.
  Date/Author: 2026-10-08, agent.

- Decision: only herdr's default socket, `~/.config/herdr/herdr.sock`, is used for now. The feature is announced by the computer program as the feature string `herdr`, with no protocol bump.
  Rationale: the server refuses any computer program whose `Protocol` differs from its own, so a bump would lock out programs not yet upgraded. Feature strings exist so that a new capability does not need one.
  Date/Author: 2026-10-08, agent.

- Decision: the program polls every pane every three seconds rather than subscribing to herdr's events.
  Rationale: herdr's state is not trusted, so the screen is read anyway, and a poll needs no per-pane subscription or reconnect logic. Fifteen panes take about 0.4 seconds.
  Date/Author: 2026-10-08, agent.

- Decision: Codex gets no hooks. Its history file records turns and questions as they happen.
  Rationale: each Codex hook must be trusted by its hash in `config.toml`, a second thing to keep right, for nothing the file does not already say.
  Date/Author: 2026-10-08, agent.

- Decision: the hooks append to one file, `~/.local/state/teanode/herdr-events.jsonl`, cut to its end past 4 MB, rather than a file per session.
  Rationale: the script then needs nothing that reads JSON, and the program reads one tail per look.
  Date/Author: 2026-10-08, agent.

- Decision: the tool's `answer` carries the chosen options' labels, and the program refuses an answer whose labels differ from the options on screen.
  Rationale: a card can only show the call's arguments; with the labels in them, what the person confirms is exactly what is pressed.
  Date/Author: 2026-10-08, agent.

- Decision: the logic lives in the program on the computer; the tool asks it directly, the API through the worker, and both get the same refusals. The person runs herdr on several computers, so every action names a computer, and list covers all of them.
  Rationale: the person asked for parity and for several computers.
  Date/Author: 2026-10-08, the person and agent.

- Decision: the dashboard's watch wakes the person's main conversation.
  Rationale: the dashboard has no conversation of its own to wake.
  Date/Author: 2026-10-08, agent.

- Decision: `answer` is write risk, as `send` is, so neither raises a confirmation card. This reverses the earlier decision that made it granting.
  Rationale: the person found a card for every answer too many and asked for sending and answering to go through. The model still answers only with the person's choice: the guidance says so, the call must carry the chosen options' labels, and the fingerprint must still be the question on screen.
  Date/Author: 2026-10-08, the person.

- Decision: watches are kept in a file beside the question appearances, and a watch read back after a restart counts as having seen its session work.
  Rationale: in a build led by the agent, a deploy by another session restarted the program while Codex reviewed, and the watch on it was lost: the review finished and nobody was woken. Deploys are frequent, so a watch has to outlive them.
  Date/Author: 2026-10-08, agent.

## Outcomes & Retrospective

All milestones are done. Checked end to end against the deployed server, in a scratch herdr workspace with a Claude Code session:

- The command line answered Claude Code's folder trust dialog (a form without numbers, answered with the arrows), typed an instruction, and listed the question it caused.
- The question reached the drawer as a card under the agent's words; a tap on a phone-width page answered it in the pane, and the card turned to answered.
- The agent's tool typed an instruction, waited, listed the question and answered it with the option the person named, with no confirmation card; and a watch woke its conversation with the session's last turns when a twenty-second task ended.
- The Herdr sessions card, the session dialog (its question, last turns and screen), the drawer card and the hooks dialog were checked at 1400, 820 and 390 pixels wide, in light and dark; the page never scrolls sideways, and the table and a terminal's screen scroll within their own boxes.
- Behavior, not only names, is the same on every surface: sending into a pane that asks, and answering with a stale fingerprint, were refused with the same words by the command line, the dashboard's own documents and the agent's tool, and nothing was typed.

What the live run found that the tests had not: the socket's spelling `recent_unwrapped` and its refusals without an id; that a read given `lines` answers with nothing; that check-in lines are hidden unless working notes are shown, so the card belongs under the agent's words; that a tab out of sight never made its first look; and that a restart told waiting questions again. Each is fixed and noted above.

Then the agent led a small build on its own: given one goal (a command-line tool with tests, built by a Claude Code session, reviewed by a Codex session, fixed, committed), it brought both sessions' folder trust prompts to the person, briefed the builder, read its work, briefed the reviewer, carried the one finding back, and had the builder commit, waking on each session's finish. The tool works and its six tests pass. Two faults it found are fixed: Codex's trust prompt wants enter after the number, and a deploy that restarted the program lost the watch on the review, so watches are now kept across restarts. The person nudged it once, for the watch that was lost.

Left for later: Codex's older "alt+↑ to answer" question is answered by typing the reply as the next message, which was not tried against that version.

## Context and Orientation

TeaNode is two programs built from this repository: the server, and `teanode computer`, which the person runs on their own machine as themselves. In this plan the second is called "the computer program". It is registered in `cmd/teanode/main.go`, its subcommands are in `internal/cmd/computer.go`, and `teanode computer start` runs it detached. It keeps a websocket open to the server at `/api/v1/agent/computer`.

The computer program's side is `internal/computer/computer.go`. Messages in both directions use one struct, `message`. The program opens with `hello`, which carries `Protocol` (currently 2) and a list of `Features` such as `FeatureBackground`. The server then sends `{"type":"act","id","action","args"}`. The method `handle()` dispatches on `action` with a `switch` and answers `{"type":"result","id","ok","data","error"}`. Each request runs in its own goroutine, and a panic becomes an error answer through `handleSafely`. To add an action:

- write an arguments struct, a result struct and a `RunX` function;
- add a `case` in `handle()`;
- append a feature string to the hello.

The server's side is `computerView` in `internal/api/v1api/apigraph/agent_computer.go`. It also handles messages the computer program sends unasked:

- `session` carries terminal output.
- `background` with event `ended` says a background command finished. The worker method `ComputerBackgroundEnded` in `internal/agent/background.go` then wakes the conversation that started the command. "Wake" means it starts a new agent turn in that conversation that reports the ending. The computer program sends the message again after every reconnect until the server answers `background_acknowledge`, so the message is not lost when the connection drops.

The batching, retries, budget checks and the limit on turns taken without the person are in `backgroundWake` and `tryWakeForBackground` in the same file.

The person is told about things through:

- alerts (`internal/agent/alert.go`, `deliverAlert`), which write a marked message into the main conversation and open the drawer;
- the "goal needs you" path (`internal/agent/goal_background.go`), which does the same for a goal that needs the person;
- chat apps: `internal/channel/relay.go` relays every turn that starts with one of `models.OwnTurnMarkers` (`internal/models/insight.go`) to the person's Telegram or Discord, and `internal/channel/channel.go` lets a reply such as "yes" answer a confirmation card.

There is no browser push notification in this code.

Tools are the functions TeaNode's language model calls. Each is a `tools.Tool` (`internal/agent/tools/tool.go`), registered with `tools.Register` from a package `init` and imported in `internal/agent/tools/all/all.go`. A tool has a `Family`, a `Risk` (or `RiskOf` per call), JSON-schema `Parameters`, `Guidance`, a `Preview` line for its confirmation card, and `Run`. Risk decides whether a card is needed:

- read and write calls need none unless the operator or the person listed the tool;
- granting, outward and destructive calls always need one.

A run with nobody present may make a call that needs a card only if the person allowed that risk for unattended runs (`Agent.IsAllowedUnattended` in `internal/models/agent.go`).

A tool reaches the attached computer through `computer.Of(run, name)` and `Ask(ctx, action, arguments, wait)`. The helper `carry` in `internal/agent/tools/computer/computer.go` calls it and marks the answer untrusted, since text from the person's files may try to steer the model. The model to copy is the `terminal` tool in `internal/agent/tools/computer/terminal.go`: one tool, an `action` enum, read and write actions, and a wait.

The API is one GraphQL endpoint whose schema is built by reflection over Go interfaces in `internal/api/v1api/apigraph/schema.go`. An operation is named after its Go method, and the method's doc comment becomes the schema description (`go generate ./internal/util/commentparse/`). Every resolver starts with `self.requireAgentPerson(ctx)`, and computer features also check `agent.FeatureAllowed(configuration, "computer")`. The command line calls the same operations through `internal/client` (`docs/decisions/20260818-the-cli-goes-through-the-api.md`).

The full example to copy, layer by layer, is background commands:

- computer program: the `background_*` cases in `internal/computer/computer.go`;
- worker: `internal/agent/background.go`;
- tool: the `shell` tool's background actions in `internal/agent/tools/computer/computer.go`;
- GraphQL: `internal/api/v1api/apigraph/agent_background.go`;
- client: `internal/client/agent_background.go`, whose `Document*` constants are also listed in `internal/api/v1api/apigraph/client_documents_test.go`;
- command: `internal/cmd/computer_background.go`;
- dashboard: `web/src/components/backgroundCommands.tsx`, on the agent page's Connections tab in `web/src/pages/agent.tsx`.

Herdr's terms, as used here:

- a pane is one terminal (identifier like `w1:p2`);
- a herdr session is the Claude Code or Codex process herdr recognized in a pane;
- herdr's own state for it is `idle`, `working`, `blocked`, `done` or `unknown`.

The socket methods used are:

- `agent.list` and `agent.get`;
- `agent.read`, the pane's text (source `recent_unwrapped` on the socket, `recent-unwrapped` on herdr's command line, joins wrapped lines; `visible` is the viewport);
- `agent.prompt`, which types text and Enter;
- `agent.send_keys`, named keys such as `1`, `enter`, `esc`, `up`, `down`, `tab`, `space`, `alt+up`;
- `agent.wait`;
- `events.subscribe`.

`herdr api schema --json` prints the full schema. `herdr agent explain <pane>` prints which detection rule decided the state.

Claude Code history files are JSON lines.

- `user` lines hold the person's text, or `tool_result` blocks, which are skipped.
- `assistant` lines hold `text`, `thinking` (skipped) and `tool_use` blocks (with `name` and `input`).
- An `attachment` line of type `queued_command` with `origin.kind` `human` is the person typing while the agent worked.
- Lines with `isMeta`, `isSidechain` or `isCompactSummary` set are not conversation.
- Text inside `<system-reminder>` or `<command-...>` tags was injected.

Codex history files are JSON lines too. `response_item` lines carry `message` (`role` `user`, `assistant` or `developer`), `function_call` and `custom_tool_call` items, and their outputs. `event_msg` lines carry `task_started` and `task_complete`. The prefixes of injected user context are listed in the `injected` filter of `internal/sources/testdata/registry/codex.md`; copy them.

## Plan of Work

### Milestone 0 (prototype): confirm the shapes and the keys

Nothing from this milestone is kept except what it teaches, which goes into Surprises & Discoveries and into the fixtures of Milestone 1.

Run a separate herdr server for the experiment, so that the person's own session is never touched:

    $ herdr --session teanode-probe
    (inside it, open one pane with Claude Code and one with Codex, in a scratch directory)

Its socket path is printed by `herdr session list`. In each pane, ask the agent to ask a question with options, then:

- a question that allows several answers;
- a question with free text ("Type something");
- for Claude Code, a plan approval (plan mode, then ask it to finish planning);
- a tool approval (start Claude Code without `--allow-dangerously-skip-permissions` and ask it to run a command).

For each, save `agent.read` with both `visible` and `recent-unwrapped` sources, and `herdr agent explain`, as fixture text under `internal/computer/testdata/herdr/`. Replace any real text in the fixtures with invented text, keeping the layout characters exactly. Then find, by trying, the shortest key sequence that answers each kind:

- choose option N of a single-answer question;
- tick options of a several-answer question and submit it;
- enter free text;
- approve or refuse a tool approval;
- approve or refuse a plan.

Write each sequence down here.

At the same time, install throwaway hooks by hand that append their standard input to a file. Use `PreToolUse` (matcher `AskUserQuestion|ExitPlanMode`), `PermissionRequest`, `Notification`, `UserPromptSubmit` and `Stop` for Claude Code, and the events Codex's hooks support. Confirm, while sessions run with permission prompts turned off as the person's do:

- whether `PreToolUse` fires for `AskUserQuestion` before the form appears, and with which `tool_input` shape;
- whether `PermissionRequest` fires at all;
- what Codex sends for `request_user_input_async`.

Remove the throwaway hooks afterwards and stop the probe session with `herdr session stop teanode-probe`.

Acceptance: the fixtures exist, and the key sequences and hook payloads are recorded in this plan.

### Milestone 1: the computer program side

At the end of this milestone, the computer program answers the new actions against a fake herdr, with tests and no server.

`internal/computer/herdr_client.go` is a small client for the herdr socket.

- Type `herdrClient` with a `socketPath`, and methods `listAgents`, `getAgent`, `readAgent`, `promptAgent`, `sendKeys`, `waitAgent` and `subscribe`.
- Each call dials, writes one request line with a fresh `id`, and reads until the line with that `id`.
- A JSON `error` becomes a Go error reading `<code>: <message>`. A missing socket becomes `errHerdrNotRunning` ("herdr is not running for this person on this computer").
- The socket path is `<Home>/.config/herdr/herdr.sock`, from the computer program's `Home` option, so tests use a temporary directory.
- Herdr's snake_case records decode into `herdrAgent`, which mirrors herdr's contract, and are converted to TeaNode's types before leaving the file.

`internal/computer/herdr_question.go` recognizes questions on a screen: `recognizeQuestion(kind string, screenText string) (*HerdrQuestion, bool)`. Write one recognizer per agent and per form, from the Milestone 0 fixtures.

- For Claude Code:
  - The form is the block after the line holding `│` (the question) and before the footer containing `Esc to cancel`.
  - Option lines match `^\s*❯?\s*(\d+)\.\s*(\[[ x✔]\]\s*)?(.+)$`, and the indented lines under an option are its description.
  - The tab bar (`←  ☐ ... ✔ Submit  →`) means several questions in one form.
  - "Type something" and "Chat about this" are kept as options of kind `freeText` and `chat`.
  - A tool approval is recognized by "Do you want to proceed?" with numbered Yes/No options.
- For Codex:
  - The waiting question is "? N question · alt+↑ to answer" in the composer.
  - Its text and options come from the newest `request_user_input_async` call in the history file that has no answer after it.
  - Approvals are recognized by "press enter to confirm or esc to cancel" (the herdr rule `live_strong_blocker`).

`questionFingerprint` is the SHA-256 of the kind, the question text and the option labels, in hex.

`internal/computer/herdr_state.go` keeps, per pane, the state the program believes, in a map guarded by a mutex. It updates the map from:

- herdr events (the program holds one subscription for `pane.agent_detected`, `pane.exited` and `pane.closed`, and one per pane for `pane.agent_status_changed`);
- a screen check on each herdr change, and every 5 seconds for any pane herdr calls `idle` or `done`, because that is where herdr misses questions;
- the history file's tail, on each change.

The rules of the Decision Log decide `herdrSessionState`. Each change to or from `asking` is queued as an event for the server (Milestone 3). The subscription reconnects with backoff when herdr restarts, and on reconnect every pane is checked again.

`internal/computer/herdr_transcript.go` finds and reads history files.

- Find Claude Code's file with `~/.claude/projects/*/<id>.jsonl`, and Codex's with `~/.codex/sessions/*/*/*/rollout-*-<id>.jsonl` (the newest, if several match).
- Check the identifier against `^[A-Za-z0-9-]+$` before it goes into a pattern.
- `readTranscriptTail(path, kind, turnCount)` reads backwards in chunks from 1 MB, doubling to at most 16 MB, until it has `turnCount` turns or reaches the start.
- A turn is the person's text, the assistant's visible text, or a tool call shown as its name and the first 200 characters of its input (for example `Bash: go test ./...`).
- Texts over 4,000 characters are cut with " [cut]". Lines it cannot decode are skipped.
- It also answers the Codex lifecycle and pending question described above.

`internal/computer/herdr.go` holds the actions and types:

    type HerdrSession struct {
        PaneID            string        `json:"paneId"`
        CodingAgentKind   string        `json:"codingAgentKind"`   // "claude" or "codex", as herdr names them
        CodingSessionID   string        `json:"codingSessionId"`   // the coding agent's own session identifier
        HerdrSessionState string        `json:"herdrSessionState"` // idle, working, asking, unknown
        HerdrAgentStatus  string        `json:"herdrAgentStatus"`  // what herdr itself said, for diagnosis
        PaneTitle         string        `json:"paneTitle"`
        WorkingDirectory  string        `json:"workingDirectory"`
        TranscriptPath    string        `json:"transcriptPath,omitempty"`
        Question          *HerdrQuestion `json:"question,omitempty"`
    }

    type HerdrQuestion struct {
        QuestionFingerprint string                `json:"questionFingerprint"`
        HerdrQuestionKind   string                `json:"herdrQuestionKind"` // question, toolApproval, planApproval
        QuestionText        string                `json:"questionText"`
        IsMultipleChoice    bool                  `json:"isMultipleChoice"`  // several options may be chosen
        Options             []HerdrQuestionOption `json:"options"`
        ScreenText          string                `json:"screenText"`        // the form as shown, for when recognition is partial
    }

    type HerdrQuestionOption struct {
        OptionNumber      int    `json:"optionNumber"`
        OptionLabel       string `json:"optionLabel"`
        OptionDescription string `json:"optionDescription,omitempty"`
        HerdrOptionKind   string `json:"herdrOptionKind"` // choice, freeText, chat
    }

The arguments struct, `HerdrArguments`, has `PaneID`, `TurnCount`, `LineCount`, `Text`, `ShouldQueue`, `WaitSeconds`, `QuestionFingerprint`, `OptionNumbers []int` and `FreeText`. The actions, dispatched from `handle()`, are:

- `herdr_list`: every session, with state and the waiting question if any.
- `herdr_read`: `{herdrSession, turns, isTruncated}`.
- `herdr_screen`: `{herdrSession, screenText}`.
- `herdr_send`:
  - refused while `asking` ("pane w1:p2 is asking a question; answer it first");
  - refused while `working` unless `ShouldQueue` is set;
  - otherwise `agent.prompt` without a wait.
- `herdr_wait`: polls the program's own state every second until it leaves `working`, or the seconds run out (`isTimedOut`). Herdr's `agent.wait` is not used, because it waits on herdr's state.
- `herdr_answer`:
  1. reads the screen again and recognizes the question;
  2. compares fingerprints, and refuses if they differ;
  3. presses the Milestone 0 key sequence for the chosen options or free text;
  4. reads the screen once more, and reports `isAnswerAccepted` when the question is gone.

Add `FeatureHerdr = "herdr"` beside `FeatureBackground` and append it to the hello.

Tests, in `internal/computer/herdr_test.go`, use a fake herdr socket server in `t.TempDir()` that answers from fixtures and records what it was sent. Name each test after the behavior, as the neighbors do. Cover:

- each Milestone 0 screen fixture is recognized with the right question, options and kind;
- a screen with no form is not;
- a Claude pane that herdr calls `idle` but whose screen shows a form is `asking`;
- a Codex pending question is read from the history file;
- `send` is refused while asking, and while working without `ShouldQueue`;
- `answer` with a stale fingerprint presses no keys;
- `answer` presses exactly the recorded sequence;
- a 20 MB history file is read without reading all of it (assert on bytes read through a counting reader);
- a missing socket gives the not-running error.

Proof: `go test ./internal/computer/ -run Herdr` passes.

### Milestone 2: list, read, screen, send, wait and answer on all three surfaces

At the end of this milestone, the person and the model can do everything in Milestone 1 from the drawer, the dashboard and the command line.

The worker is `internal/agent/herdr.go`. Its methods `HerdrSessions`, `ReadHerdrSession`, `ReadHerdrScreen`, `SendHerdrSession`, `WaitHerdrSession` and `AnswerHerdrQuestion` each find the computer, check `HasFeature("herdr")`, and call the matching action. When the feature is missing, they answer "the computer program on <name> is too old for herdr; update it and restart it".

`HasFeature(name string) bool` is added to the computer interface in `internal/agent/tools/computing.go`, generalizing the existing `HasBackground` in `internal/agent/computer.go`. The tool and the API call these worker methods, never the computer directly, so their behavior cannot drift apart.

The tool is `herdr`, in `internal/agent/tools/computer/herdr.go`.

- Actions: `list`, `read`, `screen`, `send`, `wait`, `answer`, and in later milestones `watch` and `setup`.
- Parameters are snake_case like the neighbors': `action`, `computer`, `pane`, `turns`, `lines`, `text`, `should_queue`, `seconds`, `question_fingerprint`, `options`, `free_text`.
- `RiskOf`:
  - read for `list`, `read`, `screen` and `wait`;
  - write for `send` and `setup`;
  - granting for `answer`.
- The `Preview` for `answer` shows the question and the chosen option, so the card the person confirms says exactly what will be pressed, for example: "Answer the Claude Code question in pane w1:p2 (~/src/project) 'Which approach?' with: 2. Keep the cache".
- The guidance tells the model:
  - list first;
  - read to know what a session is doing;
  - never type into a session that is asking: show the person the question and its options, and answer only with the option they chose;
  - say what you are about to send, since the person may be typing in the same pane.

The table `herdrActions` in the same file lists, for every action, the tool action, the GraphQL operation and the command name.

GraphQL is in `internal/api/v1api/apigraph/agent_herdr.go`:

- queries `ListAgentHerdrSessions`, `ReadAgentHerdrSession` and `ReadAgentHerdrScreen`;
- mutations `SendAgentHerdrSession`, `WaitAgentHerdrSession` and `AnswerAgentHerdrQuestion`.

Each takes `computer` (nullable) and camelCase fields named as in `HerdrArguments`. Add them to `Query` and `Mutation` in `schema.go`, and regenerate the comment file. `AnswerAgentHerdrQuestion` called from the dashboard or the command line is the person's own act, so it needs no card. It is recorded in the audit log like other actions the person takes on their devices.

The client is `internal/client/agent_herdr.go`, with `Document*` constants added to `client_documents_test.go`. The command is `internal/cmd/computer_herdr.go`: `teanode computer herdr list|read|screen|send|wait|answer`, with `--json`, `--computer`, `--turns`, `--lines`, `--queue`, `--seconds`, `--option` (repeatable) and `--text`. Mount it in `internal/cmd/computer.go` like `background`.

The dashboard card is `web/src/components/herdrSessions.tsx`, on the agent page's Connections tab beside the background commands card.

- It shows a table of sessions per computer: state, directory and title. On a phone the table keeps its columns and scrolls sideways.
- Choosing a row shows the last turns, the screen, and a send box. The send box is disabled with the reason while the session is asking or working.
- A waiting question shows its options as buttons, with a text box for free text, and they call `AnswerAgentHerdrQuestion`.
- Results and failures are toasts (`useToast()`), never text inside the card.
- Strings go in `web/src/i18n/en.ts`, `ja.ts` and `zh.ts` under `herdr.*`.
- Follow `docs/coding/frontend-design.md`.

The parity test is `TestHerdrParity` in `internal/cmd/computer_herdr_test.go`, modeled on `TestFinanceParityWithTheCommandLine` in `internal/cmd/finance_test.go`. It checks that:

- every entry of `herdrActions` is an action in the tool's enum;
- it is an operation in the GraphQL schema;
- it is a subcommand of `teanode computer herdr`;
- nothing exists on any surface that the table does not list.

A web test, `web/src/components/herdrSessions.test.tsx`, checks that the card's documents name the same operations.

Proof:

- `make test` and `cd web && npm run test` pass, including `TestHerdrParity`.
- Against a development server, with the computer program running inside the person's login:

      $ TEANODE_PROFILE=local ./build/teanode computer herdr list
      PANE    AGENT   STATE    DIRECTORY             TITLE
      w1:p1   codex   asking   ~/src/example-repo    Review the parser
      w1:p2   claude  idle     ~/src/other-repo      Add retry to the uploader

- `teanode computer herdr answer w1:p1 --option 2` selects the second option in that pane.
- `TEANODE_PROFILE=local` matters: the command line's default profile on the development computer points at the production server.

### Milestone 3: questions reach the person, and finished work comes back

At the end of this milestone, a question asked in any pane reaches the person in the drawer and in their chat apps, and work sent with `watch` wakes the conversation that sent it.

Questions:

- When the computer program's state for a pane becomes `asking`, it sends the unasked message `{"type":"herdr","event":"asking","herdrSession":{...}}` with the question inside. It sends `{"type":"herdr","event":"answered",...}` when the question goes, however it was answered.
- Both are sent again on reconnect until acknowledged (`herdr_acknowledge`), as background endings are.
- The server's new case in `computerView` calls `ComputerHerdrChanged` in `internal/agent/herdr.go`.
- For `asking`, it writes a message into the person's main conversation, starting with a new marker `models.HerdrQuestionMarker`. It is added to `OwnTurnMarkers` so chat apps relay it. Like the "goal needs you" path, it publishes on a new surface `herdr_question`, which opens the drawer.
- The drawer, `web/src/components/agentDrawer.tsx`, renders that message as a question card with the option buttons, calling `AnswerAgentHerdrQuestion`. When the `answered` event arrives, the card is marked answered and its buttons are disabled.
- Chat apps relay it as the question and numbered options. The person's reply goes to the agent as any reply does: the model calls `herdr answer`, and the confirmation card that follows is answered by the person's "yes", as chat apps already allow.
- A person who wants a shorter path can be given one later. The plan does not change chat parsing.
- Quiet hours and the alert rate limit (`internal/agent/alert_mute.go`) apply to these messages too.

Watching:

- The action `watch` (tool), `WatchAgentHerdrSession` (API) and `teanode computer herdr watch` (command) register, with the computer program, the run's origin (`tools.BackgroundOrigin`: agent, conversation, whether it can be woken) against a pane.
- When that pane next leaves `working`, the program sends `{"type":"herdr","event":"settled",...,"origin":{...}}`.
- The server wakes the conversation, through the batching in `backgroundWake` extended with a third list beside `endings` and `works`. The wake shows the last three turns and the state.
- A run with nothing to wake is refused, as `tools.CanLeaveRunning` already decides.
- Watches live in the computer program's memory and end when it restarts. The guidance says so.

Proof:

- Tests for the program's resend and acknowledgement.
- A test that one question produces one message even if it is sent twice.
- A test that `answered` closes the card.
- A test that a watch wakes once.
- On the development computer, with a question waiting in a pane: the card appears in the drawer; choosing an option on a phone browser selects it in the pane; and answering at the keyboard instead marks the card answered without pressing anything.

### Milestone 4: reporting hooks

At the end of this milestone, a session's state comes from the coding agent itself when the person has installed TeaNode's hooks.

The action `setup` (tool), `SetUpAgentHerdrHooks` (API) and `teanode computer herdr setup` (command) install, on the computer, a script `~/.local/share/teanode/teanode-herdr-hook`. The script reads the hook input and appends one line, with the event name, the session identifier, the pane (`HERDR_PANE_ID`) and the hook input, to `~/.local/state/teanode/herdr-events.jsonl`. It prints nothing and exits 0.

`setup` then registers the script beside herdr's own entries. Each entry carries a marker comment, so `setup` can find and replace its own entries and leave every other entry alone.

- In `~/.claude/settings.json`: `UserPromptSubmit`, `Stop`, `PreToolUse` with matcher `AskUserQuestion|ExitPlanMode`, `PermissionRequest` and `Notification`.
- In `~/.codex/hooks.json`: the events Milestone 0 found, with each command added to the `trusted_hash` entries in `~/.codex/config.toml` the way herdr's install does.

`setup --remove` takes them out again. Before writing either file, it keeps a copy beside it with the suffix `.before-teanode`.

The computer program watches that directory and gives these lines first place in deciding the state, as the Decision Log orders it. The files survive a restart of the computer program, so no report is lost while it is down. Lines older than a day are removed.

Proof:

- Tests write a settings file with herdr's entries and check that `setup` adds its own without touching herdr's, and that `--remove` restores the original.
- On the development computer, after `setup`, the question form that herdr reported `idle` is reported `asking` within a second, from a `PreToolUse` line.

### Milestone 5: record, document, deploy

- Write `docs/decisions/<date>-teanode-works-in-the-persons-herdr-sessions.md`. It records the person's three decisions: panes and never a second process; questions answered by the person from anywhere; and parity.
- Add a section "Herdr" to `docs/subsystems/devices.md`, beside "The attached terminal", with caveats: herdr's state can be wrong (`herdr agent explain`), and a watch does not survive a restart.
- Add the commands to the `teanode computer` table in `docs/reference/command-line.md`.
- Put the changelog entry, under Added, in the pull request description's Changelog block, not in `CHANGELOG.md`.
- Deploy the server, upgrade the computer program on the development computer, run `setup` there, and repeat the Milestone 2, 3 and 4 checks against the deployed server.

## Concrete Steps

All commands run from the repository root unless stated.

Check herdr first:

    $ herdr --version
    herdr 0.8.2
    $ herdr integration status | grep -E 'claude|codex'
    claude: current (v8) ...
    codex: current (v8) ...

If herdr is newer, compare `herdr api schema --json` with the methods named here, and record any difference in Surprises & Discoveries.

After each milestone:

    $ make format
    $ go test ./internal/computer/ ./internal/agent/... ./internal/cmd/ ./internal/api/...
    $ make lint-ci; echo "exit $?"
    $ (cd web && npm run test)

Run `make lint-ci` on its own and read its exit status, since a pipe through `tail` hides a failing check. `make test` starts a PostgreSQL container and needs Docker. It rewrites some files under `vendor/` with gofmt; restore them with `git checkout -- vendor` before committing, and stage files by name.

To run the computer program against a development server:

    $ make build
    $ TEANODE_PROFILE=local ./build/teanode computer daemon

## Validation and Acceptance

Accepted when, on a computer with herdr running one Claude Code and one Codex session:

- `list` shows both, and the state is right in the case herdr got wrong: a Claude Code question form shows `asking`.
- `read` shows the last turns, matching the pane, and the largest local history answers in under a second.
- `send` types into the pane in view of the person. It is refused while the session is asking, and while it is working unless queued.
- A question in either pane appears in the drawer within five seconds, as a card with the options as buttons, and is relayed to a connected chat app.
- Choosing an option from a phone answers it in the pane. Answering at the keyboard first leaves the card marked answered, and a late tap is refused without pressing a key.
- The same answer works from `teanode computer herdr answer` and from the model's `herdr` tool after the person confirms its card.
- `watch` wakes the conversation when the session finishes.
- `TestHerdrParity` passes, and fails if any one surface's action is removed.
- With a computer program from before this change, every surface answers the "too old" message.

## Idempotence and Recovery

Reads, `list`, `wait` and `setup` can be repeated safely. `setup` replaces its own entries and keeps a copy of each file it edits. `send` cannot be repeated safely: if it fails after the text may have been typed, the tool says so and tells the model to `screen` before trying again. `answer` is protected by its fingerprint, so repeating it after it was accepted is refused rather than pressing keys twice.

To roll back, revert the change and redeploy. On each computer, run `teanode computer herdr setup --remove`, or restore the `.before-teanode` copies.

## Artifacts and Notes

Herdr's own states, from its help: `idle` is ready for input and already seen in the herdr window. `done` is the same after work the person has not looked at. `blocked` is an approval or question on screen. `unknown` means herdr cannot classify the agent. Reading through the socket does not mark a pane seen, so TeaNode leaves the person's badges alone.

Herdr's `agent.prompt` with a wait answers `agent_prompt_stalled` when nothing changed within five seconds. This plan never asks it to wait.

## Interfaces and Dependencies

No new dependencies. The herdr client uses `net` (Unix sockets), `bufio`, `encoding/json` and `crypto/sha256` from the standard library.

At the end of Milestone 1, `internal/computer` exports `HerdrSession`, `HerdrQuestion`, `HerdrQuestionOption`, `HerdrArguments`, `FeatureHerdr` and `RunHerdr(ctx context.Context, options *Options, action string, arguments *HerdrArguments) (any, error)`. Use the existing options struct's real name.

At the end of Milestone 2:

- `internal/agent/tools/computing.go` has `HasFeature(name string) bool`;
- `internal/agent/herdr.go` has the worker methods named above;
- `internal/agent/tools/computer/herdr.go` has the `herdr` tool and `herdrActions`.

At the end of Milestone 3, `internal/agent/herdr.go` has `ComputerHerdrChanged`, and `internal/models/insight.go` has `HerdrQuestionMarker`.

Revision note (2026-10-08, implementation): recorded the probe's findings, the key sequences and the hook payloads, and the decisions the build made: polling, no Codex hooks, one events file, option labels in the tool's answer, logic on the computer, several computers.

Revision note (2026-10-08): renamed the tool from `coding_session` to `herdr`, at the person's request. Reversed "TeaNode never answers a question" into "the person answers questions from anywhere, through TeaNode". Replaced trust in herdr's state with TeaNode's own state, after a live test where herdr called a question form idle and the person reported the same. Added the prototype milestone, the hooks milestone and the parity table and test.
