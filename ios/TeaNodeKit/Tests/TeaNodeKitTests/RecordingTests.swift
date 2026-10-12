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

    private func media(receiptState: String, digest: String, verifiedAt: String?, clientMediaId: String = "client-media-a") throws -> RecordingMedia {
        let object: [String: Any] = [
            "id": "media-a", "recordingId": "recording-a", "clientMediaId": clientMediaId, "originalFilename": "audio.wav",
            "originalContentType": "audio/wav", "originalSizeBytes": 3, "originalSha256": digest, "timelineStartMS": 0, "mediaDurationMS": 1000,
            "partCount": 1, "recordingUploadState": receiptState, "verifiedAt": verifiedAt as Any? ?? NSNull(),
        ]
        return try JSONDecoder().decode(RecordingMedia.self, from: JSONSerialization.data(withJSONObject: object))
    }
}

extension RecordingTests {
    func testCorrectionMetadataCannotPromoteAnUnconfirmedGeneratedSpeaker() throws {
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
        XCTAssertFalse(speakers.speakerCorrections.isEmpty)
        XCTAssertNil(segment.confirmedSpeakerName)
        var confirmedObject = object
        confirmedObject["speakerCorrections"] = [correction(1, false, "identity-a")]
        let confirmed = try JSONDecoder().decode(RecordingSpeakersView.self, from: JSONSerialization.data(withJSONObject: confirmedObject))
        XCTAssertEqual(confirmed.speakerIdentities.first?.confirmedName, "Speaker A")
        XCTAssertNil(segment.confirmedSpeakerName)
        XCTAssertNil(segment.confirmedSpeakerName)
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
        var original = LocalRecording(accountKey: "profile-a", recordingTitle: "", recordingOrigin: "capture", recordingUserId: "owner-a")
        try original.pause(at: 1000)
        let unrelated = LocalRecording(accountKey: "profile-b", recordingTitle: "", recordingOrigin: "capture", recordingUserId: "owner-b")
        try await journal.create(original)
        try await journal.create(unrelated)
        let recovered = try await journal.recoverableRecording(
            localRecordingId: original.id.uuidString, retainedOwners: ["profile-b": "owner-b", "profile-a": "owner-a"])
        XCTAssertEqual(recovered?.id, original.id)
        XCTAssertEqual(recovered?.recordingCaptureState, "paused")
        let removedAccount = try await journal.recoverableRecording(
            localRecordingId: original.id.uuidString, retainedOwners: ["profile-b": "owner-b"])
        XCTAssertNil(removedAccount)
        let staleSession = try await journal.recoverableRecording(localRecordingId: UUID().uuidString, retainedOwners: ["profile-a": "owner-a"])
        XCTAssertNil(staleSession)
        original.isDeletedRemotely = true
        try await journal.save(original)
        let deleted = try await journal.recoverableRecording(localRecordingId: original.id.uuidString, retainedOwners: ["profile-a": "owner-a"])
        XCTAssertNil(deleted)
        original.isDeletedRemotely = false
        try original.stop(at: 2000)
        try await journal.save(original)
        let stopped = try await journal.recoverableRecording(localRecordingId: original.id.uuidString, retainedOwners: ["profile-a": "owner-a"])
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

extension RecordingTests {
    func testPartialNewerCorrectionCannotLabelAnEntireGeneratedStatement() throws {
        let speakers = try JSONDecoder().decode(
            RecordingSpeakersView.self,
            from: Data(
                #"{"sourceGeneration":3,"speakerIdentities":[{"id":"identity-a","confirmedName":"Speaker A"},{"id":"identity-b","confirmedName":"Speaker B"}],"speakerCorrections":[{"id":"old","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"old-attempt","speakerIdentityId":"identity-a","localSpeakerId":"old-local","passageStartMS":1000,"passageEndMS":5000,"sourceGeneration":1,"isRemoved":false},{"id":"new","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"new-attempt","speakerIdentityId":"identity-b","localSpeakerId":"new-local","passageStartMS":2000,"passageEndMS":3000,"sourceGeneration":3,"isRemoved":false}],"voiceReferences":[]}"#
                    .utf8))
        let segment = try JSONDecoder().decode(
            RecordingTranscriptSegment.self,
            from: Data(
                #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"rerun-attempt","localSpeakerId":"local-a","segmentStartMS":1000,"segmentEndMS":5000,"transcriptText":"One statement with corrected attribution inside it."}"#
                    .utf8))
        XCTAssertFalse(speakers.speakerCorrections.isEmpty)
        XCTAssertNil(segment.confirmedSpeakerName)
    }
}

extension RecordingTests {
    func testReceiptJournalSurvivesRestartBeforeIdempotentOriginalRelease() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try RecordingJournal(directory: directory)
        var recording = LocalRecording(accountKey: "account-a", recordingTitle: "", recordingOrigin: "audioImport")
        var original = LocalRecordingMedia(originalFilename: "audio.wav", originalContentType: "audio/wav", timelineStartMS: 0)
        original.originalSizeBytes = 3
        original.originalSha256 = String(repeating: "a", count: 64)
        recording.localMedia.append(original)
        try await journal.create(recording)
        let source = directory.appendingPathComponent("test-original")
        try Data([1, 2, 3]).write(to: source)
        try await journal.importOriginal(from: source, recordingId: recording.id, mediaId: original.id, accountKey: recording.accountKey)
        recording.localMedia[0].mediaManifest = try media(
            receiptState: "pending", digest: original.originalSha256!, verifiedAt: nil, clientMediaId: original.id.uuidString)
        recording.localMedia[0].completionReceipt = try media(
            receiptState: "complete", digest: original.originalSha256!, verifiedAt: "2030-01-02T03:04:05Z", clientMediaId: original.id.uuidString)
        try await journal.save(recording)
        let reopened = try RecordingJournal(directory: directory)
        let kept = try await reopened.read(recording.id, accountKey: recording.accountKey)
        XCTAssertTrue(kept.localMedia[0].isDurablyUploaded)
        let originalURL = try await reopened.originalURL(recordingId: recording.id, mediaId: original.id, accountKey: recording.accountKey)
        XCTAssertTrue(FileManager.default.fileExists(atPath: originalURL.path))
        try await reopened.releaseUploadedOriginal(recordingId: recording.id, mediaId: original.id, accountKey: recording.accountKey)
        XCTAssertFalse(FileManager.default.fileExists(atPath: originalURL.path))
        try await reopened.releaseUploadedOriginal(recordingId: recording.id, mediaId: original.id, accountKey: recording.accountKey)
    }
}

extension RecordingTests {
    func testMalformedManifestIsRefusedBeforeAnyOriginalBytesAreSent() async throws {
        let digest = String(repeating: "a", count: 64)
        let fake = FakeServer { _ in
            (
                200,
                "{\"data\":{\"CreateRecordingMedia\":{\"id\":\"media-a\",\"recordingId\":\"recording-a\",\"clientMediaId\":\"client-a\",\"originalFilename\":\"audio.wav\",\"originalContentType\":\"audio/wav\",\"originalSizeBytes\":9,\"originalSha256\":\"\(digest)\",\"timelineStartMS\":0,\"mediaDurationMS\":1000,\"partCount\":1,\"recordingUploadState\":\"pending\",\"verifiedAt\":null}}}"
            )
        }
        let account = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "test-token", refreshToken: "",
            expiresAt: Date().addingTimeInterval(40 * 86400), username: "test")
        let connection = Connection(account: account, transport: fake)
        do {
            _ = try await connection.createRecordingMedia(
                recordingId: "recording-a", clientMediaId: "client-a", originalFilename: "audio.wav", originalContentType: "audio/wav",
                originalSizeBytes: 3, originalSha256: digest, timelineStartMS: 0, mediaDurationMS: 1000)
            XCTFail("A server manifest with the wrong size was accepted")
        } catch { XCTAssertEqual(error as? TeaNodeError, .unreadableAnswer) }
        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertEqual(fake.sent[0].httpMethod, "POST")
        XCTAssertEqual(fake.sent[0].url?.path, "/api/v1/graphql")
    }
}

