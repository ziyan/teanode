import SwiftUI
import TeaNodeKit

/// MailScreen is the dashboard's mailbox on a phone: the folder being read,
/// the folders in the side menu, a conversation a tap away.
struct MailScreen: View {
    let mail: MailModel
    @Binding var place: MailPlace?
    @Binding var composing: ComposeStart?
    @Environment(AppModel.self) private var app
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            Group {
                switch place {
                case .subscriptions:
                    SubscriptionsList(mail: mail)
                case .some(let place):
                    ThreadList(mail: mail, place: place, composing: $composing)
                        .id(place)
                case nil:
                    if mail.isLoaded {
                        ContentUnavailableView("No mailbox yet", systemImage: "tray", description: Text("A mailbox is made when an address is given to you on your server."))
                    } else {
                        ProgressView()
                    }
                }
            }
            .toolbar { MenuButton() }
            .navigationDestination(for: OpenedThread.self) { opened in
                MailThreadScreen(mail: mail, opened: opened, composing: $composing)
            }
        }
        .onChange(of: place) { path = NavigationPath() }
        .onChange(of: app.openedMailItemId, initial: true) { _, itemId in
            guard let itemId else { return }
            app.openedMailItemId = nil
            path = NavigationPath()
            path.append(OpenedThread(itemId: itemId, folderId: nil))
        }
    }
}

/// OpenedThread is a conversation opened from a list, with the folder it
/// was opened in, which says what the toolbar acts on.
struct OpenedThread: Hashable {
    let itemId: String
    let folderId: String?
}

/// MailboxesList is the rail: the mailbox picker, the Inbox, Starred,
/// Priority, the pinned folders, the rest of the tree and the lists.
struct MailboxesList: View {
    let mail: MailModel
    let current: MailPlace?
    @Binding var composing: ComposeStart?
    /// Rows shown above the folders, as the side menu puts its sections
    /// there.
    var leading: AnyView?
    let onChoose: (MailPlace) -> Void

    var body: some View {
        List {
            if let leading {
                Section { leading }
                    .listSectionSeparator(.hidden)
            }
            if let entry = mail.current, let inbox = mail.folder(kind: "inbox") {
                Section {
                    FolderLink(place: .folder(inbox.id), name: String(localized: "Inbox"), systemImage: "tray", count: inbox.unread, current: current, onChoose: onChoose)
                    ForEach(childRows(of: inbox.id), id: \.folder.id) { row in
                        FolderLink(place: .folder(row.folder.id), name: row.folder.name, systemImage: "folder", count: row.folder.unread, current: current, onChoose: onChoose)
                            .padding(.leading, CGFloat(min(row.depth, 3)) * 16)
                    }
                    FolderLink(place: .starred, name: String(localized: "Starred"), systemImage: "star", count: entry.starredUnread, current: current, onChoose: onChoose)
                    if mail.mailbox?.agent?.isSorting == true {
                        FolderLink(place: .priority, name: String(localized: "Priority"), systemImage: "exclamationmark.circle", count: entry.priorityUnread, current: current, onChoose: onChoose)
                    }
                } header: {
                    // In the side menu, the mail under the parts of TeaNode
                    // is titled as Finance's sections are.
                    if leading != nil { Text("Mail") }
                }
                .listSectionSeparator(.hidden)
                let rows = mail.railRows
                if !rows.pinned.isEmpty {
                    Section("Pinned") {
                        ForEach(rows.pinned, id: \.folder.id) { row in
                            folderLink(row)
                        }
                    }
                    .listSectionSeparator(.hidden)
                }
                Section("Folders") {
                    ForEach(rows.rest.filter { $0.folder.kind != "inbox" && !isUnderInbox($0.folder) }, id: \.folder.id) { row in
                        folderLink(row)
                    }
                }
                .listSectionSeparator(.hidden)
                Section {
                    FolderLink(place: .subscriptions, name: String(localized: "Subscriptions"), systemImage: "newspaper", count: 0, current: current, onChoose: onChoose)
                }
                .listSectionSeparator(.hidden)
            }
        }
        .refreshable { await mail.load() }
        .overlay {
            if mail.isLoaded, mail.entries.isEmpty {
                ContentUnavailableView("No mailbox yet", systemImage: "tray", description: Text("A mailbox is made when an address is given to you on your server."))
            } else if !mail.isLoaded {
                ProgressView()
            }
        }
        .toolbar {
            MenuBrand()
            if mail.entries.count > 1 {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        Picker("Mailbox", selection: Binding(get: { mail.current?.mailbox?.id }, set: { mail.currentMailboxId = $0 })) {
                            ForEach(mail.entries, id: \.mailbox?.id) { entry in
                                Text(verbatim: "\(entry.mailbox?.name ?? "") (\(entry.unread))").tag(entry.mailbox?.id)
                            }
                        }
                    } label: {
                        Label("Mailboxes", systemImage: "tray.2")
                    }
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    composing = ComposeStart(kind: .new)
                } label: {
                    Label("New message", systemImage: "square.and.pencil")
                }
                .disabled(mail.mailbox == nil)
            }
        }
    }

    private func folderLink(_ row: (folder: MailFolder, depth: Int)) -> some View {
        FolderLink(
            place: .folder(row.folder.id), name: MailNames.folder(row.folder), systemImage: MailNames.symbol(row.folder),
            count: row.folder.kind == "archive" ? 0 : row.folder.unread, current: current, onChoose: onChoose
        )
        .padding(.leading, CGFloat(min(row.depth, 3)) * 16)
    }

    private func childRows(of folderId: String) -> [(folder: MailFolder, depth: Int)] {
        let rows = MailFolders.rows(mail.folders)
        guard let start = rows.firstIndex(where: { $0.folder.id == folderId }) else { return [] }
        var children: [(folder: MailFolder, depth: Int)] = []
        var index = start + 1
        while index < rows.count, rows[index].depth > rows[start].depth {
            if rows[index].folder.pinnedAt == nil { children.append(rows[index]) }
            index += 1
        }
        return children
    }

    private func isUnderInbox(_ folder: MailFolder) -> Bool {
        var parentId = folder.parentId
        while !parentId.isEmpty, let parent = mail.folder(parentId) {
            if parent.kind == "inbox" { return true }
            parentId = parent.parentId
        }
        return false
    }
}

