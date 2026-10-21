import Foundation

#if canImport(CryptoKit)
    import CommonCrypto
    import CryptoKit
#else
    import Crypto
    import _CryptoExtras
#endif

// Passwords are encrypted on the person's devices before they reach the
// server. This file is the byte-level format, written to match the Go package
// internal/passwords and web/src/passwords/crypto.ts exactly; one file of test
// vectors (internal/passwords/testdata/vectors.json) pins all three. The plan
// that fixes the format is docs/planning/passwords-execplan.md, "Encryption".
//
// CryptoKit is Apple's, so on Linux (where TeaNodeKit's tests run) the same
// calls come from swift-crypto. Every function that draws randomness takes it
// as an optional parameter, so the tests can pin it.

/// PasswordsCryptoError is why a Passwords encryption call failed. The cases
/// carry no details on purpose: a wrong key and a damaged value look the same
/// to a person trying to guess.
public enum PasswordsCryptoError: Error, Equatable, Sendable {
    /// A key was not 32 bytes.
    case invalidKeyLength
    /// A nonce was not 12 bytes.
    case invalidNonceLength
    /// A sealed value did not decrypt: the wrong key, the wrong associated data, or damage.
    case openFailed
    /// A sealed value was not the JSON object the format describes.
    case invalidSealedValue
    /// A sealed value has a version this build does not know.
    case unsupportedVersion
    /// A recovery key did not read back as 32 bytes of base32.
    case invalidRecoveryKey
    /// A public or private key was not a valid P-256 key.
    case invalidKeyPair
    /// Key derivation from a passphrase failed.
    case keyDerivationFailed
    /// A passphrase iteration count below the minimum. The count comes back from
    /// the server, and a tiny one would make the proof cheap to guess from.
    case iterationCountTooLow
    /// A passphrase iteration count above the maximum. A server that answered
    /// with an enormous one would make the phone spin until it was killed.
    case iterationCountTooHigh
}

/// PasswordsSealedValue is AES-256-GCM output: a 12 byte nonce and the
/// ciphertext with its 16 byte tag appended. It is stored in a text column as
/// the JSON `{"version":1,"nonce":"<base64>","ciphertext":"<base64>"}`.
public struct PasswordsSealedValue: Codable, Equatable, Sendable {
    public var version: Int
    public var nonce: Data
    /// The encrypted bytes with the 16 byte tag at the end.
    public var ciphertext: Data

    public init(version: Int = PasswordsCrypto.sealedValueVersion, nonce: Data, ciphertext: Data) {
        self.version = version
        self.nonce = nonce
        self.ciphertext = ciphertext
    }

    private enum CodingKeys: String, CodingKey {
        case version, nonce, ciphertext
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        version = try container.decode(Int.self, forKey: .version)
        nonce = try PasswordsBase64.decode(container, key: .nonce)
        ciphertext = try PasswordsBase64.decode(container, key: .ciphertext)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(version, forKey: .version)
        try container.encode(nonce.base64EncodedString(), forKey: .nonce)
        try container.encode(ciphertext.base64EncodedString(), forKey: .ciphertext)
    }

    /// The JSON text that is stored and sent, keys in the order the format lists them.
    public var jsonText: String {
        "{\"version\":\(version),\"nonce\":\"\(nonce.base64EncodedString())\",\"ciphertext\":\"\(ciphertext.base64EncodedString())\"}"
    }

    public init(jsonText: String) throws {
        do {
            self = try JSONDecoder().decode(PasswordsSealedValue.self, from: Data(jsonText.utf8))
        } catch {
            throw PasswordsCryptoError.invalidSealedValue
        }
    }
}

/// PasswordsKeyPairSealedValue is a key sealed for a person's (or the
/// server's agent) key pair: an ephemeral public key, so only the holder of
/// the recipient's private key can open it, then a nonce and the ciphertext
/// with its tag. JSON: `{"version":1,"ephemeralPublicKey":..,"nonce":..,"ciphertext":..}`.
public struct PasswordsKeyPairSealedValue: Codable, Equatable, Sendable {
    public var version: Int
    /// The 65 byte uncompressed P-256 point the sender made for this one value.
    public var ephemeralPublicKey: Data
    public var nonce: Data
    public var ciphertext: Data

