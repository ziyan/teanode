import Foundation
import Observation
import SwiftUI
import TeaNodeKit

// The state behind the Shopping screens: the person's lists, one list, and the
// store they are shopping at. Each reads the server, keeps what it read in
// order, and reads again when the server says a list changed.

/// ShoppingPlace is where Shopping is pushed on the part's stack: a list, or a
/// store.
enum ShoppingPlace: Hashable {
    case list(String)
    /// A store, with the lists to start with (all the lists that have items
    /// there when empty). Opening it starts nothing: the first item put in
    /// the cart there starts the person's trip.
    case store(String, listIds: [String])
}

/// ShoppingReceiptTarget is a receipt whose effect on the lists a sheet shows.
struct ShoppingReceiptTarget: Identifiable, Hashable {
    let financeReceiptId: String
    var id: String { financeReceiptId }
}

/// ShoppingFollowing keeps a screen current while it is visible: it reads
/// again each time the server says a list changed, and every thirty seconds in
/// case a message was missed. It ends when the screen goes away.
@MainActor
enum ShoppingFollowing {
    private static let pollInterval = Duration.seconds(30)

    static func follow(connection: Connection, refresh: @escaping @MainActor () async -> Void) async {
        #if DEBUG
            // The demo has no socket to listen to.
            if DemoServer.isRequested { return }
        #endif
        async let listening: Void = listen(connection: connection, refresh: refresh)
        async let polling: Void = poll(refresh: refresh)
        _ = await (listening, polling)
    }

    private static func poll(refresh: @escaping @MainActor () async -> Void) async {
        while !Task.isCancelled {
            try? await Task.sleep(for: pollInterval)
            if Task.isCancelled { return }
            await refresh()
        }
    }

    private static func listen(connection: Connection, refresh: @escaping @MainActor () async -> Void) async {
        var delay = Duration.seconds(1)
        while !Task.isCancelled {
            do {
                for try await _ in ShoppingFeed.events(connection: connection) {
                    await refresh()
                    delay = .seconds(1)
                }
            } catch is CancellationError {
                return
            } catch {
                // The feed dropped; what was missed is read again below.
            }
            try? await Task.sleep(for: delay)
            if Task.isCancelled { return }
            delay = min(delay * 2, .seconds(30))
            await refresh()
        }
    }
}

// MARK: The lists

/// ShoppingHomeModel is the Shopping screen: the person's lists, the lists
/// shared with them, and the stores their lists have items for.
@MainActor
@Observable
final class ShoppingHomeModel {
    let connection: Connection

    private(set) var lists: [ShoppingList] = []
    private(set) var stores: [ShoppingStore] = []
    /// The person's own trip, while they are shopping.
    private(set) var ownTrip: ShoppingTrip?
    /// How many items are in the person's own cart at that trip's store.
    private(set) var ownCartCount = 0
    /// The trip the person finished in the last few hours, which they can
    /// resume; nil while they shop.
    private(set) var resumableTrip: ShoppingTrip?
    /// The person's own id, set by the screen before the first read.
    var ownUserId: String?
    /// Who each of the person's shared lists is shared with, by list.
    private(set) var granteeNames: [String: [String]] = [:]
    private(set) var hasLoaded = false
    /// Why the first read failed, when there is nothing to show instead.
    private(set) var loadFailedText: String?
    var problemText: String?
    var doneText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    var ownLists: [ShoppingList] { lists.filter(\.isOwner) }
    var sharedLists: [ShoppingList] { lists.filter { !$0.isOwner } }

    func load() async {
        do {
            lists = try await connection.shoppingLists()
            hasLoaded = true
            loadFailedText = nil
        } catch {
            if hasLoaded { problemText = error.loadProblemText } else { loadFailedText = error.loadProblemText }
            return
        }
        stores = (try? await connection.shoppingStores()) ?? stores
        await loadTrips()
        var names: [String: [String]] = [:]
        for list in lists where list.isOwner && list.isShared {
            names[list.id] = (try? await connection.shoppingListShares(shoppingListId: list.id))?.shownGrantees ?? granteeNames[list.id] ?? []
        }
        granteeNames = names
    }

