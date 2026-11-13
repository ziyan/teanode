package com.teanode.app.finance

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.BudgetStatus
import com.teanode.kit.finance.CashFlow
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceTransactionFilter
import com.teanode.kit.finance.SavingSummary
import com.teanode.kit.finance.SpendingByDay
import com.teanode.kit.finance.SpendingLine
import com.teanode.kit.finance.SpendingLines
import com.teanode.kit.finance.SpendingSummary
import com.teanode.kit.finance.budgetStatus
import com.teanode.kit.finance.budgetStatusOfYear
import com.teanode.kit.finance.cashFlow
import com.teanode.kit.finance.savingSummary
import com.teanode.kit.finance.savingSummaryOfYear
import com.teanode.kit.finance.spendingByDay
import com.teanode.kit.finance.spendingSummary
import java.math.BigDecimal
import java.time.LocalDate
import java.time.YearMonth

/** What the Spending screen shows: a month or a year. */
private enum class SpendingPeriodKind { Month, Year }

private class SpendingRead(
    val base: FinanceBase,
    val cashFlow: CashFlow?,
    val byDay: SpendingByDay?,
    val saving: SavingSummary?,
    val budgets: BudgetStatus?,
    val summary: SpendingSummary?,
    val history: CashFlow?,
)

/**
 * A month or a year as the dashboard's Spending tab shows it. A month: twelve months of cash flow to it, its spending
 * day by day against the month before, its saving, its budgets and where it went. A year: every year's cash flow, and
 * the chosen year's saving, budgets and where it went, added up over the whole of it.
 */
