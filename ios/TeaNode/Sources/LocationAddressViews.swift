import SwiftUI
import TeaNodeKit

/// LocationAddressScreen finds an address by the words the person types and
/// lists the matches from OpenStreetMap, anywhere in the world. Tapping one
/// hands it to `choose`; when that worked the screen closes, and when it
/// was refused the reason is said here and the matches stay.
struct LocationAddressScreen: View {
    let model: LocationModel
    /// What choosing a match does. Nil when it worked, else the reason.
    let choose: (FoundPlace) async -> String?
    @Environment(\.dismiss) private var dismiss
    @State private var searchText = ""
    /// What the last search found, for the words in `searchedWords`.
    @State private var matches: [FoundPlace]?
    @State private var searchedWords = ""
    @State private var isSearching = false
    @State private var isChoosing = false
    /// Said on this screen: a refusal is not left to a toast behind it.
    @State private var problemText: String?
    @State private var doneText: String?

    private var trimmedSearchText: String { searchText.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        List {
            if let matches, !matches.isEmpty {
                Section {
                    ForEach(matches) { match in
                        Button {
                            Task { await choose(match: match) }
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(verbatim: match.chosenAddressText).foregroundStyle(Color(.label))
                                if match.distanceMeters > 0 {
                                    Text(verbatim: LocationText.distance(match.distanceMeters))
                                        .font(.subheadline)
                                        .foregroundStyle(Color(.secondaryLabel))
                                }
                            }
                            .frame(minHeight: 44, alignment: .leading)
                        }
                        .disabled(isChoosing)
                    }
                } footer: {
                    OpenStreetMapCredit()
                }
            }
        }
        .overlay {
            if isSearching, matches == nil {
                ProgressView()
            } else if trimmedSearchText.isEmpty || matches == nil {
                ContentUnavailableView(
                    "Type an address and press Search", systemImage: "magnifyingglass",
                    description: Text("The place moves to the match you tap and keeps its address."))
            } else if matches?.isEmpty == true, searchedWords == trimmedSearchText {
                ContentUnavailableView.search(text: trimmedSearchText)
            }
        }
        .navigationTitle("Address")
        .navigationBarTitleDisplayMode(.inline)
        // The field stays open on a pushed screen, as the check-in sheet's does.
        .searchable(text: $searchText, placement: .navigationBarDrawer(displayMode: .always), prompt: Text("Type an address"))
        .onSubmit(of: .search) { Task { await search() } }
        .onChange(of: searchText) {
            // Words changed: what was found for the old ones is not the answer.
            if trimmedSearchText != searchedWords { matches = nil }
        }
        .toast(done: $doneText, problem: $problemText)
        #if DEBUG
            .task {
                // `-TeaNodeScreen location-address`: a search already made.
                guard DemoServer.isRequested, DemoServer.screen == "location-address" else { return }
                searchText = "12 Elm Street"
                await search()
            }
        #endif
    }

    private func search() async {
        let words = trimmedSearchText
        guard !words.isEmpty else { return }
        isSearching = true
        defer { isSearching = false }
        do {
            let found = try await model.searchAddresses(words: words)
            // Other words were typed while this was read.
            guard words == trimmedSearchText else { return }
            matches = found
            searchedWords = words
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    private func choose(match: FoundPlace) async {
        isChoosing = true
        defer { isChoosing = false }
        if let refusalText = await choose(match) {
            problemText = refusalText
        } else {
            dismiss()
        }
    }
}
