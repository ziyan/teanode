# Memory pages linked to the person's records

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md` (the ExecPlan rules); a reader needs only this file and the working tree.

## Purpose / Big Picture

The agent keeps a memory: pages about people, organizations, projects and topics, joined by typed links between pages ("works on", "knows", "member of"). The person's own records live elsewhere in TeaNode and are not part of that graph: their contact cards, mail, calendar events and reminders, agent conversations, finance transactions and receipts, and recordings. A person page cannot show the person's contact card (the slot exists but nothing fills it), a fact cannot be opened on the mail or the moment of a recording it came from, and nothing can be asked from a record back to memory ("which pages mention this transaction?").

After this change the records are nodes of the graph too. A memory page links to records with typed links (this person is that contact card; wrote this mail; attended this event; spoke in this recording passage; was paid by this transaction; this receipt is for that transaction), and every link can be walked both ways: from the page to the record and from the record to the pages. The agent can read a person's contact card from their page when the person granted the agent that address book. Facts cite records the person can open with a click. Links are made automatically where a match is sure (an email address, a confirmed speaker name, a receipt already matched to a transaction) and proposed where it is a guess, and the person can make or remove any link by hand.

To see it working: open a person page on the dashboard and see a Linked records section with their contact card, the last mail they sent, the meetings they attended and the recording passages where they spoke, each opening the record; open a mail message and see "In memory" naming the people and topics linked to it; ask the agent "what is Alice's phone number" and see it read the contact through her page.

## Progress

- [x] Milestone 1: the link model and API (record links table, typed record references, contact link, page links and record backlinks, fact evidence that names a record). Done 2026-10-13: migration `0176_agent_record_link`, `models.AgentRecordLink`, the DB methods, package `recordlink` (reads a link's record under the reader's access), GraphQL and client documents, and the CLI (see Interfaces and Dependencies). Tests: `internal/db/database_graph_record_test.go`, `internal/recordlink/recordlink_test.go`, `internal/api/v1api/apigraph/agent_graph_record_test.go`, `internal/cmd/agent_graph_record_test.go`.
- [x] Milestone 2: the agent reads and makes links (memory tool operations, contact card on a person page within grants, prompt context). Done 2026-10-13: `links`, `backlinks`, `link --record`, `unlink --record` on the memory tool, the linked card in `get`, record titles in recalled pages, the self card in the prompt read only from a granted book. Tests: `internal/agent/tools/memory/records_test.go`, `internal/agent/graph_record_prompt_internal_test.go`.
- [x] Milestone 3: links made automatically (contacts by address and name, recording speakers and passages, mail participants, calendar attendees, finance merchants and receipts), as stated when sure and proposed when guessed. Done 2026-10-13: `internal/agent/dream_record_links.go`, run by every dream after the pages are divided; migration `0179_agent_record_link_dismissal`. Tests: `internal/agent/dream_record_links_internal_test.go` (one per source and the bound), `internal/db/database_graph_record_test.go`.
- [x] Milestone 4: the dashboard (Linked records on a page, clickable evidence, In memory on records, contact picker, record nodes in the graph view). Done 2026-10-13: `web/src/memoryLinks.tsx` (kinds, groups, `InMemory`), `web/src/pages/knowledgeRecords.tsx` (Linked records, Confirm, Remove, Link contact), `web/src/recording/passageMemoryDialog.tsx`, deep links on the contact, calendar, reminder, conversation, transaction and receipt pages, record squares in `knowledgeExplore.tsx`; `FinanceReceipts(receiptId)` on the server. Tests: `memoryLinks.test.tsx`, `knowledgeRecords.test.tsx`, `knowledgeExplore.test.tsx`, `addressBook.test.tsx`, `calendarLink.test.tsx`, `components/reminders.test.tsx`, `finance/financeTransactionLink.test.tsx`, and additions to the receipt, recording, conversation and knowledge tests.
- [x] Milestone 5: the iPhone app and the CLI, with the same reads and edits. Written 2026-10-13: `ios/TeaNodeKit/Sources/TeaNodeKit/KnowledgeRecords.swift` (models, grouping, `RecordTarget`, operations; tests in `KnowledgeRecordsTests.swift`), `ios/TeaNode/Sources/InMemoryViews.swift`, Linked records, Link contact and record evidence in `KnowledgeViews.swift`, In memory on the mail, event, contact, transaction, receipt and recording screens, the screenshot `knowledge-records` with its assertion `ios/scripts/assert-knowledge-screenshot.swift`; `teanode agent memory confirm`. See Outcomes for what CI showed.

## Surprises & Discoveries

- Person pages already have `AgentNode.ContactID` (`internal/models/graph.go`, column from migration `0066_agent_graph.sql`), read live by `AgentGraphPage` (`internal/api/v1api/apigraph/agent_graph.go`), and shown as one line on the dashboard and the iPhone, but nothing ever sets it except the person's own self card (`SetMyContact`).
- Evidence kinds `mail`, `commit`, `chat` and `contact` are declared in `internal/models/graph.go` but never written. Received mail is not in memory at all; only sent mail is ingested (`internal/agent/ingest_sent.go`) and cited as `document` evidence.
- The memory tool already had `link` and `unlink` for joining two pages (and the person's `LinkAgentNodes` and `UnlinkAgentNodes` and `teanode agent memory link|unlink` for the same). The record operations extend the same verbs with a `record` argument rather than adding rival names, and `links` and `backlinks` are new.
- The CLI group is `teanode agent memory ...`, not `teanode memory ...`; the commands are added there.
- A recording has many passages, so the unique key the plan first named, (agent, page, kind, id, relation), would have allowed one passage per recording per relation. The range is in the key (see the Decision Log).
- `GetAgentMessage` finds a conversation message by identifier alone, and `FirstOwnedItemIDsByMail` maps a mail id to the person's mailbox item, so a record's owner is checked after the lookup, not by the query. The resolver (`internal/recordlink`) does that check per kind, which is why it is one package that both the API and the agent use.
- Address books and calendars are created closed; `AgentGranted` is set only by `UpdateAddressBook` and `UpdateCalendar`, so a test that passes it to `Create...` gets a closed one without an error.
- `internal/agent/remember_apply.go` explains why pages must not create contact cards (it once created hundreds of empty cards); this plan keeps that rule: links point at existing cards and never create one.

## Decision Log

- Decision: records join the graph through a new table of record links rather than by turning records into pages.
  Rationale: a page is the agent's own writing about something; a record is the person's data, owned and permissioned elsewhere. Copying records into pages would duplicate data and leak it past grants; a link that names the record by kind and id keeps one copy and lets every read check the grant at the moment of reading.
  Date/Author: 2026-10-13, Claude, asked by the owner ("knowledge graph needs to be truly a graph").
- Decision: links have the same status vocabulary as page edges, stated and proposed, and automatic matching states only what is certain (an exact email address on a card, a speaker name the person confirmed, a receipt the person or the matcher already matched).
  Rationale: the graph already separates what the agent may assert from what it guesses; a contact linked by a common first name is a guess.
  Date/Author: 2026-10-13, Claude.
- Decision: the agent sees a linked record only when it could read that record anyway: an address book or calendar with `AgentGranted`, a mailbox the agent is granted, the finance source enabled, a recording of its owner. The person's own dashboard sees all their links.
  Rationale: a link must never widen access; the research for this plan found two places that read the person's own card without the grant (the page view, which is the person's own and fine, and the prompt's self card), and the agent-facing reads added here check the grant.
  Date/Author: 2026-10-13, Claude.

- Decision: the unique key of a record link includes the passage range (columns `passage_start_ms` and `passage_end_ms`, zero for every other kind), and `record_detail` holds only the transcript run of a passage and the mailbox of a mail.
  Rationale: the range has to be a column to be indexed for overlap queries and to be part of uniqueness; with it in jsonb, two passages of one recording by one speaker could not both be linked. Deviation from the unique key written in the plan.
  Date/Author: 2026-10-13, Claude.
- Decision: a mail record is named by the mailbox item identifier (the identifier the dashboard opens at `/mailbox/starred/<id>`), not by the mail's own identifier.
  Rationale: a message can be in several mailboxes, and the item says which person's and which mailbox's, which is what the grant check needs.
  Date/Author: 2026-10-13, Claude.
- Decision: the agent's reads of a finance transaction check the source the transaction's account came from (switched off withholds that institution only), and of a receipt check that any finance source of the agent is switched on.
  Rationale: a receipt belongs to no source; a transaction does.
  Date/Author: 2026-10-13, Claude.
- Decision: the agent states a link only when `link_status` is `stated` and `quote` is in the person's last message of the conversation of a run with somebody present; the default is a proposal. Removing a stated link needs the same. A proposal made again never turns a statement back into a proposal.
  Rationale: "never stated without the person's words" has to be something the tool checks and not only something the guidance says. The check is mechanical (a quote in their last message), which is the most the server can verify.
  Date/Author: 2026-10-13, Claude.
- Decision: every result of the memory tool that carries a record's title or card is marked untrusted (`Result.Untrusted`), so the loop fences it; a recalled page in a prompt carries kind and title inside the same fence (`fenced`), and leaves recording passages out of the prompt, since a recording's title is written from what was said and recorded words reach a prompt only as marked evidence. Reading a recording passage link through `links` marks the run as having read recording evidence, and evidence of kind `recording_passage` counts as recording evidence.
  Rationale: the plan's rule that titles from mail, calendar and receipts are untrusted data, and the existing rule about recorded words.
  Date/Author: 2026-10-13, Claude.
- Decision: the dashboard paths a link returns for records with no deep link yet (`/contacts?contact=`, `/calendar?event=`, `/reminders?reminder=`, `/agent?conversation=&message=`, `/finance/transactions?transaction=`, `/finance/receipts?receipt=`) are the intended addresses; the pages ignore the parameter until Milestone 4 reads it. Mail (`/mailbox/starred/<id>`) and recordings (`/recordings/<id>?version=&startMS=&endMS=`) already open.
  Rationale: the API has to be complete before the dashboard is; the address is part of the contract.
  Date/Author: 2026-10-13, Claude.
- Decision: the CLI is `teanode agent memory links|backlinks|link --record|unlink --record|contact`, and the memory tool's existing `link` and `unlink` take `record` for a record; `relation` in the tool is the union of both vocabularies (`about` is in both).
  Rationale: the existing group and verbs; two sets of verbs for one idea is a trap.
  Date/Author: 2026-10-13, Claude.
- Decision: merging two pages moves the first page's record links to the survivor (a twin the survivor already has is kept once) and the survivor takes the first page's `contact_id` when it has none.
  Rationale: a link lost in a merge is a link nobody notices is gone.
  Date/Author: 2026-10-13, Claude.
- Decision: a link the person removes is remembered (table `agent_record_link_dismissal`, same key as the link) and the automatic pass skips it; making the link by hand clears it, and a write while dreaming never does.
  Rationale: the passes are idempotent by the unique key, so without a record of the removal the next dream would put back every link the person took away, a proposal they declined included.
  Date/Author: 2026-10-13, Claude. Not in the plan; a migration of its own.
- Decision: `paid` is the page the money went to (a merchant organization), `paid_by` the page it came from; a receipt takes the `receipt_for` link, with the same status, of each page that has `paid` or `paid_by` its matched transaction, rather than being matched to a page by merchant name on its own.
  Rationale: the plan's "receipts already matched to a transaction" says the match is the certain part; the page comes from the transaction's link, which is what the person or the pass already judged.
  Date/Author: 2026-10-13, Claude.
- Decision: the pass looks at the newest records of each source and not at a cursor, writes at most 300 links a dream, and reports its counts in the log and not on the dream's record.
  Rationale: a cursor and two new counters on `agent_dream` would be a second migration and a change to every client for a number nobody has asked for; the unique key makes repeating the newest records free, and older ones are reached as pages and cards change.
  Date/Author: 2026-10-13, Claude.
- Decision: no separate on-demand trigger; "Dream now" runs the pass.
  Rationale: the task asked for one only if a similar pass has one, and the dream's other no-model passes (the quiet half, the revise pass) have none.
  Date/Author: 2026-10-13, Claude.
- Decision: a recording passage link is one speaker's turn: segments of one confirmed speaker with at most two seconds of silence between them.
  Rationale: a link per segment would make hundreds per recording; a turn is what a person means by "where they spoke", and the backlink query is a range overlap so any bubble inside the turn finds it.
  Date/Author: 2026-10-13, Claude.
- Decision: the dashboard's Linked records section is shown on a page that has links, and on every person page (which is offered Link contact); a page of another kind with no links shows nothing. The section comes after the summary and overview and before the facts on the dashboard, and right after the header on the phone, where the facts are many and a section below them is not seen.
  Rationale: an empty heading on every topic page is noise; the phone's list is long.
  Date/Author: 2026-10-13, Claude.
- Decision: the dashboard opens a conversation message by opening the conversation in the drawer (`/agent?conversation=`) and forgetting the address; the message itself is not scrolled to. A reminder opens in its dialog, whether or not it is done, and the reminders address (`/reminders`) now shows the reminders list, which it did not when the calendar was on.
  Rationale: the drawer has no way to scroll to a message; the reminders path had to mean reminders for the link to land.
  Date/Author: 2026-10-13, Claude.
- Decision: `FinanceReceipts` takes `receiptId`.
  Rationale: a link names one receipt that may be on no page of the list the dashboard has read; there was no way to read one by identifier.
  Date/Author: 2026-10-13, Claude.
- Decision: on the phone a mail, a contact card, a conversation and a passage of a recording open themselves (through the app's own routing of dashboard paths, `AppModel.openRecord`); an event, a reminder, a transaction and a receipt open the part of the app they are in (Calendar, Finance with Transactions or Receipts), not the item.
  Rationale: the item screens of Calendar and Finance are built from models their own sections hold, and could not be built and checked here without a Mac; each opens from its list.
  Date/Author: 2026-10-13, Claude. Deviation from "opens the record".
- Decision: a record's "In memory" on the phone is asked for when the screen opens and says nothing when no page is linked; the recording's is a menu item on a passage that asks for the range of that passage.
  Rationale: as on the dashboard.
  Date/Author: 2026-10-13, Claude.
- Decision: the CLI gains `teanode agent memory confirm`, which states the guesses of one record or all of a page.
  Rationale: `link` confirms too, but needs the relation; Confirm on the dashboard and phone is one action.
  Date/Author: 2026-10-13, Claude.

## Outcomes & Retrospective

Milestones 1 and 2 are built and tested; Milestones 3 to 5 are not started. The links are made by the person (GraphQL, CLI) and by the agent (memory tool, proposed unless the person's words are quoted), and every read of a linked record by the agent checks the grant when it reads. Nothing writes links automatically yet; that is Milestone 3, which can call `PutAgentRecordLink` with `LinkStatus` stated or proposed.

## Context and Orientation

TeaNode is a self-hosted mail and agent server in Go (`internal/`), a React dashboard (`web/src`), a SwiftUI iPhone app (`ios/`), and a CLI (`cmd/teanode`, commands in `internal/cmd`) that reaches the same GraphQL API (`internal/api/v1api/apigraph`, generated by reflection from Go structs; client documents in `internal/client`). Read `AGENTS.md`, `CONTRIBUTING.md` (strict naming: no abbreviations, `self` receivers, booleans start with is/has/can/should, units in names), `docs/subsystems/memory.md` and `docs/coding/database-migrations.md` first.

The memory graph (`internal/models/graph.go`, `internal/db/database_graph.go`): an `AgentNode` is a page with a path, a kind (`person`, `organization`, `project`, `topic`, `place`, `thing`, `folder`, `period`, `self`), a name, aliases, and `ContactID`. An `AgentFact` is a sentence on a page with `Evidence` (kind, id, quote, time). An `AgentEdge` joins two pages with a relation (`works_on`, `knows`, `member_of`, ...), a status (`stated` or `proposed`), evidence, a note and dates. Pages are written by the agent's remember step (`internal/agent/remember_apply.go`), by the dreams (background passes, `internal/agent/dream_*.go`), and by the person on the dashboard (`web/src/pages/knowledge.tsx`, `knowledgeExplore.tsx` for the graph view) and the iPhone (`ios/TeaNode/Sources/KnowledgeViews.swift`).

The records and their ids: contacts are `models.Contact` in address books (`internal/db/database_contact.go`; `AddressBook.AgentGranted`; exact address lookup `FindContactByAddress(userId, address)`); mail items are mailbox items with an item id (the dashboard opens `/mailbox/starred/<id>`, see `internal/models/dashboard_link.go`); calendar events and reminders are `models.CalendarObject` in table `calendar_object`, reminders being objects in a calendar of kind `reminders` (`Calendar.AgentGranted`); agent conversations are `agent_conversation` with messages `agent_message` (ULID ids); finance transactions are `agent_finance_transaction` and receipts `agent_finance_receipt` with matches `agent_finance_receipt_match` (`internal/db/database_finance*.go`); recordings are `recording`, and a passage is a recording id plus a transcript run and a millisecond range (`/recordings/<id>?version=<run>&startMS=..&endMS=..` opens it). Recording speakers the person named are `recording_speaker_identity` rows with a `confirmed_name` and `is_account_owner`.

## Plan of Work

Milestone 1, the link model and API. Add migration `0176_agent_record_link.sql` (with its reverse) creating `agent_record_link`: `agent_id`, `node_id` (the page), `record_kind` (one of `contact`, `mail`, `calendar_event`, `reminder`, `conversation_message`, `finance_transaction`, `finance_receipt`, `recording_passage`), `record_id`, `record_detail` (jsonb: for a recording passage its transcript run and start and end milliseconds; for a mail its mailbox id), `record_relation` (a vocabulary in `internal/models/graph.go`: `is_contact`, `wrote`, `received`, `attended`, `organized`, `spoke_in`, `mentioned_in`, `paid`, `paid_by`, `receipt_for`, `about`), `link_status` (`stated` or `proposed`), `evidence` (jsonb, same shape as fact evidence), `note`, `happened_at`, `created_at`, and a unique key on (agent, node, kind, id, relation). Add `models.AgentRecordLink`, DB methods to put, remove, list a page's links, and list a record's backlinks (by kind and id, and for recordings by recording id and overlapping range). Setting `ContactID` gets a person-facing mutation `SetAgentNodeContact(nodePath, contactId)` that checks the contact is in one of the person's own books, and writes an `is_contact` record link alongside. Fact evidence gains the record kinds above, each carrying the record id (and the passage range for recordings) so the dashboard can open it; existing `document` evidence that points at an ingested sent mail is mapped to its mail item when shown. GraphQL: `AgentGraphPage` returns `recordLinks` (each with a display title, date and dashboard link resolved at read time, or `isUnavailable` when the record is gone or not readable), and a new query `ReadRecordMemoryLinks(recordKind, recordId)` returns the pages that link to a record. Mutations `LinkAgentRecord` and `UnlinkAgentRecord` for the person. Tests for each: ownership refused across users, unique key, backlinks by recording range overlap, resolution of a deleted record to unavailable.

Milestone 2, the agent reads and makes links. The `memory` tool (`internal/agent/tools/memory`) gains operations `links` (a page's record links, each resolved only if the agent may read that record: granted address book, granted calendar, granted mailbox, finance source enabled, own recordings; otherwise shown as a link it may not open), `backlinks` (pages linked to a record), `link` and `unlink` (stated links the person asked for, or proposed ones the agent worked out, never stated without the person's words). Reading a person page through the tool includes the linked contact card's names, addresses, phones and organization when the book is granted. The prompt's page context (`internal/agent/graph_prompt.go`) mentions linked records by kind and title, not content. Fix the self card in the prompt to respect `AgentGranted`. Tests: a granted and an ungranted book, a deleted record, the tool refusing to state a link on its own.

Milestone 3, links made automatically. A pass that runs with the dreams (and on demand) proposes or states links: a person page and a contact card with the same email address (from the page's aliases or facts citing mail from that address) are stated `is_contact` when exactly one card matches, proposed when one card matches by full name only; a recording speaker identity whose confirmed name matches a person page (name or alias) states `spoke_in` links for that identity's passages, and the owner identity links to the self page; when sent mail is ingested, its recipients matched to person pages by address get `wrote`/`received` links on the mail item; calendar events and reminders in granted calendars link attendees by address (`attended`, `organized`) and titles that name a project page by exact name (`about`, proposed); receipts already matched to a transaction state `receipt_for`, and a transaction's merchant matches an organization page by exact name or alias (`paid`, proposed). Each pass is bounded per run and idempotent through the unique key. Tests per source with fixtures.

Milestone 4, the dashboard. On a memory page (`web/src/pages/knowledge.tsx`, `PageView`), a Linked records section groups links by kind with title, date and a link that opens the record, shows proposed links with Confirm and Remove, and offers Link contact (a picker over the person's contacts) for person pages. Fact provenance (`Provenance`) becomes a link for record evidence. Each record view shows In memory with the linked pages: the contact card, a mail message, a calendar event and a reminder, a finance transaction and a receipt, and a recording passage (in the recording bubble's actions, as "In memory"). The graph view (`knowledgeExplore.tsx`) draws record nodes as a distinct shape with the record kind's icon, expandable from a page. Follow `docs/coding/frontend-design.md`; strings in English, Chinese and Japanese.

Milestone 5, the iPhone and the CLI. The iPhone's knowledge page (`KnowledgeViews.swift`) shows Linked records with the same grouping and actions, the contact line becomes the linked card with Link contact, fact sources open the record, and the mail, calendar, contact, finance and recording screens show In memory. The CLI gains `teanode memory links <page-path>`, `teanode memory backlinks --record <kind>:<id>`, `teanode memory link <page-path> --record <kind>:<id> --relation <relation>` and `unlink`, documented in `docs/reference/command-line.md`. Deploy the server before any app build that reads the new fields.

## Concrete Steps

Work on a branch from `feat/recording-live-transcription` in its own clone under `~/.cache/claude-tmp/`. Use `TMPDIR=~/.cache/claude-tmp/tmp`. Database tests need a PostgreSQL with pgvector on port 5432 (`docker run -d --rm --name <name> -e POSTGRES_USER=teanode -e POSTGRES_PASSWORD=teanode pgvector/pgvector:pg17`, then `TEANODE_TEST_DATABASE_HOST=<its address> go test ./internal/db/ ...`). Web: `cd web && npx tsc --noEmit -p . && npx vitest run`. iOS is built by CI on branches named `ios/**`.

## Validation and Acceptance

Each milestone's tests above fail before and pass after. End to end on the deployed server: a person page shows the person's contact card and their linked mail, meetings and recording passages, each opening the record; a mail message shows In memory; the agent answers a question about a person's phone number by reading the linked card when the book is granted and says it cannot when it is not; a fact from a recording opens the recording at the passage.

## Idempotence and Recovery

The migration is additive with a reverse file; links are derived or person-made data that can be removed without touching the records. Automatic passes are idempotent by the unique key and bounded per run.

## Artifacts and Notes

None yet.

## Interfaces and Dependencies

The reference for Milestones 3 to 5. Naming follows `CONTRIBUTING.md`.

Table `agent_record_link` (migration 0176): `id`, `agent_id`, `node_id` (cascade with the agent and the page), `record_kind`, `record_id` (up to 128 characters, no foreign key), `record_detail` (jsonb `{transcriptRunId, mailboxId}`), `passage_start_ms`, `passage_end_ms`, `record_relation`, `link_status`, `evidence` (jsonb, the shape of fact evidence), `note`, `happened_at`, `created_at`, `modified_at`. Unique on (agent, page, kind, id, relation, passage start, passage end).

Go: `models.AgentRecordLink` (`Validate`, `Reference`, `Proposed`), `models.AgentRecordKind` (`contact`, `mail`, `calendar_event`, `reminder`, `conversation_message`, `finance_transaction`, `finance_receipt`, `recording_passage`), `models.AgentRecordRelation` (`is_contact`, `wrote`, `received`, `attended`, `organized`, `spoke_in`, `mentioned_in`, `paid`, `paid_by`, `receipt_for`, `about`), `models.RecordRelationsOf(kind)` (which relations fit a kind), `models.FormatRecordReference` and `ParseRecordReference` (`kind:id`, and `recording_passage:ID@START-END`). `models.Evidence` gains `TranscriptRunID`, `StartMS`, `EndMS` and the evidence kinds `calendar_event`, `reminder`, `conversation_message`, `finance_transaction`, `finance_receipt`, `recording_passage` (`mail` and `contact` existed). Database: `PutAgentRecordLink` (upsert by the unique key; a stated link is never turned into a proposal), `DeleteAgentRecordLink` (by page, kind, id, optional relation and range), `GetAgentRecordLink`, `ListAgentRecordLinks(agent, page)`, `ListAgentRecordBacklinks(agent, kind, id, startMS, endMS, limit)` (range overlap for passages), `SetAgentNodeContact(agent, page, contactId)`. Package `internal/recordlink`: `Reader{UserID, AgentID, IsAgent}`, `Resolve` and `ResolveRecord` returning `Resolved{Title, HappenedAt, DashboardPath, IsUnavailable, IsWithheld, Contact}`; Milestone 3 passes should use it to confirm a record before linking it.

GraphQL (all need `agent:use`, all scoped to the caller's own agent):
- `AgentGraphPage` gains `recordLinks` (the first 50, each an `AgentRecordLinkView`), `recordLinkCount` and `evidenceRecords` (`AgentEvidenceRecordView`: `factId`, `evidenceIndex`, `recordKind`, `recordId`, `passageStartMS`, `passageEndMS`, `transcriptRunId`, `recordTitle`, `dashboardPath`, `isUnavailable`; evidence of a document read from a sent message is mapped to its mail item).
- Query `ListAgentRecordLinks(path, first, offset)` returns `{recordLinks, recordLinkCount, nextOffset}`, at most 200 at a time.
- Query `ReadRecordMemoryLinks(recordKind, recordId, passageStartMS, passageEndMS)` returns `[AgentRecordLinkView]` with `nodePath`, `nodeName`, `nodeKind` of each linking page; for a passage the range narrows it to overlapping passages.
- Mutation `LinkAgentRecord(path, recordKind, recordId, recordRelation, passageStartMS, passageEndMS, transcriptRunId, note)` states a link and returns the view; the record must be the person's and exist, linking again confirms a proposal.
- Mutation `UnlinkAgentRecord(path, recordKind, recordId, recordRelation, passageStartMS, passageEndMS)` returns whether there was a link; no relation is all of them.
- Mutation `SetAgentNodeContact(path, contactId)` for a person page (not self, which keeps `SetMyContact`): the card has to be in one of the person's own address books; writes `contact_id` and a stated `is_contact` link, moves the link when the card changes, and clears both on an empty `contactId`. It never creates a card.
- `AgentRecordLinkView`: `id`, `nodePath`, `nodeName`, `nodeKind`, `recordKind`, `recordId`, `recordRelation`, `linkStatus`, `passageStartMS`, `passageEndMS`, `transcriptRunId`, `note`, `evidence`, `happenedAt`, `createdAt`, and found at read time `recordTitle`, `recordHappenedAt`, `dashboardPath`, `isUnavailable`. The person's views are never withheld; the agent's are (see the memory tool).

Memory tool (`internal/agent/tools/memory`): `links` (`path`, `link_offset`; 40 at a time, each link as `- <relation> <kind>:<id>  <title>  (<date>)`, with a card's name, organization, addresses and telephone for a readable contact, and `[the person has not shared this with you...]` or `[no longer there]` otherwise), `backlinks` (`record`), `link` and `unlink` with `record`, `relation`, `text` (the link's note), `link_status` (`proposed` by default), `quote`, `transcript_run`. `get` of a person page includes the linked card when its book is granted. Reads are `RiskRead`; link and unlink are writes.

CLI (`docs/reference/command-line.md`): `teanode agent memory links <path>`, `backlinks --record <kind>:<id>`, `link <path> --record <kind>:<id> --relation <relation> [--note] [--transcript-run]`, `unlink <path> --record <kind>:<id> [--relation]`, `contact <path> <contact-id> | --clear`; `--json` on the reads. Client: `client.ListAgentRecordLinks`, `ReadRecordMemoryLinks`, `LinkAgentRecord`, `UnlinkAgentRecord`, `SetAgentNodeContact` and their `Document...` strings (checked against the schema by `TestClientDocumentsMatchTheSchema`).