    /// Reads the person's own trip and what is in their cart, or the trip
    /// they could resume. Only a help to the screen, so a failure keeps what
    /// was read before.
    private func loadTrips() async {
        guard let openTrips = try? await connection.openShoppingTrips() else { return }
        ownTrip = openTrips.first { $0.userId == ownUserId }
        if let ownTrip {
            resumableTrip = nil
            let atStore = try? await connection.shoppingStoreItems(storeName: ownTrip.storeName, shoppingListIds: [])
            ownCartCount = atStore?.shoppingItems.filter { $0.isInCart && $0.cartedByUserId == ownUserId }.count ?? ownCartCount
        } else {
            ownCartCount = 0
            resumableTrip = try? await connection.resumableShoppingTrip()
        }
    }

    /// Resumes a trip finished by mistake; true when it is open again.
    func resume(_ trip: ShoppingTrip) async -> Bool {
        do {
            try await connection.resumeShopping(shoppingTripId: trip.id)
            await load()
            return true
        } catch {
            problemText = ShoppingWords.problem(error)
            return false
        }
    }

    /// Makes a list and returns it, nil when the server refused.
    func create(listName: String) async -> ShoppingList? {
        do {
            let created = try await connection.createShoppingList(listName: listName)
            await load()
            return created
        } catch {
            problemText = ShoppingWords.problem(error)
            return nil
        }
    }

    func follow() async {
        await ShoppingFollowing.follow(connection: connection) { [weak self] in await self?.load() }
    }
}

/// ShoppingListModel is one list: its items, who is shopping it, who it is
/// shared with, and the changes a person makes to it.
@MainActor
@Observable
final class ShoppingListModel {
    let connection: Connection
    let listId: String

    private(set) var list: ShoppingList?
    private(set) var items: [ShoppingItem] = []
    private(set) var trips: [ShoppingTrip] = []
    /// The person's own open trip on any list, which makes a check here put
    /// the item in their cart rather than buy it.
    private(set) var ownTrip: ShoppingTrip?
    /// The trip the person finished in the last few hours on this list, which
    /// they can resume.
    private(set) var resumableTrip: ShoppingTrip?
    /// Checks answered at once and not yet confirmed by the server, by item.
    private(set) var pendingChecks: [String: Bool] = [:]
    /// The person's own id, set by the screen before the first read.
    var ownUserId: String?
    private(set) var granteeNames: [String] = []
    private(set) var stores: [ShoppingStore] = []
    /// Whether the person is reminded of the list at a store; kept here so a
    /// switch answers at once.
    private(set) var isReminded = true
    private(set) var hasLoaded = false
    private(set) var isDeleted = false
    private(set) var loadFailedText: String?
    let receipts: ShoppingReceiptReader
    var problemText: String?
    var doneText: String?

    init(connection: Connection, listId: String) {
        self.connection = connection
        self.listId = listId
        let reader = ShoppingReceiptReader(connection: connection)
        receipts = reader
        reader.tellDone = { [weak self] text in self?.doneText = text }
        reader.tellProblem = { [weak self] text in self?.problemText = text }
    }

    var toBuy: [ShoppingItem] { items.filter { !$0.isNotNeeded } }
    var notNeeded: [ShoppingItem] { ShoppingGrouping.split(items).notNeeded }
    /// What the person has in their own cart from this list.
    var ownCart: [ShoppingItem] { items.filter { $0.isInCart && $0.cartedByUserId == ownUserId } }
    var neededCount: Int { items.filter(\.isNeeded).count }
    var estimatedTotal: ShoppingTotal? { ShoppingMoney.estimate(of: items.filter(\.isNeeded)) }

    func load() async {
        do {
            let page = try await connection.wholeShoppingList(shoppingListId: listId)
            if let read = page.shoppingList {
                list = read
                isReminded = read.isReminded
            }
            items = page.shoppingItems
            trips = page.shoppingTrips
            hasLoaded = true
            loadFailedText = nil
        } catch {
            if hasLoaded { problemText = error.loadProblemText } else { loadFailedText = error.loadProblemText }
            return
        }
        if list?.isOwner == true, list?.isShared == true {
            granteeNames = (try? await connection.shoppingListShares(shoppingListId: listId))?.shownGrantees ?? granteeNames
        } else {
            granteeNames = []
        }
        await loadTrips()
    }

