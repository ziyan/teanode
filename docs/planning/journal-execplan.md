# Journal: one page per day, written by the person and the agent together

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

TeaNode already holds most of what happens in a person's day: the coding sessions it follows, the conversations with the agent and in circle chats, the mail that came in, the documents and work chat it reads from the person's sources, meeting recordings, photos, the places the phone visited, calendar events, bank transactions, Apple Health data, shopping lists, and the memory pages it writes about all of it. Each lives on its own page. Nothing puts one day back together.

After this plan, every day has a **journal day**: one page that links (never copies) everything that happened that day, with three things only a journal has. The person's **words** (what they want to remember, in their own voice). The day's **mood** (one of five levels and a few words) and **weather**. And the agent's **narrative**: a few paragraphs and a one-line headline the agent writes during its background memory runs ("dreams"), on top of those links, after choosing which mail, documents and work messages mattered and saying why.

The person reads the journal as a **timeline**, day by day, and zooms out: a **week** (seven days side by side with the week's narrative), a **month** (a calendar of days, each with its chosen photo, mood and high temperature), a **year** (twelve small calendars colored by mood, the year's narrative and its chapters). Each period has its own narrative, written from the narratives of the days in it, and room for the person's own "looking back".

To see it working at the end: on a development server with a week of invented data (two coding sessions, a circle chat, mail, a recording, photos with times and places, visits, calendar events, transactions, a shopping trip, Health day summaries), the dashboard's **Journal** page lists the days of the week; opening Thursday shows every one of those things in the order they happened, each linking to its own page. The person writes a paragraph and picks "Good". After a dream runs (`teanode dream run`), Thursday has a headline and a narrative whose every mention opens a real record, a list of the mail and work messages the agent chose with a reason for each, and the count of the ones it left out. The week page has a narrative written from the seven days. `teanode journal day 2026-10-22` prints the same day; the agent, asked "what did I do last Thursday?", answers from the journal; the iPhone app shows the same timeline and day.

## Progress

- [x] (2026-10-26) Research: every source a day can link to, how each is dated and listed, the dreams, the record links, the design rules of the dashboard and the iPhone app. Summarized in Context and Orientation and in Surprises.
- [x] (2026-10-26) Design mockups of the day, the timeline, the week, the month, the year, and the iPhone timeline and day, shown to the maintainer.
- [x] (2026-10-26) This plan written, with the open questions in the Decision Log.
- [x] (2026-10-26) The maintainer answered the open questions (see the Decision Log).
- [ ] Milestone 1: storage, the new record kinds, and reading a day and a period on the server, with the command line.
- [ ] Milestone 2: the person's writing (words, mood, looking back), the day facts and weather steps, and the agent's `journal` tool.
- [ ] Milestone 3: the dream's journal phase: day narratives, the chosen links, weeks, months and years.
- [ ] Milestone 4: the dashboard's Journal pages.
- [ ] Milestone 5: the iPhone app's Journal.
- [ ] Milestone 6: documentation, getting started, the walk through on a development server, review rounds until a round finds nothing.

## Surprises & Discoveries

- Observation: the word "journal" is taken twice already. `models.DocumentJournal` (`internal/models/knowledge.go`) is the kind of the documents the location day files and recording passages use, and the source type `journal` (`internal/sources/testdata/registry/journal.md`) reads a person's own dated notes from a folder on their computer.
  Evidence: `internal/computer/scan_journal.go` dates a note at 12:00 UTC of its file's date.
  Consequence: the feature's tables and Go names say `journal_day` and `journal_period`, never a bare "journal entry", and the plan asks the maintainer whether notes from the `journal` source should show as the person's words on their day (see the Decision Log).

- Observation: much of what a day needs has no date range query. Agent messages, circle chat messages, recordings, notes, reminders, goals, alerts and past shopping trips are listed by cursor or by state, not by time.
  Evidence: `ListAgentMessages` (`internal/db/database_insight.go`), `ListChatConversationMessages` (`internal/db/database_chat.go`), `ListRecordings` (`internal/db/database_recording.go`), `ListNotes` (`internal/api/v1api/apigraph/note.go`).
  Consequence: Milestone 1 adds one "between" operation per source, each using the source's existing index on its time column or a new one.

- Observation: record links already do what "structural links" need. A memory page links to a record as a kind and an id (`agent_record_link`, migration 0176), and `recordlink.ResolveRecord` (`internal/recordlink/recordlink.go`) checks the reader's access every time the link is read and returns the title, the time and the dashboard path. A record the person can no longer see is not shown, and a deleted one disappears.
  Evidence: `models.AgentRecordKind` in `internal/models/graph_record_link.go` names eight kinds: contact, mail, calendar_event, reminder, conversation_message, finance_transaction, finance_receipt, recording_passage.
  Consequence: the journal reuses record kinds and the resolver, and adds the kinds it lacks (photo, place_visit, agent_document, chat_message, recording, calendar_occurrence, health_day, shopping_trip, note, idea, goal).

