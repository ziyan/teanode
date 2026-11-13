package com.teanode.kit.chats

import kotlinx.serialization.Serializable
import java.time.Instant
import java.time.ZoneId

// Chats: conversations with people in them. A chat is listed with the other conversations, read and followed the way
// they are, and written to with the same composer; this file holds what only a chat needs: who is in it, who wrote a
// message, and the small marks (who has read, who is typing) that a conversation with an agent never had. The words a
// screen shows are the app's, since this package has no localization.

/** ChatKind is how many people a chat is for. */
enum class ChatKind(val wireName: String) {
    Direct("direct"),
    Group("group"),
}

/** ChatMember is one person in a chat, as the list of conversations says. */
@Serializable
data class ChatMember(
    /** Empty once the account is deleted. */
    val userId: String = "",
    val username: String = "",
    /** What they were called when last seen, which a deleted account keeps. */
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
    /** admin or member. */
    val memberRole: String = "member",
    /** The account is gone and the name is all that is left. */
    val isDeletedAccount: Boolean = false,
    /** They are not in the chat any more; they read it up to when they left. */
    val isRemoved: Boolean = false,
    /** The newest message they have read; empty when they have read none. */
    val readThroughMessageId: String? = null,
    /** Their agent remembers this chat. */
    val isAgentReading: Boolean = false,
    /** The others in the chat may address their agent there. */
    val isAgentAskableByOthers: Boolean? = null,
    /** Which agent is theirs, what it is called and whether it is on; told for the person's own and for an agent its owner let the others ask. */
    val agentId: String? = null,
    val agentName: String? = null,
    val isAgentActive: Boolean? = null,
) {
    /** A deleted account has no id, so its name stands in. */
    val identity: String get() = userId.ifEmpty { "deleted-$username-${name.orEmpty()}" }

    val isAdmin: Boolean get() = memberRole == "admin"

    /** What a list of people calls them: their name, or the username when they have none. */
    val displayName: String get() = if (name.isNullOrEmpty()) username else name
}

/** ChatSummary is what a conversation of the kind chat carries beside the fields every conversation has. */
@Serializable
data class ChatSummary(
    val chatId: String,
    /** direct or group. */
    val chatKind: String,
    val circleId: String? = null,
    /** The group's name; for a direct chat, the other person's. */
    val title: String? = null,
    val isCircleChat: Boolean = false,
    /** The people in it, then those who left. */
    val members: List<ChatMember> = emptyList(),
    /** How many messages by others are newer than the person's read mark. */
    val unreadCount: Int = 0,
    /** What the person's phone is told of here, as the server names it; see `level`. */
    val notificationLevel: String? = null,
    /** The person is no longer in it; they read it up to when they left. */
    val isRemoved: Boolean = false,
) {
    val isGroup: Boolean get() = chatKind == ChatKind.Group.wireName

    /** What the person's phone is told of here; all when the server named none it knows. */
    val level: ChatNotificationLevel get() = ChatNotificationLevel.entries.firstOrNull { entry -> entry.wireName == notificationLevel } ?: ChatNotificationLevel.All

    fun withLevel(newLevel: ChatNotificationLevel): ChatSummary = copy(notificationLevel = newLevel.wireName)

    /**
     * The people to show a chat by: everybody but the person, and but those who left a group. A direct chat keeps
     * showing the other person after they left a circle: it is who the chat is with.
     */
    fun otherMembers(ownUserId: String?): List<ChatMember> =
        members.filter { member -> member.userId != ownUserId && (!isGroup || !member.isRemoved) }

    /** The people in it now, the person among them. */
    val presentMembers: List<ChatMember> get() = members.filter { member -> !member.isRemoved }

    /** Whether the person is an admin of it. */
    fun isAdmin(ownUserId: String?): Boolean = members.any { member -> member.userId == ownUserId && member.isAdmin }

    /** Whether the person can add people to it and take them out: a group that is not a circle's and that they are an admin of. */
    fun canChangeMembers(ownUserId: String?): Boolean = isGroup && !isCircleChat && !isRemoved && isAdmin(ownUserId)

    /** Whether the person can leave it from here: a group that is not a circle's. */
    val canBeLeft: Boolean get() = isGroup && !isCircleChat && !isRemoved

    /** Whether it can be renamed: the same. */
    val canBeRenamed: Boolean get() = isGroup && !isCircleChat && !isRemoved

    /** The member with this id, if there is one. */
    fun member(userId: String): ChatMember? = members.firstOrNull { member -> member.userId.isNotEmpty() && member.userId == userId }

    /** A member's read mark moved forward; it never goes back. */
    fun withRead(userId: String, messageId: String): ChatSummary = copy(
        members = members.map { member ->
            if (member.userId == userId && (member.readThroughMessageId ?: "") < messageId) member.copy(readThroughMessageId = messageId) else member
        },
    )

    /** A member's two agent switches set as the person just chose them, before the server has said yes; a null one is left as it is. */
    fun withAgentSwitches(userId: String, isAgentReading: Boolean? = null, isAgentAskableByOthers: Boolean? = null): ChatSummary = copy(
        members = members.map { member ->
            if (member.userId != userId) {
                member
            } else {
                member.copy(
                    isAgentReading = isAgentReading ?: member.isAgentReading,
                    isAgentAskableByOthers = isAgentAskableByOthers ?: member.isAgentAskableByOthers,
                )
            }
        },
    )

    /**
     * A chat's name: its title, else the other people's names, which is all a direct chat is. `deletedAccountNote`
     * follows the name of a person whose account is gone.
     */
    fun name(ownUserId: String?, deletedAccountNote: String, fallback: String): String {
        val trimmedTitle = title?.trim().orEmpty()
        if (trimmedTitle.isNotEmpty()) return trimmedTitle
        val names = otherMembers(ownUserId).map { member -> nameOf(member, deletedAccountNote) }
        return if (names.isEmpty()) fallback else names.joinToString(", ")
    }

    companion object {
        /** A person's name, with the note when the account is gone. */
        fun nameOf(member: ChatMember, deletedAccountNote: String): String =
            if (member.isDeletedAccount) "${member.displayName} $deletedAccountNote" else member.displayName
    }
}

