package com.teanode.app.health

import androidx.compose.ui.graphics.Color
import com.teanode.app.charts.ChartFigure
import com.teanode.app.charts.ChartKeyEntry
import com.teanode.app.charts.ChartPalette
import com.teanode.kit.health.HealthPeriod
import com.teanode.kit.health.HealthPeriodView
import com.teanode.kit.health.HealthSleepStage
import com.teanode.kit.health.HealthSummaryKind
import com.teanode.kit.health.HealthSummaryView
import com.teanode.kit.health.HealthTypeView
import com.teanode.kit.health.HealthValueKind

// What a chart of one kind of Health data draws, worked out from the server's periods. Bars for what adds up (steps,
// energy, time), the night's stages stacked for sleep, and a line with a band from the lowest to the highest reading for
// what is read as a level (heart rate, weight). The same plan as the dashboard's (web/src/pages/health/healthChart.tsx)
// and the iPhone app's HealthChartPlan.swift.

/** How far back a chart looks. */
enum class HealthRange(val dayCount: Int) {
    ThirtyDays(30),
    NinetyDays(90),
    Year(365),
    All(0);

    /** The period a range reads best in, until the person chooses one. */
    val defaultPeriod: HealthPeriod
        get() = when (this) {
            All -> HealthPeriod.Month
            Year -> HealthPeriod.Week
            ThirtyDays, NinetyDays -> HealthPeriod.Day
        }
}

/** The numbers under a chart. */
data class HealthNumbers(
    val averageValue: Double? = null,
    val averagePerDayValue: Double? = null,
    val totalValue: Double? = null,
    val lowestValue: Double? = null,
    val highestValue: Double? = null,
)

/** One day, week or month of a chart: its place, its amount, and what a touch says of it. */
data class HealthPlot(
    val periodKey: String,
    val titleText: String,
    val amountValue: Double?,
    val lowestValue: Double? = null,
    val highestValue: Double? = null,
    val figures: List<ChartFigure>,
    val spokenText: String,
)

/** A figure listed under a chart. */
data class HealthFigureRow(val label: String, val valueText: String)

/** One stage of one night, as a stacked bar's piece. */
data class HealthStageSegment(val periodKey: String, val stage: HealthSleepStage, val stageTitle: String, val hours: Double)

