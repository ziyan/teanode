# Alerts: telling the person, unasked, what their mail says they should know now

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent reads every message that arrives, sorts it, and says nothing. A person whose child's daycare writes that a child in the class has croup, or whose phone number has received fourteen sign-in codes overnight that they did not ask for, finds out when they next open their mail, if they notice at all: the second is fourteen messages each of which looks routine.

After this plan the agent tells the person, on its own, when mail shows something they should know now. It writes a short message into the main conversation, in its own voice ("Heads up: someone has been requesting sign-in codes to your number, about fourteen since yesterday morning, most of them overnight. If that wasn't you, check your login activity and change the password."), and the message reaches them where they talk to the agent: the dashboard's drawer and the chat app they linked (Telegram or Discord). It is bounded so it is worth reading: a few a day at most, none at night unless it cannot wait, never the same thing twice, and the person can say "don't tell me about these" and be heard.

To see it working: send the account's own mailbox a message that reads like a school notice about an illness in the person's child's class (invented for the test), and within a couple of minutes a message about it appears in the main conversation and in the linked chat. Send six messages that look like sign-in codes from one service within an hour, and one alert arrives about the pattern, not six.

## Progress

- [x] (2026-09-29) Surveyed triage, speaking first, schedules, the chat apps and the injection stance; wrote this plan.
- [x] (2026-09-29) Milestone 1: triage says whether a message is worth telling now (`alert_signal`, `alert_reason`), and a program notices a burst of similar messages (`alert_candidate.go`, migration 0123).
- [x] (2026-09-29) Milestone 2: the alert job (`alert.go`, `prompts/alert.txt`, migration 0124): gathers candidates, decides with the person's triage memories and recalled pages, writes one short message, bounded in code by quiet hours, a daily cap of five and a week's subject keys; the drawer opens for it.
- [x] (2026-09-29) Milestone 3: delivery: the main conversation and the linked chat app, which also receives the agent's other unasked turns (`internal/channel/relay.go`, migration 0125).
- [x] (2026-09-29) Milestone 4: control: `isAlertsEnabled`, `alertQuietStart`/`alertQuietEnd` and `alertDailyMost` on the agent and `AgentMailbox.alerts` per mailbox (migration 0126), read by `isAlertingAllowed` and `alertBoundsOf`; mutes in `agent_alert_mute` (`alert_mute.go`), read by the candidate step and the job; `ListAgentAlerts`, `ListAgentAlertMutes`, `MuteAgentAlert`, `UnmuteAgentAlert`; `teanode agent alert list|mute|mutes|unmute` and the settings keys; `agent_profile`'s `no_alerts`, `alerts_on`, `mute_alert`, `unmute_alert`; the Alerts card on the Overview tab, the switches beside speaking first, the switch per mailbox under Sorting.
- [ ] Milestone 5: docs (done with Milestone 4: the alerts section in `docs/subsystems/agents.md`, the command-line rows, the decision record), deploy, and the two scenarios above on the development server.

## Surprises & Discoveries

- Observation: nothing the agent starts on its own reaches a chat app today. `internal/channel/channel.go` follows only the turns a chat message started; speaking first, schedules and goal turns write into the main conversation and stop there. There is no web push either.
- Observation: triage's prompt files a sign-in alert as a notification of low priority, which is right for one code and wrong for fourteen in a night. The signal is the pattern, which no single message's triage can see.
- Observation: triage has no memory of people beyond memories addressed to its audience and the contact book, so it cannot know that a school is the person's child's.
- Observation: an alert is not a turn, so nothing emitted the events the drawer listens for. The alert job publishes an `asked` (note `alert`), a `message` and a `done` on the main conversation's feed with the alert's id as the run id, which the drawer treats as any other turn and which the relay between instances carries.
- Observation: the job queue's rule of one open job per agent, kind and subject counts a running job as open, so a candidate arriving while the alert job runs cannot queue another. The job looks again when it finishes and puts itself back two minutes out (a `Deferral`) when anything arrived meanwhile; the same `Deferral` is how a night's hold waits for 07:00.
- Observation: a chat message steered into a turn of the agent's own that is already running is not followed by the chat, and that turn's answer is not relayed either, because its last opening message is the person's. It was so before this plan; left alone.
- Observation: a question card raised by a turn of the agent's own (the memory check asks some) is not sent to the chat; only the turn's last word is.
- Observation: the command line and the dashboard change a mailbox's policy by reading it and sending the whole of it back, so a field missing from their selection is dropped on the next save. For `alerts` that would have been harmless (absent is on) but silently undone an "off"; both selections now carry `alerts { enabled }`, and `ReadAgent`, `UpdateAgent` and `GrantAgentMailbox` joined `TestClientDocumentsMatchTheSchema`, which had not checked them.
- Observation (review, 2026-09-29): the alert job wrote into the main conversation whatever else ran there, so an alert could land between the person's message and its answer; a bot that stopped on its own left its relay running beside the next one; a re-linked or re-enabled bot sent the backlog since it last looked; a mute from an alert named the model's subject key, which the next alert could word differently; more than thirty held candidates hid an urgent one behind them while the follow-up re-ran the job every two minutes all night; a failed or budget-held job could alert on day-old mail; five people at one domain writing the same subject made a burst, and so did old mail moved in; bare host names, shorteners, email addresses and telephone numbers survived the alert text; and the model had no way to name which alert the person answered.

