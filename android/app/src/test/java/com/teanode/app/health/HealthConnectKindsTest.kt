package com.teanode.app.health

import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.CervicalMucusRecord
import androidx.health.connect.client.records.CyclingPedalingCadenceRecord
import androidx.health.connect.client.records.DistanceRecord
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.records.PowerRecord
import androidx.health.connect.client.records.SpeedRecord
import androidx.health.connect.client.records.TotalCaloriesBurnedRecord
import androidx.health.connect.client.units.Length
import androidx.health.connect.client.units.Power
import androidx.health.connect.client.units.Velocity
import com.teanode.kit.health.HealthSport
import androidx.health.connect.client.records.MenstruationFlowRecord
import androidx.health.connect.client.records.NutritionRecord
import androidx.health.connect.client.records.OvulationTestRecord
import androidx.health.connect.client.records.BloodPressureRecord
import androidx.health.connect.client.records.metadata.Metadata
import androidx.health.connect.client.units.Energy
import androidx.health.connect.client.units.Mass
import androidx.health.connect.client.units.Pressure
import com.teanode.app.health.sync.HealthConnectKinds
import com.teanode.kit.health.HealthTypes
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant
import java.time.ZoneOffset

/** How Health Connect's records become the readings the server keeps under HealthKit's type names. */
class HealthConnectKindsTest {
    private val startedAt = Instant.parse("2026-03-04T12:00:00Z")
    private val endedAt = Instant.parse("2026-03-04T12:30:00Z")

    private fun kindFor(recordClass: kotlin.reflect.KClass<*>) = HealthConnectKinds.all.first { kind -> kind.recordClass == recordClass }

    @Test
    fun `a meal gives a reading for each nutrient it names, in the kit's units`() {
        val meal = NutritionRecord(
            startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC,
            metadata = Metadata.manualEntryWithId("meal1"), energy = Energy.kilocalories(520.0), protein = Mass.grams(31.0),
            sodium = Mass.milligrams(800.0), vitaminB12 = Mass.micrograms(2.4), caffeine = null,
        )
        val readings = kindFor(NutritionRecord::class).samples(meal)
        assertEquals(setOf("dietaryEnergyConsumed", "dietaryProtein", "dietarySodium", "dietaryVitaminB12"), readings.map { reading -> reading.healthType }.toSet())
        val sodium = readings.first { reading -> reading.healthType == "dietarySodium" }
        assertEquals(800.0, sodium.quantityValue!!, 0.0001)
        assertEquals("mg", sodium.quantityUnit)
        assertEquals(2.4, readings.first { reading -> reading.healthType == "dietaryVitaminB12" }.quantityValue!!, 0.0001)
        assertEquals(readings.size, readings.map { reading -> reading.healthSampleId }.toSet().size)
    }

    @Test
    fun `every type a kind gives is in the kit's table, with the unit the reading carries`() {
        for (kind in HealthConnectKinds.all) for (healthType in kind.healthTypes) assertTrue("$healthType is not in the table", HealthTypes.info(healthType) != null)
        val meal = NutritionRecord(
            startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC, metadata = Metadata.manualEntryWithId("meal2"),
            energy = Energy.kilocalories(1.0), totalCarbohydrate = Mass.grams(1.0), dietaryFiber = Mass.grams(1.0), sugar = Mass.grams(1.0), totalFat = Mass.grams(1.0),
            monounsaturatedFat = Mass.grams(1.0), polyunsaturatedFat = Mass.grams(1.0), saturatedFat = Mass.grams(1.0), cholesterol = Mass.grams(0.001), protein = Mass.grams(1.0),
            vitaminA = Mass.grams(0.000001), thiamin = Mass.grams(0.001), riboflavin = Mass.grams(0.001), niacin = Mass.grams(0.001), pantothenicAcid = Mass.grams(0.001),
            vitaminB6 = Mass.grams(0.001), biotin = Mass.grams(0.000001), vitaminB12 = Mass.grams(0.000001), vitaminC = Mass.grams(0.001), vitaminD = Mass.grams(0.000001),
            vitaminE = Mass.grams(0.001), vitaminK = Mass.grams(0.000001), folate = Mass.grams(0.000001), calcium = Mass.grams(0.001), chloride = Mass.grams(0.001),
            iron = Mass.grams(0.001), magnesium = Mass.grams(0.001), phosphorus = Mass.grams(0.001), potassium = Mass.grams(0.001), sodium = Mass.grams(0.001),
            zinc = Mass.grams(0.001), chromium = Mass.grams(0.000001), copper = Mass.grams(0.001), iodine = Mass.grams(0.000001), manganese = Mass.grams(0.001),
            molybdenum = Mass.grams(0.000001), selenium = Mass.grams(0.000001), caffeine = Mass.grams(0.001),
        )
        val readings = kindFor(NutritionRecord::class).samples(meal)
        assertEquals(kindFor(NutritionRecord::class).healthTypes.toSet(), readings.map { reading -> reading.healthType }.toSet())
        for (reading in readings) {
            assertEquals(reading.healthType, HealthTypes.info(reading.healthType)!!.quantityUnit, reading.quantityUnit)
            assertEquals("${reading.healthType} is one in the unit of the table", 1.0, reading.quantityValue!!, 0.0001)
        }
    }

