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
    case deleted
    case settings
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
            // People who wait for the person's OK are listed at the top of the list: go back to it.
            .onChange(of: model.confirmationRequestCount) {
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
            keyConfirmationSections
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
    }

    /// Keys the person has to decide on: an owner whose key changed (their shared items stay closed), and people waiting for an item of the person's whose key is new or changed.
    @ViewBuilder
    private var keyConfirmationSections: some View {
        if !model.ownerKeyChanges.isEmpty {
            Section {
                ForEach(model.ownerKeyChanges) { change in
                    PasswordsOwnerKeyChangeRow(change: change) { Task { await model.trustOwnerKey(of: change) } }
                }
            }
        }
        if !model.waitingConfirmations.isEmpty {
            Section {
                ForEach(model.waitingConfirmations, id: \.self) { waiting in
                    PasswordsWaitingConfirmationRow(waiting: waiting) { Task { await model.confirmWaitingShare(waiting) } }
                }
            } header: {
                Text("Waiting for your OK")
            } footer: {
                Text("These people cannot open an item of yours yet. Ask each person to open Passwords settings on their own device and read you their safety code. Confirm only if it matches.")
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
                            .swipeActions(edge: .trailing) {
                                Button(role: .destructive) {
                                    itemToDeleteForever = item
                                } label: {
                                    Label("Delete forever", systemImage: "trash")
                                }
                                Button {
                                    Task { await restore(item) }
                                } label: {
                                    Label("Restore", systemImage: "arrow.uturn.backward")
                                }
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
        .confirmationDialog(
            "Delete forever?", isPresented: Binding(get: { itemToDeleteForever != nil }, set: { if !$0 { itemToDeleteForever = nil } }),
            titleVisibility: .visible, presenting: itemToDeleteForever
        ) { item in
            Button("Delete forever", role: .destructive) { Task { await deleteForever(item) } }
        } message: { item in
            Text("\(item.name) is removed from Passwords and from this iPhone, and cannot be brought back.")
        }
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
