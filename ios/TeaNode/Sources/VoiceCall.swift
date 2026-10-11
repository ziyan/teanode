import AVFoundation
import CallKit
import Foundation
import Observation
import TeaNodeKit

/// VoiceCall is a call with the agent: what the person says is heard by the
/// server and becomes a turn, and the agent's answers are read back to them.
///
/// It is a CallKit call, so iOS treats it as one: it keeps the microphone
/// with the screen locked, shows on the lock screen, goes to AirPods and the
/// car, and gives way to a phone call. The audio runs through the system's
/// voice processing, which cancels the answer's own sound from the
/// microphone, so the person can talk over an answer to cut in.
@MainActor
@Observable
final class VoiceCall: NSObject {
    enum CallState: Equatable {
        case starting, listening, hearing, working, speaking, ended
    }

    private(set) var callState: CallState = .starting
    private(set) var captionText = ""
    private(set) var problemText: String?
    /// How loud the microphone is, for the meter: 0 to 1.
    private(set) var level: Float = 0
    var isMuted = false {
        didSet { capture.isMuted = isMuted }
    }

    private let connection: Connection
    private let model: ConversationModel
    private let sampleRate: Double
    private let agentName: String

    private var socket: URLSessionWebSocketTask?
    private let engine = AVAudioEngine()
    private let player = AVAudioPlayerNode()
    /// The system's voice processing cancels the answer's sound from the
    /// microphone; on a phone where it gives only silence, the call goes on
    /// without it.
    private var isVoiceProcessing = true
    private var isSilenceRetried = false
    private var configurationObserver: (any NSObjectProtocol)?
    private var isTapInstalled = false
    private var engineStartedAt = Date.distantPast
    /// Answers arrive as mono 16-bit PCM at 24000 Hz.
    private let answerFormat = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: 24000, channels: 1, interleaved: false)!
    private let capture = CaptureSender()

    private let callUUID = UUID()
    private let callController = CXCallController()
    private var provider: CXProvider?
    private var isAudioRunning = false

    /// The turns this call started, whose answers are read out.
    private var spokenRuns: Set<String> = []
    /// Sentences waiting to be spoken, and those asked for or playing.
    private var waitingSentences: [String] = []
    private var segments: [Segment] = []
    private var segmentCount = 0
    private var meterTask: Task<Void, Never>?
    private var stillHereTask: Task<Void, Never>?

    /// How many sentences are asked for ahead of the one playing.
    static let ahead = 3

    struct Segment {
        let segmentId: String
        let text: String
        var isDone = false
        var isPlayed = false
    }

    init(connection: Connection, model: ConversationModel, sampleRate: Int, agentName: String) {
        self.connection = connection
        self.model = model
        self.sampleRate = Double(sampleRate)
        self.agentName = agentName
        super.init()
    }

    // MARK: Starting and ending

    /// Starts the call: CallKit first, and the audio once CallKit has the
    /// audio session ready; without CallKit, the audio at once.
    func start() {
        Task {
            guard await AVAudioApplication.requestRecordPermission() else {
                end(problemText: String(localized: "TeaNode may not use the microphone. Allow it in Settings, then call again."))
                return
            }
            startWithCallKit()
        }
    }

    private func startWithCallKit() {
        let configuration = CXProviderConfiguration()
        configuration.supportsVideo = false
        configuration.maximumCallGroups = 1
        configuration.maximumCallsPerCallGroup = 1
        configuration.supportedHandleTypes = [.generic]
        configuration.includesCallsInRecents = false
        let provider = CXProvider(configuration: configuration)
        provider.setDelegate(self, queue: nil)
        self.provider = provider
        let handle = CXHandle(type: .generic, value: agentName)
        let action = CXStartCallAction(call: callUUID, handle: handle)
        action.isVideo = false
        callController.request(CXTransaction(action: action)) { error in
            if error != nil {
                Task { @MainActor in self.startWithoutCallKit() }
            }
        }
    }

    private func startWithoutCallKit() {
        provider = nil
        do {
            try configureAudioSession()
            try AVAudioSession.sharedInstance().setActive(true)
            startAudio()
        } catch {
            end(problemText: String(localized: "The microphone could not be started: \(error.localizedDescription)"))
        }
    }

    /// Ends the call: hung up by the person, by CallKit, or by a problem.
    func hangUp() {
        if provider != nil {
            callController.request(CXTransaction(action: CXEndCallAction(call: callUUID))) { _ in }
        }
        end(problemText: nil)
    }

    private func end(problemText: String?) {
        guard callState != .ended else { return }
        if let problemText { self.problemText = problemText }
        callState = .ended
        meterTask?.cancel()
        stillHereTask?.cancel()
        if let socket {
            socket.send(.string(#"{"voiceEvent":"stop"}"#)) { _ in }
            socket.cancel(with: .goingAway, reason: nil)
        }
        socket = nil
        capture.socket = nil
        stopEngine()
        if let configurationObserver {
            NotificationCenter.default.removeObserver(configurationObserver)
            self.configurationObserver = nil
        }
        if provider == nil {
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        } else {
            provider?.reportCall(with: callUUID, endedAt: Date(), reason: problemText == nil ? .remoteEnded : .failed)
        }
        model.call = nil
    }

    private func configureAudioSession() throws {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.defaultToSpeaker, .allowBluetoothHFP])
    }

    // MARK: Audio and the socket

    private func startAudio() {
        guard !isAudioRunning else { return }
        guard startEngine() else { return }
        openSocket()
        meterTask = Task { [weak self] in
            var listeningSince: Date?
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(100))
                guard let self else { return }
                self.level = self.capture.level
                self.watchForSilence(&listeningSince)
            }
        }
        // While muted nothing is sent; the server is told every twenty
        // seconds that the call is still there.
        stillHereTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(20))
                guard let self else { return }
                if self.isMuted {
                    self.socket?.send(.string(#"{"voiceEvent":"stillHere"}"#)) { _ in }
                }
            }
        }
    }

    /// A microphone that gives nothing for three seconds once the server
    /// listens (no buffers, or buffers of exact zeros, which a quiet room
    /// never is) is started again without voice processing; if that is
    /// silent too, the person is told.
    private func watchForSilence(_ listeningSince: inout Date?) {
        guard callState == .listening || callState == .hearing, !isMuted else {
            listeningSince = nil
            return
        }
        let since = listeningSince ?? Date()
        listeningSince = since
        guard Date().timeIntervalSince(since) > 3, capture.loudestLevel < 0.00001 else { return }
        listeningSince = nil
        if !isSilenceRetried {
            isSilenceRetried = true
            isVoiceProcessing = false
            restartEngine()
        } else if problemText == nil {
            problemText = String(localized: "The microphone gives no sound. Check that nothing else is using it, or try AirPods or the speaker.")
        }
    }

    /// Starts the audio again, as iOS asks after the route changes: to
    /// AirPods, the speaker or a car. The same engine is used: a second one
    /// would contend for the voice processing the first still holds.
    private func restartEngine() {
        guard callState != .ended else { return }
        // Turning voice processing on reports a change of its own just as
        // the engine starts; that one needs nothing.
        guard Date().timeIntervalSince(engineStartedAt) > 1 else { return }
        stopEngine()
        // The new route may not take voice processing as the old one did:
        // without it, the call goes on rather than ending.
        if !startEngine(isLastTry: !isVoiceProcessing) {
            isVoiceProcessing = false
            stopEngine()
            _ = startEngine()
        }
    }

    /// Tries again in a moment, while the route is still settling.
    private func restartSoon() {
        Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(500))
            self?.restartEngine()
        }
    }

    private func stopEngine() {
        let engine = engine
        let player = player
        if isTapInstalled {
            try? AudioExceptions.run { engine.inputNode.removeTap(onBus: 0) }
            isTapInstalled = false
        }
        guard isAudioRunning else { return }
        try? AudioExceptions.run {
            player.stop()
            engine.stop()
        }
        isAudioRunning = false
    }

    /// Starts the engine and the microphone's tap. Failing, it ends the call
    /// unless another try follows.
    private func startEngine(isLastTry: Bool = true) -> Bool {
        capture.resetLoudest()
        do {
            let input = engine.inputNode
            if input.isVoiceProcessingEnabled != isVoiceProcessing {
                let isWanted = isVoiceProcessing
                try AudioExceptions.run { try input.setVoiceProcessingEnabled(isWanted) }
            }
            if player.engine == nil {
                let player = player
                let engine = engine
                let answerFormat = answerFormat
                try AudioExceptions.run {
                    engine.attach(player)
                    engine.connect(player, to: engine.mainMixerNode, format: answerFormat)
                }
            }
            let inputFormat = input.outputFormat(forBus: 0)
            // Between routes the microphone can report no format at all; a
            // tap installed then would stop the app.
            guard inputFormat.sampleRate > 0, inputFormat.channelCount > 0 else {
                restartSoon()
                return true
            }
            guard let sendFormat = AVAudioFormat(commonFormat: .pcmFormatInt16, sampleRate: sampleRate, channels: 1, interleaved: true),
                let converter = AVAudioConverter(from: inputFormat, to: sendFormat)
            else {
                throw TeaNodeError.refused(String(localized: "The microphone's format cannot be sent."))
            }
            capture.converter = converter
            capture.sendFormat = sendFormat
            // A tenth of a second at a time, as the dashboard sends it.
            let frames = AVAudioFrameCount(inputFormat.sampleRate / 10)
            let sender = capture
            try AudioExceptions.run {
                input.installTap(onBus: 0, bufferSize: frames, format: inputFormat) { buffer, _ in
                    sender.send(buffer)
                }
            }
            isTapInstalled = true
            let engine = engine
            let player = player
            try AudioExceptions.run {
                try engine.start()
                player.play()
            }
            isAudioRunning = true
            engineStartedAt = Date()
        } catch {
            if isLastTry {
                end(problemText: String(localized: "The microphone could not be started: \(error.localizedDescription)"))
            }
            return false
        }
        if configurationObserver == nil {
            configurationObserver = NotificationCenter.default.addObserver(
                forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main
            ) { [weak self] _ in
                MainActor.assumeIsolated { self?.restartEngine() }
            }
        }
        return true
    }

    private func openSocket() {
        Task {
            do {
                let token = try await connection.accessToken()
                let server = await connection.account.server
                var components = URLComponents(url: server.endpoint("/api/v1/agent/voice"), resolvingAgainstBaseURL: false)!
                components.scheme = components.scheme == "http" ? "ws" : "wss"
                let socket = URLSession.shared.webSocketTask(with: components.url!)
                self.socket = socket
                socket.resume()
                let hello: [String: Any] = [
                    "voiceEvent": "hello", "authorization": "Bearer \(token)",
                    "captureSettings": [
                        "echoCancellation": true, "noiseSuppression": true, "autoGainControl": true, "sampleRate": Int(sampleRate),
                    ],
                ]
                let data = try JSONSerialization.data(withJSONObject: hello)
                try await socket.send(.string(String(decoding: data, as: UTF8.self)))
                while callState != .ended {
                    let message = try await socket.receive()
                    if case .string(let text) = message, let event = try? JSONDecoder().decode(VoiceEvent.self, from: Data(text.utf8)) {
                        handle(event)
                    }
                }
            } catch {
                if callState != .ended {
                    end(problemText: String(localized: "The call lost its connection."))
                }
            }
        }
    }

    private func handle(_ event: VoiceEvent) {
        switch event.voiceEvent {
        case "ready":
            capture.socket = socket
            callState = .listening
        case "speechStarted":
            callState = .hearing
            // Talking over an answer turns it down at once; the words that
            // follow decide whether it stops.
            if isSpeakingAnswer { player.volume = 0.15 }
        case "speechStopped":
            if callState == .hearing { callState = spokenRuns.isEmpty ? .listening : .working }
        case "transcriptDelta":
            captionText += event.transcriptText ?? ""
        case "transcriptFinal":
            let text = (event.transcriptText ?? captionText).trimmingCharacters(in: .whitespacesAndNewlines)
            captionText = ""
            guard !text.isEmpty else {
                player.volume = 1
                return
            }
            send(text)
        case "transcriptFailed":
            captionText = ""
            player.volume = 1
        case "answerAudio":
            if let segmentId = event.answerSegmentId, let audio = event.answerAudio {
                play(audio, segmentId: segmentId)
            }
        case "answerAudioDone", "answerAudioFailed":
            if let index = segments.firstIndex(where: { $0.segmentId == event.answerSegmentId }) {
                segments[index].isDone = true
            }
            requestSentences()
        case "problem":
            problemText = event.errorMessage
        case "refused", "error":
            end(problemText: event.errorMessage ?? String(localized: "The call lost its connection."))
        default:
            break
        }
    }

    // MARK: Turns

    /// Sends what the person said as a turn. Said over an answer, the
    /// answer stops, and the agent is told how much of it was heard.
    private func send(_ text: String) {
        var interrupted: (heardText: String, unheardText: String)?
        if isSpeakingAnswer || !waitingSentences.isEmpty {
            let heard = segments.filter(\.isPlayed).map(\.text).joined(separator: " ")
            let unheard = (segments.filter { !$0.isPlayed }.map(\.text) + waitingSentences).joined(separator: " ")
            interrupted = (heard, unheard)
            stopSpeaking()
        }
        player.volume = 1
        callState = .working
        Task {
            if let runId = await model.sendSpoken(text, interruptedAnswer: interrupted) {
                spokenRuns.insert(runId)
            }
        }
    }

    /// What the conversation's feed says, for the turns this call started.
    func follow(_ event: AgentEvent) {
        guard spokenRuns.contains(event.runId), callState != .ended else { return }
        switch event.kind {
        case "message":
            speak(SpokenText.of(event.text ?? ""))
        case "question":
            let choices = (event.text ?? "").split(separator: "\n").joined(separator: ", ")
            speak([event.note ?? "", choices].filter { !$0.isEmpty }.joined(separator: " "))
        case "confirmation":
            speak(String(localized: "I need your approval on screen: \(event.note ?? "")"))
        case "done", "error":
            spokenRuns.remove(event.runId)
            if !isSpeakingAnswer, waitingSentences.isEmpty, callState == .working { callState = .listening }
        default:
            break
        }
    }

    // MARK: Speaking

    private var isSpeakingAnswer: Bool {
        segments.contains { !$0.isPlayed }
    }

    private func speak(_ text: String) {
        let split = SpokenText.sentences(in: text + "\n")
        waitingSentences += split.sentences
        requestSentences()
    }

    /// Asks the server to say the next sentences, a few ahead of the one
    /// playing.
    private func requestSentences() {
        while !waitingSentences.isEmpty, segments.filter({ !$0.isDone }).count < Self.ahead {
            let text = waitingSentences.removeFirst()
            segmentCount += 1
            let segmentId = "\(callUUID.uuidString)-\(segmentCount)"
            segments.append(Segment(segmentId: segmentId, text: text))
            let request: [String: Any] = ["voiceEvent": "speakAnswer", "answerSegmentId": segmentId, "answerText": text]
            if let data = try? JSONSerialization.data(withJSONObject: request) {
                socket?.send(.string(String(decoding: data, as: UTF8.self))) { _ in }
            }
        }
    }

    private func play(_ base64: String, segmentId: String) {
        guard let data = Data(base64Encoded: base64), data.count >= 2 else { return }
        let count = data.count / 2
        guard let buffer = AVAudioPCMBuffer(pcmFormat: answerFormat, frameCapacity: AVAudioFrameCount(count)),
            let channel = buffer.floatChannelData?[0]
        else { return }
        buffer.frameLength = AVAudioFrameCount(count)
        data.withUnsafeBytes { raw in
            let samples = raw.bindMemory(to: Int16.self)
            for index in 0..<count {
                channel[index] = Float(Int16(littleEndian: samples[index])) / Float(Int16.max)
            }
        }
        callState = .speaking
        player.scheduleBuffer(buffer) { [weak self] in
            Task { @MainActor in self?.played(segmentId) }
        }
    }

    private func played(_ segmentId: String) {
        guard let index = segments.firstIndex(where: { $0.segmentId == segmentId }) else { return }
        // A segment's last buffer has played once it is done arriving.
        if segments[index].isDone {
            segments[index].isPlayed = true
            segments.removeAll { $0.isPlayed && $0.segmentId != segmentId }
        }
        if !isSpeakingAnswer, waitingSentences.isEmpty {
            callState = spokenRuns.isEmpty ? .listening : .working
        }
    }

    /// Cuts an answer short, as a tap on the dashboard's meter does.
    func cutAnswer() {
        guard isSpeakingAnswer || !waitingSentences.isEmpty else { return }
        stopSpeaking()
        player.volume = 1
        callState = spokenRuns.isEmpty ? .listening : .working
    }

    private func stopSpeaking() {
        let pending = segments.filter { !$0.isPlayed }.map(\.segmentId)
        if !pending.isEmpty, let data = try? JSONSerialization.data(withJSONObject: ["voiceEvent": "cancelAnswer", "answerSegmentIds": pending]) {
            socket?.send(.string(String(decoding: data, as: UTF8.self))) { _ in }
        }
        player.stop()
        player.play()
        segments = []
        waitingSentences = []
    }
}

