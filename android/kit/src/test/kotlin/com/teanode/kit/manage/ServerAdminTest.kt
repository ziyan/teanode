package com.teanode.kit.manage

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.ZoneOffset

class ServerAdminTest {
    private val section = TeaNodeJson.parseToJsonElement(
        """{"enabled":true,"bucket":"mail","port":9000,"ratio":0.5,"endpoint":null,"trustedSenders":["a.example","b.example"],"hasSecretAccessKey":true,"providers":[{"id":"x"}]}""",
    ).jsonObject

    @Test
    fun eachFieldGetsTheFormItsValueNeeds() {
        val fields = ServerSettingsForm.fields(section)
        assertEquals(ServerSettingField.Switch("enabled", true), fields[0])
        assertEquals(ServerSettingField.Text("bucket", "mail"), fields[1])
        assertEquals(ServerSettingField.Number("port", "9000", true), fields[2])
        assertEquals(ServerSettingField.Number("ratio", "0.5", false), fields[3])
        assertEquals(ServerSettingField.Text("endpoint", ""), fields[4])
        assertEquals(ServerSettingField.TextList("trustedSenders", "a.example, b.example"), fields[5])
        assertEquals(ServerSettingField.Secret("secretAccessKey", true), fields[6])
        assertTrue(fields[7] is ServerSettingField.Shown)
    }

    @Test
    fun aSecretIsNeverReadBackAndIsSentOnlyWhenTypedOrCleared() {
        val none = ServerSettingsForm.changes(section, emptyMap(), emptyMap(), emptyMap(), emptySet())
        assertTrue(none.isEmpty())
        val typed = ServerSettingsForm.changes(section, emptyMap(), emptyMap(), mapOf("secretAccessKey" to "new"), emptySet())
        assertEquals(JsonPrimitive("new"), typed["secretAccessKey"])
        val cleared = ServerSettingsForm.changes(section, emptyMap(), emptyMap(), emptyMap(), setOf("secretAccessKey"))
        assertEquals(JsonPrimitive(""), cleared["secretAccessKey"])
        assertFalse(section.toString().contains("hasSecretAccessKeyValue"))
    }

    @Test
    fun onlyWhatChangedIsSentAsItsType() {
        val changes = ServerSettingsForm.changes(
            section, mapOf("bucket" to "mail", "port" to "9001", "ratio" to "0.75", "trustedSenders" to "a.example, c.example"), mapOf("enabled" to false), emptyMap(), emptySet(),
        )
        assertEquals(setOf("enabled", "port", "ratio", "trustedSenders"), changes.keys)
        assertEquals(JsonPrimitive(9001L), changes["port"])
        assertEquals(JsonArray(listOf(JsonPrimitive("a.example"), JsonPrimitive("c.example"))), changes["trustedSenders"])
    }

    @Test
    fun aNumberIsValidWhenEmptyOrOfTheFieldsKind() {
        assertTrue(ServerSettingsForm.isNumberValid("", true))
        assertTrue(ServerSettingsForm.isNumberValid("12", true))
        assertFalse(ServerSettingsForm.isNumberValid("1.5", true))
        assertTrue(ServerSettingsForm.isNumberValid("1.5", false))
        assertFalse(ServerSettingsForm.isNumberValid("x", false))
    }

    @Test
    fun theUsageRangeEndsAtTheStartOfTomorrow() {
        val bounds = usageBounds(30, Instant.parse("2030-02-10T15:00:00Z"), ZoneOffset.UTC)
        assertEquals("2030-01-11T00:00:00Z", bounds.first)
        assertEquals("2030-02-11T00:00:00Z", bounds.second)
    }

    @Test
    fun theStatusAndUpgradeReadAsTheServerNamesThem() {
        val status = TeaNodeJson.decodeFromString(ServerStatus.serializer(), """{"instance":"i","version":"1.0","commit":"abcdef0123456789","supervision":"unknown","restarting":true}""")
        assertEquals("abcdef012345", status.shortCommit)
        assertTrue(status.isUnsupervised)
        assertTrue(status.isRestarting)
        val upgrade = TeaNodeJson.decodeFromString(ServerUpgrade.serializer(), """{"current":"1.0","latest":"1.1","available":true,"applicable":true}""")
        assertTrue(upgrade.canUpgrade)
        assertFalse(upgrade.copy(isApplicable = false).canUpgrade)
        assertEquals(1_000_000L, AgentUsageTotals(promptTokens = 400_000, completionTokens = 300_000, cacheReadTokens = 200_000, cacheWriteTokens = 50_000, audioInputTokens = 50_000).totalTokenCount)
        assertTrue(JsonObject(emptyMap()).isEmpty())
    }
}
