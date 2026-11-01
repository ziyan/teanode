# The phone watches the stores a list needs

This ExecPlan is a living document. The sections Progress, Surprises &
Discoveries, Decision Log and Outcomes & Retrospective are kept up to date as
the work goes on.

## Purpose / Big Picture

A list's store reminder ("At Example Market: Groceries needs milk, eggs and 3
more") is sent when the server stores an arrival at a place that is that store.
Until now an arrival came only from iOS visit monitoring or from entering a
place the person named or labeled. A store the person never named, beside a
place they were already at (another shop in the same shopping center), gave
neither: iOS took the stop for part of the visit next door, and nothing told the
person what their list needed.

After this change the iPhone app also watches the stores the person's lists
need something from, nearest first, as Apple's Reminders watches a location.
Walking into one is a visit at that store: the reminder comes, the stop is in
the location history, and the visit next door ends where it really did.

To see it: with an item needed at a store that is reminded, run
`teanode shopping watched-stores --latitude <lat> --longitude <lon>` near one of
its branches; the branch is listed with its circle. On the phone, walk into it
and stay three minutes: the reminder arrives and Location shows the visit.

## Progress

- [x] (2026-10-29) Plan written.
- [x] (2026-10-29) Server: `ListWatchedStores`, a reported visit naming an OpenStreetMap object, tests.
- [x] (2026-10-29) Command line `teanode shopping watched-stores`, and the shopping tool's `watched_stores` (the parity test asks for both).
- [x] (2026-10-29) iPhone: the chooser shares the twenty circles with the stores, the outbox reports a store's visit with its object, the places watched are kept on the phone for a launch for an event; TeaNodeKit's 1656 tests pass on Linux.
- [ ] Docs, review, deploy, TestFlight from a branch with every open pull request.

## Surprises & Discoveries

- Observation: the monitor's own record of a circle says only its center and radius. iOS launches a closed app for an event before the places are read again, so a branch would be reported with no name and its arrival would not be the store's.
  Evidence: `placeEventArrived` in `LocationRecorder.swift` rebuilt the place from the record. The places given to the monitor are now kept in the app's defaults (`locationWatchedPlaces`), and a branch known only from the record is told by the map object in its identifier.
- Observation: a branch inside a larger place of the person's (a shopping center they labeled) would have been a visit there, which reminds of nothing.
  Evidence: `placeForReportedVisit` took the place holding the point first. A branch now takes it only when that place is the same store (`isPlaceOfStore`).
- Observation: a permission on one action of a merged tool becomes the whole tool's, so `watched_stores` needing Location would have hidden shopping from people without it.
  Evidence: `TestTheRiskOfEachAction`. The action needs shopping only; the operation refuses a person without Location.

## Decision Log

- Decision: the phone watches stores; the server does not guess arrivals from positions.
  Rationale: the person chose it over reading positions on the server, since iOS tells of a watched circle with the app closed, while positions come only when the phone happens to send them.
  Date/Author: 2026-10-29, the person.
- Decision: entering a store's circle is an ordinary region visit, carrying the store's OpenStreetMap object instead of a place id when the person has no place there. The server finds their place for that object, or makes one named as the map names it, and the visit goes through the same path as any other.
  Rationale: one way in for every arrival: the reminder, the pass-by wait of three minutes (so driving past a store sends nothing), the notices to the people the location is shared with, the history and the visit next door ending all follow without new code. A separate "arrived at a store" call would remind but leave the history wrong.
  Date/Author: 2026-10-29, agent.
- Decision: at most six stores are watched, at most two branches of each, within 25 km, each on a circle of 100 m; the person's named and labeled places keep the other fourteen of iOS's twenty.
  Rationale: a store is watched only while a list needs something there, so few are wanted at once, and the person's own places matter more. 100 m is the least a circle is reliably noticed at, as for named places.
  Date/Author: 2026-10-29, agent.
