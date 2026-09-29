# Alerts: telling the person, unasked, what their mail says they should know now

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent reads every message that arrives, sorts it, and says nothing. A person whose child's daycare writes that a child in the class has croup, or whose phone number has received fourteen sign-in codes overnight that they did not ask for, finds out when they next open their mail, if they notice at all: the second is fourteen messages each of which looks routine.

After this plan the agent tells the person, on its own, when mail shows something they should know now. It writes a short message into the main conversation, in its own voice ("Heads up: someone has been requesting sign-in codes to your number, about fourteen since yesterday morning, most of them overnight. If that wasn't you, check your login activity and change the password."), and the message reaches them where they talk to the agent: the dashboard's drawer and the chat app they linked (Telegram or Discord). It is bounded so it is worth reading: a few a day at most, none at night unless it cannot wait, never the same thing twice, and the person can say "don't tell me about these" and be heard.

To see it working: send the account's own mailbox a message that reads like a school notice about an illness in the person's child's class (invented for the test), and within a couple of minutes a message about it appears in the main conversation and in the linked chat. Send six messages that look like sign-in codes from one service within an hour, and one alert arrives about the pattern, not six.

## Progress

- [x] (2026-09-29) Surveyed triage, speaking first, schedules, the chat apps and the injection stance; wrote this plan.
- [ ] Milestone 1: triage says whether a message is worth telling now, and a program notices a burst of similar messages.
- [ ] Milestone 2: the alert job: gather candidates, decide with the person's memory, write one short message, bounded by quiet hours, a daily cap and a record of what was said.
- [ ] Milestone 3: delivery: the main conversation and the linked chat app, which also starts receiving the agent's other unasked turns.
- [ ] Milestone 4: control: a setting per mailbox and for the whole agent, quiet hours, the list of recent alerts, and "don't tell me about these" from any door.
- [ ] Milestone 5: docs, deploy, and the two scenarios above on the development server.

## Surprises & Discoveries

- Observation: nothing the agent starts on its own reaches a chat app today. `internal/channel/channel.go` follows only the turns a chat message started; speaking first, schedules and goal turns write into the main conversation and stop there. There is no web push either.
- Observation: triage's prompt files a sign-in alert as a notification of low priority, which is right for one code and wrong for fourteen in a night. The signal is the pattern, which no single message's triage can see.
- Observation: triage has no memory of people beyond memories addressed to its audience and the contact book, so it cannot know that a school is the person's child's.

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

## Outcomes & Retrospective

Nothing yet.

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
