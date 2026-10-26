import PhotosUI
import SwiftUI
import TeaNodeKit

// Shopping: the person's lists and the lists shared with them, one list at
// home, and (ShoppingStoreViews.swift) the store they are shopping at.

/// ShoppingScreen is the Shopping part: the person's lists, the lists shared
/// with them, and the stores to shop at. A list or a store is pushed on its
/// stack.
struct ShoppingScreen: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var model: ShoppingHomeModel
    @State private var path: [ShoppingPlace] = []
    @State private var isCreating = false
    @State private var newListName = ""

    init(connection: Connection) {
        self.connection = connection
        _model = State(initialValue: ShoppingHomeModel(connection: connection))
    }

    var body: some View {
        NavigationStack(path: $path) {
            lists
                .navigationTitle(ShoppingWords.title)
                .navigationDestination(for: ShoppingPlace.self) { place in
                    ShoppingPlaceScreen(place: place, connection: connection, path: $path)
                }
                .toolbar {
                    MoreBackButton()
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            newListName = ""
                            isCreating = true
                        } label: {
                            Label("New list", systemImage: "plus")
                        }
                    }
                }
                .alert("New list", isPresented: $isCreating) {
                    TextField("List name", text: $newListName)
                    Button("Cancel", role: .cancel) {}
                    Button("Create") { create() }
                } message: {
                    Text("Give the list a name, such as Groceries.")
                }
                .refreshable { await model.load() }
                .task {
                    await model.load()
                    #if DEBUG
                        openDemoScreen()
                    #endif
                }
                .task { await model.follow() }
                .onChange(of: app.openedShopping, initial: true) { _, place in
                    guard let place else { return }
                    app.openedShopping = nil
                    path = [place]
                }
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }

    private var lists: some View {
        List {
            if !model.ownLists.isEmpty {
                Section("Your lists") {
                    ForEach(model.ownLists) { list in
                        NavigationLink(value: ShoppingPlace.list(list.id)) {
                            ShoppingListRow(list: list, granteeNames: model.granteeNames[list.id] ?? [])
                        }
                    }
                }
            }
            if !model.sharedLists.isEmpty {
                Section {
                    ForEach(model.sharedLists) { list in
                        NavigationLink(value: ShoppingPlace.list(list.id)) {
                            ShoppingListRow(list: list, granteeNames: [])
                        }
                    }
                } header: {
                    Text(verbatim: String(localized: "shopping.sharedWithYou", defaultValue: "Shared with you"))
                }
            }
            if !model.stores.isEmpty {
                Section {
                    ForEach(model.stores) { store in
                        NavigationLink(value: ShoppingPlace.store(store.storeName, listIds: [])) {
                            ShoppingStoreRow(store: store)
                        }
                    }
                } header: {
                    Text("Stores")
                }
            }
        }
        .overlay { stateOverlay }
    }

    @ViewBuilder
    private var stateOverlay: some View {
        if let loadFailedText = model.loadFailedText, !model.hasLoaded {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView()
        } else if model.lists.isEmpty {
            ContentUnavailableView(
                "No shopping lists yet", systemImage: AppSection.shopping.systemImage,
                description: Text("Make a list, add what you need, and share it with the people you shop with."))
        }
    }

    private func create() {
        let listName = newListName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !listName.isEmpty else { return }
        Task {
            if let created = await model.create(listName: listName) { path = [.list(created.id)] }
        }
    }

    #if DEBUG
        /// `-TeaNodeScreen shopping-list`, `shopping-item` and `shopping-import` open the
        /// first list (the last two with the item or the import sheet over it), and
        /// `shopping-store` the store screen; `shopping-list-pictures` and
        /// `shopping-store-pictures` show those two as pictures.
        private func openDemoScreen() {
            guard DemoServer.isRequested, path.isEmpty, let screen = DemoServer.screen else { return }
            if screen.hasPrefix("shopping-list") || screen == "shopping-item" || screen == "shopping-import" {
                path = [.list(ShoppingDemo.groceriesListId)]
            } else if screen.hasPrefix("shopping-store") {
                path = [.store(ShoppingDemo.exampleMarket, listIds: [])]
            }
        }
    #endif
}

