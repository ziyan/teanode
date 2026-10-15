import Charts
import Observation
import SwiftUI
import TeaNodeKit

// The dashboard's operator pages for the server and the agent, under
// /manage/server and /manage/agent. Each dashboard tab is a group of rows
// here, and each card a screen of its own with its Save in the toolbar. The
// screens a top view pushes read from one model, so a change saved on one
// shows on the list behind it.

// MARK: - Pieces the pages share

/// ServerWords is how these screens say times, amounts and the server's
/// fixed words.
private enum ServerWords {
    static func relative(_ timeText: String?) -> String {
        ServerTime.date(timeText)?.formatted(.relative(presentation: .named)) ?? ""
    }

    /// A moment with its zone: these are compared against logs from a server
    /// elsewhere.
    static func fullTime(_ timeText: String?) -> String {
        ServerTime.date(timeText)?.formatted(.dateTime.year().month(.abbreviated).day().hour().minute().timeZone(.specificName(.short))) ?? ""
    }

    static func count(_ number: Int) -> String {
        number.formatted(.number.notation(.compactName))
    }

    static func supervision(_ supervision: String) -> String {
        switch supervision {
        case "container": String(localized: "a container")
        case "systemd": String(localized: "systemd")
        default: String(localized: "nothing we can see")
        }
    }

    /// An origin without the slash a URL may end in.
    static func origin(_ origin: URL) -> String {
        var text = origin.absoluteString
        while text.hasSuffix("/") { text.removeLast() }
        return text
    }
}

/// SettingField is one typed setting: its name over the box, and what it is
/// for under it, as the dashboard's forms put a hint under each field.
private struct SettingField: View {
    let label: LocalizedStringKey
    @Binding var text: String
    var hint: LocalizedStringKey?
    var prompt: String?
    var isMonospaced = true
    var keyboardType: UIKeyboardType = .default

    var body: some View {
        Section {
            TextField(label, text: $text, prompt: prompt.map { Text(verbatim: $0) })
                .font(isMonospaced ? .body.monospaced() : .body)
                .keyboardType(keyboardType)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        } header: {
            Text(label)
        } footer: {
            if let hint { Text(hint) }
        }
    }
}

/// SecretField is a value the server will not read back. It says whether
/// one is stored rather than pretending to show it; an empty box keeps it,
/// and clearing it is a separate, deliberate step.
private struct SecretField: View {
    let label: LocalizedStringKey
    let isStored: Bool
    @Binding var typed: String
    /// What the key is for, shown in the footer, when it is not obvious.
    var hint: LocalizedStringKey?
    /// Whether clearing it is offered, and whether it is being cleared.
    var isClearing: Binding<Bool>?

    private var isKeeping: Bool { isStored && !(isClearing?.wrappedValue ?? false) }

    var body: some View {
        Section {
            SecureField(label, text: $typed, prompt: Text(isKeeping ? LocalizedStringKey("Leave empty to keep it") : "Not set"))
                .textContentType(.password)
            if let isClearing, isStored, typed.isEmpty {
                if isClearing.wrappedValue {
                    Button("Keep it") { isClearing.wrappedValue = false }
                } else {
                    Button("Clear", role: .destructive) { isClearing.wrappedValue = true }
                }
            }
        } header: {
            Text(label)
        } footer: {
            if let hint {
                Text(hint)
            }
            if isClearing?.wrappedValue == true {
                Text("Cleared when you save.")
            } else if isStored {
                Text("Stored. It is never shown again.")
            }
        }
    }
}

/// RestartNote is what a form says when saving it changes what is stored
/// and nothing else until the server restarts, with the way to the page
/// that restarts it.
private struct RestartNote: View {
    let connection: Connection

    var body: some View {
        Section {
            NavigationLink {
                ServerAboutView(connection: connection)
            } label: {
                Label("Restart it on the About page", systemImage: "arrow.clockwise")
            }
        } footer: {
            Text("These are read once, when the server starts. Saving stores the change; the server goes on using what it read until it is restarted.")
        }
    }
}

// MARK: - The server's settings

/// ServerSettingsModel is what the server's settings screens read: whether
/// the person may manage the server, and the settings, read again from what
/// each save answers with.
@MainActor
@Observable
final class ServerSettingsModel {
    let connection: Connection
    private(set) var access: OperatorAccess?
    private(set) var settings: ServerSettings?
    private(set) var isLoading = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    var canManageServer: Bool { access?.canManageServer ?? false }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let readAccess = try await connection.operatorAccess()
            access = readAccess
            guard readAccess.canManageServer else { return }
            settings = try await connection.serverSettings()
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Saves one or more sections, keeps what the server stored, and says
    /// how it went.
    func save(_ sections: [String: JSONValue], saying said: String) async -> Bool {
        do {
            settings = try await connection.updateServerSettings(sections)
            doneText = said
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }
}

/// ServerSettingsView is the dashboard's Server page: what this instance is
/// and which version it runs, what is still missing to finish setting it
/// up, and every settings tab in the order mail moves through them. It needs
/// server:manage.
struct ServerSettingsView: View {
    let connection: Connection
    @State private var model: ServerSettingsModel

    init(connection: Connection) {
        self.connection = connection
        _model = State(initialValue: ServerSettingsModel(connection: connection))
    }

    var body: some View {
        List {
            if model.canManageServer {
                ServerSettingsRows(model: model, tabs: ServerSettingsTab.allCases)
            }
        }
        .navigationTitle("Server")
        .overlay {
            if model.access == nil && model.isLoading {
                ProgressView()
            } else if let access = model.access, !access.canManageServer {
                ContentUnavailableView(
                    "Nothing to manage", systemImage: "lock",
                    description: Text("This account may not manage the server."))
            }
        }
        .task { await model.load() }
        .refreshable { await model.load() }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }
}

/// IntegrationsView is the services this server talks to: how mail leaves,
/// certificates, where messages are kept, the scanners and single sign-on.
/// The same rows are on the Server page among the rest.
struct IntegrationsView: View {
    let connection: Connection
    @State private var model: ServerSettingsModel

    init(connection: Connection) {
        self.connection = connection
        _model = State(initialValue: ServerSettingsModel(connection: connection))
    }

    var body: some View {
        List {
            if model.canManageServer {
                ServerSettingsRows(model: model, tabs: ServerSettingsTab.integrationTabs)
            }
        }
        .navigationTitle("Integrations")
        .overlay {
            if model.access == nil && model.isLoading {
                ProgressView()
            } else if let access = model.access, !access.canManageServer {
                ContentUnavailableView(
                    "Nothing to manage", systemImage: "lock",
                    description: Text("This account may not manage the server."))
            }
        }
        .task { await model.load() }
        .refreshable { await model.load() }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }
}

/// ServerSettingsRows is the rows of the server's tabs, in a few groups of
/// rows that belong together rather than a group per tab, so a header
/// never only repeats the one row under it.
private struct ServerSettingsRows: View {
    let model: ServerSettingsModel
    let tabs: [ServerSettingsTab]

    /// SettingsRow is one card of the server's settings, a row of the list.
    enum SettingsRow: CaseIterable {
        case identity, listeners, certificates, route53, resolver
        case limits, relay, proxy
        case scanner, antispam, geoIP
        case messageStorage, objectStore
        case sessions, passkeys, identityProviders

        /// The tab of the dashboard's Server page the card is on.
        var tab: ServerSettingsTab {
            switch self {
            case .identity: .identity
            case .listeners: .listeners
            case .certificates, .route53: .certificates
            case .resolver: .resolver
            case .limits: .mail
            case .relay, .proxy: .sending
            case .scanner: .scanning
            case .antispam: .spam
            case .geoIP, .messageStorage, .objectStore: .storage
            case .sessions, .passkeys: .sessions
            case .identityProviders: .sso
            }
        }
    }

    /// SettingsGroup is the rows under one header.
    struct SettingsGroup: Identifiable {
        let title: LocalizedStringKey
        let rows: [SettingsRow]
        var id: SettingsRow { rows[0] }
    }

    static let groups = [
        SettingsGroup(title: "Network", rows: [.identity, .listeners, .certificates, .route53, .resolver]),
        SettingsGroup(title: "Mail", rows: [.limits, .relay, .proxy]),
        SettingsGroup(title: "Filtering", rows: [.scanner, .antispam, .geoIP]),
        SettingsGroup(title: "Storage", rows: [.messageStorage, .objectStore]),
        SettingsGroup(title: "Sign-in", rows: [.sessions, .passkeys, .identityProviders]),
    ]

    var body: some View {
        if tabs.contains(.about) || tabs.contains(.setup) {
            Section {
                if tabs.contains(.about) {
                    NavigationLink {
                        ServerAboutView(connection: model.connection)
                    } label: {
                        Label("About", systemImage: "info.circle")
                    }
                }
                if tabs.contains(.setup) {
                    NavigationLink {
                        ServerSetupView(connection: model.connection)
                    } label: {
                        Label("Setup", systemImage: "checklist")
                    }
                }
            } footer: {
                Text("Setting this server up, what it talks to, and which version it is running.")
            }
        }
        ForEach(Self.groups) { group in
            let shownRows = group.rows.filter { tabs.contains($0.tab) }
            if !shownRows.isEmpty {
                Section {
                    ForEach(shownRows, id: \.self) { settingsRow in
                        row(settingsRow)
                    }
                } header: {
                    Text(group.title)
                }
            }
        }
    }

    @ViewBuilder
    private func row(_ settingsRow: SettingsRow) -> some View {
        switch settingsRow {
        case .identity:
            NavigationLink { IdentityFormView(model: model) } label: { Label("This server", systemImage: "server.rack") }
        case .listeners:
            NavigationLink { ListenFormView(model: model) } label: { Label("Listeners", systemImage: "antenna.radiowaves.left.and.right") }
        case .certificates:
            NavigationLink { CertificatesFormView(model: model) } label: { Label("Certificates", systemImage: "lock.shield") }
        case .route53:
            NavigationLink { Route53FormView(model: model) } label: { Label("Route53", systemImage: "globe") }
        case .resolver:
            NavigationLink { ResolverFormView(model: model) } label: { Label("Resolver", systemImage: "point.topleft.down.to.point.bottomright.curvepath") }
        case .limits:
            NavigationLink { SMTPLimitsFormView(model: model) } label: { Label("Limits", systemImage: "gauge.with.dots.needle.33percent") }
        case .relay:
            NavigationLink { RelayFormView(model: model) } label: { Label("Outgoing relay", systemImage: "paperplane") }
        case .proxy:
            NavigationLink { ProxyFormView(model: model) } label: { Label("Outgoing proxy", systemImage: "point.3.connected.trianglepath.dotted") }
        case .scanner:
            NavigationLink { ScannerFormView(model: model) } label: { Label("Virus scanning", systemImage: "cross.case") }
        case .antispam:
            NavigationLink { AntispamFormView(model: model) } label: { Label("Spam scoring", systemImage: "xmark.bin") }
        case .geoIP:
            NavigationLink { GeoIPFormView(model: model) } label: { Label("Where senders connect from", systemImage: "mappin.and.ellipse") }
        case .messageStorage:
            NavigationLink { MessageStorageFormView(model: model) } label: { Label("On this machine", systemImage: "internaldrive") }
        case .objectStore:
            NavigationLink { ObjectStoreFormView(model: model) } label: { Label("Object store", systemImage: "externaldrive.connected.to.line.below") }
        case .sessions:
            NavigationLink { SessionFormView(model: model) } label: { Label("Sessions", systemImage: "clock") }
        case .passkeys:
            NavigationLink { PasskeyFormView(model: model) } label: { Label("Passkeys", systemImage: "person.badge.key") }
        case .identityProviders:
            NavigationLink { SSOFormView(model: model) } label: { Label("Identity providers", systemImage: "person.crop.circle.badge.checkmark") }
        }
    }
}

/// ServerSettingsForm is one card of the server's settings as a screen: its
/// fields filled from what is stored, and a Save in the toolbar that sends
/// the section and goes back. A form that asks before it saves names the
/// question.
private struct ServerSettingsForm<Draft: Hashable, Fields: View>: View {
    let model: ServerSettingsModel
    let title: LocalizedStringKey
    let makeDraft: (ServerSettings) -> Draft
    let variables: (Draft) -> [String: JSONValue]
    var confirmation: ((Draft) -> (question: String, message: String, buttonTitle: String))?
    @ViewBuilder let fields: (Binding<Draft>, ServerSettings) -> Fields
    @Environment(\.dismiss) private var dismiss
    @State private var draft: Draft?
    @State private var isSaving = false
    @State private var isConfirming = false

    var body: some View {
        Form {
            if let settings = model.settings, let filled = draft {
                fields(Binding(get: { draft ?? filled }, set: { draft = $0 }), settings)
            }
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if model.settings == nil { ProgressView() }
        }
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: hasChanges) {
                if confirmation != nil {
                    isConfirming = true
                } else {
                    save()
                }
            }
        }
        // Left on the screen: the Save button lives in AgentSaveToolbar, which cannot carry a modifier from here.
        .confirmationDialog(
            confirmationText?.question ?? "", isPresented: $isConfirming, titleVisibility: .visible
        ) {
            Button(confirmationText?.buttonTitle ?? String(localized: "Save")) { save() }
        } message: {
            Text(verbatim: confirmationText?.message ?? "")
        }
        .onAppear {
            if draft == nil, let settings = model.settings { draft = makeDraft(settings) }
        }
        .onChange(of: model.settings) {
            if draft == nil, let settings = model.settings { draft = makeDraft(settings) }
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private var hasChanges: Bool {
        guard let draft, let settings = model.settings else { return false }
        return draft != makeDraft(settings)
    }

    private var confirmationText: (question: String, message: String, buttonTitle: String)? {
        guard let confirmation, let draft else { return nil }
        return confirmation(draft)
    }

    private func save() {
        guard let draft else { return }
        isSaving = true
        let sections = variables(draft)
        Task {
            let isSaved = await model.save(sections, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// IdentityFormView is what this server calls itself and how much it logs.
/// The data directory is shown and not editable: a server restarted against
/// a different one does not find what is in the old one.
private struct IdentityFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "This server", makeDraft: { IdentityDraft($0.identity) }, variables: { $0.variables }) { draft, settings in
            Section {
            } footer: {
                Text("What this server calls itself, and how much it writes down.")
            }
            SettingField(
                label: "Name", text: draft.name,
                hint: "The name this server gives in its SMTP banner and derives its own certificate from. A name that does not resolve to this machine makes other servers suspicious of your mail."
            )
            SettingField(
                label: "Mail server names", text: draft.mailServersText,
                hint: "Comma separated. The hosts every domain’s MX records name, unless that domain names its own on its Settings tab. Changing this changes the records every domain has to publish."
            )
            SettingField(
                label: "Also reached at", text: draft.externalAddressesText,
                hint: "Addresses mail reaches this server at when they are not its own: a relay in front of it, a tunnel, a load balancer. A mail server name pointing at one of these is right rather than a record to change. Comma separated; names are allowed and are resolved when the advice is worked out."
            )
            Section {
                Picker("Log level", selection: draft.logLevel) {
                    ForEach(IdentityDraft.logLevels, id: \.self) { level in
                        Text(verbatim: level).tag(level)
                    }
                }
            } footer: {
                Text("How much is written to the log. Notice is the working default; debug is a great deal and is for finding something specific, not for leaving on.")
            }
            Section {
                Text(verbatim: settings.identity.dataDirectory)
                    .font(.body.monospaced())
                    .foregroundStyle(Color(.secondaryLabel))
                    .textSelection(.enabled)
            } header: {
                Text("Data directory")
            } footer: {
                Text("Where mail and state are written. Shown and not editable here: a server restarted against a different directory does not find what is in the old one, and the mail simply is not there any more. Moving it is done with the server stopped.")
            }
        }
    }
}

/// SMTPLimitsFormView is the limits every message meets. All are read per
/// message, so a change applies to the next one.
private struct SMTPLimitsFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Limits", makeDraft: { SMTPLimitsDraft($0.smtp) }, variables: { $0.variables }) { draft, _ in
            Section {
            } footer: {
                Text("What this server accepts from whoever connects to it. Every one of these is read for each message or each connection, so a change applies to the next one.")
            }
            SettingField(
                label: "Largest message", text: draft.maxMessageSize,
                hint: "With a unit, as in the file: 50MB, 25MiB, 10485760. A message over this is refused while it is still arriving, so the sender is told rather than left to wonder."
            )
            SettingField(
                label: "Recipients per incoming message", text: draft.maxRecipientsIncomingText,
                hint: "How many addresses one arriving message may name before the rest are refused. A message to more than a handful of your addresses at once is usually somebody guessing at them.",
                keyboardType: .numberPad)
            SettingField(
                label: "Recipients per outgoing message", text: draft.maxRecipientsOutgoingText,
                hint: "The same for a message your own devices submit. It is the ceiling on how far one mistake can travel.", keyboardType: .numberPad)
            SettingField(
                label: "Greylist delay", text: draft.greylistDelay,
                hint: "How long a sender nobody here has seen before is asked to try again, as 5m or 0s. A real mail server retries; most of what does not is not worth receiving. 0s turns it off."
            )
            SettingField(
                label: "Sign-in attempts a minute", text: draft.authRateLimitText,
                hint: "How fast one address may fail to sign in before it is slowed down. This is what stands between a weak password and somebody working through a list.",
                keyboardType: .numberPad)
            SettingField(
                label: "Attempts allowed at once", text: draft.authRateBurstText,
                hint: "How many failures are allowed before that rate starts applying. Somebody mistyping their own password should not be caught by this.",
                keyboardType: .numberPad)
            SettingField(
                label: "Trusted senders", text: draft.trustedSendersText,
                hint: "Comma separated addresses or ranges, such as 10.0.0.0/8 or 192.168.1.5, that skip the checks a stranger meets. Anything here can send as anyone, so put your own machines in it and nothing else."
            )
        }
    }
}

/// ResolverFormView is how this server asks DNS questions when it checks a
/// domain.
private struct ResolverFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Resolver", makeDraft: { ResolverDraft($0.resolver) }, variables: { $0.variables }) { draft, _ in
            Section {
            } footer: {
                Text("How this server asks DNS questions when it checks what a domain has published. Read for each check, so a change applies to the next one.")
            }
            SettingField(
                label: "Resolver", text: draft.nameserver,
                hint: "As host:port, for example 1.1.1.1:53. Empty uses whatever this machine resolves with. A resolver that caches aggressively makes a record you have just published look missing for as long as it caches it."
            )
            SettingField(
                label: "Check each domain every", text: draft.checkInterval,
                hint: "As 6h or 30m. This is how quickly the DNS column on a domain notices that you have fixed a record; the Check again button does not wait for it."
            )
            SettingField(
                label: "Address services", text: draft.externalAddressServicesText,
                hint: "Comma separated URLs asked, in order, what this machine’s public address is, used to fill in the A record a domain should publish. Each is asked only if the one before it did not answer."
            )
        }
    }
}

/// SessionFormView is how long somebody stays signed in.
private struct SessionFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Sessions", makeDraft: { SessionDraft($0.session) }, variables: { $0.variables }) { draft, _ in
            Section {
            } footer: {
                Text("How long a sign-in to this dashboard lasts.")
            }
            SettingField(
                label: "A session lasts", text: draft.lifetime,
                hint: "As 720h or 30d. Shortening it does not end the sessions already issued: they were stamped with the lifetime that applied when they were made. End those on the Sessions page of your account."
            )
        }
    }
}

