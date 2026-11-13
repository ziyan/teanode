package com.teanode.kit.chats

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.accessSession
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.util.UUID

/** The surface the app names on its turns (internal/agent/surface.go): the server knows the iPhone app's names, which the Android app sends until it has its own. */
object Surface {
    const val TYPED = "ios"
    const val VOICE = "ios_voice"
}

/** How a turn ended, for a caller that waits for it without showing the conversation. */
sealed interface RunAnswer {
    /** The agent answered: the whole answer. */
    data class Answered(val answerText: String) : RunAnswer

    /** The agent waits for the person: an approval or a question, answered in the app. */
    data object WaitingForPerson : RunAnswer

    /** The turn failed: what the server said. */
    data class Failed(val errorMessage: String) : RunAnswer

    /** It was still working when the caller stopped waiting. */
    data object StillWorking : RunAnswer
}

/** ConversationPage is a page of a conversation's messages, newest last, with its open cards and the agent's list for it. */
data class ConversationPage(
    val conversation: AgentConversation?,
    val messages: List<AgentMessage>,
    val interactions: List<AgentInteraction>,
    /** How many messages the conversation has in all. */
    val total: Int,
    val todos: List<AgentTodo>,
    /** Whose agent this is, when an operator is looking at somebody else's. */
    val actingAs: String?,
)

/** AgentIdea is something the agent suggests starting with. */
@Serializable
data class AgentIdea(val id: String, val emoji: String? = null, val headline: String, val body: String? = null, val openingRequest: String? = null)

/** What the person's agent is, as the chat's composer needs it. */
data class AgentIdentity(val agentName: String, val isOn: Boolean, val hasAgent: Boolean)

/** A turn that was started: its run and the conversation it runs in. */
data class AskedTurn(val runId: String, val conversationId: String)

/** What the person said of an answer they interrupted: what they heard, and what was left unheard. */
data class InterruptedAnswer(val heardText: String, val unheardText: String)

@Serializable
private class AgentAnswer(@SerialName("ReadAgent") val read: Read) {
    @Serializable
    class Read(val agent: Agent? = null)

    @Serializable
    class Agent(val name: String? = null, val enabled: Boolean? = null)
}

@Serializable
private class ConversationsAnswer(@SerialName("ListAgentConversations") val conversations: List<AgentConversation>)

@Serializable
private class ConversationAnswer(
    @SerialName("ReadAgentConversation") val read: Read,
    @SerialName("ListAgentInteractions") val interactions: List<AgentInteraction>? = null,
) {
    @Serializable
    class Read(
        val conversation: AgentConversation? = null,
        val messages: List<AgentMessage> = emptyList(),
        val total: Int = 0,
        val todos: List<AgentTodo>? = null,
        val actingAs: String? = null,
    )
}

@Serializable
private class AskedAnswer(@SerialName("AskAgent") val asked: Asked) {
    @Serializable
    class Asked(val runId: String, val conversationId: String)
}

@Serializable
private class StartedConversationAnswer(@SerialName("StartAgentConversation") val started: AgentConversation)

@Serializable
private class BooleanAnswer(
    @SerialName("ResolveAgentConfirmation") val resolved: Boolean? = null,
    @SerialName("AnswerAgentQuestion") val answered: Boolean? = null,
)

@Serializable
private class IdeasAnswer(@SerialName("ListAgentIdeas") val listed: Listed) {
    @Serializable
    class Listed(val ideas: List<AgentIdea>)
}

@Serializable
private class StartedIdeaAnswer(@SerialName("StartAgentIdea") val started: Started) {
    @Serializable
    class Started(val conversation: Conversation? = null, val openingRequest: String? = null) {
        @Serializable
        class Conversation(val id: String)
    }
}

@Serializable
private class RunAnswerBody(@SerialName("ReadAgentRun") val run: Run) {
    @Serializable
    class Run(val done: Boolean = false, val events: List<AgentEvent> = emptyList())
}

@Serializable
private class ChatPeopleAnswer(@SerialName("ListCirclePeople") val people: List<ChatPerson>)

@Serializable
private class OpenedChatAnswer(
    @SerialName("OpenDirectChat") val opened: Opened? = null,
    @SerialName("CreateGroupChat") val created: Opened? = null,
) {
    @Serializable
    class Opened(val id: String)
}

/** An answer whose content the app does not read: the call only has to have gone through. */
@Serializable
private class UnreadAnswer(val ignored: JsonElement? = null)

