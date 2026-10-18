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

`internal/picture` reads pictures and videos: decoding JPEG and PNG upright,
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
  the long side), `large` (2048) and, for a video, `video` (H.264, 1280).
  Previews can always be made again; nothing depends on them being kept.
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
speaks the IETF resumable upload draft, because the iPhone's background
uploads do: `OPTIONS` says the largest file allowed, `POST` creates an upload
and answers an interim `104` with its address before reading the body,
`HEAD` says how many bytes are stored, `PATCH` appends from there. `OPTIONS`
writes the limit as `max-size=<bytes>` for a client that names revision 5 or
later of the draft in `Upload-Draft-Interop-Version`, and as the bare number
for one that names an earlier revision or none. What the
file is (the uploader's own name for the photo, which file of it, when it was
taken) travels in `X-Photo-*` headers, since a background upload's body is
nothing but the file. Only `X-Photo-Client-ID` is required. When the request
names its content type, a type the library does not keep is refused with a 415
before a byte is read; a request that names none, or `application/octet-stream`,
is judged by its file name and its bytes once they have all arrived. A
picture whose first bytes are a JPEG, PNG, GIF or HEIC is kept as that type
whatever it was sent as, because an iPhone sends a picture saved from a
message as `image.jpeg` in whichever format it is; processing reads those
bytes again before it picks a decoder, for files kept before this check.

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
`heif-dec` runs. A PNG or GIF with a side over
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
likeness. A named group is always listed. The agent's photo tool can take a name off a
group (`person-unname`), hide a group or list it again (`person-hide`, with
`is_hidden`; `people` with `is_hidden_included` finds the hidden ones), say one
face is not that person (`person-remove-face`, with the `photo_face_id` that
`read` gives), and choose an album's cover (`album-cover`); each is a write
that does not ask first, since the person can undo it. Faces are on for
everybody by default; switching them off deletes every face and group the
person has.

## Sharing

An album belongs to a person (`photo_album.user_id`) or to a circle
(`photo_album.circle_id`), exactly one of the two. A person shares an album
with one person they share a circle with, or with a circle they are in, at a
**share level** (`view`, to see and save its photos, or `contribute`, to also
add photos of their own and take out the ones they added), and says whether the
share includes where the photos were taken (`is_location_included`). A circle's
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
caller was let in.

What a reader who is not the photo's owner is given:

- no latitude, longitude or place name, no favorite mark and no uploader name
  for it, unless the share includes places (a circle's album has no share, so
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
- the faces as the owner's, with the names the owner gave them; a share dialog
  says so, since the names are the owner's words and are given with the photo
  whether or not places are;
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
because the original's bytes would hand the place over. Either way the new
photo is processed like any other.

Leaving a circle ends what the circle made possible. The circles code calls
`releasePhotoAlbumsOutsideCircles` (in `endSharesOutsideCircles`, inside the
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
nothing into the person's library unless they ask (`save-shared`).
Every operation of the tool that answers a list (`list`, `album-list`,
`album-read`, which gives the album's photos, `people`, `person-photos` and
`album-shares`) gives a page at a time: `page_size` and `offset` in, and when
there is more `next_offset` out (with the count, `matching_photo_count` for photos,
`total_count` for the rest), as the mail and memory tools do. `library` gives
`photo_count` for all items, videos among them, as albums count them, and `video_count` for the
videos in it. `set-date` (a write) corrects the date a photo was taken, from an
RFC 3339 time with an offset, as `teanode photo date` and the viewer's change of
date do. Turning faces on is of the risk class granting and turning them off is
destructive: finding faces is a privacy choice either way, so both ask.
`album-share` and `album-unshare` are of the risk class granting, as the
calendar's share is, so they ask the person unless they allowed it, and the
card names the album and the people. `look` does not queue a description of
somebody else's photo: a description belongs to the library of its owner.

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

What continues and what waits:

- Files already queued continue with the app closed. The backup screen says
  "Uploading in the background: N files queued".
- When an upload finishes and the app is open, the app starts the next pass
  (`finished` says whether to), so the library is worked through as uploads
  finish and not only when the app is next opened. Only an upload that went up
  starts a pass. A file the server refused, or one it asked to wait for, does
  not, so a file that always fails cannot start passes in a loop. Refusals are
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
address, valid for six hours, that streams one preview or file with ranges.

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
