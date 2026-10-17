import Foundation

// Chats: conversations with people in them. A chat is listed with the other
// conversations, read and followed the way they are, and written to with the
// same composer; this file holds what only a chat needs: who is in it, who
// wrote a message, the operations for starting one and changing who is in it,
// and the small marks (who has read, who is typing) that a conversation with
// an agent never had. The words a screen shows are the app's, since this
// package has no localization.

/// ChatKind is how many people a chat is for.
public enum ChatKind: String, Sendable {
    case direct
    case group
}

/// ChatMember is one person in a chat, as the list of conversations says.
public struct ChatMember: Codable, Hashable, Sendable, Identifiable {
    /// Empty once the account is deleted.
    public let userId: String
    public let username: String
    /// What they were called when last seen, which a deleted account keeps.
    public let name: String?
    public let avatarUrl: String?
    public let avatarColorIndex: Int?
    /// admin or member.
    public let memberRole: String
    /// The account is gone and the name is all that is left.
    public let isDeletedAccount: Bool
    /// They are not in the chat any more; they read it up to when they left.
    public let isRemoved: Bool
    /// The newest message they have read; empty when they have read none.
    public var readThroughMessageId: String?
    /// Their agent remembers this chat.
    public let isAgentReading: Bool
    /// The others in the chat may address their agent there.
    public var isAgentAskableByOthers: Bool? = nil
    /// Which agent is theirs, what it is called and whether it is on; told
    /// for the person's own and for an agent its owner let the others ask,
    /// empty for any other.
    public var agentId: String? = nil
    public var agentName: String? = nil
    public var isAgentActive: Bool? = nil

    /// A deleted account has no id, so its name stands in.
    public var id: String { userId.isEmpty ? "deleted-\(username)-\(name ?? "")" : userId }
    public var isAdmin: Bool { memberRole == CircleMemberRole.admin.rawValue }

    /// What a list of people calls them: their name, or the username when
    /// they have none.
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    public init(
        userId: String, username: String, name: String? = nil, avatarUrl: String? = nil, avatarColorIndex: Int? = nil,
        memberRole: String = "member", isDeletedAccount: Bool = false, isRemoved: Bool = false, readThroughMessageId: String? = nil,
        isAgentReading: Bool = false, isAgentAskableByOthers: Bool? = nil, agentId: String? = nil, agentName: String? = nil,
        isAgentActive: Bool? = nil
    ) {
        self.isAgentAskableByOthers = isAgentAskableByOthers
        self.agentId = agentId
        self.agentName = agentName
        self.isAgentActive = isAgentActive
        self.userId = userId
        self.username = username
        self.name = name
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
        self.memberRole = memberRole
        self.isDeletedAccount = isDeletedAccount
        self.isRemoved = isRemoved
        self.readThroughMessageId = readThroughMessageId
        self.isAgentReading = isAgentReading
    }
}

/// ChatSummary is what a conversation of the kind chat carries beside the
/// fields every conversation has.
public struct ChatSummary: Codable, Hashable, Sendable {
    public let chatId: String
    /// direct or group.
    public let chatKind: String
    public let circleId: String?
    /// The group's name; for a direct chat, the other person's.
    public let title: String?
    public let isCircleChat: Bool
    /// The people in it, then those who left.
    public var members: [ChatMember]
    /// How many messages by others are newer than the person's read mark.
    public var unreadCount: Int
    public var isMuted: Bool
    public var mutedUntil: String?
    /// The person is no longer in it; they read it up to when they left.
    public let isRemoved: Bool

    public init(
        chatId: String, chatKind: String, circleId: String? = nil, title: String? = nil, isCircleChat: Bool = false,
        members: [ChatMember], unreadCount: Int = 0, isMuted: Bool = false, mutedUntil: String? = nil, isRemoved: Bool = false
    ) {
        self.chatId = chatId
        self.chatKind = chatKind
        self.circleId = circleId
        self.title = title
        self.isCircleChat = isCircleChat
        self.members = members
        self.unreadCount = unreadCount
        self.isMuted = isMuted
        self.mutedUntil = mutedUntil
        self.isRemoved = isRemoved
    }

