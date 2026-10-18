import SwiftUI
import TeaNodeKit

/// AppSection is a part of TeaNode the side menu leads to, as the
/// dashboard's sidebar does.
enum AppSection: String, CaseIterable, Identifiable, Hashable {
    case agent, goals, ideas, mail, calendar, contacts, photos, location, knowledge, finance, recordings, coding, settings

    var id: String { rawValue }

    /// The part's name in the side menu. The agent's part is Chats: it holds the
    /// chats with people and groups as well as the agent's own.
    var title: String {
        switch self {
        case .agent: String(localized: "Chats")
        case .goals: String(localized: "Goals")
        case .ideas: String(localized: "Ideas")
        case .mail: String(localized: "Mail")
        case .calendar: String(localized: "Calendar")
        case .location: String(localized: "Location")
        case .contacts: String(localized: "Contacts")
        case .photos: String(localized: "Photos")
        case .knowledge: String(localized: "Knowledge")
        case .finance: String(localized: "Finance")
        case .recordings: String(localized: "Recordings")
        case .coding: String(localized: "Coding")
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
        case .location: "location"
        case .contacts: "person"
        case .photos: "photo.on.rectangle"
        case .knowledge: "brain"
        case .finance: "creditcard"
        case .recordings: "record.circle"
        case .coding: "chevron.left.forwardslash.chevron.right"
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
    /// The list under Location's map, which the side menu chooses.
    @State private var locationSegment = LocationSegment.visits
    /// The photo library, held here so the side menu can list its collections
    /// as it lists the mail folders.
    @State private var photos: PhotosModel
    @State private var composing: ComposeStart?
    /// How many times the side menu has been opened, for what it reads again
    /// each time. The menu itself is kept while closed, so its lists stay
    /// where they were scrolled.
    @State private var menuOpeningCount = 0
    /// Whether the services, the agent's identity and the chats have been
    /// read once. Until then it is not known whether the agent is on, and
    /// drawing the conversation or the offer to turn the agent on would be a
    /// guess that the next read undoes.
    @State private var hasReadAgentFacts = false

    init(connection: Connection, username: String) {
        self.connection = connection
        _mail = State(initialValue: MailModel(connection: connection, username: username))
        _finance = State(initialValue: FinanceModel(connection: connection))
        _photos = State(initialValue: PhotosModel.start(connection: connection))
    }

    var body: some View {
        SidePanel(isOpen: $isMenuOpen, isOpenedFromAnywhere: section != .mail && section != .location) {
            SideMenu(
                connection: connection, mail: mail, photos: photos, section: $section, conversation: $conversation, mailPlace: $mailPlace,
                financePart: $financePart, locationSegment: $locationSegment, composing: $composing, openingCount: menuOpeningCount
            ) {
                isMenuOpen = false
            }
        } content: {
            screen
                .environment(\.openMenu, OpenMenu { isMenuOpen = true })
        }
        .environment(\.symbolVariants, .none)
        .sheet(item: $composing) { start in
            ComposeView(mail: mail, start: start).environment(mail)
        }
        .onChange(of: isMenuOpen) { _, isOpen in
            if isOpen { menuOpeningCount += 1 }
        }
        #if DEBUG
            .task(id: app.canChat) {
                // `-TeaNodeScreen conversations-restored`: the menu, once it has
                // scrolled to its chats (a second after it does, in the list),
                // closed and opened again, to show it opens where it was left.
                guard DemoServer.isRequested, DemoServer.screen == "conversations-restored", app.canChat else { return }
                try? await Task.sleep(for: .seconds(3))
                isMenuOpen = false
                try? await Task.sleep(for: .milliseconds(800))
                isMenuOpen = true
            }
            .task {
                // `-TeaNodeScreen finance-part-switch`: Net worth is chosen
                // after FinanceScreen has opened a transaction list.
                guard DemoServer.isRequested, DemoServer.screen == "finance-part-switch" else { return }
                try? await Task.sleep(for: .milliseconds(2500))
                financePart = .netWorth
            }
        #endif
        .onAppear {
            if let kept = app.keptSection {
                section = kept
                app.keptSection = nil
            }
            #if DEBUG
                if let tab = DemoServer.tab, let chosen = AppSection(rawValue: tab) { section = chosen }
                if DemoServer.screen?.hasPrefix("net-worth") == true { financePart = .netWorth }
                if DemoServer.screen?.hasPrefix("finance-") == true { financePart = .accounts }
                if let screen = DemoServer.screen, ["location-places", "location-place", "location-address"].contains(screen) { locationSegment = .places }
                if DemoServer.screen == "location-sharing" { locationSegment = .sharing }
                if DemoServer.isMenuRequested { isMenuOpen = true }
                if DemoServer.isRequested, DemoServer.tab == "coding" { app.noteCodingComputers() }
                // A chat opened for a screenshot (`-TeaNodeScreen chat-direct`, `chat-group`, `chat-members`, `chat-mention`).
                if let chat = DemoServer.openedChat { conversation = chat }
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
            hasReadAgentFacts = true
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
            // Location and Coding are judged once the permissions and the computers
            // have been read, not by services.
            if section != .location, section != .coding, !app.isSectionAvailable(section) { section = AppSection.allCases.first(where: { app.isSectionAvailable($0) }) ?? .settings }
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
        // first still reaches them, as a banner over the app. A chat is held
        // back only while that chat is the one on screen, so the report
        // names the conversation, and is made again when it changes.
        .task(id: "\(scenePhase)-\(section)-\(conversation?.id ?? "")") {
            await refreshPresence()
        }
        // Which computers run herdr is asked in a task of its own, so a slow
        // computer never holds up the mail above.
        .task(id: "\(scenePhase)-\(app.account?.accountKey ?? "")") {
            await refreshCodingPresenceSlowly()
        }
    }

    /// Asks at launch, when the app comes to the front and when the account
    /// changes, then every half minute, so a question shows on the Coding
    /// row soon after it comes. Not while the Coding section is on screen: it reads the list
    /// itself and says what it finds.
    private func refreshCodingPresenceSlowly() async {
        guard scenePhase == .active else { return }
        // Whether the assistant is on decides it, and at launch that is not
        // known yet.
        if app.services == nil { await app.refreshServices() }
        repeat {
            if section != .coding { await app.refreshCodingPresence() }
            do { try await Task.sleep(for: .seconds(30)) } catch { return }
        } while !Task.isCancelled && scenePhase == .active
    }

    private func refreshPresence() async {
        let isVisible = scenePhase == .active && section == .agent
        // The main conversation has no id until it is loaded and is never a
        // chat, so it is reported as none.
        let shownConversationId = isVisible ? conversation?.id ?? "" : ""
        repeat {
            await app.refreshServices()
            if Task.isCancelled { return }
            await app.refreshChats()
            if app.services?.isAssistantEnabled == true || app.canChat { try? await connection.reportPresence(isVisible: isVisible, conversationId: shownConversationId) }
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
        } else if section == .location, !app.hasReadPermissions {
            // Whether the account may use Location is read with the chats; a
            // Location link opened at launch waits for it rather than being
            // told it is refused.
            NavigationStack {
                ServicesLoadingView(isWaitingForPermissions: true)
                    .toolbar { MenuButton() }
            }
        } else if section == .location, !app.canUseLocation {
            NavigationStack {
                LocationNotAllowedView()
                    .toolbar { MenuButton() }
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
        if !hasReadAgentFacts, conversation == nil {
            // A conversation chosen from a notification or the menu is shown as
            // asked; the rest waits until it is known what to show.
            ServicesLoadingView()
        } else if !app.isAgentOn, conversation == nil {
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
        case .location:
            LocationScreen(connection: connection, segment: $locationSegment)
        case .contacts:
            ContactsScreen(connection: connection)
        case .photos:
            PhotosScreen(model: photos)
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
        case .coding:
            CodingScreen(connection: connection)
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
                let roots: [SharedKnowledgeRoot]
                do {
                    roots = try await connection.sharedKnowledgeRoots()
                } catch {
                    app.openedLinkText = app.describe(error)
                    return
                }
                // Not among those who share with this person: the owner
                // stopped, or never did.
                guard let owner = roots.first(where: { $0.ownerUsername.caseInsensitiveCompare(link.ownerUsername) == .orderedSame })
                else {
                    app.openedLinkText = String(localized: "This page is no longer shared with you")
                    return
                }
                path = NavigationPath()
                path.append(KnowledgePlace.sharedPage(ownerUserId: owner.ownerUserId, path: link.path))
            }
        }
        #if DEBUG
            // A screenshot of a page with its records linked
            // (`-TeaNodeScreen knowledge-records`), or scrolled to the recording
            // row with its passage count (`knowledge-records-recording`), or of a
            // page another person shared (`knowledge-shared-page`).
            .task {
                if DemoServer.isRequested, ["knowledge-records", "knowledge-records-recording"].contains(DemoServer.screen), path.isEmpty {
                    path.append(KnowledgePlace.page("people/dana-fixture"))
                }
                if DemoServer.isRequested, DemoServer.screen == "knowledge-shared-page", path.isEmpty {
                    path.append(KnowledgePlace.sharedPage(ownerUserId: "sam", path: "recipes/apple-tart"))
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
    let photos: PhotosModel
    @Binding var section: AppSection
    @Binding var conversation: AgentConversation?
    @Binding var mailPlace: MailPlace?
    @Binding var financePart: FinancePart
    @Binding var locationSegment: LocationSegment
    @Binding var composing: ComposeStart?
    /// How many times the menu has been opened: the conversations are read
    /// again each time.
    let openingCount: Int
    let onClose: () -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        switch section {
        case .agent:
            ConversationsSheet(
                connection: connection, isPanel: true, currentId: conversation?.id, leading: AnyView(sections),
                openingCount: openingCount
            ) { chosen in
                conversation = chosen
                onClose()
            }
        case .mail:
            NavigationStack {
                ScrollViewReader { scroller in
                    MailboxesList(
                        mail: mail, current: mailPlace, composing: $composing, leading: AnyView(sections),
                        lastRowId: Self.partItemId(Self.mailLastRowName)
                    ) { chosen in
                        mailPlace = chosen
                        onClose()
                    }
                    .menuListStyle()
                    .navigationBarTitleDisplayMode(.inline)
                    .task(id: [availableSectionCount, mail.folders.count]) { await showPartItems(scroller, lastItemId: Self.mailLastRowName) }
                }
            }
        case .finance:
            NavigationStack {
                ScrollViewReader { scroller in
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
                                .id(Self.partItemId(part.id))
                                .menuRow(isSelected: financePart == part)
                            }
                        }
                        .listSectionSeparator(.hidden)
                    }
                    .menuListStyle()
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar { MenuBrand() }
                    .task(id: [availableSectionCount, FinancePart.allCases.count]) { await showPartItems(scroller, lastItemId: FinancePart.allCases.last?.id) }
                }
            }
        case .location:
            NavigationStack {
                ScrollViewReader { scroller in
                    List {
                        Section { sections }
                            .listSectionSeparator(.hidden)
                        Section("Location") {
                            ForEach(LocationSegment.allCases) { segment in
                                Button {
                                    locationSegment = segment
                                    onClose()
                                } label: {
                                    Label(segment.title, systemImage: segment.systemImage)
                                        .foregroundStyle(.primary)
                                }
                                .id(Self.partItemId(segment.id))
                                .menuRow(isSelected: locationSegment == segment)
                            }
                        }
                        .listSectionSeparator(.hidden)
                    }
                    .menuListStyle()
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar { MenuBrand() }
                    .task(id: [availableSectionCount, LocationSegment.allCases.count]) { await showPartItems(scroller, lastItemId: LocationSegment.allCases.last?.id) }
                }
            }
        case .photos:
            NavigationStack {
                ScrollViewReader { scroller in
                    List {
                        Section { sections }
                            .listSectionSeparator(.hidden)
                        Section("Photos") {
                            ForEach(photos.sections) { collection in
                                Button {
                                    photos.section = collection
                                    onClose()
                                } label: {
                                    Label(collection.title, systemImage: collection.symbolName)
                                        .foregroundStyle(.primary)
                                }
                                .id(Self.partItemId(collection.id))
                                .menuRow(isSelected: photos.section == collection)
                            }
                        }
                        .listSectionSeparator(.hidden)
                    }
                    .menuListStyle()
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar { MenuBrand() }
                    .task(id: [availableSectionCount, photos.sections.count]) { await showPartItems(scroller, lastItemId: photos.sections.last?.id) }
                }
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

    /// How many parts the menu lists. Some are known only once the server has answered (whether
    /// it runs the agent, has coding computers, keeps recordings), and each one that appears
    /// pushes the items of the part on screen further down.
    private var availableSectionCount: Int { AppSection.allCases.filter { app.isSectionAvailable($0) }.count }

    /// The name of the mail list's last row, which the menu scrolls to as it does to the
    /// last item of the other parts. The menu is kept while closed, so it opens where the
    /// person left it, and the scroll runs only when the list first appears or grows.
    private static let mailLastRowName = "mailLastRow"

    /// The id of a row that lists one of the items of the part on screen, under the list of the parts.
    private static func partItemId(_ itemId: some Hashable) -> String { "partItem \(itemId)" }

    /// Scrolls the menu down to the items of the part on screen: they are what the person opened
    /// the menu for, and under all the parts they would otherwise start below the bottom of the
    /// screen. The last item is brought to the bottom, so the parts stay in view above the items
    /// for as long as there is room. It runs when the part's list first appears, and again when
    /// a part or an item appears and pushes the items down; otherwise the menu, kept while
    /// closed, opens where it was left. A scroll asked for before the list has
    /// laid out its rows finds nothing to scroll to, and the menu slides in as it opens, so the
    /// scroll is asked for once the rows are there and again once the menu is in place.
    private func showPartItems(_ scroller: ScrollViewProxy, lastItemId: (some Hashable)?) async {
        guard let lastItemId else { return }
        for delay in [50, 400] {
            try? await Task.sleep(for: .milliseconds(delay))
            guard !Task.isCancelled else { return }
            scroller.scrollTo(Self.partItemId(lastItemId), anchor: .bottom)
        }
    }

    private var sections: some View {
        ForEach(AppSection.allCases.filter { app.isSectionAvailable($0) }) { part in
            Button {
                if section == part {
                    onClose()
                } else {
                    section = part
                    // The agent's conversations, the mail folders, the
                    // finance sections, Location's lists and the photo
                    // collections stay open to choose from; the other parts
                    // open at once.
                    if part != .agent, part != .mail, part != .finance, part != .location, part != .photos { onClose() }
                }
            } label: {
                // The part on screen is marked by its color and weight; the
                // highlight is kept for the conversation or folder chosen.
                Label(part.title, systemImage: part.systemImage)
                    .fontWeight(section == part ? .semibold : .regular)
                    .foregroundStyle(section == part ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
            }
            // The chats are in the agent's part: what waits in them is the
            // number on its entry. Before menuRow, so the row's hidden
            // separator stays the outermost of its modifiers.
            // The Coding row counts the sessions that ask something.
            .badge(part == .agent ? app.unreadChatCount : part == .coding ? app.codingAskingCount : 0)
            .menuRow(isSelected: false)
            .accessibilityAddTraits(section == part ? .isSelected : [])
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

/// LocationNotAllowedView is what Location says to an account that may not
/// use it: that is a permission, not a service the server has turned off.
struct LocationNotAllowedView: View {
    var body: some View {
        ContentUnavailableView {
            Label("Location is not available to you", systemImage: AppSection.location.systemImage)
        } description: {
            Text("Your account is not allowed to record or share where you go. Whoever manages your TeaNode server can allow it.")
        }
    }
}

/// ServicesLoadingView is the app asking the server which services it runs,
/// at launch, and what it says when the server cannot be reached.
struct ServicesLoadingView: View {
    @Environment(AppModel.self) private var app
    @State private var isRetrying = false

    /// Waiting for the account's permissions rather than the services: the
    /// failure and the retry are about reading those.
    var isWaitingForPermissions = false

    private var hasFailed: Bool { isWaitingForPermissions ? app.hasPermissionReadFailed : app.hasServicesFailedToLoad }

    var body: some View {
        if hasFailed {
            ContentUnavailableView {
                Label("Cannot reach your server", systemImage: "wifi.exclamationmark")
            } description: {
                if isWaitingForPermissions {
                    Text("TeaNode could not ask your server what your account may use. Check the connection and try again.")
                } else {
                    Text("TeaNode could not ask your server what it runs. Check the connection and try again.")
                }
            } actions: {
                Button {
                    isRetrying = true
                    Task {
                        if isWaitingForPermissions { await app.refreshChats() } else { await app.refreshServices() }
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
/// chats are in the menu, and need no agent. A person who has no agent at
/// all is offered nothing to turn on: only their chats.
struct AgentOffView: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        if !app.hasAgent {
            ContentUnavailableView(
                "Your chats are in the menu", systemImage: "bubble.left",
                description: Text("Open the menu to read and write to the people you share a circle with."))
        } else {
            offer
        }
    }

    private var offer: some View {
        ContentUnavailableView {
            Label("Your agent is off", systemImage: "bubble.left")
        } description: {
            Text("Each person turns on their own agent, under Settings, Your agent. Once it is on, it can answer questions, sort your mail and act for you.")
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