@Composable
fun FinanceSpendingScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    var kind by remember { mutableStateOf(SpendingPeriodKind.Month) }
    var month by remember { mutableStateOf<YearMonth?>(null) }
    var year by remember { mutableStateOf<Int?>(null) }
    var groupBy by remember { mutableStateOf("spendingCategory") }
    var selectedSliceId by remember { mutableStateOf<String?>(null) }
    val read = rememberLoad(kind, month, year, groupBy) {
        val base = connection.readFinanceBase(context)
        val shownMonth = month ?: base.thisMonth
        val shownYear = year ?: base.thisMonth.year
        val today = LocalDate.now(base.zone)
        val (from, to) = if (kind == SpendingPeriodKind.Year) {
            LocalDate.of(shownYear, 1, 1) to minOf(LocalDate.of(shownYear, 12, 31), today)
        } else {
            shownMonth.atDay(1) to minOf(shownMonth.atEndOfMonth(), today)
        }
        val summary = connection.spendingSummary(from.toString(), to.toString(), groupBy)
        if (kind == SpendingPeriodKind.Year) {
            val history = connection.cashFlow("${base.thisMonth.year - 19}-01", base.thisMonth.toString())
            SpendingRead(base, null, null, connection.savingSummaryOfYear(shownYear.toString()), connection.budgetStatusOfYear(shownYear.toString()), summary, history)
        } else {
            val toMonth = if (shownMonth < base.thisMonth.minusMonths(11)) shownMonth else base.thisMonth
            val flow = connection.cashFlow(toMonth.minusMonths(11).toString(), toMonth.toString())
            SpendingRead(
                base, flow, connection.spendingByDay(shownMonth.toString(), shownMonth.minusMonths(1).toString()),
                connection.savingSummary(shownMonth.toString()), connection.budgetStatus(shownMonth.toString()), summary, null,
            )
        }
    }
    TeaNodeScreen(title = stringResource(R.string.finance_part_spending), onBack = { navigator.back() }) { padding ->
        LoadedContent(read, modifier = Modifier.padding(padding)) { loaded ->
            val base = loaded.base
            val shownMonth = month ?: base.thisMonth
            val shownYear = year ?: base.thisMonth.year
            val currency = loaded.summary?.reportingCurrencyCode?.takeIf { code -> code.isNotEmpty() } ?: base.reportingCurrency
            val today = LocalDate.now(base.zone)
            val range = if (kind == SpendingPeriodKind.Year) LocalDate.of(shownYear, 1, 1) to minOf(LocalDate.of(shownYear, 12, 31), today) else shownMonth.atDay(1) to minOf(shownMonth.atEndOfMonth(), today)
            val lines = SpendingLines.of(loaded.summary, groupBy, base.categories, { id -> base.categoryName(id) }, stringResource(R.string.finance_unknown))
            val total = lines.fold(BigDecimal.ZERO) { sum, line -> sum + line.amount }

            fun filter(groupKey: String): FinanceTransactionFilter {
                var filter = FinanceTransactionFilter(from = range.first.toString(), to = range.second.toString())
                filter = when (groupBy) {
                    "spendingCategory" -> if (groupKey.isEmpty()) filter.copy(isUncategorized = true) else filter.copy(spendingCategoryId = groupKey)
                    "financeAccount" -> filter.copy(financeAccountId = groupKey)
                    else -> filter.copy(text = lines.firstOrNull { line -> line.id == groupKey }?.label ?: groupKey)
                }
                return filter
            }
            val slices = if (lines.isEmpty()) emptyList() else slicesOf(lines)
            val cashFlowPairs = loaded.cashFlow?.cashFlowMonths.orEmpty().filterNotNull().map { each -> BarPair(each.cashFlowMonth.takeLast(2), each.incomeAmount?.toDoubleOrNull() ?: 0.0, each.spendingAmount?.toDoubleOrNull() ?: 0.0) }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                item("period") {
                    Column(Modifier.padding(horizontal = 16.dp)) {
                        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                            SegmentedButton(kind == SpendingPeriodKind.Month, { kind = SpendingPeriodKind.Month }, SegmentedButtonDefaults.itemShape(0, 2)) { Text(stringResource(R.string.finance_month)) }
                            SegmentedButton(kind == SpendingPeriodKind.Year, { kind = SpendingPeriodKind.Year }, SegmentedButtonDefaults.itemShape(1, 2)) { Text(stringResource(R.string.finance_year)) }
                        }
                    }
                    if (kind == SpendingPeriodKind.Month) MonthStepper(shownMonth, base.thisMonth) { month = it }
                    else YearStepper(shownYear, loaded.history?.cashFlowMonths.orEmpty().filterNotNull().minOfOrNull { each -> each.cashFlowMonth.take(4).toIntOrNull() ?: shownYear } ?: shownYear, base.thisMonth.year) { year = it }
                }
                if (kind == SpendingPeriodKind.Month) {
                    val months = loaded.cashFlow?.cashFlowMonths.orEmpty().filterNotNull()
                    if (months.isNotEmpty()) {
                        item("cash-flow-header") { TeaNodeSectionHeader(stringResource(R.string.finance_cash_flow)) }
                        item("cash-flow") { CashFlowChart(currency = currency, pairs = months.map { each -> BarPair(each.cashFlowMonth.takeLast(2), each.incomeAmount?.toDoubleOrNull() ?: 0.0, each.spendingAmount?.toDoubleOrNull() ?: 0.0) }) }
                    }
                    loaded.byDay?.let { byDay ->
                        item("by-day-header") { TeaNodeSectionHeader(stringResource(R.string.finance_day_by_day)) }
                        item("by-day") { DayByDay(byDay, currency) }
                    }
                } else {
                    val years = yearsOf(loaded.history)
                    if (years.isNotEmpty()) {
                        item("years-header") { TeaNodeSectionHeader(stringResource(R.string.finance_cash_flow_years)) }
                        item("years") { CashFlowChart(years) }
                    }
                }
                loaded.saving?.let { saving ->
                    item("saving-header") { TeaNodeSectionHeader(stringResource(R.string.finance_saving)) }
                    item("income") { MoneyRow(stringResource(R.string.finance_income), saving.incomeAmount, saving.expectedIncomeAmount, saving.reportingCurrencyCode) }
                    item("spending") { MoneyRow(stringResource(R.string.finance_part_spending), saving.spendingAmount, saving.expectedSpendingAmount, saving.reportingCurrencyCode) }
                    item("saved") { MoneyRow(stringResource(R.string.finance_saved), saving.savingAmount, saving.expectedSavingAmount, saving.reportingCurrencyCode) }
                    if (!saving.savingPace.isNullOrEmpty()) item("pace") { Box(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) { PaceTag(saving.savingPace!!) } }
                }
                val budgetRows = loaded.budgets?.spendingCategories.orEmpty().filterNotNull()
                if (budgetRows.isNotEmpty()) {
                    item("budgets-header") { TeaNodeSectionHeader(stringResource(R.string.finance_part_budgets)) }
                    for (row in budgetRows) {
                        item("budget-" + row.spendingCategoryId) {
                            val name = if (base.category(row.spendingCategoryId) != null) base.categoryName(row.spendingCategoryId) else FinanceCategoryNames.shown(context, row.spendingCategoryName ?: "")
                            Box(Modifier.clickable { navigator.navigate(FinanceRoutes.transactions(FinanceTransactionFilter(from = range.first.toString(), to = range.second.toString(), spendingCategoryId = row.spendingCategoryId))) }) {
                                BudgetRow(name, row.spendingAmount, row.budgetAmount, row.projectedAmount, row.budgetPace, row.currencyCode)
                            }
                        }
                    }
                }
                item("where-header") { TeaNodeSectionHeader(stringResource(R.string.finance_where_it_went)) }
                item("group-by") {
                    val groups = listOf("spendingCategory" to R.string.finance_group_category, "merchant" to R.string.finance_group_merchant, "financeAccount" to R.string.finance_group_account)
                    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                        groups.forEachIndexed { index, (key, label) ->
                            SegmentedButton(groupBy == key, { groupBy = key }, SegmentedButtonDefaults.itemShape(index, groups.size)) { Text(stringResource(label), maxLines = 1) }
                        }
                    }
                }
                if (lines.isEmpty()) {
                    item("none") { Text(stringResource(if (kind == SpendingPeriodKind.Month) R.string.finance_no_spending_month else R.string.finance_no_spending_year), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp)) }
                } else {
                    item("donut") {
                        Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                            DonutChart(slices.map { slice -> DonutSlice(slice.id, slice.amount.toDouble(), slice.color) }, selectedSliceId) {
                                val chosen = slices.firstOrNull { slice -> slice.id == selectedSliceId }
                                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                                    Text(chosen?.label ?: stringResource(R.string.finance_total), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center, maxLines = 2)
                                    Text(FinanceMoney.shown(chosen?.amount ?: total, currency), style = MaterialTheme.typography.titleMedium)
                                }
                            }
                        }
                    }
                    lines.forEachIndexed { rank, line ->
                        val slice = slices.firstOrNull { each -> each.id == line.id } ?: slices.lastOrNull { each -> each.id == REST_SLICE_ID && rank >= FinancePalette.size }
                        item("line-" + line.id) {
                            SpendingLineRow(line, slice?.color, currency, shareText(line.amount, total)) {
                                selectedSliceId = slice?.id
                                navigator.navigate(FinanceRoutes.transactions(filter(line.id)))
                            }
                        }
                    }
                }
            }
        }
    }
}

