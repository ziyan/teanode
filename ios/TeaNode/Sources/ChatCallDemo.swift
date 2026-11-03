#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented calls that the screenshots of calls in chats photograph
    // (`-TeaNodeDemo`, docs/subsystems/chat-calls.md): the banner of a chat
    // with a call going (`-TeaNodeScreen chat-call-banner`, the Home circle's
    // chat), the call screen with three people (`chat-call-screen`), the same
    // call put away to its bar (`chat-call-minimized`), and a chat with calls
    // in it (`chat-call-lines`, the direct chat): one that
    // ended with its transcript, one missed and one going. The same people as
    // the rest of the demo.

    extension DemoServer {
        /// A time of a day some days back, plus some seconds, in the server's
        /// form.
        static func demoMoment(daysAgo: Int, hour: Int, minute: Int, plusSeconds: Double = 0) -> String {
            let day = Calendar.current.date(byAdding: .day, value: -daysAgo, to: Calendar.current.startOfDay(for: Date())) ?? Date()
            return CalendarDates.text(day.addingTimeInterval(Double(hour * 3600 + minute * 60) + plusSeconds))
        }

        private static func quoted(_ text: String?) -> String {
            text.map { "\"\($0)\"" } ?? "null"
        }

        /// One person's part in a call, as a call's view tells it.
        private static func callMemberJSON(
            _ userId: String, ringState: String, isOn: Bool, isVideoOn: Bool = false, isMuted: Bool = false, joinedAt: String?
        ) -> String {
            let person = demoPerson(userId)
            return
                #"{"userId":"\#(userId)","name":"\#(person.name)","avatarUrl":\#(person.avatarUrlJSON),"avatarColorIndex":\#(person.avatarColorIndex),"ringState":"\#(ringState)","isOn":\#(isOn),"isVideoOn":\#(isVideoOn),"isMuted":\#(isMuted),"joinedAt":\#(quoted(joinedAt)),"leftAt":null}"#
        }

        /// A call as the person sees it.
        private static func callViewJSON(
            id: String, conversationId: String, chatTitle: String, isGroup: Bool, startedBy: String, startedAt: String, answeredAt: String?,
            askerRingState: String, isAskerOn: Bool, transcriptionAskedBy: String? = nil, members: [String]
        ) -> String {
            let startedByName = demoPerson(startedBy).name
            return
                #"{"id":"\#(id)","conversationId":"\#(conversationId)","chatTitle":"\#(chatTitle)","isGroup":\#(isGroup),"startedByUserId":"\#(startedBy)","startedByName":"\#(startedByName)","startedAt":"\#(startedAt)","answeredAt":\#(quoted(answeredAt)),"endedAt":null,"endReason":"","isGoing":true,"durationMs":0,"askerRingState":"\#(askerRingState)","isAskerOn":\#(isAskerOn),"transcriptionAskedByUserId":\#(quoted(transcriptionAskedBy)),"transcriptState":"","summaryMessageId":null,"members":[\#(members.joined(separator: ","))]}"#
        }

        /// The call going in the Home circle's chat: Sam and Alex are on it,
        /// the person has not joined.
        private static var homeCallJSON: String {
            let startedAt = CalendarDates.text(Date().addingTimeInterval(-95))
            return callViewJSON(
                id: "call-home", conversationId: "chat-home", chatTitle: "Home", isGroup: true, startedBy: "sam", startedAt: startedAt,
                answeredAt: startedAt, askerRingState: "ringing", isAskerOn: false,
                members: [
                    callMemberJSON("sam", ringState: "none", isOn: true, joinedAt: startedAt),
                    callMemberJSON("alex", ringState: "answered", isOn: true, joinedAt: startedAt),
                    callMemberJSON("robin", ringState: "ringing", isOn: false, joinedAt: nil),
                ])
        }

        /// The call going in the direct chat with Sam, the third line of the
        /// chat of calls: Sam started it and is on it.
        private static var directCallJSON: String {
            let startedAt = demoMoment(daysAgo: 0, hour: 9, minute: 20)
            return callViewJSON(
                id: "call-direct-going", conversationId: "chat-sam", chatTitle: "Sam", isGroup: false, startedBy: "sam", startedAt: startedAt,
                answeredAt: nil, askerRingState: "ringing", isAskerOn: false,
                members: [
                    callMemberJSON("sam", ringState: "none", isOn: true, joinedAt: startedAt),
                    callMemberJSON("robin", ringState: "ringing", isOn: false, joinedAt: nil),
                ])
        }

        /// The call of the call screen: Robin, Sam (with his camera on) and
        /// Alex, who has turned on transcription and is muted. Four minutes
        /// twelve in.
        static var callScreenJSON: String {
            let answeredAt = CalendarDates.text(Date().addingTimeInterval(-252))
            return callViewJSON(
                id: "call-screen", conversationId: "chat-home", chatTitle: "Home", isGroup: true, startedBy: "alex", startedAt: answeredAt,
                answeredAt: answeredAt, askerRingState: "answered", isAskerOn: true, transcriptionAskedBy: "alex",
                members: [
                    callMemberJSON("robin", ringState: "answered", isOn: true, joinedAt: answeredAt),
                    callMemberJSON("sam", ringState: "answered", isOn: true, isVideoOn: true, joinedAt: answeredAt),
                    callMemberJSON("alex", ringState: "none", isOn: true, isMuted: true, joinedAt: answeredAt),
                ])
        }

        /// A call as a chat's message carries it.
        private static func callRecordJSON(
            id: String, startedBy: String, startedAt: String, answeredAt: String?, endedAt: String?, endReason: String, transcriptState: String,
            members: String
        ) -> String {
            #"{"id":"\#(id)","startedByUserId":"\#(startedBy)","startedAt":"\#(startedAt)","answeredAt":\#(quoted(answeredAt)),"endedAt":\#(quoted(endedAt)),"endReason":"\#(endReason)","transcriptionAskedByUserId":null,"transcriptState":"\#(transcriptState)","summaryMessageId":null,"members":[\#(members)]}"#
        }

        private static func callRecordMemberJSON(_ userId: String, ringState: String, joinedAt: String?, leftAt: String?) -> String {
            #"{"userId":"\#(userId)","ringState":"\#(ringState)","joinedAt":\#(quoted(joinedAt)),"leftAt":\#(quoted(leftAt)),"isVideoOn":false}"#
        }

        /// The line of a call in a chat: a message with no words.
        private static func callLineJSON(_ id: String, from userId: String, at: String, record: String) -> String {
            let person = demoPerson(userId)
            let callId = id
            return
                #"{"id":"\#(id)","createdAt":"\#(at)","role":"user","content":"","reactions":[],"references":[],"authorKind":"person","authorUserId":"\#(userId)","authorName":"\#(person.name)","authorOwnerName":"","authorAvatarUrl":\#(person.avatarUrlJSON),"authorAvatarColorIndex":\#(person.avatarColorIndex),"authorAgentId":"","isMine":\#(userId == "robin"),"mentionedUserIds":[],"isAllMentioned":false,"callId":"\#(callId)","call":\#(record)}"#
        }

        /// The calls of the direct chat with Sam: one Robin started that went
        /// four minutes twelve and was transcribed, one Sam made that Robin
        /// missed, and one going now.
        static var directChatCallMessages: [String] {
            let endedStartedAt = demoMoment(daysAgo: 1, hour: 18, minute: 40)
            let endedAnsweredAt = demoMoment(daysAgo: 1, hour: 18, minute: 40, plusSeconds: 20)
            let endedEndedAt = demoMoment(daysAgo: 1, hour: 18, minute: 40, plusSeconds: 272)
            let ended = callRecordJSON(
                id: "call-direct-ended", startedBy: "robin", startedAt: endedStartedAt, answeredAt: endedAnsweredAt, endedAt: endedEndedAt,
                endReason: "ended", transcriptState: "done",
                members: [
                    callRecordMemberJSON("robin", ringState: "none", joinedAt: endedStartedAt, leftAt: endedEndedAt),
                    callRecordMemberJSON("sam", ringState: "answered", joinedAt: endedAnsweredAt, leftAt: endedEndedAt),
                ].joined(separator: ","))
            let missedStartedAt = demoMoment(daysAgo: 0, hour: 8, minute: 20)
            let missedEndedAt = demoMoment(daysAgo: 0, hour: 8, minute: 21)
            let missed = callRecordJSON(
                id: "call-direct-missed", startedBy: "sam", startedAt: missedStartedAt, answeredAt: nil, endedAt: missedEndedAt, endReason: "missed",
                transcriptState: "",
                members: [
                    callRecordMemberJSON("sam", ringState: "none", joinedAt: missedStartedAt, leftAt: missedEndedAt),
                    callRecordMemberJSON("robin", ringState: "missed", joinedAt: nil, leftAt: nil),
                ].joined(separator: ","))
            let goingStartedAt = demoMoment(daysAgo: 0, hour: 9, minute: 20)
            let going = callRecordJSON(
                id: "call-direct-going", startedBy: "sam", startedAt: goingStartedAt, answeredAt: nil, endedAt: nil, endReason: "", transcriptState: "",
                members: [
                    callRecordMemberJSON("sam", ringState: "none", joinedAt: goingStartedAt, leftAt: nil),
                    callRecordMemberJSON("robin", ringState: "ringing", joinedAt: nil, leftAt: nil),
                ].joined(separator: ","))
            return [
                callLineJSON("call-direct-ended", from: "robin", at: endedStartedAt, record: ended),
                callLineJSON("call-direct-missed", from: "sam", at: missedStartedAt, record: missed),
                callLineJSON("call-direct-going", from: "sam", at: goingStartedAt, record: going),
            ]
        }

        /// What the calls' operations answer in the demo, or nil for any
        /// other question.
        static func chatCallAnswer(query: String, body: Data?) -> String? {
            if query.contains("ReadGoingChatCall") {
                let conversationId = requestVariables(body)["conversationId"] as? String ?? ""
                if screen == "chat-call-banner", conversationId == "chat-home" {
                    return #"{"data":{"ReadGoingChatCall":\#(homeCallJSON)}}"#
                }
                if screen == "chat-call-lines", conversationId == "chat-sam" {
                    return #"{"data":{"ReadGoingChatCall":\#(directCallJSON)}}"#
                }
                return #"{"data":{"ReadGoingChatCall":null}}"#
            }
            if query.contains("ListGoingChatCalls") {
                return #"{"data":{"ListGoingChatCalls":[]}}"#
            }
            if query.contains("ReadChatCall") {
                let segments = [
                    (1, "robin", "Robin", 4_000, 9_000, "Are we still having the tart on Saturday?"),
                    (2, "sam", "Sam", 10_000, 15_000, "Yes, bring two. Mom is making soup."),
                    (3, "robin", "Robin", 66_000, 70_000, "Perfect, I will be there at six."),
                ].map { number, userId, speaker, start, end, text -> String in
                    #"{"id":"segment-\#(number)","callId":"call-direct-ended","userId":"\#(userId)","speakerName":"\#(speaker)","startMs":\#(start),"endMs":\#(end),"text":"\#(text)"}"#
                }
                let startedAt = demoMoment(daysAgo: 1, hour: 18, minute: 40)
                let call = callViewJSON(
                    id: "call-direct-ended", conversationId: "chat-sam", chatTitle: "Sam", isGroup: false, startedBy: "robin", startedAt: startedAt,
                    answeredAt: startedAt, askerRingState: "none", isAskerOn: false, members: [])
                return #"{"data":{"ReadChatCall":{"call":\#(call),"transcript":[\#(segments.joined(separator: ","))]}}}"#
            }
            return nil
        }
    }

    extension ChatCallManager {
        /// `-TeaNodeScreen chat-call-screen`: the call screen with three
        /// people, one with his camera on, and the notice that Alex turned on
        /// transcription; `chat-call-minimized`: the same call put away.
        func showDemoCall(isMinimized: Bool = false) {
            guard let call = try? JSONDecoder().decode(ChatCallView.self, from: Data(DemoServer.callScreenJSON.utf8)) else { return }
            let demo = ChatCallSession(
                uuid: UUID(), accountKey: DemoServer.account.accountKey, conversationId: call.conversationId, title: call.chatTitle,
                isOutgoing: false)
            demo.callId = call.id
            demo.call = call
            demo.ownUserId = "robin"
            demo.phase = .connected
            demo.isMinimized = isMinimized
            showDemo(session: demo)
        }
    }
#endif