/// PasskeyFormView is how this server presents itself to an authenticator.
private struct PasskeyFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Passkeys", makeDraft: { PasskeyDraft($0.passkey) }, variables: { $0.variables }) { draft, _ in
            Section {
                Toggle("Allow signing in with a passkey", isOn: draft.isEnabled)
            } footer: {
                Text("How this server introduces itself to an authenticator. A passkey is bound to the name below, so changing it makes every passkey already registered useless.")
            }
            SettingField(
                label: "Bound to", text: draft.relyingPartyId,
                hint: "The domain a passkey belongs to, without a scheme or a port: mail.example.com. Empty derives it from the address the dashboard is reached at. Every passkey registered under the old value stops working."
            )
            SettingField(
                label: "Shown as", text: draft.displayName,
                hint: "What the authenticator shows the person deciding whether to allow it. Your server’s name, in words rather than as a host.",
                isMonospaced: false)
            SettingField(
                label: "Origins", text: draft.originsText,
                hint: "Comma separated origins a ceremony may come from, as https://mail.example.com. Empty derives one from the name above, which is right unless the dashboard is reached at more than one address."
            )
            SettingField(
                label: "Passkeys per account", text: draft.maximumPerUserText,
                hint: "How many an operator may register. Zero means no limit. More than one is normal: a phone and a laptop are two.",
                keyboardType: .numberPad)
        }
    }
}

/// ListenFormView is the addresses this server binds. It asks before it
/// saves, naming the dashboard's address: an operator who points it where
/// nothing can reach loses the dashboard after the restart.
private struct ListenFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(
            model: model, title: "Listeners", makeDraft: { ListenDraft($0.listen) }, variables: { $0.variables },
            confirmation: { draft in
                (
                    String(localized: "Save these addresses, with the dashboard on \(draft.https)?"),
                    String(localized: "Nothing changes until the server restarts. If the dashboard’s address is wrong you will not be able to reach this page after that, and the way back is a shell on the host and \"teanode-server config\"."),
                    String(localized: "Save the addresses")
                )
            }
        ) { draft, _ in
            Section {
            } footer: {
                Text("The addresses this server binds. Each is host:port, and an empty host means every address on the machine.")
            }
            SettingField(
                label: "Incoming mail", text: draft.smtpIncoming,
                hint: "Where mail from other servers arrives, normally :25. Changing this without changing what forwards to it stops mail arriving at all."
            )
            SettingField(
                label: "Submission", text: draft.smtpOutgoing,
                hint: "Where your own devices hand this server mail to send, normally :587. A mail client configured against the old port stops being able to send."
            )
            SettingField(
                label: "IMAP over TLS", text: draft.imaps,
                hint: "Where mail programs read mailboxes, encrypted from the first byte: port 993. Empty turns it off.", prompt: ":993")
            SettingField(
                label: "IMAP with STARTTLS", text: draft.imap,
                hint: "The same on port 143, where a program has to ask for encryption before it may sign in. Empty turns it off.", prompt: ":143")
            SettingField(
                label: "HTTP", text: draft.http,
                hint: "Normally :80. It serves the ACME http-01 challenge and redirects everything else, so turning it off stops certificates renewing when the challenge is http-01."
            )
            SettingField(
                label: "HTTPS", text: draft.https,
                hint: "Where this dashboard is served, normally :443. This is the address the dashboard is reached at.")
            SettingField(
                label: "Profiling", text: draft.debug,
                hint: "An address for Go’s profiling endpoints, off when empty. It has no authentication of its own, so bind it to 127.0.0.1 and nothing else."
            )
            RestartNote(connection: model.connection)
        }
    }
}

/// MessageStorageFormView is where mail is kept on this machine, and how
/// long a failing delivery is kept trying.
private struct MessageStorageFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "On this machine", makeDraft: { MessageStorageDraft($0.storage) }, variables: { $0.variables }) { draft, _ in
            Section {
            } footer: {
                Text("Where messages are written, and how long a delivery that keeps failing is kept.")
            }
            SettingField(
                label: "Message directory", text: draft.directory,
                hint: "Under the data directory. Changing it does not move what is already there, so the messages in the old one stop being readable from the dashboard."
            )
            SettingField(
                label: "Keep retrying for", text: draft.spoolRetention,
                hint: "As 30d or 72h. How long a delivery that keeps failing stays in the queue before it is given up on and bounced.")
            RestartNote(connection: model.connection)
        }
    }
}

/// GeoIPFormView is the optional database that says where a sender
/// connected from.
private struct GeoIPFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Where senders connect from", makeDraft: { GeoIPDraft($0.geoip) }, variables: { $0.variables }) { draft, _ in
            Section {
                Toggle("Look up where a sender connected from", isOn: draft.isEnabled)
            } footer: {
                Text("An optional MaxMind database, read from disk, that turns the address a message arrived from into a country. Nothing is sent anywhere to do it.")
            }
            SettingField(
                label: "Database file", text: draft.databaseFile,
                hint: "Path to a GeoLite2-Country.mmdb or GeoLite2-City.mmdb. Relative paths are resolved against the data directory.")
            RestartNote(connection: model.connection)
        }
    }
}

/// RelayFormView is where outgoing mail is handed when it is not delivered
/// from this machine. A provider fills in the host, port and encryption it
/// documents; the username and password are still the operator's.
private struct RelayFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Outgoing relay", makeDraft: { RelayDraft($0.relay) }, variables: { $0.variables }) { draft, settings in
            Section {
                Toggle("Send through a relay", isOn: draft.isEnabled)
            } footer: {
                Text("Hand outgoing mail to one mail server instead of delivering it by MX lookup. Almost every domestic ISP blocks outbound port 25, and a relay is reached on a submission port instead. It is also how this server sends through a provider: SES, Postmark and Resend all offer an SMTP endpoint, and the message arrives carrying the signature this server already applied.")
            }
            Section {
                Menu {
                    ForEach(RelayPreset.all) { preset in
                        Button(preset.label) { draft.wrappedValue.apply(preset) }
                    }
                } label: {
                    Label("Fill in from a provider", systemImage: "wand.and.stars")
                }
            } footer: {
                Text("Sets the host, port and encryption. The username and password are still yours to enter.")
            }
            Section {
                TextField("Host", text: draft.host, prompt: Text(verbatim: "smtp.example.net"))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                LabeledContent("Port") {
                    TextField("Port", text: Binding(get: { draft.wrappedValue.portText }, set: { draft.wrappedValue.portText = SettingsList.digits($0) }))
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                }
                Picker("Encryption", selection: draft.security) {
                    Text("STARTTLS (587, 2525)").tag("starttls")
                    Text("TLS (465)").tag("tls")
                    Text("Not required").tag("none")
                }
            }
            Section {
                TextField("Username", text: draft.username)
                    .textContentType(.username)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            SecretField(label: "Password", isStored: settings.relay.hasPassword, typed: draft.password, isClearing: draft.isClearingPassword)
        }
    }
}

/// ProxyFormView carries this server's own SMTP conversation out through
/// another address.
private struct ProxyFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Outgoing proxy", makeDraft: { ProxyDraft($0.proxy) }, variables: { $0.variables }) { draft, _ in
            Section {
            } footer: {
                Text("Carries this server’s own SMTP conversation out through another address. The relay answers the same question differently: it hands the message to somebody else’s mail server, while this one still delivers by MX lookup, just from somewhere else. Either is an answer to a blocked or blocklisted port 25, and neither needs the other.")
            }
            SettingField(
                label: "SOCKS5 proxy", text: draft.socks5, hint: "As host:port. Empty means outgoing mail leaves this machine directly.",
                prompt: "127.0.0.1:1080")
        }
    }
}

/// CredentialsNote says where AWS credentials come from when not from the
/// two fields above: a file, or the environment. Empty key fields read as
/// nothing configured, and on a deployment using either that is wrong.
private struct CredentialsNote: View {
    let credentialsFile: String?
    let hasKeys: Bool

    var body: some View {
        if !hasKeys {
            Section {
                if let credentialsFile, !credentialsFile.isEmpty {
                    LabeledContent("Credentials file") {
                        Text(verbatim: credentialsFile).font(.callout.monospaced())
                    }
                }
            } footer: {
                if let credentialsFile, !credentialsFile.isEmpty {
                    Text("Leave the keys empty: the credentials come from this file.")
                } else {
                    Text("Leave these empty to use whatever credentials the environment supplies, such as an instance role or the usual AWS variables.")
                }
            }
        }
    }
}

/// ObjectStoreFormView is where a copy of every stored message goes.
private struct ObjectStoreFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Object store", makeDraft: { ObjectStoreDraft($0.s3) }, variables: { $0.variables }) { draft, settings in
            Section {
                Toggle("Enabled", isOn: draft.isEnabled)
            } footer: {
                Text("Where a copy of every stored message goes. Needed only when more than one instance shares this database, because it is how any of them can read a message another one received.")
            }
            Section {
                TextField("Bucket", text: draft.bucket)
                TextField("Region", text: draft.region)
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            SettingField(
                label: "Endpoint", text: draft.endpoint, hint: "An S3-compatible service that is not AWS. Leave empty for AWS itself.",
                prompt: "http://minio:9000")
            Section {
                Toggle("Address the bucket as endpoint/bucket", isOn: draft.isPathStyle)
            }
            SettingField(label: "Access key ID", text: draft.accessKeyId)
            SecretField(
                label: "Secret access key", isStored: settings.s3.hasSecretAccessKey, typed: draft.secretAccessKey,
                isClearing: draft.isClearingSecret)
            CredentialsNote(
                credentialsFile: settings.s3.credentialsFile,
                hasKeys: !(settings.s3.accessKeyId ?? "").isEmpty || settings.s3.hasSecretAccessKey)
        }
    }
}

/// CertificatesFormView is what this server obtains certificates for, and
/// how.
private struct CertificatesFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Certificates", makeDraft: { CertificatesDraft($0.certificates) }, variables: { $0.variables }) { draft, settings in
            Section {
            } footer: {
                let hostsText = SettingsList.text(settings.certificates.hosts)
                Text("The server’s own certificate covers \(hostsText). Every domain is served that certificate unless it has one of its own.")
            }
            SettingField(
                label: "Names on this server’s certificate", text: draft.hostsText,
                hint: "Comma separated. The names the server’s own certificate covers: the dashboard’s address and this server’s mail server names. A wildcard such as *.example.com needs the dns-01 challenge."
            )
            Section {
                Toggle("Give each domain a certificate in its own name", isOn: draft.isPerDomain)
            } footer: {
                Text("A sender connecting to a domain is then handed a certificate for the name it asked for, rather than one naming a different domain. Each name must reach this server on port 80 for the certificate to be obtained. Turning this off stops renewals; certificates already issued stay in place until they expire.")
            }
            Section {
                Toggle("Obtain certificates automatically", isOn: draft.isAcmeEnabled)
            } footer: {
                Text("Off means this server uses only the certificate and key named below, and renewing them is your job.")
            }
            SettingField(
                label: "Contact address", text: draft.acmeEmail,
                hint: "Where the certificate authority writes if something is wrong or a certificate is about to expire without having renewed. Not published anywhere.",
                isMonospaced: false, keyboardType: .emailAddress)
            Section {
                Picker("Challenge", selection: draft.acmeChallenge) {
                    ForEach(CertificatesDraft.challenges, id: \.self) { challenge in
                        Text(verbatim: challenge).tag(challenge)
                    }
                }
            } footer: {
                Text("How ownership is proved. http-01 needs the HTTP listener reachable from the internet on port 80; dns-01 needs the Route53 solver, and is the only one that can obtain a wildcard.")
            }
            SettingField(
                label: "Authority", text: draft.acmeDirectoryUrl,
                hint: "The ACME directory URL. Empty is Let’s Encrypt. Point it at their staging directory while you are testing: its certificates are not trusted, and its rate limits are far more forgiving."
            )
            SettingField(
                label: "Certificate file", text: draft.certificateFile,
                hint: "A certificate supplied by hand, in PEM, used when certificates are not obtained automatically. Relative paths are resolved against the configuration directory."
            )
            SettingField(
                label: "Private key file", text: draft.privateKeyFile,
                hint: "The key belonging to that certificate, in PEM. Keep it readable only by the user this server runs as.")
        }
    }
}

/// Route53FormView is the solver for the dns-01 challenge.
private struct Route53FormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Route53", makeDraft: { Route53Draft($0.route53) }, variables: { $0.variables }) { draft, settings in
            Section {
                Toggle("Enabled", isOn: draft.isEnabled)
            } footer: {
                Text("Solves the dns-01 challenge using an AWS hosted zone. Needed only for a wildcard certificate, or when port 80 is not reachable.")
            }
            Section {
                TextField("Hosted zone ID", text: draft.zoneId)
                TextField("Region", text: draft.region)
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            SettingField(label: "Access key ID", text: draft.accessKeyId)
            SecretField(
                label: "Secret access key", isStored: settings.route53.hasSecretAccessKey, typed: draft.secretAccessKey,
                isClearing: draft.isClearingSecret)
            CredentialsNote(
                credentialsFile: settings.route53.credentialsFile,
                hasKeys: !(settings.route53.accessKeyId ?? "").isEmpty || settings.route53.hasSecretAccessKey)
        }
    }
}

/// ScannerFormView is virus scanning with ClamAV.
private struct ScannerFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Virus scanning", makeDraft: { ScannerDraft($0.antivirus) }, variables: { $0.variables }) { draft, _ in
            Section {
                Toggle("Enabled", isOn: draft.isEnabled)
            } footer: {
                Text("Scans incoming mail with ClamAV.")
            }
            Section {
                TextField("Host", text: draft.host)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                LabeledContent("Port") {
                    TextField("Port", text: Binding(get: { draft.wrappedValue.portText }, set: { draft.wrappedValue.portText = SettingsList.digits($0) }))
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                }
            }
        }
    }
}

/// AntispamFormView is the spam filter: which engine, and for the built-in
/// one which of its parts are on, with how far its classifier has learned
/// beside its switch.
private struct AntispamFormView: View {
    let model: ServerSettingsModel

    var body: some View {
        ServerSettingsForm(model: model, title: "Spam scoring", makeDraft: { AntispamDraft($0.antispam) }, variables: { $0.variables }) { draft, settings in
            Section {
                Toggle("Enabled", isOn: draft.isEnabled)
                Picker("Filter", selection: draft.engine) {
                    Text("Built in, scoring mail inside this server").tag("builtin")
                    Text("External SpamAssassin daemon").tag("spamd")
                }
            } footer: {
                Text("How incoming mail is scored for spam, and what does the scoring.")
            }
            if draft.wrappedValue.isSpamd {
                Section {
                    TextField("Host", text: draft.host)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    LabeledContent("Port") {
                        TextField(
                            "Port", text: Binding(get: { draft.wrappedValue.portText }, set: { draft.wrappedValue.portText = SettingsList.digits($0) })
                        )
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                    }
                }
            } else {
                Section {
                    Toggle("Signals the server already has: authentication results, reverse DNS, HELO", isOn: draft.isSignalsEnabled)
                    Toggle("Public block lists, over DNS", isOn: draft.isDnsEnabled)
                    Toggle(isOn: draft.isBayesEnabled) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Classifier trained on this server's own mail")
                            Text(verbatim: learningText(settings.antispam, minimumCount: draft.wrappedValue.bayesMinimumMessageCount))
                                .font(.footnote)
                                .foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                    LabeledContent("Messages to learn before the classifier speaks") {
                        TextField(
                            "Messages to learn before the classifier speaks",
                            text: Binding(
                                get: { draft.wrappedValue.bayesMinimumMessagesText },
                                set: { draft.wrappedValue.bayesMinimumMessagesText = SettingsList.digits($0) })
                        )
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                    }
                    Toggle("Published pattern rules, loaded with \"config rules import\"", isOn: draft.isRulesEnabled)
                } footer: {
                    Text("Four sources, each switchable. The signals cost nothing and are on for every server; the classifier learns from the messages you mark.")
                }
            }
        }
    }

    private func learningText(_ antispam: ServerSettings.Antispam, minimumCount: Int) -> String {
        let learnedText = antispam.bayesLearnedCount.formatted()
        let minimumText = minimumCount.formatted()
        return antispam.bayesLearnedCount >= minimumCount
            ? String(localized: "\(learnedText) messages learned, contributing")
            : String(localized: "\(learnedText) of \(minimumText) messages learned, silent until then")
    }
}

/// SSOFormView is the identity providers people may sign in through, a
/// group each. The client secret is written and never read back; a blank
/// keeps the stored one.
private struct SSOFormView: View {
    let model: ServerSettingsModel
    @State private var serverOrigin = ""

    var body: some View {
        ServerSettingsForm(model: model, title: "Identity providers", makeDraft: { SSODraft($0.sso) }, variables: { $0.variables }) { draft, _ in
            Section {
                if draft.wrappedValue.providers.isEmpty {
                    Text("No provider yet. Signing in is by password and passkey.").foregroundStyle(Color(.secondaryLabel))
                }
            } footer: {
                Text("People sign in through an OpenID Connect provider, such as Entra ID, Okta, Google, Keycloak or Authentik, with a button on the sign-in page for each one listed here. Their groups at the provider put them in the groups here that name them.")
            }
            ForEach(draft.wrappedValue.providers) { shown in
                let provider = Self.providerBinding(shown, in: draft)
                Section {
                    TextField("ID", text: provider.providerId, prompt: Text(verbatim: "okta"))
                        .font(.body.monospaced())
                    TextField("Button says", text: provider.name)
                    TextField("Issuer URL", text: provider.issuer, prompt: Text(verbatim: "https://login.example.net/"))
                        .font(.body.monospaced())
                        .keyboardType(.URL)
                    TextField("Client ID", text: provider.clientId)
                        .font(.body.monospaced())
                    SecureField(
                        "Client secret", text: provider.clientSecret,
                        prompt: shown.hasClientSecret ? Text("Kept as stored; type to replace") : Text("Client secret"))
                    TextField("Groups claim", text: provider.groupsClaim, prompt: Text(verbatim: "groups"))
                        .font(.body.monospaced())
                    Toggle("Create an account for anyone the provider signs in who has none here", isOn: provider.shouldCreateUsers)
                    Button("Remove", systemImage: "trash", role: .destructive) {
                        draft.wrappedValue.providers.removeAll { $0.id == shown.id }
                    }
                } header: {
                    Text(verbatim: shown.name.isEmpty ? shown.providerId : shown.name)
                } footer: {
                    let redirectAddress = shown.redirectAddress(serverOrigin: serverOrigin)
                    Text("Register this redirect URL at the provider: \(redirectAddress)")
                        .textSelection(.enabled)
                }
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            }
            Section {
                Button("Add a provider", systemImage: "plus") {
                    draft.wrappedValue.providers.append(SSOProviderDraft())
                }
            }
        }
        .task {
            let origin = await model.connection.account.server.origin
            serverOrigin = ServerWords.origin(origin)
        }
    }

    /// One provider of the list, found by its id each time, so removing
    /// another never leaves a field pointing past the end.
    private static func providerBinding(_ shown: SSOProviderDraft, in draft: Binding<SSODraft>) -> Binding<SSOProviderDraft> {
        Binding(
            get: { draft.wrappedValue.providers.first { $0.id == shown.id } ?? shown },
            set: { edited in
                if let index = draft.wrappedValue.providers.firstIndex(where: { $0.id == shown.id }) {
                    draft.wrappedValue.providers[index] = edited
                }
            })
    }
}

// MARK: - About

