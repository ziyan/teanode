import SwiftUI
import TeaNodeKit

/// The app: the connect screen until the phone is signed in, then the
/// conversation with the agent.
@main
struct TeaNodeApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    private var app: AppModel { delegate.app }

    var body: some Scene {
        WindowGroup {
            Group {
                if let connection = app.connection {
                    MainView(connection: connection, username: app.account?.username ?? "")
                        .id(app.account?.accountKey)
                } else {
                    ConnectView()
                }
            }
            .environment(app)
            .onOpenURL { url in app.open(url) }
        }
    }
}

/// ConversationsSheet is the drawer's conversation list: the main one first,
/// then the others by when they were last used; search across what was
/// said; the archived ones on request; and renaming, archiving, deleting and
/// making one the main conversation.
struct ConversationsSheet: View {
    let connection: Connection
    /// Shown in the side panel, under the agent's name, rather than as a
    /// sheet with a Done button.
    var isPanel = false
    /// The conversation on screen, marked in the list.
    var currentId: String?
    /// Rows shown above the conversations, as the side menu puts its
    /// sections there.
    var leading: AnyView?
    let onChoose: (AgentConversation?) -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var conversations: [AgentConversation] = []
    /// How many questions and approvals wait in each conversation.
    @State private var waitingByConversation: [String: Int] = [:]
    @State private var archived: [AgentConversation]?
    @State private var isShowingArchived = false
    @State private var words = ""
    @State private var found: [AgentConversation]?
    @State private var isLoaded = false
    @State private var problemText: String?
    @State private var renaming: AgentConversation?
    @State private var newTitle = ""
    @State private var deleting: AgentConversation?

