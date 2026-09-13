# The calendar

A person here has a calendar. They see it in the dashboard, their phone and
their laptop keep in step with it over CalDAV, and invitations arrive and leave
as mail. This says how those fit together and which decisions are load-bearing.

Its sibling is `contacts.md`, and the two share a mount and a way of signing
in. Where they differ is said here rather than assumed.

## The pieces

`internal/calendar` knows the iCalendar format and nothing else. It parses an
event, writes one out, expands a recurrence into the times it happens,
describes a time zone, and builds the three things that go in the post: an
invitation, a cancellation and a reply.

`internal/db` keeps three tables. `calendar` is the collection; `calendar_object`
is one iCalendar file, which is usually one event; `calendar_occurrence` is when
those events actually happen.

`internal/dav` serves it, beside the address book, under `/dav`.

`internal/scheduling` reads the invitations that arrive as mail.

`internal/api/v1api/apigraph/calendar.go` and `calendar_invitation.go` are what
the dashboard talks to, and `web/src/pages/calendar.tsx` and
`web/src/components/invitationCard.tsx` are what a person sees.

## The file is the event

An event is the iCalendar text, not a set of columns. The columns beside it --
summary, location, when it starts -- are pulled out when it is written so that
a listing never has to parse iCalendar, and where the two disagree the text is
right.

This is what lets a property this server has never heard of survive a round
trip. Phones invent them freely: travel time, a conferencing link, a colour, an
alarm. Somebody correcting a title in a browser must not throw those away, so
the file that is already there is what gets edited.

## The encoder is the library's, and that is the difference from contacts

The address book writes its own vCards, because the vendored encoder destroys a
quoted parameter. The equivalent was measured here before any of this was
written, against the same adversarial cases, and the iCalendar encoder survives
all of them — a parameter in quotes, a colon inside one, a newline written into
one, an escaped semicolon.

So this package does not replace it. What it adds is line folding: RFC 5545
asks for lines of at most 75 octets and the library writes a value of any
length as one line.

Encoding is a fixed point but not the identity: one pass normalises the text
and every pass after it changes nothing. So what is stored is the result of
that first pass, the ETag is taken over those bytes, and a fetch returns them.

## When things happen is an index, and it is derived

`calendar_occurrence` holds a row per occurrence within a horizon. It is
rebuilt from the text whenever an object is written, in the same transaction,
so it can never describe a version that is no longer there. Anything in it that
disagrees with the text is the index being wrong.

It exists because the two questions asked most often — "what is on this week"
and "when is this person busy" — are about a window, and answering them from
the text means decoding every event in the calendar and expanding its rule.
That is work proportional to the calendar rather than to the window.

An event that repeats for ever cannot be indexed for ever, so the horizon is a
decision: two years ahead, a year behind, measured from now. Both doors into a
calendar — the dashboard and CalDAV — index over the same one, because two
doors that disagreed would give a calendar whose contents depended on which one
last touched it. The horizon lives in `calendar.Indexed`.

**And it is moved.** The horizon is set when an event is written, so a standing
meeting saved today simply stopped appearing two years from now — everywhere
the index is read, while the event itself was still there to fetch. A repeat
starting past the horizon was never indexed at all. The scheduler extends the
repeats that are running out, nearest horizon first, a few at a time; there is
no hurry, since what is being fixed is a year away.

An event that cannot be reached in full — a repeat fine enough to fill the cap
on how many occurrences one file may contribute — records the last moment
actually written down, and is left alone for an hour afterwards. Recording the
window's end instead was a plain untruth: the index stopped five months out
while the row claimed two years, so nothing ever extended it and the event
disappeared with nothing to show for it. And the cap is spent on what is ahead
rather than on what has already happened, because worked out from a year back
it filled with last spring and had nothing in it for today.

An event that cannot be extended at all is moved on anyway, as though it had been.
Skipping it left it exactly where the query looks first, so it came back every
thirty seconds for ever — and because the question is asked of the whole table
with no account in it, twenty such rows stopped re-indexing for every account
on the server.

Which ones need it is asked of `calendar_object.indexed_until` — the horizon
each was last worked out to — and not of its furthest occurrence. Those are
different questions, and the second one has no answer for a series that has
already finished: it has no occurrence in the window at all, so it looks like
it is running out for ever. Asked that way the worker rewrote the same twenty
rows every thirty seconds and never reached the events it existed to extend.

