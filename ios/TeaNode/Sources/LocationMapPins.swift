import MapKit
import SwiftUI
import TeaNodeKit

// The pictures of people on Location's map. Pictures that would cover each
// other at the map's zoom are drawn as one group, as Find My draws them, so
// nobody is hidden under somebody else. Drawn by hand: MapKit clusters its own
// markers, but not the annotations a SwiftUI map draws.

/// LocationMapPin is one person's picture on the map: the person's own, or
/// somebody sharing with them.
struct LocationMapPin: Identifiable {
    let person: PersonLocation
    let coordinate: CLLocationCoordinate2D
    let isOwn: Bool
    /// The person's user id, or nothing for the person's own, as
    /// LocationModel.selectedUserId is nil for them.
    var id: String { isOwn ? "" : person.userId }
}

/// LocationMapPinGroup is pictures drawn as one: a picture alone, or several
/// close enough at this zoom to cover each other.
struct LocationMapPinGroup: Identifiable {
    /// The pictures, the chosen one first.
    let pins: [LocationMapPin]
    var id: String { pins.map(\.id).joined(separator: " ") }
    /// Where the group is drawn: at its first picture, so the chosen person's
    /// stays where they are.
    var coordinate: CLLocationCoordinate2D { pins[0].coordinate }
    var isChosen: Bool

    /// The diameter of a picture on the map.
    static let pictureSize: CGFloat = 36
    /// How close two pictures' middles may come, in points, before one covers
    /// the other: a picture and its ring.
    static let overlapDistance: Double = 42

    /// The groups the pictures make at this zoom, the chosen one's group last
    /// so it is drawn over the others. Before the map has said what it shows,
    /// every picture is a group of its own.
    static func grouping(
        _ pins: [LocationMapPin], chosenId: String, visibleRect: MKMapRect?, mapWidth: CGFloat
    ) -> [LocationMapPinGroup] {
        let ordered = pins.filter { $0.id == chosenId } + pins.filter { $0.id != chosenId }
        var groups: [[LocationMapPin]] = []
        if let visibleRect, visibleRect.size.width > 0, mapWidth > 0 {
            let pointsPerMapPoint = Double(mapWidth) / visibleRect.size.width
            var anchors: [MKMapPoint] = []
            for pin in ordered {
                let point = MKMapPoint(pin.coordinate)
                let overlapping = anchors.firstIndex { anchor in
                    hypot(anchor.x - point.x, anchor.y - point.y) * pointsPerMapPoint < overlapDistance
                }
                if let overlapping {
                    groups[overlapping].append(pin)
                } else {
                    groups.append([pin])
                    anchors.append(point)
                }
            }
        } else {
            groups = ordered.map { [$0] }
        }
        let made = groups.map { pins in LocationMapPinGroup(pins: pins, isChosen: pins.contains { $0.id == chosenId }) }
        return made.filter { !$0.isChosen } + made.filter(\.isChosen)
    }

    /// What the map writes under the group: each name, the person's own as "You".
    var title: String {
        ListFormatter.localizedString(byJoining: pins.map { pin in pin.isOwn ? String(localized: "You") : pin.person.displayName })
    }
}

/// LocationMapPinGroupView draws a group: a picture alone chooses its person
/// when tapped; several overlap, the chosen one in front, and a tap lists
/// them to choose from.
struct LocationMapPinGroupView: View {
    let group: LocationMapPinGroup
    let chosenId: String
    let choose: (LocationMapPin) -> Void

    /// The most pictures drawn in a group; the rest are a count beside them.
    private static let mostPictures = 3

    var body: some View {
        if group.pins.count == 1, let pin = group.pins.first {
            Button {
                choose(pin)
            } label: {
                picture(of: pin)
                    // A touch target of 44 points around the picture.
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(pin.isOwn ? Text("You") : Text(verbatim: pin.person.displayName))
            .accessibilityHint(pin.isOwn ? Text("Shows you on the map") : Text("Shows them on the map"))
        } else {
            Menu {
                ForEach(group.pins) { pin in
                    Button {
                        choose(pin)
                    } label: {
                        if pin.id == chosenId {
                            Label(name(of: pin), systemImage: "checkmark")
                        } else {
                            Text(verbatim: name(of: pin))
                        }
                    }
                }
            } label: {
                stack
            }
            .buttonStyle(.plain)
            .accessibilityLabel(Text(verbatim: group.title))
            .accessibilityHint("Lists who is here, to choose one")
        }
    }

    /// The group's pictures overlapping, the first in front, and how many more.
    private var stack: some View {
        let shown = Array(group.pins.prefix(Self.mostPictures))
        let moreCount = group.pins.count - shown.count
        return HStack(spacing: -LocationMapPinGroup.pictureSize / 2) {
            ForEach(Array(shown.enumerated()), id: \.element.id) { index, pin in
                picture(of: pin)
                    .zIndex(Double(shown.count - index))
            }
            if moreCount > 0 {
                Text(verbatim: "+\(moreCount.formatted())")
                    .font(.caption.bold())
                    .foregroundStyle(.primary)
                    .padding(.horizontal, 8)
                    .frame(minHeight: 26)
                    .background(Color(.systemBackground), in: Capsule())
                    .padding(.leading, LocationMapPinGroup.pictureSize / 2 + 2)
            }
        }
        .frame(minHeight: 44)
        .contentShape(Rectangle())
    }

    private func picture(of pin: LocationMapPin) -> some View {
        AvatarView(
            name: pin.person.displayName, avatarUrl: pin.person.avatarUrl, colorIndex: pin.person.avatarColorIndex,
            size: LocationMapPinGroup.pictureSize
        )
        .overlay(Circle().stroke(pin.id == chosenId ? Color.accentColor : Color(.systemBackground), lineWidth: 3))
    }

    private func name(of pin: LocationMapPin) -> String {
        pin.isOwn ? String(localized: "You") : pin.person.displayName
    }
}