- Observation: dreams already write month and year pages. The dream's timeline phase (`internal/agent/dream_timeline.go`) turns `Agent.Digest` (`internal/agent/digest.go`, model-free: commits, chats the person took part in, their journal notes, places, facts) into memory pages under `time/YYYY/MM`. Nothing works per day.
  Consequence: two writers of a month's story would disagree. See the Decision Log: the journal becomes the one writer of period narratives, and the timeline phase's pages are written from the journal's months.

- Observation: Health is kept out of dreams by design (`docs/planning/health-execplan.md`), and the agent reads it only when the person turned on **Let the agent read Health data** (`models.HealthSettings.CanAgentReadHealth`, `internal/models/health.go`).
  Consequence: the dashboard and the iPhone show Health on a journal day regardless (it is the person's own page), but the dream's narrative is given Health numbers only when that switch is on.

- Observation: Apple Health's State of Mind is already copied (`stateOfMind` in `internal/health/types.go`). A mood logged on the phone or Watch can fill in the day's mood, marked as coming from Apple Health.

- Observation: there is no weather history anywhere. The only weather is the registry skill `weather` (Nominatim and the US National Weather Service), today's forecast for the United States only.
  Consequence: past days need a provider with a historical archive. Open-Meteo has one, worldwide, with no key; it needs the coordinates (see the Decision Log).

- Observation: the iPhone app does not send `X-Timezone` (only `X-Language`, `TeaNodeKit/Connection.swift`); the dashboard does (`web/src/api.ts`), and the server learns the person's zone from it (`user.timezone`, `timezone_mode`). A day's edges depend on the zone, so the app must send it too (Milestone 5).

- Observation: herdr panes are not stored, and coding sessions have no stored summary. A coding session is an `agent_document` of kind `chat` with metadata `assistant` (claude-code or codex), `directory` and `channel` (the title), one per stretch of work that ends at a 30 minute silence.
  Consequence: a day lists coding sessions from those documents, and the dream writes the one-line description of each in its link reason.

## Decision Log

- Decision: a journal day links, it does not copy. What happened (photos, visits, events, transactions, messages) is read live from each source by the day's time range when the day is opened. Only three kinds of thing are stored: what only the journal has (the words, the mood, the weather, the narrative and headline), the agent's choices (which mail, documents and work messages mattered, with a reason) and the person's pins, and small numbers per day for the timeline (counts, steps, sleep, money spent).
  Rationale: the maintainer asked for structural links the agent works on top of. A copy goes stale when a photo is deleted or a transaction is corrected; a link read through `recordlink.ResolveRecord` cannot, and it checks access each time.
  Date/Author: 2026-10-26, design.

- Decision: the person's words are theirs. The agent never edits them on its own. It may add to them only when the person asks it to in a turn the person started ("add to today's journal: ..."), marked as added at the person's request, and it touches them up only when the person asks (below), keeping the original. The narrative is the agent's, shown apart and labeled as written by TeaNode, and the person can delete it or ask for it to be written again, but not edit it.
  Rationale: a diary in which the machine changes your sentences is not your diary. Keeping the two apart also tells the reader whose words they are reading.
  Date/Author: 2026-10-26, design.

- Decision: the mood is the person's. Five levels (Awful, Low, Okay, Good, Great) and optional words. When Apple Health has a State of Mind for the day and the person has not chosen, it is shown as the day's mood with "From Apple Health"; choosing replaces it. The agent never sets or guesses a mood, and may ask (see the open question on the evening question).
  Rationale: a mood inferred by a model from someone's mail would be presumptuous and often wrong.
  Date/Author: 2026-10-26, design.

- Decision: a day is the person's calendar day in their time zone at the time the day is first written (`journal_day.time_zone`), so a later change of zone does not move what was in it. Weeks start on the day the person's locale starts them (Monday in most of the world, Sunday in the United States), the same rule the calendar page uses.
  Rationale: records are stored with instants; the person thinks in local days. Freezing the zone per day keeps old days stable after travel or a move.
  Date/Author: 2026-10-26, design.

- Decision: the dream writes the narratives, hierarchically. A day is written from its own records (as a list of kind, id, time and a short description each, never whole documents), the person's words, mood and weather. A week is written from its days' narratives and choices, a month from its weeks and days, a year from its months. The model may only mention records that are in its input: every reference it returns is checked against the input set, and a narrative that names anything else is refused and written again once, then left unwritten with the reason recorded.
  Rationale: zooming out is summarizing summaries; feeding a year of raw records to a model is neither affordable nor better. Checking references is what makes "every mention opens a real thing" true.
  Date/Author: 2026-10-26, design.

- Decision: ingested documents and work chat are chosen, not dumped. The candidates for a day are the documents that happened that day and involve the person (a chat unit they posted in or were mentioned in, a document they edited or were named in, a mail thread they replied to). The dream chooses the ones that matter and says why in a few words ("you were asked to decide", "puts something on your calendar", "you wrote most of it"); the others are counted ("38 more work messages were left out as routine") and listed live under Show them.
  Rationale: the maintainer's words: the ingested documents should help complete the day, but that needs the model's judgment, which belongs in dreaming.
  Date/Author: 2026-10-26, maintainer.

- Decision: a narrative goes stale, and is written again. Each day keeps a digest of what its narrative was written from (the record ids and their modified times, the words, the mood). When late data arrives (photos uploaded the next week, a recording transcribed later) or the person edits their words, the digest no longer matches and the next dream writes the day again, then the periods above it. Today is never written by the dream; the timeline shows a model-free "so far" line built from the day's numbers.
  Rationale: a journal that froze before the photos arrived would be wrong forever.
  Date/Author: 2026-10-26, design.

- Decision: the journal is the one writer of period stories. The dream's timeline phase stops writing `time/YYYY/MM` pages from `Agent.Digest` on its own, and writes them from the journal's month and year narratives instead (the memory page links to the journal period).
  Rationale: one name per idea, one writer per fact. Two model-written stories of the same month would disagree.
  Date/Author: 2026-10-26, design; agreed by the maintainer.

- Decision: the words. **Journal** is the feature, the page and the screen. A **journal day** is one day of it; a **journal period** is a week, a month or a year. The person's text is their **words**; their text on a period is **looking back**. The agent's text is the **narrative** with a one-line **headline**. What the agent chose for a day is a **choice** with a **reason**; what the person added by hand is a **pin**. The day's numbers are its **facts**. Removing everything is **erase**, as for location history and Health.
  Rationale: one word per idea, avoiding "entry" (Passwords uses "item") and the existing `journal` document kind and source type.
  Date/Author: 2026-10-26, design.

- Decision: the maintainer's answers to the first questions (2026-10-26). Weather comes from Open-Meteo, on by default with a switch to stop it, at the rounded coordinates of the day's main place, and a day with no location has none. TeaNode asks "How was today?" each evening, on by default at 9 PM local, easy to stop. The model sees the whole day, the person's words included, and the page says so in one line. Notes from the `journal` source show on their day, read-only, marked "From your notes", when a person has that source. No sharing in this plan. Past days are backfilled (see the backfill decision below). The journal becomes the one writer of the month and year stories memory keeps under `time/YYYY/MM`.
  Rationale: the maintainer's answers.
  Date/Author: 2026-10-26, maintainer.

- Decision: any past day can be changed at any time: words, mood, looking back, pins, hidden choices. A change makes that day stale, and the next dream writes it again and then each period above it (its week, month, year), in that order, before any backfill. Backfill never overwrites what the person wrote, and never writes a day the person changed since its last narrative without reading the change.
  Rationale: the maintainer: "keep in mind I might go back and change a past day".
  Date/Author: 2026-10-26, maintainer.

- Decision: touched-up words. The person may ask TeaNode to touch up a day's words (or a period's looking back), optionally saying how ("shorter", "fix the grammar", "warmer"). From then on that day keeps two texts: the **original** the person wrote, and the **touched-up** version TeaNode made from it. The page shows the touched-up version, marked as touched up, with the original one tap away; the person reads and edits the original there, and saving it makes the touched-up version again from the new original, with the same instruction unless they change it. The person can also turn touching up off for that day (the original shows again and the touched-up text is deleted), or ask for another version. The touched-up text is never edited by hand: changes go into the original. The dream reads the original, never the touched-up text, and never touches anything up on its own. From a conversation: "touch up today's journal"; on the iPhone, the write sheet's Touch up button; on the command line, `teanode journal touch-up DATE [--how ...]` and `teanode journal touch-up DATE --off`.
  Rationale: the maintainer: "sometimes I want AI-assisted rewording, show the touched up diary, and allow me to see and edit the original text to re-generate". Keeping the original as the one text the person edits keeps the words theirs; the touched-up text is always made from it, so it cannot drift from what they wrote.
  Date/Author: 2026-10-26, maintainer.

