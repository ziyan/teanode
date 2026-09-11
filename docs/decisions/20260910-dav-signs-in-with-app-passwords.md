# CardDAV and CalDAV clients sign in with a mailbox address and an app password

- Status: accepted
- Date: 2026-09-10
- Deciders: Ziyan Zhou

## Context

Address books and calendars are to be served to phones and desktop programs
over CardDAV and CalDAV. Those programs sign in with HTTP Basic
authentication: a username and a password, sent on every request. They
cannot follow a browser sign-in, use a passkey, or hold a session cookie.

This server already answers the same question for IMAP: a mail program
signs in with one of the mailbox's addresses as the username and an *app
password* made for that device as the password, revocable on its own, its
hash kept and the secret never shown again.

## Decision

The DAV stack signs in exactly as IMAP does: the username is one of a
mailbox's addresses, the password is one of that mailbox's app passwords,
over TLS only. The address book and the calendars a client sees are the
ones belonging to the account that owns that mailbox. A person makes one
app password per device and enters it in the mail program, the contacts app
and the calendar app alike.

## Consequences

One credential per device, one place to revoke it, one code path to keep
secure. The app-password page needs no change beyond saying that the
password also works for contacts and calendars.

An account with no mailbox has no DAV sign-in, because it has no address to
sign in as. That is acceptable: such an account has no mail either, and the
dashboard remains the way to reach its address book.

The account's password itself is never accepted over Basic authentication,
here or on IMAP.
