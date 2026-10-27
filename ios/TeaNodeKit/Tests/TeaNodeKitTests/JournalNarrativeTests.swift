import Foundation
import XCTest

@testable import TeaNodeKit

final class JournalNarrativeTests: XCTestCase {
    private func record(_ kind: String, _ identifier: String, title: String, path: String = "", isUnavailable: Bool = false) throws -> JournalRecord {
        let json = #"{"recordKind":"\#(kind)","recordId":"\#(identifier)","reference":"\#(kind):\#(identifier)","title":"\#(title)","happenedAt":null,"dashboardPath":"\#(path)","isUnavailable":\#(isUnavailable)}"#
        return try JSONDecoder().decode(JournalRecord.self, from: Data(json.utf8))
    }

    func testAMentionBecomesTheRecordsTitleBetweenTheWords() throws {
        let park = try record("place_visit", "v1", title: "Riverside Park")
        let paragraphs = JournalNarrativeText.paragraphs(of: "You walked through [[place_visit:v1]] before the rain.", references: [park])
        XCTAssertEqual(paragraphs, [[.words("You walked through "), .mention(park, words: ""), .words(" before the rain.")]])
    }

    func testAMentionCarriesTheWordsTheStoryWroteForIt() throws {
        let park = try record("place_visit", "v1", title: "Visit to Riverside Park")
        let paragraphs = JournalNarrativeText.paragraphs(of: "Lunch at [[place_visit:v1| the park by the river ]], then home.", references: [park])
        XCTAssertEqual(paragraphs, [[.words("Lunch at "), .mention(park, words: "the park by the river"), .words(", then home.")]])
    }

    func testAMentionOfARecordThatCannotBeSeenLeavesOutItsWordsToo() throws {
        let gone = try record("mail", "m1", title: "", isUnavailable: true)
        let paragraphs = JournalNarrativeText.paragraphs(of: "You read [[mail:m1|the lease letter]] today.", references: [gone])
        XCTAssertEqual(paragraphs, [[.words("You read today.")]])
    }

    func testParagraphsAreSeparatedByBlankLines() throws {
        let paragraphs = JournalNarrativeText.paragraphs(of: "First.\n\nSecond.\n\n\n\nThird.", references: [])
        XCTAssertEqual(paragraphs, [[.words("First.")], [.words("Second.")], [.words("Third.")]])
    }

    func testAMentionOfARecordThatCannotBeSeenIsLeftOut() throws {
        let gone = try record("mail", "m1", title: "", isUnavailable: true)
        let paragraphs = JournalNarrativeText.paragraphs(of: "You read [[mail:m1]] and [[mail:unknown]] today.", references: [gone])
        XCTAssertEqual(paragraphs, [[.words("You read and today.")]])
    }

    func testTwoMentionsKeepTheSpaceBetweenThem() throws {
        let first = try record("mail", "a", title: "Lena")
        let second = try record("mail", "b", title: "City Water")
        let paragraphs = JournalNarrativeText.paragraphs(of: "[[mail:a]] [[mail:b]]", references: [first, second])
        XCTAssertEqual(paragraphs, [[.mention(first, words: ""), .words(" "), .mention(second, words: "")]])
    }

    func testAnOccurrenceOfARepeatingEventKeepsItsColonsAndAt() throws {
        let standup = try record("calendar_occurrence", "e1@2026-10-22T09:00:00Z", title: "Stand-up")
        let paragraphs = JournalNarrativeText.paragraphs(of: "Then [[calendar_occurrence:e1@2026-10-22T09:00:00Z]].", references: [standup])
        XCTAssertEqual(paragraphs, [[.words("Then "), .mention(standup, words: ""), .words(".")]])
    }

    func testBracketsThatAreNotAMentionStayAsWords() {
        let paragraphs = JournalNarrativeText.paragraphs(of: "A list [[like this one]] and an open [[ bracket.", references: [])
        XCTAssertEqual(paragraphs, [[.words("A list [[like this one]] and an open [[ bracket.")]])
    }

    // MARK: Where a record opens

    func testTheAppOpensTheRecordsItHasAScreenFor() {
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "mail", recordId: "m1", dashboardPath: "/mailbox/a/b/m1"), .mailItem("m1"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "contact", recordId: "c1", dashboardPath: ""), .contact("c1"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "photo", recordId: "p1", dashboardPath: "/photos"), .photo("p1"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "recording", recordId: "r1", dashboardPath: ""), .path("/recordings/r1"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "recording_passage", recordId: "r1", dashboardPath: ""), .path("/recordings/r1"))
    }

    func testARecordWithNoScreenOpensItsPartOrNothing() {
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "calendar_occurrence", recordId: "e@t", dashboardPath: "/calendar"), .part("calendar"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "finance_transaction", recordId: "t1", dashboardPath: "/finance"), .part("finance"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "idea", recordId: "i1", dashboardPath: ""), .part("ideas"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "goal", recordId: "g1", dashboardPath: ""), .part("goals"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "shopping_trip", recordId: "s1", dashboardPath: "/shopping"), .part("shopping"))
        // The part is by the app's own name for it, which the app opens.
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "shopping_trip", recordId: "s1", dashboardPath: ""), .part("shopping"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "chat_message", recordId: "x", dashboardPath: ""), .nowhere)
        XCTAssertFalse(JournalRecordOpening.nowhere.isOpenable)
    }

    func testAPlaceVisitAndAHealthDayOpenByTheDashboardsPath() {
        XCTAssertEqual(
            JournalRecordOpening.of(recordKind: "place_visit", recordId: "v1", dashboardPath: "/location?day=2026-10-22"), .path("/location?day=2026-10-22"))
        XCTAssertEqual(
            JournalRecordOpening.of(recordKind: "health_day", recordId: "stepCount@2026-10-22", dashboardPath: "/health?type=stepCount"),
            .path("/health?type=stepCount"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "note", recordId: "n1", dashboardPath: "/knowledge/notes/n1"), .path("/knowledge/notes/n1"))
        XCTAssertEqual(JournalRecordOpening.of(recordKind: "agent_document", recordId: "d1", dashboardPath: "/sessions/d1"), .nowhere)
    }
}
