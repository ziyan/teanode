#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented shares that the screenshots of the finance sharing screens
    // photograph (`-TeaNodeDemo -TeaNodeTab finance -TeaNodeScreen
    // finance-sharing`, `finance-person`): the person's accounts shared with
    // Sam and with a circle, and three accounts that Sam shares with the person,
    // one at each level. The other finance screens see nobody sharing.

    /// FinanceSharingDemo holds the invented shares.
    enum FinanceSharingDemo {
        /// The person in the demo's circle who shares accounts with the person.
        static let sharerId = "sam"

        static let sharesJSON =
            #"[{"id":"share-checking","financeAccountId":"account-checking","accountName":"Checking","accountMask":"1111","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending"},{"id":"share-card","financeAccountId":"account-statement","accountName":"Everyday card","accountMask":"4321","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"family","granteeCircleName":"Family","financeShareLevel":"balance"}]"#

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
            if query.contains("ListSharedFinanceAccounts") {
                let accounts = isPersonScreen ? FinanceSharingDemo.sharedAccountsJSON : "[]"
                return #"{"data":{"ListSharedFinanceAccounts":\#(accounts)}}"#
            }
            if query.contains("ListSharedFinanceSpending") {
                return #"{"data":{"ListSharedFinanceSpending":\#(FinanceSharingDemo.spendingJSON)}}"#
            }
            if query.contains("ListSharedFinanceTransactions") {
                return #"{"data":{"ListSharedFinanceTransactions":{"nextCursor":"","totalCount":4,"financeTransactions":\#(FinanceSharingDemo.transactionsJSON)}}}"#
            }
            if query.contains("UnshareFinanceAccount") {
                return #"{"data":{"UnshareFinanceAccount":true}}"#
            }
            if query.contains("ShareFinanceAccount(") {
                // The list is fixed, so a share made is acknowledged and not shown.
                let variables = requestVariables(body)
                let level = variables["financeShareLevel"] as? String ?? "balance"
                return #"{"data":{"ShareFinanceAccount":{"id":"share-made","financeAccountId":"account-checking","accountName":"Checking","accountMask":"1111","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"\#(level)"}}}"#
            }
            if query.contains("ListFinanceAccountShares") {
                let shares = screen == "finance-sharing" ? FinanceSharingDemo.sharesJSON : "[]"
                return #"{"data":{"ListFinanceAccountShares":\#(shares)}}"#
            }
            return nil
        }
    }
#endif
