# A file is sent to the person's main chat

This ExecPlan is a living document. The sections Progress, Surprises &
Discoveries, Decision Log and Outcomes & Retrospective are kept up to date as
the work goes on.

## Purpose / Big Picture

A coding session working over MCP renders a chart, records a screen, writes an
HTML report or a PDF, and the person has to go and find the file on the
machine it was made on. The agent's own background work (a reminder, a goal, a
job) has the same problem: `share_file` hands a file to the conversation the
run is in, and over MCP or in a run transcript that is a conversation the
person never opens.

After this change any of them can send the person a file (a picture, a video,
an HTML page, a PDF, anything) by posting it into the person's main chat: the
conversation the dashboard drawer and the iPhone open on. It appears there as
a file from the agent, with a line saying what it is and who sent it, the
phone is notified, and a linked chat app (Telegram, Discord) gets the file
too.

To see it:

- From a coding session: `share_file` with `source: "computer"` and a path on
  an attached computer, or `source: "upload"`, which answers an `upload_url`
  that `curl -T report.pdf <upload_url>` sends the bytes to. The file shows in
  the main chat when the upload finishes.
- From the command line: `teanode agent send-file chart.png --caption "the
  week's spend"`.
- From the agent: "send me the invoice from that mail in my chat" in a side
  conversation, or a reminder that attaches a file.

## Progress

- [x] (2026-11-01) Plan written.
- [x] (2026-11-01) Migration 0266: an attachment's caption, who sent it, and when it is
  waiting to be posted.
- [ ] Server: post a file into the main chat (`Agent.SendFileToMainChat`),
  the sweep that posts a waiting one once no turn runs there, the upload link
  and its endpoint, phone push and the chat-app relay carrying the file.
- [ ] `share_file`: `to` (`here`, `main_chat`) and `source: "upload"`.
- [ ] GraphQL `SendAgentFileToMainChat`, CLI `teanode agent send-file`.
- [ ] Dashboard and iPhone: an HTML file sent this way opens in the sandbox an
  artifact opens in; a PDF opens in place.
- [ ] Docs, tests, review, deploy, use it for real.

## Surprises & Discoveries

- Over MCP a run has no conversation, so a file `share_file` made there had
  no conversation and was never swept (its message id is `shared`). Files
  made over MCP now go to the main chat, so that gap closes.
- The MCP request body is capped at 1 MiB and a program's approval token works
  only at the tools endpoint, so bytes cannot travel as a tool argument (a
  model would also have to read them as text). Hence the upload link.

## Decision Log

- Decision (the person, 2026-11-01): unattended runs (reminders, goals,
  background jobs, coding sessions over MCP) post a file to the main chat
  without asking; each file says who sent it, and no run nobody is present
  for makes a link that opens without sign-in.
- Decision (the person, 2026-11-01): a file sent to the main chat may be up to
  100 MB, a setting (`agent.limits.maxSentFileBytes`).
- Decision (the person, 2026-11-01): an HTML page sent this way opens in place
  in the artifact sandbox (scripts run, no network, no session); a PDF opens in
  the browser's viewer. Before, both downloaded.
- Decision: extend `share_file` with a destination instead of adding a tool.
  Rationale: one tool already hands the person a file from mail, a computer or
  the conversation; the destination is the only new idea. The main chat is
  asked for, never the default: over MCP `share_file` is also how a program
  fetches a file off the person's computer for itself, and that must not post
  into their chat.
- Decision: a file waiting for the main chat is an `agent_attachment` row with
  `main_chat_queued_at` set; the agent's tick posts it once no turn runs
  there. Rationale: a turn in progress cannot be written into, and the upload
  completes in an HTTP handler that should not wait for one.
- Decision: the main chat shows the file the way it shows a file `share_file`
  handed over in a turn: a marker line (`[file sent]`, one of
  `models.OwnTurnMarkers`), an assistant line with a `share_file` tool call, the
  tool line with the file, and the assistant's sentence. The dashboard, the
  iPhone, push and the relay already read that shape.
- Decision: the upload link is signed with the server's secret like a shared
  file's, names one waiting attachment, takes one PUT within an hour, and
  grants nothing else.

