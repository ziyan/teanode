import SwiftUI
import TeaNodeKit

/// The app: the connect screen until the phone is signed in, then the
/// conversation with the agent.
@main
struct TeaNodeApp: App {
    @State private var app = AppModel()

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
        }
    }
}

/// MainView is the signed-in app: one conversation on screen, the main one
/// to begin with, and the others a tap away.
struct MainView: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @State private var conversation: AgentConversation?
    @State private var isChoosing = false
    @State private var isShowingSettings = false

    var body: some View {
        NavigationStack {
            ConversationView(model: ConversationModel(conversation: conversation, app: app, connection: connection))
                .id(conversation?.id ?? "main")
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button {
                            isChoosing = true
                        } label: {
                            Label("Conversations", systemImage: "list.bullet")
                        }
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button {
                            isShowingSettings = true
                        } label: {
                            Label("Settings", systemImage: "gearshape")
                        }
                    }
                }
        }
        .sheet(isPresented: $isChoosing) {
            ConversationsSheet(connection: connection) { chosen in
                conversation = chosen
                isChoosing = false
            }
        }
        .sheet(isPresented: $isShowingSettings) {
            SettingsView()
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

/// ConversationsSheet lists the person's conversations to switch to, and
/// starts a new one.
struct ConversationsSheet: View {
    let connection: Connection
    let onChoose: (AgentConversation?) -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var conversations: [AgentConversation] = []
    @State private var isLoaded = false
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Button {
                        onChoose(nil)
                    } label: {
                        Label("Your agent", systemImage: "leaf")
                    }
                }
                Section("Conversations") {
                    ForEach(conversations.filter { !$0.isMain }) { conversation in
                        Button {
                            onChoose(conversation)
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(ConversationModel.title(of: conversation))
                                    .foregroundStyle(.primary)
                                if let lastAt = ServerTime.date(conversation.lastAt) {
                                    Text(lastAt, format: .relative(presentation: .named))
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
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
            .navigationTitle("Conversations")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Done") { dismiss() }
                }
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        Task { await start() }
                    } label: {
                        Label("New conversation", systemImage: "square.and.pencil")
                    }
                }
            }
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

/// SettingsView says which server and account the phone is signed in to,
/// and signs it out.
struct SettingsView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var isConfirmingSignOut = false

    var body: some View {
        NavigationStack {
            Form {
                if let account = app.account {
                    Section("Signed in") {
                        LabeledContent("Server", value: account.server.host)
                        LabeledContent("Account", value: account.username)
                    }
                }
                Section {
                    Button("Sign out", role: .destructive) {
                        isConfirmingSignOut = true
                    }
                } footer: {
                    Text("To manage what this phone is told about, open Phones under Agent on your dashboard.")
                }
                Section("About") {
                    LabeledContent("Version", value: Self.version)
                }
            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .confirmationDialog("Sign out of TeaNode?", isPresented: $isConfirmingSignOut, titleVisibility: .visible) {
                Button("Sign out", role: .destructive) {
                    Task {
                        dismiss()
                        await app.signOut()
                    }
                }
            } message: {
                Text("This phone stops talking to your agent until you connect it again.")
            }
        }
    }

    static var version: String {
        let info = Bundle.main.infoDictionary
        let marketing = info?["CFBundleShortVersionString"] as? String ?? ""
        let build = info?["CFBundleVersion"] as? String ?? ""
        return "\(marketing) (\(build))"
    }
}