/**
 * ChatUnread is how many messages wait across the chats the person is told all of: the number on the entry to the
 * conversations. A chat told only of mentions, or of nothing, shows its own count in the list and does not ask for
 * attention from outside.
 */
object ChatUnread {
    fun total(conversations: List<AgentConversation>): Int = conversations.sumOf { conversation ->
        val chat = conversation.chat
        if (chat != null && chat.level == ChatNotificationLevel.All) chat.unreadCount else 0
    }
}

/**
 * AgentSpeaker is how an agent is named wherever a chat shows it: above its messages and in the line that says it is
 * thinking. Most agents keep the name "Agent", so two people's agents in one chat are told apart by whose they are.
 */
object AgentSpeaker {
    /** What an agent is called until its person names it. */
    const val DEFAULT_NAME = "Agent"

    /** "Name · Ada's agent", and "Ada's agent" alone for the default name. An agent whose owner is not known is called by its own name. */
    fun name(agentName: String, ownerName: String, ownedWords: (String) -> String): String {
        val trimmedName = agentName.trim()
        val trimmedOwner = ownerName.trim()
        if (trimmedOwner.isEmpty()) return trimmedName
        val owned = ownedWords(trimmedOwner)
        return if (trimmedName.isEmpty() || trimmedName == DEFAULT_NAME) owned else "$trimmedName · $owned"
    }
}

