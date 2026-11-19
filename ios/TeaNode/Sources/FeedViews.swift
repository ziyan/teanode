import SwiftUI
import TeaNodeKit
import UIKit

// The agent's feed (docs/planning/agent-feed-execplan.md): the cards it posts
// of its own accord, newest first, that the person reads like a social feed,
// answers with a tap, and talks over with the agent.

// MARK: The model

/// FeedModel is the feed: the cards read so far, how many are unread, and the
/// calls that answer, like, dismiss or read a card. It is held by the app's
/// shell, so the Feed tab's badge stays current while another tab is open.
@MainActor
@Observable
final class FeedModel {
    let connection: Connection
    /// The stack this model lists, when it is the open stack's; nil for the
    /// feed itself, which lists one entry per stack.
    let feedStackKey: String?

    private(set) var items: [AgentFeedItem] = []
    private(set) var unreadCount = 0
    private(set) var hasLoaded = false
    private(set) var isLoadingMore = false
    /// Why the first read failed, when there is nothing to show instead.
    private(set) var loadFailedText: String?
    /// The cards a call is in flight for, so a second tap does nothing.
    private(set) var busyFeedItemIds: Set<String> = []
    var doneText: String?
    var problemText: String?

    /// The stacks of the feed that are open in place, by stack key, and the
    /// cards each holds, newest first (or in the order the agent gave).
    private(set) var openStackKeys: Set<String> = []
    private(set) var stackItemsByKey: [String: [AgentFeedItem]] = [:]

    private var nextCursor: String?
    private var isLoading = false
    /// The hearts the person just pressed, shown before the server answers.
    private var likedOverrides: [String: Bool] = [:]
    /// The cards the server has been told were read, so they are not sent
    /// again while the feed still holds the old copy of them.
    private var readFeedItemIds: Set<String> = []
    /// The cards that appeared since the last report, sent together.
    private var pendingReadFeedItemIds: Set<String> = []
    private var readTask: Task<Void, Never>?

    init(connection: Connection, feedStackKey: String? = nil) {
        self.connection = connection
        self.feedStackKey = feedStackKey
    }

    /// A page of the feed, or of the stack this model lists.
    private func fetchPage(after cursor: String?) async throws -> AgentFeedPage {
        if let feedStackKey { return try await connection.agentFeedStack(feedStackKey, after: cursor) }
        return try await connection.agentFeed(after: cursor)
    }

    /// Whether any card waits for a turn of the agent to say how its answer
    /// went, which is when the feed is read again every few seconds.
    var hasPendingOutcome: Bool { items.contains { $0.isOutcomePending } }

    func isLiked(_ item: AgentFeedItem) -> Bool {
        likedOverrides[item.feedItemId] ?? item.isLiked
    }

    // MARK: Reading

    /// Reads the newest page again. What was shown stays when it cannot be
    /// read, and the cards past the first page are dropped: the person
    /// scrolls to them again.
    func load() async {
        guard !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        do {
            let page = try await fetchPage(after: nil)
            items = page.feedItems
            nextCursor = page.nextCursor
            unreadCount = page.unreadCount
            hasLoaded = true
            loadFailedText = nil
            // A stack open in place is read again with the feed.
            for openStackKey in openStackKeys { await loadStack(openStackKey) }
        } catch is CancellationError {
        } catch {
            guard (error as? URLError)?.code != .cancelled else { return }
            if hasLoaded {
                problemText = error.loadProblemText
            } else {
                loadFailedText = error.loadProblemText
            }
        }
    }

    func loadFirstIfNeeded() async {
        if !hasLoaded { await load() }
    }

