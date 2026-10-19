import Foundation
import XCTest

@testable import TeaNodeKit

// The same table of rules is in the dashboard's calendarRepeat.test.ts; the
// two editors must read and write a rule alike.
final class CalendarRepeatTests: XCTestCase {
    private let english = Locale(identifier: "en_US")

    /// A Tuesday, in a zone that is eight hours behind UTC in winter.
    private let timed = CalendarRepeat.Context(
        startDay: CalendarDay(year: 2026, month: 10, day: 20), timeZone: TimeZone(identifier: "America/Los_Angeles")!, isAllDay: false)
    private let allDay = CalendarRepeat.Context(
        startDay: CalendarDay(year: 2026, month: 10, day: 20), timeZone: TimeZone(identifier: "UTC")!, isAllDay: true)

    private func parts(
        _ frequency: CalendarRepeat.Frequency = .weekly, intervalCount: Int = 1, weekdays: [Int] = [2],
        monthly: CalendarRepeat.Monthly = .dayOfMonth(20), end: CalendarRepeat.End = .never
    ) -> CalendarRepeat.Parts {
        CalendarRepeat.Parts(frequency: frequency, intervalCount: intervalCount, weekdays: weekdays, monthly: monthly, end: end)
    }

    private struct Row {
        let name: String
        let rule: String
        let isAllDay: Bool
        let parts: CalendarRepeat.Parts
    }

