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
    /// The person cancelled Face ID, or it did not recognize them.
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

    /// PasswordsKeychainKeyStore keeps the account key in the keychain the app
    /// shares with its extensions (the first group in its entitlements, which
    /// is where an item goes when no group is named), behind biometry:
    /// `.biometryCurrentSet`, so enrolling another face or finger makes the
    /// system drop it, and `WhenPasscodeSetThisDeviceOnly`, so it is never in
    /// a backup and goes if the passcode is removed. It is never synchronizable.
    public struct PasswordsKeychainKeyStore: PasswordsKeyStore {
        static let service = "com.teanode.app.passwords"

        /// The keychain group to use; nil is the app's first group.
        public let accessGroup: String?

        public init(accessGroup: String? = nil) {
            self.accessGroup = accessGroup
        }

        /// Whether this phone has Face ID or Touch ID set up, which keeping a key needs.
        public static var isBiometryAvailable: Bool {
            var problem: NSError?
            return LAContext().canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &problem)
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

        public func save(accountKey: Data, signInAccountKey: String) async throws {
            var creationError: Unmanaged<CFError>?
            guard
                let access = SecAccessControlCreateWithFlags(
                    nil, kSecAttrAccessibleWhenPasscodeSetThisDeviceOnly, .biometryCurrentSet, &creationError)
            else { throw PasswordsKeyStoreError.unavailable }
            let existing = Self.query(signInAccountKey: signInAccountKey, accessGroup: accessGroup)
            SecItemDelete(existing as CFDictionary)
            var adding = existing
            adding[kSecValueData as String] = accountKey
            adding[kSecAttrAccessControl as String] = access
            let status = SecItemAdd(adding as CFDictionary, nil)
            guard status == errSecSuccess else {
                // No passcode, or no biometry enrolled, comes back as a parameter or
                // authentication error rather than a code of its own.
                if status == errSecParam || status == errSecNotAvailable { throw PasswordsKeyStoreError.unavailable }
                throw PasswordsKeyStoreError.failed(status)
            }
        }

        public func load(signInAccountKey: String, promptReason: String) async throws -> Data? {
            let accessGroup = self.accessGroup
            return try await withCheckedThrowingContinuation { continuation in
                // Reading waits for Face ID, so it is done off the caller's thread.
                DispatchQueue.global(qos: .userInitiated).async {
                    let context = LAContext()
                    context.localizedReason = promptReason
                    var reading = Self.query(signInAccountKey: signInAccountKey, accessGroup: accessGroup)
                    reading[kSecReturnData as String] = true
                    reading[kSecMatchLimit as String] = kSecMatchLimitOne
                    reading[kSecUseAuthenticationContext as String] = context
                    var item: CFTypeRef?
                    let status = SecItemCopyMatching(reading as CFDictionary, &item)
                    if status == errSecItemNotFound {
                        continuation.resume(returning: nil)
                    } else if status == errSecUserCanceled || status == errSecAuthFailed {
                        continuation.resume(throwing: PasswordsKeyStoreError.canceled)
                    } else if status == errSecSuccess, let data = item as? Data {
                        continuation.resume(returning: data)
                    } else {
                        continuation.resume(throwing: PasswordsKeyStoreError.failed(status))
                    }
                }
            }
        }

        public func delete(signInAccountKey: String) async throws {
            let status = SecItemDelete(Self.query(signInAccountKey: signInAccountKey, accessGroup: accessGroup) as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else { throw PasswordsKeyStoreError.failed(status) }
        }
    }
#endif
