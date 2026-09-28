# Ideas and goals: what the agent offers to do, and what it is keeping track of

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It builds on `docs/planning/agent-speaks-first-execplan.md`, whose seventh milestone added the "tips" that this plan turns into ideas; that plan stays as it was written, as history.

## Purpose / Big Picture

A person uses a small part of what their agent can do, and mostly does not know what else it could do for them. Today the agent tells them one thing at a time, at most once a day, as a chat message it starts on its own. These messages are called tips in the code today. They come from a fixed list of eight dashboard features (connect a mailbox, make a schedule, set a goal and so on), are never grounded in the person's own mail or memory, are spent the moment they are said, and leave no record of whether they helped.

After this plan there is one thing, an idea, and the word tip is gone from the product. An idea is a concrete offer of work the agent can do, written as an offer ("I can compare this month's card charges against the last three and tell you what moved") with two or three lines on exactly what happens and where it stops to ask. An idea comes from one of two places. A catalog idea is written in this repository and is offered only when the person's agent has every tool it needs; the eight features of today are catalog ideas too, offered until the person uses the feature. A personal idea is found in the person's own mail, memory and calendar during the nightly dream, or proposed by the agent in conversation, and carries the evidence that prompted it ("a subscription you keep marking as spam is still billing you").

The person manages ideas in three places that do the same things with the same words: the Ideas tab of their agent's page, the command line (`teanode agent idea list|propose|start|done|dismiss|reopen`), and the agent itself through its `idea` tool ("what ideas do you have for me?", "that one's done", "stop suggesting that"). Starting an idea opens a new conversation with the idea's opening request typed in the reply box; nothing happens until the person sends it. The Ideas tab also keeps a history of every idea: when it was started and in which conversation, done, dismissed or expired.

Ideas lead to goals. A goal already exists: a conversation can carry a standing instruction the agent keeps working toward across turns of its own, with a state (`working`, `waiting`, `met`), a one-line note on where it stands, and when it looks again. Today a goal is visible only inside its own conversation in the chat drawer. After this plan a Goals tab lists every goal in progress with its note, beside the schedules and mail rules that run on their own; ticking a goal marks it met; "Set a goal" by area opens a conversation for the agent to propose one. The command line and the agent's `goal` tool gain the same listing and the same "met". An idea started in a conversation that then gets a goal is done when that goal is met.

To see it working: on a development server whose person has a mailbox granted to their agent, run `teanode agent dream now`, wait for it to finish, and open Settings, Your agent, Ideas. A "For you" section lists personal ideas, each saying what it was drawn from; below it, catalog ideas grouped by area. Tap one: the chat drawer opens on a new conversation with the request in the reply box. Send it; the idea shows as started in History with a link to the conversation. Run `teanode agent idea list` and see the same ideas and statuses. Ask the agent in the drawer to dismiss one of the ideas and see it move to dismissed on the tab.

## Progress

- [x] (2026-09-28) Mapped the tip code as it is and wrote the first version of this plan.
- [x] (2026-09-28) Revised the plan: one name (idea) everywhere, the same operations from the dashboard, the command line and the agent's tools, and fewer concepts (see Decision Log).
- [x] (2026-09-28) Milestone 1: rename tips to ideas end to end, with the idea stored as a row, the catalog as a file, and the four operations reachable from GraphQL, the `idea` tool and the command line. The tool sends the command line's own GraphQL documents (`internal/client/agent_idea.go`), which a test validates against the schema, so the two cannot differ even in wording.
- [x] (2026-09-28) Milestone 2: the Ideas tab, checked in light and dark at 390 and 1400 pixels on the server with headless Chrome; ideas sit two to a row from 900 pixels, and the mark that dismisses one is in its corner rather than a line of its own.
- [x] (2026-09-28) Milestone 3: the full catalog, thirty-three ideas beside the eight features, checked against the registered tools' own risks.
- [x] (2026-09-28) Milestone 4: personal ideas from the dream and from conversation, through one check, and ranking from what the person did with each category.
- [x] (2026-09-28) Milestone 5: goals listed and marked met from the Goals tab, the command line and the `goal` tool, with ideas following their goals. The idea following its goal is kept in `UpdateAgentConversation` in the database layer, the one place every goal is written, rather than in each caller.

