import SwiftUI
import TeaNodeKit

// How the person got from one place to the next: the trips between visits in a
// day's list, each leg with its way of travel (which the person can correct on
// their own trips), and the Travel screen that adds the distance up by day,
// week, month and year.

/// The trips that began in a range, newest first, read a page at a time.
/// Nil when they could not be read (a server that has no trips, or no
/// connection): a list then shows no trips and says nothing, since the visits
/// it lists are the point.
func fetchTrips(connection: Connection, userId: String? = nil, from: Date, until: Date) async -> [Trip]? {
    let pageSize = 100
    let pageCountMost = 10
    var trips: [Trip] = []
    var offset = 0
    for _ in 0..<pageCountMost {
        guard let page = try? await connection.trips(userId: userId, from: from, until: until, offset: offset, limitCount: pageSize) else {
            return nil
        }
        trips += page.trips
        guard let nextOffset = page.nextOffset else { break }
        offset = nextOffset
    }
    return trips
}

extension TravelMode {
    /// The way's word, written under keys of its own: "Car" and "Train" are
    /// words of the app with other meanings in other places.
    var title: String {
        switch self {
        case .walk: String(localized: "travel-mode-walk", defaultValue: "Walk")
        case .run: String(localized: "travel-mode-run", defaultValue: "Run")
        case .bike: String(localized: "travel-mode-bike", defaultValue: "Bike")
        case .car: String(localized: "travel-mode-car", defaultValue: "Car")
        case .bus: String(localized: "travel-mode-bus", defaultValue: "Bus")
        case .train: String(localized: "travel-mode-train", defaultValue: "Train")
        case .boat: String(localized: "travel-mode-boat", defaultValue: "Boat")
        case .plane: String(localized: "travel-mode-plane", defaultValue: "Plane")
        }
    }
}

extension LocationText {
    /// The word for a way the server named; a way this version does not know
    /// is spelled as it came.
    static func travelModeTitle(_ travelMode: String) -> String {
        guard let mode = TravelMode(rawValue: travelMode) else { return travelMode.prefix(1).uppercased() + travelMode.dropFirst() }
        return mode.title
    }

    /// How far and how long a leg was, such as "11.8 km · 18 min".
    static func summary(of leg: TripLeg) -> String {
        String(localized: "\(distance(leg.distanceMeters)) · \(duration(.seconds(leg.durationSeconds)))")
    }

    /// What a period of the Travel screen is called: the day, "Week of Oct
    /// 19", the month with its year, the year.
    static func title(of period: TravelPeriod, kind: TravelPeriodKind) -> String {
        guard let startDate = period.startDate() else { return period.periodStartDay }
        switch kind {
        case .day: return dayTitle(startDate)
        case .week: return String(localized: "Week of \(startDate.formatted(.dateTime.month(.abbreviated).day()))")
        case .month: return startDate.formatted(.dateTime.month(.wide).year())
        case .year: return startDate.formatted(.dateTime.year())
        }
    }

    /// The word for each period length on the Travel screen's choice.
    static func title(of kind: TravelPeriodKind) -> String {
        switch kind {
        case .day: String(localized: "travel-period-day", defaultValue: "Day")
        case .week: String(localized: "travel-period-week", defaultValue: "Week")
        case .month: String(localized: "travel-period-month", defaultValue: "Month")
        case .year: String(localized: "travel-period-year", defaultValue: "Year")
        }
    }
}

// MARK: In a day's list