/** A variable of a document, written the way the server's schema takes it. */
private fun text(value: String): JsonElement = JsonPrimitive(value)

private fun flag(value: Boolean): JsonElement = JsonPrimitive(value)

private fun texts(values: Collection<String>): JsonElement = JsonArray(values.map { value -> JsonPrimitive(value) })

/** Runs a mutation whose answer is not read; the server refusing it is the error. */
private suspend fun Connection.executeWithoutReading(document: String, variables: Map<String, JsonElement>) {
    execute(document, UnreadAnswer.serializer(), variables)
}

/** The name the agent goes by, whether it is on, and whether the person has one at all. A chat needs none of it. */
suspend fun Connection.agentIdentity(): AgentIdentity {
    val agent = execute(ChatDocuments.AGENT, AgentAnswer.serializer()).read.agent
    return AgentIdentity(agent?.name.orEmpty(), agent?.enabled ?: false, agent != null)
}

/** The agent's name, which the app calls it by; empty when it has none. */
suspend fun Connection.agentName(): String = agentIdentity().agentName

/** The conversations, or the archived ones, or those matching words. */
suspend fun Connection.conversations(isArchived: Boolean = false, matching: String? = null): List<AgentConversation> {
    val variables = if (!matching.isNullOrEmpty()) mapOf("query" to text(matching)) else mapOf("archived" to flag(isArchived))
    return execute(ChatDocuments.CONVERSATIONS, ConversationsAnswer.serializer(), variables).conversations
}

/** One page of a conversation, its open cards and its list; a null id is the main conversation. */
suspend fun Connection.conversation(conversationId: String?, first: Int = 100, offset: Int = 0): ConversationPage {
    val variables = mutableMapOf<String, JsonElement>("first" to JsonPrimitive(first), "offset" to JsonPrimitive(offset))
    if (conversationId != null) variables["conversationId"] = text(conversationId)
    val answer = execute(ChatDocuments.CONVERSATION, ConversationAnswer.serializer(), variables)
    return ConversationPage(
        conversation = answer.read.conversation, messages = answer.read.messages, interactions = answer.interactions ?: emptyList(),
        total = answer.read.total, todos = answer.read.todos ?: emptyList(), actingAs = answer.read.actingAs,
    )
}

suspend fun Connection.updateConversation(conversationId: String, title: String? = null, isArchived: Boolean? = null) {
    val variables = mutableMapOf<String, JsonElement>("conversationId" to text(conversationId))
    if (title != null) variables["title"] = text(title)
    if (isArchived != null) variables["archived"] = flag(isArchived)
    executeWithoutReading(ChatDocuments.UPDATE_CONVERSATION, variables)
}

suspend fun Connection.deleteConversation(conversationId: String) {
    executeWithoutReading(ChatDocuments.DELETE_CONVERSATION, mapOf("conversationId" to text(conversationId)))
}

/** Makes a conversation the one a person reaches from the main chat's place. */
suspend fun Connection.setMainConversation(conversationId: String) {
    executeWithoutReading(ChatDocuments.SET_MAIN_CONVERSATION, mapOf("conversationId" to text(conversationId)))
}

/** What the agent suggests starting with, in the person's language. */
suspend fun Connection.ideas(language: String): List<AgentIdea> {
    val variables = mapOf("ideaStatuses" to texts(listOf("new", "shown")), "language" to text(language))
    return execute(ChatDocuments.IDEAS, IdeasAnswer.serializer(), variables).listed.ideas
}

/** Starts an idea in a conversation (a new one when the id is null) and answers where it went and what to ask. */
suspend fun Connection.startIdea(ideaId: String, conversationId: String?, language: String): Pair<String?, String?> {
    val variables = mutableMapOf<String, JsonElement>("ideaId" to text(ideaId), "language" to text(language))
    if (conversationId != null) variables["conversationId"] = text(conversationId)
    val started = execute(ChatDocuments.START_IDEA, StartedIdeaAnswer.serializer(), variables).started
    return started.conversation?.id to started.openingRequest
}

suspend fun Connection.markIdeasShown(ideaIds: List<String>) {
    executeWithoutReading(ChatDocuments.MARK_IDEAS_SHOWN, mapOf("ideaIds" to texts(ideaIds)))
}

/**
 * Asks the agent something; returns the run and the conversation it runs in. In a chat, `mentionedUserIds` says
 * whom the message mentions (mentioning a person asks nothing of their agent) and `voiceMessageAttachmentId` says the
 * message is the recording, whose words are the server's.
 */
