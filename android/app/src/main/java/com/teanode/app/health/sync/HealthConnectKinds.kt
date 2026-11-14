package com.teanode.app.health.sync

import androidx.health.connect.client.records.ActiveCaloriesBurnedRecord
import androidx.health.connect.client.records.BasalBodyTemperatureRecord
import androidx.health.connect.client.records.CervicalMucusRecord
import androidx.health.connect.client.records.IntermenstrualBleedingRecord
import androidx.health.connect.client.records.LeanBodyMassRecord
import androidx.health.connect.client.records.MenstruationFlowRecord
import androidx.health.connect.client.records.NutritionRecord
import androidx.health.connect.client.records.OvulationTestRecord
import androidx.health.connect.client.records.SexualActivityRecord
import androidx.health.connect.client.records.WheelchairPushesRecord
import androidx.health.connect.client.units.Mass
import androidx.health.connect.client.records.BloodGlucoseRecord
import androidx.health.connect.client.records.BloodPressureRecord
import androidx.health.connect.client.records.BodyFatRecord
import androidx.health.connect.client.records.BodyTemperatureRecord
import androidx.health.connect.client.records.DistanceRecord
import androidx.health.connect.client.records.ExerciseSessionRecord
import androidx.health.connect.client.records.FloorsClimbedRecord
import androidx.health.connect.client.records.HeartRateRecord
import androidx.health.connect.client.records.HeightRecord
import androidx.health.connect.client.records.HydrationRecord
import androidx.health.connect.client.records.OxygenSaturationRecord
import androidx.health.connect.client.records.Record
import androidx.health.connect.client.records.RespiratoryRateRecord
import androidx.health.connect.client.records.RestingHeartRateRecord
import androidx.health.connect.client.records.SleepSessionRecord
import androidx.health.connect.client.records.StepsRecord
import androidx.health.connect.client.records.Vo2MaxRecord
import androidx.health.connect.client.records.WeightRecord
import com.teanode.kit.health.HealthSampleInput
import com.teanode.kit.health.HealthSleepStage
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import kotlin.reflect.KClass

/**
 * HealthConnectKind is one kind of Health Connect record, the HealthKit type name it is sent under (so one person's iPhone
 * and Android phone add up on one server), and how a record becomes readings. A record that holds a series (heart rate,
 * sleep stages) becomes one reading for each point, named by the record's id and the point's place.
 */
class HealthConnectKind(val healthTypes: List<String>, val recordClass: KClass<out Record>, val samples: (Record) -> List<HealthSampleInput>) {
    constructor(healthType: String, recordClass: KClass<out Record>, samples: (Record) -> List<HealthSampleInput>) : this(listOf(healthType), recordClass, samples)

    /** The name the changes token is kept under: the first type, so a token kept before a record gave several types still holds. */
    val healthType: String get() = healthTypes.first()
}

object HealthConnectKinds {
    private const val ANDROID_SOURCE = "Health Connect"

    private fun sourceOf(record: Record): String = record.metadata.dataOrigin.packageName

    private fun quantity(record: Record, healthType: String, unit: String, startedAt: Instant, endedAt: Instant, value: Double, id: String = record.metadata.id): HealthSampleInput =
        HealthSampleInput(
            healthSampleId = id, healthType = healthType, startedAt = startedAt, endedAt = endedAt, quantityValue = value, quantityUnit = unit,
            sourceName = ANDROID_SOURCE, sourceBundleId = sourceOf(record), deviceName = record.metadata.device?.model.orEmpty(),
        )

    @Suppress("UNCHECKED_CAST")
    private fun <Typed : Record> single(healthType: String, unit: String, recordClass: KClass<Typed>, read: (Typed) -> Triple<Instant, Instant, Double>): HealthConnectKind =
        HealthConnectKind(healthType, recordClass) { record ->
            val (startedAt, endedAt, value) = read(record as Typed)
            listOf(quantity(record, healthType, unit, startedAt, endedAt, value))
        }

