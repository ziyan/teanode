# Changelog

Notable changes to TeaNode. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.2] - 2026-09-05

### Fixed

- The dashboard's mail list could fail to load the "opened" column when one of
  the messages it was showing had just been deleted by the retention sweep.
  Nothing was lost and the server was never at risk — the request failed and
  reloading fixed it — but it recurred for as long as the list was open. (#20)
- Log lines from the outbound SMTP client — everything about delivering a
  message to another server — were labelled `smtpd`, the name of the listener
  that receives mail, because the package declared its logger under the wrong
  name. They are labelled `smtpc` now. If you grep your logs for delivery
  problems, that is the word that changed. (#20)

## [0.4.1] - 2026-09-05

### Fixed

- The version card shows the release notes for the newest release, formatted
  as a changelog rather than as raw text, and shows them whether or not that
  release is newer than the one running. They were only ever displayed when an
  upgrade was available, which meant never on a server that was up to date or
  ahead. (#19)

## [0.4.0] - 2026-09-04

### Added

- Every setting in the configuration except the database connection can now be
  read and changed from the dashboard and the command line: the message size
  and recipient limits, greylisting, the sign-in rate limits and trusted
  senders, the DNS resolver and check interval, the session lifetime, the
  passkey relying party, the listen addresses, the server's own name, mail
  server names and log level, the message directory and spool retention, the
  GeoIP database, and the ACME contact address, challenge, directory and
  certificate files. Secrets are never returned. Settings that only take effect
  on a restart say so where they are edited, and the listen addresses ask for
  confirmation before saving. (#18)

### Changed

- A domain is one page with tabs — Overview, Settings, Aliases, Credentials,
  Templates — each with its own address, so any of them can be linked to,
  reloaded and reached with the back button. Aliases and credentials are one
  click from the domain instead of four screens down its settings page. Old
  links to `/domains/<id>` and `/domains/<id>/settings` redirect. (#18)
- The Server page's tabs are grouped around the question each answers, and the
  tab that was called DNS is Certificates. `/server/dns` redirects. (#18)
- `Remove` at the end of a row is a trash icon; a long DNS value is clamped to
  its line with a button that copies the whole of it. (#18)

### Fixed

- The last row of every list sat flush against the bottom of its own card. (#18)
- Explanatory text ran the full width of the window in some places and stopped
  at a readable measure in others, and a card could be three times as wide as
  the writing in it. (#18)
- Navigating between two domains could show "Failed to fetch" over a page that
  had loaded correctly. A dropped connection is also retried once now, which a
  browser will not do for a POST. (#18)
- Moving between a domain's pages briefly flashed "Domains" as the page
  heading. (#18)

## [0.3.1] - 2026-09-04

### Fixed

- A server running a build made from a checkout — `0.2.0-7-g8519250`, what
  `make build` stamps in when the commit is not exactly on a tag — is offered a
  release that has overtaken it. It was told about none: the version card said
  the newest release was available, which reads as up to date, and there was no
  button to press. The tag such a build was made past is still not offered, and
  a server already on one has to install this release by hand before the
  dashboard can do it for the next. (#17)

## [0.3.0] - 2026-09-04

### Added

- `teanode auth login --url https://mail.example.com` signs the client in from
  a browser: the dashboard opens, the operator presses Authorize, and the
  token comes back to the command over a loopback connection on the
  operator's own machine, tied to a nonce the page has to echo. The result
  is a profile in `~/.config/teanode/profiles.json`, one per server, with
  `auth list`, `auth switch`, `auth status` and `auth logout`, which revokes
  the token. `--profile` and `TEANODE_PROFILE` pick another for one command.
- A command for every part of the API: `domain`, `alias`, `settings`,
  `server`, `upgrade`, `session`, `passkey`, `mail`, `delivery`, `report`,
  `template` and `layout` join `user`, `token`, `credential` and `dkim`, each with
  `list`, `get`, `create`, `update` and `delete` where the API has them and
  the verbs particular to the resource — `alias match`, `domain check`,
  `mail send`, `template render`, `delivery pending`, `server restart`.
  Tables by default, `--json` everywhere. `teanode api` remains for whatever
  is added later.
- The dashboard's `/cli` page, which the client opens to sign in. It is the
  one page allowed to connect to a loopback address.
- A macOS build of the client in each release.

### Changed

- The server is `teanode-server`; `teanode` is the client. `teanode run`,
  `teanode config …` and `teanode tls …` are `teanode-server run`, `config`
  and `tls`, and `teanode user --offline` is `teanode-server user`. The
  image ships both, so `docker compose exec teanode teanode user list` still
  works. A systemd unit or a script that starts the server has to say
  `teanode-server`.
- The client no longer reads `~/.config/teanode/token`; a token kept there
  is saved as a profile with `teanode auth login --url … --token -`.
  `TEANODE_URL` and `TEANODE_TOKEN` still bypass profiles for scripts.
- `teanode user add` and `credential add|remove` keep working as aliases of
  `create` and `delete`; `credential delete` takes the identifier alone.
### Added

- `teanode auth login --url https://mail.example.com` signs the client in from
  a browser: the dashboard opens, the operator presses Authorize, and the
  token comes back to the command over a loopback connection on the
  operator's own machine, tied to a nonce the page has to echo. The result
  is a profile in `~/.config/teanode/profiles.json`, one per server, with
  `auth list`, `auth switch`, `auth status` and `auth logout`, which revokes
  the token. `--profile` and `TEANODE_PROFILE` pick another for one command. (#15)
- A command for every part of the API: `domain`, `alias`, `settings`,
  `server`, `upgrade`, `session`, `passkey`, `mail`, `delivery`, `report`,
  `template` and `layout` join `user`, `token`, `credential` and `dkim`, each
  with `list`, `get`, `create`, `update` and `delete` where the API has them
  and the verbs particular to the resource — `alias match`, `domain check`,
  `mail send`, `template render`, `delivery pending`, `server restart`.
  Tables by default, `--json` everywhere. `teanode api` remains for whatever
  is added later. (#15)
- The dashboard's `/cli` page, which the client opens to sign in. It is the
  one page allowed to connect to a loopback address. (#15)
- A macOS build of the client in each release. (#15)

### Changed

- The server is `teanode-server`; `teanode` is the client. `teanode run`,
  `teanode config …` and `teanode tls …` are `teanode-server run`, `config`
  and `tls`, and `teanode user --offline` is `teanode-server user`. The
  image ships both, so `docker compose exec teanode teanode user list` still
  works. A systemd unit or a script that starts the server has to say
  `teanode-server`. (#15)
- The client no longer reads `~/.config/teanode/token`; a token kept there
  is saved as a profile with `teanode auth login --url … --token -`.
  `TEANODE_URL` and `TEANODE_TOKEN` still bypass profiles for scripts. (#15)
- `teanode user add` and `credential add|remove` keep working as aliases of
  `create` and `delete`; `credential delete` takes the identifier alone. (#15)

## [0.2.0] - 2026-09-04

### Added

- **The server says when a new version is out, and installs it.** The Server
  page shows what is running, what has been released, the notes that came with
  it and a link to the release, and a button that installs it: downloaded,
  checked against the checksums published with it, put in place, and the server
  restarts into it — no supervisor needed, because the process replaces its own
  image once everything has been drained and closed. `upgrade.automatic` does
  the same on a schedule, off until you turn it on, optionally confined to an
  hour of the day by `upgrade.window`. (#3)
- **The dashboard notices when it has gone stale.** A yellow refresh button
  appears at the top of the rail when the server has been upgraded under the
  page you are looking at, and a dot appears on Server when a release is
  waiting. (#3)

### Changed

- **An older binary no longer reverts a newer one's migrations without being
  asked.** This program undoes migrations it does not recognise, which is how a
  deliberate downgrade works — and it cannot tell one from an upgrade that
  crashed on startup, a second instance that never got the upgrade, or somebody
  pulling last week's image to test something. A start that meets a migration
  it does not have now refuses: nothing is migrated and nothing is opened, and
  the message names the migrations, says what reverting would cost, and gives
  `TEANODE_ALLOW_MIGRATION_REVERT=true` for a downgrade you actually want. (#3)
- **Setup, Integrations and Server are one page.** They were three rows in the
  rail for one subject — what this server is, what it talks to, and which
  version it is running — which made you choose between them before knowing
  which one held the thing you wanted. They are tabs of `/server` now, Setup
  first and About last, and the old addresses redirect. (#3)

## [0.1.2] - 2026-09-03

### Changed

- `deploy/docker-compose.yml` pulls `ghcr.io/ziyan/teanode` rather than naming
  a locally built image, so `docker compose up -d` works on a machine that has
  never built this. `docker compose build` still builds the checkout. The tag
  is `latest` and the comment beside it says to pin a version: an upgrade to a
  mail server should be a thing you did, on a day you chose. (#2)

## [0.1.1] - 2026-09-03

### Fixed

- The account menu opens on a phone. It is drawn at a layer below the
  navigation rail, and on a narrow screen the rail is an overlay rather than a
  column — so the menu opened behind the rail that had just been tapped:
  invisible, with nothing on it reachable. (#1)
- The row of tabs on a message no longer runs off the side of a phone. It
  scrolls sideways instead, and the link that downloads the `.eml` is not
  offered there: a phone has little use for the file, and in a scrolling row it
  sat past the end where nobody would find it. (#1)
- Lists in the dashboard stack on a phone rather than putting a button beside a
  paragraph and leaving each of them a column four words wide. (#1)
- Every chevron in the breadcrumb has the same air on both sides. The gap fell
  on one side only, so the trail read as `Domains> example.com> Templates`. (#1)

## [0.1.0] - 2026-09-03

First open-source release. TeaNode began as the private backend of a hosted
service; this release turns it into something anybody can run for their own
domains. Almost everything below is a consequence of that.

### Fixed

- A domain can be changed one setting at a time. The domain name was a
  required field of `DomainParameters`, so the schema refused every update
  that did not carry it — before the resolver, which has always treated it as
  optional, saw the request. The dashboard sends only what it is changing, so
  saving the mail server names and moving the signing selector both failed,
  and the failure reached the operator as a button that did nothing.

- `domains[].mailServers` is written to the database. The reading half of the
  domain row mapping had the field and the writing half did not, so a domain
  configured with names of its own kept them until the configuration was next
  saved. A deployment could look correct while storing nothing, by deriving
  the same names for another reason.

- A per-message picture address is not cacheable. It set `no-store` and then
  the code that writes the bytes set a year of caching over the top of it. A
  CDN in front of the server duly kept a copy, after which the first fetch is
  counted and every one afterwards is answered by somebody else — which an
  operator reads as nobody having looked again.

- `POST /api/v1/send/{domain}/{template}` can be called again. The
  authentication middleware turned it away for having no session before the
  handler could check the credential it was called with, so it answered
  "not logged in" on any server with an account — which is every server.
  It is let through by path, the way the GraphQL endpoint is, and checks
  its credential itself.
- A message the server composes carries `MIME-Version`, wraps its base64
  at 76 columns rather than writing a part as one line longer than SMTP
  allows, and has a text part or an HTML part only when there is one: a
  `multipart/alternative` with an empty half is a message some clients show
  as blank.

- The port shown beside a new credential is the one a mail client can reach,
  not the one the process binds. Those are the same thing until something
  forwards one to the other — a container publishing 10587, a firewall taking
  587 — and then the dashboard was handing somebody a number nothing answers
  on. `smtp.submission` sets what to advertise, host and port, and both are
  editable on the Setup page; leaving them empty keeps the old behaviour of
  following the server.
- The Setup page no longer describes settings as living in `teanode.yaml` and
  being reloaded with a HUP signal. Neither has been true since configuration
  moved into the database.

- Links in a message can be clicked. The sanitiser had been putting
  `target="_blank"` on every link it kept since it was written, but the frame
  showing the message was sandboxed with `allow-same-origin` alone — and a
  browser silently drops a `_blank` click without `allow-popups`. Nothing
  reported an error; the link simply did nothing. The frame now also allows
  the opened tab to escape the sandbox, because a link that "works" and lands
  on a page with no scripts and no origin is worse than one that does not
  open. Scripts, top navigation and forms are still refused, so a message
  cannot run code, cannot navigate the dashboard away from under the reader,
  and cannot submit anything.

- Every page under `/settings` names itself in the breadcrumb and the tab
  title. The settings list already described itself as the one place a surface
  is declared, and the breadcrumb was documented as reading it, but nothing
  did: each page had to remember, and five of the six did not.
- A credential created through the dashboard can send mail immediately. The
  lookup tables are built on first read, and the new store did not mark them
  stale after a change, so a create — which reads the configuration to check
  for a duplicate before appending — left the new credential invisible to
  every lookup until the process restarted. Submitted mail was refused as
  "Invalid credentials" while the credential sat plainly in the configuration.
- Mail to a domain served by this same server is refused as a loop. The check
  compared the domain's MX records against `server.name`, and the MX records
  the dashboard asks an operator to publish name `server.mailServers` — a
  separate list. Wherever the two differ, which is the arrangement the panel
  itself recommends, nothing ever matched and the mail went round.
- The address records asked for are the ones the MX names, not `server.name`.
  With `server.mailServers` set those are different, and the page was asking
  for a record on a name nothing pointed at while not checking the names that
  mattered.
- An AAAA record is marked optional rather than missing. A server reachable
  over IPv4 alone is correctly configured, and colouring it the same as a
  missing MX teaches the reader to ignore the colour.
- A catch-all alias can be created again. An empty pattern is a catch-all —
  the configuration layer, the documentation and every existing deployment
  read it that way — but the API refused it as a missing value, while allowing
  an alias to be *edited* into one. The form now says an empty box catches
  everything else, rather than leaving it to be guessed.
- STARTTLS is only advertised when there is a certificate to complete it with.
  A server that has not obtained one yet — the first minutes of a new
  deployment — offered it anyway, and a sender that took the offer failed the
  handshake. Some retry immediately without encryption, so the mail arrived in
  plaintext and nothing recorded that it had.
- A published DKIM key is recognised when it omits the version tag. RFC 6376
  makes `v=` recommended rather than required, defaulting to DKIM1, so a
  record of the form `k=rsa; p=…` is valid and every verifier accepts one.
  The dashboard did not, and reported working keys as needing to be changed —
  which is worse than saying nothing, because it asks somebody to edit DNS
  that was already correct.
- A dns-01 certificate order waits for the challenge record to appear before
  asking the certificate authority to look for it. The wait polled a list of
  nameservers that is optional to configure, and when it was left empty the
  wait was skipped entirely — so every order failed. Unset now means "look up
  the zone's own nameservers", which is what the code always claimed to do.
- A failing certificate order gives up instead of retrying forever. Combined
  with the above it produced a new order every second or so, which is the
  fastest possible route to a certificate authority's failed-validation limit.
  Three attempts, backing off, then it waits for the next scheduled run.
- An unclaimed server no longer answers everything. It used to open the whole
  API while no account existed, so anyone reaching a freshly started server
  could read its domains, aliases and signing selectors, and then claim it.
  Creating the first account still works; nothing else does until somebody has.
- The container runs as an ordinary user rather than root, keeping only the
  capability to bind the low ports. **Upgrading:** the mounted configuration
  and data directories were created by root and have to be given to uid 65532
  once, with `chown -R 65532:65532`, or the server cannot read them.
- Every known vulnerability the code could reach is gone: the build now
  requires Go 1.25.14 and five dependencies moved forward. The one that
  mattered was reachable before authentication — a remote sender could spend
  the server's CPU through the address parser on any `RCPT TO`.
- A failed SMTP authentication no longer writes a working credential to the
  log. The error named the password that would have been accepted, and the
  client chooses the half the server does not derive, so one rejected login
  plus read access to a log file yielded a usable credential. Token
  verification had the same shape. Both now compare in constant time and say
  only that the credential was invalid.

### Added

- **Pictures in a template, served from your own domain.** Upload a picture in
  the layout or template editor and it is stored — on disk, and in the object
  store as well when one is configured — with a row in `media` holding what it
  is and which domain it belongs to. The editors insert it; the preview shows
  it; a megabyte is the limit; and only PNG, JPEG, GIF and WebP are accepted,
  decided by reading the bytes rather than believing the name. SVG is refused
  on purpose: it is a document that can carry script, and it would be served
  over HTTPS from your own domain.

- **Whether a message has been looked at.** Every picture in a message sent
  from a template gets an address of its own, under the sending domain, and a
  fetch of it is recorded — first time, last time, how many times, and from
  where. The mail list has a Pictures column and the message's page has a card.
  Both say what the number is worth: a mail program asked for a picture, which
  is neither "somebody read this" nor, when it is absent, "nobody did". Apple
  Mail fetches every picture before the recipient sees anything; most programs
  fetch none until the reader asks. It is a floor with false positives in it,
  and the dashboard says so where the number is.

- **`domains[].linkHost`, the name that serves a domain's pictures.** Where
  mail arrives and where HTTPS answers are different questions. A mail server
  name resolves to a host whose port 443 may belong to something else
  entirely, and then the mail is delivered, signed and aligned while every
  picture in it is broken — a failure that happens in the reader's mail
  program and is invisible from the server. Empty still means the first mail
  host, which is right when this server answers there. The name has to be
  under the domain: an address in somebody else's domain tells every reader
  who runs the server.

- **Writing and sending mail from the dashboard.** New message, on the Mail
  page, sends as any address at one of your domains: from a template with its
  variables filled in, or written there in rich text or plain text, with
  attachments. The message is signed with the domain's key, recorded under
  Mail like anything a credential submits, and the page links to it once it
  has gone. Behind it is a `SendMail` mutation, so the command line client
  can do the same.

- **Templates and layouts in the dashboard.** Each domain has a Templates
  page listing its templates and the layouts they sit in; each opens in an
  editor with a preview rendered by the server, with sample values for the
  variables it reads. The variables are reported by the API too
  (`Template.variables`), and `RenderTemplate` and `RenderLayout` preview
  content that has not been saved.

- **Templates and layouts in more than one language.** A template keeps its
  name and carries a translation of its subject and content per locale;
  a layout does the same for its content. Sending names a locale — the
  dashboard's language select, `"locale"` in the send endpoint's body, or
  the `locale` argument of `SendMail` — and the closest translation is
  used: `zh-CN` finds `zh-CN`, else `zh`, else any Chinese, else the
  default. The message says which in `Content-Language`. Migration
  `0006_translation` adds the two tables and a `locale` column to each of
  `template` and `layout`; a template with no translations behaves as
  before.

- **Signing in with a passkey.** WebAuthn: the server sends a challenge, an
  authenticator signs it, and the server checks the signature against a public
  key stored at registration. The private half never leaves the phone, laptop
  or security key, so there is no shared secret to leak, phish or reuse — and
  a copy of this server's database is not a set of working credentials, which
  is the one thing a password table can never say.

  Discoverable credentials, so no username is typed: the browser offers
  whichever passkeys it holds for the site. Off by default, because WebAuthn
  binds a credential to an origin permanently and a passkey registered against
  a name that is about to change can never be used again. Settings →
  Passkeys registers and removes them, up to `passkey.maximumPerUser`.

  Half-finished ceremonies wait in the process by default and in Redis when
  one is configured, which is what makes this work behind a load balancer:
  WebAuthn is two requests and the browser has no reason to come back to the
  instance it started with. `deploy/docker-compose.yml` has a Redis in the
  cluster profile. Nothing durable is kept there — keys that expire in five
  minutes and are deleted as they are read, so one challenge answers exactly
  one attempt.

- **A content security policy** on the dashboard, and the standard headers
  beside it. `default-src 'self'`, with `script-src` naming the hash of the
  one inline script the page ships — computed from what is embedded rather
  than written down, so editing the script cannot leave a stale hash behind.
  It is the third layer under the mail a stranger sends, after the sanitiser
  and the sandboxed frame, and the one that holds if either has a hole.

- **Remote images in a message are fetched by the server**, once the reader
  asks for them. Letting the browser fetch them hands the sender the reader's
  address, user agent and the exact moment the message was opened, which is
  what a tracking pixel is for; through the server they learn only that the
  mail server looked. It is also what lets the policy above say
  `img-src 'self'` and mean it.

  The address comes out of mail written by a stranger, so the fetch is
  guarded: http and https only, no credentials in the URL, every address
  actually dialled checked against the loopback, private, link-local and
  reserved ranges — including the ones a redirect causes, which is where this
  check usually has a hole — and the reply served as an image or not at all,
  capped and timed out.

- **A Profile page**, where an account's name, the username it signs in with,
  and its notification address can be changed. Renaming takes the sessions,
  API tokens and passkeys with it, so nothing has to be signed in again.

- **DMARC reports open.** The list answered "is anybody forging me, and is it
  working"; a report now opens to show who reported it, the policy they saw,
  and what they did with each batch of mail from each source — everything that
  was already parsed out of the XML and never shown.

- **An outgoing relay**, for the deployment whose connection blocks outbound
  port 25 — which is almost every domestic ISP, and many hosting providers.
  Outgoing mail is handed to one mail server on a submission port instead of
  being delivered by MX lookup: 587 and 2525 with STARTTLS, 465 with TLS from
  the first byte. Settings → Integrations has presets for Gmail, SES, Postmark
  and Resend, which fill in the host, port and encryption.

  The relay connection is checked, unlike delivery to a stranger's MX: the
  certificate is verified against the host, and a relay that will not encrypt
  is refused rather than fallen back from. There is a name to check and a
  password about to be sent, so accepting any certificate would mean handing
  that password to whoever answered. `security: none` is refused outright when
  a password is set.

  This is also how the server sends through a provider. SES, Postmark and
  Resend all offer SMTP endpoints, so there is nothing provider-specific to
  configure and the message arrives carrying the DKIM signature this server
  already applied. Their HTTP APIs are not equivalent: only SES accepts a
  built message as-is, and Postmark and Resend take decomposed fields, so
  routing through those would mean re-signing by them.

- **Sessions are rows**, so one browser can be signed out without touching the
  others. A session used to be a cookie carrying a username, an expiry and a
  signature over both, with the server keeping nothing — which is why the only
  way to end one was to rotate the signing key, ending every session on the
  server, for everybody. Settings → Sessions now lists the browsers signed in
  to an account, marks the one you are reading it from, says where each was
  last used, and ends them one at a time. "Sign out everywhere" still exists
  and now means this account rather than all of them.
- **API tokens are rows too**, and record when each was last used and from
  where. They used to live inside the account in the configuration, which meant
  writing "last used" would have rewritten the whole configuration and had
  every instance reload it. When a token was last used is data, not a setting.
- A revoked session or token is kept for thirty days, marked revoked, so the
  list can say what happened to it rather than the row silently disappearing.
  An hourly sweep removes those and anything long expired — scheduled, not
  merely written.

- **Settings → Server**, and the API behind it. A dashboard that warns a
  restart is needed should be able to do it, rather than sending the operator
  to a terminal. `GetServerStatus` says which instance you are talking to,
  which build it is running, how long it has been up, and which settings have
  changed that it is not using yet; `RestartServer` ends the process so the
  supervisor starts a new one. The page names what it thinks will start it
  again — a container, systemd, or nothing it can see — and warns in the last
  case, because that is the operator for whom restarting means staying down.
- **Settings → Integrations**, which is the first user interface for settings
  the API has been able to change since it was written: the object store, the
  Route53 solver, and the two scanners. A secret is shown as set or not set
  rather than read back, an empty box leaves it alone, and clearing one is a
  separate deliberate act.
- The object store endpoint and path style are settings the API can change.
  They existed in the configuration but not in the API, so a self-hosted store
  could only be pointed somewhere by exporting the configuration, editing it
  and loading it back — which is what had to be done to this deployment.

- A page for DMARC aggregate reports, which were being received and parsed and
  then shown nowhere. It answers the question the reports exist to answer — who
  is sending mail as one of your domains, and did the receiver believe them —
  with the sender's address and reverse DNS, whether anything aligned, and what
  the receiver did about it. Listing them no longer requires naming a domain
  first, because "is anyone forging me" is not a question about one domain.
- `server.mailServers` names the hosts mail arrives at, so a domain can be
  asked for a pair of MX records rather than one. A deployment reached at
  `mx1` and `mx2` no longer has every domain reported as having the wrong MX,
  and a server can be moved later without twenty five zones changing. Leave it
  unset and nothing changes: the MX record names the server, as before.
- Authentication is rate limited, per address, on the submission port and on
  the dashboard. Verifying an SMTP credential is an HMAC and a comparison, so
  without a limit an address could guess as fast as the network allowed; the
  dashboard's bcrypt hash made each guess expensive for the server too. Tune
  with `smtp.authRateLimit` and `smtp.authRateBurst`, or set either to zero to
  turn it off.
- The deployment test speaks GraphQL, and runs to the end. It had been calling
  three REST endpoints that no longer exist, so it failed at onboarding and
  never reached the checks that matter: receiving mail on port 25, the ARC
  seal on a forwarded message, DKIM signing, submission with a credential, and
  that all of it survives a restart.
- `docs/getting-started.md` and `docs/configuration.md`, which the README has
  been promising. The first walks from nothing to mail arriving, including the
  outbound port 25 blocking that decides whether a host is usable at all; the
  second documents every configuration field, with a check that fails when a
  new one arrives undocumented.
- The secret guard checks every hostname in the tree against a list of what is
  allowed, rather than a list of what is forbidden. A deny list only catches
  the names somebody thought to add; this catches an operator's own domain
  wherever it lands, including in a comment or a test fixture.
- One configuration file, `teanode.yaml`, holding everything an operator sets,
  including each domain's DKIM signing key. It is re-read on `SIGHUP` and
  rewritten by the dashboard.
- A signing key is generated for every domain the moment it is created, and
  the dashboard shows the exact DNS records to publish, key value included.
  Nobody has to know DKIM exists before their mail is trusted.
- The server works out its own external IPv4 and IPv6 addresses and puts them
  in the DNS guidance, so the record for the mail host says the address to use
  rather than leaving the operator to find it.
- Certificates without a cloud account: ACME `http-01` by default, with
  `tls-alpn-01` for hosts where port 80 is blocked, and `dns-01` via Route53
  kept for wildcards.
- A dashboard compiled into the binary, which renders a message as a message —
  authentication verdicts, delivery attempts, and the body with scripts
  stripped and remote images blocked until asked for.
- Dashboard authentication: users in the configuration file with bcrypt
  hashes, and a signed session cookie.
- Local storage for received messages, so a delivery can be retried after a
  restart and the dashboard has something to show. S3 becomes an optional
  mirror rather than the only copy.
- `teanode config init`, `config validate`, `config show`, `config import`,
  `dkim`, `tls self-signed`, `password`, `credential`, `user` and `token`
  commands.
- `teanode api`, which reaches every operation the server offers by reading
  the schema from it: `api list`, `api describe`, `api call` with `name=value`
  arguments, and `api graphql` for a query written by hand. Output is JSON, and
  the typed commands take `--json` too.
- API tokens, so the command line tool can administer a server over the
  network with `--url`. A token belongs to an account and acts as it; removing
  the account revokes it. On the server itself no token is needed.
- AWS credentials can live in `teanode.yaml` for Route53 and S3, instead of a
  shared credentials file or the ambient chain. They are never returned by the
  API, and `config show` redacts them along with every other secret.
- `make dev`, which brings up a development server that cannot send mail.
- `make test-deployment`, which brings the whole stack up in Docker — the
  production image, PostgreSQL, a DNS server answering for .test, and a mail
  sink — and proves it end to end: migrations, onboarding, tokens, the command
  line client over the network, a message received on port 25 and forwarded
  with a verifiable ARC seal, submission over STARTTLS delivered by MX lookup,
  survival of a restart, and that no secret is printed. It found three of the
  bugs listed below.
- The dashboard speaks English, Simplified Chinese and Japanese, picked from
  the browser's language and changeable from a control beside the appearance
  one. No i18n library: lookup and substitution is forty lines, and the
  catalogues are typed against the English one so a missing key does not
  compile. `make check-catalogs` catches what types cannot — a translation that
  dropped a placeholder, or one that was never translated.
- An appearance setting in the dashboard: auto, light or dark. Auto follows
  the operating system and is the default; the other two are for when it is
  wrong. The choice is applied before the page paints, so it does not flash
  the other theme first.
- A domain overview at `/domains/<id>`: whether its DNS is published and when
  it was last checked, how much mail it has received and when the last arrived,
  and what is configured — with a link to the mail list already narrowed to
  that domain.
- An aggregation pipeline on the list queries, in the shape used elsewhere in
  the fleet: a list of stages, each exactly one of a match, a sort, or a
  distinct, applied in order. Filtering and ordering happen in the database
  rather than over whatever the browser fetched, and `CountMailsBy` answers
  what a filter menu needs — the values a column takes and how many rows
  carry each.
- Column sorting in the dashboard's tables: ascending, descending, and back
  to the table's own order.
- A domain publishes its mail records under its own names. The MX names
  `mx1.<that domain>` rather than a name in whichever domain the server is
  called after, the bounce and report subdomain gets an MX of its own instead
  of an alias to the server, and the address records for those names are on
  that domain's page because they are that domain's to create. Pointing every
  domain at one name worked, and published in each of them the name of a
  different one: look up the MX of any and you learn the set. Nothing has to
  change on an existing installation — an MX naming the server's own hosts, or
  a bounce name still aliased to it, is still correct, and is still checked as
  correct.
- Every domain has a signing key of its own, and publishes it at its own name.
  A domain that arrives without one — from a configuration file written by
  hand, from an import, from an older database — is given one on the next
  start, and told in the log where to publish it. A key already there is never
  replaced: it matches a record already published.
- One table component behind the mail list and the queue: per-column filters,
  pagination, and timestamps as "12 hours ago" with the exact time, zone
  named, on hover.
- `smtp.requireReverseDns`, on by default. Turn it off where this server does
  not see the real client address — behind a load balancer, or on a private
  network — because there the check refuses everything.

### Changed

- **A row that goes somewhere is the target, not the one link inside it.** The
  mail list, the queue, the reports and the domains are lists of things you
  open; the link in each row stays for the keyboard and the middle button.

- **A message says what happened to it.** The page opened with the subject and
  the sender, which are the two things the reader already knew — they clicked
  the row. It opens with the verdict and a sentence saying what became of it,
  and carries what was fetched and thrown away: the TLS version, the HELO, the
  Message-ID, where the sender is, the DSN the far end returned.

  Authentication was a row of tags reading "SPF pass DKIM pass", enough to
  know nothing went wrong and never enough to work out why something did. Each
  check is a row now: the mechanism, the verdict, and what was examined — the
  key that signed, the address SPF authorised, the policy DMARC found, the
  rules the spam filter matched. The markup behind the rendered view has a tab
  of its own, highlighted.

- **Accounts are keyed by an identifier rather than by their username**, and
  the table is `user` rather than `operator`. A key that changes is not a key:
  sessions and API tokens named an account by a string a rename would have
  invalidated. They point at the identifier now and cascade from it. `setting`
  became `configuration`, which is what it holds.

- **The rail carries the server's settings beside Domains**, and the account's
  own behind your name at its foot — where opening Settings swaps the rail for
  them. There is no bar across the top any more: what was on it was a control
  belonging to the rail and two menus belonging to the reader, and what was
  left was a 56-pixel band with a rule under it.

- **The filter fields open from a control** rather than sitting under every
  header permanently, where they were the widest thing on the page and used on
  a fraction of visits.

- **Nothing draws "loading…" for the first quarter of a second.** A query on a
  local network answers in less than that, and the word was a flicker on every
  navigation.

- **"Replace the key" is gone from a domain that signs with the primary's
  key.** Such a domain publishes a CNAME rather than a key of its own, and
  replacing it would have given it a key nobody had published. It offers to
  split off instead, and says what that changes.

- **The dashboard follows a quieter design language**: colour only where it
  means something. The rail is a warm grey against a
  white page, the row you are on is a raised pill rather than a highlight, and
  what you press is near-black — inverting to near-white on dark rather than
  dimming. Group labels are sentence case rather than small caps, table heads
  are a quiet label rather than a shout, the zebra stripe is gone in favour of
  the rules that were already there, and prose stops at eighty characters.

  The rail carries its groups under their own labels, keeps the way into
  settings and who you are at its foot, and swaps itself for the settings
  navigation once you are in there — so the six settings surfaces are
  reachable from each other rather than only from a hub. Each page now names
  itself in a heading at the top of its own content, and the breadcrumb above
  shows only how you got there.

  The accent is no longer the mark's green. It was on every button, link and
  active row, which meant none of them meant anything; the green now belongs to
  the logo alone, and green, red and amber are kept for state — a record
  published, a delivery failed, a certificate nobody can see.

- Configuration lives in PostgreSQL rather than in `teanode.yaml`, so that
  more than one instance can run against it. The file could not be shared: a
  server held the whole of it in memory and rewrote it from memory on every
  change, so a second instance would not see a domain added on the first and
  would overwrite its changes at the next save. Two things stay outside, in
  the environment, because they cannot be kept in the database they describe
  or must differ per process: `TEANODE_DATABASE_URL`, and `TEANODE_INSTANCE_ID`
  — which the usage counters are keyed by, and which two instances sharing
  would lose each other's counts. `teanode config env` writes a starting
  point, `teanode config init` sets an empty database up from it, and
  `teanode config import` loads an existing `teanode.yaml` in, carrying
  identifiers, signing keys, the server secret and the session key across
  unchanged. `teanode config export` writes one back out.
- Session cookies and API tokens are identifier, secret and a signature over
  both, signed with different keys so a cookie cannot be presented as a token.
  Only a hash of the secret is stored, so a copy of the database is not a set
  of working logins. Every existing token stops working and has to be reissued;
  the format changed and there is no way to write a row that would accept an
  old one.
- Last-used is recorded at most once a minute per credential, guarded in the
  `WHERE` clause rather than in memory, so a dashboard left open on its refresh
  timer costs one row update a minute rather than one per poll — and two
  instances cannot move the column backwards.
- Stored settings are YAML, in a text column, not JSON in a `jsonb` one. A
  server secret is 32 bytes from `crypto/rand`, so most are not valid UTF-8
  and roughly one in eight contains a zero byte. `jsonb` refuses a NUL
  outright, and `encoding/json` quietly replaces an invalid byte with the
  replacement character — which would have invalidated every SMTP password on
  the server without saying anything. YAML writes such a string as `!!binary`,
  which is also what an exported file holds, so the two forms cannot drift.
- Concurrent configuration changes are resolved rather than lost. A write
  carries the version it was based on, one row is taken `FOR UPDATE` for the
  length of it, and a change that lost the race is re-applied to the newer
  configuration rather than merged into it. Instances notice a change made
  elsewhere within five seconds.
- Raw messages go to an S3-compatible object store as well as to local disk,
  which is what lets any instance show or retry a message another one
  received. MinIO is what the compose files use; the endpoint and path-style
  settings that a self-hosted store needs are now configurable.
- The server says when a setting that is only read at startup changes
  underneath it — the listeners, TLS, storage, the data directory, the
  optional integrations. That configuration is shared now, so it can change
  from the dashboard or from another instance while this process is running,
  and a setting that appears to save and does nothing is worth an hour of
  somebody's afternoon.
- Retention sweeps the object store as well as the local spool. Sweeping only
  local files would never expire a message another instance handled, so the
  bucket grew without bound — and the bucket is the copy that matters once
  there is more than one instance.
- Generated secrets are decided inside the mutation that stores them. A server
  secret generated beforehand would be written over the one another instance
  had just stored, leaving the two deriving SMTP passwords from different
  keys.
- `server.dataDirectory` has to be an absolute path. It used to resolve
  against the directory holding the configuration file; with no file it would
  land wherever each process was started from.
- The repository root is the Go module; the binary carries the dashboard.
- The HTTP API is versioned and mounted at `/api/v1`, implemented under
  `internal/api/v1api` as `apigraph` (GraphQL) and `apisend` (the template
  send endpoint).
- Logging in, logging out, claiming a new server and changing a password are
  GraphQL operations, not REST endpoints beside it. A browser's credential is
  a cookie, so those resolvers get the response writer; that is a reason to
  pass one argument, not to run a second protocol. Authorisation was always in
  the resolvers rather than the routing, and a test now reads the source and
  fails if a new one forgets to check.
- The command line tool changes configuration through the running server
  rather than writing to the database directly, so that a change made from the
  shell is validated the same way and has the same side effects as the same
  change made in the dashboard. Run on the server it reads the same
  environment the server does; `teanode user --offline` writes straight to the
  database, which is safe now, and remains for recovering from a lockout.
- API tokens and the accounts that administer the server moved out of the
  `dashboard` block: `users` is top level, each with its own `tokens`, and
  what is left is `session`.
- The database keeps only what grows without bound: mail, deliveries, DMARC
  reports, usage counters and templates. Migrations restart at `0000`.
- Domain DNS checks advise rather than gate. Mail for a configured domain is
  accepted, and the dashboard says which records are still missing.

### Removed

- The Redis-backed relay between server instances, and with it the Redis
  dependency.
- Multi-tenant user accounts, per-user domain ownership and magic-link login.
- Test fixtures made of real captured mail, replaced by messages generated at
  test time.

### Fixed

- The port shown beside a new credential is the one a mail client can reach,
  not the one the process binds. Those are the same thing until something
  forwards one to the other — a container publishing 10587, a firewall taking
  587 — and then the dashboard was handing somebody a number nothing answers
  on. `smtp.submission` sets what to advertise, host and port, and both are
  editable on the Setup page; leaving them empty keeps the old behaviour of
  following the server.
- The Setup page no longer describes settings as living in `teanode.yaml` and
  being reloaded with a HUP signal. Neither has been true since configuration
  moved into the database.

- Links in a message can be clicked. The sanitiser had been putting
  `target="_blank"` on every link it kept since it was written, but the frame
  showing the message was sandboxed with `allow-same-origin` alone — and a
  browser silently drops a `_blank` click without `allow-popups`. Nothing
  reported an error; the link simply did nothing. The frame now also allows
  the opened tab to escape the sandbox, because a link that "works" and lands
  on a page with no scripts and no origin is worse than one that does not
  open. Scripts, top navigation and forms are still refused, so a message
  cannot run code, cannot navigate the dashboard away from under the reader,
  and cannot submit anything.

- `Pagination.Options` assigned the offset to the limit, so asking for an
  offset silently changed how many rows came back and never skipped any.
- `graphapi` panicked on any named type over a builtin — `type Operation
  string`, which is what every enum in an input is. It coerced by kind and
  returned a plain `string`, which `reflect.Set` then refused. It also
  ignored `graphapi:"nullable"` on method arguments, so an optional argument
  was published as required.

- Mail from anything that composes HTML for a living rendered as a column of
  fragments. The sanitiser dropped every `<style>` block and `style`
  attribute while keeping the class names that referred to them, so a message
  arrived with its whole skeleton of nested tables and not one rule that made
  it a layout. CSS is kept and sanitised now; what stops it fetching or
  scripting is the frame's own policy, not the sanitiser deleting it.

- API replies carried no `Cache-Control`, and a 200 without one is
  heuristically cacheable. After the accounts on a development server were
  cleared, a phone went on showing a login form for a server that had none,
  because it never asked again. The same staleness would let a browser be told
  it is still logged in after logging out.
- A subscription that emitted a message with the identifier `test` every
  second, to anybody who asked, is gone. It was a placeholder nothing called,
  and it was the one operation in the schema that checked no permissions.
- The dashboard followed the operating system's light or dark setting with no
  way to override it, and the viewport did not say whether zooming was
  allowed. On a phone it now respects the notch, keeps pinch-to-zoom, and
  sizes form fields at 16px so that iOS stops zooming in when a field takes
  focus and never zooming back out.

- `arc.Validate` started one verification goroutine more than it collected
  results from, and the discarded one was usually the check covering the
  message body — so a message altered after being sealed validated as `pass`.
  It also leaked a goroutine per call.
- `teanode config show` printed the server secret, the session key and every
  domain's signing key, despite hiding passwords. Redaction is now driven by a
  struct tag with a test that fails when a new secret is not tagged.
- The ARC seal on a forwarded message named the mail host, for example
  `d=mail.example.com`, while the signing key is published at the domain. Every
  receiver that checked a forwarded message looked up a name with no key under
  it, so no seal could be verified — the entire purpose of sealing. Nothing
  here could notice, because only the receiver ever verifies one.
- `deploy/docker-compose.yml` mounted `teanode.yaml` as a file. The server
  saves by writing a temporary file and renaming it over the old one, and a
  rename over a bind-mounted file fails with `EBUSY`, so no change made in the
  dashboard could be saved. The configuration directory is mounted instead.
- An alias relaying to a mail server refused any host name without a dot, so an
  internal smarthost named `smtp` or reached by address could not be
  configured.
- `teanode dkim show --json` ignored `--json` when the server was not running,
  and failed outright before the first start, when the server secret a local
  token is signed with does not exist yet.
- A DKIM record with an empty `p=` was reported as published. An empty value
  means the key is revoked, so an operator was told their signing was fine
  while every signature failed.
- With no log directory configured, every received message was written into
  the process working directory.
- `GetCertificate` served an empty placeholder certificate before the first
  issuance, failing the TLS handshake obscurely.

### Removed

- The `X-Forwarding-Service` header. It announced this software and its version
  to every recipient of every message, which told them nothing they could use
  and told anybody else what to look up. It was also on submitted mail, which
  nobody forwarded. The `Received` header still names the host that handled the
  message, which is what tracing a delivery actually needs.

### Fixed

- The `Received` header on a message submitted over the API says where it came
  from. There is no greeting in an HTTP request, so the from clause was empty —
  `from  (unknown [address])` — which is not the form RFC 5321 describes. It
  uses the address literal, which is.
- A submitted message names one host, not two. The `Received` header said the
  name the sender reached while the `Authentication-Results` beside it still
  said the server's own, so one message carried two different answers to where
  it had been.
- `Feedback-ID` is set on mail this server was asked to send, and on nothing
  else. It used to go on every delivery carrying the delivery's identifier,
  which was wrong twice: the header exists so a receiver can group a sender's
  complaints, and its last field has to mean the same sender every time, so a
  fresh value per message grouped nothing. On forwarded mail — most of what
  this sends — a message that already carried the original sender's went out
  with two, ours first, so the receiver attributed complaints to a meaningless
  value instead of to the one the sender set deliberately. Submitted mail now
  carries the sending domain, which is the identity a receiver's tooling is
  registered against; a forwarded message carries nothing, because it belongs
  to whoever wrote it.
- DMARC is evaluated against the organizational domain when the sender's own
  subdomain publishes no record, as RFC 7489 section 6.6.3 requires. Only the
  exact name was asked, so a message from a sender like
  `rs.email.example.com` — which publishes nothing there and is covered by
  `example.com`'s `p=reject` — was recorded as having no DMARC policy at all. Bulk senders almost all send from
  a subdomain, so this was most of the mail that arrives: a large share of it
  was being judged as unprotected, and the authentication panel said so on
  messages every other receiver reports as `dmarc=pass`. Where the policy comes
  from the domain above, the subdomain policy is the one applied, and the
  domain it was found at is recorded beside it.
- SPF is one of the records a domain is asked for, and is checked. It never
  appeared at all, so a domain sending mail no receiver would accept as
  authorised looked exactly like one that was set up correctly. It is asked for
  at the bounce subdomain rather than at the domain itself, because that is the
  envelope sender on everything this server sends and it is the envelope a
  receiver evaluates — a perfect record at the domain does nothing for it. What
  is checked is that a record exists and permits something: whether it permits
  this server is a question for a resolver, and with a proxy or a relay the
  address mail leaves from is not one this server can see.
- The DMARC record the dashboard asks for names a report address the server
  will actually accept. It asked for `rua@mail.<domain>`, and a report sent
  there is refused: the server takes a report only at a signed address, which
  is what it checks the recipient against. Anybody who published exactly what
  the panel showed them received no aggregate reports and was told nothing.

### Security

- A domain's signing key is encrypted in the database rather than stored as
  PEM in a column. The key is derived from the server secret, so a copy of the
  `domain` table on its own — a partial dump, a support query, a replica of
  some tables and not others — no longer carries usable private keys. It is
  not protection from a full compromise: the server secret is in the same
  database. Keys written by an earlier release are encrypted on the first
  start after the upgrade, so there is nothing to migrate and no window where
  the column is documented as encrypted while sitting in plaintext.
  `teanode config export` still writes a plaintext file.