suspend fun Connection.ask(
    message: String,
    conversationId: String?,
    drawerId: String,
    attachmentIds: List<String> = emptyList(),
    references: List<AgentReference> = emptyList(),
    surface: String = Surface.TYPED,
    interruptedAnswer: InterruptedAnswer? = null,
    addressedAgentIds: List<String> = emptyList(),
    mentionedUserIds: List<String> = emptyList(),
    isAllMentioned: Boolean = false,
    voiceMessageAttachmentId: String? = null,
): AskedTurn {
    val variables = mutableMapOf<String, JsonElement>(
        "message" to text(message), "surface" to text(surface), "drawerId" to text(drawerId), "attachmentIds" to texts(attachmentIds),
    )
    if (references.isNotEmpty()) {
        variables["references"] = com.teanode.kit.TeaNodeJson.encodeToJsonElement(kotlinx.serialization.builtins.ListSerializer(AgentReference.serializer()), references)
    }
    if (interruptedAnswer != null) {
        variables["interruptedAnswer"] = JsonObject(mapOf("heardText" to text(interruptedAnswer.heardText), "unheardText" to text(interruptedAnswer.unheardText)))
    }
    if (addressedAgentIds.isNotEmpty()) variables["addressedAgentIds"] = texts(addressedAgentIds)
    if (mentionedUserIds.isNotEmpty()) variables["mentionedUserIds"] = texts(mentionedUserIds)
    if (isAllMentioned) variables["isAllMentioned"] = flag(true)
    if (voiceMessageAttachmentId != null) variables["voiceMessageAttachmentId"] = text(voiceMessageAttachmentId)
    if (conversationId != null) variables["conversationId"] = text(conversationId)
    val asked = execute(ChatDocuments.ASK, AskedAnswer.serializer(), variables).asked
    return AskedTurn(asked.runId, asked.conversationId)
}

suspend fun Connection.startConversation(title: String?): AgentConversation {
    val variables = if (title != null) mapOf("title" to text(title)) else emptyMap()
    return execute(ChatDocuments.START_CONVERSATION, StartedConversationAnswer.serializer(), variables).started
}

/** Settles an approval; false when the card was answered already or is no longer waiting. */
suspend fun Connection.resolveConfirmation(runId: String, callId: String, isApproved: Boolean): Boolean {
    val variables = mapOf("runId" to text(runId), "callId" to text(callId), "approve" to flag(isApproved))
    return execute(ChatDocuments.RESOLVE_CONFIRMATION, BooleanAnswer.serializer(), variables).resolved ?: false
}

/** Answers the agent's question; false when the card was answered already or is no longer waiting. */
suspend fun Connection.answerQuestion(runId: String, callId: String, answer: String): Boolean {
    val variables = mapOf("runId" to text(runId), "callId" to text(callId), "answer" to text(answer))
    return execute(ChatDocuments.ANSWER_QUESTION, BooleanAnswer.serializer(), variables).answered ?: false
}

@Serializable
private class CodingAnswer(@SerialName("AnswerAgentHerdrQuestion") val answered: Answered) {
    @Serializable
    class Answered(val isAnswerAccepted: Boolean, val answeredWith: String)
}

/** Answers a coding session's question: the options chosen, or what to type. Returns how the session took it, in its words. */
suspend fun Connection.answerCodingQuestion(question: CodingQuestion, options: List<CodingQuestion.Option>, freeText: String?): String {
    val variables = mutableMapOf<String, JsonElement>(
        "computer" to text(question.computer), "paneId" to text(question.paneId), "questionFingerprint" to text(question.questionFingerprint),
    )
    if (freeText != null) {
        variables["freeText"] = text(freeText)
    } else {
        variables["optionNumbers"] = JsonArray(options.map { option -> JsonPrimitive(option.optionNumber) })
        variables["optionLabels"] = texts(options.map { option -> option.optionLabel })
    }
    val answered = execute(ChatDocuments.ANSWER_CODING_QUESTION, CodingAnswer.serializer(), variables).answered
    if (!answered.isAnswerAccepted) throw TeaNodeError.Refused("The coding session did not take that answer. It may have moved on.")
    return answered.answeredWith
}

suspend fun Connection.stopRun(runId: String) {
    executeWithoutReading(ChatDocuments.STOP_RUN, mapOf("runId" to text(runId)))
}

