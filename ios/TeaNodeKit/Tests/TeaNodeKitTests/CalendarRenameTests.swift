import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// CalendarRenameTests cover renaming the person's own calendar: what the
/// server's answer decodes to, and what the rename sends, as the dashboard,
/// the command line and the agent's tool send it.
final class CalendarRenameTests: XCTestCase {
    private func connection(answering page: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, page) }
        let account = Account(
            server: try! ServerAddress(parsing: "mail.example.com"), clientId: "a-client", accessToken: "tnt_a", refreshToken: "",
            expiresAt: Date().addingTimeInterval(30 * 86400), username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentBody(_ fake: FakeServer) throws -> [String: JSONValue] {
        try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.first?.httpBody ?? Data())
    }

    func testACalendarKeepsWhatItIsFor() throws {
        let found = try JSONDecoder().decode(
            AgentCalendar.self,
            from: Data(#"{"id":"c","name":"Calendar","description":"Things I do","calendarKind":"events","shareLevel":"owner"}"#.utf8))
        XCTAssertEqual(found.description, "Things I do")
        // A server that leaves it out, as it does for a calendar with none, is nobody's description.
        let plain = try JSONDecoder().decode(AgentCalendar.self, from: Data(#"{"id":"c","name":"Calendar"}"#.utf8))
        XCTAssertNil(plain.description)
        XCTAssertTrue(Documents.calendars.contains("description"))
    }

    func testRenamingSendsEveryOtherFieldBackAsTheCalendarHoldsIt() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SaveCalendar":{"id":"own","name":"Work"}}}"#)
        let own = AgentCalendar(
            id: "own", name: "Calendar", description: "Things I do", colour: "#336699", timezone: "Europe/Berlin", weekStart: "monday",
            calendarKind: "events", shareLevel: "owner")
        try await connection.saveCalendar(own, name: "Work")

        XCTAssertEqual(fake.sent.count, 1)
        guard case .object(let variables) = try sentBody(fake)["variables"] else { return XCTFail("no variables") }
        // The server replaces every field it is sent, so these go back as they were.
        XCTAssertEqual(
            variables,
            [
                "id": .string("own"), "name": .string("Work"), "description": .string("Things I do"), "colour": .string("#336699"),
                "timezone": .string("Europe/Berlin"), "weekStart": .string("monday"),
            ])
        guard case .string(let document) = try sentBody(fake)["query"] else { return XCTFail("no query") }
        XCTAssertTrue(document.contains("SaveCalendar(id: $id"))
    }

    func testACalendarWithNoDescriptionOrColorSendsThemEmpty() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SaveCalendar":{"id":"own","name":"Work"}}}"#)
        try await connection.saveCalendar(AgentCalendar(id: "own", name: "Calendar"), name: "Work")
        guard case .object(let variables) = try sentBody(fake)["variables"] else { return XCTFail("no variables") }
        XCTAssertEqual(variables["description"], .string(""))
        XCTAssertEqual(variables["colour"], .string(""))
    }

    func testRenamingACircleCalendarSendsTheSameDocument() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SaveCalendar":{"id":"family","name":"Household"}}}"#)
        let family = AgentCalendar(
            id: "family", name: "Home plans", description: "Dinners", colour: "#445566", timezone: "Europe/Berlin", weekStart: "monday",
            calendarKind: "events", shareLevel: "edit", circleId: "circle", circleName: "Home", isCircleCalendar: true)
        try await connection.saveCalendar(family, name: "Household")
        guard case .object(let variables) = try sentBody(fake)["variables"] else { return XCTFail("no variables") }
        XCTAssertEqual(variables["id"], .string("family"))
        XCTAssertEqual(variables["name"], .string("Household"))
        XCTAssertEqual(variables["description"], .string("Dinners"))
    }

    func testDeletingACircleCalendarNamesTheCircle() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"DeleteCircleCalendar":true}}"#)
        try await connection.deleteCircleCalendar(circleId: "circle")
        XCTAssertEqual(fake.sent.count, 1)
        guard case .object(let variables) = try sentBody(fake)["variables"] else { return XCTFail("no variables") }
        XCTAssertEqual(variables, ["circleId": .string("circle")])
        guard case .string(let document) = try sentBody(fake)["query"] else { return XCTFail("no query") }
        XCTAssertTrue(document.contains("DeleteCircleCalendar(circleId: $circleId)"))
    }

    func testTheRenameIsInTheSchemaCheck() {
        XCTAssertNotNil(Documents.calendarAndContacts["saveCalendar"])
    }
}
