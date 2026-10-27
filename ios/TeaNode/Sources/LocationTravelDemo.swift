#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented trips and travel totals that the screenshots of the Location
    // screen photograph (`-TeaNodeDemo`): a walk then a drive, a bike trip, one
    // trip corrected to a bus, and a few weeks of distances. Every place is one
    // of the invented ones in LocationDemo.swift.

    extension LocationDemo {
        /// One leg of an invented trip.
        struct DemoLeg {
            let id: String
            let startedAt: Date
            let endedAt: Date
            let distanceMeters: Int
            let detectedTravelMode: String
            /// What counts: the detected way, or the one the person corrected it to.
            var travelMode: String? = nil

            var shownTravelMode: String { travelMode ?? detectedTravelMode }

            var json: String { json(travelMode: shownTravelMode) }

            func json(travelMode: String, isCorrected: Bool? = nil) -> String {
                let isTravelModeCorrected = isCorrected ?? (travelMode != detectedTravelMode)
                return #"""
                    {"id":"\#(id)","startedAt":"\#(CalendarDates.text(startedAt))","endedAt":"\#(CalendarDates.text(endedAt))","durationSeconds":\#(Int(endedAt.timeIntervalSince(startedAt))),"distanceMeters":\#(distanceMeters),"travelMode":"\#(travelMode)","detectedTravelMode":"\#(detectedTravelMode)","isTravelModeCorrected":\#(isTravelModeCorrected),"detectionSource":"motion"}
                    """#
            }
        }

        /// One invented trip, between two visits.
        struct DemoTrip {
            let id: String
            let startedAt: Date
            let endedAt: Date
            let fromPlaceJSON: String
            let toPlaceJSON: String
            let legs: [DemoLeg]

            var json: String {
                let distanceMeters = legs.reduce(0) { $0 + $1.distanceMeters }
                return #"""
                    {"id":"\#(id)","startedAt":"\#(CalendarDates.text(startedAt))","endedAt":"\#(CalendarDates.text(endedAt))","durationSeconds":\#(Int(endedAt.timeIntervalSince(startedAt))),"distanceMeters":\#(distanceMeters),"fromPlace":\#(fromPlaceJSON),"toPlace":\#(toPlaceJSON),"legs":[\#(legs.map(\.json).joined(separator: ","))]}
                    """#
            }
        }

        private static func demoDate(_ daysAgo: Int, _ hour: Int, _ minute: Int) -> Date {
            ServerTime.date(DemoServer.demoTime(daysAgo: daysAgo, hour: hour, minute: minute)) ?? Date()
        }

        /// The person's trips between the demo visits of today and yesterday.
        static var trips: [DemoTrip] {
            func placeJSON(_ id: String) -> String { place(id).json }
            return [
                DemoTrip(
                    id: "trip-1", startedAt: demoDate(0, 7, 50), endedAt: demoDate(0, 8, 5), fromPlaceJSON: placeJSON(homePlaceId),
                    toPlaceJSON: placeJSON(cafePlaceId),
                    legs: [DemoLeg(id: "leg-1", startedAt: demoDate(0, 7, 51), endedAt: demoDate(0, 8, 4), distanceMeters: 520, detectedTravelMode: "walk")]),
                DemoTrip(
                    id: "trip-2", startedAt: demoDate(0, 8, 40), endedAt: demoDate(0, 9, 5), fromPlaceJSON: placeJSON(cafePlaceId),
                    toPlaceJSON: placeJSON(officePlaceId),
                    legs: [
                        DemoLeg(id: "leg-2", startedAt: demoDate(0, 8, 41), endedAt: demoDate(0, 8, 47), distanceMeters: 410, detectedTravelMode: "walk"),
                        DemoLeg(id: "leg-3", startedAt: demoDate(0, 8, 47), endedAt: demoDate(0, 9, 4), distanceMeters: 4300, detectedTravelMode: "car"),
                    ]),
                DemoTrip(
                    id: "trip-4", startedAt: demoDate(1, 8, 25), endedAt: demoDate(1, 9, 0), fromPlaceJSON: placeJSON(homePlaceId),
                    toPlaceJSON: placeJSON(officePlaceId),
                    legs: [DemoLeg(id: "leg-4", startedAt: demoDate(1, 8, 30), endedAt: demoDate(1, 8, 55), distanceMeters: 4600, detectedTravelMode: "bike")]),
                DemoTrip(
                    id: "trip-5", startedAt: demoDate(1, 17, 45), endedAt: demoDate(1, 19, 10), fromPlaceJSON: placeJSON(officePlaceId),
                    toPlaceJSON: placeJSON(homePlaceId),
                    legs: [
                        DemoLeg(
                            id: "leg-5", startedAt: demoDate(1, 17, 55), endedAt: demoDate(1, 18, 45), distanceMeters: 8200, detectedTravelMode: "car",
                            travelMode: "bus")
                    ]),
            ]
        }

        /// The first person's trip today, from the gym to the school, as the
        /// path in `historyPersonPoints` runs.
        static var historyPersonTrips: [DemoTrip] {
            let now = historyPersonNow
            return [
                DemoTrip(
                    id: "trip-sam-1", startedAt: now.addingTimeInterval(-3 * 3600), endedAt: now.addingTimeInterval(-50 * 60),
                    fromPlaceJSON: historyPersonGym, toPlaceJSON: historyPersonSchool,
                    legs: [
                        DemoLeg(
                            id: "leg-sam-1", startedAt: now.addingTimeInterval(-175 * 60), endedAt: now.addingTimeInterval(-140 * 60),
                            distanceMeters: 2300, detectedTravelMode: "car", travelMode: "bus"),
                        DemoLeg(
                            id: "leg-sam-2", startedAt: now.addingTimeInterval(-140 * 60), endedAt: now.addingTimeInterval(-134 * 60),
                            distanceMeters: 350, detectedTravelMode: "walk"),
                    ])
            ]
        }

        /// The distances of invented weeks: always a car and a walk, a bike
        /// most weeks, now and then a bus or a train, and once in a while a
        /// plane. Worked out from the day a period starts, so a period has the
        /// same distances whichever page it is read on.
        static func travelTotalsJSON(period: String, from: Date, until: Date) -> [String] {
            var calendar = Calendar.current
            calendar.firstWeekday = 2
            calendar.minimumDaysInFirstWeek = 4
            let component: Calendar.Component
            let scale: Double
            switch period {
            case "day":
                component = .day
                scale = 1.0 / 7
            case "month":
                component = .month
                scale = 4.3
            case "year":
                component = .year
                scale = 52
            default:
                component = .weekOfYear
                scale = 1
            }
            let dayFormat = DateFormatter()
            dayFormat.calendar = calendar
            dayFormat.locale = Locale(identifier: "en_US_POSIX")
            dayFormat.dateFormat = "yyyy-MM-dd"
            var answers: [String] = []
            var start = calendar.dateInterval(of: component, for: from)?.start ?? from
            while start < until {
                let dayNumber = Int(start.timeIntervalSince1970 / 86400)
                let mix = Double((dayNumber * 37) % 100) / 100
                // Some days and weeks the person went nowhere.
                if component == .day || component == .weekOfYear, (dayNumber / (component == .day ? 1 : 7)) % 7 == 3 {
                    start = calendar.date(byAdding: component, value: 1, to: start) ?? until
                    continue
                }
                let vary = 0.75 + mix * 0.5
                // The way, the week's distance in kilometers, and its legs.
                var ways: [(travelMode: String, kilometers: Double, legCount: Int)] = [
                    ("car", 96 * vary, 9), ("walk", 11 * vary, 14),
                ]
                if dayNumber % 3 != 0 { ways.append(("bike", 24 * vary, 4)) }
                if dayNumber % 4 == 1 { ways.append(("bus", 18 * vary, 3)) }
                if dayNumber % 6 == 2 { ways.append(("train", 64 * vary, 2)) }
                if period != "day", dayNumber % 11 == 5 { ways.append(("plane", 740 * vary, 1)) }
                let startDay = dayFormat.string(from: start)
                for way in ways {
                    let distanceMeters = Int((way.kilometers * scale * 1000).rounded())
                    let speedMetersPerSecond: Double = ["walk": 1.4, "bike": 5, "car": 12, "bus": 8, "train": 25, "plane": 220][way.travelMode] ?? 5
                    let durationSeconds = Int((Double(distanceMeters) / speedMetersPerSecond).rounded())
                    let legCount = max(1, Int((Double(way.legCount) * scale).rounded()))
                    answers.append(
                        #"{"periodStartDay":"\#(startDay)","travelMode":"\#(way.travelMode)","distanceMeters":\#(distanceMeters),"durationSeconds":\#(durationSeconds),"legCount":\#(legCount)}"#
                    )
                }
                start = calendar.date(byAdding: component, value: 1, to: start) ?? until
            }
            return answers
        }
    }

    extension DemoServer {
        /// What the trips and the Travel screen ask, or nil for any other question.
        static func travelAnswer(query: String, body: Data?) -> String? {
            let variables = requestVariables(body)
            var from = Date.distantPast
            var until = Date.distantFuture
            if let text = variables["from"] as? String, let date = ServerTime.date(text) { from = date }
            if let text = variables["until"] as? String, let date = ServerTime.date(text) { until = date }
            if query.contains("ListTrips(") {
                let isHistoryPerson = variables["userId"] as? String == LocationDemo.historyPersonId
                let trips = isHistoryPerson ? LocationDemo.historyPersonTrips : LocationDemo.trips
                let inRange = trips.filter { $0.startedAt >= from && $0.startedAt < until }
                return #"{"data":{"ListTrips":{"trips":[\#(inRange.map(\.json).joined(separator: ","))],"nextOffset":null}}}"#
            }
            if query.contains("SetTripLegTravelMode(") {
                // Echoes what was asked; nothing is kept.
                let tripLegId = variables["tripLegId"] as? String ?? ""
                let legs = (LocationDemo.trips + LocationDemo.historyPersonTrips).flatMap(\.legs)
                guard let leg = legs.first(where: { $0.id == tripLegId }) else { return nil }
                let travelMode = variables["travelMode"] as? String
                return #"{"data":{"SetTripLegTravelMode":\#(leg.json(travelMode: travelMode ?? leg.detectedTravelMode, isCorrected: travelMode != nil))}}"#
            }
            if query.contains("ListTravelTotals(") {
                let period = variables["period"] as? String ?? "week"
                let totals = LocationDemo.travelTotalsJSON(period: period, from: from, until: until)
                return #"{"data":{"ListTravelTotals":[\#(totals.joined(separator: ","))]}}"#
            }
            return nil
        }
    }
#endif
