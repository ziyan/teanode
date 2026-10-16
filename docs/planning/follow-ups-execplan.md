# Follow-ups: the agent keeps track of bills, parcels, deadlines and coming events from mail

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It builds on `docs/planning/mail-alerts-execplan.md`, which made the alerts this plan feeds.

## Purpose / Big Picture

Alerts today react to one message at the moment it arrives: a school notice about an illness, a burst of sign-in codes. Most of what a person would want an assistant to watch is not one message but a situation that unfolds over days, and the moment that matters is often a message that never comes. A county tax office mails a bill due on the 1st, and nothing says whether it was paid. A shop mails "your order has shipped", and nobody notices that a week later it still has not arrived. A school sends a permission slip due Friday and a notice of a concert next Thursday, and a neighborhood association announces a street fair. Each arrived, was sorted, and was forgotten.

After this plan the agent keeps a list of follow-ups: things mail said are due, on their way, or coming up, each with a date and what would close it. It closes them itself when later mail or a bank transaction shows they happened (the payment posted, the parcel was delivered), and it tells the person, through the existing alerts, when one needs them: a bill due in three days with no payment seen, a bill past due, a parcel out for delivery or stuck, a deadline the day before, and once a week what is coming up at school and nearby. It does this without being asked, because a person who has to set up each kind of watching never will; "stop telling me about these" mutes a kind, as alerts already allow.

To see it working on the development server: deliver to a mailbox the agent triages an invented property tax bill (an invented county, an amount, due in five days), and within minutes `teanode agent follow list` shows a bill follow-up with that amount and date. Run the follow-up sweep as if two days had passed (`teanode agent follow sweep --as-of <date>`), and an alert in the main conversation says the bill is due in three days and no payment has been seen. Add an invented bank transaction to that payee for that amount, sweep again, and the follow-up is closed as paid and nothing more is said.

## Progress

- [x] (2026-10-16) Surveyed alerts, triage, extract, watched items, finance and the person's controls; wrote this plan.
- [ ] Milestone 1: the follow-up record and its extraction from mail (migration, `agent_follow_up`, extract returns `follow_ups`, triage flags mail that carries one).
- [ ] Milestone 2: closing follow-ups from later mail and from finance transactions.
- [ ] Milestone 3: the daily sweep that turns due, overdue, stalled and coming follow-ups into alert candidates, and the weekly coming-up roundup.
- [ ] Milestone 4: control on every surface: the agent's tool, the command line, the dashboard's Alerts card, the iPhone app; muting a kind.
- [ ] Milestone 4b: the weekly "around you" research run (home area from memory or asked once, `web_search` and `web_fetch`, bounded), feeding events, places and important dates into the Sunday roundup.
- [ ] Milestone 5: a backfill over the last 45 days of mail, so the list is useful the day it ships; docs; deploy; the scenario above.

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

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

The agent is per person (`models.Agent`), and reads the mailboxes it was granted. Every arriving message is triaged: `internal/agent/triage.go` sends `internal/agent/prompts/triage.txt` with the message and stores the answer as a mail insight (`models.MailInsight`). Triage's `extract` flag queues an extract job (`models.AgentJobExtract`, `internal/agent/extract.go`), which sends `prompts/extract.txt` and turns its answer into confirmation cards for calendar events and contacts.

An alert is a message the agent writes into the person's main conversation unasked. Sources write candidate rows (`models.AgentAlertCandidate`, kinds message, burst, budget, watched); the alert job (`internal/agent/alert.go`, `prompts/alert.txt`) waits two minutes to gather them, decides with one model call what to say, and delivers it. `planAlerts` in `alert.go` applies the bounds: at most five a day, quiet hours 22:00 to 07:00 unless urgent, no repeat of a subject key within seven days. Mutes are rows of `agent_alert_mute` (`internal/agent/alert_mute.go`) scoped by sender, domain, subject key, kind or spending category, set by the `agent_profile` tool's `mute_alert` and by the dashboard, the iPhone app and `teanode agent alert mute`.

