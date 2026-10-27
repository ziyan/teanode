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

    /// For a place that shows its label's word as its name, the name
    /// OpenStreetMap or the address gave, to say under it; nil for any other.
    var secondaryName: String? {
        guard !isNamedByPerson, label != nil else { return nil }
        if !placeName.isEmpty { return placeName }
        return addressText.isEmpty ? nil : addressText
    }

    /// The symbol that stands for it: its label's, else a map marker.
    var symbolName: String { label?.symbolName ?? "mappin" }

    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

extension FoundPlace {
    /// What the check-in sheet calls it, the same way a list of the person's
    /// places does: their own name, else for a labeled place the label's
    /// word, else the map name.
    var shownName: String {
        if !isNamedByPerson, let label { return label.title }
        return placeName
    }

    /// For a place that shows its label's word as its name, the map name or
    /// the address to say under it; nil for any other.
    var secondaryName: String? {
        guard !isNamedByPerson, label != nil else { return nil }
        if !placeName.isEmpty { return placeName }
        return addressText.isEmpty ? nil : addressText
    }

    /// The words saved as a place's address when the person picks this match:
    /// its address, or its name when the provider gave no address.
    var chosenAddressText: String { addressText.isEmpty ? placeName : addressText }

    /// The label's symbol for a labeled place of the person's, else nil.
    var labelSymbolName: String? { label?.symbolName }
}

extension LocationShareLevel {
    /// What a share at this level is called: the two plain choices.
    var title: String {
        switch self {
        case .current: String(localized: "Where I am now")
        case .history: String(localized: "Where I am and where I have been")
        case .owner: String(localized: "You")
        }
    }

    /// What somebody a location is shared at this level can see.
    var explanation: String {
        switch self {
        case .current: String(localized: "They see where you are now and the place you are at.")
        case .history: String(localized: "They also see your visits and the places you were at, including from before you shared.")
        case .owner: ""
        }
    }
}

extension PersonLocation {
    /// Where to draw them: their latest position, else the place of the visit
    /// in progress. Nowhere while somebody else has paused, whatever the
    /// phone last read: the person's own position is theirs to see.
    var coordinate: CLLocationCoordinate2D? {
        if level != .owner, isPaused() { return nil }
        return latestPoint?.coordinate ?? openVisit?.coordinate
    }
}

extension LocationPauseChoice {
    /// What the pause menu calls it.
    var title: String {
        switch self {
        case .anHour: String(localized: "For an hour")
        case .untilTomorrowMorning: String(localized: "Until tomorrow morning")
        case .untilResumed: String(localized: "Until I resume")
        }
    }
}

extension LocationPauseChoice {
    /// What the mute menu calls it: the same three choices as the pause, the
    /// last ending when the person unmutes.
    var muteTitle: String {
        switch self {
        case .anHour: String(localized: "For an hour")
        case .untilTomorrowMorning: String(localized: "Until tomorrow morning")
        case .untilResumed: String(localized: "Until I unmute")
        }
    }
}

extension PlaceVisit {
    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

extension LocationPoint {
    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

enum LocationText {
    /// A reading worse than this many meters is rough: the check-in sheet
    /// says so and "Use where I am now" refuses it.
    static let roughAccuracyMeters = 100.0

    /// How long a visit was, such as "1 hr, 25 min".
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

    /// When a pause ends, as the clock shows it: just the time when that is
    /// today, else with the day.
    static func pauseEnd(_ endDate: Date) -> String {
        if Calendar.current.isDateInToday(endDate) { return time(endDate) }
        return endDate.formatted(date: .abbreviated, time: .shortened)
    }

    /// The person's own pause: "Paused until 15:00", or until they resume.
    static func pausedUntil(_ pause: LocationPause) -> String {
        guard let endDate = pause.endedDate else { return String(localized: "Paused until you resume") }
        return String(localized: "Paused until \(pauseEnd(endDate))")
    }

    /// A person's notices the caller muted: "Notifications muted until 15:00",
    /// or without an end while the caller has to unmute.
    static func mutedUntil(_ mute: LocationNotificationMute) -> String {
        guard let endDate = mute.mutedUntilDate else { return String(localized: "Notifications muted") }
        return String(localized: "Notifications muted until \(pauseEnd(endDate))")
    }

