import AVFoundation
import SwiftUI
import TeaNodeKit

enum RecordingCaptureDisplayState {
    case recording
    case paused
    case interrupted
}

/// RecordingCaptureScreen is the recording under way: it follows MeetingRecorder, and closing it leaves the recording running.
/// It shows the conversation as it is heard: words within a second or two, and each speaker about half a minute later.
struct RecordingCaptureScreen: View {
    /// Called once the recording has been stopped and saved on the phone.
    let didStop: () async -> Void
    @State private var recorder = MeetingRecorder.shared
    @State private var conversation: RecordingConversationModel
    @State private var microphoneMode = AVCaptureDevice.activeMicrophoneMode
    @Environment(\.dismiss) private var dismiss

    init(connection: Connection, didStop: @escaping () async -> Void) {
        self.didStop = didStop
        _conversation = State(initialValue: RecordingConversationModel(connection: connection))
    }

    private var displayState: RecordingCaptureDisplayState {
        if recorder.isCapturing { return .recording }
        return recorder.localRecording?.recordingCaptureState == "interrupted" ? .interrupted : .paused
    }

    var body: some View {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: recorder.elapsedMS, displayState: displayState, inputLevel: { recorder.inputLevel() },
                recentLevels: { recorder.recentLevels() }, bubbles: conversation.bubbles, actions: passageActions, microphoneModeText: microphoneMode.displayedName,
                onMicrophoneModes: { AVCaptureDevice.showSystemUserInterface(.microphoneModes) },
                onPauseOrResume: {
                    Task { if recorder.isCapturing { await recorder.pause() } else { await recorder.resume() } }
                },
                onStop: {
                    Task {
                        await recorder.stop()
                        await didStop()
                        dismiss()
                    }
                }
            )
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
            .toast(done: $conversation.doneText, problem: $conversation.problemText)
            .recordingPassageSheets(conversation)
            .onChange(of: recorder.hasSession) { _, hasSession in
                if !hasSession { dismiss() }
            }
            // The words the phone's own stream hears show at once; the transcript with speakers is read every few seconds.
            .onChange(of: recorder.liveStream.heardUtterances) { _, heard in conversation.heardUtterances = heard }
            .onChange(of: recorder.liveStream.partialText) { _, text in
                conversation.partialText = text
                conversation.partialStartMS = recorder.liveStream.partialStartMS
            }
            // The person chooses the mode in the system's menu; it is read again while this screen is up.
            .task {
                while !Task.isCancelled {
                    microphoneMode = AVCaptureDevice.activeMicrophoneMode
                    try? await Task.sleep(for: .seconds(1))
                }
            }
            .task {
                conversation.heardUtterances = recorder.liveStream.heardUtterances
                while !Task.isCancelled {
                    // The server creates the recording a moment after it starts.
                    if conversation.recordingId.isEmpty, let remoteId = recorder.localRecording?.recordingId {
                        conversation.recordingId = remoteId
                    }
                    await conversation.loadCapture()
                    try? await Task.sleep(for: .seconds(conversation.recordingId.isEmpty ? 1 : 3))
                }
            }
        }
        .presentationDragIndicator(.visible)
    }

    /// A long press on a bubble: name its speaker, or say it is the person. There is no finished audio or text to work on yet.
    private var passageActions: RecordingPassageActions {
        RecordingPassageActions(
            play: { _ in },
            copy: { segment in
                UIPasteboard.general.string = segment.displayedTranscriptText
                conversation.doneText = String(localized: "Copied.")
            },
            correctText: { _ in }, nameSpeaker: { segment in conversation.beginNaming(segment) },
            thisIsMe: { segment in Task { await conversation.confirmAsAccountOwner(segment) } },
            notMe: { segment in conversation.beginNaming(segment, startsBlank: true) }, clearName: { segments in Task { await conversation.clearName(segments) } },
            separateSpeaker: { segment in Task { await conversation.separateSpeaker(segment) } }, isPlaybackAvailable: false)
    }
}

