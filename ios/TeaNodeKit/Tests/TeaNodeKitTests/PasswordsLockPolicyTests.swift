import Foundation
import XCTest

@testable import TeaNodeKit

final class PasswordsLockPolicyTests: XCTestCase {
    private let start = Date(timeIntervalSince1970: 1_800_000_000)

    func testNeverUsedAsksAtOnce() {
        XCTAssertTrue(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: nil, now: start))
    }

    func testAsksOnceTheLockTimeHasPassed() {
        XCTAssertFalse(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: start, now: start))
        XCTAssertFalse(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: start, now: start.addingTimeInterval(14 * 60 + 59)))
        XCTAssertTrue(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: start, now: start.addingTimeInterval(15 * 60)))
        XCTAssertTrue(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: start, now: start.addingTimeInterval(86_400)))
    }

    func testAClockSetBackAsksButASmallSkewDoesNot() {
        XCTAssertFalse(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: start, now: start.addingTimeInterval(-2)))
        XCTAssertTrue(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 15, lastUsedAt: start, now: start.addingTimeInterval(-3600)))
    }

    func testTheLockTimeIsKeptWithinWhatTheServerAccepts() {
        XCTAssertEqual(PasswordsLockPolicy.clamped(lockAfterMinutes: 0), 1)
        XCTAssertEqual(PasswordsLockPolicy.clamped(lockAfterMinutes: 99_999), 10080)
        XCTAssertTrue(PasswordsLockPolicy.isAskRequired(lockAfterMinutes: 0, lastUsedAt: start, now: start.addingTimeInterval(61)))
        XCTAssertEqual(PasswordsLockPolicy.lockDate(lockAfterMinutes: 30, lastUsedAt: start), start.addingTimeInterval(1800))
    }
}
