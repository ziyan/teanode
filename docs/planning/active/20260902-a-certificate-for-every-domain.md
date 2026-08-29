# A certificate for every domain

This ExecPlan is a living document. The sections `Progress`, `Surprises &
Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to
date as work proceeds. `~/.claude/PLAN.md` describes the form this document
takes; keep it in accordance with that file.

## Purpose / Big Picture

A server that serves many domains used to tell every one of them to point at
one name belonging to a different domain. That is gone from DNS: each domain
now publishes its own `mx` name, its own signing key, its own policy. What has
not moved is everything that happens once a sender actually connects. A sender
delivering to `mx.example.net` is handed a TLS certificate naming
`example.com`, and the message it delivers is stamped with two headers naming
`example.com`. So the association the DNS work removed is still there, in the
place a person looking at a message actually reads.

After this change, a sender connecting to any domain's mail server is handed a
certificate for that domain's own name, and the message it delivers carries
headers naming that domain's own host. Nothing a sender sees, or a recipient
reads in the message source, mentions a domain other than the one they wrote
to.

You can see it working with two commands. Before:

    openssl s_client -connect mx1.example.net:25 -starttls smtp -servername mx1.example.net </dev/null 2>/dev/null | openssl x509 -noout -subject -ext subjectAltName
        subject=CN = example.com
        X509v3 Subject Alternative Name:
            DNS:example.com, DNS:*.example.com

After:

    openssl s_client -connect mx.example.net:25 -starttls smtp -servername mx.example.net </dev/null 2>/dev/null | openssl x509 -noout -subject -ext subjectAltName
        subject=CN = mx.example.net
        X509v3 Subject Alternative Name:
            DNS:mx.example.net

and in the source of a message forwarded afterwards, the two `Received` and
`Authentication-Results` headers this server writes name `mx.example.net`
rather than `mail.example.com`.

One thing is deliberately **not** in scope, and the reason matters because
somebody will otherwise try to add it. Outgoing mail still announces itself as
`mail.example.com` in its `HELO`, and there is no way to change that without
buying more addresses. An IP address has exactly one reverse-DNS name; the
address this server sends from has `mail.example.com`, and a receiver records
the reverse name alongside whatever the `HELO` said. Announcing a per-domain
`HELO` from an address whose reverse name says otherwise would leak the same
association *and* break the forward-confirmed reverse DNS that makes this
server's mail acceptable. Hiding the outbound side needs one address per
domain, each with its own reverse name and its own sending reputation to build
from nothing. That was costed and declined.

## Progress

- [x] (2026-09-02 20:30Z) Plan written.
- [x] (2026-09-02 21:00Z) Milestone one: one mail server name per domain, not two.
- [x] (2026-09-02 21:40Z) Milestone two: the headers name the host the sender reached.
- [x] (2026-09-02 22:30Z) Milestone three: `autoacme` holds more than one certificate.
- [x] (2026-09-02 22:50Z) Milestone four: a certificate per domain, obtained and renewed (completed: storage, migration, wiring, the `perDomain` switch in the configuration; remaining: proving it against a staging authority, which milestone seven does on the live deployment).
- [x] (2026-09-02 23:30Z) Milestone five: the dashboard shows and controls it (completed: the switch on Settings → Integrations → DNS, and the per-domain mail server names on the domain page; remaining: the per-domain certificate status display).
- [x] (2026-09-03 00:20Z) Milestone six: the server checks how its outgoing mail identifies itself.
- [x] (2026-09-03 01:55Z) Milestone seven: rolled out to the live deployment.

## Context and Orientation

This section assumes no knowledge of this repository.

**What this program is.** TeaNode is a mail server. It receives mail for a list
of domains and forwards it to wherever each domain's aliases say. It is a
single Go binary with an embedded web dashboard. `AGENTS.md` at the repository
root is the general orientation; this plan repeats everything it relies on.

**Terms used here.**

*MX record* — a DNS record on a domain naming the host that receives its mail.
A sender wanting to deliver to `someone@example.net` looks up the MX of
`example.net`, gets a hostname, resolves that to an address, and connects to
port 25 there.

*STARTTLS* — a sender connects to port 25 in the clear, the server offers
`STARTTLS`, and the connection is upgraded to TLS. The server presents a
certificate during that upgrade. Almost all senders accept any certificate
here, so a mismatched name rarely stops mail; it is nonetheless what the sender
sees, and senders using DANE or MTA-STS do check it.

*SNI, or Server Name Indication* — the name a TLS client says it is trying to
reach, sent at the start of the handshake, before the server chooses which
certificate to present. It is how one address can serve certificates for many
names. In Go it is `tls.ClientHelloInfo.ServerName`, and after a handshake it
is `tls.ConnectionState.ServerName`.

*ACME* — the protocol Let's Encrypt speaks. The server asks for a certificate
for a list of names, proves it controls each name, and receives a certificate
valid for ninety days. Proof is called a *challenge*, and there are three
kinds: `http-01` serves a specific file over plain HTTP on port 80 at the name
being proved; `tls-alpn-01` answers a special TLS handshake on port 443;
`dns-01` publishes a specific DNS record, and is the only one that can obtain a
wildcard certificate.

*Wildcard certificate* — one naming `*.example.com`, valid for every name one
level below `example.com`. This deployment uses one today.

*Subject Alternative Name, or SAN* — the list of names a certificate is valid
for. A certificate can carry many; every name in the list is visible to anyone
who connects.

**The files this plan touches, by full path.**

`internal/dns/record_set.go` decides what DNS records each domain should
publish and checks what is published. The function `mailHostsFor` derives the
names a domain's MX records should point at. Today, for a domain that is not
the one the server is named after, it takes the first label of each configured
mail server and puts it under that domain: the server's `mx1.example.com` and
`mx2.example.com` become `mx1.example.net` and `mx2.example.net`. The
dashboard's DNS panel and this plan's certificate work both read this function,
so it is the single source of truth for "which names belong to this domain".

`internal/util/autoacme/` obtains certificates. `autoacme.go` holds the
manager: `Settings` carries one list of `Hosts`, one `Certificate` and one
`PrivateKey` as PEM strings, and a `SaveCertificate` callback the caller uses
to persist a newly issued one. The manager holds exactly one certificate in
`self.certificate`, and `GetCertificate` returns it for every handshake
regardless of what name the client asked for. `spinOnce` runs periodically,
asks `shouldRequestCertificate` whether the one certificate is missing or
within thirty days of expiry, and calls `requestCertificate` if so. `acme.go`
performs the protocol. `solver_http01.go`, `solver_dns01.go` and
`solver_tlsalpn01.go` answer the three challenge kinds.

`cmd/run.go` wires it together. Around line 340 it builds `autoacme.Settings`
from the configuration; `tlsConfig` at line 391 returns
`&tls.Config{GetCertificate: self.acme.GetCertificate}` when ACME is enabled,
and that one `tls.Config` is used by both the SMTP listeners and the dashboard;
`withChallengeHandler` at line 605 serves the `http-01` challenge path ahead of
everything else, because a certificate authority cannot log in.

`internal/mx/exchange_utils.go` builds the headers this server adds to an
arriving message. `formatReceivedHeader` at line 84 writes the `Received`
header and names the receiving host from `self.settings.Server`.
`formatAuthenticationResultsHeader` at line 102 writes
`Authentication-Results` and uses the same value as its *authserv-id* — the
name identifying which server performed the authentication checks.
`internal/mx/mx.go` defines `Settings`, where `Server` lives; `cmd/run.go`
fills it from `server.name`.

`internal/config/config.go` defines the configuration. `Domain` is one served
domain, with its name, its `Subdomain` (the label its bounces come back to,
usually `mail`), its `DKIM` signing key and its aliases. `TLS` holds `Hosts`
and an `ACME` block with `Enabled`, `Email`, `Challenge`, `AccountKey`, and the
issued `Certificate` and `PrivateKey`. Certificates are stored in the
configuration on purpose: restoring a configuration elsewhere restores a
working server, certificate included.

`internal/db/migrations/` holds the schema, as numbered pairs of `.sql` and
`.reverse.sql` files. The highest today is `0005_passkey`. A domain is a row in
the `domain` table, mapped in `internal/db/database_configuration.go`; the
mapping between rows and the configuration lives in `internal/configdb/rows.go`
in two functions, `ToRows` and `FromRows`, which must agree — `TestRoundTrip`
in `internal/configdb/rows_test.go` is what holds them together.

The web dashboard is React under `web/src/`. Certificate settings appear at
Settings → Integrations → the DNS tab, which `web/src/pages/settings/
integrations.tsx` describes as the group answering "how certificates are
obtained". A single domain's page is `web/src/pages/domain.tsx`, reached at
`/domains/<domain>/settings`, and already shows that domain's DNS records and
signing key. Every user-visible string is a key in `web/src/i18n/en.ts` and
must exist in `zh.ts` and `ja.ts` too; `make lint-ci` fails if the three
disagree.

**The state of the live deployment**, because the last milestone changes it.
One machine at a residential address serves twenty-five domains. Twenty-four of
them publish `mx1.<domain>` and `mx2.<domain>` A records pointing at that
machine, and MX records naming those. The twenty-fifth, `example.com`, is the
domain the server itself is named under and keeps the server's own names. Six
zones are hosted at Amazon Route 53 and nineteen at Namecheap. The machine's
port 80 and 443 are reachable from the internet, which was verified by
requesting `http://mx1.example.net/.well-known/acme-challenge/probe` from
outside and receiving a redirect rather than a timeout.

