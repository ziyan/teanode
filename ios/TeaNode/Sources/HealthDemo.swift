#if DEBUG
    import Foundation
    import SwiftUI
    import TeaNodeKit

    // The invented Health data that the screenshots of the Health screens
    // photograph (`-TeaNodeDemo`): steps, active energy, workouts, heart rate,
    // weight and sleep for the last four months of one made-up person, one
    // made-up route, the settings, and one share from a made-up member of the
    // person's circle. Every number is invented; the route is in the same
    // made-up area as the Location demo.

    /// HealthDemo holds the invented person's Health data.
    enum HealthDemo {
        /// The person in the demo's circle who shares Health with the person.
        static let sharingPersonId = "sam"
        static let runSampleId = "workout-run-1"
        /// How many days back the data reaches.
        static let dayCount = 120
        /// The categories the made-up circle member shares.
        static let sharedCategories = ["activity", "sleep"]
        /// The kinds of data the person has, in the Health app's order.
        static let healthTypes = ["stepCount", "activeEnergyBurned", "workout", "heartRate", "bodyMass", "sleepAnalysis"]

        // MARK: Days

        static func day(daysAgo: Int) -> Date {
            let calendar = Calendar.current
            return calendar.date(byAdding: .day, value: -daysAgo, to: calendar.startOfDay(for: Date())) ?? Date()
        }

        static func dayText(daysAgo: Int) -> String {
            HealthDays.text(of: day(daysAgo: daysAgo), calendar: Calendar.current)
        }

        /// How many days before today a day is; negative for a day to come.
        static func daysAgo(of date: Date) -> Int {
            let calendar = Calendar.current
            return calendar.dateComponents([.day], from: calendar.startOfDay(for: date), to: calendar.startOfDay(for: Date())).day ?? 0
        }

        // MARK: One day of a kind

        /// DayData is one kind on one day, as the phone worked it out.
        struct DayData {
            var sumValue: Double?
            var averageValue: Double?
            var minimumValue: Double?
            var maximumValue: Double?
            var sampleCount = 0
            var durationSeconds: Double?
            var categorySeconds: [String: Double] = [:]
        }

        /// A kind on a day, or nil when nothing was recorded. `scale` makes
        /// another person's numbers differ from the first person's.
        static func dayData(healthType: String, daysAgo: Int, scale: Double = 1) -> DayData? {
            guard daysAgo >= 0, daysAgo < dayCount else { return nil }
            let wave = sin(Double(daysAgo) * 0.9)
            let slowWave = cos(Double(daysAgo) * 0.37)
            switch healthType {
            case "stepCount":
                var steps = 8200 + 2600 * wave
                if daysAgo % 7 == 0 { steps += 3000 }
                // Today is part way through.
                if daysAgo == 0 { steps = 4300 }
                return DayData(sumValue: (steps * scale).rounded(), sampleCount: 48)
            case "activeEnergyBurned":
                var energy = 430 + 110 * wave
                if daysAgo == 0 { energy = 210 }
                return DayData(sumValue: (energy * scale).rounded(), sampleCount: 48)
            case "heartRate":
                return DayData(
                    averageValue: (74 + 4 * wave).rounded(), minimumValue: 52 + (3 * slowWave).rounded(),
                    maximumValue: (128 + 18 * wave).rounded(), sampleCount: 260)
            case "bodyMass":
                guard daysAgo % 3 == 0 else { return nil }
                let weight = ((71.2 + 0.012 * Double(daysAgo) + 0.15 * wave) * 10).rounded() / 10
                return DayData(averageValue: weight, minimumValue: weight, maximumValue: weight, sampleCount: 1)
            case "sleepAnalysis":
                let deep = 4300 + 700 * wave
                let core = 15800 + 1300 * slowWave
                let rapidEyeMovement = 5600 + 600 * sin(Double(daysAgo) * 1.3)
                let awake = 1500 + 400 * slowWave
                let asleep = deep + core + rapidEyeMovement
                return DayData(
                    sampleCount: 24, durationSeconds: asleep,
                    categorySeconds: [
                        "asleepDeep": deep, "asleepCore": core, "asleepREM": rapidEyeMovement, "awake": awake, "asleep": asleep,
                        "inBed": asleep + awake + 600,
                    ])
            case "workout":
                guard daysAgo % 4 == 1 else { return nil }
                return DayData(sampleCount: 1, durationSeconds: 2400 + 900 * (wave + 1) / 2)
            default:
                return nil
            }
        }

        // MARK: JSON

        private static func number(_ value: Double?) -> Any {
            guard let value else { return NSNull() }
            return value
        }

        private static func daySummaryJSON(typeInfo: HealthTypeInfo, dayText: String, data: DayData) -> [String: Any] {
            [
                "summaryDay": dayText, "quantityUnit": typeInfo.quantityUnit, "sumValue": number(data.sumValue),
                "averageValue": number(data.averageValue), "minimumValue": number(data.minimumValue),
                "maximumValue": number(data.maximumValue), "sampleCount": data.sampleCount, "durationSeconds": number(data.durationSeconds),
                "categorySeconds": data.categorySeconds,
            ]
        }

        /// A kind as `ListHealthTypes` gives it.
        static func typeJSON(healthType: String, scale: Double = 1) -> [String: Any]? {
            guard let typeInfo = HealthTypes.info(for: healthType) else { return nil }
            var latestDaySummary: Any = NSNull()
            for daysAgo in 0..<8 {
                if let data = dayData(healthType: healthType, daysAgo: daysAgo, scale: scale) {
                    latestDaySummary = daySummaryJSON(typeInfo: typeInfo, dayText: dayText(daysAgo: daysAgo), data: data)
                    break
                }
            }
            var summaryKind: Any = NSNull()
            if let kind = typeInfo.summaryKind { summaryKind = kind.rawValue }
            return [
                "healthType": typeInfo.healthType, "healthCategory": typeInfo.healthCategory.rawValue, "typeName": typeInfo.typeName,
                "valueKind": typeInfo.valueKind.rawValue, "quantityUnit": typeInfo.quantityUnit, "summaryKind": summaryKind,
                "firstDay": dayText(daysAgo: dayCount - 1), "lastDay": dayText(daysAgo: 0), "sampleCount": 3000,
                "latestDaySummary": latestDaySummary,
            ]
        }

        /// The kinds a person has: all of the first person's, and of a person
        /// sharing only the categories they share.
        static func typesJSON(ownerUserId: String?) -> [[String: Any]] {
            let isSharer = ownerUserId == sharingPersonId
            return healthTypes.compactMap { healthType in
                if isSharer, let typeInfo = HealthTypes.info(for: healthType), !sharedCategories.contains(typeInfo.healthCategory.rawValue) { return nil }
                return typeJSON(healthType: healthType, scale: isSharer ? 0.9 : 1)
            }
        }

        // MARK: Periods

        private static func periodStart(of date: Date, period: String) -> Date {
            let calendar = Calendar.current
            switch period {
            case "week":
                // Monday; Sunday is weekday 1.
                let weekday = calendar.component(.weekday, from: date)
                return calendar.date(byAdding: .day, value: -((weekday + 5) % 7), to: date) ?? date
            case "month":
                return calendar.date(from: calendar.dateComponents([.year, .month], from: date)) ?? date
            default:
                return date
            }
        }

        private static func periodEnd(startingAt start: Date, period: String) -> Date {
            let calendar = Calendar.current
            switch period {
            case "week":
                return calendar.date(byAdding: .day, value: 6, to: start) ?? start
            case "month":
                let nextMonth = calendar.date(byAdding: .month, value: 1, to: start) ?? start
                return calendar.date(byAdding: .day, value: -1, to: nextMonth) ?? start
            default:
                return start
            }
        }

        /// The days of a kind from one day to another added up by day, week
        /// or month, as the server adds up the day summaries.
        static func periodsJSON(healthType: String, fromDay: Date, toDay: Date, period: String, scale: Double = 1) -> [[String: Any]] {
            let calendar = Calendar.current
            var daysByStart: [Date: [DayData]] = [:]
            var day = fromDay
            while day <= toDay {
                if let data = dayData(healthType: healthType, daysAgo: daysAgo(of: day), scale: scale) {
                    daysByStart[periodStart(of: day, period: period), default: []].append(data)
                }
                guard let next = calendar.date(byAdding: .day, value: 1, to: day) else { break }
                day = next
            }
            let starts = daysByStart.keys.sorted().suffix(400)
            return starts.map { start -> [String: Any] in
                let days = daysByStart[start] ?? []
                let sampleCount = days.reduce(0) { $0 + $1.sampleCount }
                var categorySeconds: [String: Double] = [:]
                for data in days {
                    for (stage, seconds) in data.categorySeconds { categorySeconds[stage, default: 0] += seconds }
                }
                let weightedSum = days.reduce(0.0) { $0 + ($1.averageValue ?? 0) * Double($1.sampleCount) }
                let totalSum = days.reduce(0.0) { $0 + ($1.sumValue ?? 0) }
                let totalDuration = days.reduce(0.0) { $0 + ($1.durationSeconds ?? 0) }
                var sumValue: Double?
                if days.contains(where: { $0.sumValue != nil }) { sumValue = totalSum }
                var averageValue: Double?
                if days.contains(where: { $0.averageValue != nil }), sampleCount > 0 { averageValue = weightedSum / Double(sampleCount) }
                var durationSeconds: Double?
                if days.contains(where: { $0.durationSeconds != nil }) { durationSeconds = totalDuration }
                return [
                    "startDay": HealthDays.text(of: start, calendar: calendar),
                    "endDay": HealthDays.text(of: periodEnd(startingAt: start, period: period), calendar: calendar),
                    "sumValue": number(sumValue),
                    "averageValue": number(averageValue),
                    "minimumValue": number(days.compactMap(\.minimumValue).min()),
                    "maximumValue": number(days.compactMap(\.maximumValue).max()),
                    "sampleCount": sampleCount,
                    "durationSeconds": number(durationSeconds),
                    "dayCount": days.count,
                    "categorySeconds": categorySeconds,
                ]
            }
        }

        // MARK: Readings

        private static func sampleJSON(
            id: String, healthType: String, startedAt: Date, endedAt: Date, quantityValue: Double? = nil, quantityUnit: String = "",
            categoryValue: Int? = nil, workoutActivityType: String = "", sourceName: String, metadata: [String: Any] = [:],
            hasRoute: Bool = false
        ) -> [String: Any] {
            let metadataText = (try? JSONSerialization.data(withJSONObject: metadata)).flatMap { String(data: $0, encoding: .utf8) } ?? "{}"
            var categoryNumber: Any = NSNull()
            if let categoryValue { categoryNumber = categoryValue }
            return [
                "healthSampleId": id, "healthType": healthType, "startedAt": CalendarDates.text(startedAt),
                "endedAt": CalendarDates.text(endedAt), "quantityValue": number(quantityValue), "quantityUnit": quantityUnit,
                "categoryValue": categoryNumber, "workoutActivityType": workoutActivityType, "sourceName": sourceName,
                "sourceBundleId": "", "deviceName": "", "sampleMetadataJson": metadataText,
                "receivedAt": CalendarDates.text(endedAt), "hasRoute": hasRoute,
            ]
        }

        /// The person's readings of a kind, newest first.
        static func samplesJSON(healthType: String) -> [[String: Any]] {
            let now = Date()
            switch healthType {
            case "stepCount":
                return (0..<60).map { index in
                    let end = now.addingTimeInterval(-Double(index) * 3600)
                    return sampleJSON(
                        id: "step-\(index)", healthType: healthType, startedAt: end.addingTimeInterval(-3000), endedAt: end,
                        quantityValue: Double(220 + (index * 137) % 700), quantityUnit: "count", sourceName: index % 3 == 0 ? "Watch" : "iPhone")
                }
            case "activeEnergyBurned":
                return (0..<60).map { index in
                    let end = now.addingTimeInterval(-Double(index) * 3600)
                    return sampleJSON(
                        id: "energy-\(index)", healthType: healthType, startedAt: end.addingTimeInterval(-3000), endedAt: end,
                        quantityValue: Double(12 + (index * 5) % 30), quantityUnit: "kcal", sourceName: "Watch")
                }
            case "heartRate":
                return (0..<60).map { index in
                    let end = now.addingTimeInterval(-Double(index) * 900)
                    return sampleJSON(
                        id: "heart-\(index)", healthType: healthType, startedAt: end.addingTimeInterval(-60), endedAt: end,
                        quantityValue: Double(64 + (index * 7) % 40), quantityUnit: "count/min", sourceName: "Watch")
                }
            case "bodyMass":
                return (0..<40).map { index in
                    let start = day(daysAgo: index * 3).addingTimeInterval(7 * 3600)
                    return sampleJSON(
                        id: "weight-\(index)", healthType: healthType, startedAt: start, endedAt: start,
                        quantityValue: dayData(healthType: healthType, daysAgo: index * 3)?.averageValue, quantityUnit: "kg",
                        sourceName: "Bathroom scale")
                }
            case "sleepAnalysis":
                return sleepSamplesJSON()
            case "workout":
                return workoutSamplesJSON()
            default:
                return []
            }
        }

        /// Three nights of stages, newest first: the stage's raw value and
        /// its minutes.
        private static func sleepSamplesJSON() -> [[String: Any]] {
            let stages: [(value: Int, minutes: Double)] = [(3, 90), (4, 60), (3, 80), (5, 40), (3, 70), (2, 6), (5, 45), (3, 60)]
            var samples: [[String: Any]] = []
            for night in 0..<3 {
                var start = day(daysAgo: night).addingTimeInterval(-90 * 60)
                for (index, stage) in stages.enumerated() {
                    let end = start.addingTimeInterval(stage.minutes * 60)
                    samples.append(
                        sampleJSON(
                            id: "sleep-\(night)-\(index)", healthType: "sleepAnalysis", startedAt: start, endedAt: end,
                            categoryValue: stage.value, sourceName: "Watch"))
                    start = end
                }
            }
            return samples.reversed()
        }

        private static var runStart: Date { day(daysAgo: 1).addingTimeInterval(7 * 3600 + 10 * 60) }

        private static func workoutSamplesJSON() -> [[String: Any]] {
            let walkStart = day(daysAgo: 5).addingTimeInterval(18 * 3600)
            let rideStart = day(daysAgo: 9).addingTimeInterval(8 * 3600)
            return [
                sampleJSON(
                    id: runSampleId, healthType: "workout", startedAt: runStart, endedAt: runStart.addingTimeInterval(42 * 60),
                    workoutActivityType: "running", sourceName: "Watch",
                    metadata: ["totalDistanceMeters": 6800.0, "totalEnergyBurnedKilocalories": 480.0], hasRoute: true),
                sampleJSON(
                    id: "workout-walk-1", healthType: "workout", startedAt: walkStart, endedAt: walkStart.addingTimeInterval(35 * 60),
                    workoutActivityType: "walking", sourceName: "Watch",
                    metadata: ["totalDistanceMeters": 2900.0, "totalEnergyBurnedKilocalories": 150.0]),
                sampleJSON(
                    id: "workout-ride-1", healthType: "workout", startedAt: rideStart, endedAt: rideStart.addingTimeInterval(55 * 60),
                    workoutActivityType: "cycling", sourceName: "Watch",
                    metadata: ["totalDistanceMeters": 17400.0, "totalEnergyBurnedKilocalories": 520.0]),
            ]
        }

        /// The workout's route: a loop of about two kilometers in the demo's
        /// made-up area, a point every half minute.
        static var routePoints: [[Double]] {
            (0..<80).map { index in
                let fraction = Double(index) / 79
                let angle = fraction * 2 * Double.pi
                let latitude = 10.5040 + 0.0035 * sin(angle) + 0.0004 * sin(angle * 5)
                let longitude = 20.5050 + 0.0050 * cos(angle)
                let altitude = 12 + 4 * sin(angle * 2)
                return [Double(index) * 31, latitude, longitude, altitude]
            }
        }

        static var runRouteRequest: HealthRouteRequest {
            HealthRouteRequest(
                healthSampleId: runSampleId, titleText: HealthWords.workoutName("running"), startedAt: runStart, durationSeconds: 42 * 60,
                distanceMeters: 6800, energyKilocalories: 480)
        }

        // MARK: Settings and sharing

        static func settingsJSON(canAgentReadHealth: Bool) -> [String: Any] {
            [
                "canAgentReadHealth": canAgentReadHealth, "healthErasedAt": NSNull(), "sampleCount": 18432,
                "lastReceivedAt": CalendarDates.text(Date().addingTimeInterval(-12 * 60)), "modelProviderName": "Example AI",
                "characteristics": [
                    ["healthType": "dateOfBirth", "characteristicValue": "1990-04-12"],
                    ["healthType": "biologicalSex", "characteristicValue": "female"],
                    ["healthType": "bloodType", "characteristicValue": "oPositive"],
                ],
            ]
        }

        /// The person's own share with the family circle, and the one a
        /// made-up member of that circle makes with the person.
        static var sharesJSON: [[String: Any]] {
            [
                [
                    "id": "health-share-1", "ownerUserId": "robin", "ownerUsername": "robin", "ownerName": "Robin",
                    "granteeUserId": NSNull(), "granteeUsername": NSNull(), "granteeName": NSNull(), "granteeCircleId": "circle-home",
                    "granteeCircleName": "Home", "healthCategories": sharedCategories, "createdAt": "2026-10-10T08:00:00Z",
                ],
                [
                    "id": "health-share-2", "ownerUserId": sharingPersonId, "ownerUsername": "sam", "ownerName": "Sam",
                    "granteeUserId": "robin", "granteeUsername": "robin", "granteeName": "Robin", "granteeCircleId": NSNull(),
                    "granteeCircleName": NSNull(), "healthCategories": sharedCategories, "createdAt": "2026-10-11T08:00:00Z",
                ],
            ]
        }

        // MARK: For previews

        static func decoded<Value: Decodable>(_ object: Any, as type: Value.Type) -> Value? {
            guard let data = try? JSONSerialization.data(withJSONObject: object) else { return nil }
            return try? JSONDecoder().decode(type, from: data)
        }

        static func typeView(_ healthType: String) -> HealthTypeView? {
            typeJSON(healthType: healthType).flatMap { decoded($0, as: HealthTypeView.self) }
        }

        /// The chart a kind has by day, week or month over the last 90 days.
        static func plan(healthType: String, period: HealthPeriod) -> HealthChartPlan? {
            guard let type = typeView(healthType) else { return nil }
            let periods = periodsJSON(healthType: healthType, fromDay: day(daysAgo: 89), toDay: day(daysAgo: 0), period: period.rawValue)
            var summaryKind: Any = NSNull()
            if let kind = type.summaryKind { summaryKind = kind }
            let summary: [String: Any] = [
                "healthType": healthType, "quantityUnit": type.quantityUnit, "summaryKind": summaryKind, "periods": periods,
            ]
            guard let view = decoded(summary, as: HealthSummaryView.self) else { return nil }
            return HealthChartPlan.make(summary: view, type: type, period: period, fromDay: dayText(daysAgo: 89), toDay: dayText(daysAgo: 0))
        }
    }

    // MARK: The answers

    extension DemoServer {
        /// What the Health screens ask, or nil for any other question.
        static func healthAnswer(query: String, body: Data?) -> String? {
            let variables = requestVariables(body)
            let ownerUserId = variables["ownerUserId"] as? String
            if query.contains("ListHealthTypes") {
                return healthJSON("ListHealthTypes", HealthDemo.typesJSON(ownerUserId: ownerUserId))
            }
            if query.contains("ReadHealthSummary("), let healthType = variables["healthType"] as? String {
                let calendar = Calendar.current
                let today = calendar.startOfDay(for: Date())
                let fromDay = (variables["fromDay"] as? String).flatMap { HealthText.date(ofDay: $0) } ?? HealthDemo.day(daysAgo: 89)
                // Nothing is asked about days to come.
                let toDay = min((variables["toDay"] as? String).flatMap { HealthText.date(ofDay: $0) } ?? today, today)
                let period = variables["period"] as? String ?? "day"
                let isSharer = ownerUserId == HealthDemo.sharingPersonId
                let typeInfo = HealthTypes.info(for: healthType)
                let isHidden = isSharer && !HealthDemo.sharedCategories.contains(typeInfo?.healthCategory.rawValue ?? "")
                var periods: [[String: Any]] = []
                if !isHidden {
                    periods = HealthDemo.periodsJSON(
                        healthType: healthType, fromDay: fromDay, toDay: toDay, period: period, scale: isSharer ? 0.9 : 1)
                }
                var summaryKind: Any = NSNull()
                if let kind = typeInfo?.summaryKind { summaryKind = kind.rawValue }
                let summary: [String: Any] = [
                    "healthType": healthType, "quantityUnit": typeInfo?.quantityUnit ?? "", "summaryKind": summaryKind, "periods": periods,
                ]
                return healthJSON("ReadHealthSummary", summary)
            }
            if query.contains("ListHealthSamples("), let healthType = variables["healthType"] as? String {
                let all = HealthDemo.samplesJSON(healthType: healthType)
                let offset = variables["offset"] as? Int ?? 0
                let limit = variables["limitCount"] as? Int ?? 50
                let page = Array(all.dropFirst(offset).prefix(limit))
                var nextOffset: Any = NSNull()
                if offset + limit < all.count { nextOffset = offset + limit }
                return healthJSON("ListHealthSamples", ["samples": page, "totalCount": all.count, "nextOffset": nextOffset])
            }
            if query.contains("ReadHealthRoute(") {
                return healthJSON("ReadHealthRoute", ["healthSampleId": HealthDemo.runSampleId, "routePoints": HealthDemo.routePoints])
            }
            if query.contains("UpdateHealthSettings(") {
                return healthJSON("UpdateHealthSettings", HealthDemo.settingsJSON(canAgentReadHealth: variables["canAgentReadHealth"] as? Bool ?? false))
            }
            if query.contains("ReadHealthSettings") {
                return healthJSON("ReadHealthSettings", HealthDemo.settingsJSON(canAgentReadHealth: false))
            }
            if query.contains("EraseHealthData(") {
                return healthJSON("EraseHealthData", ["erasedSampleCount": 18432])
            }
            if query.contains("ListHealthShares") {
                return healthJSON("ListHealthShares", HealthDemo.sharesJSON)
            }
            if query.contains("SetHealthShare(") {
                // The list is fixed, so a share made is acknowledged and not shown.
                let categories = variables["healthCategories"] as? [String] ?? []
                if categories.isEmpty { return #"{"data":{"SetHealthShare":null}}"# }
                var share = HealthDemo.sharesJSON[0]
                share["healthCategories"] = categories
                return healthJSON("SetHealthShare", share)
            }
            if query.contains("ReportHealth(") {
                return healthJSON(
                    "ReportHealth", ["newSampleCount": 0, "deletedSampleCount": 0, "updatedDayCount": 0, "deletedSamples": [Any](), "healthErasedAt": NSNull()])
            }
            return nil
        }

        /// `{"data":{"<operation>": <payload>}}`.
        private static func healthJSON(_ operation: String, _ payload: Any) -> String {
            let object: [String: Any] = ["data": [operation: payload]]
            guard let data = try? JSONSerialization.data(withJSONObject: object), let text = String(data: data, encoding: .utf8) else {
                return #"{"data":null}"#
            }
            return text
        }
    }

    // MARK: Previews

    #Preview("Health setup") {
        HealthSetupView {}
    }

    #Preview("Health setup, dark") {
        HealthSetupView {}
            .preferredColorScheme(.dark)
    }

    #Preview("Health not allowed") {
        HealthNotAllowedView()
    }

    #Preview("Steps by day") {
        if let plan = HealthDemo.plan(healthType: "stepCount", period: .day) {
            HealthChartView(plan: plan, selectedKey: .constant(nil))
                .padding()
        }
    }

    #Preview("Steps by week, one touched") {
        if let plan = HealthDemo.plan(healthType: "stepCount", period: .week) {
            HealthChartView(plan: plan, selectedKey: .constant(plan.periodKeys.last))
                .padding()
        }
    }

    #Preview("Sleep by day") {
        if let plan = HealthDemo.plan(healthType: "sleepAnalysis", period: .day) {
            HealthChartView(plan: plan, selectedKey: .constant(nil))
                .padding()
        }
    }

    #Preview("Heart rate by week, dark") {
        if let plan = HealthDemo.plan(healthType: "heartRate", period: .week) {
            HealthChartView(plan: plan, selectedKey: .constant(nil))
                .padding()
                .preferredColorScheme(.dark)
        }
    }

    #Preview("Weight by month") {
        if let plan = HealthDemo.plan(healthType: "bodyMass", period: .month) {
            HealthChartView(plan: plan, selectedKey: .constant(nil))
                .padding()
        }
    }
#endif
