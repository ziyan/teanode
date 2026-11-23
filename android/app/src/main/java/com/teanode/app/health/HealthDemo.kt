package com.teanode.app.health

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import com.teanode.kit.location.ServerTime
import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.temporal.TemporalAdjusters
import kotlin.math.cos
import kotlin.math.sin

/** The invented Health data that demo mode and the screenshots show: a person's steps, heart rate, sleep, weight and workouts, and somebody who shares. */
object HealthDemo {
    /** Which state the invented world is in: the screenshot tests change it. */
    enum class Scenario { Everyday, Empty }

    @Volatile var scenario: Scenario = Scenario.Everyday

    const val SHARING_PERSON_ID = "sam"
    const val RUN_SAMPLE_ID = "workout-run-1"

    private fun today(): LocalDate = LocalDate.now()

    private class Kind(val healthType: String, val healthCategory: String, val typeName: String, val valueKind: String, val unit: String, val summaryKind: String, val latest: String)

    private val kinds = listOf(
        Kind("stepCount", "activity", "Steps", "quantity", "count", "sum", """"sumValue":8412,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":96,"durationSeconds":null"""),
        Kind("distanceWalkingRunning", "activity", "Walking + running distance", "quantity", "m", "sum", """"sumValue":6240,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":96,"durationSeconds":null"""),
        Kind("activeEnergyBurned", "activity", "Active energy", "quantity", "kcal", "sum", """"sumValue":512.7,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":96,"durationSeconds":null"""),
        Kind("workout", "activity", "Workouts", "workout", "", "duration", """"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":1,"durationSeconds":2400"""),
        Kind("bodyMass", "bodyMeasurements", "Weight", "quantity", "kg", "average", """"sumValue":null,"averageValue":72.4,"minimumValue":72.1,"maximumValue":72.6,"sampleCount":2,"durationSeconds":null"""),
        Kind("heartRate", "heart", "Heart rate", "quantity", "count/min", "average", """"sumValue":null,"averageValue":68,"minimumValue":52,"maximumValue":131,"sampleCount":480,"durationSeconds":null"""),
        Kind("restingHeartRate", "heart", "Resting heart rate", "quantity", "count/min", "average", """"sumValue":null,"averageValue":57,"minimumValue":57,"maximumValue":57,"sampleCount":1,"durationSeconds":null"""),
        Kind("oxygenSaturation", "respiratory", "Blood oxygen", "quantity", "%", "average", """"sumValue":null,"averageValue":0.975,"minimumValue":0.95,"maximumValue":0.99,"sampleCount":12,"durationSeconds":null"""),
        Kind("sleepAnalysis", "sleep", "Sleep", "category", "", "duration", """"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":6,"durationSeconds":26100"""),
        Kind("headache", "symptoms", "Headache", "category", "", "count", """"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":1,"durationSeconds":null"""),
    )

    private fun typeJson(kind: Kind): String =
        """{"healthType":"${kind.healthType}","healthCategory":"${kind.healthCategory}","typeName":"${kind.typeName}","valueKind":"${kind.valueKind}","quantityUnit":"${kind.unit}","summaryKind":"${kind.summaryKind}","firstDay":"${today().minusDays(400)}","lastDay":"${today()}","sampleCount":${if (kind.healthType == "workout") 26 else 4200},"latestDaySummary":{"summaryDay":"${today()}","quantityUnit":"${kind.unit}",${kind.latest},"categorySeconds":{}}}"""

