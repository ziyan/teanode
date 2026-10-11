import Foundation
import Observation
import SwiftUI
import TeaNodeKit
import UIKit
import UserNotifications

/// AppModel is the app's sign-in: the account kept in the keychain, the
/// connection made from it, and connecting and signing out.
@MainActor
@Observable
final class AppModel {
    private(set) var account: Account?
    private(set) var connection: Connection?

    /// Why the app signed itself out, shown on the connect screen.
    var signedOutText: String?

    /// A connect link opened from outside the app, for the connect screen.
    var openedLink: ConnectLink?

    /// What to say about a connect link opened while signed in.
    var openedLinkText: String?

    var isShowingOpenedLinkText: Bool {
        get { openedLinkText != nil }
        set { if !newValue { openedLinkText = nil } }
    }

    /// This phone as the server knows it, once it has registered.
    private(set) var phone: AgentPhone?

    /// Whether the person allowed notifications.
    private(set) var isNotificationAllowed = false

    /// Why notifications could not be set up, for Settings.
    private(set) var notificationProblemText: String?

    /// A conversation a pressed notification asks to open.
    var openedConversationId: String?

    /// Something elsewhere in the app the person asked the agent about,
    /// such as a mail; the conversation takes it as what the next message
    /// refers to.
    var askingAbout: AgentReference?

    /// A page of the agent's knowledge a link asks to open.
    var openedKnowledgePath: String?

    /// A mail a link asks to open.
    var openedMailItemId: String?

    /// An address to write to, from a contact: compose opens with it.
    var composingTo: String?

    /// A part of the app the agent asked to show.
    var openedSection: AppSection?

    /// The open_page events already followed, by run and sequence, so a
    /// feed replayed after the screen comes back does not move the app again.
    private var shownNavigations: Set<String> = []

    func show(path: String, once key: String) {
        guard shownNavigations.insert(key).inserted else { return }
        show(path: path)
    }

    /// Goes to the dashboard path the agent's open_page names, checked as
    /// the dashboard checks it: the person's own pages, in plain segments.
    func show(path written: String) {
        let path = written.count > 1 && written.hasSuffix("/") ? String(written.dropLast()) : written
        guard path.hasPrefix("/"), path.count <= 600 else { return }
        let segments = path.dropFirst().split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        let isPlain = segments.allSatisfy { segment in
            !segment.isEmpty && segment != "." && segment != ".."
                && segment.allSatisfy { $0.isLetter || $0.isNumber || "-._~".contains($0) }
        }
        guard isPlain, let first = segments.first else { return }
        switch first {
        case "knowledge":
            let page = segments.dropFirst().joined(separator: "/")
            if page.isEmpty { openedSection = .knowledge } else { openedKnowledgePath = page }
        case "mailbox":
            if segments.count >= 3, segments[1] != "compose", segments[1] != "subscriptions" {
                openedMailItemId = segments[2]
            } else {
                openedSection = .mail
            }
        case "finance":
            if path != "/finance/link" { openedSection = .finance }
        case "settings":
            openedSection = .settings
        default:
            break
        }
    }

    /// The agent's name, for the main conversation's title and the
    /// composer; empty until read, or when the agent has none.
    private(set) var agentName = ""

    /// What the main conversation is called: the agent's name.
    var agentTitle: String { agentName.isEmpty ? String(localized: "Your agent") : agentName }

