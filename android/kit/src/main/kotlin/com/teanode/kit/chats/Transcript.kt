package com.teanode.kit.chats

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.stringValue
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import java.time.Duration
import java.util.UUID

/** TranscriptLine is one line of a conversation as the app shows it. */
data class TranscriptLine(
    val id: String,
    val kind: Kind,
    val text: String,
    /** For a tool, its name; for a check-in, its marker; for a note, its kind. */
    val detail: String = "",
    val isStreaming: Boolean = false,
    /** For a tool, whether it finished; for a card, whether it was settled. */
    val isDone: Boolean = false,
    val runId: String = "",
    val callId: String = "",
    val choices: List<String> = emptyList(),
    /** How a card was settled ("approved", "declined", or the answer given); for a tool, the short note it ended with. */
    val resolution: String = "",
    val attachments: List<AgentAttachment> = emptyList(),
    val at: String? = null,
    /** For a coding session's question, the question whole. */
    val codingQuestion: CodingQuestion? = null,
    /** The stored message the line is, which a reply points at; null for a line still arriving. */
    val messageId: String? = null,
    /** On the last answer of a turn, what the whole turn cost. */
    val usage: AgentUsage? = null,
    /** What a person's message points at, besides a reply. */
    val references: List<AgentReference> = emptyList(),
    /** What a person's message replies to: an earlier message, or the question a late answer answers. */
    val quotedText: String = "",
    /** "user", "assistant" or "question": whose words quotedText are. */
    val quotedRole: String = "",
    /** For an approval, how risky the call is; "destructive" is drawn red. */
    val risk: String = "",
    /** For an answer, the replies it suggests, shown as buttons. */
    val suggestions: List<String> = emptyList(),
    /**
     * For an answer still arriving, everything that has come, which is more than `text` shows while the hidden
     * suggestions line is being written: its characters come one by one, and none of them are for the person.
     */
    val streamedText: String = "",
    /** For a tool, what it was called with. */
    val arguments: String = "",
    /** For a note, whether it is waiting on something still to happen (a turn queued behind another, the conversation being folded). */
    val isPending: Boolean = false,
    /** The person's and the agent's reactions to it, one each at most; in a chat, one for each person who reacted. */
    val reactions: List<AgentMessageReaction> = emptyList(),
    /** Who said it, for a message of a chat; null on every other line. */
    val author: ChatAuthor? = null,
    /** For a message of a chat: when its author last changed what it says, and whether they took it back (its text is then empty). */
    val editedAt: String? = null,
    val isDeleted: Boolean = false,
    /** For a message of a chat, the members it mentions by account, and whether it mentions everybody. */
    val mentionedUserIds: List<String> = emptyList(),
    val isAllMentioned: Boolean = false,
    /** For a message of a chat that is a recording the person spoke: its words, and whether they are "waiting", "done", "failed" or "off". */
    val isVoiceMessage: Boolean = false,
    val transcriptText: String = "",
    val transcriptState: String = "",
    /** For a message of a chat that is the line of a call: the call, as the message carried it. It has no words. */
    val chatCallId: String = "",
    val chatCall: ChatCallRecord? = null,
) {
    enum class Kind {
        /** What the person said. */
        Person,

        /** What the agent answered. */
        Answer,

        /** A tool the agent called. */
        Tool,

        /** A turn the agent took on its own: an alert, a goal, a coding session's question. Nobody typed it. */
        CheckIn,

        /** Something that happened: the turn was stopped, the conversation was folded into a note, a goal changed. */
        Note,

        /** A tool call waiting for the person's approval. */
        Approval,

        /** A question the agent asked the person. */
        Question,

        /** A question one of the person's coding sessions asked, with its options. */
        CodingQuestion,
    }

    /** Whether the message is the line of a call, which is drawn from the call and not as words. */
    val isCallLine: Boolean get() = chatCallId.isNotEmpty()

    /** The recording of a voice message: its one file. */
    val voiceAttachment: AgentAttachment? get() = if (isVoiceMessage) attachments.firstOrNull() else null

    /** The words to copy, quote or share: for a voice message its transcript, which is empty until it is made. */
    val readableText: String get() = if (isVoiceMessage) transcriptText else text

    /** Whether this is a message the app has sent and the server has not yet confirmed: it has no stored id to act on. */
    val isBeingSent: Boolean get() = id.startsWith(Transcript.SENT_PREFIX)

    /** The emoji one side put on it, if any. */
    fun reaction(reactorKind: String): String? = reactions.firstOrNull { reaction -> reaction.reactorKind == reactorKind }?.emoji

    /**
     * The emoji one reactor put on it. With the person's id, a person's reaction is theirs only if it names them; in a
     * conversation with the agent, which has no other people, one that names nobody is theirs too. In a chat a
     * reaction that names nobody (an account that is gone) is never the reader's own.
     */
    fun reaction(reactorKind: String, userId: String?): String? {
        val isChatLine = author != null && reactorKind == "person"
        return reactions.firstOrNull { reaction ->
            if (reaction.reactorKind != reactorKind) {
                false
            } else if (userId.isNullOrEmpty()) {
                !isChatLine
            } else if (reaction.namedUserId == null) {
                !isChatLine
            } else {
                reaction.namedUserId == userId
            }
        }?.emoji
    }
}

