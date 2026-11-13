package com.teanode.app.chats

import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import com.teanode.app.R
import com.teanode.app.session.AppSession
import com.teanode.kit.AppSection
import com.teanode.kit.Connection
import com.teanode.kit.accessSession
import com.teanode.kit.chats.AgentConversation
import com.teanode.kit.chats.ChatUnread
import com.teanode.kit.chats.LinkPreview
import com.teanode.kit.chats.LinkPreviewLoader
import com.teanode.kit.chats.agentIdentity
import com.teanode.kit.chats.conversations
import com.teanode.kit.chats.linkPreviews
import java.util.UUID

/**
 * ChatWords are the words a chat is described in, in the person's language: the parts of a sentence the kit leaves to
 * the app because it has no localization.
 */
class ChatWords(private val context: Context) {
    val deletedAccountNote: String get() = context.getString(R.string.chats_deleted_account_note)

    /** The word after an @ that mentions everybody in a chat, in the language the app is shown in. */
    val allMentionWord: String get() = context.getString(R.string.chats_all_mention_word)

    val chatFallbackTitle: String get() = context.getString(R.string.chats_chat_title)

    val newConversationTitle: String get() = context.getString(R.string.chats_new_conversation)

    /** "Ada's agent", in the person's language. */
    fun agentOf(ownerName: String): String = context.getString(R.string.chats_agent_of, ownerName)

    /** Who is typing, in a line. */
    fun typing(names: List<String>): String = when (names.size) {
        0 -> ""
        1 -> context.getString(R.string.chats_typing_one, names[0])
        2 -> context.getString(R.string.chats_typing_two, names[0], names[1])
        else -> context.getString(R.string.chats_typing_several)
    }

    /** Which agents are working on an answer, in a line: an agent is thinking where a person is typing. */
    fun thinking(names: List<String>): String = when (names.size) {
        0 -> ""
        1 -> context.getString(R.string.chats_thinking_one, names[0])
        2 -> context.getString(R.string.chats_thinking_two, names[0], names[1])
        else -> context.getString(R.string.chats_thinking_several)
    }
}

/**
 * ChatsEnvironment is what the conversations of one account share: whether the person has an agent and has turned it
 * on, who they are in a chat, how many messages wait, the switches for what a conversation shows, and the one loader
 * that asks for link cards. It is the Android side of the parts of the iPhone app's `AppModel` that only conversations
 * read. One lives for each account for as long as the process does.
 */
class ChatsEnvironment(val context: Context, val connection: Connection, val session: AppSession, private val preferences: SharedPreferences) {
    val words = ChatWords(context)

    /** The agent's name, which the app calls it by; empty when it has none. */
    var agentName: String by mutableStateOf("")
        private set

    /** Whether the person has turned their own agent on, as far as the server has said; on until it says otherwise. */
    var isAgentEnabledByPerson: Boolean by mutableStateOf(true)
        private set

    /** Whether the person has an agent at all, as far as the server has said. One who has none is offered no switch to turn it on. */
    var hasAgent: Boolean by mutableStateOf(true)
        private set

    /** Whether the agent's identity has been read once. Until then it is not known whether the agent is on. */
    var hasReadAgentFacts: Boolean by mutableStateOf(false)
        private set

    /** Whether the person can write to the people they share a circle with, which needs no agent. */
    var canChat: Boolean by mutableStateOf(false)
        private set

    /** How many messages wait in the chats that are not muted: the badge on Chats. */
    var unreadChatCount: Int by mutableStateOf(0)
        private set

    /** The person's own id, which of a chat's members they are; null until read. */
    var ownUserId: String? by mutableStateOf(null)
        private set

    /** Whether there is an agent to talk to: the server runs agents and the person turned theirs on. */
    val isAgentOn: Boolean get() = session.facts.value.services?.isAssistantEnabled == true && isAgentEnabledByPerson

    /** What the agent is called on screen. */
    val agentTitle: String get() = agentName.ifEmpty { context.getString(R.string.chats_your_agent) }

    /** Names this install to the server, so that when the agent asks where the person is, the app the turn was started from answers. */
    val drawerId: String = preferences.getString(DRAWER_ID_KEY, null) ?: UUID.randomUUID().toString().also { fresh ->
        preferences.edit().putString(DRAWER_ID_KEY, fresh).apply()
    }

    /** The conversations as last read, so a screen opened from the list shows its name at once. */
    private val knownConversations = mutableMapOf<String, AgentConversation>()

    fun known(conversationId: String): AgentConversation? = knownConversations[conversationId]

    fun remember(conversation: AgentConversation) {
        knownConversations[conversation.id] = conversation
    }

    /** Reads the agent's name, and whether the person has turned it on. */
    suspend fun loadAgentName() {
        val identity = runCatching { connection.agentIdentity() }.getOrNull() ?: return
        agentName = identity.agentName.trim()
        isAgentEnabledByPerson = identity.isOn
        hasAgent = identity.hasAgent
        hasReadAgentFacts = true
    }

