package com.teanode.app.shopping

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.shopping.ShoppingItemPrice
import com.teanode.kit.shopping.ShoppingList
import com.teanode.kit.shopping.ShoppingMoney
import com.teanode.kit.shopping.ShoppingPriceHistory
import com.teanode.kit.shopping.ShoppingQuantity
import com.teanode.kit.shopping.ShoppingReceipt
import com.teanode.kit.shopping.ShoppingReceiptEntry
import com.teanode.kit.shopping.ShoppingReceiptLine
import com.teanode.kit.shopping.ShoppingReceiptSections
import com.teanode.kit.shopping.addShoppingItemFromReceiptLine
import com.teanode.kit.shopping.decideShoppingReceiptMatch
import com.teanode.kit.shopping.matchShoppingReceipt
import com.teanode.kit.shopping.setShoppingItemOutOfStock
import com.teanode.kit.shopping.shoppingItemPrices
import com.teanode.kit.shopping.shoppingLists
import com.teanode.kit.shopping.shoppingReceipt
import com.teanode.kit.shopping.undoShoppingReceipt
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** What one receipt did to the lists: what it checked off, what it is not sure of, what was bought that is on no list, and what is still to buy. */
@Composable
internal fun ShoppingReceiptSheet(connection: Connection, financeReceiptId: String, onDismiss: () -> Unit, onChanged: () -> Unit) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val session = LocalAppSession.current
    val context = LocalContext.current
    val words = rememberShoppingWords()
    var receipt by remember { mutableStateOf<ShoppingReceipt?>(null) }
    var sections by remember { mutableStateOf<ShoppingReceiptSections?>(null) }
    var lists by remember { mutableStateOf<List<ShoppingList>>(emptyList()) }
    var previousPrices by remember { mutableStateOf<Map<String, ShoppingItemPrice>>(emptyMap()) }
    var loadFailure by remember { mutableStateOf<Throwable?>(null) }
    var isConfirmingUndo by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    var addingLine by remember { mutableStateOf<ShoppingReceiptLine?>(null) }

    suspend fun load() {
        try {
            val read = connection.shoppingReceipt(financeReceiptId)
            receipt = read
            val readSections = ShoppingReceiptSections(read)
            sections = readSections
            loadFailure = null
            if (lists.isEmpty()) lists = try { connection.shoppingLists() } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { emptyList() }
            val found = mutableMapOf<String, ShoppingItemPrice>()
            for (entry in readSections.appliedGroups.flatMap { group -> group.entries }.take(20)) {
                if (entry.match.shoppingItemPriceId.isEmpty()) continue
                val prices = try { connection.shoppingItemPrices(entry.item.id) } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { continue }
                ShoppingPriceHistory.previous(entry.match.shoppingItemPriceId, prices)?.let { previous -> found[entry.match.id] = previous }
            }
            previousPrices = found
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (receipt != null) snackbar.showError(failure) else loadFailure = failure
        }
    }
    LaunchedEffect(financeReceiptId) { load() }

    fun work(doneText: String?, call: suspend () -> Unit) {
        scope.launch {
            isWorking = true
            try {
                call()
                if (doneText != null) snackbar.show(doneText)
                load()
                onChanged()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            isWorking = false
        }
    }

    ShoppingSheet(stringResource(R.string.shopping_receipt), onDismiss, confirmLabel = stringResource(R.string.shopping_done), onConfirm = onDismiss) {
        val read = receipt
        val shown = sections
        if (read == null || shown == null) {
            val failure = loadFailure
            if (failure != null) {
                Text(session.describeLoadProblem(failure), color = MaterialTheme.colorScheme.error)
                OutlinedButton(onClick = { loadFailure = null; scope.launch { load() } }) { Text(stringResource(R.string.shopping_try_again)) }
            } else {
                Text(stringResource(R.string.shopping_loading))
            }
            return@ShoppingSheet
        }
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(read.merchantName.ifEmpty { stringResource(R.string.shopping_receipt) }, style = MaterialTheme.typography.titleMedium)
            val detail = listOfNotNull(
                read.purchasedOn.takeIf { day -> day.isNotEmpty() },
                ShoppingMoney.shown(read.totalAmount, read.currencyCode).takeIf { total -> total.isNotEmpty() },
            ).joinToString(" · ")
            if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        for (group in shown.appliedGroups) {
            SheetHeading(stringResource(R.string.shopping_checked_off_on, group.entries.size, group.listName))
            for (entry in group.entries) AppliedRow(entry, read, previousPrices[entry.match.id])
        }
        if (shown.appliedGroups.isEmpty() && shown.proposed.isEmpty()) {
            Text(stringResource(R.string.shopping_receipt_nothing), color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = { work(context.getString(R.string.shopping_receipt_matching)) { connection.matchShoppingReceipt(financeReceiptId) } }, enabled = !isWorking) {
                Text(stringResource(R.string.shopping_receipt_match_again))
            }
        }
        if (shown.proposed.isNotEmpty()) {
            SheetHeading(stringResource(R.string.shopping_receipt_proposed))
            for (entry in shown.proposed) ProposedRow(
                entry, read, isWorking,
                onApply = { work(context.getString(R.string.shopping_applied)) { connection.decideShoppingReceiptMatch(entry.match.id, true) } },
                onDismissMatch = { work(context.getString(R.string.shopping_dismissed)) { connection.decideShoppingReceiptMatch(entry.match.id, false) } },
            )
        }
        if (shown.notOnList.isNotEmpty()) {
            SheetHeading(stringResource(R.string.shopping_receipt_not_on_list))
            for (line in shown.notOnList) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(line.description, style = MaterialTheme.typography.bodyMedium)
                        Text(ShoppingMoney.shown(line.lineAmount, read.currencyCode), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Column {
                        TextButton(onClick = { addingLine = line }, enabled = !isWorking && lists.isNotEmpty()) { Text(stringResource(R.string.shopping_receipt_add_to_list)) }
                        DropdownMenu(addingLine?.financeReceiptLineId == line.financeReceiptLineId, { addingLine = null }) {
                            for (list in lists.filter { each -> each.isOwner || true }) {
                                DropdownMenuItem(
                                    text = { Text(list.listName) },
                                    onClick = {
                                        addingLine = null
                                        work(context.getString(R.string.shopping_added_to, list.listName)) { connection.addShoppingItemFromReceiptLine(line.financeReceiptLineId, list.id) }
                                    },
                                )
                            }
                        }
                    }
                }
            }
        }
        if (shown.stillNeeded.isNotEmpty()) {
            SheetHeading(stringResource(R.string.shopping_receipt_still_needed))
            for (item in shown.stillNeeded) {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    ShoppingPictureView(item)
                    Column(Modifier.weight(1f)) {
                        Text(item.itemDescription)
                        Text(words.listItemDetail(item), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    TextButton(
                        onClick = { work(context.getString(R.string.shopping_marked_out_of_stock)) { connection.setShoppingItemOutOfStock(item.id, true, read.merchantName) } },
                        enabled = !isWorking,
                    ) { Text(stringResource(R.string.shopping_out_of_stock_action)) }
                }
            }
        }
        if (shown.canUndo) {
            HorizontalDivider()
            OutlinedButton(onClick = { isConfirmingUndo = true }, enabled = !isWorking, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.shopping_receipt_undo), color = MaterialTheme.colorScheme.error)
            }
        }
    }
    if (isConfirmingUndo) {
        ConfirmationDialog(
            title = stringResource(R.string.shopping_receipt_undo_question), confirmLabel = stringResource(R.string.shopping_undo), isDestructive = true,
            onConfirm = { isConfirmingUndo = false; work(context.getString(R.string.shopping_receipt_undone)) { connection.undoShoppingReceipt(financeReceiptId) } },
            onDismiss = { isConfirmingUndo = false },
        )
    }
}