    public var isGroup: Bool { chatKind == ChatKind.group.rawValue }

    /// The people to show a chat by: everybody but the person, and but
    /// those who left a group. A direct chat keeps showing the other
    /// person after they left a circle: it is who the chat is with.
    public func otherMembers(ownUserId: String?) -> [ChatMember] {
        members.filter { member in
            member.userId != ownUserId && (!isGroup || !member.isRemoved)
        }
    }

    /// The people in it now, the person among them.
    public var presentMembers: [ChatMember] { members.filter { !$0.isRemoved } }

    /// Whether the person is an admin of it.
    public func isAdmin(ownUserId: String?) -> Bool {
        members.contains { $0.userId == ownUserId && $0.isAdmin }
    }

    /// Whether the person can add people to it and take them out: a group
    /// that is not a circle's, which follows the circle, and that they are
    /// an admin of.
    public func canChangeMembers(ownUserId: String?) -> Bool {
        isGroup && !isCircleChat && !isRemoved && isAdmin(ownUserId: ownUserId)
    }

    /// Whether the person can leave it from here: a group that is not a
    /// circle's.
    public var canBeLeft: Bool { isGroup && !isCircleChat && !isRemoved }

    /// Whether it can be renamed: the same.
    public var canBeRenamed: Bool { isGroup && !isCircleChat && !isRemoved }

    /// The member with this id, if there is one.
    public func member(_ userId: String) -> ChatMember? {
        members.first { !$0.userId.isEmpty && $0.userId == userId }
    }

    /// A member's read mark moved forward; it never goes back.
    public func withRead(userId: String, messageId: String) -> ChatSummary {
        var changed = self
        changed.members = members.map { member in
            guard member.userId == userId, (member.readThroughMessageId ?? "") < messageId else { return member }
            var moved = member
            moved.readThroughMessageId = messageId
            return moved
        }
        return changed
    }

    /// A chat's name: its title, else the other people's names, which is
    /// all a direct chat is. `deletedAccountNote` follows the name of a
    /// person whose account is gone.
    public func name(ownUserId: String?, deletedAccountNote: String, fallback: String) -> String {
        if let title = title?.trimmingCharacters(in: .whitespacesAndNewlines), !title.isEmpty { return title }
        let names = otherMembers(ownUserId: ownUserId).map { Self.name(of: $0, deletedAccountNote: deletedAccountNote) }
        return names.isEmpty ? fallback : names.joined(separator: ", ")
    }

    /// A person's name, with the note when the account is gone.
    public static func name(of member: ChatMember, deletedAccountNote: String) -> String {
        member.isDeletedAccount ? "\(member.displayName) \(deletedAccountNote)" : member.displayName
    }
}

/// ChatUnread is how many messages wait across the chats that are not
/// muted: the number on the entry to the conversations. A muted chat shows
/// its own count in the list and does not ask for attention from outside.
public enum ChatUnread {
    public static func total(of conversations: [AgentConversation]) -> Int {
        conversations.reduce(0) { sum, conversation in
            guard let chat = conversation.chat, !chat.isMuted else { return sum }
            return sum + chat.unreadCount
        }
    }
}

/// AgentSpeaker is how an agent is named wherever a chat shows it: above its
/// messages and in the line that says it is thinking. Most agents keep the
/// name "Agent", so two people's agents in one chat are told apart by whose
/// they are.
public enum AgentSpeaker {
    /// What an agent is called until its person names it.
    public static let defaultName = "Agent"

    /// "Name · Ada's agent", and "Ada's agent" alone for the default name.
    /// An agent whose owner is not known is called by its own name.
    public static func name(agentName: String, ownerName: String, ownedWords: (String) -> String) -> String {
        let name = agentName.trimmingCharacters(in: .whitespacesAndNewlines)
        let owner = ownerName.trimmingCharacters(in: .whitespacesAndNewlines)
        if owner.isEmpty { return name }
        let owned = ownedWords(owner)
        return name.isEmpty || name == defaultName ? owned : "\(name) · \(owned)"
    }
}

