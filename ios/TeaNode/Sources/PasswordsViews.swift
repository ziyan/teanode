import SwiftUI
import TeaNodeKit

// The Passwords screen: logins, cards and notes that only the person's devices
// can read. The screen is a stack that opens on whatever Passwords is in:
// setting it up, closed (locked), or open with the list of items. An item, the
// recently deleted items, sharing and the settings are pushed on it; the editor
// is a sheet.

/// PasswordsDestination is where the Passwords screen can push to.
enum PasswordsDestination: Hashable {
    case setUp
    case item(String)
    case share(String)
    /// The earlier versions of an item the person owns.
    case versions(String)
    /// One earlier version of an item, read-only, with the button that puts it back.
    case version(itemId: String, versionId: String)
    case deleted
    case settings

    /// Whether this is a screen of earlier versions, which are left together when one is restored.
    var isVersionScreen: Bool {
        switch self {
        case .versions, .version: true
        default: false
        }
    }
}

/// PasswordsEditing is an editor on screen: a new item of a kind, or a change
/// to an item the person owns.
struct PasswordsEditing: Identifiable {
    let id = UUID()
    let itemKind: PasswordsItemKind
    let item: PasswordItem?
}

/// PasswordsScreen is the person's Passwords.
struct PasswordsScreen: View {
    @Bindable var model: PasswordsModel
    @State private var path: [PasswordsDestination] = []
    @State private var searchText = ""
    @State private var filter = PasswordsFilter.everything
    @State private var editing: PasswordsEditing?
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        framed
            .toast(done: $model.doneText, problem: $model.problemText)
            .task {
                await model.start()
                #if DEBUG
                    // When start() ends on a phase that took no waiting to reach (the screens before set up), the
                    // phase change below has not run yet, and it would clear the path set here. Let it run first.
                    try? await Task.sleep(for: .milliseconds(300))
                    openDemoScreen()
                #endif
            }
            // Locks itself once the lock time has passed without use.
            .task(id: model.phase) { await watchLockTime() }
            // Whatever was pushed belongs to the state Passwords was in.
            .onChange(of: model.phase) {
                path = []
                editing = nil
            }
            // Coming back to the app from another one asks for Face ID again if Passwords locked meanwhile.
            .onChange(of: scenePhase) { previous, current in
                if previous == .background, current == .active, model.phase == .locked {
                    Task { await model.start() }
                }
            }
    }

    /// The stack, the editor over it, and the cover for the app switcher.
    private var framed: some View {
        NavigationStack(path: $path) {
            content
                .toolbar { toolbarContent }
                .navigationDestination(for: PasswordsDestination.self) { destination in
                    destinationView(destination)
                }
        }
        .sheet(item: $editing) { shown in
            PasswordsEditorScreen(model: model, editing: shown)
        }
        // The picture iOS keeps of the app for the app switcher must not show passwords.
        .passwordsAppSwitcherCover(model: model)
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        switch model.phase {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .navigationTitle(PasswordsWords.title)
                .navigationBarTitleDisplayMode(.inline)
        case .failed:
            LoadFailedView(problemText: model.failedText ?? "") { await model.start() }
                .navigationTitle(PasswordsWords.title)
                .navigationBarTitleDisplayMode(.inline)
        case .notSetUp:
            PasswordsIntroView { path.append(.setUp) }
        case .recoveryKey:
            PasswordsRecoveryKeyScreen(model: model)
        case .locked:
            PasswordsUnlockScreen(model: model)
        case .unlocked:
            PasswordsListView(model: model, filter: $filter, searchText: $searchText) { itemKind in
                editing = PasswordsEditing(itemKind: itemKind, item: nil)
            }
        }
    }

    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        MoreBackButton()
        ToolbarItemGroup(placement: .topBarTrailing) {
            switch model.phase {
            case .unlocked:
                newItemMenu
                optionsMenu
            case .locked, .notSetUp:
                NavigationLink(value: PasswordsDestination.settings) {
                    Label("Passwords settings", systemImage: "gearshape")
                }
            default:
                EmptyView()
            }
        }
    }

    private var newItemMenu: some View {
        Menu {
            Button {
                editing = PasswordsEditing(itemKind: .login, item: nil)
            } label: {
                Label("New login", systemImage: PasswordsWords.kindSystemImage(.login))
            }
            Button {
                editing = PasswordsEditing(itemKind: .card, item: nil)
            } label: {
                Label("New card", systemImage: PasswordsWords.kindSystemImage(.card))
            }
            Button {
                editing = PasswordsEditing(itemKind: .note, item: nil)
            } label: {
                Label("New note", systemImage: PasswordsWords.kindSystemImage(.note))
            }
        } label: {
            Label("New item", systemImage: "plus")
        }
    }

    private var optionsMenu: some View {
        Menu {
            Button {
                Task { await model.lock() }
            } label: {
                Label("Lock now", systemImage: "lock")
            }
            Button {
                path.append(.deleted)
            } label: {
                Label("Recently deleted", systemImage: "trash")
            }
            Button {
                path.append(.settings)
            } label: {
                Label("Settings", systemImage: "gearshape")
            }
        } label: {
            Label("Options", systemImage: "ellipsis.circle")
        }
    }

    @ViewBuilder
    private func destinationView(_ destination: PasswordsDestination) -> some View {
        switch destination {
        case .setUp: PasswordsSetUpScreen(model: model)
        case .item(let itemId): PasswordsItemScreen(model: model, itemId: itemId, editing: $editing)
        case .share(let itemId): PasswordsShareScreen(model: model, itemId: itemId)
        case .versions(let itemId): PasswordsVersionsScreen(model: model, itemId: itemId)
        case .version(let itemId, let versionId):
            PasswordsVersionScreen(model: model, itemId: itemId, versionId: versionId) {
                path.removeAll { $0.isVersionScreen }
            }
        case .deleted: PasswordsDeletedScreen(model: model)
        case .settings: PasswordsSettingsScreen(model: model)
        }
    }

    /// Checks the lock time every few seconds while Passwords is open.
    private func watchLockTime() async {
        guard model.phase == .unlocked else { return }
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(15))
            if Task.isCancelled { return }
            await model.lockIfAskRequired()
        }
    }

    #if DEBUG
        /// The screens a screenshot names: `-TeaNodeScreen passwords` (the list),
        /// `passwords-login`, `passwords-card`, `passwords-note` (an item),
        /// `passwords-editor`, `passwords-generator`, `passwords-share`,
        /// `passwords-deleted`, `passwords-settings`, `passwords-intro`,
        /// `passwords-setup` (the passphrase), `passwords-recovery-key`,
        /// `passwords-unlock`.
        private func openDemoScreen() {
            guard DemoServer.isRequested, path.isEmpty else { return }
            switch DemoServer.screen {
            case "passwords-setup": path = [.setUp]
            case "passwords-login": path = [.item(PasswordsDemoStore.loginItemId)]
            case "passwords-agent-login": path = [.item(PasswordsDemoStore.agentLoginItemId)]
            case "passwords-card": path = [.item(PasswordsDemoStore.cardItemId)]
            case "passwords-note": path = [.item(PasswordsDemoStore.noteItemId)]
            case "passwords-shared-login": path = [.item(PasswordsDemoStore.sharedWithMeItemId)]
            case "passwords-share": path = [.share(PasswordsDemoStore.sharedByMeItemId)]
            case "passwords-deleted": path = [.deleted]
            case "passwords-settings": path = [.settings]
            case "passwords-editor", "passwords-generator":
                if let item = model.item(id: PasswordsDemoStore.loginItemId) {
                    editing = PasswordsEditing(itemKind: .login, item: item)
                }
            default: break
            }
        }
    #endif
}

