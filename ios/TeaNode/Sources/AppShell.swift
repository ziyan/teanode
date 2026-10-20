import SwiftUI
import TeaNodeKit

/// AppSection is a part of TeaNode, as the dashboard's sidebar has them: a
/// tab of the tab bar, a row of the More tab, or a row of the iPad's sidebar.
enum AppSection: String, CaseIterable, Identifiable, Hashable {
    case agent, goals, ideas, mail, calendar, contacts, photos, location, health, knowledge, finance, recordings, coding, settings

    var id: String { rawValue }

    /// The part's name in the tab bar's accessibility label, the More list and the
    /// sidebar. The agent's part is Chats: it holds the chats with people and
    /// groups as well as the agent's own.
    var title: String {
        switch self {
        case .agent: String(localized: "Chats")
        case .goals: String(localized: "Goals")
        case .ideas: String(localized: "Ideas")
        case .mail: String(localized: "Mail")
        case .calendar: String(localized: "Calendar")
        case .location: String(localized: "Location")
        case .health: HealthWords.title
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
        case .health: "heart.text.square"
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

/// MainView is the signed-in app: a tab bar of icons, with Chats, Mail, Photos
/// and Calendar, then More, which holds the other parts, and Search. The bar
/// shrinks to a small pill while a list scrolls. Where the window is wide, as
/// on an iPad, the same tabs are a sidebar, and the parts that are under More
/// on the phone are tabs in its groups.
struct MainView: View {
    let connection: Connection

    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @State private var tab = MainTab.section(.agent)
    /// The parts the person keeps in the iPhone's tab bar (TabBarChoice).
    @AppStorage(TabBarChoice.storageKey) private var tabBarText = TabBarChoice.defaultText
    /// How the person arranged the iPad's sidebar, kept on this device.
    @AppStorage("sidebarCustomization") private var sidebarCustomization: TabViewCustomization

    private var tabBarSections: [AppSection] { TabBarChoice.sections(from: tabBarText) }
    /// The part open under the More tab; nil is the list of parts.
    @State private var moreSection: AppSection?
    /// A part a link asked for in a wide window, before it was known whether
    /// the sidebar has it; it is opened once that is known.
    @State private var pendingSection: AppSection?
    /// What is pushed on the list of conversations in the Chats tab.
    @State private var chatPath: [ChatPlace] = []
    @State private var mail: MailModel
    @State private var finance: FinanceModel
    @State private var financePart = FinancePart.spending
    /// The list under Location's map, which the menu under its title chooses.
    @State private var locationSegment = LocationSegment.visits
    /// The photo library, held here so a link can open it on a filter.
    @State private var photos: PhotosModel
    @State private var composing: ComposeStart?
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
        // In parts: as one chain of modifiers the body was more than the
        // compiler would type-check in reasonable time.
        presented
            // While the conversation is on screen the person is here, and
            // notifications that would tell them what they see are held back.
            // Elsewhere in the app they are away from it, so what the agent says
            // first still reaches them, as a banner over the app. A chat is held
            // back only while that chat is the one on screen, so the report
            // names the conversation, and is made again when it changes.
            .task(id: presenceKey) {
                await refreshPresence()
            }
            // Which computers run herdr is asked in a task of its own, so a slow
            // computer never holds up the mail above.
            .task(id: "\(scenePhase)-\(app.account?.accountKey ?? "")") {
                await refreshCodingPresenceSlowly()
            }
    }

    /// The tabs with what they open by, and what is shown over them.
    private var presented: some View {
        routed
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
    }

    /// Every request to open something that arrives from outside a tab: a
    /// notification, a link, the agent, the Search tab. Each selects the tab
    /// that holds it, and the part takes the rest when it is drawn.
    private var routed: some View {
        routedWithState
            // Initially too: a notification of another account asks for its
            // conversation as the app is drawn anew for that account.
            .onChange(of: app.openedConversationId, initial: true) { _, conversationId in
                guard let conversationId else { return }
                app.openedConversationId = nil
                show(.agent)
                // An empty id is the main conversation; for another, the kind is
                // learned when the conversation loads.
                if conversationId.isEmpty {
                    chatPath = [ChatPlace.main]
                } else {
                    chatPath = [ChatPlace.conversation(AgentConversation(id: conversationId, kind: "named", title: nil, summary: nil, lastAt: nil))]
                }
            }
            .onChange(of: app.askingAbout) { _, reference in
                guard reference != nil else { return }
                show(.agent)
                // The conversation on screen takes it; with the list showing, the main one opens to.
                if chatPath.isEmpty { chatPath = [ChatPlace.main] }
            }
            .onChange(of: app.openedKnowledgePath) { _, knowledgePath in
                if knowledgePath != nil { show(.knowledge) }
            }
            .onChange(of: app.openedSharedKnowledge) { _, link in
                if link != nil { show(.knowledge) }
            }
            .onChange(of: app.openedMailItemId) { _, itemId in
                if itemId != nil { show(.mail) }
            }
            .onChange(of: app.openedContactId) { _, contactId in
                if contactId != nil { show(.contacts) }
            }
            .onChange(of: app.openedPhotoPerson) { _, person in
                guard person != nil else { return }
                // Photos takes the request when it is drawn; when it cannot be shown the request ends here.
                if app.isSectionAvailable(.photos) {
                    show(.photos)
                } else {
                    app.openedPhotoPerson = nil
                }
            }
            .onChange(of: app.openedSearchedPhoto) { _, searchedPhoto in
                if searchedPhoto != nil { show(.photos) }
            }
    }

    /// The requests that set something of the part they open as well.
    private var routedWithState: some View {
        selectable
            .onChange(of: app.openedSection) { _, opened in
                guard let opened else { return }
                app.openedSection = nil
                show(opened)
            }
            .onChange(of: app.openedPhotoFilter) { _, photoFilter in
                guard let photoFilter else { return }
                app.openedPhotoFilter = nil
                photos.section = .library
                photos.photoFilter = photoFilter
                show(.photos)
            }
            .onChange(of: app.openedFinancePart) { _, part in
                guard let part else { return }
                app.openedFinancePart = nil
                financePart = part
            }
    }

    /// The tabs, with what keeps the selection to a tab that exists.
    private var selectable: some View {
        tabs
            .sheet(item: $composing) { start in
                ComposeView(mail: mail, start: start).environment(mail)
            }
            .onChange(of: app.services) {
                settleSelection()
                retryPendingSection()
            }
            .onChange(of: app.hasReadPermissions) {
                retryPendingSection()
            }
            .onChange(of: horizontalSizeClass) {
                adaptToSizeClass()
            }
            .onChange(of: tabBarText) {
                // A part taken out of the bar while it is open stays open,
                // under More.
                adaptToSizeClass()
            }
            .onAppear {
                // Chats may not be in the bar the person chose.
                adaptToSizeClass()
                if let kept = app.keptSection {
                    show(kept)
                    app.keptSection = nil
                }
                #if DEBUG
                    openDemoScreen()
                #endif
            }
            .task {
                await app.refreshServices()
                if app.services?.isAssistantEnabled == true { await app.loadAgentName() }
                await app.refreshChats()
                hasReadAgentFacts = true
                await app.setUpNotifications()
            }
            .task {
                await app.refreshServices()
                if app.services?.isMailboxEnabled == true { await mail.load() }
            }
    }

    // MARK: Tabs

    /// The tab bar: an icon for each tab, named for VoiceOver. A wide window
    /// has room to show the names, which the sidebar needs.
    private var tabs: some View {
        TabView(selection: $tab) {
            ForEach(shownBarSections) { section in
                sectionTab(section)
            }
            if horizontalSizeClass == .regular {
                ForEach(shownGroups) { group in
                    TabSection(group.title) {
                        ForEach(shownSections(in: group)) { section in
                            sectionTab(section)
                        }
                    }
                }
                sectionTab(.settings)
            }
            if horizontalSizeClass != .regular {
                Tab(value: MainTab.more) {
                    moreTab
                } label: {
                    Image(systemName: "ellipsis")
                        .accessibilityLabel("More")
                }
                .badge(moreBadgeCount)
                .customizationID("tab.more")
            }
            Tab(value: MainTab.search, role: .search) {
                SearchTabScreen(connection: connection, onOpen: openSearchResult, onShowAll: showAllInPart)
            }
            .customizationID("tab.search")
        }
        .tabViewStyle(.sidebarAdaptable)
        .tabViewCustomization($sidebarCustomization)
        // On the iPad the tabs are the sidebar from the start, as the side
        // menu was pinned, rather than a bar of tabs along the top.
        .defaultAdaptableTabBarPlacement(.sidebar)
        .tabBarMinimizeBehavior(.onScrollDown)
        .environment(\.symbolVariants, .none)
    }

    private func sectionTab(_ section: AppSection) -> some TabContent<MainTab> {
        Tab(value: MainTab.section(section)) {
            sectionScreen(section)
        } label: {
            sectionTabLabel(section)
        }
        .badge(badgeCount(of: section))
        .customizationID("tab.\(section.rawValue)")
    }

    @ViewBuilder
    private func sectionTabLabel(_ section: AppSection) -> some View {
        if horizontalSizeClass == .regular {
            Label(section.title, systemImage: section.systemImage)
        } else {
            Image(systemName: section.systemImage)
                .accessibilityLabel(section.title)
        }
    }

    /// What waits in the parts under More, for the More tab's badge.
    private var moreBadgeCount: Int {
        AppSection.allCases.filter { !tabBarSections.contains($0) }.reduce(0) { $0 + badgeCount(of: $1) }
    }

    /// What waits in a part: the unread chats, and the coding sessions that
    /// ask something.
    private func badgeCount(of section: AppSection) -> Int {
        switch section {
        case .agent: app.unreadChatCount
        case .coding: app.codingAskingCount
        default: 0
        }
    }

    /// The More tab: the list of the other parts, or the part chosen in it,
    /// which has a button back to the list.
    private var moreTab: some View {
        ZStack {
            if let moreSection {
                sectionScreen(moreSection)
                    .environment(\.leavePart, LeavePart { self.moreSection = nil })
                    .transition(.opacity)
            } else {
                MoreList { opened in moreSection = opened }
                    .transition(.opacity)
            }
        }
        .animation(.default, value: moreSection)
    }

    /// Whether a part has a tab of its own. Until the server has said which
    /// services it runs, the tabs of the phone are all there and say that
    /// they are loading, rather than coming one by one.
    private func isTabShown(_ section: AppSection) -> Bool {
        if app.services == nil { return tabBarSections.contains(section) || section == .settings }
        return app.isSectionAvailable(section)
    }

    private var shownBarSections: [AppSection] {
        tabBarSections.filter { isTabShown($0) }
    }

    /// The parts of a group the sidebar lists under it: those the server
    /// offers, and not already a tab above the groups.
    private func shownSections(in group: AppSectionGroup) -> [AppSection] {
        group.sections.filter { app.isSectionAvailable($0) && !tabBarSections.contains($0) }
    }

    private var shownGroups: [AppSectionGroup] {
        AppSectionGroup.allCases.filter { !shownSections(in: $0).isEmpty }
    }

    /// Opens a part: on its tab where it has one, under the More tab on the
    /// phone where it has not.
    private func show(_ section: AppSection) {
        if horizontalSizeClass == .regular {
            if isTabShown(section) {
                tab = .section(section)
            } else if isStillUnknown(section) {
                pendingSection = section
            } else {
                app.openedLinkText = String(localized: "\(section.title) is off")
            }
        } else if tabBarSections.contains(section), isTabShown(section) {
            tab = .section(section)
        } else {
            moreSection = section
            tab = .more
        }
    }

    /// Whether the server has yet to say if the account has a part.
    private func isStillUnknown(_ section: AppSection) -> Bool {
        switch section {
        case .location, .health: !app.hasReadPermissions
        case .coding: false
        default: app.services == nil
        }
    }

    private func retryPendingSection() {
        guard let section = pendingSection else { return }
        pendingSection = nil
        show(section)
    }

    /// Moves the selection off a tab that went away, when the services read
    /// turned its part off.
    private func settleSelection() {
        guard case .section(let selected) = tab, !isTabShown(selected) else { return }
        if let first = shownBarSections.first {
            tab = .section(first)
        } else {
            tab = .more
        }
    }

    /// Keeps the selection to a tab the window has: the parts under More on
    /// the phone are tabs in a wide window, and the other way round.
    private func adaptToSizeClass() {
        if horizontalSizeClass == .regular {
            guard tab == MainTab.more else { return }
            let reopened = moreSection ?? AppSection.agent
            tab = .section(isTabShown(reopened) ? reopened : AppSection.agent)
        } else if case .section(let selected) = tab, !tabBarSections.contains(selected) {
            moreSection = selected
            tab = .more
        }
    }

    // MARK: Parts

    /// The part on screen, if a part is.
    private var visibleSection: AppSection? {
        switch tab {
        case .section(let section): section
        case .more: moreSection
        case .search: nil
        }
    }

    /// The Chats tab: the list of conversations, and a conversation pushed on it.
    private var chatsTab: some View {
        NavigationStack(path: $chatPath) {
            ConversationsList(connection: connection, onChoose: openChat)
                .navigationDestination(for: ChatPlace.self) { place in
                    ChatPlaceScreen(place: place, connection: connection, hasReadAgentFacts: hasReadAgentFacts)
                }
        }
    }

    private func openChat(_ chosen: AgentConversation?) {
        if let chosen {
            chatPath.append(ChatPlace.conversation(chosen))
        } else {
            chatPath.append(ChatPlace.main)
        }
    }

    @ViewBuilder
    private func sectionScreen(_ section: AppSection) -> some View {
        if app.services == nil, section != .settings, !(section == .recordings && app.isSectionAvailable(.recordings)) {
            // Which services the server runs is not known yet: neither the
            // part nor "off" is true until it is. Recordings stay open
            // offline once the account is known, so a meeting can be recorded.
            NavigationStack {
                ServicesLoadingView()
                    .toolbar { MoreBackButton() }
            }
        } else if section == .location, !app.hasReadPermissions {
            // Whether the account may use Location is read with the chats; a
            // Location link opened at launch waits for it rather than being
            // told it is refused.
            NavigationStack {
                ServicesLoadingView(isWaitingForPermissions: true)
                    .toolbar { MoreBackButton() }
            }
        } else if section == .location, !app.canUseLocation {
            NavigationStack {
                LocationNotAllowedView()
                    .toolbar { MoreBackButton() }
            }
        } else if section == .health, !app.hasReadPermissions {
            // Whether the account may use Health is read with the chats, as
            // Location's is.
            NavigationStack {
                ServicesLoadingView(isWaitingForPermissions: true)
                    .toolbar { MoreBackButton() }
            }
        } else if section == .health, !app.canUseHealth {
            NavigationStack {
                HealthNotAllowedView()
                    .toolbar { MoreBackButton() }
            }
        } else if !app.isSectionAvailable(section) {
            NavigationStack {
                ServiceOffView(section: section, connection: connection)
                    .toolbar { MoreBackButton() }
            }
        } else {
            availableScreen(section)
        }
    }

    @ViewBuilder
    private func availableScreen(_ section: AppSection) -> some View {
        switch section {
        case .agent:
            chatsTab
        case .mail:
            MailScreen(mail: mail, composing: $composing)
                .toast(done: $mail.doneText, problem: $mail.problemText, undo: $mail.undo)
                .environment(mail)
        case .calendar:
            CalendarScreen(connection: connection, username: app.account?.username ?? "")
        case .location:
            LocationScreen(connection: connection, segment: $locationSegment)
        case .health:
            HealthScreen(connection: connection)
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
            FinanceScreen(finance: finance, part: $financePart)
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
                    .toolbar { MoreBackButton() }
            }
            .environment(mail)
        }
    }

    // MARK: Search

    /// Opens what the Search tab's row names, in the part that has it.
    private func openSearchResult(_ result: SearchResult, words: String) {
        switch result.opening {
        case .conversation(let conversationId): app.openedConversationId = conversationId
        case .mailItem(let mailItemId): app.openedMailItemId = mailItemId
        case .contact(let contactId): app.openedContactId = contactId
        case .photo(let photoId): app.openedSearchedPhoto = SearchedPhoto(photoId: photoId, words: words)
        case .knowledgePage(let path): app.openedKnowledgePath = path
        case .recording(let recordingId): app.show(path: "/recordings/\(recordingId)")
        case .partOnly(let area): showAllInPart(area, words: words)
        case .nowhere: break
        }
    }

    /// Opens a part with the words in its own search, where it has one.
    private func showAllInPart(_ area: SearchArea, words: String) {
        app.openedPartSearch = PartSearch(area: area, words: words)
        switch area {
        case .chats:
            chatPath = []
            show(.agent)
        case .mail:
            show(.mail)
        case .contacts:
            show(.contacts)
        case .photos:
            show(.photos)
        case .memory:
            show(.knowledge)
        case .recordings:
            show(.recordings)
        case .places:
            locationSegment = .places
            show(.location)
        case .finance:
            financePart = .transactions
            show(.finance)
        case .documents:
            // No part of the app shows documents.
            app.openedPartSearch = nil
        }
    }

    // MARK: Presence

    /// Changes whenever what the person is looking at does, so the report of it is made again.
    private var presenceKey: String {
        let shownChatKey = chatPath.last.map { $0.conversation?.id ?? "main" } ?? "list"
        return "\(scenePhase)-\(String(describing: visibleSection))-\(shownChatKey)"
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
            if visibleSection != .coding { await app.refreshCodingPresence() }
            do { try await Task.sleep(for: .seconds(30)) } catch { return }
        } while !Task.isCancelled && scenePhase == .active
    }

