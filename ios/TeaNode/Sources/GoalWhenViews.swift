import SwiftUI
import TeaNodeKit

/// GoalTimeDraft is one of a goal's times in the editor: a time of day on
/// some days of the week, a single moment, or a line of any other kind
/// that is kept as it is written.
struct GoalTimeDraft: Identifiable, Hashable {
    /// Kind is how the time reads in the editor.
    enum Kind: Hashable {
        case weekly, moment, line
    }

    let id = UUID()
    var kind: Kind = .weekly
    var clock: Date
    var dayNumbers: Set<Int> = Set(1...5)
    var moment: Date
    var line = ""

    /// A new weekly time, weekdays at eight.
    init() {
        clock = Self.today(hour: 8)
        moment = Calendar.current.date(byAdding: .day, value: 1, to: Self.today(hour: 9)) ?? Self.today(hour: 9)
    }

    /// A time as the server keeps it.
    init(time: String) {
        self.init()
        switch GoalTime(time) {
        case .weekly(let timing):
            kind = .weekly
            clock = Calendar.current.date(bySettingHour: timing.hour, minute: timing.minute, second: 0, of: .now) ?? clock
            dayNumbers = Set(timing.dayNumbers)
        case .moment(let goalMoment):
            kind = .moment
            moment = goalMoment.date()
        case .other(let otherLine):
            kind = .line
            line = otherLine
        }
    }

    private static func today(hour: Int) -> Date {
        Calendar.current.date(bySettingHour: hour, minute: 0, second: 0, of: .now) ?? .now
    }

    /// Whether it can be saved: a weekly time needs a day, a line needs words.
    var isComplete: Bool {
        switch kind {
        case .weekly: !dayNumbers.isEmpty
        case .moment: true
        case .line: !line.trimmed.isEmpty
        }
    }

    /// As the server takes it.
    var text: String {
        switch kind {
        case .weekly:
            let parts = Calendar.current.dateComponents([.hour, .minute], from: clock)
            return ScheduleTiming(hour: parts.hour ?? 0, minute: parts.minute ?? 0, dayNumbers: dayNumbers.sorted()).cron
        case .moment:
            return GoalMoment(date: moment).text
        case .line:
            return line.trimmed
        }
    }
}

/// GoalWhenDraft is when a goal runs as the editor holds it: at its own
/// pace, at set times (days and a time, a single moment, several of them),
/// or when mail arrives (what a message must match). The one copy of that
/// editor serves Start a goal and a goal's When.
struct GoalWhenDraft {
    /// Choice is what drives the goal's turns.
    enum Choice: Hashable {
        case ownPace, setTimes, mail
    }

    var choice: Choice = .ownPace
    var times: [GoalTimeDraft] = [GoalTimeDraft()]
    var conditions: [MailRuleEditor.Edited<MailRule.Condition>] = [MailRuleEditor.Edited(value: MailRule.Condition())]
    var isNotifiedWhenPosted = false
    /// A goal that has both times and mail keeps both when saved.
    private var hasBoth = false

    init() {}

    init(goal: AgentGoal) {
        if !goal.goalTimes.isEmpty {
            choice = .setTimes
            times = goal.goalTimes.map { GoalTimeDraft(time: $0) }
        } else if !goal.goalMailConditions.isEmpty {
            choice = .mail
        }
        if !goal.goalMailConditions.isEmpty {
            conditions = goal.goalMailConditions.map { MailRuleEditor.Edited(value: $0) }
        }
        isNotifiedWhenPosted = goal.isGoalNotifiedWhenPosted
        hasBoth = !goal.goalTimes.isEmpty && !goal.goalMailConditions.isEmpty
    }

    /// The conditions as they are saved: what they look for and the header's
    /// name without the spaces round them.
    private var trimmedConditions: [MailRule.Condition] {
        conditions.map { edited in
            var condition = edited.value
            condition.value = condition.value?.trimmed
            condition.header = condition.header?.trimmed
            return condition
        }
    }

    private var sentTimes: [String] {
        if choice == .setTimes || (choice == .mail && hasBoth) { return times.map(\.text) }
        return []
    }

    private var sentConditions: [MailRule.Condition] {
        if choice == .mail || (choice == .setTimes && hasBoth) { return trimmedConditions }
        return []
    }

    /// Whether what the choice needs is all there.
    var isComplete: Bool {
        switch choice {
        case .ownPace: true
        case .setTimes: !times.isEmpty && times.allSatisfy(\.isComplete)
        case .mail: GoalMailTrigger.isComplete(trimmedConditions)
        }
    }

    /// What to send. A new goal sends only the times and conditions it has;
    /// a change always sends both, so that a choice of its own pace takes the
    /// old ones away.
    func change(isUpdate: Bool) -> AgentGoalChange {
        var change = AgentGoalChange()
        let timesToSend = sentTimes
        let conditionsToSend = sentConditions
        if isUpdate || !timesToSend.isEmpty { change.goalTimes = timesToSend }
        if isUpdate || !conditionsToSend.isEmpty { change.goalMailConditions = conditionsToSend }
        if choice == .setTimes { change.isGoalNotifiedWhenPosted = isNotifiedWhenPosted }
        return change
    }
}

