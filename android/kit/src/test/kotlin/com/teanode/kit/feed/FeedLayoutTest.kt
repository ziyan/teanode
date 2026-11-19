package com.teanode.kit.feed

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.builtins.serializer
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class FeedLayoutTest {
    private val listContent = """{"listRows":[
        {"rowTitle":"A robot unloads a truck","rowDetail":"No ramp needed","rowMeta":"Example News","rowSymbol":"newspaper","rowUrl":"https://example.com/story"},
        {"rowTitle":"Receipt from the print shop","rowRecordReference":{"itemId":"abc123","subject":"Receipt"}},
        {"rowTitle":"Unsafe link","rowUrl":"javascript:alert(1)"},
        {"rowTitle":"Reads only"}]}"""

    @Test
    fun aListReadsItsRowsInOrder() {
        val layout = FeedLayout.read("list", listContent) as FeedLayout.Rows
        assertEquals(listOf("A robot unloads a truck", "Receipt from the print shop", "Unsafe link", "Reads only"), layout.rows.map { row -> row.rowTitle })
        assertEquals("Example News", layout.rows[0].rowMeta)
    }

    @Test
    fun aRowFollowsItsWebAddressOrItsRecordAndNeverAScript() {
        val rows = (FeedLayout.read("list", listContent) as FeedLayout.Rows).rows
        assertEquals("https://example.com/story", rows[0].linkAddress)
        assertEquals("mail:abc123", rows[1].linkAddress)
        assertNull(rows[2].linkAddress)
        assertNull(rows[3].linkAddress)
    }

    @Test
    fun aRowWhoseRecordReferenceIsNotAnObjectJustHasNoRecord() {
        val layout = FeedLayout.read("list", """{"listRows":[{"rowTitle":"One","rowRecordReference":"abc"}]}""") as FeedLayout.Rows
        assertNull(layout.rows.single().linkAddress)
    }

    @Test
    fun weatherReadsItsTemperaturesAndHours() {
        val layout = FeedLayout.read(
            "weather",
            """{"placeName":"Lisbon","currentTemperature":18.4,"temperatureUnit":"C","conditionText":"Mostly sunny","conditionSymbol":"cloud.sun","highTemperature":21,"lowTemperature":13.6,
                "hourlyForecast":[{"forecastHour":"2026-11-19T09:00:00+00:00","forecastTemperature":17,"forecastSymbol":"sun.max","precipitationChancePercent":5},
                {"forecastHour":"2026-11-19T10:00:00","forecastTemperature":19.2}]}""",
        ) as FeedLayout.Weather
        assertEquals("18°", layout.weather.currentText)
        assertEquals("21°", layout.weather.degrees(layout.weather.highTemperature))
        assertEquals("14°", layout.weather.degrees(layout.weather.lowTemperature))
        assertEquals(listOf("09:00", "10:00"), layout.weather.hourlyForecast.map { hour -> hour.hourLabel })
        assertNull(layout.weather.hourlyForecast[1].precipitationChancePercent)
    }

    @Test
    fun anAgendaReadsEntriesWithTheirKindsAndClockTimes() {
        val layout = FeedLayout.read(
            "agenda",
            """{"agendaEntries":[{"startsAt":"2026-11-19T09:30:00+00:00","endsAt":"2026-11-19T10:00:00+00:00","agendaTitle":"Planning call","agendaEntryKind":"meeting","agendaDetail":"Room 2"},
                {"startsAt":"2026-11-19T12:00:00","agendaTitle":"Lunch","agendaEntryKind":"break"},{"agendaTitle":"Odd one","agendaEntryKind":"something-new"}]}""",
        ) as FeedLayout.Agenda
        assertEquals(listOf(FeedAgendaKind.Meeting, FeedAgendaKind.Break, FeedAgendaKind.Other), layout.entries.map { entry -> entry.kind })
        assertEquals("09:30", layout.entries[0].startText)
        assertEquals("10:00", layout.entries[0].endText)
        assertEquals("12:00", layout.entries[1].startText)
        assertEquals("", layout.entries[1].endText)
        assertEquals("", layout.entries[2].startText)
    }

    @Test
    fun anUnknownLayoutIsPlainText() {
        assertEquals(FeedLayout.Text, FeedLayout.read("carousel", """{"slides":[]}"""))
    }

    @Test
    fun contentThatDoesNotParseIsPlainText() {
        assertEquals(FeedLayout.Text, FeedLayout.read("list", "{not json"))
        assertEquals(FeedLayout.Text, FeedLayout.read("list", "[1,2,3]"))
        assertEquals(FeedLayout.Text, FeedLayout.read("weather", """{"currentTemperature":"warm"}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("agenda", """{"agendaEntries":"none"}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("list", """{"listRows":[{"rowTitle":{"nested":1}}]}"""))
    }

    @Test
    fun contentWithNothingToShowIsPlainText() {
        assertEquals(FeedLayout.Text, FeedLayout.read("list", """{"listRows":[]}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("list", ""))
        assertEquals(FeedLayout.Text, FeedLayout.read("weather", "{}"))
        assertEquals(FeedLayout.Text, FeedLayout.read("agenda", """{"agendaEntries":[{"startsAt":"2026-11-19T09:00:00"}]}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("text", listContent))
    }

    @Test
    fun aCardReadsItsLayoutFromItsFields() {
        val card = TeaNodeJson.decodeFromString(
            AgentFeedItemView.serializer(),
            """{"feedItemId":"a","title":"T","feedCardLayout":"list","layoutContent":${TeaNodeJson.encodeToString(String.serializer(), listContent)}}""",
        )
        assertTrue(card.layout is FeedLayout.Rows)
        val plain = TeaNodeJson.decodeFromString(AgentFeedItemView.serializer(), """{"feedItemId":"a","title":"T"}""")
        assertEquals(FeedLayout.Text, plain.layout)
    }
}
