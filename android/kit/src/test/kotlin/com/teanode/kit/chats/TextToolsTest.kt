package com.teanode.kit.chats

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList

class LinkCleanTest {
    /** The cases the server's and the dashboard's tests run (internal/util/linkclean/testdata/links.json), so the three implementations answer alike. */
    @Test
    fun theSharedCasesCleanAndShortenAsTheServerDoes() {
        val file = generateSequence(File("").absoluteFile) { directory -> directory.parentFile }
            .map { directory -> File(directory, "internal/util/linkclean/testdata/links.json") }.first { candidate -> candidate.exists() }
        val cases = TeaNodeJson.parseToJsonElement(file.readText()) as JsonArray
        assertTrue(cases.size > 10, "the shared cases are missing or too few")
        for (found in cases) {
            val case = found as JsonObject
            val input = (case["input"] as JsonPrimitive).content
            assertEquals((case["cleaned"] as JsonPrimitive).content, LinkClean.cleaned(input), "cleaned $input")
            assertEquals((case["display"] as JsonPrimitive).content, LinkClean.displayText(input), "display of $input")
        }
    }

    @Test
    fun theDisplayTextIsNeverLongerThanTheLimit() {
        val text = LinkClean.displayText("https://example.com/aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffffgggggggggg")
        assertEquals(LinkClean.DISPLAY_LENGTH, text.codePointCount(0, text.length))
        assertTrue(text.contains("…"))
    }

    @Test
    fun aLinkWithNothingToRemoveIsGivenBackAsItWas() {
        val link = "https://example.com/p?a=1&&b=2#top"
        assertEquals(link, LinkClean.cleaned(link))
    }
}

class LinkTextTest {
    @Test
    fun theEndOfASentenceIsNotPartOfTheLink() {
        assertEquals("https://example.com/a", LinkText.trimmedEnd("https://example.com/a."))
        assertEquals("https://example.com/a", LinkText.trimmedEnd("https://example.com/a?!"))
        assertEquals("https://example.com/a", LinkText.trimmedEnd("https://example.com/a),"))
    }

    @Test
    fun aBracketTheLinkOpenedStays() {
        assertEquals("https://example.com/wiki/Thing_(film)", LinkText.trimmedEnd("https://example.com/wiki/Thing_(film)"))
        assertEquals("https://example.com/wiki/Thing_(film)", LinkText.trimmedEnd("https://example.com/wiki/Thing_(film))"))
    }

    @Test
    fun eachLinkIsFoundWhereItIs() {
        val text = "Try https://example.com/one, then (https://example.org/two?x=1)."
        val found = LinkText.links(text)
        assertEquals(listOf("https://example.com/one", "https://example.org/two?x=1"), found.map { span -> span.link })
        assertEquals("https://example.org/two?x=1", text.substring(found[1].start, found[1].endExclusive))
    }

    @Test
    fun aLinkStopsAtWordsOfALanguageWrittenWithoutSpaces() {
        assertEquals(listOf("https://example.com/a"), LinkText.links("看看https://example.com/a，很好").map { span -> span.link })
    }

    @Test
    fun aSchemeWithNoHostAndOtherSchemesAreNotLinks() {
        assertTrue(LinkText.links("https:// and ftp://example.com and mailto:a@example.com").isEmpty())
    }

    @Test
    fun theFirstLinkOfMarkdownLeavesOutCodeAndPictures() {
        val text = "![chart](https://example.com/chart.png)\n`https://example.com/in-code`\n```\ncurl https://example.com/in-a-block\n```\nRead [the guide](https://example.com/guide) first."
        assertEquals("https://example.com/guide", LinkText.firstLink(text, isMarkdown = true))
        assertNull(LinkText.firstLink("no links here", isMarkdown = false))
        assertEquals("https://example.com/first", LinkText.firstLink("a https://example.com/first b https://example.com/second", isMarkdown = false))
    }
}

class LinkOpeningTest {
    @Test
    fun webPagesOpenInTheAppsBrowser() {
        assertEquals(LinkOpening.Way.InApp, LinkOpening.way("https://www.example.com/shop/chair?color=blue"))
        assertEquals(LinkOpening.Way.InApp, LinkOpening.way("http://example.org/"))
        assertEquals(LinkOpening.Way.InApp, LinkOpening.way("HTTPS://Example.com/Path"))
    }

    @Test
    fun otherSchemesStayWithTheSystem() {
        for (link in listOf("mailto:someone@example.com", "tel:+15555550142", "sms:+15555550142", "teanode://open", "facetime:someone@example.com", "app-settings:", "ftp://example.com/file")) {
            assertEquals(LinkOpening.Way.System, LinkOpening.way(link), link)
        }
    }

