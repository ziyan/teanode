import Foundation
import XCTest

@testable import TeaNodeKit

final class KnowledgeSharingTests: XCTestCase {
    private func root(_ owner: String, _ path: String, name: String = "") -> SharedKnowledgeRoot {
        let json =
            #"{"ownerUserId":"\#(owner)-id","ownerUsername":"\#(owner)","ownerName":"","path":"\#(path)","name":"\#(name)","nodeKind":"topic","opening":"About it.","isQuoteShared":false}"#
        return try! JSONDecoder().decode(SharedKnowledgeRoot.self, from: Data(json.utf8))
    }

    func testTheRootsAreGroupedUnderWhoSharedThemInTheOrderGiven() {
        let groups = SharedKnowledgeOwnerGroup.grouped([
            root("ada", "people/a-grandparent", name: "A Grandparent"), root("ada", "projects/garden"), root("ben", "projects/boat"),
        ])
        XCTAssertEqual(groups.map(\.ownerName), ["ada", "ben"])
        XCTAssertEqual(groups[0].roots.map(\.path), ["people/a-grandparent", "projects/garden"])
        XCTAssertEqual(groups[0].roots.map(\.shownName), ["A Grandparent", "projects/garden"])
    }

    func testAShareReachesItsPageAndThePagesUnderItAtASlash() throws {
        let json =
            #"{"id":"s","path":"people/a","pageName":"A","granteeUserId":"u","granteeUsername":"ben","granteeName":"","granteeCircleId":null,"granteeCircleName":"","isQuoteShared":true}"#
        let share = try JSONDecoder().decode(KnowledgeShare.self, from: Data(json.utf8))
        XCTAssertTrue(share.covers("people/a"))
        XCTAssertTrue(share.covers("people/a/x"))
        XCTAssertFalse(share.covers("people/ab"))
        XCTAssertFalse(share.covers("people"))
        XCTAssertFalse(share.isWithCircle)
        XCTAssertEqual(share.granteeDisplayName, "ben")
    }

    func testAPageCitesAFactWithItsOwner() throws {
        let json = """
            {"ownerUserId":"u","ownerUsername":"ada","ownerName":"Ada Example","ownerAvatarUrl":null,"ownerAvatarColorIndex":2,
             "path":"people/a-grandparent","name":"A Grandparent","nodeKind":"person","aliases":[],"opening":"Lives by the sea.",
             "isQuoteShared":false,
             "facts":[{"number":3,"factKind":"fact","text":"Next appointment is Tuesday.","happenedAt":null,"isInferred":false,
                       "evidenceSource":"from Ada Example's mail","evidenceQuote":""}, null],
             "edges":[{"fromPath":"people/a-grandparent","toPath":"people/a-grandparent/doctor","relation":"knows","note":"","isProposed":false}],
             "children":[{"path":"people/a-grandparent/doctor","name":"The doctor","opening":""}]}
            """
        let page = try JSONDecoder().decode(SharedKnowledgePage.self, from: Data(json.utf8))
        XCTAssertEqual(page.presentFacts.count, 1)
        XCTAssertEqual(page.citation(of: page.presentFacts[0]), "people/a-grandparent#3 (shared by Ada Example)")
        XCTAssertEqual(page.presentEdges.map(\.toPath), ["people/a-grandparent/doctor"])
        XCTAssertEqual(page.presentChildren.map(\.name), ["The doctor"])
    }

    func testASharedCitationInAnswerWordsIsLinkedToItsOwnersPage() {
        func named(_ owner: String, _ name: String) -> SharedKnowledgeRoot {
            let json =
                #"{"ownerUserId":"\#(owner)-id","ownerUsername":"\#(owner)","ownerName":"\#(name)","path":"people/a-grandparent","name":"","nodeKind":"person","opening":"","isQuoteShared":false}"#
            return try! JSONDecoder().decode(SharedKnowledgeRoot.self, from: Data(json.utf8))
        }
        let usernames = SharedCitations.usernames(of: [named("ada", "Ada Example"), named("ada", "Ada Example"), named("ben", "Ben Example")])
        XCTAssertEqual(usernames, ["ada example": "ada", "ben example": "ben"])
        // Two people who share a display name cannot be told apart by it.
        XCTAssertNil(SharedCitations.usernames(of: [named("ada", "Ada Example"), named("ada2", "Ada Example")])["ada example"])

        let words = "It is on people/a-grandparent#3 (shared by Ada Example), at three."
        XCTAssertTrue(SharedCitations.hasCitation(in: words))
        XCTAssertFalse(SharedCitations.hasCitation(in: "It is on people/a-grandparent#3."))
        XCTAssertEqual(
            SharedCitations.linking(words, usernames: usernames),
            "It is on [people/a-grandparent#3 (shared by Ada Example)](memory:people/a-grandparent#3@ada), at three.")
        // An owner not known, and a citation already made a link, stay as written.
        XCTAssertEqual(SharedCitations.linking(words, usernames: ["ben example": "ben"]), words)
        let linked = "[people/a-grandparent#3 (shared by Ada Example)](memory:people/a-grandparent#3@ada)"
        XCTAssertEqual(SharedCitations.linking(linked, usernames: usernames), linked)
        // Two in one answer are each linked.
        XCTAssertEqual(
            SharedCitations.linking("a/b#1 (shared by Ada Example) and c/d#2 (shared by Ben Example)", usernames: usernames),
            "[a/b#1 (shared by Ada Example)](memory:a/b#1@ada) and [c/d#2 (shared by Ben Example)](memory:c/d#2@ben)")
    }

    func testTheDocumentsAskForTheOperationsByName() {
        for (document, operation) in [
            (Documents.knowledgeShares, "ListAgentPageShares"), (Documents.shareKnowledgePage, "ShareAgentPage"),
            (Documents.unshareKnowledgePage, "UnshareAgentPage"), (Documents.previewKnowledgeShare, "PreviewAgentPageShare"),
            (Documents.sharedKnowledgeRoots, "ListSharedAgentPages"), (Documents.sharedKnowledgePage, "ReadSharedAgentPage"),
        ] {
            XCTAssertTrue(document.contains(operation), operation)
        }
    }
}