    /// Reads the page after the last card shown.
    func loadMore() async {
        guard let cursor = nextCursor, !isLoadingMore, !isLoading else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await fetchPage(after: cursor)
            let known = Set(items.map(\.feedItemId))
            items += page.feedItems.filter { !known.contains($0.feedItemId) }
            nextCursor = page.nextCursor
            unreadCount = page.unreadCount
        } catch is CancellationError {
        } catch {
            if (error as? URLError)?.code != .cancelled { problemText = error.loadProblemText }
        }
    }

    // MARK: Stacks open in place

    /// The cards of a stack, once they have been read.
    func stackItems(_ feedStackKey: String) -> [AgentFeedItem]? {
        stackItemsByKey[feedStackKey]
    }

    /// Reads the cards of one stack, to show them under the feed's entry for it.
    func loadStack(_ feedStackKey: String) async {
        do {
            let page = try await connection.agentFeedStack(feedStackKey, pageSize: 50)
            stackItemsByKey[feedStackKey] = page.feedItems
        } catch is CancellationError {
        } catch {
            if (error as? URLError)?.code != .cancelled { problemText = error.loadProblemText }
        }
    }

    /// Opens a stack in place, or gathers it back into its deck. Called
    /// inside `withAnimation`, so the cards slide out or in.
    func setStackOpen(_ feedStackKey: String, isOpen: Bool) {
        if isOpen {
            openStackKeys.insert(feedStackKey)
        } else {
            openStackKeys.remove(feedStackKey)
        }
    }

    // MARK: Reading a card

    /// Notes that a card came on screen. Cards are reported read together
    /// after a moment, so scrolling past a screenful is one call.
    func noteAppeared(_ item: AgentFeedItem) {
        guard !item.isRead, !readFeedItemIds.contains(item.feedItemId) else { return }
        pendingReadFeedItemIds.insert(item.feedItemId)
        guard readTask == nil else { return }
        readTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1.5))
            await self?.reportRead()
        }
    }

    private func reportRead() async {
        let feedItemIds = pendingReadFeedItemIds.sorted()
        pendingReadFeedItemIds = []
        readTask = nil
        guard !feedItemIds.isEmpty else { return }
        do {
            let newlyReadCount = try await connection.markAgentFeedRead(feedItemIds)
            readFeedItemIds.formUnion(feedItemIds)
            unreadCount = max(0, unreadCount - newlyReadCount)
        } catch {
            // Left unread: the card is reported again when it next appears.
        }
    }

    // MARK: Answering

    /// Answers a card with one of its choices. The card comes back settled on
    /// the choice and stays on screen, with what the agent made of it, until
    /// the feed is read again.
    func choose(_ item: AgentFeedItem, _ choice: AgentFeedChoice) async {
        guard !busyFeedItemIds.contains(item.feedItemId) else { return }
        busyFeedItemIds.insert(item.feedItemId)
        defer { busyFeedItemIds.remove(item.feedItemId) }
        do {
            replace(try await connection.chooseAgentFeedItem(item.feedItemId, choiceKey: choice.choiceKey))
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    func toggleLike(_ item: AgentFeedItem) async {
        let isLikedNow = !isLiked(item)
        likedOverrides[item.feedItemId] = isLikedNow
        defer { likedOverrides[item.feedItemId] = nil }
        do {
            replace(try await connection.likeAgentFeedItem(item.feedItemId, isLiked: isLikedNow))
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    /// Records that the person wants fewer cards like this one; the card goes.
    func showLessLike(_ item: AgentFeedItem) async {
        do {
            _ = try await connection.showLessLikeAgentFeedItem(item.feedItemId)
            remove(item)
            doneText = String(localized: "You will see fewer like it.")
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    func dismiss(_ item: AgentFeedItem) async {
        do {
            _ = try await connection.dismissAgentFeedItem(item.feedItemId)
            remove(item)
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    // MARK: Keeping the list

    private func replace(_ item: AgentFeedItem) {
        for (feedStackKey, stacked) in stackItemsByKey {
            guard let stackIndex = stacked.firstIndex(where: { $0.feedItemId == item.feedItemId }) else { continue }
            stackItemsByKey[feedStackKey]?[stackIndex] = item
        }
        guard let index = items.firstIndex(where: { $0.feedItemId == item.feedItemId }) else { return }
        items[index] = item
    }

    private func remove(_ item: AgentFeedItem) {
        let wasUnread = !item.isRead && !readFeedItemIds.contains(item.feedItemId)
        items.removeAll { $0.feedItemId == item.feedItemId }
        for feedStackKey in Array(stackItemsByKey.keys) {
            stackItemsByKey[feedStackKey]?.removeAll { $0.feedItemId == item.feedItemId }
        }
        if wasUnread { unreadCount = max(0, unreadCount - 1) }
    }
}

// MARK: Following

/// FeedFollowing keeps the feed current while the app is in front: the server
/// says when a card is posted, replaced, answered or dismissed, and the feed
/// is read again. It is also read every minute, in case the stream is down.
enum FeedFollowing {
    private static let pollInterval = Duration.seconds(60)

    static func follow(connection: Connection, refresh: @escaping @MainActor () async -> Void) async {
        await refresh()
        #if DEBUG
            // The demo has no socket to listen to.
            if DemoServer.isRequested { return }
        #endif
        async let listening: Void = listen(connection: connection, refresh: refresh)
        async let polling: Void = poll(refresh: refresh)
        _ = await (listening, polling)
    }

    private static func poll(refresh: @escaping @MainActor () async -> Void) async {
        while !Task.isCancelled {
            try? await Task.sleep(for: pollInterval)
            if Task.isCancelled { return }
            await refresh()
        }
    }

    private static func listen(connection: Connection, refresh: @escaping @MainActor () async -> Void) async {
        var delay = Duration.seconds(1)
        while !Task.isCancelled {
            do {
                for try await _ in AgentFeedStream.events(connection: connection) {
                    await refresh()
                    delay = .seconds(1)
                }
            } catch is CancellationError {
                return
            } catch {
                // The stream dropped; what was missed is read again below.
            }
            try? await Task.sleep(for: delay)
            if Task.isCancelled { return }
            delay = min(delay * 2, .seconds(30))
            await refresh()
        }
    }
}

/// FeedBadgeFollower keeps the feed current whenever the agent is on and the
/// app is in front, whichever tab is open, and tells the app how many cards
/// are unread for the Feed tab's badge.
struct FeedBadgeFollower: ViewModifier {
    let model: FeedModel
    @Environment(AppModel.self) private var app
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content
            .task(id: "\(scenePhase)-feed-\(app.account?.accountKey ?? "")-\(app.services?.isAssistantEnabled == true)") {
                guard scenePhase == .active, app.services?.isAssistantEnabled == true else { return }
                await FeedFollowing.follow(connection: model.connection) { await model.load() }
            }
            .onChange(of: model.unreadCount, initial: true) { _, unreadCount in
                app.unreadFeedCount = unreadCount
            }
    }
}

// MARK: The screen

/// FeedStackTarget is a stack the feed opens: its key, and the source's name
/// for the title.
struct FeedStackTarget: Hashable {
    let feedStackKey: String
    let title: String
}

/// FeedScreen is the Feed tab: the agent's cards, newest first, a stack of
/// cards from one source as its newest card with the rest under it.
struct FeedScreen: View {
    let model: FeedModel
    /// The stack open on the feed, if one is.
    @State private var path: [FeedStackTarget] = []

    var body: some View {
        NavigationStack(path: $path) {
            FeedList(model: model)
                .navigationTitle("Feed")
                .toolbar { MoreBackButton() }
                .navigationDestination(for: FeedStackTarget.self) { target in
                    FeedStackScreen(target: target, parent: model)
                }
                #if DEBUG
                    // `-TeaNodeScreen feed-stack`: the stack of news cards, open; `feed-brief`: the morning brief.
                    .task {
                        guard DemoServer.isRequested, path.isEmpty else { return }
                        if DemoServer.screen == "feed-stack" {
                            path = [FeedStackTarget(feedStackKey: DemoServer.demoNewsStackKey, title: "Robots in warehouses")]
                        } else if DemoServer.screen == "feed-brief" {
                            path = [FeedStackTarget(feedStackKey: DemoServer.demoBriefStackKey, title: DemoServer.demoBriefTitle)]
                        }
                    }
                #endif
        }
    }
}

/// FeedStackScreen is one stack opened: its cards, newest first, with the
/// source's name as the title. It reads the stack itself and follows the feed
/// live, as the feed does.
struct FeedStackScreen: View {
    let target: FeedStackTarget
    let parent: FeedModel
    @State private var model: FeedModel

    init(target: FeedStackTarget, parent: FeedModel) {
        self.target = target
        self.parent = parent
        _model = State(initialValue: FeedModel(connection: parent.connection, feedStackKey: target.feedStackKey))
    }

    var body: some View {
        FeedList(model: model)
            .navigationTitle(target.title)
            .navigationBarTitleDisplayMode(.inline)
            .task {
                await FeedFollowing.follow(connection: model.connection) { await model.load() }
            }
            // What was read in the stack is counted in the feed behind it.
            .onDisappear {
                Task { await parent.load() }
            }
    }
}

/// FeedList is the cards of a model, with what the cards ask for: answering,
/// hearts, Discuss, the information sheet, paging and marking read as they
/// appear. The feed and an open stack are both one.
struct FeedList: View {
    @Bindable var model: FeedModel
    @Environment(AppModel.self) private var app
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The card whose information sheet is open. The sheet is presented from
    /// the screen, not from a card: the cards are rebuilt as the feed changes.
    @State private var informedItem: AgentFeedItem?

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    ForEach(model.items) { item in
                        entry(for: item)
                            .id(item.feedItemId)
                            .onAppear {
                                model.noteAppeared(item)
                                if item.feedItemId == model.items.last?.feedItemId {
                                    Task { await model.loadMore() }
                                }
                            }
                    }
                    if model.isLoadingMore {
                        ProgressView().padding()
                    }
                }
                .padding(.horizontal)
                .padding(.vertical, 8)
                .readableWidth()
                // A tap on the space between cards gathers an open stack.
                .background { gatherTapArea }
            }
            .background { gatherTapArea }
            // Cards sit on the grouped page, so in light mode they are white on grey.
            .background(Color(.systemGroupedBackground))
            #if DEBUG
                // `-TeaNodeScreen feed-stack-open`: the morning brief opened in place, scrolled to;
                // `feed-media`: the feed scrolled to its first card with a picture.
                .task {
                    guard DemoServer.isRequested, model.feedStackKey == nil else { return }
                    let target: String
                    if DemoServer.screen == "feed-stack-open" {
                        await model.loadFirstIfNeeded()
                        await model.loadStack(DemoServer.demoBriefStackKey)
                        model.setStackOpen(DemoServer.demoBriefStackKey, isOpen: true)
                        target = DemoServer.demoBriefTopId
                    } else if DemoServer.screen == "feed-media" {
                        await model.loadFirstIfNeeded()
                        target = DemoServer.demoMediaTopId
                    } else {
                        return
                    }
                    try? await Task.sleep(for: .milliseconds(400))
                    proxy.scrollTo(target, anchor: .top)
                }
            #endif
        }
        .scrollBounceBehavior(.always)
        .overlay { placeholder }
        .refreshable { await model.load() }
        .task { await model.loadFirstIfNeeded() }
        // While a card waits for the agent to say how its answer went, the
        // feed is read every few seconds; the live stream says the same
        // sooner when it is up.
        .task(id: model.hasPendingOutcome) {
            guard model.hasPendingOutcome else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(4))
                if Task.isCancelled { return }
                await model.load()
            }
        }
        .sheet(item: $informedItem) { item in
            FeedInformationSheet(item: item) { action in
                informedItem = nil
                switch action {
                case .showLess: Task { await model.showLessLike(item) }
                case .dismiss: Task { await model.dismiss(item) }
                case .openGoals: app.openedSection = .goals
                }
            }
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }

    // MARK: Entries, and stacks open in place

    /// One entry of the list: a card, or a stack as a deck that opens in
    /// place into a column of cards under its top card.
    @ViewBuilder
    private func entry(for item: AgentFeedItem) -> some View {
        if model.feedStackKey == nil, item.isStack {
            stackEntry(for: item)
        } else {
            card(item, isStackShown: false, isStackOpen: false, onToggleStack: nil)
                .simultaneousGesture(gatherTap, including: model.openStackKeys.isEmpty ? .subviews : .all)
        }
    }

    private func card(_ item: AgentFeedItem, isStackShown: Bool, isStackOpen: Bool, onToggleStack: (() -> Void)?) -> some View {
        FeedCard(
            item: item, isLiked: model.isLiked(item), isBusy: model.busyFeedItemIds.contains(item.feedItemId),
            isStackShown: isStackShown,
            isStackOpen: isStackOpen,
            onChoose: { choice in choose(item, choice) },
            onToggleLike: { Task { await model.toggleLike(item) } },
            onDiscuss: { discuss(item) },
            onShowInformation: { informedItem = item },
            onToggleStack: onToggleStack
        )
    }

    /// Whether the entry is a stack of the feed that is open in place.
    private func isOpenInPlace(_ item: AgentFeedItem) -> Bool {
        model.feedStackKey == nil && item.isStack && model.openStackKeys.contains(item.feedStackKey)
    }

    /// A stack's top card, which stays where it is whether the stack is open
    /// or not (its row turns into "Show Less" in place), and under it, when
    /// open, the other cards, which slide down from under it.
    @ViewBuilder
    private func stackEntry(for item: AgentFeedItem) -> some View {
        let isOpen = isOpenInPlace(item)
        VStack(spacing: 0) {
            card(item, isStackShown: true, isStackOpen: isOpen, onToggleStack: { toggleStack(item) })
                .zIndex(1)
                // A tap inside the open stack's cards is not a tap outside it.
                .onTapGesture {}
            if isOpen {
                openedCards(of: item)
                    .zIndex(0)
            }
        }
        .simultaneousGesture(gatherTap, including: isOpen || model.openStackKeys.isEmpty ? .subviews : .all)
    }

    /// The cards of an open stack but its top card, each sliding down from
    /// under the top card to its place a little after the one before.
    private func openedCards(of item: AgentFeedItem) -> some View {
        let others = stackCards(of: item).filter { $0.feedItemId != item.feedItemId }
        return VStack(spacing: 12) {
            ForEach(Array(others.enumerated()), id: \.element.feedItemId) { index, stacked in
                card(stacked, isStackShown: false, isStackOpen: false, onToggleStack: nil)
                    .zIndex(Double(-index))
                    .transition(deckTransition(index: index, count: others.count))
                    .onAppear { model.noteAppeared(stacked) }
            }
        }
        .padding(.top, 12)
        .coordinateSpace(.named(FeedDeckSlide.coordinateSpaceName))
        // Taps inside the open stack's cards do not gather it.
        .onTapGesture {}
    }

    /// The cards of an open stack; the top card alone until they are read.
    private func stackCards(of item: AgentFeedItem) -> [AgentFeedItem] {
        model.stackItems(item.feedStackKey) ?? [item]
    }

    /// The spring that moves cards out of the deck and back, about 0.38
    /// seconds; none when the person asks for reduced motion.
    private var deckAnimation: Animation? {
        if reduceMotion { return nil }
        return .spring(duration: FeedDeckSlide.duration, bounce: 0.12)
    }

    /// A card slides down from under the top card, each 45 ms after the one
    /// before; closing is the reverse, the last card going first. With
    /// reduced motion there is no motion.
    private func deckTransition(index: Int, count: Int) -> AnyTransition {
        if reduceMotion { return .identity }
        let slide = AnyTransition.modifier(active: FeedDeckSlide(progress: 0), identity: FeedDeckSlide(progress: 1))
        let opening = Animation.spring(duration: FeedDeckSlide.duration, bounce: 0.12).delay(Double(index) * FeedDeckSlide.stagger)
        let closing = Animation.spring(duration: FeedDeckSlide.duration, bounce: 0.12).delay(Double(count - 1 - index) * FeedDeckSlide.stagger)
        return .asymmetric(insertion: slide.animation(opening), removal: slide.animation(closing))
    }

    /// Opens or gathers a stack: the cards are read when they are not known
    /// yet, and a stack that cannot be read stays a deck.
    private func toggleStack(_ item: AgentFeedItem) {
        if isOpenInPlace(item) {
            gatherStack(item)
        } else {
            openStackInPlace(item)
        }
    }

    private func openStackInPlace(_ item: AgentFeedItem) {
        let feedStackKey = item.feedStackKey
        Task {
            if model.stackItems(feedStackKey) == nil { await model.loadStack(feedStackKey) }
            guard model.stackItems(feedStackKey) != nil else { return }
            withAnimation(deckAnimation) { model.setStackOpen(feedStackKey, isOpen: true) }
        }
    }

    private func gatherStack(_ item: AgentFeedItem) {
        withAnimation(deckAnimation) { model.setStackOpen(item.feedStackKey, isOpen: false) }
    }

    /// Gathers every stack that is open back into its deck.
    private func gatherAllStacks() {
        guard !model.openStackKeys.isEmpty else { return }
        withAnimation(deckAnimation) {
            for feedStackKey in Array(model.openStackKeys) { model.setStackOpen(feedStackKey, isOpen: false) }
        }
    }

    private var gatherTap: some Gesture {
        TapGesture().onEnded { gatherAllStacks() }
    }

    /// Empty space that gathers an open stack when tapped; it takes no taps
    /// while no stack is open.
    private var gatherTapArea: some View {
        Color.clear
            .contentShape(.rect)
            .onTapGesture { gatherAllStacks() }
            .allowsHitTesting(!model.openStackKeys.isEmpty)
    }

    /// What shows instead of cards: a spinner for the first read, why it
    /// failed, or why the feed is empty.
    @ViewBuilder
    private var placeholder: some View {
        if !model.hasLoaded, let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView()
        } else if model.items.isEmpty {
            ContentUnavailableView(
                "Nothing in your feed yet", systemImage: "newspaper",
                description: Text("Goals, schedules and alerts post here. Ask your agent to keep a feed of what you care about."))
        }
    }

    private func choose(_ item: AgentFeedItem, _ choice: AgentFeedChoice) {
        switch choice.choiceRole {
        case .discuss:
            discuss(item)
        case .open:
            open(item)
        case .action, .dismiss:
            Task { await model.choose(item, choice) }
        }
    }

    /// Opens the main conversation with the card attached to the next message
    /// as a chip above the box, as a mail or a photo is when asked about.
    private func discuss(_ item: AgentFeedItem) {
        app.askingAbout = AgentReference(feedItemId: item.feedItemId, feedItemTitle: item.title)
        // An empty id is the main conversation.
        app.openedConversationId = ""
    }

    /// Opens the coding session a card is about; any other card opens the
    /// Coding part.
    private func open(_ item: AgentFeedItem) {
        if let location = item.codingSessionLocation {
            app.openedCodingTarget = CodingTarget(computer: location.computer, paneId: location.paneId)
        }
        app.openedSection = .coding
    }
}

