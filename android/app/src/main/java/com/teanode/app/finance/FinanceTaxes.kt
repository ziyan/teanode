package com.teanode.app.finance

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CheckBox
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.Verified
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shopping.ShoppingPendingFile
import com.teanode.app.shopping.readPendingFile
import com.teanode.app.shopping.toPendingFile
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.asReasonSentence
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinancePaystub
import com.teanode.kit.finance.FinanceTaxBoxTitles
import com.teanode.kit.finance.FinanceTaxForm
import com.teanode.kit.finance.FinanceTaxFormView
import com.teanode.kit.finance.TaxDocumentAddress
import com.teanode.kit.finance.TaxDocumentUpload
import com.teanode.kit.finance.TaxDocumentUploadPhase
import com.teanode.kit.finance.TaxDocumentUploads
import com.teanode.kit.finance.TaxEstimateSliceKind
import com.teanode.kit.finance.TaxFilingStatus
import com.teanode.kit.finance.TaxWords
import com.teanode.kit.finance.TaxYearFigureKind
import com.teanode.kit.finance.TaxYearFigureRow
import com.teanode.kit.finance.TaxYearSetting
import com.teanode.kit.finance.TaxYearSettingChange
import com.teanode.kit.finance.TaxYearSettingForm
import com.teanode.kit.finance.TaxYearSummary
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
import com.teanode.kit.finance.isW2
import com.teanode.kit.finance.kindName
import com.teanode.kit.finance.lines
import com.teanode.kit.finance.readTaxDocumentOfAttachment
import com.teanode.kit.finance.regenerateTaxDocumentAddress
import com.teanode.kit.finance.rows
import com.teanode.kit.finance.setTaxFilingStatus
import com.teanode.kit.finance.setTaxYearSetting
import com.teanode.kit.finance.shareOfPay
import com.teanode.kit.finance.slices
import com.teanode.kit.finance.taxDocumentAddress
import com.teanode.kit.finance.taxYear
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException
import java.time.format.FormatStyle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private class TaxesRead(val summary: TaxYearSummary?, val paystubs: List<FinancePaystub>, val forms: List<FinanceTaxFormView>, val address: TaxDocumentAddress?)

/** The currency of every figure of a tax year: dollars, as a W-2's are. */
private const val TAX_CURRENCY_CODE = "USD"

private val warningColor = Color(0xFFEF6C00)

@Composable
private fun figureWords(kind: TaxYearFigureKind): String = when (kind) {
    TaxYearFigureKind.Wages -> stringResource(R.string.finance_tax_wages)
    TaxYearFigureKind.FederalIncomeTax -> stringResource(R.string.finance_tax_federal_withheld)
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

/** A day the server writes as "2030-04-03", as the reader writes days. */
internal fun taxDayText(day: String?): String {
    if (day.isNullOrEmpty()) return ""
    return try {
        LocalDate.parse(day).format(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM))
    } catch (_: DateTimeParseException) {
        day
    }
}

private fun isPositive(amount: String?): Boolean = (FinanceMoney.decimal(amount)?.signum() ?: 0) > 0

internal fun taxMoney(amount: String?): String = FinanceMoney.shown(amount, TAX_CURRENCY_CODE)

/** Tax withheld in orange, the part still to come paler, what is owed in red, and what is left in the app's green. */
@Composable
private fun sliceColor(sliceKind: TaxEstimateSliceKind): Color = when (sliceKind) {
    TaxEstimateSliceKind.Paid -> warningColor
    TaxEstimateSliceKind.UpcomingWithheld -> warningColor.copy(alpha = 0.45f)
    TaxEstimateSliceKind.Owing -> MaterialTheme.colorScheme.error
    TaxEstimateSliceKind.RemainingIncome -> MaterialTheme.colorScheme.primary
}

/** A slice's name; what was paid is "so far" only while the year is projected. */
private fun sliceWords(sliceKind: TaxEstimateSliceKind, estimate: TaxYearSummary.Estimate): Int = when (sliceKind) {
    TaxEstimateSliceKind.Paid -> if (estimate.isProjected == true) R.string.finance_tax_paid else R.string.finance_tax_withheld
    TaxEstimateSliceKind.UpcomingWithheld -> R.string.finance_tax_upcoming
    TaxEstimateSliceKind.Owing -> R.string.finance_tax_owing
    TaxEstimateSliceKind.RemainingIncome -> R.string.finance_tax_remaining_income
}

