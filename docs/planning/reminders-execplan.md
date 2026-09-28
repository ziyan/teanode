# Reminders: a list beside the calendar that phones can sync

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

A person who adds this server to an iPhone as a CalDAV account gets their calendar, and cannot add a reminder to it: the Reminders app finds no list that takes reminders, because the server tells every client its one calendar keeps events only (`SupportedComponentSet: []string{ical.CompEvent}` in `internal/dav/caldav.go`). The server also refuses to make a second calendar.

After this plan every person has a reminders list beside their calendar. The Reminders app on a phone shows it and syncs it both ways: adding, editing, ticking off and deleting a reminder. The same reminders are on the dashboard's calendar page, on the command line (`teanode reminder list|add|edit|done|reopen|remove`), and in the agent's `reminder` tool ("remind me to call the dentist on Friday"), all through one set of GraphQL operations.

The word is reminder everywhere this program names things. The iCalendar standard calls one a `VTODO` component and the protocol names stay as the standard writes them, because they are an external contract. The agent's existing `todo` tool is something else, the task list a conversation keeps, and is not touched.

To see it working: on a phone, add the server as a CalDAV account (Settings, Calendar, Accounts, Add Account, Other, CalDAV), open Reminders, add "Buy stamps" due tomorrow to the list named Reminders, then run `teanode reminder list` and see it; tick it off on the phone and see it done in the list; add one from the dashboard and see it on the phone.

## Progress

- [x] (2026-09-28) Mapped calendars, CalDAV, the calendar tool, the CLI and the dashboard; wrote this plan.
- [x] (2026-09-28) Milestone 1: a reminders list per person, stored as a calendar of kind `reminders`, served over CalDAV as a collection of to-dos, with iOS-shaped requests tested.
- [x] (2026-09-28) Milestone 2: GraphQL operations, the `reminder` tool and `teanode reminder`, one set of documents shared by the CLI and the tool.
- [ ] Milestone 3: reminders on the dashboard's calendar page.
- [ ] Milestone 4: docs, the end-to-end task, deploy, and a check with a phone.

## Surprises & Discoveries

- Observation: the storage already expects items with no time. `calendar_object.starts_at` and `ends_at` are nullable, with the comment "a file may describe something with no time at all -- a to-do, or a journal entry".
  Evidence: the column comment in `internal/db/database_calendar.go`.
- Observation: four places take "the first calendar" to mean the events calendar: the CLI (`internal/cmd/calendar.go`, `calendars[0]`), the dashboard (`web/src/pages/calendar.tsx`, `ListCalendars?.[0]`), invitations arriving by mail (`internal/scheduling/scheduling.go`, `found[0]`) and the agent's calendar tool (the first calendar granted to it). Calendars are listed oldest first, so a reminders list made later would not take over, but only by accident.

## Decision Log

- Decision: a reminders list is a calendar row with `calendar_kind = 'reminders'`, and a reminder is a `calendar_object` row whose file holds a `VTODO`; not new tables.
  Rationale: everything a reminder needs from storage an event already has: the file kept as sent, its ETag, the conditional-write checks, the size and count limits, the per-person ownership and the agent's grant switch. A second set of tables would repeat all of it. The kind column is what the few event-only paths check.
  Date/Author: 2026-09-28.

- Decision: the word is reminder in every name this program chooses: `calendar_kind` value `reminders`, `calendar.ParseReminder`, the GraphQL operations, the `reminder` tool, `teanode reminder`, the dashboard. Only the iCalendar and CalDAV names (`VTODO`, `COMPLETED`, `DUE`) stay as the standards write them.
  Rationale: the person asked for it; the agent already has a `todo` tool for a different thing, and two words for one thing, or one word for two, is what this codebase avoids.
  Date/Author: 2026-09-28, at the person's request.

- Decision: finishing a reminder is "done" in this program's words (the verbs `done` and `reopen`, the field `isDone`), and `COMPLETED` only in the file.
  Rationale: the same verbs the ideas use, and not the `todo` tool's `complete`.
  Date/Author: 2026-09-28.

