package com.teanode.app.finance

import androidx.compose.foundation.layout.Arrangement
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.mutableLongStateOf
import com.teanode.app.shopping.ShoppingPendingFile
import com.teanode.app.shopping.toPendingFile
import com.teanode.kit.finance.TaxDocumentUpload
import com.teanode.kit.finance.TaxDocumentUploadPhase
import com.teanode.kit.finance.TaxDocumentUploads
import kotlinx.coroutines.delay
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shopping.readPendingFile
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinancePaystub
import com.teanode.kit.finance.FinanceTaxFormView
import com.teanode.kit.finance.TaxDocumentAddress
import com.teanode.kit.finance.TaxFilingStatus
import com.teanode.kit.finance.TaxYearFigureKind
import com.teanode.kit.finance.TaxYearSettingChange
import com.teanode.kit.finance.TaxYearSummary
import com.teanode.kit.finance.FinanceTaxBoxTitles
import com.teanode.kit.finance.boxName
import com.teanode.kit.finance.comparisons
import com.teanode.kit.finance.deletePaystub
import com.teanode.kit.finance.deleteTaxForm
import com.teanode.kit.finance.disagreeingCount
import com.teanode.kit.finance.employments
import com.teanode.kit.finance.entries
import com.teanode.kit.finance.filingStatus
import com.teanode.kit.finance.financePaystub
import com.teanode.kit.finance.financePaystubs
import com.teanode.kit.finance.financeTaxForm
import com.teanode.kit.finance.financeTaxForms
import com.teanode.kit.finance.findings
import com.teanode.kit.finance.id
import com.teanode.kit.finance.isBalanced
import com.teanode.kit.finance.isFromW2
import com.teanode.kit.finance.kindName
import com.teanode.kit.finance.lines
import com.teanode.kit.finance.regenerateTaxDocumentAddress
import com.teanode.kit.finance.readTaxDocumentOfAttachment
import com.teanode.kit.finance.rows
import com.teanode.kit.finance.setTaxFilingStatus
import com.teanode.kit.finance.setTaxYearSetting
import com.teanode.kit.finance.shareOfPay
import com.teanode.kit.finance.slices
import com.teanode.kit.finance.taxDocumentAddress
import com.teanode.kit.finance.taxYear
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.LocalDate

private class TaxesRead(val summary: TaxYearSummary?, val paystubs: List<FinancePaystub>, val forms: List<FinanceTaxFormView>, val address: TaxDocumentAddress?)

@Composable
private fun figureWords(kind: TaxYearFigureKind): String = when (kind) {
    TaxYearFigureKind.Wages -> stringResource(R.string.finance_tax_wages)
    TaxYearFigureKind.FederalIncomeTax -> stringResource(R.string.finance_tax_federal_income_tax)
    TaxYearFigureKind.SocialSecurityWages -> stringResource(R.string.finance_tax_social_security_wages)
    TaxYearFigureKind.SocialSecurityTax -> stringResource(R.string.finance_tax_social_security_tax)
    TaxYearFigureKind.MedicareWages -> stringResource(R.string.finance_tax_medicare_wages)
    TaxYearFigureKind.MedicareTax -> stringResource(R.string.finance_tax_medicare_tax)
    TaxYearFigureKind.RetirementTraditional -> stringResource(R.string.finance_tax_retirement_traditional)
    TaxYearFigureKind.RetirementRoth -> stringResource(R.string.finance_tax_retirement_roth)
    TaxYearFigureKind.Hsa -> stringResource(R.string.finance_tax_hsa)
    TaxYearFigureKind.DependentCare -> stringResource(R.string.finance_tax_dependent_care)
    is TaxYearFigureKind.StateWages -> stringResource(R.string.finance_tax_state_wages, kind.stateCode)
    is TaxYearFigureKind.StateIncomeTax -> stringResource(R.string.finance_tax_state_income_tax, kind.stateCode)
    TaxYearFigureKind.LocalIncomeTax -> stringResource(R.string.finance_tax_local_income_tax)
}

