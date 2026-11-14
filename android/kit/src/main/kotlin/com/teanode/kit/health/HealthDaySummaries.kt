package com.teanode.kit.health

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

// The day summaries the phone works out itself. A quantity's summary comes from the platform's statistics, which merge
// the sources (a phone and a watch counting the same walk) the way the Health app does, so only the shape for them is
// here. A category (sleep, mindful minutes) and a workout have no such query, so they are worked out here from the
// samples' times: the union of the intervals, so two sources overlapping are counted once. Every function takes the
// zone to cut days with and never reads the current one.

/** HealthDays is the arithmetic on the phone's calendar days, written "YYYY-MM-DD". */
object HealthDays {
    /** The calendar day a time is in. */
    fun text(of: Instant, zone: ZoneId): String = of.atZone(zone).toLocalDate().toString()

    /** Whether the text is a day written as the server takes it. */
    fun isDayText(text: String): Boolean {
        if (text.length != 10 || text[4] != '-' || text[7] != '-') return false
        val digits = text.filterIndexed { index, _ -> index != 4 && index != 7 }
        if (!digits.all { character -> character in '0'..'9' }) return false
        val month = text.substring(5, 7).toInt()
        val day = text.substring(8, 10).toInt()
        return month in 1..12 && day in 1..31
    }

    /** The moment a day begins in the zone, or null when the text is not a day. */
    fun startOfDay(text: String, zone: ZoneId): Instant? {
        if (!isDayText(text)) return null
        val day = runCatching { LocalDate.parse(text) }.getOrNull() ?: return null
        return day.atStartOfDay(zone).toInstant()
    }

    /** From the beginning of a day to the beginning of the next: 23 or 25 hours when the clocks change. */
    fun interval(ofDay: String, zone: ZoneId): TimeSpan? {
        val start = startOfDay(ofDay, zone) ?: return null
        val next = LocalDate.parse(ofDay).plusDays(1).atStartOfDay(zone).toInstant()
        return TimeSpan(start, next)
    }

    /** The days a reading touches, earliest first. The end is not part of the reading: one that ends at midnight does not touch the day after. A reading that takes no time is on the day it is at. */
    fun days(from: Instant, to: Instant, zone: ZoneId): List<String> {
        val result = mutableListOf<String>()
        var dayStart = from.atZone(zone).toLocalDate()
        // A reading longer than ten years is a mistake in a date, not a stay.
        while (result.size < 3660) {
            result.add(dayStart.toString())
            val next = dayStart.plusDays(1)
            if (!next.atStartOfDay(zone).toInstant().isBefore(to)) break
            dayStart = next
        }
        return result
    }
}

/** A stretch of time, from [start] up to [end]. */
data class TimeSpan(val start: Instant, val end: Instant) {
    val durationSeconds: Double get() = (end.toEpochMilli() - start.toEpochMilli()) / 1000.0

    fun intersection(other: TimeSpan): TimeSpan? {
        val intersectionStart = if (start.isAfter(other.start)) start else other.start
        val intersectionEnd = if (end.isBefore(other.end)) end else other.end
        return if (intersectionEnd.isBefore(intersectionStart)) null else TimeSpan(intersectionStart, intersectionEnd)
    }
}

/** HealthIntervals is the union of time ranges. */
object HealthIntervals {
    /** The ranges merged: sorted, and any that overlap or touch made one. */
    fun union(intervals: List<TimeSpan>): List<TimeSpan> {
        val merged = mutableListOf<TimeSpan>()
        for (interval in intervals.sortedBy { each -> each.start }) {
            if (interval.durationSeconds <= 0) continue
            val last = merged.lastOrNull()
            if (last != null && !interval.start.isAfter(last.end)) {
                merged[merged.size - 1] = TimeSpan(last.start, if (last.end.isAfter(interval.end)) last.end else interval.end)
            } else {
                merged.add(interval)
            }
        }
        return merged
    }

    /** How many seconds the union of the ranges covers. */
    fun coveredSeconds(intervals: List<TimeSpan>): Double = union(intervals).sumOf { interval -> interval.durationSeconds }
}

