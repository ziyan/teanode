import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

final class RecordingLibraryTests: XCTestCase {
    private func serverRecording(
        _ recordingId: String, clientRecordingId: String, title: String = "", summary: String = "", createdAt: String = "2030-01-02T03:04:05Z",
        recordedAt: String? = nil
    ) -> Recording {
        Recording(
            id: recordingId, userId: "owner-a", createdAt: createdAt, updatedAt: createdAt, clientRecordingId: clientRecordingId,
            recordingTitle: title, recordingOrigin: "capture", recordedAt: recordedAt, recordingSummary: summary,
            recordingCaptureState: "stopped", timelineDurationMS: 60_000, sourceGeneration: 1, captureGaps: [])
    }

    private func localRecording(
        state: String = "stopped", startedAt: Date = Date(timeIntervalSince1970: 1_900_000_000), mediaSizeBytes: Int64 = 100,
        acceptedBytes: Int64 = 0, isDurable: Bool = false
    ) -> LocalRecording {
        var local = LocalRecording(accountKey: "profile-a", recordingTitle: "", recordingOrigin: "capture", recordingUserId: "owner-a", startedAt: startedAt)
        local.recordingCaptureState = state
        var media = LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 0)
        media.originalSizeBytes = mediaSizeBytes
        media.originalSha256 = String(repeating: "a", count: 64)
        if acceptedBytes > 0 {
            media.acceptedParts = [
                RecordingMediaPart(
                    recordingMediaId: "media-a", partIndex: 0, partSizeBytes: acceptedBytes, partSha256: String(repeating: "b", count: 64),
                    storedAt: "2030-01-02T03:04:05Z")
            ]
        }
        if isDurable {
            let manifest = RecordingMedia(
                id: "media-a", recordingId: "recording-a", clientMediaId: media.id.uuidString, originalFilename: "recording.caf",
                originalContentType: "audio/x-caf", originalSizeBytes: mediaSizeBytes, originalSha256: String(repeating: "a", count: 64),
                timelineStartMS: 0, mediaDurationMS: 1000, partCount: 1, recordingUploadState: "pending", verifiedAt: nil)
            media.mediaManifest = manifest
            media.completionReceipt = RecordingMedia(
                id: "media-a", recordingId: "recording-a", clientMediaId: media.id.uuidString, originalFilename: "recording.caf",
                originalContentType: "audio/x-caf", originalSizeBytes: mediaSizeBytes, originalSha256: String(repeating: "a", count: 64),
                timelineStartMS: 0, mediaDurationMS: 1000, partCount: 1, recordingUploadState: "complete", verifiedAt: "2030-01-02T03:04:05Z")
        }
        local.localMedia = [media]
        return local
    }

    func testOneRowPerRecordingEvenWhenBothSidesHaveIt() {
        var local = localRecording(acceptedBytes: 40)
        local.clientRecordingId = "client-a"
        let rows = RecordingLibrary.rows(
            recordings: [serverRecording("recording-a", clientRecordingId: "client-a", title: "Planning")], localRecordings: [local])
        XCTAssertEqual(rows.count, 1)
        XCTAssertEqual(rows[0].recordingTitle, "Planning")
        XCTAssertNotNil(rows[0].recording)
        XCTAssertNotNil(rows[0].localRecording)
        XCTAssertEqual(rows[0].status, .uploading(percent: 40))
    }

    func testRowsAreNewestRecordedFirstUsingRecordedAtThenCaptureStartThenCreatedAt() {
        var local = localRecording(startedAt: ISO8601DateFormatter().date(from: "2030-03-01T00:00:00Z")!)
        local.clientRecordingId = "client-local"
        let rows = RecordingLibrary.rows(
            recordings: [
                serverRecording("recording-a", clientRecordingId: "client-a", createdAt: "2030-05-01T00:00:00Z", recordedAt: "2030-01-01T00:00:00Z"),
                serverRecording("recording-b", clientRecordingId: "client-b", createdAt: "2030-02-01T00:00:00Z"),
            ], localRecordings: [local])
        XCTAssertEqual(rows.map(\.id), ["client-local", "client-b", "client-a"])
    }

    func testUploadedLocalEntryWithoutAServerRowIsNotListedTwice() {
        let local = localRecording(isDurable: true)
        XCTAssertTrue(RecordingLibrary.rows(recordings: [], localRecordings: [local]).isEmpty)
    }

    func testStatusFollowsTheCaptureThenTheUploadThenTheTranscript() {
        XCTAssertEqual(RecordingLibrary.status(recording: nil, localRecording: localRecording(state: "active"), progress: nil), .recording)
        XCTAssertEqual(RecordingLibrary.status(recording: nil, localRecording: localRecording(state: "paused"), progress: nil), .paused)
        XCTAssertEqual(RecordingLibrary.status(recording: nil, localRecording: localRecording(state: "interrupted"), progress: nil), .paused)
        var failedUpload = localRecording()
        failedUpload.uploadErrorMessage = "The Internet connection appears to be offline."
        XCTAssertEqual(RecordingLibrary.status(recording: nil, localRecording: failedUpload, progress: nil), .failed)
        failedUpload.isUploadWaitingForNetwork = true
        XCTAssertEqual(RecordingLibrary.status(recording: nil, localRecording: failedUpload, progress: nil), .waitingForNetwork)
        let recording = serverRecording("recording-a", clientRecordingId: "client-a")
        func status(_ states: [String], isUploadComplete: Bool = true) -> RecordingRowStatus {
            RecordingLibrary.status(
                recording: recording, localRecording: nil,
                progress: RecordingServerProgress(isUploadComplete: isUploadComplete, uploadedPercent: 25, transcriptProcessingStates: states))
        }
        XCTAssertEqual(status([], isUploadComplete: false), .uploading(percent: 25))
        XCTAssertEqual(status([]), .transcribing)
        XCTAssertEqual(status(["failed", "transcribing"]), .transcribing)
        XCTAssertEqual(status(["failed", "complete"]), .ready)
        XCTAssertEqual(status(["failed"]), .failed)
        XCTAssertEqual(status(["canceled"]), .failed)
    }

    func testServerRowBeforeProgressArrivesIsReadyOnlyWithASummary() {
        XCTAssertEqual(
            RecordingLibrary.status(recording: serverRecording("recording-a", clientRecordingId: "a", summary: "They agreed."), localRecording: nil, progress: nil),
            .ready)
        XCTAssertEqual(
            RecordingLibrary.status(recording: serverRecording("recording-a", clientRecordingId: "a"), localRecording: nil, progress: nil), .transcribing)
    }

    func testFirstLineOfTheSummarySkipsBlankLines() {
        XCTAssertEqual(RecordingLibrary.firstLine(of: "\n  They chose the second plan.\nA second line."), "They chose the second plan.")
        XCTAssertEqual(RecordingLibrary.firstLine(of: nil), "")
    }

    func testProgressFromTheServersViewCountsPartsUntilEveryOriginalIsComplete() throws {
        let json = """
            {"recording":{"id":"recording-a","userId":"owner-a","createdAt":"2030-01-02T03:04:05Z","updatedAt":"2030-01-02T03:04:05Z","clientRecordingId":"client-a","recordingTitle":"","recordingOrigin":"capture","recordingCaptureState":"stopped","timelineDurationMS":1000,"sourceGeneration":1,"captureGaps":[]},
            "mediaFiles":[{"id":"media-a","recordingId":"recording-a","clientMediaId":"c","originalFilename":"a.caf","originalContentType":"audio/x-caf","originalSizeBytes":10,"originalSha256":"\(String(repeating: "a", count: 64))","timelineStartMS":0,"mediaDurationMS":1000,"partCount":4,"recordingUploadState":"pending","verifiedAt":null}],
            "uploadParts":[{"recordingMediaId":"media-a","partIndex":0,"partSizeBytes":1,"partSha256":"\(String(repeating: "b", count: 64))","storedAt":"2030-01-02T03:04:05Z"}],
            "transcriptRuns":[]}
            """
        let view = try JSONDecoder().decode(RecordingView.self, from: Data(json.utf8))
        let progress = RecordingServerProgress(view: view)
        XCTAssertFalse(progress.isUploadComplete)
        XCTAssertEqual(progress.uploadedPercent, 25)
        XCTAssertFalse(progress.isSettled)
        XCTAssertNil(view.recording.recordingSummary)
    }

    func testALibraryKeepsAskingWhileATranscriptOrAFreshTitleIsStillComing() {
        let now = ISO8601DateFormatter().date(from: "2030-01-02T03:10:00Z")!
        let ready = RecordingServerProgress(isUploadComplete: true, uploadedPercent: 100, transcriptProcessingStates: ["complete"])
        let unnamed = RecordingLibrary.rows(recordings: [serverRecording("recording-a", clientRecordingId: "a")], localRecordings: [], serverProgress: ["recording-a": ready])
        XCTAssertEqual(unnamed[0].status, .ready)
        XCTAssertTrue(unnamed[0].isStillChanging(now: now))
        XCTAssertFalse(unnamed[0].isStillChanging(now: now.addingTimeInterval(3600)))
        let named = RecordingLibrary.rows(
            recordings: [serverRecording("recording-a", clientRecordingId: "a", title: "Planning", summary: "They agreed.")], localRecordings: [], serverProgress: ["recording-a": ready])
        XCTAssertFalse(named[0].isStillChanging(now: now))
        let transcribing = RecordingServerProgress(isUploadComplete: true, uploadedPercent: 100, transcriptProcessingStates: ["transcribing"])
        let running = RecordingLibrary.rows(recordings: [serverRecording("recording-a", clientRecordingId: "a")], localRecordings: [], serverProgress: ["recording-a": transcribing])
        XCTAssertTrue(running[0].isStillChanging(now: now.addingTimeInterval(86400)))
    }

    func testOnlyConnectionFailuresWaitForTheNetwork() {
        XCTAssertTrue(RecordingLibrary.isConnectivityFailure(URLError(.notConnectedToInternet)))
        XCTAssertTrue(RecordingLibrary.isConnectivityFailure(URLError(.networkConnectionLost)))
        XCTAssertFalse(RecordingLibrary.isConnectivityFailure(URLError(.badServerResponse)))
        XCTAssertFalse(RecordingLibrary.isConnectivityFailure(TeaNodeError.unreadableAnswer))
    }
}

