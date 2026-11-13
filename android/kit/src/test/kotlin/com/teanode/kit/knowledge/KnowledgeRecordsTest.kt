package com.teanode.kit.knowledge

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class KnowledgeRecordsTest {
    private fun link(
        id: String,
        kind: String = "mail",
        relation: String = "wrote",
        status: String = "stated",
        path: String = "/mailbox/starred/item-1",
        unavailable: Boolean = false,
        node: String = "people/fixture-guest",
        start: Int = 0,
        end: Int = 0,
        passageCount: String = "",
    ): KnowledgeRecordLink = TeaNodeJson.decodeFromString(
        KnowledgeRecordLink.serializer(),
        """
        {"id":"$id","nodePath":"$node","nodeName":"Fixture Guest","nodeKind":"person","recordKind":"$kind","recordId":"record-$id",
         "recordRelation":"$relation","linkStatus":"$status","passageStartMS":$start,"passageEndMS":$end,"transcriptRunId":"",$passageCount
         "note":"","happenedAt":null,"createdAt":"2026-01-01T00:00:00Z","recordTitle":"Fixture subject",
         "recordHappenedAt":"2026-03-04T10:00:00Z","dashboardPath":"$path","isUnavailable":$unavailable}
        """,
    )

    @Test
    fun linksAreGroupedByKindInTheOrderAPageListsThem() {
        val groups = KnowledgeRecordKinds.grouped(
            listOf(link("a", kind = "recording_passage"), link("b", kind = "mail"), link("c", kind = "contact"), link("d", kind = "mail"), link("e", kind = "something_new")),
        )
        assertEquals(listOf("contact", "mail", "recording_passage", "something_new"), groups.map { group -> group.recordKind })
        assertEquals(listOf("b", "d"), groups[1].links.map { found -> found.id })
    }

    @Test
    fun aRecordRowSaysHowManyPassagesItCoversAndAnOldServerSaysNothing() {
        assertEquals(37, link("a", kind = "recording_passage", passageCount = """"passageCount":37,""").coveredPassageCount)
        assertEquals(0, link("a", kind = "mail", passageCount = """"passageCount":0,""").coveredPassageCount)
        assertEquals(0, link("a", kind = "recording_passage").coveredPassageCount)
    }

    @Test
    fun aGuessIsProposedAndOnlyAnExistingRecordWithAPlaceOpens() {
        assertFalse(link("a").isProposed)
        assertTrue(link("a", status = "proposed").isProposed)
        assertTrue(link("a").canOpen)
        assertFalse(link("a", unavailable = true).canOpen)
        assertFalse(link("a", path = "").canOpen)
        assertFalse(link("a", path = "https://elsewhere.example.com/x").canOpen)
        assertNotNull(link("a").date)
    }

    @Test
    fun aRecordIsNamedAsTheCommandLineNamesIt() {
        assertEquals("mail:record-a", link("a").reference)
        assertEquals(
            "recording_passage:record-p@1500-4000",
            link("p", kind = "recording_passage", relation = "spoke_in", path = "/recordings/rec-1", start = 1500, end = 4000).reference,
        )
    }

    @Test
    fun eachPageThatLinksToARecordIsListedOnceAStatementWinningOverAGuess() {
        val pages = KnowledgeRecordKinds.pages(
            listOf(link("a", status = "proposed"), link("b", relation = "received"), link("c", node = "topics/fixture-topic")),
        )
        assertEquals(listOf("people/fixture-guest", "topics/fixture-topic"), pages.map { found -> found.nodePath })
        assertEquals(listOf("b", "c"), pages.map { found -> found.id })
    }

    @Test
    fun theDashboardsPathSaysWhereTheAppOpensARecord() {
        assertEquals(RecordTarget.Mail("item-1"), RecordTarget.fromDashboardPath("/mailbox/starred/item-1"))
        assertEquals(RecordTarget.Contact("card-1"), RecordTarget.fromDashboardPath("/contacts?contact=card-1"))
        assertEquals(RecordTarget.CalendarEvent("event-1"), RecordTarget.fromDashboardPath("/calendar?event=event-1"))
        assertEquals(RecordTarget.Reminder("reminder-1"), RecordTarget.fromDashboardPath("/reminders?reminder=reminder-1"))
        assertEquals(RecordTarget.Conversation("conversation-1"), RecordTarget.fromDashboardPath("/agent?conversation=conversation-1&message=message-1"))
        assertEquals(RecordTarget.FinanceTransaction("tx-1"), RecordTarget.fromDashboardPath("/finance/transactions?transaction=tx-1"))
        assertEquals(RecordTarget.FinanceReceipt("receipt-1"), RecordTarget.fromDashboardPath("/finance/receipts?receipt=receipt-1"))
        val passage = "/recordings/rec-1?version=run-1&startMS=1500&endMS=4000"
        assertEquals(RecordTarget.Recording(passage), RecordTarget.fromDashboardPath(passage))
    }

    @Test
    fun aPathThatNamesNoRecordOpensNothing() {
        for (path in listOf("", "contacts?contact=card-1", "/contacts", "/calendar?event=", "/finance/transactions", "/mailbox/starred", "/settings/agent", "/recordings")) {
            assertNull(RecordTarget.fromDashboardPath(path), path)
        }
    }

    @Test
    fun aPageReadFromAServerThatKnowsNoRecordLinksStillDecodes() {
        val page = TeaNodeJson.decodeFromString(KnowledgePage.serializer(), """{"node":null,"facts":[],"folded":[],"children":[],"attachments":[],"contact":null}""")
        assertNull(page.recordLinks)
        assertNull(page.recordLinkCount)
        val withLinks = TeaNodeJson.decodeFromString(
            KnowledgePage.serializer(),
            """
            {"node":null,"facts":[],"folded":[],"children":[],"attachments":[],"contact":null,"recordLinkCount":1,
             "recordLinks":[{"id":"a","nodePath":"p","nodeName":"P","nodeKind":"person","recordKind":"mail","recordId":"r","recordRelation":"wrote",
              "linkStatus":"stated","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"","happenedAt":null,"createdAt":"2026-01-01T00:00:00Z",
              "recordTitle":"T","recordHappenedAt":null,"dashboardPath":"/mailbox/starred/r","isUnavailable":false}],
             "evidenceRecords":[{"factId":"f","evidenceIndex":0,"recordKind":"mail","recordId":"r","passageStartMS":0,"passageEndMS":0,
              "transcriptRunId":"","recordTitle":"T","dashboardPath":"/mailbox/starred/r","isUnavailable":false}]}
            """,
        )
        assertEquals(1, withLinks.recordLinks?.filterNotNull()?.size)
        val fact = TeaNodeJson.decodeFromString(
            KnowledgeFact.serializer(), """{"id":"f","number":1,"kind":"fact","text":"T","happenedAt":null,"inferred":false,"evidence":[],"audiences":[]}""",
        )
        assertEquals(listOf("r"), withLinks.records(fact).map { record -> record.recordId })
        val other = TeaNodeJson.decodeFromString(
            KnowledgeFact.serializer(), """{"id":"g","number":2,"kind":"fact","text":"T","happenedAt":null,"inferred":false,"evidence":[],"audiences":[]}""",
        )
        assertTrue(withLinks.records(other).isEmpty())
        assertEquals(true, withLinks.evidenceRecords?.filterNotNull()?.first()?.canOpen)
    }

    @Test
    fun theDocumentsNameTheOperationsTheServerHas() {
        assertTrue(KnowledgeDocuments.READ_RECORD_LINKS.contains("ReadRecordMemoryLinks("))
        assertTrue(KnowledgeDocuments.LINK_RECORD.contains("LinkAgentRecord("))
        assertTrue(KnowledgeDocuments.UNLINK_RECORD.contains("UnlinkAgentRecord("))
        assertTrue(KnowledgeDocuments.CONFIRM_RECORD.contains("ConfirmAgentRecord("))
        assertTrue(KnowledgeDocuments.LIST_RECORD_LINKS.contains("recordLinks {"))
        assertTrue(KnowledgeDocuments.READ_RECORD_LINKS.contains("passageCount"))
        assertTrue(KnowledgeDocuments.SET_CONTACT.contains("SetAgentNodeContact("))
        assertTrue(KnowledgeDocuments.PAGE.contains("recordLinks {"))
        assertTrue(KnowledgeDocuments.PAGE.contains("evidenceRecords {"))
    }
}