/** ChatAuthor is who wrote a message of a chat. */
data class ChatAuthor(
    /** person or agent. */
    val authorKind: String,
    /** Empty once the account is deleted. */
    val authorUserId: String = "",
    val authorName: String,
    /** On an agent's message, its owner's name: most agents keep the name "Agent", and whose it is tells two apart. */
    val authorOwnerName: String = "",
    val authorAvatarUrl: String? = null,
    val authorAvatarColorIndex: Int? = null,
    val authorAgentId: String = "",
    /** Written by the person reading it or by their agent. */
    val isMine: Boolean = false,
) {
    val isAgent: Boolean get() = authorKind == "agent"
    val isPerson: Boolean get() = !isAgent

    /** The person's own words, drawn on the right. Their agent's are said as the agent, under its name, though the server counts them as theirs. */
    val isOwnWords: Boolean get() = isMine && isPerson

    /** Whether the account that wrote it is gone: it has no id left. */
    val isDeletedAccount: Boolean get() = isPerson && authorUserId.isEmpty() && authorName.isNotEmpty()

    /** How an agent is named in a chat: "Name · Ada's agent", and just "Ada's agent" while it still has the default name. A person is named as they are. */
    fun speakerName(ownedWords: (String) -> String): String =
        if (isAgent) AgentSpeaker.name(authorName, authorOwnerName, ownedWords) else authorName

    /** Names who wrote it, so that a run of one author's messages can be drawn under one name. */
    val runKey: String get() = if (isAgent) "agent:$authorAgentId" else "person:${authorUserId.ifEmpty { authorName }}"

    companion object {
        /** The author of a stored message; null for one that says none, which is a row of the person's own agent's work. */
        fun of(message: AgentMessage): ChatAuthor? {
            val authorKind = message.authorKind
            if (authorKind.isNullOrEmpty()) return null
            return ChatAuthor(
                authorKind = authorKind, authorUserId = message.authorUserId.orEmpty(), authorName = message.authorName.orEmpty(),
                authorOwnerName = message.authorOwnerName.orEmpty(), authorAvatarUrl = message.authorAvatarUrl,
                authorAvatarColorIndex = message.authorAvatarColorIndex, authorAgentId = message.authorAgentId.orEmpty(),
                isMine = message.isMine ?: false,
            )
        }
    }
}

/** How long a pause ends a run of one author's messages: the same person, much later, is saying something new and is named again. */
const val RUN_PAUSE_SECONDS: Long = 10 * 60

/**
 * Whether this message begins a run: the first of its author's in a row. One after a different author, or a long
 * pause, or on another day begins one. The day is the one the conversation draws its date dividers between, so the
 * message under a divider shows the name and the picture again however little time passed across midnight. A call's
 * line is drawn with no author, so the message under it begins one.
 */
fun TranscriptLine.beginsRun(previous: TranscriptLine?, zone: ZoneId = ZoneId.systemDefault()): Boolean {
    val currentAuthor = author ?: return true
    if (previous == null || previous.isCallLine) return true
    val earlierAuthor = previous.author ?: return true
    if (earlierAuthor.runKey != currentAuthor.runKey) return true
    val now = ServerTime.instant(at) ?: return false
    val then = ServerTime.instant(previous.at) ?: return false
    if (now.atZone(zone).toLocalDate() != then.atZone(zone).toLocalDate()) return true
    val gapSeconds = now.epochSecond - then.epochSecond
    return gapSeconds < 0 || gapSeconds > RUN_PAUSE_SECONDS
}

/**
 * The newest message of the chat that is stored, by id: what the person has read when they are looking at the end of
 * it.
 */
val Transcript.newestChatMessageId: String?
    get() = lines.lastOrNull { line -> line.author != null && line.messageId != null && !line.id.startsWith(Transcript.SENT_PREFIX) }?.messageId

/**
 * Which members to draw under which message: each other member under the newest message by somebody else that they
 * have read, never under one of their own. In a direct chat that is under the person's own newest message the other
 * has read. Ids are ULIDs, so they sort by time and a string comparison finds it.
 */
fun Transcript.readers(members: List<ChatMember>, ownUserId: String?): Map<String, List<ChatMember>> {
    val readers = linkedMapOf<String, MutableList<ChatMember>>()
    val stored = lines.mapNotNull { line ->
        val author = line.author
        val messageId = line.messageId
        if (author == null || messageId == null || line.id.startsWith(Transcript.SENT_PREFIX)) null else messageId to author
    }
    for (member in members) {
        if (member.userId == ownUserId || member.isRemoved) continue
        val readThrough = member.readThroughMessageId
        if (readThrough.isNullOrEmpty()) continue
        val found = stored.lastOrNull { (messageId, author) ->
            messageId <= readThrough && !(author.isPerson && author.authorUserId == member.userId)
        } ?: continue
        readers.getOrPut(found.first) { mutableListOf() }.add(member)
    }
    return readers
}

/** ChatTyping is who is writing in a chat now: told by an event, gone a few seconds after the last one. */
class ChatTyping {
    private val untilByUserId = linkedMapOf<String, Long>()
    private val nameByUserId = mutableMapOf<String, String>()
    private val untilByAgentId = linkedMapOf<String, Long>()
    private val nameByAgentId = mutableMapOf<String, String>()
    private val ownerNameByAgentId = mutableMapOf<String, String>()

