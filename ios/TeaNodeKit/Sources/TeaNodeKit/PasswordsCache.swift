import Foundation

// The phone's copy of the person's Passwords: one JSON file the app writes in
// the app group's container and the AutoFill extension reads, so the
// extension never needs the network. It holds only what the server sends,
// which is sealed text, plus the person's key pair (the private half sealed by
// the account key). Nothing in it opens without the account key, which lives
// in the keychain behind Face ID (PasswordsKeyStore.swift). The wrapped
// account key that a passphrase or recovery key opens is not stored here.

/// PasswordsCacheError is why the cache file could not be used.
public enum PasswordsCacheError: Error, Equatable, Sendable {
    /// The file is not a cache this build can read.
    case unreadable
    /// The file was written by a newer build.
    case unsupportedVersion
}

/// PasswordsMergeResult is what a merge changed, for a screen that wants to
/// say so and for the session to forget what it had opened.
public struct PasswordsMergeResult: Equatable, Sendable {
    /// Items that are new or whose server copy was replaced.
    public var changedItemIds: [String]
    /// Items the person can no longer reach, which the merge dropped.
    public var removedItemIds: [String]

    public init(changedItemIds: [String] = [], removedItemIds: [String] = []) {
        self.changedItemIds = changedItemIds
        self.removedItemIds = removedItemIds
    }
}

/// PasswordsCacheContents is the cache file's contents.
public struct PasswordsCacheContents: Codable, Equatable, Sendable {
    public static let currentFormatVersion = 1

    public var formatVersion: Int
    /// The signed-in person's id, which a shared item's key is opened with.
    public var userId: String
    /// The person's public key, standard base64 (65 bytes).
    public var publicKey: String
    /// The person's private key sealed with the account key.
    public var privateKeyByAccountKey: String
    /// The highest revision the copy holds, asked for as `sinceRevision` next time.
    public var revision: Int64
    public var lockAfterMinutes: Int
    /// Every item the person can reach, owned ones deleted in the last 30 days included.
    public var items: [PasswordItemView]
    /// The public keys of the people the person has shared with or received from, by
    /// user id (standard base64, 65 bytes). Public data, kept in the clear. A device
    /// seals an item key only for a key in here (or one the person just confirmed
    /// by its safety code), and opens a shared item only with the owner key in here.
    public var knownPublicKeys: [String: String]

    public init(
        userId: String, publicKey: String, privateKeyByAccountKey: String, revision: Int64 = 0, lockAfterMinutes: Int,
        items: [PasswordItemView] = [], knownPublicKeys: [String: String] = [:]
    ) {
        self.formatVersion = Self.currentFormatVersion
        self.userId = userId
        self.publicKey = publicKey
        self.privateKeyByAccountKey = privateKeyByAccountKey
        self.revision = revision
        self.lockAfterMinutes = lockAfterMinutes
        self.items = items
        self.knownPublicKeys = knownPublicKeys
    }

    private enum CodingKeys: String, CodingKey {
        case formatVersion, userId, publicKey, privateKeyByAccountKey, revision, lockAfterMinutes, items, knownPublicKeys
    }

    /// A file written before known keys existed has none, which reads as no key known yet.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        formatVersion = try container.decode(Int.self, forKey: .formatVersion)
        userId = try container.decode(String.self, forKey: .userId)
        publicKey = try container.decode(String.self, forKey: .publicKey)
        privateKeyByAccountKey = try container.decode(String.self, forKey: .privateKeyByAccountKey)
        revision = try container.decode(Int64.self, forKey: .revision)
        lockAfterMinutes = try container.decode(Int.self, forKey: .lockAfterMinutes)
        items = try container.decode([PasswordItemView].self, forKey: .items)
        knownPublicKeys = try container.decodeIfPresent([String: String].self, forKey: .knownPublicKeys) ?? [:]
    }

    /// The item with this id, if the copy holds it.
    public func item(id: String) -> PasswordItemView? {
        items.first { $0.id == id }
    }

    /// Takes in a page of changes: the items it carries replace the copy's
    /// (or join it), and every item whose id the server no longer lists is
    /// dropped. The revision never moves backwards: it becomes the page's only
    /// when that is higher, so an answer that raced a change cannot make the next
    /// read ask for revisions the copy already has.
    @discardableResult
    public mutating func merge(_ list: PasswordItemListView) -> PasswordsMergeResult {
        var changedItemIds: [String] = []
        for item in list.items {
            if let index = items.firstIndex(where: { $0.id == item.id }) {
                items[index] = item
            } else {
                items.append(item)
            }
            changedItemIds.append(item.id)
        }
        let reachableItemIds = Set(list.itemIds)
        let removedItemIds = items.map(\.id).filter { !reachableItemIds.contains($0) }
        if !removedItemIds.isEmpty {
            items.removeAll { !reachableItemIds.contains($0.id) }
        }
        revision = max(revision, list.revision)
        return PasswordsMergeResult(changedItemIds: changedItemIds, removedItemIds: removedItemIds)
    }

    /// Puts one item the server just answered with (a save, a delete, a share
    /// change) into the copy. The copy's revision stays where it was: another
    /// device's change with a lower number may not have been fetched yet.
    public mutating func replace(_ item: PasswordItemView) {
        if let index = items.firstIndex(where: { $0.id == item.id }) {
            items[index] = item
        } else {
            items.append(item)
        }
    }
}

/// PasswordsCache is the cache file at a URL the caller gives (the app passes
/// a file in the app group container).
public struct PasswordsCache: Sendable {
    public let fileURL: URL

    public init(fileURL: URL) {
        self.fileURL = fileURL
    }

    /// The contents; nil when there is no file. A file that cannot be read
    /// throws, so a reader can tell "never synced" from "damaged".
    public func load() throws -> PasswordsCacheContents? {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return nil }
        let data = try Data(contentsOf: fileURL)
        let contents: PasswordsCacheContents
        do {
            contents = try JSONDecoder().decode(PasswordsCacheContents.self, from: data)
        } catch {
            throw PasswordsCacheError.unreadable
        }
        guard contents.formatVersion <= PasswordsCacheContents.currentFormatVersion else {
            throw PasswordsCacheError.unsupportedVersion
        }
        return contents
    }

    /// Writes the contents whole and atomically, readable after the phone's
    /// first unlock (the extension may run while it is locked again) and left
    /// out of backups.
    public func save(_ contents: PasswordsCacheContents) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let data = try encoder.encode(contents)
        let directoryURL = fileURL.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directoryURL, withIntermediateDirectories: true)
        #if os(iOS)
            try data.write(to: fileURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        #else
            try data.write(to: fileURL, options: [.atomic])
        #endif
        #if canImport(Darwin)
            // A new file each time (the write is atomic), so set on each.
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            var excludedURL = fileURL
            try excludedURL.setResourceValues(values)
            // The folder too, so nothing else put there (a temporary file of an atomic write) is backed up.
            var excludedDirectoryURL = directoryURL
            try excludedDirectoryURL.setResourceValues(values)
        #endif
    }

    /// Removes the file. Nothing happens when there is none.
    public func erase() throws {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return }
        try FileManager.default.removeItem(at: fileURL)
    }
}
