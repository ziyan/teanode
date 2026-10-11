import CoreLocation
import Foundation
import Observation
import TeaNodeKit
import UIKit
import UniformTypeIdentifiers

/// ConversationModel is one conversation as the screen shows it: its
/// transcript, kept up to date from the server's feed, and what the person
/// sends into it.
@MainActor
@Observable
final class ConversationModel {
    /// The conversation's id; nil for the main conversation until it is
    /// loaded.
    private(set) var conversationId: String?
    private(set) var conversation: AgentConversation?
    private(set) var title: String
    private(set) var transcript = Transcript(messages: [])
    private(set) var isLoaded = false
    private(set) var loadProblemText: String?

    /// The messages read so far, oldest first, and how many there are in
    /// all: older ones are read a page at a time.
    private var messages: [AgentMessage] = []
    private var interactions: [AgentInteraction] = []
    private(set) var total = 0
    private(set) var isLoadingEarlier = false
    var earlierCount: Int { max(0, total - messages.count) }

    /// The agent's list for this conversation, and whose agent this is when
    /// an operator looks at somebody else's.
    private(set) var todos: [AgentTodo] = []
    private(set) var actingAs: String?

    /// Ideas to start an empty conversation with.
    private(set) var ideas: [AgentIdea] = []

    var draft = "" {
        didSet { saveDraft() }
    }
    var pendingFiles: [PendingFile] = []
    /// The message being replied to, shown above the composer.
    var replyingTo: AgentReference?
    private(set) var isSending = false
    var sendProblemText: String?

    /// Whether the send problem is on screen; dismissing it clears it.
    var isShowingSendProblem: Bool {
        get { sendProblemText != nil }
        set { if !newValue { sendProblemText = nil } }
    }

    /// The turns running or waiting, oldest first: the stop button stops
    /// the first.
    private(set) var runningRunIds: [String] = []
    var runningRunId: String? { runningRunIds.first }

    /// Changes when something is sent or a card is settled, for the haptic.
    private(set) var sentCount = 0

    /// The call with the agent, while there is one, and whether the server
    /// offers calls and at what rate it takes speech.
    var call: VoiceCall?
    private(set) var voiceSampleRate: Int?

    private let app: AppModel
    private let connection: Connection
    private var answeredLocationCalls: Set<String> = []

    /// How recent a question must be for typing to answer it rather than
    /// start a turn: one from an hour ago is not what the person is
    /// answering.
    static let cardFresh: TimeInterval = 60 * 60

    init(conversation: AgentConversation?, app: AppModel, connection: Connection) {
        self.conversationId = conversation?.id
        self.conversation = conversation
        self.title = conversation.map { ConversationModel.title(of: $0, agentTitle: app.agentTitle) } ?? app.agentTitle
        self.app = app
        self.connection = connection
        self.draft = UserDefaults.standard.string(forKey: draftKey) ?? ""
    }

    static func title(of conversation: AgentConversation, agentTitle: String) -> String {
        if conversation.isMain {
            return agentTitle
        }
        if let title = conversation.title, !title.isEmpty {
            return title
        }
        return String(localized: "New conversation")
    }

    /// The draft is kept per conversation, as the dashboard keeps it.
    private var draftKey: String { "draft.\(conversationId ?? "main")" }

    private func saveDraft() {
        if draft.isEmpty {
            UserDefaults.standard.removeObject(forKey: draftKey)
        } else {
            UserDefaults.standard.set(draft, forKey: draftKey)
        }
    }

    /// Loads the conversation and follows its feed while the screen shows
    /// it, loading again and following anew whenever the feed drops. The
    /// screen restarts this when the app comes back to the front, so a feed
    /// iOS closed in the background is opened again at once.
    func follow() async {
        var delay = Duration.seconds(1)
        while !Task.isCancelled {
            do {
                try await load()
                guard let conversationId else { return }
                for try await event in ConversationFeed.events(connection: connection, conversationId: conversationId) {
                    apply(event)
                    delay = .seconds(1)
                }
            } catch is CancellationError {
                return
            } catch {
                let text = app.describe(error)
                if !isLoaded {
                    loadProblemText = text
                }
            }
            try? await Task.sleep(for: delay)
            delay = min(delay * 2, .seconds(10))
        }
    }

