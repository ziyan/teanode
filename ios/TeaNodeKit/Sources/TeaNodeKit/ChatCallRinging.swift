import Foundation

// How the phone rings for a call. CallKit knows a call by a UUID the app
// makes; the server knows it by an id. The ledger keeps the two together and
// says what to tell CallKit when the server says something about a call, in
// one place, so that it is tested without a phone. iOS ends an app that is
// woken by a VoIP push and does not report a call to CallKit, so every push
// has an answer here that reports one.

/// ChatCallEndReason is why a call ended for the phone ringing for it, which
/// CallKit words on the lock screen.
public enum ChatCallEndReason: Equatable, Sendable {
    /// The person answered on another device.
    case answeredElsewhere
    /// The person said no on another device.
    case declinedElsewhere
    /// Nobody answered before the ringing stopped.
    case unanswered
    /// The call ended.
    case remoteEnded
    case failed
}

/// ChatCallLedger is the calls this phone rings for, is placing, or is on.
public struct ChatCallLedger: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        /// An outgoing call, started on this phone, not yet known to the server.
        case placing
        /// An incoming call, ringing.
        case ringing
        /// The person answered, or joined, and the device is connecting.
        case joining
        /// The device is on the call.
        case connected
    }

    public struct Entry: Equatable, Sendable {
        public let uuid: UUID
        /// The server's id of the call; nil while an outgoing call is placed.
        public var callId: String?
        public var conversationId: String
        /// Who is calling, or the chat that is called: what CallKit shows.
        public var title: String
        public var accountKey: String
        public var isOutgoing: Bool
        public var phase: Phase
    }

    public private(set) var entries: [Entry] = []
    /// The calls seen ending lately, so that a ring that comes late for one
    /// is not shown. Bounded.
    private var endedCallIds: [String] = []
    private static let endedCallIdsKept = 30

    public init() {}

    // MARK: Looking up

    public func entry(uuid: UUID) -> Entry? {
        entries.first { $0.uuid == uuid }
    }

    public func entry(callId: String) -> Entry? {
        entries.first { $0.callId == callId }
    }

    /// The call the phone is on, or going onto: the one that is not ringing.
    public var activeEntry: Entry? {
        entries.first { $0.phase != .ringing }
    }

    public var ringingEntries: [Entry] {
        entries.filter { $0.phase == .ringing }
    }

    // MARK: A ring from the server

    /// What to report to CallKit for a VoIP push of a call ringing.
    public enum RingDecision: Equatable, Sendable {
        /// A new incoming call under this UUID: it rings.
        case ring(uuid: UUID)
        /// A call the phone knows already, or knows to have ended: report a
        /// call under this UUID and end it at once, which satisfies iOS
        /// without ringing twice.
        case reportAndEnd(uuid: UUID)
    }

    public mutating func ringIncoming(
        callId: String, conversationId: String, title: String, accountKey: String, uuid: UUID = UUID()
    ) -> RingDecision {
        if entry(callId: callId) != nil || endedCallIds.contains(callId) {
            return .reportAndEnd(uuid: uuid)
        }
        entries.append(
            Entry(
                uuid: uuid, callId: callId, conversationId: conversationId, title: title, accountKey: accountKey, isOutgoing: false,
                phase: .ringing))
        return .ring(uuid: uuid)
    }

    /// What to report to CallKit for a VoIP push saying a call no longer
    /// rings the phone.
    public enum StopDecision: Equatable, Sendable {
        /// The phone rings for it: report that call ended.
        case stopRinging(uuid: UUID)
        /// The phone does not ring for it (it is on the call, or never knew
        /// it): report a call under this UUID and end it at once.
        case reportAndEnd(uuid: UUID)
    }

    /// The server says the call no longer rings the phone. The phone's own
    /// answer is told to its other phones too, so a call the phone is on can
    /// be told of: only a call still ringing stops.
    public mutating func stopRinging(callId: String, uuid: UUID = UUID()) -> StopDecision {
        guard let found = entry(callId: callId) else {
            // Told before the ring: the ring that follows is not shown.
            noteEnded(callId: callId)
            return .reportAndEnd(uuid: uuid)
        }
        guard found.phase == .ringing else {
            return .reportAndEnd(uuid: uuid)
        }
        remove(uuid: found.uuid)
        return .stopRinging(uuid: found.uuid)
    }

    private mutating func noteEnded(callId: String) {
        guard !endedCallIds.contains(callId) else { return }
        endedCallIds.append(callId)
        if endedCallIds.count > Self.endedCallIdsKept { endedCallIds.removeFirst(endedCallIds.count - Self.endedCallIdsKept) }
    }

    // MARK: A call the person starts, answers or ends

    /// A call started on this phone, before the server has made it.
    @discardableResult
    public mutating func place(conversationId: String, title: String, accountKey: String, uuid: UUID = UUID()) -> Entry {
        let placed = Entry(
            uuid: uuid, callId: nil, conversationId: conversationId, title: title, accountKey: accountKey, isOutgoing: true,
            phase: .placing)
        entries.append(placed)
        return placed
    }

    /// The server made the call, or said the chat has one going already. When
    /// this phone was ringing for that very call, the ringing entry is
    /// dropped and its UUID returned, for CallKit to end.
    public mutating func attach(callId: String, to uuid: UUID) -> UUID? {
        var displaced: UUID?
        if let ringing = entries.first(where: { $0.callId == callId && $0.uuid != uuid && $0.phase == .ringing }) {
            displaced = ringing.uuid
            entries.removeAll { $0.uuid == ringing.uuid }
        }
        if let index = entries.firstIndex(where: { $0.uuid == uuid }) {
            entries[index].callId = callId
        }
        return displaced
    }

    /// The person answered the ringing call: the device is joining it.
    @discardableResult
    public mutating func answer(uuid: UUID) -> Entry? {
        guard let index = entries.firstIndex(where: { $0.uuid == uuid && $0.phase == .ringing }) else { return nil }
        entries[index].phase = .joining
        return entries[index]
    }

    /// The device is on the call.
    public mutating func connected(uuid: UUID) {
        if let index = entries.firstIndex(where: { $0.uuid == uuid }) {
            entries[index].phase = .connected
        }
    }

    /// The call is over for the phone, however it ended.
    @discardableResult
    public mutating func remove(uuid: UUID) -> Entry? {
        guard let index = entries.firstIndex(where: { $0.uuid == uuid }) else { return nil }
        let removed = entries.remove(at: index)
        if let callId = removed.callId { noteEnded(callId: callId) }
        return removed
    }

    // MARK: A change the server tells

    /// What a change to a call asks of CallKit.
    public enum Reaction: Equatable, Sendable {
        case nothing
        /// The phone rings for it, or is on it, and it is over: report it
        /// ended for this reason. The entry has been dropped.
        case end(uuid: UUID, reason: ChatCallEndReason)
    }

    /// A ChatCallEvents change, as the person sees the call now.
    public mutating func apply(view: ChatCallView) -> Reaction {
        guard let found = entry(callId: view.id) else { return .nothing }
        if found.phase == .ringing {
            if !view.isGoing {
                remove(uuid: found.uuid)
                return .end(uuid: found.uuid, reason: view.endReason == "missed" ? .unanswered : .remoteEnded)
            }
            let reason: ChatCallEndReason?
            switch view.askerRingState {
            case "answered": reason = .answeredElsewhere
            case "declined": reason = .declinedElsewhere
            case "missed": reason = .unanswered
            default: reason = nil
            }
            guard let reason else { return .nothing }
            remove(uuid: found.uuid)
            return .end(uuid: found.uuid, reason: reason)
        }
        if !view.isGoing {
            remove(uuid: found.uuid)
            return .end(uuid: found.uuid, reason: view.endReason == "failed" ? .failed : .remoteEnded)
        }
        return .nothing
    }

    /// The calls going, as the person's phone reads them when it opens or
    /// finds its feed dropped: a call this phone rings for that is no longer
    /// going or ringing it has ended meanwhile.
    public mutating func reconcile(going views: [ChatCallView]) -> [Reaction] {
        var reactions: [Reaction] = []
        for found in ringingEntries {
            guard let callId = found.callId else { continue }
            if let view = views.first(where: { $0.id == callId }) {
                let reaction = apply(view: view)
                if reaction != .nothing { reactions.append(reaction) }
            } else {
                remove(uuid: found.uuid)
                reactions.append(.end(uuid: found.uuid, reason: .remoteEnded))
            }
        }
        return reactions
    }
}

