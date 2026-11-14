package com.teanode.kit.finance

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.util.UUID

// Receipts, paystubs and tax forms typed in by hand: the same arguments the CLI's record commands and the agent's
// finance tool send to RecordReceipt, PreviewRecordReceipt, RecordPaystub and RecordTaxForm, with the same words. A
// receipt, a paystub or a tax form is always recorded from a file the server holds (agentAttachmentId), so a form
// picks one first.

/** FinanceEntryKinds are the fixed vocabularies the record operations take. */
object FinanceEntryKinds {
    val receiptLineKinds = listOf("item", "discount", "tax", "fee", "tip")
    val paystubLineKinds = listOf("earning", "tax", "deduction", "reimbursement")
    val paystubTaxKinds = listOf(
        "federal_income", "social_security", "medicare", "medicare_additional", "state_income", "local_income",
        "state_disability", "other",
    )
    val paystubDeductionKinds = listOf(
        "retirement_traditional", "retirement_roth", "hsa", "health_premium", "fsa_health", "fsa_dependent_care", "commuter",
        "insurance", "other",
    )
    val taxFormKinds = listOf("w2", "1099_int", "1099_div", "1099_b", "1098")
}

/**
 * FinanceEntryProblem is the first thing a form is missing or has wrong, named by the label the form shows on the
 * field, so a form can say why it cannot go on.
 */
data class FinanceEntryProblem(
    val field: String,
    val reason: Reason,
    /** The line or box it is on, counted from one, when it is on one. */
    val lineNumber: Int? = null,
) {
    enum class Reason { Missing, NotAmount, NotNumber, NotDay, NotCurrencyCode, NotLastFourDigits, NotYear, NoLines, NoBoxes }

    internal fun on(lineNumber: Int): FinanceEntryProblem = copy(lineNumber = lineNumber)
}

/** FinanceEntryText checks what a person typed the way the server does, so a form can say it is not ready before asking. */
object FinanceEntryText {
    private val dayPattern = Regex("""^\d{4}-\d{2}-\d{2}$""")
    private val quantityPattern = Regex("""^\d+(\.\d{1,8})?$""")
    private val currencyCodePattern = Regex("""^[A-Za-z]{3}$""")
    private val lastFourDigitsPattern = Regex("""^\d{4}$""")

    /** A day written 2026-10-02. */
    fun isDay(text: String): Boolean = dayPattern.matches(text)

    /** A decimal amount, below zero only where the field allows it. */
    fun isAmount(text: String, isNegativeAllowed: Boolean): Boolean {
        val amount = normalized(text)
        return FinanceMoney.isDecimal(amount) && (isNegativeAllowed || !amount.startsWith("-"))
    }

    /** A quantity or hours: a decimal with up to eight places, never below zero. */
    fun isQuantity(text: String): Boolean = quantityPattern.matches(normalized(text))

    /** A currency code such as USD. */
    fun isCurrencyCode(text: String): Boolean = currencyCodePattern.matches(text)

    /** The last four digits of a taxpayer number, never more. */
    fun isTaxIdMask(text: String): Boolean = lastFourDigitsPattern.matches(text)

    internal fun trimmed(text: String): String = text.trim()

    /** A number as it is sent: trimmed, and a lone comma with no point read as the decimal point, as a keyboard of another region types it. */
    internal fun normalized(text: String): String {
        val value = trimmed(text)
        val commaCount = value.count { character -> character == ',' }
        if (commaCount == 1 && !value.contains('.')) return value.replace(',', '.')
        return value
    }

    /** Whether an optional field is empty or passes the check. */
    internal fun isEmptyOr(text: String, check: (String) -> Boolean): Boolean {
        val value = trimmed(text)
        return value.isEmpty() || check(value)
    }

