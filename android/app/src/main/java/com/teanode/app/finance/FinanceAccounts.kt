package com.teanode.app.finance

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
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
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.CreditUsage
import com.teanode.kit.finance.FinanceAccount
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.StatementImport
import com.teanode.kit.finance.creditUsage
import com.teanode.kit.finance.financeAccounts
import com.teanode.kit.finance.isFromStatements
import com.teanode.kit.finance.regenerateStatementImportAddress
import com.teanode.kit.finance.shownName
import com.teanode.kit.finance.statementImport
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private class AccountsRead(val accounts: List<FinanceAccount>, val usage: CreditUsage?, val statementImport: StatementImport?)

/** The accounts under their institutions, what the cards owe against their limits, and the address statements are mailed to. */
@Composable
fun FinanceAccountsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var renewed by remember { mutableStateOf<StatementImport?>(null) }
    var isRenewing by remember { mutableStateOf(false) }
    val read = rememberLoad(connection) {
        val accounts = connection.financeAccounts()
        val usage = try { connection.creditUsage() } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { null }
        val statements = try { connection.statementImport() } catch (failure: CancellationException) { throw failure } catch (_: Throwable) { null }
        AccountsRead(accounts, usage, statements)
    }
    TeaNodeScreen(title = stringResource(R.string.finance_part_accounts), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val statementImport = renewed ?: loaded.statementImport
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                loaded.usage?.takeIf { usage -> usage.creditCards.orEmpty().filterNotNull().isNotEmpty() }?.let { usage ->
                    item("credit-header") { TeaNodeSectionHeader(stringResource(R.string.finance_credit_used)) }
                    item("credit") {
                        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Row {
                                Text(stringResource(R.string.finance_owed, FinanceMoney.shown(usage.totalOwedAmount, usage.reportingCurrencyCode)), Modifier.weight(1f))
                                Text(stringResource(R.string.finance_of_limit, FinanceMoney.shown(usage.totalCreditLimitAmount, usage.reportingCurrencyCode)), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            usage.usageShare?.let { share -> LinearProgressIndicator(progress = { share.coerceIn(0.0, 1.0).toFloat() }, modifier = Modifier.fillMaxWidth()) }
                            if ((usage.leftOutCardCount ?: 0) > 0) Text(context.resources.getQuantityString(R.plurals.finance_cards_left_out, usage.leftOutCardCount!!, usage.leftOutCardCount), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                val byInstitution = loaded.accounts.groupBy { account -> account.institutionName.orEmpty() }
                for ((institution, accounts) in byInstitution) {
                    item("institution-" + institution) { TeaNodeSectionHeader(institution.ifEmpty { stringResource(R.string.finance_part_accounts) }) }
                    for (account in accounts) {
                        item("account-" + account.id) {
                            TeaNodeRow(
                                headline = account.shownName,
                                supporting = listOfNotNull(
                                    stringResource(R.string.finance_from_statements).takeIf { account.isFromStatements },
                                    stringResource(R.string.finance_sign_in_required).takeIf { account.isSignInRequired == true },
                                    account.accountKind?.takeIf { kind -> kind.isNotEmpty() },
                                ).joinToString(" · ").ifEmpty { null },
                                trailing = {
                                    Column(horizontalAlignment = Alignment.End) {
                                        AmountText(account.currentBalance, account.currencyCode)
                                        if (!account.availableBalance.isNullOrEmpty()) Text(stringResource(R.string.finance_available, FinanceMoney.shown(account.availableBalance, account.currencyCode)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    }
                                },
                                onClick = { navigator.navigate(FinanceRoutes.transactions(com.teanode.kit.finance.FinanceTransactionFilter(financeAccountId = account.id))) },
                            )
                        }
                        if (account.accountKind in setOf("investment", "brokerage", "retirement")) {
                            item("trades-" + account.id) { TeaNodeRow(stringResource(R.string.finance_trades), onClick = { navigator.navigate(FinanceRoutes.trades(account.id)) }, modifier = Modifier.padding(start = 16.dp)) }
                        }
                    }
                }
                item("statements-header") { TeaNodeSectionHeader(stringResource(R.string.finance_import_statements)) }
                item("statements") {
                    Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        val address = statementImport?.importAddress.orEmpty()
                        if (address.isEmpty()) {
                            Text(stringResource(R.string.finance_no_mailbox_address), color = MaterialTheme.colorScheme.onSurfaceVariant)
                        } else {
                            Text(address, style = MaterialTheme.typography.bodyLarge)
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                OutlinedButton(onClick = {
                                    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("address", address))
                                    snackbar.show(context.getString(R.string.finance_address_copied))
                                }) { Text(stringResource(R.string.finance_copy_address)) }
                                OutlinedButton(onClick = { isRenewing = true }) { Text(stringResource(R.string.finance_new_address)) }
                            }
                            if (statementImport?.isEnabled == false) Text(stringResource(R.string.finance_importing_off), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            statementImport?.lastStatementImport?.let { last ->
                                Text(
                                    stringResource(R.string.finance_last_import, last.addedTransactionCount ?: 0, last.updatedTransactionCount ?: 0),
                                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                        Text(stringResource(R.string.finance_import_statements_how), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
    if (isRenewing) {
        ConfirmationDialog(
            title = stringResource(R.string.finance_new_address_question), text = stringResource(R.string.finance_new_address_message),
            confirmLabel = stringResource(R.string.finance_make_new_address),
            onConfirm = {
                isRenewing = false
                scope.launch {
                    try {
                        renewed = connection.regenerateStatementImportAddress()
                        snackbar.show(context.getString(R.string.finance_new_address_made))
                    } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                }
            },
            onDismiss = { isRenewing = false },
        )
    }
}
