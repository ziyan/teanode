import Foundation

#if canImport(CryptoKit)
    import CryptoKit
#else
    import Crypto
#endif

// A passkey authenticator that holds its keys in an item's fields. ES256 only
// (ECDSA over P-256 with SHA-256, COSE algorithm -7). Everything is written to
// match the Go package internal/passwords and web/src/passwords/ byte for byte
// (docs/planning/passwords-execplan.md, "Passkeys"); the CBOR is written by
// hand because the pieces are small and fixed.
//
// A provider can only hold synced passkeys, so the flags say backup eligible
// and backed up, the sign count is always 0, and the attestation is "none".

/// PasswordsPasskeyError is why a passkey call failed.
public enum PasswordsPasskeyError: Error, Equatable, Sendable {
    /// The private key is not ES256 PKCS#8 (or SEC1) DER.
    case invalidPrivateKey
    /// A public key coordinate was not 32 bytes, or is not a point on the curve.
    case invalidPublicKey
    /// A credential id that does not fit the 2 byte length in the authenticator data.
    case invalidCredentialId
    /// A signature that is not DER ECDSA.
    case invalidSignature
}

/// PasswordsPasskeyRegistration is everything a new passkey produces: the
/// private key to keep in the login, and what the website is sent.
public struct PasswordsPasskeyRegistration: Equatable, Sendable {
    /// The private key as PKCS#8 DER, to keep (as base64) in the login's passkeys.
    public var privateKey: Data
    public var credentialId: Data
    public var publicKeyX: Data
    public var publicKeyY: Data
    /// The public key as a COSE key, CBOR encoded.
    public var cosePublicKey: Data
    public var authenticatorData: Data
    /// The attestation object, CBOR encoded, with format "none".
    public var attestationObject: Data
}

/// PasswordsPasskey is the passkey authenticator as functions.
public enum PasswordsPasskey {
    /// COSE algorithm identifier of ES256.
    public static let coseAlgorithm = -7

    /// TeaNode's authenticator id: the ASCII of "TEANODEPASSWORDS".
    public static let aaguid = Data("TEANODEPASSWORDS".utf8)

    public static let credentialIdByteCount = 16

    // Flag bits of the authenticator data.
    private static let userPresentFlag: UInt8 = 0x01
    private static let userVerifiedFlag: UInt8 = 0x04
    private static let backupEligibleFlag: UInt8 = 0x08
    private static let backedUpFlag: UInt8 = 0x10
    private static let attestedCredentialDataFlag: UInt8 = 0x40

    // MARK: Keys

    /// A new passkey private key, as PKCS#8 DER.
    public static func makePrivateKey() -> Data {
        let key = P256.Signing.PrivateKey()
        return pkcs8(scalar: key.rawRepresentation, point: key.publicKey.x963Representation)
    }

    /// A new random credential id of 16 bytes.
    public static func makeCredentialId() -> Data {
        PasswordsCrypto.randomBytes(count: credentialIdByteCount)
    }

    /// The public key's coordinates, 32 bytes each, from a private key.
    public static func publicKeyCoordinates(privateKey: Data) throws -> (x: Data, y: Data) {
        let point = try signingKey(privateKey).publicKey.x963Representation
        return (x: point.subdata(in: 1..<33), y: point.subdata(in: 33..<65))
    }

    // MARK: Registration

    /// The COSE public key: the CBOR map `{1: 2, 3: -7, -1: 1, -2: X, -3: Y}`
    /// with its keys in the order 1, 3, -1, -2, -3.
    public static func cosePublicKey(publicKeyX: Data, publicKeyY: Data) throws -> Data {
        guard publicKeyX.count == 32, publicKeyY.count == 32 else { throw PasswordsPasskeyError.invalidPublicKey }
        var encoded = Data([0xA5])  // map of 5
        encoded.append(contentsOf: [0x01, 0x02])  // kty: EC2
        encoded.append(contentsOf: [0x03, 0x26])  // alg: -7
        encoded.append(contentsOf: [0x20, 0x01])  // crv: P-256
        encoded.append(contentsOf: [0x21, 0x58, 0x20])  // x: byte string of 32
        encoded.append(publicKeyX)
        encoded.append(contentsOf: [0x22, 0x58, 0x20])  // y: byte string of 32
        encoded.append(publicKeyY)
        return encoded
    }

