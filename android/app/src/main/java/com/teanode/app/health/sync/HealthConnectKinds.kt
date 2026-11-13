package com.teanode.app.health.sync

import androidx.health.connect.client.records.ActiveCaloriesBurnedRecord
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
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import kotlin.reflect.KClass

/**
 * HealthConnectKind is one kind of Health Connect record, the HealthKit type name it is sent under (so one person's iPhone
 * and Android phone add up on one server), and how a record becomes readings. A record that holds a series (heart rate,
 * sleep stages) becomes one reading for each point, named by the record's id and the point's place.
 */
class HealthConnectKind(val healthType: String, val recordClass: KClass<out Record>, val samples: (Record) -> List<HealthSampleInput>)

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

    // The totals of a workout (energy, distance) come from Health Connect's aggregate of the session; this reads only the session itself, so they are left out of its metadata.
    private val workout = HealthConnectKind("workout", ExerciseSessionRecord::class) { record ->
        record as ExerciseSessionRecord
        listOf(
            HealthSampleInput(
                healthSampleId = record.metadata.id, healthType = "workout", startedAt = record.startTime, endedAt = record.endTime,
                workoutActivityType = workoutName(record.exerciseType), sourceName = ANDROID_SOURCE, sourceBundleId = sourceOf(record),
                sampleMetadata = if (record.title != null) mapOf("title" to JsonPrimitive(record.title)) else emptyMap(),
            ),
        )
    }

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
        HealthConnectKind("bloodPressureSystolic", BloodPressureRecord::class) { record ->
            record as BloodPressureRecord
            listOf(quantity(record, "bloodPressureSystolic", "mmHg", record.time, record.time, record.systolic.inMillimetersOfMercury, record.metadata.id))
        },
        // The same record holds both pressures; the server keeps one sample for each id, so the second is named apart.
        HealthConnectKind("bloodPressureDiastolic", BloodPressureRecord::class) { record ->
            record as BloodPressureRecord
            listOf(quantity(record, "bloodPressureDiastolic", "mmHg", record.time, record.time, record.diastolic.inMillimetersOfMercury, "${record.metadata.id}:diastolic"))
        },
        sleep,
        workout,
    )
}
