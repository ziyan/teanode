# How a person traveled: trips, ways of travel and distance

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows `~/.claude/PLAN.md`.

## Purpose / Big Picture

TeaNode already records where a person goes: positions from their iPhone (`location_point`), visits to places (`place_visit`), and named places (`place`). What it does not say is how they got from one place to the next. After this change, every stretch of moving between two visits is a trip, and each trip is made of one or more legs, each with a way of travel: walk, run, bike, car, bus, train, boat or plane. The way is worked out automatically, from the phone's motion activity when there is some and from speed and distance when there is not, and the person can correct any leg. The person (and the people they share their history with) can then see, on the dashboard, the iPhone, the command line and through the agent, how far they traveled by each way, per day, week, month and year.

To see it working: report a day of positions, visits and motion activity for a person, then run `teanode location trips` and see "08:05 to 08:40, 12.4 km: walk 0.6 km, car 11.8 km", run `teanode location travel --by week` and see the distance per way of travel per week, correct a leg with `teanode location travel-mode <leg id> bus`, and see it counted as bus from then on, including after the trip is rebuilt.

## Progress

- [x] (2026-10-26) Decisions taken with the person (see Decision Log).
- [x] (2026-10-26) Migration `0263_location_travel.sql` and its reverse, models, database operations, tests.
- [x] (2026-10-26) `internal/travel`: building trips and legs from visits, positions and motion activity; tests.
- [x] (2026-10-26) `ReportLocation` takes motion activity; trips rebuilt after each report and check-in; erase removes them; the backfill at start builds them for past days.
- [x] (2026-10-26) GraphQL: `ListTrips`, `SetTripLegTravelMode`, `ListTravelTotals`; tests including the app and dashboard documents against the schema.
- [x] (2026-10-26) Agent tool actions and CLI commands; parity tests.
- [x] (2026-10-26) Dashboard: trips in the day's list, correcting a leg, the Travel totals.
- [x] (2026-10-26) iPhone: Motion & Fitness in Location setup, motion activity sent with reports, trips in the day's list, correcting a leg, the Travel totals; demo server and screenshots (CI not yet run).
- [ ] Docs: `docs/subsystems/location.md` (Travel), `docs/reference/command-line.md`.
- [ ] Review until a round finds nothing; parity check; deploy; TestFlight; tell the journal session how to read trips.

## Surprises & Discoveries

- Deciding who still needs past trips from the data alone does not work: a person's first visit always begins before their first trip, so they looked unfinished forever. A marker table (`trip_backfill`) replaced it.
- The iPhone app's Location lists are a segmented picker in the drawer, not side-menu rows, so Travel is a row at the top of the Visits list.
- `activities` on `ReportLocation` is optional, so that phones running an older app keep reporting.

## Decision Log

- Decision: the ways of travel are walk, run, bike, car, bus, train, boat and plane. Rationale: the person chose this list (2026-10-26). Walk, run, bike and car are detected from motion activity; plane is detected from distance and speed; bus, train and boat are never detected, because the phone reports them as driving or as nothing, so they come only from a correction. Date: 2026-10-26.
- Decision: the iPhone asks for Motion & Fitness permission, offered in Location setup and skippable. Rationale: the person chose it; iOS motion activity tells walking, running, cycling and driving apart reliably even when background positions are 500 meters apart. Without it the way is guessed from speed.
- Decision: past days get trips too, guessed from speed and distance, built once at start. Rationale: the person chose it, so that totals start full.
- Decision: trips are seen by whoever sees the person's history (share level `history`), like the day's path; only the person corrects. Rationale: the person chose it.
- Decision: `TripView` carries `fromPlace` and `toPlace`, read for a page in one query (`ListPlacesOfVisits`). Rationale: the iPhone and the command line name where a trip went.
- Decision: trips and legs are derived data, rebuilt whenever the visits, positions or motion activity of their days change; the person's corrections are kept apart, as spans of time with a way of travel (`travel_mode_correction`), and applied to whatever legs they cover. Rationale: visits arrive late and out of order, so trips must be rebuildable at any time without losing what the person said.

## Outcomes & Retrospective

Not yet.

## Context and Orientation

The server is Go. Location lives in these files:

