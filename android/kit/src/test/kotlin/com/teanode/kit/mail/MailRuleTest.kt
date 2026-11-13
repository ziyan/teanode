package com.teanode.kit.mail

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class MailRuleTest {
    @Test
    fun decodesTheServersRule() {
        val json = """
            {"name":"Receipts","enabled":true,"stop":false,
             "conditions":[{"field":"subject","header":"","operator":"contains","value":"receipt"},{"field":"any","header":null,"operator":null,"value":null}],
             "actions":[{"kind":"move","folderId":"folder-1","address":""}]}
        """
        val rule = TeaNodeJson.decodeFromString(MailRule.serializer(), json)
        assertEquals("Receipts", rule.name)
        assertEquals("contains", rule.conditions[0].comparisonOperator)
        assertTrue(rule.conditions[1].isBare)
        assertEquals("folder-1", rule.actions[0].folderId)
    }

    private fun fields(vararg pairs: Pair<String, String>) = JsonObject(pairs.associate { pair -> pair.first to JsonPrimitive(pair.second) })

    @Test
    fun sendsEachPartOnlyWhereItMeansSomething() {
        val rule = MailRule(
            name = "Mixed",
            conditions = listOf(
                MailRule.Condition("from", "X-Left-Over", "contains", "example.com"),
                MailRule.Condition("needs-reply", null, "contains", ""),
                MailRule.Condition("header", "List-Id", "matches", "news"),
            ),
            actions = listOf(MailRule.Action("forward", "folder-1", "someone@example.com"), MailRule.Action("move", "folder-2", "left@example.com")),
        )
        val sent = rule.variables
        val conditions = sent["conditions"]!!.jsonArray
        val actions = sent["actions"]!!.jsonArray
        assertEquals(fields("field" to "from", "header" to "", "operator" to "contains", "value" to "example.com"), conditions[0])
        assertEquals(fields("field" to "needs-reply", "header" to "", "operator" to "", "value" to ""), conditions[1])
        assertEquals(fields("field" to "header", "header" to "List-Id", "operator" to "matches", "value" to "news"), conditions[2])
        assertEquals(fields("kind" to "forward", "folderId" to "", "address" to "someone@example.com"), actions[0])
        assertEquals(fields("kind" to "move", "folderId" to "folder-2", "address" to ""), actions[1])
    }

    @Test
    fun operatorsFollowTheField() {
        assertEquals(listOf("above", "below"), MailRule.Condition.operators("score"))
        assertEquals(listOf("equals"), MailRule.Condition.operators("priority"))
        assertEquals(listOf(""), MailRule.Condition.operators("any"))
        assertEquals(listOf("contains", "equals", "matches"), MailRule.Condition.operators("subject"))
    }

    @Test
    fun anAutoReplyFormSendsPlainTextAndNullsForNoDates() {
        val sent = MailAutoReplyDraft(isEnabled = true, subject = "Away", text = "Back soon").variables
        assertEquals(JsonPrimitive(""), sent["html"])
        assertEquals(kotlinx.serialization.json.JsonNull, sent["from"])
        val reply = Mailbox.AutoReply(enabled = true, from = "2030-01-02T09:00:00.123456789Z", subject = "Away", text = "Back")
        assertEquals(java.time.Instant.parse("2030-01-02T09:00:00Z"), MailAutoReplyDraft.of(reply).from)
    }
}