    /**
     * What is wrong with a field: nothing when it passes the check, or when it is empty and optional; missing when it
     * is empty and required.
     */
    internal fun problem(
        field: String,
        text: String,
        isRequired: Boolean,
        reason: FinanceEntryProblem.Reason,
        check: (String) -> Boolean,
    ): FinanceEntryProblem? {
        val value = trimmed(text)
        if (value.isEmpty()) return if (isRequired) FinanceEntryProblem(field, FinanceEntryProblem.Reason.Missing) else null
        return if (check(value)) null else FinanceEntryProblem(field, reason)
    }

    internal fun amountProblem(field: String, text: String, isRequired: Boolean = false, isNegativeAllowed: Boolean = false) =
        problem(field, text, isRequired, FinanceEntryProblem.Reason.NotAmount) { value -> isAmount(value, isNegativeAllowed) }

    internal fun quantityProblem(field: String, text: String) =
        problem(field, text, false, FinanceEntryProblem.Reason.NotNumber, ::isQuantity)

    internal fun dayProblem(field: String, text: String, isRequired: Boolean = false) =
        problem(field, text, isRequired, FinanceEntryProblem.Reason.NotDay, ::isDay)

    internal fun lastFourDigitsProblem(field: String, text: String) =
        problem(field, text, false, FinanceEntryProblem.Reason.NotLastFourDigits, ::isTaxIdMask)

    internal fun nameProblem(field: String, text: String): FinanceEntryProblem? =
        if (trimmed(text).isEmpty()) FinanceEntryProblem(field, FinanceEntryProblem.Reason.Missing) else null

    /** The first of the problems that is one. */
    internal fun first(problems: List<FinanceEntryProblem?>): FinanceEntryProblem? = problems.filterNotNull().firstOrNull()
}

/** Sets a text argument unless it is empty, as the server's nullable arguments are left out. */
private fun MutableMap<String, JsonElement>.setText(key: String, text: String) {
    val value = FinanceEntryText.trimmed(text)
    if (value.isNotEmpty()) this[key] = JsonPrimitive(value)
}

/** Sets a number argument unless it is empty, as the server reads it. */
private fun MutableMap<String, JsonElement>.setNumber(key: String, text: String) {
    val value = FinanceEntryText.normalized(text)
    if (value.isNotEmpty()) this[key] = JsonPrimitive(value)
}

// MARK: Receipt

/** ReceiptLineDraft is one printed line of a receipt being typed in. */
data class ReceiptLineDraft(
    val id: String = UUID.randomUUID().toString(),
    val receiptLineKind: String = "item",
    val description: String = "",
    val quantity: String = "",
    val unitPriceAmount: String = "",
    val lineAmount: String = "",
) {
    /** A line nothing was typed on, which is left out rather than refused. */
    val isBlank: Boolean
        get() = listOf(description, quantity, unitPriceAmount, lineAmount).all { text -> FinanceEntryText.trimmed(text).isEmpty() }

    /** Its amount is a number, and the unit price and quantity are numbers when given. */
    val isValid: Boolean get() = firstProblem == null

    /** The first field of the line that is not what the server takes. */
    val firstProblem: FinanceEntryProblem?
        get() = FinanceEntryText.first(
            listOf(
                FinanceEntryText.quantityProblem("Quantity", quantity),
                FinanceEntryText.amountProblem("Unit price", unitPriceAmount),
                FinanceEntryText.amountProblem("Amount", lineAmount, isRequired = true, isNegativeAllowed = true),
            ),
        )

    internal val argument: JsonElement
        get() {
            val fields = mutableMapOf<String, JsonElement>(
                "receiptLineKind" to JsonPrimitive(receiptLineKind),
                "lineAmount" to JsonPrimitive(FinanceEntryText.normalized(lineAmount)),
            )
            fields.setText("description", description)
            fields.setNumber("quantity", quantity)
            fields.setNumber("unitPriceAmount", unitPriceAmount)
            return JsonObject(fields)
        }
}

