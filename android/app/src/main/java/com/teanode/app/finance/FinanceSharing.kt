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
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shopping.ShoppingSheet
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.AllFinanceShareView
import com.teanode.kit.finance.FinanceAccount
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceShareLevel
import com.teanode.kit.finance.FinanceShareView
import com.teanode.kit.finance.ShareCircle
import com.teanode.kit.finance.SharePerson
import com.teanode.kit.finance.ShareTarget
import com.teanode.kit.finance.SharedAllFinance
import com.teanode.kit.finance.SharedFinanceAccount
import com.teanode.kit.finance.SharedFinanceSpendingRow
import com.teanode.kit.finance.SharedFinanceTransaction
import com.teanode.kit.finance.SharedSpendingSummary
import com.teanode.kit.finance.allFinanceShares
import com.teanode.kit.finance.financeAccounts
import com.teanode.kit.finance.financeShares
import com.teanode.kit.finance.id
import com.teanode.kit.finance.isAtLeast
import com.teanode.kit.finance.isShared
import com.teanode.kit.finance.level
import com.teanode.kit.finance.ownerShownName
import com.teanode.kit.finance.shareAllFinance
import com.teanode.kit.finance.shareCircles
import com.teanode.kit.finance.shareFinanceAccount
import com.teanode.kit.finance.sharePeople
import com.teanode.kit.finance.sharedAllFinance
import com.teanode.kit.finance.sharedAllFinanceSpending
import com.teanode.kit.finance.sharedAllFinanceTransactions
import com.teanode.kit.finance.sharedFinanceAccounts
import com.teanode.kit.finance.sharedFinanceSpending
import com.teanode.kit.finance.sharedFinanceTransactions
import com.teanode.kit.finance.shownAccountName
import com.teanode.kit.finance.shownName
import com.teanode.kit.finance.target
import com.teanode.kit.finance.unshareAllFinance
import com.teanode.kit.finance.unshareFinanceAccount
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.YearMonth

private fun levelName(context: android.content.Context, level: FinanceShareLevel?, raw: String): String {
    val resource = when (level) {
        FinanceShareLevel.Balance -> R.string.finance_level_balance
        FinanceShareLevel.Spending -> R.string.finance_level_spending
        FinanceShareLevel.Transactions -> R.string.finance_level_transactions
        null -> return raw
    }
    return context.getString(resource)
}

private fun levelHint(level: FinanceShareLevel, isAll: Boolean): Int = when (level) {
    FinanceShareLevel.Balance -> R.string.finance_level_balance_hint
    FinanceShareLevel.Spending -> if (isAll) R.string.finance_level_spending_all_hint else R.string.finance_level_spending_hint
    FinanceShareLevel.Transactions -> if (isAll) R.string.finance_level_transactions_all_hint else R.string.finance_level_transactions_hint
}

private class SharingRead(val shares: List<FinanceShareView>, val allShares: List<AllFinanceShareView>, val accounts: List<FinanceAccount>, val people: List<SharePerson>, val circles: List<ShareCircle>)