/// LocationTripRow is the travel between two visits, among the visits of a
/// day: each leg with its symbol, the way, how far and how long. On the
/// person's own trips a leg is a menu of the ways, to correct what was
/// detected; `correctLeg` is nil on somebody else's.
struct LocationTripRow: View {
    let trip: Trip
    /// Corrects a leg to a way, or with nil takes the correction back.
    let correctLeg: ((TripLeg, TravelMode?) -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(trip.legs) { leg in
                if let correctLeg {
                    LocationTripLegMenu(leg: leg, correctLeg: correctLeg)
                } else {
                    LocationTripLegLine(leg: leg, isMenu: false)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// LocationTripLegLine is one leg: its way's symbol, its word, and how far and
/// how long. `isMenu` adds the arrows that say it opens a menu.
struct LocationTripLegLine: View {
    let leg: TripLeg
    let isMenu: Bool
    /// The symbol's column grows with the text, so a wide symbol at a large
    /// text size does not run into the name.
    @ScaledMetric(relativeTo: .body) private var symbolColumnWidth: CGFloat = 28

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: TravelMode.symbolName(forTravelMode: leg.travelMode))
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(width: symbolColumnWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: LocationText.travelModeTitle(leg.travelMode))
                    .foregroundStyle(.primary)
                Text(verbatim: LocationText.summary(of: leg))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if isMenu {
                Spacer(minLength: 0)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.footnote)
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }
}

/// LocationTripLegMenu is a leg of the person's own trip as a menu: the eight
/// ways with a check on the one that counts, and "As detected" to take the
/// correction back.
struct LocationTripLegMenu: View {
    let leg: TripLeg
    let correctLeg: (TripLeg, TravelMode?) -> Void

    var body: some View {
        Menu {
            Picker(
                String(localized: "travel-way-of-travel", defaultValue: "Way of travel"),
                selection: Binding<TravelMode?>(
                    get: { leg.mode },
                    set: { mode in
                        if let mode { correctLeg(leg, mode) }
                    })
            ) {
                ForEach(TravelMode.allCases) { mode in
                    Label(mode.title, systemImage: mode.symbolName).tag(Optional(mode))
                }
            }
            .pickerStyle(.inline)
            Button(String(localized: "travel-as-detected", defaultValue: "As detected"), systemImage: "arrow.uturn.backward") {
                correctLeg(leg, nil)
            }
            .disabled(!leg.isTravelModeCorrected)
        } label: {
            LocationTripLegLine(leg: leg, isMenu: true)
        }
        .buttonStyle(.plain)
        .accessibilityHint("Changes the way of travel")
    }
}

// MARK: Travel

/// LocationTravelScreen is how far the person traveled by each way: a choice of
/// Day, Week, Month or Year, the periods newest first with the distance for
/// each way and the total, and a way back and forth by page of periods. It
/// shows a person sharing their history as well as the person's own.
struct LocationTravelScreen: View {
    let connection: Connection
    /// Whose travel: somebody sharing their history, or nil for the person's own.
    var userId: String?
    @State private var periodKind = TravelPeriodKind.week
    /// How many pages back from the one with the current period.
    @State private var pageBack = 0
    @State private var periods: [TravelPeriod] = []
    @State private var isLoading = true
    @State private var loadFailedText: String?

    private struct Page: Equatable {
        let periodKind: TravelPeriodKind
        let pageBack: Int
    }

    var body: some View {
        Group {
            if let loadFailedText {
                LoadFailedView(problemText: loadFailedText) { await load() }
            } else {
                List {
                    Section {
                        Picker("Period", selection: $periodKind) {
                            ForEach(TravelPeriodKind.allCases) { kind in
                                Text(verbatim: LocationText.title(of: kind)).tag(kind)
                            }
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()
                    }
                    Section {
                        Button(String(localized: "travel-earlier", defaultValue: "Earlier"), systemImage: "chevron.left") {
                            pageBack += 1
                        }
                        .actionColor()
                        if pageBack > 0 {
                            Button(String(localized: "travel-later", defaultValue: "Later"), systemImage: "chevron.right") {
                                pageBack -= 1
                            }
                            .actionColor()
                        }
                    }
                    Section {
                        ForEach(periods) { period in
                            LocationTravelPeriodRow(period: period, periodKind: periodKind)
                        }
                    }
                }
                .overlay {
                    if isLoading && periods.isEmpty {
                        ProgressView()
                    } else if periods.isEmpty {
                        ContentUnavailableView(
                            "No travel here", systemImage: "figure.walk",
                            description: Text("Trips between visits show up here once they are recorded."))
                    }
                }
            }
        }
        .navigationTitle("Travel")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: Page(periodKind: periodKind, pageBack: pageBack)) { await load() }
        .onChange(of: periodKind) { pageBack = 0 }
    }

    private func load() async {
        let requested = Page(periodKind: periodKind, pageBack: pageBack)
        let range = requested.periodKind.pageRange(pageBack: requested.pageBack)
        isLoading = true
        defer { isLoading = false }
        do {
            let totals = try await connection.travelTotals(userId: userId, period: requested.periodKind, from: range.from, until: range.until)
            // Another page was chosen while this was read.
            guard requested == Page(periodKind: periodKind, pageBack: pageBack) else { return }
            periods = TravelPeriods.group(totals)
            loadFailedText = nil
        } catch {
            guard !Task.isCancelled, requested == Page(periodKind: periodKind, pageBack: pageBack) else { return }
            loadFailedText = error.loadProblemText
        }
    }
}

/// LocationTravelPeriodRow is one period of the Travel screen: its name and the
/// distance in all, then each way with its distance.
struct LocationTravelPeriodRow: View {
    let period: TravelPeriod
    let periodKind: TravelPeriodKind
    /// The symbol's column grows with the text, so a wide symbol at a large
    /// text size does not run into the name.
    @ScaledMetric(relativeTo: .body) private var symbolColumnWidth: CGFloat = 28

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: LocationText.title(of: period, kind: periodKind))
                    .font(.headline)
                Spacer(minLength: 8)
                Text(verbatim: LocationText.distance(period.distanceMeters))
                    .font(.headline)
            }
            ForEach(period.totals, id: \.travelMode) { total in
                HStack(spacing: 12) {
                    Image(systemName: TravelMode.symbolName(forTravelMode: total.travelMode))
                        .symbolRenderingMode(.hierarchical)
                        .foregroundStyle(.secondary)
                        .frame(width: symbolColumnWidth)
                        .accessibilityHidden(true)
                    Text(verbatim: LocationText.travelModeTitle(total.travelMode))
                    Spacer(minLength: 8)
                    Text(verbatim: LocationText.distance(total.distanceMeters))
                        .foregroundStyle(.secondary)
                }
                .font(.subheadline)
                .accessibilityElement(children: .combine)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 2)
    }
}