    @Test
    fun mapsAndStorePagesAndTheAppsOwnLinksOpenTheSystemsApp() {
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https://apps.apple.com/app/id1"))
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https://maps.apple.com/?daddr=1%20Example%20Road&dirflg=d"))
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https://maps.apple/p/example"))
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https://teanode.com/app/connect#server=example.com&code=abc"))
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https://teanode.com/app/anything/else"))
        // The rest of the site is a web page like any other.
        assertEquals(LinkOpening.Way.InApp, LinkOpening.way("https://teanode.com/"))
        assertEquals(LinkOpening.Way.InApp, LinkOpening.way("https://teanode.com/application-notes"))
        assertEquals(LinkOpening.Way.InApp, LinkOpening.way("https://www.teanode.com/app/connect"))
    }

    @Test
    fun aConfigurationProfileIsTheBrowsersToInstallAndALinkWithNoHostIsTheSystems() {
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https://mail.example.com/api/v1/mailbox/profile/token.mobileconfig"))
        assertEquals(LinkOpening.Way.System, LinkOpening.way("https:///nothing"))
    }
}

class PlainTextTest {
    @Test
    fun hiddenMarkersAreRemovedAndCodeIsLeftAlone() {
        assertEquals("Saved.  Anything else?", HiddenMarkers.removed("Saved. <!-- kept: 3 --> Anything else?"))
        assertEquals("Working on it ", HiddenMarkers.removed("Working on it <!--sugg"))
        assertEquals("Done.\n\nBye.", HiddenMarkers.removed("Done.\n<!--\nnote\n-->\nBye."))
        assertEquals("A\nBC", HiddenMarkers.removed("A\n<!--a-->B<!--b-->C"))
        assertEquals("Use:\n```html\n<!-- a note -->\n```\nOkay.", HiddenMarkers.removed("Use:\n```html\n<!-- a note -->\n```\nOkay."))
        assertEquals("Write `<!-- a note -->` to comment.", HiddenMarkers.removed("Write `<!-- a note -->` to comment."))
        assertEquals("No marker here.", HiddenMarkers.removed("No marker here."))
    }

    @Test
    fun plainTextKeepsTheWordsOfAnAnswer() {
        assertEquals("Pay the invoice today?", PlainText.of("Pay [the invoice](mail:01abc) **today**?\n<!--suggestions:[\"Yes\",\"No\"]-->"))
        assertEquals("Here: the driveway INV-1", PlainText.of("Here: ![the driveway](https://example.org/a.jpg) `INV-1`"))
    }

    @Test
    fun aMarkerIsNotDrawnInTheChat() {
        assertEquals(listOf(MarkdownBlock.Paragraph("Booked."), MarkdownBlock.Paragraph("See you then.")), MarkdownBlock.parse("Booked.\n<!-- kept: 1 -->\nSee you then."))
    }
}

class TerminalTextTest {
    @Test
    fun eachRunTakesTheStyleTheCodesBeforeItSet() {
        val runs = terminalRuns("\u001B[38;2;220;40;40mred\u001B[0m plain \u001B[1;32mbold green\u001B[22m green\u001B[0m")
        assertEquals(listOf("red", " plain ", "bold green", " green"), runs.map { run -> run.text })
        assertEquals(TerminalColor(220, 40, 40), runs[0].foreground)
        assertNull(runs[1].foreground)
        assertTrue(runs[2].isBold)
        assertFalse(runs[3].isBold)
        assertEquals(runs[2].foreground, runs[3].foreground)
    }

    @Test
    fun theTwoHundredFiftySixColorsAndAnEmptyCode() {
        var style = TerminalRun("")
        style = applyTerminalCodes(listOf(38, 5, 196), style)
        assertEquals(TerminalColor(255, 0, 0), style.foreground)
        style = applyTerminalCodes(listOf(48, 5, 232), style)
        assertEquals(TerminalColor(8, 8, 8), style.background)
        val runs = terminalRuns("\u001B[31mred\u001B[mplain")
        assertEquals(listOf("red", "plain"), runs.map { run -> run.text })
        assertNull(runs[1].foreground)
    }

    @Test
    fun aFullColorWrittenWithColons() {
        val runs = terminalRuns("\u001B[38:2::10:20:30mone\u001B[38:2:40:50:60mtwo")
        assertEquals(TerminalColor(10, 20, 30), runs[0].foreground)
        assertEquals(TerminalColor(40, 50, 60), runs[1].foreground)
    }

