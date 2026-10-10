#if canImport(Security)
    import Foundation
    import Security

    /// Keychain keeps the app's secrets on this phone: the sign-in, and the
    /// key notifications are encrypted with. They go in the keychain group the
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

    /// KeychainAccountStore keeps the sign-in in the keychain.
    public struct KeychainAccountStore: AccountStore {
        static let name = "account"

        public init() {}

        public func save(_ account: Account) throws {
            try Keychain.write(JSONEncoder().encode(account), as: Self.name)
        }

        public func load() throws -> Account? {
            try Keychain.read(Self.name).map { try JSONDecoder().decode(Account.self, from: $0) }
        }

        public func delete() throws {
            try Keychain.delete(Self.name)
        }
    }

    #if canImport(CryptoKit)
        extension PayloadKey {
            static let name = "payloadKey"

            /// The phone's key, made the first time it is asked for. It is
            /// kept for as long as the app is installed: a new one would leave
            /// every notification already on its way unreadable.
            public static func current() throws -> String {
                if let saved = try stored() {
                    return saved
                }
                let made = make()
                try Keychain.write(Data(made.utf8), as: name)
                return made
            }

            /// The phone's key if one was made, for the notification
            /// extension, which never makes one.
            public static func stored() throws -> String? {
                try Keychain.read(name).map { String(decoding: $0, as: UTF8.self) }
            }

            /// Forgets the key, when the phone signs out.
            public static func forget() throws {
                try Keychain.delete(name)
            }
        }
    #endif
#endif
