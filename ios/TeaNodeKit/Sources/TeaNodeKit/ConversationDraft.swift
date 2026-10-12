import Foundation

/// ConversationDraft identifies drafts by both account and conversation.
public enum ConversationDraft {
    public static func storageKey(accountKey: String, conversationId: String?) -> String {
        let components: [String?] = [accountKey, conversationId]
        let encoded = try! JSONEncoder().encode(components)
        return "draft.account.\(encoded.base64EncodedString())"
    }
}
