import AVFoundation
import Observation
import SwiftUI
import TeaNodeKit

// Voice messages in a chat: the button held to talk, the strip that shows the
// recording, and the bubble that plays one.

/// HoldToTalkButton is the composer's field in voice mode: a wide capsule held
/// to record. Letting go sends, sliding up past a distance and letting go
/// discards. With VoiceOver running, a hold cannot be made, so a tap starts
/// the recording and another sends it, and "Cancel" is an action.
struct HoldToTalkButton: View {
    let recorder: VoiceMessageRecorder
    let heightPoints: CGFloat
    /// Called when a hold begins.
    let onStart: () -> Void
    /// Called when it ends, with whether the finger had slid up to cancel.
    let onEnd: (Bool) -> Void
    @Environment(\.accessibilityVoiceOverEnabled) private var isVoiceOverRunning
    @State private var isHolding = false

    /// How far up the finger slides to arm cancelling, in points.
    static let cancelDragPoints: CGFloat = 80

    private var fill: AnyShapeStyle {
        if recorder.isRecording, recorder.isCancelArmed { return AnyShapeStyle(Color.red) }
        if recorder.isRecording { return AnyShapeStyle(.tint) }
        return AnyShapeStyle(Color(.tertiarySystemFill))
    }

    private var wordsText: Text {
        if recorder.isRecording, recorder.isCancelArmed { return Text("Release to cancel") }
        if recorder.isRecording, isVoiceOverRunning { return Text("Tap to send") }
        if recorder.isRecording { return Text("Release to send") }
        return Text("Hold to talk")
    }

    private var label: some View {
        HStack(spacing: 8) {
            Image(systemName: recorder.isRecording && recorder.isCancelArmed ? "trash" : "mic.fill")
            wordsText
        }
        .font(.body.weight(.medium))
        .foregroundStyle(recorder.isRecording ? Color.white : Color.primary)
        .frame(maxWidth: .infinity, minHeight: max(heightPoints, 44))
        .background(fill, in: .capsule)
        .contentShape(.capsule)
        .animation(.default, value: recorder.isCancelArmed)
        .animation(.default, value: recorder.isRecording)
        .sensoryFeedback(.start, trigger: recorder.startedCount)
        .sensoryFeedback(.selection, trigger: recorder.isCancelArmed)
    }

    var body: some View {
        if isVoiceOverRunning {
            Button {
                if recorder.isRecording { onEnd(false) } else { onStart() }
            } label: {
                label
            }
            .buttonStyle(.plain)
            .accessibilityLabel(recorder.isRecording ? Text("Send voice message") : Text("Record voice message"))
            .accessibilityAction(named: Text("Cancel")) { onEnd(true) }
        } else {
            label
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { value in
                            if !isHolding {
                                isHolding = true
                                onStart()
                            }
                            let isArmed = value.translation.height < -Self.cancelDragPoints
                            if recorder.isCancelArmed != isArmed { recorder.isCancelArmed = isArmed }
                        }
                        .onEnded { value in
                            isHolding = false
                            onEnd(value.translation.height < -Self.cancelDragPoints)
                        }
                )
                .accessibilityElement()
                .accessibilityLabel("Hold to talk")
                .accessibilityAddTraits(.isButton)
        }
    }
}

