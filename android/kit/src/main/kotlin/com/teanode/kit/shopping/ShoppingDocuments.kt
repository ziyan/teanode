package com.teanode.kit.shopping

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object ShoppingDocuments {
    const val SHOPPING_ITEM_FIELDS = $$"""id shoppingListId listName itemDescription itemDetails storeName storeKey isStoreBrand aisleName aisleRank itemRank
        neededQuantity quantityUnit unitPriceAmount currencyCode shoppingItemState cartedByUserId cartedByName cartedAt
        outOfStockAt outOfStockByUserId outOfStockByName outOfStockStoreName lastBoughtAt pictureFileId picturePageUrl
        picturePath pictureSource pictureSearchedAt"""

    const val SHOPPING_LIST_FIELDS = $$"""id userId listName ownerName ownerUsername isOwner isShared isReminded neededCount inCartCount notNeededCount createdAt updatedAt"""

    const val SHOPPING_TRIP_FIELDS = $$"""id userId shopperName shopperUsername storeName storeKey shoppingListIds startedAt finishedAt"""

    const val SHOPPING_SHARE_FIELDS = $$"""id shoppingListId listName ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeName granteeCircleId
        granteeCircleName createdAt"""

    const val SHOPPING_MATCH_FIELDS = $$"""id financeReceiptId financeReceiptLineId shoppingItemId receiptMatchState matchedQuantity previousShoppingItemState
        previousNeededQuantity shoppingItemPriceId"""

    const val SHOPPING_LISTS = $$"""query { ListShoppingLists { $${SHOPPING_LIST_FIELDS} } }"""

    const val SHOPPING_LIST = $$"""query ($shoppingListId: String!, $shoppingItemStates: [String!], $offset: Int, $pageSize: Int) {
          GetShoppingList(shoppingListId: $shoppingListId, shoppingItemStates: $shoppingItemStates, offset: $offset, pageSize: $pageSize) {
            totalCount
            shoppingList { $${SHOPPING_LIST_FIELDS} }
            shoppingItems { $${SHOPPING_ITEM_FIELDS} }
            shoppingTrips { $${SHOPPING_TRIP_FIELDS} }
          }
        }"""

    const val SHOPPING_ITEM = $$"""query ($shoppingItemId: String!) { GetShoppingItem(shoppingItemId: $shoppingItemId) { $${SHOPPING_ITEM_FIELDS} } }"""

    const val SHOPPING_LIST_SHARES = $$"""query ($shoppingListId: String!) { ListShoppingListShares(shoppingListId: $shoppingListId) { $${SHOPPING_SHARE_FIELDS} } }"""

    const val SHOPPING_STORES = $$"""query { ListShoppingStores { storeKey storeName neededCount isReminded } }"""

    const val WATCHED_STORES = $$"""query ($latitudeDegrees: Float, $longitudeDegrees: Float) {
          ListWatchedStores(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees) {
            storeName storeKey placeName osmType osmId osmCategory latitudeDegrees longitudeDegrees radiusMeters distanceMeters placeId
          }
        }"""

    const val SHOPPING_STORE_ITEMS = $$"""query ($storeName: String!, $shoppingListIds: [String!], $isAnyStoreIncluded: Boolean) {
          ListShoppingStoreItems(storeName: $storeName, shoppingListIds: $shoppingListIds, isAnyStoreIncluded: $isAnyStoreIncluded) {
            storeName
            isReminded
            shoppingItems { $${SHOPPING_ITEM_FIELDS} }
            shoppingLists { shoppingListId listName neededCount isIncluded }
            shoppingTrips { $${SHOPPING_TRIP_FIELDS} }
          }
        }"""

    const val OPEN_SHOPPING_TRIPS = $$"""query { ListOpenShoppingTrips { $${SHOPPING_TRIP_FIELDS} } }"""

    const val SIMILAR_SHOPPING_ITEMS = $$"""query ($shoppingListId: String!, $itemDescription: String!) {
          ListSimilarShoppingItems(shoppingListId: $shoppingListId, itemDescription: $itemDescription) { $${SHOPPING_ITEM_FIELDS} }
        }"""

    const val SHOPPING_ITEM_PRICES = $$"""query ($shoppingItemId: String!) {
          ListShoppingItemPrices(shoppingItemId: $shoppingItemId) {
            id shoppingItemId storeName storeKey unitPriceAmount currencyCode quantity quantityUnit observedOn priceSource financeReceiptId createdAt
          }
        }"""

    const val SHOPPING_RECEIPT_MATCHES = $$"""query ($financeReceiptId: String!) {
          ListShoppingReceiptMatches(financeReceiptId: $financeReceiptId) {
            financeReceiptId merchantName purchasedOn totalAmount currencyCode
            shoppingReceiptLines {
              financeReceiptLineId lineNumber description quantity lineAmount
              shoppingReceiptMatches {
                shoppingReceiptMatch { $${SHOPPING_MATCH_FIELDS} }
                shoppingItem { $${SHOPPING_ITEM_FIELDS} }
              }
            }
            stillNeededItems { $${SHOPPING_ITEM_FIELDS} }
          }
        }"""

    const val PREVIEW_SHOPPING_IMPORT = $$"""query ($shoppingListId: String, $importText: String, $agentAttachmentIds: [String!]) {
          PreviewShoppingImport(shoppingListId: $shoppingListId, importText: $importText, agentAttachmentIds: $agentAttachmentIds) {
            shoppingItems {
              itemDescription itemDetails neededQuantity quantityUnit aisleName storeName isStoreBrand
              mergedShoppingItemId mergedShoppingItemState mergedNeededQuantity
            }
          }
        }"""

    const val CREATE_SHOPPING_LIST = $$"""mutation ($listName: String!) { CreateShoppingList(listName: $listName) { $${SHOPPING_LIST_FIELDS} } }"""

    const val RENAME_SHOPPING_LIST = $$"""mutation ($shoppingListId: String!, $listName: String!) {
          RenameShoppingList(shoppingListId: $shoppingListId, listName: $listName) { $${SHOPPING_LIST_FIELDS} }
        }"""

    const val DELETE_SHOPPING_LIST = $$"""mutation ($shoppingListId: String!) { DeleteShoppingList(shoppingListId: $shoppingListId) }"""

    const val SHARE_SHOPPING_LIST = $$"""mutation ($shoppingListId: String!, $granteeUserId: String, $granteeCircleId: String) {
          ShareShoppingList(shoppingListId: $shoppingListId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) {
            $${SHOPPING_SHARE_FIELDS}
          }
        }"""

    const val UNSHARE_SHOPPING_LIST = $$"""mutation ($shoppingListShareId: String!) { UnshareShoppingList(shoppingListShareId: $shoppingListShareId) }"""

    const val SET_SHOPPING_LIST_REMINDER = $$"""mutation ($shoppingListId: String!, $isReminded: Boolean!) {
          SetShoppingListReminder(shoppingListId: $shoppingListId, isReminded: $isReminded)
        }"""

    const val SET_SHOPPING_STORE_REMINDER = $$"""mutation ($storeName: String!, $isReminded: Boolean!) { SetShoppingStoreReminder(storeName: $storeName, isReminded: $isReminded) }"""

    const val ADD_SHOPPING_ITEMS = $$"""mutation ($shoppingListId: String!, $shoppingItems: [ShoppingItemEntryInput!]!) {
          AddShoppingItems(shoppingListId: $shoppingListId, shoppingItems: $shoppingItems) {
            isMerged
            shoppingItem { $${SHOPPING_ITEM_FIELDS} }
          }
        }"""

    const val UPDATE_SHOPPING_ITEM = $$"""mutation (
          $shoppingItemId: String!, $itemDescription: String, $itemDetails: String, $storeName: String, $isStoreBrand: Boolean,
          $aisleName: String, $neededQuantity: String, $quantityUnit: String
        ) {
          UpdateShoppingItem(
            shoppingItemId: $shoppingItemId, itemDescription: $itemDescription, itemDetails: $itemDetails, storeName: $storeName,
            isStoreBrand: $isStoreBrand, aisleName: $aisleName, neededQuantity: $neededQuantity, quantityUnit: $quantityUnit
          ) { $${SHOPPING_ITEM_FIELDS} }
        }"""

    const val REMOVE_SHOPPING_ITEMS = $$"""mutation ($shoppingItemIds: [String!]!) { RemoveShoppingItems(shoppingItemIds: $shoppingItemIds) }"""

    const val CHECK_SHOPPING_ITEM = $$"""mutation ($shoppingItemId: String!, $isChecked: Boolean!, $storeName: String) {
          CheckShoppingItem(shoppingItemId: $shoppingItemId, isChecked: $isChecked, storeName: $storeName) { $${SHOPPING_ITEM_FIELDS} }
        }"""

    const val SET_SHOPPING_ITEM_NEED = $$"""mutation ($shoppingItemId: String!, $isNeeded: Boolean!, $neededQuantity: String) {
          SetShoppingItemNeed(shoppingItemId: $shoppingItemId, isNeeded: $isNeeded, neededQuantity: $neededQuantity) { $${SHOPPING_ITEM_FIELDS} }
        }"""

    const val SET_SHOPPING_ITEM_OUT_OF_STOCK = $$"""mutation ($shoppingItemId: String!, $isOutOfStock: Boolean!, $storeName: String) {
          SetShoppingItemOutOfStock(shoppingItemId: $shoppingItemId, isOutOfStock: $isOutOfStock, storeName: $storeName) {
            $${SHOPPING_ITEM_FIELDS}
          }
        }"""

    const val RECORD_SHOPPING_ITEM_PRICE = $$"""mutation ($shoppingItemId: String!, $unitPriceAmount: String!, $storeName: String, $currencyCode: String, $observedOn: String) {
          RecordShoppingItemPrice(
            shoppingItemId: $shoppingItemId, unitPriceAmount: $unitPriceAmount, storeName: $storeName, currencyCode: $currencyCode,
            observedOn: $observedOn
          ) { id }
        }"""

    const val FIND_SHOPPING_ITEM_PICTURE = $$"""mutation ($shoppingItemId: String!) { FindShoppingItemPicture(shoppingItemId: $shoppingItemId) }"""

    const val SET_SHOPPING_ITEM_PICTURE = $$"""mutation ($shoppingItemId: String!, $agentAttachmentId: String) {
          SetShoppingItemPicture(shoppingItemId: $shoppingItemId, agentAttachmentId: $agentAttachmentId) { $${SHOPPING_ITEM_FIELDS} }
        }"""

    const val START_SHOPPING = $$"""mutation ($storeName: String, $shoppingListIds: [String!]!) {
          StartShopping(storeName: $storeName, shoppingListIds: $shoppingListIds) { $${SHOPPING_TRIP_FIELDS} }
        }"""

    const val FINISH_SHOPPING = $$"""mutation ($shoppingTripId: String) { FinishShopping(shoppingTripId: $shoppingTripId) { $${SHOPPING_TRIP_FIELDS} } }"""

    const val MATCH_SHOPPING_RECEIPT = $$"""mutation ($financeReceiptId: String!) { MatchShoppingReceipt(financeReceiptId: $financeReceiptId) }"""

    const val DECIDE_SHOPPING_RECEIPT_MATCH = $$"""mutation ($shoppingReceiptMatchId: String!, $isApplied: Boolean!) {
          DecideShoppingReceiptMatch(shoppingReceiptMatchId: $shoppingReceiptMatchId, isApplied: $isApplied) { $${SHOPPING_MATCH_FIELDS} }
        }"""

    const val UNDO_SHOPPING_RECEIPT = $$"""mutation ($financeReceiptId: String!) { UndoShoppingReceipt(financeReceiptId: $financeReceiptId) }"""

    const val ADD_SHOPPING_ITEM_FROM_RECEIPT_LINE = $$"""mutation ($financeReceiptLineId: String!, $shoppingListId: String!) {
          AddShoppingItemFromReceiptLine(financeReceiptLineId: $financeReceiptLineId, shoppingListId: $shoppingListId) {
            $${SHOPPING_ITEM_FIELDS}
          }
        }"""

    const val SHOPPING_EVENTS = $$"""subscription { ShoppingEvents { shoppingListId } }"""


    /** Every operation by name, which the tests check against the server's operations. */
    val all: Map<String, String> = mapOf(
        "shoppingLists" to SHOPPING_LISTS,
        "shoppingList" to SHOPPING_LIST,
        "shoppingItem" to SHOPPING_ITEM,
        "shoppingListShares" to SHOPPING_LIST_SHARES,
        "shoppingStores" to SHOPPING_STORES,
        "shoppingStoreItems" to SHOPPING_STORE_ITEMS,
        "openShoppingTrips" to OPEN_SHOPPING_TRIPS,
        "similarShoppingItems" to SIMILAR_SHOPPING_ITEMS,
        "shoppingItemPrices" to SHOPPING_ITEM_PRICES,
        "shoppingReceiptMatches" to SHOPPING_RECEIPT_MATCHES,
        "previewShoppingImport" to PREVIEW_SHOPPING_IMPORT,
        "createShoppingList" to CREATE_SHOPPING_LIST,
        "renameShoppingList" to RENAME_SHOPPING_LIST,
        "deleteShoppingList" to DELETE_SHOPPING_LIST,
        "shareShoppingList" to SHARE_SHOPPING_LIST,
        "unshareShoppingList" to UNSHARE_SHOPPING_LIST,
        "setShoppingListReminder" to SET_SHOPPING_LIST_REMINDER,
        "setShoppingStoreReminder" to SET_SHOPPING_STORE_REMINDER,
        "addShoppingItems" to ADD_SHOPPING_ITEMS,
        "updateShoppingItem" to UPDATE_SHOPPING_ITEM,
        "removeShoppingItems" to REMOVE_SHOPPING_ITEMS,
        "checkShoppingItem" to CHECK_SHOPPING_ITEM,
        "setShoppingItemNeed" to SET_SHOPPING_ITEM_NEED,
        "setShoppingItemOutOfStock" to SET_SHOPPING_ITEM_OUT_OF_STOCK,
        "recordShoppingItemPrice" to RECORD_SHOPPING_ITEM_PRICE,
        "findShoppingItemPicture" to FIND_SHOPPING_ITEM_PICTURE,
        "setShoppingItemPicture" to SET_SHOPPING_ITEM_PICTURE,
        "startShopping" to START_SHOPPING,
        "finishShopping" to FINISH_SHOPPING,
        "matchShoppingReceipt" to MATCH_SHOPPING_RECEIPT,
        "decideShoppingReceiptMatch" to DECIDE_SHOPPING_RECEIPT_MATCH,
        "undoShoppingReceipt" to UNDO_SHOPPING_RECEIPT,
        "addShoppingItemFromReceiptLine" to ADD_SHOPPING_ITEM_FROM_RECEIPT_LINE,
        "shoppingEvents" to SHOPPING_EVENTS,
    )
}