/// ChatAuthor is who wrote a message of a chat.
public struct ChatAuthor: Hashable, Sendable {
    /// person or agent.
    public let authorKind: String
    /// Empty once the account is deleted.
    public let authorUserId: String
    public let authorName: String
    /// On an agent's message, its owner's name: most agents keep the name
    /// "Agent", and whose it is tells two of them apart.
    public let authorOwnerName: String
    public let authorAvatarUrl: String?
    public let authorAvatarColorIndex: Int?
    public let authorAgentId: String
    /// Written by the person reading it or by their agent.
    public let isMine: Bool

    public init(
        authorKind: String, authorUserId: String = "", authorName: String, authorOwnerName: String = "", authorAvatarUrl: String? = nil,
        authorAvatarColorIndex: Int? = nil, authorAgentId: String = "", isMine: Bool = false
    ) {
        self.authorKind = authorKind
        self.authorUserId = authorUserId
        self.authorName = authorName
        self.authorOwnerName = authorOwnerName
        self.authorAvatarUrl = authorAvatarUrl
        self.authorAvatarColorIndex = authorAvatarColorIndex
        self.authorAgentId = authorAgentId
        self.isMine = isMine
    }

    /// The author of a stored message; nil for one that says none, which is
    /// a row of the person's own agent's work.
    init?(_ message: AgentMessage) {
        guard let authorKind = message.authorKind, !authorKind.isEmpty else { return nil }
        self.init(
            authorKind: authorKind, authorUserId: message.authorUserId ?? "", authorName: message.authorName ?? "",
            authorOwnerName: message.authorOwnerName ?? "", authorAvatarUrl: message.authorAvatarUrl, authorAvatarColorIndex: message.authorAvatarColorIndex,
            authorAgentId: message.authorAgentId ?? "", isMine: message.isMine ?? false)
    }

    public var isAgent: Bool { authorKind == "agent" }
    public var isPerson: Bool { !isAgent }

    /// The person's own words, drawn on the right. Their agent's are said as
    /// the agent, under its name, though the server counts them as theirs.
    public var isOwnWords: Bool { isMine && isPerson }

    /// Whether the account that wrote it is gone: it has no id left.
    public var isDeletedAccount: Bool { isPerson && authorUserId.isEmpty && !authorName.isEmpty }

    /// How an agent is named in a chat: "Name · Ada's agent", and just "Ada's
    /// agent" while it still has the default name, which would only repeat
    /// the word. A person is named as they are. `ownedWords` words "Ada's
    /// agent" in the person's language, which is the app's to do.
    public func speakerName(ownedWords: (String) -> String) -> String {
        isAgent ? AgentSpeaker.name(agentName: authorName, ownerName: authorOwnerName, ownedWords: ownedWords) : authorName
    }

    /// Names who wrote it, so that a run of one author's messages can be
    /// drawn under one name.
    public var runKey: String {
        isAgent ? "agent:\(authorAgentId)" : "person:\(authorUserId.isEmpty ? authorName : authorUserId)"
    }
}

extension TranscriptLine {
    /// Whether this is a message the app has sent and the server has not yet
    /// confirmed: it has no stored id to act on.
    public var isBeingSent: Bool { id.hasPrefix(Transcript.sentPrefix) }

    /// How long a pause ends a run of one author's messages: the same
    /// person, much later, is saying something new and is named again.
    public static let runPauseSeconds: TimeInterval = 10 * 60

    /// Whether this message begins a run: the first of its author's in a
    /// row. One after a different author, or a long pause, or on another
    /// day begins one. The day is the one the conversation draws its date
    /// dividers between, so the message under a divider shows the name and
    /// the face again however little time passed across midnight.
    public func beginsRun(after previous: TranscriptLine?, calendar: Calendar = .current) -> Bool {
        guard let author, let previous, let before = previous.author, before.runKey == author.runKey else { return true }
        guard let now = ServerTime.date(at), let then = ServerTime.date(previous.at) else { return false }
        if !calendar.isDate(now, inSameDayAs: then) { return true }
        let gap = now.timeIntervalSince(then)
        return gap < 0 || gap > Self.runPauseSeconds
    }
}

