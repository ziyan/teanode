package com.teanode.app.finance

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Flag
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
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
import com.teanode.kit.finance.FinanceAsset
import com.teanode.kit.finance.FinanceAssetKinds
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.NetWorth
import com.teanode.kit.finance.SavingsTarget
import com.teanode.kit.finance.assetHistory
import com.teanode.kit.finance.assets
import com.teanode.kit.finance.closeAsset
import com.teanode.kit.finance.closeSavingsTarget
import com.teanode.kit.finance.deleteAsset
import com.teanode.kit.finance.deleteValuation
import com.teanode.kit.finance.id
import com.teanode.kit.finance.isClosed
import com.teanode.kit.finance.netWorth
import com.teanode.kit.finance.recordValuation
import com.teanode.kit.finance.saveAsset
import com.teanode.kit.finance.saveSavingsTarget
import com.teanode.kit.finance.savingsTargets
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.LocalDate

private class NetWorthRead(val base: FinanceBase, val netWorth: NetWorth?, val assets: List<FinanceAsset>)

private fun kindWords(context: android.content.Context, kind: String): String {
    val resource = when (kind) {
        "cash" -> R.string.finance_kind_cash
        "investment" -> R.string.finance_kind_investment
        "retirement" -> R.string.finance_kind_retirement
        "property" -> R.string.finance_kind_property
        "vehicle" -> R.string.finance_kind_vehicle
        "other_asset" -> R.string.finance_kind_other_asset
        "credit_card" -> R.string.finance_kind_credit_card
        "loan" -> R.string.finance_kind_loan
        "mortgage" -> R.string.finance_kind_mortgage
        "other_liability" -> R.string.finance_kind_other_liability
        else -> return kind
    }
    return context.getString(resource)
}