- Decision: correcting a story. Under a narrative, "Something wrong?" takes a short correction in the person's words ("it was Sam at lunch, not a colleague"). It is stored with the day (or period) and given to every later writing of it as a fact the narrative must follow; the narrative is written again at once, and the periods above it at the next dream. Corrections are listed under the story, and the person can remove one. The narrative itself is never edited by hand.
  Rationale: the maintainer chose "tell it what's wrong" over editing the story. A correction survives rewrites; a hand edit would be lost at the next one, or would freeze the day.
  Date/Author: 2026-10-26, maintainer.

- Decision: no per-day private switch. Every day is read by the model, as the maintainer said. The one line on the page saying so stays.
  Rationale: the maintainer: "llm should be able to see the day", and "no switch".
  Date/Author: 2026-10-26, maintainer.

- Decision: backfill depth. Facts (the numbers) and links are built for every past day the sources reach, model-free. Narratives are written for the last three months only, newest first, a few days per dream run; a week, month or year gets a narrative only when it has written days. Older days show their numbers, links, weather and the person's words, and the person can ask for a narrative of any one day from its page.
  Rationale: the maintainer chose the last three months, to keep the model's cost down.
  Date/Author: 2026-10-26, maintainer.

- Decision: on the iPhone, Journal sits in More under "Every day", and the person can make it one of their tabs in Edit.
  Date/Author: 2026-10-26, maintainer.

