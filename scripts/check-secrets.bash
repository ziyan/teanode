#!/usr/bin/env bash
#
# Fail if anything that must never be published appears in a tracked file.
#
# The published history is a single fresh commit, so this guard is what keeps
# it clean afterwards. It scans tracked files only: whatever is gitignored is
# the operator's own runtime state and is not our business.
#
# There are two halves. The first looks for things that are secrets whatever
# they are called — keys, access ids, credentials. The second looks at every
# hostname in the tree and fails on any that is not on the allow list below,
# which is the half that catches an operator's own domain wandering into a
# comment or a test fixture. A deny list only catches the names somebody
# thought to add; an allow list catches the ones nobody thought about, which
# are the ones that leak.
#
# Run it directly, via `make check-secrets`, or from CI.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Paths that are allowed to mention the patterns below, because they are about
# them rather than containing them.
readonly EXCLUDES=(
  ':!vendor'
  ':!web/node_modules'
  ':!web/package-lock.json'
  ':!scripts/check-secrets.bash'
)

failed=0

# report <label> <pattern> [extra git-grep flags...]
report() {
  local label="$1" pattern="$2"
  shift 2

  local matches
  if matches="$(git grep -nIE "${pattern}" -- . "${EXCLUDES[@]}" "$@" 2>/dev/null)"; then
    echo "FAIL: ${label}" >&2
    echo "${matches}" | sed 's/^/  /' >&2
    echo >&2
    failed=1
  fi
}

# --- secrets, whatever they are called ---------------------------------------

# An AWS access key id. The literal prefix is split so this line does not
# match itself.
report "AWS access key id" "\b(AK""IA|AS""IA|AG""PA|AI""DA|AR""OA|AN""PA|ANV""A|AS""CA)[0-9A-Z]{16}\b"

# An AWS shared credentials file, wherever it is named.
report "AWS credentials" "aws_(secret_access|access_key)_[a-z]+[[:space:]]*="

# A Route53 hosted zone id: Z followed by 13 or more upper-case alphanumerics.
report "Route53 hosted zone id" "\bZ[0-9A-Z]{13,}\b"

# Any private key, whatever the algorithm.
report "private key" "BEGIN [A-Z ]*PRIVATE KEY"

# Somebody's home directory. A path like this says who wrote the line and how
# their machine is laid out, and it is never what a reader should copy.
report "machine path" "/(home|Users)/[a-zA-Z][a-zA-Z0-9_.-]*/"

# A personal address at a consumer mail provider. The providers themselves are
# allowed as hostnames below, because the SMTP defaults name them.
report "personal email address" "[a-zA-Z0-9._%+-]+@(gmail|googlemail|outlook|hotmail|yahoo|icloud|protonmail)\.[a-z]+"

# --- every hostname, checked against what is allowed -------------------------

# Hostnames the tree is allowed to contain. Anything else fails, including a
# domain nobody has thought of yet, which is the point.
#
# A leading dot means "this name and anything under it". Everything else is an
# exact match. Add to this list only after asking whether the name belongs in
# a published repository at all — the answer for an operator's own domain is
# no, and the reserved names below are what a comment or a fixture should use.
readonly ALLOWED_HOSTS=(
  # Reserved for documentation and testing: RFC 2606 and RFC 6761. These are
  # what every example, comment and fixture should use.
  .example.com
  .example.net
  .example.org
  .example.edu

  # Where the project lives, and what it is written in. The raw host is where
  # the deployment guide fetches the compose file from.
  .github.com
  raw.githubusercontent.com
  .golang.org
  .go.dev
  .gopkg.in
  .gorm.io
  # A module host, appearing only in go.mod and go.sum as the name of a
  # dependency. Vanity import paths are hostnames whether or not anything is
  # served there.
  .go.uber.org

  # Standards and references quoted in comments and documentation.
  .ietf.org
  .rfc-editor.org
  .wikipedia.org
  .openspf.org
  .spamhaus.org
  .semver.org
  .keepachangelog.com
  .unicode.org
  .w3.org

  # A program inside the ClamAV image, named by the compose health check. It
  # ends in a real TLD but is a file on a container, not a host.
  clamdcheck.sh

  # Services the code actually talks to.
  .letsencrypt.org
  .ipify.org
  icanhazip.com

  # Container registries named in the release workflow.
  .ghcr.io
  .gcr.io
  .docker.com
  .docker.io

  # The SMTP endpoints of the providers the outgoing relay has presets for.
  # Public, documented, and the whole point of the presets: an operator picks
  # one and the host is filled in. Nothing here identifies a deployment.
  smtp.gmail.com
  .amazonaws.com
  smtp.postmarkapp.com
  smtp.resend.com

  # Named by the SMTP defaults as senders worth trusting, and in the comments
  # explaining why. These are hostnames here, not anybody's address — an
  # address at one of them is caught by the rule above.
  google.com
  support.google.com

  # Google's public mail exchanger, named in the getting-started guide as the
  # host to open a connection to when checking whether outbound port 25 is
  # blocked. Anybody can connect to it; that is the point of the check.
  gmail-smtp-in.l.google.com
  outlook.com
  yahoo.com

  # Not a host anybody visits: the SPF macro test checks that "+a:o7-%{o7}-o7"
  # expands with the sender domain in the middle, so the name the resolver is
  # asked for is literally this. It has to match what expansion produces.
  o7-example.com

  # RFC 3464 appendix D, quoted verbatim in the delivery-report fixtures so
  # that what is parsed in the tests is what the specification describes.
  cs.utk.edu
  larry.slip.umd.edu
  sdcc13.ucsd.edu
  sun2.nsfnet-relay.ac.uk
  .vnet.ibm.com
  hpnjld.njd.hp.com
  hpnjld.njd.jp.com
  de-montfort.ac.uk
)