    @Test
    fun textWithoutCodesIsOneRun() {
        assertEquals(listOf("$ npm test"), terminalRuns("$ npm test").map { run -> run.text })
    }
}

class ConversationDraftTest {
    @Test
    fun draftsBelongToOneAccountAndConversation() {
        val firstMainKey = ConversationDraft.storageKey("first-account", null)
        assertNotEquals(firstMainKey, ConversationDraft.storageKey("second-account", null))
        assertNotEquals(firstMainKey, ConversationDraft.storageKey("first-account", "main"))
        assertEquals(firstMainKey, ConversationDraft.storageKey("first-account", null))
        assertNotEquals(ConversationDraft.storageKey("first-account", "side-chat"), ConversationDraft.storageKey("second-account", "side-chat"))
    }

    @Test
    fun signingOutFindsOnlyThatAccountsDrafts() {
        val firstMain = ConversationDraft.storageKey("first-account", null)
        val firstSide = ConversationDraft.storageKey("first-account", "side-chat")
        // An account whose key starts with the other's is not caught by it.
        val longer = ConversationDraft.storageKey("first-account-two", null)
        assertTrue(ConversationDraft.isDraftOf("first-account", firstMain))
        assertTrue(ConversationDraft.isDraftOf("first-account", firstSide))
        assertFalse(ConversationDraft.isDraftOf("first-account", longer))
    }
}

class EmojiReactionTest {
    @Test
    fun emojiOfEveryMakeIsRead() {
        val emoji = listOf("👍", "❤️", "👍🏽", "🇯🇵", "👨‍👩‍👧‍👦", "🏳️‍🌈", "1️⃣", "✌️", "🙂")
        for (one in emoji) assertEquals(one, EmojiReaction.firstEmoji(one), one)
    }

    @Test
    fun textIsNotEmoji() {
        for (text in listOf("", "a", "hello", "7", "#", "*", "é", " ", "❤︎", "🇯", "…", "(:")) assertNull(EmojiReaction.firstEmoji(text), text)
    }

    @Test
    fun theFirstEmojiTypedIsTheReaction() {
        assertEquals("🔥", EmojiReaction.firstEmoji("ok 🔥 and 🎉"))
        assertEquals("🔥", EmojiReaction.firstEmoji("🔥🎉"))
        assertEquals("🇯🇵", EmojiReaction.firstEmoji("7🇯🇵"))
    }

    @Test
    fun anEmojiLongerThanTheServerTakesIsNotOne() {
        val crowd = List(8) { "👩" }.joinToString("‍")
        assertTrue(crowd.toByteArray().size > EmojiReaction.BYTES_MOST)
        assertNull(EmojiReaction.firstEmoji(crowd))
    }
}

class PresenceTest {
    @Test
    fun theReportNamesTheConversationOnScreen() {
        val variables = presenceVariables(true, "a-chat-conversation")
        assertEquals(JsonPrimitive("a-chat-conversation"), variables["conversationId"])
        assertEquals(JsonPrimitive(true), variables["isVisible"])
        assertEquals(JsonPrimitive(presenceReporterId), variables["reporterId"])
        assertEquals(JsonPrimitive(""), presenceVariables(true, "")["conversationId"])
    }

    @Test
    fun theDocumentAsksForTheConversation() {
        assertTrue(ChatDocuments.REPORT_PRESENCE.contains("\$conversationId: String"))
        assertTrue(ChatDocuments.REPORT_PRESENCE.contains("conversationId: \$conversationId"))
    }
}

class RunAnswerTest {
    private fun event(kind: String, sequence: Int, text: String = "") = """{"kind":"$kind","runId":"r1","sequence":$sequence,"text":"$text"}"""

