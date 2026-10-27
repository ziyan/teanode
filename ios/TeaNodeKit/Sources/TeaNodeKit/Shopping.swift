import Foundation

// Shopping lists as the server keeps them: named lists of things to buy,
// shared with a circle or a person, checked off live at the store, by a
// receipt, and remembered with their prices. The dashboard, the command line
// and the agent's shopping tool use the same operations. ShoppingFeed.swift
// follows the lists as they change; the screens read them back here.

// MARK: Reading the server's answers

/// ShoppingKey is a field of the server's answer, named by its text, so that
/// a model reads the fields it knows without listing them twice.
struct ShoppingKey: CodingKey {
    let stringValue: String
    var intValue: Int? { nil }

    init?(stringValue: String) { self.stringValue = stringValue }
    init?(intValue: Int) { nil }
}

/// ShoppingFields reads the fields of one object the server sent. A field a
/// server does not send reads as empty, so a screen made for a newer server
/// still works with an older one.
struct ShoppingFields {
    let container: KeyedDecodingContainer<ShoppingKey>

    init(_ decoder: Decoder) throws {
        container = try decoder.container(keyedBy: ShoppingKey.self)
    }

    private func key(_ name: String) -> ShoppingKey { ShoppingKey(stringValue: name)! }

    /// A field every object has; reading fails without it.
    func requiredText(_ name: String) throws -> String {
        try container.decode(String.self, forKey: key(name))
    }

    func text(_ name: String) -> String {
        (try? container.decodeIfPresent(String.self, forKey: key(name))).flatMap { $0 } ?? ""
    }

    func optionalText(_ name: String) -> String? {
        (try? container.decodeIfPresent(String.self, forKey: key(name))).flatMap { $0 }
    }

    func flag(_ name: String) -> Bool {
        (try? container.decodeIfPresent(Bool.self, forKey: key(name))).flatMap { $0 } ?? false
    }

    func whole(_ name: String) -> Int {
        (try? container.decodeIfPresent(Int.self, forKey: key(name))).flatMap { $0 } ?? 0
    }

    func list<Element: Decodable>(_ name: String, of elementType: Element.Type) -> [Element] {
        (try? container.decodeIfPresent([Element].self, forKey: key(name))).flatMap { $0 } ?? []
    }

    func object<Element: Decodable>(_ name: String, of elementType: Element.Type) -> Element? {
        (try? container.decodeIfPresent(Element.self, forKey: key(name))).flatMap { $0 }
    }
}

// MARK: Numbers and money

/// ShoppingQuantity reads and writes the counts the server keeps: numbers
/// above zero, up to eight places, as text.
public enum ShoppingQuantity {
    /// The number a count stands for; nil for text that is not one.
    public static func decimal(_ text: String) -> Decimal? {
        let trimmed = text.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty else { return nil }
        return Decimal(string: trimmed, locale: Locale(identifier: "en_US_POSIX"))
    }

    /// A count as a person reads it: "2" for 2.00000000, "1.5" for 1.50000000.
    public static func shown(_ text: String) -> String {
        var shown = text.trimmingCharacters(in: .whitespaces)
        guard shown.contains(".") else { return shown.isEmpty ? "1" : shown }
        while shown.hasSuffix("0") { shown.removeLast() }
        if shown.hasSuffix(".") { shown.removeLast() }
        return shown.isEmpty ? "0" : shown
    }

    /// A count as the server takes it, from a stepper's number: up to three
    /// places, no trailing zeros.
    public static func text(of value: Double) -> String {
        shown(String(format: "%.3f", value))
    }

    /// The count as a number, 1 for one that cannot be read.
    public static func value(of text: String) -> Double {
        guard let decimal = decimal(text) else { return 1 }
        return NSDecimalNumber(decimal: decimal).doubleValue
    }
}

/// ShoppingTotal is what some items cost, in one currency.
public struct ShoppingTotal: Equatable, Sendable {
    public let amount: Decimal
    public let currencyCode: String
    /// How many of the items had a price.
    public let pricedCount: Int

    /// The amount in its currency, as "$12.40".
    public var shown: String {
        amount.formatted(.currency(code: currencyCode.isEmpty ? "USD" : currencyCode))
    }
}

/// ShoppingMoney adds up what items cost.
public enum ShoppingMoney {
    /// The price of one of an item, nil when it has none.
    public static func unitPrice(of item: ShoppingItem) -> Decimal? {
        guard let price = ShoppingQuantity.decimal(item.unitPriceAmount), price > 0 else { return nil }
        return price
    }

    /// What the items cost in all, from their unit prices and counts. Items in
    /// the currency most of them are priced in are added; a price in another
    /// currency is left out rather than added to it. Nil when no item has a
    /// price.
    public static func estimate(of items: [ShoppingItem]) -> ShoppingTotal? {
        let priced = items.filter { unitPrice(of: $0) != nil }
        guard !priced.isEmpty else { return nil }
        var countsByCurrency: [String: Int] = [:]
        for item in priced { countsByCurrency[item.currencyCode, default: 0] += 1 }
        let currencyCode = countsByCurrency.sorted { first, second in
            first.value == second.value ? first.key < second.key : first.value > second.value
        }.first?.key ?? ""
        var amount = Decimal(0)
        var pricedCount = 0
        for item in priced where item.currencyCode == currencyCode {
            guard let price = unitPrice(of: item) else { continue }
            amount += price * (ShoppingQuantity.decimal(item.neededQuantity) ?? 1)
            pricedCount += 1
        }
        return ShoppingTotal(amount: amount, currencyCode: currencyCode, pricedCount: pricedCount)
    }

    /// A price in its currency, or an empty text when there is none.
    public static func shown(_ amountText: String, currencyCode: String) -> String {
        guard let amount = ShoppingQuantity.decimal(amountText), amount > 0 else { return "" }
        return amount.formatted(.currency(code: currencyCode.isEmpty ? "USD" : currencyCode))
    }

    /// What the count of an item costs, from its unit price; empty when it has no price.
    public static func shownLineTotal(of item: ShoppingItem) -> String {
        guard let price = unitPrice(of: item) else { return "" }
        let total = price * (ShoppingQuantity.decimal(item.neededQuantity) ?? 1)
        return total.formatted(.currency(code: item.currencyCode.isEmpty ? "USD" : item.currencyCode))
    }

    /// A price as it is typed in a field: at least two places and at most
    /// four, "1.2900" as "1.29". Empty when there is no price.
    public static func typedAmount(_ amountText: String) -> String {
        guard let amount = ShoppingQuantity.decimal(amountText), amount > 0 else { return "" }
        var text = ShoppingQuantity.shown(amountText)
        if !text.contains(".") { text += ".00" }
        if let dot = text.firstIndex(of: "."), text.distance(from: dot, to: text.endIndex) == 2 { text += "0" }
        return text
    }

    /// Whether typed text is a price the server takes: a number above zero with
    /// up to four places.
    public static func isPrice(_ text: String) -> Bool {
        guard FinanceMoney.isDecimal(text), let amount = ShoppingQuantity.decimal(text) else { return false }
        return amount > 0
    }
}

/// ShoppingStoreKey is how stores are compared: a chain's name without case,
/// spaces or punctuation, so any branch matches. The server computes the same
/// key for a trip's `storeKey`. It keeps letters and decimal digits one
/// Unicode scalar at a time, as the server does per rune, so a name typed
/// with a combining accent or a voiced kana mark gets the server's key.
public enum ShoppingStoreKey {
    public static func key(of storeName: String) -> String {
        var kept = String.UnicodeScalarView()
        for scalar in storeName.lowercased().unicodeScalars {
            switch scalar.properties.generalCategory {
            case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter, .decimalNumber:
                kept.append(scalar)
            default:
                continue
            }
        }
        return String(kept)
    }
}

// MARK: Items

/// ShoppingItemState is where an item is: wanted, put in the cart at the
/// store, or bought and kept on the list to be wanted again.
public enum ShoppingItemState: String, CaseIterable, Sendable {
    case needed
    case inCart = "in_cart"
    case notNeeded = "not_needed"
}

