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
                bubbles: conversation.bubbles, actions: passageActions, microphoneModeText: microphoneMode.displayedName,
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
            notMe: { segment in conversation.beginNaming(segment, startsBlank: true) }, clearName: { segments in Task { await conversation.clearName(segments) } }, isPlaybackAvailable: false)
    }
}

/// RecordingCaptureView shows the recording under way: how long, how loud the room is, the conversation so far as bubbles, and
/// the two things to do, pause or resume, and stop.
struct RecordingCaptureView: View {
    let elapsedMS: Int64
    let displayState: RecordingCaptureDisplayState
    let inputLevel: @MainActor () -> Double
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
                TimelineView(.animation(minimumInterval: 0.1, paused: displayState != .recording)) { _ in
                    Gauge(value: displayState == .recording ? inputLevel() : 0) {
                        Text("Input level")
                    }
                    .gaugeStyle(.accessoryLinearCapacity)
                    .labelsHidden()
                    .tint(.accentColor)
                }
                .accessibilityLabel("Input level")
            }
            .padding(.horizontal)
            .padding(.bottom, 8)
            // At the largest text sizes the row would take the screen from the conversation, so it scrolls with it instead.
            if !dynamicTypeSize.isAccessibilitySize { microphoneModeRow.padding(.horizontal).padding(.bottom, 8) }
            ScrollView {
                VStack(spacing: 12) {
                    if dynamicTypeSize.isAccessibilitySize { microphoneModeRow }
                    Text(
                        "Everyone should know you are recording. TeaNode stays silent and keeps the audio on this phone until the server verifies the upload."
                    )
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
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

    /// The microphone mode in use, and a button for the system's menu to choose another.
    @ViewBuilder
    private var microphoneModeRow: some View {
        if let microphoneModeText, let onMicrophoneModes {
            Button(action: onMicrophoneModes) {
                let layout =
                    dynamicTypeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4)) : AnyLayout(HStackLayout(spacing: 8))
                layout {
                    Label("Mic mode", systemImage: "mic")
                    if !dynamicTypeSize.isAccessibilitySize { Spacer(minLength: 8) }
                    Text(microphoneModeText).foregroundStyle(.primary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
            .accessibilityLabel("Mic mode")
            .accessibilityValue(microphoneModeText)
        }
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
                elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                onPauseOrResume: {}, onStop: {})
        }.preferredColorScheme(.light)
    }
    #Preview("Live bubbles, dark") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                onPauseOrResume: {}, onStop: {})
        }.preferredColorScheme(.dark)
    }
    #Preview("Live bubbles, largest text") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .recording, inputLevel: { 0.6 }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                onPauseOrResume: {}, onStop: {})
        }.dynamicTypeSize(.accessibility5).preferredColorScheme(.light)
    }
    #Preview("Live bubbles, paused, dark") {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: 754_000, displayState: .paused, inputLevel: { 0 }, bubbles: DemoServer.liveCaptureBubbles, actions: .none,
                onPauseOrResume: {}, onStop: {})
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
