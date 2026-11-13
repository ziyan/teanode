package com.teanode.kit.agent

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentUpkeepTest {
    private fun dream(vararg changes: Pair<String, Int>): AgentDream {
        val base = AgentDream("d", startedAt = "2030-01-01T01:00:00Z", finishedAt = "2030-01-01T01:20:00Z")
        return changes.fold(base) { dream, (name, count) ->
            when (name) {
                "digested" -> dream.copy(digested = count)
                "filed" -> dream.copy(filed = count)
                "rehearsed" -> dream.copy(rehearsed = count)
                "dormant" -> dream.copy(dormant = count)
                else -> dream
            }
        }
    }

    @Test
    fun aDreamListsOnlyWhatItDidInTheOrderOfThePage() {
        val outcomes = dream("filed" to 2, "digested" to 5, "dormant" to 1).outcomes
        assertEquals(listOf(AgentDream.OutcomeKind.Read, AgentDream.OutcomeKind.Filed, AgentDream.OutcomeKind.Retired), outcomes.map { outcome -> outcome.outcomeKind })
        assertTrue(dream().outcomes.isEmpty())
    }

    @Test
    fun rehearsingShowsTheGapsAndUnknownsEvenAtZero() {
        val kinds = dream("rehearsed" to 3).outcomes.map { outcome -> outcome.outcomeKind }
        assertEquals(listOf(AgentDream.OutcomeKind.Rehearsed, AgentDream.OutcomeKind.Gaps, AgentDream.OutcomeKind.Unknown), kinds)
    }

    @Test
    fun aDreamSaysHowLongItTookOnceItIsOver() {
        assertEquals(20, dream().minutesTaken)
        assertNull(dream().copy(finishedAt = null).minutesTaken)
    }

    @Test
    fun theDreamStateIsIdleQueuedOrRunning() {
        assertTrue(AgentDreamState().isIdle)
        assertTrue(AgentDreamState("queued").isQueued)
        assertTrue(AgentDreamState("running").isRunning)
    }

    @Test
    fun theUpkeepSettingsFallBackToTheDefaultsWhenATimeIsNotATime() {
        val preferences = AgentUpkeepPreferences("a", dreamFrom = "nine", alertQuietStart = "21:30", alertDailyMost = 0, lookAroundWeekday = "someday")
        assertEquals("01:00", preferences.shownDreamFrom)
        assertEquals("21:30", preferences.shownAlertQuietStart)
        assertEquals(5, preferences.shownAlertDailyMost)
        assertEquals("thursday", preferences.shownLookAroundWeekday)
        assertTrue(preferences.isRunning)
        assertFalse(preferences.copy(operatorDisabledAt = "2030-01-01T00:00:00Z").isRunning)
    }

    @Test
    fun aScheduleKnowsWhenItRunsOnMailAndHowItIsDelivered() {
        val schedule = TeaNodeJson.decodeFromString(AgentSchedule.serializer(), """{"id":"s","name":"Brief","cron":"0 8 * * *","deliver":"mail","mailConditions":[{"field":"from"}]}""")
        assertTrue(schedule.isDeliveredByMail)
        assertTrue(schedule.isTriggeredByMail)
        assertFalse(schedule.copy(mailConditions = null).isTriggeredByMail)
    }

    @Test
    fun aServerIsPersonalWhenItAsksTheUserToSignIn() {
        assertTrue(AgentConnectedServer("a", auth = "oauth").doesAuthorizeInBrowser)
        assertTrue(AgentConnectedServer("a", auth = "user").isPersonal)
        assertFalse(AgentConnectedServer("a", auth = "none").isPersonal)
        assertTrue(AgentConnectedServer("a", status = "connected").isConnected)
    }

    @Test
    fun backgroundWorkCanBeStoppedWhileQueuedOrRunningAndOpenedWhenOver() {
        assertTrue(AgentBackgroundWork("w", workStatus = "running").isStoppable)
        assertTrue(AgentBackgroundWork("w", workStatus = "done").isOpenable)
        assertFalse(AgentBackgroundWork("w", workStatus = "done").isStoppable)
    }

    @Test
    fun aRunCountsCachedTokensWithItsPrompt() {
        assertEquals(1500, AgentRunSummary("r", usage = AgentRunUsage(1000, 300, 0.0, 200)).tokenCount)
    }
}
