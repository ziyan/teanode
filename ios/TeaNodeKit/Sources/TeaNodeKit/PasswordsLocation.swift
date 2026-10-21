import Foundation

// Where the phone keeps Passwords, so the app and the AutoFill extension agree
// without sharing code that talks to the file system. Both name the same
// folder in the app group's container and the same file in it for a sign-in.

/// PasswordsLocation names the phone's copy of Passwords.
public enum PasswordsLocation {
    /// The folder in the app group's container that holds the copies.
    public static let directoryName = "Passwords"

    /// The copy's file name for a sign-in (`Account.accountKey`). The key is
    /// percent-encoded, as Health's state file names are, so no key can name
    /// another file or a folder.
    public static func fileName(signInAccountKey: String) -> String {
        let safe = signInAccountKey.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""
        return "passwords-\(safe).json"
    }

    /// The copy's file inside the app group's container.
    public static func fileURL(groupContainer: URL, signInAccountKey: String) -> URL {
        groupContainer
            .appendingPathComponent(directoryName, isDirectory: true)
            .appendingPathComponent(fileName(signInAccountKey: signInAccountKey))
    }

    /// Removes the copy of a sign-in (its header, item files and summary), and, unless
    /// `isKeepingPins`, the pins file a cache without a keychain keeps, when the phone signs out
    /// of it. The pinned keys in the keychain are removed with `PasswordsCache.erase()` or the pin
    /// store (`PasswordsSignInEnd.isForgettingKeysAndItemMemories` says when). Nothing happens when
    /// there is none.
    public static func removeCopy(groupContainer: URL, signInAccountKey: String, isKeepingPins: Bool = false) {
        let copyURL = fileURL(groupContainer: groupContainer, signInAccountKey: signInAccountKey)
        try? FileManager.default.removeItem(at: copyURL)
        try? FileManager.default.removeItem(at: PasswordsCache.itemsDirectoryURL(cacheFileURL: copyURL))
        try? FileManager.default.removeItem(at: PasswordsCache.summaryFileURL(cacheFileURL: copyURL))
        if !isKeepingPins { try? FileManager.default.removeItem(at: PasswordsCache.pinsFileURL(cacheFileURL: copyURL)) }
    }
}

/// PasswordsSignInEnd is why a sign-in is gone from the phone. What the phone remembers about other
/// people's keys and about each item (the record versions and macs it saw, the people it sealed
/// for) is kept per server and signed-in person, and is forgotten only when the person removes the
/// account from the phone: a server that ends a sign-in (a 401 on access and refresh) must not be
/// able to make the phone forget it, and signing in again as the same person finds it as it was.
public enum PasswordsSignInEnd: Equatable, Sendable {
    /// The person removed the account from the phone (signed out, or removed it from the list).
    case removedByPerson
    /// The server ended the sign-in.
    case endedByServer

    /// Whether the pinned keys and item memories of the sign-in are removed.
    public var isForgettingKeysAndItemMemories: Bool { self == .removedByPerson }
}
