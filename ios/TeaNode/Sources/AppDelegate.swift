import TeaNodeKit
import UIKit
import UserNotifications

/// AppDelegate is what iOS tells the app about notifications: the device
/// token to register with the relay, and a button pressed on a notification.
/// It holds the app's model, so the screens and it reach the same one.
final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    let app = AppModel.shared

    func application(
        _ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        center.setNotificationCategories(NotificationCategories.fixed)
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Task { await app.register(deviceToken: deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        app.noteNotificationFailure(error)
    }

    /// A notification arriving while the app is open is shown all the same:
    /// the server holds back what the person is looking at already.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list, .sound])
    }

    /// A notification or one of its buttons pressed. iOS is told it is done
    /// on the main thread: UIKit stops the app when the async form of this
    /// finishes on another.
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse, withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        let pressed = PressedButton(response)
        // iOS hands over this handler to be called once; it is called once,
        // on the main actor, when the press has been dealt with.
        nonisolated(unsafe) let finish = completionHandler
        Task { @MainActor in
            await self.perform(pressed)
            finish()
        }
    }

    func application(_ application: UIApplication, handleEventsForBackgroundURLSession identifier: String, completionHandler: @escaping () -> Void) {
        RecordingUploads.shared.handleBackgroundEvents(identifier: identifier, completion: completionHandler)
    }

    private func perform(_ pressed: PressedButton) async {
        await app.perform(pressed)
    }
}
