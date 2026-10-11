import SwiftUI
import TeaNodeKit

/// CalendarScreen is the dashboard's Calendar page on a phone. It opens on
/// the months scrolling one after another, as Apple's Calendar does, and a
/// tap on a day opens that day as hours. The buttons at the bottom right
/// switch to the agenda, a day, a week or five days as a list of days, and
/// the reminders.
struct CalendarScreen: View {
    @State private var model: CalendarModel
    @State private var draft: EventDraft?
    @State private var opened: CalendarEvent?
    @State private var reminders: RemindersModel
    @State private var isAddingReminder = false
    @State private var isSearching = false
    /// The days opened from the month, pushed as Apple's Calendar pushes them.
    @State private var path: [CalendarDay] = []
    @State private var monthPosition: ScrollPosition
    /// The month at the top of the month view, whose year is the title.
    @State private var topMonth: CalendarMonth
    @Environment(\.accessibilityReduceMotion) private var isReduceMotionOn

    init(connection: Connection, username: String) {
        _model = State(initialValue: CalendarModel(connection: connection, username: username))
        _reminders = State(initialValue: RemindersModel(connection: connection))
        let currentMonth = CalendarMonth(containing: Date(), in: .current)
        _monthPosition = State(initialValue: ScrollPosition(id: currentMonth, anchor: .top))
        _topMonth = State(initialValue: currentMonth)
    }

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(model.viewMode == .month ? .inline : .large)
                .withoutSwipeBack()
                .toolbar { toolbar }
                .refreshable {
                    if model.viewMode == .reminders {
                        await reminders.load()
                    } else {
                        await model.reload()
                    }
                }
                .navigationDestination(for: CalendarDay.self) { day in
                    CalendarDayScreen(model: model, day: day) { opened = $0 } onCreate: { start in
                        draft = EventDraft(startingAt: start)
                    }
                }
        }
        .task {
            await model.load()
            #if DEBUG
                // The screenshots photograph a day opened from the month.
                if DemoServer.screen == "calendar-day", path.isEmpty {
                    path = [CalendarDay(containing: Date(), in: model.dates)]
                }
            #endif
        }
        .sheet(item: $draft) { draft in
            EventEditor(model: model, draft: draft)
        }
        .sheet(item: $opened) { event in
            EventOpener(model: model, event: event)
        }
        .sheet(isPresented: $isAddingReminder) {
            ReminderEditor(model: reminders, reminder: nil)
        }
        .sheet(isPresented: $isSearching) {
            CalendarSearch(model: model)
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }

    @ViewBuilder
    private var content: some View {
        switch model.viewMode {
        case .month:
            MonthScroll(model: model, position: $monthPosition, topMonth: $topMonth) { day in
                path.append(day)
            }
        case .week, .workweek:
            List {
                PendingChangeSection(model: model)
                ForEach(model.days, id: \.self) { day in
                    DaySection(model: model, day: day, isEmptyShown: true) { opened = $0 }
                }
            }
        case .day:
            DayTimeline(model: model, day: model.focus) { opened = $0 } onCreate: { start in
                draft = EventDraft(startingAt: start)
            }
        case .agenda:
            let days = model.days.filter { !model.events(on: $0).isEmpty }
            List {
                PendingChangeSection(model: model)
                ForEach(days, id: \.self) { day in
                    DaySection(model: model, day: day, isEmptyShown: false) { opened = $0 }
                }
            }
            .overlay {
                if days.isEmpty, !model.isLoading {
                    ContentUnavailableView("Nothing in the next month", systemImage: "calendar")
                }
            }
        case .reminders:
            RemindersList(model: reminders)
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        MenuButton()
        // Search and add at the top right, as Apple's Calendar has them.
        ToolbarItemGroup(placement: .topBarTrailing) {
            if model.viewMode != .reminders {
                Button {
                    isSearching = true
                } label: {
                    Label("Search events", systemImage: "magnifyingglass")
                }
            }
            Button {
                if model.viewMode == .reminders {
                    isAddingReminder = true
                } else {
                    draft = EventDraft(startingAt: Self.nextHour(on: model.focus))
                }
            } label: {
                Label(model.viewMode == .reminders ? String(localized: "New reminder") : String(localized: "New event"), systemImage: "plus")
            }
            .disabled(model.viewMode != .reminders && model.calendar == nil)
        }
        // Today at the bottom left and the views at the bottom right, so the
        // months have the top to themselves.
        if model.viewMode != .reminders {
            ToolbarItem(placement: .bottomBar) {
                Button("Today") {
                    goToToday()
                }
            }
            if model.viewMode != .month {
                ToolbarSpacer(.fixed, placement: .bottomBar)
                ToolbarItemGroup(placement: .bottomBar) {
                    Button {
                        model.step(-1)
                    } label: {
                        Label("Earlier", systemImage: "chevron.backward")
                    }
                    Button {
                        model.step(1)
                    } label: {
                        Label("Later", systemImage: "chevron.forward")
                    }
                }
            }
        }
        ToolbarSpacer(.flexible, placement: .bottomBar)
        ToolbarItemGroup(placement: .bottomBar) {
            modeToggle(.agenda)
            modeToggle(.day)
            Menu {
                Picker("View", selection: $model.viewMode) {
                    ForEach(CalendarModel.ViewMode.allCases) { mode in
                        Label(mode.title, systemImage: mode.systemImage).tag(mode)
                    }
                }
            } label: {
                Label("More views", systemImage: "ellipsis")
            }
        }
    }

    /// A view the bottom bar switches to, shown pressed while it is on; a
    /// second press goes back to the month.
    private func modeToggle(_ mode: CalendarModel.ViewMode) -> some View {
        Toggle(
            isOn: Binding(
                get: { model.viewMode == mode },
                set: { isOn in model.viewMode = isOn ? mode : .month })
        ) {
            Label(mode.title, systemImage: mode.systemImage)
        }
        .toggleStyle(.button)
    }

    /// Back to today: the current month at the top of the month view, or
    /// today's day, week or agenda.
    private func goToToday() {
        let today = Calendar.current.startOfDay(for: Date())
        model.focus = today
        if model.viewMode == .month {
            withAnimation(isReduceMotionOn ? nil : .default) {
                monthPosition.scrollTo(id: CalendarMonth(containing: today, in: model.dates), anchor: .top)
            }
        }
    }

    private var title: String {
        switch model.viewMode {
        case .month:
            return topMonth.firstMoment(in: model.dates)?.formatted(.dateTime.year()) ?? ""
        case .week, .workweek:
            let (from, until) = model.window
            let last = model.dates.date(byAdding: .day, value: -1, to: until) ?? until
            return "\(from.formatted(.dateTime.month(.abbreviated).day())) – \(last.formatted(.dateTime.month(.abbreviated).day()))"
        case .day:
            return model.focus.formatted(.dateTime.weekday(.wide).month(.abbreviated).day())
        case .agenda:
            return String(localized: "Agenda")
        case .reminders:
            return String(localized: "Reminders")
        }
    }

    /// A new event starts at the next hour on the day being looked at.
    static func nextHour(on day: Date) -> Date {
        let dates = Calendar.current
        let hour = dates.component(.hour, from: Date()) + 1
        let start = dates.startOfDay(for: day)
        return dates.date(byAdding: .hour, value: min(hour, 23), to: start) ?? start
    }
}

