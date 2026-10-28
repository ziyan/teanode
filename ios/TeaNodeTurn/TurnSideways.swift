import XCTest

/// Turns the simulator on its side, for CI's photographs of the iPad held
/// sideways. Nothing but XCTest can turn a simulator, and it stays turned
/// once the test is done, so the screens after it are taken sideways.
final class TurnSideways: XCTestCase {
    @MainActor
    func testTurnsTheDeviceSideways() {
        XCUIDevice.shared.orientation = .landscapeLeft
        XCTAssertEqual(XCUIDevice.shared.orientation, .landscapeLeft)
    }
}