/// ShoppingItem is one thing on one list.
public struct ShoppingItem: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    public let shoppingListId: String
    public let listName: String
    public let itemDescription: String
    public let itemDetails: String
    /// The store to get it from; empty for any store.
    public let storeName: String
    public let storeKey: String
    public let isStoreBrand: Bool
    public let aisleName: String
    public let aisleRank: Int
    public let itemRank: Int
    /// How many are needed, a number as text.
    public let neededQuantity: String
    public let quantityUnit: String
    /// The latest price of one, a number as text; empty when none is known.
    public let unitPriceAmount: String
    public let currencyCode: String
    /// needed, in_cart or not_needed.
    public let shoppingItemState: String
    public let cartedByUserId: String
    public let cartedByName: String
    public let cartedAt: String?
    public let outOfStockAt: String?
    public let outOfStockByUserId: String
    public let outOfStockByName: String
    public let outOfStockStoreName: String
    public let lastBoughtAt: String?
    public let pictureFileId: String
    public let picturePageUrl: String
    /// Where the picture is served from, under /api/v1; empty when there is none.
    public let picturePath: String
    /// "", "web" or "upload".
    public let pictureSource: String
    public let pictureSearchedAt: String?

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        id = try fields.requiredText("id")
        shoppingListId = fields.text("shoppingListId")
        listName = fields.text("listName")
        itemDescription = fields.text("itemDescription")
        itemDetails = fields.text("itemDetails")
        storeName = fields.text("storeName")
        storeKey = fields.text("storeKey")
        isStoreBrand = fields.flag("isStoreBrand")
        aisleName = fields.text("aisleName")
        aisleRank = fields.whole("aisleRank")
        itemRank = fields.whole("itemRank")
        neededQuantity = fields.text("neededQuantity")
        quantityUnit = fields.text("quantityUnit")
        unitPriceAmount = fields.text("unitPriceAmount")
        currencyCode = fields.text("currencyCode")
        shoppingItemState = fields.text("shoppingItemState")
        cartedByUserId = fields.text("cartedByUserId")
        cartedByName = fields.text("cartedByName")
        cartedAt = fields.optionalText("cartedAt")
        outOfStockAt = fields.optionalText("outOfStockAt")
        outOfStockByUserId = fields.text("outOfStockByUserId")
        outOfStockByName = fields.text("outOfStockByName")
        outOfStockStoreName = fields.text("outOfStockStoreName")
        lastBoughtAt = fields.optionalText("lastBoughtAt")
        pictureFileId = fields.text("pictureFileId")
        picturePageUrl = fields.text("picturePageUrl")
        picturePath = fields.text("picturePath")
        pictureSource = fields.text("pictureSource")
        pictureSearchedAt = fields.optionalText("pictureSearchedAt")
    }

    /// The state, needed for one the app does not know.
    public var state: ShoppingItemState { ShoppingItemState(rawValue: shoppingItemState) ?? .needed }
    public var isNeeded: Bool { state == .needed }
    public var isInCart: Bool { state == .inCart }
    public var isNotNeeded: Bool { state == .notNeeded }

    /// Whether somebody found the store out of it.
    public var isOutOfStock: Bool { !(outOfStockAt ?? "").isEmpty }
    public var hasPicture: Bool { !picturePath.isEmpty }
    public var isForAnyStore: Bool { storeKey.isEmpty }

    /// The price of one, nil when none is known.
    public var unitPrice: Decimal? { ShoppingMoney.unitPrice(of: self) }
    public var shownUnitPrice: String { ShoppingMoney.shown(unitPriceAmount, currencyCode: currencyCode) }
    public var lastBoughtDate: Date? { ServerTime.date(lastBoughtAt) }

    /// The count as a person reads it: "2", "1.5".
    public var shownQuantity: String { ShoppingQuantity.shown(neededQuantity) }

    /// The count with its unit, "2" or "1.5 lb".
    public var shownQuantityWithUnit: String {
        quantityUnit.isEmpty ? shownQuantity : shownQuantity + " " + quantityUnit
    }

    /// What the store screen calls the item: "2 Apples", "1.5 lb Apples", or
    /// just "Apples" for one.
    public var shownTitle: String {
        if shownQuantity == "1", quantityUnit.isEmpty { return itemDescription }
        return shownQuantityWithUnit + " " + itemDescription
    }

    /// The first words of the description, which stand for the item where a
    /// picture is missing: its first letter.
    public var initial: String { itemDescription.first.map { String($0).uppercased() } ?? "?" }
}

/// ShoppingTrip is somebody shopping a store now, for some lists.
public struct ShoppingTrip: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    public let userId: String
    public let shopperName: String
    public let shopperUsername: String
    public let storeName: String
    public let storeKey: String
    public let shoppingListIds: [String]
    public let startedAt: String
    public let finishedAt: String?

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        id = try fields.requiredText("id")
        userId = fields.text("userId")
        shopperName = fields.text("shopperName")
        shopperUsername = fields.text("shopperUsername")
        storeName = fields.text("storeName")
        storeKey = fields.text("storeKey")
        shoppingListIds = fields.list("shoppingListIds", of: String.self)
        startedAt = fields.text("startedAt")
        finishedAt = fields.optionalText("finishedAt")
    }

    /// Who is shopping: their name, or their username when they have no name.
    public var shownShopper: String { shopperName.isEmpty ? shopperUsername : shopperName }

    /// Whether it is still going.
    public var isOpen: Bool { (finishedAt ?? "").isEmpty }
}

extension Array where Element == ShoppingTrip {
    /// The names of the other people shopping a store now, once each: trips
    /// still open at the store (at any store when `storeKey` is empty), not the
    /// person's own.
    public func otherShoppers(atStoreKey storeKey: String, excludingUserId ownUserId: String?) -> [String] {
        var names: [String] = []
        for trip in self where trip.isOpen && trip.userId != ownUserId && (storeKey.isEmpty || trip.storeKey == storeKey) {
            if !names.contains(trip.shownShopper) { names.append(trip.shownShopper) }
        }
        return names
    }

    /// The trips of the other people shopping a list now.
    public func openTrips(ofListId shoppingListId: String, excludingUserId ownUserId: String?) -> [ShoppingTrip] {
        filter { $0.isOpen && $0.userId != ownUserId && $0.shoppingListIds.contains(shoppingListId) }
    }

    /// The person's own open trip at a store, if they have one.
    public func ownTrip(atStoreKey storeKey: String, userId ownUserId: String?) -> ShoppingTrip? {
        first { $0.isOpen && $0.userId == ownUserId && $0.storeKey == storeKey }
    }
}

// MARK: Lists

/// ShoppingList is a named list with its counts, as the person reaches it.
public struct ShoppingList: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    public let userId: String
    public let listName: String
    public let ownerName: String
    public let ownerUsername: String
    public let isOwner: Bool
    public let isShared: Bool
    /// Whether the person is reminded of the list on arriving at a store.
    public let isReminded: Bool
    public let neededCount: Int
    public let inCartCount: Int
    public let notNeededCount: Int
    public let createdAt: String
    public let updatedAt: String

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        id = try fields.requiredText("id")
        userId = fields.text("userId")
        listName = fields.text("listName")
        ownerName = fields.text("ownerName")
        ownerUsername = fields.text("ownerUsername")
        isOwner = fields.flag("isOwner")
        isShared = fields.flag("isShared")
        isReminded = fields.flag("isReminded")
        neededCount = fields.whole("neededCount")
        inCartCount = fields.whole("inCartCount")
        notNeededCount = fields.whole("notNeededCount")
        createdAt = fields.text("createdAt")
        updatedAt = fields.text("updatedAt")
    }

    /// The owner's name, or their username when they have no name.
    public var shownOwner: String { ownerName.isEmpty ? ownerUsername : ownerName }
}

/// ShoppingListPage is one list with its items and who is shopping it now.
public struct ShoppingListPage: Decodable, Hashable, Sendable {
    public let shoppingList: ShoppingList?
    public let shoppingItems: [ShoppingItem]
    public let shoppingTrips: [ShoppingTrip]
    public let totalCount: Int

    public init(shoppingList: ShoppingList?, shoppingItems: [ShoppingItem], shoppingTrips: [ShoppingTrip], totalCount: Int) {
        self.shoppingList = shoppingList
        self.shoppingItems = shoppingItems
        self.shoppingTrips = shoppingTrips
        self.totalCount = totalCount
    }

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        shoppingList = fields.object("shoppingList", of: ShoppingList.self)
        shoppingItems = fields.list("shoppingItems", of: ShoppingItem.self)
        shoppingTrips = fields.list("shoppingTrips", of: ShoppingTrip.self)
        totalCount = fields.whole("totalCount")
    }
}

