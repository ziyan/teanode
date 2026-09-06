# Configuration

Everything an operator sets lives in the database, alongside the mail. The
server reads it at startup and re-reads it every few seconds, so a change made
in the dashboard — or by another instance — takes effect without a restart.

There is no configuration file. A server used to be described by
`teanode.yaml`, and moving that into the database is what lets several
instances run against one another without disagreeing about which domains
exist or which credentials are valid.

Two things cannot be kept there, and come from the environment instead:
how to reach the database, and which instance this process is.

## The environment

Write a starting point with:

    teanode-server config env --output .env \
      --hostname mail.example.com --domain example.com

One variable is required:

**`TEANODE_DATABASE_URL`** — where the configuration, the mail and the counters
are kept, for example
`postgres://teanode:password@postgres:5432/teanode?sslmode=disable`. The
discrete `TEANODE_DATABASE_HOST`, `TEANODE_DATABASE_PORT`,
`TEANODE_DATABASE_USER`, `TEANODE_DATABASE_PASSWORD`, `TEANODE_DATABASE_NAME`,
`TEANODE_DATABASE_SSL_MODE` and `TEANODE_DATABASE_LOG_QUERIES` override it
field by field. There is no default
host: an instance whose variable is missing would otherwise reach an empty
local database, decide it is a brand new server, and configure itself — which
looks like it worked.

Three are optional. The first matters once there is more than one instance:

**`TEANODE_INSTANCE_ID`** — distinguishes this process from the others sharing
that database. It is part of the key under which usage counters are
accumulated, and those are added up by reading a row and writing it back, so
two instances sharing a name lose each other's counts. Defaults to the host
name, which is what a container is already given, and is truncated to the last
32 characters if it is longer.

**`TEANODE_UPGRADE_DIRECTORY`** — where an upgrade installed from the dashboard
puts a binary it cannot write over the running one, and where the next start
looks for it. Defaults to `upgrade` under `TEANODE_SERVER_DATA_DIRECTORY` when
that is set, and to nothing when it is not — in which case a deployment that
cannot replace its own executable is told it cannot upgrade itself. It must be
an absolute path, and a relative one is refused rather than resolved: a staged
binary has to be found again by a start from any working directory.

It is a variable rather than a setting for one reason: a staged binary has to
be found and run before anything opens the database, because this program
reverts migrations it does not recognise and an old binary that reached the
database first would undo the new one's schema. The settings are in the
database. This cannot be.

For a container it should be a directory on a mounted volume — the shipped
`docker-compose.yml` names `/var/lib/teanode/upgrade`. A directory inside the
image would work until the container was recreated and then silently be the old
binary again. The directory is created private to the user the server runs as,
and a staged binary that anybody else could have written is refused at the next
start rather than run.

**`TEANODE_ALLOW_MIGRATION_REVERT`** — permits an older binary to undo
migrations a newer one applied. Off by default, and the default is the
interesting half: a start that finds migrations it does not recognise refuses
to run rather than reverting them, because reverting drops the columns they
added and everything in those columns, and the three ordinary ways to arrive
there are all accidents. See `docs/coding/database-migrations.md`. Set it to
`true` to downgrade on purpose.

### First run only

The rest describe the server to create when the database has no configuration
in it yet. After that the database is the answer and these are ignored — the
server says so in its log when it finds them set.

