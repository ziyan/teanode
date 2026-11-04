import AVFoundation
import CallKit
import Foundation
import Observation
import TeaNodeKit
import UIKit
import WebRTC

/// ChatCallSession is the call this phone is on, or is going onto: what the
/// call screen draws, and the connection to the media server behind it.
@MainActor
@Observable
final class ChatCallSession {
    enum Phase: Equatable {
        /// Asking the server, or finding the path to the media server.
        case connecting
        case connected
        /// The path was lost and is looked for again.
        case reconnecting
    }

    /// CallKit's name for the call.
    let uuid: UUID
    let accountKey: String
    let conversationId: String
    /// The chat's name as the person sees it, or the caller's.
    let title: String
    let isOutgoing: Bool

    /// The server's id of the call; nil until it has made it.
    var callId: String?
    /// The call as the server last told it.
    var call: ChatCallView?
    var phase = Phase.connecting
    /// The person's own user id, which of the call's members is them.
    var ownUserId = ""
    var isMuted = false
    /// Who a call this phone starts rings: nil for everybody else in the chat.
    var ringUserIds: [String]?
    /// Who the server says is speaking, and the last of the others who did,
    /// so the person's own voice does not take the featured picture away.
    var speakingUserId = ""
    var lastOtherSpeakerId = ""
    /// Whose picture the person chose to feature, empty to follow whoever
    /// speaks.
    var featuredUserId = ""
    /// Whether the call screen is put away to the bar at the top of the app,
    /// so the person can use the rest of it while on the call.
    var isMinimized = false
    var isCameraOn = false
    var isSpeakerOn = false
    var usesFrontCamera = true
    /// The pictures of the others and the person's own, by what draws them.
    var remoteVideoTracks: [String: RTCVideoTrack] = [:]
    var localVideoTrack: RTCVideoTrack?

    @ObservationIgnored var connection: Connection?
    @ObservationIgnored var media: ChatCallMedia?
    @ObservationIgnored var signaling = ChatCallSignaling()
    @ObservationIgnored var signalsTask: Task<Void, Never>?
    /// Set once the call is being ended, so that what fails because of it is
    /// not told as a problem.
    @ObservationIgnored var isEnding = false
    @ObservationIgnored var hasReportedConnected = false

    init(uuid: UUID, accountKey: String, conversationId: String, title: String, isOutgoing: Bool) {
        self.uuid = uuid
        self.accountKey = accountKey
        self.conversationId = conversationId
        self.title = title
        self.isOutgoing = isOutgoing
    }
}

/// ChatCallProblem is a call that could not go on, in words for the person.
enum ChatCallProblem: LocalizedError {
    case microphoneRefused
    case cameraRefused
    case alreadyOnCall
    case notSignedIn

    var errorDescription: String? {
        switch self {
        case .microphoneRefused: String(localized: "TeaNode may not use the microphone. Allow it in Settings, then call again.")
        case .cameraRefused: String(localized: "TeaNode may not use the camera. Allow it in Settings, then try again.")
        case .alreadyOnCall: String(localized: "You are on a call already. Hang up first.")
        case .notSignedIn: String(localized: "Sign in to TeaNode to call.")
        }
    }
}

/// ChatCallManager is the calls with the people of a chat on this phone. It
/// brings three things together:
///
/// - CallKit, which makes the call the phone's: the system's incoming call
///   screen, the lock screen, AirPods and the car. Every call, rung or
///   started, is a CallKit call, known by a UUID this keeps with the server's
///   id of the call (`ChatCallLedger`, in the Kit, decides what to tell
///   CallKit of each ring and each change).
/// - PushKit, through `VoIPPushRegistrar`: a VoIP push wakes the app, which
///   must report a call to CallKit at once (`receiveVoIPPush`).
/// - WebRTC, through `ChatCallMedia`: the audio and video, in the order
///   `ChatCallSignaling` gives (`perform`).
///
/// It follows ChatCallEvents while the app is signed in, for the calls ringing
/// the person, the banner of a chat with a call going, and the line of a call
/// in a chat.
@MainActor
@Observable
final class ChatCallManager: NSObject {
    static let shared = ChatCallManager()

