package com.teanode.kit.coding

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.serializer
import java.time.Instant

// The person's herdr sessions: the Claude Code and Codex sessions kept in herdr's panes on each attached computer, which
// the agent works in beside them. The same operations as the dashboard's Coding page and the agent's herdr tool.

/** One option of a question a session waits on. */
@Serializable
data class HerdrQuestionOption(
    val optionNumber: Int,
    val optionLabel: String,
    val optionDescription: String,
    /** "choice", "freeText" (it takes typed text) or "chat". */
    val herdrOptionKind: String,
)

/** A question a session waits on. */
@Serializable
data class HerdrQuestion(
    val questionFingerprint: String,
    /** "question", "toolApproval" or "planApproval". */
    val herdrQuestionKind: String,
    val questionText: String,
    val isMultipleChoice: Boolean,
    val isFromTranscript: Boolean,
    val options: List<HerdrQuestionOption>,
)

/**
 * A question one of the person's coding sessions asked, as the server keeps it beside the agent's telling of it (the
 * "herdr_asked" note, internal/agent/herdr.go). The conversation's card and a session's own screen answer through it.
 */
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

    /** The question's lines worth reading, without the countdown a coding agent draws under a request it will deny by itself. */
    val shownText: String
        get() = questionText.split("\n").filter { line -> !isCountdown(line) }.joinToString("\n")

    /**
     * What the question asks, in one line, for a card that keeps the rest until the person opens it. A tool's approval is
     * named by the tool and what it is for ("Bash command · Run the tests"), since its question line is always "Do you
     * want to proceed?"; any other question by the line that asks, as the dashboard heads its card.
     */
    val headline: String
        get() {
            val lines = shownText.split("\n").map { line -> line.trim(' ', '\t') }.filter { line -> line.isNotEmpty() }
            val first = lines.firstOrNull() ?: return ""
            val chosen = if (lines.size > 1 && !first.contains('?') && first.split(' ').filter { word -> word.isNotEmpty() }.size <= 3 && !lines[1].contains('?')) {
                "$first · ${lines[1]}"
            } else {
                lines.firstOrNull { line -> line.contains('?') } ?: first
            }
            return if (chosen.length > 160) chosen.take(157) + "…" else chosen
        }

    /** The options a person picks, without the ones that ask to type. */
    val choices: List<Option> get() = options.filter { option -> option.herdrOptionKind == "choice" }

    companion object {
        private val countdownTime = Regex("""\d{1,2}:\d{2}""")

        internal fun isCountdown(line: String): Boolean = line.contains("will automatically") && countdownTime.containsMatchIn(line)
    }
}

