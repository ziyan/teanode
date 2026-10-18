import XCTest

@testable import TeaNodeKit

final class TerminalTextTests: XCTestCase {
    func testEachRunTakesTheStyleTheCodesBeforeItSet() {
        let runs = terminalRuns("\u{1B}[38;2;220;40;40mred\u{1B}[0m plain \u{1B}[1;32mbold green\u{1B}[22m green\u{1B}[0m")
        XCTAssertEqual(runs.map(\.text), ["red", " plain ", "bold green", " green"])
        XCTAssertEqual(runs[0].foreground, TerminalColor(red: 220, green: 40, blue: 40))
        XCTAssertNil(runs[1].foreground)
        XCTAssertTrue(runs[2].isBold)
        XCTAssertFalse(runs[3].isBold)
        XCTAssertEqual(runs[3].foreground, runs[2].foreground)
    }

    func testTheTwoHundredFiftySixColorsAndAnEmptyCode() {
        var style = TerminalRun(text: "")
        applyTerminalCodes([38, 5, 196], to: &style)
        XCTAssertEqual(style.foreground, TerminalColor(red: 255, green: 0, blue: 0))
        applyTerminalCodes([48, 5, 232], to: &style)
        XCTAssertEqual(style.background, TerminalColor(red: 8, green: 8, blue: 8))
        let runs = terminalRuns("\u{1B}[31mred\u{1B}[mplain")
        XCTAssertEqual(runs.map(\.text), ["red", "plain"])
        XCTAssertNil(runs[1].foreground)
    }

    func testTextWithoutCodesIsOneRun() {
        XCTAssertEqual(terminalRuns("$ npm test").map(\.text), ["$ npm test"])
    }
}
