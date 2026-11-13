package com.teanode.app.health

import androidx.test.core.app.ApplicationProvider
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.health.HealthPeriod
import com.teanode.kit.health.HealthSleepStage
import com.teanode.kit.health.HealthSummaryView
import com.teanode.kit.health.HealthTypeView
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.util.Locale

/** What a chart of a kind of Health data draws, worked out from the server's periods. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34], qualifiers = "en-rUS")
class HealthChartPlanTest {
    private val text = HealthText(ApplicationProvider.getApplicationContext(), Locale.US, java.time.ZoneId.of("UTC"))

    private fun type(healthType: String, kind: String, unit: String, summary: String) = TeaNodeJson.decodeFromString(
        HealthTypeView.serializer(),
        """{"healthType":"$healthType","healthCategory":"activity","typeName":"$healthType","valueKind":"$kind","quantityUnit":"$unit","summaryKind":"$summary","sampleCount":1}""",
    )

    private fun summary(healthType: String, unit: String, kind: String, periods: String) =
        TeaNodeJson.decodeFromString(HealthSummaryView.serializer(), """{"healthType":"$healthType","quantityUnit":"$unit","summaryKind":"$kind","periods":$periods}""")

    @Test
    fun stepsAreBarsWithTheirAverageAndTotal() {
        val periods = """[{"startDay":"2024-03-01","endDay":"2024-03-01","sumValue":8000,"sampleCount":4},{"startDay":"2024-03-02","endDay":"2024-03-02","sumValue":10000,"sampleCount":4}]"""
        val plan = HealthChartPlan.make(summary("stepCount", "count", "sum", periods), type("stepCount", "quantity", "count", "sum"), HealthPeriod.Day, "2024-03-01", "2024-03-03", text)
        assertEquals(HealthChartPlan.Shape.Bars, plan.shape)
        assertEquals(listOf("2024-03-01", "2024-03-02", "2024-03-03"), plan.periodKeys)
        assertEquals(2, plan.plots.size)
        assertEquals(8000.0, plan.plots[0].amountValue)
        assertEquals(9000.0, plan.numbers.averagePerDayValue)
        assertEquals(18000.0, plan.numbers.totalValue)
        assertEquals(listOf("9,000 steps", "18,000 steps"), plan.figureRows.map { row -> row.valueText })
        assertTrue(plan.hasData)
    }

    @Test
    fun aWeekIsReadPerDayByTheDaysThatHaveASummary() {
        val periods = """[{"startDay":"2024-03-04","endDay":"2024-03-10","sumValue":42000,"sampleCount":20,"dayCount":6}]"""
        val plan = HealthChartPlan.make(summary("stepCount", "count", "sum", periods), type("stepCount", "quantity", "count", "sum"), HealthPeriod.Week, "2024-03-04", "2024-03-10", text)
        assertEquals(7000.0, plan.plots[0].amountValue)
        assertEquals("Per day", plan.plots[0].figures[0].label)
    }

    @Test
    fun heartRateIsALevelWithABand() {
        val periods = """[{"startDay":"2024-03-01","endDay":"2024-03-01","averageValue":70,"minimumValue":50,"maximumValue":130,"sampleCount":10},{"startDay":"2024-03-02","endDay":"2024-03-02","averageValue":80,"minimumValue":55,"maximumValue":120,"sampleCount":30}]"""
        val plan = HealthChartPlan.make(summary("heartRate", "count/min", "average", periods), type("heartRate", "quantity", "count/min", "average"), HealthPeriod.Day, "2024-03-01", "2024-03-02", text)
        assertEquals(HealthChartPlan.Shape.Level, plan.shape)
        // Weighted by how many readings each day has: (70 x 10 + 80 x 30) / 40.
        assertEquals(77.5, plan.numbers.averageValue)
        assertEquals(50.0, plan.numbers.lowestValue)
        assertEquals(130.0, plan.numbers.highestValue)
        assertEquals(listOf("78 bpm", "50 bpm", "130 bpm"), plan.figureRows.map { row -> row.valueText })
    }

    @Test
    fun sleepIsStagesStackedAndItsTotalIsTheTimeAsleep() {
        val periods = """[{"startDay":"2024-03-01","endDay":"2024-03-01","durationSeconds":25200,"sampleCount":4,"categorySeconds":{"asleep":25200,"asleepDeep":3600,"asleepCore":14400,"asleepREM":7200,"awake":1800}}]"""
        val plan = HealthChartPlan.make(summary("sleepAnalysis", "", "duration", periods), type("sleepAnalysis", "category", "", "duration"), HealthPeriod.Day, "2024-03-01", "2024-03-01", text)
        assertEquals(HealthChartPlan.Shape.Stages, plan.shape)
        assertEquals(listOf(HealthSleepStage.AsleepDeep, HealthSleepStage.AsleepCore, HealthSleepStage.AsleepREM, HealthSleepStage.Awake), plan.drawnStages)
        assertEquals(7.0, plan.plots[0].amountValue)
        assertEquals(listOf(1.0, 4.0, 2.0, 0.5), plan.stageSegments.map { segment -> segment.hours })
        // Nights are not added: only the average per day is listed.
        assertEquals(1, plan.figureRows.size)
        assertEquals("7 h", plan.figureRows[0].valueText)
    }

    @Test
    fun aRangeWithNothingInItHasNoData() {
        val plan = HealthChartPlan.make(summary("stepCount", "count", "sum", "[]"), type("stepCount", "quantity", "count", "sum"), HealthPeriod.Day, "2024-03-01", "2024-03-03", text)
        assertFalse(plan.hasData)
    }

    @Test
    fun theAxisNamesAtMostFivePeriodsAndMonthsKeepTheirYearWhenTheyCrossOne() {
        val days = (1..20).map { day -> "2024-03-%02d".format(day) }
        val periods = days.joinToString(",", "[", "]") { day -> """{"startDay":"$day","endDay":"$day","sumValue":1,"sampleCount":1}""" }
        val plan = HealthChartPlan.make(summary("stepCount", "count", "sum", periods), type("stepCount", "quantity", "count", "sum"), HealthPeriod.Day, "2024-03-01", "2024-03-20", text)
        assertEquals(5, plan.labelledKeys.size)
        assertEquals("2024-03-01", plan.labelledKeys.first())
        assertEquals("2024-03-20", plan.labelledKeys.last())
        val months = HealthChartPlan.make(
            summary("stepCount", "count", "sum", """[{"startDay":"2023-12-01","endDay":"2023-12-31","sumValue":1,"sampleCount":1},{"startDay":"2024-01-01","endDay":"2024-01-31","sumValue":1,"sampleCount":1}]"""),
            type("stepCount", "quantity", "count", "sum"), HealthPeriod.Month, "2023-12-01", "2024-01-31", text,
        )
        assertEquals("Dec 2023", months.axisLabel("2023-12-01"))
    }

    @Test
    fun ranges() {
        assertEquals(HealthPeriod.Day, HealthRange.ThirtyDays.defaultPeriod)
        assertEquals(HealthPeriod.Week, HealthRange.Year.defaultPeriod)
        assertEquals(HealthPeriod.Month, HealthRange.All.defaultPeriod)
    }
}