@Composable
private fun AppliedRow(entry: ShoppingReceiptEntry, receipt: ShoppingReceipt, previous: ShoppingItemPrice?) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        ShoppingPictureView(entry.item)
        Column(Modifier.weight(1f)) {
            Text(entry.item.itemDescription)
            Text(entry.line.description, style = MaterialTheme.typography.bodySmall, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Column(horizontalAlignment = Alignment.End) {
            Text(ShoppingMoney.shown(entry.line.lineAmount, receipt.currencyCode))
            // "was $1.19": what the item cost before this receipt, when that is another price.
            if (previous != null && previous.amount?.compareTo(ShoppingQuantity.decimal(entry.item.unitPriceAmount)) != 0) {
                Text(stringResource(R.string.shopping_price_was, previous.shownAmount), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun ProposedRow(entry: ShoppingReceiptEntry, receipt: ShoppingReceipt, isWorking: Boolean, onApply: () -> Unit, onDismissMatch: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            ShoppingPictureView(entry.item)
            Column(Modifier.weight(1f)) {
                Text(entry.item.itemDescription)
                Text(entry.line.description, style = MaterialTheme.typography.bodySmall, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(ShoppingMoney.shown(entry.line.lineAmount, receipt.currencyCode))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(onClick = onApply, enabled = !isWorking) { Text(stringResource(R.string.shopping_receipt_apply)) }
            OutlinedButton(onClick = onDismissMatch, enabled = !isWorking) { Text(stringResource(R.string.shopping_receipt_dismiss)) }
        }
    }
}

/** The sheet of what a receipt did to the shopping lists, for the Finance screens that show a receipt. */
@Composable
fun ShoppingReceiptSheetOf(connection: Connection, financeReceiptId: String, onDismiss: () -> Unit) {
    ShoppingReceiptSheet(connection, financeReceiptId, onDismiss, onChanged = {})
}