- Decision: the branches found for a store near a position are kept in memory for twelve hours, shared by every account, by store and by a cell of a tenth of a degree.
  Rationale: the phone asks every minute it moves and at every significant change; a store's branches are the map's, not anybody's, and the place provider is a shared, limited service.
  Date/Author: 2026-10-29, agent.
- Decision: items for any store are not watched for.
  Rationale: they are reminded at any grocery shop, which would be every supermarket around; they still come with a store's reminder there.
  Date/Author: 2026-10-29, agent.

## Outcomes & Retrospective

(To be written at the end.)

## Context and Orientation

- `internal/agent/shopping_notify.go`: `NotifyShoppingArrival` sends the reminder for a visit's place, by the store its name or map name is.
- `internal/api/v1api/apigraph/location_history.go`: `ReportLocation` stores reported visits (`putReportedVisit`, `placeForReportedVisit`); `afterLocationChange` tells the notices, holding a region visit's arrival for `db.PassByDurationMost` (`visitNoticeDueAt`). `placeFromFound` makes a place from a found OpenStreetMap object for a check-in.
- `internal/place`: `Provider.Search` finds places by words near a position.
- `ios/TeaNodeKit/Sources/TeaNodeKit/LocationOutbox.swift`: `WatchedPlace`, `LocationPlaceWatch.choose`, `LocationOutbox.arrive` and `depart`.
- `ios/TeaNode/Sources/LocationRecorder.swift`: `refreshPlaceWatch` reads the places and gives the chosen ones to the `CLMonitor` named "places"; `placeEventArrived` turns an event into an arrival or a departure.

## Plan of Work

Server. A query `ListWatchedStores(latitudeDegrees, longitudeDegrees)`, needing
shopping and location: the reader's stores with reminders on and items needed
on lists they did not mute, most needed first; for each, the provider's places
matching the store's name within 25 km whose own name is the same store
(`models.ShoppingStoreKey`), nearest first, two each, six in all; each with the
reader's place for that object when they have one. It is one of
`isModelBackedQuery`, since it waits on the provider. `ReportedVisit` gains
`osmType`, `osmId`, `osmName` and `osmCategory`; `placeForReportedVisit` takes,
for a region visit with no place id, the reader's place for that object, else
the place holding the point, else a new one named from the object.

Command line. `teanode shopping watched-stores` prints what the phone would
watch, around a position or where the person was last reported.

iPhone. `WatchedPlace` gains an identifier (the place id, or `store:` and the
object for a store with no place) and the object. `LocationPlaceWatch.choose`
takes the stores too: the person's places nearest first, up to twenty less the
stores, then the stores, leaving out one whose place is already watched.
`LocationOutbox.arrive` and `depart` key by identifier and report the object.
`refreshPlaceWatch` reads the stores beside the places; a failure keeps the
stores already watched.

## Validation and Acceptance

`go test ./internal/api/v1api/apigraph -run 'WatchedStores|StoreVisit'` with a
fake provider: a store needed near the position is listed, a muted store and a
muted list are not, a branch of another store with a like name is not; a region
visit naming an object makes one place for it, a second finds it, and its
arrival is the reminder. TeaNodeKit's tests on gen7 (`swift:6.1`) cover the
chooser and the outbox. On the server, `teanode shopping watched-stores` lists
the store near a known position.

## Idempotence and Recovery

No migration. The query is a read; a visit naming an object is stored under its
client key as any other, so a batch sent again finds what the first made.

## Interfaces and Dependencies

`WatchedStoreView` in `apigraph`: `storeName`, `storeKey`, `placeName`,
`osmType`, `osmId`, `osmCategory`, `latitudeDegrees`, `longitudeDegrees`,
`radiusMeters`, `distanceMeters`, `placeId`. `client.ListWatchedStores`.
TeaNodeKit `WatchedStore` and `TeaNodeConnection.watchedStores`.
