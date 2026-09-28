# Ideas and goals: what the agent offers to do, and what it is keeping track of

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows the rules in `~/.claude/PLAN.md` as copied into the header of every plan under `docs/planning/`, and it builds on `docs/planning/agent-speaks-first-execplan.md`, whose seventh milestone added the tips this plan replaces.

## Purpose / Big Picture

A person uses a small part of what their agent can do, and mostly does not know what else it could do for them. Today the agent tells them one thing at a time, at most once a day, as a chat message it starts on its own: a "tip". Tips come from a fixed list of eight dashboard features (connect a mailbox, make a schedule, set a goal and so on), are never grounded in anything the person's own mail or memory says, are never shown again once said, and leave no record of whether they helped.

After this plan, the agent keeps a list of ideas: concrete offers of work it can do, written as an offer ("I can compare this month's card charges against the last three and tell you what moved") with two or three lines on exactly what happens and where it stops to ask. The list has three kinds of idea. Personal ideas are found in the person's own mail, memory and activity during the nightly dream ("a subscription you keep marking as spam is still billing you; I can find the unsubscribe link and stop the mail"), and each one carries the evidence that prompted it. Offers are general ideas from a catalog in this repository, shown only when the person's agent has every tool the offer needs ("Photograph a letter and I read what it asks for, add the deadline to your calendar and draft the reply"). Setup ideas are the eight existing tips, shown until the person uses the feature.

The person sees the list on a new Ideas tab of their agent's page: personal ideas first, then offers grouped by area of life (money, mail and paperwork, home, family, travel, shopping, health, work, fun), each with an emoji and a one-line headline. Tapping an idea opens the chat drawer in a new conversation with the idea's opening request filled in, for the person to send or edit; nothing happens until they send it. Each idea can be dismissed. The same tab tracks what became of every idea: shown, started (with a link to the conversation it started), done, dismissed or expired, and why the agent suggested it. The occasional message the agent starts on its own ("speak first") keeps its one-a-day limit, but now draws on the top personal idea rather than the fixed list.

Ideas lead to goals. A goal already exists in teanode: a conversation can carry a standing instruction the agent keeps working toward across turns of its own, with a state (`working`, `waiting`, `met`), a one-line note on where it stands and when it looks again. Today a goal is visible only inside its own conversation in the chat drawer, so a person with five goals has no single place that says what the agent is keeping track of for them. After this plan, a Goals tab lists every goal in progress as a row with a checkbox, the goal in a few words and the agent's latest note under it ("Waiting for the seller to confirm the size; I check again tomorrow at 9"), together with the schedules and mail rules that run on their own, since those are also things the agent keeps doing. Ticking a goal marks it met. Below the list, "Set a goal" offers one row per category; choosing one opens a conversation where the agent asks what the person is after and proposes a goal and a plan for it, which the person confirms before it is set. When an idea is started and its conversation ends up with a goal, the idea's status follows the goal: started while the goal is in progress, done when it is met.

To see it working: on a development server with a person whose agent has a mailbox and some memory, run `teanode agent dream` (or wait for the night), open Settings, Your agent, Ideas, and see a "For you" section with ideas that each name what they were drawn from, followed by grouped offers. Tap one; the drawer opens with the request ready to send. Send it, and the idea moves to "Started" with a link to that conversation. Dismiss another, and it moves to "Dismissed" and is not suggested again.

## Progress

- [x] (2026-09-28) Mapped the tip system as it is (see Context and Orientation) and wrote this plan.
- [ ] Milestone 1: the idea as a stored record, the catalog file, the availability check, and the GraphQL operations; the existing tips become setup ideas.
- [ ] Milestone 2: the Ideas tab, with starting, dismissing and tracking.
- [ ] Milestone 3: the offer catalog, about thirty entries, each checked against the tools it needs.
- [ ] Milestone 4: personal ideas from the dream, with evidence and an honesty check.
- [ ] Milestone 5: ranking from what the person took up and dismissed, speak-first drawing on the list, and the end-to-end task.
- [ ] Milestone 6: the Goals tab: every goal in progress with its latest note, the schedules and mail rules beside them, ticking a goal met, setting a goal by category, and an idea's status following the goal it led to.

## Surprises & Discoveries

- Observation: the plan for tips said the agent would favor tips about "tools they used" and "a skill that is installed", but the code never read either; the choice is one model call shown the person's last ten conversation titles and nothing else.
  Evidence: `chooseTip` in `internal/agent/tips.go` renders `tip_choose.txt` with `Candidates`, `Lately` and `Given` only.