    var body: some View {
        NavigationStack {
            List {
                if let leading {
                    Section { leading } footer: { MenuSectionBreak() }
                }
                if let found {
                    if found.isEmpty {
                        Text("Nothing matches.").foregroundStyle(.secondary)
                    }
                    ForEach(found) { conversation in row(conversation) }
                } else {
                    Section {
                        Button {
                            onChoose(nil)
                        } label: {
                            Label {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Main chat")
                                    if let main = conversations.first(where: \.isMain), waitingByConversation[main.id] != nil {
                                        WaitingMark()
                                    }
                                }
                            } icon: {
                                Image(systemName: "bubble.left")
                            }
                            // Marked by its highlight when it is the one on
                            // screen; green only marks what can be pressed.
                            .foregroundStyle(.primary)
                        }
                        .menuRow(isSelected: currentId == nil)
                        if !isPanel {
                            Button {
                                Task { await start() }
                            } label: {
                                Label("New side chat", systemImage: "plus")
                            }
                        }
                    }
                    Section("Conversations") {
                        ForEach(conversations.filter { !$0.isMain }) { conversation in row(conversation) }
                    }
                    Section {
                        DisclosureGroup("Archived chats", isExpanded: $isShowingArchived) {
                            if let archived {
                                if archived.isEmpty {
                                    Text("Nothing has been archived.").foregroundStyle(.secondary)
                                }
                                ForEach(archived) { conversation in row(conversation) }
                            } else {
                                ProgressView()
                            }
                        }
                    }
                }
            }
            .menuListStyle(isPanel)
            .searchable(text: $words, prompt: Text("Search conversations"))
            .task(id: words) {
                // A quarter of a second after the last key, as the drawer
                // waits.
                let typed = words.trimmingCharacters(in: .whitespacesAndNewlines)
                guard !typed.isEmpty else {
                    found = nil
                    return
                }
                try? await Task.sleep(for: .milliseconds(250))
                guard !Task.isCancelled else { return }
                found = try? await connection.conversations(matching: typed)
            }
            .onChange(of: isShowingArchived) { _, isShown in
                if isShown, archived == nil {
                    Task { archived = (try? await connection.conversations(isArchived: true))?.filter { !$0.isMain } ?? [] }
                }
            }
            .overlay {
                if let problemText {
                    ContentUnavailableView("Cannot load conversations", systemImage: "wifi.exclamationmark", description: Text(problemText))
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .refreshable { await load() }
            .task { await load() }
            .navigationTitle(isPanel ? "" : String(localized: "Conversations"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                if isPanel {
                    MenuBrand()
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            Task { await start() }
                        } label: {
                            Label("New side chat", systemImage: "square.and.pencil")
                        }
                    }
                } else {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Done") { dismiss() }
                    }
                }
            }
            .alert("Rename", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
                TextField("Title", text: $newTitle)
                Button("Cancel", role: .cancel) {}
                Button("Save") {
                    guard let conversation = renaming else { return }
                    let title = newTitle.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !title.isEmpty else { return }
                    Task { await act { try await connection.updateConversation(conversation.id, title: title) } }
                }
            }
            .confirmationDialog(
                "Delete this conversation?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                titleVisibility: .visible, presenting: deleting
            ) { conversation in
                Button("Delete", role: .destructive) {
                    Task {
                        await act { try await connection.deleteConversation(conversation.id) }
                        leaveIfOpen(conversation)
                    }
                }
            } message: { conversation in
                Text("“\(ConversationModel.title(of: conversation, agentTitle: app.agentTitle))” and everything in it, the files that came with it too.")
            }
        }
    }

    private func row(_ conversation: AgentConversation) -> some View {
        Button {
            onChoose(conversation.isMain ? nil : conversation)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(ConversationModel.title(of: conversation, agentTitle: app.agentTitle))
                    .foregroundStyle(.primary)
                if waitingByConversation[conversation.id] != nil {
                    WaitingMark()
                }
                if let summary = conversation.summary, !summary.isEmpty {
                    Text(summary).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
                }
                if let lastAt = ServerTime.date(conversation.lastAt) {
                    Text(lastAt, format: .relative(presentation: .named))
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
            }
        }
        .menuRow(isSelected: conversation.id == currentId)
        .swipeActions(edge: .trailing) {
            if !conversation.isMain {
                Button(role: .destructive) {
                    deleting = conversation
                } label: {
                    Label("Delete", systemImage: "trash")
                }
                Button {
                    Task {
                        await act { try await connection.updateConversation(conversation.id, isArchived: !conversation.isArchived) }
                        if !conversation.isArchived { leaveIfOpen(conversation) }
                    }
                } label: {
                    if conversation.isArchived {
                        Label("Unarchive", systemImage: "tray.and.arrow.up")
                    } else {
                        Label("Archive", systemImage: "archivebox")
                    }
                }
                .tint(.indigo)
            }
        }
        .swipeActions(edge: .leading) {
            if !conversation.isMain, !conversation.isArchived {
                Button {
                    Task { await act { try await connection.setMainConversation(conversation.id) } }
                } label: {
                    Label("Make main", systemImage: "star")
                }
                .tint(.accentColor)
            }
        }
        .contextMenu {
            Button {
                newTitle = conversation.title ?? ""
                renaming = conversation
            } label: {
                Label("Rename", systemImage: "pencil")
            }
        }
    }

    /// Does a change to the list and reads it again.
    /// A conversation deleted or archived while on screen gives way to the
    /// main one, as the dashboard's drawer does.
    private func leaveIfOpen(_ conversation: AgentConversation) {
        if conversation.id == currentId { onChoose(nil) }
    }

    private func act(_ change: () async throws -> Void) async {
        do {
            try await change()
            archived = nil
            if isShowingArchived {
                archived = (try? await connection.conversations(isArchived: true))?.filter { !$0.isMain } ?? []
            }
            await load()
        } catch {
            problemText = app.describe(error)
        }
    }

    private func load() async {
        do {
            conversations = try await connection.conversations()
            problemText = nil
            // The list reads without it; only the marks are missing.
            waitingByConversation = (try? await connection.waiting(first: 50))?.countByConversation ?? [:]
        } catch {
            problemText = app.describe(error)
        }
        isLoaded = true
    }

    private func start() async {
        do {
            onChoose(try await connection.startConversation(title: nil))
        } catch {
            problemText = app.describe(error)
        }
    }
}

/// SettingsForm says which server and account the phone is signed in to,
/// what it is told about, and signs it out.
struct SettingsForm: View {
    @Environment(AppModel.self) private var app
    @Environment(MailModel.self) private var mail: MailModel?
    @State private var isConfirmingSignOut = false

    var body: some View {
        Form {
            AccountsSection()
            if let mail, mail.mailbox != nil {
                Section {
                    NavigationLink {
                        MailboxSettingsView(mail: mail)
                    } label: {
                        Label("Mailbox", systemImage: "tray")
                    }
                }
            }
            NotificationsSection()
            Section {
                Button(app.accounts.count > 1 ? String(localized: "Sign out of \(app.account?.server.host ?? "")") : String(localized: "Sign out"), role: .destructive) {
                    isConfirmingSignOut = true
                }
            } footer: {
                Text("To manage what this phone is told about, open Settings, then Phones, on your dashboard.")
            }
            Section("About") {
                LabeledContent("Version", value: Self.version)
            }
        }
        .navigationTitle("Settings")
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("Sign out of TeaNode?", isPresented: $isConfirmingSignOut, titleVisibility: .visible) {
            Button("Sign out", role: .destructive) {
                Task { await app.signOut() }
            }
        } message: {
            Text("This phone stops talking to your agent until you connect it again.")
        }
    }