    public init(version: Int = PasswordsCrypto.sealedValueVersion, ephemeralPublicKey: Data, nonce: Data, ciphertext: Data) {
        self.version = version
        self.ephemeralPublicKey = ephemeralPublicKey
        self.nonce = nonce
        self.ciphertext = ciphertext
    }

    private enum CodingKeys: String, CodingKey {
        case version, ephemeralPublicKey, nonce, ciphertext
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        version = try container.decode(Int.self, forKey: .version)
        ephemeralPublicKey = try PasswordsBase64.decode(container, key: .ephemeralPublicKey)
        nonce = try PasswordsBase64.decode(container, key: .nonce)
        ciphertext = try PasswordsBase64.decode(container, key: .ciphertext)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(version, forKey: .version)
        try container.encode(ephemeralPublicKey.base64EncodedString(), forKey: .ephemeralPublicKey)
        try container.encode(nonce.base64EncodedString(), forKey: .nonce)
        try container.encode(ciphertext.base64EncodedString(), forKey: .ciphertext)
    }

    /// The JSON text that is stored and sent, keys in the order the format lists them.
    public var jsonText: String {
        "{\"version\":\(version),\"ephemeralPublicKey\":\"\(ephemeralPublicKey.base64EncodedString())\",\"nonce\":\"\(nonce.base64EncodedString())\",\"ciphertext\":\"\(ciphertext.base64EncodedString())\"}"
    }

    public init(jsonText: String) throws {
        do {
            self = try JSONDecoder().decode(PasswordsKeyPairSealedValue.self, from: Data(jsonText.utf8))
        } catch {
            throw PasswordsCryptoError.invalidSealedValue
        }
    }
}

/// PasswordsKeyPair is a person's P-256 key pair for sharing: the 32 byte
/// private scalar and the 65 byte uncompressed public point (04, X, Y).
public struct PasswordsKeyPair: Equatable, Sendable {
    public var privateKey: Data
    public var publicKey: Data

    public init(privateKey: Data, publicKey: Data) {
        self.privateKey = privateKey
        self.publicKey = publicKey
    }
}

/// PasswordsBase64 is the small amount of standard base64 the sealed values
/// need when they are decoded from JSON.
enum PasswordsBase64 {
    static func decode<Key: CodingKey>(_ container: KeyedDecodingContainer<Key>, key: Key) throws -> Data {
        let text = try container.decode(String.self, forKey: key)
        guard let data = Data(base64Encoded: text) else {
            throw DecodingError.dataCorruptedError(forKey: key, in: container, debugDescription: "not base64")
        }
        return data
    }
}

/// PasswordsCrypto is the format of the Passwords feature as functions. Keys
/// and secrets are plain `Data` of 32 bytes; nothing here keeps state.
public enum PasswordsCrypto {
    public static let keyByteCount = 32
    public static let nonceByteCount = 12
    public static let tagByteCount = 16
    public static let sealedValueVersion = 1
    public static let passphraseSaltByteCount = 16
    /// The PBKDF2 iteration count new setups use. A setup stores its own count,
    /// so this can be raised later.
    public static let passphraseIterationCount = 600_000
    /// The lowest iteration count `passphraseKey` accepts, the same as the Go package's.
    public static let minimumPassphraseIterationCount = 600_000
    /// The highest iteration count `passphraseKey` accepts, the same as the Go package's.
    public static let maximumPassphraseIterationCount = 10_000_000
    /// The fewest characters a passphrase has, on every device.
    public static let minimumPassphraseLength = 12
    public static let recoveryKeyByteCount = 32
    public static let publicKeyByteCount = 65
    public static let privateKeyByteCount = 32

    /// The recipient id of the server's agent key in a share's associated data.
    public static let agentRecipientId = "agent"

