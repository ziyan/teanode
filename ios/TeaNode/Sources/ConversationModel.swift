import CoreLocation
import Foundation
import Observation
import TeaNodeKit
import UIKit

/// ConversationModel is one conversation as the screen shows it: its
/// transcript, kept up to date from the server's feed, and what the person
/// sends into it.
@MainActor
@Observable
final class ConversationModel {
    /// The conversation's id; nil for the main conversation until it is
    /// loaded.
    private(set) var conversationId: String?
    private(set) var title: String
    private(set) var transcript = Transcript(messages: [])
    private(set) var isLoaded = false
    private(set) var loadProblemText: String?

    var draft = ""
    var pictures: [PickedPicture] = []
    private(set) var isSending = false
    var sendProblemText: String?

    /// Whether the send problem is on screen; dismissing it clears it.
    var isShowingSendProblem: Bool {
        get { sendProblemText != nil }
        set { if !newValue { sendProblemText = nil } }
    }

    /// The turn running now, which the stop button stops.
    private(set) var runningRunId: String?

    /// Changes when something is sent or a card is settled, for the haptic.
    private(set) var sentCount = 0

    private let app: AppModel
    private let connection: Connection
    private var answeredLocationCalls: Set<String> = []

    /// A coding session's question is being told: the card it is drawn as
    /// comes from a note the feed does not carry, so the conversation is
    /// read again once the telling is done.
    private var isCodingQuestionArriving = false

    init(conversation: AgentConversation?, app: AppModel, connection: Connection) {
        self.conversationId = conversation?.id
        self.title = conversation.map(ConversationModel.title(of:)) ?? String(localized: "Your agent")
        self.app = app
        self.connection = connection
    }

    static func title(of conversation: AgentConversation) -> String {
        if conversation.isMain {
            return String(localized: "Your agent")
        }
        if let title = conversation.title, !title.isEmpty {
            return title
        }
        return String(localized: "New conversation")
    }

    /// Loads the conversation and follows its feed until the screen goes
    /// away, loading again and following anew whenever the feed drops.
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
            delay = min(delay * 2, .seconds(30))
        }
    }

    /// Reads the conversation's newest messages and open cards.
    func load() async throws {
        let page = try await connection.conversation(conversationId)
        if let conversation = page.conversation {
            conversationId = conversation.id
            title = Self.title(of: conversation)
        }
        transcript = Transcript(messages: page.messages, interactions: page.interactions)
        isLoaded = true
        loadProblemText = nil
    }

    private func apply(_ event: AgentEvent) {
        transcript.apply(event)
        switch event.kind {
        case "asked":
            runningRunId = event.runId
            if event.text.map({ CheckInOrigin.of($0) == .herdrQuestion }) == true {
                isCodingQuestionArriving = true
            }
        case "done":
            if runningRunId == event.runId { runningRunId = nil }
            if isCodingQuestionArriving {
                isCodingQuestionArriving = false
                Task { try? await load() }
            }
        case "error":
            if runningRunId == event.runId { runningRunId = nil }
            sendProblemText = event.error
        case "titled":
            if let text = event.text, !text.isEmpty { title = text }
        case "locate":
            answerLocation(event)
        default:
            break
        }
    }

    var canSend: Bool {
        !isSending && (!draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !pictures.isEmpty)
    }

    /// Sends what is typed: an answer to the question waiting, when one is,
    /// or a new message.
    func send() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSend else { return }
        if let question = transcript.openQuestion, pictures.isEmpty {
            draft = ""
            if question.kind == .codingQuestion {
                await answerCodingQuestion(question, options: [], freeText: text)
            } else {
                await answer(question, with: text)
            }
            return
        }
        isSending = true
        defer { isSending = false }
        let sending = pictures
        draft = ""
        pictures = []
        do {
            var attachments: [AgentAttachment] = []
            for picture in sending {
                let id = try await connection.upload(name: picture.name, contentType: "image/jpeg", data: picture.data)
                attachments.append(AgentAttachment(id: id, name: picture.name, contentType: "image/jpeg", size: picture.data.count))
            }
            transcript.addSent(text, attachments: attachments)
            let asked = try await connection.ask(
                text, in: conversationId, attachmentIds: attachments.map(\.id), drawerId: app.drawerId)
            runningRunId = asked.runId
            sentCount += 1
            if conversationId == nil {
                conversationId = asked.conversationId
            }
        } catch {
            transcript.withdrawSent()
            draft = text
            pictures = sending
            sendProblemText = app.describe(error)
        }
    }

    func approve(_ line: TranscriptLine, isApproved: Bool) async {
        do {
            try await connection.resolveConfirmation(runId: line.runId, callId: line.callId, isApproved: isApproved)
            transcript.settle(cardId: line.id, resolution: isApproved ? "approved" : "declined")
            sentCount += 1
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    func answer(_ line: TranscriptLine, with answer: String) async {
        do {
            try await connection.answerQuestion(runId: line.runId, callId: line.callId, answer: answer)
            transcript.settle(cardId: line.id, resolution: answer)
            sentCount += 1
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    /// Answers a coding session's question with the options chosen, or with
    /// what to type into it.
    func answerCodingQuestion(_ line: TranscriptLine, options: [CodingQuestion.Option], freeText: String?) async {
        guard let question = line.codingQuestion else { return }
        do {
            let answeredWith = try await connection.answerCodingQuestion(question, options: options, freeText: freeText)
            transcript.settle(cardId: line.id, resolution: answeredWith)
            sentCount += 1
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

/// PickedPicture is a picture chosen to send, already made small enough.
struct PickedPicture: Identifiable {
    let id = UUID()
    let name: String
    let data: Data
    let preview: UIImage

    /// The longest side a picture is sent at: plenty for the agent to read
    /// a receipt or a screen, and a fraction of what a camera takes.
    static let longestSide: CGFloat = 2048

    /// A picture from the library as a JPEG at most longestSide on its
    /// longest side, whatever it was taken as.
    init?(data: Data, index: Int) {
        guard let image = UIImage(data: data) else { return nil }
        let scale = min(1, Self.longestSide / max(image.size.width, image.size.height))
        let size = CGSize(width: (image.size.width * scale).rounded(), height: (image.size.height * scale).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let resized = UIGraphicsImageRenderer(size: size, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: size))
        }
        guard let jpeg = resized.jpegData(compressionQuality: 0.8) else { return nil }
        self.name = "picture-\(index + 1).jpg"
        self.data = jpeg
        self.preview = resized
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
