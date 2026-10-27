import Foundation
import XCTest

@testable import TeaNodeKit

final class SearchTests: XCTestCase {
    private func decode(_ json: String) throws -> SearchEverythingResults {
        try JSONDecoder().decode(SearchEverythingResults.self, from: Data(json.utf8))
    }

    func testAreasDecodeInTheOrderTheServerSentThem() throws {
        let results = try decode(
            #"""
            {"words":"harbor","failedAreas":["finance"],"areas":[
              {"searchArea":"chats","hasMore":false,"results":[
                {"searchArea":"chats","targetId":"c1","titleText":"Weekend trip","detailText":"We could walk the harbor.","happenedAt":"2026-10-09T18:30:00.123456789Z"}]},
              {"searchArea":"mail","hasMore":true,"results":[
                {"searchArea":"mail","targetId":"m1","titleText":"Harbor tickets","detailText":"orders@example.com","happenedAt":null},
                {"searchArea":"mail","targetId":"m2","titleText":"Re: Harbor tickets","detailText":"sam@example.org"}]}]}
            """#)
        XCTAssertEqual(results.words, "harbor")
        XCTAssertEqual(results.areas.map(\.searchArea), ["chats", "mail"])
        XCTAssertEqual(results.areas.map(\.hasMore), [false, true])
        XCTAssertEqual(results.areas[1].results.map(\.targetId), ["m1", "m2"])
        XCTAssertEqual(results.failedAreas, ["finance"])
        XCTAssertFalse(results.isEmpty)
    }

    func testAFractionOfASecondDoesNotLoseTheTime() throws {
        let results = try decode(
            #"{"words":"a","areas":[{"searchArea":"chats","hasMore":false,"results":[{"searchArea":"chats","targetId":"c1","titleText":"t","detailText":"","happenedAt":"2026-10-09T18:30:00.123456789Z"}]}],"failedAreas":[]}"#)
        let result = try XCTUnwrap(results.areas.first?.results.first)
        XCTAssertEqual(result.happenedDate, ServerTime.date("2026-10-09T18:30:00Z"))
    }

    func testNothingFoundDecodesWithNullsAndMissingFields() throws {
        let results = try decode(#"{"words":"zzz","areas":null,"failedAreas":null}"#)
        XCTAssertTrue(results.areas.isEmpty)
        XCTAssertTrue(results.failedAreas.isEmpty)
        XCTAssertTrue(results.isEmpty)
        let bare = try decode(#"{"words":"zzz"}"#)
        XCTAssertTrue(bare.isEmpty)
    }

    func testAnAreaWithNullResultsIsEmptyNotAFailure() throws {
        let results = try decode(#"{"words":"a","areas":[{"searchArea":"photos","hasMore":false,"results":null}],"failedAreas":[]}"#)
        XCTAssertEqual(results.areas.count, 1)
        XCTAssertTrue(results.areas[0].results.isEmpty)
        XCTAssertTrue(results.isEmpty)
    }

    func testTheMainConversationSaysItIsMain() throws {
        let results = try decode(
            #"{"words":"a","areas":[{"searchArea":"chats","hasMore":false,"results":[{"searchArea":"chats","targetId":"c1","titleText":"Main chat","detailText":"","isMain":true},{"searchArea":"chats","targetId":"c2","titleText":"Tank cleaning","detailText":""}]}],"failedAreas":[]}"#)
        let chats = try XCTUnwrap(results.areas.first?.results)
        XCTAssertEqual(chats.map(\.isMain), [true, false])
    }

    func testAResultWithoutTextsKeepsWhatItHas() throws {
        let results = try decode(#"{"words":"a","areas":[{"searchArea":"contacts","hasMore":false,"results":[{"searchArea":"contacts","targetId":"k1"}]}],"failedAreas":[]}"#)
        let result = try XCTUnwrap(results.areas.first?.results.first)
        XCTAssertEqual(result.titleText, "")
        XCTAssertEqual(result.detailText, "")
        XCTAssertNil(result.happenedDate)
        XCTAssertEqual(result.id, "contacts/k1")
    }

    func testAnAreaTheAppDoesNotKnowOpensNowhere() throws {
        let results = try decode(#"{"words":"a","areas":[{"searchArea":"reminders","hasMore":false,"results":[{"searchArea":"reminders","targetId":"r1","titleText":"Call"}]}],"failedAreas":[]}"#)
        let result = try XCTUnwrap(results.areas.first?.results.first)
        XCTAssertNil(result.area)
        XCTAssertEqual(result.opening, .nowhere)
    }

    func testEachAreaOpensWhereItLives() {
        func opening(_ area: String, _ targetId: String = "x1") -> SearchOpening {
            SearchResult(searchArea: area, targetId: targetId, titleText: "t").opening
        }
        XCTAssertEqual(opening("chats"), .conversation(conversationId: "x1"))
        XCTAssertEqual(opening("mail"), .mailItem(mailItemId: "x1"))
        XCTAssertEqual(opening("contacts"), .contact(contactId: "x1"))
        XCTAssertEqual(opening("photos"), .photo(photoId: "x1"))
        XCTAssertEqual(opening("memory", "people/robin"), .knowledgePage(path: "people/robin"))
        XCTAssertEqual(opening("recordings"), .recording(recordingId: "x1"))
        XCTAssertEqual(opening("places"), .partOnly(.places))
        XCTAssertEqual(opening("finance"), .partOnly(.finance))
        XCTAssertEqual(opening("documents"), .nowhere)
        XCTAssertEqual(opening("mail", ""), .nowhere)
    }

    func testTheServersAreasAreAllKnown() {
        XCTAssertEqual(
            SearchArea.allCases.map(\.rawValue),
            ["chats", "mail", "contacts", "photos", "memory", "documents", "recordings", "places", "finance"])
    }

    func testTypedWordsAreTrimmedAndSquashed() {
        XCTAssertEqual(SearchWords.normalized("  harbor \n  tickets  "), "harbor tickets")
        XCTAssertEqual(SearchWords.normalized("harbor"), "harbor")
        XCTAssertNil(SearchWords.normalized("   "))
        XCTAssertNil(SearchWords.normalized(""))
    }

    func testTheQueryAsksForTheWordsAndTheLimit() {
        XCTAssertTrue(Documents.searchEverything.contains("SearchEverything(words: $words, perAreaLimit: $perAreaLimit)"))
        XCTAssertTrue(Documents.searchEverything.contains("failedAreas"))
        XCTAssertTrue(Documents.searchEverything.contains("results { searchArea targetId titleText detailText happenedAt isMain }"))
    }
}