    private fun interval(startedAt: Instant, endedAt: Instant, value: Double) = Triple(startedAt, endedAt, value)
    private fun instant(at: Instant, value: Double) = Triple(at, at, value)

    /** What HealthKit calls a sleep stage by the raw value: Health Connect's light sleep is HealthKit's core. */
    private fun sleepStage(stage: Int): HealthSleepStage? = when (stage) {
        SleepSessionRecord.STAGE_TYPE_SLEEPING -> HealthSleepStage.AsleepUnspecified
        SleepSessionRecord.STAGE_TYPE_LIGHT -> HealthSleepStage.AsleepCore
        SleepSessionRecord.STAGE_TYPE_DEEP -> HealthSleepStage.AsleepDeep
        SleepSessionRecord.STAGE_TYPE_REM -> HealthSleepStage.AsleepREM
        SleepSessionRecord.STAGE_TYPE_AWAKE, SleepSessionRecord.STAGE_TYPE_AWAKE_IN_BED, SleepSessionRecord.STAGE_TYPE_OUT_OF_BED -> HealthSleepStage.Awake
        else -> null
    }

    /** HealthKit's "not applicable" category value, which the types that only mark that something happened use. */
    private const val NOT_APPLICABLE = 0

    private fun <Typed : Record> category(healthType: String, recordClass: KClass<Typed>, timeOf: (Typed) -> Instant, valueOf: (Typed) -> Int?): HealthConnectKind =
        HealthConnectKind(healthType, recordClass) { record ->
            @Suppress("UNCHECKED_CAST") val typed = record as Typed
            val categoryValue = valueOf(typed)
            if (categoryValue == null) emptyList() else {
                val at = timeOf(typed)
                listOf(HealthSampleInput(
                    healthSampleId = record.metadata.id, healthType = healthType, startedAt = at, endedAt = at, categoryValue = categoryValue,
                    sourceName = ANDROID_SOURCE, sourceBundleId = sourceOf(record), deviceName = record.metadata.device?.model.orEmpty(),
                ))
            }
        }

    /** HealthKit's vaginal bleeding values: unspecified 1, light 2, medium 3, heavy 4. */
    fun menstrualFlowValue(flow: Int): Int = when (flow) {
        MenstruationFlowRecord.FLOW_LIGHT -> 2
        MenstruationFlowRecord.FLOW_MEDIUM -> 3
        MenstruationFlowRecord.FLOW_HEAVY -> 4
        else -> 1
    }

    private val menstrualFlow = category("menstrualFlow", MenstruationFlowRecord::class, { record -> record.time }) { record -> menstrualFlowValue(record.flow) }

    /** HealthKit's ovulation test values: negative 1, luteinizing hormone surge 2, indeterminate 3, estrogen surge 4. */
    fun ovulationTestResult(result: Int): Int? = when (result) {
        OvulationTestRecord.RESULT_NEGATIVE -> 1
        OvulationTestRecord.RESULT_POSITIVE -> 2
        OvulationTestRecord.RESULT_INCONCLUSIVE -> 3
        OvulationTestRecord.RESULT_HIGH -> 4
        else -> null
    }

    /** HealthKit's cervical mucus values (dry 1 to egg white 5) are Health Connect's appearance values; unusual and unknown have no match. */
    fun cervicalMucusQuality(appearance: Int): Int? = when (appearance) {
        CervicalMucusRecord.APPEARANCE_DRY -> 1
        CervicalMucusRecord.APPEARANCE_STICKY -> 2
        CervicalMucusRecord.APPEARANCE_CREAMY -> 3
        CervicalMucusRecord.APPEARANCE_WATERY -> 4
        CervicalMucusRecord.APPEARANCE_EGG_WHITE -> 5
        else -> null
    }

    private enum class MassUnit(val unit: String, val amountOf: (Mass) -> Double) {
        Grams("g", { mass -> mass.inGrams }),
        Milligrams("mg", { mass -> mass.inMilligrams }),
        Micrograms("mcg", { mass -> mass.inMicrograms }),
    }

    private class Nutrient(val healthType: String, val massUnit: MassUnit, val massOf: (NutritionRecord) -> Mass?)

