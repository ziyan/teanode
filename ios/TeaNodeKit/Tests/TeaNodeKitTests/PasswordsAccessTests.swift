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
            // A null beside the other id is still two keys, as in Go and the dashboard.
            #"{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","circleId":null}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":null,"circleId":"circle-1"}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","circleId":"circle-1"}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"groupId":"group-1"}]}"#,
            #"{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","extra":true}]}"#,
            #"{"isUsableByAgent":true,"grantees":[],"extra":true}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordVersion":0}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordVersion":-1}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordVersion":1.5}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordVersion":"1"}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordVersion":null}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordVersion":9007199254740992}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":null}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":5}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":""}"#,
            // No padding, unused bits that are not zero, and 24 and 33 bytes.
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8"}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi9="}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYn"}"#,
            #"{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8v"}"#,
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

    // MARK: The signature

    private let accountKey = Data((0..<32).map { UInt8(0xa0 + $0) })
    private let otherAccountKey = Data((0..<32).map { UInt8(0xb0 + $0) })
    private let validMac = "EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8="

    func testTheVersionAndSignatureAreWrittenAndReadBack() throws {
        let signed = PasswordsItemAccess(
            isUsableByAgent: false, grantees: [.person(userId: "user-ben")], recordVersion: 3, recordMac: validMac)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let text = String(decoding: try encoder.encode(signed), as: UTF8.self)
        XCTAssertEqual(
            text,
            #"{"grantees":[{"userId":"user-ben"}],"isUsableByAgent":false,"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8=","recordVersion":3}"#)
        for kind in PasswordsItemKind.allCases {
            let original = try PasswordsItemFields(itemKind: kind, jsonData: Data(#"{"name":"x"}"#.utf8)).withAccess(signed)
            let decoded = try PasswordsItemFields(itemKind: kind, jsonData: try original.jsonData())
            XCTAssertEqual(decoded.access, signed, "\(kind)")
        }
        // Either one may be missing: the record is read, and verifyAccess then refuses it.
        let versionOnly = try JSONDecoder().decode(
            PasswordsItemAccess.self, from: Data(#"{"isUsableByAgent":true,"grantees":[],"recordVersion":2}"#.utf8))
        XCTAssertEqual(versionOnly, PasswordsItemAccess(isUsableByAgent: true, grantees: [], recordVersion: 2))
        let neither = try JSONDecoder().decode(PasswordsItemAccess.self, from: Data(#"{"isUsableByAgent":true,"grantees":[]}"#.utf8))
        XCTAssertNil(neither.recordVersion)
        XCTAssertNil(neither.recordMac)
    }

    func testTheBuildersDropTheSignature() {
        let signed = PasswordsItemAccess(isUsableByAgent: true, grantees: access.grantees, recordVersion: 4, recordMac: validMac)
        XCTAssertEqual(PasswordsItemAccess.accessWithGrantees(signed, grantees: []), PasswordsItemAccess(isUsableByAgent: true))
        XCTAssertEqual(
            PasswordsItemAccess.accessWithAgentUse(signed, isUsableByAgent: false),
            PasswordsItemAccess(isUsableByAgent: false, grantees: access.grantees))
    }

    func testTheSignedBytesAreTheDocumentedOnes() throws {
        let record = PasswordsItemAccess(isUsableByAgent: true, grantees: [.person(userId: "user-ben"), .circle(circleId: "circle-1")], recordVersion: 12)
        let bytes = try PasswordsCrypto.accessSignedBytes(itemId: "item-1", itemKind: .note, access: record)
        XCTAssertEqual(
            String(decoding: bytes, as: UTF8.self), "teanode passwords access record v1\u{0}item-1\u{0}note\u{0}12\u{0}1\u{0}c:circle-1\u{0}u:user-ben\u{0}")
        let off = PasswordsItemAccess(isUsableByAgent: false, grantees: [], recordVersion: 12)
        XCTAssertEqual(
            String(decoding: try PasswordsCrypto.accessSignedBytes(itemId: "item-1", itemKind: .note, access: off), as: UTF8.self),
            "teanode passwords access record v1\u{0}item-1\u{0}note\u{0}12\u{0}0\u{0}")
        XCTAssertThrowsError(try PasswordsCrypto.accessSignedBytes(itemId: "item-1", itemKind: .note, access: access))
    }

    func testTheGranteesAreSortedByUTF8BytesNotByCharacter() throws {
        // U+FF5E is three bytes starting 0xef and U+1F600 is four bytes starting
        // 0xf0, so byte order puts the first one first.
        let record = PasswordsItemAccess(
            isUsableByAgent: false, grantees: [.person(userId: "\u{1F600}"), .person(userId: "\u{FF5E}")], recordVersion: 1)
        let text = String(decoding: try PasswordsCrypto.accessSignedBytes(itemId: "item-1", itemKind: .login, access: record), as: UTF8.self)
        XCTAssertTrue(text.hasSuffix("u:\u{FF5E}\u{0}u:\u{1F600}\u{0}"), text)
    }

    func testSigningCountsOneMoreThanTheHighestVersionSeenAndVerifies() throws {
        let signed = try PasswordsCrypto.signAccess(
            accountKey: accountKey, itemId: "item-1", itemKind: .login, access: access, highestVersionSeen: 4)
        XCTAssertEqual(signed.recordVersion, 5)
        XCTAssertTrue(PasswordsCrypto.isAccessVerified(accountKey: accountKey, itemId: "item-1", itemKind: .login, access: signed))
        // A version or signature in the input is not used.
        var stale = signed
        stale.recordVersion = 900
        stale.recordMac = validMac
        XCTAssertEqual(
            try PasswordsCrypto.signAccess(accountKey: accountKey, itemId: "item-1", itemKind: .login, access: stale, highestVersionSeen: 4),
            signed)
        // The order of grantees does not matter.
        var reversed = signed
        reversed.grantees.reverse()
        XCTAssertTrue(PasswordsCrypto.isAccessVerified(accountKey: accountKey, itemId: "item-1", itemKind: .login, access: reversed))
        // No record is signed as one that grants nobody and no agent use.
        let empty = try PasswordsCrypto.signAccess(
            accountKey: accountKey, itemId: "item-1", itemKind: .login, access: nil, highestVersionSeen: 0)
        XCTAssertEqual(empty.recordVersion, 1)
        XCTAssertFalse(empty.isUsableByAgent)
        XCTAssertTrue(empty.grantees.isEmpty)
    }

    func testARecordThatChangedInAnythingTheSignatureCoversIsRefused() throws {
        let signed = try PasswordsCrypto.signAccess(
            accountKey: accountKey, itemId: "item-1", itemKind: .login, access: access, highestVersionSeen: 4)
        func isVerified(
            _ record: PasswordsItemAccess?, key: Data? = nil, itemId: String = "item-1", itemKind: PasswordsItemKind = .login
        ) -> Bool {
            PasswordsCrypto.isAccessVerified(accountKey: key ?? accountKey, itemId: itemId, itemKind: itemKind, access: record)
        }
        XCTAssertTrue(isVerified(signed))
        XCTAssertFalse(isVerified(signed, key: otherAccountKey), "another account key")
        XCTAssertFalse(isVerified(signed, itemId: "item-2"), "another item")
        XCTAssertFalse(isVerified(signed, itemKind: .note), "another kind")
        var changed = signed
        changed.isUsableByAgent = false
        XCTAssertFalse(isVerified(changed), "agent use")
        changed = signed
        changed.recordVersion = 6
        XCTAssertFalse(isVerified(changed), "version")
        changed = signed
        changed.grantees.append(.person(userId: "user-cy"))
        XCTAssertFalse(isVerified(changed), "a grantee")
        changed = signed
        changed.grantees = [.circle(circleId: "user-ben"), .circle(circleId: "circle-1")]
        XCTAssertFalse(isVerified(changed), "a user turned into a circle")
        changed = signed
        var flipped = try XCTUnwrap(Data(base64Encoded: try XCTUnwrap(signed.recordMac)))
        flipped[0] ^= 1
        changed.recordMac = flipped.base64EncodedString()
        XCTAssertFalse(isVerified(changed), "a flipped signature bit")
        changed.recordMac = "%%%"
        XCTAssertFalse(isVerified(changed), "a signature that is not base64")
        changed = signed
        changed.recordMac = nil
        XCTAssertFalse(isVerified(changed), "no signature")
        changed = signed
        changed.recordVersion = nil
        XCTAssertFalse(isVerified(changed), "no version")
        XCTAssertFalse(isVerified(nil), "no record")
    }

    func testAVersionWrittenAsAWholeNumberIsTheSameVersion() throws {
        for (text, want) in [("1", 1), ("1.0", 1), ("1e0", 1), ("1E0", 1), ("7.000", 7), ("0.5e1", 5), ("9007199254740991", PasswordsCrypto.maximumRecordVersion)] {
            let record = try JSONDecoder().decode(
                PasswordsItemAccess.self, from: Data(#"{"isUsableByAgent":true,"grantees":[],"recordVersion":\#(text)}"#.utf8))
            XCTAssertEqual(record.recordVersion, want, text)
        }
        for text in ["0.0", "-1.0", "1.5", #""1""#, "null", "9007199254740992", "9007199254740993", "1e400", "1e16"] {
            XCTAssertThrowsError(
                try JSONDecoder().decode(
                    PasswordsItemAccess.self, from: Data(#"{"isUsableByAgent":true,"grantees":[],"recordVersion":\#(text)}"#.utf8)),
                text)
        }
    }

    func testAControlCharacterInAGranteeIDIsRefusedWhenSigningAndVerifying() throws {
        // An id with a NUL and "u:" in it would sign to the same bytes as two grantees.
        let pair = try PasswordsCrypto.signAccess(
            accountKey: accountKey, itemId: "item-1", itemKind: .note,
            access: PasswordsItemAccess(isUsableByAgent: false, grantees: [.person(userId: "user-a"), .person(userId: "user-b")]),
            highestVersionSeen: 0)
        let identifiers = [
            "user-a\u{0}u:user-b", "\u{0}user-a", "user-a\n", "user\t-a", "user-a\u{7f}", "user-a\u{85}",
        ]
        for identifier in identifiers {
            for grantee in [PasswordsGrantee.person(userId: identifier), .circle(circleId: identifier)] {
                let record = PasswordsItemAccess(isUsableByAgent: false, grantees: [grantee])
                XCTAssertThrowsError(
                    try PasswordsCrypto.signAccess(
                        accountKey: accountKey, itemId: "item-1", itemKind: .note, access: record, highestVersionSeen: 0),
                    identifier)
                var presented = record
                presented.recordVersion = pair.recordVersion
                presented.recordMac = pair.recordMac
                XCTAssertFalse(
                    PasswordsCrypto.isAccessVerified(accountKey: accountKey, itemId: "item-1", itemKind: .note, access: presented),
                    identifier)
            }
        }
        // A space, a non-breaking space, an accent and an emoji are not control characters.
        for identifier in ["user a", "user\u{a0}a", "user-\u{e9}", "user-\u{1F600}"] {
            let signed = try PasswordsCrypto.signAccess(
                accountKey: accountKey, itemId: "item-1", itemKind: .note,
                access: PasswordsItemAccess(isUsableByAgent: false, grantees: [.person(userId: identifier)]), highestVersionSeen: 0)
            XCTAssertTrue(
                PasswordsCrypto.isAccessVerified(accountKey: accountKey, itemId: "item-1", itemKind: .note, access: signed), identifier)
        }
    }

    func testSigningRefusesBadInput() {
        XCTAssertThrowsError(
            try PasswordsCrypto.signAccess(
                accountKey: Data(count: 16), itemId: "item-1", itemKind: .login, access: access, highestVersionSeen: 0))
        XCTAssertThrowsError(
            try PasswordsCrypto.signAccess(
                accountKey: accountKey, itemId: "item-1", itemKind: .login, access: access, highestVersionSeen: -1))
        XCTAssertThrowsError(
            try PasswordsCrypto.signAccess(
                accountKey: accountKey, itemId: "item-1", itemKind: .login, access: access,
                highestVersionSeen: PasswordsCrypto.maximumRecordVersion))
    }

    func testWithSignedAccessAndVerifyAccessWorkOnTheFields() throws {
        let fields = PasswordsItemFields.note(PasswordsNoteFields(name: "Example", text: "hello"))
        let signedFields = try PasswordsCrypto.withSignedAccess(
            accountKey: accountKey, itemId: "item-1", itemKind: .note, fields: fields, access: access, highestVersionSeen: 2)
        XCTAssertEqual(signedFields.name, "Example")
        XCTAssertEqual(signedFields.access?.recordVersion, 3)
        XCTAssertEqual(
            PasswordsCrypto.verifyAccess(accountKey: accountKey, itemId: "item-1", itemKind: .note, fields: signedFields), signedFields.access)
        XCTAssertNil(PasswordsCrypto.verifyAccess(accountKey: accountKey, itemId: "item-2", itemKind: .note, fields: signedFields))
        XCTAssertNil(PasswordsCrypto.verifyAccess(accountKey: otherAccountKey, itemId: "item-1", itemKind: .note, fields: signedFields))
        XCTAssertNil(PasswordsCrypto.verifyAccess(accountKey: accountKey, itemId: "item-1", itemKind: .note, fields: fields))
        XCTAssertNil(
            PasswordsCrypto.verifyAccess(accountKey: accountKey, itemId: "item-1", itemKind: .note, fields: fields.withAccess(access)))
    }
}
