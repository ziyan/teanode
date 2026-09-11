# One tool, one package: the agent's tools as a registry of packages

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It follows `20260910-personal-agents.md`, whose
milestones 7 to 11 put every tool the agent has into five files of one
package. It is written to be followed by somebody who has only this file
and the working tree.

## Purpose / Big Picture

After this plan, every tool the agent can use lives in a package of its own
under `internal/agent/tools/` — `tools/datetime`, `tools/memory`,
`tools/mailread`, `tools/browser` — with its definition, its guidance, its
tests and nothing else. A package registers what it offers from its own
`init`, the catalog is whatever registered, and a tool reaches the
database, the person, the operations and the run it is part of through the
context it is called with, never through a type of the `agent` package. The
`agent` package keeps the loop, the prompt, the policy and the pipeline;
it imports the tool kit, not the tools. Adding a tool becomes adding a
directory.

Nothing the agent does changes. The catalog names, families, risk classes,
parameters, guidance and overlays are the same before and after, which the
golden prompt tests and the catalog schema test hold in place.

## Progress

- [x] (2026-09-11 13:30Z) Milestone 1 — the kit: `internal/agent/tools` (definition, risk,
  family, call, result, registry, context accessors), with the `agent`
  package building its catalog from the registry and putting what a tool
  needs into the context. `Call` lost its run; `tools.WithRun` /
  `RunFrom` carry it; `AskRun` implements `tools.Run`; the old families
  register as one factory from `agent`'s init and reach the run through
  `runOf(ctx)` until each moves. Same catalog, golden prompts unchanged.
- [x] (2026-09-11 15:00Z) Milestone 2 — the general family moved:
  `tools/datetime`, `tools/webfetch`, `tools/websearch`, `tools/toolsearch`,
  `tools/artifact` (artifact and chart), `tools/memory`, `tools/askuser`
  (ask_user and todo), `tools/schedule`. The time, text, cron and feature
  helpers they share went into the kit (`timeparse.go`, `text.go`,
  `cron.go`, `policy.go`); `tools.Run` grew `Storage`, `Recall`,
  `Recalled`, `Ask` and `Enqueue`. The question card and the recall list
  stay on `AskRun` as `Ask` and `Recall`; `memoryLines` and the worker's
  schedule code stay in `agent`. `tools_general.go`, `chart.go` and
  `askuser.go` are gone.
- [ ] Milestone 3 — the mailbox family moved: mailsearch, mailread,
  mailact, maildraft, mailsend, mailcomposehelp, folder, rule,
  mailboxsettings, contact, subscription, replyqueue.
- [ ] Milestone 4 — the operator families moved: domain, alias, credential,
  queue, mailaudit, report, user, group, role, auditlog, server, settings,
  account, token, session, apppassword, accessexplain.
