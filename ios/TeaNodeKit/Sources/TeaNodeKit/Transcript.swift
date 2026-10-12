import Foundation

/// TranscriptLine is one line of a conversation as the app shows it.
public struct TranscriptLine: Identifiable, Hashable, Sendable {
    public enum Kind: Hashable, Sendable {
        /// What the person said.
        case person
        /// What the agent answered.
        case answer
        /// A tool the agent called.
        case tool
        /// A turn the agent took on its own: an alert, a goal, a coding
        /// session's question. Nobody typed it.
        case checkIn
        /// Something that happened: the turn was stopped, the conversation
        /// was folded into a note, a goal changed.
        case note
        /// A tool call waiting for the person's approval.
        case approval
        /// A question the agent asked the person.
        case question
        /// A question one of the person's coding sessions asked, with its
        /// options.
        case codingQuestion
    }

    public let id: String
    public let kind: Kind
    public var text: String
    /// For a tool, its name; for a check-in, its marker; for a note, its
    /// kind.
    public var detail: String
    public var isStreaming: Bool
    /// For a tool, whether it finished; for a card, whether it was settled.
    public var isDone: Bool
    public var runId: String
    public var callId: String
    public var choices: [String]
    /// How a card was settled ("approved", "declined", or the answer
    /// given); for a tool, the short note it ended with.
    public var resolution: String
    public var attachments: [AgentAttachment]
    public var at: String?
    /// For a coding session's question, the question whole.
    public var codingQuestion: CodingQuestion?
    /// The stored message the line is, which a reply points at; nil for a
    /// line still arriving.
    public var messageId: String?
    /// On the last answer of a turn, what the whole turn cost.
    public var usage: AgentUsage?
    /// What a person's message points at, besides a reply.
    public var references: [AgentReference] = []
    /// What a person's message replies to: an earlier message, or the
    /// question a late answer answers.
    public var quotedText: String = ""
    /// "user", "assistant" or "question": whose words quotedText are.
    public var quotedRole: String = ""
    /// For an approval, how risky the call is; "destructive" is drawn red.
    public var risk: String = ""
    /// For an answer, the replies it suggests, shown as buttons.
    public var suggestions: [String] = []
    /// For an answer still arriving, everything that has come, which is more
    /// than `text` shows while the hidden suggestions line is being written:
    /// its characters come one by one, and none of them are for the person.
    public var streamedText: String = ""
    /// For a tool, what it was called with.
    public var arguments: String = ""
    /// For a note, whether it is waiting on something still to happen
    /// (a turn queued behind another, the conversation being folded).
    public var isPending = false
    /// The person's and the agent's reactions to it, one each at most.
    public var reactions: [AgentMessageReaction] = []

    /// The emoji one side put on it, if any.
    public func reaction(of reactorKind: String) -> String? {
        reactions.first { $0.reactorKind == reactorKind }?.emoji
    }

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

/// Suggestions are the replies an answer offers, written as a hidden last
/// line, `<!--suggestions:["Yes","Not yet"]-->`, which is never shown.
public enum Suggestions {
    static let pattern = try! NSRegularExpression(pattern: #"<!--\s*suggestions:\s*(\[.*?\])\s*-->\s*$"#, options: [.dotMatchesLineSeparators])

    /// An answer's text without its suggestions line, and the suggestions;
    /// while it is still being written, a half-written line is left out too.
    public static func split(_ text: String) -> (text: String, suggestions: [String]) {
        let range = NSRange(text.startIndex..., in: text)
        if let match = pattern.firstMatch(in: text, range: range),
            let whole = Range(match.range, in: text), let list = Range(match.range(at: 1), in: text)
        {
            let parsed = (try? JSONDecoder().decode([String].self, from: Data(text[list].utf8))) ?? []
            let suggestions = parsed.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
            let display = String(text[..<whole.lowerBound]).trimmingCharacters(in: .whitespacesAndNewlines)
            return (display, (2...6).contains(suggestions.count) ? suggestions : [])
        }
        if let open = text.range(of: "<!--", options: .backwards), text[open.lowerBound...].range(of: "-->") == nil {
            return (String(text[..<open.lowerBound]).trimmingCharacters(in: .whitespacesAndNewlines), [])
        }
        return (text, [])
    }
}

/// CodingQuestion is a question one of the person's coding sessions asked,
/// as the server keeps it beside the agent's telling of it (the
/// "herdr_asked" note, internal/agent/herdr.go).
public struct CodingQuestion: Hashable, Sendable {
    public let computer: String
    public let paneId: String
    public let questionFingerprint: String
    /// "claude" or "codex".
    public let codingAgentKind: String
    /// The session's name, or where it works.
    public let sessionName: String
    public let questionText: String
    public let isMultipleChoice: Bool
    public let options: [Option]

