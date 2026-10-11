import Foundation
import XCTest

@testable import TeaNodeKit

final class GlanceTests: XCTestCase {
    private func event(_ id: String, _ summary: String, _ startsAt: String, _ endsAt: String, allDay: Bool = false, status: String? = nil) throws -> CalendarEvent {
        let status = status.map { #","status":"\#($0)""# } ?? ""
        let json = #"{"id":"\#(id)","calendarId":"c","summary":"\#(summary)","startsAt":"\#(startsAt)","endsAt":"\#(endsAt)","allDay":\#(allDay),"recurring":false\#(status)}"#
        return try JSONDecoder().decode(CalendarEvent.self, from: Data(json.utf8))
    }

    func testOnlyEventsNotOverAreShownSoonestFirst() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "America/Los_Angeles")!
        let now = ServerTime.date("2026-03-10T17:00:00Z")!
        let events = [
            try event("over", "Breakfast", "2026-03-10T15:00:00Z", "2026-03-10T16:00:00Z"),
            try event("later", "Dentist", "2026-03-11T18:00:00Z", "2026-03-11T19:00:00Z"),
            try event("now", "Standup", "2026-03-10T16:45:00Z", "2026-03-10T17:15:00Z"),
            try event("off", "Called off", "2026-03-10T20:00:00Z", "2026-03-10T21:00:00Z", status: "CANCELLED"),
            try event("day", "Holiday", "2026-03-11T00:00:00Z", "2026-03-12T00:00:00Z", allDay: true),
        ]
        let upcoming = Glance.upcoming(events, now: now, limit: 3, calendar: calendar)
        XCTAssertEqual(upcoming.map(\.title), ["Standup", "Holiday", "Dentist"])
        // The all-day event starts at local midnight of its own day, not the
        // evening before, as midnight UTC is west of Greenwich.
        let holiday = upcoming[1]
        XCTAssertTrue(holiday.isAllDay)
        XCTAssertEqual(calendar.dateComponents([.month, .day, .hour], from: holiday.startsAt), DateComponents(month: 3, day: 11, hour: 0))
    }
}
