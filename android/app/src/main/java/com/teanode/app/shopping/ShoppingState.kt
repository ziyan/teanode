package com.teanode.app.shopping

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.shopping.ShoppingFeed
import com.teanode.kit.shopping.ShoppingGrouping
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.ShoppingList
import com.teanode.kit.shopping.ShoppingMoney
import com.teanode.kit.shopping.ShoppingStore
import com.teanode.kit.shopping.ShoppingStoreItems
import com.teanode.kit.shopping.ShoppingStoreList
import com.teanode.kit.shopping.ShoppingStoreSections
import com.teanode.kit.shopping.ShoppingCartProgress
import com.teanode.kit.shopping.ShoppingTotal
import com.teanode.kit.shopping.ShoppingTrip
import com.teanode.kit.shopping.createShoppingList
import com.teanode.kit.shopping.checkShoppingItem
import com.teanode.kit.shopping.deleteShoppingList
import com.teanode.kit.shopping.finishShopping
import com.teanode.kit.shopping.otherShoppers
import com.teanode.kit.shopping.ownTrip
import com.teanode.kit.shopping.removeShoppingItems
import com.teanode.kit.shopping.renameShoppingList
import com.teanode.kit.shopping.setShoppingItemNeed
import com.teanode.kit.shopping.setShoppingItemOutOfStock
import com.teanode.kit.shopping.setShoppingListReminder
import com.teanode.kit.shopping.setShoppingStoreReminder
import com.teanode.kit.shopping.shoppingListShares
import com.teanode.kit.shopping.shoppingLists
import com.teanode.kit.shopping.shoppingStoreItems
import com.teanode.kit.shopping.shoppingStores
import com.teanode.kit.shopping.shownGrantees
import com.teanode.kit.shopping.wholeShoppingList
import com.teanode.kit.shopping.addShoppingItems
import com.teanode.kit.shopping.ShoppingItemEntry
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// The state behind the Shopping screens: the person's lists, one list, and the store they are shopping at. Each reads
// the server, keeps what it read in order, and reads again when the server says a list changed.

/**
 * Keeps a screen current while it is on screen: it reads again each time the server says a list changed, and every
 * thirty seconds in case a message was missed. It ends when the screen goes away. Demo mode has no socket to listen to.
 */
@Composable
internal fun FollowShopping(connection: Connection, isDemo: Boolean, refresh: suspend () -> Unit) {
    if (isDemo) return
    LaunchedEffect(connection) {
        val listening = launch {
            var delayMS = 1_000L
            while (true) {
                try {
                    ShoppingFeed.events(connection).collect {
                        refresh()
                        delayMS = 1_000L
                    }
                } catch (failure: CancellationException) {
                    throw failure
                } catch (_: Throwable) {
                    // The feed dropped; what was missed is read again below.
                }
                delay(delayMS)
                delayMS = minOf(delayMS * 2, 30_000L)
                refresh()
            }
        }
        val polling = launch {
            while (true) {
                delay(30_000L)
                refresh()
            }
        }
        try {
            listening.join()
            polling.join()
        } finally {
            listening.cancel()
            polling.cancel()
        }
    }
}

/** The Shopping screen: the person's lists, the lists shared with them, and the stores their lists have items for. */
internal class ShoppingHomeState(private val connection: Connection, private val scope: CoroutineScope, private val snackbar: SnackbarController) {
    var lists by mutableStateOf<List<ShoppingList>>(emptyList())
        private set
    var stores by mutableStateOf<List<ShoppingStore>>(emptyList())
        private set

    /** Who each of the person's shared lists is shared with, by list. */
    var granteeNames by mutableStateOf<Map<String, List<String>>>(emptyMap())
        private set
    var hasLoaded by mutableStateOf(false)
        private set

    /** Why the first read failed, when there is nothing to show instead. */
    var loadFailure by mutableStateOf<Throwable?>(null)
        private set

    val ownLists: List<ShoppingList> get() = lists.filter { list -> list.isOwner }
    val sharedLists: List<ShoppingList> get() = lists.filter { list -> !list.isOwner }

    suspend fun load() {
        try {
            lists = connection.shoppingLists()
            hasLoaded = true
            loadFailure = null
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (hasLoaded) snackbar.showError(failure) else loadFailure = failure
            return
        }
        stores = try {
            connection.shoppingStores()
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            stores
        }
        val names = mutableMapOf<String, List<String>>()
        for (list in lists.filter { each -> each.isOwner && each.isShared }) {
            names[list.id] = try {
                connection.shoppingListShares(list.id).shownGrantees
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                granteeNames[list.id] ?: emptyList()
            }
        }
        granteeNames = names
    }

    fun reload() {
        loadFailure = null
        scope.launch { load() }
    }

    /** Makes a list and returns it, null when the server refused. */
    suspend fun create(listName: String): ShoppingList? = try {
        val created = connection.createShoppingList(listName)
        load()
        created
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        snackbar.showError(failure)
        null
    }
}