// MARK: Intro

/// PasswordsIntroView is the screen before Passwords is set up: what it is,
/// and the way to set it up.
struct PasswordsIntroView: View {
    let setUp: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label("Keep your passwords here", systemImage: PasswordsWords.kindSystemImage(.login))
        } description: {
            Text(
                "Keep logins, passkeys, cards and notes that only your devices can read. They are encrypted on your device before they reach the server. If you lose both your passphrase and your recovery key, they are gone for good."
            )
        } actions: {
            Button("Set up Passwords", action: setUp)
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
        }
        .navigationTitle(PasswordsWords.title)
        .navigationBarTitleDisplayMode(.inline)
    }
}

// MARK: The list

/// PasswordsListView is the open Passwords: the items, searchable, and a menu
/// under the title that chooses which to show.
struct PasswordsListView: View {
    @Bindable var model: PasswordsModel
    @Binding var filter: PasswordsFilter
    @Binding var searchText: String
    let onNew: (PasswordsItemKind) -> Void
    @State private var finishAsk: PasswordsFinishAsk?
    /// The changed key a notice's "Compare codes" asked about.
    @State private var comparing: PasswordsCompareTarget?

    private var shownItems: [PasswordItem] {
        model.items.filter { filter.includes($0) && $0.matches(words: searchText) }
    }

