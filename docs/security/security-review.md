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

### SEC-7 — `X-Forwarded-Proto` is trusted from anyone (Low, fixed)

`isSecureRequest` believed the header without a trusted-proxy list. The
dangerous direction is not spoofing — a client claiming `https` only causes a
*more* restrictive cookie — but omission: behind a TLS terminator that does
not set the header, the session cookie is issued without `Secure` and will be
sent over plaintext.

Fixed in the second review (SEC-20): the header is now believed only from a
proxy listed in `server.trustedProxies`, the same rule as `X-Forwarded-For`,
and a server that terminates TLS itself redirects its plain listener to
HTTPS and sends `Strict-Transport-Security`, which closes the omission case
for the deployment this project ships.

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

`TestEveryOperationAuthorizes` already asserted that every resolver
authorizes, and its own comment claimed each one "refuses when there is no
operator" — which was not true until this change. The behavior now has a test
of its own beside it.

This was found because a check in the deployment harness asserted an empty
string, which matches any reply, so it passed however the server behaved.

### SEC-10 — An unknown API path answers 401, never 404 (Informational)

Authentication runs before routing. Before any account exists the path falls
through to the dashboard and returns 200 with HTML, which is what made the
deployment test's failures unreadable.

### SEC-11 — Mailboxes, app passwords, IMAP and single sign-on (Informational)

Added with the mailbox program (`docs/planning/active/20260906-mailboxes.md`)
and reviewed as it was built rather than after:

- Every mailbox, folder and item operation in the API resolves the row and
  refuses unless the caller owns the mailbox (`requireMailbox`,
  `requireFolder`, `requireItems` in `internal/api/v1api/apigraph`); a
  message's content is readable only by its mailbox's owner or a holder of
  `mail:audit` over its domain (`access.CanReadMail`). Denial is "not found".
- App passwords are twenty characters of a 32-letter alphabet, bcrypt-hashed,
  shown once, revoked singly, and never the account password. IMAP and
  submission sign-ins go through the same per-address rate limiter as
  credentials, and every way of being wrong is one answer.
- IMAP advertises `LOGINDISABLED` until the connection is encrypted; port
  993 is TLS from the first byte. A submission signed in with an app password
  is refused unless the sender is one of the mailbox's own addresses.
- DMARC failures are refused only under a `reject` policy now; `none` and
  `quarantine` are recorded, a quarantined message lands in Junk, and the
  spam filter scores the failure. This is looser than before and what the
  policy asks for.
- Single sign-on uses the authorization-code flow with PKCE and a nonce, a
  state signed with the server secret and expiring in ten minutes, an `https`
  issuer whose discovery document must name itself, and an HTTP client that
  refuses to connect to a private, loopback or link-local address whatever
  name resolves to it. The client secret is write-only in the API. The
  identity provider's groups only ever touch groups that name one.
- The redirect URL a provider is given is built from `Host` and
  `X-Forwarded-Proto`, so SEC-7 applies to it too.

- Files for a draft go up as a multipart body to
  `PUT /api/v1/mailbox/drafts/{itemId}/attachments` or
  `POST /api/v1/mailbox/{mailboxId}/drafts/attachments`, behind
  authentication (a session or a bearer token), with the mailbox's
  ownership checked the way the GraphQL draft resolvers check it. The
  check runs in a short transaction *before* the body is read, so a
  stranger's request costs nothing to buffer, and again when the draft is
  written. The body is capped at the message-size limit by
  `http.MaxBytesReader` and counted again file by file (413 past it); the
  files then join the parts the draft already holds, and a total past the
  limit is refused as invalid (400). With no message-size limit configured
  the upload is unbounded, as SMTP is. A stale draft id is refused. The
  reply is the draft as stored, so the page never guesses a part's index.
- `ApplyMailboxRules` runs a mailbox's stored rules over a folder that is
  already filed, the way arrival runs them over a new message. It resolves
  the mailbox through `requireMailbox` with `mail:write` and the folder
  through `requireFolder`, and refuses a folder of another mailbox as not
  found. The page is bounded at 500 messages; a `move` to a folder that is
  gone or belongs elsewhere does nothing; `delete` moves to Trash rather
  than erasing; and `forward` is counted and skipped, so the mutation
  cannot resend old mail to an address a rule names. `mail:send` is
  deliberately not consulted, because nothing leaves the server.
