# The person's own devices: a browser tab and a computer, attached to their agent

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It follows the ExecPlan format described in
`~/.claude/PLAN.md` (not checked in); `docs/planning/README.md` says how plans
are kept here.

## Purpose / Big Picture

A personal agent that can only read mail is half a helper. The other half of
a person's life is on their own devices: the browser tab they are signed
into, and the computer in front of them with its files and its shell. After
this change a person can hand both to their agent, on their own terms.

The browser extension that attaches a tab (`web/extension/`) signs in the
way the command line does: its options page has a "Sign in" button that
opens the dashboard's authorization page, the person presses Authorize
there, and a token comes back to the extension. It no longer depends on the
dashboard's session cookie in the same browser, so it works in a browser
profile that is not signed in to the dashboard, and the server it talks to
is whatever the person configured. What the agent may do in the tab is what
it could already do; what changes is how the tab proves who it belongs to.

A new command, `teanode computer`, runs a small program on the person's
computer that stays connected to the server. While it runs, the agent has
two more tools: `shell`, which runs a command on that computer, and
`filesystem`, which reads, writes, lists and searches its files. The person
starts it with `teanode computer start`, sees it with `teanode computer
status`, and stops it with `teanode computer stop`; `teanode computer
daemon` is the same program in the foreground, for a terminal or a service
manager. As with the tab, the computer is attached by the person, to their
own agent, and only a conversation with the person present may use it.

Observable outcome: with the daemon running, asking the agent in the drawer
"what is in my Downloads folder?" lists the person's files, and "how much
disk space is free?" runs `df` there and reports it; stopping the daemon
makes the agent say the computer is not attached.

## Progress

- [x] (2026-09-11 12:30Z) Milestone 1 — the extension signs in like the
      command line: `web/extension/options.*` sign in and sign out through
      `chrome.identity.launchWebAuthFlow`; `web/src/pages/cli.tsx` takes a
      `redirect` to an extension's own address and hands the token over in
      the fragment; `tabView` verifies a token in the hello through the
      authenticator (`usernameOfToken`, `agentOfPerson`), protocol 2.
      Checked on the dev server: a good token gets `welcome`, a bad one
      "the token is not one this server takes; sign in again", protocol 1
      "this server speaks protocol 2; update the extension"; the page
      opened with a chromiumapp.org redirect navigates there with
      `#state=…&token=…&tokenId=…&username=…` and the request carries no
      fragment. Not yet done with a real extension loaded in Chrome (no
      interactive Chrome here); the owner's check.
- [x] (2026-09-11 13:40Z) Milestone 2 — the computer relay and its tools:
      `internal/agent/device.go` (the relay both devices share; `tab.go`
      is a thin layer over it), `internal/agent/computer.go`, the websocket
      at `/api/v1/agent/computer` and `ReadAgentComputer` in
      `apigraph/agent_computer.go`, the rule in `internal/computer/policy.go`
      (`Classify`), tools `shell` and `filesystem` in
      `internal/agent/tools/computer/` with the `<computer>` overlay, the
      `tools.Computing` face on `AskRun`, the `computer` family dropped
      from the catalog when the feature is off, the operator's `computer`
      feature switch through config, API, dashboard and `docs/
      configuration.md`, the drawer's "Your computer is attached" line,
      `client.ReadAgentComputer`. Tests: `computer_test.go` (relay),
      `tools/computer/computer_test.go` (tools, risks, refusals, overlay),
      `internal/computer/policy_test.go` (the rule). Later the same day:
      a `grep` action (lines matching a regular expression under a
      directory, binaries and tool-made directories skipped).
- [x] (2026-09-11 14:45Z) Milestone 3 — `teanode computer
      daemon|start|status|stop` in `internal/cmd/computer.go` (with
      `computer_unix.go` and `computer_windows.go` for detaching, signalling
      and liveness), the program's side in `internal/computer/computer.go`
      (`Serve`, `RunShell`, `RunFilesystem`, paths kept inside the root, ~
      meaning the root, a command's whole process group ended when its
      time is up), `PathAgentComputer` public to the hello like the tab's,
      the computer's tools loaded from the start of a turn while one is
      attached, `client.Token()`. Checked on the dev server: `start` prints
      the pid and the log, `status` says "the server sees devbox
      (linux/amd64) since …", `teanode agent ask --new` listed the
      Documents folder, read notes.txt ("regatta on the 21st") and ran
      `uname -s` (Linux) through the daemon, `stop` ended it and `status`
      then said "not running here". Two things found on the way and
      fixed: a memory added without a title was refused and the model
      answered with the same call until the round cap (a title is now
      taken from the content), and a turn stuck on one failing call now
      stops after three tries.
