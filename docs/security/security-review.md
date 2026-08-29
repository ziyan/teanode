# Security review

- Date: 2026-08-19
- Reviewed at: branch `restructure`, commit `a0bec11`; remediation through `HEAD`
- Status: first pass, before the repository is published and before the
  running deployment is replaced

This is a review of TeaNode as it stands, written so that somebody deciding
whether to run it on the public internet can see what was examined, what was
found, and what was deliberately not looked at.

## 1. Summary

Ten findings, of which eight are fixed. One of them mattered: a rejected SMTP
authentication wrote a working credential into the log.

The controls that carry the most weight in a mail server hold up. It is not an
open relay; the aggregation pipeline cannot be used for SQL injection; a
templated subject line cannot inject headers. Each of those is now asserted by
a test rather than believed.

What remains open is hardening rather than defect, and it is one item:
`X-Forwarded-Proto` is believed from any caller (SEC-7). Authentication is
rate limited, the ACME retry loop is bounded, the container runs as a
non-root user, and the dependency backlog — 43 reachable vulnerabilities, one
of them a pre-authentication denial of service — has been cleared.

## 2. Scope

### In scope

- The SMTP server on port 25, unauthenticated, reachable by anyone
- Submission on port 587, authenticated by credential
- The dashboard and the GraphQL API on 80/443
- `POST /api/v1/send/{domain}/{template}`, authenticated by credential; the
  authentication middleware lets it through by path prefix for the handler
  to check
- `SendMail`, `RenderTemplate` and `RenderLayout` in GraphQL, operator only,
  which send as a configured domain and render operator-written templates
- Authentication and session handling for both humans and machines
- The configuration file, and what the API discloses from it
- SQL construction, including the aggregation pipeline
- Message parsing and rendering, including the dashboard's mail view
- Dependencies, by way of `govulncheck`

### Not in scope

- The host, its firewall, and the container runtime
- PostgreSQL's own configuration and its network exposure
- ClamAV and SpamAssassin, which are trusted as configured
- The DNS the server resolves against; a hostile resolver is assumed absent
- Denial of service by volume, as opposed to by algorithmic complexity
- Formal cryptographic review of DKIM, ARC and SPF implementations
- Whether any particular deployment's DNS records are correct, which is the
  dashboard's job rather than this review's

### Assumptions

- The operator is trusted. This is single-tenant software: an authenticated
  dashboard user is an administrator, and features that let an administrator
  reach inside the network are design, not defect.
- The server secret in the stored configuration is secret. Every SMTP credential is
  derived from it, so its disclosure is total compromise of submission.
- Mail arriving on port 25 is entirely hostile.

## 3. Trust boundaries

| Boundary | Who is on the other side | What is trusted |
| --- | --- | --- |
| Port 25 | anyone on the internet | nothing |
| Port 587 | holders of a credential | the credential, verified against the server secret |
| Port 80 | anyone | nothing; serves ACME challenges and redirects |
| Port 443 | anyone, then a session or token | the session cookie's HMAC, or the token's hash |
| `/api/v1/send` | holders of a credential | as 587 |
| Configuration file | the operator | fully |
| PostgreSQL | the operator's own network | fully |
| ClamAV, SpamAssassin | the operator's own network | fully |
| Outbound SMTP | remote servers | nothing |

The important line is between port 25 and port 587. Everything that decides
whether this server will carry mail to a third party sits on it.

## 4. Findings

### SEC-1 — A rejected login printed a working password (High, fixed)

`security.DecodeCredential` built its error from the password supplied *and*
the password that would have been accepted, and every caller logs that error
at error level on a failed SMTP authentication.

The client chooses the first sixteen characters of the password; the server
derives the remaining sixteen from them using the server secret. The log line
was therefore not a disclosure of an existing password but the server
completing whichever prefix the client offered. One rejected login, plus read
access to a log file, a support bundle or a backup, produced a credential that
works.

