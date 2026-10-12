import XCTest

@testable import TeaNodeKit

final class CodingQuestionHeadlineTests: XCTestCase {
    private func question(_ text: String) -> CodingQuestion {
        CodingQuestion(
            computer: "a laptop", paneId: "w1:p1", questionFingerprint: "f1", codingAgentKind: "claude", sessionName: "site",
            questionText: text, isMultipleChoice: false, options: [])
    }

    func testAToolApprovalIsHeadedByTheToolAndWhatItIsFor() {
        let approval = question(
            "Bash command\nRun the build's tests\nmake -C build test && make -C build lint\n⚠ Claude Code will automatically deny this request in 0:12, to avoid blocking progress on an unattended session\nDo you want to proceed?")
        XCTAssertEqual(approval.headline, "Bash command · Run the build's tests")
        XCTAssertFalse(approval.shownText.contains("automatically deny"))
        XCTAssertTrue(approval.shownText.contains("make -C build test"))
    }

    func testAQuestionIsHeadedByTheLineThatAsks() {
        XCTAssertEqual(question("Fruit\nWhich fruit should we pick?").headline, "Which fruit should we pick?")
        XCTAssertEqual(question("Which fruit should we pick?").headline, "Which fruit should we pick?")
    }

    func testALongHeadlineIsCut() {
        let long = question(String(repeating: "word ", count: 60) + "?")
        XCTAssertEqual(long.headline.count, 158)
        XCTAssertTrue(long.headline.hasSuffix("…"))
    }
}