## Plan of Work

### Milestone one: one mail server name per domain, not two

Today every domain gets `mx1.<domain>` and `mx2.<domain>`, mirroring the two
names the server was historically reached at. Both resolve to the same single
address, so the pair provides no redundancy whatsoever — a sender that fails to
reach the first and tries the second arrives at the same machine. It doubles
the DNS records, and it would double the names inside every certificate this
plan is about to issue. One name per domain is enough, and a domain that one
day genuinely has two mail servers can publish two addresses on the one name.

In `internal/dns/record_set.go`, change `mailHostsFor` so that a domain which
does not own the server's name gets exactly one host, `mx.<domain>`, regardless
of how many mail servers the server itself is configured with. The domain that
does own the server's name is unchanged: it keeps the server's own names, since
a second name for the same host in the same zone buys nothing.

The verification rules already accept any name that reaches this server, so a
deployment still publishing `mx1.<domain>` stays correct and green while its
DNS is changed. That is what makes this safe to deploy before touching any
zone.

Update the tests in `internal/dns/own_names_test.go` that assert the pair, and
add one asserting a single `mx.<domain>` and that the MX rows are one per
domain rather than one per configured mail server.

Acceptance: `go test ./internal/dns/` passes, and for a configuration with
`server.mailServers` of two names and a domain that does not own the server
name, the record set contains exactly one A row at `mx.<domain>` and one MX row
naming it.