- Decision: each person gets exactly one reminders list, made the first time anything asks for it, beside their one calendar; clients still cannot create more.
  Rationale: the same rule the calendar keeps, for the same reason: a client offering to create lists would be offering something this server does not do. One list is what the phone needs to have somewhere to put a reminder.
  Date/Author: 2026-09-28.

- Decision: the places that took "the first calendar" ask for the events calendar by kind.
  Rationale: see Surprises; relying on creation order is how a later change silently moves invitations into the wrong list.
  Date/Author: 2026-09-28.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

Calendars live in three layers. `internal/models/calendar.go` has `Calendar` (a person's calendar: name, colour, time zone, week start, `AgentGranted`) and `CalendarObject` (one iCalendar file: `Data` is the file and is authoritative; `UID`, `ETag`, `Summary`, `StartsAt`, `Status` and others are pulled out of it for listing). `internal/db/database_calendar.go` stores them in the tables `calendar` and `calendar_object` (migration `0051_calendar.sql` and later ones), and occurrences of events in `calendar_occurrence`. `internal/calendar` reads and writes the files: `Parse` requires a `VEVENT`, `Build` writes one, `Indexed` works out occurrences.

CalDAV is served by `internal/dav` over the `github.com/emersion/go-webdav` library. `internal/dav/caldav.go` is the library's backend: listing calendars (making the person's first one if there is none), describing a calendar to a client (`describe`, including `SupportedComponentSet`), fetching, listing, putting and deleting objects. `internal/dav/calendar_report.go` answers the REPORT requests a client uses to query: `calendar-multiget`, `calendar-query` with its component and property filters, and free-busy. Paths are `/dav/{userId}/calendars/{calendarId}/{objectId}.ics`. There is no sync-collection; clients compare ETags from a listing, which is how iOS already syncs events here. Tests start a real server over a test database: `newWorld` in `internal/dav/dav_test.go`, and `caldav_test.go` for calendar requests.

The dashboard, the CLI and the agent reach calendars through GraphQL in `internal/api/v1api/apigraph/calendar.go` (`ListCalendars`, `ListCalendarEvents`, `SaveCalendarEvent`, ...). The CLI's documents are in `internal/client/calendar.go`, its commands in `internal/cmd/calendar.go`; the agent's calendar tools in `internal/agent/tools/calendar/calendar.go`; the page in `web/src/pages/calendar.tsx`. `docs/subsystems/calendar.md` describes all of it, including the rule that no door can do what another cannot.

The newest migration on main is `0114`; the ideas work (`docs/planning/agent-ideas-execplan.md`) takes `0115`, which already ran on the development server, so this plan adds `0116`.

## Plan of Work

Milestone 1, storage and CalDAV. Migration `0116_calendar_kind.sql` adds `calendar.calendar_kind varchar(20) not null default 'events'`. `models.Calendar` gains `CalendarKind` with the constants `CalendarEvents` and `CalendarReminders`. The db layer reads and writes it, and gains `EnsureCalendar(userId, kind, name, timezone)`, which returns the person's calendar of that kind, making it when there is none; the CalDAV listing and GraphQL use it for both kinds. `internal/calendar/reminder.go` adds `Reminder` (UID, summary, notes, due time or date, done and when, priority, the file) with `ParseReminder`, which reads the first `VTODO` and refuses a file without one, and `BuildReminder(previous, fields)`, which writes one, keeping everything else a phone put in the file. In `caldav.go`, `describe` offers `VTODO` for a reminders list and `VEVENT` for a calendar; `PutCalendarObject` reads the file with the parser for the list's kind, so an event put into the reminders list or a reminder into the calendar is refused with 403 (the status the protocol gives for a component the collection does not support), and stores a reminder with its due time as `StartsAt`, `COMPLETED` or `NEEDS-ACTION` as its status, and no occurrences. `calendar_report.go` filters on the component the collection holds, so a to-do query on the reminders list answers with its reminders, property filters such as `COMPLETED is-not-defined` included, and a time range matching on the due time (a reminder without one matches any range, as the standard says). Tests in `internal/dav/caldav_test.go` send what iOS sends: PROPFIND of the home set shows two collections with their component sets; PUT of a `VTODO`, GET back, the listing's ETag, a `calendar-query` for undone reminders, ticking it off with a PUT carrying `COMPLETED`, DELETE; and the refusals across kinds.