/** Suggestions are the replies an answer offers, written as a hidden last line, `<!--suggestions:["Yes","Not yet"]-->`, which is never shown. */
object Suggestions {
    private val pattern = Regex("""<!--\s*suggestions:\s*(\[.*?\])\s*-->\s*$""", RegexOption.DOT_MATCHES_ALL)

    /** An answer's text without its suggestions line, and the suggestions; while it is still being written, a half-written line is left out too. */
    data class Split(val text: String, val suggestions: List<String>)

    fun split(text: String): Split {
        val match = pattern.find(text)
        if (match != null) {
            val parsed = try {
                TeaNodeJson.decodeFromString<List<String>>(match.groupValues[1])
            } catch (_: Exception) {
                emptyList()
            }
            val suggestions = parsed.map { suggestion -> suggestion.trim() }.filter { suggestion -> suggestion.isNotEmpty() }
            val display = text.substring(0, match.range.first).trim()
            return Split(display, if (suggestions.size in 2..6) suggestions else emptyList())
        }
        val open = text.lastIndexOf("<!--")
        if (open >= 0 && !text.substring(open).contains("-->")) return Split(text.substring(0, open).trim(), emptyList())
        return Split(text, emptyList())
    }
}

/** CodingQuestion is a question one of the person's coding sessions asked, as the server keeps it beside the agent's telling of it (the "herdr_asked" note). */
data class CodingQuestion(
    val computer: String,
    val paneId: String,
    val questionFingerprint: String,
    /** "claude" or "codex". */
    val codingAgentKind: String,
    /** The session's name, or where it works. */
    val sessionName: String,
    val questionText: String,
    val isMultipleChoice: Boolean,
    val options: List<Option>,
) {
    data class Option(
        val optionNumber: Int,
        val optionLabel: String,
        val optionDescription: String,
        /** "choice", "freeText" or "chat". */
        val herdrOptionKind: String,
    )

    val codingAgentName: String get() = if (codingAgentKind == "codex") "Codex" else "Claude Code"

    /**
     * The question's lines worth reading, without the countdown a coding agent draws under a request it will deny by
     * itself, which is not part of what it asks.
     */
    val shownText: String get() = questionText.split("\n").filter { line -> !isCountdown(line) }.joinToString("\n")

    /**
     * What the question asks, in one line, for a card that keeps the rest until the person opens it. A tool's approval
     * is named by the tool and what it is for ("Bash command · Run the tests"), since its question line is always "Do
     * you want to proceed?"; any other question by the line that asks, as the dashboard heads its card.
     */
    val headline: String
        get() {
            val lines = shownText.split("\n").map { line -> line.trim() }.filter { line -> line.isNotEmpty() }
            val first = lines.firstOrNull() ?: return ""
            val chosen = if (lines.size > 1 && !first.contains("?") && first.split(" ").filter { word -> word.isNotEmpty() }.size <= 3 && !lines[1].contains("?")) {
                "$first · ${lines[1]}"
            } else {
                lines.firstOrNull { line -> line.contains("?") } ?: first
            }
            return if (chosen.length > 160) chosen.take(157) + "…" else chosen
        }

    /** The options a person picks, without the ones that ask to type. */
    val choices: List<Option> get() = options.filter { option -> option.herdrOptionKind == "choice" }

    /** The card's line: by computer as well as pane, since two computers can each have a pane of the same id, as the dashboard tells them. */
    val cardId: String get() = cardId(computer, paneId, questionFingerprint)

    companion object {
        private val countdownTime = Regex("""\d{1,2}:\d{2}""")

        fun isCountdown(line: String): Boolean = line.contains("will automatically") && countdownTime.containsMatchIn(line)

        fun cardId(computer: String, paneId: String, questionFingerprint: String): String = "coding-$computer-$paneId-$questionFingerprint"

        @Serializable
        private class Note(val computer: String, val paneId: String, val herdrSession: Session? = null) {
            @Serializable
            class Session(
                val paneName: String? = null,
                val codingAgentKind: String? = null,
                val workingDirectory: String? = null,
                val question: Question? = null,
            ) {
                @Serializable
                class Question(
                    val questionFingerprint: String,
                    val questionText: String,
                    val isMultipleChoice: Boolean? = null,
                    val options: List<Choice>? = null,
                ) {
                    @Serializable
                    class Choice(
                        val optionNumber: Int,
                        val optionLabel: String,
                        val optionDescription: String? = null,
                        val herdrOptionKind: String? = null,
                    )
                }
            }
        }

        /** Reads a "herdr_asked" note; null for one that carries no question. */
        fun of(note: String): CodingQuestion? {
            val parsed = try {
                TeaNodeJson.decodeFromString(Note.serializer(), note)
            } catch (_: Exception) {
                return null
            }
            val session = parsed.herdrSession ?: return null
            val question = session.question ?: return null
            val place = session.paneName?.takeIf { name -> name.isNotEmpty() } ?: session.workingDirectory?.trimEnd('/')?.substringAfterLast('/').orEmpty()
            return CodingQuestion(
                computer = parsed.computer, paneId = parsed.paneId, questionFingerprint = question.questionFingerprint,
                codingAgentKind = session.codingAgentKind ?: "claude", sessionName = place, questionText = question.questionText,
                isMultipleChoice = question.isMultipleChoice ?: false,
                options = (question.options ?: emptyList()).map { choice ->
                    Option(choice.optionNumber, choice.optionLabel, choice.optionDescription.orEmpty(), choice.herdrOptionKind ?: "choice")
                },
            )
        }
    }
}

