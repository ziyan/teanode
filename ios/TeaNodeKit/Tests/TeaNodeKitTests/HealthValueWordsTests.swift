import XCTest

@testable import TeaNodeKit

/// HealthValueWordsTests pin the rules of the table at the top of
/// internal/health/words.go, which the iPhone copies: the same examples the
/// server's tests use. All the data is invented.
final class HealthValueWordsTests: XCTestCase {
    private let english = Locale(identifier: "en_US")

    private func words(_ healthType: String, _ value: Double, _ quantityUnit: String) -> String {
        HealthValueWords.englishText(healthType: healthType, value: value, quantityUnit: quantityUnit)
    }

    func testValuesAreWrittenAsTheTableSays() {
        let cases: [(String, Double, String, String)] = [
            ("stepCount", 8412.4, "count", "8,412 steps"), ("stepCount", 1, "count", "1 step"), ("flightsClimbed", 12, "count", "12 flights"),
            ("bodyMassIndex", 22.456, "count", "22.5"), ("bodyMassIndex", 22.04, "count", "22"), ("uvExposure", 3.26, "count", "3.3"),
            ("appleSleepingBreathingDisturbances", 12.34, "count", "12.3"), ("nikeFuel", 1234.5, "count", "1,234.5"),
            ("stepCount", 0, "count", "0 steps"), ("stepCount", 1.4, "count", "1 step"), ("stepCount", 2, "count", "2 steps"),
            ("flightsClimbed", 1, "count", "1 flight"), ("pushCount", 1, "count", "1 push"), ("pushCount", 3, "count", "3 pushes"),
            ("swimmingStrokeCount", 1, "count", "1 stroke"), ("numberOfTimesFallen", 1, "count", "1 fall"),
            ("inhalerUsage", 1, "count", "1 puff"), ("numberOfAlcoholicBeverages", 1, "count", "1 drink"),
            ("numberOfAlcoholicBeverages", 2, "count", "2 drinks"), ("heartRateRecoveryOneMinute", 23.4, "count/min", "23 bpm"),
            ("walkingHeartRateAverage", 101.5, "count/min", "102 bpm"), ("bodyTemperature", 98.64, "degF", "98.64°F"),
            ("bodyMass", 150.26, "lb", "150.3 lb"), ("bodyMass", 1500.4, "lb", "1,500 lb"), ("bodyMass", 0.35, "kg", "0.35 kg"),
            ("heartRate", 74.3, "count/min", "74 bpm"), ("restingHeartRate", 55.5, "count/min", "56 bpm"),
            ("respiratoryRate", 15.6, "count/min", "16 breaths/min"), ("cyclingCadence", 88.2, "count/min", "88 rpm"),
            ("distanceWalkingRunning", 850, "m", "850 m"), ("distanceWalkingRunning", 999.6, "m", "999.6 m"),
            ("distanceWalkingRunning", 1000, "m", "1 km"), ("distanceWalkingRunning", 5234, "m", "5.2 km"),
            ("distanceWalkingRunning", 12980, "m", "13 km"), ("distanceWalkingRunning", 1_234_567, "m", "1,234.6 km"),
            ("runningStrideLength", 0.93, "m", "0.93 m"), ("bodyTemperature", 36.62, "degC", "36.62°C"),
            ("vo2Max", 42.3, "ml/(kg*min)", "42.3 mL/kg·min"), ("physicalEffort", 4.25, "kcal/(kg*hr)", "4.25 kcal/kg·hr"),
            ("activeEnergyBurned", 512.7, "kcal", "512.7 kcal"), ("oxygenSaturation", 0.975, "%", "98%"),
            ("bodyMass", 72.456, "kg", "72.46 kg"), ("environmentalAudioExposure", 71.2, "dBASPL", "71.2 dB"),
            ("workoutEffortScore", 6.5, "appleEffortScore", "6.5"), ("bodyMass", 72, "", "72"), ("bodyMass", -0.001, "kg", "0 kg"),
            // Percentages: whole numbers for blood oxygen and walking steadiness,
            // one decimal for body fat, walking asymmetry and double support, the
            // usual precision for the rest.
            ("oxygenSaturation", 0.9797, "%", "98%"), ("oxygenSaturation", 0.94, "%", "94%"), ("oxygenSaturation", 1, "%", "100%"),
            ("appleWalkingSteadiness", 0.674, "%", "67%"), ("bodyFatPercentage", 0.1837, "%", "18.4%"),
            ("bodyFatPercentage", 0.18, "%", "18%"), ("walkingAsymmetryPercentage", 0.0345, "%", "3.5%"),
            ("walkingDoubleSupportPercentage", 0.2849, "%", "28.5%"), ("peripheralPerfusionIndex", 0.0235, "%", "2.35%"),
            ("bloodAlcoholContent", 0.0008, "%", "0.08%"), ("bodyFatPercentage", 0.1845, "%", "18.5%"),
            // The tie rule: half away from zero on the shortest decimal form.
            ("bodyMassIndex", 22.25, "", "22.25"), ("x", 0.355, "", "0.36"), ("x", 1.005, "", "1.01"), ("x", 0.975, "", "0.98"),
            ("x", 150.25, "", "150.3"), ("x", 1000.5, "", "1,001"), ("x", -150.25, "", "-150.3"), ("x", -0.001, "", "0"),
            ("x", -0.004, "kg", "0 kg"), ("heartRate", 72.5, "count/min", "73 bpm"), ("heartRate", -0.5, "count/min", "-1 bpm"),
            ("stepCount", 2.5, "count", "3 steps"), ("stepCount", 0.5, "count", "1 step"),
            ("stepCount", 999_999.5, "count", "1,000,000 steps"), ("distanceWalkingRunning", 1050, "m", "1.1 km"),
        ]
        for (healthType, value, quantityUnit, expected) in cases {
            XCTAssertEqual(words(healthType, value, quantityUnit), expected, "\(healthType) \(value) \(quantityUnit)")
        }
    }

