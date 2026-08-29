# Certificates are obtained with HTTP-01 by default

- Status: accepted
- Date: 2026-08-18
- Deciders: Ziyan Zhou

## Context

`internal/util/autoacme` is a hand-written ACME client. It could prove control
of a name in exactly one way: writing a TXT record into an AWS Route53 hosted
zone whose id was a command line flag default. Anyone else running this code
had no way to obtain a certificate, and the server required AWS credentials to
start.

## Decision

`autoacme` gains a `Solver` interface with three implementations, selected by
`tls.acme.challenge`:

- `http-01`, the default: serve the token at
  `/.well-known/acme-challenge/<token>` on the HTTP listener the dashboard
  already uses. Needs port 80 reachable.
- `tls-alpn-01`: answer an `acme-tls/1` handshake with a self-signed challenge
  certificate. Needs port 443, useful where port 80 is blocked.
- `dns-01`: the previous Route53 behaviour, retained because it is the only way
  to obtain a wildcard certificate.

Exactly one solver is constructed, so a server using `http-01` or
`tls-alpn-01` never builds an AWS client.

`tls.acme.directoryURL` is honoured, so a new deployment can be brought up
against the Let's Encrypt staging directory without spending production rate
limits.

## Consequences

- A deployment needs no cloud account. Verified by running with every AWS
  environment variable unset.
- The `http-01` handler must be mounted ahead of authentication and ahead of
  any HTTPS redirect, because a certificate authority fetches it over plain
  HTTP with no credentials. That ordering is a standing constraint on the HTTP
  routing.
- Wildcard certificates still require Route53 and therefore AWS credentials.
  Validation rejects a wildcard host unless `dns-01` is configured, rather than
  failing later at issuance time.