    private static let wrapKeyInfo = "teanode passwords wrap v1"
    private static let proofKeyInfo = "teanode passwords proof v1"
    private static let shareKeyInfo = "teanode passwords share v1"

    // MARK: Associated data

    /// Associated data of the account key sealed with the wrap key.
    public static let accountKeyAssociatedData = "account-key"

    /// Associated data of the private key sealed with the account key.
    public static let privateKeyAssociatedData = "private-key"

    /// Associated data of an item key sealed with the account key.
    public static func itemKeyAssociatedData(itemId: String) -> String {
        "item-key:\(itemId)"
    }

    /// Associated data of an item's fields sealed with its item key.
    public static func itemFieldsAssociatedData(itemId: String, itemKind: PasswordsItemKind) -> String {
        "item:\(itemId):\(itemKind.rawValue)"
    }

    /// Associated data of an item key sealed for a recipient: `share:<item id>:<owner id>:<recipient id>`,
    /// where the owner id is the user id of the person who owns the item, and
    /// the recipient id is the recipient's user id, or `agent` for the server's agent key.
    public static func shareAssociatedData(itemId: String, ownerId: String, recipientId: String) -> String {
        "share:\(itemId):\(ownerId):\(recipientId)"
    }

    // MARK: Randomness

    /// Random bytes from the system generator.
    public static func randomBytes(count: Int) -> Data {
        var generator = SystemRandomNumberGenerator()
        var bytes = Data(count: count)
        for index in 0..<count {
            bytes[index] = UInt8.random(in: UInt8.min...UInt8.max, using: &generator)
        }
        return bytes
    }

    /// A new 32 byte key: an account key, an item key or a recovery key's bytes.
    public static func makeKey() -> Data {
        randomBytes(count: keyByteCount)
    }

    // MARK: Sealed values

    /// Seals bytes with AES-256-GCM. The associated data is text that must be
    /// given again to open; it ties a value to what it is for. Pass `nonce`
    /// only in tests.
    public static func seal(_ plaintext: Data, key: Data, associatedData: String, nonce: Data? = nil) throws -> PasswordsSealedValue {
        let symmetricKey = try Self.symmetricKey(key)
        let nonceBytes = nonce ?? randomBytes(count: nonceByteCount)
        guard nonceBytes.count == nonceByteCount else { throw PasswordsCryptoError.invalidNonceLength }
        let box = try AES.GCM.seal(
            plaintext, using: symmetricKey, nonce: try AES.GCM.Nonce(data: nonceBytes), authenticating: Data(associatedData.utf8))
        var ciphertext = Data()
        ciphertext.append(box.ciphertext)
        ciphertext.append(box.tag)
        return PasswordsSealedValue(nonce: nonceBytes, ciphertext: ciphertext)
    }

    /// Opens a sealed value. Any failure (key, associated data or damage) is `openFailed`.
    public static func open(_ sealed: PasswordsSealedValue, key: Data, associatedData: String) throws -> Data {
        guard sealed.version == sealedValueVersion else { throw PasswordsCryptoError.unsupportedVersion }
        return try openBytes(nonce: sealed.nonce, ciphertext: sealed.ciphertext, key: key, associatedData: associatedData)
    }

    private static func openBytes(nonce: Data, ciphertext: Data, key: Data, associatedData: String) throws -> Data {
        let symmetricKey = try Self.symmetricKey(key)
        guard nonce.count == nonceByteCount, ciphertext.count >= tagByteCount else { throw PasswordsCryptoError.openFailed }
        let splitIndex = ciphertext.endIndex - tagByteCount
        do {
            let box = try AES.GCM.SealedBox(
                nonce: try AES.GCM.Nonce(data: nonce), ciphertext: ciphertext[ciphertext.startIndex..<splitIndex],
                tag: ciphertext[splitIndex...])
            return try AES.GCM.open(box, using: symmetricKey, authenticating: Data(associatedData.utf8))
        } catch {
            throw PasswordsCryptoError.openFailed
        }
    }