    /// Reads the person's own trip, or the one they could resume. Only a help
    /// to the screen, so a failure keeps what was read before.
    private func loadTrips() async {
        guard let openTrips = try? await connection.openShoppingTrips() else { return }
        ownTrip = openTrips.first { $0.userId == ownUserId }
        if ownTrip != nil {
            resumableTrip = nil
        } else {
            let resumable = try? await connection.resumableShoppingTrip()
            resumableTrip = resumable?.shoppingListIds.contains(listId) == true ? resumable : nil
        }
    }

    // MARK: Checks

    /// Whether an item's check is filled, as far as the person has just said:
    /// in a cart, or checked off.
    func isChecked(_ item: ShoppingItem) -> Bool {
        pendingChecks[item.id] ?? !item.isNeeded
    }

    /// Whether the person may tap an item's check: not while it is in
    /// somebody else's cart, which is theirs to buy.
    func canCheck(_ item: ShoppingItem) -> Bool {
        !(item.isInCart && item.cartedByUserId != ownUserId)
    }

    /// The check on a list. While the person shops, it puts the item in their
    /// cart, as the store screen does; otherwise it is bought now, with no
    /// trip and nobody told. A filled check takes back what it did.
    func toggle(_ item: ShoppingItem) async {
        guard canCheck(item) else { return }
        let isChecking = !isChecked(item)
        pendingChecks[item.id] = isChecking
        do {
            if item.isInCart {
                replace(try await connection.checkShoppingItem(shoppingItemId: item.id, isChecked: false))
            } else if ownTrip != nil && isChecking {
                replace(try await connection.checkShoppingItem(shoppingItemId: item.id, isChecked: true, storeName: ownTrip?.storeName ?? ""))
            } else {
                replace(try await connection.checkOffShoppingItem(shoppingItemId: item.id, isCheckedOff: isChecking))
            }
        } catch {
            problemText = ShoppingWords.problem(error)
        }
        pendingChecks[item.id] = nil
    }

    /// Resumes a trip finished by mistake; true when it is open again.
    func resume(_ trip: ShoppingTrip) async -> Bool {
        let isDone = await attempt { try await connection.resumeShopping(shoppingTripId: trip.id) }
        if isDone {
            await load()
            doneText = String(localized: "Shopping again. What the trip bought is back in your cart.")
        }
        return isDone
    }

    func follow() async {
        await ShoppingFollowing.follow(connection: connection) { [weak self] in await self?.load() }
    }

    /// Puts a changed item where it is in the list, or at its end.
    func replace(_ changed: ShoppingItem) {
        if let index = items.firstIndex(where: { $0.id == changed.id }) {
            items[index] = changed
        } else if changed.shoppingListId == listId {
            items.append(changed)
        }
    }

    private func attempt(_ work: () async throws -> Void) async -> Bool {
        do {
            try await work()
            return true
        } catch {
            problemText = ShoppingWords.problem(error)
            return false
        }
    }

    /// Needs a bought item again, with a count; true when it was done.
    @discardableResult
    func needAgain(_ item: ShoppingItem, quantity: String) async -> Bool {
        let isDone = await attempt {
            replace(try await connection.setShoppingItemNeed(shoppingItemId: item.id, isNeeded: true, neededQuantity: quantity))
        }
        if isDone { doneText = String(localized: "\(item.itemDescription) is on the list again") }
        return isDone
    }

    func remove(_ item: ShoppingItem) async {
        let isDone = await attempt {
            try await connection.removeShoppingItems(shoppingItemIds: [item.id])
            items.removeAll { $0.id == item.id }
        }
        if isDone { doneText = String(localized: "Removed \(item.itemDescription)") }
    }

    /// Adds items to the list; true when they were added.
    func add(_ entries: [ShoppingItemEntry]) async -> Bool {
        let isDone = await attempt {
            _ = try await connection.addShoppingItems(shoppingListId: listId, entries: entries)
            await load()
        }
        return isDone
    }

