package com.teanode.app.finance

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
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
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.LocalDate

/** Which of the three things is typed in; a receipt is kept as any upload, a paystub and a tax form as a tax document. */
internal enum class EntryKind { Receipt, Paystub, TaxForm }

@Composable
private fun problemSentence(problem: FinanceEntryProblem?): String? {
    if (problem == null) return null
    val reason = when (problem.reason) {
        FinanceEntryProblem.Reason.Missing -> R.string.finance_entry_missing
        FinanceEntryProblem.Reason.NotAmount -> R.string.finance_entry_not_amount
        FinanceEntryProblem.Reason.NotNumber -> R.string.finance_entry_not_number
        FinanceEntryProblem.Reason.NotDay -> R.string.finance_entry_not_day
        FinanceEntryProblem.Reason.NotCurrencyCode -> R.string.finance_entry_not_currency
        FinanceEntryProblem.Reason.NotLastFourDigits -> R.string.finance_entry_not_last_four
        FinanceEntryProblem.Reason.NotYear -> R.string.finance_entry_not_year
        FinanceEntryProblem.Reason.NoLines -> R.string.finance_entry_no_lines
        FinanceEntryProblem.Reason.NoBoxes -> R.string.finance_entry_no_boxes
    }
    val where = problem.lineNumber?.let { number -> " (#$number)" }.orEmpty()
    return "${problem.field}$where: ${stringResource(reason)}"
}