    fun note(userId: String, name: String, nowMS: Long = System.currentTimeMillis()) {
        untilByUserId[userId] = nowMS + SHOWN_MS
        nameByUserId[userId] = name
    }

    /** An agent working on an answer: it is thinking, where a person is typing. */
    fun noteAgent(agentId: String, name: String, ownerName: String = "", nowMS: Long = System.currentTimeMillis()) {
        untilByAgentId[agentId] = nowMS + SHOWN_MS
        nameByAgentId[agentId] = name
        ownerNameByAgentId[agentId] = ownerName
    }

    /** Someone who said something is no longer typing. */
    fun clear(userId: String) {
        untilByUserId.remove(userId)
        nameByUserId.remove(userId)
    }

    /** An agent that has answered is no longer thinking. */
    fun clearAgent(agentId: String) {
        untilByAgentId.remove(agentId)
        nameByAgentId.remove(agentId)
        ownerNameByAgentId.remove(agentId)
    }

    /** Forgets those whose notice has run out. */
    fun prune(nowMS: Long = System.currentTimeMillis()) {
        for (userId in untilByUserId.filterValues { until -> until <= nowMS }.keys) clear(userId)
        for (agentId in untilByAgentId.filterValues { until -> until <= nowMS }.keys) clearAgent(agentId)
    }

    /** The names of those still typing at a time, in the order they began. */
    fun names(nowMS: Long = System.currentTimeMillis()): List<String> =
        untilByUserId.filterValues { until -> until > nowMS }.entries.sortedBy { entry -> entry.value }
            .mapNotNull { entry -> nameByUserId[entry.key] }.filter { name -> name.isNotEmpty() }

    /** The agents still thinking at a time, in the order they began, named as a chat names an agent: with whose it is. */
    fun agentSpeakers(nowMS: Long = System.currentTimeMillis(), ownedWords: (String) -> String): List<String> =
        untilByAgentId.filterValues { until -> until > nowMS }.entries.sortedBy { entry -> entry.value }.mapNotNull { entry ->
            val name = nameByAgentId[entry.key]
            if (name.isNullOrEmpty()) null else AgentSpeaker.name(name, ownerNameByAgentId[entry.key].orEmpty(), ownedWords)
        }

    /** Whether anyone is writing or thinking at a time. */
    fun isEmpty(nowMS: Long = System.currentTimeMillis()): Boolean =
        names(nowMS).isEmpty() && untilByAgentId.none { entry -> entry.value > nowMS && !nameByAgentId[entry.key].isNullOrEmpty() }

    companion object {
        /** How long a notice stays: a little longer than the interval it is sent at, so the line does not flicker between two. */
        const val SHOWN_MS: Long = 5_000

        /** How often the person's own typing is told to the others at most. */
        const val SENT_EVERY_MS: Long = 3_000
    }
}

/**
 * ChatNotificationLevel is what a member's phone is told of in a chat: every message, only one that mentions them (by
 * name, or everybody), or nothing. It stays until they change it.
 */
enum class ChatNotificationLevel(val wireName: String) {
    All("all"),
    Mentions("mentions"),
    Off("off"),
}

/** AddressableAgent is an agent a message in a chat can be addressed to: the person's own, and the agents of members who let the others ask. */
data class AddressableAgent(
    val agentId: String,
    val name: String,
    /** The person's own; the others are named by whose they are. */
    val isOwn: Boolean,
    val ownerName: String,
)

/**
 * The agents an @ offers in this chat: the person's own when it is on, then the agent of each member who let the others
 * ask it, when that is on. An agent that is off cannot answer, so none is offered.
 */
fun ChatSummary.addressableAgents(ownUserId: String?, deletedAccountNote: String): List<AddressableAgent> {
    val own = mutableListOf<AddressableAgent>()
    val others = mutableListOf<AddressableAgent>()
    for (member in members) {
        if (member.isRemoved) continue
        val agentId = member.agentId
        if (agentId.isNullOrEmpty() || member.isAgentActive != true) continue
        val name = member.agentName?.trim().orEmpty()
        if (name.isEmpty()) continue
        val isOwn = member.userId == ownUserId
        if (!isOwn && member.isAgentAskableByOthers != true) continue
        val agent = AddressableAgent(agentId, name, isOwn, ChatSummary.nameOf(member, deletedAccountNote))
        if (isOwn) own.add(agent) else others.add(agent)
    }
    return own + others
}

