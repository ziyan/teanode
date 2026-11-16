package com.teanode.app.coding

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoRequest
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

/** One invented session of the demo: two computers with a workspace each, one session working, one asking, two idle. */
private fun demoSession(
    computer: String,
    paneId: String,
    state: String,
    directory: String,
    workspaceId: String,
    workspaceLabel: String,
    workspaceNumber: Int,
    tabLabel: String,
    tabNumber: Int,
    kind: String = "claude",
    isTabFocused: Boolean = false,
    question: String = "null",
    paneTitle: String = "",
    transcriptPath: String = "/work/transcripts/$paneId.jsonl",
): String =
    """{"computer":"$computer","paneId":"$paneId","paneName":"$workspaceLabel $tabNumber $kind","codingAgentKind":"$kind","codingSessionId":"session-$paneId","herdrSessionState":"$state","herdrAgentStatus":"$state","paneTitle":"$paneTitle","workingDirectory":"$directory","transcriptPath":"$transcriptPath","isWatched":false,"workspaceId":"$workspaceId","workspaceLabel":"$workspaceLabel","workspaceNumber":$workspaceNumber,"tabId":"$workspaceId:$tabNumber","tabLabel":"$tabLabel","tabNumber":$tabNumber,"isTabFocused":$isTabFocused,"question":$question}"""

private const val DEMO_QUESTION =
    """{"questionFingerprint":"fingerprint-1","herdrQuestionKind":"question","questionText":"Link style\nHow should the new Contact link look?","isMultipleChoice":false,"isFromTranscript":false,"options":[{"optionNumber":1,"optionLabel":"Plain text link","optionDescription":"Matches About and Privacy.","herdrOptionKind":"choice"},{"optionNumber":2,"optionLabel":"Button","optionDescription":"Stands out in the footer.","herdrOptionKind":"choice"},{"optionNumber":3,"optionLabel":"Type something","optionDescription":"","herdrOptionKind":"freeText"}]}"""

private val demoSessions: List<String> = listOf(
    demoSession("laptop", "p1", "working", "/work/website", "w1", "website", 1, "working", 1, paneTitle = "Add a Contact link to the footer"),
    demoSession("laptop", "p2", "asking", "/work/website", "w1", "website", 1, "asking", 2, isTabFocused = true, question = DEMO_QUESTION),
    demoSession("laptop", "p3", "idle", "/work/website", "w1", "website", 1, "idle", 3, kind = "codex"),
    demoSession("desk", "p4", "idle", "/work/notes", "w2", "notes", 1, "notes", 1, isTabFocused = true),
)

private fun demoSubagent(
    id: String,
    parent: String? = null,
    type: String,
    task: String,
    model: String,
    state: String,
    depth: Int,
    startedSecondsAgo: Long,
    endedSecondsAgo: Long? = null,
    tools: List<String>,
    toolCallCount: Int,
): String {
    val startedAt = Instant.now().minusSeconds(startedSecondsAgo)
    val endedAt = endedSecondsAgo?.let { seconds -> "\"" + Instant.now().minusSeconds(seconds).toString() + "\"" } ?: "null"
    val parentText = parent?.let { value -> "\"$value\"" } ?: "null"
    return """{"subagentId":"$id","parentSubagentId":$parentText,"subagentType":"$type","subagentName":null,"taskText":"$task","modelName":"$model","subagentState":"$state","spawnDepth":$depth,"startedAt":"$startedAt","endedAt":$endedAt,"toolNames":[${tools.joinToString(",") { tool -> "\"$tool\"" }}],"toolCallCount":$toolCallCount}"""
}

/** Seven invented agents of a session building a pricing page: a researcher with two agents under it, a writer and a coder with a tester and a reviewer under it. */
private fun demoSubagents(): List<String> = listOf(
    demoSubagent("researcher", type = "general-purpose", task = "Compare how three similar sites lay out their pricing tiers", model = "claude-sonnet-5-5", state = "running", depth = 1, startedSecondsAgo = 300, tools = listOf("WebFetch", "Read"), toolCallCount = 14),
    demoSubagent("finder", "researcher", "Explore", "Find the pricing table component in the repository", "claude-haiku-5-0", "done", 2, 290, 200, listOf("Grep", "Read", "Glob"), 9),
    demoSubagent("lister", "researcher", "Explore", "List the plan names and monthly prices the page must show", "claude-haiku-5-0", "running", 2, 150, null, listOf("Read"), 3),
    demoSubagent("writer", type = "technical-writer", task = "Draft the headline and a one-line description for each plan", model = "claude-sonnet-5-5", state = "done", depth = 1, startedSecondsAgo = 280, endedSecondsAgo = 120, tools = listOf("Read", "Write"), toolCallCount = 6),
    demoSubagent("coder", type = "general-purpose", task = "Build the pricing page component with three plan cards", model = "claude-sonnet-5-5", state = "running", depth = 1, startedSecondsAgo = 100, tools = listOf("Edit", "Write", "Bash"), toolCallCount = 21),
    demoSubagent("tester", "coder", "test-runner", "Run the pricing page tests and report what fails", "claude-haiku-5-0", "stopped", 2, 80, 40, listOf("Bash"), 5),
    demoSubagent("reviewer", "coder", "code-reviewer", "Review the new component for accessibility", "claude-opus-5-0", "running", 2, 60, null, listOf("Read", "Grep"), 4),
)

