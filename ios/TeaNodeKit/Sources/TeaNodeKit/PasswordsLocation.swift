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

    /// Removes the copy of a sign-in and its pinned keys, when the phone signs out of it.
    /// Nothing happens when there is none.
    public static func removeCopy(groupContainer: URL, signInAccountKey: String) {
        let copyURL = fileURL(groupContainer: groupContainer, signInAccountKey: signInAccountKey)
        try? FileManager.default.removeItem(at: copyURL)
        try? FileManager.default.removeItem(at: PasswordsCache.pinsFileURL(cacheFileURL: copyURL))
    }
}
