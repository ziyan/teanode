package com.teanode.kit.notifications

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class TurnUpdateTest {
    private fun data(state: String, isEnded: String = "false", changedAt: String = "1700000000") = mapOf(
        "pushKind" to "turn", "activityState" to state, "isActivityEnded" to isEnded, "activityChangedAt" to changedAt,
    )

    @Test
    fun aTurnMessageIsRead() {
        val update = TurnUpdate.parse(data("waiting"))!!
        assertEquals("waiting", update.activityState)
        assertFalse(update.isActivityEnded)
        assertEquals(1700000000L, update.activityChangedAtEpochSeconds)
        val ended = TurnUpdate.parse(data("done", isEnded = "true"))!!
        assertTrue(ended.isActivityEnded)
    }

    @Test
    fun aStateTheServerDoesNotHaveIsNotShown() {
        assertNull(TurnUpdate.parse(data("exploding")))
        assertNull(TurnUpdate.parse(mapOf("pushKind" to "turn")))
    }

    @Test
    fun aMissingTimeIsTheEpochSoAnythingNewerWins() {
        val update = TurnUpdate.parse(data("working", changedAt = "soon"))!!
        assertEquals(0L, update.activityChangedAtEpochSeconds)
        assertTrue(TurnUpdate.parse(data("working"))!!.isNewerThan(update.activityChangedAtEpochSeconds))
    }

    @Test
    fun anOlderUpdateDoesNotOverwriteANewerOne() {
        val late = TurnUpdate.parse(data("done", isEnded = "true", changedAt = "1700000100"))!!
        val early = TurnUpdate.parse(data("working", changedAt = "1700000050"))!!
        assertFalse(early.isNewerThan(late.activityChangedAtEpochSeconds))
        assertTrue(late.isNewerThan(early.activityChangedAtEpochSeconds))
        assertTrue(late.isNewerThan(late.activityChangedAtEpochSeconds))
    }

    @Test
    fun theServerIsToldWhichRunAndWhichRegistration() = runBlocking {
        val fake = FakeServer { 200 to """{"data":{"RegisterAgentLiveActivity":true}}""" }
        val connection = Connection(Account(ServerAddress.parse("https://example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira"), fake)
        connection.registerLiveActivity("run-1", "a-turn-registration")
        val body = TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.decodeToString()) as JsonObject
        val variables = body["variables"] as JsonObject
        assertEquals(JsonPrimitive("run-1"), variables["runId"])
        assertEquals(JsonPrimitive("a-turn-registration"), variables["pushRegistration"])
        assertTrue((body["query"] as JsonPrimitive).content.contains("RegisterAgentLiveActivity(runId: \$runId, pushRegistration: \$pushRegistration)"))
    }
}
