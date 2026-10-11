#if canImport(Security)
    import Foundation
    import Security

    /// Keychain keeps the app's secrets on this phone: the sign-ins, and the
    /// keys notifications are encrypted with. They go in the keychain group the
    /// app shares with its extensions, the first in its keychain-access-groups,
    /// which is where an item goes when no group is named. They can be read
    /// once the phone has been unlocked after starting, so a notification that
    /// arrives on a locked phone can still be decrypted, and they are never
    /// copied to another phone.
    public enum Keychain {
        static let service = "com.teanode.app.account"

        static func query(_ name: String) -> [String: Any] {
            [
                kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service,
                kSecAttrAccount as String: name,
            ]
        }

        public static func read(_ name: String) throws -> Data? {
            var reading = query(name)
            reading[kSecReturnData as String] = true
            reading[kSecMatchLimit as String] = kSecMatchLimitOne
            var item: CFTypeRef?
            let status = SecItemCopyMatching(reading as CFDictionary, &item)
            if status == errSecItemNotFound {
                return nil
            }
            guard status == errSecSuccess, let data = item as? Data else {
                throw KeychainError(status: status)
            }
            return data
        }

        public static func write(_ data: Data, as name: String) throws {
            let status = SecItemUpdate(query(name) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
            if status == errSecItemNotFound {
                var adding = query(name)
                adding[kSecValueData as String] = data
                adding[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
                let added = SecItemAdd(adding as CFDictionary, nil)
                guard added == errSecSuccess else { throw KeychainError(status: added) }
            } else if status != errSecSuccess {
                throw KeychainError(status: status)
            }
        }

        public static func delete(_ name: String) throws {
            let status = SecItemDelete(query(name) as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else {
                throw KeychainError(status: status)
            }
        }

        /// Moves an item written before the app shared a keychain group into
        /// the shared one: read, removed wherever it is, written again.
        public static func moveToSharedGroup(_ name: String) throws {
            guard let data = try read(name) else { return }
            try delete(name)
            try write(data, as: name)
        }
    }

    /// KeychainError is the keychain refusing, with its status code.
    public struct KeychainError: Error {
        public let status: OSStatus
    }

    /// KeychainAccountStore keeps the sign-ins in the keychain: every server
    /// the app is signed in to, and the one in use, which is what load and
    /// delete act on. A sign-in kept before there could be several is taken
    /// into the list the first time it is read.
    public struct KeychainAccountStore: AccountStore {
        static let name = "accounts"
        static let singleName = "account"

        public init() {}

        /// Keeps a sign-in, renewed or new, without changing the one in use.
        public func save(_ account: Account) throws {
            var ring = try accounts()
            ring.put(account)
            try write(ring)
        }

        public func load() throws -> Account? {
            try accounts().active
        }

        public func load(accountKey: String) throws -> Account? {
            try accounts().accounts.first { $0.accountKey == accountKey }
        }

        /// Forgets the sign-in in use; the next kept one is in use after it.
        public func delete() throws {
            var ring = try accounts()
            if let key = ring.active?.accountKey { ring.remove(key) }
            try write(ring)
        }

        /// Every sign-in, and the one in use.
        public func accounts() throws -> AccountRing {
            if let data = try Keychain.read(Self.name) {
                return try JSONDecoder().decode(AccountRing.self, from: data)
            }
            guard let data = try Keychain.read(Self.singleName) else { return AccountRing() }
            let single = try JSONDecoder().decode(Account.self, from: data)
            let ring = AccountRing(accounts: [single], activeKey: single.accountKey)
            try write(ring)
            try Keychain.delete(Self.singleName)
            return ring
        }

        /// Keeps a sign-in and puts it to use.
        public func add(_ account: Account) throws {
            var ring = try accounts()
            ring.put(account)
            ring.activate(account.accountKey)
            try write(ring)
        }

        public func activate(_ accountKey: String) throws {
            var ring = try accounts()
            ring.activate(accountKey)
            try write(ring)
        }

        public func remove(_ accountKey: String) throws {
            var ring = try accounts()
            ring.remove(accountKey)
            try write(ring)
        }

        func write(_ ring: AccountRing) throws {
            try Keychain.write(JSONEncoder().encode(ring), as: Self.name)
        }
    }

    /// KeychainSecrets is the keychain as a SecretStore.
    public struct KeychainSecrets: SecretStore {
        public init() {}

        public func read(_ name: String) throws -> Data? {
            try Keychain.read(name)
        }

        public func write(_ data: Data, as name: String) throws {
            try Keychain.write(data, as: name)
        }

        public func delete(_ name: String) throws {
            try Keychain.delete(name)
        }
    }

    #if canImport(CryptoKit)
        extension PayloadKey {
            /// Every account's key, in the keychain the app shares with its
            /// notification extension.
            public static var keyring: PayloadKeyring { PayloadKeyring(secrets: KeychainSecrets()) }

            /// The account's key, made the first time it is asked for.
            public static func current(for accountKey: String) throws -> String {
                try keyring.current(for: accountKey, make: make)
            }
        }
    #endif
#endif
