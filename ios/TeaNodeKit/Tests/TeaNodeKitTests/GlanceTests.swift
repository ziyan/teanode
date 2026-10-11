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

    func testWhatWaitsIsCountedByConversation() throws {
        let json = #"{"waitingCount":3,"interactions":[{"id":"a","conversationId":"c1","interactionKind":"question","interactionText":"Which day?","createdAt":"2026-03-10T17:00:00Z"},{"id":"b","conversationId":"c2","interactionKind":"approval","interactionText":"Send it?","createdAt":"2026-03-10T16:00:00Z"},{"id":"c","conversationId":"c1","interactionKind":"approval","interactionText":"Delete it?","createdAt":"2026-03-10T15:00:00Z"}]}"#
        let waiting = try JSONDecoder().decode(AgentWaiting.self, from: Data(json.utf8))
        XCTAssertEqual(waiting.countByConversation, ["c1": 2, "c2": 1])
    }

    func testIdeasAreGroupedAsTheDashboardGroupsThem() throws {
        func idea(_ id: String, _ kind: String, _ category: String?, _ status: String, _ at: String, expiredReason: String? = nil) -> String {
            let category = category.map { #""\#($0)""# } ?? "null"
            let reason = expiredReason.map { #""\#($0)""# } ?? "null"
            return #"{"id":"\#(id)","ideaKind":"\#(kind)","ideaCategory":\#(category),"headline":"\#(id)","ideaStatus":"\#(status)","expiredReason":\#(reason),"createdAt":"\#(at)"}"#
        }
        let json = #"{"ideas":["# + [
            idea("mine", "personal", nil, "open", "2026-03-01"),
            idea("plants", "catalog", "home", "open", "2026-03-02"),
            idea("budget", "catalog", "money", "open", "2026-03-03"),
            idea("old", "catalog", "home", "expired", "2026-03-04", expiredReason: "missing_tool"),
            idea("used", "catalog", "home", "expired", "2026-03-05", expiredReason: "already_used"),
        ].joined(separator: ",") + #"],"ideaCategories":[{"ideaCategory":"money","emojis":[]},{"ideaCategory":"home","emojis":[]},{"ideaCategory":"fun","emojis":[]}]}"#
        let list = try JSONDecoder().decode(IdeaList.self, from: Data(json.utf8))
        XCTAssertEqual(list.forYou.map(\.id), ["mine"])
        XCTAssertEqual(list.byCategory.map(\.category), ["money", "home"])
        XCTAssertEqual(list.history.map(\.id), ["used", "old"])
        XCTAssertEqual(list.history.map(\.canRestore), [true, false])
    }
}

