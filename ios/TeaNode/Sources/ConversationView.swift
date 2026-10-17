import PhotosUI
import QuickLook
import SwiftUI
import TeaNodeKit

/// The switches that choose how much of a conversation shows, kept on the
/// phone as the dashboard keeps them in the browser.
enum TranscriptPreference {
    static let showToolCalls = "showToolCalls"
    static let showUsage = "showUsage"
    static let showWorkingNotes = "showWorkingNotes"

    /// Notes about how the agent works rather than what it did, shown only
    /// with the working notes.
    static let workingNoteKinds: Set<String> = ["depth", "call_unreadable"]
}

/// ConversationView is one conversation, laid out like a messaging app: the
/// person's messages on the right, the agent's on the left, and the composer
/// floating over them.
struct ConversationView: View {
    @State var model: ConversationModel
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase
    @AppStorage(TranscriptPreference.showToolCalls) private var isShowingToolCalls = false
    @AppStorage(TranscriptPreference.showUsage) private var isShowingUsage = false
    @AppStorage(TranscriptPreference.showWorkingNotes) private var isShowingWorkingNotes = false
    @State private var isAtEnd = true
    /// Where the transcript is scrolled: jumping to its bottom edge works
    /// however far up it is, where scrolling to the last line needs that
    /// line built, which a long lazy list has not done.
    @State private var scrollPosition = ScrollPosition(edge: .bottom)
    /// When the person last moved the conversation themselves, and the pending second look for its end after a card folds.
    @State private var lastScrolledAt = Date.distantPast
    @State private var refindEnd: Task<Void, Never>?
    /// Whether the person has scrolled the transcript themselves. Until
    /// they have, the earlier-messages row is not read on sight: a lazy
    /// list lays itself out from the top for a moment when it comes back,
    /// and reading the page before then left the conversation at that
    /// page's edge, in the middle, rather than at its end.
    @State private var hasPersonScrolled = false
    @State private var status = StatusModel()
    @State private var isShowingStatus = false

    private static let endId = "end"

    var body: some View {
        framed
            .modifier(ConversationFollowing(model: model, status: status))
            .modifier(ConversationAlerts(model: model))
    }

