import AVFoundation
import Observation
import SwiftUI
import TeaNodeKit

/// RecordingPlayer plays a recording's originals from any point on its timeline. It fetches an original only when the
/// person first plays, checks it against the digest the server verified, and deletes the copy when the screen goes.
@MainActor
@Observable
final class RecordingPlayer {
    let connection: Connection
    private(set) var mediaFiles: [RecordingMedia] = []
    private(set) var durationMS: Int64
    private(set) var isPlaying = false
    private(set) var isPreparing = false
    private(set) var timelineMS: Int64 = 0
    /// True from the first play until the person stops it. Pausing keeps it, so the floating player stays to play on.
    private(set) var hasPlaybackSession = false
    /// Where a bubble played on its own stops on the timeline. Nil when playing is not bounded.
    private(set) var boundTimelineMS: Int64?
    var problemText: String?
    private var player: AVPlayer?
    private var loadedMedia: RecordingMedia?
    private var localURL: URL?
    private var timeObserver: Any?
    private var endObserver: (any NSObjectProtocol)?
    private var recordingUserId: String?

    init(connection: Connection, durationMS: Int64) {
        self.connection = connection
        self.durationMS = durationMS
    }

    var canPlay: Bool { !mediaFiles.isEmpty }

    func update(mediaFiles: [RecordingMedia], durationMS: Int64) {
        self.mediaFiles = RecordingTimelinePieces.playable(mediaFiles)
        self.durationMS = durationMS
    }

    func toggle() async {
        if isPlaying {
            player?.pause()
            isPlaying = false
        } else if let boundTimelineMS, timelineMS < boundTimelineMS {
            // A bubble paused part way plays on to the same end.
            await play(fromTimelineMS: timelineMS, untilTimelineMS: boundTimelineMS)
        } else {
            await play(fromTimelineMS: timelineMS)
        }
    }

    /// Plays one stretch of the timeline, such as a conversation bubble, and pauses at its end.
    func playRange(fromTimelineMS rangeStartMS: Int64, untilTimelineMS rangeEndMS: Int64) async {
        guard RecordingTimelinePieces.locateRange(rangeStartMS: rangeStartMS, rangeEndMS: rangeEndMS, in: mediaFiles) != nil else { return }
        await play(fromTimelineMS: rangeStartMS, untilTimelineMS: rangeEndMS)
    }

