import Foundation
import XCTest

@testable import TeaNodeKit

final class SharedFinanceSpendingSummaryTests: XCTestCase {
    private func row(_ month: String, _ category: String, out moneyOut: String, in moneyIn: String = "0", currency: String = "USD", count: Int = 1)
        -> SharedFinanceSpendingRow
    {
        let json = #"{"spendingMonth":"\#(month)","spendingCategoryName":"\#(category)","currencyCode":"\#(currency)","moneyOut":"\#(moneyOut)","moneyIn":"\#(moneyIn)","financeTransactionCount":\#(count)}"#
        return try! JSONDecoder().decode(SharedFinanceSpendingRow.self, from: Data(json.utf8))
    }

    private var rows: [SharedFinanceSpendingRow] {
        [
            row("2026-09", "Groceries", out: "412.30", count: 14),
            row("2026-09", "Dining", out: "128.50", count: 6),
            row("2026-09", "Pay", out: "0", in: "3200", count: 2),
            row("2026-09", "", out: "20", count: 1),
            row("2026-08", "Groceries", out: "388.90", count: 12),
            row("2026-08", "Dining", out: "50", in: "10", count: 2),
            row("2025-12", "Groceries", out: "100", count: 3),
            row("2026-09", "Groceries", out: "90", currency: "EUR", count: 2),
        ]
    }

    func testTheCurrencyWithTheMostMoneyOutComesFirst() {
        XCTAssertEqual(SharedSpendingSummary.currencyCodes(in: rows), ["USD", "EUR"])
        XCTAssertEqual(SharedSpendingSummary.currencyCodes(in: []), [])
    }

    func testMonthsAreAddedUpPerCurrencyEarliestFirst() {
        let months = SharedSpendingSummary.months(in: rows, currencyCode: "USD")
        XCTAssertEqual(months.map(\.spendingMonth), ["2025-12", "2026-08", "2026-09"])
        XCTAssertEqual(months[2].moneyOut, Decimal(string: "560.8"))
        XCTAssertEqual(months[2].moneyIn, 3200)
        XCTAssertEqual(SharedSpendingSummary.months(in: rows, currencyCode: "EUR").count, 1)
    }

    func testAPeriodIsAMonthOrAYear() {
        let months = SharedSpendingSummary.months(in: rows, currencyCode: "USD")
        XCTAssertEqual(SharedSpendingSummary.total(of: months, period: "2026-09").moneyOut, Decimal(string: "560.8"))
        let year = SharedSpendingSummary.total(of: months, period: "2026")
        XCTAssertEqual(year.moneyOut, Decimal(string: "560.8")! + Decimal(string: "438.9")!)
        XCTAssertEqual(year.moneyIn, 3210)
        XCTAssertEqual(SharedSpendingSummary.total(of: months, period: "2024").moneyOut, 0)
    }

    func testWhereItWentCountsNetSpendingAndLeavesOutIncome() {
        let lines = SharedSpendingSummary.lines(in: rows, currencyCode: "USD", period: "2026-09")
        XCTAssertEqual(lines.map(\.spendingCategoryName), ["Groceries", "Dining", ""])
        XCTAssertEqual(lines[0].spentAmount, Decimal(string: "412.3"))
        XCTAssertEqual(lines[0].financeTransactionCount, 14)
        XCTAssertFalse(lines.contains { $0.spendingCategoryName == "Pay" })
        let august = SharedSpendingSummary.lines(in: rows, currencyCode: "USD", period: "2026-08")
        XCTAssertEqual(august.first { $0.spendingCategoryName == "Dining" }?.spentAmount, 40)
    }

    func testAYearAddsItsMonthsOfACategoryTogether() {
        let lines = SharedSpendingSummary.lines(in: rows, currencyCode: "USD", period: "2026")
        XCTAssertEqual(lines.first?.spendingCategoryName, "Groceries")
        XCTAssertEqual(lines.first?.spentAmount, Decimal(string: "801.2"))
        XCTAssertEqual(lines.first?.financeTransactionCount, 26)
    }

    func testMoneyWithNoCategoryCountsWhatWentOutAlone() {
        let uncategorized = [row("2026-09", "", out: "30", in: "5")]
        XCTAssertEqual(SharedSpendingSummary.lines(in: uncategorized, currencyCode: "USD", period: "2026-09").first?.spentAmount, 30)
    }

    func testTheLatestMonthAndFirstYear() {
        XCTAssertEqual(SharedSpendingSummary.latestMonth(in: rows), "2026-09")
        XCTAssertEqual(SharedSpendingSummary.firstYear(in: rows), 2025)
        XCTAssertNil(SharedSpendingSummary.latestMonth(in: []))
        XCTAssertNil(SharedSpendingSummary.firstYear(in: []))
    }
}
