import TeaNodeKit
import UserNotifications

/// NotificationService decrypts a notification before iOS shows it. Apple and
/// the relay carry only "New message" and the content sealed for this phone;
/// here it is opened with the phone's key, and given its title, its words and
/// its buttons. A notification that cannot be opened is shown as it came.
///
/// This target builds in the Swift 5 language mode: the extension's API hands
/// over a completion handler that is not marked as safe to send between
/// threads, and the categories are read and written through another.
final class NotificationService: UNNotificationServiceExtension {
    private var contentHandler: ((UNNotificationContent) -> Void)?
    private var shown: UNMutableNotificationContent?

    /// How many question categories are kept: each question with new choices
    /// makes one, and they are only needed while it is on screen.
    private static let questionCategoriesKept = 20

    override func didReceive(
        _ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        self.contentHandler = contentHandler
        let content = (request.content.mutableCopy() as? UNMutableNotificationContent) ?? UNMutableNotificationContent()
        shown = content
        guard let sealed = request.content.userInfo["sealed"] as? String,
            let key = try? PayloadKey.stored(),
            let opened = try? PayloadKey.open(sealed, key: key)
        else {
            contentHandler(content)
            return
        }
        content.title = opened.title
        content.body = opened.body
        content.userInfo = [NotificationInfo.key: NotificationInfo(opened).dictionary]
        guard let category = NotificationCategories.category(for: opened) else {
            contentHandler(content)
            return
        }
        content.categoryIdentifier = category.identifier
        let center = UNUserNotificationCenter.current()
        center.getNotificationCategories { existing in
            if !existing.contains(where: { $0.identifier == category.identifier }) {
                let questions = existing.filter { $0.identifier.hasPrefix(NotificationCategories.questionPrefix) }
                    .prefix(Self.questionCategoriesKept - 1)
                center.setNotificationCategories(NotificationCategories.fixed.union(questions).union([category]))
            }
            contentHandler(content)
        }
    }

    override func serviceExtensionTimeWillExpire() {
        if let contentHandler, let shown {
            contentHandler(shown)
        }
    }
}
