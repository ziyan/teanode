import Charts
import PhotosUI
import SwiftUI
import TeaNodeKit

// An item of a shopping list: its picture, its row, the sheet that changes it
// and the prices it has been seen at.

/// ShoppingPictureView is an item's picture as the server keeps it, read with
/// the app's sign-in, in a rounded square. An item with none shows its first
/// letter. A zoomable picture opens full screen when tapped, to see the
/// product close up; the letter is not a picture and opens nothing.
struct ShoppingPictureView: View {
    let item: ShoppingItem
    /// The side of the square; nil fills the width it is given, as a tile's
    /// picture does.
    var size: CGFloat? = 44
    var isZoomable = false
    @Environment(AppModel.self) private var app: AppModel?
    @State private var picture: UIImage?

    var body: some View {
        Group {
            if isZoomable, let picture {
                Button {
                    LightboxPresenter.present(picture, name: item.itemDescription)
                } label: {
                    square
                }
                // Borderless, not plain: in a list a plain button takes the
                // whole row's taps.
                .buttonStyle(.borderless)
                .accessibilityLabel(Text("See the picture of \(item.itemDescription)"))
            } else {
                square
                    .accessibilityHidden(true)
            }
        }
        .task(id: item.picturePath) {
            picture = nil
            guard item.hasPicture else { return }
            picture = await app?.picture(item.picturePath)
        }
    }

    @ViewBuilder private var square: some View {
        if let size {
            ground
                .frame(width: size, height: size)
                .clipShape(.rect(cornerRadius: size / 5, style: .continuous))
        } else {
            ground
                .frame(maxWidth: .infinity)
                .aspectRatio(1, contentMode: .fit)
                .clipShape(.rect(cornerRadius: 12, style: .continuous))
        }
    }

    /// The picture on its ground, or the first letter. A tile shows the whole
    /// product, not a crop of it.
    private var ground: some View {
        ZStack {
            Color(.tertiarySystemFill)
            if let picture, size == nil {
                Image(uiImage: picture)
                    .resizable()
                    .scaledToFit()
            } else if let picture {
                Image(uiImage: picture)
                    .resizable()
                    .scaledToFill()
            } else if size == nil {
                Text(verbatim: item.initial)
                    .font(.largeTitle)
                    .foregroundStyle(.secondary)
            } else {
                Text(verbatim: item.initial)
                    .font(.headline)
                    .foregroundStyle(.secondary)
            }
        }
    }
}

/// ShoppingListItemRow is an item on a list: its picture, which opens full
/// screen, what it is and what it needs, and about what it costs. A tap on
/// the words opens the item.
struct ShoppingListItemRow: View {
    let item: ShoppingItem
    let onOpen: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        HStack(spacing: 12) {
            ShoppingPictureView(item: item, size: 44, isZoomable: true)
            Button(action: onOpen) {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: item.itemDescription)
                            .foregroundStyle(item.isNotNeeded ? .secondary : .primary)
                        Text(verbatim: ShoppingWords.listItemDetail(item))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        // At the largest sizes the price goes under the words, which
                        // otherwise break mid-word beside it.
                        if dynamicTypeSize.isAccessibilitySize {
                            priceText
                        }
                    }
                    Spacer(minLength: 8)
                    if !dynamicTypeSize.isAccessibilitySize {
                        priceText
                    }
                }
                .frame(minHeight: 44)
                .contentShape(.rect)
            }
            .shoppingWordsButton()
        }
    }

    @ViewBuilder
    private var priceText: some View {
        let price = ShoppingMoney.shownLineTotal(of: item)
        if !price.isEmpty {
            Text(verbatim: price)
                .font(.subheadline)
                .monospacedDigit()
                .foregroundStyle(.secondary)
        }
    }
}

/// ShoppingListItemTile is an item in the pictures layout of a list: the
/// picture across the tile, which opens full screen, and under it what it
/// is, what it needs and about what it costs. A tap on the words opens it.
struct ShoppingListItemTile: View {
    let item: ShoppingItem
    /// The store or aisle the tile is under; the tiles have no headings.
    let groupName: String
    let onOpen: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ShoppingPictureView(item: item, size: nil, isZoomable: true)
            Button(action: onOpen) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: groupName)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Text(verbatim: item.itemDescription)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(item.isNotNeeded ? .secondary : .primary)
                        .lineLimit(2)
                    Text(verbatim: ShoppingWords.listItemDetail(item))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(3)
                    let price = ShoppingMoney.shownLineTotal(of: item)
                    if !price.isEmpty {
                        Text(verbatim: price)
                            .font(.footnote)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(.rect)
            }
            .shoppingWordsButton()
        }
        // Every tile in a row as tall as the tallest, so their grounds line up.
        .frame(maxHeight: .infinity, alignment: .top)
        .padding(8)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 16, style: .continuous))
    }
}

