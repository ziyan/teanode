import Foundation

// How a value of Health is written, the rules of the table at the top of
// internal/health/words.go (the one reference the command line, the agent's
// tool, the dashboard and this app copy). The rules that need no language are
// here, so that they are tested; the app puts the words that are a language's
// (steps, flights, hours) into the person's, around the numbers worked out
// here.

/// HealthCountNoun is what a count of a kind counts.
public enum HealthCountNoun: String, CaseIterable, Sendable {
    case step
    case flight
    case push
    case stroke
    case fall
    case puff
    case drink
}

/// HealthUnitWord is what follows a number.
public enum HealthUnitWord: Equatable, Sendable {
    /// A plain number.
    case none
    /// A symbol that touches the number: "97%", "36.6°C".
    case touching(String)
    /// A unit after a space: "74 bpm", "72.4 kg".
    case spaced(String)
    /// What is counted, in the singular when there is exactly one: "1 step",
    /// "8,412 steps".
    case noun(HealthCountNoun, isSingular: Bool)
}

/// HealthValueParts is a value split into its number as a person reads it and
/// the word that follows.
public struct HealthValueParts: Equatable, Sendable {
    public let numberText: String
    public let unit: HealthUnitWord

    public init(numberText: String, unit: HealthUnitWord) {
        self.numberText = numberText
        self.unit = unit
    }
}

/// HealthDurationParts is a length of time in the units it is written with:
/// "7 h 12 min", "35 min", "45 s".
public enum HealthDurationParts: Equatable, Sendable {
    case seconds(Int)
    case minutes(Int)
    case hours(Int)
    case hoursMinutes(hours: Int, minutes: Int)
}

/// HealthValueWords works out how numbers, times and facts about the person
/// are written.
public enum HealthValueWords {
    /// The kinds whose unit is a plain "count" that counts something.
    static let countNouns: [String: HealthCountNoun] = [
        "stepCount": .step, "flightsClimbed": .flight, "pushCount": .push, "swimmingStrokeCount": .stroke, "numberOfTimesFallen": .fall,
        "inhalerUsage": .puff, "numberOfAlcoholicBeverages": .drink,
    ]

    /// The kinds measured in "count/min" that are not beats.
    static let rateWords: [String: String] = ["cyclingCadence": "rpm", "respiratoryRate": "breaths/min"]

    /// HealthKit's unit strings as a person writes them; one not here is
    /// written as it is, and an empty word is a plain number.
    static let unitWords: [String: String] = [
        "degC": "°C", "degF": "°F", "ml/(kg*min)": "mL/kg·min", "kcal/(kg*hr)": "kcal/kg·hr", "dBASPL": "dB", "appleEffortScore": "",
    ]

    /// A distance from this many meters up is written in kilometers.
    static let kilometerFromMeters = 1000.0

    // MARK: Numbers

    /// How many decimals a number of this size is worth: none from 1,000, one
    /// from 100, else two.
    public static func precisionDigits(for value: Double) -> Int {
        let magnitude = abs(value)
        if magnitude >= 1000 { return 0 }
        if magnitude >= 100 { return 1 }
        return 2
    }

    /// A number with up to this many decimals (trailing zeros dropped) and
    /// thousands separated.
    public static func numberText(_ value: Double, maximumFractionDigits: Int, locale: Locale = .current) -> String {
        let text = value.formatted(.number.precision(.fractionLength(0...maximumFractionDigits)).locale(locale))
        // A tiny negative number is zero, not "-0".
        let zero = (0.0).formatted(.number.precision(.fractionLength(0...0)).locale(locale))
        return text == "-" + zero ? zero : text
    }

    /// A reading's number with as many decimals as its size is worth: 8,412,
    /// 150.3, 72.4, 0.35.
    public static func numberText(_ value: Double, locale: Locale = .current) -> String {
        numberText(value, maximumFractionDigits: precisionDigits(for: value), locale: locale)
    }