    /// The call this phone is on or going onto.
    private(set) var session: ChatCallSession?
    /// The newest view of each call heard of, by call id: a call's line in a
    /// chat is drawn from it.
    private(set) var callViews: [String: ChatCallView] = [:]
    /// The call going in each chat, by the person's conversation for it.
    private(set) var goingCallIds: [String: String] = [:]
    /// What went wrong, for an alert.
    var problemText: String?

    @ObservationIgnored private var ledger = ChatCallLedger()
    private let provider: CXProvider
    private let callController = CXCallController()

    private override init() {
        let configuration = CXProviderConfiguration()
        configuration.supportsVideo = true
        // A second call rings as a waiting call; answering it ends the first
        // (the system offers "End & Accept"), since the phone is on one.
        configuration.maximumCallGroups = 2
        configuration.maximumCallsPerCallGroup = 1
        configuration.supportedHandleTypes = [.generic]
        configuration.includesCallsInRecents = false
        provider = CXProvider(configuration: configuration)
        super.init()
        // CallKit calls the delegate on the main queue when none is named.
        provider.setDelegate(self, queue: nil)
        // CallKit owns the audio session: WebRTC starts the audio when CallKit
        // says the session is active (provider(_:didActivate:)), not before.
        let audio = RTCAudioSession.sharedInstance()
        audio.useManualAudio = true
        audio.isAudioEnabled = false
    }

    /// Makes the manager, and with it CallKit's provider, which the app does
    /// at launch so that a VoIP push finds it ready.
    func start() {
        _ = provider
    }

    /// Whether this phone is on the call (or going onto it).
    func isOnCall(_ callId: String) -> Bool {
        session?.callId == callId
    }

    #if DEBUG
        /// Shows a call that is not one, for the screenshots (ChatCallDemo).
        func showDemo(session demo: ChatCallSession) {
            session = demo
        }
    #endif

    // MARK: A ring from a VoIP push

    /// A VoIP push arrived. iOS ends an app that is woken by one and does not
    /// report a call to CallKit, so every push is answered with a call
    /// reported here, before it returns: a call that rings, or the throwaway
    /// call Apple's pattern asks for when the push needs none shown (the call
    /// is not known, or is known already), which is reported and ended at once.
    /// `finish` is PushKit's completion, called when CallKit has the report.
    func receiveVoIPPush(sealed: String?, finish: @escaping @Sendable () -> Void) {
        var content: NotificationContent?
        var accountKey: String?
        if let sealed, let ring = try? KeychainAccountStore().accounts(), let keys = try? PayloadKey.keyring.keys(for: ring),
            case let (openedBy, opened)? = PayloadKey.open(sealed, keys: keys)
        {
            accountKey = openedBy
            content = opened
        }
        switch VoIPPush.decide(content: content, accountKey: accountKey, ledger: &ledger) {
        case .ring(let uuid, _, _, let callerName, _):
            reportNewCall(uuid: uuid, callerName: callerName, endsAtOnce: false, finish: finish)
        case .stopRinging(let uuid):
            provider.reportCall(with: uuid, endedAt: Date(), reason: .answeredElsewhere)
            // iOS wants a call reported for every VoIP push, this one too: a
            // call that ends as it is reported, kept out of Recents.
            reportNewCall(uuid: UUID(), callerName: "TeaNode", endsAtOnce: true, finish: finish)
        case .reportAndEnd(let uuid):
            reportNewCall(uuid: uuid, callerName: "TeaNode", endsAtOnce: true, finish: finish)
        }
    }

    /// Reports an incoming call to CallKit, which shows it ringing; or, for a
    /// push that needs no call shown, ends it at once.
    private func reportNewCall(uuid: UUID, callerName: String, endsAtOnce: Bool, finish: @escaping @Sendable () -> Void) {
        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(type: .generic, value: callerName)
        update.localizedCallerName = callerName
        update.hasVideo = false
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        update.supportsDTMF = false
        // CallKit answers on a queue of its own: the closure is Sendable and
        // goes back to the main actor for anything of the app's.
        provider.reportNewIncomingCall(with: uuid, update: update) { @Sendable error in
            let isRefused = error != nil
            Task { @MainActor in
                if endsAtOnce {
                    self.provider.reportCall(with: uuid, endedAt: Date(), reason: .remoteEnded)
                } else if isRefused {
                    // CallKit would not ring it (Do Not Disturb, a blocked caller).
                    self.ledger.remove(uuid: uuid)
                }
                finish()
            }
        }
    }