class HealthChartPlan(
    val shape: Shape,
    val summaryKind: HealthSummaryKind,
    val healthType: String,
    val quantityUnit: String,
    val period: HealthPeriod,
    val periodKeys: List<String>,
    val plots: List<HealthPlot>,
    val stageSegments: List<HealthStageSegment>,
    val numbers: HealthNumbers,
    val drawnStages: List<HealthSleepStage>,
    /** How many seconds one unit of a time chart's axis is: an hour, or a minute for workouts. */
    val chartUnitSeconds: Double,
    private val text: HealthText,
) {
    enum class Shape { Bars, Stages, Level }

    val hasData: Boolean get() = plots.isNotEmpty()

    // The axes

    /** The periods named on the horizontal axis: at most five, evenly spaced, the first and the last among them. */
    val labelledKeys: List<String>
        get() {
            if (periodKeys.size <= 5) return periodKeys
            val step = (periodKeys.size - 1) / 4.0
            return (0..4).map { index -> periodKeys[Math.round(index * step).toInt()] }
        }

    private val doesSpanYears: Boolean get() = periodKeys.isNotEmpty() && periodKeys.first().take(4) != periodKeys.last().take(4)

    fun axisLabel(periodKey: String): String =
        if (period == HealthPeriod.Month) text.monthText(periodKey, doesSpanYears) else text.shortDayText(periodKey)

    fun axisText(value: Double): String {
        if (summaryKind == HealthSummaryKind.Duration) {
            val number = text.axisText(value)
            return if (chartUnitSeconds == MINUTE_SECONDS) text.minutesAxis(number) else text.hoursAxis(number)
        }
        return text.axisText(if (quantityUnit == "%") value * 100 else value)
    }

    // The legend of sleep

    val stageKeyEntries: List<ChartKeyEntry> get() = drawnStages.map { stage -> ChartKeyEntry(text.stageName(stage), color(stage)) }
    val stageTitles: List<String> get() = drawnStages.map { stage -> text.stageName(stage) }

    // The figures under the chart

    val figureRows: List<HealthFigureRow>
        get() {
            fun amount(value: Double?) = text.amountText(value, summaryKind, healthType, quantityUnit)
            return when (summaryKind) {
                HealthSummaryKind.Average -> listOf(
                    HealthFigureRow(text.averageLabel, amount(numbers.averageValue)),
                    HealthFigureRow(text.lowestLabel, amount(numbers.lowestValue)),
                    HealthFigureRow(text.highestLabel, amount(numbers.highestValue)),
                )
                HealthSummaryKind.Count -> listOf(HealthFigureRow(text.totalTitle, amount(numbers.totalValue)))
                HealthSummaryKind.Sum, HealthSummaryKind.Duration ->
                    // The nights of a chart of sleep are not added together: a total of 177 hours says nothing about any night.
                    if (shape == Shape.Stages) {
                        listOf(HealthFigureRow(text.averagePerDayLabel, amount(numbers.averagePerDayValue)))
                    } else {
                        listOf(
                            HealthFigureRow(text.averagePerDayLabel, amount(numbers.averagePerDayValue)),
                            HealthFigureRow(text.totalTitle, amount(numbers.totalValue)),
                        )
                    }
            }
        }

    companion object {
        const val HOUR_SECONDS = 3600.0
        const val MINUTE_SECONDS = 60.0

        /** The stages a night's bar is made of, deepest first; time in bed shows only when nothing else does. */
        val shownStages: List<HealthSleepStage> = listOf(
            HealthSleepStage.AsleepDeep, HealthSleepStage.AsleepCore, HealthSleepStage.AsleepREM, HealthSleepStage.AsleepUnspecified, HealthSleepStage.Awake,
        )

        fun color(stage: HealthSleepStage): Color = when (stage) {
            HealthSleepStage.AsleepDeep -> ChartPalette.seriesColor(6)
            HealthSleepStage.AsleepCore -> ChartPalette.seriesColor(0)
            HealthSleepStage.AsleepREM -> ChartPalette.seriesColor(3)
            HealthSleepStage.AsleepUnspecified -> ChartPalette.seriesColor(2)
            HealthSleepStage.Awake -> ChartPalette.seriesColor(1)
            HealthSleepStage.InBed -> ChartPalette.restColor
        }

        /** Works out what to draw from the server's periods. */
        fun make(summary: HealthSummaryView, type: HealthTypeView, period: HealthPeriod, fromDay: String, toDay: String, text: HealthText): HealthChartPlan {
            val summaryKind = summary.summaryKindValue ?: type.summaryKindValue ?: HealthSummaryKind.Sum
            val quantityUnit = summary.quantityUnit.ifEmpty { type.quantityUnit }
            val chartUnitSeconds = if (type.valueKindValue == HealthValueKind.Workout) MINUTE_SECONDS else HOUR_SECONDS
            val filledPeriods = summary.periods.filter { row -> row.sampleCount > 0 }
            val rowsByKey = filledPeriods.associateBy { row -> row.startDay.take(10) }
            val periodKeys = if (period == HealthPeriod.Day) text.days(fromDay, toDay) else summary.periods.map { row -> row.startDay.take(10) }
            val isStaged = summaryKind == HealthSummaryKind.Duration && filledPeriods.any { row -> stageSeconds(row).isNotEmpty() }
            val shape = if (isStaged) Shape.Stages else if (summaryKind == HealthSummaryKind.Average) Shape.Level else Shape.Bars
            val builder = HealthPlotBuilder(summaryKind, type.healthType, quantityUnit, period, fromDay, toDay, shape, chartUnitSeconds, text)
            val plots = mutableListOf<HealthPlot>()
            val stageSegments = mutableListOf<HealthStageSegment>()
            for (periodKey in periodKeys) {
                val row = rowsByKey[periodKey] ?: continue
                plots.add(builder.plot(row, periodKey))
                if (isStaged) stageSegments.addAll(builder.segments(row, periodKey))
            }
            val segmentStages = stageSegments.map { segment -> segment.stage }.toSet()
            val drawnStages = (shownStages + HealthSleepStage.InBed).filter { stage -> stage in segmentStages }
            return HealthChartPlan(
                shape, summaryKind, type.healthType, quantityUnit, period, periodKeys, plots, stageSegments,
                numbers(filledPeriods, summaryKind, fromDay, toDay, text), drawnStages, chartUnitSeconds, text,
            )
        }

        /** The seconds of each sleep stage of a period; time in bed only when no stage was drawn. */
        fun stageSeconds(row: HealthPeriodView): Map<String, Double> {
            val seconds = mutableMapOf<String, Double>()
            val bySeconds = row.categorySecondsByStage
            for (stage in shownStages) {
                val value = bySeconds[stage.stageName]
                if (value != null && value > 0) seconds[stage.stageName] = value
            }
            if (seconds.isEmpty()) {
                val inBed = bySeconds[HealthSleepStage.InBed.stageName]
                if (inBed != null && inBed > 0) seconds[HealthSleepStage.InBed.stageName] = inBed
            }
            return seconds
        }

        fun stages(row: HealthPeriodView): List<HealthSleepStage> {
            val seconds = stageSeconds(row)
            return (shownStages + HealthSleepStage.InBed).filter { stage -> seconds[stage.stageName] != null }
        }

        private fun numbers(rows: List<HealthPeriodView>, summaryKind: HealthSummaryKind, fromDay: String, toDay: String, text: HealthText): HealthNumbers {
            if (rows.isEmpty()) return HealthNumbers()
            if (summaryKind == HealthSummaryKind.Average) {
                val readingCount = rows.sumOf { row -> row.sampleCount }
                val weightedSum = rows.sumOf { row -> (row.averageValue ?: 0.0) * row.sampleCount }
                return HealthNumbers(
                    averageValue = if (readingCount > 0) weightedSum / readingCount else null, lowestValue = rows.mapNotNull { row -> row.minimumValue }.minOrNull(),
                    highestValue = rows.mapNotNull { row -> row.maximumValue }.maxOrNull(),
                )
            }
            val values = rows.map { row -> HealthText.summaryValue(row.sumValue, row.averageValue, row.durationSeconds, row.sampleCount, summaryKind) ?: 0.0 }
            val total = values.sum()
            var averagePerDay: Double? = null
            if (summaryKind != HealthSummaryKind.Count) {
                val dayCount = rows.sumOf { row -> HealthPlotBuilder.dayCount(row, fromDay, toDay, text) }
                averagePerDay = if (dayCount > 0) total / dayCount else null
            }
            return HealthNumbers(averagePerDayValue = averagePerDay, totalValue = total)
        }
    }
}

