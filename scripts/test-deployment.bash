#!/usr/bin/env bash
#
# Bring up a whole TeaNode deployment in Docker and prove it works: the
# container image built from deploy/Dockerfile, PostgreSQL, migrations, the
# SMTP conversation in both directions, DKIM signing, the dashboard, the API
# and the command line client.
#
# This is what has to pass before the same image replaces a running server. It
# exercises the deployment, not the code — `make test` already covers the code,
# and passing unit tests have never once proved that a container starts.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

readonly COMPOSE=(docker compose -f deploy/docker-compose.test.yml)
readonly TEST_DIR="deploy/test"
readonly ENVIRONMENT="${TEST_DIR}/.env"
readonly BINARY="build/teanode"
readonly SERVER_BINARY="build/teanode-server"

# The database, as seen from this script. The server sees it as a service name
# on the compose network; docker-compose.test.yml overrides the variable.
readonly DATABASE_PORT=15433
readonly DATABASE_URL="postgres://teanode:teanode@127.0.0.1:${DATABASE_PORT}/teanode?sslmode=disable"

readonly SMTP_PORT=12525
readonly SUBMISSION_PORT=12587
readonly HTTP_PORT=12580
readonly MAILPIT_PORT=18025
readonly MINIO_PORT=19100

# The compose project name is taken from the "name:" in the test compose file;
# the default network is that plus "_default".
readonly COMPOSE_NETWORK="teanode-test_default"

readonly API="http://127.0.0.1:${HTTP_PORT}"
readonly GRAPHQL_PATH="/api/v1/graphql"
readonly MAILPIT="http://127.0.0.1:${MAILPIT_PORT}"

# The domain this deployment serves, and where its mail is forwarded. Neither
# resolves anywhere; mailpit is reached by name inside the compose network.
readonly DOMAIN="teanode.test"
readonly SENDER_DOMAIN="external.test"

# Fixed addresses, matching deploy/docker-compose.test.yml. The relay target is
# an address rather than a name so that forwarding does not also depend on the
# DNS server being right.
readonly TEANODE_IP="172.28.0.10"
readonly MAILPIT_IP="172.28.0.20"
readonly NETWORK="172.28.0.0/16"
readonly FORWARD_HOST="${MAILPIT_IP}"
readonly FORWARD_PORT=25

PASSED=0
FAILED=0

# --- reporting ---------------------------------------------------------------

step() { printf '\n\033[1m%s\033[0m\n' "$*"; }

pass() {
  PASSED=$((PASSED + 1))
  printf '  \033[32mok\033[0m    %s\n' "$*"
}

fail() {
  FAILED=$((FAILED + 1))
  printf '  \033[31mFAIL\033[0m  %s\n' "$*"
}

# check runs a command and reports whether it succeeded, without stopping the
# run. One failing check should not hide the state of the other twenty.
check() {
  local description="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    pass "${description}"
  else
    fail "${description}"
  fi
}

# check_contains asserts that a command's output contains a string, and prints
# what it got when it does not.
check_contains() {
  local description="$1" expected="$2"
  shift 2
  local output
  if ! output="$("$@" 2>&1)"; then
    fail "${description} (the command failed: ${output})"
    return
  fi
  if [[ "${output}" == *"${expected}"* ]]; then
    pass "${description}"
  else
    fail "${description} (expected ${expected@Q} in: ${output})"
  fi
}

# check_fails_with asserts that a command fails, and that it says why. A
# refusal that arrives as a success is the failure worth catching.
check_fails_with() {
  local description="$1" expected="$2"
  shift 2
  local output
  if output="$("$@" 2>&1)"; then
    fail "${description} (the command succeeded: ${output})"
    return
  fi
  if [[ "${output}" == *"${expected}"* ]]; then
    pass "${description}"
  else
    fail "${description} (expected ${expected@Q} in: ${output})"
  fi
}

# --- teardown ----------------------------------------------------------------

KEEP=${KEEP:-0}

cleanup() {
  local status=$?
  if [[ "${KEEP}" == "1" ]]; then
    printf '\nleaving the stack up (KEEP=1). Stop it with:\n  %s down --volumes\n' "${COMPOSE[*]}"
    return "${status}"
  fi
  printf '\ntearing down\n'
  "${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  remove_test_directory
  return "${status}"
}
trap cleanup EXIT

# remove_test_directory deletes deploy/test. The server wrote into it as the
# container's user, so some of it is not ours to delete; a throwaway container
# does it instead of asking for a password.
remove_test_directory() {
  [[ -d "${TEST_DIR}" ]] || return 0
  if ! rm -rf "${TEST_DIR}" 2>/dev/null; then
    docker run --rm -v "$(pwd)/${TEST_DIR}:/target" alpine \
      sh -c 'rm -rf /target/..?* /target/.[!.]* /target/*' >/dev/null 2>&1 || true
    rm -rf "${TEST_DIR}" 2>/dev/null || true
  fi
}

# --- the configuration this deployment runs on --------------------------------

# teanode_server runs the server binary's own commands — the ones that write
# the database directly — against the deployment's database, which is what an
# operator on the server does. It is the same environment the container gets,
# except for the address of the database.
teanode_server() {
  # The database address is the only thing that differs from what the
  # container gets. Nothing else is overridden — in particular not the object
  # store endpoint, which is a service name: no command run here connects to
  # it, and storing the host's view of it would leave the container reaching
  # for an address that means something else inside.
  env TEANODE_DATABASE_URL="${DATABASE_URL}" "${SERVER_BINARY}" "$@"
}

# teanode_local runs the client the way somebody on the server's own console
# would: with the server's environment and no token, so it reaches the server
# over loopback with a token minted from the stored secret.
teanode_local() {
  env TEANODE_DATABASE_URL="${DATABASE_URL}" "${BINARY}" "$@"
}

build_environment() {
  step "Writing the environment"

  # Always rebuilt, never reused. The image is built from the working tree on
  # every run, and a stale binary here would set the deployment up one way and
  # then be tested against a server that reads it another way — which is a
  # confusing hour, and happened.
  make build >/dev/null

  remove_test_directory
  mkdir -p "${TEST_DIR}/data"
  local data_directory
  data_directory="$(cd "${TEST_DIR}/data" && pwd)"

  # The data directory is the host path for now, so that "tls self-signed"
  # below writes where this script can see. It becomes the container path at
  # the end, once nothing else has to touch those files.
  cat >"${ENVIRONMENT}" <<ENVIRONMENT
TEANODE_DATABASE_URL=${DATABASE_URL}
TEANODE_INSTANCE_ID=test1

TEANODE_SERVER_NAME=mail.${DOMAIN}
TEANODE_SERVER_DOMAIN=${DOMAIN}
TEANODE_SERVER_DATA_DIRECTORY=${data_directory}
TEANODE_SERVER_LOG_LEVEL=DEBUG

# Where an upgrade stages a binary the running one cannot replace. Named
# outright rather than left to follow the data directory, because the data
# directory above is the host path until the last step of the setup and this
# one has to be the path inside the container from the start: it is read
# before the database is opened, so nothing can correct it later.
TEANODE_UPGRADE_DIRECTORY=/var/lib/teanode/upgrade

TEANODE_LISTEN_SMTP_INCOMING=:25
TEANODE_LISTEN_SMTP_OUTGOING=:587
TEANODE_LISTEN_HTTP=:80
TEANODE_LISTEN_HTTPS=

# No certificate authority is going to validate a domain nobody owns, and
# leaving ACME on would spend somebody's rate limit.
TEANODE_TLS_ACME_ENABLED=false

# Inside a container network the connecting address is the Docker gateway,
# which has no reverse DNS, so the check would refuse every message. This is
# the same reason the production compose uses host networking, and the reason
# the setting exists at all.
TEANODE_SMTP_REQUIRE_REVERSE_DNS=false

TEANODE_S3_ENABLED=true
TEANODE_S3_ENDPOINT=http://minio:9000
TEANODE_S3_BUCKET=teanode
TEANODE_S3_REGION=us-east-1
TEANODE_S3_PATH_STYLE=true
TEANODE_S3_ACCESS_KEY_ID=teanode
TEANODE_S3_SECRET_ACCESS_KEY=teanodeteanode
ENVIRONMENT

  chmod 666 "${ENVIRONMENT}"
  pass "environment written to ${ENVIRONMENT}"
}