/// RecordingStrip floats over the composer while a voice message is being
/// recorded: the time, the level, and what letting go does. With nothing
/// recording it holds a hint for a moment.
struct RecordingStrip: View {
    let recorder: VoiceMessageRecorder
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// At the accessibility sizes what letting go does goes under the time,
    /// which would otherwise wrap a digit at a time.
    private var layout: AnyLayout {
        dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4)) : AnyLayout(HStackLayout(spacing: 10))
    }

    var body: some View {
        layout {
            if recorder.isRecording {
                HStack(spacing: 10) {
                    Image(systemName: recorder.isCancelArmed ? "trash" : "mic.fill")
                        .foregroundStyle(recorder.isCancelArmed ? AnyShapeStyle(Color.red) : AnyShapeStyle(.tint))
                    if recorder.isCountingDown {
                        Text("\(VoiceMessageRecorder.clockText(recorder.secondsLeft)) left")
                            .monospacedDigit()
                            .contentTransition(.numericText())
                            .fixedSize()
                    } else {
                        Text(verbatim: VoiceMessageRecorder.clockText(recorder.elapsedSeconds))
                            .monospacedDigit()
                            .contentTransition(.numericText())
                            .fixedSize()
                    }
                    meter
                }
                cancelText
                    .font(.footnote)
                    .foregroundStyle(recorder.isCancelArmed ? AnyShapeStyle(Color.red) : AnyShapeStyle(.secondary))
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 2 : 1)
            } else if let hintText = recorder.hintText {
                Text(hintText)
            }
        }
        .font(.callout)
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .glassEffect(.regular, in: dynamicTypeSize.isAccessibilitySize ? AnyShape(RoundedRectangle(cornerRadius: 24, style: .continuous)) : AnyShape(Capsule()))
        .accessibilityElement(children: .combine)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    private var cancelText: Text {
        recorder.isCancelArmed ? Text("Release to cancel") : Text("Slide up to cancel")
    }

    private var meter: some View {
        Capsule()
            .fill(.tint)
            .frame(width: 8 + 40 * CGFloat(recorder.level), height: 6)
            .frame(width: 48, alignment: .leading)
            .animation(.linear(duration: 0.05), value: recorder.level)
            .accessibilityHidden(true)
    }
}

/// FailedRecordingStrip is a voice message that could not be sent, kept above
/// the composer to send again or throw away.
struct FailedRecordingStrip: View {
    let recording: VoiceRecording
    let onRetry: () -> Void
    let onDiscard: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 1) {
                Text("Voice message not sent")
                    .font(.subheadline.weight(.medium))
                    .lineLimit(2)
                Text(verbatim: VoiceMessageRecorder.clockText(recording.durationSeconds))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            Spacer(minLength: 4)
            Button("Try again", action: onRetry)
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
            Button(action: onDiscard) {
                Image(systemName: "xmark")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .frame(width: 26, height: 26)
                    .background(Color(.tertiarySystemFill), in: .circle)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Throw away the recording")
        }
        .padding(.leading, 14)
        .padding(.trailing, 2)
        .padding(.vertical, 2)
        .glassEffect(in: .rect(cornerRadius: 20, style: .continuous))
    }
}

// MARK: Playing

/// VoiceMessagePlayer plays the voice messages of chats: one at a time, each
/// read from the server once and kept in memory while the app runs.
@MainActor
@Observable
final class VoiceMessagePlayer {
    static let shared = VoiceMessagePlayer()

    /// The message loaded, whether playing or paused.
    private(set) var loadedAttachmentId: String?
    /// The message being read from the server.
    private(set) var loadingAttachmentId: String?
    private(set) var isPlaying = false
    /// How far through the loaded message, 0 to 1, and how many seconds.
    private(set) var progress: Double = 0
    private(set) var playedSeconds: TimeInterval = 0
    /// The message that could not be read or played.
    private(set) var failedAttachmentId: String?

    @ObservationIgnored private var audioPlayer: AVAudioPlayer?
    @ObservationIgnored private var dataByAttachmentId: [String: Data] = [:]
    @ObservationIgnored private var tickTask: Task<Void, Never>?
    @ObservationIgnored private var delegate: VoiceMessagePlayerDelegate?
    @ObservationIgnored private var interruptionObserver: (any NSObjectProtocol)?

    /// Plays or pauses a message; another one playing is stopped first.
    func toggle(_ attachment: AgentAttachment, app: AppModel) async {
        if loadedAttachmentId == attachment.id, let audioPlayer {
            if isPlaying {
                pause()
            } else {
                activate()
                audioPlayer.play()
                isPlaying = true
                startTicking()
            }
            return
        }
        stop()
        guard loadingAttachmentId == nil else { return }
        failedAttachmentId = nil
        loadingAttachmentId = attachment.id
        defer { loadingAttachmentId = nil }
        do {
            let data: Data
            if let kept = dataByAttachmentId[attachment.id] {
                data = kept
            } else {
                guard let connection = app.connection else { throw TeaNodeRecordingError.notStarted }
                data = try await connection.download("/api/v1/agent/attachments/\(attachment.id)")
                dataByAttachmentId[attachment.id] = data
            }
            let newPlayer = try AVAudioPlayer(data: data)
            let newDelegate = VoiceMessagePlayerDelegate { [weak self] in self?.finished() }
            newPlayer.delegate = newDelegate
            delegate = newDelegate
            audioPlayer = newPlayer
            loadedAttachmentId = attachment.id
            activate()
            guard newPlayer.play() else { throw TeaNodeRecordingError.notStarted }
            isPlaying = true
            startTicking()
            watchForInterruption()
        } catch {
            audioPlayer = nil
            loadedAttachmentId = nil
            failedAttachmentId = attachment.id
        }
    }

