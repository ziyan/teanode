# Watched mail: noticing what arrives in a mailbox TeaNode does not host, through the skill that already reads it

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.


## Purpose / Big Picture

The person's agent tells them about mail worth knowing about (a school closing early, money leaving that they did not expect) without being asked: the sorting marks a message `soon` or `now`, and the alert job decides what to say in the main conversation (`docs/planning/mail-alerts-execplan.md`). That works only for mailboxes TeaNode hosts. A person whose real mail lives in Gmail, and who has the `gmail` skill installed and pointed at their own computer, gets nothing: the agent can search and read that mailbox when asked, but never looks on its own. Other personal agents notice such a message with no setup at all, and the person expects the same.

After this plan the agent looks at the person's Gmail every ten minutes, through the same two tools of the installed skill that a conversation uses (`gmail_search` and `gmail_read`), on the computer the person chose for that skill. Each message that arrived since the last look, archived or not, is sorted with the same prompt TeaNode's own mail is sorted with, and a message the sorting marks `soon` or `now` becomes an alert candidate. The alert job then treats it exactly like one from a hosted mailbox: the person's quiet hours, daily limit and mutes apply, and the alert names the sender and the subject. Nothing new reaches Gmail: no token, no other program, no copy of the mailbox; only what the sorting needs is kept, and only with a candidate.

To see it working: with the `gmail` skill installed and enabled, a computer attached that has `gog` signed in, and alerts on, send the Gmail address a message such as "The school is closing at noon today because of the storm" and archive it at once. Within ten minutes the agent's activity shows a run titled `Sorting Gmail "…"`, and within a few more minutes the main conversation has an alert about it.


## Progress

- [x] (2026-10-06) Surveyed how the sorting, the alert candidates and the alert job work, how a skill's command runs on a computer (`skills.Skill.Run`, `computerShell`), and what the installed `gmail` skill's two read tools print (`gog gmail search … --json`: threads with id, date, from, subject, labels, messageCount; `gog gmail get … --json`: headers and the decoded body).
- [x] (2026-10-06) Milestone 1: storage. Migration 0150: `agent_watched_mail` (what has been looked at), and the columns a candidate about a watched message carries on `agent_alert_candidate`.
- [x] (2026-10-06) Milestone 2: the `watch` job: search, read, sort, candidates. Queued by the worker's tick every ten minutes for each agent with the skill enabled, alerts on and a computer attached.
- [x] (2026-10-06) Milestone 3: the alert job, the alert's subject key and its covered list read a watched candidate; mutes match its sender, domain and category.
- [x] (2026-10-06) Milestone 4a: tests (parsing, the window, a look end to end with a fake computer and model through to the alert, a muted sender, the table), docs (`agents.md`, `jobs-and-schedules.md`), the dashboard's run kind.
- [ ] Milestone 4b: review, merge, deploy, and a look on the server against the real skill.


## Surprises & Discoveries

- `gog gmail search` lists threads, not messages. A thread with one message has the message's id as its own; a reply to an old thread is a new message in a thread whose id is the first message's. The watch reads such a thread to learn its messages' ids and dates, then reads each new one.
- The skill's read prints the body as the sender wrote it, usually HTML; it is reduced to text with the same function the tools use (`tools.HTMLToText`).


## Decision Log

- Decision: the watch uses only the installed skill's own tools, run through `skills.Skill.Run` with the computer shell, exactly as a conversation runs them. It does not use the `gmail-gog` knowledge source and does not add a way of reaching Gmail.
  Rationale: the person said Gmail is already a skill and the agent should do what the skill is configured to do. The skill is what the person installed, pointed at a computer, and can switch off; the watch stops when they do.
  Date/Author: 2026-10-06.
- Decision: the watch runs with nobody present, which `computer.Of` refuses. It finds the computer itself (the skill's reach, else the only one attached) and runs only the two read tools, `gmail_search` and `gmail_read`; it never sends, drafts or labels.
  Rationale: the rule against acting on a computer unattended exists because a turn could do anything there. The watch is code, not a turn: what it runs is fixed and reads only.
  Date/Author: 2026-10-06.
- Decision: one look every ten minutes, from the newest message already looked at less ten minutes (or two hours back on the first look), leaving out sent mail, drafts, spam, trash, chats, and Gmail's promotions and social tabs. At most 25 threads (the skill's own limit) and 25 messages a look.
  Rationale: ten minutes is fast enough for "now" news and cheap: each new message costs one sorting call. The overlap catches a message Gmail indexed late; the row per message id keeps it from being sorted twice. The tabs left out are what the sorting would mark `none` anyway.
  Date/Author: 2026-10-06.
- Decision: a watched message is sorted with `TriagePrompt` and `InterpretTriage`, in one call without tools, and nothing else follows from the sorting: no insight row, no rules, no reply, no research, no extract.
  Rationale: those all act on a message TeaNode holds. Here the only question is whether the person should hear about it.
  Date/Author: 2026-10-06.
- Decision: a candidate about a watched message is a new kind, `watched`, that carries what the alert job needs in its own columns: the skill, the message's id, sender, subject, date, the sorting's category, and the message as the sorting saw it (at most 4,000 characters). It is dropped like any other when stale, muted or decided.
  Rationale: the alert job reads a hosted message from storage; a watched one is not stored, and keeping only what the decision reads, only for a candidate, is the least that works.
  Date/Author: 2026-10-06.


## Outcomes & Retrospective

(To be written when the work is done.)


## Context and Orientation

- `internal/skills/run.go`: `(*Skill).Run(ctx, toolName, arguments, running)` runs a tool; a shell step answers `{"text": what was printed}` (cut at 256 KiB).
- `internal/agent/tools_skill.go`: `computerShell` runs a command on an attached computer; `reachOf` reads which computer a skill uses.
- `internal/agent/computer.go`: `computersFor(agentId)` lists the attached computers.
- `internal/agent/triage.go`: `TriagePrompt`, `InterpretTriage`.
- `internal/agent/alert_candidate.go`, `alert.go`, `alert_mute.go`: candidates, the alert job, mutes and the covered list.
- `internal/agent/agent.go`: `tickAt` queues periodic work; `Register` binds a job kind to its handler.


## Plan of Work

Milestone 1 adds `internal/db/migrations/0150_agent_watched_mail.sql` and its reverse, `models.AgentWatchedMail`, `models.AlertCandidateWatched` and the candidate's new fields, and `internal/db/database_watched_mail.go` with `HasAgentWatchedMail`, `AddAgentWatchedMail` and `LatestAgentWatchedMailAt`.

Milestone 2 adds `models.AgentJobWatch` and `internal/agent/watched_mail.go`: `queueWatching` on the tick, `runWatch` the handler, the parsing of the skill's two answers, the sorting call and `noteWatchedCandidate`.

Milestone 3 teaches `runAlert`, `renderAlertCandidate`, `candidateFacts` and `coveredByAlert` the new kind.

Milestone 4: unit tests for the parsing, the query and the candidate; a database test for the new table; `docs/subsystems/jobs-and-schedules.md` and the alerts docs; a run on the dev server.


## Validation and Acceptance

`make test` passes. On a server with the skill installed and a computer attached that has `gog` signed in, a message sent to the Gmail address and archived is sorted within ten minutes (a run titled `Sorting Gmail "…"`), and a message the sorting marks `soon` or `now` produces an alert in the main conversation.


## Idempotence and Recovery

The migration only adds a table and columns. A look that fails is retried by the job's ladder; a message is marked as looked at only after its sorting is written, so a failure halfway sorts the rest on the next look.