/// ShoppingPlaceScreen is the screen a place on the Shopping stack is.
struct ShoppingPlaceScreen: View {
    let place: ShoppingPlace
    let connection: Connection
    @Binding var path: [ShoppingPlace]
    @Environment(AppModel.self) private var app

    var body: some View {
        switch place {
        case .list(let listId):
            ShoppingListScreen(connection: connection, listId: listId, path: $path)
        case .store(let storeName, let listIds):
            ShoppingStoreScreen(connection: connection, storeName: storeName, listIds: listIds, ownUserId: app.ownUserId)
        }
    }
}

/// ShoppingNotAllowedView is what Shopping says to an account that may not use
/// it: that is a permission, not a service the server has turned off.
struct ShoppingNotAllowedView: View {
    var body: some View {
        ContentUnavailableView {
            Label("Shopping is not available to you", systemImage: AppSection.shopping.systemImage)
        } description: {
            Text("Your account is not allowed to keep shopping lists. Whoever manages your TeaNode server can allow it.")
        }
    }
}

/// ShoppingListRow is a list: its name, who it is shared with or by, and how
/// many things are to buy.
struct ShoppingListRow: View {
    let list: ShoppingList
    let granteeNames: [String]

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: list.listName)
                if let subtitle = ShoppingWords.listRowSubtitle(list, granteeNames: granteeNames) {
                    Text(verbatim: subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer(minLength: 8)
            if list.neededCount > 0 {
                Text(verbatim: "\(list.neededCount)")
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .accessibilityLabel(Text(verbatim: ShoppingWords.toBuyCount(list.neededCount)))
            }
        }
    }
}

/// ShoppingStoreRow is a store the person's lists have items for, and how many
/// are needed there.
struct ShoppingStoreRow: View {
    let store: ShoppingStore

    var body: some View {
        HStack {
            Text(verbatim: store.storeName)
            Spacer(minLength: 8)
            if store.neededCount > 0 {
                Text(verbatim: "\(store.neededCount)")
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .accessibilityLabel(Text(verbatim: ShoppingWords.toBuyCount(store.neededCount)))
            }
        }
    }
}

// MARK: A list

/// ShoppingListGrouping is how a list's items are laid out: under the stores
/// they are for, or under the aisles they are in.
enum ShoppingListGrouping: Hashable {
    case store
    case aisle
}

/// ShoppingLayoutChoice is how a list or a store lays out its items: as rows,
/// or as tiles with big pictures, which are easier to find a product on the
/// shelf by. The choice is kept on the phone, for every list and store.
enum ShoppingLayoutChoice: String {
    case list
    case pictures

    static let storageKey = "shopping.layout"

    /// The tiles' columns: two side by side on a phone, one at the largest
    /// text sizes, where two leave the words no room.
    static func tileColumns(_ dynamicTypeSize: DynamicTypeSize) -> [GridItem] {
        let minimum: CGFloat = dynamicTypeSize.isAccessibilitySize ? 280 : 150
        return [GridItem(.adaptive(minimum: minimum), spacing: 12)]
    }

    #if DEBUG
        /// The demo's screens ending in `-pictures` show the tiles, and every
        /// other one the rows, whatever an earlier screenshot left chosen.
        static func applyDemo(to layoutName: inout String) {
            guard DemoServer.isRequested, let screen = DemoServer.screen, screen.hasPrefix("shopping") else { return }
            layoutName = screen.hasSuffix("-pictures") ? ShoppingLayoutChoice.pictures.rawValue : ShoppingLayoutChoice.list.rawValue
        }
    #endif
}

/// ShoppingLayoutPicker chooses the layout, in a screen's menu.
struct ShoppingLayoutPicker: View {
    @Binding var layoutName: String

