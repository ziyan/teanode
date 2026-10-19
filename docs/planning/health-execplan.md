# Health: the person's Apple Health data on their own server

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here. It replaces Milestone 13 of `docs/planning/ios-app-execplan.md`, which now points here.

## Purpose / Big Picture

Apple keeps a person's health data on their iPhone (and Watch): steps, heart rate, sleep, workouts, weight and a hundred other kinds. There is no web or server API for it; only an app on the phone, with the person's permission for each kind, can read it. After this plan:

- In the TeaNode iPhone app, the person turns on **Health** and allows what to copy on Apple's Health permissions screen ("Turn On All" allows everything). The app copies it to the person's own TeaNode server, history first and then kept up to date in the background.
- The dashboard has a **Health** page, and the iPhone app a Health screen, laid out like Finance: the kinds of data the person has, grouped as the Health app groups them (Activity, Heart, Sleep, Body Measurements and so on), a chart for each by day, week or month, the recent readings, and workouts with their route on a map.
- `teanode health` prints the same from the command line.
- The agent can answer "how did I sleep this week?" or "how many steps did I average this month?" through a read-only `health` tool, but only after the person turns on **Let the agent read Health data**, which says in words that what it reads goes to the model's provider.
- A person can share some of it with a circle or one person in a circle: they choose which Health categories (for example Activity and Sleep), and the people it is shared with see the day by day charts of those categories, never the individual readings or the routes.
- The person can erase all of it, or one kind, from every surface.

To see it working at the end: on a development server, a phone (or the demo data on the simulator) reports a week of steps from two sources that overlap (a phone and a watch counting the same walk), a night of sleep stages, a run with a route and a weight. The dashboard's Health page shows steps per day equal to what Apple's Health app shows, not the sum of both sources; the sleep chart shows the night's stages; the run shows its route. `teanode health summary stepCount --since 7d` prints the same numbers. The agent, asked "how much did I sleep last night", says it cannot read Health data until the person turns it on; after turning it on, it answers from the summary. A second account in the same family circle, shared Activity, sees the steps chart of the first and not their sleep. Erasing Health on the dashboard empties every page, and the phone stops sending until Health is turned on again.

## Progress

- [x] (2026-10-18) Research: Apple's rules for HealthKit apps, the HealthKit APIs, what exists in the repository, reference apps. Summarized in Surprises and the Decision Log.
- [x] (2026-10-18) The maintainer answered the open questions (see the Decision Log): routes in the first version, clinical records later; the agent switch off until turned on; sharing some health data with circles; the privacy policy drafted here; shipped only after the first App Store version is approved.
- [x] (2026-10-18) This plan written.
- [x] (2026-10-18) Milestone 1: storage, reporting, reading, erasing and sharing on the server, with the command line and the agent's tool (built by subagents from this plan, read and tested here).
- [x] (2026-10-18) Milestone 2: the iPhone reads HealthKit, reports it, and shows it (TeaNodeKit tested on Linux; the app is not compiled until the macOS run).
- [x] (2026-10-18) Milestone 3: the dashboard's Health page.
- [x] (2026-10-18) First use on a development server: four months of invented data sent as a phone sends it, sent again (nothing new), read with `teanode health`; the dashboard walked in Chrome at 1400 and 390, light and dark.
- [x] (2026-10-18) Review round 1 (server, dashboard, iPhone, words and parity, the walk-through): about 80 findings.
- [x] (2026-10-18) Review round 2 (about 60 findings, none high) and fixes: the erase guard compares no clocks, the tool refuses runs nobody watches, one table of words (`internal/health/words.go`) that the dashboard and iPhone copy and pin in tests.
- [x] (2026-10-19) Review round 3 (one medium per area) and fixes: what a turn that read Health said is left out of memory filing, percent kinds near 100 read as whole numbers, one tie rule for rounding, the dashboard lists readings without a range.
- [x] (2026-10-19) Review round 4: the dashboard and the iPhone found nothing above low; the server one medium (the memory wording promised more than the code), fixed. Walked again in Chrome at 1400 and 390, light and dark; the agent checked for real on a development server (switch off, on, and asked about somebody who shares).
- [x] (2026-10-18) Milestone 4: getting started (the paragraph in `docs/getting-started.md`, the phone tour's offer, the dashboard's and the app's empty states), `docs/subsystems/health.md`, the decision record, the command line reference, and the privacy policy on the teanode.com site (published 2026-10-18 by that site's session).
- [ ] The iPhone screenshots from CI with the ux-review skill, once the hold is lifted (the app has not been compiled yet: macOS CI only).
- [ ] Held: the pull request stays a draft with no macOS run until the first App Store version (build 518) is approved; then CI, TestFlight, the maintainer's phone.

