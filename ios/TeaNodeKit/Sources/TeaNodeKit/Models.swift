import Foundation

// What the server's GraphQL API answers with, as the app reads it. The field
// names are the server's (internal/api/v1api/apigraph), so they decode
// without mapping; times stay the server's RFC 3339 text and are read with
// `ServerTime.date(_:)` where a screen needs a date.

/// AgentConversation is one conversation with the agent: the main one, or a
/// named one.
public struct AgentConversation: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// "main", "named" or "goal".
    public let kind: String
    public let title: String?
    public let summary: String?
    public let lastAt: String?

    public init(id: String, kind: String, title: String?, summary: String?, lastAt: String?) {
        self.id = id
        self.kind = kind
        self.title = title
        self.summary = summary
        self.lastAt = lastAt
    }

    public var isMain: Bool { kind == "main" }
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
