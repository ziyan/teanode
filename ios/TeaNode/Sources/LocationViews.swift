import MapKit
import SwiftUI
import TeaNodeKit

/// LocationRoute is where the Location screen can push to.
enum LocationRoute: Hashable {
    case day(Date)
    case place(LocationPlace)
    case people
    case settings
}

/// LocationSegment is what the list under the map shows, chosen in the side
/// menu as Finance's parts and Photos' collections are.
enum LocationSegment: String, CaseIterable, Identifiable {
    case visits
    case places
    case sharing

    var id: String { rawValue }

    var title: String {
        switch self {
        case .visits: String(localized: "Visits")
        case .places: String(localized: "Places")
        case .sharing: String(localized: "Sharing")
        }
    }

    var systemImage: String {
        switch self {
        case .visits: "clock"
        case .places: "mappin"
        case .sharing: "person.2"
        }
    }
}

enum LocationMapRegion {
    /// A region that holds all of the coordinates, with room around them, and
    /// never tighter than a few hundred meters.
    static func fitting(_ coordinates: [CLLocationCoordinate2D]) -> MKCoordinateRegion? {
        guard let first = coordinates.first else { return nil }
        var south = first.latitude
        var north = first.latitude
        var west = first.longitude
        var east = first.longitude
        for coordinate in coordinates {
            south = min(south, coordinate.latitude)
            north = max(north, coordinate.latitude)
            west = min(west, coordinate.longitude)
            east = max(east, coordinate.longitude)
        }
        let latitudeDelta = max((north - south) * 1.4, 0.006)
        // Apple Maps draws its logo and the Legal link along the bottom edge,
        // and a label there would sit on them: the region reaches further
        // south than the coordinates do, so they stay in the upper part.
        let bottomRoom = latitudeDelta * 0.15
        let center = CLLocationCoordinate2D(latitude: (south + north) / 2 - bottomRoom, longitude: (west + east) / 2)
        let span = MKCoordinateSpan(latitudeDelta: latitudeDelta * 1.3, longitudeDelta: max((east - west) * 1.4, 0.006))
        return MKCoordinateRegion(center: center, span: span)
    }
}

/// OpenStreetMapCredit is the footer of every list that shows names from
/// OpenStreetMap.
struct OpenStreetMapCredit: View {
    var body: some View {
        Text("© OpenStreetMap contributors")
    }
}

/// LocationScreen is where the person is and where they have been: a map with
/// their position, the visit in progress under it, and below that their
/// visits by day and the places they were at. Which list shows is the side
/// menu's choice.
struct LocationScreen: View {
    @State private var model: LocationModel
    @Binding var segment: LocationSegment
    @State private var path: [LocationRoute] = []
    @State private var isCheckingIn = false
    @State private var isExplainingAlways = false
    @State private var isAllowingLocation = false
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(AppModel.self) private var app
    private let recorder = LocationRecorder.shared

    init(connection: Connection, segment: Binding<LocationSegment>) {
        _model = State(initialValue: LocationModel(connection: connection))
        _segment = segment
    }