    /// The authenticator data of a registration: SHA-256 of the relying party id,
    /// flags, sign count 0, the AAGUID, the credential id (with a 2 byte length)
    /// and the COSE public key. Flags are 0x5D (user present, verified, backup
    /// eligible, backed up, attested data) when the person was verified, else 0x59.
    public static func registrationAuthenticatorData(
        relyingPartyId: String, credentialId: Data, publicKeyX: Data, publicKeyY: Data, isVerified: Bool
    ) throws -> Data {
        guard !credentialId.isEmpty, credentialId.count <= 1023 else { throw PasswordsPasskeyError.invalidCredentialId }
        var flags = userPresentFlag | backupEligibleFlag | backedUpFlag | attestedCredentialDataFlag
        if isVerified { flags |= userVerifiedFlag }
        var data = relyingPartyHash(relyingPartyId)
        data.append(flags)
        data.append(contentsOf: [0, 0, 0, 0])  // sign count
        data.append(aaguid)
        data.append(contentsOf: [UInt8(credentialId.count >> 8), UInt8(credentialId.count & 0xFF)])
        data.append(credentialId)
        data.append(try cosePublicKey(publicKeyX: publicKeyX, publicKeyY: publicKeyY))
        return data
    }

    /// The attestation object: the CBOR map `{"fmt": "none", "attStmt": {}, "authData": <bytes>}`
    /// in that key order.
    public static func attestationObject(authenticatorData: Data) -> Data {
        var encoded = Data([0xA3])  // map of 3
        encoded.append(cborText("fmt"))
        encoded.append(cborText("none"))
        encoded.append(cborText("attStmt"))
        encoded.append(0xA0)  // empty map
        encoded.append(cborText("authData"))
        encoded.append(cborBytesHeader(count: authenticatorData.count))
        encoded.append(authenticatorData)
        return encoded
    }

    /// Makes a passkey for a relying party. Pass `privateKey` and `credentialId`
    /// only in tests.
    public static func register(
        relyingPartyId: String, isVerified: Bool, privateKey: Data? = nil, credentialId: Data? = nil
    ) throws -> PasswordsPasskeyRegistration {
        let key = privateKey ?? makePrivateKey()
        let identifier = credentialId ?? makeCredentialId()
        let coordinates = try publicKeyCoordinates(privateKey: key)
        let authenticatorData = try registrationAuthenticatorData(
            relyingPartyId: relyingPartyId, credentialId: identifier, publicKeyX: coordinates.x, publicKeyY: coordinates.y,
            isVerified: isVerified)
        return PasswordsPasskeyRegistration(
            privateKey: key, credentialId: identifier, publicKeyX: coordinates.x, publicKeyY: coordinates.y,
            cosePublicKey: try cosePublicKey(publicKeyX: coordinates.x, publicKeyY: coordinates.y),
            authenticatorData: authenticatorData, attestationObject: attestationObject(authenticatorData: authenticatorData))
    }

    // MARK: Sign-in

    /// The authenticator data of a sign-in: SHA-256 of the relying party id,
    /// flags (0x1D verified, 0x19 not) and sign count 0.
    public static func assertionAuthenticatorData(relyingPartyId: String, isVerified: Bool) -> Data {
        var flags = userPresentFlag | backupEligibleFlag | backedUpFlag
        if isVerified { flags |= userVerifiedFlag }
        var data = relyingPartyHash(relyingPartyId)
        data.append(flags)
        data.append(contentsOf: [0, 0, 0, 0])  // sign count
        return data
    }

    /// The sign-in signature: ECDSA P-256 over SHA-256 of the authenticator data
    /// followed by the client data hash, DER encoded. ECDSA signatures are
    /// randomized, so two calls differ; both verify.
    public static func sign(privateKey: Data, authenticatorData: Data, clientDataHash: Data) throws -> Data {
        let signature = try signingKey(privateKey).signature(for: authenticatorData + clientDataHash)
        return signature.derRepresentation
    }

    /// Checks a sign-in signature against a public key.
    public static func isSignatureValid(
        signature: Data, publicKeyX: Data, publicKeyY: Data, authenticatorData: Data, clientDataHash: Data
    ) throws -> Bool {
        guard publicKeyX.count == 32, publicKeyY.count == 32 else { throw PasswordsPasskeyError.invalidPublicKey }
        let publicKey: P256.Signing.PublicKey
        do {
            publicKey = try P256.Signing.PublicKey(x963Representation: Data([0x04]) + publicKeyX + publicKeyY)
        } catch {
            throw PasswordsPasskeyError.invalidPublicKey
        }
        let parsedSignature: P256.Signing.ECDSASignature
        do {
            parsedSignature = try P256.Signing.ECDSASignature(derRepresentation: signature)
        } catch {
            throw PasswordsPasskeyError.invalidSignature
        }
        return publicKey.isValidSignature(parsedSignature, for: authenticatorData + clientDataHash)
    }

    // MARK: Pieces

    private static func relyingPartyHash(_ relyingPartyId: String) -> Data {
        Data(SHA256.hash(data: Data(relyingPartyId.utf8)))
    }