/** CheckInOrigin is where a turn the agent took on its own came from, read from the marker its opening message starts with (the same markers the dashboard's drawer reads). */
enum class CheckInOrigin(val marker: String) {
    Goal("[goal check-in]"),
    GoalNeedsYou("[goal needs you]"),
    GoalDone("[goal done]"),
    Background("[background command]"),
    BackgroundWork("[background work]"),
    Schedule("[schedule]"),
    SpeakFirst("[speaking first]"),
    Alert("[alert]"),
    FileSent("[file sent]"),
    HerdrQuestion("[herdr question]"),
    HerdrSession("[herdr session]"),
    RestartResume("[resuming after a restart]"),
    Approved("[approved]"),
    Declined("[declined]"),
    ;

    companion object {
        /** The origin of a message's text, or null when a person wrote it. */
        fun of(text: String): CheckInOrigin? = entries.firstOrNull { origin -> text.startsWith(origin.marker) }
    }
}

/** The detail of a skill_share_call_waiting note: which call waits, in which share. */
@Serializable
data class SkillShareNote(val skillShareId: String = "", val callId: String = "", val granteeName: String? = null, val granteeUserId: String? = null) {
    companion object {
        fun parsing(noteDetail: String): SkillShareNote? {
            val note = try {
                TeaNodeJson.decodeFromString(serializer(), noteDetail)
            } catch (_: Exception) {
                return null
            }
            return if (note.callId.isEmpty() || note.skillShareId.isEmpty()) null else note
        }
    }
}

/** answeringMarker starts a late answer to a question: the question, a blank line, the answer. */
private const val ANSWERING_MARKER = "[answering]"

/**
 * Transcript turns a conversation's stored messages, its open cards and the events of the turn running now into the
 * lines the app shows, the way the dashboard's drawer does.
 */
