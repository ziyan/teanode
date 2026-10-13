import XCTest

@testable import TeaNodeKit

final class RecordingUploadSchedulingTests: XCTestCase {
    func testFailedUploadDoesNotImmediatelyRepeatAndOtherAccountsContinue() {
        var scheduling = RecordingUploadScheduling()
        scheduling.enqueue("first-account")
        scheduling.enqueue("second-account")
        XCTAssertEqual(scheduling.next(), "first-account")
        // A foreground refresh while the part is in flight must not create a retry loop after failure.
        scheduling.enqueue("first-account")
        scheduling.remove("first-account")
        XCTAssertEqual(scheduling.next(), "second-account")
        XCTAssertNil(scheduling.next())
        scheduling.enqueue("first-account")
        XCTAssertEqual(scheduling.next(), "first-account")
        XCTAssertTrue(scheduling.isEmpty)
    }
    func testVerifiedReceiptRequeuesFairlyAndOnlyOnce() {
        var scheduling = RecordingUploadScheduling()
        scheduling.enqueue("first-account")
        scheduling.enqueue("second-account")
        XCTAssertEqual(scheduling.next(), "first-account")
        scheduling.enqueue("first-account")
        scheduling.enqueue("first-account")
        XCTAssertEqual(scheduling.next(), "second-account")
        XCTAssertEqual(scheduling.next(), "first-account")
        XCTAssertNil(scheduling.next())
    }
}