    @Test
    fun `blood pressure gives both pressures from one record`() {
        val record = BloodPressureRecord(
            time = startedAt, zoneOffset = ZoneOffset.UTC, metadata = Metadata.manualEntryWithId("pressure1"),
            systolic = Pressure.millimetersOfMercury(120.0), diastolic = Pressure.millimetersOfMercury(80.0),
        )
        val readings = kindFor(BloodPressureRecord::class).samples(record)
        assertEquals(listOf("bloodPressureSystolic", "bloodPressureDiastolic"), readings.map { reading -> reading.healthType })
        assertEquals("bloodPressureSystolic", kindFor(BloodPressureRecord::class).healthType)
    }

    @Test
    fun `cycle records become HealthKit's category values`() {
        val flow = MenstruationFlowRecord(time = startedAt, zoneOffset = ZoneOffset.UTC, flow = MenstruationFlowRecord.FLOW_HEAVY, metadata = Metadata.manualEntryWithId("flow1"))
        assertEquals(4, kindFor(MenstruationFlowRecord::class).samples(flow).single().categoryValue)
        assertEquals(1, HealthConnectKinds.menstrualFlowValue(MenstruationFlowRecord.FLOW_UNKNOWN))
        assertEquals(2, HealthConnectKinds.ovulationTestResult(OvulationTestRecord.RESULT_POSITIVE))
        assertEquals(1, HealthConnectKinds.ovulationTestResult(OvulationTestRecord.RESULT_NEGATIVE))
        assertEquals(5, HealthConnectKinds.cervicalMucusQuality(CervicalMucusRecord.APPEARANCE_EGG_WHITE))
        assertNull(HealthConnectKinds.cervicalMucusQuality(CervicalMucusRecord.APPEARANCE_UNUSUAL))
        val unusual = CervicalMucusRecord(time = startedAt, zoneOffset = ZoneOffset.UTC, appearance = CervicalMucusRecord.APPEARANCE_UNUSUAL, sensation = 0, metadata = Metadata.manualEntryWithId("mucus1"))
        assertTrue(kindFor(CervicalMucusRecord::class).samples(unusual).isEmpty())
    }

    private fun session(exerciseType: Int, id: String = "session1") = ExerciseSessionRecord(
        startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC,
        metadata = Metadata.manualEntryWithId(id), exerciseType = exerciseType,
    )

    private fun distance(startOffsetMinutes: Long = 5, endOffsetMinutes: Long = 10) = DistanceRecord(
        startTime = startedAt.plusSeconds(startOffsetMinutes * 60), startZoneOffset = ZoneOffset.UTC, endTime = startedAt.plusSeconds(endOffsetMinutes * 60), endZoneOffset = ZoneOffset.UTC,
        metadata = Metadata.manualEntryWithId("distance1"), distance = Length.meters(1500.0),
    )

