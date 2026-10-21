import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsVectorsTests read the test vectors the Go package writes
/// (internal/passwords/testdata/vectors.json) and check that this
/// implementation reproduces every derived value byte for byte, opens every
/// sealed value, and agrees about signatures. The web tests read the same file.
final class PasswordsVectorsTests: XCTestCase {
    private typealias JSONObject = [String: Any]

    private func loadVectors() throws -> JSONObject {
        // This file is ios/TeaNodeKit/Tests/TeaNodeKitTests/PasswordsVectorsTests.swift.
        var url = URL(fileURLWithPath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        url.appendPathComponent("internal/passwords/testdata/vectors.json")
        guard FileManager.default.fileExists(atPath: url.path) else {
            throw XCTSkip(
                "vectors.json is not at \(url.path); run the tests with the whole repository mounted (Docker: -v <repo>:/repo -w /repo/ios/TeaNodeKit)"
            )
        }
        let object = try JSONSerialization.jsonObject(with: Data(contentsOf: url))
        return try XCTUnwrap(object as? JSONObject)
    }

    private func object(_ parent: JSONObject, _ key: String, file: StaticString = #filePath, line: UInt = #line) throws -> JSONObject {
        try XCTUnwrap(parent[key] as? JSONObject, "missing object \(key)", file: file, line: line)
    }

    private func text(_ parent: JSONObject, _ key: String, file: StaticString = #filePath, line: UInt = #line) throws -> String {
        try XCTUnwrap(parent[key] as? String, "missing text \(key)", file: file, line: line)
    }

    /// Every byte value in the file is lower-case hex with a space between bytes; only the sealed objects are base64.
    private func bytes(_ parent: JSONObject, _ key: String, file: StaticString = #filePath, line: UInt = #line) throws -> Data {
        let encoded = try text(parent, key, file: file, line: line)
        var data = Data()
        for pair in encoded.split(separator: " ") {
            let byte = try XCTUnwrap(UInt8(pair, radix: 16), "\(key) is not spaced hex", file: file, line: line)
            XCTAssertEqual(pair.count, 2, "\(key) has a byte that is not two digits", file: file, line: line)
            data.append(byte)
        }
        return data
    }

    private func sealedValue(_ parent: JSONObject, _ key: String) throws -> PasswordsSealedValue {
        let data = try JSONSerialization.data(withJSONObject: try object(parent, key))
        return try JSONDecoder().decode(PasswordsSealedValue.self, from: data)
    }

    func testPassphraseDerivations() throws {
        let section = try object(try loadVectors(), "passphrase")
        let passphraseKey = try PasswordsCrypto.passphraseKey(
            passphrase: try text(section, "passphrase"), salt: try bytes(section, "salt"),
            iterationCount: try XCTUnwrap(section["iterationCount"] as? Int))
        XCTAssertEqual(passphraseKey, try bytes(section, "passphraseKey"))
        XCTAssertEqual(try PasswordsCrypto.wrapKey(secret: passphraseKey), try bytes(section, "wrapKey"))
        let proof = try PasswordsCrypto.proof(secret: passphraseKey)
        XCTAssertEqual(proof, try bytes(section, "proof"))
        XCTAssertEqual(PasswordsCrypto.proofHash(proof: proof), try bytes(section, "proofHash"))
    }

    func testRecoveryKey() throws {
        let section = try object(try loadVectors(), "recoveryKey")
        let recoveryKey = try bytes(section, "bytes")
        XCTAssertEqual(try PasswordsCrypto.formatRecoveryKey(recoveryKey), try text(section, "text"))
        XCTAssertEqual(try PasswordsCrypto.parseRecoveryKey(try text(section, "text")), recoveryKey)
        for accepted in try XCTUnwrap(section["acceptedTexts"] as? [String]) {
            XCTAssertEqual(try PasswordsCrypto.parseRecoveryKey(accepted), recoveryKey, accepted)
        }
        XCTAssertEqual(try PasswordsCrypto.wrapKey(secret: recoveryKey), try bytes(section, "wrapKey"))
        let proof = try PasswordsCrypto.proof(secret: recoveryKey)
        XCTAssertEqual(proof, try bytes(section, "proof"))
        XCTAssertEqual(PasswordsCrypto.proofHash(proof: proof), try bytes(section, "proofHash"))
    }

    func testEveryInvalidRecoveryKeyTextIsRefused() throws {
        let texts = try XCTUnwrap(try loadVectors()["recoveryKeyInvalid"] as? [String])
        XCTAssertFalse(texts.isEmpty)
        for text in texts {
            XCTAssertThrowsError(try PasswordsCrypto.parseRecoveryKey(text), text) { error in
                XCTAssertEqual(error as? PasswordsCryptoError, .invalidRecoveryKey, text)
            }
        }
    }

    func testSealedValues() throws {
        let sections = try XCTUnwrap(try loadVectors()["sealed"] as? [JSONObject])
        XCTAssertFalse(sections.isEmpty)
        for section in sections {
            let name = try text(section, "name")
            let key = try bytes(section, "key")
            let associatedData = try text(section, "associatedData")
            let plaintext = try bytes(section, "plaintext")
            let expected = try sealedValue(section, "sealed")
            let sealed = try PasswordsCrypto.seal(plaintext, key: key, associatedData: associatedData, nonce: try bytes(section, "nonce"))
            XCTAssertEqual(sealed, expected, name)
            XCTAssertEqual(try PasswordsCrypto.open(expected, key: key, associatedData: associatedData), plaintext, name)
        }
    }

    func testItemFieldsOpenAsFields() throws {
        let sections = try XCTUnwrap(try loadVectors()["sealed"] as? [JSONObject])
        let section = try XCTUnwrap(sections.first { ($0["name"] as? String) == "item fields" }, "no item fields vector")
        // The associated data is item:<id>:<kind>.
        let parts = try text(section, "associatedData").split(separator: ":").map(String.init)
        XCTAssertEqual(parts.count, 3)
        let kind = try XCTUnwrap(PasswordsItemKind(rawValue: parts[2]))
        let fields = try PasswordsCrypto.openItemFields(
            try sealedValue(section, "sealed"), itemKey: try bytes(section, "key"), itemId: parts[1], itemKind: kind)
        XCTAssertEqual(fields.itemKind, kind)
        XCTAssertFalse(fields.name.isEmpty)
        // The access record is read, and sealed again, as the file has it.
        let shareSection = try object(try loadVectors(), "share")
        XCTAssertEqual(
            fields.access,
            PasswordsItemAccess(
                isUsableByAgent: true,
                grantees: [.person(userId: try text(shareSection, "recipientId")), .circle(circleId: "00000000-0000-4000-8000-000000000005")]))
        // Sealing the fields again with the same nonce gives something that opens to the same fields.
        let resealed = try PasswordsCrypto.sealItemFields(
            fields, itemKey: try bytes(section, "key"), itemId: parts[1], nonce: try bytes(section, "nonce"))
        XCTAssertEqual(
            try PasswordsCrypto.openItemFields(resealed, itemKey: try bytes(section, "key"), itemId: parts[1], itemKind: kind), fields)
    }

    func testPersonKey() throws {
        let section = try object(try loadVectors(), "personKey")
        let keyPair = try PasswordsCrypto.keyPair(privateKey: try bytes(section, "privateKey"))
        XCTAssertEqual(keyPair.publicKey, try bytes(section, "publicKey"))
        let userId = try text(section, "userId")
        let code = try text(section, "safetyCode")
        XCTAssertEqual(try PasswordsCrypto.safetyCode(userId: userId, publicKey: keyPair.publicKey), code)
        XCTAssertNotEqual(try PasswordsCrypto.safetyCode(userId: userId + "x", publicKey: keyPair.publicKey), code)
        let typedCodes = try XCTUnwrap(section["typedSafetyCodes"] as? [String])
        for typed in [code] + typedCodes {
            XCTAssertEqual(PasswordsCrypto.normalizeSafetyCode(typed), code, typed)
        }
        let invalidCodes = try XCTUnwrap(section["invalidSafetyCodes"] as? [String])
        XCTAssertFalse(invalidCodes.isEmpty)
        for invalid in invalidCodes {
            XCTAssertNil(PasswordsCrypto.normalizeSafetyCode(invalid), invalid)
        }
    }

    func testShare() throws {
        let section = try object(try loadVectors(), "share")
        let ownerId = try text(section, "ownerId")
        let recipientId = try text(section, "recipientId")
        let itemId = try text(section, "itemId")
        let senderPrivateKey = try bytes(section, "senderPrivateKey")
        let senderPublicKey = try bytes(section, "senderPublicKey")
        let recipientPrivateKey = try bytes(section, "recipientPrivateKey")
        let recipientPublicKey = try bytes(section, "recipientPublicKey")
        let ephemeralPrivateKey = try bytes(section, "ephemeralPrivateKey")
        let ephemeralPublicKey = try bytes(section, "ephemeralPublicKey")
        let sender = try PasswordsCrypto.keyPair(privateKey: senderPrivateKey)
        XCTAssertEqual(sender.publicKey, senderPublicKey)
        XCTAssertEqual(try PasswordsCrypto.keyPair(privateKey: recipientPrivateKey).publicKey, recipientPublicKey)
        XCTAssertEqual(try PasswordsCrypto.keyPair(privateKey: ephemeralPrivateKey).publicKey, ephemeralPublicKey)

        let ephemeralShared = try PasswordsCrypto.sharedSecret(privateKey: ephemeralPrivateKey, publicKey: recipientPublicKey)
        let ownerShared = try PasswordsCrypto.sharedSecret(privateKey: senderPrivateKey, publicKey: recipientPublicKey)
        XCTAssertEqual(ephemeralShared, try bytes(section, "ephemeralShared"))
        XCTAssertEqual(ownerShared, try bytes(section, "ownerShared"))
        // The recipient reaches the same two secrets from its own side.
        XCTAssertEqual(try PasswordsCrypto.sharedSecret(privateKey: recipientPrivateKey, publicKey: ephemeralPublicKey), ephemeralShared)
        XCTAssertEqual(try PasswordsCrypto.sharedSecret(privateKey: recipientPrivateKey, publicKey: senderPublicKey), ownerShared)
        XCTAssertEqual(
            try PasswordsCrypto.shareKey(
                ephemeralSharedSecret: ephemeralShared, ownerSharedSecret: ownerShared, recipientPublicKey: recipientPublicKey),
            try bytes(section, "key"))

        let itemKey = try bytes(section, "itemKey")
        let expectedData = try JSONSerialization.data(withJSONObject: try object(section, "sealed"))
        let expected = try JSONDecoder().decode(PasswordsKeyPairSealedValue.self, from: expectedData)
        let sealed = try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: sender, ownerId: ownerId, recipientPublicKey: recipientPublicKey, recipientId: recipientId,
            itemId: itemId, ephemeralPrivateKey: ephemeralPrivateKey, nonce: try bytes(section, "nonce"))
        XCTAssertEqual(sealed, expected)
        XCTAssertEqual(
            try PasswordsCrypto.openForKeyPair(
                expected, recipientPrivateKey: recipientPrivateKey, ownerPublicKey: senderPublicKey, ownerId: ownerId,
                recipientId: recipientId, itemId: itemId),
            itemKey)

        // Another owner key, owner id, recipient id or item id does not open it.
        let otherOwner = PasswordsCrypto.makeKeyPair()
        XCTAssertThrowsError(
            try PasswordsCrypto.openForKeyPair(
                expected, recipientPrivateKey: recipientPrivateKey, ownerPublicKey: otherOwner.publicKey, ownerId: ownerId,
                recipientId: recipientId, itemId: itemId)
        ) { error in
            XCTAssertEqual(error as? PasswordsCryptoError, .openFailed)
        }
        for (otherOwnerId, otherRecipientId, otherItemId) in [
            ("someone-else", recipientId, itemId), (ownerId, "someone-else", itemId), (ownerId, recipientId, "another-item"),
        ] {
            XCTAssertThrowsError(
                try PasswordsCrypto.openForKeyPair(
                    expected, recipientPrivateKey: recipientPrivateKey, ownerPublicKey: senderPublicKey, ownerId: otherOwnerId,
                    recipientId: otherRecipientId, itemId: otherItemId))
        }
    }

