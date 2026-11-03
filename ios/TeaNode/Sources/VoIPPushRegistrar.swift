import Foundation
import PushKit
import TeaNodeKit

/// VoIPPushRegistrar is how the phone is rung for a call in a chat. PushKit
/// gives the app a token of its own for VoIP pushes, which the app registers
/// with the push relay as it does the notification token but with push type
/// voip (AppModel.register(voipToken:)), and the server sends the ring to.
///
/// A VoIP push starts the app if it is not running, and iOS ends an app that
/// does not report a call to CallKit when one arrives. So the push's sealed
/// content is opened here, with the same keys the notification extension
/// uses, and handed to ChatCallManager at once, which reports a call before
/// this returns.
///
/// It is made at launch (AppDelegate) and listens on the main queue, where the
/// app's own state is.
@MainActor
final class VoIPPushRegistrar: NSObject, @preconcurrency PKPushRegistryDelegate {
    static let shared = VoIPPushRegistrar()

    private let registry = PKPushRegistry(queue: .main)

    /// Starts listening: the token arrives in didUpdate, and pushes in
    /// didReceiveIncomingPushWith.
    func start() {
        registry.delegate = self
        registry.desiredPushTypes = [.voIP]
    }

    func pushRegistry(_ registry: PKPushRegistry, didUpdate pushCredentials: PKPushCredentials, for type: PKPushType) {
        guard type == .voIP else { return }
        let token = pushCredentials.token
        Task { await AppModel.shared.register(voipToken: token) }
    }

    func pushRegistry(_ registry: PKPushRegistry, didInvalidatePushTokenFor type: PKPushType) {
        // The server drops a registration Apple says is gone when it next
        // rings it (it clears it itself); a new token comes with didUpdate.
    }

    func pushRegistry(
        _ registry: PKPushRegistry, didReceiveIncomingPushWith payload: PKPushPayload, for type: PKPushType, completion: @escaping () -> Void
    ) {
        // PushKit hands over this completion to be called once, after the
        // call is reported; it is called on the main actor, when CallKit has it.
        nonisolated(unsafe) let finish = completion
        guard type == .voIP else {
            finish()
            return
        }
        ChatCallManager.shared.receiveVoIPPush(sealed: payload.dictionaryPayload["sealed"] as? String, finish: { finish() })
    }
}
