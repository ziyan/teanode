import Foundation
import XCTest

@testable import TeaNodeKit

final class CalendarMonthTests: XCTestCase {
    private func calendar(firstWeekday: Int, minimumDaysInFirstWeek: Int, zone: String = "UTC") -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: zone)!
        calendar.firstWeekday = firstWeekday
        calendar.minimumDaysInFirstWeek = minimumDaysInFirstWeek
        return calendar
    }

    private func event(_ id: String, _ startsAt: String, _ endsAt: String, allDay: Bool = false, summary: String = "Event") throws -> CalendarEvent {
        let json = #"{"id":"\#(id)","calendarId":"c","summary":"\#(summary)","startsAt":"\#(startsAt)","endsAt":"\#(endsAt)","allDay":\#(allDay),"recurring":false}"#
        return try JSONDecoder().decode(CalendarEvent.self, from: Data(json.utf8))
    }

    func testMonthsCountOnAcrossTheYear() {
        let october = CalendarMonth(year: 2026, month: 10)
        XCTAssertEqual(october.adding(months: 3), CalendarMonth(year: 2027, month: 1))
        XCTAssertEqual(october.adding(months: -10), CalendarMonth(year: 2025, month: 12))
        XCTAssertEqual(october.adding(months: 1).id - october.id, 1)
        XCTAssertLessThan(october, october.adding(months: 1))
    }

    func testAMonthStartingOnThursdayFromMonday() {
        // October 2026 starts on a Thursday and has 31 days.
        let layout = MonthLayout(month: CalendarMonth(year: 2026, month: 10), calendar: calendar(firstWeekday: 2, minimumDaysInFirstWeek: 4))
        XCTAssertEqual(layout.firstDayColumn, 3)
        XCTAssertEqual(layout.weeks.count, 5)
        XCTAssertEqual(layout.weeks[0].days.prefix(3).compactMap { $0 }.count, 0)
        XCTAssertEqual(layout.weeks[0].days[3], CalendarDay(year: 2026, month: 10, day: 1))
        XCTAssertEqual(layout.weeks[4].days[5], CalendarDay(year: 2026, month: 10, day: 31))
        XCTAssertNil(layout.weeks[4].days[6])
        XCTAssertTrue(layout.weeks.allSatisfy { $0.days.count == 7 })
        // ISO 8601 weeks: October 1, 2026 is in week 40.
        XCTAssertEqual(layout.weeks.map(\.weekNumber), [40, 41, 42, 43, 44])
    }

    func testAMonthFromSundayFillsItsRowsExactly() {
        let layout = MonthLayout(month: CalendarMonth(year: 2026, month: 10), calendar: calendar(firstWeekday: 1, minimumDaysInFirstWeek: 1))
        XCTAssertEqual(layout.firstDayColumn, 4)
        XCTAssertEqual(layout.weeks.count, 5)
        XCTAssertEqual(layout.weeks[4].days[6], CalendarDay(year: 2026, month: 10, day: 31))
    }

    func testTheWeekAcrossNewYearIsTheFirstOfTheNextYear() {
        // December 28, 2026 is a Monday; that week holds January 1, 2027, a
        // Friday, so by ISO 8601 it is week 53 of 2026.
        let iso = calendar(firstWeekday: 2, minimumDaysInFirstWeek: 4)
        let december = MonthLayout(month: CalendarMonth(year: 2026, month: 12), calendar: iso)
        let january = MonthLayout(month: CalendarMonth(year: 2027, month: 1), calendar: iso)
        XCTAssertEqual(december.weeks.last?.weekNumber, january.weeks.first?.weekNumber)
        XCTAssertEqual(january.weeks.first?.weekNumber, 53)
        XCTAssertEqual(january.weeks[1].weekNumber, 1)
    }

    func testFebruaryOfALeapYearHasItsTwentyNinthDay() {
        let layout = MonthLayout(month: CalendarMonth(year: 2028, month: 2), calendar: calendar(firstWeekday: 1, minimumDaysInFirstWeek: 1))
        XCTAssertEqual(layout.weeks.flatMap(\.days).compactMap { $0 }.count, 29)
    }

    func testEventsFallOnEveryDayTheyCover() throws {
        let local = calendar(firstWeekday: 2, minimumDaysInFirstWeek: 4, zone: "America/Los_Angeles")
        let events = [
            // A trip of three days, written with its exclusive end.
            try event("trip", "2026-10-18T00:00:00Z", "2026-10-21T00:00:00Z", allDay: true, summary: "Trip"),
            // In the evening in Los Angeles, though the next day in UTC.
            try event("dinner", "2026-10-20T02:00:00Z", "2026-10-20T04:00:00Z", summary: "Dinner"),
            // Ends at midnight: nothing of it is on the next day.
            try event("late", "2026-10-05T05:00:00Z", "2026-10-05T07:00:00Z", summary: "Late"),
            // Started in September, ends on October 2.
            try event("across", "2026-09-29T00:00:00Z", "2026-10-03T00:00:00Z", allDay: true, summary: "Across"),
            // Another month altogether.
            try event("november", "2026-11-03T17:00:00Z", "2026-11-03T18:00:00Z", summary: "Later"),
        ]
        let byDay = CalendarDates.eventsByDay(events, in: CalendarMonth(year: 2026, month: 10), calendar: local)
        func titles(_ day: Int) -> [String] {
            (byDay[CalendarDay(year: 2026, month: 10, day: day)] ?? []).map { $0.summary ?? "" }
        }
        XCTAssertEqual(titles(18), ["Trip"])
        // The all-day event comes before the timed one on the same day.
        XCTAssertEqual(titles(19), ["Trip", "Dinner"])
        XCTAssertEqual(titles(20), ["Trip"])
        XCTAssertEqual(titles(21), [])
        XCTAssertEqual(titles(4), ["Late"])
        XCTAssertEqual(titles(5), [])
        XCTAssertEqual(titles(1), ["Across"])
        XCTAssertEqual(titles(2), ["Across"])
        XCTAssertEqual(titles(3), [])
        XCTAssertTrue(byDay.keys.allSatisfy { $0.month == 10 })
    }

    func testChipsLeaveTheLastLineForWhatDoesNotFit() {
        let none = EventChipSplit(eventCount: 0, lineCount: 3)
        XCTAssertEqual([none.chipCount, none.moreCount], [0, 0])
        let fits = EventChipSplit(eventCount: 3, lineCount: 3)
        XCTAssertEqual([fits.chipCount, fits.moreCount], [3, 0])
        let more = EventChipSplit(eventCount: 5, lineCount: 3)
        XCTAssertEqual([more.chipCount, more.moreCount], [2, 3])
        // At the largest text sizes a cell has two lines: one chip and "+N".
        let large = EventChipSplit(eventCount: 4, lineCount: 2)
        XCTAssertEqual([large.chipCount, large.moreCount], [1, 3])
        let single = EventChipSplit(eventCount: 2, lineCount: 1)
        XCTAssertEqual([single.chipCount, single.moreCount], [0, 2])
    }

    func testCalendarColoursAreRead() {
        XCTAssertEqual(CalendarColorComponents(hexText: "#FF8000"), CalendarColorComponents(hexText: "ff8000"))
        let orange = CalendarColorComponents(hexText: "#ff8000")
        XCTAssertEqual(orange?.red, 1)
        XCTAssertEqual(orange?.green ?? 0, 128.0 / 255, accuracy: 0.0001)
        XCTAssertEqual(orange?.blue, 0)
        XCTAssertEqual(CalendarColorComponents(hexText: "#0f0"), CalendarColorComponents(hexText: "#00ff00"))
        XCTAssertNil(CalendarColorComponents(hexText: nil))
        XCTAssertNil(CalendarColorComponents(hexText: ""))
        XCTAssertNil(CalendarColorComponents(hexText: "blue"))
    }

    func testAPaletteColorStaysWithItsCalendar() {
        let first = CalendarPalette.index(calendarId: "calendar-one", paletteCount: 8)
        XCTAssertEqual(first, CalendarPalette.index(calendarId: "calendar-one", paletteCount: 8))
        XCTAssertTrue((0..<8).contains(first))
        let spread = Set((0..<40).map { CalendarPalette.index(calendarId: "calendar-\($0)", paletteCount: 8) })
        XCTAssertGreaterThan(spread.count, 4)
        XCTAssertEqual(CalendarPalette.index(calendarId: "any", paletteCount: 0), 0)
    }

    func testLunarNames() {
        XCTAssertEqual(LunarDates.name(month: 1, day: 1, isLeapMonth: false), "正月")
        XCTAssertEqual(LunarDates.name(month: 6, day: 1, isLeapMonth: true), "闰六月")
        XCTAssertEqual(LunarDates.name(month: 8, day: 15, isLeapMonth: false), "十五")
        XCTAssertEqual(LunarDates.name(month: 3, day: 20, isLeapMonth: false), "二十")
        XCTAssertEqual(LunarDates.name(month: 3, day: 21, isLeapMonth: false), "廿一")
        XCTAssertEqual(LunarDates.name(month: 3, day: 30, isLeapMonth: false), "三十")
        XCTAssertEqual(LunarDates.name(month: 3, day: 31, isLeapMonth: false), "")
    }

    func testLunarDatesOfKnownDays() {
        let zone = TimeZone(identifier: "Asia/Shanghai")!
        // The Chinese new year of 2026 is February 17.
        XCTAssertEqual(LunarDates.name(of: CalendarDay(year: 2026, month: 2, day: 17), timeZone: zone), "正月")
        XCTAssertEqual(LunarDates.name(of: CalendarDay(year: 2026, month: 2, day: 18), timeZone: zone), "初二")
        // The mid-autumn festival, the fifteenth of the eighth month.
        XCTAssertEqual(LunarDates.name(of: CalendarDay(year: 2026, month: 9, day: 25), timeZone: zone), "十五")
    }
}
