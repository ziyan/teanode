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

/// PasswordsPins are the public keys this device holds for other people. They
/// are about other people, not about the person's own setup, so they are kept
/// in a file of their own that survives the cache being dropped (a server that
/// answers with another public key must not be able to make the phone forget
/// them) and goes only when the sign-in is removed.
public struct PasswordsPins: Codable, Equatable, Sendable {
    public static let currentFormatVersion = 1

    public var formatVersion: Int
    /// The signed-in person's id, so one person's pins are never read as another's.
    public var userId: String
    /// The key each person had the first time one of their shared items arrived, by user id
    /// (standard base64, 65 bytes). Nobody compared its safety code. It opens that person's
    /// shared items and warns when their key changes; it never lets this device seal a key.
    public var seenPublicKeys: [String: String]
    /// The key the person confirmed by comparing its safety code, by user id. Only these keys
    /// let this device seal an item key for somebody without asking.
    public var confirmedPublicKeys: [String: String]

    public init(userId: String, seenPublicKeys: [String: String] = [:], confirmedPublicKeys: [String: String] = [:]) {
        self.formatVersion = Self.currentFormatVersion
        self.userId = userId
        self.seenPublicKeys = seenPublicKeys
        self.confirmedPublicKeys = confirmedPublicKeys
    }

    /// The key to open a person's shared items with and to compare their items against: the
    /// confirmed one when there is one, else the one first seen.
    public func ownerPublicKey(forUserId otherUserId: String) -> String? {
        confirmedPublicKeys[otherUserId] ?? seenPublicKeys[otherUserId]
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
    /// The key each owner of a shared item had when the first of their items arrived, by
    /// user id (standard base64, 65 bytes). Public data. It opens that owner's items and
    /// warns when the key changes; nobody compared its safety code, so it never lets this
    /// device seal an item key. Stored in the pins file, not in this one.
    public var seenPublicKeys: [String: String]
    /// The keys the person confirmed by comparing safety codes, by user id. A device seals an
    /// item key without asking only for a key in here. Stored in the pins file, not in this one.
    public var confirmedPublicKeys: [String: String]

    /// Items the person removed somebody from, or stopped the agent using, whose new item key
    /// could not be made yet (the call failed after the narrowing went through). The next
    /// save, open or retry gives them one.
    public var itemIdsNeedingNewKey: [String]

    /// Both maps, as the pins file holds them.
    public var pins: PasswordsPins {
        get { PasswordsPins(userId: userId, seenPublicKeys: seenPublicKeys, confirmedPublicKeys: confirmedPublicKeys) }
        set {
            seenPublicKeys = newValue.seenPublicKeys
            confirmedPublicKeys = newValue.confirmedPublicKeys
        }
    }

    public init(
        userId: String, publicKey: String, privateKeyByAccountKey: String, revision: Int64 = 0, lockAfterMinutes: Int,
        items: [PasswordItemView] = [], seenPublicKeys: [String: String] = [:], confirmedPublicKeys: [String: String] = [:],
        itemIdsNeedingNewKey: [String] = []
    ) {
        self.formatVersion = Self.currentFormatVersion
        self.userId = userId
        self.publicKey = publicKey
        self.privateKeyByAccountKey = privateKeyByAccountKey
        self.revision = revision
        self.lockAfterMinutes = lockAfterMinutes
        self.items = items
        self.seenPublicKeys = seenPublicKeys
        self.confirmedPublicKeys = confirmedPublicKeys
        self.itemIdsNeedingNewKey = itemIdsNeedingNewKey
    }

    private enum CodingKeys: String, CodingKey {
        case formatVersion, userId, publicKey, privateKeyByAccountKey, revision, lockAfterMinutes, items, itemIdsNeedingNewKey
    }

    /// The key an earlier build wrote its pinned keys under.
    private enum EarlierCodingKeys: String, CodingKey {
        case knownPublicKeys
    }

    /// A file written before pins existed has none. The keys an earlier build pinned on first
    /// sight (some of them never confirmed) come back as seen keys only, so the person is asked
    /// once before this device seals anything for them. The file itself no longer holds pins: they
    /// are in the pins file, which `PasswordsCache.load()` reads in.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        formatVersion = try container.decode(Int.self, forKey: .formatVersion)
        userId = try container.decode(String.self, forKey: .userId)
        publicKey = try container.decode(String.self, forKey: .publicKey)
        privateKeyByAccountKey = try container.decode(String.self, forKey: .privateKeyByAccountKey)
        revision = try container.decode(Int64.self, forKey: .revision)
        lockAfterMinutes = try container.decode(Int.self, forKey: .lockAfterMinutes)
        items = try container.decode([PasswordItemView].self, forKey: .items)
        let earlier = try decoder.container(keyedBy: EarlierCodingKeys.self)
        seenPublicKeys = try earlier.decodeIfPresent([String: String].self, forKey: .knownPublicKeys) ?? [:]
        confirmedPublicKeys = [:]
        itemIdsNeedingNewKey = try container.decodeIfPresent([String].self, forKey: .itemIdsNeedingNewKey) ?? []
    }

