# Circle sharing: everything a family shares, at the level each person chooses

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here. It builds on `docs/planning/circles-and-chats-execplan.md` (circles, roles and the first share kinds) and on the plans of each kind it touches (`location-execplan.md`, `health-execplan.md`, `passwords-execplan.md`, `photos-execplan.md`, the finance and recording plans).

## Purpose / Big Picture

A circle is a group of accounts on one TeaNode server (a family, friends, colleagues), kept in the tables `circle` and `circle_member`. A share is a row that lets one person, or everyone in one circle, read something of another person's, at a level the sharer picks. Before this plan a person could share calendars, location, Health, password items, memory pages, paid skills and photo albums, each in its own corner of the dashboard and the iPhone app, and could not share finance, recordings or contacts at all. An audit of every kind (2026-10-22) also found places where shared data reached further than its level said.

After this plan a person can:

- share a finance account with their family at one of three levels (balance, spending, transactions), a recording at two (summary, transcript), and keep a circle address book that every member edits, besides what they could share before;
- see on one Sharing page (dashboard, iPhone, `teanode sharing`, the agent's circle tool) everything they share and everything shared with them, stop any of their shares there, and remove a share made to them by name;
- be told when something is shared with them, and when somebody joins a circle their shares reach;
- trust that the agent never repeats what others shared with the person into a group chat, never reads another person's location in a run that a mail or a webhook started, never reads Health or finance shared with the person, and that a shared memory page never carries what a recording, a finance record, the location journal or a private calendar event said;
- share an album without the names they gave to faces, and know that a playback address stops working when the share that let a reader in ends.

To see it working: on a development server with three accounts in one circle "Family", share a finance account at balance with the circle; the second account's Finance page shows the balance and no transactions; raise it to transactions; the second account is told and now sees transactions; `teanode sharing` on the first account lists the share, and `teanode sharing` on the second lists it under shared with me. Remove the second account from the circle: the share stops reaching it at once.

## Progress

- [x] (2026-10-22) Audit of every kind by six read-only reviewers: calendars and contacts, photos and albums, passwords and locations, Health and finance, memory pages and recordings, and the circle core. Findings are folded into the milestones below.
- [x] (2026-10-22) The maintainer decided the new kinds (Decision Log).
- [x] (2026-10-22) Milestone 1: the agent keeps shared data where it belongs (location runs, chat guards for calendar and photos, Health refused to the agent actor, album-unshare a write, album targets resolved like every other kind).
- [x] (2026-10-22) Milestone 2: what an album reader learns (face names behind a share setting, migration 0253; no capture time zone or original digest without places; playback addresses re-checked; `photos:use` on every photo read; counts and covers without unreadable photos).
- [x] (2026-10-22) Milestone 3: shared memory pages show only what was meant (recording words never quoted, facts from kinds with their own sharing withheld, shares follow moves and end with deletes, the preview lists the pages reached, the reach checks permissions, compaction keeps the shared marker).
- [x] (2026-10-22) Milestone 4: circles, calendar, wording and docs.
- [x] (2026-10-22) Milestone 5: the Sharing page, notices, declining a share (no new notification kind was needed, so no migration).
- [x] (2026-10-22) Milestone 6: finance account sharing (migration 0254).
- [x] (2026-10-22) Milestone 7: recording sharing (migration 0255).
- [x] (2026-10-22) Milestone 8: a circle's own address book (migration 0256).
- [ ] (in progress 2026-10-22) Integration: one branch, every package's tests, the dashboard checks, a development server walk at 1400 and 390 wide in light and dark, the iPhone screenshots from CI.
- [x] (2026-10-22) Review round 1 (four reviewers: finance and recordings, the circle address book, migrations and the Sharing page, the dashboard and iPhone): one high (a calendar shared at busy showed its title on the Sharing page), about fifteen medium, all fixed, among them a contact id that named two books, a summary that carried withheld facts, a spending level that could be read a day at a time, and the agent's own computer commands not counted as the agent's.
- [x] (2026-10-22) Deployed to production from the branch (v0.193.8-104-gcd02f1925, migrations 0253 to 0256 applied after a dump of `photo_album_share` and `addressbook`) and TestFlight dispatched from it.
- [x] (2026-10-22) Review round 2 (security across every kind; the dashboard, iPhone and docs): no high, four medium (the agent's command line reading address books its tool may not, the circle tool's Sharing list in a chat, shared search reading every fact on each search, the iPhone Finance screen for a person with only shared accounts), all fixed with the lows.
- [ ] Review round 3 on the merged result; merge on the maintainer's word.

## Surprises & Discoveries

- Observation: photo album shares were covered by the circle cleanup all along, under another name (`releasePhotoAlbumsOutsideCircles`), which made them look forgotten.
  Evidence: `internal/db/database_circle.go`, the last call in `endSharesOutsideCircles`. Milestone 4 renames it `endPhotoAlbumSharesOutsideCircles`.
- Observation: a test in the photo work expected a card text its own fixture could not produce (the share in the fixture had face names off), so it failed on its branch alone; the fixture was corrected at integration.
- Observation: the computer program already set `TEANODE_AGENT_COMMAND` on every command the agent runs; only Passwords read it. The command line now sends it as a header and the server counts such a request as the agent's, which closes the route by which the agent's shell could read shared Health, finance or recordings.
  Evidence: `internal/client/client.go`, `graph.auditPrincipal`.
- Observation: no photo original on the server was old enough to move to the second server when the transition was shortened to 7 days, because the import began on 2026-10-17. Not part of this plan's code; recorded because it decided the storage timing.

## Decision Log

- Decision: finance is shared per account at `balance`, `spending` or `transactions`; never receipts, paystubs, tax forms, credentials, rules or budgets; the recipient's agent never reads it; creating or widening needs the person's tap, as Health does.
  Rationale: the maintainer chose this (2026-10-22). A balance alone is useful to a household and reveals the least; merchant names and memos are text from outside and must not reach another person's agent.
  Date/Author: 2026-10-22, maintainer.
- Decision: a recording is shared at `summary` or `transcript`, never audio or voice samples; the share card says the other speakers did not agree; the recipient's agent reads it only in a conversation the recipient started and not in a chat, and nothing of it is filed in memory.
  Rationale: the maintainer chose this (2026-10-22). Voices are the most sensitive part and cannot be redacted by passage.
  Date/Author: 2026-10-22, maintainer.
- Decision: personal address books stay private; a circle can have its own address book that every member reads and edits, like the circle calendar.
  Rationale: the maintainer chose this (2026-10-22). A shared family book is what families want; a personal book holds notes and ties to faces and speakers that are the owner's alone.
  Date/Author: 2026-10-22, maintainer.
- Decision: a fact on a shared memory page whose every piece of evidence is a finance record, a recording, the location journal or a calendar event marked private is not shown to the reader, and the preview says how many are kept back.
  Rationale: those kinds have a sharing of their own with levels; a page share must not go around it. A fact with any other evidence, or the person's own words, stays.
  Date/Author: 2026-10-22, audit.
- Decision: face names in a shared album are off unless the share includes them; the contact and group identifiers never travel.
  Rationale: the names are the owner's address book, and the audit found every reader received them with no way to opt out.
  Date/Author: 2026-10-22, audit.
- Decision: a new member of a circle is not blocked from existing circle shares (except Health and finance, which need a tap); instead the owners are told who joined and what they can now see.
  Rationale: blocking would make every family invitation a negotiation; a notice keeps the owner informed and able to stop a share.
  Date/Author: 2026-10-22, audit.

## Outcomes & Retrospective

To be written when the work ships.

## Context and Orientation

The share kinds and where they live. Each has a table with `grantee_user_id` or `grantee_circle_id` (exactly one set), a function that answers whether and at what level a reader is reached (a SQL join on `circle_member` for both owner and reader, so leaving a circle ends a circle share at once), and a cleanup function that `endSharesOutsideCircles` in `internal/db/database_circle.go` calls when somebody leaves a circle or a circle is deleted, which deletes direct shares between people who no longer share any circle.

Calendars: `calendar_share` (migration 0195), `internal/db/database_calendar_share.go`, levels `busy`, `titles`, `details`, `edit`, redaction in `internal/calendar/visibility.go`. Location: `location_share` (0240), levels `current`, `history`. Health: `health_share` (0245), a list of categories. Passwords: `password_share` and `password_share_key` (0251), end-to-end encrypted, no levels. Memory pages: `agent_page_share` (0196), a path and everything under it, `is_quote_shared`. Skills: `skill_share` (0197), people only. Photo albums: `photo_album_share` (0224), `view` or `contribute`, `is_location_included`, and from this plan `is_face_names_included`.

Surfaces: GraphQL resolvers in `internal/api/v1api/apigraph/`, the agent's tools in `internal/agent/tools/`, the command line in `internal/cmd/` over `internal/client/`, the dashboard in `web/src/`, the iPhone app in `ios/TeaNode/Sources/` with its shared models in `ios/TeaNodeKit/`. A share target is typed as a username or a circle name and resolved by `models.ResolveShareTarget` (`internal/models/share_target.go`): a username wins, and a circle name two circles share is refused with their ids.

## Plan of Work

Milestones 1 to 3 are done (see Progress); each is a set of commits on the branch with tests next to the code they change. Milestone 4 fixes the circle core: the calendar shown at busy no longer carries its title, the calendar reach checks `calendar:use`, Health's tap is checked when an invitation is answered, an operator adding a member tells the circle's admins, leaving a circle ends live location maps, a test fails when a table with grantee columns lacks cleanup, circle admins rename and delete the circle calendar, the dashboard and iPhone change a calendar share's level in place, the leave and delete dialogs say what ends, and the wording is made one across languages. Milestone 5 adds `ListSharing` in `apigraph/sharing.go` and its four surfaces, the shared-with-you and joined notices, and declining a direct share. Milestones 6 to 8 add the three new kinds, each following the calendar's pattern and each with its cleanup added to `endSharesOutsideCircles`; finance and recordings expose `List<Kind>SharesBy` and `List<Kind>SharesReaching` for the Sharing page.

## Concrete Steps

From the repository root, with a throwaway PostgreSQL in Docker (`docker run -d --name sharing-pg -e POSTGRES_DB=teanode -e POSTGRES_USER=teanode -e POSTGRES_PASSWORD=teanode -e POSTGRES_HOST_AUTH_METHOD=trust pgvector/pgvector:pg17`) and `TEANODE_TEST_DATABASE_HOST` set to its address, run one package at a time:

    go test -mod=vendor -count=1 ./internal/db/ -run 'Share|Circle'
    go test -mod=vendor -count=1 ./internal/agent/tools/photo/ ./internal/agent/tools/location/ ./internal/agent/tools/calendar/
    go test -mod=vendor -count=1 ./internal/api/v1api/apigraph/ -run 'Share|Health|Photo|Circle|Finance|Recording|Contact'
    cd web && npx tsc --noEmit -p . && npx vitest run

Each prints `ok` per package; a failure names the test and the expectation it missed.

## Validation and Acceptance

Besides the tests, on a development server with accounts alex, sam and kim in a circle "Family": each kind is shared by alex at each level and read by sam on the dashboard, the iPhone simulator and `teanode`, showing exactly the level's fields; kim, removed from the circle, sees nothing at once; `teanode sharing` lists every share on both sides; in a group chat, asking alex's agent for sam's calendar is refused; a shared memory page with a fact read from a recording shows no quote and no such fact.

## Idempotence and Recovery

Migrations 0253, 0254, 0255 and 0256 each have a reverse script. Before deploying, back up the tables they change (`pg_dump -t photo_album_share -t addressbook` and so on) as `docs/coding/database-migrations.md` describes; a deployed migration is never edited again.

## Artifacts and Notes

The audit reports are summarized in the Decision Log and the milestones; their file and line references were checked by the agents that made each fix.

## Interfaces and Dependencies

New GraphQL operations: `ListSharing`, the decline mutation, `ShareFinanceAccount` / `UnshareFinanceAccount` / `ListFinanceAccountShares` / `ListSharedFinanceAccounts`, the recording share operations, and create, rename and delete of a circle address book. New columns and tables: `photo_album_share.is_face_names_included`, `finance_account_share`, `recording_share`, `addressbook.circle_id`.
