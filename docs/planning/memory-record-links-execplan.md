# Memory pages linked to the person's records

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md` (the ExecPlan rules); a reader needs only this file and the working tree.

## Purpose / Big Picture

The agent keeps a memory: pages about people, organizations, projects and topics, joined by typed links between pages ("works on", "knows", "member of"). The person's own records live elsewhere in TeaNode and are not part of that graph: their contact cards, mail, calendar events and reminders, agent conversations, finance transactions and receipts, and recordings. A person page cannot show the person's contact card (the slot exists but nothing fills it), a fact cannot be opened on the mail or the moment of a recording it came from, and nothing can be asked from a record back to memory ("which pages mention this transaction?").

After this change the records are nodes of the graph too. A memory page links to records with typed links (this person is that contact card; wrote this mail; attended this event; spoke in this recording passage; was paid by this transaction; this receipt is for that transaction), and every link can be walked both ways: from the page to the record and from the record to the pages. The agent can read a person's contact card from their page when the person granted the agent that address book. Facts cite records the person can open with a click. Links are made automatically where a match is sure (an email address, a confirmed speaker name, a receipt already matched to a transaction) and proposed where it is a guess, and the person can make or remove any link by hand.

To see it working: open a person page on the dashboard and see a Linked records section with their contact card, the last mail they sent, the meetings they attended and the recording passages where they spoke, each opening the record; open a mail message and see "In memory" naming the people and topics linked to it; ask the agent "what is Alice's phone number" and see it read the contact through her page.

## Progress

- [ ] Milestone 1: the link model and API (record links table, typed record references, contact link, page links and record backlinks, fact evidence that names a record).
- [ ] Milestone 2: the agent reads and makes links (memory tool operations, contact card on a person page within grants, prompt context).
- [ ] Milestone 3: links made automatically (contacts by address and name, recording speakers and passages, mail participants, calendar attendees, finance merchants and receipts), as stated when sure and proposed when guessed.
- [ ] Milestone 4: the dashboard (Linked records on a page, clickable evidence, In memory on records, contact picker, record nodes in the graph view).
- [ ] Milestone 5: the iPhone app and the CLI, with the same reads and edits.

## Surprises & Discoveries

- Person pages already have `AgentNode.ContactID` (`internal/models/graph.go`, column from migration `0066_agent_graph.sql`), read live by `AgentGraphPage` (`internal/api/v1api/apigraph/agent_graph.go`), and shown as one line on the dashboard and the iPhone, but nothing ever sets it except the person's own self card (`SetMyContact`).
- Evidence kinds `mail`, `commit`, `chat` and `contact` are declared in `internal/models/graph.go` but never written. Received mail is not in memory at all; only sent mail is ingested (`internal/agent/ingest_sent.go`) and cited as `document` evidence.
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

## Outcomes & Retrospective

Not started.

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

New: table `agent_record_link`; `models.AgentRecordLink`, `models.AgentRecordKind`, `models.AgentRecordRelation`; GraphQL `AgentGraphPage.recordLinks`, `ReadRecordMemoryLinks(recordKind, recordId)`, mutations `LinkAgentRecord`, `UnlinkAgentRecord`, `SetAgentNodeContact`; memory tool operations `links`, `backlinks`, `link`, `unlink`; CLI `teanode memory links|backlinks|link|unlink`.
