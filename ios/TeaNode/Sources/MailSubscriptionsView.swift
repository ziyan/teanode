import SwiftUI
import TeaNodeKit

/// SubscriptionsList is the mailing lists that write to the mailbox: the
/// ones still subscribed or the ones left, searched, newest first.
struct SubscriptionsList: View {
    let mail: MailModel
    @State private var subscriptions: [MailSubscription] = []
    @State private var page: MailSubscriptionPage?
    @State private var isLeft = false
    @State private var searchText = ""
    @State private var isLoaded = false

    static let pageSize = 50

    var body: some View {
        List {
            if let page, page.left > 0 || !searchText.isEmpty {
                Picker("Show", selection: $isLeft) {
                    Text("Subscribed · \(page.subscribed)").tag(false)
                    Text("Unsubscribed · \(page.left)").tag(true)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
            }
            ForEach(subscriptions) { subscription in
                NavigationLink {
                    SubscriptionReader(mail: mail, subscription: subscription) {
                        Task { await load() }
                    }
                } label: {
                    SubscriptionRow(subscription: subscription)
                }
                .onAppear {
                    if subscription.id == subscriptions.last?.id, Int64(subscriptions.count) < (page?.total ?? 0) {
                        Task { await load(isMore: true) }
                    }
                }
            }
        }
        .navigationTitle("Subscriptions")
        .searchable(text: $searchText, prompt: "Search lists")
        .onSubmit(of: .search) { Task { await load() } }
        .onChange(of: searchText) { _, text in
            if text.isEmpty { Task { await load() } }
        }
        .onChange(of: isLeft) { Task { await load() } }
        .task { await load() }
        .refreshable { await load() }
        .overlay {
            if isLoaded, subscriptions.isEmpty {
                if !searchText.isEmpty {
                    ContentUnavailableView.search(text: searchText)
                } else if isLeft {
                    ContentUnavailableView("You have not left any lists", systemImage: "newspaper")
                } else {
                    ContentUnavailableView("No mailing lists have written to this mailbox", systemImage: "newspaper")
                }
            } else if !isLoaded {
                ProgressView()
            }
        }
    }

    private func load(isMore: Bool = false) async {
        guard let mailboxId = mail.mailbox?.id else { return }
        do {
            let read = try await mail.connection.mailSubscriptions(
                mailboxId: mailboxId, isLeft: isLeft, matching: searchText.trimmingCharacters(in: .whitespaces), first: Self.pageSize,
                offset: isMore ? subscriptions.count : 0)
            let fresh = read.subscriptions.compactMap { $0 }
            subscriptions = isMore ? subscriptions + fresh.filter { row in !subscriptions.contains { $0.id == row.id } } : fresh
            page = read
            isLoaded = true
        } catch {
            mail.problemText = error.localizedDescription
        }
    }
}

/// SubscriptionRow is a list: its logo and name, how much it sent, whether
/// it is muted, and whether leaving it worked.
struct SubscriptionRow: View {
    let subscription: MailSubscription

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            SenderLogo(domain: subscription.logoDomain, name: subscription.name.isEmpty ? subscription.from : subscription.name, size: 32)
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(subscription.name.isEmpty ? subscription.from : subscription.name)
                        .font(.headline.weight(subscription.unread > 0 ? .semibold : .regular))
                        .lineLimit(1)
                    Spacer()
                    Text(MailTime.short(subscription.lastAt)).font(.caption).foregroundStyle(.secondary)
                }
                Text(counts).font(.footnote).foregroundStyle(.secondary)
                if let requested = ServerTime.date(subscription.requestedAt) {
                    if subscription.failed {
                        Text("Unsubscribing failed \(requested.formatted(.relative(presentation: .named))): \(subscription.error)")
                            .font(.footnote)
                            .foregroundStyle(.red)
                    } else {
                        Text(leftText(requested)).font(.footnote).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private var counts: String {
        var parts = [String(localized: "\(subscription.count) messages")]
        if subscription.unread > 0 { parts.append(String(localized: "\(subscription.unread) unread")) }
        if subscription.mutedAt != nil { parts.append(String(localized: "Muted")) }
        return parts.joined(separator: " · ")
    }

    private func leftText(_ requested: Date) -> String {
        let when = requested.formatted(.relative(presentation: .named))
        switch subscription.method {
        case "mail": return String(localized: "Unsubscribe sent by mail \(when)")
        case "link": return String(localized: "Unsubscribed on the sender's own page \(when)")
        default: return String(localized: "Unsubscribed \(when)")
        }
    }
}

/// SubscriptionReader is what one list sent, newest first, with the
/// actions that apply to all of it: read, archive, junk, move, delete,
/// pictures, mute and leaving.
struct SubscriptionReader: View {
    let mail: MailModel
    @State var subscription: MailSubscription
    let onChange: () -> Void
    @Environment(\.openURL) private var openURL
    @Environment(\.dismiss) private var dismiss
    @State private var thread: MailThreadView?
    @State private var expanded = Set<String>()
    @State private var isConfirmingLeave = false
    @State private var isConfirmingDelete = false

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 10) {
                    SenderLogo(domain: subscription.logoDomain, name: subscription.name, size: 44)
                    VStack(alignment: .leading) {
                        Text(subscription.name.isEmpty ? subscription.from : subscription.name).font(.title3.weight(.semibold))
                        Text(subscription.from).font(.footnote).foregroundStyle(.secondary)
                    }
                }
                if thread?.truncated == true {
                    Text("Only the newest \(thread?.entries.count ?? 0) messages are shown").font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(thread?.entries ?? [], id: \.item?.id) { entry in
                    if let item = entry.item {
                        MessageCard(mail: mail, entry: entry, item: item, isOpen: expanded.contains(item.id), isOtherFolder: true) {
                            if expanded.contains(item.id) { expanded.remove(item.id) } else { expanded.insert(item.id) }
                        }
                    }
                }
                if thread == nil {
                    ProgressView().frame(maxWidth: .infinity, minHeight: 200)
                }
            }
            .readableWidth()
            .padding()
        }
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .toolbar { toolbar }
        // The bar at the bottom is this screen's own, so the tab bar gives way to it.
        .toolbarVisibility(.hidden, for: .tabBar)
    }

    private var items: [MailItem] { (thread?.entries ?? []).compactMap(\.item).filter { !$0.draft } }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        let isAnyUnread = items.contains { !$0.seen }
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Button(isAnyUnread ? LocalizedStringKey("Mark all read") : "Mark all unread", systemImage: isAnyUnread ? "envelope.open" : "envelope.badge") {
                    Task {
                        let itemIds = items.map(\.id)
                        _ = await mail.act(saying: isAnyUnread ? String(localized: "Marked read") : String(localized: "Marked unread")) {
                            try await mail.connection.setMailFlags(itemIds, isSeen: isAnyUnread)
                        }
                        await changed()
                    }
                }
                Button(subscription.imagesAt == nil ? LocalizedStringKey("Always show pictures") : "Ask before showing pictures", systemImage: "photo") {
                    Task { await showPictures(subscription.imagesAt == nil) }
                }
                Button(subscription.mutedAt == nil ? LocalizedStringKey("Mute") : "Unmute", systemImage: subscription.mutedAt == nil ? "bell.slash" : "bell") {
                    Task { await mute(subscription.mutedAt == nil) }
                }
                if items.allSatisfy({ mail.folder($0.folderId)?.kind == "junk" }) {
                    Button("Not junk", systemImage: "hand.thumbsup") { Task { await junk(isNotJunk: true) } }
                } else {
                    Button("Report junk", systemImage: "xmark.bin") { Task { await junk(isNotJunk: false) } }
                }
                Menu("Move to…", systemImage: "folder") {
                    ForEach(MailFolders.rows(mail.folders), id: \.folder.id) { row in
                        Button(String(repeating: "  ", count: row.depth) + MailNames.folder(row.folder)) {
                            Task {
                                _ = await mail.move(MailTarget(items: items), to: row.folder.id, saying: String(localized: "Moved to \(MailNames.folder(row.folder))"))
                                await changed()
                            }
                        }
                    }
                }
                Button("Delete…", systemImage: "trash", role: .destructive) { isConfirmingDelete = true }
                    .tint(.red)
            } label: {
                Label("Actions", systemImage: "ellipsis.circle")
            }
            .disabled(items.isEmpty)
            .confirmationDialog("Delete every message from this list?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                Button("Delete \(items.count) messages", role: .destructive) {
                    Task {
                        let target = MailTarget(items: items)
                        _ = await mail.delete(target, isForever: false, saying: String(localized: "Moved to Trash"))
                        await changed()
                    }
                }
            }
        }
        ToolbarItemGroup(placement: .bottomBar) {
            if let archive = mail.folder(kind: "archive") {
                Button {
                    Task {
                        let target = MailTarget(items: items.filter { $0.folderId != archive.id })
                        _ = await mail.move(target, to: archive.id, saying: String(localized: "Archived"))
                        await changed()
                    }
                } label: {
                    Label("Archive all", systemImage: "archivebox")
                }
                .disabled(items.isEmpty)
            }
            Spacer()
            Button("Unsubscribe", role: .destructive) { isConfirmingLeave = true }
                .disabled(subscription.requestedAt != nil && !subscription.failed)
            .confirmationDialog(leaveTitle, isPresented: $isConfirmingLeave, titleVisibility: .visible) {
                if subscription.leaveKind == "none" {
                    if subscription.mutedAt == nil {
                        Button("Mute instead") { Task { await mute(true) } }
                    }
                } else {
                    Button("Unsubscribe", role: .destructive) { Task { await leave() } }
                }
            } message: {
                Text(leaveMessage)
            }
        }
    }

    private var leaveTitle: String {
        subscription.leaveKind == "none" ? String(localized: "This list gives no way out") : String(localized: "Leave \(subscription.name)?")
    }

    private var leaveMessage: String {
        switch subscription.leaveKind {
        case "oneClick": String(localized: "The sender is told at once, the way the list asks to be left.")
        case "mail": String(localized: "A message asking to leave is sent from this mailbox.")
        case "link": String(localized: "The sender's own page opens to finish leaving there.")
        default:
            subscription.stripped
                ? String(localized: "The way out was taken off the message on its way here. Muting keeps it out of the Inbox.")
                : String(localized: "The list says nothing about how to leave it. Muting keeps it out of the Inbox.")
        }
    }

    private func load() async {
        guard let mailboxId = mail.mailbox?.id else { return }
        do {
            let read = try await mail.connection.readMailSubscription(mailboxId: mailboxId, key: subscription.key)
            let isFirst = thread == nil
            thread = read
            if isFirst {
                // The newest and every unread one open, and are marked read.
                let all = read.entries.compactMap(\.item)
                var open = Set(all.filter { !$0.seen }.map(\.id))
                if let newest = all.first { open.insert(newest.id) }
                expanded = open
                let unread = all.filter { !$0.seen && open.contains($0.id) }.map(\.id)
                if !unread.isEmpty {
                    try? await mail.connection.setMailFlags(unread, isSeen: true)
                    onChange()
                }
            }
        } catch {
            mail.problemText = error.localizedDescription
        }
    }

    private func changed() async {
        await load()
        await refresh()
        onChange()
    }

    /// Reads the list's own row again, for its muted, pictures and leaving
    /// marks.
    private func refresh() async {
        guard let mailboxId = mail.mailbox?.id,
            let page = try? await mail.connection.mailSubscriptions(mailboxId: mailboxId, isLeft: subscription.requestedAt != nil, matching: subscription.name, first: 50, offset: 0),
            let read = page.subscriptions.compactMap({ $0 }).first(where: { $0.id == subscription.id })
        else { return }
        subscription = read
    }

    private func showPictures(_ isShown: Bool) async {
        guard let mailboxId = mail.mailbox?.id else { return }
        _ = await mail.act(saying: isShown ? String(localized: "Pictures from this list will be shown") : String(localized: "Pictures from this list will be asked about")) {
            try await mail.connection.showSubscriptionImages(mailboxId: mailboxId, key: subscription.key, isShown: isShown)
        }
        await changed()
    }

    private func mute(_ isMuted: Bool) async {
        guard let mailboxId = mail.mailbox?.id else { return }
        _ = await mail.act(saying: isMuted ? String(localized: "Kept out of the Inbox") : String(localized: "Back in the Inbox")) {
            try await mail.connection.muteSubscription(mailboxId: mailboxId, key: subscription.key, isMuted: isMuted)
        }
        await changed()
    }

    private func junk(isNotJunk: Bool) async {
        let said = isNotJunk ? String(localized: "Said this is not junk") : String(localized: "Reported as junk")
        _ = await mail.act(saying: said) {
            try await mail.connection.reportJunk(items.map(\.id), isNotJunk: isNotJunk)
        }
        await changed()
    }

    private func leave() async {
        guard let mailboxId = mail.mailbox?.id else { return }
        if subscription.leaveKind == "link", let page = subscription.leavePage {
            openURL(page)
        }
        _ = await mail.act(saying: String(localized: "Left \(subscription.name)")) {
            try await mail.connection.unsubscribe(mailboxId: mailboxId, key: subscription.key)
        }
        await changed()
    }
}
