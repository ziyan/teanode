import Foundation

extension Account {
    /// Which sign-in this is: one person on one server. Signing in again as
    /// the same person replaces the sign-in rather than adding another.
    public var accountKey: String { "\(server.description)|\(username.lowercased())" }
}

/// AccountRing is every server the app is signed in to, and the one in use.
/// The app, its share sheet, its widgets and Siri all act as the one in use.
public struct AccountRing: Codable, Equatable, Sendable {
    public private(set) var accounts: [Account]
    public private(set) var activeKey: String?

    public init(accounts: [Account] = [], activeKey: String? = nil) {
        self.accounts = accounts
        self.activeKey = activeKey
    }

    /// The sign-in in use; the first when none is chosen.
    public var active: Account? {
        accounts.first { $0.accountKey == activeKey } ?? accounts.first
    }

    /// Keeps a sign-in, replacing the one for the same person on the same
    /// server; the first one kept is in use.
    public mutating func put(_ account: Account) {
        if let index = accounts.firstIndex(where: { $0.accountKey == account.accountKey }) {
            accounts[index] = account
        } else {
            accounts.append(account)
        }
        if activeKey == nil || active?.accountKey != activeKey {
            activeKey = active?.accountKey
        }
    }

    /// Puts a sign-in to use; nothing happens for one not kept.
    public mutating func activate(_ accountKey: String) {
        if accounts.contains(where: { $0.accountKey == accountKey }) {
            activeKey = accountKey
        }
    }

    /// Forgets a sign-in. When it was the one in use, the next kept one is.
    public mutating func remove(_ accountKey: String) {
        accounts.removeAll { $0.accountKey == accountKey }
        if activeKey == accountKey || active?.accountKey != activeKey {
            activeKey = accounts.first?.accountKey
        }
    }
}
