import Foundation

/// TranscriptLine is one line of a conversation as the app shows it.
public struct TranscriptLine: Identifiable, Hashable, Sendable {
    public enum Kind: Hashable, Sendable {
        /// What the person said.
        case person
        /// What the agent answered.
        case answer
        /// A tool the agent called, collapsed to one line.
        case tool
        /// A turn the agent took on its own: an alert, a goal, a coding
        /// session's question. Nobody typed it, so only where it came from
        /// is shown.
        case checkIn
        /// Something that happened: the turn was stopped, the conversation
        /// was summarized.
        case note
        /// A tool call waiting for the person's approval.
        case approval
        /// A question the agent asked the person.
        case question
    }

    public let id: String
    public let kind: Kind
    public var text: String
    /// For a tool, its name; for a check-in, where it came from; for a
    /// note, its kind.
    public var detail: String
    public var isStreaming: Bool
    /// For a tool, whether it finished; for a card, whether it was settled.
    public var isDone: Bool
    public var runId: String
    public var callId: String
    public var choices: [String]
    /// How a card was settled: "approved", "declined", or the answer given.
    public var resolution: String
    public var attachments: [AgentAttachment]
    public var at: String?

    public init(
        id: String, kind: Kind, text: String, detail: String = "", isStreaming: Bool = false, isDone: Bool = false,
        runId: String = "", callId: String = "", choices: [String] = [], resolution: String = "",
        attachments: [AgentAttachment] = [], at: String? = nil
    ) {
        self.id = id
        self.kind = kind
        self.text = text
        self.detail = detail
        self.isStreaming = isStreaming
        self.isDone = isDone
        self.runId = runId
        self.callId = callId
        self.choices = choices
        self.resolution = resolution
        self.attachments = attachments
        self.at = at
    }
}

/// CheckInOrigin is where a turn the agent took on its own came from, read
/// from the marker its opening message starts with (the same markers the
/// dashboard's drawer reads).
public enum CheckInOrigin: String, CaseIterable, Sendable {
    case goal = "[goal check-in]"
    case goalNeedsYou = "[goal needs you]"
    case background = "[background command]"
    case backgroundWork = "[background work]"
    case schedule = "[schedule]"
    case speakFirst = "[speaking first]"
    case alert = "[alert]"
    case herdrQuestion = "[herdr question]"
    case herdrSession = "[herdr session]"
    case approved = "[approved]"
    case declined = "[declined]"

    /// The origin of a message's text, or nil when a person wrote it.
    public static func of(_ text: String) -> CheckInOrigin? {
        allCases.first { text.hasPrefix($0.rawValue) }
    }
}

/// answeringMarker starts a late answer to a question: the question, a blank
/// line, the answer.
let answeringMarker = "[answering]"

/// Transcript turns a conversation's stored messages, its open cards and the
/// events of the turn running now into the lines the app shows, the way the
/// dashboard's drawer does.
public struct Transcript: Sendable {
    public private(set) var lines: [TranscriptLine] = []