- Observation: a tip is recorded as given when the turn that says it starts, whatever the person does with it, so a tip ignored in a drawer nobody read is spent forever.
  Evidence: `checkIn` in `internal/agent/tips.go` calls `tx.AddAgentTip` before the turn runs.

## Decision Log

- Decision: one record for all three kinds of idea (setup, offer, personal), rather than keeping tips and adding ideas beside them.
  Rationale: the person sees one list and one tracking view, and speak-first draws on one ranking. The difference between the kinds is where an idea comes from and how its availability is checked, which is a column, not a table.
  Date/Author: 2026-09-28, plan author.

- Decision: an idea never acts. Starting one puts its opening request into a new conversation for the person to send.
  Rationale: an idea is written by a model from mail and memory the person has not read today, and some offers end in something outward (a message sent, an order placed). Making the person send the first message keeps every idea inside the approval rules that already govern the conversation, and it lets them correct a wrong premise before any work is done.
  Date/Author: 2026-09-28, plan author.

- Decision: an idea may only promise what the person's agent can do with the tools it has now, and says where it will stop to ask. This is enforced twice: a deterministic check that every tool the idea names exists in the person's tool list, and a model check that the text claims nothing beyond those tools.
  Rationale: the ideas that read best in comparable products are often ones the agent could not carry out ("I'll haggle with the seller and arrange the pickup", "I'll file the claims end to end"). An offer the agent then fails at teaches the person that ideas are advertising. The tools already carry their risk (read, write, destructive, outward, see `internal/agent/tools/tool.go`), so "where it stops to ask" can be derived rather than trusted.
  Date/Author: 2026-09-28, plan author.

- Decision: personal ideas are generated in the nightly dream, not during the day.
  Rationale: the dream already reads the day's mail digest, the timeline and memory with a budget and a cheap model (see `internal/agent/dream.go`), and it runs while nobody waits. Finding patterns ("the same kind of alert every morning", "a document with a deadline next month") needs exactly that reading.
  Date/Author: 2026-09-28, plan author.

- Decision: emoji, from a fixed list per category, rather than generated pictures or the dashboard's line icons.
  Rationale: an emoji renders in the dashboard, in Telegram and in mail with no asset pipeline, and a fixed list stops a model from choosing one that reads as a joke next to a serious idea (an immigration letter, a medical bill). The dashboard's line icons cannot tell thirty offers apart.
  Date/Author: 2026-09-28, plan author.

- Decision: the tracking view is part of the Ideas tab, not a separate page.
  Rationale: the person asked for a UI that tracks the tips. What they want to see is what became of each idea next to the ideas still open: which they started, where that conversation is, which they dismissed and why each was suggested. Two pages would split one list by status.
  Date/Author: 2026-09-28, plan author, at the person's request.

- Decision: a Goals tab listing every conversation goal in progress, with schedules and mail rules beside them, rather than a new kind of goal record.
  Rationale: the person asked for goal tracking alongside ideas, after a comparable product that lists everything its assistant is tracking with a one-line status. Everything needed already exists: a goal is stored on its conversation with its state and the agent's latest note, a schedule and a mail rule each have their own table. What is missing is one place that lists them. A new record would duplicate the goal and drift from it.
  Date/Author: 2026-09-28, plan author, at the person's request.

- Decision: "Set a goal" opens a conversation that proposes a goal for the person to confirm, rather than a form.
  Rationale: a goal is useful when it is specific and checkable ("save 300 a month toward the trip until March"), and people rarely write it that way first. The goal tool (`internal/agent/tools/goal/goal.go`) is told to `set` a goal only when the person asks for one, so the conversation proposes a goal in words, and the person saying yes is the asking; a category only frames the first question.
  Date/Author: 2026-09-28, plan author.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

The server is Go under `internal/`, the dashboard is React under `web/src/`. The "agent" is the person's assistant: `internal/agent/` holds its loop, its tools and its background work, and `models.Agent` (in `internal/models/agent.go`) is its row in the `agent` table, one per person.