    /// The conversation with its bars, title and status.
    private var framed: some View {
        transcript
            .safeAreaInset(edge: .top, spacing: 0) {
                ConversationBanners(model: model)
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                bottomBar
            }
            .overlay { loading }
            .navigationTitle(model.title)
            .navigationBarTitleDisplayMode(.inline)
            // The bar is see-through: the messages run under it, and only
            // its buttons and the name float above them.
            .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    HStack(spacing: 8) {
                        if let chat = model.chat {
                            ChatAvatarsView(chat: chat, ownUserId: model.ownUserId, size: 22)
                        }
                        Text(model.title)
                            .font(.headline)
                            .lineLimit(1)
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 6)
                    .glassEffect(in: .capsule)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    // What the agent is doing is no part of a chat of people.
                    if model.isChat {
                        ChatMenu(model: model)
                    } else {
                        StatusButton(status: status) { isShowingStatus = true }
                    }
                }
            }
            .sheet(isPresented: $isShowingStatus) {
                if let connection = app.connection {
                    StatusSheet(status: status, connection: connection)
                }
            }
            .sheet(item: Binding(get: { model.selectingText.map(SelectingText.init) }, set: { model.selectingText = $0?.text })) { selecting in
                SelectTextSheet(text: selecting.text)
            }
            .sheet(item: Binding(get: { model.readingText.map(SelectingText.init) }, set: { model.readingText = $0?.text })) { reading in
                SelectTextSheet(text: reading.text, title: "The whole message")
            }
            .sheet(item: Binding(get: { model.editingLine }, set: { model.editingLine = $0 })) { line in
                EditMessageSheet(line: line, model: model)
            }
    }

    /// The messages, scrolling, with the jump to the end.
    private var transcript: some View {
        ScrollViewReader { scroller in
            ScrollView {
                messages(scroller)
                    .padding(.horizontal)
                    .padding(.vertical, 12)
            }
            .scrollPosition($scrollPosition)
            // The jump to the end is offered only when the end is well out
            // of sight, judged from where the scroll is rather than from a
            // last line, which a new conversation may not have laid out.
            .onScrollGeometryChange(for: Bool.self) { geometry in
                geometry.contentSize.height - geometry.visibleRect.maxY < 160
            } action: { _, isNearEnd in
                isAtEnd = isNearEnd
            }
            .onScrollPhaseChange { _, phase in
                if phase == .interacting {
                    hasPersonScrolled = true
                    lastScrolledAt = .now
                }
            }
            .defaultScrollAnchor(.bottom)
            .defaultScrollAnchor(.bottom, for: .sizeChanges)
            .scrollDismissesKeyboard(.immediately)
            .overlay(alignment: .bottomTrailing) {
                if !isAtEnd {
                    Button {
                        withAnimation { scrollPosition.scrollTo(edge: .bottom) }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.headline)
                            .frame(minWidth: 44, minHeight: 44)
                    }
                    .glassEffect(.regular.interactive(), in: .circle)
                    .accessibilityLabel("Jump to the end")
                    .padding()
                }
            }
            .onChange(of: model.sentCount) {
                withAnimation { scrollPosition.scrollTo(edge: .bottom) }
            }
            // A card that folds when answered shrinks the list under the
            // scroll: the end is found again once the smaller card has been
            // laid out, or the view is left past the content, blank. Not when
            // the person has scrolled since; a newer fold replaces it.
            .onChange(of: model.cardSettledCount) {
                let foldedAt = Date.now
                scrollPosition.scrollTo(edge: .bottom)
                refindEnd?.cancel()
                refindEnd = Task { @MainActor in
                    try? await Task.sleep(for: .milliseconds(150))
                    guard !Task.isCancelled, lastScrolledAt < foldedAt else { return }
                    scrollPosition.scrollTo(edge: .bottom)
                }
            }
            // Read, or read again on coming back: opened at its end, where
            // the newest message is, whatever the lazy list measured first.
            .onChange(of: model.isLoaded) { _, isLoaded in
                if isLoaded { scrollPosition.scrollTo(edge: .bottom) }
            }
            .onAppear {
                if model.isLoaded { scrollPosition.scrollTo(edge: .bottom) }
            }
            #if DEBUG
                .task(id: model.isLoaded) {
                    // `-TeaNodeScreen conversation-long-message`: scrolled back to the top, where the long message starts;
                    // `conversation-long-message-read`: the whole of it open.
                    guard DemoServer.isRequested, let screen = DemoServer.screen, screen.hasPrefix("conversation-long-message"), model.isLoaded
                    else { return }
                    try? await Task.sleep(for: .seconds(2))
                    scrollPosition.scrollTo(edge: .top)
                    if screen == "conversation-long-message-read" {
                        model.readingText = model.transcript.lines.first(where: { $0.kind == .person })?.text
                    }
                }
            #endif
        }
    }

    private func messages(_ scroller: ScrollViewProxy) -> some View {
        LazyVStack(alignment: .leading, spacing: 10) {
            if model.earlierCount > 0 {
                // Reaching the top reads the page before it, and the
                // line that was first stays where it was.
                Button {
                    loadEarlier(scroller)
                } label: {
                    if model.isLoadingEarlier {
                        ProgressView()
                    } else {
                        Text("\(model.earlierCount) earlier messages")
                    }
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .frame(maxWidth: .infinity)
                .onAppear {
                    if hasPersonScrolled { loadEarlier(scroller) }
                }
            }
            let items = ConversationItem.items(of: shownLines)
            let readers = model.chat.map { model.transcript.readers(of: $0.members, ownUserId: model.ownUserId) } ?? [:]
            ForEach(Array(items.enumerated()), id: \.element.id) { position, item in
                ConversationItemView(
                    item: item, previous: position > 0 ? items[position - 1] : nil, readers: readers, model: model,
                    isShowingUsage: isShowingUsage, isShowingToolCalls: isShowingToolCalls || model.conversation?.jobKind == "mcp")
                    .id(item.id)
            }
            if model.isChat {
                ChatTypingLine(names: model.typing.names(), agentNames: model.typing.agentSpeakers(ownedWords: ChatWords.agentOf))
            }
            if model.runningRunId != nil, model.transcript.lines.last?.isStreaming != true {
                TypingBubble()
            }
            if model.runningRunId == nil, !model.transcript.offeredSuggestions.isEmpty {
                SuggestionsRow(suggestions: model.transcript.offeredSuggestions) { suggestion in
                    Task { await model.sendSuggestion(suggestion) }
                }
            }
            if model.isLoaded, model.transcript.lines.isEmpty {
                if model.isChat {
                    Text("Nothing has been said here yet. Say hello.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .padding(.top, 40)
                } else {
                    EmptyConversation(model: model)
                }
            }
            Color.clear.frame(height: 1).id(Self.endId)
        }
    }

    /// Whether the agent's steps show: while a turn works through them, and
    /// after it while any is left.
    private var isShowingTodos: Bool {
        !model.todos.isEmpty && (model.runningRunId != nil || model.todos.contains(where: { !$0.isDone }))
    }

    /// The message replied to, the agent's steps tucked behind the composer,
    /// and the composer.
    private var bottomBar: some View {
        VStack(spacing: 8) {
            ForEach(model.references, id: \.self) { reference in
                let drop = { model.references.removeAll { $0 == reference } }
                if let subject = reference.subject {
                    ReferenceBar(subject: subject, from: reference.from ?? "", onCancel: drop)
                } else if let path = reference.path {
                    ReferenceStrip(systemImage: "brain", title: reference.name ?? path, detail: path, onCancel: drop)
                } else if reference.recordingId != nil {
                    ReferenceStrip(systemImage: "waveform", title: reference.label, detail: "", onCancel: drop)
                } else if reference.financeTransactionId != nil {
                    ReferenceStrip(
                        systemImage: "creditcard", title: reference.merchantName.flatMap { $0.isEmpty ? nil : $0 } ?? reference.description ?? "",
                        detail: [FinanceMoney.shown(reference.amount, currencyCode: reference.currencyCode), reference.postedOn ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                        onCancel: drop)
                } else {
                    ReplyBar(reply: reference, onCancel: drop)
                }
            }
            // The agents an @ in the box offers, above the keyboard.
            if !model.mentionSuggestions.isEmpty {
                MentionRow(agents: model.mentionSuggestions) { agent in model.choose(mention: agent) }
            }
            // The steps sit behind the composer and stick out above it,
            // sliding up from behind it when a turn makes a list and back
            // down when the turn ends with nothing left.
            VStack(spacing: -TodoPanel.tuckedUnder) {
                if isShowingTodos {
                    TodoPanel(todos: model.todos)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                        .zIndex(0)
                }
                Group {
                    if let call = model.call {
                        CallBar(call: call)
                    } else {
                        Composer(model: model)
                    }
                }
                .zIndex(1)
            }
            .animation(.spring(duration: 0.4, bounce: 0.15), value: isShowingTodos)
        }
    }

    @ViewBuilder
    private var loading: some View {
        if !model.isLoaded {
            if let problemText = model.loadProblemText {
                ContentUnavailableView("Cannot reach your agent", systemImage: "wifi.exclamationmark", description: Text(problemText))
            } else {
                ProgressView()
            }
        }
    }

    private func loadEarlier(_ scroller: ScrollViewProxy) {
        guard model.isLoaded, !model.isLoadingEarlier else { return }
        let firstId = ConversationItem.items(of: shownLines).first?.id
        Task {
            await model.loadEarlier()
            if let firstId {
                scroller.scrollTo(firstId, anchor: .top)
            }
        }
    }

    /// The lines the switches leave on screen.
    private var shownLines: [TranscriptLine] {
        let isMCPRecord = model.conversation?.jobKind == "mcp"
        return model.transcript.lines.filter { line in
            switch line.kind {
            case .tool:
                return isShowingToolCalls || isMCPRecord || !AgentFile.of(toolResult: line.text).isEmpty
            case .checkIn:
                return isShowingWorkingNotes || line.detail == CheckInOrigin.goalNeedsYou.rawValue
            case .note:
                return isShowingWorkingNotes || !TranscriptPreference.workingNoteKinds.contains(line.detail)
            default:
                return true
            }
        }
    }
}

/// ConversationFollowing keeps a conversation and its head up to date while
/// the app is in front: iOS closes the feed in the background, and coming
/// back opens it again at once.
struct ConversationFollowing: ViewModifier {
    let model: ConversationModel
    let status: StatusModel
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .task(id: scenePhase == .active) {
                if scenePhase == .active, let connection = app.connection {
                    await status.follow(connection: connection) { model.conversationId }
                }
            }
            .task(id: scenePhase == .active) {
                if scenePhase == .active {
                    await model.follow()
                }
            }
            .environment(\.openURL, OpenURLAction { address in app.route(address) })
            .environment(\.pictureConversationId, model.conversationId)
            .onChange(of: app.askingAbout, initial: true) { _, reference in
                // A mail asked about elsewhere becomes what the next message
                // refers to.
                guard let reference else { return }
                model.refer(to: reference)
                app.askingAbout = nil
            }
            .onChange(of: app.isCallRequested, initial: true) { startRequestedCall() }
            .onChange(of: app.openingRequest, initial: true) { _, opening in
                guard let opening, opening.conversationId == model.conversationId else { return }
                app.openingRequest = nil
                Task { await model.sendSuggestion(opening.text) }
            }
            .onChange(of: scenePhase) { _, phase in
                // A turn left running shows on the lock screen until it ends;
                // a call shows there already, through CallKit.
                if phase == .background, let runId = model.runningRunId, model.call == nil {
                    TurnActivities.start(runId: runId, agentTitle: app.agentTitle, connection: model.connection)
                } else if phase == .active {
                    TurnActivities.endAll()
                    if model.call == nil { Task { await CallActivities.endAll() } }
                }
            }
            .onChange(of: model.voiceSampleRate) { startRequestedCall() }
    }

    /// Starts the call Siri asked for, once the server has said calls work.
    private func startRequestedCall() {
        guard app.isCallRequested, model.voiceSampleRate != nil else { return }
        app.isCallRequested = false
        model.startCall()
    }
}

/// ConversationAlerts are what a conversation says when something goes
/// wrong, the haptics of sending, and files opened in Quick Look.
struct ConversationAlerts: ViewModifier {
    @Bindable var model: ConversationModel
    @Environment(AppModel.self) private var app

    func body(content: Content) -> some View {
        @Bindable var app = app
        return content
            .quickLookPreview($app.previewAddress)
            .alert("Cannot open the file", isPresented: $app.isShowingPreviewProblem, presenting: app.previewProblemText) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
            .sensoryFeedback(.success, trigger: model.sentCount)
            .alert("Something went wrong", isPresented: $model.isShowingSendProblem, presenting: model.sendProblemText) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
            .alert(
                "Delete this message?", isPresented: Binding(get: { model.deletingLine != nil }, set: { if !$0 { model.deletingLine = nil } }),
                presenting: model.deletingLine
            ) { line in
                Button("Delete", role: .destructive) { Task { await model.delete(line) } }
                Button("Cancel", role: .cancel) {}
            } message: { _ in
                Text("Everybody in the chat will see “Message deleted” in its place.")
            }
    }
}

/// ConversationBanners say what kind of conversation this is when it is not
/// an ordinary one: somebody else's agent, a record of what the agent did on
/// its own, a goal waiting for the person.
struct ConversationBanners: View {
    let model: ConversationModel
    @Environment(AppModel.self) private var app

    var body: some View {
        VStack(spacing: 6) {
            if let actingAs = model.actingAs {
                Banner(systemImage: "person.badge.key", text: String(localized: "This is \(actingAs)'s agent. What you say here reaches it as \(actingAs), and the server log says you did."))
            }
            if model.conversation?.kind == "run" {
                Banner(systemImage: "clock.arrow.circlepath", text: String(localized: "What your agent did on its own, as it happened. Ask about it here, or go back to the main chat.")) {
                    app.openedConversationId = ""
                }
            }
            if let conversation = model.conversation, conversation.kind == "goal", let goal = conversation.goal, !goal.isEmpty {
                GoalHeader(conversation: conversation, goal: goal)
            }
            if model.isChat, !model.canWrite {
                if model.chat?.isRemoved == true {
                    Banner(systemImage: "person.fill.xmark", text: String(localized: "You are not in this chat any more. You can read it, but not write to it."))
                } else {
                    Banner(systemImage: "person.fill.xmark", text: String(localized: "You no longer share a circle with this person, so you cannot write to them."))
                }
            }
        }
        .padding(.horizontal)
    }
}

/// Banner is one line of what kind of conversation this is.
struct Banner: View {
    let systemImage: String
    let text: String
    var action: (() -> Void)?

    var body: some View {
        Group {
            if let action {
                Button(action: action) { label }
                    .buttonStyle(.plain)
            } else {
                label
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        // A thick blur, not see-through glass: the messages scroll under
        // it, and through glass their words ran into the banner's.
        .background(.regularMaterial, in: .rect(cornerRadius: 14, style: .continuous))
    }

    private var label: some View {
        Label(text, systemImage: systemImage)
            .font(.footnote)
            .foregroundStyle(.secondary)
    }
}

/// GoalHeader is a goal's conversation's one line at the top: where the
/// goal stands and the start of what it is. The whole of it, and what it
/// waits on, open in a sheet: pinned whole over the messages, a long goal
/// covered half the screen.
struct GoalHeader: View {
    let conversation: AgentConversation
    let goal: String
    @State private var isShowingGoal = false
    @Environment(AppModel.self) private var app

    var body: some View {
        Button {
            isShowingGoal = true
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "target")
                    .foregroundStyle(Color(.secondaryLabel))
                Text(GoalWords.state(conversation.goalState))
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(conversation.goalState == "waiting" ? Color.orange : Color(.secondaryLabel))
                    .fixedSize()
                Text(GoalWords.firstSentence(of: goal))
                    .font(.footnote)
                    .foregroundStyle(Color(.label))
                    .lineLimit(1)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Color(.tertiaryLabel))
            }
            .padding(.horizontal, 14)
            .frame(minHeight: 44)
            .background(.regularMaterial, in: .capsule)
            .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text(GoalWords.summary(of: conversation)))
        .accessibilityHint(Text("Shows the whole goal"))
        .sheet(isPresented: $isShowingGoal) {
            NavigationStack {
                Form {
                    Section {
                        Text(goal).textSelection(.enabled)
                    } header: {
                        Text("Goal")
                    } footer: {
                        Text(GoalWords.state(conversation.goalState))
                    }
                    if conversation.goalState == "waiting", let note = conversation.goalNote, !note.isEmpty {
                        Section("Waiting for you") {
                            Text(note).textSelection(.enabled)
                        }
                    }
                    Section {
                        Button("Open Goals", systemImage: "target") {
                            isShowingGoal = false
                            app.openedSection = .goals
                        }
                    }
                }
                .navigationTitle(conversation.title ?? String(localized: "Goal"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { isShowingGoal = false }
                    }
                }
            }
            .presentationDetents([.medium, .large])
        }
    }
}

