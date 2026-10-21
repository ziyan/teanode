import Foundation
import XCTest

@testable import TeaNodeKit

final class PasswordsDisplayTextTests: XCTestCase {
    private let space = PasswordsDisplayText.zeroWidthSpace

    func testAUserNameMayEndALineAfterTheAtSignAndTheDotButKeepsEveryCharacter() {
        let shown = PasswordsDisplayText.breakable("robin@example.com")
        XCTAssertEqual(shown, "robin@\(space)example.\(space)com")
        XCTAssertEqual(PasswordsDisplayText.plain(shown), "robin@example.com")
    }

    func testASiteMayEndALineAfterEachSlashDotAndHyphen() {
        let shown = PasswordsDisplayText.breakable("https://mail.example.com/sign-in")
        XCTAssertEqual(shown, "https:\(space)//\(space)mail.\(space)example.\(space)com/\(space)sign-\(space)in")
        XCTAssertEqual(PasswordsDisplayText.plain(shown), "https://mail.example.com/sign-in")
    }

    func testALongRandomStringMayBreakBetweenAnyTwoCharacters() {
        let shown = PasswordsDisplayText.breakable("Xk3Tq9Lm2Vw7")
        XCTAssertEqual(shown, "X\(space)k\(space)3\(space)T\(space)q\(space)9\(space)L\(space)m\(space)2\(space)V\(space)w\(space)7")
        XCTAssertEqual(PasswordsDisplayText.plain(shown), "Xk3Tq9Lm2Vw7")
    }

    func testAShortWordIsLeftWholeSoALineEndsBetweenWordsAndNotInsideThem() {
        XCTAssertEqual(PasswordsDisplayText.breakable("orchard"), "orchard")
        XCTAssertEqual(PasswordsDisplayText.breakable("Used for trips only."), "Used for trips only.")
    }

    func testANumberAndTheEndOfASentenceAreNotBrokenAtTheirDot() {
        XCTAssertEqual(PasswordsDisplayText.breakable("Costs 3.14 each."), "Costs 3.14 each.")
        XCTAssertEqual(PasswordsDisplayText.breakable("Ends here.", isProse: true), "Ends here.")
    }

    func testProseKeepsALongerWordWholeThanAnIdentifierDoes() {
        let word = "internationally"
        XCTAssertNotEqual(PasswordsDisplayText.breakable(word), word)
        XCTAssertEqual(PasswordsDisplayText.breakable(word, isProse: true), word)
    }

    func testLineBreaksAndSpacesAreKept() {
        let shown = PasswordsDisplayText.breakable("first line\nsecond  line", isProse: true)
        XCTAssertEqual(shown, "first line\nsecond  line")
    }

    func testNothingIsAddedBeforeAnOpeningSeparatorOrAfterTheLastCharacter() {
        XCTAssertEqual(PasswordsDisplayText.breakable("-5"), "-5")
        XCTAssertEqual(PasswordsDisplayText.breakable("ada@"), "ada@")
        XCTAssertEqual(PasswordsDisplayText.breakable(""), "")
    }

    func testTheValueComesBackWhateverIsInIt() {
        for value in ["a@b.c", "emoji 👩‍👩‍👧‍👦 family-of-four", "日本語のノートです。https://example.test/ノート", "Xk3Tq9Lm2Vw7!?=&_-+#%", "  "] {
            XCTAssertEqual(PasswordsDisplayText.plain(PasswordsDisplayText.breakable(value)), value, value)
            XCTAssertEqual(PasswordsDisplayText.plain(PasswordsDisplayText.breakable(value, isProse: true)), value, value)
        }
    }
}