// MARK: A card

/// FeedCard is one card of the feed: its pictures, a header line of the
/// card's badge, where it is from and when, the headline, the words, a short
/// table of facts, the choices the agent offers or what was chosen and came
/// of it, and a footer of heart, Discuss and information. The words use the
/// card's whole width. It owns no sheet; the screen presents them.
struct FeedCard: View {
    let item: AgentFeedItem
    let isLiked: Bool
    let isBusy: Bool
    /// Whether a stack's top card shows its edges and the row that opens the
    /// rest; false inside the open stack.
    var isStackShown = true
    /// Whether the stack this card tops is open: its edges are gone and its
    /// row says "Show Less", in the same place and at the same height.
    var isStackOpen = false
    let onChoose: (AgentFeedChoice) -> Void
    let onToggleLike: () -> Void
    let onDiscuss: () -> Void
    let onShowInformation: () -> Void
    /// Opens the stack this card tops in place; nil where a stack is not
    /// opened that way (a stack's own screen, the previews).
    var onToggleStack: (() -> Void)?
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        VStack(spacing: 0) {
            FeedCardMedia(mediaItems: item.cardMedia)
            content
                .padding(.horizontal, 16)
                .padding(.top, 14)
                // The footer's buttons are 44 points tall, which is room enough
                // under the last line.
                .padding(.bottom, 2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(FeedCard.surface, in: .rect(cornerRadius: Self.cornerRadius, style: .continuous))
        // The picture at the top of the card is cut to the card's corners.
        .clipShape(.rect(cornerRadius: Self.cornerRadius, style: .continuous))
        .overlay { RoundedRectangle(cornerRadius: Self.cornerRadius, style: .continuous).strokeBorder(Color(.separator).opacity(0.6), lineWidth: 0.5) }
        // A soft shadow lifts a card off the page in light mode; in dark mode
        // the lighter surface does it.
        .shadow(color: Color.primary.opacity(colorScheme == .dark ? 0 : 0.07), radius: 10, y: 3)
        .background(alignment: .bottom) { stackEdges }
        .padding(.bottom, stackEdgeCount * Self.stackEdgeStep)
        .sensoryFeedback(.success, trigger: item.chosenChoiceKey)
    }

    static let cornerRadius: CGFloat = 20
    /// A card is the grouped surface on the grouped page: white on light grey
    /// in light mode, dark grey on black in dark mode.
    static let surface = Color(.secondarySystemGroupedBackground)

    /// How far each card edge behind the top one shows below it, in points.
    private static let stackEdgeStep: CGFloat = 6

    /// One edge behind a stack of two, two behind a stack of more.
    private var stackEdgeCount: CGFloat {
        guard isStackShown, !isStackOpen, item.isStack else { return 0 }
        // One edge for each card hidden, at most two.
        return CGFloat(min(2, item.stackMoreCount))
    }

    /// The edges of the cards under the top one: the same card shape, each
    /// narrower and lower than the one before it.
    @ViewBuilder
    private var stackEdges: some View {
        if stackEdgeCount > 0 {
            ZStack(alignment: .bottom) {
                if stackEdgeCount > 1 {
                    stackEdge(inset: 28, drop: 2 * Self.stackEdgeStep)
                }
                stackEdge(inset: 14, drop: Self.stackEdgeStep)
            }
            .accessibilityHidden(true)
        }
    }

    private func stackEdge(inset: CGFloat, drop: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: Self.cornerRadius, style: .continuous)
            .fill(FeedCard.surface)
            .overlay { RoundedRectangle(cornerRadius: Self.cornerRadius, style: .continuous).strokeBorder(Color(.separator).opacity(0.6), lineWidth: 0.5) }
            .padding(.horizontal, inset)
            .offset(y: drop)
    }

