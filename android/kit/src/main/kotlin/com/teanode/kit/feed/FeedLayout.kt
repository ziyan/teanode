package com.teanode.kit.feed

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentReference
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.doubleOrNull
import java.time.Instant
import java.time.LocalDateTime
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.chrono.IsoChronology
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeFormatterBuilder
import java.time.format.FormatStyle
import java.util.Locale
import kotlin.math.roundToLong

// The structured layouts of a feed card, read as the iPhone reads them (ios/TeaNodeKit AgentFeedLayouts.swift): every
// field may be missing or of another type, and a card whose content has nothing to show is drawn as its text.

private fun JsonObject.text(key: String): String = ((this[key] as? JsonPrimitive)?.takeIf { primitive -> primitive.isString })?.content ?: ""

/** A value the server may write as text or as a number (a temperature, a percent), as the words to show. */
private fun JsonObject.flexibleText(key: String): String {
    val primitive = this[key] as? JsonPrimitive ?: return ""
    if (primitive is JsonNull) return ""
    if (primitive.isString) return primitive.content.trim()
    val number = primitive.doubleOrNull ?: return ""
    return if (number == Math.rint(number)) number.toLong().toString() else String.format(Locale.ROOT, "%.1f", number)
}

private fun JsonObject.reference(key: String): AgentReference? =
    (this[key] as? JsonObject)?.let { element -> runCatching { TeaNodeJson.decodeFromJsonElement(AgentReference.serializer(), element) }.getOrNull() }

/** The objects of an array, null elements left out; an element that is not an object (or a value that is not an array) fails the whole content. */
private fun JsonElement?.objects(): List<JsonObject>? {
    val array = this as? JsonArray ?: return null
    val objects = ArrayList<JsonObject>()
    for (element in array) {
        if (element is JsonNull) continue
        objects.add(element as? JsonObject ?: return null)
    }
    return objects
}

private fun timeOf(text: String, zone: ZoneId): java.time.ZonedDateTime? = runCatching { OffsetDateTime.parse(text).atZoneSameInstant(zone) }
    .recoverCatching { Instant.parse(text).atZone(zone) }
    .recoverCatching { LocalDateTime.parse(text).atZone(zone) }
    .getOrNull()

/** A time as the phone's language writes it, `9:30 AM` or `09:30`. */
private fun clockText(text: String, zone: ZoneId, locale: Locale): String? = timeOf(text, zone)?.format(DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT).withLocale(locale))

/** One row of a list card: news, a mention, a review, a mail. */
data class FeedListRow(
    val rowTitle: String,
    val rowDetail: String,
    /** The source of a story, or the channel and time of a mention. */
    val rowMeta: String,
    val rowSymbol: String,
    val rowUrl: String,
    /** A record of the person's own the row opens, as a message's reference. */
    val recordReference: AgentReference?,
) {
    /** The web address the row opens; only http and https are followed. */
    val webAddress: String?
        get() = rowUrl.takeIf { address -> address.lowercase().let { lowered -> lowered.startsWith("http://") || lowered.startsWith("https://") } }

    /** Whether pressing the row opens something. */
    val isOpenable: Boolean get() = webAddress != null || recordReference != null

    /** What the link opener follows: the web address, else the record when the app has a page for it; null opens nothing. */
    val linkAddress: String? get() = webAddress ?: recordReference?.feedLinkAddress
}

/** One hour of a weather card's forecast. */
data class FeedHour(val forecastHour: String, val forecastTemperature: String, val forecastSymbol: String, val precipitationChancePercent: Int) {
    /** The hour as the phone's language writes it, `9 AM`; the text itself when it is not a time. */
    fun hourText(zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String {
        val time = timeOf(forecastHour, zone) ?: return forecastHour
        val pattern = DateTimeFormatterBuilder.getLocalizedDateTimePattern(null, FormatStyle.SHORT, IsoChronology.INSTANCE, locale)
        // The short time without its minutes: "h:mm a" is "h a".
        return time.format(DateTimeFormatter.ofPattern(pattern.replace(Regex("[:.]mm"), ""), locale))
    }
}

/** The content of a weather card. */
data class FeedWeather(
    val placeName: String,
    val currentTemperature: String,
    /** C or F, with or without the degree sign. */
    val temperatureUnit: String,
    val conditionText: String,
    val conditionSymbol: String,
    val highTemperature: String,
    val lowTemperature: String,
    val hourlyForecast: List<FeedHour>,
) {
    /** A temperature with its degree sign and the unit's letter: `21°C`. */
    fun shown(temperature: String): String {
        if (temperature.isEmpty()) return ""
        val letter = temperatureUnit.replace("°", "").trim().uppercase()
        return temperature + "°" + if (letter == "C" || letter == "F") letter else ""
    }
}

enum class FeedAgendaKind {
    Meeting, Focus, Task, Break;

