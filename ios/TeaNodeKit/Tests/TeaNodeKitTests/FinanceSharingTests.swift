import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

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
        XCTAssertEqual(Documents.financeSharingDocuments.count, 12)
        for name in ["allFinanceShares", "shareAllFinance", "unshareAllFinance", "sharedAllFinance", "sharedAllFinanceSpending", "sharedAllFinanceTransactions"] {
            XCTAssertNotNil(Documents.financeSharingDocuments[name], name)
        }
    }

    func testAShareOfAllFinanceToAPersonAndToACircle() throws {
        let person = try decode(
            AllFinanceShareView.self,
            #"{"id":"s1","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending","createdAt":"2026-10-30T09:00:00Z"}"#
        )
        XCTAssertEqual(person.target, .person("u-ben"))
        XCTAssertEqual(person.level, .spending)
        let circle = try decode(
            AllFinanceShareView.self,
            #"{"id":"s2","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"transactions","createdAt":"2026-10-30T09:00:00Z"}"#
        )
        XCTAssertEqual(circle.target, .circle("c1"))
        XCTAssertEqual(circle.level, .transactions)
        let unknown = try decode(AllFinanceShareView.self, #"{"id":"s3","financeShareLevel":"everything"}"#)
        XCTAssertNil(unknown.level)
    }

    func testAPersonWhoSharesAllTheirFinanceNamesNoAccount() throws {
        let sharer = try decode(
            SharedAllFinance.self, #"{"ownerUserId":"u-mira","ownerUsername":"mira","ownerName":"Mira","financeShareLevel":"spending"}"#)
        XCTAssertEqual(sharer.id, "u-mira")
        XCTAssertEqual(sharer.ownerShownName, "Mira")
        XCTAssertEqual(sharer.level, .spending)
        XCTAssertFalse(sharer.isSharingTransactions)
        let withTransactions = try decode(
            SharedAllFinance.self, #"{"ownerUserId":"u-mira","ownerUsername":"mira","ownerName":"","financeShareLevel":"transactions"}"#)
        XCTAssertTrue(withTransactions.isSharingTransactions)
        XCTAssertEqual(withTransactions.ownerShownName, "mira")
        for document in [
            Documents.sharedAllFinance, Documents.sharedAllFinanceSpending, Documents.sharedAllFinanceTransactions, Documents.allFinanceShares,
            Documents.shareAllFinance,
        ] {
            XCTAssertFalse(document.contains("accountName"))
            XCTAssertFalse(document.contains("currentBalance"))
            XCTAssertFalse(document.contains("financeAccountId"))
        }
    }

    func testTheDocumentsForAllFinanceUseTheServersOperations() {
        XCTAssertTrue(Documents.allFinanceShares.contains("ListFinanceShares"))
        XCTAssertFalse(Documents.allFinanceShares.contains("ListFinanceAccountShares"))
        XCTAssertTrue(Documents.shareAllFinance.contains("ShareFinance(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel)"))
        XCTAssertTrue(Documents.unshareAllFinance.contains("UnshareFinance(financeShareId: $financeShareId)"))
        XCTAssertTrue(Documents.sharedAllFinance.contains("ListSharedFinance(ownerUserId: $ownerUserId)"))
        XCTAssertTrue(Documents.sharedAllFinanceSpending.contains("ListSharedFinanceSpending(ownerUserId: $ownerUserId)"))
        XCTAssertTrue(Documents.sharedAllFinanceTransactions.contains("ListSharedFinanceTransactions(ownerUserId: $ownerUserId"))
    }

    func testTheOperationsForAllFinanceSendWhatTheServerTakes() async throws {
        let server = try ServerAddress(parsing: "mail.example.com")
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        func variables(of request: URLRequest) throws -> [String: JSONValue] {
            let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
            guard case .object(let variables) = body["variables"] else { return [:] }
            return variables
        }

        let sharing = FakeServer { _ in
            (200, #"{"data":{"ShareFinance":{"id":"s1","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"transactions","createdAt":"2026-10-30T09:00:00Z"}}}"#)
        }
        let made = try await Connection(account: account, transport: sharing).shareAllFinance(with: .person("u-ben"), level: .transactions)
        XCTAssertEqual(made.level, .transactions)
        var sent = try variables(of: sharing.sent[0])
        XCTAssertEqual(sent["granteeUserId"], .string("u-ben"))
        XCTAssertEqual(sent["granteeCircleId"], .null)
        XCTAssertEqual(sent["financeShareLevel"], .string("transactions"))

        let circleSharing = FakeServer { _ in
            (200, #"{"data":{"ShareFinance":{"id":"s2","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"spending","createdAt":"2026-10-30T09:00:00Z"}}}"#)
        }
        try await Connection(account: account, transport: circleSharing).shareAllFinance(with: .circle("c1"), level: .spending)
        sent = try variables(of: circleSharing.sent[0])
        XCTAssertEqual(sent["granteeUserId"], .null)
        XCTAssertEqual(sent["granteeCircleId"], .string("c1"))

        let listing = FakeServer { _ in (200, #"{"data":{"ListFinanceShares":[{"id":"s1","granteeUserId":"u-ben","financeShareLevel":"spending"}]}}"#) }
        let shares = try await Connection(account: account, transport: listing).allFinanceShares()
        XCTAssertEqual(shares.map(\.id), ["s1"])

        let unsharing = FakeServer { _ in (200, #"{"data":{"UnshareFinance":true}}"#) }
        try await Connection(account: account, transport: unsharing).unshareAllFinance(financeShareId: "s1")
        XCTAssertEqual(try variables(of: unsharing.sent[0])["financeShareId"], .string("s1"))

        let reading = FakeServer { _ in (200, #"{"data":{"ListSharedFinance":[{"ownerUserId":"u-mira","ownerUsername":"mira","ownerName":"Mira","financeShareLevel":"spending"}]}}"#) }
        let sharers = try await Connection(account: account, transport: reading).sharedAllFinance()
        XCTAssertEqual(sharers.first?.ownerUserId, "u-mira")
        XCTAssertEqual(try variables(of: reading.sent[0])["ownerUserId"], .null)

        let spending = FakeServer { _ in
            (200, #"{"data":{"ListSharedFinanceSpending":[{"spendingMonth":"2026-09","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"42.1700","moneyIn":"0.0000","financeTransactionCount":1}]}}"#)
        }
        let rows = try await Connection(account: account, transport: spending).sharedFinanceSpending(ownerUserId: "u-mira")
        XCTAssertEqual(rows.count, 1)
        XCTAssertEqual(try variables(of: spending.sent[0])["ownerUserId"], .string("u-mira"))
        XCTAssertNil(try variables(of: spending.sent[0])["financeAccountId"])

        let transactions = FakeServer { _ in (200, #"{"data":{"ListSharedFinanceTransactions":{"nextCursor":"","totalCount":0,"financeTransactions":[]}}}"#) }
        let page = try await Connection(account: account, transport: transactions).sharedFinanceTransactions(ownerUserId: "u-mira", limit: 25, offset: 50)
        XCTAssertEqual(page.totalCount, 0)
        sent = try variables(of: transactions.sent[0])
        XCTAssertEqual(sent["ownerUserId"], .string("u-mira"))
        XCTAssertEqual(sent["limit"], .number(25))
        XCTAssertEqual(sent["offset"], .number(50))
    }

    func testAPersonCardListsTheAccountsTheyShare() throws {
        let card = try decode(
            PersonCard.self,
            #"{"canMessage":true,"finance":{"sectionState":"shown","hasMore":true,"financeAccounts":[{"financeAccountId":"a1","ownerUserId":"u","ownerUsername":"sam","financeShareLevel":"balance","accountName":"Card","currencyCode":"USD","creditUsage":null}]}}"#
        )
        XCTAssertEqual(card.finance?.sectionState, .shown)
        XCTAssertEqual(card.finance?.financeAccounts.first?.accountName, "Card")
        XCTAssertEqual(card.finance?.hasMore, true)
        XCTAssertNil(card.finance?.allFinanceLevel)
        let allCard = try decode(
            PersonCard.self,
            #"{"canMessage":true,"finance":{"sectionState":"shown","allFinanceShareLevel":"spending","financeAccounts":[],"hasMore":false}}"#)
        XCTAssertEqual(allCard.finance?.allFinanceLevel, .spending)
        let emptyLevel = try decode(
            PersonCard.self, #"{"canMessage":true,"finance":{"sectionState":"shown","allFinanceShareLevel":"","financeAccounts":[],"hasMore":false}}"#)
        XCTAssertNil(emptyLevel.finance?.allFinanceLevel)
        XCTAssertTrue(Documents.personCard.contains("allFinanceShareLevel"))
        let noFinance = try decode(PersonCard.self, #"{"canMessage":false}"#)
        XCTAssertNil(noFinance.finance)
        XCTAssertTrue(Documents.personCard.contains("financeAccounts"))
        XCTAssertFalse(Documents.personCard.contains("availableBalance"))
    }
}
