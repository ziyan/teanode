import SwiftUI

/// ScrollEnd is where a transcript's scroll view is against the end of what
/// it holds.
///
/// A lazy list measures its rows only as it draws them, so a jump to the end
/// is worked out from estimated heights. When the rows turn out shorter, or
/// a row at the bottom goes away (the typing dots, a card that folds, a
/// message whose streamed text is replaced by the finished one), the view is
/// left below the last line, blank until the person scrolls up. isPastEnd
/// says so, and the view finds its end again once the scroll is at rest.
struct ScrollEnd: Equatable {
    /// Whether the end is close enough that the jump to it is not offered.
    var isNearEnd: Bool
    /// Whether the view shows a good part of a screen below the last line.
    var isPastEnd: Bool

    /// nearEndHeight is how far above the end still counts as near it.
    init(_ geometry: ScrollGeometry, nearEndHeight: CGFloat) {
        let belowVisible = geometry.contentSize.height - geometry.visibleRect.maxY
        isNearEnd = belowVisible < nearEndHeight
        // A third of the screen, well beyond the bars over the end and the
        // bounce at it, which settle on their own.
        let pastEnd = -belowVisible - geometry.contentInsets.bottom
        isPastEnd = pastEnd > geometry.containerSize.height / 3
    }
}
