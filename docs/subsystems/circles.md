# Circles

A server often has people on it who know each other: a household, some
friends, colleagues. A circle is the list of who belongs together. It decides
nothing by itself; the features that share something between people (chats,
calendars, where a person is, Health, passwords, pages of the agent's memory,
photo albums, recordings, finance accounts and lent skills) are shared with a
circle or with one person in one, and each of them asks this list who is in it. "What can be
shared" below lists every kind, to whom, at what levels, and where it is
managed.

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

**A circle that Health or a finance account is shared with.** Whoever joins a
circle reads what was shared with it, so bringing somebody into such a circle is
sharing that with them, and is the person's own tap, as sharing it is.
`InviteToCircle` and `AddCircleMember` refuse a command line token, an
operator signed in as the person and the agent's own operations while
`CircleHasSensitiveShare` says a health share or a finance account share
names the circle (`requireTapForSensitiveSharedCircle`, in
`apigraph/circle.go`); the dashboard and the iPhone app are taps. Answering an
invitation is the invited person's own, and is not refused.

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
or takes away somebody's way in. `delete`, `remove`, `leave` and `decline_share` (see
"Sharing, in one place") are destructive; `sharing` is a read. Each of the risky ones shows the person a card that names the
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
about the other: relationships, shares of calendars, locations, Health,
passwords, pages, skills, recordings and albums, and a contact one linked to the
other's account (`endContactAccountsOutsideCircles`, see
`docs/subsystems/contacts.md`). A `CircleMember` carries `linkedContactId`, the
caller's own contact linked to
that person; it is for the caller alone and is empty in everybody else's view.

What it ends is deleted, not paused. Leaving, being removed and a circle's
deletion end, for the person who goes (and for every member when the circle is
deleted), their direct shares with anybody they no longer share any circle with,
in both directions, and the shares they made to circles they are no longer in.
None of it comes back when they join again, or when the two meet in another
circle: they share again if they want to. What other people shared with a circle
is not deleted, since it is theirs; it stops reaching the person by the join on
`circle_member` and reaches them again if they rejoin. Deleting a circle also
deletes its own calendar, its albums, its address book and its chat, which belong
to it by foreign keys. The dialogs on the dashboard and in the iPhone app, the
command line's questions and the agent's cards say this before it is done.

A map that is open does not keep the last position of somebody it no longer
reaches: the three operations work out, among the circle's members, whose
location reached whom before and after (`watchLocationReach`), and send the
location event naming the people it stopped reaching once the transaction
commits, as unsharing does.