/**
 * MentionChoice is one line an @ in a chat offers: a person, which mentions them (their phone is told even when they
 * hear only of mentions) and asks nothing of their agent; an agent, which asks it to answer; or everybody.
 */
sealed interface MentionChoice {
    val identity: String

    data class Person(val userId: String, val name: String) : MentionChoice {
        override val identity: String get() = "person:$userId"
    }

    data class Agent(val agent: AddressableAgent) : MentionChoice {
        override val identity: String get() = "agent:${agent.agentId}"
    }

    data object Everybody : MentionChoice {
        override val identity: String get() = "all"
    }
}

/**
 * What an @ offers in this chat: the person's own agent first, then everybody (in a group), then each of the others
 * followed by their agent when they let the chat ask it. A person and their agent are two lines, named apart.
 */
fun ChatSummary.mentionChoices(ownUserId: String?, deletedAccountNote: String): List<MentionChoice> {
    val agents = addressableAgents(ownUserId, deletedAccountNote)
    val choices = mutableListOf<MentionChoice>()
    agents.filter { agent -> agent.isOwn }.forEach { agent -> choices.add(MentionChoice.Agent(agent)) }
    if (isGroup) choices.add(MentionChoice.Everybody)
    for (member in members) {
        if (member.isRemoved || member.isDeletedAccount || member.userId.isEmpty() || member.userId == ownUserId) continue
        val name = ChatSummary.nameOf(member, deletedAccountNote).trim()
        if (name.isNotEmpty()) choices.add(MentionChoice.Person(member.userId, name))
        val agent = agents.firstOrNull { candidate -> !candidate.isOwn && candidate.agentId == member.agentId }
        if (agent != null) choices.add(MentionChoice.Agent(agent))
    }
    return choices
}

/**
 * ChatMention is the @ in the composer. Choosing an agent from what it offers asks that agent; an agent's name in
 * ordinary prose asks nothing: only one chosen whose name is still in the words, or the agent of a message replied to.
 * Mentioning people is read from the words: an @ and the name of somebody in the chat, or the word for everybody.
 */
object ChatMention {
    /** The words after an @ that mention everybody, in each language the apps are shown in, as the server's models.ChatAllMentionWords. */
    val allWords = listOf("all", "所有人", "全員")

    /**
     * The letters after the @ at the end of the words, when the words end in an @ that starts a word; null otherwise.
     * The caret of a text field is not known here, so a mention is the one being typed at the end.
     */
    fun query(draft: String): String? {
        val at = draft.lastIndexOf('@')
        if (at < 0) return null
        if (at > 0 && !draft[at - 1].isWhitespace()) return null
        val query = draft.substring(at + 1)
        if (query.any { character -> character.isWhitespace() }) return null
        return query
    }

    /** What follows the @ for a choice. Everybody is the word of the language the app is shown in. */
    fun word(choice: MentionChoice, allWord: String): String = when (choice) {
        is MentionChoice.Person -> choice.name
        is MentionChoice.Agent -> choice.agent.name
        MentionChoice.Everybody -> allWord
    }

    /** The choices whose word starts with what was typed after the @, in any case and with no spaces; every choice for a bare @. */
    fun matching(choices: List<MentionChoice>, query: String, allWord: String): List<MentionChoice> {
        val wanted = query.lowercase()
        fun fits(word: String): Boolean = word.lowercase().replace(" ", "").startsWith(wanted)
        return choices.filter { choice -> (choice == MentionChoice.Everybody && fits("all")) || fits(word(choice, allWord)) }
    }

    /** The words with the @ being typed made the chosen word and a space. */
    fun choosing(choice: MentionChoice, draft: String, allWord: String): String {
        val at = draft.lastIndexOf('@')
        if (at < 0) return draft
        return draft.substring(0, at) + "@${word(choice, allWord)} "
    }

    /**
     * The agents a message is for: those chosen whose names are still in the words, so that taking the name out takes
     * the agent out, and the agent of a message replied to, each once.
     */
    fun addressed(text: String, chosen: List<AddressableAgent>, repliedToAgentId: String? = null): List<String> {
        val agentIds = mutableListOf<String>()
        for (agent in chosen) {
            if (hasMention(agent.name, text) && agent.agentId !in agentIds) agentIds.add(agent.agentId)
        }
        if (!repliedToAgentId.isNullOrEmpty() && repliedToAgentId !in agentIds) agentIds.add(repliedToAgentId)
        return agentIds
    }

