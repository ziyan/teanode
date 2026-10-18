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
    @State private var isChangingAddress = false
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
                LabeledContent("Name") {
                    TextField("Name", text: $nameText, prompt: Text(verbatim: place.placeName.isEmpty ? String(localized: "Place name") : place.placeName))
                        .multilineTextAlignment(.trailing)
                        .submitLabel(.done)
                        .onSubmit { Task { await saveName() } }
                }
                Picker("Label", selection: $labelChoice) {
                    Text("None").tag("")
                    ForEach(PlaceLabel.allCases, id: \.rawValue) { label in
                        Label(label.title, systemImage: label.symbolName).tag(label.rawValue)
                    }
                }
            }
            Section {
                LabeledContent("Address") {
                    if place.addressText.isEmpty {
                        Text("No address").foregroundStyle(.secondary)
                    } else {
                        Text(verbatim: place.addressText).multilineTextAlignment(.trailing)
                    }
                }
                Button {
                    isChangingAddress = true
                } label: {
                    Label("Change address", systemImage: "magnifyingglass")
                }
                .frame(minHeight: 44)
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
                Text("A visit counts as here when you are within this distance of the center of the place.")
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
                Button(role: .destructive) {
                    isConfirmingForget = true
                } label: {
                    Label("Forget this place", systemImage: "trash").foregroundStyle(.red)
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
        .navigationDestination(isPresented: $isChangingAddress) {
            LocationAddressScreen(model: model) { address in
                await moveHere(to: address)
            }
        }
        .task {
            focusMap()
            #if DEBUG
                // `-TeaNodeScreen location-address`: the address screen over this one.
                if DemoServer.isRequested, DemoServer.screen == "location-address" { isChangingAddress = true }
            #endif
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

    /// Moves the place to the address the person chose and gives it that
    /// address; the map then shows the new position. Nil when it worked, else
    /// the reason, for the address screen to say.
    private func moveHere(to address: FoundPlace) async -> String? {
        let result = await model.saveAddress(
            PlaceChange(
                placeId: place.id, latitudeDegrees: address.latitudeDegrees, longitudeDegrees: address.longitudeDegrees,
                addressText: address.chosenAddressText))
        guard let saved = result.saved else { return result.refusalText }
        place = saved
        focusMap()
        return nil
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

/// CheckInSheet lists the named places near the phone and records that the
/// person is at the one they tap. A search finds a place by its name instead.
struct CheckInSheet: View {
    let model: LocationModel
    @Environment(\.dismiss) private var dismiss
    @State private var nearbyPlaces: [FoundPlace]?
    @State private var position: (latitudeDegrees: Double, longitudeDegrees: Double)?
    /// How precise the phone says the position is; negative when it could not tell.
    @State private var positionAccuracyMeters: Double?
    @State private var loadFailedText: String?
    @State private var searchText = ""
    /// What the last search found, for the words in `searchedWords`.
    @State private var searchedPlaces: [FoundPlace]?
    @State private var searchedWords = ""
    @State private var searchNextOffset: Int?
    @State private var isSearching = false
    @State private var isCheckingIn = false
    /// Said inside the sheet: the toast of the screen behind it is hidden.
    @State private var problemText: String?
    @State private var doneText: String?

    private var trimmedSearchText: String { searchText.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// The places listed: what the search found while words are typed, else
    /// the places near the phone.
    private var listedPlaces: [FoundPlace]? {
        trimmedSearchText.isEmpty ? nearbyPlaces : searchedPlaces
    }

    var body: some View {
        NavigationStack {
            List {
                if position != nil {
                    Section {
                        Button {
                            Task { await checkInHere() }
                        } label: {
                            HStack(spacing: 12) {
                                Image(systemName: "location.fill")
                                    .symbolRenderingMode(.hierarchical)
                                    .foregroundStyle(Color(.secondaryLabel))
                                    .frame(width: 28)
                                    .accessibilityHidden(true)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Check in here").foregroundStyle(Color(.label))
                                    if let precisionText {
                                        Text(verbatim: precisionText)
                                            .font(.subheadline)
                                            .foregroundStyle(Color(.secondaryLabel))
                                    }
                                }
                            }
                            .frame(minHeight: 44, alignment: .leading)
                        }
                        .disabled(isCheckingIn)
                    }
                }
                if let listedPlaces {
                    Section {
                        ForEach(listedPlaces) { foundPlace in
                            Button {
                                Task { await checkIn(at: foundPlace) }
                            } label: {
                                HStack(spacing: 12) {
                                    if let labelSymbolName = foundPlace.labelSymbolName {
                                        Image(systemName: labelSymbolName)
                                            .symbolRenderingMode(.hierarchical)
                                            .foregroundStyle(Color(.secondaryLabel))
                                            .frame(width: 28)
                                            .accessibilityHidden(true)
                                    }
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(verbatim: foundPlace.shownName).foregroundStyle(Color(.label))
                                        if let secondaryName = foundPlace.secondaryName {
                                            Text(verbatim: secondaryName)
                                                .font(.subheadline)
                                                .foregroundStyle(Color(.secondaryLabel))
                                        }
                                        Text(verbatim: details(of: foundPlace))
                                            .font(.subheadline)
                                            .foregroundStyle(Color(.secondaryLabel))
                                    }
                                }
                                .frame(minHeight: 44, alignment: .leading)
                            }
                            .disabled(isCheckingIn)
                        }
                        if !trimmedSearchText.isEmpty, searchNextOffset != nil {
                            Button("More places") { Task { await search(isNextPage: true) } }
                                .disabled(isSearching)
                                .frame(minHeight: 44)
                        }
                    } footer: {
                        OpenStreetMapCredit()
                    }
                }
            }
            .overlay {
                if let loadFailedText {
                    LoadFailedView(problemText: loadFailedText) { await load() }
                } else if !trimmedSearchText.isEmpty {
                    if isSearching, searchedPlaces == nil {
                        ProgressView()
                    } else if searchedPlaces == nil {
                        // Words typed and not yet searched for.
                        ContentUnavailableView(
                            "Press Search to find places by name", systemImage: "magnifyingglass",
                            description: Text("Places near you stay listed until you search."))
                    } else if searchedPlaces?.isEmpty == true, searchedWords == trimmedSearchText {
                        ContentUnavailableView.search(text: trimmedSearchText)
                    }
                } else if let nearbyPlaces {
                    if nearbyPlaces.isEmpty {
                        ContentUnavailableView(
                            "Nothing with a name nearby", systemImage: "mappin.slash",
                            description: Text("Try again from a place with a shop, park or building on the map, or find a place by its name."))
                    }
                } else {
                    ProgressView()
                }
            }
            .navigationTitle("Check in")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, prompt: Text("Find a place"))
            .onSubmit(of: .search) { Task { await search(isNextPage: false) } }
            .onChange(of: searchText) {
                // Words changed: what was found for the old ones is not the answer.
                if trimmedSearchText != searchedWords {
                    searchedPlaces = nil
                    searchNextOffset = nil
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .toast(done: $doneText, problem: $problemText)
            .task { await load() }
        }
    }

    /// How precise the position under "Check in here" is, such as "Within
    /// 20 m"; when the phone could only find the person roughly, it says so.
    /// Nil when the phone did not say.
    private var precisionText: String? {
        guard let positionAccuracyMeters, positionAccuracyMeters >= 0 else { return nil }
        let withinText = String(localized: "Within \(LocationText.distance(positionAccuracyMeters.rounded()))")
        guard positionAccuracyMeters > 300 else { return withinText }
        return String(localized: "\(withinText). The phone could only find you roughly.")
    }

    /// The kind of place it is and how far away, such as "Cafe · 40 m".
    private func details(of foundPlace: FoundPlace) -> String {
        let categoryName = LocationText.category(foundPlace.osmCategory)
        let distanceText = LocationText.distance(foundPlace.distanceMeters)
        return categoryName.isEmpty ? distanceText : String(localized: "\(categoryName) · \(distanceText)")
    }

    private func load() async {
        loadFailedText = nil
        guard let current = await model.currentReading() else {
            // Said in a toast by the model; the sheet says it too, so it is
            // not left spinning.
            loadFailedText = model.problemText ?? String(localized: "The phone could not tell where it is.")
            return
        }
        position = (current.latitudeDegrees, current.longitudeDegrees)
        positionAccuracyMeters = current.accuracyMeters
        do {
            nearbyPlaces = try await model.nearby(latitudeDegrees: current.latitudeDegrees, longitudeDegrees: current.longitudeDegrees)
        } catch {
            loadFailedText = error.loadProblemText
        }
    }

    /// Looks for the words typed among the places near the phone; the next
    /// page when asked.
    private func search(isNextPage: Bool) async {
        let words = trimmedSearchText
        guard !words.isEmpty, let position else { return }
        isSearching = true
        defer { isSearching = false }
        do {
            let page = try await model.searchPlaces(words: words, near: position, offset: isNextPage ? searchNextOffset ?? 0 : 0)
            // Other words were typed while this was read.
            guard words == trimmedSearchText else { return }
            let known = isNextPage ? searchedPlaces ?? [] : []
            searchedPlaces = known + page.foundPlaces
            searchedWords = words
            searchNextOffset = page.nextOffset
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    /// Checks in at the phone's position with nothing else: the server uses
    /// the person's own place there, or makes one and names it afterwards.
    private func checkInHere() async {
        guard let position else { return }
        isCheckingIn = true
        defer { isCheckingIn = false }
        let target = CheckInTarget.position(latitudeDegrees: position.latitudeDegrees, longitudeDegrees: position.longitudeDegrees)
        if let refusalText = await model.checkIn(target, placeName: nil) {
            problemText = refusalText
        } else {
            dismiss()
        }
    }

    private func checkIn(at foundPlace: FoundPlace) async {
        isCheckingIn = true
        defer { isCheckingIn = false }
        let target: CheckInTarget = foundPlace.placeId.map { .place($0) } ?? .found(foundPlace)
        if let refusalText = await model.checkIn(target, placeName: foundPlace.shownName) {
            problemText = refusalText
        } else {
            dismiss()
        }
    }
}
