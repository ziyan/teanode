import MapKit
import SwiftUI
import TeaNodeKit

// The people sharing their location with the person: their pictures on the
// map, a row each under it, and, once one is chosen, their visits and places.

/// LocationPeopleRows is a row for each person sharing, under the map, the
/// person's own row first. A tap selects one (the map centers on them), and
/// they stay chosen until another row is tapped: the own row is the way back
/// to the person's own location. With more people than fit under the map, one
/// row says how many and opens a screen of their own.
struct LocationPeopleRows: View {
    let model: LocationModel
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

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
                    NavigationLink(value: LocationRoute.people) {
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
                    if let person = model.selectedPerson {
                        LocationPeopleDivider()
                        LocationChosenPersonCard(person: person, own: model.location)
                    }
                } else {
                    ForEach(model.people) { person in
                        LocationPeopleDivider()
                        if person.userId == model.selectedUserId {
                            LocationChosenPersonCard(person: person, own: model.location)
                        } else {
                            LocationPersonRow(person: person, own: model.location, isSelected: false, isHighlightedWhenChosen: true) {
                                model.select(userId: person.userId)
                            }
                        }
                    }
                }
            }
            .background(Color(.secondarySystemGroupedBackground))
            .clipShape(.rect(cornerRadius: 22, style: .continuous))
            .padding(.horizontal)
            .padding(.vertical, 12)
            // The rows keep their height and the list under them gives way.
            // Past the first accessibility size the text here stops growing,
            // so the rows still leave the list room on the screen.
            .dynamicTypeSize(...DynamicTypeSize.accessibility1)
            .fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// LocationChosenPersonCard is the person shown on the map, opened up in
/// place of their row as Find My does: where they are and how far, the
/// address, how fresh the position is, and directions there. Tapping it
/// again changes nothing: they stay chosen until another row is.
struct LocationChosenPersonCard: View {
    let person: PersonLocation
    let own: PersonLocation?
    @Environment(\.openURL) private var openURL

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 12) {
                AvatarView(
                    name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex,
                    size: LocationPersonRow.avatarSize
                )
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: person.displayName)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.primary)
                    if let placeText {
                        Text(verbatim: placeText).font(.subheadline).foregroundStyle(.primary)
                    }
                    if let addressText {
                        Text(verbatim: addressText).font(.subheadline).foregroundStyle(.secondary)
                    }
                    Text(verbatim: freshText).font(.subheadline).foregroundStyle(.tint)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isSelected)
            if let point = person.latestPoint {
                Button {
                    if let url = URL(string: "https://maps.apple.com/?daddr=\(point.latitudeDegrees),\(point.longitudeDegrees)") {
                        openURL(url)
                    }
                } label: {
                    Label("Directions", systemImage: "car.fill").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
                .padding(.leading, LocationPersonRow.avatarSize + 12)
            }
        }
        .padding(16)
        .background(Color(.tertiarySystemFill))
    }

    /// "Home · Nearby": the place they are at and how far away, or either.
    private var placeText: String? {
        let placeName = person.openVisit?.place.shownName
        let distanceText = LocationText.distanceText(from: own, to: person)
        switch (placeName, distanceText) {
        case let (placeName?, distanceText?): return String(localized: "\(placeName) · \(distanceText)")
        case let (placeName?, nil): return placeName
        case let (nil, distanceText?): return distanceText
        case (nil, nil): return nil
        }
    }

    /// The address of the place they are at, when it says more than its name.
    private var addressText: String? {
        guard let place = person.openVisit?.place else { return nil }
        let addressText = place.addressText.trimmingCharacters(in: .whitespaces)
        return addressText.isEmpty || addressText == place.shownName ? nil : addressText
    }

    /// "3 minutes ago", for their latest position.
    private var freshText: String {
        guard let measured = person.latestPoint?.measuredDate else { return String(localized: "No position yet") }
        return measured.formatted(.relative(presentation: .named, unitsStyle: .wide))
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
/// a row each; choosing one shows them on the map.
struct LocationPeopleScreen: View {
    let model: LocationModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        List {
            LocationOwnRow(model: model) { dismiss() }
            ForEach(model.people) { person in
                LocationPersonRow(person: person, own: model.location, isSelected: person.userId == model.selectedUserId) {
                    model.select(userId: person.userId)
                    dismiss()
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

/// LocationPersonPanel is what is under the map once somebody sharing is
/// selected: at "Where I am and where I have been", their visits for a day
/// with a way back a day, and their places, to read and not to change.
struct LocationPersonPanel: View {
    let model: LocationModel
    let person: PersonLocation

    var body: some View {
        List {
            if person.level == .history {
                Section {
                    Button("Previous day", systemImage: "chevron.left") { model.movePersonDay(by: -1) }
                        .actionColor()
                    if model.canMovePersonDayLater {
                        Button("Next day", systemImage: "chevron.right") { model.movePersonDay(by: 1) }
                            .actionColor()
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
