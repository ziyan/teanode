import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

final class RecordingSpeakerContactTests: XCTestCase {
    private static let speakersJSON =
        #"{"sourceGeneration":5,"speakerIdentities":[{"id":"identity-a","confirmedName":"Sam","contactAddressBookId":"book-a","contactId":"contact-a","contactName":"Samantha Example"},{"id":"identity-b","confirmedName":"Alex","contactAddressBookId":"","contactId":"","contactName":""},{"id":"identity-c","confirmedName":"Jordan"}],"speakerCorrections":[],"voiceReferences":[]}"#

    private func makeConnection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ fake: FakeServer) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.last?.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func segment() throws -> RecordingTranscriptSegment {
        try JSONDecoder().decode(
            RecordingTranscriptSegment.self,
            from: Data(
                #"{"id":"segment-a","recordingTranscriptRunId":"run-a","recordingTranscriptionAttemptId":"attempt-a","localSpeakerId":"local-a","segmentStartMS":1000,"segmentEndMS":5000,"transcriptText":"Hello."}"#
                    .utf8))
    }

    func testSpeakerIdentityDecodesItsContactAndAnOlderServersAnswerWithoutOne() throws {
        let speakers = try JSONDecoder().decode(RecordingSpeakersView.self, from: Data(Self.speakersJSON.utf8))
        let tied = speakers.speakerIdentities[0]
        XCTAssertTrue(tied.isTiedToContact)
        XCTAssertEqual(tied.contactAddressBookId, "book-a")
        XCTAssertEqual(tied.contactId, "contact-a")
        XCTAssertEqual(tied.contactName, "Samantha Example")
        XCTAssertFalse(speakers.speakerIdentities[1].isTiedToContact)
        XCTAssertFalse(speakers.speakerIdentities[2].isTiedToContact)
        XCTAssertNil(speakers.speakerIdentities[2].contactId)
    }

    func testSuggestionDecodesItsContactOnlyWhenItHasOne() async throws {
        let fake = FakeServer { _ in
            (
                200,
                #"{"data":{"SuggestRecordingSpeakerNames":[{"confirmedName":"Sam","speakerNameSource":"contact","addressBookId":"book-a","contactId":"contact-a"},{"confirmedName":"Alex","speakerNameSource":"memory"}]}}"#
            )
        }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "robin")
        let suggestions = try await Connection(account: account, transport: fake).suggestRecordingSpeakerNames(searchText: "")
        XCTAssertEqual(suggestions.count, 2)
        XCTAssertTrue(suggestions[0].hasContact)
        XCTAssertEqual(suggestions[0].contactId, "contact-a")
        XCTAssertFalse(suggestions[1].hasContact)
        XCTAssertNil(suggestions[1].addressBookId)
    }

    func testNamingASpeakerFromAContactSendsTheContact() async throws {
        let (connection, fake) = try makeConnection(
            answering: #"{"data":{"CorrectRecordingSpeaker":{"sourceGeneration":6,"speakerIdentities":[],"speakerCorrections":[{"id":"correction-a","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"attempt-a","speakerIdentityId":"identity-a","clientCorrectionId":"client-a","localSpeakerId":"local-a","passageStartMS":-1,"passageEndMS":-1,"sourceGeneration":6,"isRemoved":false}],"voiceReferences":[]}}}"#)
        _ = try await connection.correctRecordingSpeaker(
            recordingId: "recording-a", expectedSourceGeneration: 5, clientCorrectionId: "client-a", confirmedName: "Sam",
            segment: try segment(), isWholeGroup: true, contactAddressBookId: "book-a", contactId: "contact-a")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["contactAddressBookId"], .string("book-a"))
        XCTAssertEqual(variables["contactId"], .string("contact-a"))
        XCTAssertEqual(variables["confirmedName"], .string("Sam"))
    }

    func testNamingASpeakerByTypingSendsNoContact() async throws {
        let (connection, fake) = try makeConnection(
            answering: #"{"data":{"CorrectRecordingSpeaker":{"sourceGeneration":6,"speakerIdentities":[],"speakerCorrections":[{"id":"correction-a","recordingMediaId":"media-a","recordingTranscriptionAttemptId":"attempt-a","speakerIdentityId":"identity-a","clientCorrectionId":"client-a","localSpeakerId":"local-a","passageStartMS":-1,"passageEndMS":-1,"sourceGeneration":6,"isRemoved":false}],"voiceReferences":[]}}}"#)
        _ = try await connection.correctRecordingSpeaker(
            recordingId: "recording-a", expectedSourceGeneration: 5, clientCorrectionId: "client-a", confirmedName: "Sam",
            segment: try segment(), isWholeGroup: true)
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["contactAddressBookId"], .null)
        XCTAssertEqual(variables["contactId"], .null)
    }

    func testNamingATemporarySpeakerFromAContactSendsTheContact() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"NameRecordingTemporarySpeaker":true}}"#)
        try await connection.nameRecordingTemporarySpeaker(
            recordingId: "recording-a", speakerLabel: "Speaker 1", confirmedName: "Sam", isAccountOwner: false,
            contactAddressBookId: "book-a", contactId: "contact-a")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["speakerLabel"], .string("Speaker 1"))
        XCTAssertEqual(variables["contactAddressBookId"], .string("book-a"))
        XCTAssertEqual(variables["contactId"], .string("contact-a"))
        XCTAssertEqual(variables["isAccountOwner"], .null)
    }

    func testSettingAndClearingASpeakersContact() async throws {
        let (connection, fake) = try makeConnection(answering: "{\"data\":{\"SetRecordingSpeakerContact\":\(Self.speakersJSON)}}")
        let linked = try await connection.setRecordingSpeakerContact(
            recordingId: "recording-a", expectedSourceGeneration: 4, speakerIdentityId: "identity-a", addressBookId: "book-a",
            contactId: "contact-a")
        XCTAssertEqual(linked.sourceGeneration, 5)
        var variables = try sentVariables(fake)
        XCTAssertEqual(variables["speakerIdentityId"], .string("identity-a"))
        XCTAssertEqual(variables["addressBookId"], .string("book-a"))
        XCTAssertEqual(variables["contactId"], .string("contact-a"))
        XCTAssertEqual(variables["expectedSourceGeneration"], .number(4))
        _ = try await connection.setRecordingSpeakerContact(
            recordingId: "recording-a", expectedSourceGeneration: 5, speakerIdentityId: "identity-a", addressBookId: "", contactId: "")
        variables = try sentVariables(fake)
        XCTAssertEqual(variables["addressBookId"], .string(""))
        XCTAssertEqual(variables["contactId"], .string(""))
    }

    @MainActor
    func testSpeakerDraftKeepsItsContactAndReadsADraftKeptBeforeContactsExisted() throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        var draft = RecordingSpeakerDraft(accountKey: "account-a", recordingId: "recording-a", segment: try segment(), expectedSourceGeneration: 4)
        draft.confirmedName = "Sam"
        draft.contactAddressBookId = "book-a"
        draft.contactId = "contact-a"
        try RecordingSpeakerDrafts.save(draft, in: defaults)
        XCTAssertEqual(RecordingSpeakerDrafts.read(accountKey: "account-a", segmentId: "segment-a", in: defaults)?.contactId, "contact-a")
        var object = try XCTUnwrap(
            try JSONSerialization.jsonObject(with: JSONEncoder().encode(draft)) as? [String: Any])
        object.removeValue(forKey: "contactAddressBookId")
        object.removeValue(forKey: "contactId")
        let older = try JSONDecoder().decode(RecordingSpeakerDraft.self, from: JSONSerialization.data(withJSONObject: object))
        XCTAssertNil(older.contactId)
        XCTAssertEqual(older.confirmedName, "Sam")
    }
}