    func pause() {
        audioPlayer?.pause()
        isPlaying = false
        tickTask?.cancel()
        tickTask = nil
    }

    /// Stops whatever is loaded, so the next message plays from its start.
    func stop() {
        audioPlayer?.stop()
        audioPlayer = nil
        delegate = nil
        loadedAttachmentId = nil
        isPlaying = false
        progress = 0
        playedSeconds = 0
        tickTask?.cancel()
        tickTask = nil
        if let interruptionObserver {
            NotificationCenter.default.removeObserver(interruptionObserver)
            self.interruptionObserver = nil
        }
    }

    private func activate() {
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.playback)
        try? session.setActive(true)
    }

    /// The message played to its end: it is ready to play again from the start.
    private func finished() {
        isPlaying = false
        progress = 0
        playedSeconds = 0
        audioPlayer?.currentTime = 0
        tickTask?.cancel()
        tickTask = nil
    }

    private func startTicking() {
        tickTask?.cancel()
        tickTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(100))
                guard let self else { return }
                guard let audioPlayer = self.audioPlayer, audioPlayer.duration > 0 else { continue }
                self.playedSeconds = audioPlayer.currentTime
                self.progress = min(1, audioPlayer.currentTime / audioPlayer.duration)
            }
        }
    }

    /// A call or another app's sound pauses it.
    private func watchForInterruption() {
        guard interruptionObserver == nil else { return }
        interruptionObserver = NotificationCenter.default.addObserver(
            forName: AVAudioSession.interruptionNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.pause() }
        }
    }
}

/// VoiceMessagePlayerDelegate tells the player when a message played to its
/// end. AVAudioPlayer calls it on the main thread.
final class VoiceMessagePlayerDelegate: NSObject, AVAudioPlayerDelegate, @unchecked Sendable {
    private let didFinish: @MainActor () -> Void

    init(didFinish: @escaping @MainActor () -> Void) {
        self.didFinish = didFinish
    }

    func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        let didFinish = didFinish
        Task { @MainActor in didFinish() }
    }
}

// MARK: The bubble

/// VoiceMessageBubble is a voice message in the transcript: a play button, the
/// recording's waveform filling as it plays, its time, and its words under it
/// when the server made them. It has the bubble's own surface: the person's
/// side in the accent's tint, another's in gray.
struct VoiceMessageBubble: View {
    let line: TranscriptLine
    let recording: AgentAttachment
    let isOthers: Bool
    /// Asks for the words of a message that has none (they were off for its
    /// sender, or failed), paid by the person's agent.
    var onTranscribe: (() async -> Void)? = nil
    @Environment(AppModel.self) private var app
    @State private var isTranscribing = false
    @ScaledMetric(relativeTo: .body) private var scaledButtonDiameterPoints: CGFloat = 36
    @ScaledMetric(relativeTo: .body) private var scaledWaveformHeightPoints: CGFloat = 28
    /// The button and the waveform grow with the text only so far: at the
    /// largest sizes they would not fit beside the time in a bubble.
    private var buttonDiameterPoints: CGFloat { min(scaledButtonDiameterPoints, 56) }
    private var waveformHeightPoints: CGFloat { min(scaledWaveformHeightPoints, 44) }
    private let player = VoiceMessagePlayer.shared

    private var isLoaded: Bool { player.loadedAttachmentId == recording.id }
    private var isPlayingThis: Bool { isLoaded && player.isPlaying }
    private var isLoadingThis: Bool { player.loadingAttachmentId == recording.id }
    private var durationSeconds: TimeInterval { TimeInterval(recording.audioDurationMS ?? 0) / 1000 }
    private var progress: Double { isLoaded ? player.progress : 0 }

    /// The whole length while it rests, and the time left while it plays.
    private var timeText: String {
        guard isLoaded, player.isPlaying || player.playedSeconds > 0 else {
            return VoiceMessageRecorder.clockText(durationSeconds.rounded(.up))
        }
        return VoiceMessageRecorder.clockText(max(0, durationSeconds - player.playedSeconds).rounded(.up))
    }

