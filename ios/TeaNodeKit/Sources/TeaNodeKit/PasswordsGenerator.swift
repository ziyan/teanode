import Foundation

// The password generator, with the rules of web/src/passwords/generator.ts:
// every character is drawn at random with no character likelier than another
// (a plain remainder favors the first characters whenever the alphabet does not
// divide 2^32, so a number from the unfair end of the range is drawn again),
// each kind asked for appears at least once, and the order is then shuffled.

/// PasswordsGeneratorError is why a password could not be generated.
public enum PasswordsGeneratorError: Error, Equatable, Sendable {
    /// No kind of character was chosen.
    case noKindChosen
    /// The length is below the number of kinds chosen, or above the most allowed.
    case lengthOutOfRange
}

/// PasswordsGeneratorOptions is what the person chooses for a new password.
public struct PasswordsGeneratorOptions: Equatable, Sendable {
    public var length: Int
    public var shouldIncludeLetters: Bool
    public var shouldIncludeDigits: Bool
    public var shouldIncludeSymbols: Bool
    /// Leaves out the characters that read as one another in many typefaces: I l 1 | O 0 o.
    public var shouldAvoidLookAlikes: Bool

    public init(
        length: Int = 20, shouldIncludeLetters: Bool = true, shouldIncludeDigits: Bool = true, shouldIncludeSymbols: Bool = true,
        shouldAvoidLookAlikes: Bool = false
    ) {
        self.length = length
        self.shouldIncludeLetters = shouldIncludeLetters
        self.shouldIncludeDigits = shouldIncludeDigits
        self.shouldIncludeSymbols = shouldIncludeSymbols
        self.shouldAvoidLookAlikes = shouldAvoidLookAlikes
    }

    /// The options a new password starts with.
    public static let standard = PasswordsGeneratorOptions()

    /// How many kinds of character are chosen: lower case and upper case
    /// letters count as two, so each appears at least once.
    public var kindCount: Int {
        (shouldIncludeLetters ? 2 : 0) + (shouldIncludeDigits ? 1 : 0) + (shouldIncludeSymbols ? 1 : 0)
    }
}

/// PasswordsGenerator makes random passwords.
public enum PasswordsGenerator {
    public static let minimumLength = 4
    public static let maximumLength = 128

    static let lowercaseLetters = "abcdefghijklmnopqrstuvwxyz"
    static let uppercaseLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
    static let digits = "0123456789"
    static let symbols = "!@#$%^&*()-_=+[]{}.,:;?"
    static let lookAlikes = "Il1|O0o"

    /// A whole number from 0 up to but not including `limit`, every one equally
    /// likely. `nextRandom` gives random 32 bit numbers; the default is the
    /// system's secure generator, and a test gives its own to count draws.
    public static func randomIndex(limit: Int, nextRandom: () -> UInt32 = { UInt32.random(in: UInt32.min...UInt32.max) }) -> Int {
        precondition(limit >= 1 && limit <= 0x1_0000_0000, "The limit must be a whole number from 1 to 2^32")
        let limitValue = UInt64(limit)
        // Numbers at or above the largest multiple of the limit would make the
        // low indexes likelier, so they are drawn again.
        let largestMultiple = (UInt64(0x1_0000_0000) / limitValue) * limitValue
        while true {
            let drawn = UInt64(nextRandom())
            if drawn < largestMultiple { return Int(drawn % limitValue) }
        }
    }

    private static func alphabet(_ characters: String, options: PasswordsGeneratorOptions) -> [Character] {
        let all = Array(characters)
        guard options.shouldAvoidLookAlikes else { return all }
        return all.filter { !lookAlikes.contains($0) }
    }

    /// A password of `options.length` characters. Each kind asked for appears
    /// at least once, and the order is shuffled.
    public static func generate(
        options: PasswordsGeneratorOptions = .standard, nextRandom: () -> UInt32 = { UInt32.random(in: UInt32.min...UInt32.max) }
    ) throws -> String {
        var alphabets: [[Character]] = []
        if options.shouldIncludeLetters {
            alphabets.append(alphabet(lowercaseLetters, options: options))
            alphabets.append(alphabet(uppercaseLetters, options: options))
        }
        if options.shouldIncludeDigits { alphabets.append(alphabet(digits, options: options)) }
        if options.shouldIncludeSymbols { alphabets.append(alphabet(symbols, options: options)) }
        guard !alphabets.isEmpty else { throw PasswordsGeneratorError.noKindChosen }
        guard options.length >= alphabets.count, options.length <= maximumLength else { throw PasswordsGeneratorError.lengthOutOfRange }

        let everyCharacter = Array(alphabets.joined())
        var characters: [Character] = alphabets.map { $0[randomIndex(limit: $0.count, nextRandom: nextRandom)] }
        while characters.count < options.length {
            characters.append(everyCharacter[randomIndex(limit: everyCharacter.count, nextRandom: nextRandom)])
        }
        // Fisher-Yates, so the characters guaranteed above do not sit at the front.
        var index = characters.count - 1
        while index > 0 {
            let otherIndex = randomIndex(limit: index + 1, nextRandom: nextRandom)
            characters.swapAt(index, otherIndex)
            index -= 1
        }
        return String(characters)
    }
}

/// PasswordsPassphraseStrength is a hint about a passphrase, not a promise:
/// length counts most, and one kind of character only counts a step less. It
/// is the dashboard's rule (web/src/pages/passwords/passwordsProtocol.ts).
public enum PasswordsPassphraseStrength: Equatable, Sendable {
    case empty, short, weak, fair, strong

    /// The fewest characters a passphrase may have.
    public static let minimumLength = PasswordsCrypto.minimumPassphraseLength

    public static func of(_ passphrase: String) -> PasswordsPassphraseStrength {
        let length = passphrase.count
        if length == 0 { return .empty }
        if length < minimumLength { return .short }
        var kindCount = 0
        if passphrase.contains(where: { $0.isASCII && $0.isLowercase }) { kindCount += 1 }
        if passphrase.contains(where: { $0.isASCII && $0.isUppercase }) { kindCount += 1 }
        if passphrase.contains(where: { $0.isASCII && $0.isNumber }) { kindCount += 1 }
        if passphrase.contains(where: { !($0.isASCII && ($0.isLetter || $0.isNumber)) }) { kindCount += 1 }
        let steps: [PasswordsPassphraseStrength] = [.weak, .fair, .strong]
        var step = length >= 16 ? 2 : (length >= 12 ? 1 : 0)
        if kindCount == 1 && length < 20 { step = max(0, step - 1) }
        return steps[step]
    }
}
