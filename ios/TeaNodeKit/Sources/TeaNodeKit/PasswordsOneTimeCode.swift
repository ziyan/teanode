import Foundation

#if canImport(CryptoKit)
    import CryptoKit
#else
    import Crypto
#endif

// One-time codes: RFC 6238 (time based) over RFC 4226, from the
// `otpauth://totp/...` link a website shows next to its QR code. The secret is
// base32 (any case, padding optional); `algorithm` is SHA1 (the default),
// SHA256 or SHA512; `digits` is 6 (the default) or 8; `period` is 30 seconds
// unless given.

/// PasswordsOneTimeCodeError is why an `otpauth://` link could not be used.
public enum PasswordsOneTimeCodeError: Error, Equatable, Sendable {
    /// Not an `otpauth://totp/` link (counter based `hotp` links are not supported).
    case notTimeBasedLink
    /// No `secret`, or one that is not base32.
    case invalidSecret
    /// An `algorithm` other than SHA1, SHA256 or SHA512.
    case unsupportedAlgorithm
    /// `digits` other than 6 or 8.
    case unsupportedDigitCount
    /// A `period` that is not a whole number of seconds above zero.
    case invalidPeriod
}

/// PasswordsOneTimeCodeAlgorithm is the hash a link names for its codes.
public enum PasswordsOneTimeCodeAlgorithm: String, Equatable, Sendable {
    case sha1 = "SHA1"
    case sha256 = "SHA256"
    case sha512 = "SHA512"
}

/// PasswordsOneTimeCodeSettings is what an `otpauth://` link says.
public struct PasswordsOneTimeCodeSettings: Equatable, Sendable {
    public var secret: Data
    public var algorithm: PasswordsOneTimeCodeAlgorithm
    public var digitCount: Int
    public var periodSeconds: Int
    /// The service named in the link, from `issuer` or the label's prefix; empty if neither.
    public var issuer: String
    /// The account named in the label after the issuer; empty if none.
    public var accountName: String

    public init(
        secret: Data, algorithm: PasswordsOneTimeCodeAlgorithm = .sha1, digitCount: Int = 6, periodSeconds: Int = 30, issuer: String = "",
        accountName: String = ""
    ) {
        self.secret = secret
        self.algorithm = algorithm
        self.digitCount = digitCount
        self.periodSeconds = periodSeconds
        self.issuer = issuer
        self.accountName = accountName
    }
}

/// PasswordsOneTimeCode reads `otpauth://` links and works out the codes.
public enum PasswordsOneTimeCode {
    /// The longest period read: far longer than any site uses, and a number every language here holds exactly.
    public static let maximumPeriodSeconds = 2_147_483_647

