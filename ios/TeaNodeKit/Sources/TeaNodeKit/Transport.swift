import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// HTTPTransport sends a request and hands back what came back. The app
/// uses URLSession; tests stand in a server of their own.
public protocol HTTPTransport: Sendable {
    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse)

    /// Reads the answer to a request into a file and returns where it is. The
    /// caller owns the file and moves or removes it. For an answer that may be
    /// hundreds of megabytes, which send would hold whole in memory.
    func download(_ request: URLRequest) async throws -> (URL, HTTPURLResponse)
}

extension HTTPTransport {
    /// A transport that has no download task of its own, such as a test's,
    /// writes what send answered.
    public func download(_ request: URLRequest) async throws -> (URL, HTTPURLResponse) {
        let (data, response) = try await send(request)
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try data.write(to: fileURL, options: .atomic)
        return (fileURL, response)
    }
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

        /// A download task, which writes to a file as the bytes arrive. The
        /// file is moved out of the system's reach before the completion
        /// handler returns, since the system removes it then.
        public func download(_ request: URLRequest) async throws -> (URL, HTTPURLResponse) {
            try await withCheckedThrowingContinuation { continuation in
                let task = session.downloadTask(with: request) { temporaryURL, response, error in
                    if let error {
                        continuation.resume(throwing: error)
                        return
                    }
                    guard let temporaryURL, let http = response as? HTTPURLResponse else {
                        continuation.resume(throwing: TeaNodeError.unreadableAnswer)
                        return
                    }
                    let keptURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
                    do {
                        try FileManager.default.moveItem(at: temporaryURL, to: keptURL)
                        continuation.resume(returning: (keptURL, http))
                    } catch {
                        continuation.resume(throwing: error)
                    }
                }
                task.resume()
            }
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
