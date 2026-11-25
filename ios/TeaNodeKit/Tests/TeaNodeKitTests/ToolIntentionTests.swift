import Foundation
import XCTest

@testable import TeaNodeKit

/// A tool line carries the agent's words for what the call does, from a
/// stored message and from a call still running, and is left without them
/// where the agent gave none.
final class ToolIntentionTests: XCTestCase {
    func testAStoredCallCarriesItsIntention() throws {
        let json = #"""
            [{"id":"m1","createdAt":"2026-10-11T09:00:00Z","role":"assistant","content":"",
              "toolCalls":[{"id":"c1","name":"mail_search","arguments":"{}","intention":"Looking for the boiler invoice"},
                           {"id":"c2","name":"calendar","arguments":"{}"}]}]
            """#
        let messages = try JSONDecoder().decode([AgentMessage].self, from: Data(json.utf8))
        let tools = Transcript(messages: messages).lines.filter { $0.kind == .tool }
        XCTAssertEqual(tools.map(\.intention), ["Looking for the boiler invoice", ""])
    }

    func testARunningCallCarriesItsIntention() throws {
        let json = #"{"kind":"tool_call","runId":"r","sequence":1,"tool":"mail_search","callId":"c1","arguments":"{}","intention":"Looking for the boiler invoice"}"#
        let event = try JSONDecoder().decode(AgentEvent.self, from: Data(json.utf8))
        var transcript = Transcript(messages: [])
        transcript.apply(event)
        XCTAssertEqual(transcript.lines.first { $0.kind == .tool }?.intention, "Looking for the boiler invoice")
    }
}
