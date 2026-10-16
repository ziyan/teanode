# Follow-ups: the agent keeps track of bills, parcels, deadlines and coming events from mail

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It builds on `docs/planning/mail-alerts-execplan.md`, which made the alerts this plan feeds.

## Purpose / Big Picture

Alerts today react to one message at the moment it arrives: a school notice about an illness, a burst of sign-in codes. Most of what a person would want an assistant to watch is not one message but a situation that unfolds over days, and the moment that matters is often a message that never comes. A county tax office mails a bill due on the 1st, and nothing says whether it was paid. A shop mails "your order has shipped", and nobody notices that a week later it still has not arrived. A school sends a permission slip due Friday and a notice of a concert next Thursday, and a neighborhood association announces a street fair. Each arrived, was sorted, and was forgotten.

After this plan the agent keeps a list of follow-ups: things mail said are due, on their way, or coming up, each with a date and what would close it. It closes them itself when later mail or a bank transaction shows they happened (the payment posted, the parcel was delivered), and it tells the person, through the existing alerts, when one needs them: a bill due in three days with no payment seen, a bill past due, a parcel out for delivery or stuck, a deadline the day before, and once a week what is coming up at school and nearby. It does this without being asked, because a person who has to set up each kind of watching never will; "stop telling me about these" mutes a kind, as alerts already allow.

To see it working on the development server: deliver to a mailbox the agent triages an invented property tax bill (an invented county, an amount, due in five days), and within minutes `teanode agent follow list` shows a bill follow-up with that amount and date. Run the follow-up sweep as if two days had passed (`teanode agent follow sweep --as-of <date>`), and an alert in the main conversation says the bill is due in three days and no payment has been seen. Add an invented bank transaction to that payee for that amount, sweep again, and the follow-up is closed as paid and nothing more is said.

## Progress

- [x] (2026-10-16) Surveyed alerts, triage, extract, watched items, finance and the person's controls; wrote this plan.
- [x] (2026-10-16) Milestone 1: the follow-up record and its extraction from mail: migration 0191 (`agent_follow_up`), `models.AgentFollowUp`, the four database functions in `internal/db/database_alert.go`, extract's `follow_ups` with `InterpretFollowUps` and `StoreFollowUps` in `internal/agent/extract.go`, triage's widened `extract`, and tests.
- [x] (2026-10-16) Milestone 2: closing follow-ups from later mail and finance transactions: `matchFollowUpPayments` and the pure `isFollowUpPaidBy` in `internal/agent/follow_up_payment.go`, run after every sync and before every sweep; a delivered parcel writes a candidate (`noteParcelDelivered` in `extract.go`).
- [x] (2026-10-16) Milestone 3: `sweepFollowUps` and the `follow_up_sweep` job in `internal/agent/follow_up_sweep.go`, queued from the worker's tick by slot (08:00 daily, Sunday 18:00 in the person's zone); candidate kind `follow_up` with `follow_up_ids`, `follow_up_told_state` and a unique `follow_up_subject_key` (migration 0191, changed in place before any deploy); `AgentFollowUpToldState`; the alert prompt and its rendering of follow-ups.
- [ ] Not done in Milestone 3: telling a parcel is out for delivery, which needs a delivery status on the follow-up.
- [x] (2026-10-16) Milestone 4: control on every surface: `ListAgentFollowUps`, `StopAgentFollowUp` and `AddAgentFollowUp` (apigraph `agent_alert.go`, `FollowUpKeyOf` and `internal/agent/follow_up.go`), `agent_profile`'s `list_follow_ups`, `add_follow_up` and `stop_follow_up`, `teanode agent follow list|add|stop`, the Following list on the dashboard's Alerts card and in the iPhone app's alert settings, and the follow-up kinds as kinds to mute. Not done: `teanode agent follow sweep --as-of`, which the scenario under Purpose uses and which waits for Milestone 5.
- [x] (2026-10-16) Milestone 4b: the weekly "around you" run: the `nearby` job (`models.AgentJobNearby`, `internal/agent/nearby.go`) queued Saturday 21:00 in the person's zone by slot (`queueNearbyRuns`), the home area and household read from the self and place pages (`nearby_home.txt`, `cleanHomeArea`), asked once as the idea `nearby_home_area` when memory has none, one turn with only `web_search` and `web_fetch` held to 12 searches and 10 page reads by the loop (`AskSettings.CallLimits`, `tool_call_limits.go`) with `prompts/nearby.txt`, items written as `event` and `deadline` follow-ups with a new `source_url` column (migration 0191, changed in place) shown as a link on the dashboard, the iPhone app, the command line and the agent tool, the kind mute `nearby`, and the alert text allowed to carry a follow-up's stored page address.
- [x] (2026-10-16) Milestone 5, the backfill: `internal/agent/follow_up_backfill.go` (`BackfillFollowUps`, `chooseFollowUpBackfill`, `looksLikeFollowUp`, `queueFollowUpBackfills`), `agent.follow_up_backfilled_at` (migration 0191, changed in place), `db.ListMailInsightHeads`, `ClaimAgentFollowUpBackfill` and `MarkAgentFollowUpBackfilled`, a parcel more than a week late let go by the sweep, `ReplaceMailProposals` no longer re-offers what was decided, the `BackfillAgentFollowUps` mutation and `teanode agent follow backfill`, and the docs in `docs/subsystems/agents.md` and `docs/reference/command-line.md`.
- [ ] Milestone 5, what remains: `teanode agent follow sweep --as-of` (named under Purpose, not built), a deploy with a backup (migration 0191 adds a table and columns), and the end-to-end scenario under Purpose on the development server, which also shows the backfill queuing the first extract jobs.