/// ServerAboutView is what this instance is, which version it runs, and the
/// two controls that act on the process rather than on the settings:
/// upgrade, and restart. The release check is read on its own and allowed
/// to fail on its own: a server that cannot reach the release list has an
/// old answer to one question, not a broken page.
struct ServerAboutView: View {
    let connection: Connection
    @State private var serverStatus: ServerStatus?
    @State private var upgrade: ServerUpgrade?
    @State private var isLoading = true
    @State private var isChecking = false
    @State private var isUpgrading = false
    @State private var isRestarting = false
    @State private var isConfirmingRestart = false
    @State private var isConfirmingUpgrade = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let serverStatus {
                instanceSection(serverStatus)
            }
            if let upgrade {
                upgradeSection(upgrade)
            }
            if let serverStatus {
                restartSection(serverStatus)
            }
        }
        .navigationTitle("About")
        .overlay {
            if serverStatus == nil && isLoading {
                ProgressView()
            } else if serverStatus == nil {
                ContentUnavailableView("The server status could not be read.", systemImage: "server.rack")
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private var upgradeQuestion: String {
        let latestText = upgrade?.latest ?? ""
        return String(localized: "Upgrade this server to \(latestText)?")
    }

    private func instanceSection(_ serverStatus: ServerStatus) -> some View {
        Section("This instance") {
            LabeledContent("Instance") {
                Text(verbatim: serverStatus.instance).font(.callout.monospaced()).textSelection(.enabled)
            }
            LabeledContent("Version") {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(verbatim: serverStatus.version)
                    Text(verbatim: serverStatus.shortCommit).font(.caption.monospaced()).foregroundStyle(Color(.secondaryLabel))
                }
            }
            LabeledContent("Started") {
                VStack(alignment: .trailing, spacing: 2) {
                    Text(verbatim: ServerWords.relative(serverStatus.startedAt))
                    Text(verbatim: ServerWords.fullTime(serverStatus.startedAt)).font(.caption).foregroundStyle(Color(.secondaryLabel))
                }
            }
            LabeledContent("Started by") {
                UpkeepTag(text: ServerWords.supervision(serverStatus.supervision), tone: serverStatus.isUnsupervised ? .warning : .good)
            }
        }
    }

    /// What has been released since this build and, where possible, the
    /// button. When there is no button, the reason is the thing to say: most
    /// deployments are containers, whose image is what to replace.
    @ViewBuilder
    private func upgradeSection(_ upgrade: ServerUpgrade) -> some View {
        Section {
            LabeledContent("Running") { Text(verbatim: upgrade.current) }
            LabeledContent("Available") {
                if upgrade.isAvailable {
                    UpkeepTag(text: upgrade.latest ?? "", tone: .good)
                } else if let latest = upgrade.latest, !latest.isEmpty {
                    Text("\(latest) is the newest release")
                } else {
                    Text("Not checked yet")
                }
            }
            LabeledContent("Last checked") {
                if let checkedAt = upgrade.checkedAt, !checkedAt.isEmpty {
                    Text(verbatim: ServerWords.relative(checkedAt))
                } else {
                    Text("Never")
                }
            }
            if let checkError = upgrade.checkError, !checkError.isEmpty {
                Label(checkError, systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.orange)
            }
            if upgrade.isAvailable, let url = upgrade.url, let address = URL(string: url) {
                Link(destination: address) {
                    Label("Release page", systemImage: "safari")
                }
            }
            // Shown whenever there are notes, not only when behind: a
            // development build is ahead of every release and still has a
            // version worth reading about.
            if let notes = upgrade.notes, !notes.isEmpty {
                let latestText = upgrade.latest ?? ""
                NavigationLink {
                    ReleaseNotesView(notes: notes, version: latestText)
                } label: {
                    Label("What changed in \(latestText)", systemImage: "doc.text")
                }
            }
            if let errorMessage = upgrade.errorMessage, !errorMessage.isEmpty {
                Label("The last upgrade did not finish: \(errorMessage)", systemImage: "exclamationmark.triangle")
                    .font(.footnote)
                    .foregroundStyle(.orange)
            }
            Button {
                Task { await checkForUpgrade() }
            } label: {
                Label(isChecking ? LocalizedStringKey("Checking…") : "Check now", systemImage: "arrow.triangle.2.circlepath")
            }
            .disabled(isChecking || isUpgrading || upgrade.isEnabled == false)
            if upgrade.canUpgrade {
                let latestText = upgrade.latest ?? ""
                Button {
                    isConfirmingUpgrade = true
                } label: {
                    Label(isUpgrading ? String(localized: "Upgrading…") : String(localized: "Upgrade to \(latestText)"), systemImage: "arrow.down.circle")
                }
                .disabled(isUpgrading || upgrade.isUpgrading)
                .confirmationDialog(
                    upgradeQuestion, isPresented: $isConfirmingUpgrade, titleVisibility: .visible, presenting: upgrade
                ) { upgrade in
                    Button("Download and restart") {
                        // The version this screen shows, so the upgrade installs
                        // what was agreed to rather than whatever is newest by then.
                        Task { await applyUpgrade(version: upgrade.latest) }
                    }
                } message: { _ in
                    Text("The release is downloaded and checked against the checksums published with it, and this server then restarts into it. That takes a few seconds during which mail is not accepted; senders retry. The binary it replaces is kept, so going back does not need the network.")
                }
            }
        } header: {
            Text("Version")
        } footer: {
            upgradeFooter(upgrade)
        }
    }

    @ViewBuilder
    private func upgradeFooter(_ upgrade: ServerUpgrade) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if upgrade.isEnabled == false {
                Text("Checking for releases is turned off (upgrade.enabled), so this says what was known when it was last on.")
            }
            if upgrade.isAutomatic == true {
                if let upgradeWindow = upgrade.upgradeWindow, !upgradeWindow.isEmpty {
                    Text("Automatic upgrades are on, between \(upgradeWindow) local time: a new release is installed without being asked.")
                } else {
                    Text("Automatic upgrades are on: a new release is installed without being asked.")
                }
            }
            if !upgrade.isApplicable, let refusalReason = upgrade.refusalReason, !refusalReason.isEmpty {
                Text("No upgrade from here: \(refusalReason).")
            }
            if isUpgrading || upgrade.isUpgrading {
                Text("Downloading, replacing the binary and restarting. This takes a minute.")
            }
        }
    }

    private func restartSection(_ serverStatus: ServerStatus) -> some View {
        Section {
            let pending = serverStatus.pendingRestart ?? []
            if pending.isEmpty {
                Text("Nothing has changed that a restart would pick up.").foregroundStyle(Color(.secondaryLabel))
            } else {
                let settingsText = pending.joined(separator: ", ")
                Text("Changed since this instance started, and not in use yet: \(settingsText). Restarting picks them up.")
            }
            if serverStatus.isUnsupervised {
                Label(
                    "Nothing recognizable started this process, so nothing may start it again. If you are running it by hand, it will stay down until you start it yourself.",
                    systemImage: "exclamationmark.triangle"
                )
                .foregroundStyle(.red)
            }
            if isRestarting {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Waiting for it to come back…").foregroundStyle(Color(.secondaryLabel))
                }
            }
            Button(role: .destructive) {
                isConfirmingRestart = true
            } label: {
                Label(isRestarting ? LocalizedStringKey("Restarting…") : "Restart this instance", systemImage: "arrow.clockwise")
            }
            .disabled(isRestarting)
            .confirmationDialog("Restart this instance?", isPresented: $isConfirmingRestart, titleVisibility: .visible, presenting: serverStatus) { _ in
                Button("Yes, restart it", role: .destructive) {
                    Task { await restart() }
                }
            } message: { serverStatus in
                Text("Restart \(serverStatus.instance)? Mail will not be accepted for a few seconds.")
            }
        } header: {
            Text("Restart")
        } footer: {
            Text("The process exits and whatever started it is expected to start a new one. Mail already accepted is on disk and is delivered afterwards; a sender connecting during those few seconds will try again.")
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            serverStatus = try await connection.serverStatus()
        } catch {
            problemText = error.localizedDescription
        }
        upgrade = try? await connection.serverUpgrade()
    }

    private func restart() async {
        isRestarting = true
        // The server answers and then shuts down, so a connection that
        // closes mid-reply is a success that looks like a failure. Whether
        // it worked is decided by the wait.
        try? await connection.restartServer()
        await waitForServer()
    }

    /// Waits for the server to answer again after it went away, as both the
    /// restart and the end of an upgrade do.
    private func waitForServer() async {
        let deadline = Date().addingTimeInterval(ServerWaits.restartTimeoutSeconds)
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(ServerWaits.restartPollSeconds))
            if (try? await connection.serverStatus()) != nil {
                isRestarting = false
                doneText = String(localized: "It came back.")
                await load()
                return
            }
            if Date() > deadline {
                isRestarting = false
                problemText = String(localized: "It has not answered since it was asked to restart. Something has to start it again.")
                return
            }
        }
    }

    /// The check runs in the background on the server, so this waits for
    /// an answer newer than the one before it, and only when the server
    /// says it started one: it may not, when checking is off, one is
    /// already running, or one was asked for less than a minute ago.
    private func checkForUpgrade() async {
        isChecking = true
        defer { isChecking = false }
        do {
            let started = try await connection.serverUpgrade(isChecking: true)
            if started.isChecking == true {
                let attemptedBefore = started.attemptedAt
                let deadline = Date().addingTimeInterval(ServerWaits.checkTimeoutSeconds)
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(ServerWaits.checkPollSeconds))
                    let polled = try await connection.serverUpgrade()
                    if polled.attemptedAt != attemptedBefore || Date() > deadline { break }
                }
            }
            upgrade = try await connection.serverUpgrade()
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Starts the upgrade and follows it: the download takes minutes while
    /// the server stays up, and then it restarts. A refusal answers before
    /// anything is downloaded and is said; a connection that closes is the
    /// restart beating the reply.
    private func applyUpgrade(version: String?) async {
        isUpgrading = true
        do {
            try await connection.applyServerUpgrade(version: version)
        } catch {
            if !ServerConnectionFailure.isLostConnection(error) {
                problemText = error.localizedDescription
                isUpgrading = false
                return
            }
        }
        var watch = ServerUpgradeWatch(previousVersion: upgrade?.current)
        let deadline = Date().addingTimeInterval(ServerUpgradeWatch.timeoutSeconds)
        polling: while !Task.isCancelled {
            if Date() > deadline {
                isUpgrading = false
                problemText = String(localized: "The upgrade is taking longer than it should. Check the server’s log: the new binary may already be in place and waiting for the restart to finish.")
                return
            }
            try? await Task.sleep(for: .seconds(ServerUpgradeWatch.pollSeconds))
            let upgradeVerdict: ServerUpgradeWatch.UpgradeVerdict
            do {
                let polled = try await connection.serverUpgrade()
                upgradeVerdict = watch.observe(polled)
            } catch {
                upgradeVerdict = watch.observeFailure(isLostConnection: ServerConnectionFailure.isLostConnection(error))
            }
            switch upgradeVerdict {
            case .keepWaiting:
                continue
            case .cameBackUpgraded:
                isUpgrading = false
                doneText = String(localized: "It came back.")
                await load()
                return
            case .stopped(let errorMessage):
                isUpgrading = false
                await load()
                if let errorMessage { problemText = errorMessage }
                return
            case .serverGone:
                break polling
            }
        }
        isRestarting = true
        await waitForServer()
        isUpgrading = false
    }
}

/// ReleaseNotesView is what changed in a release, as the release says it.
private struct ReleaseNotesView: View {
    let notes: String
    let version: String

