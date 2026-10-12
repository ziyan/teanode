import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

final class RecordingTests: XCTestCase {
    func testPauseInterruptionAndRecoveryKeepWallTimelineGaps() throws {
        var recording = LocalRecording(accountKey: "account-a", recordingTitle: "", recordingOrigin: "capture")
        try recording.pause(at: 1000)
        try recording.resume(at: 4000)
        try recording.pause(at: 8000, gapReason: "interrupted")
        let recovered = try JSONDecoder().decode(LocalRecording.self, from: JSONEncoder().encode(recording))
        XCTAssertEqual(recovered.recordingCaptureState, "interrupted")
        XCTAssertEqual(recovered.captureGaps.map(\.recordingGapReason), ["paused", "interrupted"])
        XCTAssertEqual(recovered.captureGaps[0].gapEndMS, 4000)
        try recording.stop(at: 10000)
        XCTAssertEqual(recording.captureGaps[1].gapEndMS, 10000)
        XCTAssertThrowsError(try recording.resume(at: 11000))
        XCTAssertThrowsError(try recording.pause(at: 12000))
    }

    func testJournalCannotReleaseUnconfirmedSourcesOrAnotherAccountsFiles() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try RecordingJournal(directory: directory)
        var recording = LocalRecording(accountKey: "account-a", recordingTitle: "Import", recordingOrigin: "audioImport")
        let media = LocalRecordingMedia(originalFilename: "../../outside.wav", originalContentType: "audio/wav", timelineStartMS: 0)
        recording.localMedia.append(media)
        try await journal.create(recording)
        let source = directory.appendingPathComponent("test-source")
        try Data([1, 2, 3]).write(to: source)
        try await journal.importOriginal(from: source, recordingId: recording.id, mediaId: media.id, accountKey: "account-a")
        let stored = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: "account-a")
        XCTAssertEqual(try Data(contentsOf: stored), Data([1, 2, 3]))
        XCTAssertEqual(stored.lastPathComponent, media.id.uuidString + ".original")
        do {
            try await journal.releaseUploadedOriginal(recordingId: recording.id, mediaId: media.id, accountKey: "account-a")
            XCTFail("An incomplete upload released its original")
        } catch { XCTAssertTrue(FileManager.default.fileExists(atPath: stored.path)) }
        do {
            _ = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: "account-b")
            XCTFail("Another account read a source")
        } catch { XCTAssertEqual(error as? TeaNodeError, .signedOut) }
        let other = try await journal.recordings(accountKey: "account-b")
        XCTAssertTrue(other.isEmpty)
        do {
            try await journal.removeLocalRecording(recording.id, accountKey: "account-b")
            XCTFail("Another account deleted a source")
        } catch { XCTAssertEqual(error as? TeaNodeError, .signedOut) }
    }

    func testWholeReceiptCannotClaimDurabilityWithDifferentDigestOrUnparseableTime() throws {
        let original = try media(receiptState: "pending", digest: String(repeating: "a", count: 64), verifiedAt: nil)
        let wrongDigest = try media(receiptState: "complete", digest: String(repeating: "b", count: 64), verifiedAt: "2030-01-02T03:04:05Z")
        let badTime = try media(receiptState: "complete", digest: String(repeating: "a", count: 64), verifiedAt: "garbage")
        let complete = try media(receiptState: "complete", digest: String(repeating: "a", count: 64), verifiedAt: "2030-01-02T03:04:05Z")
        XCTAssertFalse(wrongDigest.confirmsOriginal(original))
        XCTAssertFalse(badTime.confirmsOriginal(original))
        XCTAssertTrue(complete.confirmsOriginal(original))
    }

    func testUploadValidatesReceiptAndDeletedSourceDoesNotRetryAsSuccess() async throws {
        let digest = String(repeating: "a", count: 64)
        let fake = FakeServer { _ in
            (
                200,
                "{\"recordingMediaId\":\"different\",\"partIndex\":0,\"partSizeBytes\":3,\"partSha256\":\"\(digest)\",\"storedAt\":\"2030-01-02T03:04:05Z\"}"
            )
        }
        let account = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "test-token", refreshToken: "",
            expiresAt: Date().addingTimeInterval(40 * 86400), username: "test")
        let connection = Connection(account: account, transport: fake)
        do {
            _ = try await connection.putRecordingPart(mediaId: "media-a", partIndex: 0, content: Data([1, 2, 3]), partSha256: digest)
            XCTFail("A mismatched receipt succeeded")
        } catch { XCTAssertEqual(error as? TeaNodeError, .unreadableAnswer) }
        XCTAssertEqual(fake.sent[0].value(forHTTPHeaderField: "Content-Length"), "3")
        XCTAssertEqual(fake.sent[0].value(forHTTPHeaderField: "X-Recording-Part-SHA256"), digest)
        let deleted = Connection(account: account, transport: FakeServer { _ in (410, "{}") })
        do {
            _ = try await deleted.completeRecordingUpload(media(receiptState: "pending", digest: digest, verifiedAt: nil))
            XCTFail("A deleted recording resumed")
        } catch { XCTAssertNotEqual(error as? TeaNodeError, .unreadableAnswer) }
    }

    private func media(receiptState: String, digest: String, verifiedAt: String?) throws -> RecordingMedia {
        let object: [String: Any] = [
            "id": "media-a", "recordingId": "recording-a", "clientMediaId": "client-media-a", "originalFilename": "audio.wav",
            "originalContentType": "audio/wav", "originalSizeBytes": 3, "originalSha256": digest, "timelineStartMS": 0, "mediaDurationMS": 1000,
            "partCount": 1, "recordingUploadState": receiptState, "verifiedAt": verifiedAt as Any? ?? NSNull(),
        ]
        return try JSONDecoder().decode(RecordingMedia.self, from: JSONSerialization.data(withJSONObject: object))
    }
}