/** Answers the agent's question of where the person is: a place, or why there is none. */
suspend fun Connection.answerLocation(
    runId: String,
    callId: String,
    latitudeDegrees: Double?,
    longitudeDegrees: Double?,
    accuracyMeters: Double?,
    measuredAt: java.time.Instant?,
    errorMessage: String?,
) {
    val variables = mutableMapOf<String, JsonElement>("runId" to text(runId), "callId" to text(callId))
    if (latitudeDegrees != null && longitudeDegrees != null) {
        variables["latitudeDegrees"] = JsonPrimitive(latitudeDegrees)
        variables["longitudeDegrees"] = JsonPrimitive(longitudeDegrees)
        if (accuracyMeters != null) variables["accuracyMeters"] = JsonPrimitive(accuracyMeters)
        if (measuredAt != null) variables["measuredAt"] = text(measuredAt.toString())
    } else {
        variables["errorMessage"] = text(errorMessage ?: "The location is not available.")
    }
    executeWithoutReading(ChatDocuments.ANSWER_LOCATION, variables)
}

/** Puts the person's reaction on a message, replacing theirs; an empty emoji takes it away. */
suspend fun Connection.react(messageId: String, emoji: String) {
    executeWithoutReading(ChatDocuments.REACT_TO_MESSAGE, mapOf("messageId" to text(messageId), "emoji" to text(emoji)))
}

/** This launch of the app's name in its presence reports, so the server keeps its report apart from the person's dashboard tabs: the app going to the background then counts as leaving at once. */
val presenceReporterId: String = "app-" + UUID.randomUUID()

/** The variables of a presence report, apart from the call so that what is sent can be checked. */
fun presenceVariables(isVisible: Boolean, conversationId: String): Map<String, JsonElement> = mapOf(
    "isVisible" to flag(isVisible), "idleSeconds" to JsonPrimitive(0), "reporterId" to text(presenceReporterId), "conversationId" to text(conversationId),
)

/**
 * Says whether the app is in front of the person and which conversation it shows, an empty id when it is on another
 * screen: a message in a chat is held back from the phone only while that chat is on screen.
 */
suspend fun Connection.reportPresence(isVisible: Boolean, conversationId: String = "") {
    executeWithoutReading(ChatDocuments.REPORT_PRESENCE, presenceVariables(isVisible, conversationId))
}

/** A run's events from `after` on, waiting up to `waitSeconds` for the first (the server waits at most 25). */
suspend fun Connection.runEvents(runId: String, after: Int = 0, waitSeconds: Int = 20): Pair<List<AgentEvent>, Boolean> {
    val variables = mapOf("runId" to text(runId), "after" to JsonPrimitive(after), "wait" to JsonPrimitive(waitSeconds))
    val run = execute(ChatDocuments.READ_RUN, RunAnswerBody.serializer(), variables).run
    return run.events to run.done
}

/** Waits for a run to end, until `deadlineMS` (epoch), and says how it ended. The last whole message is the answer: those before it came between tool calls. */
suspend fun Connection.answerTo(runId: String, deadlineMS: Long): RunAnswer {
    var after = 0
    var answerText = ""
    while (true) {
        val secondsLeft = ((deadlineMS - System.currentTimeMillis()) / 1000).toInt()
        if (secondsLeft <= 0) return RunAnswer.StillWorking
        val (events, isDone) = runEvents(runId, after, minOf(secondsLeft, 20))
        for (event in events) {
            after = maxOf(after, event.sequence + 1)
            when (event.kind) {
                "message" -> answerText = event.text.orEmpty()
                "confirmation", "question", "locate" -> return RunAnswer.WaitingForPerson
                "error" -> return RunAnswer.Failed(event.error ?: event.text.orEmpty())
            }
        }
        if (isDone) return RunAnswer.Answered(answerText)
    }
}

/** Who the token signs in as, by id: which of a chat's members is the person. */
suspend fun Connection.userId(): String? = accessSession().userId

/** Everybody the person shares a circle with: the people a chat can be started with. */
suspend fun Connection.chatPeople(): List<ChatPerson> = execute(ChatDocuments.CHAT_PEOPLE, ChatPeopleAnswer.serializer()).people

/** The person's direct chat with somebody, made when there is none and the same one every time: the id of the person's conversation for it. */
suspend fun Connection.openDirectChat(userId: String): String =
    execute(ChatDocuments.OPEN_DIRECT_CHAT, OpenedChatAnswer.serializer(), mapOf("userId" to text(userId))).opened?.id ?: throw TeaNodeError.UnreadableAnswer

