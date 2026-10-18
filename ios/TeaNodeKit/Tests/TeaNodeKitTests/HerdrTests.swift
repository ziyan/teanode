import Foundation
import XCTest

@testable import TeaNodeKit

final class HerdrTests: XCTestCase {
    private let sessionJSON = """
        {"computer":"laptop","paneId":"w1-2","paneName":"website 2 claude","codingAgentKind":"claude","codingSessionId":"s1",
         "herdrSessionState":"asking","herdrAgentStatus":"blocked","paneTitle":"","workingDirectory":"/work/website","transcriptPath":"",
         "isWatched":false,"workspaceId":"w1","workspaceLabel":"website","workspaceNumber":1,"tabId":"w1:2","tabLabel":"asking","tabNumber":2,
         "isTabFocused":true,
         "question":{"questionFingerprint":"f1","herdrQuestionKind":"question","questionText":"Which fruit?","isMultipleChoice":false,
           "isFromTranscript":false,"options":[
             {"optionNumber":1,"optionLabel":"Apple","optionDescription":"","herdrOptionKind":"choice"},
             {"optionNumber":2,"optionLabel":"Type something","optionDescription":"","herdrOptionKind":"freeText"}]}}
        """

    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ fake: FakeServer) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.last?.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    func testTheListDecodesSessionsWithTheirPlacement() async throws {
        let (connection, fake) = try connection(
            answering: #"{"data":{"ListAgentHerdrSessions":{"computerNames":["laptop","desk"],"failedComputerNames":["desk"],"sessions":[\#(sessionJSON)]}}}"#)
        let list = try await connection.herdrSessions()
        XCTAssertEqual(list.computerNames, ["laptop", "desk"])
        XCTAssertEqual(list.failedComputerNames, ["desk"])
        let session = try XCTUnwrap(list.sessions.first)
        XCTAssertEqual(session.workspaceLabel, "website")
        XCTAssertEqual(session.tabNumber, 2)
        XCTAssertTrue(session.isTabFocused)
        XCTAssertTrue(session.isAsking)
        XCTAssertEqual(session.tabTitle, "asking")
        XCTAssertEqual(try sentVariables(fake), [:])
    }

    func testARowLeadsWithWhatTheSessionIsAbout() throws {
        func decoded(paneTitle: String, tabLabel: String) throws -> HerdrSession {
            let json = sessionJSON
                .replacingOccurrences(of: #""paneTitle":"""#, with: #""paneTitle":"\#(paneTitle)""#)
                .replacingOccurrences(of: #""tabLabel":"asking""#, with: #""tabLabel":"\#(tabLabel)""#)
            return try JSONDecoder().decode(HerdrSession.self, from: Data(json.utf8))
        }
        let titled = try decoded(paneTitle: "Fix the footer", tabLabel: "2")
        XCTAssertEqual(titled.rowTitle, "Fix the footer")
        XCTAssertEqual(titled.rowDetail, "tab 2 · Claude Code")
        let prompt = try decoded(paneTitle: "robin@laptop: ~/work/website", tabLabel: "2")
        XCTAssertEqual(prompt.rowTitle, "tab 2")
        XCTAssertEqual(prompt.rowDetail, "Claude Code")
    }

    func testTheQuestionKeepsItsAnsweringPathWithTheConversationsCard() async throws {
        let (connection, _) = try connection(
            answering: #"{"data":{"ListAgentHerdrSessions":{"computerNames":["laptop"],"failedComputerNames":[],"sessions":[\#(sessionJSON)]}}}"#)
        let list = try await connection.herdrSessions(computer: "laptop")
        let session = try XCTUnwrap(list.sessions.first)
        let question = try XCTUnwrap(session.codingQuestion)
        XCTAssertEqual(question.computer, "laptop")
        XCTAssertEqual(question.paneId, "w1-2")
        XCTAssertEqual(question.questionFingerprint, "f1")
        XCTAssertEqual(question.choices.map(\.optionLabel), ["Apple"])
    }

    func testAReadAsksForTheTurnsAndDecodesThem() async throws {
        let (connection, fake) = try connection(
            answering: #"{"data":{"ReadAgentHerdrSession":{"herdrSession":\#(sessionJSON),"turns":[{"herdrTurnRole":"person","turnText":"hello","turnAt":"2026-01-01T00:00:00Z"},{"herdrTurnRole":"tool","turnText":"Read a file","turnAt":null,"toolInputText":"{\"file_path\": \"a.txt\"}","toolResultText":"hello"},{"herdrTurnRole":"tool","turnText":"Ran a command","hasToolDetail":true}],"isTruncated":true}}}"#)
        let read = try await connection.readHerdrSession(computer: "laptop", paneId: "w1-2", turnCount: 50, shouldIncludeToolDetail: true)
        XCTAssertEqual(read.turns.map(\.herdrTurnRole), ["person", "tool", "tool"])
        XCTAssertNil(read.turns[1].turnAt)
        XCTAssertFalse(read.turns[0].hasToolDetail)
        XCTAssertTrue(read.turns[1].hasToolDetail)
        XCTAssertEqual(read.turns[1].toolResultText, "hello")
        // A read without the detail still says there is some to fetch.
        XCTAssertTrue(read.turns[2].hasToolDetail)
        XCTAssertFalse(read.turns[2].isToolDetailLoaded)
        XCTAssertTrue(read.isTruncated)
        XCTAssertEqual(
            try sentVariables(fake),
            ["computer": .string("laptop"), "paneId": .string("w1-2"), "turnCount": .number(50), "shouldIncludeToolDetail": .bool(true)])
    }

    func testTheScreenIsRead() async throws {
        let (connection, fake) = try connection(
            answering: #"{"data":{"ReadAgentHerdrScreen":{"herdrSession":\#(sessionJSON),"screenText":"$ ready"}}}"#)
        let screen = try await connection.readHerdrScreen(computer: "laptop", paneId: "w1-2")
        XCTAssertEqual(screen.screenText, "$ ready")
        XCTAssertEqual(try sentVariables(fake), ["computer": .string("laptop"), "paneId": .string("w1-2"), "shouldKeepColor": .bool(true)])
    }

    func testSendCarriesTheTextAndOnlyTheAttachmentsThereAre() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"SendAgentHerdrSession":\#(sessionJSON)}}"#)
        try await connection.sendHerdrSession(computer: "laptop", paneId: "w1-2", text: "go on")
        XCTAssertEqual(
            try sentVariables(fake), ["computer": .string("laptop"), "paneId": .string("w1-2"), "text": .string("go on")])
        try await connection.sendHerdrSession(computer: "laptop", paneId: "w1-2", text: "see", attachmentIds: ["a1", "a2"])
        XCTAssertEqual(try sentVariables(fake)["attachmentIds"], .array([.string("a1"), .string("a2")]))
    }

    func testFocusWatchAndCloseNameTheSession() async throws {
        let expected: [String: JSONValue] = ["computer": .string("laptop"), "paneId": .string("w1-2")]
        let (focusing, focusFake) = try connection(answering: #"{"data":{"FocusAgentHerdrSession":\#(sessionJSON)}}"#)
        try await focusing.focusHerdrSession(computer: "laptop", paneId: "w1-2")
        XCTAssertEqual(try sentVariables(focusFake), expected)
        let (watching, watchFake) = try connection(answering: #"{"data":{"WatchAgentHerdrSession":\#(sessionJSON)}}"#)
        try await watching.watchHerdrSession(computer: "laptop", paneId: "w1-2")
        XCTAssertEqual(try sentVariables(watchFake), expected)
        let (closing, closeFake) = try connection(answering: #"{"data":{"CloseAgentHerdrSession":\#(sessionJSON)}}"#)
        try await closing.closeHerdrSession(computer: "laptop", paneId: "w1-2")
        XCTAssertEqual(try sentVariables(closeFake), expected)
    }

    func testOpenBesideAPaneSendsThePaneAndLeavesTheRestToTheServer() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"OpenAgentHerdrSession":\#(sessionJSON)}}"#)
        try await connection.openHerdrSession(computer: "laptop", paneId: "w1-2", codingAgentKind: "codex")
        XCTAssertEqual(
            try sentVariables(fake),
            ["computer": .string("laptop"), "paneId": .string("w1-2"), "codingAgentKind": .string("codex")])
    }

    func testOpenInADirectoryCarriesTheDirectoryAndSkipsPermissionsOnlyWhenAsked() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"OpenAgentHerdrSession":\#(sessionJSON)}}"#)
        try await connection.openHerdrSession(computer: "laptop", directory: "/work/notes", codingAgentKind: "claude")
        let plain = try sentVariables(fake)
        XCTAssertEqual(plain["directory"], .string("/work/notes"))
        XCTAssertNil(plain["paneId"])
        XCTAssertNil(plain["shouldSkipPermissions"])
        try await connection.openHerdrSession(computer: "laptop", directory: "/work/notes", codingAgentKind: "claude", shouldSkipPermissions: true)
        XCTAssertEqual(try sentVariables(fake)["shouldSkipPermissions"], .bool(true))
    }

    func testEveryDocumentNamesItsOperation() {
        let names = [
            Documents.listHerdrSessions: "ListAgentHerdrSessions", Documents.readHerdrSession: "ReadAgentHerdrSession",
            Documents.readHerdrScreen: "ReadAgentHerdrScreen", Documents.sendHerdrSession: "SendAgentHerdrSession",
            Documents.focusHerdrSession: "FocusAgentHerdrSession", Documents.watchHerdrSession: "WatchAgentHerdrSession",
            Documents.openHerdrSession: "OpenAgentHerdrSession", Documents.closeHerdrSession: "CloseAgentHerdrSession",
        ]
        for (document, name) in names {
            XCTAssertTrue(document.contains("\(name)("), name)
            XCTAssertTrue(document.contains("isTabFocused"), name)
        }
    }
}