/// ShoppingListShare is a list shared with a person or a circle.
public struct ShoppingListShare: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    public let shoppingListId: String
    public let listName: String
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String
    public let granteeUserId: String
    public let granteeUsername: String
    public let granteeName: String
    public let granteeCircleId: String
    public let granteeCircleName: String
    public let createdAt: String

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        id = try fields.requiredText("id")
        shoppingListId = fields.text("shoppingListId")
        listName = fields.text("listName")
        ownerUserId = fields.text("ownerUserId")
        ownerUsername = fields.text("ownerUsername")
        ownerName = fields.text("ownerName")
        granteeUserId = fields.text("granteeUserId")
        granteeUsername = fields.text("granteeUsername")
        granteeName = fields.text("granteeName")
        granteeCircleId = fields.text("granteeCircleId")
        granteeCircleName = fields.text("granteeCircleName")
        createdAt = fields.text("createdAt")
    }

    /// Whether it is with a circle rather than a person.
    public var isWithCircle: Bool { !granteeCircleId.isEmpty }

    /// Who it is with: the circle's name, or the person's name, or their
    /// username when they have no name.
    public var shownGrantee: String {
        if isWithCircle { return granteeCircleName }
        return granteeName.isEmpty ? granteeUsername : granteeName
    }
}

extension Array where Element == ShoppingListShare {
    /// The names a list is shared with, once each: "Home, Sam".
    public var shownGrantees: [String] {
        var names: [String] = []
        for share in self where !names.contains(share.shownGrantee) { names.append(share.shownGrantee) }
        return names
    }
}

// MARK: Stores

/// ShoppingStore is a store the person's lists have items for.
public struct ShoppingStore: Decodable, Identifiable, Hashable, Sendable {
    public let storeKey: String
    public let storeName: String
    public let neededCount: Int
    /// Whether the person is reminded of what they need on arriving there.
    public let isReminded: Bool

    public var id: String { storeKey }

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        storeKey = try fields.requiredText("storeKey")
        storeName = fields.text("storeName")
        neededCount = fields.whole("neededCount")
        isReminded = fields.flag("isReminded")
    }
}

/// ShoppingStoreList is a list that has items at a store, one of the buttons
/// on the store screen that include or leave out a list.
public struct ShoppingStoreList: Decodable, Identifiable, Hashable, Sendable {
    public let shoppingListId: String
    public let listName: String
    public let neededCount: Int
    public let isIncluded: Bool

    public var id: String { shoppingListId }

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        shoppingListId = try fields.requiredText("shoppingListId")
        listName = fields.text("listName")
        neededCount = fields.whole("neededCount")
        isIncluded = fields.flag("isIncluded")
    }
}

/// ShoppingStoreItems is the store screen: the needed and in-cart items of the
/// lists included, the lists that have items there, and who is shopping.
public struct ShoppingStoreItems: Decodable, Hashable, Sendable {
    public let storeName: String
    public let isReminded: Bool
    public let shoppingItems: [ShoppingItem]
    public let shoppingLists: [ShoppingStoreList]
    public let shoppingTrips: [ShoppingTrip]

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        storeName = fields.text("storeName")
        isReminded = fields.flag("isReminded")
        shoppingItems = fields.list("shoppingItems", of: ShoppingItem.self)
        shoppingLists = fields.list("shoppingLists", of: ShoppingStoreList.self)
        shoppingTrips = fields.list("shoppingTrips", of: ShoppingTrip.self)
    }

    /// The ids of the lists included.
    public var includedListIds: [String] { shoppingLists.filter(\.isIncluded).map(\.shoppingListId) }
}

// MARK: Prices

/// ShoppingItemPrice is one price seen for an item at a store.
public struct ShoppingItemPrice: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    public let shoppingItemId: String
    public let storeName: String
    public let storeKey: String
    public let unitPriceAmount: String
    public let currencyCode: String
    public let quantity: String
    public let quantityUnit: String
    /// The day it was seen, YYYY-MM-DD.
    public let observedOn: String
    /// receipt or person.
    public let priceSource: String
    public let financeReceiptId: String
    public let createdAt: String

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        id = try fields.requiredText("id")
        shoppingItemId = fields.text("shoppingItemId")
        storeName = fields.text("storeName")
        storeKey = fields.text("storeKey")
        unitPriceAmount = fields.text("unitPriceAmount")
        currencyCode = fields.text("currencyCode")
        quantity = fields.text("quantity")
        quantityUnit = fields.text("quantityUnit")
        observedOn = fields.text("observedOn")
        priceSource = fields.text("priceSource")
        financeReceiptId = fields.text("financeReceiptId")
        createdAt = fields.text("createdAt")
    }

    public var amount: Decimal? { ShoppingQuantity.decimal(unitPriceAmount) }
    public var shownAmount: String { ShoppingMoney.shown(unitPriceAmount, currencyCode: currencyCode) }

    /// The day it was seen, at midnight UTC.
    public var observedDate: Date? {
        guard let day = CalendarDates.day(ofAllDay: observedOn + "T00:00:00Z") else { return nil }
        return CalendarDates.utc.date(from: day)
    }

    /// Whether a receipt gave it, rather than a person.
    public var isFromReceipt: Bool { priceSource == "receipt" }
}

/// ShoppingPriceHistory reads an item's prices.
public enum ShoppingPriceHistory {
    /// The price seen before a given one, newest first as the server lists
    /// them: what an item cost until a receipt changed it. Nil when there is
    /// no earlier price.
    public static func previous(before priceId: String, in prices: [ShoppingItemPrice]) -> ShoppingItemPrice? {
        guard let index = prices.firstIndex(where: { $0.id == priceId }) else { return nil }
        return prices.dropFirst(index + 1).first
    }

    /// The prices oldest first, those with a price and a day, for a chart.
    public static func chartable(_ prices: [ShoppingItemPrice]) -> [ShoppingItemPrice] {
        prices.filter { $0.amount != nil && $0.observedDate != nil }
            .sorted { ($0.observedDate ?? .distantPast) < ($1.observedDate ?? .distantPast) }
    }
}

// MARK: Importing

/// ShoppingImportItem is one item the model read out of text or pictures, and
/// the item already on the list it would merge into, when there is one.
public struct ShoppingImportItem: Decodable, Hashable, Sendable {
    public let itemDescription: String
    public let itemDetails: String
    public let neededQuantity: String
    public let quantityUnit: String
    public let aisleName: String
    public let storeName: String
    public let isStoreBrand: Bool
    public let mergedShoppingItemId: String
    public let mergedShoppingItemState: String
    public let mergedNeededQuantity: String

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        itemDescription = fields.text("itemDescription")
        itemDetails = fields.text("itemDetails")
        neededQuantity = fields.text("neededQuantity")
        quantityUnit = fields.text("quantityUnit")
        aisleName = fields.text("aisleName")
        storeName = fields.text("storeName")
        isStoreBrand = fields.flag("isStoreBrand")
        mergedShoppingItemId = fields.text("mergedShoppingItemId")
        mergedShoppingItemState = fields.text("mergedShoppingItemState")
        mergedNeededQuantity = fields.text("mergedNeededQuantity")
    }

    /// Whether the list has the item already.
    public var isOnList: Bool { !mergedShoppingItemId.isEmpty }

    /// What a row of the preview calls the item: "2 Apples", "1.5 lb Apples",
    /// or just "Apples" for one.
    public var shownTitle: String {
        let quantity = ShoppingQuantity.shown(neededQuantity)
        if quantity == "1", quantityUnit.isEmpty { return itemDescription }
        return (quantityUnit.isEmpty ? quantity : quantity + " " + quantityUnit) + " " + itemDescription
    }

    /// The item's store, aisle and details, in one line; empty when it has none.
    public var shownDetail: String {
        [storeName, aisleName, itemDetails].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// ShoppingImportPreview is a list as the model read it.
public struct ShoppingImportPreview: Decodable, Hashable, Sendable {
    public let shoppingItems: [ShoppingImportItem]

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        shoppingItems = fields.list("shoppingItems", of: ShoppingImportItem.self)
    }
}

/// ShoppingItemEntry is one item to add to a list.
public struct ShoppingItemEntry: Hashable, Sendable {
    public var itemDescription: String
    public var itemDetails = ""
    public var storeName = ""
    public var isStoreBrand = false
    public var aisleName = ""
    public var neededQuantity = "1"
    public var quantityUnit = ""

    public init(
        itemDescription: String, itemDetails: String = "", storeName: String = "", isStoreBrand: Bool = false, aisleName: String = "",
        neededQuantity: String = "1", quantityUnit: String = ""
    ) {
        self.itemDescription = itemDescription
        self.itemDetails = itemDetails
        self.storeName = storeName
        self.isStoreBrand = isStoreBrand
        self.aisleName = aisleName
        self.neededQuantity = neededQuantity
        self.quantityUnit = quantityUnit
    }