    public struct Option: Hashable, Sendable {
        public let optionNumber: Int
        public let optionLabel: String
        public let optionDescription: String
        /// "choice", "freeText" or "chat".
        public let herdrOptionKind: String
    }

    public var codingAgentName: String { codingAgentKind == "codex" ? "Codex" : "Claude Code" }

    /// The question's lines worth reading, without the countdown a coding
    /// agent draws under a request it will deny by itself, which is not part
    /// of what it asks.
    public var shownText: String {
        questionText.split(separator: "\n", omittingEmptySubsequences: false)
            .filter { line in !Self.isCountdown(String(line)) }
            .joined(separator: "\n")
    }

    /// What the question asks, in one line, for a card that keeps the rest
    /// until the person opens it. A tool's approval is named by the tool and
    /// what it is for ("Bash command · Run the tests"), since its question
    /// line is always "Do you want to proceed?"; any other question by the
    /// line that asks, as the dashboard heads its card.
    public var headline: String {
        let lines = shownText.split(separator: "\n").map { line in line.trimmingCharacters(in: .whitespaces) }.filter { line in !line.isEmpty }
        guard let first = lines.first else { return "" }
        let chosen: String
        if lines.count > 1, !first.contains("?"), first.split(separator: " ").count <= 3, !lines[1].contains("?") {
            chosen = "\(first) · \(lines[1])"
        } else {
            chosen = lines.first { line in line.contains("?") } ?? first
        }
        return chosen.count > 160 ? String(chosen.prefix(157)) + "…" : chosen
    }

