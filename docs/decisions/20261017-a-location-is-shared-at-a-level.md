# A location is shared at a level, and only as far as the owner chose

- Status: proposed
- Date: 2026-10-17
- Deciders: the-owner

## Context

A person's phone knows where they are all day. Households and friends want some
of it: whether the children got to school, where somebody is on the way home. A
person also does not want a family member, let alone a colleague, to be able to
reconstruct their week. A location is the most revealing thing the server keeps,
and what a viewer is shown cannot be taken back once it is seen.

Circles (`20261016-people-on-one-server-meet-in-circles.md`) say who belongs
together, and calendars (`20261016-a-calendar-is-shared-at-a-level.md`) already
share at a level chosen by the owner. Location needs the same shape with fewer
steps, and with a rule about the places the person has named, because a
viewer should learn "home" as a word, not as a street.

## Decision

A person shares their location with a circle, or with one person in a circle
they share, at one **share level**:

- `current`: where they are now and the place they are at.
- `history`: that, and their visits, the places those were at, and their paths.

Nothing else is shareable. There is no level that edits; the owner alone
records, renames, labels and erases.

Reach is decided at the moment of every call, in one function, from the shares
and the circles as they are then. Leaving a circle, being removed, losing
`location:use` or being disabled ends it at once. A viewer who falls short of the
level asked for is told "not found", the same as for a person who does not exist,
so that a refusal does not confirm that somebody exists or shares less.

A viewer sees a place as the owner's map draws it (name, label, address,
coordinates, kind), never the owner's own records about it (when they first went
there, how naming went, the radius, the OpenStreetMap object).

Arrivals and departures at places the owner named or labeled are told to the
people the location is shared with, on their phones, only when recent and after
the share was made. A place the server merely found a name for is never told.

The agent reads a shared location only for its own person, and never repeats
another person's into a chat, since its words are posted to everyone there.

## Consequences

One table, `location_share`, with the grantee a person or a circle. Every
operation that reads another person's location asks `GetLocationShareLevel`; a
new one that does not is a leak, and `TestLocationParity` and the reach tests
exist to catch it.

Two levels are fewer choices than a person may want (no "only at home and work",
no time limits). That is on purpose: every extra choice is a screen nobody reads,
and the person can stop sharing in one step.

A person shown only `current` has no way to see where somebody was; the tool and
the command line say so in words instead of answering "not found" for it.

The live map needs the server to decide, for each event, who may see it, so that
a stopped share stops the stream; the event carries only whose location changed,
never a coordinate.
