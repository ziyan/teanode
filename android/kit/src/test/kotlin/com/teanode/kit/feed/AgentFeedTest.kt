package com.teanode.kit.feed

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.fakeConnection
import com.teanode.kit.calendar.sentVariables
import com.teanode.kit.chats.AgentReference
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentFeedTest {
    private fun card(
        id: String,
        isRead: Boolean = false,
        isDismissed: Boolean = false,
        choices: String = "[]",
        chosen: String? = null,
        isPending: Boolean = false,
        outcome: String = "",
        sourceKind: String = "agent",
        sourceId: String = "",
    ): String =
        """{"feedItemId":"$id","feedItemKind":"decision","feedSourceKind":"$sourceKind","feedSourceId":"$sourceId","feedSourceTitle":"","runConversationId":"",
            "title":"Card $id","body":"Body","pictureSymbol":"cloud.sun","factRows":[{"factLabel":"High","factValue":"18 C"}],
            "sourceLinks":[{"sourceTitle":"Forecast","sourceUrl":"https://example.com/forecast"}],"recordReferences":[{"itemId":"abc123","subject":"Receipt"}],
            "choices":$choices,"chosenChoiceKey":${chosen?.let { key -> "\"$key\"" }},"chosenAt":null,"outcomeText":"$outcome","isOutcomePending":$isPending,
            "feedUrgency":"now","isLiked":false,"isRead":$isRead,"isDismissed":$isDismissed,"postedAt":"2026-11-18T08:00:00Z","updatedAt":"2026-11-18T08:30:00Z"}"""

    private fun item(id: String, isRead: Boolean = false, isDismissed: Boolean = false): AgentFeedItemView =
        TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card(id, isRead, isDismissed))

    private val twoChoices = """[{"choiceKey":"me","choiceLabel":"That is me","choiceRole":"dismiss"},{"choiceKey":"secure","choiceLabel":"Secure my account","choiceRole":"action"}]"""

    @Test
    fun aCardIsReadWithItsFactsLinksAndReferences() {
        val view = item("a")
        assertEquals("High", view.factRows.single().factLabel)
        assertEquals("https://example.com/forecast", view.sourceLinks.single().sourceUrl)
        assertEquals("Receipt", view.recordReferences.single().subject)
        assertTrue(view.isUrgent)
    }

    @Test
    fun aCardWithUnknownFieldsAndMissingOnesStillReads() {
        val view = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), """{"feedItemId":"x","title":"Only a title","somethingNew":1}""")
        assertEquals("update", view.feedItemKind)
        assertTrue(view.choices.isEmpty())
        assertFalse(view.isAwaitingChoice)
    }

    @Test
    fun choicesKnowTheirRolesAndAnUnknownRoleAsksTheServerLikeAnAction() {
        assertEquals(AgentFeedChoiceRole.Dismiss, AgentFeedChoice("a", "A", "dismiss").role)
        assertEquals(AgentFeedChoiceRole.Discuss, AgentFeedChoice("a", "A", "discuss").role)
        assertEquals(AgentFeedChoiceRole.Open, AgentFeedChoice("a", "A", "open").role)
        assertEquals(AgentFeedChoiceRole.Action, AgentFeedChoice("a", "A", "something-newer").role)
    }

    @Test
    fun aDecisionAwaitsAChoiceUntilOneIsMade() {
        val open = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = twoChoices))
        assertTrue(open.isAwaitingChoice)
        val pending = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = twoChoices, chosen = "secure", isPending = true))
        assertFalse(pending.isAwaitingChoice)
        assertEquals("Secure my account", pending.chosenChoice?.choiceLabel)
        assertTrue(pending.canOpenChat)
        assertNull(pending.settledText)
    }

    @Test
    fun aSettledChoiceShowsTheAgentsLineAndOffersNoChat() {
        val settled = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = twoChoices, chosen = "secure", outcome = "Signed out the other session."))
        assertEquals("Signed out the other session.", settled.settledText)
        assertFalse(settled.canOpenChat)
    }

    @Test
    fun aDismissChoiceNeverOffersTheChatBecauseNothingWasSent() {
        val answered = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = twoChoices, chosen = "me", isPending = true))
        assertFalse(answered.canOpenChat)
    }

    @Test
    fun aCodingSessionCardNamesItsComputerAndPane() {
        val coding = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", sourceKind = "coding_session", sourceId = "laptop/%3"))
        assertEquals("laptop" to "%3", coding.codingSessionPlace)
        assertNull(item("b").codingSessionPlace)
        val broken = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", sourceKind = "coding_session", sourceId = "laptop"))
        assertNull(broken.codingSessionPlace)
    }

    @Test
    fun theFirstPageLeavesOutDismissedCardsAndKeepsTheCountAndCursor() {
        val page = AgentFeedPage(listOf(item("a"), item("b", isDismissed = true)), "cursor-1", 5)
        val list = AgentFeedList.first(page)
        assertEquals(listOf("a"), list.feedItems.map { shown -> shown.feedItemId })
        assertTrue(list.hasMore)
        assertEquals(5, list.unreadCount)
    }

    @Test
    fun theNextPageGoesBelowAndANewerCopyOfAShownCardReplacesIt() {
        val first = AgentFeedList(listOf(item("a"), item("b")), "cursor-1", 3)
        val newerB = item("b", isRead = true)
        val next = first.appending(AgentFeedPage(listOf(newerB, item("c")), null, 2))
        assertEquals(listOf("a", "b", "c"), next.feedItems.map { shown -> shown.feedItemId })
        assertTrue(next.feedItems[1].isRead)
        assertFalse(next.hasMore)
        assertEquals(2, next.unreadCount)
    }

    @Test
    fun aReplacedCardMovesToTheTopAndASettledOneStaysWhereItIs() {
        val list = AgentFeedList(listOf(item("a"), item("b"), item("c")), null, 3)
        assertEquals(listOf("c", "a", "b"), list.with(item("c"), isMovedToTop = true).feedItems.map { shown -> shown.feedItemId })
        assertEquals(listOf("a", "b", "c"), list.with(item("b")).feedItems.map { shown -> shown.feedItemId })
    }

    @Test
    fun aPostedCardIsNewAtTheTopAndCountsAsUnread() {
        val list = AgentFeedList(listOf(item("a", isRead = true)), null, 0)
        val posted = list.with(item("z"))
        assertEquals(listOf("z", "a"), posted.feedItems.map { shown -> shown.feedItemId })
        assertEquals(1, posted.unreadCount)
    }

    @Test
    fun aDismissedCardLeavesTheListAndAnUnreadOneTakesOneOffTheCount() {
        val list = AgentFeedList(listOf(item("a"), item("b", isRead = true)), null, 1)
        assertEquals(listOf("b"), list.with(item("a", isDismissed = true)).feedItems.map { shown -> shown.feedItemId })
        assertEquals(0, list.removing("a").unreadCount)
        assertEquals(1, list.removing("b").unreadCount)
        assertEquals(list, list.removing("nothing"))
    }

    @Test
    fun readingCardsMarksThemAndTakesTheNewlyReadOffTheCountNeverBelowZero() {
        val list = AgentFeedList(listOf(item("a"), item("b")), null, 2)
        val read = list.markedRead(setOf("a"), 1)
        assertTrue(read.feedItems[0].isRead)
        assertFalse(read.feedItems[1].isRead)
        assertEquals(1, read.unreadCount)
        assertEquals(0, list.markedRead(setOf("a", "b"), 5).unreadCount)
    }

    @Test
    fun theListPagesWithTheCursorAndAsksForNoDismissedCardsByDefault() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ListAgentFeed":{"feedItems":[${card("a")}],"nextCursor":"next","unreadCount":4}}}""" }
        val page = connection.agentFeed(after = "cursor-9")
        assertEquals("next", page.nextCursor)
        assertEquals(4, page.unreadCount)
        assertEquals(JsonPrimitive("cursor-9"), sentVariables(fake)["after"])
        assertEquals(JsonPrimitive(AGENT_FEED_PAGE_SIZE), sentVariables(fake)["first"])
        assertEquals(JsonPrimitive(false), sentVariables(fake)["includeDismissed"])
    }

    @Test
    fun theFirstPageSendsNoCursor() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ListAgentFeed":{"feedItems":[],"nextCursor":null,"unreadCount":0}}}""" }
        connection.agentFeed()
        assertNull(sentVariables(fake)["after"])
    }

    @Test
    fun choosingSendsTheCardAndTheChoiceAndReadsTheSettledCard() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ChooseAgentFeedItem":${card("a", choices = twoChoices, chosen = "secure", isPending = true)}}}""" }
        val answered = connection.chooseAgentFeedItem("a", "secure")
        assertEquals("secure", answered.chosenChoiceKey)
        assertTrue(answered.isOutcomePending)
        assertEquals(JsonPrimitive("a"), sentVariables(fake)["feedItemId"])
        assertEquals(JsonPrimitive("secure"), sentVariables(fake)["choiceKey"])
    }

    @Test
    fun likingSendsTheWayItIsToBe() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"LikeAgentFeedItem":${card("a")}}}""" }
        connection.likeAgentFeedItem("a", true)
        assertEquals(JsonPrimitive(true), sentVariables(fake)["isLiked"])
    }

    @Test
    fun showingLessAndDismissingEachSendTheCard() = runBlocking {
        val (lessConnection, lessFake) = fakeConnection { """{"data":{"ShowLessLikeAgentFeedItem":${card("a", isDismissed = true)}}}""" }
        assertTrue(lessConnection.showLessLikeAgentFeedItem("a").isDismissed)
        assertEquals(JsonPrimitive("a"), sentVariables(lessFake)["feedItemId"])
        val (dismissConnection, dismissFake) = fakeConnection { """{"data":{"DismissAgentFeedItem":${card("b", isDismissed = true)}}}""" }
        assertTrue(dismissConnection.dismissAgentFeedItem("b").isDismissed)
        assertEquals(JsonPrimitive("b"), sentVariables(dismissFake)["feedItemId"])
    }

    @Test
    fun markingReadSendsEveryIdAndAnswersHowManyWereNew() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"MarkAgentFeedRead":2}}""" }
        assertEquals(2, connection.markAgentFeedRead(listOf("a", "b")))
        assertEquals(JsonArray(listOf(JsonPrimitive("a"), JsonPrimitive("b"))), sentVariables(fake)["feedItemIds"])
    }

    @Test
    fun discussingAnswersTheMainConversationsId() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"DiscussAgentFeedItem":"conversation-main"}}""" }
        assertEquals("conversation-main", connection.discussAgentFeedItem("a"))
        assertEquals(JsonPrimitive("a"), sentVariables(fake)["feedItemId"])
    }

    @Test
    fun oneCardIsReadByItsId() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"GetAgentFeedItem":${card("a")}}}""" }
        assertEquals("a", connection.agentFeedItem("a").feedItemId)
        assertEquals(JsonPrimitive("a"), sentVariables(fake)["feedItemId"])
    }

    @Test
    fun aChangeEventIsReadAndAnythingElseIsLeftOut() {
        val change = AgentFeedEvents.read(TeaNodeJson.parseToJsonElement("""{"agentFeedChanged":{"feedItemId":"a","feedChange":"settled","unreadCount":3}}"""))
        assertEquals(AgentFeedChanged("a", "settled", 3), change)
        assertNull(AgentFeedEvents.read(TeaNodeJson.parseToJsonElement("""{"somethingElse":{}}""")))
        assertNull(AgentFeedEvents.read(TeaNodeJson.parseToJsonElement("""{"agentFeedChanged":null}""")))
    }

    @Test
    fun theDocumentsAskForTheNamesOfTheContract() {
        for (name in listOf("feedItemId", "feedItemKind", "feedSourceKind", "feedSourceTitle", "factRows", "sourceLinks", "recordReferences", "choiceRole", "chosenChoiceKey", "outcomeText", "isOutcomePending", "feedUrgency", "isLiked", "isRead", "isDismissed", "postedAt", "updatedAt")) {
            assertTrue(AgentFeedDocuments.LIST_AGENT_FEED.contains(name), name)
        }
        assertTrue(AgentFeedDocuments.LIST_AGENT_FEED.contains("nextCursor unreadCount"))
    }

    @Test
    fun aReferenceOpensThroughTheLinkOpenerWhenTheAppHasAPageForIt() {
        assertEquals("mail:abc123", AgentReference(itemId = "abc123").feedLinkAddress)
        assertEquals("/recordings/rec1", AgentReference(recordingId = "rec1").feedLinkAddress)
        assertEquals("memory:people/sam", AgentReference(path = "people/sam").feedLinkAddress)
        assertEquals("/finance/transactions", AgentReference(financeTransactionId = "t1").feedLinkAddress)
        assertNull(AgentReference(photoId = "p1").feedLinkAddress)
    }

    @Test
    fun aSymbolNameIsItsKindByTheLongestKnownPrefixAndAnUnknownOneIsANewspaper() {
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Weather), FeedPicture.of("cloud.sun"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Weather), FeedPicture.of("cloud.sun.fill"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Security), FeedPicture.of("lock.shield"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Chart), FeedPicture.of("chart.line.uptrend.xyaxis"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Tray), FeedPicture.of("tray.full"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.News), FeedPicture.of("newspaper"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.News), FeedPicture.of("some.unknown.symbol"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.News), FeedPicture.of(""))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Security), FeedPicture.of("exclamationmark.shield"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Alert), FeedPicture.of("exclamationmark.triangle"))
    }

    @Test
    fun anEmojiIsDrawnAsItIs() {
        assertEquals(FeedPicture.Emoji("✈️"), FeedPicture.of("✈️"))
        assertEquals(FeedPicture.Emoji("🌤️"), FeedPicture.of("🌤️"))
    }
}
