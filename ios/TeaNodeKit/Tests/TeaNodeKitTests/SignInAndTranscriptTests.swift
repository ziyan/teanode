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

    func testSigningInWithAPasswordAsksForTheWholeAccountAndExchangesTheCode() async throws {
        let signIn = SignIn(server: server, transport: FakeServer { _ in (404, "{}") })
        let address = signIn.authorizationAddress(clientId: "a-client", codeChallenge: "a-challenge", state: "a-state")
        let items = Dictionary(uniqueKeysWithValues: (URLComponents(url: address, resolvingAgainstBaseURL: false)?.queryItems ?? []).map { ($0.name, $0.value ?? "") })
        XCTAssertEqual(address.path, "/oauth/authorize")
        XCTAssertEqual(items["redirect_uri"], appRedirectAddress)
        XCTAssertEqual(items["code_challenge_method"], "S256")
        XCTAssertEqual(items["resource"], "https://mail.example.com/api/v1")

        let returned = URL(string: "com.teanode.app:/oauth?code=a-code&state=a-state")!
        XCTAssertEqual(SignIn.authorizationCode(in: returned, state: "a-state"), "a-code")
        XCTAssertNil(SignIn.authorizationCode(in: returned, state: "another-state"))

        let fake = FakeServer { _ in (200, #"{"access_token":"tnt_access","refresh_token":"a-refresh","expires_in":2592000,"scope":"api"}"#) }
        let account = try await SignIn(server: server, transport: fake).exchange(authorizationCode: "a-code", codeVerifier: "a-verifier", clientId: "a-client")
        XCTAssertEqual(account.accessToken, "tnt_access")
        let sent = form(fake.sent[0])
        XCTAssertEqual(sent["grant_type"], "authorization_code")
        XCTAssertEqual(sent["code_verifier"], "a-verifier")
        XCTAssertEqual(sent["redirect_uri"], appRedirectAddress)
    }

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

    func testACallSaysWhichLanguageTheAppIsShownIn() async throws {
        let fake = FakeServer { _ in (200, #"{"data":{"GetSession":{"username":"robin"}}}"#) }
        let account = Account(server: server, clientId: "a-client", accessToken: "tnt", refreshToken: "", expiresAt: .distantFuture, username: "robin")
        _ = try await Connection(account: account, transport: fake).username()
        XCTAssertEqual(fake.sent[0].value(forHTTPHeaderField: "X-Language"), Connection.shownLanguage())
        XCTAssertEqual(Connection.shownLanguage(preferredLocalizations: ["zh-Hans", "en"]), "zh")
        XCTAssertEqual(Connection.shownLanguage(preferredLocalizations: ["ja"]), "ja")
        XCTAssertEqual(Connection.shownLanguage(preferredLocalizations: ["en"]), "en")
        XCTAssertEqual(Connection.shownLanguage(preferredLocalizations: ["fr"]), "en")
        XCTAssertEqual(Connection.shownLanguage(preferredLocalizations: []), "en")
    }

    func testASignInAnotherProcessRenewedIsTakenUpInsteadOfRenewingAgain() async throws {
        let fake = FakeServer { request in
            if request.url?.path == "/oauth/token" { return (400, #"{"error":"invalid_grant"}"#) }
            return (200, #"{"data":{"GetSession":{"username":"robin"}}}"#)
        }
        let stale = Account(server: server, clientId: "a-client", accessToken: "tnt_old", refreshToken: "spent-refresh", expiresAt: Date().addingTimeInterval(60), username: "robin")
        let newer = Account(server: server, clientId: "a-client", accessToken: "tnt_new", refreshToken: "new-refresh", expiresAt: Date().addingTimeInterval(30 * 86400), username: "robin")
        let connection = Connection(account: stale, transport: fake, store: StoredAccount(account: newer))
        _ = try await connection.username()
        XCTAssertEqual(fake.sent.map { $0.url?.path }, ["/api/v1/graphql"], "no renewal with a refresh token the other process spent")
        XCTAssertEqual(fake.sent[0].value(forHTTPHeaderField: "Authorization"), "Bearer tnt_new")
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

    func testARefusedFieldIsShownInItsWordsNotAsUnreadable() async {
        let fake = FakeServer { _ in
            (200, #"{"errors":[{"message":"that question was already answered"}],"data":{"AnswerAgentHerdrQuestion":null}}"#)
        }
        let account = Account(server: server, clientId: "a-client", accessToken: "tnt", refreshToken: "", expiresAt: .distantFuture, username: "robin")
        let question = CodingQuestion(
            computer: "desk", paneId: "pane-1", questionFingerprint: "print", codingAgentKind: "claude", sessionName: "notes",
            questionText: "Which one?", isMultipleChoice: false, options: [])
        do {
            _ = try await Connection(account: account, transport: fake).answerCodingQuestion(question, options: [], freeText: "the first")
            XCTFail("a refused answer came back as taken")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .refused("that question was already answered"))
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

    func testWhatThisAppSentIsShownOnce() {
        var transcript = Transcript(messages: [])
        transcript.addSent("Is it raining?")
        transcript.apply(AgentEvent(kind: "asked", runId: "r", sequence: 1, text: "Is it raining?"))
        transcript.apply(AgentEvent(kind: "asked", runId: "s", sequence: 1, text: "Asked on the dashboard."))
        transcript.apply(AgentEvent(kind: "asked", runId: "t", sequence: 1, text: "[alert] a parcel arrived"))
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .person, .checkIn])
        XCTAssertEqual(transcript.lines[0].runId, "r")
    }

    func testACodingSessionsQuestionIsACardWithItsOptions() {
        let asked = #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","herdrSession":{"paneName":"site","codingAgentKind":"claude","question":{"questionFingerprint":"f1","questionText":"Deploy now?","isMultipleChoice":false,"options":[{"optionNumber":1,"optionLabel":"Yes","herdrOptionKind":"choice"},{"optionNumber":2,"optionLabel":"Type something","herdrOptionKind":"freeText"}]}}}"#
        let answered = #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","answerText":"1. Yes"}"#
        var messages = [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:00Z", role: "user", content: "[herdr question] a laptop w1:p1 f1\nNobody asked for this."),
            AgentMessage(id: "2", createdAt: "2026-10-10T09:00:01Z", role: "assistant", content: "**Claude Code** in site asks:\n\n> Deploy now?"),
            AgentMessage(id: "3", createdAt: "2026-10-10T09:00:01Z", role: "note", content: asked, name: "herdr_asked"),
        ]
        var transcript = Transcript(messages: messages)
        XCTAssertEqual(transcript.lines.map(\.kind), [.codingQuestion])
        XCTAssertEqual(transcript.lines[0].choices, ["Yes"])
        XCTAssertEqual(transcript.lines[0].codingQuestion?.sessionName, "site")
        XCTAssertEqual(transcript.openQuestion?.id, transcript.lines[0].id)

        messages.append(AgentMessage(id: "4", createdAt: "2026-10-10T09:02:00Z", role: "note", content: answered, name: "herdr_answered"))
        transcript = Transcript(messages: messages)
        XCTAssertTrue(transcript.lines[0].isDone)
        XCTAssertEqual(transcript.lines[0].resolution, "1. Yes")
        XCTAssertNil(transcript.openQuestion)
    }

    func testACodingSessionsQuestionFoldsWhenTheFeedSaysItWent() {
        let asked = #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","herdrSession":{"paneName":"site","codingAgentKind":"codex","question":{"questionFingerprint":"f1","questionText":"Run the tests?","isMultipleChoice":false,"options":[{"optionNumber":1,"optionLabel":"Yes","herdrOptionKind":"choice"},{"optionNumber":2,"optionLabel":"No","herdrOptionKind":"choice"}]}}}"#
        var transcript = Transcript(messages: [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:01Z", role: "note", content: asked, name: "herdr_asked")
        ])
        XCTAssertEqual(transcript.lines[0].codingQuestion?.computer, "a laptop")
        XCTAssertFalse(transcript.lines[0].isDone)

        // Another question's note leaves this card alone.
        transcript.apply(
            AgentEvent(
                kind: "note", runId: "", sequence: 0, noteKind: "herdr_answered",
                noteDetail: #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f2","answerText":"No"}"#))
        XCTAssertFalse(transcript.lines[0].isDone)

        // Answered at the keyboard: the card folds with the answer, live.
        transcript.apply(
            AgentEvent(
                kind: "note", runId: "", sequence: 0, noteKind: "herdr_answered",
                noteDetail: #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","answerText":"1. Yes"}"#))
        XCTAssertTrue(transcript.lines[0].isDone)
        XCTAssertEqual(transcript.lines[0].resolution, "1. Yes")
        XCTAssertNil(transcript.openQuestion)
        XCTAssertEqual(transcript.lines.count, 1, "the note was drawn as a line of its own")

        // A note that knows no answer keeps the one this app gave.
        transcript.settle(cardId: transcript.lines[0].id, resolution: "2. No")
        transcript.apply(
            AgentEvent(
                kind: "note", runId: "", sequence: 0, noteKind: "herdr_answered",
                noteDetail: #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1"}"#))
        XCTAssertEqual(transcript.lines[0].resolution, "2. No")

        // A card settled here, then confirmed: an answer that later fails does not open it again.
        transcript.reopen(cardId: transcript.lines[0].id)
        XCTAssertTrue(transcript.lines[0].isDone, "a confirmed card was opened again")
    }

    func testACardAnsweredHereOpensAgainOnlyIfNothingElseSettledIt() {
        let asked = #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","herdrSession":{"paneName":"site","codingAgentKind":"codex","question":{"questionFingerprint":"f1","questionText":"Run the tests?","isMultipleChoice":false,"options":[{"optionNumber":1,"optionLabel":"Yes","herdrOptionKind":"choice"}]}}}"#
        var transcript = Transcript(messages: [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:01Z", role: "note", content: asked, name: "herdr_asked")
        ])
        guard let cardId = transcript.lines.first?.id else { return XCTFail("the question was not drawn as a card") }
        // Settled as soon as it was tapped; the answer did not go through, so it opens again.
        transcript.settleAwaiting(cardId: cardId, resolution: "Yes")
        transcript.reopen(cardId: cardId)
        XCTAssertFalse(transcript.lines[0].isDone)
        XCTAssertEqual(transcript.lines[0].resolution, "")
        // Settled when tapped, then answered at the computer while the answer was on its way: it stays answered.
        transcript.settleAwaiting(cardId: cardId, resolution: "Yes")
        transcript.apply(
            AgentEvent(
                kind: "note", runId: "", sequence: 0, noteKind: "herdr_answered",
                noteDetail: #"{"computer":"a laptop","paneId":"w1:p1","questionFingerprint":"f1","answerText":"1. Yes"}"#))
        transcript.reopen(cardId: cardId)
        XCTAssertTrue(transcript.lines[0].isDone, "an answer from the computer was undone")
        XCTAssertEqual(transcript.lines[0].resolution, "1. Yes")
    }

    func testAReplayedTurnIsNotDrawnTwice() {
        // Read back after the feed dropped: the person's message and a tool
        // call are stored; the feed then replays the turn from its start.
        var transcript = Transcript(messages: [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:00Z", role: "user", content: "Plan tomorrow."),
            AgentMessage(id: "2", createdAt: "2026-10-10T09:00:01Z", role: "assistant", content: "", toolCalls: [AgentToolCall(id: "c1", name: "calendar", arguments: "{}")]),
        ])
        transcript.apply(AgentEvent(kind: "asked", runId: "r", sequence: 1, text: "Plan tomorrow."))
        transcript.apply(AgentEvent(kind: "tool_call", runId: "r", sequence: 2, tool: "calendar", callId: "c1"))
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .tool])
    }

    func testALongTurnReadBackMidwayIsDrawnOnceWhenItReplays() {
        // Coming back to the app while a turn runs: what it stored so far
        // is read back, more than a few lines of it, a folded note among
        // them, and then the whole turn replays from its start.
        var messages = [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:00Z", role: "user", content: "Archive the notices."),
            AgentMessage(id: "2", createdAt: "2026-10-10T09:00:01Z", role: "assistant", content: "I will check the senders first."),
        ]
        for index in 0..<10 {
            messages.append(AgentMessage(
                id: "t\(index)", createdAt: "2026-10-10T09:00:02Z", role: "assistant", content: "",
                toolCalls: [AgentToolCall(id: "c\(index)", name: "mail_search", arguments: "{}")]))
        }
        messages.append(AgentMessage(id: "n", createdAt: "2026-10-10T09:00:03Z", role: "compaction", content: "The note."))
        var transcript = Transcript(messages: messages)
        transcript.apply(AgentEvent(kind: "asked", runId: "r", sequence: 0, text: "Archive the notices."))
        transcript.apply(AgentEvent(kind: "text", runId: "r", sequence: 1, text: "I will check "))
        transcript.apply(AgentEvent(kind: "text", runId: "r", sequence: 2, text: "the senders first."))
        transcript.apply(AgentEvent(kind: "message", runId: "r", sequence: 3, text: "I will check the senders first."))
        transcript.apply(AgentEvent(kind: "tool_call", runId: "r", sequence: 4, tool: "mail_search", callId: "c0"))
        XCTAssertEqual(transcript.lines.filter { $0.kind == .person }.count, 1)
        XCTAssertEqual(transcript.lines.filter { $0.kind == .answer }.map(\.text), ["I will check the senders first."])
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .answer, .tool])
    }

    func testAFileSentFromThisAppKeepsItsFileWhenTheTurnStarts() {
        var transcript = Transcript(messages: [])
        transcript.addSent("Here it is.", attachments: [AgentAttachment(id: "a1", name: "receipt.jpg", contentType: "image/jpeg", size: 10)])
        transcript.apply(AgentEvent(kind: "asked", runId: "r", sequence: 0, text: "Here it is."))
        XCTAssertEqual(transcript.lines.count, 1)
        XCTAssertEqual(transcript.lines[0].attachments.map(\.id), ["a1"])
        XCTAssertEqual(transcript.lines[0].runId, "r")
    }

    func testSuggestionsAreOfferedAndNotShown() {
        let split = Suggestions.split("Shall I book it?\n<!--suggestions:[\"Yes\",\"Not yet\"]-->")
        XCTAssertEqual(split.text, "Shall I book it?")
        XCTAssertEqual(split.suggestions, ["Yes", "Not yet"])
        XCTAssertEqual(Suggestions.split("Working on it <!--sugg").text, "Working on it")
        let transcript = Transcript(messages: [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:00Z", role: "assistant", content: "Shall I book it?\n<!--suggestions:[\"Yes\",\"Not yet\"]-->")
        ])
        XCTAssertEqual(transcript.offeredSuggestions, ["Yes", "Not yet"])
    }

    func testATurnsUsageIsOnItsLastAnswer() {
        var first = AgentMessage(id: "2", createdAt: "2026-10-10T09:00:01Z", role: "assistant", content: "Looking.")
        first.usage = AgentUsage(promptTokens: 100, completionTokens: 10, cost: 0.01)
        var second = AgentMessage(id: "3", createdAt: "2026-10-10T09:00:02Z", role: "assistant", content: "Done.")
        second.usage = AgentUsage(promptTokens: 200, completionTokens: 20, cost: 0.02)
        let transcript = Transcript(messages: [
            AgentMessage(id: "1", createdAt: "2026-10-10T09:00:00Z", role: "user", content: "Go."), first, second,
        ])
        XCTAssertNil(transcript.lines[1].usage)
        XCTAssertEqual(transcript.lines[2].usage?.promptTokens, 300)
    }

    func testAQueuedTurnSaysSoUntilItBegins() {
        var transcript = Transcript(messages: [])
        transcript.apply(AgentEvent(kind: "note", runId: "r2", sequence: 1, noteKind: "queued"))
        XCTAssertEqual(transcript.lines.first?.isPending, true)
        transcript.apply(AgentEvent(kind: "asked", runId: "r2", sequence: 2, text: "Next thing"))
        XCTAssertEqual(transcript.lines.map(\.kind), [.person])
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

final class MediaTests: XCTestCase {
    func testFilesAToolHandsOverAreRead() {
        let shared = #"<untrusted-data>{"attachment_id":"a1","name":"receipt.jpg","content_type":"image/jpeg","size":2048,"url":"/api/v1/agent/attachments/a1","caption":"The receipt"}</untrusted-data>"#
        XCTAssertEqual(AgentFile.of(toolResult: shared).map(\.name), ["receipt.jpg"])
        XCTAssertEqual(AgentFile.of(toolResult: shared).first?.isImage, true)
        let artifact = #"{"artifact_id":"b2","title":"Spending","kind":"html","url":"/api/v1/agent/attachments/b2"}"#
        XCTAssertEqual(AgentFile.of(toolResult: artifact).first?.artifactKind, "html")
        let elsewhere = #"{"attachment_id":"c3","name":"x","url":"https://example.com/x"}"#
        XCTAssertTrue(AgentFile.of(toolResult: elsewhere).isEmpty)
    }

    func testAPhotoTheAgentShowedKeepsItsIdOnlyFromThePhotoTool() {
        let shown = #"{"files":[{"photo_id":"photo-1_A","attachment_id":"a1","name":"IMG_0001.jpg","content_type":"image/jpeg","size":12,"url":"/api/v1/agent/attachments/a1"}]}"#
        XCTAssertEqual(AgentFile.of(toolResult: shown, tool: "photo").first?.photoId, "photo-1_A")
        XCTAssertNil(AgentFile.of(toolResult: shown, tool: "share_file").first?.photoId)
        XCTAssertNil(AgentFile.of(toolResult: shown).first?.photoId)
        for badId in ["../settings", "a/b", "a?b", "", String(repeating: "x", count: 65)] {
            let result = #"{"files":[{"photo_id":"\#(badId)","attachment_id":"a1","name":"x.jpg","url":"/api/v1/agent/attachments/a1"}]}"#
            let files = AgentFile.of(toolResult: result, tool: "photo")
            XCTAssertEqual(files.count, 1)
            XCTAssertNil(files.first?.photoId, badId)
        }
    }

    func testAPictureOnItsOwnLineIsABlock() {
        XCTAssertEqual(MarkdownBlock.parse("Here it is:\n\n![The park](https://example.com/park.jpg)"), [
            .paragraph("Here it is:"), .image(alt: "The park", address: "https://example.com/park.jpg", link: nil),
        ])
        XCTAssertEqual(Connection.picturePath(conversationId: "c1", address: "https://example.com/a b.jpg"),
            "/api/v1/agent/conversations/c1/picture?url=https%3A%2F%2Fexample.com%2Fa%20b.jpg")
    }
}

final class MarkdownTableTests: XCTestCase {
    func testATableAndARuleAreRead() {
        let text = "Two stops:\n\n| Time | Place |\n|---|:---:|\n| 10:00 | Park |\n| 11:00 | Lunch |\n\n---\nDone."
        XCTAssertEqual(MarkdownBlock.parse(text), [
            .paragraph("Two stops:"),
            .table(header: ["Time", "Place"], rows: [["10:00", "Park"], ["11:00", "Lunch"]]),
            .rule,
            .paragraph("Done."),
        ])
    }
}

final class MarkdownTests: XCTestCase {
    func testAnAnswersBlocksAreRead() {
        let text = "**Claude Code** asks:\n\n> What now?\n> Pick one.\n\n1. Stop\n2. Wait here,\n   then go on\n\n- one\n- two\n\n## Next\n```\nmake test\n```"
        XCTAssertEqual(MarkdownBlock.parse(text), [
            .paragraph("**Claude Code** asks:"),
            .quote("What now?\nPick one."),
            .list([.init(marker: "1.", text: "Stop"), .init(marker: "2.", text: "Wait here, then go on")]),
            .list([.init(marker: "•", text: "one"), .init(marker: "•", text: "two")]),
            .heading("Next"),
            .code("make test"),
        ])
    }
}

final class NotificationTests: XCTestCase {
    func testThePhoneRegistersWithTheRelay() async throws {
        let fake = FakeServer { _ in (200, #"{"pushRegistration":"a-sealed-registration"}"#) }
        let sealed = try await PushRelay(transport: fake).register(deviceToken: Data([0x0a, 0xff]), isSandbox: true)
        XCTAssertEqual(sealed, "a-sealed-registration")
        XCTAssertEqual(fake.sent[0].url?.absoluteString, "https://mail.teanode.com/api/v1/push/registrations")
        let body = try JSONDecoder().decode(JSONValue.self, from: fake.sent[0].httpBody ?? Data())
        XCTAssertEqual(body["deviceToken"], .string("0aff"))
        XCTAssertEqual(body["isSandbox"], .bool(true))
    }

    #if canImport(CryptoKit)
        /// Sealed by the server's own code (push.EncryptContent) with the key
        /// 0, 1, 2 ... 31, so the two sides are checked against each other.
        func testWhatTheServerSealsTheExtensionOpens() throws {
            let key = Data((0..<32).map { UInt8($0) }).base64EncodedString()
            let sealed = "H8RRRtjjpug7CHBstkS2ic1gPC+8v2ETffkv/MTXRhaeqmtyQJlSIczFvR481L+D1BqhO2Yrfj7bLeBSFAqJ6salcR93DzAV0Hleqk+fuO6pBo99yOEC9Th0d3b6jj6DbnbFCqL2MZpVKCm4va3GD+K4WMEw53BqMqCutMuPKvhXNemi9I/xNGgsTk0DwtlofvJaDfsKruPrdHYcbh6GhJnlMoyNoFN89Jyh/44oVcTaSTfMrAwh6hSWET6IAKGuwhUaJl7bpmD3aJJMWjDthHDMClCZB9rbMhezpEl62YAnPDx1iknIqW/yjXFVsqXMv6bv8yMD2jIawXoa2PlPu3qpZXZY6GxXyvgRGazoTJREK2c9gW0="
            let content = try PayloadKey.open(sealed, key: key)
            XCTAssertEqual(content.notificationKind, "question")
            XCTAssertEqual(content.body, "Which day suits you?")
            XCTAssertEqual(content.buttons?.first, NotificationButton(label: "Friday", buttonAction: "answer:1"))
            XCTAssertThrowsError(try PayloadKey.open(sealed, key: PayloadKey.make()))
        }
    #endif

    /// A coding session's question names the session it is from, so that
    /// tapping it opens that session on the Coding screen.
    func testACodingQuestionNotificationNamesItsSession() throws {
        let json = #"{"notificationKind":"herdr_question","computer":"laptop","paneId":"p3","questionFingerprint":"fingerprint-1","title":"notes on laptop asks","body":"Run the tests?","buttons":[{"label":"Open","buttonAction":"open"}],"threadId":"herdr_question:laptop:p3"}"#
        let content = try JSONDecoder().decode(NotificationContent.self, from: Data(json.utf8))
        XCTAssertEqual(content.notificationKind, "herdr_question")
        XCTAssertEqual(content.computer, "laptop")
        XCTAssertEqual(content.paneId, "p3")
        XCTAssertEqual(content.questionFingerprint, "fingerprint-1")
        // Any other notification decodes as before, with no session.
        let other = try JSONDecoder().decode(NotificationContent.self, from: Data(#"{"notificationKind":"message","title":"Tea","body":"Hello"}"#.utf8))
        XCTAssertNil(other.paneId)
    }
}

final class VoiceTests: XCTestCase {
    func testAnAnswerIsSaidWithoutItsMarks() {
        let said = SpokenText.of("## Tomorrow\n\n- **9:00** Swim. See [the map](https://example.com).\n\n```\ncode\n```\nDone.\n<!--suggestions:[\"Yes\",\"No\"]-->")
        XCTAssertEqual(said, "Tomorrow 9:00 Swim. See the map. Done.")
    }

    func testOnlyWholeSentencesAreSpoken() {
        let split = SpokenText.sentences(in: "It is sunny. Bring a hat! And a coa")
        XCTAssertEqual(split.sentences, ["It is sunny.", "Bring a hat!"])
        XCTAssertEqual(String(Array("It is sunny. Bring a hat! And a coa")[split.consumed...]).trimmingCharacters(in: .whitespaces), "And a coa")
    }

    func testAVoiceEventIsRead() throws {
        let event = try JSONDecoder().decode(VoiceEvent.self, from: Data(#"{"voiceEvent":"transcriptFinal","utteranceId":"u1","transcriptText":"Hello"}"#.utf8))
        XCTAssertEqual(event.transcriptText, "Hello")
    }
}

final class CalendarTests: XCTestCase {
    func testAnAllDayEventIsReadInUTC() {
        // Midnight UTC of the 11th is the 10th in New York; the event is on
        // the 11th all the same.
        let day = CalendarDates.day(ofAllDay: "2026-10-11T00:00:00Z")
        XCTAssertEqual(day?.day, 11)
        XCTAssertEqual(CalendarDates.allDayText(DateComponents(year: 2026, month: 10, day: 11)), "2026-10-11T00:00:00Z")
        XCTAssertEqual(CalendarDates.dayAfter(DateComponents(year: 2026, month: 10, day: 31)).month, 11)
        // An all-day event's end is the morning after its last day.
        XCTAssertEqual(CalendarDates.lastDay(startsAt: "2026-10-11T00:00:00Z", endsAt: "2026-10-13T00:00:00Z")?.day, 12)
        XCTAssertEqual(CalendarDates.lastDay(startsAt: "2026-10-11T00:00:00Z", endsAt: "2026-10-11T00:00:00Z")?.day, 11)
    }

    func testAChangeIsAskedAboutBeforeItIsSentAgain() async throws {
        let server = try ServerAddress(parsing: "mail.example.com")
        let fake = FakeServer { request in
            let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            if body.contains("GetCalendarRequest") { return (200, #"{"data":{"GetCalendarRequest":{"requestId":"r","isMissing":false}}}"#) }
            return (200, #"{"data":{"SaveCalendarEvent":{"id":"e1"}}}"#)
        }
        let account = Account(server: server, clientId: "c", accessToken: "t", refreshToken: "", expiresAt: .distantFuture, username: "robin")
        let connection = Connection(account: account, transport: fake)
        let change = CalendarChange(operation: "save", summary: "Lunch", variables: ["calendarId": .string("k")])
        try await connection.perform(change, isRetry: true)
        XCTAssertEqual(fake.sent.count, 1, "a change that had happened was sent again")
        XCTAssertEqual(change.requestId.count, 32)
    }
}

/// StoredAccount is a sign-in another process has stored.
struct StoredAccount: AccountStore {
    let account: Account
    func save(_ account: Account) throws {}
    func load() throws -> Account? { account }
    func delete() throws {}
}
