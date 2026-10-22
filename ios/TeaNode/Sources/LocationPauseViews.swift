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

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "pause.circle")
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(width: 28)
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
/// or a pause.
struct LocationTimelineRow: View {
    let entry: LocationTimelineEntry

    var body: some View {
        switch entry {
        case .visit(let visit):
            NavigationLink(value: LocationRoute.place(visit.place)) {
                LocationVisitRow(visit: visit)
            }
        case .pause(let pause):
            LocationPauseRow(pause: pause)
        }
    }
}
