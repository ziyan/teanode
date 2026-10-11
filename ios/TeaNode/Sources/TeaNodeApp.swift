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
                    MainView(connection: connection)
                        .id(app.account?.server.description)
                } else {
                    ConnectView()
                }
            }
            .environment(app)
            .onOpenURL { url in app.open(url) }
        }
    }
}

/// MainView is the signed-in app, in tabs as the dashboard's sidebar has
/// it: the agent, mail, the calendar, the contacts, and the rest (the
/// knowledge, settings) under More.
struct MainView: View {
    enum AppTab: Hashable {
        case agent, mail, calendar, contacts, more
    }

    let connection: Connection
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @State private var section = AppTab.agent

    var body: some View {
        TabView(selection: $section) {
            Tab(app.agentTitle, systemImage: "bubble.left.and.text.bubble.right", value: AppTab.agent) {
                AgentTab(connection: connection)
            }
            Tab("Mail", systemImage: "envelope", value: AppTab.mail) {
                MailScreen(connection: connection, username: app.account?.username ?? "")
            }
            Tab("Calendar", systemImage: "calendar", value: AppTab.calendar) {
                CalendarScreen(connection: connection, username: app.account?.username ?? "")
            }
            Tab("Contacts", systemImage: "person.crop.circle", value: AppTab.contacts) {
                ContactsScreen(connection: connection)
            }
            Tab("More", systemImage: "ellipsis", value: AppTab.more) {
                MoreScreen(connection: connection)
            }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
        .onAppear {
            #if DEBUG
                switch DemoServer.tab {
                case "mail": section = .mail
                case "calendar": section = .calendar
                case "contacts": section = .contacts
                case "knowledge": section = .more
                default: break
                }
            #endif
        }
        .task {
            await app.loadAgentName()
            await app.setUpNotifications()
        }
        .onChange(of: app.openedConversationId) { _, conversationId in
            if conversationId != nil { section = .agent }
        }
        .onChange(of: app.askingAbout) { _, reference in
            if reference != nil { section = .agent }
        }
        .onChange(of: app.openedKnowledgePath) { _, knowledgePath in
            if knowledgePath != nil { section = .more }
        }
        // While the app is open the person is here: notifications that
        // would tell them what they are looking at are held back.
        .task(id: scenePhase) {
            let isVisible = scenePhase == .active
            try? await connection.reportPresence(isVisible: isVisible)
            while isVisible, !Task.isCancelled {
                try? await Task.sleep(for: .seconds(60))
                try? await connection.reportPresence(isVisible: true)
            }
        }
    }
}

/// AgentTab is one conversation on screen, the main one to begin with, and
/// the others a tap away.
struct AgentTab: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var conversation: AgentConversation?
    @State private var isChoosing = false

    var body: some View {
        SidePanel(isOpen: $isChoosing) {
            ConversationsSheet(connection: connection, isPanel: true, currentId: conversation?.id) { chosen in
                conversation = chosen
                isChoosing = false
            }
            // Read again each time it opens, for what changed meanwhile.
            .id(isChoosing)
        } content: {
            NavigationStack {
                ConversationView(model: ConversationModel(conversation: conversation, app: app, connection: connection))
                    .id(conversation?.id ?? "main")
                    .toolbar {
                        ToolbarItem(placement: .topBarLeading) {
                            Button {
                                isChoosing = true
                            } label: {
                                Label("Conversations", systemImage: "line.3.horizontal")
                            }
                        }
                    }
            }
        }
        .onChange(of: app.openedConversationId) { _, conversationId in
            guard let conversationId else { return }
            app.openedConversationId = nil
            isChoosing = false
            // An empty id is the main conversation; for another, the kind is
            // learned when the conversation loads.
            conversation = conversationId.isEmpty ? nil : AgentConversation(id: conversationId, kind: "named", title: nil, summary: nil, lastAt: nil)
        }
        .alert(
            "Already connected",
            isPresented: Bindable(app).isShowingOpenedLinkText,
            presenting: app.openedLinkText
        ) { _ in
            Button("OK", role: .cancel) {}
        } message: { text in
            Text(text)
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
    let onChoose: (AgentConversation?) -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var conversations: [AgentConversation] = []
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
                            Label(String(localized: "Main chat"), systemImage: "bubble.left")
                        }
                        .listRowBackground(currentId == nil ? Color(.tertiarySystemFill) : nil)
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
            .navigationTitle(isPanel ? app.agentTitle : String(localized: "Conversations"))
            .navigationBarTitleDisplayMode(isPanel ? .large : .inline)
            .toolbar {
                if isPanel {
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
                    Task { await act { try await connection.deleteConversation(conversation.id) } }
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
        .listRowBackground(conversation.id == currentId ? Color(.tertiarySystemFill) : nil)
        .swipeActions(edge: .trailing) {
            if !conversation.isMain {
                Button(role: .destructive) {
                    deleting = conversation
                } label: {
                    Label("Delete", systemImage: "trash")
                }
                Button {
                    Task { await act { try await connection.updateConversation(conversation.id, isArchived: !conversation.isArchived) } }
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
    @State private var isConfirmingSignOut = false

    var body: some View {
        Form {
            if let account = app.account {
                Section("Signed in") {
                    LabeledContent("Server", value: account.server.host)
                    LabeledContent("Account", value: account.username)
                }
            }
            NotificationsSection()
            Section {
                Button("Sign out", role: .destructive) {
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
