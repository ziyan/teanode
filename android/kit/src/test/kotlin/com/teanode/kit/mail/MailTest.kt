package com.teanode.kit.mail

import com.teanode.kit.FakeServer
import com.teanode.kit.connectionFor
import com.teanode.kit.sentVariables
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.LocalDate
import java.time.ZoneId

class MailTest {
    private fun verdicts(json: String): MailMessage.Verdicts = TeaNodeJson.decodeFromString(MailMessage.Verdicts.serializer(), json)

    @Test
    fun foldersAreInTheRailsOrder() {
        val folders = listOf(
            MailFolder("trash", "m", name = "Trash", kind = "trash"),
            MailFolder("receipts", "m", name = "Receipts"),
            MailFolder("inbox", "m", name = "Inbox", kind = "inbox"),
            MailFolder("travel", "m", name = "Travel"),
            MailFolder("trains", "m", parentId = "travel", name = "Trains"),
            MailFolder("sent", "m", name = "Sent", kind = "sent"),
            MailFolder("lost", "m", parentId = "gone", name = "Archived elsewhere"),
        )
        val rows = MailFolders.rows(folders)
        assertEquals(listOf("inbox", "sent", "trash", "lost", "receipts", "travel", "trains"), rows.map { row -> row.folder.id })
        assertEquals(listOf(0, 0, 0, 0, 0, 0, 1), rows.map { row -> row.depth })
    }

    @Test
    fun aNotesFolderIsOneHoldingOnlyNotes() {
        assertTrue(MailFolder("a", "m", name = "Ideas", total = 3, noteCount = 3).isNotes)
        assertTrue(MailFolder("b", "m", name = "Notes").isNotes)
        assertFalse(MailFolder("c", "m", name = "Ideas", total = 3, noteCount = 2).isNotes)
        assertFalse(MailFolder("d", "m", name = "Notes", kind = "inbox").isNotes)
    }

    @Test
    fun theShieldSaysWhatTheChecksFound() {
        assertEquals(MailVerdict.Good, MailVerdict.of(verdicts("""{"dmarc":{"result":"pass"},"dkims":[],"spamFilter":{"score":1}}""")))
        assertEquals(MailVerdict.Good, MailVerdict.of(verdicts("""{"spf":{"result":"pass"},"dkims":[{"result":"pass"}]}""")))
        assertEquals(MailVerdict.Bad, MailVerdict.of(verdicts("""{"dmarc":{"result":"fail"},"dkims":[]}""")))
        assertEquals(MailVerdict.Bad, MailVerdict.of(verdicts("""{"dkims":[{"result":"fail"}]}""")))
        assertEquals(MailVerdict.Good, MailVerdict.of(verdicts("""{"dkims":[{"result":"fail"},{"result":"pass"}],"dmarc":{"result":"pass"}}""")))
        assertEquals(MailVerdict.Warn, MailVerdict.of(verdicts("""{"dmarc":{"result":"pass"},"dkims":[],"spamFilter":{"score":7.5}}""")))
        assertEquals(MailVerdict.Unknown, MailVerdict.of(null))
        // Forwarding breaks the original signature and SPF; once DMARC is satisfied, here or at a trusted forwarder, neither is a failure.
        assertEquals(MailVerdict.Good, MailVerdict.of(verdicts("""{"spf":{"result":"pass"},"dkims":[{"result":"fail"}],"dmarc":{"result":"pass"}}""")))
        val forwarded = verdicts("""{"spf":{"result":"pass"},"dkims":[{"result":"fail"}],"dmarc":{"result":"fail","passedAtForwarderDomain":"forwarder.example.com"}}""")
        assertEquals(MailVerdict.Good, MailVerdict.of(forwarded))
        assertEquals("SPF pass · DKIM fail · DMARC pass at forwarder.example.com", MailVerdict.detail(forwarded))
        assertEquals(
            "SPF pass · DKIM pass · DMARC pass · spam 0.4",
            MailVerdict.detail(verdicts("""{"spf":{"result":"pass"},"dkims":[{"result":"pass"}],"dmarc":{"result":"pass"},"spamFilter":{"score":0.4}}""")),
        )
    }

