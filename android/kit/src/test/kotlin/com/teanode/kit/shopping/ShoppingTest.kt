package com.teanode.kit.shopping

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.HTTPRequest
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.BeforeEach
import org.junit.jupiter.api.Test
import java.math.BigDecimal
import java.util.Locale

/**
 * What each of the server's views decodes to, how items are laid out and added up, what each call sends, and that the
 * documents name the server's operations.
 */
class ShoppingTest {
    private val server = ServerAddress.parse("mail.example.com")

    @BeforeEach
    fun useEnglishMoney() {
        Locale.setDefault(Locale.US)
    }

    private fun connection(answering: String): Pair<Connection, FakeServer> = connectionOf(FakeServer { _ -> 200 to answering })

    private fun connectionOf(fake: FakeServer): Pair<Connection, FakeServer> {
        val account = Account(server, "a-client", "tnt_a", "", Long.MAX_VALUE / 4, "robin")
        return Connection(account, fake) to fake
    }

    private fun sentVariables(request: HTTPRequest): JsonObject =
        TeaNodeJson.parseToJsonElement(request.body!!.toString(Charsets.UTF_8)).jsonObject["variables"]!!.jsonObject

    private fun sentQuery(request: HTTPRequest): String =
        TeaNodeJson.parseToJsonElement(request.body!!.toString(Charsets.UTF_8)).jsonObject["query"]!!.jsonPrimitive.content

    private fun text(value: String) = JsonPrimitive(value)

    private fun <Value> decode(serializer: KSerializer<Value>, json: String): Value = ShoppingJson.decodeFromString(serializer, json)

    /** An item as the server writes it, with the fields a test cares about. */
    private fun itemJson(
        id: String,
        description: String,
        listId: String = "list-1",
        listName: String = "Groceries",
        store: String = "",
        aisle: String = "",
        aisleRank: Int = 0,
        itemRank: Int = 0,
        quantity: String = "1",
        unit: String = "",
        price: String = "",
        state: String = "needed",
        outOfStockAt: String? = null,
    ): String {
        val key = store.lowercase().filter { character -> character.isLetterOrDigit() }
        val outOfStock = if (outOfStockAt == null) "null" else "\"$outOfStockAt\""
        val currency = if (price.isEmpty()) "" else "USD"
        return """
            {"id":"$id","shoppingListId":"$listId","listName":"$listName","itemDescription":"$description","itemDetails":"",
             "storeName":"$store","storeKey":"$key","isStoreBrand":false,"aisleName":"$aisle","aisleRank":$aisleRank,"itemRank":$itemRank,
             "neededQuantity":"$quantity","quantityUnit":"$unit","unitPriceAmount":"$price","currencyCode":"$currency",
             "shoppingItemState":"$state","cartedByUserId":"","cartedByName":"","cartedAt":null,"outOfStockAt":$outOfStock,
             "outOfStockByUserId":"","outOfStockByName":"","outOfStockStoreName":"","lastBoughtAt":null,"pictureFileId":"","picturePageUrl":"",
             "picturePath":"","pictureSource":"","pictureSearchedAt":null}
        """.trimIndent()
    }

    private fun item(
        id: String,
        description: String,
        listId: String = "list-1",
        store: String = "",
        aisle: String = "",
        aisleRank: Int = 0,
        itemRank: Int = 0,
        quantity: String = "1",
        unit: String = "",
        price: String = "",
        state: String = "needed",
        outOfStockAt: String? = null,
    ): ShoppingItem = decode(
        ShoppingItem.serializer(),
        itemJson(id, description, listId, "Groceries", store, aisle, aisleRank, itemRank, quantity, unit, price, state, outOfStockAt),
    )

    // MARK: The documents

