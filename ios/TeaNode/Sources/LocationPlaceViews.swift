import MapKit
import SwiftUI
import TeaNodeKit

/// LocationPlaceScreen is one place: its name and label, how wide it is on a
/// small map, the visits there, and the way to forget it.
struct LocationPlaceScreen: View {
    let model: LocationModel
    @State private var place: LocationPlace
    @State private var nameText: String
    @State private var labelChoice: String
    @State private var radiusMeters: Double
    @State private var position = MapCameraPosition.automatic
    @State private var visits: [PlaceVisit] = []
    @State private var nextVisitOffset: Int?
    @State private var isConfirmingForget = false
    @State private var isLoadingVisits = false
    @Environment(\.dismiss) private var dismiss

    init(model: LocationModel, place: LocationPlace) {
        self.model = model
        _place = State(initialValue: place)
        _nameText = State(initialValue: place.isNamedByPerson ? place.placeName : "")
        _labelChoice = State(initialValue: place.placeLabel)
        _radiusMeters = State(initialValue: place.radiusMeters)
    }

    var body: some View {
        Form {
            Section {
                TextField(place.placeName.isEmpty ? String(localized: "Place name") : place.placeName, text: $nameText)
                    .submitLabel(.done)
                    .onSubmit { Task { await saveName() } }
                Picker("Label", selection: $labelChoice) {
                    Text("None").tag("")
                    ForEach(PlaceLabel.allCases, id: \.rawValue) { label in
                        Label(label.title, systemImage: label.symbolName).tag(label.rawValue)
                    }
                }
            } footer: {
                if !place.addressText.isEmpty { Text(verbatim: place.addressText) }
            }
            Section {
                Map(position: $position, interactionModes: []) {
                    MapCircle(center: place.coordinate, radius: radiusMeters)
                        .foregroundStyle(.tint.opacity(0.2))
                        .stroke(.tint, lineWidth: 2)
                    Marker(place.shownName, systemImage: place.symbolName, coordinate: place.coordinate)
                }
                .frame(height: 180)
                .listRowInsets(EdgeInsets())
                .accessibilityLabel("Map of the place and its radius")
                LabeledContent("Radius") {
                    Text(verbatim: LocationText.radius(radiusMeters))
                }
                Slider(value: $radiusMeters, in: 25...2000, step: 25) {
                    Text("Radius")
                } onEditingChanged: { isEditing in
                    if !isEditing { Task { await saveRadius() } }
                }
            } footer: {
                Text("A visit counts as here when you are within this distance of the pin.")
            }
            Section {
                ForEach(visits) { visit in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: visit.anchorDate.map { LocationText.dayTitle($0) } ?? "")
                        Text(verbatim: LocationText.summary(of: visit))
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                    .onAppear {
                        if visit.id == visits.last?.id { Task { await loadVisits() } }
                    }
                }
                if isLoadingVisits { ProgressView().frame(maxWidth: .infinity) }
            } header: {
                Text("Visits")
            } footer: {
                OpenStreetMapCredit()
            }
            Section {
                Button("Forget this place", systemImage: "trash", role: .destructive) {
                    isConfirmingForget = true
                }
                .confirmationDialog("Forget \(place.shownName)?", isPresented: $isConfirmingForget, titleVisibility: .visible) {
                    Button("Forget this place", role: .destructive) {
                        Task {
                            if await model.forget(place) { dismiss() }
                        }
                    }
                } message: {
                    Text("Its visits are deleted with it. This cannot be undone.")
                }
            }
        }
        .navigationTitle(place.shownName)
        .navigationBarTitleDisplayMode(.inline)
        .task {
            focusMap()
            visits = []
            nextVisitOffset = nil
            await loadVisits()
        }
        .onChange(of: labelChoice) { _, choice in
            guard choice != place.placeLabel else { return }
            Task {
                if let saved = await model.save(PlaceChange(placeId: place.id, placeLabel: choice)) {
                    place = saved
                } else {
                    labelChoice = place.placeLabel
                }
            }
        }
        .onChange(of: radiusMeters) { focusMap() }
        .onDisappear { Task { await saveName() } }
    }

    private func focusMap() {
        position = .region(MKCoordinateRegion(center: place.coordinate, latitudinalMeters: radiusMeters * 3.2, longitudinalMeters: radiusMeters * 3.2))
    }

    /// Saves the name when the person changed it. An emptied box puts the
    /// name back instead: a place always has one.
    private func saveName() async {
        let trimmed = nameText.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            nameText = place.isNamedByPerson ? place.placeName : ""
            return
        }
        guard trimmed != place.placeName || !place.isNamedByPerson else { return }
        if let saved = await model.save(PlaceChange(placeId: place.id, placeName: trimmed)) {
            place = saved
            nameText = saved.placeName
        }
    }

    private func saveRadius() async {
        guard radiusMeters != place.radiusMeters else { return }
        if let saved = await model.save(PlaceChange(placeId: place.id, radiusMeters: radiusMeters)) {
            place = saved
        } else {
            radiusMeters = place.radiusMeters
        }
    }

    private func loadVisits() async {
        guard !isLoadingVisits, visits.isEmpty || nextVisitOffset != nil else { return }
        isLoadingVisits = true
        defer { isLoadingVisits = false }
        do {
            let page = try await model.visits(atPlace: place.id, offset: nextVisitOffset ?? 0)
            visits += page.placeVisits
            nextVisitOffset = page.nextOffset
        } catch {
            model.problemText = error.localizedDescription
        }
    }
}

