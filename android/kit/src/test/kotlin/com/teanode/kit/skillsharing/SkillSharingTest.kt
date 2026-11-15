package com.teanode.kit.skillsharing

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class SkillSharingTest {
    @Test
    fun moneyIsWrittenInTheCurrencysOwnDigits() {
        assertEquals("12.50 USD", SkillShareMoney.format(1250, "usd"))
        assertEquals("1000 JPY", SkillShareMoney.format(1000, "jpy"))
        assertEquals("1.234 KWD", SkillShareMoney.format(1234, "kwd"))
        assertEquals("-0.05 EUR", SkillShareMoney.format(-5, "eur"))
        assertEquals("1000.00", SkillShareMoney.typedExample("usd"))
        assertEquals("1000", SkillShareMoney.typedExample("jpy"))
    }

    @Test
    fun aTypedAmountIsPositiveAndNoMorePreciseThanTheCurrency() {
        assertEquals(1250L, SkillShareMoney.parse("12.5", "usd"))
        assertEquals(1200L, SkillShareMoney.parse("12", "usd"))
        assertNull(SkillShareMoney.parse("12.", "usd"))
        assertNull(SkillShareMoney.parse("12.345", "usd"))
        assertNull(SkillShareMoney.parse("0", "usd"))
        assertNull(SkillShareMoney.parse("-3", "usd"))
        assertNull(SkillShareMoney.parse("1.5", "jpy"))
        assertEquals(1500L, SkillShareMoney.parse("1500", "jpy"))
    }

    @Test
    fun aSkillCanBeLentOnlyWhenOnReadableAndWithTools() {
        assertEquals(true, LendableSkill("a", enabled = true, readable = true, tools = listOf(LendableTool("t"))).isLendable)
        assertEquals(false, LendableSkill("a", enabled = true, readable = true).isLendable)
        assertEquals(false, LendableSkill("a", enabled = false, readable = true, tools = listOf(LendableTool("t"))).isLendable)
    }

    @Test
    fun aCallWaitsForApprovalAndNamesTheGrantee() {
        val call = SkillShareCall("c", callState = "waiting_for_approval", granteeUsername = "sam", amountMinor = 500, currencyCode = "usd")
        assertEquals(true, call.isWaiting)
        assertEquals("sam", call.shownGrantee)
        assertEquals("5.00 USD", call.amountText)
        assertEquals("", call.copy(amountMinor = null).amountText)
    }

    @Test
    fun aReadOfACallIsFoundOnlyWhenTheShareListsIt() {
        val waiting = SkillShareCall(id = "call-1", callState = "waiting_for_approval")
        assertEquals(SkillShareCallReading.Found(waiting), SkillShareCallReading.reading("call-1", Result.success(listOf(waiting))))
        assertEquals(SkillShareCallReading.Gone, SkillShareCallReading.reading("other", Result.success(listOf(waiting))))
    }

    @Test
    fun aCallIsGoneOnlyWhenTheServerAnswersThatItIsNotThere() {
        assertEquals(SkillShareCallReading.Gone, SkillShareCallReading.reading("call-1", Result.failure(com.teanode.kit.TeaNodeError.Refused("api: not found"))))
        // A read that did not get through says nothing about the call.
        assertEquals(SkillShareCallReading.Failed, SkillShareCallReading.reading("call-1", Result.failure(java.io.IOException("offline"))))
        assertEquals(SkillShareCallReading.Failed, SkillShareCallReading.reading("call-1", Result.failure(com.teanode.kit.TeaNodeError.Refused("api: forbidden"))))
        assertEquals(SkillShareCallReading.Failed, SkillShareCallReading.reading("call-1", Result.failure(com.teanode.kit.TeaNodeError.UnreadableAnswer)))
    }

    @Test
    fun theNoteOfTheMainConversationNamesTheCall() {
        val note = com.teanode.kit.chats.SkillShareNote.parsing("""{"skillShareId":"share-1","callId":"call-1","granteeName":"Sam Example","granteeUserId":"sam-id"}""")
        assertEquals("call-1", note?.callId)
        assertEquals("share-1", note?.skillShareId)
        assertNull(com.teanode.kit.chats.SkillShareNote.parsing("not json"))
        assertNull(com.teanode.kit.chats.SkillShareNote.parsing("""{"callId":"call-1"}"""))
    }
}