    @Test
    fun theDocumentsNameTheServersOperations() {
        val operations = mapOf(
            "shoppingLists" to "ListShoppingLists", "shoppingList" to "GetShoppingList", "shoppingItem" to "GetShoppingItem",
            "shoppingListShares" to "ListShoppingListShares", "shoppingStores" to "ListShoppingStores",
            "shoppingStoreItems" to "ListShoppingStoreItems", "openShoppingTrips" to "ListOpenShoppingTrips",
            "shoppingItemPrices" to "ListShoppingItemPrices", "similarShoppingItems" to "ListSimilarShoppingItems",
            "shoppingReceiptMatches" to "ListShoppingReceiptMatches", "previewShoppingImport" to "PreviewShoppingImport",
            "createShoppingList" to "CreateShoppingList", "renameShoppingList" to "RenameShoppingList",
            "deleteShoppingList" to "DeleteShoppingList", "shareShoppingList" to "ShareShoppingList",
            "unshareShoppingList" to "UnshareShoppingList", "setShoppingListReminder" to "SetShoppingListReminder",
            "setShoppingStoreReminder" to "SetShoppingStoreReminder", "addShoppingItems" to "AddShoppingItems",
            "updateShoppingItem" to "UpdateShoppingItem", "removeShoppingItems" to "RemoveShoppingItems",
            "checkShoppingItem" to "CheckShoppingItem", "setShoppingItemNeed" to "SetShoppingItemNeed",
            "setShoppingItemOutOfStock" to "SetShoppingItemOutOfStock", "recordShoppingItemPrice" to "RecordShoppingItemPrice",
            "findShoppingItemPicture" to "FindShoppingItemPicture", "setShoppingItemPicture" to "SetShoppingItemPicture",
            "startShopping" to "StartShopping", "finishShopping" to "FinishShopping", "matchShoppingReceipt" to "MatchShoppingReceipt",
            "decideShoppingReceiptMatch" to "DecideShoppingReceiptMatch", "undoShoppingReceipt" to "UndoShoppingReceipt",
            "addShoppingItemFromReceiptLine" to "AddShoppingItemFromReceiptLine", "shoppingEvents" to "ShoppingEvents",
        )
        assertEquals(operations.keys, ShoppingDocuments.all.keys)
        for ((name, operation) in operations) {
            assertTrue(ShoppingDocuments.all[name]!!.contains(operation), "$name does not call $operation")
        }
        assertTrue(ShoppingDocuments.ADD_SHOPPING_ITEMS.contains("[ShoppingItemEntryInput!]!"))
        assertTrue(ShoppingDocuments.SHOPPING_EVENTS.startsWith("subscription"))
    }

    // MARK: Decoding

    @Test
    fun aListDecodesWithItsCounts() {
        val list = decode(
            ShoppingList.serializer(),
            """
            {"id":"list-1","userId":"user-1","listName":"Groceries","ownerName":"Robin","ownerUsername":"robin","isOwner":true,"isShared":true,
             "isReminded":true,"neededCount":5,"inCartCount":1,"notNeededCount":2,"createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T00:00:00Z"}
            """.trimIndent(),
        )
        assertEquals("list-1", list.id)
        assertEquals("Groceries", list.listName)
        assertTrue(list.isOwner)
        assertTrue(list.isShared)
        assertEquals(5, list.neededCount)
        assertEquals(1, list.inCartCount)
        assertEquals(2, list.notNeededCount)
        assertEquals("Robin", list.shownOwner)
    }

    @Test
    fun anItemDecodesWithItsStateAndPrice() {
        val apples = item("item-1", "Apples", store = "Example Market", aisle = "Produce", quantity = "6.00000000", price = "0.8900")
        assertEquals(ShoppingItemState.Needed, apples.state)
        assertTrue(apples.isNeeded)
        assertFalse(apples.isOutOfStock)
        assertEquals("examplemarket", apples.storeKey)
        assertEquals("6", apples.shownQuantity)
        assertEquals(0, BigDecimal("0.89").compareTo(apples.unitPrice))
        assertEquals("6 Apples", apples.shownTitle)
        assertFalse(apples.hasPicture)
        assertEquals("A", apples.initial)
    }

    @Test
    fun anItemInTheCartOrNotNeededOrOutOfStockSaysSo() {
        assertTrue(item("a", "Soup", state = "in_cart").isInCart)
        assertTrue(item("b", "Soup", state = "not_needed").isNotNeeded)
        assertTrue(item("c", "Soup", outOfStockAt = "2030-01-02T09:00:00Z").isOutOfStock)
        // A state the app does not know is taken for needed.
        assertEquals(ShoppingItemState.Needed, item("d", "Soup", state = "somewhere_new").state)
    }

    @Test
    fun anItemTheServerSendsFewFieldsOfStillDecodes() {
        val sparse = decode(ShoppingItem.serializer(), """{"id":"item-9","itemDescription":"Soup"}""")
        assertEquals("item-9", sparse.id)
        assertEquals("", sparse.storeName)
        assertEquals("", sparse.shoppingItemState)
        assertTrue(sparse.isNeeded)
        assertNull(sparse.cartedAt)
        // A field sent as null reads as empty too.
        assertEquals("", decode(ShoppingItem.serializer(), """{"id":"item-9","storeName":null}""").storeName)
        assertThrows(Exception::class.java) { decode(ShoppingItem.serializer(), """{"itemDescription":"Soup"}""") }
    }