/** One coding session in one herdr pane. */
@Serializable
data class HerdrSession(
    val computer: String,
    val paneId: String,
    /** The pane as the person finds it in herdr: workspace, tab, agent. */
    val paneName: String,
    /** "claude" or "codex". */
    val codingAgentKind: String,
    val codingSessionId: String,
    /** "idle", "working", "asking" or "unknown". */
    val herdrSessionState: String,
    val herdrAgentStatus: String,
    val paneTitle: String,
    val workingDirectory: String,
    val transcriptPath: String,
    val isWatched: Boolean,
    val workspaceId: String,
    val workspaceLabel: String,
    val workspaceNumber: Int,
    val tabId: String,
    val tabLabel: String,
    val tabNumber: Int,
    /** herdr shows this session's tab on the computer's screen now. */
    val isTabFocused: Boolean,
    val question: HerdrQuestion? = null,
) {
    /** Two computers can each have a pane of the same id. */
    val id: String get() = "$computer/$paneId"

    val codingAgentName: String get() = if (codingAgentKind == "codex") "Codex" else "Claude Code"

    val isWorking: Boolean get() = herdrSessionState == "working"
    val isAsking: Boolean get() = herdrSessionState == "asking" && question != null

    private val directoryName: String get() = workingDirectory.trimEnd('/').substringAfterLast('/')

    /** What names the session in a list or a tab: its tab label, or the pane's name, or the directory it works in. */
    val tabTitle: String
        get() {
            if (tabLabel.isNotEmpty()) return tabLabel
            if (paneName.isNotEmpty()) return paneName
            return directoryName.ifEmpty { paneId }
        }

    /**
     * What the session is about: the title its coding agent gave its pane, unless that is only the shell's prompt
     * (user@host: path), the workspace's name or the coding agent's, which say nothing of it.
     */
    val sessionTitle: String
        get() {
            val title = paneTitle.trim(' ', '\t')
            if (title.isEmpty() || title == workspaceLabel || title.lowercase() == codingAgentName.lowercase()) return ""
            if (shellPrompt.containsMatchIn(title)) return ""
            return title
        }

    /** A tab's words: herdr numbers unnamed tabs, and a bare number reads better as "tab 2". */
    private val tabWords: String
        get() {
            if (tabLabel.isEmpty()) return if (workspaceId.isEmpty()) paneName else ""
            return if (tabLabel.toIntOrNull() != null) "tab $tabLabel" else tabLabel
        }

    /** The first line of the session's row: what it is about, or where it is. */
    val rowTitle: String
        get() {
            val title = sessionTitle
            if (title.isNotEmpty()) return title
            return tabWords.ifEmpty { codingAgentName }
        }

    /** The second line of the session's row: its tab and coding agent. */
    val rowDetail: String
        get() {
            if (sessionTitle.isEmpty()) return if (tabWords.isEmpty()) "" else codingAgentName
            return listOf(tabWords, codingAgentName).filter { part -> part.isNotEmpty() }.joinToString(" · ")
        }

    /** What names the workspace in a list or a bar. */
    val workspaceTitle: String
        get() {
            if (workspaceLabel.isNotEmpty()) return workspaceLabel
            return directoryName.ifEmpty { computer }
        }

    /** The question as the conversation's card holds it, so one answering path serves both. */
    val codingQuestion: CodingQuestion?
        get() {
            val asked = question ?: return null
            return CodingQuestion(
                computer, paneId, asked.questionFingerprint, codingAgentKind, tabTitle, asked.questionText, asked.isMultipleChoice,
                asked.options.map { option -> CodingQuestion.Option(option.optionNumber, option.optionLabel, option.optionDescription, option.herdrOptionKind) },
            )
        }
}

private val shellPrompt = Regex("""^[^\s@]+@[^\s:]+:""")

/** Every session on every computer asked. */
@Serializable
data class HerdrList(
    val computerNames: List<String>,
    /** The computers that did not answer, whose sessions are not listed. */
    val failedComputerNames: List<String>,
    val sessions: List<HerdrSession>,
)

/** One thing said or done in a session. */
@Serializable
data class HerdrTurn(
    /** "person", "agent" or "tool". */
    val herdrTurnRole: String,
    val turnText: String,
    val turnAt: String? = null,
    /** A tool call's whole input and what the tool answered; empty when the read did not ask for them. */
    val toolInputText: String? = null,
    val toolResultText: String? = null,
    /** Whether the server said the tool call has an input or a result to show, which a read asking for the detail brings. */
    @kotlinx.serialization.SerialName("hasToolDetail") val isToolDetailed: Boolean? = null,
) {
    /** Whether the tool call has an input or a result to show. */
    val hasToolDetail: Boolean get() = isToolDetailed == true || isToolDetailLoaded

    /** Whether this read brought the input or the result. */
    val isToolDetailLoaded: Boolean get() = !toolInputText.isNullOrEmpty() || !toolResultText.isNullOrEmpty()
}

/** A session's last turns. */
@Serializable
data class HerdrRead(val herdrSession: HerdrSession, val turns: List<HerdrTurn>, val isTruncated: Boolean)

/** One agent a session started, or one an agent started. */
@Serializable
data class HerdrSubagent(
    val subagentId: String,
    /** The agent that started this one; empty or null when the session did. */
    val parentSubagentId: String? = null,
    /** The kind of agent, such as "general-purpose" or "Explore". */
    val subagentType: String,
    val subagentName: String? = null,
    /** The one-line task its parent gave it. */
    val taskText: String,
    val modelName: String,
    /** "running", "done" or "stopped". */
    val subagentState: String,
    /** 1 under the session, 2 under an agent, and so on. */
    val spawnDepth: Int,
    val startedAt: String? = null,
    /** Null while it runs. */
    val endedAt: String? = null,
    /** The tools it used most, at most five. */
    val toolNames: List<String>,
    val toolCallCount: Int,
) {
    val id: String get() = subagentId
    val isRunning: Boolean get() = subagentState == "running"
    val isStopped: Boolean get() = subagentState == "stopped"

    /** The agent that started this one, null when the session did. */
    val parentId: String? get() = parentSubagentId?.takeIf { value -> value.isNotEmpty() }

    /** How long it ran, or has run so far, as of now (in seconds); null when it has no start time. */
    fun durationSeconds(nowEpochMS: Long): Double? {
        val started = instantOf(startedAt) ?: return null
        val ended = instantOf(endedAt)?.toEpochMilli() ?: nowEpochMS
        return maxOf(0.0, (ended - started.toEpochMilli()) / 1000.0)
    }
}

