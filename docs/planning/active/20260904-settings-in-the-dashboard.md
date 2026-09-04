# Every setting an operator can change, reachable from the dashboard

## Why this matters

Most of what an operator can configure lives in PostgreSQL and can only be
changed by exporting the configuration to YAML, editing it and importing it
back. The dashboard edits nine things; the configuration has more than fifty.
An operator who wants a larger maximum message size, a different DNS resolver,
a shorter session, or an ACME contact address has to take the server's
configuration out of the database, hand-edit it, and put it back — for a
one-line change, on a running mail server.

After this change every setting except the database connection is readable and
editable from the Server page, grouped so that each tab answers one question,
with the settings that only take effect on a restart saying so where they are
edited rather than in a paragraph somewhere else.

The gap is not the dashboard's alone. `teanode settings set` is generic over
the server's own GraphQL schema — it reads the types from the API and needs no
per-setting code — so a setting the API does not expose is unreachable from
the command line too. Every field this plan adds to the API arrives in both
places at once; the dashboard form is the second half of the work, not the
first.

You can see it working by opening Server in the dashboard, changing the
maximum message size, and watching a message larger than it be refused with no
restart — then changing an HTTP listener address and seeing it appear in the
"waiting for a restart" list instead.

## What exists today, and where

The configuration is one Go struct, `config.Configuration`, in
`internal/config/config.go` line 22. Its sections are `Server`, `Listen`,
`TLS`, `Database`, `SMTP`, `DKIM`, `Domains`, `Users`, `Session`, `DNS`,
`Antivirus`, `Antispam`, `GeoIP`, `Storage`, `Passkey` and `Upgrade`. It is
stored in PostgreSQL by `internal/configdb` and reached through the
`config.Store` interface, whose `Update` method takes a function that mutates a
copy and commits it if it validates.

The API exposes a subset. `internal/api/v1api/apigraph/settings.go` (438 lines)
holds it, and the shape is the same for every setting in it:

- A `Settings` struct (line 29) with one pointer field per group, and a
  per-group struct beside it — `ServiceSettings`, `S3Settings`,
  `CertificateSettings` and so on. This is what `GetSettings` returns.
- `describeSettings` (line 163), which copies from a `*config.Configuration`
  into that struct. Secrets are never copied: a password becomes a
  `hasPassword` boolean, and `acme.accountKey`, `session.key` and a user's
  `passwordHash` are all marked `secret:"true"` in the configuration and must
  stay out of the API entirely.
- A `…Parameters` struct per group whose every field is a pointer, so that
  "not mentioned" and "set to empty" are different requests.
- `UpdateSettingsArguments` (line 322), one field per group.
- `UpdateSettings` (line 336), which calls `self.config.Update` and, inside it,
  applies each group that was sent using the helpers at the bottom of the file:
  `applyBool`, `applyString`, `applyPort`, `applySecret`. A nil pointer means
  the caller did not mention that field, so it is left alone.

Nine groups are wired up this way: `s3`, `route53`, `antivirus`, `antispam`,
`relay`, `submission`, `proxy`, `upgrade` and `certificates` — where
`certificates` is `tls.hosts` plus `tls.acme.perDomain`.

The dashboard's side is `web/src/pages/settings/integrations.tsx`. It declares
`INTEGRATION_SECTIONS` (line 105) — `sending`, `storage`, `dns`, `scanning` —
which `web/src/pages/server.tsx` renders as tabs of `/server/:tab` beside
`setup` and `about`. One GraphQL query at the top of the file reads every
group; one mutation writes them.

Restarts are already handled and need nothing new. `startupOnly` in
`internal/cmd/server/run.go` line 801 lists the sections that are read once
when the process starts — `listen`, `tls`, `smtp.relay`, `storage`,
`server.dataDirectory`, `antivirus`, `antispam`, `geoip` and
`upgrade.checkInterval`. `warnOnStartupOnlyChanges` subscribes to the store,
compares an encoding of those sections against what the running process read,
and calls `Restarter.AddPending` with the names that differ. The Server page's
About tab renders that list as `server.pendingRestart`. So a setting in one of
those sections becomes editable without any new machinery: change it, and it
appears in the pending list by itself.

Translations are three catalogues that must agree — `web/src/i18n/en.ts`,
`zh.ts` and `ja.ts` — checked by `make check-catalogs`. A value identical to
the English fails unless the key is listed in `SAME_ON_PURPOSE` in
`web/scripts/check-catalogs.mjs`, which is for protocol names and the like.

## What is being added

Everything in the configuration except `Database`, which stays out: the
connection string is how the server reaches the store these settings live in,
and editing it through them is a way to lose both.

Grouped by the question each answers, with the tab it belongs on:

