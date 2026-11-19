import AVKit
import SwiftUI
import TeaNodeKit

// The pictures and videos of a feed card (docs/planning/agent-feed-execplan.md,
// "Pictures and videos"), the small line above a headline, and the header of a
// stack opened in place. Pictures come through the app's own signed-in loader
// (AppModel.picture); a video asks the server for a playable address, as the
// Photos viewer does.

// MARK: One picture

/// FeedMediaImage is a picture the server keeps, read with the person's
/// session. It fills the width it is given; the caller sets the shape with an
/// aspect ratio. Until it is read, and when it cannot be, it is an empty fill.
struct FeedMediaImage: View {
    let path: String
    @Environment(AppModel.self) private var app
    @State private var picture: UIImage?

    var body: some View {
        Color(.tertiarySystemFill)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipped()
            .task(id: path) {
                picture = await app.picture(path)
            }
    }
}

// MARK: One item

/// FeedMediaTile is one picture or video of a card, with its caption under it
/// in secondary text. A video shows a play button and plays in place; a
/// picture of a web page opens the page; a photo has nothing to open.
struct FeedMediaTile: View {
    let media: AgentFeedMedia
    /// Width over height the picture is cropped to.
    let aspectRatio: Double
    var cornerRadius: CGFloat = 0
    /// How far the caption is kept from the sides, for a picture that runs to
    /// the card's edges.
    var captionInset: CGFloat = 0
    @Environment(\.openURL) private var openURL
    @State private var isPlaying = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            stage
                .clipShape(.rect(cornerRadius: cornerRadius, style: .continuous))
            if !media.mediaCaption.isEmpty {
                Text(media.mediaCaption)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, captionInset)
            }
        }
    }

    /// Whether pressing the picture does something.
    private var canPress: Bool {
        switch media.mediaKind {
        case .video: media.videoPhotoId != nil
        case .picture: media.linkAddress != nil
        case .photo: false
        }
    }

    @ViewBuilder
    private var stage: some View {
        if isPlaying {
            FeedInlineVideo(media: media, aspectRatio: aspectRatio)
        } else if canPress {
            Button {
                press()
            } label: {
                still
            }
            .buttonStyle(.plain)
            .accessibilityHint(pressHint)
        } else {
            still
        }
    }

    private var still: some View {
        ZStack {
            FeedMediaImage(path: media.imageUrl)
                .aspectRatio(CGFloat(aspectRatio), contentMode: .fit)
            if media.mediaKind == .video {
                Image(systemName: "play.fill")
                    .font(.title2)
                    .padding(16)
                    .glassEffect(.regular, in: .circle)
                    .accessibilityHidden(true)
            }
        }
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(stillLabel))
        .accessibilityAddTraits(media.mediaKind == .video ? AccessibilityTraits.isButton : AccessibilityTraits.isImage)
    }

    /// The caption, else what the picture is.
    private var stillLabel: String {
        if !media.mediaCaption.isEmpty { return media.mediaCaption }
        switch media.mediaKind {
        case .video: return String(localized: "Video")
        case .photo: return String(localized: "Photo")
        case .picture: return String(localized: "Picture from the page")
        }
    }

    private var pressHint: Text {
        media.mediaKind == .video ? Text("Plays the video") : Text("Opens the page")
    }

    private func press() {
        if media.mediaKind == .video {
            isPlaying = true
        } else if let address = media.linkAddress {
            openURL(address)
        }
    }
}

// MARK: Playing a video

/// FeedInlineVideo plays one of the person's own videos in place of its
/// poster. The player reads from a fresh address that opens without the app's
/// sign-in and serves byte ranges, as the Photos viewer's does.
struct FeedInlineVideo: View {
    let media: AgentFeedMedia
    let aspectRatio: Double
    @Environment(AppModel.self) private var app
    @State private var player: AVPlayer?
    @State private var isFailed = false

    var body: some View {
        ZStack {
            Color(.tertiarySystemFill)
            content
        }
        .aspectRatio(CGFloat(aspectRatio), contentMode: .fit)
        .task { await start() }
        .onDisappear { player?.pause() }
    }