    var body: some View {
        ScrollView {
            MarkdownView(text: notes)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding()
        }
        .navigationTitle(Text("What changed in \(version)"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

// MARK: - Setup

/// ServerSetupView is the dashboard's Setup tab: what an operator needs to
/// finish setting the server up. The address the outside world sees, what a
/// receiver sees when this server reaches out, and what mail clients and
/// mail programs are told to connect to.
struct ServerSetupView: View {
    let connection: Connection
    @State private var setup: ServerSetup?
    @State private var isLoading = true
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let setup {
                Section {
                } footer: {
                    Text("Everything an operator can change is in the database, and most of it is on these pages. The listeners, TLS and the database connection are the exception: those are read when the server starts, and the About page says which ones a change is waiting on.")
                }
                if let addresses = setup.addresses {
                    addressSection(addresses)
                }
                if let outgoingIdentity = setup.outgoingIdentity {
                    outgoingSection(outgoingIdentity)
                }
                clientsSection(setup)
            }
        }
        .navigationTitle("Setup")
        .overlay {
            if setup == nil && isLoading { ProgressView() }
        }
        .task { await load() }
        .refreshable { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    /// What the outside world sees this server as: the one value an operator
    /// cannot look up anywhere else, and the one every DNS record depends on.
    private func addressSection(_ addresses: ServerAddresses) -> some View {
        Section {
            if addresses.isKnown {
                if let ipv4 = addresses.ipv4, !ipv4.isEmpty {
                    LabeledContent {
                        Text(verbatim: ipv4).font(.callout.monospaced()).textSelection(.enabled)
                    } label: {
                        Text(verbatim: "A")
                    }
                }
                if let ipv6 = addresses.ipv6, !ipv6.isEmpty {
                    LabeledContent {
                        Text(verbatim: ipv6).font(.callout.monospaced()).textSelection(.enabled)
                    } label: {
                        Text(verbatim: "AAAA")
                    }
                }
            } else {
                Label {
                    VStack(alignment: .leading, spacing: 4) {
                        if let errorMessage = addresses.errorMessage, !errorMessage.isEmpty {
                            Text(verbatim: errorMessage)
                        } else {
                            Text("The external address could not be determined.")
                        }
                        Text("Point your mail host's A record at whatever public address reaches this machine.")
                    }
                } icon: {
                    Image(systemName: "exclamationmark.triangle")
                }
                .foregroundStyle(.orange)
            }
        } header: {
            Text("This server's address")
        } footer: {
            if addresses.isKnown {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Seen from the internet. Your mail host's records point here, and the MX record points at that name.")
                    if (addresses.ipv6 ?? "").isEmpty {
                        Text("No IPv6, so publish only an A record. Some large providers prefer IPv6 but none require it.")
                    }
                }
            }
        }
    }

    /// The other half of whether mail will be accepted: what a receiver sees
    /// when this server reaches out, which large receivers decide on before
    /// they read a header.
    private func outgoingSection(_ identity: OutgoingIdentity) -> some View {
        Section {
            if let uncheckableReason = identity.uncheckableReason, !uncheckableReason.isEmpty {
                Text(verbatim: uncheckableReason).foregroundStyle(Color(.secondaryLabel))
            } else {
                let address = identity.address ?? ""
                LabeledContent("Sends from") {
                    Text(verbatim: address).font(.callout.monospaced())
                }
                identityRow(
                    label: "Reverse name", shownValue: identity.reverseName, isPassing: identity.isConfirmed,
                    advice: reverseAdvice(identity))
                identityRow(
                    label: "Announces itself as", shownValue: identity.helloName, isPassing: identity.doesHelloMatch,
                    advice: helloAdvice(identity))
            }
        } header: {
            Text("How your outgoing mail identifies itself")
        } footer: {
            if (identity.uncheckableReason ?? "").isEmpty {
                if identity.via == "proxy" {
                    Text("Outgoing mail leaves through your proxy, so these are the proxy’s address and what it looks like from outside, not the address mail arrives at. Large receivers decide on these three before reading a single header.")
                } else {
                    Text("Large receivers decide on these three before reading a single header. Nothing here is about how mail reaches you: it is what a receiver sees when this server reaches out.")
                }
            }
        }
    }

    private func identityRow(label: LocalizedStringKey, shownValue: String?, isPassing: Bool, advice: String?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(label).foregroundStyle(Color(.label))
                Spacer()
                UpkeepTag(text: isPassing ? String(localized: "OK") : String(localized: "Change"), tone: isPassing ? .good : .warning)
            }
            if let shownValue, !shownValue.isEmpty {
                Text(verbatim: shownValue).font(.callout.monospaced()).foregroundStyle(Color(.label)).textSelection(.enabled)
            } else {
                Text("none").font(.callout).foregroundStyle(Color(.secondaryLabel))
            }
            if let advice {
                Text(verbatim: advice).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
        }
    }

    private func reverseAdvice(_ identity: OutgoingIdentity) -> String? {
        guard !identity.isConfirmed else { return nil }
        let address = identity.address ?? ""
        guard let reverseName = identity.reverseName, !reverseName.isEmpty else {
            return String(localized: "This address has no reverse name, and many receivers refuse mail from an address that has none. Set it where the address is managed (at a cloud provider that is the address’s own setting in the console or API, not a DNS record), then publish an A record for that name pointing at \(address).")
        }
        let resolvesText = Self.resolvesText(identity.forwardAddresses)
        return String(localized: "The reverse name is \(reverseName), but it resolves to \(resolvesText) rather than \(address), so the confirmation fails and receivers treat the mail as unverified. Publish an A record for \(reverseName) pointing at \(address), or change the reverse name to one that already does.")
    }

    private func helloAdvice(_ identity: OutgoingIdentity) -> String? {
        guard !identity.doesHelloMatch else { return nil }
        let address = identity.address ?? ""
        let helloName = identity.helloName ?? ""
        let resolvesText = Self.resolvesText(identity.helloAddresses)
        return String(localized: "This server greets other servers as \(helloName), but that name resolves to \(resolvesText) rather than \(address). Publish an A record for \(helloName) pointing at \(address), or change server.name to a host that resolves there.")
    }

    private static func resolvesText(_ addresses: [String]?) -> String {
        let joined = (addresses ?? []).joined(separator: ", ")
        return joined.isEmpty ? String(localized: "nothing") : joined
    }

    /// What a phone or a mail program is told to connect to, each opening a
    /// form for when something in front forwards a different port.
    private func clientsSection(_ setup: ServerSetup) -> some View {
        Section {
            if let submission = setup.submission {
                NavigationLink {
                    SubmissionAccessForm(connection: connection, submission: submission) { said in
                        doneText = said
                        await load()
                    }
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("What to tell a mail client").foregroundStyle(Color(.label))
                        Text(verbatim: "\(submission.effectiveHost) : \(submission.effectivePort)")
                            .font(.footnote.monospaced())
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                }
            }
            if let mailProgram = setup.mailProgram, mailProgram.isListening {
                NavigationLink {
                    MailProgramAccessForm(connection: connection, mailProgram: mailProgram) { said in
                        doneText = said
                        await load()
                    }
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Reading mail in a mail program").foregroundStyle(Color(.label))
                        let shownPort = mailProgram.effectiveTlsPort.isEmpty ? mailProgram.effectivePort : mailProgram.effectiveTlsPort
                        Text(verbatim: "\(mailProgram.effectiveHost) : \(shownPort)")
                            .font(.footnote.monospaced())
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                }
            }
        } header: {
            Text("Mail clients")
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            setup = try await connection.serverSetup()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// SubmissionAccessForm is the address a mail client is told to send
/// through. Normally it follows the server; it is wrong exactly when
/// something forwards a different port to it.
private struct SubmissionAccessForm: View {
    let connection: Connection
    let submission: SubmissionAccess
    let onSaved: @MainActor (String) async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft = SubmissionAccessDraft()
    @State private var isFilled = false
    @State private var isSaving = false
    @State private var problemText: String?

    var body: some View {
        Form {
            Section {
                Text(verbatim: "\(submission.effectiveHost) : \(submission.effectivePort)").font(.body.monospaced())
            } footer: {
                Text("The address a phone or a mail program should connect to when sending through this server, with STARTTLS. It is what the credential dialog shows.")
            }
            Section {
                TextField("Host", text: $draft.host, prompt: Text(verbatim: submission.effectiveHost))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                LabeledContent("Port") {
                    TextField("Port", text: Binding(get: { draft.portText }, set: { draft.portText = SettingsList.digits($0) }), prompt: Text(verbatim: submission.effectivePort))
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                }
            } footer: {
                Text("Leave both empty to follow the server: its own name, and the port it listens on. Set them when something forwards a different port to it, such as a container publishing one, or a firewall taking 587.")
            }
        }
        .navigationTitle("What to tell a mail client")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: isFilled && draft != SubmissionAccessDraft(submission)) { save() }
        }
        .onAppear {
            if !isFilled {
                draft = SubmissionAccessDraft(submission)
                isFilled = true
            }
        }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func save() {
        isSaving = true
        let sent = draft
        Task {
            do {
                try await connection.updateSubmissionAccess(sent)
                await onSaved(String(localized: "Saved. Credentials shown from now on will say this."))
                dismiss()
            } catch {
                problemText = error.localizedDescription
            }
            isSaving = false
        }
    }
}

/// MailProgramAccessForm is the address a mail program is told to read
/// mail at, wrong in the same way and for the same reason as the
/// submission address.
private struct MailProgramAccessForm: View {
    let connection: Connection
    let mailProgram: MailProgramAccess
    let onSaved: @MainActor (String) async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft = MailProgramAccessDraft()
    @State private var isFilled = false
    @State private var isSaving = false
    @State private var problemText: String?

    var body: some View {
        Form {
            Section {
                let shownPort = mailProgram.effectiveTlsPort.isEmpty ? mailProgram.effectivePort : mailProgram.effectiveTlsPort
                Text(verbatim: "\(mailProgram.effectiveHost) : \(shownPort)").font(.body.monospaced())
            } footer: {
                if mailProgram.effectiveTlsPort.isEmpty {
                    Text("What to type into a mail program to read this server’s mailboxes: plain, turning to TLS before signing in. It follows the server unless something in front forwards a different port.")
                } else {
                    Text("What to type into a mail program to read this server’s mailboxes: with TLS from the first byte. It follows the server unless something in front forwards a different port.")
                }
            }
            Section {
                TextField("Host", text: $draft.host, prompt: Text(verbatim: mailProgram.effectiveHost))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                LabeledContent("TLS port") {
                    TextField(
                        "TLS port", text: Binding(get: { draft.tlsPortText }, set: { draft.tlsPortText = SettingsList.digits($0) }),
                        prompt: Text(verbatim: mailProgram.effectiveTlsPort.isEmpty ? "993" : mailProgram.effectiveTlsPort)
                    )
                    .keyboardType(.numberPad)
                    .multilineTextAlignment(.trailing)
                }
                LabeledContent("STARTTLS port") {
                    TextField(
                        "STARTTLS port", text: Binding(get: { draft.portText }, set: { draft.portText = SettingsList.digits($0) }),
                        prompt: Text(verbatim: mailProgram.effectivePort.isEmpty ? "143" : mailProgram.effectivePort)
                    )
                    .keyboardType(.numberPad)
                    .multilineTextAlignment(.trailing)
                }
            } footer: {
                Text("Leave these empty to follow the server name and the ports it listens on. Fill them in when a router or a container takes the usual port on the outside and forwards it to a different one here.")
            }
        }
        .navigationTitle("Reading mail in a mail program")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: isFilled && draft != MailProgramAccessDraft(mailProgram)) { save() }
        }
        .onAppear {
            if !isFilled {
                draft = MailProgramAccessDraft(mailProgram)
                isFilled = true
            }
        }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func save() {
        isSaving = true
        let sent = draft
        Task {
            do {
                try await connection.updateMailProgramAccess(sent)
                await onSaved(String(localized: "Saved. A mail program is told this from now on."))
                dismiss()
            } catch {
                problemText = error.localizedDescription
            }
            isSaving = false
        }
    }
}

// MARK: - Everybody's agents

/// AgentAdminModel is what the agents page's screens read: which tabs the
/// person may open, the operator's agent settings, and the models the
/// providers offer, read again from what each save answers with.
@MainActor
@Observable
final class AgentAdminModel {
    let connection: Connection
    private(set) var access: OperatorAccess?
    private(set) var settings: AgentServerSettings?
    /// The models the enabled providers offer, for the model pickers.
    private(set) var knownModels: [String] = []
    private(set) var recordingModelCapabilities: [RecordingModelCapability] = []
    private(set) var isLoading = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    var tabs: [AgentAdminTab] { access?.agentAdminTabs ?? [] }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let readAccess = try await connection.operatorAccess()
            access = readAccess
            guard readAccess.canManageServer else { return }
            settings = try await connection.agentServerSettings()
            // A provider that cannot be reached contributes nothing; Test on
            // its row says why.
            if let capabilities = try? await connection.recordingModelCapabilities() {
                recordingModelCapabilities = capabilities
                knownModels = capabilities.map(\.name)
            }
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Saves part of the agent settings, keeps what the server stored, and
    /// says how it went. The models are asked again when the providers
    /// changed.
    func save(_ agentParameters: [String: JSONValue], saying said: String) async -> Bool {
        do {
            settings = try await connection.updateAgentServerSettings(agentParameters)
            doneText = said
            if agentParameters["providers"] != nil, let capabilities = try? await connection.recordingModelCapabilities() {
                recordingModelCapabilities = capabilities
                knownModels = capabilities.map(\.name)
            }
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    /// Adds what one provider said it offers to the pickers' list.
    func addKnownModels(_ names: [String]) {
        knownModels = Array(Set(knownModels + names)).sorted()
    }
}

/// AgentAdminView is the dashboard's agents page: everybody's agents with
/// their limits and switches, what they cost, every run they made, and the
/// operator's agent settings, skills and source types. Each tab is offered
/// only to whoever holds the permission it needs.
struct AgentAdminView: View {
    let connection: Connection
    @State private var model: AgentAdminModel

    init(connection: Connection) {
        self.connection = connection
        _model = State(initialValue: AgentAdminModel(connection: connection))
    }

    var body: some View {
        List {
            ForEach(model.tabs) { tab in
                tabSection(tab)
            }
        }
        .navigationTitle("Agents")
        .overlay {
            if model.access == nil && model.isLoading {
                ProgressView()
            } else if model.access != nil && model.tabs.isEmpty {
                ContentUnavailableView(
                    "Nothing to manage", systemImage: "lock",
                    description: Text("This account may not see the agents on this server."))
            }
        }
        .task { await model.load() }
        .refreshable { await model.load() }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    @ViewBuilder
    private func tabSection(_ tab: AgentAdminTab) -> some View {
        switch tab {
        case .general:
            Section("General") {
                NavigationLink { AgentListView(connection: connection) } label: { Label("Agents", systemImage: "person.2") }
                NavigationLink { AgentHouseSettingsView(model: model) } label: { Label("Personal agents", systemImage: "sparkles") }
            }
        case .usage:
            Section {
                NavigationLink { AgentUsageView(connection: connection) } label: { Label("Use and cost", systemImage: "chart.bar") }
            }
        case .runs:
            Section {
                NavigationLink { AgentRunsAdminView(connection: connection) } label: { Label("Runs", systemImage: "list.bullet.rectangle") }
            }
        case .models:
            Section("Models") {
                NavigationLink { AgentProvidersView(model: model) } label: { Label("Providers", systemImage: "cpu") }
                NavigationLink { AgentModelsFormView(model: model) } label: { Label("Models", systemImage: "square.stack.3d.up") }
                NavigationLink { AgentVoiceFormView(model: model) } label: { Label("Voice", systemImage: "waveform") }
                NavigationLink { AgentRecordingFormView(model: model) } label: { Label("Recording", systemImage: "record.circle") }
                NavigationLink { AgentFeaturesFormView(model: model) } label: { Label("What people may turn on", systemImage: "switch.2") }
                NavigationLink { AgentLimitsFormView(model: model) } label: { Label("Limits", systemImage: "gauge.with.dots.needle.67percent") }
            }
        case .tools:
            Section("Tools") {
                NavigationLink { AgentToolPolicyFormView(model: model) } label: { Label("Tool policy", systemImage: "wrench.and.screwdriver") }
                NavigationLink { AgentWebSearchFormView(model: model) } label: { Label("Web search", systemImage: "magnifyingglass") }
                NavigationLink { AgentBrowserFormView(model: model) } label: { Label("Browser", systemImage: "safari") }
                NavigationLink { ConnectedServersView(model: model) } label: { Label("Connected servers", systemImage: "server.rack") }
            }
        case .skills:
            Section {
                NavigationLink { AgentSkillsView(connection: connection) } label: { Label("Skills", systemImage: "puzzlepiece.extension") }
            }
        case .sources:
            Section("Sources") {
                NavigationLink { AgentSourceTypesView(connection: connection) } label: { Label("Source types", systemImage: "books.vertical") }
                NavigationLink { AgentFinanceFormView(model: model) } label: { Label("Finance", systemImage: "banknote") }
            }
        }
    }
}

/// AgentSettingsForm is one card of the operator's agent settings as a
/// screen: its fields filled from what is stored, and a Save in the toolbar
/// that sends the section and goes back.
private struct AgentSettingsForm<Draft: Hashable, Fields: View>: View {
    let model: AgentAdminModel
    let title: LocalizedStringKey
    let makeDraft: (AgentServerSettings) -> Draft
    let parameters: (Draft) -> [String: JSONValue]
    @ViewBuilder let fields: (Binding<Draft>, AgentServerSettings) -> Fields
    @Environment(\.dismiss) private var dismiss
    @State private var draft: Draft?
    @State private var isSaving = false

    var body: some View {
        Form {
            if let settings = model.settings, let filled = draft {
                fields(Binding(get: { draft ?? filled }, set: { draft = $0 }), settings)
            }
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if model.settings == nil { ProgressView() }
        }
        .toolbar {
            AgentSaveToolbar(isSaving: isSaving, canSave: hasChanges) { save() }
        }
        .onAppear {
            if draft == nil, let settings = model.settings { draft = makeDraft(settings) }
        }
        .onChange(of: model.settings) {
            if draft == nil, let settings = model.settings { draft = makeDraft(settings) }
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private var hasChanges: Bool {
        guard let draft, let settings = model.settings else { return false }
        return draft != makeDraft(settings)
    }

    private func save() {
        guard let draft else { return }
        isSaving = true
        let agentParameters = parameters(draft)
        Task {
            let isSaved = await model.save(agentParameters, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// AgentWords is how the agents page says the operator's settings and the
/// server's fixed words.
private enum AgentWords {
    static func featureLabel(_ feature: String) -> LocalizedStringKey {
        switch feature {
        case "triage": "Sorting: category, priority, whether a reply is needed"
        case "summaries": "Conversation summaries"
        case "draftReplies": "Draft replies on request"
        case "search": "Search by meaning"
        case "research": "Research: looking things up for a message"
        case "autoReply": "Answering mail on a person's behalf"
        case "ask": "Talking to the agent"
        case "schedules": "Reminders and scheduled runs"
        case "browser": "The browser"
        case "connectedServers": "Connected servers"
        case "computer": "The person's own computer, attached with `teanode computer`"
        case "chatApps": "Chat apps: a person's own Telegram or Discord bot"
        case "skills": "Skills: the tools an operator installs from the skill registry"
        case "subagents": "Subagents: a turn may hand a piece of work to a run of its own"
        case "remember": "Remember: file what a conversation taught once it goes quiet"
        case "knowledge": "Knowledge: read the sources people point their agent at"
        case "dreaming": "Dreaming: the runs that read, file and tidy"
        default: LocalizedStringKey(feature)
        }
    }

    static func limitLabel(_ limitName: String) -> LocalizedStringKey {
        switch limitName {
        case "dailyTokensPerAgent": "Tokens per person per day"
        case "dailyCostPerAgent": "Daily cost per person"
        case "monthlyCostPerServer": "Monthly cost, whole server"
        case "monthlyTokensPerServer": "Tokens per month, whole server (0 = no cap)"
        case "maxBodyCharacters": "Characters of a message given to a model"
        case "maxRoundsPerSubagent": "Rounds per subagent (20)"
        case "goalTurnsPerDay": "Goal turns a day (48)"
        case "goalTurnsAlone": "Goal turns without the person (24)"
        case "goalsInProgress": "Goals in progress per person (20)"
        case "backgroundWakesAlone": "Wakes by background work without the person (20)"
        case "maxRoundsPerAsk": "Rounds per conversation turn"
        case "maxRoundsPerResearch": "Rounds per research run"
        case "maxRoundsPerReply": "Rounds per reply"
        case "maxToolCallsPerRun": "Tool calls per run"
        case "maxRecordingMediaBytes": "Original recording file limit"
        case "maxRecordingAccountBytes": "Recording storage per account"
        case "requestTimeout": "Timeout per call"
        case "concurrency": "Runs at once"
        case "maxRoundsPerDream": "Rounds per dream call"
        case "scanConcurrency": "Reading calls at once"
        case "rewriteConcurrency": "Page rewrites at once"
        case "dreamShare": "Share of the day dreams may spend (0.3 by default)"
        case "ingestChunksPerRun": "Passages embedded per ingest run"
        case "embeddingTokensPerDay": "Embedding tokens per day (0 for no cap)"
        default: LocalizedStringKey(limitName)
        }
    }

    static func modelLabel(_ fieldName: String) -> LocalizedStringKey {
        switch fieldName {
        case "default": "Default"
        case "fast": "Fast"
        case "quick": "Quick"
        case "embedding": "Embedding"
        case "triage": "Sorting"
        case "research": "Research"
        case "summarize": "Summaries"
        case "reply": "Replies"
        case "ask": "Conversations"
        case "schedule": "Scheduled runs"
        case "compact": "Compaction"
        case "scan": "Reading and filing: dreams, the ingest, the remember run"
        case "synthesize": "Judgment across pages: overviews, reflections, a survey’s pages"
        case "decide": "Deciding between known answers"
        case "categorize": "Categorizing finance transactions"
        case "transcription": "Recording transcription"
        default: LocalizedStringKey(fieldName)
        }
    }

    /// What an empty assignment means, as its picker says it.
    static func modelPlaceholder(_ fieldName: String) -> String {
        switch fieldName {
        case "default", "fast", "embedding": String(localized: "None")
        case "quick": String(localized: "None: what you type starts on the conversations model")
        case "synthesize": String(localized: "Same as research (the default when research is not set)")
        case "transcription": String(localized: "Off: choose a model with speaker attribution")
        case "decide": String(localized: "None: a language model decides instead")
        case "categorize": String(localized: "Same as deciding, or the scan model when deciding is not set")
        default: String(localized: "Same as the default (fast for sorting, summaries and compaction)")
        }
    }

    static func policyWord(_ word: String) -> String {
        switch word {
        case "confirm": String(localized: "Ask first")
        case "off": String(localized: "Off")
        default: String(localized: "Allowed")
        }
    }

    static func toolCount(_ toolCount: Int) -> String {
        let countText = toolCount.formatted()
        return toolCount == 1 ? String(localized: "1 tool") : String(localized: "\(countText) tools")
    }

    static func planWindowName(_ windowMinutes: Int) -> String {
        if windowMinutes == 7 * 24 * 60 { return String(localized: "Weekly") }
        if windowMinutes == 24 * 60 { return String(localized: "Daily") }
        if windowMinutes % (24 * 60) == 0 {
            let dayText = (windowMinutes / (24 * 60)).formatted()
            return String(localized: "\(dayText)-day")
        }
        let hourText = (Double(windowMinutes) / 60).formatted(.number.precision(.fractionLength(0...1)))
        return String(localized: "\(hourText)-hour")
    }

    static func readerWord(_ reader: String) -> String {
        switch reader {
        case "files": String(localized: "reads files")
        case "journal": String(localized: "reads dated notes")
        case "sent": String(localized: "reads sent mail")
        case "web": String(localized: "reads web pages")
        case "": String(localized: "runs commands")
        default: String(localized: "unknown reader")
        }
    }

    static func usageGrouping(_ grouping: AgentUsageGrouping) -> LocalizedStringKey {
        switch grouping {
        case .day: "Day"
        case .kind: "Kind of run"
        case .model: "Model"
        case .agent: "User"
        }
    }
}

/// AgentListView is everybody with an agent: whose it is, whether it is on,
/// what it may reach and what it cost, with the two controls, a limit and a
/// switch. Tokens and counts only; what an agent read or wrote belongs to
/// the person.
private struct AgentListView: View {
    let connection: Connection
    @State private var overview: AgentAdminOverview?
    @State private var isLoading = true
    @State private var limiting: AgentAdminSummary?
    @State private var switchingOff: AgentAdminSummary?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let overview {
                Section {
                    if overview.agents.isEmpty {
                        Text("Nobody has turned on an agent yet.").foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(overview.agents) { summary in
                        row(summary)
                            .confirmationDialog(
                                "Switch this agent off?", isPresented: Binding(get: { switchingOff?.id == summary.id }, set: { if !$0 { switchingOff = nil } }),
                                titleVisibility: .visible, presenting: switchingOff
                            ) { summary in
                                Button("Switch off", role: .destructive) {
                                    Task { await setSwitchedOff(summary, isSwitchedOff: true) }
                                }
                            } message: { summary in
                                Text("\(summary.username)’s agent stops, and the work it has queued is cancelled. It can be switched on again.")
                            }
                            .swipeActions {
                                switchButton(summary)
                                Button("Daily token limit", systemImage: "pencil") { limiting = summary }
                                    .tint(.indigo)
                            }
                            .contextMenu {
                                Button("Daily token limit", systemImage: "pencil") { limiting = summary }
                                switchButton(summary)
                            }
                    }
                } footer: {
                    Text("Everyone with an agent: what it may reach and what it cost. Tokens and counts only; what an agent read or wrote belongs to the person.")
                }
            }
        }
        .navigationTitle("Agents")
        .overlay {
            if overview == nil && isLoading { ProgressView() }
        }
        .task { await load() }
        .refreshable { await load() }
        .sheet(item: $limiting) { summary in
            AgentLimitForm(connection: connection, summary: summary) { said in
                doneText = said
                await load()
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    @ViewBuilder
    private func switchButton(_ summary: AgentAdminSummary) -> some View {
        if summary.isSwitchedOff {
            Button("Switch on", systemImage: "power") {
                Task { await setSwitchedOff(summary, isSwitchedOff: false) }
            }
            .tint(.blue)
        } else {
            Button("Switch off", systemImage: "power", role: .destructive) { switchingOff = summary }
        }
    }

    private func row(_ summary: AgentAdminSummary) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(verbatim: summary.username).foregroundStyle(Color(.label))
                if summary.isSwitchedOff {
                    UpkeepTag(text: String(localized: "switched off"), tone: .warning)
                } else if summary.isEnabled {
                    UpkeepTag(text: String(localized: "on"), tone: .good)
                } else {
                    UpkeepTag(text: String(localized: "off"))
                }
            }
            Text(verbatim: details(summary)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
        }
    }

    /// The agent's name, its mailboxes, what the day came to, said the way
    /// this person's budget counts it, and its runs and jobs.
    private func details(_ summary: AgentAdminSummary) -> String {
        let grantedText = summary.grantedMailboxCount.formatted()
        let totalText = summary.sources.count.formatted()
        var parts = [summary.name, String(localized: "\(grantedText) of \(totalText) mailboxes")]
        if let today = summary.today, summary.isBoundByCost {
            let usedText = UpkeepWords.money(today.cost, currency: today.currency)
            let limitText = UpkeepWords.money(today.costLimit, currency: today.currency)
            parts.append(String(localized: "\(usedText) of \(limitText) today"))
        } else {
            let usedText = ServerWords.count(summary.today?.used ?? 0)
            let limitText = (summary.today?.limit ?? 0) > 0 ? ServerWords.count(summary.today?.limit ?? 0) : String(localized: "unlimited")
            let spentText = UpkeepWords.money(summary.today?.cost ?? 0, currency: summary.today?.currency)
            parts.append(String(localized: "\(usedText) tokens today (\(spentText)), limit \(limitText)"))
        }
        if let lastRunAt = summary.lastRunAt, !lastRunAt.isEmpty {
            let timeText = UpkeepWords.when(lastRunAt)
            parts.append(String(localized: "last run \(timeText)"))
        }
        if summary.queuedJobCount > 0 {
            let countText = summary.queuedJobCount.formatted()
            parts.append(String(localized: "\(countText) queued"))
        }
        if summary.deadJobCount > 0 {
            let countText = summary.deadJobCount.formatted()
            parts.append(String(localized: "\(countText) dead"))
        }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func setSwitchedOff(_ summary: AgentAdminSummary, isSwitchedOff: Bool) async {
        do {
            try await connection.setAgentSwitchedOff(agentId: summary.agentId, isSwitchedOff: isSwitchedOff)
            doneText = String(localized: "Done.")
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            overview = try await connection.agentAdminOverview(grouping: .day, range: .lastThirtyDays())
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AgentLimitForm sets one agent's daily limits, in tokens, in money, or
/// both; empty returns either to the server's default.
private struct AgentLimitForm: View {
    let connection: Connection
    let summary: AgentAdminSummary
    let onSaved: @MainActor (String) async -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft: AgentLimitDraft
    @State private var isSaving = false
    @State private var problemText: String?

    init(connection: Connection, summary: AgentAdminSummary, onSaved: @escaping @MainActor (String) async -> Void) {
        self.connection = connection
        self.summary = summary
        self.onSaved = onSaved
        _draft = State(initialValue: AgentLimitDraft(summary))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    LabeledContent("Daily limit") {
                        TextField(
                            "Daily limit", text: Binding(get: { draft.tokensText }, set: { draft.tokensText = SettingsList.digits($0) }),
                            prompt: Text(verbatim: tokenPrompt)
                        )
                        .keyboardType(.numberPad)
                        .multilineTextAlignment(.trailing)
                    }
                    let currencyText = (summary.today?.currency ?? "").isEmpty ? "USD" : summary.today?.currency ?? "USD"
                    LabeledContent {
                        TextField(
                            "Daily cost", text: Binding(get: { draft.costText }, set: { draft.costText = SettingsList.price($0) }),
                            prompt: Text(verbatim: costPrompt)
                        )
                        .keyboardType(.decimalPad)
                        .multilineTextAlignment(.trailing)
                    } label: {
                        Text("Daily cost (\(currencyText))")
                    }
                } footer: {
                    Text("Tokens \(summary.username)’s agent may use in a day. Empty means the server’s default.")
                }
            }
            .navigationTitle("Daily token limit")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }.disabled(isSaving)
                }
            }
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private var tokenPrompt: String {
        guard let limit = summary.today?.limit, limit > 0 else { return String(localized: "unlimited") }
        return String(limit)
    }

    private var costPrompt: String {
        guard let costLimit = summary.today?.costLimit, costLimit > 0 else { return String(localized: "unlimited") }
        return SettingsList.text(costLimit)
    }

    private func save() {
        isSaving = true
        let sent = draft
        Task {
            do {
                try await connection.setAgentLimit(agentId: summary.agentId, draft: sent)
                await onSaved(String(localized: "Limit set."))
                dismiss()
            } catch {
                problemText = error.localizedDescription
            }
            isSaving = false
        }
    }
}

/// AgentUsageView is what the agents used and cost over a range of days,
/// added up by day, kind of run, model or person, drawn and then tabled. A
/// table stays a table on a phone, scrolling sideways.
struct AgentUsageView: View {
    let connection: Connection
    @State private var grouping = AgentUsageGrouping.day
    @State private var range = AgentUsageRange.lastThirtyDays()
    @State private var overview: AgentAdminOverview?
    @State private var isLoading = true
    @State private var problemText: String?
    /// The bar touched on the chart, by the label its axis shows.
    @State private var selectedKeyLabel: String?

    private var queryId: String {
        "\(grouping.rawValue) \(range.firstDay.timeIntervalSince1970) \(range.lastDay.timeIntervalSince1970)"
    }

    var body: some View {
        List {
            Section {
                DatePicker("From", selection: $range.firstDay, in: ...range.lastDay, displayedComponents: .date)
                DatePicker("To", selection: $range.lastDay, in: range.firstDay..., displayedComponents: .date)
                Picker("By", selection: $grouping) {
                    ForEach(AgentUsageGrouping.allCases, id: \.self) { candidate in
                        Text(AgentWords.usageGrouping(candidate)).tag(candidate)
                    }
                }
            }
            if let overview {
                if overview.usageRows.isEmpty {
                    Section {
                        Text("Nothing yet.").foregroundStyle(Color(.secondaryLabel))
                    }
                } else {
                    Section {
                        chart(overview)
                    }
                    Section {
                        table(overview)
                    }
                }
            }
        }
        .navigationTitle("Use and cost")
        .overlay {
            if overview == nil && isLoading { ProgressView() }
        }
        .task(id: queryId) { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    /// A row's key as a person reads it: an agent by whose it is, never an
    /// id; the total when it has none.
    private func keyLabel(_ usageKey: String, in overview: AgentAdminOverview) -> String {
        if usageKey.isEmpty { return String(localized: "Total") }
        if grouping == .agent { return overview.username(agentId: usageKey) }
        return usageKey
    }

    /// The tokens each row used, one bar a row, all one series in one
    /// color, so it needs no legend. Touching a bar shows its row's
    /// tokens, cost and calls; the callout takes the title's place above it.
    private func chart(_ overview: AgentAdminOverview) -> some View {
        let barColor = ChartPalette.seriesColor(at: 0)
        let selectedRow = overview.usageRows.first { keyLabel($0.usageKey, in: overview) == selectedKeyLabel }
        return VStack(alignment: .leading, spacing: 8) {
            Text("Tokens used")
                .font(.subheadline)
                .foregroundStyle(Color(.secondaryLabel))
                .frame(minHeight: 44, alignment: .bottomLeading)
                .opacity(selectedRow == nil ? 1 : 0)
            Chart {
                ForEach(overview.usageRows, id: \.usageKey) { usageRow in
                    let label = keyLabel(usageRow.usageKey, in: overview)
                    BarMark(
                        x: .value("Key", label),
                        y: .value("Tokens", usageRow.totals.totalTokenCount)
                    )
                    .foregroundStyle(barColor)
                    .opacity(ChartPalette.markOpacity(isChosen: usageRow.usageKey == selectedRow?.usageKey, isAnyChosen: selectedRow != nil))
                    .accessibilityLabel(Text(verbatim: label))
                    .accessibilityValue(Text(verbatim: ServerWords.count(usageRow.totals.totalTokenCount)))
                }
                if let selectedRow {
                    ChartSelectionRule(value: keyLabel(selectedRow.usageKey, in: overview)) {
                        ChartCallout(
                            title: keyLabel(selectedRow.usageKey, in: overview),
                            figures: calloutFigures(selectedRow, isBilledShown: overview.isBilledShown, barColor: barColor))
                    }
                }
            }
            .chartXSelection(value: $selectedKeyLabel)
            .keepsSidePanelClosed()
            .chartYAxis {
                AxisMarks { axisValue in
                    AxisGridLine()
                    AxisValueLabel {
                        if let tokenCount = axisValue.as(Int.self) {
                            Text(verbatim: ServerWords.count(tokenCount))
                        }
                    }
                }
            }
            .frame(height: 200)
        }
    }

    /// The figures a touched bar shows: the billed amount only for a row a bill was read for.
    private func calloutFigures(_ selectedRow: AgentUsageRow, isBilledShown: Bool, barColor: Color) -> [ChartFigure] {
        var figures = [
            ChartFigure(label: String(localized: "Tokens"), valueText: ServerWords.count(selectedRow.totals.totalTokenCount), color: barColor),
            ChartFigure(label: isBilledShown ? String(localized: "Estimated") : String(localized: "Cost"), valueText: UpkeepWords.money(selectedRow.cost, currency: selectedRow.currency)),
        ]
        if selectedRow.billedAmount != nil {
            figures.append(ChartFigure(label: String(localized: "Billed"), valueText: Self.billedText(selectedRow)))
        }
        figures.append(ChartFigure(label: String(localized: "Calls"), valueText: selectedRow.totals.callCount.formatted()))
        return figures
    }

    /// What the provider billed for a row, or a dash where no bill was read for it.
    private static func billedText(_ usageRow: AgentUsageRow) -> String {
        guard let billedAmount = usageRow.billedAmount else { return "-" }
        return UpkeepWords.money(billedAmount, currency: usageRow.billedCurrencyCode?.uppercased())
    }

    private func table(_ overview: AgentAdminOverview) -> some View {
        ScrollView(.horizontal) {
            Grid(alignment: .trailing, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    Text(AgentWords.usageGrouping(grouping)).gridColumnAlignment(.leading)
                    Text("Prompt")
                    Text("Completion")
                    Text("Cached")
                    Text("Total")
                    if overview.isBilledShown { Text("Estimated") } else { Text("Cost") }
                    if overview.isBilledShown { Text("Billed") }
                    Text("Calls")
                }
                .font(.caption.weight(.semibold))
                .foregroundStyle(Color(.secondaryLabel))
                Divider()
                ForEach(overview.usageRows, id: \.usageKey) { usageRow in
                    GridRow {
                        Text(verbatim: keyLabel(usageRow.usageKey, in: overview)).foregroundStyle(Color(.label))
                        Text(verbatim: ServerWords.count(usageRow.totals.promptTokens))
                        Text(verbatim: ServerWords.count(usageRow.totals.completionTokens))
                        Text(verbatim: ServerWords.count(usageRow.totals.cacheReadTokens))
                        Text(verbatim: ServerWords.count(usageRow.totals.totalTokenCount))
                        Text(verbatim: UpkeepWords.money(usageRow.cost, currency: usageRow.currency))
                        if overview.isBilledShown { Text(verbatim: Self.billedText(usageRow)) }
                        Text(verbatim: usageRow.totals.callCount.formatted())
                    }
                    .font(.callout.monospacedDigit())
                    .foregroundStyle(Color(.label))
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            overview = try await connection.agentAdminOverview(grouping: grouping, range: range)
        } catch is CancellationError {
            return
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// AgentRunsAdminView is the jobs given up on, and every call any agent on
/// this server made to a model, newest first, filtered on the server. A run
/// opens as a conversation, where what is said goes to that agent as its
/// person and is logged.
private struct AgentRunsAdminView: View {
    let connection: Connection
    @State private var overview: AgentAdminOverview?
    @State private var filter = ServerAgentRunFilter()
    @State private var runs: [ServerAgentRun]?
    @State private var runTotal = 0
    @State private var isLoadingMore = false
    @State private var doneText: String?
    @State private var problemText: String?
    /// The run, or what a job given up on tried, opened from this list and pushed over it.
    @State private var openedConversationId: String?

    private static let pageSize = 50

    var body: some View {
        List {
            if let overview {
                deadJobsSection(overview)
            }
            runsSection
        }
        .navigationTitle("Runs")
        .searchable(text: $filter.query, prompt: Text("What"))
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                filterMenu
            }
        }
        .overlay {
            if runs == nil { ProgressView() }
        }
        .task { await loadOverview() }
        .task(id: filter) {
            // Typing narrows the runs as it goes, once the typing pauses.
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            await loadRuns()
        }
        .refreshable {
            await loadOverview()
            await loadRuns()
        }
        .navigationDestination(item: $openedConversationId) { conversationId in
            RunConversationView(connection: connection, conversationId: conversationId)
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private var filterMenu: some View {
        Menu {
            Picker("Whose", selection: $filter.agentId) {
                Text("Everyone").tag(String?.none)
                ForEach(overview?.agents ?? []) { summary in
                    Text(verbatim: summary.username).tag(String?.some(summary.agentId))
                }
            }
            .pickerStyle(.menu)
            Picker("Kind", selection: $filter.jobKind) {
                Text("Any kind").tag(String?.none)
                ForEach(AgentRunSummary.jobKinds, id: \.self) { jobKind in
                    Text(verbatim: UpkeepWords.runKind(jobKind)).tag(String?.some(jobKind))
                }
            }
            .pickerStyle(.menu)
        } label: {
            Label(
                "Filter",
                systemImage: filter.agentId != nil || filter.jobKind != nil
                    ? "line.3.horizontal.decrease.circle.fill" : "line.3.horizontal.decrease.circle")
        }
    }

    private func deadJobsSection(_ overview: AgentAdminOverview) -> some View {
        Section {
            if overview.deadJobs.isEmpty {
                Text("None.").foregroundStyle(Color(.secondaryLabel))
            }
            ForEach(overview.deadJobs) { job in
                let usernameText = overview.username(agentId: job.agentId)
                VStack(alignment: .leading, spacing: 3) {
                    Text("\(job.jobKind) for \(usernameText)").foregroundStyle(Color(.label))
                    Text(verbatim: deadJobDetails(job)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                .swipeActions {
                    Button("Retry", systemImage: "arrow.clockwise") {
                        Task { await retry(job) }
                    }
                    .tint(.indigo)
                }
                .contextMenu {
                    Button("Open the conversation", systemImage: "bubble.left.and.text.bubble.right") {
                        Task { await openConversation(job) }
                    }
                    Button("Runs", systemImage: "list.bullet") {
                        filter = ServerAgentRunFilter(jobId: job.id)
                    }
                    Button("Retry", systemImage: "arrow.clockwise") {
                        Task { await retry(job) }
                    }
                }
            }
        } header: {
            Text("Jobs given up on")
        } footer: {
            if !overview.deadJobs.isEmpty {
                Text("Press and hold a job to open what it tried, list its runs, or retry it.")
            }
        }
    }

    private func deadJobDetails(_ job: AgentDeadJob) -> String {
        let countText = job.attemptCount.formatted()
        let timeText = UpkeepWords.when(job.finishedAt)
        var parts = [String(localized: "after \(countText) attempts, \(timeText)")]
        if let errorMessage = job.errorMessage, !errorMessage.isEmpty { parts.append(errorMessage) }
        return parts.joined(separator: " · ")
    }

    private var runsSection: some View {
        Section {
            if filter.jobId != nil {
                Button("All runs", systemImage: "line.3.horizontal.decrease.circle") {
                    filter.jobId = nil
                }
            }
            if let runs, runs.isEmpty {
                Text("Nothing yet.").foregroundStyle(Color(.secondaryLabel))
            }
            ForEach(runs ?? []) { run in
                Button {
                    openedConversationId = run.id
                } label: {
                    runRow(run)
                }
                .onAppear {
                    if run.id == runs?.last?.id { Task { await loadMore() } }
                }
            }
            if isLoadingMore {
                ProgressView().frame(maxWidth: .infinity)
            }
        } header: {
            Text(verbatim: runCountText)
        } footer: {
            if filter.jobId != nil {
                Text("The runs of one job given up on: every call it made before it stopped, newest first.")
            } else {
                Text("Every call any agent on this server made to a model, newest first. Open one to read it; what you say there goes to that agent as its person, and is logged.")
            }
        }
    }

    private func runRow(_ run: ServerAgentRun) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: run.title).foregroundStyle(Color(.label)).lineLimit(2)
            HStack(spacing: 8) {
                if let jobKind = run.jobKind, !jobKind.isEmpty {
                    UpkeepTag(text: UpkeepWords.runKind(jobKind))
                }
                Text(verbatim: runDetails(run)).font(.caption).foregroundStyle(Color(.secondaryLabel)).lineLimit(1)
            }
        }
    }

    private func runDetails(_ run: ServerAgentRun) -> String {
        var parts = [overview?.username(agentId: run.agentId) ?? run.agentId, UpkeepWords.when(run.lastAt), ServerWords.count(run.tokenCount)]
        if run.usage.cost > 0 { parts.append(UpkeepWords.money(run.usage.cost)) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private var runCountText: String {
        let countText = runTotal.formatted()
        return runTotal == 1 ? String(localized: "1 run") : String(localized: "\(countText) runs")
    }

    private func loadOverview() async {
        do {
            overview = try await connection.agentAdminOverview(grouping: .day, range: .lastThirtyDays())
        } catch is CancellationError {
            return
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func loadRuns() async {
        do {
            let page = try await connection.serverAgentRuns(filter: filter, first: Self.pageSize)
            runs = page.runs
            runTotal = page.total
        } catch is CancellationError {
            return
        } catch {
            problemText = error.localizedDescription
            if runs == nil { runs = [] }
        }
    }

    private func loadMore() async {
        guard let loaded = runs, loaded.count < runTotal, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await connection.serverAgentRuns(filter: filter, first: Self.pageSize, offset: loaded.count)
            let knownIds = Set(loaded.map(\.id))
            runs = loaded + page.runs.filter { !knownIds.contains($0.id) }
            runTotal = page.total
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Opens what the job tried: its latest run, which is a conversation of
    /// its own, or what a remember job was about when it never got as far as
    /// a run.
    private func openConversation(_ job: AgentDeadJob) async {
        do {
            if let conversationId = try await connection.deadJobConversationId(job) {
                openedConversationId = conversationId
            } else {
                problemText = String(localized: "This job never got as far as a run.")
            }
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func retry(_ job: AgentDeadJob) async {
        do {
            try await connection.retryAgentJob(jobId: job.id)
            doneText = String(localized: "Queued again.")
            await loadOverview()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: - The operator's agent settings

/// AgentHouseSettingsView is whether people may have an agent at all, the
/// house instructions every agent here is given, and the equipment on this
/// network the agent may reach.
private struct AgentHouseSettingsView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Personal agents", makeDraft: { AgentGeneralDraft($0) }, parameters: { $0.agentParameters }) { draft, _ in
            Section {
                Toggle("Enabled: people may turn on an agent", isOn: draft.isEnabled)
            } footer: {
                Text("Model services this server may call, which model does which work, what people may turn on, and the limits. Off by default: turning it on here enables nothing for anybody, since each person turns on their own agent.")
            }
            Section {
                TextField(
                    "House instructions", text: draft.instructions,
                    prompt: Text("Standing words for every agent on this server, for example \"never answer a parent automatically\"."), axis: .vertical
                )
                .lineLimit(3...8)
            } header: {
                Text("House instructions")
            }
            SettingField(
                label: "Equipment on this network the agent may reach", text: draft.allowPrivateAddressesText,
                hint: "Addresses, ranges or names, separated by commas. The address guard refuses anything on this network by default, because a link in a message is written by whoever sent it. What is listed here is let through when a skill reaches the endpoint it declares, and for the headless browser; never for fetching a link out of mail.",
                prompt: "192.168.1.10, 10.0.0.0/24, printer.lan")
            SettingField(
                label: "Equipment whose certificate cannot be checked", text: draft.skipCertificateCheckText,
                hint: "Hosts or addresses, separated by commas. Some equipment ships a certificate issued for 127.0.0.1, which can never be valid for the address it is reached at. For a host listed here the certificate is not checked, so anything that can answer at that address can pretend to be it: the connection is still encrypted, but it is no longer proof of who answered. Only a skill reaching its own endpoint is let through this; fetching a link out of mail always checks.",
                prompt: "camera.lan, 192.168.1.10")
        }
    }
}

/// PlanUsageRows draws each window of a plan's allowance as a bar of what
/// is used, with what is left and when it starts over. The numbers are from
/// the plan's last answer, which the line under them dates.
private struct PlanUsageRows: View {
    let usage: AgentPlanUsage

    var body: some View {
        let now = Date()
        VStack(alignment: .leading, spacing: 6) {
            ForEach(Array(usage.windows.enumerated()), id: \.offset) { _, window in
                let usedPercent = window.shownUsedPercent(now: now)
                let windowText = AgentWords.planWindowName(window.windowMinutes)
                let leftText = (100 - usedPercent).formatted()
                VStack(alignment: .leading, spacing: 2) {
                    HStack {
                        Text("\(windowText): \(leftText)% left")
                        Spacer()
                        if let resetsAt = window.resetsAt, let resetDate = ServerTime.date(resetsAt) {
                            let atText = resetDate.formatted(.dateTime.weekday(.abbreviated).hour().minute())
                            Text(window.hasReset(now: now) ? LocalizedStringKey("reset since this reading, \(atText)") : "resets \(atText)")
                                .foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                    .font(.caption)
                    ProgressView(value: Double(usedPercent), total: 100)
                        .tint(usedPercent >= 90 ? .red : usedPercent >= 70 ? .orange : Color(.secondaryLabel))
                }
            }
            let observedText = ServerWords.fullTime(usage.observedAt)
            Text("As of the plan's last answer to this server, \(observedText)")
                .font(.footnote)
                .foregroundStyle(Color(.secondaryLabel))
        }
    }
}

/// ProviderEditing is which provider is being edited: one in the list, or
/// a new one.
private struct ProviderEditing: Identifiable {
    let id = UUID()
    let providerIndex: Int?
}

/// AgentProvidersView is the model services, a row each, edited one at a
/// time and saved as the editor closes. The list is sent whole whenever a
/// row changes.
private struct AgentProvidersView: View {
    let model: AgentAdminModel
    @State private var editing: ProviderEditing?
    @State private var removingIndex: Int?
    @State private var testingName: String?

    var body: some View {
        List {
            if let settings = model.settings {
                Section {
                    if settings.providers.isEmpty {
                        Text("No providers yet. Add one, and the agents on this server have a model to think with.")
                            .foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(Array(settings.providers.enumerated()), id: \.offset) { index, provider in
                        Button {
                            editing = ProviderEditing(providerIndex: index)
                        } label: {
                            row(provider)
                        }
                        .confirmationDialog(
                            "Remove the provider?", isPresented: Binding(get: { removingIndex == index }, set: { if !$0 { removingIndex = nil } }),
                            titleVisibility: .visible, presenting: removingIndex
                        ) { index in
                            Button("Remove", role: .destructive) {
                                guard let settings = model.settings else { return }
                                Task {
                                    _ = await model.save(
                                        AgentProviderDraft.agentParameters(removingProviderAt: index, in: settings), saying: String(localized: "Removed"))
                                }
                            }
                        } message: { index in
                            let nameText = model.settings?.providers[safe: index]?.name ?? ""
                            Text("Remove \(nameText)? Work assigned to its models stops until another provider offers them.")
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { removingIndex = index }
                            if provider.canBeTested {
                                Button("Test", systemImage: "checkmark.circle") {
                                    Task { await test(provider.name) }
                                }
                                .tint(.indigo)
                            }
                        }
                        .contextMenu {
                            Button("Edit", systemImage: "pencil") { editing = ProviderEditing(providerIndex: index) }
                            if provider.canBeTested {
                                Button("Test", systemImage: "checkmark.circle") {
                                    Task { await test(provider.name) }
                                }
                            }
                            Button("Remove", systemImage: "trash", role: .destructive) { removingIndex = index }
                        }
                        .disabled(testingName != nil)
                    }
                } footer: {
                    Text("A provider is a model service, named however you like; a model is always written provider:model. The openai kind covers every server that speaks the same API, including a local one.")
                }
            }
        }
        .navigationTitle("Providers")
        .overlay {
            if model.settings == nil { ProgressView() }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Add a provider", systemImage: "plus") { editing = ProviderEditing(providerIndex: nil) }
                    .disabled(model.settings == nil)
            }
        }
        .refreshable { await model.load() }
        .sheet(item: $editing) { providerEditing in
            AgentProviderForm(model: model, providerIndex: providerEditing.providerIndex)
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func row(_ provider: AgentProviderSettings) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Text(verbatim: provider.name).foregroundStyle(Color(.label))
                UpkeepTag(text: provider.providerKind)
                if let planName = provider.planUsage?.planName, !planName.isEmpty {
                    let capitalizedName = planName.prefix(1).uppercased() + planName.dropFirst()
                    UpkeepTag(text: String(localized: "\(capitalizedName) plan"))
                }
                if !provider.isEnabled {
                    UpkeepTag(text: String(localized: "off"))
                }
                if testingName == provider.name {
                    ProgressView()
                }
            }
            Text(verbatim: details(provider)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            if let planUsage = provider.planUsage, !planUsage.windows.isEmpty {
                PlanUsageRows(usage: planUsage)
            }
        }
    }

    private func details(_ provider: AgentProviderSettings) -> String {
        var parts: [String]
        if provider.isSignedInKind {
            parts = [provider.hasRefreshToken ? String(localized: "signed in") : String(localized: "not signed in")]
        } else {
            parts = [
                provider.baseUrl.isEmpty ? String(localized: "its own endpoint") : provider.baseUrl,
                provider.hasApiKey ? String(localized: "key set") : String(localized: "no key"),
            ]
        }
        if !provider.allow.isEmpty {
            let patternsText = SettingsList.text(provider.allow)
            parts.append(String(localized: "offers \(patternsText)"))
        }
        if !provider.deny.isEmpty {
            let patternsText = SettingsList.text(provider.deny)
            parts.append(String(localized: "hides \(patternsText)"))
        }
        if provider.pricingInput != 0 || provider.pricingOutput != 0 {
            let inputText = provider.pricingInput.formatted()
            let outputText = provider.pricingOutput.formatted()
            parts.append(String(localized: "\(inputText) in / \(outputText) out per million tokens"))
        }
        // The prices above are only what is left when no model's own
        // price matches, so the count of those is said too.
        let pricedCount = (provider.modelPricing ?? []).count
        if pricedCount > 0 {
            let countText = pricedCount.formatted()
            parts.append(String(localized: "\(countText) models priced apart"))
        }
        return parts.joined(separator: " · ")
    }

    private func test(_ providerName: String) async {
        testingName = providerName
        defer { testingName = nil }
        do {
            let names = try await model.connection.testAgentProvider(name: providerName)
            let countText = names.count.formatted()
            let sampleText = names.prefix(5).joined(separator: ", ")
            model.doneText = String(localized: "\(countText) models, for example \(sampleText)")
            model.addKnownModels(names)
        } catch {
            model.problemText = error.localizedDescription
        }
    }
}

extension Array {
    /// The element at an index, or nil past the end.
    fileprivate subscript(safe index: Int) -> Element? {
        indices.contains(index) ? self[index] : nil
    }
}

/// AgentProviderForm adds a provider or changes one. A provider that runs
/// on a person's plan is signed in to rather than keyed, and the sign-in is
/// what saves it.
struct AgentProviderForm: View {
    let model: AgentAdminModel
    let providerIndex: Int?

    /// The first model price's row, which the provider-prices screenshot scrolls to.
    static let modelPriceRowId = "model-price"

    @Environment(\.dismiss) private var dismiss
    @State private var draft = AgentProviderDraft()
    @State private var isFilled = false
    @State private var isSaving = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $draft.name)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Picker("Kind", selection: $draft.providerKind) {
                        ForEach(model.settings?.kinds ?? [draft.providerKind], id: \.self) { providerKind in
                            Text(verbatim: providerKind).tag(providerKind)
                        }
                    }
                }
                if draft.isSignedInKind {
                    ProviderSignInSection(
                        connection: model.connection, providerName: draft.name, isSignedIn: draft.hasRefreshToken,
                        onSignedIn: { signIn in
                            // A sign-in saves the provider on the server,
                            // creating it when it is new; what is left here
                            // would save over it.
                            model.doneText = signIn.plan.isEmpty ? String(localized: "Signed in") : String(localized: "Signed in to the \(signIn.plan) plan")
                            await model.load()
                            dismiss()
                        },
                        onProblem: { problemText = $0 })
                } else {
                    SettingField(label: "Base URL", text: $draft.baseUrl, prompt: String(localized: "empty for the public service"))
                    SecretField(label: "API key", isStored: draft.hasApiKey, typed: $draft.apiKey)
                    if draft.providerKind == "openai" {
                        SecretField(
                            label: "Billing key", isStored: draft.hasBillingApiKey, typed: $draft.billingApiKey,
                            hint: "An OpenAI admin key, used only to read what the organization was billed, so the prices below can follow the bill. Never sent with a model call.")
                    }
                }
                Section {
                    TextField("Models to offer", text: $draft.allowText, prompt: Text("Patterns, comma separated; empty offers all"))
                    TextField("Models to hide", text: $draft.denyText, prompt: Text("Patterns, comma separated"))
                }
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                Section("Price per million tokens") {
                    priceField("Input", text: $draft.pricingInputText)
                    priceField("Output", text: $draft.pricingOutputText)
                    priceField("Cache read", text: $draft.pricingCacheReadText)
                    priceField("Writing to the cache", text: $draft.pricingCacheWriteText)
                    priceField("Audio input", text: $draft.pricingAudioInputText)
                }
                modelPricesSections
                Section {
                    Toggle("Enabled", isOn: $draft.isEnabled)
                }
            }
            .navigationTitle(providerIndex == nil ? LocalizedStringKey("Add a provider") : "Edit the provider")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(!draft.canSave || isSaving || model.settings == nil)
                }
            }
            .onAppear {
                guard !isFilled else { return }
                if let providerIndex, let provider = model.settings?.providers[safe: providerIndex] {
                    draft = AgentProviderDraft(provider)
                }
                isFilled = true
            }
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func priceField(_ label: LocalizedStringKey, text: Binding<String>) -> some View {
        LabeledContent(label) {
            TextField(label, text: Binding(get: { text.wrappedValue }, set: { text.wrappedValue = SettingsList.price($0) }))
                .keyboardType(.decimalPad)
                .multilineTextAlignment(.trailing)
        }
    }

    /// The prices of particular models, a group each: the first whose model
    /// matches is the one used, so a pattern can sit under the exact names
    /// it should not cover.
    @ViewBuilder
    private var modelPricesSections: some View {
        ForEach(draft.modelPrices) { shown in
            let price = modelPriceBinding(shown)
            Section {
                TextField("Model", text: price.model, prompt: Text(verbatim: "model-name*"))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .id(Self.modelPriceRowId)
                priceField("In", text: price.inputText)
                priceField("Out", text: price.outputText)
                priceField("Cached", text: price.cacheReadText)
                priceField("Caching", text: price.cacheWriteText)
                priceField("Audio", text: price.audioInputText)
                Button("Remove", systemImage: "trash", role: .destructive) {
                    draft.modelPrices.removeAll { $0.id == shown.id }
                }
            } header: {
                Text("Prices for particular models")
            }
        }
        Section {
            Button("Price another model", systemImage: "plus") {
                draft.modelPrices.append(AgentProviderDraft.ModelPriceDraft())
            }
        } footer: {
            Text("Per million tokens, as the prices above are. The first line whose model matches is the one used, so a pattern ending in * can sit under the exact names it should not cover. Anything not matched costs what the provider costs.")
        }
    }

    private func modelPriceBinding(_ shown: AgentProviderDraft.ModelPriceDraft) -> Binding<AgentProviderDraft.ModelPriceDraft> {
        Binding(
            get: { draft.modelPrices.first { $0.id == shown.id } ?? shown },
            set: { edited in
                if let index = draft.modelPrices.firstIndex(where: { $0.id == shown.id }) { draft.modelPrices[index] = edited }
            })
    }

    private func save() {
        guard let settings = model.settings else { return }
        isSaving = true
        let agentParameters = draft.agentParameters(replacingProviderAt: providerIndex, in: settings)
        Task {
            let isSaved = await model.save(agentParameters, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

/// ProviderSignInSection signs a provider in to a person's plan: the server
/// asks the service for a one-time code, the operator types it on the
/// service's own page from any device, and this asks the server whether it
/// has been. Leaving the screen stops the asking; the code simply runs out.
private struct ProviderSignInSection: View {
    let connection: Connection
    let providerName: String
    let isSignedIn: Bool
    let onSignedIn: @MainActor (ProviderSignIn) async -> Void
    let onProblem: @MainActor (String) -> Void
    @State private var waiting: ProviderSignIn?
    @State private var isBeginning = false

    var body: some View {
        Section {
            if let waiting {
                Text("Open this page on any device, sign in to ChatGPT, and enter the code below.")
                if let address = URL(string: waiting.verificationAddress) {
                    Link(destination: address) {
                        Text(verbatim: waiting.verificationAddress)
                    }
                }
                HStack {
                    Text(verbatim: waiting.userCode)
                        .font(.title2.monospaced())
                        .textSelection(.enabled)
                    Spacer()
                    Button("Copy", systemImage: "doc.on.doc") {
                        UIPasteboard.general.string = waiting.userCode
                    }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
                }
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Waiting for the code to be entered. The code works for 15 minutes.")
                        .font(.footnote)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            } else {
                Button(isSignedIn ? LocalizedStringKey("Sign in again") : "Sign in with ChatGPT") {
                    Task { await begin() }
                }
                .disabled(isBeginning || providerName.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        } footer: {
            if waiting == nil {
                if isSignedIn {
                    Text("Signed in. Sign in again to switch account, or if requests are refused.")
                } else {
                    Text("This provider runs on your ChatGPT plan instead of an API key. Sign in with a one-time code: the provider is saved once you have. The account has to allow signing in with a code from another device.")
                }
            }
        }
        .task(id: waiting?.signInId) {
            guard let signInId = waiting?.signInId else { return }
            // Each finish waits on the server for up to half a minute, so
            // this asks again as soon as one answers that the code is still
            // unused.
            while !Task.isCancelled {
                do {
                    let finished = try await connection.finishProviderSignIn(signInId: signInId)
                    if finished.isSignedIn {
                        waiting = nil
                        await onSignedIn(finished)
                        return
                    }
                } catch is CancellationError {
                    return
                } catch {
                    if !Task.isCancelled { onProblem(error.localizedDescription) }
                    waiting = nil
                    return
                }
            }
        }
    }

    private func begin() async {
        isBeginning = true
        defer { isBeginning = false }
        do {
            waiting = try await connection.beginProviderSignIn(provider: providerName)
        } catch {
            onProblem(error.localizedDescription)
        }
    }
}

/// ModelChoiceList picks one model: what the providers offer, or a name
/// the list does not know yet.
private struct ModelChoiceList: View {
    let title: LocalizedStringKey
    /// What choosing nothing means, or nil when nothing is no choice.
    let emptyTitle: String?
    let knownModels: [String]
    @Binding var selection: String
    @Environment(\.dismiss) private var dismiss
    @State private var typedName = ""

    var body: some View {
        List {
            Section {
                TextField("provider:model", text: $typedName)
                    .font(.body.monospaced())
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .onSubmit {
                        if !typedName.trimmingCharacters(in: .whitespaces).isEmpty { choose(typedName) }
                    }
                Button("Use this name") { choose(typedName) }
                    .disabled(typedName.trimmingCharacters(in: .whitespaces).isEmpty)
            } footer: {
                Text("A model the list does not offer yet, written provider:model.")
            }
            Section {
                if let emptyTitle {
                    choiceRow(text: emptyTitle, modelName: "")
                }
                ForEach(AgentModelChoices.listed(knownModels, chosen: selection), id: \.self) { modelName in
                    choiceRow(text: modelName, modelName: modelName)
                }
            }
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
    }

    private func choiceRow(text: String, modelName: String) -> some View {
        Button {
            choose(modelName)
        } label: {
            HStack {
                Text(verbatim: text).foregroundStyle(Color(.label))
                Spacer()
                if selection == modelName {
                    Image(systemName: "checkmark").foregroundStyle(.tint)
                }
            }
        }
    }

    private func choose(_ modelName: String) {
        selection = modelName.trimmingCharacters(in: .whitespaces)
        dismiss()
    }
}

/// AgentModelsFormView assigns work to models: the default, fast, quick
/// and embedding ones, then one per kind of work, and the models a person
/// may pick for their own conversations. A deciding provider's models are
/// offered only where the server takes them.
private struct AgentModelsFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Models", makeDraft: { $0.models }, parameters: { $0.agentParameters }) { draft, settings in
            let choices = AgentModelChoices(knownModels: model.knownModels, providers: settings.providers)
            Section {
                ForEach(AgentModelAssignments.baseFieldNames, id: \.self) { fieldName in
                    modelRow(fieldName, known: choices.forWork, draft: draft)
                }
            } footer: {
                Text("Which model does which work. Default is anything not named; fast is used for sorting, summaries and compaction unless overridden; quick answers what you type first and hands anything that needs reasoning to the conversations model; embedding enables search by meaning.")
            }
            Section("Per kind of work") {
                ForEach(AgentModelAssignments.workFieldNames.filter { $0 != "transcription" }, id: \.self) { fieldName in
                    modelRow(fieldName, known: choices.forWork, draft: draft)
                }
                modelRow("decide", known: choices.forDeciding, draft: draft)
                modelRow("categorize", known: choices.forDeciding + choices.forWork, draft: draft)
            }
            Section {
                modelRow("transcription", known: model.recordingModelCapabilities.filter(\.canAttributeRecordingSpeakers).map(\.name), draft: draft)
                if let capability = model.recordingModelCapabilities.first(where: { $0.name == draft.wrappedValue.transcription }), capability.canAttributeRecordingSpeakers {
                    LabeledContent("Voice references", value: capability.hasSpeakerReferences == true ? String(localized: "Supported") : String(localized: "Unavailable"))
                    if let maximumAudioBytes = capability.maximumAudioBytes, maximumAudioBytes > 0 {
                        LabeledContent("Audio per provider request", value: ByteCountFormatter.string(fromByteCount: maximumAudioBytes, countStyle: .file))
                    }
                    if let maximumSpeakerReferenceCount = capability.maximumSpeakerReferenceCount, maximumSpeakerReferenceCount > 0 {
                        LabeledContent("Voice references per provider request", value: String(maximumSpeakerReferenceCount))
                    }
                }
            } header: { Text("Recording transcription") } footer: { Text("Empty disables file transcription. Listed choices have confirmed speaker attribution and timestamps. Chat and live-call models are not substitutes. Manually entered models are validated by the server.") }
            .id("recording-transcription")

            Section {
                ForEach(draft.wrappedValue.choices, id: \.self) { choice in
                    Text(verbatim: choice).font(.body.monospaced())
                }
                .onDelete { offsets in draft.wrappedValue.choices.remove(atOffsets: offsets) }
                NavigationLink {
                    ModelChoiceList(
                        title: "Add a model people may pick", emptyTitle: nil,
                        knownModels: choices.forWork.filter { !draft.wrappedValue.choices.contains($0) },
                        selection: Binding(
                            get: { "" },
                            set: { added in
                                if !added.isEmpty && !draft.wrappedValue.choices.contains(added) { draft.wrappedValue.choices.append(added) }
                            }))
                } label: {
                    Label("Add a model people may pick", systemImage: "plus")
                }
            } header: {
                Text("Models a person may pick")
            } footer: {
                Text("For their own conversations. None means they use the default.")
            }
        }
    }

    private func modelRow(_ fieldName: String, known: [String], draft: Binding<AgentModelAssignments>) -> some View {
        let chosen = draft.wrappedValue[fieldName: fieldName]
        let placeholder = AgentWords.modelPlaceholder(fieldName)
        return NavigationLink {
            ModelChoiceList(
                title: AgentWords.modelLabel(fieldName), emptyTitle: placeholder, knownModels: known,
                selection: Binding(get: { draft.wrappedValue[fieldName: fieldName] }, set: { draft.wrappedValue[fieldName: fieldName] = $0 }))
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(AgentWords.modelLabel(fieldName)).foregroundStyle(Color(.label))
                Text(verbatim: chosen.isEmpty ? placeholder : chosen)
                    .font(chosen.isEmpty ? .footnote : .footnote.monospaced())
                    .foregroundStyle(Color(.secondaryLabel))
            }
        }
    }
}

/// AgentVoiceFormView is talking to the agent like a phone call:
/// transcribed and read aloud by a provider of kind openai.
private struct AgentVoiceFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Voice", makeDraft: { AgentServerVoiceDraft($0.voice) }, parameters: { $0.agentParameters }) { draft, settings in
            Section {
                Toggle("Enabled", isOn: draft.isEnabled)
            } footer: {
                Text("Let people talk to their agent in the drawer, like a phone call. What they say streams through this server to the provider's realtime transcription, which hears when they start and stop; each finished utterance is sent as if typed, and the answer is read aloud with the provider's text-to-speech.")
            }
            Section {
                Picker("Provider (kind openai)", selection: draft.provider) {
                    Text("The first one").tag("")
                    ForEach(settings.providers.filter { $0.providerKind == "openai" }.map(\.name), id: \.self) { providerName in
                        Text(verbatim: providerName).tag(providerName)
                    }
                }
                TextField("Transcription model", text: draft.transcriptionModel, prompt: Text(verbatim: "gpt-4o-transcribe"))
                LabeledContent("Pause that ends a turn, ms") {
                    TextField(
                        "Pause that ends a turn, ms", text: Binding(get: { draft.wrappedValue.silenceMSText }, set: { draft.wrappedValue.silenceMSText = SettingsList.digits($0) }),
                        prompt: Text(verbatim: "500")
                    )
                    .keyboardType(.numberPad)
                    .multilineTextAlignment(.trailing)
                }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            Section {
                TextField("Model for calls (provider:model)", text: draft.askModel, prompt: Text("The one typed turns use"))
                TextField("Speech model", text: draft.speechModel, prompt: Text(verbatim: "gpt-4o-mini-tts"))
                Picker("Speech voice", selection: draft.speechVoice) {
                    Text(verbatim: "marin").tag("")
                    ForEach(AgentServerSettings.speechVoices.filter { $0 != "marin" }, id: \.self) { speechVoice in
                        Text(verbatim: speechVoice).tag(speechVoice)
                    }
                }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        }
    }
}

/// AgentRecordingFormView is what the server does with a recording while it is captured and after: the live words, and the
/// second transcription of each speaker's turn. Both are charged to the person's agent.
private struct AgentRecordingFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Recording", makeDraft: { AgentServerRecordingDraft($0.recording) }, parameters: { $0.agentParameters }) { draft, _ in
            Section {
                Toggle("Live transcription while recording", isOn: draft.isLiveTranscriptionEnabled)
            } header: {
                Text("Recording while capturing")
            } footer: {
                Text("Show words as they are spoken while a recording is being captured. The audio is also streamed through this server to the voice provider's realtime transcription, billed by the minute to the person's agent, and the words are kept only until the saved audio has been transcribed with speakers.")
            }
            Section {
                Toggle("Accurate words in recordings", isOn: draft.isAccurateWordsEnabled)
            } footer: {
                Text("Transcribe each speaker's turn a second time with the voice transcription model, which is given the names and terms from the person's own data and spells them better. Speakers and times do not change, and a person's corrections still win. The audio is charged a second time to the person's agent.")
            }
        }
    }
}

/// AgentFeaturesFormView is what people may turn on at all. A feature off
/// here is hidden from every person and refused by the API.
private struct AgentFeaturesFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(
            model: model, title: "What people may turn on", makeDraft: { $0.features }, parameters: { AgentFeatureSwitches.agentParameters($0) }
        ) { draft, _ in
            Section {
                ForEach(AgentServerSettings.featureNames, id: \.self) { feature in
                    Toggle(
                        AgentWords.featureLabel(feature),
                        isOn: Binding(
                            get: { AgentFeatureSwitches.isOn(feature, in: draft.wrappedValue) }, set: { draft.wrappedValue[feature] = $0 }))
                }
            } footer: {
                Text("A feature off here is hidden from every person and refused by the API; what they had on resumes when it comes back.")
            }
        }
    }
}

/// AgentLimitsFormView is the caps on what one person and the whole server
/// may spend, how far one run may go, what unattended work may take, and
/// how long transcripts and corrections are kept.
private struct AgentLimitsFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Limits", makeDraft: { AgentLimitsDraft($0) }, parameters: { $0.agentParameters }) { draft, _ in
            Section {
            } footer: {
                Text("Caps on what one person, and the whole server, may spend in a day and a month; how far one run may go; how long transcripts and corrections are kept.")
            }
            ForEach(Array(AgentLimitsDraft.limitGroups.enumerated()), id: \.offset) { _, limitNames in
                Section {
                    ForEach(limitNames, id: \.self) { limitName in
                        limitField(limitName, draft: draft)
                    }
                    if limitNames.contains("dailyCostPerAgent") {
                        LabeledContent("Currency") {
                            TextField(
                                "Currency", text: Binding(get: { draft.wrappedValue.currency }, set: { draft.wrappedValue.currency = String($0.uppercased().prefix(3)) }),
                                prompt: Text(verbatim: "USD")
                            )
                            .multilineTextAlignment(.trailing)
                            .textInputAutocapitalization(.characters)
                        }
                    }
                    if limitNames.contains("requestTimeout") {
                        LabeledContent("Keep run transcripts for") {
                            TextField("Keep run transcripts for", text: draft.retention.runs).multilineTextAlignment(.trailing)
                        }
                        LabeledContent("Keep corrections for") {
                            TextField("Keep corrections for", text: draft.retention.corrections).multilineTextAlignment(.trailing)
                        }
                    }
                } footer: {
                    if limitNames.contains("maxRecordingMediaBytes") {
                        Text("Sizes accept units such as GB. Zero uses 2 GB per original and 64 GB per account. Pending uploads and files awaiting deletion count toward storage.")
                    }
                    if limitNames.contains("maxRoundsPerSubagent") {
                        Text("For work nobody is watching. Zero is the default shown in brackets.")
                    }
                }
                .id(limitNames.contains("maxRecordingMediaBytes") ? "recording-quotas" : (limitNames.first ?? ""))
            }
        }
    }

    private func limitField(_ limitName: String, draft: Binding<AgentLimitsDraft>) -> some View {
        LabeledContent(AgentWords.limitLabel(limitName)) {
            TextField(
                AgentWords.limitLabel(limitName),
                text: Binding(get: { draft.wrappedValue.limitTexts[limitName] ?? "" }, set: { draft.wrappedValue.limitTexts[limitName] = $0 })
            )
            .keyboardType((limitName == "requestTimeout" || limitName.hasPrefix("maxRecording")) ? .default : .decimalPad)
            .multilineTextAlignment(.trailing)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
        }
    }
}

/// AgentToolPolicyFormView is every tool the agent can be given, by family,
/// and one word for each: allowed, ask first, or off. A family's word covers
/// its tools unless one says otherwise.
private struct AgentToolPolicyFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Tool policy", makeDraft: { AgentToolPolicyDraft($0) }, parameters: { $0.agentParameters }) { draft, settings in
            Section {
            } footer: {
                Text("What the agent may reach for, by family and by tool. Off is never offered; ask first raises a tool to a confirmation card every time; a tool marked as always asking does so by its risk whatever is set here. A family's word covers its tools unless one says otherwise.")
            }
            ForEach(settings.families, id: \.self) { family in
                let members = settings.tools.catalog.filter { $0.family == family }
                let familyWord = draft.wrappedValue.word(family)
                Section {
                    Picker(selection: wordBinding(family, draft: draft)) {
                        ForEach(AgentToolPolicyDraft.words, id: \.self) { word in
                            Text(verbatim: AgentWords.policyWord(word)).tag(word)
                        }
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: family).foregroundStyle(Color(.label))
                            Text(verbatim: AgentWords.toolCount(members.count)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                    // Its tools on a screen of their own, each a full row,
                    // rather than folded into this one.
                    NavigationLink {
                        Form {
                            Section {
                                ForEach(members) { tool in
                                    toolRow(tool, familyWord: familyWord, draft: draft)
                                }
                            } footer: {
                                if members.isEmpty || family == "servers" {
                                    Text("Connected servers add their own tools to this family as they connect, named the way they name themselves; the family word covers all of them.")
                                }
                            }
                        }
                        .navigationTitle(Text(verbatim: family))
                    } label: {
                        Text("Each tool")
                    }
                }
            }
        }
    }

    private func wordBinding(_ name: String, draft: Binding<AgentToolPolicyDraft>) -> Binding<String> {
        Binding(get: { draft.wrappedValue.word(name) }, set: { draft.wrappedValue.wordByName[name] = $0 })
    }

    /// One tool: its name, what it does and the verbs it takes, and its
    /// word, which as allowed follows the family's.
    private func toolRow(_ tool: AgentTool, familyWord: String, draft: Binding<AgentToolPolicyDraft>) -> some View {
        Picker(selection: wordBinding(tool.name, draft: draft)) {
            ForEach(AgentToolPolicyDraft.words, id: \.self) { word in
                if word == "allow" && familyWord != "allow" {
                    let familyText = AgentWords.policyWord(familyWord)
                    Text("As the family: \(familyText)").tag(word)
                } else {
                    Text(verbatim: AgentWords.policyWord(word)).tag(word)
                }
            }
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(verbatim: tool.name).foregroundStyle(Color(.label))
                    if tool.confirms {
                        Text("always asks").font(.caption).foregroundStyle(Color(.secondaryLabel))
                    }
                }
                if !tool.description.isEmpty {
                    Text(verbatim: tool.description).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                }
                if let actions = tool.actions, !actions.isEmpty {
                    Text(verbatim: actions.joined(separator: " · ")).font(.caption.monospaced()).foregroundStyle(Color(.secondaryLabel))
                }
            }
        }
    }
}

/// AgentWebSearchFormView is the service web_search asks.
private struct AgentWebSearchFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Web search", makeDraft: { AgentWebSearchDraft($0.search) }, parameters: { $0.agentParameters }) { draft, settings in
            Section {
                Picker("Provider", selection: draft.searchKind) {
                    Text("None, no web search").tag("")
                    Text(verbatim: "Brave").tag("brave")
                }
            } footer: {
                Text("The service web_search asks. Without one the agent can still fetch a page it is given.")
            }
            SecretField(label: "API key", isStored: settings.search.hasApiKey, typed: draft.apiKey)
        }
    }
}

/// AgentBrowserFormView is the headless Chrome beside the server.
private struct AgentBrowserFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Browser", makeDraft: { AgentBrowserDraft($0.browser) }, parameters: { $0.agentParameters }) { draft, _ in
            Section {
                Toggle("Enabled", isOn: draft.isEnabled)
            } footer: {
                Text("A headless Chrome the operator runs beside the server, reached over the DevTools protocol. Every run starts as a clean visitor.")
            }
            Section {
                TextField("DevTools endpoint", text: draft.cdpEndpoint, prompt: Text(verbatim: "chrome:9222"))
                LabeledContent("Idle timeout") {
                    TextField("Idle timeout", text: draft.idleTimeout).multilineTextAlignment(.trailing)
                }
                LabeledContent("Contexts at once") {
                    TextField(
                        "Contexts at once", text: Binding(get: { draft.wrappedValue.maxContextsText }, set: { draft.wrappedValue.maxContextsText = SettingsList.digits($0) })
                    )
                    .keyboardType(.numberPad)
                    .multilineTextAlignment(.trailing)
                }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            Section {
                Toggle("People may attach their own browser tab through the extension", isOn: draft.canAttachTabs)
            }
        }
    }
}

/// AgentFinanceFormView is which services people may link their banks,
/// cards, brokerages and loans through, and the operator's Plaid account.
/// Plaid is offered only once its keys are set.
private struct AgentFinanceFormView: View {
    let model: AgentAdminModel

    var body: some View {
        AgentSettingsForm(model: model, title: "Finance", makeDraft: { AgentFinanceDraft($0.finance) }, parameters: { $0.agentParameters }) { draft, settings in
            Section {
                Toggle(
                    "Plaid: needs your Plaid developer account below",
                    isOn: Binding(get: { draft.wrappedValue.offeredProviders.contains("plaid") }, set: { draft.wrappedValue.setOffered("plaid", isOffered: $0) }))
                Toggle(
                    "SimpleFIN: each person pays for the bridge and pastes their own token",
                    isOn: Binding(
                        get: { draft.wrappedValue.offeredProviders.contains("simplefin") }, set: { draft.wrappedValue.setOffered("simplefin", isOffered: $0) }))
            } footer: {
                Text("Which services people may link their banks, cards, brokerages and loans through. Each person links their own; nothing is linked from here.")
            }
            if draft.wrappedValue.isOfferingPlaid {
                Section {
                    Picker("Environment", selection: draft.environment) {
                        Text("Not chosen").tag("")
                        Text("Sandbox: test institutions").tag("sandbox")
                        Text("Production: real institutions").tag("production")
                    }
                    TextField("Client ID", text: draft.clientId)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } footer: {
                    Text("Your Plaid developer account. Plaid is offered to people only once the client ID and the secret are both set.")
                }
                SecretField(label: "Secret", isStored: settings.finance.plaid.hasSecret, typed: draft.secret)
                SettingField(
                    label: "Country codes", text: Binding(get: { draft.wrappedValue.countryCodesText }, set: { draft.wrappedValue.countryCodesText = $0.uppercased() }),
                    hint: "Two letters, separated by commas; empty means US.", prompt: "US")
                Section {
                    Toggle("Transactions (always)", isOn: .constant(true)).disabled(true)
                    Toggle(
                        "Investments: holdings and trades in brokerage and retirement accounts",
                        isOn: Binding(get: { draft.wrappedValue.hasProduct("investments") }, set: { draft.wrappedValue.setProduct("investments", isOn: $0) }))
                    Toggle(
                        "Liabilities: what is owed on loans and cards",
                        isOn: Binding(get: { draft.wrappedValue.hasProduct("liabilities") }, set: { draft.wrappedValue.setProduct("liabilities", isOn: $0) }))
                } footer: {
                    Text("What a finance source is linked with. Investments and liabilities are asked for only where the institution has them. On paid plans Plaid charges for each product, per finance source.")
                }
            }
        }
    }
}

/// ServerEditing is which connected server is being edited: one in the
/// list, or a new one.
private struct ServerEditing: Identifiable {
    let id = UUID()
    let serverIndex: Int?
}

/// ConnectedServersView is the connected servers the operator declares, a
/// row each, edited one at a time because a server has a dozen fields and
/// most of them are read once.
private struct ConnectedServersView: View {
    let model: AgentAdminModel
    @State private var editing: ServerEditing?
    @State private var removingIndex: Int?

    var body: some View {
        List {
            if let settings = model.settings {
                Section {
                    if settings.mcpServers.isEmpty {
                        Text("No connected servers. Add one to give every agent its tools.").foregroundStyle(Color(.secondaryLabel))
                    }
                    ForEach(Array(settings.mcpServers.enumerated()), id: \.offset) { index, server in
                        Button {
                            editing = ServerEditing(serverIndex: index)
                        } label: {
                            row(server)
                        }
                        .confirmationDialog(
                            "Remove the server?", isPresented: Binding(get: { removingIndex == index }, set: { if !$0 { removingIndex = nil } }),
                            titleVisibility: .visible, presenting: removingIndex
                        ) { index in
                            Button("Remove", role: .destructive) {
                                guard let settings = model.settings else { return }
                                Task {
                                    _ = await model.save(
                                        ConnectedServerDraft.agentParameters(removingServerAt: index, in: settings), saying: String(localized: "Removed"))
                                }
                            }
                        } message: { index in
                            let nameText = model.settings?.mcpServers[safe: index]?.name ?? ""
                            Text("Remove \(nameText)? Everyone connected to it loses its tools.")
                        }
                        .swipeActions {
                            Button("Remove", systemImage: "trash", role: .destructive) { removingIndex = index }
                        }
                        .contextMenu {
                            Button("Edit", systemImage: "pencil") { editing = ServerEditing(serverIndex: index) }
                            Button("Remove", systemImage: "trash", role: .destructive) { removingIndex = index }
                        }
                    }
                } footer: {
                    Text("Servers speaking the Model Context Protocol. Their tools join the catalog as mcp__server__tool and need confirmation unless listed as read-only.")
                }
            }
        }
        .navigationTitle("Connected servers")
        .overlay {
            if model.settings == nil { ProgressView() }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Add a server", systemImage: "plus") { editing = ServerEditing(serverIndex: nil) }
                    .disabled(model.settings == nil)
            }
        }
        .refreshable { await model.load() }
        .sheet(item: $editing) { serverEditing in
            ConnectedServerForm(model: model, serverIndex: serverEditing.serverIndex)
        }
        .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
    }

    private func row(_ server: ConnectedServerSettings) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(verbatim: server.name).foregroundStyle(Color(.label))
                UpkeepTag(text: server.effectiveTransport)
                if server.isHeadless { UpkeepTag(text: String(localized: "headless")) }
                if !server.isEnabled { UpkeepTag(text: String(localized: "off")) }
            }
            Text(verbatim: details(server)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
        }
    }

    private func details(_ server: ConnectedServerSettings) -> String {
        let authText = server.effectiveAuth.isEmpty ? "none" : server.effectiveAuth
        var parts = [
            server.effectiveTransport == "stdio" ? ([server.command] + server.args).joined(separator: " ") : server.url,
            String(localized: "auth \(authText)"),
        ]
        if !server.timeout.isEmpty {
            let timeoutText = server.timeout
            parts.append(String(localized: "timeout \(timeoutText)"))
        }
        if !server.readOnly.isEmpty {
            let toolsText = SettingsList.text(server.readOnly)
            parts.append(String(localized: "read-only: \(toolsText)"))
        }
        if !server.disabled.isEmpty {
            let toolsText = SettingsList.text(server.disabled)
            parts.append(String(localized: "disabled: \(toolsText)"))
        }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// ConnectedServerForm adds a connected server or changes one. Which fields
/// it asks for follows how the server is reached and how it signs in.
private struct ConnectedServerForm: View {
    let model: AgentAdminModel
    let serverIndex: Int?
    @Environment(\.dismiss) private var dismiss
    @State private var draft = ConnectedServerDraft()
    @State private var isFilled = false
    @State private var isSaving = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $draft.name)
                    Picker("Transport", selection: $draft.transport) {
                        Text("inferred").tag("")
                        Text(verbatim: "http").tag("http")
                        Text(verbatim: "stdio").tag("stdio")
                    }
                    if draft.asksLocation {
                        Picker("Where the command runs", selection: Binding(get: { draft.location }, set: { draft.setLocation($0) })) {
                            Text("On this server").tag("")
                            Text("On the person's attached computer").tag("computer")
                        }
                    }
                }
                if draft.showsUrl {
                    Section("URL (http)") {
                        TextField("URL (http)", text: $draft.url, prompt: Text(verbatim: "https://"))
                            .keyboardType(.URL)
                    }
                }
                if draft.showsCommand {
                    Section {
                        TextField("Command (stdio)", text: $draft.command)
                        TextField("Arguments", text: $draft.argsText)
                        if draft.isStdio {
                            TextField("Working directory", text: $draft.workingDir)
                        }
                    } header: {
                        Text("Command (stdio)")
                    }
                    if draft.isStdio {
                        Section {
                            TextField("Environment", text: $draft.envText, axis: .vertical)
                                .font(.body.monospaced())
                                .lineLimit(2...6)
                        } header: {
                            Text("Environment")
                        } footer: {
                            Text("NAME=value per line; a blank value keeps what is stored.")
                        }
                    }
                }
                Section {
                    Picker("Auth", selection: $draft.auth) {
                        Text("inferred").tag("")
                        Text("none").tag("none")
                        Text("static: one header for everyone").tag("static")
                        Text("user: each person gives a credential").tag("user")
                        Text("OAuth: each person signs in").tag("oauth")
                    }
                    LabeledContent("Timeout") {
                        TextField("Timeout", text: $draft.timeout, prompt: Text(verbatim: "30s")).multilineTextAlignment(.trailing)
                    }
                }
                if draft.showsAuthorization {
                    SecretField(label: "Authorization header (static)", isStored: draft.hasAuthorization, typed: $draft.authorization)
                }
                if draft.isOAuth {
                    Section("OAuth") {
                        TextField("OAuth client id", text: $draft.oauthClientId)
                        TextField("OAuth scopes", text: $draft.oauthScopesText)
                        TextField("Authorization URL", text: $draft.oauthAuthorizationUrl, prompt: Text("discovered from the server when empty"))
                        TextField("Token URL", text: $draft.oauthTokenUrl, prompt: Text("discovered from the server when empty"))
                        Picker("Authorization comes back to", selection: $draft.oauthRedirect) {
                            Text("This server's address").tag("")
                            Text("Your attached computer (for services that only accept a loopback address)").tag("computer")
                        }
                    }
                    SecretField(label: "OAuth client secret", isStored: draft.hasOauthClientSecret, typed: $draft.oauthClientSecret)
                }
                Section {
                    TextField("Read-only tools", text: $draft.readOnlyText)
                    TextField("Disabled tools", text: $draft.disabledText)
                }
                Section {
                    if draft.canBeHeadless {
                        Toggle("Usable with nobody present", isOn: $draft.isHeadless)
                    }
                    Toggle("Enabled", isOn: $draft.isEnabled)
                }
            }
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .navigationTitle(serverIndex == nil ? LocalizedStringKey("Add a server") : "Edit the server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(!draft.canSave || isSaving || model.settings == nil)
                }
            }
            .onAppear {
                guard !isFilled else { return }
                if let serverIndex, let server = model.settings?.mcpServers[safe: serverIndex] {
                    draft = ConnectedServerDraft(server)
                }
                isFilled = true
            }
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func save() {
        guard let settings = model.settings else { return }
        isSaving = true
        let agentParameters = draft.agentParameters(replacingServerAt: serverIndex, in: settings)
        Task {
            let isSaved = await model.save(agentParameters, saying: String(localized: "Saved"))
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}

// MARK: - Skills

/// AgentSkillsModel is what the skills screens read: the skills installed,
/// which of the operator's keys are filled in, and what the registry
/// offers, read again after every change.
@MainActor
@Observable
final class AgentSkillsModel {
    let connection: Connection
    private(set) var installed: [InstalledSkill]?
    private(set) var offers: [RegistryOffer]?
    /// The operator's keys that are filled in, as "skill/key".
    private(set) var filledSecretNames: Set<String> = []
    /// The skill a change is being made to.
    private(set) var busyName: String?
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    func skill(_ skillName: String) -> InstalledSkill? { installed?.first { $0.name == skillName } }

    func load() async {
        do {
            installed = try await connection.installedSkills()
        } catch {
            problemText = error.localizedDescription
            if installed == nil { installed = [] }
        }
        // Somebody who installs skills but does not manage the server sees
        // the keys without whether they are filled.
        if let filled = try? await connection.filledSkillSecrets() { filledSecretNames = filled }
    }

    /// The registry is read as the screen opens: what it offers is the way
    /// to find a skill.
    func loadRegistry() async {
        do {
            offers = try await connection.skillRegistry()
        } catch {
            problemText = String(localized: "Could not reach the skill registry")
            if offers == nil { offers = [] }
        }
    }

    /// Runs a change to one skill, reads everything again, and says how it
    /// went in a toast beside the button that caused it.
    @discardableResult
    func act(_ skillName: String, saying said: String, _ work: () async throws -> Void) async -> Bool {
        busyName = skillName
        defer { busyName = nil }
        do {
            try await work()
            await load()
            if offers != nil { offers = try? await connection.skillRegistry() }
            doneText = said
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }
}

/// AgentSkillsView is the skills installed on this server, each opening to
/// what it brings and the controls for it, and what the registry offers,
/// filtered by what is typed. Uploading a skill package is done on the
/// dashboard.
private struct AgentSkillsView: View {
    let connection: Connection
    @State private var skills: AgentSkillsModel
    @State private var filterText = ""

    init(connection: Connection) {
        self.connection = connection
        _skills = State(initialValue: AgentSkillsModel(connection: connection))
    }

    var body: some View {
        List {
            Section {
                if let installed = skills.installed, installed.isEmpty {
                    Text("No skills are installed.").foregroundStyle(Color(.secondaryLabel))
                }
                ForEach(skills.installed ?? []) { skill in
                    NavigationLink {
                        InstalledSkillView(skills: skills, skillName: skill.name)
                    } label: {
                        SkillRow(skill: skill)
                    }
                }
            } header: {
                Text("Skills")
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Tools that arrive without a release. A skill is a file of declarations, fetched from a registry that signs what it publishes; what it declares becomes tools for everybody here, under the same tool policy as the rest. A skill that runs commands runs them on a computer the person attached, never on this server.")
                    if RegistryOffer.isBehind(installedCount: skills.installed?.count ?? 0, offers: skills.offers ?? []) {
                        Text("The registry has a newer version of something installed.")
                    }
                }
            }
            RegistrySection(
                title: "What the registry offers",
                description: "Every skill the registry publishes, with its signature checked against the key this server ships with.",
                offers: skills.offers, filterText: filterText, busyName: skills.busyName
            ) { offer in
                Task {
                    let versionText = offer.version
                    await skills.act(offer.name, saying: String(localized: "Installed \(offer.name) \(versionText)")) {
                        try await connection.installSkill(name: offer.name)
                    }
                }
            }
        }
        .navigationTitle("Skills")
        .searchable(text: $filterText, prompt: Text("Filter the registry"))
        .overlay {
            if skills.installed == nil { ProgressView() }
        }
        .task {
            await skills.load()
            await skills.loadRegistry()
        }
        .refreshable {
            await skills.load()
            await skills.loadRegistry()
        }
        .toast(done: Bindable(skills).doneText, problem: Bindable(skills).problemText)
    }
}

/// SkillRow is an installed skill in a line or two: its name and state,
/// and what it brings and asks for.
private struct SkillRow: View {
    let skill: InstalledSkill

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(verbatim: skill.name).foregroundStyle(Color(.label))
                UpkeepTag(text: skill.version)
                if skill.doesRunCommands { UpkeepTag(text: String(localized: "runs commands")) }
                if !skill.isReadable { UpkeepTag(text: String(localized: "cannot be read"), tone: .bad) }
                if !skill.isEnabled { UpkeepTag(text: String(localized: "off")) }
            }
            Text(verbatim: details).font(.footnote).foregroundStyle(Color(.secondaryLabel)).lineLimit(3)
        }
    }