extension RecordingTests {
    @MainActor
    func testTextDraftRetainsExactSubmittedRequestAndSeparatesAccounts() throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        var draft = RecordingTextCorrectionDraft(
            accountKey: "account-a", recordingId: "recording-a", recordingTranscriptSegmentId: "segment-a",
            correctedTranscriptText: "Corrected words", expectedSourceGeneration: 4)
        draft.hasSubmittedRequest = true
        try RecordingTextCorrectionDrafts.save(draft, in: defaults)
        let reopened = RecordingTextCorrectionDrafts.read(accountKey: "account-a", segmentId: "segment-a", in: defaults)
        XCTAssertEqual(reopened, draft)
        XCTAssertNil(RecordingTextCorrectionDrafts.read(accountKey: "account-b", segmentId: "segment-a", in: defaults))
        let replacement = RecordingTextCorrectionDraft(
            accountKey: "account-a", recordingId: "recording-a", recordingTranscriptSegmentId: "segment-a",
            correctedTranscriptText: "Newer draft", expectedSourceGeneration: 5)
        try RecordingTextCorrectionDrafts.save(replacement, in: defaults)
        RecordingTextCorrectionDrafts.finish(draft, in: defaults)
        XCTAssertEqual(RecordingTextCorrectionDrafts.read(accountKey: "account-a", segmentId: "segment-a", in: defaults), replacement)
        RecordingTextCorrectionDrafts.finish(replacement, in: defaults)
        XCTAssertNil(RecordingTextCorrectionDrafts.read(accountKey: "account-a", segmentId: "segment-a", in: defaults))
    }

    func testEffectiveTranscriptDoesNotReplaceOriginalEvidence() throws {
        let segment = try JSONDecoder().decode(
            RecordingTranscriptSegment.self,
            from: Data(
                #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"local-a","segmentStartMS":1000,"segmentEndMS":5000,"transcriptText":"Generated words","effectiveTranscriptText":"Corrected words","appliedTextCorrectionId":"correction-a"}"#
                    .utf8))
        XCTAssertEqual(segment.transcriptText, "Generated words")
        XCTAssertEqual(segment.displayedTranscriptText, "Corrected words")
        XCTAssertEqual(segment.appliedTextCorrectionId, "correction-a")
    }

    @MainActor
    func testMismatchedTextCorrectionReceiptCannotReleaseDraft() async throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        let fake = FakeServer { _ in
            (
                200,
                #"{"data":{"CorrectRecordingTranscriptText":{"id":"correction-a","recordingId":"wrong-recording","recordingMediaId":"media-a","recordingTranscriptRunId":"run-a","recordingTranscriptSegmentId":"segment-a","clientCorrectionId":"wrong-request","passageStartMS":0,"passageEndMS":1000,"originalTranscriptText":"Generated words","correctedTranscriptText":"Corrected words","sourceGeneration":5,"createdAt":"2030-01-02T03:04:05Z","isRemoved":false}}}"#
            )
        }
        let account = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "test-token", refreshToken: "",
            expiresAt: Date().addingTimeInterval(40 * 86400), username: "test")
        let connection = Connection(account: account, transport: fake)
        var draft = RecordingTextCorrectionDraft(
            accountKey: connection.accountKey, recordingId: "recording-a", recordingTranscriptSegmentId: "segment-a",
            correctedTranscriptText: "Corrected words", expectedSourceGeneration: 4)
        draft.hasSubmittedRequest = true
        try RecordingTextCorrectionDrafts.save(draft, in: defaults)
        do {
            _ = try await connection.correctRecordingTranscriptText(draft)
            XCTFail("Mismatched correction receipt was accepted")
        } catch { XCTAssertEqual(error as? TeaNodeError, .unreadableAnswer) }
        XCTAssertEqual(RecordingTextCorrectionDrafts.read(accountKey: connection.accountKey, segmentId: "segment-a", in: defaults), draft)
        XCTAssertEqual(fake.sent.count, 1)
    }
}