    /// The input as the server takes it; what is empty is left out.
    public var variables: JSONValue {
        var object: [String: JSONValue] = ["itemDescription": .string(itemDescription)]
        if !itemDetails.isEmpty { object["itemDetails"] = .string(itemDetails) }
        if !storeName.isEmpty { object["storeName"] = .string(storeName) }
        if isStoreBrand { object["isStoreBrand"] = .bool(true) }
        if !aisleName.isEmpty { object["aisleName"] = .string(aisleName) }
        if !neededQuantity.isEmpty { object["neededQuantity"] = .string(neededQuantity) }
        if !quantityUnit.isEmpty { object["quantityUnit"] = .string(quantityUnit) }
        return .object(object)
    }
}

/// ShoppingImportDraft is one item of an import preview that the person may
/// leave out before adding.
public struct ShoppingImportDraft: Identifiable, Hashable, Sendable {
    public let id: Int
    public let item: ShoppingImportItem
    public var isIncluded: Bool

    /// The drafts of a preview, in order, all included.
    public static func drafts(of preview: ShoppingImportPreview) -> [ShoppingImportDraft] {
        preview.shoppingItems.enumerated().map { ShoppingImportDraft(id: $0.offset, item: $0.element, isIncluded: true) }
    }

    /// The entry that adds the item.
    public var entry: ShoppingItemEntry {
        ShoppingItemEntry(
            itemDescription: item.itemDescription, itemDetails: item.itemDetails, storeName: item.storeName, isStoreBrand: item.isStoreBrand,
            aisleName: item.aisleName, neededQuantity: item.neededQuantity.isEmpty ? "1" : item.neededQuantity, quantityUnit: item.quantityUnit)
    }

    /// The entries of the drafts still included.
    public static func entries(of drafts: [ShoppingImportDraft]) -> [ShoppingItemEntry] {
        drafts.filter(\.isIncluded).map(\.entry)
    }
}

/// ShoppingItemAdded is an item as it was added, and whether it went into one
/// the list had already.
public struct ShoppingItemAdded: Decodable, Hashable, Sendable {
    public let isMerged: Bool
    public let shoppingItem: ShoppingItem?

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        isMerged = fields.flag("isMerged")
        shoppingItem = fields.object("shoppingItem", of: ShoppingItem.self)
    }
}

// MARK: Receipts

/// ShoppingReceiptMatch ties a line of a receipt to an item.
public struct ShoppingReceiptMatch: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    public let financeReceiptId: String
    public let financeReceiptLineId: String
    public let shoppingItemId: String
    /// applied, proposed or dismissed.
    public let receiptMatchState: String
    public let matchedQuantity: String
    public let previousShoppingItemState: String
    public let previousNeededQuantity: String
    public let shoppingItemPriceId: String

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        id = try fields.requiredText("id")
        financeReceiptId = fields.text("financeReceiptId")
        financeReceiptLineId = fields.text("financeReceiptLineId")
        shoppingItemId = fields.text("shoppingItemId")
        receiptMatchState = fields.text("receiptMatchState")
        matchedQuantity = fields.text("matchedQuantity")
        previousShoppingItemState = fields.text("previousShoppingItemState")
        previousNeededQuantity = fields.text("previousNeededQuantity")
        shoppingItemPriceId = fields.text("shoppingItemPriceId")
    }

    public var isApplied: Bool { receiptMatchState == "applied" }
    public var isProposed: Bool { receiptMatchState == "proposed" }
}

/// ShoppingReceiptLineMatch is a match with the item it is to.
public struct ShoppingReceiptLineMatch: Decodable, Hashable, Sendable {
    public let shoppingReceiptMatch: ShoppingReceiptMatch?
    public let shoppingItem: ShoppingItem?

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        shoppingReceiptMatch = fields.object("shoppingReceiptMatch", of: ShoppingReceiptMatch.self)
        shoppingItem = fields.object("shoppingItem", of: ShoppingItem.self)
    }
}

/// ShoppingReceiptLine is one item line of a receipt as printed, and the
/// matches it has to items on the lists.
public struct ShoppingReceiptLine: Decodable, Identifiable, Hashable, Sendable {
    public let financeReceiptLineId: String
    public let lineNumber: Int
    /// The line as printed.
    public let description: String
    public let quantity: String
    public let lineAmount: String
    public let shoppingReceiptMatches: [ShoppingReceiptLineMatch]

    public var id: String { financeReceiptLineId }

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        financeReceiptLineId = try fields.requiredText("financeReceiptLineId")
        lineNumber = fields.whole("lineNumber")
        description = fields.text("description")
        quantity = fields.text("quantity")
        lineAmount = fields.text("lineAmount")
        shoppingReceiptMatches = fields.list("shoppingReceiptMatches", of: ShoppingReceiptLineMatch.self)
    }
}

/// ShoppingReceipt is what one receipt did to the person's lists.
public struct ShoppingReceipt: Decodable, Hashable, Sendable {
    public let financeReceiptId: String
    public let merchantName: String
    public let purchasedOn: String
    public let totalAmount: String
    public let currencyCode: String
    public let shoppingReceiptLines: [ShoppingReceiptLine]
    public let stillNeededItems: [ShoppingItem]

    public init(from decoder: Decoder) throws {
        let fields = try ShoppingFields(decoder)
        financeReceiptId = fields.text("financeReceiptId")
        merchantName = fields.text("merchantName")
        purchasedOn = fields.text("purchasedOn")
        totalAmount = fields.text("totalAmount")
        currencyCode = fields.text("currencyCode")
        shoppingReceiptLines = fields.list("shoppingReceiptLines", of: ShoppingReceiptLine.self)
        stillNeededItems = fields.list("stillNeededItems", of: ShoppingItem.self)
    }
}

/// ShoppingReceiptEntry is a line of a receipt with one match and its item.
public struct ShoppingReceiptEntry: Identifiable, Hashable, Sendable {
    public let line: ShoppingReceiptLine
    public let match: ShoppingReceiptMatch
    public let item: ShoppingItem

    public var id: String { match.id }
}

/// ShoppingReceiptListGroup is what a receipt checked off on one list.
public struct ShoppingReceiptListGroup: Identifiable, Hashable, Sendable {
    public let shoppingListId: String
    public let listName: String
    public let entries: [ShoppingReceiptEntry]

    public var id: String { shoppingListId }
}

/// ShoppingReceiptSections is a receipt's matches sorted into what the sheet
/// shows: what was checked off, what is proposed, what was bought and is not on
/// a list, and what is still to buy.
public struct ShoppingReceiptSections: Hashable, Sendable {
    /// Matches applied, by list.
    public let appliedGroups: [ShoppingReceiptListGroup]
    /// Matches the model was not sure of, waiting for the person.
    public let proposed: [ShoppingReceiptEntry]
    /// Lines with no match applied or proposed.
    public let notOnList: [ShoppingReceiptLine]
    /// Items on the lists the receipt touched that are still needed.
    public let stillNeeded: [ShoppingItem]

    public init(receipt: ShoppingReceipt) {
        var groups: [ShoppingReceiptListGroup] = []
        var proposed: [ShoppingReceiptEntry] = []
        var notOnList: [ShoppingReceiptLine] = []
        for line in receipt.shoppingReceiptLines {
            var hasMatch = false
            for lineMatch in line.shoppingReceiptMatches {
                guard let match = lineMatch.shoppingReceiptMatch, let item = lineMatch.shoppingItem else { continue }
                let entry = ShoppingReceiptEntry(line: line, match: match, item: item)
                if match.isApplied {
                    hasMatch = true
                    if let index = groups.firstIndex(where: { $0.shoppingListId == item.shoppingListId }) {
                        groups[index] = ShoppingReceiptListGroup(
                            shoppingListId: item.shoppingListId, listName: item.listName, entries: groups[index].entries + [entry])
                    } else {
                        groups.append(ShoppingReceiptListGroup(shoppingListId: item.shoppingListId, listName: item.listName, entries: [entry]))
                    }
                } else if match.isProposed {
                    hasMatch = true
                    proposed.append(entry)
                }
            }
            if !hasMatch { notOnList.append(line) }
        }
        appliedGroups = groups
        self.proposed = proposed
        self.notOnList = notOnList
        stillNeeded = receipt.stillNeededItems
    }

    /// How many items the receipt checked off.
    public var appliedCount: Int { appliedGroups.reduce(0) { $0 + $1.entries.count } }

    /// Whether there is anything to undo.
    public var canUndo: Bool { appliedCount > 0 }

    /// Whether the receipt did nothing to the lists and has nothing to ask.
    public var isEmpty: Bool { appliedGroups.isEmpty && proposed.isEmpty && stillNeeded.isEmpty }
}

// MARK: Grouping

