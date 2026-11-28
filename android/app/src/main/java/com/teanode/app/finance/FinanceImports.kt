package com.teanode.app.finance

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.Error
import androidx.compose.material.icons.outlined.Mail
import androidx.compose.material.icons.outlined.ReceiptLong
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.Cancel
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.chats.PictureLightbox
import com.teanode.app.chats.openServerFile
import com.teanode.app.mail.MailRoutes
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.finance.FinanceImport
import com.teanode.kit.finance.FinanceImportPage
import com.teanode.kit.finance.financeImports
import com.teanode.kit.finance.readStatement
import com.teanode.kit.finance.readTaxDocumentOfAttachment
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

private const val IMPORTS_PAGE_SIZE = 50
private const val IMPORTS_POLL_MILLISECONDS = 5_000L

/** What the person sees of one import: the job's status read together with its outcome sentence. */
private val FinanceImport.isActive: Boolean get() = agentJobStatus == "queued" || agentJobStatus == "running"

/** A finished job whose sentence says it brought nothing in: it worked, but the person has something to do. */
private val FinanceImport.isNothingImported: Boolean
    get() = agentJobStatus == "done" && outcomeText.orEmpty().trim().let { text -> text.startsWith("Imported nothing") || text.startsWith("Not read") }

/** Whether the row is a problem the person has to act on: the job died, or it finished and imported nothing. */
private val FinanceImport.isProblem: Boolean get() = agentJobStatus == "dead" || isNothingImported

private val FinanceImport.isPicture: Boolean
    get() = !agentAttachmentId.isNullOrEmpty() && attachmentContentType.orEmpty().startsWith("image/")

private fun FinanceImport.queuedInstant(): Instant? = try {
    Instant.parse(queuedAt.orEmpty())
} catch (_: Exception) {
    null
}

private fun instantOf(text: String?): Instant? = try {
    Instant.parse(text.orEmpty())
} catch (_: Exception) {
    null
}

private fun timeText(instant: Instant?): String =
    if (instant == null) "" else java.text.DateFormat.getTimeInstance(java.text.DateFormat.SHORT).format(java.util.Date.from(instant))

private fun dateTimeText(instant: Instant?): String =
    if (instant == null) "" else java.text.DateFormat.getDateTimeInstance(java.text.DateFormat.MEDIUM, java.text.DateFormat.SHORT).format(java.util.Date.from(instant))

@Composable
private fun kindText(financeImport: FinanceImport): String = when (financeImport.financeImportKind) {
    "tax_document" -> stringResource(R.string.finance_import_kind_tax_document)
    "statement" -> stringResource(R.string.finance_import_kind_statement)
    "receipt" -> stringResource(R.string.finance_import_kind_receipt)
    else -> financeImport.financeImportKind.orEmpty()
}

/** What the row says it came to: the sentence when finished, else the stage it is at. */
@Composable
private fun headlineText(financeImport: FinanceImport): String {
    val outcomeText = financeImport.outcomeText.orEmpty().trim()
    return when (financeImport.agentJobStatus) {
        "queued" -> stringResource(R.string.finance_import_waiting)
        "running" -> stringResource(R.string.finance_import_reading)
        "cancelled" -> stringResource(R.string.finance_import_status_cancelled)
        "dead" -> outcomeText.ifEmpty { stringResource(R.string.finance_import_status_dead) }
        else -> outcomeText.ifEmpty { stringResource(R.string.finance_import_status_done) }
    }
}

