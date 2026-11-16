package com.teanode.kit.shopping

import com.teanode.kit.finance.FinanceMoney
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.math.BigDecimal
import java.text.Normalizer
import java.util.Locale

// How the screens read the numbers of a list, find a store again by its name, and lay items out. Nothing here talks to
// the server.

/** ShoppingQuantity reads and writes the counts the server keeps: numbers above zero, up to eight places, as text. */
object ShoppingQuantity {
    /** The number a count stands for; null for text that is not one. */
    fun decimal(text: String): BigDecimal? {
        val trimmed = text.trim()
        if (trimmed.isEmpty()) return null
        return trimmed.toBigDecimalOrNull()
    }

    /** A count as a person reads it: "2" for 2.00000000, "1.5" for 1.50000000. */
    fun shown(text: String): String {
        var shown = text.trim()
        if (!shown.contains('.')) return shown.ifEmpty { "1" }
        shown = shown.trimEnd('0')
        if (shown.endsWith(".")) shown = shown.dropLast(1)
        return shown.ifEmpty { "0" }
    }

    /** A count as the server takes it, from a stepper's number: up to three places, no trailing zeros. */
    fun text(value: Double): String = shown(String.format(Locale.ROOT, "%.3f", value))

    /** The count as a number, 1 for one that cannot be read. */
    fun value(text: String): Double = decimal(text)?.toDouble() ?: 1.0
}

/** What some items cost, in one currency. */
data class ShoppingTotal(
    val amount: BigDecimal,
    val currencyCode: String,
    /** How many of the items had a price. */
    val pricedCount: Int,
) {
    /** The amount in its currency, as "$12.40". */
    fun shown(locale: Locale = Locale.getDefault()): String = FinanceMoney.shown(amount, currencyCode.ifEmpty { "USD" }, locale)
}

/** ShoppingMoney adds up what items cost. */
object ShoppingMoney {
    /** The price of one of an item, null when it has none. */
    fun unitPrice(item: ShoppingItem): BigDecimal? {
        val price = ShoppingQuantity.decimal(item.unitPriceAmount) ?: return null
        return if (price.signum() > 0) price else null
    }

    /**
     * What the items cost in all, from their unit prices and counts. Items in the currency most of them are priced in
     * are added; a price in another currency is left out rather than added to it. Null when no item has a price.
     */
    fun estimate(items: List<ShoppingItem>): ShoppingTotal? {
        val priced = items.filter { item -> unitPrice(item) != null }
        if (priced.isEmpty()) return null
        val countsByCurrency = mutableMapOf<String, Int>()
        for (item in priced) countsByCurrency[item.currencyCode] = (countsByCurrency[item.currencyCode] ?: 0) + 1
        val currencyCode = countsByCurrency.entries.sortedWith { first, second ->
            if (first.value == second.value) first.key.compareTo(second.key) else second.value.compareTo(first.value)
        }.firstOrNull()?.key ?: ""
        var amount = BigDecimal.ZERO
        var pricedCount = 0
        for (item in priced.filter { each -> each.currencyCode == currencyCode }) {
            val price = unitPrice(item) ?: continue
            amount += price * (ShoppingQuantity.decimal(item.neededQuantity) ?: BigDecimal.ONE)
            pricedCount += 1
        }
        return ShoppingTotal(amount, currencyCode, pricedCount)
    }

    /** A price in its currency, or an empty text when there is none. */
    fun shown(amountText: String, currencyCode: String, locale: Locale = Locale.getDefault()): String {
        val amount = ShoppingQuantity.decimal(amountText) ?: return ""
        if (amount.signum() <= 0) return ""
        return FinanceMoney.shown(amount, currencyCode.ifEmpty { "USD" }, locale)
    }

    /** What the count of an item costs, from its unit price; empty when it has no price. */
    fun shownLineTotal(item: ShoppingItem, locale: Locale = Locale.getDefault()): String {
        val price = unitPrice(item) ?: return ""
        val total = price * (ShoppingQuantity.decimal(item.neededQuantity) ?: BigDecimal.ONE)
        return FinanceMoney.shown(total, item.currencyCode.ifEmpty { "USD" }, locale)
    }

    /** A price as it is typed in a field: at least two places and at most four, "1.2900" as "1.29". Empty when there is no price. */
    fun typedAmount(amountText: String): String {
        val amount = ShoppingQuantity.decimal(amountText) ?: return ""
        if (amount.signum() <= 0) return ""
        var text = ShoppingQuantity.shown(amountText)
        if (!text.contains('.')) text += ".00"
        val dot = text.indexOf('.')
        if (dot >= 0 && text.length - dot == 2) text += "0"
        return text
    }

