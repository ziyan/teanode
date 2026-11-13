package com.teanode.kit.health

import java.math.BigDecimal
import java.math.RoundingMode
import java.text.NumberFormat
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale
import kotlin.math.abs

// How a value of Health is written, the rules of the table at the top of internal/health/words.go (the one reference
// the command line, the agent's tool, the dashboard and the apps copy). The rules that need no language are here, so
// that they are tested; the app puts the words that are a language's (steps, flights, hours) into the person's, around
// the numbers worked out here.

/** HealthCountNoun is what a count of a kind counts. */
enum class HealthCountNoun(val englishWord: String) {
    Step("step"),
    Flight("flight"),
    Push("push"),
    Stroke("stroke"),
    Fall("fall"),
    Puff("puff"),
    Drink("drink"),
}

/** HealthUnitWord is what follows a number. */
sealed class HealthUnitWord {
    /** A plain number. */
    data object None : HealthUnitWord()

    /** A symbol that touches the number: "97%", "36.6°C". */
    data class Touching(val symbol: String) : HealthUnitWord()

    /** A unit after a space: "74 bpm", "72.4 kg". */
    data class Spaced(val word: String) : HealthUnitWord()

    /** What is counted, in the singular when there is exactly one: "1 step", "8,412 steps". */
    data class Noun(val noun: HealthCountNoun, val isSingular: Boolean) : HealthUnitWord()
}

/** HealthValueParts is a value split into its number as a person reads it and the word that follows. */
data class HealthValueParts(val numberText: String, val unit: HealthUnitWord)

/** HealthDurationParts is a length of time in the units it is written with: "7 h 12 min", "35 min", "45 s". */
sealed class HealthDurationParts {
    data class Seconds(val seconds: Int) : HealthDurationParts()
    data class Minutes(val minutes: Int) : HealthDurationParts()
    data class Hours(val hours: Int) : HealthDurationParts()
    data class HoursMinutes(val hours: Int, val minutes: Int) : HealthDurationParts()
}

/** HealthValueWords works out how numbers, times and facts about the person are written. */
object HealthValueWords {
    /** The kinds whose unit is a plain "count" that counts something. */
    internal val countNouns: Map<String, HealthCountNoun> = mapOf(
        "stepCount" to HealthCountNoun.Step, "flightsClimbed" to HealthCountNoun.Flight, "pushCount" to HealthCountNoun.Push,
        "swimmingStrokeCount" to HealthCountNoun.Stroke, "numberOfTimesFallen" to HealthCountNoun.Fall, "inhalerUsage" to HealthCountNoun.Puff,
        "numberOfAlcoholicBeverages" to HealthCountNoun.Drink,
    )

    /** The kinds measured in "count/min" that are not beats. */
    private val rateWords: Map<String, String> = mapOf("cyclingCadence" to "rpm", "respiratoryRate" to "breaths/min")

    /** The unit strings as a person writes them; one not here is written as it is, and an empty word is a plain number. */
    private val unitWords: Map<String, String> = mapOf(
        "degC" to "°C", "degF" to "°F", "ml/(kg*min)" to "mL/kg·min", "kcal/(kg*hr)" to "kcal/kg·hr", "dBASPL" to "dB", "appleEffortScore" to "",
    )

    /**
     * How many decimals a percentage is worth, for the kinds whose size or whose meaning decides it: blood oxygen and
     * walking steadiness are read in whole numbers, body fat, walking asymmetry and walking double support in one.
     */
    private val percentDecimals: Map<String, Int> = mapOf(
        "oxygenSaturation" to 0, "appleWalkingSteadiness" to 0, "bodyFatPercentage" to 1, "walkingAsymmetryPercentage" to 1,
        "walkingDoubleSupportPercentage" to 1,
    )

    /** A distance from this many meters up is written in kilometers. */
    private const val KILOMETER_FROM_METERS = 1000.0

    // Numbers

    /** How many decimals a number of this size is worth: none from 1,000, one from 100, else two. */
    fun precisionDigits(forValue: Double): Int {
        val magnitude = abs(forValue)
        if (magnitude >= 1000) return 0
        if (magnitude >= 100) return 1
        return 2
    }

    /**
     * A number with up to this many decimals (trailing zeros dropped) and thousands separated. A tie is rounded half
     * away from zero as the number is written in its shortest decimal form, like the table in words.go says: 0.35 to
     * one decimal is 0.4, 1.005 to two is 1.01, 2.5 to none is 3 and -0.5 is -1.
     */
    fun numberText(value: Double, maximumFractionDigits: Int, locale: Locale = Locale.getDefault()): String {
        val rounded = BigDecimal.valueOf(value).setScale(maximumFractionDigits, RoundingMode.HALF_UP).stripTrailingZeros()
        val format = NumberFormat.getNumberInstance(locale)
        format.minimumFractionDigits = 0
        format.maximumFractionDigits = maximumFractionDigits
        format.isGroupingUsed = true
        // A tiny negative number is zero, not "-0".
        return format.format(if (rounded.signum() == 0) BigDecimal.ZERO else rounded)
    }

    /** A reading's number with as many decimals as its size is worth: 8,412, 150.3, 72.4, 0.35. */
    fun numberText(value: Double, locale: Locale = Locale.getDefault()): String = numberText(value, precisionDigits(value), locale)