| Variable | Sets |
| --- | --- |
| `TEANODE_SERVER_NAME` | `server.name` |
| `TEANODE_SERVER_DOMAIN` | a domain to serve; created on a first run, with its own signing key |
| `TEANODE_SERVER_DATA_DIRECTORY` | `server.dataDirectory` |
| `TEANODE_SERVER_LOG_LEVEL` | `server.logLevel` |
| `TEANODE_SERVER_MAIL_SERVERS` | `server.mailServers`, comma separated |
| `TEANODE_LISTEN_SMTP_INCOMING` | `listen.smtpIncoming` |
| `TEANODE_LISTEN_SMTP_OUTGOING` | `listen.smtpOutgoing` |
| `TEANODE_LISTEN_HTTP` | `listen.http` |
| `TEANODE_LISTEN_HTTPS` | `listen.https` |
| `TEANODE_TLS_HOSTS` | `tls.hosts`, comma separated; defaults to the server name |
| `TEANODE_TLS_ACME_ENABLED` | `tls.acme.enabled` |
| `TEANODE_TLS_ACME_EMAIL` | `tls.acme.email` |
| `TEANODE_SMTP_REQUIRE_REVERSE_DNS` | `smtp.requireReverseDns` |
| `TEANODE_SMTP_DISABLE_SEND` | `smtp.disableSend` |
| `TEANODE_S3_ENABLED` | `storage.s3.enabled` |
| `TEANODE_S3_ENDPOINT` | `storage.s3.endpoint` |
| `TEANODE_S3_BUCKET` | `storage.s3.bucket` |
| `TEANODE_S3_REGION` | `storage.s3.region` |
| `TEANODE_S3_PATH_STYLE` | `storage.s3.pathStyle` |
| `TEANODE_S3_ACCESS_KEY_ID` | `storage.s3.accessKeyId` |
| `TEANODE_S3_SECRET_ACCESS_KEY` | `storage.s3.secretAccessKey` |
| `TEANODE_PASSKEY_ENABLED` | `passkey.enabled` |
| `TEANODE_PASSKEY_RELYING_PARTY_ID` | `passkey.relyingPartyId`; defaults to the server name |
| `TEANODE_PASSKEY_ORIGINS` | `passkey.origins`, comma separated; defaults to https:// and the relying party |
| `TEANODE_PASSKEY_REDIS_ADDRESS` | `passkey.redis.address`; only needed for more than one instance |
| `TEANODE_PASSKEY_REDIS_PASSWORD` | `passkey.redis.password` |

A variable that is set but empty means empty, not absent — some of these turn
something off rather than change it, and `TEANODE_LISTEN_HTTPS` with no value
is how you say there is no HTTPS listener. So leave a key commented out rather
than blank unless you mean to clear the setting.

A first run needs at least a server name and a domain. The domain is created
with a signing key of its own, so DKIM is set up before anyone has heard of it.
Every domain added later gets its own key the same way. No account is created: the first person
to open the dashboard chooses their own username and password.

## Moving an existing server in

A deployment that ran on `teanode.yaml` loads it once:

    teanode-server config import --file /opt/teanode/teanode.yaml

Everything is carried across unchanged — domain and alias identifiers, signing
keys, the server secret, the session key — because changing any of them breaks
something that is working. Stop the server first, so that it does not adopt
half of a configuration you are still reviewing. `--dry-run` says what would be
loaded and writes nothing.

The reverse makes a backup, in the same format:

    teanode-server config export --file backup.yaml

## Settings that need a restart

The listeners, TLS, the object store, the data directory and the optional
integrations are read once, when the process builds what it needs. Changing
one of those stores the change and nothing more until the instance restarts.

Every instance notices and says so — in its log, and on **Settings → Server**
in the dashboard, which names the settings that are out of date and offers to
restart. Restarting means the process exits and whatever supervises it starts
a new one; there is no restart in place, because the point is to build those
things again. Mail already accepted is on disk and is delivered afterwards, and
a sender that connects during the few seconds it takes will try again.

That page also says what it thinks will start the process again — a container,
systemd, or nothing it can see. It is a guess: neither a container's restart
policy nor a unit's `Restart=` can be read from inside the process, so it
names the supervisor rather than promising a return, and errs towards warning.

Restarting is also a command, so a deployment can do it without a browser:

    teanode server restart

## Reading and checking it

    teanode-server config show                 # with secrets redacted
    teanode-server config show --show-secrets
    teanode-server config validate

These read the same environment the server does, so they run where the server
runs — in its container, or with its env file. From anywhere else, the client
reads and changes the same configuration through the API: `teanode settings
show` for the integrations, `teanode domain list` for the domains, and so on.
See `docs/reference/command-line.md`.

## Sessions and tokens

Neither is configuration. Both are rows in the database, alongside the mail:
`session` for the browsers signed in, `token` for the API tokens that let this
tool administer the server from elsewhere.

A cookie and a token are the same shape — an identifier, a secret, and a
signature over both — and only a hash of the secret is stored, so a copy of
the database is not a set of working logins. They are signed with different
keys: sessions with `session.key`, tokens with `server.secret`. Rotating
`session.key` therefore still ends every session on the server, on every
account, which is the break-glass it has always been; it leaves tokens alone.

Both record when they were last used and from where, at most once a minute, so
the dashboard can show it without a database write per request. A revoked one
is kept for thirty days, marked revoked, so the list can say what happened to
it; an hourly sweep removes those and anything long expired.

Each account manages its own. `teanode auth login` issues one through the
dashboard and keeps it as a profile on the machine that asked. The console is
the exception — it authenticates with a token minted from the server secret
and is not an account at all, so `teanode token create --user ziyan laptop`
is how somebody gets their first one without a browser.