    /// The row under a stack's top card that opens the rest of it: in place
    /// when the feed says how, else on the stack's own screen.
    @ViewBuilder
    private var stackRow: some View {
        if isStackShown, item.isStack {
            if let onToggleStack {
                Button {
                    onToggleStack()
                } label: {
                    stackRowLabel(symbol: isStackOpen ? "chevron.up" : "chevron.down")
                }
                .buttonStyle(.plain)
                .accessibilityHint(isStackOpen ? Text("Gathers the cards back into a stack") : Text("Shows the cards of this stack"))
            } else {
                NavigationLink(value: FeedStackTarget(feedStackKey: item.feedStackKey, title: stackTitle)) {
                    stackRowLabel(symbol: "chevron.forward")
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func stackRowLabel(symbol: String) -> some View {
        HStack {
            Text(stackRowText)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.accentColor)
            Spacer(minLength: 8)
            Image(systemName: symbol)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color(uiColor: .tertiaryLabel))
                .accessibilityHidden(true)
        }
        .frame(minHeight: 44)
        .contentShape(.rect)
    }

    /// The source's name, or a plain word when the card has none.
    private var stackTitle: String {
        item.stackDisplayTitle.isEmpty ? String(localized: "More cards") : item.stackDisplayTitle
    }

    private var stackRowText: String {
        if isStackOpen { return String(localized: "Show Less") }
        return item.stackDisplayTitle.isEmpty
            ? String(localized: "+\(item.stackMoreCount) more")
            : String(localized: "+\(item.stackMoreCount) more from \(item.stackDisplayTitle)")
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 10) {
            if isStackShown, item.isStack, !item.feedStackTitle.isEmpty {
                // A stack with a name of its own says it above its top card.
                Text(item.feedStackTitle)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            FeedCardHeader(item: item)
            Text(item.title)
                .font(.headline)
                .fixedSize(horizontal: false, vertical: true)
            layoutBody
            answerArea
            stackRow
            footer
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The card's words and facts, or its structured layout, which the body
    /// stands in for wherever the layout cannot be read.
    @ViewBuilder
    private var layoutBody: some View {
        switch item.layout {
        case .text:
            if !item.body.isEmpty {
                MarkdownView(text: item.body, isSelectable: false)
                    .font(.subheadline)
            }
            if !item.factRows.isEmpty {
                FeedFacts(factRows: item.factRows)
            }
        case .list(let listRows):
            FeedListLayout(listRows: listRows, thumbnails: item.rowThumbnails)
        case .weather(let weather):
            FeedWeatherLayout(weather: weather)
        case .agenda(let agendaEntries):
            FeedAgendaLayout(agendaEntries: agendaEntries)
        }
    }

    // MARK: Choices and what came of them

    /// The choices to press, or, once one was pressed, which it was and what
    /// the agent made of it.
    @ViewBuilder
    private var answerArea: some View {
        if let chosenChoice = item.chosenChoice {
            VStack(alignment: .leading, spacing: 6) {
                Label(chosenChoice.choiceLabel, systemImage: "checkmark.circle.fill")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.accentColor)
                outcome
            }
            .accessibilityElement(children: .combine)
        } else if item.isChosen {
            // The choice is no longer listed, but the answer is still given.
            outcome
        } else if !item.choices.isEmpty, !item.isDismissed {
            // Side by side and of equal width when every label fits on one
            // line, else one under the other.
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { choiceButtons }
                VStack(spacing: 8) { choiceButtons }
            }
            .padding(.top, 2)
        }
    }

    private var choiceButtons: some View {
        ForEach(orderedChoices) { choice in
            FeedChoiceButton(choice: choice, isDisabled: isBusy) { onChoose(choice) }
        }
    }

    /// The actions first, then the rest, as the prominent button comes first.
    private var orderedChoices: [AgentFeedChoice] {
        item.choices.filter { $0.choiceRole == .action } + item.choices.filter { $0.choiceRole != .action }
    }

    @ViewBuilder
    private var outcome: some View {
        if item.isOutcomePending {
            HStack(spacing: 8) {
                ProgressView()
                Text("Your agent is on it…")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
        } else if !item.outcomeText.isEmpty {
            Text(item.outcomeText)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    // MARK: Footer

    /// Heart and Discuss at the left and information at the right; each
    /// button is 44 points wide, so the row reaches past the words' edges by
    /// the room around the glyphs and the glyphs line up with the words.
    private var footer: some View {
        HStack(spacing: 4) {
            Button {
                onToggleLike()
            } label: {
                Image(systemName: isLiked ? "heart.fill" : "heart")
                    .symbolEffect(.bounce, value: isLiked)
                    .foregroundStyle(isLiked ? Color.accentColor : Color.secondary)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(isLiked ? Text("Unlike") : Text("Like"))
            .accessibilityHint(Text("Tells your agent to post more like this"))
            Button {
                onDiscuss()
            } label: {
                Label("Discuss", systemImage: "bubble.left")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityHint(Text("Opens your chat with your agent about this card"))
            Spacer(minLength: 8)
            Button {
                onShowInformation()
            } label: {
                Image(systemName: "info.circle")
                    .foregroundStyle(Color.secondary)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text("About this card"))
        }
        .padding(.horizontal, -12)
    }
}

/// FeedChoiceButton is one choice a card offers: the action in the accent,
/// the others bordered, each as wide as the card.
struct FeedChoiceButton: View {
    let choice: AgentFeedChoice
    let isDisabled: Bool
    let onPress: () -> Void

    var body: some View {
        if choice.choiceRole == .action {
            button.buttonStyle(.borderedProminent)
        } else {
            button.buttonStyle(.bordered)
        }
    }

    private var button: some View {
        Button {
            onPress()
        } label: {
            Text(choice.choiceLabel)
                .frame(maxWidth: .infinity)
        }
        .controlSize(.large)
        .buttonBorderShape(.capsule)
        .disabled(isDisabled)
    }
}

/// FeedCardHeader is the line above a card's headline, as a news app labels
/// a story: the card's badge, where it is from (the first picture's site, the
/// source's name, else what kind of card it is) and when it was posted. At the
/// largest text sizes the time goes under it.
struct FeedCardHeader: View {
    let item: AgentFeedItem
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) { badgeAndSource }
                dateText
            }
        } else {
            HStack(spacing: 8) {
                badgeAndSource
                Spacer(minLength: 8)
                dateText
            }
        }
    }

    @ViewBuilder
    private var badgeAndSource: some View {
        FeedBadge(pictureKind: item.pictureKind, badgeColor: item.badgeColor)
        Text(item.headerText.isEmpty ? item.kindWord : item.headerText)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.secondary)
            .lineLimit(1)
    }

    @ViewBuilder
    private var dateText: some View {
        if let shownAt = item.shownAt {
            Text(shownAt.formatted(.relative(presentation: .named)))
                .font(.footnote)
                .foregroundStyle(.tertiary)
                .lineLimit(1)
        }
    }
}

/// FeedBadge is a card's picture, small, as iOS draws an app's icon in a
/// notification: its symbol in white on a rounded square of the color of the
/// card's kind, or its emoji on a plain fill. A symbol this phone does not
/// have is the newspaper instead, since it would draw nothing.
struct FeedBadge: View {
    let pictureKind: AgentFeedPictureKind
    let badgeColor: Color
    @ScaledMetric(relativeTo: .footnote) private var badgeSide: CGFloat = 26

    var body: some View {
        glyph
            .frame(width: badgeSide, height: badgeSide)
            .background(fill, in: .rect(cornerRadius: badgeSide * 0.3, style: .continuous))
            .accessibilityHidden(true)
    }

    @ViewBuilder
    private var glyph: some View {
        switch pictureKind {
        case .symbol(let name):
            symbol(UIImage(systemName: name) == nil ? AgentFeedPictureKind.fallbackSymbolName : name)
        case .emoji(let text):
            Text(text).font(.footnote)
        case .fallback:
            symbol(AgentFeedPictureKind.fallbackSymbolName)
        }
    }

    private func symbol(_ name: String) -> some View {
        Image(systemName: name)
            .font(.caption.weight(.semibold))
            .symbolRenderingMode(.monochrome)
            // White on the kind's color, as an app icon's glyph is.
            .foregroundStyle(.white)
    }

    private var fill: AnyShapeStyle {
        if case .emoji = pictureKind { return AnyShapeStyle(Color(.tertiarySystemFill)) }
        return AnyShapeStyle(badgeColor.gradient)
    }
}

extension AgentFeedItem {
    /// The color of the card's badge, one for each kind of card, so cards of
    /// a kind are told apart at a glance. Never green, which is kept for
    /// what can be pressed.
    var badgeColor: Color {
        switch feedItemKind {
        case "alert": .orange
        case "decision": .indigo
        case "news": .pink
        case "report": .teal
        case "idea": .purple
        default: .blue
        }
    }

    /// What kind of card it is, for the header of a card from no named source.
    var kindWord: String {
        switch feedItemKind {
        case "alert": String(localized: "Alert")
        case "decision": String(localized: "Decision")
        case "news": String(localized: "News")
        case "report": String(localized: "Report")
        case "idea": String(localized: "Idea")
        default: String(localized: "Update")
        }
    }
}

/// FeedFacts is a card's short table: each label in secondary text at the
/// left and its value in bold at the right, the lines told apart by a rule.
struct FeedFacts: View {
    let factRows: [AgentFeedFact]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(factRows.enumerated()), id: \.offset) { index, fact in
                if index > 0 { Divider() }
                HStack(alignment: .firstTextBaseline, spacing: 12) {
                    Text(fact.factLabel)
                        .foregroundStyle(.secondary)
                    Spacer(minLength: 8)
                    Text(fact.factValue)
                        .bold()
                        .multilineTextAlignment(.trailing)
                }
                .font(.subheadline)
                .padding(.vertical, 7)
                .accessibilityElement(children: .combine)
            }
        }
    }
}

