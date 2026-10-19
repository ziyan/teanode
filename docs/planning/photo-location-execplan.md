# Changing where a photo was taken

This ExecPlan follows `~/.claude/PLAN.md`. It is a living document: update
Progress, Surprises and the Decision log as work goes on.

## Purpose

A person can correct a photo's date today, but not where it was taken. A
camera with no GPS leaves a trip's photos placed nowhere; a phone's GPS
sometimes places a photo in the wrong town; and some photos a person would
rather not have placed at all. After this change a person can, on the
dashboard, the iPhone, the command line and through the assistant:

- set where one photo or several chosen photos were taken, by picking a city
  from a search or by typing coordinates;
- remove a photo's location, which stays removed when the photo is read again.

The place name follows the coordinates, by the same bundled city table that
names every photo (`internal/place`), so a corrected photo is found by the
library's search for its city like any other.

## Scope

In: set and remove location for photos the caller owns, one or many (at most
500 at once, as Trash does); a city search over the bundled table; every
surface; docs.

Out: street addresses. The address search belongs to Location
(`SearchAddresses`), needs the Location service and an online place
provider, and the default provider (Nominatim) does not allow searching as a
person types. A photo's place name is a city, so a city search fits it.
Also out: rewriting the GPS inside the original file. Removing a location
removes it from TeaNode's record; a download of the original file still has
what the camera wrote.

## The contract every surface shares

GraphQL (reflection-registered like `UpdatePhoto` in
`internal/api/v1api/apigraph/photo.go`):

    SearchPhotoPlaces(query: String!, nearLatitude: Float, nearLongitude: Float, limitCount: Int): [PhotoPlace!]!
      PhotoPlace { placeName: String!, latitude: Float!, longitude: Float!, distanceKilometers: Float }
    SetPhotoLocation(photoIds: [String!]!, latitude: Float!, longitude: Float!): Int   (the count changed)
    RemovePhotoLocation(photoIds: [String!]!): Int                                     (the count changed)

- `SearchPhotoPlaces`: query trimmed, 1 to 100 characters; `limitCount` 10
  by default, at most 50. Matching ignores case and accents. Best match first:
  a whole-name match, then a name that starts with the words, then a word of
  the name that starts with them, then the words anywhere in "City,
  Country". Within one kind of match, nearest to the near point first when
  one is given (distanceKilometers set), otherwise the largest city first
  (distanceKilometers null). `placeName` is the table's "City, Country", the
  same text a photo's place name is. Text that reads as two numbers,
  "35.0116, 135.7681", answers one row at those coordinates, named by
  `place.Nearest` (or "35.0116, 135.7681" itself where no city is near).
  Needs `photos:use`.
- `SetPhotoLocation`: latitude -90..90, longitude -180..180, finite. Sets
  latitude, longitude and `place_name` (from `place.Nearest`, empty when no
  city is within reach) on each listed photo the caller owns that is not in
  the Trash; clears `is_location_removed`. Photos not the caller's, or
  trashed, are skipped (not an error), the way TrashPhotos counts.
- `RemovePhotoLocation`: sets latitude and longitude NULL, `place_name` '' and
  `is_location_removed` true.
- Both refresh the photos' search documents (`refreshPhotoSearchDocumentsOf`)
  and set `updated_at`.

Database: migration `0244_photo_location_removed` (after 0243, the person
relationship migration) adds `photo.is_location_removed boolean NOT NULL DEFAULT false`,
with its `.reverse.sql` and reverse test like its neighbours.
`UpdatePhotoMetadata` must not fill latitude, longitude or place name back in
from the file while `is_location_removed` is true (a Live Photo's video
arriving, or a retry, reads the file again). A photo whose location was set by
the person keeps it: the metadata only fills empty columns already.

Assistant (`internal/agent/tools/photo/photo.go`), new actions:
`search-places` (query, optional near latitude/longitude), `set-location`
(photo_ids, and either `place`, whose best match is used, or latitude and
longitude), `remove-location` (photo_ids). `set-location` is a write like
`set-date`; `remove-location` cannot be undone, so it is destructive and asks
every time.

CLI (`internal/cmd/photo.go`): `teanode photo search-places <words>`,
`teanode photo set-location <id>... --place <words> | --at <latitude>,<longitude>`,
`teanode photo remove-location <id>...`, each with `--json`. Client functions
in `internal/client/photo.go`.

Dashboard: in the viewer's details, a Change location button beside the
place (as Change date sits beside the date), and Remove location; in the
library's Select mode, Change location for the chosen photos. One dialog,
`ChangeLocationDialog` in `web/src/pages/photos/changeLocation.tsx`: a
search field that asks `SearchPhotoPlaces` 300 ms after typing stops,
nearest to the photo's current position (or the first chosen photo's), the
results as a list to pick from, and Remove location when the photo has one.
Strings in en, zh, ja.

iPhone: in the viewer's Info, Change location beside Change date, and in the
library's Select bar menu Change location for the chosen photos. A sheet with
`.searchable` asking `SearchPhotoPlaces`, a List of results, Remove location
(red, confirmed) when the photo has one. TeaNodeKit gets the three calls.
DemoServer answers them; a `photos-location` screenshot shows the sheet with
results. Strings in en, zh-Hans, ja.

## Progress

- [x] Plan written (2026-10-18).
- [x] Server: migration, database, place search, GraphQL, client, tool, CLI, tests.
- [x] Dashboard.
- [x] iPhone (Kit tested on Linux; the app compiles only on macOS CI).
- [x] Docs: `docs/subsystems/photos.md`, `docs/reference/command-line.md`.
- [x] Walk through in Chrome at 1400 and 390 wide, light and dark, on a dev server.
- [ ] Review until it converges (round one fixed); iPhone screenshots from CI.

## Surprises and discoveries

- A photo's description is written with its place name in the prompt, and the
  vector is read from the description and the place, so both can name a place
  the person removed. Moving a described photo clears both; it is described again when somebody
  looks at it or asks, as the photo-description decision says, and a
  description being written when the photo moves is dropped.
- A reader's saved copy of a shared photo is processed from the owner's
  original file, so it carries `is_location_removed` too.
- Two cities can share a name and a country; where no distance tells them
  apart, the dashboard and the iPhone show their coordinates.

## Decision log

- Cities from the bundled table, not addresses: works with no online
  provider and without the Location service, can be searched as a person
  types, and is what a photo's place name already is.
- A removed location is remembered (`is_location_removed`) so reading the
  file again does not bring it back.
