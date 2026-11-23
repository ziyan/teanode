package com.teanode.kit.agent

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentBillingTest {
    @Test
    fun aPriceIsChangedWhenTheBillDisagreesWithTheSettings() {
        assertTrue(AgentBilledPrice("model-a", "input", 1.0, 1.5, 7).isChanged)
        assertFalse(AgentBilledPrice("model-a", "input", 1.5, 1.5, 7).isChanged)
        assertEquals("model-a/output", AgentPriceChange("model-a", "output", 1.0, 2.0).identity)
    }

    @Test
    fun aProviderNeverCheckedHasNoTime() {
        val never = AgentBillingCheck("example", "0001-01-01T00:00:00Z")
        assertNull(never.checkedInstant)
        assertEquals(1_893_456_000L, AgentBillingCheck("example", "2030-01-01T00:00:00Z").checkedInstant!!.epochSecond)
    }

    @Test
    fun aCheckIsReadWithWhatTheServerLeftOutTakenAsEmpty() {
        val check = TeaNodeJson.decodeFromString(AgentBillingCheck.serializer(), """{"providerName":"example","checkedAt":"2030-01-01T00:00:00Z","compared":[{"model":"m","tokenKind":"input","configuredPerMillion":1,"billedPerMillion":2,"dayCount":3}]}""")
        assertEquals("", check.errorMessage)
        assertTrue(check.changed.isEmpty())
        assertFalse(check.compared[0].isPattern)
    }
}
