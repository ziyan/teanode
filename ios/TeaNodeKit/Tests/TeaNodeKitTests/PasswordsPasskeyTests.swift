import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(CryptoKit)
    import CryptoKit
#else
    import Crypto
#endif

/// PasswordsPasskeyTests check the authenticator's bytes against the layout
/// the plan fixes, and that what it signs verifies. PasswordsVectorsTests
/// checks the same bytes against Go.
final class PasswordsPasskeyTests: XCTestCase {
    private let relyingPartyId = "example.com"
    private let clientDataHash = Data((0..<32).map { UInt8(255 - $0) })

    func testPrivateKeyIsPKCS8NotSEC1() throws {
        let privateKey = PasswordsPasskey.makePrivateKey()
        XCTAssertEqual(privateKey.count, 138)
        // SEQUENCE, then INTEGER 0 (PKCS#8). A bare SEC1 key would start with INTEGER 1.
        XCTAssertEqual(Array(privateKey.prefix(6)), [0x30, 0x81, 0x87, 0x02, 0x01, 0x00])
        // The platform's own parser accepts it, and gives back the same public key.
        let parsed = try P256.Signing.PrivateKey(derRepresentation: privateKey)
        let coordinates = try PasswordsPasskey.publicKeyCoordinates(privateKey: privateKey)
        XCTAssertEqual(parsed.publicKey.x963Representation, Data([4]) + coordinates.x + coordinates.y)
    }

    func testOurPKCS8MatchesThePlatformsOwnWhenItWritesPKCS8() throws {
        let key = P256.Signing.PrivateKey()
        let ours = PasswordsPasskey.pkcs8(scalar: key.rawRepresentation, point: key.publicKey.x963Representation)
        // swift-crypto writes the same 138 bytes as Go. Apple's may differ in its optional
        // parts, which is why the format is written by hand; the scalar must still agree.
        #if !canImport(CryptoKit)
            XCTAssertEqual(ours, key.derRepresentation)
        #endif
        XCTAssertEqual(PasswordsPasskey.scalar(fromDER: ours), key.rawRepresentation)
        XCTAssertEqual(PasswordsPasskey.scalar(fromDER: key.derRepresentation), key.rawRepresentation)
    }

    func testBareSEC1KeysAreReadToo() throws {
        let scalar = Data((1...32).map { UInt8($0) })
        var sec1 = Data([0x30, 0x25, 0x02, 0x01, 0x01, 0x04, 0x20])
        sec1.append(scalar)
        XCTAssertEqual(PasswordsPasskey.scalar(fromDER: sec1), scalar)
    }

    func testGarbageIsNotAPrivateKey() {
        for garbage in [Data(), Data([0x30]), Data(repeating: 0, count: 138), Data([0x30, 0x03, 0x02, 0x01, 0x07])] {
            XCTAssertNil(PasswordsPasskey.scalar(fromDER: garbage))
            XCTAssertThrowsError(try PasswordsPasskey.publicKeyCoordinates(privateKey: garbage)) { error in
                XCTAssertEqual(error as? PasswordsPasskeyError, .invalidPrivateKey)
            }
        }
    }

    func testCosePublicKeyLayout() throws {
        let x = Data(repeating: 0xAA, count: 32)
        let y = Data(repeating: 0xBB, count: 32)
        let cose = try PasswordsPasskey.cosePublicKey(publicKeyX: x, publicKeyY: y)
        XCTAssertEqual(cose.count, 77)
        XCTAssertEqual(Array(cose.prefix(13)), [0xA5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20, 0xAA, 0xAA, 0xAA])
        XCTAssertEqual(cose.subdata(in: 10..<42), x)
        XCTAssertEqual(Array(cose[42..<45]), [0x22, 0x58, 0x20])
        XCTAssertEqual(cose.subdata(in: 45..<77), y)
        XCTAssertThrowsError(try PasswordsPasskey.cosePublicKey(publicKeyX: Data(count: 31), publicKeyY: y))
    }

    func testRegistrationAuthenticatorDataLayout() throws {
        let registration = try PasswordsPasskey.register(relyingPartyId: relyingPartyId, isVerified: true)
        let data = registration.authenticatorData
        XCTAssertEqual(data.count, 32 + 1 + 4 + 16 + 2 + 16 + 77)
        XCTAssertEqual(data.prefix(32), Data(SHA256.hash(data: Data(relyingPartyId.utf8))))
        XCTAssertEqual(data[32], 0x5D)
        XCTAssertEqual(Array(data[33..<37]), [0, 0, 0, 0])
        XCTAssertEqual(data.subdata(in: 37..<53), Data("TEANODEPASSWORDS".utf8))
        XCTAssertEqual(Array(data[53..<55]), [0, 16])
        XCTAssertEqual(data.subdata(in: 55..<71), registration.credentialId)
        XCTAssertEqual(data.suffix(77), registration.cosePublicKey)
        let unverified = try PasswordsPasskey.register(relyingPartyId: relyingPartyId, isVerified: false)
        XCTAssertEqual(unverified.authenticatorData[32], 0x59)
    }

