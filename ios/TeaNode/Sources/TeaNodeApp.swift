import SwiftUI
import TeaNodeKit

/// The app: the connect screen until the phone is signed in, then the
/// conversation with the agent.
@main
struct TeaNodeApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    @AppStorage(Appearance.storageKey) private var appearance = Appearance.system
    @AppStorage(DisplayLanguage.storageKey) private var language = DisplayLanguage.system
    @Environment(\.scenePhase) private var scenePhase
    /// The appearance held while the app is not in front: the one on screen
    /// as it left. iOS draws the app in light and in dark for the pictures
    /// the app switcher keeps and takes each at once, before SwiftUI has
    /// drawn every view in the new style (a dark list with light words). Held
    /// to what is on screen, neither pass changes anything, so each picture
    /// is the screen as it was. When the app comes back it follows the choice
    /// again, and the system's appearance if that changed meanwhile.
    @State private var heldAppearance: Appearance?

    private var app: AppModel { delegate.app }

    init() {
        DisplayLanguage.chosen.apply()
        // Files that photo downloads left in the temporary folder before the
        // app was last closed.
        Task.detached(priority: .utility) { PhotoTemporaryFiles.sweep() }
    }

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
            // Drawn again whole when the language changes, as words looked
            // up before would stay in the old one.
            .id(language)
            .environment(\.locale, language.locale)
            // The windows' style alone appeared not to reach the pictures the
            // app switcher keeps (a dark chat showed there with light parts),
            // so SwiftUI is told the chosen scheme as well; apply() still sets
            // the windows, which System (nil here) needs.
            .preferredColorScheme((heldAppearance ?? appearance).colorScheme)
            // After the choice is stored, so the windows' style is the last
            // thing set whichever way the change went.
            .onChange(of: appearance) { appearance.apply() }
            // A web page asked for anywhere in the app comes up in the app's
            // own browser, from the bottom. After the model is in the
            // environment's reach, which the line below gives it.
            .modifier(InAppBrowserPresenter())
            .environment(app)
            .environment(\.openURL, OpenURLAction { address in app.followLink(address) })
            // Switches in TeaNode's green: they are the system's brighter
            // green otherwise, beside the accent on the same screen. Only
            // switches: a tint here would also turn every toolbar button green.
            .toggleStyle(AccentSwitchToggleStyle())
            .onOpenURL { url in app.open(url) }
            .onAppear {
                appearance.apply()
                PhotoBackupEngine.shared.sceneBecameActive()
            }
            .onChange(of: scenePhase, initial: true) { _, phase in
                // Held to light or dark only: while what is shown cannot be
                // read it is tried again at the next phase.
                if phase != .active, heldAppearance == nil {
                    let shownAppearance = appearance.shownNow
                    if shownAppearance != .system {
                        heldAppearance = shownAppearance
                        shownAppearance.apply()
                    }
                }
                if phase == .active {
                    heldAppearance = nil
                    appearance.apply()
                    // Photos taken since are backed up when the app comes back.
                    PhotoBackupEngine.shared.sceneBecameActive()
                }
                LocationRecorder.shared.sceneDidChange(isActive: phase == .active, isBackground: phase == .background)
                HealthSync.shared.sceneDidChange(isActive: phase == .active, isBackground: phase == .background)
            }
        }
    }
}

/// ConversationsList is the Chats tab's first screen: the main conversation
/// first, then the chats with people and the others by when they were last
/// used; search across what was said; the archived ones on request; and
/// renaming, archiving, deleting and making one the main conversation. A
/// conversation chosen is pushed on top of it by the caller.
struct ConversationsList: View {
    let connection: Connection
    /// Opens a conversation; nil is the main one.
    let onChoose: (AgentConversation?) -> Void
    @Environment(AppModel.self) private var app
    @State private var conversations: [AgentConversation] = []
    /// How many questions and approvals wait in each conversation.
    @State private var waitingByConversation: [String: Int] = [:]
    @State private var archived: [AgentConversation]?
    @State private var isShowingArchived = false
    @State private var words = ""
    @State private var found: [AgentConversation]?
    @State private var isLoaded = false
    @State private var problemText: String?
    @State private var toastProblemText: String?
    @State private var renaming: AgentConversation?
    @State private var newTitle = ""
    @State private var deleting: AgentConversation?
    /// The sheets for starting a chat with a person, and a group.
    @State private var isMessagingSomeone = false
    @State private var isStartingGroup = false

