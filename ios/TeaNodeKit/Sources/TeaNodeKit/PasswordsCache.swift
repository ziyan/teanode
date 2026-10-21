import Foundation

#if canImport(CryptoKit)
    import CryptoKit
#else
    import Crypto
#endif

// The phone's copy of the person's Passwords: files the app writes in the app
// group's container and the AutoFill extension reads, so the extension never
// needs the network. They hold only what the server sends, which is sealed
// text, plus the person's key pair (the private half sealed by the account
// key). Nothing in them opens without the account key, which lives in the
// keychain behind Face ID (PasswordsKeyStore.swift). The wrapped account key
// that a passphrase or recovery key opens is not stored here.
//
// The copy is a small header file (who it is for, the key pair, the revision),
// one file for each item, and a summary of the logins that the app seals with
// the account key (PasswordsAutoFillSummary.swift). The AutoFill extension runs
// under a memory limit, so it reads the header and the summary to list logins
// and opens only the one item it is asked to fill, never the whole copy. The
// app and the extension may write at the same time: every change is made under
// a file coordinator and merged with what is on disk, so neither undoes the
// other. The keys the device holds for other people are not in these files:
// they are in a pin store (PasswordsPinStore.swift), the keychain on the phone.

/// PasswordsCacheError is why the cache file could not be used.
public enum PasswordsCacheError: Error, Equatable, Sendable {
    /// The file is not a cache this build can read.
    case unreadable
    /// The file was written by a newer build.
    case unsupportedVersion
    /// What was to be written is over the limit set for it (the AutoFill summary's byte budget).
    case tooLarge
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

/// PasswordsItemMemory is what this device remembers about one item the person owns, from
/// the last time it looked at the item in a state nobody was disputing. It lives with the
/// pinned keys, in the Passwords keychain group, because what it protects is the owner's
/// own record of who may have the item: the share extension and the widgets cannot write it.
public struct PasswordsItemMemory: Codable, Equatable, Sendable {
    /// The highest `recordVersion` this device has seen on the item's access record with a
    /// valid signature. A record with a lower version is an older one the server sent back.
    public var highestRecordVersion: Int
    /// The user ids that held a key for the item when this device last saw it without the
    /// server asking for a new key. An automatic new key is sealed only for these people.
    public var keyHolderUserIds: [String]
    /// Whether the item had a copy of its key for the agent at that time.
    public var hadAgentCopy: Bool

    public init(highestRecordVersion: Int = 0, keyHolderUserIds: [String] = [], hadAgentCopy: Bool = false) {
        self.highestRecordVersion = highestRecordVersion
        self.keyHolderUserIds = keyHolderUserIds
        self.hadAgentCopy = hadAgentCopy
    }

    /// Most key holders remembered for one item. Somebody beyond this is left waiting for the
    /// person's OK after an automatic new key, which is the safe side.
    public static let mostKeyHolderUserIds = 1000
}

/// PasswordsPins are the public keys this device holds for other people, and what it
/// remembers about each item the person owns. They are about other people and the server's
/// past answers, not about the person's own setup, so they are kept in a pin store of their
/// own that survives the cache being dropped (a server that answers with another public key
/// or an older record must not be able to make the phone forget them) and goes only when the
/// sign-in is removed.
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
    /// What this device remembers about each item the person owns, by item id.
    public var itemMemories: [String: PasswordsItemMemory]

    public init(
        userId: String, seenPublicKeys: [String: String] = [:], confirmedPublicKeys: [String: String] = [:],
        itemMemories: [String: PasswordsItemMemory] = [:]
    ) {
        self.formatVersion = Self.currentFormatVersion
        self.userId = userId
        self.seenPublicKeys = seenPublicKeys
        self.confirmedPublicKeys = confirmedPublicKeys
        self.itemMemories = itemMemories
    }

    private enum CodingKeys: String, CodingKey {
        case formatVersion, userId, seenPublicKeys, confirmedPublicKeys, itemMemories
    }

