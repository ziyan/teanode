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

    // MARK: Origin

    func testAnOriginNeedsTheSamePortAndAnHttpPageNeedsAnHttpSite() {
        XCTAssertTrue(PasswordsSiteMatch.isOrigin(sites: ["example.com"], requested: "https://example.com/sign-in"))
        XCTAssertTrue(PasswordsSiteMatch.isOrigin(sites: ["example.com"], requested: "https://example.com:443/"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: ["example.com"], requested: "https://example.com:8443/"))
        XCTAssertTrue(PasswordsSiteMatch.isOrigin(sites: ["https://example.com:8443"], requested: "https://example.com:8443/x"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: ["https://example.com:8443"], requested: "https://example.com/"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: ["https://example.com"], requested: "http://example.com/"))
        XCTAssertTrue(PasswordsSiteMatch.isOrigin(sites: ["http://example.com"], requested: "http://example.com/"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: ["http://example.com"], requested: "https://example.com/"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: ["https://example.com:0"], requested: "https://example.com/"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: [], requested: "https://example.com/"))
        XCTAssertFalse(PasswordsSiteMatch.isOrigin(sites: ["example.com"], requested: "about:blank"))
    }

    func testAFullAddressDemotesTheSameHostOnAnotherPortOrSchemeButABareDomainDoesNot() {
        let site = "https://example.com"
        XCTAssertEqual(PasswordsSiteMatch.level(site: site, requested: "https://example.com/", isPageAddress: true), .exactHost)
        XCTAssertEqual(PasswordsSiteMatch.level(site: site, requested: "https://example.com:8443/", isPageAddress: true), .none)
        XCTAssertEqual(PasswordsSiteMatch.level(site: site, requested: "http://example.com/", isPageAddress: true), .none)
        // A domain identifier carries a host and nothing else, so only the host is compared.
        XCTAssertEqual(PasswordsSiteMatch.level(site: site, requested: "example.com", isPageAddress: false), .exactHost)
        // A related host stays related.
        XCTAssertEqual(PasswordsSiteMatch.level(site: site, requested: "https://login.example.com/", isPageAddress: true), .relatedHost)
        let logins = [("Other port", ["https://example.com:8443"]), ("Exact", ["example.com"]), ("Parent", ["example.org", "https://example.com"])]
        let sorted = PasswordsSiteMatch.sorted(logins, requested: "https://example.com/", isPageAddress: true) { $0.1 }
        XCTAssertEqual(sorted.map(\.0), ["Exact", "Parent"])
    }

    // MARK: The cases shared with the Go package and the Chrome extension

    private struct SharedCases: Decodable {
        struct HostCase: Decodable {
            let address: String
            let expectedHost: String
            private enum CodingKeys: String, CodingKey {
                case address = "site"
                case expectedHost
            }
        }
        struct OriginCase: Decodable {
            let name: String
            let sites: [String]
            let page: String
            let isMatch: Bool
        }
        let normalizedHostCases: [HostCase]
        let originCases: [OriginCase]
    }

    /// internal/passwords/testdata/sitematch.json, which the Go package and the
    /// extension read too: the three must answer every case the same way.
    func testTheSharedSiteMatchCases() throws {
        // This file is ios/TeaNodeKit/Tests/TeaNodeKitTests/PasswordsSiteMatchTests.swift.
        var url = URL(fileURLWithPath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        url.appendPathComponent("internal/passwords/testdata/sitematch.json")
        guard FileManager.default.fileExists(atPath: url.path) else {
            throw XCTSkip(
                "sitematch.json is not at \(url.path); run the tests with the whole repository mounted (Docker: -v <repo>:/repo -w /repo/ios/TeaNodeKit)"
            )
        }
        let cases = try JSONDecoder().decode(SharedCases.self, from: Data(contentsOf: url))
        XCTAssertFalse(cases.normalizedHostCases.isEmpty)
        XCTAssertFalse(cases.originCases.isEmpty)
        for each in cases.normalizedHostCases {
            XCTAssertEqual(PasswordsSiteMatch.normalizedHost(of: each.address) ?? "", each.expectedHost, "normalizedHost(of: \(each.address.debugDescription))")
        }
        for each in cases.originCases {
            XCTAssertEqual(PasswordsSiteMatch.isOrigin(sites: each.sites, requested: each.page), each.isMatch, each.name)
        }
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
        XCTAssertEqual(Set(records.passwordIdentities.map(\.itemId)), ["item-one"])
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
