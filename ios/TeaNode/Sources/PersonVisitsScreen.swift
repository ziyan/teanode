import SwiftUI
import TeaNodeKit

/// PersonVisitsModel is what somebody sharing where they have been shows: their
/// visits for one day, with a way back and forth by the day, and their places.
/// Only at "Where I am and where I have been" does the server give them.
@MainActor
@Observable
final class PersonVisitsModel {
    let connection: Connection
    let userId: String
    /// The day of the visits shown.
    private(set) var day = Calendar.current.startOfDay(for: Date())
    private(set) var visits: [PlaceVisit] = []
    /// The pauses that overlap the day, which show among the visits.
    private(set) var pauses: [LocationPause] = []
    private(set) var places: [LocationPlace] = []
    private(set) var isLoading = false
    private(set) var problemText: String?

    init(connection: Connection, userId: String) {
        self.connection = connection
        self.userId = userId
    }

    /// The day's visits and pauses together, newest first.
    var entries: [LocationTimelineEntry] { LocationDays.entries(visits: visits, pauses: pauses) }

    /// Whether the day can move later: not past today.
    var canMoveLater: Bool { day < Calendar.current.startOfDay(for: Date()) }

    func move(by days: Int) {
        let today = Calendar.current.startOfDay(for: Date())
        guard let moved = Calendar.current.date(byAdding: .day, value: days, to: day) else { return }
        day = min(Calendar.current.startOfDay(for: moved), today)
        Task { await load() }
    }

    /// Reads their visits for the day shown and their places.
    func load() async {
        let requestedDay = day
        let window = LocationDays.window(of: requestedDay)
        isLoading = true
        defer { isLoading = false }
        do {
            let visitPage = try await connection.locationVisits(userId: userId, from: window.from, until: window.until, limitCount: 200)
            let placePage = try await connection.locationPlaces(userId: userId, limitCount: LocationModel.placePageSize)
            // A server that cannot list pauses has none to show.
            let pauseList = (try? await connection.locationPauses(userId: userId, from: window.from, until: window.until)) ?? []
            // Another day was chosen while this was read.
            guard day == requestedDay else { return }
            visits = visitPage.placeVisits
            pauses = pauseList
            places = placePage.places
            problemText = nil
        } catch {
            guard !Task.isCancelled, day == requestedDay else { return }
            problemText = error.loadProblemText
        }
    }
}

/// PersonVisitsScreen is the page the card's Location section pushes for
/// somebody who shares where they have been: their visits for a day, with a way
/// back a day, and their places, to read and not to change.
struct PersonVisitsScreen: View {
    @State private var model: PersonVisitsModel

    init(connection: Connection, userId: String) {
        _model = State(initialValue: PersonVisitsModel(connection: connection, userId: userId))
    }

    var body: some View {
        PersonVisitsList(model: model)
            .navigationTitle("Visits and places")
            .navigationBarTitleDisplayMode(.inline)
    }
}

/// LocationChosenPersonVisits is what is under the map on Location once
/// somebody sharing is chosen there: their visits and places, as the card's
/// page shows them, or a line saying they share only where they are now.
struct LocationChosenPersonVisits: View {
    let person: PersonLocation
    @State private var model: PersonVisitsModel

    init(connection: Connection, person: PersonLocation) {
        self.person = person
        _model = State(initialValue: PersonVisitsModel(connection: connection, userId: person.userId))
    }

    var body: some View {
        if person.level == .history {
            PersonVisitsList(model: model)
        } else {
            List {
                Section {
                    Text("\(person.displayName) shares where they are now. Their visits and places are not shared with you.")
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

/// PersonVisitsList is somebody's visits for a day, with a way back and forth
/// by the day, and their places: the list of PersonVisitsScreen and of
/// LocationChosenPersonVisits.
struct PersonVisitsList: View {
    let model: PersonVisitsModel

    var body: some View {
        List {
            Section {
                Button("Previous day", systemImage: "chevron.left") { model.move(by: -1) }
                    .actionColor()
                if model.canMoveLater {
                    Button("Next day", systemImage: "chevron.right") { model.move(by: 1) }
                        .actionColor()
                }
                if let problemText = model.problemText {
                    Text(verbatim: problemText).foregroundStyle(.secondary)
                } else if model.entries.isEmpty {
                    if model.isLoading {
                        ProgressView().frame(maxWidth: .infinity)
                    } else {
                        Text("No visits this day").foregroundStyle(.secondary)
                    }
                }
                ForEach(model.entries) { entry in
                    switch entry {
                    case .visit(let visit): LocationVisitRow(visit: visit)
                    case .pause(let pause): LocationPauseRow(pause: pause)
                    }
                }
            } header: {
                Text(verbatim: LocationText.dayTitle(model.day)).font(.headline).textCase(nil)
            }
            if !model.places.isEmpty {
                Section("Places") {
                    ForEach(model.places) { place in
                        LocationPlaceRow(place: place)
                    }
                }
            }
            Section {
            } footer: {
                OpenStreetMapCredit()
            }
        }
        .refreshable { await model.load() }
        .task { await model.load() }
    }
}