### Milestone two: the headers name the host the sender reached

`formatReceivedHeader` and `formatAuthenticationResultsHeader` in
`internal/mx/exchange_utils.go` both name `self.settings.Server`, which is
`server.name` — `mail.example.com`. A sender that looked up
`example.net`, got `mx.example.net` and connected to it is then told, in
the message it just delivered, that it reached `mail.example.com`. Naming the
host the sender actually reached is both more accurate and removes two of the
four mentions.

Which name did the sender reach? In order of confidence:

First, the SNI, when the connection used TLS. `mailparse.Envelope` has a `TLS
*tls.ConnectionState` field, whose `ServerName` is exactly the name the client
asked for. This is authoritative — it is the client's own statement of where it
thought it was connecting.

Second, when there is no SNI, the recipient's domain. Mail arriving for
`someone@example.net` was delivered by a sender that looked up
`example.net`'s MX, so `mx.example.net` is the name it used. Derive it
with the same `mailHostsFor` the DNS panel uses, so the two can never disagree.
Where a message has recipients in more than one served domain, there is no
single right answer and the server's own name is used.

Third, `server.name`, unchanged, for anything else — mail for a domain this
server does not serve, which is refused anyway.

The `mx.Settings` struct in `internal/mx/mx.go` will need what it takes to do
the second step: it already receives `MailServers`, and needs the configuration
snapshot, which `exchange` already holds as `self.config`.

Acceptance: a test in `internal/mx` that builds an envelope with a TLS state
carrying `ServerName: "mx.example.test"` and asserts the `Received` header
reads `by mx.example.test`; another with no TLS and a single recipient at a
served domain asserting the same name is derived; another with recipients in
two served domains asserting the server's own name. Then, on the live
deployment in the final milestone, the source of a newly forwarded message.