    /// Reads the conversation's newest messages and open cards. A turn
    /// still running is replayed by the feed that follows, which says it
    /// runs again; one that ended while the feed was down is not.
    func load() async throws {
        let page = try await connection.conversation(conversationId, first: max(100, messages.count))
        if let conversation = page.conversation {
            conversationId = conversation.id
            self.conversation = conversation
            title = Self.title(of: conversation, agentTitle: app.agentTitle)
        }
        messages = page.messages
        interactions = page.interactions
        total = page.total
        todos = page.todos
        actingAs = page.actingAs
        runningRunIds = []
        transcript = Transcript(messages: messages, interactions: interactions)
        isLoaded = true
        loadProblemText = nil
        if messages.isEmpty {
            await loadIdeas()
        }
        if voiceSampleRate == nil, let voice = try? await connection.voiceAvailability(), voice.isVoiceAvailable {
            voiceSampleRate = voice.sampleRate ?? 24000
        }
    }

    /// Calls the agent in this conversation.
    func startCall() {
        guard call == nil, let voiceSampleRate else { return }
        let call = VoiceCall(connection: connection, model: self, sampleRate: voiceSampleRate, agentName: app.agentTitle)
        self.call = call
        call.start()
    }

    /// Sends what was said on a call as a turn, and returns the turn, whose
    /// answers the call reads out.
    func sendSpoken(_ text: String, interruptedAnswer: (heardText: String, unheardText: String)?) async -> String? {
        transcript.addSent(text)
        do {
            let asked = try await connection.ask(
                text, in: conversationId, surface: Surface.voice, drawerId: app.drawerId, interruptedAnswer: interruptedAnswer)
            if !runningRunIds.contains(asked.runId) {
                runningRunIds.append(asked.runId)
            }
            if conversationId == nil {
                conversationId = asked.conversationId
            }
            return asked.runId
        } catch {
            transcript.withdrawSent()
            sendProblemText = app.describe(error)
            return nil
        }
    }

