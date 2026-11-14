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
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shopping.ShoppingSheet
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.Connection
import com.teanode.kit.finance.FinanceAccount
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceTransactionFilter
import com.teanode.kit.finance.SpendingRuleProposals
import com.teanode.kit.finance.TransactionRowsFile
import com.teanode.kit.finance.TransactionRowsPreview
import com.teanode.kit.finance.deleteStatementAccount
import com.teanode.kit.finance.financeProviderKinds
import com.teanode.kit.finance.financeTransactions
import com.teanode.kit.finance.id
import com.teanode.kit.finance.importFinanceCredential
import com.teanode.kit.finance.importTransactions
import com.teanode.kit.finance.newRows
import com.teanode.kit.finance.presentRows
import com.teanode.kit.finance.previewImportTransactions
import com.teanode.kit.finance.proposals
import com.teanode.kit.finance.proposeSpendingRules
import com.teanode.kit.finance.renameStatementAccount
import com.teanode.kit.finance.saveSpendingRule
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement

/** Gives an account of imported statements the person's own name and last digits. */
@Composable
internal fun RenameStatementAccountDialog(account: FinanceAccount, onDismiss: () -> Unit, onDone: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(account.accountName.orEmpty()) }
    var mask by remember { mutableStateOf(account.accountMask.orEmpty()) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_manage_rename_account)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.finance_name)) }, singleLine = true)
                OutlinedTextField(mask, { mask = it }, label = { Text(stringResource(R.string.finance_manage_last_digits)) }, singleLine = true, supportingText = { Text(stringResource(R.string.finance_manage_mask_footer)) })
            }
        },
        confirmButton = {
            TextButton(enabled = name.isNotBlank(), onClick = {
                scope.launch {
                    try { connection.renameStatementAccount(account.id, name.trim(), mask.trim()); snackbar.show(context.getString(R.string.finance_saved)); onDone() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                }
            }) { Text(stringResource(R.string.finance_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

/** Deletes an account of imported statements, with its transactions and net worth history. */
@Composable
internal fun DeleteStatementAccountDialog(account: FinanceAccount, onDismiss: () -> Unit, onDone: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    com.teanode.app.ui.components.ConfirmationDialog(
        title = stringResource(R.string.finance_manage_delete_account_question), text = stringResource(R.string.finance_manage_delete_account_message), confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
        onConfirm = {
            scope.launch {
                try {
                    val deleted = connection.deleteStatementAccount(account.id)
                    snackbar.show(context.getString(R.string.finance_manage_account_deleted, deleted?.deletedTransactionCount ?: 0))
                    onDone()
                } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
            }
        },
        onDismiss = onDismiss,
    )
}

/** The spending rules to offer for the transactions filed under a category: what they share, never saved until added. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SuggestRulesSheet(base: FinanceBase, onDismiss: () -> Unit, onAdded: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var categoryId by remember { mutableStateOf("") }
    var isOpen by remember { mutableStateOf(false) }
    var proposals by remember { mutableStateOf<SpendingRuleProposals?>(null) }
    var isEmpty by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    LaunchedEffect(categoryId) {
        if (categoryId.isEmpty()) return@LaunchedEffect
        isWorking = true
        proposals = null
        try {
            val ids = mutableListOf<String>()
            while (ids.size < 2000) {
                val page = connection.financeTransactions(FinanceTransactionFilter(spendingCategoryId = categoryId), limit = 200, offset = ids.size)
                val fresh = page.financeTransactions.filterNotNull().map { row -> row.id }
                ids.addAll(fresh)
                if (fresh.isEmpty() || ids.size >= (page.totalCount ?: 0)) break
            }
            isEmpty = ids.isEmpty()
            if (ids.isNotEmpty()) proposals = connection.proposeSpendingRules(ids, categoryId)
        } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
        isWorking = false
    }
    ShoppingSheet(stringResource(R.string.finance_manage_suggest_rules), onDismiss, confirmLabel = stringResource(R.string.finance_done), onConfirm = onDismiss) {
        ExposedDropdownMenuBox(isOpen, { isOpen = it }) {
            OutlinedTextField(if (categoryId.isEmpty()) stringResource(R.string.finance_manage_choose) else base.categoryName(categoryId), {}, readOnly = true, label = { Text(stringResource(R.string.finance_category)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth())
            ExposedDropdownMenu(isOpen, { isOpen = false }) { for (category in base.pickable(null)) DropdownMenuItem(text = { Text(base.label(category)) }, onClick = { categoryId = category.id; isOpen = false }) }
        }
        Text(stringResource(R.string.finance_manage_suggest_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (isWorking) androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth())
        if (isEmpty) Text(stringResource(R.string.finance_manage_none_filed), color = MaterialTheme.colorScheme.onSurfaceVariant)
        proposals?.let { read ->
            if (read.proposals.isEmpty()) Text(stringResource(R.string.finance_manage_no_rules_to_suggest), color = MaterialTheme.colorScheme.onSurfaceVariant)
            for (proposal in read.proposals) {
                TeaNodeRow(
                    "“${proposal.matchText}”",
                    supporting = context.resources.getQuantityString(R.plurals.finance_transaction_count, proposal.financeTransactionCount ?: 0, proposal.financeTransactionCount ?: 0),
                    trailing = {
                        TextButton(onClick = {
                            scope.launch {
                                try { connection.saveSpendingRule(null, proposal.matchText, categoryId, "", "", "", null); snackbar.show(context.getString(R.string.finance_rule_saved)); onAdded() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                            }
                        }) { Text(stringResource(R.string.finance_manage_add)) }
                    },
                )
            }
            val skipped = (read.tooGenericMatchTextCount ?: 0) + (read.changingNumberMatchTextCount ?: 0) + (read.overLimitMatchTextCount ?: 0)
            if (skipped > 0) Text(stringResource(R.string.finance_manage_left_out, skipped), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** Brings in a provider connection made elsewhere: a Plaid access token or a SimpleFIN access URL already claimed. */
@Composable
fun FinanceImportCredentialScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var kinds by remember { mutableStateOf(listOf("plaid", "simplefin")) }
    var kind by remember { mutableStateOf("simplefin") }
    var credential by remember { mutableStateOf("") }
    var institution by remember { mutableStateOf("") }
    var isWorking by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { try { connection.financeProviderKinds().takeIf { list -> list.isNotEmpty() }?.let { list -> kinds = list; if (kind !in list) kind = list.first() } } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { } }
    TeaNodeScreen(title = stringResource(R.string.finance_manage_bring_connection), onBack = { navigator.back() }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { for (each in kinds) FilterChip(kind == each, { kind = each }, label = { Text(if (each == "plaid") "Plaid" else if (each == "simplefin") "SimpleFIN" else each) }) }
            OutlinedTextField(credential, { credential = it }, label = { Text(stringResource(R.string.finance_manage_credential)) }, singleLine = true, visualTransformation = PasswordVisualTransformation(), modifier = Modifier.fillMaxWidth())
            Text(stringResource(if (kind == "plaid") R.string.finance_manage_plaid_footer else R.string.finance_manage_simplefin_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedTextField(institution, { institution = it }, label = { Text(stringResource(R.string.finance_manage_institution)) }, singleLine = true, modifier = Modifier.fillMaxWidth(), supportingText = { Text(stringResource(R.string.finance_manage_institution_footer)) })
            Button(enabled = credential.isNotBlank() && !isWorking, onClick = {
                scope.launch {
                    isWorking = true
                    try { connection.importFinanceCredential(kind, credential.trim(), institution.trim().ifEmpty { null }); snackbar.show(context.getString(R.string.finance_bank_linked)); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                    isWorking = false
                }
            }) { Text(stringResource(R.string.finance_manage_import)) }
        }
    }
}

/** Imports transaction rows from a JSON file, after a look at what that would do. */
@Composable
fun FinanceImportRowsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var arguments by remember { mutableStateOf<Map<String, JsonElement>?>(null) }
    var preview by remember { mutableStateOf<TransactionRowsPreview?>(null) }
    var isWorking by remember { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            isWorking = true
            try {
                val bytes = context.contentResolver.openInputStream(uri)?.use { stream -> stream.readBytes() } ?: ByteArray(0)
                val read = TransactionRowsFile.variables(bytes)
                if (read == null) snackbar.showProblem(context.getString(R.string.finance_manage_not_rows_file)) else {
                    arguments = read
                    preview = connection.previewImportTransactions(read)
                }
            } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
            isWorking = false
        }
    }
    TeaNodeScreen(title = stringResource(R.string.finance_manage_import_transactions), onBack = { navigator.back() }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            OutlinedButton(onClick = { picker.launch("*/*") }, enabled = !isWorking) { Text(stringResource(R.string.finance_manage_choose_file)) }
            Text(stringResource(R.string.finance_manage_rows_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (isWorking) androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth())
            preview?.let { read ->
                TeaNodeSectionHeader(stringResource(R.string.finance_account))
                Text(if (read.isNewAccount == true) stringResource(R.string.finance_manage_new_account, read.accountName.orEmpty()) else read.accountName.orEmpty())
                TeaNodeSectionHeader(stringResource(R.string.finance_manage_would_import))
                Text(stringResource(R.string.finance_manage_period, read.firstPostedOn.orEmpty(), read.lastPostedOn.orEmpty()))
                Text(stringResource(R.string.finance_money_in) + ": " + FinanceMoney.shown(read.moneyInAmount, read.currencyCode))
                Text(stringResource(R.string.finance_money_out) + ": " + FinanceMoney.shown(read.moneyOutAmount, read.currencyCode))
                read.verificationSummary?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                TeaNodeSectionHeader(stringResource(R.string.finance_manage_new_rows, read.newRows.size))
                for (row in read.newRows.take(50)) TeaNodeRow(row.description.orEmpty(), supporting = listOfNotNull(row.postedOn, stringResource(R.string.finance_manage_nearby).takeIf { row.hasNearbyStoredTransaction == true }).joinToString(" · "), trailing = { AmountText(row.amount, read.currencyCode) })
                if (read.presentRows.isNotEmpty()) Text(stringResource(R.string.finance_manage_already_held, read.presentRows.size), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Button(enabled = !isWorking && read.newRows.isNotEmpty(), onClick = {
                    scope.launch {
                        isWorking = true
                        try {
                            val done = connection.importTransactions(arguments.orEmpty())
                            snackbar.show(context.getString(R.string.finance_manage_imported, done?.addedTransactionCount ?: 0))
                            navigator.back()
                        } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                        isWorking = false
                    }
                }) { Text(stringResource(R.string.finance_manage_import)) }
            }
        }
    }
}
