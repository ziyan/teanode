import CoreLocation
import Foundation
import SwiftUI
import TeaNodeKit

// The words the Location screens say, in the person's language.

extension PlaceLabel {
    /// The label's word. Written under keys of its own: "Home" is already a
    /// word of the app with another meaning in Chinese.
    var title: String {
        switch self {
        case .home: String(localized: "place-label-home", defaultValue: "Home")
        case .work: String(localized: "place-label-work", defaultValue: "Work")
        case .school: String(localized: "place-label-school", defaultValue: "School")
        case .family: String(localized: "place-label-family", defaultValue: "Family")
        case .gym: String(localized: "place-label-gym", defaultValue: "Gym")
        }
    }
}

extension LocationPlace {
    /// What a list calls it: the name the person gave, else for a labeled
    /// place the label's word, else the name OpenStreetMap or the address
    /// gave.
    var shownName: String {
        if !isNamedByPerson, let label { return label.title }
        if !placeName.isEmpty { return placeName }
        if !addressText.isEmpty { return addressText }
        return String(localized: "A place")
    }

    /// The symbol that stands for it: its label's, else a pin.
    var symbolName: String { label?.symbolName ?? "mappin" }

    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

extension PlaceVisit {
    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

extension LocationPoint {
    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

enum LocationText {
    /// How long a stay was, such as "1 hr, 25 min".
    static func duration(_ duration: Duration) -> String {
        if duration < .seconds(60) { return String(localized: "Under a minute") }
        return duration.formatted(.units(allowed: [.days, .hours, .minutes], width: .abbreviated, maximumUnitCount: 2))
    }

    static func time(_ date: Date) -> String {
        date.formatted(date: .omitted, time: .shortened)
    }

    /// A day as a heading: "Today", "Yesterday", else "Thursday, October 15".
    static func dayTitle(_ day: Date) -> String {
        let calendar = Calendar.current
        if calendar.isDateInToday(day) { return String(localized: "Today") }
        if calendar.isDateInYesterday(day) { return String(localized: "Yesterday") }
        return day.formatted(.dateTime.weekday(.wide).month(.wide).day())
    }

    /// When a visit began and ended, such as "14:05 to 15:30"; "Since 14:05"
    /// while the person is still there.
    static func times(of visit: PlaceVisit) -> String {
        switch (visit.arrivedDate, visit.departedDate) {
        case (let arrived?, let departed?): String(localized: "\(time(arrived)) to \(time(departed))")
        case (let arrived?, nil): String(localized: "Since \(time(arrived))")
        case (nil, let departed?): String(localized: "Until \(time(departed))")
        case (nil, nil): ""
        }
    }

    /// The times of a visit and, when it is over, how long it was.
    static func summary(of visit: PlaceVisit) -> String {
        let timesText = times(of: visit)
        guard let stay = visit.stayDuration else { return timesText }
        return String(localized: "\(timesText) · \(duration(stay))")
    }

    /// "At Home since 14:05", for the visit in progress.
    static func current(_ visit: PlaceVisit) -> String {
        let name = visit.place.shownName
        if let arrived = visit.arrivedDate { return String(localized: "At \(name) since \(time(arrived))") }
        return String(localized: "At \(name)")
    }

    static func distance(_ meters: Double) -> String {
        Measurement(value: meters, unit: UnitLength.meters).formatted(.measurement(width: .abbreviated, usage: .road))
    }

    static func radius(_ meters: Double) -> String {
        Measurement(value: meters, unit: UnitLength.meters).formatted(.measurement(width: .abbreviated, usage: .asProvided))
    }

    /// "3 visits, last yesterday" for a place's row.
    static func visits(of place: LocationPlace) -> String {
        let count = String(localized: "\(place.visitCount) visits")
        guard let last = place.lastVisitDate else { return count }
        return String(localized: "\(count), last \(last.formatted(.relative(presentation: .named)))")
    }

    /// The kind of thing an OpenStreetMap tag names, in plain words: "cafe"
    /// for "amenity=cafe". Empty when there is none worth saying.
    static func category(_ osmCategory: String) -> String {
        let value = LocationWords.categoryValue(osmCategory)
        switch value {
        case "": return ""
        case "cafe": return String(localized: "Cafe")
        case "restaurant": return String(localized: "Restaurant")
        case "fast_food": return String(localized: "Fast food")
        case "pub", "bar": return String(localized: "Bar")
        case "school", "kindergarten": return String(localized: "School")
        case "university", "college": return String(localized: "University")
        case "library": return String(localized: "Library")
        case "park": return String(localized: "Park")
        case "playground": return String(localized: "Playground")
        case "pitch", "sports_centre", "stadium": return String(localized: "Sports")
        case "fitness_centre": return String(localized: "Gym")
        case "supermarket", "convenience", "greengrocer": return String(localized: "Grocery store")
        case "bakery": return String(localized: "Bakery")
        case "clinic", "doctors", "dentist": return String(localized: "Clinic")
        case "hospital": return String(localized: "Hospital")
        case "pharmacy": return String(localized: "Pharmacy")
        case "place_of_worship": return String(localized: "Place of worship")
        case "cinema": return String(localized: "Cinema")
        case "museum": return String(localized: "Museum")
        case "bank": return String(localized: "Bank")
        case "post_office": return String(localized: "Post office")
        case "fuel": return String(localized: "Gas station")
        case "parking": return String(localized: "Parking")
        case "station", "bus_station": return String(localized: "Station")
        case "hotel", "guest_house": return String(localized: "Hotel")
        default:
            // A kind this app has no word for: OpenStreetMap's own, spelled out.
            let spelled = value.replacingOccurrences(of: "_", with: " ")
            return spelled.prefix(1).uppercased() + spelled.dropFirst()
        }
    }
}