## Secrets

`server.secret`, `session.key`, the DKIM private keys, the ACME account key and
any AWS credentials are all stored with everything else. Two of them are worth
understanding before you move a server:

- **`server.secret`** signs every SMTP credential password. Carry it to the new
  database or every device you have configured stops being able to send.
- **`session.key`** signs dashboard cookies. Losing it logs everybody out,
  which is all it does.

Both are generated on the first start and never regenerated. A second instance
starting against the same database adopts them rather than making its own.

The API never returns any of them. Fields carrying a secret are redacted on the
way out, so a token with full access still cannot read them back. `config
export` does write them in the clear, because a backup that left them out
would not restore a working server; the file it writes is readable only by you.

## Paths

Relative paths resolve against `server.dataDirectory`, which has to be
absolute. It used to be allowed to be relative, resolving against the directory
holding `teanode.yaml`; with no file there is nothing to resolve it against,
and a relative path would land wherever each process happened to be started
from.

## Reference

### `server`

**`name`** — Name announced in the SMTP greeting and used as the HELO name
when sending, for example "mail.example.com". It must resolve to this host and
its reverse DNS should match, or receiving servers will distrust mail from
here.

**`dataDirectory`** — DataDirectory holds everything the server writes that is
not in the database: keys, certificates, the message spool and the secret.
Relative paths elsewhere in this file resolve against it.

**`logLevel`** — LogLevel is one of DEBUG, INFO, NOTICE, WARNING, ERROR,
CRITICAL.

**`logDirectory`** — LogDirectory, when set, receives a copy of every received
message as a .eml file. Useful when debugging; it grows without bound.

**`secret`** — Secret signs the bounce return path on outgoing mail and the
passwords derived from SMTP credential keys. Generated on first run.  Changing
it invalidates every SMTP password and orphans bounces for mail already in
flight, so when moving a server to a new machine this has to come with it.

**`mailServers`** — MailServers are the hosts to publish in every domain's MX
records, in order of preference. Optional; when empty the MX record names this
server, which is right for the single-host deployment most people run.  Set it
when mail for these domains arrives at more than one name — a pair like mx1
and mx2 pointing at the same host is common, and gives somewhere to move to
without every domain having to change its DNS. The dashboard then asks each
domain for one MX record per name, at preference 10, 20 and so on in the order
given.  These are names mail arrives at. They are unrelated to tls.hosts,
which is the names this server holds a certificate for.

### `listen`

**`smtpIncoming`** — SMTPIncoming receives mail from the internet. Port 25 in
production.

**`smtpOutgoing`** — SMTPOutgoing receives authenticated mail from your own
devices for relaying. Port 587 in production.

**`http`** — HTTP serves the dashboard and answers ACME http-01 challenges.
Port 80 must be reachable from the internet when tls.acme.challenge is
http-01.

**`https`** — HTTPS serves the dashboard over TLS.

**`debug`** — Debug, when set, serves Go pprof endpoints. Bind it to localhost
only.

### `tls`

**`hosts`** — Hosts to obtain certificates for. The first is the primary name.

**`certificateFile`** — CertificateFile and PrivateKeyFile point at PEM files
you manage yourself. When both are set, ACME is not used.

**`privateKeyFile`** — See the field above; the two are set together.

**`acme`** — ACME obtains certificates automatically from Let's Encrypt or
another ACME provider.

### `tls.acme`

**`enabled`** — See the field above; the two are set together.

**`email`** — Email is the contact address registered with the ACME provider;
it receives expiry warnings.

**`directoryUrl`** — DirectoryURL is the ACME provider. Point it at the Let's
Encrypt staging directory while testing to avoid rate limits.

**`challenge`** — Challenge is how domain control is proven: "http-01" needs
port 80 reachable, "tls-alpn-01" needs port 443, "dns-01" needs a DNS provider
below and is the only way to obtain a wildcard certificate.

**`perDomain`** — PerDomain obtains a certificate for each domain's own mail
server name, as well as the server's own. Without it every domain is served
the server's certificate, which names a domain the sender did not ask for.
Off by default, so that upgrading a server does not make it ask a certificate
authority for one certificate per domain it serves without anybody deciding
to.

**`accountKey`** — AccountKey identifies this server to the certificate
authority. Generated on first use and kept here with the other secrets; losing
it means registering again, which works but spends rate limit.

