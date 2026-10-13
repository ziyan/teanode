import Foundation
import XCTest

@testable import TeaNodeKit

final class RecordingCutPolicyTests: XCTestCase {
    /// Feeds the meter every 50 milliseconds from `fromMS` up to `untilMS` at one level.
    private func feed(_ policy: inout RecordingCutPolicy, fromMS: Int64, untilMS: Int64, decibels: Float) {
        var atMS = fromMS
        while atMS <= untilMS {
            policy.recordLevel(decibels: decibels, atMS: atMS)
            atMS += 50
        }
    }

    func testNothingIsCutBeforeTwentySecondsWhateverIsHeard() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 0)
        feed(&policy, fromMS: 0, untilMS: 19_900, decibels: -50)
        policy.recordUtteranceEnd(atMS: 15_000)
        XCTAssertNil(policy.cutReason(atMS: 19_900, isStreamConnected: true))
        XCTAssertNil(policy.cutReason(atMS: 19_900, isStreamConnected: false))
    }

    func testAConnectedStreamCutsAtTheFirstUtteranceEndAfterTwentySeconds() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 1_000)
        // An end at 15 seconds into the piece is too early to count; the quiet level is ignored while connected.
        policy.recordUtteranceEnd(atMS: 16_000)
        feed(&policy, fromMS: 1_000, untilMS: 30_000, decibels: -60)
        XCTAssertNil(policy.cutReason(atMS: 25_000, isStreamConnected: true))
        policy.recordUtteranceEnd(atMS: 27_300)
        XCTAssertEqual(policy.cutReason(atMS: 27_350, isStreamConnected: true), .utteranceEnd)
    }

    func testAnUtteranceEndOfThePreviousPieceDoesNotCutTheNextOne() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 0)
        policy.recordUtteranceEnd(atMS: 21_000)
        policy.startPiece(atMS: 21_100)
        XCTAssertNil(policy.cutReason(atMS: 41_200, isStreamConnected: true))
    }

    func testWithoutAStreamTheLevelHoldingNearItsLowestCuts() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 0)
        // Loud speech, then 200 milliseconds of quiet: not yet a pause. Then 300: a pause.
        feed(&policy, fromMS: 0, untilMS: 24_000, decibels: -20)
        feed(&policy, fromMS: 24_050, untilMS: 24_200, decibels: -58)
        XCTAssertNil(policy.cutReason(atMS: 24_200, isStreamConnected: false))
        feed(&policy, fromMS: 24_250, untilMS: 24_300, decibels: -58)
        XCTAssertEqual(policy.cutReason(atMS: 24_300, isStreamConnected: false), .levelPause)
    }

    func testBackgroundNoiseIsMeasuredAgainstItsOwnFloor() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 0)
        // Music at -30 decibels with speech over it at -10; a pause in the speech falls back to the music, within 6 decibels of
        // the lowest level of the last 10 seconds, and cuts.
        feed(&policy, fromMS: 0, untilMS: 22_000, decibels: -10)
        feed(&policy, fromMS: 22_050, untilMS: 22_400, decibels: -30)
        XCTAssertEqual(policy.cutReason(atMS: 22_400, isStreamConnected: false), .levelPause)
    }

    func testLoudnessThatIsNeverNearTheLowestDoesNotCut() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 0)
        feed(&policy, fromMS: 0, untilMS: 22_000, decibels: -10)
        feed(&policy, fromMS: 22_050, untilMS: 22_100, decibels: -45)
        feed(&policy, fromMS: 22_150, untilMS: 24_000, decibels: -10)
        XCTAssertNil(policy.cutReason(atMS: 24_000, isStreamConnected: false))
    }

    func testAMeterThatStoppedReportingDoesNotMakeAPause() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 0)
        feed(&policy, fromMS: 0, untilMS: 21_000, decibels: -40)
        XCTAssertNil(policy.cutReason(atMS: 22_000, isStreamConnected: false))
    }

    func testEveryPieceIsCutAtSixtySecondsWhateverIsHeard() {
        var policy = RecordingCutPolicy()
        policy.startPiece(atMS: 5_000)
        feed(&policy, fromMS: 5_000, untilMS: 64_950, decibels: -10)
        XCTAssertNil(policy.cutReason(atMS: 64_950, isStreamConnected: true))
        XCTAssertEqual(policy.cutReason(atMS: 65_000, isStreamConnected: true), .maximumLength)
        XCTAssertEqual(policy.cutReason(atMS: 65_000, isStreamConnected: false), .maximumLength)
    }

    func testTheLiveStreamBacksOffAndStaysAtAMinute() {
        XCTAssertEqual((0...8).map { RecordingLiveBackoff.delaySeconds(afterFailureCount: $0) }, [0, 1, 2, 4, 8, 16, 32, 60, 60])
        XCTAssertEqual(RecordingLiveBackoff.delaySeconds(afterFailureCount: 1_000), 60)
    }
}

