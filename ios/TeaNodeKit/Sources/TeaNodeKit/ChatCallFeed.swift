import Foundation

#if canImport(Darwin)
    /// ChatCallFeed streams what happens to calls: `events` is each change to
    /// a call in a chat the person is in (it rings them, somebody joined or
    /// left, a camera went on, it ended), and `signals` is what the media
    /// server tells the device on a call (offers, to be answered in order).
    ///
    /// Like `ConversationFeed`, a stream ends with an error when the
    /// connection drops, and its owner opens a new one.
    public enum ChatCallFeed {
        /// Each change to a call in the person's chats, as they see it.
        public static func events(connection: Connection, session: URLSession = .shared) -> AsyncThrowingStream<ChatCallView, Error> {
            stream(connection: connection, session: session, document: Documents.chatCallEvents, variables: [:], field: "ChatCallEvents")
        }

        /// The offers of the media server's for a call the person is on, one
        /// at a time. Opened before the device joins the call's media.
        public static func signals(
            connection: Connection, callId: String, session: URLSession = .shared
        ) -> AsyncThrowingStream<ChatCallSignalView, Error> {
            stream(
                connection: connection, session: session, document: Documents.chatCallSignals, variables: ["callId": .string(callId)],
                field: "ChatCallSignals")
        }

        private static func stream<Element: Decodable & Sendable>(
            connection: Connection, session: URLSession, document: String, variables: [String: JSONValue], field: String
        ) -> AsyncThrowingStream<Element, Error> {
            AsyncThrowingStream { continuation in
                let task = Task {
                    do {
                        try await listen(connection: connection, session: session, document: document, variables: variables, field: field) {
                            (element: Element) in continuation.yield(element)
                        }
                        continuation.finish()
                    } catch {
                        continuation.finish(throwing: error)
                    }
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }

        private static func listen<Element: Decodable>(
            connection: Connection, session: URLSession, document: String, variables: [String: JSONValue], field: String,
            yield: (Element) -> Void
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
            try await socket.send(.string(GraphQLWebSocketMessage.start(id: "1", query: document, variables: variables).encoded()))

            while !Task.isCancelled {
                let frame = try await socket.receive()
                watched.heard()
                guard case .string(let text) = frame, let socketMessage = try? GraphQLWebSocketMessage(decoding: text) else {
                    continue
                }
                switch socketMessage {
                case .connectionError(let errorMessage):
                    throw TeaNodeError.refused(errorMessage)
                case .error(_, let errors):
                    throw TeaNodeError.refused(errors.first?.message ?? "The server stopped the call's feed.")
                case .complete:
                    return
                case .data(_, let eventData, let errors):
                    if let error = errors.first {
                        throw TeaNodeError.refused(error.message)
                    }
                    guard let event = eventData?[field] else { continue }
                    yield(try JSONDecoder().decode(Element.self, from: JSONEncoder().encode(event)))
                default:
                    continue
                }
            }
        }
    }
#endif
