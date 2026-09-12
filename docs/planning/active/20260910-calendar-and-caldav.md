# Calendar, CalDAV, and invitations by mail

This ExecPlan is a living document. The sections `Progress`,
`Surprises & Discoveries`, `Decision Log` and `Outcomes & Retrospective` must
be kept up to date as work proceeds. It is written to the requirements in
`~/.claude/PLAN.md`; this repository has no PLANS.md of its own.

It is plan C of `docs/planning/active/20260910-personal-agents-roadmap.md`.
Plan A (personal agents) and plan B (contacts and CardDAV) are finished and
merged. Plan B matters here in a way plan A does not: it left behind the DAV
stack this builds on, and everything it learned the hard way is written down
in `docs/planning/active/20260910-contacts-and-carddav.md` and
`docs/subsystems/contacts.md`. You do not have to read them, because what is
needed is repeated below, but they are where the reasoning lives.

## Purpose / Big Picture

After this change a person using this server has a calendar. They see their
week in the dashboard and add an event to it. Their phone and their laptop
show the same events, because the calendar is served over CalDAV, signed in
to with the mail address and app password they already have. And — because
this is a mail server, which is the whole point of keeping a calendar here
rather than anywhere else — invitations work: a `.ics` invitation that arrives
as mail becomes an event in the reader with Accept, Tentative and Decline;
pressing one sends the reply as mail; an event created here with attendees
sends the invitation as mail.

You can see it working like this. With a development server running, sign in
and add an event on the calendar page; then, from a terminal:

    curl --user "$ADDRESS:$APP_PASSWORD" \
      -X REPORT -H 'Depth: 1' -H 'Content-Type: application/xml' \
      --data '<?xml version="1.0"?>
        <c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
          <d:prop><d:getetag/><c:calendar-data/></d:prop>
          <c:filter><c:comp-filter name="VCALENDAR">
            <c:comp-filter name="VEVENT">
              <c:time-range start="20260901T000000Z" end="20261001T000000Z"/>
            </c:comp-filter></c:comp-filter></c:filter>
        </c:calendar-query>' \
      https://localhost:10443/dav/USERID/calendars/default/

and see a 207 carrying the event you just typed, as iCalendar text.

Put the address and the app password in the environment rather than on the
command line; a credential typed as an argument is a credential in the shell's
history, and every example here is written the way it should be copied.

## Progress

- [x] (2026-09-12 22:30Z) Researched the libraries with a working spike before
      writing the plan; findings in `Surprises & Discoveries`.
- [x] (2026-09-12 22:45Z) Wrote this plan in full from the outline.
- [x] (2026-09-12) Milestone 1: migration 0051, the models, the database
      layer, and `internal/calendar` -- parsing, folding, recurrence, the
      builder and generated VTIMEZONEs. 34 tests in the package.
- [x] (2026-09-12) Milestone 2: the API and the calendar page -- month, week
      and agenda views, the event editor, and the strings in all three
      catalogues. `make test` 1220 green, `make lint-ci` clean.
- [x] (2026-09-12) Milestone 3: CalDAV read and write. The principal
      advertises both home sets, a fetch and both reports are answered from
      the stored bytes, and a calendar-query's time range is answered from the
      occurrence index. 11 tests in `internal/dav`; 1232 green overall.
- [x] (2026-09-12) Milestone 4: free-busy, answered by this server because
      the library has no answer for it at all. Transparent, declined,
      cancelled and all-day events do not make somebody busy; stretches that
      touch are merged and cut to the window; the answer says when and
      nothing else. 6 tests; 1238 green overall.
- [x] (2026-09-12) Milestone 5: invitations arriving as mail. Delivery notes
      the message and a worker reads it afterwards; the reader shows a card
      with Accept, Maybe and Decline, and pressing one marks the person's own
      copy and sends the answer. 12 tests in `internal/scheduling`, 6 more in
      `internal/calendar`; 1255 green overall.
- [x] (2026-09-12) Milestone 6: invitations and cancellations leaving as
      mail. A guest list in the editor; an invitation to each person newly
      asked, a fresh one to everybody when the meeting really changes, and a
      cancellation when it is called off by whoever called it. 8 more tests in
      `internal/calendar`; 1263 green overall.
- [x] (2026-09-12) Milestone 7: the calendar as something the agent knows --
      `calendar_agenda`, `calendar_free` and `calendar_add`, the last a write
      so the person is asked before anything goes in their diary. 9 tests;
      1272 green overall.

## Surprises & Discoveries

Everything here came out of a throwaway program written against
`github.com/emersion/go-ical` and `github.com/emersion/go-webdav` v0.7.0
before any of this plan's code existed. It is recorded because the equivalent
exercise on plan B was the difference between a week of work and a week of
rework, and because two of these findings invert what that plan learned.