final class RecordingLiveConversationTests: XCTestCase {
    private func utterance(_ utteranceId: String, start: Int64, end: Int64, text: String = "Words.") -> RecordingLiveUtterance {
        RecordingLiveUtterance(utteranceId: utteranceId, utteranceStartMS: start, utteranceEndMS: end, transcriptText: text)
    }

    private func segment(
        _ segmentId: String, start: Int64, end: Int64, speaker: String = "A", temporaryLabel: String? = nil, confirmedName: String? = nil,
        identityId: String? = nil, isAccountOwner: Bool = false, text: String = "Words."
    ) -> RecordingTranscriptSegment {
        RecordingTranscriptSegment(
            id: segmentId, recordingTranscriptRunId: "run-a", recordingTranscriptionAttemptId: "attempt-a", localSpeakerId: speaker,
            segmentStartMS: start, segmentEndMS: end, transcriptText: text, effectiveSpeakerIdentityId: identityId, effectiveSpeakerName: confirmedName,
            hasUserConfirmedSpeaker: confirmedName != nil, isAccountOwner: isAccountOwner, temporarySpeakerLabel: temporaryLabel)
    }

    func testEachLiveUtteranceIsABubbleWithoutASpeakerBetweenThePassagesAroundItInTime() {
        let bubbles = RecordingConversation.bubbles(
            [segment("s1", start: 0, end: 10_000, text: "First."), segment("s2", start: 30_000, end: 40_000, text: "Last.")],
            liveUtterances: [
                utterance("u2", start: 20_000, end: 22_000, text: "Second."), utterance("u1", start: 12_000, end: 15_000, text: "Between.")
            ])
        XCTAssertEqual(bubbles.map(\.bubbleText), ["First.", "Between.", "Second.", "Last."])
        XCTAssertEqual(bubbles.map(\.isLive), [false, true, true, false])
        XCTAssertNil(bubbles[1].confirmedSpeakerName)
        XCTAssertNil(bubbles[1].temporarySpeakerLabel)
        XCTAssertFalse(bubbles[1].isAccountOwner)
        XCTAssertEqual(bubbles[1].id, "live:u1")
    }

    func testLiveUtterancesDoNotCountAsSpeakers() {
        let segments = [segment("s1", start: 0, end: 1_000)]
        XCTAssertEqual(RecordingConversation.speakerCount(segments), 1)
    }

