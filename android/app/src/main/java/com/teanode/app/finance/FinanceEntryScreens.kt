package com.teanode.app.finance

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import com.teanode.kit.finance.isMatched
import com.teanode.kit.asReasonSentence
import androidx.compose.ui.graphics.Color
import androidx.compose.material3.Icon
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material.icons.outlined.Verified
import androidx.compose.material.icons.Icons
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shopping.readPendingFile
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.finance.FinanceEntryKinds
import com.teanode.kit.finance.FinanceEntryProblem
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.PaystubDraft
import com.teanode.kit.finance.PaystubLineDraft
import com.teanode.kit.finance.ReceiptDraft
import com.teanode.kit.finance.ReceiptEntryPreview
import com.teanode.kit.finance.ReceiptLineDraft
import com.teanode.kit.finance.TaxFormDraft
import com.teanode.kit.finance.TaxFormEntryDraft
import com.teanode.kit.finance.candidates
import com.teanode.kit.finance.isBalanced
import com.teanode.kit.finance.previewRecordReceipt
import com.teanode.kit.finance.recordPaystub
import com.teanode.kit.finance.recordReceipt
import com.teanode.kit.finance.recordTaxForm
import com.teanode.kit.finance.shownName
import com.teanode.kit.finance.exchangeRate
import com.teanode.kit.finance.reportingCurrency
import com.teanode.kit.finance.shownRate
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.LocalDate

/** Which of the three things is typed in; a receipt is kept as any upload, a paystub and a tax form as a tax document. */
internal enum class EntryKind { Receipt, Paystub, TaxForm }

/** The label a field of a form is shown with, for the English name the kit's problems name it by. */
@Composable
private fun fieldLabel(field: String): String {
    val resource = when (field) {
        "Amount" -> R.string.finance_amount
        "Amount this period" -> R.string.finance_entry_amount_period
        "Amount this year" -> R.string.finance_entry_amount_year
        "Bought on" -> R.string.finance_bought_on
        "Box" -> R.string.finance_entry_box
        "Employee" -> R.string.finance_tax_employee
        "Employer" -> R.string.finance_entry_employer
        "Employer adds this period" -> R.string.finance_entry_employer_adds_period
        "Employer adds this year" -> R.string.finance_entry_employer_adds_year
        "Gross pay" -> R.string.finance_tax_gross
        "Gross pay this year" -> R.string.finance_tax_gross_year
        "Hours" -> R.string.finance_entry_hours
        "Merchant" -> R.string.finance_merchant
        "Pay date" -> R.string.finance_entry_pay_date
        "Payer" -> R.string.finance_tax_payer
        "Period ends" -> R.string.finance_entry_period_ends
        "Period starts" -> R.string.finance_entry_period_starts
        "Quantity" -> R.string.finance_entry_quantity
        "Rate" -> R.string.finance_entry_rate
        "Recipient" -> R.string.finance_tax_recipient
        "Subtotal" -> R.string.finance_subtotal
        "Take-home pay" -> R.string.finance_tax_take_home
        "Taxpayer number, last 4" -> R.string.finance_entry_taxpayer_last_four
        "Tax year" -> R.string.finance_entry_tax_year
        "Total" -> R.string.finance_total
        "Total deductions" -> R.string.finance_tax_total_deductions
        "Total taxes" -> R.string.finance_tax_total_taxes
        "Unit price" -> R.string.finance_entry_unit_price
        "Wages this period" -> R.string.finance_entry_wages_period
        "Wages this year" -> R.string.finance_entry_wages_year
        else -> return field
    }
    return stringResource(resource)
}

/** What is wrong with a field, in a sentence: "Total is not an amount", with the line or box it is on. */
@Composable
private fun problemSentence(problem: FinanceEntryProblem?, isBox: Boolean = false): String? {
    if (problem == null) return null
    val field = fieldLabel(problem.field)
    val sentence = when (problem.reason) {
        FinanceEntryProblem.Reason.Missing -> stringResource(R.string.finance_entry_problem_empty, field)
        FinanceEntryProblem.Reason.NotAmount -> stringResource(R.string.finance_entry_problem_not_amount, field)
        FinanceEntryProblem.Reason.NotNumber -> stringResource(R.string.finance_entry_problem_not_number, field)
        FinanceEntryProblem.Reason.NotDay -> stringResource(R.string.finance_entry_problem_not_day, field)
        FinanceEntryProblem.Reason.NotCurrencyCode -> stringResource(R.string.finance_entry_problem_not_currency, field)
        FinanceEntryProblem.Reason.NotLastFourDigits -> stringResource(R.string.finance_entry_problem_not_four_digits, field)
        FinanceEntryProblem.Reason.NotYear -> stringResource(R.string.finance_entry_problem_not_year, field)
        FinanceEntryProblem.Reason.NoLines -> return stringResource(R.string.finance_entry_no_lines)
        FinanceEntryProblem.Reason.NoBoxes -> return stringResource(R.string.finance_entry_no_boxes)
    }
    val number = problem.lineNumber ?: return sentence
    return stringResource(if (isBox) R.string.finance_entry_box_problem else R.string.finance_entry_line_problem, number, sentence)
}

