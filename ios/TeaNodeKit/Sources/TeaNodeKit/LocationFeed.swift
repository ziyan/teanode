import Foundation

#if canImport(Darwin)
    /// LocationFeed streams who has moved: each time a person the viewer can
    /// see (themselves included) reports a new position or visit, the server
    /// says whose, and the screen reads that person's location again. It is
    /// opened while the Location screen is visible.
    ///
    /// Like `ConversationFeed`, the stream ends with an error when the
    /// connection drops, and the screen opens a new one.
    public enum LocationFeed {
        public static func events(connection: Connection, session: URLSession = .shared) -> AsyncThrowingStream<LocationEvent, Error> {
            AsyncThrowingStream { continuation in
                let task = Task {
                    do {
                        try await stream(connection: connection, session: session) { continuation.yield($0) }
                        continuation.finish()
                    } catch {
                        continuation.finish(throwing: error)
                    }
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }

        static func stream(connection: Connection, session: URLSession, yield: (LocationEvent) -> Void) async throws {
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
                    if watched.silence > ConversationFeed.silenceMost {
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
            try await socket.send(.string(GraphQLWebSocketMessage.start(id: "1", query: Documents.locationEvents, variables: [:]).encoded()))

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
                    throw TeaNodeError.refused(errors.first?.message ?? "The server stopped the location feed.")
                case .complete:
                    return
                case .data(_, let data, let errors):
                    if let error = errors.first {
                        throw TeaNodeError.refused(error.message)
                    }
                    guard let event = data?["LocationEvents"] else { continue }
                    yield(try JSONDecoder().decode(LocationEvent.self, from: JSONEncoder().encode(event)))
                default:
                    continue
                }
            }
        }
    }
#endif
