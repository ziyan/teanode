import AVFoundation
import Foundation
import Observation
import SwiftUI
import TeaNodeKit

/// Dictation puts what the person says into a box as they speak: the
/// microphone is streamed to the server's voice socket
/// (`/api/v1/agent/voice`, the one a call uses) with the purpose
/// "dictation", which only transcribes. Each utterance's final words are
/// appended to the text it was started on; nothing is sent or saved, and the
/// words stay editable.
@MainActor
@Observable
final class Dictation {
    enum DictationState: Equatable {
        case idle, starting, listening, hearing
    }

    /// Where the words go, as the server words its transcription prompt.
    enum Place: String {
        case agent, chat, journal
    }

    private(set) var dictationState: DictationState = .idle
    /// The words being heard, before they are final.
    private(set) var captionText = ""
    /// Why dictation stopped by itself, for a toast; nil when it did not.
    var problemText: String?

    var isDictating: Bool { dictationState != .idle }

    /// How long without a word before dictation stops by itself, as the
    /// dashboard's does.
    static let silentSecondsMost: TimeInterval = 30

    @ObservationIgnored private var socket: URLSessionWebSocketTask?
    /// Made when dictation first starts: a box that is never dictated into does not hold an audio engine.
    @ObservationIgnored private lazy var engine = AVAudioEngine()
    @ObservationIgnored private var isEngineStarted = false
    @ObservationIgnored private let capture = CaptureSender()
    @ObservationIgnored private var isTapInstalled = false
    @ObservationIgnored private var text: Binding<String>?
    @ObservationIgnored private var lastHeardAt = Date()
    @ObservationIgnored private var watchTask: Task<Void, Never>?
    @ObservationIgnored private var interruptionObserver: (any NSObjectProtocol)?

    /// Starts dictating into text, or stops when it is going already.
    func toggle(into text: Binding<String>, place: Place, connection: Connection, sampleRate: Int) {
        if isDictating {
            stop()
        } else {
            start(into: text, place: place, connection: connection, sampleRate: sampleRate)
        }
    }

    func start(into text: Binding<String>, place: Place, connection: Connection, sampleRate: Int) {
        guard dictationState == .idle else { return }
        guard !MeetingRecorder.shared.hasSession else {
            problemText = String(localized: "Stop the current recording before dictating.")
            return
        }
        self.text = text
        captionText = ""
        problemText = nil
        dictationState = .starting
        lastHeardAt = Date()
        Task {
            guard await AVAudioApplication.requestRecordPermission() else {
                finish(problemText: String(localized: "TeaNode may not use the microphone. Allow it in Settings, then try again."))
                return
            }
            guard dictationState == .starting else { return }
            do {
                try startEngine(sampleRate: Double(sampleRate))
            } catch {
                finish(problemText: String(localized: "The microphone could not be started: \(error.localizedDescription)"))
                return
            }
            watchForSilence()
            watchForInterruption()
            await listen(connection: connection, place: place, sampleRate: sampleRate)
        }
    }

    /// Stops listening. Words already put in the box stay.
    func stop() {
        finish(problemText: nil)
    }

