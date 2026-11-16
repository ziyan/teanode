package com.teanode.app.finance

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.SwapHoriz
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.ui.graphics.Color
import androidx.compose.material3.ListItem
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.finance.TradeKindWords
import com.teanode.kit.finance.TradeWords
import kotlinx.coroutines.CancellationException
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.finance.FinanceMoney
import com.teanode.kit.finance.FinanceTrade
import com.teanode.kit.finance.financeTrades
import com.teanode.kit.finance.shownName

/** An account's buys, sells and transfers of securities, newest first, fifty at a time. */
@Composable
fun FinanceTradesScreen(accountId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val firstPage = rememberLoad(connection, accountId) { connection.financeTrades(accountId, limit = TRADES_PAGE_SIZE) }
    var laterTrades by remember(accountId) { mutableStateOf<List<FinanceTrade>>(emptyList()) }
    TeaNodeScreen(title = stringResource(R.string.finance_trades), onBack = { navigator.back() }) { padding ->
        LoadedContent(
            firstPage, modifier = Modifier.padding(padding), isEmpty = { page -> page.financeTrades.isEmpty() },
            empty = { EmptyState(Icons.Outlined.SwapHoriz, stringResource(R.string.finance_no_trades), description = stringResource(R.string.finance_no_trades_description)) },
        ) { page ->
            val trades = (page.financeTrades.filterNotNull() + laterTrades).distinctBy { trade -> trade.id }
            val totalCount = page.totalCount ?: trades.size
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                if (totalCount > 0) item("count") { Text(pluralStringResource(R.plurals.finance_trades_count, totalCount, totalCount), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                items(trades, key = { trade -> trade.id }) { trade ->
                    TradeRow(trade)
                    // The last one on screen reads the next page, if the account has more.
                    if (trade.id == trades.last().id && TradeWords.hasMore(trades.size, totalCount)) {
                        LaunchedEffect(trades.size) {
                            try {
                                laterTrades = laterTrades + connection.financeTrades(accountId, limit = TRADES_PAGE_SIZE, offset = trades.size).financeTrades.filterNotNull()
                            } catch (failure: CancellationException) {
                                throw failure
                            } catch (failure: Throwable) {
                                snackbar.showError(failure)
                            }
                        }
                    }
                }
                item("footer") { Footnote(stringResource(R.string.finance_trades_footer)) }
            }
        }
    }
}

private const val TRADES_PAGE_SIZE = 50

@Composable
private fun TradeRow(trade: FinanceTrade) {
    val kindText = when (TradeWords.kind(trade.tradeKind)) {
        TradeKindWords.Buy -> stringResource(R.string.finance_trade_buy)
        TradeKindWords.Sell -> stringResource(R.string.finance_trade_sell)
        TradeKindWords.Canceled -> stringResource(R.string.finance_trade_canceled)
        TradeKindWords.Transfer -> stringResource(R.string.finance_trade_transfer)
        null -> trade.tradeKind
    }
    val quantity = TradeWords.quantity(trade)
    val quantityText = quantity?.let { read ->
        if (read.unitPriceAmount == null) read.quantityText else stringResource(R.string.finance_trade_quantity_at, read.quantityText, FinanceMoney.shown(read.unitPriceAmount, trade.currencyCode))
    }
    // "2030-10-01 · Buy · 2 at $100.00": when, the kind, and the quantity.
    val secondaryLine = listOfNotNull(trade.tradedOn, kindText, quantityText).filter { text -> text.isNotEmpty() }.joinToString(" · ")
    ListItem(
        headlineContent = { Text(trade.shownName, maxLines = 1) },
        supportingContent = {
            Column {
                Text(secondaryLine)
                if (TradeWords.hasFee(trade)) Text(stringResource(R.string.finance_trade_fee, FinanceMoney.shown(trade.feeAmount, trade.currencyCode)))
            }
        },
        trailingContent = {
            Text(FinanceMoney.shown(trade.tradeAmount, trade.currencyCode), color = if (TradeWords.isCashIn(trade)) Color(0xFF2E7D32) else MaterialTheme.colorScheme.onSurface)
        },
    )
}
