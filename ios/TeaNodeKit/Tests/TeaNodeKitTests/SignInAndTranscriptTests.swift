import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// FakeServer answers requests the way a test says, and keeps them.
final class FakeServer: HTTPTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var requests: [URLRequest] = []
    private let answer: @Sendable (URLRequest) -> (Int, String)

    init(answer: @escaping @Sendable (URLRequest) -> (Int, String)) {
        self.answer = answer
    }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        keep(request)
        let (status, body) = answer(request)
        return (Data(body.utf8), HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!)
    }

    private func keep(_ request: URLRequest) {
        lock.lock()
        defer { lock.unlock() }
        requests.append(request)
    }

    var sent: [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return requests
    }
}

func form(_ request: URLRequest) -> [String: String] {
    var fields: [String: String] = [:]
    for pair in String(decoding: request.httpBody ?? Data(), as: UTF8.self).split(separator: "&") {
        let parts = pair.split(separator: "=", maxSplits: 1).map { String($0).removingPercentEncoding ?? String($0) }
        if parts.count == 2 { fields[parts[0]] = parts[1] }
    }
    return fields
}

final class SignInTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    func testTheAppRegistersAndExchangesItsCode() async throws {
        let fake = FakeServer { request in
            switch request.url?.path {
            case "/oauth/register":
                return (201, #"{"client_id":"a-client"}"#)
            case "/oauth/token":
                return (200, #"{"access_token":"tnt_access","refresh_token":"a-refresh","expires_in":2592000,"scope":"api"}"#)
            default:
                return (404, "{}")
            }
        }
        let signIn = SignIn(server: server, transport: fake)
        let clientId = try await signIn.register()
        XCTAssertEqual(clientId, "a-client")
        let registration = try JSONDecoder().decode(JSONValue.self, from: fake.sent[0].httpBody ?? Data())
        XCTAssertEqual(registration["redirect_uris"], .array([.string(appRedirectAddress)]))

        let now = Date(timeIntervalSince1970: 1_000_000)
        let account = try await signIn.exchange(connectCode: "abc123", clientId: clientId, deviceName: "A test phone", now: now)
        XCTAssertEqual(account.accessToken, "tnt_access")
        XCTAssertEqual(account.refreshToken, "a-refresh")
        XCTAssertEqual(account.expiresAt, now.addingTimeInterval(2_592_000))
        let exchange = form(fake.sent[1])
        XCTAssertEqual(exchange["grant_type"], "urn:teanode:params:oauth:grant-type:connect-code")
        XCTAssertEqual(exchange["connect_code"], "abc123")
        XCTAssertEqual(exchange["device_name"], "A test phone")
    }

    func testASpentCodeSaysSo() async {
        let fake = FakeServer { _ in (400, #"{"error":"invalid_grant","error_description":"that connect code has been used"}"#) }
        do {
            _ = try await SignIn(server: server, transport: fake).exchange(connectCode: "used", clientId: "a-client", deviceName: "")
            XCTFail("a spent code was accepted")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .connectCodeSpent)
        }
    }

    func testATokenCloseToRunningOutIsRenewedFirst() async throws {
        let fake = FakeServer { request in
            if request.url?.path == "/oauth/token" {
                return (200, #"{"access_token":"tnt_renewed","refresh_token":"another-refresh","expires_in":2592000}"#)
            }
            return (200, #"{"data":{"GetSession":{"username":"robin"}}}"#)
        }
        let account = Account(server: server, clientId: "a-client", accessToken: "tnt_old", refreshToken: "a-refresh", expiresAt: Date().addingTimeInterval(60), username: "robin")
        let connection = Connection(account: account, transport: fake)
        let username = try await connection.username()
        XCTAssertEqual(username, "robin")
        XCTAssertEqual(fake.sent.map { $0.url?.path }, ["/oauth/token", "/api/v1/graphql"])
        XCTAssertEqual(fake.sent[1].value(forHTTPHeaderField: "Authorization"), "Bearer tnt_renewed")
        let renewed = await connection.account
        XCTAssertEqual(renewed.refreshToken, "another-refresh")
    }

    func testAnErrorFromTheServerIsShownInItsWords() async {
        let fake = FakeServer { _ in (200, #"{"errors":[{"message":"no such conversation"}]}"#) }
        let account = Account(server: server, clientId: "a-client", accessToken: "tnt", refreshToken: "", expiresAt: .distantFuture, username: "robin")
        do {
            _ = try await Connection(account: account, transport: fake).conversations()
            XCTFail("an error came back as data")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .refused("no such conversation"))
        }
    }
}

final class TranscriptTests: XCTestCase {
    func testStoredMessagesBecomeLines() {
        let transcript = Transcript(
            messages: [
                AgentMessage(id: "1", createdAt: "2026-10-10T09:00:00Z", role: "user", content: "What is on today?"),
                AgentMessage(id: "2", createdAt: "2026-10-10T09:00:01Z", role: "assistant", content: "", toolCalls: [AgentToolCall(id: "c1", name: "calendar", arguments: "{}")]),
                AgentMessage(id: "3", createdAt: "2026-10-10T09:00:02Z", role: "tool", content: "Nothing.", toolCallId: "c1"),
                AgentMessage(id: "4", createdAt: "2026-10-10T09:00:03Z", role: "assistant", content: "Nothing at all."),
                AgentMessage(id: "5", createdAt: "2026-10-10T10:00:00Z", role: "user", content: "[alert] mail showed something"),
            ],
            interactions: [
                AgentInteraction(runId: "r", callId: "c2", interactionKind: "approval", toolName: "mail_send", interactionText: "Send the reply?", interactionChoices: nil, createdAt: nil)
            ])
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .tool, .answer, .checkIn, .approval])
        XCTAssertEqual(transcript.lines[1].text, "Nothing.")
        XCTAssertEqual(transcript.lines[1].detail, "calendar")
        XCTAssertEqual(transcript.lines[3].detail, CheckInOrigin.alert.rawValue)
    }

    func testAnAnswerStreamsAndACardIsSettled() {
        var transcript = Transcript(messages: [])
        transcript.apply(AgentEvent(kind: "text", runId: "r", sequence: 1, text: "Hel"))
        transcript.apply(AgentEvent(kind: "text", runId: "r", sequence: 2, text: "lo"))
        XCTAssertEqual(transcript.lines.count, 1)
        XCTAssertEqual(transcript.lines[0].text, "Hello")
        XCTAssertTrue(transcript.lines[0].isStreaming)
        transcript.apply(AgentEvent(kind: "message", runId: "r", sequence: 3, text: "Hello."))
        XCTAssertEqual(transcript.lines[0].text, "Hello.")
        XCTAssertFalse(transcript.lines[0].isStreaming)

        transcript.apply(AgentEvent(kind: "question", runId: "r", sequence: 4, text: "Tomorrow\nFriday", callId: "c", note: "When?"))
        transcript.apply(AgentEvent(kind: "question", runId: "r", sequence: 4, text: "Tomorrow\nFriday", callId: "c", note: "When?"))
        XCTAssertEqual(transcript.lines.count, 2, "a replayed event added a second card")
        XCTAssertEqual(transcript.lines[1].choices, ["Tomorrow", "Friday"])
        transcript.apply(AgentEvent(kind: "tool_result", runId: "r", sequence: 5, text: "Friday", callId: "c"))
        XCTAssertTrue(transcript.lines[1].isDone)
        XCTAssertEqual(transcript.lines[1].resolution, "Friday")
    }

    func testServerTimesAreRead() {
        XCTAssertNotNil(ServerTime.date("2026-10-10T17:58:04.123456789-04:00"))
        XCTAssertNotNil(ServerTime.date("2026-10-10T21:58:04Z"))
        XCTAssertNil(ServerTime.date("yesterday"))
    }

    func testAnUploadIsOneFilePart() {
        let body = String(decoding: Upload.body(boundary: "b", name: "receipt\".jpg", contentType: "image/jpeg", data: Data("x".utf8)), as: UTF8.self)
        XCTAssertTrue(body.hasPrefix("--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"receipt'.jpg\"\r\n"))
        XCTAssertTrue(body.hasSuffix("\r\nx\r\n--b--\r\n"))
    }
}