## Surprises & Discoveries

- Observation: the speak-first plan said tips would favor "tools they used" and "a skill that is installed", but the code never reads either; the choice is one model call shown the person's last ten conversation titles.
  Evidence: `chooseTip` in `internal/agent/tips.go` renders `tip_choose.txt` with `Candidates`, `Lately` and `Given` only.
- Observation: a tip is recorded as given before the turn that says it runs, so a tip nobody read is spent forever.
  Evidence: `checkIn` in `internal/agent/tips.go` calls `tx.AddAgentTip` before returning the turn's instructions.
- Observation: goals can already be listed from the command line, by fetching every conversation and keeping the ones with a goal in the client.
  Evidence: `runAgentConversationGoal` in `internal/cmd/agent_chat.go` calls `client.SearchAgentConversations` (the `ListAgentConversations` operation) and filters on `Goal != ""` itself.
- Observation: the chat drawer already keeps an unsent draft per conversation in the browser, and any page can open the drawer on a conversation.
  Evidence: `draftKey` in `web/src/components/agentDrawer.tsx` (`teanode.agent.draft.<conversation id>`), and `openAgentConversation` in `web/src/api.ts`.

## Decision Log

- Decision: after a review of the finished branch, an idea's reason and evidence are capped and shown to the judging model, which also refuses an opening request asking for anything but the idea; the daily offer hands the idea's words to the turn as data; starting a started idea returns its conversation and a closed one must be reopened first; a goal taken back up after "met" starts its idea again; goals in progress are one query, archived conversations included; ideas are marked shown only when on screen.
  Rationale: the reviewer's must-fix findings. Mail anybody can send reaches the dream, so what the dream writes must not reach the agent as instructions or the person as a one-tap message that does something else. A goal still running in an archived conversation was invisible and could not be closed.
  Date/Author: 2026-09-28, after the review.

- Decision: the judging model is called inside the request's transaction, as `DraftReply` already does.
  Rationale: the reviewer asked for the model call to run with no transaction open. Every GraphQL request here runs in one transaction from the API layer, and moving a single call out of it is a change to that layer, not to ideas. The call is one short judgment; `DraftReply` has run the same way in production.
  Date/Author: 2026-09-28, after the review.

- Decision: the dream proposes ideas by calling the idea tool itself, rather than returning JSON for the pass to check and keep.
  Rationale: a call of the night already has the person's tools, and one that needs no approval runs unattended. Going through the tool means a dream's idea takes the same path as the agent's in a conversation and the person's on the command line, and the model sees a refusal's reason and can correct the one thing wrong.
  Date/Author: 2026-09-28, while implementing.

- Decision: a repeat is found by the share of words two headlines have in common (over 0.6 of the words of four letters or more), not by embeddings.
  Rationale: the check runs inside a request and for a handful of ideas; embedding each headline needs the embedding service up and adds a call for what word overlap already catches between two offers of one thing. The model's judgment is the check that needs a model.
  Date/Author: 2026-09-28, while implementing.

- Decision: the agent's `idea` tool sends the documents the command line's client defines, rather than its own.
  Rationale: found while building Milestone 1. `TestClientDocumentsMatchTheSchema` validates the client's documents against the schema; a tool's own documents are checked by nothing, and were a second copy of the same four operations.
  Date/Author: 2026-09-28, while implementing.

- Decision: one name. What is called a tip today is an idea from this plan on, in the code, the database, the GraphQL schema, the agent's tools, the command line, the dashboard, its three languages and the docs. Nothing new is called a tip, and no alias is kept.
  Rationale: the person asked for the same thing to have one name. Keeping `tips` as the switch and `ideas` as the list would leave two words for one thing, and every reader would wonder whether they differ. The one exception is history: migrations 0109 and 0112 and the speak-first plan are left as they were written, because a migration must never change after it has run and a plan records what was decided then.
  Date/Author: 2026-09-28, at the person's request.