## Decision Log

- Decision: two kinds of candidate. A message triage judges worth telling now (a new insight field), and a burst: a program's count of similar messages (same sender domain and a subject that differs only in digits) in a window, which no model is asked to notice.
  Rationale: the single-message kind needs judgment and triage already reads the message; the pattern kind needs counting, which a program does exactly and cheaply.
  Date/Author: 2026-09-29.
- Decision: the alert itself is a separate job, not part of triage: candidates gather for two minutes, then one model call, with the person's memory of people and places and the list of what was already said, decides which to tell and writes one message for all of them.
  Rationale: triage runs per message on the triage model, without memory; the decision to interrupt someone needs the person's context (whose school it is) and needs to see several candidates at once so a burst becomes one alert. Gathering is what makes three messages from one incident one alert.
  Date/Author: 2026-09-29.
- Decision: bounds: at most five alerts a day; none between 22:00 and 07:00 in the person's zone unless the decision marks it as not able to wait (an account being taken over, a safety notice), in which case it is sent; otherwise it waits and is sent at 07:00 with whatever else waited. Every alert keeps a subject key (the sender and what it is about) and nothing with the same key is said again within a week unless it changed (the count of codes grew by half, a new notice).
  Rationale: an alert that arrives at 3 a.m. for a newsletter, or three times for one incident, teaches the person to ignore all of them.
  Date/Author: 2026-09-29.
- Decision: delivery is the main conversation plus every linked chat app. The chat app starts following agent-initiated turns and messages in the main conversation (alerts, speaking first, schedules delivered to the drawer, goal check-ins there), which fixes the gap for all of them at once. Web push is left for later.
  Rationale: the person's phone already has the chat app, which is where they read these; a push service needs a service worker and keys and is a plan of its own.
  Date/Author: 2026-09-29.
- Decision: on by default for mailboxes the agent triages, with a switch per mailbox and one for the whole agent; "don't tell me about these" from the person, in any door, becomes a triage memory the alert decision reads, scoped to the sender or the kind.
  Rationale: the point is that the person did not have to ask; the controls are for when it got something wrong.
  Date/Author: 2026-09-29.
- Decision: everything from mail is data: the alert prompt fences message text and says so, the alert may not act (no tools that send, move or change anything), and it names what it saw rather than repeating links or instructions from the message.
  Rationale: an alert is written from a stranger's text and read by the person as the agent's word.
  Date/Author: 2026-09-29.

- Decision: the model is shown candidates by short labels (`c1`, `c2`) rather than their identifiers, and the code maps them back; a candidate the decision did not mention is dropped with that said. The model's JSON keys stay snake_case like triage's (`alert_signal`, `subject_key`), and the stored fields are camelCase like the rest of the insight.
  Rationale: a model copies a two-character label reliably and a twenty-six-character one not always; the keys match the closest existing object.
  Date/Author: 2026-09-29.
- Decision: the daily cap drops what is over it rather than holding it for tomorrow; urgent alerts are placed first so the day's last places go to them. A held (night) alert keeps its candidates waiting and the morning's decision sees them again with whatever arrived overnight. A candidate that may not wait (triage said `now`, or a burst) brings a job held for the morning forward.
  Rationale: yesterday's sixth alert is not news in the morning; re-deciding in the morning costs one call and lets a burst that grew overnight be one alert.
  Date/Author: 2026-09-29.
- Decision: web addresses are taken out of an alert's text in code, not only asked for in the prompt; a candidate from a mailing list, a reply or a message in Junk or Trash is never a burst, and one older than a day (a backfill) is not a candidate at all.
  Rationale: an alert is read as the agent's word; a link in it from a phishing message is the phishing message delivered by the agent. A digest's volume and issue are the digits the pattern takes out, and a conversation going back and forth is not a burst.
  Date/Author: 2026-09-29.