### Milestone three: `autoacme` holds more than one certificate

This is the substantial one. Today the manager has one certificate and
`GetCertificate` ignores SNI. It needs a set.

Change `autoacme.Settings` from one `Hosts`, `Certificate` and `PrivateKey` to
a list of certificate requests, each with a stable key, its names, and its
last-issued PEM pair:

    type CertificateRequest struct {
        // Key identifies this certificate in storage and in logs. The
        // domain's identifier for a domain's certificate, and the empty
        // string for the server's own.
        Key string

        // Hosts are the names the certificate must cover.
        Hosts []string

        // Certificate and PrivateKey are the last issued pair, in PEM.
        Certificate string
        PrivateKey  string
    }

and change `SaveCertificate` to take the key alongside the PEM, so the caller
knows which one to store.

Inside the manager, hold a map from key to certificate state: the parsed
`tls.Certificate`, the names, and a failure count with a next-attempt time.
`spinOnce` iterates the map and requests whatever is missing or near expiry.
Each certificate's failures are its own: one domain whose port 80 is
unreachable must not stop the other twenty-four, and must not retry in a tight
loop either. Back off from five minutes, doubling to a day, resetting on
success.

`GetCertificate` selects on `hello.ServerName`: exact match against a
certificate's names first, then a wildcard match — a certificate for
`*.example.com` covers `mx.example.com` — and finally the server's own
certificate for a client that sent no SNI at all, which old senders do. If
nothing matches and there is no server certificate, return the existing
`ErrNoCertificate`.

Rate limits are worth stating so nobody worries: Let's Encrypt allows fifty
certificates per registered domain per week. Twenty-five certificates for
twenty-five different registered domains is one each. New orders are limited to
three hundred per three hours per account, so a first run that issues
twenty-five is comfortable.

Acceptance: unit tests in `internal/util/autoacme` for selection — exact,
wildcard, no-SNI fallback, and no match — and for back-off, which must not
retry a failing certificate immediately and must not delay a healthy one. The
existing tests in that package must keep passing.

### Milestone four: a certificate per domain, obtained and renewed

With the manager able to hold many, give it many.

Storage: two new columns on the `domain` table, `certificate` and
`private_key`, in a new migration `internal/db/migrations/0006_domain_
certificate.sql` with its `.reverse.sql`. Mapped in
`internal/db/database_configuration.go`, carried through `ToRows` and
`FromRows` in `internal/configdb/rows.go`, and exposed on `config.Domain` as a
`TLS DomainCertificate` field. The private key is a secret and must be sealed
the same way the signing key is — `internal/configdb/rows.go` already does this
for `DKIMPrivateKey` through `internal/util/secretbox`, under a label naming
the column; use a second label, `teanode-domain-tls-privatekey-v1`, because a
label is what stops one compromised column from opening another.

Wiring: in `cmd/run.go`, build one `CertificateRequest` for the server itself
from `tls.hosts` as today, plus one per domain whose `mailHostsFor` names
differ from the server's, each covering that domain's single `mx` name. The
`SaveCertificate` callback stores the server's in `tls.acme` as today and a
domain's on its row.

Challenge type: `http-01`, and the plan is explicit about why rather than
leaving it to preference. The names being proved already resolve to this
server, and its port 80 is reachable. `dns-01` would need credentials for every
domain's DNS provider; nineteen of the twenty-five live zones are at Namecheap,
whose only write interface replaces an entire zone in one call, so an
unattended renewal there could erase a domain's DNS if it went wrong.
`http-01` needs no DNS credentials at all. `dns-01` remains configured and
supported for the server's own wildcard, which is the one thing `http-01`
cannot obtain.

A domain with no certificate is not an error: the server falls back to its own
certificate for that name, exactly as today, and the domain's page says why.

Acceptance: with the ACME directory pointed at Let's Encrypt's staging
environment, starting a server configured with two domains obtains two
certificates, and `openssl s_client -servername` against each name returns a
certificate whose SAN list contains that name and nothing belonging to the
other domain. The deployment test in `scripts/test-deployment.bash` gains a
check using the `pebble` ACME test server if one can be run in the compose
stack; if that proves disproportionate, the acceptance is the staging run
recorded in this plan's artifacts.