- The search filters of `ListMailboxItems` (`from`, `to`, `subject`) reach
  `ILIKE` as parameters, with the caller's `%`, `_` and `\` escaped first;
  `since`/`before` are typed, and the page is bounded. `SaveMailboxContact`,
  `DeleteMailboxContact` and `SetMailboxFolderPinned` go through the same
  ownership checks as the rest of the mailbox API, and the last refuses the
  Inbox, which is always at the top.

Open: the IMAP server does not advertise CONDSTORE or QRESYNC yet, so a
client syncs a large folder the slow way.

## 5. Controls verified

These were examined and found sound. Where a test now exists, it is named.

### 5.1 It is not an open relay

Carrying mail to a third party requires `envelope.CredentialID`,
`envelope.DomainID` or `envelope.MailboxID` to be set
(`internal/mx/exchange.go`). The SMTP server sets `CredentialID` or
`MailboxID`, and only from a completed `AUTH`; `DomainID` is reachable only
from the internal send path. A `MailboxID` submission is further refused
unless the sender is one of that mailbox's addresses and its owner holds
`mail:send`.

An unauthenticated message therefore goes to `handleIncoming`, which requires
the recipient domain to be one the configuration serves and answers
"mailbox unavailable" otherwise (`internal/mx/exchange_incoming.go:39`).

`handleOutgoing` re-verifies the credential's key against the configuration
rather than trusting that `AUTH` happened, and refuses a credential restricted
to one local part sending as another.

### 5.2 The aggregation pipeline cannot carry SQL

Field names reach the statement as identifiers, which cannot be
parameterised, so the only defense is that a name must be one the table
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

## 7. What to do next

1. ~~Add a trusted-proxy setting, or default `Secure` on when TLS is
   configured at all (SEC-7).~~ Done; see SEC-20.
2. Re-run `govulncheck` on a schedule. It found forty-three things nobody had
   looked for; it will find more. (It now runs weekly; see SEC-12.)
3. Sign releases (SEC-31).

## 8. What this review did not do

No fuzzing of the MIME and header parsers, which is where a mail server's
remaining memory and complexity bugs usually live. No review of the DKIM, ARC
and SPF implementations against their specifications beyond the existing
tests. No penetration test against a running instance. No dependency license
audit. Each is worth doing before this is recommended to anybody else.

---

# Second review

- Date: 2026-09-08, with a further pass on 2026-09-09 over what landed in
  v0.16.0 and v0.17.0 (SEC-33 to SEC-35)
- Reviewed at: `main` at v0.15.0 (`2b3ca32`), then at v0.17.0 (`a7be213`);
  remediation in the same pull request
- Status: second pass, over the whole program as it stands after the
  mailbox, IMAP, single sign-on, self-upgrade and built-in spam filter work

The first review did not look at the DKIM, SPF, DMARC and ARC
implementations against their specifications, at the parsers for
algorithmic complexity, at the self-upgrade mechanism, or at the command
line client. This one did, along with a second look at everything the first
covered. Nine reviewers, each over one surface, followed by verification of
every finding against the code before it was fixed.

## Summary

Thirty-four findings, of which twenty-nine are fixed here. Five of them
mattered:

- Once a person could rename their own account, they could rename it to
  the console's name and hold every permission from the next request on
  (SEC-33).
- A domain manager could read, rewrite and delete another domain's
  templates and layouts, and an auditor could resend another domain's
  deliveries (SEC-13).
- A credential restricted to one address could send as any address at its
  domain, signed and aligned (SEC-14).
- An SPF `ptr` mechanism passed for any name the sender's reverse zone
  claimed, which is a DMARC bypass for every domain that uses one (SEC-15).
- Four ways for one message from anyone on port 25 to cost the server hours
  of CPU or unbounded memory (SEC-16 to SEC-19).

What remains open is the trust in the release pipeline (SEC-31), the
database password the compose file ships with (SEC-32), and three smaller
items recorded at the end.

## Findings

### SEC-12 — Twenty-two standard library vulnerabilities reachable (High, fixed)

`govulncheck` against Go 1.26.0 reported 22 vulnerabilities in the standard
library that this code reaches, among them panics in `crypto/x509`
certificate checking reached from passkey sign-in, and parsing faults in
`net/mail`, `net/textproto`, `mime` and `net/url`, all fixed by 1.26.6. The
`go` directive is now `1.26.6`, which is the floor the first review chose
to enforce the same way, and the scan reports none. The weekly workflow
would have found them on the next Monday.