    // The units are the kit's table's, so a reading and its day's summary agree.
    private val nutrients: List<Nutrient> = listOf(
        Nutrient("dietaryCarbohydrates", MassUnit.Grams) { record -> record.totalCarbohydrate },
        Nutrient("dietaryFiber", MassUnit.Grams) { record -> record.dietaryFiber },
        Nutrient("dietarySugar", MassUnit.Grams) { record -> record.sugar },
        Nutrient("dietaryFatTotal", MassUnit.Grams) { record -> record.totalFat },
        Nutrient("dietaryFatMonounsaturated", MassUnit.Grams) { record -> record.monounsaturatedFat },
        Nutrient("dietaryFatPolyunsaturated", MassUnit.Grams) { record -> record.polyunsaturatedFat },
        Nutrient("dietaryFatSaturated", MassUnit.Grams) { record -> record.saturatedFat },
        Nutrient("dietaryCholesterol", MassUnit.Milligrams) { record -> record.cholesterol },
        Nutrient("dietaryProtein", MassUnit.Grams) { record -> record.protein },
        Nutrient("dietaryVitaminA", MassUnit.Micrograms) { record -> record.vitaminA },
        Nutrient("dietaryThiamin", MassUnit.Milligrams) { record -> record.thiamin },
        Nutrient("dietaryRiboflavin", MassUnit.Milligrams) { record -> record.riboflavin },
        Nutrient("dietaryNiacin", MassUnit.Milligrams) { record -> record.niacin },
        Nutrient("dietaryPantothenicAcid", MassUnit.Milligrams) { record -> record.pantothenicAcid },
        Nutrient("dietaryVitaminB6", MassUnit.Milligrams) { record -> record.vitaminB6 },
        Nutrient("dietaryBiotin", MassUnit.Micrograms) { record -> record.biotin },
        Nutrient("dietaryVitaminB12", MassUnit.Micrograms) { record -> record.vitaminB12 },
        Nutrient("dietaryVitaminC", MassUnit.Milligrams) { record -> record.vitaminC },
        Nutrient("dietaryVitaminD", MassUnit.Micrograms) { record -> record.vitaminD },
        Nutrient("dietaryVitaminE", MassUnit.Milligrams) { record -> record.vitaminE },
        Nutrient("dietaryVitaminK", MassUnit.Micrograms) { record -> record.vitaminK },
        Nutrient("dietaryFolate", MassUnit.Micrograms) { record -> record.folate },
        Nutrient("dietaryCalcium", MassUnit.Milligrams) { record -> record.calcium },
        Nutrient("dietaryChloride", MassUnit.Milligrams) { record -> record.chloride },
        Nutrient("dietaryIron", MassUnit.Milligrams) { record -> record.iron },
        Nutrient("dietaryMagnesium", MassUnit.Milligrams) { record -> record.magnesium },
        Nutrient("dietaryPhosphorus", MassUnit.Milligrams) { record -> record.phosphorus },
        Nutrient("dietaryPotassium", MassUnit.Milligrams) { record -> record.potassium },
        Nutrient("dietarySodium", MassUnit.Milligrams) { record -> record.sodium },
        Nutrient("dietaryZinc", MassUnit.Milligrams) { record -> record.zinc },
        Nutrient("dietaryChromium", MassUnit.Micrograms) { record -> record.chromium },
        Nutrient("dietaryCopper", MassUnit.Milligrams) { record -> record.copper },
        Nutrient("dietaryIodine", MassUnit.Micrograms) { record -> record.iodine },
        Nutrient("dietaryManganese", MassUnit.Milligrams) { record -> record.manganese },
        Nutrient("dietaryMolybdenum", MassUnit.Micrograms) { record -> record.molybdenum },
        Nutrient("dietarySelenium", MassUnit.Micrograms) { record -> record.selenium },
        Nutrient("dietaryCaffeine", MassUnit.Milligrams) { record -> record.caffeine },
    )