    @Test
    fun aListPageDecodesItsItemsAndTrips() = runBlocking {
        val json = """
            {"data":{"GetShoppingList":{"totalCount":2,
              "shoppingList":{"id":"list-1","listName":"Groceries","isOwner":false,"ownerName":"","ownerUsername":"sam","neededCount":2},
              "shoppingItems":[${itemJson("item-1", "Apples")},${itemJson("item-2", "Soup", state = "not_needed")}],
              "shoppingTrips":[{"id":"trip-1","userId":"user-2","shopperName":"","shopperUsername":"sam","storeName":"Example Market",
                "storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:00:00Z","finishedAt":null}]}}}
        """.trimIndent()
        val (connection, fake) = connection(json)
        val page = connection.shoppingList("list-1", listOf(ShoppingItemState.Needed, ShoppingItemState.InCart))
        assertEquals(2, page.totalCount)
        assertEquals(listOf("Apples", "Soup"), page.shoppingItems.map { each -> each.itemDescription })
        assertEquals("sam", page.shoppingList?.shownOwner)
        val trip = page.shoppingTrips.first()
        assertTrue(trip.isOpen)
        assertEquals("sam", trip.shownShopper)
        val variables = sentVariables(fake.sent[0])
        assertEquals(text("list-1"), variables["shoppingListId"])
        assertEquals(JsonArray(listOf(text("needed"), text("in_cart"))), variables["shoppingItemStates"])
    }

    @Test
    fun aStoreKeyKeepsLettersOneScalarAtATime() {
        assertEquals("examplemarket12", ShoppingStoreKey.key("Example Market #12"))
        // A combining accent is a mark, not a letter, as on the server.
        assertEquals("cafecorner", ShoppingStoreKey.key("Café Corner"))
        assertEquals("cafécorner", ShoppingStoreKey.key("Café Corner"))
        // A voiced kana mark written apart is dropped; the kana stays.
        assertEquals("カ", ShoppingStoreKey.key("ガ"))
    }

    @Test
    fun aWholeListIsReadPageByPage() = runBlocking {
        val pages = listOf(listOf(itemJson("item-1", "Apples"), itemJson("item-2", "Bread")).joinToString(","), itemJson("item-3", "Soup"))
        val fake = FakeServer { request ->
            val offset = sentVariables(request)["offset"]!!.jsonPrimitive.content.toInt()
            val items = if (offset == 0) pages[0] else pages[1]
            200 to """{"data":{"GetShoppingList":{"totalCount":3,"shoppingList":{"id":"list-1","listName":"Groceries"},"shoppingItems":[$items],"shoppingTrips":[]}}}"""
        }
        val (connection, _) = connectionOf(fake)
        val page = connection.wholeShoppingList("list-1", pageSize = 2)
        assertEquals(listOf("Apples", "Bread", "Soup"), page.shoppingItems.map { each -> each.itemDescription })
        assertEquals(2, fake.sent.size)
        assertEquals(2, sentVariables(fake.sent[1])["offset"]!!.jsonPrimitive.content.toInt())
    }

    @Test
    fun theStoreViewDecodesItsListsAndWhoIsShopping() {
        val json = """
            {"storeName":"Example Market","isReminded":false,
             "shoppingItems":[${itemJson("item-1", "Apples", store = "Example Market")}],
             "shoppingLists":[{"shoppingListId":"list-1","listName":"Groceries","neededCount":7,"isIncluded":true},
                              {"shoppingListId":"list-2","listName":"Party supplies","neededCount":2,"isIncluded":false}],
             "shoppingTrips":[]}
        """.trimIndent()
        val view = decode(ShoppingStoreItems.serializer(), json)
        assertEquals("Example Market", view.storeName)
        assertFalse(view.isReminded)
        assertEquals(listOf("Groceries", "Party supplies"), view.shoppingLists.map { each -> each.listName })
        assertEquals(listOf("list-1"), view.includedListIds)
        assertEquals(1, view.shoppingItems.size)
    }

    @Test
    fun sharesNameACircleOrAPerson() {
        val json = """
            [{"id":"share-1","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"","granteeCircleId":"circle-1","granteeCircleName":"Home"},
             {"id":"share-2","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"user-2","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":""},
             {"id":"share-3","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"user-3","granteeUsername":"alex","granteeName":"","granteeCircleId":""}]
        """.trimIndent()
        val shares = decode(ListSerializer(ShoppingListShare.serializer()), json)
        assertTrue(shares[0].isWithCircle)
        assertEquals(listOf("Home", "Sam Lee", "alex"), shares.map { each -> each.shownGrantee })
        assertEquals(listOf("Home", "Sam Lee", "alex"), shares.shownGrantees)
    }