internal fun instantOf(text: String?): Instant? = text?.let { value -> runCatching { Instant.parse(value) }.getOrNull() }

/** The agents a session started for its current request. */
@Serializable
data class HerdrSubagents(
    val herdrSession: HerdrSession,
    val sessionModelName: String,
    val requestStartedAt: String? = null,
    val isTruncated: Boolean,
    val subagents: List<HerdrSubagent>,
) {
    /** How many of the agents still run. */
    val runningCount: Int get() = subagents.count { subagent -> subagent.isRunning }

    /** The agents as a tree under the session. */
    val tree: List<HerdrSubagentNode> get() = HerdrSubagentNode.tree(subagents)
}

/** One agent with the agents it started under it. */
data class HerdrSubagentNode(val subagent: HerdrSubagent, val children: List<HerdrSubagentNode>) {
    val id: String get() = subagent.subagentId

    /** Every agent in this node and under it. */
    val descendantCount: Int get() = children.fold(children.size) { total, child -> total + child.descendantCount }

    companion object {
        /**
         * Turns the flat list into a tree, each level ordered by when its agents started (those without a start time last,
         * in the order given). An agent whose parent is not in the list, or that names itself or one of its own
         * descendants as parent, goes under the session, which is the top level of the result.
         */
        fun tree(subagents: List<HerdrSubagent>): List<HerdrSubagentNode> {
            val ordered = subagents.withIndex().sortedWith { first, second ->
                val firstDate = instantOf(first.value.startedAt)
                val secondDate = instantOf(second.value.startedAt)
                when {
                    firstDate != null && secondDate != null && firstDate != secondDate -> firstDate.compareTo(secondDate)
                    firstDate != null && secondDate == null -> -1
                    firstDate == null && secondDate != null -> 1
                    else -> first.index.compareTo(second.index)
                }
            }.map { indexed -> indexed.value }
            val knownIds = ordered.map { subagent -> subagent.subagentId }.toSet()
            val childrenByParent = mutableMapOf<String, MutableList<HerdrSubagent>>()
            val topLevel = mutableListOf<HerdrSubagent>()
            for (subagent in ordered) {
                val parentId = subagent.parentId
                if (parentId != null && parentId != subagent.subagentId && knownIds.contains(parentId)) {
                    childrenByParent.getOrPut(parentId) { mutableListOf() }.add(subagent)
                } else {
                    topLevel.add(subagent)
                }
            }
            val placedIds = mutableSetOf<String>()
            fun node(subagent: HerdrSubagent): HerdrSubagentNode {
                placedIds.add(subagent.subagentId)
                val children = (childrenByParent[subagent.subagentId] ?: emptyList()).filter { child -> !placedIds.contains(child.subagentId) }.map { child -> node(child) }
                return HerdrSubagentNode(subagent, children)
            }
            val roots = topLevel.map { subagent -> node(subagent) }.toMutableList()
            // Agents that name each other as parents are reached from no top level agent: show them under the session rather than lose them.
            for (subagent in ordered) if (!placedIds.contains(subagent.subagentId)) roots.add(node(subagent))
            return roots
        }
    }
}

/** A session's screen as it stands. */
@Serializable
data class HerdrScreen(val herdrSession: HerdrSession, val screenText: String)

/** The GraphQL documents of the herdr sessions: the same text as the iPhone app's (ios/TeaNodeKit/Sources/TeaNodeKit/Herdr.swift). */
object HerdrDocuments {
    private const val SESSION_FIELDS =
        "computer paneId paneName codingAgentKind codingSessionId herdrSessionState herdrAgentStatus paneTitle workingDirectory transcriptPath isWatched\n" +
            "workspaceId workspaceLabel workspaceNumber tabId tabLabel tabNumber isTabFocused\n" +
            "question { questionFingerprint herdrQuestionKind questionText isMultipleChoice isFromTranscript options { optionNumber optionLabel optionDescription herdrOptionKind } }"

