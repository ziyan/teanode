import Foundation
import Security
import TeaNodeKit

/// KeychainAccountStore keeps the sign-in in the keychain, on this phone
/// only: it is not backed up to another phone, and it is readable once the
/// phone has been unlocked after starting, so a notification can be read
/// while it is locked.
struct KeychainAccountStore: AccountStore {
    private static let service = "com.teanode.app.account"

    private var query: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.service,
            kSecAttrAccount as String: "account",
        ]
    }

    func save(_ account: Account) throws {
        let data = try JSONEncoder().encode(account)
        let status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecItemNotFound {
            var adding = query
            adding[kSecValueData as String] = data
            adding[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            let added = SecItemAdd(adding as CFDictionary, nil)
            guard added == errSecSuccess else { throw KeychainError(status: added) }
        } else if status != errSecSuccess {
            throw KeychainError(status: status)
        }
    }

    func load() throws -> Account? {
        var reading = query
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
        return try JSONDecoder().decode(Account.self, from: data)
    }

    func delete() throws {
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainError(status: status)
        }
    }
}

/// KeychainError is the keychain refusing, with its status code.
struct KeychainError: Error {
    let status: OSStatus
}