    @Test
    fun aReplyGoesBackToTheSenderFromTheAddressWrittenTo() {
        val message = TeaNodeJson.decodeFromString(
            MailMessage.serializer(),
            """{"id":"m1","from":"sam@example.org","fromName":"Sam","sender":"","subject":"Lunch","recipients":["Robin <robin@example.com>","pat@example.net","robin+notes@example.com"],"receivedAt":"2026-10-10T09:00:00Z","size":100,"logoDomain":""}""",
        )
        val own = listOf("robin@example.com", "robin+notes@example.com")
        val reply = MailWords.reply(message, null, own, isReplyAll = false)
        assertEquals(listOf("sam@example.org"), reply.to)
        assertEquals(emptyList<String>(), reply.cc)
        assertEquals("robin@example.com", reply.from)
        val all = MailWords.reply(message, "list@example.org", own, isReplyAll = true)
        assertEquals(listOf("list@example.org"), all.to)
        assertEquals(listOf("pat@example.net"), all.cc)
        assertEquals("Re: Lunch", MailWords.replySubject("Lunch"))
        assertEquals("RE: Lunch", MailWords.replySubject("RE: Lunch"))
        assertEquals("Fwd: Lunch", MailWords.forwardSubject("Lunch"))
    }

    @Test
    fun typedAddressesAreSplitAndPlainTextBecomesParagraphs() {
        assertEquals(listOf("a@example.com", "b@example.com", "c@example.com"), MailWords.addresses("a@example.com, b@example.com;\nc@example.com ,"))
        assertEquals("sam@example.org", MailWords.bareAddress("Sam <sam@example.org>"))
        assertEquals("<p>Hi &lt;you&gt;<br>there</p><p>Bye</p>", MailWords.html("Hi <you>\nthere\n\nBye"))
        assertEquals("> one\n> two", MailWords.quoted("one\ntwo"))
    }

    @Test
    fun remoteImagesLoadThroughTheServerNeverFromTheSender() {
        val html = """<p>Hi</p><img data-blocked-src="https://pictures.example/a.png?x=1&amp;y=2" src="data:image/gif;base64,R0lGOD"><img src="cid:logo">"""
        assertEquals(
            """<p>Hi</p><img src="/api/v1/mail/m1/remote?url=https%3A%2F%2Fpictures.example%2Fa.png%3Fx%3D1%26y%3D2"><img src="cid:logo">""",
            MailWords.showingRemoteImages(html, "m1"),
        )
        assertEquals("&lt;<", MailWords.decodedAttribute("&amp;lt;&lt;"))
    }

    @Test
    fun aThreadPageIsRead() {
        val page = TeaNodeJson.decodeFromString(
            MailThreadPage.serializer(),
            """
            {"total":1,"threads":[{"threadId":"t1","count":2,"unread":1,"flagged":false,"participants":["Sam","Robin"],"itemIds":["i1","i2"],"hasDraft":false,
              "item":{"id":"i1","folderId":"inbox","mailId":"m1","seen":false,"flagged":false,"answered":false,"forwarded":false,"draft":false,"addedAt":"2026-10-10T09:00:00Z","subscriptionId":"",
                "insight":{"category":"personal","priority":"high","needsReply":true,"summary":"Lunch on Friday?","actionItems":[],"notes":"","model":"m","runId":"","proposals":[]},
                "mail":{"id":"m1","from":"sam@example.org","fromName":"Sam","sender":"","subject":"Lunch","recipients":[],"receivedAt":"2026-10-10T09:00:00Z","size":10,"messageId":"<a@b>","listKey":"","listName":"","logoDomain":"",
                  "authenticationResults":{"spf":null,"dkims":[],"dmarc":{"result":"pass"},"spamFilter":null}}}}]}
            """,
        )
        assertEquals("high", page.threads.first()?.item?.insight?.priority)
        assertEquals("i1", page.threads.first()?.id)
    }

