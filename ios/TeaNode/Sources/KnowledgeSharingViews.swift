import SwiftUI
import TeaNodeKit

// Pages of the agent's memory shared with other people, and the pages other
// people shared with the person. What a reader is shown is decided on the
// server; these screens draw it. A shared page has no edit controls because
// nothing here can write it. docs/subsystems/memory.md ("Sharing") says what
// is left out and why.

/// SharedKnowledgeLink is a page another person shared that a link in an
/// answer asks to open: memory:PATH#N@username.
struct SharedKnowledgeLink: Hashable {
    let ownerUsername: String
    let path: String
}

/// SharedKnowledgeSection is the "Shared with you" part of the Knowledge
/// list at the top level: a row for each page another person shared, under
/// their picture and name. It draws nothing while it reads, when it fails (the
/// person's own pages are still the thing being looked at) and when nothing
/// is shared. The list that holds it reads the groups, so that a list with
/// none carries no section.
struct SharedKnowledgeSection: View {
    let groups: [SharedKnowledgeOwnerGroup]

    var body: some View {
        if !groups.isEmpty {
            Section {
                ForEach(groups) { group in
                    HStack(spacing: 8) {
                        AvatarView(name: group.ownerName, avatarUrl: group.ownerAvatarUrl, colorIndex: group.ownerAvatarColorIndex, size: 24)
                        Text(group.ownerName)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityAddTraits(.isHeader)
                    ForEach(group.roots) { root in
                        NavigationLink(value: KnowledgePlace.sharedPage(ownerUserId: root.ownerUserId, path: root.path)) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(root.shownName).font(.headline).lineLimit(1)
                                if !root.opening.isEmpty {
                                    Text(root.opening).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
                                }
                            }
                        }
                        .knowledgeListRow()
                    }
                }
            } header: {
                ListPeriodHeader(title: String(localized: "Shared with you"))
            }
            .listSectionSeparator(.hidden)
        }
    }
}

/// SharedKnowledgeSections is a shared page as it is read, and as its owner
/// sees it before they confirm a share: the opening, the facts
/// with where they were read (and the words, where the owner shared them),
/// the links to other shared pages and the pages under it. With an owner the
/// links and the pages under it open; without one (the preview) they do not.
struct SharedKnowledgeSections: View {
    let page: SharedKnowledgePage
    let openingOwnerUserId: String?