    private static func cborText(_ text: String) -> Data {
        // Every text here is shorter than 24 bytes, so the length is in the first byte.
        Data([0x60 | UInt8(text.utf8.count)]) + Data(text.utf8)
    }

    private static func cborBytesHeader(count: Int) -> Data {
        switch count {
        case 0..<24: return Data([0x40 | UInt8(count)])
        case 24..<256: return Data([0x58, UInt8(count)])
        default: return Data([0x59, UInt8(count >> 8), UInt8(count & 0xFF)])
        }
    }

    private static func signingKey(_ privateKey: Data) throws -> P256.Signing.PrivateKey {
        guard let scalar = scalar(fromDER: privateKey) else { throw PasswordsPasskeyError.invalidPrivateKey }
        do {
            return try P256.Signing.PrivateKey(rawRepresentation: scalar)
        } catch {
            throw PasswordsPasskeyError.invalidPrivateKey
        }
    }

    // The private key travels as PKCS#8 DER in the exact shape Go's
    // x509.MarshalPKCS8PrivateKey writes for P-256 (138 bytes: the algorithm
    // identifiers, then an ECPrivateKey holding the scalar and the public
    // point). It is written and read here by hand so that no platform's own
    // DER output matters.
    private static let pkcs8Prefix = Data([
        0x30, 0x81, 0x87, 0x02, 0x01, 0x00, 0x30, 0x13, 0x06, 0x07, 0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x02, 0x01, 0x06, 0x08, 0x2A, 0x86,
        0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07, 0x04, 0x6D, 0x30, 0x6B, 0x02, 0x01, 0x01, 0x04, 0x20,
    ])
    private static let pkcs8PublicKeyPrefix = Data([0xA1, 0x44, 0x03, 0x42, 0x00])

    static func pkcs8(scalar: Data, point: Data) -> Data {
        pkcs8Prefix + scalar + pkcs8PublicKeyPrefix + point
    }

    /// The 32 byte scalar inside PKCS#8 (or bare SEC1) ECDSA P-256 DER, or nil.
    static func scalar(fromDER der: Data) -> Data? {
        var outer = DERReader(bytes: Array(der))
        guard var sequence = outer.readNested(tag: 0x30), outer.isAtEnd, let version = sequence.readContents(tag: 0x02),
            version.count == 1
        else { return nil }
        if version[0] == 1 {
            // SEC1 ECPrivateKey: the scalar follows the version.
            return privateKeyScalar(&sequence)
        }
        guard version[0] == 0 else { return nil }
        // PKCS#8: AlgorithmIdentifier { id-ecPublicKey, prime256v1 }, then the ECPrivateKey in an OCTET STRING.
        guard var algorithm = sequence.readNested(tag: 0x30), let keyType = algorithm.readContents(tag: 0x06),
            let curve = algorithm.readContents(tag: 0x06), keyType == [0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x02, 0x01],
            curve == [0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07], var wrapper = sequence.readNested(tag: 0x04),
            var privateKey = wrapper.readNested(tag: 0x30), let innerVersion = privateKey.readContents(tag: 0x02), innerVersion == [1]
        else { return nil }
        return privateKeyScalar(&privateKey)
    }

    private static func privateKeyScalar(_ sequence: inout DERReader) -> Data? {
        guard let scalar = sequence.readContents(tag: 0x04), scalar.count == 32 else { return nil }
        return Data(scalar)
    }
}

/// DERReader walks a DER byte string one element at a time. It reads only the
/// definite lengths ASN.1 keys use, and gives up (nil) on anything else.
private struct DERReader {
    let bytes: [UInt8]
    var position = 0

    init(bytes: [UInt8]) {
        self.bytes = bytes
    }

    var isAtEnd: Bool { position >= bytes.count }

    /// Reads the next element if it has the tag; its contents are returned as the bytes.
    mutating func readContents(tag: UInt8) -> [UInt8]? {
        guard position + 2 <= bytes.count, bytes[position] == tag else { return nil }
        var length = Int(bytes[position + 1])
        var contentStart = position + 2
        if length & 0x80 != 0 {
            let lengthByteCount = length & 0x7F
            guard lengthByteCount >= 1, lengthByteCount <= 2, contentStart + lengthByteCount <= bytes.count else { return nil }
            length = 0
            for index in 0..<lengthByteCount {
                length = (length << 8) | Int(bytes[contentStart + index])
            }
            contentStart += lengthByteCount
        }
        guard contentStart + length <= bytes.count else { return nil }
        position = contentStart + length
        return Array(bytes[contentStart..<contentStart + length])
    }

    /// Reads the next element if it has the tag, as a reader over its contents.
    mutating func readNested(tag: UInt8) -> DERReader? {
        readContents(tag: tag).map { DERReader(bytes: $0) }
    }
}