/// GoalWords say where a goal stands, in the dashboard's words.
enum GoalWords {
    static func state(_ goalState: String?) -> String {
        switch goalState {
        case "working": String(localized: "working")
        case "waiting": String(localized: "waiting for you")
        case "met": String(localized: "met")
        case "dropped": String(localized: "dropped")
        default: goalState ?? ""
        }
    }

    static func summary(of conversation: AgentConversation) -> String {
        "\(state(conversation.goalState)) · \(conversation.goal ?? "")"
    }

    /// The goal's first sentence, for the one line at the top.
    static func firstSentence(of goal: String) -> String {
        let flat = goal.split(whereSeparator: \.isNewline).first.map(String.init) ?? goal
        if let end = flat.range(of: ". ") {
            return String(flat[..<end.lowerBound])
        }
        return flat
    }
}

/// ConversationItem is one thing on the screen: a line of the transcript,
/// or the day between two lines on different days.
enum ConversationItem: Identifiable {
    case line(TranscriptLine)
    case day(Date)

    var id: String {
        switch self {
        case .line(let line): line.id
        case .day(let date): "day-\(date.timeIntervalSince1970)"
        }
    }

    /// The line, when this is one.
    var line: TranscriptLine? {
        if case .line(let line) = self { return line }
        return nil
    }

    static func items(of lines: [TranscriptLine]) -> [ConversationItem] {
        var items: [ConversationItem] = []
        var lastDay: Date?
        for line in lines {
            if let date = ServerTime.date(line.at) {
                let day = Calendar.current.startOfDay(for: date)
                if day != lastDay {
                    items.append(.day(day))
                    lastDay = day
                }
            }
            items.append(.line(line))
        }
        return items
    }
}

struct ConversationItemView: View {
    let item: ConversationItem
    /// The item before this one, which says whether a chat message begins a
    /// run of its author's: a day between two ends one.
    var previous: ConversationItem?
    /// For a chat, who has read as far as which message.
    var readers: [String: [ChatMember]] = [:]
    let model: ConversationModel
    let isShowingUsage: Bool
    let isShowingToolCalls: Bool

    var body: some View {
        switch item {
        case .day(let date):
            Text(Self.dayText(date))
                .font(.caption)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity)
                .padding(.top, 6)
        case .line(let line):
            switch line.kind {
            case .person:
                if let author = line.author {
                    ChatLineFrame(
                        line: line, author: author, isRunStart: line.beginsRun(after: previous?.line),
                        readers: line.messageId.flatMap { readers[$0] } ?? []
                    ) {
                        PersonBubble(line: line) { model.readingText = line.text }
                            .onLongPressGesture(minimumDuration: 0.35) {
                                if line.messageId != nil { MessageActionsPresenter.present(line: line, model: model) }
                            }
                    }
                } else {
                    PersonBubble(line: line) { model.readingText = line.text }
                        .onLongPressGesture(minimumDuration: 0.35) {
                            MessageActionsPresenter.present(line: line, model: model)
                        }
                }
            case .answer where line.author != nil:
                // An agent's words said in a chat: under its name, in front of
                // the people in it.
                if let author = line.author {
                    ChatLineFrame(
                        line: line, author: author, isRunStart: line.beginsRun(after: previous?.line),
                        readers: line.messageId.flatMap { readers[$0] } ?? []
                    ) {
                        if line.isDeleted {
                            DeletedMessageBubble()
                        } else {
                            AgentBubble(reactions: line.reactions) {
                                MarkdownView(text: line.text, isSelectable: false)
                            }
                            .onLongPressGesture(minimumDuration: 0.35) {
                                MessageActionsPresenter.present(line: line, model: model)
                            }
                        }
                    }
                }
            case .answer:
                VStack(alignment: .leading, spacing: 4) {
                    AgentBubble(reactions: line.reactions) {
                        MarkdownView(text: SharedCitations.linking(line.text, usernames: model.sharedOwnerUsernames), isSelectable: false)
                    }
                    .onLongPressGesture(minimumDuration: 0.35) {
                        MessageActionsPresenter.present(line: line, model: model)
                    }
                    if isShowingUsage, let usage = line.usage {
                        Text(UsageWords.turn(usage))
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                            .padding(.leading, 8)
                    }
                }
            case .tool:
                VStack(alignment: .leading, spacing: 6) {
                    if isShowingToolCalls {
                        ToolLine(line: line)
                    }
                    ToolFiles(line: line)
                }
            case .checkIn:
                CheckInLine(line: line)
            case .note:
                if line.detail == "skill_share_call_waiting" {
                    // A call of a skill the person shared that waits for
                    // their word: the card reads the call for how it stands.
                    SkillShareCallWaitingCard(connection: model.connection, noteDetail: line.text)
                } else {
                    NoteLine(line: line)
                }
            case .approval, .question:
                CardSurface {
                    CardView(line: line, model: model)
                }
            case .codingQuestion:
                CardSurface(paddingPoints: line.isDone ? 12 : 16) {
                    CodingQuestionCard(line: line, model: model)
                }
            }
        }
    }

    static func dayText(_ date: Date) -> String {
        if Calendar.current.isDateInToday(date) { return String(localized: "Today") }
        if Calendar.current.isDateInYesterday(date) { return String(localized: "Yesterday") }
        return date.formatted(.dateTime.weekday(.abbreviated).month(.abbreviated).day())
    }
}

/// MessageActionsPresenter opens a message's actions over the whole screen,
/// as Messages does on a long press: everything else dims and blurs, the
/// message stays readable, and what can be done with it sits under it. It is
/// shown from the top of the window so it covers the bars and the composer.
@MainActor
enum MessageActionsPresenter {
    static func present(line: TranscriptLine, model: ConversationModel) {
        let window = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: \.isKeyWindow)
        var top = window?.rootViewController
        while let presented = top?.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        guard let top else { return }
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        weak var shown: UIViewController?
        let controller = UIHostingController(
            rootView: MessageActionsView(line: line, model: model) { then in
                shown?.dismiss(animated: true) { then?() }
            })
        controller.modalPresentationStyle = .overFullScreen
        controller.modalTransitionStyle = .crossDissolve
        controller.view.backgroundColor = .clear
        shown = controller
        top.present(controller, animated: true)
    }
}

/// MessageActionsView is a message held: the message itself, on its own
/// side, its whole text scrolling when it is long, and its actions below as
/// full rows.
struct MessageActionsView: View {
    let line: TranscriptLine
    let model: ConversationModel
    /// Closes the view, then does what was chosen, if anything.
    let onClose: (((() -> Void)?) -> Void)
    @State private var isShown = false
    /// Whether the emoji keyboard is open to choose a reaction that is not in
    /// the bar. The bar gives way to a line saying so, and the actions below
    /// leave the keyboard room.
    @State private var isChoosingEmoji = false

    /// Whose side it is on: the person's own words are on the right. In a
    /// chat that is the person's own, and not another person's.
    private var isPerson: Bool { line.author.map(\.isOwnWords) ?? (line.kind == .person) }

    var body: some View {
        ZStack {
            Rectangle()
                .fill(.ultraThinMaterial)
                .ignoresSafeArea()
                .onTapGesture { onClose(nil) }
                .accessibilityLabel("Close")
                .accessibilityAddTraits(.isButton)
            VStack(alignment: isPerson ? .trailing : .leading, spacing: 12) {
                // Each side reacts to what the other said: the person to the
                // agent's answers, not to their own messages. In a chat, to
                // anybody's.
                if line.messageId != nil, line.kind == .answer || line.author != nil {
                    if isChoosingEmoji {
                        emojiChooser
                    } else {
                        reactionBar
                    }
                }
                ScrollView {
                    bubble
                        .frame(maxWidth: .infinity, alignment: isPerson ? .trailing : .leading)
                }
                .scrollBounceBehavior(.basedOnSize)
                .frame(maxHeight: 420)
                .fixedSize(horizontal: false, vertical: true)
                if !isChoosingEmoji {
                    actions
                }
            }
            .padding(.horizontal)
            .scaleEffect(isShown ? 1 : 0.94, anchor: isPerson ? .trailing : .leading)
            .opacity(isShown ? 1 : 0)
        }
        .onAppear {
            withAnimation(.spring(duration: 0.3, bounce: 0.2)) { isShown = true }
        }
        .accessibilityAction(.escape) { onClose(nil) }
    }