    static func isCountdown(_ line: String) -> Bool {
        line.contains("will automatically") && line.range(of: #"\d{1,2}:\d{2}"#, options: .regularExpression) != nil
    }

    /// The options a person picks, without the ones that ask to type.
    public var choices: [Option] { options.filter { $0.herdrOptionKind == "choice" } }

    /// Reads a "herdr_asked" note; nil for one that carries no question.
    static func of(note: String) -> CodingQuestion? {
        struct Note: Decodable {
            struct Session: Decodable {
                struct Question: Decodable {
                    struct Choice: Decodable {
                        let optionNumber: Int
                        let optionLabel: String
                        let optionDescription: String?
                        let herdrOptionKind: String?
                    }
                    let questionFingerprint: String
                    let questionText: String
                    let isMultipleChoice: Bool?
                    let options: [Choice]?
                }
                let paneName: String?
                let codingAgentKind: String?
                let workingDirectory: String?
                let question: Question?
            }
            let computer: String
            let paneId: String
            let herdrSession: Session?
        }
        guard let note = try? JSONDecoder().decode(Note.self, from: Data(note.utf8)),
            let session = note.herdrSession, let question = session.question
        else { return nil }
        let place = session.paneName.flatMap { $0.isEmpty ? nil : $0 }
            ?? session.workingDirectory.map { ($0 as NSString).lastPathComponent } ?? ""
        return CodingQuestion(
            computer: note.computer, paneId: note.paneId, questionFingerprint: question.questionFingerprint,
            codingAgentKind: session.codingAgentKind ?? "claude", sessionName: place, questionText: question.questionText,
            isMultipleChoice: question.isMultipleChoice ?? false,
            options: (question.options ?? []).map {
                Option(optionNumber: $0.optionNumber, optionLabel: $0.optionLabel, optionDescription: $0.optionDescription ?? "", herdrOptionKind: $0.herdrOptionKind ?? "choice")
            })
    }

    /// The card's id, from the pane and the question.
    /// The card's line: by computer as well as pane, since two computers
    /// can each have a pane of the same id, as the dashboard tells them.
    var cardId: String { Self.cardId(computer: computer, paneId: paneId, questionFingerprint: questionFingerprint) }

    static func cardId(computer: String, paneId: String, questionFingerprint: String) -> String {
        "coding-\(computer)-\(paneId)-\(questionFingerprint)"
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
        // What a turn cost is added up over its rounds and shown on its
        // last answer.
        var turnUsage: AgentUsage?
        var turnAnswer: Int?
        func closeTurn() {
            if let turnAnswer, let turnUsage { lines[turnAnswer].usage = turnUsage }
            turnUsage = nil
            turnAnswer = nil
        }
        for message in messages {
            switch message.role {
            case "user":
                closeTurn()
                if let origin = CheckInOrigin.of(message.content) {
                    var line = TranscriptLine(id: message.id, kind: .checkIn, text: message.content, detail: origin.rawValue, at: message.createdAt)
                    line.messageId = message.id
                    lines.append(line)
                    continue
                }
                var line = TranscriptLine(id: message.id, kind: .person, text: message.content, attachments: message.attachments ?? [], at: message.createdAt)
                line.messageId = message.id
                line.reactions = message.reactions ?? []
                if message.content.hasPrefix(answeringMarker) {
                    let rest = message.content.dropFirst(answeringMarker.count).trimmingCharacters(in: .whitespacesAndNewlines)
                    if let gap = rest.range(of: "\n\n") {
                        line.quotedText = String(rest[..<gap.lowerBound])
                        line.quotedRole = "question"
                        line.text = String(rest[gap.upperBound...]).trimmingCharacters(in: .whitespacesAndNewlines)
                    } else {
                        line.text = rest
                    }
                }
                for reference in message.references ?? [] {
                    if reference.isReply {
                        line.quotedText = reference.quotedText ?? ""
                        line.quotedRole = reference.quotedRole ?? ""
                    } else {
                        line.references.append(reference)
                    }
                }
                lines.append(line)
            case "assistant":
                if let usage = message.usage {
                    turnUsage = (turnUsage ?? AgentUsage()) + usage
                }
                if !message.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    let split = Suggestions.split(message.content)
                    var line = TranscriptLine(id: message.id, kind: .answer, text: split.text, at: message.createdAt)
                    line.suggestions = split.suggestions
                    line.messageId = message.id
                    line.reactions = message.reactions ?? []
                    lines.append(line)
                    turnAnswer = lines.count - 1
                }
                for call in message.toolCalls ?? [] {
                    var line = TranscriptLine(
                        id: Self.toolId(callId: call.id), kind: .tool, text: results[call.id] ?? "", detail: call.name,
                        isDone: true, callId: call.id, at: message.createdAt)
                    line.arguments = call.arguments ?? ""
                    lines.append(line)
                }
            case "note":
                // A coding session's question is drawn as its card, in place
                // of the agent's telling of it and the turn that told it.
                if message.name == "herdr_asked" {
                    if let question = CodingQuestion.of(note: message.content) {
                        if lines.last?.kind == .answer { lines.removeLast() }
                        if lines.last?.kind == .checkIn, lines.last?.detail == CheckInOrigin.herdrQuestion.rawValue { lines.removeLast() }
                        lines.removeAll { $0.id == question.cardId }
                        var card = TranscriptLine(
                            id: question.cardId, kind: .codingQuestion, text: question.questionText, detail: question.codingAgentName,
                            callId: question.questionFingerprint, choices: question.choices.map(\.optionLabel), at: message.createdAt)
                        card.codingQuestion = question
                        lines.append(card)
                        turnAnswer = nil
                    }
                    continue
                }
                if message.name == "herdr_answered" {
                    answerCodingQuestion(note: message.content)
                    continue
                }
                lines.append(TranscriptLine(id: message.id, kind: .note, text: message.content, detail: message.name ?? "", at: message.createdAt))
            case "compaction":
                lines.append(TranscriptLine(id: message.id, kind: .note, text: message.content, detail: "compacted", at: message.createdAt))
            default:
                continue
            }
        }
        closeTurn()
        for interaction in interactions {
            let id = Self.cardId(runId: interaction.runId, callId: interaction.callId)
            guard !lines.contains(where: { $0.id == id }) else { continue }
            var line = TranscriptLine(
                id: id, kind: interaction.interactionKind == "approval" ? .approval : .question,
                text: interaction.interactionText, detail: interaction.toolName ?? "",
                runId: interaction.runId, callId: interaction.callId, choices: interaction.interactionChoices ?? [],
                at: interaction.createdAt)
            line.risk = interaction.toolRisk ?? ""
            lines.append(line)
        }
    }

