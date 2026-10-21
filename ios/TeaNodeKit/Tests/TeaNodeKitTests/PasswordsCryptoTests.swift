import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsCryptoTests cover the encryption format on its own: round trips,
/// every way an open must fail, and values worked out by other implementations
/// (RFC 7914 and RFC 4648). PasswordsVectorsTests checks the bytes against Go.
final class PasswordsCryptoTests: XCTestCase {
    private func hex(_ text: String) -> Data {
        var bytes = Data()
        var index = text.startIndex
        while index < text.endIndex {
            let next = text.index(index, offsetBy: 2)
            bytes.append(UInt8(text[index..<next], radix: 16)!)
            index = next
        }
        return bytes
    }

    private let key = Data((0..<32).map { UInt8($0) })
    private let nonce = Data((100..<112).map { UInt8($0) })

    // MARK: Sealed values

    func testSealThenOpenReturnsThePlaintext() throws {
        let sealed = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "account-key")
        XCTAssertEqual(sealed.version, 1)
        XCTAssertEqual(sealed.nonce.count, 12)
        XCTAssertEqual(sealed.ciphertext.count, 5 + 16)
        XCTAssertEqual(try PasswordsCrypto.open(sealed, key: key, associatedData: "account-key"), Data("hello".utf8))
    }

    func testSealWithAGivenNonceIsRepeatable() throws {
        let first = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "x", nonce: nonce)
        let second = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "x", nonce: nonce)
        XCTAssertEqual(first, second)
        XCTAssertEqual(first.nonce, nonce)
    }

    func testTwoSealsWithoutANonceUseDifferentNonces() throws {
        let first = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "x")
        let second = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "x")
        XCTAssertNotEqual(first.nonce, second.nonce)
    }

    func testOpenFailsWithTheWrongKeyAssociatedDataOrDamage() throws {
        let sealed = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "item-key:one")
        XCTAssertThrowsError(try PasswordsCrypto.open(sealed, key: Data(repeating: 9, count: 32), associatedData: "item-key:one")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .openFailed)
        }
        XCTAssertThrowsError(try PasswordsCrypto.open(sealed, key: key, associatedData: "item-key:two")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .openFailed)
        }
        var damaged = sealed
        damaged.ciphertext[0] ^= 1
        XCTAssertThrowsError(try PasswordsCrypto.open(damaged, key: key, associatedData: "item-key:one"))
        var tooShort = sealed
        tooShort.ciphertext = Data(repeating: 0, count: 5)
        XCTAssertThrowsError(try PasswordsCrypto.open(tooShort, key: key, associatedData: "item-key:one")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .openFailed)
        }
    }

    func testOpenRefusesAnUnknownVersion() throws {
        var sealed = try PasswordsCrypto.seal(Data("hello".utf8), key: key, associatedData: "x")
        sealed.version = 2
        XCTAssertThrowsError(try PasswordsCrypto.open(sealed, key: key, associatedData: "x")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .unsupportedVersion)
        }
    }

    func testSealRefusesAKeyOrNonceOfTheWrongLength() {
        XCTAssertThrowsError(try PasswordsCrypto.seal(Data(), key: Data(count: 16), associatedData: "x")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .invalidKeyLength)
        }
        XCTAssertThrowsError(try PasswordsCrypto.seal(Data(), key: key, associatedData: "x", nonce: Data(count: 8))) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .invalidNonceLength)
        }
    }

    func testAnEmptyPlaintextSealsAndOpens() throws {
        let sealed = try PasswordsCrypto.seal(Data(), key: key, associatedData: "x")
        XCTAssertEqual(sealed.ciphertext.count, 16)
        XCTAssertEqual(try PasswordsCrypto.open(sealed, key: key, associatedData: "x"), Data())
    }

    func testSealedValueJSONKeepsTheDocumentedKeyOrderAndRoundTrips() throws {
        let sealed = PasswordsSealedValue(nonce: Data([1, 2, 3]), ciphertext: Data([4, 5, 6, 7]))
        XCTAssertEqual(sealed.jsonText, "{\"version\":1,\"nonce\":\"AQID\",\"ciphertext\":\"BAUGBw==\"}")
        XCTAssertEqual(try PasswordsSealedValue(jsonText: sealed.jsonText), sealed)
        let encoded = try JSONEncoder().encode(sealed)
        XCTAssertEqual(try JSONDecoder().decode(PasswordsSealedValue.self, from: encoded), sealed)
    }

    func testSealedValueJSONRefusesGarbage() {
        XCTAssertThrowsError(try PasswordsSealedValue(jsonText: "not json")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .invalidSealedValue)
        }
        XCTAssertThrowsError(try PasswordsSealedValue(jsonText: "{\"version\":1,\"nonce\":\"%%%\",\"ciphertext\":\"AA==\"}"))
    }

    // MARK: Passphrase key

    func testPassphraseKeyMatchesRFC7914Vectors() throws {
        XCTAssertEqual(
            try PasswordsCrypto.derivePassphraseKey(passphrase: "passwd", salt: Data("salt".utf8), iterationCount: 1),
            hex("55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc"))
        XCTAssertEqual(
            try PasswordsCrypto.derivePassphraseKey(passphrase: "password", salt: Data("salt".utf8), iterationCount: 4096),
            hex("c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"))
    }

    func testPassphraseKeyNormalizesToNFC() throws {
        let decomposed = "caf\u{65}\u{301}"
        let composed = "caf\u{E9}"
        XCTAssertNotEqual(Array(decomposed.utf8), Array(composed.utf8))
        let salt = Data(repeating: 7, count: 16)
        XCTAssertEqual(
            try PasswordsCrypto.passphraseKey(passphrase: decomposed, salt: salt, iterationCount: PasswordsCrypto.minimumPassphraseIterationCount),
            try PasswordsCrypto.passphraseKey(passphrase: composed, salt: salt, iterationCount: PasswordsCrypto.minimumPassphraseIterationCount))
    }

    func testPassphraseKeyDependsOnSaltAndCount() throws {
        let first = try PasswordsCrypto.passphraseKey(passphrase: "a", salt: Data(repeating: 1, count: 16), iterationCount: PasswordsCrypto.minimumPassphraseIterationCount)
        let otherSalt = try PasswordsCrypto.passphraseKey(passphrase: "a", salt: Data(repeating: 2, count: 16), iterationCount: PasswordsCrypto.minimumPassphraseIterationCount)
        let otherCount = try PasswordsCrypto.passphraseKey(passphrase: "a", salt: Data(repeating: 1, count: 16), iterationCount: PasswordsCrypto.minimumPassphraseIterationCount + 1)
        XCTAssertEqual(first.count, 32)
        XCTAssertNotEqual(first, otherSalt)
        XCTAssertNotEqual(first, otherCount)
    }

    func testPassphraseKeyRefusesACountOutsideTheBounds() {
        for count in [0, 1, 599_999, -5] {
            XCTAssertThrowsError(try PasswordsCrypto.passphraseKey(passphrase: "a", salt: Data(), iterationCount: count)) { error in
                XCTAssertEqual(error as? PasswordsCryptoError, .iterationCountTooLow)
            }
        }
        // Counts above the 32 bit range must be refused, not trap.
        for count in [10_000_001, Int(UInt32.max) + 1, Int.max] {
            XCTAssertThrowsError(try PasswordsCrypto.passphraseKey(passphrase: "a", salt: Data(), iterationCount: count)) { error in
                XCTAssertEqual(error as? PasswordsCryptoError, .iterationCountTooHigh)
            }
        }
        XCTAssertThrowsError(try PasswordsCrypto.derivePassphraseKey(passphrase: "a", salt: Data(), iterationCount: Int(UInt32.max) + 1)) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .keyDerivationFailed)
        }
        XCTAssertEqual(PasswordsCrypto.minimumPassphraseLength, 12)
    }

    func testWrapAndProofKeysDifferAndProofHashIsSHA256() throws {
        let wrap = try PasswordsCrypto.wrapKey(secret: key)
        let proof = try PasswordsCrypto.proof(secret: key)
        XCTAssertEqual(wrap.count, 32)
        XCTAssertEqual(proof.count, 32)
        XCTAssertNotEqual(wrap, proof)
        XCTAssertNotEqual(wrap, key)
        XCTAssertEqual(wrap, try PasswordsCrypto.wrapKey(secret: key))
        // SHA-256 of 32 zero bytes.
        XCTAssertEqual(
            PasswordsCrypto.proofHash(proof: Data(count: 32)),
            hex("66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925"))
        XCTAssertThrowsError(try PasswordsCrypto.wrapKey(secret: Data(count: 5)))
    }

    // MARK: Recovery key

    func testBase32MatchesRFC4648() {
        let vectors = [("", ""), ("f", "MY"), ("fo", "MZXQ"), ("foo", "MZXW6"), ("foob", "MZXW6YQ"), ("fooba", "MZXW6YTB"), ("foobar", "MZXW6YTBOI")]
        for (text, expected) in vectors {
            XCTAssertEqual(PasswordsBase32.encode(Data(text.utf8)), expected)
            XCTAssertEqual(PasswordsBase32.decode(expected), Data(text.utf8))
        }
        XCTAssertEqual(PasswordsBase32.decode("mzxw6ytboi======"), Data("foobar".utf8))
        XCTAssertNil(PasswordsBase32.decode("MZXW1"))
    }

    func testRecoveryKeyIsThirteenGroupsOfFourAndReadsBack() throws {
        let bytes = PasswordsCrypto.makeRecoveryKey()
        XCTAssertEqual(bytes.count, 32)
        let text = try PasswordsCrypto.formatRecoveryKey(bytes)
        let groups = text.split(separator: " ")
        XCTAssertEqual(groups.count, 13)
        XCTAssertTrue(groups.allSatisfy { $0.count == 4 })
        XCTAssertEqual(try PasswordsCrypto.parseRecoveryKey(text), bytes)
        XCTAssertEqual(try PasswordsCrypto.parseRecoveryKey(text.lowercased().replacingOccurrences(of: " ", with: "-")), bytes)
        XCTAssertEqual(try PasswordsCrypto.parseRecoveryKey("  " + text + "\n"), bytes)
    }

    func testRecoveryKeyRefusesTheWrongLengthOrLetters() {
        XCTAssertThrowsError(try PasswordsCrypto.formatRecoveryKey(Data(count: 31)))
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey("MZXW 6YTB")) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .invalidRecoveryKey)
        }
        let valid = try! PasswordsCrypto.formatRecoveryKey(Data(count: 32))
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey(String(valid.dropLast()) + "1"))
    }

    func testRecoveryKeyRefusesPaddingNonAsciiAndNonZeroTrailingBits() throws {
        let valid = try PasswordsCrypto.formatRecoveryKey(Data(count: 32))
        XCTAssertEqual(try PasswordsCrypto.parseRecoveryKey(valid), Data(count: 32))
        let letters = valid.replacingOccurrences(of: " ", with: "")
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey(letters + "===="))
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey(String(letters.dropLast(1)) + "B"))  // unused bits set
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey("\u{FF21}" + letters.dropFirst()))  // full width A
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey("\u{DF}" + letters.dropFirst()))  // sharp s
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey(letters.prefix(4) + "\u{A0}" + letters.dropFirst(4)))  // no-break space
        XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey(letters.prefix(4) + "\u{2013}" + letters.dropFirst(4)))  // en dash
    }

    // MARK: Key pairs and safety codes

    func testKeyPairHasA32ByteScalarAnd65BytePoint() throws {
        let keyPair = PasswordsCrypto.makeKeyPair()
        XCTAssertEqual(keyPair.privateKey.count, 32)
        XCTAssertEqual(keyPair.publicKey.count, 65)
        XCTAssertEqual(keyPair.publicKey[0], 4)
        XCTAssertEqual(try PasswordsCrypto.keyPair(privateKey: keyPair.privateKey), keyPair)
        XCTAssertThrowsError(try PasswordsCrypto.keyPair(privateKey: Data(count: 32))) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .invalidKeyPair)
        }
    }

    func testSafetyCodeIsFiveGroupsOfFourDigitsAndStable() throws {
        let keyPair = PasswordsCrypto.makeKeyPair()
        let code = try PasswordsCrypto.safetyCode(publicKey: keyPair.publicKey)
        XCTAssertEqual(code, try PasswordsCrypto.safetyCode(publicKey: keyPair.publicKey))
        let groups = code.split(separator: " ")
        XCTAssertEqual(groups.count, 5)
        XCTAssertTrue(groups.allSatisfy { $0.count == 4 && $0.allSatisfy(\.isNumber) })
        XCTAssertNotEqual(code, try PasswordsCrypto.safetyCode(publicKey: PasswordsCrypto.makeKeyPair().publicKey))
        XCTAssertThrowsError(try PasswordsCrypto.safetyCode(publicKey: Data(count: 64)))
    }

    func testSealedForAKeyPairOpensOnlyForThatOwnerRecipientAndItem() throws {
        let owner = PasswordsCrypto.makeKeyPair()
        let recipient = PasswordsCrypto.makeKeyPair()
        let itemKey = PasswordsCrypto.makeKey()
        let sealed = try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: owner, ownerId: "user-owner", recipientPublicKey: recipient.publicKey, recipientId: "user-1",
            itemId: "item-1")
        XCTAssertEqual(sealed.ephemeralPublicKey.count, 65)
        func open(
            ownerPublicKey: Data? = nil, ownerId: String = "user-owner", recipientId: String = "user-1", itemId: String = "item-1",
            recipientPrivateKey: Data? = nil
        ) throws -> Data {
            try PasswordsCrypto.openForKeyPair(
                sealed, recipientPrivateKey: recipientPrivateKey ?? recipient.privateKey, ownerPublicKey: ownerPublicKey ?? owner.publicKey,
                ownerId: ownerId, recipientId: recipientId, itemId: itemId)
        }
        XCTAssertEqual(try open(), itemKey)
        XCTAssertThrowsError(try open(ownerPublicKey: PasswordsCrypto.makeKeyPair().publicKey))
        XCTAssertThrowsError(try open(ownerPublicKey: Data(count: 65)))
        XCTAssertThrowsError(try open(ownerId: "user-other"))
        XCTAssertThrowsError(try open(recipientId: "user-2"))
        XCTAssertThrowsError(try open(itemId: "item-2"))
        XCTAssertThrowsError(try open(recipientPrivateKey: PasswordsCrypto.makeKeyPair().privateKey))
    }

    func testSealedForTheAgentUsesTheAgentRecipientIdAndTheOwnersKey() throws {
        XCTAssertEqual(PasswordsCrypto.agentRecipientId, "agent")
        let owner = PasswordsCrypto.makeKeyPair()
        let agent = PasswordsCrypto.makeKeyPair()
        let itemKey = PasswordsCrypto.makeKey()
        let sealed = try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: owner, ownerId: "user-owner", recipientPublicKey: agent.publicKey,
            recipientId: PasswordsCrypto.agentRecipientId, itemId: "item-1")
        XCTAssertEqual(
            try PasswordsCrypto.openForKeyPair(
                sealed, recipientPrivateKey: agent.privateKey, ownerPublicKey: owner.publicKey, ownerId: "user-owner",
                recipientId: PasswordsCrypto.agentRecipientId, itemId: "item-1"), itemKey)
    }

    func testSealedForAKeyPairWithAGivenEphemeralKeyAndNonceIsRepeatable() throws {
        let owner = PasswordsCrypto.makeKeyPair()
        let recipient = PasswordsCrypto.makeKeyPair()
        let ephemeral = PasswordsCrypto.makeKeyPair()
        let first = try PasswordsCrypto.sealForKeyPair(
            key, ownerKeyPair: owner, ownerId: "o", recipientPublicKey: recipient.publicKey, recipientId: "u", itemId: "i",
            ephemeralPrivateKey: ephemeral.privateKey, nonce: nonce)
        let second = try PasswordsCrypto.sealForKeyPair(
            key, ownerKeyPair: owner, ownerId: "o", recipientPublicKey: recipient.publicKey, recipientId: "u", itemId: "i",
            ephemeralPrivateKey: ephemeral.privateKey, nonce: nonce)
        XCTAssertEqual(first, second)
        XCTAssertEqual(first.ephemeralPublicKey, ephemeral.publicKey)
    }

    func testKeyPairSealedValueJSONKeepsTheDocumentedKeyOrderAndRoundTrips() throws {
        let sealed = PasswordsKeyPairSealedValue(ephemeralPublicKey: Data([9]), nonce: Data([1, 2, 3]), ciphertext: Data([4, 5, 6, 7]))
        XCTAssertEqual(sealed.jsonText, "{\"version\":1,\"ephemeralPublicKey\":\"CQ==\",\"nonce\":\"AQID\",\"ciphertext\":\"BAUGBw==\"}")
        XCTAssertEqual(try PasswordsKeyPairSealedValue(jsonText: sealed.jsonText), sealed)
    }

    // MARK: Associated data

    func testAssociatedDataIsExactlyWhatThePlanSays() {
        XCTAssertEqual(PasswordsCrypto.accountKeyAssociatedData, "account-key")
        XCTAssertEqual(PasswordsCrypto.privateKeyAssociatedData, "private-key")
        XCTAssertEqual(PasswordsCrypto.itemKeyAssociatedData(itemId: "abc"), "item-key:abc")
        XCTAssertEqual(PasswordsCrypto.itemFieldsAssociatedData(itemId: "abc", itemKind: .login), "item:abc:login")
        XCTAssertEqual(PasswordsCrypto.itemFieldsAssociatedData(itemId: "abc", itemKind: .card), "item:abc:card")
        XCTAssertEqual(PasswordsCrypto.itemFieldsAssociatedData(itemId: "abc", itemKind: .note), "item:abc:note")
        XCTAssertEqual(PasswordsCrypto.shareAssociatedData(itemId: "abc", ownerId: "own", recipientId: "agent"), "share:abc:own:agent")
    }

    // MARK: Item fields

    func testItemFieldsRoundTripForEveryKind() throws {
        let login = PasswordsItemFields.login(
            PasswordsLoginFields(
                name: "Example", userName: "ada@example.com", password: "p4ss/w0rd", sites: ["https://example.com/sign-in"],
                oneTimeCodeUri: "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP", notes: "line one\nline two",
                passkeys: [PasswordsPasskeyFields(credentialId: "AAEC", relyingPartyId: "example.com", createdAt: "2026-10-20T10:00:00Z")],
                folder: "Work"))
        let card = PasswordsItemFields.card(PasswordsCardFields(name: "Card", number: "4111111111111111", expiryMonth: "04", expiryYear: "2030"))
        let note = PasswordsItemFields.note(PasswordsNoteFields(name: "Note", text: "Dinner at 7"))
        for fields in [login, card, note] {
            let sealed = try PasswordsCrypto.sealItemFields(fields, itemKey: key, itemId: "item-1")
            XCTAssertEqual(
                try PasswordsCrypto.openItemFields(sealed, itemKey: key, itemId: "item-1", itemKind: fields.itemKind), fields)
        }
    }

    func testItemFieldsDoNotOpenAsAnotherKindOrItem() throws {
        let sealed = try PasswordsCrypto.sealItemFields(.note(PasswordsNoteFields(name: "N")), itemKey: key, itemId: "item-1")
        XCTAssertThrowsError(try PasswordsCrypto.openItemFields(sealed, itemKey: key, itemId: "item-1", itemKind: .login))
        XCTAssertThrowsError(try PasswordsCrypto.openItemFields(sealed, itemKey: key, itemId: "item-2", itemKind: .note))
    }

    func testItemFieldsUseTheNamesThePlanFixes() throws {
        let data = try PasswordsItemFields.login(PasswordsLoginFields(name: "A", sites: ["https://a.example/x"])).jsonData()
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(
            Set(object.keys), ["name", "userName", "password", "sites", "oneTimeCodeUri", "notes", "passkeys", "folder"])
        XCTAssertTrue(String(decoding: data, as: UTF8.self).contains("https://a.example/x"), "slashes are not escaped")
        let passkeyData = try JSONEncoder().encode(PasswordsPasskeyFields())
        let passkey = try XCTUnwrap(JSONSerialization.jsonObject(with: passkeyData) as? [String: Any])
        XCTAssertEqual(
            Set(passkey.keys), ["credentialId", "relyingPartyId", "userHandle", "userName", "userDisplayName", "privateKey", "createdAt"])
        let cardData = try PasswordsItemFields.card(PasswordsCardFields()).jsonData()
        let card = try XCTUnwrap(JSONSerialization.jsonObject(with: cardData) as? [String: Any])
        XCTAssertEqual(
            Set(card.keys),
            ["name", "cardholderName", "number", "expiryMonth", "expiryYear", "securityCode", "brand", "notes", "folder"])
    }

    func testItemFieldsFillAMissingKeyWithAnEmptyValue() throws {
        let fields = try PasswordsItemFields(itemKind: .login, jsonData: Data("{\"name\":\"A\",\"password\":\"x\"}".utf8))
        XCTAssertEqual(fields, .login(PasswordsLoginFields(name: "A", password: "x")))
        let note = try PasswordsItemFields(itemKind: .note, jsonData: Data("{}".utf8))
        XCTAssertEqual(note, .note(PasswordsNoteFields()))
    }
}
