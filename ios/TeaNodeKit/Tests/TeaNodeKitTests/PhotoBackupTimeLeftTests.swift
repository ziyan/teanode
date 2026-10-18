import XCTest

@testable import TeaNodeKit

final class PhotoBackupTimeLeftTests: XCTestCase {
    private let start = Date(timeIntervalSince1970: 1_000_000)

    /// Records one sample a minute for `minuteCount` minutes after `firstMinute`,
    /// the count growing by `itemsPerMinute` each time, from `firstCount`.
    private func recordSteadily(
        _ timeLeft: inout PhotoBackupTimeLeft, firstMinute: Int, minuteCount: Int, firstCount: Int, itemsPerMinute: Int, isUploading: Bool = true
    ) {
        for minute in 0...minuteCount {
            timeLeft.record(
                date: start.addingTimeInterval(Double(firstMinute + minute) * 60), backedUpItemCount: firstCount + minute * itemsPerMinute,
                isUploading: isUploading)
        }
    }

    func testThereIsNoEstimateBeforeTheSamplesSpanTwoMinutes() {
        var timeLeft = PhotoBackupTimeLeft()
        timeLeft.record(date: start, backedUpItemCount: 0, isUploading: true)
        timeLeft.record(date: start.addingTimeInterval(60), backedUpItemCount: 100, isUploading: true)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 1_000))
        timeLeft.record(date: start.addingTimeInterval(119), backedUpItemCount: 200, isUploading: true)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 1_000))
        timeLeft.record(date: start.addingTimeInterval(120), backedUpItemCount: 200, isUploading: true)
        XCTAssertNotNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 1_000))
    }

    func testThereIsNoEstimateBeforeFiveItemsAreFinished() {
        var timeLeft = PhotoBackupTimeLeft()
        timeLeft.record(date: start, backedUpItemCount: 10, isUploading: true)
        timeLeft.record(date: start.addingTimeInterval(600), backedUpItemCount: 14, isUploading: true)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 1_000))
        timeLeft.record(date: start.addingTimeInterval(660), backedUpItemCount: 15, isUploading: true)
        XCTAssertNotNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 1_000))
    }

    func testASteadyRateGivesTheItemsLeftOverTheRate() throws {
        var timeLeft = PhotoBackupTimeLeft()
        // Ten items a minute for ten minutes.
        recordSteadily(&timeLeft, firstMinute: 0, minuteCount: 10, firstCount: 0, itemsPerMinute: 10)
        let seconds = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        XCTAssertEqual(Double(seconds), 3_600, accuracy: 1)
        let secondsForFewer = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 100))
        XCTAssertEqual(Double(secondsForFewer), 600, accuracy: 1)
    }

    func testABurstAtTheStartIsForgottenOnceItLeavesTheWindow() throws {
        var timeLeft = PhotoBackupTimeLeft()
        // 3,000 items marked backed up in the first minute (photos the server
        // already had), then ten items a minute for an hour.
        timeLeft.record(date: start, backedUpItemCount: 0, isUploading: true)
        recordSteadily(&timeLeft, firstMinute: 1, minuteCount: 60, firstCount: 3_000, itemsPerMinute: 10)
        let seconds = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        XCTAssertEqual(Double(seconds), 3_600, accuracy: 60)
    }

    func testTheRateFollowsASlowdownButNotAtOnce() throws {
        var timeLeft = PhotoBackupTimeLeft()
        recordSteadily(&timeLeft, firstMinute: 0, minuteCount: 30, firstCount: 0, itemsPerMinute: 20)
        let before = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        XCTAssertEqual(Double(before), 1_800, accuracy: 1)
        // The rate falls to five items a minute. One minute later the answer
        // has only started to move; ten minutes later it is close to the new rate.
        recordSteadily(&timeLeft, firstMinute: 31, minuteCount: 0, firstCount: 600 + 5, itemsPerMinute: 5)
        let soon = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        XCTAssertGreaterThan(soon, before)
        XCTAssertLessThan(soon, 3_600)
        recordSteadily(&timeLeft, firstMinute: 32, minuteCount: 10, firstCount: 610, itemsPerMinute: 5)
        let later = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        XCTAssertGreaterThan(later, soon)
        XCTAssertLessThan(later, 7_200 + 600)
    }

    func testNothingFinishingForTheWholeWindowGivesNoEstimate() {
        var timeLeft = PhotoBackupTimeLeft()
        recordSteadily(&timeLeft, firstMinute: 0, minuteCount: 10, firstCount: 0, itemsPerMinute: 10)
        XCTAssertNotNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        // Then the count stands still for twenty minutes, still uploading.
        recordSteadily(&timeLeft, firstMinute: 11, minuteCount: 20, firstCount: 100, itemsPerMinute: 0)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
    }

    func testThereIsNoEstimateWhileNotUploadingAndTheRunStartsFreshAfterwards() {
        var timeLeft = PhotoBackupTimeLeft()
        recordSteadily(&timeLeft, firstMinute: 0, minuteCount: 10, firstCount: 0, itemsPerMinute: 10)
        XCTAssertNotNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        timeLeft.record(date: start.addingTimeInterval(11 * 60), backedUpItemCount: 110, isUploading: false)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        // Uploading again: the pause is not counted, so a minute is not enough
        // but two minutes of the new run are.
        timeLeft.record(date: start.addingTimeInterval(40 * 60), backedUpItemCount: 110, isUploading: true)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        timeLeft.record(date: start.addingTimeInterval(42 * 60), backedUpItemCount: 130, isUploading: true)
        XCTAssertNotNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
    }

    func testSamplesOutsideTheWindowAreDroppedAndTheMemoryIsBounded() throws {
        var timeLeft = PhotoBackupTimeLeft()
        // A day of readings, one every ten seconds, one item each: far more than
        // the window holds.
        for second in stride(from: 0, through: 86_400, by: 10) {
            timeLeft.record(date: start.addingTimeInterval(Double(second)), backedUpItemCount: second / 10, isUploading: true)
        }
        // One item every ten seconds is six a minute, whatever came before.
        let seconds = try XCTUnwrap(timeLeft.estimatedSecondsLeft(itemsLeftCount: 60))
        XCTAssertEqual(Double(seconds), 600, accuracy: 10)
        let mirror = Mirror(reflecting: timeLeft)
        let samples = try XCTUnwrap(mirror.children.first { $0.label == "samples" }?.value as? [Any])
        XCTAssertLessThanOrEqual(samples.count, Int(PhotoBackupTimeLeft.windowSeconds / 10) + 1)
    }

    func testAFlatRunOfReadingsKeepsOnlyItsFirstAndLast() throws {
        var timeLeft = PhotoBackupTimeLeft()
        timeLeft.record(date: start, backedUpItemCount: 0, isUploading: true)
        for second in 1...500 {
            timeLeft.record(date: start.addingTimeInterval(Double(second)), backedUpItemCount: 0, isUploading: true)
        }
        let samples = try XCTUnwrap(Mirror(reflecting: timeLeft).children.first { $0.label == "samples" }?.value as? [Any])
        XCTAssertEqual(samples.count, 2)
    }

    func testNeverLessThanAMinuteWhileItemsRemain() throws {
        var timeLeft = PhotoBackupTimeLeft()
        recordSteadily(&timeLeft, firstMinute: 0, minuteCount: 10, firstCount: 0, itemsPerMinute: 100)
        XCTAssertEqual(timeLeft.estimatedSecondsLeft(itemsLeftCount: 1), 60)
        XCTAssertEqual(timeLeft.estimatedSecondsLeft(itemsLeftCount: 50), 60)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 0))
    }

    func testAFallingCountStartsTheRunAgain() {
        var timeLeft = PhotoBackupTimeLeft()
        recordSteadily(&timeLeft, firstMinute: 0, minuteCount: 10, firstCount: 500, itemsPerMinute: 10)
        XCTAssertNotNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
        timeLeft.record(date: start.addingTimeInterval(11 * 60), backedUpItemCount: 20, isUploading: true)
        XCTAssertNil(timeLeft.estimatedSecondsLeft(itemsLeftCount: 600))
    }
}