    func play(fromTimelineMS target: Int64, untilTimelineMS rangeEndMS: Int64? = nil) async {
        // One timeline over every original: the position picks the original and the offset into it, and a position in a gap
        // between two originals is the start of the next.
        guard !isPreparing, let location = RecordingTimelinePieces.locate(timelineMS: target, in: mediaFiles) else { return }
        let media = location.media
        // The end is in the same original as the start: a stretch that runs past it stops there.
        let range = rangeEndMS.flatMap { RecordingTimelinePieces.locateRange(rangeStartMS: target, rangeEndMS: $0, in: mediaFiles) }
        if rangeEndMS != nil, range == nil { return }
        isPreparing = true
        defer { isPreparing = false }
        do {
            if loadedMedia?.id != media.id { try await load(media) }
            guard let player else { return }
            if !MeetingRecorder.shared.hasSession {
                try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .spokenAudio)
                try? AVAudioSession.sharedInstance().setActive(true)
            }
            let offsetMS = location.offsetMS
            _ = await player.seek(to: CMTime(value: offsetMS, timescale: 1000), toleranceBefore: .zero, toleranceAfter: .zero)
            player.currentItem?.forwardPlaybackEndTime = range.map { CMTime(value: $0.endOffsetMS, timescale: 1000) } ?? .invalid
            boundTimelineMS = range?.endTimelineMS
            player.play()
            isPlaying = true
            hasPlaybackSession = true
            timelineMS = media.timelineStartMS + offsetMS
            problemText = nil
        } catch {
            if !Task.isCancelled { problemText = error.localizedDescription }
        }
    }

    /// Moves to a point on the timeline, and goes on playing from there if it was playing.
    func seek(toTimelineMS target: Int64) async {
        timelineMS = RecordingTimelinePieces.locate(timelineMS: target, in: mediaFiles)?.timelineMS ?? target
        boundTimelineMS = nil
        if isPlaying { await play(fromTimelineMS: target) }
    }

    func stop() {
        removePlayer()
        isPlaying = false
        hasPlaybackSession = false
        boundTimelineMS = nil
        if !MeetingRecorder.shared.hasSession {
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        }
    }

    private func load(_ media: RecordingMedia) async throws {
        removePlayer()
        recordingUserId = AppModel.shared.account?.cachedRecordingUserId
        try requireCurrentConnection()
        let request = try await connection.recordingPlaybackRequest(mediaId: media.id)
        var fileURL = try await Self.downloadVerifiedMedia(
            request, expectedSizeBytes: media.originalSizeBytes, expectedSha256: media.originalSha256,
            fileExtension: (media.originalFilename as NSString).pathExtension)
        var isKept = false
        defer { if !isKept { try? FileManager.default.removeItem(at: fileURL) } }
        try requireCurrentConnection()
        var asset = AVURLAsset(url: fileURL)
        let isOriginalPlayable: Bool
        do {
            isOriginalPlayable = try await asset.load(.isPlayable)
        } catch let assetError as AVError where [.fileFormatNotRecognized, .decoderNotFound, .formatUnsupported].contains(assetError.code) {
            isOriginalPlayable = false
        }
        try requireCurrentConnection()
        if let playback = try await connection.compatibleRecordingPlayback(for: media, isOriginalPlayable: isOriginalPlayable) {
            try requireCurrentConnection()
            let previewRequest = try await connection.recordingCompatiblePlaybackRequest(playback, for: media)
            let previewURL = try await Self.downloadVerifiedMedia(
                previewRequest, expectedSizeBytes: playback.playbackSizeBytes, expectedSha256: playback.playbackSha256, fileExtension: "wav")
            try? FileManager.default.removeItem(at: fileURL)
            fileURL = previewURL
            asset = AVURLAsset(url: previewURL)
            guard try await asset.load(.isPlayable) else { throw TeaNodeError.unreadableAnswer }
        }
        try requireCurrentConnection()
        let item = AVPlayerItem(asset: asset)
        let preparedPlayer = AVPlayer(playerItem: item)
        timeObserver = preparedPlayer.addPeriodicTimeObserver(forInterval: CMTime(value: 1, timescale: 4), queue: .main) { [weak self] time in
            let seconds = time.seconds
            Task { @MainActor in self?.playerTimeChanged(seconds: seconds) }
        }
        endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) {
            [weak self] _ in
            Task { @MainActor in await self?.playbackEnded() }
        }
        player = preparedPlayer
        localURL = fileURL
        loadedMedia = media
        isKept = true
    }

    private func playerTimeChanged(seconds: Double) {
        guard let loadedMedia, seconds.isFinite, isPlaying else { return }
        timelineMS = loadedMedia.timelineStartMS + Int64(seconds * 1000)
    }

    /// An original ended: the next one carries on from its start, which skips any gap between them, or playing stops.
    private func playbackEnded() async {
        // A bubble stops at its end, paused there, and does not go on into the next original.
        if let boundTimelineMS {
            isPlaying = false
            timelineMS = boundTimelineMS
            return
        }
        guard let loadedMedia, let next = RecordingTimelinePieces.original(after: loadedMedia, in: mediaFiles) else {
            isPlaying = false
            return
        }
        await play(fromTimelineMS: next.timelineStartMS)
    }

    private func removePlayer() {
        if let timeObserver { player?.removeTimeObserver(timeObserver) }
        timeObserver = nil
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = nil
        player?.pause()
        player = nil
        if let localURL { try? FileManager.default.removeItem(at: localURL) }
        localURL = nil
        loadedMedia = nil
    }

    private func requireCurrentConnection() throws {
        try Task.checkCancellation()
        guard AppModel.shared.connection === connection, let recordingUserId,
            RecordingLibraryProgress.isCurrentOwner(
                accountKey: connection.accountKey, recordingUserId: recordingUserId,
                currentAccountKey: AppModel.shared.connection?.accountKey,
                currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId),
            (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == recordingUserId
        else { throw TeaNodeError.signedOut }
    }

    nonisolated private static func downloadVerifiedMedia(
        _ request: URLRequest, expectedSizeBytes: Int64, expectedSha256: String, fileExtension: String
    ) async throws -> URL {
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let (temporary, response) = try await session.download(for: request)
        defer { try? FileManager.default.removeItem(at: temporary) }
        let verifiedDigest = try await Task.detached { try MeetingRecorder.digest(temporary) }.value
        guard (response as? HTTPURLResponse)?.statusCode == 200,
            Int64(try temporary.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0) == expectedSizeBytes,
            verifiedDigest == expectedSha256
        else { throw TeaNodeError.unreadableAnswer }
        let destination = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathExtension(fileExtension)
        try FileManager.default.moveItem(at: temporary, to: destination)
        return destination
    }
}