Tips today. `internal/agent/tips.go` holds `tipCatalog`, eight `Tip` values, each with a `TipKey` (for example `schedule`), a `Feature` sentence, a `Where` (a dashboard link or what to say) and an `IsUsed` function that answers whether the person already uses the feature. `tipsToGive` filters the catalog to tips not used and not yet given. Tips are one of three "speak first" reasons in `internal/agent/speak_first.go`: a sweep every minute looks at agents whose person has the dashboard open, and if the person has been quiet for five minutes, has not been spoken to unprompted in a day, and no tip decision was made in a day, `chooseTip` asks a model (prompt `internal/agent/prompts/tip_choose.txt`) whether to give a tip and which. If it says yes, `checkIn` records the tip in the `agent_tip` table (migration `internal/db/migrations/0112_agent_tip.sql`: `agent_id`, `tip_key`, `given_at`, `conversation_id`, unique on agent and key) and starts a turn in the main conversation telling the agent to say it in two sentences. The person can reply "no more tips" or "not now", which the `agent_profile` tool (`internal/agent/tools/agentprofile/agentprofile.go`) turns into `is_tips_enabled = false` or a day's snooze. There is no card, no list and no tracking in the dashboard: a tip is an ordinary chat message labeled "Started by the agent" in `web/src/components/agentDrawer.tsx`. The switch for tips is on the agent's settings page, `web/src/pages/agent.tsx`, under the keys `agent.tips*` in `web/src/i18n/en.ts`, `ja.ts` and `zh.ts`.

Tools. The tools a person's agent can use are listed by `DirectTools` in `internal/agent/direct.go`; each is a `tools.Tool` (in `internal/agent/tools/tool.go`) with a `Name`, a `Description`, parameters, and a `Risk`: `read`, `write`, `destructive`, `outward` (reaches another person or service on their behalf) or `granting`. Skills installed from the registry add tools named `skill__<skill>__<tool>`, and connected MCP servers add their own. Which tools exist therefore differs per person and changes when they install or remove something.

The dream. `internal/agent/dream.go` runs once a night per agent, as a job of kind `dream`, and calls passes in order (`dreamDigest`, `dreamAttachments`, `dreamTimeline`, `dreamConsolidate`, `dreamOrganize`, `dreamSplit`, `dreamAssociate`, `dreamRehearse` and others). Each pass draws on a shared `dreamBudget` and asks the model through `dreamThink` or `dreamThought` in `internal/agent/dream_request.go`, which can let the model look things up in memory and mail (`lookups`).

GraphQL. The dashboard talks to the server through operations defined as Go methods in `internal/api/v1api/apigraph/`, one file per area (for example `agent_speak_first.go`). Migrations are numbered SQL files in `internal/db/migrations/`; the newest is `0114_agent_interaction_run_call.sql`, so this plan adds `0115`. `docs/coding/database-migrations.md` says how to add one safely.

Terms used below. An "idea" is one offer of work, stored as a row. Its "kind" is `setup`, `offer` or `personal`. Its "needs" are the tool names it requires. Its "evidence" is the list of things in the person's data that prompted a personal idea, each a reference the dashboard can link to (a message, a memory page, a conversation). Its "status" is one of `open`, `shown`, `started`, `done`, `dismissed`, `expired`. Its "opening request" is the first message a started idea puts in the new conversation.

## Plan of Work

Milestone 1 makes the idea a stored record and moves the eight tips onto it, with no visible change yet except through the command line. Add migration `0115_agent_idea.sql` creating `agent_idea` with `id`, `agent_id` (foreign key to `agent`, cascade), `idea_key` (stable per agent: the catalog key for setup and offer ideas, a generated one for personal ideas), `idea_kind`, `idea_category`, `emoji`, `headline`, `body`, `opening_request`, `needed_tool_names` (text array), `evidence` (jsonb list of `{evidenceKind, evidenceId, evidenceSummary}`), `suggestion_reason` (one line, why this person), `idea_status`, `rank_score` (real), `created_at`, `shown_at`, `started_at`, `started_conversation_id`, `closed_at`, `expires_at`, and a unique index on `(agent_id, idea_key)`. Copy every `agent_tip` row into it as a setup idea with status `shown` and `shown_at = given_at`, so nothing the person was told comes back; keep `agent_tip` until Milestone 5 removes it. Add `models.AgentIdea` and the vocabularies `models.AgentIdeaKind`, `models.AgentIdeaCategory`, `models.AgentIdeaStatus` in a new `internal/models/idea.go`, and database methods in a new `internal/db/database_idea.go`: `UpsertAgentIdea` (insert, or on conflict refresh text and rank but never status), `ListAgentIdeas(agentId, statuses)`, `SetAgentIdeaStatus`, and `ExpireAgentIdeas(now)`.

