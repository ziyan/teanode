# An Android app on par with the iPhone app

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It follows the ExecPlan rules the repository's planning documents share (see the other files in `docs/planning/`).

## Purpose / Big Picture

The iPhone app (`ios/`, planned in `ios-app-execplan.md`) does everything the dashboard does, natively: chats with the agent and with people, mail, the calendar, contacts, photos, location, health, passwords, shopping, finance, recordings, coding sessions and every setting. A person with an Android phone has only the dashboard in a browser tab, which cannot notify, cannot receive a share, cannot back up photos or record location in the background.

After this change, a person with an Android phone installs TeaNode from a Firebase App Distribution invite link, connects it to their own server the same way the iPhone app does (sign in on the server's page, or scan a connect code), and has the same app: the same sections, the same screens and the same refusals, drawn with Material 3. From then on every feature ships on the dashboard, the agent's tools, the command line, the iPhone app and the Android app together (`CONTRIBUTING.md`).

To see it working: open the invite link on an Android phone, install, sign in to a server, and ask the agent something in Chats; open Mail, the calendar and Settings and find what the iPhone app shows.

## Progress

- [x] (2026-11-13) Plan written; the work split into a foundation and eight areas.
- [ ] Milestone 0: the build. `android/` with `kit` and `app`, the release signing key, distribution to Firebase App Distribution, a CI job that builds and tests.
- [ ] Milestone 1: the foundation. Sign-in (server page and connect code), the account and its token renewal, the GraphQL client and its websocket, the shell (sections in a navigation drawer grouped as the iPhone's tabs), shared components, demo mode, screenshot tests.
- [ ] Milestone 2: the areas, built in parallel, each ported file by file from `ios/TeaNode/Sources` and `ios/TeaNodeKit`:
  - [ ] chats: the agent's conversations and people chats, turns, cards, approvals, attachments, voice messages, dictation;
  - [ ] mail and contacts;
  - [ ] the calendar, reminders, goals, ideas, knowledge and the journal;
  - [ ] photos (with backup) and recordings;
  - [ ] location (with background recording) and health (Health Connect);
  - [ ] finance and shopping;
  - [ ] passwords (with the Android autofill service) and coding;
  - [ ] settings, sharing, circles, device setup and search.
- [ ] Milestone 3: the server. Push to Android through the relay (Firebase Cloud Messaging), the Android update channel, phones listed with their platform.
- [ ] Milestone 4: calls (WebRTC with a foreground service), notifications answered from the notification, the share target, home screen widgets.
- [ ] Milestone 5: published to testers, documented, `CONTRIBUTING.md` and the parity checks cover Android.

## Surprises & Discoveries

(none yet)

## Decision Log

- Decision: the app lives in the main repository under `android/`, beside `ios/`.
  Rationale: the person asked for it there; one pull request can then change a GraphQL operation and every app that uses it.
  Date/Author: 2026-11-13, the person.
- Decision: Kotlin, Jetpack Compose and Material 3; a plain Kotlin/JVM `kit` module mirroring `TeaNodeKit`; minSdk 29, target and compile SDK 36; application id `com.teanode.app`, the iPhone app's bundle id, so the server's built-in OAuth redirect `com.teanode.app:/oauth` serves both.
  Rationale: native screens, as the iPhone app chose; the logic stays testable on the JVM without an emulator.
  Date/Author: 2026-11-13.
- Decision: testers get the app through Firebase App Distribution, from one tester group whose invite link anyone may use. The release key is the project's own, never committed.
  Rationale: the person asked for Firebase public testing; Google Play can follow from the same signed build.
  Date/Author: 2026-11-13, the person.
- Decision: notifications reach Android phones through the existing push relay, which gains Firebase Cloud Messaging beside APNs. A server still never holds the app's push credential.
  Rationale: the same reason the relay exists for the iPhone app: one published app, every server.
  Date/Author: 2026-11-13.

## Context and Orientation

- `ios/TeaNodeKit` is the model for `android/kit`; `ios/TeaNode/Sources` is the model for `android/app`. The iPhone app's `DemoServer.swift` is the model for demo mode, and its screenshot list in `.github/workflows/ios.yml` for the Roborazzi screenshot tests.
- The server's API is GraphQL at `/api/v1/graphql` and its websocket; attachments upload to `/api/v1/agent/attachments`. Sign-in is OAuth with the built-in client and the redirect `com.teanode.app:/oauth` (`internal/api/remote.go`), or a connect code (`ConnectLink`).
- Push: `internal/push` (sealed registrations, APNs), `internal/api/v1api/apipush` (the relay).
- App updates: `internal/appupdate` tells the apps a newer build exists.

## Plan of Work

The foundation is built first, alone, because every area builds on its shell, components and connection. The eight areas are then built at once, each in its own branch from the foundation, each porting its iPhone files one by one and photographing every screen. The server work runs beside them. The lead merges the area branches into one pull request.

## Validation and Acceptance

- `./gradlew :kit:test :app:testDebugUnitTest :app:assembleRelease` passes in `android/`.
- The Roborazzi screenshots of every screen, light and dark, are looked at.
- A release build installs from the App Distribution invite link and signs in to a real server.

## Interfaces and Dependencies

AGP 8.13, Kotlin 2.2, the Compose BOM, kotlinx.serialization, OkHttp, CameraX and ZXing for scanning connect codes, Firebase Messaging (only when `google-services.json` is present, so the public repository builds without it), Robolectric and Roborazzi for screenshots.

## Outcomes & Retrospective

(not yet)
