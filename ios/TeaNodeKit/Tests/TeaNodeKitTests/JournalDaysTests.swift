import Foundation
import XCTest

@testable import TeaNodeKit

final class JournalDaysTests: XCTestCase {
    private let utc = TimeZone(identifier: "UTC")!
    private let tokyo = TimeZone(identifier: "Asia/Tokyo")!
    private let losAngeles = TimeZone(identifier: "America/Los_Angeles")!

    func testADayCountsTheSameInEveryTimeZone() throws {
        for zone in [utc, tokyo, losAngeles] {
            let date = try XCTUnwrap(JournalDays.date("2026-10-22", timeZone: zone))
            XCTAssertEqual(JournalDays.text(date, timeZone: zone), "2026-10-22")
            XCTAssertEqual(JournalDays.adding(1, to: "2026-10-22", timeZone: zone), "2026-10-23")
            XCTAssertEqual(JournalDays.adding(-30, to: "2026-10-22", timeZone: zone), "2026-09-22")
        }
    }

    func testTextThatIsNotADayIsNotOne() {
        XCTAssertNil(JournalDays.date("2026-06-31", timeZone: utc))
        XCTAssertNil(JournalDays.date("yesterday", timeZone: utc))
        XCTAssertNil(JournalDays.adding(1, to: "2026-13-01", timeZone: utc))
    }

    func testTheClocksChangingDoNotMoveADay() {
        // The night the clocks go back in Los Angeles in 2026 is 25 hours long.
        XCTAssertEqual(JournalDays.adding(1, to: "2026-11-01", timeZone: losAngeles), "2026-11-02")
        XCTAssertEqual(JournalDays.adding(-1, to: "2026-03-09", timeZone: losAngeles), "2026-03-08")
    }

    func testAWeekStartsWhereTheLocaleStartsIt() {
        // 2026-10-22 is a Thursday.
        XCTAssertEqual(JournalDays.weekStart(of: "2026-10-22", weekStartsOn: "monday", timeZone: utc), "2026-10-19")
        XCTAssertEqual(JournalDays.weekStart(of: "2026-10-22", weekStartsOn: "sunday", timeZone: utc), "2026-10-18")
        // The first day of a week is its own week's start.
        XCTAssertEqual(JournalDays.weekStart(of: "2026-10-19", weekStartsOn: "monday", timeZone: utc), "2026-10-19")
        XCTAssertEqual(JournalDays.weekStart(of: "2026-10-18", weekStartsOn: "sunday", timeZone: utc), "2026-10-18")
        // A Sunday ends a week that starts on Monday.
        XCTAssertEqual(JournalDays.weekStart(of: "2026-10-25", weekStartsOn: "monday", timeZone: utc), "2026-10-19")
        // A week reaches back across a month and a year.
        XCTAssertEqual(JournalDays.weekStart(of: "2027-01-01", weekStartsOn: "monday", timeZone: utc), "2026-12-28")
    }

    func testMonthsAndYearsStartOnTheirFirstDay() {
        XCTAssertEqual(JournalDays.monthStart(of: "2026-10-22"), "2026-10-01")
        XCTAssertEqual(JournalDays.yearStart(of: "2026-10-22"), "2026-01-01")
        XCTAssertNil(JournalDays.monthStart(of: "October"))
        XCTAssertEqual(JournalDays.periodStart(of: "2026-10-22", periodKind: .month, weekStartsOn: "monday", timeZone: utc), "2026-10-01")
    }

    func testAPeriodMovesByItsOwnLength() {
        XCTAssertEqual(JournalDays.movedPeriodStart(from: "2026-10-19", periodKind: .week, by: 1, timeZone: utc), "2026-10-26")
        XCTAssertEqual(JournalDays.movedPeriodStart(from: "2026-10-19", periodKind: .week, by: -1, timeZone: utc), "2026-10-12")
        XCTAssertEqual(JournalDays.movedPeriodStart(from: "2026-12-01", periodKind: .month, by: 1, timeZone: utc), "2027-01-01")
        XCTAssertEqual(JournalDays.movedPeriodStart(from: "2026-03-01", periodKind: .month, by: -1, timeZone: utc), "2026-02-01")
        XCTAssertEqual(JournalDays.movedPeriodStart(from: "2026-01-01", periodKind: .year, by: -1, timeZone: utc), "2025-01-01")
    }