final class PhotoBackupSpokenDurationTests: XCTestCase {
    private func spoken(_ seconds: Int) -> PhotoBackupSpokenDuration { PhotoBackupSpokenDuration(seconds: seconds) }

    func testShortTimesAreMinutes() {
        XCTAssertEqual(spoken(60), PhotoBackupSpokenDuration(count: 1, unit: .minutes))
        XCTAssertEqual(spoken(5), PhotoBackupSpokenDuration(count: 1, unit: .minutes))
        XCTAssertEqual(spoken(100), PhotoBackupSpokenDuration(count: 2, unit: .minutes))
        XCTAssertEqual(spoken(9 * 60 + 20), PhotoBackupSpokenDuration(count: 9, unit: .minutes))
    }

    func testMinutesFromTenOnGoToTheNearestFive() {
        XCTAssertEqual(spoken(12 * 60), PhotoBackupSpokenDuration(count: 10, unit: .minutes))
        XCTAssertEqual(spoken(41 * 60), PhotoBackupSpokenDuration(count: 40, unit: .minutes))
        XCTAssertEqual(spoken(43 * 60), PhotoBackupSpokenDuration(count: 45, unit: .minutes))
        XCTAssertEqual(spoken(57 * 60), PhotoBackupSpokenDuration(count: 55, unit: .minutes))
    }

    func testAlmostAnHourIsAnHour() {
        XCTAssertEqual(spoken(58 * 60), PhotoBackupSpokenDuration(count: 1, unit: .hours))
        XCTAssertEqual(spoken(3_600), PhotoBackupSpokenDuration(count: 1, unit: .hours))
    }

    func testHoursRoundToTheNearestHourUpToTwoDays() {
        XCTAssertEqual(spoken(Int(2.4 * 3_600)), PhotoBackupSpokenDuration(count: 2, unit: .hours))
        XCTAssertEqual(spoken(Int(2.6 * 3_600)), PhotoBackupSpokenDuration(count: 3, unit: .hours))
        XCTAssertEqual(spoken(30 * 3_600), PhotoBackupSpokenDuration(count: 30, unit: .hours))
        XCTAssertEqual(spoken(47 * 3_600), PhotoBackupSpokenDuration(count: 47, unit: .hours))
    }

    func testLongTimesAreDays() {
        XCTAssertEqual(spoken(48 * 3_600), PhotoBackupSpokenDuration(count: 2, unit: .days))
        XCTAssertEqual(spoken(100 * 3_600), PhotoBackupSpokenDuration(count: 4, unit: .days))
        XCTAssertEqual(spoken(40 * 86_400), PhotoBackupSpokenDuration(count: 40, unit: .days))
    }
}