/** Who the person's finance is shared with, at which level, with a way to share more and to stop. */
@Composable
fun FinanceSharingScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(connection) {
        SharingRead(connection.financeShares(), connection.allFinanceShares(), connection.financeAccounts(), connection.sharePeople(), connection.shareCircles())
    }
    var sharingAccount by remember { mutableStateOf(false) }
    var sharingAll by remember { mutableStateOf(false) }
    var stoppingAccount by remember { mutableStateOf<FinanceShareView?>(null) }
    var stoppingAll by remember { mutableStateOf<AllFinanceShareView?>(null) }
    fun act(saying: String, work: suspend () -> Unit) {
        scope.launch { try { work(); snackbar.show(saying) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() }
    }
    TeaNodeScreen(title = stringResource(R.string.finance_part_sharing), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            fun nameOfTarget(target: ShareTarget, granteeName: String): String = if (target is ShareTarget.Circle) context.getString(R.string.finance_share_everyone_in, granteeName) else granteeName
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("all-header") { TeaNodeSectionHeader(stringResource(R.string.finance_all_your_finance)) }
                if (loaded.allShares.isEmpty()) item("all-none") { Footnote(stringResource(R.string.finance_all_not_shared)) }
                for (share in loaded.allShares) {
                    item("all-" + share.id) {
                        val name = nameOfTarget(share.target, if (share.granteeCircleId != null) share.granteeCircleName.orEmpty() else share.granteeName?.ifEmpty { null } ?: share.granteeUsername.orEmpty())
                        TeaNodeRow(name, supporting = levelName(context, share.level, share.financeShareLevel), trailing = { TextButton(onClick = { stoppingAll = share }) { Text(stringResource(R.string.finance_stop_sharing), color = MaterialTheme.colorScheme.error) } })
                    }
                }
                item("share-all") { TextButton(onClick = { sharingAll = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_share_all_your_finance)) } }
                item("accounts-header") { TeaNodeSectionHeader(stringResource(R.string.finance_part_accounts)) }
                if (loaded.shares.isEmpty()) item("accounts-none") { Footnote(stringResource(R.string.finance_accounts_not_shared)) }
                for (share in loaded.shares) {
                    item("share-" + share.id) {
                        val name = nameOfTarget(share.target, if (share.granteeCircleId != null) share.granteeCircleName.orEmpty() else share.granteeName?.ifEmpty { null } ?: share.granteeUsername.orEmpty())
                        TeaNodeRow(share.shownAccountName, supporting = "$name · ${levelName(context, share.level, share.financeShareLevel)}", trailing = { TextButton(onClick = { stoppingAccount = share }) { Text(stringResource(R.string.finance_stop_sharing), color = MaterialTheme.colorScheme.error) } })
                    }
                }
                item("share-account") { TextButton(onClick = { sharingAccount = true }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.finance_share_an_account)) } }
            }
            if (sharingAccount) ShareSheet(false, loaded.accounts, loaded.people, loaded.circles, onDismiss = { sharingAccount = false }) { accountId, target, level ->
                sharingAccount = false
                act(context.getString(R.string.finance_shared)) { connection.shareFinanceAccount(accountId, target, level) }
            }
            if (sharingAll) ShareSheet(true, emptyList(), loaded.people, loaded.circles, onDismiss = { sharingAll = false }) { _, target, level ->
                sharingAll = false
                act(context.getString(R.string.finance_shared)) { connection.shareAllFinance(target, level) }
            }
            stoppingAccount?.let { share ->
                ConfirmationDialog(
                    title = stringResource(R.string.finance_stop_sharing), text = stringResource(R.string.finance_will_no_longer_see, share.shownAccountName), confirmLabel = stringResource(R.string.finance_stop_sharing), isDestructive = true,
                    onConfirm = { stoppingAccount = null; act(context.getString(R.string.finance_stopped_sharing)) { connection.unshareFinanceAccount(share.id) } }, onDismiss = { stoppingAccount = null },
                )
            }
            stoppingAll?.let { share ->
                ConfirmationDialog(
                    title = stringResource(R.string.finance_stop_sharing), text = stringResource(R.string.finance_all_will_no_longer_see), confirmLabel = stringResource(R.string.finance_stop_sharing), isDestructive = true,
                    onConfirm = { stoppingAll = null; act(context.getString(R.string.finance_stopped_sharing)) { connection.unshareAllFinance(share.id) } }, onDismiss = { stoppingAll = null },
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ShareSheet(
    isAll: Boolean,
    accounts: List<FinanceAccount>,
    people: List<SharePerson>,
    circles: List<ShareCircle>,
    onDismiss: () -> Unit,
    onShare: (accountId: String, target: ShareTarget, level: FinanceShareLevel) -> Unit,
) {
    val context = LocalContext.current
    var accountId by remember { mutableStateOf(accounts.firstOrNull()?.id.orEmpty()) }
    var target by remember { mutableStateOf<ShareTarget?>(null) }
    var level by remember { mutableStateOf(if (isAll) FinanceShareLevel.Spending else FinanceShareLevel.Balance) }
    var isAccountOpen by remember { mutableStateOf(false) }
    val levels = if (isAll) listOf(FinanceShareLevel.Spending, FinanceShareLevel.Transactions) else FinanceShareLevel.entries
    ShoppingSheet(
        stringResource(if (isAll) R.string.finance_share_all_your_finance else R.string.finance_share_an_account), onDismiss,
        confirmLabel = stringResource(R.string.finance_share), isConfirmEnabled = target != null && (isAll || accountId.isNotEmpty()), onConfirm = { onShare(accountId, target!!, level) },
    ) {
        if (!isAll) {
            ExposedDropdownMenuBox(isAccountOpen, { isAccountOpen = it }) {
                OutlinedTextField(accounts.firstOrNull { account -> account.id == accountId }?.shownName.orEmpty(), {}, readOnly = true, label = { Text(stringResource(R.string.finance_account)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isAccountOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth())
                ExposedDropdownMenu(isAccountOpen, { isAccountOpen = false }) { for (account in accounts) DropdownMenuItem(text = { Text(account.shownName) }, onClick = { accountId = account.id; isAccountOpen = false }) }
            }
        }
        com.teanode.app.finance.ShareTargetPicker(people, circles, target) { target = it }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            for (each in levels) FilterChip(level == each, { level = each }, label = { Text(levelName(context, each, each.rawValue)) })
        }
        Text(stringResource(levelHint(level, isAll)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

private class SharedAccountsRead(val accounts: List<SharedFinanceAccount>, val isWhole: Boolean)

/** The accounts somebody shares with the person, as the level each is shared at shows them. */
@Composable
fun FinanceSharedAccountsScreen(ownerUserId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val read = rememberLoad(connection, ownerUserId) {
        val whole = try { connection.sharedAllFinance(ownerUserId).isNotEmpty() } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { false }
        SharedAccountsRead(connection.sharedFinanceAccounts(ownerUserId), whole)
    }
    TeaNodeScreen(title = read.value?.accounts?.firstOrNull()?.ownerShownName.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                if (loaded.isWhole) item("whole") { TeaNodeNavigationRow(stringResource(R.string.finance_all_your_finance), { navigator.navigate(FinanceRoutes.sharedAllOf(ownerUserId)) }) }
                item("header") { TeaNodeSectionHeader(stringResource(R.string.finance_part_accounts)) }
                for (account in loaded.accounts) {
                    item("account-" + account.id) {
                        TeaNodeRow(
                            account.shownName, supporting = levelName(context, account.level, account.financeShareLevel),
                            trailing = { AmountText(account.currentBalance, account.currencyCode) },
                            onClick = { navigator.navigate(FinanceRoutes.sharedAccount(account.id)) },
                        )
                    }
                }
            }
        }
    }
}

private class SharedAccountRead(val account: SharedFinanceAccount, val spending: List<SharedFinanceSpendingRow>, val transactions: List<SharedFinanceTransaction>, val total: Int)

/** One account somebody shares: its balance, what it owes against its limit, its spending by month and its transactions, as far as the level shows. */
@Composable
fun FinanceSharedAccountScreen(accountId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val read = rememberLoad(connection, accountId) {
        val account = connection.sharedFinanceAccounts().first { each -> each.id == accountId }
        val spending = if (account.isShared(FinanceShareLevel.Spending)) connection.sharedFinanceSpending(accountId) else emptyList()
        val page = if (account.isShared(FinanceShareLevel.Transactions)) connection.sharedFinanceTransactions(accountId, limit = 50) else null
        SharedAccountRead(account, spending, page?.financeTransactions.orEmpty(), page?.totalCount ?: 0)
    }
    TeaNodeScreen(title = read.value?.account?.accountName.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val account = loaded.account
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("level") { Footnote(stringResource(R.string.finance_shared_at, levelName(context, account.level, account.financeShareLevel), account.ownerShownName)) }
                item("balance") { LabeledValue(stringResource(R.string.finance_balance), FinanceMoney.shown(account.currentBalance, account.currencyCode)) }
                account.creditUsage?.let { usage ->
                    item("owed") { LabeledValue(stringResource(R.string.finance_owed_label), FinanceMoney.shown(usage.owedAmount, account.currencyCode)) }
                    item("limit") { LabeledValue(stringResource(R.string.finance_credit_limit), FinanceMoney.shown(usage.creditLimitAmount, account.currencyCode)) }
                    usage.usageShare?.let { share -> item("usage") { LinearProgressIndicator(progress = { share.coerceIn(0.0, 1.0).toFloat() }, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp)) } }
                }
                if (account.isShared(FinanceShareLevel.Spending)) {
                    item("spending-header") { TeaNodeSectionHeader(stringResource(R.string.finance_part_spending)) }
                    if (loaded.spending.isEmpty()) item("spending-none") { Footnote(stringResource(R.string.finance_nothing_spent_yet)) }
                    items(loaded.spending, key = { row -> "spending-" + row.id }) { row ->
                        TeaNodeRow("${row.spendingMonth} · ${row.spendingCategoryName.ifEmpty { stringResource(R.string.finance_uncategorized) }}", supporting = stringResource(R.string.finance_received_amount, FinanceMoney.shown(row.moneyIn, row.currencyCode)), trailing = { Text(FinanceMoney.shown(row.moneyOut, row.currencyCode)) })
                    }
                }
                if (account.isShared(FinanceShareLevel.Transactions)) {
                    item("transactions-header") { TeaNodeSectionHeader(stringResource(R.string.finance_part_transactions)) }
                    items(loaded.transactions, key = { row -> "tx-" + row.id }) { transaction -> SharedTransactionRow(transaction) }
                }
            }
        }
    }
}

@Composable
internal fun SharedTransactionRow(transaction: SharedFinanceTransaction) {
    TeaNodeRow(
        transaction.merchantName.orEmpty().ifEmpty { stringResource(R.string.finance_unknown) },
        supporting = listOfNotNull(transaction.postedOn, transaction.spendingCategoryName?.takeIf { name -> name.isNotEmpty() }).joinToString(" · "),
        trailing = { AmountText(transaction.amount, transaction.currencyCode) },
    )
}

private class SharedAllRead(val sharer: SharedAllFinance?, val rows: List<SharedFinanceSpendingRow>, val transactions: List<SharedFinanceTransaction>)

/** The combined spending, and transactions when the level shows them, of somebody who shares all of their finance. */
@Composable
fun FinanceSharedAllScreen(ownerUserId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    var currencyOverride by remember { mutableStateOf<String?>(null) }
    var period by remember { mutableStateOf<String?>(null) }
    val read = rememberLoad(connection, ownerUserId) {
        val sharer = connection.sharedAllFinance(ownerUserId).firstOrNull()
        val rows = connection.sharedAllFinanceSpending(ownerUserId)
        val transactions = if (sharer?.isSharingTransactionsLevel() == true) connection.sharedAllFinanceTransactions(ownerUserId, limit = 50).financeTransactions else emptyList()
        SharedAllRead(sharer, rows, transactions)
    }
    TeaNodeScreen(title = read.value?.sharer?.ownerShownName.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val currencies = SharedSpendingSummary.currencyCodes(loaded.rows)
            val currency = currencyOverride ?: currencies.firstOrNull().orEmpty()
            val months = SharedSpendingSummary.months(loaded.rows, currency)
            val shownPeriod = period ?: SharedSpendingSummary.latestMonth(loaded.rows).orEmpty()
            val total = SharedSpendingSummary.total(months, shownPeriod)
            val lines = SharedSpendingSummary.lines(loaded.rows, currency, shownPeriod)
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                if (loaded.rows.isEmpty()) item("none") { Footnote(stringResource(R.string.finance_nothing_spent_yet)) }
                else {
                    if (currencies.size > 1) item("currencies") { Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) { for (code in currencies) FilterChip(code == currency, { currencyOverride = code }, label = { Text(code) }) } }
                    item("period") {
                        val latest = YearMonth.parse(SharedSpendingSummary.latestMonth(loaded.rows)!!)
                        val shown = YearMonth.parse(shownPeriod.take(7).ifEmpty { latest.toString() })
                        MonthStepper(shown, latest) { period = it.toString() }
                    }
                    item("out") { LabeledValue(stringResource(R.string.finance_money_out), FinanceMoney.shown(total.moneyOut, currency)) }
                    item("in") { LabeledValue(stringResource(R.string.finance_money_in), FinanceMoney.shown(total.moneyIn, currency)) }
                    item("where") { TeaNodeSectionHeader(stringResource(R.string.finance_where_it_went)) }
                    items(lines, key = { line -> "line-" + line.id }) { line ->
                        TeaNodeRow(line.spendingCategoryName.ifEmpty { stringResource(R.string.finance_uncategorized) }, supporting = androidx.compose.ui.platform.LocalContext.current.resources.getQuantityString(R.plurals.finance_transaction_count, line.financeTransactionCount, line.financeTransactionCount), trailing = { Text(FinanceMoney.shown(line.spentAmount, currency)) })
                    }
                }
                if (loaded.transactions.isNotEmpty()) {
                    item("transactions-header") { TeaNodeSectionHeader(stringResource(R.string.finance_part_transactions)) }
                    items(loaded.transactions, key = { row -> "tx-" + row.id }) { transaction -> SharedTransactionRow(transaction) }
                }
            }
        }
    }
}

private fun SharedAllFinance.isSharingTransactionsLevel(): Boolean = level?.isAtLeast(FinanceShareLevel.Transactions) ?: false