- [ ] Milestone 5 — browser and connected servers moved: `tools/browser`
  (the tool and the attached-tab relay's tool face), `tools/mcp` (the
  adapter that makes tools of a connected server's tools).
- [ ] Milestone 6 — the old files gone, the docs and the project structure
  updated, `make test` and `make lint-ci` green, the same catalog listed by
  `teanode agent tools` before and after.

## Surprises & Discoveries

(none yet)

## Decision Log

- **Registration from `init`, through a factory.** A package calls
  `tools.Register(func() []*tools.Tool)` in `init`; the registry keeps the
  factories in registration order and builds the catalog on demand. A
  factory rather than a value, so a package that needs state per catalog
  (the connected-servers adapter) can make it fresh each time. The server
  binary and the tests import `internal/agent/tools/all`, which blank-imports
  every tool package; a tool package that is not imported is not offered,
  which is the one way a deployment could leave a family out at build time.
- **What a tool needs comes through the context.** `tools.WithRun(ctx,
  run)` puts a `tools.Run` — an interface the `agent` package implements —
  into the context, and a tool asks `tools.RunFrom(ctx)` for it. The
  interface carries what the old `*AskRun` field carried: the owner, the
  agent, the mailbox, the operations, the database, the configuration, the
  conversation, the loaded and offered tools, the event sink, whether
  anybody is present. One interface rather than a value per key, because
  the things a tool needs are one thing — the run it is part of — and a
  bag of keys would have to be assembled and checked in every tool.
- **One package per tool; one per subject where tools share a body.** The
  rule is one tool, one package. Where several tools are one subject with
  shared helpers and shared tests — the five domain tools, the six rule
  tools, users/groups/roles — one package registers them all, the way a
  package registers three memory tools in the reference this follows.
  Splitting those would copy helpers into five places and test them five
  times.
- **The kit is `internal/agent/tools`, not `internal/tools`.** The tools are
  the agent's; nothing else on the server calls them. Keeping them under
  `agent` keeps the import graph honest: `agent` imports `agent/tools`,
  each tool package imports `agent/tools`, and nothing imports `agent` from
  below.
- **The prompt overlay stays a function of the context.** A tool's
  `Overlay(ctx)` reads the run from the context; the `<pending>`, `<todo>`,
  `<tab>` overlays move with their tools.

## Outcomes & Retrospective

(filled in when the plan is done)

## Context and Orientation

Today `internal/agent/tool.go` defines `Tool` (name, family, risk,
parameters, permissions, core, headless, guidance, preview, risk-of, run,
overlay), `Call` (id, `*AskRun`, arguments, confirmed), `Result`, the
`Catalog` with `Offered` (permissions and the operator's policy), `Split`
(core and deferred) and `Search`. Tools are registered by
`registerGeneralTools`, `registerMemoryTools`, `registerConversationTools`,
`registerScheduleTools`, `registerMailboxTools`, `registerOperatorTools`
and `registerBrowserTools` in `tools_general.go`, `memory.go`,
`tools_mailbox.go`, `schedule.go`, `tools_operator.go`, `tools_browser.go`,
`tools_mcp.go`, `askuser.go`, `chart.go` and `tab.go` — 6,300 lines in one
package. A tool's `Run` receives `*Call` and reaches everything through
`call.Run`: `call.Run.settings.Operations` for the API as the person,
`call.Run.Owner()` for the person, `call.Run.agent.settings.Database` and
`.Configuration()` for the server, `call.Run.settings.Agent` and
`.Conversation`, `call.Run.offered` and `.loaded` for `tool_search`,
`call.Run.emit` for events, `call.Run.settings.Headless`.

`FullCatalog()` in `agent.go` builds the catalog for the worker and for the
operator's settings. `Offered`, `Split` and `Search` are used by the loop
(`ask.go`), by `ListAgentTools` and by research (`research.go`, which has
its own read-only set by name). Tests: `tool_test.go`, the golden prompts
in `prompts_test.go` (`testdata/prompts/`), `agentqueries_test.go` in
`apigraph` (every document a tool sends is validated against the schema),
and per-tool tests inside `ask_test.go`, `mcp_test.go`, `browser_test.go`,
`tab_test.go`, `schedule_*_test.go`, `chart_test.go`.

## Plan of Work

### Milestone 1 — the kit

`internal/agent/tools/tool.go`: `Tool`, `Family`, `Risk`, `Call` (id,
arguments, confirmed — no run), `Result`, `NeedsConfirmation`, `Offered`,
`Split`, `Search`, moved as they are with `*AskRun` replaced by the
context. `internal/agent/tools/registry.go`: `Register(factory)`,
`Catalog()` building from the factories in order, `Reset()` for tests.
`internal/agent/tools/context.go`: the `Run` interface and `WithRun` /
`RunFrom`; a tool that is called with no run in the context gets an error
naming the tool, never a nil dereference. `internal/agent/tools/schema.go`:
`object`, `stringProperty` and the other schema helpers, `decodeArguments`,
`jsonResult`, `textResult`, which every tool uses today.

The `agent` package: `AskRun` implements `tools.Run`; the loop calls
`tool.Run(tools.WithRun(ctx, run), call)`; `FullCatalog()` becomes
`tools.Catalog()`; `Catalog().Offered` and friends are called through the
kit. `internal/agent/tools/all/all.go` blank-imports nothing yet.
Everything builds; the old tool files still register the old way through a
shim that feeds the registry, so the catalog is unchanged. Tests green.

### Milestones 2 to 5 — the moves

One family at a time, one commit each. For each tool: a directory, its
`init` registering a factory, the tool's code moved with `call.Run.X`
rewritten to `tools.RunFrom(ctx).X()`, its tests moved beside it (a tool's
test builds the run it needs through a fake `tools.Run` in the kit's
`toolstest` package, so no test needs the database), `all.go` gaining the
import, the old registration losing the tool. After each family the golden
prompts, the schema test and `teanode agent tools` say the catalog is the
same.

### Milestone 6 — the end

The old files are gone; `docs/reference/project-structure.md` names the
kit and the layout; `AGENTS.md` says a tool is a directory; the plan's
Outcomes are written.

## Concrete Steps

1. Inventory every `call.Run.` and `run.` reach in the tool files (the
   Context section lists them) and give each a method on `tools.Run`.
2. Write the kit and its tests; wire the loop; keep the shim; commit.
3. Move the families in order, a commit each, running `go test
   ./internal/agent/...` and `go test ./internal/api/...` after each.
4. Remove the shim and the old files; run `make lint-ci` and `make test`;
   compare `teanode agent tools --json` against a copy taken before.
5. Update the documents; fill in Outcomes.

## Validation

- `go test ./internal/agent/...` after each move; the golden prompts must
  not change (no `-update`), because the guidance, the deferred list and
  the overlays are what the model sees.
- `agentqueries_test.go` still reads every document out of every tool
  package — the test walks `internal/agent/tools/**` instead of one
  package.
- `teanode agent tools --json` on the dev server before and after, diffed:
  the same names, families, risks and descriptions.
- One conversation on the dev server after the last move exercising a tool
  from each family.

## Idempotence

Every step is a move of code that either compiles or does not; a step half
done fails to build, and `git checkout` of the two paths involved returns
to the step before. The registry is rebuilt from the factories on every
`Catalog()`, so a package imported twice or a test that registers again
after `Reset()` is not a state to recover from.

## Interfaces

- `tools.Register(factory func() []*Tool)`; `tools.Catalog() *Catalog`.
- `tools.Run` (interface): `Owner() *models.User`, `Agent()
  *models.Agent`, `Mailbox() *models.Mailbox`, `Operations()
  Operations`, `Database() db.Database`, `Configuration()
  *config.Configuration`, `Conversation() *models.AgentConversation`,
  `Offered() []*Tool`, `Loaded() map[string]bool`, `Emit(Event)`,
  `Headless() bool`, `Surface() string`, and what the inventory adds.
- `tools.WithRun(ctx, run) context.Context`; `tools.RunFrom(ctx) (Run,
  error)`.
- `Tool.Run func(ctx context.Context, call *Call) (*Result, error)`;
  `Tool.Overlay func(ctx context.Context) string`.