- Decision: a narrative names a record inside its sentence, with words of its own: `[[mail:id|Lena's question about the dock]]`. The page shows those words, linked to the record. References used as citations (side by side, or alone between sentences) are refused once with the reason.
  Rationale: shown as the record's title in place, a citation read as a run of bold titles between sentences, and a title made an awkward sentence ("lunch at Visit to the noodle place"). Drawn as a bordered chip, a long title became a boxed paragraph of its own.
  Date/Author: 2026-10-26, agent.

- Decision: a day's time worked is the time the person was at their coding sessions, read from their own posts: each post counts five minutes and runs on to their next post when that came within half an hour. The assistant's posts set when a session ended, not how long the person worked.
  Rationale: counted from each stretch's first post to its last, an assistant left working on its own, overnight or through a day away from the desk, made that whole time work, close to 24 hours a day. The person's posts are told apart by author, the names they go by in chat.
  Date/Author: 2026-10-26, agent.

- Decision: the agent reads a day only from what the person gave it: the mailboxes granted to it (their mail and notes), the calendars given to it, the chats the person lets it remember, and the transactions of the finance sources that are on. That holds for its dreams and jobs, its tool and a command it runs as the person, in the records a narrative mentions and the records it may pin. The person's own page reads all of it, and the stored numbers are counted from all of it.
  Rationale: review found the dream reading every mailbox, calendar and chat, so a person who gave the agent only a work mailbox still had private senders and subjects in the prompt and the narrative.
  Date/Author: 2026-10-27, review.

- Decision: erasing Health, or where the person went, also forgets what was written from it: the narratives, headlines and the agent's choices of the days that had any of it, the narratives of the weeks, months and years around them, and the memory pages made of those months. Removing a month's narrative, or erasing the journal, removes its memory page. Dreams write the recent ones again from what is left. The person's words, moods, corrections and pins stay.
  Date/Author: 2026-10-27, review.

- Decision: the journal tool reads the journal only in a run the person started, as it writes. A mail or a schedule could otherwise steer the agent into telling somebody else what the person wrote for themselves. A command the agent runs as the person may not remove a correction, a pin or a narrative, or stop a touch-up, as it may not erase the journal.
  Date/Author: 2026-10-27, review.

- Decision: the hourly refresh runs in the background beside the tick, and the weather service is asked between two transactions, never inside one; an answer is kept only while the day still has no weather and the same main place.
  Rationale: a slow weather service held a transaction open for minutes and kept the tick from claiming jobs for everybody.
  Date/Author: 2026-10-27, review.

## Outcomes & Retrospective

Nothing built yet.

## Context and Orientation

TeaNode is one Go server (`cmd/`, `internal/`) with a PostgreSQL database, a React dashboard built into it (`web/`), an iPhone app (`ios/`), and a command line program (`internal/cmd/`) that reaches the same GraphQL API the dashboard uses. A person's **agent** is the assistant that answers them; its **tools** (`internal/agent/tools/<area>/`) are what the model can call. **Dreams** are the agent's background runs, queued by `queueDreaming` (`internal/agent/dream_schedule.go`) when the person has been quiet for 30 minutes, at most every six hours, and run by `runDream` (`internal/agent/dream.go`), which calls one phase after another (`self.dreamIdeas`, `self.dreamDigest`, `self.dreamTimeline`, `self.dreamRecordLinks` and so on, `internal/agent/dream.go` around line 168), each spending from a shared `budget`.

**Record links.** A record is a row the person owns elsewhere in TeaNode (a mail, an event, a transaction). `models.AgentRecordKind` (`internal/models/graph_record_link.go`) names the kinds; a link is a kind and an id. `recordlink.ResolveRecord` (`internal/recordlink/recordlink.go`) turns a link into a title, a time and a dashboard path, checking that the reader may see it. The journal adds kinds and uses the same resolver.

**The sources a day reads**, with the operation that lists them for a time range (existing unless marked new):