- Decision: the chat app reads the agent's own turns back from the main conversation (`ListAgentOwnTurnAnswers`: assistant messages without tool calls whose latest opening user message carries an own-turn marker) past a cursor on the channel row, `relayed_through`, moved by compare-and-set before sending and only by the instance that holds the bot. It wakes early on the feed's `done` and otherwise every fifteen seconds, and only reads messages three seconds old, so one written in a transaction not yet committed is not skipped. A new link starts from the moment it is made.
  Rationale: the feed drops what a listener misses; an alert must not be. Moving the cursor before sending makes a failed send lost from the chat (it is still in the drawer) rather than sent twice.
  Date/Author: 2026-09-29.
- Decision: `isAlertingAllowed` (on wherever the mailbox is triaged and the triage feature is allowed) is the one place the switches of Milestone 4 will be read.
  Date/Author: 2026-09-29.
- Decision: the agent-wide settings are columns on `agent`, as `is_ideas_enabled` and `dream_from` are: `is_alerts_enabled` defaults to true (existing agents included), and `alert_quiet_start`, `alert_quiet_end` and `alert_daily_most` are empty and zero for the defaults (22:00, 07:00, 5), the convention `DreamFrom` and `AutoReply.DailyLimit` already use. The same time at both ends is no night. The mailbox's switch is `AgentMailbox.alerts`, a pointer in the policy's JSON whose absence means on, so every mailbox granted before it alerts without a migration.
  Rationale: the person's settings live where the neighbors' do; an absent value meaning on is what "on by default where the agent sorts" needs without rewriting stored policies.
  Date/Author: 2026-09-29.
- Decision: a mute is a row of a small table, `agent_alert_mute` (scope `sender`, `domain`, `subjectKey` or `kind`, and its target in lower case; unique per agent, scope and target), not a triage memory as first planned. The candidate step keeps a matching candidate and drops it at once with the reason `muted`, queuing nothing; the job drops a waiting candidate that a later mute matches before asking the model, and drops a decided alert whose subject key is muted. A domain matches its subdomains. A kind is `burst` or a category of the sorting, read from the message's insight.
  Rationale: a memory is prose the model is shown and can be argued out of by the next message; "stop" from the person has to hold, and has to be listable and taken back by id from three doors. Keeping the muted candidate makes the mute visible in what was dropped.
  Date/Author: 2026-09-29.
- Decision: muting from an alert takes the target from it: its subject key by default, or the sender, the sender's domain or the kind of the first message it covered. The agent's door is actions on `agent_profile` (`no_alerts`, `alerts_on`, `mute_alert`, `unmute_alert`) rather than a tool of its own, per the catalog's fuse; `mute_alert` with nothing named mutes the latest alert, which is what "don't tell me about these" after an alert means, and the alert's check-in line now names it.
  Date/Author: 2026-09-29.
- Decision: on the dashboard the Alerts card is on the Overview tab, where speaking first and ideas are switched, with a Mute button per alert opening a dialog to choose the subject, sender, domain or kind, and the muted list with Unmute below; the switch, the night and the day's most are a subform of the Advanced card beside speaking first; the per-mailbox switch sits under Sorting on the Mail tab, since alerts follow the sorting.
  Date/Author: 2026-09-29.

- Decision: the alert job checks for a turn in flight in the main conversation before the model is asked and defers a minute (`alertAfterTurn`) without deciding; each alert is written holding the lock Ask starts turns under (`whileNoTurnRuns`), after looking again. A turn that started while the decision ran leaves what was not said waiting, decided again next run, rather than keeping decided-but-unsaid alerts for later.
  Rationale: deciding again costs one call in a rare race; keeping undelivered decisions needs a store and rules for when they go stale.
  Date/Author: 2026-09-29.
- Decision: each run of a bot gives its relay its own context, cancelled and waited for when `Bot.Run` returns, so a channel has at most one relay.
  Date/Author: 2026-09-29.
- Decision: `PutAgentChannel` moves `relayed_through` to the newest message of the agent's main conversations (a fresh ULID when there is none) whenever the link, the linked sender or the token changes, or the bot is switched back on; the link code alone leaves it. This replaces "a new link starts from the moment it is made" in the relay.
  Date/Author: 2026-09-29.