    /// The quick reactions, then a button for any other emoji. The person's
    /// current one is marked, and shown at the end when it is not one of the
    /// quick ones; choosing it again takes it away. Each emoji keeps a full
    /// 44 point target, so on a phone too narrow for all of them the bar
    /// scrolls sideways instead of squeezing them.
    private var reactionBar: some View {
        ViewThatFits(in: .horizontal) {
            reactionButtons
            ScrollView(.horizontal) {
                reactionButtons
            }
            .scrollIndicators(.hidden)
        }
        .glassEffect(.regular, in: .capsule)
    }

    private var reactionButtons: some View {
        let current = line.reaction(of: "person", userId: model.ownUserId)
        var choices = ConversationModel.quickReactions
        if let current, !choices.contains(current) { choices.append(current) }
        return HStack(spacing: 2) {
            ForEach(choices, id: \.self) { emoji in
                Button {
                    onClose { Task { await model.react(to: line, with: emoji) } }
                } label: {
                    Text(verbatim: emoji)
                        .font(.title2)
                        .frame(width: 44, height: 44)
                        .background(current == emoji ? Color.accentColor.opacity(0.3) : .clear, in: .circle)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(current == emoji ? Text("Take away \(emoji)") : Text("React \(emoji)"))
            }
            Button {
                isChoosingEmoji = true
            } label: {
                Image(systemName: "plus")
                    .font(.body.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 32, height: 32)
                    .background(Color(.tertiarySystemFill), in: .circle)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("More emoji")
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 4)
    }

    /// What the bar becomes while the emoji keyboard is open: a line saying
    /// to choose, and a way back. The field that takes the keyboard's emoji
    /// is behind it, one point across and drawn with nothing.
    private var emojiChooser: some View {
        HStack(spacing: 8) {
            Image(systemName: "face.smiling")
                .foregroundStyle(.secondary)
            Text("Choose any emoji")
                .font(.callout)
            Button {
                isChoosingEmoji = false
            } label: {
                Image(systemName: "xmark.circle.fill")
                    .symbolRenderingMode(.hierarchical)
                    .font(.title3)
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Cancel")
        }
        .padding(.leading, 14)
        .padding(.trailing, 2)
        .glassEffect(.regular, in: .capsule)
        .background {
            EmojiKeyboardField { emoji in
                onClose { Task { await model.react(to: line, with: emoji) } }
            }
            .frame(width: 1, height: 1)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }

    @ViewBuilder
    private var bubble: some View {
        if isPerson {
            Text(line.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color.accentColor.opacity(0.2), in: .rect(cornerRadius: 20, style: .continuous))
        } else if line.kind == .person {
            // Another person's words in a chat are shown as typed.
            Text(line.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20, style: .continuous))
        } else {
            MarkdownView(text: line.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20, style: .continuous))
        }
    }

    private var actions: some View {
        VStack(spacing: 0) {
            if line.messageId != nil {
                actionRow("Reply", systemImage: "arrowshape.turn.up.left") {
                    onClose { model.reply(to: line) }
                }
                Divider()
            }
            if model.canEdit(line) {
                actionRow("Edit", systemImage: "pencil") {
                    onClose { model.editingLine = line }
                }
                Divider()
            }
            actionRow("Copy", systemImage: "doc.on.doc") {
                UIPasteboard.general.string = line.text
                onClose(nil)
            }
            Divider()
            actionRow("Select", systemImage: "selection.pin.in.out") {
                let text = line.text
                onClose { model.selectingText = text }
            }
            Divider()
            ShareLink(item: line.text) {
                actionLabel("Share", systemImage: "square.and.arrow.up")
            }
            .buttonStyle(.plain)
            if model.canDelete(line) {
                Divider()
                Button {
                    onClose { model.deletingLine = line }
                } label: {
                    HStack(spacing: 14) {
                        Image(systemName: "trash")
                            .frame(width: 24)
                        Text("Delete")
                        Spacer()
                    }
                    .font(.body)
                    .foregroundStyle(.red)
                    .padding(.horizontal, 18)
                    .frame(minHeight: 50)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
        }
        .frame(width: 250)
        .glassEffect(.regular, in: .rect(cornerRadius: 22, style: .continuous))
    }

    private func actionRow(_ title: LocalizedStringKey, systemImage: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            actionLabel(title, systemImage: systemImage)
        }
        .buttonStyle(.plain)
    }

    private func actionLabel(_ title: LocalizedStringKey, systemImage: String) -> some View {
        HStack(spacing: 14) {
            Image(systemName: systemImage)
                .frame(width: 24)
            Text(title)
            Spacer()
        }
        .font(.body)
        .foregroundStyle(Color(.label))
        .padding(.horizontal, 18)
        .frame(minHeight: 50)
        .contentShape(.rect)
    }
}

/// SelectingText is a message opened to select from.
struct SelectingText: Identifiable {
    let text: String
    var id: String { text }
}

/// SelectTextSheet shows a message's words to select part of them, which a
/// bubble's long press cannot: it opens the menu.
struct SelectTextSheet: View {
    let text: String
    var title: LocalizedStringKey = "Select text"
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            SelectableText(text: text)
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
        .presentationDetents([.medium, .large])
    }
}

/// SelectableText is text to select but not change. SwiftUI's text
/// selection takes a whole Text at once, so a UITextView does it.
struct SelectableText: UIViewRepresentable {
    let text: String

    func makeUIView(context: Context) -> UITextView {
        let view = UITextView()
        view.isEditable = false
        view.isSelectable = true
        view.font = .preferredFont(forTextStyle: .body)
        view.adjustsFontForContentSizeCategory = true
        view.textColor = .label
        view.backgroundColor = .clear
        view.textContainerInset = UIEdgeInsets(top: 16, left: 16, bottom: 16, right: 16)
        return view
    }

    func updateUIView(_ view: UITextView, context: Context) {
        if view.text != text { view.text = text }
    }
}

/// EmojiTextField is a text field that opens on the emoji keyboard, which the
/// system has no call to open, by answering which input mode it takes. A
/// person with no emoji keyboard gets the ordinary one, and anything typed on
/// it that is not an emoji is ignored. It takes the keyboard as soon as it is
/// on screen.
final class EmojiTextField: UITextField {
    override var textInputMode: UITextInputMode? {
        UITextInputMode.activeInputModes.first { $0.primaryLanguage == "emoji" } ?? super.textInputMode
    }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        if window != nil { becomeFirstResponder() }
    }
}

/// EmojiKeyboardField is the emoji keyboard for choosing one emoji: an
/// invisible field takes it, and the first emoji typed is handed to
/// `onEmoji`, once. SwiftUI has no way to open that keyboard, so a UIKit
/// text field does it.
struct EmojiKeyboardField: UIViewRepresentable {
    let onEmoji: (String) -> Void

    func makeUIView(context: Context) -> EmojiTextField {
        let field = EmojiTextField()
        field.delegate = context.coordinator
        field.addTarget(context.coordinator, action: #selector(Coordinator.textChanged(_:)), for: .editingChanged)
        field.autocorrectionType = .no
        field.spellCheckingType = .no
        field.autocapitalizationType = .none
        field.textColor = .clear
        field.tintColor = .clear
        field.backgroundColor = .clear
        field.inputAssistantItem.leadingBarButtonGroups = []
        field.inputAssistantItem.trailingBarButtonGroups = []
        return field
    }

    func updateUIView(_ field: EmojiTextField, context: Context) {
        context.coordinator.onEmoji = onEmoji
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(onEmoji: onEmoji)
    }

    @MainActor
    final class Coordinator: NSObject, UITextFieldDelegate {
        var onEmoji: (String) -> Void
        private var hasChosen = false

        init(onEmoji: @escaping (String) -> Void) {
            self.onEmoji = onEmoji
        }

        @objc func textChanged(_ field: UITextField) {
            guard !hasChosen, let emoji = EmojiReaction.firstEmoji(in: field.text ?? "") else { return }
            hasChosen = true
            field.text = ""
            onEmoji(emoji)
        }
    }
}

/// UsageWords say what a turn cost: tokens in and out, and the money.
@MainActor
enum UsageWords {
    /// The currency the agent's budget is kept in, as the head last read it.
    static var currency = "USD"

    static func count(_ count: Int) -> String {
        count.formatted(.number.notation(.compactName))
    }

    static func money(_ cost: Double) -> String {
        cost.formatted(.currency(code: currency).precision(.fractionLength(cost < 0.01 ? 4 : 2)))
    }

    static func turn(_ usage: AgentUsage) -> String {
        String(localized: "\(count(usage.promptTokens)) in, \(count(usage.completionTokens)) out · \(money(usage.cost))")
    }
}

/// DeletedMessageBubble is what stands where a message of a chat was taken
/// back, whether a person or an agent said it.
struct DeletedMessageBubble: View {
    var body: some View {
        Text("Message deleted")
            .italic()
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .overlay {
                RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Color(.separator), lineWidth: 1)
            }
    }
}

/// PersonBubble is what the person said: on the right, in the accent color,
/// with what it replies to above it.
struct PersonBubble: View {
    let line: TranscriptLine
    /// Opens the whole of a message too long to show in the bubble.
    var onShowAll: () -> Void = {}

    /// The most of a message the bubble shows. One many screens tall, such
    /// as the instruction an activity conversation starts with, is not drawn
    /// whole: a text view that tall leaves the list blank or its first lines
    /// out of reach, so it opens in a sheet that scrolls any length.
    private static let shownLineCount = 30
    private static let shownCharacterCount = 3000

