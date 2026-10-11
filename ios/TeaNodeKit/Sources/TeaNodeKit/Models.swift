import Foundation

// What the server's GraphQL API answers with, as the app reads it. The field
// names are the server's (internal/api/v1api/apigraph), so they decode
// without mapping; times stay the server's RFC 3339 text and are read with
// `ServerTime.date(_:)` where a screen needs a date.

/// AgentConversation is one conversation with the agent: the main one, or a
/// named one.
public struct AgentConversation: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// "main", "named", "run" or "goal".
    public let kind: String
    /// For a run, what kind of job it was, such as "mcp".
    public var jobKind: String? = nil
    public let title: String?
    public let summary: String?
    public let lastAt: String?
    public var archivedAt: String? = nil
    /// For a goal conversation: the goal, where it stands ("working",
    /// "waiting", "met", "dropped"), and its last note.
    public var goal: String? = nil
    public var goalState: String? = nil
    public var goalNote: String? = nil

    public init(id: String, kind: String, title: String?, summary: String?, lastAt: String?) {
        self.id = id
        self.kind = kind
        self.title = title
        self.summary = summary
        self.lastAt = lastAt
    }

    public var isMain: Bool { kind == "main" }
    public var isArchived: Bool { !(archivedAt ?? "").isEmpty }
}

/// AgentUsage is what one round of a turn cost.
public struct AgentUsage: Codable, Hashable, Sendable {
    public var promptTokens: Int
    public var completionTokens: Int
    public var cost: Double

    public init(promptTokens: Int = 0, completionTokens: Int = 0, cost: Double = 0) {
        self.promptTokens = promptTokens
        self.completionTokens = completionTokens
        self.cost = cost
    }

    static func + (left: AgentUsage, right: AgentUsage) -> AgentUsage {
        AgentUsage(
            promptTokens: left.promptTokens + right.promptTokens, completionTokens: left.completionTokens + right.completionTokens,
            cost: left.cost + right.cost)
    }
}

/// AgentReference is something a message points at: a mail thread, a
/// memory page, a finance transaction, or an earlier message replied to.
public struct AgentReference: Codable, Hashable, Sendable {
    public var itemId: String? = nil
    public var threadId: String? = nil
    public var subject: String? = nil
    public var from: String? = nil
    public var path: String? = nil
    public var name: String? = nil
    public var financeTransactionId: String? = nil
    public var postedOn: String? = nil
    public var amount: String? = nil
    public var currencyCode: String? = nil
    public var merchantName: String? = nil
    public var description: String? = nil
    public var agentMessageId: String? = nil
    /// "user" or "assistant": whose message was replied to.
    public var quotedRole: String? = nil
    public var quotedText: String? = nil

    /// A finance transaction the message asks about.
    public init(transactionId: String, postedOn: String?, amount: String?, currencyCode: String?, merchantName: String?, description: String?) {
        financeTransactionId = transactionId
        self.postedOn = postedOn
        self.amount = amount
        self.currencyCode = currencyCode
        self.merchantName = merchantName
        self.description = description
    }

    /// A page of the agent's knowledge the message asks about.
    public init(path: String, name: String) {
        self.path = path
        self.name = name
    }

    /// A mail the message asks about.
    public init(itemId: String, threadId: String?, subject: String, from: String) {
        self.itemId = itemId
        self.threadId = threadId
        self.subject = subject
        self.from = from
    }

    public init(agentMessageId: String, quotedRole: String, quotedText: String) {
        self.agentMessageId = agentMessageId
        self.quotedRole = quotedRole
        self.quotedText = quotedText
    }

    /// Whether this is a reply to an earlier message, not a thing pointed at.
    public var isReply: Bool { !(agentMessageId ?? "").isEmpty }

    /// What the reference's chip says.
    public var label: String {
        if let subject, !subject.isEmpty { return subject }
        if let name, !name.isEmpty { return name }
        if let path, !path.isEmpty { return path }
        if let merchantName, !merchantName.isEmpty {
            return [postedOn, merchantName, amount].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
        }
        return description ?? ""
    }
}