    @Test
    fun pricesDecodeAndTheEarlierOneIsFound() {
        val json = """
            [{"id":"price-3","shoppingItemId":"item-1","storeName":"Example Market","unitPriceAmount":"0.9200","currencyCode":"USD","observedOn":"2030-03-01","priceSource":"receipt","financeReceiptId":"receipt-1"},
             {"id":"price-2","shoppingItemId":"item-1","storeName":"Example Market","unitPriceAmount":"0.8900","currencyCode":"USD","observedOn":"2030-02-01","priceSource":"person","financeReceiptId":""},
             {"id":"price-1","shoppingItemId":"item-1","storeName":"Example Market","unitPriceAmount":"0.7900","currencyCode":"USD","observedOn":"2030-01-01","priceSource":"receipt","financeReceiptId":"receipt-0"}]
        """.trimIndent()
        val prices = decode(ListSerializer(ShoppingItemPrice.serializer()), json)
        assertTrue(prices[0].isFromReceipt)
        assertFalse(prices[1].isFromReceipt)
        assertEquals(0, BigDecimal("0.92").compareTo(prices[0].amount))
        assertNotNull(prices[0].observedInstant)
        assertEquals("price-2", ShoppingPriceHistory.previous("price-3", prices)?.id)
        assertNull(ShoppingPriceHistory.previous("price-1", prices))
        assertNull(ShoppingPriceHistory.previous("price-9", prices))
        assertEquals(listOf("price-1", "price-2", "price-3"), ShoppingPriceHistory.chartable(prices).map { each -> each.id })
    }

    // MARK: Numbers

    @Test
    fun countsAreShownWithoutTrailingZeros() {
        assertEquals("2", ShoppingQuantity.shown("2.00000000"))
        assertEquals("1.5", ShoppingQuantity.shown("1.50000000"))
        assertEquals("3", ShoppingQuantity.shown("3"))
        assertEquals("1", ShoppingQuantity.shown(""))
        assertEquals("2", ShoppingQuantity.text(2.0))
        assertEquals("1.5", ShoppingQuantity.text(1.5))
        assertEquals(2.5, ShoppingQuantity.value("2.50000000"))
        assertEquals(1.0, ShoppingQuantity.value("nonsense"))
    }

    @Test
    fun pricesAreTypedWithTwoPlacesAtLeast() {
        assertEquals("1.29", ShoppingMoney.typedAmount("1.2900"))
        assertEquals("2.00", ShoppingMoney.typedAmount("2"))
        assertEquals("2.50", ShoppingMoney.typedAmount("2.5"))
        assertEquals("0.845", ShoppingMoney.typedAmount("0.8450"))
        assertEquals("", ShoppingMoney.typedAmount(""))
        assertTrue(ShoppingMoney.isPrice("1.99"))
        assertFalse(ShoppingMoney.isPrice("1,99"))
        assertFalse(ShoppingMoney.isPrice("0"))
        assertFalse(ShoppingMoney.isPrice("free"))
    }

    @Test
    fun aStoreIsComparedWithoutCaseOrPunctuation() {
        assertEquals("examplemarket", ShoppingStoreKey.key("Example Market"))
        assertEquals("examplemarkets", ShoppingStoreKey.key("EXAMPLE-MARKET's"))
        assertEquals("", ShoppingStoreKey.key("  "))
    }

    @Test
    fun theEstimateAddsUnitPricesTimesCountsInTheCommonCurrency() {
        val items = listOf(item("a", "Apples", quantity = "6", price = "0.89"), item("b", "Soup", quantity = "2", price = "1.99"), item("c", "Salt"))
        val total = ShoppingMoney.estimate(items)!!
        assertEquals(0, BigDecimal("9.32").compareTo(total.amount))
        assertEquals("USD", total.currencyCode)
        assertEquals(2, total.pricedCount)
        assertTrue(total.shown(Locale.US).contains("9.32"))
        assertNull(ShoppingMoney.estimate(listOf(item("c", "Salt"))))
    }

    // MARK: Grouping

    @Test
    fun itemsGroupByStoreWithAnyStoreLast() {
        val items = listOf(
            item("a", "Soup", store = "Example Market", itemRank = 2), item("b", "Chili", store = "Corner Grocer"),
            item("c", "Towels"), item("d", "Apples", store = "example market", itemRank = 1),
        )
        val groups = ShoppingGrouping.byStore(items)
        assertEquals(listOf("Corner Grocer", "Example Market", ""), groups.map { group -> group.title })
        assertEquals(listOf("Apples", "Soup"), groups[1].items.map { each -> each.itemDescription })
        assertTrue(groups[2].isUnnamed)
    }

    @Test
    fun itemsGroupByAisleInTheOrderTheyWereGiven() {
        val items = listOf(
            item("a", "Soup", aisle = "Canned goods", aisleRank = 2), item("b", "Apples", aisle = "Produce", aisleRank = 1),
            item("c", "Towels"), item("d", "Pears", aisle = "produce", aisleRank = 1, itemRank = 1),
        )
        val groups = ShoppingGrouping.byAisle(items)
        assertEquals(listOf("Produce", "Canned goods", ""), groups.map { group -> group.title })
        assertEquals(listOf("Apples", "Pears"), groups[0].items.map { each -> each.itemDescription })
    }