/** Makes a group chat with the person as its admin and these people in it, and answers the person's conversation for it. */
suspend fun Connection.createGroupChat(draft: GroupChatDraft): String {
    val variables = mapOf("title" to text(draft.trimmedTitle), "userIds" to texts(draft.userIds.sorted()))
    return execute(ChatDocuments.CREATE_GROUP_CHAT, OpenedChatAnswer.serializer(), variables).created?.id ?: throw TeaNodeError.UnreadableAnswer
}

/** Renames a group chat. Its admins only. */
suspend fun Connection.renameChat(conversationId: String, title: String) {
    executeWithoutReading(ChatDocuments.RENAME_CHAT, mapOf("conversationId" to text(conversationId), "title" to text(title.trim())))
}

/** Puts people in a group chat. Its admins only. */
suspend fun Connection.addChatMembers(conversationId: String, userIds: List<String>) {
    executeWithoutReading(ChatDocuments.ADD_CHAT_MEMBERS, mapOf("conversationId" to text(conversationId), "userIds" to texts(userIds)))
}

/** Takes somebody out of a group chat. Its admins only. */
suspend fun Connection.removeChatMember(conversationId: String, userId: String) {
    executeWithoutReading(ChatDocuments.REMOVE_CHAT_MEMBER, mapOf("conversationId" to text(conversationId), "userId" to text(userId)))
}

/** Leaves a group chat. It stays readable up to the time of leaving. */
suspend fun Connection.leaveChat(conversationId: String) {
    executeWithoutReading(ChatDocuments.LEAVE_CHAT, mapOf("conversationId" to text(conversationId)))
}

/** Changes what a message of the person's says. */
suspend fun Connection.editChatMessage(messageId: String, message: String) {
    executeWithoutReading(ChatDocuments.EDIT_CHAT_MESSAGE, mapOf("messageId" to text(messageId), "message" to text(message)))
}

/** Writes the words of a voice message that has none, paid by the person's agent; everybody in the chat sees them, told as an edit. */
suspend fun Connection.transcribeChatVoiceMessage(messageId: String) {
    executeWithoutReading(ChatDocuments.TRANSCRIBE_CHAT_VOICE_MESSAGE, mapOf("messageId" to text(messageId)))
}

/** Takes a message back: the person's own, or any in a group chat they are an admin of. */
suspend fun Connection.deleteChatMessage(messageId: String) {
    executeWithoutReading(ChatDocuments.DELETE_CHAT_MESSAGE, mapOf("messageId" to text(messageId)))
}

/** Says the person has read a chat through a message. The mark moves forward and never back. */
suspend fun Connection.markChatRead(conversationId: String, throughMessageId: String) {
    executeWithoutReading(ChatDocuments.MARK_CHAT_READ, mapOf("conversationId" to text(conversationId), "throughMessageId" to text(throughMessageId)))
}

/** Chooses what the person's phone is told of in a chat: every message, only one that mentions them or everybody, or nothing. */
suspend fun Connection.setChatNotificationLevel(conversationId: String, level: ChatNotificationLevel) {
    executeWithoutReading(ChatDocuments.SET_CHAT_NOTIFICATION_LEVEL, mapOf("conversationId" to text(conversationId), "notificationLevel" to text(level.wireName)))
}

/** Tells the others the person is writing. Nothing is stored. */
suspend fun Connection.setChatTyping(conversationId: String) {
    executeWithoutReading(ChatDocuments.SET_CHAT_TYPING, mapOf("conversationId" to text(conversationId)))
}

/** Makes the person's agent remember a chat from now on, or stop. What it learned stays. */
suspend fun Connection.setChatAgentReading(conversationId: String, isReading: Boolean) {
    executeWithoutReading(ChatDocuments.SET_CHAT_AGENT_READING, mapOf("conversationId" to text(conversationId), "isAgentReading" to flag(isReading)))
}

/** Lets the others in a chat address the person's agent there, or stops them. */
suspend fun Connection.setChatAgentAskableByOthers(conversationId: String, isAskable: Boolean) {
    executeWithoutReading(ChatDocuments.SET_CHAT_AGENT_ASKABLE_BY_OTHERS, mapOf("conversationId" to text(conversationId), "isAgentAskableByOthers" to flag(isAskable)))
}