    const val LIST_HERDR_SESSIONS =
        "query (\$computer: String) {\n  ListAgentHerdrSessions(computer: \$computer) { computerNames failedComputerNames sessions { $SESSION_FIELDS } }\n}"
    const val READ_HERDR_SESSION =
        "query (\$computer: String, \$paneId: String!, \$turnCount: Int, \$shouldIncludeToolDetail: Boolean) {\n" +
            "  ReadAgentHerdrSession(computer: \$computer, paneId: \$paneId, turnCount: \$turnCount, shouldIncludeToolDetail: \$shouldIncludeToolDetail) {\n" +
            "    herdrSession { $SESSION_FIELDS } turns { herdrTurnRole turnText turnAt toolInputText toolResultText hasToolDetail } isTruncated\n  }\n}"
    const val READ_HERDR_SCREEN =
        "query (\$computer: String, \$paneId: String!, \$shouldKeepColor: Boolean) {\n" +
            "  ReadAgentHerdrScreen(computer: \$computer, paneId: \$paneId, shouldKeepColor: \$shouldKeepColor) {\n    herdrSession { $SESSION_FIELDS } screenText\n  }\n}"
    const val READ_HERDR_SUBAGENTS =
        "query (\$computer: String!, \$paneId: String!) {\n  ReadAgentHerdrSubagents(computer: \$computer, paneId: \$paneId) {\n" +
            "    herdrSession { $SESSION_FIELDS } sessionModelName requestStartedAt isTruncated\n" +
            "    subagents { subagentId parentSubagentId subagentType subagentName taskText modelName subagentState spawnDepth startedAt endedAt toolNames toolCallCount }\n  }\n}"
    const val SEND_HERDR_SESSION =
        "mutation (\$computer: String, \$paneId: String!, \$text: String, \$attachmentIds: [String!]) {\n" +
            "  SendAgentHerdrSession(computer: \$computer, paneId: \$paneId, text: \$text, attachmentIds: \$attachmentIds) { $SESSION_FIELDS }\n}"
    const val FOCUS_HERDR_SESSION =
        "mutation (\$computer: String, \$paneId: String!) {\n  FocusAgentHerdrSession(computer: \$computer, paneId: \$paneId) { $SESSION_FIELDS }\n}"
    const val WATCH_HERDR_SESSION =
        "mutation (\$computer: String, \$paneId: String!) {\n  WatchAgentHerdrSession(computer: \$computer, paneId: \$paneId) { $SESSION_FIELDS }\n}"
    const val OPEN_HERDR_SESSION =
        "mutation (\$computer: String, \$directory: String, \$codingAgentKind: String, \$agentName: String, \$shouldSkipPermissions: Boolean, \$paneId: String) {\n" +
            "  OpenAgentHerdrSession(computer: \$computer, directory: \$directory, codingAgentKind: \$codingAgentKind, agentName: \$agentName, shouldSkipPermissions: \$shouldSkipPermissions, paneId: \$paneId) { $SESSION_FIELDS }\n}"
    const val CLOSE_HERDR_SESSION =
        "mutation (\$computer: String, \$paneId: String!) {\n  CloseAgentHerdrSession(computer: \$computer, paneId: \$paneId) { $SESSION_FIELDS }\n}"
}

/** Runs a document and decodes the field the answer holds. */
internal suspend fun <Answer> Connection.field(document: String, field: String, deserializer: KSerializer<Answer>, variables: Map<String, JsonElement> = emptyMap()): Answer {
    val answer: JsonObject = execute(document, variables)
    val element = answer[field] ?: throw TeaNodeError.UnreadableAnswer
    return try {
        TeaNodeJson.decodeFromJsonElement(deserializer, element)
    } catch (_: Exception) {
        throw TeaNodeError.UnreadableAnswer
    }
}

/** Every coding session on the person's computers, or on the one named. */
suspend fun Connection.herdrSessions(computer: String? = null): HerdrList {
    val variables = mutableMapOf<String, JsonElement>()
    computer?.let { name -> variables["computer"] = JsonPrimitive(name) }
    return field(HerdrDocuments.LIST_HERDR_SESSIONS, "ListAgentHerdrSessions", serializer(), variables)
}

/** The last `turnCount` turns of a session (100 at most), with each tool call's input and result when `shouldIncludeToolDetail`. */
suspend fun Connection.readHerdrSession(computer: String, paneId: String, turnCount: Int, shouldIncludeToolDetail: Boolean): HerdrRead = field(
    HerdrDocuments.READ_HERDR_SESSION, "ReadAgentHerdrSession", serializer(),
    mapOf(
        "computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId), "turnCount" to JsonPrimitive(turnCount),
        "shouldIncludeToolDetail" to JsonPrimitive(shouldIncludeToolDetail),
    ),
)

