# Photos are rows, and their bytes are blobs

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

People want their photos and videos kept where their mail, contacts and
calendar already are, found by their agent and put into albums. A library is
large: tens of thousands of items and hundreds of gigabytes, most of it
video. The server already keeps big things out of PostgreSQL: a message's
bytes, a recording's originals and a conversation's files live in the blob
store (`internal/storage`, a directory or an S3 bucket), and the database
holds what is known about them.

## Decision

Everything known about a photo is a row: when and where it was taken, its
kind (picture, video or Live Photo), its albums, its faces, its description,
who it is shared with. The bytes are blobs: the files the person uploaded,
which are never rewritten, and the smaller previews made from them, which
can always be made again.

A file's blob is named by the SHA-256 of its bytes. The same picture saved
twice, by one person or by two people in a family, is stored once. Each
person's quota still counts every file they own, so sharing never moves
bytes onto somebody else's quota.

An edit made on a phone arrives as a second file beside the original, never
in place of it. A Live Photo is one photo with two files.

Photos are a hosted service like the calendar and contacts: on unless an
operator switches them off, a person needs the `photos:use` permission, and
they work without the agent. The agent adds finding, describing and organizing.

## Consequences

Tables `photo`, `photo_file`, `photo_blob`, `photo_client_alias`,
`photo_upload`, `photo_upload_part` and `photo_preview`. A blob is deleted
only by the cleanup job, after no file refers to it, so two uploads of the
same bytes never race a deletion.

A blob's row is written as soon as its bytes are stored, in a transaction of
its own, so bytes whose file could not be recorded are an unreferenced blob.
The cleanup job deletes a blob once it has had no file for an hour
(`photo_blob.unreferenced_since`, kept by a trigger on `photo_file`, counts
from when the last file went, or from when the bytes were stored if none ever
named them). Deleting an account deletes the previews and upload parts its
rows named before the rows go; its blobs are deleted by the same cleanup.

The uploader names each photo (`client_photo_id`), and sending a name again
finds the photo. The same original bytes sent under a second name, such as a
picture kept from a conversation and later backed up by a phone, join the
photo that has them, and the second name is recorded as an alias of it.

A backup of the database alone does not back up the library; the bucket or
directory has to be backed up as well. Previews do not need to be.

The S3 store has to write large objects in parts: a single `PutObject` stops
at 5 GB, and a long 4K video is bigger.