    var body: some View {
        content
            .navigationTitle(filter.filterTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbarTitleMenu { titleMenu }
            .searchable(text: $searchText, prompt: Text("Search items"))
            .onChange(of: searchText) { model.noteUse() }
            .onChange(of: filter) { model.noteUse() }
    }

    @ViewBuilder
    private var content: some View {
        if model.items.isEmpty {
            ContentUnavailableView {
                Label("No passwords yet", systemImage: PasswordsWords.kindSystemImage(.login))
            } description: {
                Text("Add a login, a card or a note. Only your devices can read them.")
            } actions: {
                Button("Add a login") { onNew(.login) }
                    .buttonStyle(.borderedProminent)
            }
        } else if shownItems.isEmpty {
            if searchText.isEmpty {
                ContentUnavailableView(filter.filterTitle, systemImage: "tray", description: Text("Nothing here."))
            } else {
                ContentUnavailableView.search(text: searchText)
            }
        } else {
            list
        }
    }

    private var list: some View {
        List {
            needsYouSection
            keyChangeSection
            circleShareSection
            Section {
                ForEach(shownItems) { item in
                    NavigationLink(value: PasswordsDestination.item(item.id)) {
                        PasswordsItemRow(item: item)
                    }
                    .contextMenu { PasswordsCopyMenu(model: model, item: item) }
                }
            }
            if model.unreadableItemCount > 0 {
                Section {
                    Label("\(model.unreadableItemCount) items could not be opened on this iPhone. They may have been saved by a newer version of TeaNode.", systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.primary)
                }
            }
        }
        .refreshable { await model.sync() }
        .sheet(item: $comparing) { target in
            PasswordsCompareCodesSheet(model: model, target: target)
        }
    }

    /// Notices of keys that changed: each was accepted, nothing waits on it, and it stays until it is dismissed.
    @ViewBuilder
    private var keyChangeSection: some View {
        ForEach(model.keyChanges) { change in
            Section {
                PasswordsKeyChangeRow(model: model, change: change) { comparing = PasswordsCompareTarget(change: change) }
            }
        }
    }

    /// Notices of people this iPhone sealed an item for through a circle for the first time.
    @ViewBuilder
    private var circleShareSection: some View {
        ForEach(model.circleShareNotices) { notice in
            Section {
                PasswordsCircleShareNoticeRow(model: model, notice: notice)
            }
        }
    }

    /// "Finish sharing N items" and one line for each item this iPhone left for the person to finish
    /// sharing (a person who joined a circle later needs one tap for each item, so one row does
    /// them all). It does not block anything: each item opens as usual, and its screen has the button.
    @ViewBuilder
    private var needsYouSection: some View {
        let waitingItems = model.items.filter { model.itemIdsNeedingYou.contains($0.id) }
        if !waitingItems.isEmpty {
            Section {
                finishEveryItemRow(count: waitingItems.count)
                ForEach(waitingItems) { item in
                    NavigationLink(value: PasswordsDestination.item(item.id)) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: item.name)
                            Text("Open this item to finish sharing it.")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            } footer: {
                needsYouFooter
            }
        }
    }

    @ViewBuilder
    private func finishEveryItemRow(count: Int) -> some View {
        if let progress = model.sharingProgress {
            HStack(spacing: 12) {
                ProgressView()
                Text("Finishing sharing: \(progress.doneCount) of \(progress.totalCount)")
                    .contentTransition(.numericText())
            }
            .accessibilityElement(children: .combine)
        } else {
            Button { Task { await askToFinishEveryItem() } } label: {
                Label("Finish sharing \(count) items", systemImage: "person.badge.plus")
                    .actionColor()
            }
            .passwordsFinishSharingDialog($finishAsk) { ask in Task { await model.finishSharingEveryItem(ask) } }
        }
    }

    /// Works out what finishing all of them would do and asks first, since a key is sealed for each
    /// person who is waiting: who gets keys for how many items, and whether the agent gets its copy back.
    private func askToFinishEveryItem() async {
        let waitingItems = model.items.filter { model.itemIdsNeedingYou.contains($0.id) }
        guard !waitingItems.isEmpty else { return }
        let itemIds = waitingItems.map(\.id)
        let plan = await model.finishPlan(itemIds: itemIds)
        finishAsk = PasswordsFinishAsk(
            itemIds: itemIds, plan: plan,
            title: PasswordsWords.finishConfirmationTitle(itemCount: itemIds.count, itemName: waitingItems.first?.name ?? ""))
    }

    @ViewBuilder
    private var needsYouFooter: some View {
        if model.hasMoreSharesWaiting || !model.namesWithTooMuchShared.isEmpty {
            VStack(alignment: .leading, spacing: 4) {
                if model.hasMoreSharesWaiting {
                    Text("More are waiting: finish these first.")
                }
                ForEach(model.namesWithTooMuchShared, id: \.self) { name in
                    Text("\(name) has too much shared with them.")
                }
            }
        }
    }

    @ViewBuilder
    private var titleMenu: some View {
        Picker("Show", selection: $filter) {
            Text("All items").tag(PasswordsFilter.everything)
            ForEach(PasswordsItemKind.allCases, id: \.self) { itemKind in
                Label(PasswordsWords.kindPluralName(itemKind), systemImage: PasswordsWords.kindSystemImage(itemKind))
                    .tag(PasswordsFilter.kind(itemKind))
            }
            Text("Shared").tag(PasswordsFilter.shared)
            Text("Used by the agent").tag(PasswordsFilter.usableByAgent)
            if !model.folders.isEmpty {
                Section("Folders") {
                    ForEach(model.folders, id: \.self) { folder in
                        Text(verbatim: folder).tag(PasswordsFilter.folder(folder))
                    }
                }
            }
        }
    }
}

/// PasswordsItemRow is one item in the list: what it is, its name, and whether
/// it is shared. Whether the agent can use it is shown by the filter and on the
/// item's own screen, not on every row.
struct PasswordsItemRow: View {
    let item: PasswordItem
    @ScaledMetric private var iconWidth: CGFloat = 28

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: PasswordsWords.kindSystemImage(item.itemKind))
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: iconWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                PasswordsValueText(item.name.isEmpty ? String(localized: "Untitled") : item.name, isProse: true)
                if !item.subtitle.isEmpty {
                    PasswordsValueText(item.subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                markers
            }
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var markers: some View {
        if item.isSharedWithMe {
            Label("Shared by \(item.ownerName)", systemImage: "person.2")
                .labelStyle(.tag)
                .font(.caption)
                .foregroundStyle(.secondary)
        } else if !item.recordedShares.isEmpty {
            Label("Shared", systemImage: "person.2")
                .labelStyle(.tag)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

/// PasswordsCopyMenu is the long-press menu of an item in the list: copy what
/// the item holds without opening it.
struct PasswordsCopyMenu: View {
    let model: PasswordsModel
    let item: PasswordItem

    var body: some View {
        switch item.fields {
        case .login(let login):
            if !login.userName.isEmpty {
                Button {
                    model.copy(login.userName, fieldName: String(localized: "User name"))
                } label: {
                    Label("Copy user name", systemImage: "person")
                }
            }
            if !login.password.isEmpty {
                Button {
                    model.copy(login.password, fieldName: String(localized: "Password"))
                } label: {
                    Label("Copy password", systemImage: "key")
                }
            }
        case .card(let card):
            if !card.number.isEmpty {
                Button {
                    model.copy(card.number.filter(\.isNumber), fieldName: String(localized: "Card number"))
                } label: {
                    Label("Copy card number", systemImage: "creditcard")
                }
            }
        case .note:
            EmptyView()
        }
    }
}

// MARK: Recently deleted

/// PasswordsDeletedScreen lists the items deleted in the last 30 days, which
/// can be restored.
struct PasswordsDeletedScreen: View {
    @Bindable var model: PasswordsModel
    @State private var itemToDeleteForever: PasswordItem?

    var body: some View {
        Group {
            if model.deletedItems.isEmpty {
                ContentUnavailableView {
                    Label("Nothing was deleted lately", systemImage: "trash")
                } description: {
                    Text("Items you delete stay here for 30 days. Then they are gone for good.")
                }
            } else {
                List {
                    Section {
                        ForEach(model.deletedItems) { item in
                            NavigationLink(value: PasswordsDestination.item(item.id)) {
                                PasswordsDeletedRow(item: item)
                            }
                            .confirmationDialog(
                                "Delete forever?",
                                isPresented: Binding(
                                    get: { itemToDeleteForever?.id == item.id }, set: { if !$0 { itemToDeleteForever = nil } }),
                                titleVisibility: .visible, presenting: itemToDeleteForever
                            ) { item in
                                Button("Delete forever", role: .destructive) { Task { await deleteForever(item) } }
                            } message: { item in
                                Text("\(item.name) is removed from Passwords and from this iPhone, and cannot be brought back.")
                            }
                            .swipeActions(edge: .trailing) {
                                Button("Restore", systemImage: "arrow.uturn.backward") { Task { await restore(item) } }
                                Button("Delete forever", systemImage: "trash", role: .destructive) { itemToDeleteForever = item }
                                    .tint(.red)
                            }
                            .contextMenu {
                                Button("Restore", systemImage: "arrow.uturn.backward") { Task { await restore(item) } }
                                Button("Delete forever", systemImage: "trash", role: .destructive) { itemToDeleteForever = item }
                                    .tint(.red)
                            }
                        }
                    } footer: {
                        Text("Items you delete stay here for 30 days. Then they are gone for good.")
                    }
                }
            }
        }
        .navigationTitle("Recently deleted")
        .navigationBarTitleDisplayMode(.inline)
    }

    private func deleteForever(_ item: PasswordItem) async {
        do {
            try await model.purge(item)
            model.doneText = String(localized: "\(item.name) was deleted forever.")
        } catch {
            model.report(error, orSay: String(localized: "The item could not be deleted forever."))
        }
    }

    private func restore(_ item: PasswordItem) async {
        do {
            try await model.restore(item)
            model.doneText = String(localized: "\(item.name) was restored.")
        } catch {
            model.report(error, orSay: String(localized: "The item could not be restored."))
        }
    }
}

/// PasswordsDeletedRow is a deleted item: its name and when it was deleted.
struct PasswordsDeletedRow: View {
    let item: PasswordItem
    @ScaledMetric private var iconWidth: CGFloat = 28

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: PasswordsWords.kindSystemImage(item.itemKind))
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: iconWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                PasswordsValueText(item.name, isProse: true)
                if let deletedAt = item.deletedAt {
                    Text("Deleted \(deletedAt.formatted(.relative(presentation: .named)))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }
}