    /** One meal or food becomes a reading for each nutrient it names, as HealthKit keeps one sample for each nutrient. */
    private val nutrition = HealthConnectKind(listOf("dietaryEnergyConsumed") + nutrients.map { nutrient -> nutrient.healthType }, NutritionRecord::class) { record ->
        record as NutritionRecord
        val energy = record.energy?.let { energy -> quantity(record, "dietaryEnergyConsumed", "kcal", record.startTime, record.endTime, energy.inKilocalories, "${record.metadata.id}:dietaryEnergyConsumed") }
        listOfNotNull(energy) + nutrients.mapNotNull { nutrient ->
            nutrient.massOf(record)?.let { mass ->
                quantity(record, nutrient.healthType, nutrient.massUnit.unit, record.startTime, record.endTime, nutrient.massUnit.amountOf(mass), "${record.metadata.id}:${nutrient.healthType}")
            }
        }
    }

    private val sleep = HealthConnectKind("sleepAnalysis", SleepSessionRecord::class) { record ->
        record as SleepSessionRecord
        fun category(id: String, startedAt: Instant, endedAt: Instant, stage: HealthSleepStage) = HealthSampleInput(
            healthSampleId = id, healthType = "sleepAnalysis", startedAt = startedAt, endedAt = endedAt, categoryValue = stage.rawValue,
            sourceName = ANDROID_SOURCE, sourceBundleId = sourceOf(record),
        )
        // A session with no stages is the whole time asleep, as a phone's own sleep log records it.
        if (record.stages.isEmpty()) listOf(category(record.metadata.id, record.startTime, record.endTime, HealthSleepStage.AsleepUnspecified))
        else record.stages.mapIndexedNotNull { index, stage -> sleepStage(stage.stage)?.let { mapped -> category("${record.metadata.id}:$index", stage.startTime, stage.endTime, mapped) } }
    }

    private val heartRate = HealthConnectKind("heartRate", HeartRateRecord::class) { record ->
        record as HeartRateRecord
        record.samples.mapIndexed { index, sample -> quantity(record, "heartRate", "count/min", sample.time, sample.time, sample.beatsPerMinute.toDouble(), "${record.metadata.id}:$index") }
    }

    /** The activity type is named by what HealthKit calls the common ones; any other is sent as "other". */
    private fun workoutName(exerciseType: Int): String = when (exerciseType) {
        ExerciseSessionRecord.EXERCISE_TYPE_RUNNING, ExerciseSessionRecord.EXERCISE_TYPE_RUNNING_TREADMILL -> "running"
        ExerciseSessionRecord.EXERCISE_TYPE_WALKING -> "walking"
        ExerciseSessionRecord.EXERCISE_TYPE_BIKING, ExerciseSessionRecord.EXERCISE_TYPE_BIKING_STATIONARY -> "cycling"
        ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_POOL, ExerciseSessionRecord.EXERCISE_TYPE_SWIMMING_OPEN_WATER -> "swimming"
        ExerciseSessionRecord.EXERCISE_TYPE_HIKING -> "hiking"
        ExerciseSessionRecord.EXERCISE_TYPE_YOGA -> "yoga"
        ExerciseSessionRecord.EXERCISE_TYPE_ROWING, ExerciseSessionRecord.EXERCISE_TYPE_ROWING_MACHINE -> "rowing"
        ExerciseSessionRecord.EXERCISE_TYPE_STRENGTH_TRAINING, ExerciseSessionRecord.EXERCISE_TYPE_WEIGHTLIFTING -> "traditionalStrengthTraining"
        else -> "other"
    }

    /** The workout's key for its energy and distance in the sample's metadata, which the day summary reads. */
    fun workoutSample(record: ExerciseSessionRecord, totals: Map<String, JsonElement> = emptyMap()): HealthSampleInput = HealthSampleInput(
        healthSampleId = record.metadata.id, healthType = "workout", startedAt = record.startTime, endedAt = record.endTime,
        workoutActivityType = workoutName(record.exerciseType), sourceName = ANDROID_SOURCE, sourceBundleId = sourceOf(record),
        sampleMetadata = (if (record.title != null) mapOf("title" to JsonPrimitive(record.title)) else emptyMap()) + totals,
    )