/// FolderLink is a row of the rail: its name, its symbol and its unread
/// count.
struct FolderLink: View {
    let place: MailPlace
    let name: String
    let systemImage: String
    let count: Int64
    let current: MailPlace?
    let onChoose: (MailPlace) -> Void

    var body: some View {
        Button {
            onChoose(place)
        } label: {
            LabeledContent {
                if count > 0 {
                    Text(count, format: .number)
                        .monospacedDigit()
                        .contentTransition(.numericText())
                }
            } label: {
                Label(name, systemImage: systemImage)
            }
            .contentShape(.rect)
        }
        .foregroundStyle(.primary)
        .menuRow(isSelected: place == current)
        .accessibilityAddTraits(place == current ? .isSelected : [])
    }
}

enum MailNames {
    /// A system folder's name in the person's language; their own as named.
    static func folder(_ folder: MailFolder) -> String {
        switch folder.kind {
        case "inbox": String(localized: "Inbox")
        case "drafts": String(localized: "Drafts")
        case "sent": String(localized: "Sent")
        case "archive": String(localized: "Archive")
        case "junk": String(localized: "Junk")
        case "trash": String(localized: "Trash")
        default: folder.name
        }
    }

    static func symbol(_ folder: MailFolder) -> String {
        switch folder.kind {
        case "inbox": "tray"
        case "drafts": "doc"
        case "sent": "paperplane"
        case "archive": "archivebox"
        case "junk": "xmark.bin"
        case "trash": "trash"
        default: folder.isNotes ? "note.text" : "folder"
        }
    }
}

/// ThreadListModel is one list's conversations, a page at a time, narrowed
/// as the person chose.
@MainActor
@Observable
final class ThreadListModel {
    let place: MailPlace
    private(set) var threads: [MailThread] = []
    private(set) var total: Int64 = 0
    private(set) var isLoading = false
    private(set) var isLoaded = false
    var filter = MailFilter()
    private var generation = 0

    static let pageSize = 50

    init(place: MailPlace) {
        self.place = place
    }

    /// Whether the list spans every folder: Starred, Priority, or a search
    /// everywhere.
    var isAcrossFolders: Bool {
        switch place {
        case .starred, .priority: true
        default: filter.isEverywhere && filter.isNarrowing
        }
    }

    func load(_ mail: MailModel, isMore: Bool = false) async {
        generation += 1
        let mine = generation
        isLoading = true
        defer { if mine == generation { isLoading = false } }
        var asked = filter
        var folderId: String?
        var mailboxId: String?
        switch place {
        case .folder(let id):
            if isAcrossFolders { mailboxId = mail.mailbox?.id } else { folderId = id }
        case .starred:
            asked.isStarredOnly = true
            mailboxId = mail.mailbox?.id
        case .priority:
            mailboxId = mail.mailbox?.id
        case .subscriptions:
            return
        }
        do {
            let page = try await mail.connection.mailThreads(
                folderId: folderId, mailboxId: mailboxId, filter: asked, isPriority: place == .priority, first: Self.pageSize,
                offset: isMore ? threads.count : 0)
            guard mine == generation else { return }
            let fresh = page.threads.compactMap { $0 }
            threads = isMore ? threads + fresh.filter { row in !threads.contains { $0.id == row.id } } : fresh
            total = page.total
            isLoaded = true
        } catch {
            guard mine == generation else { return }
            mail.problemText = error.localizedDescription
        }
    }

