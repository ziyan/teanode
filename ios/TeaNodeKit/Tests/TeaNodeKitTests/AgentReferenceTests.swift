import XCTest

@testable import TeaNodeKit

final class AgentReferenceTests: XCTestCase {
    func testRecordingReferenceIsNamedByItsTitle() {
        let reference = AgentReference(recordingId: "recording-1", recordingTitle: "Quarterly planning")
        XCTAssertEqual(reference.recordingId, "recording-1")
        XCTAssertEqual(reference.label, "Quarterly planning")
        XCTAssertFalse(reference.isReply)
    }

    func testRecordingReferenceIsSentWithItsKeys() throws {
        let reference = AgentReference(recordingId: "recording-1", recordingTitle: "Quarterly planning")
        let object = try JSONSerialization.jsonObject(with: JSONEncoder().encode(reference)) as? [String: Any]
        XCTAssertEqual(object?["recordingId"] as? String, "recording-1")
        XCTAssertEqual(object?["recordingTitle"] as? String, "Quarterly planning")
    }

    func testFeedCardReferenceIsSentWithItsKeys() throws {
        let reference = AgentReference(feedItemId: "card-1", feedItemTitle: "Morning brief")
        XCTAssertEqual(reference.label, "Morning brief")
        XCTAssertFalse(reference.isReply)
        let object = try JSONSerialization.jsonObject(with: JSONEncoder().encode(reference)) as? [String: Any]
        XCTAssertEqual(object?["feedItemId"] as? String, "card-1")
        XCTAssertEqual(object?["feedItemTitle"] as? String, "Morning brief")
    }
}
