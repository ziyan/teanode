import Foundation
import XCTest

@testable import TeaNodeKit

final class RecordingSharingTests: XCTestCase {
    private func shared(_ json: String) throws -> SharedRecording {
        try JSONDecoder().decode(SharedRecording.self, from: Data(json.utf8))
    }

    private let listing =
        #"{"recordingId":"r1","ownerUserId":"u1","ownerUsername":"alex","ownerName":"Alex","recordingTitle":"Standup","createdAt":"2030-01-02T10:00:00Z","recordedAt":null,"timelineDurationMS":125000,"recordingSummary":"Two items.","recordingShareLevel":"summary"}"#

    func testOnlyTheTwoLevelsCanBeGivenAndOnlyTranscriptShowsTheWords() {
        XCTAssertEqual(RecordingShareLevel.grantable, [.summary, .transcript])
        XCTAssertEqual(RecordingShareLevel.allCases.filter(\.canReadTranscript), [.transcript])
    }

    func testAListingHasNoSpeakersOrPassagesAndNamesItsOwner() throws {
        let found = try shared(listing)
        XCTAssertEqual(found.id, "r1")
        XCTAssertEqual(found.level, .summary)
        XCTAssertNil(found.speakerNames)
        XCTAssertNil(found.passages)
        XCTAssertTrue(found.turns.isEmpty)
        XCTAssertEqual(found.ownerDisplayName, "Alex")
        let nameless = try shared(listing.replacingOccurrences(of: #""ownerName":"Alex""#, with: #""ownerName":"""#))
        XCTAssertEqual(nameless.ownerDisplayName, "alex")
    }

    func testTheTurnsLeaveOutFillerAndJoinOneSpeakersPassages() throws {
        let found = try shared(
            String(listing.dropLast())
                + #","speakerNames":["Alex"],"passageCount":4,"hasMorePassages":true,"passages":[{"segmentStartMS":0,"segmentEndMS":1000,"speakerName":"Alex","transcriptText":"We ship on Friday.","isFiller":false},{"segmentStartMS":1000,"segmentEndMS":1500,"speakerName":"Alex","transcriptText":"Mm-hmm.","isFiller":true},{"segmentStartMS":2000,"segmentEndMS":3000,"speakerName":"Alex","transcriptText":" After the review. ","isFiller":false},{"segmentStartMS":65000,"segmentEndMS":66000,"speakerName":"Speaker 1","transcriptText":"Agreed.","isFiller":false},{"segmentStartMS":66000,"segmentEndMS":67000,"speakerName":"Alex","transcriptText":"   ","isFiller":false}]}"#
        )
        XCTAssertEqual(found.passageCount, 4)
        XCTAssertEqual(found.hasMorePassages, true)
        XCTAssertEqual(found.speakerNames, ["Alex"])
        XCTAssertEqual(
            found.turns,
            [
                SharedRecordingTurn(id: 0, speakerName: "Alex", startMS: 0, text: "We ship on Friday. After the review."),
                SharedRecordingTurn(id: 1, speakerName: "Speaker 1", startMS: 65000, text: "Agreed."),
            ])
    }

    func testAShareNamesWhoItIsWith() throws {
        func share(_ json: String) throws -> RecordingShare { try JSONDecoder().decode(RecordingShare.self, from: Data(json.utf8)) }
        let person = try share(#"{"id":"s1","recordingId":"r","granteeUserId":"u","granteeUsername":"sam","granteeName":"Sam","recordingShareLevel":"transcript"}"#)
        XCTAssertEqual(person.granteeDisplayName, "Sam")
        XCTAssertFalse(person.isWithCircle)
        XCTAssertEqual(person.level, .transcript)
        let nameless = try share(#"{"id":"s2","recordingId":"r","granteeUserId":"u","granteeUsername":"sam","granteeName":"","recordingShareLevel":"summary"}"#)
        XCTAssertEqual(nameless.granteeDisplayName, "sam")
        let circle = try share(#"{"id":"s3","recordingId":"r","granteeCircleId":"k","granteeCircleName":"Family","recordingShareLevel":"summary"}"#)
        XCTAssertTrue(circle.isWithCircle)
        XCTAssertEqual(circle.granteeDisplayName, "Family")
    }

    func testTheDocumentsAreInTheSchemaCheckAndNameTheFieldsTheScreensRead() {
        for name in ["recordingShares", "shareRecording", "unshareRecording", "sharedRecordings", "sharedRecording"] {
            XCTAssertNotNil(Documents.recordingSharing[name], name)
        }
        XCTAssertTrue(Documents.sharedRecording.contains("speakerNames"))
        XCTAssertTrue(Documents.sharedRecording.contains("hasMorePassages"))
        XCTAssertFalse(Documents.sharedRecordings.contains("passages"))
        XCTAssertTrue(Documents.shareRecording.contains("$recordingShareLevel: String!"))
        XCTAssertTrue(Documents.recordingShares.contains("granteeCircleName"))
    }
}
