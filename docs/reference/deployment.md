# Deploying with Docker Compose

This is how a TeaNode server is meant to run: one compose file, the published
image, and PostgreSQL beside it. `docs/getting-started.md` walks through the
same server installed as a binary, and explains DNS and the port 25 problem in
more detail — read its "Before you begin" first, because a host that cannot
send on port 25 cannot deliver mail however it is deployed.

## What you get

    postgres        the configuration, the keys, and every message handled
    postgres-certs  a certificate for it, generated once, so that connection
                    is encrypted and verified rather than in the clear
    teanode         the server, with the dashboard inside it
    clamav          virus scanning, optional
    spamassassin    spam scoring, optional

and, behind the `cluster` profile, MinIO and Redis, which only matter when you
run more than one instance.

## 1. Take the compose file

    mkdir -p /opt/teanode && cd /opt/teanode
    curl -L -O https://raw.githubusercontent.com/ziyan/teanode/main/deploy/docker-compose.yml

Nothing else needs to be installed — not even the binaries, since the image
carries both of them.

## 2. Describe the server

    docker run --rm ghcr.io/ziyan/teanode:latest \
      config env --output - --hostname mail.example.com --domain example.com > .env
    chmod 600 .env

`--hostname` is what the server announces over SMTP and what your MX record
will point at, so it has to be a name you can add a DNS record for.
`--domain` becomes the first domain it serves.

Open `.env` and set `TEANODE_TLS_ACME_EMAIL` to an address the certificate
authority can warn about expiry. Check that
`TEANODE_SERVER_DATA_DIRECTORY` reads `/var/lib/teanode`, which is where the
compose file mounts the volume. It has to be inside that volume: a path that is
not is either unwritable — the image runs as an unprivileged uid and cannot
create a directory at the root — or, worse, writable inside a container that
the next upgrade throws away, taking the keys and the spool with it.

The file holds a database password, which is why it is `chmod 600`.

`TEANODE_DATABASE_URL` asks for `sslmode=verify-full`. The official PostgreSQL
image ships with TLS off and no certificate, so the compose file generates a
self-signed one and starts PostgreSQL with it; `verify-full` means the
connection is encrypted *and* checked against that certificate, so nothing else
on the Docker network can answer as the database. Pointing at a PostgreSQL of
your own means pointing `sslrootcert` at that server's authority instead, or
dropping to `sslmode=require` to encrypt without checking who answered.

## 3. Start it

    docker compose up -d

The first start migrates the schema, writes the configuration the environment
describes, generates a server secret and a signing key for the domain, and
obtains a certificate over HTTP-01 — which needs port 80 reachable from the
internet. Watch it do all that:

    docker compose logs -f teanode

You are looking for `teanode is running`.

## 4. Claim the dashboard

Open `https://mail.example.com/`. **The first visitor creates the only
account**, so do this immediately. If you would rather not race anybody:

    docker compose exec teanode /usr/local/bin/teanode-server user add you

`exec` runs a command beside the server rather than through the image's
entrypoint, and the image has no shell, so the binary is named in full. Every
`teanode-server` subcommand is available this way.

It prompts for a password. In a script, where there is no terminal, pass
`--stdin` and pipe one in.

## 5. Publish DNS

The dashboard lists exactly which records are missing, per domain, and keeps
checking. `docs/getting-started.md` explains what each one is for.

## 6. Tidy the .env

Once the server has started, most of that file is dead weight. Everything
marked "first run only" was copied into the database on the first start and is
ignored from then on — the server logs a warning naming any that disagree with
what is stored, because a file that says one thing while the server does
another is how an afternoon gets lost.

Delete them. What has to stay:

| variable | why |
| --- | --- |
| `TEANODE_DATABASE_URL` | how it finds everything else. Read on every start |
| `TEANODE_INSTANCE_ID` | only if you set one; it has to differ between instances |
| `TEANODE_SERVER_DATA_DIRECTORY` | read from the environment, not from the database, so that a staged upgrade can be found before the database is open |
| `TEANODE_S3_*` | **only if you run the `cluster` profile** — compose passes these to MinIO as its root credentials, so deleting them changes the credentials on the next recreate |

That last row is the trap: they read like settings that moved into the
database, and they are, but the compose file also interpolates them into a
different service.

Settings change in the dashboard from here on, or with `config import`.

## Upgrading

From the dashboard, under Settings, when a release is available — it stages
the new binary in the volume and restarts into it. Or pull the image:

    docker compose pull teanode
    docker compose up -d teanode

Both are safe to run against a server handling mail; in-flight deliveries
finish. Pin the image tag rather than following `latest` if you would rather
an upgrade were a thing you did on a day you chose.

## What to back up

**PostgreSQL, and nothing else matters as much.** It holds the configuration,
the DKIM signing keys, the server secret from which every SMTP password is
derived, and the mail. Losing it means republishing DNS records and reissuing
credentials.

    docker compose exec postgres pg_dump -U teanode teanode | gzip > teanode-$(date +%F).sql.gz

A configuration-only backup, readable and reviewable, without the mail:

    docker compose exec teanode /usr/local/bin/teanode-server \
      config export --file /var/lib/teanode/backup.yaml

That file carries signing keys and the server secret in the clear — it has to,
or restoring it would invalidate every SMTP password and every published DKIM
record. Treat it as a private key.

`./data/teanode` holds the spool and the certificates. Certificates are
reissued automatically, so it is worth backing up but not urgent.

## More than one instance

    docker compose --profile cluster up -d

They share the configuration through PostgreSQL, the stored messages through
MinIO, and half-finished passkey sign-ins through Redis. Uncomment the
`TEANODE_S3_*` block in `.env`, set `passkey.redis.address`, and give each
instance its own `TEANODE_INSTANCE_ID`.

The object store is the part that matters: without it each instance can only
read the messages it handled itself.

## When it does not start

Read the log first — the server is specific about what is wrong.

    docker compose logs teanode | tail -40

**`cannot create ...: permission denied`.** The image runs as uid 65532, and
the data directory has to be writable by it. The `teanode-data` service in the
compose file arranges this before the server starts; if you changed the volume,
do the same thing to the new one:

    chown -R 65532:65532 ./data/teanode

**`ignoring TEANODE_...: the database wins`.** Exactly what it says, and
usually harmless — a leftover "first run only" line. Delete it, or change the
setting in the dashboard.

**A bad setting stored on the first start.** The first start writes the
configuration whether or not the server then manages to run, and after that the
environment is ignored — so editing `.env` will not fix it, and a server that
cannot start has no dashboard to fix it in. Go through the file instead:

    docker compose run --rm --no-deps teanode config export --file /var/lib/teanode/fix.yaml
    # edit ./data/teanode/fix.yaml, which is that same file from outside
    docker compose run --rm --no-deps teanode config import --file /var/lib/teanode/fix.yaml --force

`run` rather than `exec` here, because a server restarting in a loop has no
container to exec into. `--no-deps` so it does not start the rest of the stack
to do it.

`--force` is required once the database holds domains or operators; without it
the import refuses rather than replacing them.