Finance transactions (`models.FinanceTransaction` in `internal/models/finance.go`) arrive by sync (`internal/agent/ingest_finance.go`); negative amounts are money leaving. After each sync `noteBudgetCandidates` checks budgets and writes candidates; this plan's matching of payments runs in the same place.

Migrations are numbered SQL files in `internal/db/migrations/` (the newest is `0190_agent_billed_day.sql`); `docs/coding/database-migrations.md` says how to add one safely, and production is backed up before a deploy that migrates.

## Plan of Work

Milestone 1 adds `models.AgentFollowUp` and its table, `agent_follow_up`, with: id, agent id, kind, key, title (one line, in the person's language), payee or carrier, amount and currency (bills), tracking number (parcels), due or expected date, place (events), state (`open`, `closed`, `overdue`, `stalled`, `expired`, `stopped`), closed reason, the message ids it came from, and the times it was last told about and last updated. `internal/db` gains create-or-update by (agent, key), list open by agent, and close. `prompts/extract.txt` gains a `follow_ups` list with the four kinds, their fields, the key rules, and what does not count (marketing, an event nobody in the household is part of, a bill the message says is already paid); triage's description of `extract` widens to bills, shipments, deadlines and dated notices from a school, a club or the neighborhood. `extract.go` writes them. The test is a table of invented messages (a tax bill, a shipping notice and its delivery, a permission slip, a street fair, a promotion with a "sale ends Friday") run through the parsing and storing code with the model's answer given, plus one prompt test on sol that the extraction of the five gives the expected kinds.

Milestone 2 closes follow-ups: extract's answer for a message whose key matches an open follow-up updates it (out for delivery, delivered, payment received); `ingest_finance.go` calls a matcher over open bills after each sync. Tests: invented transactions that match and that miss by a cent, by payee and by date.

Milestone 3 adds the sweep: a job that runs at 08:00 in the person's zone and on demand, looks at open follow-ups against today, sets overdue, stalled and expired, and writes `follow_up` candidates for what is due to be said, plus on Sundays at 18:00 the roundup candidate with the coming week's events and deadlines. `prompts/alert.txt` learns the new kind and how to phrase a roundup. The test drives the sweep with a fixed clock through a week.

Milestone 4 gives control everywhere at once, as the repository requires (`CONTRIBUTING.md`): a `follow_up` tool for the agent (list, add from what the person says, stop), `teanode agent follow list|stop|sweep`, a Following list on the dashboard's Alerts card with Stop on each row, the same in the iPhone app's alert settings, and the `followUpKind` mute scope on all of them.

Milestone 4b adds the "around you" run: a background job (`models.AgentJobNearby`) queued Saturday at 21:00 in the person's zone. It reads the home area and the household from memory; when there is no home area it writes one speak-first question instead and stops. Otherwise it runs a research turn (the same loop a conversation uses, with nobody present, read-only tools, `web_search` and `web_fetch` only, the bounds above) with a prompt `prompts/nearby.txt` that asks for the coming week's and coming weekend's events, new or notable places, activities that suit the household, and dates that matter, each with its page, as JSON. The job writes them as follow-ups with a key of the page and date, skipping what was told in the last four weeks, and the Sunday roundup includes them. The test gives the run fixed search results through a fake `web_search` and checks the follow-ups written and the bounds.

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

In `internal/db`: `UpsertAgentFollowUp(*models.AgentFollowUp) (*models.AgentFollowUp, bool, error)`, `ListOpenAgentFollowUps(agentId string) ([]*models.AgentFollowUp, error)`, `UpdateAgentFollowUp(id string, update func(*models.AgentFollowUp) error) error`.

In `internal/agent`: `sweepFollowUps(tx db.Transaction, agent *models.Agent, now time.Time) ([]*models.AgentAlertCandidate, error)` and `matchFollowUpPayments(tx db.Transaction, agentId string, transactions []*models.FinanceTransaction) error`.

Revision 2026-10-16: the person asked that the agent also use web search to learn about where they live and what is on there; the mail-only decision was superseded, and Milestone 4b and three decisions were added.