/// ShoppingGroup is items shown under one heading: a store or an aisle.
public struct ShoppingGroup: Identifiable, Hashable, Sendable {
    public let id: String
    /// The store's or aisle's name; empty for the items that have none.
    public let title: String
    public let items: [ShoppingItem]

    public var isUnnamed: Bool { title.isEmpty }
}

/// ShoppingStoreSections is the store screen's sections: the aisles of the
/// store's own items, then the items for any store.
public struct ShoppingStoreSections: Hashable, Sendable {
    public let aisleGroups: [ShoppingGroup]
    public let anyStoreItems: [ShoppingItem]
}

/// ShoppingCartProgress is how far along the shopping at a store is.
public struct ShoppingCartProgress: Equatable, Sendable {
    public let inCartCount: Int
    public let totalCount: Int
    /// What the items in the cart cost so far.
    public let soFar: ShoppingTotal?

    /// Items still to find count, those found out of stock do not.
    public init(items: [ShoppingItem]) {
        let counted = items.filter { !$0.isNotNeeded && ($0.isInCart || !$0.isOutOfStock) }
        let carted = counted.filter(\.isInCart)
        inCartCount = carted.count
        totalCount = counted.count
        soFar = ShoppingMoney.estimate(of: carted)
    }
}

/// ShoppingGrouping lays items out by store, by aisle and for the store screen.
public enum ShoppingGrouping {
    private static func areInOrder(_ first: ShoppingItem, _ second: ShoppingItem) -> Bool {
        if first.itemRank != second.itemRank { return first.itemRank < second.itemRank }
        return first.itemDescription.localizedCaseInsensitiveCompare(second.itemDescription) == .orderedAscending
    }

    private static func areInTitleOrder(_ first: String, _ second: String) -> Bool {
        first.localizedCaseInsensitiveCompare(second) == .orderedAscending
    }

    /// The items by store, the stores in alphabetical order and the items for
    /// any store last.
    public static func byStore(_ items: [ShoppingItem]) -> [ShoppingGroup] {
        var order: [String] = []
        var membersByKey: [String: [ShoppingItem]] = [:]
        for item in items {
            if membersByKey[item.storeKey] == nil { order.append(item.storeKey) }
            membersByKey[item.storeKey, default: []].append(item)
        }
        // A store is named as the first item for it names it.
        let groups = order.map { key -> ShoppingGroup in
            let seen = membersByKey[key] ?? []
            return ShoppingGroup(id: "store:" + key, title: seen.first?.storeName ?? "", items: seen.sorted(by: areInOrder))
        }
        let named = groups.filter { !$0.isUnnamed }.sorted { areInTitleOrder($0.title, $1.title) }
        return named + groups.filter(\.isUnnamed)
    }

    /// The items by aisle, the aisles in the order they were given and the
    /// items with none last.
    public static func byAisle(_ items: [ShoppingItem]) -> [ShoppingGroup] {
        var order: [String] = []
        var membersByKey: [String: [ShoppingItem]] = [:]
        for item in items {
            let key = item.aisleName.trimmingCharacters(in: .whitespaces).lowercased()
            if membersByKey[key] == nil { order.append(key) }
            membersByKey[key, default: []].append(item)
        }
        // An aisle is named as the first item in it names it.
        let groups = order.map { key -> ShoppingGroup in
            let seen = membersByKey[key] ?? []
            let title = seen.first?.aisleName.trimmingCharacters(in: .whitespaces) ?? ""
            return ShoppingGroup(id: "aisle:" + key, title: title, items: seen.sorted(by: areInOrder))
        }
        let rank = { (group: ShoppingGroup) in group.items.map(\.aisleRank).min() ?? 0 }
        let named = groups.filter { !$0.isUnnamed }.sorted { first, second in
            rank(first) == rank(second) ? areInTitleOrder(first.title, second.title) : rank(first) < rank(second)
        }
        return named + groups.filter(\.isUnnamed)
    }

    /// The store screen's sections: the store's own items by aisle, then the
    /// items for any store in a section of their own.
    public static func forStore(_ items: [ShoppingItem]) -> ShoppingStoreSections {
        let own = items.filter { !$0.isForAnyStore }
        let anyStore = items.filter(\.isForAnyStore)
        let anyStoreGroups = byAisle(anyStore)
        return ShoppingStoreSections(aisleGroups: byAisle(own), anyStoreItems: anyStoreGroups.flatMap(\.items))
    }

    /// The items by whether they are still wanted: those to buy (needed or in
    /// the cart) and those kept on the list as not needed.
    public static func split(_ items: [ShoppingItem]) -> (toBuy: [ShoppingItem], notNeeded: [ShoppingItem]) {
        (items.filter { !$0.isNotNeeded }, items.filter(\.isNotNeeded).sorted(by: areInOrder))
    }
}

// MARK: The server's operations

extension Documents {
    static let shoppingItemFields = """
        id shoppingListId listName itemDescription itemDetails storeName storeKey isStoreBrand aisleName aisleRank itemRank
        neededQuantity quantityUnit unitPriceAmount currencyCode shoppingItemState cartedByUserId cartedByName cartedAt
        outOfStockAt outOfStockByUserId outOfStockByName outOfStockStoreName lastBoughtAt pictureFileId picturePageUrl
        picturePath pictureSource pictureSearchedAt
        """
    static let shoppingListFields = """
        id userId listName ownerName ownerUsername isOwner isShared isReminded neededCount inCartCount notNeededCount createdAt updatedAt
        """
    static let shoppingTripFields = """
        id userId shopperName shopperUsername storeName storeKey shoppingListIds startedAt finishedAt
        """
    static let shoppingShareFields = """
        id shoppingListId listName ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeName granteeCircleId
        granteeCircleName createdAt
        """
    static let shoppingMatchFields = """
        id financeReceiptId financeReceiptLineId shoppingItemId receiptMatchState matchedQuantity previousShoppingItemState
        previousNeededQuantity shoppingItemPriceId
        """

    public static let shoppingLists = """
        query { ListShoppingLists { \(shoppingListFields) } }
        """
    public static let shoppingList = """
        query ($shoppingListId: String!, $shoppingItemStates: [String!], $offset: Int, $pageSize: Int) {
          GetShoppingList(shoppingListId: $shoppingListId, shoppingItemStates: $shoppingItemStates, offset: $offset, pageSize: $pageSize) {
            totalCount
            shoppingList { \(shoppingListFields) }
            shoppingItems { \(shoppingItemFields) }
            shoppingTrips { \(shoppingTripFields) }
          }
        }
        """
    public static let shoppingItem = """
        query ($shoppingItemId: String!) { GetShoppingItem(shoppingItemId: $shoppingItemId) { \(shoppingItemFields) } }
        """
    public static let shoppingListShares = """
        query ($shoppingListId: String!) { ListShoppingListShares(shoppingListId: $shoppingListId) { \(shoppingShareFields) } }
        """
    public static let shoppingStores = """
        query { ListShoppingStores { storeKey storeName neededCount isReminded } }
        """
    public static let shoppingStoreItems = """
        query ($storeName: String!, $shoppingListIds: [String!], $isAnyStoreIncluded: Boolean) {
          ListShoppingStoreItems(storeName: $storeName, shoppingListIds: $shoppingListIds, isAnyStoreIncluded: $isAnyStoreIncluded) {
            storeName
            isReminded
            shoppingItems { \(shoppingItemFields) }
            shoppingLists { shoppingListId listName neededCount isIncluded }
            shoppingTrips { \(shoppingTripFields) }
          }
        }
        """
    public static let openShoppingTrips = """
        query { ListOpenShoppingTrips { \(shoppingTripFields) } }
        """
    public static let shoppingItemPrices = """
        query ($shoppingItemId: String!) {
          ListShoppingItemPrices(shoppingItemId: $shoppingItemId) {
            id shoppingItemId storeName storeKey unitPriceAmount currencyCode quantity quantityUnit observedOn priceSource financeReceiptId createdAt
          }
        }
        """
    public static let shoppingReceiptMatches = """
        query ($financeReceiptId: String!) {
          ListShoppingReceiptMatches(financeReceiptId: $financeReceiptId) {
            financeReceiptId merchantName purchasedOn totalAmount currencyCode
            shoppingReceiptLines {
              financeReceiptLineId lineNumber description quantity lineAmount
              shoppingReceiptMatches {
                shoppingReceiptMatch { \(shoppingMatchFields) }
                shoppingItem { \(shoppingItemFields) }
              }
            }
            stillNeededItems { \(shoppingItemFields) }
          }
        }
        """
    public static let previewShoppingImport = """
        query ($shoppingListId: String, $importText: String, $agentAttachmentIds: [String!]) {
          PreviewShoppingImport(shoppingListId: $shoppingListId, importText: $importText, agentAttachmentIds: $agentAttachmentIds) {
            shoppingItems {
              itemDescription itemDetails neededQuantity quantityUnit aisleName storeName isStoreBrand
              mergedShoppingItemId mergedShoppingItemState mergedNeededQuantity
            }
          }
        }
        """

