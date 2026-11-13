package com.teanode.app.chats

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.asReasonSentence
import com.teanode.kit.chats.AddressableAgent
import com.teanode.kit.chats.AgentConversation
import com.teanode.kit.chats.AgentAttachment
import com.teanode.kit.chats.AgentEvent
import com.teanode.kit.chats.AgentIdea
import com.teanode.kit.chats.AgentInteraction
import com.teanode.kit.chats.AgentMessage
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.chats.AgentTodo
import com.teanode.kit.chats.ChatAuthor
import com.teanode.kit.chats.ChatKind
import com.teanode.kit.chats.ChatMention
import com.teanode.kit.chats.ChatMentionWord
import com.teanode.kit.chats.ChatNotificationLevel
import com.teanode.kit.chats.ChatSummary
import com.teanode.kit.chats.ChatTyping
import com.teanode.kit.chats.CodingQuestion
import com.teanode.kit.chats.ConversationFeed
import com.teanode.kit.chats.MentionChoice
import com.teanode.kit.chats.ServerTime
import com.teanode.kit.chats.Transcript
import com.teanode.kit.chats.TranscriptLine
import com.teanode.kit.chats.addChatMembers
import com.teanode.kit.chats.addressableAgents
import com.teanode.kit.chats.answerCodingQuestion
import com.teanode.kit.chats.answerLocation
import com.teanode.kit.chats.answerQuestion
import com.teanode.kit.chats.ask
import com.teanode.kit.chats.conversation
import com.teanode.kit.chats.deleteChatMessage
import com.teanode.kit.chats.editChatMessage
import com.teanode.kit.chats.ideas
import com.teanode.kit.chats.leaveChat
import com.teanode.kit.chats.markChatRead
import com.teanode.kit.chats.markIdeasShown
import com.teanode.kit.chats.mentionChoices
import com.teanode.kit.chats.mentionWords
import com.teanode.kit.chats.newestChatMessageId
import com.teanode.kit.chats.react
import com.teanode.kit.chats.removeChatMember
import com.teanode.kit.chats.renameChat
import com.teanode.kit.chats.resolveConfirmation
import com.teanode.kit.chats.setChatAgentAskableByOthers
import com.teanode.kit.chats.setChatAgentReading
import com.teanode.kit.chats.setChatNotificationLevel
import com.teanode.kit.chats.setChatTyping
import com.teanode.kit.chats.startIdea
import com.teanode.kit.chats.stopRun
import com.teanode.kit.chats.transcribeChatVoiceMessage
import com.teanode.kit.chats.userId
import com.teanode.kit.chats.voiceAvailability
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import java.time.Instant

/** PendingFile is a file chosen to send with the next message: a picture, made small enough, or any other file as it is. */
class PendingFile(
    val id: String,
    val name: String,
    val contentType: String,
    val data: ByteArray,
    /** What the composer shows for it: the picture itself, or nothing. */
    val preview: android.graphics.Bitmap?,
) {
    companion object {
        /** The most the server takes in one message (the agent's maxAttachmentBytes, 25 MB unless the operator changed it). */
        const val BYTES_MOST = 25 shl 20
    }
}

/**
 * ConversationModel is one conversation as the screen shows it: its transcript, kept up to date from the server's feed,
 * and what the person sends into it. It is the Android side of the iPhone app's `ConversationModel`.
 */
