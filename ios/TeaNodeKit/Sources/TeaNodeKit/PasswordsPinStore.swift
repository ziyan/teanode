import Foundation

// Where a device keeps the public keys it holds for other people: the one key it
// knows for each person (the first it saw, replaced when the person's key
// changes), whether the person compared safety codes for it, and the key a
// changed one replaced until the person dismisses the notice. They decide which
// shared items open and for whom this device seals an item key, so whoever can
// write them can make the device seal for a key of their choosing.
//
// On the phone they live in the keychain, in the group only the app and the
// AutoFill extension have, one keychain item for each key. The share extension
// and the widgets share the app group's folder with the app, so a file there
// would be theirs to write; the keychain group is not. One item for each key
// also means two processes recording different keys at the same time cannot
// undo each other: each change is a single add or update of a single item,
// never a read of the whole record followed by a write of it.

/// PasswordsPinStoreError is why the pinned keys could not be read or written.
public enum PasswordsPinStoreError: Error, Equatable, Sendable {
    /// What is stored cannot be read. The caller keeps the keys it already knew; it never
    /// treats this as "there are none" and writes over it.
    case unreadable
    /// The keychain refused, with its status code.
    case failed(Int32)
}

/// PasswordsPinStore keeps the pinned keys of one sign-in.
public protocol PasswordsPinStore: Sendable {
    /// The keys pinned for this person. Empty when there are none; throws
    /// `PasswordsPinStoreError.unreadable` when something is stored but cannot be read.
    func loadPins(userId: String) throws -> PasswordsPins
    /// Records the first key seen for a person, only when none is known for them. A key that is
    /// already there is left alone.
    func recordKnownIfAbsent(userId: String, otherUserId: String, publicKey: String) throws
    /// Accepts another key for a person: it becomes the known one, and the key it replaced is
    /// kept until the notice is dismissed. Does nothing when it is the known key already.
    func recordChangedKey(userId: String, otherUserId: String, publicKey: String) throws
    /// Records that the person compared safety codes for this key. It becomes the known key if
    /// none was known, and the person's changed-key notice is cleared.
    func recordCompared(userId: String, otherUserId: String, publicKey: String) throws
    /// Dismisses the changed-key notice of a person.
    func dismissKeyChange(userId: String, otherUserId: String) throws
    /// Changes the memory kept for one owned item: reads it (an empty one when there is none),
    /// applies `change` and writes the result. Nothing is written when the change leaves it as it
    /// was. The three named operations below are built on this.
    func updateItemMemory(userId: String, itemId: String, change: (PasswordsItemMemory) -> PasswordsItemMemory) throws
    /// Forgets every pinned key and item memory of the sign-in. Only the person removing the
    /// account from the phone, or starting Passwords over, does this; a sign-in the server ended
    /// leaves them.
    func removeAll() throws
}

extension PasswordsPins {
    /// The pins with the first key seen for a person added, when none is known.
    func recordingKnownIfAbsent(otherUserId: String, publicKey: String) -> PasswordsPins {
        var recorded = self
        if recorded.knownPublicKeys[otherUserId] == nil { recorded.knownPublicKeys[otherUserId] = publicKey }
        return recorded
    }

    /// The pins with another key accepted for a person.
    func recordingChangedKey(otherUserId: String, publicKey: String) -> PasswordsPins {
        var recorded = self
        guard let previous = recorded.knownPublicKeys[otherUserId] else {
            recorded.knownPublicKeys[otherUserId] = publicKey
            return recorded
        }
        guard previous != publicKey else { return recorded }
        recorded.knownPublicKeys[otherUserId] = publicKey
        recorded.changedPublicKeys[otherUserId] = previous
        return recorded
    }

    /// The pins with a key marked as compared.
    func recordingCompared(otherUserId: String, publicKey: String) -> PasswordsPins {
        var recorded = self
        if recorded.knownPublicKeys[otherUserId] == nil { recorded.knownPublicKeys[otherUserId] = publicKey }
        recorded.comparedPublicKeys[otherUserId] = publicKey
        recorded.changedPublicKeys[otherUserId] = nil
        return recorded
    }
}

