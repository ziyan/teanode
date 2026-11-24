#if DEBUG
    import Foundation
    import TeaNodeKit
    import UIKit

    // The invented cards that the screenshots of the Feed tab photograph
    // (`-TeaNodeDemo -TeaNodeTab feed -TeaNodeScreen feed`). Every card is made
    // up. Changes are answered with the card as it would then be, but nothing
    // is kept: each read starts from these.

    extension DemoServer {
        private static func feedCard(
            _ feedItemId: String, kind: String, sourceKind: String, sourceId: String = "", sourceTitle: String = "", title: String,
            body: String, pictureSymbol: String, facts: [(String, String)] = [], links: [(String, String)] = [],
            choices: [(String, String, String)] = [], urgency: String = "whenever", isLiked: Bool = false, isRead: Bool = false,
            daysAgo: Int = 0, hour: Int, minute: Int, stackKey: String = "", stackCount: Int = 1, stackUnread: Int = 0,
            stackTitle: String = "", stackPosition: Int? = nil, layout: String = "text", layoutContent: Any? = nil,
            media: [[String: Any]] = []
        ) -> [String: Any] {
            let postedAt = DemoServer.demoTime(daysAgo: daysAgo, hour: hour, minute: minute)
            return [
                "feedItemId": feedItemId, "feedItemKind": kind, "feedSourceKind": sourceKind, "feedSourceId": sourceId,
                "feedSourceTitle": sourceTitle, "runConversationId": "run-\(feedItemId)", "title": title, "body": body,
                "pictureSymbol": pictureSymbol,
                "factRows": facts.map { ["factLabel": $0.0, "factValue": $0.1] },
                "sourceLinks": links.map { ["sourceTitle": $0.0, "sourceUrl": $0.1] },
                "choices": choices.map { ["choiceKey": $0.0, "choiceLabel": $0.1, "choiceRole": $0.2] },
                "chosenChoiceKey": NSNull(), "chosenAt": NSNull(), "outcomeText": "", "isOutcomePending": false,
                "feedUrgency": urgency, "isLiked": isLiked, "isRead": isRead, "isDismissed": false, "postedAt": postedAt,
                "updatedAt": postedAt, "feedStackKey": stackKey, "stackItemCount": stackCount, "stackUnreadCount": stackUnread,
                "feedStackTitle": stackTitle, "stackPosition": stackPosition.map { $0 as Any } ?? NSNull(),
                "feedCardLayout": layout, "layoutContent": layoutContent.map { feedJSON($0) } ?? "", "feedMedia": media,
            ]
        }

        /// One picture or video of a card. A picture of a page is served by this
        /// demo (`feedMediaPicture`), a photo or video by the demo photo library.
        private static func feedMedia(
            _ feedItemId: String, _ mediaKey: String, kind: String = "picture", photoId: String = "", linkUrl: String = "", siteName: String = "",
            caption: String = "", widthPixels: Int = 1200, heightPixels: Int = 630, row: Int? = nil
        ) -> [String: Any] {
            let imageUrl = photoId.isEmpty ? "/api/v1/feed/items/\(feedItemId)/media/\(mediaKey)" : "/api/v1/photos/\(photoId)/previews/large"
            return [
                "mediaKey": mediaKey, "feedMediaKind": kind, "imageUrl": imageUrl,
                "videoUrl": kind == "video" ? "/api/v1/photos/\(photoId)/previews/video" : "", "linkUrl": linkUrl, "siteName": siteName,
                "mediaCaption": caption, "widthPixels": widthPixels, "heightPixels": heightPixels, "listRowIndex": row.map { $0 as Any } ?? NSNull(),
            ]
        }

        /// The card the media screenshots scroll to, and the top card of the
        /// brief the open-stack screenshot scrolls to.
        static let demoMediaTopId = "feed-picture"
        static let demoBriefTopId = "brief-0"

        /// A picture for `/api/v1/feed/items/<card>/media/<key>`: a sky, hills and a
        /// building, its colors from the key so each picture differs.
        static func feedMediaPicture(for request: URLRequest) -> Data? {
            guard let path = request.url?.path, path.hasPrefix("/api/v1/feed/items/") else { return nil }
            let hue = CGFloat(path.utf8.reduce(0) { ($0 &* 31 &+ Int($1)) % 360 }) / 360
            let size = CGSize(width: 800, height: 450)
            let format = UIGraphicsImageRendererFormat()
            format.scale = 1
            return UIGraphicsImageRenderer(size: size, format: format).pngData { context in
                let drawing = context.cgContext
                drawing.setFillColor(UIColor(hue: hue, saturation: 0.3, brightness: 0.95, alpha: 1).cgColor)
                drawing.fill(CGRect(origin: .zero, size: size))
                drawing.setFillColor(UIColor(hue: hue, saturation: 0.5, brightness: 0.75, alpha: 1).cgColor)
                drawing.fillEllipse(in: CGRect(x: -100, y: 300, width: 600, height: 400))
                drawing.setFillColor(UIColor(hue: (hue + 0.1).truncatingRemainder(dividingBy: 1), saturation: 0.55, brightness: 0.6, alpha: 1).cgColor)
                drawing.fillEllipse(in: CGRect(x: 350, y: 330, width: 650, height: 400))
                drawing.setFillColor(UIColor(white: 1, alpha: 0.85).cgColor)
                drawing.fillEllipse(in: CGRect(x: 580, y: 50, width: 90, height: 90))
                drawing.setFillColor(UIColor(hue: hue, saturation: 0.6, brightness: 0.4, alpha: 1).cgColor)
                drawing.fill(CGRect(x: 200, y: 150, width: 120, height: 190))
                drawing.fill(CGRect(x: 330, y: 200, width: 90, height: 140))
            }
        }

        /// The weather card of the feed itself: a rainy evening, on its own sky.
        private static var rainWeather: [String: Any] {
            func hour(_ hour: Int, _ temperature: Int, _ symbol: String, _ rain: Int) -> [String: Any] {
                [
                    "forecastHour": DemoServer.demoTime(daysAgo: 0, hour: hour, minute: 0), "forecastTemperature": temperature, "forecastSymbol": symbol,
                    "precipitationChancePercent": rain,
                ]
            }
            return [
                "placeName": "Riverside", "currentTemperature": 14, "temperatureUnit": "C", "conditionText": "Light rain", "conditionSymbol": "cloud.rain",
                "highTemperature": 16, "lowTemperature": 9,
                "hourlyForecast": [
                    hour(17, 14, "cloud", 30), hour(18, 13, "cloud.drizzle", 50), hour(19, 12, "cloud.rain", 70), hour(20, 12, "cloud.rain", 80),
                    hour(21, 11, "cloud.rain", 60), hour(22, 10, "cloud", 20),
                ],
            ]
        }

        /// The invented feed, newest first.
        private static var feedCards: [[String: Any]] {
            [
                feedCard(
                    "feed-signin", kind: "decision", sourceKind: "alert", sourceId: "alert-signin", title: "New sign-in on a laptop",
                    body: "A laptop signed in to your account at 06:12 from a city you have not used before.", pictureSymbol: "lock.shield",
                    choices: [("mine", "That's me", "dismiss"), ("secure", "Secure my account", "action")], urgency: "now", hour: 6, minute: 13),
                feedCard(
                    "feed-coding", kind: "decision", sourceKind: "coding_session", sourceId: "laptop/p2", title: "Claude Code on laptop asks",
                    body: "Which style should the new footer link use?", pictureSymbol: "chevron.left.forwardslash.chevron.right",
                    choices: [("option:1", "Plain text link", "action"), ("option:2", "Underlined link", "action")], urgency: "now", hour: 6,
                    minute: 5),
                feedCard(
                    "feed-mail", kind: "decision", sourceKind: "schedule", sourceId: "schedule-mail", sourceTitle: "Weekly mail tidy-up",
                    title: "Tidy up your mail this week?",
                    body: "I found **42 newsletters** you have not opened in a month and 9 receipts that belong in Finance. I can file them for you.",
                    pictureSymbol: "tray.full", choices: [("approve", "Approve", "action"), ("discuss", "Discuss", "discuss"), ("later", "Not now", "dismiss")],
                    hour: 6, minute: 0),
                feedCard(
                    "feed-weather", kind: "update", sourceKind: "schedule", sourceId: "schedule-weather", sourceTitle: "Morning weather",
                    title: "Rain moves in this evening", body: "Light rain from about 18:00, clearing after 22:00. [Hourly forecast](https://weather.example.com/town)",
                    pictureSymbol: "cloud.rain", facts: [("High", "16 degrees"), ("Low", "9 degrees"), ("Rain", "80 percent")],
                    links: [("Town forecast", "https://weather.example.com/town")], isLiked: true, hour: 7, minute: 0, layout: "weather",
                    layoutContent: rainWeather),
                feedCard(
                    "feed-picture", kind: "news", sourceKind: "goal", sourceId: "goal-maps", sourceTitle: "Maps and navigation",
                    title: "Open-source maps add indoor floors",
                    body: "Stations and malls can now publish floor plans that any map app can show. [Read the report](https://news.example.com/maps)",
                    pictureSymbol: "map", links: [("Maps Daily", "https://news.example.com/maps")], hour: 6, minute: 50,
                    media: [
                        feedMedia(
                            "feed-picture", "m1", linkUrl: "https://news.example.com/maps", siteName: "Maps Daily",
                            caption: "A station's floors, drawn from the new data")
                    ]),
                feedCard(
                    "feed-video", kind: "update", sourceKind: "schedule", sourceId: "schedule-memories", sourceTitle: "Weekly memories",
                    title: "A short clip from the harbor trip", body: "From last Thursday: the boats coming in at the market.",
                    pictureSymbol: "video", hour: 6, minute: 40,
                    media: [
                        feedMedia("feed-video", "m1", kind: "video", photoId: "demo-03", caption: "23 seconds", widthPixels: 4032, heightPixels: 3024)
                    ]),
                feedCard(
                    "feed-list", kind: "news", sourceKind: "goal", sourceId: "goal-chips", sourceTitle: "Chips and hardware",
                    title: "Three hardware stories", body: "- Chip doubles speech speed\n- Battery swaps\n- Smaller sensors", pictureSymbol: "cpu",
                    hour: 6, minute: 30, layout: "list",
                    layoutContent: [
                        "listRows": [
                            ["rowTitle": "A new chip doubles on-device speech speed", "rowMeta": "Chip Weekly", "rowSymbol": "newspaper", "rowUrl": "https://news.example.com/chip"],
                            ["rowTitle": "Battery swaps replace charging breaks", "rowMeta": "Example News", "rowSymbol": "newspaper", "rowUrl": "https://news.example.com/batteries"],
                            ["rowTitle": "Smaller sensors see in the dark", "rowMeta": "Sensor Notes", "rowSymbol": "newspaper", "rowUrl": "https://news.example.com/sensors"],
                        ]
                    ],
                    // The middle story has no picture, as a story often does.
                    media: [
                        feedMedia("feed-list", "r0", siteName: "Chip Weekly", row: 0), feedMedia("feed-list", "r2", siteName: "Sensor Notes", row: 2),
                    ]),
                // The morning brief: its first card tops the stack of five.
                briefCards[0].merging(["stackItemCount": briefCards.count, "stackUnreadCount": 4] as [String: Any]) { _, stacked in stacked },
                // The newest of three news cards from one goal: the list holds the stack by its top card.
                newsCards[0].merging(["stackItemCount": newsCards.count, "stackUnreadCount": 2] as [String: Any]) { _, stacked in stacked },
                feedCard(
                    "feed-report", kind: "report", sourceKind: "schedule", sourceId: "schedule-spending", sourceTitle: "Weekly spending report",
                    title: "Spending is 8 percent under last week",
                    body: "Groceries went up a little; eating out went down. Nothing unusual.", pictureSymbol: "chart.line.uptrend.xyaxis",
                    facts: [("This week", "$412"), ("Last week", "$448"), ("Biggest category", "Groceries")], isRead: true, daysAgo: 2,
                    hour: 9, minute: 0),
            ]
        }

        /// The key of the stack of news cards, and its three cards, newest first.
        static let demoNewsStackKey = "goal:goal-robots"

        private static var newsCards: [[String: Any]] {
            [
                feedCard(
                    "feed-news", kind: "news", sourceKind: "goal", sourceId: "goal-robots", sourceTitle: "Robots in warehouses",
                    title: "A small robot maker opens a second factory",
                    body: "The company says the new site will double its output of shelf-moving robots by next year. [Read the report](https://news.example.com/robots)",
                    pictureSymbol: "newspaper", links: [("Example News", "https://news.example.com/robots")], daysAgo: 1, hour: 18, minute: 30,
                    stackKey: demoNewsStackKey),
                feedCard(
                    "feed-news-2", kind: "news", sourceKind: "goal", sourceId: "goal-robots", sourceTitle: "Robots in warehouses",
                    title: "Pickers and robots share one aisle in a new pilot",
                    body: "A grocery warehouse reports 15 percent faster orders after letting people and robots work the same aisle.",
                    pictureSymbol: "newspaper", daysAgo: 1, hour: 11, minute: 10, stackKey: demoNewsStackKey),
                feedCard(
                    "feed-news-3", kind: "news", sourceKind: "goal", sourceId: "goal-robots", sourceTitle: "Robots in warehouses",
                    title: "Battery swaps replace charging breaks",
                    body: "Fleets that swap batteries in under a minute stay on the floor around the clock.", pictureSymbol: "bolt.batteryblock",
                    isRead: true, daysAgo: 3, hour: 8, minute: 45, stackKey: demoNewsStackKey),
            ]
        }

        static let demoBriefStackKey = "brief:morning"
        static let demoBriefTitle = "Morning brief, Wed Nov 19"

        /// The brief's cards in the order the agent put them: a summary, the
        /// weather, tech stories, mentions and mail to read.
        private static var briefCards: [[String: Any]] {
            func brief(
                _ feedItemId: String, position: Int, title: String, body: String, symbol: String, facts: [(String, String)] = [],
                isRead: Bool = false, layout: String = "text", layoutContent: Any? = nil, media: [[String: Any]] = []
            ) -> [String: Any] {
                feedCard(
                    feedItemId, kind: "report", sourceKind: "schedule", sourceId: "schedule-brief", sourceTitle: "Morning brief", title: title,
                    body: body, pictureSymbol: symbol, facts: facts, isRead: isRead, hour: 7, minute: 30, stackKey: demoBriefStackKey,
                    stackTitle: demoBriefTitle, stackPosition: position, layout: layout, layoutContent: layoutContent, media: media)
            }
            func today(_ hour: Int, _ minute: Int) -> String { DemoServer.demoTime(daysAgo: 0, hour: hour, minute: minute) }
            func entry(_ startHour: Int, _ startMinute: Int, _ endHour: Int, _ endMinute: Int, _ title: String, _ kind: String, _ detail: String) -> [String: Any] {
                [
                    "startsAt": today(startHour, startMinute), "endsAt": today(endHour, endMinute), "agendaTitle": title, "agendaEntryKind": kind,
                    "agendaDetail": detail,
                ]
            }
            func row(_ title: String, detail: String = "", meta: String, symbol: String, url: String = "", mailId: String = "") -> [String: Any] {
                var built: [String: Any] = ["rowTitle": title, "rowDetail": detail, "rowMeta": meta, "rowSymbol": symbol, "rowUrl": url]
                if !mailId.isEmpty { built["rowRecordReference"] = ["itemId": mailId, "subject": title] }
                return built
            }
            func hour(_ hour: Int, _ temperature: Int, _ symbol: String, _ rain: Int) -> [String: Any] {
                ["forecastHour": today(hour, 0), "forecastTemperature": temperature, "forecastSymbol": symbol, "precipitationChancePercent": rain]
            }
            let weather: [String: Any] = [
                "placeName": "Riverside", "currentTemperature": 16, "temperatureUnit": "C", "conditionText": "Mostly sunny",
                "conditionSymbol": "cloud.sun", "highTemperature": 21, "lowTemperature": 11,
                "hourlyForecast": [
                    hour(8, 16, "sun.max", 0), hour(10, 18, "cloud.sun", 0), hour(12, 20, "sun.max", 0), hour(14, 21, "cloud.sun", 10),
                    hour(16, 20, "cloud", 20), hour(18, 18, "cloud.drizzle", 40), hour(20, 15, "cloud.rain", 60),
                ],
            ]
            let tech: [[String: Any]] = [
                row("A small robot maker opens a second factory", meta: "Example News", symbol: "newspaper", url: "https://news.example.com/robots"),
                row("Battery swaps replace charging breaks", meta: "Example News", symbol: "newspaper", url: "https://news.example.com/batteries"),
                row("A new chip doubles on-device speech speed", meta: "Chip Weekly", symbol: "newspaper", url: "https://news.example.com/chip"),
                row("Open-source maps add indoor floors", meta: "Maps Daily", symbol: "newspaper", url: "https://news.example.com/maps"),
                row("A browser syncs tab groups across phones", meta: "Browser Notes", symbol: "newspaper", url: "https://news.example.com/tabs"),
            ]
            let mentions: [[String: Any]] = [
                row("Sam in #trip", detail: "Can you confirm Friday works for the early train?", meta: "#trip, 6:48 AM", symbol: "at"),
                row("Priya in #design", detail: "The new header is in the review folder if you want a look.", meta: "#design, yesterday", symbol: "at"),
                row("Lee in #general", detail: "Lunch on Thursday?", meta: "#general, yesterday", symbol: "at"),
            ]
            let mail: [[String: Any]] = [
                row("Your lease renewal", detail: "The landlord sent the renewal terms.", meta: "Landlord, 6:20 AM", symbol: "envelope", mailId: "mail-demo-1"),
                row("School newsletter, November", detail: "Dates for the winter concert and a trip.", meta: "Riverside School, yesterday", symbol: "envelope", mailId: "mail-demo-2"),
            ]
            return [
                brief(
                    "brief-0", position: 0, title: "Today: 2 things need you",
                    body: "A sign-in to check and a form due on Friday. Stand-up at 9:00, focus time from 10:00, design review at 14:00.",
                    symbol: "sun.max", layout: "agenda",
                    layoutContent: [
                        "agendaEntries": [
                            entry(9, 0, 9, 30, "Team stand-up", "meeting", "Room 4"),
                            entry(10, 0, 12, 0, "Write the quarterly plan", "focus", "No meetings"),
                            entry(12, 30, 13, 30, "Lunch", "break", ""),
                            entry(14, 0, 15, 0, "Design review", "meeting", "With Priya"),
                            entry(15, 30, 16, 0, "Send the form due Friday", "task", ""),
                        ]
                    ]),
                brief(
                    "brief-1", position: 1, title: "Sunny and mild", body: "Highs near **21 degrees** with a light breeze.", symbol: "cloud.sun",
                    facts: [("High", "21 degrees"), ("Low", "11 degrees"), ("Rain", "10 percent")], layout: "weather", layoutContent: weather),
                brief(
                    "brief-2", position: 2, title: "Five tech stories",
                    body: "- [A small robot maker opens a second factory](https://news.example.com/robots)\n- [Battery swaps replace charging breaks](https://news.example.com/batteries)\n- [A new chip doubles on-device speech speed](https://news.example.com/chip)\n- [Open-source maps add indoor floors](https://news.example.com/maps)\n- [A browser syncs tab groups across phones](https://news.example.com/tabs)",
                    symbol: "newspaper", layout: "list", layoutContent: ["listRows": tech],
                    media: [
                        feedMedia("brief-2", "r0", siteName: "Example News", row: 0), feedMedia("brief-2", "r1", siteName: "Example News", row: 1),
                        feedMedia("brief-2", "r2", siteName: "Chip Weekly", row: 2), feedMedia("brief-2", "r3", siteName: "Maps Daily", row: 3),
                    ]),
                brief(
                    "brief-3", position: 3, title: "3 mentions since yesterday", body: "Sam asked about Friday, Priya shared a header, Lee asked about lunch.",
                    symbol: "at", layout: "list", layoutContent: ["listRows": mentions]),
                brief(
                    "brief-4", position: 4, title: "Mail worth reading", body: "A reply from the landlord and the school newsletter.", symbol: "envelope",
                    isRead: true, layout: "list", layoutContent: ["listRows": mail]),
            ]
        }

        private static func feedJSON(_ object: Any) -> String {
            let data = (try? JSONSerialization.data(withJSONObject: object)) ?? Data("null".utf8)
            return String(decoding: data, as: UTF8.self)
        }

        /// The feed's reads and changes; nil for any other query.
        static func feedAnswer(query: String, body: Data?) -> String? {
            guard query.contains("AgentFeed") || query.contains("AgentFeedItem") else { return nil }
            let variables = requestVariables(body)
            let feedItemId = variables["feedItemId"] as? String ?? ""
            var card = (feedCards + newsCards + briefCards).first { $0["feedItemId"] as? String == feedItemId } ?? feedCards[0]
            if query.contains("MarkAgentFeedRead") {
                return #"{"data":{"MarkAgentFeedRead":\#((variables["feedItemIds"] as? [String] ?? []).count)}}"#
            } else if query.contains("DiscussAgentFeedItem") {
                return #"{"data":{"DiscussAgentFeedItem":"conversation-main"}}"#
            } else if query.contains("ChooseAgentFeedItem") {
                let choiceKey = variables["choiceKey"] as? String ?? ""
                let choices = card["choices"] as? [[String: Any]] ?? []
                let role = choices.first { $0["choiceKey"] as? String == choiceKey }?["choiceRole"] as? String ?? "action"
                card["chosenChoiceKey"] = choiceKey
                card["chosenAt"] = DemoServer.demoTime(daysAgo: 0, hour: 8, minute: 0)
                card["isRead"] = true
                // An action starts a turn of the agent, whose line comes later.
                card["isOutcomePending"] = role == "action"
                card["isDismissed"] = role == "dismiss"
                return #"{"data":{"ChooseAgentFeedItem":\#(feedJSON(card))}}"#
            } else if query.contains("DismissAgentFeedItems") || query.contains("RestoreAgentFeedItems") {
                // The demo keeps no state: it answers with the ids it was given.
                let isDismissing = query.contains("DismissAgentFeedItems")
                var feedItemIds = variables["feedItemIds"] as? [String] ?? []
                if let feedStackKey = variables["feedStackKey"] as? String {
                    feedItemIds = (feedStackKey == demoBriefStackKey ? briefCards : (feedStackKey == demoNewsStackKey ? newsCards : []))
                        .compactMap { $0["feedItemId"] as? String }
                } else if variables["isEveryReadCard"] as? Bool == true {
                    feedItemIds = feedCards.filter { $0["isRead"] as? Bool == true }.compactMap { $0["feedItemId"] as? String }
                }
                let quoted = feedItemIds.map { "\"\($0)\"" }.joined(separator: ",")
                return #"{"data":{"\#(isDismissing ? "DismissAgentFeedItems" : "RestoreAgentFeedItems")":[\#(quoted)]}}"#
            } else if query.contains("ShowLessLikeAgentFeedItem") {
                card["isDismissed"] = true
                return #"{"data":{"ShowLessLikeAgentFeedItem":\#(feedJSON(card))}}"#
            } else if query.contains("LikeAgentFeedItem") {
                card["isLiked"] = variables["isLiked"] as? Bool ?? false
                return #"{"data":{"LikeAgentFeedItem":\#(feedJSON(card))}}"#
            } else if query.contains("ListAgentFeedStack") {
                let cards = (variables["feedStackKey"] as? String) == demoBriefStackKey ? briefCards : ((variables["feedStackKey"] as? String) == demoNewsStackKey ? newsCards : [])
                return #"{"data":{"ListAgentFeedStack":{"feedItems":\#(feedJSON(cards)),"nextCursor":null,"unreadCount":2}}}"#
            } else if query.contains("ListAgentFeed") {
                let unreadCount = feedCards.filter { $0["isRead"] as? Bool != true }.count
                return #"{"data":{"ListAgentFeed":{"feedItems":\#(feedJSON(feedCards)),"nextCursor":null,"unreadCount":\#(unreadCount)}}}"#
            }
            return nil
        }
    }
#endif