extension CalendarModel.ViewMode {
    var title: String {
        switch self {
        case .month: String(localized: "Month")
        case .week: String(localized: "Week")
        case .workweek: String(localized: "5 days")
        case .day: String(localized: "Day")
        case .agenda: String(localized: "Agenda")
        case .reminders: String(localized: "Reminders")
        }
    }

    var systemImage: String {
        switch self {
        case .month: "calendar"
        case .week: "calendar.day.timeline.left"
        case .workweek: "briefcase"
        case .day: "clock"
        case .agenda: "list.bullet"
        case .reminders: "checklist"
        }
    }
}

extension CalendarModel {
    /// A few system colors for calendars the server keeps no colour for.
    /// Green is left out: in this app green is what can be pressed.
    static let palette: [Color] = [.blue, .orange, .purple, .teal, .indigo, .pink, .brown, .cyan]

    /// The color an event is painted: its calendar's own when the server
    /// keeps one, or else a color of the palette, always the same one for
    /// the same calendar.
    func color(of event: CalendarEvent) -> Color {
        if event.calendarId == calendar?.id, let parts = CalendarColorComponents(hexText: calendar?.colour) {
            return Color(red: parts.red, green: parts.green, blue: parts.blue)
        }
        return Self.palette[CalendarPalette.index(calendarId: event.calendarId, paletteCount: Self.palette.count)]
    }
}

extension LunarDates {
    /// Lunar dates are shown under the days when the app speaks Chinese, as
    /// calendars printed in China show them.
    static var isShownForAppLanguage: Bool {
        Bundle.main.preferredLocalizations.first?.hasPrefix("zh") == true
    }
}

/// MonthScroll is the month view: months one after another down the
/// screen, ten years either way, each read from the server as it comes into
/// view. The weekdays stay at the top.
struct MonthScroll: View {
    let model: CalendarModel
    @Binding var position: ScrollPosition
    @Binding var topMonth: CalendarMonth
    let onOpenDay: (CalendarDay) -> Void
    @State private var months: [CalendarMonth] = {
        let current = CalendarMonth(containing: Date(), in: .current)
        return (-120...120).map { current.adding(months: $0) }
    }()