extension View {
    /// The style of a button whose label is an item's words beside other
    /// buttons in a list row: borderless, so each takes only its own taps
    /// (a plain button takes the whole row's), with the words in their own
    /// colors rather than the tint, and lines that wrap kept to the left,
    /// where a button would center them.
    func shoppingWordsButton() -> some View {
        buttonStyle(.borderless)
            .tint(Color.primary)
            .multilineTextAlignment(.leading)
    }
}

// MARK: The item sheet

/// ShoppingItemSheet is one item: its picture, what it is, how many are
/// needed, where to get it and what it costs, with marking it out of stock and
/// taking it off the list.
struct ShoppingItemSheet: View {
    let connection: Connection
    let item: ShoppingItem
    /// Says the item changed, with the item as the server now has it.
    let onChanged: (ShoppingItem) -> Void
    /// Takes the item off the list.
    let onRemove: (ShoppingItem) async -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The item as last read, which the picture and the stock change.
    @State private var shown: ShoppingItem
    @State private var itemDescription: String
    @State private var itemDetails: String
    @State private var quantity: Double
    @State private var quantityUnit: String
    @State private var storeName: String
    @State private var aisleName: String
    @State private var isStoreBrand: Bool
    @State private var unitPriceText: String
    @State private var prices: [ShoppingItemPrice] = []
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var pictureSearchCount = 0
    @State private var isSearchingPicture = false
    @State private var isWorking = false
    @State private var isConfirmingRemove = false
    @State private var problemText: String?
    @State private var doneText: String?

    init(connection: Connection, item: ShoppingItem, onChanged: @escaping (ShoppingItem) -> Void, onRemove: @escaping (ShoppingItem) async -> Void) {
        self.connection = connection
        self.item = item
        self.onChanged = onChanged
        self.onRemove = onRemove
        _shown = State(initialValue: item)
        _itemDescription = State(initialValue: item.itemDescription)
        _itemDetails = State(initialValue: item.itemDetails)
        _quantity = State(initialValue: ShoppingQuantity.value(of: item.neededQuantity))
        _quantityUnit = State(initialValue: item.quantityUnit)
        _storeName = State(initialValue: item.storeName)
        _aisleName = State(initialValue: item.aisleName)
        _isStoreBrand = State(initialValue: item.isStoreBrand)
        _unitPriceText = State(initialValue: ShoppingMoney.typedAmount(item.unitPriceAmount))
    }

