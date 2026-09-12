# Calendar, CalDAV, and invitations by mail (outline)

An outline, to be written in full as an ExecPlan when its turn comes. It is
plan C of `20260910-personal-agents-roadmap.md` and builds on the DAV stack
plan B leaves behind.

## Purpose

A person's calendars, kept here, shown in the dashboard, synchronized over
CalDAV, and — because this is a mail server — taking part in invitations the
way mail programs expect: an invitation that arrives as mail becomes a
tentative event with Accept, Tentative and Decline in the reader; accepting
sends the reply as mail; an event made here with attendees sends the
invitation as mail.

## Shape

Tables `calendar`, `calendar_object` (the iCalendar text as stored, plus
parsed uid, summary, start, end, time zone and recurrence rule, and an
expanded-occurrence index for range queries), tombstones and change
counters as in B. Recurrence expansion through a vendored rrule library.
Free-busy from the occurrence index.

Scheduling is done the mail-server way first — iMIP. A delivered message
with a `text/calendar` part and `METHOD:REQUEST` is parsed by a job, never
in the SMTP path, into a tentative event in the person's default calendar;
the reader shows an invitation card whose buttons send `METHOD:REPLY`
through `internal/mailer`; creating an event with attendees sends
`METHOD:REQUEST`; `CANCEL` removes. CalDAV scheduling over the outbox
(RFC 6638) comes later. Dashboard: month, week and agenda views, an event
editor, the invitation card. A calendar becomes a source the agent can be
granted.

## Milestones

1. Schema, model, recurrence and free-busy; the API.
2. The calendar page and event editor.
3. CalDAV read, write and sync on the stack from B.
4. iMIP in: the invitation card and the reply.
5. iMIP out: invitations from events made here; cancellations.
6. The calendar as an agent source.
