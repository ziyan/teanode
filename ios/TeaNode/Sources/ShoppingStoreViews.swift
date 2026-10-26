import PhotosUI
import SwiftUI
import TeaNodeKit

// Shopping at a store: the screen a person keeps open while they walk the
// aisles. One screen holds the needed items of every list they chose that are
// for the store or for any store, each a big check to put in the cart, and it
// follows the others shopping the same lists as they check things off.

/// ShoppingStoreScreen is the store being shopped: how far along the cart is,
/// the lists included, and the items by aisle, as rows or as tiles with big
/// pictures. Putting the first item in the cart starts the person's trip;
/// Done buys the cart, with or without a receipt.
struct ShoppingStoreScreen: View {
    let connection: Connection
    let storeName: String
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var model: ShoppingStoreModel
    @State private var pickedReceiptItems: [PhotosPickerItem] = []
    @State private var isTakingReceiptPicture = false
    @AppStorage(ShoppingLayoutChoice.storageKey) private var layoutName = ShoppingLayoutChoice.list.rawValue
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(connection: Connection, storeName: String, listIds: [String], ownUserId: String?) {
        self.connection = connection
        self.storeName = storeName
        _model = State(
            initialValue: ShoppingStoreModel(connection: connection, storeName: storeName, listIds: listIds, ownUserId: ownUserId))
    }

    var body: some View {
        screen
            .refreshable { await model.load() }
            .task {
                #if DEBUG
                    ShoppingLayoutChoice.applyDemo(to: &layoutName)
                #endif
                await model.load()
            }
            .task { await model.follow() }
            .sheet(item: Bindable(model.receipts).target) { target in
                ShoppingReceiptSheet(connection: connection, financeReceiptId: target.financeReceiptId)
            }
            .shoppingReceiptSource(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture) { file in
                Task { await model.receipts.scan(file) }
            }
            .toast(done: $model.doneText, problem: $model.problemText)
    }

