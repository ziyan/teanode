package com.teanode.kit.shopping

import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.time.Instant
import java.time.LocalDate

// Shopping lists as the server keeps them: named lists of things to buy, shared with a circle or a person, checked
// off live at the store, by a receipt, and remembered with their prices. The dashboard, the command line and the
// agent's shopping tool use the same operations. ShoppingFeed follows the lists as they change; the screens read them
// back here.

/**
 * How the server's answers are read: a field a server does not send reads as empty, and one it sends as null reads
 * as empty too, so a screen made for a newer server still works with an older one.
 */
val ShoppingJson: Json = Json(from = TeaNodeJson) { coerceInputValues = true }

/** The time an RFC 3339 text stands for; null for anything else. Go writes up to nine digits of fractions of a second. */
internal fun serverInstant(text: String?): Instant? {
    if (text.isNullOrEmpty()) return null
    return try {
        Instant.parse(text)
    } catch (_: Exception) {
        try {
            java.time.OffsetDateTime.parse(text).toInstant()
        } catch (_: Exception) {
            null
        }
    }
}

// MARK: Items

/** Where an item is: wanted, put in the cart at the store, or bought and kept on the list to be wanted again. */
enum class ShoppingItemState(val rawValue: String) {
    Needed("needed"),
    InCart("in_cart"),
    NotNeeded("not_needed"),
}

/** One thing on one list. */
@Serializable
data class ShoppingItem(
    val id: String,
    val shoppingListId: String = "",
    val listName: String = "",
    val itemDescription: String = "",
    val itemDetails: String = "",
    /** The store to get it from; empty for any store. */
    val storeName: String = "",
    val storeKey: String = "",
    val isStoreBrand: Boolean = false,
    val aisleName: String = "",
    val aisleRank: Int = 0,
    val itemRank: Int = 0,
    /** How many are needed, a number as text. */
    val neededQuantity: String = "",
    val quantityUnit: String = "",
    /** The latest price of one, a number as text; empty when none is known. */
    val unitPriceAmount: String = "",
    val currencyCode: String = "",
    /** needed, in_cart or not_needed. */
    val shoppingItemState: String = "",
    val cartedByUserId: String = "",
    val cartedByName: String = "",
    val cartedAt: String? = null,
    val outOfStockAt: String? = null,
    val outOfStockByUserId: String = "",
    val outOfStockByName: String = "",
    val outOfStockStoreName: String = "",
    val lastBoughtAt: String? = null,
    val pictureFileId: String = "",
    val picturePageUrl: String = "",
    /** Where the picture is served from, under /api/v1; empty when there is none. */
    val picturePath: String = "",
    /** "", "web" or "upload". */
    val pictureSource: String = "",
    val pictureSearchedAt: String? = null,
) {
    /** The state, needed for one the app does not know. */
    val state: ShoppingItemState get() = ShoppingItemState.entries.firstOrNull { each -> each.rawValue == shoppingItemState } ?: ShoppingItemState.Needed
    val isNeeded: Boolean get() = state == ShoppingItemState.Needed
    val isInCart: Boolean get() = state == ShoppingItemState.InCart
    val isNotNeeded: Boolean get() = state == ShoppingItemState.NotNeeded

    /** Whether somebody found the store out of it. */
    val isOutOfStock: Boolean get() = !outOfStockAt.isNullOrEmpty()
    val hasPicture: Boolean get() = picturePath.isNotEmpty()
    val isForAnyStore: Boolean get() = storeKey.isEmpty()

    /** The price of one, null when none is known. */
    val unitPrice: java.math.BigDecimal? get() = ShoppingMoney.unitPrice(this)
    val shownUnitPrice: String get() = ShoppingMoney.shown(unitPriceAmount, currencyCode)
    val lastBoughtInstant: Instant? get() = serverInstant(lastBoughtAt)

    /** The count as a person reads it: "2", "1.5". */
    val shownQuantity: String get() = ShoppingQuantity.shown(neededQuantity)

    /** The count with its unit, "2" or "1.5 lb". */
    val shownQuantityWithUnit: String get() = if (quantityUnit.isEmpty()) shownQuantity else "$shownQuantity $quantityUnit"

    /** What the store screen calls the item: "2 Apples", "1.5 lb Apples", or just "Apples" for one. */
    val shownTitle: String
        get() {
            if (shownQuantity == "1" && quantityUnit.isEmpty()) return itemDescription
            return "$shownQuantityWithUnit $itemDescription"
        }

    /** The first letter of the description, which stands for the item where a picture is missing. */
    val initial: String get() = itemDescription.firstOrNull()?.uppercase() ?: "?"
}

