import Foundation
import XCTest

@testable import TeaNodeKit

final class ReactionTests: XCTestCase {
    private func transcript() throws -> Transcript {
        let json = #"""
            [{"id":"m1","createdAt":"2026-10-11T09:00:00Z","role":"user","content":"Thanks, that helps."},
             {"id":"m2","createdAt":"2026-10-11T09:00:05Z","role":"assistant","content":"Glad to.",
              "reactions":[{"emoji":"👍","reactorKind":"person","reactedAt":"2026-10-11T09:01:00Z"}]}]
            """#
        let messages = try JSONDecoder().decode([AgentMessage].self, from: Data(json.utf8))
        return Transcript(messages: messages)
    }

    func testReactionsAreReadWithTheirMessages() throws {
        let transcript = try transcript()
        let answer = try XCTUnwrap(transcript.lines.first { $0.messageId == "m2" })
        XCTAssertEqual(answer.reaction(of: "person"), "👍")
        XCTAssertNil(answer.reaction(of: "agent"))
    }

    func testAReactedEventReplacesAndRemoves() throws {
        var transcript = try transcript()
        transcript.apply(AgentEvent(kind: "reacted", runId: "", sequence: 0, messageId: "m1", emoji: "❤️", reactorKind: "agent"))
        XCTAssertEqual(transcript.lines.first { $0.messageId == "m1" }?.reaction(of: "agent"), "❤️")
        transcript.apply(AgentEvent(kind: "reacted", runId: "", sequence: 0, messageId: "m2", emoji: "🎉", reactorKind: "person"))
        XCTAssertEqual(transcript.lines.first { $0.messageId == "m2" }?.reactions.count, 1)
        XCTAssertEqual(transcript.lines.first { $0.messageId == "m2" }?.reaction(of: "person"), "🎉")
        transcript.apply(AgentEvent(kind: "reacted", runId: "", sequence: 0, messageId: "m2", emoji: "", reactorKind: "person"))
        XCTAssertNil(transcript.lines.first { $0.messageId == "m2" }?.reaction(of: "person"))
    }

    func testAReactedEventDecodesFromTheFeed() throws {
        let json = #"{"kind":"reacted","runId":"","sequence":0,"messageId":"m2","emoji":"👍","reactorKind":"person"}"#
        let event = try JSONDecoder().decode(AgentEvent.self, from: Data(json.utf8))
        XCTAssertEqual(event.messageId, "m2")
        XCTAssertEqual(event.emoji, "👍")
        XCTAssertEqual(event.reactorKind, "person")
    }
}
