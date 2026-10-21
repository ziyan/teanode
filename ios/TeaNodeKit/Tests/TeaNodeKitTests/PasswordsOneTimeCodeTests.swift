import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsOneTimeCodeTests check the codes against RFC 6238 appendix B and
/// the links against the forms websites actually produce.
final class PasswordsOneTimeCodeTests: XCTestCase {
    private let sha1Secret = "12345678901234567890"
    private let sha256Secret = "12345678901234567890123456789012"
    private let sha512Secret = "1234567890123456789012345678901234567890123456789012345678901234"

    private func uri(secret: String, algorithm: String) -> String {
        "otpauth://totp/Example:ada@example.com?secret=\(PasswordsBase32.encode(Data(secret.utf8)))&algorithm=\(algorithm)&digits=8&issuer=Example"
    }

    func testRFC6238AppendixBVectors() throws {
        // seconds, SHA1, SHA256, SHA512
        let rows: [(Int, String, String, String)] = [
            (59, "94287082", "46119246", "90693936"),
            (1_111_111_109, "07081804", "68084774", "25091201"),
            (1_111_111_111, "14050471", "67062674", "99943326"),
            (1_234_567_890, "89005924", "91819424", "93441116"),
            (2_000_000_000, "69279037", "90698825", "38618901"),
            (20_000_000_000, "65353130", "77737706", "47863826"),
        ]
        let sha1 = try PasswordsOneTimeCode.parse(uri: uri(secret: sha1Secret, algorithm: "SHA1"))
        let sha256 = try PasswordsOneTimeCode.parse(uri: uri(secret: sha256Secret, algorithm: "SHA256"))
        let sha512 = try PasswordsOneTimeCode.parse(uri: uri(secret: sha512Secret, algorithm: "SHA512"))
        for (seconds, expectedSha1, expectedSha256, expectedSha512) in rows {
            XCTAssertEqual(PasswordsOneTimeCode.code(settings: sha1, unixSeconds: seconds), expectedSha1, "SHA1 at \(seconds)")
            XCTAssertEqual(PasswordsOneTimeCode.code(settings: sha256, unixSeconds: seconds), expectedSha256, "SHA256 at \(seconds)")
            XCTAssertEqual(PasswordsOneTimeCode.code(settings: sha512, unixSeconds: seconds), expectedSha512, "SHA512 at \(seconds)")
        }
    }

    func testDefaultsAreSHA1SixDigitsThirtySeconds() throws {
        let settings = try PasswordsOneTimeCode.parse(
            uri: "otpauth://totp/Example:ada@example.com?secret=\(PasswordsBase32.encode(Data(sha1Secret.utf8)))&issuer=Example")
        XCTAssertEqual(settings.algorithm, .sha1)
        XCTAssertEqual(settings.digitCount, 6)
        XCTAssertEqual(settings.periodSeconds, 30)
        XCTAssertEqual(settings.issuer, "Example")
        XCTAssertEqual(settings.accountName, "ada@example.com")
        XCTAssertEqual(PasswordsOneTimeCode.code(settings: settings, unixSeconds: 59), "287082")
    }

    func testLowerCaseUnpaddedAndSpacedSecretsReadTheSame() throws {
        let upper = try PasswordsOneTimeCode.parse(uri: "otpauth://totp/a?secret=JBSWY3DPEHPK3PXP")
        let lower = try PasswordsOneTimeCode.parse(uri: "otpauth://totp/a?secret=jbswy3dpehpk3pxp")
        let padded = try PasswordsOneTimeCode.parse(uri: "otpauth://totp/a?secret=JBSWY3DPEHPK3PXP======")
        let spaced = try PasswordsOneTimeCode.parse(uri: "otpauth://totp/a?secret=jbsw%20y3dp%20ehpk%203pxp")
        XCTAssertEqual(upper.secret, lower.secret)
        XCTAssertEqual(upper.secret, padded.secret)
        XCTAssertEqual(upper.secret, spaced.secret)
        XCTAssertEqual(upper.secret, Data([0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x21, 0xDE, 0xAD, 0xBE, 0xEF]))
    }

    func testPeriodAndSecondsLeft() throws {
        let settings = try PasswordsOneTimeCode.parse(uri: "otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=60")
        XCTAssertEqual(settings.periodSeconds, 60)
        XCTAssertEqual(PasswordsOneTimeCode.secondsLeft(settings: settings, unixSeconds: 0), 60)
        XCTAssertEqual(PasswordsOneTimeCode.secondsLeft(settings: settings, unixSeconds: 59), 1)
        XCTAssertEqual(PasswordsOneTimeCode.secondsLeft(settings: settings, unixSeconds: 60), 60)
        XCTAssertEqual(PasswordsOneTimeCode.secondsLeft(settings: settings, date: Date(timeIntervalSince1970: 90)), 30)
        XCTAssertEqual(
            PasswordsOneTimeCode.code(settings: settings, unixSeconds: 61), PasswordsOneTimeCode.code(settings: settings, unixSeconds: 119))
        XCTAssertNotEqual(
            PasswordsOneTimeCode.code(settings: settings, unixSeconds: 59), PasswordsOneTimeCode.code(settings: settings, unixSeconds: 60))
    }

    func testCodesKeepLeadingZeros() throws {
        let settings = try PasswordsOneTimeCode.parse(uri: uri(secret: sha1Secret, algorithm: "SHA1"))
        XCTAssertEqual(PasswordsOneTimeCode.code(settings: settings, unixSeconds: 1_111_111_109), "07081804")
    }

    func testLabelWithoutAnIssuerAndIssuerOnlyInTheQuery() throws {
        let plain = try PasswordsOneTimeCode.parse(uri: "otpauth://totp/ada%40example.test?secret=JBSWY3DPEHPK3PXP&issuer=Example%20Inc")
        XCTAssertEqual(plain.issuer, "Example Inc")
        XCTAssertEqual(plain.accountName, "ada@example.test")
    }

    func testBadLinksAreRefusedWithAReason() {
        func failure(_ link: String) -> PasswordsOneTimeCodeError? {
            do {
                _ = try PasswordsOneTimeCode.parse(uri: link)
                return nil
            } catch {
                return error as? PasswordsOneTimeCodeError
            }
        }
        XCTAssertEqual(failure("https://example.com"), .notTimeBasedLink)
        XCTAssertEqual(failure("otpauth://hotp/a?secret=JBSWY3DPEHPK3PXP&counter=1"), .notTimeBasedLink)
        XCTAssertEqual(failure("otpauth://totp/a"), .invalidSecret)
        XCTAssertEqual(failure("otpauth://totp/a?secret=!!!"), .invalidSecret)
        XCTAssertEqual(failure("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&algorithm=MD5"), .unsupportedAlgorithm)
        XCTAssertEqual(failure("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&digits=7"), .unsupportedDigitCount)
        XCTAssertEqual(failure("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=0"), .invalidPeriod)
    }
}