/// VoIPPush is what a VoIP push says, once its sealed content is opened.
public enum VoIPPush {
    /// What to report to CallKit for the push, and to remember.
    public enum Action: Equatable, Sendable {
        /// A call rings: report an incoming call.
        case ring(uuid: UUID, callId: String, conversationId: String, callerName: String, accountKey: String)
        /// A call the phone rings for no longer does: report it ended.
        case stopRinging(uuid: UUID)
        /// Report a call under this UUID and end it at once: the push is for
        /// a call the phone does not ring for, or could not be read, and iOS
        /// still wants a call reported.
        case reportAndEnd(uuid: UUID)
    }

    /// Decides what a push asks. `content` is nil when it could not be
    /// opened (no key of an account on the phone fits it).
    public static func decide(
        content: NotificationContent?, accountKey: String?, ledger: inout ChatCallLedger, uuid: UUID = UUID()
    ) -> Action {
        guard let content, let accountKey, let callId = content.callId, !callId.isEmpty else {
            return .reportAndEnd(uuid: uuid)
        }
        switch content.notificationKind {
        case "call":
            let callerName = content.title.isEmpty ? content.body : content.title
            let conversationId = content.conversationId ?? ""
            switch ledger.ringIncoming(
                callId: callId, conversationId: conversationId, title: callerName, accountKey: accountKey, uuid: uuid)
            {
            case .ring(let ringingUuid):
                return .ring(uuid: ringingUuid, callId: callId, conversationId: conversationId, callerName: callerName, accountKey: accountKey)
            case .reportAndEnd(let endedUuid):
                return .reportAndEnd(uuid: endedUuid)
            }
        case "call_ended":
            switch ledger.stopRinging(callId: callId, uuid: uuid) {
            case .stopRinging(let ringingUuid): return .stopRinging(uuid: ringingUuid)
            case .reportAndEnd(let endedUuid): return .reportAndEnd(uuid: endedUuid)
            }
        default:
            return .reportAndEnd(uuid: uuid)
        }
    }
}