extension VoiceCall: @preconcurrency CXProviderDelegate {
    func providerDidReset(_ provider: CXProvider) {
        end(problemText: nil)
    }

    func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        do {
            try configureAudioSession()
            action.fulfill()
            provider.reportOutgoingCall(with: callUUID, startedConnectingAt: Date())
            provider.reportOutgoingCall(with: callUUID, connectedAt: Date())
        } catch {
            action.fail()
        }
    }

    func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        action.fulfill()
        end(problemText: nil)
    }

    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        isMuted = action.isMuted
        action.fulfill()
    }

    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        startAudio()
    }

    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {}
}

/// CaptureSender takes the microphone's buffers on the audio thread,
/// converts them to what the server takes, and sends them; it keeps how
/// loud they were for the meter.
final class CaptureSender: @unchecked Sendable {
    private let lock = NSLock()
    private var _socket: URLSessionWebSocketTask?
    private var _isMuted = false
    private var _level: Float = 0
    var converter: AVAudioConverter?
    var sendFormat: AVAudioFormat?

    var socket: URLSessionWebSocketTask? {
        get { lock.withLock { _socket } }
        set { lock.withLock { _socket = newValue } }
    }

    var isMuted: Bool {
        get { lock.withLock { _isMuted } }
        set { lock.withLock { _isMuted = newValue } }
    }