    func rename(_ listName: String) async {
        let isDone = await attempt { _ = try await connection.renameShoppingList(shoppingListId: listId, listName: listName) }
        if isDone { await load() }
    }

    /// Deletes the list (the owner's); true when it is gone.
    func delete() async -> Bool {
        let isDone = await attempt { try await connection.deleteShoppingList(shoppingListId: listId) }
        if isDone { isDeleted = true }
        return isDone
    }

    func setReminder(_ isOn: Bool) async {
        let previous = isReminded
        isReminded = isOn
        let isDone = await attempt { try await connection.setShoppingListReminder(shoppingListId: listId, isReminded: isOn) }
        if !isDone { isReminded = previous }
    }

    /// Whether the person is shopping: they have a trip open, or something
    /// of this list in their cart.
    var isShopping: Bool { ownTrip != nil || !ownCart.isEmpty }

    /// Reads the stores the lists have items for, to choose one to shop at.
    func loadStores() async {
        stores = (try? await connection.shoppingStores()) ?? stores
    }

    /// The stores this list has something to buy at: the stores the server
    /// lists that the list's needed items are for, or, if the server could not
    /// be asked, the names on the items.
    var storeChoices: [String] {
        let wanted = toBuy.filter { !$0.isForAnyStore }
        var keys: [String] = []
        for item in wanted where !keys.contains(item.storeKey) { keys.append(item.storeKey) }
        return keys.compactMap { key in
            stores.first { $0.storeKey == key }?.storeName ?? wanted.first { $0.storeKey == key }?.storeName
        }
    }
}

// MARK: Shopping at a store

/// ShoppingStoreModel is the screen of shopping one store: the needed items
/// of the lists included that are for the store or for any store, each a check
/// to put in the cart, and the person's trip, which the server starts when
/// they put the first item in the cart and which ends with Done.
@MainActor
@Observable
final class ShoppingStoreModel {
    let connection: Connection
    let storeName: String
    /// The person's own id, which of the open trips is theirs.
    let ownUserId: String?

    private(set) var items: [ShoppingItem] = []
    private(set) var lists: [ShoppingStoreList] = []
    private(set) var trips: [ShoppingTrip] = []
    private(set) var ownTrip: ShoppingTrip?
    /// The trip the person finished here in the last few hours, which they
    /// can resume; nil while they shop.
    private(set) var resumableTrip: ShoppingTrip?
    private(set) var isReminded = true
    private(set) var hasLoaded = false
    private(set) var loadFailedText: String?
    /// The lists included, as chosen; empty until the first read says which lists
    /// have items here, when all of them are.
    private var includedListIds: [String]
    /// Checks answered at once and not yet confirmed by the server, by item.
    private(set) var pendingChecks: [String: Bool] = [:]
    let receipts: ShoppingReceiptReader
    var problemText: String?
    var doneText: String?

    init(connection: Connection, storeName: String, listIds: [String], ownUserId: String?) {
        self.connection = connection
        self.storeName = storeName
        self.includedListIds = listIds
        self.ownUserId = ownUserId
        let reader = ShoppingReceiptReader(connection: connection)
        receipts = reader
        reader.tellDone = { [weak self] text in self?.doneText = text }
        reader.tellProblem = { [weak self] text in self?.problemText = text }
    }

    var storeKey: String { ShoppingStoreKey.key(of: storeName) }
    var progress: ShoppingCartProgress { ShoppingCartProgress(items: shownItems) }
    /// What is left to find, by aisle; what is in a cart is under it.
    var sections: ShoppingStoreSections { ShoppingGrouping.forStore(shownItems.filter { !isChecked($0) }) }
    /// What is in a cart, the person's own and the others', last.
    var inCart: [ShoppingItem] { shownItems.filter { isChecked($0) } }
    /// What is in the person's own cart here.
    var ownCart: [ShoppingItem] { items.filter { $0.isInCart && $0.cartedByUserId == ownUserId } }
    var otherShopperNames: [String] { trips.otherShoppers(atStoreKey: storeKey, excludingUserId: ownUserId) }
    var includedCount: Int { lists.filter(\.isIncluded).count }