# start_database brings up everything the server needs before it can start, so
# that the configuration can be put in place the way an operator would: with
# the command line client, against the database, before the server exists.
start_database() {
  step "Starting the database and the object store"
  "${COMPOSE[@]}" up -d postgres minio minio-bucket >/dev/null
  local attempt
  for attempt in $(seq 1 60); do
    if "${COMPOSE[@]}" exec -T postgres pg_isready -U teanode >/dev/null 2>&1; then
      pass "postgres is up (after ${attempt}s)"
      return 0
    fi
    sleep 1
  done
  fail "postgres never came up"
  return 1
}

configure_deployment() {
  step "Configuring the deployment"

  set -a
  # shellcheck disable=SC1090
  source "${ENVIRONMENT}"
  set +a

  # Migrate, and store the configuration the environment describes. This is
  # the first-run path: the same thing "teanode run" would do by itself.
  teanode_server config init >/dev/null
  pass "the database was migrated and configured from the environment"

  # A certificate for STARTTLS, without which the submission port will not
  # accept a password. Self-signed, because no authority will validate a
  # domain that does not exist.
  teanode_server tls self-signed >/dev/null
  pass "a self-signed certificate was generated"

  # The rest cannot come from the environment: an alias forwarding to mailpit,
  # and the data directory as the container sees it. Both are done the way an
  # operator changes a stored configuration wholesale — export, edit, import —
  # which also exercises the pair of commands a migration depends on.
  local exported
  exported="$(mktemp)"
  teanode_server config export --file "${exported}" --force >/dev/null

  python3 - "${exported}" "${DOMAIN}" "${FORWARD_HOST}" "${FORWARD_PORT}" <<'PYTHON'
import sys

path, domain, forward_host, forward_port = sys.argv[1:5]
with open(path) as handle:
    content = handle.read()

def replace(old, new):
    global content
    if old not in content:
        raise SystemExit(f"test-deployment: expected {old!r} in the exported configuration")
    content = content.replace(old, new, 1)

# A catch-all forwarding to mailpit rather than to an address on the internet,
# so the test can go and look at what arrived instead of trusting a log line.
# The seeded domain has no alias at all, which is a domain that refuses mail.
replace("""    aliases: []""", """    aliases:
      - id: 01K2ZQ7B8N6H4K2QDX8ZR5VTAE
        pattern: ^hello$
        kind: mailServer
        mailServer:
          host: %s
          port: %s
      - id: 01K2ZQ7B8N6H4K2QDX8ZR5VTAF
        pattern: ""
        kind: mailServer
        mailServer:
          host: %s
          port: %s""" % (forward_host, forward_port, forward_host, forward_port))

# Last, because "tls self-signed" resolved its paths against the host copy.
replace("  dataDirectory: ", "  dataDirectory: /var/lib/teanode  # was: ")

with open(path, "w") as handle:
    handle.write(content)
PYTHON

  teanode_server config import --file "${exported}" --force >/dev/null
  rm -f "${exported}"
  pass "the aliases and the data directory were loaded back in"

  # Last, once the certificate and the key have been written: the container
  # runs as a user that is not this one, and a key written here with mode 600
  # is a key it cannot read.
  chmod -R 777 "${TEST_DIR}/data"

  build_zone
}

# build_zone writes the DNS the deployment is checked against: the records a
# real one would have published, including the signing key generated a moment
# ago. Without them SPF has nothing to read, DKIM cannot be verified, and DMARC
# refuses the message — which is correct behaviour, and useless as a test.
build_zone() {
  mkdir -p "${TEST_DIR}/dns"

  cat >"${TEST_DIR}/dns/Corefile" <<COREFILE
test.:53 {
    file /etc/coredns/db.test
    errors
}

# Everything else goes to Docker's own resolver, so service names still work.
.:53 {
    forward . 127.0.0.11
    errors
}
COREFILE

  local dkim_value
  dkim_value="$(teanode_local dkim show "${DOMAIN}" --json 2>/dev/null |
    python3 -c 'import json,sys; print(json.load(sys.stdin)["record"]["expected"])')"

  python3 - "${TEST_DIR}/dns/db.test" "${DOMAIN}" "${SENDER_DOMAIN}" \
    "${TEANODE_IP}" "${MAILPIT_IP}" "${NETWORK}" "${dkim_value}" <<'PYTHON'
import sys

path, domain, sender_domain, teanode_ip, mailpit_ip, network, dkim = sys.argv[1:8]

# A TXT string is at most 255 characters, and a DKIM key is longer, so the
# value is published as several strings the resolver joins back together.
def strings(value):
    chunks = [value[index:index + 255] for index in range(0, len(value), 255)]
    return " ".join('"%s"' % chunk.replace('"', '\\"') for chunk in chunks)

# Names are written relative to the "test." origin.
def relative(name):
    return name[: -len(".test")] if name.endswith(".test") else name

lines = [
    "$ORIGIN test.",
    "$TTL 60",
    "@ IN SOA ns.test. hostmaster.test. 1 7200 3600 1209600 60",
    "@ IN NS ns.test.",
    "ns IN A %s" % teanode_ip,
    "",
    "; The sending domain: where its mail is allowed to come from, and where",
    "; a bounce would go. TeaNode refuses a From domain with no MX record, so",
    "; it needs one even though nothing replies to it here.",
    "%s IN A %s" % (relative(sender_domain), mailpit_ip),
    "%s IN MX 10 mail.%s." % (relative(sender_domain), sender_domain),
    "mail.%s IN A %s" % (relative(sender_domain), mailpit_ip),
    "%s IN TXT %s" % (relative(sender_domain), strings("v=spf1 ip4:%s -all" % network)),
    "",
    "; The domain this deployment serves.",
    "%s IN A %s" % (relative(domain), teanode_ip),
    "%s IN MX 10 mail.%s." % (relative(domain), domain),
    "mail.%s IN A %s" % (relative(domain), teanode_ip),
    "%s IN TXT %s" % (relative(domain), strings("v=spf1 ip4:%s -all" % network)),
    "_dmarc.%s IN TXT %s" % (relative(domain), strings("v=DMARC1; p=reject; rua=mailto:dmarc@%s" % domain)),
    "teanode1._domainkey.%s IN TXT %s" % (relative(domain), strings(dkim)),
    "",
]
with open(path, "w") as handle:
    handle.write("\n".join(lines))
