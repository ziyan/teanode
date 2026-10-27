import SwiftUI
import TeaNodeKit

/// ShoppingReceiptSheet is what one receipt did to the person's shopping lists:
/// what it checked off, with the line as printed and what it cost; matches the
/// model was not sure of, to apply or dismiss; what was bought and is on no
/// list, to add to one; and what is still to buy. Undo puts everything back.
struct ShoppingReceiptSheet: View {
    let connection: Connection
    let financeReceiptId: String
    @Environment(\.dismiss) private var dismiss
    @State private var receipt: ShoppingReceipt?
    @State private var sections: ShoppingReceiptSections?
    @State private var lists: [ShoppingList] = []
    /// The price each checked-off item had before the receipt, by match.
    @State private var previousPrices: [String: ShoppingItemPrice] = [:]
    @State private var pricedMatchIds: [String] = []
    @State private var loadProblemText: String?
    @State private var deciding: ShoppingReceiptEntry?
    @State private var adding: ShoppingReceiptLine?
    @State private var isConfirmingUndo = false
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Receipt")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
                .overlay {
                    if let loadProblemText, receipt == nil {
                        LoadFailedView(problemText: loadProblemText) { await load() }
                    } else if receipt == nil {
                        ProgressView()
                    }
                }
                .refreshable { await load() }
                .task { await load() }
                .task { await ShoppingFollowing.follow(connection: connection) { await load() } }
                .toast(done: $doneText, problem: $problemText)
        }
    }

    // MARK: Sections

    @ViewBuilder
    private var content: some View {
        List {
            if let receipt, let sections {
                summarySection(receipt)
                ForEach(sections.appliedGroups) { group in
                    Section {
                        ForEach(group.entries) { entry in appliedRow(entry, receipt: receipt) }
                    } header: {
                        Text("Checked off \(group.entries.count) on \(group.listName)")
                    }
                }
                if sections.appliedGroups.isEmpty, sections.proposed.isEmpty {
                    noMatchSection(receipt)
                }
                if !sections.proposed.isEmpty {
                    proposedSection(sections.proposed, receipt: receipt)
                }
                if !sections.notOnList.isEmpty {
                    notOnListSection(sections.notOnList, receipt: receipt)
                }
                if !sections.stillNeeded.isEmpty {
                    stillNeededSection(sections.stillNeeded, receipt: receipt)
                }
                if sections.canUndo {
                    undoSection
                }
            }
        }
    }

    private func summarySection(_ receipt: ShoppingReceipt) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: receipt.merchantName.isEmpty ? String(localized: "Receipt") : receipt.merchantName)
                    .font(.headline)
                Text(verbatim: summaryDetail(receipt))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func summaryDetail(_ receipt: ShoppingReceipt) -> String {
        var parts: [String] = []
        if !receipt.purchasedOn.isEmpty { parts.append(receipt.purchasedOn) }
        let total = ShoppingMoney.shown(receipt.totalAmount, currencyCode: receipt.currencyCode)
        if !total.isEmpty { parts.append(total) }
        return parts.joined(separator: " · ")
    }

    private func appliedRow(_ entry: ShoppingReceiptEntry, receipt: ShoppingReceipt) -> some View {
        HStack(spacing: 12) {
            ShoppingPictureView(item: entry.item, size: 44)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: entry.item.itemDescription)
                Text(verbatim: entry.line.description)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 2) {
                Text(verbatim: ShoppingMoney.shown(entry.line.lineAmount, currencyCode: receipt.currencyCode))
                    .monospacedDigit()
                if let was = wasText(entry, receipt: receipt) {
                    Text(verbatim: was)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .combine)
        .frame(minHeight: 44)
    }

    /// "was $1.19": what the item cost before this receipt, when that is another price.
    private func wasText(_ entry: ShoppingReceiptEntry, receipt: ShoppingReceipt) -> String? {
        guard let previous = previousPrices[entry.match.id], previous.amount != ShoppingQuantity.decimal(entry.item.unitPriceAmount) else {
            return nil
        }
        return String(localized: "was \(previous.shownAmount)")
    }

    private func noMatchSection(_ receipt: ShoppingReceipt) -> some View {
        Section {
            Text("Nothing on this receipt matched your lists yet. This can take a minute after a receipt is read.")
                .foregroundStyle(.secondary)
            Button("Match again", systemImage: "arrow.clockwise") { Task { await matchAgain() } }
                .actionColor()
                .disabled(isWorking)
        }
    }

    private func proposedSection(_ entries: [ShoppingReceiptEntry], receipt: ShoppingReceipt) -> some View {
        Section {
            ForEach(entries) { entry in
                Button {
                    deciding = entry
                } label: {
                    HStack(spacing: 12) {
                        ShoppingPictureView(item: entry.item, size: 44)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: entry.item.itemDescription)
                            Text(verbatim: entry.line.description)
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                        }
                        Spacer(minLength: 8)
                        Text(verbatim: ShoppingMoney.shown(entry.line.lineAmount, currencyCode: receipt.currencyCode))
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                    .frame(minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .swipeActions(edge: .leading) {
                    Button {
                        Task { await decide(entry, isApplied: true) }
                    } label: {
                        Label("Apply", systemImage: "checkmark")
                    }
                    .tint(.accentColor)
                }
                .swipeActions(edge: .trailing) {
                    Button {
                        Task { await decide(entry, isApplied: false) }
                    } label: {
                        Label("Dismiss", systemImage: "xmark")
                    }
                    .tint(Color(.systemGray))
                }
                .confirmationDialog(
                    "Is this the same item?", isPresented: Binding(get: { deciding?.id == entry.id }, set: { if !$0 { deciding = nil } }),
                    titleVisibility: .visible, presenting: deciding
                ) { chosen in
                    Button("Apply") { Task { await decide(chosen, isApplied: true) } }
                    Button("Dismiss", role: .destructive) { Task { await decide(chosen, isApplied: false) } }
                } message: { chosen in
                    Text("\(chosen.line.description) on the receipt, and \(chosen.item.itemDescription) on \(chosen.item.listName).")
                }
            }
        } header: {
            Text("Maybe the same")
        } footer: {
            Text("Tap one to say if it is the same item. Swipe right to apply it, left to dismiss it.")
        }
    }

    private func notOnListSection(_ lines: [ShoppingReceiptLine], receipt: ShoppingReceipt) -> some View {
        Section {
            ForEach(lines) { line in
                Button {
                    adding = line
                } label: {
                    HStack(spacing: 12) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: line.description)
                            Text(verbatim: ShoppingMoney.shown(line.lineAmount, currencyCode: receipt.currencyCode))
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                        }
                        Spacer(minLength: 8)
                        Image(systemName: "plus.circle")
                            .foregroundStyle(.secondary)
                            .accessibilityHidden(true)
                    }
                    .frame(minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .confirmationDialog(
                    "Add to which list?", isPresented: Binding(get: { adding?.id == line.id }, set: { if !$0 { adding = nil } }),
                    titleVisibility: .visible, presenting: adding
                ) { chosen in
                    ForEach(addableLists) { list in
                        Button(list.listName) { Task { await add(chosen, to: list) } }
                    }
                } message: { chosen in
                    Text("\(chosen.description) is added as an item you do not need now.")
                }
            }
        } header: {
            Text("Bought, not on the list")
        } footer: {
            Text("Tap a line to put it on a list, so its price is remembered.")
        }
    }

    /// The lists an item can be added to: those the receipt touched first, then the rest.
    private var addableLists: [ShoppingList] {
        let touchedIds = (sections?.appliedGroups ?? []).map(\.shoppingListId)
        let touched = lists.filter { touchedIds.contains($0.id) }
        return touched + lists.filter { !touchedIds.contains($0.id) }
    }

    private func stillNeededSection(_ items: [ShoppingItem], receipt: ShoppingReceipt) -> some View {
        Section {
            ForEach(items) { item in
                HStack(spacing: 12) {
                    ShoppingPictureView(item: item, size: 44)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: item.shownTitle)
                        Text(verbatim: item.listName)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                }
                .frame(minHeight: 44)
                .accessibilityElement(children: .combine)
                .swipeActions(edge: .trailing) {
                    Button {
                        Task { await markOutOfStock(item, receipt: receipt) }
                    } label: {
                        Label("Out of stock", systemImage: "exclamationmark.circle")
                    }
                    .tint(.orange)
                }
            }
        } header: {
            Text("Still to buy")
        } footer: {
            Text("Swipe left on an item the store did not have.")
        }
    }

    private var undoSection: some View {
        Section {
            Button("Undo this receipt", systemImage: "arrow.uturn.backward", role: .destructive) { isConfirmingUndo = true }
                .tint(.red)
                .actionColor(isDestructive: true)
                .disabled(isWorking)
                .confirmationDialog("Undo this receipt?", isPresented: $isConfirmingUndo, titleVisibility: .visible) {
                    Button("Undo", role: .destructive) { Task { await undo() } }
                } message: {
                    Text("What it checked off is needed again, and the prices it recorded are taken back.")
                }
        }
    }

    // MARK: What they do

    private func load() async {
        do {
            let read = try await connection.shoppingReceipt(financeReceiptId: financeReceiptId)
            receipt = read
            let readSections = ShoppingReceiptSections(receipt: read)
            sections = readSections
            loadProblemText = nil
            if lists.isEmpty { lists = (try? await connection.shoppingLists()) ?? [] }
            await loadPreviousPrices(of: readSections.appliedGroups.flatMap(\.entries))
        } catch {
            if receipt != nil { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    /// Reads the prices of the checked-off items to say what each cost before, for the first
    /// twenty, and only when the matches are not the ones last read.
    private func loadPreviousPrices(of entries: [ShoppingReceiptEntry]) async {
        let shownEntries = Array(entries.prefix(20))
        let matchIds = shownEntries.map(\.match.id)
        guard matchIds != pricedMatchIds else { return }
        var found: [String: ShoppingItemPrice] = [:]
        for entry in shownEntries where !entry.match.shoppingItemPriceId.isEmpty {
            guard let prices = try? await connection.shoppingItemPrices(shoppingItemId: entry.item.id) else { continue }
            if let previous = ShoppingPriceHistory.previous(before: entry.match.shoppingItemPriceId, in: prices) {
                found[entry.match.id] = previous
            }
        }
        previousPrices = found
        pricedMatchIds = matchIds
    }

    private func decide(_ entry: ShoppingReceiptEntry, isApplied: Bool) async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.decideShoppingReceiptMatch(shoppingReceiptMatchId: entry.match.id, isApplied: isApplied)
            doneText = isApplied ? String(localized: "Applied") : String(localized: "Dismissed")
            await load()
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func add(_ line: ShoppingReceiptLine, to list: ShoppingList) async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.addShoppingItemFromReceiptLine(financeReceiptLineId: line.financeReceiptLineId, shoppingListId: list.id)
            doneText = String(localized: "Added to \(list.listName)")
            await load()
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func markOutOfStock(_ item: ShoppingItem, receipt: ShoppingReceipt) async {
        do {
            try await connection.setShoppingItemOutOfStock(shoppingItemId: item.id, isOutOfStock: true, storeName: receipt.merchantName)
            doneText = String(localized: "Marked out of stock")
            await load()
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func undo() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.undoShoppingReceipt(financeReceiptId: financeReceiptId)
            doneText = String(localized: "Receipt undone")
            await load()
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func matchAgain() async {
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.matchShoppingReceipt(financeReceiptId: financeReceiptId)
            doneText = String(localized: "Matching the receipt. Your lists update when it is done.")
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }
}
