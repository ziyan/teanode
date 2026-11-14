package com.teanode.kit.agent

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentStatus
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentConnectionsTest {
    private val laptop = AgentStatus.Computer("laptop")
    private val desk = AgentStatus.Computer("desk")

    @Test
    fun aReachThroughNoComputerGoesThroughThisServer() {
        val options = AgentReach("skill", "weather").options(listOf(laptop))
        assertEquals(listOf("" to AgentReach.OptionKind.ThroughThisServer, "laptop" to AgentReach.OptionKind.ThroughComputer), options.map { option -> option.computerName to option.optionKind })
    }

    @Test
    fun aServerOnAComputerWithSeveralAttachedAsksForAChoice() {
        val reach = AgentReach("server", "notes", isOnComputer = true)
        assertTrue(reach.needsChoice(2))
        assertFalse(reach.needsChoice(1))
        assertEquals(AgentReach.OptionKind.NoComputerChosen, reach.options(listOf(laptop, desk)).first().optionKind)
        assertEquals(AgentReach.OptionKind.ThroughOnlyComputer, reach.options(listOf(laptop)).first().optionKind)
    }

    @Test
    fun aComputerThatIsNotAttachedNowIsKeptAndShownAsSuch() {
        val options = AgentReach("server", "notes", computerName = "old-box", isOnComputer = true).options(listOf(laptop))
        assertEquals(AgentReach.Option("old-box", AgentReach.OptionKind.ThroughComputerNotAttached), options.last())
        assertFalse(AgentReach("server", "notes", computerName = "laptop").options(listOf(laptop)).any { option -> option.optionKind == AgentReach.OptionKind.ThroughComputerNotAttached })
    }

    @Test
    fun aChatBotShowsOffBeforeAnythingElse() {
        assertEquals(AgentChatChannel.Condition.Off, AgentChatChannel("telegram", enabled = false, running = true).condition)
        assertEquals(AgentChatChannel.Condition.NotAnswering, AgentChatChannel("telegram", enabled = true, lastError = "bad token").condition)
        assertEquals(AgentChatChannel.Condition.Running, AgentChatChannel("telegram", enabled = true, running = true, lastError = "").condition)
        assertEquals(AgentChatChannel.Condition.Starting, AgentChatChannel("discord", enabled = true).condition)
    }

    @Test
    fun theLinkCommandShowsOnlyUntilAChatIsLinked() {
        assertEquals("/link 4821", AgentChatChannel("telegram", linkCode = "4821").linkCommand)
        assertNull(AgentChatChannel("telegram", linkCode = "4821", linked = true).linkCommand)
        assertNull(AgentChatChannel("telegram", linkCode = "").linkCommand)
    }

    @Test
    fun aServerShowsOffThenSharedThenItsOwnStatus() {
        assertEquals(ServerCondition.Off, AgentConnectedServer("a", auth = "user", enabled = false).condition)
        assertEquals(ServerCondition.Shared, AgentConnectedServer("a", auth = "none").condition)
        assertEquals(ServerCondition.Connected, AgentConnectedServer("a", auth = "user", status = "connected").condition)
        assertEquals(ServerCondition.NotAnswering, AgentConnectedServer("a", auth = "user", status = "error").condition)
        assertEquals(ServerCondition.AuthorizationUnderway, AgentConnectedServer("a", auth = "oauth", status = "pending").condition)
        assertEquals(ServerCondition.NotConnected, AgentConnectedServer("a", auth = "user", status = "").condition)
    }

    @Test
    fun onlyAServerTakingACredentialCanBeConnectedFromThePhone() {
        assertTrue(AgentConnectedServer("a", auth = "user").canConnectWithCredential)
        assertFalse(AgentConnectedServer("a", auth = "oauth").canConnectWithCredential)
        assertFalse(AgentConnectedServer("a", auth = "user", status = "connected").canConnectWithCredential)
        assertFalse(AgentConnectedServer("a", auth = "user", enabled = false).canConnectWithCredential)
    }

    @Test
    fun theAnswersReadAsTheServerWritesThem() {
        val secret = TeaNodeJson.decodeFromString<AgentSkillSecret>("""{"skill":"weather","key":"API_KEY","description":"","set":true}""")
        assertEquals("weather.API_KEY", secret.secretID)
        val page = TeaNodeJson.decodeFromString<AgentReplyPage>("""{"total":1,"replies":[{"id":"r","status":"held","subject":"Lunch","sendAfter":"2030-01-01T10:00:00Z"}]}""")
        assertTrue(page.replies.single().isHeld)
    }
}

class AgentActivityTest {
    @Test
    fun workShowsAsItsStatusAndAnythingElseAsStopped() {
        assertEquals(WorkCondition.Queued, AgentBackgroundWork("w", workStatus = "queued").condition)
        assertEquals(WorkCondition.Finished, AgentBackgroundWork("w", workStatus = "done").condition)
        assertEquals(WorkCondition.Stopped, AgentBackgroundWork("w", workStatus = "stopped").condition)
        assertTrue(AgentBackgroundWork("w", workStatus = "running").isStoppable)
        assertTrue(AgentBackgroundWork("w", workStatus = "failed").isOpenable)
    }

    @Test
    fun theNextPageOfRunsLeavesOutWhatIsAlreadyListed() {
        val first = listOf(AgentRunSummary("a"), AgentRunSummary("b"))
        val merged = mergeRunPages(first, listOf(AgentRunSummary("b"), AgentRunSummary("c")))
        assertEquals(listOf("a", "b", "c"), merged.map { run -> run.id })
    }

    @Test
    fun everyKindOfRunIsOfferedOnce() {
        assertEquals(AgentRunKinds.jobKinds.size, AgentRunKinds.jobKinds.toSet().size)
    }
}
