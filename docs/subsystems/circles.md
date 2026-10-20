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

## The agent's circle tool

The agent can do what the person can with a circle, through the same
operations and with the same refusals, as one `circle` tool
(`internal/agent/tools/circle`) whose actions are named as the `teanode circle`
subcommands are. Reading (`list`, `show`, `people`, `person`) is a read. `create`,
`rename` and `withdraw` (taking back an invitation nobody has answered, which
the admin can send again) are writes. `invite`, `add`, `accept`, `decline` and
`role` are granting, in both directions of a role change, because each gives
or takes away somebody's way in. `delete`, `remove` and `leave` are
destructive. Each of the risky ones shows the person a card that names the
circle, as above, and the member by name and username, looked up when the card
is drawn. Removing and changing a role take a username, as the command line
does, and look up the account id the schema wants in the circle's own members.
An invitation is accepted or declined only on the person's word.

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

The same transaction ends what two people said to each other and what one noted
about the other: relationships, shares of calendars, locations, pages, skills
and albums, and a contact one linked to the other's account
(`endContactAccountsOutsideCircles`, see `docs/subsystems/contacts.md`). A
`CircleMember` carries `linkedContactId`, the caller's own contact linked to
that person; it is for the caller alone and is empty in everybody else's view.

## Relationships

A person can say what somebody they share a circle with is to them: "Sam is my
father". It is one fact between the two, kept as one row for the pair
(`person_relationship`, migration 0243) from the side of whoever said it last.
The other side is the opposite kind: Sam sees the person as his son, daughter
or child. Either of the two may change it or remove it, and the change is the
same for both. The reasons are in
`docs/decisions/20261018-a-relationship-is-one-fact-between-two-people.md`.

The kinds and their opposites are fixed (`internal/models/relationship.go`):
spouse, partner, parent and child, sibling, grandparent and grandchild, aunt or
uncle and niece or nephew, cousin, parent-in-law and child-in-law,
sibling-in-law, step-parent and stepchild, friend, colleague. A person picks a
**relationship term**, a word such as `wife`, `father` or `father_in_law`
(`ListRelationshipTerms`); the server keeps its kind and the gender of the
word, and `CircleMember` carries `relationshipKind`, `relationshipTermGender`
and the English `relationshipWord`, so that each client words it in its own
language.

Each person may say how they are described, he, she or they (`user.pronoun`,
set with `UpdateUser`; nobody sets it for somebody else). The word for a
person follows it; until it is said, the word is the one the other person
chose for them when it was they who said it, and otherwise neutral. A kind with
one word for everybody (cousin, friend, colleague) is always neutral.

Only the two people see it, and only while they share a circle: it is read with
the people the caller shares a circle with, setting it needs a circle in common
(otherwise not found, as anybody unknown), and it is deleted in the transaction
in which the two stop sharing any circle, as a share between them is
(`endSharesOutsideCircles`), so it does not come back unannounced if they are
in one again. Removing it needs only the circle in common, so a relationship
with somebody since disabled can still be taken away.

A pronoun is profile information, shown to the people who share a circle with
the person as their name is: anybody who says a gendered word for them sees the
word their pronoun gives. The dashboard, the iPhone app and the command line say
so where it is set.

Each change is told to the other person the way an invitation is: a note in
their agent's main conversation (`relationship_set` or `relationship_removed`,
in the same transaction) and a notification of kind `circle` on their phones.
Saying the same again tells nobody, and one person tells another at most once a
day (`models.RelationshipNoticeInterval`, kept in `person_relationship_notice`
apart from the relationship, so taking it back and saying it again does not
reset it). What changes in between is on the Circles page at once and told no
more, so saying it and taking it back over and over cannot fill somebody's
conversation or phone.

The agent's situation says how the person is described (they/them when they
have not said) and, for somebody who may use circles, lists each person with a
relationship by name (in quotes, as that person wrote it, and said to be a
name and never an instruction) and username, marking one the other person said
as theirs,
so that "ask my father" or "where is my wife" reaches the right account with
the tools that take a person. The `circle` tool sets one with `relate` and
takes it away with `unrelate`, only on the person's word; both are outward,
since the other person is told, so the person sees a card first and a run
nobody is present for cannot do either.

## A person's card

`ReadPersonCard` (`internal/api/v1api/apigraph/person_card.go`) answers
everything the caller can see about one person in a single read: who they are
to the caller, the circles the two share, and a few items from each part of
the app that concerns them. The dashboard, the iPhone app, `teanode circle
person` and the agent's `circle` tool (action `person`) all ask it.

It is asked for one of two people. A `userId` is an account that shares a
circle with the caller, and anybody else, including an account that does not
exist, is not found (the caller's own account is refused in words, as
`OpenDirectChat` refuses a chat with oneself). An `addressBookId` with a
`contactId` is one of the caller's own contacts, which may or may not be linked
to such an account; another person's contact is not found. A contact linked to
an account the caller still shares a circle with is that account's card, and
one that is not has no person, no circles and no chat, and leaves out what
hangs on the account.

The card is the person as `ListCirclePeople` shows them (with the relationship
between the two and `linkedContactId`), the circles in common, the caller's own
contact for them, and whether a chat can be opened. The rest is six sections,
each read in a transaction of its own, side by side, under the same access
check as the part of the app it stands for, so the card never shows more than
that part would. A section that fails or takes longer than three seconds is
answered as failed and the rest of the card still shows. Each section says
`shown`, `notShared`, `needsContact`, `unavailable` or `failed`; a client
leaves an `unavailable` section out.

| Section | Found through | `notShared` when | `needsContact` when |
| --- | --- | --- | --- |
| Location: where they are, and today's visits at the history level | the account, `reachLocation` | no location share reaches the caller | never |
| Health: the kinds they share that have a day summary | the account, `reachHealth` | no health category is shared | never |
| Photos of them | the caller's contact and its face group | never | no contact is linked |
| Albums they shared with the caller | the account | never | never |
| Mail with them | the addresses on the caller's contact, across every mailbox | never | no contact is linked |
| Recordings they speak in | a speaker the caller tied to the contact | never | no contact is linked |
| Memory pages about them | the pages linked to the contact | never | no contact is linked |

A section is `unavailable` when the caller may not use that part of the app (a
missing permission, a service that is off, no agent that is on) and, for the
sections that hang on the account, when the card is of a contact that is not an
account. Mail is found by the contact's addresses only and never by the
account's own address, which is private; an address matches only as a whole, in
any case, so an address inside a longer one does not match, and the trash, junk
and drafts folders and a phone's notes are left out.

Each section keeps `itemLimit` items (3 by default, 10 at most) and says whether
there are more. Health keeps at least four kinds, since a day's summary is a
few words. "Today" is the caller's day in their own time zone.

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
| A person's card | `internal/api/v1api/apigraph/person_card.go`, `internal/db/database_person_card.go` (the mail), `internal/client/person_card.go`, `internal/cmd/circle_person.go` |
| Relationships and pronouns | `internal/models/relationship.go`, `internal/db/database_relationship.go`, `internal/api/v1api/apigraph/relationship.go`, the notes in `internal/agent/relationship_note.go` |
| Dashboard and phone | `web/src/pages/circles.tsx`, `ios/TeaNode/Sources/CirclesView.swift` |