/** Somebody shopping a store now, for some lists. */
@Serializable
data class ShoppingTrip(
    val id: String,
    val userId: String = "",
    val shopperName: String = "",
    val shopperUsername: String = "",
    val storeName: String = "",
    val storeKey: String = "",
    val shoppingListIds: List<String> = emptyList(),
    val startedAt: String = "",
    val finishedAt: String? = null,
) {
    /** Who is shopping: their name, or their username when they have no name. */
    val shownShopper: String get() = shopperName.ifEmpty { shopperUsername }

    /** Whether it is still going. */
    val isOpen: Boolean get() = finishedAt.isNullOrEmpty()
}

/**
 * The names of the other people shopping a store now, once each: trips still open at the store (at any store when
 * [storeKey] is empty), not the person's own.
 */
fun List<ShoppingTrip>.otherShoppers(storeKey: String, ownUserId: String?): List<String> {
    val names = mutableListOf<String>()
    for (trip in this) {
        if (trip.isOpen && trip.userId != ownUserId && (storeKey.isEmpty() || trip.storeKey == storeKey)) {
            if (trip.shownShopper !in names) names.add(trip.shownShopper)
        }
    }
    return names
}

/** The trips of the other people shopping a list now. */
fun List<ShoppingTrip>.openTrips(shoppingListId: String, ownUserId: String?): List<ShoppingTrip> =
    filter { trip -> trip.isOpen && trip.userId != ownUserId && shoppingListId in trip.shoppingListIds }

/** The person's own open trip at a store, if they have one. */
fun List<ShoppingTrip>.ownTrip(storeKey: String, ownUserId: String?): ShoppingTrip? =
    firstOrNull { trip -> trip.isOpen && trip.userId == ownUserId && trip.storeKey == storeKey }

// MARK: Lists

/** A named list with its counts, as the person reaches it. */
@Serializable
data class ShoppingList(
    val id: String,
    val userId: String = "",
    val listName: String = "",
    val ownerName: String = "",
    val ownerUsername: String = "",
    val isOwner: Boolean = false,
    val isShared: Boolean = false,
    /** Whether the person is reminded of the list on arriving at a store. */
    val isReminded: Boolean = false,
    val neededCount: Int = 0,
    val inCartCount: Int = 0,
    val notNeededCount: Int = 0,
    val createdAt: String = "",
    val updatedAt: String = "",
) {
    /** The owner's name, or their username when they have no name. */
    val shownOwner: String get() = ownerName.ifEmpty { ownerUsername }
}

/** One list with its items and who is shopping it now. */
@Serializable
data class ShoppingListPage(
    val shoppingList: ShoppingList? = null,
    val shoppingItems: List<ShoppingItem> = emptyList(),
    val shoppingTrips: List<ShoppingTrip> = emptyList(),
    val totalCount: Int = 0,
)

