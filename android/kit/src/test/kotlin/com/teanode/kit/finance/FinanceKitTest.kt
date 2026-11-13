package com.teanode.kit.finance

import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.Locale

class FinanceSpendingTest {
    @Test
    fun aMonthIsReadDayByDayBesideTheMonthBefore() = runBlocking {
        val body = """{"data":{"SpendingByDay":{"month":"2026-03","compareMonth":"2026-02","reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"monthDays":[{"spentOn":"2026-03-01","spendingAmount":"12.00","cumulativeSpendingAmount":"12.00"},{"spentOn":"2026-03-02","spendingAmount":"8.50","cumulativeSpendingAmount":"20.50"}],"compareMonthDays":[{"spentOn":"2026-02-01","spendingAmount":"30.00","cumulativeSpendingAmount":"30.00"}]}}}"""
        val (connection, fake) = connectionAnswering(body)
        val byDay = connection.spendingByDay("2026-03", "2026-02")!!
        assertEquals(2, byDay.monthDays?.size)
        assertEquals("20.50", byDay.monthDays?.last()?.cumulativeSpendingAmount)
        assertEquals("2026-02-01", byDay.compareMonthDays?.first()?.spentOn)
        val sent = variablesOf(fake.sent.last())
        assertEquals("2026-03", sent.text("month"))
        assertEquals("2026-02", sent.text("compareMonth"))
    }

    @Test
    fun aYearsBudgetsAndSavingAreAskedForByTheYear() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """{"data":{"BudgetStatus":{"month":null,"year":"2025","asOf":"2025-12-31","spendingCategories":[],"incomeCategories":[]},"SavingSummary":{"reportingCurrencyCode":"USD","incomeAmount":"1000.00","spendingAmount":"400.00","savingAmount":"600.00"}}}""",
        )
        val budgets = connection.budgetStatusOfYear("2025")
        assertEquals("2025", budgets?.year)
        assertEquals("2025", variablesOf(fake.sent.last()).text("year"))
        assertNull(variablesOf(fake.sent.last())["month"])
        val saving = connection.savingSummaryOfYear("2025")
        assertEquals("600.00", saving?.savingAmount)
        assertEquals("2025", variablesOf(fake.sent.last()).text("year"))
    }
}

class FinanceStatementTest {
    @Test
    fun theStatementImportIsReadAndItsAddressRenewed() = runBlocking {
        val body = """{"data":{"StatementImport":{"sourceId":"source-1","importAddress":"robin+statements-invented0token01@example.com","isEnabled":true,"lastStatementImport":{"importedAt":"2026-10-01T12:00:00Z","addedTransactionCount":3,"updatedTransactionCount":0,"unchangedTransactionCount":1,"financeAccountNames":["Invented Card ··1a11"],"importErrorMessage":""}},"RegenerateStatementImportAddress":{"sourceId":"source-1","importAddress":"robin+statements-invented0token02@example.com","isEnabled":true}}}"""
        val (connection, fake) = connectionAnswering(body)
        val statementImport = connection.statementImport()!!
        assertEquals("robin+statements-invented0token01@example.com", statementImport.importAddress)
        assertEquals(3, statementImport.lastStatementImport?.addedTransactionCount)
        assertEquals(true, statementImport.lastStatementImport?.isImported)
        val renewed = connection.regenerateStatementImportAddress()
        assertEquals("robin+statements-invented0token02@example.com", renewed?.importAddress)
        assertNull(renewed?.lastStatementImport)
        assertTrue(queryOf(fake.sent.last()).contains("RegenerateStatementImportAddress"))
    }

    @Test
    fun anUploadedStatementIsImported() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """{"data":{"ImportStatement":{"importedAt":"2026-10-14T12:00:00Z","addedTransactionCount":2,"updatedTransactionCount":0,"unchangedTransactionCount":0,"financeAccountNames":["Invented Card ··1a11"],"importErrorMessage":""}}}""",
        )
        val imported = connection.importStatement("attachment-1")
        assertEquals(2, imported?.addedTransactionCount)
        assertEquals(true, imported?.isImported)
        assertEquals("attachment-1", variablesOf(fake.sent.last()).text("agentAttachmentId"))
    }
}