    private var details: String {
        var parts = [skill.description]
        if !skill.tools.isEmpty {
            let toolsText = skill.tools.map(\.name).joined(separator: ", ")
            parts.append(String(localized: "brings \(toolsText)"))
        }
        if !skill.secrets.isEmpty {
            let keysText = skill.secrets.joined(separator: ", ")
            parts.append(String(localized: "needs \(keysText)"))
        }
        if !skill.personalSecrets.isEmpty {
            let keysText = skill.personalSecrets.joined(separator: ", ")
            parts.append(String(localized: "needs \(keysText) from each person, set on their own agent page"))
        }
        if !skill.isReadable { parts.append(skill.problem ?? "") }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// RegistrySection is what a registry offers, a skill or a source type a
/// row, each with the button that installs it, updates it, or says it is
/// installed.
private struct RegistrySection: View {
    let title: LocalizedStringKey
    let description: LocalizedStringKey
    let offers: [RegistryOffer]?
    let filterText: String
    let busyName: String?
    let install: (RegistryOffer) -> Void

    var body: some View {
        Section {
            if offers == nil {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Asking the registry…").foregroundStyle(Color(.secondaryLabel))
                }
            } else if let offers, offers.isEmpty {
                Text("The registry offers nothing.").foregroundStyle(Color(.secondaryLabel))
            }
            ForEach((offers ?? []).filter { $0.matches(filterText) }) { offer in
                HStack(alignment: .top, spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 8) {
                            Text(verbatim: offer.name).foregroundStyle(Color(.label))
                            UpkeepTag(text: offer.version)
                        }
                        if !offer.description.isEmpty {
                            Text(verbatim: offer.description).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                        }
                        if !offer.tags.isEmpty {
                            Text(verbatim: offer.tags.joined(separator: " · ")).font(.caption).foregroundStyle(Color(.secondaryLabel))
                        }
                    }
                    Spacer()
                    if busyName == offer.name {
                        ProgressView()
                    } else {
                        Button(buttonTitle(offer)) { install(offer) }
                            .buttonStyle(.bordered)
                            .buttonBorderShape(.capsule)
                            .disabled(offer.isCurrent || busyName != nil)
                    }
                }
            }
        } header: {
            Text(title)
        } footer: {
            Text(description)
        }
    }

    private func buttonTitle(_ offer: RegistryOffer) -> String {
        if offer.isNewer {
            let versionText = offer.version
            return String(localized: "Update to \(versionText)")
        }
        return offer.isInstalled ? String(localized: "Installed") : String(localized: "Install")
    }
}

