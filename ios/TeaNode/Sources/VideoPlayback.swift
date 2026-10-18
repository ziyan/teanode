import AVFoundation
import SwiftUI
import TeaNodeKit

/// VideoAudioSession sets how a video in the photo viewer sounds. While the
/// person is browsing the video follows the ring and silent switch, as the
/// Photos app does: on silent it plays without sound. Unmuting is the person
/// asking to hear it, so then it sounds whatever the switch says.
enum VideoAudioSession {
    /// Plays with the ring and silent switch, and lets music already playing
    /// carry on.
    static func followSilentSwitch() {
        setCategory(.ambient)
    }

    /// Plays whatever the switch says.
    static func ignoreSilentSwitch() {
        setCategory(.playback)
    }

    private static func setCategory(_ category: AVAudioSession.Category) {
        let session = AVAudioSession.sharedInstance()
        // A meeting being recorded needs the session as the recorder set it:
        // taking the category away would stop the recording.
        guard session.category != .playAndRecord else { return }
        try? session.setCategory(category)
    }
}

/// VideoPlayback is the player of the video on screen in the photo viewer, and
/// what the controls over it show: how far it has played, whether it is
/// playing, whether it has ended, whether it is muted.
@MainActor
@Observable
final class VideoPlayback {
    let player: AVPlayer
    private(set) var elapsedSeconds: Double = 0
    private(set) var durationSeconds: Double
    private(set) var isPlaying = false
    private(set) var hasEnded = false
    private(set) var isMuted = false
    /// Whether a finger is on the progress bar. The video waits while it is,
    /// and the controls stay.
    private(set) var isScrubbing = false
    /// Not shown by any view, so that changing them redraws nothing.
    @ObservationIgnored private var wasPlayingBeforeScrubbing = false
    @ObservationIgnored private var timeObserver: Any?

    /// How often the position is read while the video plays.
    private static let positionIntervalSeconds: Double = 0.1
    private static let timeScale: CMTimeScale = 600

    /// `fallbackDurationSeconds` is the length the server knows, shown until
    /// the player has read the file's own.
    init(url: URL, fallbackDurationSeconds: Double) {
        player = AVPlayer(url: url)
        durationSeconds = fallbackDurationSeconds
        VideoAudioSession.followSilentSwitch()
        isMuted = player.isMuted
        let interval = CMTime(seconds: Self.positionIntervalSeconds, preferredTimescale: Self.timeScale)
        timeObserver = player.addPeriodicTimeObserver(forInterval: interval, queue: .main) { [weak self] _ in
            // The queue is the main one, which the compiler is not told.
            MainActor.assumeIsolated {
                guard let self else { return }
                self.readPlayer()
            }
        }
    }

    /// Pauses the video and lets go of the player. Called when the page stops
    /// being the one on screen, since a time observer stays on the player
    /// until it is taken off.
    func stop() {
        player.pause()
        if let timeObserver {
            player.removeTimeObserver(timeObserver)
            self.timeObserver = nil
        }
        player.replaceCurrentItem(with: nil)
        isPlaying = false
    }

    /// Plays the video, from the beginning when it has ended.
    func play() {
        if hasEnded || VideoControls.isAtEnd(elapsedSeconds: elapsedSeconds, durationSeconds: durationSeconds) {
            player.seek(to: .zero, toleranceBefore: .zero, toleranceAfter: .zero)
            elapsedSeconds = 0
            hasEnded = false
        }
        player.play()
        isPlaying = true
    }

    func pause() {
        player.pause()
        isPlaying = false
    }

    func togglePlaying() {
        if isPlaying { pause() } else { play() }
    }

    /// Mutes the video, or unmutes it, which also lets it sound with the
    /// ring and silent switch on silent.
    func toggleMuted() {
        player.isMuted.toggle()
        isMuted = player.isMuted
        if !isMuted { VideoAudioSession.ignoreSilentSwitch() }
    }

    /// The video came to its last frame: it stays there, and Play starts it
    /// again.
    func didReachEnd() {
        hasEnded = true
        isPlaying = false
        if durationSeconds > 0 { elapsedSeconds = durationSeconds }
    }

    // MARK: The progress bar

