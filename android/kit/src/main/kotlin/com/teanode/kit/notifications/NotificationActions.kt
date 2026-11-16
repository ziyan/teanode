package com.teanode.kit.notifications

import kotlinx.serialization.Serializable
import java.security.MessageDigest

// What a notification offers to do, as data. The app words each button in the person's language and draws it
// (NotificationBuilder); what each press means is decided here so that it is tested without a phone.

/** NotificationChannelKind is the group of notifications a person can turn off or quiet together in the system's settings. */
enum class NotificationChannelKind(val channelId: String) {
    /** The agent's words, approvals and questions. */
    Agent("agent"),

    /** People in chats. */
    Chats("chats"),

    /** Circle invitations and shared skills. */
    Circles("circles"),

    /** Somebody arrived or left, and shopping. */
    Places("places"),

    /** A coding session asks. */
    Coding("coding"),

    /** The journal's evening question. */
    Journal("journal"),

    /** Photos. */
    Photos("photos"),

    /** Anything else, such as a test. */
    Other("other"),
    ;

    companion object {
        fun of(notificationKind: String): NotificationChannelKind = when (notificationKind) {
            "chat" -> Chats
            "circle", "skill_share_call" -> Circles
            "location", "shopping" -> Places
            "herdr_question" -> Coding
            "journalEveningQuestion" -> Journal
            "photo" -> Photos
            "alert", "message", "approval", "question", "answer", "goal_needs_you", "speak_first" -> Agent
            else -> Other
        }
    }
}

/** FixedButtonWords are the buttons whose words the app writes in the person's language. */
enum class FixedButtonWords { DoIt, Dont, Reply, Open, Awful, Low, Okay, Good, Great }

/** ReplyHint is what a reply field says before the person types. */
enum class ReplyHint {
    /** "Ask your agent…" */
    AskYourAgent,

    /** "Message…" */
    Message,
}

/** NotificationButtonSpec is one button on a notification. */
data class NotificationButtonSpec(
    /** What the press carries back: approve, deny, reply, open, answer:<number>, or journalMood<1 to 5>. */
    val actionId: String,
    /** The words of a fixed button; null for a question's own choices, worded by [literalLabel]. */
    val fixedWords: FixedButtonWords? = null,
    val literalLabel: String? = null,
    /** Whether it asks for text to be typed. */
    val isTextInput: Boolean = false,
    val replyHint: ReplyHint? = null,
    /** Whether the phone must be unlocked first: an action that does something for the person is not given to a lock screen. */
    val requiresUnlock: Boolean = false,
    val isDestructive: Boolean = false,
    /** Whether pressing it brings the app to the front. */
    val doesOpenApp: Boolean = false,
)

object NotificationCategories {
    const val APPROVAL = "approval"
    const val MESSAGE = "message"
    const val CHAT = "chat"
    const val SKILL_SHARE_CALL = "skill_share_call"
    const val CIRCLE = "circle"
    const val LOCATION = "location"
    const val SHOPPING = "shopping"
    const val CODING_QUESTION = "herdr_question"
    const val QUESTION = "question"

    /**
     * The journal's evening question. The server sends it as a notification of this kind carrying the day it asks
     * about as `journalDay`, written YYYY-MM-DD. It is shown with five buttons, Awful to Great, which set that day's
     * mood in the background, without opening the app. Pressing the notification itself opens the day.
     */
    const val JOURNAL_EVENING_QUESTION = "journalEveningQuestion"
    const val JOURNAL_MOOD_ACTION_PREFIX = "journalMood"

    /** The most buttons a question shows. */
    const val QUESTION_BUTTONS_MOST = 4

    /** The mood, 1 (awful) to 5 (great), that a button of the evening question stands for; null for any other button. */
    fun journalMoodLevel(actionId: String): Int? {
        if (!actionId.startsWith(JOURNAL_MOOD_ACTION_PREFIX)) return null
        val level = actionId.removePrefix(JOURNAL_MOOD_ACTION_PREFIX).toIntOrNull() ?: return null
        return if (level in 1..5) level else null
    }

