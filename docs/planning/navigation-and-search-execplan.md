# A tab bar on the iPhone, and one search across everything

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

The iPhone app is reached through one side menu behind a button. It lists every part of the app (fourteen of them) and, under them, the open part's own navigation: the chats, the mail folders, Finance's parts, Location's lists, Photos' collections. It is long, it changes with the part on screen, and it has to scroll itself to the part the person is in. The person finds it complicated. And nothing searches across the parts: each page searches only its own.

After this change, the iPhone app has a tab bar of icons only (Chats, Mail, Photos, Calendar, More), with a Search tab beside it. The bar shrinks to a small pill while the person scrolls, so it takes no room while reading. Each part keeps its own navigation inside it: Mail opens on its Mailboxes list, Chats on the list of conversations, Finance and Location choose their parts at the top of their own screens. More lists the other parts in short groups, and Settings. On the iPad the same tabs become the pinned sidebar. Search finds the person's words across chats, mail, contacts, photos, memory, documents, recordings, places and finance at once, a few of each with a way to see all of them in that part. The dashboard gets the same search: a box at the top of the sidebar, Control-K or Command-K anywhere, and a results page. `teanode search <words>` and the agent's `search` tool answer the same.

To see it working: open the iPhone app; the side menu button is gone and the tab bar is at the bottom; scroll a mail list and the bar shrinks; tap Search, type a name, and get groups of chats, mail and contacts with it. On the dashboard press Control-K, type the same name, and get the same groups.

## Progress

- [x] Milestone 1 (2026-10-19): the server's search across everything (`internal/search/`), the GraphQL query `SearchEverything`, the agent's `search` tool, `teanode search`, docs. Tests: the fan-out with fake areas, the resolver against a database for access, the agent path under a permission limit, the command and the tool, the `escapeLike` fix.
- [x] (2026-10-19) Milestone 2: the dashboard's search box, Control-K, and the `/search` page. `web/src/searchEverything.ts` (areas, paths, query), `components/sidebarSearch.tsx`, `pages/search.tsx`; tsc, eslint, vitest (1673 tests), `npm run build`, `make check-catalogs check-link-buttons` pass. Not yet run against the real `SearchEverything` resolver or looked at in a browser.
- [x] (2026-10-19) Milestone 3, written without a Swift compiler for the app (the Kit builds and its `SearchTests` pass in Docker): the tab bar replaced the side menu (`MainTabs.swift`, `AppShell.swift`), each part's navigation moved into it, More, the iPad's `TabSection`s, the Search tab (`SearchTab.swift`, `Search.swift` in the Kit), the demo's `SearchEverything` and the `shoot` lines. CI's App job is the first compile, and its screenshots have not been opened yet.
- [ ] Review until a round finds nothing; CI's screenshots opened.

## Surprises & Discoveries

- (2026-10-19, research before the plan) Eight areas already search by text on the server; the search across everything reuses them and adds no index. Calendar events, reminders and todos have no text search; they are left out of the first version (see the Decision Log). `SearchAgentConversations` (`internal/db/database_insight.go:871`) builds its `ILIKE` pattern without `escapeLike`, so `%` and `_` in the words act as wildcards; fix it here.
- (2026-10-19, Milestone 2) Most pages had no address parameter for their words, so "Show all" needed small additions: chats (`/chat?search=` fills the box), contacts (`/contacts?search=` is sent as the `ListContacts` query and shown as a chip that takes it off), Knowledge (`/knowledge?search=` fills the page lookup; `/knowledge?documents=` opens the documents dialog and searches at once). Mail, photos, recordings, places (`placesQuery`) and finance (`text`) already had one. `/mailbox` redirected to the inbox and dropped its query, so `/mailbox?q=&everywhere=true` now keeps it.
- (2026-10-19, Milestone 2) A mail result is opened by asking `GetMailboxItem(itemId)` for its folder, then `/mailbox/<folder>/<id>`; this assumes the result's `targetId` is a mailbox item id (the thread's newest item, not a separate thread id). On a failed lookup it opens the mail search for the words instead. A document has no page of its own, so its row opens the documents dialog with the words. A memory result opens `/knowledge/<page path>`.
- (2026-10-19, Milestone 2) `Control-K` is handled by its own listener in `NavigationShortcuts`, because the shortcut registry ignores keys with a modifier. The rail's box is the whole answer on a wide screen; on a phone (rail behind the menu button) the results page carries its own box, and the key opens the overlay. A narrowed rail shows one search icon; the key or the icon widens it and focuses the box. "Show all" appears only for an area that has more than it shows.