    var body: some View {
        NavigationStack(path: $path) {
            GeometryReader { proxy in
                VStack(spacing: 0) {
                    LocationMapHeader(model: model, recorder: recorder, isShowingPeople: isShowingPeople)
                        // Pictures on a map stay map-sized, as in Maps.
                        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                        .frame(height: proxy.size.height * mapHeightFraction)
                    if isShowingPeople {
                        LocationPeopleRows(model: model)
                    }
                    if isShowingPeople, let person = model.selectedPerson {
                        LocationPersonPanel(model: model, person: person)
                    } else {
                        if model.isSetupShown {
                            LocationSetupList(model: model, recorder: recorder, allowLocation: allowLocation)
                        } else {
                            // The list starts close to what is above it, not
                            // a grouped list's usual top gap away.
                            content.contentMargins(.top, 8, for: .scrollContent)
                        }
                    }
                }
                // The grouped gray of the lists, so the card of people stands
                // out from it as the lists' rows do.
                .background(Color(.systemGroupedBackground))
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .withoutSwipeBack()
            // A list chosen in the side menu is shown at once: over whatever
            // was open on top of it, a person chosen on the map, or the setup
            // (whose rows say Places and Sharing are where those steps live).
            // People are chosen only under Visits, so going there keeps the
            // one a notification just opened.
            .onChange(of: segment) {
                path = []
                if !isShowingPeople { model.clearSelection() }
                if model.isSetupShown { model.dismissSetup() }
            }
            .toolbar {
                MenuButton()
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button {
                        isCheckingIn = true
                    } label: {
                        Label("Check in", systemImage: "mappin.and.ellipse")
                    }
                    NavigationLink(value: LocationRoute.settings) {
                        Label("Location settings", systemImage: "gearshape")
                    }
                }
            }
            .navigationDestination(for: LocationRoute.self) { route in
                switch route {
                case .day(let day): LocationDayScreen(model: model, day: day)
                case .place(let place): LocationPlaceScreen(model: model, place: place)
                case .people: LocationPeopleScreen(model: model)
                case .settings: LocationSettingsView(model: model)
                }
            }
        }
        .sheet(isPresented: $isCheckingIn) { CheckInSheet(model: model) }
        .sheet(isPresented: $isExplainingAlways) { AlwaysExplanationSheet() }
        .toast(done: $model.doneText, problem: $model.problemText)
        .task {
            await model.load()
            // The first time, the setup asks in its own row instead.
            if !model.isSetupShown {
                recorder.askForAuthorizationIfUndecided()
                if recorder.shouldExplainAlways { isExplainingAlways = true }
            }
            #if DEBUG
                openDemoScreen()
            #endif
        }
        // Follows the people sharing while this screen is visible.
        .task { await model.follow() }
        .onAppear { recorder.viewerAppeared() }
        .onDisappear { recorder.viewerDisappeared() }
        .onChange(of: recorder.authorizationStatus, initial: true) {
            if model.hasLoaded, recorder.shouldExplainAlways, !model.isSetupShown || isAllowingLocation { isExplainingAlways = true }
            model.finishSetupIfDone()
        }
        .onChange(of: app.openedLocationUserId, initial: true) { _, userId in
            guard let userId else { return }
            app.openedLocationUserId = nil
            segment = .visits
            model.select(userId: userId)
        }
        .onChange(of: app.openedLocationDay, initial: true) { _, day in
            guard let day else { return }
            app.openedLocationDay = nil
            path = [.day(day)]
        }
    }

    /// The map shares the screen with the people under it.
    private var mapHeightFraction: Double {
        if dynamicTypeSize.isAccessibilitySize { return 0.3 }
        return !isShowingPeople || model.people.isEmpty ? 0.45 : 0.36
    }

    /// Whether the people sharing with the person are shown: their rows under
    /// the map, their pictures on it, and the one chosen. That is where
    /// everyone is, which Visits is about; Places and Sharing are the
    /// person's own.
    private var isShowingPeople: Bool { segment == .visits }

    /// The setup's first row: When In Use, then Always, through the flow the
    /// screen already has; Settings once the person has said no to either.
    private func allowLocation() {
        isAllowingLocation = true
        if recorder.isAuthorizationUndecided {
            recorder.askForAuthorizationIfUndecided()
        } else if recorder.shouldExplainAlways {
            isExplainingAlways = true
        } else {
            LocationSettingsView.openSettings()
        }
    }

    /// The list on screen, as Finance names its part; Location while the
    /// setup or a chosen person's own visits fill the screen instead.
    private var title: String {
        model.isSetupShown || model.selectedPerson != nil ? String(localized: "Location") : segment.title
    }