    var body: some View {
        let isLunarShown = LunarDates.isShownForAppLanguage
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(months, id: \.self) { month in
                    MonthSection(model: model, month: month, isLunarShown: isLunarShown, onOpenDay: onOpenDay)
                        .onAppear { model.ensureMonth(month) }
                }
            }
            .scrollTargetLayout()
            .padding(.bottom)
        }
        .scrollPosition($position, anchor: .top)
        .onScrollTargetVisibilityChange(idType: CalendarMonth.self, threshold: 0.05) { visibleMonths in
            model.visibleMonths = Set(visibleMonths)
            if let first = visibleMonths.min(), first != topMonth {
                topMonth = first
            }
        }
        .safeAreaBar(edge: .top) {
            VStack(spacing: 0) {
                PendingChangeBox(model: model)
                    .padding(.horizontal)
                WeekdayRow(dates: model.dates)
            }
        }
        // Seven days across a phone hold a number and a chip only up to
        // the first accessibility size; beyond it the numbers would run
        // into each other. The day a tap opens has every size.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
    }
}

/// WeekdayRow is the weekdays' initials over the columns, the weekend ones
/// quieter, as the days under them are.
struct WeekdayRow: View {
    let dates: Calendar

    var body: some View {
        let weekStart = dates.dateInterval(of: .weekOfYear, for: Date())?.start ?? Date()
        HStack(spacing: 0) {
            ForEach(0..<7, id: \.self) { column in
                let day = dates.date(byAdding: .day, value: column, to: weekStart) ?? weekStart
                Text(day.formatted(.dateTime.weekday(.narrow)))
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(dates.isDateInWeekend(day) ? AnyShapeStyle(.secondary) : AnyShapeStyle(.primary))
                    .frame(maxWidth: .infinity)
            }
        }
        .padding(.horizontal, MonthWeekRow.horizontalMargin)
        .padding(.vertical, 6)
        .accessibilityHidden(true)
    }
}

/// MonthSection is one month: its name in large type, then its weeks.
struct MonthSection: View {
    let model: CalendarModel
    let month: CalendarMonth
    let isLunarShown: Bool
    let onOpenDay: (CalendarDay) -> Void

    var body: some View {
        let dates = model.dates
        let layout = MonthLayout(month: month, calendar: dates)
        let first = month.firstMoment(in: dates) ?? Date()
        VStack(alignment: .leading, spacing: 0) {
            Text(first.formatted(.dateTime.month(.wide)))
                .font(.title.bold())
                .padding(.horizontal)
                .padding(.top, 20)
                .padding(.bottom, 6)
                .accessibilityLabel(first.formatted(.dateTime.month(.wide).year()))
                .accessibilityAddTraits(.isHeader)
            ForEach(layout.weeks, id: \.self) { week in
                MonthWeekRow(model: model, week: week, isLunarShown: isLunarShown, onOpenDay: onOpenDay)
            }
        }
    }
}

/// MonthWeekRow is a week of a month under a hairline, its number small at
/// the line's left end, and a cell for each of its days in this month.
struct MonthWeekRow: View {
    static let horizontalMargin: CGFloat = 4

    let model: CalendarModel
    let week: MonthLayout.Week
    let isLunarShown: Bool
    let onOpenDay: (CalendarDay) -> Void
    @Environment(\.displayScale) private var displayScale
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let dates = model.dates
        // A cell's lines for chips and the "+N": three, or two once the
        // text is too large for more, so one chip and the count.
        let lineCount = dynamicTypeSize.isAccessibilitySize ? 2 : 3
        VStack(spacing: 2) {
            HStack(spacing: 4) {
                Text(week.weekNumber, format: .number)
                    .font(.caption2)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                Rectangle()
                    .fill(Color(.separator))
                    .frame(height: 1 / displayScale)
            }
            .accessibilityHidden(true)
            HStack(alignment: .top, spacing: 0) {
                ForEach(0..<7, id: \.self) { column in
                    if let day = week.days[column], let date = day.firstMoment(in: dates) {
                        MonthDayCell(
                            day: day, date: date, events: model.eventsByDay[day] ?? [],
                            isToday: dates.isDateInToday(date), isWeekend: dates.isDateInWeekend(date),
                            lunarName: isLunarShown ? LunarDates.name(of: day, timeZone: dates.timeZone) : nil,
                            lineCount: lineCount, colorOf: model.color(of:)
                        ) {
                            onOpenDay(day)
                        }
                    } else {
                        Color.clear
                            .frame(maxWidth: .infinity)
                            .frame(height: 1)
                    }
                }
            }
        }
        .padding(.horizontal, Self.horizontalMargin)
        .padding(.bottom, 4)
    }
}