/** A kind of line or form in words: "Social Security", not the "social_security" the server names it by. */
@Composable
private fun entryKindWords(kind: String): String {
    when (kind) {
        "w2" -> return "W-2"
        "1099_int" -> return "1099-INT"
        "1099_div" -> return "1099-DIV"
        "1099_b" -> return "1099-B"
        "1098" -> return "1098"
    }
    val context = LocalContext.current
    val identifier = context.resources.getIdentifier("finance_entry_kind_$kind", "string", context.packageName)
    return if (identifier == 0) kind else stringResource(identifier)
}

@Composable
private fun Field(label: String, value: String, onChange: (String) -> Unit, modifier: Modifier = Modifier, placeholder: String? = null) {
    OutlinedTextField(value, onChange, label = { Text(label) }, placeholder = placeholder?.let { hint -> { Text(hint) } }, singleLine = true, modifier = modifier.fillMaxWidth())
}

/** "1 EUR = 1.0834 USD" under a currency field, read when the code changes; nothing for the person's own currency, a code not typed in full, or a day the server has no rate. */
@Composable
internal fun ExchangeRateNote(currencyCode: String) {
    val connection = LocalConnection.current
    var rate by remember { mutableStateOf<com.teanode.kit.finance.CurrencyRate?>(null) }
    androidx.compose.runtime.LaunchedEffect(currencyCode.trim().uppercase()) {
        rate = null
        try {
            val pair = com.teanode.kit.finance.ExchangeRateLookup.pair(currencyCode, connection.reportingCurrency().currencyCode) ?: return@LaunchedEffect
            rate = connection.exchangeRate(pair.first, pair.second)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            // No rate is no note.
        }
    }
    val shown = rate?.shownRate ?: return
    Text(stringResource(R.string.finance_exchange_rate_note, rate!!.fromCurrencyCode, shown, rate!!.toCurrencyCode), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun KindPicker(label: String, value: String, options: List<String>, onChange: (String) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(isOpen, { isOpen = it }) {
        OutlinedTextField(entryKindWords(value), {}, readOnly = true, label = { Text(label) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth())
        ExposedDropdownMenu(isOpen, { isOpen = false }) { for (option in options) DropdownMenuItem(text = { Text(entryKindWords(option)) }, onClick = { onChange(option); isOpen = false }) }
    }
}

/** The frame of an entry form: the file it is recorded from, the form, the first thing wrong, and Record. */
@Composable
private fun EntryShell(
    title: String,
    kind: EntryKind,
    problem: FinanceEntryProblem?,
    isWorking: Boolean,
    onRecord: (attachmentId: String) -> Unit,
    extraActions: @Composable (attachmentId: String?) -> Unit = {},
    isRecordable: (attachmentId: String?) -> Boolean = { true },
    isBox: Boolean = false,
    form: @Composable (attachmentId: String?) -> Unit,
) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var attachmentId by remember { mutableStateOf<String?>(null) }
    var fileName by remember { mutableStateOf("") }
    var isUploading by remember { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            isUploading = true
            try {
                val file = context.readPendingFile(uri, "entry")
                attachmentId = connection.upload(file.name, file.contentType, file.data, isTaxDocument = kind != EntryKind.Receipt)
                fileName = file.name
            } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
            isUploading = false
        }
    }
    TeaNodeScreen(title = title, onBack = { navigator.back() }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.finance_entry_file_header), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            if (attachmentId != null) Text(stringResource(R.string.finance_entry_file_chosen, fileName), style = MaterialTheme.typography.bodyMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedButton(onClick = { picker.launch("*/*") }, enabled = !isUploading) { Text(stringResource(if (attachmentId == null) R.string.finance_entry_choose_file else R.string.finance_entry_change_file)) }
                if (attachmentId != null) TextButton(onClick = { attachmentId = null; fileName = "" }) { Text(stringResource(R.string.finance_entry_remove_file)) }
            }
            Text(
                stringResource(
                    when (kind) {
                        EntryKind.Receipt -> R.string.finance_entry_file_receipt
                        EntryKind.Paystub -> R.string.finance_entry_file_paystub
                        EntryKind.TaxForm -> R.string.finance_entry_file_form
                    },
                ),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            form(attachmentId)
            val sentence = if (attachmentId == null) stringResource(R.string.finance_entry_add_file_first) else problemSentence(problem, isBox)
            if (sentence != null) Text(sentence, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                extraActions(attachmentId)
                Button(enabled = problem == null && attachmentId != null && !isWorking && isRecordable(attachmentId), onClick = { onRecord(attachmentId!!) }) { Text(stringResource(R.string.finance_entry_record)) }
            }
        }
    }
}

