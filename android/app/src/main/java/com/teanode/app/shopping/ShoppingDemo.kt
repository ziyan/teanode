package com.teanode.app.shopping

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text

// What demo mode answers for Shopping: three lists (one shared with the person, one the person shares), two stores, items
// in every state (needed, in the cart, bought, out of stock), prices, a person shopping, a receipt and an import.

internal object ShoppingDemoData {
    const val GROCERIES_LIST_ID = "demo-list-groceries"
    const val PARTY_LIST_ID = "demo-list-party"
    const val PROJECT_LIST_ID = "demo-list-project"
    const val EXAMPLE_MARKET = "Example Market"
    const val CORNER_GROCER = "Corner Grocer"
    const val RECEIPT_ID = "demo-receipt-1"

    class Item(
        val id: String, val description: String, val listId: String = GROCERIES_LIST_ID, val listName: String = "Groceries", val store: String = "",
        val aisle: String = "", val aisleRank: Int = 0, val itemRank: Int = 0, val quantity: String = "1", val unit: String = "", val price: String = "",
        val state: String = "needed", val outOfStock: Boolean = false, val details: String = "", val cartedBy: String = "", val boughtAt: String? = null,
    ) {
        val storeKey: String get() = store.lowercase().filter { character -> character.isLetterOrDigit() }

        fun json(state: String = this.state): String = """
            {"id":"$id","shoppingListId":"$listId","listName":"$listName","itemDescription":"$description","itemDetails":"$details",
             "storeName":"$store","storeKey":"$storeKey","isStoreBrand":false,"aisleName":"$aisle","aisleRank":$aisleRank,"itemRank":$itemRank,
             "neededQuantity":"$quantity","quantityUnit":"$unit","unitPriceAmount":"$price","currencyCode":"${if (price.isEmpty()) "" else "USD"}",
             "shoppingItemState":"$state","cartedByUserId":"${if (cartedBy.isEmpty()) "" else "demo-user"}","cartedByName":"$cartedBy","cartedAt":null,
             "outOfStockAt":${if (outOfStock) "\"2030-01-02T09:00:00Z\"" else "null"},"outOfStockByUserId":"","outOfStockByName":"${if (outOfStock) "Sam" else ""}",
             "outOfStockStoreName":"","lastBoughtAt":${if (boughtAt == null) "null" else "\"$boughtAt\""},"pictureFileId":"","picturePageUrl":"","picturePath":"",
             "pictureSource":"","pictureSearchedAt":null}
        """.trimIndent()
    }

    val items = listOf(
        Item("demo-item-apples", "Apples", store = EXAMPLE_MARKET, aisle = "Produce", aisleRank = 1, quantity = "6", price = "0.89"),
        Item("demo-item-pears", "Pears", store = EXAMPLE_MARKET, aisle = "Produce", aisleRank = 1, itemRank = 1, quantity = "3", price = "1.19", details = "Ripe"),
        Item("demo-item-soup", "Tomato soup", store = EXAMPLE_MARKET, aisle = "Canned goods", aisleRank = 2, quantity = "2", price = "1.99", state = "in_cart", cartedBy = "Robin"),
        Item("demo-item-rice", "Rice", store = EXAMPLE_MARKET, aisle = "Canned goods", aisleRank = 2, itemRank = 1, quantity = "1", unit = "bag", outOfStock = true),
        Item("demo-item-chili", "Frozen chili", store = CORNER_GROCER, aisle = "Frozen", aisleRank = 3, quantity = "2", price = "4.50"),
        Item("demo-item-towels", "Paper towels", aisle = "Household", aisleRank = 4, quantity = "1"),
        Item("demo-item-bananas", "Bananas", store = EXAMPLE_MARKET, aisle = "Produce", aisleRank = 1, quantity = "1", unit = "bunch", price = "0.59", state = "not_needed", boughtAt = "2030-01-01T09:00:00Z"),
    )

    fun itemById(id: String): Item = items.firstOrNull { item -> item.id == id } ?: items.first()

    private fun trip(storeName: String, userId: String = "demo-user-2", shopper: String = "Sam") = """
        {"id":"demo-trip-1","userId":"$userId","shopperName":"$shopper","shopperUsername":"sam","storeName":"$storeName","storeKey":"${storeName.lowercase().filter { character -> character.isLetterOrDigit() }}",
         "shoppingListIds":["$GROCERIES_LIST_ID"],"startedAt":"2030-01-02T09:00:00Z","finishedAt":null}
    """.trimIndent()

    /** A test sets this to show another answer (an empty account) in place of the lists below. */
    var listsOverride: String? = null

    val lists: String get() = listsOverride ?: listsJson