PYTHON

  chmod -R 755 "${TEST_DIR}/dns"
}

# --- bringing it up -----------------------------------------------------------

start_stack() {
  step "Building the image and starting the stack"
  "${COMPOSE[@]}" build --quiet teanode
  pass "image built from deploy/Dockerfile"

  "${COMPOSE[@]}" up -d
  pass "stack started"

  step "Waiting for the server"
  local attempt
  for attempt in $(seq 1 60); do
    if graphql '{ GetSession { authenticationRequired } }' 2>/dev/null | grep -q authenticationRequired; then
      pass "the server answers on ${API} (after ${attempt}s)"
      return 0
    fi
    sleep 1
  done

  fail "the server never answered on ${API}"
  "${COMPOSE[@]}" logs teanode | tail -40
  return 1
}

# --- the checks ---------------------------------------------------------------

check_migrations() {
  step "Database"
  check_contains "migrations ran and the schema exists" "| mail " \
    "${COMPOSE[@]}" exec -T postgres psql -U teanode -d teanode -c '\dt'
  check_contains "the migration table records what ran" "migration" \
    "${COMPOSE[@]}" exec -T postgres psql -U teanode -d teanode -c '\dt'
}

check_dashboard() {
  step "Dashboard and API"
  check_contains "the dashboard is served" "<!doctype html" \
    curl -sS "${API}/"
  check_contains "the API reports no account yet" '"authenticationRequired":false' \
    graphql '{ GetSession { authenticated authenticationRequired username } }'
  # An unclaimed server refuses everything except the two operations that
  # claiming it needs. It used to answer anyone, on the reasoning that
  # onboarding has to be reachable by somebody who cannot log in — but that
  # handed the configuration to whoever found the server first.
  check_contains "an unclaimed server refuses a query" "not logged in" \
    graphql '{ ListDomains { id } }'

  check_contains "an unclaimed server still answers GetSession" '"authenticationRequired":false' \
    graphql '{ GetSession { authenticationRequired } }' 
}

# graphql posts a query and prints the reply. Everything the dashboard does
# goes through this one endpoint, so the harness talks to the server the same
# way rather than through a second surface that could drift from it.
graphql() {
  local query="$1"
  shift
  curl -sS -X POST -H 'Content-Type: application/json' \
    --data-binary "$(python3 -c 'import json,sys; print(json.dumps({"query": sys.argv[1]}))' "${query}")" \
    "$@" "${API}${GRAPHQL_PATH}"
}

# teanode_cli runs the command line client against the stack, the way somebody
# administering it from another machine would.
# psql_value runs one query and prints the single value it answers with.
psql_value() {
  "${COMPOSE[@]}" exec -T postgres psql -U teanode -d teanode -At -c "$1" 2>/dev/null | tr -d '\r'
}

teanode_cli() {
  TEANODE_URL="${API}" TEANODE_TOKEN="${TOKEN:-}" "${BINARY}" "$@"
}

check_onboarding() {
  step "Onboarding"

  check_contains "the first account can be created" '"username":"operator"' \
    graphql 'mutation { CreateFirstAccount(username: "operator", password: "a-test-password") { authenticated username } }'

  check_contains "a second account cannot claim the server" "already exists" \
    graphql 'mutation { CreateFirstAccount(username: "intruder", password: "another-password") { authenticated username } }'

  check_contains "logging in returns a session" '"username":"operator"' \
    graphql 'mutation { Login(username: "operator", password: "a-test-password") { authenticated username } }'

  check_contains "a wrong password is refused" "not logged in" \
    graphql 'mutation { Login(username: "operator", password: "not-the-password") { authenticated username } }'

  check_contains "the server now says authentication is required" '"authenticationRequired":true' \
    graphql '{ GetSession { authenticationRequired } }'

  # Still refused once claimed, for the ordinary reason rather than because
  # the server has nobody. Its predecessor expected an empty string, which
  # matches any reply at all, so it passed however the server behaved.
  check_contains "once claimed, an unauthenticated query is refused" "not logged in" \
    graphql '{ ListDomains { id } }' 
}

issue_token() {
  step "API token"

  # Issued the way the dashboard would: log in for a session, then ask for a
  # token with it. The client cannot mint a local one here, because that reads
  # the port out of the configuration and the server is behind a port mapping
  # — which is exactly the situation --url and a token exist for.
  local jar created
  jar="$(mktemp)"
  graphql 'mutation { Login(username: "operator", password: "a-test-password") { authenticated } }' \
    -c "${jar}" >/dev/null

  created="$(graphql 'mutation { CreateToken(name: "test-harness") { secret token { id username } } }' -b "${jar}")"
  rm -f "${jar}"

  TOKEN="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["CreateToken"]["secret"])' <<<"${created}" 2>/dev/null || true)"
  if [[ -z "${TOKEN}" ]]; then
    fail "the issued token was empty"
    return 1
  fi
  pass "issued a token through the API, as the dashboard does"

  check_contains "the token authenticates" "operator" \
    teanode_cli user list
  check_fails_with "a wrong token does not" "not logged in" \
    env TEANODE_URL="${API}" TEANODE_TOKEN="tnt_nope_nope" "${BINARY}" user list
}

check_cli() {
  step "Command line client, over the network"

  check_contains "api list reaches the whole schema" "ListDomains" \
    teanode_cli api list
  check_contains "the configured domain is there" "${DOMAIN}" \
    teanode_cli api call ListDomains --select "{ id domain }"
  check_contains "a domain has a signing key, generated with it" '"hasKey": true' \
    teanode_cli dkim show "${DOMAIN}" --json
  check_contains "the catch-all alias matches an arbitrary address" "mailServer" \
    teanode_cli api call MatchAliases domainId="$(domain_id)" address="anything@${DOMAIN}" --select "{ id pattern kind }"
  check_contains "the specific alias matches the address it was written for" '"^hello$"' \
    teanode_cli api call MatchAliases domainId="$(domain_id)" address="hello@${DOMAIN}" --select "{ pattern }"

  # The typed commands, which are what an operator actually types.
  check_contains "domain list shows the domain and its records" "${DOMAIN}" \
    teanode_cli domain list
  check_contains "alias match names the alias by its pattern" '^hello$' \
    teanode_cli alias match "${DOMAIN}" "hello@${DOMAIN}"
  check_contains "server status names the instance" "test1" \
    teanode_cli server status
  check_contains "settings show reads the integrations" "antispam.enabled" \
    teanode_cli settings show
  check_contains "mail list answers, empty or not" "" \
    teanode_cli mail list --first 1

  # A change made through the API has to reach the database, or a restart
  # loses it — and so does every other instance. Read back through a separate
  # connection rather than through the same server, so that what is checked is
  # what was stored and not what one process happens to be holding.
  teanode_cli domain create second.test >/dev/null 2>&1 || true
  check_contains "a change made through the API reaches the database" "second.test" \
    teanode_server config show

  check_profiles
}

