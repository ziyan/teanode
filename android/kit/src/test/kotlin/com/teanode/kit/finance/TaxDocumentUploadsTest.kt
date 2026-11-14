package com.teanode.kit.finance

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class TaxDocumentUploadsTest {
    private fun upload(uploadId: Long, attachmentId: String?, phase: TaxDocumentUploadPhase) =
        TaxDocumentUpload(uploadId, "file$uploadId.pdf", attachmentId, phase)

    @Test
    fun aFileIsFinishedOnlyWhenNothingMoreWillHappenToIt() {
        assertFalse(upload(1, null, TaxDocumentUploadPhase.Uploading).isFinished)
        assertFalse(upload(1, "a", TaxDocumentUploadPhase.Reading).isFinished)
        assertTrue(upload(1, "a", TaxDocumentUploadPhase.StillReading).isFinished)
        assertTrue(upload(1, null, TaxDocumentUploadPhase.Failed).isFinished)
    }

    @Test
    fun filesTheServerHasReadAreTakenOutAndOthersStay() {
        val uploads = listOf(upload(1, "a", TaxDocumentUploadPhase.Reading), upload(2, "b", TaxDocumentUploadPhase.Reading), upload(3, null, TaxDocumentUploadPhase.Failed))
        val kept = TaxDocumentUploads.withoutRead(uploads, setOf("a"))
        assertEquals(listOf(2L, 3L), kept.map { each -> each.uploadId })
    }

    @Test
    fun filesStillWaitingAreSaidToBeStillReading() {
        val uploads = listOf(upload(1, "a", TaxDocumentUploadPhase.Reading), upload(2, null, TaxDocumentUploadPhase.Failed))
        val after = TaxDocumentUploads.stillReading(uploads, setOf("a"))
        assertEquals(TaxDocumentUploadPhase.StillReading, after[0].uploadPhase)
        assertEquals(TaxDocumentUploadPhase.Failed, after[1].uploadPhase)
    }

    @Test
    fun theAttachmentsReadAreThoseOfPaystubsAndTaxForms() {
        val paystub = FinancePaystub(id = "p1", agentAttachmentId = "a")
        val form = FinanceTaxFormView(financeTaxForm = FinanceTaxForm(id = "f1", agentAttachmentId = "b"))
        assertEquals(setOf("a", "b"), TaxDocumentUploads.readAttachmentIds(listOf(paystub), listOf(form)))
    }
}

class ExchangeRateLookupTest {
    @Test
    fun aRateIsAskedForOnlyBetweenTwoDifferentCurrencyCodes() {
        assertEquals("EUR" to "USD", ExchangeRateLookup.pair(" eur ", ""))
        assertEquals("EUR" to "GBP", ExchangeRateLookup.pair("EUR", "gbp"))
        assertEquals(null, ExchangeRateLookup.pair("USD", ""))
        assertEquals(null, ExchangeRateLookup.pair("EU", "USD"))
        assertEquals(null, ExchangeRateLookup.pair("", "USD"))
    }

    @Test
    fun aRateIsShownToFiveSignificantDigits() {
        assertEquals("1.0834", CurrencyRate("EUR", "USD", "1.08340000").shownRate)
        assertEquals(null, CurrencyRate("EUR", "USD", "0").shownRate)
    }
}
