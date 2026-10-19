# Health is shared by category, as day summaries, and no agent reads another person's Health

- Status: proposed
- Date: 2026-10-18
- Deciders: the-owner

## Context

The iPhone app copies a person's Apple Health data to their server. Families
want some of it: whether somebody slept, how much they walked. Health data is
among the most private a person has, and Apple's rules for HealthKit apps allow
sharing it with others only with the person's permission, and with an AI only
with explicit permission.

Location is shared at a level (`20261017-a-location-is-shared-at-a-level.md`),
with one grantee per share, a user or a circle. Health has no natural levels;
it has kinds, which the Health app groups into categories a person already
knows (Activity, Heart, Sleep).

## Decision

A person shares Health with a circle, or with one person in a circle they
share, by choosing Health categories. The people it reaches see the day
summaries of the types in those categories and nothing else: no readings, no
routes, no characteristics. One share per grantee holds the list of categories;
an empty list is no share. Leaving the circle ends the share. Anybody a share
does not reach gets "not found".

The agent reads only the person's own Health, and only after the person turns
on "Let the agent read Health data". It never reads what others share with the
person, and it only reads: erasing, sharing and the switch are the person's own
to do, because each gives or takes back consent the agent cannot give for them.
It reads no routes, which say where somebody runs.

## Consequences

- A family sees "slept 7 hours, walked 9,000 steps", not a heart rate series.
- The person who shares decides what reaches a model's provider, for their own
  data only. A viewer's agent cannot pass a sharer's data to the viewer's
  provider, and text a sharer controls never reaches the viewer's agent.
- Sharing more finely (one type, readings) would need a new choice in this
  record.