    public static let createShoppingList = """
        mutation ($listName: String!) { CreateShoppingList(listName: $listName) { \(shoppingListFields) } }
        """
    public static let renameShoppingList = """
        mutation ($shoppingListId: String!, $listName: String!) {
          RenameShoppingList(shoppingListId: $shoppingListId, listName: $listName) { \(shoppingListFields) }
        }
        """
    public static let deleteShoppingList = """
        mutation ($shoppingListId: String!) { DeleteShoppingList(shoppingListId: $shoppingListId) }
        """
    public static let shareShoppingList = """
        mutation ($shoppingListId: String!, $granteeUserId: String, $granteeCircleId: String) {
          ShareShoppingList(shoppingListId: $shoppingListId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) {
            \(shoppingShareFields)
          }
        }
        """
    public static let unshareShoppingList = """
        mutation ($shoppingListShareId: String!) { UnshareShoppingList(shoppingListShareId: $shoppingListShareId) }
        """
    public static let setShoppingListReminder = """
        mutation ($shoppingListId: String!, $isReminded: Boolean!) {
          SetShoppingListReminder(shoppingListId: $shoppingListId, isReminded: $isReminded)
        }
        """
    public static let setShoppingStoreReminder = """
        mutation ($storeName: String!, $isReminded: Boolean!) { SetShoppingStoreReminder(storeName: $storeName, isReminded: $isReminded) }
        """
    public static let addShoppingItems = """
        mutation ($shoppingListId: String!, $shoppingItems: [ShoppingItemEntryInput!]!) {
          AddShoppingItems(shoppingListId: $shoppingListId, shoppingItems: $shoppingItems) {
            isMerged
            shoppingItem { \(shoppingItemFields) }
          }
        }
        """
    public static let updateShoppingItem = """
        mutation (
          $shoppingItemId: String!, $itemDescription: String, $itemDetails: String, $storeName: String, $isStoreBrand: Boolean,
          $aisleName: String, $neededQuantity: String, $quantityUnit: String
        ) {
          UpdateShoppingItem(
            shoppingItemId: $shoppingItemId, itemDescription: $itemDescription, itemDetails: $itemDetails, storeName: $storeName,
            isStoreBrand: $isStoreBrand, aisleName: $aisleName, neededQuantity: $neededQuantity, quantityUnit: $quantityUnit
          ) { \(shoppingItemFields) }
        }
        """
    public static let removeShoppingItems = """
        mutation ($shoppingItemIds: [String!]!) { RemoveShoppingItems(shoppingItemIds: $shoppingItemIds) }
        """
    public static let checkShoppingItem = """
        mutation ($shoppingItemId: String!, $isChecked: Boolean!, $storeName: String) {
          CheckShoppingItem(shoppingItemId: $shoppingItemId, isChecked: $isChecked, storeName: $storeName) { \(shoppingItemFields) }
        }
        """
    public static let setShoppingItemNeed = """
        mutation ($shoppingItemId: String!, $isNeeded: Boolean!, $neededQuantity: String) {
          SetShoppingItemNeed(shoppingItemId: $shoppingItemId, isNeeded: $isNeeded, neededQuantity: $neededQuantity) { \(shoppingItemFields) }
        }
        """
    public static let setShoppingItemOutOfStock = """
        mutation ($shoppingItemId: String!, $isOutOfStock: Boolean!, $storeName: String) {
          SetShoppingItemOutOfStock(shoppingItemId: $shoppingItemId, isOutOfStock: $isOutOfStock, storeName: $storeName) {
            \(shoppingItemFields)
          }
        }
        """
    public static let recordShoppingItemPrice = """
        mutation ($shoppingItemId: String!, $unitPriceAmount: String!, $storeName: String, $currencyCode: String, $observedOn: String) {
          RecordShoppingItemPrice(
            shoppingItemId: $shoppingItemId, unitPriceAmount: $unitPriceAmount, storeName: $storeName, currencyCode: $currencyCode,
            observedOn: $observedOn
          ) { id }
        }
        """
    public static let findShoppingItemPicture = """
        mutation ($shoppingItemId: String!) { FindShoppingItemPicture(shoppingItemId: $shoppingItemId) }
        """
    public static let setShoppingItemPicture = """
        mutation ($shoppingItemId: String!, $agentAttachmentId: String) {
          SetShoppingItemPicture(shoppingItemId: $shoppingItemId, agentAttachmentId: $agentAttachmentId) { \(shoppingItemFields) }
        }
        """
    public static let startShopping = """
        mutation ($storeName: String, $shoppingListIds: [String!]!) {
          StartShopping(storeName: $storeName, shoppingListIds: $shoppingListIds) { \(shoppingTripFields) }
        }
        """
    public static let finishShopping = """
        mutation ($shoppingTripId: String) { FinishShopping(shoppingTripId: $shoppingTripId) { \(shoppingTripFields) } }
        """
    public static let matchShoppingReceipt = """
        mutation ($financeReceiptId: String!) { MatchShoppingReceipt(financeReceiptId: $financeReceiptId) }
        """
    public static let decideShoppingReceiptMatch = """
        mutation ($shoppingReceiptMatchId: String!, $isApplied: Boolean!) {
          DecideShoppingReceiptMatch(shoppingReceiptMatchId: $shoppingReceiptMatchId, isApplied: $isApplied) { \(shoppingMatchFields) }
        }
        """
    public static let undoShoppingReceipt = """
        mutation ($financeReceiptId: String!) { UndoShoppingReceipt(financeReceiptId: $financeReceiptId) }
        """
    public static let addShoppingItemFromReceiptLine = """
        mutation ($financeReceiptLineId: String!, $shoppingListId: String!) {
          AddShoppingItemFromReceiptLine(financeReceiptLineId: $financeReceiptLineId, shoppingListId: $shoppingListId) {
            \(shoppingItemFields)
          }
        }
        """
    public static let shoppingEvents = """
        subscription { ShoppingEvents { shoppingListId } }
        """

    /// The shopping documents, for the server's schema check.
    public static let shoppingDocuments: [String: String] = [
        "shoppingLists": shoppingLists, "shoppingList": shoppingList, "shoppingItem": shoppingItem,
        "shoppingListShares": shoppingListShares, "shoppingStores": shoppingStores, "shoppingStoreItems": shoppingStoreItems,
        "openShoppingTrips": openShoppingTrips, "shoppingItemPrices": shoppingItemPrices,
        "shoppingReceiptMatches": shoppingReceiptMatches, "previewShoppingImport": previewShoppingImport,
        "createShoppingList": createShoppingList, "renameShoppingList": renameShoppingList, "deleteShoppingList": deleteShoppingList,
        "shareShoppingList": shareShoppingList, "unshareShoppingList": unshareShoppingList,
        "setShoppingListReminder": setShoppingListReminder, "setShoppingStoreReminder": setShoppingStoreReminder,
        "addShoppingItems": addShoppingItems, "updateShoppingItem": updateShoppingItem, "removeShoppingItems": removeShoppingItems,
        "checkShoppingItem": checkShoppingItem, "setShoppingItemNeed": setShoppingItemNeed,
        "setShoppingItemOutOfStock": setShoppingItemOutOfStock, "recordShoppingItemPrice": recordShoppingItemPrice,
        "findShoppingItemPicture": findShoppingItemPicture, "setShoppingItemPicture": setShoppingItemPicture,
        "startShopping": startShopping, "finishShopping": finishShopping, "matchShoppingReceipt": matchShoppingReceipt,
        "decideShoppingReceiptMatch": decideShoppingReceiptMatch, "undoShoppingReceipt": undoShoppingReceipt,
        "addShoppingItemFromReceiptLine": addShoppingItemFromReceiptLine, "shoppingEvents": shoppingEvents,
    ]
}

/// ShoppingEvent is the server telling a viewer that a list they reach changed.
public struct ShoppingEvent: Codable, Hashable, Sendable {
    public let shoppingListId: String
}

extension Connection {
    private func shoppingText(_ value: String?) -> JSONValue { value.map { .string($0) } ?? .null }

    // MARK: Reading