class FinanceTaxTest {
    @Test
    fun aTaxDocumentIsUploadedAsOne() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"attachments":[{"id":"attachment-1"}]}""")
        val attachmentId = connection.upload("stub.pdf", "application/pdf", "%PDF".toByteArray(), isTaxDocument = true)
        assertEquals("attachment-1", attachmentId)
        assertTrue(fake.sent.first().url.endsWith("/api/v1/agent/attachments?isTaxDocument=true"))
        connection.upload("picture.jpg", "image/jpeg", byteArrayOf(0xFF.toByte()))
        assertFalse(fake.sent.last().url.contains('?'))
    }

    private val figures =
        """{"wagesAmount":"8000.00","federalIncomeTaxAmount":"900.00","socialSecurityWagesAmount":"8000.00","socialSecurityTaxAmount":"496.00","medicareWagesAmount":"8000.00","medicareTaxAmount":"116.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"400.00","retirementRothAmount":"0.00","hsaAmount":"0.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"8000.00","stateIncomeTaxAmount":"0.00"}]}"""
    private val projected =
        """{"wagesAmount":"104000.00","federalIncomeTaxAmount":"11700.00","socialSecurityWagesAmount":"104000.00","socialSecurityTaxAmount":"6448.00","medicareWagesAmount":"104000.00","medicareTaxAmount":"1508.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"5200.00","retirementRothAmount":"0.00","hsaAmount":"0.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"104000.00","stateIncomeTaxAmount":"0.00"}]}"""

    @Test
    fun aTaxYearIsReadWithItsEmploymentsAndFindings() = runBlocking {
        val body = """
            {"data":{"TaxYear":{"taxYear":2026,"socialSecurityWageBaseAmount":null,"isYearOver":false,"paystubCount":2,"taxFormCount":0,
            "figures":$figures,"projectedFigures":$projected,
            "taxYearEmployments":[{"employeeName":"Robin Example","employerName":"Example Payroll","employerTaxId":null,"taxYearFigureSource":"paystubs",
              "taxFormId":null,"latestPayDate":"2026-01-30","paystubCount":2,"payCadenceDayCount":14,"isPaystubYearComplete":false,"remainingPayCount":24,
              "figures":$figures,"paystubFigures":$figures,"projectedFigures":$projected,"taxYearComparisons":[]}],
            "taxYearPeople":[],
            "taxYearFindings":[{"taxYearFindingCode":"pay_date_gap","employeeName":"Robin Example","employerName":"Example Payroll",
              "findingText":"A paystub between two pay dates is missing.","paystubIds":["paystub-1","paystub-2"]}]}}}
        """.trimIndent()
        val (connection, _) = connectionAnswering(body)
        val summary = connection.taxYear(2026)!!
        assertEquals(1, summary.employments.size)
        assertEquals(false, summary.employments.first().isFromW2)
        assertEquals(24, summary.employments.first().remainingPayCount)
        assertEquals(listOf("paystub-1", "paystub-2"), summary.findings.first().paystubIds)
        val rows = summary.figures!!.rows(summary.projectedFigures)
        assertEquals(
            listOf(
                TaxYearFigureKind.Wages, TaxYearFigureKind.FederalIncomeTax, TaxYearFigureKind.SocialSecurityWages,
                TaxYearFigureKind.SocialSecurityTax, TaxYearFigureKind.MedicareWages, TaxYearFigureKind.MedicareTax,
                TaxYearFigureKind.RetirementTraditional, TaxYearFigureKind.StateWages("ZZ"),
            ),
            rows.map { row -> row.figureKind },
        )
        assertEquals("104000.00", rows.first().projectedAmount)
    }

    @Test
    fun theEstimateIsAskedForAFilingStatusAndRingsWhatIsNotNothing() = runBlocking {
        val body = """{"data":{"TaxYear":{"taxYear":2026,"retirementDeferralLimitAmount":"24500.00","taxYearEstimate":{"taxFilingStatus":"head_of_household","isProjected":true,"payAmount":"110000.00","paidTaxAmount":"9000.00","upcomingWithheldTaxAmount":"12033.00","owingTaxAmount":"0.00","refundAmount":"0.00","remainingIncomeAmount":"88967.00","totalTaxAmount":"21033.00"}}}}"""
        val (connection, fake) = connectionAnswering(body)
        val read = connection.taxYear(2026, TaxFilingStatus.HeadOfHousehold)
        val estimate = read!!.taxYearEstimate!!
        assertEquals(TaxFilingStatus.HeadOfHousehold, estimate.filingStatus)
        assertEquals(
            listOf(TaxEstimateSliceKind.Paid, TaxEstimateSliceKind.UpcomingWithheld, TaxEstimateSliceKind.RemainingIncome),
            estimate.slices.map { slice -> slice.sliceKind },
        )
        assertEquals("24500.00", read.retirementDeferralLimitAmount)
        assertEquals("head_of_household", variablesOf(fake.sent.first()).text("taxFilingStatus"))
        assertEquals(0.1912, estimate.shareOfPay(estimate.totalTaxAmount)!!, 0.0001)
        val (keeping, kept) = connectionAnswering("""{"data":{"SetTaxFilingStatus":"single"}}""")
        keeping.setTaxFilingStatus(TaxFilingStatus.Single)
        assertEquals("single", variablesOf(kept.sent.first()).text("taxFilingStatus"))
    }

    @Test
    fun theTaxDocumentAddressIsReadAndRenewed() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """{"data":{"TaxDocumentAddress":{"taxDocumentAddress":"robin+taxes-invented0token01@example.com","canReadPdfs":true},"RegenerateTaxDocumentAddress":{"taxDocumentAddress":"robin+taxes-invented0token02@example.com","canReadPdfs":true}}}""",
        )
        val address = connection.taxDocumentAddress()
        assertEquals("robin+taxes-invented0token01@example.com", address?.taxDocumentAddress)
        assertEquals(true, address?.canReadPdfs)
        val renewed = connection.regenerateTaxDocumentAddress()
        assertEquals("robin+taxes-invented0token02@example.com", renewed?.taxDocumentAddress)
        assertTrue(queryOf(fake.sent.last()).contains("RegenerateTaxDocumentAddress"))
    }

    @Test
    fun a1099YearIsReadWithItsInvestmentIncome() = runBlocking {
        val body = """{"data":{"TaxYear":{"taxYear":2026,"taxYearInvestment":{"interestAmount":"1000.00","ordinaryDividendsAmount":"2000.00","qualifiedDividendsAmount":"1500.00","shortTermGainAmount":"-1900.00","longTermGainAmount":"10000.00","isCostBasisMissing":false,"taxFormIds":["form-int"]},"taxYearEstimate":{"payAmount":"110000.00","incomeAmount":"121600.00","investmentIncomeAmount":"11600.00","totalTaxAmount":"12160.00"}}}}"""
        val (connection, _) = connectionAnswering(body)
        val summary = connection.taxYear(2026)!!
        assertEquals("-1900.00", summary.taxYearInvestment?.shortTermGainAmount)
        // Shares are of income, pay and investment income together.
        assertEquals(0.1, summary.taxYearEstimate?.shareOfPay("12160.00")!!, 0.0001)
        val form = TeaNodeJson.decodeFromString(FinanceTaxForm.serializer(), """{"id":"form-1","taxFormKind":"1099_div"}""")
        assertEquals("1099-DIV", form.kindName)
        assertFalse(form.isW2)
    }

    @Test
    fun onlyTheChangedSettingsAreSent() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SetTaxYearSetting":{"taxYear":2026,"childCount":2,"otherIncomeAmount":"4500.0000"}}}""")
        val saved = connection.setTaxYearSetting(2026, TaxYearSettingChange(childCount = 2, otherIncomeAmount = "4500"))
        assertEquals(2, saved?.childCount)
        val variables = variablesOf(fake.sent.last())
        assertEquals(setOf("taxYear", "childCount", "otherIncomeAmount"), variables.keys)
        assertEquals("4500", variables.text("otherIncomeAmount"))
    }

    @Test
    fun zeroRowsAreLeftOutExceptWagesAndFederalTax() {
        val zeroFigures = TeaNodeJson.decodeFromString(
            TaxYearFigures.serializer(), """{"wagesAmount":"0.00","federalIncomeTaxAmount":"0.00","medicareTaxAmount":"0.00"}""",
        )
        val rows = zeroFigures.rows()
        assertEquals(listOf(TaxYearFigureKind.Wages, TaxYearFigureKind.FederalIncomeTax), rows.map { row -> row.figureKind })
        assertNull(rows.first().projectedAmount)
    }

    @Test
    fun aW2CountsTheChecksThatDisagree() {
        val body = """{"financeTaxForm":{"id":"form-1","taxYear":2025,"payerName":"Example Payroll","recipientName":"Robin Example","taxFormEntries":[{"id":"entry-2","entryNumber":2,"boxCode":"12","boxQualifier":"D","amount":"400.00"},{"id":"entry-1","entryNumber":1,"boxCode":"1","amount":"8000.00"}]},"taxFormFindings":[{"taxFormFindingCode":"social_security_rate","isAgreeing":true},{"taxFormFindingCode":"medicare_rate","isAgreeing":false}]}"""
        val view = TeaNodeJson.decodeFromString(FinanceTaxFormView.serializer(), body)
        assertEquals(1, view.disagreeingCount)
        assertEquals(listOf("1", "12 D"), view.financeTaxForm?.entries?.map { entry -> entry.boxName })
    }
}