    var body: some View {
        Picker("Layout", selection: $layoutName) {
            Label("List", systemImage: "list.bullet").tag(ShoppingLayoutChoice.list.rawValue)
            Label("Pictures", systemImage: "square.grid.2x2").tag(ShoppingLayoutChoice.pictures.rawValue)
        }
        .pickerStyle(.inline)
    }
}

/// ShoppingListScreen is one list at home: its items under their stores or
/// aisles, as rows or tiles, what is not needed now, and the controls to add,
/// import, share, open a store to shop at and say shopping is done.
struct ShoppingListScreen: View {
    let connection: Connection
    let listId: String
    @Binding var path: [ShoppingPlace]
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var model: ShoppingListModel
    @State private var grouping = ShoppingListGrouping.store
    @State private var editing: ShoppingItem?
    @State private var needingAgain: ShoppingItem?
    @State private var removing: ShoppingItem?
    @State private var isAdding = false
    @State private var isImporting = false
    @State private var isSharing = false
    @State private var isRenaming = false
    @State private var newName = ""
    @State private var isConfirmingDelete = false
    @State private var isChoosingStore = false
    @State private var pickedReceiptItems: [PhotosPickerItem] = []
    @State private var isTakingReceiptPicture = false
    @AppStorage(ShoppingLayoutChoice.storageKey) private var layoutName = ShoppingLayoutChoice.list.rawValue
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(connection: Connection, listId: String, path: Binding<[ShoppingPlace]>) {
        self.connection = connection
        self.listId = listId
        _path = path
        _model = State(initialValue: ShoppingListModel(connection: connection, listId: listId))
    }

    var body: some View {
        // In parts: as one chain of modifiers the body was more than the
        // compiler would type-check in reasonable time.
        screen
            .refreshable { await model.load() }
            .task {
                #if DEBUG
                    ShoppingLayoutChoice.applyDemo(to: &layoutName)
                #endif
                await model.load()
                #if DEBUG
                    openDemoSheet()
                #endif
            }
            .task { await model.follow() }
            .onChange(of: model.isDeleted) { _, isDeleted in
                if isDeleted { dismiss() }
            }
            .modifier(sheets)
            .shoppingReceiptSource(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture) { file in
                Task { await model.receipts.scan(file) }
            }
            .toast(done: $model.doneText, problem: $model.problemText)
    }

    private var screen: some View {
        content
            .navigationTitle(model.list?.listName ?? "")
            .navigationSubtitle(subtitle)
            .navigationBarTitleDisplayMode(.large)
            .toolbar { toolbarItems }
            .safeAreaInset(edge: .bottom) { floatingControls }
            .toolbarVisibility(.hidden, for: .tabBar)
            .overlay { stateOverlay }
    }

    private var subtitle: String {
        guard model.hasLoaded else { return "" }
        return ShoppingWords.listSubtitle(
            model.list, granteeNames: model.granteeNames, toBuyCount: model.neededCount, total: model.estimatedTotal)
    }

    // MARK: Items