extension RecordingTests {
    @MainActor
    func testSpeakerDraftRetainsRequestLocalGroupAndOriginalPassageAcrossRelaunch() throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        let segment = try JSONDecoder().decode(
            RecordingTranscriptSegment.self,
            from: Data(
                #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"local-a","segmentStartMS":1000,"segmentEndMS":5000,"transcriptText":"Source words"}"#
                    .utf8))
        var draft = RecordingSpeakerDraft(accountKey: "account-a", recordingId: "recording-a", segment: segment, expectedSourceGeneration: 4)
        draft.confirmedName = "Speaker A"
        draft.isWholeGroup = true
        draft.hasSubmittedRequest = true
        try RecordingSpeakerDrafts.save(draft, in: defaults)
        XCTAssertEqual(RecordingSpeakerDrafts.read(accountKey: "account-a", segmentId: "segment-a", in: defaults), draft)
        XCTAssertNil(RecordingSpeakerDrafts.read(accountKey: "account-b", segmentId: "segment-a", in: defaults))
        let newer = RecordingSpeakerDraft(accountKey: "account-a", recordingId: "recording-a", segment: segment, expectedSourceGeneration: 5)
        try RecordingSpeakerDrafts.save(newer, in: defaults)
        RecordingSpeakerDrafts.finish(draft, in: defaults)
        XCTAssertEqual(RecordingSpeakerDrafts.read(accountKey: "account-a", segmentId: "segment-a", in: defaults), newer)
    }
}

