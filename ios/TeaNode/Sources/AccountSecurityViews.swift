import SwiftUI
import TeaNodeKit

/// SessionsView is the dashboard's Sessions page: the browsers signed in to
/// the person's account, one signed out at a time or all at once. This phone
/// signs in with a token of its own rather than a session, so it is not in
/// the list and stays connected through both.
struct SessionsView: View {
    let connection: Connection
    @State private var sessions: [AccountSession]?
    @State private var isLoading = true
    @State private var isShowingRevoked = false
    @State private var isBusy = false
    @State private var revoking: AccountSession?
    @State private var isRevokingAll = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let sessions {
                Section {
                    ForEach(AccountSession.listed(sessions)) { session in
                        row(session)
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        if sessions.isEmpty { Text("Nothing else is signed in to this account.") }
                        Text("The browsers signed in to this account. Ending one signs that browser out; the others are untouched.")
                    }
                }
                Section {
                    Button("Sign out every browser", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                        isRevokingAll = true
                    }
                } footer: {
                    Text("Use it if you have lost a device. This iPhone stays connected.")
                }
            }
        }
        .navigationTitle("Sessions")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isShowingRevoked.toggle()
                } label: {
                    Label(
                        isShowingRevoked ? LocalizedStringKey("Hide signed out sessions") : "Show signed out sessions",
                        systemImage: isShowingRevoked ? "eye.slash" : "eye")
                }
            }
        }
        .overlay { if sessions == nil && isLoading { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task(id: isShowingRevoked) { await load() }
        .confirmationDialog(
            "Sign this browser out?", isPresented: Binding(get: { revoking != nil }, set: { if !$0 { revoking = nil } }),
            titleVisibility: .visible, presenting: revoking
        ) { session in
            Button("Sign this browser out", role: .destructive) {
                Task { await act(saying: String(localized: "Signed out")) { try await connection.revokeSession(session.id) } }
            }
        } message: { session in
            Text(verbatim: browserName(session))
        }
        .confirmationDialog("Sign out every browser?", isPresented: $isRevokingAll, titleVisibility: .visible) {
            Button("Sign out every browser", role: .destructive) {
                Task { await act(saying: String(localized: "Signed out")) { try await connection.revokeAllSessions() } }
            }
        } message: {
            Text("Every browser signed in to this account is signed out and has to sign in again. This iPhone stays connected.")
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func row(_ session: AccountSession) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: browserName(session))
            if session.revoked != nil {
                StateTag(text: String(localized: "Signed out"), systemImage: "xmark.circle", style: .red)
            } else if let impersonator = session.impersonatorUsername, !impersonator.isEmpty {
                // Said plainly, so the person sees who was signed in as
                // them, and can end it from here.
                StateTag(text: String(localized: "Signed in as you by \(impersonator)"), systemImage: "person.2", style: .orange)
            } else if session.current {
                StateTag(text: String(localized: "This browser"), systemImage: "checkmark.circle", style: .secondary)
            }
            Text(verbatim: details(session))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .swipeActions {
            if session.canRevoke {
                Button("Sign out", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { revoking = session }
            }
        }
    }

    private func browserName(_ session: AccountSession) -> String {
        session.browserName ?? String(localized: "A browser that did not say what it is")
    }

    /// Where it was last used from, and when.
    private func details(_ session: AccountSession) -> String {
        let lastUsedText =
            ServerTime.date(session.lastUsed).map { String(localized: "Last used \($0.formatted(.relative(presentation: .named)))") }
            ?? String(localized: "Not used since it was created")
        guard let address = session.ip, !address.isEmpty else { return lastUsedText }
        return "\(address) · \(lastUsedText)"
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            sessions = try await connection.accountSessions(includeRevoked: isShowingRevoked)
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func act(saying said: String, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// TokensView is the dashboard's API tokens page: the tokens the command
/// line tool authenticates with, made with a name and a lifetime, renamed or
/// given a new lifetime, and revoked. A new token's secret is shown once.
struct TokensView: View {
    let connection: Connection
    @State private var tokens: [APIToken]?
    @State private var isLoading = true
    @State private var isShowingRevoked = false
    @State private var isBusy = false
    @State private var isAdding = false
    @State private var editing: APIToken?
    @State private var revoking: APIToken?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let tokens {
                Section {
                    ForEach(tokens) { token in
                        row(token)
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        if tokens.isEmpty { Text("No tokens. Create one to administer this server from another machine.") }
                        Text(
                            "A token authenticates the command line tool the way a password authenticates a browser. On the server itself no token is needed; these are for administering it from somewhere else."
                        )
                    }
                }
            }
        }
        .navigationTitle("API tokens")
        .toolbar {
            ToolbarItemGroup(placement: .topBarTrailing) {
                Button {
                    isShowingRevoked.toggle()
                } label: {
                    Label(
                        isShowingRevoked ? LocalizedStringKey("Hide revoked tokens") : "Show revoked tokens",
                        systemImage: isShowingRevoked ? "eye.slash" : "eye")
                }
                Button {
                    isAdding = true
                } label: {
                    Label("New token", systemImage: "plus")
                }
            }
        }
        .overlay { if tokens == nil && isLoading { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task(id: isShowingRevoked) { await load() }
        .sheet(isPresented: $isAdding) {
            NewTokenForm(connection: connection) {
                Task { await load() }
            }
        }
        .sheet(item: $editing) { token in
            EditTokenForm(connection: connection, token: token) {
                doneText = String(localized: "Token changed")
                Task { await load() }
            }
        }
        .confirmationDialog(
            "Revoke this token?", isPresented: Binding(get: { revoking != nil }, set: { if !$0 { revoking = nil } }),
            titleVisibility: .visible, presenting: revoking
        ) { token in
            Button("Revoke", role: .destructive) {
                Task { await act(saying: String(localized: "Revoked")) { try await connection.deleteApiToken(token.id) } }
            }
        } message: { token in
            Text("Anything using \(TokenWords.name(token)) stops working at once. This cannot be undone; issue a new token instead.")
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func row(_ token: APIToken) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: TokenWords.name(token))
            if token.revoked != nil {
                StateTag(text: String(localized: "Revoked"), systemImage: "xmark.circle", style: .red)
            } else {
                switch token.expiry() {
                case .never:
                    // A token that never expires is a standing risk, said
                    // quietly.
                    StateTag(text: TokenWords.expiry(.never), systemImage: "exclamationmark.triangle", style: .orange)
                case .expired:
                    StateTag(text: TokenWords.expiry(.expired), systemImage: "clock.badge.xmark", style: .red)
                case .expires(let expiresAt):
                    StateTag(text: TokenWords.expiry(.expires(expiresAt)), systemImage: "clock", style: .secondary)
                }
            }
            Text(verbatim: token.id)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
            Text(verbatim: details(token))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .swipeActions {
            if token.revoked == nil {
                Button("Revoke", systemImage: "trash", role: .destructive) { revoking = token }
                Button("Edit", systemImage: "pencil") { editing = token }
                    .tint(.indigo)
            }
        }
    }

    /// When it was made, when and where it was last used, and whom it acts
    /// as.
    private func details(_ token: APIToken) -> String {
        var parts: [String] = []
        if let createdAt = ServerTime.date(token.created) {
            parts.append(String(localized: "Created \(createdAt.formatted(date: .abbreviated, time: .shortened))"))
        }
        if let lastUsedAt = ServerTime.date(token.lastUsed) {
            let lastUsedText = String(localized: "Last used \(lastUsedAt.formatted(.relative(presentation: .named)))")
            if let address = token.lastUsedIp, !address.isEmpty {
                parts.append("\(lastUsedText) (\(address))")
            } else {
                parts.append(lastUsedText)
            }
        } else {
            parts.append(String(localized: "Never used"))
        }
        if !token.username.isEmpty {
            parts.append(String(localized: "Acts as \(token.username)"))
        }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            tokens = try await connection.apiTokens(includeRevoked: isShowingRevoked)
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func act(saying said: String, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// TokenWords are the words the token screens share: a token's name and
/// when it expires.
private enum TokenWords {
    static func name(_ token: APIToken) -> String {
        token.name.isEmpty ? String(localized: "Unnamed token") : token.name
    }

    static func expiry(_ expiry: APITokenExpiry) -> String {
        switch expiry {
        case .never: String(localized: "Never expires")
        case .expired: String(localized: "Expired")
        case .expires(let expiresAt): String(localized: "Expires \(expiresAt.formatted(date: .abbreviated, time: .shortened))")
        }
    }

    /// A lifetime as the new token form offers it.
    static func lifetime(_ lifetime: APITokenLifetime) -> String {
        switch lifetime {
        case .thirtyDays: String(localized: "30 days")
        case .ninetyDays: String(localized: "90 days")
        case .year: String(localized: "A year")
        case .never: String(localized: "Never")
        }
    }

    /// A lifetime as the edit form offers it, counted from now.
    static func lifetimeFromNow(_ lifetime: APITokenLifetime) -> String {
        switch lifetime {
        case .thirtyDays: String(localized: "30 days from now")
        case .ninetyDays: String(localized: "90 days from now")
        case .year: String(localized: "A year from now")
        case .never: String(localized: "Never")
        }
    }
}

/// IssuedToken is a token just made: its secret, shown this once, and the
/// server it works with.
private struct IssuedToken {
    let secret: String
    let serverOrigin: String

    /// What the command line tool reads, to paste on another machine.
    var environmentLines: String {
        "export TEANODE_URL=\(serverOrigin)\nexport TEANODE_TOKEN=\(secret)"
    }
}

/// NewTokenForm makes a token with a name and a lifetime chosen from a
/// handful, rather than a number to type, then shows its secret in the same
/// sheet, the one time it can be read.
private struct NewTokenForm: View {
    let connection: Connection
    let onCreated: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var lifetime = APITokenLifetime.standard
    @State private var issued: IssuedToken?
    @State private var isBusy = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            if let issued {
                IssuedTokenList(issued: issued) { dismiss() }
            } else {
                form
            }
        }
        .interactiveDismissDisabled(issued != nil)
    }

    private var form: some View {
        Form {
            TextField("What will hold it", text: $name, prompt: Text("laptop"))
                .textInputAutocapitalization(.never)
            Picker("Expires after", selection: $lifetime) {
                ForEach(APITokenLifetime.allCases) { lifetime in
                    Text(TokenWords.lifetime(lifetime)).tag(lifetime)
                }
            }
        }
        .navigationTitle("New token")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Create") {
                    Task { await create() }
                }
                .disabled(name.trimmed.isEmpty || isBusy)
            }
        }
        .disabled(isBusy)
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func create() async {
        isBusy = true
        defer { isBusy = false }
        do {
            let created = try await connection.createApiToken(name: name.trimmed, lifetime: lifetime)
            let serverOrigin = await connection.account.server.origin.absoluteString
            issued = IssuedToken(secret: created.secret, serverOrigin: serverOrigin)
            onCreated()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// EditTokenForm renames a token or gives it a new lifetime counted from
/// now, or keeps the expiry it has. The token itself stays the same.
private struct EditTokenForm: View {
    let connection: Connection
    let token: APIToken
    let onChanged: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    /// The new lifetime, or nil to keep the expiry as it is.
    @State private var lifetime: APITokenLifetime?
    @State private var isBusy = false
    @State private var problemText: String?

    private var change: APITokenChange { APITokenChange(token: token, newName: name, lifetime: lifetime) }

    var body: some View {
        NavigationStack {
            Form {
                TextField("What will hold it", text: $name)
                    .textInputAutocapitalization(.never)
                Section {
                    Picker("Expires after", selection: $lifetime) {
                        Text("Keep as it is (\(TokenWords.expiry(token.expiry())))").tag(APITokenLifetime?.none)
                        ForEach(APITokenLifetime.allCases) { lifetime in
                            Text(TokenWords.lifetimeFromNow(lifetime)).tag(APITokenLifetime?.some(lifetime))
                        }
                    }
                } footer: {
                    Text("The token itself stays the same, so whatever uses it keeps working.")
                }
            }
            .navigationTitle("Change this token")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task { await save() }
                    }
                    .disabled(name.trimmed.isEmpty || !change.hasChanges || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
        .onAppear { name = token.name }
    }

    private func save() async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.updateApiToken(change)
            dismiss()
            onChanged()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// IssuedTokenList shows a new token's secret, the one time it can be
/// read, with the lines that let the command line tool use it.
private struct IssuedTokenList: View {
    let issued: IssuedToken
    let onDone: () -> Void

    var body: some View {
        List {
            Section {
                Text(verbatim: issued.secret)
                    .font(.body.monospaced())
                    .textSelection(.enabled)
                Button("Copy token", systemImage: "doc.on.doc") { UIPasteboard.general.string = issued.secret }
            } footer: {
                Text(
                    "Only the hash of this token is stored, so this is the one moment it can be read. Not even an administrator can recover it afterwards."
                )
            }
            Section {
                Text(verbatim: issued.environmentLines)
                    .font(.footnote.monospaced())
                    .textSelection(.enabled)
                Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = issued.environmentLines }
            } header: {
                Text("Use it from another machine with:")
            }
        }
        .navigationTitle("New token")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Done") { onDone() }
            }
        }
    }
}

/// ConnectedAppsView is the dashboard's Apps page: the programs the person
/// authorized to act as them, each renewing its own access. One is renamed
/// or disconnected; this iPhone is one of them, and is marked.
struct ConnectedAppsView: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var apps: [ConnectedApp]?
    /// The app this iPhone signed in as, so its row says so.
    @State private var thisClientId: String?
    @State private var isLoading = true
    @State private var isBusy = false
    @State private var renaming: ConnectedApp?
    @State private var newName = ""
    @State private var disconnecting: ConnectedApp?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if let apps {
                Section {
                    ForEach(apps) { connectedApp in
                        row(connectedApp)
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 8) {
                        if apps.isEmpty { Text("No apps. An app appears here once you authorize it.") }
                        Text(
                            "Each app here was authorized by you and acts as you. It renews its own access while it is in use, and is disconnected on its own after two months unused."
                        )
                    }
                }
            }
        }
        .navigationTitle("Apps")
        .overlay { if apps == nil && isLoading { ProgressView() } }
        .disabled(isBusy)
        .refreshable { await load() }
        .task { await load() }
        .alert(
            "Rename this app", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } }), presenting: renaming
        ) { connectedApp in
            TextField("Name", text: $newName)
            Button("Cancel", role: .cancel) {}
            Button("Save") {
                let name = newName.trimmed
                guard !name.isEmpty else { return }
                Task {
                    await act(saying: String(localized: "App renamed")) { try await connection.renameConnectedApp(connectedApp.clientId, name: name) }
                }
            }
        } message: { _ in
            Text("The name stays when the app renews its access.")
        }
        .confirmationDialog(
            "Disconnect this app?", isPresented: Binding(get: { disconnecting != nil }, set: { if !$0 { disconnecting = nil } }),
            titleVisibility: .visible, presenting: disconnecting
        ) { connectedApp in
            Button("Disconnect", role: .destructive) {
                Task { await disconnect(connectedApp) }
            }
        } message: { connectedApp in
            if connectedApp.clientId == thisClientId {
                Text("This iPhone is signed out at once, and has to be connected again to come back.")
            } else {
                Text("\(appName(connectedApp)) stops working at once, and has to be authorized again to come back.")
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func row(_ connectedApp: ConnectedApp) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: appName(connectedApp))
            if connectedApp.clientId == thisClientId {
                StateTag(text: String(localized: "This iPhone"), systemImage: "iphone", style: .secondary)
            }
            if let renewableUntil = ServerTime.date(connectedApp.renewableUntil) {
                StateTag(
                    text: String(localized: "Renews until \(renewableUntil.formatted(date: .abbreviated, time: .shortened))"),
                    systemImage: "arrow.clockwise", style: .secondary)
            }
            Group {
                if let registeredName = connectedApp.differentRegisteredName {
                    Text("Registered as \(registeredName)")
                }
                if !connectedApp.redirectHosts.isEmpty {
                    Text("Returns to \(connectedApp.redirectHosts.joined(separator: ", "))").monospaced()
                }
                Text(verbatim: usage(connectedApp))
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
        .swipeActions {
            Button("Disconnect", systemImage: "xmark.circle", role: .destructive) { disconnecting = connectedApp }
            Button("Rename", systemImage: "pencil") {
                newName = connectedApp.name
                renaming = connectedApp
            }
            .tint(.indigo)
        }
    }

    private func appName(_ connectedApp: ConnectedApp) -> String {
        connectedApp.displayName ?? String(localized: "An app that gave no name")
    }

    /// When and where it last used its access, and how many times it was
    /// authorized when that is more than once.
    private func usage(_ connectedApp: ConnectedApp) -> String {
        var lastUsedText = String(localized: "Not used yet")
        if let lastUsedAt = ServerTime.date(connectedApp.lastUsed) {
            lastUsedText = String(localized: "Last used \(lastUsedAt.formatted(.relative(presentation: .named)))")
            if let address = connectedApp.lastUsedIp, !address.isEmpty {
                lastUsedText += " (\(address))"
            }
        }
        guard connectedApp.tokenCount > 1 else { return lastUsedText }
        return "\(lastUsedText) · \(String(localized: "Authorized \(connectedApp.tokenCount) times"))"
    }

    /// Disconnects an app; disconnecting this iPhone's own signs it out,
    /// since nothing it asks for works after.
    private func disconnect(_ connectedApp: ConnectedApp) async {
        guard connectedApp.clientId == thisClientId else {
            await act(saying: String(localized: "Disconnected")) { try await connection.disconnectApp(connectedApp.clientId) }
            return
        }
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.disconnectApp(connectedApp.clientId)
            await app.signOut()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        thisClientId = await connection.account.clientId
        do {
            apps = try await connection.connectedApps()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func act(saying said: String, _ work: () async throws -> Void) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }
}

/// StateTag is the small line under a row's name that says what state it is
/// in, in words and a symbol as well as a color.
private struct StateTag<Style: ShapeStyle>: View {
    let text: String
    let systemImage: String
    let style: Style

    var body: some View {
        Label(text, systemImage: systemImage)
            .font(.caption)
            .foregroundStyle(style)
            .symbolRenderingMode(.hierarchical)
    }
}