extension Transcript {
    /// The newest message of the chat that is stored, by id: what the
    /// person has read when they are looking at the end of it.
    public var newestChatMessageId: String? {
        lines.last { $0.author != nil && $0.messageId != nil && !$0.id.hasPrefix(Self.sentPrefix) }?.messageId
    }

    /// Which members to draw under which message: each other member under
    /// the newest message by somebody else that they have read, never under
    /// one of their own. In a direct chat that is under the person's own
    /// newest message the other has read. Ids are ULIDs, so they sort by
    /// time and a string comparison finds it. A member who has read nothing
    /// of what is loaded by anybody else is under none.
    public func readers(of members: [ChatMember], ownUserId: String?) -> [String: [ChatMember]] {
        var readers: [String: [ChatMember]] = [:]
        let stored = lines.compactMap { line -> (messageId: String, author: ChatAuthor)? in
            guard let author = line.author, let messageId = line.messageId, !line.id.hasPrefix(Self.sentPrefix) else { return nil }
            return (messageId, author)
        }
        for member in members where member.userId != ownUserId && !member.isRemoved {
            guard let readThrough = member.readThroughMessageId, !readThrough.isEmpty,
                let found = stored.last(where: { message in
                    message.messageId <= readThrough && !(message.author.isPerson && message.author.authorUserId == member.userId)
                })
            else { continue }
            readers[found.messageId, default: []].append(member)
        }
        return readers
    }

    /// The line a message said in a chat is: a person's as the person kind,
    /// an agent's as an answer, either with who said it. A row of the
    /// member's own agent's work in the chat is none: it names no author.
    static func chatLine(of message: AgentMessage) -> TranscriptLine? {
        guard !["tool", "note", "compaction"].contains(message.role), message.isAgentWork != true, let author = ChatAuthor(message) else {
            return nil
        }
        // The call a round made carries no words: it is the agent's own work.
        if !(message.toolCalls ?? []).isEmpty, message.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return nil }
        var line = TranscriptLine(
            id: message.id, kind: author.isPerson ? .person : .answer, text: message.isDeleted == true ? "" : message.content,
            attachments: message.attachments ?? [], at: message.createdAt)
        line.messageId = message.id
        line.author = author
        line.runId = message.agentRunId ?? ""
        line.reactions = message.reactions ?? []
        line.editedAt = message.editedAt
        line.isDeleted = message.isDeleted ?? false
        for reference in message.references ?? [] {
            if reference.isReply {
                line.quotedText = reference.quotedText ?? ""
                line.quotedRole = reference.quotedRole ?? ""
            } else {
                line.references.append(reference)
            }
        }
        return line
    }
}

/// ChatTyping is who is writing in a chat now: told by an event, gone a few
/// seconds after the last one.
public struct ChatTyping: Equatable, Sendable {
    /// How long a notice stays: a little longer than the interval it is
    /// sent at, so the line does not flicker between two.
    public static let shownSeconds: TimeInterval = 5
    /// How often the person's own typing is told to the others at most.
    public static let sentEverySeconds: TimeInterval = 3

    private var untilByUserId: [String: Date] = [:]
    private var nameByUserId: [String: String] = [:]
    private var untilByAgentId: [String: Date] = [:]
    private var nameByAgentId: [String: String] = [:]
    private var ownerNameByAgentId: [String: String] = [:]

    public init() {}

    public mutating func note(userId: String, name: String, at date: Date = Date()) {
        untilByUserId[userId] = date.addingTimeInterval(Self.shownSeconds)
        nameByUserId[userId] = name
    }

    /// An agent working on an answer: it is thinking, where a person is
    /// typing.
    public mutating func note(agentId: String, name: String, ownerName: String = "", at date: Date = Date()) {
        untilByAgentId[agentId] = date.addingTimeInterval(Self.shownSeconds)
        nameByAgentId[agentId] = name
        ownerNameByAgentId[agentId] = ownerName
    }

    /// Someone who said something is no longer typing.
    public mutating func clear(userId: String) {
        untilByUserId[userId] = nil
        nameByUserId[userId] = nil
    }