- (2026-10-19, Milestone 3) Photos had no Collections menu to keep: its collections were only in the side menu. They are now the menu under Photos' title (`toolbarTitleMenu` with a `Picker`), and Finance's parts and Location's lists are chosen the same way, since nine parts do not fit a segmented control and a segmented control breaks at the largest text.
- (2026-10-19, Milestone 3) Every part owns a `NavigationStack` (Contacts, Coding, Health, Location, Knowledge and Finance bind a path to it), so pushing them on More's stack would nest stacks. More therefore swaps its list for the part in place, and the part's first screen shows `MoreBackButton` (an environment value, `leavePart`, that only a part under More has). The cost: no swipe back from a part's first screen to More, and a part opened from More is built again when it is reopened.
- (2026-10-19, Milestone 3) A screen with a bar of its own at the bottom hides the tab bar while it is on screen: a conversation (its composer), a mail list (New message, Select), a mail (its actions), a subscription, a coding session. Calendar is a tab with a bottom toolbar of its own (Today, views); how the two bars sit together on iOS 26 is for CI's screenshots to show.
- (2026-10-19, Milestone 3) Mail's tab used to open on the inbox and now opens on the Mailboxes list, as the plan says; a mail link pushes the inbox and then the mail, so back lands in the inbox. Chats opens on the list of conversations, so a notification pushes the conversation on it. The conversation's presence report counts as visible only while a conversation is pushed.
- (2026-10-19, Milestone 3) Search rows: chats, mail, contacts, memory pages and recordings open through the app's existing links (`openedConversationId`, `openedMailItemId`, `openedContactId`, `openedKnowledgePath`, `app.show(path:)`); a photo opens the viewer on the photo in a library search of the words (`openedSearchedPhoto`); a place or a transaction opens its part narrowed to the words, since the app has no screen for one by id; a document has no screen and its row is not a button. Show all (`openedPartSearch`) puts the words in each part's own search: Chats, Mail (every folder), Contacts, Photos, Knowledge, Recordings, Places (Location's Places list) and Finance (Transactions); documents have none.
- (2026-10-19, Milestone 3) The iPad cannot be made to show the sidebar pinned: `.sidebarAdaptable` opens as a tab bar at the top with the sidebar a tap away, and the app has no say in it. In a wide window the tabs show their names (the sidebar needs them) and More is not a tab; the parts under it are tabs in `TabSection`s.

- (2026-10-19, Milestone 1) A GraphQL query runs inside one database transaction, and a transaction runs one statement at a time, so the areas cannot be searched side by side inside the request's. `SearchEverything` is added to `isModelBackedQuery` (it asks no model, but that map is the list of queries that hold no transaction and read in phases), and each area reads through `readInPhase` in a transaction of its own, bound to the area's 3-second context so a slow query is cancelled. The agent's operations always wrap a document in a transaction and a principal; `readInPhase` reuses that principal, which is how a turn's permission limit still holds (tested).
- (2026-10-19, Milestone 1) A photo's search document is made when it is described, or its place or camera is found, not when it is stored; a photo that is only stored is found by its file name only after processing. The resolver test describes its photos for that reason.
- (2026-10-19, Milestone 1) `ListMailboxThreads` counts every matching conversation as well as listing the first few; the mail area calls it as it is (the access check is inside it) and pays for the count. If mail is the slow area in use, split the count out behind an argument; `TestEveryOperationAuthorizes` reads a resolver's own body for its check, so it cannot simply delegate to an unexported function.
- (2026-10-19, Milestone 1) Chats between people (circle chats) are not in the `chats` area. `ListAgentConversations` lists them after `SyncChatsOf`, which writes rows, and a search writes nothing. They are still found by the chats page. If wanted, make a read-only variant of the sync first.
- (2026-10-19, Milestone 1) A tool with no permission of its own is offered to everyone, and a person with only `mail:read` was offered exactly `tools.DeferralThreshold` (40) tools, so `search` made 41 and the connected server's tools in `TestConnectedServerToolsAreOfferedWithTheirRisk` waited behind `tool_search`. That test now disables `search` in its policy (it is not about search). `TestTheCatalogStaysShort` allowed 69 tools; it allows 70 with the reason written beside the number. Whoever adds the next ungated tool meets the first again.
- (2026-10-19, Milestone 1) `docs/reference/project-structure.md` was not touched (only the two docs the brief named); `internal/search` is not listed there.

## Decision Log

- Decision: icons only in the tab bar, each tab's name kept as its accessibility label; the bar minimizes on scroll (`.tabBarMinimizeBehavior(.onScrollDown)`). The person asked for both to save space. Date: 2026-10-19.
- Decision: the tabs are Chats, Mail, Photos, Calendar and More, with Search as the tab bar's search tab (`Tab(role: .search)`), the defaults offered to the person, who did not change them. Date: 2026-10-19.
- Decision: More is the app's own tab, a grouped list, not the system's automatic More: Every day (Contacts, Location, Health), Work (Coding, Knowledge, Recordings), Plans (Goals, Ideas), Money (Finance), then Settings. A part the server does not offer is left out, as the side menu leaves it out today. Date: 2026-10-19.
- Decision: the search across everything runs each area's existing search in parallel, each under the person's own access check for that area, and leaves out an area the person may not use; one area failing or taking longer than 3 seconds is reported as such and does not fail the rest. Five results an area, with how many more and the words to open that area's own search with. Words only: no embedding, so a search costs no model call. Date: 2026-10-19.
- Decision: where the search decides access (Milestone 1). `internal/search` holds the types, the area constants, the fan-out (`Everything`) and an error, `ErrNotAvailable`, and nothing about people: it takes one `Searcher` (area name and a `Func`) for each area to search. The areas are built in `internal/api/v1api/apigraph/search.go`, where each one calls the helper its own page calls to decide access (`requireConversationPerson`, `requireMailbox` through `ListMailboxThreads`, `requireOwnAddressBook`, `requirePhotosPerson`, `requireAgentPerson`, `requirePermission` for recordings, `requireLocationPerson`), so each area has one place that decides access and the search cannot read what the page would refuse. The alternative, a search package that takes the user, agent id and permissions, would have repeated each check. The signature differs from the one first written (`Everything(ctx, settings, person, words, perAreaLimit)`): it is `Everything(ctx, searchers, words, Options{PerAreaLimit, AreaTimeout})`. A refusal from a helper (not found, not signed in, service off, no agent that is on) becomes `ErrNotAvailable` and leaves the area out; any other error, a panic or the 3 seconds going by puts the area in `failedAreas`. Date: 2026-10-19.
- Decision: the GraphQL types are `SearchEverythingView`, `SearchAreaView` and `SearchResultView`, with the fields and JSON names of `Results`, `AreaResults` and `Result`. The schema names a type by its Go name, and `Result` and `Results` would collide with other types. Date: 2026-10-19.
- Decision: the agent's `search` tool is a read (`Core`, no permission of its own; the areas decide), calls `SearchEverything` through the operations like the other tools, marks what it found untrusted, and names for each area the tool that finds more of it. Date: 2026-10-19.
- Decision: documents are a document once however many passages matched (three times the limit in passages are read, then deduplicated), and memory is pages first and then facts, a page once. Date: 2026-10-19.
- Decision: calendar events, reminders and todos are not searched in this version; they have no text search to reuse, and calendar events live in CalDAV objects. Date: 2026-10-19.

## Outcomes & Retrospective

(To be written when the work is done.)

## Context and Orientation

Terms: a "part" is one of the app's sections (`AppSection` in `ios/TeaNode/Sources/AppShell.swift`: agent, goals, ideas, mail, calendar, contacts, photos, location, health, knowledge, finance, recordings, coding, settings). An "area" is a kind of data the search covers.

Server. GraphQL resolvers are in `internal/api/v1api/apigraph/`; the schema is generated from the Go types and doc comments (`make generate`). The existing searches to reuse, with their access checks:

    chats        SearchAgentConversations(agentId, query, limit)       internal/db/database_insight.go:871      requireConversationPerson, authorize.go:416
    mail         ListThreads(folderId, &ItemOptions{Search})            internal/db/database_mailbox.go:1191      requireMailbox, mailbox.go:128 (whole mailbox; notes left out)
    contacts     ListContacts(addressBookId, query, limit)              internal/db/database_contact.go:234       requireOwnAddressBook, contact.go:220
    photos       ListPhotos(userId, &PhotoFilter{SearchText})           internal/db/database_photo.go:759         requirePhotosPerson, photo.go:276 (words only, no meaning vector)
    memory       SearchAgentGraphFrom(agentId, query, offset, limit)    internal/db/database_graph.go:1402        requireAgentPerson, agent_ask.go:436
    documents    indexed.Search (SearchAgentDocuments)                  internal/agent/indexed/indexed.go:241     requireAgentPerson (passages with snippets: files, coding transcripts, chats read by the agent)
    recordings   ListRecordings(userId, beforeId, searchText, limit)    internal/db/database_recording.go:185     recording.go:103
    places       ListPlaces(userId, PlaceFilter{Query})                 internal/db/database_location.go:891      requireLocationPerson, location_history.go:547
    finance      ListFinanceTransactions(agentId, filter{Text})         internal/db/database_finance.go:1718      requireAgentPerson + requireFinanceOffered, agent_finance.go:736

The agent's tools are in `internal/agent/tools/`; the CLI in `internal/cmd/` with its client in `internal/client/`. A parity test checks that every operation is on every surface (`internal/cmd` parity tests; read them).

Dashboard. React in `web/`. The sidebar is `web/src/components/sidebar.tsx` (find it); `web/src/components/navigationShortcuts.tsx` handles keys (`/` focuses the page's search box, near line 119). Routes are in `web/src/App.tsx` or the router file (find `path="/chat"`). Strings in `web/src/i18n/{en,zh,ja}.ts`. Read `docs/coding/frontend-design.md`. Each area's page takes its search words in the URL already (mail `search`, photos `searchText`, and so on; read each page) so a group's "Show all" opens that page with the words.

iPhone. SwiftUI in `ios/TeaNode/Sources/`. `AppShell.swift` (about 900 lines): `AppSection`, `MenuButton`/`MenuBrand` toolbar items, `MainView` (the part on screen and every route that opens one: `.onChange(of: app.openedConversationId)`, `openedMailItemId`, `openedContactId`, `openedSection`, `openedPhotoFilter`, `openedPhotoPerson`, `openedFinancePart`, `openedKnowledgePath`, `openedSharedKnowledge`; keep every one, selecting a tab and pushing what it opens), `SideMenu` (the menu and each part's rows: `ConversationsSheet`, `MailboxesList`, `FinancePart`, `LocationSegment`, Photos' `photos.sections`). `CommonViews.swift` has `SidePanel` and the iPad column (`isMenuPinned`, `readableWidth`). Read `ios/DESIGN.md` and load the `ios-design` skill. Screens are shot by CI from `DemoServer.swift` with `shoot` lines in `.github/workflows/ios.yml`; there is no Swift compiler on this machine, so the app compiles first in CI. TeaNodeKit (`ios/TeaNodeKit/`) builds and tests on Linux in Docker.

Two things the Finance session asked to keep: `FinanceScreen` empties its `NavigationPath` when the part changes (`.onChange(of: part)`), so whatever chooses a part inside Finance must still empty it (or give the stack `.id(part)`); and the `finance-part-switch-dark` shot has a DEBUG task in `AppShell.swift` that sets `financePart = .netWorth` 2.5 seconds in, which moves to wherever the part is chosen now.

## Plan of Work

Milestone 1, the server. Add a package `internal/search` with `func Everything(ctx, settings, person, words string, perAreaLimit int) (*Results, error)`: for each area the person may use, run its search in a goroutine with a 3-second timeout, collect up to perAreaLimit results (default 5, at most 20) and whether there are more (ask for one more than the limit), and keep the order of areas fixed (chats, mail, contacts, photos, memory, documents, recordings, places, finance). A result names its area, an id to open it by, a title, a detail line (a snippet, a sender, a date), and when it happened. An area that failed or timed out is listed in `failedAreas`. Fix the `escapeLike` in `SearchAgentConversations`. GraphQL `SearchEverything(words: String!, perAreaLimit: Int, areas: [String!])`. The agent's tool `search` (read-only, described as finding the person's words across everything and naming each area's own tool for more). `teanode search <words> [--area mail] [--limit 5] [--json]` printing a group per area. Tests with the database test harness for each area's access check and the timeout. Docs: a short `docs/subsystems/search.md` and `docs/reference/command-line.md`.

Milestone 2, the dashboard. A search field at the top of the sidebar (the existing input style), focused by Control-K or Command-K anywhere and by `/` when the page has no search box of its own. Typing and pressing Enter opens `/search?q=<words>`; the page shows one group per area with its results (each a button row that opens the item where it lives) and a "Show all in Mail" button to that page with the words. The phone layout is the same page; nothing scrolls sideways. Strings in three languages. Vitest for the page and the shortcut.

Milestone 3, the iPhone. Replace `SideMenu`, `SidePanel` use, `MenuButton` and `MenuBrand` with a `TabView` (`.tabViewStyle(.sidebarAdaptable)`, `.tabBarMinimizeBehavior(.onScrollDown)`), one `Tab(value:)` per part in the bar and a `Tab(role: .search)`. The tab labels are icons only (an `Image` label with the part's name as its accessibility label; check in CI's screenshots that no title shows). Badges on the tabs (unread chats, coding sessions asking). Chats' root is the conversation list as a screen (from `ConversationsSheet`); Mail's root is the Mailboxes list (from `MailboxesList`), pushing a folder; Finance and Location choose their part with a segmented or menu control at the top of their screens; Photos keeps its Collections menu. More is a grouped `List` of `NavigationLink`s to the other parts and Settings. On the iPad the sidebar shows the groups as `TabSection`s. Every `app.opened…` route selects the right tab and pushes what it opens. The Search tab: a search field, results grouped by area, each row opening the item in its part, and "Show all" opening that part's own search. Demo answers for `SearchEverything`; shoot lines for each tab's root, More, Search with results, light and dark and the largest text, and the iPad. Remove what the menu no longer needs (`showPartItems`, `menuRow` if nothing else uses it, the menu's catalog strings).

## Concrete Steps

From the repository root:

    go test ./internal/search/ ./internal/cmd/ ./internal/agent/tools/... -count=1
    make generate && go build ./...
    cd web && npx tsc --noEmit -p . && npx eslint . --max-warnings 0 && npx vitest run && npm run build
    cd ios/TeaNodeKit && docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD":/kit -w /kit swift:6.1 bash -c 'swift build --build-tests -q; swift test --skip-build'
    node ios/scripts/check-button-colors.mjs
    make lint-ci

## Validation and Acceptance

`teanode search <a name in the person's mail>` prints groups of mail, contacts and chats with that name. The dashboard's Control-K, the words and Enter show the same groups; "Show all in Mail" opens the mail page searching for them. The iPhone screenshots show the icon-only tab bar, More's groups, Search's groups, each part's own navigation (Mailboxes, conversation list, Finance's and Location's choosers), and the iPad sidebar. A notification tap and a link to a conversation, a mail item, a contact and a photo still open them.

## Idempotence and Recovery

No migration and nothing written: the search only reads. The iPhone change is one shell; reverting the PR restores the menu.

## Interfaces and Dependencies

In `internal/search/search.go`:

    const (
        AreaChats = "chats"; AreaMail = "mail"; AreaContacts = "contacts"; AreaPhotos = "photos"
        AreaMemory = "memory"; AreaDocuments = "documents"; AreaRecordings = "recordings"
        AreaPlaces = "places"; AreaFinance = "finance"
    )

    // Result is one thing found: what it is, how to open it, and what to show.
    type Result struct {
        SearchArea  string     `json:"searchArea"`
        TargetID    string     `json:"targetId"`    // the conversation, mail, contact, photo, page path, document, recording, place or transaction id
        TitleText   string     `json:"titleText"`
        DetailText  string     `json:"detailText"`  // a snippet, a sender, an account
        HappenedAt  *time.Time `json:"happenedAt,omitempty"`
    }

    // AreaResults is one area's results.
    type AreaResults struct {
        SearchArea string    `json:"searchArea"`
        Results    []*Result `json:"results"`
        HasMore    bool      `json:"hasMore"`
    }

    // Results is a search across everything.
    type Results struct {
        Words       string         `json:"words"`
        Areas       []*AreaResults `json:"areas"`       // only areas with results, in the fixed order
        FailedAreas []string       `json:"failedAreas"` // areas that failed or timed out
    }

GraphQL `SearchEverything(words: String!, perAreaLimit: Int, areas: [String!]): SearchEverythingView` with the same fields. The tool `search` (arguments `words`, `areas`, `per_area_limit`), the command `teanode search`.
