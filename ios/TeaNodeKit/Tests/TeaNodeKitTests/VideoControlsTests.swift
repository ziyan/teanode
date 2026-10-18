import XCTest

@testable import TeaNodeKit

final class VideoControlsTests: XCTestCase {
    func testOnlyAPlayingVideoLeftAloneHidesItsControls() {
        XCTAssertTrue(VideoControls.shouldHide(isPlaying: true, hasEnded: false, isScrubbing: false, isVoiceOverRunning: false))
        XCTAssertFalse(VideoControls.shouldHide(isPlaying: false, hasEnded: false, isScrubbing: false, isVoiceOverRunning: false))
        XCTAssertFalse(VideoControls.shouldHide(isPlaying: false, hasEnded: true, isScrubbing: false, isVoiceOverRunning: false))
        XCTAssertFalse(VideoControls.shouldHide(isPlaying: true, hasEnded: true, isScrubbing: false, isVoiceOverRunning: false))
        XCTAssertFalse(VideoControls.shouldHide(isPlaying: true, hasEnded: false, isScrubbing: true, isVoiceOverRunning: false))
        XCTAssertFalse(VideoControls.shouldHide(isPlaying: true, hasEnded: false, isScrubbing: false, isVoiceOverRunning: true))
    }

    func testTheDelayBeforeHidingIsThreeSeconds() {
        XCTAssertEqual(VideoControls.hideDelaySeconds, 3)
    }

    func testPlayedFractionIsKeptBetweenZeroAndOne() {
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: 15, durationSeconds: 60), 0.25, accuracy: 0.0001)
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: 70, durationSeconds: 60), 1)
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: -2, durationSeconds: 60), 0)
    }

    func testPlayedFractionOfAVideoWithNoKnownLengthIsZero() {
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: 5, durationSeconds: 0), 0)
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: 5, durationSeconds: .nan), 0)
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: .nan, durationSeconds: 60), 0)
        XCTAssertEqual(VideoControls.playedFraction(elapsedSeconds: 5, durationSeconds: .infinity), 0)
    }

    func testAPointOnTheBarIsAMomentOfTheVideo() {
        XCTAssertEqual(VideoControls.seconds(atFraction: 0.5, durationSeconds: 80), 40, accuracy: 0.0001)
        XCTAssertEqual(VideoControls.seconds(atFraction: 1.4, durationSeconds: 80), 80)
        XCTAssertEqual(VideoControls.seconds(atFraction: -0.3, durationSeconds: 80), 0)
        XCTAssertEqual(VideoControls.seconds(atFraction: 0.5, durationSeconds: 0), 0)
        XCTAssertEqual(VideoControls.seconds(atFraction: .nan, durationSeconds: 80), 0)
    }

    func testAVideoAtItsEndStartsAgainFromTheBeginning() {
        XCTAssertTrue(VideoControls.isAtEnd(elapsedSeconds: 60, durationSeconds: 60))
        XCTAssertTrue(VideoControls.isAtEnd(elapsedSeconds: 59.98, durationSeconds: 60))
        XCTAssertFalse(VideoControls.isAtEnd(elapsedSeconds: 59, durationSeconds: 60))
        XCTAssertFalse(VideoControls.isAtEnd(elapsedSeconds: 0, durationSeconds: 0))
    }

    func testMovingByAStepStaysInsideTheVideo() {
        XCTAssertEqual(VideoControls.seconds(from: 10, movingBy: 5, durationSeconds: 60), 15)
        XCTAssertEqual(VideoControls.seconds(from: 3, movingBy: -5, durationSeconds: 60), 0)
        XCTAssertEqual(VideoControls.seconds(from: 58, movingBy: 5, durationSeconds: 60), 60)
        XCTAssertEqual(VideoControls.seconds(from: 10, movingBy: 5, durationSeconds: 0), 0)
    }

    func testTimeTextCountsOnlyTheSecondsReached() {
        XCTAssertEqual(VideoControls.timeText(seconds: 0), "0:00")
        XCTAssertEqual(VideoControls.timeText(seconds: 59.9), "0:59")
        XCTAssertEqual(VideoControls.timeText(seconds: 83.4), "1:23")
        XCTAssertEqual(VideoControls.timeText(seconds: 3725), "1:02:05")
    }

    func testTimeTextOfATimeThatIsNotKnownIsZero() {
        XCTAssertEqual(VideoControls.timeText(seconds: .nan), "0:00")
        XCTAssertEqual(VideoControls.timeText(seconds: -4), "0:00")
        XCTAssertEqual(VideoControls.timeText(seconds: .infinity), "0:00")
    }
}
