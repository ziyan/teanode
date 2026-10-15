import Foundation
import XCTest

@testable import TeaNodeKit

final class FinanceSpendingTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(_ fake: FakeServer) throws -> [String: Any]? {
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        return sent?["variables"] as? [String: Any]
    }

    func testAMonthIsReadDayByDayBesideTheMonthBefore() async throws {
        let body = #"{"data":{"SpendingByDay":{"month":"2026-03","compareMonth":"2026-02","reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"monthDays":[{"spentOn":"2026-03-01","spendingAmount":"12.00","cumulativeSpendingAmount":"12.00"},{"spentOn":"2026-03-02","spendingAmount":"8.50","cumulativeSpendingAmount":"20.50"}],"compareMonthDays":[{"spentOn":"2026-02-01","spendingAmount":"30.00","cumulativeSpendingAmount":"30.00"}]}}}"#
        let (connection, fake) = try connection(answering: body)
        let read = try await connection.spendingByDay(month: "2026-03", compareMonth: "2026-02")
        let byDay = try XCTUnwrap(read)
        XCTAssertEqual(byDay.monthDays?.count, 2)
        XCTAssertEqual(byDay.monthDays?.last??.cumulativeSpendingAmount, "20.50")
        XCTAssertEqual(byDay.compareMonthDays?.first??.spentOn, "2026-02-01")
        let sent = try variables(fake)
        XCTAssertEqual(sent?["month"] as? String, "2026-03")
        XCTAssertEqual(sent?["compareMonth"] as? String, "2026-02")
    }

    func testAYearsBudgetsAndSavingAreAskedForByTheYear() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"BudgetStatus":{"month":null,"year":"2025","asOf":"2025-12-31","spendingCategories":[],"incomeCategories":[]},"SavingSummary":{"reportingCurrencyCode":"USD","incomeAmount":"1000.00","spendingAmount":"400.00","savingAmount":"600.00"}}}"#)
        let budgets = try await connection.budgetStatus(year: "2025")
        XCTAssertEqual(budgets?.year, "2025")
        XCTAssertEqual(try variables(fake)?["year"] as? String, "2025")
        XCTAssertNil(try variables(fake)?["month"])
        let saving = try await connection.savingSummary(year: "2025")
        XCTAssertEqual(saving?.savingAmount, "600.00")
        XCTAssertEqual(try variables(fake)?["year"] as? String, "2025")
    }
}