class FinanceManageTest {
    @Test
    fun accountTradesArePagedAndNamed() = runBlocking {
        val body = """{"data":{"FinanceTrades":{"financeTrades":[{"id":"trade-1","financeAccountId":"account-1","financeSecurityId":"security-1","financeSecurity":{"tickerSymbol":"INVX","securityName":"Invented Index Fund"},"providerTradeId":"p1","tradedOn":"2026-09-02","tradeKind":"buy","tradeSubkind":null,"tradedQuantity":"2","unitPrice":"100","tradeAmount":"-200","feeAmount":"0","currencyCode":"USD","description":"Bought 2"},{"id":"trade-2","financeAccountId":"account-1","financeSecurity":null,"tradedOn":"2026-09-01","tradeKind":"transfer","tradeAmount":"0","description":"Moved in"}],"totalCount":60}}}"""
        val (connection, fake) = connectionAnswering(body)
        val page = connection.financeTrades("account-1", limit = 50, offset = 50)
        assertEquals(60, page.totalCount)
        val trades = page.financeTrades.filterNotNull()
        assertEquals(listOf("INVX", "Moved in"), trades.map { trade -> trade.shownName })
        assertEquals("2", trades.first().tradedQuantity)
        val sent = variablesOf(fake.sent.last())
        assertEquals("account-1", sent.text("financeAccountId"))
        assertEquals(50, sent.number("limit"))
        assertEquals(50, sent.number("offset"))
        assertNull(sent["financeSecurityId"])
    }