/// AgentTodo is one step of the agent's own list for a conversation.
public struct AgentTodo: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let text: String
    public let doneAt: String?

    public var isDone: Bool { !(doneAt ?? "").isEmpty }
}

/// AgentToolCall is a tool the agent called in one round.
public struct AgentToolCall: Codable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let arguments: String?
}

/// AgentAttachment is a file a person handed the agent with a message.
public struct AgentAttachment: Codable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let contentType: String
    public let size: Int

    public init(id: String, name: String, contentType: String, size: Int) {
        self.id = id
        self.name = name
        self.contentType = contentType
        self.size = size
    }
}

/// AgentMessage is one stored message of a conversation.
public struct AgentMessage: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let createdAt: String
    /// "user", "assistant", "tool", "note" or "compaction".
    public let role: String
    public let content: String
    public let name: String?
    public let toolCallId: String?
    public let toolCalls: [AgentToolCall]?
    public let attachments: [AgentAttachment]?
    public var usage: AgentUsage? = nil
    public var references: [AgentReference]? = nil

    public init(
        id: String, createdAt: String, role: String, content: String, name: String? = nil, toolCallId: String? = nil,
        toolCalls: [AgentToolCall]? = nil, attachments: [AgentAttachment]? = nil
    ) {
        self.id = id
        self.createdAt = createdAt
        self.role = role
        self.content = content
        self.name = name
        self.toolCallId = toolCallId
        self.toolCalls = toolCalls
        self.attachments = attachments
    }
}

/// AgentInteraction is a card left open for the person: an approval, or a
/// question the agent asked.
public struct AgentInteraction: Codable, Hashable, Sendable {
    public let runId: String
    public let callId: String
    /// "approval" or "question".
    public let interactionKind: String
    public let toolName: String?
    public let interactionText: String
    public let interactionChoices: [String]?
    public var toolRisk: String? = nil
    public let createdAt: String?
}

/// AgentEvent is one event of a running turn, as the conversation's feed
/// streams it.
public struct AgentEvent: Codable, Hashable, Sendable {
    /// asked, text, message, tool_call, tool_result, confirmation, question,
    /// note, titled, navigate, locate, done or error.
    public let kind: String
    public let runId: String
    public let sequence: Int
    public let at: String?
    public let text: String?
    public let tool: String?
    public let callId: String?
    public let arguments: String?
    public let risk: String?
    public let note: String?
    public let noteKind: String?
    public let noteDetail: String?
    public let error: String?
    public let drawerId: String?

    public init(
        kind: String, runId: String, sequence: Int, at: String? = nil, text: String? = nil, tool: String? = nil,
        callId: String? = nil, arguments: String? = nil, risk: String? = nil, note: String? = nil, noteKind: String? = nil,
        noteDetail: String? = nil, error: String? = nil, drawerId: String? = nil
    ) {
        self.kind = kind
        self.runId = runId
        self.sequence = sequence
        self.at = at
        self.text = text
        self.tool = tool
        self.callId = callId
        self.arguments = arguments
        self.risk = risk
        self.note = note
        self.noteKind = noteKind
        self.noteDetail = noteDetail
        self.error = error
        self.drawerId = drawerId
    }
}

/// AgentPhone is this phone as the server knows it.
public struct AgentPhone: Codable, Hashable, Sendable {
    public let id: String
    /// The kinds it is told about; empty is every kind.
    public let notificationKinds: [String]
    public let canBeNotified: Bool

    /// Every kind, in the order a person chooses among them.
    public static let allNotificationKinds = ["message", "approval", "question", "answer"]
}

/// ServerTime reads the server's times.
public enum ServerTime {
    /// The date an RFC 3339 time stands for; nil for anything else. Go
    /// writes up to nine digits of fractions of a second, which the
    /// platform's parsers do not all take, so they are dropped first: no
    /// screen shows less than a second.
    public static func date(_ text: String?) -> Date? {
        guard var text, !text.isEmpty else { return nil }
        if let dot = text.firstIndex(of: "."), let end = text[dot...].firstIndex(where: { !$0.isNumber && $0 != "." }) {
            text.removeSubrange(dot..<end)
        }
        return try? Date(text, strategy: .iso8601)
    }
}