## Surprises & Discoveries

- Observation: adding up samples counts the same steps twice. An iPhone and a Watch both record steps for the same walk, as separate samples from two sources. Apple's Health app shows one number because its statistics queries merge sources by the person's chosen order (Health > Data Sources & Access). Only those queries (`HKStatisticsCollectionQuery`) can do it; the order is not readable by an app.
  Evidence: HealthKit documentation for `HKStatisticsQuery` ("merges the data from all sources"), and the Health app's data source priority screen.

- Observation: a deleted sample arrives only as its identifier (`HKDeletedObject` has a UUID and nothing else), so the phone cannot tell which day it belonged to. The server can: it has the sample.
  Evidence: `HKDeletedObject` documentation.

- Observation: Health data cannot be read while the phone is locked; HealthKit encrypts its store. A background wake while locked finds nothing and must wait for the next unlock. Most types wake an app at most once an hour (`stepCount` is hourly at most on iOS), and background delivery needs the `com.apple.developer.healthkit.background-delivery` entitlement since iOS 15.
  Evidence: developer.apple.com "Protecting user privacy" (Access encrypted data) and `enableBackgroundDelivery(for:frequency:withCompletion:)`.

- Observation: an app is never told which kinds the person refused: a refused kind looks like a kind with no data.
  Evidence: "Protecting user privacy": "an app isn't aware when the user denies permission to read data".

- Observation: Apple's rules allow this. The App Store Review Guidelines (5.1.3) forbid advertising, data mining and selling health data and storing it in iCloud, and allow sharing with a third party only with permission and only one that also gives the person a health or fitness service. 5.1.2(i) now names "third-party AI" as needing explicit permission. Sending it to the person's own server is not a disclosure to a third party; App Store apps do exactly that (Health Auto Export posts to an address the person chooses), and an assistant reading Apple Health (ChatGPT Health, in 2026) passed review. A privacy policy is required and is linked from the permission sheet.
  Evidence: developer.apple.com/app-store/review/guidelines (5.1.2, 5.1.3, 2.5.1), "Protecting user privacy", read 2026-10-18.

- Observation: the dashboard's charts are drawn by hand in SVG (`web/src/components/seriesChart.tsx`, `SeriesChart`), not with ECharts as the iPhone plan's Milestone 13 said; ECharts is only for artifact pages. The iPhone draws with Swift Charts through `ios/TeaNode/Sources/ChartParts.swift`.
  Evidence: `web/src/pages/finance/financeNetWorth.tsx`, `ios/TeaNode/Sources/FinanceMoreViews.swift`.

## Decision Log

- Decision: everything HealthKit lets an app read, except clinical records: quantities, categories (sleep and the rest), workouts with their routes, electrocardiogram results (the classification and average heart rate, not the voltages), audiograms, State of Mind, and the characteristics (date of birth, biological sex, blood type, skin type, wheelchair use). Clinical records come later, behind their own entitlement. Medications (iOS 26) come later too, because they need a separate per-medication permission.
  Rationale: the maintainer asked for all their data, with routes in the first version and clinical records later. Clinical records need a second entitlement and stricter review.
  Date/Author: 2026-10-18, maintainer.

- Decision: the agent's `health` tool is off until the person turns on **Let the agent read Health data** (`user.can_agent_read_health`, false by default), separately from syncing. The switch says that what the agent reads goes to the model's provider, and names it.
  Rationale: the maintainer's answer. Apple asks for explicit permission before health data goes to a third-party AI; someone may want their data kept and charted without the agent reading it. This is the one place the project's "on by default" gives way.
  Date/Author: 2026-10-18, maintainer.

