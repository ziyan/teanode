import XCTest

@testable import TeaNodeKit

final class RecordingTimelinePiecesTests: XCTestCase {
    private func media(_ id: String, startMS: Int64, durationMS: Int64, state: String = "complete") -> RecordingMedia {
        RecordingMedia(
            id: id, recordingId: "recording-a", clientMediaId: "original-\(id)", originalFilename: "\(id).m4a",
            originalContentType: "audio/mp4", originalSizeBytes: 1000, originalSha256: String(repeating: "a", count: 64),
            timelineStartMS: startMS, mediaDurationMS: durationMS, partCount: 1, recordingUploadState: state,
            verifiedAt: "2000-01-01T00:00:00Z")
    }

    /// Two originals with a ten second gap between them: 0 to 60 s, then 70 to 100 s. One more is still uploading.
    private var pieces: [RecordingMedia] {
        [
            media("second", startMS: 70000, durationMS: 30000), media("first", startMS: 0, durationMS: 60000),
            media("pending", startMS: 5, durationMS: 5, state: "uploading"),
        ]
    }

    func testPositionMapsToTheOriginalAndOffset() {
        XCTAssertEqual(RecordingTimelinePieces.locate(timelineMS: 20000, in: pieces)?.media.id, "first")
        XCTAssertEqual(RecordingTimelinePieces.locate(timelineMS: 20000, in: pieces)?.offsetMS, 20000)
        let second = RecordingTimelinePieces.locate(timelineMS: 75000, in: pieces)
        XCTAssertEqual(second?.media.id, "second")
        XCTAssertEqual(second?.offsetMS, 5000)
        XCTAssertEqual(second?.timelineMS, 75000)
    }

    func testPositionInTheGapLandsAtTheStartOfTheNextOriginal() {
        for gapMS in [Int64(60000), 65000, 69999] {
            let location = RecordingTimelinePieces.locate(timelineMS: gapMS, in: pieces)
            XCTAssertEqual(location?.media.id, "second")
            XCTAssertEqual(location?.offsetMS, 0)
            XCTAssertEqual(location?.timelineMS, 70000)
        }
    }

    func testPositionsBeforeTheFirstAndAfterTheLastAreClamped() {
        XCTAssertEqual(RecordingTimelinePieces.locate(timelineMS: -5, in: pieces)?.timelineMS, 0)
        let end = RecordingTimelinePieces.locate(timelineMS: 500000, in: pieces)
        XCTAssertEqual(end?.media.id, "second")
        XCTAssertEqual(end?.offsetMS, 30000)
        XCTAssertEqual(end?.timelineMS, 100000)
        XCTAssertNil(RecordingTimelinePieces.locate(timelineMS: 0, in: []))
    }

    func testAnOriginalOfUnknownLengthEndsWhereTheNextStarts() {
        let unknown = [media("first", startMS: 0, durationMS: 0), media("second", startMS: 40000, durationMS: 20000)]
        XCTAssertEqual(RecordingTimelinePieces.locate(timelineMS: 39000, in: unknown)?.media.id, "first")
        XCTAssertEqual(RecordingTimelinePieces.locate(timelineMS: 41000, in: unknown)?.media.id, "second")
    }

    func testTheOriginalAfterIsTheNextOneOnTheTimeline() throws {
        let first = try XCTUnwrap(pieces.first { $0.id == "first" })
        let second = try XCTUnwrap(pieces.first { $0.id == "second" })
        XCTAssertEqual(RecordingTimelinePieces.original(after: first, in: pieces)?.id, "second")
        XCTAssertNil(RecordingTimelinePieces.original(after: second, in: pieces))
    }
}
