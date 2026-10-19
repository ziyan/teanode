# Contacts that are people in your circles

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md` (the ExecPlan rules) as the other plans under `docs/planning/` do.

## Purpose / Big Picture

A person names the people in their photos with contacts from their address book ("Sam"). The same Sam may also have an account on this server and share a circle with them. Today nothing ties the two: Photos knows a face group is the contact Sam, Circles and Location know the account sam, and the agent cannot get from "my wife" (a relationship, between accounts) to her photos (a face group, named by a contact).

After this change, a contact can say which account on this server it is, chosen from the people the owner shares a circle with. Then:

- Photos shows a linked person with their circle picture and the words "In your circles".
- From a circle member (Location's chosen person, the circle page's member row), "Photos" opens the photos they are in.
- The agent finds "photos of my wife" or "photos of Sam" through the relationship and the link, with no matching of names.
- When a contact's email address is an address of a circle member's account, the link is offered for the person to confirm. It is never made silently.

The link is the owner's own note. The linked person never sees it, is never told, and no photos are shared with them.

To see it working: on the dashboard, open a contact who shares a circle with you, choose "Is in your circles as..." and pick the account; Photos then shows that person with the circle picture, and the circle page's row for them offers "Photos".

## Progress

- [x] (2026-10-18) The maintainer chose: the link lives on the contact; it shows in Photos, opens their photos from circle members, is understood by the agent, and is suggested from matching addresses; it is private to the owner.
- [x] (2026-10-18) Tables agreed with the Photos session and the Circles session (see the Decision Log). Migration 0246 (0245 is taken by Health).
- [x] (2026-10-18) Review by the Photos session: `accountUserId` resolved through the caller's contacts only, `account` in the library's address like `contact`, a badge that does not change the tile's height, no contact made silently. Applied.
- [x] (2026-10-18) Review by the Circles session: caller-private `linkedContactId`, suggestions by mailbox address only and without addresses, links lost on re-created cards, refusals in `SetContactAccount`, agent risk levels, book order, a fuller check that the linked person sees nothing. Applied.
- [x] (2026-10-18) The maintainer chose to match mailbox addresses only (relayed by the Circles session).
- [x] (2026-10-18) Milestone 1: the column (0246), the database functions, the cleanup when two people stop sharing a circle.
- [x] (2026-10-18) Milestone 2: GraphQL, the agent tools and the command line.
- [x] (2026-10-18) Milestone 3: the dashboard (Contacts, Photos, Circles, Location).
- [x] (2026-10-18) Milestone 4: the iPhone (Contacts, Photos, Circles, Location) and TeaNodeKit.
- [x] (2026-10-19) Review rounds 1 to 3 (privacy and server, dashboard, iPhone, parity and words); the third found nothing. Walked the contact dialog, Location and Photos in headless Chrome at 1400 and 390 px, light and dark: Change and Remove kept together, and Location's button says whose photos.
- [ ] The iPhone screenshots from CI, deploy with the migration (a backup first), and use it for real.

## Surprises & Discoveries

- The contact table has one writer, `transaction.PutContact` in `internal/db/database_contact.go`, and it saves the whole row (`self.tx.Save(row)`) from a struct built afresh by its callers (CardDAV `PutAddressObject` in `internal/dav/carddav.go`, `addressbook.Commands.Save`, `SetPicture` and `RemovePicture` through `withCard` in `internal/addressbook/commands.go`). A new column would be cleared by every vCard sync and every edit unless each caller carries it over. The link is therefore written only by its own function and never by `PutContact` (see the Decision Log).
- An account has no single email address. Its addresses are its mailbox aliases (`AliasKindMailbox`, read by `loadMailboxAddresses` in `internal/db/database_mailbox.go`) plus its notification address `User.Email`; `addressesOfPerson` in `internal/agent/dream_record_links.go` already gathers them. There is no function from an address to an account.
- The link is lost when a contact moves to another book, or when a phone's CardDAV client deletes the card and puts it again rather than updating it (`DeleteAddressObject` in `internal/dav/carddav.go`): the row is a new one. This is expected, not a bug. The suggestion by address offers it again for the person to confirm.
- Address books are never shared between people: `requireOwnAddressBook` and CardDAV's `bookAt` refuse every book that is not the caller's. So a link is visible only to the book's owner without any new rule.

## Decision Log

- Decision: the link is a nullable column `contact.account_user_id`, referencing `user.id` with `ON DELETE SET NULL`.
  Rationale: the maintainer chose the contact over the face group, so Contacts, Photos and the agent share one link; a face group already points at a contact (0223) and a contact names at most one group (0242), so a second user id on the group could disagree with its contact. `user_id` means the owner everywhere in this schema, so the column is `account_user_id`: the account on this server that this contact is (the Circles session's suggestion, in the spirit of `person_relationship.related_user_id`). The same name in the model (`AccountUserID`), GraphQL (`accountUserId`), the agent (`account_user_id`) and the command line (`--person`).
  Date/Author: 2026-10-18, design.

- Decision: one contact per account in each address book: a partial unique index on (`addressbook_id`, `account_user_id`) where the account is set. Two books of the same person may each hold one.
  Rationale: two contacts in one book for the same account would make "photos of Sam" ambiguous; a person keeps separate books on purpose (work, family), and in that case the agent reads the first book that has one, by the book's `created_at` then `id`, and says which.
  Date/Author: 2026-10-18, design.

- Decision: the link is written only by `transaction.SetContactAccount(addressBookId, contactId, accountUserId string)` and cleared by the same call with an empty id, never by `PutContact`; `PutContact` names its columns so a vCard sync or an edit keeps it.
  `SetContactAccount` refuses, inside its transaction, a link to the book owner's own account and to an account the owner shares no circle with: neither can be a SQL CHECK, since the owner is reached through `addressbook.user_id`.
  Rationale: see Surprises. A vCard has no field for an account on this server, and inventing an X- property would put the link on every phone the book syncs to, which the maintainer's "private to me" rules out.
  Date/Author: 2026-10-18, design.

- Decision: the link is checked when it is made and when it is read: only an account the owner shares a circle with can be linked, and a read shows the link only while they still share one. It is cleared in the transaction in which the two stop sharing any circle, from `endSharesOutsideCircles` in `internal/db/database_circle.go`, by a new `endContactAccountsOutsideCircles(tx, userId)` beside `endRelationshipsOutsideCircles`, in both directions (the user's contacts that point at someone they no longer share a circle with, and other people's contacts that point at the user; the owner is `addressbook.user_id`).
  Rationale: the same rule as relationships and shares (agreed with the Circles session); checking on read too means a row the cleanup missed never shows.
  Date/Author: 2026-10-18, design.

- Decision: a contact cannot be linked to the owner's own account. The owner's own contact stays "my card" (`user.contact_id`, shown in Photos as "You").
  Rationale: two meanings ("this is me" and "this is a circle member") must not share a field (the Photos session's point).
  Date/Author: 2026-10-18, design.

- Decision: suggestions are made, not links. `ListContactAccountSuggestions` answers the owner's contacts that have no link and whose email addresses include a mailbox address of an account the owner shares a circle with. It answers (contact, account) pairs only, never the account's addresses, and never says which address matched. An account's notification address (`User.Email`) is not matched: it is private profile data that circle members do not otherwise see. The person confirms each suggestion.
  Rationale: the maintainer asked for suggestions; a match by address is a strong hint but a shared family address would link the wrong person. The owner already holds the address they match on; the suggestion tells them only that it belongs to an account in their circles (the Circles session's review). The maintainer chose mailbox addresses only (2026-10-18).
  Date/Author: 2026-10-18, design.

- Decision: nothing the linked person reads changes: they are never told and never see the link. The caller's own view of a circle member gains one caller-private field, `linkedContactId` on `CircleMember` (filled by `describeCircleMember`, which already takes the caller's id as `relationshipWord` does): the caller's contact linked to that member, empty when none. The member rows and Location's chosen person offer Photos when it is set and that contact names a face group.
  Rationale: the maintainer chose "private to me"; the rows need the fact without a second request per member.
  Date/Author: 2026-10-18, design.

- Decision: on people-facing surfaces, someone in your circles is a `person`, as Location already says: `teanode photo list --person <username>`, the photo tool's and contact_book's `person`, and the dashboard's `person=<user id>` in Photos' address. GraphQL keeps `accountUserId`, which mirrors the column. Opening a person's photos filters the library to them on the dashboard and the iPhone alike, so a person hidden from People still opens.
  Rationale: review round 1 found four names for one idea and the two apps opening different screens.
  Date/Author: 2026-10-18, review.

## Outcomes & Retrospective

Not started.

## Context and Orientation

TeaNode is a server a person runs for themselves and their family. The repository's guide is `AGENTS.md`; `CONTRIBUTING.md` holds the naming and testing rules.

Contacts live in address books. A row of `contact` (migration `0048_addressbook.sql`) is keyed by (`addressbook_id`, `id`), holds the vCard (`card`) and fields read from it (`name`, `emails`, `phones`), and is written only by `PutContact`. The model is `models.Contact` in `internal/models/contact.go`. GraphQL is in `internal/api/v1api/apigraph/contact.go` (`ContactView`, `SaveContact`); the agent's tool is `contact_book` in `internal/agent/tools/contact/addressbook.go`; the command line is `teanode contact` in `internal/cmd/contact.go`; the dashboard page is `web/src/pages/addressBook.tsx`; the iPhone screens are `ContactDetailView` and `ContactEditor` in `ios/TeaNode/Sources/ContactsView.swift`, with models in TeaNodeKit's `Calendar.swift`.

A circle (`internal/models/circle.go`, `circle_member`) is a group of accounts. `ListCirclePeople(userId)` and `SharesCircle(a, b)` in `internal/db/database_circle.go` answer who shares a circle with whom. When two people stop sharing any circle, `endSharesOutsideCircles` ends what was between them (relationships, calendar and location shares, page and skill shares, album shares).

Photos groups faces into face groups (`photo_face_group`, `internal/models/photo_face.go`). Naming a group points it at a contact (`contact_id`, `contact_address_book_id`, migrations 0223 and 0242). `isOwnContact` marks the group whose contact is the person's own card. `ListPhotos` takes `contactId` to list the photos a contact is in.

Relationships (`person_relationship`, 0243) say what one account is to another ("Sam is my father"). The agent reads them in `internal/agent/ask_relationship.go`.

## Plan of Work

### Milestone 1: storage

Migration `internal/db/migrations/0246_contact_account.sql` and `.reverse.sql`: add `account_user_id character varying(32) NULL REFERENCES "user" ("id") ON DELETE SET NULL` to `contact`, the partial unique index, and an index on `account_user_id` for the cleanup. Comment the column the way 0243 comments its table.

`internal/db/database_contact.go`: `contactModel` gains the column; `PutContact` saves with named columns (`Select` of the vCard columns, or an upsert that leaves `account_user_id` alone), with a test that a `PutContact` after `SetContactAccount` keeps the link. New: `SetContactAccount`, `FindContactByAccount(ownerUserId, accountUserId)` (the first book by `created_at` then `id`, only while they share a circle), and `ListContactAccountSuggestions(ownerUserId)`. Reads of contacts fill `AccountUserID` only while the two share a circle (a condition like `sharedCircleCondition` in `database_relationship.go`, with the owner reached through `addressbook.user_id`). `endContactAccountsOutsideCircles` joins the loop in `endSharesOutsideCircles`.

Tests (PostgreSQL): link and read; refused at the database layer (not only in GraphQL) for the owner's own account and for an account outside their circles; kept across `PutContact`; cleared when the last shared circle goes, in both directions; a second contact in the same book for the same account refused; suggestions from a mailbox address, none from the notification address, none once linked, and the answer carries no address.

### Milestone 2: GraphQL, agent and command line

GraphQL: `ContactView` gains `accountUserId` and `account { userId username name avatarUrl avatarColorIndex }` (the same fields `ListCirclePeople` gives). Mutations `SetContactAccount(addressBookId, contactId, accountUserId)` (an empty id clears). Query `ListContactAccountSuggestions` answering pairs of a contact and an account. `PhotoFaceGroup` gains `accountUserId` and `account`, read through its contact. `ListPhotos` gains `accountUserId`, resolved in one place through the caller's own contacts only (the same path as `contactId`), so an account never lists a face group the caller did not name; when the link is gone (they no longer share a circle) it lists nothing rather than failing.

Agent: `contact_book` gains action `link-account` (contact and `account_user_id` or `username`) and `unlink-account`, and its rows show the account's username. The photo tool's `person-photos` and `list` take `username` (or `account_user_id`) as well as a contact or face group. The relationship lines the agent reads gain, for a related person whose contact has photos, that the photo tool finds them by username. `link-account` and `unlink-account` tell nobody, so they are `RiskWrite`, not `RiskOutward` (unlike `relate`, which tells the other person). The relationship lines gain the photos hint only when the person has `photos:use`, read with one joined query per prompt, never one per person. Suggestions appear only in the `contact_book` tool's answer when the person asks about contacts ("N contacts look like people in your circles; confirm with link-account"), never in the situation every turn.

Command line: `teanode contact link <contact> <username>`, `teanode contact unlink <contact>`, `teanode contact suggestions`; `teanode contact show` prints "In your circles as <username>"; `teanode photo list --person <username>`. Parity tests check the names against the operations.

### Milestone 3: the dashboard

Contacts (`addressBook.tsx`): a contact's dialog shows "In your circles: <name>" with Change and Remove, or "Is in your circles as..." opening a picker of `ListCirclePeople` (excluding accounts already linked in that book). A banner at the top of the page lists the suggestions with Link and Not now (Not now is remembered in the browser for that contact, never on the server).

Photos (`web/src/pages/photos/people.tsx`, `personControls.tsx`): a linked person's tile shows the circle picture (the `avatarUrl` already loaded with `ListCirclePeople`) as a small badge and "In your circles", and is the same height with and without it, checked at 390 px; the naming dialog offers circle people only through a contact that already exists, never making one. The library keeps `account` in the address beside `contact` and `faceGroup` (`web/src/photos/filters.ts`), with the same chip and Back behavior, cleared when another person is picked.

Circles (`circles.tsx`) and Location (`location.tsx`): a member's menu and Location's chosen person gain "Photos", opening `/photos?person=<user id>` (the library reads `person` like `contact`), shown only when the member's `linkedContactId` is set and that contact names a face group.

### Milestone 4: the iPhone

TeaNodeKit: the contact, face group and suggestion models and calls, with decoding tests. `ContactDetailView` shows the link and a picker; `ContactEditor` leaves it alone (it is not part of the card). Photos' people grid shows the badge. `CirclesView` member rows and Location's `LocationPersonPanel` gain "Photos", switching to the Photos section filtered to that person. Demo server data links one demo contact to a demo circle member, so the contacts, photos-people and location-person screenshots show it.

## Concrete Steps

Work in `~/.cache/claude-tmp/teanode-location` on a branch from `origin/main`. Before opening the pull request, check the next free migration number on `main` and in open pull requests (`gh pr list -R ziyan/teanode --state open`), and rename 0246 if it was taken.

    make format
    set -o pipefail; make lint-ci
    TMPDIR=~/.cache/claude-tmp/tmp go test ./internal/db/ -run Contact ./internal/api/v1api/apigraph/ -run 'Contact|PhotoFace' ./internal/cmd/ ./internal/agent/tools/contact/ ./internal/agent/tools/photo/
    cd web && npx vitest run src/pages/addressBook.test.tsx src/pages/photos src/pages/circles.test.tsx && npx tsc --noEmit -p .
    docker run --rm --name kit-contacts -v ~/.cache/claude-tmp/teanode-location/ios/TeaNodeKit:/kit -w /kit swift:6.1 swift test

## Validation and Acceptance

On a development server with two invented accounts in one family circle, each with a mailbox: the first adds a contact with the second's mailbox address. `teanode contact suggestions` lists it; `teanode contact link` links it; a face group named with that contact shows the second account's picture on the dashboard's People; `teanode photo list --person <second>` lists that group's photos; the circle page's member menu offers Photos and it opens them. The second account sees nothing of it: as the second account, `teanode circle people`, `teanode contact list` and `teanode photo list` show no trace of the link, and its `ListCirclePeople` answer has no `linkedContactId` set. Removing the second account from the circle clears the link (`teanode contact show` no longer says "In your circles"). The agent, asked "show me photos of <the relationship word>" after the two said their relationship, answers with that group's photos.

## Idempotence and Recovery

The migration is additive; its reverse drops the index and the column. A link does not survive a contact being deleted and put again by a phone, or moved between books (see Surprises); the suggestion brings it back for confirmation. A backup is taken right before deploying it (`docs/reference/deployment.md`).

## Artifacts and Notes

None yet.

## Interfaces and Dependencies

    // internal/db
    SetContactAccount(addressBookId, contactId, accountUserId string) error
    FindContactByAccount(ownerUserId, accountUserId string) (*models.Contact, error)
    ListContactAccountSuggestions(ownerUserId string) ([]*ContactAccountSuggestion, error)

    // internal/models.Contact
    AccountUserID string `json:"accountUserId"`
