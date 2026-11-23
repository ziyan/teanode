package com.teanode.kit.calendar

import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** Renaming the person's own calendar: what the server's answer decodes to, and what the rename sends. */
class CalendarRenameTest {
    @Test
    fun aCalendarKeepsWhatItIsFor() {
        val found = TeaNodeJson.decodeFromString(
            AgentCalendar.serializer(), """{"id":"c","name":"Calendar","description":"Things I do","calendarKind":"events","shareLevel":"owner"}""",
        )
        assertEquals("Things I do", found.description)
        // A server that leaves it out, as it does for a calendar with none, is nobody's description.
        assertNull(TeaNodeJson.decodeFromString(AgentCalendar.serializer(), """{"id":"c","name":"Calendar"}""").description)
        assertTrue(CalendarDocuments.CALENDARS.contains("description"))
    }

    @Test
    fun renamingSendsEveryOtherFieldBackAsTheCalendarHoldsIt() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SaveCalendar":{"id":"own","name":"Work"}}}""" }
        val own = AgentCalendar(
            id = "own", name = "Calendar", description = "Things I do", colour = "#336699", timezone = "Europe/Berlin", weekStart = "monday",
            calendarKind = "events", shareLevel = "owner",
        )
        connection.saveCalendar(own, "Work")

        assertEquals(1, fake.sent.size)
        // The server replaces every field it is sent, so these go back as they were.
        assertEquals(
            mapOf(
                "id" to JsonPrimitive("own"), "name" to JsonPrimitive("Work"), "description" to JsonPrimitive("Things I do"),
                "colour" to JsonPrimitive("#336699"), "timezone" to JsonPrimitive("Europe/Berlin"), "weekStart" to JsonPrimitive("monday"),
            ),
            sentVariables(fake),
        )
        assertTrue((sentBody(fake)["query"] as JsonPrimitive).content.contains("SaveCalendar(id: \$id"))
    }

    @Test
    fun aCalendarWithNoDescriptionOrColorSendsThemEmpty() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SaveCalendar":{"id":"own","name":"Work"}}}""" }
        connection.saveCalendar(AgentCalendar(id = "own", name = "Calendar"), "Work")
        assertEquals(JsonPrimitive(""), sentVariables(fake)["description"])
        assertEquals(JsonPrimitive(""), sentVariables(fake)["colour"])
    }

    @Test
    fun renamingACircleCalendarSendsTheSameDocument() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SaveCalendar":{"id":"family","name":"Household"}}}""" }
        val family = AgentCalendar(
            id = "family", name = "Home plans", description = "Dinners", colour = "#445566", timezone = "Europe/Berlin", weekStart = "monday",
            calendarKind = "events", shareLevel = "edit", circleId = "circle", circleName = "Home", isCircleCalendar = true,
        )
        connection.saveCalendar(family, "Household")
        assertEquals(JsonPrimitive("family"), sentVariables(fake)["id"])
        assertEquals(JsonPrimitive("Household"), sentVariables(fake)["name"])
        assertEquals(JsonPrimitive("Dinners"), sentVariables(fake)["description"])
    }

    @Test
    fun deletingACircleCalendarNamesTheCircle() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"DeleteCircleCalendar":true}}""" }
        connection.deleteCircleCalendar("circle")
        assertEquals(1, fake.sent.size)
        assertEquals(mapOf("circleId" to JsonPrimitive("circle")), sentVariables(fake))
        assertTrue((sentBody(fake)["query"] as JsonPrimitive).content.contains("DeleteCircleCalendar(circleId: \$circleId)"))
    }

    @Test
    fun aRetriedChangeIsAskedAboutFirstAndNotSentAgainWhenItHappened() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"GetCalendarRequest":{"requestId":"abc","isMissing":false}}}""" }
        val change = CalendarChange("save", "Lunch", variables = mapOf("calendarId" to JsonPrimitive("own")))
        connection.perform(change, isRetry = true)
        assertEquals(1, fake.sent.size)
        assertTrue(fake.sent.single().body!!.toString(Charsets.UTF_8).contains("GetCalendarRequest"))
        assertEquals(32, change.requestId.length)
    }

    @Test
    fun aChangeThatDidNotHappenIsSentWithItsRequestId() = runBlocking {
        val (connection, fake) = fakeConnection { body ->
            if (body.contains("GetCalendarRequest")) """{"data":{"GetCalendarRequest":null}}""" else """{"data":{"SaveCalendarEvent":{"id":"e"}}}"""
        }
        val change = CalendarChange("save", "Lunch", variables = mapOf("calendarId" to JsonPrimitive("own"), "id" to JsonNull))
        connection.perform(change, isRetry = true)
        assertEquals(2, fake.sent.size)
        val variables = (TeaNodeJson.parseToJsonElement(fake.sent[1].body!!.toString(Charsets.UTF_8)) as kotlinx.serialization.json.JsonObject)["variables"]
        assertNotNull(variables)
        assertTrue(variables.toString().contains(change.requestId))
    }
}