    private var screen: some View {
        content
            .navigationTitle(storeName.isEmpty ? String(localized: "Any store") : storeName)
            .navigationBarTitleDisplayMode(.large)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    menu
                }
            }
            .safeAreaInset(edge: .bottom) { bottomControls }
            .toolbarVisibility(.hidden, for: .tabBar)
            .overlay { stateOverlay }
    }

    // MARK: Items

    private var content: some View {
        let sections = model.sections
        return List {
            // The summary and the list buttons share one section, so the
            // list does not put a section's spacing between them.
            Section {
                header
                if model.lists.count > 1 {
                    listButtons
                        .listRowInsets(EdgeInsets())
                }
            }
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
            if layoutName == ShoppingLayoutChoice.pictures.rawValue {
                // The tiles run on in aisle order without headings, so an
                // aisle of one item does not leave half a row empty; each tile
                // says which aisle it is in instead.
                if !sections.aisleGroups.isEmpty || !sections.anyStoreItems.isEmpty {
                    Section {
                        tileGrid(placedTiles(sections))
                    }
                }
            } else {
                ForEach(sections.aisleGroups) { group in
                    Section {
                        ForEach(group.items) { item in row(item) }
                    } header: {
                        Text(verbatim: ShoppingWords.aisleTitle(group))
                    }
                }
                if !sections.anyStoreItems.isEmpty {
                    Section("Also on the list, any store") {
                        ForEach(sections.anyStoreItems) { item in row(item) }
                    }
                }
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: ShoppingWords.progressLine(model.progress))
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .contentTransition(.numericText())
            if let shoppersLine = ShoppingWords.otherShoppersLine(model.otherShopperNames) {
                Text(verbatim: shoppersLine)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let readingText = model.receipts.readingText {
                HStack(spacing: 8) {
                    ProgressView()
                    Text(verbatim: readingText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .animation(reduceMotion ? nil : .default, value: model.progress)
    }

    /// One toggle for each list that has items here: a list switched off is
    /// left out of the screen.
    private var listButtons: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(model.lists) { list in
                    Toggle(isOn: includedBinding(list)) {
                        Text(verbatim: "\(list.listName) · \(list.neededCount)")
                    }
                    .toggleStyle(.button)
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 4)
        }
    }

    private func includedBinding(_ list: ShoppingStoreList) -> Binding<Bool> {
        Binding(get: { list.isIncluded }, set: { isOn in Task { await model.setIncluded(list, isIncluded: isOn) } })
    }

    /// Every item in aisle order, the ones for any store last, each with the
    /// aisle its tile names.
    private func placedTiles(_ sections: ShoppingStoreSections) -> [ShoppingPlacedItem] {
        let anyStoreName = String(localized: "Any store")
        var placed: [ShoppingPlacedItem] = []
        for group in sections.aisleGroups {
            let groupName = ShoppingWords.aisleTitle(group)
            for item in group.items { placed.append(ShoppingPlacedItem(item: item, groupName: groupName)) }
        }
        for item in sections.anyStoreItems { placed.append(ShoppingPlacedItem(item: item, groupName: anyStoreName)) }
        return placed
    }

    /// Tiles in a grid that fills the section's one row.
    private func tileGrid(_ placed: [ShoppingPlacedItem]) -> some View {
        LazyVGrid(columns: ShoppingLayoutChoice.tileColumns(dynamicTypeSize), spacing: 12) {
            ForEach(placed) { entry in tile(entry.item, groupName: entry.groupName) }
        }
        .listRowInsets(EdgeInsets())
        .listRowBackground(Color.clear)
    }

    private func tile(_ item: ShoppingItem, groupName: String) -> some View {
        ShoppingStoreItemTile(
            item: item, groupName: groupName, isChecked: model.isChecked(item), showsListName: model.includedCount > 1
        ) {
            Task { await model.toggle(item) }
        }
        .contextMenu {
            Button(outOfStockTitle(item), systemImage: "exclamationmark.circle") {
                Task { await model.setOutOfStock(item, isOutOfStock: !item.isOutOfStock) }
            }
        }
    }

    private func row(_ item: ShoppingItem) -> some View {
        ShoppingStoreItemRow(item: item, isChecked: model.isChecked(item), showsListName: model.includedCount > 1) {
            Task { await model.toggle(item) }
        }
        .swipeActions(edge: .trailing) {
            Button {
                Task { await model.setOutOfStock(item, isOutOfStock: !item.isOutOfStock) }
            } label: {
                Label(outOfStockTitle(item), systemImage: "exclamationmark.circle")
            }
            .tint(.orange)
        }
    }

    private func outOfStockTitle(_ item: ShoppingItem) -> String {
        item.isOutOfStock ? String(localized: "In stock again") : String(localized: "Out of stock")
    }

    @ViewBuilder
    private var stateOverlay: some View {
        if let loadFailedText = model.loadFailedText, !model.hasLoaded {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView()
        } else if model.sections.aisleGroups.isEmpty, model.sections.anyStoreItems.isEmpty {
            ContentUnavailableView(
                "Nothing to buy here", systemImage: AppSection.shopping.systemImage,
                description: Text("Everything on the lists you chose is in the cart or not needed."))
        }
    }

    // MARK: Controls

    private var menu: some View {
        Menu {
            ShoppingLayoutPicker(layoutName: $layoutName)
            Toggle("Remind me when I arrive here", isOn: reminderBinding)
                .disabled(storeName.isEmpty)
        } label: {
            Label("More", systemImage: "ellipsis")
        }
    }

    private var reminderBinding: Binding<Bool> {
        Binding(get: { model.isReminded }, set: { isOn in Task { await model.setReminder(isOn) } })
    }

    /// The buttons over the bottom of the screen. While the person is
    /// shopping, Done is the one prominent button, and needs no receipt; the
    /// receipt is the round button beside it. Before that, scanning a receipt
    /// is the screen's one action.
    @ViewBuilder private var bottomControls: some View {
        Group {
            if model.isShopping {
                HStack(spacing: 12) {
                    Button {
                        finish()
                    } label: {
                        Text("Done shopping")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                    Menu {
                        ShoppingReceiptSourceChoices(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture)
                    } label: {
                        Image(systemName: "doc.text.viewfinder")
                            .font(.title3.weight(.semibold))
                            .frame(width: 52, height: 52)
                            .glassEffect(.regular.interactive(), in: .circle)
                            .contentShape(.circle)
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(Color.accentColor)
                    .disabled(model.receipts.readingText != nil)
                    .accessibilityLabel(Text("Scan the receipt"))
                }
            } else {
                Menu {
                    ShoppingReceiptSourceChoices(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture)
                } label: {
                    Label("Scan the receipt", systemImage: "doc.text.viewfinder")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.capsule)
                .controlSize(.large)
                .disabled(model.receipts.readingText != nil)
            }
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        // Floating buttons stop growing before they cover the items under them.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }

    private func finish() {
        Task {
            if await model.finish() { dismiss() }
        }
    }
}

/// ShoppingStoreItemRow is an item to find at the store: a big check, its
/// picture, which opens full screen, what and how many, and who has it in the
/// cart or found it out of stock. A tap on the check or the words puts it in
/// the cart or takes it out.
struct ShoppingStoreItemRow: View {
    let item: ShoppingItem
    let isChecked: Bool
    let showsListName: Bool
    let onToggle: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(spacing: 12) {
            Button(action: onToggle) {
                ShoppingCheckMark(isChecked: isChecked)
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.borderless)
            // The words below say the same and do the same.
            .accessibilityHidden(true)
            // At the largest sizes the words need the width the picture takes.
            if !dynamicTypeSize.isAccessibilitySize {
                ShoppingPictureView(item: item, size: 44, isZoomable: true)
            }
            Button(action: onToggle) {
                HStack(spacing: 0) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: item.shownTitle)
                            .foregroundStyle(isChecked ? .secondary : .primary)
                        detailText
                    }
                    // Inside the label: a button sets its own alignment there,
                    // which centered lines that wrap.
                    .multilineTextAlignment(.leading)
                    Spacer(minLength: 0)
                }
                .frame(minHeight: 44)
                .contentShape(.rect)
            }
            .shoppingWordsButton()
            .accessibilityValue(Text(verbatim: isChecked ? String(localized: "In the cart") : String(localized: "Not in the cart")))
            .accessibilityAddTraits(isChecked ? .isSelected : [])
        }
    }

    @ViewBuilder
    private var detailText: some View {
        let detail = ShoppingWords.storeItemDetail(item, isChecked: isChecked, showsListName: showsListName)
        if !detail.isEmpty {
            Text(verbatim: detail)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
    }
}

/// ShoppingCheckMark is the check that says an item is in the cart.
struct ShoppingCheckMark: View {
    let isChecked: Bool

    var body: some View {
        Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
            .font(.title2)
            .foregroundStyle(isChecked ? Color.accentColor : Color.secondary)
            .symbolEffect(.bounce, value: isChecked)
            .frame(width: 32)
    }
}

/// ShoppingStoreItemTile is an item at the store in the pictures layout: the
/// picture across the tile, which opens full screen, and under it the check
/// with what and how many. A tap on the check or the words puts it in the
/// cart or takes it out.
struct ShoppingStoreItemTile: View {
    let item: ShoppingItem
    /// The aisle the tile is in; the tiles have no headings.
    let groupName: String
    let isChecked: Bool
    let showsListName: Bool
    let onToggle: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ShoppingPictureView(item: item, size: nil, isZoomable: true)
                .opacity(isChecked ? 0.6 : 1)
            Text(verbatim: groupName)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Button(action: onToggle) {
                HStack(alignment: .top, spacing: 6) {
                    ShoppingCheckMark(isChecked: isChecked)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: item.shownTitle)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(isChecked ? .secondary : .primary)
                            .lineLimit(2)
                        detailText
                    }
                    .multilineTextAlignment(.leading)
                    Spacer(minLength: 0)
                }
                .contentShape(.rect)
            }
            .shoppingWordsButton()
            .accessibilityValue(Text(verbatim: isChecked ? String(localized: "In the cart") : String(localized: "Not in the cart")))
            .accessibilityAddTraits(isChecked ? .isSelected : [])
        }
        // Every tile in a row as tall as the tallest, so their grounds line up.
        .frame(maxHeight: .infinity, alignment: .top)
        .padding(8)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 16, style: .continuous))
    }

    @ViewBuilder
    private var detailText: some View {
        let detail = ShoppingWords.storeItemDetail(item, isChecked: isChecked, showsListName: showsListName)
        if !detail.isEmpty {
            Text(verbatim: detail)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(3)
        }
    }
}

