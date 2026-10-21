import Foundation

// Where a device keeps the public keys it holds for other people: the key each
// owner of a shared item had when their first item opened ("seen"), and the key
// the person confirmed by comparing safety codes ("confirmed"). They decide
// which items open and for whom this device seals an item key, so whoever can
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
    /// Records a seen key for an owner, only when none is recorded for that owner. A key
    /// that is already there (seen or not) is left alone.
    func recordSeenIfAbsent(userId: String, ownerUserId: String, publicKey: String) throws
    /// Records a key the person confirmed by comparing safety codes. It replaces the seen key
    /// of that person too, so their items are checked against it.
    func recordConfirmed(userId: String, ownerUserId: String, publicKey: String) throws
    /// Forgets every pinned key of the sign-in.
    func removeAll() throws
}

// MARK: In memory

/// PasswordsMemoryPinStore keeps the keys in memory: for tests, for the demo, and
/// to stand in for the keychain when two caches must share one store.
public final class PasswordsMemoryPinStore: PasswordsPinStore, @unchecked Sendable {
    private let lock = NSLock()
    private var seen: [String: [String: String]] = [:]
    private var confirmed: [String: [String: String]] = [:]

    public init() {}

    public func loadPins(userId: String) throws -> PasswordsPins {
        lock.lock()
        defer { lock.unlock() }
        return PasswordsPins(userId: userId, seenPublicKeys: seen[userId] ?? [:], confirmedPublicKeys: confirmed[userId] ?? [:])
    }

    public func recordSeenIfAbsent(userId: String, ownerUserId: String, publicKey: String) throws {
        lock.lock()
        defer { lock.unlock() }
        if confirmed[userId]?[ownerUserId] != nil || seen[userId]?[ownerUserId] != nil { return }
        seen[userId, default: [:]][ownerUserId] = publicKey
    }

    public func recordConfirmed(userId: String, ownerUserId: String, publicKey: String) throws {
        lock.lock()
        defer { lock.unlock() }
        confirmed[userId, default: [:]][ownerUserId] = publicKey
        seen[userId, default: [:]][ownerUserId] = publicKey
    }

    public func removeAll() throws {
        lock.lock()
        defer { lock.unlock() }
        seen = [:]
        confirmed = [:]
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

    public func recordSeenIfAbsent(userId: String, ownerUserId: String, publicKey: String) throws {
        var pins = try read(userId: userId)
        guard pins.ownerPublicKey(forUserId: ownerUserId) == nil else { return }
        pins.seenPublicKeys[ownerUserId] = publicKey
        try write(pins)
    }

    public func recordConfirmed(userId: String, ownerUserId: String, publicKey: String) throws {
        var pins = try read(userId: userId)
        pins.confirmedPublicKeys[ownerUserId] = publicKey
        pins.seenPublicKeys[ownerUserId] = publicKey
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
    /// is for (`seen` or `confirmed`, the signed-in person, the owner); its data is the public key text.
    /// An item that does not read as a public key is skipped, so one damaged item never makes the
    /// others disappear.
    public struct PasswordsKeychainPinStore: PasswordsPinStore {
        static let servicePrefix = "com.teanode.app.passwords.pins."
        private static let seenKind = "seen"
        private static let confirmedKind = "confirmed"

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
            let seenPrefix = "\(Self.seenKind)|\(userId)|"
            let confirmedPrefix = "\(Self.confirmedKind)|\(userId)|"
            for attributes in found {
                guard let account = attributes[kSecAttrAccount as String] as? String,
                    let data = attributes[kSecValueData as String] as? Data, let text = String(data: data, encoding: .utf8),
                    let keyBytes = Data(base64Encoded: text), PasswordsCrypto.isPublicKeyValid(keyBytes)
                else { continue }
                if account.hasPrefix(seenPrefix) {
                    pins.seenPublicKeys[String(account.dropFirst(seenPrefix.count))] = text
                } else if account.hasPrefix(confirmedPrefix) {
                    pins.confirmedPublicKeys[String(account.dropFirst(confirmedPrefix.count))] = text
                }
            }
            return pins
        }

        /// Adds the item, or replaces its data when `isReplacing` and it is already there.
        private func put(kind: String, userId: String, ownerUserId: String, publicKey: String, isReplacing: Bool) throws {
            let data = Data(publicKey.utf8)
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

        public func recordSeenIfAbsent(userId: String, ownerUserId: String, publicKey: String) throws {
            // A confirmed key already stands for this person: nothing is learned from a first sight.
            let pins = try loadPins(userId: userId)
            guard pins.ownerPublicKey(forUserId: ownerUserId) == nil else { return }
            try put(kind: Self.seenKind, userId: userId, ownerUserId: ownerUserId, publicKey: publicKey, isReplacing: false)
        }

        public func recordConfirmed(userId: String, ownerUserId: String, publicKey: String) throws {
            try put(kind: Self.confirmedKind, userId: userId, ownerUserId: ownerUserId, publicKey: publicKey, isReplacing: true)
            try put(kind: Self.seenKind, userId: userId, ownerUserId: ownerUserId, publicKey: publicKey, isReplacing: true)
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