**`certificate`** — Certificate and PrivateKey hold the issued certificate, in
PEM. They are written here by the renewal, so an exported configuration is the
whole of a working server: restoring one elsewhere keeps the certificate
instead of asking the authority for another and spending rate limit.  This is
the opposite choice from tls.certificateFile above, and for a reason: these
are written by this server and read by nothing else, whereas a certificate you
manage yourself is written by something else and has to stay where that
something else puts it.

**`privateKey`** — See the field above; the two are set together.

**`route53`** — Route53 solves dns-01 challenges using an AWS hosted zone.

### `tls.acme.route53`

**`enabled`** — See the field above; the two are set together.

**`zoneId`** — ZoneID is the hosted zone that contains the records for
tls.hosts.

**`region`** — See the field above; the two are set together.

**`endpoint`** — Endpoint points at an S3-compatible service that is not AWS,
for example `http://minio:9000`. Empty means AWS. Self-hosting the object store
is what makes several instances able to share a spool without sharing a
filesystem, and without an account anywhere.

**`pathStyle`** — PathStyle addresses the bucket as `endpoint/bucket`. Implied
when an endpoint is set, and only worth stating to be explicit.

**`accessKeyId`** — AccessKeyID and SecretAccessKey are AWS credentials kept
here with the other secrets, so that one file is the whole of a working
server.  Leave both empty to use the default AWS credential chain instead: the
environment, a shared credentials file, or an instance role. On EC2 an
instance role is the better answer, because there is no long-lived secret to
leak.

**`secretAccessKey`** — See the field above; the two are set together.

**`credentialsFile`** — CredentialsFile is an AWS shared credentials file, as
an alternative to the two fields above.

**`nameservers`** — Nameservers to query when checking that a challenge record
has propagated, for example "ns-1.example.net:53".

### `database`

Not stored with the rest — a connection to the database cannot be kept in the
database. It comes from `TEANODE_DATABASE_URL` and the variables beside it,
described above. The fields are listed here because they are what those
variables set, and because `config show` prints them.

**`host`** — See the field above; the two are set together.

**`port`** — See the field above; the two are set together.

**`user`** — See the field above; the two are set together.

**`password`** — See the field above; the two are set together.

**`name`** — See the field above; the two are set together.

**`sslMode`** — SSLMode is passed to the PostgreSQL driver: disable, allow,
prefer, require, verify-ca or verify-full. `require` encrypts the connection
but believes whatever answers on the port; `verify-full` also checks that the
certificate is signed by `sslRootCertificate` and names the host being dialled,
which is what stops something else on the network from answering as the
database.

**`sslRootCertificate`** — SSLRootCertificate is the PEM file the server's
certificate is checked against, for the two verify modes. The compose file
generates one and mounts it at `/certs/server.crt`; a managed PostgreSQL will
publish its own. Empty means the system trust store, which a self-signed
certificate is not in.

**`logQueries`** — LogQueries echoes every SQL statement to the log. Very
noisy.

### `smtp`

**`trustedSenders`** — TrustedSenders are domains whose mail skips the
greylisting delay applied to unknown senders.

**`maxMessageSize`** — MaxMessageSize is the largest message accepted, for
example "70MB".

**`maxRecipientsIncoming`** — MaxRecipientsIncoming limits recipients per
inbound message; a low value frustrates address harvesting.

**`maxRecipientsOutgoing`** — MaxRecipientsOutgoing limits recipients per
relayed message.

**`greylistDelay`** — GreylistDelay is how long an unknown sender is stalled
before the message is accepted. Zero disables the delay.

**`requireReverseDns`** — RequireReverseDNS refuses incoming mail from an
address with no reverse DNS record that resolves back to it. On by default: it
is cheap, and most spam comes from hosts that have none.  Turn it off when
this server does not see the real client address — behind a load balancer, on
a private network, or in a container network during a test — because there the
check refuses everything.

**`socks5Proxy`** — SOCKS5Proxy, when set, routes outbound SMTP through a
proxy. Useful where the host's own IP address has a poor reputation or port 25
is blocked outbound.

**`disableSend`** — DisableSend stops the server from actually delivering
mail. Deliveries are recorded and left undelivered. Use on a development
machine.

**`authRateLimit`** — AuthRateLimit is how many authentication attempts one
address may make per minute on the submission port, and AuthRateBurst how many
it may make at once before that rate applies.  Verifying a credential is an
HMAC and a comparison, which is fast, so without a limit an address can guess
at whatever rate the network allows. The defaults let a mail client retry a
mistyped password several times and stop a program working through a list.
Zero for either disables the limit.

**`authRateBurst`** — How many attempts one address may make at once before
`authRateLimit` starts applying. A mail client retrying a mistyped password
should never see this; a program working through a list should reach it in the
first second.