    /// Reads the page of messages before those shown.
    func loadEarlier() async {
        guard earlierCount > 0, !isLoadingEarlier else { return }
        isLoadingEarlier = true
        defer { isLoadingEarlier = false }
        do {
            let page = try await connection.conversation(conversationId, first: 100, offset: messages.count)
            let known = Set(messages.map(\.id))
            messages = page.messages.filter { !known.contains($0.id) } + messages
            total = page.total
            transcript = Transcript(messages: messages, interactions: interactions)
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    private func loadIdeas() async {
        ideas = (try? await connection.ideas(language: Locale.current.language.languageCode?.identifier ?? "en"))?.prefix(3).map { $0 } ?? []
        if !ideas.isEmpty {
            try? await connection.markIdeasShown(ideas.map(\.id))
        }
    }

    private func apply(_ event: AgentEvent) {
        transcript.apply(event)
        call?.follow(event)
        switch event.kind {
        case "asked":
            if !runningRunIds.contains(event.runId) {
                runningRunIds.append(event.runId)
            }
        case "done", "error":
            runningRunIds.removeAll { $0 == event.runId }
            if event.kind == "error", let text = event.error, !text.isEmpty {
                sendProblemText = text
            }
            // The stored turn replaces the one drawn as it arrived: its
            // usage, its ids for replying, a coding session's question card.
            Task { try? await load() }
        case "tool_result" where event.tool == "todo":
            Task {
                if let page = try? await connection.conversation(conversationId, first: 1) {
                    todos = page.todos
                }
            }
        case "titled":
            if let text = event.text, !text.isEmpty { title = text }
        case "locate":
            answerLocation(event)
        default:
            break
        }
    }

    var canSend: Bool {
        !isSending && (!draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !pendingFiles.isEmpty)
    }

    /// The question that typing answers: the newest unanswered one, while
    /// its turn runs or for an hour after it was asked.
    var freshQuestion: TranscriptLine? {
        guard let question = transcript.openQuestion else { return nil }
        if runningRunIds.contains(question.runId) || question.kind == .codingQuestion { return question }
        guard let at = ServerTime.date(question.at) else { return question }
        return Date().timeIntervalSince(at) < Self.cardFresh ? question : nil
    }

    /// Sends what is typed: an answer to the question waiting, when one is
    /// fresh, or a new message.
    func send() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSend else { return }
        if let question = freshQuestion, pendingFiles.isEmpty, replyingTo == nil {
            draft = ""
            if question.kind == .codingQuestion {
                await answerCodingQuestion(question, options: [], freeText: text)
            } else {
                await answer(question, with: text)
            }
            return
        }
        if pendingFiles.reduce(0, { $0 + $1.data.count }) > PendingFile.bytesMost {
            sendProblemText = String(localized: "The files come to more than 25 MB, more than the server takes in one message.")
            return
        }
        isSending = true
        defer { isSending = false }
        let sending = pendingFiles
        let reply = replyingTo
        draft = ""
        pendingFiles = []
        replyingTo = nil
        do {
            var attachments: [AgentAttachment] = []
            for file in sending {
                let id = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
                attachments.append(AgentAttachment(id: id, name: file.name, contentType: file.contentType, size: file.data.count))
            }
            let message = text.isEmpty ? "(files)" : text
            transcript.addSent(message, attachments: attachments, replyingTo: reply)
            let asked = try await connection.ask(
                message, in: conversationId, attachmentIds: attachments.map(\.id), references: reply.map { [$0] } ?? [],
                drawerId: app.drawerId)
            if !runningRunIds.contains(asked.runId) {
                runningRunIds.append(asked.runId)
            }
            sentCount += 1
            if conversationId == nil {
                conversationId = asked.conversationId
            }
            ideas = []
        } catch {
            transcript.withdrawSent()
            draft = text
            pendingFiles = sending
            replyingTo = reply
            sendProblemText = app.describe(error)
        }
    }

    /// Sends a suggested reply as it is, leaving the draft alone.
    func sendSuggestion(_ suggestion: String) async {
        transcript.addSent(suggestion)
        do {
            let asked = try await connection.ask(suggestion, in: conversationId, drawerId: app.drawerId)
            runningRunIds.append(asked.runId)
            sentCount += 1
        } catch {
            transcript.withdrawSent()
            sendProblemText = app.describe(error)
        }
    }

    /// Starts one of the agent's ideas: what it opens with is sent at once.
    func start(_ idea: AgentIdea) async {
        do {
            let started = try await connection.startIdea(idea.id, in: conversationId, language: Locale.current.language.languageCode?.identifier ?? "en")
            ideas = []
            if let opened = started.conversationId, opened != conversationId {
                app.openedConversationId = opened
                return
            }
            await sendSuggestion(started.openingRequest)
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    /// Replies to a line: its words are quoted with what is sent next.
    func reply(to line: TranscriptLine) {
        guard let messageId = line.messageId else { return }
        replyingTo = AgentReference(
            agentMessageId: messageId, quotedRole: line.kind == .person ? "user" : "assistant",
            quotedText: String(line.text.prefix(1000)))
    }

    func approve(_ line: TranscriptLine, isApproved: Bool) async {
        do {
            if try await connection.resolveConfirmation(runId: line.runId, callId: line.callId, isApproved: isApproved) {
                transcript.settle(cardId: line.id, resolution: isApproved ? "approved" : "declined")
                sentCount += 1
            } else {
                transcript.remove(cardId: line.id)
                sendProblemText = String(localized: "That card was already answered, or is no longer waiting.")
            }
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    func answer(_ line: TranscriptLine, with answer: String) async {
        do {
            if try await connection.answerQuestion(runId: line.runId, callId: line.callId, answer: answer) {
                transcript.settle(cardId: line.id, resolution: answer == Self.chatAboutIt ? String(localized: "Chatting about it instead") : answer)
                sentCount += 1
            } else {
                transcript.remove(cardId: line.id)
                sendProblemText = String(localized: "That card was already answered, or is no longer waiting.")
            }
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    /// What a question card answers when the person would rather talk than
    /// pick (askuser.ChatAboutIt on the server), the same in every language.
    static let chatAboutIt = "[chat about it]"

    /// Answers a coding session's question with the options chosen, or with
    /// what to type into it.
    func answerCodingQuestion(_ line: TranscriptLine, options: [CodingQuestion.Option], freeText: String?) async {
        guard let question = line.codingQuestion else { return }
        do {
            let answeredWith = try await connection.answerCodingQuestion(question, options: options, freeText: freeText)
            transcript.settle(cardId: line.id, resolution: answeredWith)
            sentCount += 1
        } catch TeaNodeError.refused(let reason) where reason.contains("already answered") {
            // Answered at the computer, or by another device: the card folds
            // away so typing goes to the conversation again, as the
            // dashboard's card does.
            transcript.settle(cardId: line.id, resolution: String(localized: "Answered elsewhere"))
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    func stop() async {
        guard let runningRunId else { return }
        do {
            try await connection.stopRun(runningRunId)
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    /// Answers the agent asking where the person is, when this app started
    /// the turn that asks.
    private func answerLocation(_ event: AgentEvent) {
        guard let callId = event.callId, event.drawerId == app.drawerId, !answeredLocationCalls.contains(callId) else {
            return
        }
        answeredLocationCalls.insert(callId)
        Task {
            let reading = await LocationReader.current()
            try? await connection.answerLocation(
                runId: event.runId, callId: callId, latitudeDegrees: reading.latitudeDegrees,
                longitudeDegrees: reading.longitudeDegrees, accuracyMeters: reading.accuracyMeters,
                measuredAt: reading.measuredAt, errorMessage: reading.problemText)
        }
    }
}

/// PendingFile is a file chosen to send with the next message: a picture,
/// made small enough, or any other file as it is.
struct PendingFile: Identifiable {
    let id = UUID()
    let name: String
    let contentType: String
    let data: Data
    /// What the composer shows for it: the picture itself, or nothing.
    let preview: UIImage?

    /// The longest side a picture is sent at: plenty for the agent to read
    /// a receipt or a screen, and a fraction of what a camera takes.
    static let longestSide: CGFloat = 2048

    /// The most the server takes in one message (the agent's
    /// maxAttachmentBytes, 25 MB unless the operator changed it).
    static let bytesMost = 25 << 20

    /// A picture from the library or the camera, as a JPEG at most
    /// longestSide on its longest side, whatever it was taken as.
    init?(picture image: UIImage, index: Int) {
        let scale = min(1, Self.longestSide / max(image.size.width, image.size.height))
        let size = CGSize(width: (image.size.width * scale).rounded(), height: (image.size.height * scale).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let resized = UIGraphicsImageRenderer(size: size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: size))
        }
        guard let jpeg = resized.jpegData(compressionQuality: 0.8) else { return nil }
        self.name = "picture-\(index + 1).jpg"
        self.contentType = "image/jpeg"
        self.data = jpeg
        self.preview = resized
    }

    init?(pictureData data: Data, index: Int) {
        guard let image = UIImage(data: data) else { return nil }
        self.init(picture: image, index: index)
    }

    /// A file from Files, sent as it is.
    init?(fileAt address: URL) {
        let isScoped = address.startAccessingSecurityScopedResource()
        defer { if isScoped { address.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: address) else { return nil }
        self.name = address.lastPathComponent
        self.contentType = UTType(filenameExtension: address.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
        self.data = data
        self.preview = contentType.hasPrefix("image/") ? UIImage(data: data) : nil
    }
}

/// LocationReader reads where the phone is, once, asking the person first.
enum LocationReader {
    struct Reading: Sendable {
        var latitudeDegrees: Double?
        var longitudeDegrees: Double?
        var accuracyMeters: Double?
        var measuredAt: Date?
        var problemText: String?
    }

    @MainActor
    static func current() async -> Reading {
        let manager = CLLocationManager()
        if manager.authorizationStatus == .notDetermined {
            manager.requestWhenInUseAuthorization()
        }
        return await withTaskGroup(of: Reading.self) { group in
            group.addTask {
                do {
                    for try await update in CLLocationUpdate.liveUpdates() {
                        if update.authorizationDenied || update.authorizationDeniedGlobally {
                            return Reading(problemText: "The person has not allowed TeaNode to know where the phone is.")
                        }
                        if let location = update.location {
                            return Reading(
                                latitudeDegrees: location.coordinate.latitude, longitudeDegrees: location.coordinate.longitude,
                                accuracyMeters: location.horizontalAccuracy, measuredAt: location.timestamp)
                        }
                    }
                } catch {}
                return Reading(problemText: "The phone could not tell where it is.")
            }
            group.addTask {
                try? await Task.sleep(for: .seconds(20))
                return Reading(problemText: "The phone could not tell where it is in time.")
            }
            let first = await group.next() ?? Reading(problemText: "The phone could not tell where it is.")
            group.cancelAll()
            withExtendedLifetime(manager) {}
            return first
        }
    }
}