/** A receipt typed in from a picture the person chose: checked first, then recorded. */
@Composable
fun FinanceReceiptEntryScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var draft by remember { mutableStateOf(ReceiptDraft(currencyCode = "USD", purchasedOn = LocalDate.now().toString())) }
    var preview by remember { mutableStateOf<ReceiptEntryPreview?>(null) }
    var previewedDraft by remember { mutableStateOf<ReceiptDraft?>(null) }
    var previewedAttachmentId by remember { mutableStateOf<String?>(null) }
    var isWorking by remember { mutableStateOf(false) }
    // Whether the preview on screen is of what is typed now and of the file chosen now.
    fun isPreviewCurrent(attachmentId: String?): Boolean = preview != null && previewedDraft?.copy(isUnbalancedAccepted = false) == draft.copy(isUnbalancedAccepted = false) && previewedAttachmentId == attachmentId
    EntryShell(
        stringResource(R.string.finance_entry_receipt_title), EntryKind.Receipt, draft.firstProblem, isWorking,
        isRecordable = { id -> isPreviewCurrent(id) && (preview?.isBalanced == true || draft.isUnbalancedAccepted) },
        extraActions = { id ->
            OutlinedButton(
                enabled = draft.isReady && id != null && !isWorking,
                onClick = {
                    scope.launch {
                        isWorking = true
                        try { val read = connection.previewRecordReceipt(draft, id!!); preview = read; draft = draft.copy(isUnbalancedAccepted = false); previewedDraft = draft; previewedAttachmentId = id } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                        isWorking = false
                    }
                },
            ) { Text(stringResource(R.string.finance_entry_check)) }
        },
        onRecord = { id ->
            scope.launch {
                isWorking = true
                try {
                    val recorded = connection.recordReceipt(draft, id)
                    snackbar.show(context.getString(if (recorded?.isMatched == true) R.string.finance_entry_receipt_recorded_matched else R.string.finance_entry_receipt_recorded))
                    navigator.back()
                } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                isWorking = false
            }
        },
    ) { attachmentId ->
        Field(stringResource(R.string.finance_merchant), draft.merchantName, { draft = draft.copy(merchantName = it) })
        Field(stringResource(R.string.finance_bought_on), draft.purchasedOn, { draft = draft.copy(purchasedOn = it) })
        Field(stringResource(R.string.finance_currency), draft.currencyCode, { draft = draft.copy(currencyCode = it.uppercase()) })
        ExchangeRateNote(draft.currencyCode)
        Field(stringResource(R.string.finance_receipt_number), draft.merchantReceiptNumber, { draft = draft.copy(merchantReceiptNumber = it) })
        Field(stringResource(R.string.finance_subtotal), draft.subtotalAmount, { draft = draft.copy(subtotalAmount = it) })
        Field(stringResource(R.string.finance_total), draft.totalAmount, { draft = draft.copy(totalAmount = it) })
        Field(stringResource(R.string.finance_entry_card_last_four), draft.paymentAccountMask, { draft = draft.copy(paymentAccountMask = it) })
        draft.receiptLines.forEachIndexed { index, line ->
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                TeaNodeSectionHeader(stringResource(R.string.finance_entry_line_header, index + 1))
                KindPicker(stringResource(R.string.finance_kind), line.receiptLineKind, FinanceEntryKinds.receiptLineKinds) { kind -> draft = draft.copy(receiptLines = draft.receiptLines.replaced(index, line.copy(receiptLineKind = kind))) }
                Field(stringResource(R.string.finance_entry_description), line.description, { text -> draft = draft.copy(receiptLines = draft.receiptLines.replaced(index, line.copy(description = text))) })
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Field(stringResource(R.string.finance_entry_quantity), line.quantity, { text -> draft = draft.copy(receiptLines = draft.receiptLines.replaced(index, line.copy(quantity = text))) }, Modifier.weight(1f))
                    Field(stringResource(R.string.finance_entry_unit_price), line.unitPriceAmount, { text -> draft = draft.copy(receiptLines = draft.receiptLines.replaced(index, line.copy(unitPriceAmount = text))) }, Modifier.weight(1f))
                    Field(stringResource(R.string.finance_amount), line.lineAmount, { text -> draft = draft.copy(receiptLines = draft.receiptLines.replaced(index, line.copy(lineAmount = text))) }, Modifier.weight(1f))
                }
                if (draft.receiptLines.size > 1) TextButton(onClick = { draft = draft.copy(receiptLines = draft.receiptLines.filterIndexed { each, _ -> each != index }) }) { Text(stringResource(R.string.finance_entry_remove_line)) }
            }
        }
        TextButton(onClick = { draft = draft.copy(receiptLines = draft.receiptLines + ReceiptLineDraft()) }) { Text(stringResource(R.string.finance_entry_add_line)) }
        Footnote(stringResource(R.string.finance_entry_discount_footer))
        val read = preview
        if (read != null && isPreviewCurrent(attachmentId)) ReceiptPreviewSections(read, draft.currencyCode.trim().uppercase(), draft.isUnbalancedAccepted) { isAccepted -> draft = draft.copy(isUnbalancedAccepted = isAccepted) }
    }
}

