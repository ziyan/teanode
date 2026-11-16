package com.teanode.kit.coding

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CodingSetupTest {
    private fun connection(body: String): Pair<Connection, FakeServer> {
        val fake = FakeServer { _ -> 200 to body }
        val account = Account(ServerAddress.parse("assistant.example.com"), "test-client", "tnt_test", "", Long.MAX_VALUE / 4, "robin")
        return Connection(account, fake) to fake
    }

    private fun sentVariables(fake: FakeServer): Map<String, JsonElement> =
        ((TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.toString(Charsets.UTF_8)) as JsonObject)["variables"] as JsonObject).toMap()

    @Test fun `setting up hooks decodes what was installed`() = runBlocking {
        val (connection, fake) = connection(
            """{"data":{"SetUpAgentHerdrHooks":{"computer":"laptop","isInstalled":true,"settingsPath":"~/.claude/settings.json","scriptPath":"~/.local/share/teanode/teanode-herdr-hook","backupPath":"~/.claude/settings.json.bak","hookEventNames":["SessionStart","Stop"]}}}""",
        )
        val setup = connection.setUpHerdrHooks("laptop", false)
        assertEquals("laptop", setup.computer)
        assertTrue(setup.isInstalled)
        assertEquals("~/.claude/settings.json", setup.settingsPath)
        assertEquals(listOf("SessionStart", "Stop"), setup.hookEventNames)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("laptop"), variables["computer"])
        assertNull(variables["isRemoval"])
    }

    @Test fun `removing hooks says so and decodes an empty backup`() = runBlocking {
        val (connection, fake) = connection(
            """{"data":{"SetUpAgentHerdrHooks":{"computer":"desk","isInstalled":false,"settingsPath":"~/.claude/settings.json","scriptPath":"","backupPath":"","hookEventNames":[]}}}""",
        )
        val setup = connection.setUpHerdrHooks("desk", true)
        assertFalse(setup.isInstalled)
        assertEquals("", setup.backupPath)
        assertEquals(emptyList<String>(), setup.hookEventNames)
        assertEquals(JsonPrimitive(true), sentVariables(fake)["isRemoval"])
    }

    @Test fun `a preview without a prompt reads the session start context`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"ReadAgentCodingContext":{"projectPath":"projects/recipe-box","text":"Project: recipe-box"}}}""")
        val preview = connection.codingContextPreview("~/src/recipe-box", "laptop", null)
        assertEquals("projects/recipe-box", preview.projectPath)
        assertEquals("Project: recipe-box", preview.text)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("~/src/recipe-box"), variables["directory"])
        assertEquals(JsonPrimitive("laptop"), variables["computerName"])
        assertNull(variables["prompt"])
    }

    @Test fun `a preview with a prompt recalls and decodes nothing to show`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"RecallAgentCodingMemory":{"projectPath":null,"text":""}}}""")
        val preview = connection.codingContextPreview("~/src/unknown", null, "Fix the footer")
        assertNull(preview.projectPath)
        assertEquals("", preview.text)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("Fix the footer"), variables["prompt"])
        assertNull(variables["computerName"])
    }

    @Test fun `reading new sessions decodes whether a source was asked`() = runBlocking {
        val (connection, fake) = connection("""{"data":{"CaptureAgentCodingSession":true}}""")
        assertTrue(connection.captureCodingSession("laptop", "claude-code"))
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("laptop"), variables["computerName"])
        assertEquals(JsonPrimitive("claude-code"), variables["assistant"])
        val (other, _) = connection("""{"data":{"CaptureAgentCodingSession":false}}""")
        assertFalse(other.captureCodingSession("desk", "codex"))
    }
}