# check_profiles signs the client in the way a laptop would, with a pasted
# token, and uses the saved profile with no environment at all. The profiles
# file goes under a throwaway configuration directory, so the developer's own
# is never touched.
check_profiles() {
  step "Command line client, signed in as a profile"

  local configuration
  configuration="$(mktemp -d)"
  check_contains "auth login saves a profile from a pasted token" "saved profile" \
    env XDG_CONFIG_HOME="${configuration}" "${BINARY}" auth login --url "${API}" --token "${TOKEN}" --name harness
  check_contains "the profile is active" "harness" \
    env XDG_CONFIG_HOME="${configuration}" "${BINARY}" auth status
  check_contains "a command with no environment talks to the profile's server" "${DOMAIN}" \
    env XDG_CONFIG_HOME="${configuration}" "${BINARY}" domain list
  check "the profiles file is readable by its owner only" \
    test "$(stat -c %a "${configuration}/teanode/profiles.json")" = "600"
  check_contains "auth logout forgets the profile" "forgot profile" \
    env XDG_CONFIG_HOME="${configuration}" "${BINARY}" auth logout --keep-token
  # This shell still carries the server's environment from build_environment,
  # which is exactly the console path; a laptop has none of it.
  check_fails_with "with no profile and no environment there is nothing to talk to" "no server to talk to" \
    env -u TEANODE_DATABASE_URL XDG_CONFIG_HOME="${configuration}" "${BINARY}" domain list
  rm -rf "${configuration}"
}

domain_id() {
  teanode_cli api call ListDomains --select "{ id domain }" 2>/dev/null |
    python3 -c "import json,sys; print(next(d['id'] for d in json.load(sys.stdin) if d['domain']=='${DOMAIN}'))"
}

check_incoming_mail() {
  step "Receiving mail on port 25, and forwarding it"

  local subject="parity-$(date +%s)"
  python3 - "${SMTP_PORT}" "${DOMAIN}" "${subject}" "${SENDER_DOMAIN}" <<'PYTHON'
import smtplib, sys
from email.message import EmailMessage

port, domain, subject, sender_domain = int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]

message = EmailMessage()
message["From"] = f"sender@{sender_domain}"
message["To"] = f"anything@{domain}"
message["Subject"] = subject
message.set_content("Sent by scripts/test-deployment.bash.")

with smtplib.SMTP("127.0.0.1", port, timeout=30) as smtp:
    smtp.send_message(message)
PYTHON
  pass "the server accepted a message for ${DOMAIN}"

  # Forwarding is asynchronous, so wait for it rather than guessing.
  local attempt
  for attempt in $(seq 1 30); do
    if curl -sS "${MAILPIT}/api/v1/messages" 2>/dev/null | grep -q "${subject}"; then
      pass "the message was forwarded and arrived at the destination (after ${attempt}s)"
      check_forwarded_message "${subject}"
      return 0
    fi
    sleep 1
  done

  fail "the message never reached the forwarding destination"
  "${COMPOSE[@]}" logs teanode | tail -30
}

# check_forwarded_message looks at what actually arrived. Delivering something
# is not the same as delivering it intact and correctly attested.
#
# A forwarded message keeps the original From, so re-signing it with this
# domain's DKIM key would produce a signature that does not align and proves
# nothing. ARC is the mechanism for this: it seals what the authentication
# results were on arrival, so the next hop can see them even though forwarding
# broke SPF.
check_forwarded_message() {
  local subject="$1" raw
  raw="$(fetch_message "${subject}")"

  header_present "the forwarded message is ARC sealed" "ARC-Seal" "${raw}"
  header_present "the ARC message signature is there" "ARC-Message-Signature" "${raw}"
  header_present "the authentication results seen on arrival are sealed in" \
    "ARC-Authentication-Results" "${raw}"
  header_present "the authentication results are recorded on the message" \
    "Authentication-Results" "${raw}"

  # The d= has to be the domain, because that is where the key is published.
  # It named the mail host once, and every seal was unverifiable.
  if grep -qi "^ARC-Seal:.*d=${DOMAIN};" <<<"${raw}"; then
    pass "the seal names ${DOMAIN}, where a receiver will find the key"
  else
    fail "the seal does not name ${DOMAIN}: $(grep -io 'd=[^;]*' <<<"${raw}" | head -1)"
  fi
  # Nothing announces the software and its version to every recipient. The
  # Received header names the host that handled the message, which is what a
  # reader tracing delivery needs; a second header saying what is running here
  # told them nothing they could use and told an attacker what to look up.
  if grep -qi "^X-Forwarding-Service:" <<<"${raw}"; then
    fail "the forwarded message announces the software to its recipient"
  else
    pass "no header announces what software forwarded it"
  fi

  # Feedback-ID belongs to whoever sent the message. This server used to add
  # its own to every delivery, carrying the delivery identifier — a value that
  # groups nothing, on a header that exists so a receiver can group complaints
  # by sender, and which arrived alongside the original sender's on every
  # message from a large platform. A forwarded message gets none.
  if grep -qi "^Feedback-ID:" <<<"${raw}"; then
    fail "the forwarded message carries a Feedback-ID this server added"
  else
    pass "no Feedback-ID was invented for the forwarded message"
  fi

  if grep -q "spf=pass" <<<"${raw}"; then
    pass "SPF passed on arrival and is recorded"
  else
    fail "SPF did not pass; the sending domain's record should have allowed it"
  fi
  if grep -qi "^Subject:.*${subject}" <<<"${raw}"; then
    pass "the message arrived intact"
  else
    fail "the subject did not survive forwarding"
  fi
}

# fetch_message returns the raw message whose subject contains a string.
fetch_message() {
  local subject="$1" id
  id="$(curl -sS "${MAILPIT}/api/v1/messages" |
    python3 -c "import json,sys; print(next(m['ID'] for m in json.load(sys.stdin)['messages'] if '${subject}' in m['Subject']))")"
  curl -sS "${MAILPIT}/api/v1/message/${id}/raw"
}

# fetch_message_html returns a message's decoded HTML body.
#
# Not the raw form: that is quoted-printable, which breaks a long line with a
# soft break wherever it likes, and the addresses this reads are long enough
# to be split down the middle.
fetch_message_html() {
  local subject="$1" id
  id="$(curl -sS "${MAILPIT}/api/v1/messages" |
    python3 -c "import json,sys; print(next(m['ID'] for m in json.load(sys.stdin)['messages'] if '${subject}' in m['Subject']))")"
  curl -sS "${MAILPIT}/api/v1/message/${id}" |
    python3 -c "import json,sys; print(json.load(sys.stdin).get('HTML',''))"
}

header_present() {
  local description="$1" header="$2" raw="$3"
  if grep -qi "^${header}:" <<<"${raw}"; then
    pass "${description}"
  else
    fail "${description} (no ${header} header)"
  fi
}

