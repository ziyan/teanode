# Getting started

This walks through standing up a mail server for one domain, from nothing to
mail arriving and being forwarded. It takes about twenty minutes, most of it
waiting for DNS.

## Before you begin

You need three things:

- **A domain**, and access to edit its DNS records.
- **A host with a stable address**, reachable from the internet on ports 25,
  80, 443 and 587.
- **PostgreSQL**, which stores the mail the server has handled. The
  configuration and the keys are in a file; the database holds the history.

### The port 25 problem

Read this before you rent a host. Most consumer ISPs, and a good number of
cloud providers, block **outbound** port 25 by default — DigitalOcean, Google
Cloud, Azure and Oracle Cloud all do, and Amazon EC2 does until you ask.

Inbound port 25 is usually fine. Outbound is what gets blocked, and without it
this server can receive mail and cannot deliver it — so forwarding fails while
everything looks healthy.

Check before you commit:

    nc -vz gmail-smtp-in.l.google.com 25

If that hangs or is refused, you have three options: ask the provider to lift
the block, pick one that does not have it, or set `smtp.socks5Proxy` to route
outbound mail through a host that can reach port 25.

## 1. Install

    curl -L -o /usr/local/bin/teanode-server \
      https://github.com/ziyan/teanode/releases/latest/download/teanode-server-linux-amd64
    curl -L -o /usr/local/bin/teanode \
      https://github.com/ziyan/teanode/releases/latest/download/teanode-linux-amd64
    chmod +x /usr/local/bin/teanode-server /usr/local/bin/teanode

`teanode-server` is the server, with the dashboard inside it, so there is
nothing else to install or serve. `teanode` is the client that administers it,
and belongs on the server and on your own machine; there is a macOS build of
it. There is a container image too, with both in it, and `deploy/` has a
compose file.

## 2. Describe the server

Configuration lives in PostgreSQL. The environment says how to reach it, and
— the first time the server starts against an empty one — what kind of server
to create.

    mkdir -p /opt/teanode && cd /opt/teanode
    teanode-server config env --output .env \
      --hostname mail.example.com \
      --domain example.com

`--hostname` is the name of this server. It is what the server announces in
SMTP and what your MX record will point at, so it must be a name you can add a
DNS record for.

`--domain` is the domain whose mail you want to receive. It becomes the first
domain this server serves.

Open `.env` and set `TEANODE_DATABASE_URL` to your PostgreSQL, and
`TEANODE_TLS_ACME_EMAIL` to an address the certificate authority can warn
about expiry. Then set the database up:

    set -a; . ./.env; set +a
    teanode-server config init

That runs the migrations and stores the configuration. It generates a DKIM
signing key for the domain and a server secret, both of which live in the
database from now on — so the database is the thing to back up.

After this the `.env` is only read for where the database is: settings change
in the dashboard, and the server logs any first-run variable that disagrees
with what is stored.

### Moving an existing server in

If this deployment already ran on a `teanode.yaml`, load it instead of
creating something new. Identifiers, signing keys, the server secret and the
session key all come across unchanged, so stored mail still resolves, SMTP
passwords still work, and nobody is signed out:

    teanode-server config import --file /opt/teanode/teanode.yaml

## 3. Publish DNS

Ask the server what to publish:

    teanode dkim show example.com

You need four records. The first is the only one that decides whether mail
arrives at all; the rest decide whether it is trusted.

| Type | Name | Value |
| --- | --- | --- |
| A | `mail.example.com` | your server's address |
| MX | `example.com` | `10 mail.example.com` |
| TXT | `example.com` | `v=spf1 mx -all` |
| TXT | `teanode1._domainkey.example.com` | what `dkim show` printed |

A word on each:

- **A** — the name in your MX has to resolve, and to *this* host. Set the
  reverse DNS for the address to match if your provider lets you; receiving
  servers check.
- **MX** — without this, no mail arrives, whatever else is right. One record
  naming your server is enough. If you would rather mail arrived at a pair of
  names — `mx1` and `mx2`, so you can move the server later without every
  domain changing its DNS — list them under `server.mailServers` and the
  dashboard will ask each domain for both.
- **SPF** — `v=spf1 mx -all` says "the hosts in my MX record send my mail, and
  nobody else does". If something else sends as this domain — a newsletter
  service, a CRM — add it here or that mail starts failing.
- **DKIM** — signs your outbound mail so a receiver can tell it was not
  altered and really came from you.

Once mail is flowing, add a fifth:

    TXT   _dmarc.example.com   v=DMARC1; p=none; rua=mailto:rua@mail.example.com

`p=none` asks nobody to reject anything; it only asks for reports, which the
server parses and shows you. Move to `p=quarantine` and then `p=reject` once
the reports show your own mail passing.

## 4. Check and start

    teanode-server config validate
    teanode-server run

On the first start the server obtains a certificate over HTTP-01, which needs
port 80 reachable from the internet. If it is not, that is the first thing the
log will tell you.

### Upgrading a container that used to run as root

The image runs as uid 65532 now, not root, with only `CAP_NET_BIND_SERVICE` to
bind the low ports. If your data directory was created by an earlier
version it is owned by root, and the server will fail to read it. Once, before
starting the new image:

    chown -R 65532:65532 /opt/teanode/data/teanode

## 5. Claim the dashboard

Open `https://mail.example.com/`. The first visitor creates the only account,
so do this immediately — before the server has been reachable long enough for
anybody else to find it. If you would rather not race:

    teanode user add you

The dashboard lists exactly which DNS records are still missing or wrong, per
domain, so you can see what is left rather than guessing. It checks
periodically; there is no need to reload it.

### If you are locked out

There is no password reset by mail; the server's own host is the way back
in. On it, with the server's environment in the shell — which a container
already has — the client reaches the server as the console and can add an
account or set a password:

    teanode user create you
    teanode user password you

    docker compose exec teanode teanode user create you      # in a container

When the server is not running, or is running but nobody can sign in and the
console cannot reach it either, `teanode-server user` edits the stored
configuration directly and needs only the database:

    teanode-server user list
    teanode-server user add you
    teanode-server user password you

`teanode-server user reset` removes every account, after which the next
visitor to the dashboard creates one, as on the first day. Anyone who can
reach the dashboard can claim it until somebody does, so do not leave it in
that state.

## 6. Send yourself something

Send a message to `hello@example.com` from an account elsewhere. Within a few
seconds it should appear in the dashboard's mail list, showing SPF, DKIM and
DMARC verdicts for the sender, and a delivery attempt to the address you set
as `--forward-to`.

If it does not arrive, the queue page says why. The usual causes, in order of
how often they are the answer:

1. The MX record has not propagated yet. Check with `dig MX example.com`.
2. Port 25 is not reachable inbound. Check from elsewhere with
   `nc -vz mail.example.com 25`.
3. The forwarding destination rejected it, in which case the queue shows the
   remote server's own words.

## Where to go next

- **More addresses.** Aliases match with a regular expression, so
  `^(sales|support)$` is one alias. An empty pattern is a catch-all that
  receives whatever nothing else matched.
- **Sending from your own devices.** Add a credential per device on the
  domain's settings page; each can be restricted to one sender address. They
  authenticate on port 587.
- **More domains.** Each gets its own signing key by default. If you would
  rather they all shared one, point `<selector>._domainkey` at the primary
  domain with a CNAME and the dashboard will show you the record to publish.
- **[configuration.md](configuration.md)** documents every field.
- **[reference/command-line.md](reference/command-line.md)** covers the CLI,
  which reaches the whole API and is the better tool for anything repetitive.
