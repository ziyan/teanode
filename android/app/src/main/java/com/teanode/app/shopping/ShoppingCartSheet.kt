package com.teanode.app.shopping

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.ShoppingMoney

/**
 * Your cart, which Done shopping opens: what the person put in their own cart, each with a check that takes it out,
 * what it should come to, and the two ways to finish: Snap the receipt (prominent), or Finish without a receipt.
 * Closing the sheet keeps shopping. While [busyText] is set (the receipt is on its way) the buttons are off.
 */
@Composable
internal fun ShoppingCartSheet(
    items: List<ShoppingItem>,
    showsListName: Boolean,
    busyText: String?,
    onTakeOut: (ShoppingItem) -> Unit,
    onSnapReceipt: () -> Unit,
    onFinishWithoutReceipt: () -> Unit,
    onDismiss: () -> Unit,
) {
    val total = ShoppingMoney.estimate(items)
    val isBusy = busyText != null
    ShoppingSheet(
        title = stringResource(R.string.shopping_cart_title), onDismiss = onDismiss, dismissLabel = stringResource(R.string.shopping_close),
        footer = {
            if (busyText != null) {
                Text(busyText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
            Button(onClick = onSnapReceipt, enabled = !isBusy, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                Text(stringResource(R.string.shopping_snap_the_receipt))
            }
            OutlinedButton(onClick = onFinishWithoutReceipt, enabled = !isBusy, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                Text(stringResource(R.string.shopping_finish_without_receipt))
            }
        },
    ) {
        if (items.isEmpty()) {
            Text(stringResource(R.string.shopping_cart_empty), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else {
            Text(
                if (total == null) pluralStringResource(R.plurals.shopping_cart_summary, items.size, items.size)
                else pluralStringResource(R.plurals.shopping_cart_summary_priced, items.size, items.size, total.shown()),
                style = MaterialTheme.typography.titleMedium,
            )
            Column {
                for (item in items) ShoppingCartRow(item, showsListName, enabled = !isBusy, onTakeOut = { onTakeOut(item) })
            }
        }
    }
}

/** One item in the cart: its filled check (which takes it out), picture, count and description, list, and price. */
@Composable
private fun ShoppingCartRow(item: ShoppingItem, showsListName: Boolean, enabled: Boolean, onTakeOut: () -> Unit) {
    val price = ShoppingMoney.shownLineTotal(item)
    Row(Modifier.fillMaxWidth().heightIn(min = 64.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        ShoppingCheckButton(ShoppingListCheck(true, enabled, stringResource(R.string.shopping_take_out_of_cart, item.itemDescription), onTakeOut))
        ShoppingPictureView(item)
        Column(Modifier.weight(1f).padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(item.shownTitle, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (showsListName) Text(item.listName, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (price.isNotEmpty()) Text(price, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
