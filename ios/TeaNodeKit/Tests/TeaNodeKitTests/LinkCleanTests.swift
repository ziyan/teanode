import Foundation
import XCTest

@testable import TeaNodeKit

/// LinkCleanTests run the cases the server's and the dashboard's tests run
/// (internal/util/linkclean/testdata/links.json), so the three implementations
/// answer alike, and a few of the app's own about turning text into links.
final class LinkCleanTests: XCTestCase {
    private struct SharedCase: Decodable {
        let input: String
        let cleaned: String
        let display: String
    }

    /// The shared cases, found from this file's place in the repository:
    /// ios/TeaNodeKit/Tests/TeaNodeKitTests, four levels under the root.
    private func sharedCases() throws -> [SharedCase] {
        let root = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let file = root.appendingPathComponent("internal/util/linkclean/testdata/links.json")
        let cases = try JSONDecoder().decode([SharedCase].self, from: Data(contentsOf: file))
        XCTAssertGreaterThan(cases.count, 10, "the shared cases are missing or too few")
        return cases
    }

    func testTheSharedCasesCleanAndShortenAsTheServerDoes() throws {
        for found in try sharedCases() {
            XCTAssertEqual(LinkClean.cleaned(found.input), found.cleaned, "cleaned \(found.input.debugDescription)")
            XCTAssertEqual(LinkClean.displayText(found.input), found.display, "display of \(found.input.debugDescription)")
        }
    }

    func testTheDisplayTextIsNeverLongerThanTheLimit() {
        let text = LinkClean.displayText("https://example.com/aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffffgggggggggg")
        XCTAssertEqual(text.unicodeScalars.count, LinkClean.displayLength)
        XCTAssertTrue(text.contains("\u{2026}"))
    }

    func testALinkWithNothingToRemoveIsGivenBackAsItWas() {
        let link = "https://example.com/p?a=1&&b=2#top"
        XCTAssertEqual(LinkClean.cleaned(link), link)
    }
}
