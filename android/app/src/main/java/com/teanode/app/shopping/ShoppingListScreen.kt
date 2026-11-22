package com.teanode.app.shopping

import android.net.Uri
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
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CameraAlt
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.GridView
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.Storefront
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.shopping.ShoppingGrouping
import com.teanode.kit.shopping.ShoppingGroup
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.openTrips
import kotlinx.coroutines.launch

/** How a list or a store lays out its items: as rows, or as tiles with big pictures, which are easier to find a product on the shelf by. */
enum class ShoppingLayoutChoice { List, Pictures }

/** What a list is grouped by. */
internal enum class ShoppingListGrouping { Store, Aisle }

/** An item for a tile, with the store or aisle the tile names, since the tiles run on without headings. */
internal class ShoppingPlacedItem(val item: ShoppingItem, val groupName: String)

/** One list at home: its items under their stores or aisles, as rows or tiles, what is not needed now, and the controls to add, import, share, open a store to shop at and say shopping is done. */
@Composable
fun ShoppingListScreen(listId: String, initialLayout: ShoppingLayoutChoice = ShoppingLayoutChoice.List) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val words = rememberShoppingWords()
    val context = LocalContext.current
    val facts by session.facts.collectAsState()
    val ownUserId = facts.session?.userId
    val state = remember(connection, listId) {
        ShoppingListState(connection, listId, scope, snackbar, { text -> snackbar.show(text) }, ownUserId)
    }
    val receiptReader = remember(connection) { ShoppingReceiptReader(connection, scope, snackbar, { text -> snackbar.show(text) }, context) }
    var layout by remember { mutableStateOf(initialLayout) }
    var grouping by remember { mutableStateOf(ShoppingListGrouping.Store) }
    var isRefreshing by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isLayoutMenuOpen by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<ShoppingItem?>(null) }
    var needingAgain by remember { mutableStateOf<ShoppingItem?>(null) }
    var removing by remember { mutableStateOf<ShoppingItem?>(null) }
    // Touch and hold on an item offers what the iPhone's swipe and menu do: Need again (for one not needed now) and Remove.
    fun itemActions(item: ShoppingItem) = ShoppingItemActions(onNeedAgain = { needingAgain = item }, onRemove = { removing = item })
    @Composable
    fun itemCheck(item: ShoppingItem) = shoppingListCheck(item, ownUserId) { state.check(item) }
    var isAdding by remember { mutableStateOf(false) }
    var addingStoreName by remember { mutableStateOf("") }
    var isImporting by remember { mutableStateOf(false) }
    var isSharing by remember { mutableStateOf(false) }
    var isRenaming by remember { mutableStateOf(false) }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isChoosingStore by remember { mutableStateOf(false) }
    var isChoosingReceiptSource by remember { mutableStateOf(false) }
    var isCartOpen by remember { mutableStateOf(false) }
    var isConfirmingFinish by remember { mutableStateOf(false) }
    // Whether the receipt being chosen is the one that finishes the trip (from the cart), or one scanned from the menu.
    var isSnappingAtDone by remember { mutableStateOf(false) }
    fun receiptChosen(file: ShoppingPendingFile) {
        if (isSnappingAtDone) receiptReader.snapAndFinish(file, finish = { state.finishTrip() }, onFinished = { isCartOpen = false })
        else receiptReader.scan(file)
    }

    LaunchedEffect(state) { state.load() }
    FollowShopping(connection, session.isDemo) { state.load() }
    LaunchedEffect(state.isDeleted) { if (state.isDeleted) navigator.back() }

    val photoLauncher = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri: Uri? ->
        if (uri != null) receiptChosen(context.readPendingFile(uri, "receipt.jpg"))
    }
    val cameraLauncher = rememberLauncherForActivityResult(ActivityResultContracts.TakePicturePreview()) { bitmap ->
        if (bitmap != null) receiptChosen(bitmap.toPendingFile("receipt.jpg"))
    }

    val list = state.list
    val title = list?.listName.orEmpty()
    TeaNodeScreen(
        title = title,
        onBack = { navigator.back() },
        actions = {
            if (state.hasLoaded) {
                Box {
                    IconButton(onClick = { isLayoutMenuOpen = true }) {
                        Icon(if (layout == ShoppingLayoutChoice.List) Icons.Outlined.ViewList else Icons.Outlined.GridView, contentDescription = stringResource(R.string.shopping_layout))
                    }
                    DropdownMenu(isLayoutMenuOpen, { isLayoutMenuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.shopping_list_layout)) }, leadingIcon = { Icon(Icons.Outlined.ViewList, null) },
                            onClick = { layout = ShoppingLayoutChoice.List; isLayoutMenuOpen = false },
                            trailingIcon = { if (layout == ShoppingLayoutChoice.List) Icon(Icons.Outlined.Check, null) },
                        )
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.shopping_pictures_layout)) }, leadingIcon = { Icon(Icons.Outlined.GridView, null) },
                            onClick = { layout = ShoppingLayoutChoice.Pictures; isLayoutMenuOpen = false },
                            trailingIcon = { if (layout == ShoppingLayoutChoice.Pictures) Icon(Icons.Outlined.Check, null) },
                        )
                    }
                }
                Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.shopping_more)) }
                    DropdownMenu(isMenuOpen, { isMenuOpen = false }) {
                        if (list?.isOwner == true) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.shopping_share)) }, leadingIcon = { Icon(Icons.Outlined.Share, null) }, onClick = { isMenuOpen = false; isSharing = true })
                        }
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.shopping_shop_at_a_store)) }, leadingIcon = { Icon(Icons.Outlined.Storefront, null) },
                            onClick = {
                                isMenuOpen = false
                                scope.launch {
                                    state.loadStores()
                                    when (state.storeChoices.size) {
                                        0 -> navigator.navigate(shoppingStoreRoute("", listOf(listId)))
                                        1 -> navigator.navigate(shoppingStoreRoute(state.storeChoices.first(), listOf(listId)))
                                        else -> isChoosingStore = true
                                    }
                                }
                            },
                        )
                        DropdownMenuItem(text = { Text(stringResource(R.string.shopping_import)) }, leadingIcon = { Icon(Icons.Outlined.Download, null) }, onClick = { isMenuOpen = false; isImporting = true })
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.shopping_upload_a_receipt)) }, leadingIcon = { Icon(Icons.Outlined.CameraAlt, null) },
                            onClick = { isMenuOpen = false; isSnappingAtDone = false; isChoosingReceiptSource = true },
                        )
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.shopping_remind_me_at_stores)) },
                            trailingIcon = { Switch(state.isReminded, null) },
                            onClick = { state.setReminder(!state.isReminded) },
                        )
                        if (list?.isOwner == true) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.shopping_rename)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { isMenuOpen = false; isRenaming = true })
                            DropdownMenuItem(text = { Text(stringResource(R.string.shopping_delete)) }, leadingIcon = { Icon(Icons.Outlined.Delete, null) }, onClick = { isMenuOpen = false; isConfirmingDelete = true })
                        }
                    }
                }
            }
        },
        floatingActionButton = {
            if (state.hasLoaded) {
                Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (state.isShopping) {
                        ExtendedFloatingActionButton(
                            onClick = { isCartOpen = true },
                            icon = { Icon(Icons.Outlined.Check, null) }, text = { Text(stringResource(R.string.shopping_done_shopping_button)) },
                        )
                    }
                    ExtendedFloatingActionButton(
                        onClick = { addingStoreName = ""; isAdding = true },
                        icon = { Icon(Icons.Outlined.Add, null) }, text = { Text(stringResource(R.string.shopping_add_an_item)) },
                    )
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            val failure = state.loadFailure
            when {
                failure != null && !state.hasLoaded -> ErrorState(session.describeLoadProblem(failure), onRetry = { state.reload() })
                !state.hasLoaded -> LoadingState()
                else -> PullToRefreshBox(
                    isRefreshing = isRefreshing,
                    onRefresh = { scope.launch { isRefreshing = true; state.load(); isRefreshing = false } },
                ) {
                    val groups = when (grouping) {
                        ShoppingListGrouping.Store -> ShoppingGrouping.byStore(state.toBuy)
                        ShoppingListGrouping.Aisle -> ShoppingGrouping.byAisle(state.toBuy)
                    }
                    fun groupTitle(group: ShoppingGroup): String = when (grouping) {
                        ShoppingListGrouping.Store -> words.storeTitle(group)
                        ShoppingListGrouping.Aisle -> words.aisleTitle(group)
                    }
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 160.dp)) {
                        state.resumableTrip?.let { trip ->
                            item("resume") {
                                ShoppingResumeRow(trip, state.resumable.isResuming, onResume = { scope.launch { state.resume(context.getString(R.string.shopping_resumed)) } })
                            }
                        }
                        item("subtitle") {
                            Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                Text(
                                    words.listSubtitle(list, state.granteeNames, state.neededCount, state.estimatedTotal),
                                    style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                                words.listShoppersLine(state.trips.openTrips(listId, ownUserId))?.let { line ->
                                    Text(line, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
                                }
                                if (state.items.isNotEmpty()) {
                                    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                                        SegmentedButton(
                                            selected = grouping == ShoppingListGrouping.Store, onClick = { grouping = ShoppingListGrouping.Store },
                                            shape = SegmentedButtonDefaults.itemShape(0, 2),
                                        ) { Text(stringResource(R.string.shopping_by_store)) }
                                        SegmentedButton(
                                            selected = grouping == ShoppingListGrouping.Aisle, onClick = { grouping = ShoppingListGrouping.Aisle },
                                            shape = SegmentedButtonDefaults.itemShape(1, 2),
                                        ) { Text(stringResource(R.string.shopping_by_aisle)) }
                                    }
                                }
                            }
                        }
                        if (layout == ShoppingLayoutChoice.Pictures) {
                            val placed = groups.flatMap { group -> group.items.map { item -> ShoppingPlacedItem(item, groupTitle(group)) } }
                            tileRows(placed, words, actionsOf = ::itemActions, checkOf = { item -> itemCheck(item) }) { item -> editing = item }
                            if (state.notNeeded.isNotEmpty()) {
                                item("not-needed-header") { SectionTitle(stringResource(R.string.shopping_checked_off)) }
                                tileRows(state.notNeeded.map { item -> ShoppingPlacedItem(item, words.listItemPlace(item)) }, words, actionsOf = ::itemActions, checkOf = { item -> itemCheck(item) }) { item -> editing = item }
                            }
                        } else {
                            for (group in groups) {
                                item("header-" + group.id) {
                                    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                                        Text(groupTitle(group), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f).padding(vertical = 12.dp))
                                        if (grouping == ShoppingListGrouping.Store) {
                                            val label = if (group.isUnnamed) stringResource(R.string.shopping_add_item_for_any_store) else stringResource(R.string.shopping_add_item_at, group.title)
                                            IconButton(onClick = { addingStoreName = group.title; isAdding = true }) { Icon(Icons.Outlined.Add, contentDescription = label) }
                                        }
                                    }
                                }
                                items(group.items, key = { item -> "item-" + item.id }) { item -> ShoppingListItemRow(item, words, onOpen = { editing = item }, actions = itemActions(item), check = itemCheck(item)) }
                            }
                            if (state.notNeeded.isNotEmpty()) {
                                item("not-needed-header") { SectionTitle(stringResource(R.string.shopping_checked_off)) }
                                items(state.notNeeded, key = { item -> "not-needed-" + item.id }) { item -> ShoppingListItemRow(item, words, onOpen = { editing = item }, actions = itemActions(item), check = itemCheck(item)) }
                            }
                        }
                    }
                }
            }
        }
    }

    if (isCartOpen) {
        val ownCart = state.ownCart
        ShoppingCartSheet(
            items = ownCart, showsListName = ownCart.map { item -> item.shoppingListId }.distinct().size > 1, busyText = receiptReader.readingText,
            onTakeOut = { item -> state.check(item) },
            onSnapReceipt = { isSnappingAtDone = true; isChoosingReceiptSource = true },
            onFinishWithoutReceipt = { isConfirmingFinish = true },
            onDismiss = { isCartOpen = false },
        )
    }
    if (isConfirmingFinish) {
        val cartCount = state.ownCart.size
        ConfirmationDialog(
            title = stringResource(R.string.shopping_finish_without_receipt_question),
            text = if (cartCount == 0) stringResource(R.string.shopping_finish_without_receipt_message_empty)
            else pluralStringResource(R.plurals.shopping_finish_without_receipt_message, cartCount, cartCount),
            confirmLabel = stringResource(R.string.shopping_finish_action),
            onConfirm = {
                isConfirmingFinish = false
                scope.launch { if (state.finish(context.getString(R.string.shopping_done_shopping))) isCartOpen = false }
            },
            onDismiss = { isConfirmingFinish = false },
        )
    }
    if (isChoosingStore) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { isChoosingStore = false },
            title = { Text(stringResource(R.string.shopping_where_are_you_shopping)) },
            text = {
                Column {
                    for (storeName in state.storeChoices) {
                        TextButton(onClick = { isChoosingStore = false; navigator.navigate(shoppingStoreRoute(storeName, listOf(listId))) }) { Text(storeName) }
                    }
                    TextButton(onClick = { isChoosingStore = false; navigator.navigate(shoppingStoreRoute("", listOf(listId))) }) { Text(stringResource(R.string.shopping_any_store)) }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { isChoosingStore = false }) { Text(stringResource(R.string.shopping_cancel)) } },
        )
    }
    if (isChoosingReceiptSource) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { isChoosingReceiptSource = false },
            title = { Text(stringResource(R.string.shopping_upload_a_receipt)) },
            text = {
                Column {
                    TextButton(onClick = { isChoosingReceiptSource = false; cameraLauncher.launch(null) }) { Text(stringResource(R.string.shopping_receipt_from_camera)) }
                    TextButton(onClick = { isChoosingReceiptSource = false; photoLauncher.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }) { Text(stringResource(R.string.shopping_receipt_from_photos)) }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { isChoosingReceiptSource = false }) { Text(stringResource(R.string.shopping_cancel)) } },
        )
    }
    if (isConfirmingDelete) {
        ConfirmationDialog(
            title = stringResource(R.string.shopping_delete_list_question), confirmLabel = stringResource(R.string.shopping_delete), isDestructive = true,
            onConfirm = { isConfirmingDelete = false; scope.launch { state.delete() } }, onDismiss = { isConfirmingDelete = false },
        )
    }
    if (isRenaming) {
        NameDialog(
            title = stringResource(R.string.shopping_rename_the_list), label = stringResource(R.string.shopping_list_name), initial = title,
            confirmLabel = stringResource(R.string.shopping_save), onDismiss = { isRenaming = false },
            onConfirm = { name -> isRenaming = false; scope.launch { state.rename(name) } },
        )
    }
    editing?.let { item ->
        ShoppingItemSheet(
            connection = connection, item = item, onDismiss = { editing = null },
            onChanged = { changed -> state.replace(changed) },
            onRemove = { removed -> scope.launch { state.remove(removed) { name -> context.getString(R.string.shopping_removed_item, name) } } },
            onNeedAgain = if (item.isNotNeeded) ({ _ -> editing = null; needingAgain = item }) else null,
        )
    }
    removing?.let { item ->
        ConfirmationDialog(
            title = stringResource(R.string.shopping_remove_from_the_list), text = stringResource(R.string.shopping_item_taken_off_for_everyone, item.itemDescription),
            confirmLabel = stringResource(R.string.shopping_remove), isDestructive = true,
            onConfirm = { removing = null; scope.launch { state.remove(item) { name -> context.getString(R.string.shopping_removed_item, name) } } }, onDismiss = { removing = null },
        )
    }
    needingAgain?.let { item ->
        ShoppingNeedAgainSheet(item, onDismiss = { needingAgain = null }) { quantity ->
            needingAgain = null
            scope.launch { state.needAgain(item, quantity) { name -> context.getString(R.string.shopping_item_on_list_again, name) } }
        }
    }
    if (isAdding) {
        ShoppingAddItemSheet(
            connection = connection, listId = listId, initialStoreName = addingStoreName, onDismiss = { isAdding = false },
            onAdd = { entry -> state.add(listOf(entry)) },
            onOpen = { item -> isAdding = false; editing = item },
            onNeedAgain = { item, quantity -> state.needAgain(item, quantity) { name -> context.getString(R.string.shopping_item_on_list_again, name) } },
        )
    }
    if (isImporting) {
        ShoppingImportSheet(connection, listId, onDismiss = { isImporting = false }, onAdded = { scope.launch { state.load() } })
    }
    if (isSharing) {
        ShoppingShareSheet(connection, listId, isReminded = state.isReminded, onReminder = { isOn -> state.setReminder(isOn) }, onDismiss = { isSharing = false }, onChanged = { scope.launch { state.load() } })
    }
    receiptReader.target?.let { receiptId ->
        ShoppingReceiptSheet(connection, receiptId, onDismiss = { receiptReader.target = null }, onChanged = { scope.launch { state.load() } })
    }
    if (receiptReader.readingText != null) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = {}, title = { Text(receiptReader.readingText.orEmpty()) },
            text = { androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth()) }, confirmButton = {},
        )
    }
}

@Composable
internal fun SectionTitle(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.semantics { heading() }.padding(start = 16.dp, top = 16.dp, bottom = 8.dp))
}

/** Tiles two to a row, which fills a section's one row of the list. */
internal fun androidx.compose.foundation.lazy.LazyListScope.tileRows(
    placed: List<ShoppingPlacedItem>,
    words: ShoppingWords,
    actionsOf: ((ShoppingItem) -> ShoppingItemActions)? = null,
    checkOf: (@Composable (ShoppingItem) -> ShoppingListCheck)? = null,
    onOpen: (ShoppingItem) -> Unit,
) {
    items(placed.chunked(2), key = { row -> "tiles-" + row.first().item.id }) { row ->
        Row(Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            for (cell in row) ShoppingListItemTile(cell.item, cell.groupName, words, onOpen = { onOpen(cell.item) }, modifier = Modifier.weight(1f), actions = actionsOf?.invoke(cell.item), check = checkOf?.invoke(cell.item))
            if (row.size == 1) Box(Modifier.weight(1f))
        }
    }
}