    /** Who the words mention when they are sent. */
    data class Mentioned(val userIds: List<String>, val isAllMentioned: Boolean)

    /**
     * Who the words mention when they are sent: each other person in the chat whose name follows an @, typed or
     * chosen, and everybody when an @ is followed by the word for it in any language. A name that is the name of an
     * agent chosen from the @ is that agent, asked, and mentions nobody.
     */
    fun mentioned(text: String, chat: ChatSummary, ownUserId: String?, deletedAccountNote: String, chosenAgents: List<AddressableAgent>): Mentioned {
        val askedNames = chosenAgents.filter { agent -> hasMention(agent.name, text) }.map { agent -> agent.name.lowercase() }.toSet()
        val userIds = mutableListOf<String>()
        for (choice in chat.mentionChoices(ownUserId, deletedAccountNote)) {
            if (choice !is MentionChoice.Person) continue
            if (choice.name.lowercase() !in askedNames && hasMention(choice.name, text) && choice.userId !in userIds) userIds.add(choice.userId)
        }
        return Mentioned(userIds, allWords.any { word -> hasMention(word, text) })
    }

    /** Whether the words hold an @ and then the word, at the start of a mention (not inside an address) and not the start of a longer word. */
    fun hasMention(word: String, text: String): Boolean = ranges(listOf(word), text).isNotEmpty()

    /**
     * Where the words hold an @ and one of the given words, earliest first, the longer of two that fit at one place:
     * what a message's mentions are marked over.
     */
    fun ranges(words: List<String>, text: String): List<IntRange> {
        val candidates = words.map { word -> word.trim() }.filter { word -> word.isNotEmpty() }.sortedByDescending { word -> word.length }
        val found = mutableListOf<IntRange>()
        var from = 0
        while (true) {
            val sign = text.indexOf('@', from)
            if (sign < 0) break
            from = sign + 1
            if (sign > 0) {
                val before = text[sign - 1]
                val isAscii = before.code < 128
                if (isAscii && (before.isLetter() || before.isDigit() || before in "_.+-@")) continue
            }
            for (word in candidates) {
                if (!text.regionMatches(sign + 1, word, 0, word.length, ignoreCase = true)) continue
                val end = sign + 1 + word.length
                if (end < text.length && (text[end].isLetter() || text[end].isDigit() || text[end] == '_')) continue
                found.add(sign until end)
                break
            }
        }
        return found
    }
}

/** ChatMentionWord is a word a message mentions somebody by, after its @: a member's name, or a word for everybody. `isReader` says it is the person reading, whose mention is drawn stronger. */
data class ChatMentionWord(val word: String, val isReader: Boolean)

/**
 * The words this message of a chat mentions people by, for marking: the names of the members it mentions, and the
 * words for everybody when it mentions everybody, which include the reader.
 */
fun TranscriptLine.mentionWords(chat: ChatSummary?, ownUserId: String?, deletedAccountNote: String): List<ChatMentionWord> {
    val words = mutableListOf<ChatMentionWord>()
    for (userId in mentionedUserIds) {
        val member = chat?.member(userId) ?: continue
        words.add(ChatMentionWord(ChatSummary.nameOf(member, deletedAccountNote), userId == ownUserId))
    }
    if (isAllMentioned) words.addAll(ChatMention.allWords.map { word -> ChatMentionWord(word, true) })
    return words
}

/** ChatPerson is somebody a chat can be started with or added from: a person the caller shares a circle with. */
@Serializable
data class ChatPerson(
    val userId: String,
    val username: String = "",
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
) {
    val displayName: String get() = if (name.isNullOrEmpty()) username else name
}

/** GroupChatDraft is a new group's name and people as the form holds them. */
data class GroupChatDraft(val title: String = "", val userIds: Set<String> = emptySet()) {
    val trimmedTitle: String get() = title.trim()

    /** Whether the form can be sent: a group needs a name and someone in it. */
    val canSubmit: Boolean get() = trimmedTitle.isNotEmpty() && userIds.isNotEmpty()
}

/** Marks an instant as the time the server writes, for the few places that send one. */
fun Instant.asServerTime(): String = toString()