    val open = NotificationButtonSpec("open", FixedButtonWords.Open, doesOpenApp = true)
    private val reply = NotificationButtonSpec("reply", FixedButtonWords.Reply, isTextInput = true, replyHint = ReplyHint.AskYourAgent, requiresUnlock = true)
    private val chatReply = NotificationButtonSpec("reply", FixedButtonWords.Reply, isTextInput = true, replyHint = ReplyHint.Message, requiresUnlock = true)

    private val moodButtons = listOf(FixedButtonWords.Awful, FixedButtonWords.Low, FixedButtonWords.Okay, FixedButtonWords.Good, FixedButtonWords.Great)
        // No unlock: a mood is not private enough to ask for the passcode.
        .mapIndexed { index, words -> NotificationButtonSpec(JOURNAL_MOOD_ACTION_PREFIX + (index + 1), words) }

    /** The buttons a notification is shown with; none for one with no buttons. */
    fun buttons(content: NotificationContent): List<NotificationButtonSpec> = when (content.notificationKind) {
        "test" -> emptyList()
        APPROVAL -> listOf(
            // Approving does what the agent asked to do, so the phone must be unlocked first.
            NotificationButtonSpec("approve", FixedButtonWords.DoIt, requiresUnlock = true),
            NotificationButtonSpec("deny", FixedButtonWords.Dont, requiresUnlock = true, isDestructive = true),
        )
        CHAT -> listOf(chatReply, open)
        // Only to open the app: an answer that runs somebody's payment is given on the card, not from a lock screen.
        SKILL_SHARE_CALL -> listOf(open)
        // An invitation is answered on the Circles screen, which Open goes to.
        CIRCLE -> listOf(open)
        // Somebody arrived or left: nothing to answer, only Location to open.
        LOCATION -> listOf(open)
        // Arrived at a store, or somebody started shopping: only Shopping to open.
        SHOPPING -> listOf(open)
        // A coding session asks: it is answered on the Coding screen, where its options are.
        CODING_QUESTION -> listOf(open)
        JOURNAL_EVENING_QUESTION -> moodButtons
        QUESTION -> (content.buttons ?: emptyList()).take(QUESTION_BUTTONS_MOST).map { button ->
            when (button.buttonAction) {
                "reply" -> reply
                "open" -> open
                else -> NotificationButtonSpec(button.buttonAction, literalLabel = button.label, requiresUnlock = true)
            }
        }
        else -> listOf(reply, open)
    }

    /** A name for the set of buttons a question has, so that the same choices share one. */
    fun questionCategoryName(buttons: List<NotificationButton>): String {
        val digest = MessageDigest.getInstance("SHA-256")
            .digest(buttons.take(QUESTION_BUTTONS_MOST).joinToString("\u0001") { button -> "${button.buttonAction}\u0000${button.label}" }.toByteArray())
        return "question-" + digest.take(8).joinToString("") { byte -> "%02x".format(byte) }
    }
}

/** NotificationInfo is what a shown notification carries for the app: where it came from and what its buttons answer. */
@Serializable
data class NotificationInfo(
    /** The account whose key opened it, which is the account it came from; empty on one shown before each account had its own key. */
    val accountKey: String = "",
    val notificationKind: String = "",
    val conversationId: String = "",
    val runId: String = "",
    val callId: String = "",
    /** For a location notification, the person who arrived or left. */
    val locationUserId: String = "",
    /** For a shopping notification of arriving at a store, the store. */
    val shoppingStoreName: String = "",
    /** For a shopping notification that somebody started shopping a list, the list. */
    val shoppingListId: String = "",
    /** For a coding session's question, the computer and pane of the session. */
    val computer: String = "",
    val paneId: String = "",
    /** For the journal's evening question, the day it asks about, YYYY-MM-DD. */
    val journalDay: String = "",
    /** A question's choices, by the button that gives them. */
    val answers: Map<String, String> = emptyMap(),
) {
    companion object {
        fun of(content: NotificationContent, accountKey: String): NotificationInfo = NotificationInfo(
            accountKey = accountKey,
            notificationKind = content.notificationKind,
            conversationId = content.conversationId ?: "",
            runId = content.runId ?: "",
            callId = content.callId ?: "",
            locationUserId = content.locationUserId ?: "",
            shoppingStoreName = content.shoppingStoreName ?: "",
            shoppingListId = content.shoppingListId ?: "",
            computer = content.computer ?: "",
            paneId = content.paneId ?: "",
            journalDay = content.journalDay ?: "",
            answers = (content.buttons ?: emptyList()).filter { button -> button.buttonAction.startsWith("answer:") }.associate { button -> button.buttonAction to button.label },
        )
    }
}

