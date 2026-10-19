import SwiftUI
import TeaNodeKit

/// CalendarRepeatWords puts a repeat into the app's own localized sentences.
/// TeaNodeKit hands over the pieces with the locale's day names, their list
/// and the date already in them, so the order of the words is all that is
/// left to each language.
enum CalendarRepeatWords {
    /// The whole sentence: "Repeats every 2 weeks on Monday and Thursday,
    /// until December 31, 2026".
    static func sentenceText(_ sentence: CalendarRepeat.Sentence) -> String {
        let cadenceText = cadenceText(sentence.cadence)
        switch sentence.ending {
        case .never:
            return cadenceText
        case .onDate(let dateText):
            return String(localized: "\(cadenceText), until \(dateText)", comment: "A repeat that ends on a date, as \"Repeats every day, until December 31, 2026\": the repeat, then the date.")
        case .afterCount(let occurrenceCount):
            let timesText = String(localized: "\(occurrenceCount) times")
            return String(localized: "\(cadenceText), \(timesText)", comment: "A repeat that ends after some times, as \"Repeats every day, 10 times\": the repeat, then the count.")
        }
    }

    private static func cadenceText(_ cadence: CalendarRepeat.Sentence.Cadence) -> String {
        switch cadence {
        case .daily(let intervalCount):
            return intervalCount > 1 ? String(localized: "Repeats every \(intervalCount) days") : String(localized: "Repeats every day")
        case .yearly(let intervalCount):
            return intervalCount > 1 ? String(localized: "Repeats every \(intervalCount) years") : String(localized: "Repeats every year")
        case .weekly(let intervalCount, let weekdayNamesText):
            return intervalCount > 1
                ? String(localized: "Repeats every \(intervalCount) weeks on \(weekdayNamesText)", comment: "The days are a list of day names, as \"Monday and Thursday\".")
                : String(localized: "Repeats every week on \(weekdayNamesText)", comment: "The days are a list of day names, as \"Monday and Thursday\".")
        case .monthlyDay(let intervalCount, let dayOfMonth):
            return intervalCount > 1
                ? String(localized: "Repeats every \(intervalCount) months on day \(dayOfMonth)")
                : String(localized: "Repeats every month on day \(dayOfMonth)")
        case .monthlyWeekday(let intervalCount, let ordinal, let weekdayName):
            let ordinalText = ordinalName(ordinal)
            return intervalCount > 1
                ? String(localized: "Repeats every \(intervalCount) months on the \(ordinalText) \(weekdayName)", comment: "As \"Repeats every 2 months on the second Tuesday\": the ordinal, then the day's name.")
                : String(localized: "Repeats every month on the \(ordinalText) \(weekdayName)", comment: "As \"Repeats every month on the second Tuesday\": the ordinal, then the day's name.")
        }
    }

    /// The name of a place in the month, 1 to 5 or -1 for the last, as it
    /// stands in "the second Tuesday".
    static func ordinalName(_ ordinal: Int) -> String {
        switch ordinal {
        case 1: String(localized: "first", comment: "As in \"the first Monday of the month\".")
        case 2: String(localized: "second", comment: "As in \"the second Monday of the month\".")
        case 3: String(localized: "third", comment: "As in \"the third Monday of the month\".")
        case 4: String(localized: "fourth", comment: "As in \"the fourth Monday of the month\".")
        case 5: String(localized: "fifth", comment: "As in \"the fifth Monday of the month\".")
        default: String(localized: "last", comment: "As in \"the last Monday of the month\".")
        }
    }

    /// How often, in a few words, for the Repeat row of a custom repeat:
    /// "Every 2 weeks".
    static func shortText(_ parts: CalendarRepeat.Parts) -> String {
        let intervalCount = parts.intervalCount
        switch parts.frequency {
        case .daily: return intervalCount > 1 ? String(localized: "Every \(intervalCount) days") : String(localized: "Every day")
        case .weekly: return intervalCount > 1 ? String(localized: "Every \(intervalCount) weeks") : String(localized: "Every week")
        case .monthly: return intervalCount > 1 ? String(localized: "Every \(intervalCount) months") : String(localized: "Every month")
        case .yearly: return intervalCount > 1 ? String(localized: "Every \(intervalCount) years") : String(localized: "Every year")
        }
    }
}

