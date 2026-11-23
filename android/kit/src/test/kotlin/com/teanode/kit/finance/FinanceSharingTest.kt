package com.teanode.kit.finance

import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.JsonNull
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class FinanceSharingTest {
    private fun <Value> decode(serializer: KSerializer<Value>, json: String): Value = TeaNodeJson.decodeFromString(serializer, json)

    @Test
    fun eachLevelReachesTheOnesBelowIt() {
        assertTrue(FinanceShareLevel.Transactions.isAtLeast(FinanceShareLevel.Spending))
        assertTrue(FinanceShareLevel.Transactions.isAtLeast(FinanceShareLevel.Balance))
        assertTrue(FinanceShareLevel.Spending.isAtLeast(FinanceShareLevel.Balance))
        assertTrue(FinanceShareLevel.Balance.isAtLeast(FinanceShareLevel.Balance))
        assertFalse(FinanceShareLevel.Balance.isAtLeast(FinanceShareLevel.Spending))
        assertFalse(FinanceShareLevel.Spending.isAtLeast(FinanceShareLevel.Transactions))
        assertEquals(listOf(FinanceShareLevel.Balance, FinanceShareLevel.Spending, FinanceShareLevel.Transactions), FinanceShareLevel.entries)
    }

    @Test
    fun aShareToAPersonAndOneToACircleSayWhoTheyAreWith() {
        val person = decode(
            FinanceShareView.serializer(),
            """{"id":"s1","financeAccountId":"a1","accountName":"Checking","accountMask":"0001","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending"}""",
        )
        assertEquals(ShareTarget.Person("u-ben"), person.target)
        assertEquals(FinanceShareLevel.Spending, person.level)
        assertEquals("Checking ··0001", person.shownAccountName)
        val circle = decode(
            FinanceShareView.serializer(),
            """{"id":"s2","financeAccountId":"a2","accountName":"Card","accountMask":"","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"balance"}""",
        )
        assertEquals(ShareTarget.Circle("c1"), circle.target)
        assertEquals("Card", circle.shownAccountName)
    }

    @Test
    fun aLevelTheAppDoesNotKnowIsNotOneItHasToAvoid() {
        val share = decode(FinanceShareView.serializer(), """{"id":"s1","financeAccountId":"a1","accountName":"Checking","financeShareLevel":"everything"}""")
        assertNull(share.level)
        val account = decode(
            SharedFinanceAccount.serializer(),
            """{"financeAccountId":"a1","ownerUserId":"u","ownerUsername":"ben","financeShareLevel":"everything","accountName":"Checking","currencyCode":"USD"}""",
        )
        assertFalse(account.isShared(FinanceShareLevel.Balance))
    }

    @Test
    fun aSharedAccountSaysWhatItsLevelShows() {
        val account = decode(
            SharedFinanceAccount.serializer(),
            """{"financeAccountId":"a1","ownerUserId":"u-ben","ownerUsername":"ben","ownerName":"","financeShareLevel":"spending","accountName":"Card","accountMask":"4321","accountKind":"credit","currencyCode":"USD","currentBalance":"-100.0000","balanceAt":null,"creditUsage":{"owedAmount":"100.0000","creditLimitAmount":"400.0000","creditLimitSource":"provider","usageShare":0.25}}""",
        )
        assertEquals("a1", account.id)
        assertEquals("Card ··4321", account.shownName)
        assertEquals("ben", account.ownerShownName)
        assertTrue(account.isShared(FinanceShareLevel.Balance))
        assertTrue(account.isShared(FinanceShareLevel.Spending))
        assertFalse(account.isShared(FinanceShareLevel.Transactions))
        assertEquals(0.25, account.creditUsage?.usageShare)
    }

    @Test
    fun aPageOfSharedTransactionsHasNoDescriptionToDecode() {
        val page = decode(
            SharedFinanceTransactionPage.serializer(),
            """{"nextCursor":"","totalCount":2,"financeTransactions":[{"id":"t1","postedOn":"2026-09-10","amount":"-42.1700","currencyCode":"USD","merchantName":"Example Grocer","spendingCategoryName":"Groceries"},{"id":"t2","postedOn":"2026-09-01","amount":"2500.0000","currencyCode":"USD","merchantName":null,"spendingCategoryName":null}]}""",
        )
        assertEquals(2, page.totalCount)
        assertEquals("Example Grocer", page.financeTransactions.first().merchantName)
        assertNull(page.financeTransactions.last().merchantName)
        assertFalse(FinanceSharingDocuments.SHARED_FINANCE_TRANSACTIONS.contains("description"))
        assertFalse(FinanceSharingDocuments.SHARED_FINANCE_ACCOUNTS.contains("availableBalance"))
    }

    @Test
    fun aMonthOfSpendingIsKeyedByMonthCategoryAndCurrency() {
        val row = decode(
            SharedFinanceSpendingRow.serializer(),
            """{"spendingMonth":"2026-09","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"42.1700","moneyIn":"0.0000","financeTransactionCount":1}""",
        )
        assertEquals("2026-09|Groceries|USD", row.id)
    }

    @Test
    fun aShareOfAllFinanceToAPersonAndToACircle() {
        val person = decode(
            AllFinanceShareView.serializer(),
            """{"id":"s1","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"spending","createdAt":"2026-10-30T09:00:00Z"}""",
        )
        assertEquals(ShareTarget.Person("u-ben"), person.target)
        assertEquals(FinanceShareLevel.Spending, person.level)
        val circle = decode(
            AllFinanceShareView.serializer(),
            """{"id":"s2","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"transactions","createdAt":"2026-10-30T09:00:00Z"}""",
        )
        assertEquals(ShareTarget.Circle("c1"), circle.target)
        assertEquals(FinanceShareLevel.Transactions, circle.level)
        assertNull(decode(AllFinanceShareView.serializer(), """{"id":"s3","financeShareLevel":"everything"}""").level)
    }

    @Test
    fun aPersonWhoSharesAllTheirFinanceNamesNoAccount() {
        val sharer = decode(SharedAllFinance.serializer(), """{"ownerUserId":"u-mira","ownerUsername":"mira","ownerName":"Mira","financeShareLevel":"spending"}""")
        assertEquals("u-mira", sharer.id)
        assertEquals("Mira", sharer.ownerShownName)
        assertEquals(FinanceShareLevel.Spending, sharer.level)
        assertFalse(sharer.isSharingTransactions)
        val withTransactions = decode(SharedAllFinance.serializer(), """{"ownerUserId":"u-mira","ownerUsername":"mira","ownerName":"","financeShareLevel":"transactions"}""")
        assertTrue(withTransactions.isSharingTransactions)
        assertEquals("mira", withTransactions.ownerShownName)
        for (document in listOf(
            FinanceSharingDocuments.SHARED_ALL_FINANCE, FinanceSharingDocuments.SHARED_ALL_FINANCE_SPENDING,
            FinanceSharingDocuments.SHARED_ALL_FINANCE_TRANSACTIONS, FinanceSharingDocuments.ALL_FINANCE_SHARES,
            FinanceSharingDocuments.SHARE_ALL_FINANCE,
        )) {
            assertFalse(document.contains("accountName"))
            assertFalse(document.contains("currentBalance"))
            assertFalse(document.contains("financeAccountId"))
        }
    }

    @Test
    fun theDocumentsForAllFinanceUseTheServersOperations() {
        assertTrue(FinanceSharingDocuments.ALL_FINANCE_SHARES.contains("ListFinanceShares"))
        assertFalse(FinanceSharingDocuments.ALL_FINANCE_SHARES.contains("ListFinanceAccountShares"))
        assertTrue(FinanceSharingDocuments.SHARE_ALL_FINANCE.contains("ShareFinance(granteeUserId: \$granteeUserId, granteeCircleId: \$granteeCircleId, financeShareLevel: \$financeShareLevel)"))
        assertTrue(FinanceSharingDocuments.UNSHARE_ALL_FINANCE.contains("UnshareFinance(financeShareId: \$financeShareId)"))
        assertTrue(FinanceSharingDocuments.SHARED_ALL_FINANCE.contains("ListSharedFinance(ownerUserId: \$ownerUserId)"))
        assertTrue(FinanceSharingDocuments.SHARED_ALL_FINANCE_SPENDING.contains("ListSharedFinanceSpending(ownerUserId: \$ownerUserId)"))
        assertTrue(FinanceSharingDocuments.SHARED_ALL_FINANCE_TRANSACTIONS.contains("ListSharedFinanceTransactions(ownerUserId: \$ownerUserId"))
    }

    @Test
    fun theOperationsForAllFinanceSendWhatTheServerTakes() = runBlocking {
        val (sharing, sharingFake) = connectionAnswering(
            """{"data":{"ShareFinance":{"id":"s1","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","granteeCircleId":null,"granteeCircleName":"","financeShareLevel":"transactions","createdAt":"2026-10-30T09:00:00Z"}}}""",
        )
        val made = sharing.shareAllFinance(ShareTarget.Person("u-ben"), FinanceShareLevel.Transactions)
        assertEquals(FinanceShareLevel.Transactions, made?.level)
        var sent = variablesOf(sharingFake.sent[0])
        assertEquals("u-ben", sent.text("granteeUserId"))
        assertEquals(JsonNull, sent["granteeCircleId"])
        assertEquals("transactions", sent.text("financeShareLevel"))
        val (circleSharing, circleFake) = connectionAnswering(
            """{"data":{"ShareFinance":{"id":"s2","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"c1","granteeCircleName":"Home","financeShareLevel":"spending","createdAt":"2026-10-30T09:00:00Z"}}}""",
        )
        circleSharing.shareAllFinance(ShareTarget.Circle("c1"), FinanceShareLevel.Spending)
        sent = variablesOf(circleFake.sent[0])
        assertEquals(JsonNull, sent["granteeUserId"])
        assertEquals("c1", sent.text("granteeCircleId"))
        val (listing, _) = connectionAnswering("""{"data":{"ListFinanceShares":[{"id":"s1","granteeUserId":"u-ben","financeShareLevel":"spending"}]}}""")
        assertEquals(listOf("s1"), listing.allFinanceShares().map { share -> share.id })
        val (unsharing, unsharingFake) = connectionAnswering("""{"data":{"UnshareFinance":true}}""")
        unsharing.unshareAllFinance("s1")
        assertEquals("s1", variablesOf(unsharingFake.sent[0]).text("financeShareId"))
        val (reading, readingFake) = connectionAnswering("""{"data":{"ListSharedFinance":[{"ownerUserId":"u-mira","ownerUsername":"mira","ownerName":"Mira","financeShareLevel":"spending"}]}}""")
        assertEquals("u-mira", reading.sharedAllFinance().first().ownerUserId)
        assertEquals(JsonNull, variablesOf(readingFake.sent[0])["ownerUserId"])
        val (spending, spendingFake) = connectionAnswering(
            """{"data":{"ListSharedFinanceSpending":[{"spendingMonth":"2026-09","spendingCategoryName":"Groceries","currencyCode":"USD","moneyOut":"42.1700","moneyIn":"0.0000","financeTransactionCount":1}]}}""",
        )
        assertEquals(1, spending.sharedAllFinanceSpending("u-mira").size)
        assertEquals("u-mira", variablesOf(spendingFake.sent[0]).text("ownerUserId"))
        assertNull(variablesOf(spendingFake.sent[0])["financeAccountId"])
        val (transactions, transactionsFake) = connectionAnswering("""{"data":{"ListSharedFinanceTransactions":{"nextCursor":"","totalCount":0,"financeTransactions":[]}}}""")
        val page = transactions.sharedAllFinanceTransactions("u-mira", limit = 25, offset = 50)
        assertEquals(0, page.totalCount)
        sent = variablesOf(transactionsFake.sent[0])
        assertEquals("u-mira", sent.text("ownerUserId"))
        assertEquals(25, sent.number("limit"))
        assertEquals(50, sent.number("offset"))
    }
}
