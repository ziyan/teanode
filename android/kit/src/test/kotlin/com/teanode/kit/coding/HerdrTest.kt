package com.teanode.kit.coding

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

class HerdrTest {
    private val sessionJson = """
        {"computer":"laptop","paneId":"w1-2","paneName":"website 2 claude","codingAgentKind":"claude","codingSessionId":"s1",
         "herdrSessionState":"asking","herdrAgentStatus":"blocked","paneTitle":"","workingDirectory":"/work/website","transcriptPath":"",
         "isWatched":false,"workspaceId":"w1","workspaceLabel":"website","workspaceNumber":1,"tabId":"w1:2","tabLabel":"asking","tabNumber":2,
         "isTabFocused":true,
         "question":{"questionFingerprint":"f1","herdrQuestionKind":"question","questionText":"Which fruit?","isMultipleChoice":false,
           "isFromTranscript":false,"options":[
             {"optionNumber":1,"optionLabel":"Apple","optionDescription":"","herdrOptionKind":"choice"},
             {"optionNumber":2,"optionLabel":"Type something","optionDescription":"","herdrOptionKind":"freeText"}]}}
    """.trimIndent()

    private fun connection(body: String): Pair<Connection, FakeServer> {
        val fake = FakeServer { _ -> 200 to body }
        val account = Account(ServerAddress.parse("assistant.example.com"), "test-client", "tnt_test", "", Long.MAX_VALUE / 4, "robin")
        return Connection(account, fake) to fake
    }

