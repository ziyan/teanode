import SwiftUI
import TeaNodeKit

/// AppSection is a part of TeaNode the side menu leads to, as the
/// dashboard's sidebar does.
enum AppSection: String, CaseIterable, Identifiable, Hashable {
    case agent, goals, ideas, mail, calendar, contacts, photos, knowledge, finance, recordings, settings

    var id: String { rawValue }

    func title(agentTitle: String) -> String {
        switch self {
        case .agent: agentTitle
        case .goals: String(localized: "Goals")
        case .ideas: String(localized: "Ideas")
        case .mail: String(localized: "Mail")
        case .calendar: String(localized: "Calendar")
        case .contacts: String(localized: "Contacts")
        case .photos: String(localized: "Photos")
        case .knowledge: String(localized: "Knowledge")
        case .finance: String(localized: "Finance")
        case .recordings: String(localized: "Recordings")
        case .settings: String(localized: "Settings")
        }
    }

    var systemImage: String {
        switch self {
        case .agent: "bubble.left"
        case .goals: "target"
        case .ideas: "lightbulb"
        case .mail: "envelope"
        case .calendar: "calendar"
        case .contacts: "person"
        case .photos: "photo.on.rectangle"
        case .knowledge: "brain"
        case .finance: "creditcard"
        case .recordings: "record.circle"
        case .settings: "gearshape"
        }
    }
}

/// OpenMenu opens the side menu, for the button in every screen's bar.
struct OpenMenu {
    let action: () -> Void
    @MainActor
    func callAsFunction() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        action()
    }
}

extension EnvironmentValues {
    @Entry var openMenu = OpenMenu {}
}

/// MenuButton is the button at the top left of every screen that opens the
/// side menu.
struct MenuButton: ToolbarContent {
    @Environment(\.openMenu) private var openMenu
    @Environment(AppModel.self) private var app

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            Button {
                openMenu()
            } label: {
                Label("Menu", systemImage: "line.3.horizontal")
            }
            // Messages waiting in the chats are the news in the menu behind
            // this button, where the chats are.
            .overlay(alignment: .topTrailing) {
                if app.unreadChatCount > 0 {
                    Circle()
                        .fill(.tint)
                        .frame(width: 9, height: 9)
                        .accessibilityHidden(true)
                }
            }
            .accessibilityValue(app.unreadChatCount > 0 ? Text("\(app.unreadChatCount) unread") : Text(verbatim: ""))
        }
    }
}

/// MenuBrand is the TeaNode mark and name at the top of the side menu.
struct MenuBrand: ToolbarContent {
    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            HStack(spacing: 8) {
                Image("Logo")
                    .resizable()
                    .frame(width: 28, height: 28)
                Text(verbatim: "TeaNode")
                    .font(.title3.weight(.semibold))
            }
            .fixedSize()
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
        }
        .sharedBackgroundVisibility(.hidden)
    }
}

