package com.teanode.kit.coding

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.serializer

// What the Coding section sets up and previews beyond the sessions: the reporting hooks teanode puts into Claude Code on a
// computer, what a coding session is shown, and reading new sessions now. The same operations as the dashboard's Coding
// page and Knowledge page, and the command line.

/** What installing or removing the hooks did on one computer. */
@Serializable
data class HerdrHooksSetup(
    val computer: String,
    val isInstalled: Boolean,
    val settingsPath: String,
    val scriptPath: String,
    val backupPath: String,
    val hookEventNames: List<String>,
)

/** The text a coding session would be shown. */
@Serializable
data class CodingContextPreview(
    /** The page of the checkout the directory is in; empty where memory knows none. */
    val projectPath: String? = null,
    /** The whole of it as the session reads it; empty where there is nothing to show. */
    val text: String,
)

/** The GraphQL documents of the coding setup: the same text as the iPhone app's (CodingSetup.swift). */
object CodingSetupDocuments {
    const val SET_UP_HERDR_HOOKS =
        "mutation (\$computer: String, \$isRemoval: Boolean) {\n" +
            "  SetUpAgentHerdrHooks(computer: \$computer, isRemoval: \$isRemoval) { computer isInstalled settingsPath scriptPath backupPath hookEventNames }\n}"
    const val READ_CODING_CONTEXT =
        "query (\$directory: String!, \$computerName: String) {\n  ReadAgentCodingContext(directory: \$directory, computerName: \$computerName) { projectPath text }\n}"
    const val RECALL_CODING_MEMORY =
        "query (\$prompt: String!, \$directory: String!, \$computerName: String) {\n" +
            "  RecallAgentCodingMemory(prompt: \$prompt, directory: \$directory, computerName: \$computerName) { projectPath text }\n}"
    const val CAPTURE_CODING_SESSION =
        "mutation (\$computerName: String!, \$assistant: String!) {\n  CaptureAgentCodingSession(computerName: \$computerName, assistant: \$assistant)\n}"
}

/** Puts teanode's hooks into Claude Code's settings on a computer, or takes them out again with `isRemoval`. */
suspend fun Connection.setUpHerdrHooks(computer: String, isRemoval: Boolean): HerdrHooksSetup {
    val variables = mutableMapOf<String, JsonElement>("computer" to JsonPrimitive(computer))
    if (isRemoval) variables["isRemoval"] = JsonPrimitive(true)
    return field(CodingSetupDocuments.SET_UP_HERDR_HOOKS, "SetUpAgentHerdrHooks", serializer(), variables)
}

/**
 * What a session starting in a directory is shown; with a prompt, what that prompt recalls from the directory's project
 * instead. Nothing is marked as used.
 */
suspend fun Connection.codingContextPreview(directory: String, computerName: String?, prompt: String?): CodingContextPreview {
    val variables = mutableMapOf<String, JsonElement>("directory" to JsonPrimitive(directory))
    computerName?.let { name -> variables["computerName"] = JsonPrimitive(name) }
    if (prompt != null) {
        variables["prompt"] = JsonPrimitive(prompt)
        return field(CodingSetupDocuments.RECALL_CODING_MEMORY, "RecallAgentCodingMemory", serializer(), variables)
    }
    return field(CodingSetupDocuments.READ_CODING_CONTEXT, "ReadAgentCodingContext", serializer(), variables)
}

/** Asks the sources reading a coding tool's transcripts on a computer to read again now. `assistant` is "claude-code" or "codex". False when no source fits. */
suspend fun Connection.captureCodingSession(computerName: String, assistant: String): Boolean = field(
    CodingSetupDocuments.CAPTURE_CODING_SESSION, "CaptureAgentCodingSession", serializer(),
    mapOf("computerName" to JsonPrimitive(computerName), "assistant" to JsonPrimitive(assistant)),
)