    @Test
    fun aStatementAccountIsRenamedAndDeleted() = runBlocking {
        val (renaming, renameFake) = connectionAnswering("""{"data":{"RenameStatementAccount":{"id":"account-1","accountName":"Invented Card","accountMask":"4321"}}}""")
        val renamed = renaming.renameStatementAccount("account-1", "Invented Card", null)
        assertEquals("Invented Card", renamed?.accountName)
        assertEquals("4321", renamed?.accountMask)
        val renameSent = variablesOf(renameFake.sent.last())
        assertEquals("Invented Card", renameSent.text("accountName"))
        assertNull(renameSent["accountMask"])
        val (deleting, deleteFake) = connectionAnswering("""{"data":{"DeleteStatementAccount":{"financeAccountId":"account-1","deletedTransactionCount":12,"deletedAssetCount":1}}}""")
        val deleted = deleting.deleteStatementAccount("account-1")
        assertEquals(12, deleted?.deletedTransactionCount)
        assertEquals("account-1", variablesOf(deleteFake.sent.last()).text("financeAccountId"))
    }

    @Test
    fun spendingRulesAreProposedForTransactions() = runBlocking {
        val body = """{"data":{"ProposeSpendingRules":{"spendingRuleProposals":[{"matchText":"Corner Market","spendingCategoryId":"groceries","financeTransactionCount":4,"changedTransactionCount":1,"aheadOfSpendingRule":{"id":"rule-1","matchText":"Market","spendingCategoryId":"shopping"}},{"matchText":"Sample Cafe","spendingCategoryId":"groceries","financeTransactionCount":2,"changedTransactionCount":0,"aheadOfSpendingRule":null}],"tooGenericMatchTextCount":1,"changingNumberMatchTextCount":0,"overLimitMatchTextCount":0}}}"""
        val (connection, fake) = connectionAnswering(body)
        val proposed = connection.proposeSpendingRules(listOf("transaction-1", "transaction-2"), "groceries")!!
        val proposals = proposed.proposals
        assertEquals(listOf("Corner Market", "Sample Cafe"), proposals.map { proposal -> proposal.matchText })
        assertEquals("Market", proposals.first().aheadOfSpendingRule?.matchText)
        assertNull(proposals.last().aheadOfSpendingRule)
        assertEquals(1, proposed.tooGenericMatchTextCount)
        val sent = variablesOf(fake.sent.last())
        assertEquals(listOf("transaction-1", "transaction-2"), (sent["financeTransactionIds"] as JsonArray).map { element -> (element as JsonPrimitive).content })
        assertEquals("groceries", sent.text("spendingCategoryId"))
    }