    /// The lists the person reaches, their own first.
    public func shoppingLists() async throws -> [ShoppingList] {
        struct Answer: Decodable, Sendable { let ListShoppingLists: [ShoppingList] }
        return try await execute(Documents.shoppingLists, as: Answer.self).ListShoppingLists
    }

    /// One list with its items (of the states given, or every state), who is
    /// shopping it now.
    public func shoppingList(
        shoppingListId: String, states: [ShoppingItemState] = [], offset: Int = 0, pageSize: Int = 500
    ) async throws -> ShoppingListPage {
        struct Answer: Decodable, Sendable { let GetShoppingList: ShoppingListPage }
        var variables: [String: JSONValue] = [
            "shoppingListId": .string(shoppingListId), "offset": .number(Double(offset)), "pageSize": .number(Double(pageSize)),
        ]
        if !states.isEmpty { variables["shoppingItemStates"] = .array(states.map { .string($0.rawValue) }) }
        return try await execute(Documents.shoppingList, variables: variables, as: Answer.self).GetShoppingList
    }

    /// A whole list, read in pages of pageSize items until totalCount, since
    /// the screen groups and counts every item it has.
    public func wholeShoppingList(
        shoppingListId: String, states: [ShoppingItemState] = [], pageSize: Int = 500
    ) async throws -> ShoppingListPage {
        let first = try await shoppingList(shoppingListId: shoppingListId, states: states, pageSize: pageSize)
        var items = first.shoppingItems
        while items.count < first.totalCount {
            let next = try await shoppingList(shoppingListId: shoppingListId, states: states, offset: items.count, pageSize: pageSize)
            if next.shoppingItems.isEmpty { break }
            items += next.shoppingItems
        }
        return ShoppingListPage(shoppingList: first.shoppingList, shoppingItems: items, shoppingTrips: first.shoppingTrips, totalCount: first.totalCount)
    }

