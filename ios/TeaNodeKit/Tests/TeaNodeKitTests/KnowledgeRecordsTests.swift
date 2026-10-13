import Foundation
import XCTest

@testable import TeaNodeKit

final class KnowledgeRecordsTests: XCTestCase {
    private func link(
        _ id: String, kind: String = "mail", relation: String = "wrote", status: String = "stated", path: String = "/mailbox/starred/item-1",
        unavailable: Bool = false, node: String = "people/fixture-guest", start: Int = 0, end: Int = 0
    ) throws -> KnowledgeRecordLink {
        try JSONDecoder().decode(
            KnowledgeRecordLink.self,
            from: Data(
                """
                {"id":"\(id)","nodePath":"\(node)","nodeName":"Fixture Guest","nodeKind":"person","recordKind":"\(kind)","recordId":"record-\(id)",
                 "recordRelation":"\(relation)","linkStatus":"\(status)","passageStartMS":\(start),"passageEndMS":\(end),"transcriptRunId":"",
                 "note":"","happenedAt":null,"createdAt":"2026-01-01T00:00:00Z","recordTitle":"Fixture subject",
                 "recordHappenedAt":"2026-03-04T10:00:00Z","dashboardPath":"\(path)","isUnavailable":\(unavailable)}
                """.utf8))
    }

    func testLinksAreGroupedByKindInTheOrderAPageListsThem() throws {
        let groups = KnowledgeRecordKinds.grouped([
            try link("a", kind: "recording_passage"), try link("b", kind: "mail"), try link("c", kind: "contact"),
            try link("d", kind: "mail"), try link("e", kind: "something_new"),
        ])
        XCTAssertEqual(groups.map(\.recordKind), ["contact", "mail", "recording_passage", "something_new"])
        XCTAssertEqual(groups[1].links.map(\.id), ["b", "d"])
    }

    func testAGuessIsProposedAndOnlyAnExistingRecordWithAPlaceOpens() throws {
        XCTAssertFalse(try link("a").isProposed)
        XCTAssertTrue(try link("a", status: "proposed").isProposed)
        XCTAssertTrue(try link("a").canOpen)
        XCTAssertFalse(try link("a", unavailable: true).canOpen)
        XCTAssertFalse(try link("a", path: "").canOpen)
        XCTAssertFalse(try link("a", path: "https://elsewhere.example.com/x").canOpen)
        XCTAssertNotNil(try link("a").date)
    }

    func testARecordIsNamedAsTheCommandLineNamesIt() throws {
        XCTAssertEqual(try link("a").reference, "mail:record-a")
        XCTAssertEqual(
            try link("p", kind: "recording_passage", relation: "spoke_in", path: "/recordings/rec-1", start: 1500, end: 4000).reference,
            "recording_passage:record-p@1500-4000")
    }

    func testEachPageThatLinksToARecordIsListedOnceAStatementWinningOverAGuess() throws {
        let pages = KnowledgeRecordKinds.pages(of: [
            try link("a", status: "proposed"), try link("b", relation: "received"), try link("c", node: "topics/fixture-topic"),
        ])
        XCTAssertEqual(pages.map(\.nodePath), ["people/fixture-guest", "topics/fixture-topic"])
        XCTAssertEqual(pages.map(\.id), ["b", "c"])
    }

    func testTheDashboardsPathSaysWhereTheAppOpensARecord() {
        XCTAssertEqual(RecordTarget(dashboardPath: "/mailbox/starred/item-1"), .mail(itemId: "item-1"))
        XCTAssertEqual(RecordTarget(dashboardPath: "/contacts?contact=card-1"), .contact(id: "card-1"))
        XCTAssertEqual(RecordTarget(dashboardPath: "/calendar?event=event-1"), .calendarEvent(id: "event-1"))
        XCTAssertEqual(RecordTarget(dashboardPath: "/reminders?reminder=reminder-1"), .reminder(id: "reminder-1"))
        XCTAssertEqual(RecordTarget(dashboardPath: "/agent?conversation=conversation-1&message=message-1"), .conversation(id: "conversation-1"))
        XCTAssertEqual(RecordTarget(dashboardPath: "/finance/transactions?transaction=tx-1"), .financeTransaction(id: "tx-1"))
        XCTAssertEqual(RecordTarget(dashboardPath: "/finance/receipts?receipt=receipt-1"), .financeReceipt(id: "receipt-1"))
        let passage = "/recordings/rec-1?version=run-1&startMS=1500&endMS=4000"
        XCTAssertEqual(RecordTarget(dashboardPath: passage), .recording(path: passage))
    }

