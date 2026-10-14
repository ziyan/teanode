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
}