    func testATemporarySpeakerIsOneSpeakerAcrossLabelsAndIsNotGivenALetter() {
        let bubbles = RecordingConversation.bubbles([
            segment("s1", start: 0, end: 1_000, speaker: "A", temporaryLabel: "Speaker 1"),
            segment("s2", start: 1_000, end: 2_000, speaker: "Speaker 1", temporaryLabel: "Speaker 1"),
            segment("s3", start: 2_000, end: 3_000, speaker: "B"),
            segment("s4", start: 3_000, end: 4_000, speaker: "A", temporaryLabel: "Speaker 2"),
        ])
        XCTAssertEqual(bubbles.map(\.id), ["s1", "s3", "s4"])
        XCTAssertEqual(bubbles.map(\.temporarySpeakerLabel), ["Speaker 1", nil, "Speaker 2"])
        // The one speaker with a plain label is the first of those, whatever the temporary speakers before it.
        XCTAssertEqual(bubbles[1].unconfirmedSpeakerOrdinal, 0)
    }

    func testANamedTemporarySpeakerShowsTheName() {
        let bubbles = RecordingConversation.bubbles([
            segment("s1", start: 0, end: 1_000, speaker: "Speaker 1", temporaryLabel: nil, confirmedName: "Jun", identityId: "identity-a")
        ])
        XCTAssertEqual(bubbles[0].confirmedSpeakerName, "Jun")
        XCTAssertNil(bubbles[0].temporarySpeakerLabel)
    }

    func testAPassageCoveringMostOfAnUtteranceTakesItsPlace() {
        let covering = [segment("s1", start: 0, end: 10_000)]
        XCTAssertTrue(RecordingLiveWords.isCovered(utterance("a", start: 2_000, end: 6_000), by: covering))
        // Four tenths of it are covered: not enough, the words stay until the passage that has the rest arrives.
        XCTAssertFalse(RecordingLiveWords.isCovered(utterance("b", start: 8_000, end: 13_000), by: covering))
        XCTAssertTrue(RecordingLiveWords.isCovered(utterance("c", start: 7_000, end: 11_000), by: covering))
        XCTAssertFalse(RecordingLiveWords.isCovered(utterance("d", start: 12_000, end: 14_000), by: covering))
        XCTAssertTrue(RecordingLiveWords.isCovered(utterance("e", start: 5_000, end: 5_000), by: covering))
        XCTAssertFalse(RecordingLiveWords.isCovered(utterance("f", start: 15_000, end: 15_000), by: covering))
    }

    func testShownWordsAreTheServersAndTheOnesJustHeardEachOnceMinusWhatThePassagesCover() {
        let shown = RecordingLiveWords.shown(
            serverUtterances: [utterance("u1", start: 1_000, end: 2_000), utterance("u2", start: 50_000, end: 52_000, text: "Server.")],
            heardUtterances: [
                utterance("u2", start: 50_000, end: 52_000, text: "Heard."), utterance("u3", start: 53_000, end: 55_000, text: "Newest."),
                utterance("u0", start: 0, end: 0, text: "  "),
            ],
            partialText: "", partialStartMS: 0, segments: [segment("s1", start: 0, end: 30_000)])
        XCTAssertEqual(shown.map(\.utteranceId), ["u2", "u3"])
        XCTAssertEqual(shown[0].transcriptText, "Server.")
    }

    func testWordsStillBeingSaidComeLastAndNothingOlderThanTheWindowIsShown() {
        let shown = RecordingLiveWords.shown(
            serverUtterances: [utterance("old", start: 1_000, end: 2_000), utterance("u1", start: 70_000, end: 72_000)],
            heardUtterances: [], partialText: " and then ", partialStartMS: 71_000, segments: [], windowStartMS: 60_000)
        XCTAssertEqual(shown.map(\.utteranceId), ["u1", "partial"])
        XCTAssertEqual(shown[1].transcriptText, "and then")
        XCTAssertEqual(shown[1].utteranceStartMS, 72_000)
    }