    /// What acting on a row means: in a folder, every message of the
    /// conversation there; across folders, the one message shown.
    func target(of thread: MailThread) -> MailTarget? {
        guard let item = thread.item else { return nil }
        if isAcrossFolders { return MailTarget(items: [item]) }
        return MailTarget(itemIds: thread.itemIds.isEmpty ? [item.id] : thread.itemIds, home: item.folderId)
    }

    func target(of threads: [MailThread]) -> MailTarget? {
        let targets = threads.compactMap { target(of: $0) }
        guard var joined = targets.first else { return nil }
        for other in targets.dropFirst() {
            joined.itemIds += other.itemIds
            joined.homes.merge(other.homes) { first, _ in first }
        }
        return joined
    }

    func remove(_ removed: Set<String>) {
        let before = threads.count
        threads.removeAll { removed.contains($0.id) }
        total = max(0, total - Int64(before - threads.count))
    }

    func patch(_ ids: Set<String>, isSeen: Bool? = nil, isFlagged: Bool? = nil) {
        for index in threads.indices where ids.contains(threads[index].id) {
            threads[index] = threads[index].with(isSeen: isSeen, isFlagged: isFlagged)
        }
    }
}

/// ThreadList is a folder, Starred or Priority: the conversations newest
/// first, searched and narrowed, swiped, chosen several at a time.
struct ThreadList: View {
    let mail: MailModel
    let place: MailPlace
    @Binding var composing: ComposeStart?
    @State private var list: ThreadListModel
    @State private var selection = Set<String>()
    @State private var editMode = EditMode.inactive
    @State private var isNarrowing = false
    @State private var isSorting = false
    @State private var isConfirmingEmpty = false

    init(mail: MailModel, place: MailPlace, composing: Binding<ComposeStart?>) {
        self.mail = mail
        self.place = place
        _composing = composing
        _list = State(initialValue: ThreadListModel(place: place))
    }

    private var folder: MailFolder? {
        if case .folder(let id) = place { return mail.folder(id) }
        return nil
    }

    var body: some View {
        @Bindable var list = list
        List(selection: $selection) {
            // Sections by period, as a mail app has them: Today,
            // Yesterday, This Week, Last Week, then each month.
            ForEach(ListPeriod.periods(of: list.threads, at: { MailTime.date($0.item?.mail?.receivedAt ?? $0.item?.addedAt ?? "") })) { period in
                Section {
                    ForEach(period.items) { thread in
                        NavigationLink(value: OpenedThread(itemId: thread.item?.id ?? thread.threadId, folderId: list.isAcrossFolders ? nil : folder?.id)) {
                            ThreadRow(thread: thread, folderName: folderName(of: thread))
                        }
                        // As Outlook and Mail draw a conversation: no chevron, no
                        // divider, rows close together.
                        .navigationLinkIndicatorVisibility(.hidden)
                        .listRowInsets(ListRowSpacing.insets)
                        .listRowSeparator(.hidden)
                        .swipeActions(edge: .leading) {
                            Button {
                                Task { await setSeen([thread], isSeen: thread.unread > 0) }
                            } label: {
                                thread.unread > 0 ? Label("Mark read", systemImage: "envelope.open") : Label("Mark unread", systemImage: "envelope.badge")
                            }
                            .tint(.blue)
                        }
                        // The first is what a full swipe does: archive, as the
                        // dashboard's swipe does, and delete where there is no
                        // archive or the row is in it.
                        .swipeActions(edge: .trailing) {
                            if let archive = mail.folder(kind: "archive"), folder?.id != archive.id {
                                Button {
                                    Task { await move([thread], to: archive, saying: String(localized: "Conversation archived")) }
                                } label: {
                                    Label("Archive", systemImage: "archivebox")
                                }
                                .tint(.purple)
                            }
                            Button(role: .destructive) {
                                Task { await delete([thread]) }
                            } label: {
                                Label("Delete", systemImage: "trash")
                            }
                        }
                        .contextMenu {
                            rowMenu([thread])
                        }
                        .onAppear {
                            if thread.id == list.threads.last?.id, list.threads.count < list.total, !list.isLoading {
                                Task { await list.load(mail, isMore: true) }
                            }
                        }
                    }
                } header: {
                    ListPeriodHeader(title: period.title)
                }
                .listSectionSeparator(.hidden)
            }
            if list.isLoading, !list.threads.isEmpty {
                ProgressView().frame(maxWidth: .infinity).listRowSeparator(.hidden)
            }
        }
        .listStyle(.plain)
        .listSectionSpacing(.compact)
        .environment(\.editMode, $editMode)
        .navigationTitle(title)
        .searchable(text: $list.filter.searchText, prompt: "Search")
        .onSubmit(of: .search) { reload() }
        .onChange(of: list.filter.searchText) { _, text in
            if text.isEmpty { reload() }
        }
        .task {
            await list.load(mail)
        }
        .refreshable { await list.load(mail) }
        .overlay { emptyState }
        .toolbar { toolbar }
        .sheet(isPresented: $isNarrowing) {
            NarrowSheet(filter: $list.filter) { reload() }
        }
        .sheet(isPresented: $isSorting) {
            SortSheet(mail: mail, count: selection.count) { category, priority, needsReply in
                let target = list.target(of: chosen)
                Task {
                    guard let target else { return }
                    _ = await mail.act(saying: String(localized: "Sorted")) {
                        try await mail.connection.sortMail(target.itemIds, category: category, priority: priority, needsReply: needsReply)
                    }
                    finishEditing()
                    await list.load(mail)
                }
            }
        }
    }