- Decision: every operation on ideas and goals exists once, as a GraphQL operation, and the three ways in are thin: the dashboard calls it, the command line calls it through the client, and the agent's tool calls it through `operator.Execute`, the way `internal/agent/tools/agentprofile/agentprofile.go` already calls `UpdateAgent`. The verbs are the same in all three: list, propose, start, done, dismiss, reopen for ideas; list and met for goals.
  Rationale: the person asked for parity between the agent's tools, the web and the command line. Parity kept by hand drifts; parity by construction does not. Routing the tool through GraphQL also gives it the same permission checks and validation as the dashboard.
  Date/Author: 2026-09-28, at the person's request.

- Decision: two kinds of idea, `catalog` and `personal`, not three. The eight features of today are catalog entries that carry a `usedCheck`.
  Rationale: a setup idea differs from any other catalog idea only in when it stops being offered, which is one optional field, not a kind.
  Date/Author: 2026-09-28, simplifying the first version.

- Decision: five statuses, `open`, `started`, `done`, `dismissed`, `expired`. Being shown is a timestamp, `shown_at`, not a status.
  Rationale: an idea that was shown is still open; a status for it would make every query ask for two statuses to mean one thing.
  Date/Author: 2026-09-28, simplifying the first version.

- Decision: the model call that picked a tip goes away. The agent's unprompted message names the highest ranked open idea not yet shown.
  Rationale: the call existed to choose among eight generic features using conversation titles. Personal ideas are already judged relevant when the dream makes them, and ranking already orders them, so a second judgment adds a prompt and a failure mode and decides nothing new. The daily limit and the idle rule stay.
  Date/Author: 2026-09-28, simplifying the first version.

- Decision: no new operations for goals and no draft stored on the server. Listing goals is a `isGoalInProgress` argument on the existing `ListAgentConversations`; marking one met is a `goalState` argument on the existing `UpdateAgentConversation`; "Set a goal" is the existing `StartAgentConversation` followed by opening the drawer with a draft; starting an idea is `StartAgentIdea`, which returns the conversation and the opening request, and the dashboard puts the request in the reply box through `openAgentConversation(conversationId, draft)`.
  Rationale: each of these already exists, or is one argument away. A separate tracking operation, a goal record or a server-side draft would each be a second copy of something the code has.
  Date/Author: 2026-09-28, simplifying the first version.

- Decision: one check for every idea, whoever made it: the catalog test, the dream, the `idea` tool and `ProposeAgentIdea` all call `checkIdea`. It rejects an idea that needs a tool the person does not have, cites evidence that does not resolve, breaks the length and vocabulary rules, repeats an open or dismissed idea, or (for personal ideas, by one model call) promises more than its tools can do or leaves out where it asks first.
  Rationale: the ideas that read best in comparable products are often ones the agent could not carry out ("I'll haggle with the seller and arrange the pickup"). An offer the agent then fails at teaches the person that ideas are advertising. One check means the catalog, the dream and the agent cannot drift apart on what an honest idea is.
  Date/Author: 2026-09-28.

- Decision: starting an idea never acts. It opens a conversation with the request drafted for the person to send.
  Rationale: a personal idea is written by a model from mail the person may not have read, and some offers end in something outward. The person sending the first message keeps every idea inside the approval rules the conversation already has, and lets them correct a wrong premise first.
  Date/Author: 2026-09-28.

- Decision: the areas (categories) and their emoji are one list in Go, served with the ideas, used by both tabs. The dashboard keeps only their translated labels.
  Rationale: a second copy in TypeScript would drift. Emoji rather than pictures or the dashboard's line icons, because an emoji renders in the dashboard, in chat apps and in mail with no assets, and a fixed list keeps a model from choosing one that reads as a joke beside a serious idea.
  Date/Author: 2026-09-28.

- Decision: personal ideas are made in the nightly dream, and the agent may also propose one during a conversation.
  Rationale: the dream already reads the day's mail, the timeline and memory within a budget while nobody waits. A conversation sometimes turns up an idea too ("you do this every Monday; want me to take it over?"), and the tool lets the agent keep it rather than lose it when the conversation ends.
  Date/Author: 2026-09-28.

