import SwiftUI
import TeaNodeKit

enum RecordingCaptureDisplayState {
    case recording
    case paused
    case interrupted
}

/// RecordingCaptureScreen is the recording under way: it follows MeetingRecorder, and closing it leaves the recording running.
struct RecordingCaptureScreen: View {
    /// Called once the recording has been stopped and saved on the phone.
    let didStop: () async -> Void
    @State private var recorder = MeetingRecorder.shared
    @Environment(\.dismiss) private var dismiss

    private var displayState: RecordingCaptureDisplayState {
        if recorder.isCapturing { return .recording }
        return recorder.localRecording?.recordingCaptureState == "interrupted" ? .interrupted : .paused
    }

    var body: some View {
        NavigationStack {
            RecordingCaptureView(
                elapsedMS: recorder.elapsedMS, displayState: displayState, inputLevel: { recorder.inputLevel() },
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
            .onChange(of: recorder.hasSession) { _, hasSession in
                if !hasSession { dismiss() }
            }
        }
        .presentationDragIndicator(.visible)
    }
}

/// RecordingCaptureView shows the time recorded large, how loud the room is, and the two things to do: pause or resume, and stop.
struct RecordingCaptureView: View {
    let elapsedMS: Int64
    let displayState: RecordingCaptureDisplayState
    let inputLevel: @MainActor () -> Double
    let onPauseOrResume: @MainActor () -> Void
    let onStop: @MainActor () -> Void
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
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
            Spacer()
            GlassEffectContainer(spacing: 16) {
                let layout =
                    dynamicTypeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(spacing: 16)) : AnyLayout(HStackLayout(spacing: 16))
                layout {
                    Button(action: onPauseOrResume) {
                        Label(
                            displayState == .recording ? String(localized: "Pause recording") : String(localized: "Resume recording"),
                            systemImage: displayState == .recording ? "pause.fill" : "mic.fill"
                        )
                        .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.glassProminent)
                    Button(role: .destructive, action: onStop) {
                        Label("Stop recording", systemImage: "stop.fill").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.glass)
                }
                .controlSize(.large)
            }
            Text(
                "Everyone should know you are recording. TeaNode stays silent and keeps the audio on this phone until the server verifies the upload."
            )
            .font(.footnote)
            .foregroundStyle(.secondary)
            .multilineTextAlignment(.center)
        }
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .navigationTitle("Recording")
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
