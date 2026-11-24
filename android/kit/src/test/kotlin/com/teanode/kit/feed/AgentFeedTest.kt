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
        stackKey: String = "",
        stackCount: Int = 1,
        stackUnread: Int = 0,
        stackTitle: String = "",
    ): String =
        """{"feedItemId":"$id","feedItemKind":"decision","feedSourceKind":"$sourceKind","feedSourceId":"$sourceId","feedSourceTitle":"","runConversationId":"",
            "title":"Card $id","body":"Body","pictureSymbol":"cloud.sun","factRows":[{"factLabel":"High","factValue":"18 C"}],
            "sourceLinks":[{"sourceTitle":"Forecast","sourceUrl":"https://example.com/forecast"}],
            "choices":$choices,"chosenChoiceKey":${chosen?.let { key -> "\"$key\"" }},"chosenAt":null,"outcomeText":"$outcome","isOutcomePending":$isPending,
            "feedUrgency":"now","isLiked":false,"isRead":$isRead,"isDismissed":$isDismissed,"postedAt":"2026-11-18T08:00:00Z","updatedAt":"2026-11-18T08:30:00Z",
            "feedStackKey":"$stackKey","feedStackTitle":"$stackTitle","stackPosition":null,"stackItemCount":$stackCount,"stackUnreadCount":$stackUnread}"""

    private fun item(id: String, isRead: Boolean = false, isDismissed: Boolean = false): AgentFeedItemView =
        TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card(id, isRead, isDismissed))

    private val twoChoices = """[{"choiceKey":"me","choiceLabel":"That is me","choiceRole":"dismiss"},{"choiceKey":"secure","choiceLabel":"Secure my account","choiceRole":"action"}]"""

    @Test
    fun aCardIsReadWithItsFactsLinksAndReferences() {
        val view = item("a")
        assertEquals("High", view.factRows.single().factLabel)
        assertEquals("https://example.com/forecast", view.sourceLinks.single().sourceUrl)
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
        val withSlash = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", sourceKind = "coding_session", sourceId = "dev/box/%7"))
        assertEquals("dev/box" to "%7", withSlash.codingSessionPlace)
        assertNull(item("b").codingSessionPlace)
        val broken = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", sourceKind = "coding_session", sourceId = "laptop"))
        assertNull(broken.codingSessionPlace)
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
        assertNull(sentVariables(fake)["includeDismissed"])
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
    fun showingLessSendsTheCard() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ShowLessLikeAgentFeedItem":${card("a", isDismissed = true)}}}""" }
        assertTrue(connection.showLessLikeAgentFeedItem("a").isDismissed)
        assertEquals(JsonPrimitive("a"), sentVariables(fake)["feedItemId"])
    }

    @Test
    fun dismissingCardsSendsTheIdsAndAnswersTheOnesDismissed() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"DismissAgentFeedItems":["a"]}}""" }
        assertEquals(listOf("a"), connection.dismissAgentFeedItems(feedItemIds = listOf("a", "b")))
        assertEquals(JsonArray(listOf(JsonPrimitive("a"), JsonPrimitive("b"))), sentVariables(fake)["feedItemIds"])
        assertNull(sentVariables(fake)["feedStackKey"])
        assertNull(sentVariables(fake)["isEveryReadCard"])
    }

    @Test
    fun dismissingAStackSendsItsKey() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"DismissAgentFeedItems":["a","b","c"]}}""" }
        assertEquals(3, connection.dismissAgentFeedItems(feedStackKey = "goal:robots").size)
        assertEquals(JsonPrimitive("goal:robots"), sentVariables(fake)["feedStackKey"])
        assertNull(sentVariables(fake)["feedItemIds"])
    }

    @Test
    fun dismissingEveryReadCardSendsTheFlagAlone() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"DismissAgentFeedItems":[]}}""" }
        assertEquals(emptyList<String>(), connection.dismissAgentFeedItems(isEveryReadCard = true))
        assertEquals(JsonPrimitive(true), sentVariables(fake)["isEveryReadCard"])
        assertNull(sentVariables(fake)["feedItemIds"])
    }

    @Test
    fun dismissingNeedsExactlyOneWayToNameTheCards() = runBlocking {
        val (connection, _) = fakeConnection { """{"data":{"DismissAgentFeedItems":[]}}""" }
        assertTrue(runCatching { connection.dismissAgentFeedItems() }.exceptionOrNull() is IllegalArgumentException)
        assertTrue(runCatching { connection.dismissAgentFeedItems(feedItemIds = listOf("a"), isEveryReadCard = true) }.exceptionOrNull() is IllegalArgumentException)
    }

    @Test
    fun restoringSendsTheIdsAndAnswersTheOnesRestored() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"RestoreAgentFeedItems":["b"]}}""" }
        assertEquals(listOf("b"), connection.restoreAgentFeedItems(listOf("a", "b")))
        assertEquals(JsonArray(listOf(JsonPrimitive("a"), JsonPrimitive("b"))), sentVariables(fake)["feedItemIds"])
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
    fun aChangeEventIsReadAndAnythingElseIsLeftOut() {
        val change = AgentFeedEvents.read(TeaNodeJson.parseToJsonElement("""{"AgentFeedChanged":{"feedItemId":"a","feedChange":"settled","unreadCount":3}}"""))
        assertEquals(AgentFeedChanged("a", "settled", 3), change)
        assertNull(AgentFeedEvents.read(TeaNodeJson.parseToJsonElement("""{"somethingElse":{}}""")))
        assertNull(AgentFeedEvents.read(TeaNodeJson.parseToJsonElement("""{"AgentFeedChanged":null}""")))
    }

    @Test
    fun theDocumentsAskForTheNamesOfTheContract() {
        for (name in listOf("feedItemId", "feedItemKind", "feedSourceKind", "feedSourceTitle", "factRows", "sourceLinks", "choiceRole", "chosenChoiceKey", "outcomeText", "isOutcomePending", "feedUrgency", "isLiked", "isRead", "isDismissed", "postedAt", "updatedAt")) {
            assertTrue(AgentFeedDocuments.LIST_AGENT_FEED.contains(name), name)
        }
        assertTrue(AgentFeedDocuments.LIST_AGENT_FEED.contains("nextCursor\n    unreadCount"))
    }

    @Test
    fun aReferenceOpensThroughTheLinkOpenerWhenTheAppHasAPageForIt() {
        assertEquals("mail:abc123", AgentReference(itemId = "abc123").feedLinkAddress)
        assertNull(AgentReference(recordingId = "rec1").feedLinkAddress)
        assertEquals("memory:people/sam", AgentReference(path = "people/sam").feedLinkAddress)
        assertEquals("/finance/transactions", AgentReference(financeTransactionId = "t1").feedLinkAddress)
        assertNull(AgentReference(photoId = "p1").feedLinkAddress)
    }

    @Test
    fun aSymbolNameIsItsKindByTheLongestKnownPrefixAndAnUnknownOneIsANewspaper() {
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Weather), FeedPicture.of("cloud.sun"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Weather), FeedPicture.of("cloud.sun.fill"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Rain), FeedPicture.of("cloud.rain"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Cloud), FeedPicture.of("cloud"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Sun), FeedPicture.of("sun.max"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Night), FeedPicture.of("moon.stars"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Security), FeedPicture.of("lock.shield"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Chart), FeedPicture.of("chart.line.uptrend.xyaxis"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Tray), FeedPicture.of("tray.full"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Photo), FeedPicture.of("photo.on.rectangle"))
        assertEquals(FeedPicture.Symbol(FeedPictureKind.Video), FeedPicture.of("video"))
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

    private fun stackTop(id: String = "s1", count: Int = 4, title: String = "") =
        TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card(id, sourceKind = "goal", stackKey = "goal:robots", stackCount = count, stackUnread = 2, stackTitle = title))
            .copy(feedSourceTitle = "News about humanoid robots")

    @Test
    fun aStackTopSaysHowManyMoreItHoldsAndWhatItIsCalled() {
        val top = stackTop()
        assertTrue(top.isStack)
        assertEquals(3, top.moreInStackCount)
        assertEquals("News about humanoid robots", top.stackName)
        assertEquals("Morning brief", stackTop(title = "Morning brief").stackName)
    }

    @Test
    fun theFirstPageKeepsTheCardsTheCountAndTheCursor() {
        val page = AgentFeedPage(listOf(item("a"), item("b", isDismissed = true)), "cursor-1", 5)
        val list = AgentFeedList.first(page)
        assertEquals(listOf("a", "b"), list.feedItems.map { shown -> shown.feedItemId })
        assertTrue(list.hasMore)
        assertEquals(5, list.unreadCount)
    }

    @Test
    fun theNextPageGoesBelowAndACardAlreadyShownIsLeftAsItIs() {
        val first = AgentFeedList(listOf(item("a"), item("b")), "cursor-1", 3)
        val next = first.appending(AgentFeedPage(listOf(item("b", isRead = true), item("c")), null, 2))
        assertEquals(listOf("a", "b", "c"), next.feedItems.map { shown -> shown.feedItemId })
        assertFalse(next.feedItems[1].isRead)
        assertFalse(next.hasMore)
        assertEquals(2, next.unreadCount)
    }

    @Test
    fun anAnsweredCardIsReplacedWhereItIsAndStaysEvenWhenTheAnswerClosedIt() {
        val list = AgentFeedList(listOf(item("a"), item("b"), item("c")), null, 3)
        val answered = item("b", isDismissed = true)
        val next = list.replacing(answered)
        assertEquals(listOf("a", "b", "c"), next.feedItems.map { shown -> shown.feedItemId })
        assertTrue(next.feedItems[1].isDismissed)
        assertEquals(list, list.replacing(item("elsewhere")))
    }

    @Test
    fun aLikeOnAStackTopKeepsTheStackCounts() {
        val list = AgentFeedList(listOf(stackTop("s1", 4), item("a")), null, 3)
        val next = list.replacing(stackTop("s1", 1).copy(isLiked = true, stackUnreadCount = 0))
        assertEquals(4, next.feedItems[0].stackItemCount)
        assertEquals(2, next.feedItems[0].stackUnreadCount)
        assertTrue(next.feedItems[0].isLiked)
    }

    @Test
    fun aCardWaitingForAnOutcomeIsNoticed() {
        val pending = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = twoChoices, chosen = "secure", isPending = true))
        assertTrue(AgentFeedList(listOf(item("x"), pending)).hasPendingOutcome)
        assertFalse(AgentFeedList(listOf(item("x"))).hasPendingOutcome)
    }

    @Test
    fun aCardWhoseOnlyChoiceIsToDiscussAsksNothing() {
        val onlyDiscuss = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = """[{"choiceKey":"d","choiceLabel":"Talk","choiceRole":"discuss"}]"""))
        assertFalse(onlyDiscuss.isWaitingForAnswer)
        assertTrue(TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), card("a", choices = twoChoices)).isWaitingForAnswer)
    }

    @Test
    fun theChoicesShowActionsFirst() {
        val mixed = TeaNodeJson.decodeFromString(
            AgentFeedItemView.serializer(),
            card("a", choices = """[{"choiceKey":"later","choiceLabel":"Not now","choiceRole":"dismiss"},{"choiceKey":"approve","choiceLabel":"Approve","choiceRole":"action"},{"choiceKey":"talk","choiceLabel":"Talk","choiceRole":"discuss"}]"""),
        )
        assertEquals(listOf("approve", "later", "talk"), mixed.orderedChoices.map { choice -> choice.choiceKey })
    }

    @Test
    fun aCardAloneIsNoStackEvenWithAKey() {
        assertFalse(item("a").isStack)
        assertEquals(0, item("a").stackEdgeCount)
        assertFalse(stackTop(count = 1).isStack)
    }

    @Test
    fun aPairShowsOneEdgeAndMoreShowTwo() {
        assertEquals(1, stackTop(count = 2).stackEdgeCount)
        assertEquals(2, stackTop(count = 3).stackEdgeCount)
        assertEquals(2, stackTop(count = 30).stackEdgeCount)
    }

    @Test
    fun aStackIsReadByItsKeyAndPagedWithTheCursor() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ListAgentFeedStack":{"feedItems":[${card("s1", stackKey = "goal:robots", stackCount = 1)}],"nextCursor":"more","unreadCount":2}}}""" }
        val page = connection.agentFeedStack("goal:robots", after = "cursor-2")
        assertEquals("more", page.nextCursor)
        assertEquals(JsonPrimitive("goal:robots"), sentVariables(fake)["feedStackKey"])
        assertEquals(JsonPrimitive("cursor-2"), sentVariables(fake)["after"])
        assertEquals(JsonPrimitive(AGENT_FEED_PAGE_SIZE), sentVariables(fake)["first"])
        assertTrue(AgentFeedDocuments.LIST_AGENT_FEED_STACK.contains("ListAgentFeedStack(feedStackKey: \$feedStackKey"))
    }

    @Test
    fun theFeedDocumentsAskForTheStackFields() {
        for (name in listOf("feedStackKey", "feedStackTitle", "stackPosition", "stackItemCount", "stackUnreadCount")) {
            assertTrue(AgentFeedDocuments.LIST_AGENT_FEED.contains(name), name)
        }
        assertTrue(AgentFeedDocuments.AGENT_FEED_CHANGED.contains("AgentFeedChanged {"))
    }

    private fun media(key: String, kind: String = "photo", width: Int = 1600, height: Int = 900, rowIndex: Int? = null, site: String = "", link: String = "", image: String = "/api/v1/feed/items/a/media/$key") =
        AgentFeedMediaView(key, kind, image, if (kind == "video") "/photo/video/$key" else "", link, site, "", width, height, rowIndex)

    @Test
    fun aCardReadsItsMediaWithEveryField() {
        val view = TeaNodeJson.decodeFromString(
            AgentFeedItemView.serializer(),
            """{"feedItemId":"a","title":"T","feedMedia":[{"mediaKey":"m1","feedMediaKind":"video","imageUrl":"/p/1","videoUrl":"/v/1","linkUrl":"","siteName":"","mediaCaption":"A clip","widthPixels":1080,"heightPixels":1920,"listRowIndex":null},
                {"mediaKey":"m2","feedMediaKind":"picture","imageUrl":"/p/2","linkUrl":"https://example.com/story","siteName":"Example News","widthPixels":800,"heightPixels":450,"listRowIndex":2}]}""",
        )
        assertEquals(AgentFeedMediaKind.Video, view.feedMedia[0].kind)
        assertTrue(view.feedMedia[0].isPortrait)
        assertEquals("A clip", view.feedMedia[0].caption)
        assertEquals(listOf("m1"), view.cardMedia.map { each -> each.mediaKey })
        assertEquals(mapOf(2 to "m2"), view.rowThumbnails.mapValues { entry -> entry.value.mediaKey })
        assertEquals("", view.headerText)
    }

    @Test
    fun theHeaderLineIsTheFirstPicturesSiteElseTheSource() {
        val withSite = AgentFeedItemView(feedItemId = "a", title = "T", feedSourceTitle = "News goal", feedMedia = listOf(media("t", rowIndex = 0, site = "Row Site"), media("m", kind = "picture", site = "Example News")))
        assertEquals("Example News", withSite.headerText)
        val withoutSite = AgentFeedItemView(feedItemId = "a", title = "T", feedSourceTitle = "News goal", feedMedia = listOf(media("m", kind = "photo")))
        assertEquals("News goal", withoutSite.headerText)
        assertEquals("", AgentFeedItemView(feedItemId = "a", title = "T").headerText)
    }

    @Test
    fun aPictureWithNoImageIsLeftOutAndTheFirstThumbnailOfARowWins() {
        val view = AgentFeedItemView(
            feedItemId = "a", title = "T",
            feedMedia = listOf(media("none", image = ""), media("first", rowIndex = 1), media("second", rowIndex = 1), media("whole")),
        )
        assertEquals(listOf("whole"), view.cardMedia.map { each -> each.mediaKey })
        assertEquals("first", view.rowThumbnails.getValue(1).mediaKey)
    }

    @Test
    fun mediaFieldsMayBeMissingOrNull() {
        val view = TeaNodeJson.decodeFromString(
            AgentFeedItemView.serializer(),
            """{"feedItemId":"a","title":"T","feedMedia":[{"mediaKey":"m","feedMediaKind":"photo","imageUrl":"/p","videoUrl":null,"linkUrl":null,"siteName":null,"mediaCaption":null,"widthPixels":null,"heightPixels":null,"listRowIndex":null}]}""",
        )
        val only = view.feedMedia.single()
        assertFalse(only.isPortrait)
        assertEquals(16f / 9f, only.singleAspectRatio)
        assertEquals("", only.caption)
        assertNull(only.videoPath)
        assertNull(only.pageAddress)
    }

    @Test
    fun aPortraitPhotoIsCroppedToFourByFive() {
        assertEquals(4f / 5f, media("a", width = 900, height = 1600).singleAspectRatio)
    }

    @Test
    fun aVideoNamesItsPhotoInItsPath() {
        val video = AgentFeedMediaView(mediaKey = "v", feedMediaKind = "video", imageUrl = "/p", videoUrl = "/api/v1/photos/photo-9/previews/video?size=video")
        assertEquals("photo-9", video.videoPhotoId)
        assertNull(AgentFeedMediaView(mediaKey = "v", videoUrl = "/somewhere/else").videoPhotoId)
        assertNull(AgentFeedMediaView(mediaKey = "v").videoPhotoId)
    }

    @Test
    fun aKindFromANewerServerIsAPicture() {
        assertEquals(AgentFeedMediaKind.Picture, AgentFeedMediaKind.of("hologram"))
        assertEquals(AgentFeedMediaKind.Photo, AgentFeedMediaKind.of("photo"))
    }

    @Test
    fun theArrangementIsByHowManyPicturesThereAre() {
        assertEquals(AgentFeedMediaArrangement.None, AgentFeedMediaArrangement.of(emptyList()))
        assertEquals(AgentFeedMediaArrangement.Single, AgentFeedMediaArrangement.of(listOf(media("a"))))
        assertEquals(AgentFeedMediaArrangement.SinglePortrait, AgentFeedMediaArrangement.of(listOf(media("a", width = 900, height = 1600))))
        assertEquals(AgentFeedMediaArrangement.Grid, AgentFeedMediaArrangement.of(listOf(media("a"), media("b"))))
        assertEquals(AgentFeedMediaArrangement.Grid, AgentFeedMediaArrangement.of((1..4).map { number -> media("m$number") }))
        assertEquals(AgentFeedMediaArrangement.Row, AgentFeedMediaArrangement.of((1..5).map { number -> media("m$number") }))
        assertEquals(AgentFeedMediaArrangement.Row, AgentFeedMediaArrangement.of((1..6).map { number -> media("m$number") }))
    }

    @Test
    fun aPictureWithNoSizeIsNotPortrait() {
        assertFalse(media("a", width = 0, height = 0).isPortrait)
    }

    @Test
    fun onlyPathsOnThisServerAreReadAndOnlyWebPagesAreOpened() {
        assertEquals("/api/v1/feed/items/a/media/m", media("m").imagePath)
        assertNull(media("m", image = "https://other.example.com/p.jpg").imagePath)
        assertNull(media("m", image = "").imagePath)
        assertEquals("/photo/video/m", media("m", kind = "video").videoPath)
        assertEquals("https://example.com/s", media("m", kind = "picture", link = "https://example.com/s").pageAddress)
        assertNull(media("m", kind = "picture", link = "javascript:alert(1)").pageAddress)
    }

    @Test
    fun theDocumentsAskForTheMediaFields() {
        for (name in listOf("feedMedia", "mediaKey", "feedMediaKind", "imageUrl", "videoUrl", "linkUrl", "siteName", "mediaCaption", "widthPixels", "heightPixels", "listRowIndex")) {
            assertTrue(AgentFeedDocuments.LIST_AGENT_FEED.contains(name), name)
            assertTrue(AgentFeedDocuments.LIST_AGENT_FEED_STACK.contains(name), name)
            assertTrue(AgentFeedDocuments.CHOOSE_AGENT_FEED_ITEM.contains(name), name)
        }
    }
}
