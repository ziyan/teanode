# Contacts

A person's address book: the people they keep, edited in the dashboard and
kept in step with their phone and their desktop over CardDAV.

This is not the same thing as the addresses a mailbox learns from traffic.
Those live in `mailbox_contact`, are per mailbox, fill themselves in, and are
what the compose page completes from and the "sender is known" rule asks
about. They are a list of people who have corresponded. This is a list of
people somebody chose to keep, and nothing arrives in it by itself.

## What a contact is

A vCard. Not a set of columns that a vCard is rendered from — the card itself,
as the bytes this server holds. That decision is the reason most of the rest
of this document is short: a phone puts things on a card that this server has
never heard of, and a contact stored as columns loses them on the way through.
A photograph, a birthday, an address labelled with Apple's own
`X-ABLabel:_$!<Friend>!$_`, a grouped `item1.X-ABADR` — all of it survives an
edit made in a browser, because the edit is applied to the card rather than
replacing it.

Beside the card are a few columns — name, organization, the addresses and the
numbers — pulled out of it when it is written, so that listing and searching
never parse a vCard. Where the two disagree the card is right.

`internal/contacts` is the only package that knows the format. It parses,
writes, and computes the ETag; `internal/db/database_contact.go` stores;
`internal/dav` serves the protocol; `internal/api/v1api/apigraph/contact.go`
answers the dashboard, the command line and the agent.

## Writing a card out

The package writes vCards itself rather than using the library's encoder, and
the reasons are worth stating because each was a defect found by trying it.

The library never quotes a parameter value. A card carrying
`EMAIL;TYPE="work;main":ada@example.com` came back unquoted, and reading
*that* back gave a parameter called `MAIN` whose value was the address and an
`EMAIL` property with none. The address was destroyed on the second write —
which, for a contact edited in the dashboard, is the first edit.

It never folds, so a five-thousand-character note came out as one
five-thousand-character line, which some clients will not read. Lines are
folded at 75 octets, broken by where a character *ends*: eighteen emoji all
begin inside 75 octets and the last of them ends at 77.

And escaping has to match the library's decoder exactly, because that decoder
is what reads this back. It turns `\\` into a backslash, `\n` into a newline
and `\,` into a comma, and leaves `\;` alone — so a note reading
`call him\; he knows` gained a backslash on every trip through until the
asymmetry was handled here.

Two things are removed rather than written: control characters, because a
newline in a parameter value ends the line and everything after it becomes a
property of its own; and backslashes and quotation marks inside a parameter
value, because the decoder unquotes with Go's own rules and a value ending in
a backslash escapes the closing quote, at which point the whole property is
dropped without a word.

**What is stored is a fixed point.** Encode it, decode it, encode it again and
the bytes are the same. That is what lets the ETag be taken over the stored
text and still mean something: the version a listing names is the bytes a
fetch returns.

## The ETag, and what it promises

The ETag is a SHA-256 over the stored card, cut to 32 characters. A client
sends it back in `If-Match` when writing, and a write whose ETag is stale is
refused with `412`, so two devices editing the same person at once cannot
silently overwrite one another. A `DELETE` may carry one too, and does: a
device holding a stale copy must not destroy an edit it has never seen, and
there is no tombstone to recover from if it does.

Because the promise is that a listing and a fetch agree, every path that
serves a card serves the stored bytes: `GET`, and both of the REPORTs. The one
exception is a `PROPFIND` asking for `address-data`, which the library answers
by re-encoding; RFC 6352 confines `address-data` to the two REPORTs, so a
conforming client never asks.

## Signing in

HTTP Basic, because that is the only thing a contacts application can do: it
cannot follow a browser sign-in, hold a session cookie or use a passkey. The
username is one of a mailbox's addresses and the password one of that
mailbox's app passwords — the same credential, checked by the same function,
as a mail program signing in over IMAP. The account's own password is never
accepted. `docs/decisions/20260910-dav-signs-in-with-app-passwords.md` says
why.

Plain HTTP is refused. What counts as not-in-the-clear is deliberately wider
than the test used for the session cookie: `X-Forwarded-Proto` is taken from
anybody, because the only thing a forger gains is permission to send their own
password over their own plaintext connection. Being strict instead cost a
production outage — a server behind a CDN that had never needed to list its
proxies refused every request with a 403 that said nothing about why.

Guessing is bounded by the same limiter every other way of presenting this
credential counts against, keyed by where the request came from. It is looked
at *before* the password is checked and spent only when the check fails:
checking costs a bcrypt per app password on the mailbox, so a budget consulted
afterwards bounds nothing, and charging every request would throttle a phone
fetching five hundred cards rather than the guessing.

## Where things live

The layout is not this server's to choose. The protocol library decides what
kind of resource a URL names by counting path segments after its prefix, so:

    /dav                                        the service root
    /dav/{account}/                             the principal
    /dav/{account}/contacts/                    the home set
    /dav/{account}/contacts/{book}/             an address book
    /dav/{account}/contacts/{book}/{name}.vcf   a contact