- Decision: the history of ideas is a section of the Ideas tab, called History; the goals list is the Goals tab. Neither is called tracking.
  Rationale: the person asked for a UI that tracks the ideas and pointed at a goals list. Using "tracking" for both would give two things one name, the mirror of the rule above.
  Date/Author: 2026-09-28.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

The server is Go under `internal/`, the dashboard is React under `web/src/`, the command line is `teanode`, built from `internal/cmd/`, which talks to the server only through its GraphQL API (`internal/client/`). The "agent" is a person's assistant; `internal/agent/` holds its loop, tools and background work, and `models.Agent` in `internal/models/agent.go` is its row in the `agent` table.

What exists today under the name tip. `internal/agent/tips.go` holds `tipCatalog`, eight `Tip` values (`TipKey`, `Feature`, `Where`, `IsUsed`), `tipsToGive`, `chooseTip` and `tipReason`; the prompt is `internal/agent/prompts/tip_choose.txt`; the tests are `internal/agent/tips_internal_test.go`. A tip is one of three reasons the agent starts a conversation on its own ("speak first", `internal/agent/speak_first.go`, constant `SpeakFirstTip = "tip"`, turn surface `speak_first:tip`): with the dashboard open and the person quiet for five minutes, at most once a day. Given tips are rows of `agent_tip` (migration `internal/db/migrations/0112_agent_tip.sql`; `models.AgentTip` in `internal/models/evaluation.go`; `AddAgentTip` and `ListAgentTips` in `internal/db/database_evaluation.go`). The switch is `agent.is_tips_enabled` (migration 0109; `IsTipsEnabled` in `internal/models/agent.go` and `internal/db/database_agent.go`; the GraphQL argument `isTipsEnabled` in `internal/api/v1api/apigraph/agent.go`; the checkbox in `web/src/pages/agent.tsx` with the strings `agent.tips`, `agent.tipsHint`, `agent.tipsOn`, `agent.tipsOff` in `web/src/i18n/en.ts`, `ja.ts`, `zh.ts`). The person turns them off by telling the agent, whose `agent_profile` tool (`internal/agent/tools/agentprofile/agentprofile.go`) has the actions `no_more_tips` and `tips_on`. The command `teanode agent speak-first --reason tip` (`internal/cmd/agent.go`, `internal/client/agent_graph.go`, `internal/api/v1api/apigraph/agent_speak_first.go`) forces one. Comments naming tips are in `internal/agent/presence.go`, `web/src/hooks/useAgentPresence.ts`, `web/src/components/agentDrawer.tsx`, `internal/models/insight.go`, `internal/models/agent.go` and `internal/db/database_agent.go`, and the end-to-end task `tips-01` is in `docs/evaluation/end-to-end-tasks.md`. Three other uses of the word are not this feature: the usage chart's hover box (`DayTip` and the `usage-chart-tip*` classes in `web/src/components/usageChart.tsx` and `web/src/style.css`), a comment about a git branch tip in `.github/workflows/secrets.yml`, and words in mail parsing code. The hover box is renamed to tooltip as well, so the word tip means nothing in the dashboard; the other two are left.

Tools. `DirectTools` in `internal/agent/direct.go` lists a person's tools; each `tools.Tool` (in `internal/agent/tools/tool.go`) has a `Name`, a `Description`, parameters and a `Risk`: `read`, `write`, `destructive`, `outward` (acts toward another person or service for them) or `granting`. Skills add tools named `skill__<skill>__<tool>`, connected servers add their own, so the list differs per person. A tool that mirrors a dashboard operation calls it with `operator.Execute(ctx, <graphql document>, variables)`, as `agentprofile.go` does.

Goals. A conversation row carries `goal`, `goal_state` (`working`, `waiting`, `met`, in `models.AgentGoalState`, `internal/models/insight.go`), `goal_note`, `goal_next_at` and `goal_set_at`. The `goal` tool (`internal/agent/tools/goal/goal.go`) acts on the conversation it runs in: `set` (only when the person asks), `note`, `wait`, `met`. GraphQL lists conversations with `ListAgentConversations(archived, query)` and changes one with `UpdateAgentConversation(conversationId, title, archived, goal)`; `StartAgentConversation(title, goal)` makes a named one. The command line has `teanode agent conversation goal`, which lists or sets.