### SEC-13 — Rows scoped to "some domain" rather than their own (High, fixed)

`GetTemplate`, `DeleteTemplate`, `GetLayout`, `ModifyLayout`,
`DeleteLayout`, `GetDelivery`, `RetryDelivery`, `ListDeliveriesByMail`,
`GetReport`, `GetMailOpens` and `ListMailOpens` checked that the caller
held `domain:manage` or `mail:audit` over *some* domain, and then read
whichever row was named. Identifiers are not secrets. A group granted
`domain:manage` over one domain — which is what that permission kind is
for — could rewrite another domain's layout with a phishing page that every
template rendered through it would then carry, delete its templates, read
its deliveries' recipients and error text, and re-trigger a failed delivery
of its mail. `ModifyTemplate` did it right, which showed the intended
pattern.

Each now calls `requireDomainPermission` over the row's own domain after
loading it, which answers not found for a domain the caller does not hold
and for one that has been deleted, so nothing else changed. `ListMailOpens`
filters to the domains the caller audits. Asserted by
`TestRowsAreScopedToTheDomainThePermissionIsHeldOver`.

`TestEveryOperationAuthorizes` did not catch this because it checks that a
helper's *name* appears in each resolver, not what the helper is asked.

### SEC-14 — A restricted credential could send as anyone at its domain (High, fixed)

A credential's `alias` restriction — the thing that lets an operator hand a
newsletter service a credential that can only send as `newsletter@` — was
enforced on the envelope sender only. The `From` header, which is what the
recipient reads, was parsed and stored and never compared. The message was
then DKIM-signed for the domain and marked `auth=pass`, so the
impersonation was aligned and authenticated. The mailbox submission path
already checked both, which is now what the credential path does
(`credentialMaySendAs`). An unrestricted credential is unchanged.

### SEC-15 — SPF `ptr` validated any resolving name and matched on a bare suffix (High, fixed)

RFC 7208 §5.5 validates a PTR name only when it resolves back to the
connecting address; the evaluator kept any name that resolved to anything.
Whoever controls an address's reverse zone chooses the name it claims, so a
sender set their PTR to `www.victim.example`, and `ptr` passed for the
victim's domain. `ptr:example.test` also matched `notexample.test`, with no
label boundary. Both fixed, with fixtures for each; one existing fixture
that relied on the lax behaviour was corrected.

### SEC-16 — Quadratic header unfolding (High, fixed)

`mailparse.Split` appended each continuation line to the header string it
belonged to, copying the whole header every time. Two megabytes of ` x`
lines took over a minute and the cost grew as the square; the largest
message allowed was a day of CPU, and `Split` runs on every message before
anything else is checked — no served domain is needed, only a connection.
The header block is now built with a builder, and bounded at 4096 headers
of 64 KiB each, past which the message is refused. The same loop in the
DSN parser is fixed the same way.

### SEC-17 — Unbounded multipart nesting (High, fixed)

`TraverseParts` recursed through nested multipart bodies without limit,
each level a reader wrapped around the one above, so reading a byte at
depth N passed through N readers. A level costs a sender fifty bytes; a
message that nests twenty thousand levels took most of a minute and one
that nests a million is days. Reached on receipt for the content and virus
checks, from anyone who can send to a served domain, and again in the
dashboard. Bounded at 32 levels.

### SEC-18 — DMARC aggregate report ingestion was unbounded (High, fixed)

The `rua` address is published in every domain's DMARC record, so anyone
can send to it, and no authentication check runs on what arrives. The
report inside was inflated with no limit — a megabyte of gzip is a gigabyte
of records — every record became a row, and every distinct source address
in it was reverse-resolved, one at a time, five seconds each, inside the
open transaction, before the report's domain was even looked at.

Now: a decoded report is capped at 16 MiB and 10,000 records, a zip at 16
files; the address the report came to is resolved to the domain whose
report address it is, and a report for any other domain is dropped before
any work; at most 256 addresses are resolved, eight at a time, all within
thirty seconds. Asserted by `TestAnAggregateReportIsBounded`.

### SEC-19 — Unbounded DKIM signatures, command lines, and connections (High, fixed)

Three limits port 25 did not have:

- Every `DKIM-Signature` header was verified, in its own goroutine, with
  its own key lookup and its own hash of the whole body. A message can
  carry a million of them. Now at most eight are examined, as RFC 6376
  §6.1 permits, the body hash is computed once per canonicalization, the
  `h=` list is capped, and a goroutine that panics still answers so the
  verifier cannot wait for ever.