// MARK: The information sheet

/// FeedInformationAction is what the information sheet asks the screen to do.
enum FeedInformationAction {
    case showLess
    case dismiss
    case openGoals
}

/// FeedInformationSheet says why a card is in the feed (which goal, schedule
/// or alert made it), when it was posted and updated, and lists its sources.
/// It also holds "Show less like this" and Dismiss, which close the card.
struct FeedInformationSheet: View {
    let item: AgentFeedItem
    let onAction: (FeedInformationAction) -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL

    var body: some View {
        NavigationStack {
            List {
                Section("Why you see this") {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(sourceSentence)
                        if !item.feedSourceTitle.isEmpty {
                            Text(item.feedSourceTitle)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    if item.feedSourceKind == "goal" {
                        Button("Open Goals", systemImage: "target") { onAction(.openGoals) }
                            .actionColor()
                    }
                }
                Section("When") {
                    if let postedAt = ServerTime.date(item.postedAt) {
                        LabeledContent("Posted to the feed", value: postedAt.formatted(date: .abbreviated, time: .shortened))
                    }
                    if let updatedAt = ServerTime.date(item.updatedAt), updatedAt != ServerTime.date(item.postedAt) {
                        LabeledContent("Updated", value: updatedAt.formatted(date: .abbreviated, time: .shortened))
                    }
                }
                if !item.sourceLinks.isEmpty {
                    Section("Sources") {
                        ForEach(item.sourceLinks) { link in
                            Button {
                                if let url = URL(string: link.sourceUrl) { openURL(url) }
                            } label: {
                                Label(link.sourceTitle.isEmpty ? link.sourceUrl : link.sourceTitle, systemImage: "link")
                                    .actionColor()
                            }
                        }
                    }
                }
                Section {
                    Button("Show less like this", systemImage: "hand.thumbsdown") { onAction(.showLess) }
                        .actionColor()
                    Button("Dismiss", systemImage: "xmark.circle") { onAction(.dismiss) }
                        .actionColor()
                } footer: {
                    Text("Either one takes this card out of your feed. Showing less also tells your agent to post fewer like it.")
                }
            }
            .navigationTitle("About this card")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    /// Where the card came from, in a sentence.
    private var sourceSentence: String {
        switch item.feedSourceKind {
        case "goal": String(localized: "A goal of yours posted it.")
        case "schedule": String(localized: "A schedule of yours posted it.")
        case "alert": String(localized: "Your agent thought you should know.")
        case "coding_session": String(localized: "A coding session asked you.")
        default: String(localized: "Your agent posted it.")
        }
    }
}

// MARK: Previews

extension AgentFeedItem {
    /// Invented cards for the previews: a weather card with facts, a decision
    /// still to answer, and one answered with its outcome.
    fileprivate static let previewWeather = AgentFeedItem(
        feedItemId: "preview-weather", feedSourceKind: "schedule", feedSourceTitle: "Morning weather", title: "Sunny and mild",
        body: "Highs near **21 degrees**, with a light breeze. [Hourly forecast](https://weather.example.com/town)", pictureSymbol: "cloud.sun",
        factRows: [
            AgentFeedFact(factLabel: "High", factValue: "21 degrees"), AgentFeedFact(factLabel: "Low", factValue: "11 degrees"),
            AgentFeedFact(factLabel: "Rain", factValue: "10 percent"),
        ], isLiked: true, postedAt: "2030-03-01T07:00:00Z", updatedAt: "2030-03-01T07:00:00Z")
    fileprivate static let previewDecision = AgentFeedItem(
        feedItemId: "preview-decision", feedItemKind: "decision", feedSourceKind: "alert", title: "New sign-in on a laptop",
        body: "A laptop signed in to your account at 06:12 from a city you have not used before.", pictureSymbol: "lock.shield",
        choices: [
            AgentFeedChoice(choiceKey: "mine", choiceLabel: "That's me", choiceRoleName: "dismiss"),
            AgentFeedChoice(choiceKey: "secure", choiceLabel: "Secure my account", choiceRoleName: "action"),
        ], feedUrgency: "now", postedAt: "2030-03-01T06:13:00Z", updatedAt: "2030-03-01T06:13:00Z")
    fileprivate static let previewAnswered = AgentFeedItem(
        feedItemId: "preview-answered", feedItemKind: "decision", feedSourceKind: "alert", title: "New sign-in on a laptop",
        body: "A laptop signed in to your account at 06:12.", pictureSymbol: "lock.shield",
        choices: [
            AgentFeedChoice(choiceKey: "mine", choiceLabel: "That's me", choiceRoleName: "dismiss"),
            AgentFeedChoice(choiceKey: "secure", choiceLabel: "Secure my account", choiceRoleName: "action"),
        ], chosenChoiceKey: "secure", chosenAt: "2030-03-01T06:20:00Z", outcomeText: "Signed out the other session.",
        postedAt: "2030-03-01T06:13:00Z", updatedAt: "2030-03-01T06:20:00Z")
}

extension AgentFeedItem {
    fileprivate static let previewStack = AgentFeedItem(
        feedItemId: "preview-stack", feedItemKind: "news", feedSourceKind: "goal", feedSourceId: "goal-robots",
        feedSourceTitle: "Robots in warehouses", title: "A small robot maker opens a second factory",
        body: "The new site will double its output of shelf-moving robots.", pictureSymbol: "newspaper",
        postedAt: "2030-03-01T07:30:00Z", updatedAt: "2030-03-01T07:30:00Z", feedStackKey: "goal:goal-robots", stackItemCount: 3,
        stackUnreadCount: 2)
}

private func previewCards() -> some View {
    ScrollView {
        VStack(spacing: 12) {
            ForEach([AgentFeedItem.previewWeather, .previewStack, .previewDecision, .previewAnswered]) { item in
                FeedCard(item: item, isLiked: item.isLiked, isBusy: false, onChoose: { _ in }, onToggleLike: {}, onDiscuss: {}, onShowInformation: {})
            }
        }
        .padding()
    }
}

#Preview("Cards, light") { previewCards().environment(AppModel.shared).preferredColorScheme(.light) }
#Preview("Cards, dark") { previewCards().environment(AppModel.shared).preferredColorScheme(.dark) }

// MARK: The deck opening

/// FeedDeckSlide is how a card comes out of its deck: at progress 0 it sits
/// behind the stack's top card, at the top of the column of cards, and at 1
/// in its place. The distance is read from where the card is laid out in the
/// column, so no height needs to be known.
struct FeedDeckSlide: ViewModifier {
    let progress: Double

    static let coordinateSpaceName = "feedDeckColumn"
    static let duration: Double = 0.38
    static let stagger: Double = 0.045
    /// How far under the top card the cards start, past the column's top.
    private static let tuckedUnder: CGFloat = 40

    func body(content: Content) -> some View {
        let remaining = 1 - progress
        return content.visualEffect { effect, proxy in
            let columnTop = proxy.frame(in: .named(Self.coordinateSpaceName)).minY
            return effect
                .offset(y: -remaining * (columnTop + Self.tuckedUnder))
                .opacity(min(1, progress * 4))
        }
    }
}