    /** Reads whether the person can chat, and how much is waiting for them in their chats. */
    suspend fun refreshChats() {
        val permissions = runCatching { connection.accessSession() }.getOrNull()
        permissions?.userId?.let { userId -> ownUserId = userId }
        val canUseCircles = permissions?.holds("circle:use") ?: false
        canChat = canUseCircles
        if (!canUseCircles) {
            unreadChatCount = 0
            session.setBadgeCount(AppSection.Agent, 0)
            return
        }
        val read = runCatching { connection.conversations() }.getOrNull() ?: return
        read.forEach { conversation -> remember(conversation) }
        unreadChatCount = ChatUnread.total(read)
        session.setBadgeCount(AppSection.Agent, unreadChatCount)
    }

    /** The switches for what a conversation shows. They are the same in every conversation and kept on the phone. */
    var isShowingToolCalls: Boolean by mutableStateOf(preferences.getBoolean(SHOW_TOOL_CALLS_KEY, false))
        private set
    var isShowingUsage: Boolean by mutableStateOf(preferences.getBoolean(SHOW_USAGE_KEY, false))
        private set
    var isShowingWorkingNotes: Boolean by mutableStateOf(preferences.getBoolean(SHOW_WORKING_NOTES_KEY, false))
        private set

    fun chooseShowingToolCalls(isShowing: Boolean) {
        isShowingToolCalls = isShowing
        preferences.edit().putBoolean(SHOW_TOOL_CALLS_KEY, isShowing).apply()
    }

    fun chooseShowingUsage(isShowing: Boolean) {
        isShowingUsage = isShowing
        preferences.edit().putBoolean(SHOW_USAGE_KEY, isShowing).apply()
    }

    fun chooseShowingWorkingNotes(isShowing: Boolean) {
        isShowingWorkingNotes = isShowing
        preferences.edit().putBoolean(SHOW_WORKING_NOTES_KEY, isShowing).apply()
    }

    /** How a chat's box is written in: "keyboard", or "voice" to hold a button to talk. Kept on the phone, for every chat. */
    var composerMode: String by mutableStateOf(preferences.getString(COMPOSER_MODE_KEY, "keyboard") ?: "keyboard")
        private set

    fun chooseComposerMode(mode: String) {
        composerMode = mode
        preferences.edit().putString(COMPOSER_MODE_KEY, mode).apply()
    }

    /** The currency the agent's budget is kept in, as the head last read it. */
    var currencyCode: String = "USD"

    val draftStore: DraftStore = DraftStore(preferences, connection.accountKey, isKept = !session.isDemo)

    /** The loader of link cards: the links of a screenful in one question, ten at a time. */
    private val linkPreviewLoader = LinkPreviewLoader { conversationId, urls -> connection.linkPreviews(conversationId, urls) }
    private val answeredPreviews = mutableMapOf<String, LinkPreview?>()

    /** The card of a link written in a conversation, or null when there is none to show. */
    suspend fun linkPreview(link: String, conversationId: String): LinkPreview? {
        val key = "$conversationId\n$link"
        if (answeredPreviews.containsKey(key)) return answeredPreviews[key]
        val preview = linkPreviewLoader.preview(conversationId, link)
        if (preview != null) answeredPreviews[key] = preview
        return preview
    }

    fun answeredLinkPreview(link: String, conversationId: String): LinkPreview? = answeredPreviews["$conversationId\n$link"]

    companion object {
        private const val DRAWER_ID_KEY = "drawerId"
        private const val SHOW_TOOL_CALLS_KEY = "showToolCalls"
        private const val SHOW_USAGE_KEY = "showUsage"
        private const val SHOW_WORKING_NOTES_KEY = "showWorkingNotes"
        private const val COMPOSER_MODE_KEY = "chatComposerMode"

        private val environments = mutableMapOf<String, ChatsEnvironment>()

        /** The environment of the account the connection belongs to, made the first time it is asked for. */
        @Synchronized
        fun of(context: Context, connection: Connection, session: AppSession): ChatsEnvironment {
            val key = connection.accountKey + if (session.isDemo) "#demo" else ""
            val existing = environments[key]
            if (existing != null && existing.connection === connection) return existing
            val preferences = context.applicationContext.getSharedPreferences("teanode-chats", Context.MODE_PRIVATE)
            if (session.isDemo) preferences.edit().clear().apply()
            return ChatsEnvironment(context.applicationContext, connection, session, preferences).also { made -> environments[key] = made }
        }
    }
}

/** DraftStore keeps what was typed and not sent, for each conversation of the account, as the dashboard keeps it. */
class DraftStore(private val preferences: SharedPreferences, private val accountKey: String, private val isKept: Boolean) {
    private fun key(conversationId: String?): String = com.teanode.kit.chats.ConversationDraft.storageKey(accountKey, conversationId)

    fun read(conversationId: String?): String = if (isKept) preferences.getString(key(conversationId), "").orEmpty() else ""

    fun write(conversationId: String?, draft: String) {
        if (!isKept) return
        preferences.edit().apply { if (draft.isEmpty()) remove(key(conversationId)) else putString(key(conversationId), draft) }.apply()
    }
}

val LocalChatsEnvironment = staticCompositionLocalOf<ChatsEnvironment> { error("no ChatsEnvironment: draw the screen inside the Chats part") }
