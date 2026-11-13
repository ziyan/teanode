package com.teanode.app.shopping

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import com.teanode.app.finance.ShareTargetPicker
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.finance.ShareCircle
import com.teanode.kit.finance.SharePerson
import com.teanode.kit.finance.ShareTarget
import com.teanode.kit.shopping.ShoppingImportDraft
import com.teanode.kit.shopping.ShoppingImportPreview
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.ShoppingItemEntry
import com.teanode.kit.shopping.ShoppingList
import com.teanode.kit.shopping.ShoppingListShare
import com.teanode.kit.shopping.ShoppingQuantity
import com.teanode.kit.shopping.ShoppingStoreSuggestions
import com.teanode.kit.shopping.addShoppingItems
import com.teanode.kit.shopping.createShoppingList
import com.teanode.kit.shopping.previewShoppingImport
import com.teanode.kit.shopping.shareShoppingList
import com.teanode.kit.shopping.shoppingListShares
import com.teanode.kit.shopping.shoppingLists
import com.teanode.kit.shopping.shoppingStores
import com.teanode.kit.shopping.similarShoppingItems
import com.teanode.kit.shopping.unshareShoppingList
import com.teanode.kit.finance.shareCircles
import com.teanode.kit.finance.sharePeople
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** The frame of a sheet: a title, a way to close it, and a scrolling body. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ShoppingSheet(
    title: String,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    confirmLabel: String? = null,
    isConfirmEnabled: Boolean = true,
    onConfirm: (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    val body: @Composable () -> Unit = {
        Column(modifier.fillMaxWidth().navigationBarsPadding()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.shopping_cancel)) }
                Text(title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f), maxLines = 1, textAlign = androidx.compose.ui.text.style.TextAlign.Center)
                if (confirmLabel != null && onConfirm != null) TextButton(onClick = onConfirm, enabled = isConfirmEnabled) { Text(confirmLabel) }
                else Spacer(Modifier.padding(horizontal = 24.dp))
            }
            HorizontalDivider()
            Column(Modifier.verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) { content() }
        }
    }
    if (LocalShoppingSheetsInline.current) {
        Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surfaceContainerLow) { body() }
    } else {
        ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) { body() }
    }
}

/** Screenshots draw a sheet in the page, since a sheet is a window of its own that a picture of the page leaves out. */
internal val LocalShoppingSheetsInline = androidx.compose.runtime.staticCompositionLocalOf { false }

/** "Need 3" with a button each side that changes the count by one. */
@Composable
internal fun ShoppingQuantityStepper(quantity: Double, onChange: (Double) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(R.string.shopping_need_quantity, ShoppingQuantity.text(quantity)), modifier = Modifier.weight(1f))
        IconButton(onClick = { onChange(maxOf(1.0, quantity - 1)) }, enabled = quantity > 1) { Icon(Icons.Outlined.Remove, contentDescription = stringResource(R.string.shopping_fewer)) }
        IconButton(onClick = { onChange(minOf(999.0, quantity + 1)) }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.shopping_more_of_it)) }
    }
}

/** Needs a bought item again, with a count. */
@Composable
internal fun ShoppingNeedAgainSheet(item: ShoppingItem, onDismiss: () -> Unit, onConfirm: (String) -> Unit) {
    var quantity by remember { mutableStateOf(ShoppingQuantity.value(item.neededQuantity)) }
    ShoppingSheet(stringResource(R.string.shopping_need_again), onDismiss, confirmLabel = stringResource(R.string.shopping_add), onConfirm = { onConfirm(ShoppingQuantity.text(quantity)) }) {
        Text(item.itemDescription, style = MaterialTheme.typography.titleMedium)
        ShoppingQuantityStepper(quantity) { quantity = it }
    }
}

/**
 * Adds one item to a list: what it is, how many, and where to get it. What reads like it is listed as it is typed, and
 * asked about again before adding, so an item already on a list is used rather than added twice. The store is chosen
 * from the ones the lists have as it is typed.
 */
