package com.teanode.kit.health

import com.teanode.kit.location.instantOf
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.ZoneId
import java.util.Locale

/** The rules of the table at the top of internal/health/words.go, which the apps copy: the same examples the server's tests use. All the data is invented. */
class HealthValueWordsTest {
    private val english = Locale.US

    private fun words(healthType: String, value: Double, quantityUnit: String) = HealthValueWords.englishText(healthType, value, quantityUnit)

    @Test
    fun valuesAreWrittenAsTheTableSays() {
        val cases = listOf(
            listOf("stepCount", 8412.4, "count", "8,412 steps"), listOf("stepCount", 1.0, "count", "1 step"), listOf("flightsClimbed", 12.0, "count", "12 flights"),
            listOf("bodyMassIndex", 22.456, "count", "22.5"), listOf("bodyMassIndex", 22.04, "count", "22"), listOf("uvExposure", 3.26, "count", "3.3"),
            listOf("appleSleepingBreathingDisturbances", 12.34, "count", "12.3"), listOf("nikeFuel", 1234.5, "count", "1,234.5"),
            listOf("stepCount", 0.0, "count", "0 steps"), listOf("stepCount", 1.4, "count", "1 step"), listOf("stepCount", 2.0, "count", "2 steps"),
            listOf("flightsClimbed", 1.0, "count", "1 flight"), listOf("pushCount", 1.0, "count", "1 push"), listOf("pushCount", 3.0, "count", "3 pushes"),
            listOf("swimmingStrokeCount", 1.0, "count", "1 stroke"), listOf("numberOfTimesFallen", 1.0, "count", "1 fall"),
            listOf("inhalerUsage", 1.0, "count", "1 puff"), listOf("numberOfAlcoholicBeverages", 1.0, "count", "1 drink"),
            listOf("numberOfAlcoholicBeverages", 2.0, "count", "2 drinks"), listOf("heartRateRecoveryOneMinute", 23.4, "count/min", "23 bpm"),
            listOf("walkingHeartRateAverage", 101.5, "count/min", "102 bpm"), listOf("bodyTemperature", 98.64, "degF", "98.64°F"),
            listOf("bodyMass", 150.26, "lb", "150.3 lb"), listOf("bodyMass", 1500.4, "lb", "1,500 lb"), listOf("bodyMass", 0.35, "kg", "0.35 kg"),
            listOf("heartRate", 74.3, "count/min", "74 bpm"), listOf("restingHeartRate", 55.5, "count/min", "56 bpm"),
            listOf("respiratoryRate", 15.6, "count/min", "16 breaths/min"), listOf("cyclingCadence", 88.2, "count/min", "88 rpm"),
            listOf("distanceWalkingRunning", 850.0, "m", "850 m"), listOf("distanceWalkingRunning", 999.6, "m", "999.6 m"),
            listOf("distanceWalkingRunning", 1000.0, "m", "1 km"), listOf("distanceWalkingRunning", 5234.0, "m", "5.2 km"),
            listOf("distanceWalkingRunning", 12980.0, "m", "13 km"), listOf("distanceWalkingRunning", 1_234_567.0, "m", "1,234.6 km"),
            listOf("runningStrideLength", 0.93, "m", "0.93 m"), listOf("bodyTemperature", 36.62, "degC", "36.62°C"),
            listOf("vo2Max", 42.3, "ml/(kg*min)", "42.3 mL/kg·min"), listOf("physicalEffort", 4.25, "kcal/(kg*hr)", "4.25 kcal/kg·hr"),
            listOf("activeEnergyBurned", 512.7, "kcal", "512.7 kcal"), listOf("oxygenSaturation", 0.975, "%", "98%"),
            listOf("bodyMass", 72.456, "kg", "72.46 kg"), listOf("environmentalAudioExposure", 71.2, "dBASPL", "71.2 dB"),
            listOf("workoutEffortScore", 6.5, "appleEffortScore", "6.5"), listOf("bodyMass", 72.0, "", "72"), listOf("bodyMass", -0.001, "kg", "0 kg"),
            // Percentages: whole numbers for blood oxygen and walking steadiness, one decimal for body fat, walking asymmetry and double support, the usual precision for the rest.
            listOf("oxygenSaturation", 0.9797, "%", "98%"), listOf("oxygenSaturation", 0.94, "%", "94%"), listOf("oxygenSaturation", 1.0, "%", "100%"),
            listOf("appleWalkingSteadiness", 0.674, "%", "67%"), listOf("bodyFatPercentage", 0.1837, "%", "18.4%"),
            listOf("bodyFatPercentage", 0.18, "%", "18%"), listOf("walkingAsymmetryPercentage", 0.0345, "%", "3.5%"),
            listOf("walkingDoubleSupportPercentage", 0.2849, "%", "28.5%"), listOf("peripheralPerfusionIndex", 0.0235, "%", "2.35%"),
            listOf("bloodAlcoholContent", 0.0008, "%", "0.08%"), listOf("bodyFatPercentage", 0.1845, "%", "18.5%"),
            // The tie rule: half away from zero on the shortest decimal form.
            listOf("bodyMassIndex", 22.25, "", "22.25"), listOf("x", 0.355, "", "0.36"), listOf("x", 1.005, "", "1.01"), listOf("x", 0.975, "", "0.98"),
            listOf("x", 150.25, "", "150.3"), listOf("x", 1000.5, "", "1,001"), listOf("x", -150.25, "", "-150.3"), listOf("x", -0.001, "", "0"),
            listOf("x", -0.004, "kg", "0 kg"), listOf("heartRate", 72.5, "count/min", "73 bpm"), listOf("heartRate", -0.5, "count/min", "-1 bpm"),
            listOf("stepCount", 2.5, "count", "3 steps"), listOf("stepCount", 0.5, "count", "1 step"),
            listOf("stepCount", 999_999.5, "count", "1,000,000 steps"), listOf("distanceWalkingRunning", 1050.0, "m", "1.1 km"),
        )
        for (case in cases) {
            assertEquals(case[3], words(case[0] as String, case[1] as Double, case[2] as String), "${case[0]} ${case[1]} ${case[2]}")
        }
    }