    @ViewBuilder
    private var content: some View {
        if let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            switch segment {
            case .visits: LocationVisitsList(model: model, recorder: recorder)
            case .places: LocationPlacesList(model: model)
            case .sharing: LocationSharingList(model: model)
            }
        }
    }

    #if DEBUG
        /// The screens a screenshot names: `-TeaNodeScreen location-day`,
        /// `location-places`, `location-place`, `location-settings`,
        /// `location-sharing`, `location-person`, `location-people` (more
        /// people than fit under the map), `location-checkin` (the check-in
        /// sheet), `location-address` (the address search of a place);
        /// `location-setup` is the server having no position yet. MainView
        /// chooses the list (Places for `location-places`, `location-place`
        /// and `location-address`, Sharing for `location-sharing`).
        private func openDemoScreen() {
            guard DemoServer.isRequested, path.isEmpty else { return }
            switch DemoServer.screen {
            case "location-day": path = [.day(Date())]
            case "location-person": model.select(userId: LocationDemo.historyPersonId)
            case "location-people": path = [.people]
            case "location-checkin": isCheckingIn = true
            case "location-place", "location-address":
                if let place = model.places.first(where: { $0.id == LocationDemo.parkPlaceId }) { path = [.place(place)] }
            case "location-settings": path = [.settings]
            default: break
            }
        }
    #endif
}

/// LocationMapHeader is the map at the top: the person's position while the
/// phone is allowed to say it, else the latest point the server has, and the
/// places they named.
struct LocationMapHeader: View {
    let model: LocationModel
    let recorder: LocationRecorder
    /// Whether the pictures of the people sharing are on the map, beside the
    /// person's own.
    var isShowingPeople = true
    @State private var position = MapCameraPosition.automatic
    @Environment(AppModel.self) private var app

    private var isShowingPhone: Bool {
        #if DEBUG
            if DemoServer.isRequested { return false }
        #endif
        return recorder.isAuthorizedToLocate
    }

    /// The person's own places to pin, leaving out each one that somebody on
    /// the map is at now: MapKit draws a pin above every picture, so it would
    /// cover them, and their picture already says they are there.
    private var pinnedPlaces: [LocationPlace] {
        let shownPeople: [PersonLocation] = isShowingPeople ? model.people : []
        let coordinates: [CLLocationCoordinate2D] = ([model.latestCoordinate] + shownPeople.map(\.coordinate)).compactMap { $0 }
        let pictures: [CLLocation] = coordinates.map { CLLocation(latitude: $0.latitude, longitude: $0.longitude) }
        return model.places.filter(\.isOfPersonsOwn).filter { place in
            let center = CLLocation(latitude: place.latitudeDegrees, longitude: place.longitudeDegrees)
            return !pictures.contains { $0.distance(from: center) <= place.radiusMeters }
        }
    }

