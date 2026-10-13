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