    private var title: String {
        switch place {
        case .folder: folder.map(MailNames.folder) ?? String(localized: "Mail")
        case .starred: String(localized: "Starred")
        case .priority: String(localized: "Priority")
        case .subscriptions: String(localized: "Subscriptions")
        }
    }

    private var chosen: [MailThread] { list.threads.filter { selection.contains($0.id) } }

    @ViewBuilder
    private var emptyState: some View {
        if list.isLoaded, list.threads.isEmpty {
            if list.filter.isNarrowing {
                ContentUnavailableView("Nothing matches", systemImage: "magnifyingglass")
            } else if place == .starred {
                ContentUnavailableView("Nothing is starred", systemImage: "star", description: Text("Star a message and it appears here, whichever folder it is in."))
            } else if place == .priority {
                ContentUnavailableView("Nothing important waiting", systemImage: "exclamationmark.circle", description: Text("What your agent marks as high priority appears here."))
            } else if folder?.kind == "inbox", mail.mailbox?.sendingAddresses.isEmpty == true {
                ContentUnavailableView("This mailbox has no address yet", systemImage: "at", description: Text("Mail arrives once an address is given to it on the dashboard."))
            } else {
                ContentUnavailableView("Nothing here", systemImage: "tray")
            }
        } else if !list.isLoaded {
            ProgressView()
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Toggle("Unread", systemImage: "envelope.badge", isOn: Binding(get: { list.filter.isUnreadOnly }, set: { list.filter.isUnreadOnly = $0; reload() }))
                if place != .starred {
                    Toggle("Starred", systemImage: "star", isOn: Binding(get: { list.filter.isStarredOnly }, set: { list.filter.isStarredOnly = $0; reload() }))
                }
                Button("Narrow…", systemImage: "line.3.horizontal.decrease") { isNarrowing = true }
                if list.filter.isNarrowing {
                    Button("Show everything", systemImage: "xmark.circle") {
                        list.filter = MailFilter()
                        reload()
                    }
                }
            } label: {
                Label("Filter", systemImage: list.filter.isNarrowing ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            Button(editMode.isEditing ? LocalizedStringKey("Done") : "Select") {
                withAnimation {
                    if editMode.isEditing { finishEditing() } else { editMode = .active }
                }
            }
            .disabled(list.threads.isEmpty)
        }
        if editMode.isEditing {
            ToolbarItemGroup(placement: .bottomBar) {
                Button(selection.count == list.threads.count ? LocalizedStringKey("Select none") : "Select all") {
                    selection = selection.count == list.threads.count ? [] : Set(list.threads.map(\.id))
                }
                Spacer()
                Menu {
                    rowMenu(chosen)
                } label: {
                    Label("Actions", systemImage: "ellipsis.circle")
                }
                .disabled(selection.isEmpty)
                Button(role: .destructive) {
                    Task { await delete(chosen) }
                } label: {
                    Label(folder?.kind == "trash" ? LocalizedStringKey("Delete forever") : "Delete", systemImage: "trash")
                }
                .disabled(selection.isEmpty)
            }
        } else {
            ToolbarItemGroup(placement: .bottomBar) {
                if folder?.kind == "trash", list.total > 0 {
                    Button("Empty Trash", role: .destructive) { isConfirmingEmpty = true }
                    .confirmationDialog("Empty Trash?", isPresented: $isConfirmingEmpty, titleVisibility: .visible) {
                        Button("Empty Trash", role: .destructive) {
                            Task {
                                guard let mailboxId = mail.mailbox?.id else { return }
                                _ = await mail.act(saying: String(localized: "Trash emptied")) {
                                    try await mail.connection.emptyTrash(mailboxId: mailboxId)
                                }
                                await list.load(mail)
                            }
                        }
                    } message: {
                        Text("Every message in Trash is removed. They cannot be brought back.")
                    }
                }
                Spacer()
                Button {
                    composing = ComposeStart(kind: .new)
                } label: {
                    Label("New message", systemImage: "square.and.pencil")
                }
            }
        }
    }

    /// The actions on rows, the same in the long-press menu and when
    /// several are chosen.
    @ViewBuilder
    private func rowMenu(_ threads: [MailThread]) -> some View {
        let isAnyUnread = threads.contains { $0.unread > 0 }
        let isAnyUnstarred = threads.contains { !$0.flagged }
        Button(isAnyUnread ? LocalizedStringKey("Mark read") : "Mark unread", systemImage: isAnyUnread ? "envelope.open" : "envelope.badge") {
            Task { await setSeen(threads, isSeen: isAnyUnread) }
        }
        Button(isAnyUnstarred ? LocalizedStringKey("Star") : "Unstar", systemImage: isAnyUnstarred ? "star" : "star.slash") {
            Task { await setFlagged(threads, isFlagged: isAnyUnstarred) }
        }
        if mail.mailbox?.agent?.isSorting == true {
            Button("Sort as…", systemImage: "tag") {
                selection = Set(threads.map(\.id))
                isSorting = true
            }
        }
        if let archive = mail.folder(kind: "archive"), folder?.id != archive.id {
            Button("Archive", systemImage: "archivebox") {
                Task { await move(threads, to: archive, saying: String(localized: "Conversation archived")) }
            }
        }
        if folder?.kind == "junk" {
            Button("Not junk", systemImage: "hand.thumbsup") {
                Task { await junk(threads, isNotJunk: true) }
            }
        } else {
            Button("Report junk", systemImage: "xmark.bin") {
                Task { await junk(threads, isNotJunk: false) }
            }
        }
        Menu("Move to…", systemImage: "folder") {
            ForEach(MailFolders.rows(mail.folders).filter { $0.folder.id != folder?.id }, id: \.folder.id) { row in
                Button(String(repeating: "  ", count: row.depth) + MailNames.folder(row.folder)) {
                    Task {
                        await move(threads, to: row.folder, saying: String(localized: "Moved to \(MailNames.folder(row.folder))"))
                    }
                }
            }
        }
        Button(folder?.kind == "trash" ? LocalizedStringKey("Delete forever") : "Delete", systemImage: "trash", role: .destructive) {
            Task { await delete(threads) }
        }
    }

    private func folderName(of thread: MailThread) -> String? {
        guard list.isAcrossFolders, let folderId = thread.item?.folderId, folderId != folder?.id, let shown = mail.folder(folderId) else {
            return nil
        }
        return MailNames.folder(shown)
    }

    private func reload() {
        Task { await list.load(mail) }
    }

    private func finishEditing() {
        editMode = .inactive
        selection = []
    }

    private func setSeen(_ threads: [MailThread], isSeen: Bool) async {
        guard let target = list.target(of: threads) else { return }
        list.patch(Set(threads.map(\.id)), isSeen: isSeen)
        _ = await mail.act(saying: isSeen ? String(localized: "Marked read") : String(localized: "Marked unread")) {
            try await mail.connection.setMailFlags(target.itemIds, isSeen: isSeen)
        }
        finishEditing()
    }

    private func setFlagged(_ threads: [MailThread], isFlagged: Bool) async {
        guard let target = list.target(of: threads) else { return }
        if place == .starred, !isFlagged {
            list.remove(Set(threads.map(\.id)))
        } else {
            list.patch(Set(threads.map(\.id)), isFlagged: isFlagged)
        }
        _ = await mail.act(saying: isFlagged ? String(localized: "Starred") : String(localized: "Unstarred")) {
            try await mail.connection.setMailFlags(target.itemIds, isFlagged: isFlagged)
        }
        finishEditing()
    }

    private func move(_ threads: [MailThread], to destination: MailFolder, saying said: String) async {
        guard let target = list.target(of: threads) else { return }
        list.remove(Set(threads.map(\.id)))
        finishEditing()
        if await mail.move(target, to: destination.id, saying: said) {
            await refill()
        } else {
            await list.load(mail)
        }
    }

    private func delete(_ threads: [MailThread]) async {
        guard let target = list.target(of: threads) else { return }
        list.remove(Set(threads.map(\.id)))
        finishEditing()
        let isForever = folder?.kind == "trash"
        let said = isForever ? String(localized: "Deleted for good") : String(localized: "Moved to Trash")
        if await mail.delete(target, isForever: isForever, saying: said) {
            await refill()
        } else {
            await list.load(mail)
        }
    }

    private func junk(_ threads: [MailThread], isNotJunk: Bool) async {
        guard let target = list.target(of: threads) else { return }
        list.remove(Set(threads.map(\.id)))
        finishEditing()
        let said = isNotJunk ? String(localized: "Said this is not junk") : String(localized: "Reported as junk")
        if await mail.reportJunk(target, isNotJunk: isNotJunk, saying: said) {
            await refill()
        } else {
            await list.load(mail)
        }
    }

    /// Reads the list again once rows were taken off it, so the next ones
    /// move up and an undo puts them back.
    private func refill() async {
        await list.load(mail)
    }
}

/// ThreadRow is a conversation in a list: who, the agent's marks, the
/// subject, the agent's summary, the shield and when. Every row is the same
/// height, as Mail draws them: the summary always takes two lines, empty or
/// not, and the marks are symbols on the first line rather than a line of
/// their own.
struct ThreadRow: View {
    let thread: MailThread
    let folderName: String?