/** ReceiptDraft is a receipt being typed in: what RecordReceiptArguments take besides the file it was read from. */
data class ReceiptDraft(
    val merchantName: String = "",
    val merchantReceiptNumber: String = "",
    val purchasedOn: String = "",
    val currencyCode: String = "",
    val subtotalAmount: String = "",
    val totalAmount: String = "",
    val paymentAccountMask: String = "",
    val receiptLines: List<ReceiptLineDraft> = listOf(ReceiptLineDraft()),
    val isUnbalancedAccepted: Boolean = false,
) {
    /** The lines that have something typed on them. */
    val filledLines: List<ReceiptLineDraft> get() = receiptLines.filter { line -> !line.isBlank }

    /** Whether the server could be asked: a merchant, a currency, a total and lines that are numbers, and a day when one is given. */
    val isReady: Boolean get() = firstProblem == null

    /** The first field that is empty or not what the server takes, in the order the form shows them. */
    val firstProblem: FinanceEntryProblem?
        get() {
            val lineProblem = filledLines.withIndex().mapNotNull { (index, line) -> line.firstProblem?.on(index + 1) }.firstOrNull()
            return FinanceEntryText.first(
                listOf(
                    FinanceEntryText.nameProblem("Merchant", merchantName),
                    FinanceEntryText.dayProblem("Bought on", purchasedOn),
                    FinanceEntryText.problem("Currency", currencyCode, true, FinanceEntryProblem.Reason.NotCurrencyCode, FinanceEntryText::isCurrencyCode),
                    if (filledLines.isEmpty()) FinanceEntryProblem("Line", FinanceEntryProblem.Reason.NoLines) else null,
                    lineProblem,
                    FinanceEntryText.amountProblem("Subtotal", subtotalAmount),
                    FinanceEntryText.amountProblem("Total", totalAmount, isRequired = true),
                ),
            )
        }

    /**
     * The arguments of PreviewRecordReceipt and RecordReceipt for the file it was read from. Only the recording says a
     * receipt that does not add up is accepted.
     */
    fun variables(attachmentId: String, isRecording: Boolean): Map<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>(
            "agentAttachmentId" to JsonPrimitive(attachmentId),
            "merchantName" to JsonPrimitive(FinanceEntryText.trimmed(merchantName)),
            "currencyCode" to JsonPrimitive(FinanceEntryText.trimmed(currencyCode).uppercase()),
            "totalAmount" to JsonPrimitive(FinanceEntryText.normalized(totalAmount)),
            "receiptLines" to JsonArray(filledLines.map { line -> line.argument }),
        )
        variables.setText("merchantReceiptNumber", merchantReceiptNumber)
        variables.setText("purchasedOn", purchasedOn)
        variables.setNumber("subtotalAmount", subtotalAmount)
        variables.setText("paymentAccountMask", paymentAccountMask)
        if (isRecording && isUnbalancedAccepted) variables["isUnbalancedAccepted"] = JsonPrimitive(true)
        return variables
    }
}

val ReceiptEntryCandidate.id: String get() = financeTransactionId

val ReceiptEntryPreview.isBalanced: Boolean get() = receiptCheckState == "balanced"

val ReceiptEntryPreview.candidates: List<ReceiptEntryCandidate> get() = receiptMatchCandidates.orEmpty().filterNotNull()

val RecordedReceipt.candidates: List<ReceiptEntryCandidate> get() = receiptMatchCandidates.orEmpty().filterNotNull()

/** Whether the matcher matched it to a charge on its own. */
val RecordedReceipt.isMatched: Boolean get() = financeReceipt?.receiptMatches.orEmpty().filterNotNull().isNotEmpty()

// MARK: Paystub

/**
 * PaystubLineDraft is one line of a paystub being typed in. What a line takes depends on its kind: an earning its
 * hours and rate, a tax which tax and the wages it was figured on, a deduction what it pays for and the employer's part.
 */