/// PasswordsSealedChange is what a device records after it sealed an item key itself.
public struct PasswordsSealedChange: Equatable, Sendable {
    /// The people sealed for.
    public var userIds: [String]
    /// Whether the people sealed for replace the ones remembered (a new item key was made, so only
    /// they hold it) or are added to them (a key was added for them beside the others).
    public var isReplacing: Bool
    /// Whether this device made the agent's copy; nil leaves what is remembered.
    public var didMakeAgentCopy: Bool?
    /// The people the person named directly as grantees on this device, replacing those remembered;
    /// nil leaves what is remembered.
    public var chosenUserIds: [String]?
    /// People sealed for through a circle for the first time, to be shown as a notice.
    public var circleShares: [PasswordsCircleShare]

    public init(
        userIds: [String], isReplacing: Bool, didMakeAgentCopy: Bool? = nil, chosenUserIds: [String]? = nil,
        circleShares: [PasswordsCircleShare] = []
    ) {
        self.userIds = userIds
        self.isReplacing = isReplacing
        self.didMakeAgentCopy = didMakeAgentCopy
        self.chosenUserIds = chosenUserIds
        self.circleShares = circleShares
    }
}

extension PasswordsPinStore {
    public func recordSeenRecord(userId: String, itemId: String, version: Int, mac: String?) throws {
        try updateItemMemory(userId: userId, itemId: itemId) { $0.seeingRecord(version: version, mac: mac) }
    }

    public func recordSealed(userId: String, itemId: String, change: PasswordsSealedChange) throws {
        try updateItemMemory(userId: userId, itemId: itemId) { $0.sealing(change) }
    }

    public func dismissCircleShare(userId: String, itemId: String, recipientUserId: String) throws {
        try updateItemMemory(userId: userId, itemId: itemId) {
            var dismissed = $0
            dismissed.circleShares.removeAll { $0.recipientUserId == recipientUserId }
            return dismissed
        }
    }
}

extension PasswordsItemMemory {
    /// The memory after a record with a valid signature was seen. The version only goes up. At the
    /// version already remembered, the mac is filled in when it was not known and otherwise kept.
    func seeingRecord(version: Int, mac: String?) -> PasswordsItemMemory {
        var seen = self
        if version > highestRecordVersion {
            seen.highestRecordVersion = version
            seen.highestRecordMac = mac
        } else if version == highestRecordVersion, highestRecordMac == nil {
            seen.highestRecordMac = mac
        }
        return seen
    }

    /// Whether a record with a valid signature is older than the newest this device saw, or has the
    /// same version and another mac (the server showed another record than the one seen).
    func isReplay(version: Int, mac: String?) -> Bool {
        if version < highestRecordVersion { return true }
        guard version == highestRecordVersion, let highestRecordMac else { return false }
        return mac != highestRecordMac
    }

    /// The memory after this device sealed an item key itself; the people are sorted and capped.
    func sealing(_ change: PasswordsSealedChange) -> PasswordsItemMemory {
        var sealed = self
        let people = change.isReplacing ? Set(change.userIds) : Set(sealedForUserIds).union(change.userIds)
        sealed.sealedForUserIds = Array(people.sorted().prefix(Self.mostSealedForUserIds))
        if let didMakeAgentCopy = change.didMakeAgentCopy { sealed.didMakeAgentCopy = didMakeAgentCopy }
        if let chosenUserIds = change.chosenUserIds {
            sealed.chosenUserIds = Array(Set(chosenUserIds).sorted().prefix(Self.mostSealedForUserIds))
        }
        for circleShare in change.circleShares where !sealed.circleShares.contains(where: { $0.recipientUserId == circleShare.recipientUserId }) {
            sealed.circleShares.append(circleShare)
        }
        sealed.circleShares = Array(sealed.circleShares.suffix(Self.mostCircleShares))
        return sealed
    }
}

// MARK: In memory

/// PasswordsMemoryPinStore keeps the keys in memory: for tests, for the demo, and
/// to stand in for the keychain when two caches must share one store.
public final class PasswordsMemoryPinStore: PasswordsPinStore, @unchecked Sendable {
    private let lock = NSLock()
    private var pinsByUserId: [String: PasswordsPins] = [:]

