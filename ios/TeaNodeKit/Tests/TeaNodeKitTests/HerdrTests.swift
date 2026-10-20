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

    // MARK: Agents

    private func subagentJSON(
        id: String, parent: String? = nil, type: String = "general-purpose", state: String = "done", depth: Int = 1, startedAt: String? = "2026-01-01T10:00:00Z",
        endedAt: String? = "2026-01-01T10:01:30Z"
    ) -> String {
        let parentJSON = parent.map { #""\#($0)""# } ?? "null"
        let startedJSON = startedAt.map { #""\#($0)""# } ?? "null"
        let endedJSON = endedAt.map { #""\#($0)""# } ?? "null"
        return #"{"subagentId":"\#(id)","parentSubagentId":\#(parentJSON),"subagentType":"\#(type)","subagentName":null,"taskText":"Task of \#(id)","modelName":"model-a","subagentState":"\#(state)","spawnDepth":\#(depth),"startedAt":\#(startedJSON),"endedAt":\#(endedJSON),"toolNames":["Read","Grep"],"toolCallCount":7}"#
    }

    private func decodedSubagents(_ list: [String]) throws -> [HerdrSubagent] {
        try list.map { try JSONDecoder().decode(HerdrSubagent.self, from: Data($0.utf8)) }
    }

    func testTheAgentsAreReadAndDecoded() async throws {
        let running = subagentJSON(id: "a2", parent: "a1", type: "Explore", state: "running", depth: 2, endedAt: nil)
        let (connection, fake) = try connection(
            answering: #"{"data":{"ReadAgentHerdrSubagents":{"herdrSession":\#(sessionJSON),"sessionModelName":"model-b","requestStartedAt":"2026-01-01T09:59:00Z","isTruncated":true,"subagents":[\#(subagentJSON(id: "a1")),\#(running)]}}}"#)
        let read = try await connection.readHerdrSubagents(computer: "laptop", paneId: "w1-2")
        XCTAssertEqual(read.sessionModelName, "model-b")
        XCTAssertEqual(read.requestStartedAt, "2026-01-01T09:59:00Z")
        XCTAssertTrue(read.isTruncated)
        XCTAssertTrue(read.herdrSession.isAsking)
        XCTAssertEqual(read.subagents.map(\.subagentId), ["a1", "a2"])
        XCTAssertNil(read.subagents[0].parentId)
        XCTAssertEqual(read.subagents[1].parentId, "a1")
        XCTAssertEqual(read.subagents[1].subagentType, "Explore")
        XCTAssertEqual(read.subagents[1].spawnDepth, 2)
        XCTAssertTrue(read.subagents[1].isRunning)
        XCTAssertNil(read.subagents[1].endedAt)
        XCTAssertEqual(read.subagents[0].toolNames, ["Read", "Grep"])
        XCTAssertEqual(read.subagents[0].toolCallCount, 7)
        XCTAssertEqual(read.runningCount, 1)
        XCTAssertEqual(
            try sentVariables(fake), ["computer": .string("laptop"), "paneId": .string("w1-2")])
        let document = Documents.readHerdrSubagents
        XCTAssertTrue(document.contains("ReadAgentHerdrSubagents("))
        for field in ["subagentId", "parentSubagentId", "subagentType", "subagentName", "taskText", "modelName", "subagentState", "spawnDepth", "startedAt", "endedAt", "toolNames", "toolCallCount", "sessionModelName", "requestStartedAt", "isTruncated"] {
            XCTAssertTrue(document.contains(field), field)
        }
    }

    func testAnEmptyParentMeansTheSessionStartedTheAgent() throws {
        let json = subagentJSON(id: "a1").replacingOccurrences(of: #""parentSubagentId":null"#, with: #""parentSubagentId":"""#)
        let subagent = try JSONDecoder().decode(HerdrSubagent.self, from: Data(json.utf8))
        XCTAssertNil(subagent.parentId)
    }

    func testADurationRunsToTheEndOrToNow() throws {
        let finished = try decodedSubagents([subagentJSON(id: "a1")])[0]
        XCTAssertEqual(finished.duration(now: Date()), 90)
        let running = try decodedSubagents([subagentJSON(id: "a2", state: "running", endedAt: nil)])[0]
        let now = try XCTUnwrap(ServerTime.date("2026-01-01T10:02:00Z"))
        XCTAssertEqual(running.duration(now: now), 120)
        let unstarted = try decodedSubagents([subagentJSON(id: "a3", startedAt: nil, endedAt: nil)])[0]
        XCTAssertNil(unstarted.duration(now: now))
    }

    func testTheTreePutsAgentsUnderTheAgentThatStartedThemInStartOrder() throws {
        let subagents = try decodedSubagents([
            subagentJSON(id: "writer", startedAt: "2026-01-01T10:00:20Z"),
            subagentJSON(id: "finder-two", parent: "researcher", depth: 2, startedAt: "2026-01-01T10:00:15Z"),
            subagentJSON(id: "researcher", startedAt: "2026-01-01T10:00:05Z"),
            subagentJSON(id: "finder-one", parent: "researcher", depth: 2, startedAt: "2026-01-01T10:00:10Z"),
            subagentJSON(id: "checker", parent: "finder-one", depth: 3, startedAt: "2026-01-01T10:00:12Z"),
        ])
        let tree = HerdrSubagentNode.tree(of: subagents)
        XCTAssertEqual(tree.map(\.id), ["researcher", "writer"])
        XCTAssertEqual(tree[0].children.map(\.id), ["finder-one", "finder-two"])
        XCTAssertEqual(tree[0].children[0].children.map(\.id), ["checker"])
        XCTAssertTrue(tree[0].children[1].children.isEmpty)
        XCTAssertTrue(tree[1].children.isEmpty)
        XCTAssertEqual(tree[0].descendantCount, 3)
        XCTAssertEqual(tree[1].descendantCount, 0)
    }

    func testAnAgentWhoseParentIsMissingGoesUnderTheSession() throws {
        let subagents = try decodedSubagents([
            subagentJSON(id: "orphan", parent: "gone", depth: 2, startedAt: "2026-01-01T10:00:01Z"),
            subagentJSON(id: "first", startedAt: "2026-01-01T10:00:00Z"),
            subagentJSON(id: "child", parent: "orphan", depth: 3, startedAt: "2026-01-01T10:00:02Z"),
        ])
        let tree = HerdrSubagentNode.tree(of: subagents)
        XCTAssertEqual(tree.map(\.id), ["first", "orphan"])
        XCTAssertEqual(tree[1].children.map(\.id), ["child"])
    }

    func testAgentsWithoutAStartTimeComeLastInTheOrderGiven() throws {
        let subagents = try decodedSubagents([
            subagentJSON(id: "no-start-one", startedAt: nil, endedAt: nil),
            subagentJSON(id: "started", startedAt: "2026-01-01T10:00:00Z"),
            subagentJSON(id: "no-start-two", startedAt: nil, endedAt: nil),
            subagentJSON(id: "same-time", startedAt: "2026-01-01T10:00:00Z"),
        ])
        XCTAssertEqual(HerdrSubagentNode.tree(of: subagents).map(\.id), ["started", "same-time", "no-start-one", "no-start-two"])
    }

    func testAgentsThatNameEachOtherAsParentsAreNotLost() throws {
        let subagents = try decodedSubagents([
            subagentJSON(id: "loop-a", parent: "loop-b", depth: 2, startedAt: "2026-01-01T10:00:00Z"),
            subagentJSON(id: "loop-b", parent: "loop-a", depth: 2, startedAt: "2026-01-01T10:00:01Z"),
            subagentJSON(id: "self", parent: "self", startedAt: "2026-01-01T10:00:02Z"),
        ])
        let tree = HerdrSubagentNode.tree(of: subagents)
        XCTAssertEqual(tree.map(\.id), ["self", "loop-a"])
        XCTAssertEqual(tree[1].children.map(\.id), ["loop-b"])
        XCTAssertTrue(HerdrSubagentNode.tree(of: []).isEmpty)
    }

    func testEveryDocumentNamesItsOperation() {
        let names = [
            Documents.listHerdrSessions: "ListAgentHerdrSessions", Documents.readHerdrSession: "ReadAgentHerdrSession",
            Documents.readHerdrScreen: "ReadAgentHerdrScreen", Documents.sendHerdrSession: "SendAgentHerdrSession",
            Documents.focusHerdrSession: "FocusAgentHerdrSession", Documents.watchHerdrSession: "WatchAgentHerdrSession",
            Documents.openHerdrSession: "OpenAgentHerdrSession", Documents.closeHerdrSession: "CloseAgentHerdrSession",
            Documents.readHerdrSubagents: "ReadAgentHerdrSubagents",
        ]
        for (document, name) in names {
            XCTAssertTrue(document.contains("\(name)("), name)
            XCTAssertTrue(document.contains("isTabFocused"), name)
        }
    }
}
