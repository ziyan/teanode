import Foundation
import XCTest

@testable import TeaNodeKit

final class FinanceManageTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(of fake: FakeServer) throws -> [String: Any] {
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        return try XCTUnwrap(sent?["variables"] as? [String: Any])
    }

    func testAccountTradesArePagedAndNamed() async throws {
        let body = #"{"data":{"FinanceTrades":{"financeTrades":[{"id":"trade-1","financeAccountId":"account-1","financeSecurityId":"security-1","financeSecurity":{"tickerSymbol":"INVX","securityName":"Invented Index Fund"},"providerTradeId":"p1","tradedOn":"2026-09-02","tradeKind":"buy","tradeSubkind":null,"tradedQuantity":"2","unitPrice":"100","tradeAmount":"-200","feeAmount":"0","currencyCode":"USD","description":"Bought 2"},{"id":"trade-2","financeAccountId":"account-1","financeSecurity":null,"tradedOn":"2026-09-01","tradeKind":"transfer","tradeAmount":"0","description":"Moved in"}],"totalCount":60}}}"#
        let (connection, fake) = try connection(answering: body)
        let page = try await connection.financeTrades(financeAccountId: "account-1", limit: 50, offset: 50)
        XCTAssertEqual(page.totalCount, 60)
        let trades = page.financeTrades.compactMap { $0 }
        XCTAssertEqual(trades.map(\.shownName), ["INVX", "Moved in"])
        XCTAssertEqual(trades.first?.tradedQuantity, "2")
        let sent = try variables(of: fake)
        XCTAssertEqual(sent["financeAccountId"] as? String, "account-1")
        XCTAssertEqual(sent["limit"] as? Int, 50)
        XCTAssertEqual(sent["offset"] as? Int, 50)
        XCTAssertNil(sent["financeSecurityId"])
    }

    func testAStatementAccountIsRenamedAndDeleted() async throws {
        let (renaming, renameFake) = try connection(answering: #"{"data":{"RenameStatementAccount":{"id":"account-1","accountName":"Invented Card","accountMask":"4321"}}}"#)
        let renamed = try await renaming.renameStatementAccount("account-1", accountName: "Invented Card", accountMask: nil)
        XCTAssertEqual(renamed?.accountName, "Invented Card")
        XCTAssertEqual(renamed?.accountMask, "4321")
        let renameSent = try variables(of: renameFake)
        XCTAssertEqual(renameSent["accountName"] as? String, "Invented Card")
        XCTAssertNil(renameSent["accountMask"])

        let (deleting, deleteFake) = try connection(answering: #"{"data":{"DeleteStatementAccount":{"financeAccountId":"account-1","deletedTransactionCount":12,"deletedAssetCount":1}}}"#)
        let deleted = try await deleting.deleteStatementAccount("account-1")
        XCTAssertEqual(deleted?.deletedTransactionCount, 12)
        XCTAssertEqual(try variables(of: deleteFake)["financeAccountId"] as? String, "account-1")
    }

    func testSpendingRulesAreProposedForTransactions() async throws {
        let body = #"{"data":{"ProposeSpendingRules":{"spendingRuleProposals":[{"matchText":"Corner Market","spendingCategoryId":"groceries","financeTransactionCount":4,"changedTransactionCount":1,"aheadOfSpendingRule":{"id":"rule-1","matchText":"Market","spendingCategoryId":"shopping"}},{"matchText":"Sample Cafe","spendingCategoryId":"groceries","financeTransactionCount":2,"changedTransactionCount":0,"aheadOfSpendingRule":null}],"tooGenericMatchTextCount":1,"changingNumberMatchTextCount":0,"overLimitMatchTextCount":0}}}"#
        let (connection, fake) = try connection(answering: body)
        let proposed = try await connection.proposeSpendingRules(transactionIds: ["transaction-1", "transaction-2"], categoryId: "groceries")
        let proposals = try XCTUnwrap(proposed).proposals
        XCTAssertEqual(proposals.map(\.matchText), ["Corner Market", "Sample Cafe"])
        XCTAssertEqual(proposals.first?.aheadOfSpendingRule?.matchText, "Market")
        XCTAssertNil(proposals.last?.aheadOfSpendingRule)
        XCTAssertEqual(proposed?.tooGenericMatchTextCount, 1)
        let sent = try variables(of: fake)
        XCTAssertEqual(sent["financeTransactionIds"] as? [String], ["transaction-1", "transaction-2"])
        XCTAssertEqual(sent["spendingCategoryId"] as? String, "groceries")
    }

    func testAConnectionMadeElsewhereIsBroughtIn() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ImportFinanceCredential":{"id":"source-1"}}}"#)
        try await connection.importFinanceCredential(providerKind: "simplefin", credential: "https://invented-user:invented-pass@bridge.example.com/simplefin", institutionName: nil)
        let sent = try variables(of: fake)
        XCTAssertEqual(sent["providerKind"] as? String, "simplefin")
        XCTAssertNotNil(sent["credential"])
        XCTAssertNil(sent["institutionName"])

        let (listing, _) = try self.connection(answering: #"{"data":{"FinanceProviders":[{"providerKind":"plaid","isBrowserRequired":true},{"providerKind":"simplefin","isBrowserRequired":false}]}}"#)
        let kinds = try await listing.financeProviderKinds()
        XCTAssertEqual(kinds, ["plaid", "simplefin"])
    }

    func testARowsFileBecomesTheImportsArguments() throws {
        let file = #"{"institutionName":"Sample Bank","statementAccountKind":"bank","currencyCode":"USD","accountName":null,"ledgerBalanceAmount":1250.5,"transactionRows":[{"postedOn":"2026-09-02","description":"Corner Market","amount":-12.5,"runningBalanceAmount":"1250.50"},{"postedOn":"2026-09-03","description":"Payroll","amount":1000}],"monthlyTotals":[{"totalMonth":"2026-09","totalAmount":987.5}]}"#
        let variables = try XCTUnwrap(TransactionRowsFile.variables(from: Data(file.utf8)))
        XCTAssertNil(variables["accountName"])
        XCTAssertEqual(variables["ledgerBalanceAmount"], .string("1250.5"))
        guard case .array(let rows)? = variables["transactionRows"], case .object(let first) = rows[0], case .object(let second) = rows[1] else {
            return XCTFail("the rows were lost")
        }
        XCTAssertEqual(first["amount"], .string("-12.5"))
        XCTAssertEqual(first["runningBalanceAmount"], .string("1250.50"))
        XCTAssertEqual(second["amount"], .string("1000"))
        guard case .array(let totals)? = variables["monthlyTotals"], case .object(let total) = totals[0] else {
            return XCTFail("the totals were lost")
        }
        XCTAssertEqual(total["totalAmount"], .string("987.5"))
    }

    func testAFileThatIsNotRowsIsRefused() {
        XCTAssertNil(TransactionRowsFile.variables(from: Data("[1, 2]".utf8)))
        XCTAssertNil(TransactionRowsFile.variables(from: Data(#"{"institutionName":"Sample Bank"}"#.utf8)))
        XCTAssertNil(TransactionRowsFile.variables(from: Data("not json".utf8)))
    }

    func testRowsArePreviewedThenImported() async throws {
        let preview = #"{"data":{"PreviewImportTransactions":{"financeAccountId":null,"accountName":"Sample Bank Checking","isNewAccount":true,"accountMatch":"","currencyCode":"USD","newTransactionRows":[{"rowNumber":1,"postedOn":"2026-09-02","description":"Corner Market","amount":"-12.50","hasNearbyStoredTransaction":false},{"rowNumber":2,"postedOn":"2026-09-03","description":"Payroll","amount":"1000.00","hasNearbyStoredTransaction":true}],"presentTransactionRows":[],"firstPostedOn":"2026-09-02","lastPostedOn":"2026-09-03","moneyInAmount":"1000.00","moneyOutAmount":"-12.50","verificationSummary":"balances chain"}}}"#
        let (previewing, previewFake) = try connection(answering: preview)
        let arguments: [String: JSONValue] = ["institutionName": .string("Sample Bank"), "transactionRows": .array([])]
        let previewed = try await previewing.previewImportTransactions(arguments)
        XCTAssertEqual(previewed?.newRows.count, 2)
        XCTAssertEqual(previewed?.newRows.last?.hasNearbyStoredTransaction, true)
        XCTAssertEqual(previewed?.presentRows.count, 0)
        XCTAssertEqual(previewed?.isNewAccount, true)
        let previewSent = try JSONSerialization.jsonObject(with: try XCTUnwrap(previewFake.sent.last?.httpBody)) as? [String: Any]
        XCTAssertTrue((previewSent?["query"] as? String)?.contains("PreviewImportTransactions") == true)

        let (importing, importFake) = try connection(answering: #"{"data":{"ImportTransactions":{"importedAt":"2026-10-14T12:00:00Z","addedTransactionCount":2,"updatedTransactionCount":0,"unchangedTransactionCount":0,"financeAccountNames":["Sample Bank Checking"],"importErrorMessage":""}}}"#)
        let imported = try await importing.importTransactions(arguments)
        XCTAssertEqual(imported?.addedTransactionCount, 2)
        XCTAssertEqual(imported?.isImported, true)
        XCTAssertEqual(try variables(of: importFake)["institutionName"] as? String, "Sample Bank")
    }
}
