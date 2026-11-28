package com.teanode.app.finance

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.FinanceImport
import com.teanode.kit.finance.financeImports
import kotlinx.coroutines.CancellationException

private const val IMPORTS_PAGE_SIZE = 50

/** The files read into finance in the background (tax documents, statements, receipts), newest first, fifty at a time. */
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
            // A refresh brings a new first page, and the later pages read after the old one are dropped with it.
            var laterImports by remember(page) { mutableStateOf<List<FinanceImport>>(emptyList()) }
            var isLastPageRead by remember(page) { mutableStateOf(page.hasMore != true) }
            val imports = (page.financeImports.filterNotNull() + laterImports).distinctBy { financeImport -> financeImport.agentJobId }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                items(imports, key = { financeImport -> financeImport.agentJobId }) { financeImport ->
                    FinanceImportRow(financeImport)
                    // The last one on screen reads the next page, if there is one.
                    if (financeImport.agentJobId == imports.last().agentJobId && !isLastPageRead) {
                        LaunchedEffect(imports.size) {
                            try {
                                val next = connection.financeImports(limit = IMPORTS_PAGE_SIZE, offset = imports.size)
                                laterImports = laterImports + next.financeImports.filterNotNull()
                                isLastPageRead = next.hasMore != true || next.financeImports.isEmpty()
                            } catch (failure: CancellationException) {
                                throw failure
                            } catch (failure: Throwable) {
                                snackbar.showError(failure)
                            }
                        }
                    }
                }
            }
        }
    }
}

/** One file: its name, what it was read as and where it came from, the status with its date, and the outcome whole. */
@Composable
internal fun FinanceImportRow(financeImport: FinanceImport) {
    val kindText = when (financeImport.financeImportKind) {
        "tax_document" -> stringResource(R.string.finance_import_kind_tax_document)
        "statement" -> stringResource(R.string.finance_import_kind_statement)
        "receipt" -> stringResource(R.string.finance_import_kind_receipt)
        else -> financeImport.financeImportKind.orEmpty()
    }
    val sourceText = when (financeImport.financeImportSource) {
        "upload" -> stringResource(R.string.finance_import_source_upload)
        "photo" -> stringResource(R.string.finance_import_source_photo)
        "mail" -> stringResource(R.string.finance_import_source_mail)
        else -> financeImport.financeImportSource.orEmpty()
    }
    val statusText = when (financeImport.agentJobStatus) {
        "queued" -> stringResource(R.string.finance_import_status_queued)
        "running" -> stringResource(R.string.finance_import_status_running)
        "done" -> stringResource(R.string.finance_import_status_done)
        "dead" -> stringResource(R.string.finance_import_status_dead)
        "cancelled" -> stringResource(R.string.finance_import_status_cancelled)
        else -> financeImport.agentJobStatus.orEmpty()
    }
    val isFailed = financeImport.agentJobStatus == "dead"
    // A finished file shows when it finished; one still waiting or reading shows when it was queued.
    val dateText = shownImportDate(financeImport.finishedAt?.takeIf { text -> text.isNotEmpty() } ?: financeImport.queuedAt)
    val kindAndSource = listOf(kindText, sourceText).filter { text -> text.isNotEmpty() }.joinToString(" · ")
    val statusAndDate = listOf(statusText, dateText).filter { text -> text.isNotEmpty() }.joinToString(" · ")
    val outcomeText = financeImport.outcomeText.orEmpty().trim()
    ListItem(
        headlineContent = { Text(financeImport.fileName.orEmpty().ifEmpty { stringResource(R.string.finance_import_unnamed) }) },
        supportingContent = {
            Column {
                if (kindAndSource.isNotEmpty()) Text(kindAndSource)
                Text(statusAndDate, color = if (isFailed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
                if (outcomeText.isNotEmpty()) Text(outcomeText, Modifier.padding(top = 4.dp), color = MaterialTheme.colorScheme.onSurface)
            }
        },
    )
}

/** An RFC 3339 time as the person's phone writes a date ("Oct 14, 2030"); empty when it does not read. */
private fun shownImportDate(text: String?): String {
    val instant = try {
        java.time.Instant.parse(text.orEmpty())
    } catch (_: Exception) {
        return ""
    }
    return java.text.DateFormat.getDateInstance(java.text.DateFormat.MEDIUM).format(java.util.Date.from(instant))
}
