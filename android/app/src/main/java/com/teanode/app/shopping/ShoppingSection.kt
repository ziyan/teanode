package com.teanode.app.shopping

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ShoppingCart
import androidx.compose.material.icons.outlined.Storefront
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.AppSection
import com.teanode.kit.shopping.ShoppingList
import com.teanode.kit.shopping.ShoppingStore
import kotlinx.coroutines.launch

/** The first screen of Shopping: the person's lists, the lists shared with them, and the stores their lists have items for. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShoppingSection() {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val words = rememberShoppingWords()
    val state = remember(connection) { ShoppingHomeState(connection, scope, snackbar) }
    var isCreating by remember { mutableStateOf(false) }
    var isRefreshing by remember { mutableStateOf(false) }
    androidx.compose.runtime.LaunchedEffect(state) { state.load() }
    FollowShopping(connection, session.isDemo) { state.load() }

    TeaNodeScreen(
        title = stringResource(R.string.shopping_title),
        actions = {
            IconButton(onClick = { isCreating = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.shopping_new_list)) }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            val failure = state.loadFailure
            when {
                failure != null && !state.hasLoaded -> ErrorState(session.describeLoadProblem(failure), onRetry = { state.reload() })
                !state.hasLoaded -> LoadingState()
                state.lists.isEmpty() -> EmptyState(
                    Icons.Outlined.ShoppingCart, stringResource(R.string.shopping_empty_title), description = stringResource(R.string.shopping_empty_description),
                )
                else -> PullToRefreshBox(
                    isRefreshing = isRefreshing,
                    onRefresh = { scope.launch { isRefreshing = true; state.load(); isRefreshing = false } },
                ) {
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                        if (state.ownLists.isNotEmpty()) {
                            item("own-header") { TeaNodeSectionHeader(stringResource(R.string.shopping_your_lists)) }
                            items(state.ownLists, key = { list -> "own-" + list.id }) { list ->
                                ShoppingListRow(list, state.granteeNames[list.id].orEmpty(), words) { navigator.navigate(shoppingListRoute(list.id)) }
                            }
                        }
                        if (state.sharedLists.isNotEmpty()) {
                            item("shared-header") { TeaNodeSectionHeader(stringResource(R.string.shopping_shared_with_you)) }
                            items(state.sharedLists, key = { list -> "shared-" + list.id }) { list ->
                                ShoppingListRow(list, emptyList(), words) { navigator.navigate(shoppingListRoute(list.id)) }
                            }
                        }
                        if (state.stores.isNotEmpty()) {
                            item("stores-header") { TeaNodeSectionHeader(stringResource(R.string.shopping_stores)) }
                            items(state.stores, key = { store -> "store-" + store.storeKey }) { store ->
                                ShoppingStoreRow(store) { navigator.navigate(shoppingStoreRoute(store.storeName, emptyList())) }
                            }
                        }
                    }
                }
            }
        }
    }

    if (isCreating) {
        NameDialog(
            title = stringResource(R.string.shopping_new_list), message = stringResource(R.string.shopping_new_list_message),
            label = stringResource(R.string.shopping_list_name), initial = "", confirmLabel = stringResource(R.string.shopping_create),
            onDismiss = { isCreating = false },
            onConfirm = { name ->
                isCreating = false
                scope.launch { state.create(name)?.let { created -> navigator.navigate(shoppingListRoute(created.id)) } }
            },
        )
    }
}

@Composable
private fun ShoppingListRow(list: ShoppingList, granteeNames: List<String>, words: ShoppingWords, onClick: () -> Unit) {
    TeaNodeNavigationRow(
        headline = list.listName, onClick = onClick, supporting = words.listRowSubtitle(list, granteeNames),
        leadingIcon = Icons.Outlined.ShoppingCart,
        trailing = { if (list.neededCount > 0) Text(words.toBuyCount(list.neededCount), style = androidx.compose.material3.MaterialTheme.typography.bodySmall, color = androidx.compose.material3.MaterialTheme.colorScheme.onSurfaceVariant) },
    )
}

@Composable
private fun ShoppingStoreRow(store: ShoppingStore, onClick: () -> Unit) {
    TeaNodeNavigationRow(
        headline = store.storeName, onClick = onClick, leadingIcon = Icons.Outlined.Storefront,
        trailing = { Text(stringResource(R.string.shopping_item_count_needed, store.neededCount), style = androidx.compose.material3.MaterialTheme.typography.bodySmall, color = androidx.compose.material3.MaterialTheme.colorScheme.onSurfaceVariant) },
    )
}

/** A dialog with one text field: a list's name. */
@Composable
internal fun NameDialog(
    title: String,
    label: String,
    initial: String,
    confirmLabel: String,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
    message: String? = null,
) {
    var name by remember { mutableStateOf(initial) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (message != null) Text(message)
                OutlinedTextField(name, { name = it }, label = { Text(label) }, singleLine = true)
            }
        },
        confirmButton = { TextButton(onClick = { if (name.isNotBlank()) onConfirm(name.trim()) }, enabled = name.isNotBlank()) { Text(confirmLabel) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.shopping_cancel)) } },
    )
}

internal fun shoppingListRoute(listId: String): String = Routes.child(AppSection.Shopping, "list/${android.net.Uri.encode(listId)}")

/** A store with the lists to start with; all the lists that have items there when empty. */
internal fun shoppingStoreRoute(storeName: String, listIds: List<String>): String =
    Routes.child(AppSection.Shopping, "store/${android.net.Uri.encode(storeName.ifEmpty { "-" })}?lists=${android.net.Uri.encode(listIds.joinToString(","))}")