    /// A reading split into its number and its unit. HealthKit and the server
    /// keep a percentage as a fraction (0.975); it is written times 100.
    ///
    /// - Counts of something and rates are whole numbers ("74 bpm", "8,412
    ///   steps", and "1 step" for exactly one).
    /// - A count that counts nothing in particular keeps one decimal and drops
    ///   a trailing .0 (22.5, 3).
    /// - Meters from 1,000 are kilometers with one decimal and no trailing .0.
    public static func parts(healthType: String, value: Double, quantityUnit: String, locale: Locale = .current) -> HealthValueParts {
        let countNoun = countNouns[healthType]
        if quantityUnit == "m", abs(value) >= kilometerFromMeters {
            return HealthValueParts(
                numberText: numberText(value / kilometerFromMeters, maximumFractionDigits: 1, locale: locale), unit: .spaced("km"))
        }
        if quantityUnit == "%" {
            return HealthValueParts(numberText: numberText(value * 100, locale: locale), unit: .touching("%"))
        }
        if quantityUnit == "count/min" {
            let rounded = value.rounded()
            return HealthValueParts(
                numberText: numberText(rounded, maximumFractionDigits: 0, locale: locale), unit: .spaced(rateWords[healthType] ?? "bpm"))
        }
        if quantityUnit == "count", let countNoun {
            let rounded = value.rounded()
            return HealthValueParts(
                numberText: numberText(rounded, maximumFractionDigits: 0, locale: locale),
                unit: .noun(countNoun, isSingular: abs(rounded) == 1))
        }
        if quantityUnit == "count" {
            return HealthValueParts(numberText: numberText(value, maximumFractionDigits: 1, locale: locale), unit: .none)
        }
        let number = numberText(value, locale: locale)
        if quantityUnit.isEmpty { return HealthValueParts(numberText: number, unit: .none) }
        let word = unitWords[quantityUnit] ?? quantityUnit
        if word.isEmpty { return HealthValueParts(numberText: number, unit: .none) }
        return HealthValueParts(numberText: number, unit: word.hasPrefix("°") ? .touching(word) : .spaced(word))
    }

    /// The same, written in English the way the command line writes it: "8,412
    /// steps", "74 bpm", "97.5%".
    public static func englishText(healthType: String, value: Double, quantityUnit: String) -> String {
        let valueParts = parts(healthType: healthType, value: value, quantityUnit: quantityUnit, locale: Locale(identifier: "en_US"))
        switch valueParts.unit {
        case .none: return valueParts.numberText
        case .touching(let symbol): return valueParts.numberText + symbol
        case .spaced(let word): return valueParts.numberText + " " + word
        case .noun(let noun, let isSingular): return valueParts.numberText + " " + englishNoun(noun, isSingular: isSingular)
        }
    }

    private static func englishNoun(_ noun: HealthCountNoun, isSingular: Bool) -> String {
        if isSingular { return noun.rawValue }
        return noun == .push ? "pushes" : noun.rawValue + "s"
    }

    // MARK: Time

    /// A length of time in hours and minutes: seconds are rounded to the
    /// nearest second below a minute, and to the nearest minute from a
    /// minute on.
    public static func durationParts(seconds: Double) -> HealthDurationParts {
        let wholeSeconds = Int(seconds.rounded())
        if wholeSeconds < 60 { return .seconds(max(0, wholeSeconds)) }
        let totalMinutes = Int((seconds / 60).rounded())
        let hours = totalMinutes / 60
        let minutes = totalMinutes % 60
        if hours == 0 { return .minutes(minutes) }
        if minutes == 0 { return .hours(hours) }
        return .hoursMinutes(hours: hours, minutes: minutes)
    }

    // MARK: Facts about the person

    /// The age in whole years on `today` of someone born on a day written
    /// "YYYY-MM-DD"; nil if that is not a day or is after `today`.
    public static func ageInYears(dateOfBirth: String, today: Date, calendar: Calendar) -> Int? {
        guard HealthDays.isDayText(dateOfBirth), let year = Int(dateOfBirth.prefix(4)), let month = Int(dateOfBirth.dropFirst(5).prefix(2)),
            let day = Int(dateOfBirth.suffix(2)),
            DateComponents(calendar: calendar, year: year, month: month, day: day).isValidDate
        else { return nil }
        let now = calendar.dateComponents([.year, .month, .day], from: today)
        guard let nowYear = now.year, let nowMonth = now.month, let nowDay = now.day else { return nil }
        if (year, month, day) > (nowYear, nowMonth, nowDay) { return nil }
        var years = nowYear - year
        if (nowMonth, nowDay) < (month, day) { years -= 1 }
        return years
    }

    /// A blood type as it is written on a card: "aPositive" is "A+",
    /// "abNegative" is "AB-". Anything else is written as it was sent.
    public static func bloodTypeText(_ characteristicValue: String) -> String {
        let groups: [(prefix: String, letters: String)] = [("ab", "AB"), ("a", "A"), ("b", "B"), ("o", "O")]
        for group in groups {
            if characteristicValue == group.prefix + "Positive" { return group.letters + "+" }
            if characteristicValue == group.prefix + "Negative" { return group.letters + "-" }
        }
        return characteristicValue
    }

    /// The value a characteristic has when it was never set in Health.
    public static let notSetValue = "notSet"
}
