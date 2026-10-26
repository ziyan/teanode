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
    /// A store, with the lists to start with; all the lists that have items
    /// there when empty.
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
        var names: [String: [String]] = [:]
        for list in lists where list.isOwner && list.isShared {
            names[list.id] = (try? await connection.shoppingListShares(shoppingListId: list.id))?.shownGrantees ?? granteeNames[list.id] ?? []
        }
        granteeNames = names
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
    var neededCount: Int { items.filter(\.isNeeded).count }
    var estimatedTotal: ShoppingTotal? { ShoppingMoney.estimate(of: items.filter(\.isNeeded)) }

    func load() async {
        do {
            let page = try await connection.shoppingList(shoppingListId: listId)
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
    }

    func follow() async {
        await ShoppingFollowing.follow(connection: connection) { [weak self] in await self?.load() }
    }

    /// Puts a changed item where it is in the list, or at its end.
    func replace(_ changed: ShoppingItem) {
        if let index = items.firstIndex(where: { $0.id == changed.id }) {
            items[index] = changed
        } else {
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

    /// Needs a bought item again, with a count.
    func needAgain(_ item: ShoppingItem, quantity: String) async {
        let isDone = await attempt {
            replace(try await connection.setShoppingItemNeed(shoppingItemId: item.id, isNeeded: true, neededQuantity: quantity))
        }
        if isDone { doneText = String(localized: "\(item.itemDescription) is on the list again") }
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
/// to put in the cart, and the person's trip, which starts when the screen
/// opens and ends with Done.
@MainActor
@Observable
final class ShoppingStoreModel {
    let connection: Connection
    let storeName: String
    /// The person's own id, which of the open trips is theirs.
    private let ownUserId: String?

    private(set) var items: [ShoppingItem] = []
    private(set) var lists: [ShoppingStoreList] = []
    private(set) var trips: [ShoppingTrip] = []
    private(set) var ownTrip: ShoppingTrip?
    private(set) var isReminded = true
    private(set) var hasLoaded = false
    private(set) var loadFailedText: String?
    /// The lists included, as chosen; empty until the first read says which lists
    /// have items here, when all of them are.
    private var includedListIds: [String]
    private var hasStarted = false
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
    var sections: ShoppingStoreSections { ShoppingGrouping.forStore(shownItems) }
    var otherShopperNames: [String] { trips.otherShoppers(atStoreKey: storeKey, excludingUserId: ownUserId) }
    var includedCount: Int { lists.filter(\.isIncluded).count }

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
        } catch {
            if hasLoaded { problemText = error.loadProblemText } else { loadFailedText = error.loadProblemText }
            return
        }
        await startIfNeeded()
    }

    /// Starts the person shopping here the first time the screen is read, unless a trip of
    /// theirs at this store is open already (the screen was left and opened again).
    private func startIfNeeded() async {
        guard !hasStarted else { return }
        hasStarted = true
        if let open = trips.ownTrip(atStoreKey: storeKey, userId: ownUserId) {
            ownTrip = open
            return
        }
        let listIds = includedListIds.isEmpty ? lists.map(\.shoppingListId) : includedListIds
        guard !listIds.isEmpty else { return }
        do {
            ownTrip = try await connection.startShopping(storeName: storeName, shoppingListIds: listIds)
        } catch {
            problemText = ShoppingWords.problem(error)
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
            replace(try await connection.checkShoppingItem(shoppingItemId: item.id, isChecked: isChecking))
        } catch {
            problemText = ShoppingWords.problem(error)
        }
        pendingChecks[item.id] = nil
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

    /// Ends the trip, which buys what is in the cart; true when it is over.
    func finish() async -> Bool {
        guard let ownTrip else { return true }
        do {
            try await connection.finishShopping(shoppingTripId: ownTrip.id)
            self.ownTrip = nil
            return true
        } catch {
            problemText = ShoppingWords.problem(error)
            return false
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
