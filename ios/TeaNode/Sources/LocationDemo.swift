#if DEBUG
    import Foundation
    import TeaNodeKit

    // The invented places, visits and positions that the screenshots of the
    // Location screen photograph (`-TeaNodeDemo`). Every name and coordinate
    // is made up, in one made-up area; no map shows them but open water.

    /// LocationDemo holds the invented person's places.
    enum LocationDemo {
        static let homePlaceId = "place-home"
        static let parkPlaceId = "place-park"
        static let cafePlaceId = "place-cafe"
        static let schoolPlaceId = "place-school"
        static let officePlaceId = "place-office"

        struct Place {
            let id: String
            let name: String
            let isNamedByPerson: Bool
            let label: String
            let latitudeDegrees: Double
            let longitudeDegrees: Double
            let radiusMeters: Double
            let osmCategory: String
            let visitCount: Int
            let lastVisitDaysAgo: Int
            var isAddressTyped = false

            var json: String { json(isAddressTyped: isAddressTyped) }

            func json(isAddressTyped: Bool) -> String {
                let lastVisit = DemoServer.demoTime(daysAgo: lastVisitDaysAgo, hour: 8, minute: 5)
                return #"""
                    {"id":"\#(id)","userId":"robin","placeName":"\#(name)","isNamedByPerson":\#(isNamedByPerson),"placeLabel":"\#(label)","latitudeDegrees":\#(latitudeDegrees),"longitudeDegrees":\#(longitudeDegrees),"radiusMeters":\#(radiusMeters),"osmType":"way","osmId":1001,"osmName":"\#(name)","osmCategory":"\#(osmCategory)","addressText":"\#(isAddressTyped ? "12 Elm Street, Exampleton" : "")","isAddressTyped":\#(isAddressTyped),"visitCount":\#(visitCount),"lastVisitAt":"\#(lastVisit)","namingErrorMessage":"","createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-01T08:00:00Z"}
                    """#
            }
        }

        static let places = [
            Place(
                id: homePlaceId, name: "Home", isNamedByPerson: true, label: "home", latitudeDegrees: 10.5000, longitudeDegrees: 20.5000,
                radiusMeters: 75, osmCategory: "", visitCount: 41, lastVisitDaysAgo: 0),
            Place(
                id: officePlaceId, name: "Office", isNamedByPerson: true, label: "work", latitudeDegrees: 10.5150, longitudeDegrees: 20.5200,
                radiusMeters: 120, osmCategory: "office=company", visitCount: 22, lastVisitDaysAgo: 0),
            Place(
                id: schoolPlaceId, name: "Hillside School", isNamedByPerson: true, label: "school", latitudeDegrees: 10.5080,
                longitudeDegrees: 20.4950, radiusMeters: 150, osmCategory: "amenity=school", visitCount: 18, lastVisitDaysAgo: 1),
            Place(
                id: parkPlaceId, name: "Riverside Park", isNamedByPerson: false, label: "", latitudeDegrees: 10.5040, longitudeDegrees: 20.5060,
                radiusMeters: 200, osmCategory: "leisure=park", visitCount: 9, lastVisitDaysAgo: 0),
            Place(
                id: cafePlaceId, name: "Corner Café", isNamedByPerson: false, label: "", latitudeDegrees: 10.5025, longitudeDegrees: 20.5030,
                radiusMeters: 60, osmCategory: "amenity=cafe", visitCount: 5, lastVisitDaysAgo: 0, isAddressTyped: true),
        ]

        // Two invented people sharing with the person: the first at "Where I
        // am and where I have been", the second at "Where I am now".
        static let historyPersonId = "sam"
        static let currentPersonId = "alex"

        /// One of their places, which belongs to them and not to the person.
        static func sharedPlaceJSON(userId: String, id: String, name: String, label: String, latitude: Double, longitude: Double) -> String {
            #"""
            {"id":"\#(id)","userId":"\#(userId)","placeName":"\#(name)","isNamedByPerson":true,"placeLabel":"\#(label)","latitudeDegrees":\#(latitude),"longitudeDegrees":\#(longitude),"radiusMeters":100,"osmType":"way","osmId":2001,"osmName":"\#(name)","osmCategory":"","addressText":"","isAddressTyped":false,"visitCount":12,"lastVisitAt":"\#(CalendarDates.text(Date()))","namingErrorMessage":"","createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-01T08:00:00Z"}
            """#
        }

        static var historyPersonSchool: String {
            sharedPlaceJSON(userId: historyPersonId, id: "place-sam-school", name: "Hillside School", label: "school", latitude: 10.5080, longitude: 20.4950)
        }

        static var historyPersonGym: String {
            sharedPlaceJSON(userId: historyPersonId, id: "place-sam-gym", name: "Harbor Gym", label: "gym", latitude: 10.4960, longitude: 20.5120)
        }

        static func sharedVisitJSON(id: String, placeJSON: String, latitude: Double, longitude: Double, arrivedAt: Date, departedAt: Date?) -> String {
            let departed = departedAt.map { #""\#(CalendarDates.text($0))""# } ?? "null"
            let duration = departedAt.map { String(Int($0.timeIntervalSince(arrivedAt))) } ?? "null"
            return #"""
                {"id":"\#(id)","arrivedAt":"\#(CalendarDates.text(arrivedAt))","departedAt":\#(departed),"durationSeconds":\#(duration),"latitudeDegrees":\#(latitude),"longitudeDegrees":\#(longitude),"accuracyMeters":30,"visitSource":"visit","place":\#(placeJSON)}
                """#
        }

        /// The moment the first person's day is told from: now, or 5 a.m.
        /// when the screenshots run just after midnight, so their visits and
        /// path always fall on today.
        static var historyPersonNow: Date {
            max(Date(), Calendar.current.startOfDay(for: Date()).addingTimeInterval(5 * 3600))
        }

        /// The first person's visits today, the one at the school still in progress.
        static var historyPersonVisits: [String] {
            let now = historyPersonNow
            return [
                sharedVisitJSON(
                    id: "visit-sam-1", placeJSON: historyPersonSchool, latitude: 10.5080, longitude: 20.4950,
                    arrivedAt: now.addingTimeInterval(-50 * 60), departedAt: nil),
                sharedVisitJSON(
                    id: "visit-sam-2", placeJSON: historyPersonGym, latitude: 10.4960, longitude: 20.5120,
                    arrivedAt: now.addingTimeInterval(-4 * 3600), departedAt: now.addingTimeInterval(-3 * 3600)),
            ]
        }

        /// The first person's path today, from the gym to the school, for
        /// the part of it inside the range asked for.
        static func historyPersonPoints(from: Date, until: Date) -> [String] {
            let start = historyPersonNow.addingTimeInterval(-3 * 3600)
            return (0..<30).compactMap { step in
                let fraction = Double(step) / 29
                let measuredAt = start.addingTimeInterval(Double(step) * 4 * 60)
                guard measuredAt >= from, measuredAt < until else { return nil }
                let latitude = 10.4960 + (10.5080 - 10.4960) * fraction
                let longitude = 20.5120 + (20.4950 - 20.5120) * fraction + sin(fraction * 5) * 0.0012
                return #"{"latitudeDegrees":\#(latitude),"longitudeDegrees":\#(longitude),"accuracyMeters":8,"altitudeMeters":null,"speedMetersPerSecond":1.3,"courseDegrees":null,"measuredAt":"\#(CalendarDates.text(measuredAt))","locationSource":"phone"}"#
            }
        }

        // `-TeaNodeScreen location-paused`: the person has paused too, with one
        // earlier pause among yesterday's visits, and the second person shared
        // with has paused. `location-settings-paused` is the settings while the
        // person's own pause is in effect.
        static var isPauseScreen: Bool { DemoServer.screen == "location-paused" }
        static var isOwnPauseInEffect: Bool { DemoServer.screen == "location-paused" || DemoServer.screen == "location-settings-paused" }

        static func pauseJSON(id: String, startedAt: Date, endedAt: Date?) -> String {
            let ended = endedAt.map { #""\#(CalendarDates.text($0))""# } ?? "null"
            return #"{"id":"\#(id)","startedAt":"\#(CalendarDates.text(startedAt))","endedAt":\#(ended)}"#
        }

        /// When the person's own pause in effect began: twenty minutes before
        /// the demo first asked, the same instant for every answer.
        static let ownPauseStartedAt = Date().addingTimeInterval(-20 * 60)

        /// The person's own pause in effect, to end in forty minutes.
        static var ownPauseJSON: String {
            pauseJSON(id: "pause-now", startedAt: ownPauseStartedAt, endedAt: ownPauseStartedAt.addingTimeInterval(60 * 60))
        }

        /// The second person's pause, which has no end.
        static var sharedPauseJSON: String {
            pauseJSON(id: "pause-alex", startedAt: Date().addingTimeInterval(-90 * 60), endedAt: nil)
        }

        /// The person's pauses: the one in effect, and one yesterday evening
        /// between the office and home.
        static var ownPausesJSON: [String] {
            guard isOwnPauseInEffect else { return [] }
            func at(_ hour: Int, _ minute: Int) -> Date {
                ServerTime.date(DemoServer.demoTime(daysAgo: 1, hour: hour, minute: minute)) ?? Date()
            }
            return [ownPauseJSON, pauseJSON(id: "pause-yesterday", startedAt: at(17, 55), endedAt: at(18, 40))]
        }

        // `-TeaNodeScreen location-person-muted`: the first person is chosen and
        // the person has muted their arrived and left notices until later today.
        static var isPersonMuteScreen: Bool { DemoServer.screen == "location-person-muted" }

        /// When the mute of the first person's notices ends: three hours after
        /// the demo first asked, the same instant for every answer.
        static let personMuteEndsAt = Date().addingTimeInterval(3 * 3600)

        static var personMuteJSON: String {
            #"{"mutedUntil":"\#(CalendarDates.text(personMuteEndsAt))"}"#
        }

        /// A person as `GetLocation` and `ListSharedLocations` give them.
        static func sharedPersonJSON(userId: String) -> String {
            let person = DemoServer.demoPerson(userId)
            if isPauseScreen, userId == currentPersonId {
                return #"""
                    {"userId":"\#(userId)","username":"\#(userId)","name":"\#(person.name)","avatarUrl":\#(person.avatarUrlJSON),"avatarColorIndex":\#(person.avatarColorIndex),"shareLevel":"current","latestPoint":null,"openVisit":null,"pause":\#(sharedPauseJSON)}
                    """#
            }
            let isHistory = userId == historyPersonId
            let latitude = isHistory ? 10.5080 : 10.5110
            let longitude = isHistory ? 20.4950 : 20.5210
            let measuredAt = Date().addingTimeInterval(isHistory ? -60 : -12 * 60)
            let point =
                #"{"latitudeDegrees":\#(latitude),"longitudeDegrees":\#(longitude),"accuracyMeters":12,"altitudeMeters":null,"speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"\#(CalendarDates.text(measuredAt))","locationSource":"phone"}"#
            let open = isHistory ? historyPersonVisits[0] : "null"
            let notificationMute = isHistory && isPersonMuteScreen ? personMuteJSON : "null"
            return #"""
                {"userId":"\#(userId)","username":"\#(userId)","name":"\#(person.name)","avatarUrl":\#(person.avatarUrlJSON),"avatarColorIndex":\#(person.avatarColorIndex),"shareLevel":"\#(isHistory ? "history" : "current")","latestPoint":\#(point),"openVisit":\#(open),"notificationMute":\#(notificationMute)}
                """#
        }

        /// The person's own shares: the family circle at the fuller level, and
        /// one person at the lesser.
        static var sharesJSON: String {
            #"""
            [{"id":"share-1","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"history","createdAt":"2026-09-02T08:00:00Z"},{"id":"share-2","granteeUserId":"alex","granteeUsername":"alex","granteeName":"Alex","granteeCircleId":null,"granteeCircleName":"","shareLevel":"current","createdAt":"2026-09-03T08:00:00Z"}]
            """#
        }

        static func place(_ id: String) -> Place { places.first { $0.id == id } ?? places[0] }

        struct Visit {
            let id: String
            let placeId: String
            let arrivedAt: Date?
            let departedAt: Date?
            /// The demo photos taken during the visit, and how many there are in all (more than are shown).
            var photoIds: [String] = []
            var photoCount = 0

            var json: String {
                let place = LocationDemo.place(placeId)
                let arrived = arrivedAt.map { #""\#(CalendarDates.text($0))""# } ?? "null"
                let departed = departedAt.map { #""\#(CalendarDates.text($0))""# } ?? "null"
                var duration = "null"
                if let arrivedAt, let departedAt { duration = String(Int(departedAt.timeIntervalSince(arrivedAt))) }
                return #"""
                    {"id":"\#(id)","arrivedAt":\#(arrived),"departedAt":\#(departed),"durationSeconds":\#(duration),"latitudeDegrees":\#(place.latitudeDegrees),"longitudeDegrees":\#(place.longitudeDegrees),"accuracyMeters":30,"visitSource":"visit","photoCount":\#(photoIds.isEmpty ? "null" : String(photoCount)),"photos":\#(photoIds.isEmpty ? "null" : DemoServer.demoVisitPhotosJSON(photoIds)),"photosTakenBefore":\#(photoIds.isEmpty ? "null" : departed),"place":\#(place.json)}
                    """#
            }
        }

        /// Two days of visits: today's, ending with the one in progress, and
        /// yesterday's. While the person's own pause is in effect, the visit in
        /// progress ended where the pause began, as the server ends it.
        static var visits: [Visit] {
            let now = Date()
            func at(_ daysAgo: Int, _ hour: Int, _ minute: Int) -> Date {
                ServerTime.date(DemoServer.demoTime(daysAgo: daysAgo, hour: hour, minute: minute)) ?? now
            }
            return [
                Visit(
                    id: "visit-1", placeId: parkPlaceId, arrivedAt: now.addingTimeInterval(-35 * 60),
                    departedAt: isOwnPauseInEffect ? ownPauseStartedAt : nil),
                Visit(
                    id: "visit-2", placeId: officePlaceId, arrivedAt: at(0, 9, 5), departedAt: at(0, 12, 30),
                    photoIds: ["demo-v1", "demo-v2", "demo-v3", "demo-v4"], photoCount: 7),
                Visit(
                    id: "visit-3", placeId: cafePlaceId, arrivedAt: at(0, 8, 5), departedAt: at(0, 8, 40), photoIds: ["demo-v8", "demo-v9"],
                    photoCount: 2),
                Visit(id: "visit-4", placeId: homePlaceId, arrivedAt: at(1, 19, 10), departedAt: at(0, 7, 50)),
                Visit(id: "visit-5", placeId: schoolPlaceId, arrivedAt: at(1, 15, 0), departedAt: at(1, 15, 20)),
                Visit(id: "visit-6", placeId: officePlaceId, arrivedAt: at(1, 9, 0), departedAt: at(1, 17, 45)),
                Visit(id: "visit-7", placeId: homePlaceId, arrivedAt: at(2, 18, 30), departedAt: at(1, 8, 25)),
            ]
        }

        /// A walk from home to the park, as positions a minute apart.
        static func points(from: Date, until: Date) -> [String] {
            let home = place(homePlaceId)
            let park = place(parkPlaceId)
            let start = Calendar.current.startOfDay(for: from).addingTimeInterval(7 * 3600 + 50 * 60)
            return (0..<24).compactMap { step in
                let fraction = Double(step) / 23
                let measuredAt = start.addingTimeInterval(Double(step) * 60)
                guard measuredAt >= from, measuredAt < until else { return nil }
                let latitude = home.latitudeDegrees + (park.latitudeDegrees - home.latitudeDegrees) * fraction
                let longitude = home.longitudeDegrees + (park.longitudeDegrees - home.longitudeDegrees) * fraction + sin(fraction * 6) * 0.0008
                return #"{"latitudeDegrees":\#(latitude),"longitudeDegrees":\#(longitude),"accuracyMeters":8,"altitudeMeters":null,"speedMetersPerSecond":1.3,"courseDegrees":null,"measuredAt":"\#(CalendarDates.text(measuredAt))","locationSource":"phone"}"#
            }
        }
    }

    extension DemoServer {
        /// What the Location screens ask, or nil for any other question.
        static func locationAnswer(query: String, body: Data?) -> String? {
            let variables = requestVariables(body)
            if let travelAnswer = travelAnswer(query: query, body: body) { return travelAnswer }
            // `-TeaNodeScreen location-setup`: the server has no position yet.
            let isSetupScreen = screen == "location-setup"
            if query.contains("GetLocation("), let userId = variables["userId"] as? String {
                return #"{"data":{"GetLocation":\#(LocationDemo.sharedPersonJSON(userId: userId))}}"#
            }
            if query.contains("ListSharedLocations") {
                // `-TeaNodeScreen location-people`: more people than fit under the map.
                let sharingIds =
                    screen == "location-people"
                    ? [LocationDemo.historyPersonId, LocationDemo.currentPersonId, "jordan", "casey", "morgan"]
                    : [LocationDemo.historyPersonId, LocationDemo.currentPersonId]
                let people = sharingIds.map(LocationDemo.sharedPersonJSON)
                return #"{"data":{"ListSharedLocations":[\#(people.joined(separator: ","))]}}"#
            }
            if query.contains("ListLocationPauses(") {
                // Another person's pauses: none to list.
                let pauses: [String] = variables["userId"] is String ? [] : LocationDemo.ownPausesJSON
                return #"{"data":{"ListLocationPauses":[\#(pauses.joined(separator: ","))]}}"#
            }
            if query.contains("PauseLocation(") { return #"{"data":{"PauseLocation":\#(LocationDemo.ownPauseJSON)}}"# }
            if query.contains("UnmuteLocationNotifications(") { return #"{"data":{"UnmuteLocationNotifications":true}}"# }
            if query.contains("MuteLocationNotifications(") {
                return #"{"data":{"MuteLocationNotifications":\#(LocationDemo.personMuteJSON)}}"#
            }
            if query.contains("ResumeLocation") { return #"{"data":{"ResumeLocation":true}}"# }
            if query.contains("ListLocationShares") {
                return #"{"data":{"ListLocationShares":\#(isSetupScreen ? "[]" : LocationDemo.sharesJSON)}}"#
            }
            if query.contains("ShareLocation(") {
                return
                    #"{"data":{"ShareLocation":{"id":"share-3","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"circle-home","granteeCircleName":"Home","shareLevel":"history","createdAt":"2026-09-04T08:00:00Z"}}}"#
            }
            if query.contains("UnshareLocation(") { return #"{"data":{"UnshareLocation":true}}"# }
            if query.contains("GetLocation("), isSetupScreen {
                return
                    #"{"data":{"GetLocation":{"userId":"robin","username":"robin","name":"Robin","avatarUrl":null,"avatarColorIndex":7,"shareLevel":"owner","latestPoint":null,"openVisit":null}}}"#
            }
            if query.contains("GetLocation(") {
                // While paused nothing newer than the pause's start is recorded,
                // and no visit is in progress.
                let isPaused = LocationDemo.isOwnPauseInEffect
                let open = isPaused ? "null" : LocationDemo.visits[0].json
                let park = LocationDemo.place(LocationDemo.parkPlaceId)
                let measuredAt = isPaused ? LocationDemo.ownPauseStartedAt.addingTimeInterval(-60) : Date()
                let point =
                    #"{"latitudeDegrees":\#(park.latitudeDegrees),"longitudeDegrees":\#(park.longitudeDegrees),"accuracyMeters":10,"altitudeMeters":null,"speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"\#(CalendarDates.text(measuredAt))","locationSource":"phone"}"#
                return
                    #"{"data":{"GetLocation":{"userId":"robin","username":"robin","name":"Robin","avatarUrl":null,"avatarColorIndex":7,"shareLevel":"owner","latestPoint":\#(point),"openVisit":\#(open),"pause":\#(isPaused ? LocationDemo.ownPauseJSON : "null")}}}"#
            }
            if query.contains("ListLocationVisits("), variables["userId"] as? String == LocationDemo.historyPersonId {
                var visits = LocationDemo.historyPersonVisits
                // Only today has visits; an earlier day has none.
                if let text = variables["from"] as? String, let from = ServerTime.date(text), from < Calendar.current.startOfDay(for: Date()) {
                    visits = []
                }
                return #"{"data":{"ListLocationVisits":{"placeVisits":[\#(visits.joined(separator: ","))],"nextOffset":null}}}"#
            }
            if query.contains("ListPlaces("), variables["userId"] as? String == LocationDemo.historyPersonId {
                return
                    #"{"data":{"ListPlaces":{"places":[\#(LocationDemo.historyPersonSchool),\#(LocationDemo.historyPersonGym)],"nextOffset":null}}}"#
            }
            if query.contains("ListPlaces("), query.contains("placeLabel"), variables["placeLabel"] as? String == "home" {
                let homes = isSetupScreen ? [] : [LocationDemo.place(LocationDemo.homePlaceId).json]
                return #"{"data":{"ListPlaces":{"places":[\#(homes.joined(separator: ","))],"nextOffset":null}}}"#
            }
            if query.contains("ListLocationVisits(") {
                var visits = LocationDemo.visits
                if let placeId = variables["placeId"] as? String { visits = visits.filter { $0.placeId == placeId } }
                if let text = variables["from"] as? String, let from = ServerTime.date(text) {
                    visits = visits.filter { ($0.arrivedAt ?? $0.departedAt ?? .distantPast) >= from }
                }
                if let text = variables["until"] as? String, let until = ServerTime.date(text) {
                    visits = visits.filter { ($0.arrivedAt ?? $0.departedAt ?? .distantFuture) < until }
                }
                return #"{"data":{"ListLocationVisits":{"placeVisits":[\#(visits.map(\.json).joined(separator: ","))],"nextOffset":null}}}"#
            }
            if query.contains("ListPlaces(") {
                var places = isSetupScreen ? LocationDemo.places.filter { $0.id != LocationDemo.homePlaceId } : LocationDemo.places
                if let placeLabel = variables["placeLabel"] as? String { places = places.filter { $0.label == placeLabel } }
                if let words = variables["query"] as? String { places = places.filter { $0.name.localizedCaseInsensitiveContains(words) } }
                return #"{"data":{"ListPlaces":{"places":[\#(places.map(\.json).joined(separator: ","))],"nextOffset":null}}}"#
            }
            if query.contains("ListNearbyPlaces(") {
                return
                    #"{"data":{"ListNearbyPlaces":[{"placeName":"Riverside Park","osmType":"way","osmId":1001,"osmCategory":"leisure=park","addressText":"","latitudeDegrees":10.5040,"longitudeDegrees":20.5060,"distanceMeters":35,"placeId":"place-park"},{"placeName":"Boat Hire","osmType":"node","osmId":1002,"osmCategory":"amenity=bicycle_rental","addressText":"","latitudeDegrees":10.5043,"longitudeDegrees":20.5064,"distanceMeters":70,"placeId":null},{"placeName":"Kiosk by the Bridge","osmType":"node","osmId":1003,"osmCategory":"shop=kiosk","addressText":"","latitudeDegrees":10.5038,"longitudeDegrees":20.5069,"distanceMeters":110,"placeId":null}]}}"#
            }
            if query.contains("SearchPlaces(") {
                return
                    #"{"data":{"SearchPlaces":{"foundPlaces":[{"placeName":"Riverside Park Kiosk","osmType":"node","osmId":1004,"osmCategory":"shop=kiosk","addressText":"","latitudeDegrees":10.5042,"longitudeDegrees":20.5062,"distanceMeters":55,"placeId":null},{"placeName":"Riverside Park","osmType":"way","osmId":1001,"osmCategory":"leisure=park","addressText":"","latitudeDegrees":10.5040,"longitudeDegrees":20.5060,"distanceMeters":35,"placeId":"place-park"}],"nextOffset":null}}}"#
            }
            if query.contains("SearchAddresses(") {
                return
                    #"{"data":{"SearchAddresses":[{"placeName":"12 Elm Street","osmType":"way","osmId":2001,"osmCategory":"building=house","addressText":"12 Elm Street, Exampleton","latitudeDegrees":10.5012,"longitudeDegrees":20.5031,"distanceMeters":12,"placeId":null,"placeLabel":"","isNamedByPerson":false},{"placeName":"12 Elm Street","osmType":"way","osmId":2002,"osmCategory":"building=apartments","addressText":"12 Elm Street, Sampleford","latitudeDegrees":11.2200,"longitudeDegrees":21.1100,"distanceMeters":41200,"placeId":null,"placeLabel":"","isNamedByPerson":false},{"placeName":"12 Elm Street","osmType":"way","osmId":2003,"osmCategory":"building=house","addressText":"12 Elm Street, Mockton","latitudeDegrees":9.8800,"longitudeDegrees":19.7700,"distanceMeters":63800,"placeId":null,"placeLabel":"","isNamedByPerson":false}]}}"#
            }
            if query.contains("ListLocationPoints(") {
                var from = Date.distantPast
                var until = Date.distantFuture
                if let text = variables["from"] as? String, let date = ServerTime.date(text) { from = date }
                if let text = variables["until"] as? String, let date = ServerTime.date(text) { until = date }
                let points =
                    variables["userId"] as? String == LocationDemo.historyPersonId
                    ? LocationDemo.historyPersonPoints(from: from, until: until) : LocationDemo.points(from: from, until: until)
                return #"{"data":{"ListLocationPoints":[\#(points.joined(separator: ","))]}}"#
            }
            if query.contains("SavePlace(") {
                let place = LocationDemo.place(variables["placeId"] as? String ?? LocationDemo.parkPlaceId)
                // An empty address text gives the address back to the map.
                let isAddressCleared = (variables["addressText"] as? String)?.isEmpty == true
                return #"{"data":{"SavePlace":\#(place.json(isAddressTyped: place.isAddressTyped && !isAddressCleared))}}"#
            }
            if query.contains("CheckIn(") { return #"{"data":{"CheckIn":\#(LocationDemo.visits[0].json)}}"# }
            if query.contains("ForgetPlaceVisit(") { return #"{"data":{"ForgetPlaceVisit":true}}"# }
            if query.contains("ForgetPlace(") { return #"{"data":{"ForgetPlace":true}}"# }
            if query.contains("EraseLocationHistory(") { return #"{"data":{"EraseLocationHistory":{"pointCount":0,"visitCount":0,"placeCount":0}}}"# }
            if query.contains("ReportLocation(") {
                return
                    #"{"data":{"ReportLocation":{"newPointCount":0,"newVisitCount":0,"updatedVisitCount":0,"pausedPointCount":0,"pausedVisitCount":0,"pause":null}}}"#
            }
            return nil
        }
    }
#endif