    var body: some View {
        Map(position: $position) {
            if isShowingPhone {
                UserAnnotation()
            } else if let coordinate = model.latestCoordinate, let own = model.location {
                Annotation("You", coordinate: coordinate) {
                    // The way back from somebody chosen, as the own row is.
                    Button {
                        model.focusOnOwn()
                    } label: {
                        AvatarView(name: own.displayName, avatarUrl: own.avatarUrl, colorIndex: own.avatarColorIndex, size: 36)
                            .overlay(Circle().stroke(model.selectedUserId == nil ? Color.accentColor : Color(.systemBackground), lineWidth: 3))
                            // A touch target of 44 points around the picture.
                            .frame(minWidth: 44, minHeight: 44)
                            .contentShape(Circle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("You")
                    .accessibilityHint("Shows you on the map")
                    // A map draws an annotation's content in a host of its
                    // own, which need not carry the screen's environment;
                    // AvatarView reads the app from it.
                    .environment(app)
                }
            }
            ForEach(pinnedPlaces.prefix(40)) { place in
                Marker(place.shownName, systemImage: place.symbolName, coordinate: place.coordinate)
            }
            ForEach(isShowingPeople ? model.people : []) { person in
                if let coordinate = person.coordinate {
                    Annotation(person.displayName, coordinate: coordinate) {
                        Button {
                            // Chosen again, they stay chosen: the own picture or row is the way back.
                            if person.userId != model.selectedUserId { model.select(userId: person.userId) }
                        } label: {
                            AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 36)
                                .overlay(
                                    Circle().stroke(
                                        person.userId == model.selectedUserId ? Color.accentColor : Color(.systemBackground), lineWidth: 3)
                                )
                                // A touch target of 44 points around the picture.
                                .frame(minWidth: 44, minHeight: 44)
                                .contentShape(Circle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(person.displayName)
                        .environment(app)
                    }
                }
            }
        }
        .mapControls {
            if isShowingPhone { MapUserLocationButton() }
            MapCompass()
        }
        .onChange(of: model.latestCoordinate?.latitude, initial: true) { focus() }
        // Centers on the person chosen, or back on the person's own.
        .onChange(of: model.selectedPerson?.userId) { focus() }
        .onChange(of: model.ownFocusCount) { focus() }
        .onChange(of: recorder.livePosition?.latitudeDegrees) { _, _ in
            // The first reading centers the map once; after that the person
            // moves it as they like.
            if recorder.livePosition != nil, !hasFollowedPhone {
                hasFollowedPhone = true
                focus()
            }
        }
        .accessibilityLabel("Map of where you are")
    }

    @State private var hasFollowedPhone = false

    private func focus() {
        if let coordinate = model.selectedPerson?.coordinate {
            position = .region(MKCoordinateRegion(center: coordinate, latitudinalMeters: 1500, longitudinalMeters: 1500))
        } else if isShowingPhone, let live = recorder.livePosition {
            position = .region(MKCoordinateRegion(center: live.coordinate, latitudinalMeters: 1500, longitudinalMeters: 1500))
        } else if let coordinate = model.latestCoordinate {
            position = .region(MKCoordinateRegion(center: coordinate, latitudinalMeters: 1500, longitudinalMeters: 1500))
        }
    }
}

// MARK: Visits

struct LocationVisitsList: View {
    let model: LocationModel
    let recorder: LocationRecorder
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        List {
            Section {
                PlaceLabelFilterPicker(
                    selection: Binding(
                        get: { model.visitLabelFilter }, set: { label in Task { await model.setVisitLabelFilter(label) } }))
            }
            ForEach(model.days) { day in
                Section {
                    ForEach(day.visits) { visit in
                        NavigationLink(value: LocationRoute.place(visit.place)) {
                            LocationVisitRow(visit: visit)
                        }
                        .onAppear {
                            if visit.id == model.visits.last?.id { Task { await model.loadMoreVisits() } }
                        }
                    }
                } header: {
                    NavigationLink(value: LocationRoute.day(day.startOfDay)) {
                        HStack {
                            Text(verbatim: LocationText.dayTitle(day.startOfDay))
                                .font(.headline)
                                .foregroundStyle(.primary)
                            Spacer()
                            // At the largest text sizes the word broke across two lines; the
                            // symbol alone says it, and the hint below reads it out.
                            if dynamicTypeSize.isAccessibilitySize {
                                Image(systemName: "map")
                                    .font(.subheadline)
                                    .accessibilityLabel("Map")
                            } else {
                                Label("Map", systemImage: "map")
                                    .font(.subheadline)
                            }
                        }
                        .frame(minHeight: 44)
                    }
                    .textCase(nil)
                    .accessibilityHint("Shows the day on the map")
                }
            }
            if model.isLoadingMoreVisits {
                ProgressView().frame(maxWidth: .infinity)
            }
            Section {
            } footer: {
                OpenStreetMapCredit()
            }
        }
        .refreshable { await model.load() }
        .overlay {
            if model.days.isEmpty, model.isFilteringVisits {
                ContentUnavailableView(
                    "No visits to places with this label", systemImage: "mappin.and.ellipse",
                    description: Text("Choose another label, or all places, to see the rest."))
            } else if model.days.isEmpty {
                ContentUnavailableView(
                    "No visits yet", systemImage: "mappin.and.ellipse",
                    description: Text(
                        recorder.isRecordingOn
                            ? LocalizedStringKey("A visit shows up here after you spend a while somewhere.")
                            : LocalizedStringKey("Turn on Record where I go in the settings, and your visits show up here.")))
            }
        }
    }
}

struct LocationVisitRow: View {
    let visit: PlaceVisit

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                Image(systemName: visit.place.symbolName)
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(.secondary)
                    .frame(width: 28)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: visit.place.shownName)
                    Text(verbatim: LocationText.summary(of: visit))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            .accessibilityElement(children: .combine)
            if let photos = visit.photos, !photos.isEmpty {
                VisitPhotoStrip(visit: visit, photos: photos)
                    .padding(.leading, 40)
            }
        }
        // The whole width, so the times are not wrapped to the strip's width under them.
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// VisitPhotoStrip shows the first few photos taken during a visit, and how
/// many more there are. It is one borderless button, so that the row it sits
/// in still opens the place, and it opens Photos at exactly this visit.
struct VisitPhotoStrip: View {
    let visit: PlaceVisit
    let photos: [Photo]
    @Environment(AppModel.self) private var app
    /// A picture, not text, so it keeps its size at the largest text size, and
    /// five of them still fit beside the place's symbol.
    private let tileSide: CGFloat = 44

