import Foundation

// Where the opened account key waits between launches, so the person opens
// Passwords with Face ID instead of typing the passphrase. The account key is
// the one secret that opens everything, so it is kept only on this phone: it
// is never synced or backed up, and it needs the phone's passcode to be set and
// the face or finger that was enrolled when it was kept. A keychain that cannot
// do that (a Simulator without biometry, an unprotected phone) refuses, and the
// person types the passphrase each time instead.

/// PasswordsKeyStoreError is why a key could not be kept or read.
public enum PasswordsKeyStoreError: Error, Equatable, Sendable {
    /// The person cancelled the Face ID prompt (and nothing else: a key the
    /// system dropped, or Face ID that failed, is `failed`, and the person
    /// types the passphrase instead).
    case canceled
    /// This phone cannot keep a key behind biometry: no passcode, or no
    /// enrolled face or finger.
    case unavailable
    /// The keychain refused, with its status code.
    case failed(Int32)
}

/// PasswordsKeyStore keeps the account key, one for each signed-in account.
public protocol PasswordsKeyStore: Sendable {
    /// Keeps the account key, replacing one kept before. `signInAccountKey` is
    /// the id of the signed-in account (`Account.accountKey`), because a
    /// phone can be signed in to several servers.
    func save(accountKey: Data, signInAccountKey: String) async throws
    /// Reads the account key. `promptReason` is the sentence Face ID shows.
    /// Nil when none is kept, or when the phone's biometry changed since (the
    /// system then drops the key and the person types the passphrase).
    func load(signInAccountKey: String, promptReason: String) async throws -> Data?
    func delete(signInAccountKey: String) async throws
}

/// PasswordsMemoryKeyStore keeps keys in memory: for tests, and for where
/// there is no keychain.
public actor PasswordsMemoryKeyStore: PasswordsKeyStore {
    private var keys: [String: Data] = [:]
    /// The last reason `load` was given, for a test to check.
    public private(set) var lastPromptReason: String?

    public init() {}

    public func save(accountKey: Data, signInAccountKey: String) async throws {
        keys[signInAccountKey] = accountKey
    }

    public func load(signInAccountKey: String, promptReason: String) async throws -> Data? {
        lastPromptReason = promptReason
        return keys[signInAccountKey]
    }

    public func delete(signInAccountKey: String) async throws {
        keys[signInAccountKey] = nil
    }
}

