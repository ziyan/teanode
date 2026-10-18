import SwiftUI
import TeaNodeKit

/// PhotoLocationSheet says where photos were taken: the person types the name
/// of a city, the server answers with the cities that match (nearest first
/// when the first photo has a place to be near), and a tap on one places every
/// photo there. A photo that has a place can instead have it taken off, after
/// asking.
struct PhotoLocationSheet: View {
    let model: PhotosModel
    let photos: [Photo]
    /// Called once the server took a change, for the screen that opened this
    /// to leave Select mode.
    var onChanged: () -> Void = {}
    @Environment(\.dismiss) private var dismiss
    @State private var searchText: String
    @State private var places: [PhotoPlace] = []
    /// The words `places` answers, nil while nothing has been asked.
    @State private var answeredWords: String?
    @State private var isSaving = false
    @State private var isConfirmingRemoval = false

    init(model: PhotosModel, photos: [Photo], onChanged: @escaping () -> Void = {}) {
        self.model = model
        self.photos = photos
        self.onChanged = onChanged
        #if DEBUG
            // `-TeaNodeScreen photos-location` opens this with the words typed.
            let initialWords = DemoServer.isRequested && DemoServer.screen == "photos-location" ? "li" : ""
        #else
            let initialWords = ""
        #endif
        _searchText = State(initialValue: initialWords)
    }

    /// What is typed, without the spaces round it; nil when it is nothing.
    private var words: String? { PhotoSearch.words(from: searchText) }

    /// The first photo that says where it was taken: the cities are listed
    /// nearest to it.
    private var nearPhoto: Photo? { photos.first { $0.latitude != nil && $0.longitude != nil } }

    /// Whether any of the photos has a place that can be taken off.
    private var hasLocation: Bool { photos.contains { $0.latitude != nil && $0.longitude != nil } }

    /// What the list says above the cities, if anything.
    private var status: PhotoLocationChoices.Status {
        if words == nil { return hasLocation ? .idle : .prompt }
        if answeredWords == words { return places.isEmpty ? .noMatch : .idle }
        return places.isEmpty ? .asking : .idle
    }

    var body: some View {
        NavigationStack {
            PhotoLocationChoices(
                status: status, places: places, hasLocation: hasLocation, photoCount: photos.count, isConfirmingRemoval: $isConfirmingRemoval,
                onChoose: { place in Task { await choose(place) } }, onRemove: { Task { await remove() } }
            )
            .disabled(isSaving)
            .navigationTitle("Change location")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, placement: .navigationBarDrawer(displayMode: .always), prompt: Text("Search for a city"))
            .autocorrectionDisabled()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            // The words are asked of the server a moment after the typing
            // stops; an answer to words no longer typed is forgotten.
            .task(id: words) { await search() }
            // The screen's own toasts are behind this sheet.
            .toast(done: Bindable(model).doneText, problem: Bindable(model).problemText)
        }
        .presentationDetents([.large])
    }

    private func search() async {
        guard let words else {
            places = []
            answeredWords = nil
            return
        }
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        do {
            let found = try await model.connection.searchPhotoPlaces(words: words, nearLatitude: nearPhoto?.latitude, nearLongitude: nearPhoto?.longitude)
            guard !Task.isCancelled else { return }
            places = found
            answeredWords = words
        } catch {
            guard !Task.isCancelled else { return }
            model.problemText = model.describe(error)
        }
    }

    private func choose(_ place: PhotoPlace) async {
        isSaving = true
        defer { isSaving = false }
        if await model.changeLocation(of: photos, to: place) {
            onChanged()
            dismiss()
        }
    }

    private func remove() async {
        isSaving = true
        defer { isSaving = false }
        if await model.removeLocation(of: photos) {
            onChanged()
            dismiss()
        }
    }
}

/// PhotoLocationChoices is the list of the sheet: the cities that matched,
/// and Remove location below them when a photo has a place to take off.
struct PhotoLocationChoices: View {
    /// What the list says above the cities.
    enum Status {
        /// Nothing typed yet, and nothing to remove: it says what to do.
        case prompt
        /// Words typed, and the server not yet heard from.
        case asking
        /// The server answered and no city matched.
        case noMatch
        case idle
    }

    let status: Status
    let places: [PhotoPlace]
    let hasLocation: Bool
    let photoCount: Int
    @Binding var isConfirmingRemoval: Bool
    let onChoose: (PhotoPlace) -> Void
    let onRemove: () -> Void

    private var removalQuestion: LocalizedStringKey {
        photoCount == 1 ? LocalizedStringKey("Remove the location of this photo?") : LocalizedStringKey("Remove the location of these photos?")
    }