The catalog moves out of Go into `internal/agent/ideas/catalog.yaml`, embedded with `go:embed`, read by a new package `internal/agent/ideas`. Each entry has `ideaKey`, `ideaKind` (`setup` or `offer`), `ideaCategory`, `emoji`, `headline`, `body`, `openingRequest`, `neededToolNames`, and for setup ideas `usedCheck`, naming one of the eight existing `IsUsed` functions, which move to `internal/agent/ideas/used.go` unchanged. The package exposes `Catalog() []*Entry`, and `Available(entry, toolNames) bool`, which is true when every needed name is among the person's tools (a trailing `*` matches a prefix, so `skill__gmail__*` means any tool of the gmail skill). A test reads the catalog and fails when a category or emoji is outside its vocabulary, a headline is over 80 characters, a body over 300, or an entry with an outward or destructive tool in `neededToolNames` has a body that does not say it asks first (it must contain one of the phrases listed in the package, such as "asks you first" or "until you say so").

A function `refreshCatalogIdeas(tx, agent, owner, toolNames)` in `internal/agent/ideas_refresh.go` upserts every available catalog entry for the agent as an `open` idea, and marks `expired` an open catalog idea whose tools are gone or, for setup ideas, whose feature the person now uses. It runs at the start of each dream and when the Ideas tab is opened, at most once an hour.

GraphQL gains, in a new `internal/api/v1api/apigraph/agent_idea.go`: `ReadAgentIdeas(statuses)` returning the person's ideas with their evidence; `StartAgentIdea(ideaId)`, which creates a named conversation titled with the headline, puts the opening request in it as an unsent draft, sets the status to `started` with the conversation id, and returns the conversation; `DismissAgentIdea(ideaId)` and `ReopenAgentIdea(ideaId)`; `MarkAgentIdeasShown(ideaIds)`. The command line gains `teanode agent ideas list` and `teanode agent ideas refresh` through the generic operation reach described in `docs/reference/command-line.md`.

Milestone 2 is the Ideas tab. Add a tab `ideas` to the tabs in `web/src/pages/agent.tsx`, rendering a new `web/src/pages/agentIdeas.tsx`. The top section, "For you", lists open personal ideas; then one section per category of open offers and setup ideas, in a fixed category order, each as a row with the emoji, the headline in the title weight, and the body clamped to three lines. A personal idea shows its suggestion reason under the body in the muted color, and its evidence as small links (a message opens the mail detail page, a memory page opens the knowledge explorer). Tapping a row calls `StartAgentIdea` and opens the chat drawer on the returned conversation with the draft in the reply box. A menu on each row has "Not interested", which dismisses it. Below the open ideas, a "Tracking" section with a status filter (Started, Done, Dismissed, Expired) lists closed and started ideas in a table (tables stay tables on a phone, with sideways scroll): the headline, the kind, the status, when, and for started ones a link to the conversation. A started idea's row offers "Mark done" and a dismissed one "Bring back". When the tab is first drawn, the rows on screen are reported through `MarkAgentIdeasShown`. The empty chat drawer (a new conversation with nothing in it) shows the top three open ideas as the same rows. Every string goes through the i18n files in English, Japanese and Chinese. The layout follows `docs/coding/frontend-design.md` and is checked at phone, tablet and desktop widths, light and dark.