/** A session's screen as it stands. */
suspend fun Connection.readHerdrScreen(computer: String, paneId: String): HerdrScreen = field(
    HerdrDocuments.READ_HERDR_SCREEN, "ReadAgentHerdrScreen", serializer(),
    mapOf("computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId), "shouldKeepColor" to JsonPrimitive(true)),
)

/** The agents a session started for its current request, as a flat list; [HerdrSubagents.tree] puts them under the agents that started them. */
suspend fun Connection.readHerdrSubagents(computer: String, paneId: String): HerdrSubagents = field(
    HerdrDocuments.READ_HERDR_SUBAGENTS, "ReadAgentHerdrSubagents", serializer(),
    mapOf("computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId)),
)

/** Types text into a session and presses enter, with files the person uploaded already (their ids) sent along. */
suspend fun Connection.sendHerdrSession(computer: String, paneId: String, text: String, attachmentIds: List<String> = emptyList()): HerdrSession {
    val variables = mutableMapOf<String, JsonElement>("computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId), "text" to JsonPrimitive(text))
    if (attachmentIds.isNotEmpty()) variables["attachmentIds"] = kotlinx.serialization.json.JsonArray(attachmentIds.map { identifier -> JsonPrimitive(identifier) })
    return field(HerdrDocuments.SEND_HERDR_SESSION, "SendAgentHerdrSession", serializer(), variables)
}

/** Brings a session's tab forward in herdr on its computer, so whoever sits there sees it. */
suspend fun Connection.focusHerdrSession(computer: String, paneId: String): HerdrSession = field(
    HerdrDocuments.FOCUS_HERDR_SESSION, "FocusAgentHerdrSession", serializer(),
    mapOf("computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId)),
)

/** Wakes the person's main conversation when the session next finishes its turn. */
suspend fun Connection.watchHerdrSession(computer: String, paneId: String): HerdrSession = field(
    HerdrDocuments.WATCH_HERDR_SESSION, "WatchAgentHerdrSession", serializer(),
    mapOf("computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId)),
)

/**
 * Starts a coding agent in a new pane. Given `paneId` it opens a new tab beside that pane, in its directory with its agent
 * unless `directory` and `codingAgentKind` say otherwise; without it, `directory` and `codingAgentKind` say where and which.
 */
suspend fun Connection.openHerdrSession(
    computer: String,
    paneId: String? = null,
    directory: String? = null,
    codingAgentKind: String? = null,
    agentName: String? = null,
    shouldSkipPermissions: Boolean = false,
): HerdrSession {
    val variables = mutableMapOf<String, JsonElement>("computer" to JsonPrimitive(computer))
    paneId?.let { value -> variables["paneId"] = JsonPrimitive(value) }
    directory?.let { value -> variables["directory"] = JsonPrimitive(value) }
    codingAgentKind?.let { value -> variables["codingAgentKind"] = JsonPrimitive(value) }
    agentName?.let { value -> variables["agentName"] = JsonPrimitive(value) }
    if (shouldSkipPermissions) variables["shouldSkipPermissions"] = JsonPrimitive(true)
    return field(HerdrDocuments.OPEN_HERDR_SESSION, "OpenAgentHerdrSession", serializer(), variables)
}

/** Ends a session's coding agent and closes its pane; refused while it works. */
suspend fun Connection.closeHerdrSession(computer: String, paneId: String): HerdrSession = field(
    HerdrDocuments.CLOSE_HERDR_SESSION, "CloseAgentHerdrSession", serializer(),
    mapOf("computer" to JsonPrimitive(computer), "paneId" to JsonPrimitive(paneId)),
)

/** The document that answers a coding session's question (ios/TeaNodeKit Operations.swift). */
object CodingQuestionDocuments {
    const val ANSWER_CODING_QUESTION =
        "mutation (\$computer: String, \$paneId: String!, \$questionFingerprint: String!, \$optionNumbers: [Int!], \$optionLabels: [String!], \$freeText: String) {\n" +
            "  AnswerAgentHerdrQuestion(computer: \$computer, paneId: \$paneId, questionFingerprint: \$questionFingerprint, optionNumbers: \$optionNumbers, optionLabels: \$optionLabels, freeText: \$freeText) { isAnswerAccepted answeredWith }\n}"
}

@Serializable
private data class AnsweredCodingQuestion(val isAnswerAccepted: Boolean, val answeredWith: String)

/** Answers a coding session's question: the options chosen, or what to type. Returns how the session took it, in its words. */
suspend fun Connection.answerCodingQuestion(question: CodingQuestion, options: List<CodingQuestion.Option>, freeText: String?): String {
    val variables = mutableMapOf<String, JsonElement>(
        "computer" to JsonPrimitive(question.computer), "paneId" to JsonPrimitive(question.paneId),
        "questionFingerprint" to JsonPrimitive(question.questionFingerprint),
    )
    if (freeText != null) {
        variables["freeText"] = JsonPrimitive(freeText)
    } else {
        variables["optionNumbers"] = kotlinx.serialization.json.JsonArray(options.map { option -> JsonPrimitive(option.optionNumber) })
        variables["optionLabels"] = kotlinx.serialization.json.JsonArray(options.map { option -> JsonPrimitive(option.optionLabel) })
    }
    val answered: AnsweredCodingQuestion = field(CodingQuestionDocuments.ANSWER_CODING_QUESTION, "AnswerAgentHerdrQuestion", serializer(), variables)
    if (!answered.isAnswerAccepted) throw TeaNodeError.Refused("The coding session did not take that answer. It may have moved on.")
    return answered.answeredWith
}

/** One computer's workspaces, and what its sessions are doing, for its heading in the list. */
data class CodingComputer(val name: String, val workspaces: List<CodingWorkspace>) {
    val sessions: List<HerdrSession> get() = workspaces.flatMap { workspace -> workspace.sessions }
    val askingCount: Int get() = sessions.count { session -> session.isAsking }
    val workingCount: Int get() = sessions.count { session -> session.isWorking }
}

/** The coding sessions of one herdr workspace on one computer, in tab order. */
data class CodingWorkspace(val id: String, val computer: String, val title: String, val sessions: List<HerdrSession>)

/** The list of coding sessions grouped as the list shows them: by computer in name order, then workspace number, then tab number. */
fun HerdrList.computers(): List<CodingComputer> {
    val computerList = computerNames.toMutableList()
    for (session in sessions) if (!computerList.contains(session.computer)) computerList.add(session.computer)
    computerList.sortWith(String.CASE_INSENSITIVE_ORDER)
    val result = mutableListOf<CodingComputer>()
    for (computer in computerList) {
        val onComputer = sessions.filter { session -> session.computer == computer }
        val seen = mutableListOf<String>()
        for (session in onComputer) if (!seen.contains(session.workspaceId)) seen.add(session.workspaceId)
        val grouped = seen.map { workspaceId ->
            val inWorkspace = onComputer.filter { session -> session.workspaceId == workspaceId }.sortedBy { session -> session.tabNumber }
            CodingWorkspace(
                id = "$computer/$workspaceId", computer = computer,
                // A computer program too old to say where a pane is gives no workspace: its sessions are listed under no heading.
                title = if (workspaceId.isEmpty()) "" else inWorkspace.firstOrNull()?.workspaceTitle ?: computer,
                sessions = inWorkspace,
            )
        }
        val workspaces = grouped.sortedBy { workspace -> workspace.sessions.firstOrNull()?.workspaceNumber ?: 0 }
        if (workspaces.isNotEmpty()) result.add(CodingComputer(computer, workspaces))
    }
    return result
}

/** The list after one session as the server just answered it is put in (or replaces its old self), so a change shows before the list is read again. */
fun HerdrList.upserting(session: HerdrSession): HerdrList {
    val updated = sessions.toMutableList()
    val index = updated.indexOfFirst { held -> held.id == session.id }
    if (index >= 0) updated[index] = session else updated.add(session)
    return copy(sessions = updated)
}

fun HerdrList.removing(sessionId: String): HerdrList = copy(sessions = sessions.filter { session -> session.id != sessionId })

/**
 * The list as read now, keeping the sessions last known for a computer that did not answer this time, so a session open on
 * screen does not vanish for one slow answer.
 */
fun HerdrList.carryingFailedComputersFrom(previous: HerdrList?): HerdrList {
    if (previous == null || failedComputerNames.isEmpty()) return this
    val carried = previous.sessions.filter { session -> failedComputerNames.contains(session.computer) }
    return copy(sessions = sessions.filter { session -> !failedComputerNames.contains(session.computer) } + carried)
}