/** A tax year put together from paystubs and W-2s: what was earned and withheld, the estimate, what disagrees, and the documents behind it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FinanceTaxesScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var year by remember { mutableStateOf(LocalDate.now().year) }
    var filingStatus by remember { mutableStateOf<TaxFilingStatus?>(null) }
    var isStatusOpen by remember { mutableStateOf(false) }
    var isEditingSettings by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, year, filingStatus) {
        TaxesRead(connection.taxYear(year, filingStatus), connection.financePaystubs(year).financePaystubs.filterNotNull(), connection.financeTaxForms(year).taxForms.filterNotNull(), connection.taxDocumentAddress())
    }
    var uploads by remember { mutableStateOf<List<TaxDocumentUpload>>(emptyList()) }
    var nextUploadId by remember { mutableLongStateOf(1L) }
    var isAddMenuOpen by remember { mutableStateOf(false) }

    fun changeUpload(uploadId: Long, change: (TaxDocumentUpload) -> TaxDocumentUpload) {
        uploads = uploads.map { each -> if (each.uploadId == uploadId) change(each) else each }
    }

    /** Uploads each file as a tax document and has the agent read it, then waits for them: every four seconds, two minutes at most, as a receipt is waited for. */
    fun addFiles(pick: suspend () -> List<ShoppingPendingFile>) {
        scope.launch {
            val files = try { pick() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure); return@launch }
            if (files.isEmpty()) return@launch
            val started = files.map { file -> TaxDocumentUpload(nextUploadId++, file.name) }
            uploads = uploads + started
            val readingIds = mutableSetOf<String>()
            for ((file, upload) in files.zip(started)) {
                try {
                    val attachmentId = connection.upload(file.name, file.contentType, file.data, isTaxDocument = true)
                    connection.readTaxDocumentOfAttachment(attachmentId)
                    changeUpload(upload.uploadId) { each -> each.copy(agentAttachmentId = attachmentId, uploadPhase = TaxDocumentUploadPhase.Reading) }
                    readingIds += attachmentId
                } catch (failure: CancellationException) {
                    throw failure
                } catch (failure: Throwable) {
                    changeUpload(upload.uploadId) { each -> each.copy(uploadPhase = TaxDocumentUploadPhase.Failed, failureText = failure.message) }
                }
            }
            if (readingIds.isEmpty()) return@launch
            snackbar.show(context.resources.getQuantityString(R.plurals.finance_tax_reading_documents, readingIds.size, readingIds.size))
            var waitingIds: Set<String> = readingIds
            repeat(TaxDocumentUploads.WAIT_PASS_COUNT) {
                delay(TaxDocumentUploads.WAIT_PASS_MS)
                try {
                    val readIds = TaxDocumentUploads.readAttachmentIds(connection.financePaystubs(year).financePaystubs.filterNotNull(), connection.financeTaxForms(year).taxForms.filterNotNull())
                    uploads = TaxDocumentUploads.withoutRead(uploads, readIds)
                    waitingIds = waitingIds - readIds
                    read.refresh()
                } catch (failure: CancellationException) {
                    throw failure
                } catch (_: Throwable) {
                    // The next pass asks again.
                }
                if (waitingIds.isEmpty()) return@launch
            }
            uploads = TaxDocumentUploads.stillReading(uploads, waitingIds)
        }
    }
    val photoPicker = androidx.activity.compose.rememberLauncherForActivityResult(androidx.activity.result.contract.ActivityResultContracts.PickMultipleVisualMedia(10)) { uris ->
        if (uris.isNotEmpty()) addFiles { uris.mapIndexed { index, uri -> context.readPendingFile(uri, "tax-document-${index + 1}.jpg") } }
    }
    val cameraPicker = androidx.activity.compose.rememberLauncherForActivityResult(androidx.activity.result.contract.ActivityResultContracts.TakePicturePreview()) { bitmap ->
        if (bitmap != null) addFiles { listOf(bitmap.toPendingFile("tax-document.jpg")) }
    }
    val filePicker = androidx.activity.compose.rememberLauncherForActivityResult(androidx.activity.result.contract.ActivityResultContracts.OpenMultipleDocuments()) { uris ->
        if (uris.isNotEmpty()) addFiles { uris.mapIndexed { index, uri -> context.readPendingFile(uri, "tax-document-${index + 1}") } }
    }
    fun act(saying: String, work: suspend () -> Unit) {
        scope.launch { try { work(); snackbar.show(saying) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() }
    }
    TeaNodeScreen(
        title = stringResource(R.string.finance_part_taxes), onBack = { navigator.back() },
        actions = {
            Box {
                IconButton(onClick = { isAddMenuOpen = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.finance_tax_add_documents)) }
                DropdownMenu(isAddMenuOpen, { isAddMenuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.finance_tax_add_photo_library)) }, leadingIcon = { Icon(Icons.Outlined.PhotoLibrary, null) },
                        onClick = { isAddMenuOpen = false; photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) },
                    )
                    DropdownMenuItem(text = { Text(stringResource(R.string.finance_tax_add_camera)) }, leadingIcon = { Icon(Icons.Outlined.PhotoCamera, null) }, onClick = { isAddMenuOpen = false; cameraPicker.launch(null) })
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.finance_tax_add_files)) }, leadingIcon = { Icon(Icons.Outlined.Folder, null) },
                        onClick = { isAddMenuOpen = false; filePicker.launch(arrayOf("application/pdf", "image/*")) },
                    )
                }
            }
        },
    ) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val summary = loaded.summary
            val currency = "USD"
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("year") { YearStepper(year, LocalDate.now().year - 10, LocalDate.now().year) { year = it } }
                if (uploads.isNotEmpty()) {
                    item("adding-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_adding)) }
                    items(uploads, key = { upload -> "adding-" + upload.uploadId }) { upload ->
                        TaxDocumentUploadRow(upload, onHide = { uploads = uploads.filter { each -> each.uploadId != upload.uploadId } })
                    }
                }
                summary?.taxYearEstimate?.let { estimate ->
                    item("estimate-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_estimate)) }
                    item("estimate-total") { LabeledValue(stringResource(R.string.finance_tax_total_tax), FinanceMoney.shown(estimate.totalTaxAmount, currency), supporting = estimate.shareOfPay(estimate.totalTaxAmount)?.let { share -> stringResource(R.string.finance_tax_of_income, FinanceMoney.percent(share)) }) }
                    item("ring") {
                        val colors = listOf(FinancePalette.color(0), FinancePalette.color(1), MaterialTheme.colorScheme.error, MaterialTheme.colorScheme.surfaceVariant)
                        Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                            DonutChart(estimate.slices.map { slice -> DonutSlice(slice.sliceKind.name, slice.amountValue, colors[slice.sliceKind.ordinal]) }, null) {
                                Text(FinanceMoney.shown(estimate.totalTaxAmount, currency), style = MaterialTheme.typography.titleMedium)
                            }
                        }
                    }
                    for (slice in estimate.slices) {
                        item("slice-" + slice.sliceKind.name) {
                            val label = when (slice.sliceKind) {
                                com.teanode.kit.finance.TaxEstimateSliceKind.Paid -> R.string.finance_tax_paid
                                com.teanode.kit.finance.TaxEstimateSliceKind.UpcomingWithheld -> R.string.finance_tax_upcoming
                                com.teanode.kit.finance.TaxEstimateSliceKind.Owing -> R.string.finance_tax_owing
                                com.teanode.kit.finance.TaxEstimateSliceKind.RemainingIncome -> R.string.finance_tax_remaining_income
                            }
                            LabeledValue(stringResource(label), FinanceMoney.shown(slice.amount, currency))
                        }
                    }
                    if ((FinanceMoney.decimal(estimate.refundAmount)?.signum() ?: 0) > 0) item("refund") { LabeledValue(stringResource(R.string.finance_tax_refund), FinanceMoney.shown(estimate.refundAmount, currency)) }
                    item("status") {
                        Box(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
                            ExposedDropdownMenuBox(isStatusOpen, { isStatusOpen = it }) {
                                OutlinedTextField(
                                    stringResource(statusWords(estimate.filingStatus)) + if (estimate.isTaxFilingStatusAssumed == true) " · " + stringResource(R.string.finance_tax_assumed) else "", {}, readOnly = true,
                                    label = { Text(stringResource(R.string.finance_tax_filing_status)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isStatusOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
                                )
                                ExposedDropdownMenu(isStatusOpen, { isStatusOpen = false }) {
                                    for (each in TaxFilingStatus.entries) DropdownMenuItem(text = { Text(stringResource(statusWords(each))) }, onClick = { isStatusOpen = false; filingStatus = each; act(context.getString(R.string.finance_saved)) { connection.setTaxFilingStatus(each) } })
                                }
                            }
                        }
                    }
                    item("settings") { TextButton(onClick = { isEditingSettings = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_tax_year_settings, year)) } }
                }
                summary?.taxYearInvestmentPreview?.let { preview -> taxInvestmentPreview(preview, currency) }
                summary?.figures?.let { figures ->
                    item("figures-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_this_year)) }
                    for (row in figures.rows(summary.projectedFigures)) {
                        item("figure-" + row.figureKind.toString()) { LabeledValue(figureWords(row.figureKind), FinanceMoney.shown(row.amount, currency), supporting = row.projectedAmount?.let { projected -> stringResource(R.string.finance_tax_projected, FinanceMoney.shown(projected, currency)) }) }
                    }
                }
                for (employment in summary?.employments.orEmpty()) {
                    item("employment-" + employment.employerName + employment.employeeName) {
                        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                            Text(employment.employerName.orEmpty(), style = MaterialTheme.typography.titleSmall)
                            Text(listOfNotNull(employment.employeeName, stringResource(if (employment.isFromW2) R.string.finance_tax_from_w2 else R.string.finance_tax_from_paystubs)).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    for (comparison in employment.comparisons.filter { each -> each.isMatching == false }) {
                        item("comparison-" + employment.employerName + comparison.boxName) {
                            LabeledValue(
                                listOfNotNull(stringResource(R.string.finance_tax_box, comparison.boxName), taxBoxWords(FinanceTaxBoxTitles.boxKey("w2", comparison.boxCode))).joinToString(" · "), FinanceMoney.shown(comparison.w2Amount, currency),
                                supporting = stringResource(R.string.finance_tax_paystubs_say, FinanceMoney.shown(comparison.paystubAmount, currency)),
                            )
                        }
                    }
                }
                summary?.taxYearInvestment?.let { investment -> taxInvestment(investment, currency) }
                val findings = summary?.findings.orEmpty()
                if (findings.isNotEmpty()) {
                    item("findings-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_findings)) }
                    for (finding in findings) item("finding-" + finding.findingText) { Footnote(finding.findingText.orEmpty()) }
                }
                item("paystubs-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_paystubs)) }
                if (loaded.paystubs.isEmpty()) item("paystubs-none") { Footnote(stringResource(R.string.finance_tax_no_paystubs)) }
                for (paystub in loaded.paystubs) item("paystub-" + paystub.id) { TeaNodeRow(paystub.employerName.orEmpty(), supporting = paystub.payDate, trailing = { Text(FinanceMoney.shown(paystub.netAmount, paystub.currencyCode)) }, onClick = { navigator.navigate(FinanceRoutes.paystub(paystub.id)) }) }
                item("forms-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_forms)) }
                if (loaded.forms.isEmpty()) item("forms-none") { Footnote(stringResource(R.string.finance_tax_no_forms)) }
                for (form in loaded.forms) {
                    val taxForm = form.financeTaxForm ?: continue
                    item("form-" + form.id) { TeaNodeRow(taxForm.payerName.orEmpty(), supporting = "${taxForm.kindName} · ${taxForm.taxYear ?: ""}", trailing = { if (form.disagreeingCount > 0) Text(stringResource(R.string.finance_tax_disagrees, form.disagreeingCount), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.labelMedium) }, onClick = { navigator.navigate(FinanceRoutes.taxForm(form.id)) }) }
                }
                item("upload-footer") { Footnote(stringResource(R.string.finance_tax_upload_footer)) }
                item("entry") {
                    Row(Modifier.padding(horizontal = 8.dp)) {
                        TextButton(onClick = { navigator.navigate(FinanceRoutes.paystubEntry) }) { Text(stringResource(R.string.finance_entry_paystub_title)) }
                        TextButton(onClick = { navigator.navigate(FinanceRoutes.taxFormEntry) }) { Text(stringResource(R.string.finance_entry_tax_form_title)) }
                    }
                }
                item("address-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_documents_address)) }
                item("address") {
                    Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        val address = loaded.address?.taxDocumentAddress.orEmpty()
                        Text(address.ifEmpty { stringResource(R.string.finance_no_mailbox_address) }, style = MaterialTheme.typography.bodyLarge)
                        Text(stringResource(R.string.finance_tax_address_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (address.isNotEmpty()) OutlinedButton(onClick = { act(context.getString(R.string.finance_new_address_made)) { connection.regenerateTaxDocumentAddress() } }) { Text(stringResource(R.string.finance_new_address)) }
                    }
                }
            }
            if (isEditingSettings) TaxSettingsDialog(year, summary?.taxYearSetting, onDismiss = { isEditingSettings = false }) { change -> isEditingSettings = false; act(context.getString(R.string.finance_saved)) { connection.setTaxYearSetting(year, change) } }
        }
    }
}

/** A file being added: its name and how far it is. A finished one can be hidden. */
@Composable
internal fun TaxDocumentUploadRow(upload: TaxDocumentUpload, onHide: () -> Unit) {
    TeaNodeRow(
        headline = upload.fileName,
        supporting = when (upload.uploadPhase) {
            TaxDocumentUploadPhase.Uploading -> stringResource(R.string.finance_tax_uploading)
            TaxDocumentUploadPhase.Reading -> stringResource(R.string.finance_tax_reading)
            TaxDocumentUploadPhase.StillReading -> stringResource(R.string.finance_tax_still_reading)
            TaxDocumentUploadPhase.Failed -> upload.failureText.orEmpty()
        },
        supportingMaxLines = 3,
        leading = {
            when (upload.uploadPhase) {
                TaxDocumentUploadPhase.Uploading, TaxDocumentUploadPhase.Reading -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
                TaxDocumentUploadPhase.StillReading -> Icon(Icons.Outlined.Schedule, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                TaxDocumentUploadPhase.Failed -> Icon(Icons.Outlined.Warning, null, tint = MaterialTheme.colorScheme.error)
            }
        },
        trailing = { if (upload.isFinished) TextButton(onClick = onHide) { Text(stringResource(R.string.finance_tax_hide)) } },
    )
}

/** The year's interest and dividends as the linked accounts were paid them, until the 1099s come. */
private fun androidx.compose.foundation.lazy.LazyListScope.taxInvestmentPreview(preview: TaxYearSummary.InvestmentPreview, currency: String) {
    item("preview-header") { TeaNodeSectionHeader(stringResource(R.string.finance_investment_preview_title)) }
    for ((index, income) in preview.taxYearAccountIncomes.orEmpty().filterNotNull().withIndex()) {
        item("preview-income-$index") {
            LabeledValue(
                income.accountName.orEmpty(), FinanceMoney.shown(income.amount, currency),
                supporting = listOfNotNull(
                    stringResource(if (income.incomeKind == "dividend") R.string.finance_dividends else R.string.finance_interest),
                    if (income.isTaxAdvantaged == true) stringResource(R.string.finance_not_taxed) else null,
                ).joinToString(" · "),
            )
        }
    }
    item("preview-footer") {
        Footnote(
            stringResource(
                R.string.finance_investment_preview_footer, FinanceMoney.shown(preview.interestAmount, currency), FinanceMoney.shown(preview.projectedInterestAmount, currency),
                FinanceMoney.shown(preview.dividendsAmount, currency), FinanceMoney.shown(preview.projectedDividendsAmount, currency),
            ),
        )
    }
}

/** The year's investment income from its 1099s: interest and dividends always, every other figure when it is not nothing. */
private fun androidx.compose.foundation.lazy.LazyListScope.taxInvestment(investment: TaxYearSummary.Investment, currency: String) {
    val lines = listOf(
        Triple(R.string.finance_interest, investment.interestAmount, true),
        Triple(R.string.finance_tax_exempt_interest, investment.taxExemptInterestAmount, false),
        Triple(R.string.finance_ordinary_dividends, investment.ordinaryDividendsAmount, true),
        Triple(R.string.finance_qualified_dividends, investment.qualifiedDividendsAmount, false),
        Triple(R.string.finance_capital_gain_distributions, investment.capitalGainDistributionsAmount, false),
        Triple(R.string.finance_short_term_gain_or_loss, investment.shortTermGainAmount, false),
        Triple(R.string.finance_long_term_gain_or_loss, investment.longTermGainAmount, false),
        Triple(R.string.finance_federal_income_tax_withheld, investment.federalIncomeTaxWithheldAmount, false),
        Triple(R.string.finance_foreign_tax_paid, investment.foreignTaxPaidAmount, false),
    ).filter { (_, amount, isAlways) -> isAlways || (FinanceMoney.decimal(amount)?.signum() ?: 0) != 0 }
    item("investment-header") { TeaNodeSectionHeader(stringResource(R.string.finance_investment_income)) }
    for ((label, amount, _) in lines) item("investment-$label") { LabeledValue(stringResource(label), FinanceMoney.shown(amount, currency)) }
    item("investment-footer") { Footnote(stringResource(R.string.finance_investment_footer)) }
}

/** The button that opens the file a paystub or a tax form was read from. */
@Composable
internal fun OpenOriginalButton(attachmentId: String) {
    val connection = LocalConnection.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isOpening by remember { mutableStateOf(false) }
    Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        OutlinedButton(
            enabled = !isOpening,
            onClick = {
                scope.launch {
                    isOpening = true
                    try { com.teanode.app.chats.openServerFile(context, connection, attachmentId) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                    isOpening = false
                }
            },
        ) { Text(stringResource(R.string.finance_open_original)) }
        if (isOpening) CircularProgressIndicator(Modifier.padding(start = 12.dp).size(20.dp), strokeWidth = 2.dp)
    }
}

private fun statusWords(status: TaxFilingStatus?): Int = when (status) {
    TaxFilingStatus.Single -> R.string.finance_tax_status_single
    TaxFilingStatus.MarriedFilingJointly -> R.string.finance_tax_status_joint
    TaxFilingStatus.MarriedFilingSeparately -> R.string.finance_tax_status_separate
    TaxFilingStatus.HeadOfHousehold -> R.string.finance_tax_status_head
    null -> R.string.finance_tax_status_not_chosen
}

@Composable
private fun TaxSettingsDialog(year: Int, existing: com.teanode.kit.finance.TaxYearSetting?, onDismiss: () -> Unit, onSave: (TaxYearSettingChange) -> Unit) {
    var children by remember { mutableStateOf(existing?.childCount?.toString().orEmpty()) }
    var otherDependents by remember { mutableStateOf(existing?.otherDependentCount?.toString().orEmpty()) }
    var dependentCare by remember { mutableStateOf(existing?.dependentCareExpenseAmount.orEmpty()) }
    var otherIncome by remember { mutableStateOf(existing?.otherIncomeAmount.orEmpty()) }
    var charity by remember { mutableStateOf(existing?.charitableContributionAmount.orEmpty()) }
    var propertyTax by remember { mutableStateOf(existing?.propertyTaxAmount.orEmpty()) }
    var stateTax by remember { mutableStateOf(existing?.stateIncomeTaxPaidAmount.orEmpty()) }
    var shortGain by remember { mutableStateOf(existing?.estimatedShortTermGainAmount.orEmpty()) }
    var longGain by remember { mutableStateOf(existing?.estimatedLongTermGainAmount.orEmpty()) }
    var gainNote by remember { mutableStateOf(existing?.estimatedGainNote.orEmpty()) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_tax_year_settings, year)) },
        text = {
            Column(Modifier.verticalScroll(androidx.compose.foundation.rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(children, { children = it }, label = { Text(stringResource(R.string.finance_tax_children)) }, singleLine = true)
                OutlinedTextField(otherDependents, { otherDependents = it }, label = { Text(stringResource(R.string.finance_tax_other_dependents)) }, singleLine = true)
                OutlinedTextField(dependentCare, { dependentCare = it }, label = { Text(stringResource(R.string.finance_tax_dependent_care_expense)) }, singleLine = true)
                OutlinedTextField(otherIncome, { otherIncome = it }, label = { Text(stringResource(R.string.finance_tax_other_income)) }, singleLine = true)
                OutlinedTextField(charity, { charity = it }, label = { Text(stringResource(R.string.finance_tax_charity)) }, singleLine = true)
                OutlinedTextField(propertyTax, { propertyTax = it }, label = { Text(stringResource(R.string.finance_tax_property_tax)) }, singleLine = true)
                OutlinedTextField(stateTax, { stateTax = it }, label = { Text(stringResource(R.string.finance_tax_state_tax_paid)) }, singleLine = true)
                OutlinedTextField(shortGain, { shortGain = it }, label = { Text(stringResource(R.string.finance_tax_short_gain)) }, singleLine = true)
                OutlinedTextField(longGain, { longGain = it }, label = { Text(stringResource(R.string.finance_tax_long_gain)) }, singleLine = true)
                OutlinedTextField(gainNote, { gainNote = it }, label = { Text(stringResource(R.string.finance_tax_gain_note)) })
                Text(stringResource(R.string.finance_tax_settings_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = {
            TextButton(onClick = {
                fun changed(text: String, before: String?): String? = text.trim().takeIf { value -> value != before.orEmpty() }
                onSave(
                    TaxYearSettingChange(
                        childCount = children.trim().toIntOrNull()?.takeIf { value -> value != existing?.childCount },
                        otherDependentCount = otherDependents.trim().toIntOrNull()?.takeIf { value -> value != existing?.otherDependentCount },
                        dependentCareExpenseAmount = changed(dependentCare, existing?.dependentCareExpenseAmount), otherIncomeAmount = changed(otherIncome, existing?.otherIncomeAmount),
                        charitableContributionAmount = changed(charity, existing?.charitableContributionAmount), propertyTaxAmount = changed(propertyTax, existing?.propertyTaxAmount),
                        stateIncomeTaxPaidAmount = changed(stateTax, existing?.stateIncomeTaxPaidAmount), estimatedShortTermGainAmount = changed(shortGain, existing?.estimatedShortTermGainAmount),
                        estimatedLongTermGainAmount = changed(longGain, existing?.estimatedLongTermGainAmount), estimatedGainNote = changed(gainNote, existing?.estimatedGainNote),
                    ),
                )
            }) { Text(stringResource(R.string.finance_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

/** One paystub: who paid whom, the totals, and its lines by kind. */
@Composable
fun FinancePaystubDetailScreen(paystubId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isConfirmingDelete by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, paystubId) { connection.financePaystub(paystubId) }
    TeaNodeScreen(title = read.value?.employerName.orEmpty().ifEmpty { stringResource(R.string.finance_tax_paystub) }, onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { paystub ->
            if (paystub == null) return@LoadedContent
            val currency = paystub.currencyCode
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("employee") { LabeledValue(stringResource(R.string.finance_tax_employee), paystub.employeeName.orEmpty()) }
                item("pay-date") { LabeledValue(stringResource(R.string.finance_tax_pay_date), paystub.payDate.orEmpty()) }
                item("gross") { LabeledValue(stringResource(R.string.finance_tax_gross), FinanceMoney.shown(paystub.grossAmount, currency)) }
                item("taxes") { LabeledValue(stringResource(R.string.finance_tax_total_taxes), FinanceMoney.shown(paystub.totalTaxAmount, currency)) }
                item("deductions") { LabeledValue(stringResource(R.string.finance_tax_total_deductions), FinanceMoney.shown(paystub.totalDeductionAmount, currency)) }
                item("net") { LabeledValue(stringResource(R.string.finance_tax_take_home), FinanceMoney.shown(paystub.netAmount, currency)) }
                item("check") { Footnote(if (paystub.isBalanced) stringResource(R.string.finance_lines_add_up) else stringResource(R.string.finance_lines_off_by, FinanceMoney.shown(paystub.checkDifferenceAmount, currency))) }
                for (kind in listOf("earning", "tax", "deduction", "reimbursement")) {
                    val lines = paystub.lines(kind)
                    if (lines.isEmpty()) continue
                    item("kind-$kind") { TeaNodeSectionHeader(kind.replaceFirstChar { letter -> letter.uppercase() }) }
                    for (line in lines) item("line-" + line.id) { TeaNodeRow(line.description.orEmpty().ifEmpty { kind }, supporting = line.yearToDateAmount?.let { year -> stringResource(R.string.finance_tax_year_to_date, FinanceMoney.shown(year, currency)) }, trailing = { Text(FinanceMoney.shown(line.currentAmount, currency)) }) }
                }
                paystub.agentAttachmentId?.takeIf { attachmentId -> attachmentId.isNotEmpty() }?.let { attachmentId -> item("original") { OpenOriginalButton(attachmentId) } }
                item("delete") { TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_tax_delete_paystub), color = MaterialTheme.colorScheme.error) } }
            }
            if (isConfirmingDelete) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_tax_delete_paystub), confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
                    onConfirm = { isConfirmingDelete = false; scope.launch { try { connection.deletePaystub(paystubId); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
                    onDismiss = { isConfirmingDelete = false },
                )
            }
        }
    }
}

/** One tax form: its boxes, and what its own checks found. */
@Composable
fun FinanceTaxFormDetailScreen(taxFormId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isConfirmingDelete by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, taxFormId) { connection.financeTaxForm(taxFormId) }
    TeaNodeScreen(title = read.value?.financeTaxForm?.let { form -> form.payerName?.ifEmpty { null } ?: form.kindName }.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { view ->
            val form = view?.financeTaxForm ?: return@LoadedContent
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("kind") { LabeledValue(stringResource(R.string.finance_kind), "${form.kindName} · ${form.taxYear ?: ""}") }
                item("payer") { LabeledValue(stringResource(R.string.finance_tax_payer), form.payerName.orEmpty()) }
                item("recipient") { LabeledValue(stringResource(R.string.finance_tax_recipient), form.recipientName.orEmpty()) }
                if (form.isCorrected == true) item("corrected") { Footnote(stringResource(R.string.finance_tax_corrected)) }
                item("boxes-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_boxes)) }
                for (entry in form.entries) {
                    item("entry-" + entry.id) {
                        val termKey = FinanceTaxBoxTitles.termKey(form.taxFormKind, entry.boxQualifier)
                        // A 1099-B's boxes are told apart by their total, said in words under the box rather than as its qualifier.
                        TeaNodeRow(stringResource(R.string.finance_tax_box, if (termKey == null) entry.boxName else entry.boxCode.orEmpty()), supporting = listOfNotNull(taxBoxWords(FinanceTaxBoxTitles.boxKey(form.taxFormKind, entry.boxCode)), taxBoxWords(termKey), entry.textValue?.takeIf { text -> text.isNotEmpty() }).joinToString("\n").ifEmpty { null }, trailing = { if (!entry.amount.isNullOrEmpty()) Text(FinanceMoney.shown(entry.amount, "USD")) else if (entry.isChecked == true) Text("✓") })
                    }
                }
                val findings = view.findings
                if (findings.isNotEmpty()) {
                    item("findings-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_checks)) }
                    for (finding in findings) item("finding-" + finding.taxFormFindingCode) { TeaNodeRow(finding.findingText.orEmpty().ifEmpty { finding.taxFormFindingCode.orEmpty() }, supporting = stringResource(if (finding.isAgreeing == false) R.string.finance_tax_disagrees_short else R.string.finance_tax_agrees)) }
                }
                form.agentAttachmentId?.takeIf { attachmentId -> attachmentId.isNotEmpty() }?.let { attachmentId -> item("original") { OpenOriginalButton(attachmentId) } }
                item("delete") { TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_tax_delete_form), color = MaterialTheme.colorScheme.error) } }
            }
            if (isConfirmingDelete) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_tax_delete_form), confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
                    onConfirm = { isConfirmingDelete = false; scope.launch { try { connection.deleteTaxForm(taxFormId); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
                    onDismiss = { isConfirmingDelete = false },
                )
            }
        }
    }
}

/** The words for a box of a tax form from its key, or null for a box with none. */
@Composable
private fun taxBoxWords(key: String?): String? {
    if (key == null) return null
    val context = androidx.compose.ui.platform.LocalContext.current
    val identifier = context.resources.getIdentifier("finance_tax_box_$key", "string", context.packageName)
    return if (identifier == 0) null else context.getString(identifier)
}