### `smtp.submission`

The address a mail client should be told to connect to, which is what the
dashboard shows beside a new credential.

Normally it follows the server — `server.name`, and the port in
`listen.smtpOutgoing` — and both of these can be left empty. Set them when
something forwards a different port: a container publishing 10587, or a
firewall taking 587 on the outside. Without it the dashboard hands somebody
the port the process happens to bind, which is not the one their phone can
reach.

It changes nothing about what the server does. Only what it says.

**`host`** — What to connect to. Empty means `server.name`.

**`port`** — What port to connect on. Empty means the port in
`listen.smtpOutgoing`.

### `smtp.relay`

Hands outgoing mail to one mail server instead of delivering it by looking up
the recipient's MX and connecting on port 25.

Most people need this because their connection blocks outbound 25 — almost
every domestic ISP and many hosting providers do, since that is how a
compromised machine sends spam. A relay is reached on a submission port
instead.

It is also how this server sends through a provider. Amazon SES, Postmark and
Resend all offer an SMTP endpoint, so there is nothing provider-specific to
configure and the message arrives carrying the DKIM signature this server
already applied. Their HTTP APIs are a different matter: only SES accepts a
built message as-is, and Postmark and Resend take decomposed fields, which
means re-signing by them and no way to preserve yours. Use their SMTP
endpoints.

| Provider | Host | Port | Security |
| --- | --- | --- | --- |
| Gmail | `smtp.gmail.com` | 587 | `starttls` |
| Amazon SES | `email-smtp.<region>.amazonaws.com` | 587 or 2587 | `starttls` |
| | | 465 or 2465 | `tls` |
| Postmark | `smtp.postmarkapp.com` | 587, 2525 or 25 | `starttls` |
| Resend | `smtp.resend.com` | 587 or 2587 | `starttls` |
| | | 465 or 2465 | `tls` |

SES wants SMTP credentials derived from an IAM user, not the access key
itself, and they are per region. Resend's username is the literal string
`resend` with your API key as the password. Postmark uses its server token as
both.

One thing to know about SES: it overwrites `Message-ID` and `Date`, so a
signature covering those breaks. This server does not sign them.

Mail forwarded by an alias of kind `mailServer` is not affected — that names
its own destination, which is the point of it.

**`enabled`** — Whether outgoing mail goes through the relay.

**`host`** — The mail server to hand it to.

**`port`** — Usually 587, 465 or 2525.

**`security`** — How TLS is used: `starttls` for 587 and 2525, `tls` for 465,
or `none`.

Unlike delivering to a stranger's MX, the certificate is checked against the
host for both encrypted modes. There is a name to check and a password about
to be sent, so accepting any certificate would mean handing that password to
whoever answered. `none` does not insist — STARTTLS is still used when it is
offered — and is refused outright when a password is set.

**`username`** and **`password`** — What it authenticates as. Leave both empty
for a relay that authorises by address.

### `dkim`

**`selector`** — Selector to give a newly created domain's key. It appears in
DNS as `<selector>._domainkey.<domain>`, so it only has to be unique within the
domain, and changing it here does not affect domains already created.

### `domains[]`

**`id`** — ID identifies the domain in stored mail, deliveries and usage rows,
and in dashboard URLs. It is the domain name: unique already, stable for as
long as the domain is configured, and readable wherever it appears.  Older
configurations carry a generated identifier here instead, and keep working —
the rows that reference it still match. Changing it means updating those rows
too, so nothing rewrites it automatically.

**`domain`** — Domain is the mail domain itself, for example "example.com".

**`subdomain`** — Subdomain is the label whose CNAME points at this server, so
that bounces and DMARC reports have somewhere to arrive. Usually "mail",
making mail.example.com a CNAME to server.name.

**`linkHost`** — LinkHost is the name in the addresses this server writes into
mail it sends — today the pictures in a template, each one an address belonging
to a single message. Empty means the domain's first mail server name, which is
right when this server answers HTTPS on it. It often does not: a mail server
name resolves to a host whose port 443 belongs to something else entirely, and
then every picture in every message is broken while the mail itself is fine.
The name here is a way to say where this domain's HTTPS actually is — the site
on the apex behind a CDN, a name pointed at this server for the purpose —
without moving where its mail arrives. It has to be a name that reaches this
server over HTTPS with a certificate a mail program will accept, and it has to
be under this domain: an address in somebody else's domain tells the reader who
runs the server, which is the thing per-domain names exist to stop.