    func testAPathThatNamesNoRecordOpensNothing() {
        for path in ["", "contacts?contact=card-1", "/contacts", "/calendar?event=", "/finance/transactions", "/mailbox/starred", "/settings/agent", "/recordings"] {
            XCTAssertNil(RecordTarget(dashboardPath: path), path)
        }
    }

    func testAPageReadFromAServerThatKnowsNoRecordLinksStillDecodes() throws {
        let page = try JSONDecoder().decode(
            KnowledgePage.self, from: Data(#"{"node":null,"facts":[],"folded":[],"children":[],"attachments":[],"contact":null}"#.utf8))
        XCTAssertNil(page.recordLinks)
        XCTAssertNil(page.recordLinkCount)
        let withLinks = try JSONDecoder().decode(
            KnowledgePage.self,
            from: Data(
                """
                {"node":null,"facts":[],"folded":[],"children":[],"attachments":[],"contact":null,"recordLinkCount":1,
                 "recordLinks":[{"id":"a","nodePath":"p","nodeName":"P","nodeKind":"person","recordKind":"mail","recordId":"r","recordRelation":"wrote",
                  "linkStatus":"stated","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"","happenedAt":null,"createdAt":"2026-01-01T00:00:00Z",
                  "recordTitle":"T","recordHappenedAt":null,"dashboardPath":"/mailbox/starred/r","isUnavailable":false}],
                 "evidenceRecords":[{"factId":"f","evidenceIndex":0,"recordKind":"mail","recordId":"r","passageStartMS":0,"passageEndMS":0,
                  "transcriptRunId":"","recordTitle":"T","dashboardPath":"/mailbox/starred/r","isUnavailable":false}]}
                """.utf8))
        XCTAssertEqual(withLinks.recordLinks?.compactMap { $0 }.count, 1)
        let fact = try JSONDecoder().decode(
            KnowledgeFact.self,
            from: Data(#"{"id":"f","number":1,"kind":"fact","text":"T","happenedAt":null,"inferred":false,"evidence":[],"audiences":[]}"#.utf8))
        XCTAssertEqual(withLinks.records(of: fact).map(\.recordId), ["r"])
        let other = try JSONDecoder().decode(
            KnowledgeFact.self,
            from: Data(#"{"id":"g","number":2,"kind":"fact","text":"T","happenedAt":null,"inferred":false,"evidence":[],"audiences":[]}"#.utf8))
        XCTAssertTrue(withLinks.records(of: other).isEmpty)
        XCTAssertEqual(withLinks.evidenceRecords?.compactMap { $0 }.first?.canOpen, true)
    }

    func testTheDocumentsNameTheOperationsTheServerHas() {
        XCTAssertTrue(Documents.readRecordMemoryLinks.contains("ReadRecordMemoryLinks("))
        XCTAssertTrue(Documents.linkKnowledgeRecord.contains("LinkAgentRecord("))
        XCTAssertTrue(Documents.unlinkKnowledgeRecord.contains("UnlinkAgentRecord("))
        XCTAssertTrue(Documents.setKnowledgeContact.contains("SetAgentNodeContact("))
        XCTAssertTrue(Documents.knowledgePage.contains("recordLinks {"))
        XCTAssertTrue(Documents.knowledgePage.contains("evidenceRecords {"))
    }
}
