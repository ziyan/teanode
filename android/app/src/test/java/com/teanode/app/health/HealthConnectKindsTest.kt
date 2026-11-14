package com.teanode.app.health

import androidx.health.connect.client.records.CervicalMucusRecord
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
}