    @ViewBuilder
    private var content: some View {
        if let player {
            VideoPlayer(player: player)
        } else if isFailed {
            Label("Cannot play this video", systemImage: "exclamationmark.circle")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .padding()
        } else {
            ProgressView()
        }
    }

    private func start() async {
        guard let photoId = media.videoPhotoId, let connection = app.connection else {
            isFailed = true
            return
        }
        do {
            let address = try await connection.photoPlaybackAddress(photoId, source: .preview("video"))
            guard !Task.isCancelled else { return }
            VideoAudioSession.followSilentSwitch()
            let readyPlayer = AVPlayer(url: address)
            player = readyPlayer
            readyPlayer.play()
        } catch {
            if !Task.isCancelled { isFailed = true }
        }
    }
}

// MARK: A card's pictures

/// FeedCardMedia is the pictures at the top of a card. One item runs the
/// card's whole width, 16:9 (4:5 for a portrait photo) and is cut to the
/// card's corners by the card. Two to four are a grid; more are a row that
/// scrolls sideways.
struct FeedCardMedia: View {
    let mediaItems: [AgentFeedMedia]

    var body: some View {
        if mediaItems.count == 1, let only = mediaItems.first {
            FeedMediaTile(media: only, aspectRatio: only.singleAspectRatio, captionInset: 14)
        } else if mediaItems.count > 1 {
            FeedMediaGroup(mediaItems: mediaItems)
                .padding([.horizontal, .top], 14)
        }
    }
}

/// FeedMediaGroup is several pictures of a card, side by side.
struct FeedMediaGroup: View {
    let mediaItems: [AgentFeedMedia]

    private static let gridSpacing: CGFloat = 6
    private static let scrolledTileWidth: CGFloat = 220

    var body: some View {
        if mediaItems.count <= 4 {
            grid
        } else {
            row
        }
    }

    private var grid: some View {
        LazyVGrid(
            columns: [GridItem(.flexible(), spacing: Self.gridSpacing), GridItem(.flexible(), spacing: Self.gridSpacing)],
            alignment: .leading, spacing: Self.gridSpacing
        ) {
            ForEach(mediaItems) { media in
                FeedMediaTile(media: media, aspectRatio: 1, cornerRadius: 10)
            }
        }
    }

    private var row: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            LazyHStack(alignment: .top, spacing: 8) {
                ForEach(mediaItems) { media in
                    FeedMediaTile(media: media, aspectRatio: 4.0 / 3.0, cornerRadius: 10)
                        .frame(width: Self.scrolledTileWidth)
                }
            }
        }
    }
}

// MARK: The header line

/// FeedHeaderLine is the small line above a headline, as a news app labels a
/// story: the card's symbol and the site or source it is from.
struct FeedHeaderLine: View {
    let pictureKind: AgentFeedPictureKind
    let headerText: String

    var body: some View {
        HStack(spacing: 6) {
            symbolOrEmoji
                .accessibilityHidden(true)
            Text(headerText)
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }

    @ViewBuilder
    private var symbolOrEmoji: some View {
        switch pictureKind {
        case .symbol(let name):
            Image(systemName: UIImage(systemName: name) == nil ? AgentFeedPictureKind.fallbackSymbolName : name)
                .font(.caption)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
        case .emoji(let text):
            Text(text).font(.caption)
        case .fallback:
            Image(systemName: AgentFeedPictureKind.fallbackSymbolName)
                .font(.caption)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
        }
    }
}

// MARK: A stack opened in place

/// FeedStackHeader is the top of a stack opened in place: its name and how
/// many cards it holds, and "Show Less", which gathers the cards back into
/// the deck. Pressing the name does the same.
struct FeedStackHeader: View {
    let title: String
    let cardCount: Int
    let onGather: () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if dynamicTypeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 8) {
                titleButton
                showLessButton
            }
        } else {
            HStack(spacing: 12) {
                titleButton
                showLessButton
            }
        }
    }

    private var titleButton: some View {
        Button {
            onGather()
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.headline)
                    .foregroundStyle(Color.primary)
                    .multilineTextAlignment(.leading)
                Text("\(cardCount) cards")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityHint(Text("Gathers the cards back into a stack"))
    }

    private var showLessButton: some View {
        Button {
            onGather()
        } label: {
            Label("Show Less", systemImage: "chevron.up")
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
    }
}
