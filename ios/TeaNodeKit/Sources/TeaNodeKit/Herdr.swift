import Foundation

/// The person's herdr sessions: the Claude Code and Codex sessions kept in
/// herdr's panes on each attached computer, which the agent works in beside
/// them. The same operations as the dashboard's Coding page and the agent's
/// herdr tool.

/// HerdrQuestionOption is one option of a question a session waits on.
public struct HerdrQuestionOption: Decodable, Hashable, Sendable {
    public let optionNumber: Int
    public let optionLabel: String
    public let optionDescription: String
    /// "choice", "freeText" (it takes typed text) or "chat".
    public let herdrOptionKind: String
}

/// HerdrQuestion is a question a session waits on.
public struct HerdrQuestion: Decodable, Hashable, Sendable {
    public let questionFingerprint: String
    /// "question", "toolApproval" or "planApproval".
    public let herdrQuestionKind: String
    public let questionText: String
    public let isMultipleChoice: Bool
    public let isFromTranscript: Bool
    public let options: [HerdrQuestionOption]
}

/// HerdrSession is one coding session in one herdr pane.
public struct HerdrSession: Decodable, Hashable, Sendable, Identifiable {
    public let computer: String
    public let paneId: String
    /// The pane as the person finds it in herdr: workspace, tab, agent.
    public let paneName: String
    /// "claude" or "codex".
    public let codingAgentKind: String
    public let codingSessionId: String
    /// "idle", "working", "asking" or "unknown".
    public let herdrSessionState: String
    public let herdrAgentStatus: String
    public let paneTitle: String
    public let workingDirectory: String
    public let transcriptPath: String
    public let isWatched: Bool
    public let workspaceId: String
    public let workspaceLabel: String
    public let workspaceNumber: Int
    public let tabId: String
    public let tabLabel: String
    public let tabNumber: Int
    /// herdr shows this session's tab on the computer's screen now.
    public let isTabFocused: Bool
    public let question: HerdrQuestion?

    /// Two computers can each have a pane of the same id.
    public var id: String { "\(computer)/\(paneId)" }

    public var codingAgentName: String { codingAgentKind == "codex" ? "Codex" : "Claude Code" }

    public var isWorking: Bool { herdrSessionState == "working" }
    public var isAsking: Bool { herdrSessionState == "asking" && question != nil }

    /// What names the session in a list or a tab: its tab label, or the
    /// pane's name, or the directory it works in.
    public var tabTitle: String {
        if !tabLabel.isEmpty { return tabLabel }
        if !paneName.isEmpty { return paneName }
        let directoryName = (workingDirectory as NSString).lastPathComponent
        return directoryName.isEmpty ? paneId : directoryName
    }