    func testAMonthIsRowsOfSevenDaysStartingOnTheWeeksFirstDay() throws {
        // October 2026 starts on a Thursday and has 31 days.
        let mondayRows = JournalDays.monthRows(monthStart: "2026-10-01", weekStartsOn: "monday", timeZone: utc)
        XCTAssertEqual(mondayRows.count, 5)
        XCTAssertTrue(mondayRows.allSatisfy { $0.count == 7 })
        XCTAssertEqual(mondayRows[0].map { $0 ?? "-" }, ["-", "-", "-", "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"])
        XCTAssertEqual(mondayRows[4].map { $0 ?? "-" }, ["2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", "-"])
        let sundayRows = JournalDays.monthRows(monthStart: "2026-10-01", weekStartsOn: "sunday", timeZone: utc)
        XCTAssertEqual(sundayRows[0].map { $0 ?? "-" }, ["-", "-", "-", "-", "2026-10-01", "2026-10-02", "2026-10-03"])
        XCTAssertEqual(mondayRows.joined().compactMap { $0 }.count, 31)
        XCTAssertEqual(JournalDays.monthRows(monthStart: "nonsense", weekStartsOn: "monday", timeZone: utc), [])
    }

    private func summary(_ day: String) throws -> JournalDaySummary {
        let json = #"{"day":"\#(day)","isToday":false,"mood":null,"weather":null,"headline":"","wordsExcerpt":"","hasWords":false,"facts":null,"coverPhotoId":"","photoIds":null,"hasNarrative":false}"#
        return try JSONDecoder().decode(JournalDaySummary.self, from: Data(json.utf8))
    }

    func testTheTimelineIsGroupedByWeekNewestFirst() throws {
        let days = try ["2026-10-25", "2026-10-24", "2026-10-22", "2026-10-18", "2026-10-12"].map(summary)
        let groups = JournalDays.weekGroups(of: days, weekStartsOn: "monday", timeZone: utc)
        XCTAssertEqual(groups.map(\.weekStart), ["2026-10-19", "2026-10-12"])
        XCTAssertEqual(groups[0].days.map(\.day), ["2026-10-25", "2026-10-24", "2026-10-22"])
        XCTAssertEqual(groups[1].days.map(\.day), ["2026-10-18", "2026-10-12"])
        // The same days with weeks that start on Sunday: the 25th starts a week of its own.
        let sundayGroups = JournalDays.weekGroups(of: days, weekStartsOn: "sunday", timeZone: utc)
        XCTAssertEqual(sundayGroups.map(\.weekStart), ["2026-10-25", "2026-10-18", "2026-10-11"])
        XCTAssertEqual(sundayGroups[1].days.map(\.day), ["2026-10-24", "2026-10-22", "2026-10-18"])
    }

    func testMoneyIsCountedInTheCurrencysOwnSmallestUnit() {
        XCTAssertEqual(JournalMoney.amount(minorUnits: 5210, currency: "USD"), Decimal(string: "52.1"))
        XCTAssertEqual(JournalMoney.amount(minorUnits: -780, currency: "usd"), Decimal(string: "-7.8"))
        XCTAssertEqual(JournalMoney.amount(minorUnits: 1200, currency: "JPY"), 1200)
        XCTAssertEqual(JournalMoney.amount(minorUnits: 1500, currency: "KWD"), Decimal(string: "1.5"))
    }

    func testTheDayIsReadAgainOnlyWhileSomethingIsBeingWritten() {
        XCTAssertEqual(JournalPolling.attemptLimit, 40)
        XCTAssertTrue(JournalPolling.shouldRead(isPending: true, attemptCount: 0))
        XCTAssertTrue(JournalPolling.shouldRead(isPending: true, attemptCount: 39))
        XCTAssertFalse(JournalPolling.shouldRead(isPending: true, attemptCount: 40))
        XCTAssertFalse(JournalPolling.shouldRead(isPending: false, attemptCount: 0))
    }

    func testTheEveningQuestionTimeIsHoursAndMinutes() {
        XCTAssertEqual(JournalEveningTime.parts(of: "21:30")?.hour, 21)
        XCTAssertEqual(JournalEveningTime.parts(of: "21:30")?.minute, 30)
        XCTAssertEqual(JournalEveningTime.parts(of: "9:05")?.hour, 9)
        XCTAssertNil(JournalEveningTime.parts(of: "24:00"))
        XCTAssertNil(JournalEveningTime.parts(of: "21:75"))
        XCTAssertNil(JournalEveningTime.parts(of: "evening"))
        XCTAssertNil(JournalEveningTime.parts(of: "21"))
        XCTAssertEqual(JournalEveningTime.text(hour: 9, minute: 5), "09:05")
        XCTAssertEqual(JournalEveningTime.parts(of: JournalEveningTime.defaultText)?.hour, 21)
    }

    func testAMoodLevelIsOneToFive() {
        XCTAssertEqual(JournalMoodLevel.allCases.map(\.rawValue), [1, 2, 3, 4, 5])
        XCTAssertEqual(JournalMoodLevel(moodLevel: 4), .good)
        XCTAssertNil(JournalMoodLevel(moodLevel: 0))
        XCTAssertNil(JournalMoodLevel(moodLevel: 6))
        XCTAssertNil(JournalMoodLevel(moodLevel: nil))
    }
}
