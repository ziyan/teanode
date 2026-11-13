# Parity: Photos and Recordings

The iPhone files of this area and what the Android app does with each. Status is done, partly or not ported.
Kit tests are in `android/kit/src/test/kotlin/com/teanode/kit/{photos,recordings}`; screenshots are named
`photos-*` and `recordings-*` (`android/app/build/outputs/roborazzi/`).

## Photos: screens (ios/TeaNode/Sources)

| iOS file | Android | Status |
| --- | --- | --- |
| PhotosView.swift | `photos/PhotosSection.kt`, `PhotoGrid.kt`, `PhotoList.kt`: library by day, Years/Months/All scale bar, search, kind/date/near filters with chips, collections menu (Library, Favorites, Albums, People, Trash), choose photos (long press, select all), move to Trash, restore, delete forever, empty Trash with confirmations, trash days with days left, paging with load more, pull to refresh, empty/error/loading states, waiting and failed cells, video length and heart badges | done |
| PhotoViewer.swift | `PhotoViewer.kt`: swipe between photos, pinch zoom, heart, info sheet (date, file, size, camera, place, albums, description, text in the picture, read as receipt/statement/tax document, describe again with polling, people), share, save original/edited/Live Photo video to the gallery, save to my library, make thumbnails again, change date, change location, photos taken near here, use as album cover, remove from album, trash/restore/delete | done (zoom is pinch and drag only, no double tap) |
| VideoPlayback.swift | `VideoPlayback.kt`: ExoPlayer, play/pause, 5 s back and forward, scrub bar, controls that fade after 3 s (rules from `VideoControls`) | done, not run on a device |
| PhotoAlbums.swift | `PhotoAlbums.kt`, `PhotoSheets.kt`: albums in My albums, Shared with you and per circle, suggestions with samples, create/rename/delete, album screen with choose, remove from album, save shared photos to library, add to album sheet, new album dialog | done |
| PhotoSharingViews.swift | `PhotoAlbumShares.kt`, `PhotoSheets.kt`: shares of an album, add a share for a person or circle at a level with location and face names switches, change level, stop sharing; kind and date filter sheet; change date dialog | done (the filter's date pickers are Material date pickers) |
| PhotoPeople.swift | `PhotoPeople.kt`: people grid, face recognition off/unavailable states, find faces, stop finding faces (deletes), hidden people, person screen, name a person from contacts or a new contact (merge hint), remove name, merge with, hide | done (no "not this person" on a face in the info sheet) |
| PhotoLocationSheet.swift | `PhotoSheets.kt` `ChangeLocationSheet`: city search, remove location with confirmation | done |
| PhotoBackupView.swift | `PhotoBackupScreen.kt`: switch, status, count and progress, time left, access states (needs/limited), sign in again, back up now, pause/resume, mobile data, read new photos, space used, largest file, last finished, last problem, turn off | done |
| PhotoBackupSession.swift, PhotoBackupEngine.swift, PhotoKitLibrary.swift, PhotoLibraryBrowsing.swift | `PhotoBackupEngine.kt`: MediaStore reader, WorkManager periodic and one-time work, resumable upload protocol, records, credential in the Keystore store | partly: no location from EXIF (needs ACCESS_MEDIA_LOCATION), no Live Photo or edited files (Android has none), no hidden album switch, no foreground notification while a long pass runs, not run against a real server |
| PhotoBackupRefusedFilesView.swift | not ported: the "Files not backed up" list with Try again and Skip per file (the counts and the last problem are on the backup screen; the records keep skip state) | not ported |
| PhotoErrorText.swift | the kit's `PhotoError` has no words yet; they surface through the app's error words | partly |
| PhotosDemo.swift, PhotosDemoVideo.swift | `photos/PhotosDemo.kt`: 17 invented photos (video, Live Photo, favorites, waiting, failed, trash, shared), albums, suggestion, people, shares, circles, contacts, pictures drawn as gradients | done |

## Photos: kit (ios/TeaNodeKit/Sources/TeaNodeKit, package `com.teanode.kit.photos`)

| iOS file | Kotlin | Tests | Status |
| --- | --- | --- | --- |
| Photos.swift | `Photos.kt`, `PhotoCalls.kt`, `PhotoNamingContacts.kt`, `PhotoDocuments.kt` (documents generated from the Swift text) | `PhotosTest` | done |
| PhotoOverview.swift | `PhotoOverview.kt` | `PhotosTest` | done |
| PhotoSharing.swift | `PhotoSharing.kt`, `PhotoShareTargets.kt` (a narrower circles read for choosing who to share with) | `PhotosTest` | done (temporary file cleanup is not needed) |
| PhotoBackup.swift, PhotoBackupCoordinator.swift, PhotoBackupUploadClient.swift | `PhotoBackup.kt`: planner, headers, outcomes, upload requests and reading the answers, status | `PhotoBackupTest` | partly: the coordinator's scheduling is `PhotoBackupWorker` in the app |
| PhotoBackupTimeLeft.swift | `PhotoBackupTimeLeft.kt` | `PhotoBackupTimeLeftTest` | done |
| PhotoBackupListingWindow, PhotoBackupTailWriter, PhotoFileDigest, PhotoPreviewCache | not needed: MediaStore lists in one query, the digest is streamed in the worker, previews use the foundation's picture cache | | not ported (reason given) |
| VideoControls.swift | `VideoControls.kt` | `VideoControlsTest` | done |

## Recordings: screens

| iOS file | Android | Status |
| --- | --- | --- |
| RecordingsView.swift | `recordings/RecordingsSection.kt`: list joined with the phone's recordings, search, statuses (recording, paused, uploading %, waiting for network, transcribing, failed), rows polled while changing, new/import menu, long press menu (retry upload, play local original, remove from this phone, delete), shared with you | done |
| RecordingDetailView.swift | `RecordingDetailScreen.kt`: heading, summary, status, player, transcript as a conversation, rename with title and date, transcribe again, retry and cancel a transcription, share, delete, details (capture gaps, originals, transcription runs) | partly: no Learning card (resume reading, knowledge source), no transcription cost lines |
| RecordingTranscriptView.swift, RecordingConversationView.swift | `RecordingDetailScreen.kt` bubbles: speaker names, "Speaker A", play from here, name speaker with suggestions and the whole-voice switch, correct text, copy, load more passages | partly: no speakers and voice references screen, no unlink contact, no "Not me", no clear name, no "In memory" |
| RecordingTextEditor.swift | correct text dialog | partly: no edit history screen with remove correction |
| RecordingCaptureView.swift, RecordingEngineCapture.swift, MeetingRecorder.swift | `RecordingCaptureScreen.kt`, `RecordingCaptureService.kt`: foreground microphone service, pieces cut where the speaker pauses, pause/resume gaps, level meter, stop, notification actions | partly: no live transcription, no call interruption handling (a call does not pause it), not run on a device |
| RecordingUploads.swift | `RecordingUploadWorker.kt`: create, publish capture state and gaps, manifest, 8 MB parts with digests, complete, remove the phone's copy once verified, retries with WorkManager | done, not run against a real server |
| RecordingPlayer.swift, RecordingsView player | `RecordingPlayer.kt`: ExoPlayer over the timeline pieces, compatible audio when an original cannot be played | done, not run on a device |
| RecordingSharingViews.swift | `RecordingSharingScreens.kt`: shares, share with person or circle at a level, stop sharing, recordings shared with me, a shared recording with its turns | done |
| RecordingAttachmentImportView.swift | not ported: it opens a recording from a chat attachment, and chats own attachments | not ported |
| RecordingCitationScreen.swift, RecordingLink | route `recordings/cite/{recordingId}/{runId}/{startMS}/{endMS}` opens the recording with the passage highlighted | partly: not scrolled to the passage |
| RecordingLiveStream.swift | not ported: the live transcription socket | not ported |
| RecordingDemo.swift | `recordings/RecordingsDemo.kt`: five recordings, a transcript of three speakers, shares, shared recordings | done |

## Recordings: kit (package `com.teanode.kit.recordings`)

| iOS file | Kotlin | Tests |
| --- | --- | --- |
| Recordings.swift | `Recordings.kt` (models, calls, part and completion requests) | `RecordingLibraryTest` |
| RecordingProcessing.swift, RecordingTimeline.swift, RecordingTextCorrections.swift | `RecordingProcessing.kt` | `RecordingLiveTest` |
| RecordingSharing.swift | `RecordingSharing.kt` | |
| RecordingLibrary.swift, RecordingLibraryProgress.swift, RecordingJournal.swift (the model) | `RecordingLibrary.kt` (`LocalRecording`, rows, status, progress) | `RecordingLibraryTest` |
| RecordingPlayback.swift | `RecordingPlayback.kt` | |
| RecordingTimelinePieces.swift | `RecordingTimelinePieces.kt` | `RecordingTimelinePiecesTest` |
| RecordingConversation.swift | `RecordingConversation.kt` (bubbles, live words, level history) | `RecordingLiveTest` |
| RecordingCutPolicy.swift | `RecordingCapture.kt` (`RecordingCutPolicy`, backoff) | `RecordingCutPolicyTest` |
| RecordingCaptureTransitions, RecordingClock, RecordingUploadScheduling, RecordingLink | `RecordingCapture.kt` | `RecordingCaptureTest` |
| RecordingLearning, RecordingKnowledgeTitle, RecordingJournal.swift (file storage), RecordingOwner, RecordingCapabilities, RecordingReferenceInvalidations, RecordingSpeakerDraft, RecordingRequestIdentity, RecordingAttachmentImport | documents for Learning, reference invalidations and attachment import are copied into `RecordingDocuments.kt` but unused; the file storage is `RecordingJournal.kt` in the app; the rest are not needed | |

## Checklist counts

Screens and flows (iOS files in scope, counted once): 32 listed above.
Done: 17. Partly: 10. Not ported: 5 (refused files list, attachment import, live stream, and the two kit
helpers that Android does not need are counted as not needed, not as missing).

## Not verified

Everything that needs a real server, phone or credential: the upload protocol of photo backup and of recordings against
a server, the microphone service, ExoPlayer playback, MediaStore reading, WorkManager scheduling, the gallery save and
share, and the permission prompts. They are written against the iPhone app's documented protocol and compile, and the
screens are photographed in demo mode.

## Shared changes

Small and additive: `gradle/libs.versions.toml` and `app/build.gradle.kts` (media3 ExoPlayer and UI, WorkManager) and
`AndroidManifest.xml` (media and microphone permissions, the capture service).
