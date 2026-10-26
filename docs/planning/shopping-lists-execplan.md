# Shopping lists: named lists shared with a circle, checked off live at the store, by receipt, and remembered with their prices

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

Today a shopping list lives in a note or a message, the person retypes it every week, nobody else sees it tick while one of them is in the store, and the receipt that says what was bought and what it cost goes to Finance and nowhere else.

After this plan a person keeps **shopping lists** in TeaNode, on the dashboard, in the iPhone app, with the command line and through the agent:

- A list has a name ("Groceries") and **items**. An item has a description, details (preferences such as "Fuji preferred"), a picture the agent finds on the web, a count needed and a unit, a unit price, the store to get it from (or any store), whether it is a store brand, and the aisle. A list is reused: a bought item stays on it as not needed, with its picture, store and prices, and "Need again" brings it back with a count.
- A list is shared with a circle or a person. Everybody it reaches can add, change and check off items and upload receipts; only the owner shares it further or deletes it.
- At the store, the person **shops a store, not a list**: one screen holds the needed items of every list they reach that are for that store or for any store, grouped by aisle, each saying which list it is on when more than one list is included. Checking an item off shows on everybody's screen at once. An item can be marked out of stock. When somebody starts shopping, the others the list reaches get a notification ("Alex started shopping Groceries", "At Example Market").
- A **receipt** is one system: every Finance receipt, however it arrives (uploaded on a list or in Finance, a mail sorted as a receipt, a photo sent to the agent), is matched against the lists its owner reaches. Matches the model is sure of check items off and remember their price at that store; unsure ones wait on the receipt for the person; Undo puts everything back.
- **Prices** are remembered per item and per store, from receipts and from what people type, and shown over time.
- When the person's phone says they **arrived at a store** (a place whose OpenStreetMap or given name matches an item's store), they get one notification listing what their lists need there, plus generic items when the place is a grocery shop.
- The agent can **import** a list from pasted text or a screenshot ("add this to Groceries"), into a new or an existing list, merging items already there.

To see it working: two invented accounts in one circle. The first creates "Groceries", pastes a sectioned list into Import, adds it; pictures appear on the items within a minute. They share it with the circle. The second opens the list on their phone, presses Start shopping at a store; the first gets "started shopping". The second checks two items off; the first sees them tick on the dashboard without reloading. The second uploads a photo of the receipt in Finance; the two items stay checked, a third that was on the receipt is checked off, and the item's price history shows the receipt's price.

## Progress

- [x] (2026-10-26) Research: receipts, sharing, live events, location arrival, pushes, images, tools and parity tests (see Context and Orientation).
- [x] (2026-10-26) Sketches of every screen (a private design canvas), reviewed by the maintainer, and decisions taken (Decision Log).
- [x] (2026-10-26) This plan written; migration number 0260 claimed with the other sessions.
- [x] (2026-10-26) Milestone 1: storage (migration 0260, models, database operations and their tests); `go test ./internal/db -run Shopping` passes, as does the reverse test.
- [x] (2026-10-26) Milestone 2: GraphQL operations, live events, the receipt matching and picture jobs, the arrival and shopping notifications, the Sharing page's new kind; `TestAShoppingListIsSharedAndCheckedOffLive` and `TestEveryReceiptChecksOffWhatTheShoppingListsNeeded` pass.
- [ ] Milestone 3: the command line and the agent's `shopping` tool, with the parity test.
- [ ] Milestone 4: the dashboard's Shopping page.
- [ ] Milestone 5: the iPhone app's Shopping section.
- [ ] Milestone 6: docs, review rounds, a real run on a development server.

## Surprises & Discoveries

- Observation: there is no image search anywhere in the server. Web search is Brave's web endpoint only, and the link preview code (`internal/linkpreview`) already fetches a page's picture through `safefetch`, scales it and keeps it in the file store.
  Evidence: `internal/agent/tools/websearch/websearch.go`, `internal/linkpreview/fetch.go`.
- Observation: the location code has no hook for the owner's own agent on arrival; `afterLocationChange` in `internal/api/v1api/apigraph/location_history.go` tells only the people the location is shared with.
- Observation: GraphQL names an input object after its Go type with Input added, so a Go type ending in Input became "...InputInput"; the item to add is `ShoppingItemEntry` (GraphQL `ShoppingItemEntryInput`).
  Evidence: `ReceiptLine` in `apigraph/agent_finance_receipt.go` is `ReceiptLineInput` in `internal/client/finance.go`.