/** What the person owns and owes over time, and the assets behind it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FinanceNetWorthScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var days by remember { mutableStateOf(90) }
    var isAdding by remember { mutableStateOf(false) }
    val read = rememberLoad(connection, days) {
        val base = connection.readFinanceBase(context)
        val today = LocalDate.now(base.zone)
        NetWorthRead(base, connection.netWorth(today.minusDays(days.toLong()).toString(), today.toString()), connection.assets())
    }
    TeaNodeScreen(
        title = stringResource(R.string.finance_part_net_worth), onBack = { navigator.back() },
        actions = { IconButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.finance_add)) } },
    ) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val points = loaded.netWorth?.convertedNetWorthPoints.orEmpty().filterNotNull()
            val currency = loaded.netWorth?.reportingCurrencyCode?.ifEmpty { null } ?: loaded.base.reportingCurrency
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("range") {
                    Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        for ((value, label) in listOf(30 to R.string.finance_range_30, 90 to R.string.finance_range_90, 365 to R.string.finance_range_year, 3650 to R.string.finance_range_all)) {
                            FilterChip(days == value, { days = value }, label = { Text(stringResource(label)) })
                        }
                    }
                }
                if (points.isEmpty()) {
                    item("none") { Text(stringResource(R.string.finance_no_net_worth), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp)) }
                } else {
                    item("latest") {
                        val first = FinanceMoney.decimal(points.first().netWorthAmount)
                        val last = FinanceMoney.decimal(points.last().netWorthAmount)
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(FinanceMoney.shown(points.last().netWorthAmount, currency), style = MaterialTheme.typography.headlineMedium)
                            if (first != null && last != null) {
                                val change = last - first
                                Text("${if (change.signum() >= 0) "▲" else "▼"} ${FinanceMoney.shown(change.abs(), currency)}", color = if (change.signum() >= 0) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error)
                            }
                        }
                    }
                    item("chart") { Box(Modifier.padding(horizontal = 16.dp)) { LineChart(listOf(LineSeries(points.map { point -> point.netWorthAmount?.toDoubleOrNull() ?: 0.0 }, FinancePalette.color(0)))) } }
                }
                for (kind in FinanceAssetKinds.owned + FinanceAssetKinds.owed) {
                    val ofKind = loaded.assets.filter { asset -> asset.assetKind == kind }
                    if (ofKind.isEmpty()) continue
                    item("kind-$kind") { TeaNodeSectionHeader(kindWords(context, kind)) }
                    for (asset in ofKind) {
                        item("asset-" + asset.id) {
                            TeaNodeRow(
                                asset.assetName, supporting = if (asset.isClosed) stringResource(R.string.finance_closed) else asset.latestValuation?.valuedOn,
                                trailing = { AmountText(asset.latestValuation?.value, asset.latestValuation?.currencyCode ?: asset.currencyCode, isSignColored = false) },
                                onClick = { navigator.navigate(FinanceRoutes.asset(asset.id)) },
                            )
                        }
                    }
                }
            }
            if (isAdding) {
                AssetEditorDialog(loaded.base.reportingCurrency, onDismiss = { isAdding = false }) { fields ->
                    isAdding = false
                    scope.launch {
                        try { connection.saveAsset(null, fields); snackbar.show(context.getString(R.string.finance_asset_added)) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                        read.refresh()
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AssetEditorDialog(currency: String, onDismiss: () -> Unit, onSave: (Map<String, JsonElement>) -> Unit) {
    val context = LocalContext.current
    var name by remember { mutableStateOf("") }
    var kind by remember { mutableStateOf("cash") }
    var code by remember { mutableStateOf(currency.ifEmpty { "USD" }) }
    var value by remember { mutableStateOf("") }
    var isOpen by remember { mutableStateOf(false) }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_new_asset)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.finance_name)) }, singleLine = true)
                ExposedDropdownMenuBox(isOpen, { isOpen = it }) {
                    OutlinedTextField(kindWords(context, kind), {}, readOnly = true, label = { Text(stringResource(R.string.finance_kind)) }, trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth())
                    ExposedDropdownMenu(isOpen, { isOpen = false }) {
                        for (each in FinanceAssetKinds.owned + FinanceAssetKinds.owed) DropdownMenuItem(text = { Text(kindWords(context, each)) }, onClick = { kind = each; isOpen = false })
                    }
                }
                OutlinedTextField(code, { code = it.uppercase() }, label = { Text(stringResource(R.string.finance_currency)) }, singleLine = true)
                OutlinedTextField(value, { value = it }, label = { Text(stringResource(R.string.finance_value_now)) }, singleLine = true)
            }
        },
        confirmButton = {
            TextButton(
                enabled = name.isNotBlank() && code.length == 3 && (value.isBlank() || FinanceMoney.isDecimal(value.trim())),
                onClick = {
                    val fields = mutableMapOf<String, JsonElement>("assetName" to JsonPrimitive(name.trim()), "assetKind" to JsonPrimitive(kind), "currencyCode" to JsonPrimitive(code))
                    if (value.isNotBlank()) { fields["value"] = JsonPrimitive(value.trim()); fields["valuedOn"] = JsonPrimitive(LocalDate.now().toString()) }
                    onSave(fields)
                },
            ) { Text(stringResource(R.string.finance_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

/** An asset's values over time, adding a value, closing, reopening and deleting it. */
@Composable
fun FinanceAssetDetailScreen(assetId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(connection, assetId) {
        val asset = connection.assets().first { each -> each.id == assetId }
        asset to connection.assetHistory(assetId)
    }
    var value by remember { mutableStateOf("") }
    var note by remember { mutableStateOf("") }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    fun act(saying: String, work: suspend () -> Unit) {
        scope.launch {
            try { work(); snackbar.show(saying) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
            read.refresh()
        }
    }
    TeaNodeScreen(title = read.value?.first?.assetName.orEmpty(), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { (asset, history) ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("kind") { LabeledValue(stringResource(R.string.finance_kind), kindWords(context, asset.assetKind)) }
                item("currency") { LabeledValue(stringResource(R.string.finance_currency), asset.currencyCode.orEmpty()) }
                asset.financeSecurity?.let { security -> item("security") { LabeledValue(stringResource(R.string.finance_security), listOfNotNull(security.tickerSymbol, security.securityName).joinToString(" · ")) } }
                if (asset.valuationSource == "manual" || asset.valuationSource.isNullOrEmpty()) {
                    item("add-header") { TeaNodeSectionHeader(stringResource(R.string.finance_add_value)) }
                    item("add") {
                        Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            OutlinedTextField(value, { value = it }, label = { Text(stringResource(R.string.finance_value)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                            OutlinedTextField(note, { note = it }, label = { Text(stringResource(R.string.finance_note)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
                            Button(enabled = FinanceMoney.isDecimal(value.trim()), onClick = {
                                val typed = value.trim(); val typedNote = note.trim(); value = ""; note = ""
                                act(context.getString(R.string.finance_value_added)) { connection.recordValuation(assetId, typed, LocalDate.now().toString(), typedNote) }
                            }) { Text(stringResource(R.string.finance_add_value)) }
                        }
                    }
                }
                item("history-header") { TeaNodeSectionHeader(stringResource(R.string.finance_history)) }
                for (valuation in history) {
                    item("valuation-" + valuation.id) {
                        TeaNodeRow(
                            FinanceMoney.shown(valuation.value, valuation.currencyCode ?: asset.currencyCode), supporting = listOfNotNull(valuation.valuedOn, valuation.valuationNote?.takeIf { text -> text.isNotEmpty() }).joinToString(" · "),
                            trailing = { TextButton(onClick = { act(context.getString(R.string.finance_value_deleted)) { connection.deleteValuation(valuation.id) } }) { Text(stringResource(R.string.finance_delete), color = MaterialTheme.colorScheme.error) } },
                        )
                    }
                }
                item("actions") {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedButton(onClick = { act(context.getString(if (asset.isClosed) R.string.finance_reopened else R.string.finance_closed)) { connection.closeAsset(assetId, LocalDate.now().toString(), asset.isClosed) } }) { Text(stringResource(if (asset.isClosed) R.string.finance_reopen else R.string.finance_close)) }
                        OutlinedButton(onClick = { isConfirmingDelete = true }) { Text(stringResource(R.string.finance_delete_asset), color = MaterialTheme.colorScheme.error) }
                    }
                }
            }
            if (isConfirmingDelete) {
                ConfirmationDialog(
                    title = stringResource(R.string.finance_delete_asset), text = stringResource(R.string.finance_its_values_go_with_it, history.size), confirmLabel = stringResource(R.string.finance_delete), isDestructive = true,
                    onConfirm = { isConfirmingDelete = false; scope.launch { try { connection.deleteAsset(assetId); navigator.back() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
                    onDismiss = { isConfirmingDelete = false },
                )
            }
        }
    }
}

private class SavingsRead(val base: FinanceBase, val targets: List<SavingsTarget>)

/** What the person is saving towards, how far along it is, and what it needs a month. */
@Composable
fun FinanceSavingsTargetsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isShowingClosed by remember { mutableStateOf(false) }
    var isAdding by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<SavingsTarget?>(null) }
    val read = rememberLoad(connection) { SavingsRead(connection.readFinanceBase(context), connection.savingsTargets()) }
    TeaNodeScreen(
        title = stringResource(R.string.finance_part_savings_targets), onBack = { navigator.back() },
        actions = { IconButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.finance_add)) } },
    ) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val shown = loaded.targets.filter { target -> isShowingClosed || target.savingsTarget?.closedOn.isNullOrEmpty() }
            Column(Modifier.fillMaxSize()) {
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.finance_show_closed), Modifier.weight(1f)); Switch(isShowingClosed, { isShowingClosed = it })
                }
                if (shown.isEmpty()) {
                    EmptyState(Icons.Outlined.Flag, stringResource(R.string.finance_no_savings_targets), description = stringResource(R.string.finance_no_savings_targets_description))
                } else {
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                        for (target in shown) {
                            val goal = target.savingsTarget ?: continue
                            val progress = target.savingsTargetProgress
                            item("target-" + target.id) {
                                val isClosed = !goal.closedOn.isNullOrEmpty()
                                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                    Row(verticalAlignment = Alignment.CenterVertically) {
                                        Text(goal.savingsTargetName, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                                        if (progress?.isBehind == true) Text(stringResource(R.string.finance_behind), color = androidx.compose.ui.graphics.Color(0xFFEF6C00), style = MaterialTheme.typography.labelMedium)
                                        if (isClosed) Text(stringResource(R.string.finance_closed), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    }
                                    val saved = FinanceMoney.decimal(progress?.savedAmount)?.toDouble() ?: 0.0
                                    val goalAmount = FinanceMoney.decimal(goal.targetAmount)?.toDouble() ?: 0.0
                                    if (goalAmount > 0) LinearProgressIndicator(progress = { (saved / goalAmount).coerceIn(0.0, 1.0).toFloat() }, modifier = Modifier.fillMaxWidth())
                                    Text(stringResource(R.string.finance_saved_of_target, FinanceMoney.shown(progress?.savedAmount, goal.currencyCode), FinanceMoney.shown(goal.targetAmount, goal.currencyCode), goal.targetOn.orEmpty()), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    progress?.requiredMonthlyAmount?.takeIf { amount -> amount.isNotEmpty() }?.let { monthly -> Text(stringResource(R.string.finance_needs_a_month, FinanceMoney.shown(monthly, goal.currencyCode)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                                    TextButton(onClick = { editing = target }) { Text(stringResource(R.string.finance_edit)) }
                                    TextButton(onClick = { scope.launch { try { connection.closeSavingsTarget(goal.id, isClosed); snackbar.show(context.getString(if (isClosed) R.string.finance_reopened else R.string.finance_closed)) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() } }) {
                                        Text(stringResource(if (isClosed) R.string.finance_reopen else R.string.finance_close))
                                    }
                                }
                            }
                        }
                    }
                }
            }
            editing?.let { target ->
                SavingsTargetDialog(target, onDismiss = { editing = null }) { fields ->
                    editing = null
                    scope.launch { try { connection.saveSavingsTarget(target.id, fields); snackbar.show(context.getString(R.string.finance_saved)) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() }
                }
            }
            if (isAdding) {
                SavingsTargetDialog(null, onDismiss = { isAdding = false }) { fields ->
                    isAdding = false
                    scope.launch { try { connection.saveSavingsTarget(null, fields); snackbar.show(context.getString(R.string.finance_target_added)) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }; read.refresh() }
                }
            }
        }
    }
}

@Composable
private fun SavingsTargetDialog(existing: SavingsTarget?, onDismiss: () -> Unit, onSave: (Map<String, JsonElement>) -> Unit) {
    val goal = existing?.savingsTarget
    var name by remember { mutableStateOf(goal?.savingsTargetName.orEmpty()) }
    var amount by remember { mutableStateOf(goal?.targetAmount.orEmpty()) }
    var day by remember { mutableStateOf(goal?.targetOn ?: LocalDate.now().plusYears(1).toString()) }
    var measure by remember { mutableStateOf(goal?.targetMeasure?.ifEmpty { null } ?: "cash_flow") }
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.finance_new_savings_target)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.finance_name)) }, singleLine = true)
                OutlinedTextField(amount, { amount = it }, label = { Text(stringResource(R.string.finance_amount)) }, singleLine = true)
                OutlinedTextField(day, { day = it }, label = { Text(stringResource(R.string.finance_by_day)) }, singleLine = true)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(measure == "cash_flow", { measure = "cash_flow" }, label = { Text(stringResource(R.string.finance_measure_cash_flow)) })
                    FilterChip(measure == "net_worth", { measure = "net_worth" }, label = { Text(stringResource(R.string.finance_part_net_worth)) })
                }
            }
        },
        confirmButton = {
            TextButton(enabled = name.isNotBlank() && FinanceMoney.isDecimal(amount.trim()) && FinanceEntryDay.isDay(day.trim()), onClick = {
                onSave(mapOf("savingsTargetName" to JsonPrimitive(name.trim()), "targetAmount" to JsonPrimitive(amount.trim()), "targetOn" to JsonPrimitive(day.trim()), "targetMeasure" to JsonPrimitive(measure)))
            }) { Text(stringResource(R.string.finance_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.finance_cancel)) } },
    )
}

private object FinanceEntryDay {
    fun isDay(text: String): Boolean = com.teanode.kit.finance.FinanceEntryText.isDay(text)
}
