import SwiftUI
import TeaNodeKit

/// LocationDrawerDetent is how much of the screen the drawer over the map
/// takes: its header only, half of it, or all of it.
enum LocationDrawerDetent: CaseIterable {
    case collapsed
    case half
    case full

    /// The next taller one, and the next shorter one; itself at the ends.
    var taller: LocationDrawerDetent {
        switch self {
        case .collapsed: .half
        case .half, .full: .full
        }
    }

    var shorter: LocationDrawerDetent {
        switch self {
        case .collapsed, .half: .collapsed
        case .full: .half
        }
    }

    /// What VoiceOver says the drawer is at.
    var title: String {
        switch self {
        case .collapsed: String(localized: "Collapsed")
        case .half: String(localized: "Half height")
        case .full: String(localized: "Full height")
        }
    }

    /// How tall the drawer is at this detent, given the height of the screen
    /// between its bars and the height of the drawer's grabber and header.
    func height(availableHeight: CGFloat, headerHeight: CGFloat) -> CGFloat {
        switch self {
        case .collapsed: min(headerHeight, availableHeight)
        case .half: min(max(availableHeight * 0.5, headerHeight), availableHeight)
        case .full: availableHeight
        }
    }
}

/// LocationDrawer is the panel over the map that holds Location's lists, as
/// Find My holds its people: it opens at half the screen, and its grabber
/// drags it up to the whole screen or down to its header, or steps it with a
/// tap. Drawn by hand: a sheet with detents would cover the tab bar, and the
/// person card, the check-in and Location's other sheets come up over this one
/// from the app and the screen.
struct LocationDrawer<Header: View, Content: View>: View {
    @Binding var detent: LocationDrawerDetent
    /// The height of the grabber and the header, which is the drawer's height
    /// when collapsed; the screen reads it to keep the map's middle above the
    /// drawer.
    @Binding var headerHeight: CGFloat
    /// The height of the screen between its bars.
    let availableHeight: CGFloat
    @ViewBuilder let header: () -> Header
    @ViewBuilder let content: () -> Content
    @GestureState private var dragTranslation: CGFloat = 0

    private var restingHeight: CGFloat {
        detent.height(availableHeight: availableHeight, headerHeight: headerHeight)
    }

    /// The height while a finger drags the grabber, held between collapsed and
    /// full.
    private var shownHeight: CGFloat {
        min(max(restingHeight - dragTranslation, min(headerHeight, availableHeight)), availableHeight)
    }

    var body: some View {
        VStack(spacing: 0) {
            VStack(spacing: 0) {
                grabber
                header()
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { headerHeight = $0 }
            .contentShape(.rect)
            .gesture(drag)
            content()
                .frame(maxHeight: .infinity, alignment: .top)
                .clipped()
                // Collapsed, the lists are out of sight and out of reach.
                .accessibilityHidden(detent == .collapsed)
        }
        .frame(height: shownHeight, alignment: .top)
        .frame(maxWidth: .infinity)
        // The grouped gray of the lists in it, running on under the tab bar as
        // a sheet's does.
        .background {
            UnevenRoundedRectangle(topLeadingRadius: 28, topTrailingRadius: 28, style: .continuous)
                .fill(Color(.systemGroupedBackground))
                .shadow(radius: 4)
                .ignoresSafeArea(edges: .bottom)
        }
        .animation(.snappy, value: detent)
    }

    /// The bar at the top: a tap makes the drawer taller, or half from full;
    /// VoiceOver adjusts it up and down.
    private var grabber: some View {
        Button {
            detent = detent == .full ? .half : detent.taller
        } label: {
            Capsule()
                .fill(.tertiary)
                .frame(width: 36, height: 5)
                .frame(maxWidth: .infinity, minHeight: 22)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Drawer")
        .accessibilityValue(detent.title)
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: detent = detent.taller
            case .decrement: detent = detent.shorter
            @unknown default: break
            }
        }
    }

    /// Dragging the grabber or the header moves the drawer with the finger,
    /// and on release it settles at the detent nearest where the flick would
    /// have carried it.
    private var drag: some Gesture {
        DragGesture(minimumDistance: 8, coordinateSpace: .global)
            .updating($dragTranslation) { value, state, _ in
                state = value.translation.height
            }
            .onEnded { value in
                let landingHeight = restingHeight - value.predictedEndTranslation.height
                detent = nearestDetent(to: landingHeight)
            }
    }

    private func nearestDetent(to height: CGFloat) -> LocationDrawerDetent {
        var nearest = LocationDrawerDetent.half
        var nearestDistance = CGFloat.infinity
        for candidate in LocationDrawerDetent.allCases {
            let distance = abs(candidate.height(availableHeight: availableHeight, headerHeight: headerHeight) - height)
            if distance < nearestDistance {
                nearest = candidate
                nearestDistance = distance
            }
        }
        return nearest
    }
}

/// LocationChosenPersonHeader is the drawer's header while a person sharing is
/// chosen: who they are and where, and the way back to everyone.
struct LocationChosenPersonHeader: View {
    let person: PersonLocation
    let close: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            AvatarView(name: person.displayName, avatarUrl: person.avatarUrl, colorIndex: person.avatarColorIndex, size: 36)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: person.displayName)
                    .font(.headline)
                Text(verbatim: LocationText.whereText(of: person))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .combine)
            .frame(maxWidth: .infinity, alignment: .leading)
            Button("Close", systemImage: "xmark.circle.fill", action: close)
                .labelStyle(.iconOnly)
                .font(.title2)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(minWidth: 44, minHeight: 44)
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
    }
}