- Decision: each `agent_alert` records what it covered in stable terms (migration 0127: `covered_burst_keys`, `covered_sender_addresses`, `covered_sender_domains`, `covered_mail_categories`). Muting an alert without a scope mutes its burst keys (scope `subjectKey`), or its sender addresses when it covered no burst; every target of the scope gets a row. `subjectKey` of an alert about a burst is its burst key. Alerts from before 0127 have their terms read from their candidates. The alert list offers `muteChoices`, the default first, which the dashboard shows. A target without a scope is read: a space or `|` is a subject key, an `@` an address (a leading `@` a domain), a dotted name a domain, else a subject key. The model's key is still matched against the mutes.
  Rationale: the model makes up the subject key each time; "stop" has to hold for the next message from the same sender or of the same burst.
  Date/Author: 2026-09-29.
- Decision: waiting candidates are read pressing first (signal `now`, or a burst), then oldest. When a run held alerts, only a pressing candidate created after the run read brings the next run forward; otherwise it waits for the morning. Without a hold, anything left unread brings it two minutes forward as before.
  Date/Author: 2026-09-29.
- Decision: the job drops, before anything else, waiting candidates made more than `alertFreshness` (a day) ago, and in its loop those whose message was received more than a day ago, with the reason saying so.
  Date/Author: 2026-09-29.
- Decision: a burst is keyed by the sender's address (not domain), counts messages by `mail.received_at` while the query stays bounded by `mailbox_item.added_at` for the `mailbox_item_list` index (an item is never added before its message was received), and ignores subjects with fewer than three letters once the digits are out.
  Date/Author: 2026-09-29.
- Decision: the alert text loses host names with or without a path, shorteners, email addresses and telephone numbers (seven to fifteen digits written with a plus or a separator, or ten long; not a date), each replaced by "(link removed)", "(address removed)" or "(number removed)". A service named without a domain stays.
  Date/Author: 2026-09-29.
- Decision: the check-in line names the alert's id and says to pass it as `alert_id`; the alert is created with that id.
  Date/Author: 2026-09-29.

## Outcomes & Retrospective

Milestones 1 to 3 (2026-09-29): candidates from triage and from bursts, the alert job with its bounds, delivery to the main conversation, the drawer and the linked chat app, which now also carries speaking first, schedules, goal check-ins and background wakes. Tests cover triage's new fields, burst counting with invented messages, gathering into one job, the job with a stubbed model (one alert for a burst, a dropped newsletter, the night's hold and the urgent exception, the cap, no repeat), the fenced prompt, and the chat sending each own turn once across a restart. Milestones 4 and 5 remain: the switches and mute, docs and the scenarios on the development server.

Milestone 4 (2026-09-29): the switches, the night and the day's most, and mutes, from the dashboard, the command line and the conversation. Tests cover the settings round trip and defaults through `UpdateAgent`, the night and the cap from the settings (including a night inside one day and none at all), mutes by sender, domain, subject key and kind dropping the next candidate as muted, a mute made after a candidate dropping it in the job without a model call, unmute, the switches as the gate, a mute taken from an alert, the GraphQL operations, the command line, the tool's actions and the client documents. Milestone 5 remains: deploy and the two scenarios.

## Context and Orientation

When mail arrives, `OnMailboxDelivery` (`internal/agent/agent.go`) queues jobs without calling a model: triage (`AgentJobTriage`, `internal/agent/triage.go`) when the mailbox's `AgentMailbox.Triage.Enabled`, a summary, an embedding. Triage runs `sortWithTools` on the triage model with a few read-only tools and files a `models.MailInsight` (category, priority high/normal/low, needs reply, summary, action items) through `fileInsight`, which runs insight rules and queues reply, research and extract jobs. Its prompt is `internal/agent/prompts/triage.txt`.

Speaking first (`internal/agent/speak_first.go`) is the agent taking a turn in the main conversation with nobody having asked, for onboarding, a memory check or an idea; it waits for the person to be present in the dashboard. Its message begins `[speaking first]`, and the drawer opens for it. Schedules (`internal/agent/schedule.go`) deliver to mail or the drawer.

The chat apps (`internal/channel`, one bot per person, run on one instance by a row claim) turn a chat message into a turn on the main conversation and follow that run's events to send the answer back (`turn` and `follow` in `channel.go`). `Chat.Send` sends a message to the linked chat.

Jobs are rows of `agent_job` (kind, subject, not before), claimed by workers; `Enqueue` refuses a second open job of the same kind and subject, which is how gathering works: the alert job's subject is the agent, its not-before two minutes after the first candidate.

## Plan of Work

Milestone 1. `MailInsight` gains `AlertSignal` (`none`, `soon`, `now`) and `AlertReason` (one line); `triage.txt` asks for it with examples of what is and is not worth interrupting for (a safety or health notice about their family, an account being accessed, money leaving that they did not expect, a deadline today or tomorrow, a delivery that failed; not newsletters, receipts of things they bought, ordinary replies). Migration adds the two columns to the insight table. A burst detector, run after triage files an insight (and cheap: one indexed count), counts messages in the same mailbox over the last six hours whose sender domain matches and whose subject matches once digits are removed; at five or more it makes a burst candidate. Candidates are rows of a new `agent_alert_candidate` table (agent, mailbox, mail id or burst key, kind, reason, created), and a candidate enqueues the alert job (subject: agent, not before: now plus two minutes).

