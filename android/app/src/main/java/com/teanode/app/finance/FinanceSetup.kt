package com.teanode.app.finance

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.input.PasswordVisualTransformation
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
import com.teanode.kit.finance.FinanceSource
import com.teanode.kit.finance.ReportingCurrency
import com.teanode.kit.finance.deleteFinanceSource
import com.teanode.kit.finance.financeProviderKinds
import com.teanode.kit.finance.financeSources
import com.teanode.kit.finance.linkSimpleFIN
import com.teanode.kit.finance.reportingCurrency
import com.teanode.kit.finance.setReportingCurrency
import com.teanode.kit.finance.switchFinanceSource
import com.teanode.kit.finance.syncFinanceSource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private class SetupRead(val currency: ReportingCurrency, val sources: List<FinanceSource>, val providerKinds: List<String>)

/** The currency totals are shown in, the banks the person linked, and a way to link one through SimpleFIN. */
@Composable
fun FinanceSetupScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(connection) { SetupRead(connection.reportingCurrency(), connection.financeSources(), connection.financeProviderKinds()) }
    var isChangingCurrency by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<FinanceSource?>(null) }
    var isLinking by remember { mutableStateOf(false) }
    fun act(saying: String, work: suspend () -> Unit) {
        scope.launch { try { work(); snackbar.show(saying) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() }
    }
    TeaNodeScreen(title = stringResource(R.string.finance_part_setup), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("currency-header") { TeaNodeSectionHeader(stringResource(R.string.finance_reporting_currency)) }
                item("currency") {
                    TeaNodeRow(
                        loaded.currency.currencyCode.ifEmpty { stringResource(R.string.finance_currency_automatic) },
                        supporting = stringResource(if (loaded.currency.isChosen) R.string.finance_currency_chosen else R.string.finance_currency_follows),
                        trailing = { TextButton(onClick = { isChangingCurrency = true }) { Text(stringResource(R.string.finance_change)) } },
                    )
                }
                item("currency-footer") { Footnote(stringResource(R.string.finance_currency_footer)) }
                item("banks-header") { TeaNodeSectionHeader(stringResource(R.string.finance_linked_banks)) }
                if (loaded.sources.isEmpty()) item("banks-none") { Footnote(stringResource(R.string.finance_no_banks)) }
                for (source in loaded.sources) {
                    item("source-" + source.id) {
                        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Column(Modifier.weight(1f)) {
                                    Text(source.institutionName?.ifEmpty { null } ?: source.name.orEmpty(), style = MaterialTheme.typography.bodyLarge)
                                    Text(listOfNotNull(source.providerKind, source.lastRunAt?.let { stringResource(R.string.finance_last_synced, it.take(10)) }).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                                Switch(source.isEnabled == true, { isOn -> act(context.getString(R.string.finance_saved)) { connection.switchFinanceSource(source.id, isOn) } })
                            }
                            if (source.isSignInRequired == true) Text(stringResource(R.string.finance_sign_in_required), color = androidx.compose.ui.graphics.Color(0xFFEF6C00), style = MaterialTheme.typography.bodySmall)
                            source.lastError?.takeIf { error -> error.isNotEmpty() }?.let { error -> Text(error, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                            Row {
                                TextButton(onClick = { act(context.getString(R.string.finance_sync_started)) { connection.syncFinanceSource(source.id) } }) { Text(stringResource(R.string.finance_sync_now)) }
                                TextButton(onClick = { deleting = source }) { Text(stringResource(R.string.finance_delete), color = MaterialTheme.colorScheme.error) }
                            }
                        }
                    }
                }
                item("link") {
                    Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        if ("simplefin" in loaded.providerKinds) OutlinedButton(onClick = { isLinking = true }) { Text(stringResource(R.string.finance_link_simplefin)) }
                        if ("plaid" in loaded.providerKinds) Footnote(stringResource(R.string.finance_plaid_in_dashboard))
                    }
                }
            }
            if (isChangingCurrency) {
                CurrencyDialog(loaded.currency.currencyCode, onDismiss = { isChangingCurrency = false }) { code ->
                    isChangingCurrency = false
                    act(context.getString(R.string.finance_saved)) { connection.setReportingCurrency(code) }
                }
            }
            deleting?.let { source ->
                ConfirmationDialog(
                    title = stringResource(R.string.finance_delete_bank_question), text = stringResource(R.string.finance_delete_bank_message), confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
                    onConfirm = { deleting = null; act(context.getString(R.string.finance_deleted)) { connection.deleteFinanceSource(source.id) } }, onDismiss = { deleting = null },
                )
            }
            if (isLinking) {
                SetupTokenDialog(onDismiss = { isLinking = false }) { token ->
                    isLinking = false
                    act(context.getString(R.string.finance_bank_linked)) { connection.linkSimpleFIN(token) }
                }
            }
        }
    }
}

@Composable
private fun CurrencyDialog(current: String, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var code by remember { mutableStateOf(current) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_reporting_currency)) },
        text = { OutlinedTextField(code, { code = it.uppercase() }, label = { Text(stringResource(R.string.finance_currency_code)) }, singleLine = true, supportingText = { Text(stringResource(R.string.finance_currency_empty_clears)) }) },
        confirmButton = { TextButton(enabled = code.isEmpty() || code.length == 3, onClick = { onSave(code) }) { Text(stringResource(R.string.finance_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

@Composable
private fun SetupTokenDialog(onDismiss: () -> Unit, onLink: (String) -> Unit) {
    var token by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_link_simplefin)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(token, { token = it }, label = { Text(stringResource(R.string.finance_setup_token)) }, singleLine = true, visualTransformation = PasswordVisualTransformation())
                Text(stringResource(R.string.finance_setup_token_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = { Button(enabled = token.isNotBlank(), onClick = { onLink(token.trim()) }) { Text(stringResource(R.string.finance_link)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}