    /// What the session is about: the title its coding agent gave its pane,
    /// unless that is only the shell's prompt (user@host: path) or the
    /// workspace's name, which say nothing of the session.
    public var sessionTitle: String {
        let title = paneTitle.trimmingCharacters(in: .whitespaces)
        if title.isEmpty || title == workspaceLabel { return "" }
        if title.range(of: #"^[^\s@]+@[^\s:]+:"#, options: .regularExpression) != nil { return "" }
        return title
    }

    /// A tab's words: herdr numbers unnamed tabs, and a bare number reads
    /// better as "tab 2".
    private var tabWords: String {
        if tabLabel.isEmpty { return workspaceId.isEmpty ? paneName : "" }
        return Int(tabLabel) != nil ? "tab \(tabLabel)" : tabLabel
    }

    /// The first line of the session's row: what it is about, or where it is.
    public var rowTitle: String {
        let title = sessionTitle
        if !title.isEmpty { return title }
        return tabWords.isEmpty ? codingAgentName : tabWords
    }

    /// The second line of the session's row: its tab and coding agent.
    public var rowDetail: String {
        if sessionTitle.isEmpty { return tabWords.isEmpty ? "" : codingAgentName }
        return [tabWords, codingAgentName].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// The session's chip in the tab strip: herdr's label for its tab, then
    /// what it is about.
    public var chipTitle: String {
        let words = [tabLabel, sessionTitle].filter { !$0.isEmpty }.joined(separator: " ")
        return words.isEmpty ? codingAgentName : words
    }

    /// What names the workspace in a list or a bar.
    public var workspaceTitle: String {
        if !workspaceLabel.isEmpty { return workspaceLabel }
        let directoryName = (workingDirectory as NSString).lastPathComponent
        return directoryName.isEmpty ? computer : directoryName
    }

    /// The question as the conversation's card holds it, so one answering
    /// path serves both.
    public var codingQuestion: CodingQuestion? {
        guard let question else { return nil }
        return CodingQuestion(
            computer: computer, paneId: paneId, questionFingerprint: question.questionFingerprint,
            codingAgentKind: codingAgentKind, sessionName: tabTitle, questionText: question.questionText,
            isMultipleChoice: question.isMultipleChoice,
            options: question.options.map {
                CodingQuestion.Option(optionNumber: $0.optionNumber, optionLabel: $0.optionLabel, optionDescription: $0.optionDescription, herdrOptionKind: $0.herdrOptionKind)
            })
    }
}

/// HerdrList is every session on every computer asked.
public struct HerdrList: Decodable, Hashable, Sendable {
    public let computerNames: [String]
    /// The computers that did not answer, whose sessions are not listed.
    public let failedComputerNames: [String]
    public let sessions: [HerdrSession]

    public init(computerNames: [String], failedComputerNames: [String], sessions: [HerdrSession]) {
        self.computerNames = computerNames
        self.failedComputerNames = failedComputerNames
        self.sessions = sessions
    }
}

/// HerdrTurn is one thing said or done in a session.
public struct HerdrTurn: Decodable, Hashable, Sendable {
    /// "person", "agent" or "tool".
    public let herdrTurnRole: String
    public let turnText: String
    public let turnAt: String?
}

/// HerdrRead is a session's last turns.
public struct HerdrRead: Decodable, Hashable, Sendable {
    public let herdrSession: HerdrSession
    public let turns: [HerdrTurn]
    public let isTruncated: Bool
}

/// HerdrScreen is a session's screen as it stands.
public struct HerdrScreen: Decodable, Hashable, Sendable {
    public let herdrSession: HerdrSession
    public let screenText: String
}

extension Documents {
    static let herdrSessionFields = """
        computer paneId paneName codingAgentKind codingSessionId herdrSessionState herdrAgentStatus paneTitle workingDirectory transcriptPath isWatched
        workspaceId workspaceLabel workspaceNumber tabId tabLabel tabNumber isTabFocused
        question { questionFingerprint herdrQuestionKind questionText isMultipleChoice isFromTranscript options { optionNumber optionLabel optionDescription herdrOptionKind } }
        """
    public static let listHerdrSessions = """
        query ($computer: String) {
          ListAgentHerdrSessions(computer: $computer) { computerNames failedComputerNames sessions { \(herdrSessionFields) } }
        }
        """
    public static let readHerdrSession = """
        query ($computer: String, $paneId: String!, $turnCount: Int) {
          ReadAgentHerdrSession(computer: $computer, paneId: $paneId, turnCount: $turnCount) {
            herdrSession { \(herdrSessionFields) } turns { herdrTurnRole turnText turnAt } isTruncated
          }
        }
        """
    public static let readHerdrScreen = """
        query ($computer: String, $paneId: String!) {
          ReadAgentHerdrScreen(computer: $computer, paneId: $paneId) { herdrSession { \(herdrSessionFields) } screenText }
        }
        """
    public static let sendHerdrSession = """
        mutation ($computer: String, $paneId: String!, $text: String, $attachmentIds: [String!]) {
          SendAgentHerdrSession(computer: $computer, paneId: $paneId, text: $text, attachmentIds: $attachmentIds) { \(herdrSessionFields) }
        }
        """
    public static let focusHerdrSession = """
        mutation ($computer: String, $paneId: String!) {
          FocusAgentHerdrSession(computer: $computer, paneId: $paneId) { \(herdrSessionFields) }
        }
        """
    public static let watchHerdrSession = """
        mutation ($computer: String, $paneId: String!) {
          WatchAgentHerdrSession(computer: $computer, paneId: $paneId) { \(herdrSessionFields) }
        }
        """
    public static let openHerdrSession = """
        mutation ($computer: String, $directory: String, $codingAgentKind: String, $agentName: String, $shouldSkipPermissions: Boolean, $paneId: String) {
          OpenAgentHerdrSession(computer: $computer, directory: $directory, codingAgentKind: $codingAgentKind, agentName: $agentName, shouldSkipPermissions: $shouldSkipPermissions, paneId: $paneId) { \(herdrSessionFields) }
        }
        """
    public static let closeHerdrSession = """
        mutation ($computer: String, $paneId: String!) {
          CloseAgentHerdrSession(computer: $computer, paneId: $paneId) { \(herdrSessionFields) }
        }
        """
}

extension Connection {
    /// Every coding session on the person's computers, or on the one named.
    public func herdrSessions(computer: String? = nil) async throws -> HerdrList {
        struct Answer: Decodable, Sendable { let ListAgentHerdrSessions: HerdrList }
        var variables: [String: JSONValue] = [:]
        if let computer { variables["computer"] = .string(computer) }
        return try await execute(Documents.listHerdrSessions, variables: variables, as: Answer.self).ListAgentHerdrSessions
    }

    /// The last turnCount turns of a session (100 at most).
    public func readHerdrSession(computer: String, paneId: String, turnCount: Int) async throws -> HerdrRead {
        struct Answer: Decodable, Sendable { let ReadAgentHerdrSession: HerdrRead }
        return try await execute(
            Documents.readHerdrSession,
            variables: ["computer": .string(computer), "paneId": .string(paneId), "turnCount": .number(Double(turnCount))],
            as: Answer.self
        ).ReadAgentHerdrSession
    }

    /// A session's screen as it stands.
    public func readHerdrScreen(computer: String, paneId: String) async throws -> HerdrScreen {
        struct Answer: Decodable, Sendable { let ReadAgentHerdrScreen: HerdrScreen }
        return try await execute(
            Documents.readHerdrScreen, variables: ["computer": .string(computer), "paneId": .string(paneId)], as: Answer.self
        ).ReadAgentHerdrScreen
    }

    /// Types text into a session and presses enter, with files the person
    /// uploaded already (their ids) sent along.
    @discardableResult
    public func sendHerdrSession(computer: String, paneId: String, text: String, attachmentIds: [String] = []) async throws -> HerdrSession {
        struct Answer: Decodable, Sendable { let SendAgentHerdrSession: HerdrSession }
        var variables: [String: JSONValue] = ["computer": .string(computer), "paneId": .string(paneId), "text": .string(text)]
        if !attachmentIds.isEmpty { variables["attachmentIds"] = .array(attachmentIds.map { .string($0) }) }
        return try await execute(Documents.sendHerdrSession, variables: variables, as: Answer.self).SendAgentHerdrSession
    }

    /// Brings a session's tab forward in herdr on its computer, so whoever
    /// sits there sees it.
    @discardableResult
    public func focusHerdrSession(computer: String, paneId: String) async throws -> HerdrSession {
        struct Answer: Decodable, Sendable { let FocusAgentHerdrSession: HerdrSession }
        return try await execute(
            Documents.focusHerdrSession, variables: ["computer": .string(computer), "paneId": .string(paneId)], as: Answer.self
        ).FocusAgentHerdrSession
    }

    /// Wakes the person's main conversation when the session next finishes
    /// its turn.
    @discardableResult
    public func watchHerdrSession(computer: String, paneId: String) async throws -> HerdrSession {
        struct Answer: Decodable, Sendable { let WatchAgentHerdrSession: HerdrSession }
        return try await execute(
            Documents.watchHerdrSession, variables: ["computer": .string(computer), "paneId": .string(paneId)], as: Answer.self
        ).WatchAgentHerdrSession
    }

    /// Starts a coding agent in a new pane. Given paneId it opens a new tab
    /// beside that pane, in its directory with its agent unless directory
    /// and codingAgentKind say otherwise; without it, directory and
    /// codingAgentKind say where and which.
    @discardableResult
    public func openHerdrSession(
        computer: String, paneId: String? = nil, directory: String? = nil, codingAgentKind: String? = nil, agentName: String? = nil,
        shouldSkipPermissions: Bool = false
    ) async throws -> HerdrSession {
        struct Answer: Decodable, Sendable { let OpenAgentHerdrSession: HerdrSession }
        var variables: [String: JSONValue] = ["computer": .string(computer)]
        if let paneId { variables["paneId"] = .string(paneId) }
        if let directory { variables["directory"] = .string(directory) }
        if let codingAgentKind { variables["codingAgentKind"] = .string(codingAgentKind) }
        if let agentName { variables["agentName"] = .string(agentName) }
        if shouldSkipPermissions { variables["shouldSkipPermissions"] = .bool(true) }
        return try await execute(Documents.openHerdrSession, variables: variables, as: Answer.self).OpenAgentHerdrSession
    }

    /// Ends a session's coding agent and closes its pane; refused while it
    /// works.
    @discardableResult
    public func closeHerdrSession(computer: String, paneId: String) async throws -> HerdrSession {
        struct Answer: Decodable, Sendable { let CloseAgentHerdrSession: HerdrSession }
        return try await execute(
            Documents.closeHerdrSession, variables: ["computer": .string(computer), "paneId": .string(paneId)], as: Answer.self
        ).CloseAgentHerdrSession
    }
}