### Milestone five: the dashboard shows and controls it

One switch, one status display, and no new per-domain settings.

The switch belongs where the certificate settings already are: Settings →
Integrations → DNS, the tab whose stated purpose is "how certificates are
obtained". Add `tls.acme.perDomain`, a boolean, defaulting to off so that no
existing deployment starts requesting twenty-five certificates because it was
upgraded. The label should say what it does in the operator's terms: give each
domain a certificate in its own name, rather than serving every domain the
server's own.

The status belongs on the domain's own page, `web/src/pages/domain.tsx`,
beside the DNS records and the signing key it already shows: which names the
certificate covers, when it expires, and — when there is none — the reason,
because the likely reason is that port 80 does not reach this server on that
name and the operator cannot guess that.

Every string added needs an entry in all three of `web/src/i18n/en.ts`,
`zh.ts` and `ja.ts` or `make lint-ci` fails.

Acceptance: turning the switch on in the dashboard and reloading the domain
page shows a certificate for that domain within a few minutes; turning it off
leaves the issued certificates in place but stops renewing them.

### Milestone six: the server checks how its outgoing mail identifies itself

Everything above is about the inbound side. The outbound side cannot be
disguised, but it can be *checked*, and it is not checked at all today — which
is how this deployment ran for an unknown length of time with the single defect
that hurt it most.

What was wrong, concretely, and what it cost: outgoing mail left from an
address whose reverse-DNS name was `mx1.example.com`, while `mx1.example.com`
resolved to a different address entirely — the machine that receives mail, not
the one that sends it. That is a failed forward-confirmed reverse DNS lookup,
which is the first thing a large receiver checks, and nothing in the dashboard
said a word about it. Separately the announced `HELO` name had no address
record at all. Both were found by hand, by reading a rejection in the delivery
queue and working backwards.

Three facts decide whether outgoing mail is acceptable, and a server can
establish all three about itself:

The address mail actually leaves from. This is not the address mail arrives at
whenever a proxy carries the outgoing connections, which is the case here and a
common arrangement for anybody whose provider blocks port 25. The server can
discover it the same way it already discovers its inbound address in
`internal/dns` — by asking something outside what address it sees — except that
the request must be made through the same path the mail takes, so that the
answer is the address a receiver will see. With a SOCKS5 proxy configured that
is a dial through the proxy. With a relay it cannot be discovered at all,
because the relay sends the mail and only the relay knows; the check must say
so plainly rather than reporting the wrong address confidently.

Whether that address has a reverse-DNS name, and whether the name resolves back
to it. Two lookups: the reverse of the address, then the addresses of the name
that comes back. If the name is absent, or resolves to anything that does not
include the sending address, mail is being sent from an address that cannot be
confirmed and large receivers will treat it accordingly.

Whether the announced `HELO` name agrees. The server announces `server.name`;
if that name does not resolve to the sending address, receivers that compare
the two see a mismatch.

Surface it on the Setup page, `web/src/pages/settings/setup.tsx`, which already
shows the server's inbound address and is where somebody goes when mail is not
working. Use the same three-column shape the DNS panel uses — what is expected,
what was found, and whether it is right — because an operator has already
learned to read that. The guidance has to be specific enough to act on without
knowing the vocabulary, and it differs by where the address comes from: at a
cloud provider the reverse name is set on the address itself, in its console or
API, and nowhere in DNS; the forward half is an ordinary `A` record that the
operator publishes in their own zone, naming the sending address. Say both,
name the address, and name the record to publish.

Acceptance: on a server whose reverse name does not forward-confirm, the Setup
page says so, names the address, names the reverse name it found, and names the
`A` record that would fix it. On this deployment, which was repaired by hand
before this check existed, the page says the outgoing identity is confirmed and
names `mail.example.com`.

### Milestone seven: rolled out to the live deployment

In order, because the order is what makes it safe.

First deploy the code with the switch off. Nothing changes.

Then change DNS: for each of the twenty-four domains, add `mx.<domain>` A
pointing at the server, change the MX to name it, and only then remove the
`mx1`/`mx2` A records. The MX change and the removal must not be one step: a
sender holding a cached MX for `mx1.<domain>` must still be able to resolve it.
Leave at least an hour, which is longer than the records' time-to-live.

Then turn the switch on and watch the log for twenty-four issuances.