    private fun sentVariables(fake: FakeServer): Map<String, JsonElement> {
        val body = TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.toString(Charsets.UTF_8)) as JsonObject
        return (body["variables"] as JsonObject).toMap()
    }

    private fun decodedSession(paneTitle: String, tabLabel: String): HerdrSession {
        val json = sessionJson.replace("\"paneTitle\":\"\"", "\"paneTitle\":\"$paneTitle\"").replace("\"tabLabel\":\"asking\"", "\"tabLabel\":\"$tabLabel\"")
        return TeaNodeJson.decodeFromString(HerdrSession.serializer(), json)
    }

    @Test fun `the list decodes sessions with their placement`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"ListAgentHerdrSessions":{"computerNames":["laptop","desk"],"failedComputerNames":["desk"],"sessions":[$sessionJson]}}}""")
        val list = connection.herdrSessions()
        assertEquals(listOf("laptop", "desk"), list.computerNames)
        assertEquals(listOf("desk"), list.failedComputerNames)
        val session = list.sessions.first()
        assertEquals("website", session.workspaceLabel)
        assertEquals(2, session.tabNumber)
        assertTrue(session.isTabFocused)
        assertTrue(session.isAsking)
        assertEquals("asking", session.tabTitle)
        assertEquals(emptyMap<String, JsonElement>(), sentVariables(fake))
    }

    @Test fun `a row leads with what the session is about`() {
        val titled = decodedSession("Fix the footer", "2")
        assertEquals("Fix the footer", titled.rowTitle)
        assertEquals("tab 2 · Claude Code", titled.rowDetail)
        val prompt = decodedSession("robin@laptop: ~/work/website", "2")
        assertEquals("tab 2", prompt.rowTitle)
        assertEquals("Claude Code", prompt.rowDetail)
    }

    @Test fun `the question keeps its answering path with the conversations card`() = runBlocking {
        val (connection, _) = connection("""{"data":{"ListAgentHerdrSessions":{"computerNames":["laptop"],"failedComputerNames":[],"sessions":[$sessionJson]}}}""")
        val question = connection.herdrSessions("laptop").sessions.first().codingQuestion!!
        assertEquals("laptop", question.computer)
        assertEquals("w1-2", question.paneId)
        assertEquals("f1", question.questionFingerprint)
        assertEquals(listOf("Apple"), question.choices.map { option -> option.optionLabel })
    }

    @Test fun `a read asks for the turns and decodes them`() = runBlocking {
        val (connection, fake) = connection(
            """{"data":{"ReadAgentHerdrSession":{"herdrSession":$sessionJson,"turns":[{"herdrTurnRole":"person","turnText":"hello","turnAt":"2026-01-01T00:00:00Z"},{"herdrTurnRole":"tool","turnText":"Read a file","turnAt":null,"toolInputText":"{\"file_path\": \"a.txt\"}","toolResultText":"hello"},{"herdrTurnRole":"tool","turnText":"Ran a command","hasToolDetail":true}],"isTruncated":true}}}""",
        )
        val read = connection.readHerdrSession("laptop", "w1-2", 50, true)
        assertEquals(listOf("person", "tool", "tool"), read.turns.map { turn -> turn.herdrTurnRole })
        assertNull(read.turns[1].turnAt)
        assertFalse(read.turns[0].hasToolDetail)
        assertTrue(read.turns[1].hasToolDetail)
        assertEquals("hello", read.turns[1].toolResultText)
        // A read without the detail still says there is some to fetch.
        assertTrue(read.turns[2].hasToolDetail)
        assertFalse(read.turns[2].isToolDetailLoaded)
        assertTrue(read.isTruncated)
        assertEquals(
            mapOf("computer" to JsonPrimitive("laptop"), "paneId" to JsonPrimitive("w1-2"), "turnCount" to JsonPrimitive(50), "shouldIncludeToolDetail" to JsonPrimitive(true)),
            sentVariables(fake),
        )
    }

    @Test fun `the screen is read`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"ReadAgentHerdrScreen":{"herdrSession":$sessionJson,"screenText":"$ ready"}}}""")
        assertEquals("$ ready", connection.readHerdrScreen("laptop", "w1-2").screenText)
        assertEquals(mapOf("computer" to JsonPrimitive("laptop"), "paneId" to JsonPrimitive("w1-2"), "shouldKeepColor" to JsonPrimitive(true)), sentVariables(fake))
    }

    @Test fun `send carries the text and only the attachments there are`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"SendAgentHerdrSession":$sessionJson}}""")
        connection.sendHerdrSession("laptop", "w1-2", "go on")
        assertEquals(mapOf("computer" to JsonPrimitive("laptop"), "paneId" to JsonPrimitive("w1-2"), "text" to JsonPrimitive("go on")), sentVariables(fake))
        connection.sendHerdrSession("laptop", "w1-2", "see", listOf("a1", "a2"))
        assertEquals(JsonArray(listOf(JsonPrimitive("a1"), JsonPrimitive("a2"))), sentVariables(fake)["attachmentIds"])
    }

    @Test fun `focus watch and close name the session`() = runBlocking {
        val expected = mapOf("computer" to JsonPrimitive("laptop"), "paneId" to JsonPrimitive("w1-2"))
        val (focusing, focusFake) = connection("""{"data":{"FocusAgentHerdrSession":$sessionJson}}""")
        focusing.focusHerdrSession("laptop", "w1-2")
        assertEquals(expected, sentVariables(focusFake))
        val (watching, watchFake) = connection("""{"data":{"WatchAgentHerdrSession":$sessionJson}}""")
        watching.watchHerdrSession("laptop", "w1-2")
        assertEquals(expected, sentVariables(watchFake))
        val (closing, closeFake) = connection("""{"data":{"CloseAgentHerdrSession":$sessionJson}}""")
        closing.closeHerdrSession("laptop", "w1-2")
        assertEquals(expected, sentVariables(closeFake))
    }

    @Test fun `open beside a pane sends the pane and leaves the rest to the server`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"OpenAgentHerdrSession":$sessionJson}}""")
        connection.openHerdrSession("laptop", paneId = "w1-2", codingAgentKind = "codex")
        assertEquals(mapOf("computer" to JsonPrimitive("laptop"), "paneId" to JsonPrimitive("w1-2"), "codingAgentKind" to JsonPrimitive("codex")), sentVariables(fake))
    }

    @Test fun `open in a directory carries the directory and skips permissions only when asked`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"OpenAgentHerdrSession":$sessionJson}}""")
        connection.openHerdrSession("laptop", directory = "/work/notes", codingAgentKind = "claude")
        val plain = sentVariables(fake)
        assertEquals(JsonPrimitive("/work/notes"), plain["directory"])
        assertNull(plain["paneId"])
        assertNull(plain["shouldSkipPermissions"])
        connection.openHerdrSession("laptop", directory = "/work/notes", codingAgentKind = "claude", shouldSkipPermissions = true)
        assertEquals(JsonPrimitive(true), sentVariables(fake)["shouldSkipPermissions"])
    }

    // Agents

    private fun subagentJson(
        id: String,
        parent: String? = null,
        type: String = "general-purpose",
        state: String = "done",
        depth: Int = 1,
        startedAt: String? = "2026-01-01T10:00:00Z",
        endedAt: String? = "2026-01-01T10:01:30Z",
    ): String {
        val parentJson = parent?.let { value -> "\"$value\"" } ?: "null"
        val startedJson = startedAt?.let { value -> "\"$value\"" } ?: "null"
        val endedJson = endedAt?.let { value -> "\"$value\"" } ?: "null"
        return """{"subagentId":"$id","parentSubagentId":$parentJson,"subagentType":"$type","subagentName":null,"taskText":"Task of $id","modelName":"model-a","subagentState":"$state","spawnDepth":$depth,"startedAt":$startedJson,"endedAt":$endedJson,"toolNames":["Read","Grep"],"toolCallCount":7}"""
    }

    private fun decodedSubagents(list: List<String>): List<HerdrSubagent> = list.map { json -> TeaNodeJson.decodeFromString(HerdrSubagent.serializer(), json) }

    @Test fun `the agents are read and decoded`() = runBlocking {
        val running = subagentJson("a2", parent = "a1", type = "Explore", state = "running", depth = 2, endedAt = null)
        val (connection, fake) = connection(
            """{"data":{"ReadAgentHerdrSubagents":{"herdrSession":$sessionJson,"sessionModelName":"model-b","requestStartedAt":"2026-01-01T09:59:00Z","isTruncated":true,"subagents":[${subagentJson("a1")},$running]}}}""",
        )
        val read = connection.readHerdrSubagents("laptop", "w1-2")
        assertEquals("model-b", read.sessionModelName)
        assertEquals("2026-01-01T09:59:00Z", read.requestStartedAt)
        assertTrue(read.isTruncated)
        assertTrue(read.herdrSession.isAsking)
        assertEquals(listOf("a1", "a2"), read.subagents.map { subagent -> subagent.subagentId })
        assertNull(read.subagents[0].parentId)
        assertEquals("a1", read.subagents[1].parentId)
        assertEquals("Explore", read.subagents[1].subagentType)
        assertEquals(2, read.subagents[1].spawnDepth)
        assertTrue(read.subagents[1].isRunning)
        assertNull(read.subagents[1].endedAt)
        assertEquals(listOf("Read", "Grep"), read.subagents[0].toolNames)
        assertEquals(7, read.subagents[0].toolCallCount)
        assertEquals(1, read.runningCount)
        assertEquals(mapOf("computer" to JsonPrimitive("laptop"), "paneId" to JsonPrimitive("w1-2")), sentVariables(fake))
        val document = HerdrDocuments.READ_HERDR_SUBAGENTS
        assertTrue(document.contains("ReadAgentHerdrSubagents("))
        for (field in listOf("subagentId", "parentSubagentId", "subagentType", "subagentName", "taskText", "modelName", "subagentState", "spawnDepth", "startedAt", "endedAt", "toolNames", "toolCallCount", "sessionModelName", "requestStartedAt", "isTruncated")) {
            assertTrue(document.contains(field), field)
        }
    }

    @Test fun `an empty parent means the session started the agent`() {
        val json = subagentJson("a1").replace("\"parentSubagentId\":null", "\"parentSubagentId\":\"\"")
        assertNull(TeaNodeJson.decodeFromString(HerdrSubagent.serializer(), json).parentId)
    }

    @Test fun `a duration runs to the end or to now`() {
        val finished = decodedSubagents(listOf(subagentJson("a1")))[0]
        assertEquals(90.0, finished.durationSeconds(System.currentTimeMillis()))
        val running = decodedSubagents(listOf(subagentJson("a2", state = "running", endedAt = null)))[0]
        val now = java.time.Instant.parse("2026-01-01T10:02:00Z").toEpochMilli()
        assertEquals(120.0, running.durationSeconds(now))
        val unstarted = decodedSubagents(listOf(subagentJson("a3", startedAt = null, endedAt = null)))[0]
        assertNull(unstarted.durationSeconds(now))
    }

    @Test fun `the tree puts agents under the agent that started them in start order`() {
        val subagents = decodedSubagents(
            listOf(
                subagentJson("writer", startedAt = "2026-01-01T10:00:20Z"),
                subagentJson("finder-two", parent = "researcher", depth = 2, startedAt = "2026-01-01T10:00:15Z"),
                subagentJson("researcher", startedAt = "2026-01-01T10:00:05Z"),
                subagentJson("finder-one", parent = "researcher", depth = 2, startedAt = "2026-01-01T10:00:10Z"),
                subagentJson("checker", parent = "finder-one", depth = 3, startedAt = "2026-01-01T10:00:12Z"),
            ),
        )
        val tree = HerdrSubagentNode.tree(subagents)
        assertEquals(listOf("researcher", "writer"), tree.map { node -> node.id })
        assertEquals(listOf("finder-one", "finder-two"), tree[0].children.map { node -> node.id })
        assertEquals(listOf("checker"), tree[0].children[0].children.map { node -> node.id })
        assertTrue(tree[0].children[1].children.isEmpty())
        assertTrue(tree[1].children.isEmpty())
        assertEquals(3, tree[0].descendantCount)
        assertEquals(0, tree[1].descendantCount)
    }

    @Test fun `an agent whose parent is missing goes under the session`() {
        val subagents = decodedSubagents(
            listOf(
                subagentJson("orphan", parent = "gone", depth = 2, startedAt = "2026-01-01T10:00:01Z"),
                subagentJson("first", startedAt = "2026-01-01T10:00:00Z"),
                subagentJson("child", parent = "orphan", depth = 3, startedAt = "2026-01-01T10:00:02Z"),
            ),
        )
        val tree = HerdrSubagentNode.tree(subagents)
        assertEquals(listOf("first", "orphan"), tree.map { node -> node.id })
        assertEquals(listOf("child"), tree[1].children.map { node -> node.id })
    }

    @Test fun `agents without a start time come last in the order given`() {
        val subagents = decodedSubagents(
            listOf(
                subagentJson("no-start-one", startedAt = null, endedAt = null),
                subagentJson("started", startedAt = "2026-01-01T10:00:00Z"),
                subagentJson("no-start-two", startedAt = null, endedAt = null),
                subagentJson("same-time", startedAt = "2026-01-01T10:00:00Z"),
            ),
        )
        assertEquals(listOf("started", "same-time", "no-start-one", "no-start-two"), HerdrSubagentNode.tree(subagents).map { node -> node.id })
    }

    @Test fun `agents that name each other as parents are not lost`() {
        val subagents = decodedSubagents(
            listOf(
                subagentJson("loop-a", parent = "loop-b", depth = 2, startedAt = "2026-01-01T10:00:00Z"),
                subagentJson("loop-b", parent = "loop-a", depth = 2, startedAt = "2026-01-01T10:00:01Z"),
                subagentJson("self", parent = "self", startedAt = "2026-01-01T10:00:02Z"),
            ),
        )
        val tree = HerdrSubagentNode.tree(subagents)
        assertEquals(listOf("self", "loop-a"), tree.map { node -> node.id })
        assertEquals(listOf("loop-b"), tree[1].children.map { node -> node.id })
        assertTrue(HerdrSubagentNode.tree(emptyList()).isEmpty())
    }

    @Test fun `every document names its operation`() {
        val names = mapOf(
            HerdrDocuments.LIST_HERDR_SESSIONS to "ListAgentHerdrSessions", HerdrDocuments.READ_HERDR_SESSION to "ReadAgentHerdrSession",
            HerdrDocuments.READ_HERDR_SCREEN to "ReadAgentHerdrScreen", HerdrDocuments.SEND_HERDR_SESSION to "SendAgentHerdrSession",
            HerdrDocuments.FOCUS_HERDR_SESSION to "FocusAgentHerdrSession", HerdrDocuments.WATCH_HERDR_SESSION to "WatchAgentHerdrSession",
            HerdrDocuments.OPEN_HERDR_SESSION to "OpenAgentHerdrSession", HerdrDocuments.CLOSE_HERDR_SESSION to "CloseAgentHerdrSession",
            HerdrDocuments.READ_HERDR_SUBAGENTS to "ReadAgentHerdrSubagents",
        )
        for ((document, name) in names) {
            assertTrue(document.contains("$name("), name)
            assertTrue(document.contains("isTabFocused"), name)
        }
    }

    // The headline of a coding question

    private fun question(text: String) = CodingQuestion("a laptop", "w1:p1", "f1", "claude", "site", text, false, emptyList())

    @Test fun `a tool approval is headed by the tool and what it is for`() {
        val approval = question(
            "Bash command\nRun the build's tests\nmake -C build test && make -C build lint\n⚠ Claude Code will automatically deny this request in 0:12, to avoid blocking progress on an unattended session\nDo you want to proceed?",
        )
        assertEquals("Bash command · Run the build's tests", approval.headline)
        assertFalse(approval.shownText.contains("automatically deny"))
        assertTrue(approval.shownText.contains("make -C build test"))
    }

    @Test fun `a question is headed by the line that asks`() {
        assertEquals("Which fruit should we pick?", question("Fruit\nWhich fruit should we pick?").headline)
        assertEquals("Which fruit should we pick?", question("Which fruit should we pick?").headline)
    }

    @Test fun `a long headline is cut`() {
        val long = question("word ".repeat(60) + "?")
        assertEquals(158, long.headline.length)
        assertTrue(long.headline.endsWith("…"))
    }

    // The answer to a coding question and the list's grouping

    @Test fun `a refused answer is shown in its words`() {
        val (connection, _) = connection("""{"errors":[{"message":"that question was already answered"}],"data":{"AnswerAgentHerdrQuestion":null}}""")
        val asked = CodingQuestion("desk", "pane-1", "print", "claude", "notes", "Which one?", false, emptyList())
        val error = assertThrows<com.teanode.kit.TeaNodeError.Refused> { runBlocking { connection.answerCodingQuestion(asked, emptyList(), "the first") } }
        assertEquals("that question was already answered", error.message)
    }

    @Test fun `an answer carries the options chosen or the typed text`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"AnswerAgentHerdrQuestion":{"isAnswerAccepted":true,"answeredWith":"Apple"}}}""")
        val asked = CodingQuestion("desk", "pane-1", "print", "claude", "notes", "Which one?", false, listOf(CodingQuestion.Option(1, "Apple", "", "choice")))
        assertEquals("Apple", connection.answerCodingQuestion(asked, asked.choices, null))
        val chosen = sentVariables(fake)
        assertEquals(JsonArray(listOf(JsonPrimitive(1))), chosen["optionNumbers"])
        assertEquals(JsonArray(listOf(JsonPrimitive("Apple"))), chosen["optionLabels"])
        assertNull(chosen["freeText"])
        connection.answerCodingQuestion(asked, emptyList(), "Pear")
        val typed = sentVariables(fake)
        assertEquals(JsonPrimitive("Pear"), typed["freeText"])
        assertNull(typed["optionNumbers"])
    }

    private fun session(computer: String, paneId: String, workspaceId: String, workspaceNumber: Int, tabNumber: Int, state: String = "idle"): HerdrSession =
        decodedSession("", "t").copy(
            computer = computer, paneId = paneId, workspaceId = workspaceId, workspaceLabel = if (workspaceId.isEmpty()) "" else "ws $workspaceId",
            workspaceNumber = workspaceNumber, tabNumber = tabNumber, herdrSessionState = state, question = null,
        )

    @Test fun `the list groups by computer in name order then workspace then tab`() {
        val list = HerdrList(
            computerNames = listOf("laptop", "desk"), failedComputerNames = emptyList(),
            sessions = listOf(
                session("laptop", "p3", "w1", 1, 3), session("laptop", "p1", "w1", 1, 1, "working"), session("laptop", "p9", "w2", 2, 1),
                session("desk", "p4", "w5", 1, 1), session("laptop", "p0", "", 0, 1),
            ),
        )
        val computers = list.computers()
        assertEquals(listOf("desk", "laptop"), computers.map { computer -> computer.name })
        val laptop = computers[1]
        assertEquals(listOf("", "ws w1", "ws w2"), laptop.workspaces.map { workspace -> workspace.title })
        assertEquals(listOf("p1", "p3"), laptop.workspaces[1].sessions.map { session -> session.paneId })
        assertEquals(1, laptop.workingCount)
        assertEquals(0, laptop.askingCount)
    }

    @Test fun `a computer that did not answer keeps the sessions last known for it`() {
        val before = HerdrList(listOf("a", "b"), emptyList(), listOf(session("a", "p1", "w", 1, 1), session("b", "p2", "w", 1, 1)))
        val read = HerdrList(listOf("a", "b"), listOf("b"), listOf(session("a", "p1", "w", 1, 1)))
        assertEquals(listOf("a/p1", "b/p2"), read.carryingFailedComputersFrom(before).sessions.map { entry -> entry.id })
        assertEquals(read, read.carryingFailedComputersFrom(null))
        assertEquals(listOf("a/p1", "b/p2", "a/p3"), before.upserting(session("a", "p3", "w", 1, 2)).sessions.map { entry -> entry.id })
        assertEquals(listOf("b/p2"), before.removing("a/p1").sessions.map { entry -> entry.id })
    }
}
