import MapKit
import SwiftUI
import TeaNodeKit

/// LocationRoute is where the Location screen can push to.
enum LocationRoute: Hashable {
    case day(Date)
    case place(LocationPlace)
    case people
    case settings
    case travel
}

/// LocationSegment is what the drawer over the map shows, chosen at the top
/// of the drawer.
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

/// LocationScreen is where the person is and where they have been: a map of
/// the whole screen with their position and the people sharing with them, and
/// over it a drawer of their visits by day, their places and their sharing,
/// chosen at the top of the drawer. The drawer opens at half the screen and is
/// dragged up to all of it or down to its header.
struct LocationScreen: View {
    @State private var model: LocationModel
    @Binding var segment: LocationSegment
    @State private var path: [LocationRoute] = []
    @State private var isCheckingIn = false
    @State private var isExplainingAlways = false
    @State private var isAllowingLocation = false
    @State private var drawerDetent = LocationDrawerDetent.half
    @State private var drawerHeaderHeight: CGFloat = 64
    @Environment(AppModel.self) private var app
    private let recorder = LocationRecorder.shared

    init(connection: Connection, segment: Binding<LocationSegment>) {
        _model = State(initialValue: LocationModel(connection: connection))
        _segment = segment
    }

    var body: some View {
        NavigationStack(path: $path) {
            GeometryReader { proxy in
                // The map keeps its middle above the drawer up to half the
                // screen; over that the map is covered, and a smaller part
                // left showing would only move it.
                let mapCoveredHeight = min(
                    drawerDetent.height(availableHeight: proxy.size.height, headerHeight: drawerHeaderHeight),
                    LocationDrawerDetent.half.height(availableHeight: proxy.size.height, headerHeight: drawerHeaderHeight))
                // The drawer sits on the bottom of the screen and the map
                // behind it; neither one's size moves the other.
                Color.clear
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .overlay(alignment: .bottom) {
                        LocationDrawer(detent: $drawerDetent, headerHeight: $drawerHeaderHeight, availableHeight: proxy.size.height) {
                            drawerHeader
                        } content: {
                            drawerContent
                        }
                    }
                    .background {
                        LocationMapHeader(model: model, recorder: recorder, isShowingPeople: isShowingPeople)
                            // Pictures on a map stay map-sized, as in Maps.
                            .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                            // The map fills the screen, under the bars and the
                            // drawer, and keeps what it centers on, its
                            // controls and its logo in the part left showing.
                            .safeAreaPadding(.top, proxy.safeAreaInsets.top)
                            .safeAreaPadding(.bottom, proxy.safeAreaInsets.bottom + mapCoveredHeight)
                            .ignoresSafeArea()
                    }
            }
            // The name a pushed screen's back button carries; over the map,
            // as in Find My, the drawer says what is shown.
            .navigationTitle("Location")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(removing: .title)
            // A list chosen at the top of the drawer is shown at once, and
            // the drawer comes up to show it: over whatever was open on top
            // of it, a person chosen on the map, or the setup (whose rows say
            // Places and Sharing are where those steps live). People are
            // chosen only under Visits, so going there keeps the one a
            // notification just opened.
            .onChange(of: segment) {
                path = []
                if !isShowingPeople { model.clearSelection() }
                if model.isSetupShown { model.dismissSetup() }
                if drawerDetent == .collapsed { drawerDetent = .half }
            }
            .toolbar {
                MoreBackButton()
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
                case .travel: LocationTravelScreen(connection: model.connection)
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
        // The words the Search tab's Show all in Places carries; the list is Places already.
        .onChange(of: app.openedPartSearch, initial: true) { _, search in
            guard let search, search.area == .places else { return }
            app.openedPartSearch = nil
            segment = .places
            model.placeSearchText = search.words
        }
        .onChange(of: app.openedLocationDay, initial: true) { _, day in
            guard let day else { return }
            app.openedLocationDay = nil
            path = [.day(day)]
        }
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

    /// The setup's Motion & Fitness row: iOS asks the first time; once the
    /// person has answered, only Settings can change it.
    private func allowMotion() {
        if recorder.isMotionUndecided {
            recorder.askForMotionAuthorization()
        } else {
            LocationSettingsView.openSettings()
        }
    }

    /// Opens the screen of everyone sharing, from the row that says how many.
    private func showPeople() {
        path.append(.people)
    }

    /// The top of the drawer: the lists to choose between, or, while a person
    /// sharing is chosen, who they are and the way back to everyone.
    @ViewBuilder
    private var drawerHeader: some View {
        if isShowingPeople, let person = model.selectedPerson {
            LocationChosenPersonHeader(
                person: person,
                close: { model.focusOnOwn() },
                mute: { choice in Task { await model.muteNotifications(of: person, for: choice) } },
                unmute: { Task { await model.unmuteNotifications(of: person) } })
        } else {
            VStack(spacing: 0) {
                // The person's own pause, where it is seen with the lists.
                if let pause = recorder.activePause {
                    LocationPausedNote(pause: pause)
                }
                Picker("Location", selection: $segment) {
                    ForEach(LocationSegment.allCases) { choice in
                        Text(choice.title).tag(choice)
                    }
                }
                .pickerStyle(.segmented)
                .padding(.horizontal)
                .padding(.bottom, 10)
            }
        }
    }

    /// What the drawer holds under its header.
    @ViewBuilder
    private var drawerContent: some View {
        if isShowingPeople, let person = model.selectedPerson, let personVisits = model.chosenPersonVisits {
            // A new list for each person chosen, so one person's day and
            // visits never show under another's name.
            LocationChosenPersonVisits(model: personVisits, person: person)
                .id(person.userId)
                .contentMargins(.top, 0, for: .scrollContent)
        } else if model.isSetupShown {
            LocationSetupList(
                model: model, recorder: recorder, isShowingPeople: isShowingPeople, allowLocation: allowLocation,
                allowMotion: allowMotion, showPeople: showPeople)
                .contentMargins(.top, 0, for: .scrollContent)
        } else {
            content.contentMargins(.top, 0, for: .scrollContent)
        }
    }

    @ViewBuilder
    private var content: some View {
        if let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            switch segment {
            case .visits: LocationVisitsList(model: model, recorder: recorder, showPeople: showPeople)
            case .places: LocationPlacesList(model: model)
            case .sharing: LocationSharingList(model: model)
            }
        }
    }

    #if DEBUG
        /// The screens a screenshot names: `-TeaNodeScreen location-day`,
        /// `location-places`, `location-place`, `location-settings`,
        /// `location-sharing`, `location-paused` (the person and somebody sharing have paused),
        /// `location-settings-paused`, `location-travel` (the Travel screen at
        /// Week), `location-person`, `location-person-muted` (the same with
        /// the person's notices muted until later today), `location-person-card` (the
        /// same with the person's card over the map, which the app shell
        /// brings up), `location-people` (more
        /// people than fit under the map), `location-checkin` (the check-in
        /// sheet), `location-address` (the address search of a place);
        /// `location-setup` is the server having no position yet;
        /// `location-full` and `location-collapsed` are the drawer at full
        /// height and collapsed to its header. MainView
        /// chooses the list (Places for `location-places`, `location-place`
        /// and `location-address`, Sharing for `location-sharing`).
        private func openDemoScreen() {
            guard DemoServer.isRequested, path.isEmpty else { return }
            switch DemoServer.screen {
            case "location-day": path = [.day(Date())]
            case "location-person", "location-person-card", "location-person-muted": model.select(userId: LocationDemo.historyPersonId)
            case "location-people": path = [.people]
            case "location-checkin": isCheckingIn = true
            case "location-place", "location-address":
                if let place = model.places.first(where: { $0.id == LocationDemo.parkPlaceId }) { path = [.place(place)] }
            case "location-settings", "location-settings-paused": path = [.settings]
            case "location-travel": path = [.travel]
            case "location-full", "location-paused": drawerDetent = .full
            case "location-collapsed": drawerDetent = .collapsed
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
    /// cover them, and their picture already says they are there. None while
    /// somebody's day is drawn, whose places are the ones that matter.
    private var pinnedPlaces: [LocationPlace] {
        guard chosenDay == nil else { return [] }
        return model.places.filter(\.isOfPersonsOwn).filter { place in
            !isUnderPicture(latitudeDegrees: place.latitudeDegrees, longitudeDegrees: place.longitudeDegrees, radiusMeters: place.radiusMeters)
        }
    }

    /// Whether a place has somebody's picture on it now.
    private func isUnderPicture(latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double) -> Bool {
        let shownPeople: [PersonLocation] = isShowingPeople ? model.people : []
        let coordinates: [CLLocationCoordinate2D] = ([model.latestCoordinate] + shownPeople.map(\.coordinate)).compactMap { $0 }
        let center = CLLocation(latitude: latitudeDegrees, longitude: longitudeDegrees)
        return coordinates.contains { CLLocation(latitude: $0.latitude, longitude: $0.longitude).distance(from: center) <= radiusMeters }
    }

    /// The day of the person chosen, while they share where they have been:
    /// the map draws its path and the places they went.
    private var chosenDay: PersonVisitsModel? {
        guard isShowingPeople, model.selectedPerson?.level == .history else { return nil }
        return model.chosenPersonVisits
    }

    /// The places the person chosen went that day, each once, leaving out the
    /// one their picture is on.
    private var chosenDayPlaces: [PlaceVisit] {
        var seenPlaceIds = Set<String>()
        return (chosenDay?.visits ?? []).filter { visit in
            seenPlaceIds.insert(visit.place.id).inserted
                && !isUnderPicture(latitudeDegrees: visit.latitudeDegrees, longitudeDegrees: visit.longitudeDegrees, radiusMeters: visit.place.radiusMeters)
        }
    }

    /// The region of the chosen person's day: its path and visits, and where
    /// they are now when the day is today.
    private var chosenDayRegion: MKCoordinateRegion? {
        guard let chosenDay, chosenDay.readDay != nil else { return nil }
        let now: CLLocationCoordinate2D? = chosenDay.isShowingToday ? model.selectedPerson?.coordinate : nil
        return LocationMapRegion.fitting(chosenDay.points.map(\.coordinate) + chosenDay.visits.map(\.coordinate) + [now].compactMap { $0 })
    }

    var body: some View {
        Map(position: $position) {
            if isShowingPhone {
                UserAnnotation()
            }
            ForEach(pinnedPlaces.prefix(40)) { place in
                Marker(place.shownName, systemImage: place.symbolName, coordinate: place.coordinate)
            }
            if let chosenDay, chosenDay.path.count > 1 {
                MapPolyline(coordinates: chosenDay.path.map(\.coordinate)).stroke(.tint, lineWidth: 4)
            }
            ForEach(chosenDayPlaces) { visit in
                Marker(visit.place.shownName, systemImage: visit.place.symbolName, coordinate: visit.coordinate)
            }
            ForEach(pinGroups) { group in
                Annotation(group.title, coordinate: group.coordinate) {
                    LocationMapPinGroupView(group: group, chosenId: chosenPinId, choose: choose)
                        // A map draws an annotation's content in a host of its
                        // own, which need not carry the screen's environment;
                        // AvatarView reads the app from it.
                        .environment(app)
                }
            }
        }
        // What the map shows and how wide it is say which pictures would
        // cover each other.
        .onMapCameraChange(frequency: .onEnd) { context in visibleRect = context.rect }
        .onGeometryChange(for: CGFloat.self) { proxy in proxy.size.width } action: { width in mapWidth = width }
        .mapControls {
            if isShowingPhone { MapUserLocationButton() }
            MapCompass()
        }
        .onChange(of: model.latestCoordinate?.latitude, initial: true) { focus() }
        // Centers on the person chosen, or back on the person's own.
        .onChange(of: model.selectedPerson?.userId) { focus() }
        // Fits the chosen person's day once it is read, and each day chosen.
        .onChange(of: chosenDay?.readDay) { focus() }
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
    @State private var visibleRect: MKMapRect?
    @State private var mapWidth: CGFloat = 0

    /// The pictures on the map: the person's own, unless the phone's own dot
    /// stands for them, and everyone sharing while they are shown.
    private var pins: [LocationMapPin] {
        var pins: [LocationMapPin] = []
        if !isShowingPhone, let coordinate = model.latestCoordinate, let own = model.location {
            pins.append(LocationMapPin(person: own, coordinate: coordinate, isOwn: true))
        }
        if isShowingPeople {
            pins += model.people.compactMap { person in
                person.coordinate.map { coordinate in LocationMapPin(person: person, coordinate: coordinate, isOwn: false) }
            }
        }
        return pins
    }

    private var chosenPinId: String { model.selectedUserId ?? "" }

    private var pinGroups: [LocationMapPinGroup] {
        LocationMapPinGroup.grouping(pins, chosenId: chosenPinId, visibleRect: visibleRect, mapWidth: mapWidth)
    }

    /// Chooses whose picture was tapped. The own picture is the way back from
    /// somebody chosen, as the own row is; somebody chosen again stays chosen.
    private func choose(_ pin: LocationMapPin) {
        if pin.isOwn {
            model.focusOnOwn()
        } else if pin.person.userId != model.selectedUserId {
            model.select(userId: pin.person.userId)
        }
    }

    private func focus() {
        if let chosenDayRegion {
            position = .region(chosenDayRegion)
        } else if let coordinate = model.selectedPerson?.coordinate {
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
    let showPeople: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        List {
            LocationPeopleSection(model: model, showPeople: showPeople)
            Section {
                NavigationLink(value: LocationRoute.travel) {
                    Label("Travel", systemImage: "figure.walk")
                        .foregroundStyle(.primary)
                        .frame(minHeight: 44)
                }
            }
            if model.days.isEmpty {
                // Under the people, not over them as an overlay would be.
                Section {
                    ContentUnavailableView(
                        "No visits yet", systemImage: "mappin.and.ellipse",
                        description: Text(
                            recorder.isRecordingOn
                                ? LocalizedStringKey("A visit shows up here after you spend a while somewhere.")
                                : LocalizedStringKey("Turn on Record where I go in the settings, and your visits show up here.")))
                }
                .listRowBackground(Color.clear)
            }
            ForEach(model.days) { day in
                Section {
                    ForEach(day.entries) { entry in
                        LocationTimelineRow(entry: entry, correctLeg: { leg, travelMode in Task { await model.correct(leg, to: travelMode) } })
                            .onAppear {
                                if case .visit(let visit) = entry, visit.id == model.visits.last?.id {
                                    Task { await model.loadMoreVisits() }
                                }
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
    }
}

struct LocationVisitRow: View {
    let visit: PlaceVisit
    /// The symbol's column grows with the text, so a wide symbol at a large
    /// text size does not run into the name.
    @ScaledMetric(relativeTo: .body) private var symbolColumnWidth: CGFloat = 28

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                Image(systemName: visit.place.symbolName)
                    .symbolRenderingMode(.hierarchical)
                    .foregroundStyle(.secondary)
                    .frame(width: symbolColumnWidth)
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
                // The photos keep their size at every text size, so their
                // indent does too: five still fit across.
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
    @State private var pauses: [LocationPause] = []
    @State private var trips: [Trip] = []
    @State private var position = MapCameraPosition.automatic
    @State private var isLoading = true
    @State private var loadFailedText: String?
    @State private var isConfirmingErase = false
    @State private var isErasing = false
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The day's positions joined through the places visited.
    private var path: [LocationPoint] {
        let window = LocationDays.window(of: day)
        return LocationDays.path(points: points, visits: visits, from: window.from, until: window.until)
    }

    var body: some View {
        GeometryReader { proxy in
            VStack(spacing: 0) {
                Map(position: $position) {
                    if path.count > 1 {
                        MapPolyline(coordinates: path.map(\.coordinate)).stroke(.tint, lineWidth: 4)
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
                            ForEach(LocationDays.entries(visits: visits, pauses: pauses, trips: trips)) { entry in
                                LocationTimelineRow(entry: entry, correctLeg: { leg, travelMode in Task { await correct(leg, to: travelMode) } })
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
                        } else if visits.isEmpty && pauses.isEmpty && trips.isEmpty {
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

    /// Corrects a leg of the day's trips, then reads the day's trips again.
    private func correct(_ leg: TripLeg, to travelMode: TravelMode?) async {
        guard await model.correct(leg, to: travelMode) else { return }
        await loadTrips()
    }

    private func loadTrips() async {
        let window = LocationDays.window(of: day)
        if let read = await fetchTrips(connection: model.connection, from: window.from, until: window.until) { trips = read }
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
            async let readPauses = model.pauses(from: window.from, until: window.until)
            async let readTrips = fetchTrips(connection: model.connection, from: window.from, until: window.until)
            let (page, read) = try await (readVisits, readPoints)
            visits = page.placeVisits
            trips = await readTrips ?? []
            pauses = await readPauses
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
                ForEach(model.sortedPlaces) { place in
                    NavigationLink(value: LocationRoute.place(place)) {
                        LocationPlaceRow(place: place)
                    }
                    .onAppear {
                        if place.id == model.places.last?.id { Task { await model.loadMorePlaces() } }
                    }
                }
            } header: {
                // A small menu at the end of the list's heading, as a day of
                // visits has its Map, rather than a row of its own under the map.
                HStack {
                    Spacer()
                    PlaceLabelFilterPicker(
                        selection: Binding(
                            get: { model.placeLabelFilter }, set: { label in Task { await model.setPlaceLabelFilter(label) } }))
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
                // Taps go through to the label menu in the heading, the way out.
                ContentUnavailableView.search
                    .allowsHitTesting(false)
            } else if model.places.isEmpty {
                ContentUnavailableView(
                    "No places yet", systemImage: "mappin.slash",
                    description: Text("The places you spend time at show up here, and you can name them."))
            }
        }
    }
}

/// PlaceLabelFilterPicker narrows the places to those with one label, or
/// shows them all. It sits in the places list's heading.
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
        .labelsHidden()
        .font(.subheadline)
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
