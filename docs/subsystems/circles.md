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
row `FOR UPDATE` first so that two changes at once take turns. Every such change,
and the creation of a share between two people, first takes one PostgreSQL
advisory lock for the rest of the transaction (`circleMembershipLockKey`):
changes are rare, and a total order is what lets the cleanup of one see the
other. Leaving two circles at once used to keep a share and a group chat between
two people who then shared no circle, because each looked at the circle the
other was just leaving. Deleting an
account applies the same rules: it leaves each circle, and if it was the only
admin the member who joined first becomes one.

Deleting a circle, or its last member leaving, does for each member what leaving
does, in the same transaction: they leave the circle's chat first (so their
agents stop reading it), and once the circle is gone, every group chat in which
they now share a circle with none of the others.

One person makes at most 20 circles (those they made that still exist) and makes
at most 30 calls that invite somebody an hour (`models.CirclesMadeMost`,
`models.CircleInvitationsPerHourMost`). The refusal says which limit it is. Every
call counts, in `circle_invitation_send` (a row for the person and the time), before
anything about the person invited is looked at: a call that stores nothing (an
unknown username, a person who may not use circles) is counted the same and still
answered `true`, so the limit cannot be used to tell a username that exists from
one that does not, and deleting the circle the invitations were to does not give
the allowance back, because the rows do not belong to the circle. A person's rows
older than a day are removed by their next call.

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
can choose not to take like any other kind. The phones hang from the person's
agent row whether or not the agent is on, and the notification goes through the
phone notifier chats use, which needs no worker, so a person whose agent is off,
or a server with no model, is told all the same; only the note needs the agent
on. Pressing the notification on the iPhone opens the Circles screen, and it has
an Open button and no reply field.

**Names are not unique.** Two circles can be called the same, and a circle can
have the name of somebody's username. Wherever a person or an agent names a
circle by its name (`teanode calendar share` and `agent memory share`, the
`with` of the calendar and memory tools, the `circle` of the calendar tool's
`add`), a name that more than one circle has is refused with the ids and member
counts of the circles it could be (the same words on every surface,
`models.ResolveShareTarget`); an id is never ambiguous. A word that is the
username of somebody the person may share with is that person, even when a
circle has the same name, so whoever names a circle cannot take a share meant
for somebody; that circle is still reached by its id. The reverse is left as it
is: a word that is the username of somebody the person cannot share with (they
share no circle) is taken for a circle of that name if the person is in one,
since an answer that treated the word differently there would tell whether the
account exists. Every result, command line output and card the agent asks the
person to approve says a circle as `the circle "Family" (4 people)`, with the
name in quotes (a circle's name is typed by whoever made it, kept on one line,
and must not read as part of the sentence) and how many people it is. The dashboard and the iPhone app say it in their
pickers as "Everyone in Family (4 people)", so two circles called the same can
be told apart there too.

An operator with `user:manage` can put an account in a circle without an
invitation (`AddCircleMember`), for an account made for somebody who will not
set it up themselves. That is the operator path, and it is offered where the
operator already is: on a circle's page, **Make an account for someone** makes
the account (`CreateUser`) and then puts it in; the Users tab has **Add to a
circle** on each account; `teanode user create --circle <circle-id>` does both
from the command line; and the agent's `circle` tool has an `add` action for
somebody who manages accounts. None of them is a new operation. The account is
made first, so one that could not be put in is kept, and trying again adds it
rather than meeting "taken".

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
through it: the other members' pictures and the list of people, and the chats
that were theirs because of it. In the same transaction that changes the
circle, the person leaves the circle's chat and every group chat in which they
now share a circle with none of the others; a direct chat stays readable by
both and takes no new message until they share a circle again.

## The circle chat

Every circle has one chat (`chat.circle_id`, migration 0194), made with the
circle, called what the circle is called, and whose members and roles follow
the circle's; it is changed only through the circle. A circle that predates
chats gets its chat the first time one of its members lists their chats. The
circle's going takes its chat and what was said in it. How a chat is stored
and read is in `conversations.md`.

## Where the code is

| What | Where |
| --- | --- |
| Tables and queries | `internal/db/migrations/0193_circle.sql`, `internal/db/database_circle.go` |
| The circle chat | `internal/db/database_chat.go` (`ensureCircleChat`, `leaveCircleChats`), called from the circle queries |
| Types, kinds, roles | `internal/models/circle.go` |
| The permission `circle:use` | `internal/models/permission.go`; held by the Administrator, Operator and Member roles |
| The operations | `internal/api/v1api/apigraph/circle.go`; the helpers `requireCirclePerson`, `requireCircleMember` and `requireCircleAdmin` are in `authorize.go` |
| The invitation note and phone notification | `internal/agent/circle_invitation.go` |
| Command line | `internal/cmd/circle.go`, `internal/client/circle.go`; `--circle` on `teanode user create` is in `internal/cmd/user.go` |
| The agent's tool | `internal/agent/tools/circle` |
| Dashboard and phone | `web/src/pages/circles.tsx`, `ios/TeaNode/Sources/CirclesView.swift` |