extension RecordingTests {
    func testOnlyCanonicalConfirmedProjectionNamesTheGeneratedSpeaker() throws {
        let base: [String: Any] = [
            "id": "segment-a", "recordingTranscriptRunId": "run-a", "recordingTranscriptionAttemptId": "attempt-a", "localSpeakerId": "local-a",
            "segmentStartMS": 1000, "segmentEndMS": 5000, "transcriptText": "Evidence", "effectiveSpeakerIdentityId": "identity-a",
            "effectiveSpeakerName": "Speaker A",
        ]
        let withoutConfirmation = try JSONDecoder().decode(RecordingTranscriptSegment.self, from: JSONSerialization.data(withJSONObject: base))
        XCTAssertNil(withoutConfirmation.confirmedSpeakerName)
        var confirmed = base
        confirmed["hasUserConfirmedSpeaker"] = true
        let projection = try JSONDecoder().decode(RecordingTranscriptSegment.self, from: JSONSerialization.data(withJSONObject: confirmed))
        XCTAssertEqual(projection.confirmedSpeakerName, "Speaker A")
        confirmed["effectiveSpeakerIdentityId"] = ""
        let missingIdentity = try JSONDecoder().decode(RecordingTranscriptSegment.self, from: JSONSerialization.data(withJSONObject: confirmed))
        XCTAssertNil(missingIdentity.confirmedSpeakerName)
    }
}

