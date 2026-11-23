package com.teanode.kit.agent

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class KnowledgeSourcesTest {
    private val type = KnowledgeSourceType(
        "wiki", reader = "web",
        settings = listOf(
            KnowledgeSourceType.Setting("baseUrl", isRequired = true, pattern = "^https://"),
            KnowledgeSourceType.Setting("pageLimit", settingType = "integer", minimum = 1, maximum = 100),
            KnowledgeSourceType.Setting("spaces", settingType = "array", itemPattern = "^[A-Z]+$"),
            KnowledgeSourceType.Setting("isRecursive", settingType = "boolean", defaultValue = JsonPrimitive(true)),
            KnowledgeSourceType.Setting("mode", pattern = "^(fast|full)$"),
        ),
        secrets = listOf(KnowledgeSourceType.Secret("apiToken")),
    )

    @Test
    fun aFormStartsFromWhatIsStoredOrTheDefault() {
        val draft = type.draft(mapOf("baseUrl" to JsonPrimitive("https://wiki.example.com"), "pageLimit" to JsonPrimitive(20), "spaces" to JsonArray(listOf(JsonPrimitive("A"), JsonPrimitive("B")))))
        assertEquals("https://wiki.example.com", draft.texts["baseUrl"])
        assertEquals("20", draft.texts["pageLimit"])
        assertEquals("A, B", draft.texts["spaces"])
        assertEquals(true, draft.switches["isRecursive"])
    }

    @Test
    fun theFirstProblemSaysWhichSettingAndWhy() {
        val empty = type.draft()
        assertEquals(KnowledgeSourceType.SettingProblem.Missing, type.firstProblem(empty)!!.second)
        val base = empty.copy(texts = empty.texts + ("baseUrl" to "https://x"))
        assertNull(type.firstProblem(base))
        assertEquals(KnowledgeSourceType.SettingProblem.Refused, type.firstProblem(base.copy(texts = base.texts + ("baseUrl" to "http://x")))!!.second)
        assertEquals(KnowledgeSourceType.SettingProblem.NotWhole, type.firstProblem(base.copy(texts = base.texts + ("pageLimit" to "x")))!!.second)
        assertEquals(KnowledgeSourceType.SettingProblem.TooLarge(100), type.firstProblem(base.copy(texts = base.texts + ("pageLimit" to "101")))!!.second)
        assertEquals(KnowledgeSourceType.SettingProblem.ItemRefused("b"), type.firstProblem(base.copy(texts = base.texts + ("spaces" to "A, b")))!!.second)
    }

    @Test
    fun theValuesAreSentAsTheirTypes() {
        val draft = type.draft().let { start -> start.copy(texts = start.texts + mapOf("baseUrl" to " https://x ", "pageLimit" to "5", "spaces" to "A, B")) }
        val values = type.values(draft)
        assertEquals(JsonPrimitive("https://x"), values["baseUrl"])
        assertEquals(JsonPrimitive(5), values["pageLimit"])
        assertEquals(JsonArray(listOf(JsonPrimitive("A"), JsonPrimitive("B"))), values["spaces"])
        assertEquals(JsonPrimitive(true), values["isRecursive"])
        assertFalse(values.containsKey("mode"))
    }

    @Test
    fun aChoicePatternListsItsWords() {
        assertEquals(listOf("fast", "full"), type.settings.last().choices)
        assertNull(KnowledgeSourceType.choicesIn("^https://"))
        assertNull(KnowledgeSourceType.choicesIn("^(only)$"))
    }

    @Test
    fun aSettingNameBecomesALabel() {
        assertEquals("Base URL", KnowledgeSourceType.label("baseUrl"))
        assertEquals("Page limit", KnowledgeSourceType.label("pageLimit"))
        assertEquals("API token", KnowledgeSourceType.label("apiToken"))
        assertEquals("Reads the pages.", KnowledgeSourceType.asSentence("reads the pages"))
        assertEquals("Done!", KnowledgeSourceType.asSentence("done!"))
    }

    @Test
    fun theBuiltInReadersComeFirstAndUnreadableTypesAreNotOffered() {
        val offered = KnowledgeSourceType.offered(listOf(KnowledgeSourceType("zeta", reader = "x"), KnowledgeSourceType("web", reader = "web"), KnowledgeSourceType("files", reader = "files"), KnowledgeSourceType("bad", readable = false)))
        assertEquals(listOf("files", "web", "zeta"), offered.map { offeredType -> offeredType.name })
    }

    @Test
    fun storedSettingsAreReadWhetherAnObjectOrAText() {
        val asText = TeaNodeJson.decodeFromString(KnowledgeSourceSpecification.serializer(), """{"type":"wiki","settings":"{\"baseUrl\":\"https://x\"}"}""")
        assertEquals(JsonPrimitive("https://x"), asText.storedSettings["baseUrl"])
        val asObject = TeaNodeJson.decodeFromString(KnowledgeSourceSpecification.serializer(), """{"type":"wiki","settings":{"pageLimit":3}}""")
        assertEquals(JsonPrimitive(3), asObject.storedSettings["pageLimit"])
        assertTrue(KnowledgeSourceSpecification().storedSettings.isEmpty())
    }

    @Test
    fun aChangeSendsOnlyWhatItSets() {
        assertEquals(setOf("sourceId", "enabled"), KnowledgeSourceChange(sourceId = "s", enabled = false).variables.keys)
    }
}