    /// Reads pins written before items had a memory too: they have none.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        formatVersion = try container.decode(Int.self, forKey: .formatVersion)
        userId = try container.decode(String.self, forKey: .userId)
        seenPublicKeys = try container.decode([String: String].self, forKey: .seenPublicKeys)
        confirmedPublicKeys = try container.decode([String: String].self, forKey: .confirmedPublicKeys)
        itemMemories = try container.decodeIfPresent([String: PasswordsItemMemory].self, forKey: .itemMemories) ?? [:]
    }

    /// The key to open a person's shared items with and to compare their items against: the
    /// confirmed one when there is one, else the one first seen.
    public func ownerPublicKey(forUserId otherUserId: String) -> String? {
        confirmedPublicKeys[otherUserId] ?? seenPublicKeys[otherUserId]
    }
}

/// PasswordsCacheContents is the cache's header and, when it is loaded whole,
/// its items. The header is what the file named by `PasswordsCache.fileURL`
/// holds; each item is a file of its own.
public struct PasswordsCacheContents: Codable, Equatable, Sendable {
    public static let currentFormatVersion = 2

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
    /// Every item the person can reach, owned ones deleted in the last 30 days included. Each is
    /// stored in a file of its own, not in the header; a header loaded without its items has none.
    public var items: [PasswordItemView]
    /// The key each owner of a shared item had when the first of their items opened, by user
    /// id (standard base64, 65 bytes). Public data. It opens that owner's items and warns when
    /// the key changes; nobody compared its safety code, so it never lets this device seal an
    /// item key. Read from the pin store, not from the cache files.
    public var seenPublicKeys: [String: String]
    /// The keys the person confirmed by comparing safety codes, by user id. A device seals an
    /// item key without asking only for a key in here. Read from the pin store, not from the cache files.
    public var confirmedPublicKeys: [String: String]
    /// What this device remembers about each item the person owns, by item id. Read from the pin
    /// store, not from the cache files.
    public var itemMemories: [String: PasswordsItemMemory]

    /// Everything the pin store holds.
    public var pins: PasswordsPins {
        get {
            PasswordsPins(
                userId: userId, seenPublicKeys: seenPublicKeys, confirmedPublicKeys: confirmedPublicKeys, itemMemories: itemMemories)
        }
        set {
            seenPublicKeys = newValue.seenPublicKeys
            confirmedPublicKeys = newValue.confirmedPublicKeys
            itemMemories = newValue.itemMemories
        }
    }

    public init(
        userId: String, publicKey: String, privateKeyByAccountKey: String, revision: Int64 = 0, lockAfterMinutes: Int,
        items: [PasswordItemView] = [], seenPublicKeys: [String: String] = [:], confirmedPublicKeys: [String: String] = [:],
        itemMemories: [String: PasswordsItemMemory] = [:]
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
        self.itemMemories = itemMemories
    }

    private enum CodingKeys: String, CodingKey {
        case formatVersion, userId, publicKey, privateKeyByAccountKey, revision, lockAfterMinutes
    }

    /// Reads the header. The items and the pins are not in it: `PasswordsCache` reads them in.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        formatVersion = try container.decode(Int.self, forKey: .formatVersion)
        userId = try container.decode(String.self, forKey: .userId)
        publicKey = try container.decode(String.self, forKey: .publicKey)
        privateKeyByAccountKey = try container.decode(String.self, forKey: .privateKeyByAccountKey)
        revision = try container.decode(Int64.self, forKey: .revision)
        lockAfterMinutes = try container.decode(Int.self, forKey: .lockAfterMinutes)
        items = []
        seenPublicKeys = [:]
        confirmedPublicKeys = [:]
        itemMemories = [:]
    }

    /// Writes the header: neither the items nor the pins.
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(formatVersion, forKey: .formatVersion)
        try container.encode(userId, forKey: .userId)
        try container.encode(publicKey, forKey: .publicKey)
        try container.encode(privateKeyByAccountKey, forKey: .privateKeyByAccountKey)
        try container.encode(revision, forKey: .revision)
        try container.encode(lockAfterMinutes, forKey: .lockAfterMinutes)
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