/** What the server says recording the receipt would do: whether its lines add up, whether it replaces one read from the same file, and the charges it could explain. */
@Composable
private fun ReceiptPreviewSections(preview: ReceiptEntryPreview, currencyCode: String, isUnbalancedAccepted: Boolean, onAcceptedChange: (Boolean) -> Unit) {
    TeaNodeSectionHeader(stringResource(R.string.finance_entry_effect_header))
    if (preview.isBalanced) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Outlined.Verified, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(stringResource(R.string.finance_tax_lines_add_up), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    } else {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Outlined.Warning, null, tint = Color(0xFFEF6C00))
            Text(stringResource(R.string.finance_tax_lines_off_by, FinanceMoney.shown(preview.checkDifferenceAmount, currencyCode)), color = Color(0xFFEF6C00))
        }
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.finance_entry_accept_unbalanced), Modifier.weight(1f)); Switch(isUnbalancedAccepted, onAcceptedChange)
        }
        preview.receiptCheckSummary?.takeIf { summary -> summary.isNotEmpty() }?.let { summary -> Footnote(summary.asReasonSentence()) }
    }
    if (preview.isReplacing == true) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Outlined.Sync, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(stringResource(R.string.finance_entry_replaces), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    TeaNodeSectionHeader(stringResource(R.string.finance_entry_charges_header))
    for (candidate in preview.candidates) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(candidate.financeTransaction?.shownName.orEmpty())
                Text(
                    listOfNotNull(taxDayText(candidate.financeTransaction?.postedOn), if (candidate.isAutomatic == true) stringResource(R.string.finance_entry_matched_when_recorded) else null).filter { text -> text.isNotEmpty() }.joinToString(" · "),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Text(FinanceMoney.shown(candidate.matchedAmount, candidate.financeTransaction?.currencyCode ?: currencyCode))
        }
    }
    if (preview.candidates.isEmpty()) Footnote(stringResource(R.string.finance_entry_no_charge_matches))
}

private fun <Item> List<Item>.replaced(index: Int, item: Item): List<Item> = toMutableList().also { copy -> copy[index] = item }