    private var listContent: some View {
        List {
            if let found {
                if found.isEmpty {
                    Text("Nothing matches.").foregroundStyle(.secondary)
                }
                ForEach(found) { conversation in listRow(conversation) }
            } else {
                // A person with no agent has no main chat to open. A new
                // side chat, a chat with someone and a group are started
                // from the button at the top, so the list is only chats.
                if app.hasAgent {
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
                            .foregroundStyle(.primary)
                        }
                    }
                    .listSectionSeparator(.hidden)
                }
                // The chats with people, by when they were last spoken in
                // (the server lists them so), apart from the side chats
                // with the agent.
                let chats = conversations.filter(\.isChat)
                if !chats.isEmpty {
                    Section("People") {
                        ForEach(chats) { conversation in chatRow(conversation) }
                    }
                    .listSectionSeparator(.hidden)
                }
                Section("Side chats") {
                    ForEach(conversations.filter { !$0.isMain && !$0.isChat }) { conversation in row(conversation) }
                }
                .listSectionSeparator(.hidden)
                Section {
                    DisclosureGroup("Archived chats", isExpanded: $isShowingArchived) {
                        if let archived {
                            if archived.isEmpty {
                                Text("Nothing has been archived.").foregroundStyle(.secondary)
                            }
                            ForEach(archived) { conversation in listRow(conversation) }
                        } else {
                            ProgressView()
                        }
                    }
                }
                .listSectionSeparator(.hidden)
            }
        }
    }

    private var framed: some View {
        listContent
            .listStyle(.plain)
            .navigationTitle("Chats")
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
            .toast(done: .constant(nil), problem: $toastProblemText)
            .overlay {
                if let problemText {
                    ContentUnavailableView("Cannot load conversations", systemImage: "wifi.exclamationmark", description: Text(problemText))
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .refreshable { await load() }
            // Read when the list comes into view, which includes coming back from
            // a conversation, and every half minute while it stays.
            .task {
                repeat {
                    await load()
                    do { try await Task.sleep(for: .seconds(30)) } catch { return }
                } while !Task.isCancelled
            }
            .toolbar {
                MoreBackButton()
                ToolbarItem(placement: .topBarTrailing) {
                    newMenu
                }
            }
    }

    var body: some View {
        framed
            // The words the Search tab's Show all in Chats carries.
            .onChange(of: app.openedPartSearch, initial: true) { _, search in
                guard let search, search.area == .chats else { return }
                app.openedPartSearch = nil
                words = search.words
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
            .sheet(isPresented: $isMessagingSomeone) {
                ChatPeopleSheet(connection: connection, purpose: .direct) { userIds, _ in
                    guard let userId = userIds.first else { return }
                    await chatOpened(try await connection.openDirectChat(with: userId))
                }
            }
            .sheet(isPresented: $isStartingGroup) {
                ChatPeopleSheet(connection: connection, purpose: .group) { userIds, groupName in
                    var draft = GroupChatDraft()
                    draft.title = groupName
                    draft.userIds = Set(userIds)
                    await chatOpened(try await connection.createGroupChat(draft))
                }
            }
    }

    /// What starts a conversation: a side chat with the agent, when it is
    /// on, or a chat with a person or a group, which needs no agent.
    @ViewBuilder
    private var newMenu: some View {
        if app.canChat {
            Menu {
                if app.isAgentOn {
                    Button {
                        Task { await start() }
                    } label: {
                        Label("New side chat", systemImage: "plus")
                    }
                }
                Button {
                    isMessagingSomeone = true
                } label: {
                    Label("Message someone", systemImage: "person.crop.circle.badge.plus")
                }
                Button {
                    isStartingGroup = true
                } label: {
                    Label("New group", systemImage: "person.2")
                }
            } label: {
                Label("New conversation", systemImage: "square.and.pencil")
            }
        } else if app.isAgentOn {
            Button {
                Task { await start() }
            } label: {
                Label("New side chat", systemImage: "square.and.pencil")
            }
        }
    }

    /// A chat just opened or made is shown: the list is read again so that
    /// it comes with its people.
    private func chatOpened(_ conversationId: String) async {
        await load()
        onChoose(conversations.first { $0.id == conversationId }
            ?? AgentConversation(id: conversationId, kind: "chat", title: nil, summary: nil, lastAt: nil))
    }

    /// A chat in the list: its people's pictures, its name, bold with a count
    /// while messages wait in it, and a mark when the person is not told of
    /// all of it.
    private func chatRow(_ conversation: AgentConversation) -> some View {
        let chat = conversation.chat
        let unreadCount = chat?.unreadCount ?? 0
        let name = ConversationModel.title(of: conversation, agentTitle: app.agentTitle, ownUserId: app.ownUserId)
        return Button {
            onChoose(conversation)
        } label: {
            HStack(spacing: 12) {
                if let chat {
                    ChatAvatarsView(chat: chat, ownUserId: app.ownUserId, size: 32)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text(name)
                        .fontWeight(unreadCount > 0 ? .bold : .regular)
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    if let lastAt = ServerTime.date(conversation.lastAt) {
                        Text(lastAt, format: .relative(presentation: .named))
                            .font(.caption)
                            .foregroundStyle(.tertiary)
                    }
                }
                Spacer(minLength: 0)
                // A chat not told of everything says so: the @ for mentions
                // alone, a bell crossed out for nothing.
                if let level = chat?.level, level != .all {
                    Image(systemName: ChatWords.symbol(of: level))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(level == .mentions ? Text("Only mentions") : Text("Notifications off"))
                }
                if unreadCount > 0 {
                    UnreadBadge(unreadCount: unreadCount)
                }
            }
        }
        // No swipe actions, as for the other rows: everything is in the
        // long press.
        .contextMenu {
            if let chat {
                NotificationLevelPicker(level: chat.level) { level in
                    Task { await act { try await connection.setChatNotificationLevel(conversation.id, to: level) } }
                }
            }
            // Put away like any other conversation, for this person alone, and
            // listed with the archived until it is brought back.
            Button {
                Task {
                    await act { try await connection.updateConversation(conversation.id, isArchived: !conversation.isArchived) }
                }
            } label: {
                if conversation.isArchived {
                    Label("Unarchive", systemImage: "tray.and.arrow.up")
                } else {
                    Label("Archive", systemImage: "archivebox")
                }
            }
        }
    }

    /// A row for either kind of conversation: a chat with people has the
    /// menu of a side chat with the agent less rename, make main and delete
    /// (it has mute and archive instead), wherever the list is drawn, search
    /// results included.
    @ViewBuilder
    private func listRow(_ conversation: AgentConversation) -> some View {
        if conversation.isChat {
            chatRow(conversation)
        } else {
            row(conversation)
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
        // No swipe actions: everything is in the long press, as for the chats.
        .contextMenu {
            Button {
                newTitle = conversation.title ?? ""
                renaming = conversation
            } label: {
                Label("Rename", systemImage: "pencil")
            }
            if !conversation.isMain, !conversation.isArchived {
                Button {
                    Task { await act { try await connection.setMainConversation(conversation.id) } }
                } label: {
                    Label("Make main", systemImage: "star")
                }
            }
            if !conversation.isMain {
                Button {
                    Task {
                        await act { try await connection.updateConversation(conversation.id, isArchived: !conversation.isArchived) }
                    }
                } label: {
                    if conversation.isArchived {
                        Label("Unarchive", systemImage: "tray.and.arrow.up")
                    } else {
                        Label("Archive", systemImage: "archivebox")
                    }
                }
                Divider()
                Button(role: .destructive) {
                    deleting = conversation
                } label: {
                    Label("Delete", systemImage: "trash")
                }
                .tint(.red)
            }
        }
        .confirmationDialog(
            "Delete this conversation?", isPresented: Binding(get: { deleting?.id == conversation.id }, set: { if !$0 { deleting = nil } }),
            titleVisibility: .visible, presenting: deleting
        ) { conversation in
            Button("Delete", role: .destructive) {
                Task { await act { try await connection.deleteConversation(conversation.id) } }
            }
        } message: { conversation in
            Text("“\(ConversationModel.title(of: conversation, agentTitle: app.agentTitle))” and everything in it, the files that came with it too.")
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
            report(error)
        }
    }

    /// A failure is the whole screen only while no conversation is shown.
    /// With rows on screen they stay, and the failure is told as a toast.
    private func report(_ error: Error) {
        if conversations.isEmpty {
            problemText = app.describe(error)
        } else {
            toastProblemText = app.describe(error)
        }
    }

    private func load() async {
        do {
            conversations = try await connection.conversations()
            // The archived ones too, if they were read before, so an opened
            // list of them is not left as it was.
            if archived != nil {
                archived = (try? await connection.conversations(isArchived: true))?.filter { !$0.isMain } ?? archived
            }
            problemText = nil
            // The list reads without it; only the marks are missing.
            waitingByConversation = (try? await connection.waiting(first: 50))?.countByConversation ?? [:]
        } catch {
            report(error)
        }
        isLoaded = true
    }

    private func start() async {
        do {
            onChoose(try await connection.startConversation(title: nil))
        } catch {
            report(error)
        }
    }
}

/// SettingsForm says which server and account the phone is signed in to,
/// what it is told about, and signs it out.
struct SettingsForm: View {
    @Environment(AppModel.self) private var app
    @Environment(MailModel.self) private var mail: MailModel?
    @State private var isConfirmingSignOut = false
    private static let signOutRowId = "sign-out"
    private static let photoBackupRowId = "photo-backup"
    /// Whether the account may open the management pages, as the dashboard
    /// shows its Manage side.
    @State private var isOperator = false
    /// Whether the account may use circles, and how many invitations to join
    /// one are waiting for an answer, as the badge on its row.
    @State private var canUseCircles = false
    @State private var openCircleInvitationCount = 0
    /// Whether Circles is pushed because a notification of an invitation was pressed.
    @State private var isShowingCircles = false
    #if DEBUG
        /// Whether the screen a screenshot names (`-TeaNodeScreen`) is
        /// pushed, and whether it has been once, so coming back stays here.
        @State private var isShowingDemoScreen = false
        @State private var hasShownDemoScreen = false
    #endif

    var body: some View {
        // A reader around the form, so a screenshot can bring the Sign out row into view.
        ScrollViewReader { scroller in
            Form {
                AccountsSection()
                if let connection = app.connection {
                    Section("Your account") {
                        NavigationLink {
                            // A new name or picture shows in the accounts above at once.
                            PreferencesView(connection: connection) { saved in app.profileSaved(saved) }
                        } label: {
                            Label("Profile", systemImage: "person.crop.circle")
                                .foregroundStyle(.primary)
                        }
                        // Next to the profile, as on the dashboard's account menu: it is about who
                        // the person is among other people.
                        if canUseCircles {
                            NavigationLink {
                                CirclesView(connection: connection)
                            } label: {
                                // The count is in the label, as the Photo backup row's status is: a
                                // badge on the link itself is drawn after the chevron.
                                HStack {
                                    Label("Circles", systemImage: "person.2")
                                        .foregroundStyle(.primary)
                                    Spacer()
                                    if openCircleInvitationCount > 0 {
                                        Text(openCircleInvitationCount, format: .number)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                                .accessibilityElement(children: .combine)
                            }
                            // Everything the person shares and is shared with, next to the people it is shared among.
                            NavigationLink {
                                SharingView(connection: connection)
                            } label: {
                                Label("Sharing", systemImage: "square.and.arrow.up")
                                    .foregroundStyle(.primary)
                            }
                        }
                        NavigationLink {
                            ChangePasswordView(connection: connection)
                        } label: {
                            Label("Password", systemImage: "key")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            PasskeysView(connection: connection)
                        } label: {
                            Label("Passkeys", systemImage: "person.badge.key")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            TokensView(connection: connection)
                        } label: {
                            Label("API tokens", systemImage: "terminal")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            DevicesView(connection: connection)
                        } label: {
                            Label("Devices", systemImage: "iphone")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            ConnectedAppsView(connection: connection)
                        } label: {
                            Label("Apps", systemImage: "app.badge.checkmark")
                                .foregroundStyle(.primary)
                        }
                        // Last, as on the dashboard's menu: the account's pages keep one order.
                        NavigationLink {
                            SessionsView(connection: connection)
                        } label: {
                            Label("Sessions", systemImage: "macbook.and.iphone")
                                .foregroundStyle(.primary)
                        }
                    }
                }
                if let connection = app.connection, app.isPhotosOffered {
                    Section {
                        NavigationLink {
                            PhotoBackupView(connection: connection)
                        } label: {
                            PhotoBackupSettingsRow()
                        }
                        .id(Self.photoBackupRowId)
                        NavigationLink {
                            PhotoFacesSettingsView(connection: connection)
                        } label: {
                            Label("People in Photos", systemImage: "person.crop.circle")
                                .foregroundStyle(.primary)
                        }
                    }
                }
                if let connection = app.connection, app.canUseHealth {
                    Section {
                        NavigationLink {
                            HealthSettingsScreen(connection: connection)
                        } label: {
                            HealthSettingsRow()
                        }
                    }
                }
                if let connection = app.connection {
                    Section("Your agent") {
                        NavigationLink {
                            AgentSettingsView(connection: connection)
                        } label: {
                            Label("Agent settings", systemImage: "slider.horizontal.3")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            KnowledgeSourcesView(connection: connection)
                        } label: {
                            Label("Sources", systemImage: "tray.2")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            AgentMemorySettingsView(connection: connection)
                        } label: {
                            Label("Memory", systemImage: "brain")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            DreamsView(connection: connection)
                        } label: {
                            Label("Dreams", systemImage: "moon.zzz")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            SchedulesView(connection: connection)
                        } label: {
                            Label("Schedules", systemImage: "clock")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            MutedAlertsView(connection: connection)
                        } label: {
                            Label("Alerts", systemImage: "bell.badge")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            ConnectionsView(connection: connection)
                        } label: {
                            Label("Connections", systemImage: "point.3.connected.trianglepath.dotted")
                                .foregroundStyle(.primary)
                        }
                        NavigationLink {
                            AgentActivityView(connection: connection)
                        } label: {
                            Label("Activity", systemImage: "list.bullet.rectangle")
                                .foregroundStyle(.primary)
                        }
                    }
                }
                if isOperator, let connection = app.connection {
                    Section {
                        NavigationLink {
                            ManageOverviewView(connection: connection)
                        } label: {
                            Label("Manage", systemImage: "server.rack")
                                .foregroundStyle(.primary)
                        }
                    } footer: {
                        Text("The server: its mail, domains, people and settings.")
                    }
                }
                DisplaySection()
                if app.services?.isMailboxEnabled != false, let mail, mail.mailbox != nil {
                    Section {
                        NavigationLink {
                            MailboxSettingsView(mail: mail)
                        } label: {
                            Label("Mailbox", systemImage: "tray")
                                .foregroundStyle(.primary)
                        }
                    }
                }
                NotificationsSection()
                Section {
                    Button(app.accounts.count > 1 ? String(localized: "Sign out of \(app.account?.server.host ?? "")") : String(localized: "Sign out"), role: .destructive) {
                        isConfirmingSignOut = true
                    }
                    .id(Self.signOutRowId)
                    .confirmationDialog("Sign out of TeaNode?", isPresented: $isConfirmingSignOut, titleVisibility: .visible) {
                        Button("Sign out", role: .destructive) {
                            Task { await app.signOut() }
                        }
                    } message: {
                        Text("This phone stops talking to your agent until you connect it again.")
                    }
                } footer: {
                    Text("To manage what this phone is told about, open Settings, then Devices, on your dashboard.")
                }
                Section("About") {
                    LabeledContent("Version", value: Self.version)
                }
            }
            .navigationTitle("Settings")
            .navigationBarTitleDisplayMode(.inline)
            .task {
                if let connection = app.connection {
                    isOperator = (try? await connection.isOperator()) ?? false
                }
            }
            .task { await PhotoBackupEngine.shared.refresh() }
            // Again on coming back from the circles screen, where an invitation may have been answered.
            .onAppear {
                Task { await refreshCircles() }
            }
            .navigationDestination(isPresented: $isShowingCircles) {
                if let connection = app.connection { CirclesView(connection: connection) }
            }
            .onChange(of: app.isCirclesOpened, initial: true) { _, isOpened in
                guard isOpened else { return }
                app.isCirclesOpened = false
                isShowingCircles = true
            }
            #if DEBUG
                .navigationDestination(isPresented: $isShowingDemoScreen) { demoScreen }
                .onAppear {
                    // `-TeaNodeScreen settings-sign-out`: the sign-out confirmation open, to show where it points.
                    // `settings-photo-backup-other-account`: the Photo backup row as it reads when backup is on for another account.
                    if DemoServer.screen == "settings-photo-backup-other-account", !hasShownDemoScreen {
                        hasShownDemoScreen = true
                        Task {
                            try? await Task.sleep(for: .seconds(1))
                            withAnimation { scroller.scrollTo(Self.photoBackupRowId, anchor: .center) }
                        }
                        return
                    }
                    if DemoServer.screen == "settings-sign-out", !hasShownDemoScreen {
                        hasShownDemoScreen = true
                        Task {
                            try? await Task.sleep(for: .seconds(1))
                            withAnimation { scroller.scrollTo(Self.signOutRowId, anchor: .center) }
                            try? await Task.sleep(for: .seconds(1))
                            isConfirmingSignOut = true
                        }
                        return
                    }
                    guard !hasShownDemoScreen, let screen = DemoServer.screen, screen != "settings" else { return }
                    hasShownDemoScreen = true
                    isShowingDemoScreen = true
                }
            #endif
        }
    }

    private func refreshCircles() async {
        guard let connection = app.connection else { return }
        canUseCircles = (try? await connection.accessSession())?.holds("circle:use") ?? false
        openCircleInvitationCount = canUseCircles ? ((try? await connection.circleInvitations())?.count ?? 0) : 0
    }

    #if DEBUG
        /// The screen `-TeaNodeScreen` names, for the screenshots CI takes.
        @ViewBuilder
        private var demoScreen: some View {
            if let connection = app.connection {
                switch DemoServer.screen ?? "" {
                case "device-setup", "device-setup-collections", "device-setup-unavailable": DeviceSetupView(connection: connection)
                case "device-setup-mail", "device-setup-profile", "device-setup-expired", "device-setup-receipt-unavailable": DeviceSetupView(connection: connection, preselectedMailboxId: "mailbox-demo")
                case "agent-settings": AgentSettingsView(connection: connection)
                case "provider-prices": ProviderPricesDemoScreen(connection: connection)
                case "provider-billing-check": ProviderPricesDemoScreen(connection: connection, scrollTarget: AgentProviderForm.billedPricesRowId)
                case "manage": ManageOverviewView(connection: connection)
                case "domain": DemoDomainScreen(connection: connection, isShowingRecords: false)
                case "dns-records": DemoDomainScreen(connection: connection, isShowingRecords: true)
                case "people": PeopleView(connection: connection)
                case "server": ServerSettingsView(connection: connection)
                case "sessions": SessionsView(connection: connection)
                case "phones": PhonesView(connection: connection)
                case "photos-backup-off", "photos-backup-on", "photos-backup-waiting", "photos-backup-background", "photos-backup-paused", "photos-backup-other-account",
                    "photos-backup-refused":
                    PhotoBackupView(connection: connection)
                case "photos-backup-refused-files": PhotoBackupRefusedFilesView()
                case "photos-faces": PhotoFacesSettingsView(connection: connection)
                case "photos-conversation-card": PhotoConversationCardDemo()
                case "devices": DevicesView(connection: connection)
                case "rules":
                    if let mail {
                        MailRulesView(mail: mail, mailboxId: mail.mailbox?.id ?? "")
                    }
                case "dreams": DreamsView(connection: connection)
                case "alerts": MutedAlertsView(connection: connection)
                case "alerts-following": MutedAlertsView(connection: connection, focusesFollowing: true)
                case "activity-run": AgentActivityView(connection: connection)
                case "schedules": SchedulesView(connection: connection)
                case "schedules-brief-days": BriefDaysDemoScreen(isEmpty: false)
                case "schedules-brief-days-none": BriefDaysDemoScreen(isEmpty: true)
                case "agent-usage": AgentUsageView(connection: connection)
                case "agent-usage-cost": AgentUsageView(connection: connection, isTableAtCost: true)
                case "circles": CirclesView(connection: connection)
                case "sharing": SharingView(connection: connection)
                case "circle-detail", "circle-detail-operator": CircleDetailDemoScreen(connection: connection)
                case "circle-join-offer": CircleJoinOfferDemoScreen(connection: connection)
                case "circle-relationship": CircleRelationshipDemoScreen(connection: connection)
                case "skill-share-sheet": SkillSharingView(connection: connection)
                case "skill-share-call": SkillShareCallsView(connection: connection, share: DemoServer.lentSkillShare) {}
                case "settings-picture": PreferencesView(connection: connection) { saved in app.profileSaved(saved) }
                case "health-settings", "health-setup": HealthSettingsScreen(connection: connection)
                default: EmptyView()
                }
            }
        }
    #endif

    static var version: String {
        let info = Bundle.main.infoDictionary
        let marketing = info?["CFBundleShortVersionString"] as? String ?? ""
        let build = info?["CFBundleVersion"] as? String ?? ""
        return "\(marketing) (\(build))"
    }
}

/// PhotoBackupSettingsRow is the row in Settings that opens the Photo Backup
/// screen, saying whether backup is on.
struct PhotoBackupSettingsRow: View {
    private let engine = PhotoBackupEngine.shared
    @Environment(AppModel.self) private var app

    var body: some View {
        HStack {
            Label("Photo backup", systemImage: "icloud.and.arrow.up")
                .foregroundStyle(.primary)
            Spacer()
            statusText
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    /// On only for the account backup is on for; for another account it says
    /// which one, since this account's backup is off.
    private var statusText: Text {
        guard let enabledAccountKey = engine.enabledAccountKey else { return Text("Off") }
        if enabledAccountKey == app.connection?.accountKey { return engine.isPaused ? Text("Paused") : Text("On") }
        return Text("On for \(PhotoBackupEngine.name(ofAccountKey: enabledAccountKey))")
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
        case "answer": String(localized: "Answers that come after you have left")
        case "circle": String(localized: "Invitations to circles, and what is shared with you")
        case "chat": String(localized: "Messages from people in your chats")
        case "skill_share_call": String(localized: "Skill calls waiting for your approval")
        case "location": String(localized: "People who share their location arriving at and leaving places")
        case "photo": String(localized: "Photos added to albums shared with you")
        case "shopping": String(localized: "Shopping lists: arriving at a store, and someone starting to shop a list")
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
                    // The person as their server shows them: picture and name,
                    // with the username beside the server it signs in to.
                    let profile = app.accountProfiles[account.accountKey]
                    HStack(spacing: 12) {
                        AvatarView(
                            name: profile?.displayName ?? account.username, avatarUrl: profile?.avatarUrl,
                            colorIndex: profile?.avatarColorIndex, size: 36, account: account
                        )
                        let displayName = profile?.displayName ?? account.username
                        // The text colors as colors, not the hierarchical
                        // styles: inside a list's button those take the tint,
                        // and an account is not an action to be drawn green.
                        VStack(alignment: .leading, spacing: 2) {
                            Text(displayName).foregroundStyle(Color.primary)
                            // The username only when the name above is not it.
                            Text(verbatim: displayName == account.username ? account.server.host : "\(account.username) · \(account.server.host)")
                                .font(.footnote).foregroundStyle(Color.secondary)
                        }
                        Spacer()
                        if account.accountKey == app.account?.accountKey {
                            Image(systemName: "checkmark").foregroundStyle(.tint).accessibilityLabel("In use")
                        }
                    }
                }
                .task(id: account.accountKey) { await app.loadProfile(of: account) }
                .swipeActions {
                    if account.accountKey != app.account?.accountKey {
                        Button("Sign out", role: .destructive) { signingOut = account }
                    }
                }
                .confirmationDialog(
                    "Sign out of \(signingOut?.server.host ?? "")?", isPresented: Binding(get: { signingOut?.accountKey == account.accountKey }, set: { if !$0 { signingOut = nil } }),
                    titleVisibility: .visible
                ) {
                    Button("Sign out", role: .destructive) {
                        guard let account = signingOut else { return }
                        Task { await app.signOut(of: account) }
                    }
                }
            }
            Button {
                app.isAddingAccount = true
            } label: {
                Label("Add account", systemImage: "plus")
                    .actionColor()
            }
        } header: {
            Text("Accounts")
        } footer: {
            if app.accounts.count > 1 {
                Text("The share sheet, widgets and Siri use the account in use.")
            }
        }
    }
}

/// AccentSwitchToggleStyle is the system switch in the accent color, as the
/// app's default toggle style. The switch style is named inside, so the style
/// does not apply itself again.
struct AccentSwitchToggleStyle: ToggleStyle {
    func makeBody(configuration: Configuration) -> some View {
        Toggle(configuration).toggleStyle(.switch).tint(Color.accentColor)
    }
}
