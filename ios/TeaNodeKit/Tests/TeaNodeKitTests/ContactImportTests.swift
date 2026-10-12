import Foundation
import XCTest

@testable import TeaNodeKit

final class ContactImportTests: XCTestCase {
    private func person(_ name: String, emails: [String] = [], phones: [String] = []) -> ContactIdentity {
        ContactIdentity(name: name, emails: emails, phones: phones)
    }

    func testASharedEmailMatchesWhateverTheCaseAndSpacing() {
        let matcher = ContactMatcher(existing: [person("Ada Example", emails: ["Ada@Example.com"])])
        XCTAssertTrue(matcher.isAlreadyPresent(person("A. Example", emails: [" ada@example.COM "])))
        XCTAssertFalse(matcher.isAlreadyPresent(person("Ada Example", emails: ["ada@example.org"])))
    }

    func testAnyOneOfSeveralEmailsMatches() {
        let matcher = ContactMatcher(existing: [person("Ada Example", emails: ["ada@example.com", "ada@example.org"])])
        XCTAssertTrue(matcher.isAlreadyPresent(person("Ada", emails: ["other@example.net", "ADA@EXAMPLE.ORG"])))
    }

    func testPhonesMatchByDigitsWhateverTheWriting() {
        let matcher = ContactMatcher(existing: [person("Ben Sample", phones: ["(555) 010-1234"])])
        XCTAssertTrue(matcher.isAlreadyPresent(person("Ben", phones: ["555.010.1234"])))
        XCTAssertFalse(matcher.isAlreadyPresent(person("Ben", phones: ["555 010 1235"])))
    }

    func testAPhoneMatchesWithOrWithoutACountryCode() {
        let matcher = ContactMatcher(existing: [person("Ben Sample", phones: ["555 010 1234"])])
        XCTAssertTrue(matcher.isAlreadyPresent(person("Ben", phones: ["+1 555 010 1234"])))
        let withCode = ContactMatcher(existing: [person("Ben Sample", phones: ["+1 555 010 1234"])])
        XCTAssertTrue(withCode.isAlreadyPresent(person("Ben", phones: ["(555) 010-1234"])))
    }

    func testShortNumbersAreComparedWhole() {
        let matcher = ContactMatcher(existing: [person("Front desk", phones: ["0101234"])])
        XCTAssertTrue(matcher.isAlreadyPresent(person("Desk", phones: ["010-1234"])))
        // The last digits alone are not enough when a number is short.
        XCTAssertFalse(matcher.isAlreadyPresent(person("Desk", phones: ["555 010 1234"])))
    }

    func testTextWithoutDigitsIsNotANumber() {
        let matcher = ContactMatcher(existing: [person("Ben Sample", phones: ["none"])])
        XCTAssertFalse(matcher.isAlreadyPresent(person("Cy Sample", phones: ["n/a"])))
    }

    func testTheNameMatchesOnlyWhenNeitherSideHasAddresses() {
        let matcher = ContactMatcher(existing: [person("Cy Sample")])
        XCTAssertTrue(matcher.isAlreadyPresent(person("  cy   SAMPLE ")))
        XCTAssertFalse(matcher.isAlreadyPresent(person("Cy Sample", emails: ["cy@example.com"])))
    }

    func testTheSameNameWithAnotherAddressIsAnotherPerson() {
        let matcher = ContactMatcher(existing: [person("Cy Sample", emails: ["cy@example.com"])])
        XCTAssertFalse(matcher.isAlreadyPresent(person("Cy Sample")))
        XCTAssertFalse(matcher.isAlreadyPresent(person("Cy Sample", emails: ["cy@example.org"])))
    }

    func testNoNameMatchesNoName() {
        let matcher = ContactMatcher(existing: [person("")])
        XCTAssertFalse(matcher.isAlreadyPresent(person("  ")))
    }

    func testASummaryIsReadAsAnIdentity() throws {
        let json = #"{"id":"c1","name":"Dee Sample","emails":["dee@example.com"],"phones":["555 010 9999"]}"#
        let summary = try JSONDecoder().decode(ContactSummary.self, from: Data(json.utf8))
        XCTAssertEqual(summary.identity, person("Dee Sample", emails: ["dee@example.com"], phones: ["555 010 9999"]))
        let bare = try JSONDecoder().decode(ContactSummary.self, from: Data(#"{"id":"c2"}"#.utf8))
        XCTAssertEqual(bare.identity, person(""))
    }

    func testThePhotoIsRemovedFromACardWithTheLinesFoldedUnderIt() {
        let card = [
            "BEGIN:VCARD", "VERSION:3.0", "N:Sample;Ada;;;", "FN:Ada Sample",
            "PHOTO;ENCODING=b;TYPE=JPEG:/9j/4AAQSkZJRgABAQAAAQABAAD",
            " AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
            "\tAAAA", "EMAIL;TYPE=INTERNET:ada@example.com", "END:VCARD", "",
        ].joined(separator: "\r\n")
        let stripped = ContactCard.removingPhoto(from: card)
        XCTAssertEqual(
            stripped,
            ["BEGIN:VCARD", "VERSION:3.0", "N:Sample;Ada;;;", "FN:Ada Sample", "EMAIL;TYPE=INTERNET:ada@example.com", "END:VCARD", ""]
                .joined(separator: "\r\n"))
    }

    func testAGroupedPhotoGoesAndAFoldedNoteStays() {
        let card = [
            "BEGIN:VCARD", "item1.photo:AAAA", "NOTE:Met at the", " fair", "PHOTOGRAPHER:not a photo", "END:VCARD",
        ].joined(separator: "\n")
        XCTAssertEqual(
            ContactCard.removingPhoto(from: card),
            ["BEGIN:VCARD", "NOTE:Met at the", " fair", "PHOTOGRAPHER:not a photo", "END:VCARD"].joined(separator: "\n"))
    }

    func testACardWithoutAPhotoIsUnchanged() {
        let card = "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ada Sample\r\nEND:VCARD\r\n"
        XCTAssertEqual(ContactCard.removingPhoto(from: card), card)
    }

    func testACardTooLargeForTheServerDoesNotFit() {
        XCTAssertTrue(ContactCard.fits("BEGIN:VCARD\r\nEND:VCARD\r\n"))
        XCTAssertFalse(ContactCard.fits(String(repeating: "x", count: ContactCard.maximumByteCount + 1)))
    }

    func testTheImportDocumentSendsTheWholeCard() {
        XCTAssertTrue(Documents.importContact.contains("$card: String!"))
        XCTAssertTrue(Documents.importContact.contains("SaveContact(addressBookId: $addressBookId, card: $card)"))
    }
}
