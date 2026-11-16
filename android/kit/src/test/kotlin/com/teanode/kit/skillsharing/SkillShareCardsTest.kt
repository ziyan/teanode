package com.teanode.kit.skillsharing

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class SkillShareCardsTest {
    private fun call(arguments: String, amountMinor: Long? = null, amountArgumentName: String? = null) =
        SkillShareCall(id = "c", toolArguments = arguments, amountMinor = amountMinor, currencyCode = "usd", amountArgumentName = amountArgumentName)

    @Test
    fun argumentsComeOneToALineSortedWithTheirNamesAsWords() {
        val lines = call("""{"pay_to":"Example Cafe","note":"lunch","count":3}""").argumentLines
        assertEquals(listOf("count" to "3", "note" to "lunch", "pay to" to "Example Cafe"), lines.map { line -> line.argumentName to line.argumentValue })
    }

    @Test
    fun theAmountTheCurrencyAndTheReasonAreLeftOutBecauseTheyAreShownApart() {
        val lines = call("""{"amount_minor":500,"currency":"usd","request_reason":"for lunch","to":"Example Cafe"}""").argumentLines
        assertEquals(listOf("to"), lines.map { line -> line.argumentName })
    }

    @Test
    fun theArgumentTheAmountIsReadFromIsLeftOutOnlyWhenAnAmountWasRead() {
        val declared = call("""{"total":"5.00","to":"x"}""", amountMinor = 500, amountArgumentName = "total")
        assertEquals(listOf("to"), declared.argumentLines.map { line -> line.argumentName })
        val unread = call("""{"total":"5.00","to":"x"}""", amountMinor = null, amountArgumentName = "total")
        assertEquals(listOf("to", "total"), unread.argumentLines.map { line -> line.argumentName })
    }

    @Test
    fun aValueIsKeptWholeAndEmptyOnesAreSkipped() {
        val long = "a".repeat(400)
        val lines = call("""{"body":"$long","empty":"","nothing":null,"list":[1,2],"flag":true}""").argumentLines
        assertEquals(long, lines.first { line -> line.argumentName == "body" }.argumentValue)
        assertEquals("[1,2]", lines.first { line -> line.argumentName == "list" }.argumentValue)
        assertEquals("true", lines.first { line -> line.argumentName == "flag" }.argumentValue)
        assertTrue(lines.none { line -> line.argumentName == "empty" || line.argumentName == "nothing" })
    }

    @Test
    fun argumentsThatAreNotAnObjectGiveNoLines() {
        assertTrue(call("not json").argumentLines.isEmpty())
        assertTrue(call("[1,2]").argumentLines.isEmpty())
    }
}
