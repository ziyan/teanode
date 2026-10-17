# Photos: a library of photos and videos, backed up from the iPhone, found and organized by the agent, shared in circles

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

TeaNode keeps a person's mail, contacts and calendar on a server they run. Their photos and videos live somewhere else: on the phone, in iCloud, in a folder. After this plan TeaNode keeps them too.

- A person's iPhone backs up its whole photo library to TeaNode, in the background, without the app open. Deleting a photo on the phone leaves TeaNode's copy alone.
- The dashboard, the iPhone app and the command line show the library: a timeline by date, favorites, albums, people, a trash, and a viewer that plays videos and Live Photos. Anything can be uploaded from the dashboard or the command line, saved from a mail or a conversation, and saved from TeaNode back to the iPhone's Photos.
- Faces are found on the server, grouped by person, and named by choosing a contact from the address book. "Photos of Grandma" works.
- The agent finds photos ("the receipt from the hardware store in June", "videos of the kids at the beach"), looks at them, puts them in albums, suggests albums for a trip, and shows them in the conversation. A photo the agent or the person looks at is described by the model once, and the description is kept and searched from then on.
- An album can be shared with a circle (a family, friends or colleagues on the same server) or one person in it, to view or to contribute to, and a family can keep albums that belong to the whole circle.

To see it working: switch Photos on in the server's services, drop a few pictures on the dashboard's Photos page and see them in the timeline with their dates and places; turn on backup in the iPhone app and see the phone's photos arrive; ask the agent "find the photos from last weekend and put them in an album called Picnic"; open People, name a face group by choosing a contact; share the album with your family circle and open it as another member.

## Progress

- [x] (2026-10-16 22:30 EDT) Research: what exists, the iPhone's background upload API, the resumable upload draft, faces without a hosted service. Written into Context and Orientation and the Decision Log.
- [x] (2026-10-16 22:40 EDT) Decision records 20261016-photos-are-rows-and-their-bytes-are-blobs, -uploads-follow-the-resumable-upload-draft, -the-iphone-backs-up-and-teanode-keeps, -a-photo-is-described-when-somebody-looks-at-it, -faces-are-found-on-the-server-and-named-from-contacts, -an-album-is-shared-at-a-level.
- [x] (2026-10-16 23:20 EDT) Milestone 0: prototypes. The face reader works (0.2 s a picture, 42 MB of libraries and 39 MB of models when it reads raw pixels); ffmpeg cannot assemble tiled HEIC, libheif's `heif-dec` can; a 104 passes the middleware chain. See Surprises & Discoveries.
- [x] (2026-10-16 23:40 EDT) Milestone 1, part one: service switch (off until chosen), `photos:use`, the `photos` settings, migration 0220, models and queries, the resumable upload endpoint with device password sign-in, S3 multipart, serving, cleanup, operations including `offset` and `CountPhotos`.
- [x] (2026-10-16 23:50 EDT) Milestone 1, part two: processing (metadata, places, previews) without faces, which wait for Milestone 5; deleting forever by marking and cleanup in batches; the allow-list of file types; a ready photo without a small preview is processed again.
- [x] (2026-10-16 23:50 EDT) Milestone 2, command line (`teanode photo`) and the agent's `photo` tool.
- [x] (2026-10-17 00:00 EDT) Milestone 2, the dashboard: Photos page, viewer, uploads, selection, trash, Photos in setup, device passwords that grant photos, the limits on the server's storage page.
- [x] (2026-10-17 00:20 EDT) Milestone 2, the iPhone: Photos section, viewer, Save to Photos, disk cache of previews; it compiles on CI's macOS build and its screenshots were checked.
- [x] (2026-10-17 01:00 EDT) Walk-through on a dev server in headless Chrome: setup with Photos chosen, restart, uploads from the dashboard and the command line (a file sent from both is stored once), dates in each photo's own zone, place names, search, the agent listing, looking at and showing photos. Findings fixed: a ward named instead of its city, idle jobs logging not-found, words that differed between the command line and the dashboard, the agent giving UTC dates, a Select mode for touch screens.
- [x] (2026-10-17 01:30 EDT) Milestone 3: albums on the server, the command line, the agent tool, the dashboard and the iPhone, with suggestions from trips.
- [x] (2026-10-17 01:00 EDT) Milestone 6, in the app: backup with a device password that grants photos only, a background URLSession, the library's persistent change token, a continued processing task for the first backup and a processing task overnight. The Photos background upload extension is not built: it needs a device test of `BackgroundUploadURLBase` (see Surprises).
- [x] (2026-10-17 02:00 EDT) Signed playback addresses (`PhotoPlaybackAddress`) for players that cannot sign in; `FindPhotos` says which files the server holds; a `Z` taken-at is an instant whose offset comes from the file.
- [x] (2026-10-17) Milestone 4, server, command line and agent tool: migration 0222, the `describe_photo` job, `DescribePhoto` and `CountUndescribedPhotos`, search over `search_document` and meaning, `teanode photo describe`, the tool's `describe`. The dashboard's and the phone's Describe button wait for their own part.
- [x] (2026-10-17) Review of the server side, seventeen findings fixed. Bytes stored for a file whose recording failed get a `photo_blob` row at once, so cleanup finds them; the bytes of unfinished uploads count towards `maxAccountBytes`; a panic while processing fails the photo and not the server; attempts are counted when a photo is claimed and a failed one waits 1, 5, 15 and then 60 minutes (`processing_not_before`); decoded pictures share a budget of 128 megapixels and are turned while they are sampled; `RetryPhotoProcessing` and `teanode photo retry`; every photo with a date has an offset; the command line waits for the server to finish a large upload; two workers that claim a photo at a time; a restarted upload ends the earlier try; uploads are capped in parts and keep their next index; deleting an account deletes its previews and upload parts; saving an attachment holds no transaction; deleting photos for good takes their faces out of their groups; a search embeds its words once and looks for parts of words only when the words leave room; a photo's failure message is a fixed sentence; a type that is not kept is refused with a 415 before a byte is read. See the Decision Log.
- [ ] Milestone 5: faces and people.
- [x] (2026-10-17) Milestone 7, server, command line and agent tool: the branch is rebased onto the circles branch; migration 0224 (circle albums, `photo_album_share`); `GetPhotoAlbumAccess` and `GetPhotoAccess` as the one answer to who reads what, and the four `requirePhoto...` ways in; `SharePhotoAlbum`, `UpdatePhotoAlbumShare`, `RemovePhotoAlbumShare`, `ListPhotoAlbumShares`, `CreatePhotoAlbum(circleId)`, contributors adding and removing their own photos, `SavePhotoToLibrary`; places held back from readers on every route, search and filter; leaving a circle and taking a share back end what they allowed (`releasePhotoAlbumsOutsideCircles`); phone notifications of added photos (kind `photo`); `teanode photo album share|unshare|shares`, `album create --circle`, `photo save`; the tool's `album-share`, `album-unshare`, `album-shares`, `save-shared`. The dashboard's and the phone's part wait for their own.
- [ ] Milestone 7, dashboard and iPhone: Share sheet, Shared with you and circle albums, Add photos on an album one contributes to, Save to my library.
- [ ] Milestone 8: getting started, documentation, review until it converges, visual walk-through, deploy.

## Surprises & Discoveries

- Observation: a deployment that keeps its files in a self-hosted S3 store (MinIO in the deployment's compose file) with no local storage directory already has the library's bytes on whatever disk that store's volume is on; nothing in this plan moves storage.
  Evidence: `storage` configured with `directory: ""` and `s3.endpoint` set, which is `shared` mode.
- Observation: the Photos framework's background upload extension requires the server to implement the resumable upload draft, and reads a static `BackgroundUploadURLBase` from the extension's Info.plist. A self-hosted server's address is not known when the app is built. Community reports disagree on whether uploads to another host are refused.
  Evidence: Apple's "Uploading asset resources in the background" article (preflight `OPTIONS` with `Upload-Limit`, `104` with `Location`); two open-source apps reporting opposite results.
- Observation: an interim `104` written by a handler reaches the client through the access log and compression wrappers unchanged; compression only gzips the final answer's small body.
  Evidence: a probe test wrapping a handler that writes 104 then 201 in `LoggingMiddleware(CompressionMiddleware(...))`, read with `httptrace.ClientTrace.Got1xxResponse`: "both: interim [104] final 201 encoding gzip offset 5".
- Observation: ffmpeg 7.1's command line decodes only one tile of a tiled HEIC, the way every iPhone stores a photo. A 640 by 960 picture in 256 pixel tiles came out as one 256 by 256 tile. libheif's `heif-dec` (Debian trixie `libheif-examples` 1.23.4 with `libheif-plugin-libde265`) assembles the tiles and applies the rotation: 640 by 960, upright, in 35 ms, adding 13 MB of libraries.
  Evidence: `ffprobe` reports a "Tile Grid" stream group and `ffmpeg -i tiled.heic -frames:v 1 out.png` writes 256x256; `heif-dec tiled.heic out.jpg` writes 640x960.
- Observation: OpenCV's YuNet and SFace find and tell faces apart well enough for face groups. On public sample pictures, the same face rotated 9 degrees, shrunk and darkened scored 0.681 cosine similarity with the original, mirrored and halved 0.857, and a different person 0.130, against SFace's same-person threshold of 0.363. A picture takes about 0.2 seconds including loading the models.
  Evidence: the prototype in Milestone 0, run in `debian:trixie-slim`.
- Observation: linking OpenCV's image decoders (`imgcodecs`) drags in GDAL, HDF5 and more, about 208 MB of libraries. Reading raw pixels (a binary PPM, which Go writes from the JPEG preview it already decodes) needs only `core`, `imgproc`, `dnn` and `objdetect`: 42 MB. The two models are 39 MB (SFace 38.7 MB, YuNet 0.2 MB).
  Evidence: `ldd` and `du -chL` on the two prototype builds.

## Decision Log

- Decision: metadata goes into the database, bytes into the blob store; blobs are named by SHA-256.
  Rationale: the maintainer's rule, and what recordings and knowledge attachments already do. Content naming stores a picture saved twice once. See `docs/decisions/20261016-photos-are-rows-and-their-bytes-are-blobs.md`.
  Date/Author: 2026-10-16, the maintainer.
- Decision: the iPhone backs up by default; deleting on the phone never deletes on TeaNode; TeaNode's photos reach the phone through the app's own library first, with "Save to Photos".
  Rationale: chosen by the maintainer. See `docs/decisions/20261016-the-iphone-backs-up-and-teanode-keeps.md`.
  Date/Author: 2026-10-16, the maintainer.
- Decision: a photo is described by the model only when somebody works with it (the agent looks at it, or the person asks in the viewer), and the description is kept.
  Rationale: chosen by the maintainer; describing a whole library costs as much as the library. See `docs/decisions/20261016-a-photo-is-described-when-somebody-looks-at-it.md`.
  Date/Author: 2026-10-16, the maintainer.
- Decision: faces are part of the first version, found on the server by a separate C++ program; the Go server stays pure Go.
  Rationale: the maintainer asked for faces in v1 and for Go to stay pure, invoking an external C++ binary. OpenCV 4.10 in Debian trixie has the YuNet detector and SFace recognizer, which is all a face group needs. See `docs/decisions/20261016-faces-are-found-on-the-server-and-named-from-contacts.md`.
  Date/Author: 2026-10-16, the maintainer.
- Decision: no storage move on the maintainer's server.
  Rationale: the maintainer asked to move storage to the large disk if it was not there; its object store and database were both already on it.
  Date/Author: 2026-10-16, the maintainer.
- Decision: one upload protocol, the resumable upload draft, for every client of photos.
  Rationale: the background uploader requires it and `URLSession` speaks it; one endpoint is less to test. See `docs/decisions/20261016-uploads-follow-the-resumable-upload-draft.md`.
  Date/Author: 2026-10-16, Claude.
- Decision: background uploads sign in with a device password (`sync_credential`) that grants photos.
  Rationale: a background upload's request is fixed when it is queued and may run days later; session tokens expire within that. Device passwords already exist for exactly this (a phone's calendar), grant only what they say, and can be revoked from the dashboard.
  Date/Author: 2026-10-16, Claude.