- Decision: the phone works out each day's summary and sends it with the samples; the server keeps both. A summary for a quantity comes from Apple's statistics query (merged across sources); for a category (sleep, mindful minutes) from the union of the samples' times, so two sources overlapping are counted once; for workouts, the count, minutes, energy and distance. The day is the phone's calendar day where the samples were taken. When the server deletes samples it answers with their kinds and times, so the phone works those days out again.
  Rationale: only the phone can merge sources as the Health app does (see Surprises). A chart that disagrees with the Health app would be wrong in the person's eyes.
  Date/Author: 2026-10-18, design.

- Decision: health data is kept out of memory as far as the code can. Dreams never read it, and the agent reads it only through the tool, which answers summaries first and samples a page at a time. When conversations are filed into memory, what a turn that read Health said is left out (a steered message belongs to the turn it was steered into). Health figures the person or the agent repeats later in a conversation, or that the agent writes to memory because the person asked, are kept like anything else said there; the tool's guidance tells the agent not to copy Health into notes or memory unless asked.
  Rationale: the iPhone plan's rule. Health data is among the most private there is; finance follows the same pattern (rows read in SQL, `docs/decisions/20260929-finance-transactions-are-rows-not-documents.md`), and location is the exception because its routine is the point.
  Date/Author: 2026-10-18, design.

- Decision: Health is shared with a circle or one person in a circle by **Health category**: the person chooses which categories (Activity, Heart, Sleep, ...), and the people it is shared with read the day summaries of the kinds in those categories. Never samples, routes, characteristics or electrocardiograms. A share is one row per grantee with the list of categories; an empty list is no share.
  Rationale: the maintainer asked to share some health data with circles. A family wants to see "slept 7 hours, walked 9,000 steps", not a heart rate series. Categories are the Health app's own groups, so a person knows what they are choosing. It follows the location share (`location_share`: one grantee, a user or a circle) and the rule that a viewer short of the share gets "not found" (`docs/decisions/20261017-a-location-is-shared-at-a-level.md`).
  Date/Author: 2026-10-18, maintainer and design.

- Decision: the words. **Health** is the feature, the page and the screen. A **health type** is one kind of data, named by HealthKit's identifier without its prefix (`stepCount`, `heartRate`, `sleepAnalysis`, `workout`); a **sample** is one reading (a value over a time, or a category value over a time, or a workout); a **day summary** is one type on one day; a **Health category** is one of the Health app's groups of types; a **route** is a workout's path. Removing data is **erase**, as for location history (`EraseHealthData`). The agent's switch is **Let the agent read Health data**.
  Rationale: one word per idea, reusing HealthKit's and the Health app's words where they exist, and the location feature's "erase".
  Date/Author: 2026-10-18, design.

- Decision: the phone sends with one GraphQL operation, `ReportHealth`, as it sends location with `ReportLocation`, not a REST route as Milestone 13 sketched; the iPhone keeps no copy of health data. It reads from HealthKit from a saved anchor, sends, and moves the anchor only when the server has answered, so a failed send is read again rather than kept.
  Rationale: the location report is the working pattern (batches, a wait after a failure, halving a refused batch); REST is used only where bytes go up (photos). Keeping no copy on the phone keeps health data out of the app's files, and so out of iCloud backups (Apple forbids health data in iCloud).
  Date/Author: 2026-10-18, design.

- Decision: shipped only after the first App Store version (build 518) is approved; the pull request stays a draft without a macOS run until then. If that version is rejected, it is resubmitted from a build without HealthKit.
  Rationale: the maintainer's answer, and the iPhone plan's rule that the first release must not claim HealthKit: App Review checks what an app with the HealthKit entitlement does with it.
  Date/Author: 2026-10-18, maintainer and coordinator.

- Decision: the agent reads only the person's own Health data, never what somebody shares with them; the dashboard, the iPhone and the command line show the person what is shared with them.
  Rationale: a person who shares Health shares it with people, not with their agents' model providers, and only the sharer can agree to their data going to one. It also keeps text another person controls (type names, units) out of the agent's context. Found in review round 1.
  Date/Author: 2026-10-18, design.

- Decision: a phone says with each report the last erase time it knows of (`healthErasedAt`, read when Health is turned on), and the server stores nothing from a phone that does not know of the person's latest erase; it answers with the erase time and the phone turns Health off. (Round one compared the time Health was turned on with the erase time; round two dropped that, because it compared the phone's clock with the server's.)
  Rationale: review round 1 found that a batch in flight, or the next one, filled the server again after Erase. Erasing one kind erases what is kept of it; new readings of that kind still arrive while Health is on, and the confirmation says so.
  Date/Author: 2026-10-18, design.

