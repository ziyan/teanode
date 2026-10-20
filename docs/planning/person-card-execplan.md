# A card for each person: who they are, and everything of theirs you can see

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

Another person on the server appears in many places: as the author of a message in a chat, as a row on the Location map, as a member of a circle, as the account a contact is linked to, as a face in Photos. None of these opens anything about the person. What the viewer may see about them is spread over six parts of the app: where they are (Location), the health days they share (Health), the photos of them (Photos, through the linked contact), the mail with them (Mail, by hand, one address and one mailbox at a time), the memory pages about them (Knowledge, through the contact), and their phone and address (Contacts). Recordings they speak in cannot be found at all.

After this change, tapping a person anywhere brings up one card for them. On the iPhone it is a sheet, at half height first, with their picture, name and what they are to the viewer ("Your mother"), and buttons to message, call, mail and get directions. Below come the sections, each a few items with a way to see all of them in their part: where they are now and their visits today, their shared health today (steps, for example), recent photos of them and albums they shared, recent mail with them, recordings they speak in, what memory knows about them (their memory page and the pages linked to it), their contact details, and the circles the two share. On the Location page the card replaces the small chosen-person card: tapping a person centers the map on them and brings the card up over it. The dashboard gets the same card as a page, `/people/<userId>`, opened from the same places. `teanode circle person <username>` and the agent's `circle` tool (action `person`) answer the same.

To see it working: open a chat with Sam, tap Sam's picture on a message, and the card comes up with today's steps, the place Sam is at, the last three mails with Sam and the photos of Sam. Tap Message and the direct chat opens.

## Progress

- [ ] Milestone 1: the server's `ReadPersonCard` query, the agent action, the command, docs.
- [ ] Milestone 2: the dashboard's `/people/<userId>` page and the links into it.
- [ ] Milestone 3: the iPhone's card sheet, the taps that open it, Location's card, demo data and screenshots.
- [ ] Review until a round finds nothing; parity check.

## Surprises & Discoveries

- (2026-10-20, research before the plan) There is no query for one person: the only view of another account is `CircleMember` (`internal/api/v1api/apigraph/circle.go:131`), reached through `ListCirclePeople` or `ReadCircle`. It carries the name, username, picture, relationship and `linkedContactId`, and on purpose not the email or pronoun (`docs/subsystems/circles.md`, "Who sees whom").
- (2026-10-20) Most sections hang on the viewer's contact linked to the person (`Contact.AccountUserID`, migration 0246), not on the account: mail is found by the contact's addresses, photos of them by the face group named by the contact (`ListPhotos(accountUserId)`), memory pages by `ReadRecordMemoryLinks(recordKind: "contact")`. Location, health, shared albums and the chat hang on the account and its shares.
- (2026-10-20) Mail can only be filtered by one `from` or `to` string within one mailbox (`ListMailboxThreads`, `apigraph/mailbox.go:496`); the card needs a search over all the viewer's mailboxes for any of the contact's addresses, done on the server in one query.
- (2026-10-20) A recording's speaker is a name owned by the viewer (`RecordingSpeakerIdentity`, `internal/models/recording_speaker.go:6`) with no link to a contact or an account, and `ListRecordings` filters only by words. See the open decision below.

## Decision Log

- Decision: one server query, `ReadPersonCard(userId)`, fills the whole card, its sections run at once, each under the same access check as the query it stands for, each with a time limit (3 s) and a few items plus whether there are more; a slow or failing section is reported as such and the rest still show.
  Rationale: the same shape as `SearchEverything`, which already works this way; one round trip for the phone, and the card can never show more than the viewer could reach part by part.
  Date/Author: 2026-10-20, Claude.
- Decision: the card is for a person who shares a circle with the viewer; anybody else is "not found", as `requireCircleMember` answers today. Leaving every shared circle ends the card with the shares.
  Rationale: the existing rule in "Who sees whom"; no new way to see an account.
  Date/Author: 2026-10-20, Claude.
- Decision: mail with a person is found by the linked contact's addresses only, never by the account's own address.
  Rationale: the account's address is private profile data; matching the viewer's mail against it would show it by implication. A person with no linked contact gets a row to link one, with the suggestions `ListContactAccountSuggestions` already makes.
  Date/Author: 2026-10-20, Claude.