/** One list: its items, who is shopping it, who it is shared with, and the changes a person makes to it. */
internal class ShoppingListState(
    private val connection: Connection,
    val listId: String,
    private val scope: CoroutineScope,
    private val snackbar: SnackbarController,
    private val say: (String) -> Unit,
    private val ownUserId: String?,
) {
    var list by mutableStateOf<ShoppingList?>(null)
        private set
    var items by mutableStateOf<List<ShoppingItem>>(emptyList())
        private set
    var trips by mutableStateOf<List<ShoppingTrip>>(emptyList())
        private set
    var granteeNames by mutableStateOf<List<String>>(emptyList())
        private set
    var stores by mutableStateOf<List<ShoppingStore>>(emptyList())
        private set

    /** Whether the person is reminded of the list at a store; kept here so a switch answers at once. */
    var isReminded by mutableStateOf(true)
        private set
    var hasLoaded by mutableStateOf(false)
        private set
    var isDeleted by mutableStateOf(false)
        private set
    var loadFailure by mutableStateOf<Throwable?>(null)
        private set

    val toBuy: List<ShoppingItem> get() = items.filter { item -> !item.isNotNeeded }
    val notNeeded: List<ShoppingItem> get() = ShoppingGrouping.split(items).second
    val neededCount: Int get() = items.count { item -> item.isNeeded }
    val estimatedTotal: ShoppingTotal? get() = ShoppingMoney.estimate(items.filter { item -> item.isNeeded })

    suspend fun load() {
        try {
            val page = connection.wholeShoppingList(listId)
            page.shoppingList?.let { read ->
                list = read
                isReminded = read.isReminded
            }
            items = page.shoppingItems
            trips = page.shoppingTrips
            hasLoaded = true
            loadFailure = null
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (hasLoaded) snackbar.showError(failure) else loadFailure = failure
            return
        }
        granteeNames = if (list?.isOwner == true && list?.isShared == true) {
            try {
                connection.shoppingListShares(listId).shownGrantees
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                granteeNames
            }
        } else {
            emptyList()
        }
    }

    fun reload() {
        loadFailure = null
        scope.launch { load() }
    }

    /** Puts a changed item where it is in the list, or at its end. */
    fun replace(changed: ShoppingItem) {
        val index = items.indexOfFirst { item -> item.id == changed.id }
        items = when {
            index >= 0 -> items.toMutableList().also { copy -> copy[index] = changed }
            changed.shoppingListId == listId -> items + changed
            else -> items
        }
    }

    private suspend fun attempt(work: suspend () -> Unit): Boolean = try {
        work()
        true
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        snackbar.showError(failure)
        false
    }

    /** Needs a bought item again, with a count; true when it was done. */
    suspend fun needAgain(item: ShoppingItem, quantity: String, doneText: (String) -> String): Boolean {
        val isDone = attempt { replace(connection.setShoppingItemNeed(item.id, true, quantity)) }
        if (isDone) say(doneText(item.itemDescription))
        return isDone
    }

    suspend fun remove(item: ShoppingItem, doneText: (String) -> String) {
        val isDone = attempt {
            connection.removeShoppingItems(listOf(item.id))
            items = items.filter { each -> each.id != item.id }
        }
        if (isDone) say(doneText(item.itemDescription))
    }

    /** Adds items to the list; true when they were added. */
    suspend fun add(entries: List<ShoppingItemEntry>): Boolean = attempt {
        connection.addShoppingItems(listId, entries)
        load()
    }

    suspend fun rename(listName: String) {
        if (attempt { connection.renameShoppingList(listId, listName) }) load()
    }

    /** Deletes the list (the owner's); true when it is gone. */
    suspend fun delete(): Boolean {
        val isDone = attempt { connection.deleteShoppingList(listId) }
        if (isDone) isDeleted = true
        return isDone
    }

    fun setReminder(isOn: Boolean) {
        val previous = isReminded
        isReminded = isOn
        scope.launch {
            if (!attempt { connection.setShoppingListReminder(listId, isOn) }) isReminded = previous
        }
    }

    /** Whether Done has anything to do here: the person has a trip open on this list, or has put something on it in the cart. */
    val isShopping: Boolean
        get() = trips.any { trip -> trip.isOpen && trip.userId == ownUserId } || items.any { item -> item.isInCart && item.cartedByUserId == ownUserId }

    /** Done: what the person put in the cart is bought, no receipt needed. */
    suspend fun finish(doneText: String) {
        val isDone = attempt {
            connection.finishShopping()
            load()
        }
        if (isDone) say(doneText)
    }

    /** Reads the stores the lists have items for, to choose one to shop at. */
    suspend fun loadStores() {
        stores = try {
            connection.shoppingStores()
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            stores
        }
    }

    /**
     * The stores this list has something to buy at: the stores the server lists that the list's needed items are for,
     * or, if the server could not be asked, the names on the items.
     */
    val storeChoices: List<String>
        get() {
            val wanted = toBuy.filter { item -> !item.isForAnyStore }
            val keys = mutableListOf<String>()
            for (item in wanted) if (item.storeKey !in keys) keys.add(item.storeKey)
            return keys.mapNotNull { key ->
                stores.firstOrNull { store -> store.storeKey == key }?.storeName ?: wanted.firstOrNull { item -> item.storeKey == key }?.storeName
            }
        }
}

