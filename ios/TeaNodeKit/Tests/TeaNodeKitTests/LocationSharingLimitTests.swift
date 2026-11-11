import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// LocationSharingLimitTests cover what limits the people the person sees:
/// what the server's limit decodes to, which banner applies, and who is
/// reduced by what the person shares back.
final class LocationSharingLimitTests: XCTestCase {
    private func person(_ userId: String, shareLevel: String, grantedShareLevel: String?) -> PersonLocation {
        PersonLocation(userId: userId, username: userId, name: userId.capitalized, shareLevel: shareLevel, grantedShareLevel: grantedShareLevel)
    }

    private func limit(_ level: String, _ reason: String) -> LocationSharingLimit {
        LocationSharingLimit(shareLevelLimit: level, sharingLimitReason: reason)
    }

    func testTheLimitDecodesWhatTheServerSends() throws {
        let text =
            #"{"shareLevelLimit":"current","sharingLimitReason":"phone_while_using","phonePermission":"while_using","isRecordingOn":true,"reportedAt":null}"#
        let decoded = try JSONDecoder().decode(LocationSharingLimit.self, from: Data(text.utf8))
        XCTAssertEqual(decoded.shareLevelLimit, "current")
        XCTAssertEqual(decoded.sharingLimitReason, "phone_while_using")
        XCTAssertEqual(decoded.phonePermission, "while_using")
        XCTAssertEqual(decoded.isRecordingOn, true)
        XCTAssertNil(decoded.reportedAt)
    }

    func testAPersonDecodesWhatTheyGrantAndNothingShown() throws {
        let text =
            #"{"userId":"sam","username":"sam","name":"Sam","shareLevel":"","grantedShareLevel":"history","latestPoint":null,"openVisit":null,"pause":null}"#
        let decoded = try JSONDecoder().decode(PersonLocation.self, from: Data(text.utf8))
        XCTAssertEqual(decoded.grantedShareLevel, "history")
        XCTAssertTrue(decoded.isHiddenFromCaller)
        XCTAssertNil(decoded.latestPoint)
        XCTAssertNil(decoded.openVisit)
        XCTAssertNil(decoded.pause)
    }

    func testWhileUsingGivesItsOwnBanner() {
        let people = [person("sam", shareLevel: "current", grantedShareLevel: "history")]
        // The limit is why Sam's visits are missing, not what the person shares back.
        XCTAssertEqual(
            LocationSharingNotice.banner(limit: limit("current", "phone_while_using"), people: people), .phoneWhileUsing)
        XCTAssertTrue(LocationSharingNotice.reducedPeople(limit: limit("current", "phone_while_using"), people: people).isEmpty)
    }

    func testPhoneLocationOffRecordingOffAndPausedEachHaveABanner() {
        let people = [person("sam", shareLevel: "", grantedShareLevel: "history")]
        XCTAssertEqual(LocationSharingNotice.banner(limit: limit("", "phone_location_off"), people: people), .phoneLocationOff)
        XCTAssertEqual(LocationSharingNotice.banner(limit: limit("", "recording_off"), people: people), .recordingOff)
        XCTAssertEqual(LocationSharingNotice.banner(limit: limit("", "paused"), people: people), .paused)
    }

    func testNobodyIsReducedBySharingWhenTheLimitIsLower() {
        // Everyone is hidden by the limit; none of it is the person's sharing.
        let people = [person("sam", shareLevel: "", grantedShareLevel: "history"), person("alex", shareLevel: "", grantedShareLevel: "current")]
        XCTAssertTrue(LocationSharingNotice.reducedPeople(limit: limit("", "paused"), people: people).isEmpty)
    }

    func testSharingBackOnlyReducesPeopleWhoseShownLevelIsBelowWhatIsPossible() {
        let people = [
            person("sam", shareLevel: "", grantedShareLevel: "history"),
            person("alex", shareLevel: "current", grantedShareLevel: "history"),
            person("casey", shareLevel: "current", grantedShareLevel: "current"),
            person("morgan", shareLevel: "history", grantedShareLevel: "history"),
        ]
        let reduced = LocationSharingNotice.reducedPeople(limit: limit("history", ""), people: people)
        XCTAssertEqual(reduced.map(\.userId), ["sam", "alex"])
        XCTAssertEqual(reduced.map(\.isSeeingNothing), [true, false])
        XCTAssertEqual(
            LocationSharingNotice.banner(limit: limit("history", ""), people: people), .sharesLess(people: reduced))
    }

    func testALimitAtCurrentOnlyCountsWhatSharingBackCostsBelowIt() {
        // With the limit at current, Alex shown at current is as much as is possible.
        let people = [person("alex", shareLevel: "current", grantedShareLevel: "history")]
        XCTAssertTrue(LocationSharingNotice.reducedPeople(limit: limit("current", ""), people: people).isEmpty)
    }

    func testNoRestrictionGivesNoBanner() {
        let people = [person("sam", shareLevel: "history", grantedShareLevel: "history"), person("alex", shareLevel: "current", grantedShareLevel: "current")]
        XCTAssertNil(LocationSharingNotice.banner(limit: limit("history", ""), people: people))
        XCTAssertNil(LocationSharingNotice.banner(limit: nil, people: people))
        XCTAssertNil(LocationSharingNotice.banner(limit: nil, people: []))
    }

    func testAServerThatDoesNotSayWhatIsGrantedReducesNobody() {
        let people = [person("sam", shareLevel: "", grantedShareLevel: nil)]
        XCTAssertNil(LocationSharingNotice.banner(limit: limit("history", ""), people: people))
    }

    func testReportingThePermissionSendsBothValuesAndReadsTheLimit() async throws {
        let answer =
            #"{"data":{"ReportLocationPermission":{"shareLevelLimit":"current","sharingLimitReason":"phone_while_using","phonePermission":"while_using","isRecordingOn":true,"reportedAt":"2024-01-02T14:00:00Z"}}}"#
        let fake = FakeServer { _ in (200, answer) }
        let account = Account(
            server: try ServerAddress(parsing: "mail.example.com"), clientId: "a-client", accessToken: "tnt_a", refreshToken: "",
            expiresAt: Date().addingTimeInterval(30 * 86400), username: "robin")
        let connection = Connection(account: account, transport: fake)
        let read = try await connection.reportLocationPermission(LocationPermissionReport(phonePermission: .whileUsing, isRecordingOn: true))
        XCTAssertEqual(read.sharingLimitReason, "phone_while_using")
        let request = try XCTUnwrap(fake.sent.last)
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else { return XCTFail("no variables") }
        XCTAssertEqual(variables["phonePermission"], .string("while_using"))
        XCTAssertEqual(variables["isRecordingOn"], .bool(true))
    }
}
