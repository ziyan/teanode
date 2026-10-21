import Foundation
import XCTest

@testable import TeaNodeKit

final class PasswordsSiteMatchTests: XCTestCase {
    func testTheSameHostMatchesWhateverTheSchemeWwwPortOrPath() {
        for site in ["https://example.com", "example.com", "http://example.com/sign-in?next=1", "https://www.example.com", "EXAMPLE.com.",
            "https://example.com:8443/", "https://ada@example.com/", "//example.com", " example.com/path "]
        {
            XCTAssertTrue(PasswordsSiteMatch.isMatch(sites: [site], requested: "example.com"), site)
            XCTAssertTrue(PasswordsSiteMatch.isMatch(sites: [site], requested: "https://www.example.com/login"), site)
        }
    }

    func testAnotherHostDoesNotMatch() {
        let requested = "example.com"
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["https://login.example.com"], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["https://example.com.evil.test"], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["https://evilexample.test"], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["https://example.org"], requested: requested))
        // A user name that looks like the host does not make it the host.
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["https://example.com@evil.test/"], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: [], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["ftp://example.com"], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: [""], requested: requested))
        XCTAssertFalse(PasswordsSiteMatch.isMatch(sites: ["https://example.com"], requested: ""))
    }

    func testRelatedHostsAreBelowTheExactOnesAndLookalikesAreNothing() {
        XCTAssertEqual(PasswordsSiteMatch.level(site: "https://example.com", requested: "login.example.com"), .relatedHost)
        XCTAssertEqual(PasswordsSiteMatch.level(site: "https://login.example.com", requested: "example.com"), .relatedHost)
        XCTAssertEqual(PasswordsSiteMatch.level(site: "https://example.com", requested: "example.com.evil.test"), .none)
        XCTAssertEqual(PasswordsSiteMatch.level(site: "https://www.example.com", requested: "example.com"), .exactHost)
        XCTAssertEqual(PasswordsSiteMatch.level(site: "localhost", requested: "app.localhost"), .none)
        XCTAssertTrue(PasswordsSiteMatch.level(sites: ["https://example.org", "https://login.example.com"], requested: "example.com") == .relatedHost)
    }

    func testHostParsing() {
        XCTAssertEqual(PasswordsSiteMatch.normalizedHost(of: "https://www.Example.COM:8080/a?b#c"), "example.com")
        XCTAssertEqual(PasswordsSiteMatch.normalizedHost(of: "www.test"), "www.test")
        XCTAssertEqual(PasswordsSiteMatch.normalizedHost(of: "http://[::1]:3000/"), "[::1]")
        XCTAssertEqual(PasswordsSiteMatch.normalizedHost(of: "localhost:3000"), "localhost")
        XCTAssertNil(PasswordsSiteMatch.normalizedHost(of: "androidapp://com.example.app"))
        XCTAssertNil(PasswordsSiteMatch.normalizedHost(of: "https://"))
    }

    func testSortingPutsTheExactHostFirstThenRelatedAndKeepsTheOrderWithinALevel() {
        struct Login: Equatable { var name: String; var sites: [String] }
        let logins = [
            Login(name: "Parent", sites: ["https://example.com"]), Login(name: "Other", sites: ["https://example.org"]),
            Login(name: "Exact B", sites: ["https://login.example.com/sign-in"]), Login(name: "Exact A", sites: ["login.example.com"]),
            Login(name: "Lookalike", sites: ["https://login.example.com.evil.test"]),
        ]
        let sorted = PasswordsSiteMatch.sorted(logins, requested: "https://login.example.com/") { $0.sites }
        XCTAssertEqual(sorted.map(\.name), ["Exact B", "Exact A", "Parent"])
        let everything = PasswordsSiteMatch.sorted(logins, requested: "login.example.com", isKeepingOthers: true) { $0.sites }
        XCTAssertEqual(everything.map(\.name), ["Exact B", "Exact A", "Parent", "Other", "Lookalike"])
    }

    // MARK: Identity store records

    func testIdentityRecordsOfALogin() {
        let login = PasswordsLoginFields(
            name: "Example", userName: "ada@example.com", password: "invented", sites: ["https://www.example.com", "example.com/other", "https://shop.example.org"],
            passkeys: [
                PasswordsPasskeyFields(
                    credentialId: "AQID", relyingPartyId: "example.com", userHandle: "BAUG", userName: "ada", userDisplayName: "Ada",
                    privateKey: "key", createdAt: "2026-10-20T10:00:00Z"),
                PasswordsPasskeyFields(credentialId: "not base64!", relyingPartyId: "example.com"),
            ])
        let records = PasswordsIdentityRecords.records(itemId: "item-one", login: login)
        XCTAssertEqual(
            records.passwordIdentities.map(\.serviceDomain), ["example.com", "shop.example.org"])
        XCTAssertEqual(Set(records.passwordIdentities.map(\.recordId)), ["item-one"])
        XCTAssertEqual(records.passwordIdentities.first?.userName, "ada@example.com")
        XCTAssertEqual(records.passkeyIdentities.count, 1)
        XCTAssertEqual(records.passkeyIdentities.first?.credentialId, Data([1, 2, 3]))
        XCTAssertEqual(records.passkeyIdentities.first?.userHandle, Data([4, 5, 6]))
        XCTAssertEqual(records.passkeyIdentities.first?.relyingPartyId, "example.com")
        XCTAssertEqual(records.passkeyIdentities.first?.userName, "ada")
    }

    func testALoginWithoutAPasswordAddsNoPasswordIdentity() {
        let login = PasswordsLoginFields(name: "Example", userName: "ada", sites: ["https://example.com"])
        XCTAssertEqual(PasswordsIdentityRecords.records(itemId: "item-one", login: login).passwordIdentities, [])
    }
}