/** HealthCategorySample is one category reading, as the day summaries need it. */
data class HealthCategorySample(
    val healthSampleId: String = "",
    val startedAt: Instant,
    val endedAt: Instant,
    /** The raw value; for sleep, a [HealthSleepStage]'s. */
    val categoryValue: Int,
) {
    companion object {
        /** From a sample about to be sent; null if it holds no category value. */
        fun of(sample: HealthSampleInput): HealthCategorySample? =
            sample.categoryValue?.let { value -> HealthCategorySample(sample.healthSampleId, sample.startedAt, sample.endedAt, value) }
    }
}

/** HealthWorkoutMetadata names where a workout's totals are in the metadata of its sample. */
object HealthWorkoutMetadata {
    const val TOTAL_ENERGY_BURNED_KILOCALORIES = "totalEnergyBurnedKilocalories"
    const val TOTAL_DISTANCE_METERS = "totalDistanceMeters"
}

/** HealthWorkoutSample is one workout, as the day summaries need it. */
data class HealthWorkoutSample(
    val healthSampleId: String = "",
    val startedAt: Instant,
    val endedAt: Instant,
    val totalEnergyKilocalories: Double? = null,
    val totalDistanceMeters: Double? = null,
) {
    companion object {
        /** From a "workout" sample about to be sent, reading the totals from its metadata; null for any other type. */
        fun of(sample: HealthSampleInput): HealthWorkoutSample? {
            if (sample.healthType != "workout") return null
            return HealthWorkoutSample(
                sample.healthSampleId, sample.startedAt, sample.endedAt,
                HealthJson.number(sample.sampleMetadata[HealthWorkoutMetadata.TOTAL_ENERGY_BURNED_KILOCALORIES]),
                HealthJson.number(sample.sampleMetadata[HealthWorkoutMetadata.TOTAL_DISTANCE_METERS]),
            )
        }
    }
}

/** HealthQuantityStatistics is what the platform's statistics answer for one quantity on one day. */
data class HealthQuantityStatistics(
    val sumValue: Double? = null,
    val averageValue: Double? = null,
    val minimumValue: Double? = null,
    val maximumValue: Double? = null,
    val sampleCount: Int = 0,
)

/** HealthReading is one reading of a quantity: when it began and its value in the unit the type is sent in. */
data class HealthReading(val startedAt: Instant, val value: Double)

/** HealthDaySummaries works out the summaries of days. */
object HealthDaySummaries {
    // Quantities

    /**
     * The statistics of each day from the readings themselves, by the day each began: how many there were, their
     * average, the least and the most. [days] limits the answer to those days; null is every day the readings touch.
     * A value that is not a number is left out.
     */
    fun readingStatistics(readings: List<HealthReading>, zone: ZoneId, days: Set<String>? = null): Map<String, HealthQuantityStatistics> {
        val valuesByDay = mutableMapOf<String, MutableList<Double>>()
        for (reading in readings) {
            if (!reading.value.isFinite()) continue
            val day = HealthDays.text(reading.startedAt, zone)
            if (days != null && !days.contains(day)) continue
            valuesByDay.getOrPut(day) { mutableListOf() }.add(reading.value)
        }
        val statistics = mutableMapOf<String, HealthQuantityStatistics>()
        for ((day, values) in valuesByDay) {
            val least = values.minOrNull() ?: continue
            val most = values.maxOrNull() ?: continue
            statistics[day] = HealthQuantityStatistics(averageValue = values.sum() / values.size, minimumValue = least, maximumValue = most, sampleCount = values.size)
        }
        return statistics
    }

