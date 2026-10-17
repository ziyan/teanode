import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// CirclesTests cover the circles screens: what the server's answers decode
/// to, what each change sends, and what the forms work out.
final class CirclesTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private let circleAnswer = #"""
        {"id":"circle-1","name":"Fixture Home","circleKind":"family","createdAt":"2030-01-01T00:00:00Z","memberRole":"admin",
         "members":[
           {"userId":"user-1","username":"person-a","name":"Person A","avatarUrl":null,"avatarColorIndex":4,"memberRole":"admin","joinedAt":"2030-01-01T00:00:00Z"},
           {"userId":"user-2","username":"person-b","name":null,"avatarUrl":"/api/v1/avatar/user/user-2?v=2","avatarColorIndex":null,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z"}],
         "pendingInvitations":[
           {"id":"invitation-1","circleId":"circle-1","circleName":"Fixture Home","circleKind":"family","invitedByName":"Person A",
            "invitedUsername":"person-c","memberRole":"admin","createdAt":"2030-01-03T00:00:00Z","expiresAt":"2030-01-10T00:00:00Z"}]}
        """#

    func testReadsTheCirclesWithTheirMembersAndPendingInvitations() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ListCircles":[\#(circleAnswer)]}}"#)
        let circles = try await connection.circles()
        XCTAssertEqual(circles.count, 1)
        let circle = try XCTUnwrap(circles.first)
        XCTAssertTrue(circle.isAdmin)
        XCTAssertEqual(circle.knownCircleKind, .family)
        XCTAssertEqual(circle.members.map(\.displayName), ["Person A", "person-b"])
        XCTAssertTrue(circle.member("user-1")?.isAdmin ?? false)
        XCTAssertFalse(circle.member("user-2")?.isAdmin ?? true)
        XCTAssertNil(circle.member("user-2")?.avatarColorIndex)
        XCTAssertEqual(circle.pendingInvitations.first?.invitedUsername, "person-c")
        XCTAssertTrue(circle.pendingInvitations.first?.isAdminInvitation ?? false)
        XCTAssertEqual(fake.sent.count, 1)
    }

    func testAKindTheAppDoesNotKnowIsKeptButNotNamed() async throws {
        let unknownKind = circleAnswer.replacingOccurrences(of: #""circleKind":"family""#, with: #""circleKind":"neighbors""#)
        let (connection, _) = connection(answering: #"{"data":{"ReadCircle":\#(unknownKind)}}"#)
        let circle = try await connection.circle("circle-1")
        XCTAssertEqual(circle.circleKind, "neighbors")
        XCTAssertNil(circle.knownCircleKind)
    }

    func testReadsTheInvitationsWaitingForTheCaller() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListCircleInvitations":[
                  {"id":"invitation-2","circleId":"circle-2","circleName":"Fixture Team","circleKind":"colleagues","invitedByName":"Person B",
                   "invitedUsername":"robin","memberRole":"member","createdAt":"2030-01-03T00:00:00Z","expiresAt":"2030-01-10T00:00:00.123456789Z"}]}}
                """#)
        let invitations = try await connection.circleInvitations()
        XCTAssertEqual(invitations.first?.circleName, "Fixture Team")
        XCTAssertEqual(invitations.first?.knownCircleKind, .colleagues)
        XCTAssertFalse(invitations.first?.isAdminInvitation ?? true)
        XCTAssertNotNil(ServerTime.date(invitations.first?.expiresAt))
    }

    func testMakesAndChangesACircleWithTheNameTrimmed() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"CreateCircle":{"id":"circle-9"},"RenameCircle":{"id":"circle-9"},"DeleteCircle":true}}"#)
        var draft = CircleDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.name = "  Fixture Team  "
        draft.circleKind = .colleagues
        XCTAssertTrue(draft.canSubmit)

        let createdId = try await connection.createCircle(draft)
        XCTAssertEqual(createdId, "circle-9")
        try await connection.renameCircle("circle-9", name: " Fixture Crew ")
        try await connection.deleteCircle("circle-9")

        XCTAssertEqual(try sentVariables(fake.sent[0]), ["name": .string("Fixture Team"), "circleKind": .string("colleagues")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["circleId": .string("circle-9"), "name": .string("Fixture Crew")])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["circleId": .string("circle-9")])
    }

    func testAFormStartsFromTheCircleItEdits() throws {
        let circle = try JSONDecoder().decode(PeopleCircle.self, from: Data(circleAnswer.utf8))
        let draft = CircleDraft(circle)
        XCTAssertEqual(draft.name, "Fixture Home")
        XCTAssertEqual(draft.circleKind, .family)
    }

    func testInvitesByUsernameWithTheRoleTheBoxSays() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"InviteToCircle":true}}"#)
        var draft = CircleInvitationDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.username = "  person-d "
        XCTAssertTrue(draft.canSubmit)
        try await connection.inviteToCircle("circle-1", draft: draft)
        draft.isAdmin = true
        try await connection.inviteToCircle("circle-1", draft: draft)

        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["circleId": .string("circle-1"), "username": .string("person-d"), "memberRole": .string("member")])
        XCTAssertEqual(
            try sentVariables(fake.sent[1]),
            ["circleId": .string("circle-1"), "username": .string("person-d"), "memberRole": .string("admin")])
    }

    func testMakesAnAccountAndPutsItInTheCircleWithNoInvitation() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"CreateUser":{"id":"user-9","username":"person-d","hasPassword":true,"groupIds":[],"createdAt":"2030-01-01T00:00:00Z"},"AddCircleMember":{"id":"circle-1"}}}"#)
        var draft = CircleAccountDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.username = "  person-d "
        XCTAssertFalse(draft.canSubmit, "a username alone cannot sign in")
        draft.password = "fixture-password"
        draft.name = " Person D "
        XCTAssertTrue(draft.canSubmit)

        _ = try await connection.createAccessUser(draft.accountDraft)
        try await connection.addCircleMember("circle-1", username: draft.trimmedUsername, memberRole: draft.memberRole)
        draft.isAdmin = true
        try await connection.addCircleMember("circle-1", username: draft.trimmedUsername, memberRole: draft.memberRole)

        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            [
                "username": .string("person-d"), "password": .string("fixture-password"), "name": .string("Person D"),
                "email": .string(""),
            ])
        XCTAssertEqual(
            try sentVariables(fake.sent[1]),
            ["circleId": .string("circle-1"), "username": .string("person-d"), "memberRole": .string("member")])
        XCTAssertEqual(
            try sentVariables(fake.sent[2]),
            ["circleId": .string("circle-1"), "username": .string("person-d"), "memberRole": .string("admin")])
        XCTAssertFalse(fake.sent.contains { String(decoding: $0.httpBody ?? Data(), as: UTF8.self).contains("InviteToCircle") })
    }

    func testAnswersAndWithdrawsInvitations() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"AnswerCircleInvitation":true,"WithdrawCircleInvitation":true}}"#)
        try await connection.answerCircleInvitation("invitation-2", isAccepted: true)
        try await connection.answerCircleInvitation("invitation-2", isAccepted: false)
        try await connection.withdrawCircleInvitation("invitation-1")

        XCTAssertEqual(try sentVariables(fake.sent[0]), ["invitationId": .string("invitation-2"), "isAccepted": .bool(true)])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["invitationId": .string("invitation-2"), "isAccepted": .bool(false)])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["invitationId": .string("invitation-1")])
    }

    func testChangesRolesRemovesAndLeaves() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"SetCircleMemberRole":true,"RemoveCircleMember":true,"LeaveCircle":true}}"#)
        try await connection.setCircleMemberRole("circle-1", userId: "user-2", memberRole: .admin)
        try await connection.removeCircleMember("circle-1", userId: "user-2")
        try await connection.leaveCircle("circle-1")

        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["circleId": .string("circle-1"), "userId": .string("user-2"), "memberRole": .string("admin")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["circleId": .string("circle-1"), "userId": .string("user-2")])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["circleId": .string("circle-1")])
    }

    func testARefusalIsSaidInTheServersWords() async throws {
        let (connection, _) = connection(answering: #"{"errors":[{"message":"api: not found"}],"data":null}"#)
        do {
            try await connection.leaveCircle("circle-gone")
            XCTFail("a refused change should throw")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("not found"))
        }
    }

    private func invitation(circleKind: String) -> CircleInvitation {
        CircleInvitation(
            id: "invitation-1", circleId: "circle-1", circleName: "Fixture Home", circleKind: circleKind, invitedByName: "Person A",
            invitedUsername: "person-b", memberRole: "member", createdAt: "2030-01-01T00:00:00Z", expiresAt: "2030-01-31T00:00:00Z")
    }

    func testJoiningAFamilyOrColleaguesOffersToShareTheCalendarButJoiningFriendsDoesNot() {
        for circleKind in ["family", "colleagues", "neighbors"] {
            let offer = CircleCalendarOffer(accepting: invitation(circleKind: circleKind), calendarId: "calendar-1")
            XCTAssertEqual(offer?.circleId, "circle-1", circleKind)
            XCTAssertEqual(offer?.circleName, "Fixture Home", circleKind)
            XCTAssertEqual(offer?.calendarId, "calendar-1", circleKind)
        }
        XCTAssertNil(CircleCalendarOffer(accepting: invitation(circleKind: "friends"), calendarId: "calendar-1"))
        // Edit is chosen on the calendar screen, not offered in passing.
        XCTAssertEqual(CircleCalendarOffer.levels, [.busy, .titles, .details])
    }

    func testTheCalendarOfferedIsThePersonsOwnOfEventsAndNotARemindersListOrSomeoneElses() {
        let reminders = AgentCalendar(id: "reminders", name: "Reminders", calendarKind: "reminders", shareLevel: "owner")
        let theirs = AgentCalendar(id: "theirs", name: "Calendar", calendarKind: "events", shareLevel: "titles")
        let own = AgentCalendar(id: "own", name: "Calendar", calendarKind: "events", shareLevel: "owner")
        XCTAssertEqual(CircleCalendarOffer.ownCalendar(among: [reminders, theirs, own])?.id, "own")
        XCTAssertNil(CircleCalendarOffer.ownCalendar(among: [reminders, theirs]))
    }
}