    /** Whether typed text is a price the server takes: a number above zero with up to four places. */
    fun isPrice(text: String): Boolean {
        if (!FinanceMoney.isDecimal(text)) return false
        val amount = ShoppingQuantity.decimal(text) ?: return false
        return amount.signum() > 0
    }
}

/**
 * How stores are compared: a chain's name without case, spaces or punctuation, so any branch matches. The server
 * computes the same key for a trip's `storeKey`. It keeps letters and decimal digits one Unicode scalar at a time, as
 * the server does per rune, so a name typed with a combining accent or a voiced kana mark gets the server's key.
 */
object ShoppingStoreKey {
    fun key(storeName: String): String {
        val kept = StringBuilder()
        var index = 0
        val lowered = storeName.lowercase(Locale.ROOT)
        while (index < lowered.length) {
            val codePoint = lowered.codePointAt(index)
            index += Character.charCount(codePoint)
            when (Character.getType(codePoint).toByte()) {
                Character.UPPERCASE_LETTER, Character.LOWERCASE_LETTER, Character.TITLECASE_LETTER, Character.MODIFIER_LETTER,
                Character.OTHER_LETTER, Character.DECIMAL_DIGIT_NUMBER,
                -> kept.appendCodePoint(codePoint)
                else -> Unit
            }
        }
        return kept.toString()
    }
}

/**
 * The stores offered under a store field as it is typed in, so a store the lists already have is chosen rather than
 * typed again.
 */
object ShoppingStoreSuggestions {
    /**
     * The names among [storeNames] that match what is typed, in that order: those that begin with it, then those with
     * a word that does, then those that hold it anywhere, ignoring case, accents, spaces and punctuation. Nothing
     * typed matches every name in the order given. A name that is already what is typed is not offered.
     */
    fun names(typed: String, storeNames: List<String>, limit: Int = 5): List<String> {
        val typedKey = ShoppingStoreKey.key(typed)
        val typedFolded = folded(typed.trim())
        val ranked = mutableListOf<Pair<Int, String>>()
        for (name in storeNames) {
            val nameKey = ShoppingStoreKey.key(name)
            if (nameKey.isEmpty() || (typedKey.isNotEmpty() && nameKey == typedKey)) continue
            if (typedKey.isEmpty()) {
                ranked.add(0 to name)
                continue
            }
            val nameFolded = folded(name)
            val nameWords = nameFolded.split(Regex("""[^\p{L}\p{N}]+""")).filter { word -> word.isNotEmpty() }
            if (nameFolded.startsWith(typedFolded) || folded(nameKey).startsWith(folded(typedKey))) {
                ranked.add(0 to name)
            } else if (nameWords.any { word -> word.startsWith(typedFolded) }) {
                ranked.add(1 to name)
            } else if (folded(nameKey).contains(folded(typedKey))) {
                ranked.add(2 to name)
            }
        }
        // Stable: within a rank, the order given.
        return ranked.sortedBy { entry -> entry.first }.map { entry -> entry.second }.take(maxOf(limit, 0))
    }

    private fun folded(text: String): String =
        Normalizer.normalize(text, Normalizer.Form.NFKD).replace(Regex("""\p{M}+"""), "").lowercase(Locale.ROOT)
}

/** An item's prices read as history. */
object ShoppingPriceHistory {
    /**
     * The price seen before a given one, newest first as the server lists them: what an item cost until a receipt
     * changed it. Null when there is no earlier price.
     */
    fun previous(priceId: String, prices: List<ShoppingItemPrice>): ShoppingItemPrice? {
        val index = prices.indexOfFirst { price -> price.id == priceId }
        if (index < 0) return null
        return prices.drop(index + 1).firstOrNull()
    }

    /** The prices oldest first, those with a price and a day, for a chart. */
    fun chartable(prices: List<ShoppingItemPrice>): List<ShoppingItemPrice> =
        prices.filter { price -> price.amount != null && price.observedInstant != null }.sortedBy { price -> price.observedInstant }
}

