package com.teanode.app.shopping

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CameraAlt
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
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
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.shopping.ShoppingItem
import kotlinx.coroutines.launch

/** The screen of shopping one store; an empty [storeName] is any store. */
@Composable
fun ShoppingStoreScreen(storeName: String, listIds: List<String>, initialLayout: ShoppingLayoutChoice = ShoppingLayoutChoice.List) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val words = rememberShoppingWords()
    val context = LocalContext.current
    val facts by session.facts.collectAsState()
    val ownUserId = facts.session?.userId
    val state = remember(connection, storeName) { ShoppingStoreState(connection, storeName, listIds, scope, snackbar, ownUserId) }
    val receiptReader = remember(connection) { ShoppingReceiptReader(connection, scope, snackbar, { text -> snackbar.show(text) }, context) }
    var layout by remember { mutableStateOf(initialLayout) }
    var isRefreshing by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isConfirmingFinish by remember { mutableStateOf(false) }
    var isChoosingReceiptSource by remember { mutableStateOf(false) }
    var menuItem by remember { mutableStateOf<ShoppingItem?>(null) }
    var opened by remember { mutableStateOf<ShoppingItem?>(null) }

    LaunchedEffect(state) { state.load() }
    FollowShopping(connection, session.isDemo) { state.load() }

    val photoLauncher = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri != null) receiptReader.scan(context.readPendingFile(uri, "receipt.jpg"))
    }
    val cameraLauncher = rememberLauncherForActivityResult(ActivityResultContracts.TakePicturePreview()) { bitmap ->
        if (bitmap != null) receiptReader.scan(bitmap.toPendingFile("receipt.jpg"))
    }

    TeaNodeScreen(
        title = storeName.ifEmpty { stringResource(R.string.shopping_any_store) },
        onBack = { navigator.back() },
        actions = {
            IconButton(onClick = { layout = if (layout == ShoppingLayoutChoice.List) ShoppingLayoutChoice.Pictures else ShoppingLayoutChoice.List }) {
                Icon(if (layout == ShoppingLayoutChoice.List) Icons.Outlined.GridView else Icons.Outlined.ViewList, contentDescription = stringResource(R.string.shopping_layout))
            }
            Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.shopping_more)) }
                DropdownMenu(isMenuOpen, { isMenuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.shopping_i_am_here)) }, enabled = storeName.isNotEmpty(),
                        trailingIcon = { Switch(state.isReminded, null, enabled = storeName.isNotEmpty()) },
                        onClick = { state.setReminder(!state.isReminded) },
                    )
                }
            }
        },
        floatingActionButton = {
            if (state.hasLoaded) {
                Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (state.isShopping) {
                        ExtendedFloatingActionButton(onClick = { isConfirmingFinish = true }, icon = { Icon(Icons.Outlined.Check, null) }, text = { Text(stringResource(R.string.shopping_done_shopping_button)) })
                    }
                    ExtendedFloatingActionButton(
                        onClick = { if (receiptReader.readingText == null) isChoosingReceiptSource = true },
                        icon = { Icon(Icons.Outlined.CameraAlt, null) }, text = { Text(stringResource(R.string.shopping_scan_the_receipt)) },
                    )
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            val failure = state.loadFailure
            val sections = state.sections
            when {
                failure != null && !state.hasLoaded -> ErrorState(session.describeLoadProblem(failure), onRetry = { state.reload() })
                !state.hasLoaded -> LoadingState()
                else -> PullToRefreshBox(
                    isRefreshing = isRefreshing,
                    onRefresh = { scope.launch { isRefreshing = true; state.load(); isRefreshing = false } },
                ) {
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 168.dp)) {
                        item("header") {
                            Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                val progress = state.progress
                                Text(words.progressLine(progress), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                if (progress.totalCount > 0) {
                                    LinearProgressIndicator(progress = { progress.inCartCount.toFloat() / progress.totalCount }, modifier = Modifier.fillMaxWidth())
                                }
                                words.otherShoppersLine(state.otherShopperNames)?.let { line ->
                                    Text(line, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                                receiptReader.readingText?.let { text ->
                                    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
                                }
                                if (state.lists.size > 1) {
                                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                        for (list in state.lists) {
                                            FilterChip(
                                                selected = list.isIncluded, onClick = { state.setIncluded(list, !list.isIncluded) },
                                                label = { Text("${list.listName} · ${list.neededCount}", maxLines = 1) },
                                            )
                                        }
                                    }
                                }
                            }
                        }
                        if (sections.aisleGroups.isEmpty() && sections.anyStoreItems.isEmpty()) {
                            item("empty") {
                                EmptyState(
                                    Icons.Outlined.ShoppingCart, stringResource(R.string.shopping_nothing_here),
                                    description = stringResource(R.string.shopping_nothing_here_description), modifier = Modifier.padding(top = 32.dp),
                                )
                            }
                        } else if (layout == ShoppingLayoutChoice.Pictures) {
                            val placed = sections.aisleGroups.flatMap { group -> group.items.map { item -> ShoppingPlacedItem(item, words.aisleTitle(group)) } } +
                                sections.anyStoreItems.map { item -> ShoppingPlacedItem(item, context.getString(R.string.shopping_any_store)) }
                            items(placed.chunked(2), key = { row -> "tiles-" + row.first().item.id }) { row ->
                                Row(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                    for (cell in row) {
                                        ShoppingStoreItemTile(
                                            cell.item, state.isChecked(cell.item), cell.groupName, words.storeItemDetail(cell.item, state.isChecked(cell.item), state.includedCount > 1),
                                            onToggle = { state.toggle(cell.item) }, onOpen = { menuItem = cell.item }, modifier = Modifier.weight(1f),
                                        )
                                    }
                                    if (row.size == 1) Box(Modifier.weight(1f))
                                }
                            }
                        } else {
                            for (group in sections.aisleGroups) {
                                item("header-" + group.id) { SectionTitle(words.aisleTitle(group)) }
                                items(group.items, key = { item -> "item-" + item.id }) { item -> storeRow(item, state, words, onMenu = { menuItem = item }) }
                            }
                            if (sections.anyStoreItems.isNotEmpty()) {
                                item("any-store-header") { SectionTitle(stringResource(R.string.shopping_also_on_the_list_any_store)) }
                                items(sections.anyStoreItems, key = { item -> "any-" + item.id }) { item -> storeRow(item, state, words, onMenu = { menuItem = item }) }
                            }
                        }
                    }
                }
            }
            DropdownMenu(menuItem != null, { menuItem = null }, modifier = Modifier.padding(16.dp)) {
                menuItem?.let { item ->
                    DropdownMenuItem(
                        text = { Text(stringResource(if (item.isOutOfStock) R.string.shopping_in_stock_again else R.string.shopping_out_of_stock_action)) },
                        onClick = { menuItem = null; state.setOutOfStock(item, !item.isOutOfStock) },
                    )
                    DropdownMenuItem(text = { Text(stringResource(R.string.shopping_item)) }, onClick = { menuItem = null; opened = item })
                }
            }
        }
    }

    if (isConfirmingFinish) {
        ConfirmationDialog(
            title = stringResource(R.string.shopping_finish_question), text = stringResource(R.string.shopping_finish_message),
            confirmLabel = stringResource(R.string.shopping_done),
            onConfirm = { isConfirmingFinish = false; scope.launch { if (state.finish()) navigator.back() } },
            onDismiss = { isConfirmingFinish = false },
        )
    }
    if (isChoosingReceiptSource) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { isChoosingReceiptSource = false },
            title = { Text(stringResource(R.string.shopping_scan_the_receipt)) },
            text = {
                Column {
                    androidx.compose.material3.TextButton(onClick = { isChoosingReceiptSource = false; cameraLauncher.launch(null) }) { Text(stringResource(R.string.shopping_receipt_from_camera)) }
                    androidx.compose.material3.TextButton(onClick = { isChoosingReceiptSource = false; photoLauncher.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }) { Text(stringResource(R.string.shopping_receipt_from_photos)) }
                }
            },
            confirmButton = {},
            dismissButton = { androidx.compose.material3.TextButton(onClick = { isChoosingReceiptSource = false }) { Text(stringResource(R.string.shopping_cancel)) } },
        )
    }
    opened?.let { item ->
        ShoppingItemSheet(connection, item, onDismiss = { opened = null }, onChanged = { state.reload() }, onRemove = { state.reload() }, onNeedAgain = null)
    }
    receiptReader.target?.let { receiptId ->
        ShoppingReceiptSheet(connection, receiptId, onDismiss = { receiptReader.target = null }, onChanged = { state.reload() })
    }
}

@Composable
private fun storeRow(item: ShoppingItem, state: ShoppingStoreState, words: ShoppingWords, onMenu: () -> Unit) {
    val isChecked = state.isChecked(item)
    ShoppingStoreItemRow(item, isChecked, words.storeItemDetail(item, isChecked, state.includedCount > 1), onToggle = { state.toggle(item) }, onMenu = onMenu)
}