    companion object {
        /** An entry of a kind the app does not know is drawn as a task. */
        fun of(name: String): FeedAgendaKind = when (name) {
            "meeting" -> Meeting
            "focus" -> Focus
            "break" -> Break
            else -> Task
        }
    }
}

/** One entry of a day plan. */
data class FeedAgendaEntry(
    val startsAt: String,
    val endsAt: String,
    val agendaTitle: String,
    val kind: FeedAgendaKind,
    val agendaDetail: String,
    val recordReference: AgentReference?,
) {
    /** The start as the phone's language writes it; null for an entry with no time. */
    fun startText(zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? = clockText(startsAt, zone, locale)

    fun endText(zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? = clockText(endsAt, zone, locale)

    /** What a tap follows; null opens nothing. */
    val linkAddress: String? get() = recordReference?.feedLinkAddress
}

/**
 * FeedLayout is how a card is drawn. Anything the app cannot draw (an unknown layout, content that does not read, or
 * content with nothing in it) is [Text], the card's plain body, which every card carries for exactly this.
 */
sealed class FeedLayout {
    data object Text : FeedLayout()

    data class Rows(val rows: List<FeedListRow>) : FeedLayout()

    data class Weather(val weather: FeedWeather) : FeedLayout()

    data class Agenda(val entries: List<FeedAgendaEntry>) : FeedLayout()

    companion object {
        fun read(layoutName: String, layoutContent: String): FeedLayout {
            if (layoutName != "list" && layoutName != "weather" && layoutName != "agenda") return Text
            val content = runCatching { TeaNodeJson.parseToJsonElement(layoutContent) }.getOrNull() as? JsonObject ?: return Text
            return when (layoutName) {
                "list" -> {
                    val rows = content["listRows"].objects()?.map { row ->
                        FeedListRow(row.text("rowTitle"), row.text("rowDetail"), row.text("rowMeta"), row.text("rowSymbol"), row.text("rowUrl"), row.reference("rowRecordReference"))
                    }
                    if (rows.isNullOrEmpty()) Text else Rows(rows)
                }
                "weather" -> {
                    // An hourly forecast that does not read is left out, not the whole card.
                    val hours = content["hourlyForecast"].objects()?.map { hour ->
                        FeedHour(hour.text("forecastHour"), hour.flexibleText("forecastTemperature"), hour.text("forecastSymbol"), hour.flexibleText("precipitationChancePercent").toDoubleOrNull()?.roundToLong()?.toInt() ?: 0)
                    } ?: emptyList()
                    val weather = FeedWeather(
                        content.text("placeName"), content.flexibleText("currentTemperature"), content.text("temperatureUnit"), content.text("conditionText"),
                        content.text("conditionSymbol"), content.flexibleText("highTemperature"), content.flexibleText("lowTemperature"), hours,
                    )
                    if (weather.currentTemperature.isEmpty()) Text else Weather(weather)
                }
                else -> {
                    val entries = content["agendaEntries"].objects()?.map { entry ->
                        FeedAgendaEntry(
                            entry.text("startsAt"), entry.text("endsAt"), entry.text("agendaTitle"), FeedAgendaKind.of(entry.text("agendaEntryKind")),
                            entry.text("agendaDetail"), entry.reference("agendaRecordReference"),
                        )
                    }
                    if (entries.isNullOrEmpty()) Text else Agenda(entries)
                }
            }
        }
    }
}
