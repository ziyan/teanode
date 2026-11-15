package com.teanode.kit.finance

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class FinanceBudgetEntryTest {
    @Test
    fun aBlankOrNegativeAmountIsNotSavable() {
        assertTrue(FinanceBudgetEntry.isSavable("groceries", " 250.50 "))
        assertTrue(FinanceBudgetEntry.isSavable("groceries", "0"))
        assertFalse(FinanceBudgetEntry.isSavable("groceries", ""))
        assertFalse(FinanceBudgetEntry.isSavable("groceries", "   "))
        assertFalse(FinanceBudgetEntry.isSavable("groceries", "-5"))
        assertFalse(FinanceBudgetEntry.isSavable("", "5"))
        assertFalse(FinanceBudgetEntry.isSavable("groceries", "five"))
    }

    @Test
    fun aMonthBeforeThisOneChangesMonthsAlreadyPast() {
        assertTrue(FinanceBudgetEntry.changesMonthsAlreadyPast("2030-09", "2030-10"))
        assertFalse(FinanceBudgetEntry.changesMonthsAlreadyPast("2030-10", "2030-10"))
        assertFalse(FinanceBudgetEntry.changesMonthsAlreadyPast(" 2030-11 ", "2030-10"))
    }

    @Test
    fun aBudgetOpenedForChangeStartsThisMonthOrLater() {
        assertEquals("2030-10", FinanceBudgetEntry.startingMonth("2029-01", "2030-10"))
        assertEquals("2031-01", FinanceBudgetEntry.startingMonth("2031-01", "2030-10"))
        assertEquals("2031-01", FinanceBudgetEntry.startingMonth("2031-01-01", "2030-10"))
        assertEquals("2030-10", FinanceBudgetEntry.startingMonth("2030-10-01", "2030-10"))
    }

    @Test
    fun aBudgetStoredFromTheFirstOfAMonthIsInForceThatMonth() {
        assertTrue(FinanceBudgetEntry.isInForce("2030-10-01", "2030-10"))
        assertTrue(FinanceBudgetEntry.isInForce("2030-01-01", "2030-10"))
        assertFalse(FinanceBudgetEntry.isInForce("2030-11-01", "2030-10"))
    }

    @Test
    fun anEstimatedAssetNeedsWhatToEstimateFrom() {
        assertFalse(FinanceValuationSources.isAssetSavable("Car", "agent_estimate", " ", ""))
        assertTrue(FinanceValuationSources.isAssetSavable("Car", "agent_estimate", "Similar listings", ""))
        assertTrue(FinanceValuationSources.isAssetSavable("Car", "agent_reading", "", ""))
        assertTrue(FinanceValuationSources.isAssetSavable("Car", "manual", "", "9500.00"))
        assertFalse(FinanceValuationSources.isAssetSavable("Car", "manual", "", "lots"))
        assertFalse(FinanceValuationSources.isAssetSavable(" ", "manual", "", ""))
    }
}
