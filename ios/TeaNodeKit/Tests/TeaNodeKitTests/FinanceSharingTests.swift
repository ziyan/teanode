import Foundation
import XCTest

@testable import TeaNodeKit

final class FinanceSharingTests: XCTestCase {
    private func decode<Value: Decodable>(_ type: Value.Type, _ json: String) throws -> Value {
        try JSONDecoder().decode(type, from: Data(json.utf8))
    }

    func testEachLevelReachesTheOnesBelowIt() {
        XCTAssertTrue(FinanceShareLevel.transactions.isAtLeast(.spending))
        XCTAssertTrue(FinanceShareLevel.transactions.isAtLeast(.balance))
        XCTAssertTrue(FinanceShareLevel.spending.isAtLeast(.balance))
        XCTAssertTrue(FinanceShareLevel.balance.isAtLeast(.balance))
        XCTAssertFalse(FinanceShareLevel.balance.isAtLeast(.spending))
        XCTAssertFalse(FinanceShareLevel.spending.isAtLeast(.transactions))
        XCTAssertEqual(FinanceShareLevel.allCases, [.balance, .spending, .transactions])
    }

    func testAShareToAPersonAndOneToACircleSayWhoTheyAreWith() throws {
        let person = try decode(
            FinanceShareView.self,
            #"{"id":"s1","financeAccountId":"a1","accountName":"Checking","accountMask":"0001","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending"}"#
        )
        XCTAssertEqual(person.target, .person("u-ben"))
        XCTAssertEqual(person.level, .spending)
        XCTAssertEqual(person.shownAccountName, "Checking ··0001")
        let circle = try decode(
            FinanceShareView.self,
            #"{"id":"s2","financeAccountId":"a2","accountName":"Card","accountMask":"","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"balance"}"#
        )
        XCTAssertEqual(circle.target, .circle("c1"))
        XCTAssertEqual(circle.shownAccountName, "Card")
    }

    func testALevelTheAppDoesNotKnowIsNotOneItHasToAvoid() throws {
        let share = try decode(
            FinanceShareView.self,
            #"{"id":"s1","financeAccountId":"a1","accountName":"Checking","financeShareLevel":"everything"}"#)
        XCTAssertNil(share.level)
        let account = try decode(
            SharedFinanceAccount.self,
            #"{"financeAccountId":"a1","ownerUserId":"u","ownerUsername":"ben","financeShareLevel":"everything","accountName":"Checking","currencyCode":"USD"}"#)
        XCTAssertFalse(account.isShared(atLeast: .balance))
    }

    func testASharedAccountSaysWhatItsLevelShows() throws {
        let account = try decode(
            SharedFinanceAccount.self,
            #"{"financeAccountId":"a1","ownerUserId":"u-ben","ownerUsername":"ben","ownerName":"","financeShareLevel":"spending","accountName":"Card","accountMask":"4321","accountKind":"credit","currencyCode":"USD","currentBalance":"-100.0000","balanceAt":null,"creditUsage":{"owedAmount":"100.0000","creditLimitAmount":"400.0000","creditLimitSource":"provider","usageShare":0.25}}"#
        )
        XCTAssertEqual(account.id, "a1")
        XCTAssertEqual(account.shownName, "Card ··4321")
        XCTAssertEqual(account.ownerShownName, "ben")
        XCTAssertTrue(account.isShared(atLeast: .balance))
        XCTAssertTrue(account.isShared(atLeast: .spending))
        XCTAssertFalse(account.isShared(atLeast: .transactions))
        XCTAssertEqual(account.creditUsage?.usageShare, 0.25)
    }

    func testAPageOfSharedTransactionsHasNoDescriptionToDecode() throws {
        let page = try decode(
            SharedFinanceTransactionPage.self,
            #"{"nextCursor":"","totalCount":2,"financeTransactions":[{"id":"t1","postedOn":"2026-09-10","amount":"-42.1700","currencyCode":"USD","merchantName":"Example Grocer","spendingCategoryName":"Groceries"},{"id":"t2","postedOn":"2026-09-01","amount":"2500.0000","currencyCode":"USD","merchantName":null,"spendingCategoryName":null}]}"#
        )
        XCTAssertEqual(page.totalCount, 2)
        XCTAssertEqual(page.financeTransactions.first?.merchantName, "Example Grocer")
        XCTAssertNil(page.financeTransactions.last?.merchantName)
        XCTAssertFalse(Documents.sharedFinanceTransactions.contains("description"))
        XCTAssertFalse(Documents.sharedFinanceAccounts.contains("availableBalance"))
    }

    func testAMonthOfSpendingIsKeyedByMonthCategoryAndCurrency() throws {
        let row = try decode(
            SharedFinanceSpendingRow.self,
            #"{"spendingMonth":"2026-09","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"42.1700","moneyIn":"0.0000","financeTransactionCount":1}"#)
        XCTAssertEqual(row.id, "2026-09|Groceries|USD")
    }

    func testTheDocumentsAreInTheSchemaCheck() {
        for name in ["financeShares", "shareFinanceAccount", "unshareFinanceAccount", "sharedFinanceAccounts", "sharedFinanceSpending", "sharedFinanceTransactions"] {
            XCTAssertNotNil(Documents.financeSharingDocuments[name], name)
        }
        XCTAssertEqual(Documents.financeSharingDocuments.count, 6)
    }

    func testAPersonCardListsTheAccountsTheyShare() throws {
        let card = try decode(
            PersonCard.self,
            #"{"canMessage":true,"finance":{"sectionState":"shown","hasMore":true,"financeAccounts":[{"financeAccountId":"a1","ownerUserId":"u","ownerUsername":"sam","financeShareLevel":"balance","accountName":"Card","currencyCode":"USD","creditUsage":null}]}}"#
        )
        XCTAssertEqual(card.finance?.sectionState, .shown)
        XCTAssertEqual(card.finance?.financeAccounts.first?.accountName, "Card")
        XCTAssertEqual(card.finance?.hasMore, true)
        let noFinance = try decode(PersonCard.self, #"{"canMessage":false}"#)
        XCTAssertNil(noFinance.finance)
        XCTAssertTrue(Documents.personCard.contains("financeAccounts"))
        XCTAssertFalse(Documents.personCard.contains("availableBalance"))
    }
}
