package com.teanode.kit.agent

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AgentSourceRowsTest {
    @Test
    fun aSourceHasTheShapeItWasMadeAs() {
        assertEquals(SourceShape.Sent, KnowledgeSourceRow("a", kind = "sent", name = "Sent").shape)
        assertEquals(SourceShape.Journal, KnowledgeSourceRow("a", name = "n", specification = KnowledgeSourceSpecification(format = "journal")).shape)
        assertEquals(SourceShape.Records, KnowledgeSourceRow("a", name = "n", specification = KnowledgeSourceSpecification(format = "records")).shape)
        assertEquals(SourceShape.Files, KnowledgeSourceRow("a", name = "n", specification = KnowledgeSourceSpecification(format = "something new")).shape)
    }

    @Test
    fun aSourceSaysWhereItReads() {
        val folder = KnowledgeSourceRow("a", name = "n", specification = KnowledgeSourceSpecification(path = "/notes", computer = "studio"))
        assertEquals("/notes · studio", folder.whereItReads(emptyMap()))
        val sent = KnowledgeSourceRow("b", kind = "sent", name = "n", specification = KnowledgeSourceSpecification(mailboxId = "box"))
        assertEquals("Personal", sent.whereItReads(mapOf("box" to "Personal")))
        assertEquals("", sent.whereItReads(emptyMap()))
    }

    @Test
    fun aTypeOfCommandsSaysWhatWasFilledInForItWithoutTheEmptyOnes() {
        val settings = JsonObject(
            mapOf("baseUrl" to JsonPrimitive("https://wiki.example.com"), "isCloud" to JsonPrimitive(false), "spaces" to JsonArray(emptyList()), "label" to JsonPrimitive("")),
        )
        val source = KnowledgeSourceRow("a", name = "n", specification = KnowledgeSourceSpecification(type = "wiki", settings = settings, computer = "studio"))
        assertEquals("Base URL: https://wiki.example.com · studio", source.whereItReads(emptyMap()))
    }

    @Test
    fun declinedFilesAreGatheredUnderTheReasonTheyShareInTheOrderTheyCame() {
        val files = listOf(
            DeclinedAttachment("1", declined = "Too large"), DeclinedAttachment("2", declined = "Not a document"),
            DeclinedAttachment("3", declined = "Too large"),
        )
        val groups = DeclinedAttachment.groupedByReason(files)
        assertEquals(listOf("Too large", "Not a document"), groups.map { group -> group.declinedReason })
        assertEquals(listOf("1", "3"), groups.first().files.map { file -> file.documentId })
        assertEquals("Project · chat", DeclinedAttachment("1", thread = "Project", channel = "chat").placeText)
        assertEquals("chat", DeclinedAttachment("1", thread = " ", channel = "chat").placeText)
    }

    @Test
    fun theReadingBarNeverSaysAHundredWhileSomethingWaits() {
        assertEquals(0, ReadingProgress().readPercent)
        assertEquals(99, ReadingProgress(waiting = 1, read = 9999).readPercent)
        assertEquals(100, ReadingProgress(waiting = 0, read = 10).readPercent)
        assertEquals(80, ReadingProgress(waiting = 20, read = 80).readPercent)
        assertEquals(100, ReadingProgress(waiting = 20, read = 80).total)
    }

    @Test
    fun theTimeLeftIsSaidInMinutesHoursOrDays() {
        assertEquals(ReadingSpan.Minutes(30), ReadingProgress(waiting = 5, perHour = 10.0, hoursLeft = 0.5).timeLeft)
        assertEquals(ReadingSpan.Minutes(1), ReadingProgress(waiting = 5, perHour = 10.0, hoursLeft = 0.001).timeLeft)
        assertEquals(ReadingSpan.Hours(3), ReadingProgress(waiting = 5, perHour = 10.0, hoursLeft = 3.0).timeLeft)
        assertEquals(ReadingSpan.Days(3), ReadingProgress(waiting = 5, perHour = 10.0, hoursLeft = 72.0).timeLeft)
        assertEquals(null, ReadingProgress(waiting = 0, perHour = 10.0, hoursLeft = 3.0).timeLeft)
        assertEquals(null, ReadingProgress(waiting = 5, perHour = 0.0, hoursLeft = 3.0).timeLeft)
    }

    @Test
    fun attachmentsAreAnythingWhenAnyCountIsAboveZero() {
        assertFalse(KnowledgeSourceAttachments("a").hasAny)
        assertTrue(KnowledgeSourceAttachments("a", declined = 1).hasAny)
    }
}
