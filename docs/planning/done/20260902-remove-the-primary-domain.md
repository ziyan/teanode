# Remove the primary domain

## Why this matters

TeaNode has a setting called `server.primaryDomain`: "the domain this server
speaks as". It is documented as deciding three things, the dashboard explains
it on the Setup page, and an operator with more than one domain is invited to
choose one. It turns out to decide almost nothing, and what little it does
decide can be expressed more directly.

After this change, an operator configuring TeaNode never meets the concept.
Every domain stands on its own: it publishes its own DNS records, including
its own DKIM key record, and nothing about one domain is defined in terms of
another. There is one less question to answer during setup, one less field
that can be wrong, and one less thing whose meaning has to be explained.

The concrete, observable outcome: `teanode config show` on a configured server
prints no `primaryDomain` line, the Setup page has no "The domain this server
speaks as" card, and each domain's DNS panel asks for a `TXT` record at
`<selector>._domainkey.<domain>` rather than a `CNAME` pointing at another
domain — while a server whose DNS already uses those CNAMEs continues to show
every record as verified, because a `TXT` lookup follows a `CNAME`.

## Terms used here, in plain language

**DKIM** is a signature on outgoing mail. The server holds a private key; the
matching public key is published in DNS as a `TXT` record at a name built from
a *selector* and the domain, like `teanode1._domainkey.example.com`. A
receiving server looks that name up and checks the signature. The selector is
just a label that lets one domain publish more than one key.

**A CNAME** is a DNS record that says "this name is an alias for that name".
Anything looking up any record type at the alias gets the answer from the
target. That last point is the load-bearing fact in this plan: if
`teanode1._domainkey.example.com` is a `CNAME` to
`teanode1._domainkey.example.net`, then asking for the `TXT` record at the
first name returns the `TXT` record published at the second.

**HELO** is the greeting an SMTP client sends when it connects: "hello, I am
this host". **Reverse DNS**, or a **PTR record**, is the mapping from an IP
address back to a name. Receiving servers commonly check that the connecting
address has a PTR, and that the name it gives resolves back to that same
address, which is called **forward-confirmed reverse DNS**.

**The envelope sender**, sometimes called the return path or the bounce
address, is the address given in the SMTP `MAIL FROM` command. It is where a
bounce goes, and it is the domain whose **SPF** record — a `TXT` record listing
which addresses may send for a domain — a receiver checks.

## What the primary domain actually does today

This was established by reading the code rather than the documentation, and
the documentation was wrong. The field's own doc comment in
`internal/config/config.go` claimed it decides which hosts the server
recognises as itself for loop detection. It does not.

`Configuration.PrimaryDomain()` is defined in `internal/config/lookup.go` and
has exactly three callers:

The first is `sharedDomainKeyTarget` in `internal/dns/record_set.go`, reached
through `Configuration.SharesDomainKeyWithPrimary` in
`internal/config/lookup.go`. When a domain's DKIM key is byte-for-byte the same
as the primary domain's, the DNS panel asks for a `CNAME` pointing at the
primary's key record instead of a `TXT` carrying the key. This is the one live
behaviour.

The second is `cmd/run.go`, which passes it as `mailer.Settings`
`DefaultSenderDomain`. Read `internal/mailer/mailer.go`: that value is used
only when `envelope.Sender` is empty. There is exactly one caller of
`SendMail` in the whole repository — `internal/api/v1api/apisend/send.go`,
around line 83 — and it always sets `Sender` from a parsed address. The default
is therefore never reached.

The third is `cmd/run.go` again, passing it as `api.Settings.Domain`. Searching
`internal/api` for reads of that field finds none: the only fields of that
struct ever read are `BackendID`, `Restarter` and `Secret`. It is dead.

So: one live behaviour, two dead ones.

## What it does not do, despite appearances

The name the server greets other servers with is `server.name`. In
`cmd/run.go` the exchange is built with `Server: configuration.Server.Name`,
and `internal/mx/exchange_delivery.go` passes `Hello: self.settings.Server` on
every outgoing connection. The primary domain is not involved. Reverse DNS
therefore constrains `server.name` and says nothing about the primary domain.

Loop detection — refusing to deliver to a domain whose MX points back at this
server — compares a candidate MX host against `self.settings.Domain` in
`internal/mx/exchange_delivery.go` around line 341. `cmd/run.go` fills that
field from `configuration.Server.Name`, not from the primary domain.

