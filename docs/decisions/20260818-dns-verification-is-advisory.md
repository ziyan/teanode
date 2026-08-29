# Domain DNS verification advises rather than gates

- Status: accepted
- Date: 2026-08-18
- Deciders: Claude, within the owner's instruction to keep existing behaviour

## Context

Inbound mail was rejected with `ErrMailBoxNotActivated` unless the recipient
domain's row had `VerifiedAt` set and its MX and CNAME record checks had
passed. That gate makes sense for a hosted service, where a user can add a
domain they do not own and must prove control before mail is accepted for it.

A self-hoster owns every domain they write into their own configuration file.
Refusing their mail because a periodic DNS check has not run yet is a bad first
hour.

## Decision

The DNS checker still runs periodically over the configured domains and its
findings drive the dashboard, which shows per domain exactly which records are
missing and what to publish. It no longer gates acceptance: mail for a
configured domain is accepted, and mail for an unconfigured one is refused with
`ErrMailBoxUnavailable`, which is the correct SMTP-level answer.

Verification state is held in memory and recomputed at startup. It is derived
from DNS and does not belong in the database.

## Consequences

- A domain works the moment it is configured and its MX record points here.
- An operator who misconfigures DNS gets accepted mail that then fails to
  forward, instead of refused mail. The dashboard has to make that state
  obvious, which is now the checker's whole job.