    // A session alone has neither its totals nor its route; HealthSync reads those and sends the sample again with them.
    private val workout = HealthConnectKind("workout", ExerciseSessionRecord::class) { record -> listOf(workoutSample(record as ExerciseSessionRecord)) }

    val all: List<HealthConnectKind> = listOf(
        single("stepCount", "count", StepsRecord::class) { record -> interval(record.startTime, record.endTime, record.count.toDouble()) },
        single("distanceWalkingRunning", "m", DistanceRecord::class) { record -> interval(record.startTime, record.endTime, record.distance.inMeters) },
        single("activeEnergyBurned", "kcal", ActiveCaloriesBurnedRecord::class) { record -> interval(record.startTime, record.endTime, record.energy.inKilocalories) },
        single("flightsClimbed", "count", FloorsClimbedRecord::class) { record -> interval(record.startTime, record.endTime, record.floors) },
        heartRate,
        single("restingHeartRate", "count/min", RestingHeartRateRecord::class) { record -> instant(record.time, record.beatsPerMinute.toDouble()) },
        single("bodyMass", "kg", WeightRecord::class) { record -> instant(record.time, record.weight.inKilograms) },
        single("height", "m", HeightRecord::class) { record -> instant(record.time, record.height.inMeters) },
        // HealthKit sends a percentage as a fraction.
        single("bodyFatPercentage", "%", BodyFatRecord::class) { record -> instant(record.time, record.percentage.value / 100.0) },
        single("oxygenSaturation", "%", OxygenSaturationRecord::class) { record -> instant(record.time, record.percentage.value / 100.0) },
        single("respiratoryRate", "count/min", RespiratoryRateRecord::class) { record -> instant(record.time, record.rate) },
        single("vo2Max", "ml/(kg*min)", Vo2MaxRecord::class) { record -> instant(record.time, record.vo2MillilitersPerMinuteKilogram) },
        single("bodyTemperature", "degC", BodyTemperatureRecord::class) { record -> instant(record.time, record.temperature.inCelsius) },
        single("bloodGlucose", "mg/dL", BloodGlucoseRecord::class) { record -> instant(record.time, record.level.inMilligramsPerDeciliter) },
        single("dietaryWater", "mL", HydrationRecord::class) { record -> interval(record.startTime, record.endTime, record.volume.inMilliliters) },
        // The same record holds both pressures; the server keeps one sample for each id, so the second is named apart.
        HealthConnectKind(listOf("bloodPressureSystolic", "bloodPressureDiastolic"), BloodPressureRecord::class) { record ->
            record as BloodPressureRecord
            listOf(
                quantity(record, "bloodPressureSystolic", "mmHg", record.time, record.time, record.systolic.inMillimetersOfMercury, record.metadata.id),
                quantity(record, "bloodPressureDiastolic", "mmHg", record.time, record.time, record.diastolic.inMillimetersOfMercury, "${record.metadata.id}:diastolic"),
            )
        },
        single("leanBodyMass", "kg", LeanBodyMassRecord::class) { record -> instant(record.time, record.mass.inKilograms) },
        single("basalBodyTemperature", "degC", BasalBodyTemperatureRecord::class) { record -> instant(record.time, record.temperature.inCelsius) },
        single("pushCount", "count", WheelchairPushesRecord::class) { record -> interval(record.startTime, record.endTime, record.count.toDouble()) },
        nutrition,
        menstrualFlow,
        category("intermenstrualBleeding", IntermenstrualBleedingRecord::class, { record -> record.time }) { NOT_APPLICABLE },
        category("ovulationTestResult", OvulationTestRecord::class, { record -> record.time }) { record -> ovulationTestResult(record.result) },
        category("cervicalMucusQuality", CervicalMucusRecord::class, { record -> record.time }) { record -> cervicalMucusQuality(record.appearance) },
        category("sexualActivity", SexualActivityRecord::class, { record -> record.time }) { NOT_APPLICABLE },
        sleep,
        workout,
    )
}
