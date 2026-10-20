import MapKit
import SwiftUI
import TeaNodeKit

// The people sharing their location with the person: their pictures on the
// map, a row each under it, and, once one is chosen, their visits and places.

/// LocationPeopleRows is a row for each person sharing, at the top of the
/// drawer's visits, the person's own row first. A tap selects one (the map centers on them) and
/// brings up their card over the map, and they stay chosen until another row
/// is tapped: the own row is the way back to the person's own location. With more people than fit under the map, one
/// row says how many and opens a screen of their own.
struct LocationPeopleRows: View {
    let model: LocationModel
    /// Opens the screen of everyone sharing. A button, not a navigation link:
    /// a list makes a link inside a row a row of its own, with a second
    /// chevron and the list's text size.
    let showPeople: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(AppModel.self) private var app

    /// The most people given a row of their own under the map. At the
    /// accessibility text sizes a row is as tall as a third of the map, so
    /// everyone is one row away instead.
    private var mostRows: Int { dynamicTypeSize.isAccessibilitySize ? 0 : 3 }

    var body: some View {
        if model.location != nil || !model.people.isEmpty {
            // One card of rows, as Find My lists people: a divider between
            // rows starts under the names, not under the pictures.
            VStack(spacing: 0) {
                LocationOwnRow(model: model, isHighlightedWhenChosen: true)
                if model.people.count > mostRows {
                    LocationPeopleDivider()
                    Button(action: showPeople) {
                        HStack(spacing: 12) {
                            Image(systemName: "person.2")
                                .font(.title3)
                                .foregroundStyle(.tint)
                                .frame(width: LocationPersonRow.avatarSize)
                            Text("\(model.people.count) people")
                                .foregroundStyle(.primary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            Image(systemName: "chevron.right")
                                .font(.footnote)
                                .foregroundStyle(.tertiary)
                                .accessibilityHidden(true)
                        }
                        .padding(.horizontal, 16)
                        .frame(minHeight: 60)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint("Shows everyone who shares where they are with you")
                } else {
                    ForEach(model.people) { person in
                        LocationPeopleDivider()
                        LocationPersonRow(
                            person: person, own: model.location, isSelected: person.userId == model.selectedUserId,
                            isHighlightedWhenChosen: true
                        ) {
                            choose(person)
                        }
                    }
                }
            }
            .background(Color(.secondarySystemGroupedBackground))
            .clipShape(.rect(cornerRadius: 22, style: .continuous))
            // Past the first accessibility size the text here stops growing,
            // so a row still leaves the visits room in the drawer.
            .dynamicTypeSize(...DynamicTypeSize.accessibility1)
            .fixedSize(horizontal: false, vertical: true)
        }
    }

    /// Shows them on the map and brings up their card over it. Chosen again,
    /// they stay chosen and the card comes up again.
    private func choose(_ person: PersonLocation) {
        model.select(userId: person.userId)
        app.openedPerson = .account(userId: person.userId)
    }
}

/// LocationPeopleSection is the people's card as the first section of a list
/// in the drawer, so it scrolls with the visits under it.
struct LocationPeopleSection: View {
    let model: LocationModel
    let showPeople: () -> Void

    var body: some View {
        if model.location != nil || !model.people.isEmpty {
            Section {
                LocationPeopleRows(model: model, showPeople: showPeople)
                    .listRowInsets(EdgeInsets())
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
            }
        }
    }
}

/// LocationPeopleDivider is the line between two people, starting where
/// their names do.
struct LocationPeopleDivider: View {
    /// The picture's width, grown with the text as AvatarView grows it.
    @ScaledMetric(relativeTo: .headline) private var avatarDiameter = LocationPersonRow.avatarSize

    var body: some View {
        Divider().padding(.leading, 16 + avatarDiameter + 12)
    }
}

/// LocationPeopleScreen is everyone sharing their location with the person,
/// a row each; choosing one shows them on the map, with their card over it.
struct LocationPeopleScreen: View {
    let model: LocationModel
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app

    var body: some View {
        List {
            LocationOwnRow(model: model) { dismiss() }
            ForEach(model.people) { person in
                LocationPersonRow(person: person, own: model.location, isSelected: person.userId == model.selectedUserId) {
                    model.select(userId: person.userId)
                    dismiss()
                    app.openedPerson = .account(userId: person.userId)
                }
            }
        }
        .navigationTitle("People")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// LocationOwnRow is the person's own row, first in the list of people and
/// drawn like the others: their picture, "You", and where they are ("Hillside
/// School · 1 min. ago"). A tap centers the map on them.
struct LocationOwnRow: View {
    let model: LocationModel
    var isHighlightedWhenChosen = false
    var didChoose: () -> Void = {}

    var body: some View {
        if let own = model.location {
            LocationPeopleRowLabel(
                person: own, nameText: String(localized: "You"), whereText: LocationText.whereText(of: own, isOwn: true),
                distanceText: nil, isSelected: model.selectedUserId == nil, isHighlightedWhenChosen: isHighlightedWhenChosen
            ) {
                model.focusOnOwn()
                didChoose()
            }
            .accessibilityHint("Shows you on the map")
        }
    }
}

struct LocationPersonRow: View {
    /// The size of a person's picture in a row, as Find My draws it.
    static let avatarSize: CGFloat = 44

    let person: PersonLocation
    let own: PersonLocation?
    let isSelected: Bool
    var isHighlightedWhenChosen = false
    let action: () -> Void

    var body: some View {
        LocationPeopleRowLabel(
            person: person, nameText: person.displayName, whereText: LocationText.whereText(of: person),
            distanceText: LocationText.distanceText(from: own, to: person), isSelected: isSelected,
            isHighlightedWhenChosen: isHighlightedWhenChosen, action: action
        )
        .accessibilityHint(isSelected ? Text("Shown on the map") : Text("Shows them on the map"))
    }
}

/// LocationPeopleRowLabel is one person's row: the picture, the name over
/// where they are, and how far away on the right. The one shown on the map is
/// a filled row in the card under the map, and has a check in a list, whose
/// rows are filled already.
struct LocationPeopleRowLabel: View {
    let person: PersonLocation
    let nameText: String
    let whereText: String
    let distanceText: String?
    let isSelected: Bool
    let isHighlightedWhenChosen: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                AvatarView(
                    name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex,
                    size: LocationPersonRow.avatarSize
                )
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: nameText)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.primary)
                    Text(verbatim: whereText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if let distanceText {
                    Text(verbatim: distanceText)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if isSelected && !isHighlightedWhenChosen {
                    Image(systemName: "checkmark")
                        .foregroundStyle(.tint)
                        .accessibilityHidden(true)
                }
            }
            .padding(.vertical, isHighlightedWhenChosen ? 10 : 4)
            .padding(.horizontal, isHighlightedWhenChosen ? 16 : 0)
            .frame(minHeight: 60)
            .background(isSelected && isHighlightedWhenChosen ? Color(.tertiarySystemFill) : .clear)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}