    private var isTooLong: Bool {
        line.text.count > Self.shownCharacterCount || line.text.split(separator: "\n", omittingEmptySubsequences: false).count > Self.shownLineCount
    }

    /// Another person's words in a chat: on the left, in the gray the agent's
    /// are in, since the accent is the person's own.
    private var isOthers: Bool {
        guard let author = line.author else { return false }
        return !author.isOwnWords
    }

    var body: some View {
        HStack {
            if !isOthers { Spacer(minLength: 56) }
            VStack(alignment: isOthers ? .leading : .trailing, spacing: 4) {
                if line.isDeleted {
                    DeletedMessageBubble()
                } else {
                    content
                }
            }
            if isOthers { Spacer(minLength: 40) }
        }
    }

    @ViewBuilder
    private var content: some View {
        VStack(alignment: isOthers ? .leading : .trailing, spacing: 4) {
            if !line.quotedText.isEmpty {
                Text(line.quotedText)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 6)
                    .overlay(alignment: .leading) {
                        Capsule().fill(.tertiary).frame(width: 3)
                    }
            }
            ForEach(line.references, id: \.self) { reference in
                Label(reference.label, systemImage: reference.recordingId != nil ? "waveform" : reference.merchantName != nil ? "creditcard" : reference.path != nil ? "brain" : "envelope")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            ForEach(line.attachments, id: \.id) { attachment in
                AttachmentView(attachment: attachment)
            }
            if !line.text.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    if isTooLong {
                        Text(String(line.text.prefix(Self.shownCharacterCount)))
                            .lineLimit(Self.shownLineCount)
                        Button("Show all", action: onShowAll)
                            .font(.subheadline.weight(.semibold))
                    } else {
                        Text(line.text)
                    }
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                // The person's side in the accent's tint, the agent's in
                // gray, so whose each bubble is shows at a glance.
                .background(
                    isOthers ? AnyShapeStyle(Color(.secondarySystemBackground)) : AnyShapeStyle(Color.accentColor.opacity(0.2)),
                    in: .rect(cornerRadius: 20, style: .continuous)
                )
                .overlay(alignment: isOthers ? .bottomTrailing : .bottomLeading) {
                    ReactionBadges(reactions: line.reactions)
                        .offset(x: isOthers ? 10 : -10, y: 18)
                }
                .padding(.bottom, line.reactions.isEmpty ? 0 : 18)
            }
            if line.editedAt != nil {
                Text("Edited")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, 6)
            }
        }
        // A message of a chat on its way is quieter until the chat has it.
        .opacity(line.author != nil && line.isBeingSent ? 0.6 : 1)
    }
}

/// AgentBubble is something from the agent, in a bubble on the left as a
/// person's reply sits in Messages: the conversation reads as talking to
/// someone, with a bubble on each side.
struct AgentBubble<Content: View>: View {
    /// The reactions on it, drawn at its lower corner.
    var reactions: [AgentMessageReaction] = []
    @ViewBuilder let content: Content

    init(reactions: [AgentMessageReaction] = [], @ViewBuilder content: () -> Content) {
        self.reactions = reactions
        self.content = content()
    }

    var body: some View {
        HStack {
            content
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20, style: .continuous))
                .overlay(alignment: .bottomTrailing) {
                    ReactionBadges(reactions: reactions)
                        .offset(x: 10, y: 18)
                }
                .padding(.bottom, reactions.isEmpty ? 0 : 18)
            Spacer(minLength: 40)
        }
    }
}

/// ReactionBadges are the emoji on a message, the person's and the
/// agent's, in a small capsule at the bubble's corner as Messages draws a
/// tapback. One just put there pops in.
struct ReactionBadges: View {
    let reactions: [AgentMessageReaction]

    var body: some View {
        if !reactions.isEmpty {
            HStack(spacing: 2) {
                ForEach(reactions) { reaction in
                    ReactionBadge(reaction: reaction)
                        .id(reaction.emoji)
                }
            }
            .padding(.horizontal, 6)
            .padding(.vertical, 3)
            .background(.regularMaterial, in: .capsule)
            .overlay(Capsule().strokeBorder(Color(.separator).opacity(0.6), lineWidth: 0.5))
            .accessibilityElement(children: .combine)
        }
    }
}

/// ReactionBadge is one emoji. A reaction made a moment ago, by the person
/// here or by the agent through the feed, springs in; one read back with
/// the conversation is simply there, so scrolling does not set every badge
/// bouncing. The haptics are the model's, felt once per reaction rather
/// than once per badge drawn.
struct ReactionBadge: View {
    let reaction: AgentMessageReaction
    @State private var scale: CGFloat = 1
    @Environment(\.accessibilityReduceMotion) private var isReducingMotion

    var body: some View {
        Text(verbatim: reaction.emoji)
            .font(.body)
            .scaleEffect(scale)
            .accessibilityLabel(label)
            .onAppear {
                guard isFresh, !isReducingMotion else { return }
                scale = 0.1
                withAnimation(.spring(response: 0.4, dampingFraction: 0.45)) { scale = 1 }
            }
    }

    /// Who reacted, in words: by name where the server says whose it is, as in
    /// a chat, where several people can.
    private var label: Text {
        if reaction.reactorKind == "agent" { return Text("The agent reacted \(reaction.emoji)") }
        if let name = reaction.reactorName, !name.isEmpty { return Text("\(name) reacted \(reaction.emoji)") }
        return Text("You reacted \(reaction.emoji)")
    }

    /// Put there in the last few seconds: one this app has just made has no
    /// time from the server yet.
    private var isFresh: Bool {
        guard let reactedAt = ServerTime.date(reaction.reactedAt) else { return true }
        return Date().timeIntervalSince(reactedAt) < 10
    }
}

/// CardSurface is something that waits for the person, or a file: one quiet
/// surface with a hairline edge, the same for every card.
struct CardSurface<Content: View>: View {
    /// The room around what the card says: less for a card that is only a
    /// record of something already answered.
    var paddingPoints: CGFloat = 16
    @ViewBuilder let content: Content

    var body: some View {
        content
            .padding(paddingPoints)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator).opacity(0.5), lineWidth: 0.5)
            }
    }
}

/// TypingBubble says the agent is working on an answer: three dots rising in
/// turn inside a bubble, as a person typing shows in Messages, so the wait
/// reads as someone answering. The answer takes its place, in a bubble of
/// the same look, once its first words arrive.
struct TypingBubble: View {
    @Environment(\.accessibilityReduceMotion) private var isReducingMotion

    var body: some View {
        HStack {
            TimelineView(.animation(paused: isReducingMotion)) { timeline in
                let seconds = timeline.date.timeIntervalSinceReferenceDate
                HStack(spacing: 5) {
                    ForEach(0..<3, id: \.self) { index in
                        // Each dot peaks a third of a beat after the one
                        // before it, and the beat is 1.2 seconds.
                        let phase = isReducingMotion ? 0 : max(0, sin((seconds / 1.2 - Double(index) / 6) * 2 * .pi))
                        Circle()
                            .fill(.secondary)
                            .frame(width: 8, height: 8)
                            .opacity(0.4 + 0.6 * phase)
                            .offset(y: -3 * phase)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20, style: .continuous))
            Spacer(minLength: 56)
        }
        .accessibilityElement()
        .accessibilityLabel("Thinking…")
    }
}

/// SuggestionsRow is the replies the last answer offers, each sent as it is
/// with a tap.
struct SuggestionsRow: View {
    let suggestions: [String]
    let onChoose: (String) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack {
                ForEach(suggestions, id: \.self) { suggestion in
                    Button(suggestion) { onChoose(suggestion) }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.capsule)
                        .font(.subheadline)
                }
            }
        }
        .scrollIndicators(.hidden)
    }
}

/// EmptyConversation is what a conversation with nothing in it offers: what
/// to ask, and a few of the agent's ideas.
struct EmptyConversation: View {
    let model: ConversationModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Ask about your mail, or ask for something to be done.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            if !model.ideas.isEmpty, model.draft.isEmpty {
                Text("Or start with one of these")
                    .font(.footnote)
                    .foregroundStyle(.tertiary)
                    .padding(.top, 6)
                ForEach(model.ideas) { idea in
                    Button {
                        Task { await model.start(idea) }
                    } label: {
                        HStack(alignment: .top, spacing: 12) {
                            Text(idea.emoji ?? "💡")
                                .font(.title3)
                                .accessibilityHidden(true)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(idea.headline)
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(.primary)
                                if let body = idea.body, !body.isEmpty {
                                    Text(body)
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                        .lineLimit(3)
                                }
                            }
                            .multilineTextAlignment(.leading)
                            Spacer(minLength: 0)
                        }
                        .padding(14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
                        .contentShape(.rect(cornerRadius: 18))
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .padding(.top, 40)
    }
}

/// TodoPanel is the agent's own list of steps for this conversation, while
/// it works through them: a card behind the composer, narrower than it, its
/// foot under the composer's top.
struct TodoPanel: View {
    let todos: [AgentTodo]
    @State private var expanded: Set<String> = []

    /// How far the card reaches under the composer.
    static let tuckedUnder: CGFloat = 20

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(todos) { todo in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Image(systemName: todo.isDone ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(todo.isDone ? Color.accentColor : Color.secondary)
                            .accessibilityLabel(todo.isDone ? Text("Done") : Text("Still to do"))
                        Text(todo.text)
                            .strikethrough(todo.isDone)
                            .foregroundStyle(todo.isDone ? .secondary : .primary)
                            .lineLimit(expanded.contains(todo.id) ? nil : 1)
                            .onTapGesture {
                                if expanded.contains(todo.id) { expanded.remove(todo.id) } else { expanded.insert(todo.id) }
                            }
                    }
                    .font(.footnote)
                }
            }
            .padding(.horizontal, 14)
            .padding(.top, 12)
            .padding(.bottom, 12 + Self.tuckedUnder)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .frame(maxHeight: 120 + Self.tuckedUnder)
        .fixedSize(horizontal: false, vertical: true)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator).opacity(0.4), lineWidth: 0.5)
        }
        .padding(.horizontal, 28)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("The agent's steps")
    }
}