    private fun connectionPaging(pages: List<String>): Pair<com.teanode.kit.Connection, com.teanode.kit.FakeServer> {
        var index = 0
        val fake = com.teanode.kit.FakeServer { _ ->
            val page = pages[minOf(index, pages.size - 1)]
            index += 1
            200 to page
        }
        val account = com.teanode.kit.Account(com.teanode.kit.ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", Long.MAX_VALUE / 4, "robin")
        return com.teanode.kit.Connection(account, fake) to fake
    }

    @Test
    fun theLastWholeMessageIsTheAnswer() = runBlocking {
        val (connection, fake) = connectionPaging(
            listOf(
                """{"data":{"ReadAgentRun":{"done":false,"events":[${event("asked", 0)},${event("message", 1, "Let me look.")},${event("tool_call", 2)}]}}}""",
                """{"data":{"ReadAgentRun":{"done":true,"events":[${event("tool_result", 3)},${event("message", 4, "It is sunny.")},${event("done", 5)}]}}}""",
            ),
        )
        val answer = connection.answerTo("r1", System.currentTimeMillis() + 60_000)
        assertEquals(RunAnswer.Answered("It is sunny."), answer)
        // The second read starts after the last event the first one saw.
        assertTrue(fake.sent[1].body!!.toString(Charsets.UTF_8).contains(""""after":3"""))
    }

    @Test
    fun anApprovalWaitsForThePerson() = runBlocking {
        val (connection, _) = connectionPaging(listOf("""{"data":{"ReadAgentRun":{"done":false,"events":[${event("asked", 0)},${event("confirmation", 1)}]}}}"""))
        assertEquals(RunAnswer.WaitingForPerson, connection.answerTo("r1", System.currentTimeMillis() + 60_000))
    }

    @Test
    fun aPassedDeadlineIsStillWorking() = runBlocking {
        val (connection, fake) = connectionPaging(listOf("""{"data":{"ReadAgentRun":{"done":false,"events":[]}}}"""))
        assertEquals(RunAnswer.StillWorking, connection.answerTo("r1", System.currentTimeMillis() - 1000))
        assertTrue(fake.sent.isEmpty())
    }
}

class LinkPreviewTest {
    private val availableJson =
        """{"url":"https://www.example.com/shop/chair?gclid=abc&color=blue","cleanedUrl":"https://www.example.com/shop/chair?color=blue","displayText":"example.com/shop/chair?color=blue","isAvailable":true,"title":"Garden Chair","description":"A chair for the garden.","siteName":"Example Shop","imageUrl":"/api/v1/link-preview/image/link-preview-1","imageWidthPixels":600,"imageHeightPixels":400}"""

    private val unavailableJson =
        """{"url":"https://example.net/gone","cleanedUrl":"https://example.net/gone","displayText":"example.net/gone","isAvailable":false,"title":"","description":"","siteName":"","imageUrl":null,"imageWidthPixels":null,"imageHeightPixels":null}"""

    private fun preview(json: String) = TeaNodeJson.decodeFromString(LinkPreview.serializer(), json)

    @Test
    fun anAvailablePreviewDecodesAndHasACard() {
        val preview = preview(availableJson)
        assertEquals("https://www.example.com/shop/chair?color=blue", preview.cleanedUrl)
        assertEquals("Garden Chair", preview.title)
        assertEquals("Example Shop", preview.siteName)
        assertEquals("/api/v1/link-preview/image/link-preview-1", preview.imageUrl)
        assertEquals(600, preview.imageWidthPixels)
        assertTrue(preview.hasCard)
    }

    @Test
    fun anUnavailablePreviewHasNoCard() {
        val preview = preview(unavailableJson)
        assertFalse(preview.hasCard)
        assertNull(preview.imageUrl)
        assertEquals("example.net/gone", preview.displayText)
    }

    @Test
    fun aPreviewWhoseAddressIsNotAWebAddressHasNoCard() {
        assertFalse(LinkPreview("x", "javascript:alert(1)", "x", true, title = "Odd").hasCard)
    }

    @Test
    fun aMissingFieldIsEmptyAndNotAFailure() {
        val preview = preview("""{"url":"https://example.com/a"}""")
        assertEquals("https://example.com/a", preview.cleanedUrl)
        assertFalse(preview.isAvailable)
        assertEquals("", preview.title)
    }

