import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

final class FindATimeTests: XCTestCase {
    private var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar
    }

    private func moment(_ day: Int, _ hour: Int, _ minute: Int = 0) -> Date {
        calendar.date(from: DateComponents(year: 2030, month: 1, day: day, hour: hour, minute: minute))!
    }

    private func period(_ day: Int, _ startHour: Int, _ startMinute: Int = 0, to endHour: Int, _ endMinute: Int = 0) -> CalendarFreeBusy.Period {
        .init(startsAt: CalendarDates.text(moment(day, startHour, startMinute)), endsAt: CalendarDates.text(moment(day, endHour, endMinute)))
    }

    /// The command line's free-together test and the dashboard's expect the
    /// same stretches of the same busy times, counted in one zone.
    func testWhatNoneOfThemIsBusyInIsFreeWithOverlapsMadeOne() {
        let free = FreeTime.freeByDay(
            busy: [period(2, 10, to: 11), period(2, 10, 30, to: 12), period(2, 15, to: 16)],
            from: moment(2, 0), dayAfterLast: moment(3, 0), earliestMinutes: 9 * 60, latestMinutes: 17 * 60, calendar: calendar)
        XCTAssertEqual(
            free.map { [calendar.component(.hour, from: $0.startsAt), calendar.component(.hour, from: $0.endsAt)] },
            [[9, 10], [12, 15], [16, 17]])
    }

    func testEachDayIsOfferedOnItsOwnAndABusyDayOffersNothing() {
        let both = FreeTime.freeByDay(
            busy: [], from: moment(2, 14), dayAfterLast: moment(4, 0), earliestMinutes: 9 * 60, latestMinutes: 17 * 60, calendar: calendar)
        XCTAssertEqual(both.map { calendar.component(.day, from: $0.startsAt) }, [2, 3])
        let taken = FreeTime.freeByDay(
            busy: [period(2, 8, to: 18)], from: moment(2, 0), dayAfterLast: moment(3, 0), earliestMinutes: 9 * 60, latestMinutes: 17 * 60,
            calendar: calendar)
        XCTAssertEqual(taken, [])
    }

    func testAQuestionWhoseLastDayIsBeforeItsFirstOrWhoseDayEndsBeforeItBeginsHasAProblem() {
        var question = FreeTimeQuestion(firstDay: moment(5, 0), lastDay: moment(2, 0))
        XCTAssertEqual(question.problem, .lastDayBeforeFirst)
        question = FreeTimeQuestion(firstDay: moment(2, 0), lastDay: moment(5, 0))
        XCTAssertNil(question.problem)
        question.latestMinutes = question.earliestMinutes
        XCTAssertEqual(question.problem, .dayEndsBeforeItBegins)
    }

    private func connection(answering body: @escaping @Sendable (String) -> String) -> (Connection, FakeServer) {
        let fake = FakeServer { request in (200, body(String(decoding: request.httpBody ?? Data(), as: UTF8.self))) }
        let account = Account(
            server: try! ServerAddress(parsing: "mail.example.com"), clientId: "a-client", accessToken: "tnt_a", refreshToken: "",
            expiresAt: Date().addingTimeInterval(30 * 86400), username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    func testOnlyThePeopleWhoSharedAreOfferedAndSomeoneLeftOutIsSaid() async throws {
        let (connection, _) = connection { body in
            if body.contains("ListCirclePeople") {
                return #"{"data":{"ListCirclePeople":[{"userId":"ben-id","username":"ben","name":"Ben Example"},{"userId":"cora-id","username":"cora","name":null}]}}"#
            }
            return #"{"data":{"ListCalendarFreeBusy":[{"userId":"ben-id","username":"ben","name":"Ben Example","isShared":true,"periods":[]},{"userId":"cora-id","username":"","name":"","isShared":false,"periods":[]}]}}"#
        }
        let sharing = try await connection.peopleSharingTheirCalendar()
        XCTAssertEqual(sharing.people.map(\.userId), ["ben-id"])
        XCTAssertTrue(sharing.isAnyoneLeftOut)
    }

    private let ben = try! JSONDecoder().decode(
        CalendarSharePerson.self, from: Data(#"{"userId":"ben-id","username":"ben","name":"Ben Example"}"#.utf8))

    func testThePersonAndThoseChosenAreAskedAboutAndTheirFreeTimeIsOffered() async throws {
        let (connection, fake) = connection { _ in
            #"{"data":{"ListCalendarFreeBusy":[{"userId":"me-id","username":"me","name":"","isShared":true,"periods":[]},{"userId":"ben-id","username":"ben","name":"","isShared":true,"periods":[]}]}}"#
        }
        let question = FreeTimeQuestion(firstDay: moment(2, 0), lastDay: moment(2, 0))
        let answer = try await connection.freeTime(ownUserId: "me-id", with: [ben], question: question, calendar: calendar)
        guard case .free(let stretches) = answer else { return XCTFail("\(answer)") }
        XCTAssertEqual(stretches.count, 1)
        let sent = String(decoding: fake.sent.last?.httpBody ?? Data(), as: UTF8.self)
        XCTAssertTrue(sent.contains("me-id") && sent.contains("ben-id"), "the person is asked about too: \(sent)")
    }

    func testSomeoneWhoStoppedSharingIsSaidAndNoTimeIsOffered() async throws {
        let (connection, _) = connection { _ in
            #"{"data":{"ListCalendarFreeBusy":[{"userId":"me-id","username":"me","name":"","isShared":true,"periods":[]},{"userId":"ben-id","username":"","name":"","isShared":false,"periods":[]}]}}"#
        }
        let question = FreeTimeQuestion(firstDay: moment(2, 0), lastDay: moment(2, 0))
        let answer = try await connection.freeTime(ownUserId: "me-id", with: [ben], question: question, calendar: calendar)
        XCTAssertEqual(answer, .notShared(["ben"]))
    }
}
