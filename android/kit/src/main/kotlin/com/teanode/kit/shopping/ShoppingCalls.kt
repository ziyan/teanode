package com.teanode.kit.shopping

import com.teanode.kit.Connection
import com.teanode.kit.graphQLCall
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.decodeFromJsonElement

// What the app asks of the server about shopping lists: the same calls as Shopping.swift.

private fun text(value: String): JsonElement = JsonPrimitive(value)

private fun flag(value: Boolean): JsonElement = JsonPrimitive(value)

private fun strings(values: List<String>): JsonElement = JsonArray(values.map { value -> JsonPrimitive(value) })

private fun orNull(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

/** Reads one field of an answer the lenient way: a field the server does not send is empty. */
private suspend inline fun <reified Value> Connection.shoppingField(
    document: String,
    fieldName: String,
    variables: Map<String, JsonElement> = emptyMap(),
    requestTimeoutMS: Long? = null,
): Value {
    val data: JsonObject = execute(document, variables, requestTimeoutMS)
    val element = data[fieldName]
    if (element == null || element is JsonNull) throw com.teanode.kit.TeaNodeError.UnreadableAnswer
    return ShoppingJson.decodeFromJsonElement<Value>(element)
}

// MARK: Reading

/** The lists the person reaches, their own first. */
suspend fun Connection.shoppingLists(): List<ShoppingList> = shoppingField(ShoppingDocuments.SHOPPING_LISTS, "ListShoppingLists")

/** One list with its items (of the states given, or every state), who is shopping it now. */
suspend fun Connection.shoppingList(
    shoppingListId: String,
    states: List<ShoppingItemState> = emptyList(),
    offset: Int = 0,
    pageSize: Int = 500,
): ShoppingListPage {
    val variables = mutableMapOf(
        "shoppingListId" to text(shoppingListId), "offset" to JsonPrimitive(offset), "pageSize" to JsonPrimitive(pageSize),
    )
    if (states.isNotEmpty()) variables["shoppingItemStates"] = strings(states.map { state -> state.rawValue })
    return shoppingField(ShoppingDocuments.SHOPPING_LIST, "GetShoppingList", variables)
}

/** A whole list, read in pages of [pageSize] items until totalCount, since the screen groups and counts every item it has. */
suspend fun Connection.wholeShoppingList(
    shoppingListId: String,
    states: List<ShoppingItemState> = emptyList(),
    pageSize: Int = 500,
): ShoppingListPage {
    val first = shoppingList(shoppingListId, states, pageSize = pageSize)
    val items = first.shoppingItems.toMutableList()
    while (items.size < first.totalCount) {
        val next = shoppingList(shoppingListId, states, offset = items.size, pageSize = pageSize)
        if (next.shoppingItems.isEmpty()) break
        items.addAll(next.shoppingItems)
    }
    return ShoppingListPage(first.shoppingList, items, first.shoppingTrips, first.totalCount)
}

suspend fun Connection.shoppingItem(shoppingItemId: String): ShoppingItem =
    shoppingField(ShoppingDocuments.SHOPPING_ITEM, "GetShoppingItem", mapOf("shoppingItemId" to text(shoppingItemId)))

/** The shares of one of the person's own lists. */
suspend fun Connection.shoppingListShares(shoppingListId: String): List<ShoppingListShare> =
    shoppingField(ShoppingDocuments.SHOPPING_LIST_SHARES, "ListShoppingListShares", mapOf("shoppingListId" to text(shoppingListId)))

/** The stores the person's lists have items for. */
suspend fun Connection.shoppingStores(): List<ShoppingStore> = shoppingField(ShoppingDocuments.SHOPPING_STORES, "ListShoppingStores")

/**
 * The branches near a position (where the person was last reported to be when null) of the stores their lists need
 * something from, for the phone to watch.
 */
suspend fun Connection.watchedStores(latitudeDegrees: Double?, longitudeDegrees: Double?): List<WatchedStore> {
    val variables = mutableMapOf<String, JsonElement>()
    if (latitudeDegrees != null && longitudeDegrees != null) {
        variables["latitudeDegrees"] = JsonPrimitive(latitudeDegrees)
        variables["longitudeDegrees"] = JsonPrimitive(longitudeDegrees)
    }
    return shoppingField(ShoppingDocuments.WATCHED_STORES, "ListWatchedStores", variables)
}

/**
 * The store screen: the items of the lists included (every list with items there when none is named) that are for
 * the store, and for any store when asked.
 */
suspend fun Connection.shoppingStoreItems(
    storeName: String,
    shoppingListIds: List<String> = emptyList(),
    isAnyStoreIncluded: Boolean = true,
): ShoppingStoreItems {
    val variables = mutableMapOf("storeName" to text(storeName), "isAnyStoreIncluded" to flag(isAnyStoreIncluded))
    if (shoppingListIds.isNotEmpty()) variables["shoppingListIds"] = strings(shoppingListIds)
    return shoppingField(ShoppingDocuments.SHOPPING_STORE_ITEMS, "ListShoppingStoreItems", variables)
}

/** The trips open now on the lists the person reaches. */
suspend fun Connection.openShoppingTrips(): List<ShoppingTrip> = shoppingField(ShoppingDocuments.OPEN_SHOPPING_TRIPS, "ListOpenShoppingTrips")

/** An item's prices, newest first. */
suspend fun Connection.shoppingItemPrices(shoppingItemId: String): List<ShoppingItemPrice> =
    shoppingField(ShoppingDocuments.SHOPPING_ITEM_PRICES, "ListShoppingItemPrices", mapOf("shoppingItemId" to text(shoppingItemId)))

/** The items that read like what is about to be added, on this list and the person's others, nearest first. */
suspend fun Connection.similarShoppingItems(shoppingListId: String, itemDescription: String): List<ShoppingItem> =
    shoppingField(
        ShoppingDocuments.SIMILAR_SHOPPING_ITEMS, "ListSimilarShoppingItems",
        mapOf("shoppingListId" to text(shoppingListId), "itemDescription" to text(itemDescription)),
    )

/** What one of the person's receipts did to their lists. */
suspend fun Connection.shoppingReceipt(financeReceiptId: String): ShoppingReceipt =
    shoppingField(ShoppingDocuments.SHOPPING_RECEIPT_MATCHES, "ListShoppingReceiptMatches", mapOf("financeReceiptId" to text(financeReceiptId)))

/** Reads pasted text and uploaded pictures (the agent's attachments) as a list, and says what each item would merge into; nothing is added. */
suspend fun Connection.previewShoppingImport(shoppingListId: String?, importText: String, agentAttachmentIds: List<String>): ShoppingImportPreview {
    val variables = mutableMapOf<String, JsonElement>()
    if (shoppingListId != null) variables["shoppingListId"] = text(shoppingListId)
    val trimmedText = importText.trim()
    if (trimmedText.isNotEmpty()) variables["importText"] = text(trimmedText)
    if (agentAttachmentIds.isNotEmpty()) variables["agentAttachmentIds"] = strings(agentAttachmentIds)
    return shoppingField(ShoppingDocuments.PREVIEW_SHOPPING_IMPORT, "PreviewShoppingImport", variables, requestTimeoutMS = 180_000)
}

// MARK: Lists

suspend fun Connection.createShoppingList(listName: String): ShoppingList =
    shoppingField(ShoppingDocuments.CREATE_SHOPPING_LIST, "CreateShoppingList", mapOf("listName" to text(listName)))

suspend fun Connection.renameShoppingList(shoppingListId: String, listName: String): ShoppingList =
    shoppingField(
        ShoppingDocuments.RENAME_SHOPPING_LIST, "RenameShoppingList",
        mapOf("shoppingListId" to text(shoppingListId), "listName" to text(listName)),
    )

suspend fun Connection.deleteShoppingList(shoppingListId: String) {
    graphQLCall(ShoppingDocuments.DELETE_SHOPPING_LIST, mapOf("shoppingListId" to text(shoppingListId)))
}

/** Shares one of the person's lists with a person or a circle (one of the two). */
suspend fun Connection.shareShoppingList(shoppingListId: String, granteeUserId: String?, granteeCircleId: String?): ShoppingListShare =
    shoppingField(
        ShoppingDocuments.SHARE_SHOPPING_LIST, "ShareShoppingList",
        mapOf("shoppingListId" to text(shoppingListId), "granteeUserId" to orNull(granteeUserId), "granteeCircleId" to orNull(granteeCircleId)),
    )

suspend fun Connection.unshareShoppingList(shoppingListShareId: String) {
    graphQLCall(ShoppingDocuments.UNSHARE_SHOPPING_LIST, mapOf("shoppingListShareId" to text(shoppingListShareId)))
}

/** Whether the person is reminded of a list on arriving at a store. */
suspend fun Connection.setShoppingListReminder(shoppingListId: String, isReminded: Boolean) {
    graphQLCall(ShoppingDocuments.SET_SHOPPING_LIST_REMINDER, mapOf("shoppingListId" to text(shoppingListId), "isReminded" to flag(isReminded)))
}

/** Whether the person is reminded of what they need on arriving at a store. */
suspend fun Connection.setShoppingStoreReminder(storeName: String, isReminded: Boolean) {
    graphQLCall(ShoppingDocuments.SET_SHOPPING_STORE_REMINDER, mapOf("storeName" to text(storeName), "isReminded" to flag(isReminded)))
}

// MARK: Items

/** Adds items to a list; one it has already is merged into. */
suspend fun Connection.addShoppingItems(shoppingListId: String, entries: List<ShoppingItemEntry>): List<ShoppingItemAdded> =
    shoppingField(
        ShoppingDocuments.ADD_SHOPPING_ITEMS, "AddShoppingItems",
        mapOf("shoppingListId" to text(shoppingListId), "shoppingItems" to JsonArray(entries.map { entry -> entry.variables })),
    )

/** Changes what is given of an item; what is null stays as it is. */
suspend fun Connection.updateShoppingItem(
    shoppingItemId: String,
    itemDescription: String? = null,
    itemDetails: String? = null,
    storeName: String? = null,
    isStoreBrand: Boolean? = null,
    aisleName: String? = null,
    neededQuantity: String? = null,
    quantityUnit: String? = null,
): ShoppingItem {
    val variables = mutableMapOf("shoppingItemId" to text(shoppingItemId))
    if (itemDescription != null) variables["itemDescription"] = text(itemDescription)
    if (itemDetails != null) variables["itemDetails"] = text(itemDetails)
    if (storeName != null) variables["storeName"] = text(storeName)
    if (isStoreBrand != null) variables["isStoreBrand"] = flag(isStoreBrand)
    if (aisleName != null) variables["aisleName"] = text(aisleName)
    if (neededQuantity != null) variables["neededQuantity"] = text(neededQuantity)
    if (quantityUnit != null) variables["quantityUnit"] = text(quantityUnit)
    return shoppingField(ShoppingDocuments.UPDATE_SHOPPING_ITEM, "UpdateShoppingItem", variables)
}

suspend fun Connection.removeShoppingItems(shoppingItemIds: List<String>) {
    graphQLCall(ShoppingDocuments.REMOVE_SHOPPING_ITEMS, mapOf("shoppingItemIds" to strings(shoppingItemIds)))
}

/**
 * Puts an item in the cart, or back to needed. The first item the person puts in the cart starts their trip, at
 * [storeName] (the item's store when empty), and the others the list reaches are told.
 */
suspend fun Connection.checkShoppingItem(shoppingItemId: String, isChecked: Boolean, storeName: String = ""): ShoppingItem {
    val variables = mutableMapOf("shoppingItemId" to text(shoppingItemId), "isChecked" to flag(isChecked))
    if (storeName.isNotEmpty()) variables["storeName"] = text(storeName)
    return shoppingField(ShoppingDocuments.CHECK_SHOPPING_ITEM, "CheckShoppingItem", variables)
}

/** Needs an item again with a count, or marks it not needed. */
suspend fun Connection.setShoppingItemNeed(shoppingItemId: String, isNeeded: Boolean, neededQuantity: String? = null): ShoppingItem {
    val variables = mutableMapOf("shoppingItemId" to text(shoppingItemId), "isNeeded" to flag(isNeeded))
    if (neededQuantity != null) variables["neededQuantity"] = text(neededQuantity)
    return shoppingField(ShoppingDocuments.SET_SHOPPING_ITEM_NEED, "SetShoppingItemNeed", variables)
}

suspend fun Connection.setShoppingItemOutOfStock(shoppingItemId: String, isOutOfStock: Boolean, storeName: String? = null): ShoppingItem {
    val variables = mutableMapOf("shoppingItemId" to text(shoppingItemId), "isOutOfStock" to flag(isOutOfStock))
    if (!storeName.isNullOrEmpty()) variables["storeName"] = text(storeName)
    return shoppingField(ShoppingDocuments.SET_SHOPPING_ITEM_OUT_OF_STOCK, "SetShoppingItemOutOfStock", variables)
}

/** Records a price seen for an item, at its store unless one is given. */
suspend fun Connection.recordShoppingItemPrice(
    shoppingItemId: String,
    unitPriceAmount: String,
    storeName: String? = null,
    currencyCode: String? = null,
) {
    val variables = mutableMapOf("shoppingItemId" to text(shoppingItemId), "unitPriceAmount" to text(unitPriceAmount))
    if (!storeName.isNullOrEmpty()) variables["storeName"] = text(storeName)
    if (!currencyCode.isNullOrEmpty()) variables["currencyCode"] = text(currencyCode)
    graphQLCall(ShoppingDocuments.RECORD_SHOPPING_ITEM_PRICE, variables)
}

/** Queues the search for another picture of an item; the picture appears on the item when it is found. */
suspend fun Connection.findShoppingItemPicture(shoppingItemId: String) {
    graphQLCall(ShoppingDocuments.FIND_SHOPPING_ITEM_PICTURE, mapOf("shoppingItemId" to text(shoppingItemId)))
}

/** Gives an item a picture uploaded as an attachment, or none. */
suspend fun Connection.setShoppingItemPicture(shoppingItemId: String, agentAttachmentId: String?): ShoppingItem =
    shoppingField(
        ShoppingDocuments.SET_SHOPPING_ITEM_PICTURE, "SetShoppingItemPicture",
        mapOf("shoppingItemId" to text(shoppingItemId), "agentAttachmentId" to orNull(agentAttachmentId)),
    )

// MARK: Shopping

/**
 * Starts the person shopping a store for some lists, which tells the others the lists reach. An empty store name is
 * no store in particular.
 */
suspend fun Connection.startShopping(storeName: String, shoppingListIds: List<String>): ShoppingTrip {
    val variables = mutableMapOf("shoppingListIds" to strings(shoppingListIds))
    if (storeName.isNotEmpty()) variables["storeName"] = text(storeName)
    return shoppingField(ShoppingDocuments.START_SHOPPING, "StartShopping", variables)
}

/**
 * Done: what the person put in the cart is bought, no receipt needed. With no trip named it ends their own trip, or
 * buys their cart when none is open.
 */
suspend fun Connection.finishShopping(shoppingTripId: String? = null): ShoppingTrip {
    val variables = mutableMapOf<String, JsonElement>()
    if (!shoppingTripId.isNullOrEmpty()) variables["shoppingTripId"] = text(shoppingTripId)
    return shoppingField(ShoppingDocuments.FINISH_SHOPPING, "FinishShopping", variables)
}

// MARK: Receipts

/** Asks for a receipt to be matched against the person's lists again. */
suspend fun Connection.matchShoppingReceipt(financeReceiptId: String) {
    graphQLCall(ShoppingDocuments.MATCH_SHOPPING_RECEIPT, mapOf("financeReceiptId" to text(financeReceiptId)))
}

/** Applies a proposed match, or dismisses one (undoing it if it was applied). */
suspend fun Connection.decideShoppingReceiptMatch(shoppingReceiptMatchId: String, isApplied: Boolean): ShoppingReceiptMatch =
    shoppingField(
        ShoppingDocuments.DECIDE_SHOPPING_RECEIPT_MATCH, "DecideShoppingReceiptMatch",
        mapOf("shoppingReceiptMatchId" to text(shoppingReceiptMatchId), "isApplied" to flag(isApplied)),
    )

/** Puts back everything a receipt did to the lists. */
suspend fun Connection.undoShoppingReceipt(financeReceiptId: String) {
    graphQLCall(ShoppingDocuments.UNDO_SHOPPING_RECEIPT, mapOf("financeReceiptId" to text(financeReceiptId)))
}

/** Adds what a receipt line bought to a list as an item, not needed. */
suspend fun Connection.addShoppingItemFromReceiptLine(financeReceiptLineId: String, shoppingListId: String): ShoppingItem =
    shoppingField(
        ShoppingDocuments.ADD_SHOPPING_ITEM_FROM_RECEIPT_LINE, "AddShoppingItemFromReceiptLine",
        mapOf("financeReceiptLineId" to text(financeReceiptLineId), "shoppingListId" to text(shoppingListId)),
    )

/**
 * ShoppingFeed streams which shopping list changed: each time a list the viewer reaches changes (an item added,
 * checked off, a trip started), the server says which, and the screen reads that list again. It is opened while a
 * shopping screen is visible. The stream ends with an error when the connection drops, and the screen opens a new one.
 */
object ShoppingFeed {
    fun events(connection: Connection): Flow<ShoppingEvent> =
        connection.subscribe(ShoppingDocuments.SHOPPING_EVENTS).mapNotNull { data ->
            val event = (data as? JsonObject)?.get("ShoppingEvents") ?: return@mapNotNull null
            ShoppingJson.decodeFromJsonElement(ShoppingEvent.serializer(), event)
        }
}