The dream. `internal/agent/dream.go` runs once a night per agent and calls passes in order (`dreamDigest`, `dreamAttachments`, `dreamTimeline`, `dreamConsolidate` and more), each drawing on a shared `dreamBudget` and asking the model through `dreamThought` in `internal/agent/dream_request.go`, which can let it look things up. `teanode agent dream now` starts one.

The dashboard. `web/src/pages/agent.tsx` is the agent's page with its tabs; `openAgentConversation(conversationId)` in `web/src/api.ts` opens the chat drawer on a conversation; the drawer keeps a draft per conversation under the browser storage key `teanode.agent.draft.<id>`. Layout rules are in `docs/coding/frontend-design.md`; data tables stay tables on a phone, with sideways scroll.

Migrations are numbered SQL files in `internal/db/migrations/`, each with a `.reverse.sql`; the newest is `0114`, so this plan adds `0115`. `docs/coding/database-migrations.md` says how.

## Plan of Work

Milestone 1 renames and restructures in one pass, so that at its end there is no tip left and ideas can be listed, started and closed from all three places, with the eight features as the only catalog.

The database. Migration `0115_agent_idea.sql` creates `agent_idea` with `id`, `agent_id` (foreign key, cascade), `idea_key`, `idea_kind` (`catalog` or `personal`), `idea_category`, `emoji`, `headline`, `body`, `opening_request`, `needed_tool_names` (text array), `evidence` (jsonb list of `{evidenceKind, evidenceId, evidenceSummary}`), `suggestion_reason`, `idea_status`, `rank_score`, `created_at`, `shown_at`, `started_at`, `started_conversation_id`, `closed_at`, `expires_at`, unique on `(agent_id, idea_key)`. It copies each `agent_tip` row in as a catalog idea with status `open` and `shown_at` set to `given_at`, so nothing already said comes back unprompted; renames `agent.is_tips_enabled` to `is_ideas_enabled`; changes the subject of `speak_first` job rows from `tip` to `idea`, so the once-a-day count carries across the upgrade; and drops `agent_tip`. The reverse file does the opposite, recreating `agent_tip` from the catalog ideas that have a `shown_at`.

The model and database layer. `internal/models/idea.go` holds `AgentIdea`, `AgentIdeaEvidence` and the vocabularies `AgentIdeaKind`, `AgentIdeaStatus` and `AgentIdeaCategory`, where each area also has its emoji list (`IdeaCategories()` returns them in display order). `models.AgentTip` goes. `internal/db/database_idea.go` holds `UpsertAgentIdea` (insert, or refresh text and rank without touching the status), `ListAgentIdeas(agentId, statuses, kinds)`, `GetAgentIdea`, `SetAgentIdeaStatus` and `MarkAgentIdeasShown`; `AddAgentTip` and `ListAgentTips` go. `IsTipsEnabled` becomes `IsIdeasEnabled` in the model, the database layer and GraphQL.

The catalog. `internal/agent/tips.go` becomes `internal/agent/ideas.go`. The eight entries move to `internal/agent/ideas_catalog.yaml`, embedded with `go:embed`, each with `ideaKey`, `ideaCategory`, `emoji`, `headline`, `body`, `openingRequest`, `neededToolNames` and, for the eight, `usedCheck` naming one of the existing `IsUsed` functions (kept in Go, in a map by name). Their text is rewritten as offers under the rules in Milestone 3. `refreshIdeas` upserts every catalog entry whose tools the person has and whose feature they do not use, and expires open catalog ideas that no longer qualify. It runs at the start of each dream and when ideas are listed, at most once an hour per agent.

The operations, in `internal/api/v1api/apigraph/agent_idea.go`, each a thin resolver over functions on `*agent.Agent` in `internal/agent/ideas.go`:

    ListAgentIdeas(ideaStatuses: [String], ideaKinds: [String]) -> { ideas, ideaCategories }
    ProposeAgentIdea(idea: AgentIdeaInput) -> AgentIdea                    checked by checkIdea; kind personal
    StartAgentIdea(ideaId: String, conversationId: String) -> { idea, conversation, openingRequest }
    SetAgentIdeaStatus(ideaId: String, ideaStatus: String) -> AgentIdea    done, dismissed, or open to reopen