    private static func symmetricKey(_ key: Data) throws -> SymmetricKey {
        guard key.count == keyByteCount else { throw PasswordsCryptoError.invalidKeyLength }
        return SymmetricKey(data: key)
    }

    // MARK: Keys from a passphrase or a recovery key

    /// The passphrase key: PBKDF2-HMAC-SHA256 over the passphrase after
    /// Unicode NFC normalization, 32 bytes. This is slow on purpose (600,000
    /// iterations); the AutoFill extension must never call it. The count must
    /// be from `minimumPassphraseIterationCount` to `maximumPassphraseIterationCount`.
    public static func passphraseKey(passphrase: String, salt: Data, iterationCount: Int) throws -> Data {
        guard iterationCount >= minimumPassphraseIterationCount else { throw PasswordsCryptoError.iterationCountTooLow }
        guard iterationCount <= maximumPassphraseIterationCount else { throw PasswordsCryptoError.iterationCountTooHigh }
        return try derivePassphraseKey(passphrase: passphrase, salt: salt, iterationCount: iterationCount)
    }

    /// The derivation without the minimum, so the tests can check it against
    /// published vectors that use small counts. Nothing else calls it.
    static func derivePassphraseKey(passphrase: String, salt: Data, iterationCount: Int) throws -> Data {
        let passwordBytes = Array(passphrase.precomposedStringWithCanonicalMapping.utf8)
        // The count is a 32 bit number in CommonCrypto, and converting a bigger one traps.
        guard iterationCount > 0, iterationCount <= Int(UInt32.max) else { throw PasswordsCryptoError.keyDerivationFailed }
        #if canImport(CryptoKit)
            let passwordCharacters = passwordBytes.map { CChar(bitPattern: $0) }
            let saltBytes = [UInt8](salt)
            var derivedBytes = [UInt8](repeating: 0, count: keyByteCount)
            let status = CCKeyDerivationPBKDF(
                CCPBKDFAlgorithm(kCCPBKDF2), passwordCharacters, passwordCharacters.count, saltBytes, saltBytes.count,
                CCPseudoRandomAlgorithm(kCCPRFHmacAlgSHA256), UInt32(iterationCount), &derivedBytes, derivedBytes.count)
            guard status == Int32(kCCSuccess) else { throw PasswordsCryptoError.keyDerivationFailed }
            return Data(derivedBytes)
        #else
            do {
                let derived = try KDF.Insecure.PBKDF2.deriveKey(
                    from: passwordBytes, salt: salt, using: .sha256, outputByteCount: keyByteCount,
                    unsafeUncheckedRounds: iterationCount)
                return derived.withUnsafeBytes { Data($0) }
            } catch {
                throw PasswordsCryptoError.keyDerivationFailed
            }
        #endif
    }

    /// The key that seals the account key, from a 32 byte secret (the
    /// passphrase key, or a recovery key's bytes): HKDF-SHA256, empty salt.
    public static func wrapKey(secret: Data) throws -> Data {
        try hkdf(secret: secret, salt: Data(), info: wrapKeyInfo)
    }

    /// The proof a device sends to unlock, from the same secret as the wrap
    /// key. The server never gets the wrap key; it stores the proof's SHA-256.
    public static func proof(secret: Data) throws -> Data {
        try hkdf(secret: secret, salt: Data(), info: proofKeyInfo)
    }

    /// SHA-256 of the proof bytes, which is what the server stores and compares.
    public static func proofHash(proof: Data) -> Data {
        Data(SHA256.hash(data: proof))
    }

