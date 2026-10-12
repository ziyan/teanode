import XCTest

@testable import TeaNodeKit

final class ConversationHistoryTests: XCTestCase {
    func testEarlierHistoryKeepsStreamingAnswersAndUnconfirmedMessages() {
        let recent = AgentMessage(id: "recent", createdAt: "", role: "user", content: "A question")
        let older = AgentMessage(id: "older", createdAt: "", role: "user", content: "An earlier question")
        var transcript = Transcript(messages: [recent])
        transcript.apply(AgentEvent(kind: "text", runId: "active-turn", sequence: 1, text: "An answer in progress"))
        transcript.addSent("A message being sent")
        let liveLines = transcript.lines

        transcript.prepend(messages: [older, recent])
        XCTAssertEqual(transcript.lines.first?.messageId, "older")
        XCTAssertEqual(Array(transcript.lines.dropFirst()), liveLines)
        transcript.prepend(messages: [older])
        XCTAssertEqual(transcript.lines.count, liveLines.count + 1)
    }

    func testEarlierHistoryKeepsReactionsAndSettledCards() {
        let recent = AgentMessage(id: "recent", createdAt: "", role: "assistant", content: "An answer")
        var transcript = Transcript(messages: [recent])
        transcript.react(messageId: "recent", emoji: "👍", reactorKind: "person")
        transcript.apply(AgentEvent(kind: "confirmation", runId: "active-turn", sequence: 1, tool: "a-tool", callId: "approval-call"))
        let cardId = transcript.lines.last!.id
        transcript.settle(cardId: cardId, resolution: "Approved")
        let liveLines = transcript.lines

        transcript.prepend(messages: [AgentMessage(id: "older", createdAt: "", role: "user", content: "Earlier")])
        XCTAssertEqual(Array(transcript.lines.dropFirst()), liveLines)
    }

    func testToolResultsAreMatchedAcrossHistoryPages() {
        let toolCall = AgentMessage(
            id: "older", createdAt: "", role: "assistant", content: "",
            toolCalls: [AgentToolCall(id: "tool-call", name: "a-tool", arguments: "{}")])
        let toolResult = AgentMessage(id: "recent", createdAt: "", role: "tool", content: "The tool's answer", toolCallId: "tool-call")
        var transcript = Transcript(messages: [toolResult])
        transcript.prepend(messages: [toolCall, toolResult])
        XCTAssertEqual(transcript.lines.count, 1)
        XCTAssertEqual(transcript.lines.first?.text, "The tool's answer")
        XCTAssertEqual(transcript.lines.first?.isDone, true)
    }
}