`security.DecodeToken` had the same shape. A successful authentication also
logged the credential key at debug level.

Fixed in `4b6ceb9`: both return a sentinel error naming nothing, the debug
line drops the key, and `internal/util/security/disclosure_test.go` asserts
that no error text contains the password tried, the password that would have
worked, or its derived half.

### SEC-2 — Secrets compared byte by byte (Low, fixed)

Both comparisons above used `!=` on strings, which stops at the first
differing byte. Since the attacker controls part of the input and the server
derives the rest, this is a timing oracle on the derived half. Remote timing
attacks across a network are hard; the fix is one line. Both now use
`subtle.ConstantTimeCompare`. Fixed in `4b6ceb9`.

### SEC-3 — Dependencies were 43 known vulnerabilities behind (High, fixed)

`govulncheck` reported 43 vulnerabilities that this code actually reached.
Thirty-three were in the standard library, against a build pinned to Go
**1.25.0**; the rest were in five modules.

One deserved singling out. **GO-2025-4006**, excessive CPU consumption in
`net/mail.ParseAddress`, is reached from `mailparse.ParseAddress`, which
`handleRcpt` calls on the address in every `RCPT TO` — before authentication,
from anyone who can open a connection to port 25. That was a
pre-authentication algorithmic denial of service on the most exposed surface
the program has.

The `go` directive now requires **1.25.14**, which is past every standard
library fix govulncheck named, and the five modules moved forward:

    golang.org/x/net                v0.42.0 -> v0.58.0
    golang.org/x/text               v0.27.0 -> v0.41.0
    github.com/jackc/pgx/v5         v5.7.5  -> v5.9.2
    aws-sdk-go-v2/service/s3        v1.86.0 -> v1.97.3
    aws-sdk-go-v2/.../eventstream   v1.7.0  -> v1.7.8

The scan now reports **0 reachable vulnerabilities**. One remains in a module
that is required but never called — GO-2026-5932 in `golang.org/x/crypto` —
and has no fixed version published yet.

Neither the workflows nor the Dockerfile needed changing: `go-version: '1.25'`
and `FROM golang:1.25` already track the latest patch, and the floor in
`go.mod` is what makes an older toolchain fail loudly rather than build
quietly. The image was rebuilt to confirm it reports `go1.25.14` rather than
to assume it.

`npm audit --omit=dev` reports no vulnerabilities in the dashboard.

### SEC-4 — Nothing rate limited authentication (Medium, fixed)

There was no throttle, lockout, or backoff on SMTP `AUTH` or on the dashboard
login, so either could be guessed at line rate. The dashboard was partly
protected by accident — bcrypt above the default cost makes each attempt
expensive — but that cost is paid by the server too, which is a reason to
refuse early rather than to rely on the hash being slow. SMTP credentials had
no such accident: verification is an HMAC and a comparison.

Both are now limited per address by a token bucket, defaulting to twenty
attempts at once and ten a minute after that, tunable through
`smtp.authRateLimit` and `smtp.authRateBurst`.

Two choices in it are worth stating, because getting either wrong makes the
limit useless:

- **Keyed by address, not by account or credential.** The identity in a guess
  is chosen by whoever is guessing, so counting per identity lets them reset
  the count by changing it. The port is dropped from the key for the same
  reason — a new connection would otherwise be a new bucket.
- **A bucket is only forgotten once it has refilled.** The registry has to
  evict, because the key is a remote address and an attacker with a /64 has
  more of those than this process has memory. A full bucket is
  indistinguishable from one that never existed, so dropping it forgives
  nothing; dropping a drained one would clear the limit for whoever drained
  it. At the key limit, callers are handed an unheld bucket rather than none,
  so they are still limited.

### SEC-5 — The ACME retry loop had no ceiling (Medium, fixed)

