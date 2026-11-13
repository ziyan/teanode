# Parity: Location and Health

What the iPhone app has in `ios/` for Location and Health, and where each item stands in the Android app. "Done" means
ported and photographed or tested here; "Partly" says what is missing; "Not ported" says why. Anything that needs a real
server, a phone or a credential is marked **not verified**: the demo server and the Robolectric screenshots stand in for
them.

Android differences, in one place:

- No visit monitor: Android has no `CLVisit`. The recorder detects a stay itself (`LocationDwellDetector` in the kit) and
  reports it as a "visit" the same way, and watches the person's named places with geofences (reported as "region" visits).
- The place provider and the maps are the server's and OpenStreetMap's (osmdroid, no key); the screenshots draw a stand-in map.
- Health reads Health Connect, not HealthKit. The server's table is HealthKit-named, so Health Connect records are sent under
  the same names for the kinds both have (steps, heart rate, sleep, workouts, weight and the like).
- Charts are drawn with Compose canvas (no Swift Charts), and are shared in `com.teanode.app.charts` (`ChartParts`).

## Kit (`android/kit`, `com.teanode.kit.location`, `.health`, `.charts`)

| iOS file | Android | Status |
| --- | --- | --- |
| Location.swift (models, documents, calls, setup, place change, check-in target) | location/Location.kt, ShareTargets.kt | Done. Documents copied; the circle and share-person calls and `ShareTarget` live in `ShareTargets.kt` so Location does not depend on Calendar. |
| LocationFeed.swift (subscription) | LocationModel.listen over `Connection.subscribe` | Done (not verified against a server). |
| LocationOutbox.swift (geometry, key, point filter, outbox, batch, retry gate, watched places, days) | location/LocationOutbox.kt | Done, 52 tests ported. `WatchedStore` is read in `WatchedStore.kt`. |
| LocationPause.swift (pause, choice, tracker, retry, timeline) | location/LocationPause.kt | Done, tests ported. |
| LocationSharingLimit.swift | location/LocationSharingLimit.kt | Done, tests ported. |
| LocationTravel.swift | location/LocationTravel.kt | Done, tests ported. |
| PlaceSearch.swift | location/PlaceSearch.kt | Done, tests ported. |
| HealthTypes.swift | health/HealthTypes.kt | Done (the HealthKit identifier helper is not ported: not used on Android). |
| HealthMetadata.swift | health/HealthMetadata.kt | Done, tests ported. |
| HealthValueWords.swift | health/HealthValueWords.kt | Done, tests ported. |
| HealthDaySummaries.swift | health/HealthDaySummaries.kt | Done, tests ported. |
| HealthReport.swift (inputs, batch, sender, views, documents, calls) | health/HealthReport.kt | Done, tests ported. |
| HealthSyncState.swift | health/HealthSyncState.kt | Done, tests ported. |
| ChartShares.swift | charts/ChartShares.kt | Done, tests ported (the axis text test is Finance's). |
| (new) dwell detection for visits | location/LocationDwellDetector.kt | Done, tested. |

## Location screens (`com.teanode.app.location`)

Every row names its iOS file.

| Item | iOS file | Status |
| --- | --- | --- |
| Location screen: map of the whole screen with a drawer (Visits, Places, Sharing), check-in and settings buttons | LocationViews.swift, LocationDrawer.swift | Done as a bottom sheet over the map (Material 3 `BottomSheetScaffold`). Side panel for wide screens: not ported (phones only). |
| Map: own position, people pictures, named places, chosen person's day path and places, focus on a person, own focus | LocationViews.swift (LocationMapHeader) | Done. |
| Map pins that would overlap are grouped and listed on a tap | LocationMapPins.swift | Done (`LocationMapPinGrouping` in the kit, tested; stacked pictures with a count, a tap lists who is there). On the real map the groups are worked out again at each zoom (not verified on a device). |
| Visits list: banner, people card, Travel row, days with Map links, timeline rows, load more, empty state (recording on or off), credit | LocationViews.swift | Done. |
| Visit row with photo strip (count, first photos, opens Photos) | LocationViews.swift | Done: opens Photos filtered to the visit (`PhotoFilterRequest`, a small addition in the Photos package that its library reads once when it opens). |
| Forget a visit (not for one in progress), confirmation | LocationPauseViews.swift | Done (overflow menu in place of a swipe). |
| Pause row; own pause note over the drawer | LocationPauseViews.swift | Done. |
| Trip rows with legs; correct a leg's way of travel or take it back | LocationTripViews.swift | Done. |
| Day screen: map with path and markers, timeline, erase this day, forget a visit, empty and error states | LocationViews.swift | Done. |
| Places list: search with delay, label filter, empty states, load more | LocationViews.swift | Done. |
| Place screen: name, label, address, change address, map with radius, tap to move, use where I am now (accuracy rule), radius slider, visits, forget visit, forget place | LocationPlaceViews.swift | Done. |
| Address search (place's address, "keep as typed", home setup) | LocationAddressViews.swift | Done. |
| Check-in sheet: here (with precision), nearby, search, more | LocationPlaceViews.swift | Done. |
| People rows: own row, people card, more than three open the People screen, distance, "Nearby" | LocationPeopleViews.swift | Done. |
| Chosen person: header, mute menu, unmute, close, their day with previous and next day, travel, places, "shares where they are now" | LocationDrawer.swift, PersonVisitsScreen.swift | Done (the Travel row of a person's day opens the route `location/travel/{userId}`). |
| Sharing banner (paused, recording off, phone location off, while using, shares less) with its action | LocationSharingBannerViews.swift | Done. |
| Sharing list, change level, stop sharing, share sheet with person or circle picker | LocationSharingViews.swift | Done. |
| Setup list (allow location, motion, home, type address, family circles, not now) | LocationSetupViews.swift | Done. |
| Settings: refusal notice, missing permission, record switch, waiting count, pause and resume, motion, erase | LocationSettingsViews.swift | Done. |
| "Always" explanation sheet | LocationSettingsViews.swift | Done (`AlwaysExplanation`), shown by the recorder before the permission question. |
| Pause choices (hour, tomorrow morning, until resumed) | LocationPauseViews.swift | Done. |
| Travel screen: Day/Week/Month/Year, earlier and later, totals by way | LocationTripViews.swift | Done. |
| Words: labels, categories, times, distances in the person's units | LocationText.swift | Done (`LocationText`); the units follow `LocalDistanceUnits`, which Settings may provide. |
| Demo data, every state | LocationDemo.swift, LocationTravelDemo.swift | Done (`LocationDemo`, five scenarios). |
| Recorder: permission flow, foreground recording, outbox, reporting, pause tracker, geofences | LocationRecorder.swift | See "Recording" below. |

## Recording (`com.teanode.app.location.recording`)

Not verified on a phone or an emulator. The logic it uses (filter, outbox, batches, retries, pause tracker, dwell) is the
kit's and is tested.

| Item | Status |
| --- | --- |
| Fused location updates in a foreground service (type `location`) while recording is on | Written, not verified. |
| Positions kept by the point filter, queued in the per-account outbox file, reported in batches with the iPhone app's retry and split rules | Written, not verified. |
| Visits from the dwell detector, and arrival or departure at watched places by geofence | Written, not verified. |
| Pause and resume applied at once and retried until the server has them | Written, not verified. |
| Permission report to the server (`ReportLocationPermission`) | Written, not verified. |
| Activity recognition for the way of travel | Written, not verified: Activity Recognition transitions (driving, cycling, running, walking, still) become motion activities in the outbox, sent with the positions. `LocationMotion` in the kit is tested. Android gives no confidence, so "medium" is sent. |

## Health screens (`com.teanode.app.health`)

| Item | iOS file | Status |
| --- | --- | --- |
| Health screen: kinds grouped by category with the latest day, person menu, settings, empty states (off, copying, nobody shares), not allowed | HealthViews.swift, HealthModel.swift | Done. |
| Kind screen: range and period pickers, headline, chart, figures, recent readings with load more, erase | HealthTypeViews.swift | Done. |
| Chart plan (bars, stages, level), axes, figures, per-day reading | HealthChartPlan.swift | Done, tested. |
| Charts (bars, stacked sleep stages, level with band), touch callout | HealthChartViews.swift, ChartParts.swift | Done (`com.teanode.app.charts` holds the shared parts). |
| Workout route: map, start and finish, date, duration, distance, energy | HealthRouteViews.swift | Done. |
| Words: categories, kinds, stages, workouts, counts, durations, characteristics | HealthWords.swift, HealthText.swift | Done (about 150 strings in three languages). |
| Settings: copying switch, status, server count, agent switch, about you, sharing, erase all | HealthSettingsViews.swift, HealthSettingsModel.swift | Done. |
| Sharing by category with a person or a circle; share screen with toggles and stop | HealthSharingViews.swift | Done. |
| Setup sheet (what, where, who) before Health Connect's permission screen | HealthSetupViews.swift | Done. |
| Demo data | HealthDemo.swift | Done. |
| Reading Health Connect and sending it | HealthKitReading.swift, HealthSync.swift | See "Health Connect" below. |

## Health Connect (`com.teanode.app.health.sync`)

Not verified on a phone: Health Connect needs a device with it.

| Item | Status |
| --- | --- |
| Permissions, availability, turning on and off | Written, not verified. |
| Reading steps, distance, active energy, floors, heart rate, resting heart rate, oxygen, respiratory rate, weight, height, body fat, blood pressure (both numbers), glucose, temperature, VO2 max, hydration, sleep sessions with stages, exercise sessions | Written, not verified. |
| Workout energy and distance totals | Written, not verified: read with Health Connect's aggregate over the session and sent in the sample's metadata. |
| Workout routes | Partly: sent when Health Connect returns the route; for a workout written by another app it asks the person's consent per workout, and that consent screen is not built, so those routes are not sent. |
| Characteristics (date of birth, blood type and the like) | Not ported: Health Connect has none. |
| First pass over the history (a year with the history permission, else 29 days), then Health Connect's changes token; day summaries worked out here and sent | Written, not verified. |
| Deletions | Partly: a deleted single reading is sent as deleted and its days are worked out again; the points of a deleted series (heart rate, sleep stages) are not named by Health Connect, so they stay on the server. |
| Kinds Health Connect does not have (HealthKit only: audiograms, ECG, State of Mind, cycle tracking, and many more) | Not ported: the platform has no such data. |
| Observers that wake the app on new data | Partly: Health Connect has no callback for new data. A WorkManager job runs every 15 minutes (its shortest period; Android may delay it asleep), and a pass runs whenever a Health screen opens. |