- Observation: **the iCalendar library round-trips faithfully, and unlike the
  vCard library it does not need replacing.** This is the single most
  important finding here, because plan B's one wrong assumption was that a
  decoder and an encoder are inverses, and that assumption produced defects in
  five separate places. Here it holds. Every adversarial case that destroyed a
  vCard survives:

        quoted parameter       fixed=true  longest=70
        parameter with colon   fixed=true  longest=48
        escaped in text        fixed=true  longest=31
        utf8 value             fixed=true  longest=30
        newline in param       fixed=true  longest=47
        semicolon in text      fixed=true  longest=26

  In particular `ATTENDEE;CN="Hopper, Grace";PARTSTAT=ACCEPTED:...` comes back
  byte for byte — the quoted parameter that go-vcard silently unquoted and
  thereby destroyed an address. A `\n` written into a parameter stays literal
  and injects nothing. **Do not write another encoder for this plan.**

- Observation: encoding is a fixed point but not the identity. One pass
  normalises the text; a second pass changes nothing.
  Evidence: `fixed point: true`, `identity (bytes unchanged by one pass):
  false`. So the same rule as plan B applies: store what comes back from the
  first pass, take the ETag over that, and serve those bytes.

- Observation: **the library does not fold lines.** RFC 5545 section 3.1 asks
  for lines of at most 75 octets, as vCard does; a 300-character description
  came out as one 312-octet line.
  Evidence: `long value  fixed=true  longest=312  lines=11`.
  Most clients read a long line without complaint, which is why this is a
  conformance gap rather than a defect, but it is ours to close — see the
  Decision Log.

- Observation: **the CalDAV server answers only two REPORTs**, exactly as the
  CardDAV one did: `calendar-query` and `calendar-multiget`. There is no
  `free-busy-query` and no `sync-collection`.
  Evidence, from the library:

        return internal.HTTPErrorf(http.StatusBadRequest,
          "caldav: expected calendar-query or calendar-multiget element in REPORT request")

  Free-busy is a deliverable of this plan, so it is one this server answers
  itself — which is a thing plan B already built the shape for.

- Observation: the resource type is decided by counting path segments after
  the handler's prefix, identically to CardDAV, so the URL layout is not ours
  to choose and `Prefix` must be set. The function is the same one, in
  `caldav/server.go`:

        p := path.Clean(reqPath)
        p = strings.TrimPrefix(p, b.Prefix)
        if p == "/" { return resourceTypeRoot }
        return resourceType(len(strings.Split(p, "/")) - 1)

- Observation: **recurrence comes free and is correct.** go-ical already
  depends on `github.com/teambition/rrule-go` and exposes
  `Component.RecurrenceSet(loc)`; `caldav.Match` uses it for time-range
  filtering. It honours the time zone, the daylight-saving transition, COUNT,
  and EXDATE. Expanding a weekly event across the British autumn:

        first occurrence: 2026-09-15T10:00:00+01:00 to 2026-09-15T11:00:00+01:00
        2026-10-12 10:00 BST
        2026-11-23 10:00 GMT

  The wall-clock time holds at 10:00 across the change from BST to GMT, which
  is what a person means by "every Monday at ten". And EXDATE is applied:

        occurrences: 2026-09-14, 2026-09-28, 2026-10-05, 2026-10-12
        COUNT=5 gives 4 after one EXDATE
        the excluded day is absent: true

  The outline budgeted for vendoring and driving a recurrence library. That
  work is already done inside a dependency this plan needs anyway.