extension RecordingTests {
    func testRemovedNewestLabelDoesNotRestoreOldIdentityOnRerunOrCrossMedia() throws {
        let correction: (Int, Bool, String) -> [String: Any] = { generation, isRemoved, identity in
            [
                "id": "correction-\(generation)", "recordingMediaId": "media-a", "recordingTranscriptionAttemptId": "old-attempt",
                "speakerIdentityId": identity,
                "localSpeakerId": "local-1", "passageStartMS": 1000, "passageEndMS": 5000, "sourceGeneration": generation, "isRemoved": isRemoved,
            ]
        }
        let object: [String: Any] = [
            "sourceGeneration": 3, "speakerIdentities": [["id": "identity-a", "confirmedName": "Speaker A"]],
            "speakerCorrections": [correction(1, false, "identity-a"), correction(3, true, "")], "voiceReferences": [],
        ]
        let speakers = try JSONDecoder().decode(RecordingSpeakersView.self, from: JSONSerialization.data(withJSONObject: object))
        let segmentObject: [String: Any] = [
            "id": "segment-b", "recordingTranscriptRunId": "rerun-b", "recordingTranscriptionAttemptId": "new-attempt", "localSpeakerId": "local-2",
            "segmentStartMS": 2000, "segmentEndMS": 4000, "transcriptText": "Source evidence",
        ]
        let segment = try JSONDecoder().decode(RecordingTranscriptSegment.self, from: JSONSerialization.data(withJSONObject: segmentObject))
        XCTAssertNil(speakers.confirmedName(for: segment, mediaId: "media-a"))
        XCTAssertNil(speakers.confirmedName(for: segment, mediaId: "media-b"))
        var confirmedObject = object
        confirmedObject["speakerCorrections"] = [correction(1, false, "identity-a")]
        let confirmed = try JSONDecoder().decode(RecordingSpeakersView.self, from: JSONSerialization.data(withJSONObject: confirmedObject))
        XCTAssertEqual(confirmed.confirmedName(for: segment, mediaId: "media-a"), "Speaker A")
        XCTAssertNil(confirmed.confirmedName(for: segment, mediaId: "media-b"))
    }
}

extension RecordingTests {
    @MainActor
    func testPaidRequestIdentitySurvivesRetryAndStaysWithinItsAccountAndMedia() {
        let suite = "recording-test-" + UUID().uuidString
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let first = RecordingRequestIdentity.acquire(accountKey: "account-a", mediaId: "media-a", in: defaults)
        XCTAssertEqual(RecordingRequestIdentity.acquire(accountKey: "account-a", mediaId: "media-a", in: defaults), first)
        XCTAssertNotEqual(RecordingRequestIdentity.acquire(accountKey: "account-b", mediaId: "media-a", in: defaults), first)
        XCTAssertNotEqual(RecordingRequestIdentity.acquire(accountKey: "account-a", mediaId: "media-b", in: defaults), first)
        RecordingRequestIdentity.finish(accountKey: "account-a", mediaId: "media-a", requestId: "unrelated", in: defaults)
        XCTAssertEqual(RecordingRequestIdentity.acquire(accountKey: "account-a", mediaId: "media-a", in: defaults), first)
        RecordingRequestIdentity.finish(accountKey: "account-a", mediaId: "media-a", requestId: first, in: defaults)
        XCTAssertNotEqual(RecordingRequestIdentity.acquire(accountKey: "account-a", mediaId: "media-a", in: defaults), first)
    }
}