    private var content: some View {
        List {
            // The shoppers line and the picker share one section, so the
            // list does not put a section's spacing between them.
            let shoppersLine = ShoppingWords.listShoppersLine(model.trips.openTrips(ofListId: listId, excludingUserId: app.ownUserId))
            if shoppersLine != nil || !model.items.isEmpty {
                Section {
                    if let shoppersLine {
                        Text(verbatim: shoppersLine)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    if !model.items.isEmpty {
                        Picker("Group by", selection: $grouping) {
                            Text("By store").tag(ShoppingListGrouping.store)
                            Text("By aisle").tag(ShoppingListGrouping.aisle)
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()
                    }
                }
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
            }
            ForEach(groups) { group in
                Section {
                    items(group.items)
                } header: {
                    Text(verbatim: groupTitle(group))
                }
            }
            if !model.notNeeded.isEmpty {
                Section("Not needed") {
                    items(model.notNeeded)
                }
            }
        }
    }

    private var groups: [ShoppingGroup] {
        switch grouping {
        case .store: ShoppingGrouping.byStore(model.toBuy)
        case .aisle: ShoppingGrouping.byAisle(model.toBuy)
        }
    }

    private func groupTitle(_ group: ShoppingGroup) -> String {
        switch grouping {
        case .store: ShoppingWords.storeTitle(group)
        case .aisle: ShoppingWords.aisleTitle(group)
        }
    }

    /// A section's items: a row each, or tiles in a grid that fills the
    /// section's one row.
    @ViewBuilder private func items(_ sectionItems: [ShoppingItem]) -> some View {
        if layoutName == ShoppingLayoutChoice.pictures.rawValue {
            LazyVGrid(columns: ShoppingLayoutChoice.tileColumns(dynamicTypeSize), spacing: 12) {
                ForEach(sectionItems) { item in tile(item) }
            }
            .listRowInsets(EdgeInsets())
            .listRowBackground(Color.clear)
        } else {
            ForEach(sectionItems) { item in row(item) }
        }
    }

    /// A tile has no swipe: Need again and Remove are in its menu.
    private func tile(_ item: ShoppingItem) -> some View {
        ShoppingListItemTile(item: item) { editing = item }
            .contextMenu {
                if item.isNotNeeded {
                    Button("Need again", systemImage: "arrow.uturn.backward") { needingAgain = item }
                }
                Button("Remove", systemImage: "trash", role: .destructive) { removing = item }
                    .tint(.red)
            }
            .confirmationDialog(
                "Remove from the list", isPresented: Binding(get: { removing?.id == item.id }, set: { if !$0 { removing = nil } }),
                titleVisibility: .visible, presenting: removing
            ) { chosen in
                Button("Remove", role: .destructive) { Task { await model.remove(chosen) } }
            } message: { chosen in
                Text("\(chosen.itemDescription) is taken off the list for everyone it is shared with.")
            }
    }

    private func row(_ item: ShoppingItem) -> some View {
        ShoppingListItemRow(item: item) { editing = item }
            .swipeActions(edge: .leading) {
                if item.isNotNeeded {
                    Button {
                        needingAgain = item
                    } label: {
                        Label("Need again", systemImage: "arrow.uturn.backward")
                    }
                    .tint(.accentColor)
                }
            }
            .swipeActions(edge: .trailing) {
                Button(role: .destructive) {
                    removing = item
                } label: {
                    Label("Remove", systemImage: "trash")
                }
                .tint(.red)
            }
            .confirmationDialog(
                "Remove from the list", isPresented: Binding(get: { removing?.id == item.id }, set: { if !$0 { removing = nil } }),
                titleVisibility: .visible, presenting: removing
            ) { chosen in
                Button("Remove", role: .destructive) { Task { await model.remove(chosen) } }
            } message: { chosen in
                Text("\(chosen.itemDescription) is taken off the list for everyone it is shared with.")
            }
    }

    @ViewBuilder
    private var stateOverlay: some View {
        if let loadFailedText = model.loadFailedText, !model.hasLoaded {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView()
        } else if model.items.isEmpty {
            ContentUnavailableView(
                "Nothing on this list", systemImage: AppSection.shopping.systemImage,
                description: Text("Add an item, or import a list from text or a picture."))
        }
    }

    // MARK: Toolbar and floating controls

    @ToolbarContentBuilder
    private var toolbarItems: some ToolbarContent {
        if model.list?.isOwner == true {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isSharing = true
                } label: {
                    Label("Share", systemImage: "person.badge.plus")
                }
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            menu
        }
    }

    private var menu: some View {
        Menu {
            ShoppingLayoutPicker(layoutName: $layoutName)
            if model.isShopping(ownUserId: app.ownUserId) {
                Button("Shop at a store", systemImage: "cart") { chooseStore() }
            }
            Button("Import", systemImage: "square.and.arrow.down") { isImporting = true }
            Menu("Upload a receipt", systemImage: "doc.text.viewfinder") {
                ShoppingReceiptSourceChoices(pickedItems: $pickedReceiptItems, isTakingPicture: $isTakingReceiptPicture)
            }
            Toggle("Remind me at stores", isOn: reminderBinding)
            if model.list?.isOwner == true {
                Button("Rename", systemImage: "pencil") {
                    newName = model.list?.listName ?? ""
                    isRenaming = true
                }
                Button("Delete", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                    .tint(.red)
            }
        } label: {
            Label("More", systemImage: "ellipsis")
        }
        .confirmationDialog("Delete this list?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { Task { _ = await model.delete() } }
        } message: {
            Text("The list and its items are deleted for everyone it is shared with.")
        }
    }

    private var reminderBinding: Binding<Bool> {
        Binding(get: { model.isReminded }, set: { isOn in Task { await model.setReminder(isOn) } })
    }

    private var floatingControls: some View {
        HStack(spacing: 12) {
            // There is no starting: the first item put in the cart starts the
            // person's trip. Done shows once there is something to buy.
            Button {
                if model.isShopping(ownUserId: app.ownUserId) {
                    Task { await model.finish() }
                } else {
                    chooseStore()
                }
            } label: {
                Group {
                    if model.isShopping(ownUserId: app.ownUserId) {
                        Text("Done shopping")
                    } else {
                        Text("Shop at a store")
                    }
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.large)
            .disabled(model.neededCount == 0 && !model.isShopping(ownUserId: app.ownUserId))
            .confirmationDialog("Where are you shopping?", isPresented: $isChoosingStore, titleVisibility: .visible) {
                ForEach(model.storeChoices, id: \.self) { storeName in
                    Button(storeName) { open(storeName: storeName) }
                }
                Button("Any store") { open(storeName: "") }
            }
            Button {
                isAdding = true
            } label: {
                Image(systemName: "plus")
                    .font(.title3.weight(.semibold))
                    .frame(width: 52, height: 52)
                    .glassEffect(.regular.interactive(), in: .circle)
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .foregroundStyle(Color.accentColor)
            .accessibilityLabel(Text("Add an item"))
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        // Floating buttons stop growing before they cover the items under them.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }

    /// Opens the store to shop at: the only one the list has something to
    /// buy at, any store when it has none, and the person's choice when it
    /// has several. Opening it starts nothing.
    private func chooseStore() {
        Task {
            await model.loadStores()
            let choices = model.storeChoices
            if choices.count > 1 {
                isChoosingStore = true
            } else {
                open(storeName: choices.first ?? "")
            }
        }
    }

    private func open(storeName: String) {
        path.append(.store(storeName, listIds: [listId]))
    }

    // MARK: Sheets

    private var sheets: ShoppingListSheets {
        ShoppingListSheets(
            connection: connection, model: model, listId: listId, editing: $editing, needingAgain: $needingAgain, isAdding: $isAdding,
            isImporting: $isImporting, isSharing: $isSharing, isRenaming: $isRenaming, newName: $newName)
    }

    #if DEBUG
        /// `-TeaNodeScreen shopping-item` opens the apples, `shopping-import` the import sheet.
        private func openDemoSheet() {
            guard DemoServer.isRequested else { return }
            if DemoServer.screen == "shopping-item" {
                editing = model.items.first { $0.itemDescription == "Apples" }
            } else if DemoServer.screen == "shopping-import" {
                isImporting = true
            }
        }
    #endif
}

/// ShoppingListSheets are what a list opens over itself: an item, how many to
/// need again, adding an item, importing, sharing and renaming.
struct ShoppingListSheets: ViewModifier {
    let connection: Connection
    let model: ShoppingListModel
    let listId: String
    @Binding var editing: ShoppingItem?
    @Binding var needingAgain: ShoppingItem?
    @Binding var isAdding: Bool
    @Binding var isImporting: Bool
    @Binding var isSharing: Bool
    @Binding var isRenaming: Bool
    @Binding var newName: String

    func body(content: Content) -> some View {
        content
            .sheet(item: $editing) { item in
                ShoppingItemSheet(
                    connection: connection, item: item, onChanged: { model.replace($0) },
                    onRemove: { removed in await model.remove(removed) })
            }
            .sheet(item: $needingAgain) { item in
                ShoppingNeedAgainSheet(item: item) { quantity in Task { await model.needAgain(item, quantity: quantity) } }
            }
            .sheet(isPresented: $isAdding) {
                ShoppingAddItemSheet { entry in await model.add([entry]) }
            }
            .sheet(isPresented: $isImporting) {
                ShoppingImportSheet(connection: connection, initialListId: listId) { Task { await model.load() } }
            }
            .sheet(isPresented: $isSharing) {
                ShoppingShareSheet(connection: connection, model: model)
            }
            .sheet(item: Bindable(model.receipts).target) { target in
                ShoppingReceiptSheet(connection: connection, financeReceiptId: target.financeReceiptId)
            }
            .alert("Rename the list", isPresented: $isRenaming) {
                TextField("List name", text: $newName)
                Button("Cancel", role: .cancel) {}
                Button("Save") {
                    let listName = newName.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !listName.isEmpty else { return }
                    Task { await model.rename(listName) }
                }
            }
    }
}

// MARK: Small sheets

/// ShoppingNeedAgainSheet asks how many of a bought item to need again.
struct ShoppingNeedAgainSheet: View {
    let item: ShoppingItem
    let onConfirm: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var quantity: Double

    init(item: ShoppingItem, onConfirm: @escaping (String) -> Void) {
        self.item = item
        self.onConfirm = onConfirm
        _quantity = State(initialValue: ShoppingQuantity.value(of: item.neededQuantity))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Stepper(value: $quantity, in: 1...999, step: 1) {
                        Text(verbatim: String(localized: "Need \(ShoppingQuantity.text(of: quantity))"))
                            .monospacedDigit()
                    }
                } header: {
                    Text(verbatim: item.itemDescription)
                }
            }
            .navigationTitle("Need again")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") {
                        onConfirm(ShoppingQuantity.text(of: quantity))
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium])
    }
}

/// ShoppingAddItemSheet adds one item to a list: what it is, how many, and
/// where to get it.
struct ShoppingAddItemSheet: View {
    /// Adds the item; true when it was added.
    let onAdd: (ShoppingItemEntry) async -> Bool
    @Environment(\.dismiss) private var dismiss
    @State private var itemDescription = ""
    @State private var itemDetails = ""
    @State private var quantity = 1.0
    @State private var quantityUnit = ""
    @State private var storeName = ""
    @State private var isWorking = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("What", text: $itemDescription)
                    TextField("Details, such as a brand or a size", text: $itemDetails, axis: .vertical)
                }
                Section("How many") {
                    Stepper(value: $quantity, in: 1...999, step: 1) {
                        Text(verbatim: String(localized: "Need \(ShoppingQuantity.text(of: quantity))"))
                            .monospacedDigit()
                    }
                    TextField("Unit, such as lb or bunch", text: $quantityUnit)
                }
                Section {
                    TextField("Store", text: $storeName)
                } header: {
                    Text("Where")
                } footer: {
                    Text("Leave the store empty for an item any store has.")
                }
            }
            .navigationTitle("Add an item")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add") { add() }
                        .disabled(isWorking || itemDescription.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
        .presentationDetents([.large])
    }

    private func add() {
        let entry = ShoppingItemEntry(
            itemDescription: itemDescription.trimmingCharacters(in: .whitespaces), itemDetails: itemDetails,
            storeName: storeName.trimmingCharacters(in: .whitespaces), neededQuantity: ShoppingQuantity.text(of: quantity),
            quantityUnit: quantityUnit)
        isWorking = true
        Task {
            let isAdded = await onAdd(entry)
            isWorking = false
            if isAdded { dismiss() }
        }
    }
}
