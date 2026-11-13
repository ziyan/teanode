package com.teanode.app.finance

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ReceiptLong
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceReceipt
import com.teanode.kit.finance.FinanceTransaction
import com.teanode.kit.finance.FinanceTransactionFilter
import com.teanode.kit.finance.deleteReceipt
import com.teanode.kit.finance.financeReceipts
import com.teanode.kit.finance.financeTransactions
import com.teanode.kit.finance.matchReceipt
import com.teanode.kit.finance.proposeReceiptMatches
import com.teanode.kit.finance.shownName
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** The receipts, newest first, or only those matched to no charge. */
@Composable
fun FinanceReceiptsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    var isUnmatchedOnly by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, isUnmatchedOnly) { connection.financeReceipts(isUnmatched = isUnmatchedOnly, limit = 200).financeReceipts.filterNotNull() }
    TeaNodeScreen(
        title = stringResource(R.string.finance_part_receipts), onBack = { navigator.back() },
        actions = { androidx.compose.material3.IconButton(onClick = { navigator.navigate(FinanceRoutes.receiptEntry) }) { androidx.compose.material3.Icon(androidx.compose.material.icons.Icons.Outlined.Add, contentDescription = stringResource(R.string.finance_entry_receipt_title)) } },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.finance_only_unmatched), Modifier.weight(1f)); Switch(isUnmatchedOnly, { isUnmatchedOnly = it })
            }
            LoadedContent(
                read, isEmpty = { list -> list.isEmpty() },
                empty = { EmptyState(Icons.Outlined.ReceiptLong, stringResource(R.string.finance_no_receipts), description = stringResource(R.string.finance_no_receipts_description)) },
            ) { list ->
                LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                    items(list, key = { receipt -> receipt.id }) { receipt -> ReceiptRow(receipt) { navigator.navigate(FinanceRoutes.receipt(receipt.id)) } }
                }
            }
        }
    }
}

@Composable
internal fun ReceiptRow(receipt: FinanceReceipt, onClick: () -> Unit) {
    val lineCount = receipt.receiptLines.orEmpty().filterNotNull().size
    TeaNodeRow(
        headline = receipt.merchantName.orEmpty().ifEmpty { stringResource(R.string.finance_receipt) },
        supporting = listOfNotNull(receipt.purchasedOn, androidx.compose.ui.platform.LocalContext.current.resources.getQuantityString(R.plurals.finance_line_count, lineCount, lineCount)).joinToString(" · "),
        trailing = { Text(FinanceMoney.shown(receipt.totalAmount, receipt.currencyCode)) },
        onClick = onClick,
    )
}