    public init() {}

    private func update(userId: String, _ change: (PasswordsPins) -> PasswordsPins) {
        lock.lock()
        defer { lock.unlock() }
        pinsByUserId[userId] = change(pinsByUserId[userId] ?? PasswordsPins(userId: userId))
    }

    public func loadPins(userId: String) throws -> PasswordsPins {
        lock.lock()
        defer { lock.unlock() }
        return pinsByUserId[userId] ?? PasswordsPins(userId: userId)
    }

    public func recordKnownIfAbsent(userId: String, otherUserId: String, publicKey: String) throws {
        update(userId: userId) { $0.recordingKnownIfAbsent(otherUserId: otherUserId, publicKey: publicKey) }
    }

    public func recordChangedKey(userId: String, otherUserId: String, publicKey: String) throws {
        update(userId: userId) { $0.recordingChangedKey(otherUserId: otherUserId, publicKey: publicKey) }
    }

    public func recordCompared(userId: String, otherUserId: String, publicKey: String) throws {
        update(userId: userId) { $0.recordingCompared(otherUserId: otherUserId, publicKey: publicKey) }
    }

    public func dismissKeyChange(userId: String, otherUserId: String) throws {
        update(userId: userId) {
            var dismissed = $0
            dismissed.changedPublicKeys[otherUserId] = nil
            return dismissed
        }
    }

    public func updateItemMemory(userId: String, itemId: String, change: (PasswordsItemMemory) -> PasswordsItemMemory) throws {
        update(userId: userId) {
            var changed = $0
            changed.itemMemories[itemId] = change($0.itemMemories[itemId] ?? PasswordsItemMemory())
            return changed
        }
    }

    public func removeAll() throws {
        lock.lock()
        defer { lock.unlock() }
        pinsByUserId = [:]
    }
}

// MARK: In a file

/// PasswordsFilePinStore keeps the keys in one JSON file. It is what a cache uses when it is
/// given no other store (the tests, where there is no keychain); the app and the AutoFill
/// extension use the keychain's (`PasswordsKeychainPinStore`). A file that exists but cannot be
/// read is never written over: every call throws `unreadable` until it is removed.
public struct PasswordsFilePinStore: PasswordsPinStore {
    public let fileURL: URL

    public init(fileURL: URL) {
        self.fileURL = fileURL
    }

    private func read(userId: String) throws -> PasswordsPins {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return PasswordsPins(userId: userId) }
        guard let data = try? Data(contentsOf: fileURL), let pins = try? JSONDecoder().decode(PasswordsPins.self, from: data),
            pins.formatVersion <= PasswordsPins.currentFormatVersion
        else { throw PasswordsPinStoreError.unreadable }
        // Another person's keys are not this person's.
        return pins.userId == userId ? pins : PasswordsPins(userId: userId)
    }

    private func write(_ pins: PasswordsPins) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        try FileManager.default.createDirectory(at: fileURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        try encoder.encode(pins).write(to: fileURL, options: [.atomic])
    }

    public func loadPins(userId: String) throws -> PasswordsPins {
        try read(userId: userId)
    }

    public func recordKnownIfAbsent(userId: String, otherUserId: String, publicKey: String) throws {
        let pins = try read(userId: userId)
        guard pins.knownPublicKeys[otherUserId] == nil else { return }
        try write(pins.recordingKnownIfAbsent(otherUserId: otherUserId, publicKey: publicKey))
    }

    public func recordChangedKey(userId: String, otherUserId: String, publicKey: String) throws {
        try write(try read(userId: userId).recordingChangedKey(otherUserId: otherUserId, publicKey: publicKey))
    }

    public func recordCompared(userId: String, otherUserId: String, publicKey: String) throws {
        try write(try read(userId: userId).recordingCompared(otherUserId: otherUserId, publicKey: publicKey))
    }

    public func dismissKeyChange(userId: String, otherUserId: String) throws {
        var pins = try read(userId: userId)
        pins.changedPublicKeys[otherUserId] = nil
        try write(pins)
    }

    public func updateItemMemory(userId: String, itemId: String, change: (PasswordsItemMemory) -> PasswordsItemMemory) throws {
        var pins = try read(userId: userId)
        let current = pins.itemMemories[itemId] ?? PasswordsItemMemory()
        let changed = change(current)
        guard changed != current else { return }
        pins.itemMemories[itemId] = changed
        try write(pins)
    }

    public func removeAll() throws {
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return }
        try FileManager.default.removeItem(at: fileURL)
    }
}