An operator who puts an account in a circle with `AddCircleMember` (which needs
only `user:manage`, and brings the account in with no invitation to answer)
tells the circle's admins: a `circle_member_added` note in the main conversation
of each admin's agent when it is on, written in the same transaction, and a
phone notification of the `circle` kind after it commits, and the account is
recorded in the audit log (`circle_member`). Accepting an invitation to a circle
that Health is shared with needs a person's own tap, as inviting and putting an
account in do, since whoever joins reads that Health.

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
contact for them, and whether a chat can be opened. The rest is seven sections,
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
| Finance: the accounts they share, each as its level shows it (name, kind, balance, a card's usage) | the account, `ListReachedFinanceAccounts` | no finance account is shared | never |
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

## Sharing, in one place

Each kind of thing is shared by its own table and managed on its own page, so
nothing showed a person everything they had given out or been given.
`ListSharing` does: for the caller, **what they share** and **what is shared
with them**, across calendar, location, Health, memory pages, photo albums,
recordings, finance accounts, skills and passwords. A row says the kind, the
thing (the calendar's or album's name, the page's name, the recording's title,
the finance account's name and the last digits of its number (`subjectMask`),
the skill), who it is shared with or by, the level or scope (a calendar's `busy`
to `edit`, a location's `current` or `history`, an album's `view` or
`contribute`, a recording's `summary` or `transcript`, a finance account's
`balance`, `spending` or `transactions`, the Health categories, the skill's
tools, and `quotes` for a page or `location` or `faceNames` for an album: a scope is
worded by the kind it belongs to, so a skill's tool called `quotes` is shown as it is),
and since when. A calendar shared at `busy` is listed to the person it is shared with
by the owner's name, not the calendar's title, as the calendar itself is shown to them.
A location level is worded for its reader: "Where they are now" on the
shared-with-you side, "Where I am now" on the side of the person who shares. A share of passwords is counted, never named, because an item's title is
sealed: "3 items", not which. A share through a circle names the circle; one
made to the person by name says so (`isDirect`).

It decides nothing about who may see what. Each kind is read through its own
listing and its own reach query (`reachingSharesQuery`,
`reachingLocationSharesQuery`, `photoAlbumReachQuery`, `passwordReachSQL`,
`reachingRecordingSharesQuery`, `reachingFinanceSharesQuery`, the page, skill
and Health listings), with the share's identifier added to the columns, so the
list cannot disagree with the check. A kind the caller may not use (no
`health:use`, no `agent:use` for recordings and finance) is left out of what is
shared with them, but not of what they share. A kind whose service is off (the
calendar service or the photos service) is left out of both lists. A person who
lost a permission still sees the shares they made, which their
readers cannot reach while it is gone, and can end each one (Stopping, below). A finance account reached by name and through a circle is
one row at the higher level, as the Finance page shows it, and the row is the
share by name, so it can be declined (the circle's share still reaches the
person afterwards). A share of a memory page that no longer exists (rows left
by deletes from before a deleted page ended its shares) is not listed to the
person it was for; its owner sees it as deleted (`isSubjectDeleted`) and can
stop it. What others share of
finance is not the agent's to read, so the person's agent is never shown a
finance row, and cannot decline one; its recording rows are listed as the
person's own are.

**Stopping** a share is the kind's own operation: `UnshareCalendar`,
`UnshareLocation`, `SetHealthShare` with no categories, `UnshareAgentPage`,
`UnshareSkill`, `RemovePhotoAlbumShare`, `UnshareRecording` and
`UnshareFinanceAccount`. The Sharing page and the iPhone screen call those. None of them asks for the kind's permission (`requireShareOwner`, and `requireCalendarShareEnder` for the calendar), so a person whose role lost it can still stop what they shared of any of these, and each deletes only rows the caller owns. Passwords are stopped on the Passwords page, item by item, and need passwords:use.

**Declining** is the other side: `DeclineShare(shareKind, shareId)` ends a share
that was made to the caller by name, and only that. The row must have the caller
as its grantee, so the owner, a stranger and a member of the circle a share went
to all find it not found (the same of a recording or a finance account, through
`DeclineRecordingShare` and `DeclineFinanceShare`). A share to a circle cannot be declined; it ends when
the person leaves the circle (the row says so). A finance account shared both by name
and through a circle at a higher level is one row at the higher level, still
declinable: declining ends the share by name and the circle's share stays, so
the row remains at the circle's level and then ends only when the person leaves
the circle. Passwords shared with a person
are removed by their owner, who must give the item a new key, so they cannot be
declined. The owner is not told. Declining an album takes out the photos the
person added to it, as taking the share back does.

**Being told.** When a share is made, or widened, the people it now reaches
are told, and the sharer is not. Widened is a higher calendar, location,
album, recording or finance level, a Health category added, a page's quotes newly shared, an album's
location newly included, or a skill's tools or limits raised. The person gets a
note in the main conversation of their agent when it is on (`share_received`,
written in the same transaction as the share: "Alex shared their calendar with
you (titles)") and a notification of kind `circle` on their phones, so no new
kind of notification was needed. A share to a circle tells every member of it
except the sharer; a person who may not use that kind is not told (a memory
page and a skill reach only people with `agent:use`), and nor is an account that
is disabled. A recording's
note does not name its title ("Alex shared a recording with you (summary and
transcript)"), which is the sharer's text and may summarize what other people
said, and a finance account's does not name the account ("Alex shared a
finance account with you (balance only)"), because the note goes into the
conversation of the person's agent, which has no part in either. The same
share of the same thing at the same level, by the same sharer, is not told to the same person
twice within ten minutes, so changing a level back and forth is one notice, not
one for each tap. That memory is the server's (`shareNoticeCache`), so a restart
forgets it. A notice is recorded as sent after the transaction commits, so one
that failed to write does not silence the next try, and the cache keeps at most
4096 entries, dropping the oldest when all are fresh. Passwords are not told: the owner's device seals a key for each
recipient, and the waiting list already shows who is still without one.

When somebody joins a circle (`AnswerCircleInvitation`, `AddCircleMember`), each
other member who has shared something with it is told once (not again when the
account was in the circle already), in one note
(`share_reached`): "Sam joined Family and can now see your calendar and
location", or "your calendar, location and finance" for three: the kinds are
joined with commas and an "and" before the last, with no comma in front of it,
on the server, the dashboard and the iPhone, and in the other languages with
their own separators. The kinds are the ones the circle's shares hold (including
recordings and finance accounts), less any the new member may not use, so
Health is not named for somebody without `health:use`, nor finance for somebody
without `agent:use`.
Skills (shared with one person) and passwords (waiting for a key) are not listed.

The command line is `teanode sharing`, with `teanode sharing decline <kind>
<share-id>`; the agent's circle tool has `sharing` and `decline_share` (not
`decline`, which answers an invitation); the dashboard page is `/sharing`, from
the Circles page and the account menu; the iPhone screen is Sharing, from
Circles.

## The circle's address book

A circle can have an address book of its own, the way it can have a calendar:
every member reads and edits it, an admin renames or deletes it, any member
makes it, and it goes when the circle goes (`addressbook.circle_id`, migration
0256; the circle's page offers "Make a family address book"). A person's own
address books are never shared. Access is a join on `circle_member`, so leaving
the circle ends it at once and nothing has to be cleaned up. How it is stored
and kept from the owner-only paths is in `contacts.md`.

## The circle chat

Every circle has one chat (`chat.circle_id`, migration 0194), made with the
circle, called what the circle is called, and whose members and roles follow
the circle's; it is changed only through the circle. A circle that predates
chats gets its chat the first time one of its members lists their chats. The
circle's going takes its chat and what was said in it. How a chat is stored
and read is in `conversations.md`.

## What can be shared

Each kind of share names its grantee in a `grantee_user_id` or a
`grantee_circle_id` column, and its rows are ended when a circle stops making
them possible (the last column below). A test lists the tables with such a
column from `information_schema` and fails when one has no cleanup in
`endSharesOutsideCircles` (`TestEveryKindOfShareIsEndedWhenACircleStopsMakingItPossible`),
so a new kind of share cannot ship without one.

| Kind | To whom | Levels | Where it is managed | Ended by |
| --- | --- | --- | --- | --- |
| Calendar (`calendar_share`) | one person, or a circle | `busy`, `titles`, `details`, `edit` | Share on the Calendar page; Share my calendar in the iPhone app's calendars sheet; `teanode calendar share`; the calendar tool's `share` | `endCalendarSharesOutsideCircles` |
| Where a person is (`location_share`) | one person, or a circle | `current`, `history` (the visits and paths, including those from before the share) | Sharing on the Location page and in the iPhone app; `teanode location share`; the location tool | `endLocationSharesOutsideCircles` |
| Health (`health_share`) | one person, or a circle | chosen categories, as day summaries; more categories only on a person's tap | Settings tab of the Health page; Health in the iPhone app's Settings; `teanode health share` | `endHealthSharesOutsideCircles` |
| Passwords (`password_share`) | one person, or a circle, item by item | the item, sealed for each person it reaches | Share on an item in Passwords, on the dashboard and in the app; `teanode passwords share` | `endPasswordSharesOutsideCircles` |
| Pages of the agent's memory (`agent_page_share`) | one person, or a circle | read only, a page and everything under it, with or without the quotes | Share on a page in Knowledge; `teanode agent memory share` | `endPageSharesOutsideCircles` |
| Photo albums (`photo_album_share`) | one person, or a circle | `view`, `contribute`; whether the location of the photos and the names of the faces in them are included | Share on an album in Photos; `teanode photo album share` | `endPhotoAlbumSharesOutsideCircles` |
| Recordings (`recording_share`) | one person, or a circle, a recording at a time | `summary` (title, date, length, summary, the speaker names the owner confirmed), `transcript` (and the words, with the owner's corrections); never the audio | Share on a recording in Recordings; `teanode recording share`; the recording tool | `endRecordingSharesOutsideCircles` |
| Finance accounts (`finance_account_share`) | one person, or a circle, an account at a time | `balance`, `spending`, `transactions`; starting or widening a share only on a person's tap | Sharing panel of the Finance tab of the agent page; Finance in the iPhone app; `teanode finance share`; not the agent | `endFinanceSharesOutsideCircles` |
| Lent skills (`skill_share`) | one person | named tools of a skill, approved call by call | Skills on the agent page; `teanode agent skill share` | `endSkillSharesOutsideCircles` |

Five things belong to the circle itself and are not shares: its chat, its
calendar, its albums, its address book (every member reads and edits it, an
admin renames or deletes it, and it has no grantee column, so the cleanup above
has nothing to end: leaving the circle is what takes it away), and the list of
members. The circle's admins manage them,
and they go with the circle. The admins can also rename and delete the circle's
calendar (`SaveCalendar`, `DeleteCircleCalendar`).

Whether somebody is reached is decided when they read, and requires more than
the circle: the owner's and the reader's accounts enabled, and, for calendars,
location, Health, passwords, recordings and finance, the permission of the
feature (`calendar:use`, `location:use`, `health:use`, `passwords:use`,
`agent:use` for recordings and finance) for both. An
owner whose role lost the permission can still list and end what they shared.

## Where the code is

| What | Where |
| --- | --- |
| Tables and queries | `internal/db/migrations/0193_circle.sql`, `internal/db/database_circle.go` |
| The circle's address book | `internal/db/database_contact.go` (`AddressBookAccess`, `CreateCircleAddressBook`, `ListReachableAddressBooks`), `internal/addressbook`, `internal/api/v1api/apigraph/contact.go` |
| The circle chat | `internal/db/database_chat.go` (`ensureCircleChat`, `leaveCircleChats`), called from the circle queries |
| Types, kinds, roles | `internal/models/circle.go` |
| The permission `circle:use` | `internal/models/permission.go`; held by the Administrator, Operator and Member roles |
| The operations | `internal/api/v1api/apigraph/circle.go`; the helpers `requireCirclePerson`, `requireCircleMember` and `requireCircleAdmin` are in `authorize.go` |
| The invitation note and phone notification, and the note told to admins when an operator puts an account in | `internal/agent/circle_invitation.go` |
| Everything shared, declining, and the notes about shares | `internal/api/v1api/apigraph/sharing.go`, `sharing_notice.go`, `internal/db/database_sharing.go`, `internal/models/sharing.go`, `internal/agent/sharing_note.go`; `internal/cmd/sharing.go`, `internal/client/sharing.go` |
| The cleanup when a circle stops making a share possible | `endSharesOutsideCircles` in `internal/db/database_circle.go`, which calls `endCalendarSharesOutsideCircles` (`database_calendar_share.go`), `endLocationSharesOutsideCircles` (`database_location_share.go`), `endHealthSharesOutsideCircles` (`database_health_share.go`), `endPasswordSharesOutsideCircles` (`database_passwords.go`), `endPageSharesOutsideCircles` (`database_page_share.go`), `endSkillSharesOutsideCircles` (`database_skill_share.go`), `endPhotoAlbumSharesOutsideCircles` (`database_photo_share.go`), `endRecordingSharesOutsideCircles` (`database_recording_share.go`), `endFinanceSharesOutsideCircles` (`database_finance_share.go`), and the relationship and contact link cleanups |
| The share tables | `calendar_share` (`0195`), `agent_page_share` (`0196`), `skill_share` (`0197`), `photo_album_share` (`0224`), `location_share` (`0240`), `health_share` (`0245`), `password_share` (`0251`), `finance_account_share` (`0254`), `recording_share` (`0255`) in `internal/db/migrations/` |
| The live map told when a circle change ends a location share | `watchLocationReach` in `internal/api/v1api/apigraph/location_share.go`, called from `circle.go` |
| Command line | `internal/cmd/circle.go`, `internal/client/circle.go`; `--circle` on `teanode user create` is in `internal/cmd/user.go` |
| The agent's tool | `internal/agent/tools/circle` |
| A person's card | `internal/api/v1api/apigraph/person_card.go`, `internal/db/database_person_card.go` (the mail), `internal/client/person_card.go`, `internal/cmd/circle_person.go` |
| Relationships and pronouns | `internal/models/relationship.go`, `internal/db/database_relationship.go`, `internal/api/v1api/apigraph/relationship.go`, the notes in `internal/agent/relationship_note.go` |
| Dashboard and phone | `web/src/pages/circles.tsx`, `ios/TeaNode/Sources/CirclesView.swift`; the Sharing page `web/src/pages/sharing.tsx`, the screen `ios/TeaNode/Sources/SharingView.swift` |