# await_message waits for a message to reach the destination, since delivery is
# asynchronous.
await_message() {
  local subject="$1" attempt
  for attempt in $(seq 1 30); do
    if curl -sS "${MAILPIT}/api/v1/messages" 2>/dev/null | grep -q "${subject}"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

check_rejections() {
  step "Refusing what it should refuse"

  # 5.1.9 Mailbox unavailable: this server has no domain by that name, so it
  # has no mailbox to deliver to and will not relay it onward either.
  check_contains "mail for a domain this server does not serve is refused" "550" \
    smtp_conversation "${SMTP_PORT}" "sender@${SENDER_DOMAIN}" "someone@not-configured.test"
  # Refused for want of a credential. The code is 503 rather than 530 because
  # the server will not take MAIL FROM at all before AUTH on this port.
  check_contains "submission without a credential is refused" "550, 503, 530" \
    smtp_status "${SUBMISSION_PORT}" "someone@${DOMAIN}" "elsewhere@${SENDER_DOMAIN}"
}

# smtp_conversation attempts one delivery and prints whatever the server said,
# including the refusal, which is the part being asserted on.
smtp_conversation() {
  python3 - "$1" "$2" "$3" <<'PYTHON'
import smtplib, sys

port, sender, recipient = int(sys.argv[1]), sys.argv[2], sys.argv[3]
body = (f"From: {sender}\r\nTo: {recipient}\r\n"
        "Subject: should be refused\r\n\r\nbody\r\n")
try:
    with smtplib.SMTP("127.0.0.1", port, timeout=30) as smtp:
        smtp.sendmail(sender, [recipient], body)
except smtplib.SMTPException as error:
    print(error)
else:
    print("accepted")
PYTHON
}

# smtp_status reports which of the refusal codes came back, so the assertion
# does not depend on which stage of the conversation the server stops at.
smtp_status() {
  local output
  output="$(smtp_conversation "$@")"
  case "${output}" in
    *"(550"*) echo "refused: 550, 503, 530" ;;
    *"(503"*) echo "refused: 550, 503, 530" ;;
    *"(530"*) echo "refused: 550, 503, 530" ;;
    *) echo "not refused: ${output}" ;;
  esac
}

check_submission() {
  step "Sending through the server with a credential"

  local created username password
  created="$(teanode_cli credential add "${DOMAIN}" --comment "test harness" --json 2>/dev/null)" || {
    fail "could not create a credential"
    return
  }
  username="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["username"])' <<<"${created}")"
  password="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["password"])' <<<"${created}")"
  pass "created an SMTP credential"

  # Addressed outside this server, so delivery goes through an MX lookup and
  # a connection out, which is the path real outgoing mail takes.
  local subject="submission-$(date +%s)"
  if python3 - "${SUBMISSION_PORT}" "${username}" "${password}" "${DOMAIN}" "${subject}" "${SENDER_DOMAIN}" <<'PYTHON'
import smtplib, sys
from email.message import EmailMessage

port, username, password, domain, subject, elsewhere = (
    int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5], sys.argv[6])

message = EmailMessage()
message["From"] = f"noreply@{domain}"
message["To"] = f"someone@{elsewhere}"
message["Subject"] = subject
message.set_content("Sent through the submission port.")

with smtplib.SMTP("127.0.0.1", port, timeout=30) as smtp:
    smtp.starttls()
    smtp.login(username, password)
    smtp.send_message(message)
PYTHON
  then
    pass "the credential authenticated over STARTTLS and the message was accepted"
  else
    fail "could not send through the submission port"
    return
  fi

  if await_message "${subject}"; then
    pass "it was delivered by looking up the recipient's MX record"
  else
    fail "the submitted message never reached the recipient's mail server"
    return
  fi

  # This one TeaNode originates, so the From is its own domain and a DKIM
  # signature aligns. Unlike a forwarded message, it should carry one.
  local raw
  raw="$(fetch_message "${subject}")"
  header_present "the message this server originated is DKIM signed" "DKIM-Signature" "${raw}"
  if grep -qi "^DKIM-Signature:.*d=${DOMAIN}\|d=${DOMAIN}" <<<"${raw}"; then
    pass "signed as ${DOMAIN}, with the key published in DNS"
  else
    fail "the signature is not for ${DOMAIN}"
  fi

  # And this one does carry a Feedback-ID, because this server was asked to
  # send it: the identifier is the domain, which is what a receiver groups a
  # sender's complaints by and has to mean the same sender every time.
  if grep -qi "^Feedback-ID: *${DOMAIN}\b" <<<"${raw}"; then
    pass "the message this server sent identifies its sender for complaints"
  else
    fail "no Feedback-ID naming ${DOMAIN}: $(grep -i '^Feedback-ID:' <<<"${raw}" | head -1)"
  fi

  check_contains "the credential can be looked up again" "${username}" \
    teanode_cli credential list --show-passwords
}

# --- pictures in mail ---------------------------------------------------------

# check_media proves the whole of the picture path over HTTP, which is where it
# lives: a file uploaded through the API, served back, put in a message, and
# rewritten on the way out into an address that belongs to that one message —
# then fetched, and counted.
check_media() {
  step "Pictures in mail, and whether they were fetched"

  # The smallest real PNG. Written by a library rather than pasted as base64
  # so that what is uploaded is a file the server's own sniffing agrees is a
  # picture, which is what it checks before storing anything.
  local picture
  picture="$(mktemp /tmp/teanode-test-XXXXXX.png)"
  python3 - "${picture}" <<'PYTHON'
import struct, sys, zlib

def chunk(kind, payload):
    body = kind + payload
    return struct.pack(">I", len(payload)) + body + struct.pack(">I", zlib.crc32(body))

raw = b"\x00" + b"\xff\x00\x00" + b"\x00" + b"\x00\x00\xff"
png = (b"\x89PNG\r\n\x1a\n"
       + chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 1, 8, 2, 0, 0, 0))
       + chunk(b"IDAT", zlib.compress(raw))
       + chunk(b"IEND", b""))
