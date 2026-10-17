# An album is shared at a level, and a family album belongs to the circle

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

People in a circle want to see each other's photos of the same holiday, and
a family wants one album everybody adds to. Circles
(`20261016-people-on-one-server-meet-in-circles.md`) already decide who
somebody may share with, and calendars
(`20261016-a-calendar-is-shared-at-a-level.md`) already share at a level.

A photo carries where it was taken, to a few meters, in its file.

## Decision

A person shares an album with a circle or with one person in a circle they
share, at a **share level**: `view`, to see and save its photos, or
`contribute`, to also add photos of their own and take out the ones they
added. Only the owner renames, deletes or reshares it.

A circle may have **circle albums**, owned by the circle rather than by a
person. Every member views and contributes; admins rename and delete them.

A photo added to somebody else's album stays the photo of the person who
added it, on their quota. When they leave the circle the album was shared
through, their photos leave its albums in the same transaction. A reader who
wants to keep one saves it to their own library, which costs no bytes, since
the blob is already stored.

Readers do not see where a photo was taken, and get the large preview, with
no location in it, rather than the original file, unless the owner included
the location when sharing.

The reader's agent reads shared albums where they are, through the owner's
rows, and copies nothing into the reader's library.

## Consequences

One table, `photo_album_share`, with `grantee_user_id` or
`grantee_circle_id` and `share_level`, and `photo_album.circle_id` beside
`photo_album.user_id`, exactly one of which is set. The places that list "the
person's albums" add the ones shared with them, each marked with its owner
and level.

A reader keeps a photo by saving it. Where the share includes where the
photos were taken, the saved photo is made over the same blob and stores
nothing. Otherwise it is the large preview the reader could see, stored as a
file of their own, because the original's bytes carry the place and a copy of
them would hand it over.

A circle's album is not shared further. A person who does not reach an album
has no photos in it: taking a share back, or leaving the circle it depended
on, takes their photos out of the albums they no longer reach, in the same
transaction.