- Coding sessions: `agent_document` rows of kind `chat` whose metadata `assistant` is claude-code or codex; `ListAgentDocumentsBetween` (`internal/db/database_knowledge.go`). Placed at `happened_at`.
- Other ingested documents (work chat such as a team chat archive, wiki pages, shared drive documents, sent mail, journal notes): `agent_document` by `happened_at`, same operation, with the source in `agent_source`.
- Conversations with the agent: `agent_message.created_at`. New: `ListAgentMessagesBetween(agentId, from, until)`, grouped by conversation.
- Circle and group chats: `chat_message.created_at` in chats the person is a member of. New: `ListChatMessagesBetween(userId, from, until)`, grouped by chat.
- Mail: `mail.received_at`; `ListItems` with `Since` and `Before` (`internal/db/database_mailbox.go`); importance from `mail_insight` (priority, needs reply), `agent_alert`, `agent_follow_up`. Sent mail from the folder of kind `sent`.
- Recordings: `recording.recorded_at` plus `timeline_duration_ms`. New: `ListRecordingsBetween(userId, from, until)`.
- Photos: `photo.taken_at` and `taken_at_offset_minutes` (the clock where it was taken); `ListPhotos` with `TakenAfter` and `TakenBefore` (`internal/db/database_photo.go`).
- Places: `place_visit` from `arrived_at` to `departed_at`; `ListPlaceVisits` with `From` and `Until` (`internal/db/database_location.go`).
- Calendar: `calendar_occurrence`; `ListOccurrences(calendarId, from, until)` per calendar (`internal/db/database_calendar.go`). All-day events are dates and do not move with the zone.
- Reminders: calendar objects in a reminders calendar, by due and done time. New: a range filter on `ListReminders`.
- Money: `agent_finance_transaction.posted_on` (a date) and `transacted_at` (a time, when known); `ListFinanceTransactions`; receipts by `purchased_on`.
- Health: `health_day_summary` by `summary_day` (the phone's calendar day) per type; `ListHealthPeriods`; workouts as samples of type `workout`.
- Shopping: `shopping_trip.started_at` to `finished_at` and the items bought, once the shopping lists branch (`feat/shopping-lists`, pull request 661) has merged. New: `ListShoppingTripsBetween`.
- Memory: facts with `happened_at` that day (`ListAgentFactsBetween`, `internal/db/database_graph.go`) and the pages they belong to.
- Notes, ideas, goals: by created or modified time. New range filters.

**The person's zone and day.** `user.timezone` (migration 0031) and `tools.Location(user)` (`internal/agent/tools/timeparse.go`); `DayStart(owner, now)` in `internal/agent/policy.go`.

**Permissions.** Each source keeps its own gate (`PermissionMailRead`, `PermissionPhotosUse`, `PermissionLocationUse`, `PermissionCalendarUse`, `PermissionHealthUse`, `PermissionAgentUse` and, on the shopping branch, `PermissionShoppingUse`). A journal day shows a section only to a person holding its permission. The journal itself is gated by a new `PermissionJournalUse`, granted with the default role like the others.

**The dashboard** (`web/src/`): pages under `web/src/pages/`, registered with `lazyPage` in `web/src/app.tsx`, the rail in `web/src/components/sidebar.tsx`, strings in `web/src/i18n/en.ts`, `zh.ts`, `ja.ts`, design rules in `docs/coding/frontend-design.md` (cards, `SettingsSection`, `Tabs`, `.segmented`, tokens in `web/src/tokens.css`; the accent is near black, green "leaf" is for decoration and charts; charts are hand-drawn SVG in `web/src/components/seriesChart.tsx`; the map is MapLibre in `web/src/components/locationMap.tsx`). **The iPhone app** (`ios/TeaNode/Sources/`): SwiftUI, rules in `ios/DESIGN.md`, sections in `AppShell.swift` and `MainTabs.swift`, demo data in `DemoServer.swift`, screenshots listed in `.github/workflows/ios.yml`.

## Plan of Work

### Milestone 1: storage, record kinds, reading a day

Add a migration with the next free number (0261 if the shopping branch keeps 0260; check `internal/db/migrations/` on `main` and the open pull requests just before opening, and announce it to the other sessions), with its `.reverse.sql`:

`journal_day`: `id` (UUID), `user_id`, `day` (date), `time_zone` (text, the zone the day's edges are cut in), `words` (text, Markdown: the original the person wrote), `words_modified_at`, `touched_up_words` (text, null when touching up is off), `touch_up_how` (text, the person's instruction), `touched_up_from_digest` (text, a digest of the original it was made from), `mood_level` (smallint 1 to 5, null for none), `mood_words` (text), `mood_source` (`person` or `health`), `weather` (JSON: `conditionCode`, `highCelsius`, `lowCelsius`, `precipitationMM`, `placeName`, `fetchedAt`; null when not fetched), `facts` (JSON, below), `facts_computed_at`, `headline` (text), `narrative` (text), `narrative_written_at`, `narrative_input_digest` (text), `narrative_failure` (text, why the last attempt was refused), `narrative_corrections` (JSON: a list of `{correctionText, createdAt}`), `is_narrative_requested` (a day older than the backfill depth whose narrative the person asked for), `cover_photo_id` (null), `created_at`, `modified_at`. Unique on (`user_id`, `day`).

`journal_period`: `id`, `user_id`, `period_kind` (`week`, `month`, `year`), `period_start` (date), `looking_back` (the person's original text), `looking_back_modified_at`, `touched_up_looking_back`, `touch_up_how`, `touched_up_from_digest`, `headline`, `narrative`, `narrative_written_at`, `narrative_input_digest`, `narrative_failure`, `narrative_corrections`, `chapters` (JSON, years only: a list of `{fromDay, untilDay, title, detail}`), `created_at`, `modified_at`. Unique on (`user_id`, `period_kind`, `period_start`).

`journal_link`: `id`, `journal_day_id` or `journal_period_id` (exactly one, a CHECK), `record_kind`, `record_id`, `link_source` (`agent` for a choice, `person` for a pin), `link_reason` (text), `link_rank` (integer, order on the page), `is_hidden` (the person removed an agent choice; kept so the next dream does not choose it again), `created_at`. Unique on (owner, `record_kind`, `record_id`).

The `facts` JSON holds only numbers and ids, never content: `photoCount`, `placeCount`, `visitPlaceIds`, `stepCount`, `sleepMinutes`, `workMinutes`, `codingSessionCount`, `meetingCount`, `meetingMinutes`, `mailReceivedCount`, `spentCents` and `currency`, `recordingCount`, `conversationCount`, `photoCandidateIds` (up to twelve, for the cover), `mainPlace` (`placeId`, rounded latitude and longitude, for the weather). Health numbers are included only for a person with Health; they are shown on the page, and given to the dream only under the agent switch.

Add the record kinds in `internal/models/graph_record_link.go` and their resolvers in `internal/recordlink/recordlink.go`, each checking its source's permission and ownership like the existing ones: `photo` (id), `place_visit` (id), `agent_document` (id, covering coding sessions and ingested documents), `chat_message` (id, member check), `recording` (id; `recording_passage` stays for a span), `calendar_occurrence` (`eventId@startInstant`, so one occurrence of a repeating event can be named), `health_day` (`type@YYYY-MM-DD`, the person's own), `shopping_trip` (id, after the shopping branch merges), `note`, `idea`, `goal`. A memory page may link to the new kinds too, since they share the resolver.

Add the "between" operations listed in Context and Orientation, each with a test and, where the time column has no index for the person, an index in the same migration.

Add `internal/journal/` (a package with no model calls): `DayRange(user, day) (from, until time.Time)` cutting a day in its zone; `ReadDay(ctx, database, user, day)` assembling a `models.JournalDayView` from the sources the person may read, each section capped (for example 200 photos, 100 messages) with its total; `Moments(view)` ordering everything into the day's sequence (the "through the day" list); `ReadPeriod(ctx, database, user, kind, start)` returning the period row and the headline, mood, high temperature, cover and facts of each day in it, plus the period's sums. Expose them in GraphQL (`internal/api/v1api/apigraph/journal.go`): `ReadJournalDay(day)`, `ListJournalDays(from, until, first, offset)` (rows and facts only, for the timeline), `ReadJournalPeriod(periodKind, periodStart)`, `SearchJournal(query, first, offset)` (words, looking back, headlines and narratives). Add `teanode journal` (list), `teanode journal day [DATE]`, `teanode journal week|month|year [DATE]` and `teanode journal search QUERY` to `internal/cmd/`, with `--json`.

At the end of Milestone 1, `teanode journal day 2026-10-22 --json` on a development server with invented data prints every section of that day, and a test proves that a photo taken at 23:30 local belongs to its day and not the next one in UTC.

### Milestone 2: writing, facts, weather, the tool

GraphQL mutations: `SetJournalWords(day, words, baseModifiedAt)` (the original; refused if someone saved in between, as notes are; when touching up is on for the day, saving makes the touched-up text again from the new original), `SetJournalMood(day, moodLevel, moodWords)`, `SetJournalLookingBack(periodKind, periodStart, text)`, `PinJournalRecord(day, record)`, `HideJournalChoice(linkId)`, `TouchUpJournalWords(day | period, how)` (makes or remakes the touched-up text from the original; an empty `how` keeps the last instruction), `StopJournalTouchUp(day | period)`, `CorrectJournalNarrative(day | period, correctionText)` (stores it and writes the narrative again at once), `RemoveJournalCorrection(day | period, correctionIndex)`, `RequestJournalNarrative(day)` (for a day older than the backfill depth), `RewriteJournalNarrative(day | period)` (marks it stale), `DeleteJournalNarrative(day | period)`, `EraseJournal()` (every row of the three tables; the records themselves untouched), `SetJournalSettings` (weather and the evening question). Each with a command: `teanode journal write [DATE] (--text | --file | -) [--append]`, `teanode journal mood [DATE] good [--words ...]`, `teanode journal look-back week|month|year [DATE] ...`, `teanode journal pin DATE kind:id`, `teanode journal touch-up DATE [--how ...] | --off`, `teanode journal correct DATE "..."`, `teanode journal rewrite DATE`, `teanode journal erase`.

A model-free step in the agent's worker tick, like `queueLocationJournals` (`internal/agent/location_journal.go`), runs hourly: it computes `facts` for today and for any finished day whose facts are older than its records, and for each finished day without weather (and the setting on) fetches it once from Open-Meteo's archive (or forecast, for today) at the rounded coordinates of the main place, storing the result. It fills Health's State of Mind into the mood when the person has not chosen.

The agent's tool `internal/agent/tools/journal/`: `read_day`, `read_period`, `list_days`, `search`, and, only in a turn the person started, `add_words` (appends to the original, marked), `touch_up`, `correct`, `set_mood`, and `pin` ("put this in today's journal"). Long answers page with `offset` and `next_offset`. Reading follows the person's permissions; Health numbers only under the agent switch.

### Milestone 3: the dream writes

Add `self.dreamJournal(ctx, run, record, budget)` to the phase list in `internal/agent/dream.go`, before `dreamTimeline`. In one run, while the budget lasts: for each finished day without a narrative or with a stale digest (newest first, never today, within the last three months or asked for by the person; with the original words, never the touched-up text, and every stored correction given as a fact to follow), assemble the day's input from `journal.ReadDay` as a numbered list of `kind:id`, time and a one-line description per record (the coding session's title and directory, the mail's sender and subject with its insight, the chat unit's channel and first lines, the photo's place and time), the person's words, mood and weather, and the candidate documents; ask `dreamThink` for a JSON answer `{headline, narrative, choices: [{record, reason}], coverPhotoId}` in which the narrative refers to records as `[[kind:id|the words its sentence shows]]`, inside its sentences; check every reference and choice against the input; store. Then the periods whose days changed: a week once its last day is written, a month, a year, each from the narratives below it.

The dashboard and the iPhone render a mention as its words in bold, linked to the record (the record's title when it has no words, as in `[[kind:id]]`); a reference the reader can no longer see renders as plain words, without the words the story gave it.

Change `dreamTimeline` (`internal/agent/dream_timeline.go`) to write the `time/YYYY/MM` memory pages from the journal's month narrative and to link them to the period, if the maintainer agrees (question 7).

Add counters (`journal_days_written`, `journal_periods_written`, `journal_refused`) to `agent_dream` in the same migration as Milestone 1 if it has not shipped, or in a new one.

### Milestone 4: the dashboard

Pages in `web/src/pages/journal/`: the timeline at `/journal` (days newest first in weeks, each week with its headline, a month calendar colored by mood beside it to jump, "On this day" a year ago, the week so far; `?zoom=days|weeks|months|years` lists periods instead; pages, not "load more"), the day at `/journal/YYYY-MM-DD`, the week at `/journal/week/YYYY-MM-DD` (its first day), the month at `/journal/month/YYYY-MM`, the year at `/journal/year/YYYY`. A segmented Day, Week, Month, Year control on every period page moves to the period containing the same date; previous, today and next step by the period. The rail gets **Journal** after Chats in both rails. Strings in three languages. The layout follows the mockups: the day's mood and weather, a row of fact tiles, the person's words on a paper-toned card in a serif face, the narrative card, Work, Mail that mattered, Photos, Recordings, Conversations, Money, Shopping, Knowledge on the left; the map with the day's places, "Through the day", Health and Calendar on the right; stacked on a phone.

### Milestone 5: the iPhone app

`AppSection.journal` in the More list's "Every day" group, choosable as a tab. The timeline as a list in weeks with a `toolbarTitleMenu` for Days, Weeks, Months, Years; today's card with the five moods and one prominent "Write about today"; the day screen (mood, weather, words with Edit, the narrative, photos, through the day, places, chosen, Health, Money, Ask about this day); the period screens; the write sheet. The app sends `X-Timezone`. Demo data in `DemoServer.swift`, and `shoot` lines for the timeline, a day, a week, a month and the write sheet in `.github/workflows/ios.yml`; strings in `Localizable.xcstrings`.

### Milestone 6: docs and review

`docs/subsystems/journal.md`, the command line reference, a paragraph in `docs/getting-started.md`, a decision record `docs/decisions/<date>-a-journal-day-links-and-never-copies.md`; then the walk through below, the UX review of every page at 1400 and 390 pixels, light and dark, and review rounds until a round finds nothing.

## Concrete Steps

Work on a branch from `main` in a checkout of your own:

    git switch -c feat/journal origin/main

Run single packages while working, at low priority (the maintainer's machine is shared):

    nice -n 19 go test ./internal/journal/ ./internal/recordlink/
    cd web && nice -n 19 npx vitest run src/pages/journal

Leave the full suites and `-race` to CI. Before pushing: `make format`, `make lint-ci` with `set -o pipefail`, `npx tsc` in `web/`.

## Validation and Acceptance

On a development server (`docs/reference/local-development.md`) seed a person with a week of invented data, then:

- `teanode journal day <Thursday> --json` lists two coding sessions, the circle chat, the mail, the recording, the photos, the visits, the events, the transactions, the shopping trip and the Health day, and "through the day" orders them by time. A photo taken at 23:30 local time is on Thursday, not Friday.
- On the dashboard, the Journal page shows the week; Thursday's page shows the same sections; each row opens its record. A person without Photos permission sees no Photos section.
- `teanode journal write <Thursday> --text "An invented paragraph."` and `teanode journal mood <Thursday> good` show on the page; editing in two tabs refuses the second save.
- `teanode dream run` writes Thursday's headline and narrative; every chip in it opens a record; the chosen mail and work messages each carry a reason; "left out as routine" counts the rest. A test feeds the dream a model answer naming a record not in its input and sees it refused.
- Adding a photo to Thursday after the dream marks the day stale; the next dream writes it again and then the week.
- With **Let the agent read Health data** off, the dream's input has no Health numbers (a test inspects the prompt), and the page still shows Health.
- "Touch up" on Thursday's words shows a touched-up text marked as such; opening the original shows the person's text unchanged; editing the original and saving makes a new touched-up text from it; turning it off shows the original again. The dream's input (a test inspects the prompt) holds the original, not the touched-up text.
- A correction ("it was Sam at lunch") under Thursday's narrative writes it again at once without the mistake, and a later rewrite (after a photo is added) keeps it corrected.
- A day older than three months has its numbers and links but no narrative until the person asks for one on its page.
- The agent, asked in a conversation "what did I do last Thursday?", calls `journal` `read_day` and answers from it.
- The iPhone screenshots from CI show the timeline, the day, the week and the month in light, dark and the largest text.
- `teanode journal erase` leaves no journal rows and every record in place.

## Idempotence and Recovery

The migration is additive and has a reverse script. The facts and weather steps can run any number of times; they recompute and overwrite. A dream that is cut short leaves the days it finished written and the rest stale, and the next run continues. Deleting a narrative is safe: the next dream writes it again. Erasing the journal does not touch any record it linked to.

## Artifacts and Notes

The mockups (dashboard day, timeline, week, month, year; iPhone timeline and day) were reviewed with the maintainer outside the repository; screenshots of the built pages from CI replace them in the pull request.

## Interfaces and Dependencies

No new Go or JavaScript library. Weather comes from Open-Meteo's HTTP API (`https://archive-api.open-meteo.com/v1/archive` for past days, `https://api.open-meteo.com/v1/forecast` for today), called with `latitude`, `longitude` (rounded to one decimal), `start_date`, `end_date`, `daily=weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum` and `timezone`, through the server's existing safe outbound HTTP client (`internal/util/safefetch`), with no key.

In `internal/journal/journal.go`, define:

    func DayRange(user *models.User, day string, zone *time.Location) (from, until time.Time)
    func ReadDay(ctx context.Context, database db.Database, user *models.User, day string) (*models.JournalDayView, error)
    func ReadPeriod(ctx context.Context, database db.Database, user *models.User, periodKind models.JournalPeriodKind, periodStart string) (*models.JournalPeriodView, error)
    func DreamInput(view *models.JournalDayView, canReadHealth bool) (string, map[string]models.AgentRecordRef)
    func CheckNarrative(narrative string, choices []models.JournalChoice, allowed map[string]models.AgentRecordRef) error

A day is a `YYYY-MM-DD` string, as the repository already passes `posted_on` (`models.SharedFinanceTransaction.PostedOn`) and Health's `summary_day`; a period start is the same for its first day.

In `internal/models/journal.go`: `JournalDay`, `JournalPeriod`, `JournalLink`, `JournalPeriodKind` (`week`, `month`, `year`), `JournalLinkSource` (`agent`, `person`), `JournalMoodSource` (`person`, `health`), `JournalFacts`, `JournalWeather`, `JournalDayView`, `JournalPeriodView`, `JournalChoice`.

In `internal/db/database_journal.go`, a `JournalOperation` interface on the database with `GetJournalDay`, `ListJournalDays`, `PutJournalWords`, `PutJournalMood`, `PutJournalFacts`, `PutJournalWeather`, `PutJournalNarrative`, `ListStaleJournalDays`, `GetJournalPeriod`, `PutJournalLookingBack`, `PutJournalPeriodNarrative`, `ListJournalLinks`, `PutJournalLink`, `HideJournalLink`, `SearchJournal`, `EraseJournal`.

Revision note (2026-10-26): first version, written after the research and the mockups, before the maintainer's answers.

Revision note (2026-10-26, later): recorded the maintainer's answers: past days can be changed and the change ripples up; touched-up words beside the original; corrections to a story; no private switch; narratives three months back; Journal in More on the iPhone.
