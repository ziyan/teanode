import Foundation
import XCTest

@testable import TeaNodeKit

final class KnowledgeTests: XCTestCase {
    private func row(_ path: String, kind: String = "topic", pinned: Bool = false, children: Int = 0) throws -> KnowledgeRow {
        try JSONDecoder().decode(KnowledgeRow.self, from: Data("""
            {"node":{"id":"\(path)","path":"\(path)","kind":"\(kind)","name":"","summary":"About \(path)","pinned":\(pinned)},"hint":"","children":\(children)}
            """.utf8))
    }

    func testRowsPutSelfAndPinnedFirstThenWhatOpensIntoMore() throws {
        let rows = [
            try row("garden"), try row("projects", kind: "folder", children: 3), try row("self", kind: "person"),
            try row("music", pinned: true), try row("cooking", children: 2), try row("travel"),
        ]
        XCTAssertEqual(KnowledgeRow.ordered(rows).compactMap { $0.node?.path }, ["self", "music", "projects", "cooking", "garden", "travel"])
        XCTAssertEqual(try row("projects/garden-planner").node?.shownName, "Garden-planner")
        XCTAssertEqual(try row("projects/garden-planner").node?.parentPath, "projects")
        XCTAssertEqual(try row("garden").line, "About garden")
    }

    func testAFactsFilesAreTheAttachmentsItCites() throws {
        let page = try JSONDecoder().decode(KnowledgePage.self, from: Data(#"""
            {"node":null,"folded":[],"children":[],"contact":null,
             "facts":[{"id":"f1","number":1,"kind":"reflection","text":"Plans slip in spring.","happenedAt":null,"inferred":true,
               "evidence":[{"kind":"dream","id":"d1","quote":"reflection: pattern"},{"kind":"memory","id":"","quote":"projects/garden#3"},{"kind":"document","id":"doc7[page 2]","quote":""}]}],
             "attachments":[{"documentId":"doc7","name":"plan.jpg","contentType":"image/jpeg","channel":"photos","path":"/api/v1/agent/documents/doc7/file"},
                            {"documentId":"doc8","name":"other.pdf","contentType":"application/pdf","channel":"","path":""}]}
            """#.utf8))
        let fact = try XCTUnwrap(page.presentFacts.first)
        XCTAssertEqual(fact.reflectionKind, "pattern")
        XCTAssertEqual(fact.citations, ["projects/garden#3"])
        XCTAssertEqual(page.attachments(of: fact).map(\.name), ["plan.jpg"])
    }

    func testWhenAFactWasTrueIsAYearAMonthOrADay() {
        XCTAssertTrue(KnowledgeWords.isHappenedValid(""))
        XCTAssertTrue(KnowledgeWords.isHappenedValid("2023"))
        XCTAssertTrue(KnowledgeWords.isHappenedValid("2023-06"))
        XCTAssertTrue(KnowledgeWords.isHappenedValid("2023-06-14"))
        XCTAssertFalse(KnowledgeWords.isHappenedValid("June 2023"))
        XCTAssertFalse(KnowledgeWords.isHappenedValid("2023-6"))
    }
}
