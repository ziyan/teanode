# The agent's feed

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows ~/.claude/PLAN.md.


## Purpose / Big Picture

Today everything the agent says without being asked lands in the person's main conversation: an alert about a suspicious sign-in, an idea, a memory check, a trip reminder, the answer of a scheduled report. The main conversation becomes a pile of things the person did not start, and a question the agent needs answered ("was this sign-in you?") interrupts whatever the person was talking about.

After this change the agent has a feed: a list of cards, newest first, that the person scrolls like a social feed. Each card has a small picture, a headline, a few lines of text with links, sometimes a short table of facts, and a footer with a heart, a Discuss button, the date and an info button. A card the agent needs an answer on also carries the choices as buttons ("That's me" and "Secure my account"). Tapping a choice answers the agent without opening any conversation: the agent carries on in the background, and the card shows what was chosen and what came of it. Discuss opens a conversation about that card. The heart tells the agent "more like this"; the info button says why the card is there (which goal, schedule or alert made it) and lists its sources.

Feeds are made by goals and schedules as well as by alerts: the person can set a goal such as "keep a feed of the news I care about: humanoid robots, my brokerage's offers, the weather where I am travelling", and the goal's runs post cards. A card about the same thing as an earlier one replaces it instead of piling up (today's weather is one card that changes, not one card per hour).

To see it working: open Feed on the dashboard or the iPhone, ask the agent in the main conversation to "post a test decision to my feed with two choices", watch the card appear, tap a choice, and see the card settle on that choice with the agent's follow-up line under it, while the main conversation stays untouched.


## Orientation

TeaNode is a Go server (internal/), a React dashboard (web/), an iPhone app (ios/) and an Android app (android/). The person's agent runs turns: a turn is one model conversation step with tools. Turns happen in conversations (models.AgentConversation); a conversation of kind "main" is the person's main chat, "named" is one the person started, and "run" is the record of something the agent did on its own (a dream, a triage, a schedule run). Jobs (internal/agent/job*.go, table agent_job) are queued work the agent's worker runs.

These existing parts are what the feed is built from:

The alert pipeline, internal/agent/alert.go and alert_candidate.go: things that might be worth telling (a message the sorting flagged, a burst of similar messages, a budget crossing, a due follow-up) become alert candidates (models.AgentAlertCandidate); every couple of minutes the alert job asks a model which few to tell, within the person's daily limit and outside their night, and says them in the main conversation, recording each as models.AgentAlert.

Speaking first, internal/agent/speak_first*.go: a turn in the main conversation nobody asked for, to introduce the agent, show a newly connected phone around, mention a trip, check a memory or offer an idea.

Schedules, internal/agent/schedule.go: a prompt the agent runs at chosen times; the answer goes out by mail or is a turn in the conversation the schedule was made in.

Goals (agent goals, web/src/pages/agentGoals.tsx and internal/agent tools): long-running aims the agent checks in on.

Durable cards, internal/agent/interaction.go and models.AgentInteraction (internal/models/insight.go): a question or approval card raised during a turn stays open after the turn ends; answered later, it starts a new turn with the answer (Agent.ResumeInteraction), and an approval allows that one call. docs/planning/durable-interactions-execplan.md describes it.

The feed adds one table, one agent tool, one set of API operations and one screen per app, and changes where alerts, speaking first and schedule answers go.


## Plan of Work

### The table

