package com.teanode.app.health

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.kit.health.HealthCategory
import com.teanode.kit.health.HealthCountNoun
import com.teanode.kit.health.HealthDays
import com.teanode.kit.health.HealthDurationParts
import com.teanode.kit.health.HealthSleepStage
import com.teanode.kit.health.HealthSummaryKind
import com.teanode.kit.health.HealthTypeView
import com.teanode.kit.health.HealthTypes
import com.teanode.kit.health.HealthUnitWord
import com.teanode.kit.health.HealthValueWords
import com.teanode.kit.health.HealthSampleView
import com.teanode.kit.health.HealthValueKind
import com.teanode.kit.health.HealthWorkoutMetadata
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/** The sentence-case words of a camel-case identifier with its resource name's form: "stepCount" is "step_count". */
internal fun snakeCase(identifier: String): String = identifier.replace(Regex("([a-z0-9])([A-Z])"), "$1_$2").lowercase()

/**
 * The words the Health screens say for Health's own things, in the person's language: the Health app's groups, the kinds
 * of data, workouts and the stages of sleep, and how numbers, times and days are written. The rules that need no language
 * are the kit's [HealthValueWords], which is tested; what is added here is the words that are a language's.
 */
class HealthText(
    private val context: Context,
    private val locale: Locale = Locale.getDefault(),
    private val zone: ZoneId = ZoneId.systemDefault(),
) {
    private fun string(resource: Int, vararg arguments: Any): String = context.getString(resource, *arguments)

    private fun named(prefix: String, name: String): String? {
        val resource = context.resources.getIdentifier("health_${prefix}_${snakeCase(name)}", "string", context.packageName)
        return if (resource == 0) null else context.getString(resource)
    }

    val averageLabel: String get() = string(R.string.health_average)
    val lowestLabel: String get() = string(R.string.health_lowest)
    val highestLabel: String get() = string(R.string.health_highest)
    val averagePerDayLabel: String get() = string(R.string.health_average_per_day)
    val perDayLabel: String get() = string(R.string.health_per_day)
    fun minutesAxis(number: String): String = string(R.string.health_axis_minutes, number)
    fun hoursAxis(number: String): String = string(R.string.health_axis_hours, number)
    fun everyoneIn(name: String): String = string(R.string.health_everyone_in, name)
    fun sharingSaved(name: String): String = string(R.string.health_sharing_with_saved, name)
    fun stoppedSharing(name: String): String = string(R.string.health_stopped_sharing_with, name)
    val couldNotSaveSharing: String get() = string(R.string.health_could_not_save_sharing)
    fun rangeCut(period: com.teanode.kit.health.HealthPeriod, count: Int): String =
        if (period == com.teanode.kit.health.HealthPeriod.Week) string(R.string.health_only_latest_weeks, count) else string(R.string.health_only_latest_days, count)
    fun rangeText(from: String, until: String): String = string(R.string.health_range_from_to, from, until)

    val title: String get() = string(R.string.health_title)
    val periodTitle: String get() = string(R.string.health_period)
    val yearRangeTitle: String get() = string(R.string.health_range_year)
    val totalTitle: String get() = string(R.string.health_total)
    val erasedOnServerText: String get() = string(R.string.health_erased_on_server)

    fun categoryName(category: HealthCategory): String = named("category", category.serverWord) ?: category.categoryName

    /** The name of a kind: the app's own words for the ones it knows, else the English name the server sends. */
    fun typeName(healthType: String, serverName: String): String =
        named("type", healthType) ?: serverName.ifEmpty { HealthTypes.typeName(healthType) }

    fun workoutName(workoutActivityType: String): String {
        if (workoutActivityType.isEmpty()) return named("workout", "other").orEmpty()
        return named("workout", workoutActivityType) ?: readAsWords(workoutActivityType)
    }

    fun stageName(stage: HealthSleepStage): String = named("stage", stage.stageName) ?: stage.stageName

    /** A characteristic as a person reads it: a date of birth with the age, a blood type as on a card. */
    fun characteristicText(healthType: String, characteristicValue: String, today: Instant = Instant.now()): String {
        if (characteristicValue == HealthValueWords.NOT_SET_VALUE) return string(R.string.health_not_set)
        return when (healthType) {
            "dateOfBirth" -> {
                val age = HealthValueWords.ageInYears(characteristicValue, today, zone) ?: return characteristicValue
                val ageText = if (age == 1) string(R.string.health_age_single) else string(R.string.health_age_plural, age)
                "$characteristicValue ($ageText)"
            }
            "biologicalSex" -> when (characteristicValue) {
                "female" -> string(R.string.health_female)
                "male" -> string(R.string.health_male)
                "other" -> string(R.string.health_other)
                else -> characteristicValue
            }
            "bloodType" -> HealthValueWords.bloodTypeText(characteristicValue)
            "wheelchairUse" -> when (characteristicValue) {
                "yes" -> string(R.string.health_yes)
                "no" -> string(R.string.health_no)
                else -> characteristicValue
            }
            "activityMoveMode" -> when (characteristicValue) {
                "activeEnergy" -> typeName("activeEnergyBurned", "Active energy")
                "appleMoveTime" -> typeName("appleMoveTime", "Move minutes")
                else -> characteristicValue
            }
            else -> characteristicValue
        }
    }

    // Numbers and units

    fun quantityText(value: Double, healthType: String, quantityUnit: String): String {
        val parts = HealthValueWords.parts(healthType, value, quantityUnit, locale)
        return when (val unit = parts.unit) {
            HealthUnitWord.None -> parts.numberText
            is HealthUnitWord.Touching -> parts.numberText + unit.symbol
            is HealthUnitWord.Spaced -> parts.numberText + " " + unit.word
            is HealthUnitWord.Noun -> countText(parts.numberText, unit.noun, unit.isSingular)
        }
    }

    private fun countText(number: String, noun: HealthCountNoun, isSingular: Boolean): String =
        string(
            context.resources.getIdentifier("health_count_${noun.englishWord}_${if (isSingular) "single" else "plural"}", "string", context.packageName),
            number,
        )

    fun numberText(value: Double): String = HealthValueWords.numberText(value, locale)

    fun durationText(seconds: Double): String = when (val parts = HealthValueWords.durationParts(seconds)) {
        is HealthDurationParts.Seconds -> string(R.string.health_seconds_short, parts.seconds)
        is HealthDurationParts.Minutes -> string(R.string.health_minutes_short, parts.minutes)
        is HealthDurationParts.Hours -> string(R.string.health_hours_short, parts.hours)
        is HealthDurationParts.HoursMinutes -> string(R.string.health_hours_minutes_short, parts.hours, parts.minutes)
    }

    fun amountText(value: Double?, summaryKind: HealthSummaryKind, healthType: String, quantityUnit: String): String {
        if (value == null) return string(R.string.health_none)
        return when (summaryKind) {
            HealthSummaryKind.Duration -> durationText(value)
            HealthSummaryKind.Count -> numberText(value)
            HealthSummaryKind.Sum, HealthSummaryKind.Average -> quantityText(value, healthType, quantityUnit)
        }
    }

    /** The latest day's amount of a kind, for its row. */
    fun latestAmountText(type: HealthTypeView): String? {
        val latest = type.latestDaySummary ?: return null
        val summaryKind = type.summaryKindValue ?: HealthSummaryKind.Sum
        val value = summaryValue(latest.sumValue, latest.averageValue, latest.durationSeconds, latest.sampleCount, summaryKind)
        return amountText(value, summaryKind, type.healthType, type.quantityUnit)
    }

    /** A short number for an axis: 12k, 1.2M. */
    fun axisText(value: Double): String {
        val size = Math.abs(value)
        if (size >= 1_000_000) return HealthValueWords.numberText(value / 1_000_000, 1, locale) + "M"
        if (size >= 10_000) return HealthValueWords.numberText(value / 1000, 0, locale) + "k"
        return numberText(value)
    }

    // Days and times

    fun date(ofDay: String): LocalDate? = runCatching { LocalDate.parse(ofDay.take(10)) }.getOrNull()

    fun mediumDayText(day: String): String = date(day)?.let { date -> DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(locale).format(date) } ?: day

    fun shortDayText(day: String): String =
        date(day)?.let { date -> DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, "MMMd"), locale).format(date) } ?: day

    fun monthText(day: String, isYearIncluded: Boolean): String {
        val date = date(day) ?: return day
        return DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, if (isYearIncluded) "MMMy" else "MMM"), locale).format(date)
    }

    fun monthLongText(day: String): String? =
        date(day)?.let { date -> DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, "MMMMy"), locale).format(date) }

    fun momentText(instant: Instant): String =
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(locale).format(instant.atZone(zone))

    // Readings

    /** What a reading says: its value, a workout's kind and length, a sleep stage and length. */
    fun readingText(sample: HealthSampleView, type: HealthTypeView): String {
        val started = sample.startedInstant
        val ended = sample.endedInstant ?: started
        val durationSeconds = if (started == null || ended == null) 0.0 else maxOf(0.0, (ended.toEpochMilli() - started.toEpochMilli()) / 1000.0)
        sample.quantityValue?.let { value ->
            val sentUnit = sample.quantityUnit.ifEmpty { type.quantityUnit }
            return quantityText(value, type.healthType, sentUnit)
        }
        if (type.valueKindValue == HealthValueKind.Workout) return workoutName(sample.workoutActivityType) + ", " + durationText(durationSeconds)
        if (type.healthType == "sleepAnalysis") {
            val stage = sample.categoryValue?.let { raw -> HealthSleepStage.fromRawValue(raw) }
            if (stage != null) return stageName(stage) + ", " + durationText(durationSeconds)
        }
        if (durationSeconds < 1) return typeName(type.healthType, type.typeName)
        return durationText(durationSeconds)
    }

    /** The day number of a range's end and the earlier days in it, as the chart and its window need them. */
    fun days(from: String, to: String, dayCountMost: Int = 400): List<String> {
        val first = date(from) ?: return emptyList()
        val last = date(to) ?: return emptyList()
        if (first.isAfter(last)) return emptyList()
        val result = mutableListOf<String>()
        var day = first
        while (!day.isAfter(last) && result.size < dayCountMost) {
            result.add(day.toString())
            day = day.plusDays(1)
        }
        return result
    }

    fun dayCount(from: String, to: String): Int {
        val first = date(from) ?: return 1
        val last = date(to) ?: return 1
        return maxOf(1, java.time.temporal.ChronoUnit.DAYS.between(first, last).toInt() + 1)
    }

    fun day(day: String, adding: Int): String = date(day)?.plusDays(adding.toLong())?.toString() ?: day

    fun today(): String = HealthDays.text(Instant.now(), zone)

    companion object {
        /** The number a kind's summary reads as: the sum, the average, the time covered, or the count. */
        fun summaryValue(sumValue: Double?, averageValue: Double?, durationSeconds: Double?, sampleCount: Int, summaryKind: HealthSummaryKind): Double? = when (summaryKind) {
            HealthSummaryKind.Sum -> sumValue
            HealthSummaryKind.Average -> averageValue
            HealthSummaryKind.Duration -> durationSeconds
            HealthSummaryKind.Count -> sampleCount.toDouble()
        }

        /** The identifier read as words: "stairClimbing" is "Stair climbing". */
        fun readAsWords(identifier: String): String {
            val words = StringBuilder()
            for (character in identifier) {
                if (character.isUpperCase() && words.isNotEmpty()) {
                    words.append(' ')
                    words.append(character.lowercaseChar())
                } else {
                    words.append(character)
                }
            }
            return words.toString().replaceFirstChar { first -> first.uppercase() }
        }

        /** The number a workout's metadata holds, when it is one. */
        fun metadataNumber(value: kotlinx.serialization.json.JsonElement?): Double? = com.teanode.kit.health.HealthJson.number(value)

        const val TOTAL_DISTANCE_KEY = HealthWorkoutMetadata.TOTAL_DISTANCE_METERS
        const val TOTAL_ENERGY_KEY = HealthWorkoutMetadata.TOTAL_ENERGY_BURNED_KILOCALORIES
    }
}

@Composable
fun rememberHealthText(): HealthText {
    val context = LocalContext.current
    return remember(context) { HealthText(context) }
}
