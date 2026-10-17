import CryptoKit
import Foundation
import TeaNodeKit
import UserNotifications

// Built into both the app and its notification extension: the extension
// gives a notification its buttons, and the app does what a pressed one says.

/// NotificationCategories are the sets of buttons a notification can have.
enum NotificationCategories {
    static let approval = "approval"
    static let message = "message"
    static let chat = "chat"
    static let payment = "payment"
    static let questionPrefix = "question-"

    /// The categories known before any notification arrives. A question's
    /// choices are not known in advance, so its category is made when it
    /// arrives (category(for:)).
    static var fixed: Set<UNNotificationCategory> {
        [
            UNNotificationCategory(
                identifier: approval,
                actions: [
                    // Approving does what the agent asked to do, so the
                    // phone must be unlocked first.
                    UNNotificationAction(identifier: "approve", title: String(localized: "Do it"), options: [.authenticationRequired]),
                    UNNotificationAction(identifier: "deny", title: String(localized: "Don’t"), options: [.destructive, .authenticationRequired]),
                ],
                intentIdentifiers: []),
            UNNotificationCategory(identifier: message, actions: [replyAction, openAction], intentIdentifiers: []),
            UNNotificationCategory(identifier: chat, actions: [chatReplyAction, openAction], intentIdentifiers: []),
            // Only to open the app: an answer that runs somebody's payment is
            // given on the card, not from a lock screen.
            UNNotificationCategory(identifier: payment, actions: [openAction], intentIdentifiers: []),
        ]
    }

    static var replyAction: UNNotificationAction {
        UNTextInputNotificationAction(
            identifier: "reply", title: String(localized: "Reply"), options: [.authenticationRequired],
            textInputButtonTitle: String(localized: "Send"), textInputPlaceholder: String(localized: "Ask your agent…"))
    }

    /// Replying to a person in a chat: a message to them, not a question
    /// for the agent.
    static var chatReplyAction: UNNotificationAction {
        UNTextInputNotificationAction(
            identifier: "reply", title: String(localized: "Reply"), options: [.authenticationRequired],
            textInputButtonTitle: String(localized: "Send"), textInputPlaceholder: String(localized: "Message…"))
    }

    static var openAction: UNNotificationAction {
        UNNotificationAction(identifier: "open", title: String(localized: "Open"), options: [.foreground])
    }

    /// The category a notification is shown with, or nil for one with no
    /// buttons.
    static func category(for content: NotificationContent) -> UNNotificationCategory? {
        switch content.notificationKind {
        case "test":
            return nil
        case "approval":
            return fixed.first { $0.identifier == approval }
        case "chat":
            return fixed.first { $0.identifier == chat }
        case "payment":
            return fixed.first { $0.identifier == payment }
        case "question":
            let buttons = Array((content.buttons ?? []).prefix(4))
            let actions: [UNNotificationAction] = buttons.map { button in
                switch button.buttonAction {
                case "reply": return replyAction
                case "open": return openAction
                default:
                    return UNNotificationAction(identifier: button.buttonAction, title: button.label, options: [.authenticationRequired])
                }
            }
            // Named after its buttons, so the same choices share a category.
            let digest = SHA256.hash(data: Data(buttons.map { "\($0.buttonAction)\u{0}\($0.label)" }.joined(separator: "\u{1}").utf8))
            let identifier = questionPrefix + digest.prefix(8).map { String(format: "%02x", $0) }.joined()
            return UNNotificationCategory(identifier: identifier, actions: actions, intentIdentifiers: [])
        default:
            return fixed.first { $0.identifier == message }
        }
    }
}

/// NotificationInfo is what a shown notification carries for the app: where
/// it came from and what its buttons answer.
struct NotificationInfo: Sendable {
    /// The account whose key opened it, which is the account it came from;
    /// empty on one shown before each account had its own key.
    let accountKey: String
    let notificationKind: String
    let conversationId: String
    let runId: String
    let callId: String
    /// A question's choices, by the button that gives them.
    let answers: [String: String]

    static let key = "teanode"

    init(_ content: NotificationContent, accountKey: String) {
        self.accountKey = accountKey
        notificationKind = content.notificationKind
        conversationId = content.conversationId ?? ""
        runId = content.runId ?? ""
        callId = content.callId ?? ""
        var answers: [String: String] = [:]
        for button in content.buttons ?? [] where button.buttonAction.hasPrefix("answer:") {
            answers[button.buttonAction] = button.label
        }
        self.answers = answers
    }

    init?(userInfo: [AnyHashable: Any]) {
        guard let info = userInfo[Self.key] as? [String: Any] else { return nil }
        accountKey = info["accountKey"] as? String ?? ""
        notificationKind = info["notificationKind"] as? String ?? ""
        conversationId = info["conversationId"] as? String ?? ""
        runId = info["runId"] as? String ?? ""
        callId = info["callId"] as? String ?? ""
        answers = info["answers"] as? [String: String] ?? [:]
    }

    var dictionary: [String: Any] {
        [
            "accountKey": accountKey, "notificationKind": notificationKind, "conversationId": conversationId, "runId": runId, "callId": callId,
            "answers": answers,
        ]
    }
}
