package com.teanode.kit.feed

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.builtins.serializer
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.ZoneId
import java.util.Locale

class FeedLayoutTest {
    private val listContent = """{"listRows":[
        {"rowTitle":"A robot unloads a truck","rowDetail":"No ramp needed","rowMeta":"Example News","rowSymbol":"newspaper","rowUrl":"https://example.com/story"},
        {"rowTitle":"Receipt from the print shop","rowRecordReference":{"itemId":"abc123","subject":"Receipt"}},
        {"rowTitle":"Unsafe link","rowUrl":"javascript:alert(1)"},
        {"rowTitle":"A photo","rowRecordReference":{"photoId":"p1"}},
        {"rowTitle":"Reads only"}]}"""

    private val utc = ZoneId.of("UTC")

    @Test
    fun aListReadsItsRowsInOrder() {
        val layout = FeedLayout.read("list", listContent) as FeedLayout.Rows
        assertEquals(listOf("A robot unloads a truck", "Receipt from the print shop", "Unsafe link", "A photo", "Reads only"), layout.rows.map { row -> row.rowTitle })
        assertEquals("Example News", layout.rows[0].rowMeta)
    }

    @Test
    fun aRowOpensItsWebAddressOrItsRecordAndNeverAScript() {
        val rows = (FeedLayout.read("list", listContent) as FeedLayout.Rows).rows
        assertEquals("https://example.com/story", rows[0].linkAddress)
        assertEquals("mail:abc123", rows[1].linkAddress)
        assertNull(rows[2].linkAddress)
        assertFalse(rows[2].isOpenable)
        // A record the app has no page for is openable as the iPhone counts it, and opens nothing.
        assertTrue(rows[3].isOpenable)
        assertNull(rows[3].linkAddress)
        assertFalse(rows[4].isOpenable)
    }

    @Test
    fun aRowWhoseRecordReferenceIsNotAnObjectJustHasNoRecord() {
        val layout = FeedLayout.read("list", """{"listRows":[{"rowTitle":"One","rowRecordReference":"abc"}]}""") as FeedLayout.Rows
        assertFalse(layout.rows.single().isOpenable)
    }

    @Test
    fun weatherReadsTemperaturesWrittenAsNumbersOrTextAndShowsTheUnit() {
        val layout = FeedLayout.read(
            "weather",
            """{"placeName":"Lisbon","currentTemperature":18.4,"temperatureUnit":"\u00B0c","conditionText":"Mostly sunny","conditionSymbol":"cloud.sun","highTemperature":"21","lowTemperature":13,
                "hourlyForecast":[{"forecastHour":"2026-11-19T09:00:00+00:00","forecastTemperature":17,"forecastSymbol":"sun.max","precipitationChancePercent":5},
                {"forecastHour":"2026-11-19T10:00:00","forecastTemperature":"19.2"}]}""",
        ) as FeedLayout.Weather
        val weather = layout.weather
        assertEquals("18.4", weather.currentTemperature)
        assertEquals("18.4\u00B0C", weather.shown(weather.currentTemperature))
        assertEquals("21\u00B0C", weather.shown(weather.highTemperature))
        assertEquals("13\u00B0C", weather.shown(weather.lowTemperature))
        assertEquals("", weather.shown(""))
        assertEquals(5, weather.hourlyForecast[0].precipitationChancePercent)
        assertEquals(0, weather.hourlyForecast[1].precipitationChancePercent)
    }

    @Test
    fun aUnitThatIsNotCOrFIsLeftOff() {
        val weather = (FeedLayout.read("weather", """{"currentTemperature":"7","temperatureUnit":"kelvin"}""") as FeedLayout.Weather).weather
        assertEquals("7\u00B0", weather.shown("7"))
    }

    @Test
    fun anHourIsWrittenInThePhonesLanguageWithoutItsMinutes() {
        val hour = FeedHour("2026-11-19T15:00:00+00:00", "17", "", 0)
        assertEquals("3 PM", hour.hourText(utc, Locale.US).replace('\u202F', ' '))
        assertEquals("15", hour.hourText(utc, Locale.GERMANY))
        assertEquals("soon", FeedHour("soon", "", "", 0).hourText(utc, Locale.US))
    }