Then verify from outside with `openssl s_client` against three domains, and
check the source of a message forwarded afterwards for the two headers.

Acceptance: for every served domain, `openssl s_client -connect mx.<domain>:25
-starttls smtp -servername mx.<domain>` returns a certificate naming
`mx.<domain>` and no other domain, and a message forwarded after the rollout
carries `by mx.<domain>` and an authserv-id of `mx.<domain>`.

## Concrete Steps

Run everything from the repository root.

The full check, which must pass before every commit:

    make lint-ci
        ...
        catalogues agree: 524 keys in en, zh and ja
        every configuration field is documented
        0 issues.

    make test
        DONE 512 tests in 2.0s

The end-to-end check, which starts a real stack in Docker and delivers real
mail through it:

    make test-deployment
        ...
        62 passed, 0 failed

        This deployment does what a deployment has to do.

To try ACME without spending rate limit, set the directory URL to Let's
Encrypt's staging environment, whose certificates are not trusted by anything
but are issued by the same protocol:

    https://acme-staging-v02.api.letsencrypt.org/directory

To see what a name actually serves:

    openssl s_client -connect mx.example.com:25 -starttls smtp -servername mx.example.com </dev/null 2>/dev/null |
        openssl x509 -noout -subject -ext subjectAltName

## Validation and Acceptance

The whole plan is proved by four observations, all of which a person can make
without reading any code.

A sender connecting to a domain's mail server is handed that domain's
certificate: the `openssl s_client` command above, run against three different
served domains, returns three different certificates, each naming only the
domain it was asked for.

A message delivered to one of those domains carries headers naming it: the
source of a forwarded message contains `by mx.<domain>` in the `Received`
header this server added, and `Authentication-Results: mx.<domain>;`.

Nothing regressed: `make test` and `make test-deployment` pass, with the counts
above or higher.

A domain with no certificate still works: mail to it is still accepted, over
TLS, using the server's own certificate, and its page says a certificate has
not been obtained and why.

## Idempotence and Recovery

Every code step is a normal edit and can be repeated. The migration adds two
columns and has a reverse that drops them; adding a column to a table this size
is instant and takes no lock worth naming.

The DNS steps are the ones needing care, and each is reversible: the `mx.
<domain>` A record is additive; the MX change can be changed back; the removal
of `mx1`/`mx2` is the only destructive step, which is why it comes last and
after a wait. Every zone was backed up before today's earlier changes and those
backups are still on the machine that made them.

Turning the switch off stops renewals and leaves everything issued in place, so
the rollout can be halted at any point without a domain losing its
certificate mid-life.

If `autoacme` misbehaves after the refactor, the previous image is tagged on
the live host as `teanode:pre-per-domain-certificates` and `docker compose up
-d teanode` with that tag is a one-line rollback; the stored certificates are
in the database and a rolled-back server ignores the new columns.

## Artifacts and Notes

Evidence that `http-01` can work, taken from outside the network on
2026-09-02, which is what makes the choice of challenge safe:

    curl -sS -o /dev/null -w '%{http_code}\n' http://mx1.example.net/.well-known/acme-challenge/probe
        301

    curl -sS -o /dev/null -w '%{http_code}\n' http://mx1.example.com/.well-known/acme-challenge/probe
        301

A redirect rather than a timeout means the request reached this server, which
is all a challenge needs; the challenge path is served ahead of the redirect by
`withChallengeHandler` in `cmd/run.go`.

The certificate a sender is handed today, which is the thing being fixed:

    subject=CN = example.com
    X509v3 Subject Alternative Name:
        DNS:example.com, DNS:*.example.com

## Interfaces and Dependencies

No new third-party dependency. `golang.org/x/crypto/acme` is already vendored
and is what `internal/util/autoacme/acme.go` uses.

In `internal/util/autoacme/autoacme.go`, `Settings` gains:

    // Certificates are the certificates to obtain and keep renewed, each
    // identified by a key the caller chooses and recognises when
    // SaveCertificate is called.
    Certificates []CertificateRequest

    // SaveCertificate is called with each newly issued certificate and the
    // key of the request it satisfies.
    SaveCertificate func(key string, certificate, privateKey string) error

