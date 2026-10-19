import AuthenticationServices
import CryptoKit
import Foundation
import Observation
import SwiftUI
import TeaNodeKit
import UIKit
import UserNotifications
import WidgetKit

/// AppModel is the app's sign-ins: every account kept in the keychain, the
/// one in use and the connection made from it, and connecting, switching
/// and signing out.
/// OpenedPhotoPerson asks Photos to show the photos of one person in the circles.
struct OpenedPhotoPerson: Hashable {
    let userId: String
    let name: String
}

@MainActor
@Observable
final class AppModel {
    /// The one model: the screens, the notifications and Siri reach it.
    static let shared = AppModel()

    private(set) var account: Account?
    /// Every server the app is signed in to, the one in use among them.
    private(set) var accounts: [Account] = []
    /// The Add account sheet, over the signed-in app.
    var isAddingAccount = false
    private(set) var connection: Connection?

    private(set) var services: ServiceCapabilities?
    /// What the person may do on this server, as the access pages read it;
    /// nil until read, or when it could not be.
    private(set) var accessSession: AccessSession?
    /// Whether the server could not be asked which services it runs, and
    /// none are known yet: the app says so rather than calling them off.
    private(set) var hasServicesFailedToLoad = false
    /// Whether the person has a computer attached that runs herdr, which is
    /// what the Coding section is for.
    private(set) var hasCodingComputers = false
    /// How many of the person's coding sessions ask something, a question
    /// or an approval: the number on the side menu's Coding row.
    private(set) var codingAskingCount = 0

    /// Connections to accounts not in use, made to answer their
    /// notifications, by account. Each account renews its sign-in through
    /// one connection, so two never spend the same refresh token.
    private var otherConnections: [String: Connection] = [:]

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

    /// Whether a pressed notification asks for the Circles screen, which the
    /// settings screen opens and then clears.
    var isCirclesOpened = false

    /// Something elsewhere in the app the person asked the agent about,
    /// such as a mail; the conversation takes it as what the next message
    /// refers to.
    var askingAbout: AgentReference?

    /// A page of the agent's knowledge a link asks to open.
    var openedKnowledgePath: String?
    /// A page another person shared, which a link asks to open by the
    /// username of whoever shared it.
    var openedSharedKnowledge: SharedKnowledgeLink?
    var openedRecordingLink: RecordingLink?
    private(set) var openedRecordingAccountKey: String?

    /// A mail a link asks to open.
    var openedMailItemId: String?

    /// A contact card a link from the agent's memory asks to open.
    var openedContactId: String?

    /// The person in the circles whose photos Photos opens the library
    /// filtered to, from Circles or Location. The Photos screen takes it and
    /// clears it; the app shell clears it when Photos cannot be shown.
    var openedPhotoPerson: OpenedPhotoPerson?

    /// An address to write to, from a contact: compose opens with it.
    var composingTo: String?

    /// A part of the app the agent asked to show.
    var openedSection: AppSection?
    /// The person whose location the Location screen opens with selected: a
    /// pressed notification, or a link, names them. The screen takes it and
    /// clears it.
    var openedLocationUserId: String?
    /// The day the Location screen opens on the map of, from a link such as
    /// `/location?day=2030-01-15`. The screen takes it and clears it.
    var openedLocationDay: Date?
    /// The section to open again after the app is drawn anew, as it is when
    /// the display language changes.
    var keptSection: AppSection?

    /// Siri asked for a call: the conversation starts one once it can.
    var isCallRequested = false

    /// Siri, the Action button or Control Center asked for a recording: the recordings screen starts one once it can.
    var isRecordingRequested = false

    /// The call under way, for the buttons on its Live Activity.
    weak var currentCall: VoiceCall?

    /// A conversation opened to send its first message: an idea tried from
    /// the Ideas page.
    var openingRequest: OpeningRequest?

    /// The agent took the app to the mailbox's mail programs, where the
    /// person sets the phone up for mail, calendars and contacts.
    var isShowingMailPrograms = false

    /// The finance section a page of the dashboard's Finance names.
    var openedFinancePart: FinancePart?

    /// The open_page events already followed, by run and sequence, so a
    /// feed replayed after the screen comes back does not move the app again.
    private var shownNavigations: Set<String> = []