    private static func hkdf(secret: Data, salt: Data, info: String) throws -> Data {
        let derived = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: try symmetricKey(secret), salt: salt, info: Data(info.utf8), outputByteCount: keyByteCount)
        return derived.withUnsafeBytes { Data($0) }
    }

    // MARK: Recovery key

    /// A new recovery key's 32 bytes.
    public static func makeRecoveryKey() -> Data {
        randomBytes(count: recoveryKeyByteCount)
    }

    /// The recovery key as people see it: RFC 4648 base32 without padding in
    /// 13 groups of 4 letters, joined by single spaces.
    public static func formatRecoveryKey(_ recoveryKey: Data) throws -> String {
        guard recoveryKey.count == recoveryKeyByteCount else { throw PasswordsCryptoError.invalidRecoveryKey }
        let letters = Array(PasswordsBase32.encode(recoveryKey))
        var groups: [String] = []
        var start = 0
        while start < letters.count {
            let end = min(start + 4, letters.count)
            groups.append(String(letters[start..<end]))
            start = end
        }
        return groups.joined(separator: " ")
    }

    /// Reads a recovery key as the person typed it, ignoring spaces, tabs, line
    /// breaks, hyphens and the case of the letters A to Z. Any other character
    /// is refused (so a padding sign, a digit base32 does not use, or a letter
    /// some languages turn into another, such as a sharp s, is not quietly
    /// accepted), and so is a last letter whose unused bits are not zero. Every
    /// implementation refuses the same texts (`recoveryKeyInvalid` in the test vectors).
    public static func parseRecoveryKey(_ text: String) throws -> Data {
        var letters = ""
        for byte in text.utf8 {
            switch byte {
            case 0x20, 0x09, 0x0A, 0x0D, 0x2D:
                continue
            case UInt8(ascii: "a")...UInt8(ascii: "z"):
                letters.unicodeScalars.append(Unicode.Scalar(byte - UInt8(ascii: "a") + UInt8(ascii: "A")))
            case UInt8(ascii: "A")...UInt8(ascii: "Z"), UInt8(ascii: "2")...UInt8(ascii: "7"):
                letters.unicodeScalars.append(Unicode.Scalar(byte))
            default:
                throw PasswordsCryptoError.invalidRecoveryKey
            }
        }
        // Writing the bytes back refuses a last letter whose unused bits are not zero.
        guard letters.utf8.count == PasswordsBase32.encodedLength(byteCount: recoveryKeyByteCount),
            let bytes = PasswordsBase32.decode(letters), bytes.count == recoveryKeyByteCount,
            PasswordsBase32.encode(bytes) == letters
        else { throw PasswordsCryptoError.invalidRecoveryKey }
        return bytes
    }

    // MARK: Person key pair

    /// A new P-256 key pair for sharing.
    public static func makeKeyPair() -> PasswordsKeyPair {
        let privateKey = P256.KeyAgreement.PrivateKey()
        return PasswordsKeyPair(privateKey: privateKey.rawRepresentation, publicKey: privateKey.publicKey.x963Representation)
    }

    /// The key pair for a stored 32 byte private scalar.
    public static func keyPair(privateKey: Data) throws -> PasswordsKeyPair {
        let key = try agreementPrivateKey(privateKey)
        return PasswordsKeyPair(privateKey: key.rawRepresentation, publicKey: key.publicKey.x963Representation)
    }

    private static func agreementPrivateKey(_ scalar: Data) throws -> P256.KeyAgreement.PrivateKey {
        do {
            return try P256.KeyAgreement.PrivateKey(rawRepresentation: scalar)
        } catch {
            throw PasswordsCryptoError.invalidKeyPair
        }
    }

    private static func agreementPublicKey(_ point: Data) throws -> P256.KeyAgreement.PublicKey {
        do {
            return try P256.KeyAgreement.PublicKey(x963Representation: point)
        } catch {
            throw PasswordsCryptoError.invalidKeyPair
        }
    }

    /// The code two people compare out loud to be sure a public key is the
    /// one they think: SHA-256 of the 65 public key bytes, bytes 0 to 9 as five
    /// big-endian 16 bit numbers, each modulo 10000, as four digits joined by
    /// spaces ("0412 9930 0007 5121 8800").
    public static func safetyCode(publicKey: Data) throws -> String {
        guard publicKey.count == publicKeyByteCount else { throw PasswordsCryptoError.invalidKeyPair }
        let digest = Array(SHA256.hash(data: publicKey))
        var groups: [String] = []
        for index in 0..<5 {
            let number = (Int(digest[index * 2]) << 8 | Int(digest[index * 2 + 1])) % 10000
            let digits = String(number)
            groups.append(String(repeating: "0", count: 4 - digits.count) + digits)
        }
        return groups.joined(separator: " ")
    }

    // MARK: Sealed for a key pair

    /// The X coordinate of ECDH(private key, public key), 32 bytes.
    public static func sharedSecret(privateKey: Data, publicKey: Data) throws -> Data {
        let secret = try agreementPrivateKey(privateKey).sharedSecretFromKeyAgreement(with: try agreementPublicKey(publicKey))
        return secret.withUnsafeBytes { Data($0) }
    }

    /// Whether the bytes are a public key: 65 bytes, starting with 04, that are a point on the P-256 curve.
    public static func isPublicKeyValid(_ publicKey: Data) -> Bool {
        publicKey.count == publicKeyByteCount && publicKey.first == 0x04 && (try? agreementPublicKey(publicKey)) != nil
    }

    /// The key an item key is sealed with for a recipient: HKDF-SHA256 of the
    /// ephemeral secret followed by the owner secret, the recipient's 65 public
    /// key bytes as salt.
    public static func shareKey(ephemeralSharedSecret: Data, ownerSharedSecret: Data, recipientPublicKey: Data) throws -> Data {
        guard ephemeralSharedSecret.count == keyByteCount, ownerSharedSecret.count == keyByteCount else {
            throw PasswordsCryptoError.invalidKeyLength
        }
        let derived = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: SymmetricKey(data: ephemeralSharedSecret + ownerSharedSecret), salt: recipientPublicKey,
            info: Data(shareKeyInfo.utf8), outputByteCount: keyByteCount)
        return derived.withUnsafeBytes { Data($0) }
    }

    /// Seals bytes (an item key) so only the holder of the recipient's private
    /// key can open them, and only when they came from the owner. The key mixes
    /// two secrets: one from a fresh ephemeral key and one from the owner's own
    /// private key, so only a holder of the owner's private key can make a
    /// share that opens (the server cannot plant an item "shared by" somebody).
    /// `ownerId` is the owner's user id and `recipientId` the recipient's, or
    /// `agentRecipientId` for the server's agent key. Pass the ephemeral key and
    /// nonce only in tests.
    public static func sealForKeyPair(
        _ plaintext: Data, ownerKeyPair: PasswordsKeyPair, ownerId: String, recipientPublicKey: Data, recipientId: String, itemId: String,
        ephemeralPrivateKey: Data? = nil, nonce: Data? = nil
    ) throws -> PasswordsKeyPairSealedValue {
        let ephemeralKeyPair = try ephemeralPrivateKey.map { try keyPair(privateKey: $0) } ?? makeKeyPair()
        let ephemeralShared = try sharedSecret(privateKey: ephemeralKeyPair.privateKey, publicKey: recipientPublicKey)
        let ownerShared = try sharedSecret(privateKey: ownerKeyPair.privateKey, publicKey: recipientPublicKey)
        let key = try shareKey(
            ephemeralSharedSecret: ephemeralShared, ownerSharedSecret: ownerShared, recipientPublicKey: recipientPublicKey)
        let sealed = try seal(
            plaintext, key: key, associatedData: shareAssociatedData(itemId: itemId, ownerId: ownerId, recipientId: recipientId),
            nonce: nonce)
        return PasswordsKeyPairSealedValue(
            ephemeralPublicKey: ephemeralKeyPair.publicKey, nonce: sealed.nonce, ciphertext: sealed.ciphertext)
    }

    /// Opens a value sealed for this key pair by the owner whose public key is
    /// `ownerPublicKey`. A value sealed by anybody else (the server, say) does
    /// not open. Any failure is `openFailed`, except a version this build does not know.
    public static func openForKeyPair(
        _ sealed: PasswordsKeyPairSealedValue, recipientPrivateKey: Data, ownerPublicKey: Data, ownerId: String, recipientId: String,
        itemId: String
    ) throws -> Data {
        guard sealed.version == sealedValueVersion else { throw PasswordsCryptoError.unsupportedVersion }
        let recipientPublicKey = try keyPair(privateKey: recipientPrivateKey).publicKey
        guard isPublicKeyValid(ownerPublicKey), isPublicKeyValid(sealed.ephemeralPublicKey),
            let ephemeralShared = try? sharedSecret(privateKey: recipientPrivateKey, publicKey: sealed.ephemeralPublicKey),
            let ownerShared = try? sharedSecret(privateKey: recipientPrivateKey, publicKey: ownerPublicKey)
        else { throw PasswordsCryptoError.openFailed }
        let key = try shareKey(
            ephemeralSharedSecret: ephemeralShared, ownerSharedSecret: ownerShared, recipientPublicKey: recipientPublicKey)
        return try openBytes(
            nonce: sealed.nonce, ciphertext: sealed.ciphertext, key: key,
            associatedData: shareAssociatedData(itemId: itemId, ownerId: ownerId, recipientId: recipientId))
    }

    // MARK: Items

    /// Seals an item's fields with its item key.
    public static func sealItemFields(_ fields: PasswordsItemFields, itemKey: Data, itemId: String, nonce: Data? = nil) throws
        -> PasswordsSealedValue
    {
        try seal(
            try fields.jsonData(), key: itemKey,
            associatedData: itemFieldsAssociatedData(itemId: itemId, itemKind: fields.itemKind), nonce: nonce)
    }

    /// Opens an item's fields. The kind is part of the associated data, so a
    /// value cannot be opened as another kind.
    public static func openItemFields(_ sealed: PasswordsSealedValue, itemKey: Data, itemId: String, itemKind: PasswordsItemKind) throws
        -> PasswordsItemFields
    {
        let plaintext = try open(
            sealed, key: itemKey, associatedData: itemFieldsAssociatedData(itemId: itemId, itemKind: itemKind))
        return try PasswordsItemFields(itemKind: itemKind, jsonData: plaintext)
    }
}