    func testOnlyTheLatestHeardUtterancesAreKept() {
        var heard: [RecordingLiveUtterance] = []
        for index in 0..<(RecordingLiveWords.heardUtterancesKept + 20) {
            heard = RecordingLiveWords.appending(utterance("u\(index)", start: Int64(index), end: Int64(index) + 1), to: heard)
        }
        XCTAssertEqual(heard.count, RecordingLiveWords.heardUtterancesKept)
        XCTAssertEqual(heard.first?.utteranceId, "u20")
        heard = RecordingLiveWords.appending(utterance("u50", start: 50, end: 51, text: "Again."), to: heard)
        XCTAssertEqual(heard.filter { $0.utteranceId == "u50" }.count, 1)
        XCTAssertEqual(heard.last?.transcriptText, "Again.")
    }

    func testTheTimelineAndTheLiveSocketDecodeWhatTheServerSends() throws {
        let timelineJSON = """
            {"sourceGeneration":3,"segments":[{"id":"s1","recordingTranscriptRunId":"r","recordingTranscriptionAttemptId":"a","localSpeakerId":"A",\
            "segmentStartMS":0,"segmentEndMS":1000,"transcriptText":"Hi.","temporarySpeakerLabel":"Speaker 1"}],"transcriptRuns":[],\
            "temporarySpeakers":[{"speakerLabel":"Speaker 1","speakerIdentityId":""}],"segmentCount":1,"hasMoreSegments":false,\
            "liveUtterances":[{"utteranceId":"u1","utteranceStartMS":1500,"utteranceEndMS":2500,"transcriptText":"More."}]}
            """
        let timeline = try JSONDecoder().decode(RecordingTranscriptTimeline.self, from: Data(timelineJSON.utf8))
        XCTAssertEqual(timeline.segments[0].temporarySpeakerLabel, "Speaker 1")
        XCTAssertEqual(timeline.temporarySpeakers?.first?.speakerLabel, "Speaker 1")
        XCTAssertEqual(timeline.liveUtterances?.first?.utteranceEndMS, 2500)
        let oldJSON = #"{"sourceGeneration":3,"segments":[],"transcriptRuns":[],"segmentCount":0,"hasMoreSegments":false}"#
        XCTAssertNil(try JSONDecoder().decode(RecordingTranscriptTimeline.self, from: Data(oldJSON.utf8)).liveUtterances)
        let event = try JSONDecoder().decode(
            VoiceEvent.self, from: Data(#"{"voiceEvent":"transcriptFinal","utteranceId":"u1","transcriptText":"Hi.","utteranceStartMS":61000,"utteranceEndMS":63500}"#.utf8))
        XCTAssertEqual(event.utteranceStartMS, 61000)
        XCTAssertEqual(event.utteranceEndMS, 63500)
    }
}

final class LocalRecordingSharedManifestTests: XCTestCase {
    private func recording() -> LocalRecording {
        var recording = LocalRecording(accountKey: "account", recordingTitle: "", recordingOrigin: "capture", recordingUserId: "owner")
        var first = LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 0, mediaDurationMS: 30_000)
        first.originalSizeBytes = 960_000
        first.originalSha256 = String(repeating: "a", count: 64)
        recording.localMedia = [first]
        return recording
    }

    func testTheRecordersCopyDoesNotUndoWhatTheUploaderLearned() throws {
        var stored = recording()
        let baseline = stored
        var uploaderCopy = stored
        uploaderCopy.recordingId = "remote-a"
        uploaderCopy.uploadErrorMessage = "offline"
        uploaderCopy.isUploadWaitingForNetwork = true
        uploaderCopy.localMedia[0].acceptedParts = [
            try JSONDecoder().decode(
                RecordingMediaPart.self,
                from: Data(
                    #"{"recordingMediaId":"m","partIndex":0,"partSizeBytes":8,"partSha256":"b","storedAt":"2030-01-02T03:04:05Z"}"#.utf8))
        ]
        stored.adoptUpload(from: uploaderCopy)
        // The recorder closes a second piece and moves on, from a copy that knows nothing of the upload.
        var recorderCopy = baseline
        var second = LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 30_000)
        second.originalSizeBytes = 100
        recorderCopy.localMedia.append(second)
        recorderCopy.timelineDurationMS = 41_000
        try recorderCopy.pause(at: 41_000)
        stored.adoptCapture(from: recorderCopy)
        XCTAssertEqual(stored.recordingId, "remote-a")
        XCTAssertEqual(stored.uploadErrorMessage, "offline")
        XCTAssertEqual(stored.localMedia.count, 2)
        XCTAssertEqual(stored.localMedia[0].acceptedParts.count, 1)
        XCTAssertEqual(stored.localMedia[1].id, second.id)
        XCTAssertEqual(stored.recordingCaptureState, "paused")
        XCTAssertEqual(stored.timelineDurationMS, 41_000)
        XCTAssertEqual(stored.captureGaps.count, 1)
    }

    func testTheUploadersCopyDoesNotUndoWhatTheRecorderWrote() {
        var stored = recording()
        var uploaderCopy = stored
        stored.timelineDurationMS = 90_000
        stored.recordingCaptureState = "paused"
        stored.localMedia.append(LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 30_000))
        uploaderCopy.hasPublishedCapture = true
        uploaderCopy.recordingId = "remote-a"
        stored.adoptUpload(from: uploaderCopy)
        XCTAssertEqual(stored.timelineDurationMS, 90_000)
        XCTAssertEqual(stored.recordingCaptureState, "paused")
        XCTAssertEqual(stored.localMedia.count, 2)
        XCTAssertEqual(stored.hasPublishedCapture, true)
    }

    func testAPieceOfAStoppedCaptureWhoseDigestOnlyTheUploaderMeasuredKeepsIt() {
        var stored = recording()
        stored.localMedia[0].originalSha256 = nil
        stored.localMedia[0].originalSizeBytes = 0
        var uploaderCopy = stored
        uploaderCopy.localMedia[0].originalSha256 = String(repeating: "c", count: 64)
        uploaderCopy.localMedia[0].originalSizeBytes = 777
        stored.adoptUpload(from: uploaderCopy)
        XCTAssertEqual(stored.localMedia[0].originalSha256, String(repeating: "c", count: 64))
        XCTAssertEqual(stored.localMedia[0].originalSizeBytes, 777)
        // Once the recorder has written a digest, the uploader's copy cannot change it.
        var other = stored
        other.localMedia[0].originalSha256 = String(repeating: "d", count: 64)
        stored.adoptUpload(from: other)
        XCTAssertEqual(stored.localMedia[0].originalSha256, String(repeating: "c", count: 64))
    }

    func testAPieceTheRecorderDroppedGoesAndOnlyClosedPiecesCarryADigestFromTheRecorder() {
        var stored = recording()
        stored.localMedia.append(LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 30_000))
        var recorderCopy = stored
        recorderCopy.localMedia = [stored.localMedia[1]]
        stored.adoptCapture(from: recorderCopy)
        XCTAssertEqual(stored.localMedia.count, 1)
        XCTAssertNil(stored.localMedia[0].originalSha256)
    }