    func show(path: String, once key: String) {
        guard shownNavigations.insert(key).inserted else { return }
        show(path: path)
    }

    /// Opens one of the person's own records from a link on a page of memory,
    /// where the dashboard opens it at `dashboardPath`: a mail, a contact
    /// card, a conversation and a passage of a recording open themselves; an
    /// event, a reminder, a transaction and a receipt open the part of the
    /// app they are in.
    func openRecord(dashboardPath: String) {
        guard let target = RecordTarget(dashboardPath: dashboardPath) else { return }
        switch target {
        case .mail(let itemId):
            openedMailItemId = itemId
        case .contact(let id):
            openedContactId = id
        case .calendarEvent, .reminder:
            openedSection = .calendar
        case .conversation(let id):
            openedConversationId = id
        case .financeTransaction:
            openedFinancePart = .transactions
            openedSection = .finance
        case .financeReceipt:
            openedFinancePart = .receipts
            openedSection = .finance
        case .recording(let path):
            show(path: path)
        }
    }

    /// The Location page's address, `/location`, with the day it may name
    /// (`?day=2030-01-15`); nil for any other address. The day is nil when it
    /// is absent or not a date.
    static func locationLink(_ written: String) -> LocationLink? {
        let parts = written.split(separator: "?", maxSplits: 1, omittingEmptySubsequences: false)
        guard let pathText = parts.first, pathText == "/location" || pathText == "/location/" else { return nil }
        guard parts.count == 2 else { return LocationLink(day: nil) }
        for pair in parts[1].split(separator: "&") {
            let sides = pair.split(separator: "=", maxSplits: 1)
            guard sides.count == 2, sides[0] == "day" else { continue }
            let numbers = sides[1].split(separator: "-", omittingEmptySubsequences: false).compactMap { Int($0) }
            guard numbers.count == 3 else { continue }
            let calendar = Calendar.current
            guard let day = calendar.date(from: DateComponents(year: numbers[0], month: numbers[1], day: numbers[2])) else { continue }
            // A date that does not exist (month 13) rolls over into the next
            // one in a calendar; it is not the day that was written.
            let read = calendar.dateComponents([.year, .month, .day], from: day)
            if read.year == numbers[0], read.month == numbers[1], read.day == numbers[2] { return LocationLink(day: day) }
        }
        return LocationLink(day: nil)
    }