data class PaystubLineDraft(
    val id: String = UUID.randomUUID().toString(),
    val paystubLineKind: String = "earning",
    val description: String = "",
    val hours: String = "",
    val rateAmount: String = "",
    val currentAmount: String = "",
    val yearToDateAmount: String = "",
    val paystubTaxKind: String = "federal_income",
    val taxJurisdictionCode: String = "",
    val currentWagesAmount: String = "",
    val yearToDateWagesAmount: String = "",
    val paystubDeductionKind: String = "retirement_traditional",
    val employerCurrentAmount: String = "",
    val employerYearToDateAmount: String = "",
) {
    /** A line nothing was typed on, which is left out rather than refused. */
    val isBlank: Boolean
        get() = listOf(
            description, hours, rateAmount, currentAmount, yearToDateAmount, taxJurisdictionCode, currentWagesAmount,
            yearToDateWagesAmount, employerCurrentAmount, employerYearToDateAmount,
        ).all { text -> FinanceEntryText.trimmed(text).isEmpty() }

    /** An amount for the period or the year, every amount a number (the two the line itself carries may be below zero), and hours a decimal. */
    val isValid: Boolean get() = firstProblem == null

    /** The first field of the line that is empty or not what the server takes. */
    val firstProblem: FinanceEntryProblem?
        get() {
            val hasAmount = listOf(currentAmount, yearToDateAmount).any { text -> FinanceEntryText.trimmed(text).isNotEmpty() }
            val problems: List<FinanceEntryProblem?> = when (paystubLineKind) {
                "earning" -> listOf(
                    FinanceEntryText.quantityProblem("Hours", hours),
                    FinanceEntryText.amountProblem("Rate", rateAmount),
                )
                "tax" -> listOf(
                    FinanceEntryText.amountProblem("Wages this period", currentWagesAmount),
                    FinanceEntryText.amountProblem("Wages this year", yearToDateWagesAmount),
                )
                "deduction" -> listOf(
                    FinanceEntryText.amountProblem("Employer adds this period", employerCurrentAmount),
                    FinanceEntryText.amountProblem("Employer adds this year", employerYearToDateAmount),
                )
                else -> emptyList()
            }
            val missingAmount = if (hasAmount) null else FinanceEntryProblem("Amount this period", FinanceEntryProblem.Reason.Missing)
            return FinanceEntryText.first(
                problems + listOf(
                    missingAmount,
                    FinanceEntryText.amountProblem("Amount this period", currentAmount, isNegativeAllowed = true),
                    FinanceEntryText.amountProblem("Amount this year", yearToDateAmount, isNegativeAllowed = true),
                ),
            )
        }

    internal val argument: JsonElement
        get() {
            val fields = mutableMapOf<String, JsonElement>("paystubLineKind" to JsonPrimitive(paystubLineKind))
            fields.setText("description", description)
            fields.setNumber("currentAmount", currentAmount)
            fields.setNumber("yearToDateAmount", yearToDateAmount)
            when (paystubLineKind) {
                "earning" -> {
                    fields.setNumber("hours", hours)
                    fields.setNumber("rateAmount", rateAmount)
                }
                "tax" -> {
                    fields["paystubTaxKind"] = JsonPrimitive(paystubTaxKind)
                    fields.setText("taxJurisdictionCode", taxJurisdictionCode)
                    fields.setNumber("currentWagesAmount", currentWagesAmount)
                    fields.setNumber("yearToDateWagesAmount", yearToDateWagesAmount)
                }
                "deduction" -> {
                    fields["paystubDeductionKind"] = JsonPrimitive(paystubDeductionKind)
                    fields.setNumber("employerCurrentAmount", employerCurrentAmount)
                    fields.setNumber("employerYearToDateAmount", employerYearToDateAmount)
                }
            }
            return JsonObject(fields)
        }
}