/// PasswordsCache is the cache at a file URL the caller gives (the app passes
/// a file in the app group container): the header there, a folder of item files
/// and a summary file beside it, and a pin store for the keys of other people.
public struct PasswordsCache: Sendable {
    public let fileURL: URL
    let pinStore: any PasswordsPinStore

    /// `pinStore` keeps the keys pinned for other people. Without one they are in a file beside the
    /// cache file, which is only for tests and tools: the app and the AutoFill extension pass the
    /// keychain's (`PasswordsCache.forThisBundle`), because a file in the app group's folder can be
    /// written by the share extension and the widgets as well.
    public init(fileURL: URL, pinStore: (any PasswordsPinStore)? = nil) {
        self.fileURL = fileURL
        self.pinStore = pinStore ?? PasswordsFilePinStore(fileURL: Self.pinsFileURL(cacheFileURL: fileURL))
    }

    /// The pins file that goes with a cache file when no pin store is given: the same name with
    /// `.keys.json` in place of `.json`, in the same folder.
    public static func pinsFileURL(cacheFileURL: URL) -> URL {
        cacheFileURL.deletingPathExtension().appendingPathExtension("keys.json")
    }

    public var pinsFileURL: URL { Self.pinsFileURL(cacheFileURL: fileURL) }

    /// The folder that holds one file for each item.
    public static func itemsDirectoryURL(cacheFileURL: URL) -> URL {
        cacheFileURL.deletingPathExtension().appendingPathExtension("items")
    }

    public var itemsDirectoryURL: URL { Self.itemsDirectoryURL(cacheFileURL: fileURL) }

    /// The file the app writes the sealed summary of the logins in (PasswordsAutoFillSummary.swift).
    public static func summaryFileURL(cacheFileURL: URL) -> URL {
        cacheFileURL.deletingPathExtension().appendingPathExtension("summary.json")
    }

    public var summaryFileURL: URL { Self.summaryFileURL(cacheFileURL: fileURL) }

    /// The name of an item's file: the hash of its id, so that an id a server chose can neither
    /// name another file nor be too long to be one.
    static func itemFileName(id: String) -> String {
        SHA256.hash(data: Data(id.utf8)).map { String(format: "%02x", $0) }.joined() + ".json"
    }

    func itemFileURL(id: String) -> URL {
        itemsDirectoryURL.appendingPathComponent(Self.itemFileName(id: id))
    }

    // MARK: Coordinating

    /// Runs `body` with this process holding the cache for writing: no other process (the app, the
    /// AutoFill extension) reads or writes it meanwhile.
    private func coordinatedWrite<Output>(_ body: () throws -> Output) throws -> Output {
        #if canImport(Darwin)
            try Self.coordinate(url: fileURL, isWriting: true, body)
        #else
            try body()
        #endif
    }

    /// Runs `body` with the cache held for reading: not while another process is writing it.
    private func coordinatedRead<Output>(_ body: () throws -> Output) throws -> Output {
        #if canImport(Darwin)
            try Self.coordinate(url: fileURL, isWriting: false, body)
        #else
            try body()
        #endif
    }

    #if canImport(Darwin)
        private static func coordinate<Output>(url: URL, isWriting: Bool, _ body: () throws -> Output) throws -> Output {
            var coordinationError: NSError?
            var outcome: Result<Output, Error>?
            let coordinator = NSFileCoordinator(filePresenter: nil)
            func run(_: URL) {
                do {
                    outcome = .success(try body())
                } catch {
                    outcome = .failure(error)
                }
            }
            if isWriting {
                coordinator.coordinate(writingItemAt: url, options: [], error: &coordinationError, byAccessor: run)
            } else {
                coordinator.coordinate(readingItemAt: url, options: [], error: &coordinationError, byAccessor: run)
            }
            if let coordinationError { throw coordinationError }
            guard let outcome else { throw PasswordsCacheError.unreadable }
            return try outcome.get()
        }
    #endif

    // MARK: Reading

