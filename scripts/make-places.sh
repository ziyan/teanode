#!/usr/bin/env bash
# Rebuilds internal/place/places.tsv.gz, the table of cities the server names a
# photo's place from, out of GeoNames' list of cities of 15,000 people or more.
# Run it when you want newer data; the result is committed.
#
# GeoNames data is licensed CC BY 4.0 (https://www.geonames.org); the credit
# is in the documentation of package place.
#
# Each line of the table is: name, country name, latitude, longitude, with
# the coordinates rounded to 4 decimals (about 10 meters).
set -euo pipefail

base_url="https://download.geonames.org/export/dump"
output="$(cd "$(dirname "$0")/.." && pwd)/internal/place/places.tsv.gz"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

curl --fail --silent --show-error --location --output "${work}/cities15000.zip" "${base_url}/cities15000.zip"
curl --fail --silent --show-error --location --output "${work}/countryInfo.txt" "${base_url}/countryInfo.txt"
unzip -q -o "${work}/cities15000.zip" cities15000.txt -d "${work}"

# cities15000.txt: tab separated; column 2 is the name, 5 and 6 the latitude
# and longitude, 9 the ISO country code. countryInfo.txt: comment lines start
# with #; column 1 is the ISO code and column 5 the country name.
awk -F'\t' -v OFS='\t' '
	FILENAME ~ /countryInfo/ { if ($0 !~ /^#/ && NF >= 5) country[$1] = $5; next }
	{
		name = $2; code = $9
		if (name == "" || !(code in country)) next
		printf "%s\t%s\t%.4f\t%.4f\n", name, country[code], $5, $6
	}
' "${work}/countryInfo.txt" "${work}/cities15000.txt" | LC_ALL=C sort -u | gzip -9 -n > "${output}"

echo "wrote ${output}: $(zcat "${output}" | wc -l) places, $(stat -c %s "${output}") bytes"