**`comment`** — Comment is a note for the operator; it is never used in mail
handling.

**`dkim`** — DKIM is the key that signs mail sent from this domain. It is
generated when the domain is created; the matching public key has to be
published in DNS, which the dashboard shows you.

**`spamFilterScoreThreshold`** — SpamFilterScoreThreshold is the SpamAssassin
score at or above which mail is rejected. Only meaningful when antispam is
enabled.

**`aliases`** — Aliases decide where mail for this domain goes. Every alias
whose pattern matches produces a delivery, so one address can forward to
several places; an alias with an empty pattern is a catch-all and receives
only what nothing else matched.

**`credentials`** — Credentials may authenticate to the submission port and
send mail as this domain.

### `domains[].dkim`

**`selector`** — Selector this key is published under.

**`privateKey`** — PrivateKey is a PKCS#8 RSA key in PEM form.

### `domains[].aliases[]`

**`id`** — ID is generated once and never changes; stored deliveries reference
it.

**`pattern`** — Pattern is a Go regular expression matched against the local
part of the recipient address, the part before the "@", without regard to
case. Anchor it: "^hello$" matches only hello@, while "hello" also matches
say-hello-now@.  An empty pattern makes this a catch-all. Catch-alls are a
fallback: they receive mail only for addresses that no pattern matched, so
adding one does not duplicate mail that already has somewhere to go.

**`comment`** — Comment is a note for the operator.

**`kind`** — Kind is one of null, email, webhook or mailServer.

**`email`** — Email is the destination address when kind is email.

**`webhook`** — Webhook is the destination URL when kind is webhook.

**`mailServer`** — MailServer is the destination server when kind is
mailServer.

**`disabled`** — Disabled stops the alias from matching without deleting it.

### `domains[].aliases[].mailServer`

**`host`** — See the field above; the two are set together.

**`port`** — See the field above; the two are set together.

**`username`** — See the field above; the two are set together.

**`password`** — See the field above; the two are set together.

### `domains[].credentials[]`

**`id`** — ID identifies the domain in stored mail, deliveries and usage rows,
and in dashboard URLs. It is the domain name: unique already, stable for as
long as the domain is configured, and readable wherever it appears.  Older
configurations carry a generated identifier here instead, and keep working —
the rows that reference it still match. Changing it means updating those rows
too, so nothing rewrites it automatically.

**`key`** — Key is the secret half of the credential.

**`alias`** — Alias, when set, restricts this credential to sending as exactly
that local part of the domain. A credential for "noreply" cannot then send as
anybody else, which limits the damage if it leaks.

**`comment`** — Comment names the device or service that holds this
credential.

**`disabled`** — Disabled refuses authentication without deleting the
credential.

### `users[]`

**`username`** — See the field above; the two are set together.

**`passwordHash`** — PasswordHash is a bcrypt hash. Generate one with "teanode
password".

**`email`** — Email receives notifications, such as a domain whose DNS records
have stopped resolving. Optional.

**`tokens`** — Tokens authenticate this person's command line client, and act
as them. Removing the account removes them with it.

### `session`

**`key`** — Key signs session cookies. Generated on first run; replacing it
logs everybody out, which is the way to do that deliberately.

**`lifetime`** — Lifetime is how long a login lasts.

### `dns`

**`nameserver`** — Nameserver to query, as host:port. A public resolver is a
reasonable default because these are public records.

**`checkInterval`** — CheckInterval is how often every configured domain is
re-checked.

**`externalAddressServices`** — ExternalAddressServices are asked what address
this server appears to come from, which is what its DNS records have to point
at. A server cannot work this out alone: the address on its interface is
usually private, and only something outside can say what a sending mail server
sees.  Each is tried in turn until one answers. Empty disables the lookup, and
the dashboard then asks the operator for the address instead.

### `antivirus`

**`enabled`** — See the field above; the two are set together.

**`host`** — See the field above; the two are set together.

**`port`** — See the field above; the two are set together.

### `antispam`

Spam scoring. The score a message is compared against is the domain's
`spamFilterScoreThreshold`, not a setting here.

**`enabled`** — Whether messages are scored at all.

**`engine`** — What does the scoring: `builtin` for the filter inside this
server, or `spamd` for an external SpamAssassin daemon. Leaving it empty is
resolved rather than defaulted, so that an existing deployment keeps working
without being edited: empty with a host configured means `spamd`, and empty
with no host means `builtin`.

**`spamd`** — Where the external daemon listens, used when `engine` is
`spamd`. Its `host` and `port` are the two fields.