    /// Where a link in an answer goes. Web links open as they are; a mail
    /// item or a memory page opens its page on the dashboard; anything else
    /// is not followed, as the dashboard does not follow it.
    func route(_ address: URL) -> OpenURLAction.Result {
        let written = address.absoluteString
        switch address.scheme?.lowercased() {
        case "http", "https":
            return .systemAction(address)
        case "mail":
            let itemId = String(written.dropFirst("mail:".count))
            guard !itemId.isEmpty, itemId.allSatisfy({ $0.isLetter || $0.isNumber }) else { return .discarded }
            // A mail link opens the conversation in the app's mail.
            openedMailItemId = itemId
            return .handled
        case "memory":
            let path = String(written.dropFirst("memory:".count).split(separator: "#").first ?? "")
            let segments = path.split(separator: "/")
            guard !segments.isEmpty, segments.count <= 8,
                segments.allSatisfy({ $0.allSatisfy { $0.isLowercase || $0.isNumber || $0 == "-" } })
            else { return .discarded }
            // A memory link opens the page in the app, under More.
            openedKnowledgePath = path
            return .handled
        default:
            return .discarded
        }
    }

    /// A file being shown in Quick Look: pictures with pinch to zoom, and
    /// any other kind the phone can show.
    var previewAddress: URL?

    /// Pictures already read, by their address on the server.
    private var pictures: [String: UIImage] = [:]

    /// Reads a picture the server keeps, once.
    func picture(_ path: String) async -> UIImage? {
        if let known = pictures[path] { return known }
        guard let connection, let data = try? await connection.download(path), let picture = UIImage(data: data) else { return nil }
        pictures[path] = picture
        return picture
    }

    /// Shows a file the server keeps in Quick Look: read, written to a
    /// place of the app's own under its name, then opened.
    func preview(_ path: String, name: String) async {
        guard let connection else { return }
        do {
            let data = try await connection.download(path)
            let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let safeName = name.isEmpty ? "file" : name.replacingOccurrences(of: "/", with: "-")
            let address = folder.appendingPathComponent(safeName)
            try data.write(to: address)
            previewAddress = address
        } catch {
            previewProblemText = describe(error)
        }
    }

    /// Why a file could not be shown.
    var previewProblemText: String?

    var isShowingPreviewProblem: Bool {
        get { previewProblemText != nil }
        set { if !newValue { previewProblemText = nil } }
    }

    /// Reads the agent's name.
    func loadAgentName() async {
        if let connection, let name = try? await connection.agentName() {
            agentName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }

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
        // A sign-in kept before the app shared a keychain group with its
        // notification extension moves into the shared one, once.
        if !defaults.bool(forKey: "isKeychainShared") {
            try? Keychain.moveToSharedGroup("account")
            defaults.set(true, forKey: "isKeychainShared")
        }
        if let saved = try? store.load() {
            use(saved)
        }
    }

    private func use(_ account: Account) {
        self.account = account
        connection = Connection(account: account, transport: transport, store: store)
    }

    /// Takes a link iOS opened the app with. A connect code is used only
    /// while the phone is signed out: one opened while signed in is left
    /// unspent, and the person is told why.
    func open(_ url: URL) {
        guard let link = ConnectLink(url) else { return }
        guard let account else {
            openedLink = link
            return
        }
        if link.server == account.server {
            openedLinkText = String(localized: "This phone is already connected to \(account.server.host).")
        } else {
            openedLinkText = String(localized: "This phone is connected to \(account.server.host). To connect it to \(link.server.host), sign out in Settings first.")
        }
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
        await setUpNotifications()
    }

    /// Asks to send notifications and registers for them. Done after
    /// connecting and each time the app starts, so a new device token, or a
    /// registration the relay no longer takes, heals on its own.
    func setUpNotifications() async {
        guard let connection else { return }
        #if DEBUG
            // The screenshots CI takes would show the permission prompt.
            if DemoServer.isRequested { return }
        #endif
        let center = UNUserNotificationCenter.current()
        isNotificationAllowed = (try? await center.requestAuthorization(options: [.alert, .sound, .badge])) ?? false
        if isNotificationAllowed {
            // The device token arrives in AppDelegate, which hands it to
            // register(deviceToken:).
            UIApplication.shared.registerForRemoteNotifications()
        } else {
            // Listed on the dashboard all the same, as a phone that is told
            // nothing.
            phone = try? await connection.registerPhone(phoneName: UIDevice.current.name, phoneModel: Device.modelIdentifier)
        }
    }

