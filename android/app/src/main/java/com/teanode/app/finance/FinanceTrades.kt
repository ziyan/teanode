package com.teanode.app.finance

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.SwapHoriz
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
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

/** An account's buys, sells and transfers of securities, newest first. */
@Composable
fun FinanceTradesScreen(accountId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val trades = rememberLoad(connection, accountId) { connection.financeTrades(accountId, limit = 200).financeTrades.filterNotNull() }
    TeaNodeScreen(title = stringResource(R.string.finance_trades), onBack = { navigator.back() }) { padding ->
        LoadedContent(
            trades, modifier = Modifier.padding(padding), isEmpty = { list -> list.isEmpty() },
            empty = { EmptyState(Icons.Outlined.SwapHoriz, stringResource(R.string.finance_no_trades)) },
        ) { list ->
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                items(list, key = { trade -> trade.id }) { trade -> TradeRow(trade) }
            }
        }
    }
}

@Composable
private fun TradeRow(trade: FinanceTrade) {
    TeaNodeRow(
        headline = trade.shownName,
        supporting = listOfNotNull(trade.tradedOn, trade.tradeKind, trade.tradedQuantity?.takeIf { quantity -> quantity.isNotEmpty() }?.let { quantity -> "$quantity × ${FinanceMoney.shown(trade.unitPrice, trade.currencyCode)}" }).joinToString(" · "),
        trailing = { AmountText(trade.tradeAmount, trade.currencyCode) },
    )
}