    /// Goes to the dashboard path the agent's open_page names, checked as
    /// the dashboard checks it: the person's own pages, in plain segments.
    func show(path written: String) {
        if let address = URL(string: written), let account, let citation = RecordingLink(address, server: account.server) {
            openedRecordingAccountKey = account.accountKey
            openedRecordingLink = citation
            openedSection = .recordings
            return
        }
        if let location = Self.locationLink(written) {
            openedLocationDay = location.day
            openedSection = .location
            return
        }
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
            guard path != "/finance/link" else { return }
            // The dashboard's sections are written with dashes: net-worth.
            if segments.count >= 2, let part = FinancePart.allCases.first(where: { $0.rawValue.lowercased() == segments[1].replacingOccurrences(of: "-", with: "") }) {
                openedFinancePart = part
            }
            openedSection = .finance
        case "settings":
            openedSection = .settings
            if segments.count >= 3, segments[1] == "mailbox", segments[2] == "devices" {
                isShowingMailPrograms = true
            }
        case "connect":
            // The agent shows the code to scan in the dashboard; this phone
            // is the one connected.
            if let account {
                openedLinkText = String(localized: "This phone is already connected to \(account.server.host).")
            }
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
        if let account, let citation = RecordingLink(address, server: account.server) {
            openedRecordingAccountKey = account.accountKey
            openedRecordingLink = citation
            openedSection = .recordings
            return .handled
        }
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
            // memory:PATH#N@username is a page another person shared, with
            // the username last; without it the page is the person's own.
            let target = written.dropFirst("memory:".count).split(separator: "@", maxSplits: 1, omittingEmptySubsequences: false)
            let path = String(target.first?.split(separator: "#").first ?? "")
            let segments = path.split(separator: "/")
            guard !segments.isEmpty, segments.count <= 8,
                segments.allSatisfy({ $0.allSatisfy { $0.isLowercase || $0.isNumber || $0 == "-" } })
            else { return .discarded }
            if target.count > 1 {
                let owner = String(target[1])
                guard !owner.isEmpty, owner.allSatisfy({ $0.isLetter || $0.isNumber || "._~-".contains($0) }) else { return .discarded }
                openedSharedKnowledge = SharedKnowledgeLink(ownerUsername: owner, path: path)
                return .handled
            }
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

    /// Pictures already read, by the account and their address on its
    /// server: two servers can use the same address for different pictures.
    private var pictures: [String: UIImage] = [:]

    /// Pictures being read right now, by the same key. A picture is cached
    /// only once its download has finished, so two views that ask for the
    /// same one on first appearance (a file card and the picture inside it)
    /// would each download it; the second waits for the first read instead.
    private var pictureReads: [String: Task<UIImage?, Never>] = [:]

    private static func pictureKey(accountKey: String, path: String) -> String { accountKey + " " + path }

    /// Reads a picture the server keeps, once.
    func picture(_ path: String) async -> UIImage? {
        guard let account else { return nil }
        return await picture(path, of: account)
    }

    /// Each signed-in account's own profile, by account key, for the list of
    /// accounts: the name and picture it goes by on its server.
    private(set) var accountProfiles: [String: AccountProfile] = [:]

    /// Reads an account's profile from its own server, through the connection
    /// kept for it, so an account not in use shows its name and picture too.
    func loadProfile(of chosen: Account) async {
        guard isSignedIn(chosen), let profile = try? await connection(for: chosen).accountProfile() else { return }
        // Signed out while the answer was on its way: nothing to show it on.
        guard isSignedIn(chosen) else { return }
        accountProfiles[chosen.accountKey] = profile
    }

    /// Reads a picture from an account's own server, once: from what was read
    /// before, from the read already on its way, or from the server.
    func picture(_ path: String, of chosen: Account) async -> UIImage? {
        let key = Self.pictureKey(accountKey: chosen.accountKey, path: path)
        if let known = pictures[key] { return known }
        if let reading = pictureReads[key] { return await reading.value }
        // The read is a task of its own so that a caller that stops waiting (its view
        // went away) does not cancel the download for the callers still waiting.
        let reading = Task<UIImage?, Never> {
            let picture = await self.readPicture(path, of: chosen)
            self.pictureReads[key] = nil
            if let picture { self.pictures[key] = picture }
            return picture
        }
        pictureReads[key] = reading
        return await reading.value
    }

    /// Downloads a picture from an account's own server, if the app is still signed in to it.
    private func readPicture(_ path: String, of chosen: Account) async -> UIImage? {
        guard isSignedIn(chosen), let data = try? await connection(for: chosen).download(path), let picture = UIImage(data: data) else {
            return nil
        }
        guard isSignedIn(chosen) else { return nil }
        return picture
    }

    /// Whether the app is still signed in to an account, which a profile or
    /// picture read for it checks before and after: connection(for:) would
    /// make a new connection for one signed out of meanwhile.
    private func isSignedIn(_ chosen: Account) -> Bool {
        accounts.contains { $0.accountKey == chosen.accountKey }
    }

    /// Keeps the profile the person just saved for the account in use, so the
    /// list of accounts shows the new name or picture without reading it again.
    func profileSaved(_ saved: AccountProfile) {
        guard let account, isSignedIn(account) else { return }
        accountProfiles[account.accountKey] = saved
    }

    /// Forgets what was read for an account's list row once it is signed out of.
    private func forgetProfile(accountKey: String) {
        accountProfiles[accountKey] = nil
        pictures = pictures.filter { !$0.key.hasPrefix(accountKey + " ") }
    }

    /// Shows a file the server keeps in Quick Look: read, written to a
    /// place of the app's own under its name, then opened.
    func preview(_ path: String, name: String) async {
        if let address = await downloadedFile(path, name: name) { previewAddress = address }
    }

    /// Reads a file the server keeps and writes it, as it is, to a place of
    /// the app's own under its name: Quick Look and the share sheet show the
    /// name, and a file shared this way keeps its own bytes. Nil, with the
    /// reason in previewProblemText, when it could not be read or written.
    func downloadedFile(_ path: String, name: String) async -> URL? {
        guard let connection else { return nil }
        do {
            let data = try await connection.download(path)
            let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let safeName = name.isEmpty ? "file" : name.replacingOccurrences(of: "/", with: "-")
            let address = folder.appendingPathComponent(safeName)
            try data.write(to: address)
            return address
        } catch {
            previewProblemText = describe(error)
            return nil
        }
    }

    /// Why a file could not be shown.
    var previewProblemText: String?

    var isShowingPreviewProblem: Bool {
        get { previewProblemText != nil }
        set { if !newValue { previewProblemText = nil } }
    }

    /// Reads the agent's name, and whether the person has turned it on.
    func loadAgentName() async {
        if let connection, let identity = try? await connection.agentIdentity() {
            agentName = identity.name.trimmingCharacters(in: .whitespacesAndNewlines)
            isAgentEnabledByPerson = identity.isOn
            hasAgent = identity.hasAgent
        }
    }

    /// Whether the person has turned their own agent on, as far as the
    /// server has said; on until it says otherwise.
    private(set) var isAgentEnabledByPerson = true

    /// Whether the person has an agent at all, as far as the server has said.
    /// One who has none is offered no switch to turn it on: only their chats.
    private(set) var hasAgent = true

    /// Whether there is an agent to talk to: the server runs agents and the
    /// person turned theirs on. Without one the conversation screen shows
    /// their chats, and where the main conversation would be, the offer to
    /// turn the agent on.
    var isAgentOn: Bool { (services?.isAssistantEnabled ?? true) && isAgentEnabledByPerson }

    /// Whether the person can write to the people they share a circle with,
    /// which needs no agent.
    private(set) var canChat = false

    /// How many messages wait in the chats that are not muted: the badge on
    /// the entry to the conversations.
    private(set) var unreadChatCount = 0

    /// The person's own id, which of a chat's members they are; nil until
    /// read.
    private(set) var ownUserId: String?

    /// Whether the account may record where the person goes and look back
    /// on it (the server's location:use), as far as the server has said.
    private(set) var canUseLocation = false

    /// Whether the permissions of the account in use have been read once
    /// (`canUseLocation` and `canChat` are only true after that). Until
    /// then, a screen that needs one of them is not known to be refused.
    private(set) var hasReadPermissions = false

    /// Whether the last try to read the permissions failed, so that a screen
    /// waiting for them can offer to try again instead of waiting for ever.
    private(set) var hasPermissionReadFailed = false

    /// Reads whether the person can chat, and how much is waiting for them
    /// in their chats.
    func refreshChats() async {
        guard let connection else { return }
        let session = try? await connection.accessSession()
        guard !Task.isCancelled, self.connection === connection else { return }
        if let userId = session?.userId { ownUserId = userId }
        hasPermissionReadFailed = session == nil && !hasReadPermissions
        if let session {
            canUseLocation = session.holds("location:use")
            hasReadPermissions = true
        }
        let canUseCircles = session?.holds("circle:use") ?? false
        canChat = canUseCircles
        guard canUseCircles else {
            unreadChatCount = 0
            return
        }
        if let conversations = try? await connection.conversations(), self.connection === connection {
            unreadChatCount = ChatUnread.total(of: conversations)
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
                // In the list too, so Settings shows the account as it does
                // for a real sign-in.
                accounts = [DemoServer.account]
                connection = Connection(account: DemoServer.account, transport: transport)
                canUseLocation = true
                hasReadPermissions = true
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
        let saved = try? store.load()
        // The key made before each account had its own stays with the
        // account in use, whose server was given it last.
        try? PayloadKey.keyring.moveSingleKey(to: saved?.accountKey)
        if let saved {
            use(saved)
        }
    }

    /// What the account in use said about itself is not true of the next one.
    private func forgetAccountFacts() {
        ownUserId = nil
        canChat = false
        canUseLocation = false
        hasReadPermissions = false
        hasPermissionReadFailed = false
        unreadChatCount = 0
        isAgentEnabledByPerson = true
        hasAgent = true
    }

    private func use(_ account: Account) {
        if let local = MeetingRecorder.shared.localRecording, local.accountKey == account.accountKey,
            !local.belongsTo(accountKey: account.accountKey, recordingUserId: account.cachedRecordingUserId) {
            Task { await MeetingRecorder.shared.stopForAccountRemoval(accountKey: account.accountKey) }
        }
        let isChanged = account.accountKey != self.account?.accountKey
        if isChanged {
            services = nil
            hasCodingComputers = false
            codingAskingCount = 0
            accessSession = nil
            forgetAccountFacts()
            openedRecordingLink = nil
            openedRecordingAccountKey = nil
        }
        self.account = account
        connection = otherConnections.removeValue(forKey: account.accountKey) ?? Connection(account: account, transport: transport, store: store)
        accounts = (try? store.accounts().accounts) ?? [account]
        if let connection { RecordingUploads.shared.activate(connection: connection) }
        // The widgets read the account in use; one that changed is read again.
        if isChanged { WidgetCenter.shared.reloadAllTimelines() }
        // What the phone noticed belongs to the account that was in use.
        LocationRecorder.shared.accountDidChange(accountKey: account.accountKey)
    }

    /// Puts another sign-in to use: the whole app is drawn again for it.
    func switchTo(_ chosen: Account) async {
        guard chosen.accountKey != account?.accountKey else { return }
        try? store.activate(chosen.accountKey)
        if let account, let connection {
            otherConnections[account.accountKey] = connection
        }
        agentName = ""
        phone = nil
        use(chosen)
        await setUpNotifications()
    }

    /// Takes a link iOS opened the app with. A connect code is used only
    /// while the phone is signed out: one opened while signed in is left
    /// unspent, and the person is told why.
    func open(_ url: URL) {
        if let account, let citation = RecordingLink(url, server: account.server) {
            openedRecordingAccountKey = account.accountKey
            openedRecordingLink = citation
            openedSection = .recordings
            return
        }
        guard let link = ConnectLink(url) else { return }
        // Signed in already, the code adds an account beside the others; the
        // person is shown whose it is before it is kept.
        openedLink = link
        if account != nil {
            isAddingAccount = true
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
        account.username = try await username(of: account)
        return PendingConnection(account: account)
    }

    /// Asks a server being added whose its new sign-in is. That server
    /// ending the sign-in it just gave is its refusal, said as one: it is no
    /// reason to sign out of the account in use.
    private func username(of pending: Account) async throws -> String {
        do {
            return try await Connection(account: pending, transport: transport).username()
        } catch TeaNodeError.signedOut {
            throw TeaNodeError.refused(String(localized: "\(pending.server.host) did not accept the sign-in it gave this phone."))
        }
    }

    /// Keeps a sign-in the person confirmed, and tells the server which
    /// phone it is.
    /// Signs in with a password on the server's own page, in a browser sheet
    /// the app cannot read, and returns the sign-in to keep. The page says
    /// the app gets the whole account; the code it returns is worth nothing
    /// without the verifier this keeps.
    func signIn(to server: ServerAddress, with session: WebAuthenticationSession) async throws -> PendingConnection {
        let signIn = SignIn(server: server, transport: transport)
        let clientId = try await signIn.register()
        let verifier = Self.randomText()
        let challenge = Self.base64URL(Data(SHA256.hash(data: Data(verifier.utf8))))
        let state = Self.randomText()
        // A private sheet, sharing no cookies with Safari: "Sign in with a
        // password" always asks which account, rather than approving whoever
        // the browser happens to be signed in as.
        let returned = try await session.authenticate(
            using: signIn.authorizationAddress(clientId: clientId, codeChallenge: challenge, state: state), callbackURLScheme: "com.teanode.app",
            preferredBrowserSession: .ephemeral)
        guard let code = SignIn.authorizationCode(in: returned, state: state) else {
            throw TeaNodeError.refused(String(localized: "The server did not sign this phone in."))
        }
        var account = try await signIn.exchange(authorizationCode: code, codeVerifier: verifier, clientId: clientId)
        account.username = try await username(of: account)
        return PendingConnection(account: account)
    }

    /// Thirty-two random bytes, as the URL-safe text PKCE and a state take.
    private static func randomText() -> String {
        var bytes = [UInt8](repeating: 0, count: 32)
        _ = SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes)
        return base64URL(Data(bytes))
    }

    private static func base64URL(_ data: Data) -> String {
        data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    func confirm(_ pending: PendingConnection) async {
        try? store.add(pending.account)
        // A connection kept for the same account holds the sign-in this one
        // replaces.
        otherConnections[pending.account.accountKey] = nil
        isAddingAccount = false
        agentName = ""
        phone = nil
        use(pending.account)
        signedOutText = nil
        await setUpNotifications()
    }

    /// Asks to send notifications and registers for them. Done after
    /// connecting and each time the app starts, so a new device token, or a
    /// registration the relay no longer takes, heals on its own.
    func setUpNotifications() async {
        guard let connection else { return }
        await refreshServices()
        // A server with no model has chats and no assistant, and a message in
        // a chat is told to the phone all the same.
        if services?.isAssistantEnabled != true { await refreshChats() }
        guard services?.isAssistantEnabled == true || canChat else { return }
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
    /// registration to the server of every account, each with the key its
    /// notifications are encrypted with.
    func register(deviceToken: Data) async {
        guard let connection, let account else { return }
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
                pushRegistration: pushRegistration, payloadKey: try PayloadKey.current(for: account.accountKey))
            notificationProblemText = nil
            await registerOtherAccounts(pushRegistration: pushRegistration)
        } catch {
            notificationProblemText = describe(error)
        }
    }

    /// Registers the phone with the servers of the accounts not in use, each
    /// with its own key. A server still holding the key from before each
    /// account had its own would have its notifications opened as another
    /// account's. One that cannot be reached is tried again at the next
    /// launch, and says nothing: the account in use is fine.
    private func registerOtherAccounts(pushRegistration: String) async {
        let others = ((try? store.accounts().accounts) ?? []).filter { $0.accountKey != account?.accountKey }
        for other in others {
            guard let payloadKey = try? PayloadKey.current(for: other.accountKey) else { continue }
            _ = try? await connection(for: other).registerPhone(
                phoneName: UIDevice.current.name, phoneModel: Device.modelIdentifier, pushRegistration: pushRegistration, payloadKey: payloadKey)
        }
    }

    /// The connection to an account: the one in use, or one kept for it.
    private func connection(for chosen: Account) -> Connection {
        if chosen.accountKey == account?.accountKey, let connection {
            return connection
        }
        if let kept = otherConnections[chosen.accountKey] {
            return kept
        }
        let made = Connection(account: chosen, transport: transport, store: store)
        otherConnections[chosen.accountKey] = made
        return made
    }

    /// The account a notification came from, while it is on the phone. One
    /// shown before each account had its own key does not say; it can only
    /// be the one account when there is one.
    private func notifyingAccount(_ info: NotificationInfo) -> Account? {
        let kept = (try? store.accounts().accounts) ?? accounts
        if info.accountKey.isEmpty {
            return kept.count == 1 ? kept.first : nil
        }
        return kept.first { $0.accountKey == info.accountKey }
    }

    func noteNotificationFailure(_ error: Error) {
        notificationProblemText = error.localizedDescription.asReasonSentence
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
    /// when the button does not open the app, as the account the
    /// notification came from. Opening one of another account's switches to
    /// that account first.
    func perform(_ pressed: PressedButton) async {
        guard let info = pressed.info else { return }
        let isOpening = pressed.actionIdentifier == UNNotificationDefaultActionIdentifier || pressed.actionIdentifier == "open"
        guard let notifying = notifyingAccount(info) else {
            let text =
                info.accountKey.isEmpty
                ? String(localized: "That notification does not say which account it is from. Answer it in TeaNode.")
                : String(localized: "That notification came from an account that is no longer on this phone.")
            if isOpening {
                openedLinkText = text
            } else {
                await tell(text)
            }
            return
        }
        if isOpening {
            await switchTo(notifying)
            if info.notificationKind == "circle" {
                // An invitation is answered on Circles, in the settings.
                openedSection = .settings
                isCirclesOpened = true
            } else if info.notificationKind == "location" {
                // Somebody arrived or left: Location, with them selected.
                openedLocationUserId = info.locationUserId.isEmpty ? nil : info.locationUserId
                openedSection = .location
            } else if !info.conversationId.isEmpty {
                openedConversationId = info.conversationId
            }
            return
        }
        let notifyingConnection = connection(for: notifying)
        do {
            switch pressed.actionIdentifier {
            case "approve", "deny":
                try await notifyingConnection.resolveConfirmation(runId: info.runId, callId: info.callId, isApproved: pressed.actionIdentifier == "approve")
            case "reply":
                let text = (pressed.typedText ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                guard !text.isEmpty else { return }
                if info.notificationKind == "question", !info.callId.isEmpty {
                    try await notifyingConnection.answerQuestion(runId: info.runId, callId: info.callId, answer: text)
                } else {
                    _ = try await notifyingConnection.ask(text, in: info.conversationId.isEmpty ? nil : info.conversationId, drawerId: drawerId)
                }
            default:
                guard let answer = info.answers[pressed.actionIdentifier] else { return }
                try await notifyingConnection.answerQuestion(runId: info.runId, callId: info.callId, answer: answer)
            }
        } catch {
            // A reply to a person in a chat went to the chat, not to the agent.
            if info.notificationKind == "chat" {
                await tell(String(localized: "That did not reach the chat: \(describe(error))"))
            } else {
                await tell(String(localized: "That did not reach your agent: \(describe(error))"))
            }
        }
    }

    /// Says something where a notification's button was: nobody is looking
    /// at the app.
    private func tell(_ text: String) async {
        let content = UNMutableNotificationContent()
        content.title = "TeaNode"
        content.body = text
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
    }

    /// Gives up a sign-in the person chose not to keep.
    func decline(_ pending: PendingConnection) async {
        await SignIn(server: pending.account.server, transport: transport).revoke(pending.account)
    }

    /// Signs this phone out: on the server too, unless the server already
    /// ended the sign-in.
    /// Signs out of the account in use, forgets the key its notifications
    /// are encrypted with, and puts the next one to use.
    func signOut(signedOutText: String? = nil) async {
        if let account, MeetingRecorder.shared.localRecording?.accountKey == account.accountKey {
            await MeetingRecorder.shared.stopForAccountRemoval(accountKey: account.accountKey)
        }
        if let account { await RecordingUploads.shared.cancel(accountKey: account.accountKey) }
        // Backup goes with the sign-in, and its password is revoked while the
        // sign-in can still do it.
        if let account { await PhotoBackupEngine.shared.accountRemoved(accountKey: account.accountKey, connection: connection) }
        if let account, signedOutText == nil {
            await SignIn(server: account.server, transport: transport).revoke(account)
        }
        let gone = account?.server.host ?? ""
        if let account {
            try? PayloadKey.keyring.forget(for: account.accountKey)
            ConversationDraft.forget(accountKey: account.accountKey)
            forgetProfile(accountKey: account.accountKey)
            LocationRecorder.shared.accountWasRemoved(accountKey: account.accountKey)
        }
        try? store.delete()
        phone = nil
        agentName = ""
        if let next = try? store.load() {
            use(next)
            if let signedOutText {
                openedLinkText = String(localized: "Signed out of \(gone): \(signedOutText)")
            }
            await setUpNotifications()
            return
        }
        account = nil
        accounts = []
        connection = nil
        forgetAccountFacts()
        self.signedOutText = signedOutText
        LocationRecorder.shared.accountDidChange(accountKey: nil)
    }

    /// Signs out of the account in use when its own connection found the
    /// sign-in ended. Another server ending its sign-in (an account being
    /// added, another account's notification) says nothing about this one;
    /// and the same failure reported again by another screen, after the
    /// sign out, finds another connection in use and does nothing.
    private func signOut(ifEnded ended: Connection, signedOutText: String) async {
        guard await ended.isSignedOut, ended === connection else { return }
        await signOut(signedOutText: signedOutText)
    }

    /// Signs out of an account not in use, keeping the one in use.
    func signOut(of other: Account) async {
        guard other.accountKey != account?.accountKey else {
            await signOut()
            return
        }
        if MeetingRecorder.shared.localRecording?.accountKey == other.accountKey {
            await MeetingRecorder.shared.stopForAccountRemoval(accountKey: other.accountKey)
        }
        await RecordingUploads.shared.cancel(accountKey: other.accountKey)
        await PhotoBackupEngine.shared.accountRemoved(accountKey: other.accountKey, connection: otherConnections[other.accountKey])
        await SignIn(server: other.server, transport: transport).revoke(other)
        otherConnections[other.accountKey] = nil
        forgetProfile(accountKey: other.accountKey)
        try? PayloadKey.keyring.forget(for: other.accountKey)
        ConversationDraft.forget(accountKey: other.accountKey)
        LocationRecorder.shared.accountWasRemoved(accountKey: other.accountKey)
        try? store.remove(other.accountKey)
        accounts = (try? store.accounts().accounts) ?? accounts
    }

    /// What to show for an error from the server. An ended sign-in of the
    /// account in use signs the app out of it, since nothing else will work
    /// until it connects again.
    func describe(_ error: Error) -> String {
        if let error = error as? TeaNodeError, error == .signedOut, let connection {
            Task { await signOut(ifEnded: connection, signedOutText: error.localizedDescription) }
        }
        // The Kit names what went wrong with a photo; the words are the app's.
        if let error = error as? PhotoError { return error.localizedText }
        if let error = error as? LocalizedError, let description = error.errorDescription {
            return description.asReasonSentence
        }
        // A request called off is not the server failing: it is said as
        // iOS says it, which the toasts know to leave out.
        if (error as? URLError)?.code == .cancelled || error is CancellationError {
            return URLError(.cancelled).localizedDescription
        }
        if (error as? URLError) != nil {
            return String(localized: "The server could not be reached. Check the connection and try again.")
        }
        return error.localizedDescription.asReasonSentence
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


extension AppModel {
    func refreshServices() async {
        guard let connection else { return }
        async let refreshedServices = try? connection.serviceCapabilities()
        // Read beside the services, so that Photos is shown or hidden once,
        // not shown and then hidden.
        async let refreshedSession = try? connection.accessSession()
        let hasVerifiedRecordingOwner = (try? await connection.recordingOwnerId()) != nil
        let verifiedAccount = await connection.account
        let availableServices = await refreshedServices
        let readSession = await refreshedSession
        // A refresh cancelled as the screens are drawn again, right after
        // signing in, failed to load nothing: counted as a failure, it
        // flashed "Cannot reach your server" before another refresh landed.
        guard !Task.isCancelled, self.connection === connection else { return }
        if hasVerifiedRecordingOwner {
            self.account = verifiedAccount
            self.accounts = (try? store.accounts().accounts) ?? self.accounts
        }
        if let availableServices { self.services = availableServices }
        hasServicesFailedToLoad = availableServices == nil && services == nil
        // What the person may use decides whether Photos is offered. A read
        // that fails keeps what was known, and nothing known offers it.
        if let readSession { accessSession = readSession }
    }

    /// Whether Photos is offered: the server has it on (it is, unless an
    /// operator turned it off) and the person holds `photos:use`. It gates the
    /// Photos section, the rows for Photo backup and People in Photos, and the
    /// backup switch.
    var isPhotosOffered: Bool {
        PhotosAvailability.isOffered(isServiceEnabled: services?.isPhotosEnabled == true, session: accessSession)
    }

    func noteCodingComputers() { hasCodingComputers = true }

    /// The Coding section read the sessions: how many ask, at once, rather
    /// than at the next slow ask.
    func noteCodingAsking(_ askingCount: Int) { codingAskingCount = askingCount }

    /// Asks which computers run herdr, and how many sessions ask. A server
    /// that cannot be asked leaves what is known.
    func refreshCodingPresence() async {
        guard let connection, services?.isAssistantEnabled == true,
            let list = try? await connection.herdrSessions()
        else { return }
        hasCodingComputers = !list.computerNames.isEmpty
        codingAskingCount = list.sessions.filter(\.isAsking).count
    }

    func isSectionAvailable(_ section: AppSection) -> Bool {
        let services = services ?? .unavailable
        switch section {
        case .mail: return services.isMailboxEnabled
        case .calendar: return services.isCalendarEnabled || services.isRemindersEnabled
        case .contacts: return services.isContactsEnabled
        // Hidden when an operator turned Photos off for the server, and from
        // someone who may not use it.
        case .photos: return isPhotosOffered
        case .recordings: return account?.cachedRecordingUserId != nil || MeetingRecorder.shared.hasSession
        // The agent's screen also holds the person's chats, which need no
        // agent.
        case .agent: return services.isAssistantEnabled || canChat
        case .goals, .ideas, .knowledge, .finance: return services.isAssistantEnabled
        case .location: return canUseLocation
        case .coding: return services.isAssistantEnabled && hasCodingComputers
        case .settings: return true
        }
    }
}

/// LocationLink is what an address of the Location page asks for.
struct LocationLink: Equatable {
    /// The day to open on the map, when the address names one.
    let day: Date?
}