// MARK: Scanning a receipt

/// ShoppingReceiptSourceChoices are the two ways to give a receipt: a photo
/// from the library, or the camera. They go in a menu.
struct ShoppingReceiptSourceChoices: View {
    @Binding var pickedItems: [PhotosPickerItem]
    @Binding var isTakingPicture: Bool

    var body: some View {
        PhotosPicker(selection: $pickedItems, maxSelectionCount: 1, matching: .images) {
            Label("Photo library", systemImage: "photo.on.rectangle")
        }
        if UIImagePickerController.isSourceTypeAvailable(.camera) {
            Button("Camera", systemImage: "camera") { isTakingPicture = true }
        }
    }
}

/// ShoppingReceiptSource hands over the picture of a receipt the person chose
/// or took.
struct ShoppingReceiptSource: ViewModifier {
    @Binding var pickedItems: [PhotosPickerItem]
    @Binding var isTakingPicture: Bool
    let onFile: (PendingFile) -> Void

    func body(content: Content) -> some View {
        content
            .onChange(of: pickedItems) { _, items in
                guard let item = items.first else { return }
                pickedItems = []
                Task {
                    if let data = try? await item.loadTransferable(type: Data.self), let file = PendingFile(pictureData: data, index: 0) {
                        onFile(file)
                    }
                }
            }
            .cameraPicker(isPresented: $isTakingPicture) { picture in
                if let file = PendingFile(picture: picture, index: 0) { onFile(file) }
            }
    }
}

extension View {
    /// Calls `onFile` with the picture of a receipt chosen from the library or taken with the camera.
    func shoppingReceiptSource(
        pickedItems: Binding<[PhotosPickerItem]>, isTakingPicture: Binding<Bool>, onFile: @escaping (PendingFile) -> Void
    ) -> some View {
        modifier(ShoppingReceiptSource(pickedItems: pickedItems, isTakingPicture: isTakingPicture, onFile: onFile))
    }
}
