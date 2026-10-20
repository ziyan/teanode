import Foundation
import XCTest

@testable import TeaNodeKit

/// LinkTextTests cover finding the links in a message and making them
/// followable: shown short, opened clean, words kept.
final class LinkTextTests: XCTestCase {
    func testTheEndOfASentenceIsNotPartOfTheLink() {
        XCTAssertEqual(LinkText.trimmedEnd(of: "https://example.com/a."), "https://example.com/a")
        XCTAssertEqual(LinkText.trimmedEnd(of: "https://example.com/a?!"), "https://example.com/a")
        XCTAssertEqual(LinkText.trimmedEnd(of: "https://example.com/a),"), "https://example.com/a")
    }

    func testABracketTheLinkOpenedStays() {
        XCTAssertEqual(LinkText.trimmedEnd(of: "https://example.com/wiki/Thing_(film)"), "https://example.com/wiki/Thing_(film)")
        XCTAssertEqual(LinkText.trimmedEnd(of: "https://example.com/wiki/Thing_(film))"), "https://example.com/wiki/Thing_(film)")
    }

    func testEachLinkIsFoundWhereItIs() {
        let text = "Try https://example.com/one, then (https://example.org/two?x=1)."
        let found = LinkText.links(in: text)
        XCTAssertEqual(found.map(\.link), ["https://example.com/one", "https://example.org/two?x=1"])
        XCTAssertEqual(String(text[found[1].range]), "https://example.org/two?x=1")
    }

    func testALinkStopsAtWordsOfALanguageWrittenWithoutSpaces() {
        XCTAssertEqual(LinkText.links(in: "看看https://example.com/a，很好").map(\.link), ["https://example.com/a"])
    }

    func testASchemeWithNoHostAndOtherSchemesAreNotLinks() {
        XCTAssertTrue(LinkText.links(in: "https:// and ftp://example.com and mailto:a@example.com").isEmpty)
    }

    func testTheFirstLinkOfMarkdownLeavesOutCodeAndPictures() {
        let text = """
            ![chart](https://example.com/chart.png)
            `https://example.com/in-code`
            ```
            curl https://example.com/in-a-block
            ```
            Read [the guide](https://example.com/guide) first.
            """
        XCTAssertEqual(LinkText.firstLink(in: text, isMarkdown: true), "https://example.com/guide")
        XCTAssertNil(LinkText.firstLink(in: "no links here", isMarkdown: false))
        XCTAssertEqual(LinkText.firstLink(in: "a https://example.com/first b https://example.com/second", isMarkdown: false), "https://example.com/first")
    }

    func testPlainWordsGetTheirLinksShortAndClean() throws {
        let linked = LinkText.linked(plain: "Look at https://www.example.com/shop/item-1/?gclid=abc&color=red, it is nice.")
        XCTAssertEqual(String(linked.characters), "Look at example.com/shop/item-1/?color=red, it is nice.")
        let runs = linked.runs.filter { $0.link != nil }
        XCTAssertEqual(runs.count, 1)
        XCTAssertEqual(runs.first?.link?.absoluteString, "https://www.example.com/shop/item-1/?color=red")
    }

    func testEachOfSeveralLinksIsLinkedWhereItWas() {
        let linked = LinkText.linked(plain: "https://example.com/a then https://example.org/b")
        XCTAssertEqual(String(linked.characters), "example.com/a then example.org/b")
        XCTAssertEqual(linked.runs.compactMap(\.link).map(\.absoluteString), ["https://example.com/a", "https://example.org/b"])
    }

    func testWordsWithoutLinksAreUnchanged() {
        let linked = LinkText.linked(plain: "two\nlines  and a mailto:a@example.com")
        XCTAssertEqual(String(linked.characters), "two\nlines  and a mailto:a@example.com")
        XCTAssertNil(linked.runs.first(where: { $0.link != nil }))
    }

    func testALinkWithItsOwnWordsKeepsThemAndOpensClean() throws {
        var words = AttributedString("the chair")
        words.link = URL(string: "https://example.com/chair?fbclid=zz&color=blue")
        let linked = LinkText.linked(words)
        XCTAssertEqual(String(linked.characters), "the chair")
        XCTAssertEqual(linked.runs.first?.link?.absoluteString, "https://example.com/chair?color=blue")
    }

    func testTheWordsOfALinkThatLookLikeALinkAreKept() throws {
        var words = AttributedString("https://example.com/shown?gclid=1")
        words.link = URL(string: "https://example.com/opened?gclid=1")
        let linked = LinkText.linked(words)
        XCTAssertEqual(String(linked.characters), "https://example.com/shown?gclid=1")
        XCTAssertEqual(linked.runs.compactMap(\.link).map(\.absoluteString), ["https://example.com/opened"])
    }

    func testTextTheScreenProtectsIsLeftAlone() throws {
        let words = AttributedString("Run curl https://example.com/in-code now, or https://example.com/outside")
        let linked = LinkText.linked(words) { covered in String(covered.characters).hasSuffix("in-code") }
        XCTAssertEqual(String(linked.characters), "Run curl https://example.com/in-code now, or example.com/outside")
        XCTAssertEqual(linked.runs.compactMap(\.link).map(\.absoluteString), ["https://example.com/outside"])
    }

    func testALinkAfterAnEarlierOneIsReplacedWhereItIs() throws {
        let linked = LinkText.linked(plain: "https://www.example.com/a?utm_source=x and https://www.example.org/b?c=2 end")
        XCTAssertEqual(String(linked.characters), "example.com/a and example.org/b?c=2 end")
    }
}