    @Test
    fun theStoreScreenSeparatesTheStoresOwnItemsFromThoseForAnyStore() {
        val items = listOf(
            item("a", "Apples", store = "Example Market", aisle = "Produce", aisleRank = 1),
            item("b", "Soup", store = "Example Market", aisle = "Canned goods", aisleRank = 2), item("c", "Towels", aisle = "Household"),
        )
        val sections = ShoppingGrouping.forStore(items)
        assertEquals(listOf("Produce", "Canned goods"), sections.aisleGroups.map { group -> group.title })
        assertEquals(listOf("Towels"), sections.anyStoreItems.map { each -> each.itemDescription })
    }

    @Test
    fun boughtItemsAreSplitFromThoseToBuy() {
        val items = listOf(item("a", "Apples"), item("b", "Soup", state = "in_cart"), item("c", "Bread", state = "not_needed"))
        val (toBuy, notNeeded) = ShoppingGrouping.split(items)
        assertEquals(listOf("Apples", "Soup"), toBuy.map { each -> each.itemDescription })
        assertEquals(listOf("Bread"), notNeeded.map { each -> each.itemDescription })
    }

    @Test
    fun theCartCountsWhatIsLeftToFindAndWhatIsInIt() {
        val items = listOf(
            item("a", "Apples", quantity = "6", price = "0.89", state = "in_cart"), item("b", "Soup"),
            item("c", "Oranges", outOfStockAt = "2030-01-02T09:00:00Z"), item("d", "Bread", state = "not_needed"),
            item("e", "Salt", state = "in_cart", outOfStockAt = "2030-01-02T09:00:00Z"),
        )
        val progress = ShoppingCartProgress.of(items)
        // Apples, soup and salt count; the oranges are out of stock and the bread is bought.
        assertEquals(2, progress.inCartCount)
        assertEquals(3, progress.totalCount)
        assertEquals(0, BigDecimal("5.34").compareTo(progress.soFar?.amount))
    }

    @Test
    fun otherShoppersAreFoundByStoreAndNotCountedTwice() {
        val json = """
            [{"id":"t1","userId":"user-2","shopperName":"Sam","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:00:00Z"},
             {"id":"t2","userId":"user-2","shopperName":"Sam","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-2"],"startedAt":"2030-01-02T09:05:00Z"},
             {"id":"t3","userId":"user-1","shopperName":"Robin","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:06:00Z"},
             {"id":"t4","userId":"user-3","shopperName":"Alex","storeName":"Corner Grocer","storeKey":"cornergrocer","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:07:00Z"},
             {"id":"t5","userId":"user-4","shopperName":"Kim","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T08:00:00Z","finishedAt":"2030-01-02T08:30:00Z"}]
        """.trimIndent()
        val trips = decode(ListSerializer(ShoppingTrip.serializer()), json)
        assertEquals(listOf("Sam"), trips.otherShoppers("examplemarket", "user-1"))
        assertEquals("t3", trips.ownTrip("examplemarket", "user-1")?.id)
        assertNull(trips.ownTrip("cornergrocer", "user-1"))
        assertEquals(listOf("t2"), trips.openTrips("list-2", "user-1").map { trip -> trip.id })
    }

    // MARK: Importing

    @Test
    fun anImportPreviewMarksWhatTheListHasAndAddsWhatIsKept() = runBlocking {
        val json = """
            {"data":{"PreviewShoppingImport":{"shoppingItems":[
              {"itemDescription":"Apples","itemDetails":"","neededQuantity":"6","quantityUnit":"","aisleName":"Produce","storeName":"","isStoreBrand":false,
               "mergedShoppingItemId":"item-1","mergedShoppingItemState":"needed","mergedNeededQuantity":"4"},
              {"itemDescription":"Bread","itemDetails":"Rye","neededQuantity":"","quantityUnit":"","aisleName":"Bakery","storeName":"Corner Grocer","isStoreBrand":true,
               "mergedShoppingItemId":"","mergedShoppingItemState":"","mergedNeededQuantity":""}]}}}
        """.trimIndent()
        val (connection, fake) = connection(json)
        val preview = connection.previewShoppingImport("list-1", "  apples\nbread \n", listOf("attachment-1"))
        val variables = sentVariables(fake.sent[0])
        assertEquals(text("list-1"), variables["shoppingListId"])
        assertEquals(text("apples\nbread"), variables["importText"])
        assertEquals(JsonArray(listOf(text("attachment-1"))), variables["agentAttachmentIds"])
        assertEquals(180_000L, fake.sent[0].timeoutMS)
        var drafts = ShoppingImportDraft.drafts(preview)
        assertEquals(listOf(true, false), drafts.map { draft -> draft.item.isOnList })
        assertEquals("6 Apples", drafts[0].item.shownTitle)
        assertEquals("Corner Grocer · Bakery · Rye", drafts[1].item.shownDetail)
        drafts = listOf(drafts[0].copy(isIncluded = false), drafts[1])
        val entries = ShoppingImportDraft.entries(drafts)
        assertEquals(1, entries.size)
        // An empty count is one, and what is empty is left out of the input.
        assertEquals(
            JsonObject(
                mapOf(
                    "itemDescription" to text("Bread"), "itemDetails" to text("Rye"), "storeName" to text("Corner Grocer"),
                    "isStoreBrand" to JsonPrimitive(true), "aisleName" to text("Bakery"), "neededQuantity" to text("1"),
                ),
            ),
            entries[0].variables,
        )
    }