/** NotificationDestination is the place a notification opens: the part of the app, and what in it. */
sealed class NotificationDestination {
    /** An invitation is answered on Circles, in the settings. */
    data object Circles : NotificationDestination()

    /** Somebody arrived or left: Location, with them selected. */
    data class Location(val userId: String?) : NotificationDestination()

    /** Arrived at a store, or somebody started shopping a list: Shopping, at that store or that list. */
    data class Shopping(val storeName: String?, val listId: String?) : NotificationDestination()

    /** A coding session asks: Coding, on that session, where its question is answered. */
    data class Coding(val computer: String, val paneId: String) : NotificationDestination() {
        companion object {
            fun of(computer: String, paneId: String): NotificationDestination = if (computer.isNotEmpty() && paneId.isNotEmpty()) Coding(computer, paneId) else CodingHome
        }
    }

    /** Coding, with no session chosen. */
    data object CodingHome : NotificationDestination()

    /** The evening question: the day it asks about, in the Journal. */
    data class JournalDay(val day: String) : NotificationDestination()

    /** A conversation of the agent or of people. */
    data class Conversation(val conversationId: String) : NotificationDestination()

    /** The main conversation. */
    data object Main : NotificationDestination()
}

/** NotificationPress is what a button pressed on a notification means. */
sealed class NotificationPress {
    /** The notification is from an account that is not on the phone, or does not say which it is from. */
    data class UnknownAccount(val isOpening: Boolean, val doesNameAccount: Boolean) : NotificationPress()

    /** Open the app at a place, as the account it came from. */
    data class Open(val accountKey: String, val destination: NotificationDestination) : NotificationPress()

    data class Resolve(val runId: String, val callId: String, val isApproved: Boolean) : NotificationPress()

    /** Answer the agent's question with what was typed. */
    data class AnswerQuestion(val runId: String, val callId: String, val answer: String) : NotificationPress()

    /** Say it in the conversation (to the agent, or to the people of a chat). */
    data class Say(val conversationId: String?, val text: String, val isChat: Boolean) : NotificationPress()

    data class SetMood(val day: String, val moodLevel: Int) : NotificationPress()

    /** Nothing to do. */
    data object None : NotificationPress()