An address book belongs to the **account**, not to a mailbox: somebody with
two mailboxes has one address book and reaches it through either.

`{name}` is the client's to choose, and clients disagree — iOS names a card
after its UID, a 36-character UUID. It is stored as the contact's identifier,
scoped to its address book, so two people's clients may use the same name.

Nothing under `/dav` may be answered with a redirect. An HTTP client turns a
redirect into a `GET`, so a redirected `PROPFIND` arrives as a `GET` and is
refused as an unsupported method — and the router this mounts on is built with
`StrictSlash(true)`, which would do exactly that. There is a test that fails if
anybody makes those routes redirectable again.

## Keeping in step, without sync-collection

There is no `sync-collection`, and that is a decision rather than an omission:
the library has no server side for it. Clients fall back to what every CardDAV
client must implement anyway — list the book with `PROPFIND Depth: 1`, compare
the ETags, and `REPORT` for the ones that changed. A deletion is discovered
because the href stops being listed, which is why there are no tombstones to
keep.

The cost is one listing per poll. For an address book of a few hundred
contacts that is a few tens of kilobytes. A book may hold 10,000 contacts, and
the ceiling is enforced when a contact is written rather than by cutting the
listing short — a listing a client cannot read completely is a listing that
tells a phone to forget the contacts it could not see.

Adding `sync-collection` later is purely additive: advertise the property and
answer the REPORT.

## Searching

`addressbook-query` is answered here rather than by the library, so that the
bytes served are the stored ones. The filter is applied, case-insensitively,
which is what the protocol's default collation asks for and what the
dashboard's own search does. A collation this server does not offer, an
unknown match type or an unknown filter test is a `400` — answering emptily
would tell a client enumerating by query that everybody had been deleted.

A client that says how many results it will take gets that many.

## Setting it up

A client given only a mail address looks at `/.well-known/carddav`, which
redirects to the mount. `/.well-known/caldav` answers `404`: calendars are not
served yet, and sending a calendar client to an address book would produce
something stranger than "not here".

A domain may also publish an SRV record at `_carddavs._tcp`, so that a phone
finds the server from a mail address alone. The domain page advises one,
naming the domain's **own** mail host and the port the HTTPS listener binds —
its own host rather than the server's name, because every row on that page has
to be a record its reader can go and create. A domain whose mail is addressed
to a name somebody else owns is advised nothing, and neither is a deployment
whose TLS is ended by something in front, because then there is no honest port
to name. Nothing breaks without it: it saves a person typing the server and
the port themselves.

**CardDAV cannot be served through a CDN that does not forward `PROPFIND` and
`REPORT`.** Amazon CloudFront, for one, cannot: its allowed-methods setting is
a fixed list of `GET, HEAD, POST, PUT, PATCH, OPTIONS, DELETE`, and anything
else is refused before it reaches the origin. A deployment behind such a thing
needs a name that reaches the server directly.

## The picture on a card

A phone puts a photograph on a contact and expects to see it again. It lives
on the card, base64 inside the text, written one of two ways depending on the
version the client speaks — `PHOTO;ENCODING=b;TYPE=JPEG:` on a version 3 card,
which is what iOS sends, and a `data:` URL on a version 4 one. Both are read.
A `PHOTO` naming a URL is not: that is somebody else's picture at somebody
else's address, and this server does not fetch things on a card's say-so.

It is served from an address of its own, `/api/v1/contacts/{id}/photo`, rather
than carried inside the listing: a card with a photograph on it is several
hundred kilobytes, and a page that drew a column of faces out of the listing
would cost megabytes. The response is cached against the card's own ETag, so a
browser that has one never asks for it twice, and the media type is this
server's to decide rather than the card's — a media type tells a browser how
to treat bytes, and that is not an instruction to take from a contact
somebody synchronized.

A picture is why the megabyte limit on a card matters in practice. One
photograph from a phone is around three hundred kilobytes; two would be close
to the limit, and a card over it is refused with `507`.

## Promoting a learned address

The learned list marks the addresses that are already contacts and offers to
keep the rest. Promoting one is an ordinary save — name and address into the
address book — rather than an action of its own: a contact kept from a learned
address is just a contact, and a second way in would be a second thing to keep
right. The agent does it the same way, by searching the learned addresses with
`contact_search` and saving with `contact_book`.

Nothing is promoted automatically. The whole point of the distinction is that
one list is what happened and the other is what somebody chose.

## Caveats

A contact is capped at a megabyte, which is generous for text and is not
generous for a card carrying photographs — see above. A larger one is refused
with `507`, which is the status that makes a client stop resending rather than
retry for ever.

A deletion is a row delete with no tombstone. That is deliberate — it is what
makes the listing mechanism above work without any extra machinery — but it
means a client that goes wrong can remove contacts, and the ordinary database
backup described in `docs/reference/deployment.md` is the way back.

Contacts are not written to the administrative audit log. A phone rewrites
them all day, and a row per edit would bury the log that exists to show what
an operator did to the server. An address book appearing or disappearing is
audited, because that changes the shape of an account.