final class RecordingConversationTests: XCTestCase {
    private func segment(
        _ segmentId: String, start: Int64, speaker: String, attempt: String = "attempt-a", confirmedName: String? = nil, identityId: String? = nil,
        isAccountOwner: Bool = false, text: String = "Words."
    ) -> RecordingTranscriptSegment {
        RecordingTranscriptSegment(
            id: segmentId, recordingTranscriptRunId: "run-a", recordingTranscriptionAttemptId: attempt, localSpeakerId: speaker,
            segmentStartMS: start, segmentEndMS: start + 1000, transcriptText: text, effectiveSpeakerIdentityId: identityId,
            effectiveSpeakerName: confirmedName, hasUserConfirmedSpeaker: confirmedName != nil, isAccountOwner: isAccountOwner)
    }

    func testConsecutivePassagesOfOneSpeakerShareABubbleAndTheOwnerIsMarked() {
        let bubbles = RecordingConversation.bubbles([
            segment("s1", start: 0, speaker: "x", confirmedName: "Mina", identityId: "i1", isAccountOwner: true, text: "Hello."),
            segment("s2", start: 1000, speaker: "x", confirmedName: "Mina", identityId: "i1", isAccountOwner: true, text: "Shall we begin?"),
            segment("s3", start: 2000, speaker: "y", text: "Yes."),
            segment("s4", start: 3000, speaker: "z", text: "Ready."),
            segment("s5", start: 4000, speaker: "y", text: "Good."),
        ])
        XCTAssertEqual(bubbles.map(\.id), ["s1", "s3", "s4", "s5"])
        XCTAssertEqual(bubbles[0].bubbleText, "Hello. Shall we begin?")
        XCTAssertTrue(bubbles[0].isAccountOwner)
        XCTAssertEqual(bubbles[0].confirmedSpeakerName, "Mina")
        XCTAssertFalse(bubbles[1].isAccountOwner)
        XCTAssertNil(bubbles[1].confirmedSpeakerName)
        XCTAssertEqual(bubbles.map(\.unconfirmedSpeakerOrdinal), [0, 0, 1, 0])
        XCTAssertEqual(bubbles[2].startMS, 3000)
    }

