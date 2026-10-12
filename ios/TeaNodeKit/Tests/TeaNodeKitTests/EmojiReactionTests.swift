import XCTest

@testable import TeaNodeKit

final class EmojiReactionTests: XCTestCase {
    func testEmojiOfEveryMakeIsRead() {
        let emoji = [
            "👍",  // drawn in color by default
            "❤️",  // a heart and the emoji selector
            "👍🏽",  // with a skin tone
            "🇯🇵",  // a flag, two regional indicators
            "👨‍👩‍👧‍👦",  // a family joined by zero width joiners
            "🏳️‍🌈",  // a flag and a rainbow joined
            "1️⃣",  // a keycap
            "✌️",
            "🙂",
        ]
        for one in emoji {
            XCTAssertEqual(EmojiReaction.firstEmoji(in: one), one, one)
        }
    }

    func testTextIsNotEmoji() {
        for text in ["", "a", "hello", "7", "#", "*", "é", " ", "❤︎", "\u{1F1EF}", "…", "(:"] {
            XCTAssertNil(EmojiReaction.firstEmoji(in: text), text)
        }
    }

    func testTheFirstEmojiTypedIsTheReaction() {
        XCTAssertEqual(EmojiReaction.firstEmoji(in: "ok 🔥 and 🎉"), "🔥")
        XCTAssertEqual(EmojiReaction.firstEmoji(in: "🔥🎉"), "🔥")
        XCTAssertEqual(EmojiReaction.firstEmoji(in: "7🇯🇵"), "🇯🇵")
    }

    func testAnEmojiLongerThanTheServerTakesIsNotOne() {
        // Eight people joined: 8 × 4 bytes and seven joiners of 3 bytes.
        let crowd = Array(repeating: "👩", count: 8).joined(separator: "\u{200D}")
        XCTAssertGreaterThan(crowd.utf8.count, EmojiReaction.bytesMost)
        XCTAssertNil(EmojiReaction.firstEmoji(in: crowd))
    }
}