/** A paystub typed in from the file it was read from. */
@Composable
fun FinancePaystubEntryScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var draft by remember { mutableStateOf(PaystubDraft(currencyCode = "USD", payDate = LocalDate.now().toString())) }
    var isWorking by remember { mutableStateOf(false) }
    EntryShell(
        stringResource(R.string.finance_entry_paystub_title), EntryKind.Paystub, draft.firstProblem, isWorking,
        onRecord = { id ->
            scope.launch {
                isWorking = true
                try { connection.recordPaystub(draft, id); snackbar.show(context.getString(R.string.finance_entry_paystub_recorded)); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                isWorking = false
            }
        },
    ) { _ ->
        Field(stringResource(R.string.finance_entry_employer), draft.employerName, { draft = draft.copy(employerName = it) })
        Field(stringResource(R.string.finance_entry_employer_id), draft.employerTaxId, { draft = draft.copy(employerTaxId = it) }, placeholder = stringResource(R.string.finance_entry_optional))
        Field(stringResource(R.string.finance_tax_employee), draft.employeeName, { draft = draft.copy(employeeName = it) })
        Field(stringResource(R.string.finance_entry_taxpayer_last_four), draft.employeeTaxIdMask, { draft = draft.copy(employeeTaxIdMask = it) }, placeholder = stringResource(R.string.finance_entry_taxpayer_hint))
        Footnote(stringResource(R.string.finance_entry_taxpayer_footer))
        Field(stringResource(R.string.finance_entry_pay_date), draft.payDate, { draft = draft.copy(payDate = it) })
        Field(stringResource(R.string.finance_entry_period_starts), draft.periodStartOn, { draft = draft.copy(periodStartOn = it) })
        Field(stringResource(R.string.finance_entry_period_ends), draft.periodEndOn, { draft = draft.copy(periodEndOn = it) })
        Field(stringResource(R.string.finance_currency), draft.currencyCode, { draft = draft.copy(currencyCode = it.uppercase()) })
        ExchangeRateNote(draft.currencyCode)
        Field(stringResource(R.string.finance_tax_gross), draft.grossAmount, { draft = draft.copy(grossAmount = it) })
        Field(stringResource(R.string.finance_entry_gross_year), draft.yearToDateGrossAmount, { draft = draft.copy(yearToDateGrossAmount = it) })
        Field(stringResource(R.string.finance_tax_total_taxes), draft.totalTaxAmount, { draft = draft.copy(totalTaxAmount = it) })
        Field(stringResource(R.string.finance_tax_total_deductions), draft.totalDeductionAmount, { draft = draft.copy(totalDeductionAmount = it) })
        Field(stringResource(R.string.finance_tax_take_home), draft.netAmount, { draft = draft.copy(netAmount = it) })
        Footnote(stringResource(R.string.finance_entry_as_printed))
        draft.paystubLines.forEachIndexed { index, line ->
            fun change(next: PaystubLineDraft) { draft = draft.copy(paystubLines = draft.paystubLines.replaced(index, next)) }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                TeaNodeSectionHeader(stringResource(R.string.finance_entry_line_header, index + 1))
                KindPicker(stringResource(R.string.finance_kind), line.paystubLineKind, FinanceEntryKinds.paystubLineKinds) { change(line.copy(paystubLineKind = it)) }
                Field(stringResource(R.string.finance_entry_description), line.description, { change(line.copy(description = it)) })
                when (line.paystubLineKind) {
                    "earning" -> Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Field(stringResource(R.string.finance_entry_hours), line.hours, { change(line.copy(hours = it)) }, Modifier.weight(1f))
                        Field(stringResource(R.string.finance_entry_rate), line.rateAmount, { change(line.copy(rateAmount = it)) }, Modifier.weight(1f))
                    }
                    "tax" -> {
                        KindPicker(stringResource(R.string.finance_entry_which_tax), line.paystubTaxKind, FinanceEntryKinds.paystubTaxKinds) { change(line.copy(paystubTaxKind = it)) }
                        Field(stringResource(R.string.finance_entry_jurisdiction), line.taxJurisdictionCode, { change(line.copy(taxJurisdictionCode = it)) })
                        Field(stringResource(R.string.finance_entry_wages_period), line.currentWagesAmount, { change(line.copy(currentWagesAmount = it)) })
                        Field(stringResource(R.string.finance_entry_wages_year), line.yearToDateWagesAmount, { change(line.copy(yearToDateWagesAmount = it)) })
                    }
                    "deduction" -> {
                        KindPicker(stringResource(R.string.finance_entry_pays_for), line.paystubDeductionKind, FinanceEntryKinds.paystubDeductionKinds) { change(line.copy(paystubDeductionKind = it)) }
                        Field(stringResource(R.string.finance_entry_employer_adds_period), line.employerCurrentAmount, { change(line.copy(employerCurrentAmount = it)) })
                        Field(stringResource(R.string.finance_entry_employer_adds_year), line.employerYearToDateAmount, { change(line.copy(employerYearToDateAmount = it)) })
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Field(stringResource(R.string.finance_entry_amount_period), line.currentAmount, { change(line.copy(currentAmount = it)) }, Modifier.weight(1f))
                    Field(stringResource(R.string.finance_entry_amount_year), line.yearToDateAmount, { change(line.copy(yearToDateAmount = it)) }, Modifier.weight(1f))
                }
                if (draft.paystubLines.size > 1) TextButton(onClick = { draft = draft.copy(paystubLines = draft.paystubLines.filterIndexed { each, _ -> each != index }) }) { Text(stringResource(R.string.finance_entry_remove_line)) }
            }
        }
        TextButton(onClick = { draft = draft.copy(paystubLines = draft.paystubLines + PaystubLineDraft()) }) { Text(stringResource(R.string.finance_entry_add_line)) }
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.finance_entry_accept_unbalanced), Modifier.weight(1f)); Switch(draft.isUnbalancedAccepted, { draft = draft.copy(isUnbalancedAccepted = it) })
        }
        Footnote(stringResource(R.string.finance_entry_unbalanced_footer))
    }
}