- Decision: photos are two rows: recent photos of them in the viewer's library (the linked contact's face group) and albums they shared with the viewer.
  Rationale: both already exist as queries; the first needs the contact, the second only the account.
  Date/Author: 2026-10-20, Claude.
- Decision: on the iPhone the card is a sheet (`.presentationDetents([.medium, .large])`), presented from the app model so any screen can open it (`AppModel.openedPersonUserId`); on the dashboard it is a page, `/people/<userId>`.
  Rationale: a sheet over the chat or the map is the system's way to show a person (Find My, Contacts) and keeps what the person was looking at; the dashboard has no reading dialogs, and a page keeps the person in the address.
  Date/Author: 2026-10-20, Claude.
- Decision: on Location the card replaces `LocationChosenPersonCard` and `LocationPersonPanel`; a row still selects the person on the map, and now also brings the card up at half height, with the map above it.
  Rationale: the chosen-person card and the panel are a smaller copy of the card's Location section.
  Date/Author: 2026-10-20, Claude.
- Decision: in Circles, tapping a member opens the card (today it opens the Relationship sheet; Relationship moves into the card, and stays in the row's menu).
  Date/Author: 2026-10-20, Claude.
- Decision: a confirmed speaker name can be tied to one of the person's contacts (migration 0247: `recording_speaker_identity` gets `contact_addressbook_id` and `contact_id`, referencing the contact, cleared when it is deleted). Naming a speaker from a contact's suggestion ties it; the speaker screens can tie, change or untie it later. The card's recordings are those with a passage confirmed as a speaker tied to the contact.
  Rationale: the person chose it over finding recordings only through memory links, which would miss every recording the dreams have not linked.
  Date/Author: 2026-10-20, the person.
- Decision: a contact opens the same card as an account. For a contact not linked to an account in the person's circles, the account's sections (location, health, shared albums, Message, circles) are left out and the rest (contact details, mail, photos of them, memory, recordings) are shown. The card replaces the iPhone's contact screen, keeping its Edit, and the dashboard's contact dialog opens the card's page, its form kept for editing.
  Rationale: the person chose one card for everyone.
  Date/Author: 2026-10-20, the person.
- Decision: the card keeps what #573 settled for relationships: the dashboard saves only the pronoun when only the pronoun changed (`isTermChanged`), the iPhone's `PersonRelationshipDraft.isTermChanged` guard and the pronoun footer stay, the relationship shows only between the two and only while they share a circle, and the demo screen `circle-relationship` still shows the relationship picker.
  Date/Author: 2026-10-20, Claude, from the session that built relationships.

## Outcomes & Retrospective

(Filled in at the end.)

## Context and Orientation

The pieces the card is made of, all existing:

- The person: `CircleMember` (`apigraph/circle.go:131`, built by `describeCircleMember` at `:200`), the relationship (`apigraph/relationship.go`), the picture rule `canSeeAvatar` (`apigraph/user_avatar.go:35`), shared circles through `SharesCircle` (`internal/db/database_circle.go:684`).
- The contact linked to them: `FindContactByAccount` (`internal/db/database_contact.go:577`), `ContactView` (`apigraph/contact.go:91`).
- Chat: `OpenDirectChat(userId)` (`apigraph/chat.go:273`), idempotent; refused unless the two share a circle.
- Location: `reachLocation` (`apigraph/location_history.go:578`) for the share level; `GetLocation` (current) and `ListLocationVisits` (history).
- Health: `reachHealth` (`apigraph/health.go:622`), `ListHealthTypes(ownerUserId)` carries each type's latest day.
- Photos: `ListPhotos` with the account filter (`apigraph/photo.go:403`); albums shared to the viewer carry `ownerUserId` (`internal/models/photo.go:346`).
- Memory: `ReadRecordMemoryLinks(recordKind: "contact", recordId)` (`apigraph/agent_graph_record.go:295`); a page's contact is `AgentNode.ContactID`.
- Mail: the mailbox item filters in `internal/db/database_mailbox.go:1067`; ownership through `requireMailbox` (`apigraph/mailbox.go:127`).
- Recordings: `RecordingSpeakerIdentity` (`internal/models/recording_speaker.go`), passages carry the speaker.
- The agent's `circle` tool group (`internal/agent/tools/circle/circle.go:554`) and the `teanode circle` commands (`internal/cmd/circle.go`).
- iPhone: chat authors (`ios/TeaNode/Sources/ChatViews.swift:244`), the conversation header (`ConversationView.swift:71`), the members sheet (`ChatViews.swift:506`), Location (`LocationPeopleViews.swift`), Circles (`CirclesView.swift:366`), Contacts (`ContactsView.swift:564`), Photos' person (`PhotoPeople.swift:605`). Demo people in `CirclesDemo.swift:13`.
- Dashboard: chat authors (`web/src/components/agentChat.tsx:728`), Circles rows (`pages/circles.tsx:823`), Location rows (`pages/location.tsx:809`), the contact dialog (`pages/addressBook.tsx`).

## Plan of Work

Milestone 1, the server. Add `internal/api/v1api/apigraph/person_card.go` with `ReadPersonCard(userId, itemLimit)`: check circle:use and a shared circle, then run the sections in parallel, each calling the same database functions and access checks as its own query: `person` (the `CircleMember`, shared circles), `contact` (the linked contact's emails, phones, addresses), `location` (share level, latest point, open visit, today's visits at the history level), `health` (each shared type's day today), `photos` (recent photos of them, albums they shared), `mail` (recent threads from or to any of the contact's addresses, across the viewer's mailboxes, a new database function), `recordings` (recordings with a passage confirmed as a speaker tied to the contact), `memory` (pages linked to the contact, the person page first). Each section answers `shown`, `notShared`, `needsContact` or `failed`, its items and `hasMore`. Add `teanode circle person <username or id>` and the `circle` tool's `person` action, with the same words. Docs in `docs/subsystems/circles.md` and `docs/reference/command-line.md`.

Milestone 2, the dashboard. `web/src/pages/person.tsx` at `/people/:userId`, one card per section with Show all into the part (`/location?person=`, `/health?person=`, `/photos?person=`, `/mailbox?from=&everywhere=true`, `/recordings?...`, `/knowledge/<path>`), and the header's Message (opens the direct chat in the drawer), Mail, Call. Links into it from chat authors, Circles rows, Location's chosen person and the contact dialog's "In your circles as". Strings in three languages.

Milestone 3, the iPhone. `ios/TeaNode/Sources/PersonCard.swift` (the sheet), `ios/TeaNodeKit/Sources/TeaNodeKit/PersonCard.swift` (the query and its types, with tests), `AppModel.openedPersonUserId` presented from `MainView`. Taps from chat authors, the direct chat's header, the members sheet, Circles members, Contacts' "In your circles as", Photos' person and Location rows. Location's chosen-person card and panel give way to the sheet. Contacts' detail screen becomes the card, with its Edit. The speaker screens get a contact for a speaker. Demo data for Sam (linked, shares location history and health) and Alex (no contact), `shoot` lines `person-card`, `person-card-full`, `person-card-unlinked` in light, dark and the largest text, and the Location card.

## Concrete Steps

From the repository root: `go test -mod=vendor ./internal/api/v1api/apigraph/ -run 'TestReadPersonCard'` against a PostgreSQL container (`TEANODE_TEST_DATABASE_HOST`), `make lint-ci`; `cd web && npx tsc --noEmit && npx eslint --max-warnings 0 src && npx vitest run`; TeaNodeKit tests in Docker `swift:6.1`; the app compiles first on CI.

## Validation and Acceptance

Deployed: open the card for a person in the viewer's circles from a chat, Location and Circles on the dashboard and the phone; each section shows what the part shows, and nothing for a share that is off. A person outside the viewer's circles is "not found" from the query, the command and the tool. CI's screenshots of the card opened in light, dark and the largest text.

## Idempotence and Recovery

The query reads only. Migration 0247 gets its reverse file and is announced to the other sessions before it is deployed; back up first.

## Interfaces and Dependencies

GraphQL, in `internal/api/v1api/apigraph/`:

    ReadPersonCard(userId: ID!, itemLimit: Int): PersonCard!

    PersonCard { person: CircleMember!, sharedCircles: [CircleSummary!]!, contact: ContactView, sections: [PersonCardSection!]! }
    PersonCardSection { sectionKind: PersonCardSectionKind!, sectionState: PersonCardSectionState!, hasMore: Boolean!, location, healthDays, photos, albums, mailThreads, recordings, memoryPages }
    PersonCardSectionKind: location | health | photos | mail | recordings | memory
    PersonCardSectionState: shown | notShared | needsContact | failed

Built on the `feat/navigation-search` branch (PR #605), since the iPhone shell it presents from is rewritten there; retargeted to `main` once #605 merges.