    func testAnOpenGapIsPublishedEndingAtTheDurationSoFar() throws {
        var recording = recording()
        recording.timelineDurationMS = 30_000
        try recording.pause(at: 30_000)
        recording.timelineDurationMS = 36_000
        XCTAssertNil(recording.captureGaps[0].gapEndMS)
        XCTAssertEqual(recording.publishedCaptureGaps[0].gapEndMS, 36_000)
        XCTAssertTrue(recording.isCaptureUnderWay)
        try recording.stop(at: 40_000)
        XCTAssertFalse(recording.isCaptureUnderWay)
    }

    func testAJournalUpdateKeepsWhatAnotherWriterWroteBetweenTwoReads() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try RecordingJournal(directory: directory)
        let original = recording()
        try await journal.create(original)
        var uploaderCopy = try await journal.read(original.id, accountKey: "account")
        uploaderCopy.recordingId = "remote-a"
        var recorderCopy = original
        recorderCopy.timelineDurationMS = 12_000
        let afterRecorder = recorderCopy
        _ = try await journal.update(original.id, accountKey: "account") { $0.adoptCapture(from: afterRecorder) }
        let upload = uploaderCopy
        let merged = try await journal.update(original.id, accountKey: "account") { $0.adoptUpload(from: upload) }
        XCTAssertEqual(merged.recordingId, "remote-a")
        XCTAssertEqual(merged.timelineDurationMS, 12_000)
        let reread = try await journal.read(original.id, accountKey: "account")
        XCTAssertEqual(reread, merged)
    }
}