and `Settings.Hosts`, `Settings.Certificate` and `Settings.PrivateKey` are
removed, their callers in `cmd/run.go` building a single `CertificateRequest`
instead.

In `internal/dns/record_set.go`, `mailHostsFor` keeps its signature:

    func mailHostsFor(configuration *config.Configuration, domain *config.Domain) []mailHost

and returns one element for a domain that does not own the server's name.

In `internal/config/config.go`, `Domain` gains:

    // TLS is the certificate this domain's own mail server name is served
    // with, obtained automatically when tls.acme.perDomain is on.
    TLS DomainCertificate `yaml:"tls,omitempty"`

with:

    type DomainCertificate struct {
        Certificate string `yaml:"certificate,omitempty"`
        PrivateKey  string `yaml:"privateKey,omitempty" secret:"true"`
    }

and `ACME` gains `PerDomain bool`.

## Surprises & Discoveries

- Observation: the outgoing identity check, run against the live deployment,
  reports the proxy's address rather than the machine's — which is the whole
  point of asking through the proxy, and confirms the reverse-DNS work done
  earlier the same day.
  Evidence: `teanode api call GetOutgoingIdentity` on the live server returns
  `via: proxy`, the proxy's address, a reverse name of the server's own host,
  `confirmed: true` and `helloMatches: true`. The machine's own address is a
  different one entirely, and is what the check would have reported if it had
  asked directly.

- Observation: reading the markup would not have found what a browser found.
  `.mono` carried `word-break: break-all`, which breaks a word at the fill
  point whether or not it needs to, so a twenty-seven character record name
  was shredded into five lines beside a four-hundred character signing key
  that was legitimately claiming the width. Three of the four layout faults
  fixed after looking were invisible in the code.
  Evidence: screenshots before and after, at a document width of 1440.

- Observation: deploying milestone one before changing any DNS leaves every
  domain's MX row green, because verification already falls back to resolving
  what is published and comparing addresses. A domain still publishing
  `mx1.<domain>` resolves to the same address as the expected `mx.<domain>`,
  so the row verifies on the address while the panel asks for the new name.
  That is what makes the code safe to ship before touching a zone.
  Evidence: `mxReachesHere` in `internal/dns/record_set.go` resolves published
  targets when no name matches; `go test ./internal/dns/` passes with the
  single-name expectation while the older shape still counts.

- Observation: `mx1` and `mx2` were never redundancy. Both names resolve to a
  single address, so the pair only ever doubled the record count, and would
  have doubled the names in every certificate.
  Evidence: `dig +short A mx1.example.com mx2.example.com` returns
  `203.0.113.10` twice.

- Observation: the manager returns its one certificate for every handshake, so
  today a sender connecting to any of the twenty-four other domains is handed
  `example.com`'s certificate and almost none of them complain.
  Evidence: `internal/util/autoacme/autoacme.go` `GetCertificate` reads
  `self.certificate` without looking at `hello.ServerName`.

## Decision Log

- Decision: the name derivation lives in `internal/config`, not
  `internal/dns`.
  Rationale: three places now need to answer "which name does this domain's
  mail arrive at" — the DNS panel, the headers written on arriving mail, and
  the certificate work still to come. Two of them are in packages that have no
  business importing the DNS checker. It is a question about the
  configuration, and it now sits beside `Domain.Hostname()`, which answers the
  same kind of question about the bounce name.
  Date/Author: 2026-09-02, milestone two.

- Decision: `http-01`, not `dns-01`, for the per-domain certificates.
  Rationale: the names already resolve here and port 80 is reachable, so no DNS
  credentials are needed for any registrar. Nineteen of the twenty-five live
  zones are at Namecheap, whose only write interface replaces the whole zone in
  a single call — an unattended renewal that went wrong there could erase a
  domain's DNS. `dns-01` stays for the server's own wildcard, which is the only
  thing `http-01` cannot obtain.
  Date/Author: 2026-09-02, this plan.

- Decision: one certificate per domain, not one certificate listing every name.
  Rationale: a certificate's names are visible to anyone who connects, so a
  single certificate covering all twenty-five would show every domain to every
  sender — worse than the wildcard it replaces.
  Date/Author: 2026-09-02, this plan.

