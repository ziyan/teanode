#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented journal that the screenshots of the Journal photograph
    // (`-TeaNodeDemo`): a few weeks of days around today. One day is whole (its
    // words touched up, a story that mentions records, and every kind of
    // thing a day holds), one has only the person's words, and today is still
    // going. Every name, place and number is made up; the places are in the
    // same made-up area as the Location demo.

    /// JournalDemo holds the invented journal.
    enum JournalDemo {
        // MARK: Days

        static var today: String { JournalDays.text(Date()) }

        static func day(daysAgo: Int) -> String {
            JournalDays.adding(-daysAgo, to: today) ?? today
        }

        /// The day with everything in it.
        static var fullDay: String { day(daysAgo: 4) }
        /// The day with only the person's words.
        static var wordsDay: String { day(daysAgo: 2) }

        /// The moment `hour` hours into a day, in the phone's own zone, written
        /// as the server writes times.
        static func moment(_ day: String, hour: Double) -> String {
            let start = Calendar.current.startOfDay(for: JournalDays.date(day) ?? Date())
            return timeText(start.addingTimeInterval(hour * 3600))
        }

        static func timeText(_ date: Date) -> String {
            ISO8601DateFormatter().string(from: date)
        }

        /// The value, or the JSON null for none.
        static func orNull(_ value: Any?) -> Any {
            value ?? NSNull()
        }

        // MARK: What each day of the timeline says

        /// One day as the timeline lists it.
        struct DemoDay {
            let daysAgo: Int
            let moodLevel: Int?
            let moodWords: String
            let headline: String
            let wordsExcerpt: String
            let highCelsius: Double
            let weatherKind: String
            let facts: [String: Any]
            let photoCount: Int
        }

        // Worked out each time it is read: a stored static array of [String: Any]
        // is not concurrency-safe, and the demo reads it rarely.
        static var days: [DemoDay] {
            [
            DemoDay(
                daysAgo: 0, moodLevel: nil, moodWords: "", headline: "", wordsExcerpt: "", highCelsius: 16, weatherKind: "clear",
                facts: ["meetingCount": 2, "stepCount": 3880, "photoCount": 2], photoCount: 2),
            DemoDay(
                daysAgo: 1, moodLevel: 4, moodWords: "", headline: "A slow Sunday: soup, laundry and a long call.", wordsExcerpt: "",
                highCelsius: 16, weatherKind: "partlyCloudy", facts: ["sleepMinutes": 480, "photoCount": 4], photoCount: 4),
            DemoDay(
                daysAgo: 2, moodLevel: 3, moodWords: "", headline: "",
                wordsExcerpt: "Rain all afternoon, so I stayed in and finally sorted the shelf. Nothing else got done and that was fine.",
                highCelsius: 11, weatherKind: "rain", facts: ["stepCount": 2100], photoCount: 0),
            DemoDay(
                daysAgo: 3, moodLevel: 3, moodWords: "", headline: "Bug reports, the freeze, and home early in the rain.", wordsExcerpt: "",
                highCelsius: 11, weatherKind: "rain", facts: ["meetingCount": 3, "stepCount": 4200], photoCount: 0),
            DemoDay(
                daysAgo: 4, moodLevel: 4, moodWords: "tired, but the release is out",
                headline: "Release day: Orchard 2.4 shipped, and the park before the rain.", wordsExcerpt: "Shipped the Orchard release at last.",
                highCelsius: 14, weatherKind: "partlyCloudy",
                facts: [
                    "photoCount": 23, "stepCount": 9412, "placeCount": 5, "walkedMeters": 5400, "codingSessionCount": 5, "workProjectCount": 2, "codingMinutes": 250,
                ], photoCount: 23),
            DemoDay(
                daysAgo: 5, moodLevel: 2, moodWords: "", headline: "A late night chasing the failing upload test.", wordsExcerpt: "",
                highCelsius: 13, weatherKind: "cloudy", facts: ["sleepMinutes": 340, "codingSessionCount": 3, "workProjectCount": 1, "codingMinutes": 190], photoCount: 0),
            DemoDay(
                daysAgo: 6, moodLevel: 4, moodWords: "", headline: "Planning the freeze, and dinner with Sam.", wordsExcerpt: "",
                highCelsius: 15, weatherKind: "clear", facts: ["meetingCount": 4, "spentMinorUnits": 6820, "spentCurrency": "USD"], photoCount: 1),
            DemoDay(
                daysAgo: 7, moodLevel: 5, moodWords: "", headline: "The lake loop, the longest walk since the summer.", wordsExcerpt: "",
                highCelsius: 17, weatherKind: "clear", facts: ["walkedMeters": 9900, "placeCount": 3, "photoCount": 14], photoCount: 14),
            DemoDay(
                daysAgo: 8, moodLevel: 4, moodWords: "", headline: "Farmers market, then the bookshelf built.", wordsExcerpt: "",
                highCelsius: 15, weatherKind: "partlyCloudy", facts: ["spentMinorUnits": 9410, "spentCurrency": "USD"], photoCount: 5),
            DemoDay(
                daysAgo: 9, moodLevel: 3, moodWords: "", headline: "Flat and rainy: mostly mail and a long review.", wordsExcerpt: "",
                highCelsius: 10, weatherKind: "rain", facts: ["mailReceivedCount": 61], photoCount: 0),
            DemoDay(
                daysAgo: 10, moodLevel: 4, moodWords: "", headline: "Design review settled onboarding.", wordsExcerpt: "",
                highCelsius: 12, weatherKind: "cloudy", facts: ["meetingCount": 5], photoCount: 0),
        ]
        }

        // MARK: Pieces of the answers

        static func photoIds(_ day: DemoDay) -> [String] {
            (0..<min(4, day.photoCount)).map { "journal-photo-\(day.daysAgo)-\($0)" }
        }

        static func facts(_ values: [String: Any] = [:]) -> [String: Any] {
            var facts: [String: Any] = [
                "photoCount": 0, "placeCount": 0, "walkedMeters": 0.0, "stepCount": 0, "sleepMinutes": 0, "workoutMinutes": 0, "codingSessionCount": 0,
                "workProjectCount": 0, "codingMinutes": 0, "meetingCount": 0, "meetingMinutes": 0, "recordingCount": 0, "conversationCount": 0, "chatMessageCount": 0,
                "mailReceivedCount": 0, "mailSentCount": 0, "documentCount": 0, "transactionCount": 0, "spentMinorUnits": 0, "spentCurrency": "USD",
                "shoppingTripCount": 0, "memoryPageCount": 0, "mainPlaceName": "",
            ]
            for (key, value) in values { facts[key] = value }
            return facts
        }

        static func mood(_ day: DemoDay) -> Any {
            guard let level = day.moodLevel else { return NSNull() }
            let mood: [String: Any] = ["moodLevel": level, "moodWords": day.moodWords, "moodSource": "person"]
            return mood
        }

        static func weather(_ day: DemoDay) -> [String: Any] {
            [
                "weatherCode": 3, "weatherKind": day.weatherKind, "highCelsius": day.highCelsius, "lowCelsius": day.highCelsius - 7,
                "precipitationMM": day.weatherKind == "rain" ? 6.4 : 0.0, "placeName": "Riverside",
            ]
        }

        static func summary(_ day: DemoDay) -> [String: Any] {
            let dayText = Self.day(daysAgo: day.daysAgo)
            let photos = photoIds(day)
            return [
                "day": dayText, "isToday": day.daysAgo == 0, "mood": mood(day), "weather": weather(day), "headline": day.headline,
                "wordsExcerpt": day.wordsExcerpt, "hasWords": !day.wordsExcerpt.isEmpty, "facts": facts(day.facts), "coverPhotoId": photos.first ?? "",
                "photoIds": photos, "hasNarrative": !day.headline.isEmpty,
            ]
        }

        /// A day of the dataset by its text, if the dataset has it.
        static func demoDay(_ dayText: String) -> DemoDay? {
            days.first { day(daysAgo: $0.daysAgo) == dayText }
        }

        static func record(_ kind: String, _ identifier: String, title: String, path: String = "", hour: Double = 0, day dayText: String? = nil) -> [String: Any] {
            [
                "recordKind": kind, "recordId": identifier, "reference": "\(kind):\(identifier)", "title": title,
                "happenedAt": orNull(dayText.map { moment($0, hour: hour) }), "dashboardPath": path, "isUnavailable": false,
            ]
        }

        static func words(original: String, touchedUp: String = "", how: String = "", modifiedHour: Double = 22.8, day dayText: String) -> [String: Any] {
            [
                "original": original, "modifiedAt": orNull(original.isEmpty ? nil : moment(dayText, hour: modifiedHour)), "isTouchUpOn": !touchedUp.isEmpty,
                "touchedUp": touchedUp, "touchUpHow": how, "isTouchedUpStale": false, "touchUpPending": false, "journalNotes": [Any](),
            ]
        }

        static func narrative(
            headline: String = "", text: String = "", references: [[String: Any]] = [], corrections: [String] = [], isPending: Bool = false,
            day dayText: String
        ) -> [String: Any] {
            [
                "headline": headline, "narrative": text, "references": references, "writtenAt": orNull(text.isEmpty ? nil : moment(dayText, hour: 27.2)),
                "isStale": false, "narrativePending": isPending, "isBeyondDepth": false, "failure": "",
                "corrections": corrections.map { ["correctionText": $0, "createdAt": moment(dayText, hour: 30)] }, "sourceCount": text.isEmpty ? 0 : 71,
            ]
        }

        // MARK: The words of the whole day

        static let fullDayOriginal = "Shipped the Orchard release at last. Nothing broke, which felt almost suspicious. Walked home thru the park before the rain."
        static let fullDayTouchedUp = "Shipped the Orchard release at last. Nothing broke, which felt almost suspicious. Walked home through the park before the rain."

        static var fullDayWords: JournalWords? {
            decoded(words(original: fullDayOriginal, touchedUp: fullDayTouchedUp, how: "fix the spelling", day: fullDay), as: JournalWords.self)
        }

        static func decoded<Value: Decodable>(_ object: Any, as type: Value.Type) -> Value? {
            guard let data = try? JSONSerialization.data(withJSONObject: object) else { return nil }
            return try? JSONDecoder().decode(type, from: data)
        }

        // MARK: One day whole

        static func dayView(_ dayText: String) -> [String: Any] {
            let demo = demoDay(dayText)
            let isFull = dayText == fullDay
            let isWordsOnly = dayText == wordsDay
            let isToday = dayText == today
            var view: [String: Any] = [
                "day": dayText, "timeZone": TimeZone.current.identifier, "isToday": isToday,
                "weekday": (JournalDays.date(dayText)?.formatted(.dateTime.weekday(.wide).locale(Locale(identifier: "en_US"))) ?? "").lowercased(),
                "weekStart": JournalDays.weekStart(of: dayText, weekStartsOn: "monday") ?? dayText,
                "words": words(original: "", day: dayText), "mood": orNull(demo.map { mood($0) }), "weather": orNull(demo.map { weather($0) }),
                "facts": facts(demo?.facts ?? [:]), "narrative": narrative(day: dayText), "choices": [Any](), "pins": [Any](), "coverPhotoId": "",
                "workProjects": [Any](), "codingSessions": [Any](), "documents": [Any](), "documentTotal": 0, "conversations": [Any](), "chats": [Any](), "mail": [Any](),
                "mailTotal": 0, "recordings": [Any](), "photos": [Any](), "photoTotal": 0, "visits": [Any](), "events": [Any](), "reminders": [Any](),
                "transactions": [Any](), "health": NSNull(), "shoppingTrips": [Any](), "notes": [Any](), "memoryPages": [Any](), "unavailableSections": [Any](),
                "moments": [Any](),
            ]
            if isFull { fillFullDay(&view, dayText) }
            if isWordsOnly {
                view["words"] = words(
                    original: "Rain all afternoon, so I stayed in and finally sorted the shelf. Nothing else got done and that was fine.", day: dayText)
            }
            if isToday {
                view["events"] = [
                    event("today-standup", "Stand-up", hour: 9.5, length: 0.5, day: dayText, location: ""),
                    event("today-planning", "Planning for the freeze", hour: 14, length: 1, day: dayText, location: "Room 2"),
                ]
                let todayHealth: [String: Any] = [
                    "stepCount": 3880, "stepsByHour": [Int](), "sleepMinutes": 0, "sleepStages": [Any](), "restingHeartRate": 0.0, "workouts": [Any](),
                ]
                view["health"] = todayHealth
            }
            if let demo, !demo.headline.isEmpty, !isFull {
                view["narrative"] = narrative(headline: demo.headline, text: demo.headline, day: dayText)
            }
            return view
        }

        static func event(_ identifier: String, _ title: String, hour: Double, length: Double, day dayText: String, location: String) -> [String: Any] {
            [
                "eventId": identifier, "title": title, "calendarName": "Home", "startsAt": moment(dayText, hour: hour), "endsAt": moment(dayText, hour: hour + length),
                "isAllDay": false, "location": location, "reference": "calendar_occurrence:\(identifier)@\(moment(dayText, hour: hour))",
            ]
        }

        static func visit(_ identifier: String, _ name: String, from: Double, until: Double, latitude: Double, longitude: Double, day dayText: String) -> [String: Any] {
            [
                "visitId": identifier, "placeId": "place-\(identifier)", "placeName": name, "arrivedAt": moment(dayText, hour: from),
                "departedAt": moment(dayText, hour: until), "latitude": latitude, "longitude": longitude,
            ]
        }

        /// Everything a day can hold.
        static func fillFullDay(_ view: inout [String: Any], _ dayText: String) {
            let references = [
                record("place_visit", "visit-park", title: "Riverside Park", path: "/location?day=\(dayText)", hour: 17.5, day: dayText),
                record("recording", "recording-review", title: "design review", path: "/recordings/recording-review", hour: 13, day: dayText),
                record("mail", "mail-lena", title: "Lena's mockups", path: "/mailbox/inbox/thread/mail-lena", hour: 15, day: dayText),
            ]
            let text =
                "A release day. Two coding sessions finished the release branch and the build went out before lunch. The [[recording:recording-review|design review]] settled onboarding, and [[mail:mail-lena|Lena's question about the beta]] is waiting for a reply.\n\nOn the way home you walked through [[place_visit:visit-park|the park]] before the rain."
            view["words"] = words(original: fullDayOriginal, touchedUp: fullDayTouchedUp, how: "fix the spelling", day: dayText)
            view["narrative"] = narrative(
                headline: "Release day: Orchard 2.4 shipped, and the park before the rain.", text: text, references: references,
                corrections: ["The recording was the design review, not planning."], day: dayText)
            view["coverPhotoId"] = "journal-photo-4-0"
            view["photos"] = (0..<8).map { index in
                ["photoId": "journal-photo-4-\(index)", "takenAt": moment(dayText, hour: 17.4 + Double(index) * 0.05), "placeName": "Riverside Park", "isVideo": false]
                    as [String: Any]
            }
            view["photoTotal"] = 23
            view["visits"] = [
                visit("visit-home", "Home", from: 0, until: 9, latitude: 10.5000, longitude: 20.5000, day: dayText),
                visit("visit-cafe", "Corner Café", from: 8.5, until: 8.9, latitude: 10.5025, longitude: 20.5030, day: dayText),
                visit("visit-office", "Office", from: 9.1, until: 17.3, latitude: 10.5150, longitude: 20.5200, day: dayText),
                visit("visit-park", "Riverside Park", from: 17.5, until: 18.2, latitude: 10.5040, longitude: 20.5060, day: dayText),
                visit("visit-home-evening", "Home", from: 18.6, until: 24, latitude: 10.5000, longitude: 20.5000, day: dayText),
            ]
            view["codingSessions"] = [
                [
                    "documentId": "session-1", "title": "Release branch for Orchard", "directory": "~/projects/orchard", "assistant": "Claude Code",
                    "startedAt": moment(dayText, hour: 9.3), "endedAt": moment(dayText, hour: 12.3), "linkReason": "",
                ] as [String: Any],
                [
                    "documentId": "session-2", "title": "Fix the flaky upload test", "directory": "~/projects/orchard", "assistant": "Codex",
                    "startedAt": moment(dayText, hour: 14), "endedAt": moment(dayText, hour: 15.2), "linkReason": "",
                ] as [String: Any],
            ]
            view["documents"] = [
                [
                    "documentId": "document-1", "title": "Orchard 2.4 release notes", "sourceName": "Team wiki", "happenedAt": moment(dayText, hour: 11),
                    "isChosen": true, "linkReason": "You wrote most of it today",
                ] as [String: Any],
                [
                    "documentId": "document-2", "title": "Lunch order", "sourceName": "Team chat", "happenedAt": moment(dayText, hour: 11.6),
                    "isChosen": false, "linkReason": "",
                ] as [String: Any],
                [
                    "documentId": "document-3", "title": "Build is green again", "sourceName": "Team chat", "happenedAt": moment(dayText, hour: 15.4),
                    "isChosen": false, "linkReason": "",
                ] as [String: Any],
            ]
            view["documentTotal"] = 38
            view["workProjects"] = demoWorkProjects(dayText)
            view["memoryPages"] = [
                ["nodeId": "node-1", "path": "projects/orchard", "title": "Orchard release", "factCount": 3] as [String: Any],
                ["nodeId": "node-2", "path": "people/sam", "title": "Sam", "factCount": 1] as [String: Any],
            ]
            view["mail"] = [
                [
                    "itemId": "mail-lena", "fromName": "Lena", "subject": "Onboarding mockups", "receivedAt": moment(dayText, hour: 15), "isSent": false,
                    "isImportant": true, "isChosen": true, "linkReason": "You will want to reply",
                ] as [String: Any],
                [
                    "itemId": "mail-water", "fromName": "City Water", "subject": "Bill due November 9", "receivedAt": moment(dayText, hour: 10), "isSent": false,
                    "isImportant": false, "isChosen": true, "linkReason": "A bill with a due date",
                ] as [String: Any],
            ]
            view["mailTotal"] = 26
            view["recordings"] = [
                [
                    "recordingId": "recording-review", "title": "Design review", "recordedAt": moment(dayText, hour: 13), "durationMS": 6_480_000,
                    "summary": "Onboarding settled.",
                ] as [String: Any]
            ]
            view["choices"] = [
                [
                    "linkId": "link-1", "linkSource": "agent", "linkReason": "You were asked to decide",
                    "record": record("chat_message", "chat-freeze", title: "Release freeze, #platform", hour: 15.9, day: dayText), "isHidden": false,
                ] as [String: Any]
            ]
            // The person pinned the design review to the day themselves.
            view["pins"] = [
                [
                    "linkId": "pin-1", "linkSource": "person", "linkReason": "",
                    "record": record("recording", "recording-review", title: "Design review", path: "/recordings/recording-review", hour: 13, day: dayText),
                    "isHidden": false,
                ] as [String: Any]
            ]
            view["shoppingTrips"] = [
                [
                    "tripId": "trip-1", "storeName": "Corner Market", "listNames": ["Groceries", "Household"], "startedAt": moment(dayText, hour: 19.2),
                    "finishedAt": moment(dayText, hour: 19.8), "dashboardPath": "/shopping",
                ] as [String: Any],
                [
                    "tripId": "trip-2", "storeName": "", "listNames": ["Hardware"], "startedAt": moment(dayText, hour: 12.2),
                    "finishedAt": NSNull(), "dashboardPath": "/shopping",
                ] as [String: Any],
            ]
            view["transactions"] = [
                transaction("transaction-1", "Morning Bakery", hour: 8.7, minorUnits: -780, day: dayText),
                transaction("transaction-2", "Noodle Bar", hour: 12.5, minorUnits: -1420, day: dayText),
                transaction("transaction-3", "Corner Market", hour: 19.5, minorUnits: -5210, day: dayText),
            ]
            view["health"] = [
                "stepCount": 9412, "stepsByHour": [Int](), "sleepMinutes": 370,
                "sleepStages": [
                    ["sleepStage": "core", "minutes": 120], ["sleepStage": "deep", "minutes": 50], ["sleepStage": "core", "minutes": 90],
                    ["sleepStage": "rem", "minutes": 60], ["sleepStage": "awake", "minutes": 15], ["sleepStage": "core", "minutes": 180],
                    ["sleepStage": "rem", "minutes": 70],
                ], "restingHeartRate": 58.0,
                "workouts": [["sampleId": "workout-1", "activityType": "walking", "startedAt": moment(dayText, hour: 17.5), "minutes": 32, "meters": 2600.0]],
            ] as [String: Any]
            view["events"] = [
                event("event-standup", "Stand-up", hour: 9.5, length: 0.5, day: dayText, location: ""),
                event("event-review", "Design review", hour: 13, length: 1.8, day: dayText, location: "Room 2"),
            ]
            view["conversations"] = [
                [
                    "conversationId": "conversation-1", "title": "Release checklist", "isMain": false, "firstAt": moment(dayText, hour: 9.2),
                    "lastAt": moment(dayText, hour: 12), "messageCount": 14,
                ] as [String: Any],
                [
                    "conversationId": "conversation-main", "title": "Main chat", "isMain": true, "firstAt": moment(dayText, hour: 18.2),
                    "lastAt": moment(dayText, hour: 18.9), "messageCount": 31,
                ] as [String: Any],
            ]
            view["moments"] = [
                moment(dayText, "Woke up", "Slept 6 h 10 min", kind: "health_day", hour: 6.8),
                moment(dayText, "Release branch for Orchard", "Claude Code, 3 h", kind: "agent_document", hour: 9.3),
                moment(dayText, "Design review recorded", "1 h 48 min", kind: "recording", hour: 13, identifier: "recording-review", path: "/recordings/recording-review"),
                moment(dayText, "Asked to decide the freeze", "You chose Monday", kind: "chat_message", hour: 15.9, identifier: "chat-freeze"),
                moment(dayText, "Riverside Park", "11 photos", kind: "place_visit", hour: 17.5, identifier: "visit-park", path: "/location?day=\(dayText)"),
                moment(
                    dayText, "Shopping at Corner Market", "for Groceries and Household", kind: "shopping_trip", hour: 19.2, identifier: "trip-1",
                    path: "/shopping"),
                moment(dayText, "You wrote in the journal", "", kind: "note", hour: 22.8),
            ]
        }

        /// The day's coding by project: what was done, what it was about, and
        /// the requests sent for review.
        static func demoWorkProjects(_ dayText: String) -> [[String: Any]] {
            let orchardTopics = [
                "Release branch for Orchard", "Fix the flaky upload test", "Rename the config keys", "Update the changelog", "Tidy the build scripts",
                "Remove the old fixtures",
            ]
            let orchardRequests: [[String: Any]] = [
                [
                    "documentId": "request-1", "title": "Fix the flaky upload test", "address": "https://git.example.com/orchard/orchard/pulls/12",
                    "happenedAt": moment(dayText, hour: 15.2), "isDraft": false,
                ],
                [
                    "documentId": "request-2", "title": "Rename the config keys", "address": "https://git.example.com/orchard/orchard/pulls/13",
                    "happenedAt": moment(dayText, hour: 16.4), "isDraft": true,
                ],
            ]
            let orchard: [String: Any] = [
                "name": "orchard", "firstAt": moment(dayText, hour: 9.3), "lastAt": moment(dayText, hour: 17.1), "activeMinutes": 190, "stretchCount": 3,
                "topics": orchardTopics, "shipped": orchardRequests,
                "summary": "Cut the release branch and fixed the upload test that kept failing.", "documentId": "session-1",
            ]
            let notes: [String: Any] = [
                "name": "garden-notes", "firstAt": moment(dayText, hour: 20.1), "lastAt": moment(dayText, hour: 21), "activeMinutes": 60, "stretchCount": 1,
                "topics": ["Plan the spring beds"], "shipped": [Any](), "summary": "", "documentId": "session-3",
            ]
            return [orchard, notes]
        }

        static func moment(
            _ dayText: String, _ title: String, _ detail: String, kind: String, hour: Double, identifier: String = "", path: String = ""
        ) -> [String: Any] {
            [
                "at": Self.moment(dayText, hour: hour), "recordKind": kind, "recordId": identifier.isEmpty ? "\(kind)-\(Int(hour * 10))" : identifier, "title": title,
                "detail": detail, "dashboardPath": path,
            ]
        }

        static func transaction(_ identifier: String, _ name: String, hour: Double, minorUnits: Int, day dayText: String) -> [String: Any] {
            [
                "transactionId": identifier, "postedOn": dayText, "transactedAt": moment(dayText, hour: hour), "description": name, "accountName": "Everyday",
                "amountMinorUnits": minorUnits, "currency": "USD",
            ]
        }

        // MARK: A week, a month, a year

        static func periodView(periodKind: String, anchorDay: String) -> [String: Any] {
            let kind = JournalPeriodKind(rawValue: periodKind) ?? .week
            let start = JournalDays.periodStart(of: anchorDay, periodKind: kind, weekStartsOn: "monday") ?? anchorDay
            let end: String
            switch kind {
            case .week: end = JournalDays.adding(6, to: start) ?? start
            case .month: end = JournalDays.adding(-1, to: JournalDays.movedPeriodStart(from: start, periodKind: .month, by: 1) ?? start) ?? start
            case .year: end = String(start.prefix(4)) + "-12-31"
            }
            var daySummaries: [[String: Any]] = []
            var dayText = start
            while dayText <= end, dayText <= today, kind != .year, daySummaries.count < 31 {
                if let demo = demoDay(dayText) {
                    daySummaries.append(summary(demo))
                } else {
                    // A day the dataset has nothing for: no mood, no words.
                    let quietDay = DemoDay(
                        daysAgo: -1, moodLevel: nil, moodWords: "", headline: "", wordsExcerpt: "", highCelsius: 13, weatherKind: "cloudy", facts: [:],
                        photoCount: 0)
                    var quietSummary = summary(quietDay)
                    quietSummary["day"] = dayText
                    quietSummary["isToday"] = false
                    daySummaries.append(quietSummary)
                }
                dayText = JournalDays.adding(1, to: dayText) ?? "9999-12-31"
            }
            let moods = daySummaries.compactMap { ($0["mood"] as? [String: Any])?["moodLevel"] as? Int }
            var moodCounts = [0, 0, 0, 0, 0]
            for level in moods where (1...5).contains(level) { moodCounts[level - 1] += 1 }
            let name = periodKind == "week" ? "week" : periodKind == "month" ? "month" : "year"
            let text =
                "A \(name) that bent around one day. The early days went to the release branch, with the upload test failing twice, and Thursday the release went out. The walk through [[place_visit:visit-park|the park]] afterward was the best part."
            let months: [Any] = kind == .year ? yearMonths(start) : []
            let chapters: [Any] = kind == .year ? [yearChapter(start)] : []
            // The dream writes a story the night after the last day it covers:
            // a period still under way was written last night.
            let writtenDay = min(end, JournalDays.adding(-1, to: today) ?? today)
            let totals = facts(["photoCount": 49, "placeCount": 11, "walkedMeters": 21700.0, "stepCount": 58210, "sleepMinutes": 2530, "codingSessionCount": 7, "workProjectCount": 4, "codingMinutes": 1170, "meetingCount": 14, "mailReceivedCount": 188, "spentMinorUnits": 31240])
            return [
                "periodKind": periodKind, "periodStart": start, "periodEnd": end, "isCurrent": start <= today && today <= end,
                "lookingBack": words(original: "Too much of the week at the desk. Keep Saturdays free.", day: start),
                "narrative": narrative(
                    headline: "Orchard 2.4 shipped on Thursday. Sleep slipped midweek; the weekend was the best in a month.", text: text,
                    references: [record("place_visit", "visit-park", title: "Riverside Park", path: "/location?day=\(fullDay)", day: fullDay)], day: writtenDay),
                "choices": [
                    [
                        "linkId": "period-link-1", "linkSource": "agent", "linkReason": "The release went out",
                        "record": record("agent_document", "document-1", title: "Orchard 2.4 release notes", day: fullDay), "isHidden": false,
                    ] as [String: Any]
                ],
                "days": daySummaries,
                "months": months, "totals": totals, "moodCounts": moodCounts, "dayCount": daySummaries.count, "chapters": chapters,
            ]
        }

        static func yearChapter(_ yearStart: String) -> [String: Any] {
            ["fromDay": yearStart, "untilDay": JournalDays.adding(90, to: yearStart) ?? yearStart, "title": "A quiet winter", "detail": "Short days, a lot of reading."]
        }

        static func yearMonths(_ yearStart: String) -> [[String: Any]] {
            (0..<10).map { index in
                let start = JournalDays.movedPeriodStart(from: yearStart, periodKind: .month, by: index) ?? yearStart
                let dayMoods = (0..<30).map { dayIndex in (dayIndex * 7 + index * 3) % 6 }
                return [
                    "periodStart": start, "headline": index % 3 == 0 ? "" : "A month of steady work and short evenings.", "dayMoods": dayMoods, "placeCount": 8 + index,
                    "photoCount": 40 + index * 11,
                ]
            }
        }

        // MARK: Search

        /// The words the screenshot of the search has typed.
        static let searchQuery = "park"

        /// The days and periods that hold the words, newest first, as a page of
        /// a search: the demo's own, so every hit names a made-up day.
        static func searchPage(query: String, first: Int, offset: Int) -> [String: Any] {
            let lowercasedQuery = query.lowercased()
            let lake = day(daysAgo: 7)
            let candidates: [[String: Any]] = [
                [
                    "day": fullDay, "periodKind": "", "headline": "Release day: Orchard 2.4 shipped, and the park before the rain.",
                    "snippet": "…Nothing broke, which felt almost suspicious. Walked home through the park before the rain.",
                ],
                [
                    "day": JournalDays.weekStart(of: fullDay, weekStartsOn: "monday") ?? fullDay, "periodKind": "week",
                    "headline": "Orchard 2.4 shipped on Thursday. Sleep slipped midweek; the weekend was the best in a month.",
                    "snippet": "…The walk through the park afterward was the best part.",
                ],
                [
                    "day": lake, "periodKind": "", "headline": "The lake loop, the longest walk since the summer.",
                    "snippet": "…ended at the park by the lake, where the geese had taken over the benches.",
                ],
                [
                    "day": JournalDays.monthStart(of: lake) ?? lake, "periodKind": "month", "headline": "",
                    "snippet": "…the park, the lake and the long walks that filled the month.",
                ],
            ]
            let hits = candidates.filter { hit in
                let headline = hit["headline"] as? String ?? ""
                let snippet = hit["snippet"] as? String ?? ""
                return (headline + " " + snippet).lowercased().contains(lowercasedQuery)
            }
            return ["hits": Array(hits.dropFirst(max(0, offset)).prefix(max(0, first))), "totalCount": hits.count]
        }

        // MARK: Settings

        static func settings(isWeatherOn: Bool = true, isEveningQuestionOn: Bool = true, eveningQuestionTime: String = "21:00") -> [String: Any] {
            [
                "isWeatherOn": isWeatherOn, "isEveningQuestionOn": isEveningQuestionOn, "eveningQuestionTime": eveningQuestionTime, "weekStartsOn": "monday",
                "narrativeDepthDays": 92, "modelProviderName": "Example Provider",
            ]
        }
    }

    // MARK: The answers

    extension DemoServer {
        /// What the Journal screens ask, or nil for any other question.
        static func journalAnswer(query: String, body: Data?) -> String? {
            let variables = requestVariables(body)
            if query.contains("ListJournalDays(") {
                let page: [String: Any] = [
                    "days": JournalDemo.days.map(JournalDemo.summary), "totalCount": JournalDemo.days.count, "today": JournalDemo.today,
                    "firstDay": JournalDemo.day(daysAgo: 40),
                ]
                return journalJSON("ListJournalDays", page)
            }
            if query.contains("ReadJournalDay("), let day = variables["day"] as? String {
                return journalJSON("ReadJournalDay", JournalDemo.dayView(day))
            }
            if query.contains("ReadJournalPeriod("), let periodKind = variables["periodKind"] as? String, let day = variables["day"] as? String {
                return journalJSON("ReadJournalPeriod", JournalDemo.periodView(periodKind: periodKind, anchorDay: day))
            }
            if query.contains("UpdateJournalSettings(") {
                return journalJSON(
                    "UpdateJournalSettings",
                    JournalDemo.settings(
                        isWeatherOn: variables["isWeatherOn"] as? Bool ?? true, isEveningQuestionOn: variables["isEveningQuestionOn"] as? Bool ?? true,
                        eveningQuestionTime: variables["eveningQuestionTime"] as? String ?? "21:00"))
            }
            if query.contains("ReadJournalSettings") {
                return journalJSON("ReadJournalSettings", JournalDemo.settings())
            }
            if query.contains("EraseJournal") {
                return journalJSON("EraseJournal", ["dayCount": 120, "periodCount": 30, "linkCount": 400])
            }
            if query.contains("SearchJournal(") {
                let page = JournalDemo.searchPage(
                    query: variables["query"] as? String ?? "", first: variables["first"] as? Int ?? JournalSearch.pageSize,
                    offset: variables["offset"] as? Int ?? 0)
                return journalJSON("SearchJournal", page)
            }
            if query.contains("PinJournalRecord(") {
                let pinned: [String: Any] = [
                    "linkId": "demo-pin", "linkSource": "person", "linkReason": "", "isHidden": false,
                    "record": JournalDemo.record(
                        variables["recordKind"] as? String ?? "", variables["recordId"] as? String ?? "", title: "Pinned"),
                ]
                return journalJSON("PinJournalRecord", pinned)
            }
            if query.contains("RemoveJournalLink(") {
                return journalJSON("RemoveJournalLink", true)
            }
            // The mutations. The days are fixed, so a change is acknowledged and not shown.
            for operation in ["SetJournalWords", "SetJournalMood", "SetJournalLookingBack", "TouchUpJournalWords", "StopJournalTouchUp"] where query.contains("\(operation)(") {
                return journalJSON(operation, ["day": variables["day"] as? String ?? "", "periodKind": "", "periodStart": "", "words": NSNull(), "lookingBack": NSNull()])
            }
            for operation in ["CorrectJournalNarrative", "RemoveJournalCorrection", "WriteJournalNarrative", "DeleteJournalNarrative"] where query.contains("\(operation)(") {
                return journalJSON(operation, ["narrativePending": false])
            }
            return nil
        }

        /// `{"data":{"<operation>": <payload>}}`.
        private static func journalJSON(_ operation: String, _ payload: Any) -> String {
            let object: [String: Any] = ["data": [operation: payload]]
            guard let data = try? JSONSerialization.data(withJSONObject: object), let text = String(data: data, encoding: .utf8) else {
                return #"{"data":null}"#
            }
            return text
        }
    }
#endif
