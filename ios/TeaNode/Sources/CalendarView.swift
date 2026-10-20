import SwiftUI
import TeaNodeKit

/// CalendarScreen is the dashboard's Calendar page on a phone. It opens on
/// the months scrolling one after another, as Apple's Calendar does, and a
/// tap on a day opens that day as hours. The buttons at the bottom right
/// switch to the agenda, a day, a week or five days as a list of days, and
/// the reminders.
struct CalendarScreen: View {
    @Environment(AppModel.self) private var app
    @State private var model: CalendarModel
    @State private var draft: EventDraft?
    @State private var opened: CalendarEvent?
    @State private var reminders: RemindersModel
    @State private var isAddingReminder = false
    @State private var isSearching = false
    @State private var isShowingCalendars = false
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
                .toolbar { toolbar }
                .refreshable {
                    if model.viewMode == .reminders {
                        await reminders.load()
                    } else {
                        await model.reloadCalendars()
                    }
                }
                .navigationDestination(for: CalendarDay.self) { day in
                    CalendarDayScreen(model: model, day: day) { open($0) } onCreate: { start in
                        draft = EventDraft(startingAt: start)
                    }
                }
        }
        .task(id: app.services) {
            if app.services?.isCalendarEnabled == false {
                model.viewMode = .reminders
                path = []
                draft = nil
                opened = nil
                isSearching = false
            } else if app.services?.isRemindersEnabled == false && model.viewMode == .reminders {
                model.viewMode = .month
            }
            await model.load()
            #if DEBUG
                // The screenshots photograph a day opened from the month.
                if DemoServer.screen == "calendar-day", path.isEmpty {
                    path = [CalendarDay(containing: Date(), in: model.dates)]
                }
                // The calendars of other people as a list, which names whose each event is, and the list of calendars or the share screen open over the month.
                if DemoServer.screen == "calendar-shared" { model.viewMode = .agenda }
                // The editor of a weekday event, open on its Custom repeat.
                if DemoServer.screen == "calendar-repeat-custom", draft == nil { draft = EventDraft.demoWeekdayStandup }
                if DemoServer.screen == "calendar-sheet" || DemoServer.screen == "calendar-share-sheet" || DemoServer.screen == "calendar-find-time" {
                    isShowingCalendars = true
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
        .sheet(isPresented: $isShowingCalendars) {
            CalendarListSheet(model: model)
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }

    /// Opens an event to read or change. A busy block is not opened: all the
    /// person is told of it is that its owner is busy, so there is nothing
    /// in it.
    private func open(_ event: CalendarEvent) {
        guard !event.isBusy else { return }
        opened = event
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
                    DaySection(model: model, day: day, isEmptyShown: true) { open($0) }
                }
            }
        case .day:
            DayTimeline(model: model, day: model.focus) { open($0) } onCreate: { start in
                draft = EventDraft(startingAt: start)
            }
        case .agenda:
            let days = model.days.filter { !model.events(on: $0).isEmpty }
            List {
                PendingChangeSection(model: model)
                ForEach(days, id: \.self) { day in
                    DaySection(model: model, day: day, isEmptyShown: false) { open($0) }
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
        MoreBackButton()
        // Search and add at the top right, as Apple's Calendar has them.
        ToolbarItemGroup(placement: .topBarTrailing) {
            if model.viewMode != .reminders {
                Button {
                    isShowingCalendars = true
                } label: {
                    Label("Calendars", systemImage: "calendar.badge.checkmark")
                }
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
            if app.services?.isCalendarEnabled != false {
                modeToggle(.agenda)
                modeToggle(.day)
            }
            Menu {
                Picker("View", selection: $model.viewMode) {
                    ForEach(CalendarModel.ViewMode.allCases.filter { $0 == .reminders ? app.services?.isRemindersEnabled != false : app.services?.isCalendarEnabled != false }) { mode in
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
    /// Eight system colors for calendars, as many as the server spreads them
    /// over. Green is left out: in this app green is what can be pressed.
    static let palette: [Color] = [.blue, .orange, .purple, .teal, .indigo, .pink, .brown, .cyan]

    /// The color of a calendar that has none of its own: the place the server
    /// gave it among the colors of the person's calendars, which it spreads so
    /// that no two are alike, painted in this app's palette (the dashboard
    /// paints the same places in its own tones). Only when the server sent no
    /// place is the id hashed into the palette.
    private func spreadColor(of calendar: AgentCalendar?, calendarId: String) -> Color {
        if let colorIndex = calendar?.colorIndex {
            return Self.palette[CalendarPalette.spreadIndex(serverIndex: colorIndex)]
        }
        return Self.palette[CalendarPalette.index(calendarId: calendarId, paletteCount: Self.palette.count)]
    }

    /// The color an event is painted: its calendar's own when the server
    /// keeps one, or else the calendar's color in the spread, always the same
    /// one for the same calendar.
    func color(of event: CalendarEvent) -> Color {
        // The times somebody is busy are gray, whose calendar they are in.
        if event.isBusy { return Color(.systemGray) }
        let eventCalendar = calendar(withId: event.calendarId)
        if let parts = CalendarColorComponents(hexText: eventCalendar?.colour) {
            return Color(red: parts.red, green: parts.green, blue: parts.blue)
        }
        return spreadColor(of: eventCalendar, calendarId: event.calendarId)
    }

    /// The color a calendar is painted in the list of calendars.
    func color(of calendar: AgentCalendar) -> Color {
        if let parts = CalendarColorComponents(hexText: calendar.colour) {
            return Color(red: parts.red, green: parts.green, blue: parts.blue)
        }
        return spreadColor(of: calendar, calendarId: calendar.id)
    }
}

extension CalendarEvent {
    /// What to call it on screen: its title, or "Busy" for time its owner is
    /// busy and nothing else was shared.
    var shownTitle: String {
        if isBusy { return String(localized: "Busy") }
        return summary.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "(no title)")
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
/// screen, each read from the server as it comes into view. It starts with
/// a year back and two ahead and grows at either end as it is reached, so
/// opening on this month never lays out the years before it. The weekdays
/// stay at the top.
struct MonthScroll: View {
    let model: CalendarModel
    @Binding var position: ScrollPosition
    @Binding var topMonth: CalendarMonth
    let onOpenDay: (CalendarDay) -> Void
    @State private var months: [CalendarMonth] = {
        let current = CalendarMonth(containing: Date(), in: .current)
        return (-12...24).map { current.adding(months: $0) }
    }()
    @State private var hasPersonScrolled = false
    /// The room under the last week for the floating Today and views bar,
    /// which stays one height at every text size: the glass bar floats over
    /// the scroll view, and without this the last week stays under it.
    private let floatingBarClearance: CGFloat = 72

    var body: some View {
        let isLunarShown = LunarDates.isShownForAppLanguage
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                ForEach(months, id: \.self) { month in
                    MonthSection(model: model, month: month, isLunarShown: isLunarShown, onOpenDay: onOpenDay)
                        .onAppear {
                            model.ensureMonth(month)
                            extend(reaching: month)
                        }
                }
            }
            .scrollTargetLayout()
            .padding(.bottom, floatingBarClearance)
        }
        .scrollPosition($position, anchor: .top)
        // The month at the top names the year. It is read from the scroll
        // position, which settles where the view settles; the visible months
        // are also reported while the view first jumps to this month, from
        // wherever the estimated heights put it.
        .onChange(of: position.viewID(type: CalendarMonth.self)) { _, month in
            if let month, month != topMonth {
                topMonth = month
            }
        }
        .onScrollTargetVisibilityChange(idType: CalendarMonth.self, threshold: 0.05) { visibleMonths in
            model.visibleMonths = Set(visibleMonths)
            for month in visibleMonths {
                model.ensureMonth(month)
            }
        }
        .onScrollPhaseChange { _, phase in
            if phase == .interacting {
                hasPersonScrolled = true
            }
        }
        .task {
            // The month in view and its neighbors, whatever the first
            // layout reports.
            let target = position.viewID(type: CalendarMonth.self) ?? CalendarMonth(containing: Date(), in: model.dates)
            for offset in -1...1 {
                model.ensureMonth(target.adding(months: offset))
            }
            // The first jump lands where the estimated heights of the months
            // above put it, which can be a month off; once the months around
            // it are measured it is placed again, unless the person scrolled.
            for _ in 0..<2 {
                try? await Task.sleep(for: .milliseconds(200))
                guard !hasPersonScrolled else { break }
                position.scrollTo(id: target, anchor: .top)
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

extension MonthScroll {
    /// Another year of months past the end that was reached. Months put
    /// before the top keep the scroll position on the month in view.
    fileprivate func extend(reaching month: CalendarMonth) {
        if month == months.last {
            months.append(contentsOf: (1...12).map { month.adding(months: $0) })
        } else if month == months.first {
            months.insert(contentsOf: (1...12).reversed().map { month.adding(months: -$0) }, at: 0)
        }
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
            let monthEvents = model.monthEvents(month)
            ForEach(layout.weeks, id: \.self) { week in
                MonthWeekRow(model: model, monthEvents: monthEvents, week: week, isLunarShown: isLunarShown, onOpenDay: onOpenDay)
            }
        }
    }
}

/// MonthWeekRow is a week of a month under a hairline, its number small at
/// the line's left end, and a cell for each of its days in this month.
struct MonthWeekRow: View {
    static let horizontalMargin: CGFloat = 4

    let model: CalendarModel
    let monthEvents: MonthEvents
    let week: MonthLayout.Week
    let isLunarShown: Bool
    let onOpenDay: (CalendarDay) -> Void
    @Environment(\.displayScale) private var displayScale

    var body: some View {
        let dates = model.dates
        // A cell's lines for chips and the "+N": three, two chips and the
        // count. The accessibility sizes show a count alone, in the cell.
        let lineCount = 3
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
                            day: day, date: date, events: monthEvents.eventsByDay[day] ?? [],
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
/// events as chips with "+N" for the rest, or at the accessibility sizes
/// only how many there are, since a chip is cut to two letters there. VoiceOver
/// reads it as its date and how many events it has either way.
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
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        let split = EventChipSplit(eventCount: events.count, lineCount: lineCount)
        let size = min(circleSize, 44)
        let dateText = date.formatted(date: .complete, time: .omitted)
        let countColor = events.first.map { colorOf($0) } ?? Color.clear
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
                if dynamicTypeSize.isAccessibilitySize {
                    // A title would be cut to "Pa…", which says nothing; the
                    // day's own screen lists the events, so only the count
                    // is kept, in the tint of the day's first event. An empty
                    // day keeps the line's height, so the weeks stay even.
                    Text(events.count, format: .number)
                        .font(.caption2.weight(.medium))
                        .monospacedDigit()
                        .foregroundStyle(.primary)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(countColor.opacity(0.22), in: .capsule)
                        .frame(maxWidth: .infinity)
                        .opacity(events.isEmpty ? 0 : 1)
                } else {
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
    /// Whether to draw the lock of the person's own private events.
    let isPrivate: Bool
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    init(title: String, color: Color, isCancelled: Bool, isPrivate: Bool = false) {
        self.title = title
        self.color = color
        self.isCancelled = isCancelled
        self.isPrivate = isPrivate
    }

    init(event: CalendarEvent, color: Color) {
        self.init(title: event.shownTitle, color: color, isCancelled: event.isCancelled, isPrivate: event.isMarkedPrivate)
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
            if isPrivate {
                Image(systemName: "lock.fill")
                    .imageScale(.small)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel("Private")
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
                                EventRow(event: event, color: model.color(of: event), model: model)
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
                    ContentUnavailableView("Events could not be read", systemImage: "calendar.badge.exclamationmark", description: Text(problemText.asReasonSentence))
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
        guard model.calendar != nil,
            let from = dates.date(byAdding: .year, value: -1, to: today),
            let until = dates.date(byAdding: .year, value: 1, to: today)
        else {
            problemText = String(localized: "The calendar is not read yet. Try again in a moment.")
            return
        }
        do {
            // Every calendar shown, and none of the busy blocks: there is
            // nothing in them to find.
            var found: [CalendarEvent] = []
            for calendar in model.shownCalendars {
                found += try await model.connection.calendarEvents(calendarId: calendar.id, from: from, until: until)
            }
            events = found.filter { !$0.isBusy }
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
                PendingChangeContent(model: model, isInRow: true)
            }
        }
    }
}

/// PendingChangeContent says which change is in doubt, with its two ways out.
struct PendingChangeContent: View {
    let model: CalendarModel
    /// Whether the content is the rows of a list section: its two actions are then one row each,
    /// as in Settings, and otherwise a pair of capsule buttons of equal width.
    var isInRow = false
    @State private var isWorking = false

    var body: some View {
        if let pending = model.pending {
            if isInRow {
                pendingLabel(pending)
                Button("Try again") {
                    work { await model.retryPending() }
                }
                .disabled(isWorking)
                Button("Stop", role: .destructive) {
                    work { await model.stopPending() }
                }
                .disabled(isWorking)
            } else {
                VStack(alignment: .leading, spacing: 10) {
                    pendingLabel(pending)
                    HStack {
                        Button {
                            work { await model.retryPending() }
                        } label: {
                            Text("Try again").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                        Button(role: .destructive) {
                            work { await model.stopPending() }
                        } label: {
                            Text("Stop").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                    }
                    .buttonBorderShape(.capsule)
                    .controlSize(.large)
                    .disabled(isWorking)
                }
            }
        }
    }

    private func pendingLabel(_ pending: CalendarChange) -> some View {
        Label {
            Text("“\(pending.summary)” may not have been saved: the answer did not come back. Try it again, or stop it.")
        } icon: {
            Image(systemName: "exclamationmark.arrow.trianglehead.2.clockwise.rotate.90")
        }
        .font(.callout)
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
                        EventRow(event: event, color: model.color(of: event), model: model)
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
    /// What the row calls the event, and whose calendar it is in when that is
    /// another person's: several people's calendars share one list, and a gray
    /// "Busy" would not say whose.
    private let title: String
    private let ownerName: String?

    init(event: CalendarEvent, color: Color, model: CalendarModel) {
        self.event = event
        self.color = color
        title = model.shownTitle(of: event)
        // A busy block already carries the name in its title.
        ownerName = event.isBusy ? nil : model.ownerName(of: event)
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            RoundedRectangle(cornerRadius: 2)
                .fill(event.isCancelled ? AnyShapeStyle(.tertiary) : AnyShapeStyle(color))
                .frame(width: 4)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    if event.isMarkedPrivate {
                        Image(systemName: "lock.fill")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .accessibilityLabel("Private")
                    }
                    Text(title)
                        .font(.body)
                        .strikethrough(event.isCancelled)
                }
                if let ownerName {
                    Label(ownerName, systemImage: "person")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
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
        // Only the rows of other people's events have a symbol (the owner's
        // name): the line under every row starts at the row's edge.
        .fullWidthRowSeparator()
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
                                        HStack(spacing: 3) {
                                            if item.event.isMarkedPrivate {
                                                Image(systemName: "lock.fill")
                                                    .imageScale(.small)
                                                    .accessibilityLabel("Private")
                                            }
                                            Text(item.event.isBusy ? model.shownTitle(of: item.event) : (item.event.summary ?? ""))
                                        }
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
                                EventRow(event: event, color: model.color(of: event), model: model)
                                    .padding(8)
                                    .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 8))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal)
                    .padding(.bottom, 6)
                    // Only as tall as the rows: an event's color stripe
                    // would otherwise take all the height the bar offers.
                    .fixedSize(horizontal: false, vertical: true)
                }
            }
            .task {
                // Once the bar of all-day events above has taken its height,
                // or the hour would end up under it.
                let hour = timed.first.map { Int($0.top) } ?? max(0, Calendar.current.component(.hour, from: Date()) - 1)
                reader.scrollTo(min(hour, 20), anchor: .top)
                try? await Task.sleep(for: .milliseconds(200))
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
    /// An event in a calendar shared below edit, which is read and not changed.
    @State private var reading: CalendarEventDetail?
    @State private var problemText: String?

    var body: some View {
        Group {
            if let draft {
                EventEditor(model: model, draft: draft)
            } else if let reading {
                EventReader(model: model, calendar: model.calendar(withId: event.calendarId), eventDetail: reading)
            } else if let problemText {
                ContentUnavailableView("That event could not be read", systemImage: "calendar.badge.exclamationmark", description: Text(problemText.asReasonSentence))
            } else {
                ProgressView()
            }
        }
        .task {
            do {
                let eventDetail = try await model.connection.calendarEvent(calendarId: event.calendarId, id: event.id)
                if let calendar = model.calendar(withId: event.calendarId), !calendar.canWrite {
                    reading = eventDetail
                } else {
                    draft = EventDraft(eventDetail, calendarId: event.calendarId)
                }
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
    @State private var memory = InMemoryModel()
    /// The screens pushed over the editor, which is only the Custom repeat.
    @State private var path: [EventEditorScreen] = []

    /// EventEditorScreen is a screen the editor pushes.
    private enum EventEditorScreen: Hashable {
        case customRepeat
    }

    /// The calendar the event is in or goes in: the person's own unless they
    /// chose another.
    private var target: AgentCalendar? {
        draft.calendarId.flatMap { model.calendar(withId: $0) } ?? model.calendar
    }

    var body: some View {
        NavigationStack(path: $path) {
            Form {
                // Which calendar a new event goes in, when there is a choice:
                // their own, a circle's, or one shared with them at edit. An
                // event already kept stays where it is.
                if draft.eventId == nil, model.writableCalendars.count > 1 {
                    Section {
                        Picker("Calendar", selection: $draft.calendarId) {
                            ForEach(model.writableCalendars) { calendar in
                                Text(model.displayName(of: calendar)).tag(Optional(calendar.id))
                            }
                        }
                    }
                }
                Section {
                    TextField("Title", text: $draft.summary)
                    TextField("Location", text: $draft.location)
                }
                Section {
                    Toggle("All day", isOn: $draft.isAllDay)
                    DatePicker("Starts", selection: $draft.startsAt, displayedComponents: draft.isAllDay ? [.date] : [.date, .hourAndMinute])
                    DatePicker("Ends", selection: $draft.endsAt, in: draft.startsAt..., displayedComponents: draft.isAllDay ? [.date] : [.date, .hourAndMinute])
                    repeatRows
                }
                // Guests only in the person's own calendar: the invitations go
                // out from, and the answers come back to, the owner.
                if target?.isOwn ?? true {
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
                }
                // Only in the person's own calendar: in a circle's, or one shared
                // with them at edit, every reader is an editor and sees the
                // event whatever it says.
                if target?.isOwn ?? true {
                    Section {
                        Toggle("Private", isOn: $draft.isPrivate)
                    } footer: {
                        Text("Others you share your calendar with see only that you are busy.")
                    }
                }
                Section("Notes") {
                    TextField("Notes", text: $draft.notes, axis: .vertical)
                        .lineLimit(3...)
                }
                InMemorySection(model: memory)
                if draft.eventId != nil {
                    Section {
                        Button("Delete event", role: .destructive) {
                            isConfirmingDelete = true
                        }
                        .confirmationDialog("Delete “\(draft.summary)”?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                            Button("Delete event", role: .destructive) {
                                delete()
                            }
                        } message: {
                            Text(draft.recurrence.isEmpty ? String(localized: "It is removed from your calendar.") : String(localized: "Every time it repeats is removed."))
                        }
                    }
                }
            }
            .navigationTitle(draft.eventId == nil ? String(localized: "New event") : String(localized: "Edit event"))
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: EventEditorScreen.self) { _ in
                CustomRepeatScreen(rule: $draft.recurrence, context: draft.repeatContext(in: target))
            }
            .inMemory(memory, recordKind: "calendar_event", recordId: draft.eventId ?? "")
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
                    .disabled(isSaving || draft.summary.trimmingCharacters(in: .whitespaces).isEmpty || target == nil)
                }
            }
            .interactiveDismissDisabled(isSaving)
            .task {
                // A new event goes in the person's own calendar unless they
                // choose another, so the choice starts there.
                if draft.calendarId == nil { draft.calendarId = model.calendar?.id }
                #if DEBUG
                    // The screenshot of the Custom repeat opens straight on it.
                    if DemoServer.screen == "calendar-repeat-custom", path.isEmpty { path = [.customRepeat] }
                #endif
            }
        }
    }

    // MARK: Repeat

    /// The Repeat choice, and for a custom repeat the sentence it makes as a
    /// row that opens the screen to change it.
    @ViewBuilder
    private var repeatRows: some View {
        Picker("Repeat", selection: repeatPresetBinding) {
            Text("Never").tag(CalendarRepeat.Preset.never)
            Text("Every day").tag(CalendarRepeat.Preset.daily)
            Text("Every weekday").tag(CalendarRepeat.Preset.weekdays)
            Text("Every week").tag(CalendarRepeat.Preset.weekly)
            Text("Every month").tag(CalendarRepeat.Preset.monthly)
            Text("Every year").tag(CalendarRepeat.Preset.yearly)
            Text(verbatim: customRepeatTitle).tag(CalendarRepeat.Preset.custom)
        }
        if CalendarRepeat.Preset(rule: draft.recurrence) == .custom {
            NavigationLink(value: EventEditorScreen.customRepeat) {
                Text(verbatim: customRepeatSentence)
            }
        }
    }

    /// The named repeat the rule is. Choosing Custom pushes its screen and
    /// leaves the rule as it was, since the rule is only changed there.
    private var repeatPresetBinding: Binding<CalendarRepeat.Preset> {
        Binding(
            get: { CalendarRepeat.Preset(rule: draft.recurrence) },
            set: { preset in
                if let rule = preset.rule {
                    draft.recurrence = rule
                } else {
                    path.append(.customRepeat)
                }
            })
    }

    private var customRepeatParts: CalendarRepeat.Parts? {
        CalendarRepeat.parse(draft.recurrence, context: draft.repeatContext(in: target))
    }

    /// What the Custom choice is called: how often when the rule can be
    /// read, as "Every 2 weeks", and "Custom" when it cannot.
    private var customRepeatTitle: String {
        customRepeatParts.map(CalendarRepeatWords.shortText) ?? String(localized: "Custom")
    }

    private var customRepeatSentence: String {
        guard let parts = customRepeatParts else { return String(localized: "Set in another program") }
        return CalendarRepeatWords.sentenceText(CalendarRepeat.sentence(for: parts))
    }

    private func save() {
        guard let calendar = target else { return }
        isSaving = true
        Task {
            if await model.make(draft.change(calendar: calendar)) {
                dismiss()
            }
            isSaving = false
        }
    }

    private func delete() {
        guard let calendar = target, let eventId = draft.eventId else { return }
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
