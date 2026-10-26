import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

final class JournalTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(of request: URLRequest) throws -> [String: Any] {
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(sent["variables"] as? [String: Any])
    }

    private func query(of request: URLRequest) throws -> String {
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(sent["query"] as? String)
    }

    // MARK: Reading

    func testAPageOfTheTimelineIsDecoded() async throws {
        let answer = #"""
            {"data":{"ListJournalDays":{"days":[
              {"day":"2026-10-22","isToday":false,
               "mood":{"moodLevel":4,"moodWords":"tired, but the release is out","moodSource":"health"},
               "weather":{"weatherCode":61,"weatherKind":"rain","highCelsius":14.5,"lowCelsius":8,"precipitationMM":3.2,"placeName":"Riverside"},
               "headline":"Release day.","wordsExcerpt":"Shipped it.","hasWords":true,"facts":null,
               "coverPhotoId":"","photoIds":["photo-b","photo-c"],"hasNarrative":true},
              null,
              {"day":"2026-10-21","isToday":false,"mood":null,"weather":null,"headline":"","wordsExcerpt":"","hasWords":false,
               "facts":null,"coverPhotoId":"photo-a","photoIds":null,"hasNarrative":false}
            ],"totalCount":2,"today":"2026-10-26","firstDay":"2026-03-02"}}}
            """#
        let (connection, fake) = try connection(answering: answer)
        let page = try await connection.journalDays(first: 14, offset: 28)
        XCTAssertEqual(page.days.map(\.day), ["2026-10-22", "2026-10-21"])
        XCTAssertEqual(page.today, "2026-10-26")
        XCTAssertEqual(page.days[0].mood?.level, .good)
        XCTAssertEqual(page.days[0].mood?.isFromHealth, true)
        XCTAssertEqual(page.days[0].weather?.kind, .rain)
        XCTAssertEqual(page.days[0].shownPhotoId, "photo-b")
        // A null list is an empty one, and a cover wins over the first photo.
        XCTAssertEqual(page.days[1].photoIds, [])
        XCTAssertEqual(page.days[1].shownPhotoId, "photo-a")
        let sent = try variables(of: XCTUnwrap(fake.sent.first))
        XCTAssertEqual(sent["first"] as? Int, 14)
        XCTAssertEqual(sent["offset"] as? Int, 28)
        XCTAssertNil(sent["fromDay"])
    }

    private let dayAnswer = #"""
        {"data":{"ReadJournalDay":{
          "day":"2026-10-22","timeZone":"America/Los_Angeles","isToday":false,"weekday":"thursday","weekStart":"2026-10-19",
          "words":{"original":"Shipped it.","modifiedAt":"2026-10-22T22:48:03.123456789Z","isTouchUpOn":true,"touchedUp":"Shipped it, at last.",
                   "touchUpHow":"warmer","isTouchedUpStale":false,"touchUpPending":false,"journalNotes":null},
          "mood":null,"weather":null,"facts":null,
          "narrative":{"headline":"Release day.","narrative":"A release day.\n\nYou walked through [[place_visit:v1]].","references":[
              {"recordKind":"place_visit","recordId":"v1","reference":"place_visit:v1","title":"Riverside Park","happenedAt":null,"dashboardPath":"/location?day=2026-10-22","isUnavailable":false}],
            "writtenAt":"2026-10-23T03:12:00Z","isStale":false,"narrativePending":true,"isBeyondDepth":false,"failure":"",
            "corrections":[{"correctionText":"It was Sam at lunch.","createdAt":"2026-10-23T08:00:00Z"}],"sourceCount":71},
          "choices":[{"linkId":"l1","linkSource":"agent","linkReason":"You were asked to decide","isHidden":false,
                      "record":{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Lena: mockups","happenedAt":null,"dashboardPath":"/mailbox/x/y/m1","isUnavailable":false}}],
          "pins":null,"coverPhotoId":"photo-a","codingSessions":[],"documents":[],"documentTotal":0,"conversations":[],"chats":[],
          "mail":[],"mailTotal":0,"recordings":[],"photos":[{"photoId":"photo-a","takenAt":"2026-10-22T17:28:00Z","placeName":"","isVideo":false}],
          "photoTotal":23,"visits":[],"events":[],"reminders":[],"transactions":[],"health":null,"shoppingTrips":[],"notes":[],"memoryPages":[],
          "unavailableSections":["location"],"moments":[]}}}
        """#

    func testADayIsDecodedWithItsWordsAndNarrative() async throws {
        let (connection, fake) = try connection(answering: dayAnswer)
        let day = try await connection.journalDay("2026-10-22")
        XCTAssertEqual(day.weekStart, "2026-10-19")
        let words = try XCTUnwrap(day.words)
        XCTAssertEqual(words.modifiedAt, "2026-10-22T22:48:03.123456789Z")
        XCTAssertEqual(words.shownText, "Shipped it, at last.")
        XCTAssertTrue(words.isTouchedUpShown)
        XCTAssertEqual(words.journalNotes, [])
        let narrative = try XCTUnwrap(day.narrative)
        XCTAssertEqual(narrative.corrections.map(\.correctionText), ["It was Sam at lunch."])
        XCTAssertTrue(day.isPending)
        XCTAssertEqual(day.pins, [])
        XCTAssertEqual(day.choices.first?.record?.title, "Lena: mockups")
        XCTAssertEqual(day.unavailableSections, ["location"])
        XCTAssertEqual(day.photoTotal, 23)
        XCTAssertEqual(try variables(of: XCTUnwrap(fake.sent.first))["day"] as? String, "2026-10-22")
    }

    func testTheOriginalIsShownUntilThereIsATouchedUpText() throws {
        let json = #"{"original":"A line.","modifiedAt":null,"isTouchUpOn":true,"touchedUp":"","touchUpHow":"","isTouchedUpStale":false,"touchUpPending":true,"journalNotes":[]}"#
        let words = try JSONDecoder().decode(JournalWords.self, from: Data(json.utf8))
        XCTAssertEqual(words.shownText, "A line.")
        XCTAssertFalse(words.isTouchedUpShown)
        XCTAssertTrue(words.hasOriginal)
    }

    func testAPeriodIsDecodedWithItsDaysAndMonths() async throws {
        let answer = #"""
            {"data":{"ReadJournalPeriod":{"periodKind":"year","periodStart":"2026-01-01","periodEnd":"2026-12-31","isCurrent":true,
              "lookingBack":null,"narrative":null,"choices":[],"days":[],
              "months":[{"periodStart":"2026-10-01","headline":"The release month.","dayMoods":[0,4,5,3],"placeCount":11,"photoCount":240}],
              "totals":null,"moodCounts":[0,2,5,13,5],"dayCount":25,
              "chapters":[{"fromDay":"2026-03-01","untilDay":"2026-05-31","title":"Spring","detail":"The move."}]}}}
            """#
        let (connection, fake) = try connection(answering: answer)
        let period = try await connection.journalPeriod(.year, containing: "2026-10-22")
        XCTAssertEqual(period.kind, .year)
        XCTAssertEqual(period.months.first?.dayMoods, [0, 4, 5, 3])
        XCTAssertEqual(period.moodCounts, [0, 2, 5, 13, 5])
        XCTAssertEqual(period.chapters.first?.title, "Spring")
        let sent = try variables(of: XCTUnwrap(fake.sent.first))
        XCTAssertEqual(sent["periodKind"] as? String, "year")
        XCTAssertEqual(sent["day"] as? String, "2026-10-22")
    }

    func testTheSettingsAreDecoded() async throws {
        let answer = #"{"data":{"ReadJournalSettings":{"isWeatherOn":true,"isEveningQuestionOn":false,"eveningQuestionTime":"21:00","weekStartsOn":"sunday","narrativeDepthDays":92,"modelProviderName":"Example Provider"}}}"#
        let (connection, _) = try connection(answering: answer)
        let settings = try await connection.journalSettings()
        XCTAssertEqual(settings.eveningQuestionTime, "21:00")
        XCTAssertEqual(settings.weekStartsOn, "sunday")
        XCTAssertEqual(settings.modelProviderName, "Example Provider")
    }

    func testAnEmptyAnswerIsNotAJournalDay() async throws {
        let (connection, _) = try connection(answering: #"{"data":{"ReadJournalDay":null}}"#)
        do {
            _ = try await connection.journalDay("2026-10-22")
            XCTFail("a day was read from nothing")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, TeaNodeError.unreadableAnswer)
        }
    }

    // MARK: Writing

    func testSavingWordsSendsTheTimeTheyWereReadAtExactlyAsItCame() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"SetJournalWords":{"day":"2026-10-22"}}}"#)
        try await connection.setJournalWords(for: .day("2026-10-22"), words: "New words.", baseModifiedAt: "2026-10-22T22:48:03.123456789Z")
        let request = try XCTUnwrap(fake.sent.first)
        let sent = try variables(of: request)
        XCTAssertEqual(sent["day"] as? String, "2026-10-22")
        XCTAssertEqual(sent["words"] as? String, "New words.")
        XCTAssertEqual(sent["baseModifiedAt"] as? String, "2026-10-22T22:48:03.123456789Z")
        XCTAssertNil(sent["periodKind"])
        XCTAssertTrue(try query(of: request).contains("SetJournalWords("))
    }

    func testAFirstSaveSendsNoTimeToCheck() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"SetJournalWords":{"day":"2026-10-22"}}}"#)
        try await connection.setJournalWords(for: .day("2026-10-22"), words: "First words.", baseModifiedAt: nil)
        XCTAssertTrue(try variables(of: XCTUnwrap(fake.sent.first))["baseModifiedAt"] is NSNull)
    }

    func testLookingBackIsSavedForThePeriodContainingTheDay() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"SetJournalLookingBack":{"periodStart":"2026-10-19"}}}"#)
        try await connection.setJournalWords(for: .period(.week, containing: "2026-10-22"), words: "Too much desk.", baseModifiedAt: nil)
        let request = try XCTUnwrap(fake.sent.first)
        let sent = try variables(of: request)
        XCTAssertEqual(sent["periodKind"] as? String, "week")
        XCTAssertEqual(sent["day"] as? String, "2026-10-22")
        XCTAssertEqual(sent["lookingBack"] as? String, "Too much desk.")
        XCTAssertNil(sent["words"])
        XCTAssertTrue(try query(of: request).contains("SetJournalLookingBack("))
    }

    func testAMoodIsSetAndClearedByItsLevel() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"SetJournalMood":{"day":"2026-10-22"}}}"#)
        try await connection.setJournalMood(day: "2026-10-22", moodLevel: 4)
        try await connection.setJournalMood(day: "2026-10-22", moodLevel: nil, moodWords: "")
        let first = try variables(of: fake.sent[0])
        XCTAssertEqual(first["moodLevel"] as? Int, 4)
        XCTAssertNil(first["moodWords"])
        let second = try variables(of: fake.sent[1])
        XCTAssertTrue(second["moodLevel"] is NSNull)
        XCTAssertEqual(second["moodWords"] as? String, "")
    }

    func testTouchingUpNamesTheDayOrThePeriodAndHowOrNothing() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"TouchUpJournalWords":{"day":"2026-10-22","periodKind":""}}}"#)
        try await connection.touchUpJournalWords(for: .day("2026-10-22"), how: "shorter")
        try await connection.touchUpJournalWords(for: .period(.month, containing: "2026-10-22"), how: nil)
        let day = try variables(of: fake.sent[0])
        XCTAssertEqual(day["how"] as? String, "shorter")
        XCTAssertNil(day["periodKind"])
        let period = try variables(of: fake.sent[1])
        XCTAssertTrue(period["how"] is NSNull)
        XCTAssertEqual(period["periodKind"] as? String, "month")
    }

    func testACorrectionAndItsRemovalNameTheNarrative() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"CorrectJournalNarrative":{"narrativePending":true},"RemoveJournalCorrection":{"narrativePending":true},"WriteJournalNarrative":{"narrativePending":true}}}"#)
        try await connection.correctJournalNarrative(for: .day("2026-10-22"), correctionText: "It was Sam at lunch.")
        try await connection.removeJournalCorrection(for: .day("2026-10-22"), correctionIndex: 1)
        try await connection.writeJournalNarrative(for: .period(.week, containing: "2026-10-22"))
        XCTAssertEqual(try variables(of: fake.sent[0])["correctionText"] as? String, "It was Sam at lunch.")
        XCTAssertEqual(try variables(of: fake.sent[1])["correctionIndex"] as? Int, 1)
        XCTAssertEqual(try variables(of: fake.sent[2])["periodKind"] as? String, "week")
    }

    func testSettingsAreSentOnlyWhenChanged() async throws {
        let answer = #"{"data":{"UpdateJournalSettings":{"isWeatherOn":false,"isEveningQuestionOn":true,"eveningQuestionTime":"20:30","weekStartsOn":"monday","narrativeDepthDays":92,"modelProviderName":""}}}"#
        let (connection, fake) = try connection(answering: answer)
        let saved = try await connection.updateJournalSettings(isWeatherOn: false)
        XCTAssertFalse(saved.isWeatherOn)
        let sent = try variables(of: XCTUnwrap(fake.sent.first))
        XCTAssertEqual(sent["isWeatherOn"] as? Bool, false)
        XCTAssertNil(sent["isEveningQuestionOn"])
        XCTAssertNil(sent["eveningQuestionTime"])
    }

    func testErasingTheJournalSaysWhatWentAway() async throws {
        let (connection, _) = try connection(answering: #"{"data":{"EraseJournal":{"dayCount":120,"periodCount":30,"linkCount":400}}}"#)
        let erased = try await connection.eraseJournal()
        XCTAssertEqual(erased.dayCount, 120)
        XCTAssertEqual(erased.linkCount, 400)
    }

    // MARK: The time zone

    func testEveryRequestSaysWhichTimeZoneThePhoneIsIn() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"GetSession":{"username":"robin"}}}"#)
        _ = try await connection.username()
        XCTAssertEqual(fake.sent[0].value(forHTTPHeaderField: "X-Timezone"), TimeZone.current.identifier)
        XCTAssertEqual(Connection.shownTimeZone(timeZone: TimeZone(identifier: "Asia/Tokyo")!), "Asia/Tokyo")
    }

    // MARK: Notifications

    func testTheEveningQuestionCarriesItsDay() throws {
        let json = #"{"notificationKind":"journalEveningQuestion","title":"How was today?","body":"One tap is enough.","journalDay":"2026-10-26"}"#
        let content = try JSONDecoder().decode(NotificationContent.self, from: Data(json.utf8))
        XCTAssertEqual(content.notificationKind, "journalEveningQuestion")
        XCTAssertEqual(content.journalDay, "2026-10-26")
        XCTAssertNil(content.conversationId)
        // A server that spells the key `day` is read as well.
        let spelledShort = #"{"notificationKind":"journalEveningQuestion","title":"How was today?","body":"","day":"2026-10-27"}"#
        XCTAssertEqual(try JSONDecoder().decode(NotificationContent.self, from: Data(spelledShort.utf8)).journalDay, "2026-10-27")
        // And a notification of any other kind has no day.
        let other = #"{"notificationKind":"message","title":"Hello","body":"","conversationId":"c1"}"#
        XCTAssertNil(try JSONDecoder().decode(NotificationContent.self, from: Data(other.utf8)).journalDay)
        // It survives being written and read again.
        let again = try JSONDecoder().decode(NotificationContent.self, from: JSONEncoder().encode(content))
        XCTAssertEqual(again, content)
    }

    // MARK: Documents

    func testEveryDocumentNamesItsOperation() {
        let names: [(String, String)] = [
            (Documents.journalDays, "ListJournalDays("), (Documents.journalDay, "ReadJournalDay("),
            (Documents.journalPeriod, "ReadJournalPeriod("), (Documents.journalSettings, "ReadJournalSettings"),
            (Documents.setJournalWords, "SetJournalWords("), (Documents.setJournalMood, "SetJournalMood("),
            (Documents.setJournalLookingBack, "SetJournalLookingBack("), (Documents.touchUpJournalWords, "TouchUpJournalWords("),
            (Documents.stopJournalTouchUp, "StopJournalTouchUp("), (Documents.correctJournalNarrative, "CorrectJournalNarrative("),
            (Documents.removeJournalCorrection, "RemoveJournalCorrection("), (Documents.writeJournalNarrative, "WriteJournalNarrative("),
            (Documents.updateJournalSettings, "UpdateJournalSettings("), (Documents.eraseJournal, "EraseJournal"),
        ]
        for (document, name) in names {
            XCTAssertTrue(document.contains(name), name)
        }
    }
}