open(sys.argv[1], "wb").write(png)
PYTHON

  local uploaded mediaId
  uploaded="$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
    -F "domainId=$(domain_id)" -F "file=@${picture};type=image/png" \
    "${API}/api/v1/media")"
  rm -f "${picture}"
  mediaId="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("id",""))' <<<"${uploaded}" 2>/dev/null || true)"
  if [[ -n "${mediaId}" ]]; then
    pass "a picture uploaded through the API and was stored"
  else
    fail "the upload was refused: ${uploaded}"
    return
  fi

  # Served back at its own address, with the type the bytes actually are and
  # not the one the upload claimed.
  local served
  served="$(curl -sS -o /dev/null -w '%{http_code} %{content_type}' "${API}/media/${mediaId}")"
  if [[ "${served}" == "200 image/png" ]]; then
    pass "it is served back as image/png at its own address"
  else
    fail "fetching the picture answered ${served}"
  fi

  # Nobody may upload one. The endpoint is behind the session everything else
  # an operator does is behind, and this is the check that it stays there.
  local unauthenticated
  unauthenticated="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${API}/api/v1/media")"
  if [[ "${unauthenticated}" == "401" ]]; then
    pass "a stranger cannot upload a picture"
  else
    fail "an unauthenticated upload answered ${unauthenticated}, not 401"
  fi

  # Sent the way the compose page sends: the body names the picture by the
  # address the editor wrote, and the server is the one that turns it into an
  # address of its own.
  local subject sent mailId
  subject="picture-$(date +%s)"
  sent="$(graphql "mutation { SendMail(domainId: \"$(domain_id)\", messageParameters: {
      from: \"noreply@${DOMAIN}\"
      to: [\"someone@${SENDER_DOMAIN}\"]
      subject: \"${subject}\"
      htmlContent: \"<p>hello</p><img src=\\\"/media/${mediaId}\\\" alt=\\\"a picture\\\">\"
      textContent: \"hello\"
    }) { mail { id } } }" -H "Authorization: Bearer ${TOKEN}")"
  mailId="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["SendMail"]["mail"]["id"])' <<<"${sent}" 2>/dev/null || true)"
  if [[ -n "${mailId}" ]]; then
    pass "a message carrying the picture was accepted"
  else
    fail "the message was not sent: ${sent}"
    return
  fi

  if ! await_message "${subject}"; then
    fail "the message with the picture never arrived"
    return
  fi

  local body token
  body="$(fetch_message_html "${subject}")"

  # The address in the delivered message is under the sending domain. This is
  # the point of the whole rewrite: a recipient reading where the picture came
  # from learns the domain that wrote to them and nothing else about who runs
  # the server.
  if grep -q "https://mail\.${DOMAIN}/m/" <<<"${body}"; then
    pass "the picture is addressed under mail.${DOMAIN}"
  else
    fail "no address under the sending domain: $(grep -io 'src=[^ >]*' <<<"${body}" | head -2 | tr '\n' ' ')"
  fi
  if grep -q "/media/${mediaId}" <<<"${body}"; then
    fail "the message still names the editor's address, which every message would share"
  else
    pass "the editor's address was replaced, so the address belongs to this message alone"
  fi

  token="$(grep -o "/m/[a-z2-7]\{26\}" <<<"${body}" | head -1 | cut -d/ -f3)"
  if [[ -z "${token}" ]]; then
    fail "could not find the per-message address in the delivered message"
    return
  fi

  # Nothing has fetched it yet, so the message is trackable and unopened. The
  # difference between those two matters: a message with no picture is not
  # unopened, it is unanswerable.
  check_contains "before anybody looks, it is trackable and not opened" '"opened": false' \
    teanode_cli api call GetMailOpens mailId="${mailId}" --select "{ trackable opened openCount }"

  # One request, read twice: a second would be a second open, and the count
  # below is the point of the whole check.
  local fetched headers
  headers="$(curl -sS -D- -o /dev/null -w 'result: %{http_code} %{content_type}\n' "${API}/m/${token}")"
  fetched="$(grep '^result:' <<<"${headers}" | cut -d' ' -f2-)"
  if [[ "${fetched}" == "200 image/png" ]]; then
    pass "the per-message address serves the picture"
  else
    fail "fetching the per-message address answered ${fetched}"
  fi

  # And nothing between here and the reader may keep a copy. One cached
  # response and every open after the first is answered by somebody else,
  # invisibly: the count stops at one, and an operator reads that as nobody
  # having looked again. A CDN in front of this server did exactly that.
  if grep -qi '^cache-control:.*no-store' <<<"${headers}"; then
    pass "the per-message address may not be cached by anything"
  else
    fail "the per-message address is cacheable: $(grep -i '^cache-control:' <<<"${headers}" | tr -d '\r')"
  fi
  headers="$(curl -sS -D- -o /dev/null "${API}/media/${mediaId}")"
  if grep -qi '^cache-control:.*immutable' <<<"${headers}"; then
    pass "the picture at its own address is cached for a year, where it should be"
  else
    fail "the picture at its own address is not cacheable: $(grep -i '^cache-control:' <<<"${headers}" | tr -d '\r')"
  fi

  local opens firstOpen
  opens="$(teanode_cli api call GetMailOpens mailId="${mailId}" --select "{ opened openedAt lastOpenedAt openCount }" 2>/dev/null)"
  firstOpen="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("openedAt",""))' <<<"${opens}" 2>/dev/null || true)"
  if grep -q '"opened": true' <<<"${opens}" && grep -q '"openCount": 1' <<<"${opens}"; then
    pass "one fetch is recorded as one open, with the time it happened"
  else
    fail "the fetch was not recorded: ${opens}"
  fi

  # Fetched again. The count goes up and the first time stays, because the
  # first time is the answer to a different question than how many times.
  curl -sS -o /dev/null "${API}/m/${token}"
  opens="$(teanode_cli api call GetMailOpens mailId="${mailId}" --select "{ opened openedAt lastOpenedAt openCount }" 2>/dev/null)"
  if grep -q '"openCount": 2' <<<"${opens}"; then
    pass "a second fetch counts twice"
  else
    fail "the second fetch was not counted: ${opens}"
  fi
  if [[ -n "${firstOpen}" ]] && grep -q "\"openedAt\": \"${firstOpen}\"" <<<"${opens}"; then
    pass "the first fetch stays the first"
  else
    fail "the first open time moved: ${opens}"
  fi

  # The list asks about a page of messages at once rather than one at a
  # time, and that is a second resolver with its own way of being wrong.
  check_contains "the list can ask about several messages at once" '"openCount": 2' \
    teanode_cli api call ListMailOpens mailIds:="[\"${mailId}\"]" --select "{ mailId trackable opened openCount }"

  # Where the address is fetched from is the operator's to choose. A mail
  # server name points at a host whose port 443 may belong to something else
  # entirely, and then the mail arrives while every picture in it is broken —
  # so a domain can name where its HTTPS actually is without moving where its
  # mail goes.
  local configured
  configured="$(teanode_cli api call UpdateDomain domainId="$(domain_id)" \
    domainParameters:='{"linkHost":"pictures.'"${DOMAIN}"'"}' --select "{ linkHost linkHostname }" 2>&1 || true)"
  # Matched on the field rather than on the name appearing anywhere, because
  # an error message quotes the arguments back: a refused update printed the
  # name it had refused, and a looser check read that as success.
  if grep -q "\"linkHost\": \"pictures.${DOMAIN}\"" <<<"${configured}"; then
    pass "the domain accepted a name of its own for pictures"
  else
    fail "the name was not accepted: $(tr -d '\n' <<<"${configured}")"
  fi

  # Read back rather than believed. A field that is applied in memory and
  # dropped on the way to the database is accepted, echoed and gone, with no
  # error anywhere.
  local storedBack
  storedBack="$(teanode_cli api call GetDomain domainId="$(domain_id)" --select "{ linkHost linkHostname }" 2>&1 || true)"
  if grep -q "\"linkHostname\": \"pictures.${DOMAIN}\"" <<<"${storedBack}"; then
    pass "and uses it: the name comes back on the next read"
  else
    fail "the name did not survive: $(tr -d '\n' <<<"${storedBack}")"
    printf '    rows: %s\n' "$(psql_value "select domain || ' ' || link_host from domain" | tr '\n' ' ')"
    printf '    columns: %s\n' "$(psql_value "select string_agg(column_name, ',') from information_schema.columns where table_name = 'domain'")"
  fi

  local elsewhere elsewhereBody
  elsewhere="picture-elsewhere-$(date +%s)"
  graphql "mutation { SendMail(domainId: \"$(domain_id)\", messageParameters: {
      from: \"noreply@${DOMAIN}\"
      to: [\"someone@${SENDER_DOMAIN}\"]
      subject: \"${elsewhere}\"
      htmlContent: \"<img src=\\\"/media/${mediaId}\\\" alt=\\\"a picture\\\">\"
      textContent: \"hello\"
    }) { mail { id } } }" -H "Authorization: Bearer ${TOKEN}" >/dev/null

  if await_message "${elsewhere}"; then
    elsewhereBody="$(fetch_message_html "${elsewhere}")"
    if grep -q "https://pictures\.${DOMAIN}/m/" <<<"${elsewhereBody}"; then
      pass "a domain can say which of its names serves the pictures"
    else
      fail "the configured name was not used: $(grep -io 'src="[^"]*"' <<<"${elsewhereBody}" | head -1)"
      printf '    the server says: %s\n' "$(teanode_cli api call GetDomain domainId="$(domain_id)" --select "{ linkHost linkHostname mailHosts }" 2>&1 | tr -d '\n')"
      printf '    the database says: %s\n' "$(teanode_server config show 2>/dev/null | grep -i linkhost | tr -d '\n')"
    fi
  else
    fail "the message naming another host never arrived"
  fi

  # Put back, so nothing after this depends on it.
  teanode_cli api call UpdateDomain domainId="$(domain_id)" \
    domainParameters:='{"linkHost":""}' >/dev/null 2>&1 || true

  # An address nobody minted resolves to nothing. These are reachable by
  # anybody, and one that could be guessed would let a stranger mark somebody
  # else's message opened.
  local guessed
  guessed="$(curl -sS -o /dev/null -w '%{http_code}' "${API}/m/aaaaaaaaaaaaaaaaaaaaaaaaaa")"
  if [[ "${guessed}" == "404" ]]; then
    pass "an address that was never minted answers nothing"
  else
    fail "a guessed address answered ${guessed}"
  fi
}

