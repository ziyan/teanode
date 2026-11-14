package com.teanode.app.finance

import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceTransactionFilter
import java.time.YearMonth
import java.time.format.TextStyle
import java.util.Locale

/** The places of Finance, as routes. */
internal object FinanceRoutes {
    private fun child(path: String) = Routes.child(AppSection.Finance, path)

    val spending = child("spending")
    val accounts = child("accounts")
    val budgets = child("budgets")
    val netWorth = child("net-worth")
    val savingsTargets = child("savings-targets")
    val receipts = child("receipts")
    val taxes = child("taxes")
    val sharing = child("sharing")
    val setup = child("setup")
    val sharedAll = child("shared-all")
    val receiptEntry = child("receipt-entry")
    val paystubEntry = child("paystub-entry")
    val taxFormEntry = child("tax-form-entry")
    val importCredential = child("import-credential")
    val importRows = child("import-rows")

    const val TRANSACTIONS_PATH = "transactions?from={from}&to={to}&account={account}&category={category}&text={text}&isUncategorized={isUncategorized}"
    val transactionsPattern = child(TRANSACTIONS_PATH)
    val transactionPattern = child("transaction/{id}")
    val assetPattern = child("asset/{id}")
    val receiptPattern = child("receipt/{id}")
    val paystubPattern = child("paystub/{id}")
    val taxFormPattern = child("tax-form/{id}")
    val sharedPattern = child("shared/{ownerUserId}")
    val sharedAccountPattern = child("shared-account/{id}")
    val sharedAllPattern = child("shared-all/{ownerUserId}")
    val tradesPattern = child("trades/{id}")

    fun transactions(filter: FinanceTransactionFilter = FinanceTransactionFilter()): String = child(
        "transactions?from=${Uri.encode(filter.from)}&to=${Uri.encode(filter.to)}&account=${Uri.encode(filter.financeAccountId)}" +
            "&category=${Uri.encode(filter.spendingCategoryId)}&text=${Uri.encode(filter.text)}&isUncategorized=${filter.isUncategorized}",
    )

    fun transaction(id: String) = child("transaction/${Uri.encode(id)}")
    fun asset(id: String) = child("asset/${Uri.encode(id)}")
    fun receipt(id: String) = child("receipt/${Uri.encode(id)}")
    fun paystub(id: String) = child("paystub/${Uri.encode(id)}")
    fun taxForm(id: String) = child("tax-form/${Uri.encode(id)}")
    fun shared(ownerUserId: String) = child("shared/${Uri.encode(ownerUserId)}")
    fun sharedAccount(id: String) = child("shared-account/${Uri.encode(id)}")
    fun sharedAllOf(ownerUserId: String) = child("shared-all/${Uri.encode(ownerUserId)}")
    fun trades(accountId: String) = child("trades/${Uri.encode(accountId)}")
}

/** An amount as a row's end: the text in its currency, red for money out as the dashboard draws it. */
@Composable
internal fun AmountText(amount: String?, currency: String?, modifier: Modifier = Modifier, isSignColored: Boolean = true) {
    val value = FinanceMoney.decimal(amount)
    val color = when {
        !isSignColored || value == null -> MaterialTheme.colorScheme.onSurface
        value.signum() < 0 -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.onSurface
    }
    Text(FinanceMoney.shown(amount, currency), modifier = modifier, color = color, style = MaterialTheme.typography.bodyLarge)
}

/** A label and its value on one line, the value ending the line. */
@Composable
internal fun LabeledValue(label: String, value: String, modifier: Modifier = Modifier, supporting: String? = null) {
    Row(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Column(horizontalAlignment = Alignment.End) {
            Text(value, style = MaterialTheme.typography.bodyLarge)
            if (!supporting.isNullOrEmpty()) Text(supporting, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** An amount so far, and what was expected, side by side. */
@Composable
internal fun MoneyRow(title: String, amount: String?, expected: String?, currency: String?) {
    LabeledValue(
        title, FinanceMoney.shown(amount, currency),
        supporting = if (expected.isNullOrEmpty()) null else stringResource(R.string.finance_of_amount, FinanceMoney.shown(expected, currency)),
    )
}

/** How a budget is going, in words and color. */
@Composable
internal fun PaceTag(pace: String) {
    val (text, color) = when (pace) {
        "under", "on_track", "ahead" -> stringResource(R.string.finance_pace_on_track) to Color(0xFF2E7D32)
        "at_risk", "behind" -> stringResource(R.string.finance_pace_at_risk) to Color(0xFFEF6C00)
        "over" -> stringResource(R.string.finance_pace_over) to MaterialTheme.colorScheme.error
        else -> pace to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Text(text, color = color, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.Medium)
}

/** Moves between months: back, forward, never past this one. */
@Composable
internal fun MonthStepper(month: YearMonth, latest: YearMonth, onChange: (YearMonth) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
        IconButton(onClick = { onChange(month.minusMonths(1)) }) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowLeft, contentDescription = stringResource(R.string.finance_earlier)) }
        Text("${month.month.getDisplayName(TextStyle.FULL, Locale.getDefault())} ${month.year}", style = MaterialTheme.typography.titleMedium)
        IconButton(onClick = { onChange(month.plusMonths(1)) }, enabled = month < latest) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = stringResource(R.string.finance_later)) }
    }
}

/** Moves between years: back to the first with money in or out, forward never past this one. */
@Composable
internal fun YearStepper(year: Int, firstYear: Int, currentYear: Int, onChange: (Int) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
        IconButton(onClick = { onChange(year - 1) }, enabled = year > firstYear) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowLeft, contentDescription = stringResource(R.string.finance_earlier)) }
        Text(year.toString(), style = MaterialTheme.typography.titleMedium)
        IconButton(onClick = { onChange(year + 1) }, enabled = year < currentYear) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = stringResource(R.string.finance_later)) }
    }
}

/** A caption under a group of rows. */
@Composable
internal fun Footnote(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
}