/// ReplyBar is the message being replied to, above the composer.
struct ReplyBar: View {
    let reply: AgentReference
    let onCancel: () -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        ReferenceStrip(
            systemImage: "arrowshape.turn.up.left",
            title: reply.quotedRole == "user" ? String(localized: "Replying to your own message") : String(localized: "Replying to \(app.agentTitle)"),
            detail: reply.quotedText ?? "", onCancel: onCancel)
    }
}

/// ReferenceBar is a mail the next message asks about.
struct ReferenceBar: View {
    let subject: String
    let from: String
    let onCancel: () -> Void

    var body: some View {
        ReferenceStrip(
            systemImage: "envelope", title: subject.isEmpty ? String(localized: "(no subject)") : subject, detail: from, onCancel: onCancel)
    }
}

/// ReferenceStrip is what the next message refers to, above the composer:
/// a small symbol, a line and a line under it, and a quiet way to drop it.
struct ReferenceStrip: View {
    let systemImage: String
    let title: String
    let detail: String
    let onCancel: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: systemImage)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                    .font(.subheadline.weight(.medium))
                    .lineLimit(1)
                if !detail.isEmpty {
                    Text(detail)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 4)
            Button(action: onCancel) {
                Image(systemName: "xmark")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 26, height: 26)
                    .background(Color(.tertiarySystemFill), in: .circle)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Cancel")
        }
        .padding(.leading, 14)
        .padding(.trailing, 2)
        .padding(.vertical, 2)
        .glassEffect(in: .rect(cornerRadius: 20, style: .continuous))
        .padding(.horizontal)
    }
}

/// MarkdownView draws an answer's Markdown: its blocks, each with its
/// inline styles. Headings are no larger than a headline.
struct MarkdownView: View {
    let text: String
    /// Whether its words can be selected by pressing on them. In a chat
    /// bubble they cannot: a long press there opens the message's reactions
    /// and actions, and its Select action selects.
    var isSelectable = true

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(MarkdownBlock.parse(text).enumerated()), id: \.offset) { _, block in
                switch block {
                case .paragraph(let text):
                    Text(Self.inline(text))
                        .fixedSize(horizontal: false, vertical: true)
                        .selectable(isSelectable)
                case .heading(let text):
                    Text(Self.inline(text)).font(.headline)
                case .quote(let text):
                    HStack(spacing: 8) {
                        Capsule().fill(.tertiary).frame(width: 3)
                        Text(Self.inline(text)).foregroundStyle(.secondary)
                    }
                    .fixedSize(horizontal: false, vertical: true)
                case .list(let items):
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                            HStack(alignment: .firstTextBaseline, spacing: 6) {
                                Text(item.marker).monospacedDigit().foregroundStyle(.secondary)
                                Text(Self.inline(item.text))
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }
                    }
                case .code(let text):
                    CodeBlock(text: text)
                case .table(let header, let rows):
                    ScrollView(.horizontal) {
                        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 6) {
                            GridRow {
                                ForEach(Array(header.enumerated()), id: \.offset) { _, cell in
                                    Text(Self.inline(cell)).font(.subheadline.bold())
                                }
                            }
                            Divider()
                            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                                GridRow {
                                    ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                                        Text(Self.inline(cell)).font(.subheadline)
                                    }
                                }
                            }
                        }
                    }
                case .rule:
                    Divider()
                case .image(let alt, let address, let link):
                    AnswerPicture(alt: alt, address: address, link: link)
                }
            }
        }
    }

    static func inline(_ text: String) -> AttributedString {
        (try? AttributedString(markdown: text, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
            ?? AttributedString(text)
    }
}

/// CodeBlock is code or a tool's JSON: monospaced, scrolling when long, and
/// copied with a tap.
struct CodeBlock: View {
    let text: String
    var maxHeight: CGFloat = 400
    @State private var isCopied = false

    var body: some View {
        ZStack(alignment: .topTrailing) {
            ScrollView([.horizontal, .vertical]) {
                Text(text)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: maxHeight)
            .fixedSize(horizontal: false, vertical: true)
            Button {
                UIPasteboard.general.string = text
                isCopied = true
                Task {
                    try? await Task.sleep(for: .milliseconds(900))
                    isCopied = false
                }
            } label: {
                Image(systemName: isCopied ? "checkmark" : "doc.on.doc")
                    .font(.caption)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(minWidth: 32, minHeight: 32)
            }
            .sensoryFeedback(.success, trigger: isCopied) { _, isNow in isNow }
            .accessibilityLabel(isCopied ? Text("Copied") : Text("Copy"))
        }
        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10, style: .continuous))
    }

    /// A tool's arguments or result made readable: the untrusted-data fence
    /// a result is wrapped in taken off, and JSON indented.
    static func tidy(_ text: String) -> String {
        var trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("<untrusted-data>"), trimmed.hasSuffix("</untrusted-data>") {
            trimmed = String(trimmed.dropFirst("<untrusted-data>".count).dropLast("</untrusted-data>".count))
                .trimmingCharacters(in: .whitespacesAndNewlines)
        }
        if let data = trimmed.data(using: .utf8), let object = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]),
            !(object is String),
            let pretty = try? JSONSerialization.data(withJSONObject: object, options: [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes])
        {
            return String(decoding: pretty, as: UTF8.self)
        }
        return trimmed
    }
}

/// ToolLine is a tool the agent called: its name and how it ended on one
/// line, what it was given and what came back on a tap.
struct ToolLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                HStack(spacing: 6) {
                    if line.isDone {
                        Image(systemName: "checkmark.circle")
                    } else {
                        ProgressView().controlSize(.mini)
                    }
                    Text(line.resolution.isEmpty ? ToolWords.name(line.detail) : "\(ToolWords.name(line.detail)) · \(line.resolution)")
                        .lineLimit(1)
                    Image(systemName: "chevron.right")
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if isExpanded {
                if !line.arguments.isEmpty {
                    CodeBlock(text: CodeBlock.tidy(line.arguments), maxHeight: 240)
                }
                CodeBlock(text: line.text.isEmpty ? String(localized: "Nothing came back yet.") : CodeBlock.tidy(line.text), maxHeight: 240)
            }
        }
        .padding(.leading, 4)
    }
}

enum ToolWords {
    /// A tool's name as words: "mail_draft" is "Mail draft", and a skill's
    /// or a server's tool is named by its own last part.
    static func name(_ tool: String) -> String {
        let last = tool.components(separatedBy: "__").last ?? tool
        let words = last.replacingOccurrences(of: "_", with: " ").replacingOccurrences(of: "-", with: " ")
        return words.prefix(1).uppercased() + words.dropFirst()
    }
}

/// CheckInLine is a turn the agent took on its own, shown as where it came
/// from; a tap shows what started it.
struct CheckInLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation { isExpanded.toggle() }
            } label: {
                Label(Self.label(of: line.detail), systemImage: Self.symbol(of: line.detail))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            if isExpanded {
                CodeBlock(text: line.text, maxHeight: 240)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.top, 4)
    }

    static func label(of marker: String) -> String {
        switch CheckInOrigin(rawValue: marker) {
        case .goal: String(localized: "Goal check-in")
        case .goalNeedsYou: String(localized: "A goal needs you")
        case .background: String(localized: "Background command ended")
        case .backgroundWork: String(localized: "Background work finished")
        case .schedule: String(localized: "Scheduled turn")
        case .speakFirst: String(localized: "Started by the agent")
        case .alert: String(localized: "Alert from your mail")
        case .herdrQuestion: String(localized: "A coding session asks you")
        case .herdrSession: String(localized: "A coding session finished")
        case .restartResume: String(localized: "Carrying on after a restart")
        case .approved: String(localized: "Approved after the turn ended")
        case .declined: String(localized: "Declined after the turn ended")
        case nil: marker
        }
    }

    static func symbol(of marker: String) -> String {
        switch CheckInOrigin(rawValue: marker) {
        case .goal, .goalNeedsYou: "target"
        case .background, .herdrSession: "terminal"
        case .herdrQuestion, .alert: "exclamationmark.triangle"
        case .backgroundWork: "list.bullet"
        case .schedule: "calendar"
        case .speakFirst: "sparkles"
        case .restartResume: "arrow.clockwise"
        case .approved: "checkmark"
        case .declined: "xmark"
        case nil: "bell"
        }
    }
}