Expansion is bounded in the work it costs, not only in what it returns. Asking
the rule library for a window builds the whole list before handing it back, so
a cap on the result was no cap at all: `FREQ=SECONDLY` over the stretch this
server indexes is ninety million moments built in memory, and one message
carrying that rule was enough to take the server down — from any sender, with
no account. Occurrences are pulled one at a time now, so the bound is on what
is done.

## Zones a machine has never heard of

The library resolves a `TZID` with `time.LoadLocation` and ignores the
`VTIMEZONE` beside it. That works for `Europe/Berlin` and fails for everything
Microsoft writes — `W. Europe Standard Time`, `Pacific Standard Time` — which
is the commonest inbound invitation there is.

It failed quietly, which was the bad part: the moment came back as the zero
time, the event was stored starting in the year one, and it then appeared in no
window anybody ever asked about — not the calendar page, not the agent, not
free-busy, not a phone — while a fetch of the event itself still returned it.

The fix is to make the file say something the library can read, **once**,
before anything else looks at it — a Windows name becomes the IANA name it
means, everywhere in the file. Everything downstream is then the ordinary path,
and daylight saving, exceptions and end dates are the library's business again.

The first attempt worked around the library at each place a time was read
instead: it expanded recurrences in wall clock and put the offset back one
occurrence at a time. That looked right and was wrong in four ways at once. It
rescanned the zone on every step of every rule, so a file with ten thousand
observances and a per-second rule took 53 seconds inside a database
transaction. It took the observance's literal date rather than the rule beside
it, so the offset was an hour out for the few days a year those disagree. And
`EXDATE` and `UNTIL` are written as real instants, so neither lined up with a
wall-clock expansion — a cancelled occurrence came back, and the last of a
series went missing. Renaming the zone once makes all four disappear.

A rename happens only when the target name is free and the zone it names keeps
the same offsets the file says its zone keeps — compared at midwinter and
midsummer rather than at the event's own moment, because a file's observances
carry literal dates where the rule beside them says "the last Sunday", so the
two disagree by an hour for a few days a year and the real zone is the one that
is right. Without that check a stale table entry would silently move every
occurrence of every event in that zone, with nothing to show for it.

Several excluded or added dates on one line are split onto lines of their own
first. The format allows the list; the library reads a value as a single
moment, so one such line made a recurrence unreadable in any zone at all.

A zone nobody can name at all — no mapping and no match — has its times
rewritten as the instants they stand for, using the offsets the file itself
gives. Such a series does not follow its zone through a change of offset, but
nobody can say which zone it is, and it is far better than the event not
existing.

## Recurrence, and the three ways to get it wrong

Every one of them shows somebody a meeting that is not happening.

An event that does not recur happens once, at its own time. A recurring event's
occurrences are what its rule generates, which does **not** include its own
start unless the start satisfies the rule — an event beginning on a Tuesday
with "every Monday" first happens on the following Monday. And an occurrence
lasts as long as the first one did: it is the length that repeats, not the end.

## Time zones are written out, not named

An event anchored to a zone carries a `VTIMEZONE` describing it: the offsets
themselves and the moments they change, taken from this machine's zone
database. A name means nothing to a client that keeps its own table under
different names, and an offset alone makes "every Monday at ten" nine o'clock
for half the year.

The moments are written one by one rather than as a rule, because a rule would
have to be inferred and would be wrong the next time a government moved its
clocks.

Describing a zone stops where the zone database stops. Past the last change Go
reports the bounds of the final stretch as the moment asked about rather than
as nothing, which reads as "there is a change, and it is now" — and a loop that
takes that at face value never advances. `TestAZoneIsNotDescribedPastTheEndOfWhatIsKnown`
is there because that hung.

An all-day event is a date with no zone at all, so that a birthday does not
move a day for somebody reading it from further west.

## What CalDAV does and does not do

The layout is forced: the library decides what kind of resource a URL names by
counting path segments after its prefix.

    /dav/{userId}/                                    the principal
    /dav/{userId}/calendars/                          the home set
    /dav/{userId}/calendars/{calendarId}/             a calendar
    /dav/{userId}/calendars/{calendarId}/{id}.ics     an event

