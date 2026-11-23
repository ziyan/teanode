package com.teanode.kit.ideas

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.fakeConnection
import com.teanode.kit.calendar.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class IdeasTest {
    private fun idea(id: String, kind: String, status: String, category: String? = null, closedAt: String? = null, expiredReason: String? = null): String =
        """{"id":"$id","ideaKind":"$kind","ideaCategory":${category?.let { value -> "\"$value\"" }},"emoji":null,"headline":"Idea $id","body":null,"openingRequest":"Do $id",
            "suggestionReason":null,"ideaStatus":"$status","expiredReason":${expiredReason?.let { value -> "\"$value\"" }},"startedConversationId":null,
            "createdAt":"2026-10-01T10:00:00Z","startedAt":null,"closedAt":${closedAt?.let { value -> "\"$value\"" }}}"""

    private val list: IdeaList = TeaNodeJson.decodeFromString(
        IdeaList.serializer(),
        """{"ideas":[${idea("a", "personal", "open")},${idea("b", "catalog", "open", "home")},${idea("c", "catalog", "open", "travel")},
            ${idea("d", "catalog", "dismissed", "home", "2026-10-05T10:00:00Z")},${idea("e", "personal", "done", closedAt = "2026-10-07T10:00:00Z")},
            ${idea("f", "catalog", "expired", "home", "2026-10-06T10:00:00Z", "stale")},${idea("g", "catalog", "expired", "home", "2026-10-04T10:00:00Z", "already_used")}],
          "ideaCategories":[{"ideaCategory":"travel","emojis":["x"]},{"ideaCategory":"home","emojis":["y"]},{"ideaCategory":"work","emojis":[]}]}""",
    )

    @Test
    fun ideasForYouAreTheOpenPersonalOnes() {
        assertEquals(listOf("a"), list.forYou.map { found -> found.id })
    }

    @Test
    fun theCatalogIsByCategoryInTheCatalogsOrderAndLeavesOutEmptyOnes() {
        assertEquals(listOf("travel", "home"), list.byCategory.map { group -> group.ideaCategory })
        assertEquals(listOf("b"), list.byCategory[1].ideas.map { found -> found.id })
    }

    @Test
    fun historyIsTheRestLatestFirst() {
        assertEquals(listOf("e", "f", "d", "g"), list.history.map { found -> found.id })
    }

    @Test
    fun anExpiredCatalogIdeaComesBackOnlyWhenItWasUsed() {
        val byId = list.ideas.associateBy { found -> found.id }
        assertFalse(byId.getValue("a").canRestore)
        assertTrue(byId.getValue("d").canRestore)
        assertFalse(byId.getValue("f").canRestore)
        assertTrue(byId.getValue("g").canRestore)
        assertTrue(byId.getValue("e").canRestore)
    }

    @Test
    fun startingAnIdeaAsksForItsConversationAndOpeningRequest() = runBlocking {
        val (connection, fake) = fakeConnection {
            """{"data":{"StartAgentIdea":{"conversation":{"id":"conversation-1"},"openingRequest":"Plan the trip."}}}"""
        }
        val started = connection.startIdea("idea-1", null, "en")
        assertEquals("conversation-1", started.conversationId)
        assertEquals("Plan the trip.", started.openingRequest)
        assertEquals(JsonPrimitive("idea-1"), sentVariables(fake)["ideaId"])
        assertEquals(JsonPrimitive("en"), sentVariables(fake)["language"])
        assertNull(sentVariables(fake)["conversationId"])
    }

    @Test
    fun anIdeasStatusIsSetByItsId() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SetAgentIdeaStatus":{"id":"idea-1","ideaStatus":"dismissed"}}}""" }
        connection.setIdeaStatus("idea-1", "dismissed")
        assertEquals(JsonPrimitive("dismissed"), sentVariables(fake)["ideaStatus"])
    }

    @Test
    fun everyIdeaIsListedInTheLanguageAsked() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ListAgentIdeas":{"ideas":[],"ideaCategories":[]}}}""" }
        assertEquals(emptyList<IdeaEntry>(), connection.allIdeas("ja").ideas)
        assertEquals(JsonPrimitive("ja"), sentVariables(fake)["language"])
    }
}
