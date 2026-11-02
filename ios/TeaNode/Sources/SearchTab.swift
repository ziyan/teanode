import SwiftUI
import UIKit
import TeaNodeKit

/// PartSearch is a search the Search tab asks a part of the app to show: the
/// words typed there, in the part for the area. The part takes it and clears
/// it, as it does a link.
struct PartSearch: Equatable {
    let area: SearchArea
    let words: String
}

/// SearchedPhoto is a photo the Search tab found, with the words that found
/// it: Photos searches the library for them and opens the photo in the list.
struct SearchedPhoto: Equatable {
    let photoId: String
    let words: String
}

extension SearchArea {
    /// The area's name over its results, in the person's language.
    var title: String {
        switch self {
        case .chats: String(localized: "Chats")
        case .mail: String(localized: "Mail")
        case .contacts: String(localized: "Contacts")
        case .photos: String(localized: "Photos")
        case .memory: String(localized: "Knowledge")
        case .documents: String(localized: "Documents")
        case .recordings: String(localized: "Recordings")
        case .places: String(localized: "Places")
        case .finance: String(localized: "Finance")
        }
    }

    var systemImage: String {
        switch self {
        case .chats: AppSection.agent.systemImage
        case .mail: AppSection.mail.systemImage
        case .contacts: AppSection.contacts.systemImage
        case .photos: AppSection.photos.systemImage
        case .memory: AppSection.knowledge.systemImage
        case .documents: "doc.text"
        case .recordings: AppSection.recordings.systemImage
        case .places: "mappin"
        case .finance: AppSection.finance.systemImage
        }
    }

    /// Whether a part of the app can show all the area's results. Documents
    /// are found for the agent to read; no screen of the app lists them.
    var isShownInApp: Bool { self != .documents }
}

/// SearchTabScreen is the Search tab: a field, and under it the places of
/// the app the typed words name, then what they find in every part, a few
/// from each, grouped by part. A place opens where it is; a row opens the
/// item where it lives; Show all opens the part with the words in its own
/// search.
struct SearchTabScreen: View {
    let connection: Connection
    let mail: MailModel
    /// Opens a result, with the words that found it.
    let onOpen: (SearchResult, String) -> Void
    /// Opens a part with the words in its own search.
    let onShowAll: (SearchArea, String) -> Void
    @Environment(AppModel.self) private var app
    @State private var typedText = ""
    @State private var found: SearchEverythingResults?
    @State private var failedText: String?
    /// What the account may open on the management side, which decides the
    /// pages of Manage offered.
    @State private var access: OperatorAccess?

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Search")
                .searchable(text: $typedText, prompt: Text("Search everything"))
                .task(id: typedText) { await search() }
                .task { access = try? await connection.operatorAccess() }
                .onAppear {
                    #if DEBUG
                        // `-TeaNodeTab search -TeaNodeScreen search-results`: words typed already.
                        if DemoServer.isRequested, DemoServer.screen == "search-results", typedText.isEmpty { typedText = "harbor" }
                        // `search-places`: words that name places of the app, which are offered first.
                        if DemoServer.isRequested, DemoServer.screen == "search-places", typedText.isEmpty { typedText = "pass" }
                        // `search-open-settings-page` and `search-open-agent-page`: a place two pages deep in
                        // Settings, and a page of Agent settings, opened as a tap on its row opens it.
                        if DemoServer.isRequested, DemoServer.screen == "search-open-settings-page" { app.openPlace(.settings([.manage, .manageDomains])) }
                        if DemoServer.isRequested, DemoServer.screen == "search-open-agent-page" { app.openPlace(.settings([.agentSettings(.instructions)])) }
                    #endif
                }
        }
    }

    @ViewBuilder
    private var content: some View {
        if SearchWords.normalized(typedText) == nil {
            ContentUnavailableView(
                "Search everything", systemImage: "magnifyingglass",
                description: Text("Find a page of the app, or a name or a few words across your chats, mail, contacts, photos and more."))
        } else if !places.isEmpty {
            // The places are known at once; what the server finds follows them.
            SearchResultsList(
                found: found, places: places, failedText: failedText, isPhotosOffered: isPhotosOffered,
                onOpen: onOpen, onShowAll: onShowAll, onRetry: { await run() })
        } else if let found {
            if found.isEmpty, found.failedAreas.isEmpty, isPhotosOffered {
                // Nothing has the words, but a picture may show them.
                ContentUnavailableView {
                    Label("Nothing has these words", systemImage: "magnifyingglass")
                } description: {
                    Text(SearchPhotosRow.explanation)
                } actions: {
                    Button("Search Photos") { onShowAll(.photos, found.words) }
                }
            } else if found.isEmpty, found.failedAreas.isEmpty {
                ContentUnavailableView.search(text: typedText)
            } else {
                SearchResultsList(found: found, isPhotosOffered: isPhotosOffered, onOpen: onOpen, onShowAll: onShowAll)
            }
        } else if let failedText {
            LoadFailedView(problemText: failedText) { await run() }
        } else {
            ProgressView()
        }
    }

    /// Whether the search can go on in Photos, which also finds pictures by
    /// what is in them.
    private var isPhotosOffered: Bool { app.isSectionAvailable(.photos) }

    /// The places of the app whose names the typed words are, best first.
    private var places: [AppPlace] {
        let all = AppPlaces.all(app: app, mail: mail, access: access)
        let byId = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return PlaceSearch.matchingPlaceIds(all.map(\.placeName), words: typedText).compactMap { byId[$0] }
    }

    /// Asks a moment after the last key, as the other searches do.
    private func search() async {
        guard SearchWords.normalized(typedText) != nil else {
            found = nil
            failedText = nil
            return
        }
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        await run()
    }

    private func run() async {
        guard let words = SearchWords.normalized(typedText) else { return }
        do {
            let answer = try await connection.searchEverything(words: words)
            guard !Task.isCancelled else { return }
            found = answer
            failedText = nil
        } catch {
            guard !Task.isCancelled else { return }
            found = nil
            failedText = app.describe(error)
        }
    }
}