A fetch and every report are answered from the stored bytes rather than by
handing the decoded form back to the library's encoder. The reason is narrower
than it was for contacts — the encoder is faithful — but it still holds: it does
not fold, and what is stored is folded, so what it would write is neither the
length advertised nor the bytes the ETag was taken over.

There is no `sync-collection`: the library has no server side for it. Clients
keep in step by listing the calendar and comparing ETags, and a deletion is
discovered when an href stops being listed. Nothing keeps tombstones.

Free-busy has no server side either, so it is intercepted before the library
sees the REPORT — the same place and the same way `addressbook-query` is. It is
also the one report that answers with a calendar rather than with XML.

**How a phone finds any of it.** `/.well-known/caldav` redirects to the mount,
and a domain may publish `_caldavs._tcp` beside the address book's
`_carddavs._tcp`; the domain page advises both. On a deployment reached at a
port of its own these matter more than they look. Discovery from a bare name
goes to port 443, and whatever answers there is what the phone finds — on the
deployment this was written for, a router's administration page on a
certificate for another name entirely. Without the SRV record the phone does
not fail; it finds the wrong thing, and the person types the host and port by
hand instead.

## What makes somebody busy

Four judgements, and each is a way to tell somebody a time is taken when it is
free. Something marked transparent does not make its owner busy; one they
declined does not; a cancelled one does not; an all-day one does not, because a
birthday or "Ada is in Berlin this week" is something to know about rather than
an appointment, and counting it makes a whole week look unbookable.

Somebody else declining says nothing about whether the owner is going, which is
why the attendee has to be matched against the addresses of the mailbox the
client signed in with.

Getting any of these wrong is worse than having no free-busy at all: whoever is
arranging the meeting will believe it. Which is also why no answer here is ever
cut short. The occurrence index was read with a limit meant for events, and one
repeating file contributes thousands of those, so a long window came back
truncated — in order, so it was always the far end that went missing, and the
person was reported free for the twenty-two months nobody had looked at. A
window with more in it than this server will describe is now **refused, out
loud**, and the asker is told to narrow it.

## Invitations arriving

Delivery notes the message and does no more. Working out whether one carries an
invitation means fetching it from storage and decoding it, and that inside the
SMTP transaction is a slow disk turning into mail this server refuses after
having accepted it.

So a row is written for every delivered message and most come to nothing; those
are swept up after a couple of days, because a row per message ever delivered
is a second copy of the mail table nobody reads.

**Every claim the file makes about who is speaking is checked against the
address the message came from**, which DMARC has proven is theirs to use. This
took two goes to get right. The first version compared the `ORGANIZER` line in
the arriving file against the `ORGANIZER` line in the stored one — which is the
sender marking their own homework, since both are written by whoever sent the
message. Anyone ever forwarded an invitation could copy the organizer's name
out of it and cancel the meeting for everybody.

So, in order:

**Speaking for somebody is not the same as being them.** An assistant may send for their
employer — that is what `SENT-BY` is for — but the file saying so is the
sender's own claim, and a claim is only worth what it is checked against.

This took four rounds to get right, and the first three failed the same way.
Written as "or the sender is the organizer of the arriving file", the check
asked whether the sender is who the sender says they are, which is always true.
Narrowed to "and the arriving file agrees with the held copy about whose event
it is", it still asked nothing: the held organizer's address is printed on the
invitation every guest received, so copying it in costs a forger nothing. Both
halves were the sender's to write.

The one thing a sender does not write is their own address, which DMARC aligned
to a domain they demonstrably hold. So `SENT-BY` is believed only **within that
domain**: an assistant at the organizer's own domain may move the meeting, and
a booking service that sends from a domain of its own is refused — it may ask
this person to a meeting under its own name, but it may not quietly move one it
did not call.

The same field defeated the check beside it. An invitation naming the recipient
as the organizer, with no guests at all, satisfied "is this addressed to them"
— so a stranger could plant an event that appeared to have been called by the
person it was planted on. The organizer is only believed to be them when the
message came from them.

