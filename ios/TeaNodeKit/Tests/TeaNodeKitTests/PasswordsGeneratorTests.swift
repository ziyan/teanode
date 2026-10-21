import Foundation
import XCTest

@testable import TeaNodeKit

final class PasswordsGeneratorTests: XCTestCase {
    func testEveryKindAskedForAppearsAndTheLengthIsRight() throws {
        for attempt in 0..<50 {
            let password = try PasswordsGenerator.generate(options: PasswordsGeneratorOptions(length: 8))
            XCTAssertEqual(password.count, 8, "attempt \(attempt)")
            XCTAssertTrue(password.contains { $0.isASCII && $0.isLowercase })
            XCTAssertTrue(password.contains { $0.isASCII && $0.isUppercase })
            XCTAssertTrue(password.contains { $0.isASCII && $0.isNumber })
            XCTAssertTrue(password.contains { PasswordsGenerator.symbols.contains($0) })
        }
    }

    func testLookAlikesAreLeftOutWhenAsked() throws {
        let options = PasswordsGeneratorOptions(length: 128, shouldAvoidLookAlikes: true)
        for _ in 0..<20 {
            let password = try PasswordsGenerator.generate(options: options)
            XCTAssertFalse(password.contains { PasswordsGenerator.lookAlikes.contains($0) })
        }
    }

    func testOnlyTheKindsChosenAreUsed() throws {
        let options = PasswordsGeneratorOptions(length: 40, shouldIncludeLetters: false, shouldIncludeDigits: true, shouldIncludeSymbols: false)
        let password = try PasswordsGenerator.generate(options: options)
        XCTAssertEqual(password.count, 40)
        XCTAssertTrue(password.allSatisfy { $0.isASCII && $0.isNumber })
    }

    func testRefusesNoKindAndAnImpossibleLength() {
        let none = PasswordsGeneratorOptions(length: 20, shouldIncludeLetters: false, shouldIncludeDigits: false, shouldIncludeSymbols: false)
        XCTAssertThrowsError(try PasswordsGenerator.generate(options: none)) { error in
            XCTAssertEqual(error as? PasswordsGeneratorError, .noKindChosen)
        }
        XCTAssertThrowsError(try PasswordsGenerator.generate(options: PasswordsGeneratorOptions(length: 3))) { error in
            XCTAssertEqual(error as? PasswordsGeneratorError, .lengthOutOfRange)
        }
        XCTAssertThrowsError(try PasswordsGenerator.generate(options: PasswordsGeneratorOptions(length: 129))) { error in
            XCTAssertEqual(error as? PasswordsGeneratorError, .lengthOutOfRange)
        }
    }

    func testANumberFromTheUnfairEndOfTheRangeIsDrawnAgain() {
        // 2^32 is not a multiple of 10: the largest is 4_294_967_290, so a draw at or
        // above it is thrown away and the next one is used.
        var draws: [UInt32] = [4_294_967_295, 4_294_967_290, 13]
        let index = PasswordsGenerator.randomIndex(limit: 10) { draws.removeFirst() }
        XCTAssertEqual(index, 3)
        XCTAssertTrue(draws.isEmpty)
    }

    func testPassphraseStrength() {
        XCTAssertEqual(PasswordsPassphraseStrength.of(""), .empty)
        XCTAssertEqual(PasswordsPassphraseStrength.of("abc"), .short)
        XCTAssertEqual(PasswordsPassphraseStrength.of("abcdefgh"), .short)
        XCTAssertEqual(PasswordsPassphraseStrength.of("abcdefghijk"), .short)
        XCTAssertEqual(PasswordsPassphraseStrength.minimumLength, 12)
        XCTAssertEqual(PasswordsPassphraseStrength.of("Abcdefgh1234"), .fair)
        XCTAssertEqual(PasswordsPassphraseStrength.of("Abcdefgh12345678"), .strong)
        // One kind of character only counts a step less, until it is long.
        XCTAssertEqual(PasswordsPassphraseStrength.of("abcdefghijkl"), .weak)
        XCTAssertEqual(PasswordsPassphraseStrength.of("abcdefghijklmnopqrstu"), .strong)
    }

    func testTheCopyHasAFileOfItsOwnForEachSignIn() {
        let first = PasswordsLocation.fileName(signInAccountKey: "demo.example.com|robin")
        let second = PasswordsLocation.fileName(signInAccountKey: "demo.example.com|sam")
        XCTAssertNotEqual(first, second)
        XCTAssertFalse(first.contains("/"))
        XCTAssertFalse(first.contains("|"))
        XCTAssertTrue(first.hasPrefix("passwords-") && first.hasSuffix(".json"))
        let url = PasswordsLocation.fileURL(groupContainer: URL(fileURLWithPath: "/container"), signInAccountKey: "demo.example.com|robin")
        XCTAssertEqual(url.deletingLastPathComponent().lastPathComponent, PasswordsLocation.directoryName)
    }
}