    func testOneTimeCodes() throws {
        let sections = try XCTUnwrap(try loadVectors()["oneTimeCodes"] as? [JSONObject])
        XCTAssertFalse(sections.isEmpty)
        for section in sections {
            let uri = try text(section, "uri")
            let settings = try PasswordsOneTimeCode.parse(uri: uri)
            let unixSeconds = try XCTUnwrap(section["unixSeconds"] as? Int)
            XCTAssertEqual(PasswordsOneTimeCode.code(settings: settings, unixSeconds: unixSeconds), try text(section, "code"), uri)
        }
    }

    func testPasskey() throws {
        let section = try object(try loadVectors(), "passkey")
        let privateKey = try bytes(section, "privateKey")
        let publicKeyX = try bytes(section, "publicKeyX")
        let publicKeyY = try bytes(section, "publicKeyY")
        let relyingPartyId = try text(section, "relyingPartyId")
        let credentialId = try bytes(section, "credentialId")
        let isVerified = try XCTUnwrap(section["isVerified"] as? Bool)

        let coordinates = try PasswordsPasskey.publicKeyCoordinates(privateKey: privateKey)
        XCTAssertEqual(coordinates.x, publicKeyX)
        XCTAssertEqual(coordinates.y, publicKeyY)
        // The key is read from Go's PKCS#8, and written back in the same bytes.
        let scalar = try XCTUnwrap(PasswordsPasskey.scalar(fromDER: privateKey))
        XCTAssertEqual(PasswordsPasskey.pkcs8(scalar: scalar, point: Data([4]) + publicKeyX + publicKeyY), privateKey)

        XCTAssertEqual(try PasswordsPasskey.cosePublicKey(publicKeyX: publicKeyX, publicKeyY: publicKeyY), try bytes(section, "cosePublicKey"))
        let registrationData = try PasswordsPasskey.registrationAuthenticatorData(
            relyingPartyId: relyingPartyId, credentialId: credentialId, publicKeyX: publicKeyX, publicKeyY: publicKeyY, isVerified: isVerified)
        XCTAssertEqual(registrationData, try bytes(section, "registrationAuthenticatorData"))
        XCTAssertEqual(PasswordsPasskey.attestationObject(authenticatorData: registrationData), try bytes(section, "attestationObject"))
        let registration = try PasswordsPasskey.register(
            relyingPartyId: relyingPartyId, isVerified: isVerified, privateKey: privateKey, credentialId: credentialId)
        XCTAssertEqual(registration.authenticatorData, registrationData)

        let assertionData = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId: relyingPartyId, isVerified: isVerified)
        XCTAssertEqual(assertionData, try bytes(section, "assertionAuthenticatorData"))
        let clientDataHash = try bytes(section, "clientDataHash")

        // The signature Go made verifies here, and one made here verifies too.
        XCTAssertTrue(
            try PasswordsPasskey.isSignatureValid(
                signature: try bytes(section, "signature"), publicKeyX: publicKeyX, publicKeyY: publicKeyY,
                authenticatorData: assertionData, clientDataHash: clientDataHash))
        let ownSignature = try PasswordsPasskey.sign(privateKey: privateKey, authenticatorData: assertionData, clientDataHash: clientDataHash)
        XCTAssertTrue(
            try PasswordsPasskey.isSignatureValid(
                signature: ownSignature, publicKeyX: publicKeyX, publicKeyY: publicKeyY, authenticatorData: assertionData,
                clientDataHash: clientDataHash))
    }
}