    var body: some View {
        List {
            statusRow
            if !places.isEmpty {
                Section {
                    ForEach(places, id: \.self) { place in
                        placeRow(place)
                    }
                }
            }
            if hasLocation {
                Section {
                    Button("Remove location", systemImage: "mappin.slash", role: .destructive) { isConfirmingRemoval = true }
                        .tint(.red)
                        .actionColor(isDestructive: true)
                        .confirmationDialog(removalQuestion, isPresented: $isConfirmingRemoval, titleVisibility: .visible) {
                            Button("Remove location", role: .destructive) { onRemove() }
                        } message: {
                            Text("This can't be undone. Copies already shared or downloaded keep the location.")
                        }
                }
            }
        }
    }

    /// The row above the results that says what to do, or that nothing matched.
    @ViewBuilder
    private var statusRow: some View {
        switch status {
        case .prompt:
            ContentUnavailableView("Search for a city", systemImage: "mappin.and.ellipse")
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
        case .noMatch:
            ContentUnavailableView("No cities match", systemImage: "magnifyingglass")
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
        case .asking:
            ProgressView()
                .frame(maxWidth: .infinity)
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
        case .idle:
            EmptyView()
        }
    }

    private func placeRow(_ place: PhotoPlace) -> some View {
        Button {
            onChoose(place)
        } label: {
            HStack {
                Text(verbatim: place.placeName)
                    .foregroundStyle(.primary)
                Spacer()
                if let detailText = secondaryText(of: place) {
                    Text(verbatim: detailText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    /// How far the city is from the first photo's place, when the server said.
    /// Otherwise, for two cities of one name in one country, which read the
    /// same, where each is.
    private func secondaryText(of place: PhotoPlace) -> String? {
        if let distanceKilometers = place.distanceKilometers {
            return LocationText.distance(distanceKilometers * 1_000)
        }
        guard places.filter({ $0.placeName == place.placeName }).count > 1 else { return nil }
        return String(format: "%.2f, %.2f", place.latitude, place.longitude)
    }
}

/// PhotoChangeLocationButton is the button of the bar under a list in Select
/// mode that opens the sheet for the photos chosen.
struct PhotoChangeLocationButton: View {
    let model: PhotosModel
    let photos: [Photo]
    let onChanged: () -> Void
    @State private var isChoosing = false

    var body: some View {
        Button("Change location", systemImage: "mappin") { isChoosing = true }
            .sheet(isPresented: $isChoosing) {
                PhotoLocationSheet(model: model, photos: photos, onChanged: onChanged)
            }
    }
}

#if DEBUG
    private let previewPlaces = [
        PhotoPlace(placeName: "Springfield, Exampleland", latitude: 10.5, longitude: 20.5, distanceKilometers: 3.2),
        PhotoPlace(placeName: "Springdale, Exampleland", latitude: 11.1, longitude: 21.0, distanceKilometers: 88),
        PhotoPlace(placeName: "Spring Harbor, Exampleland", latitude: 12.0, longitude: 19.0, distanceKilometers: 412.6),
    ]

    private struct PhotoLocationPreview: View {
        let status: PhotoLocationChoices.Status
        let places: [PhotoPlace]
        let hasLocation: Bool

        var body: some View {
            NavigationStack {
                PhotoLocationChoices(
                    status: status, places: places, hasLocation: hasLocation, photoCount: 1, isConfirmingRemoval: .constant(false), onChoose: { _ in }, onRemove: {}
                )
                .navigationTitle("Change location")
                .navigationBarTitleDisplayMode(.inline)
            }
        }
    }

    #Preview("Cities with distances, light") {
        PhotoLocationPreview(status: .idle, places: previewPlaces, hasLocation: true).preferredColorScheme(.light)
    }
    #Preview("Cities with distances, dark") {
        PhotoLocationPreview(status: .idle, places: previewPlaces, hasLocation: true).preferredColorScheme(.dark)
    }
    #Preview("Cities with no distances, light") {
        PhotoLocationPreview(
            status: .idle, places: previewPlaces.map { PhotoPlace(placeName: $0.placeName, latitude: $0.latitude, longitude: $0.longitude) }, hasLocation: false
        ).preferredColorScheme(.light)
    }
    #Preview("No cities match, light") {
        PhotoLocationPreview(status: .noMatch, places: [], hasLocation: false).preferredColorScheme(.light)
    }
    #Preview("No cities match, dark") {
        PhotoLocationPreview(status: .noMatch, places: [], hasLocation: true).preferredColorScheme(.dark)
    }
    #Preview("Nothing typed, light") {
        PhotoLocationPreview(status: .prompt, places: [], hasLocation: false).preferredColorScheme(.light)
    }
#endif
