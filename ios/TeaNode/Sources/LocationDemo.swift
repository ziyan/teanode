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

            var json: String {
                let lastVisit = DemoServer.demoTime(daysAgo: lastVisitDaysAgo, hour: 8, minute: 5)
                return #"""
                    {"id":"\#(id)","userId":"robin","placeName":"\#(name)","isNamedByPerson":\#(isNamedByPerson),"placeLabel":"\#(label)","latitudeDegrees":\#(latitudeDegrees),"longitudeDegrees":\#(longitudeDegrees),"radiusMeters":\#(radiusMeters),"osmType":"way","osmId":1001,"osmName":"\#(name)","osmCategory":"\#(osmCategory)","addressText":"","visitCount":\#(visitCount),"lastVisitAt":"\#(lastVisit)","namingErrorMessage":"","createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-01T08:00:00Z"}
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
                radiusMeters: 60, osmCategory: "amenity=cafe", visitCount: 5, lastVisitDaysAgo: 0),
        ]

        static func place(_ id: String) -> Place { places.first { $0.id == id } ?? places[0] }

        struct Visit {
            let id: String
            let placeId: String
            let arrivedAt: Date?
            let departedAt: Date?

            var json: String {
                let place = LocationDemo.place(placeId)
                let arrived = arrivedAt.map { #""\#(CalendarDates.text($0))""# } ?? "null"
                let departed = departedAt.map { #""\#(CalendarDates.text($0))""# } ?? "null"
                var duration = "null"
                if let arrivedAt, let departedAt { duration = String(Int(departedAt.timeIntervalSince(arrivedAt))) }
                return #"""
                    {"id":"\#(id)","arrivedAt":\#(arrived),"departedAt":\#(departed),"durationSeconds":\#(duration),"latitudeDegrees":\#(place.latitudeDegrees),"longitudeDegrees":\#(place.longitudeDegrees),"accuracyMeters":30,"visitSource":"visit","place":\#(place.json)}
                    """#
            }
        }

        /// Two days of visits: today's, ending with the one in progress, and
        /// yesterday's.
        static var visits: [Visit] {
            let now = Date()
            func at(_ daysAgo: Int, _ hour: Int, _ minute: Int) -> Date {
                ServerTime.date(DemoServer.demoTime(daysAgo: daysAgo, hour: hour, minute: minute)) ?? now
            }
            return [
                Visit(id: "visit-1", placeId: parkPlaceId, arrivedAt: now.addingTimeInterval(-35 * 60), departedAt: nil),
                Visit(id: "visit-2", placeId: officePlaceId, arrivedAt: at(0, 9, 5), departedAt: at(0, 12, 30)),
                Visit(id: "visit-3", placeId: cafePlaceId, arrivedAt: at(0, 8, 5), departedAt: at(0, 8, 40)),
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
            if query.contains("GetLocation(") {
                let open = LocationDemo.visits[0]
                let park = LocationDemo.place(LocationDemo.parkPlaceId)
                let point =
                    #"{"latitudeDegrees":\#(park.latitudeDegrees),"longitudeDegrees":\#(park.longitudeDegrees),"accuracyMeters":10,"altitudeMeters":null,"speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"\#(CalendarDates.text(Date()))","locationSource":"phone"}"#
                return
                    #"{"data":{"GetLocation":{"userId":"robin","username":"robin","name":"Robin","avatarUrl":null,"avatarColorIndex":7,"locationShareLevel":"owner","latestPoint":\#(point),"openVisit":\#(open.json)}}}"#
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
                return #"{"data":{"ListPlaces":{"places":[\#(LocationDemo.places.map(\.json).joined(separator: ","))],"nextOffset":null}}}"#
            }
            if query.contains("ListNearbyPlaces(") {
                return
                    #"{"data":{"ListNearbyPlaces":[{"placeName":"Riverside Park","osmType":"way","osmId":1001,"osmCategory":"leisure=park","addressText":"","latitudeDegrees":10.5040,"longitudeDegrees":20.5060,"distanceMeters":35,"placeId":"place-park"},{"placeName":"Boat Hire","osmType":"node","osmId":1002,"osmCategory":"amenity=bicycle_rental","addressText":"","latitudeDegrees":10.5043,"longitudeDegrees":20.5064,"distanceMeters":70,"placeId":null},{"placeName":"Kiosk by the Bridge","osmType":"node","osmId":1003,"osmCategory":"shop=kiosk","addressText":"","latitudeDegrees":10.5038,"longitudeDegrees":20.5069,"distanceMeters":110,"placeId":null}]}}"#
            }
            if query.contains("ListLocationPoints(") {
                var from = Date.distantPast
                var until = Date.distantFuture
                if let text = variables["from"] as? String, let date = ServerTime.date(text) { from = date }
                if let text = variables["until"] as? String, let date = ServerTime.date(text) { until = date }
                return #"{"data":{"ListLocationPoints":[\#(LocationDemo.points(from: from, until: until).joined(separator: ","))]}}"#
            }
            if query.contains("SavePlace(") {
                let place = LocationDemo.place(variables["placeId"] as? String ?? LocationDemo.parkPlaceId)
                return #"{"data":{"SavePlace":\#(place.json)}}"#
            }
            if query.contains("CheckIn(") { return #"{"data":{"CheckIn":\#(LocationDemo.visits[0].json)}}"# }
            if query.contains("ForgetPlace(") { return #"{"data":{"ForgetPlace":true}}"# }
            if query.contains("ForgetLocationHistory(") { return #"{"data":{"ForgetLocationHistory":{"pointCount":0,"visitCount":0,"placeCount":0}}}"# }
            if query.contains("ReportLocation(") {
                return #"{"data":{"ReportLocation":{"newPointCount":0,"newVisitCount":0,"updatedVisitCount":0}}}"#
            }
            return nil
        }
    }
#endif
