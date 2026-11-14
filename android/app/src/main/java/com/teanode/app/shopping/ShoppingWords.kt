package com.teanode.app.shopping

import android.content.Context
import com.teanode.app.R
import com.teanode.app.ui.components.RelativeTime
import com.teanode.kit.shopping.ShoppingCartProgress
import com.teanode.kit.shopping.ShoppingGroup
import com.teanode.kit.shopping.ShoppingImportItem
import com.teanode.kit.shopping.ShoppingItem
import com.teanode.kit.shopping.ShoppingItemPrice
import com.teanode.kit.shopping.ShoppingItemState
import com.teanode.kit.shopping.ShoppingList
import com.teanode.kit.shopping.ShoppingTotal
import com.teanode.kit.shopping.ShoppingTrip
import java.util.Locale

/** ShoppingWords writes what the Shopping screens say that is not a fixed label: the lines under a list and under an item, and the progress at a store. */
internal class ShoppingWords(private val context: Context) {
    private fun joined(names: List<String>): String = android.icu.text.ListFormatter.getInstance(Locale.getDefault()).format(names)

    fun toBuyCount(count: Int): String = context.getString(R.string.shopping_to_buy_count, count)

    /** The line under a list in the list of lists: who it is shared with, or who shared it. */
    fun listRowSubtitle(list: ShoppingList, granteeNames: List<String>): String? {
        if (!list.isOwner) return list.shownOwner
        if (granteeNames.isEmpty()) return if (list.isShared) context.getString(R.string.shopping_shared) else null
        return context.getString(R.string.shopping_shared_with, joined(granteeNames))
    }

    /** The line under a list's title: who it is shared with, how many are to buy, and about what they cost. */
    fun listSubtitle(list: ShoppingList?, granteeNames: List<String>, toBuyCount: Int, total: ShoppingTotal?): String {
        val parts = mutableListOf<String>()
        if (list != null) {
            if (!list.isOwner) parts.add(context.getString(R.string.shopping_shared_by, list.shownOwner))
            else if (granteeNames.isNotEmpty()) parts.add(context.getString(R.string.shopping_shared_with, joined(granteeNames)))
        }
        parts.add(toBuyCount(toBuyCount))
        if (total != null) parts.add(context.getString(R.string.shopping_about_total, total.shown()))
        return parts.joinToString(" · ")
    }

    /** The line under an item on a list: how many it needs and its details; or that it is in the cart or out of stock; or when it was last bought. */
    fun listItemDetail(item: ShoppingItem): String {
        val parts = mutableListOf<String>()
        when (item.state) {
            ShoppingItemState.Needed -> parts.add(context.getString(R.string.shopping_need_quantity, item.shownQuantityWithUnit))
            ShoppingItemState.InCart -> parts.add(
                if (item.cartedByName.isEmpty()) context.getString(R.string.shopping_in_the_cart)
                else context.getString(R.string.shopping_in_the_cart_by, item.cartedByName),
            )
            ShoppingItemState.NotNeeded -> {
                val bought = item.lastBoughtInstant
                parts.add(
                    if (bought != null) context.getString(R.string.shopping_bought_when, RelativeTime.text(context, bought.toEpochMilli()))
                    else context.getString(R.string.shopping_not_needed),
                )
            }
        }
        if (item.isOutOfStock && !item.isNotNeeded) parts.add(context.getString(R.string.shopping_out_of_stock))
        if (item.itemDetails.isNotEmpty()) parts.add(item.itemDetails)
        return parts.joinToString(" · ")
    }

    /** The line under an item offered instead of adding one: the list it is on when that is another, then what the list says of it. */
    fun similarItemDetail(item: ShoppingItem, listId: String): String {
        val detail = listItemDetail(item)
        return if (item.shoppingListId == listId) detail else listOf(item.listName, detail).joinToString(" · ")
    }

    /** The line under an item on the store screen: its list when several are shown; then who put it in the cart, or who found it out of stock, or else its details. */
    fun storeItemDetail(item: ShoppingItem, isChecked: Boolean, showsListName: Boolean): String {
        val parts = mutableListOf<String>()
        if (showsListName) parts.add(item.listName)
        if (isChecked) {
            parts.add(
                if (item.cartedByName.isEmpty()) context.getString(R.string.shopping_in_the_cart)
                else context.getString(R.string.shopping_in_the_cart_by, item.cartedByName),
            )
        } else if (item.isOutOfStock) {
            parts.add(
                if (item.outOfStockByName.isEmpty()) context.getString(R.string.shopping_out_of_stock)
                else context.getString(R.string.shopping_out_of_stock_by, item.outOfStockByName),
            )
        } else if (item.itemDetails.isNotEmpty()) {
            parts.add(item.itemDetails)
        }
        return parts.joinToString(" · ")
    }

    /** "3 of 8 in the cart · about $12.40 so far". */
    fun progressLine(progress: ShoppingCartProgress): String {
        var line = context.getString(R.string.shopping_progress, progress.inCartCount, progress.totalCount)
        progress.soFar?.let { soFar -> line += " · " + context.getString(R.string.shopping_progress_so_far, soFar.shown()) }
        return line
    }

    /** "Alex is shopping here too". */
    fun otherShoppersLine(names: List<String>): String? {
        if (names.isEmpty()) return null
        return context.getString(if (names.size == 1) R.string.shopping_one_shopper_here else R.string.shopping_many_shoppers_here, joined(names))
    }

    /** The line under a list while others shop it: "Sam is shopping this list at Example Market". */
    fun listShoppersLine(trips: List<ShoppingTrip>): String? {
        val first = trips.firstOrNull() ?: return null
        val names = joined(trips.map { trip -> trip.shownShopper })
        if (first.storeName.isEmpty()) {
            return context.getString(if (trips.size == 1) R.string.shopping_one_shopper_list else R.string.shopping_many_shoppers_list, names)
        }
        return context.getString(
            if (trips.size == 1) R.string.shopping_one_shopper_list_at else R.string.shopping_many_shoppers_list_at, names, first.storeName,
        )
    }

    /** An aisle's name, or the words for none. */
    fun aisleTitle(group: ShoppingGroup): String = if (group.isUnnamed) context.getString(R.string.shopping_no_aisle) else group.title

    /** A store's name, or the words for any store. */
    fun storeTitle(group: ShoppingGroup): String = if (group.isUnnamed) context.getString(R.string.shopping_any_store) else group.title

    /** Where an item is, for a tile that has no heading above it: its store (or any store) and its aisle. */
    fun listItemPlace(item: ShoppingItem): String {
        val storeName = if (item.isForAnyStore) context.getString(R.string.shopping_any_store) else item.storeName
        return if (item.aisleName.isEmpty()) storeName else "$storeName · ${item.aisleName}"
    }

    /** Whether an item an import found is new or already on the list. */
    fun importMark(item: ShoppingImportItem): String = context.getString(if (item.isOnList) R.string.shopping_on_the_list else R.string.shopping_new)

    /** The words under a price in the list of an item's prices: where it was seen and who said. */
    fun priceDetail(price: ShoppingItemPrice): String {
        val parts = mutableListOf<String>()
        if (price.storeName.isNotEmpty()) parts.add(price.storeName)
        parts.add(context.getString(if (price.isFromReceipt) R.string.shopping_from_a_receipt else R.string.shopping_typed_in))
        return parts.joinToString(" · ")
    }
}