    private func refreshPresence() async {
        let isChatsTabShown = visibleSection == .agent
        // A conversation is on screen when the Chats tab has one pushed; the
        // list of them is not one.
        let shownChat: ChatPlace? = isChatsTabShown ? chatPath.last : nil
        let isVisible = scenePhase == .active && shownChat != nil
        // The main conversation has no id until it is loaded and is never a
        // chat, so it is reported as none.
        let shownConversationId = isVisible ? shownChat?.conversation?.id ?? "" : ""
        repeat {
            await app.refreshServices()
            if Task.isCancelled { return }
            await app.refreshChats()
            if app.services?.isAssistantEnabled == true || app.canChat { try? await connection.reportPresence(isVisible: isVisible, conversationId: shownConversationId) }
            if isChatsTabShown && app.services?.isMailboxEnabled == true { await mail.load() }
            if scenePhase != .active { return }
            do { try await Task.sleep(for: .seconds(30)) } catch { return }
        } while !Task.isCancelled
    }

    #if DEBUG
        /// What a screenshot asks the app to open (`-TeaNodeTab`, `-TeaNodeScreen`).
        private func openDemoScreen() {
            if let name = DemoServer.tab {
                if name == "more" {
                    tab = .more
                } else if name == "search" {
                    tab = .search
                } else if let chosen = AppSection(rawValue: name) {
                    show(chosen)
                }
            }
            if DemoServer.screen?.hasPrefix("net-worth") == true { financePart = .netWorth }
            if DemoServer.screen?.hasPrefix("finance-") == true { financePart = .accounts }
            if let screen = DemoServer.screen, ["location-places", "location-place", "location-address"].contains(screen) { locationSegment = .places }
            if DemoServer.screen == "location-sharing" { locationSegment = .sharing }
            if DemoServer.isRequested, DemoServer.tab == "coding" { app.noteCodingComputers() }
            // A chat opened for a screenshot (`-TeaNodeScreen chat-direct`, `chat-group`, `chat-members`, `chat-mention`),
            // or the main conversation (`conversation`, `conversation-long-message`).
            if let chat = DemoServer.openedChat {
                chatPath = [ChatPlace.conversation(chat)]
            } else if DemoServer.opensMainChat {
                chatPath = [ChatPlace.main]
            }
        }
    #endif
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
                .toolbar { MoreBackButton() }
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
/// chats are in the list, and need no agent. A person who has no agent at
/// all is offered nothing to turn on: only their chats.
struct AgentOffView: View {
    @Environment(AppModel.self) private var app

    var body: some View {
        if !app.hasAgent {
            ContentUnavailableView(
                "Your chats are in the list", systemImage: "bubble.left",
                description: Text("Go back to the list to read and write to the people you share a circle with."))
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
