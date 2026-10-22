# Pause and resume location recording

This ExecPlan is a living document, kept as described in `~/.claude/PLAN.md`.

## Purpose

A person can stop TeaNode recording where they go for a while, without turning
recording off on the phone and remembering to turn it on again. They pause for
an hour, until tomorrow morning, or until they resume, from the iPhone, the
dashboard, the command line or by asking their agent. While paused nothing new
is recorded, the people they share with see "Location paused" instead of where
they are, and the timeline shows the gap as a pause, never as a stay.

Today the only control is "Record where I go" on the phone, which the phone
alone knows. A pause is the person's, so it lives on the server, and every
surface reads and sets the same thing.

## Progress

- [ ] Plan (this file).
- [ ] Server: migration 0257, database, GraphQL, report filtering, reach, events,
      journal, agent tool, CLI, docs.
- [ ] Dashboard.
- [ ] iPhone.
- [ ] Deploy from the branch, TestFlight from the branch, use it for real.
- [ ] Review until a round finds nothing; parity check.

## Decision Log

- 2026-10-22, the person: a pause is timed (an hour, until tomorrow morning) or
  open (until resumed). A timed pause ends by itself.
- 2026-10-22, the person: viewers see "Location paused" (and until when, if
  timed) with no position and no visit in progress; no arrive or leave
  notifications are sent while paused (none can be: nothing is recorded).
- 2026-10-22, the person: the agent pauses without a card; resuming early asks
  for a tap (it widens what is recorded).
- 2026-10-22, the person: the gap is shown ("Paused 14:00 to 15:00") on the
  timeline, and the journal and routine treat it as unknown, never as a stay.
- Pauses are rows, not a flag, so the timeline can show past gaps: one table,
  `location_pause`, a row per pause. Resuming sets its end to now.
- "Until tomorrow morning" is 08:00 the next day in the person's zone, computed
  by each client; the server takes only an end time.
- The server drops positions and visits measured inside a pause, whatever sends
  them, so a phone that was offline cannot fill the gap afterwards.
- Pausing ends the visit in progress at the pause's start, so a viewer is never
  left seeing "at home" for the length of the pause.
- The phone's "Record where I go" stays: it is the phone's switch (battery,
  permissions). A pause is the person's, across devices.
