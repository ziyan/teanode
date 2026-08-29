#!/usr/bin/env bash
#
# Prepare a development server: write dev/.env if it is missing, then make
# sure the database behind it holds a configuration safe to run on a laptop.
#
# Idempotent. Each step checks for its own result rather than the whole script
# being skipped once dev/.env exists, so a run that failed halfway through
# completes on the next attempt instead of leaving a half-built dev setup.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

readonly BINARY="build/teanode"
readonly ENVIRONMENT="dev/.env"

if [[ ! -x "${BINARY}" ]]; then
  echo "building ${BINARY} first" >&2
  make build >/dev/null
fi

mkdir -p dev dev/data

# Everything a development box must not do: bind privileged ports, ask a
# certificate authority for a certificate it can never validate, or actually
# deliver mail to a stranger because an alias was left pointing somewhere real.
if [[ ! -f "${ENVIRONMENT}" ]]; then
  cat >"${ENVIRONMENT}" <<EOF
# Written by scripts/dev-config.bash. Gitignored, and not touched again once
# it exists — edit it freely.

TEANODE_DATABASE_URL=postgres://teanode:teanode@127.0.0.1:15432/teanode?sslmode=disable
TEANODE_INSTANCE_ID=dev

TEANODE_SERVER_NAME=mail.example.com
TEANODE_SERVER_DOMAIN=example.com
TEANODE_SERVER_DATA_DIRECTORY=$(pwd)/dev/data
TEANODE_SERVER_LOG_LEVEL=DEBUG

TEANODE_LISTEN_SMTP_INCOMING=127.0.0.1:10025
TEANODE_LISTEN_SMTP_OUTGOING=127.0.0.1:10587
TEANODE_LISTEN_HTTP=127.0.0.1:10081
TEANODE_LISTEN_HTTPS=

# Nothing leaves this machine: no certificate authority is asked for a
# certificate, and no mail is actually delivered.
TEANODE_TLS_ACME_ENABLED=false
TEANODE_SMTP_DISABLE_SEND=true

# A laptop is one host talking to itself, so there is no reverse DNS to check
# and requiring it would refuse every message.
TEANODE_SMTP_REQUIRE_REVERSE_DNS=false

# The object store, so that the multi-instance path is what gets exercised in
# development too. Started by "make dev-up".
TEANODE_S3_ENABLED=true
TEANODE_S3_ENDPOINT=http://127.0.0.1:19000
TEANODE_S3_BUCKET=teanode
TEANODE_S3_REGION=us-east-1
TEANODE_S3_PATH_STYLE=true
TEANODE_S3_ACCESS_KEY_ID=teanode
TEANODE_S3_SECRET_ACCESS_KEY=teanodeteanode
EOF
  echo "created ${ENVIRONMENT}"
fi

set -a
# shellcheck disable=SC1090
source "${ENVIRONMENT}"
set +a

# Migrate and store the configuration the environment describes. Does nothing
# to a database that already has one, which is what makes this safe to re-run.
"${BINARY}" config init >dev/init.log

# A development server needs a certificate for two reasons: the submission
# port only offers AUTH after STARTTLS, and a browser needs one for HTTPS.
# Nothing will trust it, which is fine here.
#
# The condition is what the configuration points at, not whether some file
# happens to exist: a rebuilt database leaves the old file behind, and
# checking the file alone silently produced a server with no TLS and therefore
# no way to authenticate.
if ! "${BINARY}" config show | grep -qE '^  certificateFile: .+'; then
  "${BINARY}" tls self-signed >>dev/init.log
fi

"${BINARY}" config validate >/dev/null

cat <<EOF
development server ready

  smtp in    127.0.0.1:10025      swaks --to hello@example.com --server 127.0.0.1:10025
  smtp out   127.0.0.1:10587
  dashboard  http://127.0.0.1:10081
  database   127.0.0.1:15432
  object     http://127.0.0.1:19001 (console; teanode / teanodeteanode)

Sending is disabled and ACME is off, so nothing leaves this machine.
Configuration is in the database — change it in the dashboard, or with
"teanode config show" and the other subcommands after sourcing ${ENVIRONMENT}.
EOF