`StartAgentIdea` with no conversation makes a named conversation titled with the headline; with one (the agent's tool passes the conversation it is in), it records that one. Either way the status becomes `started`. The speak-first reason becomes `SpeakFirstIdea = "idea"`, the surface `speak_first:idea`; its check-in takes the highest ranked open idea with no `shown_at`, marks it shown, and tells the agent to offer it in two sentences with the opening request as a suggested reply. `chooseTip` and `tip_choose.txt` are deleted, for the reason in the Decision Log.

The agent's tool. A new `internal/agent/tools/idea/idea.go` registers `idea` (risk `write`), with actions `list`, `propose`, `start`, `done`, `dismiss`, `reopen`, each calling the operation above through `operator.Execute`. Its guidance says: list when the person asks what you could do for them; propose only with evidence you looked up, and never an idea that needs a tool you lack; start when the person takes one up in this conversation; done, dismiss or reopen when they say so. `agent_profile`'s `no_more_tips` and `tips_on` become `no_more_ideas` and `ideas_on`, setting `isIdeasEnabled`.

The command line. `internal/cmd/agent_idea.go` adds `teanode agent idea list [--status] [--kind] [--json]`, `propose` (fields as flags, for scripts), `start <idea-id> [--send]` (prints the conversation id and the opening request; `--send` sends it as `teanode agent ask --conversation` would), `done <idea-id>`, `dismiss <idea-id>` and `reopen <idea-id>`, each calling the client function for its operation. `speak-first --reason` takes `idea`. `docs/reference/command-line.md` gets the row.

The dashboard, in this milestone only the rename: the checkbox reads "Ideas" with the strings `agent.ideas`, `agent.ideasHint`, `agent.ideasOn` and `agent.ideasOff` in all three languages, the GraphQL documents say `isIdeasEnabled`, the comments in `agentDrawer.tsx` and `useAgentPresence.ts` say idea, and the usage chart's `DayTip` and `usage-chart-tip*` become `DayTooltip` and `usage-chart-tooltip*`.

Milestone 2 is the Ideas tab: a tab `ideas` in `web/src/pages/agent.tsx`, rendering `web/src/pages/agentIdeas.tsx`, reading `ListAgentIdeas`. "For you" lists open personal ideas; then one section per area in the order the server gives, each idea a row with its emoji, headline and body clamped to three lines, a personal idea also showing its suggestion reason in the muted color and its evidence as links (a message to the mail detail page, a memory page to the knowledge explorer, a conversation to the drawer). Tapping a row calls `StartAgentIdea` and then `openAgentConversation(conversation.id, openingRequest)`; `openAgentConversation` gains the optional `draft`, which the drawer writes to the conversation's draft key before it shows it. A row menu offers "Not interested" (dismiss). History, below, is a table with a status filter (started, done, dismissed, expired): headline, status, when, and the conversation link for started ones, with "Mark done" on started rows and "Bring back" on dismissed and expired ones. Rows drawn on screen are reported shown. An empty conversation in the drawer shows the top three open ideas as the same rows, from one shared component. All strings in three languages; checked at phone, tablet and desktop widths, light and dark.

Milestone 3 fills the catalog to about thirty entries across the areas, invented and general (no names, places or companies from anyone's data). The text rules, which `checkIdea` enforces and the dream's prompt repeats: the headline is a first-person offer, or a situation followed by one, at most 80 characters; the body, at most 300, says what the person gives, what the agent does and where it stops to ask; an idea needing a tool of risk `outward` or `destructive` says it asks first; no "always", "every time" or "guaranteed"; the opening request is what the person would type, in their voice. An entry that needs a skill names the skill's tools with a trailing `*` (`skill__gmail__*`), which `checkIdea` and `refreshIdeas` read as any tool with that prefix.

Milestone 4 adds personal ideas. `checkIdea(ctx, run, idea, toolRisks)` in `internal/agent/ideas_check.go` runs the deterministic rules, then for personal ideas resolves each evidence reference against the person's messages, memory pages and conversations, compares the text by embedding with open and dismissed ideas (too close above 0.9, using the dream's embedder), and asks one model call, `internal/agent/prompts/idea_check.txt`, shown only the idea and the tool list with risks, whether it promises more than those tools do or leaves out where it asks first. A pass `dreamIdeas` in `internal/agent/dream_ideas.go`, after `dreamTimeline`, gives the model the day's digest, the next thirty days of the timeline, the tool list with risks, the areas and emoji, the open and recently dismissed ideas and the text rules, asks for at most six ideas with evidence it looked up, sends each through the function behind `ProposeAgentIdea`, and counts the rejections with their reasons on the dream record. A personal idea expires after fourteen days, or earlier when the model gives the date it stops mattering. Ranking (`internal/agent/ideas_rank.go`) orders open ideas by kind (personal first), age, and the person's history per area: dismissals lower an area, starts raise it.

Milestone 5 is goals. `ListAgentConversations` gains `isGoalInProgress: Boolean`, returning only conversations with a goal not met; `UpdateAgentConversation` gains `goalState`, accepting only `met` from a person and writing the note "Marked met by the person" so the next turn does not reopen it. The command line's `conversation goal` list passes `isGoalInProgress` and drops its own filter, and `conversation goal <id> --met` marks one met. The `goal` tool gains `list` (every goal in progress, through `ListAgentConversations(isGoalInProgress: true)`) and lets `met` name another conversation with `conversation_id`, so the person can say in the main chat that something is done. The Goals tab, `web/src/pages/agentGoals.tsx`: "In progress" lists goals with a checkbox (ticking marks met, with an undo toast for five seconds), the title, the note in the muted color and when it looks again, and a link to the conversation; "Runs on its own" lists the schedules and the rules of the mailboxes the agent may read, from the operations their own pages already use, each linking there; "Set a goal" lists the areas from `ListAgentIdeas`' `ideaCategories`, and choosing one calls `StartAgentConversation` and `openAgentConversation` with a drafted first line in the person's language. When the goal tool sets or meets a goal on a conversation that is an idea's `started_conversation_id`, the idea becomes `started` or `done`.

## Concrete Steps

From the repository root. Database tests need Docker (`make test` starts PostgreSQL).

    go test -mod=vendor ./internal/agent/ ./internal/agent/tools/idea/ ./internal/db/ -run 'Idea|SpeakFirst'
    go test -mod=vendor ./internal/cmd/ -run Idea
    cd web && npx tsc --noEmit -p . && npx vitest run
    make build
    TEANODE_PROFILE=local ./build/teanode agent idea list
    TEANODE_PROFILE=local ./build/teanode agent idea start <idea-id>

The last command prints the new conversation's id and the opening request; `agent idea list` then shows the idea as started.

After the rename, this must print nothing but the unrelated uses named in Context and Orientation and the two historical migrations and plan:

    git grep -n -iE '\btips?\b|tips?[A-Z_]|_tips?\b|Tips?Enabled|tip_choose|agent_tip' -- internal web/src docs

## Validation and Acceptance

Milestone 1: the grep above is clean; the migration applied to a copy of a database with given tips leaves those ideas with `shown_at` set, the switch as it was, and today's speak-first count unchanged; the same idea is listed identically by `teanode agent idea list --json`, by GraphQL `ListAgentIdeas`, and by the agent when asked what ideas it has; dismissing it from any one of the three shows it dismissed in the other two; `StartAgentIdea` returns a conversation in which no turn has run; `agent_profile` with `no_more_ideas` turns the switch off and the Ideas checkbox shows it off.

Milestone 2: in Chrome at 390, 820 and 1400 pixels, light and dark, the sections and rows read as described; tapping an idea opens the drawer on a new conversation with the request in the reply box and nothing sent; "Not interested" moves the idea to History as dismissed, and "Bring back" returns it.

Milestone 3: `go test ./internal/agent/ -run Catalog` passes on the full catalog and fails on an entry with an outward tool and no "asks first" wording, and a person with the default tools gets at least fifteen catalog ideas across at least six areas.

Milestone 4: a dream on a development account with a week of seeded mail yields personal ideas whose evidence links open the right messages; a proposed idea naming a tool the person lacks, citing a message that does not exist, or repeating a dismissed idea is refused with that reason, through the tool and through the command line alike.

Milestone 5: with two conversations carrying goals and one schedule, the Goals tab, `teanode agent conversation goal` and the agent asked what it is working on list the same two goals; ticking one on the tab marks it met everywhere; an idea started, given a goal in its conversation and marked met shows as done on the Ideas tab.

A task `ideas-01` in `docs/evaluation/end-to-end-tasks.md` replaces `tips-01` and covers the switch, starting an idea from the tab, managing one by talking to the agent, and a goal marked met.

## Idempotence and Recovery

Refreshing catalog ideas is an upsert that never changes a status, so it can run any number of times. The migration is reversible by its reverse file; take a database backup before applying it to a server with data, as `docs/reference/deployment.md` describes. Deleting every personal idea (`delete from agent_idea where idea_kind = 'personal'`) returns an agent to catalog ideas, and the next dream finds new ones.

## Artifacts and Notes

An invented catalog entry, to show the shape:

    ideaKey: letter_to_deadline
    ideaCategory: paperwork
    emoji: "📄"
    headline: Photograph a letter. I'll pull out what it asks and by when.
    body: Send a photo of a letter or form and I read what it asks for, put the deadline on your calendar and draft the reply. Nothing is sent until you say so.
    openingRequest: Here is a letter I got. What does it ask me to do, and by when?
    neededToolNames: [calendar, mail_draft]

## Interfaces and Dependencies

In `internal/models/idea.go`:

    type AgentIdeaKind string      // "catalog", "personal"
    type AgentIdeaStatus string    // "open", "started", "done", "dismissed", "expired"
    type AgentIdeaCategory string  // "money", "paperwork", "mail", "home", "family", "travel", "shopping", "health", "work", "fun", "assistant"

    type AgentIdeaEvidence struct {
        EvidenceKind    string `json:"evidenceKind"`    // "message", "page", "conversation"
        EvidenceID      string `json:"evidenceId"`
        EvidenceSummary string `json:"evidenceSummary"`
    }

    type AgentIdea struct {
        ID, AgentID, IdeaKey          string
        IdeaKind                      AgentIdeaKind
        IdeaCategory                  AgentIdeaCategory
        Emoji, Headline, Body         string
        OpeningRequest                string
        SuggestionReason              string
        NeededToolNames               []string
        Evidence                      []AgentIdeaEvidence
        IdeaStatus                    AgentIdeaStatus
        RankScore                     float64
        StartedConversationID         string
        CreatedAt                     time.Time
        ShownAt, StartedAt, ClosedAt  *time.Time
        ExpiresAt                     *time.Time
    }

In `internal/agent/ideas.go`, used by the GraphQL resolvers and the dream, and by nothing else:

    func (self *Agent) ListIdeas(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, statuses []models.AgentIdeaStatus, kinds []models.AgentIdeaKind) ([]*models.AgentIdea, error)
    func (self *Agent) ProposeIdea(ctx context.Context, run *Run, idea *models.AgentIdea) (*models.AgentIdea, error)
    func (self *Agent) StartIdea(ctx context.Context, tx db.Transaction, agent *models.Agent, ideaId, conversationId string) (*models.AgentIdea, *models.AgentConversation, error)
    func (self *Agent) SetIdeaStatus(ctx context.Context, tx db.Transaction, agent *models.Agent, ideaId string, status models.AgentIdeaStatus) (*models.AgentIdea, error)

No new libraries: YAML is already vendored for skills, and the embedder and model calls are the dream's own.

Revision 2026-09-28: renamed tips to ideas throughout at the person's request; made every operation one GraphQL operation reached the same way from the dashboard, the command line and the agent's tools; cut what the first version had twice (three kinds to two, a shown status to a timestamp, a tracking operation, a goal-start operation and a server-side draft to existing operations, the tip-choosing model call to ranking); named the two views History and Goals so that no word means two things.