- A command line was read until a newline arrived, however long that took;
  a client that never sent one was buffered at line rate for the read
  deadline. Lines are now refused past 8 KiB with `500 5.5.2`.
- Connections were unbounded, each a goroutine and, once it sent DATA, a
  buffer the size of the largest message, for up to an hour. Each listener
  now serves 1000 at once and answers `421` past that. Handling an accepted
  message is bounded at ten minutes.

The GraphQL endpoint, which is reachable before authentication because
logging in is a mutation, now caps its request body at 1 MiB, and both HTTP
listeners have header, read and idle timeouts.

### SEC-20 — The dashboard was served in plaintext on port 80 (Medium, fixed)

Port 80 answered ACME challenges and then served the whole dashboard and
API. A hostname typed into a browser goes there first, and a reader who
signed in there sent their password in the clear and received a cookie
without `Secure`, which the browser then kept sending that way. When this
process serves HTTPS itself, the plain listener now redirects everything
but the challenge path there, with a `308`; behind a proxy that terminates
TLS, where there is no HTTPS listener here, it serves as before. Responses
over TLS carry `Strict-Transport-Security`. `X-Forwarded-Proto` is believed
only from a listed proxy (SEC-7), in the session cookie, the single sign-on
cookie and the redirect URL given to the identity provider.

### SEC-21 — Two `From` headers (Medium, fixed)

DMARC was evaluated on the last `From` header and mail programs show the
first, so a message signed by the attacker's own domain in the last and
naming the victim in the first passed DMARC and displayed as the victim.
RFC 7489 §6.6.1 says to refuse such a message; it is now refused with the
invalid-From error.

### SEC-22 — `<noscript>` carried markup past the sanitizer (Medium, fixed)

The sanitizer parsed mail with scripting enabled, under which a parser
keeps the inside of `<noscript>` as one piece of text and writes it back
out untouched. The frame that shows the message runs without scripts, so
the browser turned that text into elements — a `preconnect` to a tracking
host, a `meta refresh`, a referrer policy — none of which the sanitizer had
seen. It now parses the way the frame will, with scripting off, and removes
`noscript` and the other raw-text elements outright.

Reply and forward also pasted the sanitized message into the compose
editor, which is part of the dashboard's own document rather than the
frame: a `<style>` block in a message restyled the whole page while the
reader wrote, and could lay a fake sign-in over it. The quote now loses
style, link, meta and id before it reaches the editor, and the editor
contains its own painting.

### SEC-23 — IMAP ignored `mail:write` (Medium, fixed)

Every dashboard mutation checks `mail:write`; nothing on the IMAP path
did, so a user whose role allows only reading could flag, move, expunge,
append and delete folders from a mail program. The permission is now read
at sign-in and at each recheck; a folder opens read-only without it, and
the commands that write refuse.

### SEC-24 — A mailbox rule could forward a message in a loop (Medium, fixed)

An alias forward adds `Delivered-To`, which is how a message is kept from
being forwarded back; a rule forward did not, so two mailboxes forwarding
to each other passed a message back and forth for ever, one `Received`
longer each time. Rule forwards now add the header, and a rule does not
forward a message to an address it has been delivered to or one that has
crossed more than twenty-five hosts.

### SEC-25 — Terminal control characters in the client's output (Medium, fixed)

`teanode mail list` wrote subject lines and sender names to the terminal
as they were. A terminal obeys control characters, so a message could move
the cursor up and overwrite the row above — a rejected message reads as
delivered — or load the clipboard with a command. Every cell, field, error
and, when standard output is a terminal, message body now has such
characters shown as their escapes. Output to a file or pipe is unchanged.

### SEC-26 — Forwarding to a mail server sent its password over unverified TLS (Low, fixed)

An alias of kind `mailserver` with a username and password connected with
opportunistic, unverified TLS, so whoever answered at that name got the
password. The relay path verified; this one now does the same when a
password is present, requiring STARTTLS and checking the certificate
against the configured host.

### SEC-27 — Sign-in timing named the addresses with mailboxes (Low, fixed)

An app-password sign-in for an address with no mailbox behind it was
refused at once; one with a mailbox was refused after a bcrypt. Every
refusal now costs one bcrypt, and a mailbox may hold at most twenty app
passwords, each of which is one more on a sign-in. Starting a passkey
ceremony, which anybody may do and which is held for five minutes, now
counts against the login limiter. The send endpoint, which verifies a
credential on every request, counts against the submission limiter.