    private val listsJson = """
        [{"id":"$GROCERIES_LIST_ID","userId":"demo-user","listName":"Groceries","ownerName":"Robin","ownerUsername":"robin","isOwner":true,"isShared":true,"isReminded":true,"neededCount":5,"inCartCount":1,"notNeededCount":1,"createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T00:00:00Z"},
         {"id":"$PARTY_LIST_ID","userId":"demo-user","listName":"Party supplies","ownerName":"Robin","ownerUsername":"robin","isOwner":true,"isShared":false,"isReminded":true,"neededCount":0,"inCartCount":0,"notNeededCount":0,"createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T00:00:00Z"},
         {"id":"$PROJECT_LIST_ID","userId":"demo-user-2","listName":"Weekend project","ownerName":"Sam Lee","ownerUsername":"sam","isOwner":false,"isShared":true,"isReminded":true,"neededCount":4,"inCartCount":0,"notNeededCount":0,"createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T00:00:00Z"}]
    """.trimIndent()

    fun listPage(listId: String): String {
        val own = if (listId == PROJECT_LIST_ID) """{"id":"$PROJECT_LIST_ID","userId":"demo-user-2","listName":"Weekend project","ownerName":"Sam Lee","ownerUsername":"sam","isOwner":false,"isShared":true,"isReminded":true,"neededCount":4}"""
        else """{"id":"$GROCERIES_LIST_ID","userId":"demo-user","listName":"Groceries","ownerName":"Robin","ownerUsername":"robin","isOwner":true,"isShared":true,"isReminded":true,"neededCount":5,"inCartCount":1,"notNeededCount":1}"""
        val shown = if (listId == PROJECT_LIST_ID) emptyList() else items
        return """{"totalCount":${shown.size},"shoppingList":$own,"shoppingItems":[${shown.joinToString(",") { item -> item.json() }}],"shoppingTrips":[${trip(EXAMPLE_MARKET)}]}"""
    }

    val stores = """
        [{"storeKey":"examplemarket","storeName":"$EXAMPLE_MARKET","neededCount":4,"isReminded":true},
         {"storeKey":"cornergrocer","storeName":"$CORNER_GROCER","neededCount":1,"isReminded":false}]
    """.trimIndent()

    fun storeItems(storeName: String): String {
        val here = items.filter { item -> !item.state.equals("not_needed") && (item.store == storeName || item.store.isEmpty()) }
        return """{"storeName":"$storeName","isReminded":true,"shoppingItems":[${here.joinToString(",") { item -> item.json() }}],
            "shoppingLists":[{"shoppingListId":"$GROCERIES_LIST_ID","listName":"Groceries","neededCount":4,"isIncluded":true},{"shoppingListId":"$PROJECT_LIST_ID","listName":"Weekend project","neededCount":2,"isIncluded":true}],
            "shoppingTrips":[${trip(storeName)}]}"""
    }

    val prices = """
        [{"id":"demo-price-3","shoppingItemId":"demo-item-apples","storeName":"$EXAMPLE_MARKET","unitPriceAmount":"0.8900","currencyCode":"USD","observedOn":"2030-03-01","priceSource":"receipt","financeReceiptId":"$RECEIPT_ID"},
         {"id":"demo-price-2","shoppingItemId":"demo-item-apples","storeName":"$EXAMPLE_MARKET","unitPriceAmount":"0.7900","currencyCode":"USD","observedOn":"2030-02-01","priceSource":"person","financeReceiptId":""},
         {"id":"demo-price-1","shoppingItemId":"demo-item-apples","storeName":"$EXAMPLE_MARKET","unitPriceAmount":"0.9900","currencyCode":"USD","observedOn":"2030-01-01","priceSource":"receipt","financeReceiptId":"demo-receipt-0"}]
    """.trimIndent()

    val receipt = """
        {"financeReceiptId":"$RECEIPT_ID","merchantName":"$CORNER_GROCER","purchasedOn":"2030-01-02","totalAmount":"14.53","currencyCode":"USD",
         "shoppingReceiptLines":[
           {"financeReceiptLineId":"line-1","lineNumber":1,"description":"FRZN CHILI","quantity":"1","lineAmount":"4.50","shoppingReceiptMatches":[
             {"shoppingReceiptMatch":{"id":"match-1","financeReceiptId":"$RECEIPT_ID","financeReceiptLineId":"line-1","shoppingItemId":"demo-item-chili","receiptMatchState":"applied","shoppingItemPriceId":"demo-price-3"},"shoppingItem":${items[4].json()}}]},
           {"financeReceiptLineId":"line-2","lineNumber":2,"description":"SOUP","quantity":"2","lineAmount":"3.98","shoppingReceiptMatches":[
             {"shoppingReceiptMatch":{"id":"match-2","financeReceiptId":"$RECEIPT_ID","financeReceiptLineId":"line-2","shoppingItemId":"demo-item-soup","receiptMatchState":"proposed"},"shoppingItem":${items[2].json()}}]},
           {"financeReceiptLineId":"line-3","lineNumber":3,"description":"DISH SOAP","quantity":"1","lineAmount":"3.25","shoppingReceiptMatches":[]}],
         "stillNeededItems":[${items[5].json()}]}
    """.trimIndent()

