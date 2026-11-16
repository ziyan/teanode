package com.teanode.kit.finance

import org.junit.jupiter.api.Test
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue

class TaxYearSettingFormTest {
    private val setting = TaxYearSetting(
        taxYear = 2030, childCount = 2, otherDependentCount = 0, dependentCareExpenseAmount = "3000.00", otherIncomeAmount = "0",
        estimatedShortTermGainAmount = "1250.50", estimatedLongTermGainAmount = "0", estimatedGainNote = "Example brokerage",
    )

    @Test
    fun plainDropsTrailingZerosAndEmptiesNothing() {
        assertEquals("3000", TaxYearSettingForm.plain("3000.00"))
        assertEquals("12.5", TaxYearSettingForm.plain("12.50"))
        assertEquals("", TaxYearSettingForm.plain("0.00"))
        assertEquals("", TaxYearSettingForm.plain(null))
        assertEquals("-40", TaxYearSettingForm.plain("-40.0"))
    }

    @Test
    fun formFromSettingSaysNothingChanged() {
        val change = TaxYearSettingForm.of(setting).change(setting)
        assertNull(change.childCount)
        assertNull(change.dependentCareExpenseAmount)
        assertNull(change.estimatedShortTermGainAmount)
        assertNull(change.estimatedGainNote)
        assertFalse(change.isEstimatedGainCleared)
        assertTrue(change.variables.isEmpty())
    }

    @Test
    fun anEmptiedAmountIsSentAsZero() {
        val change = TaxYearSettingForm.of(setting).copy(dependentCareExpenseText = " ").change(setting)
        assertEquals("0", change.dependentCareExpenseAmount)
    }

    @Test
    fun aChangedCountAndAmountAreSent() {
        val change = TaxYearSettingForm.of(setting).copy(childCountText = "3", charitableContributionText = "500").change(setting)
        assertEquals(3, change.childCount)
        assertEquals("500", change.charitableContributionAmount)
        assertNull(change.otherDependentCount)
    }

    @Test
    fun bothGainsEmptiedClearTheEstimate() {
        val change = TaxYearSettingForm.of(setting).copy(shortTermGainText = "", longTermGainText = "").change(setting)
        assertTrue(change.isEstimatedGainCleared)
        assertNull(change.estimatedShortTermGainAmount)
    }

    @Test
    fun emptyGainsWithNoneBeforeClearNothing() {
        val change = TaxYearSettingForm().copy(childCountText = "0", otherDependentCountText = "0").change(null)
        assertFalse(change.isEstimatedGainCleared)
    }

    @Test
    fun personTextShowsTheTaxpayerNumberEnding() {
        assertEquals("Alex Example ··1234", TaxWords.personText("Alex Example", "1234"))
        assertEquals("Alex Example", TaxWords.personText("Alex Example", ""))
        assertEquals("", TaxWords.personText(null, null))
    }

    @Test
    fun excessSocialSecurityIsNotAProblem() {
        assertFalse(TaxWords.isProblem(TaxYearSummary.Finding(taxYearFindingCode = "excess_social_security")))
        assertTrue(TaxWords.isProblem(TaxYearSummary.Finding(taxYearFindingCode = "w2_mismatch")))
        assertEquals("Alex, Example Co", TaxWords.whoseText(TaxYearSummary.Finding(employeeName = "Alex", employerName = "Example Co")))
        assertEquals("", TaxWords.whoseText(TaxYearSummary.Finding(employeeName = "")))
    }
}

class StatementAccountRenameTest {
    private val account = FinanceAccount(id = "account-1", accountName = "Everyday", accountMask = "1234")

    @Test
    fun nothingChangedCannotBeSaved() {
        assertFalse(StatementAccountRename(account, "Everyday", "1234").canSave)
        assertFalse(StatementAccountRename(account, "  ", "9999").canSave)
    }

    @Test
    fun onlyWhatChangedIsSent() {
        val rename = StatementAccountRename(account, " Savings ", "1234")
        assertTrue(rename.canSave)
        assertEquals("Savings", rename.sentName)
        assertNull(rename.sentMask)
        assertEquals(StatementAccountRenameWords.NameOnly, rename.words)
    }

    @Test
    fun anEmptiedMaskTakesTheStatementsNumberBack() {
        val rename = StatementAccountRename(account, "Everyday", "")
        assertEquals("", rename.sentMask)
        assertNull(rename.sentName)
        assertEquals(StatementAccountRenameWords.MaskCleared, rename.words)
        assertEquals(StatementAccountRenameWords.NameAndMaskCleared, StatementAccountRename(account, "Savings", "").words)
    }

    @Test
    fun aNewMaskAndNameAreToldTogether() {
        assertEquals(StatementAccountRenameWords.NameAndMask, StatementAccountRename(account, "Savings", "5678").words)
        assertEquals(StatementAccountRenameWords.MaskOnly, StatementAccountRename(account, "Everyday", "5678").words)
    }

    @Test
    fun theFieldsAreLimited() {
        val (name, mask) = StatementAccountRename.limited("x".repeat(250), "1234567890")
        assertEquals(200, name.length)
        assertEquals("12345678", mask)
    }
}