class Transcript(
    messages: List<AgentMessage> = emptyList(),
    interactions: List<AgentInteraction> = emptyList(),
    /**
     * Whether this is a chat: what an agent says there reaches every member as the chat's own events, so the turn's
     * own answer is not drawn again.
     */
    private val isChat: Boolean = false,
) {
    private val storedLines = mutableListOf<TranscriptLine>()

    /** The cards this app settled while their answer is still on its way. */
    private val awaitingCardIds = mutableSetOf<String>()

    /** The lines now. The list is a copy: a screen holds it while the feed goes on changing this one. */
    val lines: List<TranscriptLine> get() = storedLines.toList()

    private fun change(index: Int, transform: TranscriptLine.() -> TranscriptLine) {
        storedLines[index] = storedLines[index].transform()
    }

    init {
        val results = mutableMapOf<String, String>()
        for (message in messages) {
            if (message.role == "tool") message.toolCallId?.let { callId -> results[callId] = message.content }
        }
        // What a turn cost is added up over its rounds and shown on its last answer.
        var turnUsage: AgentUsage? = null
        var turnAnswer: Int? = null
        fun closeTurn() {
            val answerIndex = turnAnswer
            val usage = turnUsage
            if (answerIndex != null && usage != null) change(answerIndex) { copy(usage = usage) }
            turnUsage = null
            turnAnswer = null
        }
        for (message in messages) {
            // What was said in a chat is drawn as what a person or an agent said there; the rows of the member's own
            // agent's work in it name no author and are drawn as anywhere else.
            if (isChat) {
                val said = chatLine(message)
                if (said != null) {
                    closeTurn()
                    storedLines.add(said)
                    continue
                }
            }
            when (message.role) {
                "user" -> {
                    closeTurn()
                    val origin = CheckInOrigin.of(message.content)
                    if (origin != null) {
                        storedLines.add(TranscriptLine(message.id, TranscriptLine.Kind.CheckIn, message.content, detail = origin.marker, at = message.createdAt, messageId = message.id))
                        continue
                    }
                    var line = TranscriptLine(
                        message.id, TranscriptLine.Kind.Person, message.content, attachments = message.attachments ?: emptyList(),
                        at = message.createdAt, messageId = message.id, reactions = message.reactions ?: emptyList(),
                    )
                    if (message.content.startsWith(ANSWERING_MARKER)) {
                        val rest = message.content.removePrefix(ANSWERING_MARKER).trim()
                        val gap = rest.indexOf("\n\n")
                        line = if (gap >= 0) {
                            line.copy(quotedText = rest.substring(0, gap), quotedRole = "question", text = rest.substring(gap + 2).trim())
                        } else {
                            line.copy(text = rest)
                        }
                    }
                    val pointedAt = mutableListOf<AgentReference>()
                    for (reference in message.references ?: emptyList()) {
                        if (reference.isReply) {
                            line = line.copy(quotedText = reference.quotedText.orEmpty(), quotedRole = reference.quotedRole.orEmpty())
                        } else {
                            pointedAt.add(reference)
                        }
                    }
                    storedLines.add(line.copy(references = pointedAt))
                }
                "assistant" -> {
                    message.usage?.let { usage -> turnUsage = (turnUsage ?: AgentUsage()) + usage }
                    if (message.content.trim().isNotEmpty()) {
                        val split = Suggestions.split(message.content)
                        storedLines.add(
                            TranscriptLine(
                                message.id, TranscriptLine.Kind.Answer, split.text, at = message.createdAt, suggestions = split.suggestions,
                                messageId = message.id, reactions = message.reactions ?: emptyList(),
                            ),
                        )
                        turnAnswer = storedLines.size - 1
                    }
                    for (call in message.toolCalls ?: emptyList()) {
                        storedLines.add(
                            TranscriptLine(
                                toolId(call.id), TranscriptLine.Kind.Tool, results[call.id].orEmpty(), detail = call.name, isDone = true,
                                callId = call.id, at = message.createdAt, arguments = call.arguments.orEmpty(),
                            ),
                        )
                    }
                }
                "note" -> {
                    // A coding session's question is drawn as its card, in place of the agent's telling of it and the turn that told it.
                    if (message.name == "herdr_asked") {
                        val question = CodingQuestion.of(message.content)
                        if (question != null) {
                            if (storedLines.lastOrNull()?.kind == TranscriptLine.Kind.Answer) storedLines.removeAt(storedLines.size - 1)
                            val last = storedLines.lastOrNull()
                            if (last?.kind == TranscriptLine.Kind.CheckIn && last.detail == CheckInOrigin.HerdrQuestion.marker) storedLines.removeAt(storedLines.size - 1)
                            storedLines.removeAll { line -> line.id == question.cardId }
                            storedLines.add(
                                TranscriptLine(
                                    question.cardId, TranscriptLine.Kind.CodingQuestion, question.questionText, detail = question.codingAgentName,
                                    callId = question.questionFingerprint, choices = question.choices.map { option -> option.optionLabel },
                                    at = message.createdAt, codingQuestion = question,
                                ),
                            )
                            turnAnswer = null
                        }
                        continue
                    }
                    if (message.name == "herdr_answered") {
                        answerCodingQuestion(message.content)
                        continue
                    }
                    storedLines.add(TranscriptLine(message.id, TranscriptLine.Kind.Note, message.content, detail = message.name.orEmpty(), at = message.createdAt))
                }
                "compaction" -> storedLines.add(TranscriptLine(message.id, TranscriptLine.Kind.Note, message.content, detail = "compacted", at = message.createdAt))
                else -> continue
            }
        }
        closeTurn()
        for (interaction in interactions) {
            val id = cardId(interaction.runId, interaction.callId)
            if (storedLines.any { line -> line.id == id }) continue
            storedLines.add(
                TranscriptLine(
                    id, if (interaction.interactionKind == "approval") TranscriptLine.Kind.Approval else TranscriptLine.Kind.Question,
                    interaction.interactionText, detail = interaction.toolName.orEmpty(), runId = interaction.runId, callId = interaction.callId,
                    choices = interaction.interactionChoices ?: emptyList(), at = interaction.createdAt, risk = interaction.toolRisk.orEmpty(),
                ),
            )
        }
    }

    /** Adds older history without rebuilding lines changed by the live feed or by an optimistic action while the history request was in flight. */
    fun prepend(messages: List<AgentMessage>, earlierCount: Int, isChat: Boolean = false) {
        val whole = Transcript(messages, isChat = isChat)
        val newer = Transcript(messages.drop(earlierCount), isChat = isChat)
        val skippedLineIds = storedLines.map { line -> line.id }.toSet() + newer.lines.map { line -> line.id }
        storedLines.addAll(0, whole.lines.filter { line -> line.id !in skippedLineIds })
    }

    /**
     * Carries over the answers of agents in a chat that are still being written from the transcript this one replaces,
     * so that reading the chat again in the middle of a turn does not wipe the words that have come. One whose words
     * the stored messages already hold is left out: it is the message it was about to become.
     */
    fun keepStreaming(earlier: Transcript) {
        for (line in earlier.lines) {
            if (!line.isStreaming || !line.id.startsWith(STREAMING_PREFIX)) continue
            if (storedLines.any { stored -> stored.id == line.id }) continue
            val words = line.streamedText.trim()
            val isStored = storedLines.any { stored ->
                stored.runId == line.runId && stored.author?.isAgent == true && !stored.isStreaming && words.isNotEmpty() && stored.text.trim().startsWith(words)
            }
            if (!isStored) storedLines.add(line)
        }
    }

    /**
     * The person's words a turn began with, or the check-in of a turn the agent took on its own, read back with the
     * transcript, when other words of theirs come after them: the same words, stored on or after the moment the turn
     * was asked, the earliest such.
     */
    private fun storedOpeningIndex(text: String, runId: String, askedAt: String?): Int? {
        val asked = ServerTime.instant(askedAt) ?: return null
        val index = storedLines.indexOfFirst { line ->
            if ((line.kind != TranscriptLine.Kind.Person && line.kind != TranscriptLine.Kind.CheckIn) || line.text != text || !(line.runId.isEmpty() || line.runId == runId)) {
                false
            } else {
                val stored = ServerTime.instant(line.at)
                stored != null && Duration.between(asked, stored).toMillis() >= -OPENING_STORED_LEEWAY_MS
            }
        }
        return if (index < 0) null else index
    }

    /** Shows what the person just sent from this app, before the turn it begins is under way. */
    fun addSent(
        text: String,
        attachments: List<AgentAttachment> = emptyList(),
        replyingTo: AgentReference? = null,
        author: ChatAuthor? = null,
        isVoiceMessage: Boolean = false,
    ): String {
        val line = TranscriptLine(
            SENT_PREFIX + UUID.randomUUID(), TranscriptLine.Kind.Person, text, attachments = attachments, author = author, isVoiceMessage = isVoiceMessage,
            quotedText = replyingTo?.quotedText.orEmpty(), quotedRole = replyingTo?.quotedRole.orEmpty(),
        )
        storedLines.add(line)
        return line.id
    }

    /** Takes back only the message belonging to a refused request, unless the feed already confirmed that the server accepted it. */
    fun withdrawSent(lineId: String) {
        val index = storedLines.indexOfFirst { line -> line.id == lineId && line.id.startsWith(SENT_PREFIX) && line.runId.isEmpty() }
        if (index >= 0) storedLines.removeAt(index)
    }

    /** The question waiting for the person's answer, if one is: the agent's, or a coding session's. */
    val openQuestion: TranscriptLine?
        get() = storedLines.lastOrNull { line -> (line.kind == TranscriptLine.Kind.Question || line.kind == TranscriptLine.Kind.CodingQuestion) && !line.isDone }

    /** The suggested replies to offer now: those of the last answer, when nothing but notes came after it and it is finished. */
    val offeredSuggestions: List<String>
        get() {
            val last = storedLines.lastOrNull { line -> line.kind != TranscriptLine.Kind.Note } ?: return emptyList()
            return if (last.kind == TranscriptLine.Kind.Answer && !last.isStreaming) last.suggestions else emptyList()
        }

    /** A message that was just said in a chat: added, unless it is there already. The person's own, drawn as sent while it was on its way, replaces that line. */
    fun applySaid(message: AgentMessage) {
        val said = chatLine(message) ?: return
        if (storedLines.any { line -> line.messageId == message.id }) return
        // An agent's answer drawn while it was being written is replaced where it stood, not added beside the message that is the same words kept.
        val author = said.author
        if (author != null && author.isAgent && said.runId.isNotEmpty()) {
            val streaming = storedLines.indexOfFirst { line -> line.id == streamingId(said.runId) }
            if (streaming >= 0) {
                storedLines[streaming] = said
                return
            }
        }
        if (author != null && author.isOwnWords) {
            val pending = storedLines.indexOfFirst { line -> line.id.startsWith(SENT_PREFIX) && line.author != null && line.text == said.text }
            if (pending >= 0) {
                storedLines[pending] = said
                return
            }
        }
        storedLines.add(said)
    }

    /** Words of an agent's answer in a chat that are still being written, told to every member: added to the line for its run, or a new one. */
    fun applyStreamed(event: AgentEvent) {
        val agentId = event.authorAgentId
        if (agentId.isNullOrEmpty()) return
        val id = streamingId(event.runId)
        val index = storedLines.indexOfFirst { line -> line.id == id }
        if (index >= 0) {
            val streamed = storedLines[index].streamedText + event.text.orEmpty()
            change(index) { copy(streamedText = streamed, text = streamed) }
            return
        }
        storedLines.add(
            TranscriptLine(
                id, TranscriptLine.Kind.Answer, event.text.orEmpty(), isStreaming = true, runId = event.runId, at = event.at, streamedText = event.text.orEmpty(),
                author = ChatAuthor("agent", authorName = event.name.orEmpty(), authorOwnerName = event.authorOwnerName.orEmpty(), authorAgentId = agentId),
            ),
        )
    }

    /** A message of a chat whose words were changed. */
    fun applyEdited(message: AgentMessage) {
        val index = storedLines.indexOfFirst { line -> line.messageId == message.id }
        if (index < 0) return
        change(index) {
            copy(
                text = if (message.isDeleted == true) "" else message.content, editedAt = message.editedAt,
                // The words of a voice message arrive after the message, as an edit.
                transcriptText = if (message.isVoiceMessage == true) message.transcriptText.orEmpty() else transcriptText,
                transcriptState = if (message.isVoiceMessage == true) message.transcriptState.orEmpty() else transcriptState,
            )
        }
    }

    /**
     * A message of a chat that was deleted: a mark that it was, not a gap. The line an agent's words were drawn as
     * while they came is taken off instead, when the turn was stopped or failed: no message was stored for it.
     */
    fun applyDeleted(messageId: String) {
        if (messageId.startsWith(STREAMING_PREFIX)) {
            storedLines.removeAll { line -> line.id == messageId && line.isStreaming }
            return
        }
        val index = storedLines.indexOfFirst { line -> line.messageId == messageId }
        if (index < 0) return
        change(index) {
            copy(text = "", isDeleted = true, attachments = emptyList(), isVoiceMessage = false, transcriptText = "", reactions = emptyList())
        }
    }

    /** Puts one side's reaction on a message, or takes it away when the emoji is empty; as the server keeps it, one each at most. */
    fun react(messageId: String, emoji: String, reactorKind: String, reactorUserId: String? = null, reactorName: String? = null) {
        val index = storedLines.indexOfLast { line -> line.messageId == messageId }
        if (index < 0) return
        // With a person named only theirs is replaced, in a chat where others' stay; without, the kind is the reactor.
        // In a chat a reaction that names nobody is an account that is gone, never the one being replaced.
        val isChatLine = storedLines[index].author != null && reactorKind == "person"
        val namedUserId = reactorUserId?.takeIf { text -> text.isNotEmpty() }
        val kept = storedLines[index].reactions.filterNot { reaction ->
            if (reaction.reactorKind != reactorKind) {
                false
            } else if (isChatLine) {
                namedUserId != null && reaction.namedUserId == namedUserId
            } else {
                namedUserId == null || reaction.namedUserId == null || reaction.namedUserId == namedUserId
            }
        }
        val reactions = if (emoji.isEmpty()) kept else kept + AgentMessageReaction(emoji, reactorKind, reactorUserId = reactorUserId, reactorName = reactorName)
        change(index) { copy(reactions = reactions) }
    }

    /** Applies an event of a running turn. */
    fun apply(event: AgentEvent) {
        // A turn waiting behind another has begun once anything else of it arrives.
        if (event.kind != "note" || event.noteKind != "queued") storedLines.removeAll { line -> line.id == "${event.runId}-queued" }
        when (event.kind) {
            "reacted" -> {
                val messageId = event.messageId
                val reactorKind = event.reactorKind
                if (messageId != null && reactorKind != null) react(messageId, event.emoji.orEmpty(), reactorKind, event.reactorUserId, event.reactorName)
            }
            "said" -> event.message?.let { message -> applySaid(message) }
            "edited" -> event.message?.let { message -> applyEdited(message) }
            "deleted" -> event.messageId?.let { messageId -> applyDeleted(messageId) }
            "asked" -> applyAsked(event)
            "text" -> {
                // The words of an agent's answer in a chat come with the agent they are the answer of, and to everybody there.
                if (!event.authorAgentId.isNullOrEmpty()) {
                    applyStreamed(event)
                    return
                }
                val last = storedLines.indexOfLast { line -> line.runId == event.runId && line.kind == TranscriptLine.Kind.Answer && line.isStreaming }
                if (last >= 0) {
                    val streamed = storedLines[last].streamedText + event.text.orEmpty()
                    change(last) { copy(streamedText = streamed, text = Suggestions.split(streamed).text) }
                } else {
                    storedLines.add(
                        TranscriptLine(
                            "${event.runId}-${event.sequence}", TranscriptLine.Kind.Answer, Suggestions.split(event.text.orEmpty()).text, isStreaming = true,
                            runId = event.runId, at = event.at, streamedText = event.text.orEmpty(),
                        ),
                    )
                }
            }
            "message" -> {
                // In a chat the answer is the chat's message, which arrives as its own event and replaces the words drawn as they were written.
                if (isChat) return
                val split = Suggestions.split(event.text.orEmpty())
                val last = storedLines.indexOfLast { line -> line.runId == event.runId && line.kind == TranscriptLine.Kind.Answer && line.isStreaming }
                if (last >= 0) {
                    change(last) { copy(text = if (event.text == null) text else split.text, suggestions = split.suggestions, isStreaming = false) }
                } else if (split.text.trim().isNotEmpty() &&
                    storedLines.takeLast(REPLAY_LOOK_BACK).none { line -> line.kind == TranscriptLine.Kind.Answer && line.text == split.text }
                ) {
                    storedLines.add(
                        TranscriptLine("${event.runId}-${event.sequence}", TranscriptLine.Kind.Answer, split.text, runId = event.runId, at = event.at, suggestions = split.suggestions),
                    )
                }
            }
            "tool_call" -> {
                val id = toolId(event.callId.orEmpty())
                if (storedLines.any { line -> line.id == id }) return
                storedLines.add(
                    TranscriptLine(id, TranscriptLine.Kind.Tool, "", detail = event.tool.orEmpty(), runId = event.runId, callId = event.callId.orEmpty(), at = event.at, arguments = event.arguments.orEmpty()),
                )
            }
            "tool_result" -> {
                val toolIndex = storedLines.indexOfFirst { line -> line.id == toolId(event.callId.orEmpty()) }
                if (toolIndex >= 0) change(toolIndex) { copy(isDone = true, text = event.text.orEmpty(), resolution = event.note.orEmpty()) }
                // A card's call has an answer: settled, wherever it was settled.
                val cardId = cardId(event.runId, event.callId.orEmpty())
                awaitingCardIds.remove(cardId)
                val cardIndex = storedLines.indexOfFirst { line -> line.id == cardId }
                if (cardIndex >= 0 && !storedLines[cardIndex].isDone) {
                    val resolution = resolution(event.text.orEmpty(), storedLines[cardIndex].kind)
                    change(cardIndex) { copy(isDone = true, resolution = resolution) }
                }
            }
            "confirmation" -> {
                val id = cardId(event.runId, event.callId.orEmpty())
                if (storedLines.any { line -> line.id == id }) return
                storedLines.add(
                    TranscriptLine(id, TranscriptLine.Kind.Approval, event.note.orEmpty(), detail = event.tool.orEmpty(), runId = event.runId, callId = event.callId.orEmpty(), at = event.at, risk = event.risk.orEmpty()),
                )
            }
            "question" -> {
                val id = cardId(event.runId, event.callId.orEmpty())
                if (storedLines.any { line -> line.id == id }) return
                val choices = event.text.orEmpty().split("\n").filter { choice -> choice.isNotEmpty() }
                storedLines.add(
                    TranscriptLine(id, TranscriptLine.Kind.Question, event.note.orEmpty(), runId = event.runId, callId = event.callId.orEmpty(), choices = choices, at = event.at),
                )
            }
            "note" -> applyNote(event)
            "done", "error" -> {
                for (index in storedLines.indices) {
                    if (storedLines[index].runId == event.runId && storedLines[index].isStreaming) change(index) { copy(isStreaming = false) }
                }
                storedLines.removeAll { line -> line.runId == event.runId && line.isPending }
            }
            else -> Unit
        }
    }

    private fun applyAsked(event: AgentEvent) {
        // In a chat what was said is a message of the chat, drawn from its own event; the turn it started is only followed.
        if (event.note == "chat") return
        // A turn began: something the person said, here or anywhere else, or a turn the agent took on its own. One
        // already shown, sent from this app or read back before the feed replayed the turn, is not drawn again.
        val text = event.text
        if (text.isNullOrEmpty()) return
        // Requests can finish in a different order from the taps that started them. Confirm the matching pending line
        // without removing another message or events already on screen.
        val pending = storedLines.indexOfFirst { line -> line.id.startsWith(SENT_PREFIX) && line.runId.isEmpty() && line.text == text }
        if (pending >= 0) {
            change(pending) { copy(runId = event.runId) }
            return
        }
        // The person's latest words, when they are these, are this turn read back with the transcript: what came after
        // them is the turn as far as it was stored, which the replay draws again in order, so it goes.
        val latest = storedLines.indexOfLast { line -> line.kind == TranscriptLine.Kind.Person }
        if (latest >= 0 && storedLines[latest].text == text && (storedLines[latest].runId.isEmpty() || storedLines[latest].runId == event.runId)) {
            while (storedLines.size > latest + 1) storedLines.removeAt(storedLines.size - 1)
            change(latest) { copy(runId = event.runId) }
            return
        }
        // Not the latest words: what the person wrote while this turn ran went into it, stored after its opening. The
        // opening is still the turn as far as it was stored, and goes from there the same way.
        val opening = storedOpeningIndex(text, event.runId, event.at)
        if (opening != null) {
            while (storedLines.size > opening + 1) storedLines.removeAt(storedLines.size - 1)
            change(opening) { copy(runId = event.runId) }
            return
        }
        val recent = storedLines.indices.toList().takeLast(REPLAY_LOOK_BACK)
        val sameRun = recent.lastOrNull { index ->
            storedLines[index].text == text && (storedLines[index].runId.isEmpty() || storedLines[index].runId == event.runId) && storedLines[index].kind == TranscriptLine.Kind.CheckIn
        }
        val anyRun = recent.lastOrNull { index -> storedLines[index].kind == TranscriptLine.Kind.CheckIn && storedLines[index].text == text }
        val origin = CheckInOrigin.of(text)
        if (sameRun != null) {
            change(sameRun) { copy(runId = event.runId) }
        } else if (anyRun != null) {
            change(anyRun) { copy(runId = event.runId) }
        } else if (origin != null) {
            storedLines.add(TranscriptLine("${event.runId}-asked", TranscriptLine.Kind.CheckIn, text, detail = origin.marker, runId = event.runId, at = event.at))
        } else {
            storedLines.add(TranscriptLine("${event.runId}-asked", TranscriptLine.Kind.Person, text, runId = event.runId, at = event.at))
        }
    }

    private fun applyNote(event: AgentEvent) {
        val noteKind = event.noteKind.orEmpty()
        when (noteKind) {
            "queued" -> {
                if (storedLines.any { line -> line.id == "${event.runId}-queued" }) return
                storedLines.add(TranscriptLine("${event.runId}-queued", TranscriptLine.Kind.Note, "", detail = "queued", runId = event.runId, at = event.at, isPending = true))
            }
            "compacting" -> {
                if (storedLines.any { line -> line.id == "${event.runId}-compacting" }) return
                storedLines.add(TranscriptLine("${event.runId}-compacting", TranscriptLine.Kind.Note, "", detail = "compacting", runId = event.runId, at = event.at, isPending = true))
            }
            "herdr_asked" -> return
            // A coding session's question went, here or at the computer: its card folds with what it was answered with.
            "herdr_answered" -> answerCodingQuestion(event.noteDetail.orEmpty())
            else -> {
                // A call that waits is keyed by the call it names, as the dashboard keys it: the notes of two calls can
                // arrive with the same run and sequence, and the second card is not the first one again.
                var id = "${event.runId}-note-${event.sequence}"
                if (noteKind == "skill_share_call_waiting") {
                    SkillShareNote.parsing(event.noteDetail.orEmpty())?.let { note -> id = skillShareCallWaitingId(note.callId) }
                }
                if (storedLines.any { line -> line.id == id }) return
                val line = TranscriptLine(id, TranscriptLine.Kind.Note, event.noteDetail ?: event.note.orEmpty(), detail = noteKind, runId = event.runId, at = event.at)
                val compacting = storedLines.indexOfFirst { candidate -> candidate.id == "${event.runId}-compacting" }
                if (noteKind == "compacted" && compacting >= 0) storedLines[compacting] = line else storedLines.add(line)
            }
        }
    }

    @Serializable
    private class AnsweredNote(val computer: String, val paneId: String, val questionFingerprint: String, val answerText: String? = null)

    /** Folds a coding session's question card from a "herdr_answered" note, with what it was answered with when that is known. An answer this app gave is kept when the note knows none. */
    private fun answerCodingQuestion(note: String) {
        val answered = try {
            TeaNodeJson.decodeFromString(AnsweredNote.serializer(), note)
        } catch (_: Exception) {
            return
        }
        val index = storedLines.indexOfLast { line -> line.id == CodingQuestion.cardId(answered.computer, answered.paneId, answered.questionFingerprint) }
        if (index < 0) return
        awaitingCardIds.remove(storedLines[index].id)
        val answerText = answered.answerText
        change(index) { copy(isDone = true, resolution = if (!answerText.isNullOrEmpty()) answerText else resolution) }
    }

    /** Marks a card settled from this app, before the feed says so. */
    fun settle(cardId: String, resolution: String) {
        awaitingCardIds.remove(cardId)
        val index = storedLines.indexOfFirst { line -> line.id == cardId }
        if (index >= 0) change(index) { copy(isDone = true, resolution = resolution) }
    }

    /** Marks a card settled as soon as it is answered here, while the answer is still on its way: it can open again with `reopen` if the answer does not go through. */
    fun settleAwaiting(cardId: String, resolution: String) {
        settle(cardId, resolution)
        awaitingCardIds.add(cardId)
    }

    /** Opens a card again that was settled before its answer was sent, when the answer did not go through. A card the feed or a confirmed answer settled since stays settled. */
    fun reopen(cardId: String) {
        if (!awaitingCardIds.remove(cardId)) return
        val index = storedLines.indexOfFirst { line -> line.id == cardId }
        if (index >= 0) change(index) { copy(isDone = false, resolution = "") }
    }

    /** Takes a card away that the server says is no longer waiting. */
    fun remove(cardId: String) {
        storedLines.removeAll { line -> line.id == cardId }
    }

    companion object {
        /** sentPrefix starts the id of a line this app sent, until the feed says which turn it began. */
        const val SENT_PREFIX = "sent-"

        /** What every streaming line's id begins with, and so what a deleted event that names one begins with. */
        const val STREAMING_PREFIX = "streaming-"

        /** How far back a turn's opening is looked for, so one the feed replays is matched with the line already there rather than drawn again. */
        const val REPLAY_LOOK_BACK = 8

        /** How much earlier than a turn's asked event its words can be stored: the two clocks are read a moment apart and a time is read to the second. */
        const val OPENING_STORED_LEEWAY_MS = 5_000L

        fun cardId(runId: String, callId: String): String = "$runId-$callId-card"

        /** The id of the card for a call of a shared skill that waits, built from the call so that each call has one card however the note reached the transcript. */
        fun skillShareCallWaitingId(callId: String): String = "skill-share-call-waiting-$callId"

        /** A tool line's id, the same whether it was read back or arrived live, so a turn replayed after the feed reconnects does not draw it twice. */
        fun toolId(callId: String): String = "tool-$callId"

        /** The id of the line an agent's answer in a chat is drawn as while it is being written: one for each run. */
        fun streamingId(runId: String): String = "$STREAMING_PREFIX$runId"

        fun resolution(result: String, kind: TranscriptLine.Kind): String {
            if (kind == TranscriptLine.Kind.Approval) {
                val lowered = result.lowercase()
                return if (lowered.contains("declined") || lowered.contains("refused") || lowered.contains("denied")) "declined" else "approved"
            }
            // A question's tool result is {"answer": ...}.
            return try {
                val answered = TeaNodeJson.parseToJsonElement(result) as? JsonObject
                answered?.get("answer")?.stringValue ?: result
            } catch (_: Exception) {
                result
            }
        }

        /**
         * The line a message said in a chat is: a person's as the person kind, an agent's as an answer, either with
         * who said it. A row of the member's own agent's work in the chat is none: it names no author.
         */
        fun chatLine(message: AgentMessage): TranscriptLine? {
            if (message.role in listOf("tool", "note", "compaction") || message.isAgentWork == true) return null
            val author = ChatAuthor.of(message) ?: return null
            // The call a round made carries no words: it is the agent's own work.
            if (!message.toolCalls.isNullOrEmpty() && message.content.trim().isEmpty()) return null
            var replyText = ""
            var replyRole = ""
            val pointedAt = mutableListOf<AgentReference>()
            for (reference in message.references ?: emptyList()) {
                if (reference.isReply) {
                    replyText = reference.quotedText.orEmpty()
                    replyRole = reference.quotedRole.orEmpty()
                } else {
                    pointedAt.add(reference)
                }
            }
            return TranscriptLine(
                id = message.id, kind = if (author.isPerson) TranscriptLine.Kind.Person else TranscriptLine.Kind.Answer,
                text = if (message.isDeleted == true) "" else message.content, attachments = message.attachments ?: emptyList(), at = message.createdAt,
                messageId = message.id, author = author, runId = message.agentRunId.orEmpty(), reactions = message.reactions ?: emptyList(),
                editedAt = message.editedAt, isDeleted = message.isDeleted ?: false, mentionedUserIds = message.mentionedUserIds ?: emptyList(),
                isAllMentioned = message.isAllMentioned ?: false, isVoiceMessage = message.isVoiceMessage ?: false,
                transcriptText = message.transcriptText.orEmpty(), transcriptState = message.transcriptState.orEmpty(),
                chatCallId = message.callId.orEmpty(), chatCall = message.call, quotedText = replyText, quotedRole = replyRole, references = pointedAt,
            )
        }
    }
}