    /// A finger came down on the progress bar: the video waits while it is
    /// moved, and goes on afterwards if it was playing.
    func beginScrubbing() {
        guard !isScrubbing else { return }
        isScrubbing = true
        wasPlayingBeforeScrubbing = isPlaying
        hasEnded = false
        player.pause()
    }

    /// The finger moved to `fraction` of the bar: show that frame. A seek that
    /// need not land on the exact frame follows the finger faster.
    func scrub(toFraction fraction: Double) {
        elapsedSeconds = VideoControls.seconds(atFraction: fraction, durationSeconds: durationSeconds)
        player.seek(to: CMTime(seconds: elapsedSeconds, preferredTimescale: Self.timeScale))
    }

    /// The finger came up at `fraction` of the bar: land on that frame, and
    /// play on if the video was playing.
    func endScrubbing(atFraction fraction: Double) {
        elapsedSeconds = VideoControls.seconds(atFraction: fraction, durationSeconds: durationSeconds)
        player.seek(to: CMTime(seconds: elapsedSeconds, preferredTimescale: Self.timeScale), toleranceBefore: .zero, toleranceAfter: .zero)
        isScrubbing = false
        if wasPlayingBeforeScrubbing { play() }
    }

    /// Moves `stepSeconds` on or back, for VoiceOver's adjust gesture.
    func skip(bySeconds stepSeconds: Double) {
        let target = VideoControls.seconds(from: elapsedSeconds, movingBy: stepSeconds, durationSeconds: durationSeconds)
        elapsedSeconds = target
        hasEnded = false
        player.seek(to: CMTime(seconds: target, preferredTimescale: Self.timeScale), toleranceBefore: .zero, toleranceAfter: .zero)
    }

    /// Shows the first frame of the video at `seconds`, paused. The demo
    /// screenshot uses it, so that the controls are not hidden by the time the
    /// picture is taken.
    func pause(atSeconds seconds: Double) {
        pause()
        elapsedSeconds = seconds
        player.seek(to: CMTime(seconds: seconds, preferredTimescale: Self.timeScale), toleranceBefore: .zero, toleranceAfter: .zero)
    }

    // MARK: Reading the player

    /// Takes the position, the length, whether it is playing and whether it is
    /// muted from the player, which also changes without being asked: a phone
    /// call pauses it, and the length is only known once the file is read.
    private func readPlayer() {
        if let item = player.currentItem {
            let itemSeconds = item.duration.seconds
            if itemSeconds.isFinite, itemSeconds > 0, itemSeconds != durationSeconds { durationSeconds = itemSeconds }
        }
        if player.isMuted != isMuted { isMuted = player.isMuted }
        guard !isScrubbing, !hasEnded else { return }
        let playerSeconds = player.currentTime().seconds
        if playerSeconds.isFinite {
            elapsedSeconds = durationSeconds > 0 ? min(max(0, playerSeconds), durationSeconds) : max(0, playerSeconds)
        }
        // A video waiting for more of the file to arrive is still playing as far as
        // the person is concerned: the controls must not flicker between the two.
        let isPlayerPlaying = player.timeControlStatus != .paused
        if isPlayerPlaying != isPlaying { isPlaying = isPlayerPlaying }
    }
}

/// VideoLayerView shows a player's video and nothing else: the controls are
/// drawn by SwiftUI over it, as the Photos app does, not by the system player.
struct VideoLayerView: UIViewRepresentable {
    let player: AVPlayer

    func makeUIView(context: Context) -> VideoLayerUIView {
        let view = VideoLayerUIView()
        view.playerLayer.player = player
        return view
    }

    func updateUIView(_ view: VideoLayerUIView, context: Context) {
        if view.playerLayer.player !== player { view.playerLayer.player = player }
    }
}

/// VideoLayerUIView is a view with a player layer filling it. The video fits
/// inside the view whole, whatever shape it is.
final class VideoLayerUIView: UIView {
    let playerLayer = AVPlayerLayer()

    override init(frame: CGRect) {
        super.init(frame: frame)
        configure()
    }

    required init?(coder: NSCoder) {
        super.init(coder: coder)
        configure()
    }

    private func configure() {
        backgroundColor = .clear
        playerLayer.videoGravity = .resizeAspect
        layer.addSublayer(playerLayer)
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        // A layer that is not a view's own moves with an animation unless it is told not to.
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        playerLayer.frame = bounds
        CATransaction.commit()
    }
}