### SEC-28 — A password change left every other session valid (Low, fixed)

Changing a password, or an administrator resetting one, is usually done
because somebody else has it, and that somebody may already be signed in.
Every other session of the account is now ended; the one making the change
stays.

### SEC-29 — The client followed redirects with its token, and wrote its profile world-readable for a moment (Low, fixed)

The API client followed redirects and the standard library keeps the
`Authorization` header on a redirect to the same host, including from
`https` to `http`. It now reports a redirect as the answer it is. The
profile file was created with the default mode and made private
afterwards; it is created private. The fallback command the sign-in page
offered put the token on the command line, where the shell's history keeps
it; it now reads the token from the terminal without echo.

### SEC-30 — Smaller items fixed

- Incoming `Authentication-Results` headers naming this server are removed
  on arrival, as RFC 8601 §5 requires; one that stayed sat above the real
  one for everything downstream.
- `rsa-sha1` signatures are refused, as RFC 8301 requires.
- A `_dmarc` name with another TXT record beside the policy refused every
  message from the domain, because the records were joined; only
  `v=DMARC1` records are read now, and more than one is none.
- An ARC chain that could not be validated refused the message with a
  permanent error; it is now a failed chain, which is what the next hop is
  told, as RFC 8617 §5.2 says.
- The signed bounce address's signature is compared in constant time.
- `safefetch` refuses the 6to4, Teredo and NAT64 prefixes, which carry an
  IPv4 address inside them.
- The spam filter's header scan, header tokenizer and link extraction are
  bounded the way its body scan already was.
- An out-of-office reply requires `mail:send`, as a rule forward does.
- Every list query is capped at 1000 rows however many were asked for.
- A disabled account that reached the GraphQL handler no longer has a
  principal built from its old grants; the middleware already refused it,
  so this was latent.

### SEC-31 — Releases are verified by checksum, not signature (Medium, open)

The self-upgrade downloads a release from GitHub over TLS and checks it
against the `SHA256SUMS` published beside it. The checksum is produced by
the same job that builds the binary, so it proves the bytes arrived intact
and nothing about who produced them. Anyone who can publish a release —
the maintainer's account, the release token, or the third-party action the
release workflow uses at a mutable tag — can hand a hostile binary to every
server with automatic upgrades on, which executes it with the database
credentials in its environment. The fix is a signing key held outside the
build: sign `SHA256SUMS` in the release job, ship the public key in the
binary, and refuse a release without a valid signature. That is a change to
the release process as much as to the code, and is left for its own change.
Until then, the workflow's actions should be pinned to commits.

### SEC-32 — The compose file ships a fixed database password (Low, open)

`deploy/docker-compose.yml` sets `POSTGRES_PASSWORD: teanode` and publishes
the database on `127.0.0.1:5432`, so any account on the host reads the
server secret and every stored message. The review's trust model already
places PostgreSQL inside the operator's network; on a single-purpose host
that is the host's other users. Generating the password in `config env`
and reading it from `.env` is the fix, and is a change existing
deployments have to be walked through, so it is not made here.

### SEC-33 — An account renamed to the console's name became the console (Critical, fixed)

The console — the command line run on the server with the server secret —
is told apart from an account by its username alone: a request carrying
`(local)` is handled as holding every permission, and is never looked up.
v0.17.0 let a person edit their own account, including its username, and
nothing reserved that name. A member with no permissions beyond signing in
could call `UpdateUser` with `username: "(local)"`; their session, keyed by
user id, then carried the console's name on every request, which reads
every mailbox, changes every setting, and mints API tokens for any
account — a takeover that outlives the rename.

The name is now refused wherever an account is named — `User.Validate`,
which the database, the command line and identity-provider provisioning
all go through, and the API's own check — and an account that somehow
carries it is refused at sign-in, so the row cannot be believed even if it
exists. Asserted at each layer. Found the day after it shipped, in the
pass over v0.17.0; the self-service test proved which *fields* a person
may change and not which *values*.

### SEC-34 — The sign-in page wrote a query parameter into a command to paste (Medium, fixed)

