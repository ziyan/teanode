# Location: where a person goes, the places they name, and who they share it with

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

Today TeaNode knows where a person is only for the one moment the agent asks their browser or phone, and forgets it right after. After this plan:

- The iPhone app records where the person goes. While the app is open it follows them live; while it is closed or in the background, iOS wakes it when they arrive somewhere, leave somewhere, or move a long way, and it reports that to the server. Each stay becomes a **visit**: a place, when they arrived, when they left, how long they stayed.
- Every visit is at a **place** with a name. The name comes from OpenStreetMap (the name of the park, the café or the school on the map), and the person can rename any place ("Grandma's", "the piano teacher's") and give it a **label**: home, work, school, family, gym. A label is what the person and the agent mean by "at home" or "at work", wherever that is, and it puts an icon on the map. A person can also **check in**: pick a place near them from a list and record that they are there.
- A person can share where they are with a circle (their family) or one person in a circle: just where they are now, or that and their history of visits. The people it is shared with see them on a map on the dashboard and in the app, live while the sharer's app is open, and get a phone notification when the sharer arrives at or leaves a place the sharer named or labeled ("arrived home", "left school").
- The agent sees all of it, the same as the person does: where they are now, where they were last weekend, how long they stayed at the gym this week. It learns their routine in memory ("usually at the gym on Tuesday and Thursday mornings"), and it can search for places near them ("playgrounds within 5 km") to plan a weekend that is not a repeat of the last one.

To see it working at the end: two accounts in one family circle, each with the app on an iPhone. The first shares their location with the circle at `history`. They label their house "home" from the app. They drive to a park and stay twenty minutes: a visit appears on the dashboard's Location page and in the app, at the park's name, with arrival time and duration; the second person's phone gets no notification (the park is not a place the first person named). They drive home: the second person's phone shows "<name> arrived home". The second person opens Location and sees the first person on the map at home, and the visit to the park in their history. The first person asks their agent "where did I go last weekend, and find me a different park for this Saturday": the agent lists the visits and suggests parks it found near home that are not in the history.

## Progress