    /// An agent that has answered is no longer thinking.
    public mutating func clear(agentId: String) {
        untilByAgentId[agentId] = nil
        nameByAgentId[agentId] = nil
        ownerNameByAgentId[agentId] = nil
    }

    /// Forgets those whose notice has run out.
    public mutating func prune(at date: Date = Date()) {
        for (userId, until) in untilByUserId where until <= date {
            untilByUserId[userId] = nil
            nameByUserId[userId] = nil
        }
        for (agentId, until) in untilByAgentId where until <= date {
            untilByAgentId[agentId] = nil
            nameByAgentId[agentId] = nil
            ownerNameByAgentId[agentId] = nil
        }
    }

    /// The names of those still typing at a time, in the order they began.
    public func names(at date: Date = Date()) -> [String] {
        untilByUserId.filter { $0.value > date }.sorted { $0.value < $1.value }.compactMap { nameByUserId[$0.key] }.filter { !$0.isEmpty }
    }

    /// The agents still thinking at a time, in the order they began, named
    /// as a chat names an agent: with whose it is.
    public func agentSpeakers(at date: Date = Date(), ownedWords: (String) -> String) -> [String] {
        untilByAgentId.filter { $0.value > date }.sorted { $0.value < $1.value }.compactMap { entry -> String? in
            guard let name = nameByAgentId[entry.key], !name.isEmpty else { return nil }
            return AgentSpeaker.name(agentName: name, ownerName: ownerNameByAgentId[entry.key] ?? "", ownedWords: ownedWords)
        }
    }

    /// Whether anyone is writing or thinking at a time.
    public func isEmpty(at date: Date = Date()) -> Bool {
        names(at: date).isEmpty && !untilByAgentId.contains { $0.value > date && !(nameByAgentId[$0.key] ?? "").isEmpty }
    }
}

/// AddressableAgent is an agent a message in a chat can be addressed to: the
/// person's own, and the agents of members who let the others ask.
public struct AddressableAgent: Hashable, Sendable, Identifiable {
    public let agentId: String
    public let name: String
    /// The person's own; the others are named by whose they are.
    public let isOwn: Bool
    public let ownerName: String

    public var id: String { agentId }

    public init(agentId: String, name: String, isOwn: Bool, ownerName: String) {
        self.agentId = agentId
        self.name = name
        self.isOwn = isOwn
        self.ownerName = ownerName
    }
}

extension ChatSummary {
    /// The agents an @ offers in this chat: the person's own when it is on,
    /// then the agent of each member who let the others ask it, when that is
    /// on. An agent that is off cannot answer, so none is offered; one
    /// addressed anyway, by replying to its message, is told so by the server.
    public func addressableAgents(ownUserId: String?, deletedAccountNote: String) -> [AddressableAgent] {
        var own: [AddressableAgent] = []
        var others: [AddressableAgent] = []
        for member in members where !member.isRemoved {
            guard let agentId = member.agentId, !agentId.isEmpty, member.isAgentActive == true,
                let name = member.agentName?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty
            else { continue }
            let isOwn = member.userId == ownUserId
            guard isOwn || member.isAgentAskableByOthers == true else { continue }
            let agent = AddressableAgent(
                agentId: agentId, name: name, isOwn: isOwn, ownerName: Self.name(of: member, deletedAccountNote: deletedAccountNote))
            if isOwn { own.append(agent) } else { others.append(agent) }
        }
        return own + others
    }
}

/// ChatMention is the @ in the composer that chooses which agent a message in
/// a chat is for. An agent's name in ordinary prose addresses nothing: only
/// one chosen from the offer, whose name is still in the words, or the agent of
/// a message replied to.
public enum ChatMention {
    /// The letters after the @ at the end of the words, when the words end
    /// in an @ that starts a word; nil otherwise. The caret of a text field
    /// is not known here, so a mention is the one being typed at the end.
    public static func query(in draft: String) -> String? {
        guard let at = draft.lastIndex(of: "@") else { return nil }
        if at != draft.startIndex, !draft[draft.index(before: at)].isWhitespace { return nil }
        let query = draft[draft.index(after: at)...]
        if query.contains(where: \.isWhitespace) { return nil }
        return String(query)
    }

