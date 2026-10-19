import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// HealthTests cover what the phone sends the server about Health, what it
/// reads back, how batches are cut, how day summaries are worked out from
/// samples, and what it remembers between runs. All the data is invented.
final class HealthTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func date(_ text: String) -> Date { ServerTime.date(text)! }

    private func calendar(_ identifier: String) throws -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: identifier))
        return calendar
    }

    /// A moment on a calendar's clock.
    private func moment(_ calendar: Calendar, _ year: Int, _ month: Int, _ day: Int, _ hour: Int, _ minute: Int = 0) throws -> Date {
        try XCTUnwrap(calendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute)))
    }

    private func sample(_ number: Int, healthType: String = "stepCount") -> HealthSampleInput {
        HealthSampleInput(
            healthSampleId: "sample-\(number)", healthType: healthType, startedAt: date("2024-03-01T10:00:00Z"),
            endedAt: date("2024-03-01T10:05:00Z"), quantityValue: Double(number), quantityUnit: "count")
    }

    private func summary(_ number: Int) -> HealthDaySummaryInput {
        HealthDaySummaryInput(
            healthType: "stepCount", summaryDay: String(format: "2024-01-%02d", number % 28 + 1), quantityUnit: "count", sumValue: Double(number),
            sampleCount: 1)
    }

    private func route(_ workoutId: String) -> HealthRouteInput {
        HealthRouteInput(healthSampleId: workoutId, routePoints: [[0, 10.5, 20.5, 3], [60, 10.6, 20.6, 4]])
    }

    // MARK: The upload shapes

    func testASampleIsSentWithTheServersKeysAndOnlyTheValuesItHas() throws {
        let sample = HealthSampleInput(
            healthSampleId: "sample-1", healthType: "heartRate", startedAt: date("2024-03-01T10:00:00Z"), endedAt: date("2024-03-01T10:00:30Z"),
            quantityValue: 61.5, quantityUnit: "count/min", sourceName: "Fixture Watch", sourceBundleId: "com.example.watch", deviceName: "Watch",
            sampleMetadata: ["motionContext": .number(2)])
        let variables = sample.variables
        XCTAssertEqual(
            variables,
            .object([
                "healthSampleId": .string("sample-1"), "healthType": .string("heartRate"), "startedAt": .string("2024-03-01T10:00:00Z"),
                "endedAt": .string("2024-03-01T10:00:30Z"), "quantityValue": .number(61.5), "quantityUnit": .string("count/min"),
                "workoutActivityType": .string(""), "sourceName": .string("Fixture Watch"), "sourceBundleId": .string("com.example.watch"),
                "deviceName": .string("Watch"), "sampleMetadataJson": .string(#"{"motionContext":2}"#),
            ]))
        // A category sample has no quantity and the other way round.
        let sleep = HealthSampleInput(
            healthSampleId: "sample-2", healthType: "sleepAnalysis", startedAt: date("2024-03-01T22:00:00Z"), endedAt: date("2024-03-02T06:00:00Z"),
            categoryValue: 3)
        XCTAssertEqual(sleep.variables["categoryValue"], .number(3))
        XCTAssertNil(sleep.variables["quantityValue"])
        XCTAssertEqual(sleep.variables["sampleMetadataJson"], .string("{}"))
    }

    func testASampleReadsBackFromItsOwnJSON() throws {
        let original = HealthSampleInput(
            healthSampleId: "sample-3", healthType: "workout", startedAt: date("2024-03-01T07:00:00Z"), endedAt: date("2024-03-01T07:30:00Z"),
            workoutActivityType: "running", sampleMetadata: [HealthWorkoutMetadata.totalDistanceMeters: .number(5000)])
        let decoded = try JSONDecoder().decode(HealthSampleInput.self, from: JSONEncoder().encode(original))
        XCTAssertEqual(decoded, original)
    }

    func testADaySummaryCarriesItsSecondsAsJSONText() {
        let sleep = HealthDaySummaryInput(
            healthType: "sleepAnalysis", summaryDay: "2024-03-02", sampleCount: 4, durationSeconds: 25_200,
            categorySeconds: ["asleep": 25_200, "asleepCore": 14_400])
        XCTAssertEqual(
            sleep.variables,
            .object([
                "healthType": .string("sleepAnalysis"), "summaryDay": .string("2024-03-02"), "quantityUnit": .string(""), "sampleCount": .number(4),
                "durationSeconds": .number(25_200), "categorySecondsJson": .string(#"{"asleep":25200,"asleepCore":14400}"#),
            ]))
        let steps = summary(5)
        XCTAssertEqual(steps.variables["sumValue"], .number(5))
        XCTAssertNil(steps.variables["averageValue"])
        XCTAssertNil(steps.variables["durationSeconds"])
        XCTAssertEqual(steps.variables["categorySecondsJson"], .string("{}"))
    }

    func testARouteAndACharacteristicAreSentWithTheirKeys() {
        XCTAssertEqual(
            route("workout-1").variables,
            .object([
                "healthSampleId": .string("workout-1"),
                "routePoints": .array([
                    .array([.number(0), .number(10.5), .number(20.5), .number(3)]), .array([.number(60), .number(10.6), .number(20.6), .number(4)]),
                ]),
            ]))
        XCTAssertEqual(
            HealthCharacteristicInput(healthType: "bloodType", characteristicValue: "aPositive").variables,
            .object(["healthType": .string("bloodType"), "characteristicValue": .string("aPositive")]))
    }

    func testAnEnormousRouteIsThinnedToTheMostKeepingTheEnds() {
        let points = (0..<45_000).map {
            HealthRoutePoint(secondsFromStart: Double($0), latitudeDegrees: 10 + Double($0) / 1_000_000, longitudeDegrees: 20, altitudeMeters: 0)
        }
        let thinned = HealthRouteInput(healthSampleId: "workout-1", points: points)
        XCTAssertEqual(thinned.routePoints.count, HealthRouteInput.pointCountMost)
        XCTAssertEqual(thinned.points.first?.secondsFromStart, 0)
        XCTAssertEqual(thinned.points.last?.secondsFromStart, 44_999)
        XCTAssertTrue(thinned.isAcceptable)
        // A point off the earth is left out rather than costing the route.
        let withBadPoint = HealthRouteInput(
            healthSampleId: "workout-1",
            points: [
                HealthRoutePoint(secondsFromStart: 0, latitudeDegrees: 10, longitudeDegrees: 20, altitudeMeters: 1),
                HealthRoutePoint(secondsFromStart: 1, latitudeDegrees: 100, longitudeDegrees: 20, altitudeMeters: 1),
            ])
        XCTAssertEqual(withBadPoint.routePoints.count, 1)
    }

    func testReportsABatchWithThePhoneAndEveryList() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ReportHealth":{"newSampleCount":1,"deletedSampleCount":2,"updatedDayCount":3,
                 "deletedSamples":[{"healthType":"stepCount","startedAt":"2024-03-01T10:00:00.123456789Z","endedAt":"2024-03-01T10:05:00Z"}],
                 "healthErasedAt":"2024-03-02T00:00:00Z"}}}
                """#)
        let batch = HealthBatch(
            samples: [sample(1)], deletedHealthSampleIds: ["gone-1"], daySummaries: [summary(1)], routes: [route("workout-1")],
            characteristics: [HealthCharacteristicInput(healthType: "biologicalSex", characteristicValue: "female")])
        let result = try await connection.reportHealth(batch, agentPhoneId: "phone-1", healthErasedAt: "2024-03-01T00:00:00.123456789Z")
        XCTAssertEqual(result.newSampleCount, 1)
        XCTAssertEqual(result.deletedSampleCount, 2)
        XCTAssertEqual(result.updatedDayCount, 3)
        XCTAssertEqual(result.deletedSamples.first?.healthType, "stepCount")
        XCTAssertEqual(result.deletedSamples.first?.startedDate, date("2024-03-01T10:00:00Z"))
        XCTAssertEqual(result.healthErasedDate, date("2024-03-02T00:00:00Z"))
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["agentPhoneId"], .string("phone-1"))
        XCTAssertEqual(
            Set(variables.keys),
            ["agentPhoneId", "healthErasedAt", "samples", "deletedHealthSampleIds", "daySummaries", "routes", "characteristics"])
        XCTAssertEqual(variables["healthErasedAt"], .string("2024-03-01T00:00:00.123456789Z"))
        XCTAssertEqual(variables["deletedHealthSampleIds"], .array([.string("gone-1")]))
        XCTAssertEqual(variables["samples"], .array([sample(1).variables]))
        XCTAssertEqual(variables["routes"], .array([route("workout-1").variables]))
    }


    func testEveryReportSaysTheLastEraseThePhoneKnowsOfExactlyAsTheServerWroteItOrNullWhenItKnowsOfNone() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ReportHealth":{"newSampleCount":0,"deletedSampleCount":0,"updatedDayCount":0}}}"#)
        _ = try await connection.reportHealth(HealthBatch(samples: [sample(1)]), healthErasedAt: "2024-03-01T08:30:15.987654321+09:00")
        _ = try await connection.reportHealth(HealthBatch(samples: [sample(1)]), healthErasedAt: nil)
        XCTAssertEqual(try sentVariables(fake.sent[0])["healthErasedAt"], .string("2024-03-01T08:30:15.987654321+09:00"))
        XCTAssertEqual(try sentVariables(fake.sent[1])["healthErasedAt"], JSONValue.null)
        let query = try XCTUnwrap(String(data: try XCTUnwrap(fake.sent[0].httpBody), encoding: .utf8))
        XCTAssertTrue(query.contains("healthErasedAt: $healthErasedAt"))
        XCTAssertFalse(query.contains("healthTurnedOnAt"))
    }

    func testAnAnswerReadsTheRoutesLeftOutForWantOfTheirWorkout() throws {
        let result = try JSONDecoder().decode(
            ReportHealthResult.self,
            from: Data(
                #"{"newSampleCount":2,"deletedSampleCount":0,"updatedDayCount":0,"deletedSamples":[],"unmatchedRouteHealthSampleIds":["workout-9"],"healthErasedAt":null}"#
                    .utf8))
        XCTAssertEqual(result.unmatchedRouteHealthSampleIds, ["workout-9"])
        let both = result.merged(with: ReportHealthResult(newSampleCount: 1, deletedSampleCount: 0, updatedDayCount: 0, unmatchedRouteHealthSampleIds: ["workout-10"]))
        XCTAssertEqual(both.unmatchedRouteHealthSampleIds, ["workout-9", "workout-10"])
        let none = try JSONDecoder().decode(ReportHealthResult.self, from: Data(#"{"newSampleCount":0}"#.utf8))
        XCTAssertTrue(none.unmatchedRouteHealthSampleIds.isEmpty)
    }

    // MARK: The views

    func testReadsTheTypesAPersonHas() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListHealthTypes":[
                 {"healthType":"stepCount","healthCategory":"activity","typeName":"Steps","valueKind":"quantity","quantityUnit":"count",
                  "summaryKind":"sum","firstDay":"2024-01-01","lastDay":"2024-03-01","sampleCount":120,
                  "latestDaySummary":{"summaryDay":"2024-03-01","quantityUnit":"count","sumValue":8000,"averageValue":null,"minimumValue":null,
                   "maximumValue":null,"sampleCount":12,"durationSeconds":null,"categorySeconds":{}}},
                 {"healthType":"sleepAnalysis","healthCategory":"sleep","typeName":"Sleep","valueKind":"category","quantityUnit":"",
                  "summaryKind":"duration","firstDay":null,"lastDay":null,"sampleCount":0,"latestDaySummary":null}]}}
                """#)
        let types = try await connection.healthTypes(ownerUserId: "user-2")
        XCTAssertEqual(types.map(\.healthType), ["stepCount", "sleepAnalysis"])
        XCTAssertEqual(types[0].healthCategoryValue, .activity)
        XCTAssertEqual(types[0].summaryKindValue, .sum)
        XCTAssertEqual(types[0].latestDaySummary?.sumValue, 8000)
        XCTAssertEqual(types[0].latestDaySummary?.summaryDay, "2024-03-01")
        XCTAssertNil(types[1].latestDaySummary)
        XCTAssertNil(types[1].firstDay)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["ownerUserId": .string("user-2")])
    }

    func testReadsASummaryByPeriodWithSleepStages() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ReadHealthSummary":{"healthType":"sleepAnalysis","quantityUnit":"","summaryKind":"duration","periods":[
                 {"startDay":"2024-03-04","endDay":"2024-03-10","sumValue":null,"averageValue":null,"minimumValue":null,"maximumValue":null,
                  "sampleCount":30,"durationSeconds":176400,"categorySeconds":{"asleep":176400,"asleepREM":30000}}]}}}
                """#)
        let summary = try await connection.healthSummary(healthType: "sleepAnalysis", fromDay: "2024-03-04", toDay: "2024-03-10", period: .week)
        XCTAssertEqual(summary.periods.count, 1)
        XCTAssertEqual(summary.periods[0].durationSeconds, 176_400)
        XCTAssertEqual(summary.periods[0].categorySeconds["asleepREM"], 30_000)
        XCTAssertEqual(summary.summaryKindValue, .duration)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["period"], .string("week"))
        XCTAssertEqual(variables["fromDay"], .string("2024-03-04"))
        XCTAssertEqual(variables["toDay"], .string("2024-03-10"))
        XCTAssertNil(variables["ownerUserId"])
    }

    func testAPeriodReadsItsSecondsAsJSONTextToo() throws {
        let period = try JSONDecoder().decode(
            HealthPeriodView.self,
            from: Data(#"{"startDay":"2024-03-04","endDay":"2024-03-04","sampleCount":1,"categorySeconds":"{\"awake\":600}"}"#.utf8))
        XCTAssertEqual(period.categorySeconds, ["awake": 600])
        XCTAssertNil(period.sumValue)
        let none = try JSONDecoder().decode(HealthPeriodView.self, from: Data(#"{"startDay":"2024-03-04","endDay":"2024-03-04"}"#.utf8))
        XCTAssertTrue(none.categorySeconds.isEmpty)
    }

    func testAPeriodSaysHowManyOfItsDaysHaveASummary() throws {
        let period = try JSONDecoder().decode(
            HealthPeriodView.self,
            from: Data(#"{"startDay":"2024-03-01","endDay":"2024-03-31","sampleCount":12,"dayCount":12,"sumValue":96000}"#.utf8))
        XCTAssertEqual(period.dayCount, 12)
        let older = try JSONDecoder().decode(HealthPeriodView.self, from: Data(#"{"startDay":"2024-03-04","endDay":"2024-03-04"}"#.utf8))
        XCTAssertEqual(older.dayCount, 0)
        XCTAssertTrue(Documents.healthSummary.contains("dayCount"))
    }

    func testReadsAPageOfSamplesWithTheirMetadata() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListHealthSamples":{"totalCount":51,"nextOffset":50,"samples":[
                 {"healthSampleId":"workout-1","healthType":"workout","startedAt":"2024-03-01T07:00:00Z","endedAt":"2024-03-01T07:30:00Z",
                  "quantityValue":null,"quantityUnit":"","categoryValue":null,"workoutActivityType":"running","sourceName":"Fixture Watch",
                  "sourceBundleId":"com.example.watch","deviceName":"Watch","sampleMetadataJson":"{\"totalDistanceMeters\":5000}",
                  "receivedAt":"2024-03-01T08:00:00Z","hasRoute":true}]}}}
                """#)
        let page = try await connection.healthSamples(healthType: "workout", from: date("2024-03-01T00:00:00Z"), limitCount: 50, offset: 0)
        XCTAssertEqual(page.totalCount, 51)
        XCTAssertEqual(page.nextOffset, 50)
        let workout = try XCTUnwrap(page.samples.first)
        XCTAssertEqual(workout.id, "workout-1")
        XCTAssertTrue(workout.hasRoute)
        XCTAssertEqual(workout.workoutActivityType, "running")
        XCTAssertEqual(workout.sampleMetadata[HealthWorkoutMetadata.totalDistanceMeters], .number(5000))
        XCTAssertEqual(workout.startedDate, date("2024-03-01T07:00:00Z"))
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["from"], .string("2024-03-01T00:00:00Z"))
        XCTAssertNil(variables["to"])
        XCTAssertEqual(variables["limitCount"], .number(50))
    }

    func testReadsARoute() async throws {
        let (connection, _) = connection(
            answering: #"{"data":{"ReadHealthRoute":{"healthSampleId":"workout-1","routePoints":[[0,10.5,20.5,3],[60,10.6,20.6,4.5]]}}}"#)
        let route = try await connection.healthRoute(healthSampleId: "workout-1")
        XCTAssertEqual(route.points.count, 2)
        XCTAssertEqual(route.points[1], HealthRoutePoint(secondsFromStart: 60, latitudeDegrees: 10.6, longitudeDegrees: 20.6, altitudeMeters: 4.5))
    }

    func testTheSettingsReadWhenTheServerSendsNullForTheListOfFacts() throws {
        let settings = try JSONDecoder().decode(
            HealthSettingsView.self,
            from: Data(#"{"canAgentReadHealth":true,"healthErasedAt":null,"sampleCount":3,"lastReceivedAt":null,"modelProviderName":"","characteristics":null}"#.utf8))
        XCTAssertTrue(settings.canAgentReadHealth)
        XCTAssertTrue(settings.characteristics.isEmpty)
        let asked = try JSONDecoder().decode(HealthSettingsView.self, from: Data(#"{"canAgentReadHealth":false}"#.utf8))
        XCTAssertEqual(asked.sampleCount, 0)
        XCTAssertEqual(asked.modelProviderName, "")
    }

    func testReadsAndUpdatesTheSettings() async throws {
        let settingsAnswer = #"""
            {"canAgentReadHealth":true,"healthErasedAt":null,"sampleCount":500,"lastReceivedAt":"2024-03-01T08:00:00Z",
             "modelProviderName":"Example Models","characteristics":[{"healthType":"bloodType","characteristicValue":"aPositive"}]}
            """#
        let (connection, fake) = connection(answering: #"{"data":{"ReadHealthSettings":\#(settingsAnswer),"UpdateHealthSettings":\#(settingsAnswer)}}"#)
        let settings = try await connection.healthSettings()
        XCTAssertTrue(settings.canAgentReadHealth)
        XCTAssertNil(settings.healthErasedDate)
        XCTAssertEqual(settings.lastReceivedDate, date("2024-03-01T08:00:00Z"))
        XCTAssertEqual(settings.modelProviderName, "Example Models")
        XCTAssertEqual(settings.characteristics, [HealthCharacteristicView(healthType: "bloodType", characteristicValue: "aPositive")])
        _ = try await connection.updateHealthSettings(canAgentReadHealth: false)
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["canAgentReadHealth": .bool(false)])
    }

    func testErasesOneTypeOrEverything() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"EraseHealthData":{"erasedSampleCount":7}}}"#)
        let one = try await connection.eraseHealthData(healthType: "stepCount")
        XCTAssertEqual(one.erasedSampleCount, 7)
        _ = try await connection.eraseHealthData()
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["healthType": .string("stepCount")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), [:])
    }

    func testReadsAndSetsShares() async throws {
        let shareAnswer = #"""
            {"id":"share-1","ownerUserId":"user-1","ownerUsername":"robin","ownerName":null,"granteeUserId":null,"granteeUsername":null,
             "granteeName":"","granteeCircleId":"circle-1","granteeCircleName":"Household","healthCategories":["sleep","activity","somethingNew"],
             "createdAt":"2024-03-01T08:00:00Z"}
            """#
        let (connection, fake) = connection(answering: #"{"data":{"ListHealthShares":[\#(shareAnswer)],"SetHealthShare":\#(shareAnswer)}}"#)
        let shares = try await connection.healthShares()
        XCTAssertEqual(shares.count, 1)
        XCTAssertEqual(shares[0].granteeCircleName, "Household")
        XCTAssertEqual(shares[0].sharedCategories, [.activity, .sleep])
        let set = try await connection.setHealthShare(granteeUserId: nil, granteeCircleId: "circle-1", healthCategories: [.sleep, .activity])
        XCTAssertEqual(set?.id, "share-1")
        let variables = try sentVariables(fake.sent[1])
        XCTAssertEqual(variables["granteeUserId"], .null)
        XCTAssertEqual(variables["granteeCircleId"], .string("circle-1"))
        XCTAssertEqual(variables["healthCategories"], .array([.string("sleep"), .string("activity")]))
    }

    func testRemovingAShareAnswersNothing() async throws {
        let (connection, _) = connection(answering: #"{"data":{"SetHealthShare":null}}"#)
        let removed = try await connection.setHealthShare(granteeUserId: "user-2", granteeCircleId: nil, healthCategories: [])
        XCTAssertNil(removed)
    }

    func testEveryHealthDocumentIsRegistered() {
        for name in [
            "reportHealth", "healthTypes", "healthSummary", "healthSamples", "healthRoute", "healthSettings", "updateHealthSettings",
            "eraseHealthData", "healthShares", "setHealthShare",
        ] {
            XCTAssertNotNil(Documents.healthDocuments[name], name)
        }
        XCTAssertEqual(Documents.healthDocuments.count, 10)
    }

    // MARK: Batches

    func testWhatWaitsIsCutIntoCallsOfTheMostTheServerTakes() {
        let plan = HealthBatch.requests(
            samples: (0..<2500).map { sample($0) }, deletedHealthSampleIds: (0..<4500).map { "gone-\($0)" },
            daySummaries: (0..<1001).map { summary($0) }, routes: [], characteristics: [])
        XCTAssertEqual(plan.rejectedEntryCount, 0)
        XCTAssertEqual(plan.requests.count, 3)
        XCTAssertEqual(plan.requests.map(\.samples.count), [1000, 1000, 500])
        XCTAssertEqual(plan.requests.map(\.deletedHealthSampleIds.count), [2000, 2000, 500])
        XCTAssertEqual(plan.requests.map(\.daySummaries.count), [1000, 1, 0])
        XCTAssertTrue(plan.requests.allSatisfy(\.isWithinLimits))
        // Nothing is lost or repeated, and the oldest go first.
        XCTAssertEqual(plan.requests.flatMap(\.samples).map(\.healthSampleId), (0..<2500).map { "sample-\($0)" })
        XCTAssertEqual(plan.requests.flatMap(\.deletedHealthSampleIds), (0..<4500).map { "gone-\($0)" })
    }

    func testAtMostFiveRoutesAndTwentyCharacteristicsGoInACall() {
        let plan = HealthBatch.requests(
            routes: (0..<12).map { route("workout-\($0)") },
            characteristics: (0..<21).map { HealthCharacteristicInput(healthType: "type-\($0)", characteristicValue: "value") })
        XCTAssertEqual(plan.requests.count, 3)
        XCTAssertEqual(plan.requests.map(\.routes.count), [5, 5, 2])
        XCTAssertEqual(plan.requests.map(\.characteristics.count), [20, 1, 0])
        XCTAssertTrue(plan.requests.allSatisfy(\.isWithinLimits))
    }

    func testARouteIsNeverSentBeforeItsWorkout() {
        let workouts = (0..<1500).map { sample($0, healthType: "workout") }
        let plan = HealthBatch.requests(samples: workouts, routes: [route("sample-1200"), route("sample-3"), route("not-in-this-batch")])
        XCTAssertEqual(plan.requests.count, 2)
        XCTAssertEqual(plan.requests[0].routes.map(\.healthSampleId), ["sample-3", "not-in-this-batch"])
        XCTAssertEqual(plan.requests[1].routes.map(\.healthSampleId), ["sample-1200"])
    }

    func testWhatTheServerWouldRefuseIsLeftOutAndCounted() {
        let nan = HealthSampleInput(
            healthSampleId: "sample-nan", healthType: "stepCount", startedAt: date("2024-03-01T10:00:00Z"),
            endedAt: date("2024-03-01T10:05:00Z"), quantityValue: .nan)
        let backwards = HealthSampleInput(
            healthSampleId: "sample-backwards", healthType: "stepCount", startedAt: date("2024-03-01T10:05:00Z"),
            endedAt: date("2024-03-01T10:00:00Z"), quantityValue: 1)
        let badDay = HealthDaySummaryInput(healthType: "stepCount", summaryDay: "2024-3-1", sumValue: 1, sampleCount: 1)
        let plan = HealthBatch.requests(
            samples: [sample(1), nan, backwards], deletedHealthSampleIds: ["gone-1", ""], daySummaries: [summary(1), badDay])
        XCTAssertEqual(plan.rejectedEntryCount, 4)
        XCTAssertEqual(plan.requests.count, 1)
        XCTAssertEqual(plan.requests[0].entryCount, 3)
        XCTAssertTrue(HealthBatch.requests().requests.isEmpty)
    }

    func testBatchesTooBigToSendAreCutInHalvesUntilTheyFit() {
        // Five routes of ten thousand points are a few megabytes of JSON.
        func longRoute(_ workoutId: String) -> HealthRouteInput {
            var rows: [[Double]] = []
            for index in 0..<10_000 {
                let offset = Double(index) * 0.00001
                rows.append([Double(index), 10.5 + offset, 20.5 + offset, 3])
            }
            return HealthRouteInput(healthSampleId: workoutId, routePoints: rows)
        }
        let routes = (0..<5).map { longRoute("workout-\($0)") }
        let whole = HealthBatch(routes: routes)
        XCTAssertGreaterThan(whole.encodedByteCount, 1_000_000)
        let pieces = whole.cutToFit(byteCountMost: whole.encodedByteCount / 2)
        XCTAssertGreaterThan(pieces.count, 1)
        XCTAssertEqual(pieces.flatMap(\.routes).map(\.healthSampleId), (0..<5).map { "workout-\($0)" })
        XCTAssertTrue(pieces.allSatisfy { $0.encodedByteCount <= whole.encodedByteCount / 2 || $0.entryCount == 1 })
        // A single entry is never cut, and what fits is left as it is.
        XCTAssertEqual(HealthBatch(routes: [routes[0]]).cutToFit(byteCountMost: 10).count, 1)
        XCTAssertEqual(whole.cutToFit(byteCountMost: HealthBatch.encodedByteCountMost).count, 1)
        // The plan applies the same limit.
        XCTAssertTrue(HealthBatch.requests(samples: (0..<3).map { sample($0) }).requests.allSatisfy { $0.encodedByteCount < HealthBatch.encodedByteCountMost })
    }

    func testMetadataOverTheServersLimitLosesKeysAndNeverTheSample() {
        let writerKeys: [String: JSONValue] = [
            "HKExternalUUID": .string(String(repeating: "x", count: 5000)), "HKWasUserEntered": .bool(true),
            "note": .string(String(repeating: "n", count: 4000)),
        ]
        let ownKeys: [String: JSONValue] = [
            HealthWorkoutMetadata.totalDistanceMeters: .number(6800), HealthWorkoutMetadata.totalEnergyBurnedKilocalories: .number(480),
        ]
        let big = HealthSampleInput(
            healthSampleId: "workout-1", healthType: "workout", startedAt: date("2024-03-01T10:00:00Z"), endedAt: date("2024-03-01T10:40:00Z"),
            workoutActivityType: "running", sampleMetadata: writerKeys.merging(ownKeys) { first, _ in first })
        XCTAssertLessThanOrEqual(HealthJSON.text(big.sampleMetadata).utf8.count, HealthMetadataFit.byteCountMost)
        // The writing app's longest keys go first; the app's own totals and the small key stay.
        XCTAssertNil(big.sampleMetadata["HKExternalUUID"])
        XCTAssertEqual(big.sampleMetadata[HealthWorkoutMetadata.totalDistanceMeters], .number(6800))
        XCTAssertEqual(big.sampleMetadata["HKWasUserEntered"], .bool(true))
        XCTAssertTrue(big.isAcceptable)

        // Metadata that fits is left alone, and a text beyond the limit in an
        // own key goes last of all, not the sample.
        let small: [String: JSONValue] = ["HKWasUserEntered": .bool(false)]
        XCTAssertEqual(HealthMetadataFit.fitting(small), small)
        let onlyOwn = HealthMetadataFit.fitting(["audiogramPoints": .string(String(repeating: "a", count: 20_000))])
        XCTAssertTrue(onlyOwn.isEmpty)
    }

    func testANullCharacterIsTakenOutOfTheTextTheServerWouldRefuse() {
        let sample = HealthSampleInput(
            healthSampleId: "sample-1", healthType: "stepCount", startedAt: date("2024-03-01T10:00:00Z"), endedAt: date("2024-03-01T10:05:00Z"),
            quantityValue: 1, quantityUnit: "count", sourceName: "Wa\u{0}tch", sampleMetadata: ["note": .string("a\u{0}b")])
        XCTAssertEqual(sample.sourceName, "Watch")
        XCTAssertEqual(sample.sampleMetadata["note"], .string("ab"))
        XCTAssertFalse(HealthJSON.text(sample.sampleMetadata).contains("\\u0000"))
    }

    func testABatchTheServerCallsInvalidIsSplitUntilTheOneEntryIsFound() throws {
        let batch = HealthBatch(
            samples: [sample(1), sample(2), sample(3)], deletedHealthSampleIds: ["gone-1"], daySummaries: [summary(1)], routes: [route("sample-1")])
        let apart = try XCTUnwrap(batch.split())
        // The samples and the deleted identifiers first, the rest after, so a
        // route still comes after its workout.
        XCTAssertEqual(apart.first.samples.count, 3)
        XCTAssertEqual(apart.first.deletedHealthSampleIds, ["gone-1"])
        XCTAssertTrue(apart.first.routes.isEmpty)
        XCTAssertEqual(apart.second.daySummaries.count, 1)
        XCTAssertEqual(apart.second.routes.count, 1)
        XCTAssertEqual(apart.first.entryCount + apart.second.entryCount, batch.entryCount)

        let halves = try XCTUnwrap(HealthBatch(samples: [sample(1), sample(2), sample(3)]).split())
        XCTAssertEqual(halves.first.samples.map(\.healthSampleId), ["sample-1"])
        XCTAssertEqual(halves.second.samples.map(\.healthSampleId), ["sample-2", "sample-3"])
        let ids = try XCTUnwrap(HealthBatch(deletedHealthSampleIds: ["a", "b"]).split())
        XCTAssertEqual(ids.first.deletedHealthSampleIds, ["a"])
        XCTAssertEqual(ids.second.deletedHealthSampleIds, ["b"])

        // One entry cannot be split: it is the one that is refused.
        XCTAssertNil(HealthBatch(samples: [sample(1)]).split())
        XCTAssertNil(HealthBatch(routes: [route("workout-1")]).split())
        XCTAssertNil(HealthBatch().split())
    }

    /// Records what a stand-in server was sent.
    private final class Recorder: @unchecked Sendable {
        var calls: [HealthBatch] = []
    }

    func testOneBadSampleIsDroppedAndTheOthersBesideItAreSent() async throws {
        let recorder = Recorder()
        let batch = HealthBatch(samples: (0..<9).map { sample($0) }, daySummaries: [summary(1)])
        let outcome = try await HealthBatchSender.sendSplittingInvalid(batch) { attempt in
            recorder.calls.append(attempt)
            if attempt.samples.contains(where: { $0.healthSampleId == "sample-5" }) {
                throw TeaNodeError.refused("api: invalid arguments: sample 5 is wrong")
            }
            return ReportHealthResult(
                newSampleCount: attempt.samples.count, deletedSampleCount: 0, updatedDayCount: attempt.daySummaries.count,
                healthErasedAt: attempt.samples.isEmpty ? nil : "2024-03-02T00:00:00Z")
        }
        XCTAssertEqual(outcome.droppedEntryCount, 1)
        XCTAssertEqual(outcome.droppedReasons, ["api: invalid arguments: sample 5 is wrong"])
        XCTAssertEqual(outcome.result.newSampleCount, 8)
        XCTAssertEqual(outcome.result.updatedDayCount, 1)
        XCTAssertEqual(outcome.result.healthErasedAt, "2024-03-02T00:00:00Z")
        let accepted = recorder.calls.filter { !$0.samples.contains(where: { $0.healthSampleId == "sample-5" }) }
        XCTAssertEqual(Set(accepted.flatMap(\.samples).map(\.healthSampleId)), Set((0..<9).filter { $0 != 5 }.map { "sample-\($0)" }))
    }

    func testAFailureThatIsNotTheBatchStopsAtOnceAndKeepsEverything() async {
        let recorder = Recorder()
        let batch = HealthBatch(samples: (0..<4).map { sample($0) })
        do {
            _ = try await HealthBatchSender.sendSplittingInvalid(batch) { attempt in
                recorder.calls.append(attempt)
                throw URLError(.notConnectedToInternet)
            }
            XCTFail("should have thrown")
        } catch {
            XCTAssertEqual(HealthReportFailure.classify(error), .retryLater)
        }
        XCTAssertEqual(recorder.calls.count, 1)
    }

    func testEveryDaySummaryCarriesWhenThePhoneWorkedItOut() throws {
        let plain = summary(1)
        XCTAssertNil(plain.workedOutAt)
        let stamped = plain.workedOut(at: date("2024-03-01T08:30:15Z"))
        XCTAssertEqual(stamped.workedOutAt, date("2024-03-01T08:30:15Z"))
        XCTAssertEqual(stamped.sumValue, plain.sumValue)
        XCTAssertEqual(stamped.summaryDay, plain.summaryDay)
        guard case .array(let rows)? = HealthBatch(daySummaries: [stamped]).variables["daySummaries"], case .object(let fields)? = rows.first else {
            return XCTFail("no summary")
        }
        XCTAssertEqual(fields["workedOutAt"], .string("2024-03-01T08:30:15Z"))
        let decoded = try JSONDecoder().decode(HealthDaySummaryInput.self, from: JSONEncoder().encode(stamped))
        XCTAssertEqual(decoded.workedOutAt, stamped.workedOutAt)
    }

    func testTheEntryTheServerNamesIsDroppedAndTheRestIsSentWithoutHalving() async throws {
        let recorder = Recorder()
        let batch = HealthBatch(
            samples: (0..<5).map { sample($0) }, deletedHealthSampleIds: ["gone-1"], daySummaries: [summary(1), summary(2)])
        let outcome = try await HealthBatchSender.sendSplittingInvalid(batch) { attempt in
            recorder.calls.append(attempt)
            if let position = attempt.samples.firstIndex(where: { $0.healthSampleId == "sample-2" }) {
                throw TeaNodeError.refused("api: invalid arguments: samples[\(position)] ends before it starts")
            }
            if attempt.daySummaries.count == 2 {
                throw TeaNodeError.refused("api: invalid arguments: daySummaries[1] is more than a day in the future")
            }
            return ReportHealthResult(newSampleCount: attempt.samples.count, deletedSampleCount: 0, updatedDayCount: attempt.daySummaries.count)
        }
        XCTAssertEqual(outcome.droppedEntryCount, 2)
        XCTAssertEqual(outcome.droppedReasons.count, 2)
        XCTAssertEqual(outcome.result.newSampleCount, 4)
        XCTAssertEqual(outcome.result.updatedDayCount, 1)
        // The whole, then without the sample, then without the summary: no halving.
        XCTAssertEqual(recorder.calls.count, 3)
        XCTAssertEqual(recorder.calls.last?.samples.count, 4)
        XCTAssertEqual(recorder.calls.last?.deletedHealthSampleIds, ["gone-1"])
    }

    func testAReasonThatNamesNoEntryTheBatchHasFallsBackToHalving() {
        let batch = HealthBatch(samples: [sample(1), sample(2)], routes: [route("workout-1")])
        XCTAssertNil(batch.removingEntry(named: "api: invalid arguments: send at most 1000 samples at a time"))
        XCTAssertNil(batch.removingEntry(named: "api: invalid arguments: samples[7] ends before it starts"))
        XCTAssertNil(batch.removingEntry(named: "api: invalid arguments: samples[x] is wrong"))
        XCTAssertEqual(batch.removingEntry(named: "invalid arguments: samples[0] is wrong")?.samples.map(\.healthSampleId), ["sample-2"])
        XCTAssertEqual(batch.removingEntry(named: "invalid arguments: routes[0] is wrong")?.routes.count, 0)
        XCTAssertEqual(
            HealthBatch(characteristics: [HealthCharacteristicInput(healthType: "bloodType", characteristicValue: "x")])
                .removingEntry(named: "invalid arguments: characteristics[0] is wrong")?.isEmpty,
            true)
    }

    func testAFaultOfTheCallItselfIsNotMendedByHalvingTheBatch() async {
        let recorder = Recorder()
        let batch = HealthBatch(samples: (0..<4).map { sample($0) })
        do {
            _ = try await HealthBatchSender.sendSplittingInvalid(batch) { attempt in
                recorder.calls.append(attempt)
                throw TeaNodeError.refused("api: invalid arguments: healthErasedAt is not a time")
            }
            XCTFail("should have thrown")
        } catch {
            XCTAssertEqual(HealthReportFailure.classify(error), .batchInvalid("api: invalid arguments: healthErasedAt is not a time"))
        }
        // Sent once, whole: nothing was dropped.
        XCTAssertEqual(recorder.calls.count, 1)
        XCTAssertEqual(recorder.calls.first?.samples.count, 4)
    }

    func testAFailedBatchIsRetriedOnlyForACallThatDidNotGetThrough() {
        XCTAssertEqual(HealthReportFailure.classify(TeaNodeError.refused("api: permission denied")), .refused("api: permission denied"))
        XCTAssertEqual(
            HealthReportFailure.classify(TeaNodeError.refused("api: invalid arguments: bad sample")), .batchInvalid("api: invalid arguments: bad sample"))
        XCTAssertEqual(HealthReportFailure.classify(TeaNodeError.refused("api: not found")), .phoneNotFound)
        XCTAssertEqual(HealthReportFailure.classify(TeaNodeError.refused("db: not found")), .phoneNotFound)
        XCTAssertEqual(HealthReportFailure.classify(TeaNodeError.refused("something went wrong")), .retryLater)
        XCTAssertEqual(HealthReportFailure.classify(TeaNodeError.signedOut), .retryLater)
        XCTAssertEqual(HealthReportFailure.classify(URLError(.notConnectedToInternet)), .retryLater)
    }

    func testEventsWaitHalfAMinuteAfterAFailureButNotAfterASuccess() {
        var gate = HealthRetryGate()
        let start = date("2024-03-01T10:00:00Z")
        XCTAssertTrue(gate.canTry(at: start))
        gate.didFail(at: start)
        XCTAssertFalse(gate.canTry(at: start.addingTimeInterval(29)))
        XCTAssertTrue(gate.canTry(at: start.addingTimeInterval(30)))
        gate.didFail(at: start)
        gate.didSucceed()
        XCTAssertTrue(gate.canTry(at: start))
    }

    // MARK: The types

    func testEveryTypeIsListedOnceWithAUnitOnlyWhenItIsAQuantity() {
        let identifiers = HealthTypes.all.map(\.healthType)
        XCTAssertEqual(Set(identifiers).count, identifiers.count)
        for info in HealthTypes.all {
            XCTAssertEqual(info.valueKind == .quantity, !info.quantityUnit.isEmpty, info.healthType)
            XCTAssertEqual(info.valueKind == .characteristic, info.summaryKind == nil, info.healthType)
            XCTAssertFalse(info.typeName.isEmpty, info.healthType)
        }
        for healthCategory in HealthCategory.allCases {
            XCTAssertFalse(HealthTypes.types(in: healthCategory).isEmpty, healthCategory.rawValue)
        }
    }

    func testTheUnitsAreOnesHealthKitKnows() {
        let known: Set<String> = [
            "count", "count/min", "m", "m/s", "cm", "kg", "kcal", "min", "ms", "%", "degC", "mg/dL", "mmHg", "ml/(kg*min)", "W", "dBASPL", "S",
            "L", "L/min", "IU", "g", "mg", "mcg", "mL", "kcal/(kg*hr)",
        ]
        for info in HealthTypes.types(of: .quantity) {
            XCTAssertTrue(known.contains(info.quantityUnit), "\(info.healthType): \(info.quantityUnit)")
            XCTAssertTrue(info.summaryKind == .sum || info.summaryKind == .average, info.healthType)
        }
    }

    func testTheCommonTypesAreWhereTheHealthAppPutsThem() throws {
        XCTAssertEqual(HealthTypes.info(for: "stepCount")?.healthCategory, .activity)
        XCTAssertEqual(HealthTypes.info(for: "stepCount")?.summaryKind, .sum)
        XCTAssertEqual(HealthTypes.info(for: "heartRate")?.quantityUnit, "count/min")
        XCTAssertEqual(HealthTypes.info(for: "heartRate")?.summaryKind, .average)
        XCTAssertEqual(HealthTypes.info(for: "bodyMass")?.healthCategory, .bodyMeasurements)
        XCTAssertEqual(HealthTypes.info(for: "sleepAnalysis")?.valueKind, .category)
        XCTAssertEqual(HealthTypes.info(for: "sleepAnalysis")?.summaryKind, .duration)
        XCTAssertEqual(HealthTypes.info(for: "workout")?.valueKind, .workout)
        XCTAssertEqual(HealthTypes.info(for: "headache")?.healthCategory, .symptoms)
        XCTAssertEqual(HealthTypes.info(for: "dateOfBirth")?.valueKind, .characteristic)
        XCTAssertNil(HealthTypes.info(for: "notAType"))
    }

    func testHealthKitsNameForATypeIsItsPrefixAndTheCapitalizedIdentifier() throws {
        XCTAssertEqual(HealthTypes.info(for: "stepCount")?.healthKitIdentifier, "HKQuantityTypeIdentifierStepCount")
        XCTAssertEqual(HealthTypes.info(for: "vo2Max")?.healthKitIdentifier, "HKQuantityTypeIdentifierVO2Max")
        XCTAssertEqual(HealthTypes.info(for: "uvExposure")?.healthKitIdentifier, "HKQuantityTypeIdentifierUVExposure")
        XCTAssertEqual(HealthTypes.info(for: "sleepAnalysis")?.healthKitIdentifier, "HKCategoryTypeIdentifierSleepAnalysis")
        XCTAssertEqual(HealthTypes.info(for: "bloodType")?.healthKitIdentifier, "HKCharacteristicTypeIdentifierBloodType")
        XCTAssertNil(HealthTypes.info(for: "workout")?.healthKitIdentifier)
        XCTAssertNil(HealthTypes.info(for: "electrocardiogram")?.healthKitIdentifier)
    }

    func testATypeNotInTheTableIsNamedFromItsIdentifier() {
        XCTAssertEqual(HealthTypes.typeName(for: "someNewMeasure"), "Some new measure")
        XCTAssertEqual(HealthTypes.typeName(for: "heartRateVariabilitySDNN"), "Heart rate variability")
        XCTAssertEqual(HealthTypes.words(from: "heartRateVariabilitySDNN"), "Heart rate variability SDNN")
        XCTAssertEqual(HealthTypes.words(from: "someSDNNValue"), "Some SDNN value")
        XCTAssertEqual(HealthTypes.words(from: "stepCount"), "Step count")
        XCTAssertEqual(HealthTypes.typeName(for: "stepCount"), "Steps")
    }

    // MARK: Days

    func testADayIsWrittenOnTheCalendarsClock() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let losAngeles = try calendar("America/Los_Angeles")
        let moment = date("2024-03-01T20:30:00Z")
        XCTAssertEqual(HealthDays.text(of: moment, calendar: tokyo), "2024-03-02")
        XCTAssertEqual(HealthDays.text(of: moment, calendar: losAngeles), "2024-03-01")
        XCTAssertTrue(HealthDays.isDayText("2024-03-01"))
        XCTAssertFalse(HealthDays.isDayText("2024-3-1"))
        XCTAssertFalse(HealthDays.isDayText("2024-13-01"))
        XCTAssertFalse(HealthDays.isDayText("2024/03/01"))
        XCTAssertFalse(HealthDays.isDayText(""))
    }

    func testTheDayWhenTheClocksChangeIsNot24Hours() throws {
        let losAngeles = try calendar("America/Los_Angeles")
        XCTAssertEqual(HealthDays.interval(ofDay: "2026-03-08", calendar: losAngeles)?.duration, 23 * 3600)
        XCTAssertEqual(HealthDays.interval(ofDay: "2026-11-01", calendar: losAngeles)?.duration, 25 * 3600)
        XCTAssertEqual(HealthDays.interval(ofDay: "2026-06-01", calendar: losAngeles)?.duration, 24 * 3600)
    }

    func testAReadingEndingAtMidnightDoesNotTouchTheNextDay() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let start = try moment(tokyo, 2024, 3, 1, 22)
        XCTAssertEqual(HealthDays.days(from: start, to: try moment(tokyo, 2024, 3, 2, 0), calendar: tokyo), ["2024-03-01"])
        XCTAssertEqual(HealthDays.days(from: start, to: try moment(tokyo, 2024, 3, 2, 1), calendar: tokyo), ["2024-03-01", "2024-03-02"])
        XCTAssertEqual(HealthDays.days(from: start, to: start, calendar: tokyo), ["2024-03-01"])
    }

    // MARK: Sleep

    private func sleepSample(_ id: String, _ stage: HealthSleepStage, _ start: Date, _ end: Date) -> HealthCategorySample {
        HealthCategorySample(healthSampleId: id, startedAt: start, endedAt: end, categoryValue: stage.rawValue)
    }

    func testTwoSourcesSleepingOverlappedAreCountedOnce() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let phone = sleepSample("phone", .asleepUnspecified, try moment(tokyo, 2024, 3, 1, 23), try moment(tokyo, 2024, 3, 1, 23, 59))
        // A watch reports the stages for the same night, starting 15 minutes
        // later and ending 15 minutes earlier.
        let core = sleepSample("core", .asleepCore, try moment(tokyo, 2024, 3, 1, 23, 15), try moment(tokyo, 2024, 3, 1, 23, 40))
        let deep = sleepSample("deep", .asleepDeep, try moment(tokyo, 2024, 3, 1, 23, 40), try moment(tokyo, 2024, 3, 1, 23, 45))
        let summaries = HealthDaySummaries.sleepSummaries([phone, core, deep], calendar: tokyo)
        XCTAssertEqual(summaries.count, 1)
        let night = summaries[0]
        XCTAssertEqual(night.healthType, "sleepAnalysis")
        XCTAssertEqual(night.summaryDay, "2024-03-01")
        // 59 minutes of phone sleep covers every watch stage inside it.
        XCTAssertEqual(night.categorySeconds["asleep"], 59 * 60)
        XCTAssertEqual(night.durationSeconds, 59 * 60)
        XCTAssertEqual(night.categorySeconds["asleepUnspecified"], 59 * 60)
        XCTAssertEqual(night.categorySeconds["asleepCore"], 25 * 60)
        XCTAssertEqual(night.categorySeconds["asleepDeep"], 5 * 60)
        XCTAssertNil(night.categorySeconds["asleepREM"])
        XCTAssertEqual(night.sampleCount, 3)
    }

    func testTimeInBedAndAwakeAreNotSleep() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let samples = [
            sleepSample("bed", .inBed, try moment(tokyo, 2024, 3, 1, 22), try moment(tokyo, 2024, 3, 1, 23, 30)),
            sleepSample("rem", .asleepREM, try moment(tokyo, 2024, 3, 1, 23), try moment(tokyo, 2024, 3, 1, 23, 20)),
            sleepSample("awake", .awake, try moment(tokyo, 2024, 3, 1, 23, 20), try moment(tokyo, 2024, 3, 1, 23, 30)),
            // A value HealthKit may add later still counts as a sample.
            HealthCategorySample(healthSampleId: "future", startedAt: try moment(tokyo, 2024, 3, 1, 23), endedAt: try moment(tokyo, 2024, 3, 1, 23, 1), categoryValue: 9),
        ]
        let night = try XCTUnwrap(HealthDaySummaries.sleepSummaries(samples, calendar: tokyo).first)
        XCTAssertEqual(night.categorySeconds["inBed"], 90 * 60)
        XCTAssertEqual(night.categorySeconds["asleep"], 20 * 60)
        XCTAssertEqual(night.categorySeconds["awake"], 10 * 60)
        XCTAssertEqual(night.durationSeconds, 20 * 60)
        XCTAssertEqual(night.sampleCount, 4)
    }

    func testANightIsSplitAtMidnightByTheCalendarsDay() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let night = sleepSample("night", .asleepCore, try moment(tokyo, 2024, 3, 1, 23), try moment(tokyo, 2024, 3, 2, 7))
        let summaries = HealthDaySummaries.sleepSummaries([night], calendar: tokyo)
        XCTAssertEqual(summaries.map(\.summaryDay), ["2024-03-01", "2024-03-02"])
        XCTAssertEqual(summaries.map { $0.categorySeconds["asleep"] }, [3600.0, 7.0 * 3600])
        XCTAssertEqual(summaries.map(\.sampleCount), [1, 1])
        // The same night on another calendar is cut elsewhere: it is 6 am to
        // 2 pm on the 1st in Los Angeles, which is one day.
        let losAngeles = try calendar("America/Los_Angeles")
        let sameNight = HealthDaySummaries.sleepSummaries([night], calendar: losAngeles)
        XCTAssertEqual(sameNight.map(\.summaryDay), ["2024-03-01"])
        XCTAssertEqual(sameNight.first?.categorySeconds["asleep"], 8 * 3600)
    }

    func testANightWhenTheClocksGoBackHasTheExtraHourOnTheSecondDay() throws {
        let losAngeles = try calendar("America/Los_Angeles")
        // 2026-11-01 is the day the clocks go back: 11 pm on the 31st to 7 am
        // by the new clock is nine hours, not eight.
        let start = try moment(losAngeles, 2026, 10, 31, 23)
        let end = try moment(losAngeles, 2026, 11, 1, 7)
        XCTAssertEqual(end.timeIntervalSince(start), 9 * 3600)
        let summaries = HealthDaySummaries.sleepSummaries([sleepSample("night", .asleepCore, start, end)], calendar: losAngeles)
        XCTAssertEqual(summaries.map(\.summaryDay), ["2026-10-31", "2026-11-01"])
        XCTAssertEqual(summaries[0].categorySeconds["asleep"], 3600)
        XCTAssertEqual(summaries[1].categorySeconds["asleep"], 8 * 3600)
        XCTAssertEqual(summaries.compactMap(\.durationSeconds).reduce(0, +), 9 * 3600)
    }

    func testANightWhenTheClocksGoForwardLosesAnHour() throws {
        let losAngeles = try calendar("America/Los_Angeles")
        let start = try moment(losAngeles, 2026, 3, 7, 23)
        let end = try moment(losAngeles, 2026, 3, 8, 7)
        XCTAssertEqual(end.timeIntervalSince(start), 7 * 3600)
        let summaries = HealthDaySummaries.sleepSummaries([sleepSample("night", .asleepCore, start, end)], calendar: losAngeles)
        XCTAssertEqual(summaries.map { $0.categorySeconds["asleep"] }, [3600.0, 6.0 * 3600])
    }

    func testOnlyTheDaysAskedForAreWorkedOut() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let night = sleepSample("night", .asleepCore, try moment(tokyo, 2024, 3, 1, 23), try moment(tokyo, 2024, 3, 2, 7))
        let summaries = HealthDaySummaries.categorySummaries(healthType: "sleepAnalysis", samples: [night], calendar: tokyo, days: ["2024-03-02"])
        XCTAssertEqual(summaries.map(\.summaryDay), ["2024-03-02"])
        XCTAssertEqual(summaries.first?.categorySeconds["asleep"], 7 * 3600)
    }

    func testASampleReadTwiceIsCountedOnce() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let night = sleepSample("night", .asleepCore, try moment(tokyo, 2024, 3, 1, 1), try moment(tokyo, 2024, 3, 1, 2))
        let summary = try XCTUnwrap(HealthDaySummaries.sleepSummaries([night, night], calendar: tokyo).first)
        XCTAssertEqual(summary.sampleCount, 1)
        XCTAssertEqual(summary.durationSeconds, 3600)
    }

    // MARK: Other categories

    func testMindfulMinutesOverlappingAreCountedOnce() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let samples = [
            HealthCategorySample(healthSampleId: "a", startedAt: try moment(tokyo, 2024, 3, 1, 8), endedAt: try moment(tokyo, 2024, 3, 1, 8, 10), categoryValue: 0),
            HealthCategorySample(healthSampleId: "b", startedAt: try moment(tokyo, 2024, 3, 1, 8, 5), endedAt: try moment(tokyo, 2024, 3, 1, 8, 20), categoryValue: 0),
            HealthCategorySample(healthSampleId: "c", startedAt: try moment(tokyo, 2024, 3, 1, 18), endedAt: try moment(tokyo, 2024, 3, 1, 18, 5), categoryValue: 0),
        ]
        let summaries = HealthDaySummaries.categorySummaries(healthType: "mindfulSession", samples: samples, calendar: tokyo)
        XCTAssertEqual(summaries.count, 1)
        XCTAssertEqual(summaries[0].durationSeconds, 25 * 60)
        XCTAssertEqual(summaries[0].sampleCount, 3)
        XCTAssertNil(summaries[0].sumValue)
    }

    func testEventsAndSymptomsAreCountedByTheDayTheyBegan() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let samples = [
            HealthCategorySample(healthSampleId: "a", startedAt: try moment(tokyo, 2024, 3, 1, 8), endedAt: try moment(tokyo, 2024, 3, 1, 9), categoryValue: 2),
            HealthCategorySample(healthSampleId: "b", startedAt: try moment(tokyo, 2024, 3, 1, 23), endedAt: try moment(tokyo, 2024, 3, 2, 2), categoryValue: 3),
            HealthCategorySample(healthSampleId: "c", startedAt: try moment(tokyo, 2024, 3, 2, 8), endedAt: try moment(tokyo, 2024, 3, 2, 8), categoryValue: 1),
        ]
        let summaries = HealthDaySummaries.categorySummaries(healthType: "headache", samples: samples, calendar: tokyo)
        XCTAssertEqual(summaries.map(\.summaryDay), ["2024-03-01", "2024-03-02"])
        XCTAssertEqual(summaries.map(\.sampleCount), [2, 1])
        XCTAssertNil(summaries[0].durationSeconds)
    }

    // MARK: Workouts

    func testWorkoutsAddUpByTheDayTheyBegan() throws {
        let tokyo = try calendar("Asia/Tokyo")
        let workouts = [
            HealthWorkoutSample(
                healthSampleId: "run", startedAt: try moment(tokyo, 2024, 3, 1, 7), endedAt: try moment(tokyo, 2024, 3, 1, 7, 30),
                totalEnergyKilocalories: 300, totalDistanceMeters: 5000),
            HealthWorkoutSample(
                healthSampleId: "swim", startedAt: try moment(tokyo, 2024, 3, 1, 18), endedAt: try moment(tokyo, 2024, 3, 1, 18, 45),
                totalEnergyKilocalories: 250),
            // Begins before midnight and ends after: all on the first day.
            HealthWorkoutSample(
                healthSampleId: "late", startedAt: try moment(tokyo, 2024, 3, 1, 23, 30), endedAt: try moment(tokyo, 2024, 3, 2, 0, 30)),
            HealthWorkoutSample(
                healthSampleId: "walk", startedAt: try moment(tokyo, 2024, 3, 2, 9), endedAt: try moment(tokyo, 2024, 3, 2, 9, 20),
                totalDistanceMeters: 1500),
        ]
        let summaries = HealthDaySummaries.workoutSummaries(workouts + [workouts[0]], calendar: tokyo)
        XCTAssertEqual(summaries.map(\.summaryDay), ["2024-03-01", "2024-03-02"])
        XCTAssertTrue(summaries.allSatisfy { $0.healthType == "workout" })
        XCTAssertEqual(summaries[0].sampleCount, 3)
        XCTAssertEqual(summaries[0].durationSeconds, (30 + 45 + 60) * 60)
        XCTAssertEqual(summaries[0].categorySeconds["energyKilocalories"], 550)
        XCTAssertEqual(summaries[0].categorySeconds["distanceMeters"], 5000)
        XCTAssertEqual(summaries[1].sampleCount, 1)
        XCTAssertEqual(summaries[1].durationSeconds, 20 * 60)
        XCTAssertNil(summaries[1].categorySeconds["energyKilocalories"])
        XCTAssertEqual(summaries[1].categorySeconds["distanceMeters"], 1500)
    }

    func testAWorkoutSampleCarriesItsTotalsInItsMetadata() throws {
        let sample = HealthSampleInput(
            healthSampleId: "run", healthType: "workout", startedAt: date("2024-03-01T07:00:00Z"), endedAt: date("2024-03-01T07:30:00Z"),
            workoutActivityType: "running",
            sampleMetadata: [
                HealthWorkoutMetadata.totalEnergyBurnedKilocalories: .number(300), HealthWorkoutMetadata.totalDistanceMeters: .number(5000),
            ])
        let workout = try XCTUnwrap(HealthWorkoutSample(sample))
        XCTAssertEqual(workout.totalEnergyKilocalories, 300)
        XCTAssertEqual(workout.totalDistanceMeters, 5000)
        XCTAssertNil(HealthWorkoutSample(self.sample(1)))
        XCTAssertNil(HealthCategorySample(self.sample(1)))
    }

    // MARK: Quantities

    func testAQuantitysSummaryKeepsOnlyTheNumbersThatMeanSomething() {
        let statistics = HealthQuantityStatistics(sumValue: 9000, averageValue: 750, minimumValue: 1, maximumValue: 2000, sampleCount: 12)
        let steps = HealthDaySummaries.quantitySummary(healthType: "stepCount", summaryDay: "2024-03-01", statistics: statistics)
        XCTAssertEqual(steps.quantityUnit, "count")
        XCTAssertEqual(steps.sumValue, 9000)
        XCTAssertNil(steps.averageValue)
        XCTAssertNil(steps.maximumValue)
        let heart = HealthDaySummaries.quantitySummary(healthType: "heartRate", summaryDay: "2024-03-01", statistics: statistics)
        XCTAssertEqual(heart.quantityUnit, "count/min")
        XCTAssertNil(heart.sumValue)
        XCTAssertEqual(heart.averageValue, 750)
        XCTAssertEqual(heart.minimumValue, 1)
        XCTAssertEqual(heart.maximumValue, 2000)
        XCTAssertEqual(heart.sampleCount, 12)
        XCTAssertTrue(heart.isAcceptable)
    }

    // MARK: What the phone remembers

    func testTheStateReadsBackFromItsFile() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("health-state-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: directory) }
        let tokyo = try calendar("Asia/Tokyo")
        var state = HealthSyncState(accountKey: "https://mail.example.com|robin")
        state.turnOn(healthErasedAt: "2024-02-01T00:00:00.5Z")
        state.setAnchor(Data([1, 2, 3, 250]), for: "stepCount")
        state.setAnchor(Data([9]), for: "heartRate")
        state.markPending(healthType: "sleepAnalysis", from: try moment(tokyo, 2024, 3, 1, 23), to: try moment(tokyo, 2024, 3, 2, 7), calendar: tokyo)
        state.recordSent(
            HealthBatch(samples: [sample(1), sample(2)], daySummaries: [summary(1)], routes: [route("workout-1")]),
            result: ReportHealthResult(newSampleCount: 2, deletedSampleCount: 3, updatedDayCount: 1), at: date("2024-03-01T01:00:00Z"))

        try state.save(inDirectory: directory)
        let url = directory.appendingPathComponent(HealthSyncState.fileName(accountKey: "https://mail.example.com|robin"))
        XCTAssertTrue(FileManager.default.fileExists(atPath: url.path))
        XCTAssertFalse(url.lastPathComponent.contains("/"))
        XCTAssertFalse(url.lastPathComponent.contains("|"))

        let loaded = HealthSyncState.load(accountKey: "https://mail.example.com|robin", inDirectory: directory)
        XCTAssertEqual(loaded, state)
        XCTAssertEqual(loaded.anchor(for: "stepCount"), Data([1, 2, 3, 250]))
        XCTAssertNil(loaded.anchor(for: "bodyMass"))
        XCTAssertEqual(loaded.pendingDays(for: "sleepAnalysis"), ["2024-03-01", "2024-03-02"])
        XCTAssertEqual(loaded.sentSampleCount, 2)
        XCTAssertEqual(loaded.sentDeletedSampleCount, 3)
        XCTAssertEqual(loaded.sentDaySummaryCount, 1)
        XCTAssertEqual(loaded.sentRouteCount, 1)
        XCTAssertEqual(loaded.lastSentAt, date("2024-03-01T01:00:00Z"))
        XCTAssertTrue(loaded.isTurnedOn)
        XCTAssertEqual(loaded.healthErasedAt, "2024-02-01T00:00:00.5Z")

        // The same bytes every time, so the file does not churn.
        XCTAssertEqual(try loaded.encoded(), try state.encoded())
    }

    func testAnotherAccountOrAMissingOrBrokenFileStartsClean() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("health-state-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: directory) }
        XCTAssertEqual(HealthSyncState.load(accountKey: "account-a", inDirectory: directory), HealthSyncState(accountKey: "account-a"))
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: "2024-02-01T00:00:00.5Z")
        try state.save(inDirectory: directory)
        XCTAssertNotEqual(HealthSyncState.load(accountKey: "account-a", inDirectory: directory), HealthSyncState(accountKey: "account-a"))
        XCTAssertEqual(HealthSyncState.load(accountKey: "account-b", inDirectory: directory), HealthSyncState(accountKey: "account-b"))
        XCTAssertNotEqual(HealthSyncState.fileName(accountKey: "account-a"), HealthSyncState.fileName(accountKey: "account-b"))
        try Data("not json".utf8).write(to: directory.appendingPathComponent(HealthSyncState.fileName(accountKey: "account-a")))
        XCTAssertEqual(HealthSyncState.load(accountKey: "account-a", inDirectory: directory), HealthSyncState(accountKey: "account-a"))
    }

    func testTheAnswerToABatchIsTakenOnlyWhenTheServerStoredIt() {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: "2024-03-01T00:00:00Z")
        state.setAnchor(Data([1]), for: "stepCount")
        let batch = HealthBatch(samples: [sample(1), sample(2)])

        // Taken: counted by what was new to the server, and the caller may move anchors.
        XCTAssertTrue(
            state.accept(
                ReportHealthResult(newSampleCount: 1, deletedSampleCount: 0, updatedDayCount: 0, healthErasedAt: "2024-03-01T00:00:00Z"), for: batch,
                at: date("2024-03-01T01:00:00Z")))
        XCTAssertEqual(state.sentSampleCount, 1)
        XCTAssertEqual(state.lastSentAt, date("2024-03-01T01:00:00Z"))

        // The server stored nothing because the person erased everything after
        // the erase the phone knew of: nothing is counted, the anchors are
        // forgotten and Health is off, with the reason kept for the settings.
        let erased = ReportHealthResult(newSampleCount: 0, deletedSampleCount: 0, updatedDayCount: 0, healthErasedAt: "2024-03-02T00:00:00Z")
        XCTAssertFalse(state.accept(erased, for: batch, at: date("2024-03-02T01:00:00Z")))
        XCTAssertFalse(state.isTurnedOn)
        XCTAssertTrue(state.isStoppedByErase)
        XCTAssertTrue(state.anchors.isEmpty)
        XCTAssertEqual(state.sentSampleCount, 0)
        XCTAssertNil(state.lastSentAt)
        XCTAssertNil(state.healthErasedAt)

        // Turned on again, having read that erase from the server: it is not news.
        state.turnOn(healthErasedAt: "2024-03-02T00:00:00Z")
        XCTAssertFalse(state.isStoppedByErase)
        XCTAssertTrue(state.accept(erased, for: batch, at: date("2024-03-03T01:00:00Z")))
        XCTAssertTrue(state.isTurnedOn)
    }

    func testTurningOnKeepsTheEraseTimeExactlyAsTheServerWroteItAndTheFileAgrees() throws {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: "2024-03-01T08:30:15.123456789+09:00")
        XCTAssertEqual(state.healthErasedAt, "2024-03-01T08:30:15.123456789+09:00")
        let loaded = try HealthSyncState.decoded(from: try state.encoded())
        XCTAssertEqual(loaded.healthErasedAt, state.healthErasedAt)
        XCTAssertTrue(loaded.isTurnedOn)
        // Turning on again changes nothing.
        state.turnOn(healthErasedAt: nil)
        XCTAssertEqual(state.healthErasedAt, "2024-03-01T08:30:15.123456789+09:00")
        // A person who never erased: nothing is known, and nothing is sent.
        var never = HealthSyncState(accountKey: "account-a")
        never.turnOn(healthErasedAt: nil)
        XCTAssertNil(never.healthErasedAt)
        XCTAssertTrue(never.isTurnedOn)
    }

    func testTheStoppedByEraseNoticeSurvivesTheFileAndIsClearedByTurningOffOrOn() throws {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: nil)
        XCTAssertTrue(state.applyServerErase(healthErasedAt: "2024-03-02T00:00:00Z"))
        XCTAssertTrue(try HealthSyncState.decoded(from: try state.encoded()).isStoppedByErase)
        state.turnOff()
        XCTAssertFalse(state.isStoppedByErase)
        XCTAssertTrue(state.applyServerErase(healthErasedAt: "2024-03-02T00:00:00Z") == false, "off: nothing to stop")
    }

    func testTheRoutesSentAndTheHistoryAreKeptInTheFileAndForgottenOnReset() throws {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: nil)
        XCTAssertFalse(state.hasSentRoute(healthSampleId: "workout-1"))
        state.recordRouteSent(healthSampleId: "workout-1")
        state.recordRouteSent(healthSampleId: "workout-1")
        XCTAssertEqual(state.sentRouteHealthSampleIds, ["workout-1"])
        state.markHistoryDone()
        let loaded = try HealthSyncState.decoded(from: try state.encoded())
        XCTAssertTrue(loaded.hasSentRoute(healthSampleId: "workout-1"))
        XCTAssertTrue(loaded.isHistoryDone)
        // Only the latest are remembered.
        for number in 0..<(HealthSyncState.sentRouteCountMost + 5) { state.recordRouteSent(healthSampleId: "run-\(number)") }
        XCTAssertEqual(state.sentRouteHealthSampleIds.count, HealthSyncState.sentRouteCountMost)
        XCTAssertEqual(state.sentRouteHealthSampleIds.last, "run-\(HealthSyncState.sentRouteCountMost + 4)")
        state.turnOff()
        XCTAssertFalse(state.isHistoryDone)
        XCTAssertTrue(state.sentRouteHealthSampleIds.isEmpty)
    }

    func testTheAccountsHealthIsOnForAreFoundFromTheirFiles() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("health-state-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: directory) }
        XCTAssertTrue(HealthSyncState.accountKeysTurnedOn(inDirectory: directory).isEmpty)
        var on = HealthSyncState(accountKey: "https://mail.example.com|robin")
        on.turnOn(healthErasedAt: nil)
        try on.save(inDirectory: directory)
        try HealthSyncState(accountKey: "account-off").save(inDirectory: directory)
        try Data("not json".utf8).write(to: directory.appendingPathComponent("health-broken.json"))
        XCTAssertEqual(HealthSyncState.accountKeysTurnedOn(inDirectory: directory), ["https://mail.example.com|robin"])
        HealthSyncState.remove(accountKey: "https://mail.example.com|robin", inDirectory: directory)
        XCTAssertTrue(HealthSyncState.accountKeysTurnedOn(inDirectory: directory).isEmpty)
    }

    func testAnEraseLaterThanTheOneThePhoneKnowsOfResetsTheStateAndTurnsHealthOff() {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: "2024-02-01T00:00:00Z")
        state.setAnchor(Data([1]), for: "stepCount")
        state.markPending(HealthPendingDay(healthType: "stepCount", day: "2024-03-01"))
        state.recordSent(HealthBatch(samples: [sample(1)]), result: .nothing, at: date("2024-03-01T01:00:00Z"))

        // No erase at all, the one the phone knows of, and an older one change nothing.
        XCTAssertFalse(state.applyServerErase(healthErasedAt: nil))
        XCTAssertFalse(state.applyServerErase(healthErasedAt: "2024-02-01T00:00:00Z"))
        XCTAssertFalse(state.applyServerErase(healthErasedAt: "2024-01-01T00:00:00Z"))
        XCTAssertTrue(state.isTurnedOn)
        XCTAssertFalse(state.isStoppedByErase)
        XCTAssertNotNil(state.anchor(for: "stepCount"))

        XCTAssertTrue(state.applyServerErase(healthErasedAt: "2024-03-02T00:00:00Z"))
        XCTAssertFalse(state.isTurnedOn)
        XCTAssertTrue(state.isStoppedByErase)
        XCTAssertTrue(state.anchors.isEmpty)
        XCTAssertTrue(state.pendingDays.isEmpty)
        XCTAssertEqual(state.sentSampleCount, 0)
        XCTAssertNil(state.lastSentAt)
        XCTAssertEqual(state.accountKey, "account-a")

        // Already off: nothing to reset.
        XCTAssertFalse(state.applyServerErase(healthErasedAt: "2024-03-09T00:00:00Z"))
    }

    func testAnyEraseIsNewsToAPhoneThatKnowsOfNoneAndTheFractionOfASecondDecides() {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: nil)
        XCTAssertTrue(state.isErasedLater(than: "2024-03-02T00:00:00Z"))
        XCTAssertFalse(state.isErasedLater(than: nil))

        // The server compares to the fraction, so the phone must too: the
        // erase it was told of is not later than itself, a later fraction is.
        var knowing = HealthSyncState(accountKey: "account-a")
        knowing.turnOn(healthErasedAt: "2024-03-02T00:00:00.250000000Z")
        XCTAssertFalse(knowing.isErasedLater(than: "2024-03-02T00:00:00.25Z"))
        XCTAssertFalse(knowing.isErasedLater(than: "2024-03-02T00:00:00.2Z"))
        XCTAssertTrue(knowing.isErasedLater(than: "2024-03-02T00:00:00.250000001Z"))
        XCTAssertTrue(knowing.isErasedLater(than: "2024-03-02T00:00:01Z"))
        // Another offset for the same moment is the same moment.
        XCTAssertFalse(knowing.isErasedLater(than: "2024-03-02T09:00:00.25+09:00"))
        XCTAssertTrue(ServerTime.isLater("2024-03-02T09:00:00.5+09:00", than: "2024-03-02T00:00:00.25Z"))
        XCTAssertFalse(ServerTime.isLater("not a time", than: "2024-03-02T00:00:00Z"))
        XCTAssertTrue(ServerTime.isLater("2024-03-02T00:00:00Z", than: "not a time"))
    }

    func testEntriesTheServerRefusedOneByOneAreCountedUntilHealthIsTurnedOff() throws {
        var state = HealthSyncState(accountKey: "account-a")
        state.turnOn(healthErasedAt: nil)
        state.recordDropped(entryCount: 2)
        state.recordDropped(entryCount: 0)
        state.recordDropped(entryCount: -4)
        XCTAssertEqual(state.droppedEntryCount, 2)
        XCTAssertEqual(try HealthSyncState.decoded(from: try state.encoded()).droppedEntryCount, 2)
        state.turnOff()
        XCTAssertEqual(state.droppedEntryCount, 0)
    }

    func testTheLaterEraseOfTwoAnswersIsKeptToTheFraction() {
        let early = ReportHealthResult(newSampleCount: 1, deletedSampleCount: 0, updatedDayCount: 0, healthErasedAt: "2024-03-02T00:00:00.1Z")
        let late = ReportHealthResult(newSampleCount: 1, deletedSampleCount: 0, updatedDayCount: 0, healthErasedAt: "2024-03-02T00:00:00.9Z")
        XCTAssertEqual(early.merged(with: late).healthErasedAt, "2024-03-02T00:00:00.9Z")
        XCTAssertEqual(late.merged(with: early).healthErasedAt, "2024-03-02T00:00:00.9Z")
        XCTAssertEqual(ReportHealthResult.nothing.merged(with: early).healthErasedAt, "2024-03-02T00:00:00.1Z")
        XCTAssertEqual(early.merged(with: .nothing).healthErasedAt, "2024-03-02T00:00:00.1Z")
        XCTAssertEqual(early.merged(with: late).newSampleCount, 2)
    }

    func testTheDaysOfDeletedSamplesWaitToBeWorkedOutAgain() throws {
        let tokyo = try calendar("Asia/Tokyo")
        var state = HealthSyncState(accountKey: "account-a")
        state.markPending(
            deletedSamples: [
                HealthDeletedSample(healthType: "stepCount", startedAt: "2024-03-01T10:00:00Z", endedAt: "2024-03-01T10:05:00Z"),
                HealthDeletedSample(healthType: "sleepAnalysis", startedAt: "2024-03-01T14:00:00Z", endedAt: "2024-03-01T22:00:00Z"),
                HealthDeletedSample(healthType: "stepCount", startedAt: "not a time", endedAt: "2024-03-01T10:05:00Z"),
            ], calendar: tokyo)
        XCTAssertEqual(state.pendingDays(for: "stepCount"), ["2024-03-01"])
        // 23:00 on the 1st to 07:00 on the 2nd in Tokyo.
        XCTAssertEqual(state.pendingDays(for: "sleepAnalysis"), ["2024-03-01", "2024-03-02"])
        state.clearPending(state.pendingDays.filter { $0.healthType == "sleepAnalysis" })
        XCTAssertTrue(state.pendingDays(for: "sleepAnalysis").isEmpty)
        XCTAssertEqual(state.pendingDays.count, 1)
        state.setAnchor(Data([1]), for: "stepCount")
        state.setAnchor(nil, for: "stepCount")
        XCTAssertNil(state.anchor(for: "stepCount"))
    }
}
