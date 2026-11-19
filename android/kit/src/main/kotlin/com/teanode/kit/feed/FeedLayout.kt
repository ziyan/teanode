package com.teanode.kit.feed

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentReference
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import java.time.LocalDateTime
import java.time.OffsetDateTime
import java.time.format.DateTimeFormatter

/** One row of a list card: a story, a mention, a review request, a mail. */
@Serializable
data class FeedListRow(
    val rowTitle: String = "",
    val rowDetail: String = "",
    /** The source of a story, or the channel and time of a mention. */
    val rowMeta: String = "",
    val rowSymbol: String = "",
    val rowUrl: String = "",
    val rowRecordReference: JsonElement? = null,
) {
    /** The record the row points at, when it names one the app reads. */
    val recordReference: AgentReference?
        get() = rowRecordReference?.let { element -> runCatching { TeaNodeJson.decodeFromJsonElement(AgentReference.serializer(), element) }.getOrNull() }

    /** What a tap on the row follows: its web address (http or https only), else the record it points at; null for a row that only reads. */
    val linkAddress: String?
        get() {
            val lowered = rowUrl.lowercase()
            if (lowered.startsWith("http://") || lowered.startsWith("https://")) return rowUrl
            return recordReference?.feedLinkAddress
        }
}

@Serializable
data class FeedListContent(val listRows: List<FeedListRow> = emptyList())

@Serializable
data class FeedHourForecast(
    /** An ISO time. */
    val forecastHour: String = "",
    val forecastTemperature: Double? = null,
    val forecastSymbol: String = "",
    val precipitationChancePercent: Int? = null,
) {
    /** The hour as the place writes it, `09:00`; the text itself when it is not a time. */
    val hourLabel: String
        get() = runCatching { OffsetDateTime.parse(forecastHour).format(HOUR_FORMAT) }
            .recoverCatching { LocalDateTime.parse(forecastHour).format(HOUR_FORMAT) }
            .getOrDefault(forecastHour)

    private companion object {
        val HOUR_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
    }
}

@Serializable
data class FeedWeatherContent(
    val placeName: String = "",
    val currentTemperature: Double? = null,
    /** C or F. */
    val temperatureUnit: String = "",
    val conditionText: String = "",
    val conditionSymbol: String = "",
    val highTemperature: Double? = null,
    val lowTemperature: Double? = null,
    val hourlyForecast: List<FeedHourForecast> = emptyList(),
) {
    /** A temperature as a whole number with the degree sign, `21°`; the unit goes on the current one only. */
    fun degrees(temperature: Double?): String = if (temperature == null) "" else "${Math.round(temperature)}°"

    val currentText: String get() = degrees(currentTemperature)
}

@Serializable
data class FeedAgendaEntry(
    val startsAt: String = "",
    val endsAt: String = "",
    val agendaTitle: String = "",
    /** meeting, focus, task or break. */
    val agendaEntryKind: String = "",
    val agendaDetail: String = "",
    val agendaRecordReference: JsonElement? = null,
) {
    val recordReference: AgentReference?
        get() = agendaRecordReference?.let { element -> runCatching { TeaNodeJson.decodeFromJsonElement(AgentReference.serializer(), element) }.getOrNull() }

    val kind: FeedAgendaKind get() = FeedAgendaKind.of(agendaEntryKind)

    /** The time it starts, `09:30`, written in the clock of the time it was given in; empty for an entry with none. */
    val startText: String get() = clockText(startsAt)

    val endText: String get() = clockText(endsAt)

    private fun clockText(time: String): String = runCatching { OffsetDateTime.parse(time).format(CLOCK) }
        .recoverCatching { LocalDateTime.parse(time).format(CLOCK) }
        .getOrDefault("")

    private companion object {
        val CLOCK: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
    }
}

enum class FeedAgendaKind {
    Meeting, Focus, Task, Break, Other;

    companion object {
        fun of(name: String): FeedAgendaKind = when (name) {
            "meeting" -> Meeting
            "focus" -> Focus
            "task" -> Task
            "break" -> Break
            else -> Other
        }
    }
}

@Serializable
data class FeedAgendaContent(val agendaEntries: List<FeedAgendaEntry> = emptyList())

/**
 * FeedLayout is how a card is drawn. Anything the app cannot draw (an unknown layout, content that is not a JSON
 * object of the layout's shape, or content with nothing to show) is [Text], the card's plain body, which every card
 * carries for exactly this.
 */
sealed class FeedLayout {
    data object Text : FeedLayout()

    data class Rows(val rows: List<FeedListRow>) : FeedLayout()

    data class Weather(val weather: FeedWeatherContent) : FeedLayout()

    data class Agenda(val entries: List<FeedAgendaEntry>) : FeedLayout()

    companion object {
        fun read(layoutName: String, layoutContent: String): FeedLayout {
            if (layoutName == "text" || layoutName.isEmpty() || layoutContent.isBlank()) return Text
            val element = runCatching { TeaNodeJson.parseToJsonElement(layoutContent) }.getOrNull() as? JsonObject ?: return Text
            return runCatching {
                when (layoutName) {
                    "list" -> TeaNodeJson.decodeFromJsonElement(FeedListContent.serializer(), element).listRows.filter { row -> row.rowTitle.isNotEmpty() }
                        .let { rows -> if (rows.isEmpty()) Text else Rows(rows) }
                    "weather" -> TeaNodeJson.decodeFromJsonElement(FeedWeatherContent.serializer(), element)
                        .let { weather -> if (weather.currentTemperature == null && weather.conditionText.isEmpty()) Text else Weather(weather) }
                    "agenda" -> TeaNodeJson.decodeFromJsonElement(FeedAgendaContent.serializer(), element).agendaEntries.filter { entry -> entry.agendaTitle.isNotEmpty() }
                        .let { entries -> if (entries.isEmpty()) Text else Agenda(entries) }
                    else -> Text
                }
            }.getOrDefault(Text)
        }
    }
}
