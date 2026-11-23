import Foundation
import TeaNodeKit

/// ShoppingWords writes what the Shopping screens say that is not a fixed
/// label: the lines under a list and under an item, and the progress at a
/// store.
enum ShoppingWords {
    static var title: String { String(localized: "Shopping") }

    /// What a refused change says: the server's reason, else the advice every
    /// screen gives.
    static func problem(_ error: Error) -> String {
        error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
    }

    /// "N to buy".
    static func toBuyCount(_ count: Int) -> String {
        String(localized: "\(count) to buy")
    }

    /// The line under a list in the list of lists: who it is shared with, or who shared it.
    static func listRowSubtitle(_ list: ShoppingList, granteeNames: [String]) -> String? {
        if !list.isOwner { return list.shownOwner }
        if granteeNames.isEmpty { return list.isShared ? String(localized: "Shared") : nil }
        return String(localized: "Shared with \(ListFormatter.localizedString(byJoining: granteeNames))")
    }

    /// The line under a list's title: how many are to buy, and about what
    /// they cost.
    static func listSubtitle(toBuyCount: Int, total: ShoppingTotal?) -> String {
        var parts = [Self.toBuyCount(toBuyCount)]
        if let total { parts.append(String(localized: "about \(total.shown)")) }
        return parts.joined(separator: " · ")
    }

    /// Who a list is shared with or by, at its top; nil for a list nobody
    /// else has.
    static func listSharingLine(_ list: ShoppingList?, granteeNames: [String]) -> String? {
        guard let list else { return nil }
        if !list.isOwner { return String(localized: "Shared by \(list.shownOwner)") }
        if !granteeNames.isEmpty { return String(localized: "Shared with \(ListFormatter.localizedString(byJoining: granteeNames))") }
        return list.isShared ? String(localized: "Shared") : nil
    }

    /// The line under an item on a list: how many it needs and its details; or
    /// that it is in the cart or out of stock; or when it was last bought.
    static func listItemDetail(_ item: ShoppingItem) -> String {
        var parts: [String] = []
        switch item.state {
        case .needed:
            parts.append(String(localized: "Need \(item.shownQuantityWithUnit)"))
        case .inCart:
            parts.append(
                item.cartedByName.isEmpty
                    ? String(localized: "In the cart") : String(localized: "In the cart · \(item.cartedByName)"))
        case .notNeeded:
            if let bought = item.lastBoughtDate {
                parts.append(String(localized: "Bought \(bought.formatted(.relative(presentation: .named)))"))
            } else {
                parts.append(String(localized: "Not needed"))
            }
        }
        if item.isOutOfStock && !item.isNotNeeded { parts.append(String(localized: "Out of stock")) }
        if !item.itemDetails.isEmpty { parts.append(item.itemDetails) }
        return parts.joined(separator: " · ")
    }

    /// The line under an item offered instead of adding one: the list it is on
    /// when that is another, then what the list says of it.
    static func similarItemDetail(_ item: ShoppingItem, onListId listId: String) -> String {
        let detail = listItemDetail(item)
        return item.shoppingListId == listId ? detail : [item.listName, detail].joined(separator: " · ")
    }

    /// The line under an item on the store screen: its list when several are
    /// shown; then who put it in the cart, or who found it out of stock, or
    /// else its details.
    static func storeItemDetail(_ item: ShoppingItem, isChecked: Bool, showsListName: Bool) -> String {
        var parts: [String] = []
        if showsListName { parts.append(item.listName) }
        if isChecked {
            parts.append(
                item.cartedByName.isEmpty
                    ? String(localized: "In the cart") : String(localized: "In the cart · \(item.cartedByName)"))
        } else if item.isOutOfStock {
            parts.append(
                item.outOfStockByName.isEmpty
                    ? String(localized: "Out of stock") : String(localized: "Out of stock (\(item.outOfStockByName))"))
        } else if !item.itemDetails.isEmpty {
            parts.append(item.itemDetails)
        }
        return parts.joined(separator: " · ")
    }

    /// "3 of 8 in the cart · about $12.40 so far".
    static func progressLine(_ progress: ShoppingCartProgress) -> String {
        var line = String(localized: "\(progress.inCartCount) of \(progress.totalCount) in the cart")
        if let soFar = progress.soFar { line += " · " + String(localized: "about \(soFar.shown) so far") }
        return line
    }