    // MARK: Receipts

    @Test
    fun aReceiptIsSortedIntoWhatItCheckedOffProposedAndLeftOver() = runBlocking {
        val checked = itemJson("item-1", "Frozen chili", store = "Corner Grocer")
        val proposed = itemJson("item-2", "Soup", listId = "list-2", listName = "Party supplies")
        val still = itemJson("item-3", "Paper towels")
        val json = """
            {"data":{"ListShoppingReceiptMatches":{"financeReceiptId":"receipt-1","merchantName":"Corner Grocer","purchasedOn":"2030-01-02","totalAmount":"14.53","currencyCode":"USD",
              "shoppingReceiptLines":[
                {"financeReceiptLineId":"line-1","lineNumber":1,"description":"FRZN CHILI","quantity":"1","lineAmount":"4.50","shoppingReceiptMatches":[
                  {"shoppingReceiptMatch":{"id":"match-1","financeReceiptId":"receipt-1","financeReceiptLineId":"line-1","shoppingItemId":"item-1","receiptMatchState":"applied","shoppingItemPriceId":"price-1"},"shoppingItem":$checked}]},
                {"financeReceiptLineId":"line-2","lineNumber":2,"description":"SOUP","quantity":"2","lineAmount":"3.98","shoppingReceiptMatches":[
                  {"shoppingReceiptMatch":{"id":"match-2","financeReceiptId":"receipt-1","financeReceiptLineId":"line-2","shoppingItemId":"item-2","receiptMatchState":"proposed"},"shoppingItem":$proposed}]},
                {"financeReceiptLineId":"line-3","lineNumber":3,"description":"DISH SOAP","quantity":"1","lineAmount":"3.25","shoppingReceiptMatches":[
                  {"shoppingReceiptMatch":{"id":"match-3","financeReceiptId":"receipt-1","financeReceiptLineId":"line-3","shoppingItemId":"item-2","receiptMatchState":"dismissed"},"shoppingItem":$proposed}]},
                {"financeReceiptLineId":"line-4","lineNumber":4,"description":"BREAD","quantity":"1","lineAmount":"2.80","shoppingReceiptMatches":[]}],
              "stillNeededItems":[$still]}}}
        """.trimIndent()
        val (connection, fake) = connection(json)
        val receipt = connection.shoppingReceipt("receipt-1")
        assertEquals(text("receipt-1"), sentVariables(fake.sent[0])["financeReceiptId"])
        assertEquals("Corner Grocer", receipt.merchantName)
        val sections = ShoppingReceiptSections(receipt)
        assertEquals(1, sections.appliedCount)
        assertEquals(listOf("Groceries"), sections.appliedGroups.map { group -> group.listName })
        assertEquals("FRZN CHILI", sections.appliedGroups[0].entries[0].line.description)
        assertEquals("price-1", sections.appliedGroups[0].entries[0].match.shoppingItemPriceId)
        assertEquals(listOf("match-2"), sections.proposed.map { entry -> entry.match.id })
        // A dismissed match leaves its line on no list, like a line with no match.
        assertEquals(listOf("DISH SOAP", "BREAD"), sections.notOnList.map { line -> line.description })
        assertEquals(listOf("Paper towels"), sections.stillNeeded.map { each -> each.itemDescription })
        assertTrue(sections.canUndo)
        assertFalse(sections.isEmpty)
    }

    @Test
    fun decidingAMatchSendsTheAnswer() = runBlocking {
        val (connection, fake) = connection("""{"data":{"DecideShoppingReceiptMatch":{"id":"match-2","receiptMatchState":"applied"}}}""")
        val match = connection.decideShoppingReceiptMatch("match-2", true)
        assertTrue(match.isApplied)
        val variables = sentVariables(fake.sent[0])
        assertEquals(text("match-2"), variables["shoppingReceiptMatchId"])
        assertEquals(JsonPrimitive(true), variables["isApplied"])
    }