- Observation (2026-09-12, while implementing milestone 1): **the version of
  the iCalendar library that arrives by default has a defect in exactly this
  area, and it must be pinned past it.** The WebDAV library asks for a 2024
  revision, so that is what `go mod tidy` settles on unless a newer one is
  named. In it, `RecurrenceSet` reads the exception dates a second time where
  it means to read the added ones:

        for _, rdateProp := range comp.Props[PropExceptionDates] {

  so `RDATE` is ignored altogether and an occurrence somebody put in by hand
  is simply not there. The 2025 revision fixes it. `EXDATE` is unharmed --
  exclusion still wins over the date the bug re-adds -- so the exception test
  passes either way and only the added-date test notices. That asymmetry is
  the reason `TestADateAddedByHandHappens` exists and says so in its comment:
  putting the defect back into the vendored copy fails that test and no other.
  The version is pinned in `go.mod` as a direct dependency, which is also what
  stops `go mod tidy` from quietly walking it back to the one the WebDAV
  library asks for.

- Observation: a recurrence set does **not** include DTSTART when DTSTART does
  not itself satisfy the rule. An event starting Tuesday the 15th with
  `RRULE:FREQ=WEEKLY;BYDAY=MO` first occurs on Monday the 21st. That is
  correct per RFC 5545 and is a thing an occurrence index must get right,
  because the obvious implementation — "DTSTART, then the rule" — is wrong.
  It also means a test written carelessly proves nothing: the first EXDATE
  test here excluded a date the rule never generated, and passed without
  testing anything.

## Decision Log

- Decision: use `github.com/emersion/go-ical` for the format and
  `github.com/emersion/go-webdav/caldav` for the protocol, both vendored,
  and **do not write an encoder** the way plan B had to.
  Rationale: the spike shows the round trip is faithful for exactly the cases
  that broke the vCard library, including quoted parameters. Writing one
  anyway would be carrying plan B's scar tissue into a place that does not
  have the wound.
  Date/Author: 2026-09-12, Claude.

- Decision: fold lines on the way out, in this repository, without replacing
  the encoder.
  Rationale: RFC 5545 asks for 75 octets and the library writes whatever
  length the value is. Folding is a small, well-understood transformation over
  the encoder's output — break on a character boundary at or before the limit,
  continue with one space — and plan B's `writeFold` already exists to copy
  from, along with the lesson that cost it two rounds: measure by where a
  character *ends*, because eighteen emoji all begin within seventy-five
  octets and the last of them ends at seventy-seven.
  Date/Author: 2026-09-12, Claude.

- Decision: no `sync-collection`, as in plan B, and for the same reason: the
  library has no server side for it. Clients keep in step by listing the
  calendar and comparing ETags. No tombstones are kept, because a deletion is
  discovered when an href stops being listed.
  Date/Author: 2026-09-12, Claude.

- Decision: **answer `free-busy-query` ourselves**, in the same place and the
  same way plan B answers `addressbook-query` — intercept REPORT before the
  library sees it. Plan B's `internal/dav/report.go` is the shape to follow,
  including the two mistakes it made and fixed: an href is a URL reference and
  must be percent-decoded on the way in and encoded on the way out, and a
  filter the server cannot carry out is a 400 rather than an empty answer.
  Date/Author: 2026-09-12, Claude.

- Decision: keep an **occurrence index** — a row per expanded occurrence
  within a horizon — rather than expanding recurrence on every query.
  Rationale: the dashboard's month view and free-busy both ask "what is in
  this window", and answering that by decoding every event in the calendar and
  expanding its rule is work proportional to the calendar rather than to the
  window. The index is derived, never authoritative: it is rebuilt from the
  iCalendar text whenever an object is written, and anything that disagrees
  with the text is the index being wrong.
  Consequence: the horizon has to be a decision rather than an accident. An
  event recurring forever cannot be indexed forever. See "The occurrence
  index" below.
  Date/Author: 2026-09-12, Claude.

- Decision: scheduling is done by mail (iMIP) first, and CalDAV scheduling
  (RFC 6638, the scheduling outbox) is not in this plan.
  Rationale: this is a mail server, the people being invited are reachable by
  mail, and iMIP is what every mail client already understands. The outbox is
  additive later.
  Date/Author: 2026-09-12, from the outline.

- Decision: the URL layout is `/dav/{userId}/calendars/{calendarId}/{id}.ics`,
  beside the address books at `/dav/{userId}/contacts/...`.
  Rationale: the segment count is forced by the library, and `contacts` was
  already chosen as a home-set segment with `calendars` in mind.
  Date/Author: 2026-09-12, Claude.

## Outcomes & Retrospective

All seven milestones are done. `make test` is 1272 green, `make lint-ci` and
`make lint` clean.

**What the spike bought.** The one deliberate act at the start -- writing a
throwaway program against both libraries before writing any of this -- decided
three things that would each have been a rewrite to discover later: that the
iCalendar encoder needed no replacing, that it needed folding, and that
free-busy had no server side at all. Plan B learned its equivalent lesson by
shipping four defects and fixing them over five review rounds. An afternoon
against a week.

**The thing the spike did not catch.** It ran against the library version it
had asked for by name. The repository, left to itself, resolved a 2024 version
that the WebDAV library pins -- and that version reads the exception dates
where it means to read the added ones, so `RDATE` is silently dropped. It was
found while writing `TestADateAddedByHandHappens`, which is the only test in
the package that notices. The lesson is narrower than "pin your dependencies":
a spike proves something about the version the spike ran, and the repository
does not necessarily agree about which version that is.

**Where the real difficulty was, and it was not the protocol.** CalDAV itself
took one milestone and largely reused plan B's stack. The care went into three
places that have nothing to do with HTTP:

- *Recurrence.* Every way of getting it wrong shows somebody a meeting that is
  not happening. An event does not occur on its own start date unless its rule
  says that day; an occurrence lasts as long as the first one rather than
  ending when it did; ten o'clock stays ten o'clock when the clocks change.
- *Time zones.* Writing "every Monday at ten" as an offset from UTC makes it
  nine o'clock for half the year. Describing the zone properly meant generating
  a VTIMEZONE from this machine's own table, and that produced the one real
  hang in this work: past the end of the table Go reports the bounds of the
  last stretch as the moment asked about rather than as nothing, which reads as
  "there is a change, and it is now". The loop never advanced, and the bound
  never fired because it counted the observances it wrote and the step it was
  stuck on wrote none. An event repeating with no end is the ordinary way to
  reach that.
- *Who to believe.* An invitation is an instruction to write into somebody's
  calendar, and a cancellation an instruction to take something out. Each of
  the four refusals in milestone 5 -- unproven sender, stale sequence,
  cancellation from somebody who is not the organizer, answer from somebody
  never invited -- is a way for a stranger with an address to change what
  somebody believes about their own week.

**What was harder than expected.** Free-busy, which the outline treated as a
line item. Deciding what makes somebody busy is four separate judgements
(transparent, declined, cancelled, all-day), and getting any of them wrong is
worse than having no free-busy at all, because whoever is arranging the meeting
will believe it.

**What was easier.** Recurrence expansion and time-range matching both arrived
inside a dependency this plan needed anyway. The outline had budgeted a
milestone for vendoring and driving a recurrence library.

**Two small things worth keeping.** The naming check earned its place: this
work introduced `TimeZone` where the repository says `Timezone` sixty-seven
times, and the same thing having two names is exactly what the convention
exists to stop. And the authorization guard caught all six new resolvers at
once -- it reads the source rather than trusting the author, which is why it
works.

## Context and Orientation

This section assumes you know nothing about this repository.

**What this program is.** TeaNode is a mail server written in Go with a React
dashboard. `AGENTS.md` at the repository root is the orientation document. Go
code lives under `internal/`, one directory per subsystem; the dashboard under
`web/src/`.

**What plan B left you, which this plan mostly reuses.** `internal/dav/` is a
`web.Component` mounted at `/dav`, registered in `internal/cmd/server/run.go`.
It already does all of this, and none of it needs doing again:

- *Signing in.* HTTP Basic, because a calendar application cannot do anything
  else. The username is one of a mailbox's addresses and the password one of
  that mailbox's app passwords, checked by
  `access.AuthenticateAppPasswordWithID` — the same function IMAP uses. The
  account's own password is never accepted. Plain HTTP is refused unless the
  request never left the machine. The credential limiter is consulted *before*
  the password is checked and spent only on a failure, because checking costs
  a bcrypt per app password and a limiter consulted afterwards bounds nothing.
- *Routing.* One `PathPrefix("/dav")` route with the boundary checked in Go,
  because two routes would make `StrictSlash` redirect `/dav/` to `/dav` —
  and a redirect turns a `PROPFIND` into a `GET`, which is then refused as an
  unsupported method. There is a test that fails if anybody makes those routes
  redirectable again. **This applies to every route this plan adds.**
- *The principal and the home set.* `webdav.ServePrincipal` serves the
  principal; it is not the CardDAV handler's job and will not be the CalDAV
  handler's either. It already advertises the address-book home set, and this
  plan adds the calendar home set beside it.
- *Serving stored bytes.* A fetch and both REPORTs are answered from what is
  stored rather than by re-encoding, because the ETag's promise is that the
  version a listing names is the bytes a fetch returns.
- *Conditional writes.* `If-Match` with a stale ETag is 412; `If-None-Match`
  is honoured; a `DELETE` carrying `If-Match` is honoured, which needs the
  header threaded through the request context because the backend interface is
  handed only a path.

**How the database layer is written.** One file per area under `internal/db/`.
Each defines a GORM row type with `TableName()`, conversions to and from the
`models` type, and methods on `*transaction`, every one of which is also
declared in the interface in `internal/db/database_memory.go` — the compiler
tells you if you forget. Writes that belong in the administrative audit log go
through `applyMutation`; a person's own data does not, which is why contacts
are not audited and calendar objects will not be either.

**How migrations work.** One SQL file per change in `internal/db/migrations/`,
named `NNNN_short_name.sql`, each with a `NNNN_short_name.reverse.sql` beside
it. They are discovered by `//go:embed *.sql` and applied in name order. The
highest number in the tree at the time of writing is `0050`; this plan adds
`0051`.

**How the dashboard talks to the server.** A GraphQL-shaped endpoint under
`/api/`, built by reflection over Go interfaces in
`internal/api/v1api/apigraph/`. Declare a method on an interface with a doc
comment, implement it on `*graph`, and it is exposed. **An input type's Go
name gets `Input` appended when it reaches the schema**: a Go type called
`AddressInput` became `AddressInputInput` and broke every save from the
dashboard, because nothing validated the dashboard's documents. There is a
test now — `TestTheSchemaHasWhatTheDashboardNames` — and any new input type
this plan adds belongs in it.

**How mail arrives.** `internal/mx/exchange.go`, starting at `HandleEnvelope`.
`exchange_mailbox.go` is where a message is filed into a mailbox, and where
plan A enqueues the agent's work. **No work that can be slow or can fail
happens in the SMTP transaction**: it is queued and a worker does it. An
invitation arriving as mail is exactly that kind of work.

**How the agent's jobs work.** `internal/agent/` holds a job queue claimed
with `FOR UPDATE SKIP LOCKED`, so several instances can run. A job kind is a
constant and a handler. This plan adds one for invitations.

**Terms used here, in plain language.**

*iCalendar* is the file format a calendar entry is written in: lines like
`SUMMARY:Weekly sync` between `BEGIN:VEVENT` and `END:VEVENT`, wrapped in a
`BEGIN:VCALENDAR`. One file may hold several components — the event itself,
the time zone it is anchored to, alarms.

*CalDAV* is a way of keeping calendars in step over HTTP, defined in RFC 4791.
It is WebDAV, as CardDAV is, with rules about calendars. The methods beyond
ordinary HTTP that matter are `PROPFIND`, which asks for properties of a URL
and, with `Depth: 1`, of everything inside it; and `REPORT`, which runs a
named query — here `calendar-query`, which asks for the events in a window,
and `calendar-multiget`, which asks for named ones.

*RRULE* is the line that makes an event recur: `FREQ=WEEKLY;BYDAY=MO;COUNT=10`
is "every Monday, ten times". *EXDATE* removes one occurrence. An *occurrence*
is one instance of a recurring event.

*Free-busy* is the question "when is this person busy", answered without
saying what they are doing. It is what a calendar shows when somebody is
arranging a meeting.

*iMIP* is scheduling by mail, defined in RFC 6047: the invitation is a
`text/calendar` part with `METHOD:REQUEST`, and the answer is one with
`METHOD:REPLY`. It is how invitations have always worked between organisations
that share no calendar server.

## Plan of Work

Seven milestones. The first two build a calendar that works in the dashboard
with no protocol at all, as plan B did, so that the storage, the recurrence
and the editing can be seen working before anything is served. The third
serves it. The fourth answers free-busy. The fifth and sixth make invitations
work in each direction. The seventh connects it to the agent.

### Milestone 1 — the calendar itself, and what recurs

At the end of this milestone a calendar and its events exist in the database,
a recurring event can be asked "what are your occurrences between these two
dates", and all of it is covered by tests. Nothing is visible yet.

*Schema.* `internal/db/migrations/0051_calendar.sql` and its reverse. Three
tables. The calendar:

    CREATE TABLE "calendar" (
        "id"          varchar(32)  NOT NULL,
        "user_id"     varchar(32)  NOT NULL REFERENCES "user" ("id") ON DELETE CASCADE,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "name"        varchar(200) NOT NULL DEFAULT '',
        "description" text         NOT NULL DEFAULT '',
        "colour"      varchar(16)  NOT NULL DEFAULT '',
        "time_zone"   varchar(64)  NOT NULL DEFAULT '',
        PRIMARY KEY ("id")
    );
    CREATE INDEX "calendar_user" ON "calendar" ("user_id");

The object, which is one iCalendar file — usually one event, sometimes an
event and its overridden occurrences:

    CREATE TABLE "calendar_object" (
        "id"          varchar(255) NOT NULL,
        "calendar_id" varchar(32)  NOT NULL REFERENCES "calendar" ("id") ON DELETE CASCADE,
        "created_at"  timestamptz  NOT NULL,
        "modified_at" timestamptz  NOT NULL,
        "uid"         varchar(255) NOT NULL,
        "etag"        varchar(64)  NOT NULL,
        "data"        text         NOT NULL,
        "summary"     varchar(512) NOT NULL DEFAULT '',
        "location"    varchar(512) NOT NULL DEFAULT '',
        "starts_at"   timestamptz,
        "ends_at"     timestamptz,
        "all_day"     boolean      NOT NULL DEFAULT false,
        "recurring"   boolean      NOT NULL DEFAULT false,
        "status"      varchar(32)  NOT NULL DEFAULT '',
        PRIMARY KEY ("calendar_id", "id")
    );
    CREATE UNIQUE INDEX "calendar_object_uid" ON "calendar_object" ("calendar_id", "uid");
    CREATE INDEX "calendar_object_when" ON "calendar_object" ("calendar_id", "starts_at");

Note the key: `(calendar_id, id)`, both columns, and `id` is `varchar(255)`.
Plan B learned both of these the hard way. The identifier is the file name the
*client* chooses, so it is only unique within one collection — a global key
let one person's client stop everybody else's from using a name, and said so.
And iOS names a file after its UID, a 36-character UUID, against a column
32 wide: every contact an iPhone ever made was refused with a 500 until the
column was widened. Do not repeat either.

And the occurrence index, which is derived:

    CREATE TABLE "calendar_occurrence" (
        "calendar_id" varchar(32)  NOT NULL REFERENCES "calendar" ("id") ON DELETE CASCADE,
        "object_id"   varchar(255) NOT NULL,
        "starts_at"   timestamptz  NOT NULL,
        "ends_at"     timestamptz  NOT NULL,
        "all_day"     boolean      NOT NULL DEFAULT false,
        PRIMARY KEY ("calendar_id", "object_id", "starts_at"),
        FOREIGN KEY ("calendar_id", "object_id")
            REFERENCES "calendar_object" ("calendar_id", "id") ON DELETE CASCADE
    );
    CREATE INDEX "calendar_occurrence_window" ON "calendar_occurrence" ("calendar_id", "starts_at", "ends_at");

*The iCalendar package.* Add `internal/calendar/`, the only package that knows
the format — the counterpart of `internal/contacts/`. It needs:

    package calendar

    // Parse reads one iCalendar file and returns what to keep beside it.
    func Parse(data []byte) (*Parsed, error)

    // Encode writes one back out in the form this server stores, folded.
    func Encode(cal *ical.Calendar) ([]byte, error)

    // ETag names a version of an object.
    func ETag(data []byte) string

    // Occurrences are when an object happens, between two moments.
    func Occurrences(parsed *Parsed, from, until time.Time) ([]Occurrence, error)

    type Parsed struct {
        UID       string
        Summary   string
        Location  string
        StartsAt  time.Time
        EndsAt    time.Time
        AllDay    bool
        Recurring bool
        Status    string
        Organizer string
        Attendees []Attendee
        Method    string   // REQUEST, REPLY, CANCEL, or empty
        Data      []byte
    }

    type Occurrence struct {
        StartsAt time.Time
        EndsAt   time.Time
        AllDay   bool
    }

`Encode` folds; nothing else about the library's output is changed, because
the spike shows it does not need changing. Copy `writeFold` from
`internal/contacts/encode.go` and its lesson: break at the last character
boundary *at or before* the limit, measuring where a character ends.

`Occurrences` uses `Component.RecurrenceSet(loc)`. Get these right, because
each is a way to show somebody a meeting that is not happening:

- A non-recurring event has exactly one occurrence, its own time.
- A recurring event's set does **not** include DTSTART unless DTSTART
  satisfies the rule. Do not prepend it.
- EXDATE removes occurrences; the library does this, and a test must prove it
  with a date the rule actually generates.
- The duration of an occurrence is DTEND minus DTSTART applied to the
  occurrence's start, not DTEND itself.
- An all-day event is a DATE rather than a DATE-TIME and has no time zone. It
  must not be shifted by one.

*Database layer.* `internal/db/database_calendar.go`, with the row types, the
conversions, and methods on `*transaction`: `ListCalendars(userId)`,
`GetCalendar(id)`, `CreateCalendar`, `UpdateCalendar`, `DeleteCalendar`,
`ListCalendarObjects(calendarId)`, `GetCalendarObject(calendarId, id)`,
`GetCalendarObjectByUID(calendarId, uid)`, `PutCalendarObject`,
`DeleteCalendarObject(calendarId, id)`, and
`ListOccurrences(calendarId, from, until)`. Declare every one in
`internal/db/database_memory.go`.

`PutCalendarObject` writes the object and replaces its occurrence rows in the
same transaction, so the index cannot be left describing a previous version.

Acceptance: `make test` passes, including new tests in `internal/calendar/`
that put a real recurring event with a time zone, an RRULE and an EXDATE
through `Parse` and `Occurrences` and check the dates by hand; and tests in
`internal/db/` that store an object and read its occurrences back for a
window.

### Milestone 2 — the calendar page

At the end of this milestone a person opens the dashboard, sees a month, adds
an event, edits it, deletes it, and sees a recurring event on every week it
occurs.

*API.* `internal/api/v1api/apigraph/calendar.go`: `ListCalendars`,
`ListCalendarEvents(calendarId, from, until)` — answered from the occurrence
index — `GetCalendarEvent`, `SaveCalendarEvent`, `DeleteCalendarEvent`. As
with contacts, accept either whole iCalendar text or the filled-in fields, and
gate on the signed-in account owning the calendar. Give the account a calendar
named "Calendar" the first time it looks, rather than asking it to make one.

**Any input type added here must go into
`TestTheSchemaHasWhatTheDashboardNames`**, and must not be named `...Input` in
Go, because the generator appends that.

*Dashboard.* `web/src/pages/calendar.tsx`: a month grid, a week view and an
agenda list, with the view in the address so a link to a week is a link to
that week. An event editor in a `FormDialog`. Follow
`docs/coding/frontend-design.md`, reuse what the mailbox pages use, and note
two house rules learned in plan B: **every success and every error is reported
with a toast**, never inline text near the form; and a table keeps its shape
on a phone and scrolls sideways, which needs `min-width: max-content` on a
table whose cells do not wrap, or the columns past the edge are unreachable.

Every user-visible string needs an entry in all three of `web/src/i18n/en.ts`,
`zh.ts` and `ja.ts`; `make lint-ci` fails if they disagree.

Acceptance: `make dev`, sign in, open the calendar, add "Weekly sync" every
Monday for ten weeks, and see it on ten Mondays; edit one and see it change;
delete it and see it gone — each with a toast.

### Milestone 3 — a device shows the same calendar

At the end of this milestone somebody adds a CalDAV account on a phone and
sees the events from milestone 2, and an event added on the phone appears in
the dashboard.

Add the calendar home set to the principal that `internal/dav` already serves,
mount `caldav.Handler` under `/dav/{userId}/calendars/` with `Prefix: "/dav"`,
and implement `caldav.Backend` over the storage from milestone 1.

Everything plan B learned applies unchanged, and the code to copy is in
`internal/dav/`:

- Serve a fetch and both REPORTs from the stored bytes, not by re-encoding.
- Percent-decode an href on the way in and encode it on the way out. A client
  sends back exactly what a listing gave it, and a listing is a URL.
- `If-Match`, `If-None-Match` and a conditional `DELETE` are honoured.
- A UID already used by another object in the calendar is a 409, whether the
  object is new or replacing one.
- Database errors are wrapped so they never reach a client as a 500 body
  carrying an index name.
- A filter the server cannot carry out is a 400, not an empty 207.

`calendar-query` carries a time range, which is what a calendar client asks
on every sync. Answer it from the occurrence index rather than by decoding
every object.

Acceptance: on a phone, add a CalDAV account with the server's address, the
mail address and an app password, and see the events. From a terminal, the
`calendar-query` in the Purpose section returns them.

### Milestone 4 — free-busy

At the end of this milestone a calendar client asking when somebody is busy
gets an answer, without being told what they are doing.

The library does not answer `free-busy-query`, so intercept REPORT before it,
exactly as `internal/dav/report.go` does for `addressbook-query`. Answer from
the occurrence index: the busy periods are the occurrences overlapping the
requested window, merged where they touch, as a `VFREEBUSY` component.

Events with `TRANSP:TRANSPARENT` are not busy. A declined event is not busy.
Neither detail is optional: a free-busy that says somebody is busy when they
are not is worse than no free-busy at all, because a person arranging a
meeting will believe it.

Acceptance: a `free-busy-query` REPORT over a window containing one two-hour
event returns one `FREEBUSY` period of two hours, and says nothing about what
the event is.

### Milestone 5 — an invitation arrives

At the end of this milestone an invitation that arrives as mail is an event in
the reader, with Accept, Tentative and Decline, and pressing one sends the
reply.

*Reading it.* A delivered message may carry a `text/calendar` part with
`METHOD:REQUEST`. In `internal/mx/exchange_mailbox.go`, after the rules run,
enqueue a job — **never parse it in the SMTP transaction**. The job puts the
event in the person's default calendar with `PARTSTAT=NEEDS-ACTION`, and
records which message it came from.

*Showing it.* The reader shows an invitation card for a message whose event is
known: what, when, who else is invited, and the three buttons.

*Answering.* Pressing one sets the person's `PARTSTAT`, and sends a
`METHOD:REPLY` to the organizer through `internal/mailer`, containing only
that person's `ATTENDEE` line as RFC 6047 requires.

What to refuse, and why each matters: an invitation from a sender who fails
authentication is filed but not shown as an invitation, because an invitation
is an instruction to write something into somebody's calendar and a forged one
should not be; a `METHOD:REQUEST` for an event whose `SEQUENCE` is lower than
the one already held is stale and ignored; a `METHOD:CANCEL` removes the event
only if it comes from the organizer the event already names.

Acceptance: deliver a message carrying an invitation with
`internal/util/testmail`, see the card in the reader, press Accept, and see
the reply leave with `METHOD:REPLY` and the right `PARTSTAT`; and see the
event's status change in the calendar.

### Milestone 6 — an invitation leaves

At the end of this milestone an event created here with attendees invites
them, and changing or deleting it tells them.

Creating an event with attendees sends `METHOD:REQUEST` to each. Changing one
increments `SEQUENCE` and sends again. Deleting it sends `METHOD:CANCEL`.
Replies that arrive update the attendee's `PARTSTAT`, so the organiser sees
who has accepted.

Sending on somebody's behalf is the riskiest thing in this plan, so it obeys
the same limits the out-of-office does: it never replies to a list, never to a
`no-reply` address, and never to a message that failed authentication.

Acceptance: create an event with an attendee at an address on a second local
mailbox; see the invitation arrive there as mail; accept it; and see the
organiser's copy show that attendee as accepted.

### Milestone 7 — the calendar as something the agent knows

Plan A gave each person an agent that reaches the server through the same
operations the dashboard uses, with their permissions. Give it a `calendar`
tool: what is on, when somebody is free, and — as a write, which asks first —
put something in. A calendar becomes a source a person grants, beside their
mailboxes.

Acceptance: ask the agent what is on this week and get the week's events; ask
it to put something in and confirm the card it offers.

## Concrete Steps

Run everything from the root of this repository, which is the directory
holding `go.mod` and `Makefile`.

Before starting, confirm the tree is clean and the tests pass, so that any
later failure is yours:

    make test        # starts a PostgreSQL container; needs Docker
    git checkout -- vendor
    make lint-ci

`make test` prints a line like `DONE 1181 tests in 5.0s` and exits 0.

Vendor the libraries once, at the start of milestone 1:

    go get github.com/emersion/go-ical@latest
    go mod tidy
    go mod vendor
    go build -mod=vendor ./...

`go-webdav` is already vendored by plan B, and `rrule-go` arrives as a
dependency of `go-ical`.

After every milestone: `make test`, then `git checkout -- vendor` (the test
target leaves vendored files gofmt-rewritten), then `make lint-ci`, then
`make lint`. Commit with the files named explicitly; never `git add -A`.

## Validation and Acceptance

Each milestone states its own acceptance above, in terms of what a person can
do. Beyond those, these tests must exist and must fail before the change.

In `internal/calendar/`: a realistic event with a VTIMEZONE, an RRULE and an
EXDATE parses, re-encodes to a fixed point, and expands to the dates a person
would write down by hand; an all-day event does not move by a day or an hour;
an event whose DTSTART does not satisfy its own rule does not occur on its
DTSTART; a line longer than 75 octets is folded and unfolds to what it was; a
file that is not iCalendar is refused rather than stored.

In `internal/dav/`: a `calendar-query` with a time range returns the events in
it and no others; a `calendar-multiget` of an href exactly as listed returns
the object rather than a 404; a free-busy query reports the right busy periods
and no summaries; a `PUT` with a stale `If-Match` is 412; another account's
calendar is refused; and — the one that encodes a discovery — a `PROPFIND` on
a collection **without** a trailing slash is answered 207 rather than
redirected.

In `internal/mx/` or `internal/agent/`: an invitation arriving as mail becomes
an event; one from a sender that failed authentication does not; a stale
`SEQUENCE` is ignored; a `CANCEL` from somebody other than the organizer is
ignored.

The whole suite: `make test`, `make lint-ci`, `make lint`, all clean.

## Idempotence and Recovery

Every step is safe to repeat. The migration is additive: three new tables,
nothing existing touched, and the reverse drops exactly those three. The
occurrence index is derived and can be rebuilt from the iCalendar text at any
time; a milestone that gets it wrong is fixed by rebuilding it, not by
restoring anything.

The irreversible things are two, and both deserve care. Deleting a calendar
object deletes it, with no tombstone — the same decision as contacts, and the
database backup in `docs/reference/deployment.md` is the way back. And
milestone 6 sends mail on somebody's behalf, which cannot be unsent; develop
it against a second local mailbox, never against an address outside the
deployment.

## Artifacts and Notes

The spike is not checked in; it was a scratch program. Its output is quoted
throughout `Surprises & Discoveries`, and the two lines that matter most are:

    fixed point: true
    identity (bytes unchanged by one pass): false

and

    quoted parameter       fixed=true  longest=70

That second line is the difference between this plan and plan B. The same
shape of input destroyed an email address there and is preserved here.

## Interfaces and Dependencies

New dependency: `github.com/emersion/go-ical`, MIT, by the author of the IMAP
and WebDAV libraries already vendored, bringing
`github.com/teambition/rrule-go`. `github.com/emersion/go-webdav` is already
vendored and its `caldav` package is already present.

In `internal/calendar/`, define:

    func Parse(data []byte) (*Parsed, error)
    func Encode(cal *ical.Calendar) ([]byte, error)
    func ETag(data []byte) string
    func Occurrences(parsed *Parsed, from, until time.Time) ([]Occurrence, error)
    func FreeBusy(occurrences []Occurrence) []Period

In `internal/models/calendar.go`, define `Calendar`, `CalendarObject` and
`Occurrence`, plain types with no format knowledge.

In `internal/db/database_calendar.go`, define on `*transaction` and declare in
`internal/db/database_memory.go`:

    ListCalendars(userId string) ([]*models.Calendar, error)
    GetCalendar(calendarId string) (*models.Calendar, error)
    CreateCalendar(calendar *models.Calendar) (*models.Calendar, error)
    UpdateCalendar(calendar *models.Calendar) (*models.Calendar, error)
    DeleteCalendar(calendarId string) error
    ListCalendarObjects(calendarId string) ([]*models.CalendarObject, error)
    GetCalendarObject(calendarId, objectId string) (*models.CalendarObject, error)
    GetCalendarObjectByUID(calendarId, uid string) (*models.CalendarObject, error)
    PutCalendarObject(object *models.CalendarObject, occurrences []models.Occurrence) (*models.CalendarObject, error)
    DeleteCalendarObject(calendarId, objectId string) error
    ListOccurrences(calendarId string, from, until time.Time) ([]*models.Occurrence, error)

In `internal/dav/`, extend the existing component: add the calendar home set
to the principal, mount `caldav.Handler` with `Prefix: "/dav"`, implement
`caldav.Backend`, and extend the REPORT interception to `calendar-query`,
`calendar-multiget` and `free-busy-query`.

---

*Revision note (2026-09-12, Claude):* written in full from the outline that
previously occupied this file. The substantive departures are in the Decision
Log, and the largest is that this plan does **not** write its own encoder: the
spike shows the iCalendar library round-trips faithfully where the vCard
library did not, which was plan B's most expensive wrong assumption. Two
deliverables the outline treated as work turn out to be free — recurrence
expansion and time-range matching both arrive inside dependencies this plan
needs anyway — and one it treated as free is not: free-busy has no server-side
support and is ours to answer.