- [x] (2026-09-11 16:20Z) Milestone 5 — the extension opens the
      dashboard's own drawer on any page, and the agent opens and controls
      tabs. The extension is built by webpack into `web/extension/dist`
      (`web/webpack.extension.config.js`, part of `npm run build`), with
      the dashboard's tokens moved to `web/src/tokens.css` and shared by
      the dashboard, the artifact look and the options page. `/drawer` is
      the drawer as a page (`app.tsx`, `AgentDrawer standalone`), framed
      by anybody (`frame-ancestors *` for that path in `middlewares.go`)
      and signed in only by the token the framing page posts to it
      (`api.ts` `signInWithToken`, the websocket's `connection_init`
      carrying `Authorization`, verified in `websocket.go`). The
      extension's `panel.js` builds a bar (attach, the drawer's own close)
      and that frame, or on the dashboard's origin raises `teanode:agent`,
      which the built-in drawer answers. The tab actions `open`, `tabs`,
      `switch`, `close` in `background.js`, the agent's tabs grouped under
      "TeaNode" per window, never the person's own tab closed; the
      browser tool and its overlay say so. The drawer shows what is
      attached as marks in its head with the names on hover; several
      computers attach at once, by name. Checked in Chrome for Testing:
      the panel on a foreign page shows the signed-in primary conversation;
      on the dashboard the built-in drawer opens and no copy is made; the
      agent opened a grouped tab, listed both, switched back, closed the
      one it opened and listed one again. An https page cannot frame an
      http server (mixed content), so on dev the foreign page was a second
      origin of the dev server; production is https.
- [x] (2026-09-11 14:50Z) Milestone 4 — docs: `docs/reference/
      command-line.md` (teanode computer), `web/extension/README.md`
      (signing in), `docs/configuration.md` (the feature), `docs/decisions/
      20260911-the-computer-is-a-device-the-person-runs.md` and the
      decisions index, `docs/reference/project-structure.md` and
      `AGENTS.md` (`internal/computer/`), the changelog. `make lint-ci` and
      `make test` run at the end of the session. Not done here: the
      extension loaded unpacked in a real Chrome (no interactive Chrome in
      this environment).

## Surprises & Discoveries

