import SwiftUI
import TeaNodeKit

// The dismissed cards (docs/planning/feed-dismissed-view-execplan.md): what the
// person put away, drawn by the feed's own card view, each with a way back.
// Cards that share a stack key and sit together are a deck, opened on a screen
// of their own from the cards already read.

/// FeedDismissedStackTarget is a deck of the dismissed list that is opened: its
/// stack key, and its name for the title.
struct FeedDismissedStackTarget: Hashable {
    let feedStackKey: String
    let title: String
}

/// FeedDismissedScreen is the Dismissed Cards screen pushed from the feed's
/// menu. It reads the dismissed cards itself, and the feed behind it is read
/// again when it goes, so a restored card is back there.
struct FeedDismissedScreen: View {
    let parent: FeedModel
    @State private var model: FeedModel
    @State private var openedStack: FeedDismissedStackTarget?

    init(parent: FeedModel) {
        self.parent = parent
        _model = State(initialValue: FeedModel(connection: parent.connection, isDismissedOnly: true))
    }

    var body: some View {
        FeedDismissedList(model: model, stackKey: nil) { deck in
            let title = deck.topItem.stackDisplayTitle
            openedStack = FeedDismissedStackTarget(
                feedStackKey: deck.feedStackKey, title: title.isEmpty ? String(localized: "More cards") : title)
        }
        .navigationTitle("Dismissed Cards")
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(item: $openedStack) { target in
            FeedDismissedStackScreen(model: model, target: target)
        }
        .task {
            await FeedFollowing.follow(connection: model.connection) { await model.load() }
        }
        .onDisappear {
            Task { await parent.load() }
        }
    }
}

/// FeedDismissedStackScreen is one deck of the dismissed list opened: its
/// cards, from the entries the list has already read (the server's stack read
/// lists only cards that are not dismissed). It closes when its last card is
/// restored.
struct FeedDismissedStackScreen: View {
    let model: FeedModel
    let target: FeedDismissedStackTarget
    @Environment(\.dismiss) private var dismiss

    private var hasCards: Bool {
        model.items.contains { $0.feedStackKey == target.feedStackKey }
    }

    var body: some View {
        FeedDismissedList(model: model, stackKey: target.feedStackKey) { _ in }
            .navigationTitle(target.title)
            .navigationBarTitleDisplayMode(.inline)
            .onChange(of: hasCards) { _, isStillShowingCards in
                if !isStillShowingCards { dismiss() }
            }
    }
}

/// FeedDismissedList is the dismissed cards of a model as a list: each card in
/// the feed's card view with nothing dimmed or disabled, a swipe that restores
/// it, and the information sheet with Restore in it. With a stack key it lists
/// the cards of that stack, one each; without, the whole list with
/// neighbours of one stack key made into decks.
struct FeedDismissedList: View {
    @Bindable var model: FeedModel
    let stackKey: String?
    /// Called when the person opens a deck.
    let onOpenDeck: (AgentFeedDeck) -> Void
    @Environment(AppModel.self) private var app
    /// The card whose information sheet is open, presented from the list and
    /// not from a row, which is rebuilt as the list changes.
    @State private var informedItem: AgentFeedItem?

    private var decks: [AgentFeedDeck] {
        if let stackKey {
            return model.items.filter { $0.feedStackKey == stackKey }.map { AgentFeedDeck(feedItems: [$0]) }
        }
        return AgentFeedDeck.group(model.items)
    }

    var body: some View {
        List {
            ForEach(decks) { deck in
                row(for: deck)
                    .listRowInsets(EdgeInsets(top: 6, leading: 16, bottom: 6, trailing: 16))
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
                    .onAppear {
                        model.noteAppeared(deck.feedItems[0])
                        if deck.id == decks.last?.id {
                            Task { await model.loadMore() }
                        }
                    }
                    .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                        Button {
                            Task { await model.restoreDismissed(deck.feedItems[0]) }
                        } label: {
                            Label("Restore", systemImage: "arrow.uturn.backward.circle")
                        }
                        .tint(Color.accentColor)
                    }
            }
            if model.isLoadingMore {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .listRowSeparator(.hidden)
                    .listRowBackground(Color.clear)
            }
        }
        .listStyle(.plain)
        .readableListWidth()
        .scrollContentBackground(.hidden)
        // Cards sit on the grouped page, as in the feed.
        .background(Color(.systemGroupedBackground))
        .overlay { placeholder }
        .refreshable { await model.refresh() }
        .task { await model.loadFirstIfNeeded() }
        .sheet(item: $informedItem) { item in
            FeedInformationSheet(item: item, canRestore: true) { action in
                informedItem = nil
                switch action {
                case .restore: Task { await model.restoreDismissed(item) }
                case .openGoals: app.openedSection = .goals
                case .showLess, .dismiss: break
                }
            }
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }

