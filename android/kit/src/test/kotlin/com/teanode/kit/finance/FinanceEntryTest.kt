package com.teanode.kit.finance

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class FinanceEntryTest {
    private fun receiptDraft(): ReceiptDraft {
        val apples = ReceiptLineDraft(description = "Apples", quantity = "2", unitPriceAmount = "3.00", lineAmount = "6.00")
        val bread = ReceiptLineDraft(description = "Bread", lineAmount = "5.50")
        val bag = ReceiptLineDraft(receiptLineKind = "fee", lineAmount = "1.00")
        return ReceiptDraft(
            currencyCode = "usd", purchasedOn = "2026-09-10", merchantName = " Corner Market ", totalAmount = "12.50",
            subtotalAmount = "11.50", paymentAccountMask = "1234", receiptLines = listOf(apples, bread, ReceiptLineDraft(), bag),
        )
    }

    @Test
    fun aReceiptIsReadyWhenItHasMerchantTotalAndLinesThatAreNumbers() {
        assertTrue(receiptDraft().isReady)
        val brokenLine = receiptDraft().let { draft -> draft.copy(receiptLines = draft.receiptLines.toMutableList().also { lines -> lines[0] = lines[0].copy(lineAmount = "six") }) }
        assertFalse(brokenLine.isReady)
        assertFalse(receiptDraft().copy(totalAmount = "-1").isReady)
        assertFalse(receiptDraft().copy(currencyCode = "US").isReady)
        assertFalse(receiptDraft().copy(purchasedOn = "10 September").isReady)
        assertFalse(receiptDraft().copy(receiptLines = listOf(ReceiptLineDraft())).isReady, "a receipt of blank lines has no lines")
    }

    @Test
    fun aDiscountLineMayBeBelowZeroButAnItemsUnitPriceMayNot() {
        val discount = ReceiptLineDraft(receiptLineKind = "discount", lineAmount = "-1.25")
        assertTrue(discount.isValid)
        assertFalse(discount.copy(unitPriceAmount = "-1").isValid)
    }

    @Test
    fun aReceiptIsPreviewedWithItsFileAndOnlyRecordedAsUnbalancedWhenAccepted() = runBlocking {
        val body = """
            {"data":{"PreviewRecordReceipt":{"receiptCheckState":"unbalanced","checkDifferenceAmount":"-1.00",
            "receiptCheckSummary":"The lines come to 1.00 less than the total.","isReplacing":false,
            "receiptMatchCandidates":[{"financeTransactionId":"charge-1","financeTransaction":{"id":"charge-1","amount":"-12.50","currencyCode":"USD","merchantName":"Corner Market"},
            "matchedAmount":"12.50","isExactAmount":true,"isSameAccount":false,"isMerchantNameShared":true,"dayDistanceCount":1,"isAutomatic":true,"matchConfidence":"high"}]}}}
        """.trimIndent()
        val (connection, fake) = connectionAnswering(body)
        val draft = receiptDraft().copy(isUnbalancedAccepted = true)
        val preview = connection.previewRecordReceipt(draft, "attachment-1")!!
        assertFalse(preview.isBalanced)
        assertEquals("-1.00", preview.checkDifferenceAmount)
        assertEquals(listOf("charge-1"), preview.candidates.map { candidate -> candidate.financeTransactionId })
        assertEquals(true, preview.candidates.first().isAutomatic)
        assertEquals("Corner Market", preview.candidates.first().financeTransaction?.shownName)
        val sent = variablesOf(fake.sent.first())
        assertEquals("attachment-1", sent.text("agentAttachmentId"))
        assertEquals("Corner Market", sent.text("merchantName"))
        assertEquals("USD", sent.text("currencyCode"))
        assertEquals("2026-09-10", sent.text("purchasedOn"))
        assertEquals("11.50", sent.text("subtotalAmount"))
        assertEquals("1234", sent.text("paymentAccountMask"))
        assertNull(sent["merchantReceiptNumber"], "an empty field is left out")
        assertNull(sent["isUnbalancedAccepted"], "a preview writes nothing, so it accepts nothing")
        val lines = sent.objects("receiptLines")
        assertEquals(3, lines.size, "the blank line is left out")
        assertEquals("2", lines[0].text("quantity"))
        assertEquals("3.00", lines[0].text("unitPriceAmount"))
        assertEquals("item", lines[0].text("receiptLineKind"))
        assertEquals("fee", lines[2].text("receiptLineKind"))
        assertNull(lines[1]["quantity"])
        val query = queryOf(fake.sent.first())
        assertTrue(query.contains("PreviewRecordReceipt("))
        assertTrue(query.startsWith("query"))
    }

    @Test
    fun aRecordedReceiptSaysWhetherItWasMatched() = runBlocking {
        val body = """
            {"data":{"RecordReceipt":{"financeReceipt":{"id":"receipt-1","merchantName":"Corner Market","currencyCode":"USD","totalAmount":"12.50",
            "receiptLines":[],"receiptMatches":[{"receiptId":"receipt-1","financeTransactionId":"charge-1","matchedAmount":"12.50","receiptMatchSource":"receipt_matcher"}]},
            "receiptCheckSummary":"The lines add up.","receiptMatchCandidates":[],"isReplaced":true,"droppedReceiptMatchReasons":[]}}}
        """.trimIndent()
        val (connection, fake) = connectionAnswering(body)
        val recorded = connection.recordReceipt(receiptDraft().copy(isUnbalancedAccepted = true), "attachment-1")!!
        assertEquals("receipt-1", recorded.financeReceipt?.id)
        assertTrue(recorded.isMatched)
        assertEquals(true, recorded.isReplaced)
        assertEquals(true, variablesOf(fake.sent.first()).flag("isUnbalancedAccepted"))
    }

    @Test
    fun aPaystubTakesOnlyWhatItsLinesKindUses() = runBlocking {
        val earning = PaystubLineDraft(
            description = "Regular", hours = "80", rateAmount = "50.00", currentAmount = "4000.00", taxJurisdictionCode = "ZZ",
        )
        val tax = PaystubLineDraft(
            paystubLineKind = "tax", paystubTaxKind = "state_income", taxJurisdictionCode = "ZZ", currentAmount = "400.00",
            currentWagesAmount = "4000.00",
        )
        val deduction = PaystubLineDraft(
            paystubLineKind = "deduction", paystubDeductionKind = "hsa", currentAmount = "500.00", employerCurrentAmount = "100.00",
            hours = "9",
        )
        val draft = PaystubDraft(
            currencyCode = "usd", payDate = "2026-09-30", employerName = "Example Payroll", employeeName = "Robin Example",
            employeeTaxIdMask = "1234", grossAmount = "4000.00", netAmount = "3100.00",
            paystubLines = listOf(earning, tax, deduction, PaystubLineDraft()),
        )
        assertTrue(draft.isReady)
        val body = """{"data":{"RecordPaystub":{"id":"paystub-1","employerName":"Example Payroll","payDate":"2026-09-30","grossAmount":"4000.00","netAmount":"3100.00","paystubCheckState":"balanced","paystubLines":[]}}}"""
        val (connection, fake) = connectionAnswering(body)
        val recorded = connection.recordPaystub(draft, "attachment-2")!!
        assertEquals("paystub-1", recorded.id)
        assertTrue(recorded.isBalanced)
        val sent = variablesOf(fake.sent.first())
        assertEquals("attachment-2", sent.text("agentAttachmentId"))
        assertEquals("USD", sent.text("currencyCode"))
        assertEquals("1234", sent.text("employeeTaxIdMask"))
        assertNull(sent["isUnbalancedAccepted"])
        val lines = sent.objects("paystubLines")
        assertEquals(3, lines.size)
        assertEquals("80", lines[0].text("hours"))
        assertEquals("50.00", lines[0].text("rateAmount"))
        assertNull(lines[0]["taxJurisdictionCode"], "an earning has no jurisdiction")
        assertEquals("state_income", lines[1].text("paystubTaxKind"))
        assertEquals("4000.00", lines[1].text("currentWagesAmount"))
        assertNull(lines[1]["paystubDeductionKind"])
        assertEquals("hsa", lines[2].text("paystubDeductionKind"))
        assertEquals("100.00", lines[2].text("employerCurrentAmount"))
        assertNull(lines[2]["hours"], "a deduction has no hours")
    }

    @Test
    fun aPaystubNeedsAPayDateTakeHomePayAndLinesWithAnAmount() {
        var draft = PaystubDraft(
            currencyCode = "USD", payDate = "2026-09-30", employerName = "Example Payroll", employeeName = "Robin Example",
            grossAmount = "100.00", netAmount = "-5.00", paystubLines = listOf(PaystubLineDraft(currentAmount = "100.00")),
        )
        assertTrue(draft.isReady, "take-home pay may be below zero")
        draft = draft.copy(grossAmount = "-100.00")
        assertFalse(draft.isReady)
        draft = draft.copy(grossAmount = "100.00", payDate = "")
        assertFalse(draft.isReady)
        draft = draft.copy(payDate = "2026-09-30", employeeTaxIdMask = "123")
        assertFalse(draft.isReady, "the mask is the last four digits")
        draft = draft.copy(employeeTaxIdMask = "", paystubLines = listOf(PaystubLineDraft(currentAmount = "", description = "Regular")))
        assertFalse(draft.isReady, "a line with no amount is refused")
    }

    @Test
    fun aTaxFormIsSentWithItsKindYearAndBoxes() = runBlocking {
        val interest = TaxFormEntryDraft(boxCode = " 1 ", amount = "120.50")
        val state = TaxFormEntryDraft(boxCode = "16", boxQualifier = "ZZ", textValue = "123")
        val mark = TaxFormEntryDraft(boxCode = "13", isChecked = true)
        val draft = TaxFormDraft(
            taxYear = 2025, taxFormKind = "1099_int", payerName = "Sample Bank", recipientName = "Robin Example",
            recipientTaxIdMask = "4321", isCorrected = true, taxFormEntries = listOf(interest, TaxFormEntryDraft(), state, mark),
        )
        assertTrue(draft.isReady)
        val body = """{"data":{"RecordTaxForm":{"financeTaxForm":{"id":"form-1","taxFormKind":"1099_int","taxYear":2025,"payerName":"Sample Bank","taxFormEntries":[]},"taxFormFindings":[]}}}"""
        val (connection, fake) = connectionAnswering(body)
        val recorded = connection.recordTaxForm(draft, "attachment-3")!!
        assertEquals("form-1", recorded.id)
        assertEquals("1099-INT", recorded.financeTaxForm?.kindName)
        val sent = variablesOf(fake.sent.first())
        assertEquals("1099_int", sent.text("taxFormKind"))
        assertEquals(2025, sent.number("taxYear"))
        assertEquals(true, sent.flag("isCorrected"))
        assertEquals("4321", sent.text("recipientTaxIdMask"))
        assertNull(sent["payerTaxId"])
        val entries = sent.objects("taxFormEntries")
        assertEquals(3, entries.size, "the empty box is left out")
        assertEquals("1", entries[0].text("boxCode"))
        assertEquals("120.50", entries[0].text("amount"))
        assertNull(entries[0]["isChecked"])
        assertEquals("ZZ", entries[1].text("boxQualifier"))
        assertEquals("123", entries[1].text("textValue"))
        assertEquals(true, entries[2].flag("isChecked"))
    }

    @Test
    fun aTaxFormNeedsABoxNumberOnEveryFilledBox() {
        var draft = TaxFormDraft(taxYear = 2025, payerName = "Sample Bank", recipientName = "Robin Example")
        draft = draft.copy(taxFormEntries = listOf(draft.taxFormEntries[0].copy(amount = "10.00")))
        assertFalse(draft.isReady, "an amount with no box")
        draft = draft.copy(taxFormEntries = listOf(draft.taxFormEntries[0].copy(boxCode = "1")))
        assertTrue(draft.isReady)
        draft = draft.copy(taxFormEntries = listOf(draft.taxFormEntries[0].copy(amount = "ten")))
        assertFalse(draft.isReady)
        draft = draft.copy(taxFormEntries = listOf(draft.taxFormEntries[0].copy(amount = "10.00")), taxYear = 25)
        assertFalse(draft.isReady)
    }

    @Test
    fun anExchangeRateIsAskedForAndShown() = runBlocking {
        val body = """{"data":{"ExchangeRate":{"fromCurrencyCode":"EUR","toCurrencyCode":"USD","rate":"1.08340000","rateOn":"2026-10-16","rateSource":"central_bank"}}}"""
        val (connection, fake) = connectionAnswering(body)
        val rate = connection.exchangeRate("eur", "usd")!!
        assertEquals("USD", rate.toCurrencyCode)
        assertEquals("1.0834", rate.shownRate)
        val sent = variablesOf(fake.sent.first())
        assertEquals("EUR", sent.text("fromCurrencyCode"))
        assertEquals("USD", sent.text("toCurrencyCode"))
        assertNull(sent["rateOn"], "today's rate")
    }

    private fun rateOf(rate: String): CurrencyRate =
        TeaNodeJson.decodeFromString(CurrencyRate.serializer(), """{"fromCurrencyCode":"EUR","toCurrencyCode":"USD","rate":"$rate"}""")

    @Test
    fun aRateThatIsNotAPositiveNumberShowsNothing() {
        assertNull(rateOf("0").shownRate)
    }

    @Test
    fun aRateIsShownToFiveSignificantDigitsWithoutTrailingZeros() {
        assertEquals("1.0834", rateOf("1.08340000").shownRate)
        assertEquals("1.0834", rateOf("1.08337").shownRate)
        assertEquals("1.5", rateOf("1.50000000").shownRate)
        assertEquals("0.0092", rateOf("0.0092").shownRate)
        assertEquals("150.12", rateOf("150.123456").shownRate)
        assertEquals("2", rateOf("2").shownRate)
    }

    @Test
    fun numbersTypedWithACommaAreReadAsDecimalPoints() {
        assertEquals("12.50", FinanceEntryText.normalized(" 12,50 "))
        assertEquals("1,250.00", FinanceEntryText.normalized("1,250.00"), "a comma beside a point is a thousands mark, which the server refuses")
        assertTrue(FinanceEntryText.isAmount("12,5", false))
        assertFalse(FinanceEntryText.isAmount("1,250.00", false))
        assertTrue(FinanceEntryText.isQuantity("0,5"))
        val draft = receiptDraft().copy(totalAmount = "12,50")
        assertTrue(draft.isReady)
        assertEquals(JsonPrimitive("12.50"), draft.variables("attachment-1", false)["totalAmount"])
    }

    @Test
    fun aFormNamesTheFirstFieldThatIsNotReady() {
        var draft = receiptDraft()
        assertNull(draft.firstProblem)
        draft = draft.copy(totalAmount = "twelve")
        assertEquals(FinanceEntryProblem("Total", FinanceEntryProblem.Reason.NotAmount), draft.firstProblem)
        draft = draft.copy(merchantName = " ")
        assertEquals(FinanceEntryProblem("Merchant", FinanceEntryProblem.Reason.Missing), draft.firstProblem, "the first in the order of the form")
        draft = receiptDraft()
        draft = draft.copy(receiptLines = draft.receiptLines.toMutableList().also { lines -> lines[1] = lines[1].copy(lineAmount = "") })
        assertEquals(FinanceEntryProblem("Amount", FinanceEntryProblem.Reason.Missing, 2), draft.firstProblem)
        draft = draft.copy(receiptLines = listOf(ReceiptLineDraft()))
        assertEquals(FinanceEntryProblem.Reason.NoLines, draft.firstProblem?.reason)
        val paystub = PaystubDraft(currencyCode = "USD", payDate = "", employerName = "Example Payroll", employeeName = "Robin Example")
        assertEquals(FinanceEntryProblem("Pay date", FinanceEntryProblem.Reason.Missing), paystub.firstProblem)
        var form = TaxFormDraft(taxYear = 2025, payerName = "Sample Bank", recipientName = "Robin Example")
        assertEquals(FinanceEntryProblem.Reason.NoBoxes, form.firstProblem?.reason)
        form = form.copy(taxFormEntries = listOf(form.taxFormEntries[0].copy(amount = "10")))
        assertEquals(FinanceEntryProblem("Box", FinanceEntryProblem.Reason.Missing, 1), form.firstProblem)
        assertNotNull(form.firstProblem)
    }
}
