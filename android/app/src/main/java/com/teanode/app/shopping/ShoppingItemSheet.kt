package com.teanode.app.shopping

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.ShoppingItemPrice
import com.teanode.kit.shopping.ShoppingMoney
import com.teanode.kit.shopping.ShoppingPriceHistory
import com.teanode.kit.shopping.ShoppingQuantity
import com.teanode.kit.shopping.findShoppingItemPicture
import com.teanode.kit.shopping.recordShoppingItemPrice
import com.teanode.kit.shopping.setShoppingItemOutOfStock
import com.teanode.kit.shopping.setShoppingItemPicture
import com.teanode.kit.shopping.shoppingItem
import com.teanode.kit.shopping.shoppingItemPrices
import com.teanode.kit.shopping.updateShoppingItem
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** One item: its picture, what it is, how many are needed, where to get it and what it costs, with marking it out of stock and taking it off the list. */
@Composable
internal fun ShoppingItemSheet(
    connection: Connection,
    item: ShoppingItem,
    onDismiss: () -> Unit,
    onChanged: (ShoppingItem) -> Unit,
    onRemove: (ShoppingItem) -> Unit,
    onNeedAgain: ((String) -> Unit)?,
) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    var shown by remember { mutableStateOf(item) }
    var itemDescription by remember { mutableStateOf(item.itemDescription) }
    var itemDetails by remember { mutableStateOf(item.itemDetails) }
    var quantity by remember { mutableStateOf(ShoppingQuantity.value(item.neededQuantity)) }
    var quantityUnit by remember { mutableStateOf(item.quantityUnit) }
    var storeName by remember { mutableStateOf(item.storeName) }
    var aisleName by remember { mutableStateOf(item.aisleName) }
    var isStoreBrand by remember { mutableStateOf(item.isStoreBrand) }
    var unitPriceText by remember { mutableStateOf(ShoppingMoney.typedAmount(item.unitPriceAmount)) }
    var prices by remember { mutableStateOf<List<ShoppingItemPrice>>(emptyList()) }
    var isSearchingPicture by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    var isConfirmingRemove by remember { mutableStateOf(false) }
    var isShowingPrices by remember { mutableStateOf(false) }

    LaunchedEffect(item.id) {
        prices = try {
            connection.shoppingItemPrices(item.id)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            emptyList()
        }
    }
    val photoLauncher = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            isWorking = true
            try {
                val file = context.readPendingFile(uri, "picture.jpg")
                val attachmentId = connection.upload(file.name, file.contentType, file.data)
                val fresh = connection.setShoppingItemPicture(item.id, attachmentId)
                shown = fresh
                onChanged(fresh)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            isWorking = false
        }
    }

    fun searchForPicture() {
        scope.launch {
            isSearchingPicture = true
            val before = shown.picturePath
            try {
                connection.findShoppingItemPicture(item.id)
                repeat(20) {
                    delay(3_000)
                    val fresh = try {
                        connection.shoppingItem(item.id)
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (_: Throwable) {
                        null
                    }
                    if (fresh != null && fresh.picturePath != before) {
                        shown = fresh
                        onChanged(fresh)
                        isSearchingPicture = false
                        return@launch
                    }
                }
                snackbar.show(context.getString(R.string.shopping_no_other_picture))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            isSearchingPicture = false
        }
    }

    fun save() {
        val typedPrice = unitPriceText.trim()
        if (typedPrice.isNotEmpty() && !ShoppingMoney.isPrice(typedPrice)) {
            snackbar.showProblem(context.getString(R.string.shopping_type_the_price))
            return
        }
        scope.launch {
            isWorking = true
            val newQuantity = ShoppingQuantity.text(quantity)
            val trimmedDescription = itemDescription.trim()
            val trimmedStore = storeName.trim()
            try {
                var fresh = shown
                val hasChanged = trimmedDescription != item.itemDescription || itemDetails != item.itemDetails || trimmedStore != item.storeName ||
                    aisleName != item.aisleName || isStoreBrand != item.isStoreBrand || newQuantity != item.shownQuantity || quantityUnit != item.quantityUnit
                if (hasChanged) {
                    fresh = connection.updateShoppingItem(item.id, trimmedDescription, itemDetails, trimmedStore, isStoreBrand, aisleName, newQuantity, quantityUnit)
                }
                if (typedPrice.isNotEmpty() && typedPrice != ShoppingMoney.typedAmount(item.unitPriceAmount)) {
                    connection.recordShoppingItemPrice(item.id, typedPrice, trimmedStore, item.currencyCode)
                    fresh = connection.shoppingItem(item.id)
                }
                onChanged(fresh)
                onDismiss()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            isWorking = false
        }
    }

    ShoppingSheet(
        stringResource(R.string.shopping_item), onDismiss, confirmLabel = stringResource(R.string.shopping_save),
        isConfirmEnabled = !isWorking && itemDescription.isNotBlank(), onConfirm = ::save,
    ) {
        Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            ShoppingPictureView(shown, size = 120.dp)
            if (isSearchingPicture) Text(stringResource(R.string.shopping_looking_for_picture), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                TextButton(onClick = ::searchForPicture, enabled = !isWorking && !isSearchingPicture) { Text(stringResource(R.string.shopping_find_another)) }
                TextButton(onClick = { photoLauncher.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }, enabled = !isWorking) { Text(stringResource(R.string.shopping_receipt_from_photos)) }
                if (shown.hasPicture) {
                    TextButton(
                        enabled = !isWorking,
                        onClick = {
                            scope.launch {
                                isWorking = true
                                try {
                                    val fresh = connection.setShoppingItemPicture(item.id, null)
                                    shown = fresh
                                    onChanged(fresh)
                                } catch (failure: CancellationException) {
                                    throw failure
                                } catch (failure: Throwable) {
                                    snackbar.showError(failure)
                                }
                                isWorking = false
                            }
                        },
                    ) { Text(stringResource(R.string.shopping_remove_the_picture)) }
                }
            }
        }
        OutlinedTextField(itemDescription, { itemDescription = it }, label = { Text(stringResource(R.string.shopping_what)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(itemDetails, { itemDetails = it }, label = { Text(stringResource(R.string.shopping_item_details_hint)) }, modifier = Modifier.fillMaxWidth())
        Text(stringResource(R.string.shopping_how_many), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        ShoppingQuantityStepper(quantity) { quantity = it }
        OutlinedTextField(quantityUnit, { quantityUnit = it }, label = { Text(stringResource(R.string.shopping_unit_hint)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        Text(stringResource(R.string.shopping_where), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        OutlinedTextField(storeName, { storeName = it }, label = { Text(stringResource(R.string.shopping_item_store)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(aisleName, { aisleName = it }, label = { Text(stringResource(R.string.shopping_item_aisle)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.shopping_item_store_brand), modifier = Modifier.weight(1f))
            Switch(isStoreBrand, { isStoreBrand = it })
        }
        Text(stringResource(R.string.shopping_latest_price), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        OutlinedTextField(
            unitPriceText, { unitPriceText = it }, label = { Text(stringResource(R.string.shopping_price_amount)) }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal),
            supportingText = if (shown.currencyCode.isNotEmpty()) ({ Text(stringResource(R.string.shopping_in_currency, shown.currencyCode)) }) else null,
        )
        if (ShoppingPriceHistory.chartable(prices).size > 1) ShoppingPriceChart(prices)
        if (prices.isNotEmpty()) TextButton(onClick = { isShowingPrices = true }) { Text(stringResource(R.string.shopping_all_prices)) }
        HorizontalDivider()
        if (shown.isNotNeeded && onNeedAgain != null) {
            OutlinedButton(onClick = { onNeedAgain(ShoppingQuantity.text(quantity)); onDismiss() }, enabled = !isWorking, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.shopping_need_again)) }
        }
        OutlinedButton(
            enabled = !isWorking, modifier = Modifier.fillMaxWidth(),
            onClick = {
                scope.launch {
                    isWorking = true
                    val isOut = !shown.isOutOfStock
                    try {
                        val fresh = connection.setShoppingItemOutOfStock(item.id, isOut)
                        shown = fresh
                        onChanged(fresh)
                        snackbar.show(context.getString(if (isOut) R.string.shopping_marked_out_of_stock else R.string.shopping_back_in_stock))
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        snackbar.showError(failure)
                    }
                    isWorking = false
                }
            },
        ) { Text(stringResource(if (shown.isOutOfStock) R.string.shopping_in_stock_again else R.string.shopping_mark_out_of_stock)) }
        OutlinedButton(onClick = { isConfirmingRemove = true }, enabled = !isWorking, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.shopping_remove_from_the_list), color = MaterialTheme.colorScheme.error)
        }
    }
    if (isConfirmingRemove) {
        ConfirmationDialog(
            title = stringResource(R.string.shopping_remove_item_question, shown.itemDescription), text = stringResource(R.string.shopping_remove_footer),
            confirmLabel = stringResource(R.string.shopping_remove), isDestructive = true,
            onConfirm = { isConfirmingRemove = false; onRemove(shown); onDismiss() }, onDismiss = { isConfirmingRemove = false },
        )
    }
    if (isShowingPrices) ShoppingPricesSheet(prices, shown.itemDescription) { isShowingPrices = false }
}

/** An item's price over time, as a line. */
@Composable
internal fun ShoppingPriceChart(prices: List<ShoppingItemPrice>) {
    val points = ShoppingPriceHistory.chartable(prices)
    val lineColor = MaterialTheme.colorScheme.primary
    val amounts = points.map { price -> price.amount?.toDouble() ?: 0.0 }
    val low = amounts.min()
    val high = amounts.max()
    val span = if (high - low < 1e-9) 1.0 else high - low
    Canvas(Modifier.fillMaxWidth().height(140.dp).padding(vertical = 8.dp)) {
        val path = Path()
        points.forEachIndexed { index, _ ->
            val x = if (points.size == 1) size.width / 2 else size.width * index / (points.size - 1)
            val y = size.height - (size.height * ((amounts[index] - low) / span)).toFloat()
            if (index == 0) path.moveTo(x, y) else path.lineTo(x, y)
            drawCircle(lineColor, radius = 4.dp.toPx(), center = Offset(x, y))
        }
        drawPath(path, lineColor, style = Stroke(width = 2.dp.toPx()))
    }
}

/** Every price an item has been seen at, newest first. */
@Composable
internal fun ShoppingPricesSheet(prices: List<ShoppingItemPrice>, itemTitle: String, onDismiss: () -> Unit) {
    val words = rememberShoppingWords()
    ShoppingSheet(itemTitle, onDismiss, confirmLabel = stringResource(R.string.shopping_done), onConfirm = onDismiss) {
        if (prices.isEmpty()) {
            Text(stringResource(R.string.shopping_no_prices_title), style = MaterialTheme.typography.titleMedium)
            Text(stringResource(R.string.shopping_no_prices_description), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        for (price in prices) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(price.shownAmount)
                    Text(words.priceDetail(price), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Text(price.observedOn, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