/// CustomRepeatScreen is the screen behind Custom in an event's Repeat row,
/// laid out as the iPhone Calendar's: how often, the days, and when it stops,
/// with the sentence it makes under the first section. It writes the rule
/// into the event as each part changes, so going back is all it takes to
/// keep it.
struct CustomRepeatScreen: View {
    @Binding var rule: String
    let context: CalendarRepeat.Context
    /// The rule and its parts as the screen was opened on them, so that a
    /// rule this screen cannot show is kept exactly until something it does
    /// show is changed. State, so that the editor making the screen again
    /// as the rule changes does not replace them.
    @State private var originalRule: String
    @State private var initialParts: CalendarRepeat.Parts
    @State private var isRuleReadable: Bool
    @State private var parts: CalendarRepeat.Parts

    init(rule: Binding<String>, context: CalendarRepeat.Context) {
        _rule = rule
        self.context = context
        let readParts = CalendarRepeat.parse(rule.wrappedValue, context: context)
        let openedParts = readParts ?? CalendarRepeat.defaultParts(frequency: .weekly, context: context)
        _originalRule = State(initialValue: rule.wrappedValue)
        _initialParts = State(initialValue: openedParts)
        _isRuleReadable = State(initialValue: readParts != nil || rule.wrappedValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        _parts = State(initialValue: openedParts)
    }

    var body: some View {
        Form {
            howOftenSection
            if parts.frequency == .weekly {
                weekdaysSection
            }
            if parts.frequency == .monthly {
                monthlySection
            }
            endSection
        }
        .navigationTitle("Custom")
        .navigationBarTitleDisplayMode(.inline)
        .onChange(of: parts) { _, editedParts in
            rule = CalendarRepeat.editedRule(original: originalRule, initial: initialParts, edited: editedParts, context: context)
        }
    }

    // MARK: How often

    private var howOftenSection: some View {
        Section {
            Picker("Frequency", selection: $parts.frequency) {
                Text("Every day").tag(CalendarRepeat.Frequency.daily)
                Text("Every week").tag(CalendarRepeat.Frequency.weekly)
                Text("Every month").tag(CalendarRepeat.Frequency.monthly)
                Text("Every year").tag(CalendarRepeat.Frequency.yearly)
            }
            // The whole phrase as the stepper's label: a title and a value beside
            // the stepper were squeezed until the largest text broke the word.
            Stepper(value: $parts.intervalCount, in: 1...999) {
                Text(verbatim: CalendarRepeatWords.shortText(parts))
            }
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text(verbatim: sentenceText)
                if isShowingKeptNote {
                    Text("Set in another program. It stays as it is unless you change it here.")
                }
            }
        }
    }

    private var sentenceText: String {
        CalendarRepeatWords.sentenceText(CalendarRepeat.sentence(for: parts))
    }

    /// Whether to say that the rule was set elsewhere and is kept: it has a
    /// part this screen cannot show, or it could not be read at all, and
    /// nothing has been changed yet.
    private var isShowingKeptNote: Bool {
        (initialParts.hasUnshownParts || !isRuleReadable) && parts == initialParts
    }

    // MARK: The days of a week

    private var weekdaysSection: some View {
        Section {
            WeekdayChecklist(dayNumbers: weekdayNumbersBinding, isEmptyAllowed: false)
        } header: {
            Text("Repeat on")
        }
    }

    private var weekdayNumbersBinding: Binding<Set<Int>> {
        Binding(
            get: { Set(parts.weekdays) },
            set: { parts.weekdays = $0.sorted() })
    }

    // MARK: The day of a month

    private enum MonthlyKind: Hashable {
        case dayOfMonth, ordinalWeekday
    }

    private var monthlySection: some View {
        Section {
            Picker("Repeat by", selection: monthlyKindBinding) {
                Text("Day of the month").tag(MonthlyKind.dayOfMonth)
                Text("Weekday").tag(MonthlyKind.ordinalWeekday)
            }
            if monthlyKindBinding.wrappedValue == .dayOfMonth {
                Picker("Day", selection: dayOfMonthBinding) {
                    ForEach(1...31, id: \.self) { day in
                        Text(day, format: .number).tag(day)
                    }
                }
            } else {
                Picker("Which", selection: ordinalBinding) {
                    ForEach(CalendarRepeat.monthlyOrdinals, id: \.self) { value in
                        Text(verbatim: CalendarRepeatWords.ordinalName(value)).tag(value)
                    }
                }
                Picker("Weekday", selection: weekdayBinding) {
                    ForEach(ScheduleWeekday.weekOrder(firstWeekday: Calendar.current.firstWeekday), id: \.self) { dayNumber in
                        Text(verbatim: ScheduleWeekday.name(dayNumber)).tag(dayNumber)
                    }
                }
            }
        } header: {
            Text("Repeat on")
        }
    }