    private fun subscription(oneClick: Boolean, unsubscribe: List<String>): MailSubscription {
        val addresses = unsubscribe.joinToString(",") { address -> "\"$address\"" }
        return TeaNodeJson.decodeFromString(
            MailSubscription.serializer(),
            """{"id":"s","key":"k","name":"News","from":"news@example.org","count":3,"unread":1,"lastAt":"2026-10-10T09:00:00Z","lastItemId":"i","oneClick":$oneClick,"unsubscribe":[$addresses],"logoDomain":"","method":"","failed":false,"error":"","stripped":false}""",
        )
    }

    @Test
    fun aListOffersTheWayOutItHas() {
        assertEquals("oneClick", subscription(true, listOf("https://example.org/leave")).leaveKind)
        assertEquals("mail", subscription(false, listOf("https://example.org/leave", "mailto:leave@example.org")).leaveKind)
        assertEquals("link", subscription(false, listOf("https://example.org/leave")).leaveKind)
        assertEquals("none", subscription(false, emptyList()).leaveKind)
    }

    @Test
    fun anOutgoingMessageSendsOnlyWhatItIs() {
        val message = MailOutgoing(from = "robin@example.com", to = listOf("sam@example.org"), subject = "Hi")
        assertNull(message.variables["forwardItemId"])
        assertNull(message.variables["draftItemId"])
        val drafted = message.copy(draftItemId = "d1", keepAttachments = listOf(0, 2)).variables
        assertEquals(JsonArray(listOf(JsonPrimitive(0), JsonPrimitive(2))), drafted["keepAttachments"])
    }

    @Test
    fun aFlagChangeMakesTheRowReadWithoutAsking() {
        val thread = MailThread(threadId = "t", unread = 3, item = MailItem(id = "i", seen = false))
        assertEquals(0, thread.with(isSeen = true).unread)
        assertTrue(thread.with(isSeen = true).item!!.seen)
        assertEquals(1, thread.copy(unread = 0).with(isSeen = false).unread)
        assertTrue(thread.with(isFlagged = true).flagged)
    }

    @Test
    fun theThreadsCallSendsOnlyWhatNarrowsTheList() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"ListMailboxThreads":{"total":0,"threads":[]}}}""" }
        val connection = connectionFor(server)
        val filter = MailFilter(searchText = "  lunch ", isUnreadOnly = true, since = LocalDate.of(2030, 1, 2), before = LocalDate.of(2030, 1, 3), hasAttachment = false)
        connection.mailThreads("inbox", null, filter, isPriority = false, first = 50, offset = 100, zone = ZoneId.of("UTC"))
        val variables = sentVariables(server.sent.first())
        assertEquals("inbox", variables["folderId"]?.let { (it as JsonPrimitive).content })
        assertEquals("lunch", (variables["search"] as JsonPrimitive).content)
        assertEquals("2030-01-02T00:00:00Z", (variables["since"] as JsonPrimitive).content)
        assertEquals("2030-01-04T00:00:00Z", (variables["before"] as JsonPrimitive).content)
        assertEquals("true", (variables["unread"] as JsonPrimitive).content)
        assertEquals("false", (variables["hasAttachment"] as JsonPrimitive).content)
        assertNull(variables["flagged"])
        assertNull(variables["priority"])
        assertNull(variables["mailboxId"])
        assertTrue(MailFilter().isNarrowing.not())
    }

    @Test
    fun aMessageThatWasSentBeforeIsAskedAboutBeforeItIsSentAgain() = runBlocking {
        val server = FakeServer { request ->
            if (request.body!!.toString(Charsets.UTF_8).contains("GetMailboxSubmission")) 200 to """{"data":{"GetMailboxSubmission":{"mailId":"m","sentItemId":"item-9"}}}"""
            else 200 to """{"data":{"SendMailboxMessage":{"mail":{"id":"m"},"item":{"id":"item-1"}}}}"""
        }
        val connection = connectionFor(server)
        val sending = MailSending("box", MailOutgoing(to = listOf("sam@example.org")), "submission-1")
        assertEquals("item-9", connection.send(sending, isRetry = true))
        assertEquals(1, server.sent.size)
        assertEquals("item-1", connection.send(sending, isRetry = false))
        assertEquals(2, server.sent.size)
    }
}