    // MARK: Following the server

    /// Follows ChatCallEvents while the app is signed in, reading the calls
    /// going again whenever the feed has to be opened anew. The Kit's feed
    /// ends with an error when the connection drops.
    func follow(connection: Connection) async {
        #if DEBUG
            if DemoServer.isRequested { return }
        #endif
        var delay = Duration.seconds(1)
        while !Task.isCancelled {
            await readCallsGoing(connection: connection)
            do {
                for try await view in ChatCallFeed.events(connection: connection) {
                    note(view)
                    delay = .seconds(1)
                }
            } catch is CancellationError {
                return
            } catch {
                // The feed dropped; what was missed is read again above.
            }
            try? await Task.sleep(for: delay)
            delay = min(delay * 2, .seconds(30))
        }
    }

    /// Reads the calls going the person has a part in: the rings that are
    /// over are ended, and a call ringing them that this phone does not know
    /// rings, when the app is in front (a phone that registered no VoIP token
    /// is rung this way, and a ring the push missed).
    private func readCallsGoing(connection: Connection) async {
        guard let views = try? await connection.listGoingChatCalls() else { return }
        for view in views where view.isGoing {
            callViews[view.id] = view
            goingCallIds[view.conversationId] = view.id
            if let session, session.callId == view.id { session.call = view }
        }
        for reaction in ledger.reconcile(going: views) { handle(reaction) }
        guard UIApplication.shared.applicationState == .active, session == nil else { return }
        let accountKey = connection.accountKey
        for view in views where view.isGoing && view.askerRingState == "ringing" {
            let callerName = view.startedByName.isEmpty ? view.chatTitle : view.startedByName
            if case .ring(let uuid) = ledger.ringIncoming(
                callId: view.id, conversationId: view.conversationId, title: callerName, accountKey: accountKey)
            {
                reportNewCall(uuid: uuid, callerName: callerName, endsAtOnce: false, finish: {})
            }
        }
    }

    /// Reads the call going in a chat that has just been opened, for its
    /// banner; later changes come with ChatCallEvents.
    func readGoingCall(inConversation conversationId: String, connection: Connection) async {
        guard let found = try? await connection.readGoingChatCall(conversationId: conversationId) else {
            // Nothing going, or the server could not be asked: the banner is
            // the one the events built, if any.
            return
        }
        note(found)
    }

    /// A call changed, as the person sees it now.
    func note(_ view: ChatCallView) {
        callViews[view.id] = view
        if view.isGoing {
            goingCallIds[view.conversationId] = view.id
        } else if goingCallIds[view.conversationId] == view.id {
            goingCallIds[view.conversationId] = nil
        }
        if let session, session.callId == view.id { session.call = view }
        handle(ledger.apply(view: view))
    }

    /// What CallKit and the call screen do for a change the ledger decided on.
    private func handle(_ reaction: ChatCallLedger.Reaction) {
        guard case .end(let uuid, let reason) = reaction else { return }
        provider.reportCall(with: uuid, endedAt: Date(), reason: Self.callKitReason(reason))
        if let session, session.uuid == uuid { teardown(session) }
    }

    private static func callKitReason(_ reason: ChatCallEndReason) -> CXCallEndedReason {
        switch reason {
        case .answeredElsewhere: .answeredElsewhere
        case .declinedElsewhere: .declinedElsewhere
        case .unanswered: .unanswered
        case .remoteEnded: .remoteEnded
        case .failed: .failed
        }
    }

    // MARK: Starting and joining

    /// Calls the people of a chat, with the camera on or off, ringing
    /// everybody else in it or only the people in userIds: starts a call, or
    /// joins the one going in it.
    func startCall(conversationId: String, title: String, isVideoOn: Bool = false, userIds: [String]? = nil) async {
        if let callId = goingCallIds[conversationId] {
            await join(callId: callId, conversationId: conversationId, title: title)
            return
        }
        if isVideoOn, !(await Self.requestCameraAccess()) {
            problemText = ChatCallProblem.cameraRefused.errorDescription
            return
        }
        await place(conversationId: conversationId, title: title, existingCallId: nil, isVideoOn: isVideoOn, userIds: userIds)
    }