    // MARK: What each call sends

    @Test
    fun changingAnItemSendsOnlyWhatIsGiven() = runBlocking {
        val (connection, fake) = connection("""{"data":{"UpdateShoppingItem":${itemJson("item-1", "Pears")}}}""")
        val changed = connection.updateShoppingItem("item-1", itemDescription = "Pears", isStoreBrand = false, neededQuantity = "3")
        assertEquals("Pears", changed.itemDescription)
        val variables = sentVariables(fake.sent[0])
        assertEquals(text("item-1"), variables["shoppingItemId"])
        assertEquals(text("Pears"), variables["itemDescription"])
        assertEquals(text("3"), variables["neededQuantity"])
        assertEquals(JsonPrimitive(false), variables["isStoreBrand"])
        assertNull(variables["storeName"])
        assertNull(variables["aisleName"])
    }

    @Test
    fun checkingNeedingAndMarkingOutOfStockSendTheirArguments() = runBlocking {
        val answer = itemJson("item-1", "Pears")
        val (checking, checkFake) = connection("""{"data":{"CheckShoppingItem":$answer}}""")
        checking.checkShoppingItem("item-1", true)
        assertEquals(JsonPrimitive(true), sentVariables(checkFake.sent[0])["isChecked"])
        assertNull(sentVariables(checkFake.sent[0])["storeName"])
        // Checking at a store names it, for the trip the first check starts.
        val (atStore, atStoreFake) = connection("""{"data":{"CheckShoppingItem":$answer}}""")
        atStore.checkShoppingItem("item-1", true, "Example Market")
        assertEquals(text("Example Market"), sentVariables(atStoreFake.sent[0])["storeName"])
        val (needing, needFake) = connection("""{"data":{"SetShoppingItemNeed":$answer}}""")
        needing.setShoppingItemNeed("item-1", true, "2")
        val needed = sentVariables(needFake.sent[0])
        assertEquals(JsonPrimitive(true), needed["isNeeded"])
        assertEquals(text("2"), needed["neededQuantity"])
        val (stocking, stockFake) = connection("""{"data":{"SetShoppingItemOutOfStock":$answer}}""")
        stocking.setShoppingItemOutOfStock("item-1", true, "Example Market")
        val stocked = sentVariables(stockFake.sent[0])
        assertEquals(JsonPrimitive(true), stocked["isOutOfStock"])
        assertEquals(text("Example Market"), stocked["storeName"])
    }

    @Test
    fun startingAndFinishingShoppingSendTheStoreAndTheLists() = runBlocking {
        val trip = """{"id":"trip-1","userId":"user-1","shopperName":"Robin","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1","list-2"],"startedAt":"2030-01-02T09:00:00Z"}"""
        val (starting, startFake) = connection("""{"data":{"StartShopping":$trip}}""")
        val started = starting.startShopping("Example Market", listOf("list-1", "list-2"))
        assertEquals("trip-1", started.id)
        val variables = sentVariables(startFake.sent[0])
        assertEquals(text("Example Market"), variables["storeName"])
        assertEquals(JsonArray(listOf(text("list-1"), text("list-2"))), variables["shoppingListIds"])
        // No store in particular leaves the name out.
        val (anywhere, anywhereFake) = connection("""{"data":{"StartShopping":$trip}}""")
        anywhere.startShopping("", listOf("list-1"))
        assertNull(sentVariables(anywhereFake.sent[0])["storeName"])
        val (finishing, finishFake) = connection("""{"data":{"FinishShopping":$trip}}""")
        finishing.finishShopping("trip-1")
        assertEquals(text("trip-1"), sentVariables(finishFake.sent[0])["shoppingTripId"])
        // Done with no trip named buys the cart all the same.
        val (done, doneFake) = connection("""{"data":{"FinishShopping":$trip}}""")
        done.finishShopping()
        assertNull(sentVariables(doneFake.sent[0])["shoppingTripId"])
    }

    @Test
    fun theStoreScreenAsksForTheListsIncludedAndAnyStore() = runBlocking {
        val (connection, fake) = connection(
            """{"data":{"ListShoppingStoreItems":{"storeName":"Example Market","isReminded":true,"shoppingItems":[],"shoppingLists":[],"shoppingTrips":[]}}}""",
        )
        connection.shoppingStoreItems("Example Market", listOf("list-1"))
        val variables = sentVariables(fake.sent[0])
        assertEquals(text("Example Market"), variables["storeName"])
        assertEquals(JsonArray(listOf(text("list-1"))), variables["shoppingListIds"])
        assertEquals(JsonPrimitive(true), variables["isAnyStoreIncluded"])
        assertTrue(sentQuery(fake.sent[0]).contains("ListShoppingStoreItems"))
    }