/** PaystubDraft is a paystub being typed in: what RecordPaystubArguments take besides the file it was read from. */
data class PaystubDraft(
    val employerName: String = "",
    val employerTaxId: String = "",
    val employeeName: String = "",
    val employeeTaxIdMask: String = "",
    val payDate: String = "",
    val periodStartOn: String = "",
    val periodEndOn: String = "",
    val currencyCode: String = "",
    val grossAmount: String = "",
    val yearToDateGrossAmount: String = "",
    val totalTaxAmount: String = "",
    val totalDeductionAmount: String = "",
    val netAmount: String = "",
    val paystubLines: List<PaystubLineDraft> = listOf(PaystubLineDraft()),
    val isUnbalancedAccepted: Boolean = false,
) {
    /** The lines that have something typed on them. */
    val filledLines: List<PaystubLineDraft> get() = paystubLines.filter { line -> !line.isBlank }

    /** Whether the server could be asked: who paid whom, the pay date, gross and take-home pay, and lines with an amount each. */
    val isReady: Boolean get() = firstProblem == null

    /** The first field that is empty or not what the server takes, in the order the form shows them. */
    val firstProblem: FinanceEntryProblem?
        get() {
            val lineProblem = filledLines.withIndex().mapNotNull { (index, line) -> line.firstProblem?.on(index + 1) }.firstOrNull()
            return FinanceEntryText.first(
                listOf(
                    FinanceEntryText.nameProblem("Employer", employerName),
                    FinanceEntryText.nameProblem("Employee", employeeName),
                    FinanceEntryText.lastFourDigitsProblem("Taxpayer number, last 4", employeeTaxIdMask),
                    FinanceEntryText.dayProblem("Pay date", payDate, isRequired = true),
                    FinanceEntryText.dayProblem("Period starts", periodStartOn),
                    FinanceEntryText.dayProblem("Period ends", periodEndOn),
                    FinanceEntryText.problem("Currency", currencyCode, false, FinanceEntryProblem.Reason.NotCurrencyCode, FinanceEntryText::isCurrencyCode),
                    FinanceEntryText.amountProblem("Gross pay", grossAmount, isRequired = true),
                    FinanceEntryText.amountProblem("Gross pay this year", yearToDateGrossAmount),
                    FinanceEntryText.amountProblem("Total taxes", totalTaxAmount),
                    FinanceEntryText.amountProblem("Total deductions", totalDeductionAmount),
                    FinanceEntryText.amountProblem("Take-home pay", netAmount, isRequired = true, isNegativeAllowed = true),
                    if (filledLines.isEmpty()) FinanceEntryProblem("Line", FinanceEntryProblem.Reason.NoLines) else null,
                    lineProblem,
                ),
            )
        }

    /** The arguments of RecordPaystub for the file it was read from. */
    fun variables(attachmentId: String): Map<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>(
            "agentAttachmentId" to JsonPrimitive(attachmentId),
            "employerName" to JsonPrimitive(FinanceEntryText.trimmed(employerName)),
            "employeeName" to JsonPrimitive(FinanceEntryText.trimmed(employeeName)),
            "payDate" to JsonPrimitive(FinanceEntryText.trimmed(payDate)),
            "grossAmount" to JsonPrimitive(FinanceEntryText.normalized(grossAmount)),
            "netAmount" to JsonPrimitive(FinanceEntryText.normalized(netAmount)),
            "paystubLines" to JsonArray(filledLines.map { line -> line.argument }),
        )
        variables.setText("employerTaxId", employerTaxId)
        variables.setText("employeeTaxIdMask", employeeTaxIdMask)
        variables.setText("periodStartOn", periodStartOn)
        variables.setText("periodEndOn", periodEndOn)
        variables.setNumber("yearToDateGrossAmount", yearToDateGrossAmount)
        variables.setNumber("totalTaxAmount", totalTaxAmount)
        variables.setNumber("totalDeductionAmount", totalDeductionAmount)
        val code = FinanceEntryText.trimmed(currencyCode)
        if (code.isNotEmpty()) variables["currencyCode"] = JsonPrimitive(code.uppercase())
        if (isUnbalancedAccepted) variables["isUnbalancedAccepted"] = JsonPrimitive(true)
        return variables
    }
}

// MARK: Tax form

