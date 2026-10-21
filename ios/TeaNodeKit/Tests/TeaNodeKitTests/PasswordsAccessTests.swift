import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsAccessTests cover the access record sealed inside every item's
/// fields: that it survives a decode, an edit and an encode, that any other
/// shape makes the item unreadable, and the helpers the screens call.
final class PasswordsAccessTests: XCTestCase {
    private let access = PasswordsItemAccess(isUsableByAgent: true, grantees: [.person(userId: "user-ben"), .circle(circleId: "circle-1")])

    private func jsonObject(_ fields: PasswordsItemFields) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: try fields.jsonData()) as? [String: Any])
    }

    func testTheRecordIsWrittenAsTheContractSays() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let text = String(decoding: try encoder.encode(access), as: UTF8.self)
        XCTAssertEqual(text, #"{"grantees":[{"userId":"user-ben"},{"circleId":"circle-1"}],"isUsableByAgent":true}"#)
    }

    func testNoRecordIsLeftOutOfTheJSON() throws {
        for fields in [PasswordsItemFields.login(.init()), .card(.init()), .note(.init())] {
            XCTAssertNil(try jsonObject(fields)["access"], "\(fields.itemKind)")
        }
    }

    func testTheRecordSurvivesDecodeEditEncodeInEveryKind() throws {
        let originals: [PasswordsItemFields] = [
            .login(PasswordsLoginFields(name: "Example", access: access)),
            .card(PasswordsCardFields(name: "Example", access: access)),
            .note(PasswordsNoteFields(name: "Example", access: access)),
        ]
        for original in originals {
            let decoded = try PasswordsItemFields(itemKind: original.itemKind, jsonData: try original.jsonData())
            XCTAssertEqual(decoded.access, access)
            var edited = decoded
            switch decoded {
            case .login(var fields):
                fields.notes = "edited"
                edited = .login(fields)
            case .card(var fields):
                fields.notes = "edited"
                edited = .card(fields)
            case .note(var fields):
                fields.text = "edited"
                edited = .note(fields)
            }
            let again = try PasswordsItemFields(itemKind: original.itemKind, jsonData: try edited.jsonData())
            XCTAssertEqual(again.access, access, "\(original.itemKind)")
        }
    }

    func testWithAccessReplacesOnlyTheRecord() throws {
        let login = PasswordsItemFields.login(PasswordsLoginFields(name: "Example", password: "secret"))
        XCTAssertNil(login.access)
        let shared = login.withAccess(access)
        XCTAssertEqual(shared.access, access)
        XCTAssertEqual(shared.withAccess(nil), login)
        XCTAssertEqual(PasswordsItemFields.note(.init(name: "n")).withAccess(access).access, access)
        XCTAssertEqual(PasswordsItemFields.card(.init(name: "c")).withAccess(access).access, access)
    }

    func testNullOrMissingIsNoRecord() throws {
        for json in [#"{"name":"Example"}"#, #"{"name":"Example","access":null}"#] {
            let fields = try PasswordsItemFields(itemKind: .note, jsonData: Data(json.utf8))
            XCTAssertNil(fields.access, json)
        }
    }

    func testAnyOtherShapeMakesTheItemUnreadable() {
        let wrong = [
            #""yes""#, "5", "true", "[]", "{}",
            #"{"isUsableByAgent":true}"#,
            #"{"grantees":[]}"#,
            #"{"isUsableByAgent":"true","grantees":[]}"#,
            #"{"isUsableByAgent":true,"grantees":{}}"#,
            #"{"isUsableByAgent":true,"grantees":null}"#,
            #"{"isUsableByAgent":true,"grantees":["user-ben"]}"#,
            #"{"isUsableByAgent":true,"grantees":[null]}"#,
            #"{"isUsableByAgent":true,"grantees":[{}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":5}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":""}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"circleId":null}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","circleId":"circle-1"}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"groupId":"group-1"}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","extra":true}]}"#,
            #"{"isUsableByAgent":true,"grantees":[],"extra":true}"#,
        ]
        for record in wrong {
            let json = #"{"name":"Example","access":"# + record + "}"
            for kind in PasswordsItemKind.allCases {
                XCTAssertThrowsError(try PasswordsItemFields(itemKind: kind, jsonData: Data(json.utf8)), "\(kind): \(record)")
            }
        }
    }

    func testARecipientIsReachedByAPersonOrACircleGrantee() {
        XCTAssertTrue(PasswordsItemAccess.isReachedByAccess(access, recipientUserId: "user-ben", circleIdsOfRecipient: []))
        XCTAssertTrue(PasswordsItemAccess.isReachedByAccess(access, recipientUserId: "user-cy", circleIdsOfRecipient: ["circle-2", "circle-1"]))
        XCTAssertFalse(PasswordsItemAccess.isReachedByAccess(access, recipientUserId: "user-cy", circleIdsOfRecipient: ["circle-2"]))
        XCTAssertFalse(PasswordsItemAccess.isReachedByAccess(access, recipientUserId: "circle-1", circleIdsOfRecipient: []))
        XCTAssertFalse(PasswordsItemAccess.isReachedByAccess(access, recipientUserId: "", circleIdsOfRecipient: ["circle-1"]))
        XCTAssertFalse(PasswordsItemAccess.isReachedByAccess(nil, recipientUserId: "user-ben", circleIdsOfRecipient: ["circle-1"]))
        XCTAssertFalse(
            PasswordsItemAccess.isReachedByAccess(
                PasswordsItemAccess(), recipientUserId: "user-ben", circleIdsOfRecipient: ["circle-1"]))
    }

    func testTheBuildersKeepTheOtherHalf() {
        XCTAssertEqual(
            PasswordsItemAccess.accessWithGrantees(access, grantees: []), PasswordsItemAccess(isUsableByAgent: true, grantees: []))
        XCTAssertEqual(
            PasswordsItemAccess.accessWithAgentUse(access, isUsableByAgent: false),
            PasswordsItemAccess(isUsableByAgent: false, grantees: access.grantees))
        XCTAssertEqual(
            PasswordsItemAccess.accessWithGrantees(nil, grantees: [.circle(circleId: "circle-1")]),
            PasswordsItemAccess(isUsableByAgent: false, grantees: [.circle(circleId: "circle-1")]))
        XCTAssertEqual(
            PasswordsItemAccess.accessWithAgentUse(nil, isUsableByAgent: true), PasswordsItemAccess(isUsableByAgent: true, grantees: []))
    }
}