extension RecordingTests {
    func testRecreatedUsernameCannotUploadOrRecoverAnotherServerUsersLocalOriginal() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try RecordingJournal(directory: directory)
        let account = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "test-token", refreshToken: "",
            expiresAt: Date().addingTimeInterval(40 * 86400), username: "same-name")
        var recording = LocalRecording(accountKey: account.accountKey, recordingTitle: "", recordingOrigin: "capture", recordingUserId: "old-user")
        let original = LocalRecordingMedia(originalFilename: "audio.wav", originalContentType: "audio/wav", timelineStartMS: 0)
        recording.localMedia.append(original)
        try recording.pause(at: 1000)
        try await journal.create(recording)
        let source = directory.appendingPathComponent("test-original")
        try Data([1, 2, 3]).write(to: source)
        try await journal.importOriginal(from: source, recordingId: recording.id, mediaId: original.id, accountKey: account.accountKey)
        let fake = FakeServer { _ in (200, #"{"data":{"GetSession":{"userId":"new-user"}}}"#) }
        let connection = Connection(account: account, transport: fake, store: MemoryAccountStore([account]))
        do {
            _ = try await connection.verifiedRecordingOwner(recording)
            XCTFail("A recreated username rebound another user's original")
        } catch {
            guard let refusal = error as? TeaNodeError, case .refused = refusal else { return XCTFail("Unexpected ownership error: \(error)") }
        }
        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertTrue(String(decoding: fake.sent[0].httpBody ?? Data(), as: UTF8.self).contains("GetSession"))
        let recovered = try await journal.recoverableRecording(
            localRecordingId: recording.id.uuidString, retainedOwners: [account.accountKey: "new-user"])
        XCTAssertNil(recovered)
        let originalURL = try await journal.originalURL(recordingId: recording.id, mediaId: original.id, accountKey: account.accountKey)
        XCTAssertEqual(try Data(contentsOf: originalURL), Data([1, 2, 3]))
        XCTAssertFalse(recording.belongsTo(accountKey: account.accountKey, recordingUserId: "new-user"))
        XCTAssertFalse(
            LocalRecording(accountKey: account.accountKey, recordingTitle: "", recordingOrigin: "capture").belongsTo(
                accountKey: account.accountKey, recordingUserId: "new-user"))
    }

    func testKnownRecordingOwnerCannotAdoptNewSameUsernameCredentials() async throws {
        var original = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "old-token", refreshToken: "", expiresAt: Date(),
            username: "same-name")
        original.cachedRecordingUserId = "old-user"
        var replacement = original
        replacement.accessToken = "new-token"
        replacement.expiresAt = Date().addingTimeInterval(40 * 86400)
        replacement.cachedRecordingUserId = "new-user"
        let fake = FakeServer { _ in (200, "{}") }
        let connection = Connection(account: original, transport: fake, store: MemoryAccountStore([replacement]))
        do {
            _ = try await connection.accessToken()
            XCTFail("An old connection adopted a different server user")
        } catch { XCTAssertEqual(error as? TeaNodeError, .signedOut) }
        XCTAssertTrue(fake.sent.isEmpty)
    }
}

extension RecordingTests {
    func testPassagePlaybackUsesOriginalOffsetInsteadOfAbsoluteMeetingTime() throws {
        var object =
            try JSONSerialization.jsonObject(
                with: JSONEncoder().encode(media(receiptState: "pending", digest: String(repeating: "a", count: 64), verifiedAt: nil)))
            as! [String: Any]
        object["timelineStartMS"] = 600000
        let original = try JSONDecoder().decode(RecordingMedia.self, from: JSONSerialization.data(withJSONObject: object))
        XCTAssertEqual(original.playbackOffsetMS(for: 602000), 2000)
        XCTAssertEqual(original.playbackOffsetMS(for: 605000), 5000)
        XCTAssertEqual(original.playbackOffsetMS(for: 3000), 0)
    }
}

extension RecordingTests {
    @MainActor
    func testAttachmentImportDraftSurvivesUnknownResponseWithoutCrossAccountReuse() throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        let draft = RecordingAttachmentImportDraft(accountKey: "account-a", agentAttachmentId: "attachment-a", recordingTitle: "Discussion")
        try RecordingAttachmentImportDrafts.save(draft, in: defaults)
        XCTAssertEqual(RecordingAttachmentImportDrafts.read(accountKey: "account-a", attachmentId: "attachment-a", in: defaults), draft)
        XCTAssertNil(RecordingAttachmentImportDrafts.read(accountKey: "account-b", attachmentId: "attachment-a", in: defaults))
        let newer = RecordingAttachmentImportDraft(accountKey: "account-a", agentAttachmentId: "attachment-a", recordingTitle: "New discussion")
        try RecordingAttachmentImportDrafts.save(newer, in: defaults)
        RecordingAttachmentImportDrafts.finish(draft, in: defaults)
        XCTAssertEqual(RecordingAttachmentImportDrafts.read(accountKey: "account-a", attachmentId: "attachment-a", in: defaults), newer)
    }

    func testAutomaticRecordingSourceTitleComesFromSettingsInsteadOfStableIdentity() throws {
        let source = try JSONDecoder().decode(
            KnowledgeSource.self,
            from: Data(
                #"{"id":"source-a","kind":"recording","name":"recording:recording-a","enabled":true,"documentCount":1,"chunkCount":1,"more":false,"lastError":"","specification":{"settings":{"recordingId":"recording-a","recordingTitle":"Discussion"}}}"#
                    .utf8))
        XCTAssertEqual(source.recordingTitle, "Discussion")
        XCTAssertEqual(source.name, "recording:recording-a")
    }
}