extension RecordingTests {
    func testActivityRecoveryRequiresTheExactSessionAndRetainedAccount() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try RecordingJournal(directory: directory)
        var original = LocalRecording(accountKey: "profile-a", recordingTitle: "", recordingOrigin: "capture")
        try original.pause(at: 1000)
        let unrelated = LocalRecording(accountKey: "profile-b", recordingTitle: "", recordingOrigin: "capture")
        try await journal.create(original)
        try await journal.create(unrelated)
        let recovered = try await journal.recoverableRecording(
            localRecordingId: original.id.uuidString, retainedAccountKeys: ["profile-b", "profile-a"])
        XCTAssertEqual(recovered?.id, original.id)
        XCTAssertEqual(recovered?.recordingCaptureState, "paused")
        let removedAccount = try await journal.recoverableRecording(localRecordingId: original.id.uuidString, retainedAccountKeys: ["profile-b"])
        XCTAssertNil(removedAccount)
        let staleSession = try await journal.recoverableRecording(localRecordingId: UUID().uuidString, retainedAccountKeys: ["profile-a"])
        XCTAssertNil(staleSession)
        original.isDeletedRemotely = true
        try await journal.save(original)
        let deleted = try await journal.recoverableRecording(localRecordingId: original.id.uuidString, retainedAccountKeys: ["profile-a"])
        XCTAssertNil(deleted)
        original.isDeletedRemotely = false
        try original.stop(at: 2000)
        try await journal.save(original)
        let stopped = try await journal.recoverableRecording(localRecordingId: original.id.uuidString, retainedAccountKeys: ["profile-a"])
        XCTAssertNil(stopped)
    }
}

extension RecordingTests {
    func testLateUploadJournalSaveCannotRecreateAnExplicitlyRemovedLocalSource() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try RecordingJournal(directory: directory)
        var recording = LocalRecording(accountKey: "account-a", recordingTitle: "", recordingOrigin: "audioImport")
        try await journal.create(recording)
        try await journal.removeLocalRecording(recording.id, accountKey: recording.accountKey)
        recording.uploadErrorMessage = "A late transfer callback"
        do {
            try await journal.save(recording)
            XCTFail("A late callback recreated the deleted local source")
        } catch {}
        let recordings = try await journal.recordings(accountKey: "account-a")
        XCTAssertTrue(recordings.isEmpty)
    }
}

extension RecordingTests {
    func testRecordingCapabilityRequiresProviderEvidenceForAllRequiredFeatures() throws {
        let advertised = try JSONDecoder().decode(
            [RecordingModelCapability].self,
            from: Data(
                #"[{"name":"service:unknown-diarize"},{"name":"service:plain","canTranscribe":true,"hasSpeakerDiarization":false,"hasSegmentTimestamps":true},{"name":"service:confirmed","canTranscribe":true,"hasSpeakerDiarization":true,"hasSegmentTimestamps":true,"hasSpeakerReferences":true,"maximumAudioBytes":25000000,"maximumSpeakerReferenceCount":4}]"#
                    .utf8))
        XCTAssertFalse(advertised[0].canAttributeRecordingSpeakers)
        XCTAssertFalse(advertised[1].canAttributeRecordingSpeakers)
        XCTAssertTrue(advertised[2].canAttributeRecordingSpeakers)
        XCTAssertEqual(advertised[2].maximumSpeakerReferenceCount, 4)
    }
}

extension RecordingTests {
    func testLearningProgressUsesExistingSourceMetadataAndAllowsNoAgentOrSource() throws {
        let pending = try JSONDecoder().decode(
            RecordingLearningView.self,
            from: Data(
                #"{"sourceGeneration":2,"hasAgent":false,"knowledgeSource":null,"pendingDocumentCount":0,"digestedDocumentCount":0,"lastPublishedSourceGeneration":0,"isCurationPending":false}"#
                    .utf8))
        XCTAssertFalse(pending.hasAgent)
        XCTAssertNil(pending.knowledgeSource)
        let published = try JSONDecoder().decode(
            RecordingLearningView.self,
            from: Data(
                #"{"sourceGeneration":3,"hasAgent":true,"knowledgeSource":{"id":"source-a","name":"Recording evidence","kind":"recording","enabled":true,"documentCount":4,"chunkCount":4},"pendingDocumentCount":1,"digestedDocumentCount":3,"lastPublishedSourceGeneration":2,"isCurationPending":true}"#
                    .utf8))
        XCTAssertEqual(published.knowledgeSource?.kind, "recording")
        XCTAssertEqual(published.pendingDocumentCount, 1)
        XCTAssertEqual(published.digestedDocumentCount, 3)
        XCTAssertTrue(published.isCurationPending)
    }
}
