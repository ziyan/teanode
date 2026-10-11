import SwiftUI
import TeaNodeKit

/// CalendarScreen is the dashboard's Calendar page on a phone: a month with
/// the chosen day's events under it, a week or five days as a list of days,
/// a day as hours, an agenda, and the reminders.
struct CalendarScreen: View {
    @State private var model: CalendarModel
    @State private var selectedDay = Calendar.current.startOfDay(for: Date())
    @State private var draft: EventDraft?
    @State private var opened: CalendarEvent?
    @State private var reminders: RemindersModel
    @State private var isAddingReminder = false

    init(connection: Connection, username: String) {
        _model = State(initialValue: CalendarModel(connection: connection, username: username))
        _reminders = State(initialValue: RemindersModel(connection: connection))
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.large)
                .withoutSwipeBack()
                .toolbar { toolbar }
                .refreshable {
                    if model.viewMode == .reminders {
                        await reminders.load()
                    } else {
                        await model.load()
                    }
                }
        }
        .task { await model.load() }
        .sheet(item: $draft) { draft in
            EventEditor(model: model, draft: draft)
        }
        .sheet(item: $opened) { event in
            EventOpener(model: model, event: event)
        }
        .sheet(isPresented: $isAddingReminder) {
            ReminderEditor(model: reminders, reminder: nil)
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }

    @ViewBuilder
    private var content: some View {
        switch model.viewMode {
        case .month:
            List {
                PendingChangeSection(model: model)
                Section {
                    MonthGrid(model: model, selectedDay: $selectedDay)
                        .listRowInsets(EdgeInsets(top: 8, leading: 8, bottom: 8, trailing: 8))
                }
                DaySection(model: model, day: selectedDay, isEmptyShown: true) { opened = $0 }
            }
        case .week, .workweek:
            List {
                PendingChangeSection(model: model)
                ForEach(model.days, id: \.self) { day in
                    DaySection(model: model, day: day, isEmptyShown: true) { opened = $0 }
                }
            }
        case .day:
            DayTimeline(model: model) { opened = $0 } onCreate: { start in
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
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Picker("View", selection: $model.viewMode) {
                    ForEach(CalendarModel.ViewMode.allCases) { mode in
                        Label(mode.title, systemImage: mode.systemImage).tag(mode)
                    }
                }
            } label: {
                Label("View", systemImage: model.viewMode.systemImage)
            }
        }
        // Today and the arrows along the bottom, as Apple's Calendar keeps
        // them, so the title has the top to itself.
        if model.viewMode != .reminders {
            ToolbarItemGroup(placement: .bottomBar) {
                Button("Today") {
                    selectedDay = Calendar.current.startOfDay(for: Date())
                    model.focus = selectedDay
                }
                Spacer()
                Button {
                    step(-1)
                } label: {
                    Label("Earlier", systemImage: "chevron.backward")
                }
                Button {
                    step(1)
                } label: {
                    Label("Later", systemImage: "chevron.forward")
                }
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            Button {
                if model.viewMode == .reminders {
                    isAddingReminder = true
                } else {
                    draft = EventDraft(startingAt: nextHour(on: model.viewMode == .month ? selectedDay : model.focus))
                }
            } label: {
                Label(model.viewMode == .reminders ? String(localized: "New reminder") : String(localized: "New event"), systemImage: "plus")
            }
            .disabled(model.viewMode != .reminders && model.calendar == nil)
        }
    }

    private func step(_ direction: Int) {
        model.step(direction)
        selectedDay = model.viewMode == .month
            ? model.dates.date(from: model.dates.dateComponents([.year, .month], from: model.focus)) ?? model.focus
            : model.focus
    }

    private var title: String {
        switch model.viewMode {
        case .month:
            return model.focus.formatted(.dateTime.month(.wide).year())
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
    private func nextHour(on day: Date) -> Date {
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

/// StepBar steps back and forward a month, a week or a day.
struct StepBar: View {
    let model: CalendarModel
    let title: String
    @Binding var selectedDay: Date

    var body: some View {
        HStack(spacing: 2) {
            Button {
                step(-1)
            } label: {
                Image(systemName: "chevron.backward")
                    .frame(minWidth: 36, minHeight: 44)
            }
            .accessibilityLabel("Earlier")
            Text(title)
                .font(.headline)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .contentTransition(.numericText())
            Button {
                step(1)
            } label: {
                Image(systemName: "chevron.forward")
                    .frame(minWidth: 36, minHeight: 44)
            }
            .accessibilityLabel("Later")
        }
    }

    private func step(_ direction: Int) {
        model.step(direction)
        selectedDay = model.viewMode == .month
            ? model.dates.date(from: model.dates.dateComponents([.year, .month], from: model.focus)) ?? model.focus
            : model.focus
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

/// MonthGrid is the month's six weeks: each day's number, today ringed, the
/// chosen day filled, and a dot for each of its first three events.
struct MonthGrid: View {
    let model: CalendarModel
    @Binding var selectedDay: Date

    var body: some View {
        let dates = model.dates
        let month = dates.component(.month, from: model.focus)
        let days = model.days
        // A plain grid, laid out once: a lazy one in a list row sized itself
        // again as the list scrolled, and the list jittered.
        Grid(horizontalSpacing: 0, verticalSpacing: 4) {
            GridRow {
                ForEach(Array(weekdayNames(dates).enumerated()), id: \.offset) { _, name in
                    Text(name)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                }
            }
            ForEach(0..<(days.count / 7), id: \.self) { week in
                GridRow {
                    ForEach(days[(week * 7)..<(week * 7 + 7)], id: \.self) { day in
                        dayCell(day, dates: dates, month: month)
                    }
                }
            }
        }
    }

    private func dayCell(_ day: Date, dates: Calendar, month: Int) -> some View {
        let isOtherMonth = dates.component(.month, from: day) != month
        let isToday = dates.isDateInToday(day)
        let isSelected = dates.isDate(day, inSameDayAs: selectedDay)
        let count = model.events(on: day).count
        return Button {
            selectedDay = day
        } label: {
            VStack(spacing: 3) {
                Text(day.formatted(.dateTime.day()))
                    .font(.callout)
                    .fontWeight(isToday ? .bold : .regular)
                    .foregroundStyle(isSelected ? AnyShapeStyle(.background) : isToday ? AnyShapeStyle(.tint) : isOtherMonth ? AnyShapeStyle(.tertiary) : AnyShapeStyle(.primary))
                    .frame(width: 34, height: 34)
                    .background {
                        if isSelected {
                            Circle().fill(isToday ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
                        }
                    }
                HStack(spacing: 3) {
                    ForEach(0..<min(count, 3), id: \.self) { _ in
                        Circle().fill(.secondary).frame(width: 4, height: 4)
                    }
                }
                .frame(height: 4)
            }
            .frame(maxWidth: .infinity, minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Text("\(day.formatted(date: .complete, time: .omitted)), \(count) events"))
    }

    private func weekdayNames(_ dates: Calendar) -> [String] {
        let names = dates.veryShortStandaloneWeekdaySymbols
        let first = dates.firstWeekday - 1
        return (0..<7).map { index in names[(first + index) % 7] }
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
                        EventRow(event: event)
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

/// EventRow is one event: its time, its title and where it is.
struct EventRow: View {
    let event: CalendarEvent

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            RoundedRectangle(cornerRadius: 2)
                .fill(event.isCancelled ? AnyShapeStyle(.tertiary) : AnyShapeStyle(.tint))
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
    let onOpen: (CalendarEvent) -> Void
    /// A tap on an empty hour starts an event at that half hour, as the
    /// dashboard's grid does.
    let onCreate: (Date) -> Void
    @ScaledMetric private var hourHeight: CGFloat = 56

    var body: some View {
        let events = model.events(on: model.focus)
        let allDay = events.filter(\.allDay)
        let timed = placed(events.filter { !$0.allDay })
        ScrollViewReader { reader in
            ScrollView {
                VStack(alignment: .leading, spacing: 6) {
                    PendingChangeBox(model: model)
                    ForEach(allDay, id: \.occurrenceId) { event in
                        Button {
                            onOpen(event)
                        } label: {
                            EventRow(event: event)
                                .padding(8)
                                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 8))
                        }
                        .buttonStyle(.plain)
                    }
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
                            let start = Calendar.current.startOfDay(for: model.focus).addingTimeInterval(Double(halfHours) * 1800)
                            onCreate(start)
                        }
                        if Calendar.current.isDateInToday(model.focus) {
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
                                    .background(.tint.opacity(0.2), in: .rect(cornerRadius: 6))
                                    .overlay(alignment: .leading) {
                                        Rectangle().fill(.tint).frame(width: 3)
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
        let start = Calendar.current.startOfDay(for: model.focus)
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