    // MARK: Rows

    @ViewBuilder
    private func row(for deck: AgentFeedDeck) -> some View {
        if deck.isDeck, stackKey == nil {
            card(deck.topItem, onToggleStack: { onOpenDeck(deck) })
                // The deck's feet show under its top card, as in the feed.
                .background(alignment: .bottom) { deckFeet }
                .padding(.bottom, 12)
        } else {
            card(deck.feedItems[0], onToggleStack: nil)
        }
    }

    private func card(_ item: AgentFeedItem, onToggleStack: (() -> Void)?) -> some View {
        FeedCard(
            item: item, isLiked: model.isLiked(item), isBusy: model.busyFeedItemIds.contains(item.feedItemId),
            isStackShown: onToggleStack != nil,
            isStackOpen: false,
            onChoose: { choice in choose(item, choice) },
            onToggleLike: { Task { await model.toggleLike(item) } },
            onDiscuss: { discuss(item) },
            onShowInformation: { informedItem = item },
            onToggleStack: onToggleStack
        )
    }

    private var deckFeet: some View {
        ZStack(alignment: .bottom) {
            deckFoot(inset: 34, drop: 12)
            deckFoot(inset: 22, drop: 6)
        }
    }

    private func deckFoot(inset: CGFloat, drop: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: FeedCard.cornerRadius, style: .continuous)
            .fill(FeedCard.surface)
            .overlay {
                RoundedRectangle(cornerRadius: FeedCard.cornerRadius, style: .continuous)
                    .strokeBorder(Color(.separator).opacity(0.6), lineWidth: 0.5)
            }
            .frame(height: 40)
            .padding(.horizontal, inset)
            .offset(y: drop)
            .accessibilityHidden(true)
    }

    // MARK: Placeholder and actions

    @ViewBuilder
    private var placeholder: some View {
        if !model.hasLoaded, let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView()
        } else if decks.isEmpty {
            ContentUnavailableView(
                "Nothing dismissed", systemImage: "tray",
                description: Text("Dismissed cards are kept until they expire."))
        }
    }

    private func choose(_ item: AgentFeedItem, _ choice: AgentFeedChoice) {
        switch choice.choiceRole {
        case .discuss:
            discuss(item)
        case .open:
            if let location = item.codingSessionLocation {
                app.openedCodingTarget = CodingTarget(computer: location.computer, paneId: location.paneId)
            }
            app.openedSection = .coding
        case .action, .dismiss:
            Task { await model.choose(item, choice) }
        }
    }

    private func discuss(_ item: AgentFeedItem) {
        app.askingAbout = AgentReference(feedItemId: item.feedItemId, feedItemTitle: item.title)
        // An empty id is the main conversation.
        app.openedConversationId = ""
    }
}

// MARK: Previews

private func previewDismissedCards() -> some View {
    ScrollView {
        VStack(spacing: 12) {
            FeedCard(
                item: AgentFeedItem(
                    feedItemId: "preview-dismissed", feedItemKind: "decision", title: "Tidy up your mail this week?",
                    body: "Forty-two newsletters have not been opened in a month.", pictureSymbol: "tray.full",
                    choices: [AgentFeedChoice(choiceKey: "approve", choiceLabel: "Approve", choiceRoleName: "action")], isDismissed: true,
                    dismissedAt: "2030-03-02T08:30:00Z", postedAt: "2030-03-01T06:00:00Z", updatedAt: "2030-03-01T06:00:00Z"),
                isLiked: false, isBusy: false, onChoose: { _ in }, onToggleLike: {}, onDiscuss: {}, onShowInformation: {})
        }
        .padding()
    }
}

#Preview("Dismissed card, light") { previewDismissedCards().environment(AppModel.shared).preferredColorScheme(.light) }
#Preview("Dismissed card, dark") { previewDismissedCards().environment(AppModel.shared).preferredColorScheme(.dark) }