    /// Registers the device token with the relay and hands the sealed
    /// registration, with the key notifications are encrypted with, to the
    /// server.
    func register(deviceToken: Data) async {
        guard let connection else { return }
        #if DEBUG
            // A build installed from Xcode is delivered to by Apple's sandbox.
            let isSandbox = true
        #else
            let isSandbox = false
        #endif
        do {
            let pushRegistration = try await PushRelay(transport: transport).register(deviceToken: deviceToken, isSandbox: isSandbox)
            phone = try await connection.registerPhone(
                phoneName: UIDevice.current.name, phoneModel: Device.modelIdentifier,
                pushRegistration: pushRegistration, payloadKey: try PayloadKey.current())
            notificationProblemText = nil
        } catch {
            notificationProblemText = describe(error)
        }
    }

    func noteNotificationFailure(_ error: Error) {
        notificationProblemText = error.localizedDescription
    }

    /// Whether this phone is told about a kind of notification.
    func wants(_ notificationKind: String) -> Bool {
        guard let phone else { return true }
        return phone.notificationKinds.isEmpty || phone.notificationKinds.contains(notificationKind)
    }

    /// Turns one kind of notification on or off for this phone. At least
    /// one stays on: none chosen means every kind.
    func setNotificationKind(_ notificationKind: String, isOn: Bool) async {
        guard let connection, let phone, wants(notificationKind) != isOn else { return }
        var kinds = phone.notificationKinds.isEmpty ? AgentPhone.allNotificationKinds : phone.notificationKinds
        if isOn {
            kinds.append(notificationKind)
        } else {
            kinds.removeAll { $0 == notificationKind }
        }
        guard !kinds.isEmpty else { return }
        do {
            self.phone = try await connection.updatePhone(phone.id, notificationKinds: kinds)
        } catch {
            notificationProblemText = describe(error)
        }
    }

    /// Does what a pressed notification button says, in the background
    /// when the button does not open the app.
    func perform(_ pressed: PressedButton) async {
        guard let info = pressed.info else { return }
        if pressed.actionIdentifier == UNNotificationDefaultActionIdentifier || pressed.actionIdentifier == "open" {
            if !info.conversationId.isEmpty {
                openedConversationId = info.conversationId
            }
            return
        }
        guard let connection else { return }
        do {
            switch pressed.actionIdentifier {
            case "approve", "deny":
                try await connection.resolveConfirmation(runId: info.runId, callId: info.callId, isApproved: pressed.actionIdentifier == "approve")
            case "reply":
                let text = (pressed.typedText ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                guard !text.isEmpty else { return }
                if info.notificationKind == "question", !info.callId.isEmpty {
                    try await connection.answerQuestion(runId: info.runId, callId: info.callId, answer: text)
                } else {
                    _ = try await connection.ask(text, in: info.conversationId.isEmpty ? nil : info.conversationId, drawerId: drawerId)
                }
            default:
                guard let answer = info.answers[pressed.actionIdentifier] else { return }
                try await connection.answerQuestion(runId: info.runId, callId: info.callId, answer: answer)
            }
        } catch {
            // Nobody is looking at the app: say so where the button was.
            let content = UNMutableNotificationContent()
            content.title = "TeaNode"
            content.body = String(localized: "That did not reach your agent: \(describe(error))")
            try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
        }
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
        try? PayloadKey.forget()
        account = nil
        connection = nil
        phone = nil
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

/// PressedButton is a button pressed on a notification, read out of iOS's
/// response so it can cross to the main actor.
struct PressedButton: Sendable {
    let actionIdentifier: String
    let typedText: String?
    let info: NotificationInfo?

    init(_ response: UNNotificationResponse) {
        actionIdentifier = response.actionIdentifier
        typedText = (response as? UNTextInputNotificationResponse)?.userText
        info = NotificationInfo(userInfo: response.notification.request.content.userInfo)
    }
}