/// MainView is the signed-in app: one part of TeaNode on screen, and the
/// side menu under it, as the Claude app keeps its list. The button at the
/// top left or a drag to the right opens the menu.
struct MainView: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @State private var section = AppSection.agent
    @State private var isMenuOpen = false
    @State private var conversation: AgentConversation?
    @State private var mail: MailModel
    @State private var mailPlace: MailPlace?
    @State private var finance: FinanceModel
    @State private var financePart = FinancePart.spending
    @State private var composing: ComposeStart?

    init(connection: Connection, username: String) {
        self.connection = connection
        _mail = State(initialValue: MailModel(connection: connection, username: username))
        _finance = State(initialValue: FinanceModel(connection: connection))
    }

    var body: some View {
        SidePanel(isOpen: $isMenuOpen, isOpenedFromAnywhere: section != .mail) {
            SideMenu(
                connection: connection, mail: mail, section: $section, conversation: $conversation, mailPlace: $mailPlace,
                financePart: $financePart, composing: $composing
            ) {
                isMenuOpen = false
            }
            // Read again each time it opens, for what changed meanwhile.
            .id(isMenuOpen ? "open" : "closed")
        } content: {
            screen
                .environment(\.openMenu, OpenMenu { isMenuOpen = true })
        }
        .environment(\.symbolVariants, .none)
        .sheet(item: $composing) { start in
            ComposeView(mail: mail, start: start).environment(mail)
        }
        .onAppear {
            if let kept = app.keptSection {
                section = kept
                app.keptSection = nil
            }
            #if DEBUG
                if let tab = DemoServer.tab, let chosen = AppSection(rawValue: tab) { section = chosen }
                if DemoServer.screen?.hasPrefix("net-worth") == true { financePart = .netWorth }
                if DemoServer.isMenuRequested { isMenuOpen = true }
            #endif
        }
        .task {
            await app.refreshServices()
            if app.services?.isAssistantEnabled == true { await app.loadAgentName() }
            await app.refreshChats()
            // With the agent off the screen opens on a chat, the newest, and
            // with none on the offer to turn the agent on.
            if !app.isAgentOn, conversation == nil, app.canChat,
                let chats = try? await connection.conversations().filter(\.isChat),
                let newest = chats.max(by: { ($0.lastAt ?? "") < ($1.lastAt ?? "") })
            {
                conversation = newest
            }
            await app.setUpNotifications()
        }
        .task {
            await app.refreshServices()
            if app.services?.isMailboxEnabled == true { await mail.load() }
            if mailPlace == nil, let inbox = mail.folder(kind: "inbox") {
                mailPlace = .folder(inbox.id)
            }
        }
        .onChange(of: app.services) {
            if !app.isSectionAvailable(section) { section = AppSection.allCases.first(where: { app.isSectionAvailable($0) }) ?? .settings }
        }
        .onChange(of: mail.currentMailboxId) {
            if let inbox = mail.folder(kind: "inbox") { mailPlace = .folder(inbox.id) }
        }
        // Initially too: a notification of another account asks for its
        // conversation as the app is drawn anew for that account.
        .onChange(of: app.openedConversationId, initial: true) { _, conversationId in
            guard let conversationId else { return }
            app.openedConversationId = nil
            section = .agent
            isMenuOpen = false
            // An empty id is the main conversation; for another, the kind is
            // learned when the conversation loads.
            conversation = conversationId.isEmpty ? nil : AgentConversation(id: conversationId, kind: "named", title: nil, summary: nil, lastAt: nil)
        }
        .onChange(of: app.askingAbout) { _, reference in
            if reference != nil { section = .agent }
        }
        .onChange(of: app.openedKnowledgePath) { _, knowledgePath in
            if knowledgePath != nil { section = .knowledge }
        }
        .onChange(of: app.openedSharedKnowledge) { _, link in
            if link != nil { section = .knowledge }
        }
        .onChange(of: app.openedMailItemId) { _, itemId in
            if itemId != nil { section = .mail }
        }
        .onChange(of: app.openedContactId) { _, contactId in
            if contactId != nil { section = .contacts }
        }
        .onChange(of: app.openedSection) { _, opened in
            guard let opened else { return }
            app.openedSection = nil
            section = opened
            isMenuOpen = false
        }
        .onChange(of: app.openedFinancePart) { _, part in
            guard let part else { return }
            app.openedFinancePart = nil
            financePart = part
        }
        .sheet(isPresented: Bindable(app).isShowingMailPrograms) {
            NavigationStack {
                if let mailboxId = mail.mailbox?.id {
                    MailAppPasswordsView(mail: mail, mailboxId: mailboxId)
                        .toolbar {
                            ToolbarItem(placement: .confirmationAction) {
                                Button("Done") { app.isShowingMailPrograms = false }
                            }
                        }
                } else {
                    ContentUnavailableView("No mailbox", systemImage: "envelope", description: Text("This account has no mailbox to set up on the phone."))
                }
            }
            .environment(mail)
        }
        .onChange(of: app.composingTo) { _, address in
            guard let address else { return }
            app.composingTo = nil
            composing = ComposeStart(kind: .new, to: address)
        }
        .sheet(isPresented: Bindable(app).isAddingAccount) {
            ConnectView(isAdding: true)
        }
        .alert(
            "TeaNode",
            isPresented: Bindable(app).isShowingOpenedLinkText,
            presenting: app.openedLinkText
        ) { _ in
            Button("OK", role: .cancel) {}
        } message: { text in
            Text(text)
        }
        // While the conversation is on screen the person is here, and
        // notifications that would tell them what they see are held back.
        // Elsewhere in the app they are away from it, so what the agent says
        // first still reaches them, as a banner over the app.
        .task(id: "\(scenePhase)-\(section)") {
            await refreshPresence()
        }
    }

    private func refreshPresence() async {
        let isVisible = scenePhase == .active && section == .agent
        repeat {
            await app.refreshServices()
            if Task.isCancelled { return }
            await app.refreshChats()
            if app.services?.isAssistantEnabled == true || app.canChat { try? await connection.reportPresence(isVisible: isVisible) }
            if isVisible && app.services?.isMailboxEnabled == true { await mail.load() }
            if scenePhase != .active { return }
            do { try await Task.sleep(for: .seconds(30)) } catch { return }
        } while !Task.isCancelled
    }

    @ViewBuilder
    private var screen: some View {
        if app.services == nil, section != .settings, !(section == .recordings && app.isSectionAvailable(.recordings)) {
            // Which services the server runs is not known yet: neither the
            // part nor "off" is true until it is. Recordings stay open
            // offline once the account is known, so a meeting can be recorded.
            NavigationStack {
                ServicesLoadingView()
                    .toolbar {
                        // Only the logo while it loads; the menu returns
                        // when the server cannot be reached.
                        if app.hasServicesFailedToLoad { MenuButton() }
                    }
            }
        } else if !app.isSectionAvailable(section) {
            NavigationStack {
                ServiceOffView(section: section, connection: connection)
                    .toolbar { MenuButton() }
            }
        } else {
            availableScreen
        }
    }

    /// The conversation chosen, or where the main one would be with the
    /// agent off, the offer to turn it on.
    @ViewBuilder
    private var agentScreen: some View {
        if !app.isAgentOn, conversation == nil {
            AgentOffView()
        } else {
            ConversationView(model: ConversationModel(conversation: conversation, app: app, connection: connection))
                .id(conversation?.id ?? "main")
        }
    }

    @ViewBuilder
    private var availableScreen: some View {
        switch section {
        case .agent:
            NavigationStack {
                agentScreen
                    .toolbar { MenuButton() }
                    .withoutSwipeBack()
            }
        case .mail:
            MailScreen(mail: mail, place: $mailPlace, composing: $composing)
                .toast(done: $mail.doneText, problem: $mail.problemText, undo: $mail.undo)
                .environment(mail)
        case .calendar:
            CalendarScreen(connection: connection, username: app.account?.username ?? "")
        case .contacts:
            ContactsScreen(connection: connection)
        case .photos:
            PhotosScreen(connection: connection)
        case .goals:
            GoalsScreen(connection: connection)
        case .ideas:
            IdeasScreen(connection: connection)
        case .knowledge:
            KnowledgeStack(connection: connection)
        case .finance:
            FinanceScreen(finance: finance, part: financePart)
        case .recordings:
            #if DEBUG
            if DemoServer.isRequested, let screen = DemoServer.screen, screen.hasPrefix("recording-") {
                RecordingDemoScreen(connection: connection, screen: screen)
            } else {
                RecordingsScreen(connection: connection)
            }
            #else
            RecordingsScreen(connection: connection)
            #endif
        case .settings:
            NavigationStack {
                SettingsForm()
                    .toolbar { MenuButton() }
                    .withoutSwipeBack()
            }
            .environment(mail)
        }
    }
}