/// RecordingMiniPlayerView is the compact player: play or pause, a bar to move along the recording, and the time.
struct RecordingMiniPlayerView: View {
    let canPlay: Bool
    let isPlaying: Bool
    let isPreparing: Bool
    let timelineMS: Int64
    let durationMS: Int64
    let onToggle: @MainActor () -> Void
    let onSeek: @MainActor (Int64) -> Void
    @State private var scrubbedMS: Double?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        // At the largest text sizes the button goes above the slider, so the two times have the card's whole width and an
        // hour-long length does not break across lines.
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12)) : AnyLayout(HStackLayout(spacing: 12))
        layout {
            Button(action: onToggle) {
                if isPreparing {
                    ProgressView()
                } else {
                    Image(systemName: isPlaying ? "pause.fill" : "play.fill").contentTransition(.symbolEffect(.replace))
                }
            }
            .buttonStyle(.bordered)
            .buttonBorderShape(.circle)
            .controlSize(.large)
            .disabled(!canPlay || isPreparing)
            .accessibilityLabel(isPlaying ? String(localized: "Pause") : String(localized: "Play"))
            VStack(spacing: 2) {
                Slider(
                    value: Binding(get: { scrubbedMS ?? Double(min(timelineMS, durationMS)) }, set: { scrubbedMS = $0 }),
                    in: 0...Double(max(durationMS, 1))
                ) { isEditing in
                    guard !isEditing, let scrubbedMS else { return }
                    onSeek(Int64(scrubbedMS))
                    self.scrubbedMS = nil
                }
                .disabled(!canPlay)
                .accessibilityLabel("Position in the recording")
                HStack {
                    Text(RecordingDuration.text(Int64(scrubbedMS ?? Double(timelineMS)))).fixedSize()
                    Spacer()
                    Text(RecordingDuration.text(durationMS)).fixedSize()
                }
                .font(.caption).monospacedDigit().foregroundStyle(.secondary)
            }
        }
        .padding(12)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator).opacity(0.5), lineWidth: 0.5)
        }
    }
}

/// RecordingFloatingPlayerView is the small bar over the foot of the screen while the recording plays, so a bubble that was
/// started can be paused or stopped from wherever the transcript is scrolled to.
struct RecordingFloatingPlayerView: View {
    let isPlaying: Bool
    let isPreparing: Bool
    let timelineMS: Int64
    let onToggle: @MainActor () -> Void
    let onStop: @MainActor () -> Void

    var body: some View {
        HStack(spacing: 8) {
            Button(action: onToggle) {
                if isPreparing {
                    ProgressView()
                } else {
                    Image(systemName: isPlaying ? "pause.fill" : "play.fill").contentTransition(.symbolEffect(.replace))
                }
            }
            .disabled(isPreparing)
            .accessibilityLabel(isPlaying ? String(localized: "Pause") : String(localized: "Play"))
            Text(RecordingDuration.text(timelineMS))
                .font(.subheadline).monospacedDigit()
                .accessibilityLabel(Text("Position in the recording"))
                .accessibilityValue(Text(RecordingDuration.text(timelineMS)))
            Button(action: onStop) { Image(systemName: "xmark") }
                .accessibilityLabel(String(localized: "Stop playback"))
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .glassEffect(.regular, in: .capsule)
        .padding(.bottom, 8)
    }
}

#Preview("Player paused, light") {
    RecordingMiniPlayerView(
        canPlay: true, isPlaying: false, isPreparing: false, timelineMS: 125_000, durationMS: 754_000, onToggle: {}, onSeek: { _ in }
    ).padding().preferredColorScheme(.light)
}
#Preview("Player playing, dark") {
    RecordingMiniPlayerView(
        canPlay: true, isPlaying: true, isPreparing: false, timelineMS: 125_000, durationMS: 754_000, onToggle: {}, onSeek: { _ in }
    ).padding().preferredColorScheme(.dark)
}
#Preview("Player still uploading, light") {
    RecordingMiniPlayerView(
        canPlay: false, isPlaying: false, isPreparing: false, timelineMS: 0, durationMS: 754_000, onToggle: {}, onSeek: { _ in }
    ).padding().preferredColorScheme(.light)
}
#Preview("Floating player playing, light") {
    RecordingFloatingPlayerView(isPlaying: true, isPreparing: false, timelineMS: 125_000, onToggle: {}, onStop: {})
        .padding().preferredColorScheme(.light)
}
#Preview("Floating player paused, dark") {
    RecordingFloatingPlayerView(isPlaying: false, isPreparing: false, timelineMS: 125_000, onToggle: {}, onStop: {})
        .padding().preferredColorScheme(.dark)
}