    /// Reads an `otpauth://totp/...` link. The reading is strict, and the same as the Go package and the
    /// dashboard's (the cases in internal/passwords/testdata/onetimecode.json): only ASCII white space is
    /// trimmed from the ends; a control character anywhere is refused; the host is exactly `totp` (no
    /// user name, no port); a malformed percent escape or a semicolon in the query is refused; the
    /// secret is base32 letters with spaces and hyphens ignored and `=` only at the end; the digit
    /// count and the period are plain ASCII digits, the period at most `maximumPeriodSeconds`.
    /// Written by hand rather than with URLComponents, whose answers differ from platform to platform.
    public static func parse(uri: String) throws -> PasswordsOneTimeCodeSettings {
        let asciiWhiteSpace: Set<Unicode.Scalar> = [" ", "\t", "\n", "\r", "\u{0C}", "\u{0B}"]
        var scalars = Array(uri.unicodeScalars)
        while let first = scalars.first, asciiWhiteSpace.contains(first) { scalars.removeFirst() }
        while let last = scalars.last, asciiWhiteSpace.contains(last) { scalars.removeLast() }
        guard !scalars.contains(where: { $0.value < 0x20 || $0.value == 0x7F }) else {
            throw PasswordsOneTimeCodeError.notTimeBasedLink
        }
        var rest = String(String.UnicodeScalarView(scalars))
        // A fragment is not part of what the link says.
        if let fragmentStart = rest.firstIndex(of: "#") { rest = String(rest[rest.startIndex..<fragmentStart]) }

        guard let schemeEnd = rest.firstIndex(of: ":"), rest[rest.startIndex..<schemeEnd].lowercased() == "otpauth",
            rest[rest.index(after: schemeEnd)...].hasPrefix("//")
        else { throw PasswordsOneTimeCodeError.notTimeBasedLink }
        rest = String(rest[rest.index(rest.index(after: schemeEnd), offsetBy: 2)...])

        var queryText = ""
        if let queryStart = rest.firstIndex(of: "?") {
            queryText = String(rest[rest.index(after: queryStart)...])
            rest = String(rest[rest.startIndex..<queryStart])
        }
        var host = rest
        var pathText = ""
        if let pathStart = rest.firstIndex(of: "/") {
            host = String(rest[rest.startIndex..<pathStart])
            pathText = String(rest[pathStart...])
        }
        guard host.lowercased() == "totp" else { throw PasswordsOneTimeCodeError.notTimeBasedLink }

        var parameters: [String: String] = [:]
        guard !queryText.contains(";") else { throw PasswordsOneTimeCodeError.notTimeBasedLink }
        for pair in queryText.split(separator: "&", omittingEmptySubsequences: true) {
            let name: Substring
            let value: Substring
            if let equals = pair.firstIndex(of: "=") {
                name = pair[pair.startIndex..<equals]
                value = pair[pair.index(after: equals)...]
            } else {
                name = pair
                value = ""
            }
            guard let decodedName = percentDecoded(String(name), isPlusSpace: true),
                let decodedValue = percentDecoded(String(value), isPlusSpace: true)
            else { throw PasswordsOneTimeCodeError.notTimeBasedLink }
            // The first of a repeated name is the one read.
            if parameters[decodedName] == nil { parameters[decodedName] = decodedValue }
        }

        let secretText = (parameters["secret"] ?? "").filter { $0 != " " && $0 != "-" }
        let trimmedSecretText = String(secretText.reversed().drop(while: { $0 == "=" }).reversed())
        guard !trimmedSecretText.isEmpty, let secret = lenientBase32(trimmedSecretText), !secret.isEmpty else {
            throw PasswordsOneTimeCodeError.invalidSecret
        }

        var algorithm = PasswordsOneTimeCodeAlgorithm.sha1
        if let algorithmText = parameters["algorithm"], !algorithmText.isEmpty {
            guard let parsed = PasswordsOneTimeCodeAlgorithm(rawValue: algorithmText.uppercased()) else {
                throw PasswordsOneTimeCodeError.unsupportedAlgorithm
            }
            algorithm = parsed
        }

        var digitCount = 6
        if let digitText = parameters["digits"], !digitText.isEmpty {
            guard let parsed = asciiNumber(digitText), parsed == 6 || parsed == 8 else {
                throw PasswordsOneTimeCodeError.unsupportedDigitCount
            }
            digitCount = parsed
        }

        var periodSeconds = 30
        if let periodText = parameters["period"], !periodText.isEmpty {
            guard let parsed = asciiNumber(periodText), parsed >= 1, parsed <= maximumPeriodSeconds else {
                throw PasswordsOneTimeCodeError.invalidPeriod
            }
            periodSeconds = parsed
        }

        // The label is the path without its leading slash: "Issuer:account" or "account".
        guard var label = percentDecoded(pathText, isPlusSpace: false) else { throw PasswordsOneTimeCodeError.notTimeBasedLink }
        if label.hasPrefix("/") { label.removeFirst() }
        var issuer = parameters["issuer"] ?? ""
        var accountName = label.trimmingCharacters(in: .whitespacesAndNewlines)
        if let separator = label.firstIndex(of: ":") {
            let labelIssuer = String(label[label.startIndex..<separator]).trimmingCharacters(in: .whitespacesAndNewlines)
            accountName = String(label[label.index(after: separator)...]).trimmingCharacters(in: .whitespacesAndNewlines)
            if issuer.isEmpty { issuer = labelIssuer }
        }

        return PasswordsOneTimeCodeSettings(
            secret: secret, algorithm: algorithm, digitCount: digitCount, periodSeconds: periodSeconds, issuer: issuer,
            accountName: accountName)
    }

    /// Text with its `%XX` escapes read (and `+` as a space in a query); nil for an escape that is not two hex digits.
    private static func percentDecoded(_ text: String, isPlusSpace: Bool) -> String? {
        var bytes: [UInt8] = []
        let source = Array(text.utf8)
        var index = 0
        while index < source.count {
            let byte = source[index]
            if byte == UInt8(ascii: "%") {
                guard index + 2 < source.count, let high = hexValue(source[index + 1]), let low = hexValue(source[index + 2]) else {
                    return nil
                }
                bytes.append(UInt8(high * 16 + low))
                index += 3
            } else if byte == UInt8(ascii: "+") && isPlusSpace {
                bytes.append(UInt8(ascii: " "))
                index += 1
            } else {
                bytes.append(byte)
                index += 1
            }
        }
        return String(decoding: bytes, as: UTF8.self)
    }