- [x] (2026-10-17) Research: what exists in the repository, the iOS location services, the place name providers and their terms, map display, reference projects (OwnTracks, Dawarich, Home Assistant, Arc). Summarized in Context and Orientation and the Decision Log.
- [x] (2026-10-17) Maintainer answered the open questions (see the Decision Log): live while open plus background updates when closed; OpenStreetMap for names, check-ins and place search; the agent sees everything; keep everything; no special rules for children; notifications on; built on the circles branch.
- [x] (2026-10-17) This plan written.
- [x] (2026-10-17) Maintainer asked for labels on places (home, work, school and so on); added as `place_label`.
- [x] (2026-10-17) Milestone 0, server half: Nominatim reverse and search, and the OpenFreeMap style's hosts, checked by hand (see Surprises).
- [ ] Milestone 0, the rest: the map drawn under the dashboard's content security policy (with Milestone 3); visits timed on a real iPhone (with Milestone 2's first TestFlight build).
- [x] (2026-10-17) Milestone 1: storage, reporting and naming on the server, with the command line and the agent's tool (built by subagents from briefs, read and tested here).
- [x] (2026-10-17) Milestone 2: the iPhone records, and shows the person their own location (compiles on CI only; first CI build found one inaccessible initializer, fixed).
- [x] (2026-10-17) Milestone 3: the dashboard's Location page.
- [x] (2026-10-17) Milestone 4: sharing with circles, the people on the map, and notifications.
- [x] (2026-10-17) Milestone 5: the agent: memory of where the person goes, their routine, and place search.
- [x] (2026-10-17) Milestone 5b: getting started: setting up Location the first time, the phone tour, and the offer on joining a family circle.
- [x] (2026-10-17) First use on a development server: a family circle of two invented accounts, a morning reported as a phone would, resent unchanged (nothing new), read back with `teanode location now`, `visits`, `places`.
- [x] (2026-10-17) Review round 1: security, correctness, parity, words and conventions, the dashboard, the iPhone; about a hundred findings.
- [x] (2026-10-17) Fix round 1 (four fixers: server data, server API, dashboard, iPhone), review round 2 (43 findings, mostly low), fix round 2, review round 3 (dashboard none; words 2 low; iPhone 4; server 8), fix round 3.
- [x] (2026-10-17) Walked Location in headless Chrome at 1400 and 390 px, light and dark, for a person with a home, a family member sharing at history, and a person with nothing (the setup): found the map had no height (MapLibre's stylesheet), a labeled place shown by its street, a place named "6", one named after a lift, the map credits covering a phone's map, and three phone layout breaks; fixed.
- [x] (2026-10-17) Rebased onto main after circles (#528) merged as v0.170.0.
- [x] (2026-10-17) Review rounds 4 and 5: the server's round 5 and the iPhone's round 4 found nothing a merge would be blocked on; the dashboard was clean from round 3.
- [x] (2026-10-17) The iPhone screenshots from CI looked at with the ux-review skill: the person's own row first and labeled You, button rows in label colors, map contents clear of the Maps logo, red destructive icons, a labeled name field; fixed.
- [x] (2026-10-17) Deployed to production on top of what it ran (deploy/combined-20261017i: the newest combined branch plus location), after a backup; migration 0240 applied, v0.170.0-167-gbcb85558. Used there: the command line, the settings, a search (which found nothing for "coffee": fixed by asking again in OpenStreetMap's word), the map proxy behind the real host, and the agent answering where the person went and finding playgrounds. A TestFlight build of the same branch was dispatched.
- [ ] The maintainer's phone: a day of real visits, and the timings of visit arrivals and departures (Milestone 0's third question).
- [ ] Milestone 6: documentation, the security review section, review rounds, use it for real, deploy.

## Surprises & Discoveries

- Observation: the agent already has a `location` tool, but it only asks the browser (or the app) the person is chatting in, once, and keeps nothing. It is offered only where a browser can answer (`BrowserOnly`).
  Evidence: `internal/agent/tools/location/location.go`, `internal/agent/locate.go` (`AskRun.Locate`, `locationWait`, `AnswerLocation`), `internal/agent/surface.go` (`canLocate`), iOS `LocationReader` in `ios/TeaNode/Sources/ConversationModel.swift` (When In Use only, `CLLocationUpdate.liveUpdates()` with a 20 second timeout).

- Observation: the photos branch (PR #531) already names a spot without any online service: `internal/place` holds a table of every city of 15,000 people or more (GeoNames, CC BY 4.0) built into the server, and `place.Nearest(latitude, longitude)` answers "City, Country". Its package comment says a default code path must not depend on a cloud service. Photos store `latitude`, `longitude` and `place_name`.
  Evidence: `internal/place/place.go` and `internal/photo/process.go` (`namePlace`) on branch `photos`; migration `0220_photo.sql` there.

- Observation: Google Places allows only its place id to be stored, everything else must be fetched again, and its results may only be shown on a Google map. Mapbox requires its own map. Apple's server API reportedly allows only temporary caching. OpenStreetMap data (ODbL) may be stored and shown to anybody with the credit "© OpenStreetMap contributors".
  Evidence: the published terms of each: the Google Maps Platform policies for the Places API, the Mapbox geocoding documentation, and the OpenStreetMap Foundation's Nominatim usage policy (read 2026-10-17).

- Observation: iOS relaunches a closed app for visit events (`startMonitoringVisits`) and for significant location changes (about 500 m, at most every five minutes), without the `location` background mode, as long as the person allowed "Always". From iOS 18, an app that is allowed Always receives updates in the background only while it holds a `CLServiceSession(authorization: .always)`, which it may only start while in the foreground and must start again at every launch. Apple publishes no figure for how late a visit's arrival and departure arrive.
  Evidence: developer.apple.com/documentation/corelocation/cllocationmanager/startmonitoringvisits(); WWDC24 session 10212 "What's new in location authorization".

- Observation: the dashboard's content security policy loads nothing from anywhere but the server (`default-src 'self'`, `img-src 'self' data:`, `connect-src 'self'`), on purpose, because it renders mail written by strangers. A map that fetches tiles from a tile server in the browser would have to widen it.
  Evidence: `internal/web/middlewares.go` (`securityPolicy` and the comment above `MakeSecurityHeadersMiddleware`).

- Observation (Milestone 0): Nominatim's reverse naming at `zoom=18` answers the smallest named thing at the point, which can be a feature inside a place (a clock on a tower rather than the building or the square). Search with words and a bounding box (`viewbox=<west>,<north>,<east>,<south>&bounded=1`) finds things by kind: "playground" in a square of about 3 km answered five playgrounds with `category` `leisure`, `type` `playground`, `osm_type` and `osm_id`, in under a second. Naming a visit therefore asks reverse naming restricted to points of interest first (`layer=poi`), and an address (`layer=address`) when there is none.
  Evidence: `curl -A "TeaNode/..." "https://nominatim.openstreetmap.org/reverse?format=jsonv2&lat=..&lon=..&zoom=18"` answered `category amenity`, `type clock`; the search answered `way ... leisure playground` rows.

- Observation (Milestone 0): every file the OpenFreeMap style names comes from one host: the style (`https://tiles.openfreemap.org/styles/liberty`), its vector tiles (`https://tiles.openfreemap.org/planet`, a TileJSON pointing at dated `{z}/{x}/{y}.pbf` files, zoom 0 to 14), its fonts (`/fonts/{fontstack}/{range}.pbf`), its icons (`/sprites/...`) and a shaded relief (`/natural_earth/...png`). The proxy rewrites that one origin to `/api/v1/map` in the style and the TileJSON, and fetches only from it.
  Evidence: the style's `sources`, `sprite` and `glyphs` fields, read with curl on 2026-10-17.

- Observation (first use): places were never named on a server whose agent is off, because the naming job ran only in the agent's worker, which the server does not build while the agent is off. Location belongs to the person, not to the agent.
  Evidence: a development server with no model: `teanode location places` still said "Paris, France" (the city table's name) for both places after three minutes, and `internal/agent/agent.go` says "Nothing here is constructed while the agent is off".

- Observation (review round 1): joining two visits deleted the row holding the absorbed visit's key, so a resent batch counted it again and notified again; erasing history left the journal documents the agent reads; a departure iOS never sent left a visit open for ever, which the journal read as a whole day there.
  Evidence: `review-1-correctness.md` findings 2 to 4, reproduced with throwaway tests.

## Decision Log

- Decision: the iPhone follows the person live while the app is open, and while it is closed relies on what iOS wakes it for: visits, significant changes, and entering or leaving the places the person named. There is no continuous tracking in the background and no `location` background mode.
  Rationale: the maintainer asked for live tracking when the app is open and background updates when it is not. Continuous background tracking costs the battery of a navigation app and shows the blue status bar pill all day; what iOS wakes the app for costs almost nothing and is what Find My style family sharing needs (where somebody is, within minutes). App Review asks every background use of location to be justified; leaving the background mode out keeps the request small.
  Date/Author: 2026-10-17, maintainer and design.

- Decision: places are named from OpenStreetMap through a configurable provider, Nominatim by default (the OpenStreetMap Foundation's public server), or Photon (for example one the operator runs). With the provider switched off, a place is named after its city from the table the photos branch bundles, and the person can still name it themselves. The same provider answers check-ins (places near here) and the agent's place search.
  Rationale: the maintainer asked for OpenStreetMap or an equivalent. It is the one source whose names may be kept forever and shown to family; Google and Mapbox forbid one or the other. The public Nominatim server allows light use from an application with few users that caches what it gets (one request a second at most, an identifying User-Agent); a TeaNode server has a handful of people, and each place is looked up once, when it is first visited, because the `place` row is the cache.
  Date/Author: 2026-10-17, maintainer and design.

- Decision: the agent reads all of the person's location, history included, with no switch of its own.
  Rationale: the maintainer's answer, and the project's rule that the agent gets no more and no less than the person. Unlike health data in the iPhone plan, nothing here is held back from the dream: the routine is the point.
  Date/Author: 2026-10-17, maintainer.

- Decision: everything is kept until the person deletes it: every point, visit and place. The person can delete a day, a range or everything, on every surface.
  Rationale: the maintainer's answer ("as long as storage allows"). A day of points from a phone is a few hundred rows at most, because the phone sends a point only when it has moved (see Milestone 2).
  Date/Author: 2026-10-17, maintainer.

- Decision: no rule for children. Each person decides what they share; there is no guardian who sees a child's location without the child sharing it.
  Rationale: the maintainer's answer ("yes for now, no need to complicate"). It matches the circles decision that a circle has admins and members and nothing else.
  Date/Author: 2026-10-17, maintainer.

- Decision: a place may carry a **label** from a fixed list, `home`, `work`, `school`, `family` (somebody's home the person visits) and `gym`, besides its name, and more than one place may have the same label (two offices, two children's schools). The label is shown translated ("home", "自宅", "家"), as an icon on the map, and is what the agent, the routine and the weekly look around read for "home" and "work"; the name stays the person's or OpenStreetMap's. A place with a label and no name of the person's is shown by its label.
  Rationale: the maintainer asked to label places home, work, school and so on. A label is a vocabulary the program can act on (an icon, "left work", the town for the look around, "at work 09:00 to 17:30" in the routine) and a name cannot; keeping both lets a school keep its own name and be a school at once. The list is short on purpose and grows by adding a value to the check and the translations.
  Date/Author: 2026-10-17, maintainer and design.

- Decision: the people a location is shared with get a phone notification when the sharer arrives at or leaves a place the sharer named or labeled, under a new notification kind `location` the receiver can switch off per phone. Places named only by OpenStreetMap do not notify.
  Rationale: the maintainer asked for notifications. A notification for every coffee shop is noise; "arrived home", "left school" is what families use, and the places a person bothered to name or label are the ones that matter to them.
  Date/Author: 2026-10-17, maintainer and design.

- Decision: built on the circles branch (`design/circles-and-chats`, PR #528) as branch `location`, one pull request stacked on #528, migrations from 0240.
  Rationale: the maintainer's answer. Sharing needs circles. Circles uses migrations 0191 to 0198 and photos 0220 to 0224; 0240 leaves room for both to add more.
  Date/Author: 2026-10-17, maintainer and design.

- Decision: the words. **Location** is where a person is, as coordinates over time; a **point** is one reported position; a **place** is a spot with a name and a radius, belonging to one person; a **visit** is one stay at a place, with an arrival and, once over, a departure; a **check-in** is a visit the person recorded by hand. A location is shared at a **location share level**: `current` (where they are now, and the place) or `history` (that, and their visits). The agent's tool stays `location`, the page and the app screen are "Location", the memory folder stays "Places".
  Rationale: one word per idea, reusing what exists: the `location` tool, the `places/` memory folder, `share_level` and `grantee_*` from the circles shares. `ShareLevel` is already the calendar's Go type, so the location one is `LocationShareLevel`.
  Date/Author: 2026-10-17, design.

- Decision: the dashboard draws maps with MapLibre GL JS and vector tiles from OpenFreeMap, fetched through the server (`/api/v1/map/...`) rather than by the browser, so the content security policy stays `'self'` and a viewer's browser never talks to a third party. The operator can point it at another style, such as their own Protomaps file. The iPhone uses Apple's MapKit. Milestone 0 proves it before anything depends on it.
  Rationale: the policy exists because the dashboard shows mail from strangers (see Surprises). Raster tiles from tile.openstreetmap.org are not meant for software that ships with them on by default; OpenFreeMap serves free vector tiles without a key for exactly this. MapKit on the phone is free and native.
  Date/Author: 2026-10-17, design (to confirm in Milestone 0).

- Decision: what the agent learns goes through memory as it does for recordings: a **location** knowledge source per person writes one document per finished day ("where they went on Saturday 11 October"), which the dream reads into facts on pages under Places; and the month digest the timeline is written from gets a section of places and hours. Questions about particular days are answered by the tool from the visits themselves, never from memory.
  Rationale: the dream already turns documents into pages with evidence, and the recordings feature set the pattern (`docs/decisions/20261012-recordings-are-passive-knowledge-sources.md`). A routine is a fact about many days; a day is a record. Keeping both answers exact.
  Date/Author: 2026-10-17, design.

- Decision: one word per idea, settled in review round 1: **visit** (never "stay"); **position** in everything a person reads and **point** in API and code identifiers, defined as one reported position; **erase** for deleting history (`EraseLocationHistory`, renamed from `ForgetLocationHistory`) and **forget** for deleting one place; `shareLevel` on every view; the two levels read "Where I am now" and "Where I am and where I have been".
  Rationale: the reviewers found "stay" in two hundred lines beside "visit", "point" and "position" mixed in what people read, and "forget" and "erase" for the same action. Renaming the API's point would touch every surface for a word only developers see; a person never reads "point".
  Date/Author: 2026-10-17, design.

- Decision: OpenStreetMap stays on by default, and the person is told where names come from where they set Location up and the operator beside the provider setting: the server sends the position of each new place to the place provider.
  Rationale: the maintainer asked for OpenStreetMap names; the security review pointed out that positions leave the server without notice. Telling, rather than switching it off, keeps the feature working on a new server.
  Date/Author: 2026-10-17, design.

- Decision: a new place is named right after the report that made it is committed, outside the agent; the agent's periodic job only retries places whose first try failed. Notifications go out only for an arrival or departure in the last fifteen minutes and after the share was made.
  Rationale: naming must work with the agent off (see Surprises); a phone that was offline for a day must not ring the family for every place it passed.
  Date/Author: 2026-10-17, design.

## Outcomes & Retrospective

Built in one pull request (#536) on every surface: the iPhone records visits, significant moves and the person's named places while closed and follows them while open; every visit is at a place named from OpenStreetMap that the person can rename and label; check-in and search find places by name or kind; a location is shared at current or history with a circle or a person in one, live on the map, with notifications for named and labeled places; the agent reads it all through the location tool, a day journal and a monthly digest; Location is set up from its first screen, the phone tour and the offer on joining a family circle. Deployed to production on 2026-10-17.

What changed from the plan, and why. Places are named right after a report, not by the agent's job alone, because a server with the agent off never named them. Visits are normalized after every report (ordered, open ones ended at the next arrival, same-place ones joined), because a phone delivers arrivals and departures out of order and offline; all 720 orders of a day's events now give one answer. A visit with nothing after it lasts while the person is still there, up to 72 hours after their last report, because the phone sends nothing while they stay put. Search asks again in OpenStreetMap's own word for a kind of place. The map goes through the server (`/api/v1/map`), and its element needed an explicit size because MapLibre's stylesheet overrides the page's.

What is left. A day on the maintainer's phone, to see how late iOS reports arrivals and departures and whether the recorder behaves across a real day. The App Store privacy answers need Precise Location added before a build with Location goes to review; that is the maintainer's to do.

Lessons. Each review round found real defects until the fifth; the reviewers' throwaway tests (`go test -overlay`) reproduced the subtle ones without touching the repository. A walkthrough in a real browser found what four reviews could not: a map with no height and a CSS rule the browser dropped.

## Context and Orientation

TeaNode is a Go program (`cmd/teanode-server`, the server, and `cmd/teanode`, the command line) with a React dashboard (`web/`) embedded in the server binary and a SwiftUI iPhone app (`ios/`). Read `AGENTS.md` and `CONTRIBUTING.md` first. This plan is built in a clone on branch `location`, made from `design/circles-and-chats`; everything below describes that branch.

**Accounts and permissions.** `internal/models/user.go` defines `User` (table `user`) with `ID` (a ULID string of 26 characters, stored as `character varying(32)`), `Username`, `Name`, `Timezone`. Permissions are listed in `internal/models/permission.go` (`calendar:use`, `circle:use`, ...) and seeded into roles by a migration, the way `0193_circle.sql` grants `circle:use` to the Administrator, Operator and Member roles with `INSERT ... SELECT "id", 'circle:use' FROM "role" WHERE lower("name") IN (...)`. Every GraphQL resolver must call one of the helpers in `internal/api/v1api/apigraph/authorize.go`; `TestEveryOperationAuthorizes` fails otherwise. A refusal answers "not found", never "forbidden".

**Circles and shares.** A circle (`circle`, `circle_member`, migration `0193_circle.sql`) is a set of people on the server: family, friends or colleagues, with admins and members. Every share in the circles feature has the same shape, and this plan copies it from calendars: `0195_calendar_share.sql` makes `calendar_share` with `grantee_user_id` or `grantee_circle_id` (exactly one, by `CHECK (num_nonnulls(...) = 1)`), a `share_level`, `created_at`, `created_by_user_id`, and one unique index per grantee column. Sharing with a person needs the sharer and that person to be in a circle together, and sharing with a circle needs the sharer to be a member; `endSharesOutsideCircles` in `internal/db/database_circle.go` runs inside the transaction that removes a member, lets one leave or deletes a circle, and deletes the direct shares between people who no longer share a circle (it calls `endCalendarSharesOutsideCircles`, `endPageSharesOutsideCircles`, `endSkillSharesOutsideCircles`). A circle's share reaches only current members because reads join `circle_member`. The target of a share ("with") is resolved by `models.ResolveShareTarget` (`internal/models/share_target.go`): an id first, then a username, then a circle name, and a circle name two circles have is refused with both ids. The agent's tools use `tools.ResolveGrantee` (`internal/agent/tools/grantee.go`). The decision record `docs/decisions/20261016-people-on-one-server-meet-in-circles.md` is the base rule.

**The agent's location today.** The `location` tool (`internal/agent/tools/location/location.go`) asks, through `tools.Locating` (`internal/agent/tools/locating.go`), the browser or app the turn came from for one position: `AskRun.Locate` in `internal/agent/locate.go` emits an `EventLocate` event, the dashboard or the app answers with the GraphQL mutation `AnswerAgentLocation`, and the run waits up to a minute. A surface says whether it can answer in `internal/agent/surface.go` (`canLocate`). On the iPhone, `LocationReader` in `ios/TeaNode/Sources/ConversationModel.swift` asks for When In Use permission and reads one fix. Nothing is stored. The weekly look around (`internal/agent/nearby.go`) finds the person's town from memory pages under `models.PathPlaces` (`internal/models/graph.go`, the folder "Places: Where things are.").

**Phones and notifications.** `agent_phone` rows (migration 0160, `internal/models/phone.go`) are a person's iPhone installs, keyed by their agent. `models.NotificationKinds` lists the kinds a phone may take (`message`, `approval`, `question`, `answer`, `circle`, `chat`, `skill_share_call`), and a phone chooses among them. `PhoneNotifier.NotifyPhones(agentId, notificationKind, content)` in `internal/agent/phone_notifier.go` sends one to every phone of an agent that wants that kind, sealed per phone so neither the relay nor Apple can read it; `internal/chat/notify.go` and `internal/agent/circle_invitation.go` are the two examples that notify somebody other than the agent's own person.

**Memory and the dream.** The graph (`docs/subsystems/memory.md`) is pages (`agent_node`) with numbered facts carrying evidence. The **dream** is the background run each night (`internal/agent/dream.go` lists its stages in order). It reads documents from knowledge sources (`models.AgentKnowledgeSource`, kinds such as `recording`, `chat`, `finance` in `internal/models/knowledge.go`; documents are `models.AgentDocument` of a `models.AgentDocumentKind` such as `journal`) and files facts on pages. `dreamTimeline` (`internal/agent/dream_timeline.go`) writes a page for each month from `Agent.Digest` (`internal/agent/digest.go`), which is arithmetic over the month's records with one model call to write it up. Recordings put their transcripts in as `journal` documents (`internal/db/database_recording_knowledge.go`, `PutAgentDocument`).

**Configuration.** Server settings live in the database and are edited on the dashboard's server settings (`internal/config`; `docs/reference/deployment.md`). The closest example of a provider setting is `config.AgentSearch{Kind, APIKey}` (`internal/config/agent.go`) behind the `web_search` tool, shown in `internal/api/v1api/apigraph/settings_agent.go`. A secret field is tagged `secret:"true"`.

**The surfaces of one capability.** A capability is one GraphQL operation (`internal/api/v1api/apigraph/`, grouped in a `...Query` and a `...Mutation` struct per area, such as `CalendarQuery`), called by the command line (`internal/client/`, `internal/cmd/`), by the agent's tool (`internal/agent/tools/<area>/`, as an action), by the dashboard (`web/src/pages/`, strings in `web/src/i18n/{en,zh,ja}.ts`) and by the iPhone app (`ios/TeaNode/Sources/`, operations in `ios/TeaNodeKit/Sources/TeaNodeKit/`, strings in `ios/TeaNode/Resources/Localizable.xcstrings` in English, Chinese and Japanese, demo data in `DemoServer.swift`, screenshots in `.github/workflows/ios.yml`). A parity test in `internal/cmd/` (for example `TestCalendarSharingParity` in `internal/cmd/calendar_share_test.go`) lists every operation of an area with its subcommand and tool action and fails when a new operation is missing from the table.

**Terms used below.** *Nominatim* is OpenStreetMap's search and naming server: given coordinates it answers the nearest named thing ("reverse"), and given words it finds places ("search"). *Photon* is another server over the same data, faster at search as you type, which an operator can run themselves. *MapLibre GL JS* is a browser library that draws maps from *vector tiles* (map data cut into squares, drawn by the browser) described by a *style* (a JSON file naming the tile, font and icon addresses and how to color them). *OpenFreeMap* is a free public server of such tiles and styles, made from OpenStreetMap. On the iPhone, *Core Location* is Apple's location framework; `CLLocationManager` is its older interface, *visit monitoring* (`startMonitoringVisits`) reports arrivals and departures, *significant change monitoring* reports moves of about 500 m, and `CLMonitor` reports entering and leaving up to twenty circles the app defines.

## Plan of Work

### Milestone 0: prototypes

Three questions decide the rest, and each is answered by a small program before it is built on.

First, OpenStreetMap from Go. Write `internal/place/osm.go` (package `place`, which arrives with the photos branch; if this branch does not have it yet, take `internal/place` from `photos` as a first commit, since the photos pull request adds the same files and the merge will see them as identical) with a `Provider` interface and a Nominatim implementation, and a test program under the scratchpad that reverse-names five invented coordinates (a park, a school, a shop, a house in a street, a spot in a field) and searches "playground" near one of them within 5 km. Record in Surprises what comes back: the `name`, `category`/`type`, `display_name` and `osm_type`/`osm_id` fields, how a house comes back (a street address, no name), and the latency. Keep to the usage policy while testing: one request a second, a User-Agent of `TeaNode/<version> (+https://<server host>)`.

Second, the map on the dashboard. Add `maplibre-gl` to `web/package.json`, and a server endpoint `GET /api/v1/map/{path...}` that fetches from the configured style host (default `https://tiles.openfreemap.org`), rewrites the style's tile, glyph and sprite addresses to go through the same endpoint, and passes on the caching headers. Configure MapLibre to load its worker from a file the server serves (`maplibregl.setWorkerUrl`, the CSP build), so no `blob:` worker is needed. Draw a map with three markers on a test page and check in the browser's console that nothing breaks the content security policy and nothing is fetched from another origin. If OpenFreeMap's terms or behavior rule it out, fall back to raster tiles through the same proxy and record why.

Third, visits on a real iPhone. This needs the maintainer's phone, so it rides with Milestone 2's first TestFlight build rather than blocking the server work: a debug screen in the app lists every visit, significant change and region event with the time it arrived, and the maintainer carries the phone for a day. Record in Surprises how late arrivals and departures came compared with the truth.

### Milestone 1: storage, reporting and naming on the server

Add migrations `internal/db/migrations/0240_location.sql` and its `.reverse.sql`, with a `reverse_*_test.go` like every migration, creating these tables (all ids `character varying(32)`, all times `timestamp with time zone`):

`location_point` is one position a phone or browser reported: `id`, `user_id` (references `user`, on delete cascade), `latitude_degrees` and `longitude_degrees` (`double precision`), `accuracy_meters` (`double precision`), `altitude_meters`, `speed_meters_per_second` and `course_degrees` (nullable `double precision`), `measured_at`, `received_at`, `location_source` (`'phone'` or `'browser'`), `agent_phone_id` (nullable, references `agent_phone`, on delete set null). Unique on `(user_id, measured_at, location_source)`, so a phone that sends a batch twice adds nothing; indexed on `(user_id, measured_at DESC)`.

`place` is a spot belonging to one person: `id`, `user_id`, `place_name` (what the person sees; their own name if they gave one, otherwise the OpenStreetMap name, otherwise the street address, otherwise the city), `is_named_by_person` (boolean), `place_label` (`'home'`, `'work'`, `'school'`, `'family'`, `'gym'` or empty, checked by the table), `latitude_degrees`, `longitude_degrees`, `radius_meters` (default 75), `osm_type` (`'node'`, `'way'`, `'relation'` or empty), `osm_id` (bigint, nullable), `osm_name`, `osm_category` (such as `amenity=cafe`), `address_text`, `created_at`, `updated_at`. Indexed on `user_id` and on `(user_id, place_label)` where the label is not empty. Each person has their own places, even for the same park, because what one person calls a place is theirs.

`place_visit` is one stay: `id`, `user_id`, `place_id` (references `place`, on delete cascade), `arrived_at` (nullable: iOS may report a departure whose arrival it did not see), `departed_at` (nullable while the person is still there), `latitude_degrees`, `longitude_degrees`, `accuracy_meters`, `visit_source` (`'visit'` from iOS visit monitoring, `'region'` from entering a named place, `'check_in'` by hand, `'browser'`), `client_visit_key` (what the phone calls it, so a report sent twice is one visit; unique with `user_id`), `created_at`, `updated_at`. Indexed on `(user_id, arrived_at DESC)` and on `place_id`.

`location_share` copies `calendar_share`: `id`, `user_id` (the sharer), `grantee_user_id` or `grantee_circle_id` (exactly one), `share_level` (`'current'` or `'history'`), `created_at`, `created_by_user_id`; unique per sharer and grantee column, partial indexes on each grantee column. The table is created here and used from Milestone 4.

The migration also grants a new permission `location:use` ("record where you go, and share it") to the Administrator, Operator and Member roles, added to `internal/models/permission.go` beside `circle:use`.

Models go in `internal/models/location.go`: `LocationPoint`, `Place`, `PlaceVisit`, `LocationShare`, the enums `LocationSource`, `VisitSource`, `PlaceLabel` (`PlaceLabelHome`, `PlaceLabelWork`, `PlaceLabelSchool`, `PlaceLabelFamily`, `PlaceLabelGym`, and `PlaceLabels` listing them in the order a picker offers them) and `LocationShareLevel` with their constants (`LocationShareLevelCurrent`, `LocationShareLevelHistory`) and `IsGrantable`. Database methods go in `internal/db/database_location.go`, following `internal/db/database_calendar_share.go` for shape and tests (`database_location_test.go` against the test database).

Naming is `internal/place`: a `Provider` interface,

    type Provider interface {
        // Name answers the named thing at a point, or the address there.
        Name(ctx context.Context, latitudeDegrees, longitudeDegrees float64) (*Found, error)
        // Search finds places matching words near a point, nearest first.
        Search(ctx context.Context, query string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*Found, error)
    }

with `Found` carrying the OpenStreetMap fields above, implementations `nominatimProvider` and `photonProvider`, and one process-wide limiter of one request a second shared by both. A new top-level configuration section `Places` (`yaml:"places"`, in `internal/config/places.go`): `ProviderKind` (`"nominatim"`, `"photon"` or `""` for off; default `"nominatim"`), `ProviderURL` (default `https://nominatim.openstreetmap.org`), `MapStyleURL` (default the OpenFreeMap style found in Milestone 0). It is validated in `internal/config/validate.go` and shown on the dashboard's server settings beside the web search provider, with its help text saying that names come from OpenStreetMap and what the public server allows.

Reporting is one GraphQL mutation, `ReportLocation`, in `internal/api/v1api/apigraph/location.go` (`LocationQuery` and `LocationMutation`). It takes up to 500 points and up to 50 visits, each visit carrying `clientVisitKey`, coordinates, accuracy, `arrivedAt`, `departedAt` and `visitSource`. It requires `location:use`, refuses coordinates that are not on the earth (`isOnEarth`, already in `internal/agent`, moves to `internal/place` and is used by both), refuses times more than a day in the future, and answers how many points and visits were new. A visit is matched to a place in `internal/place/visit.go`: the person's place whose radius holds it, nearest first; otherwise a new place named by the provider (a provider error leaves it named after its city by `place.Nearest`, and the job below names it later). A visit whose `clientVisitKey` exists updates that visit (an arrival is followed by a departure for the same visit). Two visits at one place less than ten minutes apart are joined into one, since iOS sometimes splits a stay.

Naming waits for nobody: `ReportLocation` answers before the provider is asked, and a periodic job (`internal/agent/place_job.go`, scheduled the way `internal/agent/photo_job.go` on the photos branch is) names places that have only a city, at most one a second, and records the error on the row (`naming_error_message`, `named_at`) so a failing provider is visible on the Location page rather than retried forever in silence.

The browser's answers to the `location` tool are stored as points too (`location_source = 'browser'`), in `AnswerAgentLocation`'s resolver, so the dashboard counts.

The other operations of this milestone, each with its command line subcommand and tool action:

`GetLocation(userId?)` answers where the person (or, from Milestone 4, somebody sharing with them) is: the latest point, its age, and the visit in progress with its place. `ListLocationVisits(userId?, from, until, placeId?, offset, limitCount)` pages visits newest first with `nextOffset`. `ListPlaces(userId?, query?, offset, limitCount)` pages the person's places with the number of visits and the last visit. `SavePlace(placeId?, placeName?, placeLabel?, latitudeDegrees?, longitudeDegrees?, radiusMeters?)` names, renames or labels a place (a name sets `is_named_by_person`; an empty label takes the label off) or makes a new one at coordinates. `ListPlaces` and `ListLocationVisits` take `placeLabel` to filter, so "how long was I at work this week" is one call. `ForgetPlace(placeId)` deletes a place and its visits. `CheckIn(placeId? | osmType+osmId+coordinates)` records a visit starting now at a place, making the place from the provider's answer when it is new, and ends the visit in progress if there is one. `ListLocationPoints(userId?, from, until)` answers a day's points (at most two days, at most 5000), for drawing the path; the agent reads visits instead, so it has no tool action. `ListNearbyPlaces(latitudeDegrees, longitudeDegrees, radiusMeters)` lists named things near a point for the check-in list (the provider's search with an empty query falls back to reverse naming of the point; record what works in Milestone 0). `SearchPlaces(query, near, radiusMeters, offset, limitCount)` is the provider's search near a point (`near` is coordinates, a place id, or empty for where the person is). `EraseLocationHistory(from?, until?)` deletes points and visits in a range, or all of them, and places left with no visit that the person did not name.

The command line is `teanode location` in `internal/cmd/location.go` with subcommands `now`, `visits`, `places`, `path`, `save` (with `--name`, `--label` and `--radius`), `forget`, `checkin`, `nearby`, `search`, `erase` (for `EraseLocationHistory`), and from Milestone 4 `people`, `shares`, `share`, `unshare`. The client calls are in `internal/client/location.go`. `TestLocationParity` in `internal/cmd/location_test.go` lists every operation of `LocationQuery` and `LocationMutation` with its subcommand and tool action, the way `TestCalendarSharingParity` does; `ReportLocation` is listed with no subcommand and no action, because only a phone reports, and the table says so.

The agent's tool stays `location` and gains actions, in `internal/agent/tools/location/`. `now` is today's behavior when the turn came from a browser or the app (it asks, and stores the answer), and otherwise answers the latest point and the visit in progress, saying how old it is. `visits`, `places`, `save_place`, `forget_place`, `check_in`, `nearby`, `search` and `erase` call the operations above through the tool's GraphQL operations like the calendar tool does. `BrowserOnly` goes, because history and search work anywhere; `Locate` keeps refusing on surfaces with nobody to ask, and `now` then falls back to what is stored. Risk: reading actions are `read`; `save_place` and `check_in` change only the person's own records and are `write`; `forget_place` and `erase` are `destructive` and wait on a card. Long results page with `offset` and `next_offset` like every list.

### Milestone 2: the iPhone records, and shows the person their own location

Add `INFOPLIST_KEY_NSLocationAlwaysAndWhenInUseUsageDescription` to `ios/project.yml` ("TeaNode records the places you go, so you can look back, share where you are with your family, and so your agent knows your routine.") and rewrite the When In Use text to cover live following while the app is open. Do not add `location` to `UIBackgroundModes`.

`ios/TeaNode/Sources/LocationRecorder.swift` owns one `CLLocationManager`, made at launch in `AppDelegate.application(_:didFinishLaunchingWithOptions:)` whether or not the launch came from a location event (iOS relaunches a closed app with the `location` launch option and delivers the events only to a manager made then). While recording is on and Always is granted it: starts visit monitoring and significant change monitoring; holds a `CLServiceSession(authorization: .always)` made when the app was last in the foreground and made again at each launch; and, through `CLMonitor`, watches the person's named places (the twenty nearest, refreshed when significant change reports a move). While the app is in the foreground it also reads `CLLocationUpdate.liveUpdates()` and keeps a point when the person moved at least 25 m or 60 seconds passed while moving, and none while still. Every event becomes a point or a visit in an outbox, a file in the app group so it survives the app being killed, and the outbox is sent with `ReportLocation` right away, inside `beginBackgroundTask` when in the background, and again at every launch until the server has it; a batch is deleted only after the server answered. A region exit or a visit departure with no arrival is sent as it is; the server joins it to the open visit.

Permission is asked in two steps, the way iOS requires: When In Use the first time the person opens Location or turns recording on, then Always from a screen that says why. The app works without Always: Location shows the map and live following while open, and says once, plainly, what is lost and how to allow it in Settings. Recording is on by default once Always is allowed, and the switch is the first row of Location's settings ("Record where I go"), with "Erase history" below it.

The Location screen (`LocationViews.swift`, `LocationModel.swift`, a new `AppSection.location` in `AppShell.swift` with the `location` symbol, after Calendar) follows `ios/DESIGN.md` (load the `ios-design` skill): a MapKit map with the person and, from Milestone 4, the people sharing with them; under it the visit in progress; tabs or a segmented control for Visits (by day, newest first, each day a list of places with arrival, departure and duration, and the day's path on the map when a day is opened), Places (labeled and named first, then the rest, each with its count and last visit; tap to rename, pick a label from the list, change the radius on a small map, or forget; a labeled place has the label's symbol, `house`, `briefcase`, `graduationcap`, `figure.2.and.child.holdinghands`, `dumbbell`, on the map and in lists) and, from Milestone 4, Sharing. A Check in button lists `ListNearbyPlaces` and records the one tapped. The footer of every list that shows names says "© OpenStreetMap contributors". The operations go in `ios/TeaNodeKit/Sources/TeaNodeKit/Location.swift`; `TestTheAppDocumentsMatchTheSchema` checks them against the server. Demo data for the screenshots goes in `DemoServer.swift` with invented places only, and shots of the map, a day of visits and the places list are added to `.github/workflows/ios.yml`. Strings in English, Chinese and Japanese.

### Milestone 3: the dashboard's Location page

`web/src/pages/location.tsx` at `/location`, linked from the sidebar beside Calendar and in the breadcrumb table (`web/src/components/breadcrumb.tsx`), lazy loaded like the other pages in `web/src/app.tsx`. It has the same parts as the app: a map (`web/src/components/locationMap.tsx`, MapLibre from Milestone 0) with the person and the people sharing with them; Visits by day with pages, not load more, and the day in the URL (`?day=2026-10-11`); Places, with rename, a label picker, radius and forget, and the label's icon on the map and in lists; and Sharing from Milestone 4. A check-in from the dashboard uses the browser's position (`web/src/components/browserLocation.ts`). Errors and successes are toasts. Strings in `web/src/i18n/{en,zh,ja}.ts`. Follow `docs/coding/frontend-design.md`; the page is walked at desktop width and at 390 pixels, light and dark.

### Milestone 4: sharing with circles, the people on the map, and notifications

`ShareLocation(with, shareLevel)`, `UnshareLocation(shareId)` and `ListLocationShares()` copy the calendar's (`internal/api/v1api/apigraph/calendar_share.go`, `internal/db/database_calendar_share.go`, `internal/agent/tools/calendar/sharing.go`, `internal/cmd/calendar_share.go`), with `with` resolved by `ResolveShareTarget` and `tools.ResolveGrantee`. Sharing is `granting` risk for the agent, so it waits on a card. Add `endLocationSharesOutsideCircles` to `endSharesOutsideCircles` in `internal/db/database_circle.go`, with a test that a person who leaves the circle stops seeing the sharer at once.

`ListSharedLocations()` answers every person sharing with the caller, at the highest level any of their shares gives (a person reached both directly and through a circle gets the higher), with their name, avatar, latest point and visit in progress. `GetLocation`, `ListLocationVisits` and `ListPlaces` take `userId` and answer for a sharer only at the level shared: `current` allows `GetLocation` and the place of the visit in progress, `history` allows visits and places too; anything else answers not found. A place's name is shown to a grantee as the sharer named it.

Live updates: `ReportLocation` sends `NOTIFY` on a new channel `location_event` (named in `internal/db/database.go` beside the others) with only the user id, and a GraphQL subscription `LocationEvents` tells each subscribed viewer which person moved, if that person shares with them; the dashboard and the app then call `GetLocation` again. The map on both surfaces shows the people sharing with the viewer with their avatars, and updates while open.

Notifications: add `NotificationLocation = "location"` to `internal/models/phone.go` and to `NotificationKinds`, with its words on every surface that lists the kinds. When a visit at a place with `is_named_by_person` or a `place_label` starts (an arrival) or ends (a departure), `internal/agent/location_notify.go` notifies the phones of every person the location is shared with at any level, through `PhoneNotifier.NotifyPhones` on each receiver's agent, as "<name> arrived <place>" or "<name> left <place>", where a labeled place the person did not name is called by its label in the receiver's language ("arrived home"). At most one notification per sharer, place and receiver in ten minutes, so a region that flickers does not ring twice. The notification opens Location with that person selected.

### Milestone 5: the agent

The agent's tool gets `people`, `shares`, `share` and `unshare`, and `now`, `visits` and `places` take `person` (a username, resolved through `ResolveGrantee` among people sharing with the caller). The tool's description tells the model what each action is for, including that a weekend plan should look at last weekend's visits and search for places not in them; the system prompt does not change.

A knowledge source of kind `location` (`models.SourceLocation`, added beside `SourceRecording` in `internal/models/knowledge.go`) is made for a person the first time they report, switched on, described as "where you went, one day at a time". After a day ends in the person's time zone, `internal/db/database_location_knowledge.go` writes one `journal` document for it (external id `location:<YYYY-MM-DD>`, title "Where they went on <day>", happened at the day's start, URL `/location?day=<YYYY-MM-DD>`, private), listing each visit with place, arrival, departure and duration, in the shape recordings use (`database_recording_knowledge.go`). A day whose visits change later (a departure arriving late, a place renamed) is written again with a new hash, which the reading already handles.

`Agent.Digest` (`internal/agent/digest.go`) gets a section "## places they went", arithmetic over the month's visits: each place with the number of visits, hours spent, the days of the week and the usual hours, as ranges so the facts do not churn. Places with a label are read first: the day document says "at home", "at work"; the digest groups by label before place; `nearby.go` takes the town of the person's place labeled `home`, if there is one, before guessing it from memory.

### Milestone 5b: getting started

The maintainer asked that Location be easy to set up as part of getting started. TeaNode has no checklist per person; a person meets a feature in four places, and Location is added to each, the same way on every surface.

The first time a person opens Location (on the iPhone, or on the dashboard with no point yet), the screen is a short setup in place of the empty map, three rows that each finish in one tap and show as done when done: allow location (on the iPhone: When In Use, then Always, as in Milestone 2; on the dashboard: "Install the TeaNode app on your iPhone", linking to `/settings/devices`, because only the phone records in the background), "This is home" (labels the place where the person is now `home`, from the phone's or the browser's position, with `SavePlace`), and, when the person is in a family circle, "Share with <circle>" (`ShareLocation` at `history`, with "Where I am now" one tap away). Each row can be skipped; skipping all three, or finishing, shows the ordinary screen, and the setup does not come back. Whether it is finished is worked out from the server (`GetLocation` has a point, a `home` place exists, a share exists), not stored, the way the server setup steps work out theirs (`web/src/pages/serviceSetup.tsx`).

The agent's phone tour (`internal/agent/speak_first_phone.go`, `phoneTourFacts` and `phoneTourInstructions`) gains an offer, when the person has `location:use`: what Location does in one sentence, and opening it (`show` of `/location`, which the iPhone's `AppModel.show(path:)` learns to open) to set it up; `TestANewPhoneIsShownAroundOnce` checks it is offered.

Joining a circle: the dashboard (`web/src/pages/circles.tsx`, `answer()`) and the iPhone (`CirclesView.answer`, beside `CircleCalendarOfferSheet`) already offer to share the calendar once, right after a person accepts an invitation to a family or colleagues circle. For a family circle the same moment offers location too, in the same sheet as a second choice ("Share where I am: now and where I have been / only where I am now / not now"), so the person answers once. The agent's `circle_answer` result and `teanode circle accept` say that the calendar and location can be shared with it, and how.

The location tool's description says, for a person with no point yet, to offer to set Location up rather than answer that nothing is known.

### Milestone 6: finish

Documentation: `docs/subsystems/location.md` (what is recorded, how a visit is made, naming, sharing, notifications, what the agent sees), the `location` commands in `docs/reference/command-line.md`, the `places` settings in `docs/reference/deployment.md`. Two decision records under `docs/decisions/`, dated 2026-10-17: `a-location-is-shared-at-a-level` and `places-are-named-from-openstreetmap`. A section of `docs/security/security-review.md` for this feature: who may read whose location, the refusals on every operation and the subscription, what a notification reveals, the map proxy (it must fetch only from the configured style's hosts, never an address a request names), and the provider requests (what coordinates leave the server, and to whom). Review the whole diff, fix, review again until a round finds nothing; check parity explicitly; run the `ux-review` skill on every screen; deploy and use it for real for a day with the maintainer's phone.

## Concrete Steps

Work in the clone, never in the main checkout, which another session uses:

    cd ~/.cache/claude-tmp/teanode-location
    git status            # branch location, made from origin/design/circles-and-chats

The server's tests need Docker for PostgreSQL:

    make test             # everything; needs Docker
    go test ./internal/db/ -run 'Location|Place' -count=1
    go test ./internal/cmd/ -run TestLocationParity -count=1
    make lint-ci          # what CI runs; stage new files first, check-secrets reads tracked files
    (cd web && npm ci && npm run lint && npm test)

After `make test`, put back the vendor files it rewrites before committing:

    git checkout -- vendor

Before opening and before merging, check that no other branch took migration 0240 or later:

    git fetch origin
    for branch in main design/circles-and-chats photos; do git ls-tree --name-only origin/$branch internal/db/migrations/ | grep -E '/02[4-9][0-9]_' ; done

Expected: nothing printed for any branch but this one.

## Validation and Acceptance

Milestone 1, from the command line against a development server (`docs/reference/local-development.md`), with `TEANODE_PROFILE=local` so nothing reaches production:

    teanode location checkin --latitude 40.0005 --longitude -75.0003
    teanode location now
    teanode location visits --from 2026-10-17
    teanode location save <placeId> --label home
    teanode location search playground --radius 5000

`now` shows the check-in's place and how long ago it started; `visits` lists it; after `save` the place shows as home with the house icon everywhere, and `teanode location places --label home` lists it; `search` lists playgrounds with their distance. Sending the same `ReportLocation` twice answers zero new points and visits the second time (a test does this). A test reports an arrival and then a departure with one `clientVisitKey` and finds one visit with both times.

Milestone 2, on the maintainer's phone from TestFlight: with the app closed, a drive to a shop and back produces two visits on the server within minutes of each event, at named places; opening the app shows the live dot moving.

Milestone 4: with two accounts in one circle, the second sees nothing of the first until the first shares; at `current` the second sees where the first is but `teanode location visits --person <first>` answers not found; at `history` it lists the visits; when the first leaves the circle, the second sees nothing again at once. Arriving at a place the first named notifies the second's phone once.

Milestone 5: in a conversation, "where was I last Saturday?" makes the agent call `location` with `visits` for that day and answer from it; "find a park we haven't been to for this weekend" calls `visits` and `search`. After a dream, a page under Places carries facts about the person's routine with the day documents as evidence.

## Idempotence and Recovery

Every report is safe to send again (unique points, visit keys). The naming job may run on any instance and claims a place before naming it. The migration has a reverse migration and its test. Production gets a backup before the migration is applied (`teanode-ship`). Erasing history is the person's own action and cannot be undone, which the card and the confirmation say.

## Artifacts and Notes

The milestones' evidence (the Nominatim answers, the CSP check, the phone's visit timings) is added here as it is gathered.

## Interfaces and Dependencies

New dependencies: `maplibre-gl` in `web/package.json` (the dashboard's map); no Go module (the providers are plain HTTP with `net/http` and `encoding/json`); Apple's MapKit and Core Location on the iPhone, already part of iOS. External services, all optional by configuration: the configured place provider (default the public Nominatim server) and the configured map style (default OpenFreeMap), both reached only from the server.

At the end of Milestone 1 these exist: `place.Provider`, `place.Found`, `place.NewProvider(configuration config.Places) (place.Provider, error)` (nil provider when `ProviderKind` is empty); `models.LocationPoint`, `models.Place`, `models.PlaceVisit`, `models.LocationShare`, `models.LocationShareLevel`; `apigraph.LocationQuery` and `apigraph.LocationMutation` with the operations named in Milestone 1; `cmd.NewLocationCommand()`; the `location` tool with the actions named above.

Revision note (2026-10-17): first version, written after the research and the maintainer's answers to the seven open questions.

Revision note (2026-10-17): progress through five review rounds, the walkthrough, the deploy, and the outcomes.

Revision note (2026-10-17): Milestone 5b, getting started, because the maintainer asked that Location be easy to set up as part of getting started; `ListLocationPoints` added for drawing a day's path; `NearbyPlaces` is `ListNearbyPlaces`, the list operations' form.

Revision note (2026-10-17): labels on places (`place_label`: home, work, school, family, gym), at the maintainer's request; they drive icons, notifications, the routine and the look around.