    var level: Float { lock.withLock { _level } }

    private var _loudestLevel: Float = 0
    /// The loudest the microphone has been since the engine started; zero
    /// means it has given nothing at all.
    var loudestLevel: Float { lock.withLock { _loudestLevel } }

    func resetLoudest() {
        lock.withLock { _loudestLevel = 0 }
    }

    func send(_ buffer: AVAudioPCMBuffer) {
        if let channel = buffer.floatChannelData?[0], buffer.frameLength > 0 {
            var sum: Float = 0
            for index in 0..<Int(buffer.frameLength) { sum += channel[index] * channel[index] }
            let rootMeanSquare = (sum / Float(buffer.frameLength)).squareRoot()
            lock.withLock {
                _level = min(1, rootMeanSquare * 8)
                _loudestLevel = max(_loudestLevel, rootMeanSquare)
            }
        }
        guard let socket, !isMuted, let converter, let sendFormat else { return }
        let ratio = sendFormat.sampleRate / buffer.format.sampleRate
        let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 16
        guard let converted = AVAudioPCMBuffer(pcmFormat: sendFormat, frameCapacity: capacity) else { return }
        var isGiven = false
        var error: NSError?
        converter.convert(to: converted, error: &error) { _, status in
            if isGiven {
                status.pointee = .noDataNow
                return nil
            }
            isGiven = true
            status.pointee = .haveData
            return buffer
        }
        guard error == nil, converted.frameLength > 0, let samples = converted.int16ChannelData?[0] else { return }
        let data = Data(bytes: samples, count: Int(converted.frameLength) * 2)
        socket.send(.data(data)) { _ in }
    }
}

/// AudioExceptions runs AVFAudio work that may raise an Objective-C
/// exception, and throws it as an error: uncaught, it would end the app.
enum AudioExceptions {
    static func run(_ work: () throws -> Void) throws {
        var thrown: Error?
        try ExceptionCatcher.catchException {
            do {
                try work()
            } catch {
                thrown = error
            }
        }
        if let thrown { throw thrown }
    }
}