/// SearchResultsList is what a search found: the places of the app the
/// words name, a section for each part with results, and a line saying which
/// parts could not be searched. With places to show, it is shown while the
/// server is still asked, or could not be.
struct SearchResultsList: View {
    let found: SearchEverythingResults?
    var places: [AppPlace] = []
    var failedText: String? = nil
    let isPhotosOffered: Bool
    let onOpen: (SearchResult, String) -> Void
    let onShowAll: (SearchArea, String) -> Void
    var onRetry: (() async -> Void)? = nil
    @Environment(AppModel.self) private var app

    var body: some View {
        List {
            if !places.isEmpty {
                Section {
                    ForEach(places) { place in
                        Button {
                            app.openPlace(place.target)
                        } label: {
                            // In the text's colors: a place is somewhere to go, not an action (ios/DESIGN.md).
                            Label(place.shownTitle, systemImage: place.systemImage)
                                .foregroundStyle(Color.primary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .contentShape(.rect)
                        }
                    }
                } header: {
                    Text("Go to")
                }
            }
            if let found {
                ForEach(found.areas) { areaResults in
                    if let area = areaResults.area {
                        SearchAreaSection(area: area, areaResults: areaResults, words: found.words, onOpen: onOpen, onShowAll: onShowAll)
                    }
                }
                if isPhotosOffered, !isPhotosShowAllShown(found) {
                    SearchPhotosRow(words: found.words, onShowAll: onShowAll)
                }
                if !found.failedAreas.isEmpty {
                    Section {
                        Label(String(localized: "Could not search \(failedNames(found))."), systemImage: "exclamationmark.triangle")
                            .foregroundStyle(.secondary)
                    }
                }
            } else if let failedText {
                Section {
                    Label(failedText, systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.secondary)
                    if let onRetry {
                        Button("Try again") { Task { await onRetry() } }
                    }
                }
            } else {
                Section {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                }
            }
        }
    }

    /// Whether the Photos section already ends in Show all, which opens the
    /// same search in Photos.
    private func isPhotosShowAllShown(_ found: SearchEverythingResults) -> Bool {
        found.areas.contains { $0.area == .photos && $0.hasMore && !$0.results.isEmpty }
    }

    /// The parts that did not answer, by name.
    private func failedNames(_ found: SearchEverythingResults) -> String {
        found.failedAreas.map { name in SearchArea(rawValue: name)?.title ?? name }.joined(separator: ", ")
    }
}

/// SearchPhotosRow carries the words on to Photos' own search, which also
/// finds pictures by what is in them: the search here reads words only.
struct SearchPhotosRow: View {
    let words: String
    let onShowAll: (SearchArea, String) -> Void