    /// What somebody who paused reads as to the people who share with them.
    static func locationPaused(_ pause: LocationPause) -> String {
        guard let endDate = pause.endedDate else { return String(localized: "Location paused") }
        return String(localized: "Location paused until \(pauseEnd(endDate))")
    }

    /// When a pause began and ended, such as "14:00 to 15:00"; "Since 14:00"
    /// while it has no end. The end has its day only when it is not the day
    /// the pause began, as the row already sits under that day.
    static func times(of pause: LocationPause) -> String {
        switch (pause.startedDate, pause.endedDate) {
        case (let startedDate?, let endDate?) where Calendar.current.isDate(endDate, inSameDayAs: startedDate):
            String(localized: "\(time(startedDate)) to \(time(endDate))")
        case (let startedDate?, let endDate?):
            String(localized: "\(time(startedDate)) to \(endDate.formatted(date: .abbreviated, time: .shortened))")
        case (let startedDate?, nil): String(localized: "Since \(time(startedDate))")
        case (nil, _): ""
        }
    }

    /// The times of a visit and, when it is over, how long it was.
    static func summary(of visit: PlaceVisit) -> String {
        let timesText = times(of: visit)
        guard let visitDuration = visit.visitDuration else { return timesText }
        return String(localized: "\(timesText) · \(duration(visitDuration))")
    }

    /// Where somebody sharing is, under their name, as Find My says it: the
    /// place they are at and how long ago their latest position came ("Home ·
    /// 1 min. ago"), either one alone when that is all there is, else that
    /// there is no position yet ("No position recorded yet" on the person's
    /// own row).
    static func whereText(of person: PersonLocation, isOwn: Bool = false) -> String {
        if !isOwn, let pause = person.pause, pause.isInEffect(at: Date()) { return locationPaused(pause) }
        let placeName = person.openVisit?.place.shownName
        // A reading at or a moment ahead of this iPhone's clock reads as "1
        // sec. ago": the formatter calls a difference of zero "in 0 sec.".
        let agoText = person.latestPoint?.measuredDate.map {
            min($0, Date().addingTimeInterval(-1)).formatted(.relative(presentation: .numeric, unitsStyle: .abbreviated))
        }
        switch (placeName, agoText) {
        case let (placeName?, agoText?): return String(localized: "\(placeName) · \(agoText)")
        case let (placeName?, nil): return placeName
        case let (nil, agoText?): return agoText
        case (nil, nil): return isOwn ? String(localized: "No position recorded yet") : String(localized: "No position yet")
        }
    }

    /// How far somebody sharing is from the person: "Nearby" within 200 m,
    /// else the distance in the units the person chose; nil without both
    /// positions.
    static func distanceText(from own: PersonLocation?, to person: PersonLocation, measurementSystem: MeasurementSystem) -> String? {
        guard let ownPoint = own?.latestPoint, let point = person.latestPoint else { return nil }
        let meters = CLLocation(latitude: ownPoint.latitudeDegrees, longitude: ownPoint.longitudeDegrees)
            .distance(from: CLLocation(latitude: point.latitudeDegrees, longitude: point.longitudeDegrees))
        if meters < 200 { return String(localized: "Nearby") }
        return distance(meters, measurementSystem: measurementSystem)
    }