    private var totalCount: Int { max(visit.photoCount ?? photos.count, photos.count) }
    private var remainingCount: Int { totalCount - photos.count }

    var body: some View {
        Button {
            // A visit still going on has no end yet, so its photos run to now.
            guard let arrival = visit.arrivedDate else { return }
            app.openedPhotoFilter = PhotoFilter(takenAfter: arrival, takenBefore: visit.photosTakenBeforeDate ?? visit.departedDate ?? Date())
        } label: {
            HStack(spacing: 4) {
                ForEach(photos) { photo in
                    VisitPhotoTile(photo: photo, connection: app.connection)
                        .frame(width: tileSide, height: tileSide)
                        .clipShape(.rect(cornerRadius: 3))
                }
                if remainingCount > 0 {
                    Text(verbatim: "+\(remainingCount)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .frame(width: tileSide, height: tileSide)
                        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 3))
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(String(localized: "\(totalCount) photos taken during this visit"))
        .accessibilityHint(String(localized: "Opens them in Photos"))
    }
}

/// VisitPhotoTile is one small picture of a photo, filling the space it is
/// given.
struct VisitPhotoTile: View {
    let photo: Photo
    let connection: Connection?
    @State private var picture: UIImage?

    init(photo: Photo, connection: Connection?) {
        self.photo = photo
        self.connection = connection
        if let connection, let path = photo.gridPreviewPath {
            _picture = State(initialValue: PhotoImages.cachedPicture(path: path, accountKey: connection.accountKey))
        }
    }

    var body: some View {
        Color(.tertiarySystemFill)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipped()
            .task(id: photo.gridPreviewPath) {
                guard let connection, let path = photo.gridPreviewPath else { return }
                picture = await PhotoImages.picture(path: path, connection: connection)
            }
    }
}

// MARK: One day

/// LocationDayScreen is one day: the path walked on the map, the visits as
/// markers, and the visits under it.
struct LocationDayScreen: View {
    let model: LocationModel
    let day: Date
    @State private var points: [LocationPoint] = []
    @State private var visits: [PlaceVisit] = []
    @State private var position = MapCameraPosition.automatic
    @State private var isLoading = true
    @State private var loadFailedText: String?
    @State private var isConfirmingErase = false
    @State private var isErasing = false
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        GeometryReader { proxy in
            VStack(spacing: 0) {
                Map(position: $position) {
                    if points.count > 1 {
                        MapPolyline(coordinates: points.map(\.coordinate)).stroke(.tint, lineWidth: 4)
                    }
                    ForEach(visits) { visit in
                        Marker(visit.place.shownName, systemImage: visit.place.symbolName, coordinate: visit.coordinate)
                    }
                }
                .frame(height: proxy.size.height * (dynamicTypeSize.isAccessibilitySize ? 0.3 : 0.45))
                .accessibilityLabel("Map of the day")
                if let loadFailedText {
                    LoadFailedView(problemText: loadFailedText) { await load() }
                } else {
                    List {
                        Section {
                            ForEach(visits) { visit in
                                NavigationLink(value: LocationRoute.place(visit.place)) {
                                    LocationVisitRow(visit: visit)
                                }
                            }
                        } footer: {
                            OpenStreetMapCredit()
                        }
                        Section {
                            Button(role: .destructive) {
                                isConfirmingErase = true
                            } label: {
                                Label("Erase this day", systemImage: "trash").foregroundStyle(.red)
                            }
                            .tint(.red)
                            .actionColor(isDestructive: true)
                                .disabled(isErasing)
                                .frame(minHeight: 44)
                                .confirmationDialog(
                                    "Erase \(LocationText.dayTitle(day))?", isPresented: $isConfirmingErase, titleVisibility: .visible
                                ) {
                                    Button("Erase this day", role: .destructive) { Task { await erase() } }
                                } message: {
                                    Text(
                                        "Erases your positions and visits of this day, and the places left with no visit that you did not name or label. Places you named or labeled stay. Anything your agent already learned from those days stays in its memory until you remove it. This cannot be undone."
                                    )
                                }
                        }
                    }
                    .overlay {
                        if isLoading {
                            ProgressView()
                        } else if visits.isEmpty {
                            ContentUnavailableView("No visits this day", systemImage: "mappin.and.ellipse")
                        }
                    }
                }
            }
        }
        .navigationTitle(LocationText.dayTitle(day))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func erase() async {
        isErasing = true
        defer { isErasing = false }
        let window = LocationDays.window(of: day)
        if await model.eraseHistory(from: window.from, until: window.until, dayText: LocationText.dayTitle(day)) { await load() }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        let window = LocationDays.window(of: day)
        do {
            async let readVisits = model.connection.locationVisits(from: window.from, until: window.until, limitCount: 200)
            async let readPoints = model.points(on: day)
            let (page, read) = try await (readVisits, readPoints)
            visits = page.placeVisits
            points = read
            loadFailedText = nil
            let coordinates = read.map(\.coordinate) + page.placeVisits.map(\.coordinate)
            if let region = LocationMapRegion.fitting(coordinates) { position = .region(region) }
        } catch {
            loadFailedText = error.loadProblemText
        }
    }
}

// MARK: Places

struct LocationPlacesList: View {
    @Bindable var model: LocationModel

    var body: some View {
        List {
            Section {
                PlaceLabelFilterPicker(
                    selection: Binding(
                        get: { model.placeLabelFilter }, set: { label in Task { await model.setPlaceLabelFilter(label) } }))
            }
            Section {
                ForEach(model.sortedPlaces) { place in
                    NavigationLink(value: LocationRoute.place(place)) {
                        LocationPlaceRow(place: place)
                    }
                    .onAppear {
                        if place.id == model.places.last?.id { Task { await model.loadMorePlaces() } }
                    }
                }
            }
            if model.isLoadingMorePlaces {
                ProgressView().frame(maxWidth: .infinity)
            }
            Section {
            } footer: {
                OpenStreetMapCredit()
            }
        }
        .refreshable { await model.load() }
        .searchable(text: $model.placeSearchText, prompt: Text("Search places"))
        // Read a moment after the last key, not at every key.
        .task(id: model.placeSearchText) {
            guard model.isPlaceSearchPending else { return }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            await model.reloadPlaces()
        }
        .overlay {
            if model.places.isEmpty, model.isFilteringPlaces {
                ContentUnavailableView.search
            } else if model.places.isEmpty {
                ContentUnavailableView(
                    "No places yet", systemImage: "mappin.slash",
                    description: Text("The places you spend time at show up here, and you can name them."))
            }
        }
    }
}

/// PlaceLabelFilterPicker narrows a list to the places with one label, or
/// shows them all.
struct PlaceLabelFilterPicker: View {
    @Binding var selection: PlaceLabel?

    var body: some View {
        Picker("Label", selection: $selection) {
            Text("All labels").tag(PlaceLabel?.none)
            ForEach(PlaceLabel.allCases, id: \.rawValue) { label in
                Label(label.title, systemImage: label.symbolName).tag(Optional(label))
            }
        }
        .pickerStyle(.menu)
    }
}

struct LocationPlaceRow: View {
    let place: LocationPlace

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: place.symbolName)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(width: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: place.shownName)
                if let secondaryName = place.secondaryName {
                    Text(verbatim: secondaryName)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                Text(verbatim: LocationText.visits(of: place))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
