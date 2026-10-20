import SwiftUI
import TeaNodeKit
import UIKit

/// PhotoScaleBar floats over the bottom of the library: the switch of Years,
/// Months and All in one capsule. Glass, because it floats over the photos.
/// The library is searched from the Search tab, whose button is in the tab bar
/// just below, so the bar has no search of its own.
struct PhotoScaleBar: View {
    @Binding var scale: PhotoScale
    @Environment(\.accessibilityReduceMotion) private var isReduceMotionEnabled

    private var switchAnimation: Animation? {
        isReduceMotionEnabled ? nil : Animation.snappy
    }

    var body: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 12) {
                // There is no floating segmented control in the system, so this is drawn: three
                // buttons in a glass capsule, the one chosen marked with the accent.
                HStack(spacing: 0) {
                    ForEach(PhotoScale.allCases) { option in
                        segment(option)
                    }
                }
                .padding(4)
                .glassEffect(.regular, in: .capsule)
                .animation(switchAnimation, value: scale)
            }
        }
    }

    private func segment(_ option: PhotoScale) -> some View {
        let isChosen = option == scale
        return Button {
            scale = option
        } label: {
            Text(option.title)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(isChosen ? Color.accentColor : Color.primary)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .frame(maxWidth: .infinity, minHeight: 44)
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

/// PhotoOverviewItem is one month or one year in the library's Months and
/// Years: its name, the photo that stands for it, and the newest photo in it,
/// which the grid is scrolled to when it is pressed.
struct PhotoOverviewItem: Identifiable {
    let id: String
    let coverPhoto: Photo?
    let title: String
    let newestPhotoId: String?

    init(month: PhotoMonth) {
        id = month.id
        coverPhoto = month.coverPhoto
        title = month.titleText() ?? String(localized: "Unknown date")
        newestPhotoId = month.photos.first?.id
    }

    init(year: PhotoYear) {
        id = year.id
        coverPhoto = year.coverPhoto
        title = year.titleText() ?? String(localized: "Unknown date")
        newestPhotoId = year.photos.first?.id
    }
}

/// PhotoOverviewList is the Months and Years of the library: a large picture
/// for each, edge to edge, with its name over the bottom. Only the photos read
/// so far are in it; while the last one is on screen the next page is read.
struct PhotoOverviewList: View {
    let items: [PhotoOverviewItem]
    let aspectRatio: CGFloat
    let titleFont: Font
    let listing: PhotoListing
    let listKey: PhotoListKey
    let model: PhotosModel
    let onOpen: (PhotoOverviewItem) -> Void
    @State private var isLastItemShown = false

    /// Changes whenever another page has been read while the last picture is on
    /// screen, which asks for the page after it; nil while it is not on screen.
    private var loadTrigger: Int? { isLastItemShown ? listing.photos.count : nil }

    var body: some View {
        LazyVStack(spacing: 1) {
            ForEach(items) { item in
                PhotoOverviewTile(item: item, aspectRatio: aspectRatio, titleFont: titleFont, model: model) {
                    onOpen(item)
                }
                .onAppear {
                    if item.id == items.last?.id { isLastItemShown = true }
                }
                .onDisappear {
                    if item.id == items.last?.id { isLastItemShown = false }
                }
            }
        }
        .task(id: loadTrigger) {
            guard loadTrigger != nil, let lastPhotoId = listing.photos.last?.id else { return }
            await model.loadMoreIfNeeded(showing: lastPhotoId, in: listKey)
        }
    }
}

/// PhotoOverviewTile is one month or year: a picture across the width, with
/// the name over a shade at the bottom left.
struct PhotoOverviewTile: View {
    let item: PhotoOverviewItem
    let aspectRatio: CGFloat
    let titleFont: Font
    let model: PhotosModel
    let onOpen: () -> Void

    var body: some View {
        Button(action: onOpen) {
            Color(.secondarySystemBackground)
                .aspectRatio(aspectRatio, contentMode: .fit)
                .overlay {
                    if let coverPhoto = item.coverPhoto {
                        PhotoThumbnailImage(photo: coverPhoto, model: model, isLarge: true)
                    }
                }
                .overlay(alignment: .bottomLeading) {
                    // Light text on a shade, whatever the picture under it is.
                    Text(item.title)
                        .font(titleFont)
                        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                        .foregroundStyle(.white)
                        .shadow(color: .black.opacity(0.5), radius: 3)
                        .lineLimit(2)
                        .padding(16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(LinearGradient(colors: [.clear, .black.opacity(0.45)], startPoint: .top, endPoint: .bottom))
                }
                .clipped()
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(item.title)
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
