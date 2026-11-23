package com.teanode.kit.finance

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class FinanceTaxBoxTitlesTest {
    @Test
    fun aBoxIsTitledByItsFormAndNumber() {
        assertEquals("1099_int_10", FinanceTaxBoxTitles.boxKey("1099_int", "10"))
        assertEquals("1099_div_2c", FinanceTaxBoxTitles.boxKey("1099_div", " 2C "))
        assertEquals("1098_2", FinanceTaxBoxTitles.boxKey("1098", "2"))
        assertEquals("1099_b_1g", FinanceTaxBoxTitles.boxKey("1099_b", "1g"))
        assertEquals("w2_1", FinanceTaxBoxTitles.boxKey("w2", "1"))
        assertEquals("w2_17", FinanceTaxBoxTitles.boxKey(null, "17"))
        assertNull(FinanceTaxBoxTitles.boxKey("1099_int", "99"))
    }

    @Test
    fun aBrokersBoxNamesItsTotal() {
        assertEquals("term_short_covered", FinanceTaxBoxTitles.termKey("1099_b", "short_covered"))
        assertNull(FinanceTaxBoxTitles.termKey("1099_div", "short_covered"))
        assertNull(FinanceTaxBoxTitles.termKey("1099_b", "other"))
    }
}