    public init(messages: [AgentMessage], interactions: [AgentInteraction] = []) {
        var results: [String: String] = [:]
        for message in messages where message.role == "tool" {
            if let callId = message.toolCallId { results[callId] = message.content }
        }
        for message in messages {
            switch message.role {
            case "user":
                if let origin = CheckInOrigin.of(message.content) {
                    lines.append(TranscriptLine(id: message.id, kind: .checkIn, text: message.content, detail: origin.rawValue, at: message.createdAt))
                } else if message.content.hasPrefix(answeringMarker) {
                    let rest = message.content.dropFirst(answeringMarker.count).trimmingCharacters(in: .whitespacesAndNewlines)
                    let answer = rest.range(of: "\n\n").map { String(rest[$0.upperBound...]) } ?? rest
                    lines.append(TranscriptLine(id: message.id, kind: .person, text: answer.trimmingCharacters(in: .whitespacesAndNewlines), at: message.createdAt))
                } else {
                    lines.append(TranscriptLine(id: message.id, kind: .person, text: message.content, attachments: message.attachments ?? [], at: message.createdAt))
                }
            case "assistant":
                if !message.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    lines.append(TranscriptLine(id: message.id, kind: .answer, text: message.content, at: message.createdAt))
                }
                for call in message.toolCalls ?? [] {
                    lines.append(TranscriptLine(
                        id: "\(message.id)-\(call.id)", kind: .tool, text: results[call.id] ?? "", detail: call.name,
                        isDone: true, callId: call.id, at: message.createdAt))
                }
            case "note":
                // The coding session notes are read by the question's line in
                // the drawer and say nothing on their own.
                if message.name == "herdr_answered" || message.name == "herdr_asked" { continue }
                lines.append(TranscriptLine(id: message.id, kind: .note, text: message.content, detail: message.name ?? "", at: message.createdAt))
            case "compaction":
                lines.append(TranscriptLine(id: message.id, kind: .note, text: message.content, detail: "compacted", at: message.createdAt))
            default:
                continue
            }
        }
        for interaction in interactions {
            let id = Self.cardId(runId: interaction.runId, callId: interaction.callId)
            guard !lines.contains(where: { $0.id == id }) else { continue }
            lines.append(TranscriptLine(
                id: id, kind: interaction.interactionKind == "approval" ? .approval : .question,
                text: interaction.interactionText, detail: interaction.toolName ?? "",
                runId: interaction.runId, callId: interaction.callId, choices: interaction.interactionChoices ?? [],
                at: interaction.createdAt))
        }
    }

    static func cardId(runId: String, callId: String) -> String { "\(runId)-\(callId)-card" }

    /// Applies an event of a running turn.
    public mutating func apply(_ event: AgentEvent) {
        switch event.kind {
        case "text":
            if let last = lines.indices.last, lines[last].kind == .answer, lines[last].isStreaming {
                lines[last].text += event.text ?? ""
            } else {
                lines.append(TranscriptLine(id: "\(event.runId)-\(event.sequence)", kind: .answer, text: event.text ?? "", isStreaming: true, runId: event.runId, at: event.at))
            }
        case "message":
            if let last = lines.indices.last, lines[last].kind == .answer, lines[last].isStreaming {
                lines[last].text = event.text ?? lines[last].text
                lines[last].isStreaming = false
            } else if let text = event.text, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                lines.append(TranscriptLine(id: "\(event.runId)-\(event.sequence)", kind: .answer, text: text, runId: event.runId, at: event.at))
            }
        case "tool_call":
            let id = "\(event.runId)-\(event.callId ?? "")"
            guard !lines.contains(where: { $0.id == id }) else { return }
            lines.append(TranscriptLine(id: id, kind: .tool, text: "", detail: event.tool ?? "", runId: event.runId, callId: event.callId ?? "", at: event.at))
        case "tool_result":
            if let index = lines.firstIndex(where: { $0.id == "\(event.runId)-\(event.callId ?? "")" }) {
                lines[index].isDone = true
                lines[index].text = event.text ?? ""
            }
            // A card's call has an answer: settled, wherever it was settled.
            if let index = lines.firstIndex(where: { $0.id == Self.cardId(runId: event.runId, callId: event.callId ?? "") }), !lines[index].isDone {
                lines[index].isDone = true
                lines[index].resolution = Self.resolution(of: event.text ?? "", kind: lines[index].kind)
            }
        case "confirmation":
            let id = Self.cardId(runId: event.runId, callId: event.callId ?? "")
            guard !lines.contains(where: { $0.id == id }) else { return }
            lines.append(TranscriptLine(id: id, kind: .approval, text: event.note ?? "", detail: event.tool ?? "", runId: event.runId, callId: event.callId ?? "", at: event.at))
        case "question":
            let id = Self.cardId(runId: event.runId, callId: event.callId ?? "")
            guard !lines.contains(where: { $0.id == id }) else { return }
            let choices = (event.text ?? "").split(separator: "\n").map(String.init).filter { !$0.isEmpty }
            lines.append(TranscriptLine(id: id, kind: .question, text: event.note ?? "", runId: event.runId, callId: event.callId ?? "", choices: choices, at: event.at))
        case "done":
            for index in lines.indices where lines[index].runId == event.runId && lines[index].isStreaming {
                lines[index].isStreaming = false
            }
        default:
            return
        }
    }

    /// Marks a card settled from this app, before the feed says so.
    public mutating func settle(cardId: String, resolution: String) {
        if let index = lines.firstIndex(where: { $0.id == cardId }) {
            lines[index].isDone = true
            lines[index].resolution = resolution
        }
    }

    static func resolution(of result: String, kind: TranscriptLine.Kind) -> String {
        if kind == .approval {
            let lowered = result.lowercased()
            return lowered.contains("declined") || lowered.contains("refused") || lowered.contains("denied") ? "declined" : "approved"
        }
        return result
    }
}