    /**
     * A reading split into its number and its unit. A percentage is a fraction (0.975); it is written times 100, in
     * whole numbers or with one decimal for the kinds in [percentDecimals].
     *
     * - Counts of something and rates are whole numbers ("74 bpm", "8,412 steps", and "1 step" for exactly one).
     * - A count that counts nothing in particular keeps one decimal and drops a trailing .0 (22.5, 3).
     * - Meters from 1,000 are kilometers with one decimal and no trailing .0.
     */
    fun parts(healthType: String, value: Double, quantityUnit: String, locale: Locale = Locale.getDefault()): HealthValueParts {
        val countNoun = countNouns[healthType]
        if (quantityUnit == "m" && abs(value) >= KILOMETER_FROM_METERS) {
            return HealthValueParts(numberText(value / KILOMETER_FROM_METERS, 1, locale), HealthUnitWord.Spaced("km"))
        }
        if (quantityUnit == "%") {
            val shownValue = value * 100
            val decimals = percentDecimals[healthType]
            val percentText = if (decimals != null) numberText(shownValue, decimals, locale) else numberText(shownValue, locale)
            return HealthValueParts(percentText, HealthUnitWord.Touching("%"))
        }
        if (quantityUnit == "count/min") {
            return HealthValueParts(numberText(roundedHalfAwayFromZero(value), 0, locale), HealthUnitWord.Spaced(rateWords[healthType] ?: "bpm"))
        }
        if (quantityUnit == "count" && countNoun != null) {
            val rounded = roundedHalfAwayFromZero(value)
            return HealthValueParts(numberText(rounded, 0, locale), HealthUnitWord.Noun(countNoun, abs(rounded) == 1.0))
        }
        if (quantityUnit == "count") return HealthValueParts(numberText(value, 1, locale), HealthUnitWord.None)
        val number = numberText(value, locale)
        if (quantityUnit.isEmpty()) return HealthValueParts(number, HealthUnitWord.None)
        val word = unitWords[quantityUnit] ?: quantityUnit
        if (word.isEmpty()) return HealthValueParts(number, HealthUnitWord.None)
        return HealthValueParts(number, if (word.startsWith("°")) HealthUnitWord.Touching(word) else HealthUnitWord.Spaced(word))
    }

    /** A tie goes away from zero, as the table says; `Math.round` takes a negative tie toward zero. */
    private fun roundedHalfAwayFromZero(value: Double): Double = BigDecimal.valueOf(value).setScale(0, RoundingMode.HALF_UP).toDouble()

    /** The same, written in English the way the command line writes it: "8,412 steps", "74 bpm", "97.5%". */
    fun englishText(healthType: String, value: Double, quantityUnit: String): String {
        val valueParts = parts(healthType, value, quantityUnit, Locale.US)
        return when (val unit = valueParts.unit) {
            HealthUnitWord.None -> valueParts.numberText
            is HealthUnitWord.Touching -> valueParts.numberText + unit.symbol
            is HealthUnitWord.Spaced -> valueParts.numberText + " " + unit.word
            is HealthUnitWord.Noun -> valueParts.numberText + " " + englishNoun(unit.noun, unit.isSingular)
        }
    }

    private fun englishNoun(noun: HealthCountNoun, isSingular: Boolean): String {
        if (isSingular) return noun.englishWord
        return if (noun == HealthCountNoun.Push) "pushes" else noun.englishWord + "s"
    }

    // Time

    /** A length of time in hours and minutes: seconds are rounded to the nearest second below a minute, and to the nearest minute from a minute on. */
    fun durationParts(seconds: Double): HealthDurationParts {
        val wholeSeconds = roundedHalfAwayFromZero(seconds).toInt()
        if (wholeSeconds < 60) return HealthDurationParts.Seconds(maxOf(0, wholeSeconds))
        val totalMinutes = roundedHalfAwayFromZero(seconds / 60).toInt()
        val hours = totalMinutes / 60
        val minutes = totalMinutes % 60
        if (hours == 0) return HealthDurationParts.Minutes(minutes)
        if (minutes == 0) return HealthDurationParts.Hours(hours)
        return HealthDurationParts.HoursMinutes(hours, minutes)
    }

    // Facts about the person

    /** The age in whole years on [today] of someone born on a day written "YYYY-MM-DD"; null if that is not a day or is after [today]. */
    fun ageInYears(dateOfBirth: String, today: Instant, zone: ZoneId): Int? {
        if (!HealthDays.isDayText(dateOfBirth)) return null
        val born = runCatching { LocalDate.parse(dateOfBirth) }.getOrNull() ?: return null
        val now = today.atZone(zone).toLocalDate()
        if (born.isAfter(now)) return null
        return java.time.Period.between(born, now).years
    }

    /** A blood type as it is written on a card: "aPositive" is "A+", "abNegative" is "AB-". Anything else is written as it was sent. */
    fun bloodTypeText(characteristicValue: String): String {
        val groups = listOf("ab" to "AB", "a" to "A", "b" to "B", "o" to "O")
        for ((prefix, letters) in groups) {
            if (characteristicValue == prefix + "Positive") return "$letters+"
            if (characteristicValue == prefix + "Negative") return "$letters-"
        }
        return characteristicValue
    }

    /** The value a characteristic has when it was never set in Health. */
    const val NOT_SET_VALUE = "notSet"
}