    static let explanation = String(localized: "Photos also finds pictures by what is in them.")

    var body: some View {
        Section {
            Button {
                onShowAll(.photos, words)
            } label: {
                Label("Search Photos", systemImage: "photo.on.rectangle")
                    .actionColor()
            }
        } footer: {
            Text(Self.explanation)
        }
    }
}

/// SearchAreaSection is one part's results under the part's name. Photos are
/// a grid of pictures, as in Photos; everything else is rows.
struct SearchAreaSection: View {
    let area: SearchArea
    let areaResults: SearchAreaResults
    let words: String
    let onOpen: (SearchResult, String) -> Void
    let onShowAll: (SearchArea, String) -> Void

    var body: some View {
        Section {
            if area == .photos {
                SearchPhotoGrid(results: areaResults.results, words: words, onOpen: onOpen)
            } else {
                ForEach(Array(areaResults.results.enumerated()), id: \.offset) { _, result in
                    if case .nowhere = result.opening {
                        SearchResultRow(result: result)
                    } else {
                        Button {
                            onOpen(result, words)
                        } label: {
                            SearchResultRow(result: result)
                        }
                    }
                }
            }
            if areaResults.hasMore, area.isShownInApp {
                Button {
                    onShowAll(area, words)
                } label: {
                    Label("Show all in \(area.title)", systemImage: "arrow.forward")
                        .actionColor()
                }
            }
        } header: {
            Label(area.title, systemImage: area.systemImage)
        }
    }
}

/// SearchPhotoGrid is the photos found, as pictures a few across and nothing
/// under them: a file name says little about which photo it is. A tap opens
/// the photo, with the words that found it.
struct SearchPhotoGrid: View {
    let results: [SearchResult]
    let words: String
    let onOpen: (SearchResult, String) -> Void

    private static let spacing: CGFloat = 4

    var body: some View {
        // Four across on a phone held upright, more on a wider screen.
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 76), spacing: Self.spacing)], spacing: Self.spacing) {
            ForEach(results.filter { !$0.targetId.isEmpty }) { result in
                Button {
                    onOpen(result, words)
                } label: {
                    SearchPhotoThumbnail(photoId: result.targetId)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(accessibilityText(of: result))
            }
        }
        .padding(.vertical, Self.spacing)
    }

    /// What a screen reader says for a tile: it is a photo, and the day it
    /// was taken.
    private func accessibilityText(of result: SearchResult) -> String {
        var parts = [String(localized: "Photo")]
        if let happenedDate = result.happenedDate { parts.append(happenedDate.formatted(date: .complete, time: .omitted)) }
        return parts.joined(separator: ", ")
    }
}

/// SearchResultRow is one thing found: what it is called, a line from it, and
/// when it happened.
struct SearchResultRow: View {
    let result: SearchResult

    /// The result's name: the main conversation is named in the person's
    /// language, as the list of chats names it; anything else by its title,
    /// or by its line when it has no title.
    private var shownTitle: String {
        if result.isMain { return String(localized: "Main chat") }
        return result.titleText.isEmpty ? result.detailText : result.titleText
    }

    var body: some View {
        // In the text's colors, which a button would otherwise draw green: a
        // result is a place to go, not an action (ios/DESIGN.md).
        VStack(alignment: .leading, spacing: 2) {
            Text(shownTitle)
                .foregroundStyle(Color.primary)
                .lineLimit(2)
            if result.isMain || !result.titleText.isEmpty, !result.detailText.isEmpty {
                Text(result.detailText)
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
                    .lineLimit(2)
            }
            if let happenedDate = result.happenedDate {
                Text(happenedDate, format: .relative(presentation: .named))
                    .font(.caption)
                    .foregroundStyle(Color(uiColor: .tertiaryLabel))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }
}

/// SearchPhotoThumbnail is the picture of a photo found, a square that fills
/// the width it is given. A photo with no preview yet keeps the square, empty.
struct SearchPhotoThumbnail: View {
    let photoId: String
    @Environment(AppModel.self) private var app
    @State private var picture: UIImage?

    var body: some View {
        Color(.tertiarySystemFill)
            .aspectRatio(1, contentMode: .fit)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipShape(.rect(cornerRadius: 8, style: .continuous))
            .contentShape(.rect)
            .task(id: photoId) {
                picture = await app.picture("/api/v1/photos/\(photoId)/previews/small")
            }
    }
}
