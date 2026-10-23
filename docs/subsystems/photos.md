# Photos

A person here can keep their photos and videos beside their mail, contacts and
calendar. An iPhone backs its library up, the dashboard and the command line
upload and browse, the agent finds, looks at, describes and organizes, and
faces are grouped into people named from the address book. This says how
those fit together and which choices are load-bearing. The reasons are in the
decision records dated 2026-10-16 whose names start with `photos-`,
`uploads-`, `the-iphone-`, `a-photo-`, `faces-` and `an-album-`.

## The pieces

`internal/photo` is the library: the resumable upload endpoint, completing an
upload into a photo, the processing job, faces and their groups, album
suggestions, signed playback addresses and cleanup. It knows the database and
the blob store and nothing about HTTP routes or GraphQL.

`internal/picture` reads pictures and videos: decoding JPEG, PNG, GIF and WebP
upright (WebP through the Go team's `golang.org/x/image/webp`),
scaling, EXIF from JPEG and HEIC, HEIC through libheif's `heif-dec`, and video
through `ffprobe` and `ffmpeg`. `internal/place` names the city a photo was
taken in from a table bundled with the server. `internal/faces` runs
`teanode-faces`, the C++ face reader in `tools/faces/`. Every program they run
(`ffmpeg`, `ffprobe`, `heif-dec`, `teanode-faces`) is given a minimal
environment, `PATH`, `LANG` and a `HOME` that is a private directory of ours
(`picture.ProgramEnvironment`), and not the server's: it holds the server's
secret and database address, and the programs open files nobody has checked.

`internal/db/database_photo*.go` keeps the rows, migrations 0220 to 0224 make
them, and `internal/api/v1api/apigraph/photo*.go` is what every client talks
to. The dashboard is `web/src/pages/photos/` and `web/src/photos/`, the
command line `internal/cmd/photo*.go`, the agent's tool
`internal/agent/tools/photo/`, and the iPhone `ios/TeaNode/Sources/Photo*.swift`
with `ios/TeaNodeKit/Sources/TeaNodeKit/Photo*.swift`.

Photos are a hosted service: `services.photos` is on unless an operator turns
it off, on an assistant-only server too, and a person needs `photos:use`,
which the built-in roles have. They work without the agent; the
agent adds describing and organizing.

## Rows, and the bytes beside them

Everything known about a photo is a row; the bytes are blobs in the store
(`internal/storage`, a directory or an S3 bucket).

- `photo` is one item of the library: a picture, a video, or a Live Photo.
  It holds when and where it was taken (the instant, and the offset of the
  clock where it was taken, so a trip's photos keep the trip's dates), the
  place's name, the camera, its size, favorite, trash, the state of its
  processing, and its description.
- `photo_file` is one file of a photo by role: `original`, `edited` (an edit
  made on a phone, beside the original, never in place of it) and
  `liveVideo`. It names its blob by SHA-256.
- `photo_blob` is the bytes of a file, named by their SHA-256, so the same
  picture uploaded twice, or by two people, is stored once. Each person's
  quota still counts every file they own.
- `photo_preview` is a smaller copy made for showing: `small` (400 pixels on
  the long side), `large` (2048) and, for a video or a Live Photo (from its
  video, which an iPhone records as HEVC that many browsers cannot play),
  `video` (H.264, 1280). Previews can always be made again; nothing depends
  on them being kept.
- `photo_client_alias` is another name an uploader has for a photo that is
  there already (see Uploads).
- `photo_upload` and `photo_upload_part` are a file on its way in.
- `photo_album` and `photo_album_item`; `photo_album_share` (who an album is
  shared with); `photo_vector` (a description's embedding); `photo_face` and
  `photo_face_group`.

A backup of the database is not a backup of the library: the blob store has
to be backed up too.

## Uploads

Every client uploads through one endpoint, `/api/v1/photos/uploads`, which
speaks the IETF resumable upload draft, so that any client that knows the
draft can continue an upload and any other can send a file in one `POST`
(the iPhone does not use the system's resumable upload support: it makes its own
`POST`, `HEAD` and `PATCH`, described under the iPhone's backup): `OPTIONS` says the largest file allowed, `POST` creates an upload
and answers an interim `104` with its address before reading the body,
`HEAD` says how many bytes are stored (and is never cached:
`Cache-Control: no-store`), `PATCH` appends from there. `OPTIONS`
writes the limit as `max-size=<bytes>` for a client that names revision 5 or
later of the draft in `Upload-Draft-Interop-Version`, and as the bare number
for one that names an earlier revision or none. What the
file is (the uploader's own name for the photo, which file of it, when it was
taken) travels in `X-Photo-*` headers, since a background upload's body is
nothing but the file. Only `X-Photo-Client-ID` is required. When the request
names its content type, a type the library does not keep is refused with a 415
before a byte is read; a request that names none, or `application/octet-stream`,
is judged by its file name and its bytes once they have all arrived. A
picture whose first bytes are a JPEG, PNG, GIF, WebP or HEIC is kept as that type
whatever it was sent as, because an iPhone sends a picture saved from a
message as `image.jpeg` in whichever format it is; processing reads those
bytes again before it picks a decoder, for files kept before this check.

A client that cannot see an answer before its request ends, as a background
upload on a phone cannot, makes the upload first: a `POST` with
`Upload-Complete: ?0`, `Upload-Length` and no body answers 201 with the
upload's address and `Upload-Offset: 0`, and the bytes follow in a `PATCH` at
the offset `HEAD` reports, which ends with `Upload-Complete: ?1`. The `X-Photo-*`
headers and the declared digest belong to the `POST`; the `PATCH` does not
repeat them, and a `PATCH` whose bytes do not hash to the digest ends the upload
(`internal/photo/upload_test.go` has the cases). A header that says the end of
the file twice counts as saying it once when every item agrees: iOS 17 and later
may add resumable upload headers of its own to a background upload, so
`Upload-Complete` can arrive as `?1, ?1` or on two lines. Items that disagree
(`?1, ?0`) are refused with a 400.

The body is stored in parts of at most 8 MiB as it arrives, each with a
receipt row once it is whole, so the offset a client is told is always bytes
that are durable and a dropped connection costs at most the part in flight.
Completing reads the parts back, checks them against their receipts and the
declared digest, stores the file under its SHA-256 (in parts on S3 for files
over 256 MiB), and records it; the blob's row is committed right after its
bytes, so bytes whose recording then fails are reclaimed by cleanup. In the same
transaction that records the file, after it, one byte of the blob is read back
to see it is in the store; that transaction holds the blob's advisory lock
(`LockPhotoBlob`, a Postgres lock on the digest) from before it clears the
blob's mark until it commits, and cleanup deletes the object under the same
lock, so cleanup cannot delete bytes a file has just claimed. If the object is
gone the step is done again with the bytes stored. The quota counts
what is stored and what is still arriving, where "arriving" is the uploads a
part arrived in during the last day (`openUploadQuotaWindow`): an upload that
sat idle for longer does not hold room for the week cleanup keeps it, and a
second check bounds what those idle uploads add up to: the account's other
unfinished uploads of any age may not hold more than its limit. A refusal that
the account's other uploads cause, as opposed to this file's own size, answers
503 with a `Retry-After` (seconds until the oldest of them stops counting, at
least 60 and at most 3600) and leaves the upload resumable
(`QuotaError.IsCausedByOtherUploads`), because every client takes that as "try
again"; a refusal its own bytes cause answers 413 and ends it. A request that
names the digest it will have (`X-Photo-Declared-SHA256`) of bytes the person
already has as the original of a photo, and sends no more than their size, is
not asked for room at all: it costs none.

A client whose last part got no answer asks where the upload is (`HEAD`). When
every byte is there and the upload is not complete, either the server is still
completing it or its attempt failed and nothing will try again, and an append
with no bytes, `Upload-Offset` at the file's end and `Upload-Complete: ?1`,
completes it (the client sends it on every poll); a server that is busy with
the upload answers 409 and the client waits.

The same picture can reach the library under two names: kept from a
conversation, which names it by its digest, and then backed up by a phone
under its own. When the first file of an upload (role `original`) completes
under a name the person has no photo for, and the person already has a photo, in
the library or the Trash, whose `original` has the same SHA-256, the upload is
recorded against that photo, which costs no room, and the name goes into
`photo_client_alias` so that `FindPhotos` finds the photo by either name (with
the name that was asked as its `clientPhotoId`). The photo's own name does not
change. A name is an alias only because its original matched: a file of another
role (an edit, a Live Photo's video) arriving under an alias makes a photo of its
own for that name, over the same original blob (a `photo_file` of its own, which
counts against the quota), and the alias row is deleted; two assets with the same
original and different edits end as two photos.

A phone signs these requests with a device password that grants photos and
nothing else (`sync_credential.has_photos_access`), because a background
upload may run days after it was queued. Other clients use their session.

A taken-at that ends in `Z` is an instant without an offset (all an iPhone's
library knows); the offset then comes from the file, or from the owner's time
zone. An explicit offset is kept as given.

## Processing

A periodic job, `photo:process`, runs two workers that claim one waiting
photo at a time (`FOR UPDATE SKIP LOCKED`, with a claim that expires, so
several instances share the work). For each it reads the metadata (EXIF for
pictures, `ffprobe` for videos; values the uploader sent win), names the
place, makes the previews, finds faces, and marks the photo ready. HEIC goes
through `heif-dec`, because ffmpeg 7.1 decodes only one tile of an iPhone
photo. Decoded pixels in flight are bounded, a panic is a failure of that
photo rather than of the server, and a failure waits longer each time (1, 5,
15, 60 minutes) up to five attempts; a store or database failure does not
count against the photo, and waits 1, 5, 15 and then 60 minutes by how many
such failures there were in a row (`processing_outage_count`), so a missing
program or a full disk is not retried every minute.
`RetryPhotoProcessing` starts a failed photo again, and makes the thumbnails of a ready one again to replace thumbnails that look wrong: the dashboard's and the iPhone's "Make thumbnails again", `teanode photo retry` and the agent tool's `retry`.

A claim is known by the time it was made (`processing_claimed_at`, which each
renewal moves), and what a worker records at the end, ready or failed, is
recorded only while the photo is still claimed at that time. If the claim was
lost, because it ran out and another instance took the photo or because the
photo's file was replaced, nothing is recorded; when the photo's file is a
different one from the file the worker read, the preview rows the worker wrote
are deleted, since they are of a file the photo no longer has, but only the rows
that are still the ones it made (by the time they were made): preview objects
are named by photo and size alone, so the work that overtook it may have written
over them, and the objects are left to be written over or deleted with the
photo. A renewal moves the claim to a time the worker chooses, so a renewal
whose answer was lost is settled by reading the claim: if it is at that time, it
was made and is still this worker's. A worker that is
stopped (the server shuts down) gives the attempt back and clears the claim,
in a transaction that does not end with the request, so the next run takes the
photo at once and a restart does not cost it an attempt or ten minutes.

A HEIC says how large its pictures are in its boxes (`ispe` of each item),
which `picture.ReadHEIFSize` reads before `heif-dec` is run, since `heif-dec`
decodes every top-level picture and not only the primary one: each is bounded
(64 megapixels, 65535 on a side), all together may hold twice that, and a
picture that does not say its size is refused. Thumbnails, auxiliary pictures
and the tiles of a grid (`thmb`, `auxl`, `dimg`) are not top-level. The pixels
of all of them are taken from the same budget decoded JPEGs use for as long as
`heif-dec` runs. A PNG, GIF or WebP with a side over
65535 pixels is refused from its header too: a strip a billion pixels wide is
under the pixel limit and would still make the scaler allocate a row of
gigabytes. The size a JPEG's EXIF block gives is swapped for an orientation of
5 to 8; a HEIC's is not, since `heif-dec` applies its own rotation.

A file is copied to the work directory (a video, or a picture too large to
decode whose date is read) only if the disk has room for twice its size and a
reserve of 1 GiB after the room promised to the copies already being worked on
(`workSpace`). A video is not claimed at all while `ffmpeg` or `ffprobe` is
missing (the claim excludes videos, and the programs are looked for again every
five minutes), and the programs are looked for before the file is copied, so a
server without them does not copy every video once a minute.

A date a file states for itself is believed only if it is from 1990 on and not
more than a day ahead of now (`picture.isPlausibleDate`), for EXIF and for a
video's tags alike: a camera whose clock was never set writes the year it
starts at.

## Where a photo was taken

A person can set where one photo or several were taken, or remove it, for a
camera with no GPS or a phone that placed a photo in the wrong
town. `SearchPhotoPlaces(query, nearLatitude, nearLongitude, limitCount)`
searches the same bundled table that names every photo (`internal/place`),
never an online geocoder, so it works with no provider and can be asked as a
person types. It ignores case, accents and punctuation, and answers best match
first: a whole name, then a name that starts with the words, then a name with a
word that starts with them, then the words anywhere in "City, Country".
Within one kind of match the city nearest to the near point comes first when
one is given (and `distanceKilometers` is set), otherwise the largest. The query is
1 to 100 characters and `limitCount` is 10 by default, at most 50. Text that is
two numbers, `35.0116, 135.7681`, answers one row at that position, named by
`place.Nearest`, or by the numbers where no city is near. Street addresses are
not searched here; that belongs to Location, which needs its service and an
online provider.

`SetPhotoLocation(photoIds, latitude, longitude)` sets latitude, longitude and
`place_name` (from `place.Nearest`, empty where no city is within reach) of
each listed photo the caller owns that is not in the Trash. `RemovePhotoLocation(photoIds)`
makes latitude and longitude null and the place name empty, and sets
`photo.is_location_removed`. Both take up to 500 photos, say how many photos
changed (photos that are not the caller's, or are in the Trash, are skipped, as
`TrashPhotos` counts), set `updated_at` and refresh the photos' search
documents, so the library's search finds a corrected photo by its new city.

A photo that had a description loses it, with the vector read from it: the
description was written knowing the old place and may name it, which a removed
location must not leave behind. It is not asked for again on its own, since a
photo is described only when somebody looks at it or asks; the next
description is written with the new place. A `describe_photo` job that was
looking at the photo when it moved keeps nothing and ends its request: it
holds the photo's row (`LockPhoto`) while it compares the place it was told
with the place the photo has now, so a move cannot fall between the two. A
file being read when the photo moved names the position it read
(`PhotoMetadata.PlaceNameLatitude` and `PlaceNameLongitude`), and that name is
not put next to a position the photo no longer has. A copy a reader saved from
a shared album keeps the owner's `is_location_removed`, since it is made from
the owner's original file.

Removing is for good: TeaNode keeps no copy of the old position, so every
surface asks first (the dashboard's dialog, the iPhone's question, the
command line's `--force`, the assistant's card). The original file is not
changed, so a copy already shared, saved by a reader or downloaded keeps
what the camera wrote, and every question says so.

`is_location_removed` is why a removed location stays removed: `UpdatePhotoMetadata`
reads the file again when a Live Photo's video arrives or a retry runs, and
while the flag is set it fills in no latitude, longitude or place name. A location
the person set is kept by the same rule that keeps any column the photo already has.
Setting a location clears the flag. Neither operation changes the file: a
download of the original still has what the camera wrote.

The cities come with the server, so searching, naming and changing a
location need no map service. A photo's details look further when they are
opened: `ReadPhotoPlace(photoId)` answers the person's own Location place the
position is inside (`locationPlace`, from `FindPlaceAt`, only for a caller with
Location) and otherwise, behind the place provider setting, the street address
the provider gives for the position (`addressText`). It is null for a photo
with no position, a removed location, or a position the caller may not see (the
rule that clears a shared photo's coordinates), and then the provider is not
asked. A provider that is off, or fails, gives an empty `addressText`, never an
error. The address is remembered in memory (`place.RememberedAddresses`) by
the position rounded to 4 decimals, about ten meters, for a day, so a moved
photo never shows the old street and opening a photo again does not ask the
provider again.

## Finding photos

`ListPhotos` filters by date, kind, favorites, the trash, an album, a person
or a contact, and a point and a distance, and searches: full-text over the
place, file name, camera, album titles, the names of the people in it, the
description and the text in the picture, and, when an embedding model
is configured, by meaning over the descriptions. The library is listed newest
taken first; the trash is listed newest moved to the trash first (`trashed_at`,
then id), the order its groups are read in, and a search by how well it
matches. A search with words embeds
them first, in a transaction of its own that is over before the model is asked
(`ListPhotos` and `CountPhotos` are in `isModelBackedQuery`, and read in phases
as recall does), so a slow model does not hold a database connection.

A photo is described only when somebody works with it: the agent looks at it,
or a person asks in the viewer. The agent's `describe_photo` job asks the
scan model for a few plain sentences and the words it can read, and keeps
them, so a search finds the photo by them from then on. Before that a photo
is found only by what the server reads without a model; the agent is told so
and narrows by date or place first.

## Asking the agent about a photo

A photo can be attached to a turn as a reference, as a recording or a finance
transaction is: `AgentReference.photoId`, with `photoTitle` for the chip. The
server reads the photo with the asking agent's owner when it keeps the turn,
so a photo the person may not read (one that is not theirs and is in no album
that reaches them), or one that is deleted, is refused by `AskAgent` (not
found) and dropped by any other caller; a photo in the Trash is kept and said
to be there. The chip is rewritten from the stored row, whatever the client
sent: the place and the day it was taken on the clock where it was taken,
separated by a middle dot, or the file name, or "Photo". The model is told the
photo's id, kind, time, whether it is in the Trash, and the place and file name
fenced as data, and that it can `look` at it with the photo tool. A photo of
somebody else's album is told without its place unless the share includes
places. Nothing from the description is copied in. The same photo twice is told once.

## Faces

`teanode-faces` reads a picture as raw pixels on standard input and prints the
faces it found, each with a box and a vector of 128 numbers; it is a separate
program so the server stays pure Go, and it reads raw pixels so OpenCV's image
decoders stay out of the image. The server runs it on the large preview. A
face joins the person's face group whose vector is closest, if similar
enough, under a per-person lock; otherwise it starts a group. A group is
nobody until the person names it by choosing a contact, or making one; the
agent never names a face by guessing. A group nobody has named is listed as a
person once it has at least three faces
(`MinimumUnnamedPhotoFaceGroupFaceCount`); a pair is more often a chance
likeness. A named group is always listed. When the contact that names a group
is the person's own card (`users.contact_id`, the card the agent knows as
them), the group is them: `PhotoFaceGroup.isOwnContact` and
`PhotoFace.isOwnContact` say so, the agent's tool rows carry `is_you`, the
command line writes "(you)" after the name, and the iPhone shows "You".
It is set per request from the caller's own user row, and is false for an
unnamed group and for a face in somebody else's shared photo, whose names are
the owner's. A contact names one person. Naming a group
with a contact that already names another of the person's groups merges the two
inside `NamePhotoFaceGroup`: the group already named stays, with the faces of
both and its own cover, and the person is hidden only if both were. A group never holds two faces of one photo, so when two groups each had a different face of one photo, the face the person confirmed stays, else the one whose vector is most like the merged group's sum (cosine), and the other is in no group and not confirmed (the face is still in the photo, but no pass groups it again until the photo's faces are found again); this holds for `person-merge` and the merge button as well. The answer
carries the group left, `mergedPhotoFaceGroupId` (the one that went into it;
the tool says `merged_into`, the command line "merged with") and
`isContactPictureSet`. Groups that were named with one contact before this was
so were merged once by migration 0242. When the contact has no picture it is
given one: the face of the group with the highest detection confidence among
those whose box covers at least `MinimumContactPictureFaceAreaFraction` (3
percent) of their photo, in the person's own photos out of the Trash, cut square
from the large preview with 40 percent of room on each side, at most 512 pixels,
as a JPEG set as the card's `PHOTO` (so CardDAV clients see it; `contact_picture_set`
in the tool, "used a face as their picture" on the command line). A picture a
contact has is never replaced, and a photo that cannot be read leaves the naming
done and the contact as it was. `RemoveContactPicture` takes a picture off; it is
the toast's Remove and is on every surface of the address book. The agent's photo
tool can take a name off a
group (`person-unname`), hide a group or list it again (`person-hide`, with
`is_hidden`; `people` with `is_hidden_included` finds the hidden ones), say one
face is not that person (`person-remove-face`, with the `photo_face_id` that
`read` gives), and choose an album's cover (`album-cover`); each is a write
that does not ask first, since the person can undo it. Faces are on for
everybody by default; switching them off deletes every face and group the
person has.

A contact that is a person in the owner's circles (`contact.account_user_id`, see
`docs/subsystems/contacts.md`) makes the group say who: `PhotoFaceGroup.accountUserId`
and `account` are set from the owner's own contacts, only while they still share a
circle, and are null otherwise; the agent's `people` rows carry `person` (the username).
`ListPhotos`, `CountPhotos` and `CountUndescribedPhotos` take `accountUserId`, which
is resolved through the caller's contacts only (`FindContactByAccount`, then the
contact in its own address book) and never reaches a group the caller did not name:
an account with no link, or one the caller no longer shares a circle with, lists
nothing rather than failing. The photo tool's `list` and `person-photos` take
`person` (a username), and `teanode photo list --person` the same. Nothing is shared with
the linked person.

