#!/usr/bin/env bash
#
# Copy the data worth keeping out of a database written by an older release
# into one with the current schema.
#
# The schema changed shape rather than evolving: domains, aliases, credentials,
# users and tokens became configuration, and the foreign keys into them went
# with them. Migrations therefore restart at 0000 and cannot transform an old
# database in place, so the move is a copy into a fresh one.
#
# What is copied: mail, deliveries, DMARC reports, usage counters, templates
# and layouts. What is not: the configuration tables, which "teanode config
# import" turns into teanode.yaml instead.
#
#     scripts/migrate-legacy.bash <source-dsn> <target-dsn>
#
# Both arguments are psql connection strings. The target must already have the
# current schema, which it gets by starting teanode against it once.

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <source-dsn> <target-dsn>" >&2
  echo "example: $0 'postgres://teanode:teanode@127.0.0.1:25432/teanode' 'postgres://teanode:teanode@127.0.0.1:35432/teanode'" >&2
  exit 1
fi

readonly SOURCE="$1"
readonly TARGET="$2"

# Order matters: delivery and report reference mail, template references
# layout. Copying out of order trips a foreign key.
readonly TABLES=(
  layout
  template
  mail
  delivery
  report
  domain_usage
  alias_usage
  credential_usage
)

echo "checking the target has the current schema"
for table in "${TABLES[@]}"; do
  if ! psql "${TARGET}" -tAc "select to_regclass('public.${table}')" | grep -q "${table}"; then
    echo "ERROR: the target has no ${table} table; start teanode against it once to create the schema" >&2
    exit 1
  fi
done

for table in "${TABLES[@]}"; do
  source_count="$(psql "${SOURCE}" -tAc "select count(*) from \"${table}\"" 2>/dev/null || echo 0)"
  if [[ "${source_count}" == "0" ]]; then
    printf '%-18s %s\n' "${table}:" "nothing to copy"
    continue
  fi

  # Stream the rows straight across rather than through a file, so nothing
  # containing mail is left on disk.
  psql "${SOURCE}" -c "\\copy (select * from \"${table}\") to stdout" \
    | psql "${TARGET}" -c "\\copy \"${table}\" from stdin"

  target_count="$(psql "${TARGET}" -tAc "select count(*) from \"${table}\"")"
  if [[ "${source_count}" != "${target_count}" ]]; then
    echo "ERROR: ${table}: copied ${target_count} rows but the source has ${source_count}" >&2
    exit 1
  fi
  printf '%-18s %s\n' "${table}:" "${target_count} rows"
done

echo
echo "done. Now check the result:"
echo "  teanode config validate --config <your teanode.yaml>"