    /// The header as the file holds it, without items or pins; nil when there is no file.
    private func readHeader() throws -> PasswordsCacheContents? {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return nil }
        let data = try Data(contentsOf: fileURL)
        let header: PasswordsCacheContents
        do {
            header = try JSONDecoder().decode(PasswordsCacheContents.self, from: data)
        } catch {
            throw PasswordsCacheError.unreadable
        }
        guard header.formatVersion <= PasswordsCacheContents.currentFormatVersion else { throw PasswordsCacheError.unsupportedVersion }
        // A copy written before items had files of their own holds them in the header: it is not used.
        guard header.formatVersion == PasswordsCacheContents.currentFormatVersion else { throw PasswordsCacheError.unreadable }
        return header
    }

    /// The pins of this person, best effort: a pin store that cannot be read gives none here. The
    /// session reads them with `loadPins(userId:)`, which tells the two apart.
    private func pinsForReading(userId: String) -> PasswordsPins {
        (try? pinStore.loadPins(userId: userId)) ?? PasswordsPins(userId: userId)
    }

    /// The header and every item, with the pins read in; nil when there is no file. A file that
    /// cannot be read throws, so a reader can tell "never synced" from "damaged". An item file that
    /// cannot be read is left out: the next read of the server brings the item again.
    public func load() throws -> PasswordsCacheContents? {
        try coordinatedRead {
            guard var contents = try readHeader() else { return nil }
            contents.items = readEveryItem()
            contents.pins = pinsForReading(userId: contents.userId)
            return contents
        }
    }

    /// The header with the pins read in, but no items: what the AutoFill extension needs to open the
    /// key pair, because it opens one item at a time (`loadItem(id:)`). Nil when there is no file.
    public func loadHeader() throws -> PasswordsCacheContents? {
        try coordinatedRead {
            guard var contents = try readHeader() else { return nil }
            contents.pins = pinsForReading(userId: contents.userId)
            return contents
        }
    }

    /// One item, read from its own file; nil when the copy has none.
    public func loadItem(id: String) throws -> PasswordItemView? {
        try coordinatedRead { readItem(at: itemFileURL(id: id)) }
    }

    private func readItem(at url: URL) -> PasswordItemView? {
        guard let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode(PasswordItemView.self, from: data)
    }

    private func readEveryItem() -> [PasswordItemView] {
        guard let names = try? FileManager.default.contentsOfDirectory(atPath: itemsDirectoryURL.path) else { return [] }
        var items: [PasswordItemView] = []
        for name in names where name.hasSuffix(".json") {
            if let item = readItem(at: itemsDirectoryURL.appendingPathComponent(name)) { items.append(item) }
        }
        items.sort { $0.id < $1.id }
        return items
    }

    /// The pins kept for this person; throws `PasswordsPinStoreError.unreadable` when something is
    /// stored but cannot be read, so the caller keeps the keys it had and never reads that as none.
    public func loadPins(userId: String) throws -> PasswordsPins {
        try pinStore.loadPins(userId: userId)
    }

    // MARK: Writing

    /// Writes the contents, merged with what is on disk, and returns the revision written.
    ///
    /// Another process (the AutoFill extension saving a passkey while the app syncs) may have written
    /// since this one read, so nothing is written whole from a stale snapshot: the revision written is
    /// the higher of the two, only the items in `changedItemIds` are written (all of them when it is
    /// nil, which also removes the files of items that are not in `contents`), and only the items in
    /// `removedItemIds` are removed. The header is written last, so a write that stops halfway leaves
    /// items the next read of the server replaces.
    @discardableResult
    public func save(
        _ contents: PasswordsCacheContents, changedItemIds: Set<String>? = nil, removedItemIds: Set<String> = []
    ) throws -> Int64 {
        try coordinatedWrite {
            var header = contents
            if let onDisk = try? readHeader(), onDisk.userId == header.userId, onDisk.publicKey == header.publicKey {
                header.revision = max(header.revision, onDisk.revision)
            }
            try FileManager.default.createDirectory(at: itemsDirectoryURL, withIntermediateDirectories: true)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            for item in contents.items where changedItemIds?.contains(item.id) ?? true {
                try write(try encoder.encode(item), to: itemFileURL(id: item.id))
            }
            if changedItemIds == nil {
                let keptNames = Set(contents.items.map { Self.itemFileName(id: $0.id) })
                for name in (try? FileManager.default.contentsOfDirectory(atPath: itemsDirectoryURL.path)) ?? [] where !keptNames.contains(name) {
                    try? FileManager.default.removeItem(at: itemsDirectoryURL.appendingPathComponent(name))
                }
            }
            for itemId in removedItemIds {
                try? FileManager.default.removeItem(at: itemFileURL(id: itemId))
            }
            try write(try encoder.encode(header), to: fileURL)
            return header.revision
        }
    }

    /// The sealed summary the app wrote, as the file holds it; nil when there is none.
    public func loadSummaryData() throws -> Data? {
        try coordinatedRead { readSummaryFile() }
    }

    /// The summary file's bytes; nil when there is none, or when it is bigger than any summary
    /// this build writes (an earlier build did not limit it), without reading it.
    private func readSummaryFile() -> Data? {
        let size = (try? FileManager.default.attributesOfItem(atPath: summaryFileURL.path)[.size] as? NSNumber)?.intValue
        if let size, size > PasswordsAutoFillSummary.mostSealedBytes { return nil }
        return try? Data(contentsOf: summaryFileURL)
    }

    /// Replaces the summary file.
    public func saveSummaryData(_ data: Data) throws {
        try coordinatedWrite { try write(data, to: summaryFileURL) }
    }

    /// Runs `update` on the summary file's data under the write coordinator and writes what it
    /// returns, so two processes changing the summary one after the other do not lose a change.
    /// `update` gets nil when there is no file; returning nil removes it.
    public func updateSummaryData(_ update: (Data?) -> Data?) throws {
        try coordinatedWrite {
            let current = readSummaryFile()
            if let next = update(current) {
                try write(next, to: summaryFileURL)
            } else {
                try? FileManager.default.removeItem(at: summaryFileURL)
            }
        }
    }

    /// Records a key seen on the first shared item of an owner that opened, unless a key is known already.
    public func recordSeenIfAbsent(userId: String, ownerUserId: String, publicKey: String) throws {
        try pinStore.recordSeenIfAbsent(userId: userId, ownerUserId: ownerUserId, publicKey: publicKey)
    }

    /// Records a key the person confirmed by comparing safety codes.
    public func recordConfirmed(userId: String, ownerUserId: String, publicKey: String) throws {
        try pinStore.recordConfirmed(userId: userId, ownerUserId: ownerUserId, publicKey: publicKey)
    }

    /// Remembers that an owned item's access record was seen at this version with a valid
    /// signature; never lowers what is remembered.
    public func raiseRecordVersion(userId: String, itemId: String, version: Int) throws {
        try pinStore.raiseRecordVersion(userId: userId, itemId: itemId, version: version)
    }

    /// Remembers who held a key for an owned item, and whether it had an agent copy, as of now.
    public func recordKeyHolders(userId: String, itemId: String, keyHolderUserIds: [String], hadAgentCopy: Bool) throws {
        try pinStore.recordKeyHolders(userId: userId, itemId: itemId, keyHolderUserIds: keyHolderUserIds, hadAgentCopy: hadAgentCopy)
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

    // MARK: Erasing

    /// Removes the cache (header, items and summary) and keeps the pinned keys. For when the
    /// server's answers say the copy is not this setup's any more: the pins are about other
    /// people's keys, which the server must not be able to make the phone forget. Nothing
    /// happens when there is nothing.
    public func eraseKeepingPins() throws {
        try coordinatedWrite {
            for url in [fileURL, summaryFileURL, itemsDirectoryURL] where FileManager.default.fileExists(atPath: url.path) {
                try FileManager.default.removeItem(at: url)
            }
        }
    }

    /// Removes the cache and the pinned keys. Nothing happens when there are none.
    public func erase() throws {
        try eraseKeepingPins()
        try pinStore.removeAll()
    }
}