## Surprises & Discoveries

- Observation: nothing in the code keeps a situation open. Watched items (`internal/agent/watched_item.go`, `models.AgentWatchedItem`) are a log of what a skill's watch has already seen, kept 30 days for de-duplication; an alert candidate is dropped after 24 hours (`alertFreshness` in `internal/agent/alert_candidate.go`); subject keys only stop repeats for a week.
  Evidence: the survey of `internal/agent/alert*.go`, 2026-10-16.
- Observation: the only things re-checked against the clock are budgets (after each finance sync, `noteBudgetCandidates` in `internal/agent/budget_status.go`) and goals the person set. Both are the shape this plan needs: server code that looks at stored state and emits a candidate.

## Decision Log

- Decision: four kinds of follow-up: `bill` (a payee, an amount, a due date), `parcel` (a carrier, a tracking number, an expected date), `deadline` (something the person must do or return by a date: a form, a permission slip, an RSVP, a renewal), and `event` (a dated happening the person may want to go to: a school concert, a parents' evening, a street fair, a council meeting).
  Rationale: these cover the cases asked for (a property tax bill, a parcel, a school's goings-on, neighborhood events) and differ in what closes them and when to speak, which is what the kind is for. Anything else stays with ordinary alerts.
  Date/Author: 2026-10-16.
- Decision: follow-ups come out of the extract job, not triage. Triage (one call per message on the triage model) only says whether a message carries one, through a widened `extract` flag; the extract job (`internal/agent/extract.go`, `prompts/extract.txt`) already reads flagged messages in full with today's date and the person's zone, and returns `follow_ups` beside `events` and `contacts`.
  Rationale: triage runs on every message and must stay cheap; extract already exists for exactly "what does this message carry that belongs somewhere else", and dates need its date handling.
  Date/Author: 2026-10-16.
- Decision: a follow-up is written straight to the list, without a confirmation card, unlike extract's events and contacts.
  Rationale: the list is the agent's own notebook, not the person's calendar or address book; writing it changes nothing they own, and asking about every parcel would be the hassle this plan exists to remove. The person sees it through what the agent says, and can stop any of it.
  Date/Author: 2026-10-16.
- Decision: each follow-up has a key that the same situation always produces (the tracking number for a parcel; the payee and the due date for a bill; the sender's domain and the date for an event), so the third "your order has shipped / is out for delivery / was delivered" updates one follow-up instead of making three.
  Rationale: one situation, one row; and a later message with the same key is what closes it.
  Date/Author: 2026-10-16.
- Decision: closing is by evidence, in code where it can be: a later message that extract says resolves the key (delivered, payment received, receipt), or a finance transaction leaving an account whose amount matches a bill's to the cent and whose payee or description matches its payee by words, posted within 10 days either side of the due date. A follow-up past its date with nothing to close it is not closed silently: a bill becomes overdue (said once), a parcel stalled (said once, three days past its expected date), and an event or deadline simply expires the day after.
  Rationale: the missing payment is the thing worth saying, so it must not be guessed away; matching on amount and words is exact enough for a bill and needs no model.
  Date/Author: 2026-10-16.
- Decision: when to speak. A bill: three days before the due date if no payment has been seen, and once the day after it if still none. A parcel: when out for delivery, when delivered, when a carrier reports a problem, and when stalled. A deadline: the day before. Events: one roundup a week, Sunday at 18:00 in the person's zone, of the coming week's events and deadlines, and nothing per event. Each of these is an alert candidate of a new kind, `follow_up`, and passes through the existing bounds (daily cap, quiet hours, the week's subject keys) and mutes unchanged.
  Rationale: a reminder is worth a message; a week of events in one message is a digest, where one message each would be noise. Going through the alert job means the decision step, the delivery to the main conversation, the phone and the chat app, and every control already built apply.
  Date/Author: 2026-10-16.
- Decision: on by default for every agent with alerts on; a person mutes a kind ("stop telling me about neighborhood events") with the existing `mute_alert` and a new mute scope `followUpKind`, or one follow-up with `stop`.
  Rationale: asked for in so many words: the agent should start, and the person should be able to stop it.
  Date/Author: 2026-10-16.
- Decision: superseded the same day. At first neighborhood events were to come only from the person's mail. The person asked for more: the agent may use `web_search` and `web_fetch` to learn about where they live and what is on there. So once a week an "around you" research run looks outward: events, places and restaurants worth knowing (new ones especially), weekend activities that fit the household (children's ages, interests, food the person likes or avoids, all from memory), and dates that matter (public and school holidays, daylight saving, election and registration deadlines, tax deadlines). What it finds is written as follow-ups of kind `event` or `deadline` with the page it came from, and goes in the Sunday roundup.
  Rationale: asked for in so many words; mail alone misses most of what is on nearby. Searches name the town or neighborhood the person lives in, never a street address, so what goes to a search provider is what anyone might search for.
  Date/Author: 2026-10-16.
- Decision: the home area is read from the agent's memory (the person's self page and the places it knows). When memory does not say, the agent asks once, the way it speaks first today, and keeps the answer as a fact; until then the run is skipped.
  Rationale: there is no home-location field anywhere in the model, and the person has usually said where they live already; asking once is cheaper than a setting nobody fills in.
  Date/Author: 2026-10-16.
- Decision: the run is bounded: one a week, Saturday night before the Sunday roundup, at most 12 searches and 10 page reads, at most 8 items in the roundup from it, and nothing it already said in the last four weeks. It is muted like any kind (`nearby`).
  Rationale: a weekly message worth reading is short; a research run without bounds costs and repeats itself.
  Date/Author: 2026-10-16.

- Decision: the look around is its own job kind, `nearby`, queued by slot (`<Saturday's date>T21`, also named on the Sunday morning for a run missed while the server was down) from the worker tick, exactly as the sweep is, and it makes two model calls: one that reads the self and place pages for the town and household (kind of work triage), and one research turn (kind of work research).
  Rationale: reading memory for a town is a different job from searching the web, and the second must not start without the first's answer; splitting them also keeps the household and the town out of the research turn's prompt unless a town was found.
  Date/Author: 2026-10-16.
- Decision: the bounds on searches and page reads are enforced by the ask loop, not asked of the model: `AskSettings.CallLimits` names how many times a tool may be called in a turn, and `runTool` answers a call past the limit with an error that says so and does not run it. The limits travel from the job to the turn in the context (`withToolCallLimits`), as the recording evidence safety does, because the thinking functions already take a dozen arguments.
  Rationale: a prompt that says "at most 12" is a request; the loop is the only place every call passes, and a batch of calls in one round is counted under a lock.
  Date/Author: 2026-10-16.
- Decision: the town is cleaned in code after the model reads it (`cleanHomeArea`): the pieces between commas and lines that name a street, a unit or a box are dropped and words with a digit are taken out of the rest. The agent's usual prompt still carries the whole self page into the research turn (every turn does), so the street is not hidden from the model that searches, only from the run's own message; the prompt tells it to put the town and the kind of thing in a search and nothing else.
  Rationale: what leaves for the search provider is the model's query, so the control is the instruction plus a town that never had a street in it; hiding the self page from one kind of turn would be a change to every turn's prompt for this one use.
  Date/Author: 2026-10-16.
- Decision: asking for the town is one personal idea, `nearby_home_area`, written directly (not through `ProposeIdea`, which would judge it with a model), offered by the existing idea mechanism when the person is at the dashboard and quiet. One row per agent whatever became of it is what makes it asked once: dismissed or left to expire it is not made again, and when a later run finds the town in memory the open one is marked done. The run searches nothing and asks nothing when the operator has configured no search service.
  Rationale: the idea mechanism already knows when speaking is welcome, records that it was shown, and starts a conversation whose turn notes the answer in memory, so no new code listens for the answer.
  Date/Author: 2026-10-16.
- Decision: an item is written only if its page was in something the run read: the lower-cased tool results of its conversation must contain the address's host and path. Its key is the kind, `nearby:`, the normalized page address and the day, so one page and day is one item. The model's "because" line steers its choice and is not stored.
  Rationale: a model asked for "the page that says it" will sometimes write one that looks right; checking against what the tools returned costs one read of the conversation. No column was added for the reason line because nothing shows it.
  Date/Author: 2026-10-16.
- Decision: the weekly mute `nearby` stops the run, and also keeps what the run wrote out of the Sunday roundup (by the key's marker), leaving the follow-ups read out of mail; `follow_up` (all) stops the run too.
  Rationale: muting "what is on near me" has to stop hearing of it in the roundup, not only stop new finds.
  Date/Author: 2026-10-16.
- Decision: the alert text may carry a page address, but only one stored on a follow-up the alert covers (`cleanAlertTextKeeping`); every other address, host name and link is taken out as before.
  Rationale: the one-line permission in `prompts/alert.txt` would otherwise do nothing, since the cleaning that makes an alert safe to read as the agent's word removes every address; the addresses the server stored are the ones it can vouch for as web pages.
  Date/Author: 2026-10-16.

- Decision: the stored key is the kind, a colon, and the model's key in lower case with its spaces gathered (`parcel:tn-48151623`), made in code, and a repeat message never changes a follow-up's state: only the sweep, a closing payment or message, and the person do.
  Rationale: a model's spelling of a key varies in case and spacing, and a tracking number must not collide with another kind's key; a reminder mail arriving after a bill went overdue must not make it look open again.
  Date/Author: 2026-10-16.
- Decision: the event exclusion is "an event a business holds to sell something", not "an event nobody in the household is part of".
  Rationale: the second would have excluded the street fairs and neighborhood events the person asked for.
  Date/Author: 2026-10-16.

- Decision: what the person was told is its own enum, `AgentFollowUpToldState` (reminded, overdue, stalled, delivered, roundup), apart from the follow-up's state; each is told once, and the candidate's unique subject key (the follow-up key and the told state) is the backstop.
  Rationale: a bill is reminded while it is still open, so "told" cannot be a state of the follow-up.
  Date/Author: 2026-10-16.
- Decision: the payment matcher counts transfers, and extract skips a bill the message says is paid automatically.
  Rationale: paying a card's bill moves money between the person's own accounts and is filed as a transfer; excluding transfers would have called every card bill overdue. An autopay statement needs no follow-up.
  Date/Author: 2026-10-16.
- Decision: a bill first found more than 14 days past its day is let go as expired without a word, and a parcel more than 30 days late is let go too.
  Rationale: the 45-day look back over old mail would otherwise announce a run of "overdue" bills that were paid in ways the linked accounts do not show (escrow, another bank), and a parcel a month late has been dealt with some other way.
  Date/Author: 2026-10-16.
- Decision: the sweep is queued by slot (`<date>T08`, `<date>T18` on Sunday) as the job's subject, from the worker tick every ten minutes, and the job first matches payments, then sweeps, then queues the alert job; it calls no model.
  Rationale: a slot as the subject survives a restart and makes a missed sweep run at the next look; matching first keeps a bill whose payment is already stored from being reminded.
  Date/Author: 2026-10-16.

- Decision: the surfaces share the alert mutations' permission (agent:use) and live beside them (`AgentAlertQuery`, `AgentAlertMutation`), and the tool actions sit in `agent_profile` next to `mute_alert` rather than in a tool of their own. A person-added follow-up that already exists and is over (closed, expired or stopped) is opened again and told about again; one still being watched is left as it is. A Mute of an alert about a follow-up offers the follow-up's kind, by giving the candidate's facts the kind as their mail category.
  Rationale: "stop following that" and "no more parcel alerts" are said in the same breath as muting an alert, and a neighbor tool for one more small noun would be a second place to look.
  Date/Author: 2026-10-16.

- Decision: the backfill chooses messages from stored insights without a model: those with the extract flag, plus those whose category is `invitation` or whose category or subject has a word of a short list in code (bill, invoice, statement, payment, due, tax, parcel, shipped, delivery, form, permission slip, RSVP, school, club, neighborhood, concert, meeting, fair and a few more), never a category `phishing`, `junk` or `promotion`, never a message with an item in Junk or Trash, none received more than 45 days ago, at most 150 over all the agent's granted mailboxes, newest first. Triage is not run again.
  Rationale: the list is a net that may catch too much, because the extract run reads the whole message and keeps only a usable follow-up; a model to choose would cost more than the reading it saves.
  Date/Author: 2026-10-16.
- Decision: that the backfill ran is the column `agent.follow_up_backfilled_at`, claimed with `UPDATE ... WHERE follow_up_backfilled_at IS NULL` (`ClaimAgentFollowUpBackfill`), by a worker look every ten minutes (`queueFollowUpBackfills`) for active agents with alerts on, once there is a model to read with and a granted mailbox. The on-demand run writes the time anew without asking.
  Rationale: a job subject would need a new job kind with its names on the dashboard and the iPhone app, and jobs are the wrong record of a thing that must outlive them; a column is claimed atomically, survives restarts and a second worker, and an agent that turns alerts on later is picked up by the same look with no extra code. It is not set for an agent with no granted mailbox, so the look still happens once there is one.
  Date/Author: 2026-10-16.
- Decision: the extract jobs are ordinary `extract` jobs, queued with `NotBefore` 20 seconds apart (the first at once), not a new priority.
  Rationale: the queue has no priority except putting ingest, dream and backfill jobs last, and has `NotBefore`; a job not yet due is not claimed, so what arrives in the next hour is claimed ahead of the later ones. 150 jobs take about 50 minutes at that pace.
  Date/Author: 2026-10-16.
- Decision: a parcel first seen more than 7 days past its expected day, and not yet told as stalled, is let go without a word (`followUpParcelStaleDays`), as a bill more than 14 days past already was. An event or deadline whose day went already expired silently.
  Rationale: without it a parcel found by the backfill 5 to 30 days late would have been announced as stalled; a live parcel is stalled on the third day by the daily sweep, so a week is far past anything the sweep would have missed but a server down for a week.
  Date/Author: 2026-10-16.
- Decision: `ReplaceMailProposals` now leaves out a new proposal that says what a proposal the person already accepted or dismissed says (the same event summary and start, or a person sharing an address or number), and still replaces the offers waiting.
  Rationale: before this it kept the decided ones and appended every new one, so reading a message a second time put the event the person had dismissed back in front of them, which the comment on `MailProposalDismissed` says never happens; the backfill reads messages that live triage already queued for extract, so it would have done so.
  Date/Author: 2026-10-16.
- Decision: the on-demand run is the mutation `BackfillAgentFollowUps` (agent:use, beside the follow-up mutations) and `teanode agent follow backfill`; it queues the same way, works with alerts off, and answers how many messages it queued. No dashboard or iPhone button.
  Rationale: asked for on the command line; the person who needs it is one who turned alerts on later, and the worker's look already covers them.
  Date/Author: 2026-10-16.

- Decision: a message received more than seven days ago offers no event or contact cards when it is read, and no message offers an appointment whose time has come (an all-day one lasts until its day is over); its follow-ups are still kept.
  Rationale: the look back over past mail reruns extract on messages up to 45 days old, which would otherwise put cards for meetings long over in front of the person; the same is true of a live message about something already past.
  Date/Author: 2026-10-16.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

The agent is per person (`models.Agent`), and reads the mailboxes it was granted. Every arriving message is triaged: `internal/agent/triage.go` sends `internal/agent/prompts/triage.txt` with the message and stores the answer as a mail insight (`models.MailInsight`). Triage's `extract` flag queues an extract job (`models.AgentJobExtract`, `internal/agent/extract.go`), which sends `prompts/extract.txt` and turns its answer into confirmation cards for calendar events and contacts.

An alert is a message the agent writes into the person's main conversation unasked. Sources write candidate rows (`models.AgentAlertCandidate`, kinds message, burst, budget, watched); the alert job (`internal/agent/alert.go`, `prompts/alert.txt`) waits two minutes to gather them, decides with one model call what to say, and delivers it. `planAlerts` in `alert.go` applies the bounds: at most five a day, quiet hours 22:00 to 07:00 unless urgent, no repeat of a subject key within seven days. Mutes are rows of `agent_alert_mute` (`internal/agent/alert_mute.go`) scoped by sender, domain, subject key, kind or spending category, set by the `agent_profile` tool's `mute_alert` and by the dashboard, the iPhone app and `teanode agent alert mute`.

Finance transactions (`models.FinanceTransaction` in `internal/models/finance.go`) arrive by sync (`internal/agent/ingest_finance.go`); negative amounts are money leaving. After each sync `noteBudgetCandidates` checks budgets and writes candidates; this plan's matching of payments runs in the same place.

Migrations are numbered SQL files in `internal/db/migrations/` (the newest is `0190_agent_billed_day.sql`); `docs/coding/database-migrations.md` says how to add one safely, and production is backed up before a deploy that migrates.

## Plan of Work

Milestone 1 adds `models.AgentFollowUp` and its table, `agent_follow_up`, with: id, agent id, kind, key, title (one line, in the person's language), payee or carrier, amount and currency (bills), tracking number (parcels), due or expected date, place (events), state (`open`, `closed`, `overdue`, `stalled`, `expired`, `stopped`), closed reason, the message ids it came from, and the times it was last told about and last updated. `internal/db` gains create-or-update by (agent, key), list open by agent, and close. `prompts/extract.txt` gains a `follow_ups` list with the four kinds, their fields, the key rules, and what does not count (marketing and sales deadlines, an event a business holds to sell something, a bill the message says is already paid); triage's description of `extract` widens to bills, shipments, deadlines and dated notices from a school, a club or the neighborhood. `extract.go` writes them. The test is a table of invented messages (a tax bill, a shipping notice and its delivery, a permission slip, a street fair, a promotion with a "sale ends Friday") run through the parsing and storing code with the model's answer given, plus one prompt test on sol that the extraction of the five gives the expected kinds.

Milestone 2 closes follow-ups: extract's answer for a message whose key matches an open follow-up updates it (out for delivery, delivered, payment received); `ingest_finance.go` calls a matcher over open bills after each sync. Tests: invented transactions that match and that miss by a cent, by payee and by date.

Milestone 3 adds the sweep: a job that runs at 08:00 in the person's zone and on demand, looks at open follow-ups against today, sets overdue, stalled and expired, and writes `follow_up` candidates for what is due to be said, plus on Sundays at 18:00 the roundup candidate with the coming week's events and deadlines. `prompts/alert.txt` learns the new kind and how to phrase a roundup. The test drives the sweep with a fixed clock through a week.

Milestone 4 gives control everywhere at once, as the repository requires (`CONTRIBUTING.md`): a `follow_up` tool for the agent (list, add from what the person says, stop), `teanode agent follow list|stop|sweep`, a Following list on the dashboard's Alerts card with Stop on each row, the same in the iPhone app's alert settings, and the `followUpKind` mute scope on all of them.

Milestone 4b adds the "around you" run: a background job (`models.AgentJobNearby`) queued Saturday at 21:00 in the person's zone. It reads the home area and the household from memory; when there is no home area it offers one question as an idea the agent puts to the person unasked, once, and stops. Otherwise it runs a research turn (the same loop a conversation uses, with nobody present, read-only tools, `web_search` and `web_fetch` only, the bounds above) with a prompt `prompts/nearby.txt` that asks for the coming week's and coming weekend's events, new or notable places, activities that suit the household, and dates that matter, each with its page, as JSON. The job writes them as follow-ups with a key of the page and date, skipping what was told in the last four weeks, and the Sunday roundup includes them. The test gives the run fixed search results through a fake `web_search` and checks the follow-ups written and the bounds.

Milestone 5 backfills: one job per agent over the last 45 days of triaged mail that triage would now flag, through extract, so bills and parcels already in flight are on the list on day one; then the docs (`docs/subsystems/agents.md` alerts section, `docs/reference/command-line.md`), a deploy with a backup, and the scenario under Purpose.

## Concrete Steps

From the repository root, with Docker running:

    make test
    go test ./internal/agent -run FollowUp
    go run ./cmd/teanode agent follow list

Expected after Milestone 1, with the invented tax bill delivered on the development server:

    kind   due         amount    title
    bill   2026-10-21  1,234.00  Property tax, first half (an invented county)

## Validation and Acceptance

The scenario under Purpose, on the development server with an invented mailbox: the bill appears, the sweep with `--as-of` three days before the due date writes one alert, the matching transaction closes it, and a second sweep says nothing. A shipping notice followed two days later by a delivery notice makes one follow-up, told about when delivered, then closed. A Sunday sweep with two invented school events and a street fair in the coming week writes one roundup. `mute_alert` with the kind `event` stops the roundup.

## Idempotence and Recovery

Extraction updates by key, so running extract twice on one message, or the backfill twice, changes nothing. The sweep only writes a candidate for a follow-up not told about that state, so running it twice in a day says nothing twice. The migration only adds a table and a mute scope value; rolling back is dropping the table.

## Artifacts and Notes

None yet.

## Interfaces and Dependencies

In `internal/models/follow_up.go`:

    type AgentFollowUpKind string // "bill", "parcel", "deadline", "event"
    type AgentFollowUpState string // "open", "closed", "overdue", "stalled", "expired", "stopped"
    type AgentFollowUp struct { ID, AgentID string; Kind AgentFollowUpKind; Key, Title string; ... }

In `internal/db`: `UpsertAgentFollowUp(*models.AgentFollowUp) (*models.AgentFollowUp, bool, error)`, `ListOpenAgentFollowUps(agentId string) ([]*models.AgentFollowUp, error)`, `UpdateAgentFollowUp(id string, update func(*models.AgentFollowUp) error) (*models.AgentFollowUp, error)`, and `ListAgentFollowUps(agentId string, limit int) ([]*models.AgentFollowUp, error)`.

In `internal/agent`: `sweepFollowUps(tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) ([]*models.AgentAlertCandidate, error)` (the owner's zone says which day it is), `matchFollowUpPayments(tx db.Transaction, agentId string, today time.Time) (int, error)` (it reads the stored transactions itself and answers how many bills it closed), and for the look around `(*Agent).lookAround(ctx, run, now) error`, `nearbySlot(now, location) string`, `interpretNearbyItems(items, agentId, today, seen) []*models.AgentFollowUp`, `cleanHomeArea(text) string` and `withToolCallLimits(ctx, limits) context.Context`.

`models.AgentFollowUp` has `SourceURL` (`source_url`, GraphQL `sourceUrl`), the page an event or deadline found on the web came from.

In `internal/agent/follow_up_backfill.go`: `BackfillFollowUps(tx db.Transaction, agent *models.Agent, now time.Time, isForced bool) (int, error)` (answers how many messages it queued), `chooseFollowUpBackfill(heads []*db.MailInsightHead, limit int) []*db.MailInsightHead`, `looksLikeFollowUp(category, subject string) bool`, `(*Agent).queueFollowUpBackfills(ctx, now)`, and `FollowUpBackfillView{QueuedMessageCount}`. In `internal/db`: `ListMailInsightHeads(mailboxId string, since time.Time, leftOutCategories []string, limit int) ([]*MailInsightHead, error)`, `ClaimAgentFollowUpBackfill(agentId string, at time.Time) (bool, error)` and `MarkAgentFollowUpBackfilled(agentId string, at time.Time) error`. `models.Agent.FollowUpBackfilledAt`. GraphQL `BackfillAgentFollowUps` returning `{ queuedMessageCount }`; `client.BackfillAgentFollowUps`.

Revision 2026-10-16: the person asked that the agent also use web search to learn about where they live and what is on there; the mail-only decision was superseded, and Milestone 4b and three decisions were added.
