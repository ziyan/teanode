package com.teanode.kit.journal

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Test

class JournalNarrativeTest {
    private fun record(kind: String, identifier: String, title: String, path: String = "", isUnavailable: Boolean = false): JournalRecord =
        TeaNodeJson.decodeFromString(
            JournalRecord.serializer(),
            """{"recordKind":"$kind","recordId":"$identifier","reference":"$kind:$identifier","title":"$title","happenedAt":null,"dashboardPath":"$path","isUnavailable":$isUnavailable}""",
        )

    private fun words(text: String) = JournalNarrativePiece.Words(text)

    private fun mention(record: JournalRecord, words: String = "") = JournalNarrativePiece.Mention(record, words)

    @Test
    fun aMentionBecomesTheRecordsTitleBetweenTheWords() {
        val park = record("place_visit", "v1", "Riverside Park")
        assertEquals(
            listOf(listOf(words("You walked through "), mention(park), words(" before the rain."))),
            JournalNarrativeText.paragraphs("You walked through [[place_visit:v1]] before the rain.", listOf(park)),
        )
    }

    @Test
    fun aMentionCarriesTheWordsTheStoryWroteForIt() {
        val park = record("place_visit", "v1", "Visit to Riverside Park")
        assertEquals(
            listOf(listOf(words("Lunch at "), mention(park, "the park by the river"), words(", then home."))),
            JournalNarrativeText.paragraphs("Lunch at [[place_visit:v1| the park by the river ]], then home.", listOf(park)),
        )
    }

    @Test
    fun aMentionOfARecordThatCannotBeSeenLeavesOutItsWordsToo() {
        val gone = record("mail", "m1", "", isUnavailable = true)
        assertEquals(
            listOf(listOf(words("You read today."))),
            JournalNarrativeText.paragraphs("You read [[mail:m1|the lease letter]] today.", listOf(gone)),
        )
    }

    @Test
    fun paragraphsAreSeparatedByBlankLines() {
        assertEquals(
            listOf(listOf(words("First.")), listOf(words("Second.")), listOf(words("Third."))),
            JournalNarrativeText.paragraphs("First.\n\nSecond.\n\n\n\nThird.", emptyList()),
        )
    }

    @Test
    fun aMentionOfARecordThatCannotBeSeenIsLeftOut() {
        val gone = record("mail", "m1", "", isUnavailable = true)
        assertEquals(
            listOf(listOf(words("You read and today."))),
            JournalNarrativeText.paragraphs("You read [[mail:m1]] and [[mail:unknown]] today.", listOf(gone)),
        )
    }

    @Test
    fun twoMentionsKeepTheSpaceBetweenThem() {
        val first = record("mail", "a", "Lena")
        val second = record("mail", "b", "City Water")
        assertEquals(
            listOf(listOf(mention(first), words(" "), mention(second))),
            JournalNarrativeText.paragraphs("[[mail:a]] [[mail:b]]", listOf(first, second)),
        )
    }

    @Test
    fun anOccurrenceOfARepeatingEventKeepsItsColonsAndAt() {
        val standup = record("calendar_occurrence", "e1@2026-10-22T09:00:00Z", "Stand-up")
        assertEquals(
            listOf(listOf(words("Then "), mention(standup), words("."))),
            JournalNarrativeText.paragraphs("Then [[calendar_occurrence:e1@2026-10-22T09:00:00Z]].", listOf(standup)),
        )
    }

    @Test
    fun bracketsThatAreNotAMentionStayAsWords() {
        assertEquals(
            listOf(listOf(words("A list [[like this one]] and an open [[ bracket."))),
            JournalNarrativeText.paragraphs("A list [[like this one]] and an open [[ bracket.", emptyList()),
        )
    }

    @Test
    fun theAppOpensTheRecordsItHasAScreenFor() {
        assertEquals(JournalRecordOpening.MailItem("m1"), JournalRecordOpening.of("mail", "m1", "/mailbox/a/b/m1"))
        assertEquals(JournalRecordOpening.Contact("c1"), JournalRecordOpening.of("contact", "c1", ""))
        assertEquals(JournalRecordOpening.Photo("p1"), JournalRecordOpening.of("photo", "p1", "/photos"))
        assertEquals(JournalRecordOpening.Path("/recordings/r1"), JournalRecordOpening.of("recording", "r1", ""))
        assertEquals(JournalRecordOpening.Path("/recordings/r1"), JournalRecordOpening.of("recording_passage", "r1", ""))
    }

    @Test
    fun aRecordWithNoScreenOpensItsPartOrNothing() {
        assertEquals(JournalRecordOpening.Part("calendar"), JournalRecordOpening.of("calendar_occurrence", "e@t", "/calendar"))
        assertEquals(JournalRecordOpening.Part("finance"), JournalRecordOpening.of("finance_transaction", "t1", "/finance"))
        assertEquals(JournalRecordOpening.Part("ideas"), JournalRecordOpening.of("idea", "i1", ""))
        assertEquals(JournalRecordOpening.Part("goals"), JournalRecordOpening.of("goal", "g1", ""))
        assertEquals(JournalRecordOpening.Part("shopping"), JournalRecordOpening.of("shopping_trip", "s1", "/shopping"))
        // The part is by the app's own name for it, which the app opens.
        assertEquals(JournalRecordOpening.Part("shopping"), JournalRecordOpening.of("shopping_trip", "s1", ""))
        assertEquals(JournalRecordOpening.Nowhere, JournalRecordOpening.of("chat_message", "x", ""))
        assertFalse(JournalRecordOpening.Nowhere.isOpenable)
    }

    @Test
    fun aPlaceVisitAndAHealthDayOpenByTheDashboardsPath() {
        assertEquals(JournalRecordOpening.Path("/location?day=2026-10-22"), JournalRecordOpening.of("place_visit", "v1", "/location?day=2026-10-22"))
        assertEquals(
            JournalRecordOpening.Path("/health?type=stepCount"), JournalRecordOpening.of("health_day", "stepCount@2026-10-22", "/health?type=stepCount"),
        )
        assertEquals(JournalRecordOpening.Path("/knowledge/notes/n1"), JournalRecordOpening.of("note", "n1", "/knowledge/notes/n1"))
        assertEquals(JournalRecordOpening.Nowhere, JournalRecordOpening.of("agent_document", "d1", "/sessions/d1"))
    }
}
