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
            let offer = CircleCalendarOffer(accepting: invitation(circleKind: circleKind), calendarId: "calendar-1", canShareLocation: true)
            XCTAssertEqual(offer?.circleId, "circle-1", circleKind)
            XCTAssertEqual(offer?.circleName, "Fixture Home", circleKind)
            XCTAssertEqual(offer?.calendarId, "calendar-1", circleKind)
        }
        XCTAssertNil(CircleCalendarOffer(accepting: invitation(circleKind: "friends"), calendarId: "calendar-1", canShareLocation: true))
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

    func testJoiningAFamilyAlsoOffersLocationButColleaguesDoNot() {
        XCTAssertEqual(CircleCalendarOffer(accepting: invitation(circleKind: "family"), calendarId: "calendar-1", canShareLocation: true)?.isLocationOffered, true)
        XCTAssertEqual(CircleCalendarOffer(accepting: invitation(circleKind: "colleagues"), calendarId: "calendar-1", canShareLocation: true)?.isLocationOffered, false)
        XCTAssertEqual(CircleCalendarOffer.locationLevels, [.history, .current])
    }

    func testLocationIsOfferedOnlyToAPersonWhoMayUseIt() {
        let family = invitation(circleKind: "family")
        let withCalendar = CircleCalendarOffer(accepting: family, calendarId: "calendar-1", canShareLocation: false)
        XCTAssertEqual(withCalendar?.isLocationOffered, false)
        XCTAssertEqual(withCalendar?.isCalendarOffered, true)
        XCTAssertNil(CircleCalendarOffer(accepting: family, calendarId: nil, canShareLocation: false))
    }

    func testAFamilyIsOfferedLocationEvenWithoutACalendarButColleaguesAreOfferedNothing() {
        let family = CircleCalendarOffer(accepting: invitation(circleKind: "family"), calendarId: nil, canShareLocation: true)
        XCTAssertEqual(family?.isCalendarOffered, false)
        XCTAssertEqual(family?.isLocationOffered, true)
        XCTAssertNil(CircleCalendarOffer(accepting: invitation(circleKind: "colleagues"), calendarId: nil, canShareLocation: true))
        XCTAssertNil(CircleCalendarOffer(accepting: invitation(circleKind: "friends"), calendarId: nil, canShareLocation: true))
    }

    // MARK: Relationships

    private let termsAnswer = #"""
        {"data":{"ListRelationshipTerms":[
          {"term":"parent","relationshipKind":"parent","termGender":"neutral","word":"parent"},
          {"term":"father","relationshipKind":"parent","termGender":"masculine","word":"father"},
          {"term":"mother","relationshipKind":"parent","termGender":"feminine","word":"mother"},
          {"term":"child","relationshipKind":"child","termGender":"neutral","word":"child"},
          {"term":"cousin","relationshipKind":"cousin","termGender":"neutral","word":"cousin"},
          {"term":"friend","relationshipKind":"friend","termGender":"neutral","word":"friend"}]}}
        """#

    private func related(kind: String, termGender: String, word: String) throws -> CircleMember {
        let json = """
            {"userId":"user-2","username":"person-b","name":"Person B","avatarUrl":null,"avatarColorIndex":3,"memberRole":"member",
             "joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"\(kind)","relationshipTermGender":"\(termGender)","relationshipWord":"\(word)"}
            """
        return try JSONDecoder().decode(CircleMember.self, from: Data(json.utf8))
    }

    func testMembersCarryWhatTheyAreToTheCallerAndNothingWhenNeitherHasSaid() async throws {
        let withRelationship = circleAnswer.replacingOccurrences(
            of: #""memberRole":"member","joinedAt":"2030-01-02T00:00:00Z""#,
            with: #""memberRole":"member","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"parent","relationshipTermGender":"masculine","relationshipWord":"father""#)
        let (connection, fake) = connection(answering: #"{"data":{"ReadCircle":\#(withRelationship)}}"#)
        let circle = try await connection.circle("circle-1")

        let father = try XCTUnwrap(circle.member("user-2"))
        XCTAssertTrue(father.hasRelationship)
        XCTAssertEqual(father.knownRelationshipKind, .parent)
        XCTAssertEqual(father.knownRelationshipTermGender, .masculine)
        XCTAssertEqual(father.relationshipWord, "father")

        // The caller's own row, and a server that predates the fields, say nothing.
        let own = try XCTUnwrap(circle.member("user-1"))
        XCTAssertFalse(own.hasRelationship)
        XCTAssertEqual(own.relationshipKind, "")
        XCTAssertEqual(own.relationshipTermGender, "")
        XCTAssertEqual(own.relationshipWord, "")
        XCTAssertNil(own.knownRelationshipKind)
        XCTAssertEqual(own.knownRelationshipTermGender, .neutral)

        let sent = String(decoding: try XCTUnwrap(fake.sent.first?.httpBody), as: UTF8.self)
        XCTAssertTrue(sent.contains("relationshipKind relationshipTermGender relationshipWord"))
    }

    func testAKindTheAppDoesNotKnowIsKeptWithItsEnglishWord() throws {
        let member = try related(kind: "godparent", termGender: "neutral", word: "godparent")
        XCTAssertTrue(member.hasRelationship)
        XCTAssertNil(member.knownRelationshipKind)
        XCTAssertEqual(member.relationshipWord, "godparent")
    }

    func testEveryKindHasAnOppositeThatPointsBack() {
        for kind in RelationshipKind.allCases {
            XCTAssertEqual(kind.opposite.opposite, kind, kind.rawValue)
        }
        XCTAssertEqual(RelationshipKind.parent.opposite, .child)
        XCTAssertEqual(RelationshipKind.auntOrUncle.opposite, .nieceOrNephew)
        XCTAssertEqual(RelationshipKind.parentInLaw.opposite, .childInLaw)
        XCTAssertEqual(RelationshipKind.stepParent.opposite, .stepChild)
        XCTAssertEqual(RelationshipKind.spouse.opposite, .spouse)
        XCTAssertEqual(RelationshipKind.allCases.count, 17)
    }

    func testOnlyACousinAFriendAndAColleagueHaveNoGenderedWords() {
        let plain = RelationshipKind.allCases.filter { !$0.hasGenderedWords }
        XCTAssertEqual(Set(plain), [.cousin, .friend, .colleague])
    }

    func testAPronounGendersTheWordsForThePerson() {
        XCTAssertEqual(PersonPronoun.he.termGender, .masculine)
        XCTAssertEqual(PersonPronoun.she.termGender, .feminine)
        XCTAssertEqual(PersonPronoun.they.termGender, .neutral)
        XCTAssertEqual(PersonPronoun.notSet.termGender, .neutral)
        XCTAssertEqual(PersonPronoun.notSet.rawValue, "")
        XCTAssertEqual(PersonPronoun.choices, [.he, .she, .they])
    }

    func testReadsTheTermsInTheOrderTheServerOffersThem() async throws {
        let (connection, fake) = connection(answering: termsAnswer)
        let terms = try await connection.relationshipTerms()
        XCTAssertEqual(terms.map(\.term), ["parent", "father", "mother", "child", "cousin", "friend"])
        XCTAssertEqual(terms[1].knownRelationshipKind, .parent)
        XCTAssertEqual(terms[1].knownTermGender, .masculine)
        XCTAssertEqual(terms[1].id, "father")
        XCTAssertEqual(fake.sent.count, 1)
    }

    func testSettingARelationshipSendsTheTermAndAnswersTheMemberAsTheCallerNowSeesThem() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"SetPersonRelationship":{"userId":"user-2","username":"person-b","name":"Person B","avatarUrl":null,
                 "avatarColorIndex":3,"memberRole":"","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"parent",
                 "relationshipTermGender":"feminine","relationshipWord":"mother"}}}
                """#)
        let member = try await connection.setPersonRelationship("user-2", term: "mother")
        XCTAssertEqual(member.relationshipWord, "mother")
        XCTAssertEqual(member.knownRelationshipKind, .parent)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-2"), "relationshipTerm": .string("mother")])
    }

    func testRemovingARelationshipSaysWhetherThereWasOne() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"RemovePersonRelationship":true}}"#)
        let wasThere = try await connection.removePersonRelationship("user-2")
        XCTAssertTrue(wasThere)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-2")])

        let (emptyConnection, _) = self.connection(answering: #"{"data":{"RemovePersonRelationship":false}}"#)
        let wasNone = try await emptyConnection.removePersonRelationship("user-2")
        XCTAssertFalse(wasNone)
    }

    func testTheSheetStartsOnTheTermTheMemberHasNow() async throws {
        let (connection, _) = connection(answering: termsAnswer)
        let terms = try await connection.relationshipTerms()

        let father = try related(kind: "parent", termGender: "masculine", word: "father")
        XCTAssertEqual(PersonRelationshipDraft(member: father, terms: terms, ownPronoun: .notSet).selectedTerm, "father")
        // The word follows how the member is described, which may have no
        // word of that gender: the neutral one stands in.
        let cousin = try related(kind: "cousin", termGender: "feminine", word: "cousin")
        XCTAssertEqual(PersonRelationshipDraft(member: cousin, terms: terms, ownPronoun: .notSet).selectedTerm, "cousin")
        let none = try XCTUnwrap(try JSONDecoder().decode(PeopleCircle.self, from: Data(circleAnswer.utf8)).member("user-2"))
        let untouched = PersonRelationshipDraft(member: none, terms: terms, ownPronoun: .notSet)
        XCTAssertEqual(untouched.selectedTerm, "")
        XCTAssertFalse(untouched.isRemoving, "there is nothing to remove")
        XCTAssertFalse(untouched.canSave)
    }

    func testChoosingATermNamesWhatTheOtherPersonWillSeeAndAsksHowThePersonIsDescribed() async throws {
        let (connection, _) = connection(answering: termsAnswer)
        let terms = try await connection.relationshipTerms()
        let none = try XCTUnwrap(try JSONDecoder().decode(PeopleCircle.self, from: Data(circleAnswer.utf8)).member("user-2"))

        var draft = PersonRelationshipDraft(member: none, terms: terms, ownPronoun: .notSet)
        draft.selectedTerm = "mother"
        XCTAssertEqual(draft.selectedKind, .parent)
        XCTAssertEqual(draft.oppositeKind, .child)
        XCTAssertTrue(draft.isPronounAsked)
        XCTAssertNil(draft.pronounToSet, "nothing is set until one is chosen")
        XCTAssertTrue(draft.canSave)
        draft.pronoun = .she
        XCTAssertEqual(draft.pronounToSet, .she)
        XCTAssertEqual(draft.oppositeTermGender, .feminine)

        // A friend is the same word whoever they are: nothing to ask.
        draft.selectedTerm = "friend"
        XCTAssertFalse(draft.isPronounAsked)
        XCTAssertNil(draft.pronounToSet)

        // Somebody who has said already is not asked again.
        var known = PersonRelationshipDraft(member: none, terms: terms, ownPronoun: .he)
        known.selectedTerm = "father"
        XCTAssertFalse(known.isPronounAsked)
        XCTAssertEqual(known.oppositeTermGender, .masculine)
    }

    func testSavingTheSheetSetsThePronounFirstThenTheRelationship() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"UpdateUser":{"id":"user-1","username":"robin","pronoun":"she"},
                 "SetPersonRelationship":{"userId":"user-2","username":"person-b","memberRole":"","joinedAt":"2030-01-02T00:00:00Z",
                 "relationshipKind":"parent","relationshipTermGender":"feminine","relationshipWord":"mother"}}}
                """#)
        let terms = [RelationshipTerm(term: "mother", relationshipKind: "parent", termGender: "feminine", word: "mother")]
        let none = try XCTUnwrap(try JSONDecoder().decode(PeopleCircle.self, from: Data(circleAnswer.utf8)).member("user-2"))
        var draft = PersonRelationshipDraft(member: none, terms: terms, ownPronoun: .notSet)
        draft.selectedTerm = "mother"
        draft.pronoun = .she

        try await connection.saveRelationship(draft, ownUserId: "user-1")

        XCTAssertEqual(fake.sent.count, 2)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-1"), "pronoun": .string("she")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["userId": .string("user-2"), "relationshipTerm": .string("mother")])
    }

    func testAnsweringOnlyThePronounKeepsTheWordThePersonChose() async throws {
        // The member is shown as "mother" because of how they are described;
        // the person may have said "parent", which saving the shown term again
        // would undo.
        let (connection, fake) = connection(answering: #"{"data":{"UpdateUser":{"id":"user-1","username":"robin","pronoun":"he"}}}"#)
        let terms = [
            RelationshipTerm(term: "parent", relationshipKind: "parent", termGender: "neutral", word: "parent"),
            RelationshipTerm(term: "mother", relationshipKind: "parent", termGender: "feminine", word: "mother"),
        ]
        let mother = try related(kind: "parent", termGender: "feminine", word: "mother")
        var draft = PersonRelationshipDraft(member: mother, terms: terms, ownPronoun: .notSet)
        XCTAssertTrue(draft.isPronounAsked)
        draft.pronoun = .he
        XCTAssertTrue(draft.canSave)
        XCTAssertFalse(draft.isTermChanged)
        XCTAssertFalse(draft.isRemoving)

        try await connection.saveRelationship(draft, ownUserId: "user-1")

        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-1"), "pronoun": .string("he")])
    }

    func testSavingNoneRemovesTheRelationshipAndLeavesThePronounAlone() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"RemovePersonRelationship":true}}"#)
        let terms = [RelationshipTerm(term: "mother", relationshipKind: "parent", termGender: "feminine", word: "mother")]
        let mother = try related(kind: "parent", termGender: "feminine", word: "mother")
        var draft = PersonRelationshipDraft(member: mother, terms: terms, ownPronoun: .notSet)
        XCTAssertFalse(draft.canSave)
        draft.selectedTerm = ""
        draft.pronoun = .he
        XCTAssertTrue(draft.canSave)

        try await connection.saveRelationship(draft, ownUserId: "user-1")

        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-2")])
    }
}