- Decision: percentages are kept as HealthKit keeps them, a fraction (0.97), and every surface shows them as a percentage. Units are written the same way everywhere: steps and other counts as plain numbers, heart rate in bpm, distance in km from 1000 m, temperature in °C.
  Rationale: the walk-through found "0.97%" on the dashboard, "count/min" in the command line and "74.3/min" on the dashboard for the same heart rate.
  Date/Author: 2026-10-18, design.

## Outcomes & Retrospective

(To be written at the end.)

## Context and Orientation

TeaNode is a Go program (`cmd/teanode-server`, the server, and `cmd/teanode`, the command line) with a React dashboard (`web/`) embedded in the server binary and a SwiftUI iPhone app (`ios/`). Read `AGENTS.md`, `CONTRIBUTING.md` and `docs/coding/frontend-design.md` first, and `ios/DESIGN.md` for any iPhone screen.

The Location feature (`docs/planning/location-execplan.md`, `docs/subsystems/location.md`) is the model for almost everything here:

- Permissions are listed in `internal/models/permission.go` (`location:use`) and granted to roles by a migration (`0240_location.sql` grants `location:use` to Administrator, Operator and Member). Every GraphQL resolver calls a helper in `internal/api/v1api/apigraph/authorize.go`; `TestEveryOperationAuthorizes` fails otherwise. A refusal answers "not found".
- The phone reports with `ReportLocation(agentPhoneId, points, visits)` in `internal/api/v1api/apigraph/location_history.go` (validation in `validateReport`, at most so many rows a call, a per-person lock `tx.LockLocation`, `ON CONFLICT DO NOTHING` so a resent batch adds nothing).
- Sharing is `location_share` (`grantee_user_id` or `grantee_circle_id`, one of them), checked on every read by `reachLocation` / `GetLocationShareLevel`.
- On the iPhone, `ios/TeaNode/Sources/LocationRecorder.swift` and `ios/TeaNodeKit/Sources/TeaNodeKit/LocationOutbox.swift` (batches, `LocationRetryGate`, `LocationReportFailure.classify`, `sendSplittingInvalid`, `BackgroundTaskHolder`); Photos backup uses `BGProcessingTaskRequest` (`PhotoBackupEngine.swift`, identifiers in `BGTaskSchedulerPermittedIdentifiers` in `ios/project.yml`).
- Per-person switches live on the user (`user.is_photo_face_recognition_enabled`) or the agent (`agent.is_tips_enabled`, set by `UpdateAgent`).

Finance is the model for the charts and the reading tool: `web/src/components/seriesChart.tsx`, `web/src/pages/finance/financeNetWorth.tsx` (`NetWorthChart`, ranges 30/90/365/all), `financeSpending.tsx` (the period in the address); iPhone `ChartParts.swift`, `FinanceMoreViews.swift` (`NetWorthView`); the agent's `internal/agent/tools/finance/finance.go` (series operations, row lists with `limit`, `offset` and a total); the command line `internal/cmd/finance.go` (`rangeFlags`: `--from`, `--to`, `--since`).

## The contract every surface shares

As first planned, and corrected where review changed it (see the Decision Log); where they differ, `internal/api/v1api/apigraph/health.go` and `docs/subsystems/health.md` are the reference. Two tables were added in review: `health_type_total` (counts per type, kept by a trigger) and `health_deleted_sample` (deletes of the last 30 days).

