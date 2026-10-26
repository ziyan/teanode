import PhotosUI
import SwiftUI
import TeaNodeKit

// Shopping at a store: the screen a person keeps open while they walk the
// aisles. One screen holds the needed items of every list they chose that are
// for the store or for any store, each a big check to put in the cart, and it
// follows the others shopping the same lists as they check things off.

/// ShoppingStoreScreen is the store being shopped: how far along the cart is,
/// the lists included, and the items by aisle. Opening it starts the person's
/// trip; Done ends it.
struct ShoppingStoreScreen: View {
    let connection: Connection
    let storeName: String
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var model: ShoppingStoreModel
    @State private var pickedReceiptItems: [PhotosPickerItem] = []
    @State private var isTakingReceiptPicture = false

    init(connection: Connection, storeName: String, listIds: [String], ownUserId: String?) {
        self.connection = connection
        self.storeName = storeName
        _model = State(
            initialValue: ShoppingStoreModel(connection: connection, storeName: storeName, listIds: listIds, ownUserId: ownUserId))
    }

    var body: some View {
        screen
            .refreshable { await model.load() }
            .task { await model.load() }
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
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { finish() }
                }
            }
            .safeAreaInset(edge: .bottom) { scanButton }
            .toolbarVisibility(.hidden, for: .tabBar)
            .overlay { stateOverlay }
    }

    // MARK: Items

    private var content: some View {
        let sections = model.sections
        return List {
            Section {
                header
            }
            .listRowBackground(Color.clear)
            if model.lists.count > 1 {
                Section {
                    listButtons
                }
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
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
            Toggle("Remind me when I arrive here", isOn: reminderBinding)
                .disabled(storeName.isEmpty)
        } label: {
            Label("More", systemImage: "ellipsis")
        }
    }

    private var reminderBinding: Binding<Bool> {
        Binding(get: { model.isReminded }, set: { isOn in Task { await model.setReminder(isOn) } })
    }

    /// The screen's one prominent button: a picture of the receipt, from the
    /// camera or the library.
    private var scanButton: some View {
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
        .padding(.horizontal)
        .padding(.bottom, 8)
    }

    private func finish() {
        Task {
            if await model.finish() { dismiss() }
        }
    }
}

/// ShoppingStoreItemRow is an item to find at the store: a big check, its
/// picture, what and how many, and who has it in the cart or found it out of
/// stock. A tap anywhere on it puts it in the cart or takes it out.
struct ShoppingStoreItemRow: View {
    let item: ShoppingItem
    let isChecked: Bool
    let showsListName: Bool
    let onToggle: () -> Void

    var body: some View {
        Button(action: onToggle) {
            HStack(spacing: 12) {
                Image(systemName: isChecked ? "checkmark.circle.fill" : "circle")
                    .font(.title2)
                    .foregroundStyle(isChecked ? Color.accentColor : Color.secondary)
                    .symbolEffect(.bounce, value: isChecked)
                    .frame(width: 32)
                ShoppingPictureView(item: item, size: 44)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: item.shownTitle)
                        .foregroundStyle(isChecked ? .secondary : .primary)
                    detailText
                }
                Spacer(minLength: 0)
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityValue(Text(verbatim: isChecked ? String(localized: "In the cart") : String(localized: "Not in the cart")))
        .accessibilityAddTraits(isChecked ? .isSelected : [])
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