Every one of these fails **closed**. Written the other way round — "if it
parses, and it names an organizer, and that is not the sender" — the check let
two whole populations through: an event this server can no longer read, which
is exactly what tightening the parser creates, and an event with no organizer,
which is every appointment somebody made for themselves. Both were then
replaceable by anyone who knew the identifier, which every other guest on the
original invitation does.

- a message that did not prove where it came from is not an invitation;
- an assistant or a booking system may send for an organizer, which is what
  `SENT-BY` is for, but only from an address the message has proven;
- an invitation is acted on only when its organizer *is* the sender;
- an event already held may be changed only by the organizer it already names,
  again matched against the sender — otherwise anybody the file reached could
  rewrite the time and the title, with nothing kept to recover from;
- a request carrying a lower `SEQUENCE` than what is held is an older copy
  arriving late, and mail is not ordered;
- a cancellation is honoured only from the organizer the event names, and it
  marks the event rather than deleting it — being told a meeting is off is the
  useful part;
- an invitation that does not ask this person is not put in their calendar,
  and the ceiling on how much a calendar holds applies here as at the other two
  doors — otherwise any sender who passes DMARC can write unlimited events,
  with text of their choosing, in front of somebody;
- a reply changes what *the sender* said and nothing else. A reply carries
  attendee lines, and applying all of them let one message mark several other
  people as not coming. An answer from somebody the event does not invite is
  refused rather than added to the guest list.

## Invitations leaving

Everybody on the guest list is sent one, from an address of the person's own
mailbox — an organizer at an address this server does not receive is a meeting
whose replies go nowhere.

Only the people newly asked when nothing else changed, so that correcting a typo
in the notes does not put an invitation in five mailboxes; everybody when the
meeting really moved. An event with guests that has changed increments its
`SEQUENCE`, because recipients' programs match a new copy to the one they hold
by identifier and sequence and ignore one that does not look newer.

A reply carries the answer and not the event: one attendee line, the person
answering. Sending the guest list back tells everybody who accepted to everybody
who replies.

Deleting sends a cancellation, and only when this person is the one who called
the meeting: deleting an event somebody else organized is leaving it, not
cancelling it.

## Things that will catch you

**The trailing slash.** The router uses `StrictSlash(true)`, which answers a
collection asked for without its slash with a redirect — and an HTTP client
turns a redirect into a `GET`, so a `PROPFIND` arrives as a `GET` and is
refused. The whole mount is one route with the boundary checked in Go, and
there is a test that fails if anybody makes it redirectable.

Which leaves the slash to be added here instead, and for a while it was not.
The library computes what kind of resource a path names by counting segments
but then compares the path against its own idea of the address, which ends in a
slash — so a slashless home set matched nothing and came back `207` with an
empty list. A client synchronizing reads that as every calendar in the account
having just been deleted. The test that covered the URL checked the status code
and not the body, so it passed throughout. A collection's address now gets its
slash before anything looks at it, and a file's address with one is a `404`.

**A version check is only as good as the row it was read from.** `If-Match` was
read outside any lock, so two devices holding the same version were both told
it was still theirs and both wrote — the check that exists to stop one device
overwriting another silently permitted it. Both the calendar and the address
book read the row `FOR UPDATE` before deciding.

**A filter that matches nothing is not the same as no filter.** Both were a nil
filter inside, and the caller read nil as "no filter", so a query for the
to-dos this server does not keep answered with every event in the calendar —
and a client asking for to-dos and events together had its time range thrown
away with the to-do filter. The property conditions in a query were read off
the wire and then never applied, which told a client using one as its only
filter that everything matched. Both are now carried out, the way the address
book already carried out its own.

**Each collection has its own permission.** `contacts:use` for the address
books, `calendar:use` for the calendars, chosen from the path. Checking only
the first meant an operator who took `calendar:use` away from a role took it
from the dashboard and the agent and not from the phones. The principal
advertises only the home sets the account may actually reach, because telling a
phone about a collection it is then refused sends it round a loop.

**The file name is the client's.** It is unique only within one collection, and
it is wide: a phone names a file after its UID, which is a 36-character UUID.

**The library version.** The WebDAV library asks for a 2024 revision of the
iCalendar library whose `RecurrenceSet` reads the exception dates where it means
to read the added ones, so `RDATE` is silently dropped. It is pinned past that,
and `TestADateAddedByHandHappens` is the only test that notices.
