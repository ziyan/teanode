import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AccountSecurityTests cover the sessions, API tokens and connected apps:
/// reading the server's answers, and what the lists decide from them.
final class AccountSecurityTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    func testReadsTheServersSessions() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListSessions":[
                  {"id":"session-1","current":false,"created":"2030-01-02T03:04:05.123456789Z","lastUsed":"2030-01-03T00:00:00Z",
                   "ip":"192.0.2.10","userAgent":"Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0","revoked":null},
                  {"id":"session-2","current":true,"created":"2030-01-01T00:00:00Z","impersonatorUsername":"operator"}
                ]}}
                """#)
        let sessions = try await connection.accountSessions(includeRevoked: true)
        XCTAssertEqual(sessions.map(\.id), ["session-1", "session-2"])
        XCTAssertEqual(sessions[0].ip, "192.0.2.10")
        XCTAssertNil(sessions[0].revoked)
        XCTAssertEqual(sessions[1].impersonatorUsername, "operator")
        XCTAssertNil(sessions[1].userAgent)
        XCTAssertEqual(try sentVariables(fake.sent[0])["includeRevoked"], .bool(true))
    }

    func testListsTheCurrentSessionFirstAndNeverOffersToRevokeIt() {
        let sessions = [
            AccountSession(id: "newest", created: "2030-01-03T00:00:00Z"),
            AccountSession(id: "mine", current: true, created: "2030-01-02T00:00:00Z"),
            AccountSession(id: "ended", created: "2030-01-01T00:00:00Z", revoked: "2030-01-04T00:00:00Z"),
        ]
        let listed = AccountSession.listed(sessions)
        XCTAssertEqual(listed.map(\.id), ["mine", "newest", "ended"])
        XCTAssertEqual(listed.map(\.canRevoke), [false, true, false])
    }

    func testNamesTheBrowserAPersonRecognizes() {
        func name(_ userAgent: String?) -> String? {
            AccountSession(id: "a-session", created: "2030-01-01T00:00:00Z", userAgent: userAgent).browserName
        }
        XCTAssertNil(name(nil))
        XCTAssertNil(name(""))
        XCTAssertEqual(
            name("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"),
            "Chrome · Windows NT 10.0")
        XCTAssertEqual(name("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Gecko/20100101 Firefox/140.0"), "Firefox · Macintosh")
        XCTAssertEqual(name("SomeTool/1.0 Edg/140.0"), "Edge")
        XCTAssertEqual(name("an-unknown-client"), "an-unknown-client")
        let long = String(repeating: "x", count: 80)
        XCTAssertEqual(name(long), String(repeating: "x", count: 60) + "…")
    }

    func testRevokesOneSessionOrAll() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"RevokeSession":null,"RevokeAllSessions":{"authenticated":false}}}"#)
        try await connection.revokeSession("session-1")
        try await connection.revokeAllSessions()
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["sessionId": .string("session-1")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), [:])
    }

    func testReadsTheServersTokens() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListTokens":[
                  {"id":"tnt_one","name":"laptop","username":"robin","created":"2030-01-01T00:00:00Z","expires":"2030-04-01T00:00:00Z",
                   "lastUsed":"2030-01-02T00:00:00Z","lastUsedIp":"198.51.100.7","revoked":null},
                  {"id":"tnt_two","name":"","username":"robin","created":"2030-01-01T00:00:00Z"}
                ]}}
                """#)
        let tokens = try await connection.apiTokens()
        XCTAssertEqual(tokens.map(\.id), ["tnt_one", "tnt_two"])
        XCTAssertEqual(tokens[0].lastUsedIp, "198.51.100.7")
        XCTAssertNil(tokens[1].expires)
    }

    func testTellsWhenATokenExpires() {
        let now = ServerTime.date("2030-02-01T00:00:00Z")!
        func expiry(_ expires: String?) -> APITokenExpiry {
            APIToken(id: "tnt_one", name: "laptop", username: "robin", created: "2030-01-01T00:00:00Z", expires: expires).expiry(now: now)
        }
        XCTAssertEqual(expiry(nil), .never)
        XCTAssertEqual(expiry("2030-01-15T00:00:00Z"), .expired)
        XCTAssertEqual(expiry("2030-03-01T00:00:00Z"), .expires(ServerTime.date("2030-03-01T00:00:00Z")!))
    }

    func testCreatesATokenWithALifetimeOrNone() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"CreateToken":{"secret":"tnt_made_up_secret",
                  "token":{"id":"tnt_made","name":"laptop","username":"robin","created":"2030-01-01T00:00:00Z"}}}}
                """#)
        let created = try await connection.createApiToken(name: "laptop", lifetime: .thirtyDays)
        XCTAssertEqual(created.secret, "tnt_made_up_secret")
        XCTAssertEqual(created.token?.id, "tnt_made")
        _ = try await connection.createApiToken(name: "laptop", lifetime: .never)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["name": .string("laptop"), "lifetime": .string("720h")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["name": .string("laptop"), "lifetime": .null])
    }

    func testChangesOnlyWhatWasChanged() {
        let token = APIToken(id: "tnt_one", name: "laptop", username: "robin", created: "2030-01-01T00:00:00Z")

        let kept = APITokenChange(token: token, newName: " laptop ", lifetime: nil)
        XCTAssertFalse(kept.hasChanges)

        let renamed = APITokenChange(token: token, newName: " desktop ", lifetime: nil)
        XCTAssertTrue(renamed.hasChanges)
        XCTAssertEqual(renamed.variables, ["tokenId": .string("tnt_one"), "name": .string("desktop"), "lifetime": .null])

        let neverExpiring = APITokenChange(token: token, newName: "laptop", lifetime: .never)
        XCTAssertEqual(neverExpiring.variables, ["tokenId": .string("tnt_one"), "name": .null, "lifetime": .string("never")])

        let renewed = APITokenChange(token: token, newName: "laptop", lifetime: .year)
        XCTAssertEqual(renewed.variables["lifetime"], .string("8760h"))
    }

    func testReadsTheServersApps() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListApps":[
                  {"clientId":"client-1","name":"","registeredName":"Example Assistant","redirectHosts":["assistant.example.com"],
                   "registered":"2030-01-01T00:00:00Z","lastUsed":null,"expires":"2030-02-01T00:00:00Z",
                   "renewableUntil":"2030-04-01T00:00:00Z","tokenCount":2}
                ]}}
                """#)
        let apps = try await connection.connectedApps()
        XCTAssertEqual(apps.count, 1)
        XCTAssertEqual(apps[0].id, "client-1")
        XCTAssertEqual(apps[0].redirectHosts, ["assistant.example.com"])
        XCTAssertEqual(apps[0].tokenCount, 2)
        XCTAssertNil(apps[0].lastUsedIp)
    }

    func testNamesAnAppAsTheDashboardDoes() {
        XCTAssertEqual(ConnectedApp(clientId: "one", name: "My notes", registeredName: "Notes").displayName, "My notes")
        XCTAssertEqual(ConnectedApp(clientId: "one", name: "My notes", registeredName: "Notes").differentRegisteredName, "Notes")
        XCTAssertNil(ConnectedApp(clientId: "one", name: "Notes", registeredName: "Notes").differentRegisteredName)
        XCTAssertEqual(ConnectedApp(clientId: "one", name: "", registeredName: "Notes").displayName, "Notes")
        XCTAssertNil(ConnectedApp(clientId: "one", name: "", registeredName: "").displayName)
        XCTAssertNil(ConnectedApp(clientId: "one", name: "Notes", registeredName: "").differentRegisteredName)
    }

    func testRenamesAndDisconnectsAnApp() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"RenameApp":null,"DisconnectApp":null}}"#)
        try await connection.renameConnectedApp("client-1", name: "Notes")
        try await connection.disconnectApp("client-1")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["clientId": .string("client-1"), "name": .string("Notes")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["clientId": .string("client-1")])
    }
}