**Mail** — `smtp.maxMessageSize`, `smtp.maxRecipientsIncoming`,
`smtp.maxRecipientsOutgoing`, `smtp.greylistDelay`, `smtp.authRateLimit`,
`smtp.authRateBurst`, `smtp.trustedSenders`. All read per message or per
connection, so all take effect immediately.

**Resolver** — `dns.nameserver`, `dns.checkInterval`,
`dns.externalAddressServices`. Immediate.

**Sessions** — `session.lifetime`, and the `passkey` group: `enabled`,
`relyingPartyId`, `displayName`, `origins`, `maximumPerUser`. Immediate.

**Certificates** — the existing `hosts` and `perDomain`, plus `acme.enabled`,
`acme.email`, `acme.directoryUrl`, `acme.challenge`, and the file-based pair
`tls.certificateFile` and `tls.privateKeyFile`. In `tls`, so a restart.

**Listeners** — `listen.smtpIncoming`, `listen.smtpOutgoing`, `listen.http`,
`listen.https`, `listen.debug`. A restart.

**Identity** — `server.name`, `server.mailServers`, `server.logLevel`,
`server.dataDirectory`. Only `dataDirectory` needs a restart; the other three
are read live.

**Storage** — the existing `s3` group plus `storage.directory` and
`storage.spoolRetention`, and `geoip.enabled` with `geoip.databaseFile`. A
restart.

Secrets stay out: `acme.accountKey`, `session.key`, and anything else marked
`secret:"true"`. Where one exists the API reports a `hasX` boolean, as it
already does for `hasSecretAccessKey` and `hasPassword`.

## The shape of one section, end to end

Every milestone below repeats this, and it is worth reading once. Taking the
existing antivirus group as the example:

    settings.go:37    Antivirus *ServiceSettings `json:"antivirus"`
    settings.go:186   describeSettings copies enabled, host and port across
    settings.go:325   Antivirus *ServiceParameters in UpdateSettingsArguments
    settings.go:362   applyBool / applyString / applyPort inside Update

and on the dashboard:

    integrations.tsx:13   antivirus { enabled host port } in the query
    integrations.tsx:25   $antivirus: ServiceParametersInput in the mutation
    integrations.tsx      a form section rendering those three fields

A new group is the same six edits. Nothing about the store, the validation or
the restart list has to be touched: `config.Update` validates before it
commits, so a bad value is refused by `internal/config/validate.go` and the
error reaches the form.

Two things need care and are the reason this is a plan rather than a chore.

**Durations and sizes are not strings.** `smtp.maxMessageSize` is a
`config.ByteSize` and `smtp.greylistDelay`, `dns.checkInterval` and
`session.lifetime` are `config.Duration`. Both marshal to and from text
("50MB", "6h"). The API should carry them as strings, exactly as
`upgrade.window` already does, and let the configuration's own parser reject
what it cannot read — so that what an operator types in the dashboard is what
they would write in the file.

**A list is not a string.** `smtp.trustedSenders`,
`dns.externalAddressServices`, `server.mailServers`, `tls.hosts` and
`passkey.origins` are `[]string`. `certificates.hosts` already shows how:
`Hosts []string` in the settings and `*[]string` in the parameters, so that
"not mentioned" and "made empty" stay distinguishable.

## Milestone one: the settings that take effect at once

At the end of this milestone the Mail, Resolver and Sessions groups are
readable and writable through the API and editable on the Server page, and
none of them asks for a restart.

Add to `settings.go`: `SmtpSettings`, `ResolverSettings`, `SessionSettings`
and `PasskeySettings`, their four `…Parameters` twins, the fields on `Settings`
and `UpdateSettingsArguments`, the copies in `describeSettings` and the applies
in `UpdateSettings`. Durations and sizes cross as strings; parse them with the
configuration's own types and return the parse error rather than storing
something unparsed.

Add three tabs to `INTEGRATION_SECTIONS` in `web/src/pages/settings/
integrations.tsx` — `mail`, `resolver`, `sessions` — with a form section each,
following the shape of the antivirus and relay sections already in that file.
Add their labels and one hint per field to all three catalogues.

Acceptance. `make test` passes and `cd web && npm run build` compiles. Then,
against a dev server: `teanode settings get --json` shows the new groups
before any UI work is trusted, and `teanode settings set smtp.maxMessageSize
1MB` changes it. On the page, set the maximum message size to 1MB and send a
larger message with `teanode mail send`; it is refused, with no restart. Set
the session lifetime to a minute, wait, and be asked to sign in again. Set the
resolver to something unroutable and watch a domain check fail; set it back.
Check that `teanode server status` does **not** list any of these as waiting
for a restart.