## Sharing

An album belongs to a person (`photo_album.user_id`) or to a circle
(`photo_album.circle_id`), exactly one of the two. A person shares an album
with one person they share a circle with, or with a circle they are in, at a
**share level** (`view`, to see and save its photos, or `contribute`, to also
add photos of their own and take out the ones they added), and says whether the
share includes where the photos were taken (`is_location_included`) and the
names given to the faces in them (`is_face_names_included`). A circle's
album is read and added to by every member and managed (renamed, covered,
deleted) by the circle's admins; it is not shared further, since every member
has it. A photo in somebody else's album is still the photo of whoever added
it (`photo_album_item.added_by_user_id`), on their space, in their library.

Who may do what is one answer, asked of the database: `GetPhotoAlbumAccess`
(owner, member of the owning circle, direct share, share to a circle both the
reader and the owner are in) and `GetPhotoAccess` (the owner, or any album that
holds the photo and reaches the reader; the trash is the owner's alone). The
dashboard, the phone, the command line and the agent all go through
`requirePhotoReader`, `requirePhotoAlbumReader`, `requirePhotoAlbumContributor`
and `requirePhotoAlbumManager` in `apigraph/photo_authorize.go`, and every
refusal is "not found". A shared photo is read through its owner's rows: its
files, previews and faces are fetched with the owner's identifier once the
caller was let in. A photo whose owner is disabled or has lost `photos:use` is
not open to anybody else, in an album of another person as in their own, and
the album's count, cover and listing leave it out for the same reason
(`photoIsOpenToSQL`), so an album never promises a photo that answers not found.

What a reader who is not the photo's owner is given:

- no latitude, longitude or place name, no offset from UTC (which, with the
  date, says what part of the world it was taken in; the instant stays), no
  favorite mark and no uploader name for it, unless the share includes places
  (the reference chip of a photo the reader points the agent at is read the same
  way; a circle's album has no share, so
  its members see only their own photos' places, and the owner of an album
  never sees the places of others' photos in it, since no share points back at
  the owner);
- the preview in place of the original: the `files` route, a file in
  `ReadPhoto` and a signed playback address for a file all answer with the
  `large` preview (or a video's `video` preview), named as a picture or a
  video, because the original's bytes carry the place. A Live Photo's video has
  no such preview and is not found. Previews are made afresh and carry none;
- the same listing, search and count as the owner, but a listing of an album
  gives a place away by no other means either: the distance filter and the
  words of a place find only the reader's own photos in it;
- the faces as the owner's: the boxes round them, and the names the owner gave
  them only through a share that includes them (`is_face_names_included`),
  and then only on the album owner's own photos, as places are. The names are
  entries in the owner's address book, so a reader is never given the contact
  or the group (`contactId`, `photoFaceGroupId`, the owner's `userId` and
  `isGroupConfirmed` come back empty); in a circle's album a member has the
  names of the photos they added themselves and no others. `PhotoAccess.
  IsFaceNamesIncluded` is the answer, and `ReadPhoto` applies it
  (`withheldFaceNames`);
- no description and no text in the picture, no file name (the photo's own, and
  its files': a file is named `photo-` and the photo's id, and so is the copy the
  reader keeps), and no `describedAt`, so the reader's own assistant, which reads
  through the same operations, is told none either. These are held back as the
  place is, because a description says what is there ("the lighthouse at ...") and a
  file is often named for where it was taken. The search document a reader
  searches is only the camera, the names of the people and the title of the
  album being listed: not the place, the description, the file name or the titles
  of the other albums the photo is in, which include the owner's own that are
  not shared.

`SavePhotoToLibrary` makes a reader a photo of their own. Where the share
includes places it is made over the same blob (the owner's `photo_file` rows
are copied to a new `photo` of the reader's with source `shared` and client
photo id `shared:` and the original's SHA-256, so saving twice is saving once),
nothing new is stored, and the quota counts the files. Without places it stores
the preview the reader could see as the new photo's original, one new blob,
because the original's bytes would hand the place over, with client photo id
`shared-preview:` and the photo's id (not the original's digest, which names
the file the reader was kept from) and no offset from UTC. Either way the new
photo is processed like any other.

Leaving a circle ends what the circle made possible. The circles code calls
`endPhotoAlbumSharesOutsideCircles` (in `endSharesOutsideCircles`, inside the
membership lock) for each person who left, deleting direct shares between
people who no longer share any circle and the person's shares to circles they
left, and then taking out of every album the person owns or added to the
photos of people who no longer reach it at all, by asking
`GetPhotoAlbumAccess` of each. The same sweep runs when a share is taken back.
Making a share, or adding to somebody else's album, takes the membership lock
first, so a change made as somebody leaves is either cleaned up by the leaving
or refused.

Photos added to an album other people read are announced to their phones:
"Ana added 6 photos to Family", at most once per album per reader every ten
minutes, to the readers who may keep photos and have a phone registered. The
notification kind is `photo`, which a phone that chose its kinds before it
existed does not receive until it is switched on. The ten minutes are kept in
memory by each server instance.

The agent's tool shows the albums that are not the person's with their owner
and level (`album-list`), and reads them where they are: `list` and `look`
with the album's `album_id`, each photo marked with its owner. It copies
nothing into the person's library unless they ask (`save-shared`). In a chat
turn, whose words are posted to everybody in the room, it reads only the
person's own: `album-list` leaves the others out, and `album-read`, `list`,
`read`, `look` and `show` refuse a photo or an album that is not the person's
(`IsOwnedByCaller` is false) and say it is not repeated into a chat.
Every operation of the tool that answers a list (`list`, `album-list`,
`album-read`, which gives the album's photos, `people`, `person-photos` and
`album-shares`) gives a page at a time: `page_size` and `offset` in, and when
there is more `next_offset` out (with the count, `matching_photo_count` for
photos, `total_count` for the rest), as the mail and memory tools do. `library`
gives `photo_count` for all items, videos among them, as albums count them, and
`video_count` for the videos in it. `set-date` (a write) corrects the date a
photo was taken, from an RFC 3339 time with an offset, as `teanode photo
set-date` and the viewer's change of date do. `search-places` (`query`, and
`near_latitude` with `near_longitude`) finds cities, `set-location`
(`photo_ids`, and either `place`, whose best match is used, or `latitude` and
`longitude`) and `remove-location` (`photo_ids`) change where photos were
taken, as `teanode photo search-places`, `set-location` and `remove-location`
do, for up to 100 photos at a time. `set-location` is a write like `set-date`;
`remove-location` cannot be undone, so it is destructive and asks the person
every time. `read` also says where a photo was taken: `location_place` (`id`,
`place_name`, `place_label`) when it was inside one of the person's own
Location places, else `address_text` from the place provider, beside the city
in `place_name`; the command line's `teanode photo read` says the same in its
place line (your own place, else the street, else the city) and adds
`locationPlace` and `addressText` to `--json`. `delete` (photos that are in the
Trash) and `empty-trash` delete forever, as `teanode photo delete` and
`empty-trash` do, and are destructive: they ask the person every time. `delete`
says how many went and, when some did not, that the others were not in the Trash
and were left alone. `person-read` gives one group of faces, as `teanode photo
people read` does, and `person-photos` its photos. Turning faces on is of the
risk class granting and turning them off is destructive: finding faces is a
privacy choice either way, so both ask.

`album-share` is of the risk class granting, as the calendar's share is, so it
asks the person unless they allowed it, and the card names the album and the
people. It takes one `with`, a username or a circle's name or id, resolved by
`tools.ResolveGrantee` as `calendar_share` does and `teanode photo album share
ID <username|circle>` does through `shareTarget`: a username wins over a circle
of the same name, and a circle name that two circles have is refused with their
ids and sizes. `album-create` with `circle` and `teanode photo album create
--circle` refuse such a name the same way. `album-unshare` only takes access away, so it is a write, as
`calendar_unshare` is, and does not ask. `album-share-update` changes one share (`teanode
photo album share-update`, the same `UpdatePhotoAlbumShare` the dashboard and
the phone use). It takes the album as well as the share, reads the album's
shares, and changes only the settings the call names, `share_level`,
`is_location_included` and `is_face_names_included`: the operation writes all
three, so the ones left out are sent as the share has them, and stopping
somebody adding photos does not take the places away. A call that names what the share already has makes no request and
says that nothing changed. The risk class is judged from the arguments alone,
since the share is not known at that point: a call that names `contribute`, places on or
face names on is granting, even when the share has that setting already, and any
other call can only keep or take away access, so it is an ordinary write with no
card. The card for a granting call reads the album and the share, names the
album and the person or circle, and says what changes and what stays, or that
nothing changes. `look` does not queue a description of somebody else's photo: a
description belongs to the library of its owner.

## The iPhone

The app shows the server's library itself (previews cached on disk) and
saves photos to the phone's Photos when asked. Backup sends every photo and
video in the phone's library, the original, an edited version and a Live
Photo's video, over a background URLSession, using the library's persistent
change token to find what changed; the first backup runs as a continued
processing task the person starts, and catching up runs overnight. Deleting a
photo on the phone never deletes it on the server. Photos' own background
upload extension is not used yet: it needs a fixed server address in the
app's build, which a self-hosted server does not have.

### How the backup keeps going

A pass (`PhotoBackupCoordinator.runPass`) reads the library, notes every file
still to send in the index, and hands files to the background URLSession. A
library is read in batches, so that a library of 35,000 items starts uploading
in seconds, resumes where it left off, and reports progress while it is read
(see "Reading a large library" below). The queue the pass hands over is long, up to 200 files and 10 GB, because iOS goes on
sending whatever is queued after the person has left the app, and a short queue
runs dry within minutes. Every queued file is an exported copy in the app's
outgoing folder, so the bytes queued may not pass a quarter of the free space
on the phone, and the free space is read again before each file is exported.
The session sends four at a time (`httpMaximumConnectionsPerHost`), so the
server is not flooded however many are queued. Files that are on the phone are
queued first; files that have to come down from iCloud come after them, so
that the queue fills fast and one slow download does not stand in front of the
rest.

Every file goes up as an upload made first (see Uploads), so that its address
is known before a byte is sent. The pass creates the upload with a small
request of its own (`PhotoBackupRequest.makeCreation`, sent through
`PhotoBackupUploadClient` while the app runs, since the background session takes
upload tasks only), keeps the address in the index
(`PhotoBackupRoleRecord.partialUpload`: the path, the file's size and its
SHA-256), and hands the background session a `PATCH` of the exported file at
offset 0.

What continues and what waits:

- Files already queued continue with the app closed. The backup screen says
  "Uploading in the background: N files queued".
- A file can be queued only after its upload has been made, and the app makes
  that request itself (the background session takes upload tasks only), so
  queueing needs the app running and a network it may use. With "Wi-Fi only" on
  mobile data, or with no network, a pass queues nothing new, and the first
  request that finds there is no network ends the pass's queueing (the rest
  would each wait out a timeout to find the same). Before uploads were made
  first, a file could be queued to wait for Wi-Fi and start with the app closed;
  now only files that were queued while Wi-Fi was there do. The others wait for
  the app to be opened, the network to come back, or the overnight task. The
  cost is a `POST` round trip for every file queued (and a `HEAD` before it for
  a file with an address), which a wake's top-up pays within its 18 seconds, so
  it queues fewer files than it did.