/// InstalledSkillView is one skill: what it brings, the keys it asks for
/// and who fills them in, whether it is offered, and removing it.
private struct InstalledSkillView: View {
    let skills: AgentSkillsModel
    let skillName: String
    @Environment(\.dismiss) private var dismiss
    @State private var isFillingKeys = false
    @State private var isRemoving = false

    var body: some View {
        List {
            if let skill = skills.skill(skillName) {
                Section {
                    if !skill.description.isEmpty {
                        Text(verbatim: skill.description).foregroundStyle(Color(.label))
                    }
                    LabeledContent("Version") { Text(verbatim: skill.version) }
                    if !skill.publisher.isEmpty {
                        LabeledContent("Publisher") { Text(verbatim: skill.publisher) }
                    }
                    if !skill.isReadable {
                        Label(skill.problem ?? String(localized: "cannot be read"), systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.red)
                    }
                }
                if !skill.tools.isEmpty {
                    Section("Tools") {
                        ForEach(skill.tools, id: \.name) { tool in
                            VStack(alignment: .leading, spacing: 2) {
                                HStack(spacing: 6) {
                                    Text(verbatim: tool.name).font(.callout.monospaced()).foregroundStyle(Color(.label))
                                    if tool.doesNeedComputer { UpkeepTag(text: String(localized: "runs commands")) }
                                }
                                if !tool.description.isEmpty {
                                    Text(verbatim: tool.description).font(.footnote).foregroundStyle(Color(.secondaryLabel))
                                }
                            }
                        }
                    }
                }
                if skill.hasSecrets {
                    keysSection(skill)
                }
                Section {
                    Button(skill.isEnabled ? LocalizedStringKey("Stop offering") : "Offer it") {
                        let isEnabled = !skill.isEnabled
                        let said = isEnabled ? String(localized: "\(skill.name) is offered again") : String(localized: "\(skill.name) is installed but not offered")
                        Task {
                            await skills.act(skill.name, saying: said) {
                                try await skills.connection.setSkillEnabled(name: skill.name, isEnabled: isEnabled)
                            }
                        }
                    }
                    Button("Remove", systemImage: "trash", role: .destructive) { isRemoving = true }
                        .confirmationDialog("Remove \(skillName)?", isPresented: $isRemoving, titleVisibility: .visible) {
                            Button("Remove", role: .destructive) {
                                Task {
                                    let isRemoved = await skills.act(skillName, saying: String(localized: "Removed \(skillName)")) {
                                        try await skills.connection.removeSkill(name: skillName)
                                    }
                                    if isRemoved { dismiss() }
                                }
                            }
                        } message: {
                            Text("Its tools stop being offered to everybody here. It can be installed again from the registry.")
                        }
                }
                .disabled(skills.busyName != nil)
            } else if skills.installed != nil {
                ContentUnavailableView("This skill is no longer installed", systemImage: "puzzlepiece.extension")
            }
        }
        .navigationTitle(Text(verbatim: skillName))
        .navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $isFillingKeys) {
            if let skill = skills.skill(skillName) {
                SkillSecretsForm(skills: skills, skill: skill)
            }
        }
        .toast(done: Bindable(skills).doneText, problem: Bindable(skills).problemText)
    }

    /// The keys it asks for: the operator's, filled in here, and each
    /// person's, filled in on their own agent page; and who fills them in,
    /// which only the operator knows the right answer to.
    private func keysSection(_ skill: InstalledSkill) -> some View {
        Section {
            if !skill.secrets.isEmpty {
                let filledText = skill.filledSecretCount(skills.filledSecretNames).formatted()
                let totalText = skill.secrets.count.formatted()
                Button {
                    isFillingKeys = true
                } label: {
                    LabeledContent {
                        Text("\(filledText) of \(totalText) set")
                    } label: {
                        Text("Keys").foregroundStyle(Color(.label))
                    }
                }
            }
            Picker(
                "Who fills its values in",
                selection: Binding(
                    get: { skill.skillScope },
                    set: { skillScope in
                        Task {
                            await skills.act(skill.name, saying: String(localized: "Changed who fills in \(skill.name)")) {
                                try await skills.connection.setSkillScope(name: skill.name, skillScope: skillScope)
                            }
                        }
                    })
            ) {
                Text("As the skill says").tag("")
                Text("One set for everybody").tag("operator")
                Text("Each person's own").tag("person")
            }
        } header: {
            Text("Keys")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if !skill.secrets.isEmpty {
                    let keysText = skill.secrets.joined(separator: ", ")
                    Text("Needs \(keysText).")
                }
                if !skill.personalSecrets.isEmpty {
                    let keysText = skill.personalSecrets.joined(separator: ", ")
                    Text("Needs \(keysText) from each person, set on their own agent page.")
                }
            }
        }
    }
}

