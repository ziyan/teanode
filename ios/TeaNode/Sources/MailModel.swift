import Foundation
import Observation
import TeaNodeKit

/// MailPlace is a list the mail tab can show: a folder, the two views that
/// gather from every folder, or the mailing lists.
enum MailPlace: Hashable {
    case folder(String)
    case starred
    case priority
    case subscriptions
}

/// MailTarget is what an action acts on: the items, and where each came
/// from, so the action can be undone. A conversation's row in a folder
/// stands for all its messages there, whose home is that folder.
struct MailTarget {
    var itemIds: [String]
    var homes: [String: String] = [:]
    var home: String

    init(items: [MailItem]) {
        itemIds = items.map(\.id)
        homes = Dictionary(items.map { ($0.mailId, $0.folderId) }, uniquingKeysWith: { first, _ in first })
        home = items.first?.folderId ?? ""
    }

    init(itemIds: [String], home: String) {
        self.itemIds = itemIds
        self.home = home
    }

    func home(of mailId: String) -> String { homes[mailId] ?? home }
}

/// MailModel is the person's mailboxes as the dashboard's rail keeps them:
/// the one being read, its folders and counts, read again after each change
/// and whenever the app comes back.
@MainActor
@Observable
final class MailModel {
    private(set) var entries: [MailboxEntry] = []
    private(set) var isLoaded = false
    private(set) var categories: [String] = []
    var doneText: String?
    var problemText: String?
    /// What the last action can be undone with, offered beside its toast.
    var undo: (@MainActor () async -> Void)?

    let connection: Connection
    /// Whose mail it is, to keep what is remembered on the phone apart.
    let username: String
    private let currentKey: String

    var currentMailboxId: String? {
        didSet { UserDefaults.standard.set(currentMailboxId, forKey: currentKey) }
    }

    init(connection: Connection, username: String) {
        self.connection = connection
        self.username = username
        currentKey = "mail-current-mailbox.\(username)"
        currentMailboxId = UserDefaults.standard.string(forKey: currentKey)
    }

    var current: MailboxEntry? {
        entries.first { $0.mailbox?.id == currentMailboxId } ?? entries.first
    }

    var mailbox: Mailbox? { current?.mailbox }
    var folders: [MailFolder] { current?.presentFolders ?? [] }

    func folder(_ id: String) -> MailFolder? {
        entries.lazy.flatMap(\.presentFolders).first { $0.id == id }
    }

    func folder(kind: String) -> MailFolder? {
        MailFolders.folder(kind: kind, in: folders)
    }

    /// The folders for the list: pinned ones first in the order they were
    /// pinned, each with its children, then the rest of the tree.
    var railRows: (pinned: [(folder: MailFolder, depth: Int)], rest: [(folder: MailFolder, depth: Int)]) {
        let rows = MailFolders.rows(folders)
        let pinnedIds = folders.filter { $0.pinnedAt != nil && $0.kind != "inbox" }
            .sorted { ($0.pinnedAt ?? "") < ($1.pinnedAt ?? "") }
            .map(\.id)
        var pinned: [(folder: MailFolder, depth: Int)] = []
        var taken = Set<String>()
        for pinnedId in pinnedIds where !taken.contains(pinnedId) {
            guard let start = rows.firstIndex(where: { $0.folder.id == pinnedId }) else { continue }
            let depth = rows[start].depth
            var index = start
            repeat {
                pinned.append((rows[index].folder, rows[index].depth - depth))
                taken.insert(rows[index].folder.id)
                index += 1
            } while index < rows.count && rows[index].depth > depth
        }
        return (pinned, rows.filter { !taken.contains($0.folder.id) })
    }