    /** One period of a kind: a day, week or month, with numbers that move a little from day to day. */
    private fun periodJson(kind: Kind, startDay: LocalDate, endDay: LocalDate, dayCount: Int, step: Int): String {
        val wave = sin(step / 3.0) * 0.25 + cos(step / 7.0) * 0.12
        val days = dayCount.toDouble()
        val common = """"startDay":"$startDay","endDay":"$endDay","dayCount":$dayCount"""
        return when (kind.healthType) {
            "stepCount" -> """{$common,"sumValue":${Math.round((8200 + wave * 5000) * days)},"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":${90 * dayCount},"durationSeconds":null,"categorySeconds":{}}"""
            "distanceWalkingRunning" -> """{$common,"sumValue":${Math.round((6100 + wave * 3500) * days)},"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":${90 * dayCount},"durationSeconds":null,"categorySeconds":{}}"""
            "activeEnergyBurned" -> """{$common,"sumValue":${Math.round((480 + wave * 220) * days)},"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":${90 * dayCount},"durationSeconds":null,"categorySeconds":{}}"""
            "bodyMass" -> """{$common,"sumValue":null,"averageValue":${"%.1f".format(72.3 + wave * 1.2)},"minimumValue":${"%.1f".format(71.8 + wave)},"maximumValue":${"%.1f".format(72.9 + wave)},"sampleCount":${2 * dayCount},"durationSeconds":null,"categorySeconds":{}}"""
            "heartRate", "restingHeartRate" -> {
                val base = if (kind.healthType == "heartRate") 70 else 58
                """{$common,"sumValue":null,"averageValue":${"%.1f".format(base + wave * 8)},"minimumValue":${base - 14 + (wave * 4).toInt()},"maximumValue":${base + 60 + (wave * 10).toInt()},"sampleCount":${240 * dayCount},"durationSeconds":null,"categorySeconds":{}}"""
            }
            "oxygenSaturation" -> """{$common,"sumValue":null,"averageValue":${"%.3f".format(0.975 + wave * 0.01)},"minimumValue":0.94,"maximumValue":0.99,"sampleCount":${12 * dayCount},"durationSeconds":null,"categorySeconds":{}}"""
            "sleepAnalysis" -> {
                val asleep = (25_000 + wave * 5000) * days
                """{$common,"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":${5 * dayCount},"durationSeconds":${asleep.toLong()},"categorySeconds":{"asleep":${asleep.toLong()},"asleepDeep":${(asleep * 0.18).toLong()},"asleepCore":${(asleep * 0.55).toLong()},"asleepREM":${(asleep * 0.27).toLong()},"awake":${(1500 * days).toLong()}}}"""
            }
            "workout" -> if (step % 3 == 0) """{$common,"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":1,"durationSeconds":${2400 + step % 5 * 300},"categorySeconds":{}}""" else """{$common,"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":0,"durationSeconds":null,"categorySeconds":{}}"""
            else -> """{$common,"sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":${if (step % 4 == 0) 1 else 0},"durationSeconds":null,"categorySeconds":{}}"""
        }
    }