    /// The agents whose name starts with what was typed after the @, in any
    /// case; every agent for a bare @.
    public static func matching(_ agents: [AddressableAgent], query: String) -> [AddressableAgent] {
        let wanted = query.lowercased()
        return agents.filter { $0.name.lowercased().replacingOccurrences(of: " ", with: "").hasPrefix(wanted) }
    }

    /// The words with the @ being typed made the chosen agent's name and a
    /// space.
    public static func choosing(_ agent: AddressableAgent, in draft: String) -> String {
        guard let at = draft.lastIndex(of: "@") else { return draft }
        return String(draft[..<at]) + "@\(agent.name) "
    }

    /// The agents a message is for: those chosen whose names are still in the
    /// words, so that taking the name out takes the agent out, and the agent
    /// of a message replied to, each once.
    public static func addressed(in text: String, chosen: [AddressableAgent], repliedToAgentId: String? = nil) -> [String] {
        var ids: [String] = []
        for agent in chosen {
            let mention = "@\(agent.name)"
            guard let found = text.range(of: mention) else { continue }
            if let next = text[found.upperBound...].first, next.isLetter || next.isNumber || next == "_" { continue }
            if !ids.contains(agent.agentId) { ids.append(agent.agentId) }
        }
        if let repliedToAgentId, !repliedToAgentId.isEmpty, !ids.contains(repliedToAgentId) { ids.append(repliedToAgentId) }
        return ids
    }
}

/// ChatPerson is somebody a chat can be started with or added from: a
/// person the caller shares a circle with.
public struct ChatPerson: Codable, Hashable, Sendable, Identifiable {
    public let userId: String
    public let username: String
    public let name: String?
    public let avatarUrl: String?
    public let avatarColorIndex: Int?

    public var id: String { userId }

    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    public init(userId: String, username: String, name: String? = nil, avatarUrl: String? = nil, avatarColorIndex: Int? = nil) {
        self.userId = userId
        self.username = username
        self.name = name
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
    }
}

/// GroupChatDraft is a new group's name and people as the form holds them.
public struct GroupChatDraft: Hashable, Sendable {
    public var title = ""
    public var userIds: Set<String> = []

    public init() {}