- Observation: Google Chrome 137 and later ignores `--load-extension`, so
  the branded Chrome on the development machine could not load the
  extension unpacked from a script; Chrome for Testing (the build
  Puppeteer installs) still does, and the extension loaded there with no
  errors, attached a tab with its token, and the agent read the page and
  clicked a link through it.
  Evidence: `chrome://extensions` listed nothing under branded Chrome
  151 with the flag; under Chrome for Testing 153 it listed "TeaNode
  agent tab", and the dev server logged `ziyan attached their browser tab
  "Example Domain"`.
- Observation: the browser tool refused the attached tab when the server
  had no headless browser configured ("the browser is off on this
  server"), and the browser family was dropped from the catalog for the
  same reason, so a person with only the extension had nothing.
  Evidence: the transcript of the first attached-tab ask on dev, three
  refusals then "stopped: the same call failed three times". Fixed: the
  headless check applies only to targets other than the tab, and the
  family stays while a tab is attached.
- Observation: a small model answered the tools' first refusals — a
  memory without a title, a path outside the allowed directory — by
  repeating the same call until the round cap.
  Evidence: `agent conversation show` after the first daemon runs. Fixed
  by naming the memory from its content, by letting ~ mean the allowed
  directory with an error that says so, and by stopping a turn after the
  same call fails three times.

- Observation: the tab websocket authenticated by the dashboard's session
  cookie plus a CSRF token the extension read with `chrome.cookies`, so the
  extension only worked in a browser profile signed in to the dashboard.
  Evidence: `internal/api/v1api/apigraph/agent_tab.go`, `tabView`, and the
  `hello` message in `web/extension/background.js`.

- Observation: while the extension carried a click that waited for a
  page to load, one `teanode agent ask` poll took longer than the
  client's minute and the command reported "cannot reach"; the turn went
  on and finished on the server, and the transcript has the answer. The
  server caps a poll at 25 seconds, so something else held that response;
  it is the long poll inside the request's transaction that the code
  review left open (`docs/planning/done/20260910-personal-agents.md`,
  retrospective), and it is still open.
  Evidence: `client: cannot reach http://127.0.0.1:10081 … Client.Timeout
  exceeded while awaiting headers` at 09:00 on 2026-09-11, then `agent
  conversation show` with the finished turn.

- Observation: three reviews of the branch (code, front-end, security)
  found the framed drawer could be signed in by the page around it — the
  page is the frame's parent as much as the extension's script is, so a
  posted token could be anybody's; the relay gave up on a command after
  a minute while the daemon ran it for ten; a token given as a flag was
  copied onto the daemon's command line; the extension's worker died
  after thirty idle seconds with the badge still saying on; the decision
  record promised a card a compromised server would never show; an
  artifact could leave its sandbox by navigating itself; and the
  filesystem tool wrote where the shell's rule would have asked.
  Evidence: the three review reports of 2026-09-11, in the session.
  Fixed: the token travels in the frame's address fragment and the frame
  tells the panel whom it signed in as, so a swapped frame is caught;
  the relay waits the command's own timeout and a margin, and the daemon
  answers "busy" rather than blocking; the token reaches the daemon by
  its environment; the extension pings every twenty seconds and the
  socket answers; a socket that has not said who it is gets fifteen
  seconds; the record says what is true; a page that would leave is
  refused when made; moving asks, and so does a write into what the
  machine runs on its own; the compaction note is marked as data.

## Decision Log

- Decision: the extension gets its token through the same `/cli` page the
  command line uses, with the page redirecting to the extension's own
  callback address (`https://<extension id>.chromiumapp.org/`) instead of
  posting to a loopback port.
  Rationale: one authorization page, one token kind, one place that
  explains what is being handed over; the browser's web-auth flow gives
  extensions exactly such a callback address, and only the extension that
  opened the flow receives it.
  Date/Author: 2026-09-11, Claude with the owner.
- Decision: a device says who it is in its first websocket message (the
  token in the `hello`), not in the address.
  Rationale: a browser's websocket cannot set a header; a token in the
  query string would be written into access logs and proxies. The server
  verifies the hello exactly as it verifies an `Authorization` header.
  Date/Author: 2026-09-11, Claude.
- Decision: the computer is a device like the tab — attached by the person,
  to their agent, used only with them present, with the refusals enforced
  on the device — and the two share one relay type on the server.
  Rationale: the tab relay already does the waiting and the answering; a
  computer is the same shape with different actions. The person's own
  machine holds more than a tab does, so the same rule applies: nothing
  unattended.
  Date/Author: 2026-09-11, Claude with the owner.
- Decision: `shell` asks first for what changes the machine or reaches
  out (removals, moves, sudo, package installs, git pushes, curl piped to
  a shell, kill, shutdown) and for the gravest shapes (a recursive removal
  of the root, formatting a disk, a fork bomb), each with its reason on
  the card, and runs the rest; `filesystem` reads freely, writes as a
  write, deletes as a destructive action. Nothing is refused and nothing
  is confined: the program acts as the person, anywhere on the machine.
  Rationale: the confirmation card is the person's say; a card for every
  `ls` would teach them to press yes without reading. The owner asked
  (2026-09-11) that nothing be restricted beyond that — it is their
  machine, and the agent is to have what a terminal of theirs has. An
  earlier version refused the gravest shapes and confined paths to a
  directory (`--root`); both are gone.
  Date/Author: 2026-09-11, Claude with the owner.

## Outcomes & Retrospective

Done 2026-09-11 in one session, uncommitted at the owner's request. The
extension signs in through the same page the command line uses and says
who it is in its hello; the computer is a second device beside the tab,
sharing one relay, with a rule over commands applied on both ends, and
the agent used it end to end on the dev server within a day of the ask.

What held up: the device relay was already the right shape — the tab's
code became a thin layer over it without a change in behaviour; putting
the rule in `internal/computer` let the tool and the program share it
without either importing the other's world.

What was learned: a small model answers a tool's refusal by repeating the
call, so a refusal should either do the sensible thing (name the memory)
or the turn should stop; a tool behind tool_search is a tool the model
may never reach for, so what the person attached is loaded outright; a
path the model writes with ~ means "where I am allowed", not the home
directory the program happens to run in.

