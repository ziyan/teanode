import SwiftUI
import TeaNodeKit

// Where a pause shows on the Location screen: a line over the drawer for the
// person's own, and a row among the visits for each one made.

/// LocationPausedNote is the line at the top of the drawer while the person's
/// recording is paused: until when, or until they resume. The Pause section of
/// the settings is where to end it.
struct LocationPausedNote: View {
    let pause: LocationPause

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "pause.circle")
                .symbolRenderingMode(.hierarchical)
                .accessibilityHidden(true)
            Text(verbatim: LocationText.pausedUntil(pause))
        }
        .font(.subheadline)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity)
        .padding(.horizontal)
        .padding(.bottom, 8)
        .accessibilityElement(children: .combine)
        // Past the first accessibility size the text stops growing, as in the
        // drawer's other header, so the lists keep room.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }
}

/// LocationPauseRow is a pause among the visits of a day: a gap, never a
/// stay. It opens nothing.
struct LocationPauseRow: View {
    let pause: LocationPause
    /// The symbol's column grows with the text, so a wide symbol at a large
    /// text size does not run into the name.
    @ScaledMetric(relativeTo: .body) private var symbolColumnWidth: CGFloat = 28

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "pause.circle")
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(width: symbolColumnWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text("Location paused")
                Text(verbatim: LocationText.times(of: pause))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// LocationTimelineRow is a row of a timeline: a visit, which opens its place,
/// a pause, or a trip. `correctLeg` is given on the person's own timeline,
/// where a trip's legs can be corrected. `askToForgetVisit` is given there
/// too: it sets the visit the screen's list asks about forgetting
/// (`forgetVisitDialog`), since a row is rebuilt as the list reloads.
struct LocationTimelineRow: View {
    let entry: LocationTimelineEntry
    var correctLeg: ((TripLeg, TravelMode?) -> Void)?
    var askToForgetVisit: ((PlaceVisit) -> Void)?

    var body: some View {
        switch entry {
        case .visit(let visit):
            NavigationLink(value: LocationRoute.place(visit.place)) {
                LocationVisitRow(visit: visit)
            }
            .swipeActions {
                // A visit in progress is ended by a check-in before it can be
                // forgotten: the phone would report its end as a visit again.
                if let askToForgetVisit, visit.departedAt != nil {
                    Button("Forget", systemImage: "trash", role: .destructive) { askToForgetVisit(visit) }
                        .tint(.red)
                }
            }
        case .pause(let pause):
            LocationPauseRow(pause: pause)
        case .trip(let trip):
            LocationTripRow(trip: trip, correctLeg: correctLeg)
        }
    }
}

extension View {
    /// Asks whether to forget the visit set, from the list a row of it was
    /// swiped in, and forgets it on yes.
    func forgetVisitDialog(_ forgettingVisit: Binding<PlaceVisit?>, forget: @escaping (PlaceVisit) -> Void) -> some View {
        confirmationDialog(
            "Forget this visit?", isPresented: Binding(get: { forgettingVisit.wrappedValue != nil }, set: { if !$0 { forgettingVisit.wrappedValue = nil } }),
            titleVisibility: .visible, presenting: forgettingVisit.wrappedValue
        ) { chosenVisit in
            Button("Forget", role: .destructive) { forget(chosenVisit) }
        } message: { _ in
            Text("It is removed from your history, as if you were never there. The place stays, with its other visits.")
        }
    }
}
