# Contacts and CardDAV (outline)

An outline, to be written in full as an ExecPlan when its turn comes. It is
plan B of `20260910-personal-agents-roadmap.md`.

## Purpose

A person's address book, kept here, edited in the dashboard, and synchronized
to their phone and desktop over CardDAV. Today the mailbox learns contacts
from traffic for completion and for the "sender is known" rule; those stay,
as a separate source of suggestions, and "save to contacts" promotes one into
the address book proper.

## Context

`models.MailboxContact` and `TouchContact` in `internal/db/database_mailbox.go`
are the learned contacts. App passwords (`models.MailboxAppPassword`) are how
a mail program signs in over IMAP, and — per
`docs/decisions/20260910-dav-signs-in-with-app-passwords.md` — how a DAV
client will. The IMAP library already vendored has sibling packages for
WebDAV, vCard and iCalendar by the same author, which are the first choice;
the fallback is WebDAV over `net/http` by hand with the same parsers.

## Shape

Tables `addressbook` (one per account by default), `contact` (the vCard text
as stored, plus parsed name, emails and phones for search and completion),
a per-collection change counter and a tombstone table so `sync-collection`
works, ETags from a content hash. New package `internal/dav/` mounted at
`/dav/` in `internal/web`, with `/.well-known/carddav` and `/.well-known/caldav`,
principal and home-set discovery. Advisory `_carddavs._tcp` SRV checks in
`internal/dns`. `web/src/pages/mailboxContacts.tsx` becomes editable. The
address book becomes a source the agent can be granted; the agent's
`contact_*` tools read it.

## Milestones

1. Schema, model and API; the dashboard lists and edits contacts.
2. CardDAV read: a client subscribes and sees the address book.
3. CardDAV write and `sync-collection`; ETags; conflicts refused with 412.
4. Sign-in with app passwords and discovery; the DNS advisory records.
5. The address book as an agent source; learned contacts as suggestions.