    /// Writes everything but the pins, which go to the pins file.
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(formatVersion, forKey: .formatVersion)
        try container.encode(userId, forKey: .userId)
        try container.encode(publicKey, forKey: .publicKey)
        try container.encode(privateKeyByAccountKey, forKey: .privateKeyByAccountKey)
        try container.encode(revision, forKey: .revision)
        try container.encode(lockAfterMinutes, forKey: .lockAfterMinutes)
        try container.encode(items, forKey: .items)
        try container.encode(itemIdsNeedingNewKey, forKey: .itemIdsNeedingNewKey)
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
/// a file in the app group container), and the pins file beside it.
public struct PasswordsCache: Sendable {
    public let fileURL: URL

    public init(fileURL: URL) {
        self.fileURL = fileURL
    }

    /// The pins file that goes with a cache file: the same name with `.keys.json`
    /// in place of `.json`, in the same folder.
    public static func pinsFileURL(cacheFileURL: URL) -> URL {
        cacheFileURL.deletingPathExtension().appendingPathExtension("keys.json")
    }

    public var pinsFileURL: URL { Self.pinsFileURL(cacheFileURL: fileURL) }

    /// The contents, with the pins read in from the pins file; nil when there is no
    /// file. A file that cannot be read throws, so a reader can tell "never synced"
    /// from "damaged". A cache file written before the pins file existed keeps the keys it
    /// holds as seen keys, until the next save moves them.
    public func load() throws -> PasswordsCacheContents? {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return nil }
        let data = try Data(contentsOf: fileURL)
        var contents: PasswordsCacheContents
        do {
            contents = try JSONDecoder().decode(PasswordsCacheContents.self, from: data)
        } catch {
            throw PasswordsCacheError.unreadable
        }
        guard contents.formatVersion <= PasswordsCacheContents.currentFormatVersion else {
            throw PasswordsCacheError.unsupportedVersion
        }
        if let stored = storedPins(userId: contents.userId) { contents.pins = stored }
        return contents
    }

    /// The pins kept for this person, whether or not the cache file is there; empty when there are none.
    public func loadPins(userId: String) -> PasswordsPins {
        storedPins(userId: userId) ?? PasswordsPins(userId: userId)
    }

    /// The pins file's contents when it is there, readable and this person's.
    private func storedPins(userId: String) -> PasswordsPins? {
        guard let data = try? Data(contentsOf: pinsFileURL),
            let pins = try? JSONDecoder().decode(PasswordsPins.self, from: data),
            pins.formatVersion <= PasswordsPins.currentFormatVersion, pins.userId == userId
        else { return nil }
        return pins
    }

    /// Writes the contents whole and atomically, readable after the phone's
    /// first unlock (the extension may run while it is locked again) and left
    /// out of backups. The pins go to their own file first, so a write that stops
    /// halfway never loses them.
    public func save(_ contents: PasswordsCacheContents) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        try write(try encoder.encode(contents.pins), to: pinsFileURL)
        try write(try encoder.encode(contents), to: fileURL)
    }

    /// Writes only the pins, for when there is no cache file to write them with.
    public func savePins(_ pins: PasswordsPins) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        try write(try encoder.encode(pins), to: pinsFileURL)
    }

    private func write(_ data: Data, to targetURL: URL) throws {
        let directoryURL = targetURL.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directoryURL, withIntermediateDirectories: true)
        #if os(iOS)
            try data.write(to: targetURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        #else
            try data.write(to: targetURL, options: [.atomic])
        #endif
        #if canImport(Darwin)
            // A new file each time (the write is atomic), so set on each.
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            var excludedURL = targetURL
            try excludedURL.setResourceValues(values)
            // The folder too, so nothing else put there (a temporary file of an atomic write) is backed up.
            var excludedDirectoryURL = directoryURL
            try excludedDirectoryURL.setResourceValues(values)
        #endif
    }

    /// Removes the cache file and keeps the pins file. For when the server's answers
    /// say the copy is not this setup's any more: the pins are about other people's keys,
    /// which the server must not be able to make the phone forget. Nothing happens when there is no file.
    public func eraseKeepingPins() throws {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return }
        try FileManager.default.removeItem(at: fileURL)
    }

    /// Removes the cache file and the pins file. Nothing happens when there are none.
    public func erase() throws {
        try eraseKeepingPins()
        guard FileManager.default.fileExists(atPath: pinsFileURL.path) else { return }
        try FileManager.default.removeItem(at: pinsFileURL)
    }
}