/** Works out the plot of each period. */
class HealthPlotBuilder(
    private val summaryKind: HealthSummaryKind,
    private val healthType: String,
    private val quantityUnit: String,
    private val period: HealthPeriod,
    private val fromDay: String,
    private val toDay: String,
    private val shape: HealthChartPlan.Shape,
    private val chartUnitSeconds: Double,
    private val text: HealthText,
) {
    private val isPerDay: Boolean get() = period != HealthPeriod.Day && (summaryKind == HealthSummaryKind.Sum || summaryKind == HealthSummaryKind.Duration)

    private fun perDay(row: HealthPeriodView, value: Double): Double =
        if (!isPerDay) value else value / dayCount(row, fromDay, toDay, text)

    fun plot(row: HealthPeriodView, periodKey: String): HealthPlot {
        val titleText = periodTitle(row, periodKey)
        return when (shape) {
            HealthChartPlan.Shape.Level -> levelPlot(row, periodKey, titleText)
            HealthChartPlan.Shape.Stages -> stagesPlot(row, periodKey, titleText)
            HealthChartPlan.Shape.Bars -> barsPlot(row, periodKey, titleText)
        }
    }

    private fun barsPlot(row: HealthPeriodView, periodKey: String, titleText: String): HealthPlot {
        val raw = HealthText.summaryValue(row.sumValue, row.averageValue, row.durationSeconds, row.sampleCount, summaryKind)
        val value = raw?.let { amount -> perDay(row, amount) }
        val valueText = text.amountText(value, summaryKind, healthType, quantityUnit)
        val figureLabel = if (isPerDay) text.perDayLabel else null
        return HealthPlot(periodKey, titleText, chartAmount(value), figures = listOf(ChartFigure(figureLabel, valueText)), spokenText = valueText)
    }

    private fun levelPlot(row: HealthPeriodView, periodKey: String, titleText: String): HealthPlot {
        val averageText = text.amountText(row.averageValue, summaryKind, healthType, quantityUnit)
        val figures = mutableListOf(ChartFigure(text.averageLabel, averageText))
        val lowest = row.minimumValue
        val highest = row.maximumValue
        if (lowest != null && highest != null) {
            figures.add(ChartFigure(text.lowestLabel, text.quantityText(lowest, healthType, quantityUnit)))
            figures.add(ChartFigure(text.highestLabel, text.quantityText(highest, healthType, quantityUnit)))
        }
        return HealthPlot(periodKey, titleText, row.averageValue, lowest, highest, figures, averageText)
    }

    private fun stagesPlot(row: HealthPeriodView, periodKey: String, titleText: String): HealthPlot {
        val seconds = HealthChartPlan.stageSeconds(row)
        val figures = mutableListOf<ChartFigure>()
        for (stage in HealthChartPlan.stages(row)) {
            val stageSeconds = seconds[stage.stageName] ?: continue
            figures.add(ChartFigure(text.stageName(stage), text.durationText(perDay(row, stageSeconds)), HealthChartPlan.color(stage)))
        }
        // The total is the time asleep, as the headline is: the time awake is a stage of its own and not part of it.
        var asleepSeconds = row.categorySecondsByStage[HealthSleepStage.ASLEEP_KEY] ?: row.durationSeconds ?: 0.0
        if (asleepSeconds <= 0) asleepSeconds = seconds[HealthSleepStage.InBed.stageName] ?: asleepSeconds
        val asleepText = text.durationText(perDay(row, asleepSeconds))
        figures.add(ChartFigure(text.totalTitle, asleepText))
        return HealthPlot(periodKey, titleText, perDay(row, asleepSeconds) / chartUnitSeconds, figures = figures, spokenText = asleepText)
    }

    fun segments(row: HealthPeriodView, periodKey: String): List<HealthStageSegment> {
        val seconds = HealthChartPlan.stageSeconds(row)
        return HealthChartPlan.stages(row).mapNotNull { stage ->
            val stageSeconds = seconds[stage.stageName] ?: return@mapNotNull null
            HealthStageSegment(periodKey, stage, text.stageName(stage), perDay(row, stageSeconds) / chartUnitSeconds)
        }
    }

    private fun chartAmount(value: Double?): Double? = if (value == null) null else if (summaryKind == HealthSummaryKind.Duration) value / chartUnitSeconds else value

    private fun periodTitle(row: HealthPeriodView, periodKey: String): String = when (period) {
        HealthPeriod.Day -> text.mediumDayText(periodKey)
        HealthPeriod.Week -> text.rangeText(text.mediumDayText(row.startDay), text.mediumDayText(row.endDay))
        HealthPeriod.Month -> text.monthLongText(periodKey) ?: periodKey
    }

    companion object {
        /** How many days of a period have a summary: what its amount is divided by to be read per day. */
        fun dayCount(row: HealthPeriodView, fromDay: String, toDay: String, text: HealthText): Int {
            if (row.dayCount > 0) return row.dayCount
            val startDay = row.startDay.take(10)
            val endDay = row.endDay.take(10)
            val first = if (startDay > fromDay) startDay else fromDay
            val last = if (endDay < toDay) endDay else toDay
            return text.dayCount(first, last)
        }
    }
}