    static func cardId(runId: String, callId: String) -> String { "\(runId)-\(callId)-card" }

    /// A tool line's id, the same whether it was read back or arrived live,
    /// so a turn replayed after the feed reconnects does not draw it twice.
    static func toolId(callId: String) -> String { "tool-\(callId)" }

    /// sentPrefix starts the id of a line this app sent, until the feed
    /// says which turn it began.
    static let sentPrefix = "sent-"

    /// Adds older history without rebuilding lines changed by the live feed
    /// or by an optimistic action while the history request was in flight.
    /// Supply the complete stored history, with how many of its messages
    /// are the older page, so tool results and card answers can be matched
    /// across page boundaries. Only the older page's lines are added: a
    /// line of the newer page that is not on screen was taken away on
    /// purpose (a turn the feed replays, a card answered elsewhere), and
    /// open cards come from the conversation's own reading, not from here.
    public mutating func prepend(messages: [AgentMessage], earlierCount: Int) {
        let whole = Transcript(messages: messages)
        let newer = Transcript(messages: Array(messages.dropFirst(earlierCount)))
        let skippedLineIds = Set(lines.map(\.id)).union(newer.lines.map(\.id))
        lines.insert(contentsOf: whole.lines.filter { line in !skippedLineIds.contains(line.id) }, at: 0)
    }

    /// How far back a turn's opening is looked for, so one the feed replays
    /// is matched with the line already there rather than drawn again.
    static let replayLookBack = 8

    /// Shows what the person just sent from this app, before the turn it
    /// begins is under way.
    @discardableResult
    public mutating func addSent(_ text: String, attachments: [AgentAttachment] = [], replyingTo reply: AgentReference? = nil) -> String {
        var line = TranscriptLine(id: Self.sentPrefix + UUID().uuidString, kind: .person, text: text, attachments: attachments)
        if let reply {
            line.quotedText = reply.quotedText ?? ""
            line.quotedRole = reply.quotedRole ?? ""
        }
        lines.append(line)
        return line.id
    }

    /// Takes back only the message belonging to a refused request, unless
    /// the feed already confirmed that the server accepted it.
    public mutating func withdrawSent(lineId: String) {
        if let index = lines.firstIndex(where: { $0.id == lineId && $0.id.hasPrefix(Self.sentPrefix) && $0.runId.isEmpty }) {
            lines.remove(at: index)
        }
    }

    /// The question waiting for the person's answer, if one is: the
    /// agent's, or a coding session's.
    public var openQuestion: TranscriptLine? {
        lines.last { ($0.kind == .question || $0.kind == .codingQuestion) && !$0.isDone }
    }

    /// The suggested replies to offer now: those of the last answer, when
    /// nothing but notes came after it and it is finished.
    public var offeredSuggestions: [String] {
        guard let last = lines.last(where: { $0.kind != .note }), last.kind == .answer, !last.isStreaming else { return [] }
        return last.suggestions
    }

    /// Applies an event of a running turn.
    /// Puts one side's reaction on a message, or takes it away when the
    /// emoji is empty; as the server keeps it, one each at most.
    public mutating func react(messageId: String, emoji: String, reactorKind: String) {
        guard let index = lines.lastIndex(where: { $0.messageId == messageId }) else { return }
        lines[index].reactions.removeAll { $0.reactorKind == reactorKind }
        if !emoji.isEmpty {
            lines[index].reactions.append(AgentMessageReaction(emoji: emoji, reactorKind: reactorKind))
        }
    }