extension RecordingTests {
    func testOwnerVerificationCannotOverwriteAConcurrentSameUsernameSignIn() async throws {
        var original = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "old-token", refreshToken: "old-refresh",
            expiresAt: Date().addingTimeInterval(40 * 86400), username: "same-name")
        original.cachedRecordingUserId = "old-user"
        var replacement = original
        replacement.accessToken = "new-token"
        replacement.refreshToken = "new-refresh"
        replacement.cachedRecordingUserId = "new-user"
        let store = MemoryAccountStore([original])
        let newAccount = replacement
        let fake = FakeServer { _ in
            try? store.save(newAccount)
            return (200, #"{"data":{"GetSession":{"userId":"old-user"}}}"#)
        }
        let connection = Connection(account: original, transport: fake, store: store)
        do {
            _ = try await connection.recordingOwnerId()
            XCTFail("Owner verification replaced a newer sign-in")
        } catch { XCTAssertEqual(error as? TeaNodeError, .signedOut) }
        XCTAssertEqual(try store.load(accountKey: original.accountKey), replacement)
    }
}

extension RecordingTests {
    func testInvalidImportedDurationStaysUnknownWithoutIntegerConversionTrap() {
        XCTAssertEqual(RecordingClock.durationMS(seconds: 45), 45000)
        XCTAssertEqual(RecordingClock.durationMS(seconds: .infinity), 0)
        XCTAssertEqual(RecordingClock.durationMS(seconds: .nan), 0)
        XCTAssertEqual(RecordingClock.durationMS(seconds: .greatestFiniteMagnitude), 0)
        XCTAssertEqual(RecordingClock.durationMS(seconds: -1), 0)
    }
}

extension RecordingTests {
    func testRecordingSourceScheduleKeepsInternalNameAndOmitsForbiddenFields() async throws {
        let source = try JSONDecoder().decode(
            KnowledgeSource.self,
            from: Data(
                #"{"id":"source-a","kind":"recording","name":"recording:recording-a","enabled":true,"documentCount":1,"chunkCount":1,"more":false,"lastError":"","specification":{"settings":{"recordingId":"recording-a","recordingTitle":"Discussion"}}}"#
                    .utf8))
        let fake = FakeServer { _ in (200, #"{"data":{"SaveAgentKnowledgeSource":{"id":"source-a"}}}"#) }
        let account = Account(
            server: try ServerAddress(parsing: "example.com"), clientId: "client-a", accessToken: "test-token", refreshToken: "",
            expiresAt: Date().addingTimeInterval(40 * 86400), username: "test")
        let connection = Connection(account: account, transport: fake)
        try await connection.saveRecordingSourceSchedule(source, cron: "17 3 * * *")
        try await connection.saveRecordingSourceSchedule(source, cron: "")
        for request in fake.sent {
            let body = try XCTUnwrap(request.httpBody)
            let requestObject = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            let variables = try XCTUnwrap(requestObject["variables"] as? [String: Any])
            XCTAssertEqual(variables["sourceId"] as? String, source.id)
            XCTAssertEqual(variables["name"] as? String, source.name)
            XCTAssertTrue(Set(variables.keys).isSubset(of: ["sourceId", "name", "cron"]))
        }
        let firstBody = try XCTUnwrap(fake.sent.first?.httpBody)
        let firstRequest = try XCTUnwrap(JSONSerialization.jsonObject(with: firstBody) as? [String: Any])
        XCTAssertEqual((firstRequest["variables"] as? [String: Any])?["cron"] as? String, "17 3 * * *")
        let lastBody = try XCTUnwrap(fake.sent.last?.httpBody)
        let lastRequest = try XCTUnwrap(JSONSerialization.jsonObject(with: lastBody) as? [String: Any])
        XCTAssertEqual((lastRequest["variables"] as? [String: Any])?["cron"] as? String, "")
    }
}
