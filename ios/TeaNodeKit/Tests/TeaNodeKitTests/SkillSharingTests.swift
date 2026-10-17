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
        XCTAssertEqual(long.argumentLines[2].value.count, 300)
    }

    func testWhatToTypeIsWrittenInTheCurrencysOwnDigits() {
        XCTAssertEqual(SkillShareMoney.typedExample(currencyCode: "usd"), "1000.00")
        XCTAssertEqual(SkillShareMoney.typedExample(currencyCode: "jpy"), "1000")
        XCTAssertEqual(SkillShareMoney.typedExample(currencyCode: "kwd"), "1000.000")
        XCTAssertEqual(SkillShareMoney.typedExample(currencyCode: ""), "1000.00")
    }

    func testACallIsGoneOnlyWhenTheServerSaysItIsNotThere() throws {
        let waiting = try call()
        XCTAssertEqual(SkillShareCallReading.reading(callId: "call-1", of: .success([waiting])), .found(waiting))
        XCTAssertEqual(SkillShareCallReading.reading(callId: "other", of: .success([waiting])), .gone)
        XCTAssertEqual(SkillShareCallReading.reading(callId: "call-1", of: .failure(TeaNodeError.refused("api: not found"))), .gone)
        // A read that did not get through says nothing about the call.
        XCTAssertEqual(SkillShareCallReading.reading(callId: "call-1", of: .failure(URLError(.notConnectedToInternet))), .failed)
        XCTAssertEqual(SkillShareCallReading.reading(callId: "call-1", of: .failure(TeaNodeError.refused("api: forbidden"))), .failed)
        XCTAssertEqual(SkillShareCallReading.reading(callId: "call-1", of: .failure(TeaNodeError.unreadableAnswer)), .failed)
    }

    func testAFailureIsToldInTheServersWordsOnlyWhenTheServerAnswered() {
        XCTAssertEqual(TeaNodeError.refused("that call is not waiting any more").refusalReason(orSay: "Try again."), "that call is not waiting any more")
        XCTAssertEqual(URLError(.notConnectedToInternet).refusalReason(orSay: "Try again."), "Try again.")
        XCTAssertEqual(TeaNodeError.refused("").refusalReason(orSay: "Try again."), "Try again.")
        XCTAssertTrue(TeaNodeError.refused("db: not found").isNotFound)
        XCTAssertFalse(TeaNodeError.signedOut.isNotFound)
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

    /// Two calls that wait can reach the feed as notes with the same run and
    /// sequence; each is its own card, and the same call twice is one.
    func testEachCallThatWaitsIsOneCardWhateverRunAndSequenceItsNoteCameWith() {
        func note(callId: String) -> AgentEvent {
            AgentEvent(
                kind: "note", runId: "", sequence: 0, noteKind: "skill_share_call_waiting",
                noteDetail: #"{"skillShareId":"share-1","callId":"\#(callId)"}"#)
        }
        var transcript = Transcript(messages: [])
        transcript.apply(note(callId: "call-1"))
        transcript.apply(note(callId: "call-2"))
        transcript.apply(note(callId: "call-1"))
        XCTAssertEqual(
            transcript.lines.map(\.id), ["skill-share-call-waiting-call-1", "skill-share-call-waiting-call-2"])
        XCTAssertEqual(transcript.lines.map(\.detail), ["skill_share_call_waiting", "skill_share_call_waiting"])
        XCTAssertEqual(SkillShareNote.parsing(transcript.lines[1].text)?.callId, "call-2")
    }
}