    private var monthlyKindBinding: Binding<MonthlyKind> {
        Binding(
            get: { () -> MonthlyKind in
                if case .ordinalWeekday = parts.monthly { return .ordinalWeekday }
                return .dayOfMonth
            },
            set: { kind in
                switch kind {
                case .dayOfMonth:
                    parts.monthly = .dayOfMonth(context.startDay.day)
                case .ordinalWeekday:
                    parts.monthly = .ordinalWeekday(ordinal: CalendarRepeat.ordinal(of: context.startDay), weekday: CalendarRepeat.weekday(of: context.startDay))
                }
            })
    }

    private var dayOfMonthBinding: Binding<Int> {
        Binding(
            get: { () -> Int in
                if case .dayOfMonth(let chosenDay) = parts.monthly { return chosenDay }
                return context.startDay.day
            },
            set: { parts.monthly = .dayOfMonth($0) })
    }

    private var ordinalBinding: Binding<Int> {
        Binding(
            get: { () -> Int in
                if case .ordinalWeekday(let chosenOrdinal, _) = parts.monthly { return chosenOrdinal }
                return CalendarRepeat.ordinal(of: context.startDay)
            },
            set: { parts.monthly = .ordinalWeekday(ordinal: $0, weekday: weekdayBinding.wrappedValue) })
    }

    private var weekdayBinding: Binding<Int> {
        Binding(
            get: { () -> Int in
                if case .ordinalWeekday(_, let chosenWeekday) = parts.monthly { return chosenWeekday }
                return CalendarRepeat.weekday(of: context.startDay)
            },
            set: { parts.monthly = .ordinalWeekday(ordinal: ordinalBinding.wrappedValue, weekday: $0) })
    }

    // MARK: The end

    private enum EndKind: Hashable {
        case never, onDate, afterCount
    }

    private var endSection: some View {
        Section {
            Picker("End repeat", selection: endKindBinding) {
                Text("Never").tag(EndKind.never)
                Text("On date").tag(EndKind.onDate)
                Text("After a number of times").tag(EndKind.afterCount)
            }
            if endKindBinding.wrappedValue == .onDate {
                DatePicker("Last day", selection: endDateBinding, in: firstEndDate..., displayedComponents: .date)
            }
            if endKindBinding.wrappedValue == .afterCount {
                Stepper(value: occurrenceCountBinding, in: 1...999) {
                    Text(verbatim: occurrenceCountText)
                }
            }
        } header: {
            Text("End repeat")
        }
    }

    private var endKindBinding: Binding<EndKind> {
        Binding(
            get: { () -> EndKind in
                switch parts.end {
                case .never: return .never
                case .onDate: return .onDate
                case .afterCount: return .afterCount
                }
            },
            set: { kind in
                switch kind {
                case .never: parts.end = .never
                case .onDate: parts.end = .onDate(CalendarRepeat.dayOneYearAfter(context.startDay))
                case .afterCount: parts.end = .afterCount(occurrenceCount: 10)
                }
            })
    }

    /// The first day a repeat may end on: the day the event starts.
    private var firstEndDate: Date {
        context.startDay.firstMoment(in: .current) ?? Date.distantPast
    }

    private var endDateBinding: Binding<Date> {
        Binding(
            get: { () -> Date in
                if case .onDate(let endDay) = parts.end { return endDay.firstMoment(in: .current) ?? firstEndDate }
                return firstEndDate
            },
            set: { parts.end = .onDate(CalendarDay(containing: $0, in: .current)) })
    }

    private var occurrenceCountText: String {
        String(localized: "\(occurrenceCountBinding.wrappedValue) times")
    }

    private var occurrenceCountBinding: Binding<Int> {
        Binding(
            get: { () -> Int in
                if case .afterCount(let chosenCount) = parts.end { return chosenCount }
                return 10
            },
            set: { parts.end = .afterCount(occurrenceCount: $0) })
    }
}