    @Test
    fun aConnectionMadeElsewhereIsBroughtIn() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ImportFinanceCredential":{"id":"source-1"}}}""")
        connection.importFinanceCredential("simplefin", "https://invented-user:invented-pass@bridge.example.com/simplefin", null)
        val sent = variablesOf(fake.sent.last())
        assertEquals("simplefin", sent.text("providerKind"))
        assertNotNull(sent["credential"])
        assertNull(sent["institutionName"])
        val (listing, _) = connectionAnswering("""{"data":{"FinanceProviders":[{"providerKind":"plaid","isBrowserRequired":true},{"providerKind":"simplefin","isBrowserRequired":false}]}}""")
        assertEquals(listOf("plaid", "simplefin"), listing.financeProviderKinds())
    }

    @Test
    fun aRowsFileBecomesTheImportsArguments() {
        val file = """{"institutionName":"Sample Bank","statementAccountKind":"bank","currencyCode":"USD","accountName":null,"ledgerBalanceAmount":1250.5,"transactionRows":[{"postedOn":"2026-09-02","description":"Corner Market","amount":-12.5,"runningBalanceAmount":"1250.50"},{"postedOn":"2026-09-03","description":"Payroll","amount":1000}],"monthlyTotals":[{"totalMonth":"2026-09","totalAmount":987.5}]}"""
        val variables = TransactionRowsFile.variables(file.toByteArray())!!
        assertNull(variables["accountName"])
        assertEquals(JsonPrimitive("1250.5"), variables["ledgerBalanceAmount"])
        val rows = (variables["transactionRows"] as JsonArray).map { element -> element as JsonObject }
        assertEquals(JsonPrimitive("-12.5"), rows[0]["amount"])
        assertEquals(JsonPrimitive("1250.50"), rows[0]["runningBalanceAmount"])
        assertEquals(JsonPrimitive("1000"), rows[1]["amount"])
        val total = (variables["monthlyTotals"] as JsonArray).first() as JsonObject
        assertEquals(JsonPrimitive("987.5"), total["totalAmount"])
    }

    @Test
    fun aFileThatIsNotRowsIsRefused() {
        assertNull(TransactionRowsFile.variables("[1, 2]".toByteArray()))
        assertNull(TransactionRowsFile.variables("""{"institutionName":"Sample Bank"}""".toByteArray()))
        assertNull(TransactionRowsFile.variables("not json".toByteArray()))
    }

    @Test
    fun rowsArePreviewedThenImported() = runBlocking {
        val preview = """{"data":{"PreviewImportTransactions":{"financeAccountId":null,"accountName":"Sample Bank Checking","isNewAccount":true,"accountMatch":"","currencyCode":"USD","newTransactionRows":[{"rowNumber":1,"postedOn":"2026-09-02","description":"Corner Market","amount":"-12.50","hasNearbyStoredTransaction":false},{"rowNumber":2,"postedOn":"2026-09-03","description":"Payroll","amount":"1000.00","hasNearbyStoredTransaction":true}],"presentTransactionRows":[],"firstPostedOn":"2026-09-02","lastPostedOn":"2026-09-03","moneyInAmount":"1000.00","moneyOutAmount":"-12.50","verificationSummary":"balances chain"}}}"""
        val (previewing, previewFake) = connectionAnswering(preview)
        val arguments = mapOf("institutionName" to JsonPrimitive("Sample Bank"), "transactionRows" to JsonArray(emptyList()))
        val previewed = previewing.previewImportTransactions(arguments)
        assertEquals(2, previewed?.newRows?.size)
        assertEquals(true, previewed?.newRows?.last()?.hasNearbyStoredTransaction)
        assertEquals(0, previewed?.presentRows?.size)
        assertEquals(true, previewed?.isNewAccount)
        assertTrue(queryOf(previewFake.sent.last()).contains("PreviewImportTransactions"))
        val (importing, importFake) = connectionAnswering(
            """{"data":{"ImportTransactions":{"importedAt":"2026-10-14T12:00:00Z","addedTransactionCount":2,"updatedTransactionCount":0,"unchangedTransactionCount":0,"financeAccountNames":["Sample Bank Checking"],"importErrorMessage":""}}}""",
        )
        val imported = importing.importTransactions(arguments)
        assertEquals(2, imported?.addedTransactionCount)
        assertEquals(true, imported?.isImported)
        assertEquals("Sample Bank", variablesOf(importFake.sent.last()).text("institutionName"))
    }
}