    private func finish(problemText: String?) {
        if let problemText { self.problemText = problemText }
        guard dictationState != .idle || socket != nil else { return }
        dictationState = .idle
        captionText = ""
        watchTask?.cancel()
        watchTask = nil
        if let interruptionObserver {
            NotificationCenter.default.removeObserver(interruptionObserver)
            self.interruptionObserver = nil
        }
        if let socket {
            socket.send(.string(#"{"voiceEvent":"stop"}"#)) { @Sendable _ in }
            socket.cancel(with: .goingAway, reason: nil)
        }
        socket = nil
        capture.socket = nil
        stopEngine()
        text = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    // MARK: The microphone

    private func startEngine(sampleRate: Double) throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker, .allowBluetoothHFP])
        try session.setActive(true)
        let input = engine.inputNode
        let inputFormat = input.outputFormat(forBus: 0)
        guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0 else {
            throw TeaNodeError.refused(String(localized: "The microphone is not ready. Try again in a moment."))
        }
        guard let sendFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: sampleRate, channels: 1, interleaved: true),
            let converter = AVAudioConverter(from: inputFormat, to: sendFormat)
        else {
            throw TeaNodeError.refused(String(localized: "The microphone's format cannot be sent."))
        }
        capture.converter = converter
        capture.sendFormat = sendFormat
        // A tenth of a second at a time, as a call and the dashboard send it.
        let frames = AVAudioFrameCount(inputFormat.sampleRate / 10)
        let sender = capture
        try AudioExceptions.run {
            input.installTap(onBus: 0, bufferSize: frames, format: inputFormat) { @Sendable buffer, _ in
                sender.send(buffer)
            }
        }
        isTapInstalled = true
        let engine = engine
        try AudioExceptions.run { try engine.start() }
        isEngineStarted = true
    }

    private func stopEngine() {
        guard isEngineStarted || isTapInstalled else { return }
        let engine = engine
        isEngineStarted = false
        if isTapInstalled {
            try? AudioExceptions.run { engine.inputNode.removeTap(onBus: 0) }
            isTapInstalled = false
        }
        try? AudioExceptions.run { engine.stop() }
    }

    /// Stops after a long silence, so a microphone left on is not left on.
    private func watchForSilence() {
        watchTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                guard let self else { return }
                if self.dictationState == .listening, Date().timeIntervalSince(self.lastHeardAt) > Self.silentSecondsMost {
                    self.stop()
                    return
                }
            }
        }
    }

    /// A phone call or another app taking the microphone ends dictation.
    private func watchForInterruption() {
        interruptionObserver = NotificationCenter.default.addObserver(
            forName: AVAudioSession.interruptionNotification, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.stop() }
        }
    }

    // MARK: The socket

    private func listen(connection: Connection, place: Place, sampleRate: Int) async {
        do {
            let token = try await connection.accessToken()
            let server = await connection.account.server
            guard dictationState != .idle else { return }
            var components = URLComponents(url: server.endpoint("/api/v1/agent/voice"), resolvingAgainstBaseURL: false)!
            components.scheme = components.scheme == "http" ? "ws" : "wss"
            let socket = URLSession.shared.webSocketTask(with: components.url!)
            self.socket = socket
            socket.resume()
            let hello: [String: Any] = [
                "voiceEvent": "hello", "authorization": "Bearer \(token)", "purpose": "dictation", "dictationPlace": place.rawValue,
                "captureSettings": ["echoCancellation": false, "noiseSuppression": false, "autoGainControl": false, "sampleRate": sampleRate],
            ]
            let data = try JSONSerialization.data(withJSONObject: hello)
            try await socket.send(.string(String(decoding: data, as: UTF8.self)))
            while dictationState != .idle {
                let message = try await socket.receive()
                if case .string(let received) = message,
                    let event = try? JSONDecoder().decode(VoiceEvent.self, from: Data(received.utf8))
                {
                    handle(event)
                }
            }
        } catch {
            if dictationState != .idle {
                finish(problemText: String(localized: "Dictation lost its connection."))
            }
        }
    }

    private func handle(_ event: VoiceEvent) {
        switch event.voiceEvent {
        case "ready":
            capture.socket = socket
            dictationState = .listening
            lastHeardAt = Date()
        case "speechStarted":
            dictationState = .hearing
            lastHeardAt = Date()
        case "speechStopped":
            if dictationState == .hearing { dictationState = .listening }
            lastHeardAt = Date()
        case "transcriptDelta":
            captionText += event.transcriptText ?? ""
            lastHeardAt = Date()
        case "transcriptFinal":
            let words = (event.transcriptText ?? captionText).trimmingCharacters(in: .whitespacesAndNewlines)
            captionText = ""
            lastHeardAt = Date()
            if !words.isEmpty { append(words) }
        case "transcriptFailed":
            captionText = ""
        case "problem":
            problemText = event.errorMessage
        case "refused", "error":
            finish(problemText: event.errorMessage ?? String(localized: "Dictation lost its connection."))
        default:
            break
        }
    }

    /// Puts words at the end of the text, a space after what is there.
    private func append(_ words: String) {
        guard let text else { return }
        text.wrappedValue = Dictation.appending(words, to: text.wrappedValue)
    }

    /// The text with the words after it, a space between unless the text is
    /// empty or already ends in white space.
    static func appending(_ words: String, to existing: String) -> String {
        guard let last = existing.last, !last.isWhitespace else { return existing + words }
        return existing + " " + words
    }
}

/// DictationButton is the microphone that starts and stops dictating: a
/// symbol that pulses while the microphone is listening.
struct DictationButton: View {
    let dictation: Dictation
    let action: () -> Void
    @Environment(\.accessibilityReduceMotion) private var isMotionReduced
    @ScaledMetric(relativeTo: .body) private var scaledButtonDiameterPoints: CGFloat = 36

    var body: some View {
        Button(action: action) {
            Image(systemName: dictation.isDictating ? "mic.fill" : "mic")
                .font(.body.weight(.medium))
                .foregroundStyle(dictation.isDictating ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                .symbolEffect(.pulse, isActive: dictation.dictationState == .hearing && !isMotionReduced)
                .frame(width: min(scaledButtonDiameterPoints, 60), height: min(scaledButtonDiameterPoints, 60))
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(.rect)
        }
        .accessibilityLabel(dictation.isDictating ? Text("Stop dictating") : Text("Dictate"))
    }
}