    @Test
    fun thePartsSayWhichWordFollowsAndWhetherItIsSingular() {
        assertEquals(HealthValueParts("1", HealthUnitWord.Noun(HealthCountNoun.Step, true)), HealthValueWords.parts("stepCount", 1.0, "count", english))
        assertEquals(HealthValueParts("8,412", HealthUnitWord.Noun(HealthCountNoun.Step, false)), HealthValueWords.parts("stepCount", 8412.0, "count", english))
        assertEquals(HealthValueParts("98", HealthUnitWord.Touching("%")), HealthValueWords.parts("oxygenSaturation", 0.975, "%", english))
        assertEquals(HealthValueParts("18.4", HealthUnitWord.Touching("%")), HealthValueWords.parts("bodyFatPercentage", 0.1837, "%", english))
        assertEquals(HealthValueParts("74", HealthUnitWord.Spaced("bpm")), HealthValueWords.parts("heartRate", 74.0, "count/min", english))
        assertEquals(HealthValueParts("22.5", HealthUnitWord.None), HealthValueWords.parts("bodyMassIndex", 22.5, "count", english))
        assertEquals(HealthValueParts("6.5", HealthUnitWord.None), HealthValueWords.parts("workoutEffortScore", 6.5, "appleEffortScore", english))
        assertEquals(HealthValueParts("36.6", HealthUnitWord.Touching("°C")), HealthValueWords.parts("bodyTemperature", 36.6, "degC", english))
    }

    @Test
    fun everyCountNounIsUsedByAKindThatCountsIt() {
        assertEquals(HealthCountNoun.entries.toSet(), HealthValueWords.countNouns.values.toSet())
        for (healthType in HealthValueWords.countNouns.keys) assertEquals("count", HealthTypes.info(healthType)?.quantityUnit, healthType)
    }

    @Test
    fun theSizeOfANumberDecidesItsDecimals() {
        assertEquals(0, HealthValueWords.precisionDigits(1000.0))
        assertEquals(0, HealthValueWords.precisionDigits(-1000.0))
        assertEquals(1, HealthValueWords.precisionDigits(999.9))
        assertEquals(1, HealthValueWords.precisionDigits(100.0))
        assertEquals(2, HealthValueWords.precisionDigits(99.9))
        assertEquals("8,412", HealthValueWords.numberText(8412.4, english))
        assertEquals("150.3", HealthValueWords.numberText(150.26, english))
        assertEquals("72.4", HealthValueWords.numberText(72.4, english))
        assertEquals("0.35", HealthValueWords.numberText(0.35, english))
    }

