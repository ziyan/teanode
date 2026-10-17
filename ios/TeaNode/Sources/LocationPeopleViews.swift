import MapKit
import SwiftUI
import TeaNodeKit

// The people sharing their location with the person: their pictures on the
// map, a row each under it, and, once one is chosen, their visits and places.

/// LocationPeopleRows is a row for each person sharing, under the map. A tap
/// selects them (the map centers on them); a tap on the selected one goes
/// back to the person's own location. With more people than fit under the
/// map, one row says how many and opens a screen of their own.
struct LocationPeopleRows: View {
    let model: LocationModel

    /// The most people given a row of their own under the map.
    private static let mostRows = 3

    var body: some View {
        if !model.people.isEmpty {
            VStack(spacing: 0) {
                if model.people.count > Self.mostRows {
                    NavigationLink(value: LocationRoute.people) {
                        HStack {
                            Label("\(model.people.count) people", systemImage: "person.2")
                                .foregroundStyle(.primary)
                            Spacer()
                            Image(systemName: "chevron.right")
                                .font(.footnote)
                                .foregroundStyle(.tertiary)
                                .accessibilityHidden(true)
                        }
                        .frame(minHeight: 44)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint("Shows everyone who shares where they are with you")
                    if let person = model.selectedPerson {
                        LocationPersonRow(person: person, isSelected: true) { model.clearSelection() }
                    }
                } else {
                    ForEach(model.people) { person in
                        LocationPersonRow(person: person, isSelected: person.userId == model.selectedUserId) {
                            if person.userId == model.selectedUserId { model.clearSelection() } else { model.select(userId: person.userId) }
                        }
                    }
                }
            }
            .padding(.horizontal)
        }
    }
}

/// LocationPeopleScreen is everyone sharing their location with the person,
/// a row each; choosing one shows them on the map.
struct LocationPeopleScreen: View {
    let model: LocationModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        List(model.people) { person in
            LocationPersonRow(person: person, isSelected: person.userId == model.selectedUserId) {
                model.select(userId: person.userId)
                dismiss()
            }
        }
        .navigationTitle("People")
        .navigationBarTitleDisplayMode(.inline)
    }
}

struct LocationPersonRow: View {
    let person: PersonLocation
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 32)
                Text(verbatim: LocationText.line(of: person))
                    .font(.subheadline)
                    .foregroundStyle(.primary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if isSelected {
                    Image(systemName: "checkmark")
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                }
            }
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
        .accessibilityHint(isSelected ? Text("Shows your own location") : Text("Shows them on the map"))
    }
}

/// LocationPersonPanel is what is under the map once somebody sharing is
/// selected: at "Where I am and where I have been", their visits for a day
/// with a way back a day, and their places, to read and not to change.
struct LocationPersonPanel: View {
    let model: LocationModel
    let person: PersonLocation

    var body: some View {
        List {
            Section {
                Button("Back to my location", systemImage: "chevron.backward") { model.clearSelection() }
            }
            if person.level == .history {
                Section {
                    Button("Previous day", systemImage: "chevron.left") { model.movePersonDay(by: -1) }
                    if model.canMovePersonDayLater {
                        Button("Next day", systemImage: "chevron.right") { model.movePersonDay(by: 1) }
                    }
                    if let problem = model.personProblemText {
                        Text(verbatim: problem).foregroundStyle(.secondary)
                    } else if model.personVisits.isEmpty {
                        if model.isLoadingPerson {
                            ProgressView().frame(maxWidth: .infinity)
                        } else {
                            Text("No visits this day").foregroundStyle(.secondary)
                        }
                    }
                    ForEach(model.personVisits) { visit in
                        LocationVisitRow(visit: visit)
                    }
                } header: {
                    Text(verbatim: LocationText.dayTitle(model.personDay)).font(.headline).textCase(nil)
                }
                if !model.personPlaces.isEmpty {
                    Section("Places") {
                        ForEach(model.personPlaces) { place in
                            LocationPlaceRow(place: place)
                        }
                    }
                }
                Section {
                } footer: {
                    OpenStreetMapCredit()
                }
            } else {
                Section {
                    Text("\(person.displayName) shares where they are now. Their visits and places are not shared with you.")
                        .foregroundStyle(.secondary)
                }
            }
        }
        .refreshable { await model.loadPerson() }
    }
}