    @Test
    fun sharingAListNamesAPersonOrACircle() = runBlocking {
        val share = """{"id":"share-1","shoppingListId":"list-1","listName":"Groceries","granteeCircleId":"circle-1","granteeCircleName":"Home"}"""
        val (connection, fake) = connection("""{"data":{"ShareShoppingList":$share}}""")
        val made = connection.shareShoppingList("list-1", null, "circle-1")
        assertEquals("Home", made.shownGrantee)
        val variables = sentVariables(fake.sent[0])
        assertEquals(JsonNull, variables["granteeUserId"])
        assertEquals(text("circle-1"), variables["granteeCircleId"])
    }

    @Test
    fun addingItemsSendsEachEntryAndReadsWhetherItMerged() = runBlocking {
        val answer = """{"data":{"AddShoppingItems":[{"isMerged":true,"shoppingItem":${itemJson("item-1", "Apples")}},{"isMerged":false,"shoppingItem":${itemJson("item-2", "Soup")}}]}}"""
        val (connection, fake) = connection(answer)
        val added = connection.addShoppingItems(
            "list-1", listOf(ShoppingItemEntry("Apples", neededQuantity = "6"), ShoppingItemEntry("Soup", storeName = "Example Market")),
        )
        assertEquals(listOf(true, false), added.map { each -> each.isMerged })
        assertEquals(listOf("Apples", "Soup"), added.mapNotNull { each -> each.shoppingItem?.itemDescription })
        val variables = sentVariables(fake.sent[0])
        assertEquals(text("list-1"), variables["shoppingListId"])
        val entries = variables["shoppingItems"]!!.jsonArray
        assertEquals(2, entries.size)
        assertEquals(text("Example Market"), entries[1].jsonObject["storeName"])
    }

    @Test
    fun aPictureIsGivenByAttachmentOrTakenAway() = runBlocking {
        val (connection, fake) = connection("""{"data":{"SetShoppingItemPicture":${itemJson("item-1", "Apples")}}}""")
        connection.setShoppingItemPicture("item-1", "attachment-1")
        connection.setShoppingItemPicture("item-1", null)
        assertEquals(text("attachment-1"), sentVariables(fake.sent[0])["agentAttachmentId"])
        assertEquals(JsonNull, sentVariables(fake.sent[1])["agentAttachmentId"])
    }

    @Test
    fun anEventNamesTheListThatChanged() {
        assertEquals("list-1", decode(ShoppingEvent.serializer(), """{"shoppingListId":"list-1"}""").shoppingListId)
    }

    @Test
    fun aWatchedStoreNamesItsPlaceOnlyWhenThePersonHasOne() {
        val branch = decode(
            WatchedStore.serializer(),
            """{"storeName":"Example Market","storeKey":"examplemarket","osmType":"node","osmId":42,"latitudeDegrees":1.5,"longitudeDegrees":2.5,"placeId":""}""",
        )
        assertNull(branch.placeId)
        assertEquals("store:node:42", branch.id)
        assertEquals(WATCHED_STORE_RADIUS_METERS_LEAST, branch.radiusMeters)
        assertEquals("place-1", decode(WatchedStore.serializer(), """{"osmType":"node","osmId":42,"latitudeDegrees":1.5,"longitudeDegrees":2.5,"placeId":"place-1"}""").placeId)
    }

    @Test
    fun storesOfferedForWhatIsTypedBeginWithItFirst() {
        val stores = listOf("Corner Grocer", "Example Market", "Café Mercado", "The Market Hall", "Supermarket Two")
        assertEquals(listOf("Corner Grocer", "Example Market", "Café Mercado"), ShoppingStoreSuggestions.names("", stores, 3))
        // A word that begins with it, in the order given, then a name that holds it.
        assertEquals(listOf("Example Market", "The Market Hall", "Supermarket Two"), ShoppingStoreSuggestions.names("mar", stores))
        assertEquals(listOf("Corner Grocer"), ShoppingStoreSuggestions.names("co", stores))
        assertEquals(listOf("Café Mercado"), ShoppingStoreSuggestions.names("cafe", stores))
        assertEquals(listOf("Example Market"), ShoppingStoreSuggestions.names("examplem", stores))
        assertEquals(listOf("Corner Grocer"), ShoppingStoreSuggestions.names("  CORNER ", stores))
    }

    @Test
    fun aStoreAlreadyTypedIsNotOfferedAgain() {
        val stores = listOf("Example Market", "Example Market Express")
        assertEquals(listOf("Example Market Express"), ShoppingStoreSuggestions.names("example market", stores))
        assertEquals(emptyList<String>(), ShoppingStoreSuggestions.names("zzz", stores))
    }
}
