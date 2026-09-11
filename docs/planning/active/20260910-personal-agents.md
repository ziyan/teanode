# Personal agents: the LLM core, the agent, and the mailbox as its first source

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up
to date as work proceeds. It is plan A of
`20260910-personal-agents-roadmap.md`, and it is written to be followed by
somebody who has only this file and the working tree.

## Purpose / Big Picture

After this plan, a person on this server can turn on an agent of their own,
give it a name and standing instructions, and grant it their mailbox. From
then on, every message that arrives in that mailbox is sorted — given a
category, a priority, and a verdict on whether it needs an answer — and the
mailbox's rules can act on those; conversations get a summary at the top of
the reader; the composer can draft a reply on request; and, if the person
turns it on and sets the policy, the agent answers mail on their behalf,
holding each reply for a few minutes so it can be cancelled. The person can
talk to their agent from a drawer on every page, from a page of its own, or
from the command line, and it can do anything they could do themselves:
search and file their mail, write and send, change their rules, and — for an
operator — add a domain, create a user, read the audit log. It can look
things up on the web, use tools on servers the operator has connected, and
drive a browser. It remembers what it is told, learns from corrections, and
can be asked to do something every Monday at eight.

An operator configures the model providers, decides which features the
deployment offers, sets limits, and watches usage; they cannot turn anyone's
agent on, and they see tokens, never content.

You can see it working like this. Configure a provider on the server
settings page and test the connection; the model list appears. Open the
Agent page as a person, turn the agent on, grant the mailbox. Send that
mailbox a newsletter from outside: within a worker tick it carries a
`newsletter` chip in the list, and a rule "category is newsletter → move to
Reading" files it. Open a five-message thread: a summary sits above it. Ask
in the drawer "what did the plumber say about Thursday?" and get the answer
with the message linked. Turn on answering for known contacts, send a message
from a contact's address, and watch a held draft appear with "Replying in 10
min — Cancel"; let it go, and the reply lands in Sent with
`Auto-Submitted: auto-replied`.

## Progress

Each milestone is a checkbox; sub-steps are listed as they are started.
Timestamps are UTC.