/** One item to add to a list. */
data class ShoppingItemEntry(
    val itemDescription: String,
    val itemDetails: String = "",
    val storeName: String = "",
    val isStoreBrand: Boolean = false,
    val aisleName: String = "",
    val neededQuantity: String = "1",
    val quantityUnit: String = "",
) {
    /** The input as the server takes it; what is empty is left out. */
    val variables: JsonElement
        get() {
            val fields = mutableMapOf<String, JsonElement>("itemDescription" to JsonPrimitive(itemDescription))
            if (itemDetails.isNotEmpty()) fields["itemDetails"] = JsonPrimitive(itemDetails)
            if (storeName.isNotEmpty()) fields["storeName"] = JsonPrimitive(storeName)
            if (isStoreBrand) fields["isStoreBrand"] = JsonPrimitive(true)
            if (aisleName.isNotEmpty()) fields["aisleName"] = JsonPrimitive(aisleName)
            if (neededQuantity.isNotEmpty()) fields["neededQuantity"] = JsonPrimitive(neededQuantity)
            if (quantityUnit.isNotEmpty()) fields["quantityUnit"] = JsonPrimitive(quantityUnit)
            return JsonObject(fields)
        }
}

/** One item of an import preview that the person may leave out before adding. */
data class ShoppingImportDraft(val id: Int, val item: ShoppingImportItem, val isIncluded: Boolean) {
    /** The entry that adds the item. */
    val entry: ShoppingItemEntry
        get() = ShoppingItemEntry(
            itemDescription = item.itemDescription, itemDetails = item.itemDetails, storeName = item.storeName,
            isStoreBrand = item.isStoreBrand, aisleName = item.aisleName,
            neededQuantity = item.neededQuantity.ifEmpty { "1" }, quantityUnit = item.quantityUnit,
        )

    companion object {
        /** The drafts of a preview, in order, all included. */
        fun drafts(preview: ShoppingImportPreview): List<ShoppingImportDraft> =
            preview.shoppingItems.mapIndexed { index, item -> ShoppingImportDraft(index, item, true) }

        /** The entries of the drafts still included. */
        fun entries(drafts: List<ShoppingImportDraft>): List<ShoppingItemEntry> = drafts.filter { draft -> draft.isIncluded }.map { draft -> draft.entry }
    }
}

/** A line of a receipt with one match and its item. */
data class ShoppingReceiptEntry(val line: ShoppingReceiptLine, val match: ShoppingReceiptMatch, val item: ShoppingItem) {
    val id: String get() = match.id
}

/** What a receipt checked off on one list. */
data class ShoppingReceiptListGroup(val shoppingListId: String, val listName: String, val entries: List<ShoppingReceiptEntry>) {
    val id: String get() = shoppingListId
}

/**
 * A receipt's matches sorted into what the sheet shows: what was checked off, what is proposed, what was bought and is
 * not on a list, and what is still to buy.
 */
class ShoppingReceiptSections(receipt: ShoppingReceipt) {
    /** Matches applied, by list. */
    val appliedGroups: List<ShoppingReceiptListGroup>

    /** Matches the model was not sure of, waiting for the person. */
    val proposed: List<ShoppingReceiptEntry>

    /** Lines with no match applied or proposed. */
    val notOnList: List<ShoppingReceiptLine>

    /** Items on the lists the receipt touched that are still needed. */
    val stillNeeded: List<ShoppingItem> = receipt.stillNeededItems

    init {
        val groups = mutableListOf<ShoppingReceiptListGroup>()
        val proposedEntries = mutableListOf<ShoppingReceiptEntry>()
        val lines = mutableListOf<ShoppingReceiptLine>()
        for (line in receipt.shoppingReceiptLines) {
            var hasMatch = false
            for (lineMatch in line.shoppingReceiptMatches) {
                val match = lineMatch.shoppingReceiptMatch ?: continue
                val item = lineMatch.shoppingItem ?: continue
                val entry = ShoppingReceiptEntry(line, match, item)
                if (match.isApplied) {
                    hasMatch = true
                    val index = groups.indexOfFirst { group -> group.shoppingListId == item.shoppingListId }
                    if (index >= 0) {
                        groups[index] = ShoppingReceiptListGroup(item.shoppingListId, item.listName, groups[index].entries + entry)
                    } else {
                        groups.add(ShoppingReceiptListGroup(item.shoppingListId, item.listName, listOf(entry)))
                    }
                } else if (match.isProposed) {
                    hasMatch = true
                    proposedEntries.add(entry)
                }
            }
            if (!hasMatch) lines.add(line)
        }
        appliedGroups = groups
        proposed = proposedEntries
        notOnList = lines
    }

    /** How many items the receipt checked off. */
    val appliedCount: Int get() = appliedGroups.sumOf { group -> group.entries.size }

    /** Whether there is anything to undo. */
    val canUndo: Boolean get() = appliedCount > 0

