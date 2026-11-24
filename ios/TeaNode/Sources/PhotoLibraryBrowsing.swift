import SwiftUI
import TeaNodeKit
import UIKit

/// PhotoScaleBar floats over the bottom of the library: the switch of Years,
/// Months and All in one capsule. Glass, because it floats over the photos.
/// The library is searched from the Search tab, whose button is in the tab bar
/// just below, so the bar has no search of its own. On a phone the tab bar
/// holds the switch instead (PhotoScaleAccessory); this bar is the iPad's.
struct PhotoScaleBar: View {
    @Binding var scale: PhotoScale

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            PhotoScaleSegments(scale: $scale, isCompact: false)
                .padding(4)
                .glassEffect(.regular, in: .capsule)
        }
    }
}

/// PhotoScaleAccessory puts the switch in the tab bar, as the Photos app does:
/// over the tab bar while it is whole, and between the tab and Search once it
/// collapses as the photos scroll. The bar draws the glass around it.
struct PhotoScaleAccessory: ViewModifier {
    let isShown: Bool
    @Binding var scale: PhotoScale

    /// Whether the tab bar holds the switch: on a phone, from the first system
    /// that can take the accessory away again while the library is not shown.
    static func isUsed(horizontalSizeClass: UserInterfaceSizeClass?) -> Bool {
        guard #available(iOS 26.1, *) else { return false }
        return UIDevice.current.userInterfaceIdiom != .pad || horizontalSizeClass != .regular
    }

    func body(content: Content) -> some View {
        if #available(iOS 26.1, *) {
            content.tabViewBottomAccessory(isEnabled: isShown) {
                PhotoScaleAccessoryContent(scale: $scale)
            }
        } else {
            content
        }
    }
}

/// PhotoScaleAccessoryContent is the switch in the tab bar, smaller once the
/// bar has collapsed and it sits between the tab and Search.
private struct PhotoScaleAccessoryContent: View {
    @Binding var scale: PhotoScale
    @Environment(\.tabViewBottomAccessoryPlacement) private var placement

    var body: some View {
        PhotoScaleSegments(scale: $scale, isCompact: placement == .inline)
            .padding(.horizontal, 4)
    }
}

/// PhotoScaleSegments is the three choices side by side, the one chosen
/// marked with the accent. There is no floating segmented control in the
/// system, so this is drawn: three buttons in whatever glass holds them.
struct PhotoScaleSegments: View {
    @Binding var scale: PhotoScale
    /// Smaller words and height, for the collapsed tab bar.
    let isCompact: Bool
    @Environment(\.accessibilityReduceMotion) private var isReduceMotionEnabled

    private var switchAnimation: Animation? {
        isReduceMotionEnabled ? nil : Animation.snappy
    }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(PhotoScale.allCases) { option in
                segment(option)
            }
        }
        .animation(switchAnimation, value: scale)
    }

    private func segment(_ option: PhotoScale) -> some View {
        let isChosen = option == scale
        return Button {
            scale = option
        } label: {
            Text(option.title)
                .font(isCompact ? .footnote.weight(.semibold) : .subheadline.weight(.semibold))
                .foregroundStyle(isChosen ? Color.accentColor : Color.primary)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .frame(maxWidth: .infinity, minHeight: isCompact ? 32 : 44)
                .background {
                    if isChosen {
                        Capsule().fill(Color.accentColor.opacity(0.18))
                    }
                }
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(isChosen ? .isSelected : [])
    }
}

/// PhotoPeriodReading is what asking the server about one year or month came
/// to: the photo that stands for it and how many photos it has, or that it has none.
enum PhotoPeriodReading: Equatable {
    case summary(PhotoPeriodSummary)
    case empty
}

/// PhotoOverviewReader reads the Years or Months of one list a period at a
/// time, as the tiles come near the screen: the years the list spans first,
/// then for each period its newest photo and how many it has. It asks for a few
/// periods at once, however many tiles are on screen.
@MainActor
@Observable
final class PhotoOverviewReader {
    /// How many periods are asked of the server at once.
    static let mostReadsAtOnce = 6

    private let connection: Connection
    let list: PhotoListQuery
    /// The years the list has photos in; nil until read.
    private(set) var yearSpan: PhotoYearSpan?
    private(set) var failureText: String?
    /// What each period read so far came to, by the period's id.
    private(set) var readings: [String: PhotoPeriodReading] = [:]
    @ObservationIgnored private var readCount = 0
    @ObservationIgnored private var waitingReads: [CheckedContinuation<Void, Never>] = []
    @ObservationIgnored private var periodIdsBeingRead: Set<String> = []

    init(connection: Connection, list: PhotoListQuery) {
        self.connection = connection
        self.list = list
    }

    /// Reads the years the list spans, unless they have been read.
    func readYearSpan() async {
        guard yearSpan == nil else { return }
        do {
            yearSpan = try await connection.photoYearSpan(of: list)
            failureText = nil
        } catch {
            guard !Task.isCancelled else { return }
            failureText = AppModel.shared.describe(error)
        }
    }