A failed order was retried with no backoff and no give-up. A name that cannot
be validated — a domain whose port 80 is unreachable, which is the ordinary
way this fails — was retried every five minutes for ever, and walked into
Let's Encrypt's failed-validation rate limit, where it then stayed. With a
certificate per domain that is worse than noise: one broken name spends the
allowance every other name needs.

Each certificate now carries its own failure count and next attempt:
`requestBackoff` of five minutes, doubling to a day
(`internal/util/autoacme/autoacme.go`). A name that starts working is retried
within the day; a name that never works costs one attempt a day and nothing
else.

### SEC-6 — The container ran as root (Low, fixed)

`deploy/Dockerfile` set no `USER`, and `gcr.io/distroless/static-debian12`
defaults to root. Binding 25, 80, 443 and 587 needs privilege and the process
never dropped it afterwards.

Worth recording that this was a regression rather than a standing weakness:
the deployment being replaced already runs unprivileged, as `nobody`. The
restructure lost that when the container definition was rewritten, and nobody
noticed because nothing checked.

Fixed with the `:nonroot` tag, which runs as uid 65532, plus
`CAP_NET_BIND_SERVICE` in both compose files. The capability is the whole of
what root was being used for, so granting only it is strictly smaller than
granting root. No code changed. The deployment test passes as non-root,
including binding port 25 and receiving mail on it.

One consequence for anyone upgrading: the mounted configuration and data
directories were created by a process running as root, and uid 65532 cannot
read them. `chown -R 65532:65532` on both, once, before starting the new
image. The compose file says so and so does the getting-started guide.

### SEC-7 — `X-Forwarded-Proto` is trusted from anyone (Low, open)

`isSecureRequest` believes the header without a trusted-proxy list. The
dangerous direction is not spoofing — a client claiming `https` only causes a
*more* restrictive cookie — but omission: behind a TLS terminator that does
not set the header, the session cookie is issued without `Secure` and will be
sent over plaintext.

### SEC-8 — Webhook aliases reach wherever the operator points them (Informational)

An alias of kind `webhook` makes the server POST a message to a configured
URL. The URL is validated as http or https and nothing else, so an
administrator can direct it at a private address. This is accepted: an
administrator can already forward mail to an arbitrary host. Worth knowing
that `sendWebhook` uses `http.DefaultClient`, which has no timeout of its own
and follows redirects, so it depends entirely on the context's deadline.

### SEC-9 — The API was open until the server was claimed (Medium, fixed)

`requireOperator` returned nil while the configuration held no user. That made
onboarding possible — the first account has to be creatable by somebody who
cannot log in yet — but it opened the whole API, not just the onboarding
mutations.

Between first start and the first account being created, anyone who reaches
the server can read the configuration through `ListDomains` and the rest, and
then call `CreateFirstAccount` and own it. On a server started from
`config init` there is already a domain, its aliases and its DKIM selector to
read.

The deployment runbook works around this by telling the operator to claim the
server in advance, which is right for that deployment and is not a fix for
anybody else.

Fixed by deleting the exemption. `GetSession`, `CreateFirstAccount`, `Login`
and `Logout` never call `requireOperator`, so claiming a server still works;
everything else now refuses an anonymous caller whether or not an account
exists. `teanode user add --offline` writes the configuration file directly and
is unaffected, which is the path the cutover runbook uses.

`TestEveryOperationAuthorises` already asserted that every resolver
authorises, and its own comment claimed each one "refuses when there is no
operator" — which was not true until this change. The behaviour now has a test
of its own beside it.

This was found because a check in the deployment harness asserted an empty
string, which matches any reply, so it passed however the server behaved.

### SEC-10 — An unknown API path answers 401, never 404 (Informational)

Authentication runs before routing. Before any account exists the path falls
through to the dashboard and returns 200 with HTML, which is what made the
deployment test's failures unreadable.

## 5. Controls verified

These were examined and found sound. Where a test now exists, it is named.

### 5.1 It is not an open relay