#if canImport(Security) && canImport(LocalAuthentication)
    import LocalAuthentication
    import Security

    /// PasswordsKeychainKeyStore keeps the account key in the keychain behind
    /// biometry: `.biometryCurrentSet`, so enrolling another face or finger
    /// makes the system drop it, and `WhenPasscodeSetThisDeviceOnly`, so it is
    /// never in a backup and goes if the passcode is removed. It is never
    /// synchronizable.
    ///
    /// The key lives in a keychain group of its own (`accessGroup`), which only
    /// the app and the AutoFill extension are entitled to, so the other
    /// extensions that share the sign-in's group cannot ask for it. Before that
    /// group existed the key was kept in the app's first group (the one it
    /// shares with its extensions); `load` moves such a key into `accessGroup`
    /// once, and `delete` removes it from both.
    public struct PasswordsKeychainKeyStore: PasswordsKeyStore {
        static let service = "com.teanode.app.passwords"

        /// The Info.plist key that names the group: the app's and the
        /// extension's both carry it, expanded from the build setting
        /// `TEANODE_PASSWORDS_KEYCHAIN_GROUP` with the team's prefix.
        public static let accessGroupInfoKey = "TeaNodePasswordsKeychainGroup"

        /// The keychain group to use; nil is the app's first group.
        public let accessGroup: String?

        public init(accessGroup: String? = nil) {
            self.accessGroup = accessGroup
        }

        /// The store with the group the running app or extension names in its
        /// Info.plist. A build that names none keeps the key in the app's first group.
        public static func forThisBundle(_ bundle: Bundle = .main) -> PasswordsKeychainKeyStore {
            let named = bundle.object(forInfoDictionaryKey: accessGroupInfoKey) as? String
            return PasswordsKeychainKeyStore(accessGroup: named?.isEmpty == false ? named : nil)
        }

        /// Whether this phone has Face ID or Touch ID set up, which keeping a key needs.
        public static var isBiometryAvailable: Bool {
            var problem: NSError?
            return LAContext().canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &problem)
        }

        /// Asks the person for Face ID (or the passcode when Face ID will not
        /// do) for one thing that must be them, such as erasing Passwords.
        /// False when they cancel or it fails.
        public static func confirmDeviceOwner(reason: String) async -> Bool {
            let context = LAContext()
            do {
                return try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
            } catch {
                return false
            }
        }

        private static func query(signInAccountKey: String, accessGroup: String?) -> [String: Any] {
            var query: [String: Any] = [
                kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service,
                kSecAttrAccount as String: signInAccountKey,
                kSecAttrSynchronizable as String: false,
            ]
            if let accessGroup { query[kSecAttrAccessGroup as String] = accessGroup }
            return query
        }

        private static func accessControl() throws -> SecAccessControl {
            var creationError: Unmanaged<CFError>?
            guard
                let access = SecAccessControlCreateWithFlags(
                    nil, kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly, .biometryCurrentSet, &creationError)
            else { throw PasswordsKeyStoreError.unavailable }
            return access
        }

        /// Adds the key in `accessGroup`, replacing one kept there.
        private static func add(accountKey: Data, signInAccountKey: String, accessGroup: String?) throws {
            let access = try accessControl()
            let existing = query(signInAccountKey: signInAccountKey, accessGroup: accessGroup)
            SecItemDelete(existing as CFDictionary)
            var adding = existing
            adding[kSecValueData as String] = accountKey
            adding[kSecAttrAccessControl as String] = access
            let status = SecItemAdd(adding as CFDictionary, nil)
            guard status == errSecSuccess else {
                // No passcode, or no biometry enrolled, comes back as a parameter or
                // authentication error rather than a code of its own. A group the
                // build is not entitled to (an unsigned Simulator build) is refused the same way.
                if status == errSecParam || status == errSecNotAvailable || status == errSecMissingEntitlement {
                    throw PasswordsKeyStoreError.unavailable
                }
                throw PasswordsKeyStoreError.failed(status)
            }
        }

        public func save(accountKey: Data, signInAccountKey: String) async throws {
            try Self.add(accountKey: accountKey, signInAccountKey: signInAccountKey, accessGroup: accessGroup)
            // A key kept before the group existed is stale now.
            if accessGroup != nil { Self.deleteFirstGroupCopy(signInAccountKey: signInAccountKey, besidesGroup: accessGroup) }
        }

        public func load(signInAccountKey: String, promptReason: String) async throws -> Data? {
            let accessGroup = self.accessGroup
            return try await withCheckedThrowingContinuation { continuation in
                // Reading waits for Face ID, so it is done off the caller's thread.
                DispatchQueue.global(qos: .userInitiated).async {
                    do {
                        let context = LAContext()
                        context.localizedReason = promptReason
                        if let found = try Self.read(signInAccountKey: signInAccountKey, accessGroup: accessGroup, context: context) {
                            continuation.resume(returning: found.data)
                        } else if accessGroup != nil,
                            let moved = try Self.moveFromFirstGroup(
                                signInAccountKey: signInAccountKey, toGroup: accessGroup, context: context)
                        {
                            continuation.resume(returning: moved)
                        } else {
                            continuation.resume(returning: nil)
                        }
                    } catch {
                        continuation.resume(throwing: error)
                    }
                }
            }
        }

        /// The kept key and the group it is in; nil when there is none there.
        /// `accessGroup` nil searches every group the process is entitled to.
        private static func read(signInAccountKey: String, accessGroup: String?, context: LAContext) throws -> (
            data: Data, group: String?
        )? {
            var reading = query(signInAccountKey: signInAccountKey, accessGroup: accessGroup)
            reading[kSecReturnData as String] = true
            reading[kSecReturnAttributes as String] = true
            reading[kSecMatchLimit as String] = kSecMatchLimitOne
            reading[kSecUseAuthenticationContext as String] = context
            var item: CFTypeRef?
            let status = SecItemCopyMatching(reading as CFDictionary, &item)
            if status == errSecItemNotFound { return nil }
            // Only the person's own cancel is a cancel. Face ID that did not
            // recognize them, or a key the system has invalidated, is a failure:
            // the caller says to type the passphrase and forgets the key.
            if status == errSecUserCanceled { throw PasswordsKeyStoreError.canceled }
            guard status == errSecSuccess, let attributes = item as? [String: Any], let data = attributes[kSecValueData as String] as? Data
            else { throw PasswordsKeyStoreError.failed(status) }
            return (data, attributes[kSecAttrAccessGroup as String] as? String)
        }

        /// Moves a key kept in another group (the app's first one, before the key
        /// had a group of its own) into `toGroup`, then removes the old copy.
        /// Nil when there is no such key. The new copy is made before the old is
        /// deleted, so a failure in between loses nothing.
        private static func moveFromFirstGroup(signInAccountKey: String, toGroup: String?, context: LAContext) throws -> Data? {
            guard let old = try read(signInAccountKey: signInAccountKey, accessGroup: nil, context: context), old.group != toGroup else {
                return nil
            }
            try add(accountKey: old.data, signInAccountKey: signInAccountKey, accessGroup: toGroup)
            if let oldGroup = old.group {
                SecItemDelete(query(signInAccountKey: signInAccountKey, accessGroup: oldGroup) as CFDictionary)
            }
            return old.data
        }

        /// Deletes the copy in the app's other groups, leaving the one in `besidesGroup`.
        private static func deleteFirstGroupCopy(signInAccountKey: String, besidesGroup: String?) {
            var listing = query(signInAccountKey: signInAccountKey, accessGroup: nil)
            listing[kSecReturnAttributes as String] = true
            listing[kSecMatchLimit as String] = kSecMatchLimitAll
            // Listing attributes never asks for Face ID: only the data is behind it.
            var items: CFTypeRef?
            guard SecItemCopyMatching(listing as CFDictionary, &items) == errSecSuccess, let found = items as? [[String: Any]] else { return }
            for attributes in found {
                guard let group = attributes[kSecAttrAccessGroup as String] as? String, group != besidesGroup else { continue }
                SecItemDelete(query(signInAccountKey: signInAccountKey, accessGroup: group) as CFDictionary)
            }
        }

        public func delete(signInAccountKey: String) async throws {
            let status = SecItemDelete(Self.query(signInAccountKey: signInAccountKey, accessGroup: accessGroup) as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw PasswordsKeyStoreError.failed(status) }
            if accessGroup != nil { Self.deleteFirstGroupCopy(signInAccountKey: signInAccountKey, besidesGroup: nil) }
        }
    }
#endif