    public var trimmedTitle: String { title.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// Whether the form can be sent: a group needs a name and someone in it.
    public var canSubmit: Bool { !trimmedTitle.isEmpty && !userIds.isEmpty }
}

extension Documents {
    /// What every read of a chat answers with. Written out in full, since the
    /// server's test of the app's documents fills in one level of shared
    /// fields.
    static let chatFields = """
        chatId chatKind circleId title isCircleChat unreadCount isMuted mutedUntil isRemoved
        members { userId username name avatarUrl avatarColorIndex memberRole isDeletedAccount isRemoved readThroughMessageId isAgentReading isAgentAskableByOthers agentId agentName isAgentActive }
        """
    /// What every read of a message answers with, in every conversation.
    static let messageFields = """
        id createdAt role content name toolCallId
        attachments { id name contentType size isRemoved }
        references { itemId threadId subject from path name recordingId recordingTitle photoId photoTitle financeTransactionId postedOn amount currencyCode merchantName description agentMessageId quotedRole quotedText }
        reactions { emoji reactorKind reactedAt reactorUserId reactorName }
        authorKind authorUserId authorName authorOwnerName authorAvatarUrl authorAvatarColorIndex authorAgentId agentRunId isMine isAgentWork editedAt isDeleted
        """
    public static let chatPeople = """
        query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }
        """
    public static let openDirectChat = """
        mutation ($userId: String!) { OpenDirectChat(userId: $userId) { id } }
        """
    public static let createGroupChat = """
        mutation ($title: String, $userIds: [String!]!) { CreateGroupChat(title: $title, userIds: $userIds) { id } }
        """
    public static let renameChat = """
        mutation ($conversationId: String!, $title: String!) { RenameChat(conversationId: $conversationId, title: $title) { id } }
        """
    public static let addChatMembers = """
        mutation ($conversationId: String!, $userIds: [String!]!) { AddChatMembers(conversationId: $conversationId, userIds: $userIds) { id } }
        """
    public static let removeChatMember = """
        mutation ($conversationId: String!, $userId: String!) { RemoveChatMember(conversationId: $conversationId, userId: $userId) { id } }
        """
    public static let leaveChat = """
        mutation ($conversationId: String!) { LeaveChat(conversationId: $conversationId) }
        """
    public static let editChatMessage = """
        mutation ($messageId: String!, $message: String!) { EditChatMessage(messageId: $messageId, message: $message) { id } }
        """
    public static let deleteChatMessage = """
        mutation ($messageId: String!) { DeleteChatMessage(messageId: $messageId) }
        """
    public static let markChatRead = """
        mutation ($conversationId: String!, $throughMessageId: String!) {
          MarkChatRead(conversationId: $conversationId, throughMessageId: $throughMessageId)
        }
        """
    public static let muteChat = """
        mutation ($conversationId: String!, $mutedUntil: DateTime) { MuteChat(conversationId: $conversationId, mutedUntil: $mutedUntil) { id } }
        """
    public static let setChatTyping = """
        mutation ($conversationId: String!) { SetChatTyping(conversationId: $conversationId) }
        """
    public static let setChatAgentReading = """
        mutation ($conversationId: String!, $isAgentReading: Boolean!) {
          SetChatAgentReading(conversationId: $conversationId, isAgentReading: $isAgentReading) { id }
        }
        """
    public static let setChatAgentAskableByOthers = """
        mutation ($conversationId: String!, $isAgentAskableByOthers: Boolean!) {
          SetChatAgentAskableByOthers(conversationId: $conversationId, isAgentAskableByOthers: $isAgentAskableByOthers) { id }
        }
        """
}

extension Connection {
    /// Who the token signs in as, by id: which of a chat's members is the
    /// person.
    public func userId() async throws -> String? {
        try await accessSession().userId
    }

    /// What the person's agent is called, and whether they have turned it on.
    /// A chat needs neither.
    public func agentIdentity() async throws -> (name: String, isOn: Bool) {
        struct Answer: Decodable, Sendable {
            struct Read: Decodable, Sendable {
                struct Agent: Decodable, Sendable {
                    let name: String?
                    let enabled: Bool?
                }
                let agent: Agent?
            }
            let ReadAgent: Read
        }
        let agent = try await execute(Documents.agent, as: Answer.self).ReadAgent.agent
        return (agent?.name ?? "", agent?.enabled ?? false)
    }

    /// Everybody the person shares a circle with: the people a chat can be
    /// started with.
    public func chatPeople() async throws -> [ChatPerson] {
        struct Answer: Decodable, Sendable { let ListCirclePeople: [ChatPerson] }
        return try await execute(Documents.chatPeople, as: Answer.self).ListCirclePeople
    }

