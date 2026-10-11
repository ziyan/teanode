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
            } else if query.contains("ListMailboxes") {
                answer = Self.mailboxes
            } else if query.contains("ListMailboxThreads") {
                answer = Self.threads
            } else if query.contains("AgentGraphChildren") {
                answer = Self.knowledge
            } else if query.contains("FinanceProviders") {
                answer = #"{"data":{"FinanceProviders":[{"providerKind":"simplefin","isBrowserRequired":false}],"FinanceSources":[{"id":"bank"}]}}"#
            } else if query.contains("ReadAgent") {
                answer = #"{"data":{"ReadAgent":{"timezone":"UTC","categories":[]}}}"#
            } else if query.contains("ListCalendars") {
                answer = #"{"data":{"ListCalendars":[{"id":"home","name":"Home","timezone":null,"weekStart":"monday","calendarKind":"events"}]}}"#
            } else if query.contains("ListCalendarEvents") {
                answer = Self.events
            } else if query.contains("ListReminders") {
                answer = #"{"data":{"ListReminders":[]}}"#
            } else if query.contains("ListAddressBooks") {
                answer = #"{"data":{"ListAddressBooks":[{"id":"book","name":"Contacts","contacts":4}]}}"#
            } else if query.contains("ListContacts") {
                answer = Self.contacts
            } else if query.contains("AgentGraphPage") {
                answer = #"{"data":{"AgentGraphPage":{"contact":{"id":"robin"}}}}"#
            } else if query.contains("GetSession") {
                answer = #"{"data":{"GetSession":{"username":"robin"}}}"#
            } else {
                answer = #"{"data":{}}"#
            }
            return (Data(answer.utf8), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
        }

        /// The tab a screenshot opens on: `-TeaNodeTab calendar`.
        static var tab: String? {
            let arguments = ProcessInfo.processInfo.arguments
            guard let index = arguments.firstIndex(of: "-TeaNodeTab"), index + 1 < arguments.count else { return nil }
            return arguments[index + 1]
        }

        /// A few events this week, wherever the week falls.
        static var events: String {
            let today = Calendar.current.startOfDay(for: Date())
            func at(_ days: Int, _ hours: Double) -> String {
                CalendarDates.text(today.addingTimeInterval(Double(days) * 86400 + hours * 3600))
            }
            let day = Calendar.current.dateComponents([.year, .month, .day], from: today)
            let rows = [
                #"{"id":"e1","calendarId":"home","summary":"Plumber","location":"Home","startsAt":"\#(at(0, 9))","endsAt":"\#(at(0, 10))","allDay":false,"recurring":false,"occurrence":false,"status":"confirmed"}"#,
                #"{"id":"e2","calendarId":"home","summary":"Lunch with Sam","location":"The noodle place","startsAt":"\#(at(0, 13.5))","endsAt":"\#(at(0, 14.5))","allDay":false,"recurring":false,"occurrence":false,"status":"confirmed"}"#,
                #"{"id":"e3","calendarId":"home","summary":"Book club","location":null,"startsAt":"\#(at(2, 19))","endsAt":"\#(at(2, 21))","allDay":false,"recurring":true,"occurrence":false,"status":"confirmed"}"#,
                #"{"id":"e4","calendarId":"home","summary":"Garden day","location":null,"startsAt":"\#(CalendarDates.allDayText(day))","endsAt":"\#(CalendarDates.allDayText(CalendarDates.dayAfter(day)))","allDay":true,"recurring":false,"occurrence":false,"status":"confirmed"}"#,
            ]
            return #"{"data":{"ListCalendarEvents":["# + rows.joined(separator: ",") + "]}}"
        }

        static let mailboxes = #"""
            {"data":{"ListMailboxes":[{"mailbox":{"id":"box","name":"Robin","signatureText":"","signatureHtml":"",
              "addresses":[{"address":"robin@example.com","domain":"example.com","domainId":"d"}],"autoReply":null,
              "agent":{"granted":true,"draftReplies":true,"triage":{"enabled":true}}},
              "folders":[
                {"id":"inbox","mailboxId":"box","parentId":"","name":"Inbox","kind":"inbox","pinnedAt":null,"unread":2,"total":3,"noteCount":0},
                {"id":"sent","mailboxId":"box","parentId":"","name":"Sent","kind":"sent","pinnedAt":null,"unread":0,"total":9,"noteCount":0},
                {"id":"archive","mailboxId":"box","parentId":"","name":"Archive","kind":"archive","pinnedAt":null,"unread":0,"total":40,"noteCount":0},
                {"id":"trash","mailboxId":"box","parentId":"","name":"Trash","kind":"trash","pinnedAt":null,"unread":0,"total":0,"noteCount":0}],
              "unread":2,"starredUnread":0,"priorityUnread":1,"maxMessageSize":26214400}]}}
            """#

        static var threads: String {
            func at(_ hours: Double) -> String { CalendarDates.text(Date().addingTimeInterval(-hours * 3600)) }
            func row(_ id: String, _ sender: String, _ address: String, _ subject: String, _ summary: String, _ priority: String, isSeen: Bool, _ hours: Double) -> String {
                #"{"threadId":"\#(id)","count":1,"unread":\#(isSeen ? 0 : 1),"flagged":false,"participants":[],"itemIds":["\#(id)"],"hasDraft":false,"item":{"id":"\#(id)","folderId":"inbox","mailId":"\#(id)","seen":\#(isSeen),"flagged":false,"answered":false,"forwarded":false,"draft":false,"addedAt":"\#(at(hours))","subscriptionId":"","insight":{"category":"personal","priority":"\#(priority)","needsReply":\#(priority == "high"),"summary":"\#(summary)","actionItems":[],"notes":"","model":"","runId":"","proposals":[]},"mail":{"id":"\#(id)","from":"\#(address)","fromName":"\#(sender)","sender":"","subject":"\#(subject)","recipients":["robin@example.com"],"receivedAt":"\#(at(hours))","size":100,"messageId":"","listKey":"","listName":"","logoDomain":"","authenticationResults":{"spf":{"result":"pass"},"dkims":[{"result":"pass"}],"dmarc":{"result":"pass"},"spamFilter":{"score":0.2}}}}}"#
            }
            let rows = [
                row("m1", "The plumber", "plumber@example.com", "Thursday at 9", "Asks you to confirm Thursday at 9.", "high", isSeen: false, 1),
                row("m2", "Sam", "sam@example.org", "Lunch?", "Lunch on Friday at the noodle place.", "normal", isSeen: false, 3),
                row("m3", "Bookshop", "orders@bookshop.example", "Your order has shipped", "Your parcel arrives tomorrow.", "low", isSeen: true, 26),
            ]
            return #"{"data":{"ListMailboxThreads":{"total":3,"threads":["# + rows.joined(separator: ",") + "]}}}"
        }

        static let knowledge = #"""
            {"data":{"AgentGraphChildren":{"total":5,"rows":[
              {"node":{"id":"self","path":"self","kind":"person","name":"","summary":"Who you are, as your agent knows you."},"hint":"","children":0},
              {"node":{"id":"people","path":"people","kind":"folder","name":"People","summary":""},"hint":"Family, friends and the people you work with","children":24},
              {"node":{"id":"projects","path":"projects","kind":"folder","name":"Projects","summary":""},"hint":"What you are building and planning","children":9},
              {"node":{"id":"home","path":"home","kind":"topic","name":"Home","summary":"The house, the garden and what needs doing."},"hint":"","children":3},
              {"node":{"id":"travel","path":"travel","kind":"topic","name":"Travel","summary":"Trips taken and planned."},"hint":"","children":0}]}}}
            """#

        /// Whether a screenshot opens with the side menu out: `-TeaNodeMenu`.
        static var isMenuRequested: Bool {
            ProcessInfo.processInfo.arguments.contains("-TeaNodeMenu")
        }

        static let contacts = #"""
            {"data":{"ListContacts":[
              {"id":"plumber","name":"The plumber","organization":null,"emails":["plumber@example.com"],"phones":["+1 555 0100"],"hasPhoto":false,"addresses":[]},
              {"id":"robin","name":"Robin","organization":null,"emails":["robin@example.com"],"phones":[],"hasPhoto":false,"addresses":[]},
              {"id":"sam","name":"Sam","organization":null,"emails":["sam@example.org"],"phones":["+1 555 0142"],"hasPhoto":false,"addresses":[]},
              {"id":"shop","name":null,"organization":null,"emails":["orders@bookshop.example"],"phones":[],"hasPhoto":false,"addresses":[]}
            ]}}
            """#

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
                  {"id":"9","createdAt":"2026-10-10T09:05:02Z","role":"assistant","content":"Your parcel from the bookshop was left at the front door a few minutes ago."},
                  {"id":"10","createdAt":"2026-10-10T11:20:00Z","role":"user","content":"[herdr question] laptop w1:p1 f1"},
                  {"id":"11","createdAt":"2026-10-10T11:20:01Z","role":"assistant","content":"**Claude Code** in garden-planner asks:"},
                  {"id":"12","createdAt":"2026-10-10T11:20:01Z","role":"note","name":"herdr_asked","content":"{\"computer\":\"laptop\",\"paneId\":\"w1:p1\",\"questionFingerprint\":\"f1\",\"herdrSession\":{\"paneName\":\"garden-planner\",\"codingAgentKind\":\"claude\",\"question\":{\"questionFingerprint\":\"f1\",\"questionText\":\"The tests pass. Open a pull request now?\",\"isMultipleChoice\":false,\"options\":[{\"optionNumber\":1,\"optionLabel\":\"Yes\",\"herdrOptionKind\":\"choice\"},{\"optionNumber\":2,\"optionLabel\":\"Not yet\",\"optionDescription\":\"Keep working on the layout first\",\"herdrOptionKind\":\"choice\"},{\"optionNumber\":3,\"optionLabel\":\"Type something\",\"herdrOptionKind\":\"freeText\"}]}}}"}
                ],
                "total":12
              },
              "ListAgentInteractions":[
                {"runId":"r","callId":"c3","interactionKind":"approval","toolName":"mail_send","interactionText":"Send the reply to the plumber: \"Thursday at 9 works, see you then.\"","interactionChoices":null,"createdAt":"2026-10-10T09:01:04Z"}
              ]
            }}
            """#
    }
#endif
