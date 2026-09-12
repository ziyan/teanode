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
arranging the meeting will believe it.

## Invitations arriving

Delivery notes the message and does no more. Working out whether one carries an
invitation means fetching it from storage and decoding it, and that inside the
SMTP transaction is a slow disk turning into mail this server refuses after
having accepted it.

So a row is written for every delivered message and most come to nothing; those
are swept up after a couple of days, because a row per message ever delivered
is a second copy of the mail table nobody reads.

Four things are refused, and each is a way for a stranger with an address to
change what somebody believes about their own week:

- a message that did not prove where it came from is not an invitation;
- a request carrying a lower `SEQUENCE` than what is already held is an older
  copy arriving late, and mail is not ordered;
- a cancellation is honoured only from the organizer the event already names,
  and it marks the event rather than deleting it — being told a meeting is off
  is the useful part;
- a reply changes exactly one thing, what that person said. A reply carries the
  organizer's own words echoed back, and a program that trusted them would let
  an invitee rewrite the meeting by answering it. An answer from somebody never
  invited is refused rather than added to the guest list.

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

**The file name is the client's.** It is unique only within one collection, and
it is wide: a phone names a file after its UID, which is a 36-character UUID.

**The library version.** The WebDAV library asks for a 2024 revision of the
iCalendar library whose `RecurrenceSet` reads the exception dates where it means
to read the added ones, so `RDATE` is silently dropped. It is pinned past that,
and `TestADateAddedByHandHappens` is the only test that notices.