    var body: some View {
        let item = thread.item
        let message = item?.mail
        let isUnread = thread.unread > 0
        let insight = item?.insight
        HStack(alignment: .top, spacing: 12) {
            // The unread mark sits in the margin, as Mail draws it.
            Circle()
                .fill(isUnread ? AnyShapeStyle(.tint) : AnyShapeStyle(.clear))
                .frame(width: 8, height: 8)
                .padding(.top, 16)
                .accessibilityLabel(isUnread ? Text("Unread") : Text(verbatim: ""))
            SenderLogo(domain: message?.logoDomain ?? "", name: message?.shownSender ?? "", size: 40)
            VStack(alignment: .leading, spacing: 2) {
                // Who, in bold, with when on the right: the time today and
                // yesterday, else the weekday or the date.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    // The name keeps its room ahead of the marks, so a row
                    // with several of them still says who it is from.
                    Text(senders)
                        .font(.headline)
                        .lineLimit(1)
                        .layoutPriority(1)
                    Spacer(minLength: 4)
                    // Only what asks for attention: high priority, a reply
                    // owed, and a sender whose checks failed. One stop for
                    // VoiceOver, which reads their words together.
                    let marks = MailRowMarks(insight: insight, verdict: MailVerdict.of(message?.authenticationResults))
                    HStack(spacing: 6) {
                        if marks.isHigh {
                            Image(systemName: "exclamationmark.circle.fill").foregroundStyle(.red)
                                .accessibilityLabel("Important")
                        }
                        if marks.needsReply {
                            Image(systemName: "arrowshape.turn.up.left.fill").foregroundStyle(.tint)
                                .accessibilityLabel("Needs reply")
                        }
                        if marks.verdict == .bad {
                            Image(systemName: "xmark.shield.fill").foregroundStyle(.red)
                                .accessibilityLabel("Not verified")
                        } else if marks.verdict == .warn {
                            Image(systemName: "exclamationmark.shield.fill").foregroundStyle(.orange)
                                .accessibilityLabel("Looks like spam")
                        }
                        if thread.flagged {
                            Image(systemName: "star.fill").foregroundStyle(.orange)
                                .accessibilityLabel("Starred")
                        }
                    }
                    .font(.caption)
                    .accessibilityElement(children: .combine)
                    Text(ListTime.label(of: MailTime.date(message?.receivedAt ?? item?.addedAt ?? "")))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .fixedSize()
                }
                // The subject, with whether it carries attachments and how
                // many messages the conversation holds.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    if thread.hasDraft {
                        Text("Draft").foregroundStyle(.red)
                    }
                    if let folderName {
                        Text(folderName).foregroundStyle(.secondary)
                    }
                    Text(message?.subject.isEmpty == false ? message?.subject ?? "" : String(localized: "(no subject)"))
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if (message?.attachmentCount ?? 0) > 0 {
                        Image(systemName: "paperclip")
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("Has attachments")
                    }
                    if thread.count > 1 {
                        Text(verbatim: "\(thread.count)")
                            .font(.footnote.monospacedDigit())
                            .foregroundStyle(isUnread ? AnyShapeStyle(.white) : AnyShapeStyle(.secondary))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 1)
                            .background(
                                isUnread ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.tertiarySystemFill)), in: .rect(cornerRadius: 5)
                            )
                            .accessibilityLabel(Text("\(thread.count) messages"))
                    }
                }
                .font(.subheadline)
                Text(insight?.summary ?? "")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2, reservesSpace: true)
            }
        }
        .padding(.leading, -12)
    }

    private var senders: String {
        if thread.participants.count > 1 { return thread.participants.joined(separator: ", ") }
        let sender = thread.item?.mail?.shownSender ?? ""
        return sender.isEmpty ? String(localized: "Unknown sender") : sender
    }
}

