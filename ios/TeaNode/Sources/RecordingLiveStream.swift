import AVFoundation
import Foundation
import Observation
import TeaNodeKit

/// RecordingLiveStream sends the microphone to the server while a recording is captured and hears the words back, to show
/// them as they are said. It is best effort: the durable recording is written by MeetingRecorder's AVAudioRecorder alone,
/// and nothing here can stop it or make it fail. If the engine will not start, the socket cannot connect or the server
/// refuses, the recording goes on without live words, and the stream connects again after a growing wait.
///
/// The engine's input tap runs beside the recorder. Its audio is converted to 24000 Hz mono 16-bit, as voice mode sends it
/// (`CaptureSender` does the conversion and the sending), and goes to `/api/v1/recordings/{id}/live`, whose hello says where
/// on the recording's timeline the stream begins.
@MainActor
@Observable
final class RecordingLiveStream {
    /// Whether the server has said it is listening: the cut policy cuts at utterance ends only while this is so.
    private(set) var isConnected = false
    /// Utterances the server finished hearing on this phone's streams, newest last.
    private(set) var heardUtterances: [RecordingLiveUtterance] = []
    /// The words of the utterance being said now, and where on the timeline it began.
    private(set) var partialText = ""
    private(set) var partialStartMS: Int64 = 0

    /// Called when the server hears an utterance end.
    @ObservationIgnored var didHearUtteranceEnd: (@MainActor () -> Void)?

    private let engine = AVAudioEngine()
    private let sender = CaptureSender()
    private var socket: URLSessionWebSocketTask?
    private var runTask: Task<Void, Never>?
    private var isTapInstalled = false
    private var isEngineRunning = false
    private var configurationObserver: (any NSObjectProtocol)?

    /// A new recording begins: nothing heard before belongs to it.
    func reset() {
        heardUtterances = []
        partialText = ""
    }

    /// Starts streaming for the recording the server knows as `recordingId`. `timelineMS` says where on the timeline the
    /// recording is now.
    func begin(connection: Connection, recordingId: String, timelineMS: @escaping @MainActor () -> Int64) {
        guard runTask == nil else { return }
        startEngine()
        runTask = Task { [weak self] in
            var failureCount = 0
            while !Task.isCancelled {
                guard let self else { return }
                let wasListening = await self.runSession(connection: connection, recordingId: recordingId, timelineMS: timelineMS)
                guard !Task.isCancelled else { return }
                failureCount = wasListening ? 1 : failureCount + 1
                try? await Task.sleep(for: .seconds(RecordingLiveBackoff.delaySeconds(afterFailureCount: failureCount)))
            }
        }
    }

    /// Stops streaming, for a pause, the stop or a failure of the recording.
    func end() {
        runTask?.cancel()
        runTask = nil
        closeSocket()
        stopEngine()
        if let configurationObserver {
            NotificationCenter.default.removeObserver(configurationObserver)
            self.configurationObserver = nil
        }
        partialText = ""
    }

    // MARK: The socket

    /// One connection, until it ends. True if the server was listening at some point.
    private func runSession(connection: Connection, recordingId: String, timelineMS: @MainActor () -> Int64) async -> Bool {
        var wasListening = false
        do {
            let token = try await connection.accessToken()
            let server = await connection.account.server
            var components = URLComponents(url: server.endpoint("/api/v1/recordings/\(recordingId)/live"), resolvingAgainstBaseURL: false)!
            components.scheme = components.scheme == "http" ? "ws" : "wss"
            let socket = URLSession.shared.webSocketTask(with: components.url!)
            self.socket = socket
            socket.resume()
            // The audio from here on is queued behind the hello, so the stream starts where the hello says: the server
            // reads the hello first, then waits for the transcription to open before it reads the audio.
            let hello: [String: Any] = ["voiceEvent": "hello", "authorization": "Bearer \(token)", "streamStartMS": timelineMS()]
            let data = try JSONSerialization.data(withJSONObject: hello)
            socket.send(.string(String(decoding: data, as: UTF8.self))) { @Sendable _ in }
            sender.socket = socket
            while !Task.isCancelled {
                let message = try await socket.receive()
                guard case .string(let text) = message, let event = try? JSONDecoder().decode(VoiceEvent.self, from: Data(text.utf8)) else {
                    continue
                }
                if event.voiceEvent == "ready" { wasListening = true }
                try handle(event, timelineMS: timelineMS())
            }
        } catch {}
        closeSocket()
        return wasListening
    }

    private func closeSocket() {
        sender.socket = nil
        if let socket {
            socket.send(.string(#"{"voiceEvent":"stop"}"#)) { @Sendable _ in }
            socket.cancel(with: .goingAway, reason: nil)
        }
        socket = nil
        isConnected = false
    }

    /// Throws for the events that end the session; the caller connects again after a wait.
    private func handle(_ event: VoiceEvent, timelineMS: Int64) throws {
        switch event.voiceEvent {
        case "ready":
            isConnected = true
        case "speechStarted":
            partialText = ""
            partialStartMS = event.utteranceStartMS ?? timelineMS
        case "speechStopped":
            didHearUtteranceEnd?()
        case "transcriptDelta":
            partialText += event.transcriptText ?? ""
        case "transcriptFinal":
            partialText = ""
            let text = (event.transcriptText ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if let utteranceId = event.utteranceId, !text.isEmpty, let startMS = event.utteranceStartMS, let endMS = event.utteranceEndMS {
                heardUtterances = RecordingLiveWords.appending(
                    RecordingLiveUtterance(utteranceId: utteranceId, utteranceStartMS: startMS, utteranceEndMS: endMS, transcriptText: text),
                    to: heardUtterances)
            }
        case "transcriptFailed":
            partialText = ""
        case "refused", "error":
            throw TeaNodeError.refused(event.errorMessage ?? "")
        default:
            break
        }
    }

    // MARK: The microphone

    private func startEngine() {
        guard !isEngineRunning else { return }
        do {
            let input = engine.inputNode
            let inputFormat = input.outputFormat(forBus: 0)
            // Between routes the microphone can report no format at all; a tap installed then would stop the app.
            guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0,
                let sendFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: 24000, channels: 1, interleaved: true),
                let converter = AVAudioConverter(from: inputFormat, to: sendFormat)
            else { return }
            sender.converter = converter
            sender.sendFormat = sendFormat
            // A tenth of a second at a time, as voice mode sends it.
            let frames = AVAudioFrameCount(inputFormat.sampleRate / 10)
            let sender = sender
            try AudioExceptions.run {
                input.installTap(onBus: 0, bufferSize: frames, format: inputFormat) { @Sendable buffer, _ in
                    sender.send(buffer)
                }
            }
            isTapInstalled = true
            let engine = engine
            try AudioExceptions.run { try engine.start() }
            isEngineRunning = true
        } catch {
            stopEngine()
            return
        }
        if configurationObserver == nil {
            configurationObserver = NotificationCenter.default.addObserver(
                forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main
            ) { [weak self] _ in
                // The route changed (AirPods, a car): the tap is put in again for the new format.
                MainActor.assumeIsolated {
                    guard let self, self.runTask != nil else { return }
                    self.stopEngine()
                    self.startEngine()
                }
            }
        }
    }

    private func stopEngine() {
        let engine = engine
        if isTapInstalled {
            try? AudioExceptions.run { engine.inputNode.removeTap(onBus: 0) }
            isTapInstalled = false
        }
        if isEngineRunning {
            try? AudioExceptions.run { engine.stop() }
            isEngineRunning = false
        }
    }
}
