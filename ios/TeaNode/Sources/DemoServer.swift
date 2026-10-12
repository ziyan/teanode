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
            } else if query.contains("FinanceSpendingSummary") {
                answer = Self.spendingSummary
            } else if query.contains("CashFlow") {
                answer = Self.cashFlow()
            } else if query.contains("NetWorth") {
                answer = Self.netWorth(answering: request.httpBody)
            } else if query.contains("SpendingCategories") {
                answer = Self.spendingCategories
            } else if query.contains("FinanceAccounts") {
                answer = #"{"data":{"FinanceAccounts":[]}}"#
            } else if query.contains("ReportingCurrency") {
                answer = #"{"data":{"ReportingCurrency":{"reportingCurrencyCode":"USD","isChosen":true}}}"#
            } else if query.contains("ReadAgentVoice") {
                // No voice, so the conversation shows no call button.
                answer = #"{"data":{"ReadAgentVoice":{"isVoiceAvailable":false,"sampleRate":null}}}"#
            } else if query.contains("ReadAgent") {
                answer = Self.agent
            } else if query.contains("ListAgentDreams") {
                answer = Self.dreams
            } else if query.contains("AgentDreamState") {
                answer = #"{"data":{"AgentDreamState":{"dreamJobStatus":"","queuedAt":null,"startedAt":null}}}"#
            } else if query.contains("AgentReadingProgress") {
                answer = #"{"data":{"AgentReadingProgress":{"waiting":340,"read":2860,"perHour":410,"hoursLeft":0.83,"bootstrapping":false,"spent":1.84,"costLeft":0.22,"currency":"USD","dreams":5}}}"#
            } else if query.contains("ListAgentSchedules") {
                answer = Self.schedules
            } else if query.contains("ListSessions") {
                answer = Self.sessions
            } else if query.contains("ListAgentPhones") {
                answer = Self.phones
            } else if query.contains("ListUsers") {
                answer = Self.people
            } else if query.contains("ListGroups") {
                answer = Self.groups
            } else if query.contains("ListRoles") {
                answer = Self.roles
            } else if query.contains("ListDomains") {
                answer = #"{"data":{"ListDomains":[{"id":"d","domain":"example.com"},{"id":"d2","domain":"example.net"}]}}"#
            } else if query.contains("GetSettings") {
                answer = Self.serverSettings
            } else if query.contains("ListCalendars") {
                answer = ##"{"data":{"ListCalendars":[{"id":"home","name":"Home","colour":"#2F7FE0","timezone":null,"weekStart":"monday","calendarKind":"events"}]}}"##
            } else if query.contains("ListCalendarEvents") {
                answer = Self.events(answering: request.httpBody)
            } else if query.contains("ListReminders") {
                answer = #"{"data":{"ListReminders":[]}}"#
            } else if query.contains("ListAddressBooks") {
                answer = #"{"data":{"ListAddressBooks":[{"id":"book","name":"Contacts","contacts":4}]}}"#
            } else if query.contains("ListContacts") {
                answer = Self.contacts
            } else if query.contains("AgentGraphPage") {
                answer = #"{"data":{"AgentGraphPage":{"contact":{"id":"robin"}}}}"#
            } else if query.contains("ListAgentIdeas") {
                answer = Self.ideas
            } else if query.contains("ListAgentGoals") {
                answer = Self.goals
            } else if query.contains("GetSession") {
                answer = Self.session
            } else {
                answer = #"{"data":{}}"#
            }
            return (Data(answer.utf8), HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!)
        }

        static let goals = #"""
            {"data":{"ListAgentGoals":[
            {"conversationId":"g1","goalTitle":"Plan the spring trip","goalDescription":"Find dates and a place everyone can make.","goalState":"waiting","goalStatus":"Two weekends work; which one do you prefer?","goalSetAt":"2026-10-01T09:00:00Z","goalNextAt":null,"lastAt":"2026-10-10T08:00:00Z"},
            {"conversationId":"g2","goalTitle":"Keep the reading list short","goalDescription":"Each week, drop what I will not read.","goalState":"working","goalStatus":"Dropped four articles this week.","goalSetAt":"2026-09-20T09:00:00Z","goalNextAt":"2026-10-17T09:00:00Z","lastAt":"2026-10-10T07:00:00Z"},
            {"conversationId":"g3","goalTitle":"Renew the passport","goalDescription":"Before the summer.","goalState":"met","goalStatus":"Renewed; the new one arrived.","goalSetAt":"2026-08-01T09:00:00Z","goalNextAt":null,"lastAt":"2026-09-02T07:00:00Z"}
            ]}}
            """#

        static let ideas = #"""
            {"data":{"ListAgentIdeas":{"ideas":[
            {"id":"i1","ideaKind":"personal","ideaCategory":null,"emoji":"🔧","headline":"Confirm the plumber for Thursday","body":"They asked you to confirm the 9:00 visit.","openingRequest":"Reply to the plumber that Thursday at 9 works.","suggestionReason":"From their message yesterday.","ideaStatus":"open","createdAt":"2026-10-10T08:00:00Z"},
            {"id":"i2","ideaKind":"catalog","ideaCategory":"home","emoji":"🪴","headline":"Remind me to water the plants","body":"A weekly reminder on the day you choose.","openingRequest":"Remind me every Sunday to water the plants.","ideaStatus":"open","createdAt":"2026-10-01T08:00:00Z"},
            {"id":"i3","ideaKind":"catalog","ideaCategory":"money","emoji":"💳","headline":"Watch for unusual charges","body":"Tell me when a charge is far above what a merchant usually takes.","openingRequest":"Watch my card for unusual charges.","ideaStatus":"open","createdAt":"2026-10-01T08:00:00Z"},
            {"id":"i4","ideaKind":"catalog","ideaCategory":"mail","emoji":"📬","headline":"Unsubscribe from what I never read","ideaStatus":"done","startedConversationId":"demo-unsubscribe","createdAt":"2026-09-20T08:00:00Z","closedAt":"2026-09-21T08:00:00Z"}
            ],"ideaCategories":[{"ideaCategory":"home","emojis":["🪴"]},{"ideaCategory":"money","emojis":["💳"]},{"ideaCategory":"mail","emojis":["📬"]}]}}}
            """#

        /// The built-in categories, as a new server has them.
        static let spendingCategories = #"""
            {"data":{"SpendingCategories":[
            {"id":"income","spendingCategoryName":"income","parentSpendingCategoryId":null,"isIncome":true,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"housing","spendingCategoryName":"housing","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"groceries","spendingCategoryName":"groceries","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"dining","spendingCategoryName":"dining","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"shopping","spendingCategoryName":"shopping","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"transport","spendingCategoryName":"transport","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"utilities","spendingCategoryName":"utilities","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"health","spendingCategoryName":"health","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"entertainment","spendingCategoryName":"entertainment","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"subscriptions","spendingCategoryName":"subscriptions","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"gifts","spendingCategoryName":"gifts and donations","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":false},
            {"id":"transfer","spendingCategoryName":"transfer","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":true,"isOther":false},
            {"id":"other","spendingCategoryName":"other","parentSpendingCategoryId":null,"isIncome":false,"isHidden":false,"isTransfer":false,"isOther":true}
            ]}}
            """#

        /// An invented month of spending: ten categories, so the donut has
        /// eight slices of their own and the last two together.
        static let spendingSummary = #"""
            {"data":{"FinanceSpendingSummary":{"groupBy":"spendingCategory","reportingCurrencyCode":null,"convertedMoneyOut":null,"convertedMoneyIn":null,"unconvertedCurrencyCodes":[],"convertedSpendingSummaryRows":null,
            "spendingSummaryRows":[
            {"groupKey":"housing","groupLabel":"housing","currencyCode":"USD","moneyOut":"1850.00","moneyIn":"0","financeTransactionCount":1},
            {"groupKey":"groceries","groupLabel":"groceries","currencyCode":"USD","moneyOut":"431.60","moneyIn":"19.00","financeTransactionCount":14},
            {"groupKey":"dining","groupLabel":"dining","currencyCode":"USD","moneyOut":"236.40","moneyIn":"0","financeTransactionCount":9},
            {"groupKey":"shopping","groupLabel":"shopping","currencyCode":"USD","moneyOut":"189.99","moneyIn":"0","financeTransactionCount":5},
            {"groupKey":"transport","groupLabel":"transport","currencyCode":"USD","moneyOut":"142.30","moneyIn":"0","financeTransactionCount":11},
            {"groupKey":"utilities","groupLabel":"utilities","currencyCode":"USD","moneyOut":"128.75","moneyIn":"0","financeTransactionCount":3},
            {"groupKey":"health","groupLabel":"health","currencyCode":"USD","moneyOut":"96.00","moneyIn":"0","financeTransactionCount":2},
            {"groupKey":"entertainment","groupLabel":"entertainment","currencyCode":"USD","moneyOut":"64.50","moneyIn":"0","financeTransactionCount":4},
            {"groupKey":"subscriptions","groupLabel":"subscriptions","currencyCode":"USD","moneyOut":"38.97","moneyIn":"0","financeTransactionCount":3},
            {"groupKey":"gifts","groupLabel":"gifts and donations","currencyCode":"USD","moneyOut":"25.00","moneyIn":"0","financeTransactionCount":1},
            {"groupKey":"income","groupLabel":"income","currencyCode":"USD","moneyOut":"0","moneyIn":"6400.00","financeTransactionCount":2}
            ]}}}
            """#

        /// An invented year of income and spending, ending this month,
        /// which is only partly through.
        static func cashFlow() -> String {
            var calendar = Calendar(identifier: .gregorian)
            calendar.timeZone = TimeZone(identifier: "UTC") ?? .current
            let incomeAmounts = [6400, 6400, 6400, 6400, 6400, 8900, 6400, 6400, 6650, 6400, 6400, 3200]
            let spendingAmounts = [4820, 5110, 4390, 6240, 4675, 5030, 4510, 5880, 4720, 4955, 5310, 3204]
            let months = (0..<12).map { index -> String in
                let date = calendar.date(byAdding: .month, value: index - 11, to: Date()) ?? Date()
                let parts = calendar.dateComponents([.year, .month], from: date)
                let cashFlowMonth = String(format: "%04d-%02d", parts.year ?? 2026, parts.month ?? 1)
                let incomeAmount = incomeAmounts[index]
                let spendingAmount = spendingAmounts[index]
                return #"{"cashFlowMonth":"\#(cashFlowMonth)","incomeAmount":"\#(incomeAmount).00","spendingAmount":"\#(spendingAmount).00","netAmount":"\#(incomeAmount - spendingAmount).00"}"#
            }
            return #"{"data":{"CashFlow":{"reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"cashFlowMonths":["# + months.joined(separator: ",") + "]}}}"
        }

        /// An invented net worth for each day asked for: a slow climb with
        /// the ups and downs of a market.
        static func netWorth(answering body: Data?) -> String {
            var calendar = Calendar(identifier: .gregorian)
            calendar.timeZone = TimeZone(identifier: "UTC") ?? .current
            let today = calendar.startOfDay(for: Date())
            var from = calendar.date(byAdding: .day, value: -90, to: today) ?? today
            if let body, let request = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
                let variables = request["variables"] as? [String: Any], let text = variables["from"] as? String
            {
                let parts = text.split(separator: "-").compactMap { Int($0) }
                if parts.count == 3, let date = calendar.date(from: DateComponents(year: parts[0], month: parts[1], day: parts[2])) { from = date }
            }
            // At most a year of days, as the server keeps them.
            from = max(from, calendar.date(byAdding: .day, value: -365, to: today) ?? from)
            var points: [String] = []
            var day = from
            while day <= today {
                let dayNumber = Double(calendar.dateComponents([.day], from: from, to: day).day ?? 0)
                let daysBeforeToday = Double(calendar.dateComponents([.day], from: day, to: today).day ?? 0)
                let amount = 184_000 - daysBeforeToday * 62 + sin(dayNumber / 9) * 2_400 + sin(dayNumber / 2.3) * 650
                let parts = calendar.dateComponents([.year, .month, .day], from: day)
                let netWorthOn = String(format: "%04d-%02d-%02d", parts.year ?? 2026, parts.month ?? 1, parts.day ?? 1)
                points.append(#"{"netWorthOn":"\#(netWorthOn)","netWorthAmount":"\#(String(format: "%.2f", amount))"}"#)
                day = calendar.date(byAdding: .day, value: 1, to: day) ?? today.addingTimeInterval(86_400)
            }
            return #"{"data":{"NetWorth":{"reportingCurrencyCode":"USD","unconvertedCurrencyCodes":[],"convertedNetWorthPoints":["# + points.joined(separator: ",") + "]}}}"
        }

        /// The tab a screenshot opens on: `-TeaNodeTab calendar`.
        static var tab: String? {
            let arguments = ProcessInfo.processInfo.arguments
            guard let index = arguments.firstIndex(of: "-TeaNodeTab"), index + 1 < arguments.count else { return nil }
            return arguments[index + 1]
        }

        /// The screen Settings opens for a screenshot: `-TeaNodeScreen people`.
        static var screen: String? {
            let arguments = ProcessInfo.processInfo.arguments
            guard let index = arguments.firstIndex(of: "-TeaNodeScreen"), index + 1 < arguments.count else { return nil }
            return arguments[index + 1]
        }

        /// Invented events for the window asked for, the same for a day
        /// however often it is asked, so the month view finds a believable
        /// month wherever it scrolls: a few things most days, some busy days,
        /// weekly classes, a trip over three days each month, and today's
        /// plumber and lunch from the demo conversation.
        static func events(answering body: Data?) -> String {
            let local = Calendar.current
            let today = local.startOfDay(for: Date())
            var from = today
            var until = local.date(byAdding: .day, value: 7, to: today) ?? today
            if let body, let request = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
                let variables = request["variables"] as? [String: Any]
            {
                if let text = variables["from"] as? String, let date = ServerTime.date(text) { from = date }
                if let text = variables["until"] as? String, let date = ServerTime.date(text) { until = date }
            }
            var rows: [String] = []
            func timed(_ id: String, _ summary: String, _ location: String?, _ day: Date, _ startHour: Double, _ endHour: Double, isRecurring: Bool = false) {
                let location = location.map { #""\#($0)""# } ?? "null"
                let startsAt = CalendarDates.text(day.addingTimeInterval(startHour * 3600))
                let endsAt = CalendarDates.text(day.addingTimeInterval(endHour * 3600))
                rows.append(
                    #"{"id":"\#(id)","calendarId":"home","summary":"\#(summary)","location":\#(location),"startsAt":"\#(startsAt)","endsAt":"\#(endsAt)","allDay":false,"recurring":\#(isRecurring),"occurrence":\#(isRecurring),"status":"confirmed"}"#
                )
            }
            func allDay(_ id: String, _ summary: String, _ first: Date, dayCount: Int) {
                let firstDay = local.dateComponents([.year, .month, .day], from: first)
                var endDay = firstDay
                for _ in 0..<dayCount { endDay = CalendarDates.dayAfter(endDay) }
                rows.append(
                    #"{"id":"\#(id)","calendarId":"home","summary":"\#(summary)","location":null,"startsAt":"\#(CalendarDates.allDayText(firstDay))","endsAt":"\#(CalendarDates.allDayText(endDay))","allDay":true,"recurring":false,"occurrence":false,"status":"confirmed"}"#
                )
            }
            let errands: [(String, String?, Double, Double)] = [
                ("Dentist", "Main Street clinic", 10, 11), ("Haircut", nil, 17.5, 18.25),
                ("Piano lesson", "Music school", 16, 17), ("Call the bank", nil, 11, 11.5),
                ("Dinner with friends", "The corner bistro", 19, 21), ("Car service", "Garage", 8.5, 9.5),
                ("Parent evening", "School", 18, 19.5), ("Farmers market", "Town square", 9, 11),
                ("Movie night", nil, 20, 22), ("Pick up the bike", "Bike shop", 12, 12.5),
                ("Coffee with a neighbor", nil, 8, 9), ("Vet for the cat", "Animal clinic", 15, 15.5),
                ("Team review", "Office", 14, 15), ("Planning session", "Office", 10, 12),
            ]
            var day = local.startOfDay(for: from)
            while day < until {
                let dayNumber = local.ordinality(of: .day, in: .era, for: day) ?? 0
                let weekday = local.component(.weekday, from: day)
                let dayOfMonth = local.component(.day, from: day)
                // The same few numbers for the same day, wherever it is
                // asked, mixed (SplitMix64) so neighboring days differ.
                var mixed = UInt64(truncatingIfNeeded: dayNumber) &+ 0x9E37_79B9_7F4A_7C15
                mixed = (mixed ^ (mixed >> 30)) &* 0xBF58_476D_1CE4_E5B9
                mixed = (mixed ^ (mixed >> 27)) &* 0x94D0_49BB_1331_11EB
                let seed = mixed ^ (mixed >> 31)
                if local.isDate(day, inSameDayAs: today) {
                    allDay("garden-\(dayNumber)", "Garden day", day, dayCount: 1)
                    timed("plumber", "Plumber", "Home", day, 9, 10)
                    timed("lunch", "Lunch with Sam", "The noodle place", day, 13.5, 14.5)
                } else {
                    if weekday == 3 { timed("yoga", "Yoga", "Studio", day, 18, 19, isRecurring: true) }
                    if weekday == 5, dayNumber / 7 % 2 == 0 { timed("book-club", "Book club", nil, day, 19, 21, isRecurring: true) }
                    if weekday == 7 { timed("football", "Football practice", "Park", day, 10, 11.5, isRecurring: true) }
                    // Most days one or two things, now and then a busy one.
                    let errandCount = [0, 1, 1, 0, 2, 1, 0, 1, 3, 2, 1, 4][Int(seed % 12)]
                    for index in 0..<errandCount {
                        let errand = errands[Int((seed / 12 + UInt64(index) * 5) % UInt64(errands.count))]
                        timed("e\(dayNumber)-\(index)", errand.0, errand.1, day, errand.2, errand.3)
                    }
                }
                if dayOfMonth == 17 { allDay("trip-\(dayNumber)", "Trip to the coast", day, dayCount: 3) }
                if dayOfMonth == 1 { allDay("rent-\(dayNumber)", "Pay the rent", day, dayCount: 1) }
                day = local.date(byAdding: .day, value: 1, to: day) ?? until
            }
            // A trip that began before the window still shows on its days.
            for back in 1...2 {
                if let start = local.date(byAdding: .day, value: -back, to: local.startOfDay(for: from)), local.component(.day, from: start) == 17 {
                    allDay("trip-\(local.ordinality(of: .day, in: .era, for: start) ?? 0)", "Trip to the coast", start, dayCount: 3)
                }
            }
            return #"{"data":{"ListCalendarEvents":["# + rows.joined(separator: ",") + "]}}"
        }

        static let mailboxes = #"""
            {"data":{"ListMailboxes":[{"mailbox":{"id":"box","name":"Robin","signatureText":"","signatureHtml":"",
              "addresses":[{"address":"robin@example.com","domain":"example.com","domainId":"d"}],"autoReply":null,
              "agent":{"granted":true,"draftReplies":true,"triage":{"enabled":true}},
              "rules":[
                {"name":"Receipts to the archive","enabled":true,"stop":false,
                  "conditions":[{"field":"category","header":null,"operator":"equals","value":"receipt"}],
                  "actions":[{"kind":"markRead","folderId":null,"address":null},{"kind":"move","folderId":"archive","address":null}]},
                {"name":"The plumber first","enabled":true,"stop":true,
                  "conditions":[{"field":"from","header":null,"operator":"equals","value":"plumber@example.com"}],
                  "actions":[{"kind":"flag","folderId":null,"address":null}]},
                {"name":"Club newsletter","enabled":false,"stop":false,
                  "conditions":[{"field":"header","header":"List-Id","operator":"contains","value":"news.example.org"}],
                  "actions":[{"kind":"move","folderId":"archive","address":null}]}]},
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
                "todos":[
                  {"id":"t1","text":"Read the plumber's message","doneAt":"2026-10-10T09:00:03Z"},
                  {"id":"t2","text":"Reply that Thursday at 9 works","doneAt":null},
                  {"id":"t3","text":"Put the visit on the calendar","doneAt":null}
                ],
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

        /// Every GetSession the app sends, answered at once: who is signed
        /// in, and an operator holding every permission the Manage pages
        /// check.
        static let session = #"""
            {"data":{"GetSession":{"username":"robin","userId":"robin","manages":true,
              "permissions":{"everywhere":["mail:audit","queue:manage","report:read","domain:manage","user:manage","group:manage","role:manage","audit:read","server:manage","agent:audit","agent:act"],"byDomain":[]},
              "impersonatorUsername":null}}}
            """#

        /// Every ReadAgent the app sends, answered at once: the agent's
        /// settings and its upkeep, with no name, so the app still calls it
        /// "Your agent", and no budget.
        static let agent = #"""
            {"data":{"ReadAgent":{
              "agent":{"id":"agent","name":"","enabled":true,"instructions":"Keep replies short. Ask before booking anything that costs money.",
                "language":"","knowledgeLanguage":"","askModel":"","speechVoice":"","operatorDisabledAt":null,
                "confirm":["mail_send","calendar"],"unattendedAllowedRisks":["outward"],"isMemoryCheckEnabled":true,"isIdeasEnabled":true,
                "voice":{"tone":"casual","length":"short","greeting":"Hi","signoff":"Robin"},
                "categories":[{"name":"home","description":"The house, the garden and the people who fix things."},{"name":"travel","description":"Tickets, bookings and plans for trips."}],
                "dreamFrom":"01:00","dreamUntil":"06:00","dreamBootstrap":false,"isAlertsEnabled":true,"alertQuietStart":"22:00","alertQuietEnd":"07:00","alertDailyMost":5},
              "sources":[{"mailboxId":"box","name":"Robin","addresses":["robin@example.com"],"policy":{"granted":true,"draftReplies":true,"search":true,"research":false,
                "triage":{"enabled":true,"backfill":"recent","replyExpectation":"direct"},"summaries":{"enabled":true,"minimumMessages":3,"style":"brief"},
                "autoReply":null,"alerts":{"enabled":true}}}],
              "collections":[{"id":"home","name":"Home","kind":"calendar","granted":true,"items":4},{"id":"book","name":"Contacts","kind":"addressBook","granted":true,"items":4}],
              "allowed":{"enabled":true,"triage":true,"summaries":true,"draftReplies":true,"search":true,"research":true,"autoReply":true,"ask":true,"schedules":true,"browser":true,"connectedServers":true},
              "budget":null,"choices":["standard","thorough"],"timezone":"UTC","language":"en","knowledgeLanguage":"en",
              "categories":["personal","work","newsletter","notification","receipt","promotion","social","invitation","phishing","junk","other","home","travel"]}}}
            """#

        static let dreams = #"""
            {"data":{"ListAgentDreams":[
              {"id":"dream3","jobId":"job3","startedAt":"2026-10-10T02:10:00Z","finishedAt":"2026-10-10T02:31:00Z","digested":46,"filed":38,"merged":3,"rewritten":5,"moved":1,"dormant":2,"embedded":52,"backlog":340,"coarse":false,"strengthened":12,"associated":9,"rehearsed":12,"gaps":2,"unknown":1,"revised":4,"overviewsWritten":1,"themesMade":1,"themesUpdated":2,"reflectionsWritten":1,"lastError":null,"cost":0.41,"currency":"USD"},
              {"id":"dream2","jobId":"job2","startedAt":"2026-10-09T01:45:00Z","finishedAt":"2026-10-09T02:02:00Z","digested":31,"filed":27,"merged":1,"rewritten":3,"moved":0,"dormant":0,"embedded":35,"backlog":386,"coarse":false,"strengthened":8,"associated":6,"rehearsed":10,"gaps":1,"unknown":0,"revised":2,"overviewsWritten":0,"themesMade":0,"themesUpdated":1,"reflectionsWritten":1,"lastError":null,"cost":0.33,"currency":"USD"},
              {"id":"dream1","jobId":"job1","startedAt":"2026-10-08T03:05:00Z","finishedAt":"2026-10-08T03:19:00Z","digested":24,"filed":20,"merged":2,"rewritten":1,"moved":2,"dormant":1,"embedded":26,"backlog":417,"coarse":false,"strengthened":5,"associated":4,"rehearsed":0,"gaps":0,"unknown":0,"revised":1,"overviewsWritten":1,"themesMade":0,"themesUpdated":0,"reflectionsWritten":0,"lastError":null,"cost":0.27,"currency":"USD"}
            ]}}
            """#

        static let schedules = #"""
            {"data":{"ListAgentSchedules":[
              {"id":"brief","name":"Daily brief","cron":"30 7 * * 1-5","prompt":"What is on today, what arrived overnight that needs me, and the weather for the walk to the station.","deliver":"drawer","enabled":true,"lastRunAt":"2026-10-09T07:30:00Z","nextRunAt":"2026-10-12T07:30:00Z"},
              {"id":"roundup","name":"Friday round-up","cron":"0 17 * * 5","prompt":"Sum up the week: what I answered, what still waits, and what is on next week.","deliver":"mail","enabled":true,"lastRunAt":"2026-10-09T17:00:00Z","nextRunAt":"2026-10-16T17:00:00Z"},
              {"id":"plants","name":"Water the plants","cron":"0 9 * * 0","prompt":"Remind me to water the plants, and say which ones were watered last week.","deliver":"drawer","enabled":false,"lastRunAt":"2026-09-27T09:00:00Z","nextRunAt":null}
            ]}}
            """#

        static let sessions = #"""
            {"data":{"ListSessions":[
              {"id":"session1","current":true,"created":"2026-10-01T08:12:00Z","expires":"2026-10-31T08:12:00Z","lastUsed":"2026-10-10T09:04:00Z","ip":"198.51.100.23","userAgent":"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15","revoked":null,"impersonatorUsername":null},
              {"id":"session2","current":false,"created":"2026-09-28T19:30:00Z","expires":"2026-10-28T19:30:00Z","lastUsed":"2026-10-09T18:40:00Z","ip":"203.0.113.54","userAgent":"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:140.0) Gecko/20100101 Firefox/140.0","revoked":null,"impersonatorUsername":null},
              {"id":"session3","current":false,"created":"2026-09-20T12:05:00Z","expires":"2026-10-20T12:05:00Z","lastUsed":"2026-10-03T16:15:00Z","ip":"192.0.2.77","userAgent":"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36","revoked":null,"impersonatorUsername":null}
            ]}}
            """#

        static let phones = #"""
            {"data":{"ListAgentPhones":{"phones":[
              {"id":"phone1","phoneName":"Robin's phone","phoneModel":"iPhone","notificationKinds":["message","approval","question","answer"],"canBeNotified":true,"lastSeenAt":"2026-10-10T09:05:00Z","lastDeliveredAt":"2026-10-10T09:05:02Z","lastDeliveryError":""},
              {"id":"phone2","phoneName":"Kitchen tablet","phoneModel":"iPad","notificationKinds":["approval","question"],"canBeNotified":true,"lastSeenAt":"2026-10-09T20:10:00Z","lastDeliveredAt":"2026-10-09T20:11:00Z","lastDeliveryError":""},
              {"id":"phone3","phoneName":"Old phone","phoneModel":"iPhone","notificationKinds":["message","approval","question","answer"],"canBeNotified":false,"lastSeenAt":"2026-06-20T08:00:00Z","lastDeliveredAt":null,"lastDeliveryError":""}
              ],"isPushEnabled":true,"notificationKinds":["message","approval","question","answer"]}}}
            """#

        static let people = #"""
            {"data":{"ListUsers":[
              {"id":"robin","username":"robin","name":"Robin","email":"robin@example.com","disabledAt":null,"hasPassword":true,"groupIds":["members","operators"],"createdAt":"2026-03-02T10:00:00Z"},
              {"id":"sam","username":"sam","name":"Sam","email":"sam@example.org","disabledAt":null,"hasPassword":true,"groupIds":["members"],"createdAt":"2026-03-09T14:20:00Z"},
              {"id":"jordan","username":"jordan","name":"Jordan","email":"jordan@example.net","disabledAt":null,"hasPassword":false,"groupIds":["members"],"createdAt":"2026-05-17T09:45:00Z"},
              {"id":"frontdesk","username":"frontdesk","name":"Front desk","email":"frontdesk@example.com","disabledAt":null,"hasPassword":true,"groupIds":["members"],"createdAt":"2026-06-01T08:00:00Z"},
              {"id":"casey","username":"casey","name":null,"email":null,"disabledAt":"2026-09-14T08:00:00Z","hasPassword":true,"groupIds":[],"createdAt":"2026-04-11T11:30:00Z"}
            ]}}
            """#

        static let groups = #"""
            {"data":{"ListGroups":[
              {"id":"members","name":"Members","description":"Everyone who reads mail here.","idpGroup":null,"userIds":["robin","sam","jordan","frontdesk"],"roleIds":["member"],"domainIds":["d","d2"]},
              {"id":"operators","name":"Operators","description":"Run the server.","idpGroup":null,"userIds":["robin"],"roleIds":["operator"],"domainIds":["d","d2"]}
            ]}}
            """#

        static let roles = #"""
            {"data":{"ListRoles":[
              {"id":"member","name":"Member","description":"Reads and sends the mail of the mailboxes they are given.","permissions":["mail:read"]},
              {"id":"operator","name":"Operator","description":"Everything on the server.","permissions":["mail:audit","queue:manage","report:read","domain:manage","user:manage","group:manage","role:manage","audit:read","server:manage","agent:audit","agent:act"]}
            ]}}
            """#

        static let serverSettings = #"""
            {"data":{"GetSettings":{
              "s3":{"enabled":false,"bucket":"","region":"","endpoint":null,"pathStyle":false,"accessKeyId":null,"hasSecretAccessKey":false,"credentialsFile":null},
              "route53":{"enabled":false,"zoneId":"","region":"","accessKeyId":null,"hasSecretAccessKey":false,"credentialsFile":null},
              "antivirus":{"enabled":false,"host":"","port":3310},
              "antispam":{"enabled":true,"engine":"builtin","effectiveEngine":"builtin","host":"","port":783,"signalsEnabled":true,"dnsEnabled":true,"bayesEnabled":true,"rulesEnabled":true,"bayesMinimumMessages":200,"bayesLearnedSpam":412,"bayesLearnedHam":1380},
              "relay":{"enabled":false,"host":"","port":587,"security":"starttls","username":null,"hasPassword":false},
              "proxy":{"socks5":null},
              "certificates":{"perDomain":true,"hosts":[],"acmeEnabled":true,"acmeEmail":"postmaster@example.com","acmeDirectoryUrl":null,"acmeChallenge":"http-01","certificateFile":null,"privateKeyFile":null},
              "smtp":{"maxMessageSize":"25MB","maxRecipientsIncoming":100,"maxRecipientsOutgoing":50,"greylistDelay":"5m","authRateLimit":10,"authRateBurst":20,"trustedSenders":[]},
              "resolver":{"nameserver":"","checkInterval":"1h","externalAddressServices":[]},
              "session":{"lifetime":"720h"},
              "passkey":{"enabled":true,"relyingPartyId":"demo.example.com","displayName":"TeaNode","origins":["https://demo.example.com"],"maximumPerUser":10},
              "listen":{"smtpIncoming":":25","smtpOutgoing":":587","imap":":143","imaps":":993","http":":80","https":":443","debug":null},
              "sso":{"providers":[]},
              "identity":{"name":"demo.example.com","mailServers":["mx.example.com"],"externalAddresses":["203.0.113.10"],"logLevel":"info","dataDirectory":"/var/lib/teanode"},
              "storage":{"directory":"/var/lib/teanode/messages","spoolRetention":"168h"},
              "geoip":{"enabled":false,"databaseFile":null}}}}
            """#
    }
#endif