/** The files read into finance in the background (tax documents, statements, receipts), newest first, grouped by day. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun FinanceImportsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val firstPage = rememberLoad(connection) { connection.financeImports(limit = IMPORTS_PAGE_SIZE) }
    TeaNodeScreen(title = stringResource(R.string.finance_part_imports), onBack = { navigator.back() }) { padding ->
        LoadedContent(
            firstPage, modifier = Modifier.padding(padding), isEmpty = { page -> page.financeImports.isEmpty() },
            empty = { EmptyState(Icons.Outlined.Download, stringResource(R.string.finance_no_imports), description = stringResource(R.string.finance_no_imports_description)) },
        ) { page ->
            // The first page as the quiet five-second refresh last read it; a pull to refresh brings a new page and clears it.
            var polledPage by remember(page) { mutableStateOf<FinanceImportPage?>(null) }
            var laterImports by remember { mutableStateOf<List<FinanceImport>>(emptyList()) }
            var isLastPageRead by remember(page) { mutableStateOf(page.hasMore != true) }
            var isLoadingMore by remember { mutableStateOf(false) }
            val shownFirstPage = polledPage ?: page
            val firstImports = shownFirstPage.financeImports.filterNotNull()
            val firstIds = firstImports.map { financeImport -> financeImport.agentJobId }.toSet()
            val imports = firstImports + laterImports.filter { financeImport -> financeImport.agentJobId !in firstIds }.distinctBy { financeImport -> financeImport.agentJobId }
            val isAnyActive = firstImports.any { financeImport -> financeImport.isActive }
            // While a listed import is waiting or being read, the first page is read again every five seconds.
            LaunchedEffect(isAnyActive, page) {
                while (isAnyActive) {
                    delay(IMPORTS_POLL_MILLISECONDS)
                    try {
                        polledPage = connection.financeImports(limit = IMPORTS_PAGE_SIZE)
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (_: Throwable) {
                        // The next round tries again; a failed quiet refresh keeps what is on screen.
                    }
                }
            }
            val zone = remember { ZoneId.systemDefault() }
            val groups = remember(imports) { imports.groupBy { financeImport -> financeImport.queuedInstant()?.atZone(zone)?.toLocalDate() }.toList() }
            val today = remember { LocalDate.now(zone) }
            val todayText = stringResource(R.string.finance_import_today)
            val yesterdayText = stringResource(R.string.finance_import_yesterday)
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                for ((day, dayImports) in groups) {
                    if (day != null) {
                        stickyHeader(key = "day-$day") {
                            Text(
                                importDayTitle(day, today, todayText, yesterdayText),
                                Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surface).padding(horizontal = 16.dp, vertical = 8.dp),
                                style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary,
                            )
                        }
                    }
                    items(dayImports.size, key = { index -> dayImports[index].agentJobId }) { index ->
                        val financeImport = dayImports[index]
                        FinanceImportRow(financeImport) { navigator.navigate(FinanceRoutes.financeImport(financeImport.agentJobId)) }
                        // The last row on screen reads the next page, if there is one.
                        if (financeImport.agentJobId == imports.last().agentJobId && !isLastPageRead) {
                            LaunchedEffect(imports.size) {
                                isLoadingMore = true
                                try {
                                    val next = connection.financeImports(limit = IMPORTS_PAGE_SIZE, offset = imports.size)
                                    laterImports = laterImports + next.financeImports.filterNotNull()
                                    isLastPageRead = next.hasMore != true || next.financeImports.isEmpty()
                                } catch (failure: CancellationException) {
                                    throw failure
                                } catch (failure: Throwable) {
                                    snackbar.showError(failure)
                                    isLastPageRead = true
                                } finally {
                                    isLoadingMore = false
                                }
                            }
                        }
                    }
                }
                if (isLoadingMore) {
                    item("loading-more") {
                        Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp) }
                    }
                }
            }
        }
    }
}

private fun importDayTitle(day: LocalDate, today: LocalDate, todayText: String, yesterdayText: String): String {
    if (day == today) return todayText
    if (day == today.minusDays(1)) return yesterdayText
    val locale = Locale.getDefault()
    return DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, "EEEEMMMMd"), locale).format(day)
}

/** The square before a row: the picture's thumbnail, or an icon on a tinted square for a file, a mail or a receipt. */
@Composable
private fun ImportTile(financeImport: FinanceImport, size: androidx.compose.ui.unit.Dp, iconSize: androidx.compose.ui.unit.Dp = 24.dp) {
    val picture = if (financeImport.isPicture) rememberServerPicture("/api/v1/agent/attachments/${financeImport.agentAttachmentId}") else null
    if (picture != null) {
        Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.size(size).clip(RoundedCornerShape(8.dp)))
    } else {
        val icon: ImageVector = when {
            !financeImport.agentAttachmentId.isNullOrEmpty() -> Icons.Outlined.Description
            financeImport.financeImportSource == "mail" -> Icons.Outlined.Mail
            else -> Icons.Outlined.ReceiptLong
        }
        Box(Modifier.size(size).clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.secondaryContainer), contentAlignment = Alignment.Center) {
            Icon(icon, contentDescription = null, Modifier.size(iconSize), tint = MaterialTheme.colorScheme.onSecondaryContainer)
        }
    }
}