The four places that took the first calendar ask for the events calendar by kind: GraphQL `ListCalendars` returns `calendarKind`, and the CLI, the dashboard, the agent's calendar tool and `internal/scheduling` choose the one of kind `events`.

Milestone 2, one set of operations. In a new `internal/api/v1api/apigraph/reminder.go`: `ListReminders(isDone: Boolean)`, `SaveReminder(reminderId, title, notes, dueAt, dueOn, priority)`, `SetReminderDone(reminderId, isDone)`, `DeleteReminder(reminderId)`, each checking `calendar:use` and the person's own list, writing through `calendar.BuildReminder` so a reminder edited here keeps what a phone put in its file. `internal/client/reminder.go` holds the documents, validated by `TestClientDocumentsMatchTheSchema`; `internal/cmd/reminder.go` adds `teanode reminder list|add|edit|done|reopen|remove`; `internal/agent/tools/reminder/reminder.go` adds the `reminder` tool with the same verbs, sending the client's documents through `operator.Execute`, and reaching the list only when the person has granted it to the agent, as the calendar tool does.

Milestone 3, the dashboard. The calendar page gains a Reminders panel beside the grid on a wide screen and under it on a phone: undone reminders by due date, each a row with a checkbox that marks it done, the title and the due time; a field to add one; editing in the same dialog shape the events use; done ones behind a "Done" toggle.

Milestone 4: `docs/subsystems/calendar.md` gains the reminders list; `docs/reference/command-line.md` the command; `docs/evaluation/end-to-end-tasks.md` a task; deploy; the person adds a reminder from a phone.

## Concrete Steps

From the repository root; tests that touch the database need Docker.

    go test -mod=vendor ./internal/calendar/ ./internal/dav/ ./internal/db/ -run 'Reminder|Calendar'
    go test -mod=vendor ./internal/api/v1api/apigraph/ ./internal/agent/tools/reminder/ ./internal/cmd/
    cd web && npx tsc --noEmit -p .

## Validation and Acceptance

Milestone 1 is accepted when the CalDAV tests above pass and a PROPFIND of the calendar home shows two collections, one offering `VEVENT` and one `VTODO`. Milestone 2 when a reminder added with `teanode reminder add "Buy stamps" --due tomorrow` is listed by GraphQL, by the agent's `reminder` tool and by a CalDAV listing, and ticking it off in any one shows it done in the others. Milestone 3 when the calendar page shows, adds and ticks off reminders at 390 and 1400 pixels, light and dark. Milestone 4 when a reminder added on a phone appears on the dashboard, and one added on the dashboard appears on the phone.

## Idempotence and Recovery

The migration only adds a column with a default, and its reverse drops it after deleting the reminders lists (a reminders list without its column would read as a second calendar). `EnsureCalendar` makes a list only when there is none, so it can be called by every door on every request.

## Artifacts and Notes

A reminder as iOS writes it, which the tests use (invented):

    BEGIN:VCALENDAR
    VERSION:2.0
    PRODID:-//Example//Reminders//EN
    BEGIN:VTODO
    UID:reminder-1@example.com
    DTSTAMP:20260928T120000Z
    SUMMARY:Buy stamps
    DUE;VALUE=DATE:20260929
    STATUS:NEEDS-ACTION
    END:VTODO
    END:VCALENDAR

## Interfaces and Dependencies

In `internal/models/calendar.go`:

    type CalendarKind string   // "events", "reminders"

In `internal/calendar/reminder.go`:

    type Reminder struct {
        UID, Title, Notes string
        DueAt             time.Time   // zero when there is none
        IsDueDate         bool        // due on a day rather than at a time
        IsDone            bool
        DoneAt            time.Time
        Priority          int         // 0 none, 1 highest to 9 lowest, as the standard has it
        Data              []byte
    }
    func ParseReminder(data []byte) (*Reminder, error)
    func BuildReminder(previous []byte, fields *ReminderFields) (*Reminder, error)

No new libraries.
