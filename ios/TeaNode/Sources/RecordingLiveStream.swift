import AVFoundation
import Foundation
import Observation
import TeaNodeKit

/// RecordingLiveStream sends the microphone to the server while a recording is captured and hears the words back, to show
/// them as they are said. It is best effort: the durable recording is written by the capture alone, and nothing here can
/// stop it or make it fail. If the socket cannot connect or the server refuses, the recording goes on without live words,
/// and the stream connects again after a growing wait.
///
/// The audio is the recording's own capture (`RecordingEngineCapture`), whose tap hands each buffer to `sender` as well as to the
/// files. `CaptureSender` converts it to 24000 Hz mono 16-bit, as voice mode sends it, and sends it to
/// `/api/v1/recordings/{id}/live`, whose hello says where on the recording's timeline the stream begins.
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

    /// Takes the microphone's buffers from the recording's capture, converts them and sends them while a socket is attached.
    let sender = CaptureSender()
    private var socket: URLSessionWebSocketTask?
    private var runTask: Task<Void, Never>?

    /// A new recording begins: nothing heard before belongs to it.
    func reset() {
        heardUtterances = []
        partialText = ""
    }

    /// Starts streaming for the recording the server knows as `recordingId`. `timelineMS` says where on the timeline the
    /// recording is now.
    func begin(connection: Connection, recordingId: String, timelineMS: @escaping @MainActor () -> Int64) {
        guard runTask == nil else { return }
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
}
