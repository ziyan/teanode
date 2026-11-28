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

    func testFieldsTheServerSendsEmptyReadAsMissing() throws {
        let written = #"{"feedItemId":"card-1","feedItemTitle":"Two payments are due","recordingId":"","photoId":"","subject":"","merchantName":""}"#
        let reference = try JSONDecoder().decode(AgentReference.self, from: Data(written.utf8))
        XCTAssertEqual(reference.feedItemId, "card-1")
        XCTAssertNil(reference.recordingId)
        XCTAssertNil(reference.photoId)
        XCTAssertNil(reference.subject)
        XCTAssertNil(reference.merchantName)
        XCTAssertEqual(reference.label, "Two payments are due")
    }
}
