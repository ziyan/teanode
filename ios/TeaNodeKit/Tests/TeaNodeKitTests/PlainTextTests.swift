import XCTest

@testable import TeaNodeKit

final class PlainTextTests: XCTestCase {
    func testHiddenMarkersAreRemovedAndCodeIsLeftAlone() {
        XCTAssertEqual(HiddenMarkers.removed(from: "Saved. <!-- kept: 3 --> Anything else?"), "Saved.  Anything else?")
        XCTAssertEqual(HiddenMarkers.removed(from: "Working on it <!--sugg"), "Working on it ")
        XCTAssertEqual(HiddenMarkers.removed(from: "Done.\n<!--\nnote\n-->\nBye."), "Done.\n\nBye.")
        XCTAssertEqual(HiddenMarkers.removed(from: "A\n<!--a-->B<!--b-->C"), "A\nBC")
        XCTAssertEqual(HiddenMarkers.removed(from: "Use:\n```html\n<!-- a note -->\n```\nOkay."), "Use:\n```html\n<!-- a note -->\n```\nOkay.")
        XCTAssertEqual(HiddenMarkers.removed(from: "Write `<!-- a note -->` to comment."), "Write `<!-- a note -->` to comment.")
        XCTAssertEqual(HiddenMarkers.removed(from: "No marker here."), "No marker here.")
    }

    func testPlainTextKeepsTheWordsOfAnAnswer() {
        XCTAssertEqual(PlainText.of("Pay [the invoice](mail:01abc) **today**?\n<!--suggestions:[\"Yes\",\"No\"]-->"), "Pay the invoice today?")
        XCTAssertEqual(PlainText.of("Here: ![the driveway](https://example.org/a.jpg) `INV-1`"), "Here: the driveway INV-1")
    }

    func testAMarkerIsNotDrawnInTheChat() {
        XCTAssertEqual(
            MarkdownBlock.parse("Booked.\n<!-- kept: 1 -->\nSee you then."),
            [.paragraph("Booked."), .paragraph("See you then.")])
    }

    func testTheSuggestionsLineIsNotShownWhileItIsWritten() {
        var transcript = Transcript(messages: [])
        transcript.apply(AgentEvent(kind: "text", runId: "r", sequence: 1, text: "Booked for Friday."))
        for (index, piece) in ["\n<!--sugg", "estions:[\"Thanks\",", "\"Not now\"]-->"].enumerated() {
            transcript.apply(AgentEvent(kind: "text", runId: "r", sequence: 2 + index, text: piece))
            XCTAssertEqual(transcript.lines.last?.text, "Booked for Friday.")
        }
        transcript.apply(
            AgentEvent(kind: "message", runId: "r", sequence: 5, text: "Booked for Friday.\n<!--suggestions:[\"Thanks\",\"Not now\"]-->"))
        XCTAssertEqual(transcript.lines.last?.text, "Booked for Friday.")
        XCTAssertEqual(transcript.offeredSuggestions, ["Thanks", "Not now"])
    }

    func testAnAnswerIsSpokenWithoutAnyHiddenComment() {
        XCTAssertEqual(SpokenText.of("It is sunny. <!-- kept: 2 --> Bring a hat.\n<!--suggestions:[\"Yes\",\"No\"]-->"), "It is sunny. Bring a hat.")
    }
}
