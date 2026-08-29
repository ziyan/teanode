#!/usr/bin/env bash
#
# Fail if a configuration field is not documented.
#
# The reference in docs/configuration.md is written by hand from the comments
# on the configuration structs, which means it can fall behind them. A field
# nobody documented is a field nobody can use, and the usual way that happens
# is that somebody adds one and does not know the page exists.
#
# Run it directly, via `make check-config-docs`, or from CI.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

readonly REFERENCE="docs/configuration.md"

# deprecated.go is excluded on purpose. The fields there are read so that an
# older file still loads; documenting them would be an invitation to write new
# ones.
readonly SOURCES=(internal/config/config.go internal/config/token.go)

missing=()
while IFS= read -r field; do
  [[ -z "${field}" ]] && continue
  # A field is documented either by its own entry, or — for one that only
  # holds a section — by the heading that names it.
  if grep -qF "**\`${field}\`**" "${REFERENCE}"; then
    continue
  fi
  if grep -qE "^### \`([a-zA-Z0-9.]+\.)?${field}(\[\])?\`$" "${REFERENCE}"; then
    continue
  fi
  missing+=("${field}")
done < <(grep -rhoE 'yaml:"[a-zA-Z0-9]+' "${SOURCES[@]}" | sed 's/yaml:"//' | sort -u)

if [[ "${#missing[@]}" -ne 0 ]]; then
  {
    echo "FAIL: configuration fields with no entry in ${REFERENCE}:"
    printf '  %s\n' "${missing[@]}"
    echo
    echo "Document each one, then the guard passes. The reference is generated"
    echo "from the doc comments on the structs, so writing the comment well is"
    echo "most of the work."
  } >&2
  exit 1
fi

echo "every configuration field is documented"