- Observation: a job kind is at most 16 characters (the run's job kind column), so the jobs are `match_shopping` and `shopping_picture`.

## Decision Log

- Decision: quantities are one number, the count needed, with a unit. Nothing records what is at home.
  Rationale: the maintainer chose "need only" after seeing need and have sketched.
  Date/Author: 2026-10-26, the maintainer.
- Decision: a bought item stays on its list as not needed; "Need again" brings it back.
  Rationale: the maintainer's choice; the list becomes a catalog that is reused.
  Date/Author: 2026-10-26, the maintainer.
- Decision: pictures come from a web search the agent picks from (the product page's own picture), with "Find another" and an upload of one's own.
  Rationale: the maintainer's choice; free, shows the real packaging of a store brand.
  Date/Author: 2026-10-26, the maintainer.
- Decision: the arrival reminder lists the items for that store plus items for any store when the place is a grocery-type shop by its OpenStreetMap category. Each person who reaches a list chooses the reminder for themselves, and can switch it off for a store.
  Rationale: the maintainer's choice; a pharmacy should not list tomatoes.
  Date/Author: 2026-10-26, the maintainer.
- Decision: people a list is shared with do everything but share it further or delete it. No levels.
  Rationale: the maintainer's choice; shopping together needs everybody to check off and add.
  Date/Author: 2026-10-26, the maintainer.
- Decision: a receipt is one system. Every Finance receipt is matched against the lists its owner reaches, wherever it came from; sure matches are applied at once, unsure ones are proposed, Undo restores.
  Rationale: the maintainer: "if I upload finance receipt it should automatically try to match list items. Receipt is a single system on teanode."
  Date/Author: 2026-10-26, the maintainer.
- Decision: starting to shop pushes a notification to the others the included lists reach.
  Rationale: the maintainer chose the push over showing it in the app only.
  Date/Author: 2026-10-26, the maintainer.
- Decision: the agent adds, changes and checks off without asking; removing items, deleting a list and sharing are its destructive actions, which ask.
  Rationale: the maintainer's choice.
  Date/Author: 2026-10-26, the maintainer.
- Decision: shopping is per store across lists. The same thing on two lists stays two rows, one per list, so that each list's own people see only their list; checking one does not check the other.
  Rationale: the maintainer asked about shopping for several lists at once; keeping rows per list keeps sharing exact.
  Date/Author: 2026-10-26, this plan.
- Decision: a store is a chain name ("Example Market"), compared without case and punctuation, so any branch matches; prices are kept per store name.
  Rationale: what a receipt and OpenStreetMap both give is the name; a branch adds nothing a list needs.
  Date/Author: 2026-10-26, this plan.
- Decision: an item checked at the store is "in the cart" until the trip is finished (Done), a receipt matches it, or somebody starts a new trip on that list more than six hours later; then it is bought (not needed, last bought now).
  Rationale: a check is undone often while shopping; "bought" must not be decided on the first tap, and a forgotten Done must not keep items in the cart for ever.
  Date/Author: 2026-10-26, this plan.
- Decision: importing an item already on the list (same description and store, compared loosely) raises its count needed to the larger of the two and adds the new details, rather than making a second row.
  Rationale: importing the same weekly list twice must not double it.
  Date/Author: 2026-10-26, this plan.
- Decision: a new permission, `shopping:use`, granted to the administrator, operator and member roles by the migration, gates every operation, as `photos:use` gates photos.
  Rationale: an operator can switch a feature off for a role in the usual place.
  Date/Author: 2026-10-26, this plan.

## Outcomes & Retrospective

(To be written at the end.)

## Context and Orientation

TeaNode is one Go server (`cmd/teanode-server`) with a PostgreSQL database, a React dashboard (`web/`), an iPhone app (`ios/`), a command line (`cmd/teanode`), and per-person agents that call the same GraphQL operations through tools. Read `AGENTS.md` and `CONTRIBUTING.md` first.

The pieces this feature joins:

Migrations are numbered SQL files in `internal/db/migrations/` with a `.reverse.sql` beside each and a reverse test (`reverse_*_test.go`). Database operations are interfaces composed in `internal/db/db.go` and implemented on `*transaction` in `internal/db/database_*.go`.

Sharing follows the newest pattern, `recording_share` (`internal/db/migrations/0255_recording_share.sql`, `internal/db/database_recording_share.go`): a share names exactly one of a person or a circle; who it reaches is decided by one query that joins `circle_member`, so a share stops reaching somebody the moment the circle it depended on is gone; `endSharesOutsideCircles` in `internal/db/database_circle.go` deletes direct shares between people who no longer share a circle. The Sharing page lists every kind (`internal/api/v1api/apigraph/sharing.go`, `models.ShareKind`).

Receipts are Finance's (`agent_finance_receipt`, `agent_finance_receipt_line` in migration 0154; `internal/db/database_finance_receipt.go`). Every receipt, from any source, is stored by `Agent.RecordReceipt` in `internal/agent/finance_receipt.go`; that is where the shopping match job is queued.

Agent jobs are queued with `Agent.Enqueue(tx, kind, agentId, mailboxId, subjectId)` and registered in `internal/agent/agent.go`; `thinkAbout` asks a model in a job (see `internal/agent/receipt_job.go`).

Live updates are GraphQL subscriptions over the websocket at `/api/v1/graphql`, fed by PostgreSQL `LISTEN/NOTIFY` (`internal/db/database.go`); the location hub (`internal/api/v1api/apigraph/location_events.go`) is the pattern copied: one listener per process, the event carries an id, the client reads again.

Pushes go through `agent.PhoneNotifier.NotifyPhones(agentId, kind, *push.Content)` (`internal/agent/phone_notifier.go`); kinds are in `internal/models/phone.go`.

Places and visits are in `internal/models/location.go`; a place has `PlaceName`, `IsNamedByPerson`, `OSMName` and `OSMCategory` (such as `shop=supermarket`). `afterLocationChange` in `internal/api/v1api/apigraph/location_history.go` runs after a report is stored and calls the notifier for each arrival.

The command line and the agent's tools share the GraphQL documents in `internal/client/`. Parity tests in `internal/cmd/*_parity_test.go` reflect over the GraphQL interfaces and require each operation to have a command and a tool action.

## The contract every surface shares

Storage (migration `0260_shopping`):

`shopping_list` is a person's named list: `id`, `user_id` (the owner), `list_name`, `created_at`, `updated_at`.

`shopping_list_share` gives one person or one circle the list: `id`, `shopping_list_id`, exactly one of `grantee_user_id` and `grantee_circle_id`, `created_by_user_id`, `created_at`. Reach is the recording-share query: a share to a person needs the two to share a circle; one to a circle needs both the reader and the owner in it; the owner must be enabled and have `shopping:use`.

`shopping_list_reminder_mute` (`shopping_list_id`, `user_id`) is a person who switched the store reminder off for that list; `shopping_store_mute` (`user_id`, `store_key`, `store_name`) is a store they asked never to be reminded at.

`shopping_item` is one thing on one list: `item_description`, `item_details`, `store_name` ('' for any store) and its `store_key` (lower case, letters and digits only), `is_store_brand`, `aisle_name`, `aisle_rank` (the order aisles were given in), `item_rank`, `needed_quantity` (numeric, above zero), `quantity_unit`, `unit_price_amount` and `currency_code` (the latest price), `shopping_item_state` (`needed`, `in_cart`, `not_needed`), `carted_by_user_id` and `carted_at`, `out_of_stock_at`, `out_of_stock_by_user_id` and `out_of_stock_store_name`, `last_bought_at`, the picture (`picture_file_id`, `picture_page_url`, `picture_source` '' or `web` or `upload`, `picture_searched_at`), `created_by_user_id`, `created_at`, `updated_at`.

`shopping_item_price` is one price seen: `shopping_item_id`, `store_name`, `store_key`, `unit_price_amount`, `currency_code`, `quantity`, `quantity_unit`, `observed_on`, `price_source` (`receipt` or `person`), `finance_receipt_id` (set null when the receipt goes), `created_by_user_id`, `created_at`.

`shopping_receipt_match` ties a receipt line to an item: `finance_receipt_id`, `finance_receipt_line_id`, `shopping_item_id`, `receipt_match_state` (`applied`, `proposed`, `dismissed`), `matched_quantity`, what the item was before (`previous_shopping_item_state`, `previous_needed_quantity`), the price it wrote (`shopping_item_price_id`), `decided_by_user_id`, `created_at`, `decided_at`; one row per line and item.

`shopping_trip` is somebody shopping a store now: `user_id`, `store_name`, `store_key`, `started_at`, `finished_at`; `shopping_trip_list` names the lists it includes.

GraphQL (`internal/api/v1api/apigraph/shopping*.go`), every operation needing `shopping:use`:

Queries: `ListShoppingLists`, `GetShoppingList` (items by state, paged), `ListShoppingListShares`, `ListShoppingStores`, `ListShoppingStoreItems` (the store screen), `ListShoppingItemPrices`, `ListShoppingReceiptMatches`, `PreviewShoppingImport` (the model reads text or pictures into items, with what each would merge into).

Mutations: `CreateShoppingList`, `RenameShoppingList`, `DeleteShoppingList`, `ShareShoppingList`, `UnshareShoppingList`, `SetShoppingListReminder`, `SetShoppingStoreReminder`, `AddShoppingItems`, `UpdateShoppingItem`, `RemoveShoppingItems`, `CheckShoppingItem`, `SetShoppingItemNeed`, `SetShoppingItemOutOfStock`, `RecordShoppingItemPrice`, `FindShoppingItemPicture`, `SetShoppingItemPicture`, `StartShopping`, `FinishShopping`, `MatchShoppingReceipt`, `DecideShoppingReceiptMatch`, `UndoShoppingReceipt`, `AddShoppingItemFromReceiptLine`.

Subscription: `ShoppingEvents` says which list changed; clients read it again.

Pictures are served at `GET /api/v1/shopping/pictures/{shoppingItemId}` to anybody who reaches the item's list.

The agent's `shopping` tool has one action per operation; `delete_list`, `share`, `unshare` and `remove_items` are destructive and ask. The command line is `teanode shopping ...` with one subcommand per operation.

## Plan of Work

Milestone 1 writes the migration, its reverse and reverse test, `internal/models/shopping.go`, and `internal/db/database_shopping*.go` with tests against a real database (`internal/db/database_shopping_test.go`).

Milestone 2 writes the GraphQL resolvers, the event hub, the picture route, the two agent jobs (`AgentJobMatchShoppingReceipt` queued from `RecordReceipt`; `AgentJobFindShoppingPicture` queued when an item is added without a picture), the arrival notifier hooked in `afterLocationChange`, the shopping notification kind, and the Sharing page's new kind.

Milestone 3 writes `internal/client/shopping.go`, `internal/cmd/shopping.go`, `internal/agent/tools/shopping/`, and `internal/cmd/shopping_parity_test.go`.

Milestone 4 writes `web/src/pages/shopping/` (lists, list, store, item dialog, import dialog, share dialog, receipt matches) with strings in `web/src/i18n/{en,zh,ja}.ts`, a rail row, and tests.

Milestone 5 writes the iPhone screens (`ios/TeaNode/Sources/Shopping*.swift`), the live feed in TeaNodeKit, demo data in `DemoServer.swift`, `shoot` lines in `.github/workflows/ios.yml`, and strings in `Localizable.xcstrings` in English, Simplified Chinese and Japanese.

Milestone 6 writes `docs/subsystems/shopping.md`, the command-line reference, and runs the whole thing on a development server.

## Validation and Acceptance

Run, from the repository root, the package tests touched (each one at a time, under `nice`): `go test -mod=vendor ./internal/db/ -run Shopping`, `./internal/api/v1api/apigraph/ -run Shopping`, `./internal/agent/... -run Shopping`, `./internal/cmd/ -run Shopping`, the migration reverse test, and `npx vitest run src/pages/shopping` in `web/`. `make lint-ci` passes. On a development server the scenario in Purpose works end to end.

## Idempotence and Recovery

The migration is additive (new tables and one permission row); its reverse drops them. A deploy that applies it takes a backup first, as `docs/reference/deployment.md` says.

## Interfaces and Dependencies

No new libraries. Pictures reuse `internal/linkpreview` (fetch through `safefetch`, scale, store) and the Brave web search configured for the agent; with no search configured an item simply has no picture until one is uploaded.
