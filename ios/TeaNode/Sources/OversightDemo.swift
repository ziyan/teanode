#if DEBUG
    import Foundation

    // The invented people and conversations that the screenshots of the
    // Oversight screen photograph (`-TeaNodeDemo -TeaNodeScreen oversight`).
    // DemoServer answers the three operations only this screen asks.

    extension DemoServer {
        /// The answer to Oversight's own operations, or nil for any other.
        static func oversightAnswer(query: String, body: Data?) -> String? {
            if query.contains("ListAllAgentRuns") {
                return oversightRuns(variables: requestVariables(body))
            }
            if query.contains("ListFailedAgentTurns") {
                return oversightFailedTurns
            }
            if query.contains("ReadAgentConversation"), screen == "oversight-observed" {
                return oversightObservedConversation
            }
            return nil
        }

        private static func oversightRun(
            _ id: String, owner: String, ownerName: String, title: String, kind: String, at lastAt: String, isRunning: Bool, tokens: Int
        ) -> String {
            #"{"id":"\#(id)","agentId":"agent-\#(owner)","title":"\#(title)","jobKind":"\#(kind)","lastAt":"\#(lastAt)","ownerUsername":"\#(owner)","ownerName":"\#(ownerName)","isRunning":\#(isRunning),"usage":{"promptTokens":\#(tokens),"cacheReadTokens":0,"completionTokens":\#(tokens / 10),"cost":0.0\#(tokens % 7 + 1)}}"#
        }

        private static func oversightRuns(variables: [String: Any]) -> String {
            let everything: [(kind: String, isRunning: Bool, row: String)] = [
                ("main", true, oversightRun("o1", owner: "quinn", ownerName: "Quinn Fixture", title: "", kind: "main", at: "2026-11-25T09:41:00Z", isRunning: true, tokens: 18200)),
                ("named", true, oversightRun("o2", owner: "lee", ownerName: "Lee Sample", title: "Planning the garden beds", kind: "named", at: "2026-11-25T09:38:00Z", isRunning: true, tokens: 9400)),
                ("named", false, oversightRun("o3", owner: "morgan", ownerName: "Morgan Placeholder", title: "Birthday party ideas", kind: "named", at: "2026-11-25T08:15:00Z", isRunning: false, tokens: 6100)),
                ("main", false, oversightRun("o4", owner: "avery", ownerName: "Avery Demo", title: "", kind: "main", at: "2026-11-24T21:02:00Z", isRunning: false, tokens: 24800)),
                ("triage", false, oversightRun("o5", owner: "quinn", ownerName: "Quinn Fixture", title: "Sorted a message from the electrician", kind: "triage", at: "2026-11-24T17:30:00Z", isRunning: false, tokens: 2300)),
                ("remember", false, oversightRun("o6", owner: "lee", ownerName: "Lee Sample", title: "Remembered the plumber's visit", kind: "remember", at: "2026-11-24T12:10:00Z", isRunning: false, tokens: 3900)),
                ("dream", false, oversightRun("o7", owner: "morgan", ownerName: "Morgan Placeholder", title: "Went over last week", kind: "dream", at: "2026-11-23T03:00:00Z", isRunning: false, tokens: 41000)),
            ]
            let isRunningOnly = variables["isRunningOnly"] as? Bool ?? false
            let kinds = variables["kinds"] as? [String] ?? []
            let rows = everything.filter { entry in
                (!isRunningOnly || entry.isRunning) && (kinds.isEmpty || kinds.contains(entry.kind))
            }.map(\.row)
            return #"{"data":{"ListAllAgentRuns":{"total":\#(rows.count),"totalCount":\#(rows.count),"runs":[\#(rows.joined(separator: ","))]}}}"#
        }

        private static let oversightFailedTurns = #"""
            {"data":{"ListFailedAgentTurns":{"totalCount":3,"failedTurns":[
              {"conversationId":"o2","conversationKind":"named","conversationTitle":"Planning the garden beds","ownerUsername":"lee","ownerName":"Lee Sample","failedAt":"2026-11-25T09:12:00Z","errorText":"The model service did not answer in time.","agentId":"agent-lee"},
              {"conversationId":"o4","conversationKind":"main","conversationTitle":"","ownerUsername":"avery","ownerName":"Avery Demo","failedAt":"2026-11-24T20:58:00Z","errorText":"This agent has used its limit for the day.","agentId":"agent-avery"},
              {"conversationId":"o3","conversationKind":"named","conversationTitle":"Birthday party ideas","ownerUsername":"morgan","ownerName":"Morgan Placeholder","failedAt":"2026-11-23T18:40:00Z","errorText":"A tool call was refused: the calendar is not connected.","agentId":"agent-morgan"}
            ]}}}
            """#

        /// Somebody else's conversation, as an operator reads it.
        private static let oversightObservedConversation = #"""
            {"data":{
              "ReadAgentConversation":{
                "conversation":{"id":"o2","kind":"named","title":"Planning the garden beds","summary":null,"lastAt":"2026-11-25T09:38:00Z"},
                "actingAs":"Lee Sample","isObserved":true,
                "todos":[],
                "messages":[
                  {"id":"1","createdAt":"2026-11-25T09:30:00Z","role":"user","content":"Which of these can go in the shady corner?"},
                  {"id":"2","createdAt":"2026-11-25T09:30:05Z","role":"assistant","content":"Ferns and hostas do well in shade. Tomatoes and peppers need at least six hours of sun, so keep them along the south fence."},
                  {"id":"3","createdAt":"2026-11-25T09:36:00Z","role":"user","content":"Make me a planting plan for the four beds."},
                  {"id":"4","createdAt":"2026-11-25T09:36:08Z","role":"assistant","content":"Here is a plan:\n\n- **Bed 1** (south fence): tomatoes and basil\n- **Bed 2**: peppers and marigolds\n- **Bed 3** (shady corner): ferns and hostas\n- **Bed 4**: lettuce and radishes, replanted every three weeks"}
                ],
                "total":4
              },
              "ListAgentInteractions":[]
            }}
            """#
    }
#endif