    static var version: String {
        let info = Bundle.main.infoDictionary
        let marketing = info?["CFBundleShortVersionString"] as? String ?? ""
        let build = info?["CFBundleVersion"] as? String ?? ""
        return "\(marketing) (\(build))"
    }
}

/// NotificationsSection is what this phone is told about, and the way to
/// turn notifications on when the person said no at first.
struct NotificationsSection: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        Section {
            if app.isNotificationAllowed, app.phone?.canBeNotified == true {
                ForEach(AgentPhone.allNotificationKinds, id: \.self) { kind in
                    NotificationKindToggle(notificationKind: kind)
                }
            } else {
                Text("Notifications are off for TeaNode. Turn them on to hear when your agent needs you.")
                    .foregroundStyle(.secondary)
                Button("Open Settings") {
                    if let address = URL(string: UIApplication.openNotificationSettingsURLString) {
                        UIApplication.shared.open(address)
                    }
                }
            }
        } header: {
            Text("Notifications")
        } footer: {
            if let text = app.notificationProblemText {
                Text(text)
            }
        }
    }
}

/// NotificationKindToggle turns one kind of notification on or off.
struct NotificationKindToggle: View {
    let notificationKind: String
    @Environment(AppModel.self) private var app
    @State private var isOn = true

    var body: some View {
        Toggle(Self.label(of: notificationKind), isOn: $isOn)
            .onAppear { isOn = app.wants(notificationKind) }
            .onChange(of: isOn) { _, wanted in
                Task {
                    await app.setNotificationKind(notificationKind, isOn: wanted)
                    isOn = app.wants(notificationKind)
                }
            }
    }

    static func label(of notificationKind: String) -> String {
        switch notificationKind {
        case "message": String(localized: "What your agent says on its own")
        case "approval": String(localized: "Approvals waiting for you")
        case "question": String(localized: "Questions for you")
        case "answer": String(localized: "Answers to what you asked from the phone")
        default: notificationKind
        }
    }
}

/// WaitingMark says a question or an approval waits in a conversation, in
/// the orange its card has, as the dashboard's list says it.
struct WaitingMark: View {
    var body: some View {
        Text("Waiting for you")
            .font(.caption.weight(.semibold))
            .foregroundStyle(.orange)
    }
}

/// AccountsSection is every server the app is signed in to, as the top of
/// Settings: the one in use ticked, a tap to switch, a swipe to sign out of
/// another, and adding one.
struct AccountsSection: View {
    @Environment(AppModel.self) private var app
    @State private var signingOut: Account?

    var body: some View {
        Section {
            ForEach(app.accounts, id: \.accountKey) { account in
                Button {
                    Task { await app.switchTo(account) }
                } label: {
                    HStack(spacing: 12) {
                        Text(String(account.username.prefix(1)).uppercased())
                            .font(.headline)
                            .foregroundStyle(.white)
                            .frame(width: 36, height: 36)
                            .background(Color.accentColor, in: .circle)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(account.username).foregroundStyle(.primary)
                            Text(account.server.host).font(.footnote).foregroundStyle(.secondary)
                        }
                        Spacer()
                        if account.accountKey == app.account?.accountKey {
                            Image(systemName: "checkmark").foregroundStyle(.tint).accessibilityLabel("In use")
                        }
                    }
                }
                .swipeActions {
                    if account.accountKey != app.account?.accountKey {
                        Button("Sign out", role: .destructive) { signingOut = account }
                    }
                }
            }
            Button {
                app.isAddingAccount = true
            } label: {
                Label("Add account", systemImage: "plus")
            }
        } header: {
            Text("Accounts")
        } footer: {
            if app.accounts.count > 1 {
                Text("The share sheet, widgets and Siri use the account in use.")
            }
        }
        .confirmationDialog(
            "Sign out of \(signingOut?.server.host ?? "")?", isPresented: Binding(get: { signingOut != nil }, set: { if !$0 { signingOut = nil } }),
            titleVisibility: .visible
        ) {
            Button("Sign out", role: .destructive) {
                guard let account = signingOut else { return }
                Task { await app.signOut(of: account) }
            }
        }
    }
}

