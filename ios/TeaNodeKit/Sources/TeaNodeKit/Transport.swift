import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// HTTPTransport sends a request and hands back what came back. The app
/// uses URLSession; tests stand in a server of their own.
public protocol HTTPTransport: Sendable {
    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

#if canImport(Darwin)
    /// URLSessionTransport sends through a URLSession.
    public struct URLSessionTransport: HTTPTransport {
        let session: URLSession

        public init(session: URLSession = .shared) {
            self.session = session
        }

        public func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                throw TeaNodeError.unreadableAnswer
            }
            return (data, http)
        }
    }
#endif

/// TeaNodeError is what can go wrong talking to a server, in words a person
/// can act on.
public enum TeaNodeError: Error, Equatable, Sendable, LocalizedError {
    /// The server answered something this app cannot read.
    case unreadableAnswer
    /// The server refused, with its reason.
    case refused(String)
    /// The sign-in has ended: the person removed the phone, or signed it
    /// out. Connecting again is the way back.
    case signedOut
    /// The connect code was used, expired, or was made for another server.
    case connectCodeSpent
    /// A durable recording deletion refuses upload retries.
    case recordingDeleted

    /// Whether the server refused because what was asked for is not there, or
    /// is not the person's to see: it answers both the same way ("api: not
    /// found"). Every other failure, such as no connection, says nothing about
    /// whether the thing is there.
    public var isNotFound: Bool {
        guard case .refused(let reason) = self else { return false }
        return reason.contains("api: not found") || reason.contains("db: not found")
    }

    public var errorDescription: String? {
        switch self {
        case .unreadableAnswer:
            return "The server answered something this app cannot read."
        case .refused(let reason):
            return reason
        case .signedOut:
            return "This phone is no longer signed in. Connect it again."
        case .recordingDeleted:
            return "This recording was deleted. Its upload cannot be resumed."
        case .connectCodeSpent:
            return "That code has been used or has expired. Show a new one on the dashboard."
        }
    }
}

extension Error {
    /// Whether this is the server answering that what was asked for is not
    /// there, rather than a call that did not get through.
    public var isNotFoundRefusal: Bool { (self as? TeaNodeError)?.isNotFound ?? false }

    /// The server's own reason when it answered and refused, since the reason
    /// says why; otherwise the sentence the caller wrote for a call that did not
    /// get through, which says what to do next.
    public func refusalReason(orSay fallback: String) -> String {
        if let known = self as? TeaNodeError, case .refused(let reason) = known, !reason.isEmpty { return reason }
        return fallback
    }
}