final class RecordingPieceBoundaryTests: XCTestCase {
    func testPiecesOfOneCaptureFollowEachOtherWithoutAGapOrAnOverlap() {
        // Three pieces of 16000 hertz audio: 20.5, 31.25 and 60 seconds long.
        let frames: [Int64] = [328_000, 500_000, 960_000]
        var startMS: Int64 = 12_000
        var starts: [Int64] = []
        for count in frames {
            starts.append(startMS)
            startMS = RecordingPieceBoundary.nextStartMS(previousStartMS: startMS, writtenFrames: count, sampleRate: 16000)
        }
        XCTAssertEqual(starts, [12_000, 32_500, 63_750])
        XCTAssertEqual(startMS, 123_750)
        XCTAssertEqual(RecordingPieceBoundary.gapMS(previousEndMS: starts[1], startMS: starts[1]), 0)
    }

    func testALengthIsRoundedToTheNearestMillisecondAndEmptyOrInvalidIsZero() {
        XCTAssertEqual(RecordingPieceBoundary.lengthMS(writtenFrames: 24, sampleRate: 16000), 2)
        XCTAssertEqual(RecordingPieceBoundary.lengthMS(writtenFrames: 23, sampleRate: 16000), 1)
        XCTAssertEqual(RecordingPieceBoundary.lengthMS(writtenFrames: 0, sampleRate: 16000), 0)
        XCTAssertEqual(RecordingPieceBoundary.lengthMS(writtenFrames: -5, sampleRate: 16000), 0)
        XCTAssertEqual(RecordingPieceBoundary.lengthMS(writtenFrames: 100, sampleRate: 0), 0)
    }

    func testAPauseLeavesExactlyTheTimeBetweenTheLastPieceAndTheNextOne() throws {
        var recording = LocalRecording(accountKey: "account", recordingTitle: "", recordingOrigin: "capture", recordingUserId: "owner")
        let firstEndMS = RecordingPieceBoundary.nextStartMS(previousStartMS: 0, writtenFrames: 480_000, sampleRate: 16000)
        recording.timelineDurationMS = firstEndMS
        try recording.pause(at: firstEndMS, gapReason: "interrupted")
        try recording.resume(at: 45_000)
        XCTAssertEqual(recording.captureGaps[0].gapStartMS, 30_000)
        XCTAssertEqual(recording.captureGaps[0].gapEndMS, 45_000)
        XCTAssertEqual(RecordingPieceBoundary.gapMS(previousEndMS: firstEndMS, startMS: 45_000), 15_000)
        XCTAssertEqual(RecordingPieceBoundary.gapMS(previousEndMS: 45_000, startMS: 44_990), 0)
    }

    func testACorrectedStartOfAPieceStillBeingRecordedIsTakenFromTheRecorderNotTheUploadersCopy() {
        var stored = LocalRecording(accountKey: "account", recordingTitle: "", recordingOrigin: "capture", recordingUserId: "owner")
        stored.localMedia = [LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: 30_000)]
        var recorderCopy = stored
        recorderCopy.localMedia[0].timelineStartMS = 30_040
        stored.adoptCapture(from: recorderCopy)
        XCTAssertEqual(stored.localMedia[0].timelineStartMS, 30_040)
    }
}