    /// Joins a call going: the one ringing this phone is answered through
    /// CallKit, one not ringing is joined as a call this phone places.
    func join(callId: String, conversationId: String, title: String) async {
        if let ringing = ledger.entry(callId: callId), ringing.phase == .ringing {
            callController.request(CXTransaction(action: CXAnswerCallAction(call: ringing.uuid))) { @Sendable _ in }
            return
        }
        // The call the phone is on already: its screen comes back.
        if let session, session.callId == callId {
            session.isMinimized = false
            return
        }
        await place(conversationId: conversationId, title: title, existingCallId: callId)
    }

    private func place(
        conversationId: String, title: String, existingCallId: String?, isVideoOn: Bool = false, userIds: [String]? = nil
    ) async {
        guard session == nil, ledger.activeEntry == nil else {
            problemText = ChatCallProblem.alreadyOnCall.errorDescription
            return
        }
        let app = AppModel.shared
        guard let accountKey = app.account?.accountKey, let connection = app.connection else {
            problemText = ChatCallProblem.notSignedIn.errorDescription
            return
        }
        let entry = ledger.place(conversationId: conversationId, title: title, accountKey: accountKey)
        if let existingCallId { _ = ledger.attach(callId: existingCallId, to: entry.uuid) }
        let placed = ChatCallSession(uuid: entry.uuid, accountKey: accountKey, conversationId: conversationId, title: title, isOutgoing: true)
        placed.connection = connection
        placed.callId = existingCallId
        placed.ownUserId = app.ownUserId ?? ""
        placed.isCameraOn = isVideoOn
        placed.ringUserIds = userIds
        session = placed
        let action = CXStartCallAction(call: entry.uuid, handle: CXHandle(type: .generic, value: title))
        action.isVideo = isVideoOn
        callController.request(CXTransaction(action: action)) { @Sendable error in
            guard error != nil else { return }
            // CallKit would not take the call (another call it cannot hold).
            Task { @MainActor in
                self.problemText = String(localized: "The call could not be started. Finish the call you are on and try again.")
                self.endLocally(uuid: entry.uuid)
            }
        }
    }

    /// The server's side of a call this phone places or joins, once CallKit
    /// has the call.
    private func runPlaced(_ session: ChatCallSession) async {
        guard let connection = session.connection else { return }
        do {
            guard await Self.requestMicrophoneAccess() else { throw ChatCallProblem.microphoneRefused }
            if session.ownUserId.isEmpty, let userId = try await connection.userId() { session.ownUserId = userId }
            var view: ChatCallView
            if let callId = session.callId {
                view = try await connection.joinChatCall(callId: callId, agentPhoneId: AppModel.shared.phone?.id)
            } else {
                view = try await connection.startChatCall(
                    conversationId: session.conversationId, isVideoOn: session.isCameraOn, userIds: session.ringUserIds)
                // A call going in the chat is returned to be joined.
                if !view.isAskerOn { view = try await connection.joinChatCall(callId: view.id, agentPhoneId: AppModel.shared.phone?.id) }
            }
            session.callId = view.id
            note(view)
            if let displaced = ledger.attach(callId: view.id, to: session.uuid) {
                provider.reportCall(with: displaced, endedAt: Date(), reason: .answeredElsewhere)
            }
            if session.isEnding {
                _ = try? await connection.leaveChatCall(callId: view.id)
                return
            }
            try await startMedia(session)
        } catch {
            fail(session, error)
        }
    }

    /// The server's side of a ringing call the person answered.
    private func runAnswered(_ session: ChatCallSession) async {
        guard let connection = session.connection, let callId = session.callId else { return }
        do {
            guard await Self.requestMicrophoneAccess() else { throw ChatCallProblem.microphoneRefused }
            if session.ownUserId.isEmpty, let userId = try await connection.userId() { session.ownUserId = userId }
            let view = try await connection.joinChatCall(callId: callId, agentPhoneId: AppModel.shared.phone?.id)
            note(view)
            if session.isEnding {
                _ = try? await connection.leaveChatCall(callId: callId)
                return
            }
            try await startMedia(session)
        } catch {
            fail(session, error)
        }
    }

    private static func requestMicrophoneAccess() async -> Bool {
        await AVAudioApplication.requestRecordPermission()
    }