/// ListPeriod is a stretch of time in a list ordered newest first, as a mail
/// app groups its rows: Today, Yesterday, This Week, Last Week, This Month,
/// then each month. A row says its time within the first two and its weekday or date
/// beyond them.
struct ListPeriod<Item: Identifiable>: Identifiable {
    let id: String
    let title: String
    let items: [Item]

    /// The periods of rows already ordered newest first, keeping that order.
    static func periods(of items: [Item], at moment: (Item) -> Date) -> [ListPeriod<Item>] {
        var periods: [ListPeriod<Item>] = []
        var currentKey: String?
        var currentTitle = ""
        var currentItems: [Item] = []
        for item in items {
            let (key, title) = ListTime.period(of: moment(item))
            if key != currentKey, let currentKey, !currentItems.isEmpty {
                periods.append(ListPeriod(id: currentKey, title: currentTitle, items: currentItems))
                currentItems = []
            }
            currentKey = key
            currentTitle = title
            currentItems.append(item)
        }
        if let currentKey, !currentItems.isEmpty {
            periods.append(ListPeriod(id: currentKey, title: currentTitle, items: currentItems))
        }
        return periods
    }
}

/// ListTime says when a row happened, in the words a mail list uses.
enum ListTime {
    /// The period a moment falls in: a key that tells periods apart and the
    /// heading it is shown under.
    static func period(of moment: Date) -> (String, String) {
        let calendar = Calendar.current
        let now = Date()
        if moment == .distantPast { return ("earlier", String(localized: "Earlier")) }
        if calendar.isDateInToday(moment) { return ("today", String(localized: "Today")) }
        if calendar.isDateInYesterday(moment) { return ("yesterday", String(localized: "Yesterday")) }
        if calendar.isDate(moment, equalTo: now, toGranularity: .weekOfYear) { return ("this-week", String(localized: "This Week")) }
        if let lastWeek = calendar.date(byAdding: .weekOfYear, value: -1, to: now),
            calendar.isDate(moment, equalTo: lastWeek, toGranularity: .weekOfYear)
        {
            return ("last-week", String(localized: "Last Week"))
        }
        let month = calendar.dateComponents([.year, .month], from: moment)
        if calendar.isDate(moment, equalTo: now, toGranularity: .month) {
            return ("this-month", String(localized: "This Month"))
        }
        let title =
            calendar.isDate(moment, equalTo: now, toGranularity: .year)
            ? moment.formatted(.dateTime.month(.wide)) : moment.formatted(.dateTime.month(.wide).year())
        return ("month-\(month.year ?? 0)-\(month.month ?? 0)", title)
    }

