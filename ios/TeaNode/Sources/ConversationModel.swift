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
    /// Who shared the pages shared with the person, by display name, so that
    /// a shared citation in an answer's words ("people/a#3 (shared by Ada)")
    /// is linked to its owner's page. Read once, when an answer first cites
    /// one.
    private(set) var sharedOwnerUsernames: [String: String] = [:]
    private var didAskSharedOwners = false
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
        didSet {
            saveDraft()
            if draft != oldValue { tellTyping() }
        }
    }
    var pendingFiles: [PendingFile] = []
    /// The message being replied to, shown above the composer.
    /// What the next message refers to: a message replied to, and mails,
    /// pages and transactions asked about, several at once as the
    /// dashboard's drawer holds them.
    var references: [AgentReference] = []
    /// A message opened to select part of its words, from its menu.
    var selectingText: String?
    /// The whole of a message too long for its bubble, open to read.
    var readingText: String?
    /// A message of a chat opened to change what it says, and one asked
    /// about taking back.
    var editingLine: TranscriptLine?
    var deletingLine: TranscriptLine?

    /// The message replied to, of the references: one at most.
    var replyingTo: AgentReference? {
        get { references.first { $0.agentMessageId != nil } }
        set {
            references.removeAll { $0.agentMessageId != nil }
            if let newValue { references.insert(newValue, at: 0) }
        }
    }

    /// Adds something asked about, once.
    func refer(to reference: AgentReference) {
        if !references.contains(reference) { references.append(reference) }
    }
    private(set) var isSending = false
    /// The files of the message being sent, and those of them already on
    /// the server: they stay in the composer, each with its progress, until
    /// the message is sent.
    private(set) var uploadingFileIds: Set<UUID> = []
    private(set) var uploadedFileIds: Set<UUID> = []
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
    /// Counts the cards settled from this app as soon as they were answered, which shrink the list under the scroll.
    private(set) var cardSettledCount = 0

    /// For a chat: which of its members the person is, who is writing in it
    /// now, and the newest message told to the server as read.
    private(set) var ownUserId: String?
    private(set) var typing = ChatTyping()
    private var typingToldAt = Date.distantPast
    private var typingPruning: Task<Void, Never>?
    private var readThroughMessageId = ""
    /// Whether ownUserId was read from this model's own connection, which
    /// is the one thing that says whose chat this is: the id the app held
    /// when the model was made may be an earlier account's.
    private var isOwnUserIdConfirmed = false
    private let confirmingSent = ConfirmingTasks()

    /// Whether this is a chat: a conversation with people in it.
    var isChat: Bool { conversation?.isChat == true }
    var chat: ChatSummary? { conversation?.chat }

    /// The call with the agent, while there is one, and whether the server
    /// offers calls and at what rate it takes speech.
    var call: VoiceCall?
    private(set) var voiceSampleRate: Int?

    private let app: AppModel
    let connection: Connection
    private var answeredLocationCalls: Set<String> = []

    /// How recent a question must be for typing to answer it rather than
    /// start a turn: one from an hour ago is not what the person is
    /// answering.
    static let cardFresh: TimeInterval = 60 * 60

    init(conversation: AgentConversation?, app: AppModel, connection: Connection) {
        self.conversationId = conversation?.id
        self.conversation = conversation
        self.ownUserId = app.ownUserId
        self.title = conversation.map { ConversationModel.title(of: $0, agentTitle: app.agentTitle, ownUserId: app.ownUserId) } ?? app.agentTitle
        self.app = app
        self.connection = connection
        // Named once: the main conversation learns its id when it loads,
        // and a key made from that would never be read at the next launch.
        self.draftKey = ConversationDraft.storageKey(
            accountKey: connection.accountKey, conversationId: conversation.flatMap { $0.isMain ? nil : $0.id })
        self.draft = UserDefaults.standard.string(forKey: draftKey) ?? ""
    }

    static func title(of conversation: AgentConversation, agentTitle: String, ownUserId: String? = nil) -> String {
        if conversation.isMain {
            return agentTitle
        }
        if conversation.isChat {
            let fallback = conversation.title.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "Chat")
            return conversation.chat?.name(ownUserId: ownUserId, deletedAccountNote: String(localized: "(deleted account)"), fallback: fallback)
                ?? fallback
        }
        if let title = conversation.title, !title.isEmpty {
            return title
        }
        return String(localized: "New conversation")
    }

    /// The draft is kept per conversation, as the dashboard keeps it.
    private let draftKey: String

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
                try await load(isFeedReplaying: true)
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

    /// Reads the conversation's newest messages and open cards. When the
    /// feed is about to be opened anew, a turn still running is replayed by
    /// it, which says it runs again, and one that ended while the feed was
    /// down is not; the turns running and the words of an answer being
    /// written are then forgotten. Read again while the feed is up (a member
    /// changed, a message was too long to travel whole) they are kept, since
    /// nothing will say them again.
    func load(isFeedReplaying: Bool = false) async throws {
        let page = try await connection.conversation(conversationId, first: max(100, messages.count))
        if page.conversation?.isChat == true, !isOwnUserIdConfirmed, let userId = try? await connection.userId() {
            isOwnUserIdConfirmed = true
            if userId != ownUserId { ownUserId = userId }
        }
        if let conversation = page.conversation {
            conversationId = conversation.id
            self.conversation = conversation
            title = Self.title(of: conversation, agentTitle: app.agentTitle, ownUserId: ownUserId)
        }
        messages = page.messages
        interactions = page.interactions
        total = page.total
        todos = page.todos
        actingAs = page.actingAs
        let earlier = transcript
        transcript = Transcript(messages: messages, interactions: interactions, isChat: isChat)
        if isFeedReplaying {
            runningRunIds = []
        } else {
            transcript.keepStreaming(from: earlier)
        }
        isLoaded = true
        loadProblemText = nil
        #if DEBUG
            // `-TeaNodeScreen chat-direct`: Sam is typing, for an hour, since no feed says so or says it stopped.
            if DemoServer.isRequested, DemoServer.screen == "chat-direct" {
                typing.note(userId: "sam", name: "Sam", at: Date().addingTimeInterval(60 * 60))
            }
            // `-TeaNodeScreen chat-mention`: an @ typed in the box, so the menu of agents it offers is open.
            if DemoServer.isRequested, DemoServer.screen == "chat-mention", draft.isEmpty { draft = "@" }
        #endif
        loadSharedOwnersIfCited()
        // A chat is people: no ideas to start it with, and no call to the
        // agent.
        if isChat {
            await markRead()
            return
        }
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
        app.currentCall = call
        call.start()
    }

    /// Sends what was said on a call as a turn, and returns the turn, whose
    /// answers the call reads out.
    func sendSpoken(_ text: String, interruptedAnswer: (heardText: String, unheardText: String)?) async -> String? {
        let sentLineId = transcript.addSent(text)
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
            transcript.withdrawSent(lineId: sentLineId)
            sendProblemText = app.describe(error)
            return nil
        }
    }

    /// Reads who shared the pages cited in the answers, once.
    private func loadSharedOwnersIfCited() {
        guard !didAskSharedOwners,
            messages.contains(where: { $0.role == "assistant" && SharedCitations.hasCitation(in: $0.content) })
        else { return }
        didAskSharedOwners = true
        Task {
            // The words still read without it; they are only not a link.
            if let roots = try? await connection.sharedKnowledgeRoots() {
                sharedOwnerUsernames = SharedCitations.usernames(of: roots)
            }
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
            let earlierMessages = page.messages.filter { !known.contains($0.id) }
            messages = earlierMessages + messages
            total = page.total
            transcript.prepend(messages: messages, earlierCount: earlierMessages.count, isChat: isChat)
        } catch {
            sendProblemText = app.describe(error)
        }
    }

    private func loadIdeas() async {
        ideas = (try? await connection.ideas(language: DisplayLanguage.languageCode))?.prefix(3).map { $0 } ?? []
        if !ideas.isEmpty {
            try? await connection.markIdeasShown(ideas.map(\.id))
        }
    }

    private func apply(_ event: AgentEvent) {
        transcript.apply(event)
        call?.follow(event)
        // The agent's reaction arriving is felt, as a friend's tapback is:
        // a double tap, apart from the light one the person's own makes.
        if event.kind == "reacted", event.reactorKind == "agent", !(event.emoji ?? "").isEmpty {
            UINotificationFeedbackGenerator().notificationOccurred(.success)
        }
        switch event.kind {
        case "asked":
            if !runningRunIds.contains(event.runId) {
                runningRunIds.append(event.runId)
            }
        case "done", "error":
            runningRunIds.removeAll { $0 == event.runId }
            if event.kind == "error", let text = event.error, !text.isEmpty {
                sendProblemText = text.asReasonSentence
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
        case "said", "edited":
            // A message too long to travel whole is read again with the chat.
            if event.message == nil || event.isMessageCut == true {
                Task { try? await load() }
            } else if event.kind == "said" {
                // Whoever said it has finished: a person is no longer typing,
                // and an agent that answered is no longer thinking.
                if event.message?.authorKind == "agent" {
                    if let agentId = event.message?.authorAgentId, !agentId.isEmpty { typing.clear(agentId: agentId) }
                } else if let author = event.message?.authorUserId, !author.isEmpty {
                    typing.clear(userId: author)
                }
                Task { await markRead() }
            }
        case "members":
            // Somebody was added, taken out or left, or changed what their
            // agent does here (who can be asked, who remembers the chat): the
            // members are read again with the chat.
            Task { try? await load() }
        case "read":
            if let userId = event.userId, let messageId = event.messageId, let chat = conversation?.chat {
                conversation?.chat = chat.withRead(userId: userId, messageId: messageId)
            }
        case "typing":
            // An agent working on an answer is told by its agent and is
            // thinking; a person is told by theirs and is typing.
            if let agentId = event.authorAgentId, !agentId.isEmpty {
                typing.note(agentId: agentId, name: event.name ?? "", ownerName: event.authorOwnerName ?? "")
            } else if let userId = event.userId, userId != ownUserId {
                typing.note(userId: userId, name: event.name ?? "")
            } else {
                break
            }
            typingPruning?.cancel()
            typingPruning = Task {
                try? await Task.sleep(for: .seconds(ChatTyping.shownSeconds + 0.5))
                if !Task.isCancelled { typing.prune() }
            }
        case "titled":
            if let text = event.text, !text.isEmpty { title = text }
        case "locate":
            answerLocation(event)
        case "navigate":
            // The agent's open_page: the app goes there, as the dashboard
            // does, but only for a turn this phone is following now.
            if let path = event.text { app.show(path: path, once: "\(event.runId)-\(event.sequence)") }
        default:
            break
        }
    }

    var canSend: Bool {
        canWrite && !isSending && (!draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !pendingFiles.isEmpty)
    }

    /// Whether the person can still write here: always, but in a chat they
    /// have left, or a direct chat with somebody they no longer share a
    /// circle with, which they can read and not write to.
    var canWrite: Bool {
        guard isChat, let chat else { return true }
        return !chat.isRemoved && !(chat.chatKind == ChatKind.direct.rawValue && chat.otherMembers(ownUserId: ownUserId).contains(where: \.isRemoved))
    }

    /// The question that typing answers: the newest unanswered one, while
    /// its turn runs or for an hour after it was asked.
    var freshQuestion: TranscriptLine? {
        guard let question = transcript.openQuestion else { return nil }
        if runningRunIds.contains(question.runId) || question.kind == .codingQuestion { return question }
        guard let at = ServerTime.date(question.at) else { return question }
        return Date().timeIntervalSince(at) < Self.cardFresh ? question : nil
    }

    /// The reactions offered first when a message is held, as Messages
    /// offers its own.
    static let quickReactions = ["👍", "❤️", "😂", "😮", "😢", "🙏", "🔥", "🎉"]

    /// Puts the person's reaction on a message, or takes it away when it is
    /// the one already there. It shows at once, and goes back if the server
    /// refuses it.
    func react(to line: TranscriptLine, with emoji: String) async {
        guard let messageId = line.messageId else { return }
        let previous = line.reaction(of: "person", userId: ownUserId) ?? ""
        let chosen = previous == emoji ? "" : emoji
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
        transcript.react(messageId: messageId, emoji: chosen, reactorKind: "person", reactorUserId: ownUserId)
        do {
            try await connection.react(to: messageId, emoji: chosen)
        } catch {
            transcript.react(messageId: messageId, emoji: previous, reactorKind: "person", reactorUserId: ownUserId)
            sendProblemText = app.describe(error)
        }
    }

    /// Sends what is typed: an answer to the question waiting, when one is
    /// fresh, or a new message. In a chat typing never answers a card: the
    /// words are for the people in it.
    func send() async {
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSend else { return }
        if !isChat, let question = freshQuestion, pendingFiles.isEmpty, references.isEmpty {
            isSending = true
            defer { isSending = false }
            draft = ""
            let isAnswered: Bool
            if question.kind == .codingQuestion {
                isAnswered = await answerCodingQuestion(question, options: [], freeText: text)
            } else {
                isAnswered = await answer(question, with: text)
            }
            if !isAnswered, draft.isEmpty { draft = text }
            return
        }
        if pendingFiles.reduce(0, { $0 + $1.data.count }) > PendingFile.bytesMost {
            sendProblemText = String(localized: "The files come to more than 25 MB, more than the server takes in one message.")
            return
        }
        let sending = pendingFiles
        let sendingIds = Set(sending.map(\.id))
        isSending = true
        uploadingFileIds = sendingIds
        uploadedFileIds = []
        defer {
            isSending = false
            uploadingFileIds = []
            uploadedFileIds = []
        }
        let reply = replyingTo
        let referring = references
        // In a chat, which agents the message is for: those chosen from the @
        // whose names are still in it, and the agent of a message it replies
        // to.
        let addressed = addressedAgentIds(for: text)
        let choosing = chosenAgents
        draft = ""
        references = []
        chosenAgents = []
        var sentLineId: String?
        do {
            var attachments: [AgentAttachment] = []
            for file in sending {
                let id = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
                attachments.append(AgentAttachment(id: id, name: file.name, contentType: file.contentType, size: file.data.count))
                uploadedFileIds.insert(file.id)
            }
            // Files added while these were sent stay for the next message.
            pendingFiles.removeAll { sendingIds.contains($0.id) }
            // A chat takes files with no words; a turn with the agent needs
            // something to read.
            let message = text.isEmpty && !isChat ? "(files)" : text
            sentLineId = transcript.addSent(message, attachments: attachments, replyingTo: reply, author: ownAuthor)
            let asked = try await connection.ask(
                message, in: conversationId, attachmentIds: attachments.map(\.id), references: referring,
                drawerId: app.drawerId, addressedAgentIds: addressed)
            // A message in a chat comes back as the chat's own, and if it does
            // not, the chat is read again so that nothing sent looks unsent for
            // long. It has a turn to follow only when it asked the person's own
            // agent.
            if isChat, let sentLineId { confirmSoon(sentLineId) }
            if !asked.runId.isEmpty, !runningRunIds.contains(asked.runId) {
                runningRunIds.append(asked.runId)
            }
            sentCount += 1
            if conversationId == nil {
                conversationId = asked.conversationId
            }
            ideas = []
        } catch {
            if let sentLineId { transcript.withdrawSent(lineId: sentLineId) }
            if draft.isEmpty {
                draft = text
                chosenAgents = choosing
            }
            pendingFiles = sending + pendingFiles.filter { !sendingIds.contains($0.id) }
            references = referring
            sendProblemText = app.describe(error)
        }
    }

    /// Sends a suggested reply as it is, leaving the draft alone.
    func sendSuggestion(_ suggestion: String) async {
        let sentLineId = transcript.addSent(suggestion)
        do {
            let asked = try await connection.ask(suggestion, in: conversationId, drawerId: app.drawerId)
            if !runningRunIds.contains(asked.runId) {
                runningRunIds.append(asked.runId)
            }
            sentCount += 1
        } catch {
            transcript.withdrawSent(lineId: sentLineId)
            sendProblemText = app.describe(error)
        }
    }

    /// Starts one of the agent's ideas: what it opens with is sent at once.
    func start(_ idea: AgentIdea) async {
        do {
            let started = try await connection.startIdea(idea.id, in: conversationId, language: DisplayLanguage.languageCode)
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
        // In a chat the person's own words are the user's, and everybody
        // else's are what an answer is in a conversation with an agent.
        let isOwn = line.author.map(\.isOwnWords) ?? (line.kind == .person)
        replyingTo = AgentReference(agentMessageId: messageId, quotedRole: isOwn ? "user" : "assistant", quotedText: String(line.text.prefix(1000)))
    }

    // MARK: Chats

    /// The agents chosen from the @ for the message being written.
    private(set) var chosenAgents: [AddressableAgent] = []

    /// The agents an @ offers in this chat: the person's own, and those of
    /// members who let the others ask.
    var addressableAgents: [AddressableAgent] {
        guard isChat, let chat, !chat.isRemoved else { return [] }
        return chat.addressableAgents(ownUserId: ownUserId, deletedAccountNote: String(localized: "(deleted account)"))
    }

    /// The agents the @ at the end of the words offers, those the letters
    /// typed so far match; none when no @ is being typed.
    var mentionSuggestions: [AddressableAgent] {
        guard isChat, let query = ChatMention.query(in: draft) else { return [] }
        return ChatMention.matching(addressableAgents, query: query)
    }

    /// Puts the chosen agent's name where the @ is, and keeps the choice for
    /// the message.
    func choose(mention agent: AddressableAgent) {
        draft = ChatMention.choosing(agent, in: draft)
        if !chosenAgents.contains(where: { $0.agentId == agent.agentId }) { chosenAgents.append(agent) }
    }

    /// The agents a message is for: those chosen whose names are still in the
    /// words, and the agent of a message it replies to.
    private func addressedAgentIds(for text: String) -> [String] {
        guard isChat else { return [] }
        var repliedToAgentId: String?
        if let replied = replyingTo?.agentMessageId,
            let author = transcript.lines.first(where: { $0.messageId == replied })?.author, author.isAgent
        {
            repliedToAgentId = author.authorAgentId
        }
        return ChatMention.addressed(in: text, chosen: chosenAgents, repliedToAgentId: repliedToAgentId)
    }

    /// Makes the person's agent remember this chat from now on, or stop. The
    /// switch moves at once and goes back if the server refuses; the refusal
    /// is returned for the members sheet to say.
    func setAgentReading(_ isReading: Bool) async -> String? {
        guard let conversationId, let ownUserId, let before = chat?.member(ownUserId) else { return nil }
        conversation?.chat = chat?.withAgentSwitches(userId: ownUserId, isAgentReading: isReading)
        return await changeChatReporting(
            isReportedOnScreen: false,
            undo: { self.conversation?.chat = self.chat?.withAgentSwitches(userId: ownUserId, isAgentReading: before.isAgentReading) }
        ) { try await connection.setChatAgentReading(conversationId, isReading: isReading) }
    }

    /// Lets the others in this chat address the person's agent, or stops them,
    /// as setAgentReading does.
    func setAgentAskableByOthers(_ isAskable: Bool) async -> String? {
        guard let conversationId, let ownUserId, let before = chat?.member(ownUserId) else { return nil }
        conversation?.chat = chat?.withAgentSwitches(userId: ownUserId, isAgentAskableByOthers: isAskable)
        return await changeChatReporting(
            isReportedOnScreen: false,
            undo: {
                self.conversation?.chat = self.chat?.withAgentSwitches(
                    userId: ownUserId, isAgentAskableByOthers: before.isAgentAskableByOthers ?? false)
            }
        ) { try await connection.setChatAgentAskableByOthers(conversationId, isAskable: isAskable) }
    }

    /// The person as the author of a message they have just sent in a chat,
    /// until the chat says it was said; nil outside a chat.
    private var ownAuthor: ChatAuthor? {
        isChat ? ChatAuthor(authorKind: "person", authorUserId: ownUserId ?? "", authorName: "", isMine: true) : nil
    }

    /// Reads the chat again if a message just sent has not come back as the
    /// chat's own within a few seconds.
    private func confirmSoon(_ lineId: String) {
        confirmingSent.tasks[lineId] = Task { [weak self] in
            try? await Task.sleep(for: .seconds(3))
            guard !Task.isCancelled, let self else { return }
            confirmingSent.tasks[lineId] = nil
            if transcript.lines.contains(where: { $0.id == lineId }) { try? await load() }
        }
    }

    /// Tells the server the person has read the chat through its newest
    /// message, which moves the mark the others see and clears the count in
    /// the list. Only while the screen is following the chat, which is only
    /// while the app is in front.
    func markRead() async {
        guard isChat, let conversationId, let newest = transcript.newestChatMessageId, newest > readThroughMessageId else { return }
        let previous = readThroughMessageId
        readThroughMessageId = newest
        do {
            try await connection.markChatRead(conversationId, through: newest)
            conversation?.chat?.unreadCount = 0
            await app.refreshChats()
        } catch {
            readThroughMessageId = previous
        }
    }

    /// Lets the others know words are being written, at most every few
    /// seconds however fast the keys.
    private func tellTyping() {
        guard isChat, let conversationId, !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        let now = Date()
        guard now.timeIntervalSince(typingToldAt) >= ChatTyping.sentEverySeconds else { return }
        typingToldAt = now
        Task { try? await connection.setChatTyping(conversationId) }
    }

    /// Whether the person may take a message back: their own, or any in a
    /// group they are an admin of.
    func canDelete(_ line: TranscriptLine) -> Bool {
        guard line.author != nil, line.messageId != nil, !line.isDeleted, !line.isBeingSent else { return false }
        return line.author?.isOwnWords == true || (chat?.isGroup == true && chat?.isAdmin(ownUserId: ownUserId) == true)
    }

    /// Whether the person may change what a message says: their own words.
    func canEdit(_ line: TranscriptLine) -> Bool {
        line.author?.isOwnWords == true && line.messageId != nil && !line.isDeleted && !line.isBeingSent
    }

    func edit(_ line: TranscriptLine, to text: String) async {
        let words = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let messageId = line.messageId, !words.isEmpty, words != line.text else { return }
        await changeChat { try await connection.editChatMessage(messageId, message: words) }
    }

    func delete(_ line: TranscriptLine) async {
        guard let messageId = line.messageId else { return }
        await changeChat { try await connection.deleteChatMessage(messageId) }
    }

    func rename(to title: String) async {
        guard let conversationId else { return }
        await changeChat { try await connection.renameChat(conversationId, title: title) }
    }

    /// Silences the chat's notifications until a time, or with none stops
    /// silencing them.
    func mute(until: Date?) async {
        guard let conversationId else { return }
        await changeChat { try await connection.muteChat(conversationId, until: until) }
    }

    /// Puts people in the group; a refusal is thrown for the sheet that
    /// asked to say.
    func addMembers(_ userIds: [String]) async throws {
        guard let conversationId, !userIds.isEmpty else { return }
        try await connection.addChatMembers(conversationId, userIds: userIds)
        try? await load()
    }

    /// Takes somebody out of the group; the refusal is returned for the
    /// members sheet to say, since the screen under it is not in view.
    func removeMember(_ member: ChatMember) async -> String? {
        guard let conversationId else { return nil }
        return await changeChatReporting(isReportedOnScreen: false, undo: nil) {
            try await connection.removeChatMember(conversationId, userId: member.userId)
        }
    }

    /// Leaves the group; false when the server refused.
    func leave() async -> Bool {
        guard let conversationId else { return false }
        do {
            try await connection.leaveChat(conversationId)
            await app.refreshChats()
            return true
        } catch {
            sendProblemText = app.describe(error)
            return false
        }
    }

    /// Makes a change to the chat and reads it again, so what the server
    /// now has is what shows. A refusal is told as any send problem is, or,
    /// when the change was made from a sheet that covers the screen, only
    /// returned, for the sheet to say; `undo` first puts back what was shown
    /// ahead of the answer.
    private func changeChat(_ change: () async throws -> Void) async {
        _ = await changeChatReporting(isReportedOnScreen: true, undo: nil, change)
    }

    private func changeChatReporting(isReportedOnScreen: Bool, undo: (() -> Void)?, _ change: () async throws -> Void) async -> String? {
        var problemText: String?
        do {
            try await change()
        } catch {
            undo?()
            problemText = app.describe(error)
            if isReportedOnScreen { sendProblemText = problemText }
        }
        try? await load()
        return problemText
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

    @discardableResult
    func answer(_ line: TranscriptLine, with answer: String) async -> Bool {
        do {
            if try await connection.answerQuestion(runId: line.runId, callId: line.callId, answer: answer) {
                transcript.settle(cardId: line.id, resolution: answer == Self.chatAboutIt ? String(localized: "Chatting about it instead") : answer)
                sentCount += 1
                return true
            } else {
                transcript.remove(cardId: line.id)
                sendProblemText = String(localized: "That card was already answered, or is no longer waiting.")
            }
        } catch {
            sendProblemText = app.describe(error)
        }
        return false
    }

    /// What a question card answers when the person would rather talk than
    /// pick (askuser.ChatAboutIt on the server), the same in every language.
    static let chatAboutIt = "[chat about it]"

    /// Answers a coding session's question with the options chosen, or with
    /// what to type into it.
    @discardableResult
    func answerCodingQuestion(_ line: TranscriptLine, options: [CodingQuestion.Option], freeText: String?) async -> Bool {
        // The card as it is now, not as the view last drew it: a second tap
        // before the view caught up, or an answer from the computer, finds
        // it settled already.
        guard let question = line.codingQuestion, transcript.lines.first(where: { $0.id == line.id })?.isDone == false else { return false }
        // The card shows the answer as soon as it is chosen: typing it into
        // the session at the computer takes a few seconds, and a card that
        // stays as it was looks like a tap that did nothing. It opens again
        // if the answer does not go through.
        let chosenWords = freeText ?? options.map(\.optionLabel).joined(separator: ", ")
        transcript.settleAwaiting(cardId: line.id, resolution: chosenWords)
        cardSettledCount += 1
        do {
            let answeredWith = try await connection.answerCodingQuestion(question, options: options, freeText: freeText)
            transcript.settle(cardId: line.id, resolution: answeredWith.isEmpty ? chosenWords : answeredWith)
            sentCount += 1
            return true
        } catch TeaNodeError.refused(let reason) where reason.contains("already answered") {
            // Answered at the computer, or by another device: the card folds
            // away so typing goes to the conversation again, as the
            // dashboard's card does, and the note the server publishes for
            // it fills in what it was answered with.
            transcript.settle(cardId: line.id, resolution: "")
        } catch {
            transcript.reopen(cardId: line.id)
            sendProblemText = app.describe(error)
        }
        return false
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

/// ConfirmingTasks holds the reads waiting to confirm a message just sent,
/// and cancels them when the conversation model that started them goes away,
/// which a model's own deinit cannot do from outside the main actor.
final class ConfirmingTasks: @unchecked Sendable {
    var tasks: [String: Task<Void, Never>] = [:]

    deinit {
        for task in tasks.values { task.cancel() }
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