    /** Whether the receipt did nothing to the lists and has nothing to ask. */
    val isEmpty: Boolean get() = appliedGroups.isEmpty() && proposed.isEmpty() && stillNeeded.isEmpty()
}

/** Items shown under one heading: a store or an aisle. */
data class ShoppingGroup(
    val id: String,
    /** The store's or aisle's name; empty for the items that have none. */
    val title: String,
    val items: List<ShoppingItem>,
) {
    val isUnnamed: Boolean get() = title.isEmpty()
}

/** The store screen's sections: the aisles of the store's own items, then the items for any store. */
data class ShoppingStoreSections(val aisleGroups: List<ShoppingGroup>, val anyStoreItems: List<ShoppingItem>)

/** How far along the shopping at a store is. */
data class ShoppingCartProgress(val inCartCount: Int, val totalCount: Int, val soFar: ShoppingTotal?) {
    companion object {
        /** Items still to find count, those found out of stock do not. */
        fun of(items: List<ShoppingItem>): ShoppingCartProgress {
            val counted = items.filter { item -> !item.isNotNeeded && (item.isInCart || !item.isOutOfStock) }
            val carted = counted.filter { item -> item.isInCart }
            return ShoppingCartProgress(carted.size, counted.size, ShoppingMoney.estimate(carted))
        }
    }
}

/** Lays items out by store, by aisle and for the store screen. */
object ShoppingGrouping {
    private val titleCollator: java.text.Collator
        get() = java.text.Collator.getInstance().apply { strength = java.text.Collator.SECONDARY }

    private fun compareTitles(first: String, second: String): Int = titleCollator.compare(first, second)

    private val itemOrder = Comparator<ShoppingItem> { first, second ->
        if (first.itemRank != second.itemRank) first.itemRank.compareTo(second.itemRank)
        else compareTitles(first.itemDescription, second.itemDescription)
    }

    /** The items by store, the stores in alphabetical order and the items for any store last. */
    fun byStore(items: List<ShoppingItem>): List<ShoppingGroup> {
        val membersByKey = LinkedHashMap<String, MutableList<ShoppingItem>>()
        for (item in items) membersByKey.getOrPut(item.storeKey) { mutableListOf() }.add(item)
        // A store is named as the first item for it names it.
        val groups = membersByKey.map { (key, seen) ->
            ShoppingGroup("store:$key", seen.firstOrNull()?.storeName ?: "", seen.sortedWith(itemOrder))
        }
        val named = groups.filter { group -> !group.isUnnamed }.sortedWith { first, second -> compareTitles(first.title, second.title) }
        return named + groups.filter { group -> group.isUnnamed }
    }

    /** The items by aisle, the aisles in the order they were given and the items with none last. */
    fun byAisle(items: List<ShoppingItem>): List<ShoppingGroup> {
        val membersByKey = LinkedHashMap<String, MutableList<ShoppingItem>>()
        for (item in items) membersByKey.getOrPut(item.aisleName.trim().lowercase(Locale.ROOT)) { mutableListOf() }.add(item)
        // An aisle is named as the first item in it names it.
        val groups = membersByKey.map { (key, seen) ->
            ShoppingGroup("aisle:$key", seen.firstOrNull()?.aisleName?.trim() ?: "", seen.sortedWith(itemOrder))
        }
        fun rank(group: ShoppingGroup): Int = group.items.minOfOrNull { item -> item.aisleRank } ?: 0
        val named = groups.filter { group -> !group.isUnnamed }.sortedWith { first, second ->
            if (rank(first) == rank(second)) compareTitles(first.title, second.title) else rank(first).compareTo(rank(second))
        }
        return named + groups.filter { group -> group.isUnnamed }
    }

    /** The store screen's sections: the store's own items by aisle, then the items for any store in a section of their own. */
    fun forStore(items: List<ShoppingItem>): ShoppingStoreSections {
        val own = items.filter { item -> !item.isForAnyStore }
        val anyStore = items.filter { item -> item.isForAnyStore }
        return ShoppingStoreSections(byAisle(own), byAisle(anyStore).flatMap { group -> group.items })
    }

    /** The items by whether they are still wanted: those to buy (needed or in the cart) and those kept on the list as not needed. */
    fun split(items: List<ShoppingItem>): Pair<List<ShoppingItem>, List<ShoppingItem>> =
        items.filter { item -> !item.isNotNeeded } to items.filter { item -> item.isNotNeeded }.sortedWith(itemOrder)
}

