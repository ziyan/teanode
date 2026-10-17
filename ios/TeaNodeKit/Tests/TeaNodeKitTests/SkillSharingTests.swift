import Foundation
import XCTest

@testable import TeaNodeKit

final class SkillSharingTests: XCTestCase {
    func testAnAmountIsWrittenAndReadInTheCurrencysOwnDigits() {
        XCTAssertEqual(SkillShareMoney.format(minor: 14000, currencyCode: "usd"), "140.00 USD")
        XCTAssertEqual(SkillShareMoney.format(minor: 5, currencyCode: "usd"), "0.05 USD")
        XCTAssertEqual(SkillShareMoney.format(minor: 1400, currencyCode: "jpy"), "1400 JPY")
        XCTAssertEqual(SkillShareMoney.format(minor: 1500, currencyCode: "kwd"), "1.500 KWD")
        XCTAssertEqual(SkillShareMoney.format(minor: 250, currencyCode: ""), "2.50")

        XCTAssertEqual(SkillShareMoney.parse("100", currencyCode: "usd"), 10000)
        XCTAssertEqual(SkillShareMoney.parse("100.5", currencyCode: "usd"), 10050)
        XCTAssertEqual(SkillShareMoney.parse(" 0.05 ", currencyCode: "usd"), 5)
        XCTAssertEqual(SkillShareMoney.parse("1400", currencyCode: "jpy"), 1400)
        XCTAssertEqual(SkillShareMoney.parse("7.250", currencyCode: "kwd"), 7250)
        for typed in ["", "0", "0.00", "-5", "1,000", "100.", "100.505", "12abc", "9999999999999999"] {
            XCTAssertNil(SkillShareMoney.parse(typed, currencyCode: "usd"), typed)
        }
        XCTAssertNil(SkillShareMoney.parse("1400.5", currencyCode: "jpy"))
    }

    private func call(state: String = "waiting_for_approval", arguments: String = #"{"action":"card","merchant":"Example Store","amount_minor":14000,"currency":"usd","request_reason":"x","note":""}"#) throws -> SkillShareCall {
        let json = """
            {"id":"call-1","skillShareId":"share-1","skillName":"payments","granteeUserId":"ben-id","granteeUsername":"ben","granteeName":"Ben Example",
             "granteeAvatarUrl":null,"granteeAvatarColorIndex":3,"ownerName":"Ada Example","toolName":"pay",
             "toolArguments":\(String(decoding: try JSONEncoder().encode(arguments), as: UTF8.self)),
             "requestReason":"the weekly groceries","amountMinor":14000,"currencyCode":"usd","callState":"\(state)","toolOutput":"",
             "answeredAt":null,"answerNote":"","expiresAt":"2026-10-18T10:00:00Z","createdAt":"2026-10-17T10:00:00Z"}
            """
        return try JSONDecoder().decode(SkillShareCall.self, from: Data(json.utf8))
    }

    func testACallShowsItsAmountAndItsArgumentsLessWhatIsShownApart() throws {
        let waiting = try call()
        XCTAssertTrue(waiting.isWaiting)
        XCTAssertEqual(waiting.amountText, "140.00 USD")
        XCTAssertEqual(waiting.shownGrantee, "Ben Example")
        XCTAssertEqual(waiting.argumentLines.map(\.name), ["action", "merchant"])
        XCTAssertEqual(waiting.argumentLines.map(\.value), ["card", "Example Store"])
        XCTAssertFalse(try call(state: "ran").isWaiting)
        XCTAssertTrue(try call(arguments: "not json").argumentLines.isEmpty)
        let long = try call(arguments: "{\"note\":\"\(String(repeating: "y", count: 300))\",\"count\":3,\"nested\":{\"a\":1}}")
        XCTAssertEqual(long.argumentLines.map(\.name), ["count", "nested", "note"])
        XCTAssertEqual(long.argumentLines[0].value, "3")
        XCTAssertEqual(long.argumentLines[2].value.count, 201)
    }

    func testTheNoteOfTheMainConversationNamesTheCall() {
        let note = SkillShareNote.parsing(#"{"skillShareId":"share-1","callId":"call-1","granteeName":"Ben Example","granteeUserId":"ben-id"}"#)
        XCTAssertEqual(note?.callId, "call-1")
        XCTAssertEqual(note?.skillShareId, "share-1")
        XCTAssertNil(SkillShareNote.parsing("not json"))
        XCTAssertNil(SkillShareNote.parsing(#"{"callId":"call-1"}"#))
        XCTAssertNil(SkillShareNote.parsing(#"{"skillShareId":"","callId":""}"#))
    }

    func testAShareReadsItsPeopleAndLimits() throws {
        let json = """
            {"id":"s","skillName":"payments","toolNames":["pay"],"ownerUserId":"u1","ownerUsername":"ada","ownerName":"",
             "ownerAvatarUrl":null,"ownerAvatarColorIndex":1,"granteeUserId":"u2","granteeUsername":"ben","granteeName":"Ben Example",
             "granteeAvatarUrl":null,"granteeAvatarColorIndex":2,"paymentLimitMinor":10000,"monthlyLimitMinor":null,"currencyCode":"usd",
             "isApprovalRequired":false,"isOwnedByCaller":true,"waitingCallCount":2,"monthTotalMinor":1250}
            """
        let share = try JSONDecoder().decode(SkillShare.self, from: Data(json.utf8))
        XCTAssertEqual(share.shownOwner, "ada")
        XCTAssertEqual(share.shownGrantee, "Ben Example")
        XCTAssertEqual(share.paymentLimitMinor, 10000)
        XCTAssertNil(share.monthlyLimitMinor)
        XCTAssertEqual(share.waitingCallCount, 2)
    }

    func testASkillIsLendableOnlyWhenItIsOnReadableAndHasTools() throws {
        func skill(enabled: Bool, readable: Bool, tools: String) throws -> LendableSkill {
            let json = #"{"name":"payments","description":"d","enabled":\#(enabled),"readable":\#(readable),"tools":\#(tools)}"#
            return try JSONDecoder().decode(LendableSkill.self, from: Data(json.utf8))
        }
        let tool = #"[{"name":"pay","description":"Pay","needsComputer":true,"isAmountDeclared":true,"isScoped":false}]"#
        XCTAssertTrue(try skill(enabled: true, readable: true, tools: tool).isLendable)
        XCTAssertFalse(try skill(enabled: false, readable: true, tools: tool).isLendable)
        XCTAssertFalse(try skill(enabled: true, readable: false, tools: tool).isLendable)
        XCTAssertFalse(try skill(enabled: true, readable: true, tools: "[]").isLendable)
        XCTAssertTrue(try skill(enabled: true, readable: true, tools: tool).tools[0].isAmountDeclared)
    }

    func testTheDocumentsAskForTheOperationsByName() {
        for (document, operation) in [
            (Documents.skillShares, "ListSkillShares"), (Documents.shareSkill, "ShareSkill"), (Documents.unshareSkill, "UnshareSkill"),
            (Documents.skillShareCalls, "ListSkillShareCalls"), (Documents.answerSkillShareCall, "AnswerSkillShareCall"),
            (Documents.lendableSkills, "ListAgentSkills"),
        ] {
            XCTAssertTrue(document.contains(operation), operation)
        }
        // A limit is an Int64: it can pass two billion cents.
        XCTAssertTrue(Documents.shareSkill.contains("$paymentLimitMinor: Int64"))
    }
}