    private static func requestCameraAccess() async -> Bool {
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: return true
        case .notDetermined: return await AVCaptureDevice.requestAccess(for: .video)
        default: return false
        }
    }

    // MARK: The media

    /// Opens the signals, then joins the call's media: the order of
    /// docs/subsystems/chat-calls.md, "A device on a call".
    private func startMedia(_ session: ChatCallSession) async throws {
        guard let connection = session.connection, let callId = session.callId else { return }
        let media = try ChatCallMedia(streamId: "teanode-phone")
        session.media = media
        media.setMuted(session.isMuted)
        media.onConnectionState = { [weak self, weak session] state in
            guard let self, let session else { return }
            self.mediaStateChanged(state, in: session)
        }
        media.onRemoteVideoChange = { [weak session, weak media] in
            session?.remoteVideoTracks = media?.remoteVideoTracks ?? [:]
        }
        if session.isCameraOn {
            media.setCamera(isOn: true, usesFrontCamera: session.usesFrontCamera)
            session.localVideoTrack = media.localVideoTrack
            // A picture is looked at, not held to the ear.
            if !session.isSpeakerOn, Self.isOnReceiver { setSpeaker(true) }
        }
        openSignals(session, connection: connection, callId: callId)
        let commands = session.signaling.start()
        await perform(commands, in: session, connection: connection, callId: callId)
    }

    /// Follows the offers of the media server for as long as the call goes,
    /// opening the feed again when it drops: the server keeps what it said
    /// meanwhile.
    private func openSignals(_ session: ChatCallSession, connection: Connection, callId: String) {
        session.signalsTask?.cancel()
        session.signalsTask = Task { [weak self, weak session] in
            var delay = Duration.milliseconds(500)
            while !Task.isCancelled {
                do {
                    for try await signal in ChatCallFeed.signals(connection: connection, callId: callId) {
                        guard let self, let session else { return }
                        delay = .milliseconds(500)
                        if let speaking = signal.speakingUserId, !speaking.isEmpty {
                            session.speakingUserId = speaking
                            if speaking != session.ownUserId { session.lastOtherSpeakerId = speaking }
                            continue
                        }
                        let commands = session.signaling.receive(offer: signal.offerSdp)
                        await self.perform(commands, in: session, connection: connection, callId: callId)
                    }
                } catch is CancellationError {
                    return
                } catch {
                    // Opened again below.
                }
                try? await Task.sleep(for: delay)
                delay = min(delay * 2, .seconds(5))
            }
        }
    }

    /// Takes the steps the signaling asks for, one after the other: the join,
    /// then each offer of the server's, answered in order. A step that fails
    /// ends the call.
    private func perform(_ commands: [ChatCallSignalingCommand], in session: ChatCallSession, connection: Connection, callId: String) async {
        var pending = commands
        while !pending.isEmpty {
            let command = pending.removeFirst()
            guard let media = session.media, !session.isEnding else { return }
            do {
                switch command {
                case .join:
                    let offer = try await media.makeJoinOffer()
                    let answer = try await connection.joinChatCallMedia(callId: callId, offerSdp: offer)
                    try await media.setJoinAnswer(answer)
                    pending += session.signaling.joinAnswered()
                case .answer(let offerSdp):
                    let answer = try await media.answer(offerSdp: offerSdp)
                    try await connection.answerChatCallMedia(callId: callId, answerSdp: answer)
                    pending += session.signaling.answerSent()
                }
            } catch {
                session.signaling.fail()
                fail(session, error)
                return
            }
        }
    }

    private func mediaStateChanged(_ state: ChatCallConnectionState, in session: ChatCallSession) {
        guard !session.isEnding else { return }
        switch state {
        case .connecting:
            break
        case .connected:
            session.phase = .connected
            ledger.connected(uuid: session.uuid)
            if session.isOutgoing, !session.hasReportedConnected {
                session.hasReportedConnected = true
                provider.reportOutgoingCall(with: session.uuid, connectedAt: Date())
            }
        case .disconnected:
            session.phase = .reconnecting
        case .failed:
            problemText = String(localized: "The call lost its connection.")
            endAndReport(session, reason: .failed)
        }
    }

    // MARK: What the person does on the call

    /// Mutes or unmutes through CallKit, so that the lock screen agrees.
    func setMuted(_ isMuted: Bool) {
        guard let session else { return }
        let action = CXSetMutedCallAction(call: session.uuid, muted: isMuted)
        callController.request(CXTransaction(action: action)) { @Sendable error in
            guard error != nil else { return }
            Task { @MainActor in self.applyMuted(isMuted, to: session) }
        }
    }

    /// Mutes the microphone and tells the others, to show it beside the
    /// name. The sound stops either way, so a failure to tell them is not
    /// the person's to handle.
    private func applyMuted(_ isMuted: Bool, to session: ChatCallSession) {
        session.isMuted = isMuted
        session.media?.setMuted(isMuted)
        guard session.media != nil, let callId = session.callId, let connection = session.connection else { return }
        Task { if let view = try? await connection.setChatCallMuted(callId: callId, isMuted: isMuted) { note(view) } }
    }

    /// Features a person's picture, or nil to follow whoever speaks, and asks
    /// the server to send that one whole. The picture is featured here at
    /// once; a failure to tell the server leaves it small.
    func setFeatured(_ userId: String?) {
        guard let session else { return }
        session.featuredUserId = userId ?? ""
        guard session.media != nil, let callId = session.callId, let connection = session.connection else { return }
        Task { try? await connection.setChatCallFeatured(callId: callId, userId: userId) }
    }

    /// Turns the camera on or off, and tells the others.
    func setCamera(isOn: Bool) async {
        guard let session else { return }
        if isOn, !(await Self.requestCameraAccess()) {
            problemText = ChatCallProblem.cameraRefused.errorDescription
            return
        }
        session.isCameraOn = isOn
        if let media = session.media {
            if !media.setCamera(isOn: isOn, usesFrontCamera: session.usesFrontCamera) {
                session.isCameraOn = false
                session.localVideoTrack = nil
                problemText = ChatCallProblem.cameraRefused.errorDescription
                return
            }
            session.localVideoTrack = media.localVideoTrack
        }
        // A picture is looked at, not held to the ear.
        if isOn, !session.isSpeakerOn, Self.isOnReceiver { setSpeaker(true) }
        if let callId = session.callId, let connection = session.connection {
            do {
                note(try await connection.setChatCallVideo(callId: callId, isVideoOn: isOn))
            } catch {
                problemText = AppModel.shared.describe(error)
            }
        }
    }

    /// Changes to the phone's other camera.
    func flipCamera() {
        guard let session, session.isCameraOn, let media = session.media else { return }
        // Only a camera that opened is the one in use: a phone whose other
        // camera will not open stays on this one.
        if media.flipCamera(usesFrontCamera: !session.usesFrontCamera) {
            session.usesFrontCamera.toggle()
        }
    }

    /// Plays the call on the speaker, or back on the receiver or whatever is
    /// connected (headphones, AirPods, a car).
    func setSpeaker(_ isOn: Bool) {
        guard let session else { return }
        session.isSpeakerOn = isOn
        let port: AVAudioSession.PortOverride = isOn ? AVAudioSession.PortOverride.speaker : AVAudioSession.PortOverride.none
        try? AVAudioSession.sharedInstance().overrideOutputAudioPort(port)
    }

    private static var isOnReceiver: Bool {
        AVAudioSession.sharedInstance().currentRoute.outputs.contains { $0.portType == .builtInReceiver }
    }

    /// Asks for the call to be transcribed. Everybody on it is asked.
    func askTranscription() async {
        guard let session, let callId = session.callId, let connection = session.connection else { return }
        do {
            note(try await connection.askChatCallTranscription(callId: callId))
        } catch {
            problemText = AppModel.shared.describe(error)
        }
    }

    // MARK: Ending

    /// Hangs up, through CallKit so that the lock screen agrees.
    func endCall() {
        guard let session else { return }
        let uuid = session.uuid
        callController.request(CXTransaction(action: CXEndCallAction(call: uuid))) { @Sendable error in
            guard error != nil else { return }
            Task { @MainActor in self.endLocally(uuid: uuid) }
        }
    }

    /// Ends a call CallKit does not know (it refused it, or lost it).
    private func endLocally(uuid: UUID) {
        guard let session, session.uuid == uuid else {
            ledger.remove(uuid: uuid)
            return
        }
        leaveOnServer(session)
        ledger.remove(uuid: uuid)
        teardown(session)
    }

    /// Ends a call that went wrong, and tells CallKit.
    private func endAndReport(_ session: ChatCallSession, reason: ChatCallEndReason) {
        guard !session.isEnding else { return }
        provider.reportCall(with: session.uuid, endedAt: Date(), reason: Self.callKitReason(reason))
        leaveOnServer(session)
        ledger.remove(uuid: session.uuid)
        teardown(session)
    }

    private func fail(_ session: ChatCallSession, _ error: Error) {
        guard !session.isEnding else { return }
        problemText = AppModel.shared.describe(error)
        endAndReport(session, reason: .failed)
    }

    private func leaveOnServer(_ session: ChatCallSession) {
        guard let callId = session.callId, let connection = session.connection else { return }
        Task { _ = try? await connection.leaveChatCall(callId: callId) }
    }

    /// Lets go of the connection, the camera and the call screen.
    private func teardown(_ session: ChatCallSession) {
        session.isEnding = true
        session.signalsTask?.cancel()
        session.signalsTask = nil
        session.media?.close()
        session.media = nil
        session.remoteVideoTracks = [:]
        session.localVideoTrack = nil
        if self.session === session { self.session = nil }
    }

    // MARK: CallKit's actions

    private func prepareAudioSession() {
        // Voice chat mode: the system's echo cancelling and the receiver by
        // default; the speaker is the person's choice.
        try? AVAudioSession.sharedInstance().setCategory(.playAndRecord, mode: .voiceChat, options: [.allowBluetoothHFP])
    }
}