/** A tax form typed in from the file it was read from. */
@Composable
fun FinanceTaxFormEntryScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var draft by remember { mutableStateOf(TaxFormDraft(taxYear = LocalDate.now().year - 1)) }
    var year by remember { mutableStateOf(draft.taxYear.toString()) }
    var isWorking by remember { mutableStateOf(false) }
    EntryShell(
        stringResource(R.string.finance_entry_tax_form_title), EntryKind.TaxForm, draft.firstProblem, isWorking, isBox = true,
        onRecord = { id ->
            scope.launch {
                isWorking = true
                try { connection.recordTaxForm(draft, id); snackbar.show(context.getString(R.string.finance_entry_tax_form_recorded)); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                isWorking = false
            }
        },
    ) { _ ->
        KindPicker(stringResource(R.string.finance_kind), draft.taxFormKind, FinanceEntryKinds.taxFormKinds) { draft = draft.copy(taxFormKind = it) }
        Field(stringResource(R.string.finance_entry_tax_year), year, { text -> year = text; draft = draft.copy(taxYear = text.trim().toIntOrNull() ?: 0) })
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.finance_entry_corrected_form), Modifier.weight(1f)); Switch(draft.isCorrected, { draft = draft.copy(isCorrected = it) }) }
        Field(stringResource(R.string.finance_tax_payer), draft.payerName, { draft = draft.copy(payerName = it) })
        Field(stringResource(R.string.finance_entry_payer_tax_id), draft.payerTaxId, { draft = draft.copy(payerTaxId = it) })
        Field(stringResource(R.string.finance_tax_recipient), draft.recipientName, { draft = draft.copy(recipientName = it) })
        Field(stringResource(R.string.finance_entry_taxpayer_last_four), draft.recipientTaxIdMask, { draft = draft.copy(recipientTaxIdMask = it) }, placeholder = stringResource(R.string.finance_entry_taxpayer_hint))
        Footnote(stringResource(R.string.finance_entry_taxpayer_footer))
        draft.taxFormEntries.forEachIndexed { index, entry ->
            fun change(next: TaxFormEntryDraft) { draft = draft.copy(taxFormEntries = draft.taxFormEntries.replaced(index, next)) }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                TeaNodeSectionHeader(stringResource(R.string.finance_tax_box, (index + 1).toString()))
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Field(stringResource(R.string.finance_entry_box), entry.boxCode, { change(entry.copy(boxCode = it)) }, Modifier.weight(1f))
                    Field(stringResource(R.string.finance_entry_qualifier), entry.boxQualifier, { change(entry.copy(boxQualifier = it)) }, Modifier.weight(1f))
                }
                Field(stringResource(R.string.finance_amount), entry.amount, { change(entry.copy(amount = it)) })
                Field(stringResource(R.string.finance_entry_text), entry.textValue, { change(entry.copy(textValue = it)) })
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.finance_entry_checked), Modifier.weight(1f)); Switch(entry.isChecked, { change(entry.copy(isChecked = it)) }) }
                taxBoxTitleWords(draft.taxFormKind, entry.boxCode)?.let { boxTitle -> Footnote(boxTitle) }
                if (draft.taxFormEntries.size > 1) TextButton(onClick = { draft = draft.copy(taxFormEntries = draft.taxFormEntries.filterIndexed { each, _ -> each != index }) }) { Text(stringResource(R.string.finance_entry_remove_box)) }
            }
        }
        TextButton(onClick = { draft = draft.copy(taxFormEntries = draft.taxFormEntries + TaxFormEntryDraft()) }) { Text(stringResource(R.string.finance_entry_add_box)) }
        Footnote(stringResource(R.string.finance_entry_only_boxes))
    }
}