Left open: a richer extension — driving the attached tab through the
browser's own debugging protocol so that clicks and typing are real
events rather than scripted ones, and an overlay in the page to talk to
the agent from there — is a plan of its own; the owner's check of the
extension in a real Chrome; a Windows try of `teanode computer`.

## Context and Orientation

This is a mail server written in Go with a React dashboard. A personal
agent (`internal/agent/`) answers a person in the dashboard's drawer and
runs tools (`internal/agent/tools/`, one package per tool, each registering
itself with `tools.Register`). A tool's `Risk` — read, write, destructive
or outward — decides whether a confirmation card is shown before it runs;
an operator can switch a tool off or make it always ask (`agent.tools` in
the configuration), and a person can ask to be asked first (their agent's
`Confirm` list). The operator's `agent.features` are switches per
capability (`internal/config/agent.go`, `AgentFeatures`, `FeatureOn`),
shown on the server's Agents tab (`web/src/pages/settings/agentSettings.tsx`,
the `features` query and list) and set through `UpdateAgentSettings` in
`internal/api/v1api/apigraph/settings_agent.go`; every configuration field
is documented in `docs/configuration.md`, which `make lint-ci` checks.

A **device** here is something on the person's side that keeps a
websocket open to the server so the agent can ask it to do things. The
first device is the attached browser tab: the extension in
`web/extension/` (a Chrome extension, "manifest version 3", with a
background script `background.js` and an options page) connects to
`/api/v1/agent/tab` (`internal/api/path.go`, `PathAgentTab`), served by
`tabView` in `internal/api/v1api/apigraph/agent_tab.go`. The relay on the
server is `internal/agent/tab.go`: `AttachTab`, `DetachTab`, `TabAnswered`,
and `attachedTab.Ask`, which sends a numbered request and waits for the
numbered answer. The `browser` tool (`internal/agent/tools/browser/`) with
`target: "tab"` calls `Ask` through the `tools.Browsing` face
(`internal/agent/tools/browsing.go`: `AttachedTab() Tab`, `TabsAllowed()`),
which `AskRun` implements in `internal/agent/ask.go`. The tool's `Overlay`
(`browserOverlay`) tells the model each round that a tab is attached, in a
`<tab>` block. Only an interactive run has a tab: `run.Headless()` runs —
schedules, triage, research — are refused.

The command line client is `internal/cmd/` (built into `build/teanode`),
with saved profiles in `~/.config/teanode/profiles.json`
(`internal/cmd/profile.go`) and the sign-in flow in
`internal/cmd/loopback.go` and `auth.go`: `teanode auth login` opens the
dashboard's `/cli` page (`web/src/pages/cli.tsx`) with a loopback port and
a nonce, the page mints a token with the `CreateToken` mutation and posts it
to the port. `internal/cmd/client.go` (`resolveTarget`) turns a profile or
`--url`/`--token` into a connection. The server verifies a bearer token in
`internal/web/session.go` (`authenticateBearer`), reached through the
`web.Authenticator` interface's `Authenticate(request)`.

The tools the model sees are described by `tools.Tool` (`internal/agent/
tools/tool.go`): `Name`, `Family`, `Risk`, `RiskOf` (a risk that depends
on the arguments), `Description`, `Parameters`, `Guidance`, `Run`,
`Overlay`, `Core`, and the optional faces a run may implement. A tool that
needs something only some runs have asks for it through such a face
(`tools.Browsing` is one) and refuses when it is absent.

## Plan of Work

### Milestone 1 — the extension signs in like the command line

`web/extension/manifest.json` gains the `identity` permission. The options
page (`options.html`, `options.js`) shows the server address, who the
extension is signed in as, and two buttons: Sign in and Sign out. Sign in
calls `chrome.identity.launchWebAuthFlow` with the dashboard's `/cli` page
and the parameters `state` (a random nonce), `name` (`extension`),
`redirect` (`chrome.identity.getRedirectURL('authorized')`, an address of
the form `https://<id>.chromiumapp.org/authorized`). When the flow ends,
the final address carries `#state=…&token=…&username=…`; the extension
checks the nonce and keeps the token and the username in
`chrome.storage.local`. Sign out forgets them (and, best effort, revokes
the token with the `RevokeToken` mutation).