    /// The person's direct chat with somebody, made when there is none and
    /// the same one every time: the id of the person's conversation for it.
    public func openDirectChat(with userId: String) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Opened: Decodable, Sendable { let id: String }
            let OpenDirectChat: Opened
        }
        return try await execute(Documents.openDirectChat, variables: ["userId": .string(userId)], as: Answer.self).OpenDirectChat.id
    }

    /// Makes a group chat with the person as its admin and these people in
    /// it, and answers the person's conversation for it.
    public func createGroupChat(_ draft: GroupChatDraft) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Created: Decodable, Sendable { let id: String }
            let CreateGroupChat: Created
        }
        let variables: [String: JSONValue] = [
            "title": .string(draft.trimmedTitle), "userIds": .array(draft.userIds.sorted().map { .string($0) }),
        ]
        return try await execute(Documents.createGroupChat, variables: variables, as: Answer.self).CreateGroupChat.id
    }

    /// Renames a group chat. Its admins only.
    public func renameChat(_ conversationId: String, title: String) async throws {
        struct Answer: Decodable, Sendable { let RenameChat: JSONValue? }
        _ = try await execute(
            Documents.renameChat,
            variables: ["conversationId": .string(conversationId), "title": .string(title.trimmingCharacters(in: .whitespacesAndNewlines))],
            as: Answer.self)
    }

    /// Puts people in a group chat. Its admins only.
    public func addChatMembers(_ conversationId: String, userIds: [String]) async throws {
        struct Answer: Decodable, Sendable { let AddChatMembers: JSONValue? }
        _ = try await execute(
            Documents.addChatMembers,
            variables: ["conversationId": .string(conversationId), "userIds": .array(userIds.map { .string($0) })], as: Answer.self)
    }

    /// Takes somebody out of a group chat. Its admins only.
    public func removeChatMember(_ conversationId: String, userId: String) async throws {
        struct Answer: Decodable, Sendable { let RemoveChatMember: JSONValue? }
        _ = try await execute(
            Documents.removeChatMember, variables: ["conversationId": .string(conversationId), "userId": .string(userId)], as: Answer.self)
    }

    /// Leaves a group chat. It stays readable up to the time of leaving.
    public func leaveChat(_ conversationId: String) async throws {
        struct Answer: Decodable, Sendable { let LeaveChat: JSONValue? }
        _ = try await execute(Documents.leaveChat, variables: ["conversationId": .string(conversationId)], as: Answer.self)
    }

    /// Changes what a message of the person's says.
    public func editChatMessage(_ messageId: String, message: String) async throws {
        struct Answer: Decodable, Sendable { let EditChatMessage: JSONValue? }
        _ = try await execute(
            Documents.editChatMessage, variables: ["messageId": .string(messageId), "message": .string(message)], as: Answer.self)
    }

    /// Takes a message back: the person's own, or any in a group chat they
    /// are an admin of.
    public func deleteChatMessage(_ messageId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteChatMessage: JSONValue? }
        _ = try await execute(Documents.deleteChatMessage, variables: ["messageId": .string(messageId)], as: Answer.self)
    }

    /// Says the person has read a chat through a message. The mark moves
    /// forward and never back.
    public func markChatRead(_ conversationId: String, through messageId: String) async throws {
        struct Answer: Decodable, Sendable { let MarkChatRead: JSONValue? }
        _ = try await execute(
            Documents.markChatRead, variables: ["conversationId": .string(conversationId), "throughMessageId": .string(messageId)],
            as: Answer.self)
    }

    /// Silences a chat's notifications until a time, or with none stops
    /// silencing them.
    public func muteChat(_ conversationId: String, until: Date?) async throws {
        struct Answer: Decodable, Sendable { let MuteChat: JSONValue? }
        _ = try await execute(
            Documents.muteChat,
            variables: [
                "conversationId": .string(conversationId), "mutedUntil": until.map { .string($0.formatted(.iso8601)) } ?? .null,
            ], as: Answer.self)
    }

    /// Tells the others the person is writing. Nothing is stored.
    public func setChatTyping(_ conversationId: String) async throws {
        struct Answer: Decodable, Sendable { let SetChatTyping: JSONValue? }
        _ = try await execute(Documents.setChatTyping, variables: ["conversationId": .string(conversationId)], as: Answer.self)
    }

    /// Makes the person's agent remember a chat from now on, or stop. What it
    /// learned stays.
    public func setChatAgentReading(_ conversationId: String, isReading: Bool) async throws {
        struct Answer: Decodable, Sendable { let SetChatAgentReading: JSONValue? }
        _ = try await execute(
            Documents.setChatAgentReading, variables: ["conversationId": .string(conversationId), "isAgentReading": .bool(isReading)],
            as: Answer.self)
    }

    /// Lets the others in a chat address the person's agent there, or stops
    /// them.
    public func setChatAgentAskableByOthers(_ conversationId: String, isAskable: Bool) async throws {
        struct Answer: Decodable, Sendable { let SetChatAgentAskableByOthers: JSONValue? }
        _ = try await execute(
            Documents.setChatAgentAskableByOthers,
            variables: ["conversationId": .string(conversationId), "isAgentAskableByOthers": .bool(isAskable)], as: Answer.self)
    }
}