/// GoalWhenSections is the form sections that choose when a goal runs: the
/// choice, and under it the time editor or the mail conditions editor.
struct GoalWhenSections: View {
    @Binding var draft: GoalWhenDraft
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        Section {
            choicePicker
        } header: {
            Text("When")
        } footer: {
            if draft.choice == .ownPace {
                Text("Your agent decides when it takes a turn.")
            }
        }
        if draft.choice == .setTimes {
            timeSections
        }
        if draft.choice == .mail {
            mailSection
        }
    }

    /// Segmented, or a menu at the accessibility text sizes, where three
    /// segments no longer fit.
    @ViewBuilder
    private var choicePicker: some View {
        if dynamicTypeSize.isAccessibilitySize {
            choiceControl.pickerStyle(.menu)
        } else {
            choiceControl.pickerStyle(.segmented)
        }
    }

    private var choiceControl: some View {
        Picker("When", selection: $draft.choice) {
            Text("Own pace").tag(GoalWhenDraft.Choice.ownPace)
            Text("Set times").tag(GoalWhenDraft.Choice.setTimes)
            Text("On mail").tag(GoalWhenDraft.Choice.mail)
        }
        .labelsHidden()
    }

    @ViewBuilder
    private var timeSections: some View {
        ForEach($draft.times) { $time in
            Section {
                GoalTimeRows(time: $time)
                if draft.times.count > 1 {
                    Button("Remove this time", systemImage: "trash", role: .destructive) {
                        let removedId = time.id
                        draft.times.removeAll { $0.id == removedId }
                    }
                }
            }
        }
        Section {
            Button("Add a time", systemImage: "plus") {
                draft.times.append(GoalTimeDraft())
            }
            .actionColor()
            Toggle("Notify me when it posts", isOn: $draft.isNotifiedWhenPosted)
        } footer: {
            Text("Times are in your zone.")
        }
    }

    private var mailSection: some View {
        Section {
            ForEach($draft.conditions) { $condition in
                MailConditionEditor(condition: $condition.value, fields: GoalMailTrigger.fields, words: MailRuleWords(folders: []))
            }
            .onDelete { offsets in draft.conditions.remove(atOffsets: offsets) }
            Button("Add a condition", systemImage: "plus") {
                draft.conditions.append(MailRuleEditor.Edited(value: MailRule.Condition()))
            }
            .actionColor()
        } header: {
            Text("When all of these match")
        } footer: {
            Text("The goal takes a turn for each new message that matches all of these. To take one for every new message, choose Any message.")
        }
    }
}

/// GoalTimeRows is one time of a goal: every week on some days at a time,
/// or once at a moment; a line of any other kind is kept as it is written.
struct GoalTimeRows: View {
    @Binding var time: GoalTimeDraft

    var body: some View {
        if time.kind == .line {
            TextField("When, as a cron line", text: $time.line, prompt: Text(verbatim: "0 8 * * 1-5"))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .fontDesign(.monospaced)
        } else {
            Picker("Repeats", selection: $time.kind) {
                Text("Every week").tag(GoalTimeDraft.Kind.weekly)
                Text("Once").tag(GoalTimeDraft.Kind.moment)
            }
            if time.kind == .weekly {
                DatePicker("At", selection: $time.clock, displayedComponents: .hourAndMinute)
                NavigationLink {
                    BriefDaysScreen(dayNumbers: $time.dayNumbers)
                } label: {
                    LabeledContent("On days", value: UpkeepWords.dayPatternText(Array(time.dayNumbers)))
                }
            } else {
                DatePicker("On", selection: $time.moment, displayedComponents: [.date, .hourAndMinute])
            }
        }
    }
}

/// GoalWhenEditor changes when a goal runs, in a sheet: the same sections
/// Start a goal has.
struct GoalWhenEditor: View {
    let connection: Connection
    let goal: AgentGoal
    let onSaved: () -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var draft: GoalWhenDraft
    @State private var isSaving = false
    @State private var problemText: String?

    init(connection: Connection, goal: AgentGoal, onSaved: @escaping () -> Void) {
        self.connection = connection
        self.goal = goal
        self.onSaved = onSaved
        _draft = State(initialValue: GoalWhenDraft(goal: goal))
    }

    var body: some View {
        NavigationStack {
            Form {
                GoalWhenSections(draft: $draft)
                if let problemText {
                    Text(problemText.asReasonSentence).foregroundStyle(.red)
                }
            }
            .navigationTitle("When")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await save() } }
                        .disabled(!draft.isComplete || isSaving)
                }
            }
        }
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        do {
            try await connection.updateGoal(goal.conversationId, change: draft.change(isUpdate: true))
            onSaved()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

#if DEBUG
    /// The screen `-TeaNodeScreen goal-when-editor` photographs: the editor
    /// open on a goal that takes a turn when mail arrives.
    struct GoalWhenEditorDemoScreen: View {
        let connection: Connection
        @State private var isEditing = false

        private let goal = AgentGoal(
            conversationId: "g4", goalTitle: "School pictures", goalDescription: "Save the pictures and videos it links to the photo library.",
            goalMailConditions: [MailRule.Condition(field: "from", comparisonOperator: "contains", value: "news@school.example")])

        var body: some View {
            Color(.systemGroupedBackground)
                .ignoresSafeArea()
                .sheet(isPresented: $isEditing) {
                    GoalWhenEditor(connection: connection, goal: goal) {}
                }
                // Opened once the screen is in place: a sheet asked for as
                // the screen is pushed is never shown.
                .task {
                    try? await Task.sleep(for: .seconds(1))
                    isEditing = true
                }
        }
    }

    /// The screen `-TeaNodeScreen goal-detail` photographs: a goal's page,
    /// with its times.
    struct GoalDetailDemoScreen: View {
        let connection: Connection
        @State private var doneText: String?
        @State private var problemText: String?

        var body: some View {
            GoalDetail(connection: connection, conversationId: "g2", doneText: $doneText, problemText: $problemText)
        }
    }
#endif
