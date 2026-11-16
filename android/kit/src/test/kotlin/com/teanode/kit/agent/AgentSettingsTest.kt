package com.teanode.kit.agent

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentSettingsTest {
    private val page = """{"agent":{"id":"agent-1","name":"Juniper","enabled":true,"instructions":"Sign as R.","language":"","knowledgeLanguage":"en","askModel":"","speechVoice":"",
      "operatorDisabledAt":null,"confirm":["calendar","mail_send"],"unattendedAllowedRisks":["money"],"isMemoryCheckEnabled":true,"isIdeasEnabled":false,
      "voice":{"tone":"formal","length":null,"greeting":"Hello","signoff":null},"categories":[{"name":"club","description":"The sailing club"}]},
      "sources":[{"mailboxId":"mailbox-1","name":"Robin","addresses":["robin@example.com"],"policy":{"granted":true,"draftReplies":true,"search":false,"research":false,
        "triage":{"enabled":true,"backfill":"","replyExpectation":"any"},"summaries":null,"autoReply":{"enabled":false,"guidance":null},"alerts":null}},
        {"mailboxId":"mailbox-2","name":"Club","addresses":["club@example.org"],"policy":null}],
      "collections":[{"id":"calendar-1","name":"Home","kind":"calendar","granted":true,"items":12},{"id":"book-1","name":"Contacts","kind":"addressBook","granted":false,"items":40}],
      "allowed":{"enabled":true,"triage":true},"budget":{"used":1200,"limit":100000,"cost":0.25,"costLimit":0,"currency":"USD"},
      "choices":["model-a"],"timezone":"Europe/Berlin","language":"en","knowledgeLanguage":"en","categories":["personal","work","newsletter","notification","receipt","promotion","social","invitation","phishing","junk","other","club"]}"""

    private fun read() = TeaNodeJson.decodeFromString(AgentSettings.serializer(), page)

    @Test
    fun thePageIsReadWithWhatTheServerLeftOutTakenAsEmpty() {
        val settings = read()
        assertEquals("Juniper", settings.agent?.name)
        assertFalse(settings.agent!!.isOperatorDisabled)
        assertTrue(settings.source("mailbox-1")!!.isGranted)
        assertFalse(settings.source("mailbox-2")!!.isGranted)
        assertEquals(MailboxPolicy.defaultPolicy, settings.source("mailbox-2")!!.shownPolicy)
        assertTrue(settings.collections[0].isCalendar)
        assertFalse(settings.allowed.ask)
        assertEquals(9, settings.fixedCategoryNames.size)
        val empty = TeaNodeJson.decodeFromString(AgentSettings.serializer(), "{}")
        assertNull(empty.agent)
        assertFalse(empty.allowed.enabled)
    }

    @Test
    fun theBudgetIsAsFullAsItsCloserLimit() {
        assertEquals(0.012, read().budget!!.fraction!!, 0.0001)
        assertNull(AgentBudget(used = 5.0).fraction)
        assertEquals(0.5, AgentBudget(used = 1.0, limit = 10.0, cost = 5.0, costLimit = 10.0).fraction!!, 0.0001)
    }

    @Test
    fun aChangeSendsOnlyWhatItChangesAsTheDashboardSendsIt() {
        assertEquals(emptyMap<String, Any>(), AgentChange().variables)
        val variables = AgentChange(
            name = " Juniper ", language = " de ", voice = AgentVoice(tone = "formal"),
            categories = listOf(AgentCategory("club", "The club"), AgentCategory("  ", "dropped")), confirm = listOf("mail"),
        ).variables
        assertEquals(JsonPrimitive("Juniper"), variables["name"])
        assertEquals(JsonPrimitive("de"), variables["language"])
        assertEquals(JsonPrimitive(""), variables["voice"]!!.jsonObject["length"])
        assertEquals(1, variables["categories"]!!.jsonArray.size)
        assertEquals(setOf("name", "language", "voice", "categories", "confirm"), variables.keys)
    }

    @Test
    fun aPolicyIsCleanedBeforeItIsSent() {
        val unset = MailboxPolicy(draftReplies = true).cleaned
        assertTrue(unset.granted)
        assertNull(unset.autoReply)
        val outside = MailboxPolicy(autoReply = MailboxAutoReply(enabled = true, `when` = "outsideHours")).cleaned
        assertEquals(MailboxHours.workingHours, outside.autoReply!!.hours)
        val always = MailboxPolicy(autoReply = MailboxAutoReply(enabled = true, `when` = "always", hours = MailboxHours("08:00", "12:00", listOf(1)))).cleaned
        assertNull(always.autoReply!!.hours)
        val sent = outside.variables.jsonObject
        assertEquals(JsonPrimitive(true), sent["granted"])
        assertEquals("09:00", sent["autoReply"]!!.jsonObject["hours"]!!.jsonObject["from"]!!.let { (it as JsonPrimitive).content })
        assertEquals(JsonNull, always.variables.jsonObject["autoReply"]!!.jsonObject["hours"])
        assertTrue(MailboxPolicy().isAlertsEnabled)
        assertFalse(MailboxPolicy(alerts = MailboxAlerts(false)).isAlertsEnabled)
    }

    @Test
    fun listsNumbersAndTimesAreTypedAsTheServerTakesThem() {
        assertEquals(listOf("a", "b", "c"), AgentSettings.splitList("a, b\n c ,,"))
        assertEquals("a, b", AgentSettings.joinList(listOf("a", "b")))
        assertEquals("", AgentSettings.joinList(null))
        assertEquals(0, AgentSettings.wholeNumber("x"))
        assertEquals(0, AgentSettings.wholeNumber("-4"))
        assertEquals(12, AgentSettings.wholeNumber(" 12 "))
        assertEquals(9 to 5, AgentSettings.clockParts("9:05"))
        assertNull(AgentSettings.clockParts("24:00"))
        assertNull(AgentSettings.clockParts("nine"))
        assertEquals("09:05", AgentSettings.clockText(9, 5))
    }

    @Test
    fun theAskFirstListFollowsAToolsFamilyUnlessTheToolIsNamed() {
        val tools = listOf(AgentTool("mail_send", "mail"), AgentTool("mail_search", "mail"), AgentTool("calendar", "calendar"))
        assertEquals(listOf("mail", "calendar"), AgentTool.families(tools))
        var draft = AgentConfirmDraft.of(listOf("mail", "mail"))
        assertEquals(listOf("mail"), draft.askFirstNames)
        assertTrue(draft.doesToolAskFirst(tools[0]))
        assertFalse(draft.doesToolAskFirst(tools[2]))
        draft = draft.settingAskingFirst("calendar", true).settingAskingFirst("mail", false)
        assertEquals(listOf("calendar"), draft.askFirstNames)
        assertEquals(draft, draft.settingAskingFirst("calendar", true))
    }

    @Test
    fun whatMayBeDoneWhenAwayIsSentInTheOrderOfTheSettings() {
        val draft = AgentUnattendedDraft.of(null).setting("listed", true).setting("outward", true)
        assertEquals(listOf("outward", "listed"), draft.orderedAllowedRisks)
        assertEquals(setOf("money"), AgentUnattendedDraft.of(read().agent).allowedRisks)
    }
}