Carrying mail to a third party requires `envelope.CredentialID` or
`envelope.DomainID` to be set (`internal/mx/exchange.go:126`). The SMTP server
only ever sets `CredentialID`, and only from a completed `AUTH`
(`internal/util/smtpd/smtpd.go:482`). `DomainID` is reachable only from the
internal send path.

An unauthenticated message therefore goes to `handleIncoming`, which requires
the recipient domain to be one the configuration serves and answers
"mailbox unavailable" otherwise (`internal/mx/exchange_incoming.go:39`).

`handleOutgoing` re-verifies the credential's key against the configuration
rather than trusting that `AUTH` happened, and refuses a credential restricted
to one local part sending as another.

### 5.2 The aggregation pipeline cannot carry SQL

Field names reach the statement as identifiers, which cannot be
parameterised, so the only defence is that a name must be one the table
offered — a map lookup in `Columns.resolve`. Values always go to a
placeholder. Sort direction is a literal `ASC` or `DESC` chosen by a branch,
never caller text, and is validated against a closed set besides.

Asserted by `internal/util/aggregate/injection_test.go`, which tries eight
hostile field names against filter, sort and distinct, and confirms a hostile
*value* is carried as a parameter rather than refused.

### 5.3 A templated subject cannot inject headers

The send API renders caller-supplied variables into a template's subject.
Every value passes through `EncodeHeaderValue`, which RFC 2047 base64-encodes,
so `\r\n` becomes part of the encoded word and never appears literally.
Probed with four CRLF payloads.

Envelope addresses are separately parsed with `mail.ParseAddress` and only the
parsed `Address` is used, so the display name cannot smuggle anything either.

### 5.4 Credentials at rest

- Human passwords: bcrypt, at above the default cost, in the stored configuration.
- API tokens: 32 bytes from `crypto/rand`, stored as SHA-256 of the secret
  half, compared with `subtle.ConstantTimeCompare`. The plaintext is shown
  once and never stored.
- SMTP credentials: derived by HMAC-SHA256 from the server secret, so the
  configuration holds no password to steal — but the secret is equivalent to
  all of them.
- The GraphQL layer redacts anything tagged `secret:"true"`, which covers the
  server secret, AWS credentials, token hashes and password hashes.

### 5.5 Session handling

The cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` when the request is
TLS (subject to SEC-7). Its value is an HMAC over username and expiry,
verified in constant time. `SameSite=Lax` is what stands between the GraphQL
endpoint and cross-site request forgery, since a cross-site POST does not
carry the cookie.

### 5.6 Rendered mail

Messages render inside a sandboxed iframe under a restrictive CSP. Scripts do
not run. Remote images and `cid:` references are blocked, which is why inline
images appear as gaps. CSS is kept — deliberately, so messages look like
messages — with `@import`, `expression(`, `behavior:` and `-moz-binding`
stripped.

## 6. Accepted residual risks

- **The server secret is a single point of total failure for submission.**
  Every SMTP credential derives from it. There is no rotation path that does
  not invalidate every credential at once.
- **An administrator can reach the internal network** through webhook aliases
  and forwarding targets. Single-tenant software; the administrator is
  trusted.
- **DNS is trusted.** SPF, DKIM and DMARC verification believe the resolver.
  DNSSEC is not validated.
- **No audit log.** There is no record of who changed what in the
  configuration.

## 7. What to do next

1. Add a trusted-proxy setting, or default `Secure` on when TLS is configured
   at all (SEC-7).
2. Re-run `govulncheck` on a schedule. It found forty-three things nobody had
   looked for; it will find more.
3. Give the configuration an audit log. Nothing records who changed what.

## 8. What this review did not do

No fuzzing of the MIME and header parsers, which is where a mail server's
remaining memory and complexity bugs usually live. No review of the DKIM, ARC
and SPF implementations against their specifications beyond the existing
tests. No penetration test against a running instance. No dependency licence
audit. Each is worth doing before this is recommended to anybody else.