The envelope sender on forwarded mail is built in
`internal/mx/exchange_delivery.go` from `delivery.Mail.Domain.Hostname()` —
the bounce host of the domain the mail *arrived for*. So SPF on a forward is
checked against that domain's own subdomain. Again, not the primary.

## The one real consequence, and why removing it is safe

Dropping the shared-key `CNAME` means the panel asks every domain for its own
`TXT` record. The cost is that rotating a shared key becomes one DNS edit per
domain rather than one edit in total. That is a real cost and it is the reason
the feature exists.

It is safe to remove because of the CNAME fact stated above. A deployment whose
DNS already carries those CNAMEs keeps working and keeps *verifying*: the
checker in `internal/dns/record_set.go` resolves the `TXT` record at
`<selector>._domainkey.<domain>`, the resolver follows the CNAME, and the value
that comes back is the key it expected. Nothing has to be changed in DNS on
upgrade. An operator who prefers the one-edit rotation can go on publishing
CNAMEs by hand and the panel will still show them as verified; it simply stops
*recommending* them.

This was confirmed against a live deployment carrying twenty-five domains, all
of which publish `teanode1._domainkey.<domain>` as a CNAME to the same record
on one of them. Twenty-three of the twenty-five report every record verified
today and must continue to after this change.

## Orientation: the files involved

`internal/config/config.go` holds the configuration structs, including
`Server.PrimaryDomain`. `internal/config/lookup.go` holds `PrimaryDomain()` and
`SharesDomainKeyWithPrimary`. `internal/config/validate.go` checks that a
configured primary domain names a real domain. `internal/config/defaults.go`
holds `NewID`. `internal/bootstrap/bootstrap.go` maps environment variables
onto the configuration on a first run. `internal/dns/record_set.go` builds the
list of DNS records a domain needs and checks whether they are published.
`cmd/run.go` wires everything together at startup.
`internal/api/v1api/apigraph/settings.go` is the settings the dashboard reads
and writes. `web/src/pages/setup.tsx` is the Setup page.
`web/src/i18n/en.ts`, `ja.ts` and `zh.ts` are the three message catalogues,
which must stay in step or the build fails.

## Milestone one: stop recommending the shared-key CNAME

The goal of this milestone is that every domain's DNS panel asks for a `TXT`
record carrying that domain's public key, and that a deployment already using
CNAMEs still shows those records as verified.

In `internal/dns/record_set.go`, find the block that begins with the comment
"A domain signing with the same key as the primary one can point at the
primary's record instead of repeating the key" and calls
`sharedDomainKeyTarget`. Delete the block and the `sharedDomainKeyTarget`
function beneath it. The record that remains is the `TXT` record with the
domain's own public key, which the code above the deleted block already built.

Then delete `SharesDomainKeyWithPrimary` from `internal/config/lookup.go`,
which now has no callers.

The DNS verification code below the deleted block is unchanged and is what
makes this backward compatible: it calls `self.resolveTxt(ctx, name)` and
compares what comes back with `sameDKIMKey`. Do not change it.

Run the tests:

    cd <the repository root>
    make test

`internal/dns` has a test named `TestSharedDomainKeyTarget` — visible in the
test output as `PASS internal/dns.TestSharedDomainKeyTarget/same.test`. It
covers the behaviour being removed, so delete it along with any helper it alone
uses. Do not delete `TestPublishesDKIMKey` or the tests around
`_domainkey` naming; those cover the `TXT` record that remains.

Write a new test in `internal/dns/record_set_test.go` asserting that two
domains sharing one key are each told to publish a `TXT` record at their own
`<selector>._domainkey.<domain>` name, and that neither is told to publish a
`CNAME`. This is the test that would have failed before the change and passes
after.

Acceptance for this milestone: `make test` passes, and on a development server
the DNS panel for any domain shows a `TXT` row at
`teanode1._domainkey.<domain>` rather than a `CNAME` row. To see it, follow
`docs/reference/local-development.md` to start a server, sign in, open a
domain, and look at the records list.

## Milestone two: remove the setting

The goal of this milestone is that `primaryDomain` no longer exists anywhere:
not in the configuration, not in the API, not in the dashboard, not in the
documentation.

Start at the leaves and work in. In `cmd/run.go`, replace the
`DefaultSenderDomain: configuration.PrimaryDomain()` line — delete the field
from `mailer.Settings` in `internal/mailer/mailer.go` as well, along with
`DefaultSenderAlias` beside it if it is likewise unused, and delete the `else`
branch in `SendMail` that reads it. That branch cannot be reached: its one
caller always sets a sender. Make the missing-sender case an explicit error
instead, so that a future caller which forgets is told rather than quietly
sending as some arbitrary domain.