/// RecordingCaptureView shows the recording under way: how long, how loud the room is, the conversation so far as bubbles, and
/// the two things to do, pause or resume, and stop.
struct RecordingCaptureView: View {
    let elapsedMS: Int64
    let displayState: RecordingCaptureDisplayState
    let inputLevel: @MainActor () -> Double
    /// The latest levels, oldest first, for the graph of bars under the clock; empty draws silence.
    var recentLevels: @MainActor () -> [Double] = { [] }
    /// The conversation so far, oldest first: transcript passages with their speakers, and after them the words just heard.
    var bubbles: [RecordingConversationBubble] = []
    var actions: RecordingPassageActions = .none
    /// The microphone mode now in use (Standard, Voice Isolation or Wide Spectrum), and what opens the system's menu to choose
    /// one; without them the row is not drawn.
    var microphoneModeText: String?
    var onMicrophoneModes: (@MainActor () -> Void)?
    let onPauseOrResume: @MainActor () -> Void
    let onStop: @MainActor () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        VStack(spacing: 0) {
            VStack(spacing: 8) {
                Label {
                    Text(stateText)
                } icon: {
                    Image(systemName: stateSymbol).symbolEffect(.pulse, isActive: displayState == .recording)
                }
                .font(.headline)
                .foregroundStyle(displayState == .recording ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
                Text(Duration.milliseconds(elapsedMS).formatted(.time(pattern: .hourMinuteSecond)))
                    .font(.largeTitle.bold())
                    .fontDesign(.rounded)
                    .monospacedDigit()
                    .contentTransition(.numericText())
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                // Voice Memos' graph: a bar for each tenth of a second, the newest on the right, scrolling left. It holds
                // still when paused.
                TimelineView(.animation(minimumInterval: 0.1, paused: displayState != .recording)) { _ in
                    RecordingLevelGraph(levels: recentLevels(), isActive: displayState == .recording)
                }
                .frame(height: 44)
                .accessibilityElement()
                .accessibilityLabel("Input level")
            }
            .padding(.horizontal)
            .padding(.bottom, 8)
            ScrollView {
                VStack(spacing: 12) {
                    Text(
                        "Everyone should know you are recording. TeaNode stays silent and keeps the audio on this phone until the server verifies the upload."
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    // Held at a size that fits under the timer: the list
                    // keeps its end in view, which at the largest sizes cut
                    // this note off mid-sentence.
                    .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
                    ForEach(bubbles) { bubble in
                        RecordingBubbleView(bubble: bubble, playingSegmentId: nil, actions: actions)
                    }
                }
                .padding(.horizontal)
                .padding(.vertical, 8)
            }
            // The newest words are at the bottom, as in a chat, and stay in view while the person is at the bottom.
            .defaultScrollAnchor(.bottom)
        }
        .safeAreaInset(edge: .bottom) {
            GlassEffectContainer(spacing: 16) {
                let layout =
                    dynamicTypeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(spacing: 16)) : AnyLayout(HStackLayout(spacing: 16))
                layout {
                    // Pause and Stop have the room; the microphone mode is an icon beside them (a full-width button at the
                    // largest sizes, where the row stacks).
                    if let microphoneModeText, let onMicrophoneModes {
                        Button(action: onMicrophoneModes) {
                            if dynamicTypeSize.isAccessibilitySize {
                                Label("Mic mode: \(microphoneModeText)", systemImage: "mic").lineLimit(2).frame(maxWidth: .infinity)
                            } else {
                                Image(systemName: "mic").frame(minWidth: 24)
                            }
                        }
                        .buttonStyle(.glass)
                        .accessibilityLabel("Mic mode: \(microphoneModeText)")
                    }
                    Button(action: onPauseOrResume) {
                        Label(
                            displayState == .recording ? String(localized: "Pause") : String(localized: "Resume"),
                            systemImage: displayState == .recording ? "pause.fill" : "mic.fill"
                        )
                        .lineLimit(1)
                        .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.glassProminent)
                    .accessibilityLabel(
                        displayState == .recording ? String(localized: "Pause recording") : String(localized: "Resume recording"))
                    Button(role: .destructive, action: onStop) {
                        Label("Stop", systemImage: "stop.fill").lineLimit(1).frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.glass)
                    .accessibilityLabel("Stop recording")
                }
                .controlSize(.large)
            }
            .padding(.horizontal)
            .padding(.bottom, 8)
        }
        .navigationBarTitleDisplayMode(.inline)
    }