    private var rows: [Row] {
        [
            Row(name: "every day", rule: "FREQ=DAILY", isAllDay: false, parts: parts(.daily)),
            Row(name: "every 3 days", rule: "FREQ=DAILY;INTERVAL=3", isAllDay: false, parts: parts(.daily, intervalCount: 3)),
            Row(name: "every week, on the start weekday", rule: "FREQ=WEEKLY", isAllDay: false, parts: parts()),
            Row(name: "every weekday", rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", isAllDay: false, parts: parts(weekdays: [1, 2, 3, 4, 5])),
            Row(
                name: "every 2 weeks on Monday, Wednesday and Friday until a date",
                rule: "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20270101T075959Z", isAllDay: false,
                parts: parts(intervalCount: 2, weekdays: [1, 3, 5], end: .onDate(CalendarDay(year: 2026, month: 12, day: 31)))),
            Row(
                name: "until a date in summer time", rule: "FREQ=DAILY;UNTIL=20261101T065959Z", isAllDay: false,
                parts: parts(.daily, end: .onDate(CalendarDay(year: 2026, month: 10, day: 31)))),
            Row(
                name: "every week until a date, all day", rule: "FREQ=WEEKLY;BYDAY=SA,SU;UNTIL=20261231", isAllDay: true,
                parts: parts(weekdays: [6, 7], end: .onDate(CalendarDay(year: 2026, month: 12, day: 31)))),
            Row(name: "every month, on the start day", rule: "FREQ=MONTHLY", isAllDay: false, parts: parts(.monthly)),
            Row(name: "every month on day 15", rule: "FREQ=MONTHLY;BYMONTHDAY=15", isAllDay: false, parts: parts(.monthly, monthly: .dayOfMonth(15))),
            Row(
                name: "every month on the second Tuesday", rule: "FREQ=MONTHLY;BYDAY=2TU", isAllDay: false,
                parts: parts(.monthly, monthly: .ordinalWeekday(ordinal: 2, weekday: 2))),
            Row(
                name: "every 3 months on the last Friday", rule: "FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR", isAllDay: false,
                parts: parts(.monthly, intervalCount: 3, monthly: .ordinalWeekday(ordinal: -1, weekday: 5))),
            Row(name: "every year", rule: "FREQ=YEARLY", isAllDay: false, parts: parts(.yearly)),
            Row(name: "ten times", rule: "FREQ=DAILY;COUNT=10", isAllDay: false, parts: parts(.daily, end: .afterCount(occurrenceCount: 10))),
        ]
    }

    private func context(_ isAllDay: Bool) -> CalendarRepeat.Context { isAllDay ? allDay : timed }

    func testEveryRuleInTheTableReadsAndWrites() {
        for row in rows {
            XCTAssertEqual(CalendarRepeat.parse(row.rule, context: context(row.isAllDay)), row.parts, row.name)
            XCTAssertEqual(CalendarRepeat.write(row.parts, context: context(row.isAllDay)), row.rule, row.name)
        }
    }

    func testLowerCaseAndAnIntervalOfOneAreReadAndWrittenPlainly() throws {
        let read = try XCTUnwrap(CalendarRepeat.parse("freq=weekly;interval=1;byday=mo,we", context: timed))
        XCTAssertEqual(read, parts(weekdays: [1, 3]))
        XCTAssertEqual(CalendarRepeat.write(read, context: timed), "FREQ=WEEKLY;BYDAY=MO,WE")
    }

    func testAnEmptyRuleAndTextThatIsNotARuleHaveNothingToRead() {
        for rule in ["", "   ", "FREQ=HOURLY", "FREQ=DAILY;INTERVAL=0", "FREQ=DAILY;COUNT=x", "FREQ=WEEKLY;BYDAY=XX", "every other tuesday"] {
            XCTAssertNil(CalendarRepeat.parse(rule, context: timed), rule)
        }
    }

    func testARuleWithPartsItCannotShowIsKeptUntilChanged() throws {
        let rule = "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1"
        let read = try XCTUnwrap(CalendarRepeat.parse(rule, context: timed))
        XCTAssertTrue(read.hasUnshownParts)
        XCTAssertEqual(read.frequency, .monthly)
        // Left as it was shown, the rule is kept exactly.
        XCTAssertEqual(CalendarRepeat.editedRule(original: rule, initial: read, edited: read, context: timed), rule)
        // Changed, it becomes what the editor shows.
        var changed = read
        changed.intervalCount = 2
        XCTAssertEqual(CalendarRepeat.editedRule(original: rule, initial: read, edited: changed, context: timed), "FREQ=MONTHLY;INTERVAL=2")
        // A rule the editor cannot read at all is kept the same way.
        let unreadable = CalendarRepeat.defaultParts(frequency: .weekly, context: timed)
        XCTAssertEqual(CalendarRepeat.editedRule(original: "FREQ=SECONDLY", initial: unreadable, edited: unreadable, context: timed), "FREQ=SECONDLY")
    }

    func testOtherPartsTheEditorDoesNotShowAreMarked() {
        for rule in [
            "FREQ=YEARLY;BYMONTH=10", "FREQ=MONTHLY;BYMONTHDAY=1,15", "FREQ=MONTHLY;BYMONTHDAY=-1", "FREQ=WEEKLY;WKST=SU", "FREQ=DAILY;BYHOUR=9",
            "FREQ=WEEKLY;BYDAY=1MO", "FREQ=DAILY;COUNT=3;UNTIL=20261231",
        ] {
            XCTAssertEqual(CalendarRepeat.parse(rule, context: timed)?.hasUnshownParts, true, rule)
        }
    }

    func testPresetsAreNamedWhateverOrderAnotherProgramWroteThem() {
        XCTAssertEqual(CalendarRepeat.Preset(rule: ""), .never)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "FREQ=DAILY"), .daily)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "freq=weekly"), .weekly)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"), .weekdays)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "BYDAY=FR,MO,TU,WE,TH;FREQ=WEEKLY"), .weekdays)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "FREQ=MONTHLY"), .monthly)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "FREQ=YEARLY"), .yearly)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "FREQ=WEEKLY;INTERVAL=2"), .custom)
        XCTAssertEqual(CalendarRepeat.Preset(rule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH"), .custom)
        XCTAssertEqual(CalendarRepeat.Preset.weekdays.rule, "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR")
        XCTAssertNil(CalendarRepeat.Preset.custom.rule)
    }

    func testTheEndOfTheChosenDayIsInTheEventZone() {
        let end = CalendarRepeat.End.onDate(CalendarDay(year: 2026, month: 12, day: 31))
        XCTAssertTrue(CalendarRepeat.write(parts(end: end), context: timed).hasSuffix("UNTIL=20270101T075959Z"))
        let summer = CalendarRepeat.End.onDate(CalendarDay(year: 2026, month: 7, day: 1))
        XCTAssertTrue(CalendarRepeat.write(parts(end: summer), context: timed).hasSuffix("UNTIL=20260702T065959Z"))
        let tokyo = CalendarRepeat.Context(startDay: timed.startDay, timeZone: TimeZone(identifier: "Asia/Tokyo")!, isAllDay: false)
        XCTAssertTrue(CalendarRepeat.write(parts(end: end), context: tokyo).hasSuffix("UNTIL=20261231T145959Z"))
    }

    func testDaysKnowTheirWeekdayAndOrdinal() {
        XCTAssertEqual(CalendarRepeat.weekday(of: CalendarDay(year: 2026, month: 10, day: 20)), 2)
        XCTAssertEqual(CalendarRepeat.weekday(of: CalendarDay(year: 2026, month: 10, day: 18)), 7)
        XCTAssertEqual(CalendarRepeat.weekday(of: CalendarDay(year: 2026, month: 10, day: 19)), 1)
        XCTAssertEqual(CalendarRepeat.ordinal(of: CalendarDay(year: 2026, month: 10, day: 20)), 3)
        XCTAssertEqual(CalendarRepeat.ordinal(of: CalendarDay(year: 2026, month: 10, day: 1)), 1)
        // The fifth Saturday of the month is the last one.
        XCTAssertEqual(CalendarRepeat.ordinal(of: CalendarDay(year: 2026, month: 10, day: 31)), -1)
        XCTAssertEqual(CalendarRepeat.dayOneYearAfter(CalendarDay(year: 2026, month: 10, day: 20)), CalendarDay(year: 2027, month: 10, day: 20))
    }

    private func sentence(_ rule: String, context: CalendarRepeat.Context? = nil) throws -> CalendarRepeat.Sentence {
        let read = try XCTUnwrap(CalendarRepeat.parse(rule, context: context ?? timed))
        return CalendarRepeat.sentence(for: read, locale: english)
    }

    func testTheSentenceUsesTheLocalesNamesListsAndDates() throws {
        XCTAssertEqual(try sentence("FREQ=DAILY").cadence, .daily(intervalCount: 1))
        XCTAssertEqual(try sentence("FREQ=DAILY;INTERVAL=3").cadence, .daily(intervalCount: 3))
        XCTAssertEqual(
            try sentence("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR").cadence,
            .weekly(intervalCount: 1, weekdayNamesText: "Monday, Tuesday, Wednesday, Thursday, and Friday"))
        let spaced = try sentence("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20270101T075959Z")
        XCTAssertEqual(spaced.cadence, .weekly(intervalCount: 2, weekdayNamesText: "Monday, Wednesday, and Friday"))
        XCTAssertEqual(spaced.ending, .onDate(dateText: "December 31, 2026"))
        XCTAssertEqual(try sentence("FREQ=MONTHLY;BYMONTHDAY=15").cadence, .monthlyDay(intervalCount: 1, dayOfMonth: 15))
        XCTAssertEqual(try sentence("FREQ=MONTHLY;BYDAY=2TU").cadence, .monthlyWeekday(intervalCount: 1, ordinal: 2, weekdayName: "Tuesday"))
        XCTAssertEqual(try sentence("FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR").cadence, .monthlyWeekday(intervalCount: 3, ordinal: -1, weekdayName: "Friday"))
        XCTAssertEqual(try sentence("FREQ=YEARLY").cadence, .yearly(intervalCount: 1))
        XCTAssertEqual(try sentence("FREQ=DAILY;COUNT=10").ending, .afterCount(occurrenceCount: 10))
        XCTAssertEqual(try sentence("FREQ=WEEKLY;UNTIL=20261231", context: allDay).ending, .onDate(dateText: "December 31, 2026"))
        XCTAssertEqual(try sentence("FREQ=WEEKLY").ending, .never)
    }

    func testTheSentenceNamesDaysInTheReadersLanguage() throws {
        let read = try XCTUnwrap(CalendarRepeat.parse("FREQ=WEEKLY;BYDAY=MO,WE", context: timed))
        let japanese = CalendarRepeat.sentence(for: read, locale: Locale(identifier: "ja_JP"))
        guard case .weekly(_, let weekdayNamesText) = japanese.cadence else { return XCTFail("not weekly") }
        XCTAssertTrue(weekdayNamesText.contains("月曜日"))
        XCTAssertFalse(weekdayNamesText.contains("Monday"))
    }
}