/**
 * The screen of shopping one store: the needed items of the lists included that are for the store or for any store,
 * each a check to put in the cart, and the person's trip, which the server starts when they put the first item in the
 * cart and which ends with Done.
 */
internal class ShoppingStoreState(
    private val connection: Connection,
    val storeName: String,
    listIds: List<String>,
    private val scope: CoroutineScope,
    private val snackbar: SnackbarController,
    private val ownUserId: String?,
) {
    var items by mutableStateOf<List<ShoppingItem>>(emptyList())
        private set
    var lists by mutableStateOf<List<ShoppingStoreList>>(emptyList())
        private set
    var trips by mutableStateOf<List<ShoppingTrip>>(emptyList())
        private set
    var ownTrip by mutableStateOf<ShoppingTrip?>(null)
        private set
    var isReminded by mutableStateOf(true)
        private set
    var hasLoaded by mutableStateOf(false)
        private set
    var loadFailure by mutableStateOf<Throwable?>(null)
        private set

    /** The lists included, as chosen; empty until the first read says which lists have items here, when all of them are. */
    private var includedListIds: List<String> = listIds

    /** Checks answered at once and not yet confirmed by the server, by item. */
    private val pendingChecks = mutableStateMapOf<String, Boolean>()

    val storeKey: String get() = com.teanode.kit.shopping.ShoppingStoreKey.key(storeName)
    private val shownItems: List<ShoppingItem> get() = items.filter { item -> !item.isNotNeeded }
    val progress: ShoppingCartProgress get() = ShoppingCartProgress.of(shownItems)
    val sections: ShoppingStoreSections get() = ShoppingGrouping.forStore(shownItems)
    val otherShopperNames: List<String> get() = trips.otherShoppers(storeKey, ownUserId)
    val includedCount: Int get() = lists.count { list -> list.isIncluded }

    /** Whether Done has anything to do: the person has a trip open, or has put something here in the cart. */
    val isShopping: Boolean get() = ownTrip != null || items.any { item -> item.isInCart && item.cartedByUserId == ownUserId }

    /** Whether an item is checked, as far as the person has just said. */
    fun isChecked(item: ShoppingItem): Boolean = pendingChecks[item.id] ?: item.isInCart

    suspend fun load() {
        try {
            val read = connection.shoppingStoreItems(storeName, includedListIds)
            items = read.shoppingItems
            lists = read.shoppingLists
            trips = read.shoppingTrips
            isReminded = read.isReminded
            if (includedListIds.isEmpty()) includedListIds = read.includedListIds
            hasLoaded = true
            loadFailure = null
            ownTrip = trips.firstOrNull { trip -> trip.isOpen && trip.userId == ownUserId }
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (hasLoaded) snackbar.showError(failure) else loadFailure = failure
        }
    }

    fun reload() {
        loadFailure = null
        scope.launch { load() }
    }

    private fun replace(changed: ShoppingItem) {
        val index = items.indexOfFirst { item -> item.id == changed.id }
        if (index >= 0) items = items.toMutableList().also { copy -> copy[index] = changed }
    }

    /** Puts an item in the cart or takes it out, showing it at once. */
    fun toggle(item: ShoppingItem) {
        val isChecking = !isChecked(item)
        pendingChecks[item.id] = isChecking
        scope.launch {
            try {
                // The server starts the person's trip with the first item they put in the cart, at this store, and tells the others.
                replace(connection.checkShoppingItem(item.id, isChecking, storeName))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            pendingChecks.remove(item.id)
            if (isChecking && ownTrip == null) load()
        }
    }

    fun setOutOfStock(item: ShoppingItem, isOutOfStock: Boolean) {
        scope.launch {
            try {
                replace(connection.setShoppingItemOutOfStock(item.id, isOutOfStock, storeName))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
        }
    }

    /** Includes a list in the screen, or leaves it out. */
    fun setIncluded(list: ShoppingStoreList, isIncluded: Boolean) {
        val ids = (if (includedListIds.isEmpty()) lists.filter { each -> each.isIncluded }.map { each -> each.shoppingListId } else includedListIds)
            .filter { id -> id != list.shoppingListId }.toMutableList()
        if (isIncluded) ids.add(list.shoppingListId)
        // At least one list stays, or the server would take none to mean all.
        if (ids.isEmpty()) return
        includedListIds = ids
        scope.launch { load() }
    }

    fun setReminder(isOn: Boolean) {
        val previous = isReminded
        isReminded = isOn
        scope.launch {
            try {
                connection.setShoppingStoreReminder(storeName, isOn)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                isReminded = previous
                snackbar.showError(failure)
            }
        }
    }

    /** Done: what the person put in the cart is bought, no receipt needed; true when it is over. */
    suspend fun finish(): Boolean = try {
        connection.finishShopping()
        ownTrip = null
        true
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        snackbar.showError(failure)
        false
    }
}