    /// "11.8 km" or "7.3 mi", in the units the person chose. Automatic follows
    /// the phone's language and region. Metric says meters under a kilometer
    /// and US says feet under a tenth of a mile, as a road sign would.
    static func distance(_ meters: Double, measurementSystem: MeasurementSystem) -> String {
        let distance = Measurement(value: meters, unit: UnitLength.meters)
        let fractionStyle: FloatingPointFormatStyle<Double> = .number.precision(.fractionLength(0...1))
        let wholeStyle: FloatingPointFormatStyle<Double> = .number.precision(.fractionLength(0))
        switch measurementSystem {
        case .automatic:
            return distance.formatted(.measurement(width: .abbreviated, usage: .road))
        case .metric:
            if meters < 1000 {
                return distance.formatted(.measurement(width: .abbreviated, usage: .asProvided, numberFormatStyle: wholeStyle))
            }
            return distance.converted(to: .kilometers).formatted(
                .measurement(width: .abbreviated, usage: .asProvided, numberFormatStyle: fractionStyle))
        case .us:
            if meters < 160.9344 {
                return distance.converted(to: .feet).formatted(
                    .measurement(width: .abbreviated, usage: .asProvided, numberFormatStyle: wholeStyle))
            }
            return distance.converted(to: .miles).formatted(
                .measurement(width: .abbreviated, usage: .asProvided, numberFormatStyle: fractionStyle))
        }
    }

    static func radius(_ meters: Double) -> String {
        Measurement(value: meters, unit: UnitLength.meters).formatted(.measurement(width: .abbreviated, usage: .asProvided))
    }

    /// "3 visits, last yesterday" for a place's row.
    static func visits(of place: LocationPlace) -> String {
        let visitCountText = String(localized: "\(place.visitCount) visits")
        guard let last = place.lastVisitDate else { return visitCountText }
        return String(localized: "\(visitCountText), last \(last.formatted(.relative(presentation: .named)))")
    }

    /// The kind of thing an OpenStreetMap tag names, in plain words: "cafe"
    /// for "amenity=cafe". Empty when there is none worth saying.
    static func category(_ osmCategory: String) -> String {
        let categoryKey = LocationWords.categoryValue(osmCategory)
        switch categoryKey {
        case "": return ""
        case "cafe": return String(localized: "place-category-cafe", defaultValue: "Cafe")
        case "restaurant": return String(localized: "place-category-restaurant", defaultValue: "Restaurant")
        case "fast_food": return String(localized: "place-category-fast-food", defaultValue: "Fast food")
        case "pub", "bar": return String(localized: "place-category-pub", defaultValue: "Bar")
        case "school", "kindergarten": return String(localized: "place-category-school", defaultValue: "School")
        case "university", "college": return String(localized: "place-category-university", defaultValue: "University")
        case "library": return String(localized: "place-category-library", defaultValue: "Library")
        case "park": return String(localized: "place-category-park", defaultValue: "Park")
        case "playground": return String(localized: "place-category-playground", defaultValue: "Playground")
        case "pitch", "sports_centre", "stadium": return String(localized: "place-category-pitch", defaultValue: "Sports")
        case "fitness_centre": return String(localized: "place-category-fitness-centre", defaultValue: "Gym")
        case "supermarket", "convenience", "greengrocer": return String(localized: "place-category-supermarket", defaultValue: "Grocery store")
        case "bakery": return String(localized: "place-category-bakery", defaultValue: "Bakery")
        case "clinic", "doctors", "dentist": return String(localized: "place-category-clinic", defaultValue: "Clinic")
        case "hospital": return String(localized: "place-category-hospital", defaultValue: "Hospital")
        case "pharmacy": return String(localized: "place-category-pharmacy", defaultValue: "Pharmacy")
        case "place_of_worship": return String(localized: "place-category-place-of-worship", defaultValue: "Place of worship")
        case "cinema": return String(localized: "place-category-cinema", defaultValue: "Cinema")
        case "museum": return String(localized: "place-category-museum", defaultValue: "Museum")
        case "bank": return String(localized: "place-category-bank", defaultValue: "Bank")
        case "post_office": return String(localized: "place-category-post-office", defaultValue: "Post office")
        case "fuel": return String(localized: "place-category-fuel", defaultValue: "Gas station")
        case "parking": return String(localized: "place-category-parking", defaultValue: "Parking")
        case "station", "bus_station": return String(localized: "place-category-station", defaultValue: "Station")
        case "hotel", "guest_house": return String(localized: "place-category-hotel", defaultValue: "Hotel")
        default:
            // A kind this app has no word for: OpenStreetMap's own, spelled out.
            let spelled = categoryKey.replacingOccurrences(of: "_", with: " ")
            return spelled.prefix(1).uppercased() + spelled.dropFirst()
        }
    }
}