## Outcomes & Retrospective

(Filled in when the work lands.)

## Context and Orientation

- `internal/agent/tools/share/share.go`: `share_file`, sources mail, computer
  and conversation, cap 32 MiB.
- `internal/agent/finance_statement.go` `tellAgent`: writes a pair of rows into
  the main conversation while no turn runs, and publishes the events.
- `internal/agent/phone_push.go` and `internal/channel/relay.go`: push and the
  chat-app relay pick up the agent's own turns in the main chat by
  `models.OwnTurnMarkers`; both send text only today.
- `internal/agent/share.go`: the signed, expiring address of one file.
- `internal/api/v1api/apigraph/agent_attachment.go`: upload (multipart, the
  person's session) and download; HTML and PDF are served as downloads.
- `internal/agent/direct.go`: a call over MCP (`directRun`), no conversation.
- Web `web/src/components/agentDrawer.tsx` (`sharedFilesOf`, `FileCard`,
  `ArtifactCard`), iPhone `ios/TeaNodeKit/Sources/TeaNodeKit/Media.swift`
  (`AgentFile.of(toolResult:)`) and `MediaViews.swift` (`ToolFiles`).

## Plan of Work

1. Migration `0266_files_sent_to_main_chat.sql` (and reverse): on
   `agent_attachment`, `caption text not null default ''`, `sent_by text not
   null default ''` (who sent it, shown on the line: "a coding session over
   MCP", "a reminder"), `main_chat_queued_at timestamptz null`, an index on the
   waiting ones.
2. `Agent.SendFileToMainChat(ctx, attachmentId)`: marks it waiting and tries
   at once; the tick (`postWaitingFiles`) retries what a running turn held up.
   It writes the four rows into the main conversation, sets the attachment's
   conversation, clears the mark, and publishes.
3. Push: `ListAgentOwnTurnAnswers` already picks the sentence; the push names
   the file. Relay: after the text, send each file of the exchange the way
   `sendArtifacts` sends one.
4. Upload: `share_file` `source: "upload"` with `name` (and optional
   `content_type`) makes a waiting row with no bytes and answers `upload_url`
   (`PUT /api/v1/agent/attachments/{id}/upload?upload=<signed>`). The endpoint
   checks the signature and that the row has no bytes yet, reads at most the
   cap, stores, sets size and type, and posts it. A row never uploaded is
   removed by the sweep after the link expires.
5. `share_file` `to`: `here` (today's behavior, the default) or `main_chat`.
6. GraphQL `SendAgentFileToMainChat(attachmentId, caption)` for a file the
   person uploaded with the existing endpoint; client and `teanode agent
   send-file <path>... [--caption]`.
7. Serving: a file sent this way that is HTML is served with the artifact
   sandbox policy, inline; a PDF inline. The dashboard and the iPhone show an
   HTML one in the artifact card.
8. Docs: `docs/subsystems/conversations.md`, `docs/subsystems/mcp.md`,
   `docs/reference/command-line.md`; an ADR,
   `docs/decisions/20261101-a-file-can-be-sent-to-the-main-chat.md`.

## Validation and Acceptance

- Agent tests: a file sent while a turn runs is posted after it; an upload
  link works once, for its file only, and not after it expires; a run with no
  conversation sends to the main chat; the rows read back as a shared file.
- apigraph tests: the upload endpoint refuses a bad or used signature and a
  file over the cap; HTML sent this way carries the sandbox policy.
- CLI parity test for `send-file`.
- Live: from this machine's coding session over MCP, send an HTML page, a PDF,
  a picture and a video; each shows in the drawer and on the iPhone, the phone
  is notified.

## Idempotence and Recovery

The migration only adds columns and an index; the reverse drops them. A
waiting file is posted once: the mark is cleared in the same transaction that
writes the rows.

## Interfaces and Dependencies

- `func (self *Agent) SendFileToMainChat(ctx context.Context, attachmentId string) error`
- `tools.MainChatSending` on a run, implemented by every run type, so the tool
  can post to the main chat whatever run it is in.
- GraphQL `SendAgentFileToMainChat(attachmentId: String!, caption: String): AgentAttachment`
