import SwiftUI

/// PageRowLabelStyle draws the label of a list row that opens a page: the
/// symbol in a column of one width, centered, in the color of the words.
/// A plain Label in a list greens its symbol on iOS 26 and gives each symbol
/// the width of its own glyph, so a wide one pushes its title to the right
/// of the rows around it. Actions keep `.actionColor()` and do not use this.
struct PageRowLabelStyle: LabelStyle {
    @ScaledMetric(relativeTo: .body) private var iconWidth: CGFloat = 28

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 16) {
            configuration.icon
                .symbolRenderingMode(.monochrome)
                .foregroundStyle(.primary)
                .frame(width: iconWidth)
            // The row's divider starts under the words, as a system Label's
            // does, not under the symbol.
            configuration.title
                .foregroundStyle(.primary)
                .alignmentGuide(.listRowSeparatorLeading) { dimensions in dimensions[.leading] }
        }
    }
}

extension LabelStyle where Self == PageRowLabelStyle {
    static var pageRow: PageRowLabelStyle { PageRowLabelStyle() }
}