A new migration (the next free number, 0276 at the time of writing; check internal/db/migrations and the open pull requests first) creates agent_feed_item with a reverse migration that drops it. Each row is one card:

    id                    text primary key (the usual ULID)
    agent_id              text, the agent that posted it
    user_id               text, whose feed it is
    feed_item_kind        text: update, decision, alert, report, idea, news
    feed_source_kind      text: goal, schedule, alert, speak_first, coding_session, agent (a turn's own post)
    feed_source_id        text, the goal, schedule or alert it came from, empty for a turn's own post
    run_conversation_id   text, the run that posted it, to read the agent's work
    title                 text, the headline
    body                  text, Markdown: a few lines, links allowed
    picture_symbol        text, a symbol or emoji chosen by the agent for the card's picture
    fact_rows             jsonb, [{factLabel, factValue}] for the short table, at most eight
    source_links          jsonb, [{sourceTitle, sourceUrl}] for the info sheet
    record_references     jsonb, links to TeaNode records (a mail, an event, a transaction), the same shape messages use
    coalesce_key          text, nullable: a later post with the same key from the same agent replaces this card
    choices               jsonb, [{choiceKey, choiceLabel, choiceRole}], choiceRole one of action, dismiss, discuss, open
    interaction_id        text, the durable card behind a decision, nullable
    chosen_choice_key     text, nullable
    chosen_at             timestamptz, nullable
    outcome_text          text, the agent's one line after a choice ("Signed out the other session.")
    feed_urgency          text: now, today, whenever
    is_liked              boolean
    read_at, dismissed_at, created_at, updated_at, expires_at   timestamptz

Indexes: (user_id, dismissed_at, created_at desc) for the list; a unique index on (agent_id, coalesce_key) where coalesce_key is not null and dismissed_at is null, so that replacing is an upsert.

### The contract every surface builds against

GraphQL, in internal/api/v1api/apigraph/agent_feed.go, following agent_goal.go's pattern (a query interface, a mutation interface, argument structs, a view struct, each documented):

    query    ListAgentFeed(first: Int, after: String, includeDismissed: Boolean): AgentFeedPage
             AgentFeedPage { feedItems: [AgentFeedItemView], nextCursor: String (nullable), unreadCount: Int }
    query    GetAgentFeedItem(feedItemId: String): AgentFeedItemView
    mutation ChooseAgentFeedItem(feedItemId: String, choiceKey: String): AgentFeedItemView
    mutation LikeAgentFeedItem(feedItemId: String, isLiked: Boolean): AgentFeedItemView
    mutation ShowLessLikeAgentFeedItem(feedItemId: String): AgentFeedItemView     (records the preference and dismisses)
    mutation DismissAgentFeedItem(feedItemId: String): AgentFeedItemView
    mutation MarkAgentFeedRead(feedItemIds: [String]): Int                         (how many were newly read)
    mutation DiscussAgentFeedItem(feedItemId: String): String                      (the main conversation's id, with the card attached as a reference to the next message)
    mutation PostAgentFeedItem(...the tool's post fields...): AgentFeedItemView    (the tool's post, for the CLI and tests)
    subscription event: agentFeedChanged { feedItemId, feedChange: posted | replaced | settled | dismissed, unreadCount }

    AgentFeedItemView {
      feedItemId, feedItemKind, feedSourceKind, feedSourceId, feedSourceTitle (the goal's or schedule's name, empty otherwise),
      runConversationId, title, body, pictureSymbol,
      factRows: [{ factLabel, factValue }], sourceLinks: [{ sourceTitle, sourceUrl }],
      recordReferences: the same reference objects messages carry,
      choices: [{ choiceKey, choiceLabel, choiceRole }]   (choiceRole: action, dismiss, discuss, open),
      chosenChoiceKey (nullable), chosenAt (nullable), outcomeText, isOutcomePending (a resumed turn still running),
      feedUrgency, isLiked, isRead, isDismissed, postedAt, updatedAt
    }

pictureSymbol is an SF Symbol name (for example cloud.sun, lock.shield, chart.line.uptrend.xyaxis, newspaper); the dashboard maps the same names to its icon set and falls back to a newspaper. Command line: teanode agent feed list [--all], show ID, choose ID CHOICE, like ID, unlike ID, less ID, dismiss ID, read ID..., discuss ID, post --title ... (--json on every one).

### Posting

A new agent tool, internal/agent/tools/feed/feed.go, named feed, with actions post, update, withdraw and list. post takes title, body, pictureSymbol, factRows, sourceLinks, recordReferences, coalesceKey, feedUrgency and choices. A post with a coalesceKey that matches an open card replaces that card's content, moves it to the top and marks it unread. A post with choices whose roles include action raises a durable question card (models.AgentInteraction) in the posting turn's conversation, with the choices as its options, and stores its id on the feed item; the turn does not wait on it. The tool's description tells the model: post to the feed what the person did not ask for; keep the main conversation for answering them; one card per subject; never post the same news twice (the tool refuses a sourceUrl already in an open card from the last 30 days).

The operations live where the dashboard and the command line reach them too (internal/api/v1api/apigraph for GraphQL, internal/client and internal/cmd for the command line), so the tool, the API and the CLI are one operation each, as every TeaNode capability is.

### Routing what is said unasked

The alert job (internal/agent/alert.go) posts each decided alert as a feed item of kind alert, with the alert's subject key as coalesce key, instead of a turn in the main conversation; its bounds (a few a day, nothing at night unless it cannot wait) stay, and decide the card's urgency. Speaking first posts ideas, memory checks and trip notes as cards (an introduction and a newly connected phone's tour stay in the main conversation, since they are a conversation). Schedules gain a destination, feed, beside mail and conversation, and it becomes the default for schedules made from now on. Goals post with the feed tool during their runs. Each of these can be sent back to the main conversation by a per-agent setting, feedDestination, so nothing is lost for a person who prefers the old way.

### Choosing

ChooseAgentFeedItem(feedItemId, choiceKey) records the choice and then, by role: a dismiss choice ("That's me", "Ignore") closes the card and resolves its durable card with that answer without starting a turn, unless the posting turn asked to be told (a flag on the choice); an action choice sends a message in the person's main conversation on their behalf, with the card attached as a reference and worded as their choice ("Approve: Sort newsletters into a Newsletters folder"), which starts an ordinary main-conversation turn that carries the choice out (a durable approval card behind the feed card is resolved with the choice, so its one call is allowed in that turn); the card settles with the choice, and the first line of that turn's answer is stored as its outcome_text; a discuss choice, and the Discuss button on every card, opens a conversation about the card (see the decision below) with the card attached as a reference, so the agent knows what is being discussed.

### Coding session questions

A coding session (Claude Code or Codex in a herdr pane on one of the person's computers) that asks a question already notifies the phones (Agent.tellHerdrQuestion in internal/agent/herdr.go) and is answered on the Coding page or screen (Agent.AnswerHerdrQuestion). With the feed, the same asking event also posts a card: kind decision, feedSourceKind coding_session, feedSourceId the computer and pane joined by a slash, coalesce key the computer, pane and question fingerprint, title "<agent> on <computer> asks", body the question, and one choice per option (choiceRole action, choiceKey "option:N" with N the option's number). Choosing one calls AnswerHerdrQuestion with that option number, without any agent turn, and the card settles with the option as its outcome. A question that takes several options or free words gets one choice, "Open session" (choiceRole open), which opens the coding session where it can be answered fully. When the session reports the question answered (computer.HerdrEventKindAnswered), from anywhere, the card settles as "Answered on the computer". The phone notification stays as it is (it is urgent: a session is blocked), and on the iPhone it carries the option buttons as notification actions.

### Liking and why

LikeAgentFeedItem toggles is_liked; a like or unlike is written to the agent's memory as a preference about the card's subject ("likes news about humanoid robots in logistics"), which the goals that post news read before choosing what to post. The info button shows the card's source (the goal, schedule or alert, linking to it), when it was posted and updated, its source links, and "Show less like this", which records the opposite preference and dismisses the card.

### Reading

ListAgentFeed(first, after, includeDismissed) pages newest first; MarkAgentFeedRead(feedItemIds); DismissAgentFeedItem; a subscription event when a card is posted, replaced or settled, so an open feed updates live. The count of unread cards shows on the Feed entry in the dashboard's sidebar and the iPhone's tab. Cards past expires_at (30 days for news and updates, never for an unanswered decision) drop out of the list.

### Notifications

A card of urgency now sends a phone notification; on the iPhone a decision's notification carries its choices as notification actions, so "That's me" can be tapped from the lock screen. Other cards are silent and only raise the unread count.

### Surfaces

Dashboard: a Feed page (web/src/pages/feed.tsx) at /feed, first in the sidebar, cards as described, with the drawer opening for Discuss; strings in web/src/i18n/en.ts, zh.ts, ja.ts. iPhone: a Feed tab (ios/TeaNode/Sources/FeedViews.swift), a TeaNodeKit model and calls, DemoServer data and shoot lines in .github/workflows/ios.yml, strings in Localizable.xcstrings in three languages. Android: the same screen, built by the Android session. Command line: teanode agent feed list, show, choose, like, dismiss, read, discuss. Docs: docs/subsystems/feed.md and docs/reference/command-line.md.


## Concrete Steps

From the repository root:

    nice make test PACKAGES=./internal/agent/... ./internal/db/... ./internal/api/...
    cd web && nice npx tsc --noEmit -p . && nice npx vitest run src/pages/feed
    nice make lint-ci

The iPhone app is built only by CI (.github/workflows/ios.yml); its screenshots feed-light, feed-dark, feed-largest-text and ipad-feed-light are looked at before merging.


## Validation and Acceptance

With a dev server running, in the main conversation: "post a decision to my feed: was the sign-in from a new laptop you? choices That's me and Secure my account". The feed shows the card with both buttons; the main conversation shows only the agent's short acknowledgement. Tapping That's me settles the card with no new turn. Repeating with Secure my account starts a turn in a run conversation and the card shows its outcome line. Posting twice with the same coalesce key leaves one card. A test in internal/agent covers each of those, and one covers an alert going to the feed instead of the main conversation.


## Idempotence and Recovery

The migration is additive with a reverse migration; back up production before applying it. Posting with a coalesce key is an upsert and safe to repeat. Choosing an already chosen card is refused with "already answered" and changes nothing.


## Progress

- [x] (2026-11-18) Research and this plan.
- [x] (2026-11-18) Decisions answered by the person (see the Decision Log).
- [ ] Table, model, database calls, tests.
- [ ] Feed tool, routing of alerts, speaking first, schedules, goals.
- [ ] GraphQL, subscription, CLI, parity tests.
- [ ] Dashboard page.
- [ ] iPhone tab, notification actions.
- [ ] Android (Android session).
- [ ] Docs, review rounds, deploy, use it for real.


## Surprises & Discoveries

None yet.


## Decision Log

- Decision: a decision card is backed by the existing durable card (models.AgentInteraction) rather than a new mechanism. Rationale: answering late, resuming the turn with the answer and allowing one approved call already work and are tested.
- Decision: the posting turn does not wait on its feed card. Rationale: the point is not to hold anything up; the answer arrives whenever the person gets to it.
- Decision (the person, 2026-11-18): everything said unasked goes to the feed by default: alerts, ideas, memory checks, trip notes, scheduled reports and goal updates. The main conversation keeps conversations the person starts and the agent's introduction. A per-agent setting can send any of them back.
- Decision (the person): Discuss opens the main conversation with the card attached, so the agent has the usual context and memory.
- Decision (the person): only urgent cards (feedUrgency now) notify the phone, with a decision's choices as notification actions; the rest only raise the unread count.
- Decision (the person): Feed is its own tab, first, on iPhone and Android, and first in the dashboard sidebar, with an unread count.
- Decision (the person, 2026-11-18): an action choice is a message sent in the main conversation on the person's behalf with the card referenced, so the approval and what the agent did about it are in the conversation the person reads; dismiss choices and coding session answers send nothing.
- Decision (the person): a coding session's question is a feed card too, its options as buttons that answer the session directly.
- Decision: the card layout follows a reference screenshot the person gave (kept out of the repository): picture at the left, bold headline, a few lines with links, an optional table of facts, choices as buttons, and a footer of heart, Discuss, date and info.


## Outcomes & Retrospective

Not started.
