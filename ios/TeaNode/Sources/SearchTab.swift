import SwiftUI
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

/// SearchTabScreen is the Search tab: a field, and under it what the typed
/// words find in every part, a few from each, grouped by part. A row opens
/// the item where it lives; Show all opens the part with the words in its own
/// search.
struct SearchTabScreen: View {
    let connection: Connection
    /// Opens a result, with the words that found it.
    let onOpen: (SearchResult, String) -> Void
    /// Opens a part with the words in its own search.
    let onShowAll: (SearchArea, String) -> Void
    @Environment(AppModel.self) private var app
    @State private var typedText = ""
    @State private var found: SearchEverythingResults?
    @State private var failedText: String?

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Search")
                .searchable(text: $typedText, prompt: Text("Search everything"))
                .task(id: typedText) { await search() }
                .onAppear {
                    #if DEBUG
                        // `-TeaNodeTab search -TeaNodeScreen search-results`: words typed already.
                        if DemoServer.isRequested, DemoServer.screen == "search-results", typedText.isEmpty { typedText = "harbor" }
                    #endif
                }
        }
    }

    @ViewBuilder
    private var content: some View {
        if SearchWords.normalized(typedText) == nil {
            ContentUnavailableView(
                "Search everything", systemImage: "magnifyingglass",
                description: Text("Find a name or a few words across your chats, mail, contacts, photos and more."))
        } else if let found {
            if found.isEmpty, found.failedAreas.isEmpty {
                ContentUnavailableView.search(text: typedText)
            } else {
                SearchResultsList(found: found, onOpen: onOpen, onShowAll: onShowAll)
            }
        } else if let failedText {
            LoadFailedView(problemText: failedText) { await run() }
        } else {
            ProgressView()
        }
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

/// SearchResultsList is what a search found: a section for each part with
/// results, and a line saying which parts could not be searched.
struct SearchResultsList: View {
    let found: SearchEverythingResults
    let onOpen: (SearchResult, String) -> Void
    let onShowAll: (SearchArea, String) -> Void

    var body: some View {
        List {
            ForEach(found.areas) { areaResults in
                if let area = areaResults.area {
                    SearchAreaSection(area: area, areaResults: areaResults, words: found.words, onOpen: onOpen, onShowAll: onShowAll)
                }
            }
            if !found.failedAreas.isEmpty {
                Section {
                    Label(String(localized: "Could not search \(failedNames)."), systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    /// The parts that did not answer, by name.
    private var failedNames: String {
        found.failedAreas.map { name in SearchArea(rawValue: name)?.title ?? name }.joined(separator: ", ")
    }
}

/// SearchAreaSection is one part's results under the part's name.
struct SearchAreaSection: View {
    let area: SearchArea
    let areaResults: SearchAreaResults
    let words: String
    let onOpen: (SearchResult, String) -> Void
    let onShowAll: (SearchArea, String) -> Void

    var body: some View {
        Section {
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

/// SearchResultRow is one thing found: what it is called, a line from it, and
/// when it happened.
struct SearchResultRow: View {
    let result: SearchResult

    var body: some View {
        // In the text's colors, which a button would otherwise draw green: a
        // result is a place to go, not an action (ios/DESIGN.md).
        VStack(alignment: .leading, spacing: 2) {
            Text(result.titleText.isEmpty ? result.detailText : result.titleText)
                .foregroundStyle(Color.primary)
                .lineLimit(2)
            if !result.titleText.isEmpty, !result.detailText.isEmpty {
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