    var body: some View {
        NavigationStack {
            Form {
                pictureSection
                itemSection
                quantitySection
                whereSection
                priceSection
                actionsSection
            }
            .navigationTitle("Item")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(isWorking || itemDescription.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            .task { prices = (try? await connection.shoppingItemPrices(shoppingItemId: item.id)) ?? [] }
            .task(id: pictureSearchCount) {
                guard pictureSearchCount > 0 else { return }
                await searchForPicture()
            }
            .onChange(of: pickedItems) { _, picked in
                guard let first = picked.first else { return }
                pickedItems = []
                Task { await upload(first) }
            }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    // MARK: Sections

    private var pictureSection: some View {
        Section {
            Menu {
                Button("Find another", systemImage: "magnifyingglass") { pictureSearchCount += 1 }
                PhotosPicker(selection: $pickedItems, maxSelectionCount: 1, matching: .images) {
                    Label("Choose a photo", systemImage: "photo.on.rectangle")
                }
                if shown.hasPicture {
                    Button("Remove the picture", systemImage: "xmark.circle") { Task { await removePicture() } }
                }
            } label: {
                changePictureLabel
            }
            .disabled(isWorking)
        } header: {
            // The picture stands above the menu's row as the section's
            // header, so the row keeps its rounded card and no divider.
            VStack(spacing: 12) {
                ShoppingPictureView(item: shown, size: 120, isZoomable: true)
                    .frame(maxWidth: .infinity)
                if isSearchingPicture {
                    HStack(spacing: 8) {
                        ProgressView()
                        Text("Looking for another picture…")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .textCase(nil)
            .padding(.bottom, 8)
        }
    }

    /// The words of the picture's menu. At the largest sizes the symbol beside
    /// them left so little room that "Change" broke mid-word, so there they
    /// stand alone.
    @ViewBuilder private var changePictureLabel: some View {
        if dynamicTypeSize.isAccessibilitySize {
            Text("Change picture")
                .actionColor()
        } else {
            Label("Change picture", systemImage: "photo")
                .actionColor()
        }
    }

    private var itemSection: some View {
        Section {
            TextField("What", text: $itemDescription)
            TextField("Details, such as a brand or a size", text: $itemDetails, axis: .vertical)
        }
    }

    private var quantitySection: some View {
        Section("How many") {
            Stepper(value: $quantity, in: 1...999, step: 1) {
                Text(verbatim: String(localized: "Need \(ShoppingQuantity.text(of: quantity))"))
                    .monospacedDigit()
            }
            TextField("Unit, such as lb or bunch", text: $quantityUnit)
        }
    }

    private var whereSection: some View {
        Section("Where") {
            TextField("Store", text: $storeName)
            TextField("Aisle", text: $aisleName)
            Toggle("Store brand", isOn: $isStoreBrand)
        }
    }

    private var priceSection: some View {
        Section {
            LabeledContent("Price of one") {
                TextField("0.00", text: $unitPriceText)
                    .keyboardType(.decimalPad)
                    .multilineTextAlignment(.trailing)
                    .monospacedDigit()
            }
            if ShoppingPriceHistory.chartable(prices).count > 1 {
                ShoppingPriceChart(prices: prices)
            }
            if !prices.isEmpty {
                NavigationLink {
                    ShoppingPricesScreen(prices: prices, itemTitle: shown.itemDescription)
                } label: {
                    Text("All prices")
                }
            }
        } header: {
            Text("Price")
        } footer: {
            if !shown.currencyCode.isEmpty {
                Text("In \(shown.currencyCode)")
            }
        }
    }

    private var actionsSection: some View {
        Section {
            Button(outOfStockTitle, systemImage: "exclamationmark.circle") {
                Task { await toggleOutOfStock() }
            }
            .actionColor()
            .disabled(isWorking)
            Button("Remove from the list", systemImage: "trash", role: .destructive) { isConfirmingRemove = true }
                .tint(.red)
                .actionColor(isDestructive: true)
                .disabled(isWorking)
                .confirmationDialog("Remove \(shown.itemDescription) from the list?", isPresented: $isConfirmingRemove, titleVisibility: .visible) {
                    Button("Remove", role: .destructive) { Task { await remove() } }
                } message: {
                    Text("It is taken off the list for everyone it is shared with.")
                }
        }
    }

    private var outOfStockTitle: String {
        shown.isOutOfStock ? String(localized: "Back in stock") : String(localized: "Mark out of stock")
    }

    // MARK: What they do

    private func searchForPicture() async {
        isSearchingPicture = true
        defer { isSearchingPicture = false }
        let before = shown.picturePath
        do {
            try await connection.findShoppingItemPicture(shoppingItemId: item.id)
            for _ in 0..<20 {
                try await Task.sleep(for: .seconds(3))
                if let fresh = try? await connection.shoppingItem(shoppingItemId: item.id), fresh.picturePath != before {
                    shown = fresh
                    onChanged(fresh)
                    return
                }
            }
            doneText = String(localized: "No other picture yet. It will appear on the item if one is found.")
        } catch is CancellationError {
            return
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func upload(_ picked: PhotosPickerItem) async {
        isWorking = true
        defer { isWorking = false }
        guard let data = try? await picked.loadTransferable(type: Data.self), let file = PendingFile(pictureData: data, index: 0) else {
            problemText = String(localized: "That photo could not be read.")
            return
        }
        do {
            let attachmentId = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data)
            let fresh = try await connection.setShoppingItemPicture(shoppingItemId: item.id, agentAttachmentId: attachmentId)
            shown = fresh
            onChanged(fresh)
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func removePicture() async {
        isWorking = true
        defer { isWorking = false }
        do {
            let fresh = try await connection.setShoppingItemPicture(shoppingItemId: item.id, agentAttachmentId: nil)
            shown = fresh
            onChanged(fresh)
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func toggleOutOfStock() async {
        isWorking = true
        defer { isWorking = false }
        let isOut = !shown.isOutOfStock
        do {
            let fresh = try await connection.setShoppingItemOutOfStock(shoppingItemId: item.id, isOutOfStock: isOut)
            shown = fresh
            onChanged(fresh)
            doneText = isOut ? String(localized: "Marked out of stock") : String(localized: "Back in stock")
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }

    private func remove() async {
        isWorking = true
        await onRemove(shown)
        isWorking = false
        dismiss()
    }

    /// Sends what changed: the item's own fields, and the price when it was typed.
    private func save() async {
        let typedPrice = unitPriceText.trimmingCharacters(in: .whitespaces)
        if !typedPrice.isEmpty, !ShoppingMoney.isPrice(typedPrice) {
            problemText = String(localized: "Type the price as a number, such as 1.99.")
            return
        }
        isWorking = true
        defer { isWorking = false }
        let newQuantity = ShoppingQuantity.text(of: quantity)
        let trimmedDescription = itemDescription.trimmingCharacters(in: .whitespaces)
        let trimmedStore = storeName.trimmingCharacters(in: .whitespaces)
        do {
            var fresh = shown
            let hasChanged =
                trimmedDescription != item.itemDescription || itemDetails != item.itemDetails || trimmedStore != item.storeName
                || aisleName != item.aisleName || isStoreBrand != item.isStoreBrand || newQuantity != item.shownQuantity
                || quantityUnit != item.quantityUnit
            if hasChanged {
                fresh = try await connection.updateShoppingItem(
                    shoppingItemId: item.id, itemDescription: trimmedDescription, itemDetails: itemDetails, storeName: trimmedStore,
                    isStoreBrand: isStoreBrand, aisleName: aisleName, neededQuantity: newQuantity, quantityUnit: quantityUnit)
            }
            if !typedPrice.isEmpty, typedPrice != ShoppingMoney.typedAmount(item.unitPriceAmount) {
                try await connection.recordShoppingItemPrice(
                    shoppingItemId: item.id, unitPriceAmount: typedPrice, storeName: trimmedStore, currencyCode: item.currencyCode)
                fresh = try await connection.shoppingItem(shoppingItemId: item.id)
            }
            onChanged(fresh)
            dismiss()
        } catch {
            problemText = ShoppingWords.problem(error)
        }
    }
}

// MARK: Prices

/// ShoppingPriceChart is an item's price over time, as a line.
struct ShoppingPriceChart: View {
    let prices: [ShoppingItemPrice]

    var body: some View {
        let points = ShoppingPriceHistory.chartable(prices)
        Chart {
            ForEach(points) { price in
                if let date = price.observedDate, let amount = price.amount {
                    LineMark(x: .value("Day", date), y: .value("Price", NSDecimalNumber(decimal: amount).doubleValue))
                        .foregroundStyle(ChartPalette.seriesColor(at: 0))
                        .accessibilityLabel(Text(verbatim: date.formatted(date: .abbreviated, time: .omitted)))
                        .accessibilityValue(Text(verbatim: price.shownAmount))
                    PointMark(x: .value("Day", date), y: .value("Price", NSDecimalNumber(decimal: amount).doubleValue))
                        .foregroundStyle(ChartPalette.seriesColor(at: 0))
                        .symbolSize(24)
                }
            }
        }
        .chartYScale(domain: .automatic(includesZero: false))
        .frame(height: 140)
        .padding(.vertical, 4)
    }
}

/// ShoppingPricesScreen is every price an item has been seen at, newest first.
struct ShoppingPricesScreen: View {
    let prices: [ShoppingItemPrice]
    let itemTitle: String

    var body: some View {
        List {
            ForEach(prices) { price in
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: price.shownAmount)
                            .monospacedDigit()
                        Text(verbatim: ShoppingWords.priceDetail(price))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    if let date = price.observedDate {
                        Text(verbatim: date.formatted(date: .abbreviated, time: .omitted))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        }
        .navigationTitle(itemTitle)
        .navigationBarTitleDisplayMode(.inline)
    }
}