    /// Reads one period, unless it has been read or is being read. A failure
    /// leaves it unread, to be tried again when its tile next comes on screen.
    func read(_ period: PhotoPeriod) async {
        guard readings[period.id] == nil, !periodIdsBeingRead.contains(period.id) else { return }
        periodIdsBeingRead.insert(period.id)
        defer { periodIdsBeingRead.remove(period.id) }
        await waitForTurn()
        defer { finishTurn() }
        guard !Task.isCancelled else { return }
        do {
            if let summary = try await connection.photoPeriodSummary(of: list, period: period) {
                readings[period.id] = .summary(summary)
            } else {
                readings[period.id] = .empty
            }
        } catch {
            // The tile asks again when it comes back on screen.
        }
    }

    private func waitForTurn() async {
        if readCount < Self.mostReadsAtOnce {
            readCount += 1
            return
        }
        await withCheckedContinuation { waitingReads.append($0) }
    }

    private func finishTurn() {
        if waitingReads.isEmpty {
            readCount -= 1
        } else {
            waitingReads.removeFirst().resume()
        }
    }
}

/// PhotoOverviewList is the Months and Years of a list: a large picture for
/// each period, edge to edge, with its name and how many photos it holds over the
/// bottom. The server is asked for a period when its tile comes near the
/// screen, so the list does not read the photos of the years it shows. A period
/// with no photos shows nothing.
struct PhotoOverviewList: View {
    enum Kind {
        case years, months

        var aspectRatio: CGFloat { self == .years ? 1 : 1.6 }
        var titleFont: Font { self == .years ? .largeTitle.bold() : .title2.bold() }
    }

    let kind: Kind
    let listKey: PhotoListKey
    let model: PhotosModel
    let onOpen: (PhotoPeriod) -> Void
    @State private var reader: PhotoOverviewReader?

    private func periods(in span: PhotoYearSpan) -> [PhotoPeriod] {
        switch kind {
        case .years: PhotoPeriod.years(newestYear: span.newestYear, oldestYear: span.oldestYear).compactMap { PhotoPeriod(year: $0) }
        case .months: PhotoPeriod.months(newestYear: span.newestYear, oldestYear: span.oldestYear)
        }
    }

    var body: some View {
        content
            .task(id: model.overviewVersion) {
                reader = model.overviewReader(for: listKey)
                await reader?.readYearSpan()
            }
    }

    @ViewBuilder
    private var content: some View {
        if let reader, let span = reader.yearSpan {
            LazyVStack(spacing: 1) {
                ForEach(periods(in: span)) { period in
                    PhotoPeriodTile(period: period, kind: kind, reader: reader, model: model, onOpen: onOpen)
                }
            }
        } else if let failureText = reader?.failureText {
            PhotosFailedView(failureText: failureText) {
                Task { await reader?.readYearSpan() }
            }
            .frame(maxWidth: .infinity, minHeight: 320)
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, minHeight: 320)
        }
    }
}

/// PhotoPeriodTile is one month or year: a picture across the width, with
/// the name over a shade at the bottom left and how many photos it holds under
/// it. It asks for its period when it appears, and shows a plain square, with
/// its name, until the answer comes.
struct PhotoPeriodTile: View {
    let period: PhotoPeriod
    let kind: PhotoOverviewList.Kind
    let reader: PhotoOverviewReader
    let model: PhotosModel
    let onOpen: (PhotoPeriod) -> Void

    private var titleText: String { period.titleText() ?? String(localized: "Unknown date") }

    var body: some View {
        switch reader.readings[period.id] {
        case .empty:
            EmptyView()
        case .summary(let summary):
            tile(summary: summary)
        case nil:
            tile(summary: nil)
                .task(id: ObjectIdentifier(reader)) { await reader.read(period) }
        }
    }

    private func tile(summary: PhotoPeriodSummary?) -> some View {
        Button {
            onOpen(period)
        } label: {
            Color(.secondarySystemBackground)
                .aspectRatio(kind.aspectRatio, contentMode: .fit)
                .overlay {
                    if let summary {
                        PhotoThumbnailImage(photo: summary.coverPhoto, model: model, isLarge: true)
                    }
                }
                .overlay(alignment: .bottomLeading) {
                    // Light text on a shade, whatever the picture under it is.
                    VStack(alignment: .leading, spacing: 2) {
                        Text(titleText)
                            .font(kind.titleFont)
                            .lineLimit(2)
                        if let summary {
                            Text(PhotoAlbumWords.photoCount(summary.photoCount))
                                .font(.subheadline)
                        }
                    }
                    .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                    .foregroundStyle(.white)
                    .shadow(color: .black.opacity(0.5), radius: 3)
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(LinearGradient(colors: [.clear, .black.opacity(0.45)], startPoint: .top, endPoint: .bottom))
                }
                .clipped()
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(summary.map { "\(titleText), \(PhotoAlbumWords.photoCount($0.photoCount))" } ?? titleText)
    }
}

#if DEBUG
    #Preview("Scale bar, light") {
        PhotoScaleBar(scale: .constant(.months))
            .padding()
            .preferredColorScheme(.light)
    }
    #Preview("Scale bar, dark") {
        PhotoScaleBar(scale: .constant(.all))
            .padding()
            .preferredColorScheme(.dark)
    }
#endif