/** One receipt: what it says, whether its lines add up, the charge it is matched to, and what it did to the shopping lists. */
@Composable
fun FinanceReceiptDetailScreen(receiptId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isShowingShopping by remember { mutableStateOf(false) }
    var isMatching by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, receiptId) {
        val receipt = connection.financeReceipts(limit = 200).financeReceipts.filterNotNull().first { each -> each.id == receiptId }
        val matched = receipt.receiptMatches.orEmpty().filterNotNull().mapNotNull { match ->
            connection.financeTransactions(FinanceTransactionFilter(), ids = listOf(match.financeTransactionId), limit = 1).financeTransactions.firstOrNull()
        }
        receipt to matched
    }
    TeaNodeScreen(title = read.value?.first?.merchantName.orEmpty().ifEmpty { stringResource(R.string.finance_receipt) }, onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { (receipt, matched) ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("merchant") { LabeledValue(stringResource(R.string.finance_merchant), receipt.merchantName.orEmpty()) }
                receipt.purchasedOn?.takeIf { day -> day.isNotEmpty() }?.let { day -> item("day") { LabeledValue(stringResource(R.string.finance_bought_on), day) } }
                receipt.merchantReceiptNumber?.takeIf { number -> number.isNotEmpty() }?.let { number -> item("number") { LabeledValue(stringResource(R.string.finance_receipt_number), number) } }
                receipt.subtotalAmount?.takeIf { amount -> amount.isNotEmpty() }?.let { amount -> item("subtotal") { LabeledValue(stringResource(R.string.finance_subtotal), FinanceMoney.shown(amount, receipt.currencyCode)) } }
                item("total") { LabeledValue(stringResource(R.string.finance_total), FinanceMoney.shown(receipt.totalAmount, receipt.currencyCode)) }
                item("check") {
                    if (receipt.receiptCheckState == "balanced") Footnote(stringResource(R.string.finance_lines_add_up))
                    else if (receipt.receiptCheckState == "unbalanced") Footnote(stringResource(R.string.finance_lines_off_by, FinanceMoney.shown(receipt.checkDifferenceAmount, receipt.currencyCode)))
                }
                val lines = receipt.receiptLines.orEmpty().filterNotNull().sortedBy { line -> line.lineNumber ?: 0 }
                if (lines.isNotEmpty()) {
                    item("lines-header") { TeaNodeSectionHeader(stringResource(R.string.finance_lines)) }
                    for (line in lines) {
                        item("line-" + line.id) {
                            TeaNodeRow(
                                line.description.orEmpty().ifEmpty { line.receiptLineKind.orEmpty() },
                                supporting = listOfNotNull(line.receiptLineKind?.takeIf { kind -> kind != "item" }, line.quantity?.takeIf { quantity -> quantity.isNotEmpty() }?.let { quantity -> "$quantity ${line.quantityUnit.orEmpty()}".trim() }).joinToString(" · ").ifEmpty { null },
                                trailing = { Text(FinanceMoney.shown(line.lineAmount, receipt.currencyCode)) },
                            )
                        }
                    }
                }
                item("matched-header") { TeaNodeSectionHeader(stringResource(R.string.finance_matched_charge)) }
                if (matched.isEmpty()) item("unmatched") { Footnote(stringResource(R.string.finance_not_matched_yet)) }
                for (transaction in matched) item("match-" + transaction.id) { TransactionRow(transaction, null) { navigator.navigate(FinanceRoutes.transaction(transaction.id)) } }
                item("match-actions") {
                    Row(Modifier.padding(horizontal = 8.dp)) {
                        if (matched.isEmpty()) TextButton(onClick = { isMatching = true }) { Text(stringResource(R.string.finance_match_to_charge)) }
                        for (transaction in matched) {
                            TextButton(onClick = { scope.launch { try { connection.matchReceipt(receiptId, transaction.id, null, isUndone = true); snackbar.show(context.getString(R.string.finance_taken_off)) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() } }) { Text(stringResource(R.string.finance_take_off_charge)) }
                        }
                    }
                }
                item("shopping-header") { TeaNodeSectionHeader(stringResource(R.string.finance_shopping_lists)) }
                item("shopping") {
                    Column(Modifier.padding(horizontal = 16.dp)) {
                        OutlinedButton(onClick = { isShowingShopping = true }) { Text(stringResource(R.string.finance_see_what_it_did)) }
                        Footnote(stringResource(R.string.finance_matched_against_lists))
                    }
                }
                item("delete") { TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_delete_receipt), color = MaterialTheme.colorScheme.error) } }
            }
            if (isConfirmingDelete) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_delete_receipt_question), text = stringResource(R.string.finance_delete_receipt_message), confirmLabel = stringResource(R.string.finance_delete_receipt), isDestructive = true,
                    onConfirm = { isConfirmingDelete = false; scope.launch { try { connection.deleteReceipt(receiptId); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
                    onDismiss = { isConfirmingDelete = false },
                )
            }
            if (isShowingShopping) com.teanode.app.shopping.ShoppingReceiptSheetOf(connection, receiptId) { isShowingShopping = false }
            if (isMatching) MatchReceiptSheet(receiptId, onDismiss = { isMatching = false }, onMatched = { isMatching = false; read.refresh() })
        }
    }
}

/** The charges a receipt could belong to, nearest first. */
@Composable
private fun MatchReceiptSheet(receiptId: String, onDismiss: () -> Unit, onMatched: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val proposals = rememberLoad(connection, receiptId) { connection.proposeReceiptMatches(receiptId) }
    com.teanode.app.shopping.ShoppingSheet(stringResource(R.string.finance_match_to_charge), onDismiss) {
        val read = proposals.value
        if (read == null) Text(stringResource(R.string.finance_loading))
        else if (read.isEmpty()) Text(stringResource(R.string.finance_no_candidates), color = MaterialTheme.colorScheme.onSurfaceVariant)
        else for (proposal in read) {
            val transaction: FinanceTransaction? = proposal.financeTransaction
            TeaNodeRow(
                transaction?.shownName.orEmpty(), supporting = listOfNotNull(transaction?.postedOn, if (proposal.isExactAmount == true) stringResource(R.string.finance_exact_amount) else null).joinToString(" · "),
                trailing = { AmountText(transaction?.amount, transaction?.currencyCode) },
                onClick = { scope.launch { try { connection.matchReceipt(receiptId, proposal.financeTransactionId, proposal.matchedAmount); onMatched() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
            )
        }
    }
}
