import Foundation

// The combined spending of somebody who shares all of their finance: the
// server answers with one row per month, spending category and currency, and
// the screen shows a month or a year of it the way the person's own Spending
// does. The sums are made here so they can be tested.

/// SharedSpendingLine is one spending category over a month or a year: what
/// went out of it, less what came back in.
public struct SharedSpendingLine: Identifiable, Hashable, Sendable {
    /// Empty for money with no category yet.
    public let spendingCategoryName: String
    public let spentAmount: Decimal
    public let financeTransactionCount: Int

    public var id: String { spendingCategoryName }

    public init(spendingCategoryName: String, spentAmount: Decimal, financeTransactionCount: Int) {
        self.spendingCategoryName = spendingCategoryName
        self.spentAmount = spentAmount
        self.financeTransactionCount = financeTransactionCount
    }
}

/// SharedSpendingMonth is a month's money out and money in, added up over
/// every spending category.
public struct SharedSpendingMonth: Identifiable, Hashable, Sendable {
    /// "2026-09".
    public let spendingMonth: String
    public let moneyOut: Decimal
    public let moneyIn: Decimal

    public var id: String { spendingMonth }

    public init(spendingMonth: String, moneyOut: Decimal, moneyIn: Decimal) {
        self.spendingMonth = spendingMonth
        self.moneyOut = moneyOut
        self.moneyIn = moneyIn
    }
}

/// SharedSpendingSummary adds up the rows of a person's combined spending.
public enum SharedSpendingSummary {
    /// The currencies the rows are in, the one with the most money out first.
    public static func currencyCodes(in rows: [SharedFinanceSpendingRow]) -> [String] {
        var outByCurrency: [String: Decimal] = [:]
        for row in rows {
            outByCurrency[row.currencyCode, default: 0] += FinanceMoney.decimal(row.moneyOut) ?? 0
        }
        return outByCurrency.keys.sorted { left, right in
            let leftOut = outByCurrency[left] ?? 0
            let rightOut = outByCurrency[right] ?? 0
            return leftOut == rightOut ? left < right : leftOut > rightOut
        }
    }

    /// Each month that has rows in the currency, earliest first.
    public static func months(in rows: [SharedFinanceSpendingRow], currencyCode: String) -> [SharedSpendingMonth] {
        var outByMonth: [String: Decimal] = [:]
        var inByMonth: [String: Decimal] = [:]
        for row in rows where row.currencyCode == currencyCode {
            outByMonth[row.spendingMonth, default: 0] += FinanceMoney.decimal(row.moneyOut) ?? 0
            inByMonth[row.spendingMonth, default: 0] += FinanceMoney.decimal(row.moneyIn) ?? 0
        }
        let monthTexts = Set(outByMonth.keys).union(inByMonth.keys).sorted()
        return monthTexts.map { monthText in
            SharedSpendingMonth(spendingMonth: monthText, moneyOut: outByMonth[monthText] ?? 0, moneyIn: inByMonth[monthText] ?? 0)
        }
    }

    /// The months of a period added up: "2026-09" for a month, "2026" for a
    /// year.
    public static func total(of months: [SharedSpendingMonth], period: String) -> SharedSpendingMonth {
        let inPeriod = months.filter { $0.spendingMonth.hasPrefix(period) }
        return SharedSpendingMonth(
            spendingMonth: period, moneyOut: inPeriod.reduce(Decimal(0)) { $0 + $1.moneyOut },
            moneyIn: inPeriod.reduce(Decimal(0)) { $0 + $1.moneyIn })
    }

    /// Where the money of a period went, largest first. A category counts
    /// what went out of it less what came back in, as the person's own
    /// Spending counts it; money with no category yet counts what went out
    /// alone. A category that took in more than it spent is left out, which
    /// leaves out income.
    public static func lines(in rows: [SharedFinanceSpendingRow], currencyCode: String, period: String) -> [SharedSpendingLine] {
        var outByCategory: [String: Decimal] = [:]
        var inByCategory: [String: Decimal] = [:]
        var countByCategory: [String: Int] = [:]
        for row in rows where row.currencyCode == currencyCode && row.spendingMonth.hasPrefix(period) {
            outByCategory[row.spendingCategoryName, default: 0] += FinanceMoney.decimal(row.moneyOut) ?? 0
            inByCategory[row.spendingCategoryName, default: 0] += FinanceMoney.decimal(row.moneyIn) ?? 0
            countByCategory[row.spendingCategoryName, default: 0] += row.financeTransactionCount
        }
        var lines: [SharedSpendingLine] = []
        for (categoryName, moneyOut) in outByCategory {
            let moneyIn = inByCategory[categoryName] ?? 0
            let spentAmount = categoryName.isEmpty ? moneyOut : moneyOut - moneyIn
            if spentAmount > 0 {
                lines.append(
                    SharedSpendingLine(
                        spendingCategoryName: categoryName, spentAmount: spentAmount, financeTransactionCount: countByCategory[categoryName] ?? 0))
            }
        }
        return lines.sorted { left, right in
            left.spentAmount == right.spentAmount ? left.spendingCategoryName < right.spendingCategoryName : left.spentAmount > right.spentAmount
        }
    }

    /// The latest month that has rows, "2026-09".
    public static func latestMonth(in rows: [SharedFinanceSpendingRow]) -> String? {
        rows.map(\.spendingMonth).max()
    }

    /// The first year that has rows.
    public static func firstYear(in rows: [SharedFinanceSpendingRow]) -> Int? {
        rows.compactMap { Int($0.spendingMonth.prefix(4)) }.min()
    }
}