// MARK: In the keychain

#if canImport(Security)
    import Security

    /// PasswordsKeychainPinStore keeps each pinned key as a keychain item in the keychain group of
    /// Passwords (the group `PasswordsKeychainKeyStore` uses), which only the app and the AutoFill
    /// extension are entitled to. The items are not synchronized, are never in a backup, and can be
    /// read once the phone has been unlocked after starting. The account of an item says what the key
    /// is for (`known`, `compared` or `changed`, the signed-in person, the other person); its data is the
    /// public key text. The service names the server and the person's sign-in name (`signInAccountKey`) and
    /// each account carries the signed-in person's user id, so the pins of one person on one server are
    /// never read as another's. They are removed only when the person removes the account from the phone
    /// (`removeAll`), never because the server ended the sign-in. An item that does not read as a public key
    /// is skipped, so one damaged item never makes the others disappear. The first version kept `seen` and `confirmed` keys; `loadPins` moves
    /// them into `known` (and `compared` for a confirmed one) once and deletes them.
    public struct PasswordsKeychainPinStore: PasswordsPinStore {
        static let servicePrefix = "com.teanode.app.passwords.pins."
        private static let knownKind = "known"
        private static let comparedKind = "compared"
        private static let changedKind = "changed"
        private static let legacySeenKind = "seen"
        private static let legacyConfirmedKind = "confirmed"
        private static let itemKind = "item"

        public let signInAccountKey: String
        /// The keychain group to use; nil is the app's first group.
        public let accessGroup: String?

        public init(signInAccountKey: String, accessGroup: String? = nil) {
            self.signInAccountKey = signInAccountKey
            self.accessGroup = accessGroup
        }

        /// The store with the group the running app or extension names in its Info.plist
        /// (`PasswordsKeychainKeyStore.accessGroupInfoKey`).
        public static func forThisBundle(signInAccountKey: String, bundle: Bundle = .main) -> PasswordsKeychainPinStore {
            let named = bundle.object(forInfoDictionaryKey: PasswordsKeychainKeyStore.accessGroupInfoKey) as? String
            return PasswordsKeychainPinStore(signInAccountKey: signInAccountKey, accessGroup: named?.isEmpty == false ? named : nil)
        }

        private var service: String { Self.servicePrefix + signInAccountKey }

        private func query() -> [String: Any] {
            var query: [String: Any] = [
                kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service,
                kSecAttrSynchronizable as String: false,
            ]
            if let accessGroup { query[kSecAttrAccessGroup as String] = accessGroup }
            return query
        }

        private static func account(kind: String, userId: String, ownerUserId: String) -> String {
            "\(kind)|\(userId)|\(ownerUserId)"
        }

        private func accountQuery(kind: String, userId: String, ownerUserId: String) -> [String: Any] {
            var accountQuery = query()
            accountQuery[kSecAttrAccount as String] = Self.account(kind: kind, userId: userId, ownerUserId: ownerUserId)
            return accountQuery
        }

        public func loadPins(userId: String) throws -> PasswordsPins {
            var listing = query()
            listing[kSecReturnAttributes as String] = true
            listing[kSecReturnData as String] = true
            listing[kSecMatchLimit as String] = kSecMatchLimitAll
            var items: CFTypeRef?
            let status = SecItemCopyMatching(listing as CFDictionary, &items)
            if status == errSecItemNotFound { return PasswordsPins(userId: userId) }
            guard status == errSecSuccess, let found = items as? [[String: Any]] else { throw PasswordsPinStoreError.failed(status) }
            var pins = PasswordsPins(userId: userId)
            var legacySeenKeys: [String: String] = [:]
            var legacyConfirmedKeys: [String: String] = [:]
            let prefixes = [Self.knownKind, Self.comparedKind, Self.changedKind, Self.legacySeenKind, Self.legacyConfirmedKind].map {
                ($0, "\($0)|\(userId)|")
            }
            let itemPrefix = "\(Self.itemKind)|\(userId)|"
            for attributes in found {
                guard let account = attributes[kSecAttrAccount as String] as? String,
                    let data = attributes[kSecValueData as String] as? Data
                else { continue }
                if account.hasPrefix(itemPrefix) {
                    if let memory = try? JSONDecoder().decode(PasswordsItemMemory.self, from: data) {
                        pins.itemMemories[String(account.dropFirst(itemPrefix.count))] = memory
                    }
                    continue
                }
                guard let text = String(data: data, encoding: .utf8), let keyBytes = Data(base64Encoded: text),
                    PasswordsCrypto.isPublicKeyValid(keyBytes),
                    let match = prefixes.first(where: { account.hasPrefix($0.1) })
                else { continue }
                let otherUserId = String(account.dropFirst(match.1.count))
                switch match.0 {
                case Self.knownKind: pins.knownPublicKeys[otherUserId] = text
                case Self.comparedKind: pins.comparedPublicKeys[otherUserId] = text
                case Self.changedKind: pins.changedPublicKeys[otherUserId] = text
                case Self.legacySeenKind: legacySeenKeys[otherUserId] = text
                default: legacyConfirmedKeys[otherUserId] = text
                }
            }
            migrateLegacyKeys(userId: userId, seenKeys: legacySeenKeys, confirmedKeys: legacyConfirmedKeys, into: &pins)
            return pins
        }

        /// Moves the first version's keys into the one known key of each person: a confirmed key
        /// is also the compared one. A key is added before the old item is deleted, and one that
        /// could not be added keeps its old item for the next read. Best effort, as the keys are
        /// all in `pins` for this read either way.
        private func migrateLegacyKeys(
            userId: String, seenKeys: [String: String], confirmedKeys: [String: String], into pins: inout PasswordsPins
        ) {
            for (otherUserId, publicKey) in confirmedKeys {
                pins.knownPublicKeys[otherUserId] = publicKey
                pins.comparedPublicKeys[otherUserId] = publicKey
                let isKnownKept = (try? put(
                    kind: Self.knownKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: true)) != nil
                let isComparedKept = (try? put(
                    kind: Self.comparedKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: true)) != nil
                if isKnownKept && isComparedKept {
                    SecItemDelete(accountQuery(kind: Self.legacyConfirmedKind, userId: userId, ownerUserId: otherUserId) as CFDictionary)
                }
            }
            for (otherUserId, publicKey) in seenKeys {
                // A confirmed key, when there is one, was recorded as the seen key too and stands for the person.
                if pins.knownPublicKeys[otherUserId] == nil { pins.knownPublicKeys[otherUserId] = publicKey }
                let isKnownKept = (try? put(
                    kind: Self.knownKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: false)) != nil
                if isKnownKept {
                    SecItemDelete(accountQuery(kind: Self.legacySeenKind, userId: userId, ownerUserId: otherUserId) as CFDictionary)
                }
            }
        }

        /// Adds the item, or replaces its data when `isReplacing` and it is already there.
        private func put(kind: String, userId: String, ownerUserId: String, publicKey: String, isReplacing: Bool) throws {
            try put(kind: kind, userId: userId, ownerUserId: ownerUserId, data: Data(publicKey.utf8), isReplacing: isReplacing)
        }

        private func put(kind: String, userId: String, ownerUserId: String, data: Data, isReplacing: Bool) throws {
            let existing = accountQuery(kind: kind, userId: userId, ownerUserId: ownerUserId)
            if isReplacing {
                let status = SecItemUpdate(existing as CFDictionary, [kSecValueData as String: data] as CFDictionary)
                if status == errSecSuccess { return }
                guard status == errSecItemNotFound else { throw PasswordsPinStoreError.failed(status) }
            }
            var adding = existing
            adding[kSecValueData as String] = data
            adding[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            let status = SecItemAdd(adding as CFDictionary, nil)
            // Somebody else added it in between: for a replacement, update what is there now.
            if status == errSecDuplicateItem {
                guard isReplacing else { return }
                let retried = SecItemUpdate(existing as CFDictionary, [kSecValueData as String: data] as CFDictionary)
                guard retried == errSecSuccess else { throw PasswordsPinStoreError.failed(retried) }
                return
            }
            guard status == errSecSuccess else { throw PasswordsPinStoreError.failed(status) }
        }

        public func recordKnownIfAbsent(userId: String, otherUserId: String, publicKey: String) throws {
            let pins = try loadPins(userId: userId)
            guard pins.knownPublicKeys[otherUserId] == nil else { return }
            try put(kind: Self.knownKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: false)
        }

        public func recordChangedKey(userId: String, otherUserId: String, publicKey: String) throws {
            let pins = try loadPins(userId: userId)
            guard let previous = pins.knownPublicKeys[otherUserId] else {
                try put(kind: Self.knownKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: false)
                return
            }
            guard previous != publicKey else { return }
            // The notice is kept before the key moves, so a failure in between never accepts a key silently.
            try put(kind: Self.changedKind, userId: userId, ownerUserId: otherUserId, publicKey: previous, isReplacing: true)
            try put(kind: Self.knownKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: true)
        }

        public func recordCompared(userId: String, otherUserId: String, publicKey: String) throws {
            try put(kind: Self.knownKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: false)
            try put(kind: Self.comparedKind, userId: userId, ownerUserId: otherUserId, publicKey: publicKey, isReplacing: true)
            try dismissKeyChange(userId: userId, otherUserId: otherUserId)
        }

        public func dismissKeyChange(userId: String, otherUserId: String) throws {
            let status = SecItemDelete(accountQuery(kind: Self.changedKind, userId: userId, ownerUserId: otherUserId) as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw PasswordsPinStoreError.failed(status) }
        }

        /// The memory kept for one item, read from its own keychain item.
        private func memory(userId: String, itemId: String) throws -> PasswordsItemMemory {
            var reading = accountQuery(kind: Self.itemKind, userId: userId, ownerUserId: itemId)
            reading[kSecReturnData as String] = true
            var found: CFTypeRef?
            let status = SecItemCopyMatching(reading as CFDictionary, &found)
            if status == errSecItemNotFound { return PasswordsItemMemory() }
            guard status == errSecSuccess else { throw PasswordsPinStoreError.failed(status) }
            guard let data = found as? Data, let memory = try? JSONDecoder().decode(PasswordsItemMemory.self, from: data) else {
                throw PasswordsPinStoreError.unreadable
            }
            return memory
        }

        private func write(_ memory: PasswordsItemMemory, userId: String, itemId: String) throws {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            try put(kind: Self.itemKind, userId: userId, ownerUserId: itemId, data: try encoder.encode(memory), isReplacing: true)
        }

        // The memory of one item is one keychain item, changed by reading that item and writing it
        // back. The app and the AutoFill extension can both do that at the same moment; the worst
        // that comes of it is a version, a mac or a list of people sealed for that is one change
        // behind, which only ever makes a later check stricter, never looser.
        public func updateItemMemory(userId: String, itemId: String, change: (PasswordsItemMemory) -> PasswordsItemMemory) throws {
            let current = try memory(userId: userId, itemId: itemId)
            let changed = change(current)
            guard changed != current else { return }
            try write(changed, userId: userId, itemId: itemId)
        }

        public func removeAll() throws {
            let status = SecItemDelete(query() as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw PasswordsPinStoreError.failed(status) }
        }
    }

    extension PasswordsCache {
        /// The cache at a file, with the pinned keys in the keychain group of Passwords (the one the
        /// running bundle names), where the share extension and the widgets cannot read or write
        /// them. The app and the AutoFill extension open their cache this way.
        public static func forThisBundle(fileURL: URL, signInAccountKey: String, bundle: Bundle = .main) -> PasswordsCache {
            PasswordsCache(
                fileURL: fileURL, pinStore: PasswordsKeychainPinStore.forThisBundle(signInAccountKey: signInAccountKey, bundle: bundle))
        }
    }
#endif
