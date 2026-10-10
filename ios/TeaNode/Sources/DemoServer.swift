#if DEBUG
    import Foundation
    import TeaNodeKit

    #if canImport(FoundationNetworking)
        import FoundationNetworking
    #endif

    /// DemoServer stands in for a server in debug builds started with
    /// `-TeaNodeDemo`, so CI can photograph a conversation without one. It
    /// answers the app's GraphQL with an invented conversation; its live feed
    /// never connects, which the screens show as they would a slow network.
    struct DemoServer: HTTPTransport {
        static var isRequested: Bool {
            ProcessInfo.processInfo.arguments.contains("-TeaNodeDemo")
        }

        static let account = Account(
            server: try! ServerAddress(parsing: "demo.example.com"), clientId: "demo", accessToken: "demo",
            refreshToken: "", expiresAt: .distantFuture, username: "robin")

        func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
            let query = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            let answer: String
            if query.contains("ReadAgentConversation") {
                answer = Self.conversation
            } else if query.contains("ListAgentConversations") {
                answer = Self.conversations
            } else if query.contains("GetSession") {
                answer = #"{"data":{"GetSession":{"username":"robin"}}}"#
            } else {
                answer = #"{"data":{}}"#
            }
            return (Data(answer.utf8), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
        }

        static let conversations = #"""
            {"data":{"ListAgentConversations":[
              {"id":"main","kind":"main","title":null,"summary":null,"lastAt":"2026-10-10T09:05:00Z"},
              {"id":"trip","kind":"named","title":"Weekend trip","summary":null,"lastAt":"2026-10-09T18:30:00Z"},
              {"id":"taxes","kind":"named","title":"Receipts for the year","summary":null,"lastAt":"2026-10-02T12:00:00Z"}
            ]}}
            """#

        static let conversation = #"""
            {"data":{
              "ReadAgentConversation":{
                "conversation":{"id":"main","kind":"main","title":null,"summary":null,"lastAt":"2026-10-10T09:05:00Z"},
                "messages":[
                  {"id":"1","createdAt":"2026-10-10T09:00:00Z","role":"user","content":"What is on my calendar tomorrow?"},
                  {"id":"2","createdAt":"2026-10-10T09:00:02Z","role":"assistant","content":"","toolCalls":[{"id":"c1","name":"calendar","arguments":"{}"}]},
                  {"id":"3","createdAt":"2026-10-10T09:00:03Z","role":"tool","content":"2 events","toolCallId":"c1"},
                  {"id":"4","createdAt":"2026-10-10T09:00:05Z","role":"assistant","content":"Two things tomorrow:\n\n- **9:00** The plumber comes to look at the kitchen sink.\n- **13:30** Lunch with Sam at the noodle place.\n\nThe plumber wrote yesterday asking you to confirm. Should I reply?"},
                  {"id":"5","createdAt":"2026-10-10T09:01:00Z","role":"user","content":"Yes, say 9 works."},
                  {"id":"6","createdAt":"2026-10-10T09:01:02Z","role":"assistant","content":"","toolCalls":[{"id":"c2","name":"mail_draft","arguments":"{}"}]},
                  {"id":"7","createdAt":"2026-10-10T09:01:03Z","role":"tool","content":"drafted","toolCallId":"c2"},
                  {"id":"8","createdAt":"2026-10-10T09:05:00Z","role":"user","content":"[alert] A parcel was delivered to the front door."},
                  {"id":"9","createdAt":"2026-10-10T09:05:02Z","role":"assistant","content":"Your parcel from the bookshop was left at the front door a few minutes ago."}
                ],
                "total":9
              },
              "ListAgentInteractions":[
                {"runId":"r","callId":"c3","interactionKind":"approval","toolName":"mail_send","interactionText":"Send the reply to the plumber: \"Thursday at 9 works, see you then.\"","interactionChoices":null,"createdAt":"2026-10-10T09:01:04Z"}
              ]
            }}
            """#
    }
#endif
