package com.teanode.kit.health

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.location.ServerTime
import com.teanode.kit.location.connectionAnswering
import com.teanode.kit.location.instantOf
import com.teanode.kit.location.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File
import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneId
import java.util.UUID

/** What the phone sends the server about Health, what it reads back, how batches are cut, how day summaries are worked out from samples, and what it remembers between runs. All the data is invented. */
class HealthTest {
    private fun zone(identifier: String): ZoneId = ZoneId.of(identifier)

    /** A moment on a zone's clock. */
    private fun moment(zone: ZoneId, year: Int, month: Int, day: Int, hour: Int, minute: Int = 0): Instant =
        LocalDateTime.of(year, month, day, hour, minute).atZone(zone).toInstant()

    private fun sample(number: Int, healthType: String = "stepCount") = HealthSampleInput(
        healthSampleId = "sample-$number", healthType = healthType, startedAt = instantOf("2024-03-01T10:00:00Z"),
        endedAt = instantOf("2024-03-01T10:05:00Z"), quantityValue = number.toDouble(), quantityUnit = "count",
    )

    private fun summary(number: Int) = HealthDaySummaryInput(
        healthType = "stepCount", summaryDay = String.format("2024-01-%02d", number % 28 + 1), quantityUnit = "count", sumValue = number.toDouble(), sampleCount = 1,
    )

    private fun route(workoutId: String) = HealthRouteInput(workoutId, listOf(listOf(0.0, 10.5, 20.5, 3.0), listOf(60.0, 10.6, 20.6, 4.0)))

    // The upload shapes

    @Test
    fun aSampleIsSentWithTheServersKeysAndOnlyTheValuesItHas() {
        val sample = HealthSampleInput(
            "sample-1", "heartRate", instantOf("2024-03-01T10:00:00Z"), instantOf("2024-03-01T10:00:30Z"), 61.5, "count/min",
            sourceName = "Fixture Watch", sourceBundleId = "com.example.watch", deviceName = "Watch", sampleMetadata = mapOf("motionContext" to JsonPrimitive(2)),
        )
        assertEquals(
            JsonObject(
                mapOf(
                    "healthSampleId" to JsonPrimitive("sample-1"), "healthType" to JsonPrimitive("heartRate"), "startedAt" to JsonPrimitive("2024-03-01T10:00:00Z"),
                    "endedAt" to JsonPrimitive("2024-03-01T10:00:30Z"), "quantityValue" to JsonPrimitive(61.5), "quantityUnit" to JsonPrimitive("count/min"),
                    "workoutActivityType" to JsonPrimitive(""), "sourceName" to JsonPrimitive("Fixture Watch"), "sourceBundleId" to JsonPrimitive("com.example.watch"),
                    "deviceName" to JsonPrimitive("Watch"), "sampleMetadataJson" to JsonPrimitive("""{"motionContext":2}"""),
                ),
            ),
            sample.variables,
        )
        // A category sample has no quantity and the other way round.
        val sleep = HealthSampleInput("sample-2", "sleepAnalysis", instantOf("2024-03-01T22:00:00Z"), instantOf("2024-03-02T06:00:00Z"), categoryValue = 3)
        assertEquals(JsonPrimitive(3), (sleep.variables as JsonObject)["categoryValue"])
        assertNull((sleep.variables as JsonObject)["quantityValue"])
        assertEquals(JsonPrimitive("{}"), (sleep.variables as JsonObject)["sampleMetadataJson"])
    }

    @Test
    fun aSampleIsTheSameSampleWhenItsJsonIsTheSame() {
        val original = HealthSampleInput(
            "sample-3", "workout", instantOf("2024-03-01T07:00:00Z"), instantOf("2024-03-01T07:30:00Z"), workoutActivityType = "running",
            sampleMetadata = mapOf(HealthWorkoutMetadata.TOTAL_DISTANCE_METERS to JsonPrimitive(5000.0)),
        )
        val sameAgain = HealthSampleInput(
            "sample-3", "workout", instantOf("2024-03-01T07:00:00Z"), instantOf("2024-03-01T07:30:00Z"), workoutActivityType = "running",
            sampleMetadata = HealthJson.objectOf(HealthJson.text(original.sampleMetadata)),
        )
        assertEquals(original, sameAgain)
        assertEquals(original.hashCode(), sameAgain.hashCode())
    }

    @Test
    fun aDaySummaryCarriesItsSecondsAsJsonText() {
        val sleep = HealthDaySummaryInput(
            healthType = "sleepAnalysis", summaryDay = "2024-03-02", sampleCount = 4, durationSeconds = 25_200.0,
            categorySeconds = mapOf("asleep" to 25_200.0, "asleepCore" to 14_400.0),
        )
        val fields = sleep.variables as JsonObject
        assertEquals(JsonPrimitive("sleepAnalysis"), fields["healthType"])
        assertEquals(JsonPrimitive(4), fields["sampleCount"])
        assertEquals(JsonPrimitive(25_200.0), fields["durationSeconds"])
        assertEquals(JsonPrimitive("""{"asleep":25200.0,"asleepCore":14400.0}"""), fields["categorySecondsJson"])
        val steps = summary(5).variables as JsonObject
        assertEquals(JsonPrimitive(5.0), steps["sumValue"])
        assertNull(steps["averageValue"])
        assertNull(steps["durationSeconds"])
        assertEquals(JsonPrimitive("{}"), steps["categorySecondsJson"])
    }

    @Test
    fun aRouteAndACharacteristicAreSentWithTheirKeys() {
        val sent = route("workout-1").variables as JsonObject
        assertEquals(JsonPrimitive("workout-1"), sent["healthSampleId"])
        assertEquals(2, (sent["routePoints"] as JsonArray).size)
        assertEquals(
            JsonObject(mapOf("healthType" to JsonPrimitive("bloodType"), "characteristicValue" to JsonPrimitive("aPositive"))),
            HealthCharacteristicInput("bloodType", "aPositive").variables,
        )
    }

    @Test
    fun anEnormousRouteIsThinnedToTheMostKeepingTheEnds() {
        val points = (0 until 45_000).map { index -> HealthRoutePoint(index.toDouble(), 10 + index / 1_000_000.0, 20.0, 0.0) }
        val thinned = HealthRouteInput.of("workout-1", points)
        assertEquals(HealthRouteInput.POINT_COUNT_MOST, thinned.routePoints.size)
        assertEquals(0.0, thinned.points.first().secondsFromStart)
        assertEquals(44_999.0, thinned.points.last().secondsFromStart)
        assertTrue(thinned.isAcceptable)
        // A point off the earth is left out rather than costing the route.
        val withBadPoint = HealthRouteInput.of("workout-1", listOf(HealthRoutePoint(0.0, 10.0, 20.0, 1.0), HealthRoutePoint(1.0, 100.0, 20.0, 1.0)))
        assertEquals(1, withBadPoint.routePoints.size)
    }