# Top-level domains worth looking for. Restricted rather than open-ended so a
# Go selector like `strings.Contains` is not mistaken for a hostname, and
# matched in lower case only for the same reason: a real TLD is never
# capitalised, and an exported Go method almost always is.
# `.email` and `.store` are real top-level domains and are deliberately absent:
# `alias.email` and `self.store` are field accesses, they occur far more often
# than either TLD, and nobody here owns one.
readonly TLDS='com|net|org|edu|gov|mil|int|io|dev|app|sh|fm|so|me|tv|cc|co|info|biz|xyz|cloud|life|love|site|online|tech|blog|news|wiki|uk|us|ca|de|fr|jp|cn|au|nl|se|ch|it|es|ru|br|in|kr|nz|pl|tr'

# Names reserved by RFC 6761 for testing. A hostname under any of them is
# always fine, whatever is in front of it.
readonly RESERVED_TLDS='test|invalid|localhost|example'

# Filenames in this repository, so that `release.sh` is read as the script it
# is rather than as a domain under a TLD somebody really does sell. Asking the
# repository rather than keeping a list of extensions matters: `.sh` and `.dev`
# are both real TLDs, and excluding them wholesale would blind this check to a
# domain that happens to end in one.
# Each filename is recorded along with every prefix of it that ends at a dot,
# so that `docker-compose.dev` is recognised from `docker-compose.dev.yml`.
declare -A REPOSITORY_FILES=()
while IFS= read -r path; do
  name="${path##*/}"
  while [[ "${name}" == *.* ]]; do
    REPOSITORY_FILES["${name}"]=1
    name="${name%.*}"
  done
done < <(git ls-files)

is_repository_file() {
  [[ -n "${REPOSITORY_FILES[$1]:-}" ]]
}

host_allowed() {
  local host="$1" allowed
  for allowed in "${ALLOWED_HOSTS[@]}"; do
    if [[ "${allowed}" == .* ]]; then
      # A leading dot allows the name itself and anything under it.
      [[ "${host}" == "${allowed#.}" || "${host}" == *"${allowed}" ]] && return 0
    else
      [[ "${host}" == "${allowed}" ]] && return 0
    fi
  done
  return 1
}

# Every hostname in the tree, with where it was found.
found="$(git grep -nIoE "\b([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+(${TLDS}|${RESERVED_TLDS})\b" \
  -- . "${EXCLUDES[@]}" 2>/dev/null || true)"

unexpected=""
while IFS= read -r line; do
  [[ -z "${line}" ]] && continue

  # git grep -o prints path:line:match.
  host="${line##*:}"

  # A name under a reserved TLD is always allowed.
  [[ "${host}" =~ \.(${RESERVED_TLDS})$ ]] && continue

  # A filename is not a hostname.
  is_repository_file "${host}" && continue

  host_allowed "${host}" && continue

  unexpected+="${line}"$'\n'
done <<<"${found}"

if [[ -n "${unexpected}" ]]; then
  echo "FAIL: hostname that is not on the allow list" >&2
  echo "${unexpected}" | sed '/^$/d; s/^/  /' >&2
  echo >&2
  failed=1
fi

if [[ "${failed}" -ne 0 ]]; then
  cat >&2 <<'EOF'
One or more secrets or private references were found in tracked files.

For a secret, remove it. For a hostname, rewrite it to one of the reserved
documentation names — example.com, example.net, or anything under .test,
.example, .invalid or .localhost — which is what comments, fixtures and
documentation should use.

If a hostname genuinely belongs in a published repository, add it to
ALLOWED_HOSTS in scripts/check-secrets.bash with a comment saying why. A
leading dot allows a name and everything under it.
EOF
  exit 1
fi

echo "no secrets found in tracked files"