/** One figure of an estimate: its name, with the slice's swatch when it is one, and its amount over its share of pay. */
@Composable
private fun EstimateFigureRow(label: String, amount: String?, estimate: TaxYearSummary.Estimate, swatchColor: Color? = null, isTotal: Boolean = false, onClick: (() -> Unit)? = null) {
    Row(
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (swatchColor != null) Box(Modifier.padding(end = 8.dp).size(12.dp).background(swatchColor, CircleShape))
        Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Column(horizontalAlignment = Alignment.End) {
            Text(taxMoney(amount), style = MaterialTheme.typography.bodyLarge, fontWeight = if (isTotal) FontWeight.SemiBold else FontWeight.Normal)
            estimate.shareOfPay(amount)?.let { share -> Text(FinanceMoney.percentWithOneDecimal(share), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

/** One figure of a year, with where it is heading by December 31 under it when there is a projection. */
@Composable
private fun TaxFigureRow(row: TaxYearFigureRow) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.Top) {
        Text(figureWords(row.figureKind), Modifier.weight(1f).padding(end = 12.dp), style = MaterialTheme.typography.bodyLarge)
        Column(horizontalAlignment = Alignment.End) {
            Text(taxMoney(row.amount), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            row.projectedAmount?.takeIf { projected -> projected.isNotEmpty() }?.let { projected ->
                Text(stringResource(R.string.finance_tax_wages_projected, taxMoney(projected)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** One thing worth knowing about the year, in its sentence, and whose it is. */
@Composable
internal fun TaxYearFindingRow(finding: TaxYearSummary.Finding, onClick: (() -> Unit)? = null) {
    val isProblem = TaxWords.isProblem(finding)
    Row(
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(if (isProblem) Icons.Outlined.Warning else Icons.Outlined.Info, null, tint = if (isProblem) warningColor else MaterialTheme.colorScheme.onSurfaceVariant)
        Column(Modifier.weight(1f)) {
            Text(finding.findingText.orEmpty(), style = MaterialTheme.typography.bodyLarge)
            val whose = TaxWords.whoseText(finding)
            if (whose.isNotEmpty()) Text(whose, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** One W-2 box beside the paystubs' figure, and whether they match. */
@Composable
private fun TaxYearComparisonRow(comparison: TaxYearSummary.Comparison) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(comparison.boxLabel?.takeIf { label -> label.isNotEmpty() } ?: comparison.boxName, style = MaterialTheme.typography.bodyLarge)
            Text(
                stringResource(R.string.finance_tax_comparison_detail, comparison.boxName, taxMoney(comparison.w2Amount), taxMoney(comparison.paystubAmount)),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        if (comparison.isMatching == true) {
            Icon(Icons.Outlined.CheckCircle, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(stringResource(R.string.finance_tax_matches), Modifier.padding(start = 4.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            Icon(Icons.Outlined.Warning, null, tint = warningColor)
            Text(stringResource(R.string.finance_tax_differs_by, taxMoney(comparison.differenceAmount)), Modifier.padding(start = 4.dp), style = MaterialTheme.typography.bodyMedium, color = warningColor)
        }
    }
}

/** A tax form in a list: the payer, its kind and year, and whether it agrees with itself. */
@Composable
private fun TaxFormRow(form: FinanceTaxFormView, onClick: () -> Unit) {
    val taxForm = form.financeTaxForm
    ListItem(
        headlineContent = { Text(taxForm?.payerName?.takeIf { name -> name.isNotEmpty() } ?: taxForm?.kindName.orEmpty()) },
        supportingContent = {
            Column {
                Text(
                    listOfNotNull(
                        taxForm?.kindName, taxForm?.recipientName?.takeIf { name -> name.isNotEmpty() }, taxForm?.taxYear?.toString(),
                        if (taxForm?.isCorrected == true) stringResource(R.string.finance_tax_corrected) else null,
                    ).joinToString(" · "),
                )
                if (form.disagreeingCount > 0) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.Warning, null, Modifier.size(16.dp), tint = warningColor)
                        Text(pluralStringResource(R.plurals.finance_tax_checks_disagree, form.disagreeingCount, form.disagreeingCount), Modifier.padding(start = 4.dp), color = warningColor)
                    }
                }
            }
        },
        modifier = Modifier.clickable(role = Role.Button, onClick = onClick),
    )
}

/** A paystub in a list: the employer, the pay date, the take-home pay, and how much it is off by when it does not add up. */
@Composable
private fun PaystubRow(paystub: FinancePaystub, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(paystub.employerName?.takeIf { name -> name.isNotEmpty() } ?: stringResource(R.string.finance_tax_paystub)) },
        supportingContent = {
            Column {
                Text(taxDayText(paystub.payDate))
                if (!paystub.isBalanced) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.Warning, null, Modifier.size(16.dp), tint = warningColor)
                        Text(stringResource(R.string.finance_tax_off_by, FinanceMoney.shown(paystub.checkDifferenceAmount, paystub.currencyCode)), Modifier.padding(start = 4.dp), color = warningColor)
                    }
                }
            }
        },
        trailingContent = { Text(FinanceMoney.shown(paystub.netAmount, paystub.currencyCode)) },
        modifier = Modifier.clickable(role = Role.Button, onClick = onClick),
    )
}

/** What the year shows before any paystub or tax form is in: what to add, and the way to add it. */
@Composable
internal fun TaxDocumentsEmptyState(onAdd: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Icon(Icons.Outlined.Description, null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(stringResource(R.string.finance_tax_no_documents), style = MaterialTheme.typography.titleMedium)
        Text(stringResource(R.string.finance_tax_no_documents_hint), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        OutlinedButton(onClick = onAdd) { Text(stringResource(R.string.finance_tax_add_documents)) }
    }
}

/** A tax year put together from paystubs and W-2s: what was earned and withheld, the estimate, what is worth knowing, and the documents behind it. */
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
    var isRegeneratingAddress by remember { mutableStateOf(false) }
    var selectedSliceKind by remember { mutableStateOf<TaxEstimateSliceKind?>(null) }
    val read = rememberLoad(connection, year, filingStatus) {
        TaxesRead(connection.taxYear(year, filingStatus), connection.financePaystubs(year).financePaystubs.filterNotNull(), connection.financeTaxForms(year).taxForms.filterNotNull(), connection.taxDocumentAddress())
    }
    var uploads by remember { mutableStateOf<List<TaxDocumentUpload>>(emptyList()) }
    var nextUploadId by remember { mutableLongStateOf(1L) }
    var isAddMenuOpen by remember { mutableStateOf(false) }
    var renewedAddress by remember { mutableStateOf<TaxDocumentAddress?>(null) }

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
    val photoPicker = androidx.activity.compose.rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(10)) { uris ->
        if (uris.isNotEmpty()) addFiles { uris.mapIndexed { index, uri -> context.readPendingFile(uri, "tax-document-${index + 1}.jpg") } }
    }
    val cameraPicker = androidx.activity.compose.rememberLauncherForActivityResult(ActivityResultContracts.TakePicturePreview()) { bitmap ->
        if (bitmap != null) addFiles { listOf(bitmap.toPendingFile("tax-document.jpg")) }
    }
    val filePicker = androidx.activity.compose.rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { uris ->
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
                    HorizontalDivider()
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.finance_entry_paystub_title)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) },
                        onClick = { isAddMenuOpen = false; navigator.navigate(FinanceRoutes.paystubEntry) },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.finance_entry_tax_form_title)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) },
                        onClick = { isAddMenuOpen = false; navigator.navigate(FinanceRoutes.taxFormEntry) },
                    )
                }
            }
        },
    ) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val summary = loaded.summary
            val address = renewedAddress ?: loaded.address
            val paystubs = loaded.paystubs.sortedByDescending { paystub -> paystub.payDate.orEmpty() }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("year") { YearStepper(year, LocalDate.now().year - 4, LocalDate.now().year) { year = it } }
                if (address != null) {
                    item("mail-in-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_mail_in)) }
                    item("mail-in") {
                        Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            val mailInAddress = address.taxDocumentAddress.orEmpty()
                            if (mailInAddress.isNotEmpty()) {
                                SelectionContainer { Text(mailInAddress, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace) }
                                OutlinedButton(onClick = {
                                    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("address", mailInAddress))
                                    snackbar.show(context.getString(R.string.finance_tax_address_copied))
                                }) { Icon(Icons.Outlined.ContentCopy, null, Modifier.padding(end = 8.dp).size(18.dp)); Text(stringResource(R.string.finance_tax_copy_address)) }
                            } else {
                                Text(stringResource(R.string.finance_tax_no_mailbox_address), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            OutlinedButton(onClick = { isRegeneratingAddress = true }) { Icon(Icons.Outlined.Refresh, null, Modifier.padding(end = 8.dp).size(18.dp)); Text(stringResource(R.string.finance_new_address)) }
                        }
                    }
                    item("mail-in-footer") { Footnote(stringResource(if (address.canReadPdfs == false) R.string.finance_tax_mail_in_footer_photos else R.string.finance_tax_mail_in_footer)) }
                }
                if (uploads.isNotEmpty()) {
                    item("adding-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_adding)) }
                    items(uploads, key = { upload -> "adding-" + upload.uploadId }) { upload ->
                        TaxDocumentUploadRow(upload, onHide = { uploads = uploads.filter { each -> each.uploadId != upload.uploadId } })
                    }
                }
                if (paystubs.isEmpty() && loaded.forms.isEmpty()) {
                    item("none") { TaxDocumentsEmptyState(onAdd = { isAddMenuOpen = true }) }
                }
                summary?.taxYearEstimate?.let { estimate ->
                    item("estimate-header") { TeaNodeSectionHeader(stringResource(if (estimate.isProjected == true) R.string.finance_tax_estimate_header_projected else R.string.finance_tax_estimate_header, year.toString())) }
                    val slices = estimate.slices
                    if (slices.isNotEmpty()) {
                        item("ring") {
                            val selectedSlice = slices.firstOrNull { slice -> slice.sliceKind == selectedSliceKind }
                            val sliceColors = slices.map { slice -> DonutSlice(slice.sliceKind.name, slice.amountValue, sliceColor(slice.sliceKind)) }
                            Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                                DonutChart(sliceColors, selectedSlice?.sliceKind?.name) {
                                    Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.width(110.dp)) {
                                        Text(
                                            selectedSlice?.let { slice -> stringResource(sliceWords(slice.sliceKind, estimate)) } ?: stringResource(R.string.finance_tax_for_the_year),
                                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center, maxLines = 2,
                                        )
                                        Text(taxMoney(selectedSlice?.amount ?: estimate.totalTaxAmount), style = MaterialTheme.typography.titleMedium, maxLines = 1)
                                    }
                                }
                            }
                        }
                        for (slice in slices) {
                            item("slice-" + slice.sliceKind.name) {
                                EstimateFigureRow(
                                    stringResource(sliceWords(slice.sliceKind, estimate)), slice.amount, estimate, swatchColor = sliceColor(slice.sliceKind),
                                    onClick = { selectedSliceKind = if (selectedSliceKind == slice.sliceKind) null else slice.sliceKind },
                                )
                            }
                        }
                    }
                    if (isPositive(estimate.refundAmount)) item("refund") { EstimateFigureRow(stringResource(R.string.finance_tax_refund), estimate.refundAmount, estimate) }
                    item("pay") { EstimateFigureRow(stringResource(R.string.finance_tax_pay), estimate.payAmount, estimate, isTotal = true) }
                    if ((FinanceMoney.decimal(estimate.investmentIncomeAmount)?.signum() ?: 0) != 0) {
                        item("investment-income") { EstimateFigureRow(stringResource(R.string.finance_tax_investment_income), estimate.investmentIncomeAmount, estimate) }
                        item("income") { EstimateFigureRow(stringResource(R.string.finance_tax_income), estimate.incomeAmount, estimate, isTotal = true) }
                    }
                    item("status") {
                        Box(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
                            ExposedDropdownMenuBox(isStatusOpen, { isStatusOpen = it }) {
                                OutlinedTextField(
                                    stringResource(statusWords(estimate.filingStatus ?: TaxFilingStatus.Single)), {}, readOnly = true,
                                    label = { Text(stringResource(R.string.finance_tax_filing_status)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isStatusOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
                                )
                                ExposedDropdownMenu(isStatusOpen, { isStatusOpen = false }) {
                                    for (each in TaxFilingStatus.entries) DropdownMenuItem(text = { Text(stringResource(statusWords(each))) }, onClick = { isStatusOpen = false; filingStatus = each; act(context.getString(R.string.finance_saved)) { connection.setTaxFilingStatus(each) } })
                                }
                            }
                        }
                    }
                    item("federal") { EstimateFigureRow(stringResource(R.string.finance_tax_federal_income_tax), estimate.federalIncomeTaxAmount, estimate) }
                    if (isPositive(estimate.preferentialIncomeTaxAmount)) item("preferential") { EstimateFigureRow(stringResource(R.string.finance_tax_preferential), estimate.preferentialIncomeTaxAmount, estimate) }
                    if (isPositive(estimate.foreignTaxCreditAmount)) item("foreign-credit") { EstimateFigureRow(stringResource(R.string.finance_tax_foreign_credit), "-" + estimate.foreignTaxCreditAmount, estimate) }
                    item("social-security") { EstimateFigureRow(stringResource(R.string.finance_tax_social_security), estimate.socialSecurityTaxAmount, estimate) }
                    item("medicare") { EstimateFigureRow(stringResource(R.string.finance_tax_medicare), estimate.medicareTaxAmount, estimate) }
                    if (isPositive(estimate.netInvestmentIncomeTaxAmount)) item("net-investment") { EstimateFigureRow(stringResource(R.string.finance_tax_net_investment), estimate.netInvestmentIncomeTaxAmount, estimate) }
                    if (isPositive(estimate.stateIncomeTaxAmount)) item("state") { EstimateFigureRow(stringResource(R.string.finance_tax_state_as_withheld), estimate.stateIncomeTaxAmount, estimate) }
                    if (isPositive(estimate.localIncomeTaxAmount)) item("local") { EstimateFigureRow(stringResource(R.string.finance_tax_local_as_withheld), estimate.localIncomeTaxAmount, estimate) }
                    if (isPositive(estimate.childTaxCreditAmount)) item("child-credit") { EstimateFigureRow(stringResource(R.string.finance_tax_child_credit), "-" + estimate.childTaxCreditAmount, estimate) }
                    if (isPositive(estimate.dependentCareCreditAmount)) item("care-credit") { EstimateFigureRow(stringResource(R.string.finance_tax_care_credit), "-" + estimate.dependentCareCreditAmount, estimate) }
                    item("total") { EstimateFigureRow(stringResource(R.string.finance_tax_for_the_year), estimate.totalTaxAmount, estimate, isTotal = true) }
                    item("settings") { TeaNodeRow(stringResource(R.string.finance_tax_year_settings_row), leadingIcon = Icons.Outlined.Tune, onClick = { isEditingSettings = true }) }
                    item("estimate-footer") {
                        Column {
                            if (estimate.isTaxFilingStatusAssumed == true) Footnote(stringResource(R.string.finance_tax_status_assumed_footer))
                            if (isPositive(estimate.otherIncomeAmount)) Footnote(stringResource(R.string.finance_tax_includes_other_income, taxMoney(estimate.otherIncomeAmount)))
                            Footnote(
                                when {
                                    estimate.isItemized == true -> stringResource(
                                        R.string.finance_tax_estimate_itemized, taxMoney(estimate.taxableIncomeAmount), taxMoney(estimate.deductionAmount), taxMoney(estimate.mortgageInterestAmount),
                                        taxMoney(estimate.stateAndLocalTaxDeductionAmount), taxMoney(estimate.charitableContributionAmount),
                                    )
                                    isPositive(estimate.charitableContributionAmount) -> stringResource(
                                        R.string.finance_tax_estimate_charity, taxMoney(estimate.taxableIncomeAmount), taxMoney(estimate.standardDeductionAmount), taxMoney(estimate.charitableContributionAmount),
                                    )
                                    else -> stringResource(R.string.finance_tax_estimate_standard, taxMoney(estimate.taxableIncomeAmount), taxMoney(estimate.standardDeductionAmount))
                                },
                            )
                        }
                    }
                }
                summary?.taxYearInvestmentPreview?.let { preview -> taxInvestmentPreview(preview) }
                val findings = summary?.findings.orEmpty()
                if (findings.isNotEmpty()) {
                    item("findings-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_findings)) }
                    for ((index, finding) in findings.withIndex()) {
                        item("finding-$index") {
                            val paystubId = finding.paystubIds?.firstOrNull()
                            TaxYearFindingRow(finding, onClick = paystubId?.let { id -> { navigator.navigate(FinanceRoutes.paystub(id)) } })
                        }
                    }
                }
                val employments = summary?.employments.orEmpty()
                for ((employmentIndex, employment) in employments.withIndex()) {
                    item("employment-$employmentIndex-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_employment_header, employment.employeeName.orEmpty(), employment.employerName.orEmpty())) }
                    item("employment-$employmentIndex-source") {
                        val taxFormId = employment.taxFormId
                        if (employment.isFromW2 && taxFormId != null && loaded.forms.any { form -> form.id == taxFormId }) {
                            TeaNodeRow(stringResource(R.string.finance_tax_from_w2), onClick = { navigator.navigate(FinanceRoutes.taxForm(taxFormId)) })
                        } else {
                            val sourceText = when {
                                employment.isFromW2 -> stringResource(R.string.finance_tax_from_w2)
                                (employment.paystubCount ?: 0) == 1 -> stringResource(R.string.finance_tax_from_one_paystub, taxDayText(employment.latestPayDate))
                                else -> stringResource(R.string.finance_tax_from_paystubs_through, employment.paystubCount ?: 0, taxDayText(employment.latestPayDate))
                            }
                            Text(sourceText, Modifier.padding(horizontal = 16.dp, vertical = 10.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    employment.figures?.let { figures ->
                        for (row in figures.rows(employment.projectedFigures)) item("employment-$employmentIndex-figure-" + row.figureKind.toString()) { TaxFigureRow(row) }
                    }
                    if (employment.projectedFigures != null) {
                        item("employment-$employmentIndex-footer") {
                            Column {
                                Footnote(pluralStringResource(R.plurals.finance_tax_projected_paychecks, employment.remainingPayCount ?: 0, employment.remainingPayCount ?: 0))
                                val limit = summary?.retirementDeferralLimitAmount
                                if (employment.isRetirementDeferralCapped == true && limit != null) Footnote(stringResource(R.string.finance_tax_retirement_capped, taxMoney(limit)))
                            }
                        }
                    }
                    val comparisons = employment.comparisons
                    if (comparisons.isNotEmpty()) {
                        item("employment-$employmentIndex-comparison-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_comparison_header)) }
                        for ((comparisonIndex, comparison) in comparisons.withIndex()) item("employment-$employmentIndex-comparison-$comparisonIndex") { TaxYearComparisonRow(comparison) }
                        item("employment-$employmentIndex-comparison-footer") { Footnote(stringResource(R.string.finance_tax_comparison_footer)) }
                    }
                }
                val householdFigures = summary?.figures
                if (employments.size > 1 && householdFigures != null) {
                    item("household-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_household)) }
                    for (row in householdFigures.rows(summary.projectedFigures)) item("household-figure-" + row.figureKind.toString()) { TaxFigureRow(row) }
                    if (summary.projectedFigures != null) item("household-footer") { Footnote(stringResource(R.string.finance_tax_household_projected)) }
                }
                summary?.taxYearInvestment?.let { investment -> taxInvestment(investment) }
                if (loaded.forms.isNotEmpty()) {
                    item("forms-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_forms)) }
                    for (form in loaded.forms) item("form-" + form.id) { TaxFormRow(form, onClick = { navigator.navigate(FinanceRoutes.taxForm(form.id)) }) }
                    item("type-form") { TextButton(onClick = { navigator.navigate(FinanceRoutes.taxFormEntry) }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_entry_tax_form_title)) } }
                }
                if (paystubs.isNotEmpty()) {
                    item("paystubs-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_paystubs)) }
                    for (paystub in paystubs) item("paystub-" + paystub.id) { PaystubRow(paystub, onClick = { navigator.navigate(FinanceRoutes.paystub(paystub.id)) }) }
                    item("type-paystub") { TextButton(onClick = { navigator.navigate(FinanceRoutes.paystubEntry) }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_entry_paystub_title)) } }
                }
            }
            if (isEditingSettings) {
                TaxSettingsDialog(year, summary?.taxYearSetting, onDismiss = { isEditingSettings = false }) { change ->
                    isEditingSettings = false
                    act(context.getString(R.string.finance_tax_settings_saved, year.toString())) { connection.setTaxYearSetting(year, change) }
                }
            }
            if (isRegeneratingAddress) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_tax_new_address_question), confirmLabel = stringResource(R.string.finance_tax_make_new_address), isDestructive = true,
                    text = stringResource(R.string.finance_tax_new_address_message),
                    onConfirm = {
                        isRegeneratingAddress = false
                        scope.launch {
                            try {
                                renewedAddress = connection.regenerateTaxDocumentAddress()
                                snackbar.show(context.getString(R.string.finance_tax_new_address_made))
                            } catch (failure: CancellationException) {
                                throw failure
                            } catch (failure: Throwable) {
                                snackbar.showError(failure)
                            }
                        }
                    },
                    onDismiss = { isRegeneratingAddress = false },
                )
            }
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
            TaxDocumentUploadPhase.Failed -> upload.failureText.orEmpty().asReasonSentence()
        },
        supportingMaxLines = 3,
        leading = {
            when (upload.uploadPhase) {
                TaxDocumentUploadPhase.Uploading, TaxDocumentUploadPhase.Reading -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
                TaxDocumentUploadPhase.StillReading -> Icon(Icons.Outlined.Schedule, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                TaxDocumentUploadPhase.Failed -> Icon(Icons.Outlined.Warning, null, tint = warningColor)
            }
        },
        trailing = { if (upload.isFinished) TextButton(onClick = onHide) { Text(stringResource(R.string.finance_tax_hide)) } },
    )
}

/** The year's interest and dividends as the linked accounts were paid them, until the 1099s come. */
private fun androidx.compose.foundation.lazy.LazyListScope.taxInvestmentPreview(preview: TaxYearSummary.InvestmentPreview) {
    item("preview-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_investment_preview_header)) }
    for ((index, income) in preview.taxYearAccountIncomes.orEmpty().filterNotNull().withIndex()) {
        item("preview-income-$index") {
            LabeledValue(
                income.accountName.orEmpty(), taxMoney(income.amount),
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
                R.string.finance_investment_preview_footer, taxMoney(preview.interestAmount), taxMoney(preview.projectedInterestAmount),
                taxMoney(preview.dividendsAmount), taxMoney(preview.projectedDividendsAmount),
            ),
        )
    }
}

/** The year's investment income from its 1099s: interest and dividends always, every other figure when it is not nothing. */
private fun androidx.compose.foundation.lazy.LazyListScope.taxInvestment(investment: TaxYearSummary.Investment) {
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
    item("investment-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_investment_income)) }
    for ((label, amount, _) in lines) item("investment-$label") { LabeledValue(stringResource(label), taxMoney(amount)) }
    item("investment-footer") { Footnote(stringResource(R.string.finance_tax_investment_footer)) }
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

private fun statusWords(status: TaxFilingStatus): Int = when (status) {
    TaxFilingStatus.Single -> R.string.finance_tax_status_single
    TaxFilingStatus.MarriedFilingJointly -> R.string.finance_tax_status_joint
    TaxFilingStatus.MarriedFilingSeparately -> R.string.finance_tax_status_separate
    TaxFilingStatus.HeadOfHousehold -> R.string.finance_tax_status_head
}

/** What a year's estimate takes that no document says: children and the care paid for them, other household income, taxes and gifts for itemizing, and the gains realized so far. */
@Composable
private fun TaxSettingsDialog(year: Int, existing: TaxYearSetting?, onDismiss: () -> Unit, onSave: (TaxYearSettingChange) -> Unit) {
    var form by remember { mutableStateOf(TaxYearSettingForm.of(existing)) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_tax_year_settings, year.toString())) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                val countKeyboard = KeyboardOptions(keyboardType = KeyboardType.Number)
                val amountKeyboard = KeyboardOptions(keyboardType = KeyboardType.Decimal)
                Text(stringResource(R.string.finance_tax_family), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                OutlinedTextField(form.childCountText, { text -> form = form.copy(childCountText = text) }, label = { Text(stringResource(R.string.finance_tax_children)) }, singleLine = true, keyboardOptions = countKeyboard)
                OutlinedTextField(form.otherDependentCountText, { text -> form = form.copy(otherDependentCountText = text) }, label = { Text(stringResource(R.string.finance_tax_other_dependents)) }, singleLine = true, keyboardOptions = countKeyboard)
                OutlinedTextField(form.dependentCareExpenseText, { text -> form = form.copy(dependentCareExpenseText = text) }, label = { Text(stringResource(R.string.finance_tax_dependent_care_expense)) }, keyboardOptions = amountKeyboard)
                Text(stringResource(R.string.finance_tax_income_and_deductions), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                OutlinedTextField(form.otherIncomeText, { text -> form = form.copy(otherIncomeText = text) }, label = { Text(stringResource(R.string.finance_tax_other_income)) }, keyboardOptions = amountKeyboard)
                OutlinedTextField(form.propertyTaxText, { text -> form = form.copy(propertyTaxText = text) }, label = { Text(stringResource(R.string.finance_tax_property_tax)) }, singleLine = true, keyboardOptions = amountKeyboard)
                OutlinedTextField(form.stateIncomeTaxPaidText, { text -> form = form.copy(stateIncomeTaxPaidText = text) }, label = { Text(stringResource(R.string.finance_tax_state_tax_paid)) }, keyboardOptions = amountKeyboard)
                OutlinedTextField(form.charitableContributionText, { text -> form = form.copy(charitableContributionText = text) }, label = { Text(stringResource(R.string.finance_tax_charity)) }, singleLine = true, keyboardOptions = amountKeyboard)
                Text(stringResource(R.string.finance_tax_gains_header), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                val gainKeyboard = KeyboardOptions(keyboardType = KeyboardType.Text)
                OutlinedTextField(form.shortTermGainText, { text -> form = form.copy(shortTermGainText = text) }, label = { Text(stringResource(R.string.finance_tax_short_gain)) }, singleLine = true, keyboardOptions = gainKeyboard)
                OutlinedTextField(form.longTermGainText, { text -> form = form.copy(longTermGainText = text) }, label = { Text(stringResource(R.string.finance_tax_long_gain)) }, singleLine = true, keyboardOptions = gainKeyboard)
                OutlinedTextField(form.gainNoteText, { text -> form = form.copy(gainNoteText = text) }, label = { Text(stringResource(R.string.finance_tax_gain_note)) })
                Text(stringResource(R.string.finance_tax_settings_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = { TextButton(onClick = { onSave(form.change(existing)) }) { Text(stringResource(R.string.finance_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

/** What the paystub read says besides its figures: a paystub detail's findings are the year's findings that name it. */
private class PaystubRead(val paystub: FinancePaystub?, val findings: List<TaxYearSummary.Finding>)

/** One paystub: who paid whom and when, the totals, every line by kind, what the year says about it, its file, and deleting it. */
@Composable
fun FinancePaystubDetailScreen(paystubId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var isConfirmingDelete by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, paystubId) {
        val paystub = connection.financePaystub(paystubId)
        val paidYear = paystub?.payDate?.take(4)?.toIntOrNull()
        val findings = if (paidYear == null) emptyList() else try {
            connection.taxYear(paidYear, null)?.findings.orEmpty().filter { finding -> finding.paystubIds?.contains(paystubId) == true }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            emptyList()
        }
        PaystubRead(paystub, findings)
    }
    TeaNodeScreen(title = read.value?.paystub?.employerName.orEmpty().ifEmpty { stringResource(R.string.finance_tax_paystub) }, onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val paystub = loaded.paystub ?: return@LoadedContent
            val currency = paystub.currencyCode
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("employer") { LabeledValue(stringResource(R.string.finance_tax_employer), paystub.employerName.orEmpty()) }
                item("employee") { LabeledValue(stringResource(R.string.finance_tax_employee), TaxWords.personText(paystub.employeeName, paystub.employeeTaxIdMask)) }
                item("pay-date") { LabeledValue(stringResource(R.string.finance_tax_pay_date), taxDayText(paystub.payDate)) }
                val periodStart = paystub.periodStartOn
                val periodEnd = paystub.periodEndOn
                if (!periodStart.isNullOrEmpty() && !periodEnd.isNullOrEmpty()) item("period") { LabeledValue(stringResource(R.string.finance_tax_pay_period), stringResource(R.string.finance_tax_to, taxDayText(periodStart), taxDayText(periodEnd))) }
                item("gross") { LabeledValue(stringResource(R.string.finance_tax_gross), FinanceMoney.shown(paystub.grossAmount, currency)) }
                if (!paystub.totalTaxAmount.isNullOrEmpty()) item("taxes") { LabeledValue(stringResource(R.string.finance_tax_taxes), FinanceMoney.shown(paystub.totalTaxAmount, currency)) }
                if (!paystub.totalDeductionAmount.isNullOrEmpty()) item("deductions") { LabeledValue(stringResource(R.string.finance_tax_deductions), FinanceMoney.shown(paystub.totalDeductionAmount, currency)) }
                item("net") { LabeledValue(stringResource(R.string.finance_tax_take_home), FinanceMoney.shown(paystub.netAmount, currency)) }
                if (!paystub.yearToDateGrossAmount.isNullOrEmpty()) item("gross-year") { LabeledValue(stringResource(R.string.finance_tax_gross_year), FinanceMoney.shown(paystub.yearToDateGrossAmount, currency)) }
                item("check") {
                    Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        if (paystub.isBalanced) {
                            Icon(Icons.Outlined.Verified, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                            Text(stringResource(R.string.finance_tax_lines_add_up), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        } else {
                            Icon(Icons.Outlined.Warning, null, tint = warningColor)
                            Text(stringResource(R.string.finance_tax_lines_off_by, FinanceMoney.shown(paystub.checkDifferenceAmount, currency)), color = warningColor)
                        }
                    }
                }
                for ((lineKind, titleRes) in listOf("earning" to R.string.finance_tax_earnings, "tax" to R.string.finance_tax_taxes, "deduction" to R.string.finance_tax_deductions, "reimbursement" to R.string.finance_tax_reimbursements)) {
                    val lines = paystub.lines(lineKind)
                    if (lines.isEmpty()) continue
                    item("kind-$lineKind") { TeaNodeSectionHeader(stringResource(titleRes)) }
                    for (line in lines) item("line-" + line.id) { PaystubLineRow(line, currency) }
                }
                if (loaded.findings.isNotEmpty()) {
                    item("findings-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_findings)) }
                    for ((index, finding) in loaded.findings.withIndex()) item("finding-$index") { TaxYearFindingRow(finding) }
                }
                paystub.agentAttachmentId?.takeIf { attachmentId -> attachmentId.isNotEmpty() }?.let { attachmentId -> item("original") { OpenOriginalButton(attachmentId) } }
                item("delete") { TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_tax_delete_paystub), color = MaterialTheme.colorScheme.error) } }
            }
            if (isConfirmingDelete) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_tax_delete_paystub_question), text = stringResource(R.string.finance_tax_delete_paystub_message),
                    confirmLabel = stringResource(R.string.finance_tax_delete_paystub), isDestructive = true,
                    onConfirm = {
                        isConfirmingDelete = false
                        scope.launch {
                            try {
                                connection.deletePaystub(paystubId)
                                snackbar.show(context.getString(R.string.finance_tax_paystub_deleted))
                                navigator.back()
                            } catch (failure: CancellationException) {
                                throw failure
                            } catch (failure: Throwable) {
                                snackbar.showError(failure)
                            }
                        }
                    },
                    onDismiss = { isConfirmingDelete = false },
                )
            }
        }
    }
}

/** One printed line of a paystub: what it is, this period's amount, and the year's so far, with an earning's hours, a tax's wages and a deduction's employer part when printed. */
@Composable
private fun PaystubLineRow(line: FinancePaystub.Line, currency: String?) {
    val detailParts = mutableListOf<String>()
    FinanceMoney.decimal(line.hours)?.let { hours ->
        val hoursText = hours.stripTrailingZeros().toPlainString()
        val rate = line.rateAmount
        detailParts += if (!rate.isNullOrEmpty()) stringResource(R.string.finance_tax_hours_at, hoursText, FinanceMoney.shown(rate, currency)) else stringResource(R.string.finance_tax_hours, hoursText)
    }
    line.taxJurisdictionCode?.takeIf { code -> code.isNotEmpty() }?.let { code -> detailParts += code }
    line.currentWagesAmount?.takeIf { wages -> wages.isNotEmpty() }?.let { wages -> detailParts += stringResource(R.string.finance_tax_on_wages, FinanceMoney.shown(wages, currency)) }
    line.employerCurrentAmount?.takeIf { amount -> amount.isNotEmpty() }?.let { amount -> detailParts += stringResource(R.string.finance_tax_employer_adds, FinanceMoney.shown(amount, currency)) }
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.Top) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(line.description.orEmpty(), style = MaterialTheme.typography.bodyLarge)
            if (detailParts.isNotEmpty()) Text(detailParts.joinToString(", "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Column(horizontalAlignment = Alignment.End) {
            Text(FinanceMoney.shown(line.currentAmount, currency), style = MaterialTheme.typography.bodyLarge)
            line.yearToDateAmount?.takeIf { amount -> amount.isNotEmpty() }?.let { amount ->
                Text(stringResource(R.string.finance_tax_year_to_date, FinanceMoney.shown(amount, currency)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** One W-2, 1099 or 1098: whose it is, whether it agrees with itself, every box, its file, and deleting it. */
@Composable
fun FinanceTaxFormDetailScreen(taxFormId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var isConfirmingDelete by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, taxFormId) { connection.financeTaxForm(taxFormId) }
    TeaNodeScreen(title = read.value?.financeTaxForm?.let { form -> form.payerName?.ifEmpty { null } ?: form.kindName }.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { view ->
            val form = view?.financeTaxForm ?: return@LoadedContent
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("kind") { LabeledValue(stringResource(R.string.finance_tax_form), form.kindName) }
                if (form.isW2) {
                    item("payer") { LabeledValue(stringResource(R.string.finance_tax_employer), form.payerName.orEmpty()) }
                    item("recipient") { LabeledValue(stringResource(R.string.finance_tax_employee), TaxWords.personText(form.recipientName, form.recipientTaxIdMask)) }
                } else {
                    item("payer") { LabeledValue(stringResource(R.string.finance_tax_payer), form.payerName.orEmpty()) }
                    item("recipient") { LabeledValue(stringResource(R.string.finance_tax_recipient), TaxWords.personText(form.recipientName, form.recipientTaxIdMask)) }
                }
                form.taxYear?.let { taxYear -> item("year") { LabeledValue(stringResource(R.string.finance_tax_year_label), taxYear.toString()) } }
                if (form.isCorrected == true) {
                    item("corrected") {
                        Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Icon(Icons.Outlined.Edit, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                            Text(stringResource(R.string.finance_tax_corrected_form), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                val findings = view.findings
                if (findings.isNotEmpty()) {
                    item("findings-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_checks)) }
                    for ((index, finding) in findings.withIndex()) {
                        item("finding-$index") {
                            val isAgreeing = finding.isAgreeing == true
                            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                Icon(if (isAgreeing) Icons.Outlined.CheckCircle else Icons.Outlined.Warning, null, tint = if (isAgreeing) MaterialTheme.colorScheme.onSurfaceVariant else warningColor)
                                Text(finding.findingText.orEmpty().ifEmpty { finding.taxFormFindingCode.orEmpty() }, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                            }
                        }
                    }
                }
                if (form.entries.isNotEmpty()) {
                    item("boxes-header") { TeaNodeSectionHeader(stringResource(R.string.finance_tax_boxes)) }
                    for (entry in form.entries) item("entry-" + entry.id) { TaxFormEntryRow(entry, form.taxFormKind) }
                }
                form.agentAttachmentId?.takeIf { attachmentId -> attachmentId.isNotEmpty() }?.let { attachmentId -> item("original") { OpenOriginalButton(attachmentId) } }
                item("delete") { TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_tax_delete_form), color = MaterialTheme.colorScheme.error) } }
            }
            if (isConfirmingDelete) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_tax_delete_form_question), text = stringResource(R.string.finance_tax_delete_form_message),
                    confirmLabel = stringResource(R.string.finance_tax_delete_form), isDestructive = true,
                    onConfirm = {
                        isConfirmingDelete = false
                        scope.launch {
                            try {
                                connection.deleteTaxForm(taxFormId)
                                snackbar.show(context.getString(R.string.finance_tax_form_deleted))
                                navigator.back()
                            } catch (failure: CancellationException) {
                                throw failure
                            } catch (failure: Throwable) {
                                snackbar.showError(failure)
                            }
                        }
                    },
                    onDismiss = { isConfirmingDelete = false },
                )
            }
        }
    }
}

/** One box of a tax form: its number and name, and its amount, text or mark. */
@Composable
private fun TaxFormEntryRow(entry: FinanceTaxForm.Entry, taxFormKind: String?) {
    // A 1099-B's boxes are told apart by their total, said in words under the box rather than as its qualifier.
    val termKey = FinanceTaxBoxTitles.termKey(taxFormKind, entry.boxQualifier)
    val boxTitle = taxBoxWords(FinanceTaxBoxTitles.boxKey(taxFormKind, entry.boxCode))
    val termTitle = taxBoxWords(termKey)
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(end = 12.dp)) {
            Text(stringResource(R.string.finance_tax_box, if (termKey == null) entry.boxName else entry.boxCode.orEmpty()), style = MaterialTheme.typography.bodyLarge)
            if (boxTitle != null) Text(boxTitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (termTitle != null) Text(termTitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        when {
            !entry.amount.isNullOrEmpty() -> Text(taxMoney(entry.amount), style = MaterialTheme.typography.bodyLarge)
            !entry.textValue.isNullOrEmpty() -> Text(entry.textValue.orEmpty(), style = MaterialTheme.typography.bodyLarge)
            entry.isChecked == true -> {
                Icon(Icons.Outlined.CheckBox, null)
                Text(stringResource(R.string.finance_entry_marked), Modifier.padding(start = 4.dp), style = MaterialTheme.typography.bodyLarge)
            }
        }
    }
}

/** The words for a box of a tax form from its key, or null for a box with none. */
@Composable
internal fun taxBoxWords(key: String?): String? {
    if (key == null) return null
    val context = LocalContext.current
    val identifier = context.resources.getIdentifier("finance_tax_box_$key", "string", context.packageName)
    return if (identifier == 0) null else context.getString(identifier)
}

/** What a box of a form is, by the form's kind and the box's number as typed. */
@Composable
internal fun taxBoxTitleWords(taxFormKind: String, boxCode: String): String? = taxBoxWords(FinanceTaxBoxTitles.boxKey(taxFormKind, boxCode.trim().lowercase()))