## Milestone two: certificates, whole

At the end of this milestone the Certificates tab carries the whole of `tls`
except the account key.

Extend `CertificateSettings` and `CertificateParameters` with `acmeEnabled`,
`acmeEmail`, `acmeDirectoryUrl`, `acmeChallenge`, `certificateFile` and
`privateKeyFile`. The challenge is one of a small set — `http-01` and `dns-01`
— so render it as a choice rather than a text field, and let
`internal/config/validate.go` remain the authority on what is allowed.

The page must say what these cost. Changing any of them changes nothing until
the process restarts, and `tls` is already in `startupOnly`, so saving puts
"tls" in the pending list. Say so in the section, next to the fields, and link
to the About tab where the restart button is.

Acceptance. Change the ACME contact address, save, and see `tls` appear in
`teanode server status` under what is waiting for a restart. Confirm
`teanode settings get --json` never returns the account key, and confirm the
same by reading the GraphQL response directly. Set the challenge to something
invalid through the API and see the error come back from validation rather
than being stored.

## Milestone three: listeners and identity

At the end of this milestone the addresses this server listens on and its own
name are editable.

Add `ListenSettings` and `IdentitySettings` with their parameter twins, and two
tabs. `server.name`, `server.mailServers` and `server.logLevel` take effect
live; `server.dataDirectory` and every listener address do not.

This is the milestone that can lock somebody out. An operator who sets the
HTTPS listener to a port nothing can reach loses the page they set it on, and
the fix is a command line on the host. Two guards: the section says plainly
that these take effect on restart and that a wrong address is recovered with
`teanode-server config`, and the form asks for confirmation before saving a
listener change, naming the address it is about to store.

Acceptance. Change the debug listener, save, restart, and see it listening
there. Change the log level and watch the log's verbosity change with no
restart. Confirm the confirmation dialog appears for a listener and does not
for the log level.

## Milestone four: storage and geolocation

At the end of this milestone the Storage tab carries `storage.directory`,
`storage.spoolRetention` and the `geoip` pair beside the S3 group already
there.

Both sections are in `startupOnly`, so both land in the pending list on their
own. `storage.directory` is where mail is written: say that moving it does not
move what is already there.

Acceptance. Change the spool retention, save, restart, and see the change take
effect. Point GeoIP at a file that does not exist and see the server refuse it
in validation rather than at the next message.

## Milestone five: the tabs make sense together

At the end of this milestone the Server page reads as one page rather than as
a list of everything.

By milestone four the page has ten or more tabs, which is a row nobody can
scan. Group them: Setup, Mail, Sending, Certificates, Listeners, Storage,
Resolver, Scanning, Sessions, Identity, About. If that is still too many, the
answer is a rail down the side of the Server page rather than a longer row of
tabs; decide it with the real list on screen rather than here.

Every setting on the page needs its sentence. The pages that exist are readable
because each field says what it is for and what happens if it is wrong, and a
field with a label and no hint is a field somebody will guess at.

Acceptance. Open every tab and read it end to end. Every field has a hint;
every group that needs a restart says so where it is edited, not only on the
About tab.

## Progress

- [ ] Milestone one: smtp limits, resolver, session and passkey — the settings
      that take effect at once
- [ ] Milestone two: the rest of the ACME block and the certificate files
- [ ] Milestone three: listeners and server identity, with a confirmation
      before a listener is changed
- [ ] Milestone four: storage paths, spool retention and geoip
- [ ] Milestone five: the tabs regrouped, and a hint on every field

## Decisions

**The database connection stays out.** It is how the server reaches the store
that these settings live in. An operator who gets it wrong through the
dashboard loses the dashboard and the configuration together.

**Hand-written forms per section, not generated from the schema.** A generated
surface would arrive complete and stay current for free, and it was considered.
It was rejected because the value of these pages is the sentence under each
field — what the setting is for, what it costs, what goes wrong — and a schema
carries none of that unless every field grows annotations, which is the same
work with the labels further from the form.

**Durations and sizes cross the API as text.** "6h" and "50MB" are what an
operator writes in the file and what the configuration's own types parse. A
number of seconds would be a second spelling of the same setting, and the two
would drift.

**Restart-required settings are editable.** The alternative was to keep them
out on the grounds that a setting that does nothing until a restart is a trap.
But the trap is already there — the configuration has them, the file can set
them — and the pending-restart list exists precisely to make the delay visible.
Hiding them does not make them safe; it makes them undiscoverable.

## Still open

Whether `server.dataDirectory` should be editable at all. It is the one field
here whose change silently strands data: the server starts against the new
directory and the mail in the old one is simply not there. Milestone four
should either refuse it, or say what has to be moved by hand before restarting.