/// KnowledgeStack is the knowledge with its own way back, opening a page a
/// memory link asks for.
struct KnowledgeStack: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            KnowledgeScreen(connection: connection, path: $path)
                .toolbar { MenuButton() }
                .withoutSwipeBack()
                .modifier(KnowledgeDestination(connection: connection, path: $path))
        }
        .onChange(of: app.openedKnowledgePath, initial: true) { _, knowledgePath in
            guard let knowledgePath else { return }
            app.openedKnowledgePath = nil
            path = NavigationPath()
            path.append(KnowledgePlace.page(knowledgePath))
        }
        .onChange(of: app.openedSharedKnowledge, initial: true) { _, link in
            guard let link else { return }
            app.openedSharedKnowledge = nil
            // The link names the person by username; the page is asked for
            // by the identifier they are listed under, which only the pages
            // shared with this person can say.
            Task {
                let roots = (try? await connection.sharedKnowledgeRoots()) ?? []
                guard let owner = roots.first(where: { $0.ownerUsername.caseInsensitiveCompare(link.ownerUsername) == .orderedSame })
                else { return }
                path = NavigationPath()
                path.append(KnowledgePlace.sharedPage(ownerUserId: owner.ownerUserId, path: link.path))
            }
        }
        #if DEBUG
            // A screenshot of a page with its records linked
            // (`-TeaNodeScreen knowledge-records`), or scrolled to the recording
            // row with its passage count (`knowledge-records-recording`).
            .task {
                if DemoServer.isRequested, ["knowledge-records", "knowledge-records-recording"].contains(DemoServer.screen), path.isEmpty {
                    path.append(KnowledgePlace.page("people/dana-fixture"))
                }
            }
        #endif
    }
}