    private fun summaryJson(variables: kotlinx.serialization.json.JsonObject): String {
        val healthType = variables.text("healthType") ?: "stepCount"
        val kind = kinds.firstOrNull { each -> each.healthType == healthType } ?: kinds.first()
        val from = LocalDate.parse(variables.text("fromDay") ?: today().minusDays(29).toString())
        val to = LocalDate.parse(variables.text("toDay") ?: today().toString())
        val period = variables.text("period") ?: "day"
        val rows = mutableListOf<String>()
        var step = 0
        when (period) {
            "week" -> {
                var start = from.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY))
                while (!start.isAfter(to)) { rows.add(periodJson(kind, start, start.plusDays(6), 7, step++)); start = start.plusWeeks(1) }
            }
            "month" -> {
                var start = from.withDayOfMonth(1)
                while (!start.isAfter(to)) { rows.add(periodJson(kind, start, start.plusMonths(1).minusDays(1), start.lengthOfMonth(), step++)); start = start.plusMonths(1) }
            }
            else -> {
                var day = from
                while (!day.isAfter(to)) { rows.add(periodJson(kind, day, day, 1, step++)); day = day.plusDays(1) }
            }
        }
        return """{"healthType":"${kind.healthType}","quantityUnit":"${kind.unit}","summaryKind":"${kind.summaryKind}","periods":${rows.joinToString(",", "[", "]")}}"""
    }

    private fun sampleJson(id: String, healthType: String, minutesAgo: Long, minutes: Long, quantity: String, unit: String, extra: String = ""): String {
        val started = ServerTime.text(Instant.now().minusSeconds(minutesAgo * 60))
        val ended = ServerTime.text(Instant.now().minusSeconds((minutesAgo - minutes) * 60))
        return """{"healthSampleId":"$id","healthType":"$healthType","startedAt":"$started","endedAt":"$ended","quantityValue":$quantity,"quantityUnit":"$unit","categoryValue":null,"workoutActivityType":"","sourceName":"Example Watch","sourceBundleId":"com.example.watch","deviceName":"Watch","sampleMetadataJson":"{}","receivedAt":"$ended","hasRoute":false$extra}"""
    }

    private fun samples(healthType: String): String {
        val rows = when (healthType) {
            "workout" -> listOf(
                """{"healthSampleId":"$RUN_SAMPLE_ID","healthType":"workout","startedAt":"${ServerTime.text(Instant.now().minusSeconds(6 * 3600))}","endedAt":"${ServerTime.text(Instant.now().minusSeconds(6 * 3600 - 2400))}","quantityValue":null,"quantityUnit":"","categoryValue":null,"workoutActivityType":"running","sourceName":"Example Watch","sourceBundleId":"com.example.watch","deviceName":"Watch","sampleMetadataJson":"{\"totalDistanceMeters\":5200,\"totalEnergyBurnedKilocalories\":410}","receivedAt":"${ServerTime.text(Instant.now())}","hasRoute":true}""",
                """{"healthSampleId":"workout-walk-1","healthType":"workout","startedAt":"${ServerTime.text(Instant.now().minusSeconds(30 * 3600))}","endedAt":"${ServerTime.text(Instant.now().minusSeconds(30 * 3600 - 1800))}","quantityValue":null,"quantityUnit":"","categoryValue":null,"workoutActivityType":"walking","sourceName":"Example Watch","sourceBundleId":"com.example.watch","deviceName":"Watch","sampleMetadataJson":"{}","receivedAt":"${ServerTime.text(Instant.now())}","hasRoute":false}""",
            )
            "heartRate" -> listOf(sampleJson("hr-1", healthType, 5, 0, "64", "count/min"), sampleJson("hr-2", healthType, 35, 0, "71", "count/min"), sampleJson("hr-3", healthType, 95, 0, "132", "count/min"))
            "bodyMass" -> listOf(sampleJson("w-1", healthType, 600, 0, "72.4", "kg"), sampleJson("w-2", healthType, 2000, 0, "72.1", "kg"))
            "sleepAnalysis" -> listOf(
                """{"healthSampleId":"sl-1","healthType":"sleepAnalysis","startedAt":"${ServerTime.text(Instant.now().minusSeconds(9 * 3600))}","endedAt":"${ServerTime.text(Instant.now().minusSeconds(8 * 3600))}","quantityValue":null,"quantityUnit":"","categoryValue":4,"workoutActivityType":"","sourceName":"Example Watch","sourceBundleId":"com.example.watch","deviceName":"Watch","sampleMetadataJson":"{}","receivedAt":"${ServerTime.text(Instant.now())}","hasRoute":false}""",
            )
            else -> listOf(sampleJson("s-1", healthType, 60, 30, "1200", "count"), sampleJson("s-2", healthType, 180, 30, "950", "count"))
        }
        return """{"samples":${rows.joinToString(",", "[", "]")},"totalCount":${rows.size},"nextOffset":null}"""
    }

    private fun shares(): String = """[{"id":"share-in","ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam Example","granteeUserId":"robin","granteeUsername":"robin","granteeName":"Robin Example","granteeCircleId":null,"granteeCircleName":null,"healthCategories":["activity","sleep"],"createdAt":"2026-09-01T08:00:00Z"},{"id":"share-out","ownerUserId":"robin","ownerUsername":"robin","ownerName":"Robin Example","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"circle-1","granteeCircleName":"Example Household","healthCategories":["activity","heart","sleep"],"createdAt":"2026-09-02T08:00:00Z"}]"""

    private fun settingsJson(canAgentRead: Boolean = false): String =
        """{"canAgentReadHealth":$canAgentRead,"healthErasedAt":null,"sampleCount":18420,"lastReceivedAt":"${ServerTime.text(Instant.now().minusSeconds(600))}","modelProviderName":"Example Models","characteristics":[{"healthType":"dateOfBirth","characteristicValue":"1990-04-12"},{"healthType":"biologicalSex","characteristicValue":"female"},{"healthType":"bloodType","characteristicValue":"aPositive"}]}"""

    private fun routeJson(): String {
        val rows = (0 until 40).map { step ->
            val fraction = step / 39.0
            "[${step * 60},${10.5 + 0.012 * fraction + sin(fraction * 6) * 0.002},${20.5 + 0.02 * fraction},${3 + step % 5}]"
        }
        return """{"healthSampleId":"$RUN_SAMPLE_ID","routePoints":${rows.joinToString(",", "[", "]")}}"""
    }

    /** What demo mode answers for Health. */
    val answerers: List<DemoAnswerer> = listOf(
        graphQLField("ListHealthTypes") { variables ->
            val kindsShown = if (scenario == Scenario.Empty) emptyList() else if (variables.text("ownerUserId") != null) kinds.filter { kind -> kind.healthCategory in setOf("activity", "sleep") } else kinds
            kindsShown.joinToString(",", "[", "]") { kind -> typeJson(kind) }
        },
        graphQLField("ReadHealthSummary") { variables -> summaryJson(variables) },
        graphQLField("ListHealthSamples") { variables -> samples(variables.text("healthType") ?: "stepCount") },
        graphQLField("ReadHealthRoute") { routeJson() },
        graphQLField("ReadHealthSettings") { settingsJson() },
        graphQLField("UpdateHealthSettings") { variables -> settingsJson(variables.text("canAgentReadHealth") == "true") },
        graphQLField("EraseHealthData") { """{"erasedSampleCount":4200}""" },
        graphQLField("ListHealthShares") { shares() },
        graphQLField("SetHealthShare") { """{"id":"share-new","ownerUserId":"robin","ownerUsername":"robin","ownerName":"Robin Example","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"circle-1","granteeCircleName":"Example Household","healthCategories":["activity"],"createdAt":"2026-09-02T08:00:00Z"}""" },
        graphQLField("ReportHealth") { """{"newSampleCount":0,"deletedSampleCount":0,"updatedDayCount":0,"deletedSamples":[],"unmatchedRouteHealthSampleIds":[],"healthErasedAt":null}""" },
    )
}

/** What demo mode answers for Health, so every screen and state can be reached and photographed without a server. */
val healthDemoAnswerers: List<DemoAnswerer> = HealthDemo.answerers