    private static func hexValue(_ byte: UInt8) -> Int? {
        switch byte {
        case UInt8(ascii: "0")...UInt8(ascii: "9"): return Int(byte - UInt8(ascii: "0"))
        case UInt8(ascii: "a")...UInt8(ascii: "f"): return Int(byte - UInt8(ascii: "a")) + 10
        case UInt8(ascii: "A")...UInt8(ascii: "F"): return Int(byte - UInt8(ascii: "A")) + 10
        default: return nil
        }
    }

    /// A whole number made only of ASCII digits (no sign, no space); nil when it is anything else or too large.
    private static func asciiNumber(_ text: String) -> Int? {
        guard !text.isEmpty, text.utf8.allSatisfy({ $0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9") }) else { return nil }
        return Int(text)
    }

    /// Base32 letters (any case, 2 to 7) read to bytes; the bits left over after the last whole byte are
    /// dropped, as the Go package and the dashboard do. Nil for any other character.
    private static func lenientBase32(_ text: String) -> Data? {
        var bytes = Data()
        var buffer = 0
        var bitCount = 0
        for byte in text.utf8 {
            let value: Int
            switch byte {
            case UInt8(ascii: "A")...UInt8(ascii: "Z"): value = Int(byte - UInt8(ascii: "A"))
            case UInt8(ascii: "a")...UInt8(ascii: "z"): value = Int(byte - UInt8(ascii: "a"))
            case UInt8(ascii: "2")...UInt8(ascii: "7"): value = Int(byte - UInt8(ascii: "2")) + 26
            default: return nil
            }
            buffer = (buffer << 5) | value
            bitCount += 5
            if bitCount >= 8 {
                bitCount -= 8
                bytes.append(UInt8((buffer >> bitCount) & 0xFF))
                buffer &= (1 << bitCount) - 1
            }
        }
        return bytes
    }

    /// The code at a time, as digits with leading zeros kept.
    public static func code(settings: PasswordsOneTimeCodeSettings, unixSeconds: Int) -> String {
        let counter = UInt64(max(unixSeconds, 0) / settings.periodSeconds)
        var message = Data()
        for shift in stride(from: 56, through: 0, by: -8) {
            message.append(UInt8((counter >> UInt64(shift)) & 0xFF))
        }
        let digest = hmac(settings: settings, message: message)
        let offset = Int(digest[digest.count - 1] & 0x0F)
        let binary =
            (UInt32(digest[offset] & 0x7F) << 24) | (UInt32(digest[offset + 1]) << 16) | (UInt32(digest[offset + 2]) << 8)
            | UInt32(digest[offset + 3])
        var modulus: UInt32 = 1
        for _ in 0..<settings.digitCount { modulus *= 10 }
        let digits = String(binary % modulus)
        return String(repeating: "0", count: settings.digitCount - digits.count) + digits
    }

    /// The code at a date.
    public static func code(settings: PasswordsOneTimeCodeSettings, date: Date) -> String {
        code(settings: settings, unixSeconds: Int(date.timeIntervalSince1970))
    }

    /// How many seconds the code at a time is still good for (1 to the period).
    public static func secondsLeft(settings: PasswordsOneTimeCodeSettings, unixSeconds: Int) -> Int {
        settings.periodSeconds - max(unixSeconds, 0) % settings.periodSeconds
    }

    /// How many seconds the code at a date is still good for.
    public static func secondsLeft(settings: PasswordsOneTimeCodeSettings, date: Date) -> Int {
        secondsLeft(settings: settings, unixSeconds: Int(date.timeIntervalSince1970))
    }

    private static func hmac(settings: PasswordsOneTimeCodeSettings, message: Data) -> [UInt8] {
        let key = SymmetricKey(data: settings.secret)
        switch settings.algorithm {
        case .sha1: return Array(HMAC<Insecure.SHA1>.authenticationCode(for: message, using: key))
        case .sha256: return Array(HMAC<SHA256>.authenticationCode(for: message, using: key))
        case .sha512: return Array(HMAC<SHA512>.authenticationCode(for: message, using: key))
        }
    }
}