    @Test
    fun reportsABatchWithThePhoneAndEveryList() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """
            {"data":{"ReportHealth":{"newSampleCount":1,"deletedSampleCount":2,"updatedDayCount":3,
             "deletedSamples":[{"healthType":"stepCount","startedAt":"2024-03-01T10:00:00.123456789Z","endedAt":"2024-03-01T10:05:00Z"}],
             "healthErasedAt":"2024-03-02T00:00:00Z"}}}
            """,
        )
        val batch = HealthBatch(
            samples = listOf(sample(1)), deletedHealthSampleIds = listOf("gone-1"), daySummaries = listOf(summary(1)), routes = listOf(route("workout-1")),
            characteristics = listOf(HealthCharacteristicInput("biologicalSex", "female")),
        )
        val result = connection.reportHealth(batch, "phone-1", "2024-03-01T00:00:00.123456789Z")
        assertEquals(1, result.newSampleCount)
        assertEquals(2, result.deletedSampleCount)
        assertEquals(3, result.updatedDayCount)
        assertEquals("stepCount", result.deletedSamples.first().healthType)
        assertEquals(instantOf("2024-03-01T10:00:00Z"), result.deletedSamples.first().startedInstant)
        assertEquals(instantOf("2024-03-02T00:00:00Z"), ServerTime.instant(result.healthErasedAt))
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("phone-1"), variables["agentPhoneId"])
        assertEquals(setOf("agentPhoneId", "healthErasedAt", "samples", "deletedHealthSampleIds", "daySummaries", "routes", "characteristics"), variables.keys)
        assertEquals(JsonPrimitive("2024-03-01T00:00:00.123456789Z"), variables["healthErasedAt"])
        assertEquals(JsonArray(listOf(JsonPrimitive("gone-1"))), variables["deletedHealthSampleIds"])
        assertEquals(JsonArray(listOf(sample(1).variables)), variables["samples"])
        assertEquals(JsonArray(listOf(route("workout-1").variables)), variables["routes"])
    }

    @Test
    fun everyReportSaysTheLastEraseThePhoneKnowsOfExactlyAsTheServerWroteItOrNullWhenItKnowsOfNone() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ReportHealth":{"newSampleCount":0,"deletedSampleCount":0,"updatedDayCount":0}}}""")
        connection.reportHealth(HealthBatch(samples = listOf(sample(1))), healthErasedAt = "2024-03-01T08:30:15.987654321+09:00")
        connection.reportHealth(HealthBatch(samples = listOf(sample(1))), healthErasedAt = null)
        assertEquals(JsonPrimitive("2024-03-01T08:30:15.987654321+09:00"), sentVariables(fake, 0)["healthErasedAt"])
        assertEquals(JsonNull, sentVariables(fake, 1)["healthErasedAt"])
        val query = fake.sent[0].body!!.toString(Charsets.UTF_8)
        assertTrue(query.contains("healthErasedAt: \$healthErasedAt"))
        assertFalse(query.contains("healthTurnedOnAt"))
    }

    @Test
    fun anAnswerReadsTheRoutesLeftOutForWantOfTheirWorkout() {
        val result = TeaNodeJson.decodeFromString(
            ReportHealthResult.serializer(),
            """{"newSampleCount":2,"deletedSampleCount":0,"updatedDayCount":0,"deletedSamples":[],"unmatchedRouteHealthSampleIds":["workout-9"],"healthErasedAt":null}""",
        )
        assertEquals(listOf("workout-9"), result.unmatchedRouteHealthSampleIds)
        val both = result.merged(ReportHealthResult(newSampleCount = 1, unmatchedRouteHealthSampleIds = listOf("workout-10")))
        assertEquals(listOf("workout-9", "workout-10"), both.unmatchedRouteHealthSampleIds)
        assertTrue(TeaNodeJson.decodeFromString(ReportHealthResult.serializer(), """{"newSampleCount":0}""").unmatchedRouteHealthSampleIds.isEmpty())
    }

    // The views

    @Test
    fun readsTheTypesAPersonHas() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """
            {"data":{"ListHealthTypes":[
             {"healthType":"stepCount","healthCategory":"activity","typeName":"Steps","valueKind":"quantity","quantityUnit":"count",
              "summaryKind":"sum","firstDay":"2024-01-01","lastDay":"2024-03-01","sampleCount":120,
              "latestDaySummary":{"summaryDay":"2024-03-01","quantityUnit":"count","sumValue":8000,"averageValue":null,"minimumValue":null,
               "maximumValue":null,"sampleCount":12,"durationSeconds":null,"categorySeconds":{}}},
             {"healthType":"sleepAnalysis","healthCategory":"sleep","typeName":"Sleep","valueKind":"category","quantityUnit":"",
              "summaryKind":"duration","firstDay":null,"lastDay":null,"sampleCount":0,"latestDaySummary":null}]}}
            """,
        )
        val types = connection.healthTypes("user-2")
        assertEquals(listOf("stepCount", "sleepAnalysis"), types.map { each -> each.healthType })
        assertEquals(HealthCategory.Activity, types[0].healthCategoryValue)
        assertEquals(HealthSummaryKind.Sum, types[0].summaryKindValue)
        assertEquals(8000.0, types[0].latestDaySummary?.sumValue)
        assertEquals("2024-03-01", types[0].latestDaySummary?.summaryDay)
        assertNull(types[1].latestDaySummary)
        assertNull(types[1].firstDay)
        assertEquals(mapOf("ownerUserId" to JsonPrimitive("user-2")), sentVariables(fake))
    }

    @Test
    fun readsASummaryByPeriodWithSleepStages() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """
            {"data":{"ReadHealthSummary":{"healthType":"sleepAnalysis","quantityUnit":"","summaryKind":"duration","periods":[
             {"startDay":"2024-03-04","endDay":"2024-03-10","sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,
              "sampleCount":30,"durationSeconds":176400,"categorySeconds":{"asleep":176400,"asleepREM":30000}}]}}}
            """,
        )
        val summary = connection.healthSummary("sleepAnalysis", "2024-03-04", "2024-03-10", HealthPeriod.Week)
        assertEquals(1, summary.periods.size)
        assertEquals(176_400.0, summary.periods[0].durationSeconds)
        assertEquals(30_000.0, summary.periods[0].categorySecondsByStage["asleepREM"])
        assertEquals(HealthSummaryKind.Duration, summary.summaryKindValue)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("week"), variables["period"])
        assertEquals(JsonPrimitive("2024-03-04"), variables["fromDay"])
        assertEquals(JsonPrimitive("2024-03-10"), variables["toDay"])
        assertNull(variables["ownerUserId"])
    }

    @Test
    fun aPeriodReadsItsSecondsAsJsonTextToo() {
        val period = TeaNodeJson.decodeFromString(
            HealthPeriodView.serializer(), """{"startDay":"2024-03-04","endDay":"2024-03-04","sampleCount":1,"categorySeconds":"{\"awake\":600}"}""",
        )
        assertEquals(mapOf("awake" to 600.0), period.categorySecondsByStage)
        assertNull(period.sumValue)
        assertTrue(TeaNodeJson.decodeFromString(HealthPeriodView.serializer(), """{"startDay":"2024-03-04","endDay":"2024-03-04"}""").categorySecondsByStage.isEmpty())
    }

    @Test
    fun aPeriodSaysHowManyOfItsDaysHaveASummary() {
        val period = TeaNodeJson.decodeFromString(
            HealthPeriodView.serializer(), """{"startDay":"2024-03-01","endDay":"2024-03-31","sampleCount":12,"dayCount":12,"sumValue":96000}""",
        )
        assertEquals(12, period.dayCount)
        assertEquals(0, TeaNodeJson.decodeFromString(HealthPeriodView.serializer(), """{"startDay":"2024-03-04","endDay":"2024-03-04"}""").dayCount)
        assertTrue(HealthDocuments.HEALTH_SUMMARY.contains("dayCount"))
    }

    @Test
    fun readsAPageOfSamplesWithTheirMetadata() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """
            {"data":{"ListHealthSamples":{"totalCount":51,"nextOffset":50,"samples":[
             {"healthSampleId":"workout-1","healthType":"workout","startedAt":"2024-03-01T07:00:00Z","endedAt":"2024-03-01T07:30:00Z",
              "quantityValue":null,"quantityUnit":"","categoryValue":null,"workoutActivityType":"running","sourceName":"Fixture Watch",
              "sourceBundleId":"com.example.watch","deviceName":"Watch","sampleMetadataJson":"{\"totalDistanceMeters\":5000}",
              "receivedAt":"2024-03-01T08:00:00Z","hasRoute":true}]}}}
            """,
        )
        val page = connection.healthSamples("workout", from = instantOf("2024-03-01T00:00:00Z"), limitCount = 50, offset = 0)
        assertEquals(51, page.totalCount)
        assertEquals(50, page.nextOffset)
        val workout = page.samples.first()
        assertEquals("workout-1", workout.id)
        assertTrue(workout.hasRoute)
        assertEquals("running", workout.workoutActivityType)
        assertEquals(5000.0, HealthJson.number(workout.sampleMetadata[HealthWorkoutMetadata.TOTAL_DISTANCE_METERS]))
        assertEquals(instantOf("2024-03-01T07:00:00Z"), workout.startedInstant)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("2024-03-01T00:00:00Z"), variables["from"])
        assertNull(variables["to"])
        assertEquals(JsonPrimitive(50), variables["limitCount"])
    }

    @Test
    fun readsARoute() = runBlocking {
        val (connection, _) = connectionAnswering("""{"data":{"ReadHealthRoute":{"healthSampleId":"workout-1","routePoints":[[0,10.5,20.5,3],[60,10.6,20.6,4.5]]}}}""")
        val route = connection.healthRoute("workout-1")
        assertEquals(2, route.points.size)
        assertEquals(HealthRoutePoint(60.0, 10.6, 20.6, 4.5), route.points[1])
    }

    @Test
    fun theSettingsReadWhenTheServerSendsNullForTheListOfFacts() {
        val settings = TeaNodeJson.decodeFromString(
            HealthSettingsView.serializer(),
            """{"canAgentReadHealth":true,"healthErasedAt":null,"sampleCount":3,"lastReceivedAt":null,"modelProviderName":"","characteristics":null}""",
        )
        assertTrue(settings.canAgentReadHealth)
        assertTrue(settings.characteristicList.isEmpty())
        val asked = TeaNodeJson.decodeFromString(HealthSettingsView.serializer(), """{"canAgentReadHealth":false}""")
        assertEquals(0, asked.sampleCount)
        assertEquals("", asked.modelProviderName)
    }

    @Test
    fun readsAndUpdatesTheSettings() = runBlocking {
        val settingsAnswer = """
            {"canAgentReadHealth":true,"healthErasedAt":null,"sampleCount":500,"lastReceivedAt":"2024-03-01T08:00:00Z",
             "modelProviderName":"Example Models","characteristics":[{"healthType":"bloodType","characteristicValue":"aPositive"}]}
            """
        val (connection, fake) = connectionAnswering("""{"data":{"ReadHealthSettings":$settingsAnswer,"UpdateHealthSettings":$settingsAnswer}}""")
        val settings = connection.healthSettings()
        assertTrue(settings.canAgentReadHealth)
        assertNull(settings.healthErasedInstant)
        assertEquals(instantOf("2024-03-01T08:00:00Z"), settings.lastReceivedInstant)
        assertEquals("Example Models", settings.modelProviderName)
        assertEquals(listOf(HealthCharacteristicView("bloodType", "aPositive")), settings.characteristicList)
        connection.updateHealthSettings(false)
        assertEquals(mapOf("canAgentReadHealth" to JsonPrimitive(false)), sentVariables(fake, 1))
    }

    @Test
    fun erasesOneTypeOrEverything() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"EraseHealthData":{"erasedSampleCount":7}}}""")
        assertEquals(7, connection.eraseHealthData("stepCount").erasedSampleCount)
        connection.eraseHealthData()
        assertEquals(mapOf("healthType" to JsonPrimitive("stepCount")), sentVariables(fake, 0))
        assertEquals(emptyMap<String, JsonElement>(), sentVariables(fake, 1))
    }

    @Test
    fun readsAndSetsShares() = runBlocking {
        val shareAnswer = """
            {"id":"share-1","ownerUserId":"user-1","ownerUsername":"robin","ownerName":null,"granteeUserId":null,"granteeUsername":null,
             "granteeName":"","granteeCircleId":"circle-1","granteeCircleName":"Household","healthCategories":["sleep","activity","somethingNew"],
             "createdAt":"2024-03-01T08:00:00Z"}
            """
        val (connection, fake) = connectionAnswering("""{"data":{"ListHealthShares":[$shareAnswer],"SetHealthShare":$shareAnswer}}""")
        val shares = connection.healthShares()
        assertEquals(1, shares.size)
        assertEquals("Household", shares[0].granteeCircleName)
        assertEquals(listOf(HealthCategory.Activity, HealthCategory.Sleep), shares[0].sharedCategories)
        val set = connection.setHealthShare(null, "circle-1", listOf(HealthCategory.Sleep, HealthCategory.Activity))
        assertEquals("share-1", set?.id)
        val variables = sentVariables(fake, 1)
        assertEquals(JsonNull, variables["granteeUserId"])
        assertEquals(JsonPrimitive("circle-1"), variables["granteeCircleId"])
        assertEquals(JsonArray(listOf(JsonPrimitive("sleep"), JsonPrimitive("activity"))), variables["healthCategories"])
    }

    @Test
    fun removingAShareAnswersNothing() = runBlocking {
        val (connection, _) = connectionAnswering("""{"data":{"SetHealthShare":null}}""")
        assertNull(connection.setHealthShare("user-2", null, emptyList()))
    }

    // Batches

    @Test
    fun whatWaitsIsCutIntoCallsOfTheMostTheServerTakes() {
        val plan = HealthBatch.requests(
            samples = (0 until 2500).map { index -> sample(index) }, deletedHealthSampleIds = (0 until 4500).map { index -> "gone-$index" },
            daySummaries = (0 until 1001).map { index -> summary(index) },
        )
        assertEquals(0, plan.rejectedEntryCount)
        assertEquals(3, plan.requests.size)
        assertEquals(listOf(1000, 1000, 500), plan.requests.map { batch -> batch.samples.size })
        assertEquals(listOf(2000, 2000, 500), plan.requests.map { batch -> batch.deletedHealthSampleIds.size })
        assertEquals(listOf(1000, 1, 0), plan.requests.map { batch -> batch.daySummaries.size })
        assertTrue(plan.requests.all { batch -> batch.isWithinLimits })
        // Nothing is lost or repeated, and the oldest go first.
        assertEquals((0 until 2500).map { index -> "sample-$index" }, plan.requests.flatMap { batch -> batch.samples }.map { each -> each.healthSampleId })
        assertEquals((0 until 4500).map { index -> "gone-$index" }, plan.requests.flatMap { batch -> batch.deletedHealthSampleIds })
    }

    @Test
    fun atMostFiveRoutesAndTwentyCharacteristicsGoInACall() {
        val plan = HealthBatch.requests(
            routes = (0 until 12).map { index -> route("workout-$index") },
            characteristics = (0 until 21).map { index -> HealthCharacteristicInput("type-$index", "value") },
        )
        assertEquals(3, plan.requests.size)
        assertEquals(listOf(5, 5, 2), plan.requests.map { batch -> batch.routes.size })
        assertEquals(listOf(20, 1, 0), plan.requests.map { batch -> batch.characteristics.size })
        assertTrue(plan.requests.all { batch -> batch.isWithinLimits })
    }

    @Test
    fun aRouteIsNeverSentBeforeItsWorkout() {
        val workouts = (0 until 1500).map { index -> sample(index, "workout") }
        val plan = HealthBatch.requests(samples = workouts, routes = listOf(route("sample-1200"), route("sample-3"), route("not-in-this-batch")))
        assertEquals(2, plan.requests.size)
        assertEquals(listOf("sample-3", "not-in-this-batch"), plan.requests[0].routes.map { each -> each.healthSampleId })
        assertEquals(listOf("sample-1200"), plan.requests[1].routes.map { each -> each.healthSampleId })
    }

    @Test
    fun whatTheServerWouldRefuseIsLeftOutAndCounted() {
        val nan = HealthSampleInput("sample-nan", "stepCount", instantOf("2024-03-01T10:00:00Z"), instantOf("2024-03-01T10:05:00Z"), Double.NaN)
        val backwards = HealthSampleInput("sample-backwards", "stepCount", instantOf("2024-03-01T10:05:00Z"), instantOf("2024-03-01T10:00:00Z"), 1.0)
        val badDay = HealthDaySummaryInput(healthType = "stepCount", summaryDay = "2024-3-1", sumValue = 1.0, sampleCount = 1)
        val plan = HealthBatch.requests(samples = listOf(sample(1), nan, backwards), deletedHealthSampleIds = listOf("gone-1", ""), daySummaries = listOf(summary(1), badDay))
        assertEquals(4, plan.rejectedEntryCount)
        assertEquals(1, plan.requests.size)
        assertEquals(3, plan.requests[0].entryCount)
        assertTrue(HealthBatch.requests().requests.isEmpty())
    }

    @Test
    fun batchesTooBigToSendAreCutInHalvesUntilTheyFit() {
        // Five routes of ten thousand points are a few megabytes of JSON.
        fun longRoute(workoutId: String) = HealthRouteInput(workoutId, (0 until 10_000).map { index -> listOf(index.toDouble(), 10.5 + index * 0.00001, 20.5 + index * 0.00001, 3.0) })
        val routes = (0 until 5).map { index -> longRoute("workout-$index") }
        val whole = HealthBatch(routes = routes)
        assertTrue(whole.encodedByteCount > 1_000_000)
        val pieces = whole.cutToFit(whole.encodedByteCount / 2)
        assertTrue(pieces.size > 1)
        assertEquals((0 until 5).map { index -> "workout-$index" }, pieces.flatMap { batch -> batch.routes }.map { each -> each.healthSampleId })
        assertTrue(pieces.all { batch -> batch.encodedByteCount <= whole.encodedByteCount / 2 || batch.entryCount == 1 })
        // A single entry is never cut, and what fits is left as it is.
        assertEquals(1, HealthBatch(routes = listOf(routes[0])).cutToFit(10).size)
        assertEquals(1, whole.cutToFit(HealthBatch.ENCODED_BYTE_COUNT_MOST).size)
        assertTrue(HealthBatch.requests(samples = (0 until 3).map { index -> sample(index) }).requests.all { batch -> batch.encodedByteCount < HealthBatch.ENCODED_BYTE_COUNT_MOST })
    }

    @Test
    fun metadataOverTheServersLimitLosesKeysAndNeverTheSample() {
        val writerKeys = mapOf(
            "HKExternalUUID" to JsonPrimitive("x".repeat(5000)), "HKWasUserEntered" to JsonPrimitive(true), "note" to JsonPrimitive("n".repeat(4000)),
        )
        val ownKeys = mapOf(
            HealthWorkoutMetadata.TOTAL_DISTANCE_METERS to JsonPrimitive(6800), HealthWorkoutMetadata.TOTAL_ENERGY_BURNED_KILOCALORIES to JsonPrimitive(480),
        )
        val big = HealthSampleInput(
            "workout-1", "workout", instantOf("2024-03-01T10:00:00Z"), instantOf("2024-03-01T10:40:00Z"), workoutActivityType = "running", sampleMetadata = writerKeys + ownKeys,
        )
        assertTrue(HealthJson.text(big.sampleMetadata).toByteArray().size <= HealthMetadataFit.BYTE_COUNT_MOST)
        // The writing app's longest keys go first; the app's own totals and the small key stay.
        assertNull(big.sampleMetadata["HKExternalUUID"])
        assertEquals(JsonPrimitive(6800), big.sampleMetadata[HealthWorkoutMetadata.TOTAL_DISTANCE_METERS])
        assertEquals(JsonPrimitive(true), big.sampleMetadata["HKWasUserEntered"])
        assertTrue(big.isAcceptable)

        // Metadata that fits is left alone, and a text beyond the limit in an own key goes last of all, not the sample.
        val small = mapOf("HKWasUserEntered" to JsonPrimitive(false))
        assertEquals(small, HealthMetadataFit.fitting(small))
        assertTrue(HealthMetadataFit.fitting(mapOf("audiogramPoints" to JsonPrimitive("a".repeat(20_000)))).isEmpty())
    }

    @Test
    fun aNullCharacterIsTakenOutOfTheTextTheServerWouldRefuse() {
        val sample = HealthSampleInput(
            "sample-1", "stepCount", instantOf("2024-03-01T10:00:00Z"), instantOf("2024-03-01T10:05:00Z"), 1.0, "count",
            sourceName = "Wa\u0000tch", sampleMetadata = mapOf("note" to JsonPrimitive("a\u0000b")),
        )
        assertEquals("Watch", sample.sourceName)
        assertEquals(JsonPrimitive("ab"), sample.sampleMetadata["note"])
        assertFalse(HealthJson.text(sample.sampleMetadata).contains("\\u0000"))
    }

    @Test
    fun aBatchTheServerCallsInvalidIsSplitUntilTheOneEntryIsFound() {
        val batch = HealthBatch(
            samples = listOf(sample(1), sample(2), sample(3)), deletedHealthSampleIds = listOf("gone-1"), daySummaries = listOf(summary(1)), routes = listOf(route("sample-1")),
        )
        val apart = batch.split()!!
        // The samples and the deleted identifiers first, the rest after, so a route still comes after its workout.
        assertEquals(3, apart.first.samples.size)
        assertEquals(listOf("gone-1"), apart.first.deletedHealthSampleIds)
        assertTrue(apart.first.routes.isEmpty())
        assertEquals(1, apart.second.daySummaries.size)
        assertEquals(1, apart.second.routes.size)
        assertEquals(batch.entryCount, apart.first.entryCount + apart.second.entryCount)

        val halves = HealthBatch(samples = listOf(sample(1), sample(2), sample(3))).split()!!
        assertEquals(listOf("sample-1"), halves.first.samples.map { each -> each.healthSampleId })
        assertEquals(listOf("sample-2", "sample-3"), halves.second.samples.map { each -> each.healthSampleId })
        val ids = HealthBatch(deletedHealthSampleIds = listOf("a", "b")).split()!!
        assertEquals(listOf("a"), ids.first.deletedHealthSampleIds)
        assertEquals(listOf("b"), ids.second.deletedHealthSampleIds)

        // One entry cannot be split: it is the one that is refused.
        assertNull(HealthBatch(samples = listOf(sample(1))).split())
        assertNull(HealthBatch(routes = listOf(route("workout-1"))).split())
        assertNull(HealthBatch().split())
    }

    @Test
    fun oneBadSampleIsDroppedAndTheOthersBesideItAreSent() = runBlocking {
        val calls = mutableListOf<HealthBatch>()
        val batch = HealthBatch(samples = (0 until 9).map { index -> sample(index) }, daySummaries = listOf(summary(1)))
        val outcome = HealthBatchSender.sendSplittingInvalid(batch) { attempt ->
            calls.add(attempt)
            if (attempt.samples.any { each -> each.healthSampleId == "sample-5" }) throw TeaNodeError.Refused("api: invalid arguments: sample 5 is wrong")
            ReportHealthResult(newSampleCount = attempt.samples.size, updatedDayCount = attempt.daySummaries.size, healthErasedAt = if (attempt.samples.isEmpty()) null else "2024-03-02T00:00:00Z")
        }
        assertEquals(1, outcome.droppedEntryCount)
        assertEquals(listOf("api: invalid arguments: sample 5 is wrong"), outcome.droppedReasons)
        assertEquals(8, outcome.result.newSampleCount)
        assertEquals(1, outcome.result.updatedDayCount)
        assertEquals("2024-03-02T00:00:00Z", outcome.result.healthErasedAt)
        val accepted = calls.filter { attempt -> attempt.samples.none { each -> each.healthSampleId == "sample-5" } }
        assertEquals((0 until 9).filter { index -> index != 5 }.map { index -> "sample-$index" }.toSet(), accepted.flatMap { attempt -> attempt.samples }.map { each -> each.healthSampleId }.toSet())
    }

    @Test
    fun aFailureThatIsNotTheBatchStopsAtOnceAndKeepsEverything() = runBlocking {
        val calls = mutableListOf<HealthBatch>()
        val batch = HealthBatch(samples = (0 until 4).map { index -> sample(index) })
        val failure = runCatching {
            HealthBatchSender.sendSplittingInvalid(batch) { attempt ->
                calls.add(attempt)
                throw java.io.IOException("offline")
            }
        }.exceptionOrNull()
        assertNotNull(failure)
        assertEquals(HealthReportFailure.RetryLater, HealthReportFailure.classify(failure!!))
        assertEquals(1, calls.size)
    }

    @Test
    fun everyDaySummaryCarriesWhenThePhoneWorkedItOut() {
        val plain = summary(1)
        assertNull(plain.workedOutAt)
        val stamped = plain.workedOut(instantOf("2024-03-01T08:30:15Z"))
        assertEquals(instantOf("2024-03-01T08:30:15Z"), stamped.workedOutAt)
        assertEquals(plain.sumValue, stamped.sumValue)
        assertEquals(plain.summaryDay, stamped.summaryDay)
        val rows = HealthBatch(daySummaries = listOf(stamped)).variables["daySummaries"] as JsonArray
        assertEquals(JsonPrimitive("2024-03-01T08:30:15Z"), (rows.first() as JsonObject)["workedOutAt"])
    }

    @Test
    fun theEntryTheServerNamesIsDroppedAndTheRestIsSentWithoutHalving() = runBlocking {
        val calls = mutableListOf<HealthBatch>()
        val batch = HealthBatch(samples = (0 until 5).map { index -> sample(index) }, deletedHealthSampleIds = listOf("gone-1"), daySummaries = listOf(summary(1), summary(2)))
        val outcome = HealthBatchSender.sendSplittingInvalid(batch) { attempt ->
            calls.add(attempt)
            val position = attempt.samples.indexOfFirst { each -> each.healthSampleId == "sample-2" }
            if (position >= 0) throw TeaNodeError.Refused("api: invalid arguments: samples[$position] ends before it starts")
            if (attempt.daySummaries.size == 2) throw TeaNodeError.Refused("api: invalid arguments: daySummaries[1] is more than a day in the future")
            ReportHealthResult(newSampleCount = attempt.samples.size, updatedDayCount = attempt.daySummaries.size)
        }
        assertEquals(2, outcome.droppedEntryCount)
        assertEquals(2, outcome.droppedReasons.size)
        assertEquals(4, outcome.result.newSampleCount)
        assertEquals(1, outcome.result.updatedDayCount)
        // The whole, then without the sample, then without the summary: no halving.
        assertEquals(3, calls.size)
        assertEquals(4, calls.last().samples.size)
        assertEquals(listOf("gone-1"), calls.last().deletedHealthSampleIds)
    }

    @Test
    fun aReasonThatNamesNoEntryTheBatchHasFallsBackToHalving() {
        val batch = HealthBatch(samples = listOf(sample(1), sample(2)), routes = listOf(route("workout-1")))
        assertNull(batch.removingEntry("api: invalid arguments: send at most 1000 samples at a time"))
        assertNull(batch.removingEntry("api: invalid arguments: samples[7] ends before it starts"))
        assertNull(batch.removingEntry("api: invalid arguments: samples[x] is wrong"))
        assertEquals(listOf("sample-2"), batch.removingEntry("invalid arguments: samples[0] is wrong")?.samples?.map { each -> each.healthSampleId })
        assertEquals(0, batch.removingEntry("invalid arguments: routes[0] is wrong")?.routes?.size)
        assertEquals(true, HealthBatch(characteristics = listOf(HealthCharacteristicInput("bloodType", "x"))).removingEntry("invalid arguments: characteristics[0] is wrong")?.isEmpty)
    }

    @Test
    fun aFaultOfTheCallItselfIsNotMendedByHalvingTheBatch() = runBlocking {
        val calls = mutableListOf<HealthBatch>()
        val batch = HealthBatch(samples = (0 until 4).map { index -> sample(index) })
        val failure = runCatching {
            HealthBatchSender.sendSplittingInvalid(batch) { attempt ->
                calls.add(attempt)
                throw TeaNodeError.Refused("api: invalid arguments: healthErasedAt is not a time")
            }
        }.exceptionOrNull()
        assertEquals(HealthReportFailure.BatchInvalid("api: invalid arguments: healthErasedAt is not a time"), HealthReportFailure.classify(failure!!))
        // Sent once, whole: nothing was dropped.
        assertEquals(1, calls.size)
        assertEquals(4, calls.first().samples.size)
    }

    @Test
    fun aFailedBatchIsRetriedOnlyForACallThatDidNotGetThrough() {
        assertEquals(HealthReportFailure.Refused("api: permission denied"), HealthReportFailure.classify(TeaNodeError.Refused("api: permission denied")))
        assertEquals(HealthReportFailure.BatchInvalid("api: invalid arguments: bad sample"), HealthReportFailure.classify(TeaNodeError.Refused("api: invalid arguments: bad sample")))
        assertEquals(HealthReportFailure.PhoneNotFound, HealthReportFailure.classify(TeaNodeError.Refused("api: not found")))
        assertEquals(HealthReportFailure.PhoneNotFound, HealthReportFailure.classify(TeaNodeError.Refused("db: not found")))
        assertEquals(HealthReportFailure.RetryLater, HealthReportFailure.classify(TeaNodeError.Refused("something went wrong")))
        assertEquals(HealthReportFailure.RetryLater, HealthReportFailure.classify(TeaNodeError.SignedOut))
        assertEquals(HealthReportFailure.RetryLater, HealthReportFailure.classify(java.io.IOException("offline")))
    }

    @Test
    fun eventsWaitHalfAMinuteAfterAFailureButNotAfterASuccess() {
        val gate = HealthRetryGate()
        val start = instantOf("2024-03-01T10:00:00Z")
        assertTrue(gate.canTry(start))
        gate.didFail(start)
        assertFalse(gate.canTry(start.plusSeconds(29)))
        assertTrue(gate.canTry(start.plusSeconds(30)))
        gate.didFail(start)
        gate.didSucceed()
        assertTrue(gate.canTry(start))
    }

    // The types

    @Test
    fun everyTypeIsListedOnceWithAUnitOnlyWhenItIsAQuantity() {
        val identifiers = HealthTypes.all.map { info -> info.healthType }
        assertEquals(identifiers.size, identifiers.toSet().size)
        for (info in HealthTypes.all) {
            assertEquals(info.valueKind == HealthValueKind.Quantity, info.quantityUnit.isNotEmpty(), info.healthType)
            assertEquals(info.valueKind == HealthValueKind.Characteristic, info.summaryKind == null, info.healthType)
            assertFalse(info.typeName.isEmpty(), info.healthType)
        }
        for (category in HealthCategory.entries) assertFalse(HealthTypes.types(category).isEmpty(), category.serverWord)
    }

    @Test
    fun theUnitsAreOnesTheServerKnows() {
        val known = setOf(
            "count", "count/min", "m", "m/s", "cm", "kg", "kcal", "min", "ms", "%", "degC", "mg/dL", "mmHg", "ml/(kg*min)", "W", "dBASPL", "S",
            "L", "L/min", "IU", "g", "mg", "mcg", "mL", "kcal/(kg*hr)",
        )
        for (info in HealthTypes.types(HealthValueKind.Quantity)) {
            assertTrue(known.contains(info.quantityUnit), "${info.healthType}: ${info.quantityUnit}")
            assertTrue(info.summaryKind == HealthSummaryKind.Sum || info.summaryKind == HealthSummaryKind.Average, info.healthType)
        }
    }

    @Test
    fun theCommonTypesAreWhereTheHealthAppPutsThem() {
        assertEquals(HealthCategory.Activity, HealthTypes.info("stepCount")?.healthCategory)
        assertEquals(HealthSummaryKind.Sum, HealthTypes.info("stepCount")?.summaryKind)
        assertEquals("count/min", HealthTypes.info("heartRate")?.quantityUnit)
        assertEquals(HealthSummaryKind.Average, HealthTypes.info("heartRate")?.summaryKind)
        assertEquals(HealthCategory.BodyMeasurements, HealthTypes.info("bodyMass")?.healthCategory)
        assertEquals(HealthValueKind.Category, HealthTypes.info("sleepAnalysis")?.valueKind)
        assertEquals(HealthSummaryKind.Duration, HealthTypes.info("sleepAnalysis")?.summaryKind)
        assertEquals(HealthValueKind.Workout, HealthTypes.info("workout")?.valueKind)
        assertEquals(HealthCategory.Symptoms, HealthTypes.info("headache")?.healthCategory)
        assertEquals(HealthValueKind.Characteristic, HealthTypes.info("dateOfBirth")?.valueKind)
        assertNull(HealthTypes.info("notAType"))
    }

    @Test
    fun aTypeNotInTheTableIsNamedFromItsIdentifier() {
        assertEquals("Some new measure", HealthTypes.typeName("someNewMeasure"))
        assertEquals("Heart rate variability", HealthTypes.typeName("heartRateVariabilitySDNN"))
        assertEquals("Heart rate variability SDNN", HealthTypes.words("heartRateVariabilitySDNN"))
        assertEquals("Some SDNN value", HealthTypes.words("someSDNNValue"))
        assertEquals("Step count", HealthTypes.words("stepCount"))
        assertEquals("Steps", HealthTypes.typeName("stepCount"))
    }

    // Days

    @Test
    fun aDayIsWrittenOnTheZonesClock() {
        val moment = instantOf("2024-03-01T20:30:00Z")
        assertEquals("2024-03-02", HealthDays.text(moment, zone("Asia/Tokyo")))
        assertEquals("2024-03-01", HealthDays.text(moment, zone("America/Los_Angeles")))
        assertTrue(HealthDays.isDayText("2024-03-01"))
        assertFalse(HealthDays.isDayText("2024-3-1"))
        assertFalse(HealthDays.isDayText("2024-13-01"))
        assertFalse(HealthDays.isDayText("2024/03/01"))
        assertFalse(HealthDays.isDayText(""))
    }

    @Test
    fun theDayWhenTheClocksChangeIsNot24Hours() {
        val losAngeles = zone("America/Los_Angeles")
        assertEquals(23 * 3600.0, HealthDays.interval("2026-03-08", losAngeles)?.durationSeconds)
        assertEquals(25 * 3600.0, HealthDays.interval("2026-11-01", losAngeles)?.durationSeconds)
        assertEquals(24 * 3600.0, HealthDays.interval("2026-06-01", losAngeles)?.durationSeconds)
    }

    @Test
    fun aReadingEndingAtMidnightDoesNotTouchTheNextDay() {
        val tokyo = zone("Asia/Tokyo")
        val start = moment(tokyo, 2024, 3, 1, 22)
        assertEquals(listOf("2024-03-01"), HealthDays.days(start, moment(tokyo, 2024, 3, 2, 0), tokyo))
        assertEquals(listOf("2024-03-01", "2024-03-02"), HealthDays.days(start, moment(tokyo, 2024, 3, 2, 1), tokyo))
        assertEquals(listOf("2024-03-01"), HealthDays.days(start, start, tokyo))
    }

    // Sleep

    private fun sleepSample(id: String, stage: HealthSleepStage, start: Instant, end: Instant) = HealthCategorySample(id, start, end, stage.rawValue)

    @Test
    fun twoSourcesSleepingOverlappedAreCountedOnce() {
        val tokyo = zone("Asia/Tokyo")
        val phone = sleepSample("phone", HealthSleepStage.AsleepUnspecified, moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 1, 23, 59))
        // A watch reports the stages for the same night, starting 15 minutes later and ending 15 minutes earlier.
        val core = sleepSample("core", HealthSleepStage.AsleepCore, moment(tokyo, 2024, 3, 1, 23, 15), moment(tokyo, 2024, 3, 1, 23, 40))
        val deep = sleepSample("deep", HealthSleepStage.AsleepDeep, moment(tokyo, 2024, 3, 1, 23, 40), moment(tokyo, 2024, 3, 1, 23, 45))
        val summaries = HealthDaySummaries.sleepSummaries(listOf(phone, core, deep), tokyo)
        assertEquals(1, summaries.size)
        val night = summaries[0]
        assertEquals("sleepAnalysis", night.healthType)
        assertEquals("2024-03-01", night.summaryDay)
        // 59 minutes of phone sleep covers every watch stage inside it.
        assertEquals(59 * 60.0, night.categorySeconds["asleep"])
        assertEquals(59 * 60.0, night.durationSeconds)
        assertEquals(59 * 60.0, night.categorySeconds["asleepUnspecified"])
        assertEquals(25 * 60.0, night.categorySeconds["asleepCore"])
        assertEquals(5 * 60.0, night.categorySeconds["asleepDeep"])
        assertNull(night.categorySeconds["asleepREM"])
        assertEquals(3, night.sampleCount)
    }

    @Test
    fun timeInBedAndAwakeAreNotSleep() {
        val tokyo = zone("Asia/Tokyo")
        val samples = listOf(
            sleepSample("bed", HealthSleepStage.InBed, moment(tokyo, 2024, 3, 1, 22), moment(tokyo, 2024, 3, 1, 23, 30)),
            sleepSample("rem", HealthSleepStage.AsleepREM, moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 1, 23, 20)),
            sleepSample("awake", HealthSleepStage.Awake, moment(tokyo, 2024, 3, 1, 23, 20), moment(tokyo, 2024, 3, 1, 23, 30)),
            // A value the platform may add later still counts as a sample.
            HealthCategorySample("future", moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 1, 23, 1), 9),
        )
        val night = HealthDaySummaries.sleepSummaries(samples, tokyo).first()
        assertEquals(90 * 60.0, night.categorySeconds["inBed"])
        assertEquals(20 * 60.0, night.categorySeconds["asleep"])
        assertEquals(10 * 60.0, night.categorySeconds["awake"])
        assertEquals(20 * 60.0, night.durationSeconds)
        assertEquals(4, night.sampleCount)
    }

    @Test
    fun aNightIsSplitAtMidnightByTheZonesDay() {
        val tokyo = zone("Asia/Tokyo")
        val night = sleepSample("night", HealthSleepStage.AsleepCore, moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 2, 7))
        val summaries = HealthDaySummaries.sleepSummaries(listOf(night), tokyo)
        assertEquals(listOf("2024-03-01", "2024-03-02"), summaries.map { each -> each.summaryDay })
        assertEquals(listOf(3600.0, 7.0 * 3600), summaries.map { each -> each.categorySeconds["asleep"] })
        assertEquals(listOf(1, 1), summaries.map { each -> each.sampleCount })
        // The same night on another zone is cut elsewhere: it is 6 am to 2 pm on the 1st in Los Angeles, which is one day.
        val sameNight = HealthDaySummaries.sleepSummaries(listOf(night), zone("America/Los_Angeles"))
        assertEquals(listOf("2024-03-01"), sameNight.map { each -> each.summaryDay })
        assertEquals(8 * 3600.0, sameNight.first().categorySeconds["asleep"])
    }

    @Test
    fun aNightWhenTheClocksGoBackHasTheExtraHourOnTheSecondDay() {
        val losAngeles = zone("America/Los_Angeles")
        // 2026-11-01 is the day the clocks go back: 11 pm on the 31st to 7 am by the new clock is nine hours, not eight.
        val start = moment(losAngeles, 2026, 10, 31, 23)
        val end = moment(losAngeles, 2026, 11, 1, 7)
        assertEquals(9 * 3600L, end.epochSecond - start.epochSecond)
        val summaries = HealthDaySummaries.sleepSummaries(listOf(sleepSample("night", HealthSleepStage.AsleepCore, start, end)), losAngeles)
        assertEquals(listOf("2026-10-31", "2026-11-01"), summaries.map { each -> each.summaryDay })
        assertEquals(3600.0, summaries[0].categorySeconds["asleep"])
        assertEquals(8 * 3600.0, summaries[1].categorySeconds["asleep"])
        assertEquals(9 * 3600.0, summaries.mapNotNull { each -> each.durationSeconds }.sum())
    }

    @Test
    fun aNightWhenTheClocksGoForwardLosesAnHour() {
        val losAngeles = zone("America/Los_Angeles")
        val start = moment(losAngeles, 2026, 3, 7, 23)
        val end = moment(losAngeles, 2026, 3, 8, 7)
        assertEquals(7 * 3600L, end.epochSecond - start.epochSecond)
        val summaries = HealthDaySummaries.sleepSummaries(listOf(sleepSample("night", HealthSleepStage.AsleepCore, start, end)), losAngeles)
        assertEquals(listOf(3600.0, 6.0 * 3600), summaries.map { each -> each.categorySeconds["asleep"] })
    }

    @Test
    fun onlyTheDaysAskedForAreWorkedOut() {
        val tokyo = zone("Asia/Tokyo")
        val night = sleepSample("night", HealthSleepStage.AsleepCore, moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 2, 7))
        val summaries = HealthDaySummaries.categorySummaries("sleepAnalysis", listOf(night), tokyo, setOf("2024-03-02"))
        assertEquals(listOf("2024-03-02"), summaries.map { each -> each.summaryDay })
        assertEquals(7 * 3600.0, summaries.first().categorySeconds["asleep"])
    }

    @Test
    fun aSampleReadTwiceIsCountedOnce() {
        val tokyo = zone("Asia/Tokyo")
        val night = sleepSample("night", HealthSleepStage.AsleepCore, moment(tokyo, 2024, 3, 1, 1), moment(tokyo, 2024, 3, 1, 2))
        val summary = HealthDaySummaries.sleepSummaries(listOf(night, night), tokyo).first()
        assertEquals(1, summary.sampleCount)
        assertEquals(3600.0, summary.durationSeconds)
    }

    // Other categories

    @Test
    fun mindfulMinutesOverlappingAreCountedOnce() {
        val tokyo = zone("Asia/Tokyo")
        val samples = listOf(
            HealthCategorySample("a", moment(tokyo, 2024, 3, 1, 8), moment(tokyo, 2024, 3, 1, 8, 10), 0),
            HealthCategorySample("b", moment(tokyo, 2024, 3, 1, 8, 5), moment(tokyo, 2024, 3, 1, 8, 20), 0),
            HealthCategorySample("c", moment(tokyo, 2024, 3, 1, 18), moment(tokyo, 2024, 3, 1, 18, 5), 0),
        )
        val summaries = HealthDaySummaries.categorySummaries("mindfulSession", samples, tokyo)
        assertEquals(1, summaries.size)
        assertEquals(25 * 60.0, summaries[0].durationSeconds)
        assertEquals(3, summaries[0].sampleCount)
        assertNull(summaries[0].sumValue)
    }

    @Test
    fun eventsAndSymptomsAreCountedByTheDayTheyBegan() {
        val tokyo = zone("Asia/Tokyo")
        val samples = listOf(
            HealthCategorySample("a", moment(tokyo, 2024, 3, 1, 8), moment(tokyo, 2024, 3, 1, 9), 2),
            HealthCategorySample("b", moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 2, 2), 3),
            HealthCategorySample("c", moment(tokyo, 2024, 3, 2, 8), moment(tokyo, 2024, 3, 2, 8), 1),
        )
        val summaries = HealthDaySummaries.categorySummaries("headache", samples, tokyo)
        assertEquals(listOf("2024-03-01", "2024-03-02"), summaries.map { each -> each.summaryDay })
        assertEquals(listOf(2, 1), summaries.map { each -> each.sampleCount })
        assertNull(summaries[0].durationSeconds)
    }

    // Workouts

    @Test
    fun workoutsAddUpByTheDayTheyBegan() {
        val tokyo = zone("Asia/Tokyo")
        val workouts = listOf(
            HealthWorkoutSample("run", moment(tokyo, 2024, 3, 1, 7), moment(tokyo, 2024, 3, 1, 7, 30), 300.0, 5000.0),
            HealthWorkoutSample("swim", moment(tokyo, 2024, 3, 1, 18), moment(tokyo, 2024, 3, 1, 18, 45), 250.0),
            // Begins before midnight and ends after: all on the first day.
            HealthWorkoutSample("late", moment(tokyo, 2024, 3, 1, 23, 30), moment(tokyo, 2024, 3, 2, 0, 30)),
            HealthWorkoutSample("walk", moment(tokyo, 2024, 3, 2, 9), moment(tokyo, 2024, 3, 2, 9, 20), totalDistanceMeters = 1500.0),
        )
        val summaries = HealthDaySummaries.workoutSummaries(workouts + workouts[0], tokyo)
        assertEquals(listOf("2024-03-01", "2024-03-02"), summaries.map { each -> each.summaryDay })
        assertTrue(summaries.all { each -> each.healthType == "workout" })
        assertEquals(3, summaries[0].sampleCount)
        assertEquals((30 + 45 + 60) * 60.0, summaries[0].durationSeconds)
        assertEquals(550.0, summaries[0].categorySeconds["energyKilocalories"])
        assertEquals(5000.0, summaries[0].categorySeconds["distanceMeters"])
        assertEquals(1, summaries[1].sampleCount)
        assertEquals(20 * 60.0, summaries[1].durationSeconds)
        assertNull(summaries[1].categorySeconds["energyKilocalories"])
        assertEquals(1500.0, summaries[1].categorySeconds["distanceMeters"])
    }

    @Test
    fun aWorkoutSampleCarriesItsTotalsInItsMetadata() {
        val sample = HealthSampleInput(
            "run", "workout", instantOf("2024-03-01T07:00:00Z"), instantOf("2024-03-01T07:30:00Z"), workoutActivityType = "running",
            sampleMetadata = mapOf(HealthWorkoutMetadata.TOTAL_ENERGY_BURNED_KILOCALORIES to JsonPrimitive(300.0), HealthWorkoutMetadata.TOTAL_DISTANCE_METERS to JsonPrimitive(5000.0)),
        )
        val workout = HealthWorkoutSample.of(sample)!!
        assertEquals(300.0, workout.totalEnergyKilocalories)
        assertEquals(5000.0, workout.totalDistanceMeters)
        assertNull(HealthWorkoutSample.of(sample(1)))
        assertNull(HealthCategorySample.of(sample(1)))
    }

    // Quantities

    @Test
    fun aQuantitysSummaryKeepsOnlyTheNumbersThatMeanSomething() {
        val statistics = HealthQuantityStatistics(9000.0, 750.0, 1.0, 2000.0, 12)
        val steps = HealthDaySummaries.quantitySummary("stepCount", "2024-03-01", statistics)
        assertEquals("count", steps.quantityUnit)
        assertEquals(9000.0, steps.sumValue)
        assertNull(steps.averageValue)
        assertNull(steps.maximumValue)
        val heart = HealthDaySummaries.quantitySummary("heartRate", "2024-03-01", statistics)
        assertEquals("count/min", heart.quantityUnit)
        assertNull(heart.sumValue)
        assertEquals(750.0, heart.averageValue)
        assertEquals(1.0, heart.minimumValue)
        assertEquals(2000.0, heart.maximumValue)
        assertEquals(12, heart.sampleCount)
        assertTrue(heart.isAcceptable)
    }

    // What the phone remembers

    private fun temporaryDirectory(): File = File(System.getProperty("java.io.tmpdir"), "health-state-${UUID.randomUUID()}")

    @Test
    fun theStateReadsBackFromItsFile() {
        val directory = temporaryDirectory()
        try {
            val tokyo = zone("Asia/Tokyo")
            val state = HealthSyncState("https://mail.example.com|robin")
            state.turnOn("2024-02-01T00:00:00.5Z")
            state.setAnchor("token-a", "stepCount")
            state.setAnchor("token-b", "heartRate")
            state.markPending("sleepAnalysis", moment(tokyo, 2024, 3, 1, 23), moment(tokyo, 2024, 3, 2, 7), tokyo)
            state.recordSent(
                HealthBatch(samples = listOf(sample(1), sample(2)), daySummaries = listOf(summary(1)), routes = listOf(route("workout-1"))),
                ReportHealthResult(newSampleCount = 2, deletedSampleCount = 3, updatedDayCount = 1), instantOf("2024-03-01T01:00:00Z"),
            )

            state.save(directory)
            val fileName = HealthSyncState.fileName("https://mail.example.com|robin")
            assertTrue(File(directory, fileName).exists())
            assertFalse(fileName.contains("/"))
            assertFalse(fileName.contains("|"))

            val loaded = HealthSyncState.load("https://mail.example.com|robin", directory)
            assertEquals("token-a", loaded.anchor("stepCount"))
            assertNull(loaded.anchor("bodyMass"))
            assertEquals(listOf("2024-03-01", "2024-03-02"), loaded.pendingDays("sleepAnalysis"))
            assertEquals(2, loaded.sentSampleCount)
            assertEquals(3, loaded.sentDeletedSampleCount)
            assertEquals(1, loaded.sentDaySummaryCount)
            assertEquals(1, loaded.sentRouteCount)
            assertEquals(instantOf("2024-03-01T01:00:00Z"), loaded.lastSentAt)
            assertTrue(loaded.isTurnedOn)
            assertEquals("2024-02-01T00:00:00.5Z", loaded.healthErasedAt)

            // The same bytes every time, so the file does not churn.
            assertEquals(state.encoded(), loaded.encoded())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun anotherAccountOrAMissingOrBrokenFileStartsClean() {
        val directory = temporaryDirectory()
        try {
            assertEquals(HealthSyncState("account-a").encoded(), HealthSyncState.load("account-a", directory).encoded())
            val state = HealthSyncState("account-a")
            state.turnOn("2024-02-01T00:00:00.5Z")
            state.save(directory)
            assertNotEquals(HealthSyncState("account-a").encoded(), HealthSyncState.load("account-a", directory).encoded())
            assertEquals(HealthSyncState("account-b").encoded(), HealthSyncState.load("account-b", directory).encoded())
            assertNotEquals(HealthSyncState.fileName("account-a"), HealthSyncState.fileName("account-b"))
            File(directory, HealthSyncState.fileName("account-a")).writeText("not json")
            assertEquals(HealthSyncState("account-a").encoded(), HealthSyncState.load("account-a", directory).encoded())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun theAnswerToABatchIsTakenOnlyWhenTheServerStoredIt() {
        val state = HealthSyncState("account-a")
        state.turnOn("2024-03-01T00:00:00Z")
        state.setAnchor("token", "stepCount")
        val batch = HealthBatch(samples = listOf(sample(1), sample(2)))

        // Taken: counted by what was new to the server, and the caller may move anchors.
        assertTrue(state.accept(ReportHealthResult(newSampleCount = 1, healthErasedAt = "2024-03-01T00:00:00Z"), batch, instantOf("2024-03-01T01:00:00Z")))
        assertEquals(1, state.sentSampleCount)
        assertEquals(instantOf("2024-03-01T01:00:00Z"), state.lastSentAt)

        // The server stored nothing because the person erased everything after the erase the phone knew of: nothing is counted, the anchors are forgotten and Health is off, with the reason kept for the settings.
        val erased = ReportHealthResult(healthErasedAt = "2024-03-02T00:00:00Z")
        assertFalse(state.accept(erased, batch, instantOf("2024-03-02T01:00:00Z")))
        assertFalse(state.isTurnedOn)
        assertTrue(state.isStoppedByErase)
        assertTrue(state.anchors.isEmpty())
        assertEquals(0, state.sentSampleCount)
        assertNull(state.lastSentAt)
        assertNull(state.healthErasedAt)

        // Turned on again, having read that erase from the server: it is not news.
        state.turnOn("2024-03-02T00:00:00Z")
        assertFalse(state.isStoppedByErase)
        assertTrue(state.accept(erased, batch, instantOf("2024-03-03T01:00:00Z")))
        assertTrue(state.isTurnedOn)
    }

    @Test
    fun turningOnKeepsTheEraseTimeExactlyAsTheServerWroteItAndTheFileAgrees() {
        val state = HealthSyncState("account-a")
        state.turnOn("2024-03-01T08:30:15.123456789+09:00")
        assertEquals("2024-03-01T08:30:15.123456789+09:00", state.healthErasedAt)
        val loaded = HealthSyncState.decoded(state.encoded())
        assertEquals(state.healthErasedAt, loaded.healthErasedAt)
        assertTrue(loaded.isTurnedOn)
        // Turning on again changes nothing.
        state.turnOn(null)
        assertEquals("2024-03-01T08:30:15.123456789+09:00", state.healthErasedAt)
        // A person who never erased: nothing is known, and nothing is sent.
        val never = HealthSyncState("account-a")
        never.turnOn(null)
        assertNull(never.healthErasedAt)
        assertTrue(never.isTurnedOn)
    }

    @Test
    fun theStoppedByEraseNoticeSurvivesTheFileAndIsClearedByTurningOffOrOn() {
        val state = HealthSyncState("account-a")
        state.turnOn(null)
        assertTrue(state.applyServerErase("2024-03-02T00:00:00Z"))
        assertTrue(HealthSyncState.decoded(state.encoded()).isStoppedByErase)
        state.turnOff()
        assertFalse(state.isStoppedByErase)
        assertFalse(state.applyServerErase("2024-03-02T00:00:00Z"), "off: nothing to stop")
    }

    @Test
    fun theRoutesSentAndTheHistoryAreKeptInTheFileAndForgottenOnReset() {
        val state = HealthSyncState("account-a")
        state.turnOn(null)
        assertFalse(state.hasSentRoute("workout-1"))
        state.recordRouteSent("workout-1")
        state.recordRouteSent("workout-1")
        assertEquals(listOf("workout-1"), state.sentRouteHealthSampleIds)
        state.markHistoryDone()
        val loaded = HealthSyncState.decoded(state.encoded())
        assertTrue(loaded.hasSentRoute("workout-1"))
        assertTrue(loaded.isHistoryDone)
        // Only the latest are remembered.
        for (number in 0 until (HealthSyncState.SENT_ROUTE_COUNT_MOST + 5)) state.recordRouteSent("run-$number")
        assertEquals(HealthSyncState.SENT_ROUTE_COUNT_MOST, state.sentRouteHealthSampleIds.size)
        assertEquals("run-${HealthSyncState.SENT_ROUTE_COUNT_MOST + 4}", state.sentRouteHealthSampleIds.last())
        state.turnOff()
        assertFalse(state.isHistoryDone)
        assertTrue(state.sentRouteHealthSampleIds.isEmpty())
    }

    @Test
    fun theAccountsHealthIsOnForAreFoundFromTheirFiles() {
        val directory = temporaryDirectory()
        try {
            assertTrue(HealthSyncState.accountKeysTurnedOn(directory).isEmpty())
            val on = HealthSyncState("https://mail.example.com|robin")
            on.turnOn(null)
            on.save(directory)
            HealthSyncState("account-off").save(directory)
            File(directory, "health-broken.json").writeText("not json")
            assertEquals(setOf("https://mail.example.com|robin"), HealthSyncState.accountKeysTurnedOn(directory))
            HealthSyncState.remove("https://mail.example.com|robin", directory)
            assertTrue(HealthSyncState.accountKeysTurnedOn(directory).isEmpty())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun anEraseLaterThanTheOneThePhoneKnowsOfResetsTheStateAndTurnsHealthOff() {
        val state = HealthSyncState("account-a")
        state.turnOn("2024-02-01T00:00:00Z")
        state.setAnchor("token", "stepCount")
        state.markPending(HealthPendingDay("stepCount", "2024-03-01"))
        state.recordSent(HealthBatch(samples = listOf(sample(1))), ReportHealthResult.nothing, instantOf("2024-03-01T01:00:00Z"))

        // No erase at all, the one the phone knows of, and an older one change nothing.
        assertFalse(state.applyServerErase(null))
        assertFalse(state.applyServerErase("2024-02-01T00:00:00Z"))
        assertFalse(state.applyServerErase("2024-01-01T00:00:00Z"))
        assertTrue(state.isTurnedOn)
        assertFalse(state.isStoppedByErase)
        assertNotNull(state.anchor("stepCount"))

        assertTrue(state.applyServerErase("2024-03-02T00:00:00Z"))
        assertFalse(state.isTurnedOn)
        assertTrue(state.isStoppedByErase)
        assertTrue(state.anchors.isEmpty())
        assertTrue(state.pendingDays.isEmpty())
        assertEquals(0, state.sentSampleCount)
        assertNull(state.lastSentAt)
        assertEquals("account-a", state.accountKey)

        // Already off: nothing to reset.
        assertFalse(state.applyServerErase("2024-03-09T00:00:00Z"))
    }

    @Test
    fun anyEraseIsNewsToAPhoneThatKnowsOfNoneAndTheFractionOfASecondDecides() {
        val state = HealthSyncState("account-a")
        state.turnOn(null)
        assertTrue(state.isErasedLater("2024-03-02T00:00:00Z"))
        assertFalse(state.isErasedLater(null))

        // The server compares to the fraction, so the phone must too: the erase it was told of is not later than itself, a later fraction is.
        val knowing = HealthSyncState("account-a")
        knowing.turnOn("2024-03-02T00:00:00.250000000Z")
        assertFalse(knowing.isErasedLater("2024-03-02T00:00:00.25Z"))
        assertFalse(knowing.isErasedLater("2024-03-02T00:00:00.2Z"))
        assertTrue(knowing.isErasedLater("2024-03-02T00:00:00.250000001Z"))
        assertTrue(knowing.isErasedLater("2024-03-02T00:00:01Z"))
        // Another offset for the same moment is the same moment.
        assertFalse(knowing.isErasedLater("2024-03-02T09:00:00.25+09:00"))
        assertTrue(ServerTime.isLater("2024-03-02T09:00:00.5+09:00", "2024-03-02T00:00:00.25Z"))
        assertFalse(ServerTime.isLater("not a time", "2024-03-02T00:00:00Z"))
        assertTrue(ServerTime.isLater("2024-03-02T00:00:00Z", "not a time"))
    }

    @Test
    fun entriesTheServerRefusedOneByOneAreCountedUntilHealthIsTurnedOff() {
        val state = HealthSyncState("account-a")
        state.turnOn(null)
        state.recordDropped(2)
        state.recordDropped(0)
        state.recordDropped(-4)
        assertEquals(2, state.droppedEntryCount)
        assertEquals(2, HealthSyncState.decoded(state.encoded()).droppedEntryCount)
        state.turnOff()
        assertEquals(0, state.droppedEntryCount)
    }

    @Test
    fun theLaterEraseOfTwoAnswersIsKeptToTheFraction() {
        val early = ReportHealthResult(newSampleCount = 1, healthErasedAt = "2024-03-02T00:00:00.1Z")
        val late = ReportHealthResult(newSampleCount = 1, healthErasedAt = "2024-03-02T00:00:00.9Z")
        assertEquals("2024-03-02T00:00:00.9Z", early.merged(late).healthErasedAt)
        assertEquals("2024-03-02T00:00:00.9Z", late.merged(early).healthErasedAt)
        assertEquals("2024-03-02T00:00:00.1Z", ReportHealthResult.nothing.merged(early).healthErasedAt)
        assertEquals("2024-03-02T00:00:00.1Z", early.merged(ReportHealthResult.nothing).healthErasedAt)
        assertEquals(2, early.merged(late).newSampleCount)
    }

    @Test
    fun theDaysOfDeletedSamplesWaitToBeWorkedOutAgain() {
        val tokyo = zone("Asia/Tokyo")
        val state = HealthSyncState("account-a")
        state.markPending(
            listOf(
                HealthDeletedSample("stepCount", "2024-03-01T10:00:00Z", "2024-03-01T10:05:00Z"),
                HealthDeletedSample("sleepAnalysis", "2024-03-01T14:00:00Z", "2024-03-01T22:00:00Z"),
                HealthDeletedSample("stepCount", "not a time", "2024-03-01T10:05:00Z"),
            ),
            tokyo,
        )
        assertEquals(listOf("2024-03-01"), state.pendingDays("stepCount"))
        // 23:00 on the 1st to 07:00 on the 2nd in Tokyo.
        assertEquals(listOf("2024-03-01", "2024-03-02"), state.pendingDays("sleepAnalysis"))
        state.clearPending(state.pendingDays.filter { pending -> pending.healthType == "sleepAnalysis" })
        assertTrue(state.pendingDays("sleepAnalysis").isEmpty())
        assertEquals(1, state.pendingDays.size)
        state.setAnchor("token", "stepCount")
        state.setAnchor(null, "stepCount")
        assertNull(state.anchor("stepCount"))
    }

    @Test
    fun everyHealthDocumentAsksForItsOwnOperation() {
        for ((document, operation) in mapOf(
            HealthDocuments.REPORT_HEALTH to "ReportHealth", HealthDocuments.HEALTH_TYPES to "ListHealthTypes", HealthDocuments.HEALTH_SUMMARY to "ReadHealthSummary",
            HealthDocuments.HEALTH_SAMPLES to "ListHealthSamples", HealthDocuments.HEALTH_ROUTE to "ReadHealthRoute", HealthDocuments.HEALTH_SETTINGS to "ReadHealthSettings",
            HealthDocuments.UPDATE_HEALTH_SETTINGS to "UpdateHealthSettings", HealthDocuments.ERASE_HEALTH_DATA to "EraseHealthData",
            HealthDocuments.HEALTH_SHARES to "ListHealthShares", HealthDocuments.SET_HEALTH_SHARE to "SetHealthShare",
        )) {
            assertTrue(document.contains(operation), operation)
        }
    }
}
