#if DEBUG
    import Foundation
    import TeaNodeKit
    import UIKit

    // The invented lists, items, prices and receipt that the screenshots of the
    // Shopping screens photograph (`-TeaNodeDemo -TeaNodeTab shopping`). Every
    // store, product and person is made up, and the pictures are drawn here
    // since no server is behind the demo. Changes are answered with the item as
    // it would then be, but nothing is kept: each read starts from these.

    /// ShoppingDemo holds the invented lists of the demo person.
    enum ShoppingDemo {
        static let groceriesListId = "list-groceries"
        static let partyListId = "list-party"
        static let exampleMarket = "Example Market"
        static let cornerGrocer = "Corner Grocer"
        static let receiptId = "receipt-corner-grocer"

        /// One invented item on one of the two lists.
        struct Item {
            let id: String
            let listId: String
            let description: String
            var details = ""
            var store = ""
            var isStoreBrand = false
            var aisle = ""
            var aisleRank = 0
            var rank = 0
            var quantity = "1"
            var unit = ""
            var price = ""
            var state = "needed"
            var cartedBy = ""
            var outOfStockBy = ""
            var boughtDaysAgo: Int?
            var hasPicture = true

            var listName: String { listId == ShoppingDemo.groceriesListId ? "Groceries" : "Party supplies" }

            var dictionary: [String: Any] {
                var object: [String: Any] = [
                    "id": id, "shoppingListId": listId, "listName": listName, "itemDescription": description, "itemDetails": details,
                    "storeName": store, "storeKey": ShoppingStoreKey.key(of: store), "isStoreBrand": isStoreBrand, "aisleName": aisle,
                    "aisleRank": aisleRank, "itemRank": rank, "neededQuantity": quantity, "quantityUnit": unit, "unitPriceAmount": price,
                    "currencyCode": price.isEmpty ? "" : "USD", "shoppingItemState": state, "cartedByUserId": cartedBy.isEmpty ? "" : "alex",
                    "cartedByName": cartedBy, "outOfStockByUserId": outOfStockBy.isEmpty ? "" : "alex", "outOfStockByName": outOfStockBy,
                    "outOfStockStoreName": outOfStockBy.isEmpty ? "" : store, "pictureFileId": hasPicture ? "demo" : "",
                    "picturePageUrl": "", "picturePath": hasPicture ? "/api/v1/shopping/pictures/\(id)?v=demo" : "",
                    "pictureSource": hasPicture ? "web" : "",
                ]
                if !cartedBy.isEmpty { object["cartedAt"] = DemoServer.demoTime(daysAgo: 0, hour: 9, minute: 5) }
                if !outOfStockBy.isEmpty { object["outOfStockAt"] = DemoServer.demoTime(daysAgo: 0, hour: 9, minute: 10) }
                if let boughtDaysAgo { object["lastBoughtAt"] = DemoServer.demoTime(daysAgo: boughtDaysAgo, hour: 17, minute: 30) }
                return object
            }
        }

        static let items: [Item] = [
            Item(
                id: "item-apples", listId: groceriesListId, description: "Apples", details: "Fuji preferred", store: exampleMarket, aisle: "Produce",
                aisleRank: 1, rank: 1, quantity: "6", price: "0.89"),
            Item(
                id: "item-oranges", listId: groceriesListId, description: "Navel oranges", store: exampleMarket, aisle: "Produce", aisleRank: 1,
                rank: 2, quantity: "4", price: "0.79", outOfStockBy: "Alex"),
            Item(
                id: "item-tomatoes", listId: groceriesListId, description: "Tomatoes", details: "On the vine", store: exampleMarket, aisle: "Produce",
                aisleRank: 1, rank: 3, quantity: "1", unit: "lb", price: "2.49", state: "in_cart", cartedBy: "Alex"),
            Item(
                id: "item-soup", listId: groceriesListId, description: "Soup", details: "Chicken noodle", store: exampleMarket, isStoreBrand: true,
                aisle: "Canned goods", aisleRank: 2, rank: 4, quantity: "2", unit: "cans", price: "1.99"),
            Item(
                id: "item-chili", listId: groceriesListId, description: "Frozen chili", store: cornerGrocer, aisle: "Frozen", aisleRank: 1, rank: 5,
                quantity: "1", price: "4.50"),
            Item(
                id: "item-towels", listId: groceriesListId, description: "Paper towels", aisle: "Household", aisleRank: 3, rank: 6, quantity: "1",
                unit: "pack", price: "6.99"),
            Item(
                id: "item-bananas", listId: groceriesListId, description: "Bananas", store: exampleMarket, aisle: "Produce", aisleRank: 1, rank: 7,
                quantity: "1", unit: "bunch", price: "1.20", state: "not_needed", boughtDaysAgo: 3),
            Item(
                id: "item-olive-oil", listId: groceriesListId, description: "Olive oil", aisle: "Pantry", aisleRank: 4, rank: 8, quantity: "1",
                price: "8.50", state: "not_needed", boughtDaysAgo: 12, hasPicture: false),
            Item(
                id: "item-plates", listId: partyListId, description: "Paper plates", store: exampleMarket, aisle: "Household", aisleRank: 3, rank: 1,
                quantity: "2", unit: "packs", price: "3.25"),
            Item(
                id: "item-balloons", listId: partyListId, description: "Balloons", aisle: "Party", aisleRank: 5, rank: 2, quantity: "20",
                price: "0.15", hasPicture: false),
        ]

        static func items(ofList listId: String) -> [Item] { items.filter { $0.listId == listId } }

        static func count(_ listId: String, state: String) -> Int { items(ofList: listId).filter { $0.state == state }.count }

        static func json(_ object: Any) -> String {
            guard let data = try? JSONSerialization.data(withJSONObject: object, options: [.fragmentsAllowed]), let text = String(data: data, encoding: .utf8) else { return "null" }
            return text
        }

        static func listDictionary(_ listId: String) -> [String: Any] {
            let isOwn = listId == groceriesListId
            return [
                "id": listId, "userId": isOwn ? "robin" : "sam", "listName": isOwn ? "Groceries" : "Party supplies",
                "ownerName": isOwn ? "Robin" : "Sam", "ownerUsername": isOwn ? "robin" : "sam", "isOwner": isOwn, "isShared": true,
                "isReminded": true, "neededCount": count(listId, state: "needed"), "inCartCount": count(listId, state: "in_cart"),
                "notNeededCount": count(listId, state: "not_needed"), "createdAt": "2026-09-20T09:00:00Z", "updatedAt": "2026-10-20T09:00:00Z",
            ]
        }

        static var alexTrip: [String: Any] {
            [
                "id": "trip-alex", "userId": "alex", "shopperName": "Alex", "shopperUsername": "alex", "storeName": exampleMarket,
                "storeKey": ShoppingStoreKey.key(of: exampleMarket), "shoppingListIds": [groceriesListId, partyListId],
                "startedAt": DemoServer.demoTime(daysAgo: 0, hour: 9, minute: 0),
            ]
        }

        static func tripDictionary(storeName: String, isFinished: Bool) -> [String: Any] {
            var object: [String: Any] = [
                "id": "trip-robin", "userId": "robin", "shopperName": "Robin", "shopperUsername": "robin", "storeName": storeName,
                "storeKey": ShoppingStoreKey.key(of: storeName), "shoppingListIds": [groceriesListId],
                "startedAt": DemoServer.demoTime(daysAgo: 0, hour: 9, minute: 30),
            ]
            if isFinished { object["finishedAt"] = DemoServer.demoTime(daysAgo: 0, hour: 10, minute: 0) }
            return object
        }

        /// The store screen: the items of the lists included that are for the store or for any store.
        static func storeView(storeName: String, listIds: [String]) -> [String: Any] {
            let key = ShoppingStoreKey.key(of: storeName)
            let atStore = items.filter { $0.state != "not_needed" && ($0.store.isEmpty || ShoppingStoreKey.key(of: $0.store) == key) }
            var lists: [[String: Any]] = []
            for listId in [groceriesListId, partyListId] where atStore.contains(where: { $0.listId == listId }) {
                let listItems = atStore.filter { $0.listId == listId }
                lists.append([
                    "shoppingListId": listId, "listName": listId == groceriesListId ? "Groceries" : "Party supplies",
                    "neededCount": listItems.filter { $0.state == "needed" }.count, "isIncluded": listIds.isEmpty || listIds.contains(listId),
                ])
            }
            let shown = atStore.filter { listIds.isEmpty || listIds.contains($0.listId) }
            return [
                "storeName": storeName, "isReminded": true, "shoppingItems": shown.map(\.dictionary), "shoppingLists": lists,
                "shoppingTrips": [alexTrip],
            ]
        }

        static func priceDictionaries(itemId: String) -> [[String: Any]] {
            guard let item = items.first(where: { $0.id == itemId }) else { return [] }
            let base = Double(item.price) ?? 1
            let history: [(daysAgo: Int, factor: Double, source: String)] = [
                (5, 1.0, "receipt"), (40, 1.04, "receipt"), (75, 0.92, "person"), (110, 0.89, "receipt"), (150, 0.97, "person"),
            ]
            let calendar = Calendar.current
            let formatter = DateFormatter()
            formatter.locale = Locale(identifier: "en_US_POSIX")
            formatter.dateFormat = "yyyy-MM-dd"
            return history.enumerated().map { index, entry in
                let day = calendar.date(byAdding: .day, value: -entry.daysAgo, to: Date()) ?? Date()
                let store = item.store.isEmpty ? exampleMarket : item.store
                return [
                    "id": "price-\(itemId)-\(index)", "shoppingItemId": itemId, "storeName": store, "storeKey": ShoppingStoreKey.key(of: store),
                    "unitPriceAmount": String(format: "%.2f", base * entry.factor), "currencyCode": "USD", "quantity": "1", "quantityUnit": "",
                    "observedOn": formatter.string(from: day), "priceSource": entry.source,
                    "financeReceiptId": entry.source == "receipt" ? receiptId : "", "createdAt": CalendarDates.text(day),
                ]
            }
        }

        static func match(id: String, lineId: String, itemId: String, state: String) -> [String: Any] {
            [
                "id": id, "financeReceiptId": receiptId, "financeReceiptLineId": lineId, "shoppingItemId": itemId, "receiptMatchState": state,
                "matchedQuantity": "1", "previousShoppingItemState": "needed", "previousNeededQuantity": "1",
                "shoppingItemPriceId": state == "applied" ? "price-\(itemId)-0" : "",
            ]
        }

        /// A receipt from the second store: one line checked off, one proposed, two on no list, and one thing still to buy.
        static var receiptView: [String: Any] {
            func itemDictionary(_ itemId: String) -> [String: Any] { items.first { $0.id == itemId }?.dictionary ?? [:] }
            let lines: [[String: Any]] = [
                [
                    "financeReceiptLineId": "line-chili", "lineNumber": 1, "description": "FRZN CHILI 15OZ", "quantity": "1", "lineAmount": "4.50",
                    "shoppingReceiptMatches": [
                        ["shoppingReceiptMatch": match(id: "match-chili", lineId: "line-chili", itemId: "item-chili", state: "applied"),
                            "shoppingItem": itemDictionary("item-chili")]
                    ],
                ],
                [
                    "financeReceiptLineId": "line-soup", "lineNumber": 2, "description": "SOUP CHKN NDL", "quantity": "2", "lineAmount": "3.98",
                    "shoppingReceiptMatches": [
                        ["shoppingReceiptMatch": match(id: "match-soup", lineId: "line-soup", itemId: "item-soup", state: "proposed"),
                            "shoppingItem": itemDictionary("item-soup")]
                    ],
                ],
                [
                    "financeReceiptLineId": "line-soap", "lineNumber": 3, "description": "DISH SOAP LEMON", "quantity": "1", "lineAmount": "3.25",
                    "shoppingReceiptMatches": [],
                ],
                [
                    "financeReceiptLineId": "line-bread", "lineNumber": 4, "description": "BREAD RYE SLCD", "quantity": "1", "lineAmount": "2.80",
                    "shoppingReceiptMatches": [],
                ],
            ]
            return [
                "financeReceiptId": receiptId, "merchantName": cornerGrocer, "purchasedOn": "2026-10-24", "totalAmount": "14.53",
                "currencyCode": "USD", "shoppingReceiptLines": lines, "stillNeededItems": [itemDictionary("item-towels")],
            ]
        }

        /// What the import preview found: four items the list has already and two it has not.
        static var importItems: [[String: Any]] {
            [
                importItem("Apples", quantity: "6", aisle: "Produce", mergedItemId: "item-apples", mergedQuantity: "6"),
                importItem("Tomatoes", quantity: "1", aisle: "Produce", mergedItemId: "item-tomatoes", mergedState: "in_cart", mergedQuantity: "1"),
                importItem("Sourdough loaf", quantity: "1", aisle: "Bakery"),
                importItem("Soup", details: "2 cans", quantity: "2", unit: "cans", aisle: "Canned goods", mergedItemId: "item-soup", mergedQuantity: "2"),
                importItem("Navel oranges", quantity: "4", aisle: "Produce", mergedItemId: "item-oranges", mergedQuantity: "4"),
                importItem("Greek yogurt", details: "Plain, large tub", quantity: "1", aisle: "Dairy"),
            ]
        }

        static func importItem(
            _ description: String, details: String = "", quantity: String, unit: String = "", aisle: String, mergedItemId: String = "",
            mergedState: String = "needed", mergedQuantity: String = ""
        ) -> [String: Any] {
            [
                "itemDescription": description, "itemDetails": details, "neededQuantity": quantity, "quantityUnit": unit, "aisleName": aisle,
                "storeName": "", "isStoreBrand": false, "mergedShoppingItemId": mergedItemId,
                "mergedShoppingItemState": mergedItemId.isEmpty ? "" : mergedState, "mergedNeededQuantity": mergedQuantity,
            ]
        }
    }

    extension DemoServer {
        /// What the Shopping screens ask, or nil for any other question.
        static func shoppingAnswer(query: String, body: Data?) -> String? {
            guard query.contains("Shopping") else { return nil }
            let variables = requestVariables(body)
            let itemId = variables["shoppingItemId"] as? String ?? ""
            func answer(_ operation: String, _ value: Any) -> String {
                #"{"data":{"\#(operation)":\#(ShoppingDemo.json(value))}}"#
            }
            func changedItem(_ change: (inout [String: Any]) -> Void) -> [String: Any] {
                var item = ShoppingDemo.items.first { $0.id == itemId }?.dictionary ?? ShoppingDemo.items[0].dictionary
                change(&item)
                return item
            }

            // Reading.
            if query.contains("ListShoppingListShares") {
                return answer(
                    "ListShoppingListShares",
                    [
                        [
                            "id": "share-home", "shoppingListId": ShoppingDemo.groceriesListId, "listName": "Groceries", "ownerUserId": "robin",
                            "ownerUsername": "robin", "ownerName": "Robin", "granteeUserId": "", "granteeUsername": "", "granteeName": "",
                            "granteeCircleId": "circle-home", "granteeCircleName": "Home", "createdAt": "2026-09-21T09:00:00Z",
                        ]
                    ])
            }
            if query.contains("ListShoppingLists") {
                return answer("ListShoppingLists", [ShoppingDemo.listDictionary(ShoppingDemo.groceriesListId), ShoppingDemo.listDictionary(ShoppingDemo.partyListId)])
            }
            if query.contains("GetShoppingList(") {
                let listId = variables["shoppingListId"] as? String ?? ShoppingDemo.groceriesListId
                let trips: [[String: Any]] = listId == ShoppingDemo.groceriesListId || listId == ShoppingDemo.partyListId ? [ShoppingDemo.alexTrip] : []
                let page: [String: Any] = [
                    "totalCount": ShoppingDemo.items(ofList: listId).count, "shoppingList": ShoppingDemo.listDictionary(listId),
                    "shoppingItems": ShoppingDemo.items(ofList: listId).map(\.dictionary), "shoppingTrips": trips,
                ]
                return answer("GetShoppingList", page)
            }
            if query.contains("GetShoppingItem(") { return answer("GetShoppingItem", changedItem { _ in }) }
            if query.contains("ListShoppingStoreItems") {
                let listIds = variables["shoppingListIds"] as? [String] ?? []
                return answer("ListShoppingStoreItems", ShoppingDemo.storeView(storeName: variables["storeName"] as? String ?? "", listIds: listIds))
            }
            if query.contains("ListShoppingStores") {
                return answer(
                    "ListShoppingStores",
                    [
                        ["storeKey": "examplemarket", "storeName": ShoppingDemo.exampleMarket, "neededCount": 4, "isReminded": true],
                        ["storeKey": "cornergrocer", "storeName": ShoppingDemo.cornerGrocer, "neededCount": 1, "isReminded": true],
                    ])
            }
            if query.contains("ListOpenShoppingTrips") { return answer("ListOpenShoppingTrips", [ShoppingDemo.alexTrip]) }
            if query.contains("ListShoppingItemPrices") { return answer("ListShoppingItemPrices", ShoppingDemo.priceDictionaries(itemId: itemId)) }
            if query.contains("ListShoppingReceiptMatches") { return answer("ListShoppingReceiptMatches", ShoppingDemo.receiptView) }
            if query.contains("PreviewShoppingImport") { return answer("PreviewShoppingImport", ["shoppingItems": ShoppingDemo.importItems]) }

            // Changing.
            if query.contains("CreateShoppingList") || query.contains("RenameShoppingList") {
                var list = ShoppingDemo.listDictionary(ShoppingDemo.groceriesListId)
                list["listName"] = variables["listName"] as? String ?? "Groceries"
                if query.contains("CreateShoppingList") { list["id"] = "list-new" }
                return answer(query.contains("CreateShoppingList") ? "CreateShoppingList" : "RenameShoppingList", list)
            }
            if query.contains("ShareShoppingList") && !query.contains("UnshareShoppingList") {
                return answer(
                    "ShareShoppingList",
                    [
                        "id": "share-new", "shoppingListId": ShoppingDemo.groceriesListId, "listName": "Groceries", "ownerUserId": "robin",
                        "ownerUsername": "robin", "ownerName": "Robin", "granteeUserId": variables["granteeUserId"] as? String ?? "",
                        "granteeUsername": "", "granteeName": "", "granteeCircleId": variables["granteeCircleId"] as? String ?? "",
                        "granteeCircleName": "", "createdAt": CalendarDates.text(Date()),
                    ])
            }
            for name in [
                "DeleteShoppingList", "UnshareShoppingList", "SetShoppingListReminder", "SetShoppingStoreReminder", "FindShoppingItemPicture",
                "MatchShoppingReceipt", "UndoShoppingReceipt",
            ] where query.contains(name) {
                return answer(name, true)
            }
            if query.contains("RemoveShoppingItems") { return answer("RemoveShoppingItems", 1) }
            if query.contains("AddShoppingItemFromReceiptLine") { return answer("AddShoppingItemFromReceiptLine", changedItem { $0["shoppingItemState"] = "not_needed" }) }
            if query.contains("AddShoppingItems") {
                let entries = variables["shoppingItems"] as? [[String: Any]] ?? []
                let added = entries.enumerated().map { index, entry -> [String: Any] in
                    var item = ShoppingDemo.items[0].dictionary
                    item["id"] = "item-added-\(index)"
                    item["itemDescription"] = entry["itemDescription"] as? String ?? ""
                    item["picturePath"] = ""
                    return ["isMerged": false, "shoppingItem": item]
                }
                return answer("AddShoppingItems", added)
            }
            if query.contains("UpdateShoppingItem") {
                return answer("UpdateShoppingItem", changedItem { item in
                    for key in ["itemDescription", "itemDetails", "storeName", "isStoreBrand", "aisleName", "neededQuantity", "quantityUnit"] {
                        if let value = variables[key] { item[key] = value }
                    }
                })
            }
            if query.contains("CheckShoppingItem") {
                let isChecked = variables["isChecked"] as? Bool ?? true
                return answer("CheckShoppingItem", changedItem { $0["shoppingItemState"] = isChecked ? "in_cart" : "needed" })
            }
            if query.contains("SetShoppingItemNeed") {
                let isNeeded = variables["isNeeded"] as? Bool ?? true
                return answer("SetShoppingItemNeed", changedItem {
                    $0["shoppingItemState"] = isNeeded ? "needed" : "not_needed"
                    if let quantity = variables["neededQuantity"] { $0["neededQuantity"] = quantity }
                })
            }
            if query.contains("SetShoppingItemOutOfStock") {
                let isOut = variables["isOutOfStock"] as? Bool ?? true
                return answer("SetShoppingItemOutOfStock", changedItem {
                    if isOut {
                        $0["outOfStockAt"] = CalendarDates.text(Date())
                        $0["outOfStockByName"] = "Robin"
                    } else {
                        $0["outOfStockAt"] = NSNull()
                        $0["outOfStockByName"] = ""
                    }
                })
            }
            if query.contains("RecordShoppingItemPrice") { return answer("RecordShoppingItemPrice", ["id": "price-new"]) }
            if query.contains("SetShoppingItemPicture") { return answer("SetShoppingItemPicture", changedItem { _ in }) }
            if query.contains("StartShopping") {
                return answer("StartShopping", ShoppingDemo.tripDictionary(storeName: variables["storeName"] as? String ?? "", isFinished: false))
            }
            if query.contains("FinishShopping") {
                return answer("FinishShopping", ShoppingDemo.tripDictionary(storeName: ShoppingDemo.exampleMarket, isFinished: true))
            }
            if query.contains("DecideShoppingReceiptMatch") {
                let isApplied = variables["isApplied"] as? Bool ?? true
                return answer(
                    "DecideShoppingReceiptMatch",
                    ShoppingDemo.match(id: variables["shoppingReceiptMatchId"] as? String ?? "match-soup", lineId: "line-soup", itemId: "item-soup", state: isApplied ? "applied" : "dismissed"))
            }
            return nil
        }

        /// An item's picture, for a read of an address under `/api/v1/shopping/pictures/`: a
        /// round shape on a color of the item's own, drawn here.
        static func shoppingPicture(for request: URLRequest) -> Data? {
            guard let path = request.url?.path, path.hasPrefix("/api/v1/shopping/pictures/") else { return nil }
            let hue = Double(path.unicodeScalars.reduce(0) { ($0 + 7 * Int($1.value)) % 360 }) / 360
            let side: CGFloat = 160
            let image = UIGraphicsImageRenderer(size: CGSize(width: side, height: side)).image { context in
                let drawing = context.cgContext
                drawing.setFillColor(UIColor(hue: hue, saturation: 0.18, brightness: 0.96, alpha: 1).cgColor)
                drawing.fill(CGRect(x: 0, y: 0, width: side, height: side))
                drawing.setFillColor(UIColor(hue: hue, saturation: 0.55, brightness: 0.82, alpha: 1).cgColor)
                drawing.fillEllipse(in: CGRect(x: side * 0.2, y: side * 0.26, width: side * 0.6, height: side * 0.6))
                drawing.setFillColor(UIColor(hue: 0.3, saturation: 0.6, brightness: 0.55, alpha: 1).cgColor)
                drawing.fillEllipse(in: CGRect(x: side * 0.46, y: side * 0.12, width: side * 0.2, height: side * 0.16))
            }
            return image.pngData()
        }
    }
#endif