/// SideMenu is the menu under the screen: TeaNode's mark, the parts of
/// TeaNode, and under them the list of the part on screen: the
/// conversations with the agent, or the mail folders.
struct SideMenu: View {
    let connection: Connection
    let mail: MailModel
    @Binding var section: AppSection
    @Binding var conversation: AgentConversation?
    @Binding var mailPlace: MailPlace?
    @Binding var financePart: FinancePart
    @Binding var composing: ComposeStart?
    let onClose: () -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        switch section {
        case .agent:
            ConversationsSheet(connection: connection, isPanel: true, currentId: conversation?.id, leading: AnyView(sections)) { chosen in
                conversation = chosen
                onClose()
            }
        case .mail:
            NavigationStack {
                MailboxesList(mail: mail, current: mailPlace, composing: $composing, leading: AnyView(sections)) { chosen in
                    mailPlace = chosen
                    onClose()
                }
                .menuListStyle()
                .navigationBarTitleDisplayMode(.inline)
            }
        case .finance:
            NavigationStack {
                List {
                    Section { sections }
                        .listSectionSeparator(.hidden)
                    Section("Finance") {
                        ForEach(FinancePart.allCases) { part in
                            Button {
                                financePart = part
                                onClose()
                            } label: {
                                Label(part.title, systemImage: part.systemImage)
                                    .foregroundStyle(.primary)
                            }
                            .menuRow(isSelected: financePart == part)
                        }
                    }
                    .listSectionSeparator(.hidden)
                }
                .menuListStyle()
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { MenuBrand() }
            }
        default:
            NavigationStack {
                List {
                    Section { sections }
                        .listSectionSeparator(.hidden)
                }
                .menuListStyle()
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { MenuBrand() }
            }
        }
    }

    private var sections: some View {
        ForEach(AppSection.allCases.filter { app.isSectionAvailable($0) }) { part in
            Button {
                if section == part {
                    onClose()
                } else {
                    section = part
                    // The agent's conversations, the mail folders and the
                    // finance sections stay open to choose from; the other
                    // parts open at once.
                    if part != .agent, part != .mail, part != .finance { onClose() }
                }
            } label: {
                // The part on screen is marked by its color and weight; the
                // highlight is kept for the conversation or folder chosen.
                Label(part.title(agentTitle: app.agentTitle), systemImage: part.systemImage)
                    .fontWeight(section == part ? .semibold : .regular)
                    .foregroundStyle(section == part ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
            }
            .menuRow(isSelected: false)
            .accessibilityAddTraits(section == part ? .isSelected : [])
            // The chats are in the agent's part: what waits in them is the
            // number on its entry.
            .badge(part == .agent ? app.unreadChatCount : 0)
        }
    }
}

/// ServiceOffView is a part of TeaNode the server has turned off: which one,
/// and how it is turned on again, with the way there for whoever manages the
/// server.
struct ServiceOffView: View {
    let section: AppSection
    let connection: Connection
    @State private var canManageServer = false

    var body: some View {
        ContentUnavailableView {
            Label(String(localized: "\(section.title) is off"), systemImage: section.systemImage)
        } description: {
            if canManageServer {
                Text("It is turned off for this server. Turn it on in Services.")
            } else {
                Text("It is turned off for this server. Whoever manages your TeaNode server can turn it on.")
            }
        } actions: {
            if canManageServer {
                NavigationLink {
                    ServerServicesView(connection: connection)
                } label: {
                    Label("Open Services", systemImage: "switch.2")
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .task(id: section) {
            canManageServer = (try? await connection.operatorAccess())?.canManageServer ?? false
        }
    }
}

/// ServicesLoadingView is the app asking the server which services it runs,
/// at launch, and what it says when the server cannot be reached.
struct ServicesLoadingView: View {
    @Environment(AppModel.self) private var app
    @State private var isRetrying = false

    var body: some View {
        if app.hasServicesFailedToLoad {
            ContentUnavailableView {
                Label("Cannot reach your server", systemImage: "wifi.exclamationmark")
            } description: {
                Text("TeaNode could not ask your server what it runs. Check the connection and try again.")
            } actions: {
                Button {
                    isRetrying = true
                    Task {
                        await app.refreshServices()
                        isRetrying = false
                    }
                } label: {
                    if isRetrying { ProgressView() } else { Text("Try again") }
                }
                .buttonStyle(.borderedProminent)
                .disabled(isRetrying)
            }
        } else {
            // The logo, as the app opens, with a quiet spinner under it; the
            // words are for VoiceOver, which reads the spinner's label.
            VStack(spacing: 24) {
                Image("Logo")
                    .resizable()
                    .scaledToFit()
                    .frame(width: 96, height: 96)
                    .clipShape(.rect(cornerRadius: 22))
                    .accessibilityHidden(true)
                ProgressView()
                    .accessibilityLabel("Connecting to your server")
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

/// AgentOffView is where the main conversation would be for a person who has
/// not turned their agent on: the offer to, with the way to Settings. Their
/// chats are in the menu, and need no agent.
struct AgentOffView: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        ContentUnavailableView {
            Label("Your agent is not on", systemImage: "bubble.left")
        } description: {
            Text("Your agent is not on yet. Each person turns on their own, under Settings, Your agent.")
        } actions: {
            Button {
                app.openedSection = .settings
            } label: {
                Label("Open Settings", systemImage: "gearshape")
            }
            .buttonStyle(.borderedProminent)
        }
    }
}