    func testThePartsSayWhichWordFollowsAndWhetherItIsSingular() {
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "stepCount", value: 1, quantityUnit: "count", locale: english),
            HealthValueParts(numberText: "1", unit: .noun(.step, isSingular: true)))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "stepCount", value: 8412, quantityUnit: "count", locale: english),
            HealthValueParts(numberText: "8,412", unit: .noun(.step, isSingular: false)))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "oxygenSaturation", value: 0.975, quantityUnit: "%", locale: english),
            HealthValueParts(numberText: "98", unit: .touching("%")))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "bodyFatPercentage", value: 0.1837, quantityUnit: "%", locale: english),
            HealthValueParts(numberText: "18.4", unit: .touching("%")))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "heartRate", value: 74, quantityUnit: "count/min", locale: english),
            HealthValueParts(numberText: "74", unit: .spaced("bpm")))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "bodyMassIndex", value: 22.5, quantityUnit: "count", locale: english),
            HealthValueParts(numberText: "22.5", unit: .none))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "workoutEffortScore", value: 6.5, quantityUnit: "appleEffortScore", locale: english),
            HealthValueParts(numberText: "6.5", unit: .none))
        XCTAssertEqual(
            HealthValueWords.parts(healthType: "bodyTemperature", value: 36.6, quantityUnit: "degC", locale: english),
            HealthValueParts(numberText: "36.6", unit: .touching("°C")))
    }

    func testEveryCountNounIsUsedByAKindThatCountsIt() {
        XCTAssertEqual(Set(HealthValueWords.countNouns.values), Set(HealthCountNoun.allCases))
        for (healthType, _) in HealthValueWords.countNouns {
            XCTAssertEqual(HealthTypes.info(for: healthType)?.quantityUnit, "count", healthType)
        }
    }

    func testTheSizeOfANumberDecidesItsDecimals() {
        XCTAssertEqual(HealthValueWords.precisionDigits(for: 1000), 0)
        XCTAssertEqual(HealthValueWords.precisionDigits(for: -1000), 0)
        XCTAssertEqual(HealthValueWords.precisionDigits(for: 999.9), 1)
        XCTAssertEqual(HealthValueWords.precisionDigits(for: 100), 1)
        XCTAssertEqual(HealthValueWords.precisionDigits(for: 99.9), 2)
        XCTAssertEqual(HealthValueWords.numberText(8412.4, locale: english), "8,412")
        XCTAssertEqual(HealthValueWords.numberText(150.26, locale: english), "150.3")
        XCTAssertEqual(HealthValueWords.numberText(72.4, locale: english), "72.4")
        XCTAssertEqual(HealthValueWords.numberText(0.35, locale: english), "0.35")
    }

    func testTimesAreHoursAndMinutes() {
        let cases: [(Double, HealthDurationParts)] = [
            (0, .seconds(0)), (45, .seconds(45)), (59.4, .seconds(59)), (60, .minutes(1)), (2100, .minutes(35)), (3600, .hours(1)),
            (25920, .hoursMinutes(hours: 7, minutes: 12)), (25950, .hoursMinutes(hours: 7, minutes: 13)), (3599, .hours(1)),
            (637_800, .hoursMinutes(hours: 177, minutes: 10)),
        ]
        for (seconds, expected) in cases {
            XCTAssertEqual(HealthValueWords.durationParts(seconds: seconds), expected, "\(seconds)")
        }
    }

    func testAnAgeCountsWholeYearsAndIsNilForADayThatIsNotThereYet() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let today = try XCTUnwrap(ServerTime.date("2026-03-02T15:00:00Z"))
        XCTAssertEqual(HealthValueWords.ageInYears(dateOfBirth: "1990-03-02", today: today, calendar: calendar), 36)
        XCTAssertEqual(HealthValueWords.ageInYears(dateOfBirth: "1990-03-03", today: today, calendar: calendar), 35)
        XCTAssertEqual(HealthValueWords.ageInYears(dateOfBirth: "2025-03-02", today: today, calendar: calendar), 1)
        XCTAssertEqual(HealthValueWords.ageInYears(dateOfBirth: "2026-03-02", today: today, calendar: calendar), 0)
        XCTAssertNil(HealthValueWords.ageInYears(dateOfBirth: "2030-01-01", today: today, calendar: calendar))
        XCTAssertNil(HealthValueWords.ageInYears(dateOfBirth: "not a date", today: today, calendar: calendar))
        XCTAssertNil(HealthValueWords.ageInYears(dateOfBirth: "1990-02-30", today: today, calendar: calendar))
    }

    func testBloodTypesAreWrittenAsOnACard() {
        let cases = [
            "aPositive": "A+", "aNegative": "A-", "bPositive": "B+", "bNegative": "B-", "abPositive": "AB+", "abNegative": "AB-",
            "oPositive": "O+", "oNegative": "O-", "notSet": "notSet", "something": "something",
        ]
        for (sent, expected) in cases { XCTAssertEqual(HealthValueWords.bloodTypeText(sent), expected, sent) }
    }

    func testTheReadingsOfADayAreCountedAndAveragedByTheDayEachBegan() throws {
        var tokyo = Calendar(identifier: .gregorian)
        tokyo.timeZone = try XCTUnwrap(TimeZone(identifier: "Asia/Tokyo"))
        func reading(_ text: String, _ value: Double) throws -> HealthReading {
            HealthReading(startedAt: try XCTUnwrap(ServerTime.date(text)), value: value)
        }
        let readings = [
            try reading("2024-03-01T01:00:00Z", 60), try reading("2024-03-01T05:00:00Z", 80), try reading("2024-03-01T05:30:00Z", 70),
            // 00:30 on the 2nd in Tokyo.
            try reading("2024-03-01T15:30:00Z", 90), try reading("2024-03-01T16:00:00Z", .nan),
        ]
        let statistics = HealthDaySummaries.readingStatistics(readings, calendar: tokyo)
        XCTAssertEqual(Set(statistics.keys), ["2024-03-01", "2024-03-02"])
        XCTAssertEqual(statistics["2024-03-01"]?.sampleCount, 3)
        XCTAssertEqual(statistics["2024-03-01"]?.averageValue, 70)
        XCTAssertEqual(statistics["2024-03-01"]?.minimumValue, 60)
        XCTAssertEqual(statistics["2024-03-01"]?.maximumValue, 80)
        XCTAssertNil(statistics["2024-03-01"]?.sumValue)
        XCTAssertEqual(statistics["2024-03-02"]?.sampleCount, 1)
        XCTAssertEqual(HealthDaySummaries.readingStatistics(readings, calendar: tokyo, days: ["2024-03-02"]).keys.sorted(), ["2024-03-02"])
        XCTAssertTrue(HealthDaySummaries.readingStatistics([], calendar: tokyo).isEmpty)
        // The summary of a day made from them is an average with its range.
        let summary = HealthDaySummaries.quantitySummary(
            healthType: "environmentalAudioExposure", summaryDay: "2024-03-01", statistics: try XCTUnwrap(statistics["2024-03-01"]),
            quantityUnit: "dBASPL")
        XCTAssertEqual(summary.averageValue, 70)
        XCTAssertEqual(summary.sampleCount, 3)
    }
}