    func testAAGUIDIsTheDocumentedBytes() {
        XCTAssertEqual(
            Array(PasswordsPasskey.aaguid),
            [0x54, 0x45, 0x41, 0x4E, 0x4F, 0x44, 0x45, 0x50, 0x41, 0x53, 0x53, 0x57, 0x4F, 0x52, 0x44, 0x53])
    }

    func testAttestationObjectLayout() throws {
        let registration = try PasswordsPasskey.register(relyingPartyId: relyingPartyId, isVerified: true)
        let object = registration.attestationObject
        var expected = Data([0xA3])
        expected.append(Data([0x63]) + Data("fmt".utf8))
        expected.append(Data([0x64]) + Data("none".utf8))
        expected.append(Data([0x67]) + Data("attStmt".utf8))
        expected.append(0xA0)
        expected.append(Data([0x68]) + Data("authData".utf8))
        expected.append(Data([0x58, UInt8(registration.authenticatorData.count)]))
        expected.append(registration.authenticatorData)
        XCTAssertEqual(object, expected)
    }

    func testAssertionAuthenticatorDataLayout() {
        let verified = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId: relyingPartyId, isVerified: true)
        XCTAssertEqual(verified.count, 37)
        XCTAssertEqual(verified.prefix(32), Data(SHA256.hash(data: Data(relyingPartyId.utf8))))
        XCTAssertEqual(verified[32], 0x1D)
        XCTAssertEqual(Array(verified[33..<37]), [0, 0, 0, 0])
        XCTAssertEqual(PasswordsPasskey.assertionAuthenticatorData(relyingPartyId: relyingPartyId, isVerified: false)[32], 0x19)
    }

    func testSignatureVerifiesAndFailsForAnotherMessageOrKey() throws {
        let registration = try PasswordsPasskey.register(relyingPartyId: relyingPartyId, isVerified: true)
        let authenticatorData = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId: relyingPartyId, isVerified: true)
        let signature = try PasswordsPasskey.sign(
            privateKey: registration.privateKey, authenticatorData: authenticatorData, clientDataHash: clientDataHash)
        XCTAssertEqual(signature[0], 0x30, "DER sequence")
        XCTAssertTrue(
            try PasswordsPasskey.isSignatureValid(
                signature: signature, publicKeyX: registration.publicKeyX, publicKeyY: registration.publicKeyY,
                authenticatorData: authenticatorData, clientDataHash: clientDataHash))
        XCTAssertFalse(
            try PasswordsPasskey.isSignatureValid(
                signature: signature, publicKeyX: registration.publicKeyX, publicKeyY: registration.publicKeyY,
                authenticatorData: authenticatorData, clientDataHash: Data(count: 32)))
        let other = try PasswordsPasskey.register(relyingPartyId: relyingPartyId, isVerified: true)
        XCTAssertFalse(
            try PasswordsPasskey.isSignatureValid(
                signature: signature, publicKeyX: other.publicKeyX, publicKeyY: other.publicKeyY, authenticatorData: authenticatorData,
                clientDataHash: clientDataHash))
        XCTAssertThrowsError(
            try PasswordsPasskey.isSignatureValid(
                signature: Data([1, 2, 3]), publicKeyX: registration.publicKeyX, publicKeyY: registration.publicKeyY,
                authenticatorData: authenticatorData, clientDataHash: clientDataHash)
        ) { error in
            XCTAssertEqual(error as? PasswordsPasskeyError, .invalidSignature)
        }
    }

    func testRegistrationWithGivenKeyAndCredentialIsRepeatable() throws {
        let privateKey = PasswordsPasskey.makePrivateKey()
        let credentialId = Data(repeating: 3, count: 16)
        let first = try PasswordsPasskey.register(
            relyingPartyId: relyingPartyId, isVerified: true, privateKey: privateKey, credentialId: credentialId)
        let second = try PasswordsPasskey.register(
            relyingPartyId: relyingPartyId, isVerified: true, privateKey: privateKey, credentialId: credentialId)
        XCTAssertEqual(first, second)
        XCTAssertEqual(first.privateKey, privateKey)
    }
}