**`host`**, **`port`** — Deprecated: use `spamd`. Kept because it is what
deployments in the field have stored, and it still works.

**`builtin`** — The filter inside this server. It scores from what the server
already knows, from public block lists, from a classifier trained on your own
mail, and optionally from public pattern rules. The four groups below can be
turned on and off independently.

**`signals`** — Scoring from what the server already established about a
message: its authentication results, the sending host's confirmed reverse DNS
name, and the name it gave in HELO. Costs no lookups, because all of it is
computed before scoring begins. Its one field is `enabled`.

**`dns`** — Reputation lookups in public block lists. A block list is queried
with an ordinary DNS lookup, so this needs no service of its own.

**`timeout`** — Bounds the whole set of block list lookups for one message.

**`addressLists`** — The lists consulted about the connecting address. Each
entry has a `zone` and a `weight`.

**`domainLists`** — The lists consulted about domains found in the message.
Each entry has a `zone` and a `weight`.

**`zone`** — The suffix queries are built with, for example
`zen.spamhaus.org`.

**`weight`** — The points a listing contributes.

**`maximumDomains`** — Caps how many domains from one message are looked up,
so that a message full of links is not a burst of DNS queries.

**`bayes`** — A classifier trained on this server's own mail, from messages
marked as spam or not spam in the dashboard. It is usually the most accurate
part of a spam filter, because it learns the mail you actually get.

**`minimumMessages`** — How many messages must have been learned before the
classifier is allowed to contribute anything. A classifier trained on four
messages is confidently wrong.

**`rules`** — Public pattern rules, downloaded into the database and evaluated
in this process. Off by default: an upgrade should not begin downloading and
running rule files nobody asked for.

**`channels`** — The update channels to fetch, by name.

**`updateInterval`** — How often to look for a new version of the rules.

**`maximumEvaluationTime`** — Bounds one message's whole rule pass. Thousands
of patterns run over text an attacker chose, so this is a limit rather than a
target.

The rule data published on `updates.spamassassin.org` is produced by the
Apache SpamAssassin project and licensed under the Apache License 2.0. The
built-in filter is a different program and is not SpamAssassin; it only reads
that published rule data when you enable it.

### `geoip`

**`enabled`** — See the field above; the two are set together.

**`databaseFile`** — DatabaseFile is a MaxMind .mmdb file.

### `storage`

**`directory`** — Directory holds the raw messages, relative to
server.dataDirectory. They are kept out of the database because they are
large, are never queried, and would make a backup expensive.

**`spoolRetention`** — SpoolRetention is how long a message is kept, which is
how far back the dashboard can show content and how long a stalled delivery
can still be retried. Zero keeps messages forever and eventually fills the
disk.

**`s3`** — See the field above; the two are set together.

### `storage.s3`

**`enabled`** — See the field above; the two are set together.

**`bucket`** — See the field above; the two are set together.

**`region`** — See the field above; the two are set together.

**`endpoint`** — Endpoint points at an S3-compatible service that is not AWS,
for example `http://minio:9000`. Empty means AWS. Self-hosting the object store
is what makes several instances able to share a spool without sharing a
filesystem, and without an account anywhere.

**`pathStyle`** — PathStyle addresses the bucket as `endpoint/bucket`. Implied
when an endpoint is set, and only worth stating to be explicit.

**`accessKeyId`** — AccessKeyID and SecretAccessKey are AWS credentials kept
here with the other secrets, so that one file is the whole of a working
server.  Leave both empty to use the default AWS credential chain instead: the
environment, a shared credentials file, or an instance role. On EC2 an
instance role is the better answer, because there is no long-lived secret to
leak.

**`secretAccessKey`** — See the field above; the two are set together.

**`credentialsFile`** — CredentialsFile is an AWS shared credentials file, as
an alternative to the two fields above.

### `passkey`

**`enabled`** — Enabled offers passkeys on the sign-in form and in the
account's settings. Off by default: a server reached over plain HTTP, or on a
name that is about to change, is one where registering a passkey would create
something that cannot be used later.

**`relyingPartyId`** — RelyingPartyID is the domain a credential is bound to,
without a scheme or a port — `mail.example.com`, or `example.com` to let the
same passkey work on every subdomain. Empty means `server.name`. WebAuthn binds
a credential permanently, so a passkey registered against the wrong name is one
that will never work again and cannot be repaired, only deleted.

**`displayName`** — DisplayName is what the browser shows when it asks somebody
to create or use a passkey. Empty means `server.name`.

