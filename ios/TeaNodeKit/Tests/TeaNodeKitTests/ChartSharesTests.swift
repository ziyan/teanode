import Foundation
import XCTest

@testable import TeaNodeKit

final class ChartSharesTests: XCTestCase {
    func testGroupsBeyondTheLimitShareOneSlice() {
        let amounts: [Decimal] = [900, 800, 700, 600, 500, 400, 300, 200, 100, 50, 25]
        let sliceAmounts = ChartShares.sliceAmounts(amounts)
        XCTAssertEqual(sliceAmounts.count, ChartShares.ownSliceLimit + 1)
        XCTAssertEqual(Array(sliceAmounts.prefix(ChartShares.ownSliceLimit)), Array(amounts.prefix(ChartShares.ownSliceLimit)))
        XCTAssertEqual(sliceAmounts.last, 175)
        XCTAssertEqual(ChartShares.sliceIndex(ofRank: 0), 0)
        XCTAssertEqual(ChartShares.sliceIndex(ofRank: 7), 7)
        XCTAssertEqual(ChartShares.sliceIndex(ofRank: 8), 8)
        XCTAssertEqual(ChartShares.sliceIndex(ofRank: 10), 8)
    }

    func testFewGroupsKeepTheirOwnSlices() {
        let amounts: [Decimal] = [300, 200, 100]
        XCTAssertEqual(ChartShares.sliceAmounts(amounts), amounts)
        let exactlyAtTheLimit = Array(repeating: Decimal(10), count: ChartShares.ownSliceLimit)
        XCTAssertEqual(ChartShares.sliceAmounts(exactlyAtTheLimit), exactlyAtTheLimit)
    }

    func testAnAngleFindsItsSlice() {
        let amounts = [50.0, 30.0, 20.0]
        XCTAssertEqual(ChartShares.sliceIndex(atRunningTotal: 0, amounts: amounts), 0)
        XCTAssertEqual(ChartShares.sliceIndex(atRunningTotal: 49.9, amounts: amounts), 0)
        XCTAssertEqual(ChartShares.sliceIndex(atRunningTotal: 50.1, amounts: amounts), 1)
        XCTAssertEqual(ChartShares.sliceIndex(atRunningTotal: 99, amounts: amounts), 2)
        // A touch reported past the end, from rounding, is still the last slice.
        XCTAssertEqual(ChartShares.sliceIndex(atRunningTotal: 100.5, amounts: amounts), 2)
        XCTAssertNil(ChartShares.sliceIndex(atRunningTotal: 10, amounts: []))
    }

    func testATouchPicksTheNearestPoint() {
        let start = Date(timeIntervalSince1970: 1_800_000_000)
        let dates = [0.0, 10.0, 30.0].map { start.addingTimeInterval($0 * 86_400) }
        XCTAssertEqual(ChartShares.nearestIndex(to: start.addingTimeInterval(-86_400), in: dates), 0)
        XCTAssertEqual(ChartShares.nearestIndex(to: start.addingTimeInterval(4 * 86_400), in: dates), 0)
        XCTAssertEqual(ChartShares.nearestIndex(to: start.addingTimeInterval(6 * 86_400), in: dates), 1)
        XCTAssertEqual(ChartShares.nearestIndex(to: start.addingTimeInterval(25 * 86_400), in: dates), 2)
        XCTAssertNil(ChartShares.nearestIndex(to: start, in: []))
    }

    func testAxisAmountsAreShortAndInTheirCurrency() {
        let text = FinanceMoney.axisText(12_500, currencyCode: "USD")
        XCTAssertTrue(text.contains("12"), text)
        XCTAssertTrue(text.contains("K"), text)
        XCTAssertFalse(text.contains("500"), text)
        XCTAssertTrue(FinanceMoney.axisText(950, currencyCode: "").contains("950"))
    }
}