/// MonthDayCell is a day in the month: its number, today's in a red circle
/// as Apple's Calendar draws it, the lunar date in Chinese, and its first
/// events as chips with "+N" for the rest. VoiceOver reads it as its date
/// and how many events it has.
struct MonthDayCell: View {
    let day: CalendarDay
    let date: Date
    let events: [CalendarEvent]
    let isToday: Bool
    let isWeekend: Bool
    let lunarName: String?
    let lineCount: Int
    let colorOf: @MainActor (CalendarEvent) -> Color
    let onOpen: () -> Void
    @ScaledMetric(relativeTo: .subheadline) private var circleSize: CGFloat = 28

    var body: some View {
        let split = EventChipSplit(eventCount: events.count, lineCount: lineCount)
        let size = min(circleSize, 44)
        let dateText = date.formatted(date: .complete, time: .omitted)
        Button(action: onOpen) {
            VStack(spacing: 2) {
                Text(day.day, format: .number)
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                    // White on the red circle in both appearances, as
                    // Calendar has it; the system red keeps its contrast in
                    // dark mode and with Increase Contrast.
                    .foregroundStyle(isToday ? AnyShapeStyle(.white) : isWeekend ? AnyShapeStyle(.secondary) : AnyShapeStyle(.primary))
                    .frame(width: size, height: size)
                    .background {
                        if isToday {
                            Circle().fill(.red)
                        }
                    }
                if let lunarName {
                    Text(lunarName)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                }
                VStack(spacing: 2) {
                    ForEach(0..<lineCount, id: \.self) { line in
                        if line < split.chipCount {
                            EventChip(event: events[line], color: colorOf(events[line]))
                        } else if line == split.chipCount, split.moreCount > 0 {
                            Text(verbatim: "+\(split.moreCount)")
                                .font(.caption2.weight(.medium))
                                .foregroundStyle(.secondary)
                                .lineLimit(1)
                                .padding(.horizontal, 3)
                                .padding(.vertical, 1)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        } else {
                            // Keeps every row of the month the same height.
                            EventChip(title: " ", color: .clear, isCancelled: false)
                                .hidden()
                        }
                    }
                }
            }
            .padding(.horizontal, 1)
            .frame(maxWidth: .infinity)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(isToday ? String(localized: "Today, \(dateText)") : dateText)
        .accessibilityValue(String(localized: "\(events.count) events"))
        .accessibilityAddTraits(.isButton)
    }
}

/// EventChip is an event in a day of the month: a small rounded tag tinted
/// with its calendar's color, a calendar glyph and the title cut short. The
/// words are the primary color, so they read on any calendar's tint in
/// light and dark.
struct EventChip: View {
    let title: String
    let color: Color
    let isCancelled: Bool
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(title: String, color: Color, isCancelled: Bool) {
        self.title = title
        self.color = color
        self.isCancelled = isCancelled
    }

    init(event: CalendarEvent, color: Color) {
        self.init(
            title: event.summary.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "(no title)"),
            color: color, isCancelled: event.isCancelled)
    }

    var body: some View {
        HStack(spacing: 2) {
            // At the accessibility sizes the glyph would leave no room for
            // the words, so the tint alone says which calendar it is.
            if !dynamicTypeSize.isAccessibilitySize {
                Image(systemName: "calendar")
                    .imageScale(.small)
                    .foregroundStyle(color)
            }
            Text(title)
                .lineLimit(1)
                .strikethrough(isCancelled)
        }
        .font(.caption2.weight(.medium))
        .foregroundStyle(isCancelled ? AnyShapeStyle(.secondary) : AnyShapeStyle(.primary))
        .padding(.horizontal, 3)
        .padding(.vertical, 1)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.opacity(0.22), in: .rect(cornerRadius: 4))
    }
}

/// CalendarDayScreen is a day opened from the month: its hours, with today
/// and the days either side along the bottom.
struct CalendarDayScreen: View {
    let model: CalendarModel
    @State private var day: CalendarDay
    let onOpen: (CalendarEvent) -> Void
    let onCreate: (Date) -> Void

    init(model: CalendarModel, day: CalendarDay, onOpen: @escaping (CalendarEvent) -> Void, onCreate: @escaping (Date) -> Void) {
        self.model = model
        _day = State(initialValue: day)
        self.onOpen = onOpen
        self.onCreate = onCreate
    }