    private var stateText: String {
        switch displayState {
        case .recording: String(localized: "Recording")
        case .paused: String(localized: "Paused")
        case .interrupted: String(localized: "Interrupted")
        }
    }

    private var stateSymbol: String {
        switch displayState {
        case .recording: "record.circle"
        case .paused: "pause.circle"
        case .interrupted: "exclamationmark.circle"
        }
    }
}

#Preview("Recording, light") {
    NavigationStack {
        RecordingCaptureView(elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, onPauseOrResume: {}, onStop: {})
    }.preferredColorScheme(.light)
}
#Preview("Recording, dark") {
    NavigationStack {
        RecordingCaptureView(elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, onPauseOrResume: {}, onStop: {})
    }.preferredColorScheme(.dark)
}
#Preview("Paused, light") {
    NavigationStack {
        RecordingCaptureView(elapsedMS: 1_805_000, displayState: .paused, inputLevel: { 0 }, onPauseOrResume: {}, onStop: {})
    }.preferredColorScheme(.light)
}
#Preview("Paused, dark") {
    NavigationStack {
        RecordingCaptureView(elapsedMS: 1_805_000, displayState: .paused, inputLevel: { 0 }, onPauseOrResume: {}, onStop: {})
    }.preferredColorScheme(.dark)
}
#Preview("Interrupted, light") {
    NavigationStack {
        RecordingCaptureView(elapsedMS: 92_000, displayState: .interrupted, inputLevel: { 0 }, onPauseOrResume: {}, onStop: {})
    }.preferredColorScheme(.light)
}
#Preview("Interrupted, dark") {
    NavigationStack {
        RecordingCaptureView(elapsedMS: 92_000, displayState: .interrupted, inputLevel: { 0 }, onPauseOrResume: {}, onStop: {})
    }.preferredColorScheme(.dark)
}

#if DEBUG
    #Preview("Live bubbles, light") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, recentLevels: { DemoServer.levelBars }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                microphoneModeText: String(localized: "Voice Isolation"), onMicrophoneModes: {}, onPauseOrResume: {}, onStop: {})
        }.preferredColorScheme(.light)
    }
    #Preview("Live bubbles, dark") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, recentLevels: { DemoServer.levelBars }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                microphoneModeText: String(localized: "Voice Isolation"), onMicrophoneModes: {}, onPauseOrResume: {}, onStop: {})
        }.preferredColorScheme(.dark)
    }
    #Preview("Live bubbles, largest text") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, recentLevels: { DemoServer.levelBars }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                microphoneModeText: String(localized: "Voice Isolation"), onMicrophoneModes: {}, onPauseOrResume: {}, onStop: {})
        }.dynamicTypeSize(.accessibility5).preferredColorScheme(.light)
    }
    #Preview("Live bubbles, paused, dark") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .paused, inputLevel: { 0 }, recentLevels: { DemoServer.levelBars }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                microphoneModeText: String(localized: "Voice Isolation"), onMicrophoneModes: {}, onPauseOrResume: {}, onStop: {})
        }.preferredColorScheme(.dark)
    }
#endif

extension AVCaptureDevice.MicrophoneMode {
    /// The mode's name as the system's menu shows it.
    var displayedName: String {
        switch self {
        case .standard: String(localized: "Standard")
        case .wideSpectrum: String(localized: "Wide Spectrum")
        case .voiceIsolation: String(localized: "Voice Isolation")
        @unknown default: String(localized: "Standard")
        }
    }
}

/// RecordingLevelGraph draws the latest input levels as vertical bars, the newest on the right.
struct RecordingLevelGraph: View {
    let levels: [Double]
    let isActive: Bool

    var body: some View {
        Canvas { context, size in
            let barCount = max(levels.count, 1)
            let spacing: CGFloat = 2
            let barWidth = max(1, (size.width - spacing * CGFloat(barCount - 1)) / CGFloat(barCount))
            let minimumHeight: CGFloat = 3
            for (index, level) in levels.enumerated() {
                let height = max(minimumHeight, size.height * CGFloat(level))
                let rect = CGRect(x: CGFloat(index) * (barWidth + spacing), y: (size.height - height) / 2, width: barWidth, height: height)
                context.fill(Path(roundedRect: rect, cornerRadius: barWidth / 2), with: .color(isActive ? .red : .secondary))
            }
        }
    }
}