    /// The words under the bubble: a note while they are made, and nothing
    /// when the server makes none.
    private var transcriptText: Text? {
        if line.transcriptState == "waiting" || isTranscribing { return Text("Transcribing…") }
        if line.transcriptState == "failed" { return Text("Could not transcribe") }
        if line.transcriptState == "done", !line.transcriptText.isEmpty { return Text(verbatim: line.transcriptText) }
        return nil
    }

    /// Whether the message has no words to show and could have some.
    private var canTranscribe: Bool { ["off", "failed", ""].contains(line.transcriptState) && line.messageId != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                Button {
                    Task { await player.toggle(recording, app: app) }
                } label: {
                    ZStack {
                        Circle().fill(.tint)
                        if isLoadingThis {
                            ProgressView().tint(.white)
                        } else {
                            Image(systemName: isPlayingThis ? "pause.fill" : "play.fill")
                                .font(.body)
                                .dynamicTypeSize(...DynamicTypeSize.accessibility1)
                                .foregroundStyle(.white)
                        }
                    }
                    .frame(width: buttonDiameterPoints, height: buttonDiameterPoints)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(isPlayingThis ? Text("Pause voice message") : Text("Play voice message"))
                .accessibilityValue(Text(verbatim: VoiceMessageRecorder.clockText(durationSeconds.rounded(.up))))
                VoiceWaveform(levels: VoiceWaveform.decodedLevels(of: recording.audioWaveform), progress: progress)
                    .frame(minWidth: 60, idealWidth: 150, maxWidth: 150)
                    .frame(height: waveformHeightPoints)
                Text(verbatim: timeText)
                    .font(.footnote.monospacedDigit())
                    .foregroundStyle(.secondary)
                    .fixedSize()
            }
            if let transcriptText {
                transcriptText
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: 260, alignment: .leading)
            }
            if let onTranscribe, canTranscribe, !isTranscribing {
                Button("Transcribe") {
                    isTranscribing = true
                    Task {
                        await onTranscribe()
                        isTranscribing = false
                    }
                }
                .font(.footnote)
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .controlSize(.small)
            }
            if player.failedAttachmentId == recording.id {
                Text("Could not play this voice message")
                    .font(.footnote)
                    .foregroundStyle(.red)
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, 14)
        .padding(.vertical, 4)
        .background(
            isOthers ? AnyShapeStyle(Color(.secondarySystemBackground)) : AnyShapeStyle(Color.accentColor.opacity(0.2)),
            in: .rect(cornerRadius: 20, style: .continuous)
        )
        .overlay(alignment: isOthers ? .bottomTrailing : .bottomLeading) {
            ReactionBadges(reactions: line.reactions)
                .offset(x: isOthers ? 10 : -10, y: 18)
        }
        .padding(.bottom, line.reactions.isEmpty ? 0 : 18)
    }
}

/// VoiceWaveform draws a recording's loudness as bars, those already played
/// in the accent.
struct VoiceWaveform: View {
    let levels: [UInt8]
    let progress: Double

    /// The levels of a message: base64 of one byte for each bar, 0 to 255. A
    /// message with none gets an even row, so its bubble is not empty.
    static func decodedLevels(of waveform: String?) -> [UInt8] {
        if let waveform, let data = Data(base64Encoded: waveform), !data.isEmpty { return [UInt8](data) }
        return [UInt8](repeating: 60, count: 48)
    }

    var body: some View {
        Canvas { context, size in
            guard !levels.isEmpty else { return }
            let step = size.width / CGFloat(levels.count)
            let barWidth = max(2, min(4, step * 0.6))
            for (index, level) in levels.enumerated() {
                let height = max(3, size.height * CGFloat(level) / 255)
                let bar = CGRect(
                    x: CGFloat(index) * step + (step - barWidth) / 2, y: (size.height - height) / 2, width: barWidth, height: height)
                let isPlayed = Double(index) / Double(levels.count) < progress
                context.fill(
                    Path(roundedRect: bar, cornerRadius: barWidth / 2),
                    with: .color(isPlayed ? Color.accentColor : Color(.tertiaryLabel)))
            }
        }
        .accessibilityHidden(true)
    }
}
