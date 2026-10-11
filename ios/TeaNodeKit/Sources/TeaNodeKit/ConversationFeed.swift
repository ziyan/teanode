import Foundation

#if canImport(Darwin)
    /// ConversationFeed streams a conversation's events as they happen: a
    /// turn's words as they are written, its tools, its cards. A turn already
    /// running when the feed opens is replayed from its start, so a screen
    /// that opens mid-turn shows all of it.
    ///
    /// The stream ends with an error when the connection drops; the screen
    /// reloads the conversation and opens a new feed.
    public enum ConversationFeed {
        /// How long the feed may say nothing before it is taken for dead:
        /// the server sends a keep-alive every second, and a connection a
        /// phone lost while asleep can stay open without saying so.
        static let silenceMost: TimeInterval = 15

        public static func events(
            connection: Connection, conversationId: String, session: URLSession = .shared
        ) -> AsyncThrowingStream<AgentEvent, Error> {
            AsyncThrowingStream { continuation in
                let task = Task {
                    do {
                        try await stream(connection: connection, conversationId: conversationId, session: session) {
                            continuation.yield($0)
                        }
                        continuation.finish()
                    } catch {
                        continuation.finish(throwing: error)
                    }
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }

        static func stream(
            connection: Connection, conversationId: String, session: URLSession, yield: (AgentEvent) -> Void
        ) async throws {
            let token = try await connection.accessToken()
            let server = await connection.account.server
            var components = URLComponents(url: server.endpoint("/api/v1/graphql"), resolvingAgainstBaseURL: false)!
            components.scheme = components.scheme == "http" ? "ws" : "wss"
            let socket = session.webSocketTask(with: components.url!, protocols: [GraphQLWebSocketMessage.subprotocol])
            socket.resume()
            let watched = WatchedSocket(socket)
            let watchdog = Task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(5))
                    if watched.silence > silenceMost {
                        watched.close()
                        return
                    }
                }
            }
            defer {
                watchdog.cancel()
                watched.close()
            }

            try await socket.send(.string(GraphQLWebSocketMessage.connectionInit(authorization: "Bearer \(token)").encoded()))
            try await socket.send(.string(GraphQLWebSocketMessage.start(
                id: "1", query: Documents.conversationEvents, variables: ["conversationId": .string(conversationId)]
            ).encoded()))

            while !Task.isCancelled {
                let frame = try await socket.receive()
                watched.heard()
                guard case .string(let text) = frame, let message = try? GraphQLWebSocketMessage(decoding: text) else {
                    continue
                }
                switch message {
                case .connectionError(let reason):
                    throw TeaNodeError.refused(reason)
                case .error(_, let errors):
                    throw TeaNodeError.refused(errors.first?.message ?? "The server stopped the conversation's feed.")
                case .complete:
                    return
                case .data(_, let data, let errors):
                    if let error = errors.first {
                        throw TeaNodeError.refused(error.message)
                    }
                    guard let event = data?["AgentConversationEvents"] else { continue }
                    yield(try JSONDecoder().decode(AgentEvent.self, from: JSONEncoder().encode(event)))
                default:
                    continue
                }
            }
        }
    }

    /// WatchedSocket is the feed's socket and when it last said anything, for
    /// the watchdog that closes one gone quiet.
    final class WatchedSocket: @unchecked Sendable {
        private let lock = NSLock()
        private let socket: URLSessionWebSocketTask
        private var heardAt = Date()

        init(_ socket: URLSessionWebSocketTask) {
            self.socket = socket
        }

        func heard() {
            lock.lock()
            defer { lock.unlock() }
            heardAt = Date()
        }

        var silence: TimeInterval {
            lock.lock()
            defer { lock.unlock() }
            return Date().timeIntervalSince(heardAt)
        }

        func close() {
            socket.cancel(with: .goingAway, reason: nil)
        }
    }
#endif
