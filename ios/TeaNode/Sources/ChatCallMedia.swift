import AVFoundation
import Foundation
import TeaNodeKit
import WebRTC

/// ChatCallConnectionState is how the path to the media server stands.
enum ChatCallConnectionState: Sendable {
    case connecting
    case connected
    case disconnected
    case failed
}

/// ChatCallPeerEvent is what the peer connection tells, handed from the
/// thread it calls on to the main actor.
private enum ChatCallPeerEvent: Sendable {
    case gathering(isComplete: Bool)
    case connection(ChatCallConnectionState)
}

/// ChatCallPeerDelegate is the peer connection's delegate. WebRTC calls it on
/// a thread of its own, so it holds no state of the app: it passes the two
/// things the call waits for (the local addresses gathered, the path found or
/// lost) through a stream that ChatCallMedia reads on the main actor. The
/// other callbacks are required by the protocol and need nothing: the remote
/// tracks are found from the offers (ChatCallMedia.refreshRemoteTracks).
private final class ChatCallPeerDelegate: NSObject, RTCPeerConnectionDelegate, @unchecked Sendable {
    private let events: AsyncStream<ChatCallPeerEvent>.Continuation

    init(events: AsyncStream<ChatCallPeerEvent>.Continuation) {
        self.events = events
        super.init()
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}

    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {
        switch newState {
        case .connected, .completed: events.yield(.connection(.connected))
        case .disconnected: events.yield(.connection(.disconnected))
        case .failed, .closed: events.yield(.connection(.failed))
        default: events.yield(.connection(.connecting))
        }
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {
        events.yield(.gathering(isComplete: newState == .complete))
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}
}

/// ChatCallMedia is a phone's peer connection to the call's media server, as
/// docs/subsystems/chat-calls.md describes it: one connection with no ICE
/// servers (the server is an ICE-lite agent that answers whatever reaches it),
/// the microphone, and a video transceiver whose track is the camera when it
/// is on and none when it is off. The order of what is said to the server is
/// ChatCallSignaling's; this does each step.
@MainActor
final class ChatCallMedia {
    enum MediaError: LocalizedError {
        case cannotCreate
        case noDescription

        var errorDescription: String? {
            String(localized: "The call lost its connection.")
        }
    }

    /// One factory for the app, which WebRTC wants: it owns the audio device.
    private static let factory = RTCPeerConnectionFactory(
        encoderFactory: RTCDefaultVideoEncoderFactory(), decoderFactory: RTCDefaultVideoDecoderFactory())

    /// How long the local addresses are waited for before the offer goes with
    /// the ones found.
    private static let gatheringWait = Duration.seconds(5)

    private let peerConnection: RTCPeerConnection
    private let delegate: ChatCallPeerDelegate
    private let audioTrack: RTCAudioTrack
    private let videoTransceiver: RTCRtpTransceiver
    private var videoSource: RTCVideoSource?
    private var capturer: RTCCameraVideoCapturer?
    private var eventsTask: Task<Void, Never>?
    private var gatheringContinuation: CheckedContinuation<Void, Never>?

    /// The camera's track while the camera is on; the preview draws it.
    private(set) var localVideoTrack: RTCVideoTrack?
    private var cameraTrack: RTCVideoTrack?

    /// The tracks of the others' pictures, by the user id of whoever sends
    /// them (the track's stream id).
    private(set) var remoteVideoTracks: [String: RTCVideoTrack] = [:]
    private var userIdByMid: [String: String] = [:]

    /// Told when the path to the server is found, lost or failed, and when
    /// the others' pictures change.
    var onConnectionState: (@MainActor (ChatCallConnectionState) -> Void)?
    var onRemoteVideoChange: (@MainActor () -> Void)?

