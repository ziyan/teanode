import Foundation

/// What the Coding section sets up and previews beyond the sessions: the
/// reporting hooks teanode puts into Claude Code on a computer, what a coding
/// session is shown, and reading new sessions now. The same operations as
/// the dashboard's Coding page and Knowledge page, and the command line.

/// HerdrHooksSetup says what installing or removing the hooks did on one computer.
public struct HerdrHooksSetup: Decodable, Hashable, Sendable {
    public let computer: String
    public let isInstalled: Bool
    public let settingsPath: String
    public let scriptPath: String
    public let backupPath: String
    public let hookEventNames: [String]
}

/// CodingContextPreview is the text a coding session would be shown.
public struct CodingContextPreview: Decodable, Hashable, Sendable {
    /// The page of the checkout the directory is in; empty where memory knows none.
    public let projectPath: String?
    /// The whole of it as the session reads it; empty where there is nothing to show.
    public let text: String
}

extension Documents {
    public static let setUpHerdrHooks = """
        mutation ($computer: String, $isRemoval: Boolean) {
          SetUpAgentHerdrHooks(computer: $computer, isRemoval: $isRemoval) { computer isInstalled settingsPath scriptPath backupPath hookEventNames }
        }
        """
    public static let readCodingContext = """
        query ($directory: String!, $computerName: String) {
          ReadAgentCodingContext(directory: $directory, computerName: $computerName) { projectPath text }
        }
        """
    public static let recallCodingMemory = """
        query ($prompt: String!, $directory: String!, $computerName: String) {
          RecallAgentCodingMemory(prompt: $prompt, directory: $directory, computerName: $computerName) { projectPath text }
        }
        """
    public static let captureCodingSession = """
        mutation ($computerName: String!, $assistant: String!) {
          CaptureAgentCodingSession(computerName: $computerName, assistant: $assistant)
        }
        """
}

extension Connection {
    /// Puts teanode's hooks into Claude Code's settings on a computer, or
    /// takes them out again with isRemoval.
    public func setUpHerdrHooks(computer: String, isRemoval: Bool) async throws -> HerdrHooksSetup {
        struct Answer: Decodable, Sendable { let SetUpAgentHerdrHooks: HerdrHooksSetup }
        var variables: [String: JSONValue] = ["computer": .string(computer)]
        if isRemoval { variables["isRemoval"] = .bool(true) }
        return try await execute(Documents.setUpHerdrHooks, variables: variables, as: Answer.self).SetUpAgentHerdrHooks
    }

    /// What a session starting in a directory is shown; with a prompt, what
    /// that prompt recalls from the directory's project instead. Nothing is
    /// marked as used.
    public func codingContextPreview(directory: String, computerName: String?, prompt: String?) async throws -> CodingContextPreview {
        var variables: [String: JSONValue] = ["directory": .string(directory)]
        if let computerName { variables["computerName"] = .string(computerName) }
        if let prompt {
            struct RecallAnswer: Decodable, Sendable { let RecallAgentCodingMemory: CodingContextPreview }
            variables["prompt"] = .string(prompt)
            return try await execute(Documents.recallCodingMemory, variables: variables, as: RecallAnswer.self).RecallAgentCodingMemory
        }
        struct StartAnswer: Decodable, Sendable { let ReadAgentCodingContext: CodingContextPreview }
        return try await execute(Documents.readCodingContext, variables: variables, as: StartAnswer.self).ReadAgentCodingContext
    }

    /// Asks the sources reading a coding tool's transcripts on a computer to
    /// read again now. assistant is "claude-code" or "codex". False when no
    /// source fits.
    public func captureCodingSession(computerName: String, assistant: String) async throws -> Bool {
        struct Answer: Decodable, Sendable { let CaptureAgentCodingSession: Bool }
        return try await execute(
            Documents.captureCodingSession, variables: ["computerName": .string(computerName), "assistant": .string(assistant)],
            as: Answer.self
        ).CaptureAgentCodingSession
    }
}