@Composable
private fun Field(label: String, value: String, onChange: (String) -> Unit, modifier: Modifier = Modifier) {
    OutlinedTextField(value, onChange, label = { Text(label) }, singleLine = true, modifier = modifier.fillMaxWidth())
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun KindPicker(label: String, value: String, options: List<String>, onChange: (String) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(isOpen, { isOpen = it }) {
        OutlinedTextField(value, {}, readOnly = true, label = { Text(label) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth())
        ExposedDropdownMenu(isOpen, { isOpen = false }) { for (option in options) DropdownMenuItem(text = { Text(option) }, onClick = { onChange(option); isOpen = false }) }
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
    form: @Composable () -> Unit,
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
                fileName = file.contentType
            } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
            isUploading = false
        }
    }
    TeaNodeScreen(title = title, onBack = { navigator.back() }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(stringResource(R.string.finance_entry_from_file), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            Text(stringResource(if (kind == EntryKind.Receipt) R.string.finance_entry_file_receipt else R.string.finance_entry_file_tax), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = { picker.launch("*/*") }, enabled = !isUploading) { Text(if (attachmentId == null) stringResource(R.string.finance_entry_choose_file) else stringResource(R.string.finance_entry_file_chosen, fileName)) }
            form()
            problemSentence(problem)?.let { sentence -> Text(sentence, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium) }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                extraActions(attachmentId)
                Button(enabled = problem == null && attachmentId != null && !isWorking, onClick = { onRecord(attachmentId!!) }) { Text(stringResource(R.string.finance_entry_record)) }
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
    var isWorking by remember { mutableStateOf(false) }
    EntryShell(
        stringResource(R.string.finance_entry_receipt_title), EntryKind.Receipt, draft.firstProblem, isWorking,
        extraActions = { id ->
            OutlinedButton(
                enabled = draft.isReady && id != null && !isWorking,
                onClick = {
                    scope.launch {
                        isWorking = true
                        try { preview = connection.previewRecordReceipt(draft, id!!) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                        isWorking = false
                    }
                },
            ) { Text(stringResource(R.string.finance_entry_check)) }
        },
        onRecord = { id ->
            scope.launch {
                isWorking = true
                try {
                    connection.recordReceipt(draft, id)
                    snackbar.show(context.getString(R.string.finance_entry_receipt_recorded))
                    navigator.back()
                } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                isWorking = false
            }
        },
    ) {
        Field(stringResource(R.string.finance_merchant), draft.merchantName, { draft = draft.copy(merchantName = it) })
        Field(stringResource(R.string.finance_bought_on), draft.purchasedOn, { draft = draft.copy(purchasedOn = it) })
        Field(stringResource(R.string.finance_currency), draft.currencyCode, { draft = draft.copy(currencyCode = it.uppercase()) })
        Field(stringResource(R.string.finance_receipt_number), draft.merchantReceiptNumber, { draft = draft.copy(merchantReceiptNumber = it) })
        Field(stringResource(R.string.finance_subtotal), draft.subtotalAmount, { draft = draft.copy(subtotalAmount = it) })
        Field(stringResource(R.string.finance_total), draft.totalAmount, { draft = draft.copy(totalAmount = it) })
        Field(stringResource(R.string.finance_entry_card_last_four), draft.paymentAccountMask, { draft = draft.copy(paymentAccountMask = it) })
        TeaNodeSectionHeader(stringResource(R.string.finance_lines))
        draft.receiptLines.forEachIndexed { index, line ->
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
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
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.finance_entry_accept_unbalanced), Modifier.weight(1f)); Switch(draft.isUnbalancedAccepted, { draft = draft.copy(isUnbalancedAccepted = it) })
        }
        preview?.let { read ->
            Text(read.receiptCheckSummary.orEmpty().ifEmpty { stringResource(if (read.isBalanced) R.string.finance_lines_add_up else R.string.finance_entry_not_balanced) }, style = MaterialTheme.typography.bodyMedium)
            for (candidate in read.candidates) Text("${candidate.financeTransaction?.shownName.orEmpty()} · ${FinanceMoney.shown(candidate.financeTransaction?.amount, candidate.financeTransaction?.currencyCode)}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
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
    ) {
        Field(stringResource(R.string.finance_entry_employer), draft.employerName, { draft = draft.copy(employerName = it) })
        Field(stringResource(R.string.finance_tax_employee), draft.employeeName, { draft = draft.copy(employeeName = it) })
        Field(stringResource(R.string.finance_entry_taxpayer_last_four), draft.employeeTaxIdMask, { draft = draft.copy(employeeTaxIdMask = it) })
        Field(stringResource(R.string.finance_tax_pay_date), draft.payDate, { draft = draft.copy(payDate = it) })
        Field(stringResource(R.string.finance_entry_period_starts), draft.periodStartOn, { draft = draft.copy(periodStartOn = it) })
        Field(stringResource(R.string.finance_entry_period_ends), draft.periodEndOn, { draft = draft.copy(periodEndOn = it) })
        Field(stringResource(R.string.finance_currency), draft.currencyCode, { draft = draft.copy(currencyCode = it.uppercase()) })
        Field(stringResource(R.string.finance_tax_gross), draft.grossAmount, { draft = draft.copy(grossAmount = it) })
        Field(stringResource(R.string.finance_entry_gross_year), draft.yearToDateGrossAmount, { draft = draft.copy(yearToDateGrossAmount = it) })
        Field(stringResource(R.string.finance_tax_total_taxes), draft.totalTaxAmount, { draft = draft.copy(totalTaxAmount = it) })
        Field(stringResource(R.string.finance_tax_total_deductions), draft.totalDeductionAmount, { draft = draft.copy(totalDeductionAmount = it) })
        Field(stringResource(R.string.finance_tax_take_home), draft.netAmount, { draft = draft.copy(netAmount = it) })
        TeaNodeSectionHeader(stringResource(R.string.finance_lines))
        draft.paystubLines.forEachIndexed { index, line ->
            fun change(next: PaystubLineDraft) { draft = draft.copy(paystubLines = draft.paystubLines.replaced(index, next)) }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
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
        stringResource(R.string.finance_entry_tax_form_title), EntryKind.TaxForm, draft.firstProblem, isWorking,
        onRecord = { id ->
            scope.launch {
                isWorking = true
                try { connection.recordTaxForm(draft, id); snackbar.show(context.getString(R.string.finance_entry_tax_form_recorded)); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                isWorking = false
            }
        },
    ) {
        KindPicker(stringResource(R.string.finance_kind), draft.taxFormKind, FinanceEntryKinds.taxFormKinds) { draft = draft.copy(taxFormKind = it) }
        Field(stringResource(R.string.finance_entry_tax_year), year, { text -> year = text; draft = draft.copy(taxYear = text.trim().toIntOrNull() ?: 0) })
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.finance_tax_corrected_form), Modifier.weight(1f)); Switch(draft.isCorrected, { draft = draft.copy(isCorrected = it) }) }
        Field(stringResource(R.string.finance_tax_payer), draft.payerName, { draft = draft.copy(payerName = it) })
        Field(stringResource(R.string.finance_entry_payer_tax_id), draft.payerTaxId, { draft = draft.copy(payerTaxId = it) })
        Field(stringResource(R.string.finance_tax_recipient), draft.recipientName, { draft = draft.copy(recipientName = it) })
        Field(stringResource(R.string.finance_entry_taxpayer_last_four), draft.recipientTaxIdMask, { draft = draft.copy(recipientTaxIdMask = it) })
        TeaNodeSectionHeader(stringResource(R.string.finance_tax_boxes))
        draft.taxFormEntries.forEachIndexed { index, entry ->
            fun change(next: TaxFormEntryDraft) { draft = draft.copy(taxFormEntries = draft.taxFormEntries.replaced(index, next)) }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    Field(stringResource(R.string.finance_entry_box), entry.boxCode, { change(entry.copy(boxCode = it)) }, Modifier.weight(1f))
                    Field(stringResource(R.string.finance_entry_qualifier), entry.boxQualifier, { change(entry.copy(boxQualifier = it)) }, Modifier.weight(1f))
                }
                Field(stringResource(R.string.finance_amount), entry.amount, { change(entry.copy(amount = it)) })
                Field(stringResource(R.string.finance_entry_text), entry.textValue, { change(entry.copy(textValue = it)) })
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.finance_entry_marked), Modifier.weight(1f)); Switch(entry.isChecked, { change(entry.copy(isChecked = it)) }) }
                if (draft.taxFormEntries.size > 1) TextButton(onClick = { draft = draft.copy(taxFormEntries = draft.taxFormEntries.filterIndexed { each, _ -> each != index }) }) { Text(stringResource(R.string.finance_entry_remove_box)) }
            }
        }
        TextButton(onClick = { draft = draft.copy(taxFormEntries = draft.taxFormEntries + TaxFormEntryDraft()) }) { Text(stringResource(R.string.finance_entry_add_box)) }
    }
}
