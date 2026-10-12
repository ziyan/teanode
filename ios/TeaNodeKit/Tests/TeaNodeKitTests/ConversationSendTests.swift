import XCTest

@testable import TeaNodeKit

final class ConversationSendTests: XCTestCase {
    func testARefusedRequestRemovesOnlyItsPendingMessage() {
        var transcript = Transcript(messages: [])
        let firstLineId = transcript.addSent("First request")
        let secondLineId = transcript.addSent("Second request")
        transcript.withdrawSent(lineId: firstLineId)
        XCTAssertEqual(transcript.lines.map(\.id), [secondLineId])
    }

    func testAFeedConfirmationKeepsOtherPendingMessages() {
        var transcript = Transcript(messages: [])
        let firstLineId = transcript.addSent("First request")
        let secondLineId = transcript.addSent("Second request")
        transcript.apply(AgentEvent(kind: "asked", runId: "first-turn", sequence: 1, text: "First request"))
        XCTAssertEqual(transcript.lines.map(\.id), [firstLineId, secondLineId])
        XCTAssertEqual(transcript.lines[0].runId, "first-turn")
        transcript.withdrawSent(lineId: firstLineId)
        XCTAssertEqual(transcript.lines.count, 2)
        transcript.withdrawSent(lineId: secondLineId)
        XCTAssertEqual(transcript.lines.map(\.id), [firstLineId])
    }

    func testAFeedConfirmationDoesNotRemoveAnAnswerAlreadyArriving() {
        var transcript = Transcript(messages: [])
        transcript.addSent("A request")
        transcript.apply(AgentEvent(kind: "text", runId: "a-turn", sequence: 2, text: "An answer"))
        transcript.apply(AgentEvent(kind: "asked", runId: "a-turn", sequence: 1, text: "A request"))
        XCTAssertEqual(transcript.lines.map(\.text), ["A request", "An answer"])
    }

    func testInterleavedAnswersKeepTheirOwnTextAndCompletion() {
        var transcript = Transcript(messages: [])
        transcript.apply(AgentEvent(kind: "text", runId: "first-turn", sequence: 1, text: "First"))
        transcript.apply(AgentEvent(kind: "text", runId: "second-turn", sequence: 1, text: "Second"))
        transcript.apply(AgentEvent(kind: "text", runId: "first-turn", sequence: 2, text: " answer"))
        transcript.apply(AgentEvent(kind: "message", runId: "first-turn", sequence: 3, text: "First answer."))
        XCTAssertEqual(transcript.lines.map(\.text), ["First answer.", "Second"])
        XCTAssertFalse(transcript.lines[0].isStreaming)
        XCTAssertTrue(transcript.lines[1].isStreaming)
        transcript.apply(AgentEvent(kind: "message", runId: "second-turn", sequence: 2, text: "Second answer."))
        XCTAssertEqual(transcript.lines.map(\.text), ["First answer.", "Second answer."])
        XCTAssertFalse(transcript.lines[1].isStreaming)
    }
}