    public mutating func apply(_ event: AgentEvent) {
        // A turn waiting behind another has begun once anything else of it
        // arrives.
        if event.kind != "note" || event.noteKind != "queued" {
            lines.removeAll { $0.id == "\(event.runId)-queued" }
        }
        switch event.kind {
        case "reacted":
            if let messageId = event.messageId, let reactorKind = event.reactorKind {
                react(messageId: messageId, emoji: event.emoji ?? "", reactorKind: reactorKind)
            }
            return
        case "asked":
            // A turn began: something the person said, here or anywhere
            // else, or a turn the agent took on its own. One already shown,
            // sent from this app or read back before the feed replayed the
            // turn, is not drawn again.
            guard let text = event.text, !text.isEmpty else { return }
            // Requests can finish in a different order from the taps that
            // started them. Confirm the matching pending line without
            // removing another message or events already on screen.
            if let index = lines.firstIndex(where: {
                $0.id.hasPrefix(Self.sentPrefix) && $0.runId.isEmpty && $0.text == text
            }) {
                lines[index].runId = event.runId
                return
            }
            // The person's latest words, when they are these, are this turn
            // read back with the transcript: what came after them is the
            // turn as far as it was stored, which the replay draws again in
            // order, so it goes, as the dashboard drops it. However many
            // tool calls the turn made, nothing of it is drawn twice.
            if let index = lines.lastIndex(where: { $0.kind == .person }), lines[index].text == text,
                lines[index].runId.isEmpty || lines[index].runId == event.runId
            {
                lines.removeSubrange((index + 1)...)
                lines[index].runId = event.runId
                return
            }
            let recent = lines.indices.suffix(Self.replayLookBack)
            if let index = recent.last(where: { lines[$0].text == text && (lines[$0].runId.isEmpty || lines[$0].runId == event.runId) && lines[$0].kind == .checkIn }) {
                lines[index].runId = event.runId
            } else if let index = recent.last(where: { lines[$0].kind == .checkIn && lines[$0].text == text }) {
                lines[index].runId = event.runId
            } else if let origin = CheckInOrigin.of(text) {
                lines.append(TranscriptLine(id: "\(event.runId)-asked", kind: .checkIn, text: text, detail: origin.rawValue, runId: event.runId, at: event.at))
            } else {
                lines.append(TranscriptLine(id: "\(event.runId)-asked", kind: .person, text: text, runId: event.runId, at: event.at))
            }
        case "text":
            if let last = lines.lastIndex(where: { $0.runId == event.runId && $0.kind == .answer && $0.isStreaming }) {
                lines[last].streamedText += event.text ?? ""
                lines[last].text = Suggestions.split(lines[last].streamedText).text
            } else {
                var line = TranscriptLine(
                    id: "\(event.runId)-\(event.sequence)", kind: .answer, text: Suggestions.split(event.text ?? "").text, isStreaming: true,
                    runId: event.runId, at: event.at)
                line.streamedText = event.text ?? ""
                lines.append(line)
            }
        case "message":
            let split = Suggestions.split(event.text ?? "")
            if let last = lines.lastIndex(where: { $0.runId == event.runId && $0.kind == .answer && $0.isStreaming }) {
                lines[last].text = event.text == nil ? lines[last].text : split.text
                lines[last].suggestions = split.suggestions
                lines[last].isStreaming = false
            } else if !split.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
                !lines.suffix(Self.replayLookBack).contains(where: { $0.kind == .answer && $0.text == split.text })
            {
                var line = TranscriptLine(id: "\(event.runId)-\(event.sequence)", kind: .answer, text: split.text, runId: event.runId, at: event.at)
                line.suggestions = split.suggestions
                lines.append(line)
            }
        case "tool_call":
            let id = Self.toolId(callId: event.callId ?? "")
            guard !lines.contains(where: { $0.id == id }) else { return }
            var line = TranscriptLine(id: id, kind: .tool, text: "", detail: event.tool ?? "", runId: event.runId, callId: event.callId ?? "", at: event.at)
            line.arguments = event.arguments ?? ""
            lines.append(line)
        case "tool_result":
            if let index = lines.firstIndex(where: { $0.id == Self.toolId(callId: event.callId ?? "") }) {
                lines[index].isDone = true
                lines[index].text = event.text ?? ""
                lines[index].resolution = event.note ?? ""
            }
            // A card's call has an answer: settled, wherever it was settled.
            if let index = lines.firstIndex(where: { $0.id == Self.cardId(runId: event.runId, callId: event.callId ?? "") }), !lines[index].isDone {
                lines[index].isDone = true
                lines[index].resolution = Self.resolution(of: event.text ?? "", kind: lines[index].kind)
            }
        case "confirmation":
            let id = Self.cardId(runId: event.runId, callId: event.callId ?? "")
            guard !lines.contains(where: { $0.id == id }) else { return }
            var line = TranscriptLine(id: id, kind: .approval, text: event.note ?? "", detail: event.tool ?? "", runId: event.runId, callId: event.callId ?? "", at: event.at)
            line.risk = event.risk ?? ""
            lines.append(line)
        case "question":
            let id = Self.cardId(runId: event.runId, callId: event.callId ?? "")
            guard !lines.contains(where: { $0.id == id }) else { return }
            let choices = (event.text ?? "").split(separator: "\n").map(String.init).filter { !$0.isEmpty }
            lines.append(TranscriptLine(id: id, kind: .question, text: event.note ?? "", runId: event.runId, callId: event.callId ?? "", choices: choices, at: event.at))
        case "note":
            let noteKind = event.noteKind ?? ""
            switch noteKind {
            case "queued":
                guard !lines.contains(where: { $0.id == "\(event.runId)-queued" }) else { return }
                var line = TranscriptLine(id: "\(event.runId)-queued", kind: .note, text: "", detail: "queued", runId: event.runId, at: event.at)
                line.isPending = true
                lines.append(line)
            case "compacting":
                guard !lines.contains(where: { $0.id == "\(event.runId)-compacting" }) else { return }
                var line = TranscriptLine(id: "\(event.runId)-compacting", kind: .note, text: "", detail: "compacting", runId: event.runId, at: event.at)
                line.isPending = true
                lines.append(line)
            case "herdr_asked":
                return
            case "herdr_answered":
                // A coding session's question went, here or at the computer:
                // its card folds with what it was answered with.
                answerCodingQuestion(note: event.noteDetail ?? "")
            default:
                let id = "\(event.runId)-note-\(event.sequence)"
                guard !lines.contains(where: { $0.id == id }) else { return }
                let line = TranscriptLine(id: id, kind: .note, text: event.noteDetail ?? event.note ?? "", detail: noteKind, runId: event.runId, at: event.at)
                if noteKind == "compacted", let index = lines.firstIndex(where: { $0.id == "\(event.runId)-compacting" }) {
                    lines[index] = line
                } else {
                    lines.append(line)
                }
            }
        case "done", "error":
            for index in lines.indices where lines[index].runId == event.runId && lines[index].isStreaming {
                lines[index].isStreaming = false
            }
            lines.removeAll { $0.runId == event.runId && $0.isPending }
        default:
            return
        }
    }

