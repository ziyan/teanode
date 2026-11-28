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
    /// Whether this model lists the dismissed cards, one entry per card, in
    /// place of the feed.
    let isDismissedOnly: Bool

    private(set) var items: [AgentFeedItem] = []
    private(set) var unreadCount = 0
    private(set) var hasLoaded = false
    private(set) var isLoadingMore = false
    /// Whether the feed is being read again because a dismissal left it with
    /// no cards; the screen shows the loading mark, not "nothing here", until
    /// that read is done.
    private(set) var isReadingAfterEmptying = false
    /// Why the first read failed, when there is nothing to show instead.
    private(set) var loadFailedText: String?
    /// The cards a call is in flight for, so a second tap does nothing.
    private(set) var busyFeedItemIds: Set<String> = []
    var doneText: String?
    var problemText: String?
    /// What brings back the cards just dismissed, offered by the toast.
    var undo: (@MainActor () async -> Void)?

    /// The stacks of the feed that are open in place, by stack key, and the
    /// cards each holds, newest first (or in the order the agent gave).
    /// The answered cards the person opened again; every other answered card
    /// is folded to its title and one line.
    private(set) var shownAnsweredFeedItemIds: Set<String> = []
    private(set) var openStackKeys: Set<String> = []
    private(set) var stackItemsByKey: [String: [AgentFeedItem]] = [:]

    private var nextCursor: String?
    private var isLoading = false
    /// The cards this model took off the list, which a read already on its way
    /// may still hold. They are left out of what it brings back; a read that
    /// began after they were taken off no longer needs the reminder.
    private var hiddenFeedItemIds: Set<String> = []
    /// The read of the newest page under way, which a second caller waits
    /// for instead of starting another.
    private var loadTask: Task<Void, Never>?
    /// The hearts the person just pressed, shown before the server answers.
    private var likedOverrides: [String: Bool] = [:]
    /// The cards the server has been told were read, so they are not sent
    /// again while the feed still holds the old copy of them.
    private var readFeedItemIds: Set<String> = []
    /// The cards that appeared since the last report, sent together.
    private var pendingReadFeedItemIds: Set<String> = []
    private var readTask: Task<Void, Never>?

    init(connection: Connection, feedStackKey: String? = nil, isDismissedOnly: Bool = false) {
        self.connection = connection
        self.feedStackKey = feedStackKey
        self.isDismissedOnly = isDismissedOnly
    }

    /// A page of the feed, or of the stack this model lists.
    private func fetchPage(after cursor: String?) async throws -> AgentFeedPage {
        if let feedStackKey { return try await connection.agentFeedStack(feedStackKey, after: cursor) }
        return try await connection.agentFeed(after: cursor, isDismissedOnly: isDismissedOnly)
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
    /// scrolls to them again. A call while a read is under way waits for
    /// that read rather than returning at once: the live stream reads the
    /// feed again every few seconds while a coding session works, and a
    /// pull to refresh that returned at once left the page stuck pulled
    /// down.
    func load() async {
        if let loadTask {
            await loadTask.value
            return
        }
        let task = Task { await readNewestPage() }
        loadTask = task
        await task.value
        loadTask = nil
    }

    /// The person pulled the page down: a read under way may have started
    /// before what they expect to see, so it is waited for and the feed is
    /// read once more.
    func refresh() async {
        if let loadTask { await loadTask.value }
        await load()
    }

    private func readNewestPage() async {
        isLoading = true
        defer { isLoading = false }
        let hiddenAtStart = hiddenFeedItemIds
        do {
            let page = try await fetchPage(after: nil)
            items = page.feedItems.filter { !hiddenFeedItemIds.contains($0.feedItemId) }
            hiddenFeedItemIds.subtract(hiddenAtStart)
            nextCursor = page.nextCursor
            unreadCount = page.unreadCount
            hasLoaded = true
            loadFailedText = nil
            // The stacks shown, open or tucked as decks, are read again with
            // the feed, and those no longer in it are forgotten.
            let shownStackKeys = Set(items.filter(\.isStack).map(\.feedStackKey))
            stackItemsByKey = stackItemsByKey.filter { shownStackKeys.contains($0.key) }
            for feedStackKey in Array(stackItemsByKey.keys) { await loadStack(feedStackKey) }
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
            items += page.feedItems.filter { !known.contains($0.feedItemId) && !hiddenFeedItemIds.contains($0.feedItemId) }
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

    /// Opens an answered card that was folded.
    func showAnsweredCard(_ item: AgentFeedItem) {
        shownAnsweredFeedItemIds.insert(item.feedItemId)
    }

    /// Marks a card resolved, with the person's line on how when they gave one.
    func resolve(_ item: AgentFeedItem, resolutionNote: String) async {
        do {
            replace(try await connection.resolveAgentFeedItem(item.feedItemId, resolutionNote: resolutionNote))
            doneText = String(localized: "Card resolved.")
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    /// Takes the resolution off a card.
    func unresolve(_ item: AgentFeedItem) async {
        do {
            replace(try await connection.unresolveAgentFeedItem(item.feedItemId))
            doneText = String(localized: "Card reopened.")
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
            await readAgainIfEmptied()
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    /// Dismisses one card; the cards leave at once and the toast offers Undo.
    func dismiss(_ item: AgentFeedItem) async {
        await dismissCards(.feedItemIds([item.feedItemId]))
    }

    /// Dismisses every card of the stack the card tops.
    func dismissStack(_ item: AgentFeedItem) async {
        await dismissCards(.feedStackKey(item.feedStackKey))
    }

    /// Dismisses every card that is read and waits for no answer.
    func dismissEveryReadCard() async {
        await dismissCards(.everyReadCard)
    }

    /// Asks the server to dismiss, takes the cards it dismissed out of the
    /// list, reads the feed again for what is left of their stacks, and says
    /// how many went, with a button that brings them back.
    private func dismissCards(_ dismissal: Connection.AgentFeedDismissal) async {
        do {
            let dismissedFeedItemIds = try await connection.dismissAgentFeedItems(dismissal)
            guard !dismissedFeedItemIds.isEmpty else {
                doneText = String(localized: "No cards to dismiss.")
                return
            }
            withAnimation { remove(feedItemIds: Set(dismissedFeedItemIds)) }
            doneText = dismissedFeedItemIds.count == 1
                ? String(localized: "\(dismissedFeedItemIds.count) card dismissed.")
                : String(localized: "\(dismissedFeedItemIds.count) cards dismissed.")
            undo = { [weak self] in await self?.restore(dismissedFeedItemIds) }
            if items.isEmpty {
                await readAgainIfEmptied()
            } else {
                await load()
            }
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    /// When the list has no cards left after a card was taken off, the feed is
    /// read again with the loading mark showing, and is said to be empty only
    /// if that read is. Cards taken off stay hidden from that read. A read
    /// already under way began before the card went, so it is waited for and
    /// another is made.
    private func readAgainIfEmptied() async {
        guard items.isEmpty else { return }
        isReadingAfterEmptying = true
        defer { isReadingAfterEmptying = false }
        await refresh()
    }

    /// Brings one card of the dismissed list back. The card leaves the list at
    /// once and stays off it for a read already on its way.
    func restoreDismissed(_ item: AgentFeedItem) async {
        await restoreDismissed([item])
    }

    /// Brings cards of the dismissed list back, a whole deck at once.
    func restoreDismissed(_ restoring: [AgentFeedItem]) async {
        let feedItemIds = restoring.map(\.feedItemId)
        guard !feedItemIds.isEmpty else { return }
        do {
            let restoredFeedItemIds = try await connection.restoreAgentFeedItems(feedItemIds)
            withAnimation { remove(feedItemIds: Set(feedItemIds)) }
            if restoredFeedItemIds.count == feedItemIds.count {
                doneText = feedItemIds.count == 1
                    ? String(localized: "Card restored")
                    : String(localized: "\(feedItemIds.count) cards restored.")
            } else if restoredFeedItemIds.isEmpty {
                // The server leaves a card out when the feed holds a newer one like it, or it expired.
                doneText = String(localized: "This card could not be restored.")
            } else {
                doneText = String(localized: "\(restoredFeedItemIds.count) of \(feedItemIds.count) cards restored.")
            }
        } catch is CancellationError {
        } catch {
            problemText = error.loadProblemText
        }
    }

    /// Brings dismissed cards back and reads the feed again.
    private func restore(_ feedItemIds: [String]) async {
        do {
            _ = try await connection.restoreAgentFeedItems(feedItemIds)
            hiddenFeedItemIds.subtract(feedItemIds)
            await load()
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
        remove(feedItemIds: [item.feedItemId])
    }

    private func remove(feedItemIds: Set<String>) {
        hiddenFeedItemIds.formUnion(feedItemIds)
        let unreadCountRemoved = items.filter {
            feedItemIds.contains($0.feedItemId) && !$0.isRead && !readFeedItemIds.contains($0.feedItemId)
        }.count
        items.removeAll { feedItemIds.contains($0.feedItemId) }
        for feedStackKey in Array(stackItemsByKey.keys) {
            stackItemsByKey[feedStackKey]?.removeAll { feedItemIds.contains($0.feedItemId) }
        }
        unreadCount = max(0, unreadCount - unreadCountRemoved)
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
    /// Whether the Dismissed Cards screen is pushed on the feed.
    @State private var isShowingDismissed = false

    var body: some View {
        NavigationStack(path: $path) {
            FeedList(model: model)
                .navigationTitle("Feed")
                .toolbar {
                    MoreBackButton()
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu {
                            Button("Dismiss Read", systemImage: "xmark.circle") { Task { await model.dismissEveryReadCard() } }
                            Button("Dismissed Cards", systemImage: "tray.full") { isShowingDismissed = true }
                        } label: {
                            Label("More", systemImage: "ellipsis")
                        }
                    }
                }
                .navigationDestination(for: FeedStackTarget.self) { target in
                    FeedStackScreen(target: target, parent: model)
                }
                .navigationDestination(isPresented: $isShowingDismissed) {
                    FeedDismissedScreen(parent: model)
                }
                #if DEBUG
                    // `-TeaNodeScreen feed-stack`: the stack of news cards, open; `feed-brief`: the morning brief.
                    .task {
                        guard DemoServer.isRequested, path.isEmpty else { return }
                        if DemoServer.screen == "feed-dismissed" {
                            isShowingDismissed = true
                        } else if DemoServer.screen == "feed-stack" {
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
                    // A newer app stays above every card of the feed until it
                    // is installed or put off; an open stack does not show it.
                    if model.feedStackKey == nil, let update = app.appUpdates.updateToOffer {
                        FeedAppUpdateCard(update: update)
                            .transition(.opacity.combined(with: .scale(scale: 0.96)))
                    }
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
                // `feed-media`: the feed scrolled to its first card with a picture; `feed-resolved`, `feed-answered`,
                // `feed-series`, `feed-bars` and `feed-camera`: the card of that name.
                .task {
                    guard DemoServer.isRequested, model.feedStackKey == nil else { return }
                    let target: String
                    if DemoServer.screen == "feed-stack-open" {
                        await model.loadFirstIfNeeded()
                        await model.loadStack(DemoServer.demoBriefStackKey)
                        model.setStackOpen(DemoServer.demoBriefStackKey, isOpen: true)
                        target = DemoServer.demoBriefTopId
                    } else if DemoServer.screen == "feed-deck" {
                        await model.loadFirstIfNeeded()
                        await model.loadStack(DemoServer.demoBriefStackKey)
                        target = DemoServer.demoBriefTopId
                    } else if DemoServer.screen == "feed-media" {
                        await model.loadFirstIfNeeded()
                        target = DemoServer.demoMediaTopId
                    } else if let cardId = DemoServer.cardRuleScreenTargets[DemoServer.screen ?? ""] {
                        await model.loadFirstIfNeeded()
                        target = cardId
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
        .refreshable { await model.refresh() }
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
                case .restore: Task { await model.restoreDismissed(item) }
                case .resolve(let resolutionNote): Task { await model.resolve(item, resolutionNote: resolutionNote) }
                case .unresolve: Task { await model.unresolve(item) }
                case .openGoals: app.openedSection = .goals
                }
            }
        }
        .toast(done: $model.doneText, problem: $model.problemText, undo: $model.undo)
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
            isAnsweredCardShown: model.shownAnsweredFeedItemIds.contains(item.feedItemId),
            onShowAnsweredCard: { model.showAnsweredCard(item) },
            onChoose: { choice in choose(item, choice) },
            onToggleLike: { Task { await model.toggleLike(item) } },
            onDiscuss: { discuss(item) },
            onShowInformation: { informedItem = item },
            onToggleStack: onToggleStack
        )
        .contextMenu {
            Button("Dismiss", systemImage: "xmark.circle") { Task { await model.dismiss(item) } }
            if model.feedStackKey == nil, isStackShown, item.isStack {
                Button("Dismiss All (\(item.stackItemCount))", systemImage: "xmark.circle.fill") { Task { await model.dismissStack(item) } }
            }
        }
        // A swipe either way dismisses the card, as the long press does; on a
        // stack's closed deck it dismisses the whole stack, as Dismiss All does.
        .modifier(swipeToDismiss(item, isStackShown: isStackShown, isStackOpen: isStackOpen))
    }

    /// The swipe that takes a card away: the card alone, or the whole stack
    /// when the card is a stack's top card with its deck closed.
    private func swipeToDismiss(_ item: AgentFeedItem, isStackShown: Bool, isStackOpen: Bool) -> FeedSwipeToDismiss {
        if model.feedStackKey == nil, isStackShown, item.isStack, !isStackOpen {
            return FeedSwipeToDismiss(isEnabled: true, actionTitle: "Dismiss All") { Task { await model.dismissStack(item) } }
        }
        return FeedSwipeToDismiss(isEnabled: true) { Task { await model.dismiss(item) } }
    }

    /// Whether the entry is a stack of the feed that is open in place.
    private func isOpenInPlace(_ item: AgentFeedItem) -> Bool {
        model.feedStackKey == nil && item.isStack && model.openStackKeys.contains(item.feedStackKey)
    }

    /// A stack's top card, which stays where it is whether the stack is open
    /// or not (its row turns into "Show Less" in place), and under it the
    /// stack's other cards: tucked under it as its deck, the feet of the first
    /// two showing, or spread out under it when open. Opening and gathering
    /// move those same cards. The stack is cut off at its top card's top edge
    /// and its own bottom, so a tucked card never shows over the card above,
    /// and a card shows below the top card only as it slides out.
    @ViewBuilder
    private func stackEntry(for item: AgentFeedItem) -> some View {
        let isOpen = isOpenInPlace(item)
        VStack(spacing: 0) {
            card(item, isStackShown: true, isStackOpen: isOpen, onToggleStack: { toggleStack(item) })
                .zIndex(1)
                // A tap inside the open stack's cards is not a tap outside it.
                .onTapGesture {}
            stackColumn(of: item, isOpen: isOpen)
                .zIndex(0)
        }
        // Cut off exactly at the top card's top edge: any room above it would show
        // a tucked card taller than the top card in the gap over it.
        .mask { Rectangle().padding(.horizontal, -40).padding(.bottom, -12) }
        .simultaneousGesture(gatherTap, including: isOpen || model.openStackKeys.isEmpty ? .subviews : .all)
        // The deck is the stack's own cards, so they are read with the feed.
        .task(id: item.feedStackKey) {
            if model.stackItems(item.feedStackKey) == nil { await model.loadStack(item.feedStackKey) }
        }
    }

    /// The cards of a stack but its top card, in their places, which the
    /// column shows whole when the stack is open and only the deck's feet of
    /// when it is not; each card is tucked or not by FeedDeckTuck.
    private func stackColumn(of item: AgentFeedItem, isOpen: Bool) -> some View {
        let others = stackCards(of: item).filter { $0.feedItemId != item.feedItemId }
        let footCount = min(FeedDeckTuck.footMost, others.isEmpty ? item.stackItemCount - 1 : others.count)
        let closedHeight: CGFloat? = isOpen ? nil : FeedDeckTuck.footStep * CGFloat(max(0, footCount))
        return VStack(spacing: 12) {
            ForEach(Array(others.enumerated()), id: \.element.feedItemId) { index, stacked in
                card(stacked, isStackShown: false, isStackOpen: false, onToggleStack: nil)
                    // A card is drawn over the cards under it, as in a deck.
                    .zIndex(Double(-index))
                    .modifier(FeedDeckTuck(tuckedFraction: isOpen ? 0 : 1, footLevel: min(index, max(0, footCount - 1))))
            }
        }
        .padding(.top, 12)
        // Every card is laid out whole whatever the column's height, so a
        // tucked card is the same card that slides out, not one squeezed into
        // the deck's few points.
        .fixedSize(horizontal: false, vertical: true)
        .coordinateSpace(.named(FeedDeckTuck.coordinateSpaceName))
        // Closed, the column is as tall as the feet that show below the top
        // card; the cards keep their places in it and are drawn tucked.
        .frame(height: closedHeight, alignment: .top)
        .allowsHitTesting(isOpen)
        .accessibilityHidden(!isOpen)
        // Taps inside the open stack's cards do not gather it.
        .onTapGesture {}
    }

    /// The cards of a stack, the top one too; the top card alone until they
    /// are read.
    private func stackCards(of item: AgentFeedItem) -> [AgentFeedItem] {
        model.stackItems(item.feedStackKey) ?? [item]
    }

    /// How the cards move out of the deck and back: every card and the
    /// column on one curve, so they spread evenly and never cross, and none
    /// when the person asks for reduced motion.
    private func deckAnimation(isOpening: Bool) -> Animation? {
        if reduceMotion { return nil }
        return isOpening
            ? .timingCurve(0.45, 0, 0.2, 1, duration: FeedDeckTuck.openDuration)
            : .timingCurve(0.5, 0, 0.3, 1, duration: FeedDeckTuck.closeDuration)
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
            guard let stacked = model.stackItems(feedStackKey) else { return }
            withAnimation(deckAnimation(isOpening: true)) { model.setStackOpen(feedStackKey, isOpen: true) }
            // The cards are seen once they are spread out, not while tucked.
            for stackedItem in stacked where stackedItem.feedItemId != item.feedItemId { model.noteAppeared(stackedItem) }
        }
    }

    private func gatherStack(_ item: AgentFeedItem) {
        withAnimation(deckAnimation(isOpening: false)) { model.setStackOpen(item.feedStackKey, isOpen: false) }
    }

    /// Gathers every stack that is open back into its deck.
    private func gatherAllStacks() {
        guard !model.openStackKeys.isEmpty else { return }
        withAnimation(deckAnimation(isOpening: false)) {
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
        } else if !model.hasLoaded || model.isReadingAfterEmptying {
            ProgressView()
        } else if model.items.isEmpty, model.feedStackKey != nil || app.appUpdates.updateToOffer == nil {
            ContentUnavailableView(
                "Nothing in your feed yet", systemImage: "newspaper",
                description: Text("Goals and alerts post here. Ask your agent to keep a feed of what you care about."))
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
    /// Whether an answered card is drawn whole; false folds it to its title
    /// and one line, with a button that opens it again.
    var isAnsweredCardShown = true
    /// Opens a folded card; nil where cards are never folded.
    var onShowAnsweredCard: (() -> Void)?
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
            if !isFolded {
                FeedCardMedia(mediaItems: item.pictureMedia)
                ForEach(item.cameraStills) { still in
                    FeedCameraStill(media: still)
                }
            }
            content
                .padding(.horizontal, 16)
                .padding(.top, 14)
                // The footer's buttons are 44 points tall, which is room enough
                // under the last line.
                .padding(.bottom, 2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(item.isResolved ? FeedCard.resolvedSurface : FeedCard.surface, in: .rect(cornerRadius: Self.cornerRadius, style: .continuous))
        // The picture at the top of the card is cut to the card's corners.
        .clipShape(.rect(cornerRadius: Self.cornerRadius, style: .continuous))
        .overlay { RoundedRectangle(cornerRadius: Self.cornerRadius, style: .continuous).strokeBorder(Color(.separator).opacity(0.6), lineWidth: 0.5) }
        // A soft shadow lifts a card off the page in light mode; in dark mode
        // the lighter surface does it.
        // A resolved card has none: it is put down.
        .shadow(color: Color.primary.opacity(colorScheme == .dark || item.isResolved ? 0 : 0.07), radius: 10, y: 3)
        .sensoryFeedback(.success, trigger: item.chosenChoiceKey)
    }

    static let cornerRadius: CGFloat = 20
    /// A card is the grouped surface on the grouped page: white on light grey
    /// in light mode, dark grey on black in dark mode.
    static let surface = Color(.secondarySystemGroupedBackground)
    /// A resolved card is the same surface, drawn quieter.
    static let resolvedSurface = Color(.secondarySystemGroupedBackground).opacity(0.55)

    /// Whether the card is shown as its title and one line: it was answered,
    /// the agent has said how it went, and the person has not opened it.
    private var isFolded: Bool {
        item.isChosen && !item.isOutcomePending && !isAnsweredCardShown
    }

    /// How much of a resolved card's words show.
    private var wordsOpacity: Double { item.isResolved ? 0.7 : 1 }

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

    @ViewBuilder
    private var content: some View {
        if isFolded {
            foldedContent
        } else {
            fullContent
        }
    }

    /// The title and one muted line with the choice and what came of it.
    private var foldedContent: some View {
        VStack(alignment: .leading, spacing: 10) {
            FeedCardHeader(item: item, stackTitle: "")
            Text(item.title)
                .font(.headline)
                .fixedSize(horizontal: false, vertical: true)
            Text(foldedLine)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Button {
                onShowAnsweredCard?()
            } label: {
                Text("Show the card")
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.accentColor)
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// The choice, then what the agent made of it.
    private var foldedLine: String {
        let choiceText = item.chosenChoice?.choiceLabel ?? ""
        if choiceText.isEmpty { return item.outcomeText.isEmpty ? String(localized: "Answered") : item.outcomeText }
        return item.outcomeText.isEmpty ? choiceText : "\(choiceText): \(item.outcomeText)"
    }

    private var fullContent: some View {
        VStack(alignment: .leading, spacing: 10) {
            // A stack with a name of its own says it in its top card's header,
            // in place of where the card is from.
            FeedCardHeader(item: item, stackTitle: isStackShown && item.isStack ? item.feedStackTitle : "")
            // A resolved card's words are drawn quieter; its header and footer are not.
            VStack(alignment: .leading, spacing: 10) {
                Text(item.title)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                layoutBody
                if !item.resolutionNote.isEmpty {
                    Text(item.resolutionNote)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                ForEach(Array(item.drawableCharts.enumerated()), id: \.offset) { _, chart in
                    FeedChartView(chart: chart)
                }
            }
            .opacity(wordsOpacity)
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
        } else if !item.choices.isEmpty {
            // Side by side and of equal width when every label fits on one
            // line at that width, else one under the other.
            ViewThatFits(in: .horizontal) {
                FeedEqualRow(spacing: 8) { choiceButtons }
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

/// FeedEqualRow lays a card's choices side by side, each as wide as the row
/// allows divided evenly. Its ideal width is that of the widest choice times
/// their number, so ViewThatFits picks it only when every label fits on one
/// line at the even width, and stacks the choices otherwise.
struct FeedEqualRow: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard !subviews.isEmpty else { return .zero }
        let idealSizes = subviews.map { $0.sizeThatFits(.unspecified) }
        let widest = idealSizes.map(\.width).max() ?? 0
        let tallest = idealSizes.map(\.height).max() ?? 0
        let idealWidth = widest * CGFloat(subviews.count) + spacing * CGFloat(subviews.count - 1)
        return CGSize(width: proposal.width ?? idealWidth, height: tallest)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard !subviews.isEmpty else { return }
        let count = CGFloat(subviews.count)
        let width = max(0, (bounds.width - spacing * (count - 1)) / count)
        var left = bounds.minX
        for subview in subviews {
            subview.place(at: CGPoint(x: left, y: bounds.minY), proposal: ProposedViewSize(width: width, height: bounds.height))
            left += width + spacing
        }
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
    /// The name of the stack the card tops, said in place of where the card
    /// is from; empty for a card shown on its own.
    var stackTitle = ""
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 8) { badgeAndSource }
                resolvedChip
                dateText
            }
        } else {
            HStack(spacing: 8) {
                badgeAndSource
                Spacer(minLength: 8)
                resolvedChip
                dateText
            }
        }
    }

    @ViewBuilder
    private var badgeAndSource: some View {
        FeedBadge(pictureKind: item.pictureKind, badgeColor: item.badgeColor)
            // The badge grows with the text only so far; at the largest sizes
            // the words beside it carry the line.
            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        Text(sourceText)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.secondary)
            .lineLimit(1)
    }

    private var sourceText: String {
        if !stackTitle.isEmpty { return stackTitle }
        return item.headerText.isEmpty ? item.kindWord : item.headerText
    }

    @ViewBuilder
    private var resolvedChip: some View {
        if item.isResolved {
            FeedResolvedChip(resolvedDate: item.resolvedDate)
        }
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

/// FeedResolvedChip says a card's problem cleared: a checkmark and "Resolved
/// 06:41", the time in the person's own zone, in the secondary text color on
/// a quiet fill. Not green: green is kept for what can be pressed (DESIGN.md).
struct FeedResolvedChip: View {
    let resolvedDate: Date?

    var body: some View {
        Label {
            Text(chipText)
        } icon: {
            Image(systemName: "checkmark.circle.fill")
        }
        .font(.caption.weight(.semibold))
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .fixedSize()
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(Color(.tertiarySystemFill), in: .capsule)
    }

    private var chipText: String {
        guard let resolvedDate else { return String(localized: "Resolved") }
        let timeText = resolvedDate.formatted(date: .omitted, time: .shortened)
        return String(localized: "Resolved \(timeText)")
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
    case restore
    /// Resolves the card, with the person's line on how; empty for none.
    case resolve(String)
    case unresolve
    case openGoals
}

/// FeedInformationSheet says why a card is in the feed (which goal
/// or alert made it), when it was posted and updated, and lists its sources.
/// It also holds "Show less like this" and Dismiss, which close the card.
struct FeedInformationSheet: View {
    let item: AgentFeedItem
    /// Whether the card is one of the dismissed list, which offers Restore in
    /// place of Show less and Dismiss.
    var canRestore = false
    let onAction: (FeedInformationAction) -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    /// Whether the alert that asks for a resolution's note is up, and what
    /// the person has typed in it.
    @State private var isAskingResolutionNote = false
    @State private var resolutionNote = ""

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
                    if let resolvedDate = item.resolvedDate {
                        LabeledContent("Resolved", value: resolvedDate.formatted(date: .abbreviated, time: .shortened))
                    }
                    if let dismissedDate = item.dismissedDate {
                        LabeledContent("Dismissed", value: dismissedDate.formatted(date: .abbreviated, time: .shortened))
                    }
                }
                Section {
                    if item.isResolved {
                        Button("Unresolve", systemImage: "arrow.uturn.backward.circle") { onAction(.unresolve) }
                            .actionColor()
                    } else {
                        Button("Resolve", systemImage: "checkmark.circle") {
                            resolutionNote = ""
                            isAskingResolutionNote = true
                        }
                        .actionColor()
                    }
                } footer: {
                    Text("A resolved card stays in your feed, shown quieter, with the time its problem cleared.")
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
                if canRestore {
                    Section {
                        Button("Restore", systemImage: "arrow.uturn.backward.circle") { onAction(.restore) }
                            .actionColor()
                    } footer: {
                        Text("Puts this card back in your feed.")
                    }
                } else {
                    Section {
                        Button("Show less like this", systemImage: "hand.thumbsdown") { onAction(.showLess) }
                            .actionColor()
                        Button("Dismiss", systemImage: "xmark.circle") { onAction(.dismiss) }
                            .actionColor()
                    } footer: {
                        Text("Either one takes this card out of your feed. Showing less also tells your agent to post fewer like it.")
                    }
                }
            }
            .navigationTitle("About this card")
            .navigationBarTitleDisplayMode(.inline)
            // The note is asked for by an alert on the list, not on the row that opens it.
            .alert("Resolve this card?", isPresented: $isAskingResolutionNote) {
                TextField("Note (optional)", text: $resolutionNote)
                Button("Resolve") { onAction(.resolve(resolutionNote.trimmingCharacters(in: .whitespacesAndNewlines))) }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Say in a few words how it cleared, or leave this empty.")
            }
            .onChange(of: resolutionNote) { _, newNote in
                // A card's note is one line of at most 160 characters.
                if newNote.count > 160 { resolutionNote = String(newNote.prefix(160)) }
            }
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
        feedItemId: "preview-weather", feedSourceKind: "goal", feedSourceTitle: "Morning weather", title: "Sunny and mild",
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

/// FeedDeckTuck places one of a stack's cards in its deck or in its own place.
/// At a tucked fraction of 1 the card is under the stack's top card, scaled
/// from its foot so it is narrower than the top card by the deck's inset, with
/// only its foot showing below the top card; at 0 it is in its place. The
/// distance is read from where the card is laid out in the column, so no
/// height needs to be known. The scale and offset it makes are animated by
/// SwiftUI between the two, so the card moves on the column's curve.
struct FeedDeckTuck: ViewModifier {
    let tuckedFraction: Double
    /// Which foot of the deck the card shows: 0 just under the top card, 1
    /// under that; the cards further down hide behind the second.
    let footLevel: Int

    static let coordinateSpaceName = "feedDeckColumn"
    static let openDuration: Double = 0.44
    static let closeDuration: Double = 0.34
    /// How much of each foot shows below the one above it, in points.
    static let footStep: CGFloat = 6
    /// How many feet the deck shows.
    static let footMost = 2
    /// How much narrower each foot is than the top card at either side: more
    /// than the card's corner radius, so a tucked card's edge never shows
    /// beside the top card's rounded top corners.
    private static let footInsets: [CGFloat] = [22, 34]

    func body(content: Content) -> some View {
        let fraction = CGFloat(tuckedFraction)
        let level = min(footLevel, Self.footInsets.count - 1)
        return content.visualEffect { effect, proxy in
            let frame = proxy.frame(in: .named(Self.coordinateSpaceName))
            // The column's origin is the top card's foot.
            let footY = Self.footStep * CGFloat(level + 1)
            let width = max(proxy.size.width, 1)
            let scale = (width - 2 * Self.footInsets[level]) / width
            return effect
                .scaleEffect(1 - fraction * (1 - scale), anchor: .bottom)
                .offset(y: fraction * (footY - frame.maxY))
        }
    }
}