    @Test
    fun anAgendaReadsEntriesWithTheirKindsAndTimes() {
        val layout = FeedLayout.read(
            "agenda",
            """{"agendaEntries":[{"startsAt":"2026-11-19T09:30:00+00:00","endsAt":"2026-11-19T10:00:00+00:00","agendaTitle":"Planning call","agendaEntryKind":"meeting","agendaDetail":"Room 2"},
                {"startsAt":"2026-11-19T12:00:00","agendaTitle":"Lunch","agendaEntryKind":"break"},{"agendaTitle":"Odd one","agendaEntryKind":"something-new"}]}""",
        ) as FeedLayout.Agenda
        assertEquals(listOf(FeedAgendaKind.Meeting, FeedAgendaKind.Break, FeedAgendaKind.Task), layout.entries.map { entry -> entry.kind })
        assertEquals("9:30 AM", layout.entries[0].startText(utc, Locale.US)?.replace('\u202F', ' '))
        assertEquals("10:00 AM", layout.entries[0].endText(utc, Locale.US)?.replace('\u202F', ' '))
        assertNull(layout.entries[1].endText(utc, Locale.US))
        assertNull(layout.entries[2].startText(utc, Locale.US))
    }

    @Test
    fun anUnknownLayoutIsPlainText() {
        assertEquals(FeedLayout.Text, FeedLayout.read("carousel", """{"slides":[]}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("text", listContent))
        assertEquals(FeedLayout.Text, FeedLayout.read("", listContent))
    }

    @Test
    fun contentThatDoesNotParseIsPlainText() {
        assertEquals(FeedLayout.Text, FeedLayout.read("list", "{not json"))
        assertEquals(FeedLayout.Text, FeedLayout.read("list", "[1,2,3]"))
        assertEquals(FeedLayout.Text, FeedLayout.read("list", ""))
        assertEquals(FeedLayout.Text, FeedLayout.read("agenda", """{"agendaEntries":"none"}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("list", """{"listRows":["one"]}"""))
    }

    @Test
    fun contentWithNothingToShowIsPlainText() {
        assertEquals(FeedLayout.Text, FeedLayout.read("list", """{"listRows":[]}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("weather", "{}"))
        assertEquals(FeedLayout.Text, FeedLayout.read("weather", """{"conditionText":"Sunny"}"""))
        assertEquals(FeedLayout.Text, FeedLayout.read("agenda", """{"agendaEntries":[]}"""))
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

    @Test
    fun theSkyIsChosenAsTheIphoneChoosesIt() {
        assertEquals(FeedWeatherSky.ClearDay, FeedWeatherSky.of("sun.max", ""))
        assertEquals(FeedWeatherSky.ClearDay, FeedWeatherSky.of("cloud.sun", "Mostly sunny"))
        assertEquals(FeedWeatherSky.Cloudy, FeedWeatherSky.of("cloud.fill", ""))
        assertEquals(FeedWeatherSky.Rain, FeedWeatherSky.of("cloud.rain", ""))
        assertEquals(FeedWeatherSky.Storm, FeedWeatherSky.of("cloud.bolt.rain", ""))
        assertEquals(FeedWeatherSky.Storm, FeedWeatherSky.of("", "Thunderstorms"))
        assertEquals(FeedWeatherSky.Snow, FeedWeatherSky.of("cloud.snow", ""))
        assertEquals(FeedWeatherSky.ClearNight, FeedWeatherSky.of("moon.stars", ""))
        assertEquals(FeedWeatherSky.Rain, FeedWeatherSky.of("", "Light showers"))
        assertEquals(FeedWeatherSky.Cloudy, FeedWeatherSky.of("unknown.symbol", "Overcast"))
        assertEquals(FeedWeatherSky.Cloudy, FeedWeatherSky.of("", ""))
    }

    @Test
    fun whiteWordsReadOnEveryGradientInLightAndDark() {
        for (sky in FeedWeatherSky.entries) {
            for (isDark in listOf(false, true)) {
                val (top, bottom) = sky.colors(isDark)
                assertTrue(FeedWeatherSky.whiteContrast(top) >= 4.5, "$sky top dark=$isDark")
                assertTrue(FeedWeatherSky.whiteContrast(bottom) >= 4.5, "$sky bottom dark=$isDark")
            }
        }
    }

    @Test
    fun theDarkGradientIsDarkerThanTheLightOne() {
        val (lightTop, _) = FeedWeatherSky.ClearDay.colors(false)
        val (darkTop, _) = FeedWeatherSky.ClearDay.colors(true)
        assertTrue(FeedWeatherSky.whiteContrast(darkTop) > FeedWeatherSky.whiteContrast(lightTop))
    }

    @Test
    fun aCardReadsItsWeatherSky() {
        val weather = (FeedLayout.read("weather", """{"currentTemperature":"5","conditionSymbol":"cloud.snow"}""") as FeedLayout.Weather).weather
        assertEquals(FeedWeatherSky.Snow, weather.sky)
    }
}