    /// "Alex is shopping here too".
    static func otherShoppersLine(_ names: [String]) -> String? {
        guard !names.isEmpty else { return nil }
        let joined = ListFormatter.localizedString(byJoining: names)
        return names.count == 1
            ? String(localized: "\(joined) is shopping here too") : String(localized: "\(joined) are shopping here too")
    }

    /// The line under a list while others shop it: "Sam is shopping this list at Example Market".
    static func listShoppersLine(_ trips: [ShoppingTrip]) -> String? {
        guard let first = trips.first else { return nil }
        let names = ListFormatter.localizedString(byJoining: trips.map(\.shownShopper))
        if first.storeName.isEmpty {
            return trips.count == 1 ? String(localized: "\(names) is shopping this list") : String(localized: "\(names) are shopping this list")
        }
        return trips.count == 1
            ? String(localized: "\(names) is shopping this list at \(first.storeName)")
            : String(localized: "\(names) are shopping this list at \(first.storeName)")
    }

    /// "3 items · about $12.40": the cart at Done.
    static func cartSummary(itemCount: Int, total: ShoppingTotal?) -> String {
        var line = itemCount == 1 ? String(localized: "1 item") : String(localized: "\(itemCount) items")
        if let total { line += " · " + String(localized: "about \(total.shown)") }
        return line
    }

    /// What finishing without a receipt does, asked before it is done.
    static func finishWithoutReceiptMessage(itemCount: Int) -> String {
        itemCount == 1
            ? String(localized: "The item in your cart is marked bought. You can resume this trip for the next three hours.")
            : String(localized: "The \(itemCount) items in your cart are marked bought. You can resume this trip for the next three hours.")
    }

    /// "You finished shopping at Example Market at 3:41 PM".
    static func finishedLine(_ trip: ShoppingTrip) -> String {
        let time = trip.finishedDate?.formatted(date: .omitted, time: .shortened) ?? ""
        return trip.storeName.isEmpty
            ? String(localized: "You finished shopping at \(time)")
            : String(localized: "You finished shopping at \(trip.storeName) at \(time)")
    }

    /// What resuming a trip does: "3 items go back in your cart".
    static func resumeDetail(_ trip: ShoppingTrip) -> String {
        trip.boughtItemCount == 1
            ? String(localized: "1 item goes back in your cart")
            : String(localized: "\(trip.boughtItemCount) items go back in your cart")
    }

    /// "Shopping at Example Market", the person's own trip.
    static func shoppingNowLine(_ trip: ShoppingTrip) -> String {
        trip.storeName.isEmpty ? String(localized: "You are shopping") : String(localized: "Shopping at \(trip.storeName)")
    }

    /// "3 in your cart".
    static func inYourCart(_ count: Int) -> String {
        count == 0 ? String(localized: "Nothing in your cart yet") : String(localized: "\(count) in your cart")
    }

    /// "Done shopping · 3", the button that opens the cart.
    static func doneShoppingTitle(cartCount: Int) -> String {
        cartCount == 0 ? String(localized: "Done shopping") : String(localized: "Done shopping · \(cartCount)")
    }

    /// An aisle's name, or the words for none.
    static func aisleTitle(_ group: ShoppingGroup) -> String {
        group.isUnnamed ? String(localized: "No aisle") : group.title
    }

    /// A store's name, or the words for any store.
    static func storeTitle(_ group: ShoppingGroup) -> String {
        group.isUnnamed ? String(localized: "Any store") : group.title
    }

    /// Where an item is, for a tile that has no heading above it: its store
    /// (or any store) and its aisle.
    static func listItemPlace(_ item: ShoppingItem) -> String {
        let storeName = item.isForAnyStore ? String(localized: "Any store") : item.storeName
        return item.aisleName.isEmpty ? storeName : "\(storeName) · \(item.aisleName)"
    }

    /// Whether an item an import found is new or already on the list.
    static func importMark(_ item: ShoppingImportItem) -> String {
        item.isOnList ? String(localized: "On the list") : String(localized: "New")
    }

    /// The words under a price in the list of an item's prices: where it was seen and who said.
    static func priceDetail(_ price: ShoppingItemPrice) -> String {
        var parts: [String] = []
        if !price.storeName.isEmpty { parts.append(price.storeName) }
        parts.append(price.isFromReceipt ? String(localized: "From a receipt") : String(localized: "Typed in"))
        return parts.joined(separator: " · ")
    }
}