`web/src/pages/cli.tsx` accepts `redirect` in place of `port`: a redirect
must match `^https://[a-z]{32}\.chromiumapp\.org/`; anything else is
refused as not opened by a command. After `CreateToken`, the page navigates
to the redirect with the state, token, token id and username in the
fragment (never the query, so a server never sees it). The page's text says
it is an extension asking, when it is.

`web/extension/background.js` sends the token in its `hello` message
instead of the CSRF cookie. `tabView` in `agent_tab.go` accepts either: a
`hello` with a `token` is verified by building a request with an
`Authorization: Bearer` header and asking the authenticator, and the
person is that token's; a `hello` without one falls back to the session
cookie and CSRF as before. The check moves before `AttachTab`, so the
socket is upgraded first and refused in-protocol. The `tabProtocol`
number rises to 2; an extension speaking 1 is told so.

Acceptance: load the extension unpacked in a Chrome profile that is not
signed in to the dashboard, open its options, enter the dev server's
address, press Sign in, authorize; the options page says "Signed in as
ziyan"; on any tab, press the extension's button; the badge says on, and
`teanode agent` in the drawer answers "what page am I looking at?" with
the tab's title.

### Milestone 2 — the computer relay and its tools

The relay in `internal/agent/tab.go` becomes generic: `internal/agent/
device.go` holds `deviceLink` (a connection, a title and an address or a
name, the pending answers by number, `Ask(ctx, action, args)`) and the
per-agent maps; `tab.go` keeps `AttachTab` and friends as thin wrappers,
and `computer.go` adds `AttachComputer(agentId, connection, name, system)`,
`DetachComputer`, `ComputerAnswered`, `computerFor`, `ComputerAttached`.
One device of each kind per agent; a second attach replaces the first.

`internal/api/path.go` adds `PathAgentComputer = Prefix + "/agent/computer"`;
`agent_computer.go` in `apigraph` serves it exactly like `tabView` with the
token-in-hello check from Milestone 1 (a computer has no session cookie),
gated by `agent.features.computer` and `agent:use`, and offers
`ReadAgentComputer` (attached, name, system, since) for the CLI and the
agent page. Messages over the wire are `hello` {protocol, token, name,
system}, `welcome`, `refused` {reason}, `act` {id, action, args}, `result`
{id, ok, data, error}, `ping`/`pong` every minute from the server so a
dead link is noticed.