Still in `cmd/run.go`, delete `Domain: configuration.PrimaryDomain()` from the
`api.Settings` literal and delete the `Domain` field from that struct in
`internal/api/api.go`. Nothing reads it.

Then delete `PrimaryDomain()` from `internal/config/lookup.go` and the
`PrimaryDomain` field from `Server` in `internal/config/config.go`. Two
supporting pieces go with them: the check in `internal/config/validate.go`
around line 78 that a configured primary names a real domain, and the
`SERVER_PRIMARY_DOMAIN` entry in `seedVariables` in
`internal/bootstrap/bootstrap.go`.

Watch for one non-obvious use. `internal/bootstrap/bootstrap.go` around line
372 creates a first domain from `seed.Server.PrimaryDomain` when the seed names
one and no domains exist, and derives that domain's `Subdomain` with
`subdomainOf(seed.Server.Name, seed.Server.PrimaryDomain)`. This is how a brand
new server started with `TEANODE_SERVER_PRIMARY_DOMAIN=example.com` ends up
with `example.com` already configured. That convenience should not be lost.
Rename the environment variable to `TEANODE_SERVER_DOMAIN`, keep the same
behaviour of creating that one domain on a first run, and make it a seed value
that is not stored as a setting — it names a domain to create, not a property
of the server. Update `docs/configuration.md`, where every environment
variable must be documented or `make lint-ci` fails on `check-config-docs`.

Then the API. In `internal/api/v1api/apigraph/settings.go`, delete
`PrimarySettings`, `PrimaryParameters`, the `Primary` field on `Settings` and
on `UpdateSettingsArguments`, the branch in `UpdateSettings` that applies it,
and the `Primary:` entry in `describeSettings`. Keep `ptrOr` only if something
else uses it.

Then the dashboard. In `web/src/pages/setup.tsx`, delete `UPDATE_PRIMARY`, the
`Primary` type, the `primary { ... }` selection in the `OVERVIEW` query, the
`PrimaryDomainCard` component and the place it is rendered. In each of
`web/src/i18n/en.ts`, `ja.ts` and `zh.ts`, delete every key beginning
`setup.primary`. The three catalogues are checked against each other by
`make check-catalogs`, which `make lint-ci` runs, so a key removed from one
must be removed from all three.

Run everything:

    cd <the repository root>
    make lint-ci
    make test

Both must be clean. `make lint-ci` runs the secret scanner, the catalogue
check, the configuration-documentation check and golangci-lint; it prints
`0 issues.` when it is happy.

Acceptance for this milestone: `grep -rn PrimaryDomain --include=*.go . |
grep -v vendor` returns nothing, `grep -rn "setup.primary" web/src` returns
nothing, and on a development server `teanode config show` prints no
`primaryDomain` line while the server starts, receives a message and forwards
it exactly as before.

## Milestone three: make loop detection mean something

This milestone is separable and could be done first. It is included because
investigating the primary domain revealed that loop detection does not work on
at least one real deployment, and the fix belongs with this work rather than
after it.

Loop detection exists to stop the server delivering to a domain whose MX
records point back at this same server, which would loop. In
`internal/mx/exchange_delivery.go` around line 341 it does this:

    for _, mx := range mxs {
        if strings.HasSuffix(strings.ToLower(strings.Trim(mx.Host, ".")), "."+self.settings.Domain) {
            return 0, fmt.Errorf("mx: domain %q is using teanode, loop detected", domain)
        }
    }

`self.settings.Domain` is `server.name`. On a deployment where `server.name` is
`mail.example.com` and the MX records point at `mx1.example.com` and
`mx2.example.com` — which is the shape the DNS panel itself recommends, since
`server.mailServers` is a separate list — no MX host ends with
`.mail.example.com`, so the check never fires and the loop is not detected.

Change it to compare against the names this server actually answers on: the
entries of `server.mailServers`, falling back to `server.name` when that list
is empty, which is what `Configuration.MailServers()` in
`internal/config/lookup.go` already returns. Compare for equality with each
name as well as for a suffix match, since an MX host is normally exactly one of
those names rather than something beneath it.

Add a test in `internal/mx` that a delivery to a domain whose MX is one of this
server's own mail server names is refused with a loop error, and that a
delivery to an unrelated MX is not. The first assertion fails before this
change on a configuration with distinct `name` and `mailServers` values.

Acceptance: `make test` passes including the new test, and the new test fails
if the change is reverted.

## Milestone four: every domain gets its own key

