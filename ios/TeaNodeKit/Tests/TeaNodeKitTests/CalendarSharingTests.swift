import Foundation
import XCTest

@testable import TeaNodeKit

final class CalendarSharingTests: XCTestCase {
    private func calendar(_ json: String) throws -> AgentCalendar {
        try JSONDecoder().decode(AgentCalendar.self, from: Data(json.utf8))
    }

    func testACalendarFromAServerThatKnowsNothingOfSharingIsTheirOwn() throws {
        let found = try calendar(#"{"id":"c","name":"Calendar","calendarKind":"events"}"#)
        XCTAssertTrue(found.isOwn)
        XCTAssertTrue(found.canWrite)
        XCTAssertTrue(found.canRead)
        XCTAssertFalse(found.isNamedForOwner)
    }

    func testACalendarSharedAtTitlesIsReadOnlyAndNamedForItsOwner() throws {
        let found = try calendar(
            #"{"id":"c","name":"Calendar","calendarKind":"events","shareLevel":"titles","ownerUserId":"u","ownerName":"Ben","colorIndex":3}"#)
        XCTAssertFalse(found.isOwn)
        XCTAssertTrue(found.canRead)
        XCTAssertFalse(found.canWrite)
        XCTAssertTrue(found.isNamedForOwner)
        XCTAssertEqual(found.level, .titles)
    }

    func testACalendarSharedAtBusyHasNoEventsToRead() throws {
        let found = try calendar(#"{"id":"c","name":"Calendar","calendarKind":"events","shareLevel":"busy","ownerName":"Cora"}"#)
        XCTAssertFalse(found.canRead)
        XCTAssertFalse(found.canWrite)
    }

    func testACircleCalendarIsWrittenByEveryMemberAndNamedForTheCircle() throws {
        let found = try calendar(
            #"{"id":"c","name":"The household","calendarKind":"events","shareLevel":"edit","circleId":"circle","circleName":"Home","isCircleCalendar":true}"#
        )
        XCTAssertFalse(found.isOwn)
        XCTAssertTrue(found.canWrite)
        XCTAssertFalse(found.isNamedForOwner)
        XCTAssertEqual(found.circleId, "circle")
    }

    func testASharedCalendarSaysWhoseItIsBesideItsEventsAndTheirOwnSaysNothing() throws {
        XCTAssertNil(try calendar(#"{"id":"c","name":"Calendar"}"#).whoseName)
        XCTAssertEqual(try calendar(#"{"id":"c","name":"Calendar","shareLevel":"busy","ownerName":"Ben Example"}"#).whoseName, "Ben Example")
        XCTAssertEqual(
            try calendar(#"{"id":"c","name":"Calendar","shareLevel":"edit","isCircleCalendar":true,"circleName":"The household"}"#).whoseName,
            "The household")
        XCTAssertNil(try calendar(#"{"id":"c","name":"Calendar","shareLevel":"titles","ownerName":""}"#).whoseName)
    }

    func testAPickerNamesAPersonWithTheirUsernameInBrackets() {
        XCTAssertEqual(PersonWords.pickerLabel(name: "Ben Example", username: "ben"), "Ben Example (ben)")
        XCTAssertEqual(PersonWords.pickerLabel(name: "Ben Example", username: "ben2"), "Ben Example (ben2)")
        XCTAssertEqual(PersonWords.pickerLabel(name: nil, username: "cora"), "cora")
        XCTAssertEqual(PersonWords.pickerLabel(name: "  ", username: "cora"), "cora")
        XCTAssertEqual(PersonWords.pickerLabel(name: "cora", username: "cora"), "cora")
        let person = CalendarSharePerson(userId: "u", username: "ben", name: "Ben Example", avatarUrl: nil, avatarColorIndex: nil)
        XCTAssertEqual(person.pickerLabel, "Ben Example (ben)")
    }

    func testAShareTargetIsAPersonOrACircleAndNeverBoth() {
        XCTAssertEqual(ShareTarget.person("u1").granteeUserId, "u1")
        XCTAssertNil(ShareTarget.person("u1").granteeCircleId)
        XCTAssertEqual(ShareTarget.circle("c1").granteeCircleId, "c1")
        XCTAssertNil(ShareTarget.circle("c1").granteeUserId)
    }

    func testOnlyTheFourLevelsCanBeGiven() {
        XCTAssertEqual(CalendarShareLevel.grantable, [.busy, .titles, .details, .edit])
        XCTAssertFalse(CalendarShareLevel.grantable.contains(.owner))
        XCTAssertEqual(CalendarShareLevel.allCases.filter(\.canWrite), [.edit, .owner])
        XCTAssertEqual(CalendarShareLevel.allCases.filter(\.canRead), [.titles, .details, .edit, .owner])
    }

    func testABusyBlockHasNothingToOpenAndNoneAreTheSameOccurrence() throws {
        func event(_ json: String) throws -> CalendarEvent { try JSONDecoder().decode(CalendarEvent.self, from: Data(json.utf8)) }
        let first = try event(
            #"{"id":"","calendarId":"ben","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isBusyOnly":true}"#)
        let second = try event(
            #"{"id":"","calendarId":"cora","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isBusyOnly":true}"#)
        let ordinary = try event(
            #"{"id":"e","calendarId":"ben","summary":"Lunch","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false}"#)
        XCTAssertTrue(first.isBusy)
        XCTAssertFalse(ordinary.isBusy)
        // Two people busy at the same time are two blocks, which a list that
        // is keyed by occurrence has to be able to tell apart.
        XCTAssertNotEqual(first.occurrenceId, second.occurrenceId)
    }

    func testAPrivateEventIsMarkedAndAnOlderServerSaysNothing() throws {
        func event(_ json: String) throws -> CalendarEvent { try JSONDecoder().decode(CalendarEvent.self, from: Data(json.utf8)) }
        let marked = try event(
            #"{"id":"e","calendarId":"own","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isPrivate":true}"#)
        let plain = try event(
            #"{"id":"f","calendarId":"own","summary":"Lunch","startsAt":"2030-01-02T12:00:00Z","endsAt":"2030-01-02T13:00:00Z","allDay":false,"recurring":false,"isPrivate":false}"#)
        let older = try event(
            #"{"id":"g","calendarId":"own","summary":"Walk","startsAt":"2030-01-02T14:00:00Z","endsAt":"2030-01-02T15:00:00Z","allDay":false,"recurring":false}"#)
        XCTAssertTrue(marked.isMarkedPrivate)
        XCTAssertFalse(plain.isMarkedPrivate)
        XCTAssertFalse(older.isMarkedPrivate)

        let detail = try JSONDecoder().decode(
            CalendarEventDetail.self,
            from: Data(
                #"{"id":"e","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isPrivate":true}"#.utf8))
        XCTAssertEqual(detail.isPrivate, true)

        // The documents carry the field both ways.
        XCTAssertTrue(Documents.calendarEvents.contains("isPrivate"))
        XCTAssertTrue(Documents.calendarEvent.contains("isPrivate"))
        XCTAssertTrue(Documents.saveCalendarEvent.contains("isPrivate: $isPrivate"))
    }

    func testAShareNamesWhoItIsWith() throws {
        func share(_ json: String) throws -> CalendarShare { try JSONDecoder().decode(CalendarShare.self, from: Data(json.utf8)) }
        let person = try share(#"{"id":"s1","calendarId":"c","granteeUserId":"u","granteeUsername":"ben","granteeName":"Ben","shareLevel":"titles"}"#)
        XCTAssertEqual(person.granteeDisplayName, "Ben")
        XCTAssertFalse(person.isWithCircle)
        XCTAssertEqual(person.level, .titles)
        let nameless = try share(#"{"id":"s2","calendarId":"c","granteeUserId":"u","granteeUsername":"ben","granteeName":"","shareLevel":"busy"}"#)
        XCTAssertEqual(nameless.granteeDisplayName, "ben")
        let circle = try share(#"{"id":"s3","calendarId":"c","granteeCircleId":"k","granteeCircleName":"Home","shareLevel":"edit"}"#)
        XCTAssertTrue(circle.isWithCircle)
        XCTAssertEqual(circle.granteeDisplayName, "Home")
    }

    func testTheDocumentsAreInTheSchemaCheck() {
        for name in ["calendarShares", "shareCalendar", "unshareCalendar", "calendarSharePeople", "calendarShareCircles", "createCircleCalendar"] {
            XCTAssertNotNil(Documents.calendarAndContacts[name], name)
        }
        XCTAssertTrue(Documents.calendars.contains("shareLevel"))
        XCTAssertTrue(Documents.calendarEvents.contains("isBusyOnly"))
    }
}
