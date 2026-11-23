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
    feed_stack_key        text, empty for a card that stands alone (see Stacks)
    feed_stack_title      text, the stack's title when it has one
    stack_position        integer, nullable: a card's place inside its stack, lowest on top
    feed_card_layout      text: text, list, weather, agenda (see Card layouts)
    layout_content        jsonb, the layout's structured content
    is_liked              boolean
    read_at, dismissed_at, created_at, updated_at, expires_at   timestamptz

Indexes: (user_id, dismissed_at, created_at desc) for the list; a unique index on (agent_id, coalesce_key) where coalesce_key is not null and dismissed_at is null, so that replacing is an upsert.

### The contract every surface builds against

GraphQL, in internal/api/v1api/apigraph/agent_feed.go, following agent_goal.go's pattern (a query interface, a mutation interface, argument structs, a view struct, each documented):

    query    ListAgentFeed(first: Int, after: String, includeDismissed: Boolean): AgentFeedPage
             AgentFeedPage { feedItems: [AgentFeedItemView], nextCursor: String (nullable), unreadCount: Int }
    query    GetAgentFeedItem(feedItemId: String): AgentFeedItemView
    query    ListAgentFeedStack(feedStackKey: String!, first: Int, after: String): AgentFeedPage   (one stack's cards, newest first)
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
      feedUrgency, isLiked, isRead, isDismissed, postedAt, updatedAt,
      feedStackKey, feedStackTitle, stackPosition (nullable), stackItemCount, stackUnreadCount,
      feedCardLayout (text, list, weather, agenda), layoutContent (JSON object as a string, empty for text; see Card layouts)   (the last two filled by ListAgentFeed on a stack's top card, 1 and 0 or 1 elsewhere)
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

### Stacks

Cards from the same source pile up (every story a news goal finds, every idea), so the feed shows them as stacks: one entry per stack, the newest card on top with "+N more from <source>", and tapping it opens the rest, newest first. Each card has a feed_stack_key (text, empty for a card that stands alone). By default a card posted by a goal or a schedule is stacked by its source (feedSourceKind and feedSourceId joined by a colon), ideas, memory checks and trip notes from speaking first stack as "speak_first:<reason>", and a turn's own post may name a stackKey; a card with action, dismiss or open choices, an alert card and a coding session question never stack, because each needs the person on its own. ListAgentFeed returns one entry per stack (the newest card of the stack, by updatedAt) with stackItemCount, how many cards the stack holds including that one (1 for a card alone), and stackUnreadCount; a new query ListAgentFeedStack(feedStackKey: String!, first: Int, after: String): AgentFeedPage lists one stack's cards newest first. Opening a stack marks its cards read as they are seen, as the main list does; unreadCount still counts cards. A stack is drawn as its top card with one or two card edges showing behind it.

### A brief: a stack per run, in order

A daily brief (for example every weekday at 7: today's plan, weather, five tech stories, five headlines, chat mentions, open review requests, mail worth reading) is a schedule whose run posts several cards that belong together. Three additions make that read well. First, a schedule run's cards stack per run, not per schedule: the default stack key of a schedule post is "schedule:<scheduleId>:<runId>", so each morning is its own stack and yesterday's sinks below today's. Second, a card may carry stack_position (integer, default 0); inside a stack, cards with a position sort by it ascending (the summary at 0 on top), and only cards without one sort newest first; the stack's top card in ListAgentFeed is the lowest position, or the newest when none has one. Third, a stack may carry a title (feed_stack_title, for example "Morning brief, Wed Nov 19"), taken from the first card that names one, shown instead of the source's name in "+N more from". A schedule also gains isNotifiedWhenPosted (default off): when on, the first card of each run sends one quiet notification "<schedule title> is ready" even though its cards are not urgent. The tool's post accepts stackKey, stackPosition and stackTitle. A brief's run uses only reading tools; anything it would do (archive newsletters, reply) is offered as an action choice on its own card, which stands outside the stack like every decision.

### Card layouts

A brief's sections read best each in its own shape, so a card has a feed_card_layout and, for the layouts other than text, structured content in layout_content (jsonb) that every app draws natively. The body stays filled in plain Markdown for every layout, so the command line, and an app that does not know a layout yet, still show the card sensibly.

    text      the default: headline, body, optional facts (what every card has)
    list      layout_content { listRows: [{ rowTitle, rowDetail, rowMeta, rowSymbol, rowUrl, rowRecordReference }] }, at most 10 rows:
              news stories (rowMeta the source), chat mentions (rowDetail the snippet, rowMeta the channel and time),
              review requests and assigned issues, mail worth reading (rowRecordReference opens the mail)
    weather   layout_content { placeName, currentTemperature, temperatureUnit, conditionText, conditionSymbol, highTemperature,
              lowTemperature, hourlyForecast: [{ forecastHour (ISO time), forecastTemperature, forecastSymbol, precipitationChancePercent }] (at most 12) }
    agenda    layout_content { agendaEntries: [{ startsAt, endsAt, agendaTitle, agendaEntryKind: meeting, focus, task, break,
              agendaDetail, agendaRecordReference }] } for a day plan, in time order

The tool's post takes cardLayout and layoutContent and checks layoutContent against the layout's shape, refusing a malformed one with a message that says which field is wrong. Links in rowUrl must be http or https.

### Pictures and videos

A card is easier to take in with a picture, so a card carries media (feed_media, jsonb, migration 0277): at most six entries, each one of

    photo     one of the person's own photos (photoId), shown from its large preview
    video     one of the person's own videos (photoId), its poster from the large preview, played from the video preview
    picture   the picture of a web page (pageUrl): the server reads the page, fetches its picture, scales it down and keeps
              it in the file store, the way link previews in a conversation are made, so the person's device never loads
              anything from the linked site

with an optional mediaCaption and an optional listRowIndex, which makes it the thumbnail of that row of a list card instead of a picture of the whole card.

The post takes media: [{ photoId | pageUrl, mediaCaption, listRowIndex }]. When a post gives none, a text card with a source link gets the picture of its first link, and a list card's rows with a rowUrl get theirs, so news cards and a brief's top stories have pictures without the model doing anything. A page that has no picture, or cannot be read in time (15 seconds for all of a card's pages, five at once), leaves the card without one; it is never an error.

A page is fetched only when its address appears word for word in what a tool answered in the conversation posting the card (or the card is posted by the person, from the CLI or the dashboard). An address the model composed, which could carry what it read in its query, is never fetched; such a page simply has no picture. A photo must be one the person may see.

Each entry is answered as AgentFeedMediaView { mediaKey, feedMediaKind (photo, video, picture), imageUrl, videoUrl (video only), linkUrl (picture only: the page), siteName, mediaCaption, widthPixels, heightPixels, listRowIndex }. imageUrl and videoUrl are server paths that work for the signed-in person only: a picture is served from /api/v1/feed/items/{feedItemId}/media/{mediaKey}, a photo or video from the photo preview paths. A picture's file goes when its card is replaced without it.

How the apps draw it:

- One picture or video: full width at the top of the card, 16:9 cropped to fill (a portrait photo 4:5), rounded with the card; a video has a play button and plays in place (the person's own) or opens its page (a page's picture). Two to four: a grid; more: a row that scrolls sideways. The caption under it in secondary text.
- A list row with a thumbnail shows it at the row's leading edge, square, rounded.
- The site name and the source's symbol sit in a small header line above the headline, the way a news app labels a story.
- The weather card is drawn on a gradient chosen by its condition (clear, cloudy, rain, snow, night), with white text; the agenda card's entries hang off a time rail with a colored dot per kind.

### A stack is a deck

A stack is shown as its top card with the next two peeking out below it, each a little narrower and lower, so it reads as a deck with a count. Tapping it fans the deck out in place: the cards slide down out of the deck into a column, one after another, with the rest of the feed moving down to make room. A "Show less" control at the top of the open stack (and tapping the stack's title) gathers them back: the cards slide up into the deck and the peeking edges return. The opened stack's own screen stays for a notification that opens a stack. The motion is a spring of about 0.35 seconds and is skipped when the person asks for reduced motion.

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
- [ ] Pictures and videos on cards (0277), richer layouts, a stack drawn as a deck that fans out and gathers back (the person, 2026-11-19), on the server, dashboard, iPhone and Android.


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
- Decision (the person, 2026-11-18): cards from one source are coalesced into a stack that opens to show the rest; decisions, alerts and coding questions never stack.
- Decision (the person, 2026-11-19): coding session questions do stack after all, one stack per day of the person's, since a day of sessions filled the feed with them; the cards already posted were moved into their day's stack by migration 0278.
- Decision (2026-11-18, from the person's morning brief example): a schedule's cards stack per run, may be ordered inside the stack and titled, and a schedule may send one quiet "is ready" notification per run.
- Decision (the person, 2026-11-18): a brief needs cards of different designs; four layouts (text, list, weather, agenda) with structured content, each also carrying a plain body.
- Decision: the card layout follows a reference screenshot the person gave (kept out of the repository): picture at the left, bold headline, a few lines with links, an optional table of facts, choices as buttons, and a footer of heart, Discuss, date and info.

- Decision (the person, 2026-11-19): cards should not be mostly text: photos and videos, more visual designs, and a stack that fans out of its deck and gathers back into it.
- Decision: a page's picture is fetched by the server and kept, as link previews are, and only for an address a tool answered with, so a card cannot be used to send what the agent read to a site of its choosing.

## Outcomes & Retrospective

Not started.