    /**
     * The summary of a quantity on a day from the statistics, with only the numbers that mean something for the type:
     * the sum for a type that adds up, the average with the least and the most for one that averages.
     */
    fun quantitySummary(healthType: String, summaryDay: String, statistics: HealthQuantityStatistics, quantityUnit: String? = null): HealthDaySummaryInput {
        val info = HealthTypes.info(healthType)
        val unit = quantityUnit ?: info?.quantityUnit ?: ""
        return when (info?.summaryKind) {
            HealthSummaryKind.Sum -> HealthDaySummaryInput(
                healthType = healthType, summaryDay = summaryDay, quantityUnit = unit, sumValue = statistics.sumValue, sampleCount = statistics.sampleCount,
            )
            HealthSummaryKind.Average -> HealthDaySummaryInput(
                healthType = healthType, summaryDay = summaryDay, quantityUnit = unit, averageValue = statistics.averageValue,
                minimumValue = statistics.minimumValue, maximumValue = statistics.maximumValue, sampleCount = statistics.sampleCount,
            )
            else -> HealthDaySummaryInput(
                healthType = healthType, summaryDay = summaryDay, quantityUnit = unit, sumValue = statistics.sumValue,
                averageValue = statistics.averageValue, minimumValue = statistics.minimumValue, maximumValue = statistics.maximumValue,
                sampleCount = statistics.sampleCount,
            )
        }
    }

    // Categories

    /** A reading cut by the days it touches: each day with the part of the reading on it. */
    private fun parts(from: Instant, to: Instant, zone: ZoneId): List<Pair<String, TimeSpan>> {
        if (to.isBefore(from)) return emptyList()
        if (to == from) return listOf(HealthDays.text(from, zone) to TimeSpan(from, from))
        return HealthDays.days(from, to, zone).mapNotNull { day ->
            val window = HealthDays.interval(day, zone) ?: return@mapNotNull null
            val part = window.intersection(TimeSpan(from, to)) ?: return@mapNotNull null
            day to part
        }
    }

    /** Each distinct sample once: a sample read twice (from two queries) must not count twice. */
    private fun distinct(samples: List<HealthCategorySample>): List<HealthCategorySample> {
        val seen = mutableSetOf<String>()
        return samples.filter { sample -> sample.healthSampleId.isEmpty() || seen.add(sample.healthSampleId) }
    }

    /**
     * The days' summaries of a category type, by how its days add up in the table: sleep by stage; mindful minutes and
     * other durations as the time covered; events and symptoms as a count.
     */
    fun categorySummaries(healthType: String, samples: List<HealthCategorySample>, zone: ZoneId, days: Set<String>? = null): List<HealthDaySummaryInput> {
        if (healthType == "sleepAnalysis") return sleepSummaries(samples, zone, days)
        if (HealthTypes.info(healthType)?.summaryKind == HealthSummaryKind.Duration) return durationSummaries(healthType, samples, zone, days)
        return countSummaries(healthType, samples, zone, days)
    }

    /**
     * Sleep by day. Each stage's seconds are the union of that stage's intervals, so a phone and a watch both reporting
     * a night count it once; "asleep" is the union of the asleep stages together, which is the night's sleep.
     * durationSeconds is the asleep seconds. A night that crosses midnight is cut by the days, each day getting the
     * part on it. The count is the samples that touch the day.
     */
    fun sleepSummaries(samples: List<HealthCategorySample>, zone: ZoneId, days: Set<String>? = null): List<HealthDaySummaryInput> {
        val stageIntervals = mutableMapOf<String, MutableMap<Int, MutableList<TimeSpan>>>()
        val sampleCounts = mutableMapOf<String, Int>()
        for (sample in distinct(samples)) {
            for ((day, interval) in parts(sample.startedAt, sample.endedAt, zone)) {
                if (days != null && !days.contains(day)) continue
                sampleCounts[day] = (sampleCounts[day] ?: 0) + 1
                stageIntervals.getOrPut(day) { mutableMapOf() }.getOrPut(sample.categoryValue) { mutableListOf() }.add(interval)
            }
        }
        return sampleCounts.keys.sorted().map { day ->
            val byStage = stageIntervals[day].orEmpty()
            val seconds = mutableMapOf<String, Double>()
            for (stage in HealthSleepStage.entries) {
                val covered = HealthIntervals.coveredSeconds(byStage[stage.rawValue].orEmpty())
                if (covered > 0) seconds[stage.stageName] = covered
            }
            val asleepSeconds = HealthIntervals.coveredSeconds(HealthSleepStage.entries.filter { stage -> stage.isAsleep }.flatMap { stage -> byStage[stage.rawValue].orEmpty() })
            seconds[HealthSleepStage.ASLEEP_KEY] = asleepSeconds
            HealthDaySummaryInput(
                healthType = "sleepAnalysis", summaryDay = day, sampleCount = sampleCounts[day] ?: 0, durationSeconds = asleepSeconds, categorySeconds = seconds,
            )
        }
    }