Removing the shared key makes a promise the code did not quite keep: that
every domain has a key of its own and publishes it at its own name. A domain
created in the dashboard has had one from the moment it was created, but a
domain that arrives any other way — a configuration file written by hand, an
import, a database written by a release that only gave keys to some of them —
can have none, and nothing ever gives it one. It validates, it receives mail,
and it sends unsigned. Nothing says so except a receiver's spam folder.

`config.EnsureSecrets` in `internal/config/secret.go` is where this belongs. It
already exists to fill in what a new installation does not have yet, it runs
once on the way up, and every decision it makes happens inside the mutation for
the reason its comment gives: two instances starting together must agree, and
the store re-runs the mutation after a lost race. A key generated outside the
mutation would be written over the winner's, leaving one instance signing with
a key the other did not store.

So: for every domain with no private key, generate one with
`config.GenerateDomainKey`, taking the selector from `dkim.selector` unless the
domain already names one of its own, and log where the operator has to publish
it. Extend the short-circuit at the top — the one that avoids a write, and so a
version bump and a reload on every other instance — with a `missingDomainKeys`
check beside `missingUserIdentifiers`.

Fill in, never replace. A key already in the configuration matches a DNS record
already published; regenerating it stops that domain's mail from verifying
until somebody notices and republishes. That includes the twenty-three domains
on the live deployment that hold a copy of the old shared key: they keep it,
they keep verifying through the CNAMEs they already publish, and an operator
who wants distinct keys rotates them one at a time from the dashboard, when it
suits them.

`internal/bootstrap/bootstrap.go` generates a key for a seeded domain today,
and keeps doing so even though `EnsureSecrets` would now cover it. Dropping it
was tried and reverted: configuring a database is its own step, and an operator
who runs it can ask `teanode dkim show` for the record and publish it before
the server has ever started. A domain with no key until first run makes that
impossible, which the end-to-end deployment test noticed by failing at exactly
that point. It costs a key written before there is a secret to encrypt it with,
which milestone five handles by design rather than by accident.

Test in `internal/config`: three domains, one of them already holding a valid
key under a selector of its own. After `EnsureSecrets` all three have a key,
no two are the same, the generated ones can be published, the existing one is
untouched, and a second call changes nothing.

## Milestone five: signing keys encrypted at rest

A domain's private key sits in a column of the `domain` table as PEM. It is the
only secret in this program that is both a private key and a row of its own,
which is exactly the shape that leaks: a copy of one table, a support query, a
row printed into a log, a replica of the domains and not the settings.

Port the pattern already used elsewhere for this — HKDF-SHA256 from a master
secret under a per-column label, then AES-256-GCM with a fresh nonce per seal —
into `internal/util/secretbox`. The master secret is `server.secret`. The label
is `teanode-domain-dkim-privatekey-v1`, fixed for the lifetime of the rows
sealed under it, because nothing sealed under one label opens under another.

The boundary is `internal/configdb/rows.go`, and only there. `ToRows` seals on
the way in and `FromRows` opens on the way out, so everything above the store
goes on holding plaintext and nothing else has to know: signing is untouched,
and `config export` stays a plaintext file an operator can read, edit and
restore. Neither function needs a new argument, because the configuration
passed to `ToRows` and the settings read first by `FromRows` both carry the
secret.

Three cases the code has to handle, all of them in the tests:

- **A value written before this existed** has no seal on it. `secretbox.Sealed`
  says so, it is read as it stands, and the next save seals it. A column
  converts as its rows are rewritten, with no migration — which is just as
  well, because a migration would need the master secret to run.
- **A sealed value that will not open** is fatal, not skipped. A domain that
  silently loses its key signs nothing and says nothing.
- **No secret yet.** The first save of a brand new server writes the seed
  before `EnsureSecrets` has generated anything. Store the key as it stands;
  `EnsureSecrets` runs immediately afterwards and that save seals it. Refusing
  would mean a server that cannot start, in exchange for a window of
  milliseconds.

What this is worth, plainly: the master secret is itself in the database, so a
full dump discloses both halves and this defends against nothing. What it does
is stop a private key from leaving in a *partial* dump, and put the boundary in
place for the day the master secret comes from outside the database — which is
the change that would make it worth something against a full one, and is not
in this plan.

## Milestone six: a domain's records name only itself

Removing the setting left the shape it created. Every domain's MX named the
mail hosts of whichever domain the server is called after, and every bounce
name was a CNAME to the server — so looking up the MX of any one domain handed
you the name of another, and from there the set. That is the primary domain
still being published, in every zone, after being deleted from the code.