    companion object {
        /** The words that stand for pressing the notification itself. */
        const val DEFAULT_ACTION = "default"

        /**
         * What a press means. [keptAccountKeys] are the accounts on the phone; one shown before each account had its
         * own key does not say which it came from, and can only be the one account when there is one.
         */
        fun decide(info: NotificationInfo, actionId: String, typedText: String?, keptAccountKeys: List<String>): NotificationPress {
            val isOpening = actionId == DEFAULT_ACTION || actionId == "open"
            val notifying = if (info.accountKey.isEmpty()) keptAccountKeys.singleOrNull() else keptAccountKeys.firstOrNull { key -> key == info.accountKey }
            if (notifying == null) return UnknownAccount(isOpening, doesNameAccount = info.accountKey.isNotEmpty())
            if (isOpening) return Open(notifying, destinationOf(info))
            when (actionId) {
                "approve", "deny" -> return Resolve(info.runId, info.callId, actionId == "approve")
                "reply" -> {
                    val text = (typedText ?: "").trim()
                    if (text.isEmpty()) return None
                    if (info.notificationKind == "question" && info.callId.isNotEmpty()) return AnswerQuestion(info.runId, info.callId, text)
                    return Say(info.conversationId.ifEmpty { null }, text, isChat = info.notificationKind == "chat")
                }
            }
            if (info.notificationKind == NotificationCategories.JOURNAL_EVENING_QUESTION) {
                // One of the five faces of the evening question.
                val moodLevel = NotificationCategories.journalMoodLevel(actionId) ?: return None
                if (!isDayText(info.journalDay)) return None
                return SetMood(info.journalDay, moodLevel)
            }
            val answer = info.answers[actionId] ?: return None
            return AnswerQuestion(info.runId, info.callId, answer)
        }

        /** Whether text is a day written YYYY-MM-DD that exists. */
        fun isDayText(text: String): Boolean = try {
            text.length == 10 && java.time.LocalDate.parse(text).toString() == text
        } catch (_: java.time.format.DateTimeParseException) {
            false
        }

        fun destinationOf(info: NotificationInfo): NotificationDestination = when {
            info.notificationKind == "circle" -> NotificationDestination.Circles
            info.notificationKind == "location" -> NotificationDestination.Location(info.locationUserId.ifEmpty { null })
            info.notificationKind == "shopping" ->
                NotificationDestination.Shopping(info.shoppingStoreName.ifEmpty { null }, if (info.shoppingStoreName.isEmpty()) info.shoppingListId.ifEmpty { null } else null)
            info.notificationKind == "herdr_question" -> NotificationDestination.Coding.of(info.computer, info.paneId)
            info.notificationKind == NotificationCategories.JOURNAL_EVENING_QUESTION -> NotificationDestination.JournalDay(info.journalDay)
            info.conversationId.isNotEmpty() -> NotificationDestination.Conversation(info.conversationId)
            else -> NotificationDestination.Main
        }
    }
}

object NotificationDocuments {
    const val RESOLVE_CONFIRMATION = """
        mutation (${'$'}runId: String!, ${'$'}callId: String!, ${'$'}approve: Boolean!) { ResolveAgentConfirmation(runId: ${'$'}runId, callId: ${'$'}callId, approve: ${'$'}approve) }
        """
    const val ANSWER_QUESTION = """
        mutation (${'$'}runId: String!, ${'$'}callId: String!, ${'$'}answer: String!) { AnswerAgentQuestion(runId: ${'$'}runId, callId: ${'$'}callId, answer: ${'$'}answer) }
        """
    const val ASK = """
        mutation (${'$'}conversationId: String, ${'$'}message: String!, ${'$'}surface: String, ${'$'}attachmentIds: [String!], ${'$'}references: [AgentReferenceInput!], ${'$'}drawerId: String, ${'$'}interruptedAnswer: InterruptedAnswerInput, ${'$'}addressedAgentIds: [String!], ${'$'}mentionedUserIds: [String!], ${'$'}isAllMentioned: Boolean, ${'$'}voiceMessageAttachmentId: String) {
          AskAgent(conversationId: ${'$'}conversationId, message: ${'$'}message, surface: ${'$'}surface, attachmentIds: ${'$'}attachmentIds, references: ${'$'}references, drawerId: ${'$'}drawerId, interruptedAnswer: ${'$'}interruptedAnswer, addressedAgentIds: ${'$'}addressedAgentIds, mentionedUserIds: ${'$'}mentionedUserIds, isAllMentioned: ${'$'}isAllMentioned, voiceMessageAttachmentId: ${'$'}voiceMessageAttachmentId) { runId conversationId }
        }
        """
    const val SET_JOURNAL_MOOD = """
        mutation (${'$'}day: String!, ${'$'}moodLevel: Int, ${'$'}moodWords: String) {
          SetJournalMood(day: ${'$'}day, moodLevel: ${'$'}moodLevel, moodWords: ${'$'}moodWords) { day }
        }
        """
}
