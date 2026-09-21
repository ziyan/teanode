# The address book is the only list of people

- Status: accepted
- Date: 2026-09-13
- Changed: 2026-09-21 — the address book is still the only list of people,
  and the graph no longer writes to it. See "What the graph does not do"
  below.
- Deciders: the maintainer

## Context

From the first mailbox release, every address that wrote to a mailbox got a
row in `mailbox_contact`: the address, a name if a header offered one, a count
and a last-seen time. Nobody asked for any of it. Four things read it:

- the composer's address completion,
- the `sender-known` rule condition, which meant "has written more than once",
- an agent tool for searching those addresses, beside the one for the address
  book,
- the out-of-office reply's memory of whom it had already answered, so that a
  sender was left alone for a week after one reply.

Then the address book arrived — a real one, per account, synchronized over
CardDAV — and there were two lists of people with two meanings of "contact".
The Contacts page showed both, one above the other, with a paragraph
explaining that they were not the same thing. That paragraph is the tell: a
design that needs a paragraph on the page to say which list a word means has
one list too many.

## Decision

`mailbox_contact` is dropped. The address book is the only list of people the
server keeps, and the three things worth keeping read it:

- the composer completes from `ListContacts`,
- `sender-known` is `FindContactByAddress` over the account's own books,
- the agent has `contact_book` and nothing beside it.

The fourth is not replaced. "Answer each sender at most once a week" was the
only feature that needed a row per address, and a mail server keeping a record
of everyone who has ever written to it so that it can avoid writing back twice
is keeping the wrong thing. What actually stops two away messages talking to
each other is the hourly cap, which is now a count per mailbox per hour in
`mailbox_auto_reply` and needs no addresses at all.

In its place the out-of-office setting gains `SameDomainOnly`: answer only
people at the mailbox's own domains. An away message is written for
colleagues; the honest way to stop telling strangers that somebody is away
until the 30th is to not answer strangers, not to answer each of them once a
week.

## Consequences

- **A `sender-known` rule changes meaning.** It was true for the second
  message from anybody, including every newsletter that wrote twice. It is now
  true only for people the person keeps. Stricter, and it is what the words on
  the rule editor already said.
- Address completion offers fewer addresses: the people you keep, not everyone
  who has written. Somebody who completes from traffic has to keep the person
  first — one save, on the same page.
- The ledger is deleted, not migrated. It was learned from mail that has
  already been delivered, and nothing re-learns it; promoting all of it into
  the address book would import the newsletters too.
- `teanode mailbox contact`, the `ListMailboxContacts` API, and the agent's
  search over learned addresses are gone. The reverse migration recreates the
  table empty.

## What the graph does not do

Added 2026-09-21.

The decision above is about there being one list of people, and it stands. A
corollary was drawn from it that does not: that a page under `people/` and a
card for that person are two views of one thing, so the night that made the
page also made the card.

They are not two views of one thing. The graph files whoever turns up in a
commit log, a chat channel or a document. An address book is the people
somebody keeps. Those sets overlap and are not the same, and filling one from
the other fills it with the difference.

What that came to, measured: five hundred and nineteen cards, of which five
hundred and eighteen held a name and nothing else -- no address, no number,
nothing to reach anybody by. Among them a release bot and a build account,
because both commit. The one card with anything on it was the owner's own.

Two further faults followed from the same corollary. Merging two pages for
one person left both cards, because merging a page has never touched a card.
And a card outlived the page that made it, because nothing removes one: two
of those bots had no page left at all.

So the graph does not write to the address book. A card is made when a person
makes one, or when they promote somebody the mail has learned, which is what
the decision above already provided for. A page may still point at a card
where somebody has linked them, and what the graph knows about a person lives
on their page, where being wrong costs nothing more than a wrong page.