/// NoteLine is something that happened, in the dashboard's words; the note
/// the earlier chat was folded into opens on a tap.
struct NoteLine: View {
    let line: TranscriptLine
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if line.detail == "compacted", !line.text.isEmpty {
                Button {
                    withAnimation { isExpanded.toggle() }
                } label: {
                    Label(Self.words(of: line), systemImage: isExpanded ? "chevron.down" : "chevron.right")
                }
                .buttonStyle(.plain)
                if isExpanded {
                    AgentBubble { MarkdownView(text: line.text) }
                }
            } else {
                HStack(spacing: 6) {
                    if line.isPending { ProgressView().controlSize(.mini) }
                    Text(Self.words(of: line))
                }
            }
        }
        .font(.caption)
        .foregroundStyle(.secondary)
        .multilineTextAlignment(.leading)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    static func words(of line: TranscriptLine) -> String {
        let detail = line.text.trimmingCharacters(in: .whitespacesAndNewlines)
        switch line.detail {
        case "queued": return String(localized: "Waiting for the turn before it…")
        case "stopped": return detail.isEmpty ? String(localized: "Stopped.") : String(localized: "Stopped: \(detail)")
        case "failed": return detail.isEmpty ? String(localized: "The turn failed.") : String(localized: "The turn failed: \(detail)")
        case "call_unreadable": return String(localized: "A tool call could not be read, so it was asked for again.")
        case "repeated_failure": return String(localized: "Stopped: the same call failed three times.")
        case "round_limit": return String(localized: "Stopped after the most rounds a turn may take.")
        case "compacting": return String(localized: "Folding the earlier chat into a note…")
        case "compacted": return String(localized: "The earlier chat was folded into a note.")
        case "depth": return detail.isEmpty ? String(localized: "Looking into this carefully.") : String(localized: "Looking into this carefully: \(detail)")
        case "goal_set": return String(localized: "Goal set: \(detail)")
        case "goal_set_again": return String(localized: "Goal set again: \(detail)")
        case "goal_changed": return String(localized: "Goal changed: \(detail)")
        case "goal_cleared": return String(localized: "Goal cleared: \(detail)")
        case "goal_met": return String(localized: "Goal met: \(detail)")
        case "goal_stalled": return String(localized: "Goal stalled: \(detail) turns since you last wrote and it is not met. Write to keep going, or clear or change it.")
        // What a person is told when they addressed an agent in a chat and no
        // turn of it started; the detail is the agent's name.
        case "chat_agent_owner_only":
            return detail.isEmpty
                ? String(localized: "Only its owner can ask that agent in this chat.") : String(localized: "Only \(detail)'s owner can ask it in this chat.")
        case "chat_agent_off":
            return detail.isEmpty ? String(localized: "That agent is switched off.") : String(localized: "\(detail) is switched off.")
        case "chat_agent_absent":
            return detail.isEmpty
                ? String(localized: "That agent's owner is not in this chat.") : String(localized: "\(detail)'s owner is not in this chat.")
        case "circle_invited":
            // Two names travel as JSON, since one plain detail cannot hold
            // both; a detail that is not JSON is shown as the server wrote it.
            guard let data = detail.data(using: .utf8),
                let invitation = try? JSONDecoder().decode(CircleInvitationNote.self, from: data), !invitation.circleName.isEmpty
            else { return detail }
            return String(localized: "\(invitation.invitedByName) invited you to \(invitation.circleName). Answer it under Circles in Settings.")
        default: return detail
        }
    }
}

/// CircleInvitationNote is the detail of a circle_invited note: who invited
/// the person, and to which circle.
private struct CircleInvitationNote: Decodable {
    var invitedByName: String = ""
    var circleName: String = ""
}

/// CardView is something waiting for the person: a tool call to approve, or
/// the agent's question.
struct CardView: View {
    let line: TranscriptLine
    let model: ConversationModel
    /// The answer picked, sent only with Send, as the dashboard's card asks:
    /// one stray tap sends nothing.
    @State private var chosen: String?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The card's two buttons side by side, or one above the other once the
    /// text is too large for both words to fit on a line.
    private var buttonLayout: AnyLayout {
        dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 10)) : AnyLayout(HStackLayout(spacing: 10))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if line.kind == .approval {
                CardHeader(
                    systemImage: line.risk == "destructive" ? "exclamationmark.triangle" : "hand.raised",
                    title: line.detail.isEmpty ? String(localized: "Approve?") : ToolWords.name(line.detail))
            } else {
                CardHeader(systemImage: "questionmark.bubble", title: String(localized: "Your agent asks"))
            }
            MarkdownView(text: line.text)
            if line.isDone {
                Label(resolutionText, systemImage: "checkmark.circle")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            } else if line.kind == .approval {
                buttonLayout {
                    Button(role: line.risk == "destructive" ? .destructive : nil) {
                        Task { await model.approve(line, isApproved: true) }
                    } label: {
                        Text("Do it").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(line.risk == "destructive" ? .red : .accentColor)
                    Button {
                        Task { await model.approve(line, isApproved: false) }
                    } label: {
                        Text("Don’t").frame(maxWidth: .infinity).foregroundStyle(.primary)
                    }
                    .buttonStyle(.bordered)
                }
                .buttonBorderShape(.capsule)
                .controlSize(.large)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(line.choices.enumerated()), id: \.offset) { index, choice in
                        if index > 0 { Divider() }
                        ChoiceRow(title: choice, detail: "", isChosen: chosen == choice, isMultiple: false) {
                            chosen = chosen == choice ? nil : choice
                        }
                    }
                }
                buttonLayout {
                    if !line.choices.isEmpty {
                        Button {
                            guard let chosen else { return }
                            Task { await model.answer(line, with: chosen) }
                        } label: {
                            Text("Send").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(chosen == nil)
                    }
                    Button {
                        Task { await model.answer(line, with: ConversationModel.chatAboutIt) }
                    } label: {
                        Text("Chat about it").frame(maxWidth: line.choices.isEmpty ? nil : .infinity).foregroundStyle(.primary)
                    }
                    .buttonStyle(.bordered)
                }
                .buttonBorderShape(.capsule)
                .controlSize(.large)
                Text("Or type your answer below.")
                    .font(.footnote)
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private var resolutionText: String {
        switch line.resolution {
        case "approved": String(localized: "Approved")
        case "declined": String(localized: "Declined")
        default: line.resolution
        }
    }
}

/// CodingQuestionCard is a question one of the person's coding sessions
/// asked, with its options as buttons: one press answers, or with several
/// allowed, the chosen ones are sent together.
struct CodingQuestionCard: View {
    let line: TranscriptLine
    let model: ConversationModel
    @State private var chosen: Set<Int> = []
    /// The whole question is kept until the person opens it: a tool's
    /// approval carries its command, its warnings and its countdown, which
    /// filled the screen, and the line that says what it asks is enough to
    /// answer most of them.
    @State private var isShowingDetails = false

    /// Which agent, which session and on which computer, as the dashboard's
    /// card names them: one person may have sessions of the same name on
    /// several machines.
    private func sessionTitle(_ question: CodingQuestion) -> String {
        [question.codingAgentName, question.sessionName, question.computer].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// An answered question is a record, so it takes two lines: the session,
    /// then what it asked with what was answered. A tap opens the rest.
    @ViewBuilder
    private func answered(_ question: CodingQuestion) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Button {
                withAnimation { isShowingDetails.toggle() }
            } label: {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 6) {
                        Image(systemName: "terminal")
                        Text(verbatim: sessionTitle(question)).lineLimit(1).truncationMode(.middle)
                        Spacer(minLength: 4)
                        Image(systemName: isShowingDetails ? "chevron.up" : "chevron.down").imageScale(.small)
                    }
                    .font(.footnote.weight(.semibold))
                    HStack(spacing: 6) {
                        Image(systemName: "checkmark.circle")
                        Text(verbatim: question.headline).lineLimit(1)
                        Spacer(minLength: 4)
                        Text(line.resolution.isEmpty ? String(localized: "Answered") : line.resolution).lineLimit(1)
                    }
                    .font(.subheadline)
                }
                .foregroundStyle(.secondary)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint(isShowingDetails ? Text("Hides the details") : Text("Shows the details"))
            if isShowingDetails {
                CodingQuestionDetails(text: question.shownText)
            }
        }
    }

    var body: some View {
        if let question = line.codingQuestion, line.isDone {
            answered(question)
        } else if let question = line.codingQuestion {
            VStack(alignment: .leading, spacing: 12) {
                // The session, with the details beside it rather than on a
                // row of their own.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    CardHeader(systemImage: "terminal", title: sessionTitle(question))
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Spacer(minLength: 4)
                    Button(isShowingDetails ? "Hide details" : "Details") {
                        withAnimation { isShowingDetails.toggle() }
                    }
                    .font(.footnote)
                    .buttonStyle(.borderless)
                }
                Text(verbatim: question.headline)
                    .lineLimit(3)
                if isShowingDetails {
                    CodingQuestionDetails(text: question.shownText)
                }
                VStack(spacing: 0) {
                    ForEach(Array(question.choices.enumerated()), id: \.element.optionNumber) { index, option in
                        if index > 0 { Divider() }
                        ChoiceRow(
                            title: option.optionLabel, detail: option.optionDescription, isChosen: chosen.contains(option.optionNumber),
                            isMultiple: question.isMultipleChoice
                        ) {
                            if question.isMultipleChoice {
                                if chosen.contains(option.optionNumber) {
                                    chosen.remove(option.optionNumber)
                                } else {
                                    chosen.insert(option.optionNumber)
                                }
                            } else {
                                Task { await model.answerCodingQuestion(line, options: [option], freeText: nil) }
                            }
                        }
                    }
                }
                if question.isMultipleChoice {
                    Button {
                        let options = question.choices.filter { chosen.contains($0.optionNumber) }
                        Task { await model.answerCodingQuestion(line, options: options, freeText: nil) }
                    } label: {
                        Text("Send").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                    .disabled(chosen.isEmpty)
                }
                if question.options.contains(where: { $0.herdrOptionKind != "choice" }) {
                    Text("Or type your answer below.")
                        .font(.footnote)
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }
}

/// CodingQuestionDetails is the whole of a coding session's question as the session wrote it: a command, a script, a
/// diff. Read as Markdown, a script's comments became headings and its quotes and line breaks were taken apart, so it
/// is shown as it is, in a fixed-width face, in a box that scrolls and whose text can be selected.
struct CodingQuestionDetails: View {
    let text: String

    var body: some View {
        ScrollView {
            Text(verbatim: text)
                .font(.footnote.monospaced())
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(10)
        }
        .frame(maxHeight: 280)
        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10, style: .continuous))
    }
}