    @Test
    fun timesAreHoursAndMinutes() {
        val cases = listOf(
            0.0 to HealthDurationParts.Seconds(0), 45.0 to HealthDurationParts.Seconds(45), 59.4 to HealthDurationParts.Seconds(59),
            60.0 to HealthDurationParts.Minutes(1), 2100.0 to HealthDurationParts.Minutes(35), 3600.0 to HealthDurationParts.Hours(1),
            25920.0 to HealthDurationParts.HoursMinutes(7, 12), 25950.0 to HealthDurationParts.HoursMinutes(7, 13), 3599.0 to HealthDurationParts.Hours(1),
            637_800.0 to HealthDurationParts.HoursMinutes(177, 10),
        )
        for ((seconds, expected) in cases) assertEquals(expected, HealthValueWords.durationParts(seconds), "$seconds")
    }

    @Test
    fun anAgeCountsWholeYearsAndIsNullForADayThatIsNotThereYet() {
        val utc = ZoneId.of("UTC")
        val today = instantOf("2026-03-02T15:00:00Z")
        assertEquals(36, HealthValueWords.ageInYears("1990-03-02", today, utc))
        assertEquals(35, HealthValueWords.ageInYears("1990-03-03", today, utc))
        assertEquals(1, HealthValueWords.ageInYears("2025-03-02", today, utc))
        assertEquals(0, HealthValueWords.ageInYears("2026-03-02", today, utc))
        assertNull(HealthValueWords.ageInYears("2030-01-01", today, utc))
        assertNull(HealthValueWords.ageInYears("not a date", today, utc))
        assertNull(HealthValueWords.ageInYears("1990-02-30", today, utc))
    }

    @Test
    fun bloodTypesAreWrittenAsOnACard() {
        val cases = mapOf(
            "aPositive" to "A+", "aNegative" to "A-", "bPositive" to "B+", "bNegative" to "B-", "abPositive" to "AB+", "abNegative" to "AB-",
            "oPositive" to "O+", "oNegative" to "O-", "notSet" to "notSet", "something" to "something",
        )
        for ((sent, expected) in cases) assertEquals(expected, HealthValueWords.bloodTypeText(sent), sent)
    }

    @Test
    fun theReadingsOfADayAreCountedAndAveragedByTheDayEachBegan() {
        val tokyo = ZoneId.of("Asia/Tokyo")
        fun reading(text: String, value: Double) = HealthReading(instantOf(text), value)
        val readings = listOf(
            reading("2024-03-01T01:00:00Z", 60.0), reading("2024-03-01T05:00:00Z", 80.0), reading("2024-03-01T05:30:00Z", 70.0),
            // 00:30 on the 2nd in Tokyo.
            reading("2024-03-01T15:30:00Z", 90.0), reading("2024-03-01T16:00:00Z", Double.NaN),
        )
        val statistics = HealthDaySummaries.readingStatistics(readings, tokyo)
        assertEquals(setOf("2024-03-01", "2024-03-02"), statistics.keys)
        assertEquals(3, statistics["2024-03-01"]?.sampleCount)
        assertEquals(70.0, statistics["2024-03-01"]?.averageValue)
        assertEquals(60.0, statistics["2024-03-01"]?.minimumValue)
        assertEquals(80.0, statistics["2024-03-01"]?.maximumValue)
        assertNull(statistics["2024-03-01"]?.sumValue)
        assertEquals(1, statistics["2024-03-02"]?.sampleCount)
        assertEquals(listOf("2024-03-02"), HealthDaySummaries.readingStatistics(readings, tokyo, setOf("2024-03-02")).keys.sorted())
        assertTrue(HealthDaySummaries.readingStatistics(emptyList(), tokyo).isEmpty())
        // The summary of a day made from them is an average with its range.
        val summary = HealthDaySummaries.quantitySummary("environmentalAudioExposure", "2024-03-01", statistics.getValue("2024-03-01"), "dBASPL")
        assertEquals(70.0, summary.averageValue)
        assertEquals(3, summary.sampleCount)
    }
}
