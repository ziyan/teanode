package com.teanode.app.finance

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.ReceiptLong
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material.icons.outlined.QuestionAnswer
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
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shopping.ShoppingSheet
import com.teanode.app.shopping.readPendingFile
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.Connection
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceReceipt
import com.teanode.kit.finance.FinanceTransaction
import com.teanode.kit.finance.FinanceTransactionFilter
import com.teanode.kit.finance.annotate
import com.teanode.kit.finance.categorize
import com.teanode.kit.finance.count
import com.teanode.kit.finance.financeReceipts
import com.teanode.kit.finance.financeTransactions
import com.teanode.kit.finance.isDuplicate
import com.teanode.kit.finance.readReceipt
import com.teanode.kit.finance.shownName
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private const val PAGE_SIZE = 50

/** The transactions, narrowed to a filter, a page at a time. */
@Composable
fun FinanceTransactionsScreen(initialFilter: FinanceTransactionFilter) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var filter by remember { mutableStateOf(initialFilter) }
    var base by remember { mutableStateOf<FinanceBase?>(null) }
    var rows by remember { mutableStateOf<List<FinanceTransaction>>(emptyList()) }
    var total by remember { mutableStateOf(0) }
    var leftOut by remember { mutableStateOf(0) }
    var hasLoaded by remember { mutableStateOf(false) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var isMoreLoading by remember { mutableStateOf(false) }
    var isFiltering by remember { mutableStateOf(false) }

    suspend fun load(isMore: Boolean) {
        try {
            if (base == null) base = connection.readFinanceBase(context)
            val page = connection.financeTransactions(filter, limit = PAGE_SIZE, offset = if (isMore) rows.size else 0)
            val read = page.financeTransactions.filterNotNull()
            rows = if (isMore) rows + read else read
            total = page.totalCount ?: rows.size
            leftOut = page.leftOutDuplicateCount ?: 0
            hasLoaded = true
            failure = null
        } catch (error: CancellationException) {
            throw error
        } catch (error: Throwable) {
            if (hasLoaded) snackbar.showError(error) else failure = error
        }
    }
    LaunchedEffect(filter) { hasLoaded = false; load(false) }

    TeaNodeScreen(
        title = stringResource(R.string.finance_part_transactions), onBack = { navigator.back() },
        actions = {
            IconButton(onClick = { isFiltering = true }) {
                Icon(Icons.Outlined.FilterList, contentDescription = stringResource(R.string.finance_filter), tint = if (filter.isNarrowing) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            val problem = failure
            when {
                problem != null && !hasLoaded -> ErrorState(session.describeLoadProblem(problem), onRetry = { failure = null; scope.launch { load(false) } })
                !hasLoaded -> LoadingState()
                rows.isEmpty() -> EmptyState(Icons.Outlined.ViewList, stringResource(R.string.finance_no_transactions))
                else -> LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                    items(rows, key = { row -> row.id }) { row -> TransactionRow(row, base) { navigator.navigate(FinanceRoutes.transaction(row.id)) } }
                    if (rows.size < total) {
                        item("more") {
                            Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                                OutlinedButton(onClick = { scope.launch { isMoreLoading = true; load(true); isMoreLoading = false } }, enabled = !isMoreLoading) { Text(stringResource(R.string.finance_load_more)) }
                            }
                        }
                    }
                    if (leftOut > 0 && !filter.isDuplicateIncluded) {
                        item("left-out") { Footnote(context.resources.getQuantityString(R.plurals.finance_duplicates_left_out, leftOut, leftOut)) }
                    }
                }
            }
        }
    }
    if (isFiltering) {
        TransactionFilterSheet(base, filter, onDismiss = { isFiltering = false }, onApply = { filter = it; isFiltering = false })
    }
}

@Composable
internal fun TransactionRow(transaction: FinanceTransaction, base: FinanceBase?, onClick: () -> Unit) {
    val category = base?.categoryName(transaction.spendingCategoryId) ?: ""
    TeaNodeRow(
        headline = transaction.shownName,
        supporting = listOfNotNull(transaction.postedOn, category.takeIf { text -> text.isNotEmpty() },
            stringResource(R.string.finance_pending).takeIf { transaction.isPending == true },
            stringResource(R.string.finance_duplicate).takeIf { transaction.isDuplicate },
        ).joinToString(" · "),
        trailing = {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if ((transaction.receiptCount ?: 0) > 0) Icon(Icons.Outlined.ReceiptLong, contentDescription = stringResource(R.string.finance_has_receipt), modifier = Modifier.padding(end = 2.dp))
                AmountText(transaction.amount, transaction.currencyCode)
            }
        },
        onClick = onClick,
    )
}

