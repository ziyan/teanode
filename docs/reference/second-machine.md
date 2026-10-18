# A second machine: a standby, a mirror and an archive

A second machine with its own disks can do three things for a TeaNode
server, each independent of the others:

- **a hot standby of PostgreSQL**, which replays every change as it is made,
  so losing the server's disk loses seconds, not a day;
- **a mirror of the MinIO bucket**, the mail, attachments, recordings and
  photos that the database only names;
- **an archive tier for MinIO**, where old photo and video originals move
  once the server's disk should be kept for what is new.

None of them needs a change to TeaNode or its configuration. They are
PostgreSQL's streaming replication and MinIO's mirroring and lifecycle
tiering, set up beside the compose file in `deploy/`. The examples call the
server `10.0.0.1` and the second machine `10.0.0.2`, on a private network
between them; substitute your own.

The MinIO parts apply when the files are in MinIO, which is the `cluster`
profile of the compose file. With the default, files under
`storage.directory` on the data volume, mirror that directory instead
(`rsync -a --delete` on a timer), and there is no tier.

A standby and a mirror copy deletions as faithfully as everything else. They
protect against a lost disk or machine, not against a mistake: keep taking
dumps (see [What to back up](deployment.md#what-to-back-up)), and take one
before an upgrade with a migration, because the standby applies the migration
too.

## The PostgreSQL standby

The standby needs to reach the server's PostgreSQL, which the compose file
keeps on its own network. Publish it on the server's loopback, and no
further:

      postgres:
        ports:
          - "127.0.0.1:35432:5432"

Rather than publish it on the network, let the server open an SSH tunnel to
the second machine, so the database is reachable there on the second
machine's own loopback and nowhere else.

**On the server**, make a role that may only replicate, a slot that keeps
the WAL the standby has not yet received, and a cap on how much of it the
slot may hold, so that a second machine switched off for a week cannot fill
the server's disk:

    docker compose exec postgres psql -U teanode teanode -c "CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD '<a long random password>'"
    docker compose exec postgres psql -U teanode teanode -c "SELECT pg_create_physical_replication_slot('standby')"
    docker compose exec postgres psql -U teanode teanode -c "ALTER SYSTEM SET max_slot_wal_keep_size = '40GB'"

Let the replicator in from the Docker network's gateway, which is where a
connection through the published port arrives from (`docker network inspect
<project>_default` shows it), above the catch-all line of `pg_hba.conf` in
the data directory, then reload:

    host replication replicator 172.18.0.1/32 scram-sha-256

    docker compose exec postgres psql -U teanode teanode -c "SELECT pg_reload_conf()"

Keep the tunnel up with a systemd unit on the server, using a key the second
machine accepts:

    [Unit]
    Description=Tunnel for the TeaNode PostgreSQL standby
    After=network-online.target docker.service
    Wants=network-online.target

    [Service]
    ExecStart=/usr/bin/ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -R 127.0.0.1:35432:127.0.0.1:35432 root@10.0.0.2
    Restart=always
    RestartSec=10

    [Install]
    WantedBy=multi-user.target

`35432` is the loopback port PostgreSQL was published on above.

**On the second machine**, use the same image as the server, down to the
minor version (`docker save` on the server and `docker load` here is the
surest way), and copy the database once with `pg_basebackup`. `-R` writes the
standby's settings and `-S` names the slot:

    mkdir -p /srv/teanode-standby && chown 999:999 /srv/teanode-standby && chmod 700 /srv/teanode-standby
    docker run --rm --network host --user 999:999 -v /srv/teanode-standby:/standby pgvector/pgvector:pg17 \
      pg_basebackup -d "host=127.0.0.1 port=35432 user=replicator password=<the password> sslmode=require" \
      -D /standby -R -S standby -X stream -c fast

Then run it, on the loopback and a port of its own:

    services:
      postgres-standby:
        image: pgvector/pgvector:pg17
        command: ["postgres", "-c", "port=5433", "-c", "listen_addresses=127.0.0.1", "-c", "hot_standby=on"]
        restart: unless-stopped
        network_mode: host
        user: "999:999"
        volumes:
          - /srv/teanode-standby:/var/lib/postgresql/data

It answers read-only queries. On the server, `SELECT * FROM
pg_stat_replication` shows it streaming; on the standby, `SELECT
pg_is_in_recovery(), now() - pg_last_xact_replay_timestamp()` shows how far
behind it is. If the slot is ever dropped for holding too much WAL, the
standby stops and has to be copied again with `pg_basebackup`.

Should the server's database be lost, `SELECT pg_promote()` on the standby
makes it a primary, and the server's `.env` (or a new server) points at it.

## The MinIO mirror

Run a second MinIO on the second machine, on the disk meant for it and
reachable only from the private network:

    services:
      minio-backup:
        image: minio/minio:latest
        command: ["server", "/data"]
        restart: unless-stopped
        environment:
          MINIO_ROOT_USER: ${BACKUP_MINIO_ROOT_USER}
          MINIO_ROOT_PASSWORD: ${BACKUP_MINIO_ROOT_PASSWORD}
        ports:
          - "10.0.0.2:9100:9000"
        volumes:
          - /srv/teanode-minio-backup:/data

On the server, give the mirror a MinIO user that may only list, read and
watch the bucket, not the root credentials. A policy, saved as
`mirror-read.json` and copied into the MinIO container:

    {"Version": "2012-10-17", "Statement": [
      {"Effect": "Allow", "Action": ["s3:GetBucketLocation", "s3:ListBucket", "s3:ListenBucketNotification"], "Resource": ["arn:aws:s3:::teanode"]},
      {"Effect": "Allow", "Action": ["s3:GetObject"], "Resource": ["arn:aws:s3:::teanode/*"]}]}

    mc admin policy create local teanode-mirror-read /tmp/mirror-read.json
    mc admin user add local <mirror access key> <mirror secret key>
    mc admin policy attach local teanode-mirror-read --user <mirror access key>

Then a service beside MinIO in the server's compose file, with those
credentials and the second MinIO's in a `mirror.env` only root can read:

      minio-mirror:
        image: minio/minio:latest
        restart: always
        depends_on:
          minio:
            condition: service_healthy
        env_file: mirror.env
        entrypoint:
          - sh
          - -c
          - |
            mc alias set primary http://minio:9000 "$${MIRROR_ACCESS_KEY}" "$${MIRROR_SECRET_KEY}" >/dev/null &&
            mc alias set backup http://10.0.0.2:9100 "$${BACKUP_MINIO_ROOT_USER}" "$${BACKUP_MINIO_ROOT_PASSWORD}" >/dev/null &&
            mc mb --ignore-existing backup/teanode &&
            exec mc mirror --watch --overwrite --remove --retry --quiet primary/teanode backup/teanode

Started, it compares the two buckets and copies what is missing, then copies
each change as it happens. A few retries on `media/photo-upload-` objects are
normal: those are the parts of an upload in progress, which TeaNode deletes
once the upload is whole.

## The archive tier

MinIO can move objects to another MinIO by age and keep serving them: a
request for a moved object is answered by the server's MinIO, which fetches it
from the tier. TeaNode never notices, so the rule decides what moves.

What is worth moving is the originals of photos and videos. They are stored
as `media/photo-blob-<sha256>`; the smaller copies the library and the grid
show are `media/photo-preview-...`, and mail and recordings have names of
their own, so a rule on the `media/photo-blob-` prefix moves only the large
files, and browsing the library never touches the second machine.

Run a third MinIO on the second machine for the tier (or a bucket on the
backup one, though a disk of its own keeps the two roles apart), make a bucket
for it, and on the server:

    mc ilm tier add minio local ARCHIVE --endpoint http://10.0.0.2:9000 \
      --access-key <tier access key> --secret-key <tier secret key> --bucket teanode-tier --prefix teanode/
    mc ilm tier check local ARCHIVE
    mc ilm rule add local/teanode --prefix media/photo-blob- --transition-days 180 --transition-tier ARCHIVE

Before relying on it, prove the round trip with a throwaway bucket and a
zero-day rule: the object moves within minutes (`mc stat` shows its storage
class as the tier's name), `mc cat` still reads it through the server, and
removing it removes the tier's copy.

What to know before turning it on:

- **Age is upload age.** A phone's whole library uploaded in one evening moves
  in one evening, 180 days later.
- **The tier has to be up to open a moved original.** Browsing and previews
  keep working without it; opening a full-size photo, downloading one, or
  playing a moved video does not.
- **It is not a backup.** The mirror above copies the moved objects too,
  because it reads them through the server; without a mirror, a moved
  original lives only on the tier's disk.
- **It is hard to undo.** A tier cannot be removed while objects live on it;
  they have to be restored first.
