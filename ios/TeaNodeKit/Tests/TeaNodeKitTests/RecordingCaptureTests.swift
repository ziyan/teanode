import Foundation
import XCTest

@testable import TeaNodeKit

final class RecordingCaptureTests: XCTestCase {
    func testPendingRolloverIsDrainedOnceAfterJournalWriteAndStopWins() {
        let recorderId = UUID()
        var transitions = RecordingCaptureTransitions()
        transitions.enqueue(.rollover(recorderId: recorderId, wasSuccessful: true))
        XCTAssertEqual(transitions.next(), .rollover(recorderId: recorderId, wasSuccessful: true))
        XCTAssertNil(transitions.next())
        transitions.enqueue(.rollover(recorderId: recorderId, wasSuccessful: true))
        transitions.enqueue(.pause(isInterruption: false))
        transitions.enqueue(.pause(isInterruption: true))
        XCTAssertFalse(transitions.shouldStartNextSegment)
        XCTAssertEqual(transitions.next(), .pause(isInterruption: true))
        XCTAssertNil(transitions.next())
        transitions.enqueue(.rollover(recorderId: recorderId, wasSuccessful: true))
        transitions.enqueue(.pause(isInterruption: true))
        transitions.enqueue(.stop)
        XCTAssertFalse(transitions.shouldStartNextSegment)
        XCTAssertEqual(transitions.next(), .stop)
        XCTAssertNil(transitions.next())
    }

}
