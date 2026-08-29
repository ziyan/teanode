# Reporting a security problem

Please do not open a public issue for anything exploitable. That includes
mail handling — SPF, DKIM, DMARC and ARC verification, header parsing, the
MIME reader — and authentication, session handling, certificate issuance and
the aggregation pipeline behind the dashboard's filters.

Report it through GitHub's private vulnerability reporting, on the
[Security tab](https://github.com/ziyan/teanode/security/advisories/new) of
this repository. It is private to the maintainers until an advisory is
published, and it gives us somewhere to talk that is not a public issue
thread.

Tell us what you did, what happened, and what you expected. A message that
reproduces it — a `.eml` file, an SMTP transcript, a request — is worth more
than a description of one.

## What is in scope

The program in this repository, in its default configuration. In particular:
accepting mail on port 25, authenticated submission on 587, everything under
`/api/v1`, and the certificates it obtains.

## What is not

- Anything that requires the operator's own credentials or configuration
  access. This is single-tenant software; the operator is trusted, and the
  documented ways for them to reach their own network — webhook aliases,
  forwarding targets, a relay host — are features.
- The services it is configured to talk to: PostgreSQL, ClamAV, SpamAssassin,
  an object store. They are trusted as configured, which is why the
  documentation says to keep them off the public internet.
- Denial of service by volume. Algorithmic denial of service is in scope.

## What has already been looked at

`docs/security/security-review.md` records a review of the whole program: what
was found, what was fixed, what is still open, and what the review did not
cover. Reading it first will save you finding something that is already
written down.