    public func shoppingItem(shoppingItemId: String) async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let GetShoppingItem: ShoppingItem }
        return try await execute(Documents.shoppingItem, variables: ["shoppingItemId": .string(shoppingItemId)], as: Answer.self).GetShoppingItem
    }

    /// The shares of one of the person's own lists.
    public func shoppingListShares(shoppingListId: String) async throws -> [ShoppingListShare] {
        struct Answer: Decodable, Sendable { let ListShoppingListShares: [ShoppingListShare] }
        return try await execute(
            Documents.shoppingListShares, variables: ["shoppingListId": .string(shoppingListId)], as: Answer.self
        ).ListShoppingListShares
    }

    /// The stores the person's lists have items for.
    public func shoppingStores() async throws -> [ShoppingStore] {
        struct Answer: Decodable, Sendable { let ListShoppingStores: [ShoppingStore] }
        return try await execute(Documents.shoppingStores, as: Answer.self).ListShoppingStores
    }

    /// The store screen: the items of the lists included (every list with
    /// items there when none is named) that are for the store, and for any
    /// store when asked.
    public func shoppingStoreItems(
        storeName: String, shoppingListIds: [String] = [], isAnyStoreIncluded: Bool = true
    ) async throws -> ShoppingStoreItems {
        struct Answer: Decodable, Sendable { let ListShoppingStoreItems: ShoppingStoreItems }
        var variables: [String: JSONValue] = ["storeName": .string(storeName), "isAnyStoreIncluded": .bool(isAnyStoreIncluded)]
        if !shoppingListIds.isEmpty { variables["shoppingListIds"] = .array(shoppingListIds.map { .string($0) }) }
        return try await execute(Documents.shoppingStoreItems, variables: variables, as: Answer.self).ListShoppingStoreItems
    }

    /// The trips open now on the lists the person reaches.
    public func openShoppingTrips() async throws -> [ShoppingTrip] {
        struct Answer: Decodable, Sendable { let ListOpenShoppingTrips: [ShoppingTrip] }
        return try await execute(Documents.openShoppingTrips, as: Answer.self).ListOpenShoppingTrips
    }

    /// An item's prices, newest first.
    public func shoppingItemPrices(shoppingItemId: String) async throws -> [ShoppingItemPrice] {
        struct Answer: Decodable, Sendable { let ListShoppingItemPrices: [ShoppingItemPrice] }
        return try await execute(
            Documents.shoppingItemPrices, variables: ["shoppingItemId": .string(shoppingItemId)], as: Answer.self
        ).ListShoppingItemPrices
    }

    /// What one of the person's receipts did to their lists.
    public func shoppingReceipt(financeReceiptId: String) async throws -> ShoppingReceipt {
        struct Answer: Decodable, Sendable { let ListShoppingReceiptMatches: ShoppingReceipt }
        return try await execute(
            Documents.shoppingReceiptMatches, variables: ["financeReceiptId": .string(financeReceiptId)], as: Answer.self
        ).ListShoppingReceiptMatches
    }

    /// Reads pasted text and uploaded pictures (the agent's attachments) as a
    /// list, and says what each item would merge into; nothing is added.
    public func previewShoppingImport(shoppingListId: String?, importText: String, agentAttachmentIds: [String]) async throws -> ShoppingImportPreview {
        struct Answer: Decodable, Sendable { let PreviewShoppingImport: ShoppingImportPreview }
        var variables: [String: JSONValue] = [:]
        if let shoppingListId { variables["shoppingListId"] = .string(shoppingListId) }
        let trimmedText = importText.trimmingCharacters(in: .whitespacesAndNewlines)
        if !trimmedText.isEmpty { variables["importText"] = .string(trimmedText) }
        if !agentAttachmentIds.isEmpty { variables["agentAttachmentIds"] = .array(agentAttachmentIds.map { .string($0) }) }
        return try await execute(Documents.previewShoppingImport, variables: variables, requestTimeoutInterval: 180, as: Answer.self)
            .PreviewShoppingImport
    }

    // MARK: Lists

    @discardableResult
    public func createShoppingList(listName: String) async throws -> ShoppingList {
        struct Answer: Decodable, Sendable { let CreateShoppingList: ShoppingList }
        return try await execute(Documents.createShoppingList, variables: ["listName": .string(listName)], as: Answer.self).CreateShoppingList
    }

    @discardableResult
    public func renameShoppingList(shoppingListId: String, listName: String) async throws -> ShoppingList {
        struct Answer: Decodable, Sendable { let RenameShoppingList: ShoppingList }
        return try await execute(
            Documents.renameShoppingList, variables: ["shoppingListId": .string(shoppingListId), "listName": .string(listName)], as: Answer.self
        ).RenameShoppingList
    }

    public func deleteShoppingList(shoppingListId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteShoppingList: Bool? }
        _ = try await execute(Documents.deleteShoppingList, variables: ["shoppingListId": .string(shoppingListId)], as: Answer.self)
    }

    /// Shares one of the person's lists with a person or a circle (one of the two).
    @discardableResult
    public func shareShoppingList(shoppingListId: String, granteeUserId: String?, granteeCircleId: String?) async throws -> ShoppingListShare {
        struct Answer: Decodable, Sendable { let ShareShoppingList: ShoppingListShare }
        return try await execute(
            Documents.shareShoppingList,
            variables: [
                "shoppingListId": .string(shoppingListId), "granteeUserId": shoppingText(granteeUserId),
                "granteeCircleId": shoppingText(granteeCircleId),
            ], as: Answer.self
        ).ShareShoppingList
    }

    public func unshareShoppingList(shoppingListShareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareShoppingList: Bool? }
        _ = try await execute(
            Documents.unshareShoppingList, variables: ["shoppingListShareId": .string(shoppingListShareId)], as: Answer.self)
    }

    /// Whether the person is reminded of a list on arriving at a store.
    public func setShoppingListReminder(shoppingListId: String, isReminded: Bool) async throws {
        struct Answer: Decodable, Sendable { let SetShoppingListReminder: Bool? }
        _ = try await execute(
            Documents.setShoppingListReminder, variables: ["shoppingListId": .string(shoppingListId), "isReminded": .bool(isReminded)],
            as: Answer.self)
    }

    /// Whether the person is reminded of what they need on arriving at a store.
    public func setShoppingStoreReminder(storeName: String, isReminded: Bool) async throws {
        struct Answer: Decodable, Sendable { let SetShoppingStoreReminder: Bool? }
        _ = try await execute(
            Documents.setShoppingStoreReminder, variables: ["storeName": .string(storeName), "isReminded": .bool(isReminded)], as: Answer.self)
    }

    // MARK: Items

    /// Adds items to a list; one it has already is merged into.
    @discardableResult
    public func addShoppingItems(shoppingListId: String, entries: [ShoppingItemEntry]) async throws -> [ShoppingItemAdded] {
        struct Answer: Decodable, Sendable { let AddShoppingItems: [ShoppingItemAdded] }
        return try await execute(
            Documents.addShoppingItems,
            variables: ["shoppingListId": .string(shoppingListId), "shoppingItems": .array(entries.map(\.variables))], as: Answer.self
        ).AddShoppingItems
    }

    /// Changes what is given of an item; what is nil stays as it is.
    @discardableResult
    public func updateShoppingItem(
        shoppingItemId: String, itemDescription: String? = nil, itemDetails: String? = nil, storeName: String? = nil,
        isStoreBrand: Bool? = nil, aisleName: String? = nil, neededQuantity: String? = nil, quantityUnit: String? = nil
    ) async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let UpdateShoppingItem: ShoppingItem }
        var variables: [String: JSONValue] = ["shoppingItemId": .string(shoppingItemId)]
        if let itemDescription { variables["itemDescription"] = .string(itemDescription) }
        if let itemDetails { variables["itemDetails"] = .string(itemDetails) }
        if let storeName { variables["storeName"] = .string(storeName) }
        if let isStoreBrand { variables["isStoreBrand"] = .bool(isStoreBrand) }
        if let aisleName { variables["aisleName"] = .string(aisleName) }
        if let neededQuantity { variables["neededQuantity"] = .string(neededQuantity) }
        if let quantityUnit { variables["quantityUnit"] = .string(quantityUnit) }
        return try await execute(Documents.updateShoppingItem, variables: variables, as: Answer.self).UpdateShoppingItem
    }

    public func removeShoppingItems(shoppingItemIds: [String]) async throws {
        struct Answer: Decodable, Sendable { let RemoveShoppingItems: Int? }
        _ = try await execute(
            Documents.removeShoppingItems, variables: ["shoppingItemIds": .array(shoppingItemIds.map { .string($0) })], as: Answer.self)
    }

    /// Puts an item in the cart, or back to needed. The first item the person
    /// puts in the cart starts their trip, at storeName (the item's store
    /// when empty), and the others the list reaches are told.
    @discardableResult
    public func checkShoppingItem(shoppingItemId: String, isChecked: Bool, storeName: String = "") async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let CheckShoppingItem: ShoppingItem }
        var variables: [String: JSONValue] = ["shoppingItemId": .string(shoppingItemId), "isChecked": .bool(isChecked)]
        if !storeName.isEmpty { variables["storeName"] = .string(storeName) }
        return try await execute(Documents.checkShoppingItem, variables: variables, as: Answer.self).CheckShoppingItem
    }

    /// Needs an item again with a count, or marks it not needed.
    @discardableResult
    public func setShoppingItemNeed(shoppingItemId: String, isNeeded: Bool, neededQuantity: String? = nil) async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let SetShoppingItemNeed: ShoppingItem }
        var variables: [String: JSONValue] = ["shoppingItemId": .string(shoppingItemId), "isNeeded": .bool(isNeeded)]
        if let neededQuantity { variables["neededQuantity"] = .string(neededQuantity) }
        return try await execute(Documents.setShoppingItemNeed, variables: variables, as: Answer.self).SetShoppingItemNeed
    }

    @discardableResult
    public func setShoppingItemOutOfStock(shoppingItemId: String, isOutOfStock: Bool, storeName: String? = nil) async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let SetShoppingItemOutOfStock: ShoppingItem }
        var variables: [String: JSONValue] = ["shoppingItemId": .string(shoppingItemId), "isOutOfStock": .bool(isOutOfStock)]
        if let storeName, !storeName.isEmpty { variables["storeName"] = .string(storeName) }
        return try await execute(Documents.setShoppingItemOutOfStock, variables: variables, as: Answer.self).SetShoppingItemOutOfStock
    }

    /// Records a price seen for an item, at its store unless one is given.
    public func recordShoppingItemPrice(shoppingItemId: String, unitPriceAmount: String, storeName: String? = nil, currencyCode: String? = nil) async throws {
        struct Answer: Decodable, Sendable {
            struct Price: Decodable, Sendable { let id: String }
            let RecordShoppingItemPrice: Price?
        }
        var variables: [String: JSONValue] = ["shoppingItemId": .string(shoppingItemId), "unitPriceAmount": .string(unitPriceAmount)]
        if let storeName, !storeName.isEmpty { variables["storeName"] = .string(storeName) }
        if let currencyCode, !currencyCode.isEmpty { variables["currencyCode"] = .string(currencyCode) }
        _ = try await execute(Documents.recordShoppingItemPrice, variables: variables, as: Answer.self)
    }

    /// Queues the search for another picture of an item; the picture appears
    /// on the item when it is found.
    public func findShoppingItemPicture(shoppingItemId: String) async throws {
        struct Answer: Decodable, Sendable { let FindShoppingItemPicture: Bool? }
        _ = try await execute(Documents.findShoppingItemPicture, variables: ["shoppingItemId": .string(shoppingItemId)], as: Answer.self)
    }

    /// Gives an item a picture uploaded as an attachment, or none.
    @discardableResult
    public func setShoppingItemPicture(shoppingItemId: String, agentAttachmentId: String?) async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let SetShoppingItemPicture: ShoppingItem }
        return try await execute(
            Documents.setShoppingItemPicture,
            variables: ["shoppingItemId": .string(shoppingItemId), "agentAttachmentId": shoppingText(agentAttachmentId)], as: Answer.self
        ).SetShoppingItemPicture
    }

    // MARK: Shopping

    /// Starts the person shopping a store for some lists, which tells the
    /// others the lists reach. An empty store name is no store in particular.
    @discardableResult
    public func startShopping(storeName: String, shoppingListIds: [String]) async throws -> ShoppingTrip {
        struct Answer: Decodable, Sendable { let StartShopping: ShoppingTrip }
        var variables: [String: JSONValue] = ["shoppingListIds": .array(shoppingListIds.map { .string($0) })]
        if !storeName.isEmpty { variables["storeName"] = .string(storeName) }
        return try await execute(Documents.startShopping, variables: variables, as: Answer.self).StartShopping
    }

    /// Done: what the person put in the cart is bought, no receipt needed.
    /// With no trip named it ends their own trip, or buys their cart when
    /// none is open.
    @discardableResult
    public func finishShopping(shoppingTripId: String? = nil) async throws -> ShoppingTrip {
        struct Answer: Decodable, Sendable { let FinishShopping: ShoppingTrip }
        var variables: [String: JSONValue] = [:]
        if let shoppingTripId, !shoppingTripId.isEmpty { variables["shoppingTripId"] = .string(shoppingTripId) }
        return try await execute(Documents.finishShopping, variables: variables, as: Answer.self).FinishShopping
    }

    // MARK: Receipts

    /// Asks for a receipt to be matched against the person's lists again.
    public func matchShoppingReceipt(financeReceiptId: String) async throws {
        struct Answer: Decodable, Sendable { let MatchShoppingReceipt: Bool? }
        _ = try await execute(Documents.matchShoppingReceipt, variables: ["financeReceiptId": .string(financeReceiptId)], as: Answer.self)
    }

    /// Applies a proposed match, or dismisses one (undoing it if it was applied).
    @discardableResult
    public func decideShoppingReceiptMatch(shoppingReceiptMatchId: String, isApplied: Bool) async throws -> ShoppingReceiptMatch {
        struct Answer: Decodable, Sendable { let DecideShoppingReceiptMatch: ShoppingReceiptMatch }
        return try await execute(
            Documents.decideShoppingReceiptMatch,
            variables: ["shoppingReceiptMatchId": .string(shoppingReceiptMatchId), "isApplied": .bool(isApplied)], as: Answer.self
        ).DecideShoppingReceiptMatch
    }

    /// Puts back everything a receipt did to the lists.
    public func undoShoppingReceipt(financeReceiptId: String) async throws {
        struct Answer: Decodable, Sendable { let UndoShoppingReceipt: Bool? }
        _ = try await execute(Documents.undoShoppingReceipt, variables: ["financeReceiptId": .string(financeReceiptId)], as: Answer.self)
    }

    /// Adds what a receipt line bought to a list as an item, not needed.
    @discardableResult
    public func addShoppingItemFromReceiptLine(financeReceiptLineId: String, shoppingListId: String) async throws -> ShoppingItem {
        struct Answer: Decodable, Sendable { let AddShoppingItemFromReceiptLine: ShoppingItem }
        return try await execute(
            Documents.addShoppingItemFromReceiptLine,
            variables: ["financeReceiptLineId": .string(financeReceiptLineId), "shoppingListId": .string(shoppingListId)], as: Answer.self
        ).AddShoppingItemFromReceiptLine
    }
}