/** What the transaction list is narrowed to. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun TransactionFilterSheet(base: FinanceBase?, initial: FinanceTransactionFilter, onDismiss: () -> Unit, onApply: (FinanceTransactionFilter) -> Unit) {
    var draft by remember { mutableStateOf(initial) }
    var isAccountOpen by remember { mutableStateOf(false) }
    var isCategoryOpen by remember { mutableStateOf(false) }
    ShoppingSheet(stringResource(R.string.finance_filter), onDismiss, confirmLabel = stringResource(R.string.finance_apply), onConfirm = { onApply(draft) }) {
        OutlinedTextField(draft.text, { draft = draft.copy(text = it) }, label = { Text(stringResource(R.string.finance_search_words)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(draft.from, { draft = draft.copy(from = it) }, label = { Text(stringResource(R.string.finance_from_day)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(draft.to, { draft = draft.copy(to = it) }, label = { Text(stringResource(R.string.finance_to_day)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        ExposedDropdownMenuBox(isAccountOpen, { isAccountOpen = it }) {
            OutlinedTextField(
                value = base?.account(draft.financeAccountId)?.shownNameText() ?: stringResource(R.string.finance_all_accounts), onValueChange = {}, readOnly = true,
                label = { Text(stringResource(R.string.finance_account)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isAccountOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
            )
            ExposedDropdownMenu(isAccountOpen, { isAccountOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.finance_all_accounts)) }, onClick = { draft = draft.copy(financeAccountId = ""); isAccountOpen = false })
                for (account in base?.accounts.orEmpty()) DropdownMenuItem(text = { Text(account.shownNameText()) }, onClick = { draft = draft.copy(financeAccountId = account.id); isAccountOpen = false })
            }
        }
        ExposedDropdownMenuBox(isCategoryOpen, { isCategoryOpen = it }) {
            OutlinedTextField(
                value = if (draft.spendingCategoryId.isEmpty()) stringResource(R.string.finance_all_categories) else base?.categoryName(draft.spendingCategoryId).orEmpty(), onValueChange = {}, readOnly = true,
                label = { Text(stringResource(R.string.finance_category)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isCategoryOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
            )
            ExposedDropdownMenu(isCategoryOpen, { isCategoryOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.finance_all_categories)) }, onClick = { draft = draft.copy(spendingCategoryId = ""); isCategoryOpen = false })
                for (category in base?.pickable(draft.spendingCategoryId).orEmpty()) DropdownMenuItem(text = { Text(base!!.label(category)) }, onClick = { draft = draft.copy(spendingCategoryId = category.id); isCategoryOpen = false })
            }
        }
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.finance_only_uncategorized), Modifier.weight(1f))
            Switch(draft.isUncategorized, { draft = draft.copy(isUncategorized = it) })
        }
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.finance_show_duplicates), Modifier.weight(1f))
            Switch(draft.isDuplicateIncluded, { draft = draft.copy(isDuplicateIncluded = it) })
        }
        TextButton(onClick = { draft = FinanceTransactionFilter() }) { Text(stringResource(R.string.finance_clear), color = MaterialTheme.colorScheme.error) }
    }
}

private fun com.teanode.kit.finance.FinanceAccount.shownNameText(): String = shownName

/** One transaction: what it was, its category (and a rule for the merchant), a note, its duplicates and its receipts. */
@Composable
fun FinanceTransactionDetailScreen(transactionId: String) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var base by remember { mutableStateOf<FinanceBase?>(null) }
    var transaction by remember { mutableStateOf<FinanceTransaction?>(null) }
    var duplicates by remember { mutableStateOf<List<FinanceTransaction>>(emptyList()) }
    var receipts by remember { mutableStateOf<List<FinanceReceipt>>(emptyList()) }
    var annotation by remember { mutableStateOf("") }
    var categoryId by remember { mutableStateOf("") }
    var madeRuleFor by remember { mutableStateOf<String?>(null) }
    var readingText by remember { mutableStateOf<String?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var isCategoryOpen by remember { mutableStateOf(false) }

    suspend fun load() {
        try {
            if (base == null) base = connection.readFinanceBase(context)
            val read = connection.financeTransactions(FinanceTransactionFilter(), ids = listOf(transactionId), limit = 1).financeTransactions.firstOrNull() ?: return
            transaction = read
            annotation = read.annotation ?: ""
            categoryId = read.spendingCategoryId ?: ""
            if (!read.isDuplicate) duplicates = connection.financeTransactions(FinanceTransactionFilter(), duplicatesOf = read.id, limit = 200).financeTransactions.filterNotNull()
            receipts = connection.financeReceipts(transactionId = read.id, limit = 200).financeReceipts.filterNotNull()
            failure = null
        } catch (error: CancellationException) {
            throw error
        } catch (error: Throwable) {
            if (transaction != null) snackbar.showError(error) else failure = error
        }
    }
    LaunchedEffect(transactionId) { load() }

    fun act(saying: String, work: suspend () -> Unit) {
        scope.launch {
            try {
                work()
                snackbar.show(saying)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Throwable) {
                snackbar.showError(error)
            }
            load()
        }
    }

    val photoLauncher = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            readingText = context.getString(R.string.finance_reading_receipt)
            try {
                val file = context.readPendingFile(uri, "receipt.jpg")
                val attachmentId = connection.upload(file.name, file.contentType, file.data)
                connection.readReceipt(attachmentId = attachmentId, transactionId = transactionId)
                repeat(30) {
                    delay(4_000)
                    val found = try { connection.financeReceipts(transactionId = transactionId, limit = 200).financeReceipts.filterNotNull() } catch (error: CancellationException) { throw error } catch (_: Throwable) { receipts }
                    if (found.size > receipts.size) {
                        receipts = found
                        readingText = null
                        return@launch
                    }
                }
                readingText = null
                snackbar.show(context.getString(R.string.finance_still_reading))
            } catch (error: CancellationException) {
                throw error
            } catch (error: Throwable) {
                readingText = null
                snackbar.showError(error)
            }
        }
    }

    TeaNodeScreen(
        title = transaction?.shownName.orEmpty(), onBack = { navigator.back() },
        actions = {
            transaction?.let { asked ->
                androidx.compose.material3.IconButton(onClick = {
                    com.teanode.app.chats.ChatsHooks.askAbout(
                        navigator,
                        com.teanode.kit.chats.AgentReference.transaction(asked.id, asked.postedOn, asked.amount, asked.currencyCode, asked.merchantName, asked.description),
                    )
                }) { androidx.compose.material3.Icon(androidx.compose.material.icons.Icons.Outlined.QuestionAnswer, contentDescription = stringResource(R.string.chats_ask_about_this)) }
            }
        },
    ) { padding ->
        val shown = transaction
        val loadedBase = base
        val problem = failure
        when {
            shown == null && problem != null -> ErrorState(session.describeLoadProblem(problem), onRetry = { failure = null; scope.launch { load() } }, modifier = Modifier.padding(padding))
            shown == null || loadedBase == null -> LoadingState(Modifier.padding(padding))
            else -> LazyColumn(Modifier.fillMaxSize().padding(padding), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("amount") { LabeledValue(stringResource(R.string.finance_amount), FinanceMoney.shown(shown.amount, shown.currencyCode)) }
                item("posted") { LabeledValue(stringResource(R.string.finance_posted), shown.postedOn ?: "") }
                loadedBase.account(shown.financeAccountId)?.let { account -> item("account") { LabeledValue(stringResource(R.string.finance_account), account.shownName) } }
                if (!shown.description.isNullOrEmpty()) item("description") { Footnote(shown.description!!) }
                if (shown.isPending == true) item("pending") { Footnote(stringResource(R.string.finance_pending)) }
                item("category") {
                    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        @OptIn(ExperimentalMaterial3Api::class)
                        ExposedDropdownMenuBox(isCategoryOpen, { isCategoryOpen = it }) {
                            OutlinedTextField(
                                value = if (categoryId.isEmpty()) stringResource(R.string.finance_uncategorized) else loadedBase.categoryName(categoryId), onValueChange = {}, readOnly = true,
                                label = { Text(stringResource(R.string.finance_category)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isCategoryOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
                            )
                            ExposedDropdownMenu(isCategoryOpen, { isCategoryOpen = false }) {
                                for (category in loadedBase.pickable(categoryId)) {
                                    DropdownMenuItem(
                                        text = { Text(loadedBase.label(category)) },
                                        onClick = {
                                            isCategoryOpen = false
                                            categoryId = category.id
                                            if (category.id != (shown.spendingCategoryId ?: "")) act(context.getString(R.string.finance_filed_under, loadedBase.categoryName(category.id))) { connection.categorize(transactionId, category.id, false) }
                                        },
                                    )
                                }
                            }
                        }
                        val by = shown.categorizedBy
                        if (!by.isNullOrEmpty() && by != "person") Text(FinanceCategoryNames.filedBy(context, by), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (categoryId.isNotEmpty() && madeRuleFor == null) {
                            TextButton(onClick = {
                                scope.launch {
                                    try {
                                        madeRuleFor = connection.categorize(transactionId, categoryId, true) ?: ""
                                        snackbar.show(context.getString(R.string.finance_rule_made))
                                    } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
                                    load()
                                }
                            }) { Text(stringResource(R.string.finance_always_for_merchant)) }
                        }
                        madeRuleFor?.let { words -> Text(stringResource(R.string.finance_rule_files_here, words), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        if (shown.isDuplicate) Text(stringResource(R.string.finance_duplicate_category_not_counted), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                item("note-header") { TeaNodeSectionHeader(stringResource(R.string.finance_note)) }
                item("note") {
                    Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        OutlinedTextField(annotation, { annotation = it }, label = { Text(stringResource(R.string.finance_note_hint)) }, modifier = Modifier.fillMaxWidth())
                        Row {
                            if (annotation.trim() != (shown.annotation ?: "")) {
                                TextButton(onClick = { act(context.getString(R.string.finance_note_saved)) { connection.annotate(transactionId, annotation.trim()) } }) { Text(stringResource(R.string.finance_save_note)) }
                            }
                            if (!shown.annotation.isNullOrEmpty()) {
                                TextButton(onClick = { act(context.getString(R.string.finance_note_cleared)) { connection.annotate(transactionId, "") } }) { Text(stringResource(R.string.finance_clear_note), color = MaterialTheme.colorScheme.error) }
                            }
                        }
                    }
                }
                if (shown.isDuplicate) {
                    item("duplicate") {
                        Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(stringResource(R.string.finance_not_counted_duplicate))
                            Button(onClick = { act(context.getString(R.string.finance_counted)) { connection.count(transactionId, false) } }) { Text(stringResource(R.string.finance_count_this_one)) }
                            if (shown.duplicateDecidedBy == "person") TextButton(onClick = { act(context.getString(R.string.finance_detection_decides)) { connection.count(transactionId, true) } }) { Text(stringResource(R.string.finance_let_detection_decide)) }
                        }
                    }
                } else if (duplicates.isNotEmpty()) {
                    item("dups-header") { TeaNodeSectionHeader(stringResource(R.string.finance_duplicates_not_counted)) }
                    items(duplicates, key = { row -> "dup-" + row.id }) { duplicate -> TransactionRow(duplicate, loadedBase) { navigator.navigate(FinanceRoutes.transaction(duplicate.id)) } }
                }
                item("receipts-header") { TeaNodeSectionHeader(stringResource(R.string.finance_receipts)) }
                items(receipts, key = { receipt -> "receipt-" + receipt.id }) { receipt ->
                    TeaNodeRow(
                        headline = receipt.merchantName.orEmpty().ifEmpty { stringResource(R.string.finance_receipt) },
                        supporting = listOfNotNull(receipt.purchasedOn, FinanceMoney.shown(receipt.totalAmount, receipt.currencyCode)).joinToString(" · "),
                        onClick = { navigator.navigate(FinanceRoutes.receipt(receipt.id)) },
                    )
                }
                readingText?.let { text -> item("reading") { Footnote(text) } }
                item("add-receipt") {
                    val isMoneyIn = (FinanceMoney.decimal(shown.amount)?.signum() ?: 0) > 0
                    Column(Modifier.padding(horizontal = 16.dp)) {
                        OutlinedButton(onClick = { photoLauncher.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }, enabled = !isMoneyIn) { Text(stringResource(R.string.finance_add_receipt)) }
                        if (isMoneyIn) Footnote(stringResource(R.string.finance_receipts_money_out))
                    }
                }
            }
        }
    }
}