- When an upload finishes and the app is open, the app starts the next pass
  (`finished` says whether to), so the library is worked through as uploads
  finish and not only when the app is next opened. Only an upload that went up,
  or one found to have moved on (described under "Continuing a file that was cut
  off"), starts a pass. A file the server refused, or one it asked to wait for,
  does not, so a file that always fails cannot start passes in a loop. Refusals
  are
  counted per file and stop at three. After three server failures in a row a
  pass stops and the app tries again after a minute, then two, then four, up to
  a quarter of an hour; a longer `Retry-After` from the server stands.
- When iOS wakes the app because uploads finished, it has about 30 seconds. In
  the first 18 the app queues more files that are on the phone already,
  smallest first (`PhotoBackupCoordinator.topUp`), writes the index, and only
  then calls the completion handler. It lists no library and never reads from
  iCloud in that time.
- Files in iCloud, files that did not fit in the queue, and the rest of the
  library wait for the app to be opened, a network that comes back, or the
  overnight processing task (on power and a network). The backup screen says
  how many wait.
- A file the photo library does not hand over within 30 minutes (a download
  from iCloud that never finishes) is left for a later pass, and the pass goes
  on with the next file. Before this, one such file held up the pass and every
  later attempt to start one, because a pass already running is joined, not
  started twice.

### Continuing a file that was cut off

A file can be cut off: the person pauses backup, the app is killed, or the
connection drops and iOS gives up on the task. The server keeps the whole parts
it received, so the next try sends only what it lacks. When a file with a kept
address is queued again, `PhotoBackupCoordinator.prepareSending`:

- exports the file again (a pause deletes the exported copies, to give the room
  back) and compares its size and SHA-256 with the ones kept. A file that is not
  the same bytes is not continued: the app sends a `DELETE` for the old upload
  (`PhotoBackupCoordinator.forgetKeptUpload`), and the server also ends an
  unfinished upload of the same photo and file when a new one starts, in case
  the `DELETE` did not arrive.
- asks the server where the upload is (`HEAD`). The answers are: an offset, and
  the file goes on from there; complete, and the photo is recorded from the
  `X-Photo-ID` it names without sending a byte (the case of an answer that was
  lost); gone (404, or an offset past the end of the file), and a new upload is
  made and the file goes up from its first byte. A refused question (any 4xx
  other than a 401, a 404 or 410, and the answers that say to wait: 408, 425,
  429) also drops the address and makes a new upload. A 403 is one of them: a
  `HEAD` has no body, so it cannot tell a device password made without photos
  from any other refusal, and the request that makes the new upload has a body
  that can. A question that cannot be answered (no network, the server failing,
  a sign-in refused) queues nothing and keeps the address for the next pass.
- writes the bytes from the offset into `<exported name>.tail` in the outgoing
  folder, since a background upload reads a file, and deletes the whole exported
  file at once. The queue counts the rest, not the whole file. The rest is
  deleted when its task ends, whatever the outcome; one left by a killed app is
  deleted by the next pass like any file nothing queued refers to. The rest is
  written only if it is at most half of the free space, because the whole file
  is on the phone until it is done; otherwise the file starts over from its
  first byte, which needs no room beyond the file. The rest has the attributes
  of the exported file (readable by the app alone, and protected until the
  phone's first unlock). It is written a piece at a time on a task of its own,
  and pausing, turning backup off or cancelling the pass ends the write between
  two pieces and deletes what was written. If the write fails (a full disk), the
  address is forgotten, the upload is dropped on the server, and the whole
  file, still on the phone, goes up as a new upload, so the same failure does
  not come back at every pass.

An offset equal to the file's size means the server has every byte and has not
completed the upload: the rest is empty. Its `PATCH`, at the end of the file
with `Upload-Complete: ?1`, is not given to the background session, which may
not run a task for a file of no bytes. The app sends it itself, as a plain
request with a body of nothing, and records the photo from its answer. The
server completes the upload whether or not the app hears the answer in time, so
a request that times out is harmless: the next `HEAD` says the upload is
complete and names the photo.

A `PATCH` the background session sends can be answered 409 (the server has
another number of bytes than the request said, as when the part that was in
flight lands after the question was asked) or 404 or 410 (the upload is gone).
The file is not at fault and the server is not failing, so this is no error on
the screen and no failure toward the three that stop a pass
(`PhotoBackupOutcome.uploadMovedOn`). `finished` says to start a pass at once,
which asks `HEAD` again: a new offset is continued from, and a gone upload makes
a new one. The first three times in a row for a file (`uploadMovedOnLimit`) each
start a pass at once; the fourth is counted as the server failing, so that a
file that is never where it is said to be cannot start passes without end. The
count starts again when the file goes up.

When the app forgets the address of an upload that the server may still hold
(the file is not the same bytes any more, the offset is beyond the file, the
rest could not be written, the photo was deleted from the library, or backup was
turned off), it sends `DELETE` for the upload. This is best effort and is not
waited for, except when backup is turned off, where the requests are sent
before the device password they carry is revoked. An upload the server said it
does not have is not deleted. An upload nobody continues is ended by the server
after a week in any case; the `DELETE` gives its room back sooner.

The address is written to the index with the next change after thirty seconds,
and when the app goes to the background, like the rest of it. A kill between
two writes loses the address, and the file then starts over in an upload of its
own. A file whose digest could not be worked out is sent the same way but its
address is not kept.

### Reading a large library

The first pass, and a pass after the library's change token expired, lists the
whole library. It does not read it whole and then start:

- The library gives only identifiers and creation dates (`listBatch`), newest
  photo first, 300 at a time. Items (the files of each photo, and iCloud's
  identifier for it) are built only for the 300 at hand, and only for photos the
  index has no record of. No more than a batch of items is in memory.
- For each batch the pass asks the server which photos it has (`FindPhotos`),
  plans, writes the files to the index as still to send, and queues what the
  budget allows, before it reads the next batch. The first upload is queued
  after one batch. What the budget does not take stays in the index and is sent
  after the listing, from the index's own line (`sendOrder` in each record, so
  newest photo first), read 300 items at a time. Photos that come into the
  library later go to the front of that line.
- The index keeps where the listing stopped (`PhotoBackupListingProgress`: the
  creation date, to the fraction of a second, and identifier of the last item,
  the number listed, and the change token as it was when the listing began). A
  pass that stops (iOS ends the continued processing task, the person leaves,
  the app is killed) is followed by one that goes on from there and does not
  begin another listing. The library's change token is stored only when the last
  batch is done, so changes made during the listing are read afterwards.
- The index file is replaced whole, so it is not written for each batch: a
  listing writes it when it begins and ends, and otherwise at most every 30
  seconds, when a pass ends, and when the app goes to the background or the
  continued processing task is ended by iOS. Writing it after every batch of
  35,000 items took 14 s in the test build, against 0.4 s for the batches and
  0.2 s for the one write at the end. A pass killed without notice loses at most
  those 30 seconds of listing.
- The status says how far the listing has got ("Reading your photo library:
  1,200 of 35,103"). The continued processing task's progress counts the
  listing as 30 percent of the work and the files as the rest, and never goes
  back, so iOS does not end the task as stalled.
- A file that has to come down from iCloud is given 60 seconds while the
  library is being listed, so that one slow download does not hold the listing.
  It stays in the index and the pass tries it again afterwards with the full time.
- A pass tries each file once. The files read again after the listing skip the
  ones the pass already queued, refused or left for later.

The backup screen asks the server for the library's size (`photoLibrary`) when
it appears, when the app comes to the foreground and when backup finishes
something, and at most once every 15 seconds. The numbers it shows for backup
come from the phone. The rest of the traffic seen while the screen is open is
the app's presence report: `AppShell.refreshPresence` repeats every 30 seconds
on every screen (services, access session, chats, presence).

Players cannot send a sign-in, so `PhotoPlaybackAddress` answers a signed
address, valid for six hours, that streams one preview or file with ranges. The
signature covers the owner, the reader it was made for, the photo, which
preview or file and the expiry, and each fetch asks `GetPhotoAccess` again for
that reader (and that they are enabled and have `photos:use`), so an address
stops with the share that let the reader in, and a file address stops when the
share stops including places. The query carries `owner`, `reader` and
`signature`.

## Cleanup

`photo:cleanup` deletes uploads that failed or went idle for a week, photos
in the trash past `photos.trashRetention` (and those deleted forever, which
are only marked in the request and deleted here in batches), blobs no file
refers to an hour after they became unreferenced, and face groups left
empty.

A blob is found by `photo_blob.unreferenced_since`, which a trigger on
`photo_file` keeps (cleared when a file names the blob, set when the last
one stops, whatever stops it: a replaced file, a deleted photo, a deleted
account), through a partial index, so a pass does not look at every blob. It is
deleted in two steps: marked (`deleting_at`) in a transaction that commits,
which is what makes a file arriving with the same bytes store them again; and
then one transaction that takes the blob's advisory lock (the one a file takes
while it is recorded), reads the blob again, and only if it is still marked and
no file names it deletes the object and then the row. A file that claimed the
bytes after the mark has cleared it or named the blob, and is seen by that
second read. A blob whose object cannot be deleted is logged and passed over,
and the ones behind it go on.

Deleting an account lists its objects (previews and upload parts) while its
rows say what they are, deletes the account, and deletes the objects after the
transaction commits, so an account whose deletion fails keeps its previews.
