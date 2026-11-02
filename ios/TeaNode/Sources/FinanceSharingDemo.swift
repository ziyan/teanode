#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented shares that the screenshots of the finance sharing screens
    // photograph (`-TeaNodeDemo -TeaNodeTab finance -TeaNodeScreen
    // finance-sharing`, `finance-person`): the person's accounts shared with
    // Sam and with a circle, and three accounts that Sam shares with the person,
    // one at each level. `finance-person-all` adds Mira, an invented person who
    // shares all of her finance with the person at the spending level, and
    // `finance-sharing` lists two shares of all the person's finance. The other
    // finance screens see nobody sharing.

    /// FinanceSharingDemo holds the invented shares.
    enum FinanceSharingDemo {
        /// The person in the demo's circle who shares accounts with the person.
        static let sharerId = "sam"

        static let sharesJSON =
            #"[{"id":"share-checking","financeAccountId":"account-checking","accountName":"Checking","accountMask":"1111","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending"},{"id":"share-card","financeAccountId":"account-statement","accountName":"Everyday card","accountMask":"4321","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"family","granteeCircleName":"Family","financeShareLevel":"balance"}]"#

        /// The invented person who shares all of her finance with the person.
        static let wholeSharerId = "mira"

        /// The person's own shares of all their finance: one with a person and
        /// one with a circle.
        static let allSharesJSON =
            #"[{"id":"share-all-sam","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"transactions","createdAt":"2026-10-20T09:00:00Z"},{"id":"share-all-family","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"family","granteeCircleName":"Family","financeShareLevel":"spending","createdAt":"2026-10-21T09:00:00Z"}]"#

        /// Who shares all of their finance with the person.
        static let sharedAllJSON =
            #"[{"ownerUserId":"mira","ownerUsername":"mira","ownerName":"Mira","financeShareLevel":"spending"}]"#

        /// Mira's money out and in by month and category, all her accounts together.
        static let combinedSpendingJSON: String = {
            let months = ["2026-04", "2026-05", "2026-06", "2026-07", "2026-08", "2026-09", "2026-10"]
            let categories: [(name: String, out: Double, count: Int)] = [
                ("Housing", 1450, 1), ("Groceries", 410, 14), ("Dining", 135, 6), ("Transport", 96, 5), ("Utilities", 118, 3), ("Health", 54, 2),
            ]
            var rows: [String] = []
            for (monthIndex, month) in months.enumerated() {
                for (categoryIndex, category) in categories.enumerated() {
                    // A little different each month, so the bars are not all one height.
                    let moneyOut = category.out * (1 + Double((monthIndex * 3 + categoryIndex * 2) % 7) / 40)
                    rows.append(
                        #"{"spendingMonth":"\#(month)","spendingCategoryName":"\#(category.name)","currencyCode":"USD","moneyOut":"\#(String(format: "%.4f", moneyOut))","moneyIn":"0.0000","financeTransactionCount":\#(category.count)}"#
                    )
                }
                rows.append(
                    #"{"spendingMonth":"\#(month)","spendingCategoryName":"Paycheck","currencyCode":"USD","moneyOut":"0.0000","moneyIn":"\#(String(format: "%.4f", 3900 + Double(monthIndex) * 25))","financeTransactionCount":2}"#
                )
            }
            return "[" + rows.joined(separator: ",") + "]"
        }()

        static let sharedAccountsJSON =
            #"[{"financeAccountId":"shared-checking","ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","financeShareLevel":"transactions","accountName":"Joint checking","accountMask":"7788","accountKind":"depository","currencyCode":"USD","currentBalance":"2450.1000","balanceAt":"2026-10-20T08:00:00Z","creditUsage":null},{"financeAccountId":"shared-savings","ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","financeShareLevel":"spending","accountName":"Savings","accountMask":"9900","accountKind":"depository","currencyCode":"USD","currentBalance":"18200.0000","balanceAt":"2026-10-20T08:00:00Z","creditUsage":null},{"financeAccountId":"shared-card","ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","financeShareLevel":"balance","accountName":"Store card","accountMask":"5566","accountKind":"credit","currencyCode":"USD","currentBalance":"-100.0000","balanceAt":"2026-10-20T08:00:00Z","creditUsage":{"owedAmount":"100.0000","creditLimitAmount":"400.0000","creditLimitSource":"provider","usageShare":0.25}}]"#

        /// The first two of those, which a person's card lists.
        static let cardAccountsJSON =
            #"[{"financeAccountId":"shared-checking","ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","financeShareLevel":"transactions","accountName":"Joint checking","accountMask":"7788","accountKind":"depository","currencyCode":"USD","currentBalance":"2450.1000","balanceAt":"2026-10-20T08:00:00Z","creditUsage":null},{"financeAccountId":"shared-card","ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","financeShareLevel":"balance","accountName":"Store card","accountMask":"5566","accountKind":"credit","currencyCode":"USD","currentBalance":"-100.0000","balanceAt":"2026-10-20T08:00:00Z","creditUsage":{"owedAmount":"100.0000","creditLimitAmount":"400.0000","creditLimitSource":"provider","usageShare":0.25}}]"#

        static let spendingJSON =
            #"[{"spendingMonth":"2026-09","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"412.3000","moneyIn":"0.0000","financeTransactionCount":14},{"spendingMonth":"2026-09","spendingCategoryName":"Dining","currencyCode":"USD","moneyOut":"128.5000","moneyIn":"0.0000","financeTransactionCount":6},{"spendingMonth":"2026-09","spendingCategoryName":"","currencyCode":"USD","moneyOut":"0.0000","moneyIn":"3200.0000","financeTransactionCount":2},{"spendingMonth":"2026-08","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"388.9000","moneyIn":"0.0000","financeTransactionCount":12}]"#

        static let transactionsJSON =
            #"[{"id":"shared-transaction-1","postedOn":"2026-09-28","amount":"-42.1700","currencyCode":"USD","merchantName":"Corner Market","spendingCategoryName":"Groceries"},{"id":"shared-transaction-2","postedOn":"2026-09-26","amount":"-18.4000","currencyCode":"USD","merchantName":"Lakeside Diner","spendingCategoryName":"Dining"},{"id":"shared-transaction-3","postedOn":"2026-09-15","amount":"1600.0000","currencyCode":"USD","merchantName":"","spendingCategoryName":""},{"id":"shared-transaction-4","postedOn":"2026-09-12","amount":"-63.9500","currencyCode":"USD","merchantName":"Sample Pantry","spendingCategoryName":"Groceries"}]"#
    }

    extension DemoServer {
        /// What the finance sharing screens ask, or nil for any other question.
        static func financeSharingAnswer(query: String, body: Data?) -> String? {
            let isPersonScreen = screen?.hasPrefix("finance-person") == true
            let isWholeSharerScreen = screen == "finance-person-all"
            if query.contains("ListSharedFinanceAccounts") {
                let accounts = isPersonScreen ? FinanceSharingDemo.sharedAccountsJSON : "[]"
                return #"{"data":{"ListSharedFinanceAccounts":\#(accounts)}}"#
            }
            if query.contains("ListSharedFinance(") {
                let sharers = isWholeSharerScreen ? FinanceSharingDemo.sharedAllJSON : "[]"
                return #"{"data":{"ListSharedFinance":\#(sharers)}}"#
            }
            if query.contains("ListSharedFinanceSpending") {
                // Asked of a person's whole finance (an owner, no account) or of one account.
                let isWhole = requestVariables(body)["ownerUserId"] is String
                let rows = isWhole ? FinanceSharingDemo.combinedSpendingJSON : FinanceSharingDemo.spendingJSON
                return #"{"data":{"ListSharedFinanceSpending":\#(rows)}}"#
            }
            if query.contains("ListSharedFinanceTransactions") {
                return #"{"data":{"ListSharedFinanceTransactions":{"nextCursor":"","totalCount":4,"financeTransactions":\#(FinanceSharingDemo.transactionsJSON)}}}"#
            }
            if query.contains("UnshareFinanceAccount") {
                return #"{"data":{"UnshareFinanceAccount":true}}"#
            }
            if query.contains("UnshareFinance(") {
                return #"{"data":{"UnshareFinance":true}}"#
            }
            if query.contains("ShareFinance(") {
                // The list is fixed, so a share made is acknowledged and not shown.
                let variables = requestVariables(body)
                let level = variables["financeShareLevel"] as? String ?? "spending"
                return #"{"data":{"ShareFinance":{"id":"share-all-made","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"\#(level)","createdAt":"2026-10-22T09:00:00Z"}}}"#
            }
            if query.contains("ShareFinanceAccount(") {
                // The list is fixed, so a share made is acknowledged and not shown.
                let variables = requestVariables(body)
                let level = variables["financeShareLevel"] as? String ?? "balance"
                return #"{"data":{"ShareFinanceAccount":{"id":"share-made","financeAccountId":"account-checking","accountName":"Checking","accountMask":"1111","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"\#(level)"}}}"#
            }
            if query.contains("ListFinanceShares") {
                let shares = screen?.hasPrefix("finance-sharing") == true ? FinanceSharingDemo.allSharesJSON : "[]"
                return #"{"data":{"ListFinanceShares":\#(shares)}}"#
            }
            if query.contains("ListFinanceAccountShares") {
                let shares = screen?.hasPrefix("finance-sharing") == true ? FinanceSharingDemo.sharesJSON : "[]"
                return #"{"data":{"ListFinanceAccountShares":\#(shares)}}"#
            }
            return nil
        }
    }
#endif