class FinanceMoneyTest {
    @Test
    fun moneyIsShownInItsCurrencyOrADashWhenThereIsNone() {
        assertEquals("$12.50", FinanceMoney.shown("12.5", "USD", Locale.US))
        assertEquals("-$3.00", FinanceMoney.shown("-3", "", Locale.US))
        assertEquals("—", FinanceMoney.shown(null, "USD", Locale.US))
        assertEquals("—", FinanceMoney.shown("", "USD", Locale.US))
    }

    @Test
    fun typedMoneyIsADecimalOfUpToFourPlaces() {
        assertTrue(FinanceMoney.isDecimal("12.3456"))
        assertTrue(FinanceMoney.isDecimal("-1"))
        assertFalse(FinanceMoney.isDecimal("12.34567"))
        assertFalse(FinanceMoney.isDecimal("1e5"))
        assertFalse(FinanceMoney.isDecimal(""))
    }

    @Test
    fun aShareIsAWholePercentageAndASmallOneSaysLessThanOne() {
        assertEquals("25%", FinanceMoney.percent(0.25, Locale.US))
        assertEquals("<1%", FinanceMoney.percent(0.002, Locale.US))
        assertEquals("0%", FinanceMoney.percent(0.0, Locale.US))
    }

    @Test
    fun anAxisHasRoomForAFewCharacters() {
        assertEquals("$12K", FinanceMoney.axisText(12000.0, "USD", Locale.US))
        assertEquals("$1.2M", FinanceMoney.axisText(1_200_000.0, "USD", Locale.US))
    }

    @Test
    fun categoriesAreLabeledUnderTheirParentAndOrderedWithOtherAndTransferLast() {
        val categories = listOf(
            SpendingCategory("t", "Transfer", isTransfer = true),
            SpendingCategory("o", "Other", isOther = true),
            SpendingCategory("h", "Hidden", isHidden = true),
            SpendingCategory("f", "Food"),
            SpendingCategory("g", "Groceries", parentSpendingCategoryId = "f"),
            SpendingCategory("a", "Auto"),
        )
        assertEquals("Food › Groceries", FinanceCategories.label(categories[4], categories))
        assertEquals(listOf("Auto", "Food", "Food › Groceries", "Other", "Transfer"), FinanceCategories.pickable(categories).map { category -> FinanceCategories.label(category, categories) })
        assertTrue(FinanceCategories.pickable(categories, keeping = "h").any { category -> category.id == "h" })
    }

    @Test
    fun namesWithNumbersAreOrderedByTheNumber() {
        assertTrue(standardCompare("Item 2", "Item 10") < 0)
        assertTrue(standardCompare("apple", "Banana") < 0)
    }

    @Test
    fun aFilterSendsOnlyWhatNarrowsIt() {
        assertFalse(FinanceTransactionFilter().isNarrowing)
        val filter = FinanceTransactionFilter(from = " 2026-01-01 ", text = " ", isUncategorized = true)
        assertEquals(setOf("from", "isUncategorized"), filter.variables.keys)
        assertEquals(JsonPrimitive("2026-01-01"), filter.variables["from"])
        assertTrue(filter.isNarrowing)
        assertEquals(JsonNull, JsonNull)
    }
}