- `internal/db/migrations/NNNN_name.sql` and `NNNN_name.reverse.sql`: schema changes, embedded and applied at start. Migrations through 0262 exist or are claimed by other work (0262 is the journal's `user.measurement_system`); this plan uses `0263_location_travel`. Each migration has a test in `internal/db/migrations/reverse_<name>_test.go` that inserts rows, checks constraints and runs the reverse file (see `reverse_location_pause_test.go`).
- `internal/models/location.go`: the model types (`LocationPoint`, `PlaceVisit`, `Place`, `LocationPause`, `LocationNotificationMute`).
- `internal/db/database_location*.go`: the database operations, declared on interfaces collected in `internal/db/db.go` (the `Transaction` interface lists `LocationPauseOperation`, `LocationNotificationMuteOperation` and so on).
- `internal/api/v1api/apigraph/location*.go`: the GraphQL API. Operations are methods on the `graph` type declared in an interface in `location_history.go`; arguments and answers are Go structs whose `json` tags are the GraphQL names; `graphapi:"nullable"` marks an optional argument. The schema scalars are `String` (ids included) and `DateTime`. `ReportLocation` (in `location_history.go`) is what the phone calls with `points` and `visits`; after storing visits it calls `tx.NormalizePlaceVisits` so visits that came out of order are in order, deletes pass-bys, and calls `afterLocationChange`. `reachLocation(ctx, principal, userId, level)` answers whose location a call is about and refuses a caller below the level; `models.LocationShareLevelHistory` is the level that sees the past. `requireLocationPerson` checks the `location:use` permission.
- `internal/place/distance.go`: `place.DistanceMeters(firstLatitude, firstLongitude, secondLatitude, secondLongitude)`.
- `internal/agent/tools/location/`: the agent's `location` tool; `actions.go` has one function per action and the GraphQL documents it sends.
- `internal/client/location_history.go` and `internal/cmd/location.go`: the command line `teanode location ...`; `internal/cmd/location_test.go` has a map from each GraphQL operation to the command that uses it, which a parity test checks.
- `internal/cmd/server/run.go` starts background work; `internal/cmd/server/visitrepair.go` is a once-at-start pass over location data that this plan's backfill copies.
- `web/src/pages/location.tsx` and `web/src/components/location*.ts(x)`: the dashboard's Location page; strings in `web/src/i18n/{en,zh,ja}.ts`.
- `ios/TeaNodeKit/Sources/TeaNodeKit/Location*.swift` (models, GraphQL documents, `LocationOutbox`, which queues what the phone noticed until the server has it) and `ios/TeaNode/Sources/Location*.swift` (the screens; `LocationRecorder.swift` is what listens to iOS). `ios/TeaNode/Sources/LocationDemo.swift` answers the demo server used for CI screenshots; `.github/workflows/ios.yml` lists the `shoot` lines. Strings are in `ios/TeaNode/Resources/Localizable.xcstrings` in English, Simplified Chinese and Japanese. Info.plist keys are set in `ios/project.yml`.

Terms used here: a visit is a stay at one place with an arrival and usually a departure. A trip is the travel between the departure of one visit and the arrival of the next. A leg is a part of a trip with one way of travel. Motion activity is what iOS's Core Motion says the person was doing from a moment on (stationary, walking, running, cycling, automotive or unknown, each with a confidence of low, medium or high); one activity lasts until the next one starts. A correction is the person saying that a span of time was traveled some way.

## Plan of Work

Data. Migration `0263_location_travel.sql` adds four tables, each with `user_id` referencing `"user"("id") ON DELETE CASCADE`:

- `motion_activity`: `id`, `user_id`, `started_at`, `activity_kind` (one of `stationary`, `walking`, `running`, `cycling`, `automotive`, `unknown`), `activity_confidence` (`low`, `medium`, `high`), `created_at`; unique on (`user_id`, `started_at`), so a report sent twice adds nothing; index on (`user_id`, `started_at`).
- `trip`: `id`, `user_id`, `started_at`, `ended_at` (after `started_at`), `from_visit_id` and `to_visit_id` referencing `place_visit(id) ON DELETE SET NULL`, `distance_meters`, `created_at`; index on (`user_id`, `started_at`).
- `trip_leg`: `id`, `trip_id` referencing `trip(id) ON DELETE CASCADE`, `user_id`, `started_at`, `ended_at`, `detected_travel_mode` (one of `walk`, `run`, `bike`, `car`, `bus`, `train`, `boat`, `plane`), `detection_source` (`motion` or `speed`), `distance_meters`; index on (`user_id`, `started_at`).
- `travel_mode_correction`: `id`, `user_id`, `started_at`, `ended_at` (after `started_at`), `travel_mode` (the eight), `created_at`, `updated_at`; index on (`user_id`, `started_at`).
- `trip_backfill`: `user_id` (primary key), `backfilled_at`: the people whose past days have had their trips built once.

A leg's way of travel is the correction that covers at least half of its time, if any, otherwise what was detected. Correcting a leg deletes the person's corrections that overlap the leg's span and adds one for exactly that span. Clearing it deletes those.

Building trips (`internal/travel`, pure functions with no database, so they are tested on their own). Input: the person's visits in a window in arrival order, their positions in it, their motion activity in it. For each pair of consecutive visits where the first has a departure and the second an arrival after it, the trip runs from that departure to that arrival. Its path is the first visit's coordinates, the positions measured inside the trip (dropping those with an accuracy worse than 500 meters when better ones exist, and any position that would need more than 300 meters a second from the one before), then the second visit's coordinates. A trip whose path is shorter than 150 meters is not a trip (the person moved within one place). Legs: the motion activity inside the trip is mapped to ways (walking to walk, running to run, cycling to bike, automotive to car; stationary and unknown belong to no way) and cut where the way changes; a leg shorter than one minute or 100 meters is merged into the neighbor it touches longest; the leg's distance is the path length inside its time, with the path interpolated at the cuts; `detection_source` is `motion`. When motion activity covers less than half of the trip's time, the trip is one leg guessed from its average speed (path length divided by duration): at most 2.2 meters a second is walk, at most 5 is bike, at most 75 is car, faster is plane; `detection_source` is `speed`. Run is never guessed from speed. Whatever the source, a leg of at least 100 kilometers at an average of at least 55 meters a second is plane, since iOS reports a plane as driving. Bus, train and boat are never detected.

When trips are built. After `ReportLocation` stores anything, the trips of the days it touched are rebuilt: the window starts a day before the earliest time in the report (positions, visits, activity) and ends two days after the latest; trips that start in the window are deleted and trips are built from the visits whose departure lies in it. `EraseLocationHistory` also erases the trips, legs, motion activity and corrections in its range. At start, a once-over (`internal/cmd/server/tripbackfill.go`) builds trips for each person with visits who is not in `trip_backfill`, back from their earliest trip (or now) in seven-day windows, skipping weeks with no visit, then marks them, so past days get trips and a later start finds nobody. Reading the database and replacing a window's trips is package `internal/travel/rebuild` (`rebuild.Around` for the days a report touched, `rebuild.Window` for the backfill), so that `internal/travel` stays free of the database. A check-in rebuilds the trips around it too.

API. `ReportLocation` gains `activities: [ReportedActivity]` (`startedAt`, `activityKind`, `activityConfidence`), at most 500 a report, refused like an invalid point when a kind or confidence is unknown or a time is more than a day ahead. New operations, at share level history for reading:

- `ListTrips(userId: String = null, from: DateTime!, until: DateTime!, offset: Int = null, limitCount: Int = null): TripPageView` with `trips` newest first and `nextOffset`. `TripView`: `id`, `startedAt`, `endedAt`, `durationSeconds`, `distanceMeters`, `fromPlace` and `toPlace` (the visits' places, as `PlaceView`, null when gone), `legs: [TripLegView]`. `TripLegView`: `id`, `startedAt`, `endedAt`, `durationSeconds`, `distanceMeters`, `travelMode` (as corrected), `detectedTravelMode`, `isTravelModeCorrected`, `detectionSource`.
- `SetTripLegTravelMode(tripLegId: String!, travelMode: String = null): TripLegView`, the caller's own legs only; null clears the correction.
- `ListTravelTotals(userId: String = null, period: String!, from: DateTime!, until: DateTime!): [TravelTotalView]`, `period` one of `day`, `week`, `month`, `year`, in the traveler's time zone (the zone they last used, `agent.Location(user)`), weeks starting on Monday; a leg counts in the period it started in. `TravelTotalView`: `periodStartDay` (a date as YYYY-MM-DD), `travelMode`, `distanceMeters`, `durationSeconds`, `legCount`. At most 400 periods in one call.

Agent tool (`location`): actions `trips` (person, day or from and until), `travel_totals` (person, period, from, until) and `travel_mode` (trip_leg_id, travel_mode, empty to clear; a write). CLI: `teanode location trips [--day] [--from] [--until] [--person]`, `teanode location travel [--by day|week|month|year] [--from] [--until] [--person]`, `teanode location travel-mode <leg id> <walk|run|bike|car|bus|train|boat|plane|detected>`.

Dashboard. In the day's list of visits, the trip between two visits shows as its own row: each leg's icon and way, its distance and time ("Walk 0.6 km · 8 min, Car 11.8 km · 18 min"). On the person's own trips each leg has a menu with the eight ways and "As detected" to clear. A Travel tab beside Visits, Places and Sharing shows the totals: a Day, Week, Month, Year choice, then a table of periods newest first with the distance for each way and the total. Distance in kilometers or miles as the dashboard already formats distances; once the journal's `user.measurement_system` (migration 0262, #666) is on main, both clients follow it.

iPhone. Location setup offers Motion & Fitness after location (skippable); `NSMotionUsageDescription` in `ios/project.yml`. `LocationRecorder` reads `CMMotionActivityManager.queryActivityStarting(from:to:to:)` from the last activity it sent (at most seven days back, which is what iOS keeps) whenever it sends a report, and queues the activities in the outbox (`LocationOutbox.activities`, keyed by start, at most 5000 kept). The day's list shows trips between visits as in the dashboard, and a leg's menu corrects it. A Travel row in the Location side menu opens the totals with a segmented Day, Week, Month, Year choice. Demo server: trips for the demo day and totals; screenshots `location-travel` and the existing `location-day`.

## Concrete Steps

From the repository root (a clone, not the main checkout):

    make format
    set -o pipefail; make lint-ci
    export TEANODE_TEST_DATABASE_HOST=<ip of a postgres test container>
    go test -mod=vendor -count=1 ./internal/travel/ ./internal/db/ ./internal/db/migrations/ ./internal/api/v1api/apigraph/ ./internal/agent/tools/location/ ./internal/cmd/ ./internal/cmd/server/ -run 'Travel|Trip|Location|Activity'
    (cd web && npx tsc --noEmit && npx vitest run src/pages/location.test.tsx)
    docker run --rm --name travel-swift-$$ -v "$PWD/ios/TeaNodeKit":/src -w /src swift:6.1 swift test --filter Location

## Validation and Acceptance

A test in `internal/travel` builds, from a home visit, positions along a road with walking then automotive activity, and an office visit, one trip with a walk leg then a car leg whose distances add up to the path length. Without activity, the same trip is one car leg guessed from speed; a 600 km trip in an hour is plane. A test in `apigraph` reports such a day, lists the trips, corrects the car leg to bus, reports more positions for the same day (which rebuilds the trips) and still sees bus; `ListTravelTotals` by week shows the bus distance in that week. Erasing the day removes its trips. On the dashboard and the iPhone, the day's list shows the trip rows and the Travel tab shows totals (CI screenshots `location-day`, `location-travel`).

## Idempotence and Recovery

Rebuilding trips is safe to repeat: they are derived, and corrections are separate. Reports sent twice add no activity (unique start). The migration only adds tables; its reverse drops them. Back up the production database before deploying (the migration is applied at start).

## Artifacts and Notes

None yet.

## Interfaces and Dependencies

In `internal/travel/travel.go`:

    type Mode string // walk, run, bike, car, bus, train, boat, plane
    type Activity struct { StartedAt time.Time; ActivityKind string; ActivityConfidence string }
    type Stay struct { VisitID string; ArrivedAt, DepartedAt *time.Time; LatitudeDegrees, LongitudeDegrees float64 }
    type Position struct { MeasuredAt time.Time; LatitudeDegrees, LongitudeDegrees, AccuracyMeters float64 }
    type Leg struct { StartedAt, EndedAt time.Time; DetectedMode Mode; DetectionSource string; DistanceMeters float64 }
    type Trip struct { FromVisitID, ToVisitID string; StartedAt, EndedAt time.Time; DistanceMeters float64; Legs []Leg }
    func BuildTrips(stays []Stay, positions []Position, activities []Activity) []Trip

In `internal/db`: `TravelOperation` with `PutMotionActivities`, `ReplaceTrips(userId, windowStart, windowEnd, trips)`, `ListTrips`, `GetTripLeg`, `SetTravelModeCorrection(userId, startedAt, endedAt, mode *string)`, `ListTravelTotals`, and the erase.