The page the command line client opens to sign in takes the profile name
from the URL and, when the browser cannot reach the client, offers a
`teanode auth login --name <name> …` command to copy. Nothing checked the
name, so a crafted link — sent to an operator, who presses Authorize and
pastes what they are given — ran whatever the name held the moment it
reached a shell. The page now includes a name only when it is a host name
or a word, and the client refuses any other name as a `--name`, so a
legitimate one never trips it.

### SEC-35 — A spoofed `List-Id` borrowed the reader's "always load pictures" (Low, fixed)

v0.17.0 lets a reader say once that a mailing list's pictures may be
loaded without asking. The list is identified by its `List-Id`, which is a
header anyone can write, so a stranger who guessed which lists a reader
trusts had their tracking pictures loaded — through the proxy, so no
address leaks, but the opening and its time do, which is what the question
exists to withhold. The standing answer now applies only to a message
that passed DMARC, which is the rule the list's mark already followed; the
server applies it, and so does the list's own reader, which had applied
the answer to every message on screen.

### Also open

- A DSN for a delivery can be replayed by whoever received it: each one
  rewrites the delivery's status, and for an outgoing original creates a
  fresh delivery of the bounce to the sender's alias target. The holder is
  the receiving server, so this is bounded; a window after which a delivery
  no longer accepts notifications would close it.
- The `dns-01` solver writes challenge records for every configured host
  into the one hosted zone, so `perDomain` with `dns-01` and a domain
  outside that zone fails the server's own order. An availability bug
  rather than a security one.
- The IMAP `SEARCH TEXT` reads and parses every message in the folder from
  storage per command, and `APPEND` files a message as outgoing mail from
  whatever `From` it carries, which an auditor of the domain then sees.
- `PDF` attachments open inline on the dashboard's origin. No known
  browser runs script from one, so this is hardening.

## Controls verified this time

Beyond the first review's list, and named so nobody repeats the work:

- **DKIM**: `l=` refused; RSA under 1024 bits refused; empty `p=` is an
  error not a pass; `k=` must match `a=`; `x=` enforced; `i=` must be under
  `d=`; header selection bottom-up with oversigned absent headers
  contributing nothing; a verification error never refuses a message on its
  own.
- **SPF**: the ten-lookup limit is counted for every mechanism that
  resolves; more than one record, more than one `redirect=`, and more than
  ten MX hosts are `permerror`; `%{p}` is never resolved; macro output
  cannot carry a `/`.
- **DMARC**: organizational domain from the public suffix list; `sp=`
  applied only when the record came from above; no reports are *sent*, so
  external destination verification does not arise; Go's XML has no entity
  expansion.
- **ARC**: not consulted by the verdict, so no trusted-sealer list is
  needed; capped at fifty sets.
- **IMAP**: every folder and item is resolved through the signed-in
  mailbox, so a UID or name of another mailbox's is not found;
  `LOGINDISABLED` and `AUTHENTICATE` both wait for TLS; literals are
  bounded; `SEARCH` is evaluated in Go, so no client string reaches SQL.
- **Passkeys and single sign-on**: ceremonies single-use and expiring;
  registration bound to the caller; assertion verified for challenge,
  origin, RP ID, signature and user handle; state and nonce sealed and
  expiring; identities linked by `(provider, subject)` only, never by
  email; the return path restricted to a same-site path; the identity
  provider reached through a client that refuses private addresses at the
  dial.
- **Self-upgrade**: repository and endpoint compiled in; HTTPS only, no
  downgrade on redirect; strictly newer versions only; staged binary
  written private, ownership and mode checked before exec; `server:manage`
  required; nothing reaches a shell.
- **Storage**: identifiers refused if they carry a path character; every
  one comes from a database row.
- **The client**: loopback listener on `127.0.0.1`, random port, nonce
  checked, token delivered by POST, one callback, five-minute timeout;
  browser opened without a shell; TLS verified unless `--insecure`, which
  is announced; passwords read without echo; `mail download` never
  overwrites.
- **Deployment**: non-root image with `NET_BIND_SERVICE` only; no Docker
  socket; workflow permissions least-privilege; no `pull_request_target`;
  untrusted fields passed through `env` rather than interpolated.
- **Secrets at rest**: `gitleaks` over every commit finds nothing.
- **Dashboard dependencies**: `npm audit --omit=dev` finds nothing.

## What this review did not do

No fuzzing, still. No penetration test against a running instance. The
protocol implementations were reviewed against their specifications by
reading, not by conformance suites. The findings above are what reading
found; a fuzzer over `mailparse`, `dkim` and `dmarc` is the next thing
worth doing.