/// SkillSecretsForm fills in the keys a skill asks the operator for, one
/// value each for everybody. A value is written and never shown again;
/// a key left blank keeps its value.
private struct SkillSecretsForm: View {
    let skills: AgentSkillsModel
    let skill: InstalledSkill
    @Environment(\.dismiss) private var dismiss
    @State private var draft: SkillSecretsDraft
    @State private var isSaving = false
    @State private var problemText: String?

    init(skills: AgentSkillsModel, skill: InstalledSkill) {
        self.skills = skills
        self.skill = skill
        _draft = State(initialValue: SkillSecretsDraft(skillName: skill.name))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                } footer: {
                    Text("Kept on this server for every person's agent. A value is written and never shown again; type a new one to change it, leave it blank to keep it.")
                }
                ForEach(skill.secrets, id: \.self) { key in
                    let isFilled = skills.filledSecretNames.contains("\(skill.name)/\(key)")
                    Section {
                        SecureField(
                            key,
                            text: Binding(get: { draft.valueByKey[key] ?? "" }, set: { draft.valueByKey[key] = $0 }),
                            prompt: isFilled ? Text("leave blank to keep it") : nil)
                    } header: {
                        Text(verbatim: key)
                    } footer: {
                        Text(isFilled ? LocalizedStringKey("set") : "not set")
                    }
                }
            }
            .navigationTitle(Text("Keys for \(skill.name)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }.disabled(isSaving)
                }
            }
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func save() {
        let keys = skill.secrets
        let typedKeys = draft.typedKeys(keys)
        guard !typedKeys.isEmpty else {
            dismiss()
            return
        }
        isSaving = true
        let sent = draft
        let keysText = typedKeys.joined(separator: ", ")
        Task {
            do {
                try await skills.connection.saveSkillSecrets(sent, keys: keys)
                await skills.load()
                skills.doneText = String(localized: "Kept \(keysText) for \(skill.name)")
                dismiss()
            } catch {
                problemText = error.localizedDescription
            }
            isSaving = false
        }
    }
}

