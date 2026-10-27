import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// ShoppingTests cover the shopping lists: what each of the server's views
/// decodes to, how items are laid out and added up, what each call sends, and
/// that the documents name the server's operations.
final class ShoppingTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func sentQuery(_ request: URLRequest) throws -> String {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        return body["query"]?.stringValue ?? ""
    }

    /// An item as the server writes it, with the fields a test cares about.
    private func itemJSON(
        _ id: String, _ description: String, listId: String = "list-1", listName: String = "Groceries", store: String = "", aisle: String = "",
        aisleRank: Int = 0, itemRank: Int = 0, quantity: String = "1", unit: String = "", price: String = "", state: String = "needed",
        outOfStockAt: String? = nil
    ) -> String {
        let key = store.lowercased().filter { $0.isLetter || $0.isNumber }
        let outOfStock = outOfStockAt.map { #""\#($0)""# } ?? "null"
        return #"""
            {"id":"\#(id)","shoppingListId":"\#(listId)","listName":"\#(listName)","itemDescription":"\#(description)","itemDetails":"",
             "storeName":"\#(store)","storeKey":"\#(key)","isStoreBrand":false,"aisleName":"\#(aisle)","aisleRank":\#(aisleRank),"itemRank":\#(itemRank),
             "neededQuantity":"\#(quantity)","quantityUnit":"\#(unit)","unitPriceAmount":"\#(price)","currencyCode":"\#(price.isEmpty ? "" : "USD")",
             "shoppingItemState":"\#(state)","cartedByUserId":"","cartedByName":"","cartedAt":null,"outOfStockAt":\#(outOfStock),
             "outOfStockByUserId":"","outOfStockByName":"","outOfStockStoreName":"","lastBoughtAt":null,"pictureFileId":"","picturePageUrl":"",
             "picturePath":"","pictureSource":"","pictureSearchedAt":null}
            """#
    }

    private func item(
        _ id: String, _ description: String, listId: String = "list-1", store: String = "", aisle: String = "", aisleRank: Int = 0,
        itemRank: Int = 0, quantity: String = "1", unit: String = "", price: String = "", state: String = "needed", outOfStockAt: String? = nil
    ) throws -> ShoppingItem {
        let json = itemJSON(
            id, description, listId: listId, store: store, aisle: aisle, aisleRank: aisleRank, itemRank: itemRank, quantity: quantity,
            unit: unit, price: price, state: state, outOfStockAt: outOfStockAt)
        return try JSONDecoder().decode(ShoppingItem.self, from: Data(json.utf8))
    }

    // MARK: The documents

    func testTheDocumentsNameTheServersOperations() {
        let operations: [String: String] = [
            "shoppingLists": "ListShoppingLists", "shoppingList": "GetShoppingList", "shoppingItem": "GetShoppingItem",
            "shoppingListShares": "ListShoppingListShares", "shoppingStores": "ListShoppingStores",
            "shoppingStoreItems": "ListShoppingStoreItems", "openShoppingTrips": "ListOpenShoppingTrips",
            "shoppingItemPrices": "ListShoppingItemPrices", "shoppingReceiptMatches": "ListShoppingReceiptMatches",
            "previewShoppingImport": "PreviewShoppingImport", "createShoppingList": "CreateShoppingList",
            "renameShoppingList": "RenameShoppingList", "deleteShoppingList": "DeleteShoppingList", "shareShoppingList": "ShareShoppingList",
            "unshareShoppingList": "UnshareShoppingList", "setShoppingListReminder": "SetShoppingListReminder",
            "setShoppingStoreReminder": "SetShoppingStoreReminder", "addShoppingItems": "AddShoppingItems",
            "updateShoppingItem": "UpdateShoppingItem", "removeShoppingItems": "RemoveShoppingItems", "checkShoppingItem": "CheckShoppingItem",
            "setShoppingItemNeed": "SetShoppingItemNeed", "setShoppingItemOutOfStock": "SetShoppingItemOutOfStock",
            "recordShoppingItemPrice": "RecordShoppingItemPrice", "findShoppingItemPicture": "FindShoppingItemPicture",
            "setShoppingItemPicture": "SetShoppingItemPicture", "startShopping": "StartShopping", "finishShopping": "FinishShopping",
            "matchShoppingReceipt": "MatchShoppingReceipt", "decideShoppingReceiptMatch": "DecideShoppingReceiptMatch",
            "undoShoppingReceipt": "UndoShoppingReceipt", "addShoppingItemFromReceiptLine": "AddShoppingItemFromReceiptLine",
            "shoppingEvents": "ShoppingEvents",
        ]
        XCTAssertEqual(Set(Documents.shoppingDocuments.keys), Set(operations.keys))
        for (name, operation) in operations {
            XCTAssertTrue(Documents.shoppingDocuments[name]?.contains(operation) == true, "\(name) does not call \(operation)")
        }
        XCTAssertTrue(Documents.addShoppingItems.contains("[ShoppingItemEntryInput!]!"))
        XCTAssertTrue(Documents.shoppingEvents.hasPrefix("subscription"))
    }

    // MARK: Decoding

    func testAListDecodesWithItsCounts() throws {
        let json = #"""
            {"id":"list-1","userId":"user-1","listName":"Groceries","ownerName":"Robin","ownerUsername":"robin","isOwner":true,"isShared":true,
             "isReminded":true,"neededCount":5,"inCartCount":1,"notNeededCount":2,"createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T00:00:00Z"}
            """#
        let list = try JSONDecoder().decode(ShoppingList.self, from: Data(json.utf8))
        XCTAssertEqual(list.id, "list-1")
        XCTAssertEqual(list.listName, "Groceries")
        XCTAssertTrue(list.isOwner)
        XCTAssertTrue(list.isShared)
        XCTAssertEqual(list.neededCount, 5)
        XCTAssertEqual(list.inCartCount, 1)
        XCTAssertEqual(list.notNeededCount, 2)
        XCTAssertEqual(list.shownOwner, "Robin")
    }

    func testAnItemDecodesWithItsStateAndPrice() throws {
        let apples = try item("item-1", "Apples", store: "Example Market", aisle: "Produce", quantity: "6.00000000", price: "0.8900")
        XCTAssertEqual(apples.state, .needed)
        XCTAssertTrue(apples.isNeeded)
        XCTAssertFalse(apples.isOutOfStock)
        XCTAssertEqual(apples.storeKey, "examplemarket")
        XCTAssertEqual(apples.shownQuantity, "6")
        XCTAssertEqual(apples.unitPrice, Decimal(string: "0.89"))
        XCTAssertEqual(apples.shownTitle, "6 Apples")
        XCTAssertFalse(apples.hasPicture)
        XCTAssertEqual(apples.initial, "A")
    }

    func testAnItemInTheCartOrNotNeededOrOutOfStockSaysSo() throws {
        XCTAssertTrue(try item("a", "Soup", state: "in_cart").isInCart)
        XCTAssertTrue(try item("b", "Soup", state: "not_needed").isNotNeeded)
        let missing = try item("c", "Soup", outOfStockAt: "2030-01-02T09:00:00Z")
        XCTAssertTrue(missing.isOutOfStock)
        // A state the app does not know is taken for needed.
        XCTAssertEqual(try item("d", "Soup", state: "somewhere_new").state, .needed)
    }

    func testAnItemTheServerSendsFewFieldsOfStillDecodes() throws {
        let sparse = try JSONDecoder().decode(ShoppingItem.self, from: Data(#"{"id":"item-9","itemDescription":"Soup"}"#.utf8))
        XCTAssertEqual(sparse.id, "item-9")
        XCTAssertEqual(sparse.storeName, "")
        XCTAssertEqual(sparse.shoppingItemState, "")
        XCTAssertTrue(sparse.isNeeded)
        XCTAssertNil(sparse.cartedAt)
        XCTAssertThrowsError(try JSONDecoder().decode(ShoppingItem.self, from: Data(#"{"itemDescription":"Soup"}"#.utf8)))
    }

    func testAListPageDecodesItsItemsAndTrips() async throws {
        let json = #"""
            {"data":{"GetShoppingList":{"totalCount":2,
              "shoppingList":{"id":"list-1","listName":"Groceries","isOwner":false,"ownerName":"","ownerUsername":"sam","neededCount":2},
              "shoppingItems":[\#(itemJSON("item-1", "Apples")),\#(itemJSON("item-2", "Soup", state: "not_needed"))],
              "shoppingTrips":[{"id":"trip-1","userId":"user-2","shopperName":"","shopperUsername":"sam","storeName":"Example Market",
                "storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:00:00Z","finishedAt":null}]}}}
            """#
        let (connection, fake) = connection(answering: json)
        let page = try await connection.shoppingList(shoppingListId: "list-1", states: [.needed, .inCart])
        XCTAssertEqual(page.totalCount, 2)
        XCTAssertEqual(page.shoppingItems.map(\.itemDescription), ["Apples", "Soup"])
        XCTAssertEqual(page.shoppingList?.shownOwner, "sam")
        let trip = try XCTUnwrap(page.shoppingTrips.first)
        XCTAssertTrue(trip.isOpen)
        XCTAssertEqual(trip.shownShopper, "sam")
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["shoppingListId"], .string("list-1"))
        XCTAssertEqual(variables["shoppingItemStates"], .array([.string("needed"), .string("in_cart")]))
    }

    func testAStoreKeyKeepsLettersOneScalarAtATime() {
        XCTAssertEqual(ShoppingStoreKey.key(of: "Example Market #12"), "examplemarket12")
        // A combining accent is a mark, not a letter, as on the server.
        XCTAssertEqual(ShoppingStoreKey.key(of: "Cafe\u{301} Corner"), "cafecorner")
        XCTAssertEqual(ShoppingStoreKey.key(of: "Café Corner"), "cafécorner")
        // A voiced kana mark written apart is dropped; the kana stays.
        XCTAssertEqual(ShoppingStoreKey.key(of: "\u{30AB}\u{3099}"), "\u{30AB}")
    }

    func testAWholeListIsReadPageByPage() async throws {
        let pages = [
            [itemJSON("item-1", "Apples"), itemJSON("item-2", "Bread")].joined(separator: ","),
            itemJSON("item-3", "Soup"),
        ]
        let fake = FakeServer { request in
            let body = try? JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
            guard case .object(let variables) = body?["variables"], case .number(let offset) = variables["offset"] else { return (400, "") }
            let items = offset == 0 ? pages[0] : pages[1]
            return (200, #"{"data":{"GetShoppingList":{"totalCount":3,"shoppingList":{"id":"list-1","listName":"Groceries"},"#
                + #""shoppingItems":[\#(items)],"shoppingTrips":[]}}}"#)
        }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        let page = try await Connection(account: account, transport: fake).wholeShoppingList(shoppingListId: "list-1", pageSize: 2)
        XCTAssertEqual(page.shoppingItems.map(\.itemDescription), ["Apples", "Bread", "Soup"])
        XCTAssertEqual(fake.sent.count, 2)
        XCTAssertEqual(try sentVariables(fake.sent[1])["offset"], .number(2))
    }

    func testTheStoreViewDecodesItsListsAndWhoIsShopping() throws {
        let json = #"""
            {"storeName":"Example Market","isReminded":false,
             "shoppingItems":[\#(itemJSON("item-1", "Apples", store: "Example Market"))],
             "shoppingLists":[{"shoppingListId":"list-1","listName":"Groceries","neededCount":7,"isIncluded":true},
                              {"shoppingListId":"list-2","listName":"Party supplies","neededCount":2,"isIncluded":false}],
             "shoppingTrips":[]}
            """#
        let view = try JSONDecoder().decode(ShoppingStoreItems.self, from: Data(json.utf8))
        XCTAssertEqual(view.storeName, "Example Market")
        XCTAssertFalse(view.isReminded)
        XCTAssertEqual(view.shoppingLists.map(\.listName), ["Groceries", "Party supplies"])
        XCTAssertEqual(view.includedListIds, ["list-1"])
        XCTAssertEqual(view.shoppingItems.count, 1)
    }

    func testSharesNameACircleOrAPerson() throws {
        let json = #"""
            [{"id":"share-1","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"","granteeCircleId":"circle-1","granteeCircleName":"Home"},
             {"id":"share-2","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"user-2","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":""},
             {"id":"share-3","shoppingListId":"list-1","listName":"Groceries","granteeUserId":"user-3","granteeUsername":"alex","granteeName":"","granteeCircleId":""}]
            """#
        let shares = try JSONDecoder().decode([ShoppingListShare].self, from: Data(json.utf8))
        XCTAssertTrue(shares[0].isWithCircle)
        XCTAssertEqual(shares.map(\.shownGrantee), ["Home", "Sam Lee", "alex"])
        XCTAssertEqual(shares.shownGrantees, ["Home", "Sam Lee", "alex"])
    }

    func testPricesDecodeAndTheEarlierOneIsFound() throws {
        let json = #"""
            [{"id":"price-3","shoppingItemId":"item-1","storeName":"Example Market","unitPriceAmount":"0.9200","currencyCode":"USD","observedOn":"2030-03-01","priceSource":"receipt","financeReceiptId":"receipt-1"},
             {"id":"price-2","shoppingItemId":"item-1","storeName":"Example Market","unitPriceAmount":"0.8900","currencyCode":"USD","observedOn":"2030-02-01","priceSource":"person","financeReceiptId":""},
             {"id":"price-1","shoppingItemId":"item-1","storeName":"Example Market","unitPriceAmount":"0.7900","currencyCode":"USD","observedOn":"2030-01-01","priceSource":"receipt","financeReceiptId":"receipt-0"}]
            """#
        let prices = try JSONDecoder().decode([ShoppingItemPrice].self, from: Data(json.utf8))
        XCTAssertTrue(prices[0].isFromReceipt)
        XCTAssertFalse(prices[1].isFromReceipt)
        XCTAssertEqual(prices[0].amount, Decimal(string: "0.92"))
        XCTAssertNotNil(prices[0].observedDate)
        XCTAssertEqual(ShoppingPriceHistory.previous(before: "price-3", in: prices)?.id, "price-2")
        XCTAssertNil(ShoppingPriceHistory.previous(before: "price-1", in: prices))
        XCTAssertNil(ShoppingPriceHistory.previous(before: "price-9", in: prices))
        XCTAssertEqual(ShoppingPriceHistory.chartable(prices).map(\.id), ["price-1", "price-2", "price-3"])
    }

    // MARK: Numbers

    func testCountsAreShownWithoutTrailingZeros() {
        XCTAssertEqual(ShoppingQuantity.shown("2.00000000"), "2")
        XCTAssertEqual(ShoppingQuantity.shown("1.50000000"), "1.5")
        XCTAssertEqual(ShoppingQuantity.shown("3"), "3")
        XCTAssertEqual(ShoppingQuantity.shown(""), "1")
        XCTAssertEqual(ShoppingQuantity.text(of: 2), "2")
        XCTAssertEqual(ShoppingQuantity.text(of: 1.5), "1.5")
        XCTAssertEqual(ShoppingQuantity.value(of: "2.50000000"), 2.5)
        XCTAssertEqual(ShoppingQuantity.value(of: "nonsense"), 1)
    }

    func testPricesAreTypedWithTwoPlacesAtLeast() {
        XCTAssertEqual(ShoppingMoney.typedAmount("1.2900"), "1.29")
        XCTAssertEqual(ShoppingMoney.typedAmount("2"), "2.00")
        XCTAssertEqual(ShoppingMoney.typedAmount("2.5"), "2.50")
        XCTAssertEqual(ShoppingMoney.typedAmount("0.8450"), "0.845")
        XCTAssertEqual(ShoppingMoney.typedAmount(""), "")
        XCTAssertTrue(ShoppingMoney.isPrice("1.99"))
        XCTAssertFalse(ShoppingMoney.isPrice("1,99"))
        XCTAssertFalse(ShoppingMoney.isPrice("0"))
        XCTAssertFalse(ShoppingMoney.isPrice("free"))
    }

    func testAStoreIsComparedWithoutCaseOrPunctuation() {
        XCTAssertEqual(ShoppingStoreKey.key(of: "Example Market"), "examplemarket")
        XCTAssertEqual(ShoppingStoreKey.key(of: "EXAMPLE-MARKET's"), "examplemarkets")
        XCTAssertEqual(ShoppingStoreKey.key(of: "  "), "")
    }

    func testTheEstimateAddsUnitPricesTimesCountsInTheCommonCurrency() throws {
        let items = [
            try item("a", "Apples", quantity: "6", price: "0.89"), try item("b", "Soup", quantity: "2", price: "1.99"),
            try item("c", "Salt"),
        ]
        let total = try XCTUnwrap(ShoppingMoney.estimate(of: items))
        XCTAssertEqual(total.amount, Decimal(string: "9.32"))
        XCTAssertEqual(total.currencyCode, "USD")
        XCTAssertEqual(total.pricedCount, 2)
        XCTAssertTrue(total.shown.contains("9.32"))
        XCTAssertNil(ShoppingMoney.estimate(of: [try item("c", "Salt")]))
    }

    // MARK: Grouping

    func testItemsGroupByStoreWithAnyStoreLast() throws {
        let items = [
            try item("a", "Soup", store: "Example Market", itemRank: 2), try item("b", "Chili", store: "Corner Grocer"),
            try item("c", "Towels"), try item("d", "Apples", store: "example market", itemRank: 1),
        ]
        let groups = ShoppingGrouping.byStore(items)
        XCTAssertEqual(groups.map(\.title), ["Corner Grocer", "Example Market", ""])
        XCTAssertEqual(groups[1].items.map(\.itemDescription), ["Apples", "Soup"])
        XCTAssertTrue(groups[2].isUnnamed)
    }

    func testItemsGroupByAisleInTheOrderTheyWereGiven() throws {
        let items = [
            try item("a", "Soup", aisle: "Canned goods", aisleRank: 2), try item("b", "Apples", aisle: "Produce", aisleRank: 1),
            try item("c", "Towels"), try item("d", "Pears", aisle: "produce", aisleRank: 1, itemRank: 1),
        ]
        let groups = ShoppingGrouping.byAisle(items)
        XCTAssertEqual(groups.map(\.title), ["Produce", "Canned goods", ""])
        XCTAssertEqual(groups[0].items.map(\.itemDescription), ["Apples", "Pears"])
    }

    func testTheStoreScreenSeparatesTheStoresOwnItemsFromThoseForAnyStore() throws {
        let items = [
            try item("a", "Apples", store: "Example Market", aisle: "Produce", aisleRank: 1),
            try item("b", "Soup", store: "Example Market", aisle: "Canned goods", aisleRank: 2), try item("c", "Towels", aisle: "Household"),
        ]
        let sections = ShoppingGrouping.forStore(items)
        XCTAssertEqual(sections.aisleGroups.map(\.title), ["Produce", "Canned goods"])
        XCTAssertEqual(sections.anyStoreItems.map(\.itemDescription), ["Towels"])
    }

    func testBoughtItemsAreSplitFromThoseToBuy() throws {
        let items = [try item("a", "Apples"), try item("b", "Soup", state: "in_cart"), try item("c", "Bread", state: "not_needed")]
        let split = ShoppingGrouping.split(items)
        XCTAssertEqual(split.toBuy.map(\.itemDescription), ["Apples", "Soup"])
        XCTAssertEqual(split.notNeeded.map(\.itemDescription), ["Bread"])
    }

    func testTheCartCountsWhatIsLeftToFindAndWhatIsInIt() throws {
        let items = [
            try item("a", "Apples", quantity: "6", price: "0.89", state: "in_cart"), try item("b", "Soup"),
            try item("c", "Oranges", outOfStockAt: "2030-01-02T09:00:00Z"), try item("d", "Bread", state: "not_needed"),
            try item("e", "Salt", state: "in_cart", outOfStockAt: "2030-01-02T09:00:00Z"),
        ]
        let progress = ShoppingCartProgress(items: items)
        // Apples, soup and salt count; the oranges are out of stock and the bread is bought.
        XCTAssertEqual(progress.inCartCount, 2)
        XCTAssertEqual(progress.totalCount, 3)
        XCTAssertEqual(progress.soFar?.amount, Decimal(string: "5.34"))
    }

    func testOtherShoppersAreFoundByStoreAndNotCountedTwice() throws {
        let json = #"""
            [{"id":"t1","userId":"user-2","shopperName":"Sam","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:00:00Z"},
             {"id":"t2","userId":"user-2","shopperName":"Sam","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-2"],"startedAt":"2030-01-02T09:05:00Z"},
             {"id":"t3","userId":"user-1","shopperName":"Robin","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:06:00Z"},
             {"id":"t4","userId":"user-3","shopperName":"Alex","storeName":"Corner Grocer","storeKey":"cornergrocer","shoppingListIds":["list-1"],"startedAt":"2030-01-02T09:07:00Z"},
             {"id":"t5","userId":"user-4","shopperName":"Kim","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1"],"startedAt":"2030-01-02T08:00:00Z","finishedAt":"2030-01-02T08:30:00Z"}]
            """#
        let trips = try JSONDecoder().decode([ShoppingTrip].self, from: Data(json.utf8))
        XCTAssertEqual(trips.otherShoppers(atStoreKey: "examplemarket", excludingUserId: "user-1"), ["Sam"])
        XCTAssertEqual(trips.ownTrip(atStoreKey: "examplemarket", userId: "user-1")?.id, "t3")
        XCTAssertNil(trips.ownTrip(atStoreKey: "cornergrocer", userId: "user-1"))
        XCTAssertEqual(trips.openTrips(ofListId: "list-2", excludingUserId: "user-1").map(\.id), ["t2"])
    }

    // MARK: Importing

    func testAnImportPreviewMarksWhatTheListHasAndAddsWhatIsKept() async throws {
        let json = #"""
            {"data":{"PreviewShoppingImport":{"shoppingItems":[
              {"itemDescription":"Apples","itemDetails":"","neededQuantity":"6","quantityUnit":"","aisleName":"Produce","storeName":"","isStoreBrand":false,
               "mergedShoppingItemId":"item-1","mergedShoppingItemState":"needed","mergedNeededQuantity":"4"},
              {"itemDescription":"Bread","itemDetails":"Rye","neededQuantity":"","quantityUnit":"","aisleName":"Bakery","storeName":"Corner Grocer","isStoreBrand":true,
               "mergedShoppingItemId":"","mergedShoppingItemState":"","mergedNeededQuantity":""}]}}}
            """#
        let (connection, fake) = connection(answering: json)
        let preview = try await connection.previewShoppingImport(shoppingListId: "list-1", importText: "  apples\nbread \n", agentAttachmentIds: ["attachment-1"])
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["shoppingListId"], .string("list-1"))
        XCTAssertEqual(variables["importText"], .string("apples\nbread"))
        XCTAssertEqual(variables["agentAttachmentIds"], .array([.string("attachment-1")]))

        var drafts = ShoppingImportDraft.drafts(of: preview)
        XCTAssertEqual(drafts.map(\.item.isOnList), [true, false])
        XCTAssertEqual(drafts[0].item.shownTitle, "6 Apples")
        XCTAssertEqual(drafts[1].item.shownDetail, "Corner Grocer · Bakery · Rye")
        drafts[0].isIncluded = false
        let entries = ShoppingImportDraft.entries(of: drafts)
        XCTAssertEqual(entries.count, 1)
        // An empty count is one, and what is empty is left out of the input.
        XCTAssertEqual(
            entries[0].variables,
            .object([
                "itemDescription": .string("Bread"), "itemDetails": .string("Rye"), "storeName": .string("Corner Grocer"),
                "isStoreBrand": .bool(true), "aisleName": .string("Bakery"), "neededQuantity": .string("1"),
            ]))
    }

    // MARK: Receipts

    func testAReceiptIsSortedIntoWhatItCheckedOffProposedAndLeftOver() async throws {
        let checked = itemJSON("item-1", "Frozen chili", store: "Corner Grocer")
        let proposed = itemJSON("item-2", "Soup", listId: "list-2", listName: "Party supplies")
        let still = itemJSON("item-3", "Paper towels")
        let json = #"""
            {"data":{"ListShoppingReceiptMatches":{"financeReceiptId":"receipt-1","merchantName":"Corner Grocer","purchasedOn":"2030-01-02","totalAmount":"14.53","currencyCode":"USD",
              "shoppingReceiptLines":[
                {"financeReceiptLineId":"line-1","lineNumber":1,"description":"FRZN CHILI","quantity":"1","lineAmount":"4.50","shoppingReceiptMatches":[
                  {"shoppingReceiptMatch":{"id":"match-1","financeReceiptId":"receipt-1","financeReceiptLineId":"line-1","shoppingItemId":"item-1","receiptMatchState":"applied","shoppingItemPriceId":"price-1"},"shoppingItem":\#(checked)}]},
                {"financeReceiptLineId":"line-2","lineNumber":2,"description":"SOUP","quantity":"2","lineAmount":"3.98","shoppingReceiptMatches":[
                  {"shoppingReceiptMatch":{"id":"match-2","financeReceiptId":"receipt-1","financeReceiptLineId":"line-2","shoppingItemId":"item-2","receiptMatchState":"proposed"},"shoppingItem":\#(proposed)}]},
                {"financeReceiptLineId":"line-3","lineNumber":3,"description":"DISH SOAP","quantity":"1","lineAmount":"3.25","shoppingReceiptMatches":[
                  {"shoppingReceiptMatch":{"id":"match-3","financeReceiptId":"receipt-1","financeReceiptLineId":"line-3","shoppingItemId":"item-2","receiptMatchState":"dismissed"},"shoppingItem":\#(proposed)}]},
                {"financeReceiptLineId":"line-4","lineNumber":4,"description":"BREAD","quantity":"1","lineAmount":"2.80","shoppingReceiptMatches":[]}],
              "stillNeededItems":[\#(still)]}}}
            """#
        let (connection, fake) = connection(answering: json)
        let receipt = try await connection.shoppingReceipt(financeReceiptId: "receipt-1")
        XCTAssertEqual(try sentVariables(fake.sent[0])["financeReceiptId"], .string("receipt-1"))
        XCTAssertEqual(receipt.merchantName, "Corner Grocer")

        let sections = ShoppingReceiptSections(receipt: receipt)
        XCTAssertEqual(sections.appliedCount, 1)
        XCTAssertEqual(sections.appliedGroups.map(\.listName), ["Groceries"])
        XCTAssertEqual(sections.appliedGroups[0].entries[0].line.description, "FRZN CHILI")
        XCTAssertEqual(sections.appliedGroups[0].entries[0].match.shoppingItemPriceId, "price-1")
        XCTAssertEqual(sections.proposed.map(\.match.id), ["match-2"])
        // A dismissed match leaves its line on no list, like a line with no match.
        XCTAssertEqual(sections.notOnList.map(\.description), ["DISH SOAP", "BREAD"])
        XCTAssertEqual(sections.stillNeeded.map(\.itemDescription), ["Paper towels"])
        XCTAssertTrue(sections.canUndo)
        XCTAssertFalse(sections.isEmpty)
    }

    func testDecidingAMatchSendsTheAnswer() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"DecideShoppingReceiptMatch":{"id":"match-2","receiptMatchState":"applied"}}}"#)
        let match = try await connection.decideShoppingReceiptMatch(shoppingReceiptMatchId: "match-2", isApplied: true)
        XCTAssertTrue(match.isApplied)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["shoppingReceiptMatchId"], .string("match-2"))
        XCTAssertEqual(variables["isApplied"], .bool(true))
    }

    // MARK: What each call sends

    func testChangingAnItemSendsOnlyWhatIsGiven() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateShoppingItem":\#(itemJSON("item-1", "Pears"))}}"#)
        let changed = try await connection.updateShoppingItem(shoppingItemId: "item-1", itemDescription: "Pears", isStoreBrand: false, neededQuantity: "3")
        XCTAssertEqual(changed.itemDescription, "Pears")
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["shoppingItemId"], .string("item-1"))
        XCTAssertEqual(variables["itemDescription"], .string("Pears"))
        XCTAssertEqual(variables["neededQuantity"], .string("3"))
        XCTAssertEqual(variables["isStoreBrand"], .bool(false))
        XCTAssertNil(variables["storeName"])
        XCTAssertNil(variables["aisleName"])
    }

    func testCheckingNeedingAndMarkingOutOfStockSendTheirArguments() async throws {
        let answer = itemJSON("item-1", "Pears")
        let (checking, checkFake) = connection(answering: #"{"data":{"CheckShoppingItem":\#(answer)}}"#)
        try await checking.checkShoppingItem(shoppingItemId: "item-1", isChecked: true)
        XCTAssertEqual(try sentVariables(checkFake.sent[0])["isChecked"], .bool(true))
        XCTAssertNil(try sentVariables(checkFake.sent[0])["storeName"])

        // Checking at a store names it, for the trip the first check starts.
        let (atStore, atStoreFake) = connection(answering: #"{"data":{"CheckShoppingItem":\#(answer)}}"#)
        try await atStore.checkShoppingItem(shoppingItemId: "item-1", isChecked: true, storeName: "Example Market")
        XCTAssertEqual(try sentVariables(atStoreFake.sent[0])["storeName"], .string("Example Market"))

        let (needing, needFake) = connection(answering: #"{"data":{"SetShoppingItemNeed":\#(answer)}}"#)
        try await needing.setShoppingItemNeed(shoppingItemId: "item-1", isNeeded: true, neededQuantity: "2")
        let needed = try sentVariables(needFake.sent[0])
        XCTAssertEqual(needed["isNeeded"], .bool(true))
        XCTAssertEqual(needed["neededQuantity"], .string("2"))

        let (stocking, stockFake) = connection(answering: #"{"data":{"SetShoppingItemOutOfStock":\#(answer)}}"#)
        try await stocking.setShoppingItemOutOfStock(shoppingItemId: "item-1", isOutOfStock: true, storeName: "Example Market")
        let stocked = try sentVariables(stockFake.sent[0])
        XCTAssertEqual(stocked["isOutOfStock"], .bool(true))
        XCTAssertEqual(stocked["storeName"], .string("Example Market"))
    }

    func testStartingAndFinishingShoppingSendTheStoreAndTheLists() async throws {
        let trip = #"{"id":"trip-1","userId":"user-1","shopperName":"Robin","storeName":"Example Market","storeKey":"examplemarket","shoppingListIds":["list-1","list-2"],"startedAt":"2030-01-02T09:00:00Z"}"#
        let (starting, startFake) = connection(answering: #"{"data":{"StartShopping":\#(trip)}}"#)
        let started = try await starting.startShopping(storeName: "Example Market", shoppingListIds: ["list-1", "list-2"])
        XCTAssertEqual(started.id, "trip-1")
        let variables = try sentVariables(startFake.sent[0])
        XCTAssertEqual(variables["storeName"], .string("Example Market"))
        XCTAssertEqual(variables["shoppingListIds"], .array([.string("list-1"), .string("list-2")]))

        // No store in particular leaves the name out.
        let (anywhere, anywhereFake) = connection(answering: #"{"data":{"StartShopping":\#(trip)}}"#)
        try await anywhere.startShopping(storeName: "", shoppingListIds: ["list-1"])
        XCTAssertNil(try sentVariables(anywhereFake.sent[0])["storeName"])

        let (finishing, finishFake) = connection(answering: #"{"data":{"FinishShopping":\#(trip)}}"#)
        try await finishing.finishShopping(shoppingTripId: "trip-1")
        XCTAssertEqual(try sentVariables(finishFake.sent[0])["shoppingTripId"], .string("trip-1"))

        // Done with no trip named buys the cart all the same.
        let (done, doneFake) = connection(answering: #"{"data":{"FinishShopping":\#(trip)}}"#)
        try await done.finishShopping()
        XCTAssertNil(try sentVariables(doneFake.sent[0])["shoppingTripId"])
    }

    func testTheStoreScreenAsksForTheListsIncludedAndAnyStore() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"ListShoppingStoreItems":{"storeName":"Example Market","isReminded":true,"shoppingItems":[],"shoppingLists":[],"shoppingTrips":[]}}}"#)
        _ = try await connection.shoppingStoreItems(storeName: "Example Market", shoppingListIds: ["list-1"])
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["storeName"], .string("Example Market"))
        XCTAssertEqual(variables["shoppingListIds"], .array([.string("list-1")]))
        XCTAssertEqual(variables["isAnyStoreIncluded"], .bool(true))
        XCTAssertTrue(try sentQuery(fake.sent[0]).contains("ListShoppingStoreItems"))
    }

    func testSharingAListNamesAPersonOrACircle() async throws {
        let share = #"{"id":"share-1","shoppingListId":"list-1","listName":"Groceries","granteeCircleId":"circle-1","granteeCircleName":"Home"}"#
        let (connection, fake) = connection(answering: #"{"data":{"ShareShoppingList":\#(share)}}"#)
        let made = try await connection.shareShoppingList(shoppingListId: "list-1", granteeUserId: nil, granteeCircleId: "circle-1")
        XCTAssertEqual(made.shownGrantee, "Home")
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["granteeUserId"], .null)
        XCTAssertEqual(variables["granteeCircleId"], .string("circle-1"))
    }

    func testAddingItemsSendsEachEntryAndReadsWhetherItMerged() async throws {
        let answer = #"{"data":{"AddShoppingItems":[{"isMerged":true,"shoppingItem":\#(itemJSON("item-1", "Apples"))},{"isMerged":false,"shoppingItem":\#(itemJSON("item-2", "Soup"))}]}}"#
        let (connection, fake) = connection(answering: answer)
        let added = try await connection.addShoppingItems(
            shoppingListId: "list-1",
            entries: [ShoppingItemEntry(itemDescription: "Apples", neededQuantity: "6"), ShoppingItemEntry(itemDescription: "Soup", storeName: "Example Market")])
        XCTAssertEqual(added.map(\.isMerged), [true, false])
        XCTAssertEqual(added.compactMap(\.shoppingItem?.itemDescription), ["Apples", "Soup"])
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["shoppingListId"], .string("list-1"))
        guard case .array(let entries)? = variables["shoppingItems"] else { return XCTFail("no entries") }
        XCTAssertEqual(entries.count, 2)
        XCTAssertEqual(entries[1]["storeName"], .string("Example Market"))
    }

    func testAPictureIsGivenByAttachmentOrTakenAway() async throws {
        let answer = #"{"data":{"SetShoppingItemPicture":\#(itemJSON("item-1", "Apples"))}}"#
        let (connection, fake) = connection(answering: answer)
        try await connection.setShoppingItemPicture(shoppingItemId: "item-1", agentAttachmentId: "attachment-1")
        try await connection.setShoppingItemPicture(shoppingItemId: "item-1", agentAttachmentId: nil)
        XCTAssertEqual(try sentVariables(fake.sent[0])["agentAttachmentId"], .string("attachment-1"))
        XCTAssertEqual(try sentVariables(fake.sent[1])["agentAttachmentId"], .null)
    }

    // MARK: The rest of the app

    func testAShoppingNotificationCarriesTheStoreOrTheList() throws {
        let arrival = #"{"notificationKind":"shopping","shoppingStoreName":"Example Market","title":"At Example Market","body":"4 things on your lists","threadId":"shopping"}"#
        let content = try JSONDecoder().decode(NotificationContent.self, from: Data(arrival.utf8))
        XCTAssertEqual(content.notificationKind, "shopping")
        XCTAssertEqual(content.shoppingStoreName, "Example Market")
        XCTAssertNil(content.shoppingListId)
        let started = #"{"notificationKind":"shopping","shoppingListId":"list-1","title":"Sam started shopping Groceries","body":"","threadId":"shopping"}"#
        XCTAssertEqual(try JSONDecoder().decode(NotificationContent.self, from: Data(started.utf8)).shoppingListId, "list-1")
        XCTAssertTrue(AgentPhone.allNotificationKinds.contains("shopping"))
    }

    func testShoppingListsAreAKindOfShareAndTheirRunsAreKnown() {
        XCTAssertEqual(SharingKind(rawValue: "shopping_list"), .shoppingList)
        let made = SharingMade(shareKind: "shopping_list", shareId: "share-1", subjectId: "list-1", subjectName: "Groceries")
        XCTAssertEqual(made.knownKind, .shoppingList)
        XCTAssertTrue(made.canStop)
        for kind in ["match_shopping", "shopping_picture", "shopping_import"] {
            XCTAssertTrue(AgentRunSummary.jobKinds.contains(kind), kind)
        }
    }

    func testAnEventNamesTheListThatChanged() throws {
        let event = try JSONDecoder().decode(ShoppingEvent.self, from: Data(#"{"shoppingListId":"list-1"}"#.utf8))
        XCTAssertEqual(event.shoppingListId, "list-1")
    }
}