So a domain that is not the one the server is named under gets `mx1.<itself>`
and `mx2.<itself>`, address records of its own pointing at the same host, an MX
at its bounce name instead of the alias, and its own DMARC record. The domain
that owns the server's name keeps it: a second name for the same host in the
same zone buys nothing.

Verification is about where mail lands rather than how the name is spelled, so
nothing has to change on an existing installation: an MX naming the server's
own hosts counts, a bounce name still aliased to the server counts because an
MX lookup follows the alias, and a name this code never suggested counts if it
resolves to the same address.

Two things found while doing it, both fixed here:

- The DMARC record asked for `rua@mail.<domain>`, an address the server
  refuses. A recipient is special only if it is signed — checked at `RCPT`
  against the server secret — so a report sent to the address the dashboard
  displayed bounced, and nothing said so. It publishes a signed address now.
  The identifier in it is derived from the domain rather than being the
  domain's own, because a signed address must carry a ULID and a domain's
  identifier is usually its name.
- The address records for the mail hosts were listed only on the domain that
  owns the server's name, so that twenty-four pages out of twenty-five did not
  repeat rows that were somebody else's to publish. With per-domain names that
  reason is gone: every page lists its own, and every row on it is one the
  reader can go and create.

What this does not hide, and no DNS record can: outgoing mail still says the
server's name in `HELO`, the reverse DNS of the sending address names it, and
STARTTLS presents a certificate for it. A reader of the headers still learns
which server sent the message. Hiding that needs a name and a certificate per
domain, which is a different piece of work.

## What this plan deliberately does not change

The DNS and reverse-DNS problems found on the live deployment are separate
from the code, because they are changes an operator makes in DNS and in their
cloud provider, not changes to TeaNode. They have since been made, and are
recorded here only so the next reader knows why they are not in the diff:
outgoing mail left through addresses whose PTR named a host whose `A` record
pointed somewhere else, so forward-confirmed reverse DNS failed on every
message, and the HELO name had no `A` record at all. Both were fixed by giving
the HELO name the addresses mail actually leaves from and pointing both PTRs at
it. Neither was caused by the primary domain and neither was fixed by removing
it.

## Progress

- [x] Milestone one: stop recommending the shared-key CNAME
- [x] Milestone two: remove the setting
- [x] Milestone three: make loop detection mean something
- [x] Milestone four: every domain gets its own key
- [x] Milestone five: signing keys encrypted at rest
- [x] Milestone six: a domain's records name only itself

## Decision log

**2026-09-02 — Encrypt the signing key and nothing else.** The other secrets in
the configuration — the server secret, the session key, the AWS keys — live in
the settings rows, and a key derived from one of them cannot protect the row it
came from. The signing keys are the one secret that lives apart from the master
secret, so they are the one secret encrypting it can separate. Encrypting them
with a key that is itself in the database is worth saying out loud: it is not
protection from a full compromise, it is protection from a partial one, and it
is the boundary a master secret from outside the database would plug into.

**2026-09-02 — A tagged value rather than a heuristic.** A sealed value is
stored as `sealed:` followed by base64. The alternative was to recognise
ciphertext by shape, by trying to parse PEM first. Rejected: a reader must be
able to tell "not encrypted" from "encrypted and I cannot open it" with
certainty, because guessing wrong in one direction hands out a corrupt key and
in the other quietly stops encrypting the column. The tag also lets somebody
who meets one in a database dump see it is deliberate.

**2026-09-02 — Remove rather than redefine.** The alternative was to keep a
setting naming the domain that holds the shared DKIM key, renamed to something
honest like `dkim.keyDomain`. Rejected: it keeps a concept an operator has to
understand in order to answer a question they would rather not be asked, in
exchange for saving DNS edits during a rotation that most deployments will
never perform. An operator who wants the one-edit rotation can still publish
CNAMEs by hand, and the panel will still verify them.

**2026-09-02 — Backward compatibility comes free.** Establishing that a `TXT`
lookup follows a `CNAME` is what made removal safe without a DNS migration.
Twenty-three domains on the live deployment publish those CNAMEs today and must
keep verifying; they will, because the checker asks for `TXT` and the resolver
follows the alias.

**2026-09-02 — The doc comment was the source of the error.** The claim that
the primary domain governs loop detection came from the comment on the field,
which the dashboard copy then repeated. Both have been corrected in commit
`1623de1`. This is recorded because it is the reason the field looked
load-bearing when it was not: three plausible responsibilities were attributed
to it and only one was real.
