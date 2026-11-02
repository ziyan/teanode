import AVFoundation
import Foundation
import Observation
import UIKit

/// VoiceRecording is a voice message recorded and not yet sent: the file, and
/// how long it runs.
struct VoiceRecording: Identifiable {
    let id = UUID()
    let address: URL
    let durationSeconds: TimeInterval
}

/// VoiceMessageRecorder records what the person says while they hold the
/// button in a chat: AAC in an m4a file, mono at 44.1 kHz and 64 kbps, which
/// the server takes as it is. Half a second is the least a message is and two
/// minutes the most; from 1:50 the time left is counted down.
@MainActor
@Observable
final class VoiceMessageRecorder {
    static let shortestSeconds: TimeInterval = 0.5
    static let longestSeconds: TimeInterval = 120
    static let countdownFromSeconds: TimeInterval = 110

    private(set) var isRecording = false
    /// Whether the finger has slid far enough up that letting go discards.
    var isCancelArmed = false
    private(set) var elapsedSeconds: TimeInterval = 0
    /// How loud the microphone is, for the meter: 0 to 1.
    private(set) var level: Float = 0
    /// Counts the recordings begun, for the haptic.
    private(set) var startedCount = 0
    /// A word of guidance that shows for a moment in the strip.
    private(set) var hintText: String?
    /// Why a recording could not begin, for a toast.
    var problemText: String?
    /// Called when the recording reaches its longest, to send it.
    @ObservationIgnored var onLongest: (() -> Void)?

    @ObservationIgnored private var audioRecorder: AVAudioRecorder?
    @ObservationIgnored private var tickTask: Task<Void, Never>?
    @ObservationIgnored private var hintTask: Task<Void, Never>?
    @ObservationIgnored private var observers: [any NSObjectProtocol] = []

    /// Whether the time left is counted down.
    var isCountingDown: Bool { isRecording && elapsedSeconds >= Self.countdownFromSeconds }
    var secondsLeft: TimeInterval { max(0, Self.longestSeconds - elapsedSeconds) }

    /// The time as minutes and seconds: 0:07, 1:50.
    static func clockText(_ seconds: TimeInterval) -> String {
        let whole = max(0, Int(seconds.rounded(.down)))
        return "\(whole / 60):" + String(format: "%02d", whole % 60)
    }

    // MARK: Recording

    /// Starts recording. Without the microphone's permission it only asks
    /// for it, the first time, and the person holds again.
    func start() {
        guard !isRecording else { return }
        let permission = AVAudioApplication.shared.recordPermission
        switch permission {
        case .granted:
            break
        case .undetermined:
            Task {
                _ = await AVAudioApplication.requestRecordPermission()
                showHint(String(localized: "Hold again to talk"))
            }
            return
        default:
            problemText = String(localized: "TeaNode may not use the microphone. Allow it in Settings, then try again.")
            return
        }
        guard !MeetingRecorder.shared.hasSession, AppModel.shared.currentCall == nil else {
            problemText = String(localized: "Stop the current recording or call before recording a voice message.")
            return
        }
        do {
            try begin()
        } catch {
            stopRecording()
            problemText = String(localized: "The microphone could not be started: \(error.localizedDescription)")
        }
    }

    private func begin() throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker, .allowBluetoothHFP])
        try session.setActive(true)
        let address = FileManager.default.temporaryDirectory.appendingPathComponent("Voice message \(UUID().uuidString).m4a")
        let settings: [String: Any] = [
            AVFormatIDKey: Int(kAudioFormatMPEG4AAC),
            AVSampleRateKey: 44100,
            AVNumberOfChannelsKey: 1,
            AVEncoderBitRateKey: 64000,
        ]
        let recorder = try AVAudioRecorder(url: address, settings: settings)
        recorder.isMeteringEnabled = true
        guard recorder.record() else {
            throw TeaNodeRecordingError.notStarted
        }
        audioRecorder = recorder
        isRecording = true
        isCancelArmed = false
        elapsedSeconds = 0
        level = 0
        hintText = nil
        startedCount += 1
        watchForInterruptions()
        tickTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(50))
                guard let self else { return }
                self.tick()
            }
        }
    }

    private func tick() {
        guard isRecording, let audioRecorder else { return }
        audioRecorder.updateMeters()
        elapsedSeconds = audioRecorder.currentTime
        // Decibels from silence at -160 up to 0; speech is heard from about -50.
        level = max(0, min(1, (audioRecorder.averagePower(forChannel: 0) + 50) / 50))
        if elapsedSeconds >= Self.longestSeconds { onLongest?() }
    }

    /// Ends the recording and hands it over to be sent. One shorter than half
    /// a second is thrown away, with a word on how to record.
    func finish() -> VoiceRecording? {
        guard isRecording, let audioRecorder else { return nil }
        let seconds = min(audioRecorder.currentTime, Self.longestSeconds)
        let address = audioRecorder.url
        stopRecording()
        guard seconds >= Self.shortestSeconds else {
            try? FileManager.default.removeItem(at: address)
            showHint(String(localized: "Hold to record, release to send"))
            return nil
        }
        return VoiceRecording(address: address, durationSeconds: seconds)
    }

    /// Ends the recording and throws it away.
    func cancel() {
        guard isRecording, let address = audioRecorder?.url else { return }
        stopRecording()
        try? FileManager.default.removeItem(at: address)
    }

    private func stopRecording() {
        tickTask?.cancel()
        tickTask = nil
        audioRecorder?.stop()
        audioRecorder = nil
        isRecording = false
        isCancelArmed = false
        level = 0
        for observer in observers { NotificationCenter.default.removeObserver(observer) }
        observers = []
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    /// A phone call, another app taking the microphone, or leaving the app
    /// cancels the recording: nothing half-said is sent.
    private func watchForInterruptions() {
        let interruption = NotificationCenter.default.addObserver(
            forName: AVAudioSession.interruptionNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.cancel() }
        }
        let resigned = NotificationCenter.default.addObserver(
            forName: UIApplication.willResignActiveNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.cancel() }
        }
        observers = [interruption, resigned]
    }

    private func showHint(_ text: String) {
        hintText = text
        hintTask?.cancel()
        hintTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(2.5))
            guard !Task.isCancelled else { return }
            self?.hintText = nil
        }
    }

    #if DEBUG
        /// `-TeaNodeScreen chat-voice-hold`: the strip as it looks seven seconds into a recording, since a screenshot has no
        /// microphone and no finger.
        func showDemoHold() {
            isRecording = true
            elapsedSeconds = 7.4
            level = 0.6
        }
    #endif
}

/// TeaNodeRecordingError is what a recording says when the system declined to
/// begin it without saying why.
enum TeaNodeRecordingError: LocalizedError {
    case notStarted

    var errorDescription: String? {
        String(localized: "The recording did not start.")
    }
}