class ConversationModel(
    private val environment: ChatsEnvironment,
    conversationId: String?,
    initialConversation: AgentConversation? = null,
) {
    val connection: Connection = environment.connection
    private val words = environment.words
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    /** Named once: the main conversation learns its id when it loads, and a key made from that would never be read at the next launch. */
    private val draftConversationId: String? = if (initialConversation?.isMain == true) null else conversationId

    private var followJob: Job? = null
    private var typingPruning: Job? = null

    /** The conversation's id; null for the main conversation until it is loaded. */
    var conversationId: String? = conversationId
        private set
    var conversation: AgentConversation? by mutableStateOf(initialConversation)
        private set
    var title: String by mutableStateOf("")
        private set
    private var current: Transcript = Transcript()

    /** The lines of the transcript as the screen shows them; replaced whole after every change. */
    var lines: List<TranscriptLine> by mutableStateOf(emptyList())
        private set
    var isLoaded: Boolean by mutableStateOf(false)
        private set
    var loadProblemText: String? by mutableStateOf(null)
        private set

    /** The messages read so far, oldest first, and how many there are in all: older ones are read a page at a time. */
    private var messages: List<AgentMessage> = emptyList()
    private var interactions: List<AgentInteraction> = emptyList()
    var total: Int by mutableStateOf(0)
        private set
    var storedMessageCount: Int by mutableStateOf(0)
        private set
    var isLoadingEarlier: Boolean by mutableStateOf(false)
        private set
    val earlierCount: Int get() = maxOf(0, total - storedMessageCount)

    /** The agent's list for this conversation, and whose agent this is when an operator looks at somebody else's. */
    var todos: List<AgentTodo> by mutableStateOf(emptyList())
        private set
    var actingAs: String? by mutableStateOf(null)
        private set

    /** Ideas to start an empty conversation with. */
    var ideas: List<AgentIdea> by mutableStateOf(emptyList())
        private set

    var draft: String by mutableStateOf("")
        private set
    var pendingFiles: List<PendingFile> by mutableStateOf(emptyList())
        private set

    /** What the next message refers to: a message replied to, and mails, pages and transactions asked about. */
    var references: List<AgentReference> by mutableStateOf(emptyList())
        private set

    /** A message opened to select part of its words, from its menu. */
    var selectingText: String? by mutableStateOf(null)

    /** The whole of a message too long for its bubble, open to read. */
    var readingText: String? by mutableStateOf(null)

    /** A message of a chat opened to change what it says, and one asked about taking back. */
    var editingLine: TranscriptLine? by mutableStateOf(null)
    var deletingLine: TranscriptLine? by mutableStateOf(null)

    var isSending: Boolean by mutableStateOf(false)
        private set
    var uploadingFileIds: Set<String> by mutableStateOf(emptySet())
        private set
    var uploadedFileIds: Set<String> by mutableStateOf(emptySet())
        private set

    /** Something that went wrong, for a snackbar; the screen clears it once it has been said. */
    var problemText: String? by mutableStateOf(null)

    /** The turns running or waiting, oldest first: the stop button stops the first. */
    var runningRunIds: List<String> by mutableStateOf(emptyList())
        private set
    val runningRunId: String? get() = runningRunIds.firstOrNull()

    /** Changes when something is sent or a card is settled, for the haptic. */
    var sentCount: Int by mutableStateOf(0)
        private set

    /** Counts the cards settled from this app as soon as they were answered, which shrink the list under the scroll. */
    var cardSettledCount: Int by mutableStateOf(0)
        private set

    /** For a chat: which of its members the person is, who is writing in it now, and the newest message told to the server as read. */
    var ownUserId: String? by mutableStateOf(environment.ownUserId)
        private set
    private val typing = ChatTyping()

    /** Changes when somebody starts or stops typing, so the line that says so is drawn again. */
    var typingVersion: Int by mutableStateOf(0)
        private set
    private var typingToldAtMS = 0L
    private var readThroughMessageId = ""
    private var isOwnUserIdConfirmed = false
    private val answeredLocationCalls = mutableSetOf<String>()

    /** An agent asked where the person is: the screen asks for the permission if it must, then answers. */
    var locationRequestEvent: AgentEvent? by mutableStateOf(null)
        private set

    val isChat: Boolean get() = conversation?.isChat == true
    val chat: ChatSummary? get() = conversation?.chat

    /** The server's voice: the rate it takes speech at, null while it has not said yes. */
    var voiceSampleRate: Int? by mutableStateOf(null)
        private set

    /** Whether the server prepares and transcribes voice messages in chats. */
    var isVoiceMessageAvailable: Boolean by mutableStateOf(false)
        private set

    /** Dictating needs the server's voice, in a chat as anywhere. */
    val canDictate: Boolean get() = voiceSampleRate != null

    /** A recording that could not be sent, kept to send again. */
    var failedRecording: VoiceRecording? by mutableStateOf(null)
        private set

    /** Why a voice message or dictation did not go, with the way to try again beside it, for the snackbar. */
    var voiceProblemText: String? by mutableStateOf(null)

    init {
        title = conversation?.let { found -> titleOf(found) } ?: environment.agentTitle
        draft = environment.draftStore.read(draftConversationId)
        conversation?.let { found -> environment.remember(found) }
    }

    private fun titleOf(found: AgentConversation): String = titleOfConversation(found, environment.agentTitle, ownUserId, words)

    fun describe(error: Throwable): String = environment.session.describe(error)

    /** Stops following and lets go of everything this holds. */
    fun close() {
        followJob?.cancel()
        scope.cancel()
    }

    // MARK: Following

    /**
     * Loads the conversation and follows its feed until the screen is left, loading again and following anew whenever
     * the feed drops. The screen starts it when it comes to the front again.
     */
    fun startFollowing() {
        followJob?.cancel()
        followJob = scope.launch { follow() }
    }

    fun stopFollowing() {
        followJob?.cancel()
        followJob = null
    }

    private suspend fun follow() {
        var delayMS = 1_000L
        while (true) {
            try {
                load(isFeedReplaying = true)
                val id = conversationId ?: return
                ConversationFeed.events(connection, id).collect { event ->
                    apply(event)
                    delayMS = 1_000L
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                if (!isLoaded) loadProblemText = describe(error)
            }
            delay(delayMS)
            delayMS = minOf(delayMS * 2, 10_000L)
        }
    }

    /**
     * Reads the conversation's newest messages and open cards. When the feed is about to be opened anew, a turn still
     * running is replayed by it, which says it runs again, and one that ended while the feed was down is not; the
     * turns running and the words of an answer being written are then forgotten. Read again while the feed is up (a
     * member changed, a message was too long to travel whole) they are kept, since nothing will say them again.
     */
    suspend fun load(isFeedReplaying: Boolean = false) {
        val page = connection.conversation(conversationId, first = maxOf(100, messages.size))
        if (page.conversation?.isChat == true && !isOwnUserIdConfirmed) {
            runCatching { connection.userId() }.getOrNull()?.let { userId ->
                isOwnUserIdConfirmed = true
                if (userId != ownUserId) ownUserId = userId
            }
        }
        page.conversation?.let { found ->
            conversationId = found.id
            conversation = found
            environment.remember(found)
            title = titleOf(found)
        }
        messages = page.messages
        interactions = page.interactions
        storedMessageCount = page.messages.size
        total = page.total
        todos = page.todos
        actingAs = page.actingAs
        val fresh = Transcript(messages, interactions, isChat = isChat)
        if (isFeedReplaying) {
            runningRunIds = emptyList()
        } else {
            fresh.keepStreaming(current)
        }
        current = fresh
        publish()
        isLoaded = true
        loadProblemText = null
        if (isChat) {
            markRead()
            loadVoiceAvailability()
            return
        }
        if (messages.isEmpty()) loadIdeas()
        loadVoiceAvailability()
    }

    private fun publish() {
        lines = current.lines
    }

    /** Reads whether the server offers calls, dictation and voice messages, until it has said yes. */
    private suspend fun loadVoiceAvailability() {
        if (voiceSampleRate != null && isVoiceMessageAvailable) return
        val voice = runCatching { connection.voiceAvailability() }.getOrNull() ?: return
        if (voice.isVoiceAvailable) voiceSampleRate = voice.sampleRate ?: 24_000
        isVoiceMessageAvailable = voice.isVoiceMessageAvailable ?: false
    }

    /** Reads the page of messages before those shown. */
    fun loadEarlier() {
        if (earlierCount <= 0 || isLoadingEarlier) return
        isLoadingEarlier = true
        scope.launch {
            try {
                val page = connection.conversation(conversationId, first = 100, offset = messages.size)
                val known = messages.map { message -> message.id }.toSet()
                val earlierMessages = page.messages.filter { message -> message.id !in known }
                messages = earlierMessages + messages
                storedMessageCount = messages.size
                total = page.total
                current.prepend(messages, earlierMessages.size, isChat)
                publish()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                problemText = describe(error)
            } finally {
                isLoadingEarlier = false
            }
        }
    }

    private suspend fun loadIdeas() {
        ideas = runCatching { connection.ideas(languageCode()) }.getOrNull()?.take(3) ?: emptyList()
        if (ideas.isNotEmpty()) runCatching { connection.markIdeasShown(ideas.map { idea -> idea.id }) }
    }

    private fun languageCode(): String = java.util.Locale.getDefault().language.let { language ->
        when (language) {
            "zh" -> "zh"
            "ja" -> "ja"
            else -> "en"
        }
    }

    // MARK: Events

    /** Told of every event of the conversation's feed; the calls part follows a turn with it (see `ConversationCalls.kt`). */
    var onEvent: ((AgentEvent) -> Unit)? = null

    private fun apply(event: AgentEvent) {
        current.apply(event)
        publish()
        onEvent?.invoke(event)
        when (event.kind) {
            "asked" -> if (event.runId !in runningRunIds) runningRunIds = runningRunIds + event.runId
            "done", "error" -> {
                runningRunIds = runningRunIds.filter { runId -> runId != event.runId }
                val errorText = event.error
                if (event.kind == "error" && !errorText.isNullOrEmpty()) problemText = errorText.asReasonSentence()
                // The stored turn replaces the one drawn as it arrived: its usage, its ids for replying, a coding session's question card.
                scope.launch { runCatching { load() } }
            }
            "tool_result" -> if (event.tool == "todo") {
                scope.launch { runCatching { connection.conversation(conversationId, first = 1) }.getOrNull()?.let { page -> todos = page.todos } }
            }
            "said", "edited" -> {
                // A message too long to travel whole is read again with the chat.
                val message = event.message
                if (message == null || event.isMessageCut == true) {
                    scope.launch { runCatching { load() } }
                } else if (event.kind == "said") {
                    // Whoever said it has finished: a person is no longer typing, and an agent that answered is no longer thinking.
                    if (message.authorKind == "agent") {
                        message.authorAgentId?.takeIf { id -> id.isNotEmpty() }?.let { id -> typing.clearAgent(id) }
                    } else {
                        message.authorUserId?.takeIf { id -> id.isNotEmpty() }?.let { id -> typing.clear(id) }
                    }
                    typingVersion += 1
                    scope.launch { markRead() }
                }
            }
            // Somebody was added, taken out or left, or changed what their agent does here: the members are read again with the chat.
            "members" -> scope.launch { runCatching { load() } }
            "read" -> {
                val userId = event.userId
                val messageId = event.messageId
                val found = conversation
                val summary = found?.chat
                if (userId != null && messageId != null && summary != null) conversation = found.copy(chat = summary.withRead(userId, messageId))
            }
            "typing" -> {
                // An agent working on an answer is told by its agent and is thinking; a person is told by theirs and is typing.
                val agentId = event.authorAgentId
                val userId = event.userId
                if (!agentId.isNullOrEmpty()) {
                    typing.noteAgent(agentId, event.name.orEmpty(), event.authorOwnerName.orEmpty())
                } else if (userId != null && userId != ownUserId) {
                    typing.note(userId, event.name.orEmpty())
                } else {
                    return
                }
                typingVersion += 1
                typingPruning?.cancel()
                typingPruning = scope.launch {
                    delay(ChatTyping.SHOWN_MS + 500)
                    typing.prune()
                    typingVersion += 1
                }
            }
            "titled" -> event.text?.takeIf { text -> text.isNotEmpty() }?.let { text -> title = text }
            "locate" -> askForLocation(event)
        }
    }

    /** Who is typing now, and which agents are thinking, for the line under the messages. */
    fun typingNames(): List<String> = typing.names()

    fun thinkingNames(): List<String> = typing.agentSpeakers(ownedWords = words::agentOf)

    // MARK: Writing

    fun updateDraft(text: String) {
        if (text == draft) return
        draft = text
        environment.draftStore.write(draftKeyId(), text)
        tellTyping()
    }

    private fun draftKeyId(): String? = draftConversationId

    fun addPendingFile(file: PendingFile) {
        pendingFiles = pendingFiles + file
    }

    fun removePendingFile(fileId: String) {
        pendingFiles = pendingFiles.filter { file -> file.id != fileId }
    }

    /** The message replied to, of the references: one at most. */
    val replyingTo: AgentReference? get() = references.firstOrNull { reference -> reference.agentMessageId != null }

    fun setReplyingTo(reply: AgentReference?) {
        val kept = references.filter { reference -> reference.agentMessageId == null }
        references = if (reply != null) listOf(reply) + kept else kept
    }

    /** Adds something asked about, once. */
    fun refer(reference: AgentReference) {
        if (reference !in references) references = references + reference
    }

    fun dropReference(reference: AgentReference) {
        references = references.filter { candidate -> candidate != reference }
    }

    val canSend: Boolean get() = canWrite && !isSending && (draft.isNotBlank() || pendingFiles.isNotEmpty())

    /**
     * Whether the person can still write here: always, but in a chat they have left, or a direct chat with somebody
     * they no longer share a circle with, which they can read and not write to.
     */
    val canWrite: Boolean
        get() {
            val summary = chat
            if (!isChat || summary == null) return true
            return !summary.isRemoved && !(summary.chatKind == ChatKind.Direct.wireName && summary.otherMembers(ownUserId).any { member -> member.isRemoved })
        }

    /** The question that typing answers: the newest unanswered one, while its turn runs or for an hour after it was asked. */
    val freshQuestion: TranscriptLine?
        get() {
            val question = lines.lastOrNull { line -> (line.kind == TranscriptLine.Kind.Question || line.kind == TranscriptLine.Kind.CodingQuestion) && !line.isDone } ?: return null
            if (question.runId in runningRunIds || question.kind == TranscriptLine.Kind.CodingQuestion) return question
            val at = ServerTime.instant(question.at) ?: return question
            return if (Instant.now().epochSecond - at.epochSecond < CARD_FRESH_SECONDS) question else null
        }

    /** The suggested replies to offer now. */
    val offeredSuggestions: List<String> get() = current.offeredSuggestions

    /** Puts the person's reaction on a message, or takes it away when it is the one already there. It shows at once, and goes back if the server refuses it. */
    fun react(line: TranscriptLine, emoji: String) {
        val messageId = line.messageId ?: return
        val previous = line.reaction("person", ownUserId).orEmpty()
        val chosen = if (previous == emoji) "" else emoji
        current.react(messageId, chosen, "person", ownUserId)
        publish()
        scope.launch {
            try {
                connection.react(messageId, chosen)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                current.react(messageId, previous, "person", ownUserId)
                publish()
                problemText = describe(error)
            }
        }
    }

    /**
     * Sends what is typed: an answer to the question waiting, when one is fresh, or a new message. In a chat typing
     * never answers a card: the words are for the people in it.
     */
    fun send() {
        val text = draft.trim()
        if (!canSend) return
        scope.launch { sendText(text) }
    }

    private suspend fun sendText(text: String) {
        val question = freshQuestion
        if (!isChat && question != null && pendingFiles.isEmpty() && references.isEmpty()) {
            isSending = true
            try {
                updateDraft("")
                val isAnswered = if (question.kind == TranscriptLine.Kind.CodingQuestion) {
                    answerCodingQuestion(question, emptyList(), text)
                } else {
                    answer(question, text)
                }
                if (!isAnswered && draft.isEmpty()) updateDraft(text)
            } finally {
                isSending = false
            }
            return
        }
        if (pendingFiles.sumOf { file -> file.data.size } > PendingFile.BYTES_MOST) {
            problemText = environment.context.getString(com.teanode.app.R.string.chats_files_too_large)
            return
        }
        val sending = pendingFiles
        val sendingIds = sending.map { file -> file.id }.toSet()
        isSending = true
        uploadingFileIds = sendingIds
        uploadedFileIds = emptySet()
        val reply = replyingTo
        val referring = references
        // In a chat, which agents the message is for: those chosen from the @ whose names are still in it, and the agent of a message it replies to.
        val addressed = addressedAgentIds(text)
        // And whom it mentions: the people named after an @, and everybody. Mentioning a person asks nothing of their agent.
        val mentioned = mentionedPeople(text)
        val choosing = chosenAgents
        updateDraft("")
        references = emptyList()
        chosenAgents = emptyList()
        var sentLineId: String? = null
        try {
            val attachments = mutableListOf<AgentAttachment>()
            for (file in sending) {
                val id = connection.upload(file.name, file.contentType, file.data)
                attachments.add(AgentAttachment(id, file.name, file.contentType, file.data.size))
                uploadedFileIds = uploadedFileIds + file.id
            }
            // Files added while these were sent stay for the next message.
            pendingFiles = pendingFiles.filter { file -> file.id !in sendingIds }
            // A chat takes files with no words; a turn with the agent needs something to read.
            val message = if (text.isEmpty() && !isChat) "(files)" else text
            sentLineId = current.addSent(message, attachments, reply, ownAuthor())
            publish()
            val asked = connection.ask(
                message, conversationId, environment.drawerId, attachmentIds = attachments.map { attachment -> attachment.id }, references = referring,
                addressedAgentIds = addressed, mentionedUserIds = mentioned.userIds, isAllMentioned = mentioned.isAllMentioned,
            )
            // A message in a chat comes back as the chat's own, and if it does not, the chat is read again so that nothing sent looks unsent for long.
            if (isChat) sentLineId.let { lineId -> confirmSoon(lineId) }
            if (asked.runId.isNotEmpty() && asked.runId !in runningRunIds) runningRunIds = runningRunIds + asked.runId
            sentCount += 1
            if (conversationId == null) conversationId = asked.conversationId
            ideas = emptyList()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            sentLineId?.let { lineId -> current.withdrawSent(lineId) }
            publish()
            if (draft.isEmpty()) {
                updateDraft(text)
                chosenAgents = choosing
            }
            pendingFiles = sending + pendingFiles.filter { file -> file.id !in sendingIds }
            references = referring
            problemText = describe(error)
        } finally {
            isSending = false
            uploadingFileIds = emptySet()
            uploadedFileIds = emptySet()
        }
    }

    /** Sends a suggested reply as it is, leaving the draft alone. */
    fun sendSuggestion(suggestion: String) {
        scope.launch { sendLine(suggestion) }
    }

    private suspend fun sendLine(text: String) {
        val sentLineId = current.addSent(text)
        publish()
        try {
            val asked = connection.ask(text, conversationId, environment.drawerId)
            if (asked.runId !in runningRunIds) runningRunIds = runningRunIds + asked.runId
            sentCount += 1
            if (conversationId == null) conversationId = asked.conversationId
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            current.withdrawSent(sentLineId)
            publish()
            problemText = describe(error)
        }
    }

    // MARK: Voice messages

    /** The microphone for a voice message, held by the button in the composer. */
    val voiceRecorder: VoiceMessageRecorder = VoiceMessageRecorder(environment.context)

    /** The finger went down on the button: starts recording. */
    fun beginVoiceMessage() {
        if (!isChat || !canWrite || isSending) return
        voiceRecorder.onLongest = { endVoiceMessage(isCancelled = false) }
        voiceRecorder.start()
        voiceRecorder.problemText?.let { text ->
            voiceProblemText = text
            voiceRecorder.problemText = null
        }
    }

    /** The finger came up: sends the recording, or throws it away when the finger had slid up to cancel. */
    fun endVoiceMessage(isCancelled: Boolean) {
        if (!voiceRecorder.isRecording) return
        if (isCancelled) {
            voiceRecorder.cancel()
            return
        }
        val recording = voiceRecorder.finish() ?: return
        scope.launch { sendVoiceMessage(recording) }
    }

    /** Sends a recording as a voice message: the file is uploaded as one, then the chat is asked to say it. A message that fails keeps its recording, to send again. */
    suspend fun sendVoiceMessage(recording: VoiceRecording) {
        if (isSending) return
        failedRecording = null
        voiceProblemText = null
        isSending = true
        val reply = replyingTo
        val referring = references
        var sentLineId: String? = null
        try {
            val data = recording.file.readBytes()
            val id = connection.upload(VOICE_MESSAGE_NAME, "audio/mp4", data, isVoiceMessage = true)
            val attachment = AgentAttachment(id, VOICE_MESSAGE_NAME, "audio/mp4", data.size, audioDurationMS = (recording.durationSeconds * 1000).toLong())
            references = emptyList()
            sentLineId = current.addSent("", listOf(attachment), reply, ownAuthor(), isVoiceMessage = true)
            publish()
            val asked = connection.ask("", conversationId, environment.drawerId, references = referring, voiceMessageAttachmentId = id)
            confirmSoon(sentLineId)
            if (asked.runId.isNotEmpty() && asked.runId !in runningRunIds) runningRunIds = runningRunIds + asked.runId
            sentCount += 1
            if (conversationId == null) conversationId = asked.conversationId
            recording.file.delete()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            sentLineId?.let { lineId -> current.withdrawSent(lineId) }
            publish()
            references = referring
            failedRecording = recording
            voiceProblemText = describe(error)
        } finally {
            isSending = false
        }
    }

    /** Sends the recording that failed once more. */
    fun retryVoiceMessage() {
        val failed = failedRecording ?: return
        scope.launch { sendVoiceMessage(failed) }
    }

    /** Throws away the recording that failed. */
    fun discardFailedRecording() {
        failedRecording?.file?.delete()
        failedRecording = null
    }

    /** Starts one of the agent's ideas: what it opens with is sent at once. */
    fun start(idea: AgentIdea, onOpenConversation: (String) -> Unit) {
        scope.launch {
            try {
                val (openedId, openingRequest) = connection.startIdea(idea.id, conversationId, languageCode())
                ideas = emptyList()
                if (openedId != null && openedId != conversationId) {
                    onOpenConversation(openedId)
                    return@launch
                }
                if (!openingRequest.isNullOrEmpty()) sendLine(openingRequest)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                problemText = describe(error)
            }
        }
    }

    /** Replies to a line: its words are quoted with what is sent next. */
    fun reply(line: TranscriptLine, voiceMessageWords: String) {
        val messageId = line.messageId ?: return
        // In a chat the person's own words are the user's, and everybody else's are what an answer is in a conversation with an agent.
        val isOwn = line.author?.isOwnWords ?: (line.kind == TranscriptLine.Kind.Person)
        val quoted = if (line.isVoiceMessage) line.transcriptText.ifEmpty { voiceMessageWords } else line.text
        setReplyingTo(AgentReference.reply(messageId, if (isOwn) "user" else "assistant", quoted.take(1000)))
    }

    /** Who wrote the message a reply quotes, named as the chat names them ("Ada", "Ada's agent"), or null when it is not a chat's message. */
    fun replyAuthorName(reply: AgentReference): String? {
        val messageId = reply.agentMessageId ?: return null
        val author = lines.firstOrNull { line -> line.messageId == messageId }?.author ?: return null
        return if (author.isDeletedAccount) author.authorName else author.speakerName(words::agentOf)
    }

    // MARK: Chats

    /** The agents chosen from the @ for the message being written. */
    var chosenAgents: List<AddressableAgent> by mutableStateOf(emptyList())
        private set

    /** The agents an @ offers in this chat: the person's own, and those of members who let the others ask. */
    val addressableAgents: List<AddressableAgent>
        get() {
            val summary = chat
            if (!isChat || summary == null || summary.isRemoved) return emptyList()
            return summary.addressableAgents(ownUserId, words.deletedAccountNote)
        }

    /** What an @ offers in this chat: the person's own agent, everybody in a group, and each of the others followed by their agent when it may be asked. */
    val mentionChoices: List<MentionChoice>
        get() {
            val summary = chat
            if (!isChat || summary == null || summary.isRemoved) return emptyList()
            return summary.mentionChoices(ownUserId, words.deletedAccountNote)
        }

    /** The choices the @ at the end of the words offers, those the letters typed so far match; none when no @ is being typed. */
    val mentionSuggestions: List<MentionChoice>
        get() {
            if (!isChat) return emptyList()
            val query = ChatMention.query(draft) ?: return emptyList()
            return ChatMention.matching(mentionChoices, query, words.allMentionWord)
        }

    /** Puts the chosen word where the @ is, and keeps a chosen agent for the message. */
    fun choose(mention: MentionChoice) {
        updateDraft(ChatMention.choosing(mention, draft, words.allMentionWord))
        if (mention is MentionChoice.Agent && chosenAgents.none { agent -> agent.agentId == mention.agent.agentId }) {
            chosenAgents = chosenAgents + mention.agent
        }
    }

    /** The words a message of the chat mentions people by, for its bubble to mark; none outside a chat. */
    fun mentionWordsOf(line: TranscriptLine): List<ChatMentionWord> {
        if (!isChat || (line.mentionedUserIds.isEmpty() && !line.isAllMentioned)) return emptyList()
        return line.mentionWords(chat, ownUserId, words.deletedAccountNote)
    }

    private fun mentionedPeople(text: String): ChatMention.Mentioned {
        val summary = chat
        if (!isChat || summary == null) return ChatMention.Mentioned(emptyList(), false)
        return ChatMention.mentioned(text, summary, ownUserId, words.deletedAccountNote, chosenAgents)
    }

    private fun addressedAgentIds(text: String): List<String> {
        if (!isChat) return emptyList()
        var repliedToAgentId: String? = null
        val replied = replyingTo?.agentMessageId
        if (replied != null) {
            val author = lines.firstOrNull { line -> line.messageId == replied }?.author
            if (author != null && author.isAgent) repliedToAgentId = author.authorAgentId
        }
        return ChatMention.addressed(text, chosenAgents, repliedToAgentId)
    }

    /**
     * Makes the person's agent remember this chat from now on, or stop. The switch moves at once and goes back if the
     * server refuses; the refusal is returned for the members sheet to say.
     */
    suspend fun setAgentReading(isReading: Boolean): String? {
        val id = conversationId ?: return null
        val userId = ownUserId ?: return null
        val before = chat?.member(userId) ?: return null
        replaceChat { summary -> summary.withAgentSwitches(userId, isAgentReading = isReading) }
        return changeChatReporting(isReportedOnScreen = false, undo = { replaceChat { summary -> summary.withAgentSwitches(userId, isAgentReading = before.isAgentReading) } }) {
            connection.setChatAgentReading(id, isReading)
        }
    }

    /** Lets the others in this chat address the person's agent, or stops them, as [setAgentReading] does. */
    suspend fun setAgentAskableByOthers(isAskable: Boolean): String? {
        val id = conversationId ?: return null
        val userId = ownUserId ?: return null
        val before = chat?.member(userId) ?: return null
        replaceChat { summary -> summary.withAgentSwitches(userId, isAgentAskableByOthers = isAskable) }
        return changeChatReporting(
            isReportedOnScreen = false,
            undo = { replaceChat { summary -> summary.withAgentSwitches(userId, isAgentAskableByOthers = before.isAgentAskableByOthers ?: false) } },
        ) { connection.setChatAgentAskableByOthers(id, isAskable) }
    }

    private fun replaceChat(change: (ChatSummary) -> ChatSummary) {
        val found = conversation ?: return
        val summary = found.chat ?: return
        conversation = found.copy(chat = change(summary))
    }

    /** The person as the author of a message they have just sent in a chat, until the chat says it was said; null outside a chat. */
    private fun ownAuthor(): ChatAuthor? = if (isChat) ChatAuthor("person", authorUserId = ownUserId.orEmpty(), authorName = "", isMine = true) else null

    /** Reads the chat again if a message just sent has not come back as the chat's own within a few seconds. */
    private fun confirmSoon(lineId: String) {
        scope.launch {
            delay(3_000)
            if (lines.any { line -> line.id == lineId }) runCatching { load() }
        }
    }

    /**
     * Tells the server the person has read the chat through its newest message, which moves the mark the others see
     * and clears the count in the list. Only while the screen is following the chat.
     */
    suspend fun markRead() {
        val id = conversationId ?: return
        if (!isChat) return
        val newest = current.newestChatMessageId ?: return
        if (newest <= readThroughMessageId) return
        val previous = readThroughMessageId
        readThroughMessageId = newest
        try {
            connection.markChatRead(id, newest)
            val found = conversation
            val summary = found?.chat
            if (summary != null) conversation = found.copy(chat = summary.copy(unreadCount = 0))
            environment.refreshChats()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (_: Throwable) {
            readThroughMessageId = previous
        }
    }

    /** Lets the others know words are being written, at most every few seconds however fast the keys. */
    private fun tellTyping() {
        val id = conversationId ?: return
        if (!isChat || draft.isBlank()) return
        val now = System.currentTimeMillis()
        if (now - typingToldAtMS < ChatTyping.SENT_EVERY_MS) return
        typingToldAtMS = now
        scope.launch { runCatching { connection.setChatTyping(id) } }
    }

    /** Whether the person may take a message back: their own, or any in a group they are an admin of. */
    fun canDelete(line: TranscriptLine): Boolean {
        if (line.author == null || line.messageId == null || line.isDeleted || line.isBeingSent) return false
        return line.author?.isOwnWords == true || (chat?.isGroup == true && chat?.isAdmin(ownUserId) == true)
    }

    /** Whether the person may change what a message says: their own words, but not a voice message, which is taken back and said again. */
    fun canEdit(line: TranscriptLine): Boolean =
        line.author?.isOwnWords == true && line.messageId != null && !line.isDeleted && !line.isBeingSent && !line.isVoiceMessage

    fun edit(line: TranscriptLine, text: String) {
        val wordsOfText = text.trim()
        val messageId = line.messageId ?: return
        if (wordsOfText.isEmpty() || wordsOfText == line.text) return
        scope.launch { changeChat { connection.editChatMessage(messageId, wordsOfText) } }
    }

    fun delete(line: TranscriptLine) {
        val messageId = line.messageId ?: return
        scope.launch { changeChat { connection.deleteChatMessage(messageId) } }
    }

    /** Asks for the words of a voice message that has none, paid by the person's agent; they come back as an edit of the message. */
    suspend fun transcribe(line: TranscriptLine) {
        val messageId = line.messageId ?: return
        if (!line.isVoiceMessage) return
        changeChat { connection.transcribeChatVoiceMessage(messageId) }
    }

    fun rename(title: String) {
        val id = conversationId ?: return
        scope.launch { changeChat { connection.renameChat(id, title) } }
    }

    /** Chooses what the person's phone is told of in this chat. */
    fun setNotificationLevel(level: ChatNotificationLevel) {
        val id = conversationId ?: return
        scope.launch { changeChat { connection.setChatNotificationLevel(id, level) } }
    }

    /** Puts people in the group; a refusal is thrown for the sheet that asked to say. */
    suspend fun addMembers(userIds: List<String>) {
        val id = conversationId ?: return
        if (userIds.isEmpty()) return
        connection.addChatMembers(id, userIds)
        runCatching { load() }
    }

    /** Takes somebody out of the group; the refusal is returned for the members sheet to say, since the screen under it is not in view. */
    suspend fun removeMember(member: com.teanode.kit.chats.ChatMember): String? {
        val id = conversationId ?: return null
        return changeChatReporting(isReportedOnScreen = false, undo = null) { connection.removeChatMember(id, member.userId) }
    }

    /** Leaves the group; false when the server refused. */
    suspend fun leave(): Boolean {
        val id = conversationId ?: return false
        return try {
            connection.leaveChat(id)
            environment.refreshChats()
            true
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            problemText = describe(error)
            false
        }
    }

    private suspend fun changeChat(change: suspend () -> Unit) {
        changeChatReporting(isReportedOnScreen = true, undo = null, change = change)
    }

    /**
     * Makes a change to the chat and reads it again, so what the server now has is what shows. A refusal is told as
     * any send problem is, or, when the change was made from a sheet that covers the screen, only returned, for the
     * sheet to say; `undo` first puts back what was shown ahead of the answer.
     */
    private suspend fun changeChatReporting(isReportedOnScreen: Boolean, undo: (() -> Unit)?, change: suspend () -> Unit): String? {
        var failure: String? = null
        try {
            change()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            undo?.invoke()
            failure = describe(error)
            if (isReportedOnScreen) problemText = failure
        }
        runCatching { load() }
        return failure
    }

    // MARK: Cards

    fun approve(line: TranscriptLine, isApproved: Boolean) {
        scope.launch {
            try {
                if (connection.resolveConfirmation(line.runId, line.callId, isApproved)) {
                    current.settle(line.id, if (isApproved) "approved" else "declined")
                    publish()
                    sentCount += 1
                } else {
                    current.remove(line.id)
                    publish()
                    problemText = environment.context.getString(com.teanode.app.R.string.chats_card_already_answered)
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                problemText = describe(error)
            }
        }
    }

    fun answerCard(line: TranscriptLine, answerText: String) {
        scope.launch { answer(line, answerText) }
    }

    suspend fun answer(line: TranscriptLine, answerText: String): Boolean {
        try {
            if (connection.answerQuestion(line.runId, line.callId, answerText)) {
                val resolution = if (answerText == CHAT_ABOUT_IT) environment.context.getString(com.teanode.app.R.string.chats_chatting_about_it) else answerText
                current.settle(line.id, resolution)
                publish()
                sentCount += 1
                return true
            }
            current.remove(line.id)
            publish()
            problemText = environment.context.getString(com.teanode.app.R.string.chats_card_already_answered)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            problemText = describe(error)
        }
        return false
    }

    fun answerCoding(line: TranscriptLine, options: List<CodingQuestion.Option>, freeText: String?) {
        scope.launch { answerCodingQuestion(line, options, freeText) }
    }

    /** Answers a coding session's question with the options chosen, or with what to type into it. */
    suspend fun answerCodingQuestion(line: TranscriptLine, options: List<CodingQuestion.Option>, freeText: String?): Boolean {
        // The card as it is now, not as the view last drew it: a second tap before the view caught up, or an answer from the computer, finds it settled already.
        val question = line.codingQuestion ?: return false
        if (current.lines.firstOrNull { candidate -> candidate.id == line.id }?.isDone != false) return false
        // The card shows the answer as soon as it is chosen: typing it into the session at the computer takes a few seconds, and a card that stays as it was looks like a tap that did nothing.
        val chosenWords = freeText ?: options.joinToString(", ") { option -> option.optionLabel }
        current.settleAwaiting(line.id, chosenWords)
        publish()
        cardSettledCount += 1
        try {
            val answeredWith = connection.answerCodingQuestion(question, options, freeText)
            current.settle(line.id, answeredWith.ifEmpty { chosenWords })
            publish()
            sentCount += 1
            return true
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (refused: TeaNodeError.Refused) {
            if (refused.reason.contains("already answered")) {
                // Answered at the computer, or by another device: the card folds away so typing goes to the conversation again.
                current.settle(line.id, "")
            } else {
                current.reopen(line.id)
                problemText = describe(refused)
            }
            publish()
        } catch (error: Throwable) {
            current.reopen(line.id)
            publish()
            problemText = describe(error)
        }
        return false
    }

    fun stop() {
        val runId = runningRunId ?: return
        scope.launch {
            try {
                connection.stopRun(runId)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                problemText = describe(error)
            }
        }
    }

    // MARK: Location

    private fun askForLocation(event: AgentEvent) {
        val callId = event.callId ?: return
        if (event.drawerId != environment.drawerId || callId in answeredLocationCalls) return
        answeredLocationCalls.add(callId)
        locationRequestEvent = event
    }

    /** Answers the agent asking where the person is, once the screen has a reading or a reason there is none. */
    fun answerLocation(event: AgentEvent, reading: LocationReading) {
        locationRequestEvent = null
        val callId = event.callId ?: return
        scope.launch {
            runCatching {
                connection.answerLocation(
                    event.runId, callId, reading.latitudeDegrees, reading.longitudeDegrees, reading.accuracyMeters,
                    reading.measuredAt, reading.problemText,
                )
            }
        }
    }

    companion object {
        /** How recent a question must be for typing to answer it rather than start a turn: one from an hour ago is not what the person is answering. */
        const val CARD_FRESH_SECONDS = 60 * 60

        /** What a question card answers when the person would rather talk than pick (askuser.ChatAboutIt on the server), the same in every language. */
        const val CHAT_ABOUT_IT = "[chat about it]"

        /** The reactions offered first when a message is held, as Messages offers its own. */
        val quickReactions = listOf("👍", "❤️", "😂", "😮", "😢", "🙏", "🔥", "🎉")

        private const val VOICE_MESSAGE_NAME = "Voice message.m4a"

        /** The name a conversation is shown under. */
        fun titleOfConversation(found: AgentConversation, agentTitle: String, ownUserId: String?, words: ChatWords): String {
            if (found.isMain) return agentTitle
            if (found.isChat) {
                val fallback = found.title?.takeIf { text -> text.isNotEmpty() } ?: words.chatFallbackTitle
                return found.chat?.name(ownUserId, words.deletedAccountNote, fallback) ?: fallback
            }
            val named = found.title
            if (!named.isNullOrEmpty()) return named
            return words.newConversationTitle
        }
    }
}

/** A place read from the phone, or why there is none. */
class LocationReading(
    val latitudeDegrees: Double? = null,
    val longitudeDegrees: Double? = null,
    val accuracyMeters: Double? = null,
    val measuredAt: Instant? = null,
    val problemText: String? = null,
)