    /// What a row shows on its right: the time today and yesterday, the
    /// weekday this week and last, else the short date (10/2/26 in English).
    static func label(of moment: Date) -> String {
        let calendar = Calendar.current
        let now = Date()
        if moment == .distantPast { return "" }
        if calendar.isDateInToday(moment) || calendar.isDateInYesterday(moment) {
            return moment.formatted(date: .omitted, time: .shortened)
        }
        if let lastWeek = calendar.date(byAdding: .weekOfYear, value: -1, to: now),
            calendar.isDate(moment, equalTo: now, toGranularity: .weekOfYear)
                || calendar.isDate(moment, equalTo: lastWeek, toGranularity: .weekOfYear)
        {
            return moment.formatted(.dateTime.weekday(.wide))
        }
        return moment.formatted(.dateTime.year(.twoDigits).month(.defaultDigits).day())
    }
}

/// ListPeriodHeader is a period's heading in a list: plain words, as a mail
/// app draws them.
struct ListPeriodHeader: View {
    let title: String

    var body: some View {
        Text(title)
            .font(.subheadline.weight(.medium))
            .foregroundStyle(.secondary)
            .textCase(nil)
    }
}

/// ListRowSpacing is the room around a row of the mail and recordings lists:
/// close together, as a mail app has them.
enum ListRowSpacing {
    static let insets = EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16)
}

/// MailRowMarks are what a mail row says beyond who and what: only what
/// asks for attention.
struct MailRowMarks {
    let isHigh: Bool
    let needsReply: Bool
    let verdict: MailVerdict

    init(insight: MailInsight?, verdict: MailVerdict) {
        isHigh = insight?.priority == "high"
        needsReply = insight?.needsReply == true
        self.verdict = verdict
    }
}

/// InsightChips are the agent's marks on a message: high priority, its
/// category, and that it needs a reply.
struct InsightChips: View {
    let insight: MailInsight

    var body: some View {
        let isHigh = insight.priority == "high"
        let category = insight.category == "other" ? "" : insight.category
        if isHigh || !category.isEmpty || insight.needsReply {
            HStack(spacing: 4) {
                if isHigh {
                    Chip(text: "!", isAlert: true).accessibilityLabel("High priority")
                }
                if !category.isEmpty {
                    Chip(text: category, isAlert: false)
                }
                if insight.needsReply {
                    Chip(text: String(localized: "Needs reply"), isAlert: false)
                }
            }
        }
    }
}

struct Chip: View {
    let text: String
    let isAlert: Bool

    var body: some View {
        Text(verbatim: text)
            .font(.caption2.weight(.medium))
            .foregroundStyle(isAlert ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
            .padding(.horizontal, 6)
            .padding(.vertical, 1)
            .background(Color(.tertiarySystemFill), in: .capsule)
    }
}

/// SenderLogo is the sender's published logo when the server has one, and
/// otherwise their initial.
struct SenderLogo: View {
    let domain: String
    let name: String
    var size: CGFloat = 36
    @Environment(AppModel.self) private var app
    @State private var logo: UIImage?

    var body: some View {
        Group {
            if let logo {
                Image(uiImage: logo).resizable().scaledToFit()
            } else {
                Text(Monogram.letter(of: name))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.white)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Monogram.color(of: name))
            }
        }
        .frame(width: size, height: size)
        .clipShape(.circle)
        .accessibilityHidden(true)
        .task(id: domain) {
            guard !domain.isEmpty else { return }
            logo = await app.picture("/api/v1/logo/\(domain)")
        }
    }
}

/// VerdictShield is whether a message is from who it says: a green shield
/// when checked, orange when it looks like spam, red when it failed.
struct VerdictShield: View {
    let results: MailMessage.Verdicts?