    var body: some View {
        let date = day.firstMoment(in: model.dates) ?? Date()
        DayTimeline(model: model, day: date, onOpen: onOpen, onCreate: onCreate)
            .id(day)
            .navigationTitle(date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day()))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        onCreate(CalendarScreen.nextHour(on: date))
                    } label: {
                        Label("New event", systemImage: "plus")
                    }
                    .disabled(model.calendar == nil)
                }
                ToolbarItem(placement: .bottomBar) {
                    Button("Today") {
                        day = CalendarDay(containing: Date(), in: model.dates)
                    }
                }
                ToolbarSpacer(.flexible, placement: .bottomBar)
                ToolbarItemGroup(placement: .bottomBar) {
                    Button {
                        step(-1)
                    } label: {
                        Label("Previous day", systemImage: "chevron.backward")
                    }
                    Button {
                        step(1)
                    } label: {
                        Label("Next day", systemImage: "chevron.forward")
                    }
                }
            }
            .task(id: day) { model.ensureMonth(day.calendarMonth) }
    }

    private func step(_ direction: Int) {
        let dates = model.dates
        guard let date = day.firstMoment(in: dates), let next = dates.date(byAdding: .day, value: direction, to: date) else { return }
        day = CalendarDay(containing: next, in: dates)
    }
}

/// CalendarSearch finds events by their title or place, from a year back to
/// a year ahead. The server has no search of its own, so those two years
/// are read once and searched on the phone; what is still to come is listed
/// first.
struct CalendarSearch: View {
    let model: CalendarModel
    @Environment(\.dismiss) private var dismiss
    @State private var searchText = ""
    @State private var isSearchPresented = true
    @State private var events: [CalendarEvent]?
    @State private var problemText: String?
    @State private var opened: CalendarEvent?