    init(streamId: String) throws {
        let (events, continuation) = AsyncStream.makeStream(of: ChatCallPeerEvent.self)
        let delegate = ChatCallPeerDelegate(events: continuation)
        let configuration = RTCConfiguration()
        configuration.iceServers = []
        configuration.sdpSemantics = .unifiedPlan
        configuration.continualGatheringPolicy = .gatherOnce
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: ["DtlsSrtpKeyAgreement": "true"])
        // Typed as optional: WebRTC says it may fail to make one.
        let madeConnection: RTCPeerConnection? = Self.factory.peerConnection(with: configuration, constraints: constraints, delegate: delegate)
        guard let peerConnection = madeConnection else {
            throw MediaError.cannotCreate
        }
        let audioSource = Self.factory.audioSource(with: RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil))
        let audioTrack = Self.factory.audioTrack(with: audioSource, trackId: "audio-\(streamId)")
        peerConnection.add(audioTrack, streamIds: [streamId])
        let videoInit = RTCRtpTransceiverInit()
        videoInit.direction = .sendRecv
        videoInit.streamIds = [streamId]
        videoInit.sendEncodings = Self.videoLayers()
        let addedTransceiver: RTCRtpTransceiver? = peerConnection.addTransceiver(of: .video, init: videoInit)
        guard let videoTransceiver = addedTransceiver else {
            throw MediaError.cannotCreate
        }
        self.peerConnection = peerConnection
        self.delegate = delegate
        self.audioTrack = audioTrack
        self.videoTransceiver = videoTransceiver
        eventsTask = Task { [weak self] in
            for await event in events {
                self?.handle(event)
            }
        }
    }

    // MARK: Events from the connection

    private func handle(_ event: ChatCallPeerEvent) {
        switch event {
        case .gathering(let isComplete):
            if isComplete { finishWaitingForGathering() }
        case .connection(let state):
            onConnectionState?(state)
        }
    }

    private func finishWaitingForGathering() {
        gatheringContinuation?.resume()
        gatheringContinuation = nil
    }

    /// Waits until the local addresses are all found (the delegate's gathering
    /// state), or a few seconds, so that the offer or answer carries them: the
    /// server is sent no candidates of their own.
    private func waitForGathering() async {
        if peerConnection.iceGatheringState == .complete { return }
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            gatheringContinuation = continuation
            Task { [weak self] in
                try? await Task.sleep(for: Self.gatheringWait)
                self?.finishWaitingForGathering()
            }
        }
    }

    // MARK: Offers and answers

    private static var offerConstraints: RTCMediaConstraints {
        RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
    }

    private func createOffer() async throws -> String {
        let constraints = Self.offerConstraints
        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            peerConnection.offer(for: constraints) { description, error in
                if let description {
                    continuation.resume(returning: description.sdp)
                } else {
                    continuation.resume(throwing: error ?? MediaError.noDescription)
                }
            }
        }
    }

    private func createAnswer() async throws -> String {
        let constraints = Self.offerConstraints
        return try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<String, Error>) in
            peerConnection.answer(for: constraints) { description, error in
                if let description {
                    continuation.resume(returning: description.sdp)
                } else {
                    continuation.resume(throwing: error ?? MediaError.noDescription)
                }
            }
        }
    }

    private func setLocalDescription(type: RTCSdpType, sdp: String) async throws {
        let description = RTCSessionDescription(type: type, sdp: sdp)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            peerConnection.setLocalDescription(description) { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume()
                }
            }
        }
    }

    private func setRemoteDescription(type: RTCSdpType, sdp: String) async throws {
        let description = RTCSessionDescription(type: type, sdp: sdp)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            peerConnection.setRemoteDescription(description) { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume()
                }
            }
        }
    }

    /// The device's offer for JoinChatCallMedia, with its addresses in it.
    func makeJoinOffer() async throws -> String {
        let offer = try await createOffer()
        try await setLocalDescription(type: .offer, sdp: offer)
        await waitForGathering()
        return peerConnection.localDescription?.sdp ?? offer
    }

    /// Sets the server's answer to the join.
    func setJoinAnswer(_ answerSdp: String) async throws {
        try await setRemoteDescription(type: .answer, sdp: answerSdp)
        refreshRemoteTracks(from: answerSdp)
    }

    /// Answers one of the server's offers: sets it, makes the answer and sets
    /// that, and returns it to be sent with AnswerChatCallMedia.
    func answer(offerSdp: String) async throws -> String {
        try await setRemoteDescription(type: .offer, sdp: offerSdp)
        let answer = try await createAnswer()
        try await setLocalDescription(type: .answer, sdp: answer)
        await waitForGathering()
        refreshRemoteTracks(from: offerSdp)
        return peerConnection.localDescription?.sdp ?? answer
    }

    /// Finds the others' pictures. Each track belongs to the person whose
    /// user id is its stream id, which the offer names for every media
    /// section (`a=msid`); the section's own transceiver holds the track.
    private func refreshRemoteTracks(from sdp: String) {
        for section in ChatCallSDP.sections(in: sdp) {
            userIdByMid[section.mid] = section.isSending ? section.streamId : nil
        }
        var tracks: [String: RTCVideoTrack] = [:]
        for transceiver in peerConnection.transceivers {
            guard transceiver.mediaType == .video, let userId = userIdByMid[transceiver.mid],
                let track = transceiver.receiver.track as? RTCVideoTrack
            else { continue }
            tracks[userId] = track
        }
        remoteVideoTracks = tracks
        onRemoteVideoChange?()
    }

    // MARK: The person's own microphone and camera

    /// Stops or starts sending the microphone's sound.
    func setMuted(_ isMuted: Bool) {
        audioTrack.isEnabled = !isMuted
    }

    /// Turns the camera on or off: the video sender's track is replaced, with
    /// the camera's or with none, so no new offer is needed. Returns false when
    /// the phone has no such camera.
    @discardableResult
    func setCamera(isOn: Bool, usesFrontCamera: Bool) -> Bool {
        guard isOn else {
            stopCapture()
            videoTransceiver.sender.track = nil
            localVideoTrack = nil
            return true
        }
        let source = videoSource ?? Self.factory.videoSource()
        videoSource = source
        let track = cameraTrack ?? Self.factory.videoTrack(with: source, trackId: "video-camera")
        cameraTrack = track
        guard startCapture(into: source, usesFrontCamera: usesFrontCamera) else { return false }
        videoTransceiver.sender.track = track
        localVideoTrack = track
        return true
    }

    /// Changes to the other camera while the camera is on.
    @discardableResult
    func flipCamera(usesFrontCamera: Bool) -> Bool {
        guard let videoSource else { return false }
        stopCapture()
        if startCapture(into: videoSource, usesFrontCamera: usesFrontCamera) { return true }
        // The other camera would not open: the one before goes on again.
        _ = startCapture(into: videoSource, usesFrontCamera: !usesFrontCamera)
        return false
    }

    private func startCapture(into source: RTCVideoSource, usesFrontCamera: Bool) -> Bool {
        let wantedPosition: AVCaptureDevice.Position = usesFrontCamera ? .front : .back
        let devices = RTCCameraVideoCapturer.captureDevices()
        guard let device = devices.first(where: { $0.position == wantedPosition }) ?? devices.first else { return false }
        let capturer = self.capturer ?? RTCCameraVideoCapturer(delegate: source)
        self.capturer = capturer
        // The format nearest 960 by 540, which looks good in a tile and is
        // light enough for a phone to send; at most 30 frames a second.
        let formats = RTCCameraVideoCapturer.supportedFormats(for: device)
        guard
            let format = formats.min(by: { first, second in
                Self.distance(of: first, from: 960 * 540) < Self.distance(of: second, from: 960 * 540)
            })
        else { return false }
        let largestRate = format.videoSupportedFrameRateRanges.map(\.maxFrameRate).max() ?? 30
        let framesPerSecond = Int(min(30, largestRate))
        capturer.startCapture(with: device, format: format, fps: framesPerSecond) { _ in }
        return true
    }

    /// The two sizes the camera is sent in, named as the server knows them
    /// (internal/call/video.go): q, a third of the picture, and f, the whole
    /// of it. The server passes on the whole one only to those who feature
    /// this person.
    private static func videoLayers() -> [RTCRtpEncodingParameters] {
        let small = RTCRtpEncodingParameters()
        small.rid = "q"
        small.scaleResolutionDownBy = NSNumber(value: 3)
        small.maxBitrateBps = NSNumber(value: 200_000)
        let whole = RTCRtpEncodingParameters()
        whole.rid = "f"
        whole.maxBitrateBps = NSNumber(value: 1_200_000)
        return [small, whole]
    }

    private static func distance(of format: AVCaptureDevice.Format, from pixelCount: Int) -> Int {
        let size = CMVideoFormatDescriptionGetDimensions(format.formatDescription)
        return abs(Int(size.width) * Int(size.height) - pixelCount)
    }

    private func stopCapture() {
        capturer?.stopCapture { }
    }

    /// Ends the connection and lets go of the camera and the microphone.
    func close() {
        eventsTask?.cancel()
        eventsTask = nil
        finishWaitingForGathering()
        stopCapture()
        videoTransceiver.sender.track = nil
        localVideoTrack = nil
        remoteVideoTracks = [:]
        peerConnection.close()
        onConnectionState = nil
        onRemoteVideoChange = nil
    }
}