    /// Folds a coding session's question card from a "herdr_answered" note,
    /// with what it was answered with when that is known. An answer this
    /// app gave is kept when the note knows none.
    mutating func answerCodingQuestion(note: String) {
        struct Answered: Decodable {
            let computer: String
            let paneId: String
            let questionFingerprint: String
            let answerText: String?
        }
        guard let answered = try? JSONDecoder().decode(Answered.self, from: Data(note.utf8)),
            let index = lines.lastIndex(where: {
                $0.id == CodingQuestion.cardId(computer: answered.computer, paneId: answered.paneId, questionFingerprint: answered.questionFingerprint)
            })
        else { return }
        lines[index].isDone = true
        if let answerText = answered.answerText, !answerText.isEmpty {
            lines[index].resolution = answerText
        }
    }

    /// Marks a card settled from this app, before the feed says so.
    public mutating func settle(cardId: String, resolution: String) {
        if let index = lines.firstIndex(where: { $0.id == cardId }) {
            lines[index].isDone = true
            lines[index].resolution = resolution
        }
    }

    /// Takes a card away that the server says is no longer waiting.
    public mutating func remove(cardId: String) {
        lines.removeAll { $0.id == cardId }
    }

    static func resolution(of result: String, kind: TranscriptLine.Kind) -> String {
        if kind == .approval {
            let lowered = result.lowercased()
            return lowered.contains("declined") || lowered.contains("refused") || lowered.contains("denied") ? "declined" : "approved"
        }
        // A question's tool result is {"answer": ...}.
        if let data = result.data(using: .utf8), let answered = try? JSONDecoder().decode([String: JSONValue].self, from: data),
            let answer = answered["answer"]?.stringValue
        {
            return answer
        }
        return result
    }
}