Milestone 3 writes the offer catalog: about thirty entries across the categories, every one invented and general (no names, no places, no companies from anyone's data), each written to the rules below and each naming its tools. The rules for the text, which the dream's prompt in Milestone 4 repeats: the headline is an offer in the first person or a situation followed by one ("Moving? I'll keep the whole move on one dated list."); the body says what the person gives, what the agent does step by step, and where it stops to ask; nothing is promised that a listed tool cannot do; nothing says "always", "every" or "guaranteed"; the opening request is what the person would type to start it, in their voice. Where a good idea needs a tool that exists only as a skill, the entry names the skill's tools with a prefix, so it appears only for people who installed it.

Milestone 4 adds personal ideas. A new dream pass `dreamIdeas` in `internal/agent/dream_ideas.go` runs after `dreamTimeline` and before `dreamConsolidate`, within the dream budget. It gives the model, through `dreamThought` with lookups on, the day's digest, the next thirty days of the timeline, the person's tool list with each tool's risk, the categories and emoji, the open ideas and the ones dismissed in the last ninety days, and the text rules, and asks for at most six candidate ideas as JSON, each with evidence references it looked up. Every candidate then goes through `checkIdea`, in `internal/agent/ideas_check.go`, which rejects it when a needed tool is not in the person's list, when an evidence reference does not resolve to a message, page or conversation of this person, when the text breaks the length or vocabulary rules, when the text is too close to an open or dismissed idea (by embedding similarity over 0.9, using the embedder the dream already uses), or when a second model call, prompt `internal/agent/prompts/idea_check.txt`, shown only the idea and the tool list with risks, answers that it promises something those tools cannot do or omits where it asks first. Accepted ideas are upserted as `personal` with an expiry of fourteen days unless the model gave an earlier date the idea stops mattering (a deadline). The pass logs each rejection with its reason at debug level and counts them on the dream record, so a prompt that produces mostly rejects shows up in the dream's summary.

Milestone 5 ranks and closes the loop. `rank_score` for an open idea is computed in `internal/agent/ideas_rank.go` from its kind (personal above offer above setup), its age, how many of the person's recent conversations touch its category, and the person's history: each dismissed idea in a category lowers that category, each started one raises it. The Ideas tab sorts by it within each section. The tip speak-first reason in `internal/agent/tips.go` changes to take the highest ranked open personal or setup idea not yet shown, pass it to the model with the same "is this a good moment" question, and tell it in the chat as today with the headline and the opening request as suggested replies; the idea is marked `shown`, not given, so it stays on the Ideas tab. `agent_tip` is then dropped by migration `0116`. `docs/evaluation/end-to-end-tasks.md` gets a task `ideas-01` (open the Ideas tab, start a personal idea, check that the draft is there and nothing ran; dismiss one; check the tracking list) replacing the tip half of `tips-01`, and `docs/subsystems/` gets a page `ideas.md` describing the whole of it.

Milestone 6 is the Goals tab. Add a GraphQL operation `ReadAgentTracking` in a new `internal/api/v1api/apigraph/agent_tracking.go` returning three lists: conversations of the person's agent with a goal whose state is `working` or `waiting` (id, title, goal text, `goalState`, `goalNote`, `goalNextAt`, `goalSetAt`), the agent's schedules as `ListAgentSchedules` returns them, and the rules of the mailboxes the agent may read (a rule belongs to a mailbox and is edited on the Rules tab of that mailbox's settings page, `web/src/pages/mailboxSettings.tsx`; nothing records whether the person or the agent wrote it, so all of them are listed). Add `SetAgentGoalMet(conversationId)`, which does what the goal tool does when the agent decides a goal is met: sets the state to `met` with a note that the person marked it, so the next turn does not reopen it. Add a tab `goals` to `web/src/pages/agent.tsx`, rendering `web/src/pages/agentGoals.tsx`: a "Tracking" list with, for each goal, a checkbox (ticking calls `SetAgentGoalMet` after a toast that can undo it for five seconds), the goal's title, the note in the muted color and a relative time for the next check, and a row menu with "Open conversation"; then "Runs on its own" with the schedules and rules, each linking to where it is edited today; then "Set a goal", one row per idea category with its emoji and a plus. Choosing a category calls a new `StartAgentGoal(ideaCategory)`, which creates a named conversation with an unsent draft ("I'd like to set a goal about my health.", in the person's language) and returns it, and the drawer opens on it; the agent then asks what they are after and proposes a goal in words, and sets it with the goal tool once they agree. Finally, the idea status follows the goal: when a goal is set or met on a conversation that is some idea's `started_conversation_id`, the goal tool's handler updates that idea to `started` or `done`. The acceptance: with two conversations carrying goals, one waiting and one working, and one schedule, the Goals tab shows both goals with their notes and the schedule; ticking one moves it out of the list and its conversation shows the goal as met; choosing "Money" under "Set a goal" opens the drawer on a new conversation with the draft in the box; starting an idea, letting the agent set a goal in that conversation and then ticking it on the Goals tab shows the idea as done on the Ideas tab.

## Concrete Steps

All commands run from the repository root. Tests that touch the database need Docker, as `make test` starts PostgreSQL in a container; a single package can be tested against it the same way.

After Milestone 1:

    go test -mod=vendor ./internal/agent/ideas/ ./internal/db/ -run 'Idea|Catalog'
    go test -mod=vendor ./internal/agent/ -run 'Tip|Idea'
    make build && TEANODE_PROFILE=local ./build/teanode agent ideas refresh && TEANODE_PROFILE=local ./build/teanode agent ideas list

The last command prints the available setup and offer ideas for the development account, one per line with key, kind, category and status.

After Milestone 2, `make dev` and open http://127.0.0.1:10000/settings/agent/ideas.

After Milestone 4, on the development server with a mailbox granted to the agent:

    TEANODE_PROFILE=local ./build/teanode agent dream
    TEANODE_PROFILE=local ./build/teanode agent ideas list --status open

and expect personal ideas, each with at least one evidence reference.

## Validation and Acceptance

Milestone 1 is accepted when the catalog test fails on a catalog entry with an outward tool and no "asks first" wording and passes on the shipped catalog; when a person whose agent lacks a tool an offer needs does not get that offer, and gets it after the tool appears; when the eight former tips appear as setup ideas with their given ones already `shown`; and when `StartAgentIdea` returns a conversation whose draft is the opening request and no turn has run in it.

Milestone 2 is accepted by hand in Chrome at 390, 820 and 1400 pixels wide, in light and dark: sections and rows as described, starting an idea opens the drawer on the new conversation with the draft in the box, dismissing moves the row to Tracking under Dismissed, "Bring back" returns it, and a started idea links to its conversation.

Milestone 3 is accepted when the catalog test passes with the full catalog and a person with the default tools sees at least fifteen offers across at least six categories.

Milestone 4 is accepted when a dream on a development account with a week of seeded mail produces personal ideas whose evidence links open the right messages; when a seeded idea that names a tool the person lacks, or cites a message that does not exist, is rejected with that reason; and when an idea close to a dismissed one is not suggested again.

Milestone 6 is accepted as described at the end of its paragraph in Plan of Work, checked in Chrome at the same three widths.

Milestone 5 is accepted when dismissing three ideas of one category moves that category's remaining ideas below others on the tab; when the agent's unprompted tip names an idea from the list and the idea then shows as shown, not gone; and when the end-to-end task `ideas-01` passes on the development server.

## Idempotence and Recovery

The migration only adds a table and copies rows; its down file drops the table, and `agent_tip` is untouched until Milestone 5. Refreshing catalog ideas is an upsert that never changes a status, so it can run any number of times. The dream pass only adds or updates rows with kind `personal`; deleting every such row (`delete from agent_idea where idea_kind = 'personal'`) returns an agent to catalog ideas only, and the next dream finds new ones.

## Artifacts and Notes

An invented catalog entry, to show the shape:

    ideaKey: letter_to_deadline
    ideaKind: offer
    ideaCategory: paperwork
    emoji: "📄"
    headline: Photograph a letter. I'll pull out what it asks and by when.
    body: Send a photo of any letter or form and I read what it asks for, put the deadline on your calendar and draft the reply. Nothing is sent until you say so.
    openingRequest: Here is a letter I got. What does it ask me to do, and by when?
    neededToolNames: [calendar, mail_draft]

## Interfaces and Dependencies

In `internal/models/idea.go`:

    type AgentIdeaKind string      // "setup", "offer", "personal"
    type AgentIdeaCategory string  // "money", "paperwork", "mail", "home", "family", "travel", "shopping", "health", "work", "fun", "setup"
    type AgentIdeaStatus string    // "open", "shown", "started", "done", "dismissed", "expired"

    type AgentIdeaEvidence struct {
        EvidenceKind    string `json:"evidenceKind"`    // "message", "page", "conversation"
        EvidenceID      string `json:"evidenceId"`
        EvidenceSummary string `json:"evidenceSummary"`
    }

    type AgentIdea struct {
        ID, AgentID, IdeaKey string
        IdeaKind AgentIdeaKind
        IdeaCategory AgentIdeaCategory
        Emoji, Headline, Body, OpeningRequest, SuggestionReason string
        NeededToolNames []string
        Evidence []AgentIdeaEvidence
        IdeaStatus AgentIdeaStatus
        RankScore float64
        CreatedAt time.Time
        ShownAt, StartedAt, ClosedAt, ExpiresAt *time.Time
        StartedConversationID string
    }

In `internal/agent/ideas`:

    func Catalog() []*Entry
    func Available(entry *Entry, toolNames []string) bool

In `internal/agent`:

    func (self *Agent) refreshCatalogIdeas(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User) error
    func (self *Agent) dreamIdeas(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget)
    func (self *Agent) checkIdea(ctx context.Context, run *Run, candidate *models.AgentIdea, toolRisks map[string]tools.Risk) (bool, string)

No new libraries: YAML is already vendored for skills, and the embedder and the model calls are the dream's own.