extension ChatCallManager: @preconcurrency CXProviderDelegate {
    func providerDidReset(_ provider: CXProvider) {
        // CallKit lost every call: so did the phone. Only the calls CallKit
        // had: one it was never told of (the screenshots' demo call, which a
        // simulator's CallKit resets right after launch) stays.
        if let session, ledger.entry(uuid: session.uuid) != nil {
            leaveOnServer(session)
            teardown(session)
        }
        ledger = ChatCallLedger()
    }

    func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        guard let session, session.uuid == action.callUUID else {
            action.fail()
            return
        }
        prepareAudioSession()
        provider.reportOutgoingCall(with: session.uuid, startedConnectingAt: Date())
        let update = CXCallUpdate()
        update.localizedCallerName = session.title
        update.hasVideo = false
        provider.reportCall(with: session.uuid, updated: update)
        action.fulfill()
        Task { await runPlaced(session) }
    }

    func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) {
        guard let entry = ledger.answer(uuid: action.callUUID),
            let connection = AppModel.shared.callConnection(accountKey: entry.accountKey)
        else {
            action.fail()
            return
        }
        // The phone is on one call: the first is left, whether CallKit's "End &
        // Accept" has told its end yet or not.
        if let existing = session, existing.uuid != entry.uuid {
            leaveOnServer(existing)
            ledger.remove(uuid: existing.uuid)
            teardown(existing)
        }
        let answered = ChatCallSession(
            uuid: entry.uuid, accountKey: entry.accountKey, conversationId: entry.conversationId, title: entry.title, isOutgoing: false)
        answered.callId = entry.callId
        answered.connection = connection
        answered.ownUserId = AppModel.shared.ownUserId ?? ""
        session = answered
        prepareAudioSession()
        action.fulfill()
        Task { await runAnswered(answered) }
    }

    func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        action.fulfill()
        let uuid = action.callUUID
        guard let removed = ledger.remove(uuid: uuid) else { return }
        if removed.phase == .ringing {
            // Said no to a call that was ringing.
            if let callId = removed.callId, let connection = AppModel.shared.callConnection(accountKey: removed.accountKey) {
                Task { _ = try? await connection.declineChatCall(callId: callId, agentPhoneId: AppModel.shared.phone?.id) }
            }
            return
        }
        if let session, session.uuid == uuid {
            leaveOnServer(session)
            teardown(session)
        }
    }

    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        if let session, session.uuid == action.callUUID {
            applyMuted(action.isMuted, to: session)
        }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        let audio = RTCAudioSession.sharedInstance()
        audio.audioSessionDidActivate(audioSession)
        audio.isAudioEnabled = true
    }

    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        let audio = RTCAudioSession.sharedInstance()
        audio.audioSessionDidDeactivate(audioSession)
        audio.isAudioEnabled = false
    }
}
