package com.teanode.kit.manage

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentAdminTest {
    @Test
    fun anAgentSwitchedOffByAnOperatorSaysSo() {
        val summary = TeaNodeJson.decodeFromString(AgentAdminSummary.serializer(), """{"agentId":"a","username":"robin","name":"","operatorDisabledAt":"2030-01-01T00:00:00Z","dailyTokens":1000}""")
        assertTrue(summary.isSwitchedOff)
        assertEquals("robin", summary.shownName)
        assertFalse(summary.copy(operatorDisabledAt = null).isSwitchedOff)
    }

    @Test
    fun limitsAreTypedAsDigitsAndDecimals() {
        assertEquals(0L, AgentLimitDraft().dailyTokens)
        assertEquals(5000L, AgentLimitDraft("5000", "").dailyTokens)
        assertEquals(1.5, AgentLimitDraft("", "1,5").dailyCost, 0.0001)
        assertEquals(0.0, AgentLimitDraft("", "-2").dailyCost, 0.0001)
        assertTrue(AgentLimitDraft("", "").isValid)
        assertFalse(AgentLimitDraft("12x", "").isValid)
    }

    @Test
    fun theDraftStartsFromTheLimitsTheAgentHas() {
        val draft = AgentLimitDraft.of(AgentAdminSummary("a", dailyTokens = 2000, dailyCost = 0.0))
        assertEquals("2000", draft.dailyTokensText)
        assertEquals("", draft.dailyCostText)
    }
}
