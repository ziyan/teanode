package com.teanode.kit.finance

/**
 * What a box of a tax form is, by the form's kind and the box's number (iOS TaxWords.boxTitle). The title is a key the app
 * has the words for ("finance_tax_box_<key>"); null for a box with no title, which is then shown by its number alone.
 */
object FinanceTaxBoxTitles {
    private val knownKeys: Set<String> = setOf(
        "w2_1",
        "w2_2",
        "w2_3",
        "w2_4",
        "w2_5",
        "w2_6",
        "w2_7",
        "w2_8",
        "w2_10",
        "w2_11",
        "w2_12",
        "w2_13",
        "w2_14",
        "w2_15",
        "w2_16",
        "w2_17",
        "w2_18",
        "w2_19",
        "w2_20",
        "1099_int_1",
        "1099_int_2",
        "1099_int_3",
        "1099_int_4",
        "1099_int_5",
        "1099_int_6",
        "1099_int_8",
        "1099_int_9",
        "1099_int_10",
        "1099_int_11",
        "1099_int_12",
        "1099_int_13",
        "1099_int_16",
        "1099_int_17",
        "1099_div_1a",
        "1099_div_1b",
        "1099_div_2a",
        "1099_div_2b",
        "1099_div_2c",
        "1099_div_2d",
        "1099_div_2e",
        "1099_div_2f",
        "1099_div_3",
        "1099_div_4",
        "1099_div_5",
        "1099_div_6",
        "1099_div_7",
        "1099_div_9",
        "1099_div_10",
        "1099_div_12",
        "1099_div_13",
        "1099_div_15",
        "1099_div_16",
        "1098_1",
        "1098_2",
        "1098_3",
        "1098_4",
        "1098_5",
        "1098_6",
        "1098_8",
        "1098_9",
        "1098_10",
        "1098_11",
        "1099_b_1d",
        "1099_b_1e",
        "1099_b_1f",
        "1099_b_1g",
        "1099_b_4",
        "1099_b_15",
        "1099_b_16",
        "term_short_covered",
        "term_short_noncovered",
        "term_long_covered",
        "term_long_noncovered",
        "term_unknown_term",
    )

    private fun formPrefix(taxFormKind: String?): String = when (taxFormKind) {
        "1099_int", "1099_div", "1098", "1099_b" -> taxFormKind
        else -> "w2"
    }

    fun boxKey(taxFormKind: String?, boxCode: String?): String? {
        val key = formPrefix(taxFormKind) + "_" + boxCode.orEmpty().trim().lowercase()
        return key.takeIf { each -> knownKeys.contains(each) }
    }

    /** Which of a 1099-B's totals a box belongs to, by the box's qualifier. */
    fun termKey(taxFormKind: String?, boxQualifier: String?): String? {
        if (taxFormKind != "1099_b") return null
        val key = "term_" + boxQualifier.orEmpty()
        return key.takeIf { each -> knownKeys.contains(each) }
    }
}