private const val REST_SLICE_ID = "\u0000rest"

private class SpendingSlice(val id: String, val label: String, val amount: BigDecimal, val color: Color)

@Composable
private fun slicesOf(lines: List<SpendingLine>): List<SpendingSlice> {
    val drawn = lines.take(FinancePalette.size)
    val slices = drawn.mapIndexed { index, line -> SpendingSlice(line.id, line.label, line.amount, FinancePalette.color(index)) }.toMutableList()
    val rest = lines.drop(FinancePalette.size)
    if (rest.isNotEmpty()) slices.add(SpendingSlice(REST_SLICE_ID, stringResource(R.string.finance_other_groups), rest.fold(BigDecimal.ZERO) { sum, line -> sum + line.amount }, MaterialTheme.colorScheme.outline))
    return slices
}

private fun shareText(amount: BigDecimal, total: BigDecimal): String =
    if (total.signum() <= 0) "" else FinanceMoney.percent(amount.toDouble() / total.toDouble())

@Composable
private fun SpendingLineRow(line: SpendingLine, color: Color?, currency: String, shareText: String, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.size(12.dp).background(color ?: Color.Transparent, CircleShape))
        Column(Modifier.weight(1f)) {
            Text(line.label, style = MaterialTheme.typography.bodyLarge)
            Text(pluralTransactions(line.count), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Column(horizontalAlignment = Alignment.End) {
            Text(FinanceMoney.shown(line.amount, currency), style = MaterialTheme.typography.bodyLarge)
            Text(shareText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun pluralTransactions(count: Int): String = androidx.compose.ui.platform.LocalContext.current.resources.getQuantityString(R.plurals.finance_transaction_count, count, count)

/** A category's spending against its budget. */
@Composable
internal fun BudgetRow(name: String, spent: String?, budget: String?, projected: String?, pace: String?, currency: String?) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(name, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
            if (!pace.isNullOrEmpty()) PaceTag(pace)
        }
        val spentValue = FinanceMoney.decimal(spent)?.toDouble() ?: 0.0
        val budgetValue = FinanceMoney.decimal(budget)?.toDouble() ?: 0.0
        if (budgetValue > 0) {
            androidx.compose.material3.LinearProgressIndicator(progress = { (spentValue / budgetValue).coerceIn(0.0, 1.0).toFloat() }, modifier = Modifier.fillMaxWidth())
        }
        Text(
            stringResource(R.string.finance_spent_of_budget, FinanceMoney.shown(spent, currency), FinanceMoney.shown(budget, currency)) +
                if (!projected.isNullOrEmpty()) " · " + stringResource(R.string.finance_projected, FinanceMoney.shown(projected, currency)) else "",
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun CashFlowChart(pairs: List<BarPair>, currency: String = "USD") {
    var selected by remember { mutableStateOf<Int?>(null) }
    Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            LegendEntry(stringResource(R.string.finance_income), FinancePalette.color(0))
            LegendEntry(stringResource(R.string.finance_part_spending), FinancePalette.color(1))
        }
        PairedBarChart(pairs, FinancePalette.color(0), FinancePalette.color(1), selectedIndex = selected, onSelect = { selected = it })
        selected?.let { index -> pairs.getOrNull(index)?.let { pair -> Text(stringResource(R.string.finance_callout_cash_flow, pair.label, FinanceMoney.shown(java.math.BigDecimal(pair.first), currency), FinanceMoney.shown(java.math.BigDecimal(pair.second), currency), FinanceMoney.shown(java.math.BigDecimal(pair.first - pair.second), currency)), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary) } }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            for (pair in pairs) Text(pair.label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun LegendEntry(label: String, color: Color) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Box(Modifier.size(10.dp).background(color, CircleShape))
        Text(label, style = MaterialTheme.typography.labelMedium)
    }
}

/** The chosen month's spending added up day by day, and where the month before stood on the same days. */
@Composable
private fun DayByDay(byDay: SpendingByDay, currency: String = "USD") {
    var selected by remember { mutableStateOf<Int?>(null) }
    val month = (byDay.monthDays.orEmpty().filterNotNull().map { day -> day.cumulativeSpendingAmount?.toDoubleOrNull() ?: 0.0 })
    val before = (byDay.compareMonthDays.orEmpty().filterNotNull().map { day -> day.cumulativeSpendingAmount?.toDoubleOrNull() ?: 0.0 })
    Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (month.isEmpty() && before.isEmpty()) {
            Text(stringResource(R.string.finance_no_spending_month), color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                LegendEntry(monthName(byDay.month), FinancePalette.color(0))
                LegendEntry(monthName(byDay.compareMonth), FinancePalette.color(1))
            }
            LineChart(listOf(LineSeries(before, FinancePalette.color(1), isDashed = true), LineSeries(month, FinancePalette.color(0))), isZeroIncluded = true, selectedIndex = selected, onSelect = { selected = it })
            selected?.let { index ->
                val spent = month.getOrNull(index); val spentBefore = before.getOrNull(index)
                val text = listOfNotNull("${index + 1}", spent?.let { value -> FinanceMoney.shown(java.math.BigDecimal(value), currency) }, spentBefore?.let { value -> FinanceMoney.shown(java.math.BigDecimal(value), currency) }, if (spent != null && spentBefore != null) FinanceMoney.shown(java.math.BigDecimal(Math.abs(spent - spentBefore)), currency) + " " + stringResource(if (spent > spentBefore) R.string.finance_callout_more else if (spent < spentBefore) R.string.finance_callout_less else R.string.finance_callout_same) else null).joinToString(" · ")
                Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
            }
            Text(stringResource(R.string.finance_day_by_day_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** Every year's income and spending, added up from its months as the server counted each. */
private fun yearsOf(history: CashFlow?): List<BarPair> {
    val months = history?.cashFlowMonths.orEmpty().filterNotNull()
    return months.groupBy { month -> month.cashFlowMonth.take(4) }.toSortedMap().map { (year, ofYear) ->
        BarPair(year.takeLast(2), ofYear.sumOf { month -> month.incomeAmount?.toDoubleOrNull() ?: 0.0 }, ofYear.sumOf { month -> month.spendingAmount?.toDoubleOrNull() ?: 0.0 })
    }
}

/** "November 2030" for "2030-11"; the text as it came when it is not a month. */
private fun monthName(text: String?): String = try {
    val month = YearMonth.parse(text)
    "${month.month.getDisplayName(java.time.format.TextStyle.FULL, java.util.Locale.getDefault())} ${month.year}"
} catch (_: Exception) {
    text.orEmpty()
}