/// CardHeader is what a card is, small and quiet above what it says.
struct CardHeader: View {
    let systemImage: String
    let title: String

    var body: some View {
        Label {
            Text(verbatim: title)
        } icon: {
            Image(systemName: systemImage)
        }
        .font(.footnote.weight(.semibold))
        .foregroundStyle(.secondary)
    }
}

/// ChoiceRow is one answer a card offers: a row with a radio or a check, as
/// a list row is, rather than a button of its own.
struct ChoiceRow: View {
    let title: String
    let detail: String
    let isChosen: Bool
    let isMultiple: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: isMultiple ? (isChosen ? "checkmark.square.fill" : "square") : (isChosen ? "largecircle.fill.circle" : "circle"))
                    .font(.title3)
                    .foregroundStyle(isChosen ? AnyShapeStyle(.tint) : AnyShapeStyle(.tertiary))
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).multilineTextAlignment(.leading)
                    if !detail.isEmpty {
                        Text(detail).font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.leading)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(.vertical, 10)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isChosen ? .isSelected : [])
    }
}

/// Composer is where the person writes: a glass field floating over the
/// conversation, with files to add and a button to send, or to stop the
/// turn that is running.
struct Composer: View {
    @Bindable var model: ConversationModel
    @Environment(AppModel.self) private var app
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var isPickingPhotos = false
    @State private var isTakingPicture = false
    @State private var isPickingFiles = false
    /// Pictures picked and still being read from the library.
    @State private var loadingPictureCount = 0
    @FocusState private var isFocused: Bool

    var body: some View {
        VStack(spacing: 8) {
            if !model.pendingFiles.isEmpty || loadingPictureCount > 0 {
                ScrollView(.horizontal) {
                    HStack {
                        ForEach(model.pendingFiles) { file in
                            PendingFileChip(
                                file: file, isUploading: model.uploadingFileIds.contains(file.id),
                                isUploaded: model.uploadedFileIds.contains(file.id)
                            ) {
                                model.pendingFiles.removeAll { $0.id == file.id }
                            }
                        }
                        ForEach(0..<loadingPictureCount, id: \.self) { _ in
                            LoadingFileChip()
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
            // One field with its buttons inside it, as the Claude app's: the
            // words on top, adding files at the bottom left, calling and
            // sending at the bottom right.
            VStack(alignment: .leading, spacing: 4) {
                TextField(placeholder, text: $model.draft, axis: .vertical)
                    .lineLimit(1...8)
                    .focused($isFocused)
                    .disabled(!model.canWrite)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.top, 14)
                HStack(spacing: 4) {
                    Menu {
                        Button {
                            isPickingPhotos = true
                        } label: {
                            Label("Photo library", systemImage: "photo.on.rectangle")
                        }
                        if UIImagePickerController.isSourceTypeAvailable(.camera) {
                            Button {
                                isTakingPicture = true
                            } label: {
                                Label("Camera", systemImage: "camera")
                            }
                        }
                        Button {
                            isPickingFiles = true
                        } label: {
                            Label("Files", systemImage: "folder")
                        }
                        // A screenshot just taken, pasted as the dashboard
                        // takes one pasted into its box.
                        if UIPasteboard.general.hasImages {
                            Button {
                                if let picture = UIPasteboard.general.image,
                                    let file = PendingFile(picture: picture, index: model.pendingFiles.count)
                                {
                                    model.pendingFiles.append(file)
                                }
                            } label: {
                                Label("Paste picture", systemImage: "doc.on.clipboard")
                            }
                        }
                    } label: {
                        Image(systemName: "plus")
                            .font(.body.weight(.medium))
                            .foregroundStyle(.secondary)
                            .frame(width: 36, height: 36)
                            .background(Color(.tertiarySystemFill), in: .circle)
                            .frame(minWidth: 44, minHeight: 44)
                            .contentShape(.rect)
                    }
                    .accessibilityLabel("Add files")
                    Spacer()
                    if model.voiceSampleRate != nil, model.draft.isEmpty, model.pendingFiles.isEmpty, model.conversation?.kind != "run" {
                        Button {
                            model.startCall()
                        } label: {
                            Image(systemName: "waveform")
                                .font(.body.weight(.medium))
                                .foregroundStyle(.secondary)
                                .frame(width: 36, height: 36)
                                .frame(minWidth: 44, minHeight: 44)
                        }
                        .accessibilityLabel("Call your agent")
                    }
                    if model.runningRunId != nil, model.draft.isEmpty, model.pendingFiles.isEmpty {
                        Button {
                            Task { await model.stop() }
                        } label: {
                            Image(systemName: "stop.fill")
                                .font(.footnote)
                                .foregroundStyle(Color(.systemBackground))
                                .frame(width: 36, height: 36)
                                .background(Color.primary, in: .circle)
                                .frame(minWidth: 44, minHeight: 44)
                        }
                        .accessibilityLabel("Stop")
                    } else {
                        Button {
                            Task { await model.send() }
                        } label: {
                            Group {
                                if model.isSending {
                                    ProgressView().tint(.white)
                                } else {
                                    Image(systemName: "arrow.up")
                                        .font(.body.weight(.semibold))
                                        .foregroundStyle(.white)
                                }
                            }
                            .frame(width: 36, height: 36)
                            .background(model.canSend || model.isSending ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(.tertiaryLabel)), in: .circle)
                            .frame(minWidth: 44, minHeight: 44)
                        }
                        .disabled(!model.canSend)
                        .accessibilityLabel(model.isSending ? Text("Sending…") : Text("Send"))
                    }
                }
                .padding(.horizontal, 6)
                .padding(.bottom, 4)
            }
            // A tap anywhere on the panel outside its buttons starts typing,
            // not only a tap on the words of the placeholder. The tap is
            // caught behind the buttons, so it never takes theirs; and the
            // glass is not interactive, as that is for a control and it
            // took touches meant for the add menu.
            .background {
                Color.clear
                    .contentShape(.rect(cornerRadius: 26, style: .continuous))
                    .onTapGesture { isFocused = true }
            }
            .glassEffect(.regular, in: .rect(cornerRadius: 26, style: .continuous))
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
        .photosPicker(isPresented: $isPickingPhotos, selection: $pickedItems, maxSelectionCount: 8, matching: .images)
        .cameraPicker(isPresented: $isTakingPicture) { picture in
            if let file = PendingFile(picture: picture, index: model.pendingFiles.count) {
                model.pendingFiles.append(file)
            }
        }
        .fileImporter(isPresented: $isPickingFiles, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let addresses) = result else { return }
            for address in addresses {
                if let file = PendingFile(fileAt: address) {
                    model.pendingFiles.append(file)
                }
            }
        }
        .onChange(of: pickedItems) { _, items in
            guard !items.isEmpty else { return }
            pickedItems = []
            Task { await add(items) }
        }
    }

    private var placeholder: String {
        if model.isChat {
            // Where an agent can be asked, the box says how.
            if !model.addressableAgents.isEmpty { return String(localized: "Message \(model.title), or type @ to ask an agent…") }
            return String(localized: "Message \(model.title)…")
        }
        if model.freshQuestion != nil { return String(localized: "Your answer") }
        return app.agentName.isEmpty ? String(localized: "Ask your agent…") : String(localized: "Ask \(app.agentName)…")
    }

    private func add(_ items: [PhotosPickerItem]) async {
        loadingPictureCount += items.count
        for item in items {
            defer { loadingPictureCount -= 1 }
            guard let data = try? await item.loadTransferable(type: Data.self),
                let file = PendingFile(pictureData: data, index: model.pendingFiles.count)
            else { continue }
            model.pendingFiles.append(file)
        }
    }
}

extension View {
    /// Text that can be selected, or not, as asked.
    @ViewBuilder
    func selectable(_ isSelectable: Bool) -> some View {
        if isSelectable {
            textSelection(.enabled)
        } else {
            textSelection(.disabled)
        }
    }
}