**`origins`** — Origins are where the dashboard is served from, each with its
scheme and any non-default port: `https://mail.example.com`. An assertion from
an origin that is not listed is refused, which is what stops a page on another
site from using these credentials. Empty means `https://` and the relying
party.

**`maximumPerUser`** — MaximumPerUser bounds how many an account may register.
Zero means five, which is a phone, a laptop, a security key and room to replace
one before removing it.

### `passkey.redis`

Where half-finished passkey sign-ins wait. A WebAuthn sign-in is two requests,
and behind a load balancer the browser has no reason to come back to the
instance it started with, so the challenge minted by the first has to be
somewhere the second can find it. With one instance leave the address empty and
the challenges stay in the process; nothing durable is kept here either way,
and losing the lot costs one retry.

**`address`** — Address as host:port, for example `redis:6379`. Empty disables
it.

**`username`** — Username, when the server requires one.

**`password`** — Password, when the server requires one.

**`database`** — Database number, zero unless something else shares the server.

### `upgrade`

**`enabled`** — Enabled asks the release list what the newest version is, on
CheckInterval, and shows it in the dashboard. One HTTPS request to a public
endpoint, carrying nothing about this deployment. On by default: knowing that a
version exists is not the same as installing it, and an operator who is never
told is an operator running last year's bugs.

**`automatic`** — Automatic installs what it finds without being asked:
download, verify against the release's checksums, replace this binary, restart.
Off by default, because a release can change how mail is handled and nobody
installs a mail server expecting it to change underneath them. It takes any
newer release, minor and major alike — a rule that stopped at a minor version
would be a rule that quietly stopped upgrading.

It is refused, with the reason shown in the dashboard, where there is nowhere
to put the new binary: a deployment whose executable it cannot write over and
which has not been given a writable staging directory in
`TEANODE_UPGRADE_DIRECTORY`. A container is not refused — it stages onto its
volume and runs that at the next start, so an upgrade survives a recreate — but
a container that was never given such a volume is, and there
`docker compose pull` is the answer.

The restart does not need a supervisor. The process replaces its own image with
the new binary, keeping the same arguments and environment, once everything has
been drained and closed — so a server started by hand upgrades itself as well
as one under systemd.

**One limitation, and it matters most to the deployment this is easiest to turn
on for.** A release that crashes before it finishes starting is recovered from
automatically only where the new binary is staged rather than written over the
old one — a container runs the binary in its image again and says why. Where
the binary was replaced in place, there is nothing left to fall back to on its
own: a supervisor will restart the broken binary until somebody stops it. The
binary it replaced is kept beside it, so the recovery is one command,

    mv /usr/local/bin/teanode.previous /usr/local/bin/teanode

and then a restart. Automatic upgrades are worth more on a deployment that
stages, and worth thinking about twice on one that does not.

**`checkInterval`** — CheckInterval is how often to look. Six hours by default:
often enough that a security release is noticed the same day, rarely enough
that it is not a request anybody would notice. Read once at startup, so
changing it takes a restart — the dashboard says so. `enabled`, `automatic` and
`window` are re-read every time the loop wakes and take effect without one.

The loop itself wakes more often than this and asks nothing most of the time.
That is what makes `window` work: a check every six hours happens at four fixed
times a day, and a two-hour window would be hit or missed depending on when the
process started.

**`window`** — Window restricts automatic upgrades to a time of day, in local
time, as `02:00-04:00`. It may cross midnight. Empty means any time. An upgrade
restarts the server, which takes a few seconds during which mail is not
accepted — senders retry, but a busy hour is still a worse time than a quiet
one.

What verification means here, said plainly: the binary and the release's
`SHA256SUMS` are fetched over HTTPS from the repository this server was built
from, and the hash must match. That proves the bytes are the ones GitHub is
serving for that release and that the download was not corrupted. It does not
prove a human meant to publish them: anybody who can publish a release to that
repository can publish a binary, and this will install it. The repository is
compiled in rather than configured, so a stolen dashboard session cannot point
a server at somebody else's builds.

### `users[].tokens[]`

**`id`** — ID identifies the token, and is the half of the token string that
is not secret. It appears in the log, so a token can be traced back to an
entry here and revoked.

**`name`** — Name says what holds it, for example "laptop". Not unique.

**`hash`** — Hash is the SHA-256 of the secret half, hex encoded. A token is
32 random bytes, so there is nothing for a slow hash to protect against —
unlike a password, it cannot be guessed.

**`created`** — Created records when it was issued, for the operator's
benefit.

**`expires`** — Expires, when set, is when it stops working.

