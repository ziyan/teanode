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
`teanode-faces`, the C++ face reader in `tools/faces/`.

`internal/db/database_photo*.go` keeps the rows, migrations 0220 to 0223 make
them, and `internal/api/v1api/apigraph/photo*.go` is what every client talks
to. The dashboard is `web/src/pages/photos/` and `web/src/photos/`, the
command line `internal/cmd/photo*.go`, the agent's tool
`internal/agent/tools/photo/`, and the iPhone `ios/TeaNode/Sources/Photo*.swift`
with `ios/TeaNodeKit/Sources/TeaNodeKit/Photo*.swift`.

Photos are a hosted service: `services.photos` is off until an operator turns
it on (unlike the services that came before the switches), and a person needs
`photos:use`, which the built-in roles have. They work without the agent; the
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
- `photo_upload` and `photo_upload_part` are a file on its way in.
- `photo_album` and `photo_album_item`; `photo_vector` (a description's
  embedding); `photo_face` and `photo_face_group`.

A backup of the database is not a backup of the library: the blob store has
to be backed up too.

## Uploads

Every client uploads through one endpoint, `/api/v1/photos/uploads`, which
speaks the IETF resumable upload draft, because the iPhone's background
uploads do: `OPTIONS` says the largest file allowed, `POST` creates an upload
and answers an interim `104` with its address before reading the body,
`HEAD` says how many bytes are stored, `PATCH` appends from there. What the
file is (the uploader's own name for the photo, which file of it, when it was
taken) travels in `X-Photo-*` headers, since a background upload's body is
nothing but the file.

The body is stored in parts of at most 8 MiB as it arrives, each with a
receipt row once it is whole, so the offset a client is told is always bytes
that are durable and a dropped connection costs at most the part in flight.
Completing reads the parts back, checks them against their receipts and the
declared digest, stores the file under its SHA-256 (in parts on S3 for files
over 256 MiB), and records it; the blob's row is committed right after its
bytes, so bytes whose recording then fails are reclaimed by cleanup. The
quota counts what is stored and what is still arriving.

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
count against the photo. `RetryPhotoProcessing` starts a failed photo again.

## Finding photos

`ListPhotos` filters by date, kind, favorites, the trash, an album, a person
or a contact, and a point and a distance, and searches: full-text over the
place, file name, camera, album titles, the names of the people in it, the
description and the words read in the picture, and, when an embedding model
is configured, by meaning over the descriptions.

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
so a photo that is not theirs, or is deleted, is refused by `AskAgent` (not
found) and dropped by any other caller; a photo in the Trash is kept and said
to be there. The chip is rewritten from the stored row, whatever the client
sent: the place and the day it was taken on the clock where it was taken,
separated by a middle dot, or the file name, or "Photo". The model is told the
photo's id, kind, time, whether it is in the Trash, and the place and file name
fenced as data, and that it can `look` at it with the photo tool. Nothing from
the description is copied in. The same photo twice is told once.

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
(`MinimumUnnamedPhotoFaceGroupFaceCount`), which is three photos, since a
group never takes two faces from one photo; a pair is more often a chance
likeness. A named group is always listed. The agent's photo tool can take a name off a
group (`person-unname`), hide a group or list it again (`person-hide`, with
`is_hidden`; `people` with `is_hidden_included` finds the hidden ones), say one
face is not that person (`person-remove-face`, with the `photo_face_id` that
`read` gives), and choose an album's cover (`album-cover`); each is a write
that does not ask first, since the person can undo it. Faces are on for
everybody by default; switching them off deletes every face and group the
person has.

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

Players cannot send a sign-in, so `PhotoPlaybackAddress` answers a signed
address, valid for six hours, that streams one preview or file with ranges.

## Cleanup

`photo:cleanup` deletes uploads that failed or went idle for a week, photos
in the trash past `photos.trashRetention` (and those deleted forever, which
are only marked in the request and deleted here in batches), blobs no file
refers to an hour after they became unreferenced, and face groups left
empty.