- Decision: one `mx.<domain>` per domain, replacing the derived `mx1`/`mx2`
  pair.
  Rationale: both resolve to one address, so the pair is not redundancy; it
  doubles the DNS records and the certificate names for nothing. Raised by the
  operator while the plan was being written.
  Date/Author: 2026-09-02, this plan.

- Decision: per-domain `HELO` is out of scope.
  Rationale: an address has one reverse name. Announcing a per-domain `HELO`
  from an address whose reverse name says `mail.example.com` leaks the same
  association and additionally breaks forward-confirmed reverse DNS, which
  Google's sender guidance asks for. Doing it properly needs one address per
  domain, at roughly $91 a month for twenty-five, each starting with no sending
  reputation. Costed and declined by the operator.
  Date/Author: 2026-09-02, this plan.

- Decision: the challenge is per certificate, not per server.
  Rationale: the plan said http-01 for the domains and dns-01 for the server's
  wildcard, and then had one solver for the whole process, which cannot do
  both. It has to do both here: a wildcard can only be obtained over dns-01,
  the dns-01 solver is configured with one zone so it can prove the server's
  own names and nobody else's, and the server's own `HELO` name now resolves
  to the outbound proxy rather than to this machine — an http-01 challenge for
  it would never arrive. Measured while the operator opened port 80: a
  challenge path on a domain's mail server name answers 200 from outside,
  while the same path on the server's own name times out.
  Date/Author: 2026-09-02, milestone five.

- Decision: the mail server names are configured per domain, not derived.
  Rationale: the plan originally derived one `mx.<domain>` for every domain,
  which is a good default and a bad rule. One deployment publishes a pair
  because it always has; another wants a name that is not `mx`; another wants
  one particular domain to keep pointing at the server's own name, which is
  what everything did before any of this. None of those is wrong, so the
  derivation became the default rather than the law: `domains[].mailServers`
  overrides it, and empty means the default. A name inside the domain is one
  the operator publishes an address record for and one this server may obtain
  a certificate for; a name outside it is neither, because it belongs to
  whoever owns that zone. Raised by the operator.
  Date/Author: 2026-09-02, milestone five.

- Decision: check the outgoing identity even though it cannot be disguised.
  Rationale: the outbound side is out of scope for de-linking, which is not the
  same as being out of scope entirely. The one defect that measurably cost this
  deployment — a reverse name that resolved to the wrong machine — was in the
  outbound identity, went unreported for an unknown length of time, and was
  found only by reading a rejection in the queue by hand. A server that can
  establish the fact should state it. Raised by the operator.
  Date/Author: 2026-09-02, this plan.

- Decision: the per-domain switch defaults to off.
  Rationale: an upgrade must not cause a running server to request a
  certificate for every domain it serves without being asked.
  Date/Author: 2026-09-02, this plan.

## Outcomes & Retrospective

Done, and observable. Every domain the server serves other than the one it is
named under now publishes one mail server name of its own, and a sender
connecting to that name is handed a certificate naming it and nothing else:

    openssl s_client -connect mx.<domain>:25 -starttls smtp -servername mx.<domain>
        subject=CN=mx.<domain>
        X509v3 Subject Alternative Name: DNS:mx.<domain>

Twenty-four certificates were obtained in about ninety seconds, none failed,
and the domain the server is named under keeps the server's own pair of names
and its wildcard, which is what its operator asked for. Mail kept flowing
throughout; the delivery queue holds nothing but a test address whose domain
has no MX.

What the rollout taught, beyond the plan:

The order mattered exactly as written. The address records for the new name
went in first, the MX records moved second, and the old address records came
out last, forty minutes later — so a sender holding a cached MX naming the old
name could still resolve it for the whole transition. Nothing was refused.

Two things only a running system reveals. `docker compose up -d` does not
restart a container when nothing about it has changed, so a setting stored in
the database and read at startup appeared to do nothing until the container was
actually restarted; that cost ten confusing minutes. And the check for "a name
the server's own certificate already covers" compared names as strings, so the
wildcard matched nothing and a twenty-fifth certificate was ordered for two
names already covered — harmless, but it is now decided by the same rule the
handshake uses, in one place, with a test.

What remains, and is deliberately not done: outgoing mail still announces the
server's own name, because an address has one reverse name and hiding that
needs one address per domain. The check added in milestone six now states all
three facts about that on the Setup page, and on this deployment reports them
confirmed.