    /// Whether Done has anything to do: the person has a trip open, or has
    /// put something here in the cart.
    var isShopping: Bool {
        ownTrip != nil || items.contains { $0.isInCart && $0.cartedByUserId == ownUserId }
    }

    /// The items shown: those still wanted, whichever the server sent.
    private var shownItems: [ShoppingItem] { items.filter { !$0.isNotNeeded } }

    /// Whether an item is checked, as far as the person has just said.
    func isChecked(_ item: ShoppingItem) -> Bool {
        pendingChecks[item.id] ?? item.isInCart
    }

    func load() async {
        do {
            let read = try await connection.shoppingStoreItems(storeName: storeName, shoppingListIds: includedListIds)
            items = read.shoppingItems
            lists = read.shoppingLists
            trips = read.shoppingTrips
            isReminded = read.isReminded
            if includedListIds.isEmpty { includedListIds = read.includedListIds }
            hasLoaded = true
            loadFailedText = nil
            ownTrip = trips.first { $0.isOpen && $0.userId == ownUserId }
        } catch {
            if hasLoaded { problemText = error.loadProblemText } else { loadFailedText = error.loadProblemText }
            return
        }
        if ownTrip != nil {
            resumableTrip = nil
        } else {
            let resumable = try? await connection.resumableShoppingTrip()
            resumableTrip = resumable?.storeKey == storeKey ? resumable : nil
        }
    }

    func follow() async {
        await ShoppingFollowing.follow(connection: connection) { [weak self] in await self?.load() }
    }

    private func replace(_ changed: ShoppingItem) {
        if let index = items.firstIndex(where: { $0.id == changed.id }) {
            items[index] = changed
        }
    }

    /// Puts an item in the cart or takes it out, showing it at once.
    func toggle(_ item: ShoppingItem) async {
        let isChecking = !isChecked(item)
        pendingChecks[item.id] = isChecking
        do {
            // The server starts the person's trip with the first item they
            // put in the cart, at this store, and tells the others.
            replace(try await connection.checkShoppingItem(shoppingItemId: item.id, isChecked: isChecking, storeName: storeName))
        } catch {
            problemText = ShoppingWords.problem(error)
        }
        pendingChecks[item.id] = nil
        if isChecking && ownTrip == nil { await load() }
    }