    /** The time covered by a category's samples on each day, sources overlapping counted once (mindful minutes). */
    fun durationSummaries(healthType: String, samples: List<HealthCategorySample>, zone: ZoneId, days: Set<String>? = null): List<HealthDaySummaryInput> {
        val intervals = mutableMapOf<String, MutableList<TimeSpan>>()
        val sampleCounts = mutableMapOf<String, Int>()
        for (sample in distinct(samples)) {
            for ((day, interval) in parts(sample.startedAt, sample.endedAt, zone)) {
                if (days != null && !days.contains(day)) continue
                sampleCounts[day] = (sampleCounts[day] ?: 0) + 1
                intervals.getOrPut(day) { mutableListOf() }.add(interval)
            }
        }
        return sampleCounts.keys.sorted().map { day ->
            HealthDaySummaryInput(
                healthType = healthType, summaryDay = day, sampleCount = sampleCounts[day] ?: 0,
                durationSeconds = HealthIntervals.coveredSeconds(intervals[day].orEmpty()),
            )
        }
    }

    /** How many samples there are on each day, by the day each began (events and symptoms). */
    fun countSummaries(healthType: String, samples: List<HealthCategorySample>, zone: ZoneId, days: Set<String>? = null): List<HealthDaySummaryInput> {
        val sampleCounts = mutableMapOf<String, Int>()
        for (sample in distinct(samples)) {
            val day = HealthDays.text(sample.startedAt, zone)
            if (days != null && !days.contains(day)) continue
            sampleCounts[day] = (sampleCounts[day] ?: 0) + 1
        }
        return sampleCounts.keys.sorted().map { day -> HealthDaySummaryInput(healthType = healthType, summaryDay = day, sampleCount = sampleCounts[day] ?: 0) }
    }

    // Workouts

    /**
     * Workouts by the day each began: how many, the time they took (durationSeconds, added up), and, where the workouts
     * carried them, the energy burned and the distance. A workout that crosses midnight is all on the day it began.
     */
    fun workoutSummaries(workouts: List<HealthWorkoutSample>, zone: ZoneId, days: Set<String>? = null): List<HealthDaySummaryInput> {
        class Totals {
            var count = 0
            var seconds = 0.0
            var energy: Double? = null
            var distance: Double? = null
        }
        val byDay = mutableMapOf<String, Totals>()
        val seen = mutableSetOf<String>()
        for (workout in workouts) {
            if (workout.endedAt.isBefore(workout.startedAt)) continue
            if (workout.healthSampleId.isNotEmpty() && !seen.add(workout.healthSampleId)) continue
            val day = HealthDays.text(workout.startedAt, zone)
            if (days != null && !days.contains(day)) continue
            val totals = byDay.getOrPut(day) { Totals() }
            totals.count += 1
            totals.seconds += (workout.endedAt.toEpochMilli() - workout.startedAt.toEpochMilli()) / 1000.0
            workout.totalEnergyKilocalories?.takeIf { energy -> energy.isFinite() }?.let { energy -> totals.energy = (totals.energy ?: 0.0) + energy }
            workout.totalDistanceMeters?.takeIf { distance -> distance.isFinite() }?.let { distance -> totals.distance = (totals.distance ?: 0.0) + distance }
        }
        return byDay.keys.sorted().map { day ->
            val totals = byDay.getValue(day)
            val extras = mutableMapOf<String, Double>()
            totals.energy?.let { energy -> extras["energyKilocalories"] = energy }
            totals.distance?.let { distance -> extras["distanceMeters"] = distance }
            HealthDaySummaryInput(healthType = "workout", summaryDay = day, sampleCount = totals.count, durationSeconds = totals.seconds, categorySeconds = extras)
        }
    }
}
