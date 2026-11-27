import SwiftUI

/// FeedSwipeToDismiss lets a card be swiped away, either way, as a message
/// is in Mail: the card follows the finger with "Dismiss" showing behind it,
/// and past two fifths of its width, or flicked, it flies off and is
/// dismissed. Anything shorter springs back. The feed is a ScrollView, not a
/// List, so the system's swipe actions are not there to use; a drag is told
/// apart from a scroll by its direction, decided once at its start, so a
/// scroll that wanders sideways stays a scroll.
///
/// The card moves with the finger, so to a link or button under it the
/// finger never left it, and letting go would read as a tap there. From the
/// moment a drag is taken as a swipe until shortly after it ends, the card's
/// buttons are switched off and its links are not followed.
struct FeedSwipeToDismiss: ViewModifier {
    let isEnabled: Bool
    let onDismiss: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.openURL) private var inheritedOpenURL
    @State private var offset: CGFloat = 0
    @State private var cardWidth: CGFloat = 0
    /// Whether the drag under way was taken as a swipe; nil until it moved
    /// far enough to tell.
    @State private var isSwiping: Bool?
    @State private var isDismissing = false
    /// Whether the card's own taps are held back: from the start of a swipe
    /// until just after it ends, so the lift that ends it is no tap.
    @State private var isSwipeHeld = false
    /// The same, read when a link is followed, which may come in the same
    /// moment as the swipe's end, before the view is drawn again.
    @State private var swipeHold = FeedSwipeHold()
    /// Whether a finger is on the card in a drag; it turns false however the
    /// drag ends, a cancelled one too, which tells onEnded nothing.
    @GestureState private var isDragging = false

    /// The share of the width past which letting go dismisses.
    private static let dismissShare: CGFloat = 0.4

    func body(content: Content) -> some View {
        if isEnabled {
            content
                .disabled(isSwipeHeld)
                .environment(\.openURL, OpenURLAction { address in
                    if swipeHold.isHeld { return .handled }
                    inheritedOpenURL(address)
                    return .handled
                })
                .offset(x: offset)
                .background(alignment: offset < 0 ? .trailing : .leading) { swipeBackground }
                .onGeometryChange(for: CGFloat.self) { proxy in proxy.size.width } action: { width in cardWidth = width }
                .simultaneousGesture(drag)
                .onChange(of: isDragging) { _, isNowDragging in
                    guard !isNowDragging else { return }
                    // A drag cut off before it ended puts the card back as a
                    // short swipe does, and the next drag is decided afresh;
                    // onEnded has had its say by then.
                    Task { @MainActor in
                        try? await Task.sleep(for: .milliseconds(50))
                        guard !isDismissing else { return }
                        isSwiping = nil
                        guard isSwipeHeld else { return }
                        if offset != 0 { withAnimation(.spring(duration: 0.3)) { offset = 0 } }
                        releaseSwipeHold()
                    }
                }
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
        // Measured on the screen: the card moves with the finger, so in
        // its own space the finger would hardly move.
        DragGesture(minimumDistance: 20, coordinateSpace: .global)
            .updating($isDragging) { _, isDraggingNow, _ in isDraggingNow = true }
            .onChanged { value in
                guard !isDismissing else { return }
                if isSwiping == nil {
                    // Decided once: mostly sideways is a swipe, anything
                    // else is the scroll's.
                    isSwiping = abs(value.translation.width) > abs(value.translation.height) * 1.5
                }
                if isSwiping == true {
                    if !isSwipeHeld {
                        isSwipeHeld = true
                        swipeHold.isHeld = true
                    }
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
                    releaseSwipeHold()
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

    /// Gives the card its taps back once the lift that ended the swipe has
    /// passed, so a tap that comes in with it is still dropped.
    private func releaseSwipeHold() {
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(400))
            isSwipeHeld = false
            swipeHold.isHeld = false
        }
    }
}

/// Whether a swipe holds back the card's taps, kept in an object so a link
/// followed in the same moment reads it as it is now, not as it was when the
/// card was last drawn.
final class FeedSwipeHold {
    var isHeld = false
}