- The PR is stacked on `fix/circle-sharing` (#633), which changes the location
  tool and the share reach.

## Context

Location is described in `docs/subsystems/location.md`. Code:
`internal/db/database_location*.go`, `internal/api/v1api/apigraph/location*.go`
(resolvers on `*graph`: `GetLocation`, `ListSharedLocations`, `ReportLocation`,
`ListLocationVisits`, `EraseLocationHistory`, `LocationEvents` ...),
`internal/agent/tools/location/`, `internal/agent/location_journal.go`,
`internal/client/location*.go`, `internal/cmd/location.go`,
`web/src/pages/location.tsx`, `ios/TeaNode/Sources/Location*.swift`,
`ios/TeaNodeKit/Sources/TeaNodeKit/Location*.swift`.

## Interfaces (fixed; every surface builds against these)

Migration `0257_location_pause.sql` (and `.reverse.sql`, with a reverse test
beside `reverse_location_test.go`):

    CREATE TABLE "location_pause" (
      "id" text PRIMARY KEY,
      "user_id" text NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
      "started_at" timestamptz NOT NULL,
      "ended_at" timestamptz,            -- null: until resumed
      "created_at" timestamptz NOT NULL,
      "updated_at" timestamptz NOT NULL,
      CHECK ("ended_at" IS NULL OR "ended_at" > "started_at")
    );
    CREATE INDEX ... ON "location_pause" ("user_id", "started_at");

A pause is in effect at instant T when `started_at <= T AND (ended_at IS NULL OR
ended_at > T)`. At most one pause is in effect at a time: pausing while paused
changes the pause in effect (its `ended_at`), it does not add one.

Model `models.LocationPause{ID, UserID, StartedAt, EndedAt *time.Time, ...}`.

GraphQL (names exact):

- `LocationPauseView { id, startedAt, endedAt (nullable) }`.
- `LocationView.pause: LocationPauseView` (nullable): the pause in effect. For
  a viewer who is not the owner, while paused `latestPoint` and `openVisit` are
  null. The owner still sees their own latest position.
- `PauseLocation(endedAt: Time = null): LocationPauseView`: pauses now, or
  changes the end of the pause in effect. `endedAt` in the past or now is
  refused; more than 30 days ahead is refused. Needs `location:use`.
- `ResumeLocation: Boolean`: ends the pause in effect now; true when there was
  one. Needs `location:use`.
- `ListLocationPauses(userId: ID = null, from: Time, to: Time): [LocationPauseView]`:
  the pauses overlapping the range, at level `history` for another person, the
  owner's own otherwise. Same reach rules as `ListLocationVisits`.
- `ReportLocationView` gains `pause: LocationPauseView` (the pause in effect
  after the report, so a phone learns of a pause set elsewhere) and
  `pausedPointCount: Int` (positions dropped because they fell in a pause;
  visits so dropped count too, as `pausedVisitCount: Int`).
- `LocationEvents` sends an event when a pause starts, changes or ends, so open
  maps read again.
- `EraseLocationHistory` erases the pauses too (the pause in effect, if any,
  stays in effect: erasing does not resume).

Agent tool `location`, two actions:

- `pause` with optional `ended_at` (RFC 3339; omitted means until the person
  resumes). No card. Answers when it ends.
- `resume`: needs `call.Confirmed` like `forget_place` and `erase` do. The card
  says recording starts again now.
- `status`-style reads of another person's location (the existing actions) say
  "<name> has paused sharing where they are (until <time>)" and give no
  position.

CLI (`internal/cmd/location.go`):

- `teanode location pause [--for 1h30m | --until 2026-10-23T08:00]`, neither
  means until resumed; `--for` and `--until` together are refused.
- `teanode location resume`.
- The existing show of the person's own location prints "Paused until ..." or
  "Paused until you resume" when a pause is in effect; another person's prints
  "Location paused ...".
- `teanode location visits` lists the pauses in the range among the visits, as
  "paused 14:00 to 15:00".

## Plan of work

1. Server (one agent): migration, `database_location_pause.go` (create, change,
   end, get in effect, list overlapping, erase), filtering in the report path
   (points and visits measured inside any pause of the person are dropped and
   counted), ending the open visit at a pause's start, `LocationView.pause` and
   the hiding for viewers in `locationViewOf` (both `GetLocation` and
   `ListSharedLocations`, the latter with one query for all sharers),
   `ListLocationPauses`, events, erase, the person card if it shows location
   (`person_card*.go`), the journal (`location_journal.go`: a pause in the day
   is written as "paused from 14:00 to 15:00; where they were is not known"),
   the agent tool, the client and CLI with their parity tests, and docs
   (`docs/subsystems/location.md` a "Pausing" section, `docs/reference/command-line.md`).
2. Dashboard (one agent): on the location page, the person's own card has a
   Pause button with a menu (For an hour, Until tomorrow morning, Until I
   resume); while paused a notice "Recording paused until 15:00" with Resume
   (toast on success and failure). Another person's row reads "Location paused"
   (and "until 15:00"). The timeline shows a pause as its own row. A map that is
   open re-reads when a timed pause ends. Strings in en, zh, ja.
3. iPhone (one agent): `LocationRecorder` learns the pause from `GetLocation`
   for the person at launch and foreground and from each report answer; while a
   pause is in effect it keeps its low-power monitoring (visits, significant
   changes) but puts nothing in the outbox for an instant inside the pause and
   stops live updates. Pausing on the phone applies at once locally and sends
   `PauseLocation`, retrying until the server has it. `LocationSettingsView` gets
   a Pause section (menu with the three choices; while paused "Paused until
   15:00" and Resume). The location screen shows the pause; people who paused
   read "Location paused". Timeline rows for pauses. Demo data, `shoot` lines,
   String Catalog in three languages, TeaNodeKit model and tests.

## Validation

- Database tests: overlapping and in-effect queries, pausing while paused,
  resume, the 30-day bound, erase.
- GraphQL tests: a viewer at `current` and at `history` sees `pause` and no
  position while paused and the position again after; a report inside a pause
  stores nothing and counts it; a report straddling a pause keeps the outside
  part; an open visit ends at the pause's start; reach rules for
  `ListLocationPauses`; agent actor can pause; resume without confirmation is
  refused by the tool.
- CLI parity tests; web tests; TeaNodeKit tests on Linux; CI screenshots.
- On the server: pause from the dashboard, see the phone stop; resume from the
  CLI, see it start; a shared viewer's map shows "Location paused".

## Outcomes

(To be written when it ships.)