    func testAConfirmedSpeakerIsOnePersonAcrossRequestsButALabelIsNot() {
        let bubbles = RecordingConversation.bubbles([
            segment("s1", start: 0, speaker: "x", attempt: "attempt-a", confirmedName: "Omar", identityId: "i2"),
            segment("s2", start: 1000, speaker: "q", attempt: "attempt-b", confirmedName: "Omar", identityId: "i2"),
            segment("s3", start: 2000, speaker: "x", attempt: "attempt-a"),
            segment("s4", start: 3000, speaker: "x", attempt: "attempt-b"),
        ])
        XCTAssertEqual(bubbles.map(\.id), ["s1", "s3", "s4"])
    }

    func testSpeakerCountCountsAConfirmedPersonOnce() {
        XCTAssertEqual(
            RecordingConversation.speakerCount([
                segment("s1", start: 0, speaker: "x", attempt: "attempt-a", confirmedName: "Omar", identityId: "i2"),
                segment("s2", start: 1000, speaker: "q", attempt: "attempt-b", confirmedName: "Omar", identityId: "i2"),
                segment("s3", start: 2000, speaker: "x", attempt: "attempt-a"),
            ]), 2)
    }

    func testSpeakerLettersRunPastTheAlphabetIntoNumbers() {
        XCTAssertEqual(RecordingConversation.speakerLetter(ordinal: 0), "A")
        XCTAssertEqual(RecordingConversation.speakerLetter(ordinal: 25), "Z")
        XCTAssertEqual(RecordingConversation.speakerLetter(ordinal: 26), "27")
    }

    func testInputLevelIsClampedBetweenTheFloorAndZeroDecibels() {
        XCTAssertEqual(RecordingInputLevel.normalized(decibels: -160), 0)
        XCTAssertEqual(RecordingInputLevel.normalized(decibels: 0), 1)
        XCTAssertEqual(RecordingInputLevel.normalized(decibels: -25), 0.5, accuracy: 0.001)
        XCTAssertEqual(RecordingInputLevel.normalized(decibels: .nan), 0)
    }
}
