import Foundation
import XCTest

@testable import TeaNodeKit

final class CodingSetupTests: XCTestCase {
    private func makeConnection(answering body: String) throws -> (Connection, FakeServer) {
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

    func testSettingUpHooksDecodesWhatWasInstalled() async throws {
        let (connection, fake) = try makeConnection(
            answering: #"{"data":{"SetUpAgentHerdrHooks":{"computer":"laptop","isInstalled":true,"settingsPath":"~/.claude/settings.json","scriptPath":"~/.local/share/teanode/teanode-herdr-hook","backupPath":"~/.claude/settings.json.bak","hookEventNames":["SessionStart","Stop"]}}}"#)
        let setup = try await connection.setUpHerdrHooks(computer: "laptop", isRemoval: false)
        XCTAssertEqual(setup.computer, "laptop")
        XCTAssertTrue(setup.isInstalled)
        XCTAssertEqual(setup.settingsPath, "~/.claude/settings.json")
        XCTAssertEqual(setup.hookEventNames, ["SessionStart", "Stop"])
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["computer"], .string("laptop"))
        XCTAssertNil(variables["isRemoval"])
    }

    func testRemovingHooksSaysSoAndDecodesAnEmptyBackup() async throws {
        let (connection, fake) = try makeConnection(
            answering: #"{"data":{"SetUpAgentHerdrHooks":{"computer":"desk","isInstalled":false,"settingsPath":"~/.claude/settings.json","scriptPath":"","backupPath":"","hookEventNames":[]}}}"#)
        let setup = try await connection.setUpHerdrHooks(computer: "desk", isRemoval: true)
        XCTAssertFalse(setup.isInstalled)
        XCTAssertEqual(setup.backupPath, "")
        XCTAssertEqual(setup.hookEventNames, [])
        XCTAssertEqual(try sentVariables(fake)["isRemoval"], .bool(true))
    }

    func testAPreviewWithoutAPromptReadsTheSessionStartContext() async throws {
        let (connection, fake) = try makeConnection(
            answering: #"{"data":{"ReadAgentCodingContext":{"projectPath":"projects/recipe-box","text":"Project: recipe-box"}}}"#)
        let preview = try await connection.codingContextPreview(directory: "~/src/recipe-box", computerName: "laptop", prompt: nil)
        XCTAssertEqual(preview.projectPath, "projects/recipe-box")
        XCTAssertEqual(preview.text, "Project: recipe-box")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["directory"], .string("~/src/recipe-box"))
        XCTAssertEqual(variables["computerName"], .string("laptop"))
        XCTAssertNil(variables["prompt"])
    }

    func testAPreviewWithAPromptRecallsAndDecodesNothingToShow() async throws {
        let (connection, fake) = try makeConnection(
            answering: #"{"data":{"RecallAgentCodingMemory":{"projectPath":null,"text":""}}}"#)
        let preview = try await connection.codingContextPreview(directory: "~/src/unknown", computerName: nil, prompt: "Fix the footer")
        XCTAssertNil(preview.projectPath)
        XCTAssertEqual(preview.text, "")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["prompt"], .string("Fix the footer"))
        XCTAssertNil(variables["computerName"])
    }

    func testReadingNewSessionsDecodesWhetherASourceWasAsked() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"CaptureAgentCodingSession":true}}"#)
        let isAsked = try await connection.captureCodingSession(computerName: "laptop", assistant: "claude-code")
        XCTAssertTrue(isAsked)
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["computerName"], .string("laptop"))
        XCTAssertEqual(variables["assistant"], .string("claude-code"))

        let (other, _) = try makeConnection(answering: #"{"data":{"CaptureAgentCodingSession":false}}"#)
        let isOtherAsked = try await other.captureCodingSession(computerName: "desk", assistant: "codex")
        XCTAssertFalse(isOtherAsked)
    }
}
