import Foundation

/// ConversationDraft identifies drafts by both account and conversation.
///
/// A key is "draft.account.", the account, a dot, and the conversation,
/// each encoded so that no dot can appear inside one: every draft of an
/// account starts with the same prefix, and signing out of the account
/// removes them all. The main conversation is "main"; a named one is "c."
/// and its id, so the two can never be taken for each other.
public enum ConversationDraft {
    public static func storageKey(accountKey: String, conversationId: String?) -> String {
        let conversationPart = conversationId.map { conversationId in "c.\(encoded(conversationId))" } ?? "main"
        return accountPrefix(accountKey: accountKey) + conversationPart
    }

    /// Removes every draft of an account, for when the app signs out of it.
    public static func forget(accountKey: String, in defaults: UserDefaults = .standard) {
        let prefix = accountPrefix(accountKey: accountKey)
        for key in defaults.dictionaryRepresentation().keys where key.hasPrefix(prefix) {
            defaults.removeObject(forKey: key)
        }
    }

    static func accountPrefix(accountKey: String) -> String {
        "draft.account.\(encoded(accountKey))."
    }

    /// Base64 with no dots in it, so a part cannot run into the next.
    private static func encoded(_ text: String) -> String {
        Data(text.utf8).base64EncodedString()
    }
}