    @Test
    fun `a distance is kept under the sport of the session it sits in, and as walking and running with none`() {
        val kind = kindFor(DistanceRecord::class)
        assertEquals("distanceWalkingRunning", kind.samples(distance()).single().healthType)
        assertEquals("distanceCycling", kind.samples(distance(), listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_BIKING))).single().healthType)
        assertEquals("distanceSwimming", kind.samples(distance(), listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_POOL))).single().healthType)
        assertEquals("distanceWalkingRunning", kind.samples(distance(), listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_RUNNING))).single().healthType)
        assertEquals("distanceWalkingRunning", kind.samples(distance(), listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_YOGA))).single().healthType)
        val afterSession = distance(startOffsetMinutes = 40, endOffsetMinutes = 45)
        assertEquals("distanceWalkingRunning", kind.samples(afterSession, listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_BIKING))).single().healthType)
        assertEquals(1500.0, kind.samples(distance()).single().quantityValue!!, 0.0001)
    }

    @Test
    fun `a speed is the sport's speed inside a session and left out without one`() {
        val record = SpeedRecord(
            startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC, metadata = Metadata.manualEntryWithId("speed1"),
            samples = listOf(SpeedRecord.Sample(startedAt.plusSeconds(60), Velocity.metersPerSecond(3.5)), SpeedRecord.Sample(startedAt.plusSeconds(120), Velocity.metersPerSecond(3.9))),
        )
        val kind = kindFor(SpeedRecord::class)
        assertTrue(kind.samples(record).isEmpty())
        assertTrue(kind.samples(record, listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_YOGA))).isEmpty())
        val running = kind.samples(record, listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_RUNNING)))
        assertEquals(listOf("runningSpeed", "runningSpeed"), running.map { reading -> reading.healthType })
        assertEquals(listOf(3.5, 3.9), running.map { reading -> reading.quantityValue })
        assertEquals("m/s", running.first().quantityUnit)
        assertEquals(running.size, running.map { reading -> reading.healthSampleId }.toSet().size)
        assertEquals("cyclingSpeed", kind.samples(record, listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_BIKING))).first().healthType)
    }

    @Test
    fun `power is the sport's power inside a session and cadence is cycling cadence`() {
        val power = PowerRecord(
            startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC, metadata = Metadata.manualEntryWithId("power1"),
            samples = listOf(PowerRecord.Sample(startedAt.plusSeconds(60), Power.watts(210.0))),
        )
        val powerKind = kindFor(PowerRecord::class)
        assertTrue(powerKind.samples(power).isEmpty())
        val cycling = powerKind.samples(power, listOf(session(ExerciseSessionRecord.EXERCISE_TYPE_BIKING))).single()
        assertEquals("cyclingPower", cycling.healthType)
        assertEquals(210.0, cycling.quantityValue!!, 0.0001)
        assertEquals("W", cycling.quantityUnit)
        val cadence = CyclingPedalingCadenceRecord(
            startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC, metadata = Metadata.manualEntryWithId("cadence1"),
            samples = listOf(CyclingPedalingCadenceRecord.Sample(startedAt.plusSeconds(60), 88.0)),
        )
        val reading = kindFor(CyclingPedalingCadenceRecord::class).samples(cadence).single()
        assertEquals("cyclingCadence", reading.healthType)
        assertEquals(88.0, reading.quantityValue!!, 0.0001)
        assertEquals("count/min", reading.quantityUnit)
    }

    @Test
    fun `resting energy is the total less the active energy and nothing when none is left`() {
        val total = TotalCaloriesBurnedRecord(
            startTime = startedAt, startZoneOffset = ZoneOffset.UTC, endTime = endedAt, endZoneOffset = ZoneOffset.UTC,
            metadata = Metadata.manualEntryWithId("total1"), energy = Energy.kilocalories(180.0),
        )
        val resting = HealthConnectKinds.restingEnergySample(total, 60.0).single()
        assertEquals("basalEnergyBurned", resting.healthType)
        assertEquals(120.0, resting.quantityValue!!, 0.0001)
        assertEquals("kcal", resting.quantityUnit)
        assertTrue(HealthConnectKinds.restingEnergySample(total, 180.0).isEmpty())
        assertTrue(HealthConnectKinds.restingEnergySample(total, 200.0).isEmpty())
    }

    @Test
    fun `an exercise type names the sport HealthKit has a type for`() {
        assertEquals(HealthSport.Cycling, HealthConnectKinds.sportOf(ExerciseSessionRecord.EXERCISE_TYPE_BIKING_STATIONARY))
        assertEquals(HealthSport.Running, HealthConnectKinds.sportOf(ExerciseSessionRecord.EXERCISE_TYPE_RUNNING_TREADMILL))
        assertEquals(HealthSport.Walking, HealthConnectKinds.sportOf(ExerciseSessionRecord.EXERCISE_TYPE_HIKING))
        assertEquals(HealthSport.Other, HealthConnectKinds.sportOf(ExerciseSessionRecord.EXERCISE_TYPE_SKIING))
        assertEquals(HealthSport.Other, HealthConnectKinds.sportOf(ExerciseSessionRecord.EXERCISE_TYPE_YOGA))
    }

    @Test
    fun `every permission the kinds ask for is declared in the manifest`() {
        val manifest = java.io.File("src/main/AndroidManifest.xml").readText()
        for (kind in HealthConnectKinds.all) {
            val permission = HealthPermission.getReadPermission(kind.recordClass)
            assertTrue("$permission is not declared for ${kind.healthType}", manifest.contains("\"$permission\""))
        }
        assertTrue(manifest.contains("android.permission.health.READ_EXERCISE"))
    }
}