// MARK: - Source types

/// AgentSourceTypesView is how a knowledge source is read: the types
/// installed here, each with what it says about itself, and what the
/// registry offers. Adding a type of one's own is a file pasted on the
/// dashboard.
private struct AgentSourceTypesView: View {
    let connection: Connection
    @State private var installed: [InstalledSourceType]?
    @State private var offers: [RegistryOffer]?
    @State private var filterText = ""
    @State private var busyName: String?
    @State private var reading: InstalledSourceType?
    @State private var removing: InstalledSourceType?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            Section {
                if let installed, installed.isEmpty {
                    Text("No source types are installed.").foregroundStyle(Color(.secondaryLabel))
                }
                ForEach(installed ?? []) { sourceType in
                    Button {
                        if sourceType.hasGuide { reading = sourceType }
                    } label: {
                        row(sourceType)
                    }
                    .confirmationDialog(
                        "Remove \(removing?.name ?? "")?", isPresented: Binding(get: { removing?.id == sourceType.id }, set: { if !$0 { removing = nil } }),
                        titleVisibility: .visible, presenting: removing
                    ) { sourceType in
                        Button("Remove", role: .destructive) {
                            // Refused while a source is of this type; the server says
                            // which, and the toast carries its words.
                            Task {
                                await act(sourceType.name, saying: String(localized: "Removed \(sourceType.name)")) {
                                    try await connection.removeSourceType(name: sourceType.name)
                                }
                            }
                        }
                    } message: { _ in
                        Text("Nobody can add a source of it any more. It is refused while any source is of this type.")
                    }
                    .swipeActions {
                        Button("Remove", systemImage: "trash", role: .destructive) { removing = sourceType }
                    }
                    .contextMenu {
                        if sourceType.hasGuide {
                            Button("What it says about itself", systemImage: "list.bullet") { reading = sourceType }
                        }
                        Button("Remove", systemImage: "trash", role: .destructive) { removing = sourceType }
                    }
                    .disabled(busyName == sourceType.name)
                }
            } header: {
                Text("Source types")
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    Text("How a knowledge source is read. Install one from the registry, and everybody here can add a source of it on their agent page. A type of your own is added on the dashboard.")
                    if RegistryOffer.isBehind(installedCount: installed?.count ?? 0, offers: offers ?? []) {
                        Text("The registry has a newer version of a type installed here.")
                    }
                }
            }
            RegistrySection(
                title: "Source types the registry offers",
                description: "Signed by the registry and checked before they are kept. Installing one lets everybody here add a source of it.",
                offers: offers, filterText: filterText, busyName: busyName
            ) { offer in
                Task {
                    let versionText = offer.version
                    await act(offer.name, saying: String(localized: "Installed \(offer.name) \(versionText)")) {
                        try await connection.installSourceType(name: offer.name)
                    }
                }
            }
        }
        .navigationTitle("Source types")
        .searchable(text: $filterText, prompt: Text("Filter the registry"))
        .overlay {
            if installed == nil { ProgressView() }
        }
        .task {
            await load()
            await loadRegistry()
        }
        .refreshable {
            await load()
            await loadRegistry()
        }
        .sheet(item: $reading) { sourceType in
            SourceTypeGuideSheet(sourceType: sourceType)
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func row(_ sourceType: InstalledSourceType) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(verbatim: sourceType.name).foregroundStyle(Color(.label))
                if !sourceType.version.isEmpty { UpkeepTag(text: sourceType.version) }
                if sourceType.isLocal { UpkeepTag(text: String(localized: "local"), tone: .warning) }
                if sourceType.isReadable {
                    UpkeepTag(text: AgentWords.readerWord(sourceType.reader))
                } else {
                    UpkeepTag(text: String(localized: "cannot be read"), tone: .bad)
                }
            }
            Text(verbatim: details(sourceType)).font(.footnote).foregroundStyle(Color(.secondaryLabel)).lineLimit(3)
        }
    }

    private func details(_ sourceType: InstalledSourceType) -> String {
        var parts = [sourceType.description]
        if !sourceType.runs.isEmpty {
            let placesText = sourceType.runs.map { place in
                switch place {
                case "server": String(localized: "this server")
                case "computer": String(localized: "a computer")
                default: place
                }
            }.joined(separator: ", ")
            parts.append(String(localized: "runs on \(placesText)"))
        }
        if !sourceType.requires.isEmpty {
            let toolsText = sourceType.requires.joined(separator: ", ")
            parts.append(String(localized: "needs \(toolsText)"))
        }
        if !sourceType.isReadable { parts.append(sourceType.problem ?? "") }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func load() async {
        do {
            installed = try await connection.installedSourceTypes()
        } catch {
            problemText = String(localized: "Could not list the source types")
            if installed == nil { installed = [] }
        }
    }

    private func loadRegistry() async {
        do {
            offers = try await connection.sourceTypeRegistry()
        } catch {
            problemText = String(localized: "Could not reach the source type registry")
            if offers == nil { offers = [] }
        }
    }

    private func act(_ sourceTypeName: String, saying said: String, _ work: () async throws -> Void) async {
        busyName = sourceTypeName
        defer { busyName = nil }
        do {
            try await work()
            await load()
            if offers != nil { offers = try? await connection.sourceTypeRegistry() }
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// SourceTypeGuideSheet is what a source type says about itself.
private struct SourceTypeGuideSheet: View {
    let sourceType: InstalledSourceType
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                MarkdownView(text: sourceType.guide)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding()
            }
            .navigationTitle(Text("About \(sourceType.name)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

#if DEBUG
    struct RecordingOperatorDemoScreen: View {
        let screen: String
        @State private var model: AgentAdminModel

        init(connection: Connection, screen: String) {
            self.screen = screen
            _model = State(initialValue: AgentAdminModel(connection: connection))
        }

        var body: some View {
            ScrollViewReader { proxy in
                Group {
                    if screen == "recording-model-settings" {
                        AgentModelsFormView(model: model)
                    } else {
                        AgentLimitsFormView(model: model)
                    }
                }
                .task {
                    await model.load()
                    try? await Task.sleep(for: .milliseconds(200))
                    proxy.scrollTo(screen == "recording-model-settings" ? "recording-transcription" : "recording-quotas", anchor: .top)
                }
            }
        }
    }
#endif

#if DEBUG
    /// `-TeaNodeScreen provider-prices`: the provider editor in a sheet, scrolled to the price of a particular model.
    struct ProviderPricesDemoScreen: View {
        @State private var model: AgentAdminModel
        @State private var isShowingSheet = false

        init(connection: Connection) {
            _model = State(initialValue: AgentAdminModel(connection: connection))
        }

        var body: some View {
            Color(.systemGroupedBackground)
                .ignoresSafeArea()
                .task {
                    await model.load()
                    isShowingSheet = model.settings != nil
                }
                .sheet(isPresented: $isShowingSheet) {
                    ScrollViewReader { scroller in
                        AgentProviderForm(model: model, providerIndex: 0)
                            .task {
                                try? await Task.sleep(for: .seconds(1))
                                scroller.scrollTo(AgentProviderForm.modelPriceRowId, anchor: .center)
                            }
                    }
                }
        }
    }
#endif