/// VideoControlsBar floats over the bottom of a video: play or pause, how far
/// it has played with a bar that can be dragged, and mute. It is glass over
/// the picture and always dark, since the viewer is black in both appearances.
struct VideoControlsBar: View {
    let playback: VideoPlayback
    /// Called when the person uses a control, so that the controls stay for
    /// another few seconds.
    let onUse: () -> Void

    private static let touchTargetPoints: CGFloat = 44

    var body: some View {
        HStack(spacing: 4) {
            Button {
                playback.togglePlaying()
                onUse()
            } label: {
                Image(systemName: playSymbol)
                    .font(.title3)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(width: Self.touchTargetPoints, height: Self.touchTargetPoints)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(playLabel)

            VideoProgressBar(playback: playback, onUse: onUse)

            Button {
                playback.toggleMuted()
                onUse()
            } label: {
                Image(systemName: playback.isMuted ? "speaker.slash.fill" : "speaker.wave.2.fill")
                    .font(.title3)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(width: Self.touchTargetPoints, height: Self.touchTargetPoints)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(muteLabel)
        }
        .padding(.horizontal, 8)
        .glassEffect(.regular, in: .capsule)
        .environment(\.colorScheme, .dark)
    }

    private var playSymbol: String {
        playback.isPlaying ? "pause.fill" : "play.fill"
    }

    private var playLabel: LocalizedStringKey {
        if playback.hasEnded { return "Replay" }
        if playback.isPlaying { return "Pause" }
        return "Play"
    }

    private var muteLabel: LocalizedStringKey {
        if playback.isMuted { return "Unmute" }
        return "Mute"
    }
}

/// VideoProgressBar is a capsule that fills as the video plays. Touch it or
/// drag along it to go to that moment of the video.
struct VideoProgressBar: View {
    let playback: VideoPlayback
    let onUse: () -> Void
    @Environment(\.accessibilityReduceMotion) private var isReduceMotionEnabled

    private static let touchTargetPoints: CGFloat = 44
    private static let trackHeightPoints: CGFloat = 6
    private static let scrubbingTrackHeightPoints: CGFloat = 12

    var body: some View {
        GeometryReader { proxy in
            let width = max(proxy.size.width, 1)
            ZStack(alignment: .leading) {
                Capsule()
                    .fill(Color.primary.opacity(0.3))
                Capsule()
                    .fill(Color.primary)
                    .frame(width: width * playedFraction)
            }
            .frame(height: playback.isScrubbing ? Self.scrubbingTrackHeightPoints : Self.trackHeightPoints)
            .animation(isReduceMotionEnabled ? nil : Animation.snappy, value: playback.isScrubbing)
            .frame(maxHeight: .infinity)
            .contentShape(.rect)
            .gesture(scrubGesture(width: width))
        }
        .frame(height: Self.touchTargetPoints)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Video position")
        .accessibilityValue(spokenProgress)
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: playback.skip(bySeconds: VideoControls.adjustStepSeconds)
            case .decrement: playback.skip(bySeconds: -VideoControls.adjustStepSeconds)
            @unknown default: break
            }
            onUse()
        }
    }

    private var playedFraction: Double {
        VideoControls.playedFraction(elapsedSeconds: playback.elapsedSeconds, durationSeconds: playback.durationSeconds)
    }

    /// How far along the video is, as it is read aloud: the time reached out
    /// of the whole length.
    private var spokenProgress: String {
        let elapsedText = VideoControls.timeText(seconds: playback.elapsedSeconds)
        let durationText = VideoControls.timeText(seconds: playback.durationSeconds)
        return String(localized: "\(elapsedText) of \(durationText)")
    }

    private func fraction(of locationX: CGFloat, in width: CGFloat) -> Double {
        Double(min(1, max(0, locationX / width)))
    }

    private func scrubGesture(width: CGFloat) -> some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { value in
                playback.beginScrubbing()
                playback.scrub(toFraction: fraction(of: value.location.x, in: width))
            }
            .onEnded { value in
                playback.endScrubbing(atFraction: fraction(of: value.location.x, in: width))
                onUse()
            }
    }
}