private const val DEMO_TURNS =
    """[{"herdrTurnRole":"person","turnText":"Add a Contact link to the footer, after About.","turnAt":null},{"herdrTurnRole":"agent","turnText":"I will look at the footer component first.","turnAt":null},{"herdrTurnRole":"tool","turnText":"Read src/Footer.tsx","turnAt":null,"toolInputText":"{\n  \"file_path\": \"src/Footer.tsx\"\n}","toolResultText":"export const footerLinks = [\"About\", \"Privacy\"]"},{"herdrTurnRole":"tool","turnText":"Bash: npm test -- --runInBand src/components/Footer.test.tsx src/components/Header.test.tsx --coverage","turnAt":null,"toolInputText":"{\n  \"command\": \"npm test -- --runInBand src/components/Footer.test.tsx src/components/Header.test.tsx --coverage\"\n}","toolResultText":"Tests: 12 passed, 12 total"},{"herdrTurnRole":"agent","turnText":"The footer lists its links from one array, so the new link is one more entry.\n\n| File | Change |\n|---|---|\n| `src/Footer.tsx` | Contact after About |\n| `src/Footer.test.tsx` | Snapshot updated |\n\n- The tests pass.\n- Nothing else uses the array.","turnAt":null}]"""

private const val DEMO_SCREEN = "$ npm test\\n \\u001b[1;42;30m PASS \\u001b[0m src/Footer.test.tsx\\n \\u001b[1mTests:\\u001b[22m \\u001b[32m4 passed\\u001b[39m, 4 total"

private fun sessionNamed(request: DemoRequest): String {
    val paneId = (request.variables["paneId"] as? JsonPrimitive)?.content ?: "p2"
    return demoSessions.firstOrNull { session -> session.contains("\"paneId\":\"$paneId\"") } ?: demoSessions[1]
}

/**
 * What demo mode answers for Coding: the sessions of two invented computers, a transcript, the screen, the agents a
 * session started, the hooks and what a session is shown. Most specific first.
 */
val codingDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ReadAgentHerdrSubagents") { _ ->
        """{"herdrSession":${demoSessions[0]},"sessionModelName":"claude-opus-5-0","requestStartedAt":"${Instant.now().minusSeconds(320)}","isTruncated":false,"subagents":[${demoSubagents().joinToString(",")}]}"""
    },
    DemoAnswerer { request ->
        when {
            request.document.contains("ListAgentHerdrSessions") ->
                DemoResponse("""{"data":{"ListAgentHerdrSessions":{"computerNames":["laptop","desk"],"failedComputerNames":[],"sessions":[${demoSessions.joinToString(",")}]}}}""")
            request.document.contains("ReadAgentHerdrSession(") ->
                DemoResponse("""{"data":{"ReadAgentHerdrSession":{"herdrSession":${sessionNamed(request)},"turns":$DEMO_TURNS,"isTruncated":false}}}""")
            request.document.contains("ReadAgentHerdrScreen") ->
                DemoResponse("""{"data":{"ReadAgentHerdrScreen":{"herdrSession":${sessionNamed(request)},"screenText":"$DEMO_SCREEN"}}}""")
            request.document.contains("SetUpAgentHerdrHooks") -> {
                val isInstalled = (request.variables["isRemoval"] as? JsonPrimitive)?.content != "true"
                val computer = (request.variables["computer"] as? JsonPrimitive)?.content ?: "laptop"
                DemoResponse(
                    """{"data":{"SetUpAgentHerdrHooks":{"computer":"$computer","isInstalled":$isInstalled,"settingsPath":"~/.claude/settings.json","scriptPath":"~/.local/share/teanode/teanode-herdr-hook","backupPath":"~/.claude/settings.json.bak","hookEventNames":["SessionStart","UserPromptSubmit","Stop"]}}}""",
                )
            }
            request.document.contains("AnswerAgentHerdrQuestion") ->
                DemoResponse("""{"data":{"AnswerAgentHerdrQuestion":{"isAnswerAccepted":true,"answeredWith":"Plain text link"}}}""")
            request.document.contains("CaptureAgentCodingSession") -> DemoResponse("""{"data":{"CaptureAgentCodingSession":true}}""")
            request.document.contains("RecallAgentCodingMemory") ->
                DemoResponse("""{"data":{"RecallAgentCodingMemory":{"projectPath":"projects/recipe-box","text":"Relevant to this prompt:\n- projects/recipe-box: the footer links come from one array in src/Footer.tsx.\n- Lesson: update the snapshot test when a link is added."}}}""")
            request.document.contains("ReadAgentCodingContext") ->
                DemoResponse("""{"data":{"ReadAgentCodingContext":{"projectPath":"projects/recipe-box","text":"Project: projects/recipe-box (~/src/recipe-box)\n- Tests run with npm test.\n- The footer lists its links from one array.\nLast session here stopped after adding the Contact link."}}}""")
            // Send, focus, watch, open and close answer with the session.
            else -> listOf("SendAgentHerdrSession", "FocusAgentHerdrSession", "WatchAgentHerdrSession", "OpenAgentHerdrSession", "CloseAgentHerdrSession")
                .firstOrNull { name -> request.document.contains(name) }
                ?.let { name -> DemoResponse("""{"data":{"$name":${sessionNamed(request)}}}""") }
        }
    },
)