    const val IMPORT_PREVIEW = """
        {"shoppingItems":[
          {"itemDescription":"Apples","itemDetails":"","neededQuantity":"6","quantityUnit":"","aisleName":"Produce","storeName":"","isStoreBrand":false,"mergedShoppingItemId":"demo-item-apples","mergedShoppingItemState":"needed","mergedNeededQuantity":"4"},
          {"itemDescription":"Bread","itemDetails":"Rye","neededQuantity":"","quantityUnit":"","aisleName":"Bakery","storeName":"Corner Grocer","isStoreBrand":true,"mergedShoppingItemId":"","mergedShoppingItemState":"","mergedNeededQuantity":""},
          {"itemDescription":"Oat milk","itemDetails":"","neededQuantity":"2","quantityUnit":"","aisleName":"Dairy","storeName":"","isStoreBrand":false,"mergedShoppingItemId":"","mergedShoppingItemState":"","mergedNeededQuantity":""}]}
    """

    const val SHARES = """
        [{"id":"share-1","shoppingListId":"demo-list-groceries","listName":"Groceries","granteeUserId":"demo-user-2","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":""},
         {"id":"share-2","shoppingListId":"demo-list-groceries","listName":"Groceries","granteeUserId":"","granteeCircleId":"demo-circle-1","granteeCircleName":"Home"}]
    """

    const val PEOPLE = """[{"userId":"demo-user-2","username":"sam","name":"Sam Lee","avatarUrl":null,"avatarColorIndex":2},{"userId":"demo-user-3","username":"alex","name":"","avatarUrl":null,"avatarColorIndex":4}]"""
    const val CIRCLES = """[{"id":"demo-circle-1","name":"Home","members":[{"userId":"demo-user"},{"userId":"demo-user-2"}]}]"""
}

/** What demo mode answers for Shopping. The most specific document names come first. */
val shoppingDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListShoppingListShares", ShoppingDemoData.SHARES),
    graphQLField("ListShoppingStoreItems") { variables -> ShoppingDemoData.storeItems(variables.text("storeName").orEmpty()) },
    graphQLField("ListShoppingStores", ShoppingDemoData.stores),
    graphQLField("ListShoppingLists", ShoppingDemoData.lists),
    graphQLField("GetShoppingList") { variables -> ShoppingDemoData.listPage(variables.text("shoppingListId").orEmpty()) },
    graphQLField("GetShoppingItem") { variables -> ShoppingDemoData.itemById(variables.text("shoppingItemId").orEmpty()).json() },
    graphQLField("ListShoppingItemPrices", ShoppingDemoData.prices),
    graphQLField("ListSimilarShoppingItems") { variables ->
        val typed = variables.text("itemDescription").orEmpty().lowercase()
        val like = ShoppingDemoData.items.filter { item -> typed.length >= 2 && item.description.lowercase().contains(typed.take(3)) }
        "[${like.joinToString(",") { item -> item.json() }}]"
    },
    graphQLField("ListShoppingReceiptMatches", ShoppingDemoData.receipt),
    graphQLField("ListOpenShoppingTrips", "[]"),
    graphQLField("PreviewShoppingImport", ShoppingDemoData.IMPORT_PREVIEW),
    graphQLField("ListWatchedStores", "[]"),
    graphQLField("ListCirclePeople", ShoppingDemoData.PEOPLE),
    graphQLField("ListCircles", ShoppingDemoData.CIRCLES),
    graphQLField("CheckShoppingItem") { variables ->
        val isChecked = variables.text("isChecked") == "true"
        ShoppingDemoData.itemById(variables.text("shoppingItemId").orEmpty()).json(if (isChecked) "in_cart" else "needed")
    },
    graphQLField("SetShoppingItemNeed") { variables -> ShoppingDemoData.itemById(variables.text("shoppingItemId").orEmpty()).json("needed") },
    graphQLField("SetShoppingItemOutOfStock") { variables -> ShoppingDemoData.itemById(variables.text("shoppingItemId").orEmpty()).json() },
    graphQLField("UpdateShoppingItem") { variables -> ShoppingDemoData.itemById(variables.text("shoppingItemId").orEmpty()).json() },
    graphQLField("SetShoppingItemPicture") { variables -> ShoppingDemoData.itemById(variables.text("shoppingItemId").orEmpty()).json() },
    graphQLField("AddShoppingItems", """[{"isMerged":false,"shoppingItem":${ShoppingDemoData.items.first().json()}}]"""),
    graphQLField("CreateShoppingList", """{"id":"demo-list-new","userId":"demo-user","listName":"New list","ownerName":"Robin","ownerUsername":"robin","isOwner":true}"""),
    graphQLField("StartShopping", """{"id":"demo-trip-1","userId":"demo-user","shopperName":"Robin","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["demo-list-groceries"],"startedAt":"2030-01-02T09:00:00Z"}"""),
    graphQLField("FinishShopping", """{"id":"demo-trip-1","userId":"demo-user","shopperName":"Robin","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["demo-list-groceries"],"startedAt":"2030-01-02T09:00:00Z","finishedAt":"2030-01-02T10:00:00Z"}"""),
    graphQLField("DecideShoppingReceiptMatch", """{"id":"match-2","receiptMatchState":"applied"}"""),
    graphQLField("ShareShoppingList", """{"id":"share-3","shoppingListId":"demo-list-groceries","listName":"Groceries","granteeUserId":"demo-user-3","granteeUsername":"alex","granteeName":"","granteeCircleId":""}"""),
)
