#if DEBUG
    import Foundation
    import TeaNodeKit
    import UIKit

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
            if query.contains("ReadTaxDocument") {
                return #"{"data":{"ReadTaxDocument":{"agentAttachmentIds":["attachment-demo"],"agentJobIds":["job-demo"]}}}"#
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

        /// The file of an import, read from its address
        /// (`/api/v1/agent/attachments/demo-import-picture` or `demo-import-document`):
        /// an invented sheet drawn here, a picture or a one page PDF, since no
        /// server is behind the demo. Nil for any other request.
        static func financeImportFile(for request: URLRequest) -> Data? {
            guard let path = request.url?.path else { return nil }
            let bounds = CGRect(x: 0, y: 0, width: 300, height: 400)
            func drawSheet(_ context: CGContext) {
                UIColor(white: 0.97, alpha: 1).setFill()
                context.fill(bounds)
                UIColor(white: 0.55, alpha: 1).setFill()
                for row in 0..<9 {
                    context.fill(CGRect(x: 24, y: 40 + CGFloat(row) * 36, width: row == 0 ? 160 : 252, height: 10))
                }
            }
            if path == "/api/v1/agent/attachments/demo-import-picture" {
                let format = UIGraphicsImageRendererFormat()
                format.scale = 1
                return UIGraphicsImageRenderer(bounds: bounds, format: format).jpegData(withCompressionQuality: 0.8) { context in
                    drawSheet(context.cgContext)
                }
            }
            if path == "/api/v1/agent/attachments/demo-import-document" {
                return UIGraphicsPDFRenderer(bounds: bounds).pdfData { context in
                    context.beginPage()
                    drawSheet(context.cgContext)
                }
            }
            return nil
        }

        /// The files read into finance in the background, as the Imports screen
        /// shows them (`-TeaNodeScreen finance-imports`, and `finance-import-detail`
        /// for the page of the one that imported nothing): today one waiting,
        /// one being read and one done, yesterday five more. The times are
        /// today's and yesterday's, whenever the screenshot is taken.
        private static var demoFinanceImportsJSON: String {
            let calendar = Calendar.current
            let startOfToday = calendar.startOfDay(for: Date())
            let startOfYesterday = calendar.date(byAdding: .day, value: -1, to: startOfToday) ?? startOfToday
            let formatter = ISO8601DateFormatter()
            func time(_ start: Date, hour: Int, minute: Int) -> String {
                formatter.string(from: calendar.date(byAdding: .minute, value: hour * 60 + minute, to: start) ?? start)
            }
            func row(
                id: String, kind: String, source: String, status: String, fileName: String, queuedAt: String, finishedAt: String,
                outcome: String, attachmentId: String, contentType: String, mailboxItemId: String
            ) -> [String: Any] {
                [
                    "agentJobId": id, "financeImportKind": kind, "financeImportSource": source, "agentJobStatus": status,
                    "fileName": fileName, "queuedAt": queuedAt, "finishedAt": finishedAt, "outcomeText": outcome,
                    "runConversationId": "", "agentAttachmentId": attachmentId, "attachmentContentType": contentType,
                    "mailboxItemId": mailboxItemId,
                ]
            }
            let imports: [[String: Any]] = [
                row(id: "job-queued", kind: "statement", source: "upload", status: "queued", fileName: "picture-1.jpg",
                    queuedAt: time(startOfToday, hour: 9, minute: 41), finishedAt: "", outcome: "",
                    attachmentId: "demo-import-picture", contentType: "image/jpeg", mailboxItemId: ""),
                row(id: "job-running", kind: "statement", source: "upload", status: "running", fileName: "picture-1.jpg",
                    queuedAt: time(startOfToday, hour: 9, minute: 26), finishedAt: "", outcome: "Reading the statement picture-1.jpg",
                    attachmentId: "demo-import-picture", contentType: "image/jpeg", mailboxItemId: ""),
                row(id: "job-added", kind: "statement", source: "upload", status: "done", fileName: "picture-1.jpg",
                    queuedAt: time(startOfToday, hour: 8, minute: 5), finishedAt: time(startOfToday, hour: 8, minute: 6),
                    outcome: "2 transactions added to Example Bank ··4821",
                    attachmentId: "demo-import-picture", contentType: "image/jpeg", mailboxItemId: ""),
                row(id: "job-nothing", kind: "statement", source: "upload", status: "done", fileName: "picture-1.jpg",
                    queuedAt: time(startOfYesterday, hour: 21, minute: 15), finishedAt: time(startOfYesterday, hour: 21, minute: 16),
                    outcome: "Imported nothing: it shows no account number, and no account at Example Bank was imported before, so it is not clear which account it is. Share a screenshot that shows the account's number or its last digits",
                    attachmentId: "demo-import-picture", contentType: "image/jpeg", mailboxItemId: ""),
                row(id: "job-known", kind: "statement", source: "upload", status: "done", fileName: "picture-1.jpg",
                    queuedAt: time(startOfYesterday, hour: 18, minute: 40), finishedAt: time(startOfYesterday, hour: 18, minute: 41),
                    outcome: "Nothing new in picture-1.jpg: Example Bank ··4821 has it all already",
                    attachmentId: "demo-import-picture", contentType: "image/jpeg", mailboxItemId: ""),
                row(id: "job-mailed", kind: "tax_document", source: "mail", status: "done", fileName: "form-1099.pdf",
                    queuedAt: time(startOfYesterday, hour: 14, minute: 5), finishedAt: time(startOfYesterday, hour: 14, minute: 7),
                    outcome: "Tax form 1099-INT from Example Bank read: 41.20 of interest",
                    attachmentId: "", contentType: "", mailboxItemId: "demo-mail-1"),
                row(id: "job-document", kind: "statement", source: "upload", status: "done", fileName: "statement-october.pdf",
                    queuedAt: time(startOfYesterday, hour: 9, minute: 12), finishedAt: time(startOfYesterday, hour: 9, minute: 14),
                    outcome: "14 transactions added to Example Bank ··4821",
                    attachmentId: "demo-import-document", contentType: "application/pdf", mailboxItemId: ""),
            ]
            let body: [String: Any] = ["data": ["FinanceImports": ["financeImports": imports, "hasMore": false, "totalCount": imports.count]]]
            guard let data = try? JSONSerialization.data(withJSONObject: body), let text = String(data: data, encoding: .utf8) else { return "{}" }
            return text
        }

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