/** A list shared with a person or a circle. */
@Serializable
data class ShoppingListShare(
    val id: String,
    val shoppingListId: String = "",
    val listName: String = "",
    val ownerUserId: String = "",
    val ownerUsername: String = "",
    val ownerName: String = "",
    val granteeUserId: String = "",
    val granteeUsername: String = "",
    val granteeName: String = "",
    val granteeCircleId: String = "",
    val granteeCircleName: String = "",
    val createdAt: String = "",
) {
    /** Whether it is with a circle rather than a person. */
    val isWithCircle: Boolean get() = granteeCircleId.isNotEmpty()

    /** Who it is with: the circle's name, or the person's name, or their username when they have no name. */
    val shownGrantee: String
        get() {
            if (isWithCircle) return granteeCircleName
            return granteeName.ifEmpty { granteeUsername }
        }
}

/** The names a list is shared with, once each: "Home, Sam". */
val List<ShoppingListShare>.shownGrantees: List<String>
    get() {
        val names = mutableListOf<String>()
        for (share in this) if (share.shownGrantee !in names) names.add(share.shownGrantee)
        return names
    }

// MARK: Stores

/** A store the person's lists have items for. */
@Serializable
data class ShoppingStore(
    val storeKey: String,
    val storeName: String = "",
    val neededCount: Int = 0,
    /** Whether the person is reminded of what they need on arriving there. */
    val isReminded: Boolean = false,
) {
    val id: String get() = storeKey
}

/** What a place the phone watches is named by: "store:node:42". */
fun watchedStoreIdentifier(osmType: String, osmId: Long): String = "store:$osmType:$osmId"

/** The least radius, in meters, a watched place has. */
const val WATCHED_STORE_RADIUS_METERS_LEAST: Double = 100.0

/**
 * A branch, near the person, of a store their lists need something from with reminders on, for the phone to watch:
 * walking into it is a visit there, which brings the store's reminder.
 */
@Serializable
data class WatchedStore(
    val osmType: String,
    val osmId: Long,
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val storeName: String = "",
    val storeKey: String = "",
    /** The branch's name on the map. */
    val placeName: String = "",
    val osmCategory: String = "",
    val radiusMeters: Double = WATCHED_STORE_RADIUS_METERS_LEAST,
    val distanceMeters: Double = 0.0,
    @SerialName("placeId") private val storedPlaceId: String? = null,
) {
    val id: String get() = watchedStoreIdentifier(osmType, osmId)

    /** The person's place for the branch, when they have one. */
    val placeId: String? get() = storedPlaceId?.takeIf { text -> text.isNotEmpty() }
}

/** A list that has items at a store, one of the buttons on the store screen that include or leave out a list. */
@Serializable
data class ShoppingStoreList(
    val shoppingListId: String,
    val listName: String = "",
    val neededCount: Int = 0,
    val isIncluded: Boolean = false,
) {
    val id: String get() = shoppingListId
}

/** The store screen: the needed and in-cart items of the lists included, the lists that have items there, and who is shopping. */
@Serializable
data class ShoppingStoreItems(
    val storeName: String = "",
    val isReminded: Boolean = false,
    val shoppingItems: List<ShoppingItem> = emptyList(),
    val shoppingLists: List<ShoppingStoreList> = emptyList(),
    val shoppingTrips: List<ShoppingTrip> = emptyList(),
) {
    /** The ids of the lists included. */
    val includedListIds: List<String> get() = shoppingLists.filter { list -> list.isIncluded }.map { list -> list.shoppingListId }
}

// MARK: Prices

/** One price seen for an item at a store. */
@Serializable
data class ShoppingItemPrice(
    val id: String,
    val shoppingItemId: String = "",
    val storeName: String = "",
    val storeKey: String = "",
    val unitPriceAmount: String = "",
    val currencyCode: String = "",
    val quantity: String = "",
    val quantityUnit: String = "",
    /** The day it was seen, YYYY-MM-DD. */
    val observedOn: String = "",
    /** receipt or person. */
    val priceSource: String = "",
    val financeReceiptId: String = "",
    val createdAt: String = "",
) {
    val amount: java.math.BigDecimal? get() = ShoppingQuantity.decimal(unitPriceAmount)
    val shownAmount: String get() = ShoppingMoney.shown(unitPriceAmount, currencyCode)

    /** The day it was seen, at midnight UTC. */
    val observedInstant: Instant?
        get() = try {
            LocalDate.parse(observedOn).atStartOfDay(java.time.ZoneOffset.UTC).toInstant()
        } catch (_: Exception) {
            null
        }

    /** Whether a receipt gave it, rather than a person. */
    val isFromReceipt: Boolean get() = priceSource == "receipt"
}

