import SwiftUI
import TeaNodeKit

/// AppSection is a part of TeaNode the side menu leads to, as the
/// dashboard's sidebar does.
enum AppSection: String, CaseIterable, Identifiable, Hashable {
    case agent, goals, ideas, mail, calendar, contacts, knowledge, finance, settings

    var id: String { rawValue }

    func title(agentTitle: String) -> String {
        switch self {
        case .agent: agentTitle
        case .goals: String(localized: "Goals")
        case .ideas: String(localized: "Ideas")
        case .mail: String(localized: "Mail")
        case .calendar: String(localized: "Calendar")
        case .contacts: String(localized: "Contacts")
        case .knowledge: String(localized: "Knowledge")
        case .finance: String(localized: "Finance")
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
        case .knowledge: "brain"
        case .finance: "creditcard"
        case .settings: "gearshape"
        }
    }
}

/// OpenMenu opens the side menu, for the button in every screen's bar.
struct OpenMenu {
    let action: () -> Void
    func callAsFunction() { action() }
}

extension EnvironmentValues {
    @Entry var openMenu = OpenMenu {}
}

/// MenuButton is the button at the top left of every screen that opens the
/// side menu.
struct MenuButton: ToolbarContent {
    @Environment(\.openMenu) private var openMenu

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            Button {
                openMenu()
            } label: {
                Label("Menu", systemImage: "line.3.horizontal")
            }
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
            #if DEBUG
                if let tab = DemoServer.tab, let chosen = AppSection(rawValue: tab) { section = chosen }
                if DemoServer.isMenuRequested { isMenuOpen = true }
            #endif
        }
        .task {
            await app.loadAgentName()
            await app.setUpNotifications()
        }
        .task {
            await mail.load()
            if mailPlace == nil, let inbox = mail.folder(kind: "inbox") {
                mailPlace = .folder(inbox.id)
            }
        }
        .onChange(of: mail.currentMailboxId) {
            if let inbox = mail.folder(kind: "inbox") { mailPlace = .folder(inbox.id) }
        }
        .onChange(of: app.openedConversationId) { _, conversationId in
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
        .onChange(of: app.openedMailItemId) { _, itemId in
            if itemId != nil { section = .mail }
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
        .alert(
            "Already connected",
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
            let isVisible = scenePhase == .active && section == .agent
            try? await connection.reportPresence(isVisible: isVisible)
            if isVisible { await mail.load() }
            while isVisible, !Task.isCancelled {
                try? await Task.sleep(for: .seconds(30))
                await mail.load()
                try? await connection.reportPresence(isVisible: true)
            }
        }
    }

    @ViewBuilder
    private var screen: some View {
        switch section {
        case .agent:
            NavigationStack {
                ConversationView(model: ConversationModel(conversation: conversation, app: app, connection: connection))
                    .id(conversation?.id ?? "main")
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
        case .goals:
            GoalsScreen(connection: connection)
        case .ideas:
            IdeasScreen(connection: connection)
        case .knowledge:
            KnowledgeStack(connection: connection)
        case .finance:
            FinanceScreen(finance: finance, part: financePart)
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
                    Section { sections } footer: { MenuSectionBreak() }
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
                }
                .menuListStyle()
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { MenuBrand() }
            }
        default:
            NavigationStack {
                List {
                    Section { sections }
                }
                .menuListStyle()
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { MenuBrand() }
            }
        }
    }

    private var sections: some View {
        ForEach(AppSection.allCases) { part in
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
        }
    }
}
