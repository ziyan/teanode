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
    /// Reads an `otpauth://totp/...` link.
    public static func parse(uri: String) throws -> PasswordsOneTimeCodeSettings {
        guard let components = URLComponents(string: uri.trimmingCharacters(in: .whitespacesAndNewlines)),
            components.scheme?.lowercased() == "otpauth", components.host?.lowercased() == "totp"
        else { throw PasswordsOneTimeCodeError.notTimeBasedLink }

        var parameters: [String: String] = [:]
        for item in components.queryItems ?? [] {
            if let value = item.value, parameters[item.name.lowercased()] == nil {
                parameters[item.name.lowercased()] = value
            }
        }

        guard let secretText = parameters["secret"], let secret = PasswordsBase32.decode(secretText.filter { $0 != "-" }),
            !secret.isEmpty
        else { throw PasswordsOneTimeCodeError.invalidSecret }

        var algorithm = PasswordsOneTimeCodeAlgorithm.sha1
        if let algorithmText = parameters["algorithm"] {
            guard let parsed = PasswordsOneTimeCodeAlgorithm(rawValue: algorithmText.uppercased()) else {
                throw PasswordsOneTimeCodeError.unsupportedAlgorithm
            }
            algorithm = parsed
        }

        var digitCount = 6
        if let digitText = parameters["digits"] {
            guard let parsed = Int(digitText), parsed == 6 || parsed == 8 else {
                throw PasswordsOneTimeCodeError.unsupportedDigitCount
            }
            digitCount = parsed
        }

        var periodSeconds = 30
        if let periodText = parameters["period"] {
            guard let parsed = Int(periodText), parsed > 0 else { throw PasswordsOneTimeCodeError.invalidPeriod }
            periodSeconds = parsed
        }

        // The label is the path without its leading slash: "Issuer:account" or "account".
        var label = components.path
        if label.hasPrefix("/") { label.removeFirst() }
        var issuer = parameters["issuer"] ?? ""
        var accountName = label
        if let separator = label.firstIndex(of: ":") {
            let labelIssuer = String(label[label.startIndex..<separator]).trimmingCharacters(in: .whitespaces)
            accountName = String(label[label.index(after: separator)...]).trimmingCharacters(in: .whitespaces)
            if issuer.isEmpty { issuer = labelIssuer }
        }

        return PasswordsOneTimeCodeSettings(
            secret: secret, algorithm: algorithm, digitCount: digitCount, periodSeconds: periodSeconds, issuer: issuer,
            accountName: accountName)
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