// MARK: Importing

/** One item the model read out of text or pictures, and the item already on the list it would merge into, when there is one. */
@Serializable
data class ShoppingImportItem(
    val itemDescription: String = "",
    val itemDetails: String = "",
    val neededQuantity: String = "",
    val quantityUnit: String = "",
    val aisleName: String = "",
    val storeName: String = "",
    val isStoreBrand: Boolean = false,
    val mergedShoppingItemId: String = "",
    val mergedShoppingItemState: String = "",
    val mergedNeededQuantity: String = "",
) {
    /** Whether the list has the item already. */
    val isOnList: Boolean get() = mergedShoppingItemId.isNotEmpty()

    /** What a row of the preview calls the item: "2 Apples", "1.5 lb Apples", or just "Apples" for one. */
    val shownTitle: String
        get() {
            val quantity = ShoppingQuantity.shown(neededQuantity)
            if (quantity == "1" && quantityUnit.isEmpty()) return itemDescription
            return (if (quantityUnit.isEmpty()) quantity else "$quantity $quantityUnit") + " " + itemDescription
        }

    /** The item's store, aisle and details, in one line; empty when it has none. */
    val shownDetail: String get() = listOf(storeName, aisleName, itemDetails).filter { text -> text.isNotEmpty() }.joinToString(" · ")
}

/** A list as the model read it. */
@Serializable
data class ShoppingImportPreview(val shoppingItems: List<ShoppingImportItem> = emptyList())

/** An item as it was added, and whether it went into one the list had already. */
@Serializable
data class ShoppingItemAdded(val isMerged: Boolean = false, val shoppingItem: ShoppingItem? = null)

// MARK: Receipts

/** Ties a line of a receipt to an item. */
@Serializable
data class ShoppingReceiptMatch(
    val id: String,
    val financeReceiptId: String = "",
    val financeReceiptLineId: String = "",
    val shoppingItemId: String = "",
    /** applied, proposed or dismissed. */
    val receiptMatchState: String = "",
    val matchedQuantity: String = "",
    val previousShoppingItemState: String = "",
    val previousNeededQuantity: String = "",
    val shoppingItemPriceId: String = "",
) {
    val isApplied: Boolean get() = receiptMatchState == "applied"
    val isProposed: Boolean get() = receiptMatchState == "proposed"
}

/** A match with the item it is to. */
@Serializable
data class ShoppingReceiptLineMatch(val shoppingReceiptMatch: ShoppingReceiptMatch? = null, val shoppingItem: ShoppingItem? = null)

/** One item line of a receipt as printed, and the matches it has to items on the lists. */
@Serializable
data class ShoppingReceiptLine(
    val financeReceiptLineId: String,
    val lineNumber: Int = 0,
    /** The line as printed. */
    val description: String = "",
    val quantity: String = "",
    val lineAmount: String = "",
    val shoppingReceiptMatches: List<ShoppingReceiptLineMatch> = emptyList(),
) {
    val id: String get() = financeReceiptLineId
}

/** What one receipt did to the person's lists. */
@Serializable
data class ShoppingReceipt(
    val financeReceiptId: String = "",
    val merchantName: String = "",
    val purchasedOn: String = "",
    val totalAmount: String = "",
    val currencyCode: String = "",
    val shoppingReceiptLines: List<ShoppingReceiptLine> = emptyList(),
    val stillNeededItems: List<ShoppingItem> = emptyList(),
)

/** The server telling a viewer that a list they reach changed. */
@Serializable
data class ShoppingEvent(val shoppingListId: String)
