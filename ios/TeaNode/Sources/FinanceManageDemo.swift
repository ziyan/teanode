#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented accounts, trades, rule suggestions and rows that the
    // screenshots of the finance management screens photograph
    // (`-TeaNodeDemo -TeaNodeScreen finance-trades`, `finance-import-preview`,
    // and the screens for typing in a receipt, a paystub or a tax form).
    // DemoServer answers the app's GraphQL here for the operations only
    // these screens ask.

    extension DemoServer {
        /// What the finance management screens ask beyond the rest of the demo,
        /// or nil for any other question.
        static func financeManageAnswer(query: String) -> String? {
            if query.contains("FinanceTrades") {
                return #"{"data":{"FinanceTrades":{"financeTrades":[\#(demoTradeJSON)],"totalCount":4}}}"#
            }
            if query.contains("ProposeSpendingRules") {
                return demoRuleProposalsJSON
            }
            if query.contains("PreviewImportTransactions") {
                return demoRowsPreviewJSON
            }
            if query.contains("ImportTransactions") {
                return #"{"data":{"ImportTransactions":{"importedAt":"2026-10-14T12:00:00Z","statementImportOrigin":"transaction_rows","addedTransactionCount":3,"updatedTransactionCount":0,"unchangedTransactionCount":1,"financeAccountNames":["Sample Bank Checking"],"importErrorMessage":""}}}"#
            }
            if query.contains("RenameStatementAccount") {
                return #"{"data":{"RenameStatementAccount":{"id":"account-statement","accountName":"Everyday card","accountMask":"4321"}}}"#
            }
            if query.contains("DeleteStatementAccount") {
                return #"{"data":{"DeleteStatementAccount":{"financeAccountId":"account-statement","deletedTransactionCount":12,"deletedAssetCount":0}}}"#
            }
            if query.contains("ImportFinanceCredential") {
                return #"{"data":{"ImportFinanceCredential":{"id":"source-imported"}}}"#
            }
            if query.contains("ReadStatement") {
                return #"{"data":{"ReadStatement":{"agentAttachmentId":"attachment-demo","agentJobId":"job-demo"}}}"#
            }
            if query.contains("FinanceImports") {
                return demoFinanceImportsJSON
            }
            if query.contains("StatementImport") {
                return #"{"data":{"StatementImport":{"sourceId":"source-statements","importAddress":"robin+statements-demo0token01@example.com","isEnabled":true,"lastStatementImport":{"importedAt":"2026-10-12T09:30:00Z","statementImportOrigin":"mail","addedTransactionCount":3,"updatedTransactionCount":0,"unchangedTransactionCount":1,"financeAccountNames":["Everyday card ··4321"],"importErrorMessage":""}}}}"#
            }
            // Every `finance-` screen opens on the accounts, as AppShell does.
            let isManageScreen = screen?.hasPrefix("finance-") == true
            if isManageScreen, query.contains("FinanceTransactions") {
                return #"{"data":{"FinanceTransactions":{"financeTransactions":[\#(demoTransactionJSON(id: "transaction-1", description: "Corner Market 0412")),\#(demoTransactionJSON(id: "transaction-2", description: "Corner Market 0977"))],"totalCount":2,"leftOutDuplicateCount":0}}}"#
            }
            if isManageScreen, query.contains("FinanceAccounts") {
                return #"{"data":{"FinanceAccounts":[\#(demoAccountJSON(id: "account-checking", institution: "Sample Bank", name: "Checking", mask: "1111", kind: "depository", provider: "simplefin", balance: "2450.10")),\#(demoAccountJSON(id: "account-brokerage", institution: "Sample Brokerage", name: "Brokerage", mask: "2222", kind: "investment", provider: "simplefin", balance: "18200.00")),\#(demoAccountJSON(id: "account-statement", institution: "Sample Card", name: "Everyday card", mask: "4321", kind: "credit", provider: "statement", balance: "-312.40"))]}}"#
            }
            return nil
        }

        /// The files read into finance in the background, as the Imports screen
        /// shows them (`-TeaNodeScreen finance-imports`): one of each status.
        private static let demoFinanceImportsJSON = #"{"data":{"FinanceImports":{"financeImports":[{"agentJobId":"job-1","financeImportKind":"statement","financeImportSource":"upload","agentJobStatus":"queued","fileName":"picture-1.jpg","queuedAt":"2026-10-14T12:00:00Z","finishedAt":"","outcomeText":"","runConversationId":""},{"agentJobId":"job-2","financeImportKind":"statement","financeImportSource":"upload","agentJobStatus":"running","fileName":"picture-2.jpg","queuedAt":"2026-10-14T11:50:00Z","finishedAt":"","outcomeText":"","runConversationId":"conversation-2"},{"agentJobId":"job-3","financeImportKind":"statement","financeImportSource":"upload","agentJobStatus":"done","fileName":"statement-october.pdf","queuedAt":"2026-10-13T08:00:00Z","finishedAt":"2026-10-13T08:02:10Z","outcomeText":"Added 14 transactions to Example Bank Checking ending 4321, and skipped 2 that were already there.","runConversationId":"conversation-3"},{"agentJobId":"job-4","financeImportKind":"receipt","financeImportSource":"photo","agentJobStatus":"done","fileName":"","queuedAt":"2026-10-12T17:20:00Z","finishedAt":"2026-10-12T17:21:00Z","outcomeText":"Read a receipt from Corner Market for 24.10 and matched it to a charge.","runConversationId":"conversation-4"},{"agentJobId":"job-5","financeImportKind":"statement","financeImportSource":"mail","agentJobStatus":"dead","fileName":"picture-3.jpg","queuedAt":"2026-10-11T09:00:00Z","finishedAt":"2026-10-11T09:01:00Z","outcomeText":"The picture was too blurry to read the amounts. Send a sharper one.","runConversationId":"conversation-5"},{"agentJobId":"job-6","financeImportKind":"tax_document","financeImportSource":"upload","agentJobStatus":"cancelled","fileName":"paystub.pdf","queuedAt":"2026-10-10T10:00:00Z","finishedAt":"2026-10-10T10:05:00Z","outcomeText":"","runConversationId":""}],"hasMore":false,"totalCount":6}}}"#

        private static func demoAccountJSON(id: String, institution: String, name: String, mask: String, kind: String, provider: String, balance: String) -> String {
            #"{"id":"\#(id)","sourceId":"source-\#(provider)","providerKind":"\#(provider)","institutionName":"\#(institution)","accountName":"\#(name)","accountMask":"\#(mask)","accountKind":"\#(kind)","currencyCode":"USD","currentBalance":"\#(balance)","availableBalance":null,"balanceAt":null,"isSignInRequired":false,"reportingCurrencyCode":"USD","convertedCurrentBalance":"\#(balance)"}"#
        }

        private static func demoTransactionJSON(id: String, description: String) -> String {
            #"{"id":"\#(id)","financeAccountId":"account-checking","postedOn":"2026-10-02","amount":"-24.10","currencyCode":"USD","description":"\#(description)","merchantName":"","isPending":false,"spendingCategoryId":"groceries"}"#
        }

        private static let demoTradeJSON: String = [
            ("buy", "2026-10-01", "2", "100.00", "-200.00", "0.00", "Bought 2 shares"),
            ("sell", "2026-09-18", "1.5", "102.40", "153.60", "0.00", "Sold 1.5 shares"),
            ("buy", "2026-09-02", "10", "98.00", "-980.00", "1.00", "Bought 10 shares"),
            ("transfer", "2026-08-20", "5", "", "0.00", "0.00", "Moved in from another account"),
        ].enumerated().map { index, trade in
            let unitPriceJSON = trade.3.isEmpty ? "null" : "\"\(trade.3)\""
            return #"{"id":"trade-\#(index)","financeAccountId":"account-brokerage","financeSecurityId":"security-fund","financeSecurity":{"tickerSymbol":"INVX","securityName":"Invented Index Fund"},"providerTradeId":"provider-trade-\#(index)","tradedOn":"\#(trade.1)","tradeKind":"\#(trade.0)","tradeSubkind":null,"tradedQuantity":"\#(trade.2)","unitPrice":\#(unitPriceJSON),"tradeAmount":"\#(trade.4)","feeAmount":"\#(trade.5)","currencyCode":"USD","description":"\#(trade.6)"}"#
        }.joined(separator: ",")

        private static let demoRuleProposalsJSON = #"{"data":{"ProposeSpendingRules":{"spendingRuleProposals":[{"matchText":"Corner Market","spendingCategoryId":"groceries","financeTransactionCount":2,"changedTransactionCount":1,"aheadOfSpendingRule":null},{"matchText":"Sample Pantry","spendingCategoryId":"groceries","financeTransactionCount":1,"changedTransactionCount":0,"aheadOfSpendingRule":{"id":"rule-demo","matchText":"Sample","spendingCategoryId":"shopping"}}],"tooGenericMatchTextCount":1,"changingNumberMatchTextCount":0,"overLimitMatchTextCount":0}}}"#

        private static let demoRowsPreviewJSON = #"{"data":{"PreviewImportTransactions":{"financeAccountId":null,"accountName":"Sample Bank Checking","isNewAccount":true,"accountMatch":"","currencyCode":"USD","newTransactionRows":[{"rowNumber":1,"postedOn":"2026-09-02","description":"Corner Market","amount":"-12.50","hasNearbyStoredTransaction":false},{"rowNumber":2,"postedOn":"2026-09-03","description":"Payroll","amount":"1000.00","hasNearbyStoredTransaction":false},{"rowNumber":3,"postedOn":"2026-09-05","description":"Sample Cafe","amount":"-8.75","hasNearbyStoredTransaction":true}],"presentTransactionRows":[{"rowNumber":4,"postedOn":"2026-09-01","description":"Rent","amount":"-900.00","hasNearbyStoredTransaction":false}],"firstPostedOn":"2026-09-02","lastPostedOn":"2026-09-05","moneyInAmount":"1000.00","moneyOutAmount":"-21.25","verificationSummary":"running balances chain across all rows"}}}"#

        /// The rows file the import screen starts with in the screenshot, as
        /// the command line's import-transactions reads one.
        static let demoRowsFile = #"{"institutionName":"Sample Bank","accountName":"Sample Bank Checking","statementAccountKind":"bank","currencyCode":"USD","transactionRows":[{"postedOn":"2026-09-02","description":"Corner Market","amount":"-12.50"},{"postedOn":"2026-09-03","description":"Payroll","amount":"1000.00"},{"postedOn":"2026-09-05","description":"Sample Cafe","amount":"-8.75"},{"postedOn":"2026-09-01","description":"Rent","amount":"-900.00"}]}"#
    }
#endif
