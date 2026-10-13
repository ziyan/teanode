import XCTest

@testable import TeaNodeKit

final class RecordingLibraryProgressTests: XCTestCase {
    private func published(_ recordingId: String, clientRecordingId: String = "client-new", userId: String = "owner-a") -> Recording {
        Recording(
            id: recordingId, userId: userId, createdAt: "2000-01-01T00:00:00Z", updatedAt: "2000-01-01T00:00:00Z",
            clientRecordingId: clientRecordingId, recordingTitle: "Meeting", recordingOrigin: "audioImport",
            recordingCaptureState: "stopped", timelineDurationMS: 1000, sourceGeneration: 1, captureGaps: [])
    }

    func testVerifiedUploadRemovesLocalPlaybackButBadReceiptKeepsIt() {
        var local = LocalRecording(accountKey: "profile-a", recordingTitle: "Meeting", recordingOrigin: "audioImport", recordingUserId: "owner-a")
        var media = LocalRecordingMedia(originalFilename: "meeting.wav", originalContentType: "audio/wav", timelineStartMS: 0)
        media.originalSizeBytes = 3
        media.originalSha256 = String(repeating: "a", count: 64)
        let manifest = RecordingMedia(
            id: "media-a", recordingId: "recording-new", clientMediaId: media.id.uuidString,
            originalFilename: "meeting.wav", originalContentType: "audio/wav", originalSizeBytes: 3,
            originalSha256: String(repeating: "a", count: 64), timelineStartMS: 0, mediaDurationMS: 1000,
            partCount: 1, recordingUploadState: "pending", verifiedAt: nil)
        media.mediaManifest = manifest
        local.localMedia = [media]
        XCTAssertTrue(RecordingLibraryProgress.canPlayLocalOriginal(local))
        XCTAssertEqual(RecordingLibraryProgress.pendingLocalRecordingIds([local]), [local.id])
        func receipt(digest: String) -> RecordingMedia {
            RecordingMedia(
                id: manifest.id, recordingId: manifest.recordingId, clientMediaId: manifest.clientMediaId,
                originalFilename: manifest.originalFilename, originalContentType: manifest.originalContentType,
                originalSizeBytes: manifest.originalSizeBytes, originalSha256: digest, timelineStartMS: 0, mediaDurationMS: 1000,
                partCount: 1, recordingUploadState: "complete", verifiedAt: "2000-01-01T00:00:00Z")
        }
        local.localMedia[0].completionReceipt = receipt(digest: String(repeating: "b", count: 64))
        XCTAssertTrue(RecordingLibraryProgress.canPlayLocalOriginal(local))
        local.localMedia[0].completionReceipt = receipt(digest: manifest.originalSha256)
        XCTAssertFalse(RecordingLibraryProgress.canPlayLocalOriginal(local))
        XCTAssertTrue(RecordingLibraryProgress.pendingLocalRecordingIds([local]).isEmpty)
    }

    func testNewOwnedItemPreservesLoadedPagesAndDoesNotDuplicate() {
        let pages = (0..<100).reversed().map { published(String(format: "page-%03d", $0), clientRecordingId: "client-\($0)") }
        var local = LocalRecording(accountKey: "profile-a", recordingTitle: "Meeting", recordingOrigin: "audioImport", recordingUserId: "owner-a")
        local.recordingId = "recording-new"
        let newest = published("recording-new", clientRecordingId: local.clientRecordingId)
        let merged = RecordingLibraryProgress.mergingPublishedRecording(
            newest, into: pages, from: local,
            accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a")
        XCTAssertEqual(merged.count, 101)
        XCTAssertEqual(Array(merged.dropFirst()), pages)
        XCTAssertEqual(
            RecordingLibraryProgress.mergingPublishedRecording(
                newest, into: merged, from: local,
                accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a"), merged)
    }

    func testForeignLogoutAndRecreatedOwnerResultsAreIgnored() {
        let pages = [published("m-existing")]
        var local = LocalRecording(accountKey: "profile-a", recordingTitle: "Meeting", recordingOrigin: "audioImport", recordingUserId: "owner-a")
        local.recordingId = "recording-new"
        for currentOwner in [String?.none, "owner-b"] {
            let merged = RecordingLibraryProgress.mergingPublishedRecording(
                published("recording-new", clientRecordingId: local.clientRecordingId), into: pages, from: local,
                accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: currentOwner == nil ? nil : "profile-a",
                currentRecordingUserId: currentOwner)
            XCTAssertEqual(merged, pages)
        }
        let foreign = RecordingLibraryProgress.mergingPublishedRecording(
            published("recording-new", clientRecordingId: local.clientRecordingId, userId: "foreign-owner"), into: pages, from: local,
            accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a")
        XCTAssertEqual(foreign, pages)
        let wrongOriginal = RecordingLibraryProgress.mergingPublishedRecording(
            published("recording-new", clientRecordingId: "unrelated-original"), into: pages, from: local,
            accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a")
        XCTAssertEqual(wrongOriginal, pages)
    }
    func testOlderHistoryStaysBehindPaginationAndCanceledResultIsIgnored() {
        let pages = [published("m-existing")]
        var local = LocalRecording(accountKey: "profile-a", recordingTitle: "Meeting", recordingOrigin: "audioImport", recordingUserId: "owner-a")
        local.recordingId = "a-older"
        let older = Recording(
            id: "a-older", userId: "owner-a", createdAt: "2099-01-01T00:00:00Z", updatedAt: "2099-01-01T00:00:00Z",
            clientRecordingId: local.clientRecordingId, recordingTitle: "Meeting", recordingOrigin: "audioImport",
            recordingCaptureState: "stopped", timelineDurationMS: 1000, sourceGeneration: 1, captureGaps: [])
        XCTAssertEqual(
            RecordingLibraryProgress.mergingPublishedRecording(
                older, into: pages, from: local,
                accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a",
                hasMoreRecordings: true), pages)
        XCTAssertEqual(
            RecordingLibraryProgress.mergingPublishedRecording(
                older, into: pages, from: local,
                accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a",
                hasMoreRecordings: false), pages + [older])
        XCTAssertEqual(
            RecordingLibraryProgress.mergingPublishedRecording(
                older, into: pages, from: local,
                accountKey: "profile-a", recordingUserId: "owner-a", currentAccountKey: "profile-a", currentRecordingUserId: "owner-a",
                hasMoreRecordings: false, isCancelled: true), pages)
    }

}