@Composable
internal fun ShoppingAddItemSheet(
    connection: Connection,
    listId: String,
    initialStoreName: String,
    onDismiss: () -> Unit,
    onAdd: suspend (ShoppingItemEntry) -> Boolean,
    onOpen: (ShoppingItem) -> Unit,
    onNeedAgain: suspend (ShoppingItem, String) -> Boolean,
) {
    val scope = rememberCoroutineScope()
    val words = rememberShoppingWords()
    var itemDescription by remember { mutableStateOf("") }
    var itemDetails by remember { mutableStateOf("") }
    var quantity by remember { mutableStateOf(1.0) }
    var quantityUnit by remember { mutableStateOf("") }
    var storeName by remember { mutableStateOf(initialStoreName) }
    var isWorking by remember { mutableStateOf(false) }
    var similarItems by remember { mutableStateOf<List<ShoppingItem>>(emptyList()) }
    var alikeItems by remember { mutableStateOf<List<ShoppingItem>>(emptyList()) }
    var isAskingAboutAlike by remember { mutableStateOf(false) }
    var storeNames by remember { mutableStateOf<List<String>>(emptyList()) }
    var isStoreFocused by remember { mutableStateOf(false) }
    val typedDescription = itemDescription.trim()

    LaunchedEffect(Unit) {
        // Only a help, so a failure leaves none.
        storeNames = try {
            connection.shoppingStores().withIndex().sortedWith(compareBy({ -it.value.neededCount }, { it.index })).map { it.value.storeName }
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            emptyList()
        }
    }
    LaunchedEffect(itemDescription) {
        // Reads what is like the words once typing pauses: a new word cancels the wait.
        if (typedDescription.length < 2) {
            similarItems = emptyList()
            return@LaunchedEffect
        }
        delay(250)
        similarItems = try {
            connection.similarShoppingItems(listId, typedDescription)
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            similarItems
        }
    }

    fun isNeededAgainHere(item: ShoppingItem) = item.shoppingListId == listId && item.isNotNeeded

    suspend fun addEntry() {
        val entry = ShoppingItemEntry(
            itemDescription = typedDescription, itemDetails = itemDetails, storeName = storeName.trim(),
            neededQuantity = ShoppingQuantity.text(quantity), quantityUnit = quantityUnit,
        )
        isWorking = true
        val isAdded = onAdd(entry)
        isWorking = false
        if (isAdded) onDismiss()
    }

    ShoppingSheet(
        title = stringResource(if (isAskingAboutAlike) R.string.shopping_is_it_one_of_these else R.string.shopping_add_an_item), onDismiss = onDismiss,
        confirmLabel = if (isAskingAboutAlike) null else stringResource(R.string.shopping_add),
        isConfirmEnabled = !isWorking && typedDescription.isNotEmpty(),
        onConfirm = {
            scope.launch {
                isWorking = true
                val alike = try {
                    connection.similarShoppingItems(listId, typedDescription)
                } catch (failure: CancellationException) {
                    throw failure
                } catch (_: Throwable) {
                    emptyList()
                }
                isWorking = false
                if (alike.isEmpty()) addEntry() else {
                    alikeItems = alike
                    isAskingAboutAlike = true
                }
            }
        },
    ) {
        if (isAskingAboutAlike) {
            Text(stringResource(R.string.shopping_already_on_your_lists), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            for (item in alikeItems) {
                val isNeededAgain = isNeededAgainHere(item)
                Row(
                    Modifier.fillMaxWidth().clickable(enabled = !isWorking) {
                        if (!isNeededAgain) {
                            onOpen(item)
                            onDismiss()
                        } else scope.launch {
                            isWorking = true
                            val isDone = onNeedAgain(item, ShoppingQuantity.text(quantity))
                            isWorking = false
                            if (isDone) onDismiss()
                        }
                    },
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    ShoppingPictureView(item)
                    Column(Modifier.weight(1f)) {
                        Text(item.itemDescription)
                        Text(words.similarItemDetail(item, listId), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    if (isNeededAgain) Text(stringResource(R.string.shopping_need_again), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
                }
            }
            Text(stringResource(R.string.shopping_choose_one_instead, typedDescription), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = { scope.launch { addEntry() } }, enabled = !isWorking) { Text(stringResource(R.string.shopping_add_anyway, typedDescription)) }
            return@ShoppingSheet
        }
        OutlinedTextField(itemDescription, { itemDescription = it }, label = { Text(stringResource(R.string.shopping_what)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(itemDetails, { itemDetails = it }, label = { Text(stringResource(R.string.shopping_item_details_hint)) }, modifier = Modifier.fillMaxWidth())
        if (similarItems.isNotEmpty()) {
            Text(stringResource(R.string.shopping_already_on_your_lists), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            for (item in similarItems) {
                Row(
                    Modifier.fillMaxWidth().clickable { onOpen(item); onDismiss() },
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    ShoppingPictureView(item)
                    Column(Modifier.weight(1f)) {
                        Text(item.itemDescription)
                        Text(words.similarItemDetail(item, listId), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        Text(stringResource(R.string.shopping_where), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        OutlinedTextField(
            storeName, { storeName = it; isStoreFocused = true }, label = { Text(stringResource(R.string.shopping_item_store)) }, singleLine = true,
            modifier = Modifier.fillMaxWidth(), supportingText = { Text(stringResource(R.string.shopping_leave_store_empty)) },
        )
        if (isStoreFocused) {
            for (suggestion in ShoppingStoreSuggestions.names(storeName, storeNames)) {
                TextButton(onClick = { storeName = suggestion; isStoreFocused = false }) { Text(suggestion) }
            }
        }
        Text(stringResource(R.string.shopping_how_many), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        ShoppingQuantityStepper(quantity) { quantity = it }
        OutlinedTextField(quantityUnit, { quantityUnit = it }, label = { Text(stringResource(R.string.shopping_unit_hint)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
    }
}

/** The people and circles a list can be shared with, and who it is shared with now. */
@Composable
internal fun ShoppingShareSheet(
    connection: Connection,
    listId: String,
    isReminded: Boolean,
    onReminder: (Boolean) -> Unit,
    onDismiss: () -> Unit,
    onChanged: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    var shares by remember { mutableStateOf<List<ShoppingListShare>>(emptyList()) }
    var people by remember { mutableStateOf<List<SharePerson>>(emptyList()) }
    var circles by remember { mutableStateOf<List<ShareCircle>>(emptyList()) }
    var isLoaded by remember { mutableStateOf(false) }
    var chosen by remember { mutableStateOf<ShareTarget?>(null) }
    var stopping by remember { mutableStateOf<ShoppingListShare?>(null) }
    var isWorking by remember { mutableStateOf(false) }
    var reminded by remember { mutableStateOf(isReminded) }

    suspend fun load() {
        try {
            shares = connection.shoppingListShares(listId)
            people = connection.sharePeople()
            circles = connection.shareCircles()
            isLoaded = true
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            snackbar.showError(failure)
        }
    }
    LaunchedEffect(Unit) { load() }
    fun nameOf(share: ShoppingListShare): String =
        if (share.isWithCircle) context.getString(R.string.shopping_everyone_in, share.shownGrantee) else share.shownGrantee

    ShoppingSheet(stringResource(R.string.shopping_share_this_list), onDismiss, confirmLabel = stringResource(R.string.shopping_done), onConfirm = onDismiss) {
        Text(stringResource(R.string.shopping_shared_with_heading), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        if (isLoaded && shares.isEmpty()) Text(stringResource(R.string.shopping_not_shared_yet), color = MaterialTheme.colorScheme.onSurfaceVariant)
        for (share in shares) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(nameOf(share), modifier = Modifier.weight(1f))
                TextButton(onClick = { stopping = share }, enabled = !isWorking) { Text(stringResource(R.string.shopping_stop_sharing), color = MaterialTheme.colorScheme.error) }
            }
        }
        if (isLoaded && people.isEmpty() && circles.isEmpty()) {
            Text(stringResource(R.string.shopping_no_circle_yet), color = MaterialTheme.colorScheme.onSurfaceVariant)
        } else if (isLoaded) {
            ShareTargetPicker(people, circles, chosen) { chosen = it }
            Text(stringResource(R.string.shopping_share_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Button(
                enabled = !isWorking && chosen != null,
                onClick = {
                    val target = chosen ?: return@Button
                    val name = com.teanode.app.finance.ShareTargetNames.nameOf(target, people, circles, context)
                    isWorking = true
                    scope.launch {
                        try {
                            connection.shareShoppingList(listId, target.granteeUserId, target.granteeCircleId)
                            snackbar.show(context.getString(R.string.shopping_shared_with, name))
                            chosen = null
                            shares = connection.shoppingListShares(listId)
                            onChanged()
                        } catch (failure: CancellationException) {
                            throw failure
                        } catch (failure: Throwable) {
                            snackbar.showError(failure)
                        }
                        isWorking = false
                    }
                },
            ) { Text(stringResource(R.string.shopping_share)) }
        }
        HorizontalDivider()
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.shopping_remind_me_at_stores), modifier = Modifier.weight(1f))
            Switch(reminded, { reminded = it; onReminder(it) })
        }
        Text(stringResource(R.string.shopping_remind_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    stopping?.let { share ->
        ConfirmationDialog(
            title = stringResource(R.string.shopping_stop_sharing), text = stringResource(R.string.shopping_will_no_longer_see, nameOf(share)),
            confirmLabel = stringResource(R.string.shopping_stop_sharing), isDestructive = true,
            onConfirm = {
                stopping = null
                isWorking = true
                scope.launch {
                    try {
                        connection.unshareShoppingList(share.id)
                        snackbar.show(context.getString(R.string.shopping_stopped_sharing_with, nameOf(share)))
                        shares = connection.shoppingListShares(listId)
                        onChanged()
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        snackbar.showError(failure)
                    }
                    isWorking = false
                }
            },
            onDismiss = { stopping = null },
        )
    }
}

/** Reads pasted text and pictures as a list, shows what was found against what the list has, and adds what is kept. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ShoppingImportSheet(connection: Connection, initialListId: String?, onDismiss: () -> Unit, onAdded: () -> Unit) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val words = rememberShoppingWords()
    var lists by remember { mutableStateOf<List<ShoppingList>>(emptyList()) }
    var destinationListId by remember { mutableStateOf(initialListId.orEmpty()) }
    var newListName by remember { mutableStateOf("") }
    var importText by remember { mutableStateOf("") }
    var files by remember { mutableStateOf<List<ShoppingPendingFile>>(emptyList()) }
    var attachmentIds by remember { mutableStateOf<Map<ShoppingPendingFile, String>>(emptyMap()) }
    var drafts by remember { mutableStateOf<List<ShoppingImportDraft>>(emptyList()) }
    var hasRead by remember { mutableStateOf(false) }
    var isReading by remember { mutableStateOf(false) }
    var isAdding by remember { mutableStateOf(false) }
    var isListMenuOpen by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        lists = try {
            connection.shoppingLists()
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            emptyList()
        }
    }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(6)) { uris ->
        files = files + uris.mapIndexed { index, uri -> context.readPendingFile(uri, "list-${files.size + index + 1}.jpg") }
    }
    val included = drafts.filter { draft -> draft.isIncluded }
    val isNameMissing = destinationListId.isEmpty() && newListName.isBlank()
    val hasInput = importText.isNotBlank() || files.isNotEmpty()

    ShoppingSheet(stringResource(R.string.shopping_import_title), onDismiss, confirmLabel = if (hasRead && included.isNotEmpty()) stringResource(R.string.shopping_import_add_count, included.size) else null, isConfirmEnabled = !isAdding && !isNameMissing, onConfirm = {
        isAdding = true
        scope.launch {
            try {
                val listId = destinationListId.ifEmpty { connection.createShoppingList(newListName.trim()).id }
                connection.addShoppingItems(listId, ShoppingImportDraft.entries(drafts))
                snackbar.show(context.getString(R.string.shopping_import_added, included.size))
                onAdded()
                onDismiss()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            isAdding = false
        }
    }) {
        OutlinedTextField(
            importText, { importText = it }, label = { Text(stringResource(R.string.shopping_import_paste)) }, minLines = 4, maxLines = 10, modifier = Modifier.fillMaxWidth(),
            supportingText = { Text(stringResource(R.string.shopping_import_footer)) },
        )
        OutlinedButton(onClick = { picker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) }) { Text(stringResource(R.string.shopping_import_choose_pictures)) }
        for (file in files) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(file.name, modifier = Modifier.weight(1f))
                TextButton(onClick = { files = files - file }) { Text(stringResource(R.string.shopping_remove)) }
            }
        }
        Text(stringResource(R.string.shopping_add_to), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        ExposedDropdownMenuBox(isListMenuOpen, { isListMenuOpen = it }) {
            OutlinedTextField(
                value = lists.firstOrNull { list -> list.id == destinationListId }?.listName ?: stringResource(R.string.shopping_a_new_list),
                onValueChange = {}, readOnly = true, label = { Text(stringResource(R.string.shopping_list)) },
                trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(isListMenuOpen) }, modifier = Modifier.menuAnchor().fillMaxWidth(),
            )
            ExposedDropdownMenu(isListMenuOpen, { isListMenuOpen = false }) {
                for (list in lists) DropdownMenuItem(text = { Text(list.listName) }, onClick = { destinationListId = list.id; isListMenuOpen = false })
                DropdownMenuItem(text = { Text(stringResource(R.string.shopping_a_new_list)) }, onClick = { destinationListId = ""; isListMenuOpen = false })
            }
        }
        if (destinationListId.isEmpty()) {
            OutlinedTextField(newListName, { newListName = it }, label = { Text(stringResource(R.string.shopping_name_of_new_list)) }, singleLine = true, modifier = Modifier.fillMaxWidth())
        }
        Button(
            enabled = hasInput && !isReading && !isAdding,
            onClick = {
                isReading = true
                scope.launch {
                    try {
                        val ids = mutableListOf<String>()
                        val known = attachmentIds.toMutableMap()
                        for (file in files) {
                            val id = known[file] ?: connection.upload(file.name, file.contentType, file.data).also { uploaded -> known[file] = uploaded }
                            ids.add(id)
                        }
                        attachmentIds = known
                        val preview: ShoppingImportPreview = connection.previewShoppingImport(destinationListId.ifEmpty { null }, importText, ids)
                        drafts = ShoppingImportDraft.drafts(preview)
                        hasRead = true
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        snackbar.showError(failure)
                    }
                    isReading = false
                }
            },
        ) { Text(stringResource(if (isReading) R.string.shopping_reading else R.string.shopping_import_read)) }
        if (isReading) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (hasRead) {
            Text(stringResource(R.string.shopping_found), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            if (drafts.isEmpty()) Text(stringResource(R.string.shopping_import_nothing), color = MaterialTheme.colorScheme.onSurfaceVariant)
            for (draft in drafts) {
                Row(Modifier.fillMaxWidth().clickable { drafts = drafts.map { each -> if (each.id == draft.id) each.copy(isIncluded = !each.isIncluded) else each } }, verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(draft.isIncluded, { isOn -> drafts = drafts.map { each -> if (each.id == draft.id) each.copy(isIncluded = isOn) else each } })
                    Column(Modifier.weight(1f)) {
                        Text(draft.item.shownTitle)
                        if (draft.item.shownDetail.isNotEmpty()) Text(draft.item.shownDetail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Text(words.importMark(draft.item), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Text(stringResource(R.string.shopping_import_merge_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Spacer(Modifier.height(24.dp))
    }
}

/** A heading inside a sheet. */
@Composable
internal fun SheetHeading(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
}
