#if DEBUG
    import SwiftUI
    import TeaNodeKit
    import UIKit

    // The invented people, circles, chats and shares that the screenshots of
    // the circles feature photograph (`-TeaNodeDemo`, docs/planning/circles-and-chats-execplan.md).
    // DemoServer answers the app's GraphQL here for the operations only these
    // screens ask: the same people everywhere, so a name in the circle's page
    // is the name above a message in its chat.

    /// DemoPerson is somebody the demo's circles and chats are made of.
    struct DemoPerson {
        let userId: String
        let name: String
        let avatarColorIndex: Int
        /// Whether they have a picture; without one, their initial is drawn.
        let hasPicture: Bool

        /// The picture's address as JSON, or null without one.
        var avatarUrlJSON: String { hasPicture ? #""/api/v1/avatar/user/\#(userId)?v=1""# : "null" }
    }

    extension DemoServer {
        static let demoPeople: [String: DemoPerson] = [
            "robin": DemoPerson(userId: "robin", name: "Robin", avatarColorIndex: 7, hasPicture: true),
            "sam": DemoPerson(userId: "sam", name: "Sam", avatarColorIndex: 2, hasPicture: true),
            "alex": DemoPerson(userId: "alex", name: "Alex", avatarColorIndex: 4, hasPicture: false),
            // A name of 60 characters, the longest a row has to hold.
            "jordan": DemoPerson(userId: "jordan", name: "Jordan Alexander Montgomery-Featherstonehaugh Wellington-Amy", avatarColorIndex: 9, hasPicture: true),
        ]

        static func demoPerson(_ userId: String) -> DemoPerson { demoPeople[userId]! }

        /// A time some days back at an hour of that day, in this zone, so that
        /// the chats read "Yesterday" and "Today" whenever they are photographed.
        static func demoTime(daysAgo: Int, hour: Int, minute: Int) -> String {
            let calendar = Calendar.current
            let day = calendar.date(byAdding: .day, value: -daysAgo, to: calendar.startOfDay(for: Date())) ?? Date()
            return CalendarDates.text(day.addingTimeInterval(Double(hour * 3600 + minute * 60)))
        }

        // MARK: The answers

        /// What the circles feature's screens ask beyond the rest of the demo, or
        /// nil for any other question.
        static func circlesAnswer(query: String, body: Data?) -> String? {
            let variables = requestVariables(body)
            if query.contains("ReadAgentConversation"), let page = chatPage(conversationId: variables["conversationId"] as? String) {
                return page
            }
            if query.contains("ListCircleInvitations") {
                return #"{"data":{"ListCircleInvitations":[\#(hikingInvitationJSON)]}}"#
            }
            if query.contains("ListCirclePeople") {
                let people = ["sam", "alex", "jordan"].map(personJSON).joined(separator: ",")
                return #"{"data":{"ListCirclePeople":[\#(people)]}}"#
            }
            if query.contains("ListCircles") {
                return #"{"data":{"ListCircles":[\#(homeCircleJSON),\#(bookClubCircleJSON)]}}"#
            }
            if query.contains("GetCurrentUser") {
                return
                    #"{"data":{"GetCurrentUser":{"id":"robin","username":"robin","name":"Robin","email":"robin@example.com","avatarUrl":"/api/v1/avatar/user/robin?v=1","avatarColorIndex":7,"timezone":"UTC","timezoneMode":"auto"}}}"#
            }
            // A circle's admin who does not run the server: no row to make an
            // account, so the pending invitation is still on the first screen.
            if query.contains("GetSession"), screen == "circle-detail" {
                return session.replacingOccurrences(of: #""user:manage","#, with: "")
            }
            if query.contains("ListCalendarFreeBusy") { return calendarFreeBusyAnswer(variables: variables) }
            if let screen, ["calendar-shared", "calendar-sheet", "calendar-share-sheet", "calendar-find-time"].contains(screen) {
                if query.contains("ListCalendars") { return #"{"data":{"ListCalendars":[\#(sharedCalendarsJSON)]}}"# }
                if query.contains("ListCalendarEvents"), screen == "calendar-shared" { return sharedCalendarEvents(variables: variables, body: body) }
            }
            if query.contains("ListCalendarShares") {
                return #"{"data":{"ListCalendarShares":[\#(calendarSharesJSON)]}}"#
            }
            // Two pages of the person's own, so that what others shared is on the first
            // screen even at the largest text.
            if query.contains("AgentGraphChildren"), screen == "knowledge-shared" {
                return knowledgeOwnPagesJSON
            }
            if query.contains("ListSharedAgentPages") {
                return #"{"data":{"ListSharedAgentPages":[\#(sharedKnowledgeRootsJSON)]}}"#
            }
            if query.contains("ReadSharedAgentPage") {
                return #"{"data":{"ReadSharedAgentPage":\#(sharedKnowledgePageJSON)}}"#
            }
            if query.contains("ListSkillShares") {
                return #"{"data":{"ListSkillShares":[\#(lentSkillShareJSON),\#(givenSkillShareJSON)]}}"#
            }
            if query.contains("ListSkillShareCalls") {
                return #"{"data":{"ListSkillShareCalls":[\#(waitingSkillShareCallJSON),\#(ranSkillShareCallJSON)]}}"#
            }
            // The installed skills the person could lend; the manage pages ask
            // for the same list with other fields.
            if query.contains("ListAgentSkills"), query.contains("isAmountDeclared") {
                return #"{"data":{"ListAgentSkills":[\#(lendableSkillJSON)]}}"#
            }
            return nil
        }

        /// The variables of a request, as the app sent them.
        static func requestVariables(_ body: Data?) -> [String: Any] {
            guard let body, let request = try? JSONSerialization.jsonObject(with: body) as? [String: Any] else { return [:] }
            return request["variables"] as? [String: Any] ?? [:]
        }

        /// The picture of a person, for a read of an address under
        /// `/api/v1/avatar/user/`: a head and shoulders on a color of the
        /// person's own, drawn here since no server is behind the demo.
        static func avatarPicture(for request: URLRequest) -> Data? {
            guard let path = request.url?.path, path.hasPrefix("/api/v1/avatar/user/") else { return nil }
            let hue = Double(path.unicodeScalars.reduce(0) { ($0 + Int($1.value)) % 360 }) / 360
            let side: CGFloat = 128
            let image = UIGraphicsImageRenderer(size: CGSize(width: side, height: side)).image { context in
                let drawing = context.cgContext
                drawing.setFillColor(UIColor(hue: hue, saturation: 0.35, brightness: 0.9, alpha: 1).cgColor)
                drawing.fill(CGRect(x: 0, y: 0, width: side, height: side))
                drawing.setFillColor(UIColor.white.withAlphaComponent(0.85).cgColor)
                drawing.fillEllipse(in: CGRect(x: side * 0.34, y: side * 0.18, width: side * 0.32, height: side * 0.32))
                drawing.fillEllipse(in: CGRect(x: side * 0.18, y: side * 0.56, width: side * 0.64, height: side * 0.7))
            }
            return image.pngData()
        }

        // MARK: Circles

        static func personJSON(_ userId: String) -> String {
            let person = demoPerson(userId)
            return
                #"{"userId":"\#(userId)","username":"\#(userId)","name":"\#(person.name)","avatarUrl":\#(person.avatarUrlJSON),"avatarColorIndex":\#(person.avatarColorIndex)}"#
        }

        static func circleMemberJSON(_ userId: String, memberRole: String) -> String {
            let person = demoPerson(userId)
            return
                #"{"userId":"\#(userId)","username":"\#(userId)","name":"\#(person.name)","avatarUrl":\#(person.avatarUrlJSON),"avatarColorIndex":\#(person.avatarColorIndex),"memberRole":"\#(memberRole)","joinedAt":"2026-09-01T09:00:00Z"}"#
        }

        /// The invitation to the hiking group, waiting for the person's answer.
        static let hikingInvitationJSON =
            #"{"id":"invitation-hiking","circleId":"circle-hiking","circleName":"Hiking group","circleKind":"friends","invitedByName":"Sam","invitedUsername":"robin","memberRole":"member","createdAt":"2026-10-15T09:00:00Z","expiresAt":"2026-10-29T09:00:00Z"}"#

        /// The family circle, as its admin reads it: four members and an
        /// invitation nobody has answered.
        static var homeCircleJSON: String {
            let members = [("robin", "admin"), ("sam", "member"), ("alex", "member"), ("jordan", "member")]
                .map { circleMemberJSON($0.0, memberRole: $0.1) }.joined(separator: ",")
            let pending =
                #"{"id":"invitation-casey","circleId":"circle-home","circleName":"Home","circleKind":"family","invitedByName":"Robin","invitedUsername":"casey","memberRole":"member","createdAt":"2026-10-15T09:00:00Z","expiresAt":"2026-10-29T09:00:00Z"}"#
            return
                #"{"id":"circle-home","name":"Home","circleKind":"family","createdAt":"2026-09-01T09:00:00Z","memberRole":"admin","members":[\#(members)],"pendingInvitations":[\#(pending)]}"#
        }

        /// A circle of friends the person is only a member of.
        static var bookClubCircleJSON: String {
            let members = [("sam", "admin"), ("robin", "member"), ("alex", "member")].map { circleMemberJSON($0.0, memberRole: $0.1) }
                .joined(separator: ",")
            return
                #"{"id":"circle-club","name":"Book club","circleKind":"friends","createdAt":"2026-09-03T09:00:00Z","memberRole":"member","members":[\#(members)],"pendingInvitations":[]}"#
        }

        // MARK: Chats

        static func chatMemberJSON(
            _ userId: String, memberRole: String = "member", readThroughMessageId: String? = nil, agentId: String? = nil,
            isAgentReading: Bool = false, isAgentAskableByOthers: Bool = false
        ) -> String {
            let person = demoPerson(userId)
            let readThrough = readThroughMessageId.map { #""\#($0)""# } ?? "null"
            let agentFields =
                agentId.map {
                    #","isAgentAskableByOthers":\#(isAgentAskableByOthers),"agentId":"\#($0)","agentName":"Agent","isAgentActive":true"#
                } ?? ""
            return
                #"{"userId":"\#(userId)","username":"\#(userId)","name":"\#(person.name)","avatarUrl":\#(person.avatarUrlJSON),"avatarColorIndex":\#(person.avatarColorIndex),"memberRole":"\#(memberRole)","isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":\#(readThrough),"isAgentReading":\#(isAgentReading)\#(agentFields)}"#
        }

        /// The direct chat with Sam as the conversation list and the chat's own
        /// page both say it.
        static func directChatConversationJSON(unreadCount: Int) -> String {
            let robin = chatMemberJSON("robin")
            let sam = chatMemberJSON("sam", readThroughMessageId: "c06")
            let lastAt = demoTime(daysAgo: 0, hour: 8, minute: 55)
            return
                #"{"id":"chat-sam","kind":"chat","title":null,"summary":null,"lastAt":"\#(lastAt)","chat":{"chatId":"chat-sam","chatKind":"direct","circleId":null,"title":null,"isCircleChat":false,"unreadCount":\#(unreadCount),"isMuted":false,"mutedUntil":null,"isRemoved":false,"members":[\#(robin),\#(sam)]}}"#
        }

        /// The family circle's chat: three people, each of whom has an agent
        /// there but Alex.
        static func groupChatConversationJSON(unreadCount: Int) -> String {
            let robin = chatMemberJSON("robin", memberRole: "admin", readThroughMessageId: "g06", agentId: "agent-robin", isAgentReading: true)
            let sam = chatMemberJSON(
                "sam", readThroughMessageId: "g06", agentId: "agent-sam", isAgentReading: true, isAgentAskableByOthers: true)
            let alex = chatMemberJSON("alex", readThroughMessageId: "g06")
            let lastAt = demoTime(daysAgo: 0, hour: 17, minute: 12)
            return
                #"{"id":"chat-home","kind":"chat","title":"Home","summary":null,"lastAt":"\#(lastAt)","chat":{"chatId":"chat-home","chatKind":"group","circleId":"circle-home","title":"Home","isCircleChat":true,"unreadCount":\#(unreadCount),"isMuted":false,"mutedUntil":null,"isRemoved":false,"members":[\#(robin),\#(sam),\#(alex)]}}"#
        }

        /// The chats in the conversation list: the group has messages waiting.
        static var directChatListEntry: String { directChatConversationJSON(unreadCount: 0) }
        static var groupChatListEntry: String { groupChatConversationJSON(unreadCount: 3) }

        static func chatMessageJSON(
            _ id: String, daysAgo: Int, hour: Int, minute: Int, from userId: String, _ content: String, reactions: String = "[]",
            references: String = "[]"
        ) -> String {
            let person = demoPerson(userId)
            let createdAt = demoTime(daysAgo: daysAgo, hour: hour, minute: minute)
            let isMine = userId == "robin"
            return
                #"{"id":"\#(id)","createdAt":"\#(createdAt)","role":"user","content":"\#(content)","reactions":\#(reactions),"references":\#(references),"authorKind":"person","authorUserId":"\#(userId)","authorName":"\#(person.name)","authorOwnerName":"","authorAvatarUrl":\#(person.avatarUrlJSON),"authorAvatarColorIndex":\#(person.avatarColorIndex),"authorAgentId":"","isMine":\#(isMine)}"#
        }

        /// A message by somebody's agent, said in front of the whole chat.
        static func chatAgentMessageJSON(_ id: String, daysAgo: Int, hour: Int, minute: Int, ownerUserId: String, _ content: String) -> String {
            let createdAt = demoTime(daysAgo: daysAgo, hour: hour, minute: minute)
            let ownerName = demoPerson(ownerUserId).name
            return
                #"{"id":"\#(id)","createdAt":"\#(createdAt)","role":"assistant","content":"\#(content)","authorKind":"agent","authorUserId":"\#(ownerUserId)","authorName":"Agent","authorOwnerName":"\#(ownerName)","authorAvatarUrl":null,"authorAvatarColorIndex":null,"authorAgentId":"agent-\#(ownerUserId)","isMine":false}"#
        }

        /// The direct chat: two days, a reply, a reaction and a read mark.
        static var directChatMessages: [String] {
            let reaction =
                #"[{"emoji":"👍","reactorKind":"person","reactedAt":"\#(demoTime(daysAgo: 0, hour: 8, minute: 41))","reactorUserId":"sam","reactorName":"Sam"}]"#
            let reply =
                #"[{"agentMessageId":"c04","quotedRole":"user","quotedText":"I found a recipe for an apple tart. Does that work?"}]"#
            return [
                chatMessageJSON("c01", daysAgo: 1, hour: 18, minute: 20, from: "sam", "Are you coming to dinner on Saturday?"),
                chatMessageJSON("c02", daysAgo: 1, hour: 18, minute: 24, from: "robin", "Yes! What can I bring?"),
                chatMessageJSON("c03", daysAgo: 1, hour: 18, minute: 25, from: "sam", "Just dessert. Mom is making her soup."),
                chatMessageJSON(
                    "c04", daysAgo: 0, hour: 8, minute: 40, from: "robin", "I found a recipe for an apple tart. Does that work?",
                    reactions: reaction),
                chatMessageJSON("c05", daysAgo: 0, hour: 8, minute: 52, from: "sam", "Perfect, bring two!", references: reply),
                chatMessageJSON("c06", daysAgo: 0, hour: 8, minute: 55, from: "robin", "Two it is."),
            ]
        }

        /// The circle's chat: a question for Sam's agent and its answer, which
        /// is told apart from an answer of anybody else's agent by whose it is.
        static var groupChatMessages: [String] {
            [
                chatMessageJSON("g01", daysAgo: 0, hour: 17, minute: 2, from: "alex", "Who is picking up groceries tonight?"),
                chatMessageJSON("g02", daysAgo: 0, hour: 17, minute: 5, from: "robin", "I can, on my way home."),
                chatMessageJSON("g03", daysAgo: 0, hour: 17, minute: 7, from: "sam", "@Agent when does the farmers market close today?"),
                chatAgentMessageJSON(
                    "g04", daysAgo: 0, hour: 17, minute: 7, ownerUserId: "sam",
                    "The farmers market closes at 4 pm today, so it is closed by now. The corner shop is open until 10 pm."),
                chatMessageJSON("g05", daysAgo: 0, hour: 17, minute: 10, from: "alex", "Thanks, I will make the list."),
                chatMessageJSON("g06", daysAgo: 0, hour: 17, minute: 12, from: "robin", "Great, I will start the rice."),
            ]
        }

        /// What a read of the chat answers with, or nil for a conversation
        /// that is not one of the demo's chats.
        static func chatPage(conversationId: String?) -> String? {
            let conversation: String
            let messages: [String]
            switch conversationId {
            case "chat-sam"?:
                conversation = directChatConversationJSON(unreadCount: 0)
                messages = directChatMessages
            case "chat-home"?:
                conversation = groupChatConversationJSON(unreadCount: 0)
                messages = groupChatMessages
            default:
                return nil
            }
            return
                #"{"data":{"ReadAgentConversation":{"conversation":\#(conversation),"todos":[],"messages":[\#(messages.joined(separator: ","))],"total":\#(messages.count)},"ListAgentInteractions":[]}}"#
        }

        /// The chat a screenshot opens (`-TeaNodeScreen chat-direct`, `chat-group`
        /// or `chat-members`), or nil for any other screen.
        static var openedChat: AgentConversation? {
            switch screen ?? "" {
            case "chat-direct": AgentConversation(id: "chat-sam", kind: "chat", title: nil, summary: nil, lastAt: nil)
            case "chat-group", "chat-members": AgentConversation(id: "chat-home", kind: "chat", title: "Home", summary: nil, lastAt: nil)
            default: nil
            }
        }

        // MARK: Calendars

        /// The person's own calendar, the family circle's, and two that other
        /// people shared at different levels.
        static let sharedCalendarsJSON = #"""
            {"id":"home","name":"Personal","colour":"#2F7FE0","timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"owner","ownerUserId":"robin","ownerName":"Robin"},
            {"id":"circle-home","name":"Home","colour":null,"timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"edit","circleId":"circle-home","circleName":"Home","isCircleCalendar":true,"colorIndex":4},
            {"id":"sam-calendar","name":"Calendar","colour":null,"timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"titles","ownerUserId":"sam","ownerName":"Sam","ownerAvatarUrl":"/api/v1/avatar/user/sam?v=1","ownerAvatarColorIndex":2,"colorIndex":1},
            {"id":"alex-calendar","name":"Calendar","colour":null,"timezone":null,"weekStart":"monday","calendarKind":"events","shareLevel":"busy","ownerUserId":"alex","ownerName":"Alex","ownerAvatarColorIndex":4,"colorIndex":6}
            """#

        /// When each person asked about is busy: Sam and Alex shared their calendars
        /// and have a few things on each of the next days, Jordan shared nothing, and
        /// the person has an hour of their own.
        static func calendarFreeBusyAnswer(variables: [String: Any]) -> String {
            let local = Calendar.current
            let today = local.startOfDay(for: Date())
            // Each busy stretch as a day from today and the hours of that day it runs between.
            func periods(_ busy: [(Int, Double, Double)]) -> String {
                busy.map { day, from, to -> String in
                    let start = local.date(byAdding: .day, value: day, to: today) ?? today
                    return #"{"startsAt":"\#(CalendarDates.text(start.addingTimeInterval(from * 3600)))","endsAt":"\#(CalendarDates.text(start.addingTimeInterval(to * 3600)))"}"#
                }.joined(separator: ",")
            }
            let week = Array(0..<7)
            let robinBusy: [(Int, Double, Double)] = week.map { ($0, 11.0, 12.0) }
            let samBusy: [(Int, Double, Double)] = week.flatMap { [($0, 10.0, 11.5), ($0, 15.0, 16.0)] }
            let alexBusy: [(Int, Double, Double)] = week.flatMap { [($0, 9.5, 10.5), ($0, 13.0, 14.0)] }
            let answers = (variables["userIds"] as? [String] ?? []).map { userId -> String in
                switch userId {
                case "robin":
                    return #"{"userId":"robin","username":"robin","name":"Robin","isShared":true,"periods":[\#(periods(robinBusy))]}"#
                case "sam":
                    return #"{"userId":"sam","username":"sam","name":"Sam","isShared":true,"periods":[\#(periods(samBusy))]}"#
                case "alex":
                    return #"{"userId":"alex","username":"alex","name":"Alex","isShared":true,"periods":[\#(periods(alexBusy))]}"#
                default:
                    return #"{"userId":"\#(userId)","username":"","name":"","isShared":false,"periods":[]}"#
                }
            }
            return #"{"data":{"ListCalendarFreeBusy":[\#(answers.joined(separator: ","))]}}"#
        }

        /// Who the person's calendar is shared with: one person and one circle.
        static let calendarSharesJSON = #"""
            {"id":"share-sam","calendarId":"home","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeCircleId":null,"granteeCircleName":null,"shareLevel":"details"},
            {"id":"share-home","calendarId":"home","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"busy"}
            """#

        /// The events of a calendar for the window asked for. The person's own
        /// calendar has the usual demo events and a private one, and the others
        /// have a few of today's, each from the level it is shared at.
        static func sharedCalendarEvents(variables: [String: Any], body: Data?) -> String {
            let calendarId = variables["calendarId"] as? String ?? ""
            let local = Calendar.current
            let today = local.startOfDay(for: Date())
            var from = today
            var until = local.date(byAdding: .day, value: 7, to: today) ?? today
            if let text = variables["from"] as? String, let date = ServerTime.date(text) { from = date }
            if let text = variables["until"] as? String, let date = ServerTime.date(text) { until = date }
            func event(_ id: String, _ summary: String, _ location: String?, _ startHour: Double, _ endHour: Double, isBusyOnly: Bool = false, isPrivate: Bool = false)
                -> String
            {
                let startsAt = today.addingTimeInterval(startHour * 3600)
                let location = location.map { #""\#($0)""# } ?? "null"
                return
                    #"{"id":"\#(id)","calendarId":"\#(calendarId)","summary":"\#(summary)","location":\#(location),"startsAt":"\#(CalendarDates.text(startsAt))","endsAt":"\#(CalendarDates.text(today.addingTimeInterval(endHour * 3600)))","allDay":false,"recurring":false,"occurrence":false,"status":"confirmed","isBusyOnly":\#(isBusyOnly),"isPrivate":\#(isPrivate)}"#
            }
            var rows: [String] = []
            switch calendarId {
            case "home": rows = [event("doctor", "Doctor", nil, 11, 12, isPrivate: true)]
            case "circle-home": rows = [event("family-dinner", "Family dinner", "Home", 18.5, 20)]
            case "sam-calendar": rows = [event("soccer", "Soccer practice", "The park", 16, 17.5), event("recital", "Piano recital", "Music school", 19, 20)]
            case "alex-calendar":
                rows = [event("busy-morning", "", nil, 10.5, 12, isBusyOnly: true), event("busy-afternoon", "", nil, 15, 16.5, isBusyOnly: true)]
            default: break
            }
            // The rows are today's: they are left out of a window that does not
            // reach it.
            if from > today.addingTimeInterval(12 * 3600) || until <= today { rows = [] }
            let extra = rows.joined(separator: ",")
            guard calendarId == "home" else { return #"{"data":{"ListCalendarEvents":[\#(extra)]}}"# }
            let usual = events(answering: body)
            if extra.isEmpty { return usual }
            if usual.hasSuffix("[]}}") { return String(usual.dropLast(3)) + extra + "]}}" }
            return String(usual.dropLast(3)) + "," + extra + "]}}"
        }

        // MARK: Knowledge

        static let knowledgeOwnPagesJSON = #"""
            {"data":{"AgentGraphChildren":{"total":2,"rows":[
              {"node":{"id":"people","path":"people","kind":"folder","name":"People","summary":""},"hint":"Family, friends and the people you work with","children":24},
              {"node":{"id":"home","path":"home","kind":"topic","name":"Home","summary":"The house, the garden and what needs doing."},"hint":"","children":3}]}}}
            """#

        /// The pages two people shared with the person: two of Sam's and one of
        /// Alex's, who has no picture.
        static let sharedKnowledgeRootsJSON = #"""
            {"ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","ownerAvatarUrl":"/api/v1/avatar/user/sam?v=1","ownerAvatarColorIndex":2,"path":"recipes","name":"Recipes","nodeKind":"folder","opening":"What the family cooks, who likes what, and where each recipe came from.","isQuoteShared":false},
            {"ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","ownerAvatarUrl":"/api/v1/avatar/user/sam?v=1","ownerAvatarColorIndex":2,"path":"travel/coast-trip","name":"Coast trip","nodeKind":"topic","opening":"Dates, the cottage and what to pack for the autumn trip.","isQuoteShared":false},
            {"ownerUserId":"alex","ownerUsername":"alex","ownerName":"Alex","ownerAvatarUrl":null,"ownerAvatarColorIndex":4,"path":"projects/garden","name":"Garden","nodeKind":"topic","opening":"The beds, the seeds and the watering schedule.","isQuoteShared":true}
            """#

        /// One of Sam's pages as the person reads it.
        static let sharedKnowledgePageJSON = #"""
            {"ownerUserId":"sam","ownerUsername":"sam","ownerName":"Sam","ownerAvatarUrl":"/api/v1/avatar/user/sam?v=1","ownerAvatarColorIndex":2,
              "path":"recipes/apple-tart","name":"Apple tart","nodeKind":"topic","aliases":["tarte aux pommes"],
              "opening":"A shortcrust apple tart for eight, baked the day before. Sam makes it for family dinners.","isQuoteShared":false,
              "facts":[
                {"number":1,"factKind":"fact","text":"The pastry rests in the fridge overnight.","happenedAt":null,"isInferred":false,"evidenceSource":"A note","evidenceQuote":null},
                {"number":2,"factKind":"fact","text":"It serves eight, with cream.","happenedAt":null,"isInferred":false,"evidenceSource":"A message","evidenceQuote":null},
                {"number":3,"factKind":"fact","text":"Robin likes it with less sugar.","happenedAt":null,"isInferred":true,"evidenceSource":null,"evidenceQuote":null}],
              "edges":[{"fromPath":"recipes/apple-tart","toPath":"recipes/shortcrust","relation":"uses","isProposed":false}],
              "children":[{"path":"recipes/apple-tart/variations","name":"Variations","opening":"Pear and almond versions."}]}
            """#

        // MARK: Skills

        /// A skill of the person's own that can be lent: one tool pays, one does not.
        static let lendableSkillJSON = #"""
            {"name":"grocery-orders","description":"Order the weekly shop for pickup.","enabled":true,"readable":true,"tools":[
              {"name":"place_order","description":"Pays for an order.","needsComputer":true,"isAmountDeclared":true,"isScoped":false},
              {"name":"list_orders","description":"Lists recent orders.","needsComputer":true,"isAmountDeclared":false,"isScoped":false}]}
            """#

        /// A skill the person lent to Sam, with limits and a call waiting.
        static let lentSkillShareJSON = #"""
            {"id":"share-grocery","skillName":"grocery-orders","toolNames":["place_order"],"ownerUserId":"robin","ownerUsername":"robin","ownerName":"Robin","ownerAvatarUrl":"/api/v1/avatar/user/robin?v=1","ownerAvatarColorIndex":7,
              "granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeAvatarUrl":"/api/v1/avatar/user/sam?v=1","granteeAvatarColorIndex":2,
              "paymentLimitMinor":10000,"monthlyLimitMinor":40000,"currencyCode":"usd","isApprovalRequired":false,"isOwnedByCaller":true,"waitingCallCount":1,"monthTotalMinor":12500}
            """#

        /// A skill Alex lent to the person.
        static let givenSkillShareJSON = #"""
            {"id":"share-prints","skillName":"photo-prints","toolNames":["order_prints"],"ownerUserId":"alex","ownerUsername":"alex","ownerName":"Alex","ownerAvatarUrl":null,"ownerAvatarColorIndex":4,
              "granteeUserId":"robin","granteeUsername":"robin","granteeName":"Robin","granteeAvatarUrl":"/api/v1/avatar/user/robin?v=1","granteeAvatarColorIndex":7,
              "paymentLimitMinor":null,"monthlyLimitMinor":null,"currencyCode":"","isApprovalRequired":true,"isOwnedByCaller":false,"waitingCallCount":0,"monthTotalMinor":0}
            """#

        /// A call above the limit, waiting for the owner.
        static var waitingSkillShareCallJSON: String {
            let createdAt = demoTime(daysAgo: 0, hour: 7, minute: 30)
            let expiresAt = CalendarDates.text(Date().addingTimeInterval(3 * 24 * 3600))
            return
                #"{"id":"call-weekly-shop","skillShareId":"share-grocery","skillName":"grocery-orders","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeAvatarUrl":"/api/v1/avatar/user/sam?v=1","granteeAvatarColorIndex":2,"ownerName":"Robin","toolName":"place_order","toolArguments":"{\"item_count\":12,\"pickup\":\"Saturday morning\",\"amount_minor\":14800,\"currency\":\"usd\",\"request_reason\":\"The weekly shop for Saturday's dinner.\"}","requestReason":"The weekly shop for Saturday's dinner.","amountMinor":14800,"currencyCode":"usd","callState":"waiting_for_approval","toolOutput":"","answeredAt":null,"answerNote":"","expiresAt":"\#(expiresAt)","createdAt":"\#(createdAt)"}"#
        }

        /// An earlier call that ran.
        static var ranSkillShareCallJSON: String {
            let createdAt = demoTime(daysAgo: 6, hour: 9, minute: 5)
            return
                #"{"id":"call-earlier","skillShareId":"share-grocery","skillName":"grocery-orders","granteeUserId":"sam","granteeUsername":"sam","granteeName":"Sam","granteeAvatarUrl":"/api/v1/avatar/user/sam?v=1","granteeAvatarColorIndex":2,"ownerName":"Robin","toolName":"place_order","toolArguments":"{\"item_count\":5,\"amount_minor\":4250,\"currency\":\"usd\"}","requestReason":"Ingredients for the soup.","amountMinor":4250,"currencyCode":"usd","callState":"ran","toolOutput":"Order placed.","answeredAt":null,"answerNote":"","expiresAt":null,"createdAt":"\#(createdAt)"}"#
        }

        /// The skill the person lent to Sam, as the share's calls are opened from.
        static var lentSkillShare: SkillShare { recordingObject(lentSkillShareJSON, as: SkillShare.self) }
    }

    /// CircleDetailDemoScreen is the page of the family circle, which the
    /// circles list pushes, opened as it is for a screenshot
    /// (`-TeaNodeScreen circle-detail`). The page scrolls itself to the
    /// invitation waiting for an answer, so the members above it and the
    /// invitation are on one screen.
    struct CircleDetailDemoScreen: View {
        @State private var directory: CircleDirectory

        init(connection: Connection) {
            _directory = State(initialValue: CircleDirectory(connection: connection))
        }

        var body: some View {
            CircleDetailView(directory: directory, circleId: "circle-home")
                .task { await directory.load() }
        }
    }
#endif
