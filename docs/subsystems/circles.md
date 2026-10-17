# Circles

A server often has people on it who know each other: a household, some
friends, colleagues. A circle is the list of who belongs together. It decides
nothing by itself; the features that share something between people (chats,
calendars, memory, payments) are shared with a circle or with one person in
one, and each of them asks this list who is in it.

The reasons for the shape are in
`docs/decisions/20261016-people-on-one-server-meet-in-circles.md`.

## What a circle is

A circle has a name and a **circle kind**: `family`, `friends` or
`colleagues`. The kind is how screens describe it ("your family") and nothing
else. An account may be in several circles. People on other servers cannot be
in one.

Three tables hold it (migration `0193_circle.sql`): `circle`, `circle_member`
and `circle_invitation`. `internal/models/circle.go` defines the types and
`internal/db/database_circle.go` the queries, including `SharesCircle` and
`CircleUserIDs`, which the features built on circles authorize with.

## Roles

Each member has a **member role**, `admin` or `member`.

- An admin manages the circle: its name, who is invited, who is removed, who
  else is an admin, and deleting it.
- A member takes part, and may leave at any time.

A role never decides what somebody may see of anybody else's. That is always
the owner's choice, whatever their role.

A circle always keeps at least one admin. The last admin cannot leave, be
removed, or be made a member while others remain; the refusal says to make
somebody else an admin first, or delete the circle. A circle whose last member
leaves is deleted. Both rules are kept in `internal/db/database_circle.go`, in
the one transaction that changes who is in a circle, which takes the circle's
row `FOR UPDATE` first so that two changes at once take turns. Deleting an
account applies the same rules: it leaves each circle, and if it was the only
admin the member who joined first becomes one.

## Joining

Joining is by **invitation**, naming an exact username, made by an admin
(`InviteToCircle`). Nothing about the invited person reaches the circle until
they accept (`AnswerCircleInvitation`).

The answer to inviting is `true` whether or not the username is an account,
whether or not the person is in the circle already, and whether or not they may
use circles (`circle:use`). In those cases nothing is stored. Inviting is
therefore not a way to find out who has an account here.

An invitation expires thirty days after it was made; the check is made when it
is read, not by a sweep. Inviting somebody who has an invitation waiting
renews it, with the new role, and does not tell them a second time. An admin
may withdraw an invitation nobody has answered. Answered invitations stay as
rows, with the answer.

An invitation reaches the invited person in three places: the Circles page,
where it waits for an answer; a note in the main conversation of their agent,
when it is on (`circle_invited`, written in the same transaction as the
invitation); and a notification on their phones of kind `circle`, which a phone
can choose not to take like any other kind.

An operator with `user:manage` can put an account in a circle without an
invitation (`AddCircleMember`), for an account made for somebody who will not
set it up themselves.

## Who sees whom

A person sees other people only through the circles they share: name,
username and picture. `ListCirclePeople` is that list, everybody the caller
shares any circle with, once each. A picture is served only to the person,
to anyone who manages accounts, and to anyone who shares a circle with them.

A circle the caller is not in is not found, as is every other refusal about a
row the caller may not know exists. A member who is not an admin can see the
circle, so asking for something only an admin may do is answered in words.
Only an admin sees who is still being waited for.

Leaving a circle, or being removed from it, ends what the person could see
through it: today that is the other members' pictures and the list of people.

## Where the code is

| What | Where |
| --- | --- |
| Tables and queries | `internal/db/migrations/0193_circle.sql`, `internal/db/database_circle.go` |
| Types, kinds, roles | `internal/models/circle.go` |
| The permission `circle:use` | `internal/models/permission.go`; held by the Administrator, Operator and Member roles |
| The operations | `internal/api/v1api/apigraph/circle.go`; the helpers `requireCirclePerson`, `requireCircleMember` and `requireCircleAdmin` are in `authorize.go` |
| The invitation note and phone notification | `internal/agent/circle_invitation.go` |
| Command line | `internal/cmd/circle.go`, `internal/client/circle.go` |
| The agent's tool | `internal/agent/tools/circle` |
| Dashboard and phone | `web/src/pages/circles.tsx`, `ios/TeaNode/Sources/CirclesView.swift` |
