import XCTest

@testable import TeaNodeKit

/// AvatarColorTests hold the vectors the server's InitialColor and the
/// dashboard's avatarColorIndexOf are checked against, so the three cannot
/// drift apart.
final class AvatarColorTests: XCTestCase {
    func testMatchesTheSharedVectors() {
        XCTAssertEqual(AvatarColor.index(of: "01JABCDEFGHJKMNPQRSTVWXYZ0"), 10)
        XCTAssertEqual(AvatarColor.index(of: "01HZZZZZZZZZZZZZZZZZZZZZZZ"), 4)
        XCTAssertEqual(AvatarColor.index(of: "user-a"), 0)
    }

    func testTheServersIndexWinsWhenItIsUsable() {
        XCTAssertEqual(AvatarColor.index(serverIndex: 7, fallbackText: "user-a"), 7)
        XCTAssertEqual(AvatarColor.index(serverIndex: 0, fallbackText: "01JABCDEFGHJKMNPQRSTVWXYZ0"), 0)
    }

    func testAMissingOrOutOfRangeIndexFallsBackToTheText() {
        XCTAssertEqual(AvatarColor.index(serverIndex: nil, fallbackText: "01JABCDEFGHJKMNPQRSTVWXYZ0"), 10)
        XCTAssertEqual(AvatarColor.index(serverIndex: 12, fallbackText: "01HZZZZZZZZZZZZZZZZZZZZZZZ"), 4)
        XCTAssertEqual(AvatarColor.index(serverIndex: -1, fallbackText: "user-a"), 0)
    }

    func testEveryTextLandsOnOneOfTheTwelveColors() {
        for text in ["", "a", "\u{4E2D}\u{6587}", "01JZZZZZZZZZZZZZZZZZZZZZZZ"] {
            XCTAssertTrue((0..<AvatarColor.count).contains(AvatarColor.index(of: text)))
        }
    }
}