### Storage (migration `0245_health`, with `.reverse.sql` and a reverse test like its neighbours)

    health_sample
      id                  varchar(32) primary key (ULID)
      user_id             varchar(32) not null, references user on delete cascade
      health_sample_id    varchar(64) not null      -- HealthKit's UUID; unique per person
      health_type         varchar(96) not null      -- "stepCount", "sleepAnalysis", "workout", ...
      started_at          timestamptz not null
      ended_at            timestamptz not null
      quantity_value      double precision          -- a quantity's value in quantity_unit
      quantity_unit       varchar(32) not null default ''
      category_value      integer                   -- a category sample's value (HealthKit's raw value)
      workout_activity_type varchar(64) not null default ''  -- "running", ... for workout
      source_name         varchar(256) not null default ''
      source_bundle_id    varchar(256) not null default ''
      device_name         varchar(256) not null default ''
      sample_metadata     jsonb not null default '{}'  -- workout totals, ECG classification, audiogram points, State of Mind labels, HealthKit metadata keys
      received_at         timestamptz not null
      unique (user_id, health_sample_id)
      index (user_id, health_type, started_at desc)

    health_day_summary
      user_id, health_type, summary_day date        -- primary key (user_id, health_type, summary_day)
      quantity_unit       varchar(32) not null default ''
      sum_value, average_value, minimum_value, maximum_value  double precision (null when not meaningful)
      sample_count        integer not null default 0
      duration_seconds    double precision            -- categories and workouts: time covered, sources overlapping counted once
      category_seconds    jsonb not null default '{}' -- sleep: {"asleepCore": 12600, "asleepDeep": 3000, "asleepREM": 5400, "awake": 600, "inBed": 27000}
      updated_at          timestamptz not null

    health_workout_route
      user_id, health_sample_id (the workout's)    -- primary key (user_id, health_sample_id)
      route_points        jsonb not null           -- [[secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters], ...], at most 20,000
      received_at         timestamptz not null

    health_characteristic
      user_id, health_type                         -- primary key; "dateOfBirth", "biologicalSex", "bloodType", "fitzpatrickSkinType", "wheelchairUse"
      characteristic_value varchar(64) not null    -- "1990-04-12", "female", "aPositive", ...
      updated_at

    health_share
      id, user_id, grantee_user_id | grantee_circle_id (exactly one), health_categories text[] not null,
      created_at, created_by_user_id; unique (user_id, grantee_user_id), unique (user_id, grantee_circle_id)

    user.can_agent_read_health boolean not null default false
    user.health_erased_at      timestamptz             -- the phone stops and forgets its anchors when this is newer than when it turned Health on

    permission health:use granted to Administrator, Operator and Member.

### The health types (`internal/health/types.go`)

One table in Go: each health type's identifier, its Health category, its English name, what its value is (`quantity`, `category`, `workout`, `electrocardiogram`, `audiogram`, `stateOfMind`, `characteristic`), the unit it is sent in, and how its days add up (`sum` for steps, energy, distance; `average` with minimum and maximum for heart rate, weight; `duration` for sleep, mindful minutes; `count` for events). The categories are the Health app's: `activity`, `bodyMeasurements`, `cycleTracking`, `hearing`, `heart`, `mentalWellbeing`, `mobility`, `nutrition`, `respiratory`, `sleep`, `symptoms`, `vitals`, `other`. A type the table does not know is accepted (stored under `other`, named from its identifier) so a new iOS type needs no server change. The iPhone has the same identifiers in TeaNodeKit (`HealthTypes.swift`) to ask HealthKit for; the dashboard and the app translate the names (en, zh, ja) and fall back to the server's English name.

### GraphQL (`internal/api/v1api/apigraph/health.go`)

    ReportHealth(healthErasedAt, samples: [HealthSampleInput], deletedHealthSampleIds: [String], daySummaries: [HealthDaySummaryInput], routes: [HealthRouteInput], characteristics: [HealthCharacteristicInput]): ReportHealthView
      at most 1000 samples, 2000 deleted ids, 1000 day summaries, 5 routes, 20 characteristics a call
      ReportHealthView { newSampleCount, deletedSampleCount, updatedDayCount, deletedSamples: [{ healthType, startedAt, endedAt }], healthErasedAt }
    ListHealthTypes(ownerUserId?): [HealthTypeView]          -- the types the person (or a sharer) has: healthType, healthCategory, typeName, valueKind, quantityUnit, summaryKind, firstDay, lastDay, sampleCount, latestDaySummary
    ReadHealthSummary(healthType, fromDay, toDay, period: day|week|month, ownerUserId?): HealthSummaryView
      HealthSummaryView { healthType, quantityUnit, summaryKind, periods: [{ startDay, endDay, sumValue, averageValue, minimumValue, maximumValue, sampleCount, durationSeconds, categorySeconds }] }
      at most 400 periods; a week starts on Monday; a month's average is weighted by sample count
    ListHealthSamples(healthType, from, to, limitCount, offset): HealthSamplePage   -- own data only; limitCount default 50, at most 200; { samples, totalCount, nextOffset }
    ReadHealthRoute(healthSampleId): HealthRouteView                              -- own data only
    ReadHealthSettings: { canAgentReadHealth, healthErasedAt, sampleCount, lastReceivedAt, modelProviderName, characteristics }
    UpdateHealthSettings(canAgentReadHealth): same
    EraseHealthData(healthType?): { erasedSampleCount }       -- no type: everything, and health_erased_at = now
    ListHealthShares: [HealthShareView]                        -- the caller's own shares, and the ones shared with them (sharer, categories)
    SetHealthShare(granteeUserId? | granteeCircleId?, healthCategories: [String]): HealthShareView   -- empty list removes it; the grantee must share a circle with the caller
    
The shapes, named the same in Go (`json` tags), GraphQL, TeaNodeKit and the dashboard:

    HealthSampleInput / HealthSampleView: healthSampleId, healthType, startedAt, endedAt, quantityValue?, quantityUnit,
      categoryValue?, workoutActivityType, sourceName, sourceBundleId, deviceName, sampleMetadataJson (a JSON object as a string)
      (the view adds receivedAt and hasRoute)
    HealthDaySummaryInput: healthType, summaryDay ("YYYY-MM-DD"), quantityUnit, sumValue?, averageValue?,
      minimumValue?, maximumValue?, sampleCount, durationSeconds?, categorySecondsJson (a JSON object as a string), workedOutAt
    period rows: startDay, endDay, the values, sampleCount, dayCount (days with a summary), durationSeconds?, categorySeconds (a JSON object)
    HealthRouteInput / HealthRouteView: healthSampleId, routePoints: [[Float]] ([secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters])
    HealthCharacteristicInput: healthType, characteristicValue
    HealthShareView: id, ownerUserId, ownerUsername, ownerName, granteeUserId?, granteeUsername?, granteeCircleId?, granteeCircleName?, healthCategories, createdAt

Every operation needs `health:use`. `ownerUserId` reads a sharer's data: allowed only while a share reaches the caller (directly, or through a circle both are in) and only for types in the shared categories, and only summaries and types; otherwise "not found". Erasing removes samples, summaries, routes and characteristics (the type's, or all).