# --- upgrades -----------------------------------------------------------------

# This stack is a container, which is the deployment that cannot upgrade itself
# — the binary is on a read-only layer and the image is what needs replacing.
# What it must do is say so, with the reason, rather than offering a button
# that would swap a file and lose it at the next start.
check_upgrades() {
  step "Upgrades"

  local status
  status="$(teanode_cli api call GetUpgrade --select "{ current applicable reason automatic upgrading }" 2>&1 || true)"

  # This stack is a container, and a container used to be told it could not
  # upgrade itself at all. It can: the new binary goes on the volume, and the
  # next start runs that instead of the one in the image. What the volume is
  # for is the whole reason this assertion is here — an upgrade that wrote
  # into the image would report success and be undone by the next recreate.
  if grep -q '"applicable": true' <<<"${status}"; then
    pass "a container can upgrade itself, because the new binary goes on the volume"
  else
    fail "the container says it cannot upgrade: $(tr -d '\n' <<<"${status}")"
  fi
  if grep -qE '"reason": "[^"]+"' <<<"${status}"; then
    fail "it gave a reason for refusing something it is not refusing: $(tr -d '\n' <<<"${status}")"
  else
    pass "and there is no refusal to explain"
  fi
  if grep -q '"upgrading": false' <<<"${status}"; then
    pass "and nothing is being installed right now"
  else
    fail "it thinks an upgrade is in progress: $(tr -d '\n' <<<"${status}")"
  fi
  if grep -q '"automatic": false' <<<"${status}"; then
    pass "automatic upgrades are off unless somebody turns them on"
  else
    fail "automatic upgrades are on by default"
  fi

  # Saying it can is not cheap talk: answering that question probes the
  # volume, so a stack whose data directory were read-only would have said
  # otherwise above. The directory itself is deliberately not there yet — it
  # is made, private to this user, at the moment something is staged, because
  # asking whether an upgrade is possible should not leave anything behind on
  # a deployment that will never install one.
  if [ -e "${TEST_DIR}/data/upgrade" ]; then
    fail "asking about upgrades created ${TEST_DIR}/data/upgrade"
  else
    pass "and asking left nothing behind on the volume"
  fi
}

check_restart() {
  step "Restarting"

  "${COMPOSE[@]}" restart teanode >/dev/null
  local attempt
  for attempt in $(seq 1 60); do
    if graphql '{ GetSession { authenticationRequired } }' 2>/dev/null | grep -q authenticationRequired; then
      break
    fi
    sleep 1
  done

  check_contains "the server comes back up" '"authenticationRequired":true' \
    graphql '{ GetSession { authenticationRequired } }' 
  check_contains "the account survived the restart" "operator" \
    teanode_cli user list
  check_contains "the domain created through the API survived too" "second.test" \
    teanode_cli api call ListDomains --select "{ domain }"
  check_contains "the received message is still in the database" "parity-" \
    teanode_cli api call ListMails domainId="$(domain_id)" --select "{ id subject }"
}

# check_upgrade_seals_keys proves the upgrade path from a release that stored
# the signing keys in the clear.
#
# Reading tolerates an unsealed key and sealing happens on the way out, so the
# column converts on the next save — but a server that is merely running does
# not save, and a column documented as encrypted while sitting in plaintext is
# worse than one that was never encrypted, because it is believed. This writes
# a key in the old way, restarts, and requires the server to have fixed it
# without being asked.
#
# The key is generated here rather than taken from the server, because which
# key it is does not matter and a key made outside proves the other half: the
# public key the server derives after opening what it sealed has to be the one
# openssl derives from the same PEM. A seal that does not open, or opens to
# something else, signs nothing — and would say nothing.
check_upgrade_seals_keys() {
  step "Upgrading from a release that did not encrypt the keys"

  local pem expected
  pem="$(mktemp)"
  openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "${pem}" 2>/dev/null
  expected="$(openssl rsa -in "${pem}" -pubout -outform DER 2>/dev/null | base64 -w0)"
  if [[ -z "${expected}" ]]; then
    fail "could not generate a signing key to upgrade from"
    rm -f "${pem}"
    return
  fi

  # Dollar quoting, because a key is several lines of PEM.
  {
    printf "update domain set dkim_private_key = \$pem\$"
    cat "${pem}"
    printf "\$pem\$ where domain = '%s';\n" "${DOMAIN}"
  } | "${COMPOSE[@]}" exec -T postgres psql -U teanode -d teanode -q >/dev/null
  rm -f "${pem}"

  if [[ "$(psql_value "select left(dkim_private_key, 5) from domain where domain = '${DOMAIN}'")" != "-----" ]]; then
    fail "the plaintext key did not go in, so there is nothing to upgrade from"
    return
  fi
  pass "a key was stored the way a release without encryption stored it"

  "${COMPOSE[@]}" restart teanode >/dev/null
  local attempt
  for attempt in $(seq 1 60); do
    if graphql '{ GetSession { authenticationRequired } }' 2>/dev/null | grep -q authenticationRequired; then
      break
    fi
    sleep 1
  done

  local stored
  stored="$(psql_value "select dkim_private_key from domain where domain = '${DOMAIN}'")"
  if [[ "${stored}" == sealed:* ]]; then
    pass "the upgrade encrypted it on the way up, without being asked"
  else
    fail "the key is still stored as ${stored:0:16}"
    return
  fi

  check_contains "and it opens to the same key, so signing still works" "${expected:0:64}" \
    teanode_local dkim show "${DOMAIN}"
}