`internal/agent/tools/computer/computer.go` registers two tools in a new
family `computer` (`tools.FamilyComputer`): `shell` (`command`,
`directory`, `timeout` seconds up to 600, `environment`) and `filesystem`
(`action` read|write|list|info|mkdir|delete|move|search with `path`,
`content`, `destination`, `pattern`, `offset`, `limit`, `recursive`). Both
reach the computer through a new face `tools.Computing` (`internal/agent/
tools/computing.go`: `AttachedComputer() Computer`, `ComputersAllowed()`),
implemented by `AskRun`. `shell`'s `RiskOf` classifies the command (see
the Decision Log): denied patterns return an error before anything is
sent; approval patterns are `RiskDestructive`; the rest `RiskWrite`.
`filesystem`'s `RiskOf` is read for read|list|info|search, write for
write|mkdir|move, destructive for delete. Results are capped at
`tools.ResultCharacters` and marked `Untrusted` (a file's content is not
the person's words). An `Overlay` writes `<computer>` with the name, the
system and what the tools reach, when one is attached; the `Description`
of each tool says what happens when none is.

The config gains `Features.Computer *bool` (default on, like the others),
`FeatureOn("computer")`, the settings API field, the dashboard's feature
row (`agentSettings.feature.computer` in `en`, `zh`, `ja`), and a line in
`docs/configuration.md`. `docs/planning` and `AGENTS.md`'s package list
mention `internal/computer/`.

Acceptance: a Go test in `internal/agent/` attaches a fake computer
connection, asks `shell` for `echo hi` through a run, answers on the fake,
and sees `hi` in the tool result; a run with nobody present gets "the
computer is not reached by a run with nobody present".

### Milestone 3 — `teanode computer`

`internal/computer/` is the daemon's side, independent of the CLI so it
can be tested: `Serve(ctx, connection, options)` reads `act` messages and
answers them; `runShell` runs `sh -c` (or `cmd /C` on Windows) with a
timeout, a working directory (the home directory by default) and extra
environment, and returns stdout, stderr, the exit code, whether each
stream was cut at 256 kB, and whether it timed out; the filesystem
actions mirror the tool's, refusing paths outside what the person allowed
(`--root`, the home directory by default) and files over 4 MB for a read
in one go. The refusals of the Decision Log are enforced here too
(`commandpolicy.go` with tests), whatever the server sent.

`internal/cmd/computer.go` adds the command group: `daemon` connects with
the active profile (or `--url`/`--token`), reconnects with a backoff that
grows to a minute, logs to stderr, and exits on SIGINT/SIGTERM; `start`
runs `daemon` detached (a new session, stdout and stderr to
`~/.config/teanode/computer.log`) and writes `~/.config/teanode/computer.pid`;
`status` reads the pid file, says whether that process is alive, and asks
the server (`ReadAgentComputer`) whether it sees the computer; `stop`
signals the process and removes the pid file. Windows gets `start`
without a new session and `stop` by killing the process (build-tagged
files, as `internal/cmd/loopback.go` has none of this today).

Acceptance: in the dev environment, `TEANODE_PROFILE=local build/teanode
computer start` prints "started, pid N"; `computer status` prints "running
(pid N); attached as <hostname> since <time>"; the drawer answers "what is
in my home directory?" with a listing; `computer stop` prints "stopped";
the drawer then says the computer is not attached.

### Milestone 4 — docs, changelog, checks

`docs/reference/command-line.md` gets a "teanode computer" section;
`web/extension/README.md` describes signing in; `docs/decisions/
20260911-the-computer-is-a-device.md` records the decision; `CHANGELOG.md`
under Unreleased. `make lint-ci`, `make test`, and the dev-server checks
above.

## Concrete Steps

All commands run in the repository root, `/home/ziyan/projects/ziyan/teanode`.

    make web                      # builds the dashboard, including the cli page
    go build ./... && go vet ./internal/agent/... ./internal/computer/... ./internal/cmd/...
    make lint-ci
    make test                     # starts a PostgreSQL container; needs Docker

To try the daemon against the dev server (see
`docs/reference/local-development.md` for `make dev` and `dev/.env`):

    set -a; . ./dev/.env; set +a
    TEANODE_PROFILE=local ./build/teanode computer start
    TEANODE_PROFILE=local ./build/teanode computer status
    TEANODE_PROFILE=local ./build/teanode agent ask "what is in my home directory?"
    TEANODE_PROFILE=local ./build/teanode computer stop

## Validation and Acceptance

Each milestone's acceptance is stated with it. Overall: `make test` passes
with the new tests (`internal/agent/computer_test.go`,
`internal/computer/*_test.go`, `internal/cmd/computer_test.go`), and the
end-to-end scenario in the Purpose holds on the dev server.

## Idempotence and Recovery

Every step is a file edit or a build; running them twice changes nothing.
`teanode computer start` when a daemon already runs says so and exits 0;
`stop` when none runs says so. A stale pid file (the process is gone) is
removed by `start` and `status`. The tab protocol change refuses an old
extension with a message naming the version rather than failing silently.

## Artifacts and Notes

To be filled as work proceeds.

## Interfaces and Dependencies

Server, package `internal/agent`:

    func (self *Agent) AttachComputer(agentId string, connection DeviceConnection, name, system string)
    func (self *Agent) DetachComputer(agentId string, connection DeviceConnection)
    func (self *Agent) ComputerAnswered(agentId string, id int64, ok bool, data json.RawMessage, failure string)
    func (self *Agent) ComputerAttached(agentId string) (attached bool, name, system string, since time.Time)

Tool kit, package `internal/agent/tools`:

    type Computer interface {
        Ask(ctx context.Context, action string, args any) (json.RawMessage, error)
        Name() string
        System() string
    }
    type Computing interface {
        AttachedComputer() Computer
        ComputersAllowed() bool
    }
    const FamilyComputer Family = "computer"

Daemon, package `internal/computer`:

    type Options struct { Root string; Shell string }
    func Serve(ctx context.Context, connection Connection, options *Options) error
    func Classify(command string) Decision   // deny, ask, allow with a reason

Command line, package `internal/cmd`:

    func NewComputerCommand() *cli.Command   // daemon, start, status, stop

Dependencies: `github.com/gorilla/websocket` (already vendored) for the
daemon's client side; no new modules.