    func setOutOfStock(_ item: ShoppingItem, isOutOfStock: Bool) async {
        do {
            replace(try await connection.setShoppingItemOutOfStock(shoppingItemId: item.id, isOutOfStock: isOutOfStock, storeName: storeName))
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    /// Includes a list in the screen, or leaves it out.
    func setIncluded(_ list: ShoppingStoreList, isIncluded: Bool) async {
        var ids = includedListIds.isEmpty ? lists.filter(\.isIncluded).map(\.shoppingListId) : includedListIds
        ids.removeAll { $0 == list.shoppingListId }
        if isIncluded { ids.append(list.shoppingListId) }
        // At least one list stays, or the server would take none to mean all.
        guard !ids.isEmpty else { return }
        includedListIds = ids
        await load()
    }

    func setReminder(_ isOn: Bool) async {
        let previous = isReminded
        isReminded = isOn
        do {
            try await connection.setShoppingStoreReminder(storeName: storeName, isReminded: isOn)
        } catch {
            isReminded = previous
            problemText = ShoppingWords.problem(error)
        }
    }

    /// Resumes a trip finished by mistake; true when it is open again.
    func resume(_ trip: ShoppingTrip) async -> Bool {
        do {
            try await connection.resumeShopping(shoppingTripId: trip.id)
            await load()
            doneText = String(localized: "Shopping again. What the trip bought is back in your cart.")
            return true
        } catch {
            problemText = ShoppingWords.problem(error)
            return false
        }
    }

    /// After the cart sheet finished the trip: what is left is read again,
    /// and the trip is offered to resume.
    func finished(saying text: String) async {
        ownTrip = nil
        await load()
        doneText = text
    }
}

// MARK: The cart

/// ShoppingCartModel is the person's cart at Done: what they put in it, to
/// look over before it is bought, and the two ways to finish, with a picture
/// of the receipt or without.
@MainActor
@Observable
final class ShoppingCartModel {
    let connection: Connection
    let storeName: String
    private let ownUserId: String?

    private(set) var items: [ShoppingItem]
    private(set) var isWorking = false
    /// What the sheet is doing while it sends the receipt and finishes.
    private(set) var workingText: String?
    var problemText: String?

    /// shownItems are the items the screen already has; the store's are read
    /// too, so an item put in the cart from another list is not missed.
    init(connection: Connection, storeName: String, ownUserId: String?, shownItems: [ShoppingItem]) {
        self.connection = connection
        self.storeName = storeName
        self.ownUserId = ownUserId
        items = shownItems.filter { $0.isInCart && $0.cartedByUserId == ownUserId }
    }

    var total: ShoppingTotal? { ShoppingMoney.estimate(of: items) }
    var listCount: Int { Set(items.map(\.shoppingListId)).count }

    func load() async {
        guard let atStore = try? await connection.shoppingStoreItems(storeName: storeName, shoppingListIds: []) else { return }
        var merged = items
        for item in atStore.shoppingItems where item.isInCart && item.cartedByUserId == ownUserId {
            if let index = merged.firstIndex(where: { $0.id == item.id }) {
                merged[index] = item
            } else {
                merged.append(item)
            }
        }
        items = merged
    }

    /// Takes an item out of the cart, back to needed.
    func takeOut(_ item: ShoppingItem) async {
        do {
            _ = try await connection.checkShoppingItem(shoppingItemId: item.id, isChecked: false)
            items.removeAll { $0.id == item.id }
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    /// Finishes the trip, after sending the receipt's picture to be read when
    /// there is one. The reading goes on after: a receipt read late still
    /// prices what was just bought. Answers what to tell the person, nil when
    /// it did not work.
    func finish(receipt file: PendingFile?) async -> String? {
        isWorking = true
        defer {
            isWorking = false
            workingText = nil
        }
        do {
            if let file {
                workingText = String(localized: "Sending the receipt…")
                let attachmentId = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
                try await connection.readReceipt(attachmentId: attachmentId)
            }
            workingText = String(localized: "Finishing…")
            try await connection.finishShopping()
            return file == nil
                ? String(localized: "Done shopping.")
                : String(localized: "Done shopping. The receipt is being read; prices are added when it is done.")
        } catch {
            problemText = ShoppingWords.problem(error)
            return nil
        }
    }
}

// MARK: Receipts

/// ShoppingReceiptReader uploads a picture of a receipt, has the agent read
/// it, and waits for it to be kept; the sheet that says what it did to the
/// lists then opens. A screen that offers to scan a receipt holds one.
@MainActor
@Observable
final class ShoppingReceiptReader {
    let connection: Connection
    /// What the reading is doing, nil when none is going on.
    private(set) var readingText: String?
    var target: ShoppingReceiptTarget?
    /// Says something to the person, as the screen's toast does.
    @ObservationIgnored var tellDone: (String) -> Void = { _ in }
    @ObservationIgnored var tellProblem: (String) -> Void = { _ in }

    init(connection: Connection) {
        self.connection = connection
    }

    /// Waits every four seconds, two minutes at most, as Finance waits.
    func scan(_ file: PendingFile) async {
        readingText = String(localized: "Reading the receipt…")
        tellDone(String(localized: "The receipt is being read. Your lists update when it is done."))
        do {
            let attachmentId = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
            try await connection.readReceipt(attachmentId: attachmentId)
            for _ in 0..<30 {
                try? await Task.sleep(for: .seconds(4))
                let recent = (try? await connection.financeReceipts(limit: 20).financeReceipts.compactMap { $0 }) ?? []
                if let kept = recent.first(where: { $0.agentAttachmentId == attachmentId }) {
                    readingText = nil
                    target = ShoppingReceiptTarget(financeReceiptId: kept.id)
                    return
                }
            }
            readingText = nil
            tellDone(String(localized: "Still reading. The receipt will update your lists when it is done."))
        } catch {
            readingText = nil
            tellProblem(ShoppingWords.problem(error))
        }
    }
}
