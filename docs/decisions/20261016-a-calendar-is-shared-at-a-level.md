# A calendar is shared at a level, and a family calendar belongs to the circle

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Every person has one events calendar and one reminders list, and nobody
else can read either. Free-busy exists, as one piece of code for the
terminal, the agent and CalDAV (`calendar.FreeBusy`), and answers only for
the person themselves.

People who live or work together ask three different things of each
other's calendars. Colleagues want to know when somebody is free. A couple
wants to see what the other has on, by name. A household wants one
calendar everybody adds to: the dentist, the school play, whose turn it is
to drive.

## Decision

A person shares their calendar with a circle or with one person in a
circle they share, at one **share level**:

- `busy`: when they are busy, and nothing else; the same answer free-busy
  gives, from the same code.
- `titles`: the time, title and location of each event.
- `details`: everything in the event, including its description and its
  guests.
- `edit`: `details`, and the reader may add, change and remove events.

An event the owner marked private (`CLASS:PRIVATE` or
`CLASS:CONFIDENTIAL`) is `busy` to everybody else whatever the level, and
that rule lives beside the four judgements of what makes somebody busy.

A family circle may have a **circle calendar**: a calendar row owned by
the circle rather than by an account, which every member reads and
writes. It appears beside each member's own calendar on every surface and
over CalDAV, so a phone shows it as one more calendar.

The agent reads shared calendars at the level they were shared at, and
can answer "when are we all free on Saturday" with free-busy across the
people who shared with the person.

## Consequences

One table, `calendar_share`, and `calendar.circle_id` beside
`calendar.user_id`, exactly one of which is set. The places that take "the
person's calendars" learn to add the ones shared with them, each marked
with its owner and level.

A shared calendar over CalDAV advertises read-only privileges below
`edit`, so a phone refuses an edit before it is sent rather than failing
after.

Invitations by mail stay the owner's: changing an event with guests in
somebody else's calendar is refused, because the reply would go out from
the wrong person. A circle calendar's events have no organizer and send
no invitations in this design.