    func load() async {
        do {
            entries = try await connection.mailboxes()
            if current?.mailbox?.id != currentMailboxId {
                currentMailboxId = current?.mailbox?.id
            }
            isLoaded = true
            if categories.isEmpty, mailbox?.agent?.isSorting == true {
                categories = (try? await connection.agentCategories()) ?? []
            }
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// The agent's categories, or the fixed list when it has none.
    var sortCategories: [String] {
        categories.isEmpty
            ? ["personal", "work", "newsletter", "notification", "receipt", "promotion", "social", "invitation", "phishing", "junk", "other"]
            : categories
    }

    /// Runs an action, says what it did, and reads the counts again. An
    /// undo is offered when the action has one.
    func act(saying said: String, undo: (@MainActor () async -> Void)? = nil, _ work: @MainActor () async throws -> Void) async -> Bool {
        do {
            try await work()
            self.undo = undo
            doneText = said
            await load()
            return true
        } catch {
            self.undo = nil
            problemText = String(localized: "That did not work: \(error.localizedDescription)")
            await load()
            return false
        }
    }

    // MARK: Actions with an undo

    /// Moves messages, offering to move them back to where each came from.
    func move(_ target: MailTarget, to folderId: String, saying said: String) async -> Bool {
        var moved: [MailMoved] = []
        return await act(saying: said, undo: { [weak self] in
            await self?.putBack(moved, target: target)
        }) {
            moved = try await connection.moveMail(target.itemIds, to: folderId)
        }
    }

    /// Moves messages to Trash, or out of it for good. From another folder
    /// it can be undone: what landed in Trash is found and moved home.
    func delete(_ target: MailTarget, isForever: Bool, saying said: String) async -> Bool {
        guard !isForever, let trash = folder(kind: "trash") else {
            return await act(saying: said) { try await connection.deleteMail(target.itemIds) }
        }
        let since = Date().addingTimeInterval(-1)
        return await act(saying: said, undo: { [weak self] in
            guard let self else { return }
            let landed = await self.landed(in: trash.id, since: since)
            await self.putBack(landed, target: target)
        }) {
            try await connection.deleteMail(target.itemIds)
        }
    }

    /// Reports junk, or not junk; undone by reporting the other way and
    /// moving each message back to where it was.
    func reportJunk(_ target: MailTarget, isNotJunk: Bool, saying said: String) async -> Bool {
        let since = Date().addingTimeInterval(-1)
        let landingKind = isNotJunk ? "inbox" : "junk"
        let returnKind = isNotJunk ? "junk" : "inbox"
        return await act(saying: said, undo: { [weak self] in
            guard let self, let landing = self.folder(kind: landingKind) else { return }
            let landed = await self.landed(in: landing.id, since: since)
            let undoneSince = Date().addingTimeInterval(-1)
            try? await self.connection.reportJunk(landed.map(\.id), isNotJunk: !isNotJunk)
            if let back = self.folder(kind: returnKind) {
                let mailIds = Set(landed.map(\.mailId))
                let returned = await self.landed(in: back.id, since: undoneSince).filter { mailIds.contains($0.mailId) }
                await self.putBack(returned.filter { target.home(of: $0.mailId) != back.id }, target: target)
            }
            await self.load()
        }) {
            try await connection.reportJunk(target.itemIds, isNotJunk: isNotJunk)
        }
    }

    /// The items that arrived in a folder since a time, as the dashboard
    /// finds what an action put there.
    private func landed(in folderId: String, since: Date) async -> [MailMoved] {
        let newest = (try? await connection.newestMailItems(folderId: folderId)) ?? []
        return newest.filter { item in
            guard let addedAt = ServerTime.date(item.addedAt) else { return false }
            return addedAt >= since
        }
    }

    /// Moves items back to the folders they came from, a folder at a time.
    private func putBack(_ moved: [MailMoved], target: MailTarget) async {
        let byHome = Dictionary(grouping: moved) { target.home(of: $0.mailId) }
        for (home, items) in byHome where !home.isEmpty {
            _ = try? await connection.moveMail(items.map(\.id), to: home)
        }
        undo = nil
        doneText = String(localized: "Undone")
        await load()
    }
}
