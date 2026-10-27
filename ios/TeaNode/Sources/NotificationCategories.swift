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
    static let skillShareCall = "skill_share_call"
    static let circle = "circle"
    static let location = "location"
    static let shopping = "shopping"
    static let codingQuestion = "herdr_question"
    static let questionPrefix = "question-"

    // The journal's evening question. The server sends it as a notification
    // of kind journalEveningQuestion (NotificationContent) carrying the day it
    // asks about as journalDay, written YYYY-MM-DD. It is shown with five
    // buttons, Awful to Great, which set that day's mood in the background,
    // without opening the app. Pressing the notification itself opens the day.
    static let journalEveningQuestion = "journalEveningQuestion"
    static let journalMoodActionPrefix = "journalMood"

    /// The mood, 1 (awful) to 5 (great), that a button of the evening
    /// question stands for; nil for any other button.
    static func journalMoodLevel(forAction actionIdentifier: String) -> Int? {
        guard actionIdentifier.hasPrefix(journalMoodActionPrefix), let level = Int(actionIdentifier.dropFirst(journalMoodActionPrefix.count)),
            (1...5).contains(level)
        else { return nil }
        return level
    }

    /// The five buttons of the evening question, the mood's name on each.
    private static var journalMoodActions: [UNNotificationAction] {
        let names = [
            String(localized: "Awful"), String(localized: "Low"), String(localized: "Okay"), String(localized: "Good"), String(localized: "Great"),
        ]
        return names.enumerated().map { index, name in
            // No options: pressing one is answered without opening the app or
            // unlocking the phone, since a mood is not private enough to ask
            // for the passcode.
            UNNotificationAction(identifier: journalMoodActionPrefix + String(index + 1), title: name, options: [])
        }
    }

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
            UNNotificationCategory(identifier: skillShareCall, actions: [openAction], intentIdentifiers: []),
            // An invitation is answered on the Circles screen, which Open goes
            // to: it is not a question for the agent, so it has no reply field.
            UNNotificationCategory(identifier: circle, actions: [openAction], intentIdentifiers: []),
            // Somebody arrived or left: nothing to answer, only Location to open.
            UNNotificationCategory(identifier: location, actions: [openAction], intentIdentifiers: []),
            // Arrived at a store, or somebody started shopping: only Shopping to open.
            UNNotificationCategory(identifier: shopping, actions: [openAction], intentIdentifiers: []),
            // A coding session asks: it is answered on the Coding screen, where
            // its options are, so Open is the only button.
            UNNotificationCategory(identifier: codingQuestion, actions: [openAction], intentIdentifiers: []),
            UNNotificationCategory(identifier: journalEveningQuestion, actions: journalMoodActions, intentIdentifiers: []),
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
        case "skill_share_call":
            return fixed.first { $0.identifier == skillShareCall }
        case "circle":
            return fixed.first { $0.identifier == circle }
        case "location":
            return fixed.first { $0.identifier == location }
        case "shopping":
            return fixed.first { $0.identifier == shopping }
        case "herdr_question":
            return fixed.first { $0.identifier == codingQuestion }
        case journalEveningQuestion:
            return fixed.first { $0.identifier == journalEveningQuestion }
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
    /// For a location notification, the person who arrived or left.
    let locationUserId: String
    /// For a shopping notification of arriving at a store, the store.
    let shoppingStoreName: String
    /// For a shopping notification that somebody started shopping a list, the list.
    let shoppingListId: String
    /// For a coding session's question, the computer and pane of the session.
    let computer: String
    let paneId: String
    /// For the journal's evening question, the day it asks about, YYYY-MM-DD.
    let journalDay: String
    /// A question's choices, by the button that gives them.
    let answers: [String: String]

    static let key = "teanode"

    init(_ content: NotificationContent, accountKey: String) {
        self.accountKey = accountKey
        notificationKind = content.notificationKind
        conversationId = content.conversationId ?? ""
        runId = content.runId ?? ""
        callId = content.callId ?? ""
        locationUserId = content.locationUserId ?? ""
        shoppingStoreName = content.shoppingStoreName ?? ""
        shoppingListId = content.shoppingListId ?? ""
        computer = content.computer ?? ""
        paneId = content.paneId ?? ""
        journalDay = content.journalDay ?? ""
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
        locationUserId = info["locationUserId"] as? String ?? ""
        shoppingStoreName = info["shoppingStoreName"] as? String ?? ""
        shoppingListId = info["shoppingListId"] as? String ?? ""
        computer = info["computer"] as? String ?? ""
        paneId = info["paneId"] as? String ?? ""
        journalDay = info["journalDay"] as? String ?? info["day"] as? String ?? ""
        answers = info["answers"] as? [String: String] ?? [:]
    }

    var dictionary: [String: Any] {
        [
            "accountKey": accountKey, "notificationKind": notificationKind, "conversationId": conversationId, "runId": runId, "callId": callId,
            "locationUserId": locationUserId, "shoppingStoreName": shoppingStoreName, "shoppingListId": shoppingListId,
            "computer": computer, "paneId": paneId, "journalDay": journalDay, "answers": answers,
        ]
    }
}
