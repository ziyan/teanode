import Foundation
import XCTest

@testable import TeaNodeKit

final class RecordingPlaybackTests: XCTestCase {
    private var media: RecordingMedia {
        RecordingMedia(
            id: "media-a", recordingId: "recording-a", clientMediaId: "original-a", originalFilename: "meeting.caf",
            originalContentType: "audio/x-caf", originalSizeBytes: 32000, originalSha256: String(repeating: "a", count: 64),
            timelineStartMS: 600000, mediaDurationMS: 1000, partCount: 1, recordingUploadState: "complete", verifiedAt: "2000-01-01T00:00:00Z")
    }
    private var playbackJSON: String {
        #"{"recordingMediaId":"media-a","playbackPath":"/api/v1/recordings/media/media-a/playback","playbackContentType":"audio/wav","playbackSizeBytes":32044,"playbackSha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","playbackDurationMS":1000,"timelineStartMS":600000}"#
    }
    private func connection(answer: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, answer) }
        let account = Account(
            server: try ServerAddress(parsing: "node.example.com"), clientId: "test-client", accessToken: "test-token", refreshToken: "",
            expiresAt: .distantFuture, username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }
    func testPendingReadDoesNotPrepareAndReadyReceiptUsesOwnedAuthenticatedRoute() async throws {
        let (pending, pendingServer) = try connection(answer: #"{"data":{"ReadRecordingPlayback":null}}"#)
        let missing = try await pending.recordingPlayback(for: media)
        XCTAssertNil(missing)
        XCTAssertEqual(pendingServer.sent.count, 1)
        let (ready, _) = try connection(answer: "{\"data\":{\"ReadRecordingPlayback\":\(playbackJSON)}}")
        let candidate = try await ready.recordingPlayback(for: media)
        let playback = try XCTUnwrap(candidate)
        let request = try await ready.recordingCompatiblePlaybackRequest(playback, for: media)
        XCTAssertEqual(request.url?.absoluteString, "https://node.example.com/api/v1/recordings/media/media-a/playback")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer test-token")
        XCTAssertEqual(media.playbackOffsetMS(for: playback.timelineStartMS + 700), 700)
    }
    func testForeignMediaPathAndClockSubstitutionsNeverProduceDownloadRequest() async throws {
        let substitutions = [
            playbackJSON.replacingOccurrences(of: "\"recordingMediaId\":\"media-a\"", with: "\"recordingMediaId\":\"media-other\""),
            playbackJSON.replacingOccurrences(of: "/api/v1/recordings/media/media-a/playback", with: "https://foreign.example.com/playback"),
            playbackJSON.replacingOccurrences(of: "600000", with: "0"),
            playbackJSON.replacingOccurrences(of: "32044", with: "0"),
            playbackJSON.replacingOccurrences(of: "audio/wav", with: "text/plain"),
        ]
        let (connection, fake) = try connection(answer: #"{"data":{}}"#)
        for substitution in substitutions {
            let playback = try JSONDecoder().decode(RecordingPlayback.self, from: Data(substitution.utf8))
            do {
                _ = try await connection.recordingCompatiblePlaybackRequest(playback, for: media)
                XCTFail("substituted preview accepted")
            } catch {}
        }
        XCTAssertTrue(fake.sent.isEmpty)
    }
    func testExplicitPreparationValidatesReturnedDescriptorAndPreservesRefusal() async throws {
        let (connection, fake) = try connection(answer: "{\"data\":{\"PrepareRecordingPlayback\":\(playbackJSON)}}")
        let playback = try await connection.prepareRecordingPlayback(for: media)
        XCTAssertTrue(playback.confirmsMedia(media))
        XCTAssertEqual(fake.sent.count, 1)
        for answer in [#"{"errors":[{"message":"not permitted"}]}"#, #"{"data":{"PrepareRecordingPlayback":null}}"#] {
            let (refused, _) = try self.connection(answer: answer)
            do {
                _ = try await refused.prepareRecordingPlayback(for: media)
                XCTFail("missing or refused preview became ready")
            } catch {}
        }
    }
}