    var body: some View {
        switch MailVerdict.of(results) {
        case .good:
            Image(systemName: "checkmark.shield").foregroundStyle(.green).font(.caption)
                .accessibilityLabel(Text("Sender checked: \(MailVerdict.detail(results))"))
        case .warn:
            Image(systemName: "exclamationmark.shield").foregroundStyle(.orange).font(.caption)
                .accessibilityLabel(Text("Looks like spam: \(MailVerdict.detail(results))"))
        case .bad:
            Image(systemName: "xmark.shield").foregroundStyle(.red).font(.caption)
                .accessibilityLabel(Text("Sender not verified: \(MailVerdict.detail(results))"))
        case .unknown:
            EmptyView()
        }
    }
}

enum MailTime {
    /// The moment a server time stands for, or the distant past for none.
    static func date(_ text: String) -> Date {
        ServerTime.date(text) ?? .distantPast
    }

    /// A time today, a weekday this week, else a date.
    static func short(_ text: String) -> String {
        guard let date = ServerTime.date(text) else { return "" }
        return short(date)
    }

    /// A time today, a weekday this week, else a date.
    static func short(_ date: Date) -> String {
        let dates = Calendar.current
        if dates.isDateInToday(date) { return date.formatted(date: .omitted, time: .shortened) }
        if dates.isDateInYesterday(date) { return String(localized: "Yesterday") }
        if let days = dates.dateComponents([.day], from: date, to: Date()).day, days < 7 {
            return date.formatted(.dateTime.weekday(.wide))
        }
        return date.formatted(date: .numeric, time: .omitted)
    }
}

/// NarrowSheet is the dashboard's Narrow panel: search the whole mailbox,
/// by who, by subject, between two days, with or without attachments.
struct NarrowSheet: View {
    @Binding var filter: MailFilter
    let onApply: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft = MailFilter()

    var body: some View {
        NavigationStack {
            Form {
                Toggle("Search the whole mailbox", isOn: $draft.isEverywhere)
                Section {
                    TextField("From", text: $draft.from).textInputAutocapitalization(.never)
                    TextField("To", text: $draft.to).textInputAutocapitalization(.never)
                    TextField("Subject", text: $draft.subject)
                }
                Section {
                    OptionalDatePicker(title: String(localized: "Since"), date: $draft.since)
                    OptionalDatePicker(title: String(localized: "Before"), date: $draft.before)
                }
                Section {
                    Picker("Attachments", selection: $draft.hasAttachment) {
                        Text("Any").tag(Bool?.none)
                        Text("With attachments").tag(Bool?.some(true))
                        Text("Without").tag(Bool?.some(false))
                    }
                }
                Section {
                    Button("Clear", role: .destructive) {
                        draft = MailFilter()
                        draft.searchText = filter.searchText
                    }
                }
            }
            .navigationTitle("Narrow")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Apply") {
                        filter = draft
                        onApply()
                        dismiss()
                    }
                }
            }
            .onAppear { draft = filter }
        }
        .presentationDetents([.medium, .large])
    }
}

/// OptionalDatePicker is a day that may be left unset.
struct OptionalDatePicker: View {
    let title: String
    @Binding var date: Date?

    var body: some View {
        Toggle(title, isOn: Binding(get: { date != nil }, set: { date = $0 ? (date ?? Date()) : nil }))
        if let chosen = date {
            DatePicker(title, selection: Binding(get: { chosen }, set: { date = $0 }), displayedComponents: .date)
                .labelsHidden()
        }
    }
}

/// SortSheet tells the agent how to sort the chosen messages: category,
/// priority and whether they need a reply, each left unchanged by default.
struct SortSheet: View {
    let mail: MailModel
    let count: Int
    let onSort: (String?, String?, Bool?) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var category: String?
    @State private var priority: String?
    @State private var needsReply: Bool?

    var body: some View {
        NavigationStack {
            Form {
                Picker("Category", selection: $category) {
                    Text("Unchanged").tag(String?.none)
                    ForEach(mail.sortCategories, id: \.self) { name in
                        Text(verbatim: name).tag(String?.some(name))
                    }
                }
                Picker("Priority", selection: $priority) {
                    Text("Unchanged").tag(String?.none)
                    Text("High").tag(String?.some("high"))
                    Text("Normal").tag(String?.some("normal"))
                    Text("Low").tag(String?.some("low"))
                }
                Picker("Needs reply", selection: $needsReply) {
                    Text("Unchanged").tag(Bool?.none)
                    Text("Yes").tag(Bool?.some(true))
                    Text("No").tag(Bool?.some(false))
                }
            }
            .navigationTitle(Text("Sort \(count) messages"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Sort") {
                        onSort(category, priority, needsReply)
                        dismiss()
                    }
                    .disabled(category == nil && priority == nil && needsReply == nil)
                }
            }
        }
        .presentationDetents([.medium])
    }
}
