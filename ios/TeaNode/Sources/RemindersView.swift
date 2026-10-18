import SwiftUI
import TeaNodeKit

/// RemindersModel is the reminders as the dashboard's Reminders view keeps
/// them: what is still to do, and what is done when asked for.
@MainActor
@Observable
final class RemindersModel {
    private(set) var open: [AgentReminder] = []
    private(set) var done: [AgentReminder] = []
    private(set) var isLoaded = false
    var isShowingDone = false {
        didSet { Task { await load() } }
    }
    var doneText: String?
    var problemText: String?

    let connection: Connection

    init(connection: Connection) {
        self.connection = connection
    }

    func load() async {
        do {
            open = try await connection.reminders(isDone: false)
            if isShowingDone {
                done = try await connection.reminders(isDone: true)
            }
            isLoaded = true
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Runs a change, says so, and reads the lists again.
    func act(saying said: String?, _ work: @MainActor () async throws -> Void) async -> Bool {
        do {
            try await work()
            doneText = said
            await load()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    func setDone(_ reminder: AgentReminder, isDone: Bool) async {
        let said = isDone
            ? (reminder.isRepeating == true ? String(localized: "Moved on to the next time") : String(localized: "Marked done"))
            : nil
        _ = await act(saying: said) {
            try await connection.setReminderDone(reminder.id, isDone: isDone)
        }
    }

    func delete(_ reminder: AgentReminder) async {
        _ = await act(saying: String(localized: "Reminder removed")) {
            try await connection.deleteReminder(reminder.id)
        }
    }
}

/// RemindersList is the reminders: To do and Done, a tap to tick one off,
/// and a tap on its words to change it.
struct RemindersList: View {
    let model: RemindersModel
    @State private var editing: AgentReminder?
    @State private var deleting: AgentReminder?
    @State private var quickTitle = ""

    var body: some View {
        @Bindable var model = model
        List {
            Section {
                Picker("Show", selection: $model.isShowingDone) {
                    Text("To do").tag(false)
                    Text("Done").tag(true)
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
            if !model.isShowingDone {
                // Adding is the list's first line, as on the dashboard: type
                // and press return.
                HStack(spacing: 12) {
                    Image(systemName: "plus.circle")
                        .font(.title2)
                        .foregroundStyle(.tint)
                        .frame(minWidth: 44)
                    TextField("Add a reminder", text: $quickTitle)
                        .submitLabel(.done)
                        .onSubmit {
                            let title = quickTitle.trimmed
                            guard !title.isEmpty else { return }
                            quickTitle = ""
                            Task {
                                _ = await model.act(saying: String(localized: "Reminder added")) {
                                    try await model.connection.saveReminder(nil, fields: ["title": .string(title)])
                                }
                            }
                        }
                }
            }
            let reminders = model.isShowingDone ? model.done : model.open
            ForEach(reminders) { reminder in
                ReminderRow(reminder: reminder) {
                    Task { await model.setDone(reminder, isDone: !reminder.isDone) }
                } onOpen: {
                    editing = reminder
                }
                .swipeActions {
                    Button("Delete", systemImage: "trash", role: .destructive) {
                        deleting = reminder
                    }
                    .tint(.red)
                }
                .confirmationDialog(
                    "Delete “\(deleting?.title ?? "")”?", isPresented: Binding(get: { deleting?.id == reminder.id }, set: { if !$0 { deleting = nil } }),
                    titleVisibility: .visible
                ) {
                    Button("Delete reminder", role: .destructive) {
                        guard let reminder = deleting else { return }
                        Task { await model.delete(reminder) }
                    }
                }
            }
        }
        .overlay {
            if model.isLoaded, (model.isShowingDone ? model.done : model.open).isEmpty {
                ContentUnavailableView(
                    model.isShowingDone ? LocalizedStringKey("Nothing done yet") : LocalizedStringKey("Nothing to do"),
                    systemImage: "checklist",
                    description: model.isShowingDone ? nil : Text("Add a reminder with +, or ask your agent to remind you."))
            }
        }
        .task { await model.load() }
        .sheet(item: $editing) { reminder in
            ReminderEditor(model: model, reminder: reminder)
        }
        .toast(done: $model.doneText, problem: $model.problemText)
    }
}

/// ReminderRow is one reminder: a circle to tick, its title, notes and when
/// it is due, red once overdue.
struct ReminderRow: View {
    let reminder: AgentReminder
    let onToggle: () -> Void
    let onOpen: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Button(action: onToggle) {
                Image(systemName: reminder.isDone ? "checkmark.circle.fill" : "circle")
                    .font(.title2)
                    .foregroundStyle(reminder.isDone ? AnyShapeStyle(.tint) : AnyShapeStyle(.secondary))
                    .contentTransition(.symbolEffect(.replace))
                    .frame(minWidth: 44, minHeight: 44)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(reminder.isDone ? Text("Mark not done") : Text("Mark done"))
            Button(action: onOpen) {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 4) {
                        if let priority = reminder.priority, priority > 0, priority <= 4 {
                            Text(verbatim: "!").foregroundStyle(.red).accessibilityLabel("High priority")
                        }
                        Text(reminder.title)
                            .foregroundStyle(reminder.isDone ? .secondary : .primary)
                    }
                    if let notes = reminder.notes, !notes.isEmpty {
                        Text(notes)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                    }
                    if let due = ReminderWords.due(reminder) {
                        HStack(spacing: 4) {
                            Text(due.text)
                            if reminder.isRepeating == true {
                                Image(systemName: "repeat").accessibilityLabel("Repeats")
                            }
                        }
                        .font(.footnote)
                        .foregroundStyle(due.isOverdue ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
                    }
                }
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
        }
    }
}

enum ReminderWords {
    /// When a reminder is due, in words, and whether that has passed.
    static func due(_ reminder: AgentReminder) -> (text: String, isOverdue: Bool)? {
        guard let dueAt = reminder.dueAt, let date = ServerTime.date(dueAt) else { return nil }
        if reminder.isDueDate == true, let day = CalendarDates.day(ofAllDay: dueAt), let local = Calendar.current.date(from: day) {
            let isOverdue = !reminder.isDone && local < Calendar.current.startOfDay(for: Date())
            return (local.formatted(date: .abbreviated, time: .omitted), isOverdue)
        }
        return (date.formatted(date: .abbreviated, time: .shortened), !reminder.isDone && date < Date())
    }
}

/// ReminderEditor adds a reminder or changes one: its title, notes, and a
/// day it is due, with a time or without.
struct ReminderEditor: View {
    let model: RemindersModel
    let reminder: AgentReminder?
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var notes = ""
    @State private var isDue = false
    @State private var hasTime = false
    @State private var dueAt = Date()
    @State private var isSaving = false
    @State private var isConfirmingDelete = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $title)
                    TextField("Notes", text: $notes, axis: .vertical)
                        .lineLimit(2...)
                }
                Section {
                    Toggle("Due", isOn: $isDue.animation())
                    if isDue {
                        Toggle("At a time", isOn: $hasTime.animation())
                        DatePicker("When", selection: $dueAt, displayedComponents: hasTime ? [.date, .hourAndMinute] : [.date])
                    }
                }
                if let reminder {
                    Section {
                        Button("Delete reminder", role: .destructive) {
                            isConfirmingDelete = true
                        }
                        .confirmationDialog("Delete “\(title)”?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                            Button("Delete reminder", role: .destructive) {
                                Task {
                                    await model.delete(reminder)
                                    dismiss()
                                }
                            }
                        }
                    } footer: {
                        if reminder.isRepeating == true {
                            Text("This reminder repeats. Ticking it off moves it on to the next time.")
                        }
                    }
                }
            }
            .navigationTitle(reminder == nil ? String(localized: "New reminder") : String(localized: "Edit reminder"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(isSaving || title.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            .onAppear(perform: fill)
        }
    }

    private func fill() {
        guard let reminder else { return }
        title = reminder.title
        notes = reminder.notes ?? ""
        guard let text = reminder.dueAt, let date = ServerTime.date(text) else { return }
        isDue = true
        if reminder.isDueDate == true, let day = CalendarDates.day(ofAllDay: text) {
            hasTime = false
            dueAt = Calendar.current.date(from: day) ?? date
        } else {
            hasTime = true
            dueAt = date
        }
    }

    /// The title, the notes and the due time, as the dashboard sends them: a
    /// day alone is due that day, a day and a time a moment where the phone
    /// is.
    private func save() {
        var fields: [String: JSONValue] = [
            "title": .string(title.trimmingCharacters(in: .whitespacesAndNewlines)),
            "notes": .string(notes.trimmingCharacters(in: .whitespacesAndNewlines)),
        ]
        if !isDue {
            if reminder?.dueAt != nil { fields["isDueCleared"] = .bool(true) }
        } else if hasTime {
            fields["dueAt"] = .string(CalendarDates.text(dueAt))
        } else {
            let day = Calendar.current.dateComponents([.year, .month, .day], from: dueAt)
            fields["dueOn"] = .string(String(format: "%04d-%02d-%02d", day.year ?? 1970, day.month ?? 1, day.day ?? 1))
        }
        isSaving = true
        Task {
            let isSaved = await model.act(saying: reminder == nil ? String(localized: "Reminder added") : String(localized: "Reminder saved")) {
                try await model.connection.saveReminder(reminder?.id, fields: fields)
            }
            isSaving = false
            if isSaved { dismiss() }
        }
    }
}