    @Test
    fun theLinksAreSentAsTheyWereWritten() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ReadLinkPreviews":[$availableJson,$unavailableJson]}}""")
        val urls = listOf("https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone")
        val previews = connection.linkPreviews("conversation-1", urls)
        assertEquals(listOf(true, false), previews.map { found -> found.isAvailable })
        val sent = TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.toString(Charsets.UTF_8)) as JsonObject
        assertTrue((sent["query"] as JsonPrimitive).content.contains("ReadLinkPreviews(conversationId: \$conversationId, urls: \$urls)"))
        val variables = sent["variables"] as JsonObject
        assertEquals(JsonPrimitive("conversation-1"), variables["conversationId"])
        assertEquals(JsonArray(urls.map { url -> JsonPrimitive(url) }), variables["urls"])
    }

    private class Recorder {
        val asked = CopyOnWriteArrayList<Pair<String, List<String>>>()
    }

    private fun found(urls: List<String>): List<LinkPreview> = urls.map { url -> LinkPreview(url, url, url, true, title = "Title of $url") }

    @Test
    fun theLinksOfAScreenfulAreAskedForInOneQuestionTenAtATime() = runBlocking {
        val recorder = Recorder()
        val loader = LinkPreviewLoader(collectingForMS = 50) { conversationId, urls ->
            recorder.asked.add(conversationId to urls)
            found(urls)
        }
        val addresses = (0 until 12).map { index -> "https://example.com/page-$index" }
        val previews = addresses.map { address -> async { loader.preview("conversation-1", address) } }.awaitAll()
        assertEquals(addresses, previews.map { found -> found?.url })
        assertEquals(12, recorder.asked.sumOf { question -> question.second.size })
        assertTrue(recorder.asked.all { question -> question.second.size <= LinkPreview.MOST_URLS && question.first == "conversation-1" })
        assertTrue(recorder.asked.size <= 3)
    }

    @Test
    fun eachConversationIsAskedInAQuestionOfItsOwnWithTheLinksAsWritten() = runBlocking {
        val recorder = Recorder()
        val loader = LinkPreviewLoader(collectingForMS = 50) { conversationId, urls ->
            recorder.asked.add(conversationId to urls)
            found(urls)
        }
        val tracked = "https://example.com/page?gclid=abc&color=blue"
        val first = async { loader.preview("conversation-1", tracked) }
        val other = async { loader.preview("conversation-2", "https://example.com/other") }
        val second = async { loader.preview("conversation-1", "https://example.com/second") }
        awaitAll(first, other, second)
        val byConversation = recorder.asked.associate { question -> question.first to question.second }
        assertEquals(2, recorder.asked.size)
        assertEquals(setOf(tracked, "https://example.com/second"), byConversation["conversation-1"]!!.toSet())
        assertEquals(listOf("https://example.com/other"), byConversation["conversation-2"])
    }

    @Test
    fun aLinkAskedForAgainIsNotAskedForAgainUnlessItIsInAnotherConversation() = runBlocking {
        val recorder = Recorder()
        val loader = LinkPreviewLoader(collectingForMS = 10) { conversationId, urls ->
            recorder.asked.add(conversationId to urls)
            found(urls)
        }
        val first = loader.preview("conversation-1", "https://example.com/once")
        val second = loader.preview("conversation-1", "https://example.com/once")
        assertEquals("Title of https://example.com/once", first?.title)
        assertEquals("Title of https://example.com/once", second?.title)
        assertEquals(1, recorder.asked.size)
        loader.preview("conversation-2", "https://example.com/once")
        assertEquals(2, recorder.asked.size)
    }

    @Test
    fun aQuestionThatFailsHasNoPreviewAndIsAskedAgainAfterAWhile() = runBlocking {
        val recorder = Recorder()
        val loader = LinkPreviewLoader(collectingForMS = 10, failedForMS = 100) { conversationId, urls ->
            recorder.asked.add(conversationId to urls)
            if (recorder.asked.size == 1) throw TeaNodeError.Refused("down")
            found(urls)
        }
        assertNull(loader.preview("conversation-1", "https://example.com/later"))
        assertNull(loader.preview("conversation-1", "https://example.com/later"))
        assertEquals(1, recorder.asked.size)
        delay(300)
        assertEquals(true, loader.preview("conversation-1", "https://example.com/later")?.isAvailable)
        assertEquals(2, recorder.asked.size)
    }

    @Test
    fun theLinksAheadAreTheNewestFirstOnceAndOnlyOfFinishedMessages() {
        val taken = TranscriptLine("m6", TranscriptLine.Kind.Person, "https://example.org/taken-back", isDeleted = true)
        val lines = listOf(
            TranscriptLine("m1", TranscriptLine.Kind.Person, "Look at https://example.com/first"),
            TranscriptLine("m2", TranscriptLine.Kind.Answer, "See `https://example.com/code` and [the page](https://example.com/answer)."),
            TranscriptLine("m3", TranscriptLine.Kind.Tool, "https://example.com/tool"),
            TranscriptLine("m4", TranscriptLine.Kind.Person, "Again https://example.com/first"),
            TranscriptLine("m5", TranscriptLine.Kind.Answer, "https://example.com/streaming", isStreaming = true),
            taken,
            TranscriptLine(Transcript.SENT_PREFIX + "7", TranscriptLine.Kind.Person, "https://example.com/sending"),
        )
        assertEquals(listOf("https://example.com/first", "https://example.com/answer"), LinkPreview.linksAhead(lines))
        assertNotNull(lines)
    }
}