/// CheckInSheet lists the named things near the phone and records that the
/// person is at the one they tap.
struct CheckInSheet: View {
    let model: LocationModel
    @Environment(\.dismiss) private var dismiss
    @State private var found: [FoundPlace]?
    @State private var loadFailedText: String?
    @State private var isCheckingIn = false

    var body: some View {
        NavigationStack {
            List {
                if let found {
                    Section {
                        ForEach(found) { thing in
                            Button {
                                Task { await checkIn(at: thing) }
                            } label: {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(verbatim: thing.placeName).foregroundStyle(.primary)
                                    Text(verbatim: details(of: thing))
                                        .font(.subheadline)
                                        .foregroundStyle(.secondary)
                                }
                            }
                            .disabled(isCheckingIn)
                        }
                    } footer: {
                        OpenStreetMapCredit()
                    }
                }
            }
            .overlay {
                if let loadFailedText {
                    LoadFailedView(problemText: loadFailedText) { await load() }
                } else if let found {
                    if found.isEmpty {
                        ContentUnavailableView(
                            "Nothing with a name nearby", systemImage: "mappin.slash",
                            description: Text("Try again from a place with a shop, park or building on the map."))
                    }
                } else {
                    ProgressView()
                }
            }
            .navigationTitle("Check in")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .task { await load() }
        }
    }

    /// The kind of thing it is and how far away, such as "Cafe · 40 m".
    private func details(of thing: FoundPlace) -> String {
        let kind = LocationText.category(thing.osmCategory)
        let distance = LocationText.distance(thing.distanceMeters)
        return kind.isEmpty ? distance : String(localized: "\(kind) · \(distance)")
    }

    private func load() async {
        loadFailedText = nil
        guard let position = await model.currentPosition() else {
            // Said in a toast by the model; the sheet says it too, so it is
            // not left spinning.
            loadFailedText = model.problemText ?? String(localized: "The phone could not tell where it is.")
            return
        }
        do {
            found = try await model.nearby(latitudeDegrees: position.latitudeDegrees, longitudeDegrees: position.longitudeDegrees)
        } catch {
            loadFailedText = error.loadProblemText
        }
    }

    private func checkIn(at thing: FoundPlace) async {
        isCheckingIn = true
        defer { isCheckingIn = false }
        let target: CheckInTarget = thing.placeId.map { .place($0) } ?? .found(thing)
        if await model.checkIn(target, placeName: thing.placeName) { dismiss() }
    }
}