/** TaxFormEntryDraft is one box of a tax form being typed in: its number, what tells boxes of one number apart, and its amount, text or mark. */
data class TaxFormEntryDraft(
    val id: String = UUID.randomUUID().toString(),
    val boxCode: String = "",
    val boxQualifier: String = "",
    val amount: String = "",
    val textValue: String = "",
    val isChecked: Boolean = false,
) {
    /** A box nothing was typed on, which is left out rather than refused. */
    val isBlank: Boolean
        get() = listOf(boxCode, boxQualifier, amount, textValue).all { text -> FinanceEntryText.trimmed(text).isEmpty() } && !isChecked

    /** A box number, and an amount that is a number when given. */
    val isValid: Boolean get() = firstProblem == null

    /** The first field of the box that is empty or not what the server takes. */
    val firstProblem: FinanceEntryProblem?
        get() = FinanceEntryText.first(
            listOf(FinanceEntryText.nameProblem("Box", boxCode), FinanceEntryText.amountProblem("Amount", amount)),
        )

    internal val argument: JsonElement
        get() {
            val fields = mutableMapOf<String, JsonElement>("boxCode" to JsonPrimitive(FinanceEntryText.trimmed(boxCode)))
            fields.setText("boxQualifier", boxQualifier)
            fields.setNumber("amount", amount)
            fields.setText("textValue", textValue)
            if (isChecked) fields["isChecked"] = JsonPrimitive(true)
            return JsonObject(fields)
        }
}

/** TaxFormDraft is a tax form being typed in: what RecordTaxFormArguments take besides the file it was read from. */
data class TaxFormDraft(
    val taxYear: Int,
    val taxFormKind: String = "w2",
    val isCorrected: Boolean = false,
    val payerName: String = "",
    val payerTaxId: String = "",
    val recipientName: String = "",
    val recipientTaxIdMask: String = "",
    val taxFormEntries: List<TaxFormEntryDraft> = listOf(TaxFormEntryDraft()),
) {
    /** The boxes that have something typed on them. */
    val filledEntries: List<TaxFormEntryDraft> get() = taxFormEntries.filter { entry -> !entry.isBlank }

    /** Whether the server could be asked: who paid whom, and boxes that are numbers. */
    val isReady: Boolean get() = firstProblem == null

    /** The first field that is empty or not what the server takes, in the order the form shows them. */
    val firstProblem: FinanceEntryProblem?
        get() {
            val entryProblem = filledEntries.withIndex().mapNotNull { (index, entry) -> entry.firstProblem?.on(index + 1) }.firstOrNull()
            return FinanceEntryText.first(
                listOf(
                    if (taxYear in 1900..2999) null else FinanceEntryProblem("Tax year", FinanceEntryProblem.Reason.NotYear),
                    FinanceEntryText.nameProblem("Payer", payerName),
                    FinanceEntryText.nameProblem("Recipient", recipientName),
                    FinanceEntryText.lastFourDigitsProblem("Taxpayer number, last 4", recipientTaxIdMask),
                    if (filledEntries.isEmpty()) FinanceEntryProblem("Box", FinanceEntryProblem.Reason.NoBoxes) else null,
                    entryProblem,
                ),
            )
        }

    /** The arguments of RecordTaxForm for the file it was read from. */
    fun variables(attachmentId: String): Map<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>(
            "agentAttachmentId" to JsonPrimitive(attachmentId),
            "taxFormKind" to JsonPrimitive(taxFormKind),
            "taxYear" to JsonPrimitive(taxYear),
            "payerName" to JsonPrimitive(FinanceEntryText.trimmed(payerName)),
            "recipientName" to JsonPrimitive(FinanceEntryText.trimmed(recipientName)),
            "taxFormEntries" to JsonArray(filledEntries.map { entry -> entry.argument }),
        )
        variables.setText("payerTaxId", payerTaxId)
        variables.setText("recipientTaxIdMask", recipientTaxIdMask)
        if (isCorrected) variables["isCorrected"] = JsonPrimitive(true)
        return variables
    }
}
