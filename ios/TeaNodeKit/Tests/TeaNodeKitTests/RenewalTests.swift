import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// RotatingServer renews a sign-in the way the server does: each refresh
/// token works once, and the one it hands back replaces it. A renewal takes
/// a moment, so callers that arrive meanwhile overlap with it.
final class RotatingServer: HTTPTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var refreshToken: String
    private var renewalNumber = 1
    private var requests: [URLRequest] = []
    /// What a GraphQL request with an access token is answered.
    private let answerGraphQL: @Sendable (String) -> (Int, String)
    /// Runs as a refresh token arrives, before it is answered: another
    /// process storing its own renewal at that moment.
    private let onRefresh: @Sendable () -> Void

    init(
        refreshToken: String, onRefresh: @escaping @Sendable () -> Void = {},
        answerGraphQL: @escaping @Sendable (String) -> (Int, String) = { _ in (200, #"{"data":{"ReadAgent":{"agent":{"name":"Tea"}}}}"#) }
    ) {
        self.refreshToken = refreshToken
        self.onRefresh = onRefresh
        self.answerGraphQL = answerGraphQL
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        keep(request)
        let (statusCode, body): (Int, String)
        if request.url?.path == "/oauth/token" {
            onRefresh()
            try await Task.sleep(for: .milliseconds(50))
            (statusCode, body) = redeem(form(request)["refresh_token"] ?? "")
        } else {
            let authorization = request.value(forHTTPHeaderField: "Authorization") ?? ""
            (statusCode, body) = answerGraphQL(String(authorization.dropFirst("Bearer ".count)))
        }
        return (Data(body.utf8), HTTPURLResponse(url: request.url!, statusCode: statusCode, httpVersion: nil, headerFields: nil)!)
    }

    private func redeem(_ presented: String) -> (Int, String) {
        lock.lock()
        defer { lock.unlock() }
        guard presented == refreshToken else {
            return (400, #"{"error":"invalid_grant","error_description":"The refresh token is not valid."}"#)
        }
        renewalNumber += 1
        refreshToken = "refresh-\(renewalNumber)"
        return (200, #"{"access_token":"access-\#(renewalNumber)","refresh_token":"refresh-\#(renewalNumber)","expires_in":2592000}"#)
    }

    private func keep(_ request: URLRequest) {
        lock.lock()
        defer { lock.unlock() }
        requests.append(request)
    }

    var renewalCount: Int {
        lock.lock()
        defer { lock.unlock() }
        return requests.filter { $0.url?.path == "/oauth/token" }.count
    }

    var graphQLCount: Int {
        lock.lock()
        defer { lock.unlock() }
        return requests.filter { $0.url?.path == "/api/v1/graphql" }.count
    }
}

/// MemoryAccountStore keeps sign-ins as the keychain does, shared by every
/// connection made with it, as the app's processes share the keychain.
final class MemoryAccountStore: AccountStore, @unchecked Sendable {
    private let lock = NSLock()
    private var ring: AccountRing

    init(_ accounts: [Account]) {
        ring = AccountRing(accounts: accounts, activeKey: accounts.first?.accountKey)
    }

    func save(_ account: Account) throws {
        lock.lock()
        defer { lock.unlock() }
        ring.put(account)
    }

    func load() throws -> Account? {
        lock.lock()
        defer { lock.unlock() }
        return ring.active
    }

    func load(accountKey: String) throws -> Account? {
        lock.lock()
        defer { lock.unlock() }
        return ring.accounts.first { $0.accountKey == accountKey }
    }

    func delete() throws {
        lock.lock()
        defer { lock.unlock() }
        if let key = ring.active?.accountKey { ring.remove(key) }
    }
}

/// MemorySecrets keeps named secrets as the keychain does.
final class MemorySecrets: SecretStore, @unchecked Sendable {
    private let lock = NSLock()
    private var secrets: [String: Data] = [:]

    func read(_ name: String) throws -> Data? {
        lock.lock()
        defer { lock.unlock() }
        return secrets[name]
    }

    func write(_ data: Data, as name: String) throws {
        lock.lock()
        defer { lock.unlock() }
        secrets[name] = data
    }

    func delete(_ name: String) throws {
        lock.lock()
        defer { lock.unlock() }
        secrets[name] = nil
    }

    var names: Set<String> {
        lock.lock()
        defer { lock.unlock() }
        return Set(secrets.keys)
    }
}

final class RenewalTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    /// A sign-in an hour from running out, so the next token is renewed.
    func expiring(accessToken: String = "access-1", refreshToken: String = "refresh-1") -> Account {
        Account(
            server: server, clientId: "a-client", accessToken: accessToken, refreshToken: refreshToken,
            expiresAt: Date().addingTimeInterval(3600), username: "robin")
    }

    func testCallersAtTheSameTimeShareOneRenewal() async throws {
        let rotating = RotatingServer(refreshToken: "refresh-1")
        let store = MemoryAccountStore([expiring()])
        let connection = Connection(account: expiring(), transport: rotating, store: store)
        let tokens = try await withThrowingTaskGroup(of: String.self) { group in
            for _ in 0..<5 {
                group.addTask { try await connection.accessToken() }
            }
            var tokens: [String] = []
            for try await token in group { tokens.append(token) }
            return tokens
        }
        XCTAssertEqual(tokens, Array(repeating: "access-2", count: 5))
        XCTAssertEqual(rotating.renewalCount, 1, "each caller renewed with the same refresh token")
        XCTAssertEqual(try store.load(accountKey: expiring().accountKey)?.refreshToken, "refresh-2")
        let isSignedOut = await connection.isSignedOut
        XCTAssertFalse(isSignedOut)
    }

    func testRequestsAtTheSameTimeShareOneRenewal() async throws {
        let rotating = RotatingServer(refreshToken: "refresh-1")
        let connection = Connection(account: expiring(), transport: rotating, store: MemoryAccountStore([expiring()]))
        try await withThrowingTaskGroup(of: Void.self) { group in
            for _ in 0..<3 {
                group.addTask { _ = try await connection.agentName() }
            }
            try await group.waitForAll()
        }
        XCTAssertEqual(rotating.renewalCount, 1)
        XCTAssertEqual(rotating.graphQLCount, 3)
    }

    /// Another process renews with the same refresh token a moment first:
    /// the server refuses this one, and what the other stored is used.
    func testARefusedRenewalTakesUpWhatAnotherProcessStored() async throws {
        let store = MemoryAccountStore([expiring()])
        let account = expiring()
        let elsewhere = Account(
            server: server, clientId: "a-client", accessToken: "access-elsewhere", refreshToken: "refresh-elsewhere",
            expiresAt: Date().addingTimeInterval(30 * 24 * 3600), username: "robin")
        let rotating = RotatingServer(refreshToken: "refresh-elsewhere", onRefresh: { try? store.save(elsewhere) })
        let connection = Connection(account: account, transport: rotating, store: store)
        let token = try await connection.accessToken()
        XCTAssertEqual(token, "access-elsewhere")
        let isSignedOut = await connection.isSignedOut
        XCTAssertFalse(isSignedOut, "a renewal another process made signed this one out")
        let kept = await connection.account
        XCTAssertEqual(kept.refreshToken, "refresh-elsewhere")
    }

    func testARefusedRenewalWithNothingNewerStoredHasEnded() async throws {
        let rotating = RotatingServer(refreshToken: "refresh-retired-by-the-server")
        let connection = Connection(account: expiring(), transport: rotating, store: MemoryAccountStore([expiring()]))
        do {
            _ = try await connection.accessToken()
            XCTFail("a refused refresh token was taken")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .signedOut)
        }
        let isSignedOut = await connection.isSignedOut
        XCTAssertTrue(isSignedOut)
    }

    /// The server refuses a token another process's renewal retired; the
    /// request goes again with the token that process stored.
    func testA401IsTriedAgainWithTheStoredToken() async throws {
        let held = Account(
            server: server, clientId: "a-client", accessToken: "access-1", refreshToken: "refresh-1",
            expiresAt: Date().addingTimeInterval(10 * 24 * 3600), username: "robin")
        var stored = held
        stored.accessToken = "access-2"
        stored.refreshToken = "refresh-2"
        stored.expiresAt = Date().addingTimeInterval(30 * 24 * 3600)
        let rotating = RotatingServer(refreshToken: "refresh-2", answerGraphQL: { token in
            token == "access-2" ? (200, #"{"data":{"ReadAgent":{"agent":{"name":"Tea"}}}}"#) : (401, "")
        })
        let connection = Connection(account: held, transport: rotating, store: MemoryAccountStore([stored]))
        _ = try await connection.agentName()
        XCTAssertEqual(rotating.graphQLCount, 2)
        XCTAssertEqual(rotating.renewalCount, 0)
        let isSignedOut = await connection.isSignedOut
        XCTAssertFalse(isSignedOut)
    }

    /// With nothing newer stored, a 401 renews the sign-in and tries again.
    func testA401WithNothingStoredRenewsAndTriesAgain() async throws {
        let held = Account(
            server: server, clientId: "a-client", accessToken: "access-1", refreshToken: "refresh-1",
            expiresAt: Date().addingTimeInterval(10 * 24 * 3600), username: "robin")
        let rotating = RotatingServer(refreshToken: "refresh-1", answerGraphQL: { token in
            token == "access-2" ? (200, #"{"data":{"ReadAgent":{"agent":{"name":"Tea"}}}}"#) : (401, "")
        })
        let connection = Connection(account: held, transport: rotating, store: MemoryAccountStore([held]))
        _ = try await connection.agentName()
        XCTAssertEqual(rotating.renewalCount, 1)
        XCTAssertEqual(rotating.graphQLCount, 2)
    }

    func testA401AgainAfterAFreshTokenHasEnded() async throws {
        let held = Account(
            server: server, clientId: "a-client", accessToken: "access-1", refreshToken: "refresh-1",
            expiresAt: Date().addingTimeInterval(10 * 24 * 3600), username: "robin")
        let rotating = RotatingServer(refreshToken: "refresh-1", answerGraphQL: { _ in (401, "") })
        let connection = Connection(account: held, transport: rotating)
        do {
            _ = try await connection.agentName()
            XCTFail("a request refused twice succeeded")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .signedOut)
        }
        XCTAssertEqual(rotating.graphQLCount, 2, "a 401 was tried more than once more")
        let isSignedOut = await connection.isSignedOut
        XCTAssertTrue(isSignedOut)
    }
}

final class PayloadKeyringTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    func account(_ username: String) -> Account {
        Account(server: server, clientId: "a-client", accessToken: "a", refreshToken: "r", expiresAt: .distantFuture, username: username)
    }

    func testTheSingleKeyStaysWithTheAccountInUse() throws {
        let secrets = MemorySecrets()
        try secrets.write(Data("the-single-key".utf8), as: "payloadKey")
        let keyring = PayloadKeyring(secrets: secrets)
        let ring = AccountRing(accounts: [account("robin"), account("sam")], activeKey: account("sam").accountKey)

        XCTAssertEqual(try keyring.keys(for: ring), [AccountPayloadKey(accountKey: account("sam").accountKey, payloadKey: "the-single-key")])
        XCTAssertFalse(secrets.names.contains("payloadKey"), "the single key was kept on its own after it was moved")

        // The other account gets a key of its own, and the moved one stays.
        XCTAssertEqual(try keyring.current(for: account("robin").accountKey, make: { "a-new-key" }), "a-new-key")
        XCTAssertEqual(try keyring.current(for: account("sam").accountKey, make: { "never-made" }), "the-single-key")
        XCTAssertEqual(
            try keyring.keys(for: ring),
            [
                AccountPayloadKey(accountKey: account("robin").accountKey, payloadKey: "a-new-key"),
                AccountPayloadKey(accountKey: account("sam").accountKey, payloadKey: "the-single-key"),
            ])

        try keyring.forget(for: account("sam").accountKey)
        XCTAssertNil(try keyring.stored(for: account("sam").accountKey))
        XCTAssertEqual(try keyring.keys(for: ring).map(\.accountKey), [account("robin").accountKey])
    }

    func testTheSingleKeyNeverReplacesAnAccountsOwn() throws {
        let secrets = MemorySecrets()
        let keyring = PayloadKeyring(secrets: secrets)
        _ = try keyring.current(for: account("robin").accountKey, make: { "robins-key" })
        try secrets.write(Data("the-single-key".utf8), as: "payloadKey")
        try keyring.moveSingleKey(to: account("robin").accountKey)
        XCTAssertEqual(try keyring.stored(for: account("robin").accountKey), "robins-key")
        XCTAssertFalse(secrets.names.contains("payloadKey"))
    }

    func testTheSingleKeyWaitsWhileNoAccountIsKept() throws {
        let secrets = MemorySecrets()
        try secrets.write(Data("the-single-key".utf8), as: "payloadKey")
        XCTAssertEqual(try PayloadKeyring(secrets: secrets).keys(for: AccountRing()), [])
        XCTAssertTrue(secrets.names.contains("payloadKey"))
    }

    #if canImport(CryptoKit)
        func testTheKeyThatOpensANotificationNamesItsAccount() throws {
            let key = Data((0..<32).map { UInt8($0) }).base64EncodedString()
            let sealed = "H8RRRtjjpug7CHBstkS2ic1gPC+8v2ETffkv/MTXRhaeqmtyQJlSIczFvR481L+D1BqhO2Yrfj7bLeBSFAqJ6salcR93DzAV0Hleqk+fuO6pBo99yOEC9Th0d3b6jj6DbnbFCqL2MZpVKCm4va3GD+K4WMEw53BqMqCutMuPKvhXNemi9I/xNGgsTk0DwtlofvJaDfsKruPrdHYcbh6GhJnlMoyNoFN89Jyh/44oVcTaSTfMrAwh6hSWET6IAKGuwhUaJl7bpmD3aJJMWjDthHDMClCZB9rbMhezpEl62YAnPDx1iknIqW/yjXFVsqXMv6bv8yMD2jIawXoa2PlPu3qpZXZY6GxXyvgRGazoTJREK2c9gW0="
            let keys = [
                AccountPayloadKey(accountKey: account("robin").accountKey, payloadKey: PayloadKey.make()),
                AccountPayloadKey(accountKey: account("sam").accountKey, payloadKey: key),
            ]
            let opened = PayloadKey.open(sealed, keys: keys)
            XCTAssertEqual(opened?.accountKey, account("sam").accountKey)
            XCTAssertEqual(opened?.content.notificationKind, "question")
            XCTAssertNil(PayloadKey.open(sealed, keys: Array(keys.prefix(1))))
        }
    #endif
}