/** One import: what came of it as the headline, then what it was read as and when it was shared. */
@Composable
internal fun FinanceImportRow(financeImport: FinanceImport, onClick: () -> Unit) {
    val isProblem = financeImport.isProblem
    val supportingText = listOf(kindText(financeImport), timeText(instantOf(financeImport.queuedAt))).filter { text -> text.isNotEmpty() }.joinToString(" · ")
    ListItem(
        modifier = Modifier.clickable(role = Role.Button, onClick = onClick),
        colors = ListItemDefaults.colors(containerColor = Color.Transparent),
        leadingContent = { ImportTile(financeImport, 40.dp) },
        headlineContent = {
            Text(headlineText(financeImport), maxLines = 2, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis, color = if (isProblem) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface)
        },
        supportingContent = { Text(supportingText) },
        trailingContent = when {
            financeImport.agentJobStatus == "running" -> {
                { CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) }
            }
            financeImport.agentJobStatus == "queued" -> {
                { Icon(Icons.Outlined.Schedule, contentDescription = stringResource(R.string.finance_import_status_queued), tint = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            isProblem -> {
                { Icon(Icons.Outlined.Error, contentDescription = stringResource(R.string.finance_import_status_dead), tint = MaterialTheme.colorScheme.error) }
            }
            else -> null
        },
    )
}

/** One import in full: the picture or file, what came of it, the details of where it came from, and what to do next. */
@Composable
fun FinanceImportDetailScreen(agentJobId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(connection, agentJobId) {
        var offset = 0
        var found: FinanceImport? = null
        while (found == null) {
            val page = connection.financeImports(limit = IMPORTS_PAGE_SIZE, offset = offset)
            val imports = page.financeImports.filterNotNull()
            found = imports.firstOrNull { financeImport -> financeImport.agentJobId == agentJobId }
            if (found != null || page.hasMore != true || imports.isEmpty()) break
            offset += imports.size
        }
        found ?: throw IllegalStateException(context.getString(R.string.finance_import_not_found))
    }
    val shown = read.value
    TeaNodeScreen(title = shown?.let { financeImport -> kindText(financeImport) }.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { financeImport ->
            val attachmentId = financeImport.agentAttachmentId.orEmpty()
            val mailboxItemId = financeImport.mailboxItemId.orEmpty()
            val fileName = financeImport.fileName.orEmpty().ifEmpty { stringResource(R.string.finance_import_unnamed) }
            val isPicture = financeImport.isPicture
            val outcomeText = financeImport.outcomeText.orEmpty().trim()
            val canReadAgain = attachmentId.isNotEmpty() && financeImport.isProblem && (financeImport.financeImportKind == "statement" || financeImport.financeImportKind == "tax_document")
            val statusText: String
            val statusIcon: ImageVector
            val isStatusProblem = financeImport.isProblem
            when {
                financeImport.isNothingImported -> { statusText = stringResource(R.string.finance_import_status_nothing_imported); statusIcon = Icons.Outlined.Error }
                financeImport.agentJobStatus == "dead" -> { statusText = stringResource(R.string.finance_import_status_dead); statusIcon = Icons.Outlined.Error }
                financeImport.agentJobStatus == "queued" -> { statusText = stringResource(R.string.finance_import_status_queued); statusIcon = Icons.Outlined.Schedule }
                financeImport.agentJobStatus == "running" -> { statusText = stringResource(R.string.finance_import_status_running); statusIcon = Icons.Outlined.Sync }
                financeImport.agentJobStatus == "cancelled" -> { statusText = stringResource(R.string.finance_import_status_cancelled); statusIcon = Icons.Outlined.Cancel }
                else -> { statusText = stringResource(R.string.finance_import_status_done); statusIcon = Icons.Outlined.CheckCircle }
            }
            val statusColor = if (isStatusProblem) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface
            val sourceText = when (financeImport.financeImportSource) {
                "upload" -> stringResource(R.string.finance_import_source_upload)
                "photo" -> stringResource(R.string.finance_import_source_photo)
                "mail" -> stringResource(R.string.finance_import_source_mail)
                else -> financeImport.financeImportSource.orEmpty()
            }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("file") {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                        if (isPicture) {
                            val picturePath = "/api/v1/agent/attachments/$attachmentId"
                            val picture = rememberServerPicture(picturePath)
                            var isPictureOpen by remember { mutableStateOf(false) }
                            if (picture != null) {
                                Image(
                                    picture, contentDescription = fileName, contentScale = ContentScale.Fit,
                                    modifier = Modifier.fillMaxWidth().heightIn(max = 360.dp).clip(RoundedCornerShape(12.dp)).clickable(role = Role.Button) { isPictureOpen = true },
                                )
                                if (isPictureOpen) PictureLightbox(picturePath, fileName, picturePath, onClose = { isPictureOpen = false })
                            } else {
                                Box(Modifier.fillMaxWidth().height(160.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp) }
                            }
                        } else {
                            ImportTile(financeImport, 96.dp, iconSize = 48.dp)
                            if (attachmentId.isNotEmpty()) Text(fileName, Modifier.padding(top = 8.dp), style = MaterialTheme.typography.bodyMedium)
                        }
                    }
                }
                item("outcome-header") { TeaNodeSectionHeader(stringResource(R.string.finance_import_what_came_of_it)) }
                item("outcome") {
                    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Icon(statusIcon, contentDescription = null, tint = if (isStatusProblem) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
                            Text(statusText, style = MaterialTheme.typography.titleMedium, color = statusColor)
                        }
                        val reasonText = financeImportReason(outcomeText)
                        if (reasonText.isNotEmpty()) Text(reasonText, style = MaterialTheme.typography.bodyLarge)
                    }
                }
                item("details-header") { TeaNodeSectionHeader(stringResource(R.string.finance_import_details)) }
                item("read-as") { LabeledValue(stringResource(R.string.finance_import_read_as), kindText(financeImport)) }
                if (sourceText.isNotEmpty()) item("from") { LabeledValue(stringResource(R.string.finance_import_from), sourceText) }
                if (attachmentId.isNotEmpty() || financeImport.fileName.orEmpty().isNotEmpty()) item("file-name") { LabeledValue(stringResource(R.string.finance_import_file), fileName) }
                dateTimeText(instantOf(financeImport.queuedAt)).takeIf { text -> text.isNotEmpty() }?.let { text -> item("received") { LabeledValue(stringResource(R.string.finance_import_received), text) } }
                dateTimeText(instantOf(financeImport.finishedAt)).takeIf { text -> text.isNotEmpty() }?.let { text -> item("finished") { LabeledValue(stringResource(R.string.finance_import_finished), text) } }
                if ((attachmentId.isNotEmpty() && !isPicture) || mailboxItemId.isNotEmpty() || canReadAgain) {
                    item("actions") {
                        Column(Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                            if (attachmentId.isNotEmpty() && !isPicture) {
                                TextButton(onClick = {
                                    scope.launch {
                                        try { openServerFile(context, connection, attachmentId) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                                    }
                                }) { Text(stringResource(R.string.finance_import_open_file)) }
                            }
                            if (mailboxItemId.isNotEmpty()) {
                                TextButton(onClick = { navigator.navigate(MailRoutes.thread(mailboxItemId, null)) }) { Text(stringResource(R.string.finance_import_open_message)) }
                            }
                            if (canReadAgain) {
                                TextButton(onClick = {
                                    scope.launch {
                                        try {
                                            if (financeImport.financeImportKind == "statement") connection.readStatement(attachmentId) else connection.readTaxDocumentOfAttachment(attachmentId)
                                            snackbar.show(context.getString(R.string.finance_import_reading_again))
                                            navigator.back()
                                        } catch (failure: CancellationException) {
                                            throw failure
                                        } catch (failure: Throwable) {
                                            snackbar.showError(failure)
                                        }
                                    }
                                }) { Text(stringResource(R.string.finance_import_read_again)) }
                            }
                        }
                    }
                }
            }
        }
    }
}

/** The outcome without the words its status already says: "Imported nothing: it shows no account number" reads "It shows no account number" under a status of Imported nothing. */
internal fun financeImportReason(outcomeText: String): String {
    for (prefix in listOf("Imported nothing: ", "Not read: ")) {
        if (outcomeText.startsWith(prefix)) return outcomeText.removePrefix(prefix).replaceFirstChar { letter -> letter.uppercaseChar() }
    }
    return outcomeText
}