    var body: some View {
        Section {
            if !page.aliases.isEmpty {
                Text("Also called \(page.aliases.joined(separator: ", "))").font(.footnote).foregroundStyle(.secondary)
            }
            if page.opening.isEmpty {
                Text("No summary yet.").foregroundStyle(.secondary)
            } else {
                MarkdownView(text: page.opening)
            }
            Text("The owner's longer overview of this page is not shared.").font(.footnote).foregroundStyle(.secondary)
        }
        Section {
            if page.presentFacts.isEmpty {
                Text("No facts on this page are shared.").foregroundStyle(.secondary)
            }
            ForEach(page.presentFacts, id: \.number) { fact in
                VStack(alignment: .leading, spacing: 4) {
                    Text("#\(fact.number) \(fact.text)")
                    if fact.isInferred {
                        Chip(text: String(localized: "Worked out, not told"), isAlert: false)
                    }
                    if let source = fact.evidenceSource, !source.isEmpty {
                        Text(source).font(.footnote).foregroundStyle(.secondary)
                    }
                    if let quote = fact.evidenceQuote, !quote.isEmpty {
                        Text(quote).font(.footnote).italic().foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
        } header: {
            Text("Facts")
        }
        if !page.presentEdges.isEmpty {
            Section {
                ForEach(page.presentEdges, id: \.self) { edge in
                    let far = edge.fromPath == page.path ? edge.toPath : edge.fromPath
                    let relation = edge.relation.replacingOccurrences(of: "_", with: " ")
                    if let openingOwnerUserId {
                        NavigationLink(value: KnowledgePlace.sharedPage(ownerUserId: openingOwnerUserId, path: far)) {
                            linkRow(edge, relation: relation, far: far)
                        }
                    } else {
                        linkRow(edge, relation: relation, far: far)
                    }
                }
            } header: {
                Text("Links to other shared pages")
            }
        }
        if !page.presentChildren.isEmpty {
            Section {
                ForEach(page.presentChildren, id: \.path) { child in
                    if let openingOwnerUserId {
                        NavigationLink(value: KnowledgePlace.sharedPage(ownerUserId: openingOwnerUserId, path: child.path)) {
                            childRow(child)
                        }
                    } else {
                        childRow(child)
                    }
                }
            } header: {
                Text("Pages under it")
            }
        }
    }

    private func linkRow(_ edge: SharedKnowledgeEdge, relation: String, far: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("\(relation) \(far)").font(.body)
            if edge.isProposed {
                Text("The agent’s guess").font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    private func childRow(_ child: SharedKnowledgeChild) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(child.name.isEmpty ? child.path : child.name)
            if let opening = child.opening, !opening.isEmpty {
                Text(opening).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
            }
        }
    }
}

/// SharedKnowledgePageView is a page another person shared, read only, with
/// whose it is.
struct SharedKnowledgePageView: View {
    let connection: Connection
    let ownerUserId: String
    let pagePath: String
    @State private var page: SharedKnowledgePage?
    @State private var problemText: String?
    /// Whether the server answered that the page is not shared, as against a
    /// read that did not get through.
    @State private var isNotShared = false

    var body: some View {
        List {
            if let page {
                Section {
                    HStack(spacing: 8) {
                        AvatarView(name: page.shownOwner, avatarUrl: page.ownerAvatarUrl, colorIndex: page.ownerAvatarColorIndex, size: 28)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Shared by \(page.shownOwner)")
                            Text(page.path).font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    HStack(spacing: 6) {
                        Chip(text: String(localized: "Read only"), isAlert: false)
                        if page.isQuoteShared {
                            Chip(text: String(localized: "Quotes shown"), isAlert: false)
                        }
                    }
                }
                SharedKnowledgeSections(page: page, openingOwnerUserId: ownerUserId)
            }
        }
        .navigationTitle(page?.shownName ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if isNotShared {
                // A page that is not shared answers as not found, whoever
                // asks and whatever the reason, so the screen can only say
                // that.
                ContentUnavailableView(
                    "This page is not available", systemImage: "lock",
                    description: Text("This page is not shared with you, or the person stopped sharing it."))
            } else if let problemText, page == nil {
                LoadFailedView(problemText: problemText) { await load() }
            } else if page == nil {
                ProgressView()
            }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            page = try await connection.sharedKnowledgePage(ownerUserId: ownerUserId, path: pagePath)
            problemText = nil
            isNotShared = false
        } catch {
            // With the page already on screen a failed refresh leaves it be.
            guard page == nil else { return }
            if error.isNotFoundRefusal { isNotShared = true } else { problemText = error.loadProblemText }
        }
    }
}

/// KnowledgeShareSheet shares one of the person's pages, and everything under
/// it, with a person they share a circle with or a circle they are in. It
/// lists who the page is already shared with (a share of a page above it
/// reaches this one too), with a way to stop each, and offers the people and
/// circles the server would accept. Sharing opens exactly what the other side
/// will be shown first, and is confirmed there, once that has been read.
struct KnowledgeShareSheet: View {
    let connection: Connection
    let pagePath: String
    @Environment(\.dismiss) private var dismiss
    @State private var shares: [KnowledgeShare] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var isQuoteShared = false
    @State private var stopping: KnowledgeShare?
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            List {
                currentShares
                if isLoaded && people.isEmpty && circles.isEmpty {
                    Section {
                        Text("You do not share a circle with anybody yet. Make or join a circle to share a page.")
                            .foregroundStyle(.secondary)
                        OpenCirclesLink(connection: connection)
                    }
                } else if isLoaded {
                    Section {
                        ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                    } header: {
                        Text("Share with")
                    } footer: {
                        Text("Choose a person, or a whole circle. They read this page and everything under it as it is now, and cannot change it.")
                    }
                    Section {
                        Toggle("Also show the words each fact was read in", isOn: $isQuoteShared)
                    } footer: {
                        Text("Those words are quoted from your mail and documents. Off, they see what a fact says and what kind of thing it was read in, not the words. Words from a chat are never shown, since they are other people's too.")
                    }
                    Section {
                        NavigationLink {
                            KnowledgeSharePreviewScreen(
                                connection: connection, pagePath: pagePath, granteeUserId: chosen?.granteeUserId,
                                granteeCircleId: chosen?.granteeCircleId, isQuoteShared: isQuoteShared
                            ) {
                                await share()
                            }
                        } label: {
                            Label("Share", systemImage: "square.and.arrow.up")
                        }
                        .disabled(chosen == nil || isWorking)
                    } footer: {
                        Text("Before it is shared, you see exactly what they will be shown.")
                    }
                }
            }
            .navigationTitle("Share this page")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .overlay {
                if let loadProblemText, !isLoaded {
                    LoadFailedView(problemText: loadProblemText) { await load() }
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .refreshable { await load() }
            .task { await load() }
            .toast(done: $doneText, problem: $problemText)
        }
    }

    @ViewBuilder
    private var currentShares: some View {
        let covering = shares.filter { $0.covers(pagePath) }
        Section {
            if isLoaded && covering.isEmpty {
                Text("Not shared with anybody yet.").foregroundStyle(.secondary)
            }
            ForEach(covering) { share in
                VStack(alignment: .leading, spacing: 2) {
                    Text(Self.name(of: share))
                    if share.path != pagePath {
                        Text("Shared as part of \(share.path)").font(.footnote).foregroundStyle(.secondary)
                    }
                    if share.isQuoteShared {
                        Text("Quotes shown").font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
                .confirmationDialog(
                    "Stop sharing", isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                    titleVisibility: .visible, presenting: stopping
                ) { share in
                    Button("Stop sharing", role: .destructive) { stop(share) }
                } message: { share in
                    Text("\(Self.name(of: share)) will no longer read this page.")
                }
                .swipeActions {
                    Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                }
                .contextMenu {
                    Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                }
            }
        } header: {
            Text("Shared with")
        } footer: {
            if !covering.isEmpty {
                Text("Swipe left, or touch and hold, to stop sharing.")
            }
        }
    }

    private static func name(of share: KnowledgeShare) -> String {
        share.isWithCircle ? String(localized: "Everyone in \(share.granteeDisplayName)") : share.granteeDisplayName
    }

    private func load() async {
        do {
            async let readShares = connection.knowledgeShares()
            async let readPeople = connection.calendarSharePeople()
            async let readCircles = connection.calendarShareCircles()
            (shares, people, circles) = try await (readShares, readPeople, readCircles)
            isLoaded = true
            loadProblemText = nil
        } catch {
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    /// Shares the page with the choice, once its preview has been read and
    /// confirmed. Answers whether it was shared, so that the preview can go
    /// back to this sheet, which says so.
    private func share() async -> Bool {
        guard let target = chosen else { return false }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        isWorking = true
        defer { isWorking = false }
        do {
            try await connection.shareKnowledgePage(
                path: pagePath, granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId, isQuoteShared: isQuoteShared)
            doneText = String(localized: "Shared with \(name)")
            chosen = nil
            isQuoteShared = false
            shares = (try? await connection.knowledgeShares()) ?? shares
            return true
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Sharing was not changed. Check your connection and try again."))
            return false
        }
    }

    private func stop(_ share: KnowledgeShare) {
        let name = Self.name(of: share)
        isWorking = true
        Task {
            do {
                try await connection.unshareKnowledgePage(shareId: share.id)
                doneText = String(localized: "Stopped sharing with \(name)")
                shares = try await connection.knowledgeShares()
            } catch {
                problemText = error.refusalReason(orSay: String(localized: "Sharing was not changed. Check your connection and try again."))
            }
            isWorking = false
        }
    }
}

/// KnowledgeSharePreviewScreen is exactly what the person or circle chosen
/// would be shown of the page, drawn the way they would read it, and how many
/// pages the share reaches. Nothing is shared by looking: Share is here, and is
/// on only once the preview has been read, so a page is never shared unseen.
struct KnowledgeSharePreviewScreen: View {
    let connection: Connection
    let pagePath: String
    let granteeUserId: String?
    let granteeCircleId: String?
    let isQuoteShared: Bool
    /// Shares the page; answers whether it was.
    let onShare: () async -> Bool
    @Environment(\.dismiss) private var dismiss
    @State private var preview: KnowledgeSharePreview?
    @State private var problemText: String?
    @State private var isSharing = false

    var body: some View {
        List {
            if let preview {
                Section {
                    Text(verbatim: Self.pagesText(of: preview))
                }
                SharedKnowledgeSections(page: preview.page, openingOwnerUserId: nil)
            }
        }
        .navigationTitle("What they will see")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Share") {
                    isSharing = true
                    Task {
                        if await onShare() { dismiss() }
                        isSharing = false
                    }
                }
                .disabled(preview == nil || isSharing)
            }
        }
        .overlay {
            if let problemText {
                LoadFailedView(problemText: problemText) { await load() }
            } else if preview == nil {
                ProgressView()
            }
        }
        .task { await load() }
    }

    /// How many pages the share reaches, as the dashboard says it.
    private static func pagesText(of preview: KnowledgeSharePreview) -> String {
        let count = preview.reachedPageCount
        switch (preview.isReachedPageCountCut, count == 1) {
        case (false, true): return String(localized: "This shares 1 page: this one and everything under it.")
        case (false, false): return String(localized: "This shares \(count) pages: this one and everything under it.")
        case (true, true): return String(localized: "This shares at least 1 page: this one and everything under it.")
        case (true, false): return String(localized: "This shares at least \(count) pages: this one and everything under it.")
        }
    }

    private func load() async {
        do {
            preview = try await connection.previewKnowledgeShare(
                path: pagePath, granteeUserId: granteeUserId, granteeCircleId: granteeCircleId, isQuoteShared: isQuoteShared)
            problemText = nil
        } catch {
            problemText = error.loadProblemText
        }
    }
}