Milestone 2. `AgentJobAlert` (`internal/agent/alert.go`): reads the unsent candidates, the alerts sent in the last week (a new `agent_alert` table: id, agent, subject key, text, sent at, candidate ids, conversation message id), the person's triage memories and the pages of people and places the candidates' senders or subjects match (memory search, read-only), and the candidates' messages fenced. One call on the triage model's betters (the `synthesize` slot, falling back to research) answers JSON: alerts to send, each with a subject key, `isUrgent`, the candidate ids it covers, and the text (at most 400 characters, the agent's voice, what happened and what to do, no links from the message); and candidates to drop. Code enforces: the daily cap (five, counted from `agent_alert`), quiet hours (22:00 to 07:00 in the person's zone unless urgent; otherwise the job is put back to 07:00), the week's subject keys (drop a repeat unless the model marks it changed with a reason). Sent alerts are appended to the main conversation as the agent's message with a note marker `[alert]` and surface `alert`, and recorded. Candidates the model dropped are marked dropped with its reason, readable later.

Milestone 3. The chat app follows the main conversation: when the agent appends a message in the main conversation that no chat turn asked for (surface alert, speak_first, schedule, goal, background), the channel sends its text to the linked chat. Find the event the drawer uses for new messages and subscribe the channel's runner to it, on the instance that holds the bot; a reply in the chat is an ordinary turn as today. The drawer opens for an alert like it does for speaking first.

Milestone 4. Settings: `AgentMailbox.Alerts.Enabled` (default on when triage is on) and agent-wide `IsAlertsEnabled` (default on) with `AlertQuietStart`/`AlertQuietEnd` (default 22:00/07:00) and `AlertDailyMost` (default 5), in GraphQL settings, the CLI (`teanode agent settings`, `teanode agent alert list|mute`), the agent tool (`agent_profile` actions `no_alerts`, `alerts_on`, and a `mute` that writes the triage memory), and the dashboard: a card on the agent page listing recent alerts with what they covered and a Mute for each, and the switches on the settings and mailbox pages. "Don't tell me about these" said in the conversation reaches the same mute through the tool.

Milestone 5: `docs/subsystems` (a section on alerts where triage is described, and the channel change), `docs/reference/command-line.md`, a decision record, deploy, and the two scenarios on the development server with invented test messages sent to the account's own mailbox and removed after.

## Concrete Steps

    go test -mod=vendor ./internal/agent/ -run 'Alert|Triage|Channel|Burst'
    go test -mod=vendor ./internal/channel/... ./internal/db/ ./internal/api/v1api/apigraph/ ./internal/client/ ./internal/cmd/...
    cd web && npx tsc --noEmit -p . && npx vitest run
    set -o pipefail; make lint-ci

## Validation and Acceptance

Milestone 1: triage of an invented illness notice sets `now` or `soon`; six invented code messages in an hour make one burst candidate. Milestone 2: the job writes one alert for the burst, drops a newsletter candidate with a reason, holds a non-urgent alert at 03:00 until 07:00, sends an urgent one at 03:00, stops at the daily cap, and does not repeat a subject key. Milestone 3: an alert, a speaking-first turn and a schedule delivered to the drawer each reach the linked chat once; a chat reply is answered as before. Milestone 4: muting from the dashboard, the CLI and the conversation each stop the next alert of that kind. Milestone 5: the two scenarios on the server.

## Idempotence and Recovery

The migrations only add columns and tables. A job re-run after a restart reads unsent candidates and the sent alerts, so it does not send twice what it already recorded; the record is written in the same transaction as the conversation message. Turning alerts off stops new candidates and leaves the history.

## Interfaces and Dependencies

    // models
    MailInsight.AlertSignal string // "none" | "soon" | "now"
    MailInsight.AlertReason string
    type AgentAlertCandidate struct { ID, AgentID, MailboxID, MailID, BurstKey, CandidateKind, CandidateReason string; CreatedAt time.Time; DroppedAt *time.Time; DropReason string }
    type AgentAlert struct { ID, AgentID, SubjectKey, AlertText, MessageID string; IsUrgent bool; CandidateIDs []string; SentAt time.Time }
    const AgentJobAlert AgentJobKind = "alert"

No new libraries.