/// PasswordsBase32 is RFC 4648 base32, written by hand because neither
/// Foundation nor CryptoKit has it. It is used for recovery keys and for the
/// secrets in one-time code links.
enum PasswordsBase32 {
    private static let alphabet = Array("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")

    /// The letters for bytes, without padding.
    static func encode(_ bytes: Data) -> String {
        var letters = ""
        var buffer = 0
        var bufferBitCount = 0
        for byte in bytes {
            buffer = (buffer << 8) | Int(byte)
            bufferBitCount += 8
            while bufferBitCount >= 5 {
                letters.append(alphabet[(buffer >> (bufferBitCount - 5)) & 31])
                bufferBitCount -= 5
            }
            buffer &= (1 << bufferBitCount) - 1
        }
        if bufferBitCount > 0 {
            letters.append(alphabet[(buffer << (5 - bufferBitCount)) & 31])
        }
        return letters
    }

    /// How many letters `byteCount` bytes make without padding.
    static func encodedLength(byteCount: Int) -> Int {
        (byteCount * 8 + 4) / 5
    }

    /// The bytes for letters, ignoring case, spaces and `=` padding; nil for any other character.
    /// Bits left over at the end (fewer than 8) are dropped.
    static func decode(_ text: String) -> Data? {
        var bytes = Data()
        var buffer = 0
        var bufferBitCount = 0
        for character in text.uppercased() {
            if character == "=" || character == " " { continue }
            guard let value = alphabet.firstIndex(of: character) else { return nil }
            buffer = (buffer << 5) | value
            bufferBitCount += 5
            if bufferBitCount >= 8 {
                bytes.append(UInt8((buffer >> (bufferBitCount - 8)) & 0xFF))
                bufferBitCount -= 8
                buffer &= (1 << bufferBitCount) - 1
            }
        }
        return bytes
    }
}