# check_relay proves the outgoing relay is used instead of an MX lookup, which
# is the whole point of it: an ISP that blocks outbound 25 leaves MX delivery
# with nowhere to go.
#
# The proof is a recipient whose domain has no MX at all. Delivering that by
# lookup cannot work; if it arrives, it went through the relay.
check_relay() {
  step "Relaying outgoing mail instead of looking up MX"

  local exported
  exported="$(mktemp)"
  teanode_server config export --file "${exported}" --force >/dev/null

  python3 - "${exported}" "${FORWARD_HOST}" <<'PYTHON'
import sys

path, relay_host = sys.argv[1:3]
lines = open(path).read().split("\n")

start = next(index for index, line in enumerate(lines) if line == "  relay:")
end = start + 1
while end < len(lines) and lines[end].startswith("    "):
    end += 1

block = [
    "  relay:",
    "    enabled: true",
    "    host: %s" % relay_host,
    "    port: 25",
    # The stand-in offers no STARTTLS and has no certificate worth checking,
    # which is exactly what "none" is for.
    "    security: none",
]
open(path, "w").write("\n".join(lines[:start] + block + lines[end:]))
PYTHON

  teanode_server config import --file "${exported}" --force >/dev/null
  rm -f "${exported}"
  pass "the relay was configured"

  # Read once at startup, like the listeners and the object store.
  "${COMPOSE[@]}" restart teanode >/dev/null
  local attempt
  for attempt in $(seq 1 60); do
    if graphql '{ GetSession { authenticationRequired } }' 2>/dev/null | grep -q authenticationRequired; then
      break
    fi
    sleep 1
  done

  check_contains "the server says it is relaying" "not delivered by MX lookup" \
    "${COMPOSE[@]}" logs --tail 200 teanode

  local subject="relayed-$(date +%s)"
  local created username password
  created="$(teanode_cli credential add "${DOMAIN}" --comment "relay harness" --json 2>/dev/null)" || {
    fail "could not create a credential for the relay test"
    return
  }
  username="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["username"])' <<<"${created}")"
  password="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["password"])' <<<"${created}")"

  # no-mx.test is in no zone file, so it has no MX and no address. Delivering
  # to it by lookup is impossible.
  if python3 - "${SUBMISSION_PORT}" "${username}" "${password}" "${DOMAIN}" "${subject}" <<'PYTHON'
import smtplib, ssl, sys
from email.message import EmailMessage

port, username, password, domain, subject = (
    int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5])

message = EmailMessage()
message["From"] = f"noreply@{domain}"
message["To"] = "someone@no-mx.test"
message["Subject"] = subject
message.set_content("Sent to a domain with no MX, so only a relay can deliver it.")

context = ssl.create_default_context()
context.check_hostname = False
context.verify_mode = ssl.CERT_NONE

with smtplib.SMTP("127.0.0.1", port, timeout=30) as smtp:
    smtp.starttls(context=context)
    smtp.login(username, password)
    smtp.send_message(message)
PYTHON
  then
    pass "the message was accepted for a domain that has no MX"
  else
    fail "the submission was refused"
    return
  fi

  if await_message "${subject}"; then
    pass "it arrived through the relay, which an MX lookup could not have done"
  else
    fail "the relayed message never arrived"
  fi
}

check_secrets_not_leaked() {
  step "Secrets"

  check_contains "config show redacts the secrets" "(redacted)" \
    teanode_server config show

  local shown
  shown="$(teanode_server config show 2>/dev/null)"
  # Assembled rather than written out, so that the secret scanner does not
  # flag this check for containing the thing it checks for.
  if grep -q "BEGIN .*PRIVATE KEY" <<<"${shown}"; then
    fail "config show printed a private key"
  else
    pass "config show prints no private key"
  fi

  # The secret every SMTP password is derived from. Printing it in a support
  # bundle would hand over every credential on the server.
  local secret
  secret="$(teanode_server config show --show-secrets 2>/dev/null | grep -oP '(?<=^  secret: ).*' | head -1)"
  if [[ -z "${secret}" ]]; then
    fail "the server never generated a secret"
  elif grep -qF "${secret}" <<<"${shown}"; then
    fail "config show printed the server secret"
  else
    pass "config show does not print the server secret"
  fi

  # And the row itself. A signing key is the one secret here that lives in a
  # table of its own, which is the shape that leaves in a partial dump — so it
  # is stored sealed, and this is what says so about a real database rather
  # than a round trip in a unit test.
  local stored
  stored="$("${COMPOSE[@]}" exec -T postgres psql -U teanode -d teanode -At \
    -c 'select dkim_private_key from domain' 2>/dev/null)"
  if [[ -z "${stored}" ]]; then
    fail "no domain has a signing key stored"
  elif grep -q "BEGIN .*PRIVATE KEY" <<<"${stored}"; then
    fail "the domain table holds the signing key as plaintext"
  elif ! grep -q '^sealed:' <<<"${stored}"; then
    fail "the stored signing key is neither sealed nor recognisable: ${stored:0:20}"
  else
    pass "the signing keys are encrypted in the database"
  fi
}

# check_object_store proves the raw message went where several instances can
# read it, rather than only onto the disk of the one that received it. That is
# the whole reason the object store is here.
check_object_store() {
  step "Object store"

  local listing
  listing="$(docker run --rm --network "${COMPOSE_NETWORK}" --entrypoint sh minio/mc:latest -c \
    "mc alias set local http://minio:9000 teanode teanodeteanode >/dev/null && mc ls --recursive local/teanode" 2>/dev/null || true)"

  if grep -q '\.eml' <<<"${listing}"; then
    pass "received messages are in the bucket, not only on local disk"
  else
    fail "nothing was stored in the object store"
  fi

  # The pictures too. They are served on every open of every message that
  # carries them, long after the disk they were uploaded to may have been
  # replaced, so a deployment with an object store has to be putting them
  # there and not only beside the process.
  if grep -q 'media/' <<<"${listing}"; then
    pass "an uploaded picture is in the bucket as well"
  else
    fail "the uploaded picture never reached the object store"
  fi

  # Not asserted here: that a rejected message keeps its content. Both
  # rejections this test makes happen before DATA — one at RCPT for a domain
  # the server does not serve, one before MAIL FROM for want of a credential
  # — so there is no content to keep. Storing a refusal that arrived with a
  # body is covered in internal/mx.
}

# --- run ----------------------------------------------------------------------

main() {
  build_environment
  start_database
  configure_deployment
  start_stack

  check_migrations
  check_dashboard
  check_onboarding
  issue_token
  check_cli
  check_incoming_mail
  check_rejections
  check_submission
  check_media
  check_upgrades
  check_restart
  check_upgrade_seals_keys
  check_relay
  check_object_store
  check_secrets_not_leaked

  printf '\n\033[1m%d passed, %d failed\033[0m\n' "${PASSED}" "${FAILED}"
  if (( FAILED > 0 )); then
    printf '\nThe last of the server log:\n'
    "${COMPOSE[@]}" logs teanode 2>&1 | tail -40
    return 1
  fi
  printf '\nThis deployment does what a deployment has to do.\n'
}

main "$@"
