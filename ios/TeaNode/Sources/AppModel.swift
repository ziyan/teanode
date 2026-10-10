import Foundation
import Observation
import TeaNodeKit
import UIKit

/// AppModel is the app's sign-in: the account kept in the keychain, the
/// connection made from it, and connecting and signing out.
@MainActor
@Observable
final class AppModel {
    private(set) var account: Account?
    private(set) var connection: Connection?

    /// Why the app signed itself out, shown on the connect screen.
    var signedOutText: String?

    let store = KeychainAccountStore()
    let transport: any HTTPTransport

    /// Names this install to the server, so that when the agent asks where
    /// the person is, the app the turn was started from is the one that
    /// answers.
    let drawerId: String

    init() {
        let defaults = UserDefaults.standard
        if let saved = defaults.string(forKey: "drawerId") {
            drawerId = saved
        } else {
            drawerId = UUID().uuidString
            defaults.set(drawerId, forKey: "drawerId")
        }
        #if DEBUG
            if DemoServer.isRequested {
                transport = DemoServer()
                account = DemoServer.account
                connection = Connection(account: DemoServer.account, transport: transport)
                return
            }
        #endif
        transport = URLSessionTransport()
        if let saved = try? store.load() {
            use(saved)
        }
    }

    private func use(_ account: Account) {
        self.account = account
        connection = Connection(account: account, transport: transport, store: store)
    }

    /// Exchanges a connect link's code for a sign-in and finds out whose it
    /// is. Nothing is kept until the person confirms.
    func exchange(_ link: ConnectLink) async throws -> PendingConnection {
        guard let code = link.connectCode else {
            throw ConnectProblem.hasNoCode
        }
        let signIn = SignIn(server: link.server, transport: transport)
        let clientId = try await signIn.register()
        var account = try await signIn.exchange(connectCode: code, clientId: clientId, deviceName: UIDevice.current.name)
        account.username = try await Connection(account: account, transport: transport).username()
        return PendingConnection(account: account)
    }

    /// Keeps a sign-in the person confirmed, and tells the server which
    /// phone it is.
    func confirm(_ pending: PendingConnection) async {
        try? store.save(pending.account)
        use(pending.account)
        signedOutText = nil
        // A server without phones yet (before notifications came) refuses
        // this, which costs nothing: the phone works without it.
        try? await connection?.registerPhone(phoneName: UIDevice.current.name, phoneModel: Device.modelIdentifier)
    }

    /// Gives up a sign-in the person chose not to keep.
    func decline(_ pending: PendingConnection) async {
        await SignIn(server: pending.account.server, transport: transport).revoke(pending.account)
    }

    /// Signs this phone out: on the server too, unless the server already
    /// ended the sign-in.
    func signOut(signedOutText: String? = nil) async {
        if let account, signedOutText == nil {
            await SignIn(server: account.server, transport: transport).revoke(account)
        }
        try? store.delete()
        account = nil
        connection = nil
        self.signedOutText = signedOutText
    }

    /// What to show for an error from the server. An ended sign-in signs the
    /// app out, since nothing else will work until it connects again.
    func describe(_ error: Error) -> String {
        if let error = error as? TeaNodeError, error == .signedOut {
            Task { await signOut(signedOutText: error.localizedDescription) }
        }
        if let error = error as? LocalizedError, let description = error.errorDescription {
            return description
        }
        if (error as? URLError) != nil {
            return String(localized: "The server could not be reached. Check the connection and try again.")
        }
        return error.localizedDescription
    }
}

/// PendingConnection is a sign-in waiting for the person to say it is the
/// right server and the right account.
struct PendingConnection: Identifiable {
    let id = UUID()
    let account: Account
}

/// ConnectProblem is what is wrong with a scanned or pasted link.
enum ConnectProblem: LocalizedError {
    case isNotALink
    case hasNoCode

    var errorDescription: String? {
        switch self {
        case .isNotALink:
            return String(localized: "That is not a TeaNode connect code. Use the code on your dashboard's Connect a phone page.")
        case .hasNoCode:
            return String(localized: "That link has no code in it. Make a new one on your dashboard's Connect a phone page.")
        }
    }
}

/// Device is what this phone is.
enum Device {
    /// The model's identifier, such as "iPhone17,1".
    static var modelIdentifier: String {
        var system = utsname()
        uname(&system)
        return withUnsafeBytes(of: &system.machine) { bytes in
            String(decoding: bytes.prefix(while: { $0 != 0 }), as: UTF8.self)
        }
    }
}
