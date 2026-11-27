import SwiftUI

/// FeedSwipeToDismiss lets a card be swiped away, either way, as a message
/// is in Mail: the card follows the finger with "Dismiss" showing behind it,
/// and past two fifths of its width, or flicked, it flies off and is
/// dismissed. Anything shorter springs back. The feed is a ScrollView, not a
/// List, so the system's swipe actions are not there to use; a drag is told
/// apart from a scroll by its direction, decided once at its start, so a
/// scroll that wanders sideways stays a scroll.
struct FeedSwipeToDismiss: ViewModifier {
    let isEnabled: Bool
    let onDismiss: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var offset: CGFloat = 0
    @State private var cardWidth: CGFloat = 0
    /// Whether the drag under way was taken as a swipe; nil until it moved
    /// far enough to tell.
    @State private var isSwiping: Bool?
    @State private var isDismissing = false

    /// The share of the width past which letting go dismisses.
    private static let dismissShare: CGFloat = 0.4

    func body(content: Content) -> some View {
        if isEnabled {
            content
                .offset(x: offset)
                .background(alignment: offset < 0 ? .trailing : .leading) { swipeBackground }
                .onGeometryChange(for: CGFloat.self) { proxy in proxy.size.width } action: { width in cardWidth = width }
                .simultaneousGesture(drag)
                .accessibilityAction(named: Text("Dismiss")) { onDismiss() }
        } else {
            content
        }
    }

    /// What shows where the card was: the word and its symbol, stronger as
    /// the card goes further.
    private var swipeBackground: some View {
        Label("Dismiss", systemImage: "xmark.circle")
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 24)
            .opacity(cardWidth > 0 ? min(1, abs(offset) / (cardWidth * Self.dismissShare)) : 0)
            .accessibilityHidden(true)
    }

    private var drag: some Gesture {
        DragGesture(minimumDistance: 20, coordinateSpace: .local)
            .onChanged { value in
                guard !isDismissing else { return }
                if isSwiping == nil {
                    // Decided once: mostly sideways is a swipe, anything
                    // else is the scroll's.
                    isSwiping = abs(value.translation.width) > abs(value.translation.height) * 1.5
                }
                if isSwiping == true {
                    offset = value.translation.width
                }
            }
            .onEnded { value in
                defer { isSwiping = nil }
                guard isSwiping == true, !isDismissing else { return }
                let isFarEnough = abs(value.translation.width) > cardWidth * Self.dismissShare
                let isFlicked = abs(value.predictedEndTranslation.width) > cardWidth * 0.8
                guard isFarEnough || isFlicked else {
                    withAnimation(.spring(duration: 0.3)) { offset = 0 }
                    return
                }
                isDismissing = true
                let direction: CGFloat = (isFarEnough ? value.translation.width : value.predictedEndTranslation.width) < 0 ? -1 : 1
                if reduceMotion {
                    onDismiss()
                    return
                }
                withAnimation(.easeOut(duration: 0.2)) {
                    offset = direction * (cardWidth + 60)
                } completion: {
                    onDismiss()
                }
            }
    }
}
