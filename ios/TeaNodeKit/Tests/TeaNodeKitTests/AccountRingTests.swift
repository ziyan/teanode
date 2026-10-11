import Foundation
import XCTest

@testable import TeaNodeKit

final class AccountRingTests: XCTestCase {
    private func account(_ host: String, _ username: String, token: String = "tnt_a") throws -> Account {
        Account(server: try ServerAddress(parsing: host), clientId: "client-\(host)-\(username)", accessToken: token, refreshToken: "",
            expiresAt: Date().addingTimeInterval(86400), username: username)
    }

    func testTheFirstKeptIsInUseAndAddingAnotherKeepsIt() throws {
        var ring = AccountRing()
        ring.put(try account("mail.example.com", "robin"))
        ring.put(try account("mail.example.org", "sam"))
        XCTAssertEqual(ring.active?.username, "robin")
        XCTAssertEqual(ring.accounts.count, 2)
    }

    func testSigningInAgainReplacesTheSameSignIn() throws {
        var ring = AccountRing()
        ring.put(try account("mail.example.com", "robin", token: "tnt_old"))
        ring.put(try account("mail.example.com", "Robin", token: "tnt_new"))
        XCTAssertEqual(ring.accounts.count, 1)
        XCTAssertEqual(ring.active?.accessToken, "tnt_new")
    }

    func testSwitchingAndForgettingMoveTheOneInUse() throws {
        var ring = AccountRing()
        let first = try account("mail.example.com", "robin")
        let second = try account("mail.example.org", "sam")
        ring.put(first)
        ring.put(second)
        ring.activate(second.accountKey)
        XCTAssertEqual(ring.active?.username, "sam")
        ring.activate("nobody")
        XCTAssertEqual(ring.active?.username, "sam")
        ring.remove(second.accountKey)
        XCTAssertEqual(ring.active?.username, "robin")
        ring.remove(first.accountKey)
        XCTAssertNil(ring.active)
    }

    func testTheSamePersonOnTwoServersIsTwoSignIns() throws {
        var ring = AccountRing()
        ring.put(try account("mail.example.com", "robin"))
        ring.put(try account("mail.example.org", "robin"))
        XCTAssertEqual(ring.accounts.count, 2)
    }
}