### The agent (`internal/agent/tools/health/health.go`)

One read-only tool, `health`, needing `health:use`, with actions `types`, `summary` (health_type, from, to, period) and `samples` (health_type, from, to, limit_count, offset), and `set_up`; the person's own data only (see the Decision Log). While `can_agent_read_health` is false every action but set_up answers that the person has not let the agent read Health data, and where they turn it on (the Settings tab of the Health page on the dashboard, or Health in the iPhone app's Settings). Summaries are preferred: the description says to use `summary` and to read samples only for a question about particular readings; samples page like every other read and say when there is more.

### The command line (`internal/client/health.go`, `internal/cmd/health.go`)

`teanode health types`, `teanode health summary <type> [--from --to --since --period day|week|month] [--person <username>]`, `teanode health samples <type> [--from --to --since --limit --offset]`, `teanode health route <sample-id>`, `teanode health settings [--agent-can-read on|off]`, `teanode health erase [<type>] --force`, `teanode health shares`, `teanode health share <circle-or-username> <category>...` (none removes), each with `--json`. The parity tests check the names against the operations.

### The dashboard (`web/src/pages/health/`)

A Health page in the side menu (after Location, shown with `health:use`): the person's types grouped by category, each with its latest day; choosing one opens its chart (day, week or month bars or a line for averages, ranges 30 days, 90 days, a year, all; the type, period and range in the address), its numbers, and its recent samples (own data only), a workout's route on the map (the Location page's map component). A person picker at the top when someone shares Health with them. Settings: Let the agent read Health data (with the provider named), the shares (category checkboxes per circle or person), Erase. With no data yet: what Health is and that it comes from the iPhone app (Settings > Health in the app), as a button to the app's page.

### The iPhone

- `ios/project.yml`: the HealthKit capability (`com.apple.developer.healthkit`, `com.apple.developer.healthkit.access` empty, `com.apple.developer.healthkit.background-delivery`), `NSHealthShareUsageDescription` ("TeaNode copies the Health data you choose to your own TeaNode server, and nowhere else."), a BGTask identifier for the history.
- TeaNodeKit (Linux tested): `HealthTypes.swift` (the identifiers and their categories, as the server's table), `HealthReport.swift` (the upload shapes and the GraphQL calls), `HealthDaySummaries.swift` (the union of category samples' times per day, the workout totals; tested), `HealthSyncState.swift` (per type anchors as opaque data, the days waiting to be worked out again, when Health was turned on; stored in the app group with complete file protection and excluded from backup).
- App: `HealthSync.swift` (HealthKit: the permission request for every readable type; per type an `HKAnchoredObjectQuery` from the saved anchor, a page of up to 1000 at a time; `HKStatisticsCollectionQuery` for the day summaries of the days touched, in the phone's calendar; routes through `HKWorkoutRouteQuery`; characteristics; an `HKObserverQuery` per type with `enableBackgroundDelivery(.hourly)` set up at launch; the history in a `BGProcessingTask`; nothing read while the phone is locked), `HealthViews.swift` (the Health screen: categories, a chart per type with Swift Charts through `ChartParts`, recent samples, a workout route on a MapKit map, the person picker), `HealthSettingsViews.swift` (Health on or off, sync now, last sent and how much, the agent switch, sharing, Erase). The side menu gets Health after Location. The demo server answers every health operation with invented data, and the screenshot workflow photographs the Health screen, a type's chart and the settings.
- Health is a choice in settings, never a step of connecting; turning it on shows one screen saying what is copied and where, then Apple's Health permissions screen.

### Tests

Server: a batch sent twice stores it once; deleting samples answers their times; summaries for week and month add up and weight averages; erasing one type and everything; a person cannot read another's data, and a share reaches only its categories and only summaries; the reverse migration; the tool refuses while the switch is off and answers after; the command line's parity. iPhone: TeaNodeKit turns samples into the upload shape, unions overlapping sleep from two sources into one night, splits a night across midnight by the calendar's day, and decodes every view.

## Plan of Work

Milestone 1, the server (two subagents from this plan: storage and GraphQL; then tool and command line against the GraphQL). Milestone 2, the iPhone (one subagent for TeaNodeKit and its tests, one for the app against TeaNodeKit's interfaces as written above). Milestone 3, the dashboard (one subagent, against the GraphQL). Milestone 4, docs (`docs/subsystems/health.md`, `docs/reference/command-line.md`, `docs/getting-started.md`, the phone tour), the privacy policy (`app/privacy.html` on the teanode.com site, checked with that site's session first), and a decision record `docs/decisions/20261018-health-is-shared-by-category-as-day-summaries.md`. Every diff is read, built and tested here before it is committed.

## Validation and Acceptance

    TMPDIR=~/.cache/claude-tmp/tmp go test ./internal/health/... ./internal/db/ -run Health ./internal/api/v1api/apigraph/ -run 'Health|EveryOperation' ./internal/agent/tools/health/ ./internal/cmd/ ./internal/client/
    cd web && npx vitest run src/pages/health && npx tsc --noEmit -p . && npx eslint --max-warnings 0 src/pages/health
    docker run --rm -v "$PWD/ios/TeaNodeKit:/kit" -w /kit swift:6.1 swift test
    make lint-ci

Then the walk in "Purpose" on a development server, the dashboard in Chrome at 1400 and 390 wide in light and dark, and, once the hold is lifted, the iPhone screenshots and a TestFlight build on the maintainer's phone.

## Idempotence and Recovery

Reports are idempotent (unique sample identifiers, day summaries replaced). Erasing is final; the phone learns of it from `healthErasedAt` and stops. The migration's reverse drops the tables, the two user columns and the permission.

## Interfaces and Dependencies

No new Go or npm dependency. HealthKit, Swift Charts and MapKit on the iPhone. The maintainer adds Health to the App Store privacy answers (Health & Fitness, linked to the person, not used for tracking) before a build with Health goes to review. Before the first signed build with Health, the App ID `com.teanode.app` gets the HealthKit capability with background delivery in the developer portal (CI signs automatically and may add it itself; check the first TestFlight run), and the App Store version's description says what Health does, as Apple asks of a HealthKit app.