    var body: some View {
        let groups = found
        NavigationStack {
            List {
                ForEach(groups, id: \.day) { group in
                    Section {
                        ForEach(group.events, id: \.occurrenceId) { event in
                            Button {
                                opened = event
                            } label: {
                                EventRow(event: event, color: model.color(of: event))
                            }
                            .buttonStyle(.plain)
                        }
                    } header: {
                        Text(group.date.formatted(.dateTime.weekday(.wide).month(.abbreviated).day().year()))
                    }
                }
            }
            .overlay {
                if let problemText {
                    ContentUnavailableView("Events could not be read", systemImage: "calendar.badge.exclamationmark", description: Text(problemText))
                } else if events == nil {
                    ProgressView()
                } else if words.isEmpty {
                    ContentUnavailableView("Search your calendar", systemImage: "magnifyingglass", description: Text("Titles and places, from a year back to a year ahead."))
                } else if groups.isEmpty {
                    ContentUnavailableView.search(text: words)
                }
            }
            .navigationTitle("Search")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, isPresented: $isSearchPresented, prompt: Text("Titles and places"))
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .task { await read() }
            .sheet(item: $opened) { event in
                EventOpener(model: model, event: event)
            }
        }
    }

    private var words: String { searchText.trimmingCharacters(in: .whitespacesAndNewlines) }

    struct DayGroup {
        let day: CalendarDay
        let date: Date
        let events: [CalendarEvent]
    }

    /// The events that match, by the day they start: upcoming days first,
    /// soonest first, then past days, latest first.
    private var found: [DayGroup] {
        guard let events, !words.isEmpty else { return [] }
        let dates = model.dates
        let matching = events.filter { event in
            (event.summary ?? "").localizedStandardContains(words) || (event.location ?? "").localizedStandardContains(words)
        }
        let byDay = Dictionary(grouping: matching) { event in
            CalendarDates.dayRange(of: event, in: dates)?.lowerBound ?? CalendarDay(year: 1970, month: 1, day: 1)
        }
        let today = CalendarDay(containing: Date(), in: dates)
        let upcoming = byDay.keys.filter { $0 >= today }.sorted()
        let past = byDay.keys.filter { $0 < today }.sorted(by: >)
        return (upcoming + past).map { day in
            DayGroup(
                day: day, date: day.firstMoment(in: dates) ?? Date(),
                events: (byDay[day] ?? []).sorted(by: CalendarDates.isShownBefore))
        }
    }

    private func read() async {
        guard events == nil else { return }
        let dates = model.dates
        let today = dates.startOfDay(for: Date())
        guard let calendar = model.calendar,
            let from = dates.date(byAdding: .year, value: -1, to: today),
            let until = dates.date(byAdding: .year, value: 1, to: today)
        else {
            problemText = String(localized: "The calendar is not read yet. Try again in a moment.")
            return
        }
        do {
            events = try await model.connection.calendarEvents(calendarId: calendar.id, from: from, until: until)
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// PendingChangeSection is a change whose answer never came back: it is
/// tried again or stopped before another is made, so nothing happens twice.
struct PendingChangeSection: View {
    let model: CalendarModel

    var body: some View {
        if model.pending != nil {
            Section {
                PendingChangeContent(model: model)
            }
        }
    }
}

/// PendingChangeContent says which change is in doubt, with its two ways out.
struct PendingChangeContent: View {
    let model: CalendarModel
    @State private var isWorking = false

    var body: some View {
        if let pending = model.pending {
            VStack(alignment: .leading, spacing: 10) {
                Label {
                    Text("“\(pending.summary)” may not have been saved: the answer did not come back. Try it again, or stop it.")
                } icon: {
                    Image(systemName: "exclamationmark.arrow.trianglehead.2.clockwise.rotate.90")
                }
                .font(.callout)
                HStack {
                    Button("Try again") {
                        work { await model.retryPending() }
                    }
                    .buttonStyle(.glassProminent)
                    Button("Stop", role: .destructive) {
                        work { await model.stopPending() }
                    }
                    .buttonStyle(.glass)
                }
                .disabled(isWorking)
            }
        }
    }

    private func work(_ action: @MainActor @escaping () async -> Void) {
        isWorking = true
        Task {
            await action()
            isWorking = false
        }
    }
}

/// DaySection is a day's events under its date.
struct DaySection: View {
    let model: CalendarModel
    let day: Date
    let isEmptyShown: Bool
    let onOpen: (CalendarEvent) -> Void

    var body: some View {
        let events = model.events(on: day)
        if !events.isEmpty || isEmptyShown {
            Section {
                if events.isEmpty {
                    Text("Nothing on this day")
                        .foregroundStyle(.secondary)
                }
                ForEach(events, id: \.occurrenceId) { event in
                    Button {
                        onOpen(event)
                    } label: {
                        EventRow(event: event, color: model.color(of: event))
                    }
                    .buttonStyle(.plain)
                }
            } header: {
                Text(day.formatted(.dateTime.weekday(.wide).month(.abbreviated).day()))
                    .foregroundStyle(model.dates.isDateInToday(day) ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
            }
        }
    }
}

/// EventRow is one event: its calendar's color, its time, its title and
/// where it is.
struct EventRow: View {
    let event: CalendarEvent
    let color: Color

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            RoundedRectangle(cornerRadius: 2)
                .fill(event.isCancelled ? AnyShapeStyle(.tertiary) : AnyShapeStyle(color))
                .frame(width: 4)
            VStack(alignment: .leading, spacing: 2) {
                Text(event.summary.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "(no title)"))
                    .font(.body)
                    .strikethrough(event.isCancelled)
                HStack(spacing: 4) {
                    Text(EventWords.when(event))
                    if event.recurring {
                        Image(systemName: "repeat")
                            .accessibilityLabel("Repeats")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                if let location = event.location, !location.isEmpty {
                    Text(location)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(.rect)
    }
}

enum EventWords {
    /// "All day", or the start and end times.
    static func when(_ event: CalendarEvent) -> String {
        if event.allDay { return String(localized: "All day") }
        guard let start = ServerTime.date(event.startsAt) else { return "" }
        let end = ServerTime.date(event.endsAt) ?? start
        return "\(start.formatted(date: .omitted, time: .shortened)) – \(end.formatted(date: .omitted, time: .shortened))"
    }
}

/// DayTimeline is a day as hours, with all-day events above them and each
/// event placed at its time; events at the same time share the width.
struct DayTimeline: View {
    let model: CalendarModel
    let day: Date
    let onOpen: (CalendarEvent) -> Void
    /// A tap on an empty hour starts an event at that half hour, as the
    /// dashboard's grid does.
    let onCreate: (Date) -> Void
    @ScaledMetric private var hourHeight: CGFloat = 56

    var body: some View {
        let events = model.events(on: day)
        let allDay = events.filter(\.allDay)
        let timed = placed(events.filter { !$0.allDay })
        ScrollViewReader { reader in
            ScrollView {
                VStack(alignment: .leading, spacing: 6) {
                    ZStack(alignment: .topLeading) {
                        VStack(spacing: 0) {
                            ForEach(0..<24, id: \.self) { hour in
                                HStack(alignment: .top, spacing: 6) {
                                    Text(hourText(hour))
                                        .font(.caption2)
                                        .foregroundStyle(.secondary)
                                        .frame(width: 44, alignment: .trailing)
                                    VStack { Divider() }
                                }
                                .frame(height: hourHeight, alignment: .top)
                                .id(hour)
                            }
                        }
                        .contentShape(.rect)
                        .onTapGesture { location in
                            let halfHours = max(0, min(47, Int((location.y - 6) / (hourHeight / 2))))
                            let start = Calendar.current.startOfDay(for: day).addingTimeInterval(Double(halfHours) * 1800)
                            onCreate(start)
                        }
                        if Calendar.current.isDateInToday(day) {
                            // Now, as a line across the hours.
                            TimelineView(.everyMinute) { context in
                                let hours = context.date.timeIntervalSince(Calendar.current.startOfDay(for: context.date)) / 3600
                                HStack(spacing: 0) {
                                    Circle().fill(.red).frame(width: 8, height: 8)
                                    Rectangle().fill(.red).frame(height: 1.5)
                                }
                                .padding(.leading, 46)
                                .offset(y: hours * hourHeight + 6 - 4)
                                .allowsHitTesting(false)
                                .accessibilityHidden(true)
                            }
                        }
                        GeometryReader { geometry in
                            let width = geometry.size.width - 50
                            ForEach(timed, id: \.event.occurrenceId) { item in
                                let color = model.color(of: item.event)
                                Button {
                                    onOpen(item.event)
                                } label: {
                                    VStack(alignment: .leading, spacing: 1) {
                                        Text(item.event.summary ?? "")
                                            .font(.footnote.weight(.semibold))
                                        Text(EventWords.when(item.event))
                                            .font(.caption2)
                                    }
                                    .padding(4)
                                    .frame(width: max(0, width / CGFloat(item.columns) - 2), height: max(22, item.height * hourHeight), alignment: .topLeading)
                                    // The tint over the plain background, so
                                    // the hour lines do not run through it.
                                    .background(color.opacity(0.2), in: .rect(cornerRadius: 6))
                                    .background(Color(.systemBackground), in: .rect(cornerRadius: 6))
                                    .overlay(alignment: .leading) {
                                        Rectangle().fill(color).frame(width: 3)
                                    }
                                    .clipShape(.rect(cornerRadius: 6))
                                }
                                .buttonStyle(.plain)
                                .offset(x: 50 + width / CGFloat(item.columns) * CGFloat(item.column), y: item.top * hourHeight + 6)
                            }
                        }
                    }
                    .frame(height: hourHeight * 24)
                }
                .padding(.horizontal)
            }
            // All-day events and a pending change stay above the hours,
            // as Calendar keeps them, rather than scrolling off with the
            // morning.
            .safeAreaBar(edge: .top) {
                if model.pending != nil || !allDay.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        PendingChangeBox(model: model)
                        ForEach(allDay, id: \.occurrenceId) { event in
                            Button {
                                onOpen(event)
                            } label: {
                                EventRow(event: event, color: model.color(of: event))
                                    .padding(8)
                                    .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 8))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal)
                    .padding(.bottom, 6)
                }
            }
            .onAppear {
                let hour = timed.first.map { Int($0.top) } ?? max(0, Calendar.current.component(.hour, from: Date()) - 1)
                reader.scrollTo(min(hour, 20), anchor: .top)
            }
        }
    }

    struct Placed {
        let event: CalendarEvent
        /// Hours from midnight to the start, and how many hours it lasts.
        let top: CGFloat
        let height: CGFloat
        var column = 0
        var columns = 1
    }

    /// Lays timed events into columns, so ones that overlap sit side by side.
    private func placed(_ events: [CalendarEvent]) -> [Placed] {
        let start = Calendar.current.startOfDay(for: day)
        var items: [Placed] = events.compactMap { event in
            guard let from = ServerTime.date(event.startsAt) else { return nil }
            let until = ServerTime.date(event.endsAt) ?? from.addingTimeInterval(1800)
            let top = max(0, from.timeIntervalSince(start) / 3600)
            let bottom = min(24, max(top + 0.25, until.timeIntervalSince(start) / 3600))
            return Placed(event: event, top: top, height: bottom - top)
        }
        var groupStart = 0
        var columnEnds: [CGFloat] = []
        var groupEnd: CGFloat = -1
        for index in items.indices {
            if items[index].top >= groupEnd {
                for member in groupStart..<index { items[member].columns = max(1, columnEnds.count) }
                groupStart = index
                columnEnds = []
            }
            if let free = columnEnds.firstIndex(where: { $0 <= items[index].top }) {
                items[index].column = free
                columnEnds[free] = items[index].top + items[index].height
            } else {
                items[index].column = columnEnds.count
                columnEnds.append(items[index].top + items[index].height)
            }
            groupEnd = max(groupEnd, items[index].top + items[index].height)
        }
        for member in groupStart..<items.count { items[member].columns = max(1, columnEnds.count) }
        return items
    }

    private func hourText(_ hour: Int) -> String {
        let date = Calendar.current.date(bySettingHour: hour, minute: 0, second: 0, of: Date()) ?? Date()
        return date.formatted(.dateTime.hour())
    }
}

/// PendingChangeBox is the pending change outside a list.
struct PendingChangeBox: View {
    let model: CalendarModel

    var body: some View {
        if model.pending != nil {
            PendingChangeContent(model: model)
                .padding()
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 12))
        }
    }
}

/// EventOpener reads an event whole before it is edited, since every field
/// is written back.
struct EventOpener: View {
    let model: CalendarModel
    let event: CalendarEvent
    @State private var draft: EventDraft?
    @State private var problemText: String?

    var body: some View {
        Group {
            if let draft {
                EventEditor(model: model, draft: draft)
            } else if let problemText {
                ContentUnavailableView("That event could not be read", systemImage: "calendar.badge.exclamationmark", description: Text(problemText))
            } else {
                ProgressView()
            }
        }
        .task {
            guard let calendar = model.calendar else { return }
            do {
                draft = EventDraft(try await model.connection.calendarEvent(calendarId: calendar.id, id: event.id))
            } catch {
                problemText = error.localizedDescription
            }
        }
    }
}

/// EventEditor is a new event, or one read whole: its title, when, where,
/// how it repeats, who is invited and notes.
struct EventEditor: View {
    let model: CalendarModel
    @State var draft: EventDraft
    @Environment(\.dismiss) private var dismiss
    @State private var isSaving = false
    @State private var isConfirmingDelete = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $draft.summary)
                    TextField("Location", text: $draft.location)
                }
                Section {
                    Toggle("All day", isOn: $draft.isAllDay)
                    DatePicker("Starts", selection: $draft.startsAt, displayedComponents: draft.isAllDay ? [.date] : [.date, .hourAndMinute])
                    DatePicker("Ends", selection: $draft.endsAt, in: draft.startsAt..., displayedComponents: draft.isAllDay ? [.date] : [.date, .hourAndMinute])
                    Picker("Repeat", selection: $draft.recurrence) {
                        Text("Never").tag("")
                        Text("Every day").tag("FREQ=DAILY")
                        Text("Every week").tag("FREQ=WEEKLY")
                        Text("Every month").tag("FREQ=MONTHLY")
                        Text("Every year").tag("FREQ=YEARLY")
                        if !EventDraft.repeats.contains(draft.recurrence) {
                            Text("As it was set").tag(draft.recurrence)
                        }
                    }
                }
                Section {
                    TextField("One address per line", text: $draft.invited, axis: .vertical)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.emailAddress)
                        .autocorrectionDisabled()
                } header: {
                    Text("Invited")
                } footer: {
                    Text("Invitations go out by mail when the event is saved.")
                }
                Section("Notes") {
                    TextField("Notes", text: $draft.notes, axis: .vertical)
                        .lineLimit(3...)
                }
                if draft.eventId != nil {
                    Section {
                        Button("Delete event", role: .destructive) {
                            isConfirmingDelete = true
                        }
                    }
                }
            }
            .navigationTitle(draft.eventId == nil ? String(localized: "New event") : String(localized: "Edit event"))
            .navigationBarTitleDisplayMode(.inline)
            .onChange(of: draft.startsAt) { previous, startsAt in
                // The end moves with the start, keeping how long it lasts.
                draft.endsAt = max(startsAt, draft.endsAt.addingTimeInterval(startsAt.timeIntervalSince(previous)))
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        save()
                    }
                    .disabled(isSaving || draft.summary.trimmingCharacters(in: .whitespaces).isEmpty || model.calendar == nil)
                }
            }
            .confirmationDialog("Delete “\(draft.summary)”?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                Button("Delete event", role: .destructive) {
                    delete()
                }
            } message: {
                Text(draft.recurrence.isEmpty ? String(localized: "It is removed from your calendar.") : String(localized: "Every time it repeats is removed."))
            }
            .interactiveDismissDisabled(isSaving)
        }
    }

    private func save() {
        guard let calendar = model.calendar else { return }
        isSaving = true
        Task {
            if await model.make(draft.change(calendar: calendar)) {
                dismiss()
            }
            isSaving = false
        }
    }

    private func delete() {
        guard let calendar = model.calendar, let eventId = draft.eventId else { return }
        isSaving = true
        Task {
            let change = CalendarChange(
                operation: "delete", summary: draft.summary,
                variables: ["calendarId": .string(calendar.id), "id": .string(eventId)])
            if await model.make(change) {
                dismiss()
            }
            isSaving = false
        }
    }
}