- Decision: no change feed and no phone-side database in this plan.
  Rationale: the in-app library pages from the server like the dashboard does, and the phone's backup only needs to know which of its own photos went up, which a small file in the app group holds. A photo's `client_photo_id` is the phone's iCloud identifier when it has one, so a second iPhone on the same iCloud library finds its photos already there.
  Date/Author: 2026-10-16, Claude.
- Decision: album sharing is built last, after the circles branch (`design/circles-and-chats`, pull request #528) pushes its circles and invitations, and this branch is rebased onto it. Migrations here start at 0220 so they do not collide with circles' 0192 to about 0200, and are renumbered to follow circles' last one at that rebase.
  Rationale: agreed with the session building circles. Sharing needs `circle`, `circle_member`, `SharesCircle` and `requireCircleMember`.
  Date/Author: 2026-10-16, Claude and the circles session.
- Decision: place names come from a small table of cities bundled with the server (GeoNames, cities of 15,000 people or more), never from an online geocoder.
  Rationale: `CONTRIBUTING.md` forbids a cloud dependency in a default code path; a city is what people search by.
  Date/Author: 2026-10-16, Claude.
- Decision: HEIC and HEIF pictures are decoded with libheif's `heif-dec`, added to the image beside ffmpeg; ffmpeg makes video posters and video previews; JPEG and PNG are decoded in Go.
  Rationale: ffmpeg 7.1 cannot assemble the tiles of an iPhone photo (see Surprises). `heif-dec` reads a file path, so the server writes the HEIC to a private temporary file first.
  Date/Author: 2026-10-16, Claude.
- Decision: the face reader reads a binary PPM (raw RGB pixels with a short text header), not an encoded picture.
  Rationale: it keeps OpenCV's image decoders and their 166 MB of libraries out of the image; Go already decodes the JPEG preview.
  Date/Author: 2026-10-16, Claude.
- Decision: the dashboard's timeline pages (200 photos a page, `Pager`, the page in the address); the iPhone's grid loads more as the person scrolls.
  Rationale: `docs/coding/frontend-design.md` says a list longer than a page pages rather than loading more, so a reload and Back keep the place; a phone list that loads as it scrolls is the platform's own behavior, and the app's other long lists do it.
  Date/Author: 2026-10-16, Claude.
- Decision: an omitted `services.photos` switch means off, unlike the services that came before the switches.
  Rationale: the other services' omitted switches mean on so an upgrade keeps what a server already did; no server had a photo library before, so turning one on by upgrading would hand every member a library and its disk use nobody chose. Setup and the services page turn it on.
  Date/Author: 2026-10-16, Claude.
- Decision: deleting forever only marks a photo, and `photo:cleanup` deletes the rows and the previews' bytes in batches of 100.
  Rationale: emptying a trash of thousands inside the request's one transaction timed out, and a rollback left previews deleted under rows that remained. The mark is `trashed_at` set to the epoch (no photo is trashed before 1970), plus the photo's client name changed to `deleting-<id>` so that sending the same file again makes a new photo; every query that reads a photo treats a marked one as gone, and the quota stops counting its files at once. The bytes go before the rows and outside any transaction, which is safe because a marked photo can no longer be seen or restored.
  Date/Author: 2026-10-16, Claude.
- Decision: the library keeps only the picture and video types phones and cameras write (JPEG, PNG, GIF, WebP, HEIC and HEIF, AVIF, TIFF, common camera raw files, MP4, QuickTime, WebM, 3GPP, M4V, Matroska), and refuses the rest naming the type.
  Rationale: anything starting with `image/` let in SVG, a document that can carry script, and the library serves what it keeps to a browser.
  Date/Author: 2026-10-16, Claude.
- Decision: faces are on by default for every person, who can switch them off; switching off deletes their faces and groups.
  Rationale: the maintainer wants faces; a person who does not can remove them entirely.
  Date/Author: 2026-10-16, Claude.
- Decision: the attempt is counted when a photo is claimed, a failure that is the file's keeps it and makes the photo wait (1, 5, 15, then 60 minutes, in `processing_not_before`), and a failure of the store or the database gives it back and waits a minute.
  Rationale: a file that kills the process never reaches the code that records a failure, so counting there let it be tried for ever; counting at the claim, and failing a photo that is claimed again with no attempts left, ends it. An outage of the store must not use up the attempts of every photo it touched, and waiting spares a broken file the pass every fifteen seconds.
  Date/Author: 2026-10-17, Claude.
- Decision: what a photo is told about its own failure is a fixed sentence ("The picture could not be read.", "The video could not be read."), and the program's last line goes to the log.
  Rationale: the failure message is shown to the person and handed to the model, and a tool's last line names a path.
  Date/Author: 2026-10-17, Claude.
- Decision: every photo that has a date has an offset. A picture date with no `OffsetTimeOriginal` is a clock reading and is read in the owner's time zone (`user.timezone`, UTC when there is none), a video's creation time and an uploader's `Z` instant are instants, and the owner's zone gives the offset it had then; the file's own offset completes an instant only when the file's date is the same moment. Every place that writes a date uses the photo's offset (`models.Photo.TakenAtOnClock`), and a photo not yet processed is on UTC. A date the person corrects is stored with the offset of the RFC 3339 value they gave.
  Rationale: renderers disagreed about a date with no offset (the viewer's zone, the computer's zone, UTC); deciding once, at processing, with the zone the server knows, lets all of them follow the one rule.
  Date/Author: 2026-10-17, Claude.
- Decision: temporary copies of videos, and the files heif-dec reads, are made in `photo-work/` inside the data directory, emptied when the server starts, and a file larger than the free space left there is refused; at most 128 megapixels of decoded pictures are held at once across the workers, a picture over 64 megapixels is refused, and a picture is turned while it is sampled (the decoded pixels are a view, and the scaler writes each pixel where the turn puts it).
  Rationale: the system's temporary directory may be memory, a video can be gigabytes, and full-size rotated copies of a 100 megapixel picture were the largest allocation of a processing pass.
  Date/Author: 2026-10-17, Claude.
- Decision: processing runs two workers that each claim one photo at a time and, when nothing waits, look again every two seconds while the other worker is busy.
  Rationale: a pass that claimed a batch and waited for it held every photo that arrived behind a long video until the video was done.
  Date/Author: 2026-10-17, Claude.
- Decision: the mutation that saves an attachment as a photo runs outside the request's transaction (`isTransactionFreeMutation`), reading the attachment's row in a short transaction and then hashing and storing the file; the agent's operations do the same.
  Rationale: the file takes as long as it is big, and a mutation otherwise holds its transaction for all of it. It changes only what it changes in transactions of its own, which the photo store already did.
  Date/Author: 2026-10-17, Claude.

- Decision: `photo_vector` names its model column `model` and carries a `user_id` scope column, not `vector_model`.
  Rationale: `Nearest` and `EnsureVectorIndex` in `internal/db/database_vector.go` read the columns `model`, `vector` and a scope column by name, as every other vector table has them; the pgvector index is built at start for `PhotoVectorTable` like the others.
  Date/Author: 2026-10-17, Claude.
- Decision: a search by words keeps the old substring match on the place, file name and camera beside the full-text match, and orders by rank with a ranked list of at most 500.
  Rationale: full-text matches whole words, and "harb" or "IMG_12" found photos before; a ranked order has no date cursor, so `beforePhotoId` continues after that photo's place in the ranking and `offset` skips into it.
  Date/Author: 2026-10-17, Claude.
- Decision: an older request for a description than a day is no longer pending (`isDescriptionRequested`), and a failed or refused job clears the request.
  Rationale: a job that dies must not leave a viewer showing "describing" for good; the job gives up within about 5 hours.
  Date/Author: 2026-10-17, Claude.
- Decision: the agent tool's `look` asks for descriptions through the `DescribePhoto` operation, not the queue directly, and its search answer carries `undescribed_photo_count` only for a search with a date or place filter.
  Rationale: one path for the person, the command line and the agent keeps the permission and availability checks in one place; a count of the whole library is not something to narrow by.
  Date/Author: 2026-10-17, Claude.
- Decision: a face group names its contact by two columns, `addressbook_id` and `contact_id`, with a foreign key on both that sets both null when the contact is deleted; the operations take the contact id alone and look it up in the person's address books in order, as `GetContact` does.
  Rationale: a contact's key is the pair (address book, id) because a CardDAV client chooses its own ids (migration 0050), so a single `contact_id` could not carry a foreign key. A contact that is deleted leaves its group unnamed instead of leaving a name that points nowhere.
  Date/Author: 2026-10-17, Claude.
- Decision: `photo_face_group` keeps the sum of its faces' unit vectors (`group_vector_sum`) and how many went into it, not the normalized mean; its face and photo counts and its cover picture are worked out when it is read.
  Rationale: only the direction of the sum matters to a cosine similarity, a sum takes one more face with one update, and counts worked out from the faces that are in photos outside the trash stay right when photos are trashed or deleted. Merging and taking a face out recompute the sum from the faces in the database in one statement.
  Date/Author: 2026-10-17, Claude.
- Decision: faces are grouped under a lock per person (`pg_advisory_xact_lock` on a key made from the person's id, also taken by switching faces off, merging and taking a face out), and two faces of one photo are never put in one group.
  Rationale: without the lock two photos read at the same time start two groups for one new person (the test fails without it); a photo does not hold the same person twice, so a group that already took one face of the photo is not a candidate for another.
  Date/Author: 2026-10-17, Claude.
- Decision: the faces of a photo are stored once, until the record that they were found (`faces_found_at`) is cleared, which a different original or edited file does; the faces the person confirmed carry over to the new face found where the old box was (overlap of at least a half).
  Rationale: two instances reading the same photo must not replace each other's faces, and an edited version should not lose the correction a person made.
  Date/Author: 2026-10-17, Claude.
- Decision: the box columns are `box_left_fraction`, `box_top_fraction`, `box_width_fraction` and `box_height_fraction`, and the JSON keys the same, not `box_left` and so on.
  Rationale: a name says what the value holds and in what unit, so a reader never has to look at the parent to know a box is in fractions of the picture.
  Date/Author: 2026-10-17, Claude.
- Decision: merging keeps the first group's contact, or takes the first other group's contact when the first has none; contacts renamed in the address book are not pushed into the search document of the photos their groups are in until those photos change.
  Rationale: a merge should not lose a name the person gave, and the address book has no way to tell the photo library that a card changed.
  Date/Author: 2026-10-17, Claude.

- Decision: migration 0224 is new, and 0221 is not changed in place.
  Rationale: the photo migrations have been applied on a dev server already, and a migration changed after it ran there would never apply there.
  Date/Author: 2026-10-17, Claude.
- Decision: a circle's album is not shared further, and a share exists only on a person's album.
  Rationale: every member of the circle reads and adds to it already, so a share would only hand it to someone outside the circle; the people and circles a share may name are defined by the owner's circles, which a circle does not have.
  Date/Author: 2026-10-17, Claude.
- Decision: a reader who is not the owner of a photo sees its place only through a share that includes places; the owner of an album does not see the places of photos others added to it, and the members of a circle's album see only their own photos' places.
  Rationale: where a photo was taken is the choice of the photo's owner, who gave it to an album for the people the album reaches to see, not for its owner to learn. A share's `is_location_included` is the owner's yes; a circle's album has no share to say it.
  Date/Author: 2026-10-17, Claude.
- Decision: saving a shared photo without places stores the preview the reader could see as a file of the reader's own (one new blob), and only a share that includes places is copied over the same blob.
  Rationale: the original's bytes carry the place in their metadata, and a copy of them in the reader's library would hand it over through the reader's own `files` route. The brief's no-new-blob copy is exactly the included case.
  Date/Author: 2026-10-17, Claude.
- Decision: a reader is given the large preview (or the video preview) where they ask for a file they may not have, named as a picture or a video, rather than a refusal; a Live Photo's video, which has no preview to stand for it, is not found.
  Rationale: a viewer that downloads or plays "the original" keeps working, and nothing in a preview carries a place: the large preview is encoded afresh from the decoded pixels and the video preview is made with `-map_metadata -1`.
  Date/Author: 2026-10-17, Claude.
- Decision: the sweep of photos that people no longer reach is state-based (ask `GetPhotoAlbumAccess` of each person who added to an album that changed) and needs no circle named, so it is one call in `endSharesOutsideCircles` and not a second beside `leaveCircleChats`.
  Rationale: it gives the same answer for a person leaving, an owner leaving, a circle deleted and a share taken back, and after a circle is deleted there is no circle row to name.
  Date/Author: 2026-10-17, Claude.
- Decision: the agent's `album-share` and `album-unshare` are risk class granting, as the calendar's share is, and not outward.
  Rationale: they change who may see the person's photos and send nothing out; granting is the class that asks unless the person allowed it.
  Date/Author: 2026-10-17, Claude.
- Decision: the ten minutes between notifications of added photos are kept in memory by each server instance.
  Rationale: a restart costs one extra notification at most, and a table for it would be a migration for a convenience.
  Date/Author: 2026-10-17, Claude.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

This section describes the repository as it is before this plan, for a reader who knows nothing about it. Read `AGENTS.md` and `CONTRIBUTING.md` at the repository root first; they are short.

**The program.** TeaNode is one Go server binary (`cmd/teanode-server`, built with `CGO_ENABLED=0`, so it has no C dependencies) and one client binary (`cmd/teanode`, the command line). The server embeds the dashboard (React and TypeScript under `web/`) and serves a GraphQL API at `/api/v1/graphql`; the dashboard, the command line, the agent's tools and the iPhone app (`ios/`) all go through that API. Configuration an operator sets lives in PostgreSQL as one YAML document per section (table `configuration`), with its shape in `internal/config`.

**GraphQL by reflection.** Operations are Go methods. `internal/api/v1api/apigraph/schema.go` lists interfaces such as `RecordingQuery` and `RecordingMutation`; each method takes `context.Context` and one arguments struct, and returns a value and an error. `internal/util/graphapi` builds the schema from them, field names from the `json` tags, and a `graphapi:"nullable"` tag makes a field optional. A resolver starts with `self.requirePermission(ctx, models.PermissionX)` and works in `self.transaction(ctx)`, a database transaction for the request, and turns errors into API errors with `translateError`. The closest example of a feature with big files is recordings: `internal/api/v1api/apigraph/recording.go` (operations) and `recording_media.go` (plain HTTP handlers for the bytes, registered in `apigraph.go` `AddRoutes` with paths from `internal/api/path.go`).

**The database.** PostgreSQL through GORM, in `internal/db`. Each table has its file (`database_recording.go`); `db.Transaction` is the interface of all queries. Migrations are numbered SQL files in `internal/db/migrations/`, each with a `.reverse.sql` (required: the runner panics without one; see `docs/coding/database-migrations.md`). Identifiers are ULIDs in `varchar(32)`. Column names say what they hold and in what unit (`original_size_bytes`, `timeline_duration_ms`), enum columns are named after their enum (`recording_upload_state`), booleans start with `is_` or `has_`. Models shared across packages are in `internal/models`.

**The blob store.** `internal/storage` keeps bytes outside the database. `storage.Storage` is opened once at start (`internal/cmd/server/run.go`), in `local` mode (a directory, with an optional S3 copy) or `shared` mode (S3 only, for example against a self-hosted MinIO). Its `StreamFiles` interface (`internal/storage/stream.go`) is what large files use: `PutFileStream(ctx, id, io.ReadSeeker, sizeBytes)` writes atomically, `OpenFileRange(ctx, id, offsetBytes, sizeBytes)` reads a range, `DeleteFileEverywhere(ctx, id)` deletes and reports failure so a cleanup can retry. An identifier must be at least two characters and contain no `/`, `\`, `.` or NUL (`identifier.go`). The S3 implementation (`s3.go`) writes each object with one `PutObject`, which S3 limits to 5 GB.

**Recordings, the closest feature.** A recording's original audio or video goes up in 8 MiB parts (`PUT /recordings/media/{id}/parts/{n}` with `X-Recording-Part-SHA256`), each stored as its own blob with a receipt row (`recording_media_part`), then `POST .../complete` re-reads and checks the whole file's SHA-256 (`internal/recording/media.go`). Quota is checked in the database when the upload is declared (`CreateRecordingMedia` in `internal/db/database_recording.go`, against `agent.limits.maxRecordingAccountBytes`). A periodic job, `recording:cleanup`, made with `internal/util/periodic` in `run.go`, deletes blobs of deleted recordings one per transaction so a restart resumes. Originals are served with Range support by `recordingMediaView`. The iPhone uploads them with a background `URLSession` (`ios/TeaNode/Sources/RecordingUploads.swift`).

**Media tools.** The runtime image (`deploy/Dockerfile`) copies `ffmpeg`, `ffprobe`, `pdftotext` and `pdftoppm` with their libraries from a Debian trixie stage (`media-readers`) into a distroless image. Go code runs them as child processes (`internal/recording/decode.go`). Debian trixie's ffmpeg is 7.1; its command line decodes only the first tile of a tiled HEIC picture, which is how an iPhone stores every photo, so this plan adds libheif's `heif-dec` for HEIC and HEIF. The Go code decodes only PNG, JPEG and GIF with the standard library; `internal/avatar` and `internal/llm/picture.go` each read a JPEG's orientation from its EXIF block themselves. There is no EXIF library in `go.mod`, and the project adds dependencies rarely.

**The model sees pictures.** `internal/llm` sends `ContentPart{Type: "image"}` to every provider, shrinking pictures to a budget in `picture.go`. The agent already looks at pictures from conversation attachments, `share_file`'s `look`, and memory's `look` (`internal/agent/tools/memory/look.go`). Embeddings are text only: a vector is stored as `real[]` with its model's name, and ranking is cosine similarity in Go over a bounded set of candidates (`internal/db/database_vector.go`), with a pgvector index added when the extension is present.

**Hosted services and permissions.** An operator chooses which services the server hosts in the `services` configuration section (`internal/config/services.go`: `MailRouting`, `Mailboxes`, `Calendar`, `Reminders`, `Contacts`, each a `HostedService` with `IsEnabled`), chosen in the setup wizard (`web/src/pages/serviceSetup.tsx`, operation `UpdateServices`) and reported to clients as `isCalendarEnabled` and so on. A person needs a permission to use one (`internal/models/permission.go`: `calendar:use`, `contacts:use`); migration `0051_calendar.sql` shows how a new permission is given to the built-in Administrator, Operator and Member roles.

**Device passwords.** `sync_credential` (migration 0163) is a password for one device that grants only the collections it names (`has_calendar_access`, `has_reminders_access`, `has_contacts_access`) and cannot sign in to mail or the dashboard. CalDAV and CardDAV accept it with HTTP Basic authentication (`internal/dav`).

**The agent's tools.** `internal/agent/tools/<name>/` holds one tool per directory, registered in `init` with `tools.Register` and imported by `internal/agent/tools/all`. A tool declares its operations as an enum parameter, a `RiskOf` function (read, write, outward, destructive; destructive raises a card the person must approve), and runs GraphQL operations through `tools.MustRun(ctx).Operations()`, with the operation documents in `internal/client`. `internal/agent/tools/recording/recording.go` and `internal/agent/tools/note/` are examples. Parity tests compare command line and tool names with operation names.

**The iPhone app.** SwiftUI, iOS 26.0 and later, generated by XcodeGen from `ios/project.yml`. Screens are `AppSection` cases in `ios/TeaNode/Sources/AppShell.swift`; GraphQL documents and models are in the `TeaNodeKit` package (`ios/TeaNodeKit/Sources/TeaNodeKit/`), which builds and tests on Linux. `ios/DESIGN.md` is the design contract (system components, Liquid Glass only on controls, semantic colors, localized strings in English, Simplified Chinese and Japanese). The app uses `PhotosPicker` only; it has no photo library access, no `NSPhotoLibraryUsageDescription`, no BGTaskScheduler, and no on-disk image cache (`AppModel.picture` keeps pictures in memory). Signing is automatic in CI (`.github/workflows/ios.yml`); a new extension or entitlement must first be enabled on the App ID at developer.apple.com.

**Circles (in flight on another branch).** Pull request #528, branch `design/circles-and-chats`, is adding `circle` (a named set of accounts of kind `family`, `friends` or `colleagues`), `circle_member` (`member_role` `admin` or `member`), `circle_invitation`, `SharesCircle(userId, otherUserId)` and `CircleUserIDs(userId)` in `internal/db/database_circle.go`, `requireCircleMember` and `requireCircleAdmin` in `apigraph/authorize.go`, the query `ListCirclePeople`, and avatars (`avatarUrl` and `avatarColorIndex` on a user, `web/src/components/avatar.tsx`, the iPhone `AvatarView`). Its shares are rows with `grantee_user_id` or `grantee_circle_id` (exactly one, checked with `num_nonnulls`) and a `share_level`. A direct share is deleted in the transaction in which the two people stop sharing any circle.

**Words used in this plan.** A **photo** is one item in a person's library: a picture, a video, or a Live Photo (a picture with a short video). A **photo file** is one of the files a photo is made of: its `original`, an `edited` version made on a phone, or a Live Photo's `liveVideo`. A **blob** is bytes in the blob store. A **preview** is a smaller picture or video made from a photo's file for showing it: `small` (at most 400 pixels on the long side), `large` (at most 2048) and, for a video, `video` (H.264 at most 1280, which every browser plays). The **trash** holds photos the person deleted, for 30 days. A **description** is what the model wrote about a picture, with the words it could read in it. A **face** is one face found in a picture, with its box and vector (128 numbers; two faces of the same person have vectors pointing the same way). A **face group** is the faces the server believes are one person; it is named by linking it to a **contact**. An **album** is a named set of photos. A **share level** is `view` or `contribute`. **Backup** is the iPhone sending its library to TeaNode.

## Plan of Work

The work is nine milestones. Each ends with something a person can see working. Milestones 2 to 7 each ship a capability on every surface together (dashboard, command line, agent tool, iPhone app), through the same GraphQL operations with the same names and the same refusals, as `CONTRIBUTING.md` requires.

### Milestone 0: prototypes

Three things could sink the design and are checked first, in a scratch directory, not in the tree.

The face reader. Build a C++ program against Debian trixie's OpenCV 4.10 (`libopencv-objdetect-dev`, `libopencv-dnn-dev`, `libopencv-imgcodecs-dev`, `libopencv-imgproc-dev`, `libopencv-core-dev`) that reads one encoded picture on standard input, finds faces with `cv::FaceDetectorYN` (model `face_detection_yunet_2023mar.onnx` from the OpenCV model zoo, MIT license), aligns each with `cv::FaceRecognizerSF::alignCrop`, computes its 128-number vector with `FaceRecognizerSF::feature` (model `face_recognition_sface_2021dec.onnx`, Apache 2.0), and prints JSON. Run it in a `debian:trixie-slim` container on two public sample pictures of the same person and one of another. Accept when the same person's vectors have cosine similarity above 0.363 (SFace's published threshold for "same person") and the other's below, each picture takes under a second on one core, and the libraries the program links add an acceptable amount to the image (measure with `ldd` and `du`).

HEIC previews. Run the image's ffmpeg on an iPhone HEIC picture (portrait orientation, HDR) and a JPEG with an EXIF orientation of 6: `ffmpeg -i in.heic -vf scale=... -frames:v 1 out.png`. Accept when the HEIC comes out upright and the JPEG's handling is known (if ffmpeg ignores the JPEG's EXIF orientation, Go applies it, as `internal/avatar` does).

A 104 through the middleware. In a Go test, wrap a handler that writes `WriteHeader(104)` with a `Location` header and then `201` in the server's middleware chain (`internal/web`), serve it with `httptest`, and use `httptrace.ClientTrace.Got1xxResponse` to see whether the client receives the 104. If compression or the access log breaks it, the photo upload routes are registered outside them.

Record the results in Surprises & Discoveries.

### Milestone 1: the library on the server

At the end of this milestone a photo can be uploaded with `curl` through the resumable endpoint, is read for its date, place and size, gets previews, and can be listed, read, favorited, trashed, restored and deleted through GraphQL. Nothing shows it yet.

**Service and permission.** Add `models.ServicePhotos` and `Photos HostedService` to `internal/config/services.go` (with `IsEnabled`, `Defaults` and the running intersection like `Contacts`), `isPhotosEnabled` beside `isContactsEnabled` in the capabilities and `UpdateServices`, and the permission `PermissionPhotosUse Permission = "photos:use"` with a comment ("keep a photo library, and back up a device to it"). Every photo operation and handler starts by checking the service is enabled (the same helper the calendar uses) and the permission.

**Limits.** Add a `Photos` section to `internal/config` (`config.go`, beside `Storage`): `MaxFileBytes` (default 8 GiB), `MaxAccountBytes` (default 0, meaning no limit beyond the disk), `TrashRetention` (default 30 days), and `IsFaceRecognitionEnabled` (default true; the operator's switch). Follow the shape and validation of the existing sections, including how they are described to the dashboard's server settings.

**Tables.** Migration `0220_photo.sql` (and its reverse) creates:

    photo: id varchar(32) pk; user_id references "user" on delete cascade, not null;
      created_at, updated_at timestamptz not null;
      client_photo_id varchar(255) not null   -- the uploader's stable name for it
      photo_kind varchar(32) not null          -- picture | video | livePhoto
      photo_source varchar(32) not null        -- iphone | dashboard | commandLine | conversation | mail | computer
      taken_at timestamptz null; taken_at_offset_minutes integer null;
      width_pixels integer not null default 0; height_pixels integer not null default 0;
      duration_ms bigint not null default 0;
      latitude double precision null; longitude double precision null;
      place_name varchar(255) not null default '';
      camera_make varchar(128) not null default ''; camera_model varchar(128) not null default '';
      original_filename varchar(255) not null default '';
      is_favorite boolean not null default false; is_screenshot boolean not null default false;
      photo_processing_state varchar(32) not null  -- waiting | ready | failed
      processing_attempt_count integer not null default 0; processing_error_message text not null default '';
      processing_claimed_at timestamptz null; processing_not_before timestamptz null;  -- a failed photo waits
      trashed_at timestamptz null;
      unique (user_id, client_photo_id)
    indexes: (user_id, taken_at desc, id desc) where trashed_at is null; (user_id, trashed_at) where trashed_at is not null;
      (photo_processing_state, created_at) where photo_processing_state = 'waiting'

    photo_blob: blob_sha256 varchar(64) pk; blob_size_bytes bigint not null; created_at timestamptz not null;
      deleting_at timestamptz null

    photo_file: photo_id references photo on delete cascade; photo_file_role varchar(32) not null  -- original | edited | liveVideo
      blob_sha256 references photo_blob; content_type varchar(128); file_size_bytes bigint; original_filename varchar(255);
      created_at; primary key (photo_id, photo_file_role)

    photo_upload: id varchar(32) pk; user_id references "user" on delete cascade; created_at, updated_at;
      photo_upload_state varchar(32)   -- receiving | complete | failed
      declared_size_bytes bigint null; received_size_bytes bigint not null default 0; part_count integer not null default 0;
      upload_headers jsonb not null    -- what the request said the file is (see below)
      photo_id varchar(32) null        -- set when complete

    photo_upload_part: photo_upload_id references photo_upload on delete cascade; part_index integer;
      part_size_bytes bigint; part_sha256 varchar(64); stored_at; primary key (photo_upload_id, part_index)

    photo_preview: photo_id references photo on delete cascade; preview_size varchar(32)  -- small | large | video
      content_type; preview_size_bytes bigint; width_pixels; height_pixels; created_at; primary key (photo_id, preview_size)

It also adds `has_photos_access boolean not null default false` to `sync_credential`, and gives `photos:use` to the Administrator, Operator and Member roles the way `0051_calendar.sql` does. Blob identifiers in the store are `photo-blob-<sha256>`, `photo-upload-<uploadId>-<partIndex>` and `photo-preview-<photoId>-<previewSize>`.

Models go in `internal/models/photo.go` (`Photo`, `PhotoFile`, `PhotoUpload`, `PhotoPreview`, and the enums `PhotoKind`, `PhotoSource`, `PhotoFileRole`, `PhotoProcessingState`, `PhotoUploadState`, `PhotoPreviewSize` with their values as constants, following `internal/models/recording.go`). Queries go in `internal/db/database_photo.go` and `database_photo_upload.go`, all scoped by the owner's `user_id`.

**The upload endpoint.** `internal/photo/upload.go` implements the draft's server side over `storage.StreamFiles`, and `apigraph/photo_upload.go` mounts it at `/api/v1/photos/uploads` (collection) and `/api/v1/photos/uploads/{photoUploadId}` (one upload). Milestone 0 showed the 104 passes the existing middleware, so nothing special is needed in the chain.

- `OPTIONS` on the collection answers 200 with `Upload-Limit: max-size=<MaxFileBytes>` (and the older bare-number form Apple's article shows, `Upload-Limit: <bytes>`, decided by the client's `Upload-Draft-Interop-Version`).
- `POST` on the collection creates a `photo_upload` from the request's headers, writes `104` with `Location` (and `Upload-Draft-Interop-Version` echoed), then reads the body in pieces of at most 8 MiB, storing each as a part with a receipt row as soon as it is whole. When the request says the upload is complete (`Upload-Complete: ?1`, or the older `Upload-Incomplete: ?0`, or no such header at all, which is a plain upload) and the body ends, it completes the upload and answers 201 with `Location`, `Upload-Offset`, and `X-Photo-ID`. If the body ends early, it answers with the offset stored so far.
- `HEAD` on an upload answers `Upload-Offset` (bytes in stored parts) and `Upload-Complete`. A part that was being written when the connection dropped is not counted, so the offset is always durable bytes.
- `PATCH` on an upload checks `Upload-Offset` equals the stored offset (409 with the right offset otherwise), appends, and completes as `POST` does.
- `DELETE` marks it failed; the cleanup job deletes its parts.

What the file is comes in headers, all optional except the first two: `X-Photo-Client-ID` (the uploader's stable name for the photo; the dashboard and command line use the SHA-256 of the original), `X-Photo-File-Role` (`original` by default), `X-Photo-Kind`, `X-Photo-Filename`, `Content-Type`, `X-Photo-Taken-At` (RFC 3339; with an offset it is the instant and the wall clock where the photo was taken, and both win over the file's EXIF; ending in `Z` it is the instant alone, because a phone knows when and not in which zone, and the offset is then taken from the file's `OffsetTimeOriginal` when the file's own date is the same moment, else from the owner's time zone at that instant; an uploader that knows the photo was taken in UTC writes `+00:00`), `X-Photo-Latitude`, `X-Photo-Longitude`, `X-Photo-Is-Favorite`, `X-Photo-Is-Screenshot`, `X-Photo-Source`, and `X-Photo-Declared-SHA256` when the client knows it. Header values are bounded in length and validated; a bad one is a 400 naming the header.

Authentication: the usual session (bearer token or cookie, through the existing authenticator) or HTTP Basic with a `sync_credential` that has `has_photos_access`. Write the Basic check beside DAV's (`internal/dav`), reusing its password verification.

Completing an upload, in `internal/photo/complete.go`: read every part in order computing the SHA-256 and size; if `X-Photo-Declared-SHA256` was given and differs, fail the upload. In one transaction: insert `photo_blob` if it is not there (if it is, the parts are just deleted afterwards: the bytes are already stored), find or create the `photo` by `(user_id, client_photo_id)`, check the quota (the sum of `file_size_bytes` of the person's photo files plus this one, against `MaxAccountBytes`, refusing with a message that names the limit), upsert its `photo_file` for the role, set `photo_processing_state` to `waiting` when the role is `original` or `edited`, and mark the upload complete with its `photo_id`. If the blob was new, assemble it outside the transaction first: `PutFileStream` from a reader that reads the parts in order (`OpenFileRange` on each), then insert the row in a transaction of its own and at once, before the transaction that records the file, which can fail on the quota or the database: the bytes are then an unreferenced blob that cleanup deletes when it is an hour old, and not bytes nothing names. A part that no longer matches its receipt, or is gone, ends the upload (`ErrPartDamaged`, answered 500 with "start the upload again"). The quota counts the bytes of the account's unfinished uploads as well, when a size is declared and as each part arrives; starting an upload for the same client name and role ends the earlier try, and an upload may have at most `2 * maxFileBytes / 8 MiB + 16` parts. A photo that is in the trash stays in the trash when the same file arrives again.

**S3 in parts.** In `internal/storage/s3.go`, make `PutFileStream` use S3's multipart upload (`CreateMultipartUpload`, `UploadPart` in 64 MiB pieces, `CompleteMultipartUpload`, `AbortMultipartUpload` on any failure) when the size is over 256 MiB, so files over 5 GB can be stored. Test against a fake S3 handler like the existing S3 tests do.

**Processing.** `internal/photo/process.go` runs from a periodic job `photo:process` (every 15 seconds, in `run.go` beside `recording:cleanup`). Each pass runs two workers, each claiming one waiting photo at a time with `FOR UPDATE SKIP LOCKED` (setting `processing_claimed_at` and counting an attempt, so several instances share work and a claim older than 10 minutes is taken again), and for each:

1. Reads metadata from the file it previews (the `edited` file if there is one, else the `original`). Pictures: a small reader of EXIF in `internal/photo/exif.go` that finds the EXIF block in a JPEG (the APP1 segment) or in a HEIC/HEIF file (the `Exif` item located through the `meta`, `iinf` and `iloc` boxes), and reads `DateTimeOriginal` with `OffsetTimeOriginal`, the GPS latitude and longitude, `Make`, `Model`, orientation and pixel size. Videos: `ffprobe -show_format -show_streams -of json` for duration, size, rotation, `creation_time` and the QuickTime location tag (`com.apple.quicktime.location.ISO6709`). Values the uploader sent in headers win over what the file says, because the phone's library knows the date and place the person edited.
2. Names the place: `internal/photo/place.go` finds the nearest city within 25 km in a table embedded in the binary (`internal/photo/places.tsv.gz`, made by `scripts/make-places.sh` from GeoNames' `cities15000.txt` and `countryInfo.txt`, keeping name, country name and coordinates; GeoNames is CC BY 4.0, credited in `docs/configuration.md` and the dashboard's about text). It answers "Alpha, Testland".
3. Makes previews: `small` and `large` as JPEG (quality 82), EXIF stripped, the right way up; for a video also a poster as `small`/`large` from the first second, and `video`, H.264 and AAC in MP4 at most 1280 pixels, `-movflags +faststart`. HEIC and HEIF go through `heif-dec` to a JPEG in a private directory under `photo-work/` (it applies the tiles and the rotation), then Go; JPEG and PNG are decoded in Go (at most 64 megapixels, and 128 megapixels of decoded pictures across the workers), turned the right way up by their EXIF orientation while they are scaled, and scaled with an area-average scaler written for this (the standard library has none); video goes through ffmpeg. A video preview is made only for videos up to 30 minutes; longer ones play from the original. Add `libheif-examples` and `libheif-plugin-libde265` to the `media-readers` stage of `deploy/Dockerfile` and copy `heif-dec` and its libraries and plugin directory like the other readers.
4. Faces (Milestone 5).
5. Sets `ready`, or on error stores a fixed sentence for the person; a failure that is the file's keeps the attempt claiming counted and sets `processing_not_before` (1, 5, 15, then 60 minutes), a failure of the store or the database gives the attempt back, and after 5 attempts the photo is `failed` and shown as such. A panic is recorded as a failure of the file. `RetryPhotoProcessing(photoIds)` puts failed and ready photos back to waiting with a fresh count.

**Serving.** `GET /api/v1/photos/{photoId}/previews/{previewSize}` serves a preview with an ETag and `Cache-Control: private, max-age=31536000, immutable` (the URL includes `?v=<updated_at unix>` from the API, so a new preview is a new URL). Range is supported for `video`. `GET /api/v1/photos/{photoId}/files/{photoFileRole}` serves a file with Range and `Content-Disposition: attachment; filename=...`. Both answer 404 for anybody who may not read the photo (its owner, and the readers of an album that holds it: Milestone 7). Follow `recordingMediaView` for Range. A player cannot send the person's sign-in, so `PhotoPlaybackAddress(photoId, photoPreviewSize | photoFileRole)` answers an address on `/api/v1/photos/playback/{photoId}/previews/{size}` or `.../files/{role}` with `?owner=<userId>&signature=<expires>.<hex>`: the signature is an HMAC of the owner, the photo, which preview or file, and the expiry, made with the server's secret, so it opens that one thing for six hours and nothing else (a changed or expired signature, another photo, or an owner who has since lost the Photos permission is a 404). The paths are public prefixes in the middleware and the handler checks the signature; they serve with Range exactly as the signed-in paths do. Readers of a shared album are given them too, signed with the owner's id after the reader's access is checked; a file the reader may not have is addressed as the preview that stands for it.

**Cleanup.** `photo:cleanup`, every minute: delete uploads that are failed or idle for more than 7 days, with their parts (the upload is claimed and ended in a transaction, its parts are deleted with no lock held, and the row goes last); delete forever the photos trashed longer than `TrashRetention`; delete blobs no file refers to that are older than one hour (set `deleting_at`, call `DeleteFileEverywhere`, delete the row; one per transaction); delete previews of deleted photos (they cascade as rows; their blobs are deleted by name when the photo row goes, from a list kept in the same transaction, or by a sweep of `photo-preview-` names whose photo is gone).

**Operations,** in `apigraph/photo.go`, interfaces `PhotoQuery` and `PhotoMutation` added to `schema.go`:

    ListPhotos(ListPhotosArguments) ([]*models.Photo, error)
      arguments: beforePhotoId, pageSize (default 100, at most 500), isFavorite, isTrashed, photoKind, takenAfter, takenBefore,
      searchText (place, filename, camera; descriptions from Milestone 4), albumId (Milestone 3), faceGroupId (Milestone 5),
      nearLatitude, nearLongitude, nearDistanceKilometers. Ordered by taken_at (or created_at when unknown) newest first.
    ReadPhoto(PhotoArguments) (*PhotoView, error)     -- photo, files, previews with URLs, albums and faces when those exist
    ReadPhotoLibrary() (*PhotoLibraryView, error)       -- counts (photos, videos, favorites, trash), bytes used, limits,
      upload URL, isFaceRecognitionAvailable, isDescriptionAvailable, the person's face switch
    FindPhotos(FindPhotosArguments) ([]*models.Photo, error) -- by clientPhotoIds, for an uploader deciding what to skip; each photo carries `trashedAt` (set when it is in the Trash, so an uploader can tell "already in your library" from "in the Trash") and `photoFileRoles`, the roles of the files the server holds (original, edited, liveVideo), so an uploader after a reinstall sends only what is missing
    UpdatePhoto(UpdatePhotoArguments) (*models.Photo, error) -- isFavorite, takenAt (the person correcting a date)
    TrashPhotos(PhotoIdsArguments) (int, error); RestorePhotos(PhotoIdsArguments) (int, error)
    DeletePhotos(PhotoIdsArguments) (int, error)       -- forever; only photos already in the trash
    EmptyPhotoTrash() (int, error)
    SavePhotoFromAttachment(SavePhotoFromAttachmentArguments) (*models.Photo, error) -- a conversation attachment, by id

`Photo` returned to clients carries `smallPreviewUrl`, `largePreviewUrl` and `videoPreviewUrl` (empty until made), computed in the resolver like `avatarUrl`. `SavePhotoFromAttachment` copies the attachment's bytes into a blob (named by SHA-256, so no copy when it is already stored) with `client_photo_id` set to that SHA-256 and `photo_source` `conversation`.

Tests, in the package of each piece: EXIF from a JPEG and a HEIC fixture built in the test (write the boxes and the TIFF block by hand; no real photos and no real coordinates: use invented ones such as 10.0, 20.0); place lookup on a three-city table; the upload protocol end to end against `httptest` with a memory store (create, interrupt, `HEAD`, `PATCH` from the offset, complete, the 104 seen with `Got1xxResponse`, a wrong offset answered 409, a declared SHA-256 mismatch refused, the same bytes twice stored once); quota refusal; trash and restore; cleanup deleting an unreferenced blob and keeping a referenced one; the multipart S3 path against a fake server. Database tests use the container described in `docs/reference/local-development.md`.

Acceptance: on a dev server with Photos enabled,

    curl -i -X OPTIONS http://127.0.0.1:<port>/api/v1/photos/uploads -H "Authorization: Bearer <token>"
      HTTP/1.1 200 OK  Upload-Limit: max-size=8589934592
    curl -i -X POST http://127.0.0.1:<port>/api/v1/photos/uploads -H "Authorization: Bearer <token>" \
      -H "X-Photo-Client-ID: example-1" -H "Content-Type: image/jpeg" --data-binary @picture.jpg
      HTTP/1.1 104 ...  Location: .../api/v1/photos/uploads/01J...
      HTTP/1.1 201 Created  X-Photo-ID: 01J...
    teanode graphql '{ ListPhotos(pageSize: 5) { id takenAt placeName photoProcessingState smallPreviewUrl } }'

shows the photo `ready` within half a minute, and its small preview opens in a browser.

### Milestone 2: the library on every surface

At the end of this milestone a person can switch Photos on during setup, upload and browse on the dashboard, from the command line and from the agent, and browse on the iPhone and save to the phone's Photos.

**Setup.** `serviceSetup.tsx` gets a Photos choice beside Calendar and Contacts, with one line saying what it is ("Keep your photos and videos, and back up your iPhone"). The getting started guide (`docs/getting-started.md`) and the welcome page mention it in their list of services. The server settings page shows the Photos limits. The account's device passwords (dashboard, iPhone, command line) get a "Photos" access choice beside Calendar, Reminders and Contacts.

**Dashboard.** A Photos item in the sidebar (shown when `isPhotosEnabled` and the person has `photos:use`, placed with Calendar and Contacts; icon from the existing icon set), and `web/src/pages/photos/` with:

- `library.tsx`: the timeline, newest first, grouped under day headings ("Saturday, July 12, 2025", in the person's locale and time zone), a grid of square small previews filling the width (about 120 to 180 pixels a cell), 200 photos a page with `Pager` in the foot and the page in the address. Videos show their length on the cell, Live Photos a small mark, favorites a heart. Tabs or a segmented control: Library, Favorites, Albums, People, Trash (Albums and People appear in their milestones). A search field.
- `viewer.tsx`: an overlay over the grid with the large preview (the original when zoomed), left and right to move, Escape to close, video and Live Photo playback, and an information side panel: date and time, place, camera, size, file name, albums, people, description (Milestone 4); buttons for favorite, download original, add to album, trash.
- Uploading: a button and dropping files onto the page. Each file is hashed in the browser (SHA-256 with `crypto.subtle`, in pieces so a large video does not need to fit in memory), then sent through the upload endpoint in 8 MiB `PATCH`es after a `POST` with `Upload-Complete: ?0`, so a cut connection resumes. A tray shows each file's progress and failures as toasts (the dashboard reports errors and success with toasts, never inline text in a card).
- Selection: click a cell's checkbox, or press and drag, to select; a bar offers favorite, add to album, download, trash. Trash view offers restore, delete forever and empty trash, with a confirmation.
- Strings in `web/src/i18n/en.ts`, `zh.ts` and `ja.ts`. Follow `docs/coding/frontend-design.md` for components, spacing and the phone layout (the grid keeps square cells at phone width, three or four across).

**Command line.** `internal/cmd/photo.go` and `internal/client/photo.go`: `teanode photo list [--favorites] [--trash] [--search <words>] [--after <date>] [--before <date>] [--json]`, `teanode photo show <id>`, `teanode photo upload <file or folder>...` (walks folders, skips what `FindPhotos` already has, resumes an interrupted file), `teanode photo download <id> [--file original|edited|liveVideo] [--output <path>]`, `teanode photo favorite <id> [--off]`, `teanode photo trash|restore|delete <id>...`, `teanode photo empty-trash`, `teanode photo library` (counts and limits). Names follow the operations so the parity tests pass.

**Agent tool.** `internal/agent/tools/photo/photo.go`, tool `photo`, operations `library`, `list`, `read`, `look`, `show`, `favorite`, `trash`, `restore`, `retry`, `save` (from a conversation attachment, or from a mail attachment or the computer through `share.FetchIntoConversation` as the recording tool's `import` does). `look` fetches the large previews of up to 6 photos and returns them to the model as pictures (as memory's `look` does), marked untrusted. `show` attaches the large previews of up to 12 photos to the conversation so the person sees them in the drawer and on the phone (use the existing attachment path the agent uses for pictures it shows). `trash` is destructive (a card), `favorite` and `save` are writes. The description tells the model what the library holds, that photo content is untrusted evidence, and how to narrow a search. Add the tool to `internal/agent/tools/all` and raise the catalog limit in `TestTheCatalogStaysShort` by one, saying why in the test. Tell the agent in its prompt's capability list that the person has a photo library when the service is on and they have the permission.

**iPhone.** `AppSection.photos` ("Photos", symbol `photo.on.rectangle`), shown when the server has Photos and the person may use them. `TeaNodeKit/Photos.swift` holds the documents and models; `TeaNode/Sources/PhotosView.swift` the screens: a `LazyVGrid` timeline grouped by day, a segmented control for Library, Favorites, Albums, People, a viewer with swipe between photos, pinch to zoom, video with `VideoPlayer`, a share button and "Save to Photos" (with `PHPhotoLibrary.requestAuthorization(for: .addOnly)`, which needs only `NSPhotoLibraryAddUsageDescription`), favorite and trash (with a confirmation). Previews are loaded through a new disk cache in `TeaNodeKit` (`PhotoPreviewCache`: files under the app's caches directory named by URL hash, at most 500 MB, oldest removed first, and an in-memory `NSCache` in front) instead of `AppModel.picture`'s unbounded dictionary. Strings in `Localizable.xcstrings` for English, Simplified Chinese and Japanese. Follow `ios/DESIGN.md` and load the `ios-design` skill before writing a screen.

Acceptance: on a fresh dev server run through setup with Photos chosen, upload five pictures and a video from the dashboard; see them grouped by day with places; open one, move with the arrow keys, favorite it, trash one and restore it. `teanode photo list` shows the same five. Ask the agent "what did I photograph most recently?" and see it look and answer. In the simulator, open Photos, see the same grid, open a photo and save it to the simulator's Photos.

### Milestone 3: albums

At the end of this milestone a person makes, fills, renames and deletes albums on every surface, and the agent organizes photos into albums and suggests albums for trips and events.

Migration `0221_photo_album.sql`: `photo_album` (id, `user_id` references user on delete cascade, not null for now, Milestone 7 makes it nullable beside `circle_id`; `album_title varchar(255)`; `cover_photo_id` null; created_at, updated_at) and `photo_album_item` (`photo_album_id` references photo_album on delete cascade, `photo_id` references photo on delete cascade, `added_by_user_id`, `added_at`, primary key both ids; index on `photo_id`).

Operations: `ListPhotoAlbums` (each with its photo count, cover preview URL and date range), `ReadPhotoAlbum`, `CreatePhotoAlbum(albumTitle, photoIds)`, `RenamePhotoAlbum`, `DeletePhotoAlbum` (the photos stay in the library), `AddPhotosToAlbum(photoAlbumId, photoIds)`, `RemovePhotosFromAlbum`, `SetPhotoAlbumCover`, and `ListPhotos(albumId)`. `SuggestPhotoAlbums(takenAfter, takenBefore)` groups photos not yet in an album into runs: a run is broken by a gap of more than 6 hours, and a run of at least 12 photos becomes a suggestion; a suggestion has the run's dates, the place names it covers ("Alpha and Beta, Testland"), a proposed title from those, a count and up to 6 sample photo ids. Nothing is created until somebody asks.

Surfaces: the dashboard's Albums view (a grid of covers with titles and counts, an album page with the same grid and viewer, "New album" from a selection or empty, rename and delete in the album's menu, add from the viewer and the selection bar), the command line (`teanode photo album list|show|create|rename|delete|add|remove|cover|suggest`), the agent tool (`album-list`, `album-read`, `album-create`, `album-rename`, `album-delete` (destructive), `album-add`, `album-remove`, `album-suggest`), and the iPhone (Albums segment, album screen, "Add to album" in the viewer and in selection).

Acceptance: ask the agent "put last weekend's photos in an album called Picnic"; it lists by date, creates the album and adds them; the album appears on the dashboard and the phone. `teanode photo album suggest` lists a trip with its places and dates.

### Milestone 4: descriptions and search

At the end of this milestone the agent finds photos by what is in them, once they have been looked at.

Migration `0222_photo_description.sql`: on `photo`, `photo_description text not null default ''`, `description_model varchar(128) not null default ''`, `described_at timestamptz null`, and `search_document tsvector` maintained by the code on every change (place name, file name, camera, album titles, description, and the names of the people in it), with a GIN index; and `photo_vector (photo_id references photo on delete cascade, vector_model varchar(128), vector real[], primary key (photo_id, vector_model))`, following `agent_chunk_vector` and the pgvector index helper in `internal/db/database_vector.go`.

`DescribePhoto(photoId)` asks the agent's quick model, through the person's agent (so its usage and limits apply; refused with a clear message when the agent is off), with the large preview as a picture and a fixed instruction: two to four plain sentences saying what and who is in it, where it seems to be and what is happening, then any words that can be read in it, without guessing names. It stores the text, the model and the time, embeds the text with the agent's embedding model into `photo_vector`, and refreshes `search_document`. It answers the stored description at once when there is one, unless `isRedescribed` is set.

Who calls it: the agent tool's `look`, for each photo it looks at that has no description (so looking describes); `describe` explicitly; and the viewer's "Describe" button on the dashboard and the phone, beside the description in the information panel. Nothing describes photos in the background.

Search: `ListPhotos(searchText)` matches `search_document` with `websearch_to_tsquery` in the person's language configuration, and when an embedding model is configured also ranks described photos by cosine similarity to the embedded query, merging both like the knowledge search does. The agent tool's `search` operation says in its answer how many photos in the range have no description yet, so the agent knows to narrow and look.

Acceptance: look at a picture of a street sign with the agent; ask later in a new conversation "find the photo with the street sign" and see it found by its description without being looked at again.

### Milestone 5: faces and people

At the end of this milestone the People view shows face groups, the person names them from their contacts, and "photos of <name>" works on every surface.

**The reader.** `tools/faces/teanode-faces.cpp` (the Milestone 0 prototype, cleaned) and `tools/faces/README.md`. It takes the two model paths as flags (default `/usr/share/teanode/faces/`), reads one binary PPM (`P6`, maximum value 255) on standard input, at most 40 megapixels, and prints `{"widthPixels":..,"heightPixels":..,"faces":[{"boxLeft":..,"boxTop":..,"boxWidth":..,"boxHeight":..,"detectionScore":..,"faceVector":[128 numbers]}]}`. In `deploy/Dockerfile`, a `faces-reader` stage from `debian:trixie-slim` installs the OpenCV development packages and a compiler, builds it, downloads the two model files at fixed versions and checks their SHA-256, and copies the program, its libraries (the same `ldd` copy as `media-readers`) and the models into the runtime image. `make docker` builds it; nothing else needs a compiler.

**Go.** `internal/photo/faces.go` decodes the `large` preview (a JPEG the server made), writes it as a PPM to the reader's standard input (so the reader links no image decoders), and runs it with a 60 second timeout, from the processing job. When the program is missing, or the operator's `IsFaceRecognitionEnabled` is false, or the person switched faces off, it skips, and `ReadPhotoLibrary` reports `isFaceRecognitionAvailable` false with the reason.

Migration `0223_photo_face.sql`: `photo_face` (id, `photo_id` references photo on delete cascade, `user_id`, box as fractions of the picture (`box_left`, `box_top`, `box_width`, `box_height` real), `detection_score` real, `face_vector real[]`, `photo_face_group_id` null, `is_group_confirmed boolean` (the person put it there or confirmed it), created_at), `photo_face_group` (id, `user_id`, `contact_id` null, `cover_photo_face_id`, `group_vector real[]` (the normalized mean of its faces), `face_count integer`, `is_hidden boolean`, created_at, updated_at), and on `photo`, `faces_found_at timestamptz null`. On `user`, `is_photo_face_recognition_enabled boolean not null default true`.

Grouping, in `internal/photo/face_group.go`: a new face goes into the group whose `group_vector` has the highest cosine similarity with it, if that is at least 0.40 (a little above SFace's 0.363, to prefer splitting over merging), updating the group's vector and count; otherwise it starts a new group. Faces with a detection score under 0.85 or a box smaller than 3 percent of the picture's short side are kept but not grouped. Groups with fewer than 3 faces are not listed in People until named.

Operations: `ListPhotoFaceGroups` (named ones first by name, then the rest by face count, each with its contact's name and a face picture URL), `ReadPhotoFaceGroup`, `NamePhotoFaceGroup(photoFaceGroupId, contactId)` (the contact must be the person's; the dashboard and phone offer a contact picker with "New contact", which creates a contact first with the existing contact operations), `UnnamePhotoFaceGroup`, `MergePhotoFaceGroups(photoFaceGroupIds)` (into the first), `RemovePhotoFaceFromGroup(photoFaceId)` (the face starts its own confirmed group), `HidePhotoFaceGroup`, `SetPhotoFaceRecognition(isEnabled)` (off deletes every face and group of the person, in one transaction; on queues their photos to be read again), and `ListPhotos(faceGroupId)` and `ListPhotos(contactId)`. A face picture is served by `GET /api/v1/photos/faces/{photoFaceId}/picture`, cropped from the large preview with some margin, scaled to 256 pixels, cached with an ETag. When two named groups would merge into one contact, they simply both point at it and `ListPhotos(contactId)` returns both.

Surfaces: People on the dashboard (a grid of round face pictures with names or "Name this person", a person's page with their photos, "Name", "Merge with...", "This is not <name>" on a face in the viewer's people list), the phone (People segment, the same actions), the command line (`teanode photo people list|show|name|unname|merge|hide`, `teanode photo faces on|off`), and the agent tool (`people`, `person-photos`, and `person-name` only with a contact the person chose: the tool refuses to guess a name from a face, and its description says so). The person's account settings get "Find faces in my photos", explaining that turning it off deletes the faces.

Acceptance: upload a few pictures of two people (made-up test pictures from a generator, or public sample pictures that are not committed), see two groups in People, name one by choosing a contact, ask the agent "show me photos of <that contact>" and see them.

### Milestone 6: iPhone backup

At the end of this milestone an iPhone backs up its whole library to TeaNode, the first time with the app open and a progress bar, then by itself.

**What the app knows.** `TeaNodeKit/PhotoBackup.swift`: a small store in the app group (`<AppGroup>/PhotoBackup/index.json`, written atomically) keyed by the phone's `localIdentifier`, recording for each photo whether its files are on the server, plus the saved `PHPersistentChangeToken` (archived with `NSKeyedArchiver`). It is shared with the extension. `client_photo_id` is `PHPhotoLibrary.cloudIdentifierMappings(forLocalIdentifiers:)`'s identifier when there is one, else `"local:" + localIdentifier`.

**Turning it on.** Settings, "Back up this iPhone": a switch, the count backed up and waiting, "Use mobile data" (off by default), and what is happening now. Turning it on asks for full library access (`requestAuthorization(for: .readWrite)`; adds `NSPhotoLibraryUsageDescription` to `project.yml`), creates a device password for itself with Photos access only (`has_photos_access`) through the existing device password operation, keeps it in the keychain, and starts the first backup.

**The first backup and catching up, in the app.** `PhotoBackupEngine` lists the library (all assets, oldest first) or, after the first pass, the changes since the saved token (`fetchPersistentChanges(since:)`; on an expired token, list everything again and skip what the index or `FindPhotos` already has). For each photo it exports its resources (`PHAssetResourceManager.writeData(for:toFile:)`, which downloads from iCloud when needed) to a temporary file in the app group, and queues the upload to the endpoint on a background `URLSession` (`background("photo-backup")`, `isDiscretionary` false, `allowsCellularAccess` from the setting), with the photo's date, place, favorite and screenshot flags in the headers and the device password in `Authorization`. The original, the edited version (`.fullSizePhoto` or `.fullSizeVideo` when the asset was edited) and a Live Photo's `.pairedVideo` are each one upload. Completion handlers update the index and delete the temporary file; `AppDelegate`'s `handleEventsForBackgroundURLSession` resumes the session, as recordings do. At most 4 uploads and 2 GB of temporary files are in flight. The first backup runs inside a `BGContinuedProcessingTask` that the person starts with "Back up now" (the system shows its progress and lets it run with the app in the background); after that, a `BGProcessingTask` (`com.teanode.app.photo-backup`, requiring power and network) catches up overnight, and the app catches up whenever it comes to the foreground. Add the identifiers to `BGTaskSchedulerPermittedIdentifiers` and `processing` to the background modes in `project.yml`.

**The Photos background upload extension**, iOS 26.1 and later, only after a device test shows it can reach a server whose address is not the one in `BackgroundUploadURLBase` (see Surprises). A target `TeaNodePhotoUpload` (extension point `com.apple.photos.background-upload`, app group and keychain group) implements `processJobs()`: acknowledge finished jobs (recording them in the index from the `X-Photo-ID` response header), retry failed ones with a fresh destination after a 401, and create jobs for photos the index has not sent, each with a `URLRequest` to the upload endpoint with the same headers, keeping the queue full up to the job limit and returning `.processing` on `limitExceeded`. The app enables it with `setUploadJobExtensionEnabled(true)` when backup is on and disables it when backup is off or the person signs out. If the device test fails, the extension is left out, recorded in the Decision Log, and the in-app engine with `BGProcessingTask` is how backup works.

**Server side.** Nothing new beyond Milestone 1, except `X-Photo-Source: iphone` and the device password check, and showing on the dashboard's device password list which password backs up which phone.

Acceptance, on a real iPhone with a few hundred photos: turn backup on, tap "Back up now", lock the phone, and see the dashboard's count rise to the phone's count; take a new photo and see it arrive overnight or when the app is opened; delete a photo on the phone and see it still on TeaNode.

### Milestone 7: albums shared in circles

Start by rebasing this branch onto the circles branch once it has pushed circles and invitations (the circles session sends a message), and renumber this plan's migrations to follow its last one.

Migration `NNNN_photo_album_share.sql`: `photo_album.user_id` becomes nullable, `photo_album.circle_id` references circle on delete cascade, with `check (num_nonnulls(user_id, circle_id) = 1)`; `photo_album_share` (id, `photo_album_id` references photo_album on delete cascade, `grantee_user_id` references user on delete cascade, `grantee_circle_id` references circle on delete cascade, `check (num_nonnulls(grantee_user_id, grantee_circle_id) = 1)`, `share_level varchar(32)` (`view` or `contribute`), `is_location_included boolean not null default false`, `created_by_user_id`, created_at; unique on album and grantee).

Authorization, in `apigraph/authorize.go`: `requirePhotoReader(ctx, photoId)` lets in the owner, and anybody an album containing the photo is shared with (directly, or through a circle they are a member of) or a member of the circle that owns the album; `requirePhotoAlbumContributor` the owner, a `contribute` grantee, or any member of an owning circle; renaming, deleting and resharing an album need its owner or, for a circle album, a circle admin. A reader of somebody else's photo sees no latitude, longitude or place name, and gets the large preview instead of the original file, unless the share has `is_location_included`. Faces in a shared photo show the owner's names, and are never found for the reader.

Operations: `SharePhotoAlbum(photoAlbumId, granteeUserId | granteeCircleId, shareLevel, isLocationIncluded)` (the grantee must share a circle with the owner; pickers use `ListCirclePeople` and the person's circles), `UpdatePhotoAlbumShare`, `RemovePhotoAlbumShare`, `ListPhotoAlbumShares`, `CreatePhotoAlbum(circleId)` for a circle album, and `ListPhotoAlbums` returning the person's own, their circles', and those shared with them, each with `ownerUserId` or `circleId`, owner name and avatar, and the caller's `shareLevel`. `SavePhotoToLibrary(photoId)` makes the reader a photo of their own over the same blob (and the same file rows), with `photo_source` `shared`.

Leaving: in the circles code's transaction that removes somebody from a circle, also delete the `photo_album_item` rows they added to that circle's albums and to albums shared through that circle, and the direct shares between two people who no longer share any circle (the circles code already deletes its own direct shares there; add photos beside it).

Notifications: when photos are added to an album shared with others, each other reader with the app gets one notification per album at most every 10 minutes ("Ana added 6 photos to Family"), through the existing phone notification path.

Surfaces: the dashboard (a Share button on an album, a sheet choosing a circle or a person with avatars, the level and "Include where photos were taken"; "Shared with you" and circle albums in Albums, marked with the owner's avatar; "Add photos" on an album one contributes to; "Save to my library" in the viewer of a shared photo), the phone (the same), the command line (`teanode photo album share|unshare|shares`, `teanode photo album create --circle <id>`), and the agent tool (`album-share` and `album-unshare`, outward risk so they ask the person unless the person allowed it; and shared albums in `album-list`, `list` and `search`, where the reader's agent reads them in place).

Acceptance: on the dev server with two accounts in one family circle, share an album at `view`; the other account sees it under "Shared with you", without places, and cannot add; change to `contribute`, add a photo as the other account; remove the other account from the circle and see the share and their photo gone from the album; make a circle album and see both accounts add to it.

### Milestone 8: getting started, documentation, review, walk-through, deploy

Documentation: `docs/subsystems/photos.md` (how a photo flows: upload, processing, previews, faces, descriptions, sharing, cleanup), `docs/reference/command-line.md` (the `photo` commands), `docs/configuration.md` (the `photos` section and the GeoNames credit), `docs/reference/deployment.md` (the blob store holds the library: back up the bucket or directory as well as the database; size the disk), `docs/getting-started.md` (Photos as a service, backup from the iPhone), and a changelog entry in the pull request description's Changelog block (not in `CHANGELOG.md`; see the pull request template).

Review: read the whole diff as a reviewer, or have a reviewer agent read it, fix every finding, and repeat until a pass finds nothing worth fixing. Check naming (`CONTRIBUTING.md`'s Naming, one word per idea: photo, photo file, preview, album, face group, share level, trash, description), comments, tests, and that nothing real (names, places, coordinates, hosts) appears in tests, fixtures, comments or docs.

Walk-through: on a running dev server, in Chrome, every new screen at desktop and phone widths, light and dark, in English and one other language: library, viewer, upload, albums, people, trash, sharing, setup, settings. Run the `ux-review` skill on each. In the iPhone simulator (through CI screenshots), the Photos screens in light, dark and the largest text size.

Deploy to the production server as the maintainer's deploy notes describe (back up the database first, since this adds migrations; check the build includes every migration already applied there; confirm the version inside the container), with Photos enabled in the services, and check one upload and one preview there.

## Concrete Steps

All commands run from the repository root unless they say otherwise.

Build and test the server:

    make build
    go test ./internal/photo/... ./internal/storage/...
    make test          # starts a PostgreSQL container; needs Docker
    make lint-ci       # what CI runs; run its parts separately to see each failure

Run a dev server: see `docs/reference/local-development.md`. Use a database and ports of its own (another session's dev server may be on the defaults), and `TEANODE_PROFILE=local` for the command line, because the default profile points at production.

Build the face reader alone while working on it:

    docker build --target faces-reader -t teanode-faces-reader -f deploy/Dockerfile .
    docker run --rm -i teanode-faces-reader /usr/local/bin/teanode-faces < sample.jpg

The web dashboard:

    cd web && npm ci && npm run build && npx tsc --noEmit && npx vitest run

The iPhone package tests (Linux):

    cd ios/TeaNodeKit && swift test

## Validation and Acceptance

Each milestone's acceptance is written in its section. The whole plan is accepted when, on a server with Photos switched on:

1. A person uploads a folder with `teanode photo upload ~/Pictures/sample`, interrupts it, runs it again, and every picture and video appears once in the dashboard's timeline with its date and place.
2. The same person's iPhone backs up its library with the app closed; deleting on the phone leaves TeaNode's copy.
3. The agent answers "find the photos from my trip to <a city in the library> and make an album", "show me photos of <a named contact>", and "which of these has the receipt?" (looking, describing, and finding it again later by its description).
4. An album shared with a family circle at `contribute` lets another member add a photo, and leaving the circle takes their photo out.
5. `make test`, `make lint-ci`, the web checks and the iPhone package tests pass; new tests cover the upload protocol, EXIF, places, processing, quota, cleanup, faces grouping, albums, descriptions and sharing authorization, and fail without the change.

## Idempotence and Recovery

Every migration is additive until Milestone 7 makes `photo_album.user_id` nullable; its reverse refuses to run while a circle album exists, deleting nothing on its own. Uploads are resumable by design, and a repeated upload of the same file finds the same photo and the same blob. The processing job can be stopped at any point: a claim older than 10 minutes is taken again, and previews are written before the row says they exist. Cleanup deletes one blob per transaction and marks it first, so an interrupted run resumes. Turning faces off deletes data on purpose; turning them on again reads the photos again.

Before deploying, back up the database to a disk with room for it, check that the build includes every migration already applied there (other sessions deploy too), and deploy with `docker compose up -d --no-deps --force-recreate teanode`. Roll back by restoring the previous image tag in the compose file; the photo tables can stay.

## Artifacts and Notes

Results of the Milestone 0 prototypes are in Surprises & Discoveries. The face reader prototype's core, which Milestone 5 cleans up:

    std::cin >> magic >> width >> height >> maximum; std::cin.get();      // P6 header
    cv::Mat rgb(height, width, CV_8UC3); std::cin.read(rgb.data, width * height * 3);
    cv::cvtColor(rgb, image, cv::COLOR_RGB2BGR);
    auto detector = cv::FaceDetectorYN::create(detectorPath, "", image.size(), 0.8f, 0.3f, 5000);
    auto recognizer = cv::FaceRecognizerSF::create(recognizerPath, "");
    detector->detect(image, faces);                                        // one row per face: box, 5 landmarks, score
    recognizer->alignCrop(image, faces.row(row), aligned); recognizer->feature(aligned, feature); cv::normalize(feature, feature);

    g++ -O2 -std=c++17 faces.cpp -o teanode-faces -I/usr/include/opencv4 -lopencv_objdetect -lopencv_dnn -lopencv_imgproc -lopencv_core

The background uploader's handshake, from Apple's article, which the endpoint must produce:

    OPTIONS /upload            ->  200 OK, Upload-Limit: 107374182400
    POST /upload               ->  104 Upload Resumption Supported, Location: /upload/b530ce8ff
       Upload-Incomplete: ?0       201 Created, Location: /upload/b530ce8ff, Upload-Offset: 52428800

## Interfaces and Dependencies

No new Go module. HEIC decoding needs Debian trixie's `libheif-examples` and `libheif-plugin-libde265` in the image. The face reader needs Debian trixie's OpenCV 4.10 packages (`core`, `imgproc`, `dnn`, `objdetect`) at image build time and the two OpenCV model zoo files (YuNet `face_detection_yunet_2023mar.onnx`, MIT; SFace `face_recognition_sface_2021dec.onnx`, Apache 2.0). Place names need GeoNames' cities file, regenerated by `scripts/make-places.sh` and committed compressed (CC BY 4.0, credited).

In `internal/photo`, define:

    type Store struct { ... }                         // database, files storage.StreamFiles, settings
    func New(database db.Database, files storage.StreamFiles, configuration func() *config.Config) *Store
    func (self *Store) Uploads() http.Handler          // the resumable upload endpoint
    func (self *Store) Process(ctx context.Context, batchSize int) error
    func (self *Store) Cleanup(ctx context.Context, batchSize int) error
    func ReadPictureMetadata(content io.ReaderAt, sizeBytes int64) (*PictureMetadata, error)  // exif.go
    func NearestPlace(latitude, longitude float64) (string, bool)                             // place.go
    type FaceReader interface { ReadFaces(ctx context.Context, jpeg []byte) ([]*FoundFace, error) } // faces.go

In `internal/models/photo.go`: `Photo`, `PhotoFile`, `PhotoUpload`, `PhotoPreview`, `PhotoAlbum`, `PhotoAlbumItem`, `PhotoFace`, `PhotoFaceGroup`, `PhotoAlbumShare`, and the enums named in the milestones.

The upload endpoint's headers: `X-Photo-Client-ID`, `X-Photo-File-Role`, `X-Photo-Kind`, `X-Photo-Filename`, `X-Photo-Taken-At`, `X-Photo-Latitude`, `X-Photo-Longitude`, `X-Photo-Is-Favorite`, `X-Photo-Is-Screenshot`, `X-Photo-Source`, `X-Photo-Declared-SHA256`, and on the answer `X-Photo-ID`; plus the draft's `Upload-Complete`, `Upload-Incomplete`, `Upload-Offset`, `Upload-Limit`, `Upload-Length` and `Upload-Draft-Interop-Version`.

Revision 2026-10-16 23:20 EDT: Milestone 0 run. HEIC now goes through libheif's `heif-dec` instead of ffmpeg, the face reader reads raw pixels to keep OpenCV's decoders out of the image, and the upload routes need no change to the middleware chain. Recorded in Surprises & Discoveries and the Decision Log.