- [x] (2026-09-10 15:40Z) Milestone 1 — `internal/llm`, configuration, settings section. `internal/llm` with the three clients, registry, structured extraction and token estimate; `config.Agent` with defaults, validation, redaction and the `agent` section stored; `AgentSettings`/`AgentParameters` with `TestAgentProvider` and `ListAgentModels`; the Agent tab on `/server`; `docs/configuration.md`; `openAgent` returning nil when off. `make test` 1010 passed, `make lint-ci` clean.
- [x] (2026-09-10 17:10Z) Milestone 2 — queue, worker, the agent and its sources, permissions, usage and limits. Migration `0031_personal_agents`; `models.Agent` / `AgentMailbox`; `database_agent.go` (queue with `SKIP LOCKED`, usage rows, location touch); `agent:use` / `agent:audit` seeded; `internal/agent` worker with the retry ladder, deferral on budget, the delivery hook via `mx.AgentHook`; `apigraph/agent.go` (person and operator operations), zone and language learned from the request; `teanode agent …`; the Agent page, the mailbox's Agent tab, the profile's time zone, the Agents tab on `/server`. `make test` 1012 passed, `make lint-ci` clean. Notifications by mail and dashboard are wired in milestone 6 with the held reply, which is the first event worth telling.
- [x] (2026-09-10 19:30Z) Milestone 3 — triage. Migration `0032_agent_insight` (`mail_insight`, `agent_conversation`, `agent_message`); `database_insight.go`; `context.go` reduces a stored message (HTML boiled down, quoted history cut, attachments by name and type, capped); `prompts/system.txt` and `prompts/triage.txt` with golden files under `testdata/prompts`; `triage.go` asks for one JSON object, writes the insight, records the tokens and a `run` transcript, then runs the rules that were waiting; `backfill` per the source's choice on grant. Rules gained `category`, `priority` and `needs-reply` conditions and a second phase (`RunInsightRules`). `ListMailboxItems`/`ListMailboxThreads` filter by category, priority and needs-reply and every item carries `insight`. Dashboard: chips on the rows, the Priority view beside Starred (shown when the mailbox is sorted), the three conditions in the rules editor. `make test` 1018 passed, `make lint-ci` clean, `make web` builds. Memory and corrections blocks in the prompt stay empty until milestone 9.
- [x] (2026-09-10 20:40Z) Milestone 4 — summaries. Migration `0033_thread_summary`; `models.ThreadSummary`; `PutThreadSummary`/`GetThreadSummary`/`DeleteThreadSummaries`; `summarize.go` rewrites the previous summary with only the messages since it, capped, and leaves a transcript; queued at delivery once a conversation reaches the source's minimum, and by `GetMailboxThread` when the reader opens a conversation the summary has fallen behind on (coalesced); `MailboxThreadView.summary` with `stale`/`pending`; the reader shows it folded above the messages and asks again while one is pending. Revoking a mailbox or forgetting the agent now deletes its insights and summaries. `make test` 1021 passed, `make lint-ci` clean.
- [x] (2026-09-10 21:10Z) Milestone 5 — draft replies. `draft.go` and `prompts/draft.txt`: the full conduct (voice included), the instruction line, the conversation's summary when it reads through the message or the last six messages otherwise, research notes when there are any; `DraftReply(itemId, instructions)` runs in the request through `api.AgentService`, records usage as kind `draft` and a transcript; `teanode agent draft`; the composer offers *Draft with agent* with a one-line box when replying in a mailbox granted with drafts on, and puts the text into the editor above whatever is there — sending stays with the person.
- [x] (2026-09-10 23:30Z) Milestone 6 — auto-reply. Migration `0034_agent_reply`; `models.AgentReply` (held, sent, cancelled, refused, failed) and `database_reply.go`; `reply.go` climbs the ladder — the out-of-office refusals through `Exchange.AutoReplyRefusal` with the source's quiet days, then never/scope/list, category, when (hours in the person's zone, away), the person already replied, a held reply exists, the day's limit, the budget — then asks the model under `prompts/reply.txt` (`{reply, reason}`; a decline is recorded with its reason), composes the draft through the mailer, files it in Drafts in the conversation with the composer's own headers, and queues `send` for when the hold ends; `send.go` climbs again, claims the sender's quiet period, sends as the person with `Auto-Submitted: auto-replied`, `In-Reply-To`/`References`, the audit actor `agent`, removes the draft, flags the message answered. A draft saved or discarded by hand takes the reply over. API `ListAgentReplies`, `CancelAgentReply`, `MailboxThreadView.heldReply`; CLI `teanode agent replies [cancel]`; the reader's banner with Cancel and Take it over; the Replies card on the agent page showing every refusal and its reason; notifications by mail from the source mailbox when the person asked for mail. Every refusal is recorded as a reply row, so an unanswered message is never a mystery.
- [x] (2026-09-11 02:30Z) Milestone 7 — Ask. `tool.go` (risk classes, families, the catalog filtered by permissions and the operator's `agent.tools`, per-call risk for tools whose actions differ, confirmation as the floor plus the two lists, deferral behind `tool_search` above forty); `operations.go` (`Operations`: the API executed as the person, implemented in `apigraph/agent_ask.go` with the audit actor `agent`); `tools_mailbox.go` (mail_search with keyword and meaning merged, mail_read, mail_act, mail_draft, mail_send, mail_compose_help, folder_list/manage, rule_list/add/update/remove/test/apply, mailbox_settings, contact_search, reply_queue with the `<pending>` overlay); `tools_general.go` (datetime, web_fetch through `safefetch`, web_search via Brave, tool_search); `ask.go` (conversations, the layered prompt in `prompts/ask.txt`, the situation computed each round, overlays `<viewing>`, `<surface>`, `<pending>`, `<budget>`, `<now>`, streaming with a fallback, compaction with a resume point in `prompts/compact.txt`, titles from the fast model, the confirmation pause with a ten-minute wait, stop); embeddings (`0035_mail_embedding`, `embed.go`, backfill on grant, cosine in Go with a floor); API `AskAgent`, `ResolveAgentConfirmation`, `StopAgentRun`, `ReadAgentRun` (long-poll), conversation and run operations, `ListAgentTools`, the first subscription `AgentRunEvents` over the existing websocket; the drawer on every page; `teanode agent ask|chat|conversation|run|tools`. Memories in the prompt, the `<todo>` and `<recalled>` overlays wait for milestone 9; `<tab>` for 11.
- [x] (2026-09-11 04:10Z) Milestone 8 — operator tools. `tools_operator.go`: Family 2 (domain_list/get/add/update/remove, domain_dns_check, alias_list/add/update/remove/match, credential_list/create/update/remove, queue_list/retry), Family 3 (mail_audit_search over the audit's pipeline, mail_audit_get, mail_audit_content, mail_audit_mark, report_list), Family 4 (user_list/add/update/remove, group_list/manage, role_list/manage, audit_log), Family 5 (server_status, settings_get/update, server_upgrade), Family 6 (account_get/update, token_manage, session_revoke, app_password_manage) and `access_explain`; every one over `Operations`, offered by permission, deferred behind `tool_search` since the catalog now passes forty; secrets a tool makes come back `show_verbatim`. The operator's `agent.tools` lists were already in the settings section since milestone 1; the audit page marks a row the agent wrote *via their agent*; `teanode agent tools` lists the catalog. A test reads every document out of the agent's source and validates it against the schema, as the client's are.
- [x] (2026-09-11 06:00Z) Milestone 9 — memory, corrections, schedules, questions, todo. Migration `0036_agent_memory` (`agent_memory`, `agent_feedback`, `agent_schedule`, `agent_todo`); `database_memory.go`; the `memory` tool and the top of memory folded into every prompt by audience (twenty for the conversation, thirty for a run), with `<recalled>`; corrections recorded from the person's own hands — a sorted message moved by them (`MoveMailboxItems`, never the agent's own moves), a held reply cancelled or taken over — shown to triage and reply as examples and swept by `retention.corrections`; schedules with a five-field cron read in the person's zone, queued by the tick, run headless through an `OperationsFactory` the API provides, delivered by mail or into the main conversation, with the `schedule` tool; `ask_user` with a question card in the drawer and on the terminal; `todo` with the `<todo>` overlay and the drawer's checklist; the hourly sweep of old runs, jobs, replies and corrections; the agent page's memory, schedule and correction cards; `teanode agent memory|schedule|feedback`.
- [x] (2026-09-11 08:30Z) Milestone 10 — connected servers and research. `internal/mcp` (JSON-RPC client; streamable HTTP with the session id and server-sent events; a subprocess over stdio; OAuth 2.1 with PKCE, metadata discovered from the protected resource or the origin, refresh) tested against in-process servers, the stdio one being the test binary itself. Migration `0037_agent_connection`; `tools_mcp.go` — per person and server, discovery cached five minutes and withdrawn after three failures, tools named `mcp__<server>__<tool>`, outward unless the operator listed them read-only, disabled by name, `Headless` for runs with nobody present, credentials and tokens sealed with the server secret; `research.go` — a headless turn over the fixed read-only set plus headless read-only remote tools, at most `maxRoundsPerResearch` rounds, whose answer becomes `mail_insight.notes` for the reader and the reply. API `ListAgentServers`, `ConnectAgentServer` (probed at once), `DisconnectAgentServer`, `BeginAgentServerOAuth`/`FinishAgentServerOAuth` with the dashboard's `/agent?connect=` return; the servers card on the agent page; `teanode agent mcp`. The reply run stays one call and reads the research notes rather than running tools itself — see the decision log.
- [x] (2026-09-11 10:30Z) Milestone 11 — browser. `internal/browser` (a DevTools client over websocket; `Connect` from `http://host:9222` or a `ws://` address; a fresh isolated context per turn with downloads denied and every request checked by `Fetch.requestPaused` against the fetch guard plus `allowPrivateAddresses`; the snapshot script with `[ref=N]` on everything interactive; click, hover, select, type — refusing password and payment fields in the page itself — press, scroll, wait, back, evaluate, screenshot) tested against a fake Chrome. `tools_browser.go`: the one `browser` tool with its actions and `steps`, per-turn contexts closed when the turn ends or after `idleTimeout`, at most `maxContexts` at once, offered only where `agent.browser` is on, the reading subset alone for a run with nobody present. The tab relay: `tab.go`, the `/api/v1/agent/tab` websocket (session cookie, CSRF token, protocol version), `target: tab` carrying actions to the extension and answers back, the `<tab>` overlay, `ReadAgentTab` and a line in the drawer, `attachTabs` to switch it off. The extension under `web/extension/` (manifest v3, no build step) does the acting in the page and enforces its own refusals: password and card fields, forms that pay or change credentials without `confirmed`, fetch limited to the attached site, storage of the attached site only. The compose file gains a `chrome` service under the `browser` profile. Subscriptions over the websocket now run as the signed-in person, which the run events needed.
- [x] (2026-09-11 03:20Z) Milestone 12 — the conversation, held well. Turns of one conversation run in order (`Agent.latest`, a queued run waiting on the previous run's `done`, the note "queued behind the turn before it", `AskRun.Queued`); a stopped turn keeps the streamed words and a `note` row saying "stopped"; `agent_attachment` (`0038`), `AttachmentOperation`, `POST/GET /api/v1/agent/attachments`, `AskAgent(attachmentIds, references)`, `userTurn`/`historyTurn` in `attachment.go` (images as parts in the turn they came with, text files inlined and capped, the rest named), orphans swept after a day, files gone with the agent; `<references>` from the reader's "Ask the agent about this"; the drawer's paperclip, drop and paste, chips, thumbnails, tool lines that open, per-turn usage, the two switches, rename, list refresh; `teanode agent ask --attach`. Verified in Chrome: two turns queued and answered in order, Stop mid-essay with the words kept after a reload, a picture described and a text file read, a video named as unopenable, a thread referenced from the toolbar, tool detail and usage shown, a conversation renamed. After review with the owner: the drawer floats over the page again (a column squeezed the reader), with a solid ground — `--card` was never a token, which is why it showed the page through; the picker is a menu with a chevron that turns; the two switches live in the account menu (`agentPreferences.ts`); the box starts one line tall and Send is a round mark; tokens read as 13.5k; a tool's JSON is coloured with a copy mark and no labels; the server's `Agent` and `Agents` tabs are one `Agents` tab, settings above the people. Audit against the dashboard's design rules (2026-09-11): the agent page and the operator's list rebuilt on `SettingsSection`/`SettingsRow`/`FormDialog`/`SaveRow`, narrow forms, icon row actions, empty states; the agent page moved to `/settings/agent`, the profile page became `/settings/preference` and holds the two switches; the "Tell me when" card went (no notification system yet). From the feature inventory: conversations titled after the first exchange and described — title and summary — by a worker once quiet for three minutes with something new said (`describe.go`, `0039`, `0040`; never after every turn, never on a fixed count), found by words (`SearchAgentConversations`), deleted after a confirmation; timestamps, day dividers, thinking dots, pinned scrolling, a jump-to-end mark, drafts kept per conversation, an IME guard on Enter; `artifact` and `chart` tools with a sandboxed, inline-served page and a server-drawn SVG; memory and schedule editing, `@at` one-shot schedules, an activity list that opens a run in the drawer; a fuller Markdown renderer (fences with copy, numbered lists, tables, quotes). Turns in one conversation run in order (a second turn queues behind the first, and says so); a stopped turn keeps the words that had come; attachments — photos read by the model, text files inlined, everything else named — uploaded to the spool and shown in the transcript; a thread referenced from the reader with one click; tool calls that open to their arguments and results, and per-turn usage, each behind a switch the person sets once; the picker refreshes and renames.
- [x] (2026-09-11 01:30Z) Audit round with the owner, on production. The operator's Agents tab rebuilt as one card per subject with its own Save — providers and connected servers as rows with an edit dialog, Test on the row, Remove after a confirmation, models picked from what the providers offer with room for a name the list lacks, the persons' choices as chips; the agent page likewise — About me, Voice, categories as rows, each source a block with Grant/Revoke and four small forms (sorting, summaries, replies and search, answering), Ask me first as a form; forms reset on the values, never on the object, since the page polls. Across the dashboard every native `<select>`/`<datalist>` became `Select` or `Combobox`, every inline error or "saved" became a toast, and text-link actions became buttons. The drawer: Open on an activity row loads that run (the id was set after the drawer opened, so the remembered conversation won); the reference chips sit under the rule, above the box; a two-word bubble is no longer as wide as its hidden date; the star is a star; "AI can make mistakes." The agent: the time left the system prompt (it defeated caching — see surprises); `web_search`, `web_fetch` and `subscription` are core; `tool_search` says it finds tools, not facts, and the conduct says to search for a tool before saying no; `@in 20m` one-shot schedules. Production's main conversation read end to end for the confusions that drove these. Then every page this work touched was looked at through a headless Chrome driven over the DevTools protocol (a bearer token in a header, device metrics for 1400 and 390 wide, `prefers-color-scheme` both ways, Accept-Language en/ja/zh, zones New York, Tokyo and Shanghai): the operator's Agents tab, the agent page, the mailbox's agent tab, Preferences, the inbox, the reader, the composer, the rules, the drawer with its picker open. Found and fixed: the web-search provider dropdown squeezed to its chevron; the agent page's "Replies and search" heading used twice; the composer's recipient suggestions dropping every contact over the form on load (they wait for a keystroke now); tool lines on a phone spaced like paragraphs by the phone's taller buttons. Nothing overflowed sideways at 390 px. The extension's own screenshots hung on unchecked checkboxes, which is why the headless route was needed.

## Surprises & Discoveries

- A `models.Mail` read back from the database has no headers: the header
  block lives in the spool with the body, and only a message freshly parsed
  at delivery carries `Headers` in memory. The reply ladder, which reads To,
  Cc, Auto-Submitted and the list headers, refused every message with "none
  of the mailbox's addresses is in To or Cc" on the first end-to-end run
  (2026-09-10, dev server, run `01m26nt5333yh85geeesn2yncn`). Every run
  that reads headers now loads them from the spool first (`LoadHeaders`).
- Roles seeded before a release do not gain the permissions the release
  adds: on the dev database the Member role had no `agent:use` and nobody
  could reach the agent after upgrading. Migration `0031` now gives the
  seeded roles, by name, the two agent keys — once, so that an operator who
  later takes one away is not overruled at every start.
- The websocket subscriptions had no principal — the endpoint predates any
  real subscription — so `AgentRunEvents` refused everyone until the socket
  learned to resolve the signed-in person the way a request does. And a
  subscription resolves in a goroutine of its own, after the transaction
  the socket opened for it has been committed, so the resolver opens one
  of its own rather than reading the request's.
- The drawer's `viewing` input failed validation on every folder page:
  the schema made each field of `agent.Viewing` non-null, and a folder
  view has no item. Every field is nullable now.
- Moving a message makes a new item with a new id (`MoveItems` creates a
  row in the target folder), so a model that kept the id from its search
  got "not found" on the next action. `mail_act` hands the new ids back
  and says so; `getThread` says why an old id is gone.
- The model answered "the round-up has been scheduled" without any tool
  call: `schedule` was behind `tool_search` and the model never searched
  for it. `schedule` is a core tool now, offered every round.
- `runSend` claimed the sender's quiet period before sending; a send that
  failed and was retried was then refused by its own first try. The
  ladder reads the quiet period; the sender is marked replied to after
  the reply has gone.
- The "Tell me when" card and the mail behind it are gone (owner's
  decision, 2026-09-11): there is no notification system yet, and what the
  agent does is shown in place — the held reply in the reader and on the
  agent page, the priority in the list, a failed run in the activity.
  `Agent.Notifications` stays in the model, unused, for when there is one.
- `ReadAgentRun`, the long-poll for a client without a websocket, holds
  the request's database transaction for up to 25 seconds per call; the
  dashboard uses the subscription, and the command line's polling is one
  connection per open terminal. Left as is, noted here.
- The system prompt carried the time to the minute ("it is now Thursday, 10 September 2026 20:41 EDT"), so no two rounds a minute apart shared a prefix and the providers' caches were cold most of the time — 220k of 521k prompt tokens cached on the first day. The `<now>` overlay after the history had carried the time all along; the situation now says the zone and the language only. Anything that changes between rounds belongs after the history, not before it.
- Reading production's main conversation: asked "what is iPhone Duo", the model called `tool_search` with the product's name, found nothing, and said there was no information; told it had a web search tool it searched the mailbox. A deferred `web_search` is a tool the model does not know it has. The tools a person reaches for by name are core now, `tool_search` says what it is for, and the conduct says to search for a tool before saying "I cannot" — which is also what it answered when asked to add a connected server, with `settings_update` a search away.
- Production's 115 sorting runs, read as a set (2026-09-11): a scam — "free pillows and two nights from Marriott" — padded with a paragraph of neighbourly chat about a park walk was sorted *personal, needs a reply*, with the walk as an action item; a plan-expiry notice and a community site's question of the day were marked as needing a reply because their text said "reply"; GitHub's automatic mail landed in three categories (`work` for failed runs, `other` for dependency bumps, `notification` for the rest). The prompt now names padded scams and says a service's automatic mail is a notification whatever it concerns, and `InterpretTriage` refuses `needs_reply` for the five categories that never can — the prompt's own rule, enforced. Token use by kind: conversations cache half their prompt tokens after the time left the system prompt; sorting caches almost nothing because a sorting prompt is under the provider's minimum, which is fine. No job was given up on; five sorting jobs took a second attempt without recording why, which is the retry ladder working.

## Decision Log

- Decision: full auto-reply from the start, behind a per-source policy, a
  hold window, a refusal ladder that reuses the out-of-office protections,
  caps, and an audit row.
  Rationale: the owner wants the agent to act, not only to draft; the
  protections already exist for the out-of-office reply and are the ones
  worth trusting. Date/Author: 2026-09-10, Ziyan Zhou.
- Decision: the agent belongs to the person, and a mailbox is a source it
  is granted. Rationale: `docs/decisions/20260910-agents-belong-to-people.md`.
  Date/Author: 2026-09-10, Ziyan Zhou.
- Decision: embeddings as `real[]`, ranked in Go. Rationale:
  `docs/decisions/20260910-embeddings-without-pgvector.md`.
  Date/Author: 2026-09-10, Ziyan Zhou.
- Decision: the agent section's secrets — provider keys, the search key,
  connected-server credentials — are sealed with the server secret before
  they reach the settings rows and opened when read (`config/seal.go`),
  using the same box the domain table's keys use; the other sections are
  left as they were. Rationale: the owner asked for it; a dump of the
  settings table must not hold keys that spend money; the mechanism already
  existed and is trusted. Date/Author: 2026-09-10, Ziyan Zhou.
- Decision: the reply run is one model call that reads the research notes,
  rather than a tool loop of its own; the lookups happen in the research
  run, which triage asks for. Rationale: one place does lookups and leaves
  notes the reader can see; a reply that could call tools would be a second
  research run with a send at the end of it, and the hold and re-check are
  the protections, not the number of rounds. Date/Author: 2026-09-11,
  Ziyan Zhou.
- Decision: a draft the agent holds becomes the person's the moment they
  save or discard it by hand (the composer replaces the draft item, and the
  reply row is marked taken over), and the send job re-climbs the whole
  ladder before sending. Rationale: an edit is ownership; a message that
  passed the ladder ten minutes ago may not pass it now. Date/Author:
  2026-09-10, Ziyan Zhou.
- Decision: the words *rules*, *conduct*, *instructions*, *house
  instructions*, *guidance*, *memory*, *corrections* and *source* each mean
  one thing, defined in the roadmap; in particular "rules" is never used for
  anything but the mailbox's rules. Rationale: the mailbox already has
  rules, and a second thing called rules would be misread by every reader.
  Date/Author: 2026-09-10, Ziyan Zhou.
- Decision: the plan ships in two halves, A1 (milestones 1–6) and A2
  (7–11). Rationale: plan B need not wait for the agent you talk to.
  Date/Author: 2026-09-10, Ziyan Zhou.

## Outcomes & Retrospective

(Written at the end of each half.)

## Context and Orientation

This is a mail server written in Go with a React dashboard. Mail arrives
over SMTP in `internal/util/smtpd`, is handed to `internal/mx` (start at
`exchange.go`, `HandleEnvelope`), authenticated, stored once as a row in the
`mail` table with the raw message in `internal/storage`, and delivered. One
kind of delivery is into a **mailbox**: `internal/mx/exchange_mailbox.go`
adds an item to the Inbox folder, learns the sender as a contact, runs the
mailbox's **rules** (`exchange_rules.go`), and may send the out-of-office
reply (`exchange_autoreply.go`). People sign in to the dashboard, own
mailboxes, and read them there or over IMAP (`internal/imap`).

Everything an operator configures lives in one document in the database,
whose shape is `internal/config/config.go`; it is validated by
`internal/config/validate.go`, redacted by `redact.go` (every field tagged
`secret:"true"`), and documented by hand in `docs/configuration.md`, which
`make lint-ci` checks against the struct tags. Optional integrations follow
the shape of `config.Antispam`: an `Enabled` flag, and a constructor in
`internal/cmd/server/run.go` (`openAntispam`) that returns nil when it is off
so that no client is built for a disabled integration.

The API is GraphQL generated by reflection from Go interfaces in
`internal/api/v1api/apigraph/` (see `util/graphapi`); each interface method's
doc comment becomes the operation's description; `internal/client` is the
Go client with one function per operation and the query documents it sends;
`internal/cmd` is the command line built on that client, one file per
resource group, every command offering `--json`. The dashboard is
`web/src/`, with strings in `web/src/i18n/{en,ja,zh}.ts`; `make check-catalogs`
fails when a key is missing from any of the three.

Background work runs under `internal/util/periodic` (see `listBackfill` in
`run.go`). Hourly usage counters follow `models.Usage` and
`internal/db/database_alias_usage.go`. Permissions are the vocabulary in
`internal/models/permission.go`, carried by roles, grouped, seeded by
`internal/access`, and resolved per request into `api.Principal`.

Terms used below and nowhere defined by the code yet:

- **Agent** — the row per account that holds what the agent knows about a
  person; `internal/models/agent.go`.
- **Source** — a mailbox the person granted; `Mailbox.Agent *AgentMailbox`,
  a JSON column on the mailbox with the processing policy for it.
- **Run** — one unit of work the worker executes for an agent: `triage`,
  `research`, `summarize`, `embed`, `reply`, `send`, `schedule`; and `ask`
  for an interactive turn. Every run leaves a transcript.
- **Conduct** — the fixed part of the prompt, `internal/agent/prompts/system.txt`.
- **Confirmation** — a tool call whose risk class is `destructive` or
  `outward` does nothing until the person approves it on a card; a run with
  nobody present cannot approve.

## Plan of Work

Two new packages, mirroring the seam-and-engine split of `spamfilter` and
`strainer`.

**`internal/llm/`** talks to models and knows nothing about mail.
`chat.go` holds `ChatRequest`, `ChatMessage` (content as text or parts, tool
calls), `ToolDefinition`, `ChatResponse`, `Usage` (prompt, completion,
cache-read, cache-write tokens) and `StreamEvent`. `provider.go` defines
`Provider` with `Chat`, `ChatStream` and `ListModels`, and the optional
`Embedder` with `Embed`. `openai.go` speaks `/v1/chat/completions` and
`/v1/embeddings`, which covers OpenAI and every compatible server (Ollama,
vLLM, llama.cpp, OpenRouter, xAI, Mistral); `anthropic.go` speaks the
Messages API with tool use and prompt caching; `gemini.go` speaks
`generateContent` with function calling. Each is `net/http` only and tested
against `httptest` servers replaying recorded response shapes.
`registry.go` is built from `config.Agent`, filters each provider's model
list through its `allow`/`deny` globs, caches `ListModels` for five minutes,
and resolves work to a model with `ForWork(kind)`: the override for that
kind, else `fast` for triage, summarize and compact, else `default`; and
`Embedding()`. It is constructed only when `agent.enabled` is true.
`structured.go` has `Extract[T]`, which strips fences, finds the outermost
JSON object, repairs common breakage with the vendored
`github.com/kaptinlin/jsonrepair`, and unmarshals. `tokens.go` estimates
tokens cheaply for budgeting.

**`internal/agent/`** knows mail and knows nothing about HTTP. `agent.go`
holds the registry, database, storage and exchange and offers
`Enqueue(tx, kind, agentId, mailboxId, subjectId)`. `worker.go` is the claim
loop over `agent_job` with `FOR UPDATE SKIP LOCKED`, a short retry ladder,
and dead-lettering after five attempts with the error kept. `context.go`
turns a `Mail` into model input: the headers that matter, plain text with
HTML reduced by the reader's sanitizer, quoted history trimmed, attachments
by name and type, capped at `limits.maxBodyCharacters`; never raw MIME,
never attachment contents. `prompts/` holds `go:embed` templates —
`system.txt` (the conduct, with a short variant), `triage.txt`,
`research.txt`, `summarize.txt`, `draft.txt`, `reply.txt`, `compact.txt` —
rendered with `text/template`; the person's instructions and the operator's
house instructions are appended as labelled blocks, never interpolated into
the conduct. `triage.go`, `research.go`, `summarize.go`, `draft.go`,
`reply.go` and `search.go` are pure functions of (context, input) → typed
result so they test without a database. `policy.go` answers every "may
it?" question in one place. `usage.go` records hourly token rows per agent
with the run kind, source and model as dimensions, and the daily and
monthly budgets read the same rows. `ask.go` is the tool loop; `conversation.go`
the stored turns; `compact.go` the structured summary with a resume point;
`prompt.go` and `overlay.go` the layers; `schedule.go` headless runs;
`tab.go` the relay for an attached tab. `tools/` is the catalog, one file
per family plus `catalog.go`.

The configuration block, the per-person and per-source model, the tables,
the delivery hook, the pipelines, the refusal ladder, the tool catalog, the
prompt layers, the conversations, the operator's controls, the command line
and the dashboard are each specified in the milestone that builds them
below, so that a reader implementing milestone 3 has what milestone 3 needs
in one place.

### Milestone 1 — `internal/llm`, configuration, settings section

Goal: an operator can declare a provider, see its models, and assign work to
them; nothing else changes.

Add `github.com/kaptinlin/jsonrepair v0.2.8` to `go.mod` and vendor it
(`go mod tidy && go mod vendor`; it is in the module cache).

In `internal/config/config.go` add `Agent Agent` with `yaml:"agent"` to
`Configuration`, after `Upgrade`, and the types:

    Agent { Enabled bool; Instructions string; Providers []AgentProvider;
            Models AgentModels; Features AgentFeatures; Limits AgentLimits;
            Retention AgentRetention; Search AgentSearch; Tools AgentTools;
            Browser AgentBrowser; MCP AgentMCP }
    AgentProvider { Name, Kind, BaseURL string; APIKey string `secret:"true"`;
            Enabled *bool; Models AgentProviderModels; Pricing AgentPricing }
    AgentProviderModels { Allow, Deny []string }
    AgentPricing { Input, Output, CacheRead float64 }   // per million tokens
    AgentModels { Default, Fast, Embedding string; Triage, Research, Summarize,
            Reply, Ask, Schedule, Compact string; Choices []string }
    AgentFeatures { Triage, Summaries, DraftReplies, Search, Research,
            AutoReply, Ask, Schedules, Browser, ConnectedServers *bool }
    AgentLimits { MaxBodyCharacters int; DailyTokensPerAgent int64;
            MonthlyTokensPerServer int64; MaxRoundsPerAsk, MaxRoundsPerResearch,
            MaxRoundsPerReply, MaxToolCallsPerRun int; RequestTimeout Duration;
            Concurrency int }
    AgentRetention { Runs, Corrections Duration }
    AgentSearch { Kind string; APIKey string `secret:"true"` }
    AgentTools { Disabled, Confirm []string }
    AgentBrowser { Enabled bool; CDPEndpoint string; AttachTabs *bool;
            AllowPrivateAddresses []string; IdleTimeout Duration; MaxContexts int }
    AgentMCP { Servers []AgentMCPServer }
    AgentMCPServer { Name, Transport, URL, Command string; Args []string;
            Env map[string]string `secret:"true"`; WorkingDir string;
            Auth string; Authorization string `secret:"true"`;
            OAuth AgentMCPOAuth; Headless bool; ReadOnly, Disabled []string;
            Timeout Duration; Enabled *bool }
    AgentMCPOAuth { ClientID string; ClientSecret string `secret:"true"`;
            Scopes []string; AuthorizationURL, TokenURL string }

Pointer booleans mean "unset is on". Defaults in `defaults.go`: limits
12000 / 200000 / 0 / 40 / 8 / 6 / 60 / 60s / 2; retention 30d / 90d;
browser idle 5m, max contexts 4. Validation in `validate.go`, reported with
paths like the rest: `agent.enabled` with no enabled provider; a model name
that is not `provider:model`, names an unknown or disabled provider, or a
model the provider's filter denies; a `choices` entry likewise; a provider
`kind` outside `openai`, `anthropic`, `gemini`; a duplicate provider name;
`search.kind` outside `brave`; `browser.enabled` without an endpoint; an MCP
server without a unique name, `http` without a URL, `stdio` without a
command, `oauth` without a client id, `auth` outside the four modes; a
`tools.disabled` or `confirm` entry that names neither a family nor a tool
(the family names are `mailbox`, `domains`, `audit`, `access`, `server`,
`account`, `general`, `mcp`, `browser`). Document every field in
`docs/configuration.md` under a new `### \`agent\`` section, or `lint-ci`
fails. Extend `TestEverySecretIsTagged`'s expectations if it enumerates
secrets.

`internal/llm` as described above. The settings API in
`internal/api/v1api/apigraph/settings.go` gains an `Agent *AgentSettings`
section on `Settings` and `AgentParameters` on `UpdateSettingsArguments`,
following `AntispamSettings`/`AntispamParameters`: providers with
`hasApiKey` rather than the key, models, features, limits, tools, search
with `hasApiKey`, browser, MCP servers with `hasAuthorization` and
`hasClientSecret`. Two new operations on a `ServerQuery`-style interface:
`TestAgentProvider(name)` returns the filtered model list or the error, and
`ListAgentModels` returns every enabled provider's filtered list, each
entry `provider:model` with context length where known. Both need
`server:manage`. The client (`internal/client/settings.go`) and the command
line (`teanode settings show|set|describe agent`) pick the section up from
the schema; the `UPDATE` document in the dashboard and `settingsSelection`
in the client list the new fields.

Dashboard: `web/src/pages/settings/integrations.tsx` gains an `AgentForm`
under a new `agent` section of `/server` (add the tab where `spam` is
declared): the enabled switch, house instructions, a providers table (name,
kind, base URL, key with "set"/"kept", enabled, allow/deny, test), a models
table (one row per kind of work, a picker fed by `ListAgentModels`,
choices as a multi-select), features as switches, limits as inputs, tools
policy as two text lists, search kind and key, browser fields, and MCP
servers as an editable list. Strings in all three catalogs.

Acceptance: with `OPENAI_API_KEY` configured as a provider, the Agent
section's *Test* lists that provider's models; `teanode settings show
agent` prints the section with the key as `(redacted)`; with
`agent.enabled: false`, `TestRegistryIsNilWhenDisabled` asserts that
`openAgent` returns nil and constructs no HTTP client.

### Milestone 2 — queue, worker, the agent and its sources, permissions, usage and limits

Goal: a person turns their agent on, grants a mailbox, and the worker picks
up a no-op job for a delivered message; the operator sees the usage.

Migration `0031_personal_agents.sql` (+ reverse) creates `agent`,
`agent_job`, `agent_usage`, adds `agent jsonb` to `mailbox`, and adds
`timezone`, `timezone_mode`, `timezone_seen_at`, `locale_seen` to `user`.
Models in `internal/models/agent.go`: `Agent` (ID, UserID, Name, Enabled,
Instructions, Language, Voice, Categories, Notifications, Confirm, AskModel,
DailyTokens, OperatorDisabledAt, timestamps) and `AgentMailbox` (Granted,
Triage, Summaries, DraftReplies, Search, Research, AutoReply) with the
nested `AgentVoice`, `AgentCategory`, `AgentTriage`, `AgentSummaries`,
`AgentAutoReply`, `AgentHours`, `AgentNotifications`, each with `Validate`.
Database operations in `internal/db/database_agent.go`: get/create/update
agent by user, list agents, grant/revoke mailbox, enqueue and claim jobs
(`FOR UPDATE SKIP LOCKED`, `not_before`, attempts, dead-letter), usage put
and sum following `database_alias_usage.go`.

Permissions `agent:use` and `agent:audit` in `models/permission.go`, seeded
onto the default Member and Operator roles by `internal/access`, listed by
`ListPermissions`. The session middleware reads `X-Timezone` and the
browser language into the user row at most hourly (`internal/web/session.go`).

API in `apigraph/agent.go`: `ReadAgent`, `UpdateAgent` (with `forget`),
`GrantAgentMailbox`, `RevokeAgentMailbox`, `AgentUsage`; operator:
`AgentServerUsage`, `ListAgents`, `SetAgentLimit`, `SetAgentDisabled`,
`ListAgentDeadLetters`, `RetryAgentJob`. `UpdateProfile` gains timezone and
mode. Worker: `internal/agent/worker.go` started from `run.go` like
`listBackfill`, only when `openAgent` returned non-nil; a job kind `noop`
exists for this milestone's test and is removed in milestone 3. Delivery
hook in `exchange_mailbox.go`: after `runRules`, if the mailbox's
`Agent.Granted` and its owner's agent is enabled and not operator-disabled,
enqueue `triage` (and `embed` when `Search` is on and an embedding model is
configured) in the same transaction.

Dashboard: `web/src/pages/agent.tsx` in the rail (turn on, name, *About
me*, *Tell me when*, *Advanced*, sources with their cards, today's tokens,
*Forget*), a *Sources* card on `mailboxSettings.tsx`, the time zone on the
profile page, and an *Agent* tab on `/server` for `agent:audit` (usage,
agents, limits, switch-off, dead letters). Command line: `teanode agent
settings|source|usage|admin`.

Acceptance as written in the roadmap's plan A milestone 2.

### Milestone 3 — triage

`mail_insight` table (mail_id, mailbox_id, agent_id, category, priority,
needs_reply, research_asked, summary, action_items, notes, notes_run_id,
model, run_id, created_at). `agent_conversation` and `agent_message` tables
arrive here too, because every run leaves a transcript (kind `run`).
`triage.go` builds the prompt from the short conduct, the house
instructions, the person's instructions and voice, memory addressed to
`triage` (none yet — the block exists and is empty), the newest twenty
corrections (none yet), the fixed vocabulary plus the person's own
categories, and the message; asks for one JSON object; writes the insight.
`MailboxRuleCondition.Field` gains `category` and `priority`; `runRules`
gains a phase, and rules with those conditions are skipped at delivery and
run by the worker after the insight is written. `GetMailboxThread`,
`ListMailboxThreads` and `ListMailboxItems` carry `insight`; the list shows
chips; a *Priority* view sits beside Starred; the rules editor offers the
two conditions. Backfill per the source's choice when it is granted.

### Milestone 4 — summaries

`thread_summary` (thread_id, mailbox_id, agent_id, summary, through_mail_id,
model, run_id, created_at). `summarize` runs when a granted mailbox's thread
gains a message and is open in the reader or has reached the source's
`MinimumMessages`; coalesced per thread; incremental from `through_mail_id`.
Shown collapsed at the top of the reader.

### Milestone 5 — draft replies

`DraftReply(threadId, instructions)` and `draft.go`; "Draft with agent" in
the composer with a one-line instruction box; the result is a normal draft.

### Milestone 6 — auto-reply

`agent_reply` (id, agent_id, mailbox_id, mail_id, draft_item_id, run_id,
status held|cancelled|sent|refused, reason, send_after, sent_mail_id,
created_at). The policy on the source: enabled, guidance, scope
known|everyone|list, allow, never, categories, when always|outsideHours|
whenAway, hours, holdMinutes (10), dailyLimit (20), quietDays (7, minimum
1). The refusal ladder, in order, each reason recorded: everything
`autoReplyRefusal` already refuses, called as-is; the person already replied
in the thread or a reply is held; scope, lists, category and when;
`ClaimAutoReply` with the source's quiet days and the fixed fifty an hour,
plus the daily limit and the token budget; the model's own decline
(`{"reply": null, "reason": "…"}`). A reply that passes is a draft in
Drafts in its thread with `send_after`; the `send` job re-checks the ladder,
sends through submission with `Auto-Submitted: auto-replied`, `In-Reply-To`
and `References`, files in Sent, writes the audit row and marks the row
sent; an edit by the person cancels. Banner and toast in the reader;
notifications per the agent's setting (dashboard, or mail through
`internal/mailer` to the account's notification address).

### Milestone 7 — Ask: the loop, the prompt, the mailbox and general tools

Conversations (`kind` main|named|run), the layered prompt (identity,
conduct short/full, house instructions, situation, the person's words with
the top twenty `ask` memories, tool guidance and the deferred catalog),
overlays (`viewing`, `surface`, `todo`, `recalled`, `pending`, `budget`,
`confirmation`, `tab`, `now`), profiles by context length, compaction with
a resume point, the tool runtime with risk classes and the confirmation
pause, Family 1 (mailbox) and the general tools `datetime`, `web_fetch`
(through `util/safefetch`), `web_search`, `tool_search`, embeddings with
hybrid ranking behind `mail_search`, the drawer streaming over the
websocket, `AskAgent`, `ResolveAgentConfirmation`, `StopAgentRun`,
conversation operations, `teanode agent ask|chat|conversation|run`.

### Milestone 8 — operator tools, policy, audit marker

Families 2–6 over an `Operations` interface the API package satisfies and
hands to the agent package at construction; the permission-filtered
catalog with `tool_search` deferral above forty definitions; `agent.tools`
in the settings section; every write audited "via agent"; `access_explain`;
`teanode agent tools`.

### Milestone 9 — memory, corrections, schedules, questions, todo

`agent_memory` with `applies_to`, folded into prompts by audience;
`agent_feedback` recorded from the person's own actions; `agent_schedule`
with headless runs delivered by mail or to the drawer; `ask_user`; `todo`
and `agent_todo`; the command line for each.

### Milestone 10 — connected servers, and research in the pipeline

`internal/mcp/` (client, streamable HTTP and stdio transports, OAuth 2.1
with PKCE and metadata discovery), `agent_mcp_connection`, discovery and
namespacing `mcp__<server>__<tool>`, Family 8 with risk `outward` unless
`readOnly`; the `research` run and triage's `research` answer; the bounded
read-only tool set for `research` and `reply`; `teanode agent mcp`.

### Milestone 11 — browser

`internal/browser/` over the DevTools protocol with per-run contexts and
the address guard; `agent.browser` with a compose profile; the `browser`
tool with its reading subset in headless runs; the extension in
`web/extension/`, the tab relay, the `<tab>` overlay, and the refusals
enforced in the extension.

### Milestone 12 — the conversation, held well

What a person meets when they use the drawer for more than a question:

- **Turns in order.** `Agent.Ask` keeps one run at a time per conversation:
  a second turn queues behind the first, emits a note saying so, and
  starts when the first is over. Stopping the run in flight leaves the
  queued turns to run. A stopped turn persists the words that had streamed
  by then, so the transcript after a reload matches what was seen.
- **Attachments.** `POST /api/v1/agent/attachments` takes a multipart body
  as the draft upload does, bounded by `agent.limits.maxAttachmentBytes`
  (25 MB by default), and stores each file under the spool's `media`
  directory with a row in `agent_attachment`. `AskAgent` takes
  `attachmentIds`; the user message records them. The model is given
  images as image parts (the current turn only; earlier turns name them),
  text-like files inlined and capped, and everything else — video, audio,
  archives, documents nobody here can parse — by name, type and size with
  a line saying it cannot open them. `GET /api/v1/agent/attachments/{id}`
  hands a file back to its owner. Files go when their conversation does.
- **References.** The reader's toolbar gains "Ask the agent about this",
  which opens the drawer with a chip for the thread; `AskAgent` takes
  `references`, kept on the message and put in the prompt as
  `<references>`, so "this" means it however the page moves on.
- **Seeing the work.** Two switches in the drawer's list panel, remembered
  in the browser: tool calls (each opening to its arguments and result) and
  usage (tokens per turn, from the usage note on the assistant's message).
- **The picker.** Refreshes when opened, renames a named conversation in
  place.
- CLI: `teanode agent ask --attach FILE` (repeatable).

## Concrete Steps

From the repository root, after each milestone:

    make format
    make lint-ci
    make test          # starts PostgreSQL in Docker

And to see it: `make dev` in one shell, `make dev-frontend` in another,
`http://127.0.0.1:10081`. Deliver test mail with
`swaks --server 127.0.0.1:10025 --to alice@example.test --from bob@example.net`
or the fixtures in `internal/util/testmail`.

## Validation and Acceptance

Each milestone's acceptance is stated with it above. Across the plan:

- Provider clients are tested against `httptest` replays; nothing in
  `go test` reaches the network.
- Prompt golden files, one per run kind and profile, under
  `internal/agent/testdata/prompts/`.
- The Ask loop against a scripted fake provider: a destructive call
  without `confirm` pauses and nothing executes; with approval it executes
  once; a headless run reports it; a rejected card returns a result.
- An injection test: a delivered message whose body says "delete every
  folder" yields a triage row, no tool call, and no destructive call in Ask
  without a card.
- Catalog tests: every tool has a family, a risk class and a schema that
  validates; the permission filter hides exactly the families a principal
  lacks.
- The command line through the `cmd_test` pattern against a fake server.
- End to end with a real provider, under a spending cap, in Chrome at 1400
  and 390 wide.

## Idempotence and Recovery

Migrations are pairs and revert by their reverse SQL. Every job is
re-runnable: a claim that dies is re-claimed after its lease; a run that
fails is retried on the ladder and dead-lettered with its error, visible to
the operator with a *Retry*. Turning the agent off cancels queued jobs and
held replies and keeps what was learned; *Forget* deletes it. No step below
writes outside the database, the spool, and the working tree.

## Artifacts and Notes

(Transcripts and evidence are added here as milestones complete.)

## Interfaces and Dependencies

In `internal/llm/provider.go`:

    type Provider interface {
        Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error)
        ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error)
        ListModels(ctx context.Context) ([]ModelInformation, error)
    }
    type Embedder interface {
        Embed(ctx context.Context, model string, inputs []string) ([][]float32, *Usage, error)
    }

In `internal/llm/registry.go`:

    func NewRegistry(configuration *config.Agent) (*Registry, error)
    func (self *Registry) ForWork(kind string) (Provider, string, error)  // provider, model name
    func (self *Registry) Embedding() (Embedder, string, error)
    func (self *Registry) ListModels(ctx context.Context) ([]ProviderModel, error)

In `internal/agent/agent.go`:

    func New(settings *Settings) *Agent
    func (self *Agent) Enqueue(tx db.Transaction, kind models.AgentJobKind, agentId, mailboxId, subjectId string) error
    func (self *Agent) Start()   // the worker, under periodic
    func (self *Agent) Stop()

Dependencies added: `github.com/kaptinlin/jsonrepair` (milestone 1); the
MCP and browser packages use only the standard library and the vendored
`gorilla/websocket` (milestones 10 and 11).
